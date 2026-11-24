package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func openTestMultiVersionRepo(t *testing.T) (*Store, *LibraryRepo) {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "mv.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, NewLibraryRepo(store)
}

func TestMultiVersionFindersAndUpsertMediaSource(t *testing.T) {
	ctx := context.Background()
	_, repo := openTestMultiVersionRepo(t)

	lib1, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{"/media/movies"})
	if err != nil {
		t.Fatal(err)
	}
	lib2, err := repo.CreateLibrary(ctx, "Other", "movies", []string{"/media/other"})
	if err != nil {
		t.Fatal(err)
	}

	// Primary movie with lowercase "tmdb" and uppercase "Imdb"
	m1 := &ItemRecord{
		ID:            "movie001",
		LibraryID:     lib1.ID,
		ParentID:      lib1.ID,
		Type:          "Movie",
		MediaType:     "Video",
		Name:          "流浪地球",
		OriginalTitle: "The Wandering Earth",
		SortName:      "流浪地球",
		Path:          "/media/movies/4k/wandering.mkv",
		Container:     "mkv",
		Extra: ItemExtra{
			ProviderIDs: map[string]string{
				"tmdb": "535167",
				"Imdb": "tt7605074",
			},
		},
	}
	if err := repo.UpsertItem(ctx, m1); err != nil {
		t.Fatal(err)
	}

	// Upsert primary MediaSource (default Protocol/ExtraJSON/ID)
	if err := repo.UpsertMediaSourceAndStreams(ctx, m1.ID, MediaSourceRecord{
		Name:      "2160p DolbyVision · MKV",
		Path:      m1.Path,
		Container: "mkv",
		Size:      1000,
		Streams: []MediaStreamRecord{
			{StreamIndex: 0, StreamType: "Video", Codec: "hevc", Height: 2160, VideoRange: "DOVI"},
		},
	}); err != nil {
		t.Fatalf("UpsertMediaSourceAndStreams primary: %v", err)
	}

	// Upsert second (alternate) MediaSource — must preserve the first!
	altPath := "/media/movies/1080p/wandering.strm"
	if err := repo.UpsertMediaSourceAndStreams(ctx, m1.ID, MediaSourceRecord{
		Name:      "1080p · STRM",
		Path:      altPath,
		Protocol:  "Http",
		Container: "strm",
		Size:      64,
		IsRemote:  true,
		ExtraJSON: `{"file_mtime":1700000000}`,
		Streams: []MediaStreamRecord{
			{StreamIndex: 0, StreamType: "Video", Codec: "h264", Height: 1080},
			{StreamIndex: 1, StreamType: "Audio", Codec: "aac", Channels: 2},
		},
	}); err != nil {
		t.Fatalf("UpsertMediaSourceAndStreams alt: %v", err)
	}

	// Update existing alternate MediaSource idempotently by (item_id, path)
	if err := repo.UpsertMediaSourceAndStreams(ctx, m1.ID, MediaSourceRecord{
		Name:      "1080p Updated · STRM",
		Path:      altPath,
		Protocol:  "Http",
		Container: "strm",
		Size:      128,
		IsRemote:  true,
		ExtraJSON: `{"file_mtime":1700000100}`,
		Streams: []MediaStreamRecord{
			{StreamIndex: 0, StreamType: "Video", Codec: "hevc", Height: 1080},
		},
	}); err != nil {
		t.Fatalf("UpsertMediaSourceAndStreams update: %v", err)
	}

	sources, err := repo.GetMediaSourcesByItemID(ctx, m1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 2 {
		t.Fatalf("expected 2 media sources preserved, got %d", len(sources))
	}
	if sources[1].Name != "1080p Updated · STRM" || sources[1].Size != 128 || len(sources[1].Streams) != 1 {
		t.Fatalf("unexpected updated alt source: %+v", sources[1])
	}

	// FindItemByMediaSourcePath: hits primary path, hits alternate path, misses empty/unknown
	gotPrimary, err := repo.FindItemByMediaSourcePath(ctx, lib1.ID, m1.Path)
	if err != nil || gotPrimary.ID != m1.ID {
		t.Fatalf("FindItemByMediaSourcePath primary: got=%+v err=%v", gotPrimary, err)
	}
	gotAlt, err := repo.FindItemByMediaSourcePath(ctx, lib1.ID, altPath)
	if err != nil || gotAlt.ID != m1.ID {
		t.Fatalf("FindItemByMediaSourcePath alt: got=%+v err=%v", gotAlt, err)
	}
	if _, err := repo.FindItemByMediaSourcePath(ctx, lib1.ID, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for empty path, got %v", err)
	}
	if _, err := repo.FindItemByMediaSourcePath(ctx, lib1.ID, "/nonexistent.mkv"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing path, got %v", err)
	}

	// FindItemByProviderID: case-insensitive key ("Tmdb", "tmdb", "TMDB", "imdb", "IMDB") and library scoping
	for _, tc := range []struct {
		key, val, itemType string
	}{
		{"Tmdb", "535167", "Movie"},
		{"tmdb", "535167", "Movie"},
		{"TMDB", "535167", ""},
		{"imdb", "tt7605074", "Movie"},
		{"IMDb", "TT7605074", "Movie"},
	} {
		got, err := repo.FindItemByProviderID(ctx, lib1.ID, tc.itemType, tc.key, tc.val)
		if err != nil || got.ID != m1.ID {
			t.Fatalf("FindItemByProviderID(%q,%q,%q) = %+v, %v", tc.itemType, tc.key, tc.val, got, err)
		}
	}
	if _, err := repo.FindItemByProviderID(ctx, lib2.ID, "Movie", "Tmdb", "535167"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound in different library, got %v", err)
	}
	if _, err := repo.FindItemByProviderID(ctx, lib1.ID, "Movie", "", "535167"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for empty key, got %v", err)
	}
	if _, err := repo.FindItemByProviderID(ctx, lib1.ID, "Movie", "Tmdb", "999999"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing value, got %v", err)
	}

	// FindMovieInDirByBaseTitle: same dir hits by Name & OriginalTitle; subdir does not match
	if got, err := repo.FindMovieInDirByBaseTitle(ctx, lib1.ID, "/media/movies/4k", "流浪地球"); err != nil || got.ID != m1.ID {
		t.Fatalf("FindMovieInDirByBaseTitle by name: %+v, %v", got, err)
	}
	if got, err := repo.FindMovieInDirByBaseTitle(ctx, lib1.ID, "/media/movies/4k", "the wandering earth"); err != nil || got.ID != m1.ID {
		t.Fatalf("FindMovieInDirByBaseTitle by original title: %+v, %v", got, err)
	}
	if _, err := repo.FindMovieInDirByBaseTitle(ctx, lib1.ID, "/media/movies", "流浪地球"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected parent dir not to match nested file, got %v", err)
	}
	if _, err := repo.FindMovieInDirByBaseTitle(ctx, lib1.ID, "", "流浪地球"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for empty dir, got %v", err)
	}
	if _, err := repo.FindMovieInDirByBaseTitle(ctx, lib1.ID, "/media/movies/4k", "Other Movie"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for non-matching title, got %v", err)
	}

	// FindSeasonByNumber & FindEpisodeByNumber
	sNum, eNum := 1, 3
	series := &ItemRecord{ID: "series01", LibraryID: lib1.ID, ParentID: lib1.ID, Type: "Series", IsFolder: true, Name: "三体"}
	season := &ItemRecord{ID: "season01", LibraryID: lib1.ID, ParentID: series.ID, SeriesID: series.ID, Type: "Season", IsFolder: true, Name: "Season 1", IndexNumber: &sNum}
	ep := &ItemRecord{ID: "ep0103", LibraryID: lib1.ID, ParentID: season.ID, SeriesID: series.ID, SeasonID: season.ID, Type: "Episode", Name: "Ep 3", ParentIndexNumber: &sNum, IndexNumber: &eNum}
	for _, rec := range []*ItemRecord{series, season, ep} {
		if err := repo.UpsertItem(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := repo.FindSeasonByNumber(ctx, series.ID, 1); err != nil || got.ID != season.ID {
		t.Fatalf("FindSeasonByNumber: %+v, %v", got, err)
	}
	if _, err := repo.FindSeasonByNumber(ctx, "", 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for empty seriesID")
	}
	if _, err := repo.FindSeasonByNumber(ctx, series.ID, 9); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing season")
	}
	if got, err := repo.FindEpisodeByNumber(ctx, series.ID, 1, 3); err != nil || got.ID != ep.ID {
		t.Fatalf("FindEpisodeByNumber: %+v, %v", got, err)
	}
	if _, err := repo.FindEpisodeByNumber(ctx, "", 1, 3); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for empty seriesID")
	}
	if _, err := repo.FindEpisodeByNumber(ctx, series.ID, 1, 99); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing episode")
	}
}

func TestMergeAndSplitItemVersions(t *testing.T) {
	ctx := context.Background()
	_, repo := openTestMultiVersionRepo(t)

	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{"/movies"})
	if err != nil {
		t.Fatal(err)
	}

	year := 2024
	rt := int64(7200_0000000)
	makeMovie := func(id, name, path, container string, size int64) {
		rec := &ItemRecord{
			ID:             id,
			LibraryID:      lib.ID,
			ParentID:       lib.ID,
			Type:           "Movie",
			MediaType:      "Video",
			Name:           name,
			ProductionYear: &year,
			RuntimeTicks:   &rt,
			Container:      container,
			Path:           path,
			Extra:          ItemExtra{Overview: "Overview", ProviderIDs: map[string]string{"Tmdb": "42"}},
		}
		if err := repo.UpsertItem(ctx, rec); err != nil {
			t.Fatal(err)
		}
		if err := repo.ReplaceItemAncestors(ctx, id, []AncestorLink{{AncestorID: lib.ID, Depth: 1}}); err != nil {
			t.Fatal(err)
		}
		if err := repo.UpsertSearchFTS(ctx, id, name, name, "", "", ""); err != nil {
			t.Fatal(err)
		}
		if err := repo.UpsertMediaSourceAndStreams(ctx, id, MediaSourceRecord{
			Name:         name + " " + container,
			Path:         path,
			Container:    container,
			Size:         size,
			RuntimeTicks: rt,
			Streams: []MediaStreamRecord{
				{StreamIndex: 0, StreamType: "Video", Codec: "hevc"},
			},
		}); err != nil {
			t.Fatal(err)
		}
	}

	makeMovie("m1", "Dune", "/movies/4k/dune.mkv", "mkv", 5000)
	makeMovie("m2", "Dune", "/movies/1080p/dune.mp4", "mp4", 2000)
	makeMovie("m3", "Dune", "/movies/4k/dune.mkv", "mkv", 5000) // duplicate path to test dedup

	// Validation errors
	if _, _, err := repo.MergeItemVersions(ctx, []string{"m1", "m1", ""}); err == nil {
		t.Fatalf("expected error merging fewer than 2 distinct IDs")
	}
	if _, _, err := repo.MergeItemVersions(ctx, []string{"missing-primary", "m2"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing primary, got %v", err)
	}

	// Merge m1, m2, m3, and a nonexistent secondary ID
	primaryID, mergedCount, err := repo.MergeItemVersions(ctx, []string{"m1", "m2", "m3", "nonexistent-secondary"})
	if err != nil {
		t.Fatalf("MergeItemVersions: %v", err)
	}
	if primaryID != "m1" || mergedCount != 2 {
		t.Fatalf("expected primary=m1 mergedCount=2, got %s %d", primaryID, mergedCount)
	}

	// Secondary items removed
	if _, err := repo.GetItemRecordByID(ctx, "m2"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected m2 deleted, got %v", err)
	}
	if _, err := repo.GetItemRecordByID(ctx, "m3"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected m3 deleted, got %v", err)
	}

	// Primary now has exactly 2 deduplicated MediaSources, each with its stream reassigned to m1
	srcs, err := repo.GetMediaSourcesByItemID(ctx, "m1")
	if err != nil {
		t.Fatal(err)
	}
	if len(srcs) != 2 {
		t.Fatalf("expected 2 deduplicated media sources on m1, got %d", len(srcs))
	}
	for _, s := range srcs {
		if s.ItemID != "m1" || len(s.Streams) != 1 || s.Streams[0].ItemID != "m1" {
			t.Fatalf("unexpected merged media source/streams: %+v", s)
		}
	}

	// Split m1 back into independent items
	createdIDs, err := repo.SplitItemVersions(ctx, "m1")
	if err != nil {
		t.Fatalf("SplitItemVersions: %v", err)
	}
	if len(createdIDs) != 1 {
		t.Fatalf("expected 1 split item created, got %v", createdIDs)
	}

	// Primary now has 1 MediaSource; new item has 1 MediaSource, metadata, ancestors, and FTS
	srcsAfter, err := repo.GetMediaSourcesByItemID(ctx, "m1")
	if err != nil || len(srcsAfter) != 1 {
		t.Fatalf("expected 1 source remaining on m1, got %d err=%v", len(srcsAfter), err)
	}
	splitRec, err := repo.GetItemRecordByID(ctx, createdIDs[0])
	if err != nil {
		t.Fatalf("GetItemRecordByID split item: %v", err)
	}
	if splitRec.Name != "Dune" || splitRec.Path != "/movies/1080p/dune.mp4" || splitRec.Container != "mp4" {
		t.Fatalf("unexpected split item record: %+v", splitRec)
	}
	splitSrcs, err := repo.GetMediaSourcesByItemID(ctx, createdIDs[0])
	if err != nil || len(splitSrcs) != 1 || len(splitSrcs[0].Streams) != 1 || splitSrcs[0].Streams[0].ItemID != createdIDs[0] {
		t.Fatalf("unexpected split media sources: %+v err=%v", splitSrcs, err)
	}

	// Splitting a single-version item is a no-op
	noopIDs, err := repo.SplitItemVersions(ctx, "m1")
	if err != nil || len(noopIDs) != 0 {
		t.Fatalf("expected empty split on single-version item, got %v err=%v", noopIDs, err)
	}
	// Splitting missing item returns ErrNotFound
	if _, err := repo.SplitItemVersions(ctx, "does-not-exist"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound splitting missing item, got %v", err)
	}
}
