package sqlite

import (
	"context"
	"math"
	"path/filepath"
	"testing"
)

func openParityTestStore(t *testing.T) (*Store, *LibraryRepo) {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st, NewLibraryRepo(st)
}

func TestCascadeMarkPlayedAndUnplayed(t *testing.T) {
	st, repo := openParityTestStore(t)
	ctx := context.Background()

	lib, err := repo.CreateLibrary(ctx, "TV", "tvshows", []string{"/tv"})
	if err != nil {
		t.Fatal(err)
	}
	u := &UserRecord{Name: "cascade-tester"}
	if err := NewUserRepo(st).CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	userID := u.ID

	// Series -> Season 1 -> Ep 1, Ep 2
	series := &ItemRecord{ID: "ser1", LibraryID: lib.ID, ParentID: lib.ID, Type: "Series", IsFolder: true, Name: "Test Show"}
	season := &ItemRecord{ID: "sea1", LibraryID: lib.ID, ParentID: "ser1", SeriesID: "ser1", Type: "Season", IsFolder: true, Name: "Season 1"}
	ep1 := &ItemRecord{ID: "ep1", LibraryID: lib.ID, ParentID: "sea1", SeriesID: "ser1", SeasonID: "sea1", Type: "Episode", MediaType: "Video", Name: "Ep 1"}
	ep2 := &ItemRecord{ID: "ep2", LibraryID: lib.ID, ParentID: "sea1", SeriesID: "ser1", SeasonID: "sea1", Type: "Episode", MediaType: "Video", Name: "Ep 2"}
	for _, rec := range []*ItemRecord{series, season, ep1, ep2} {
		if err := repo.UpsertItem(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}

	// Mark Series played -> Season and both Episodes must become played
	ud, err := repo.MarkPlayed(ctx, userID, series.ID)
	if err != nil || !ud.Played {
		t.Fatalf("MarkPlayed series: ud=%+v err=%v", ud, err)
	}
	for _, id := range []string{"sea1", "ep1", "ep2"} {
		childUD, err := repo.GetUserItemData(ctx, userID, id)
		if err != nil || !childUD.Played {
			t.Fatalf("expected %s to be played after series MarkPlayed, got %+v (err=%v)", id, childUD, err)
		}
	}

	// Mark Season unplayed -> both Episodes must become unplayed
	ud, err = repo.MarkUnplayed(ctx, userID, season.ID)
	if err != nil || ud.Played {
		t.Fatalf("MarkUnplayed season: ud=%+v err=%v", ud, err)
	}
	for _, id := range []string{"ep1", "ep2"} {
		childUD, err := repo.GetUserItemData(ctx, userID, id)
		if err != nil || childUD.Played {
			t.Fatalf("expected %s to be unplayed after season MarkUnplayed, got %+v (err=%v)", id, childUD, err)
		}
	}

	// MusicAlbum -> Audio tracks cascade
	album := &ItemRecord{ID: "alb1", LibraryID: lib.ID, ParentID: lib.ID, Type: "MusicAlbum", IsFolder: true, Name: "Album 1"}
	track1 := &ItemRecord{ID: "trk1", LibraryID: lib.ID, ParentID: "alb1", Type: "Audio", MediaType: "Audio", Name: "Track 1"}
	track2 := &ItemRecord{ID: "trk2", LibraryID: lib.ID, ParentID: "alb1", Type: "Audio", MediaType: "Audio", Name: "Track 2"}
	for _, rec := range []*ItemRecord{album, track1, track2} {
		if err := repo.UpsertItem(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repo.MarkPlayed(ctx, userID, album.ID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"trk1", "trk2"} {
		childUD, _ := repo.GetUserItemData(ctx, userID, id)
		if !childUD.Played {
			t.Fatalf("expected track %s played after album MarkPlayed", id)
		}
	}
	if _, err := repo.MarkUnplayed(ctx, userID, album.ID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"trk1", "trk2"} {
		childUD, _ := repo.GetUserItemData(ctx, userID, id)
		if childUD.Played {
			t.Fatalf("expected track %s unplayed after album MarkUnplayed", id)
		}
	}

	// BoxSet -> Member movies cascade
	mov1 := &ItemRecord{ID: "mov1", LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", MediaType: "Video", Name: "Movie 1"}
	mov2 := &ItemRecord{ID: "mov2", LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", MediaType: "Video", Name: "Movie 2"}
	for _, rec := range []*ItemRecord{mov1, mov2} {
		if err := repo.UpsertItem(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}
	col, err := repo.CreateCollection(ctx, "Trilogy", "", []string{mov1.ID, mov2.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.MarkPlayed(ctx, userID, col.ID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{mov1.ID, mov2.ID} {
		childUD, _ := repo.GetUserItemData(ctx, userID, id)
		if !childUD.Played {
			t.Fatalf("expected movie %s played after BoxSet MarkPlayed", id)
		}
	}
	if _, err := repo.MarkUnplayed(ctx, userID, col.ID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{mov1.ID, mov2.ID} {
		childUD, _ := repo.GetUserItemData(ctx, userID, id)
		if childUD.Played {
			t.Fatalf("expected movie %s unplayed after BoxSet MarkUnplayed", id)
		}
	}
}

func TestHydrateDTOsEmbyParityFields(t *testing.T) {
	st, repo := openParityTestStore(t)
	ctx := context.Background()

	lib, err := repo.CreateLibrary(ctx, "TV", "tvshows", []string{"/tv"})
	if err != nil {
		t.Fatal(err)
	}
	u := &UserRecord{Name: "hydrate-tester"}
	if err := NewUserRepo(st).CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	userID := u.ID

	series := &ItemRecord{ID: "ser1", LibraryID: lib.ID, ParentID: lib.ID, Type: "Series", IsFolder: true, Name: "Andor"}
	season1 := &ItemRecord{ID: "sea1", LibraryID: lib.ID, ParentID: "ser1", SeriesID: "ser1", Type: "Season", IsFolder: true, Name: "Season 1"}
	season2 := &ItemRecord{ID: "sea2", LibraryID: lib.ID, ParentID: "ser1", SeriesID: "ser1", Type: "Season", IsFolder: true, Name: "Season 2"}
	ep1 := &ItemRecord{
		ID: "ep1", LibraryID: lib.ID, ParentID: "sea1", SeriesID: "ser1", SeasonID: "sea1",
		Type: "Episode", MediaType: "Video", Name: "Episode 1",
		Extra: ItemExtra{
			Chapters: []ChapterRecord{
				{StartPositionTicks: 0, Name: "Intro"},
				{StartPositionTicks: 1200000000, Name: "Main"},
			},
		},
	}
	ep2 := &ItemRecord{ID: "ep2", LibraryID: lib.ID, ParentID: "sea1", SeriesID: "ser1", SeasonID: "sea1", Type: "Episode", MediaType: "Video", Name: "Episode 2"}
	ep3 := &ItemRecord{ID: "ep3", LibraryID: lib.ID, ParentID: "sea2", SeriesID: "ser1", SeasonID: "sea2", Type: "Episode", MediaType: "Video", Name: "Episode 3"}
	for _, rec := range []*ItemRecord{series, season1, season2, ep1, ep2, ep3} {
		if err := repo.UpsertItem(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}

	// Series Primary + Backdrop images
	if err := repo.ReplaceItemImages(ctx, "ser1", []ImageRecord{
		{ItemID: "ser1", ImageType: "Primary", ImageIndex: 0, Path: "/p.jpg", Tag: "ser-pri-tag"},
		{ItemID: "ser1", ImageType: "Backdrop", ImageIndex: 0, Path: "/b.jpg", Tag: "ser-bd-tag"},
	}); err != nil {
		t.Fatal(err)
	}

	// Tags on Series
	if err := repo.ReplaceItemTaxonomies(ctx, "ser1", "tag", []string{"豆瓣 Top 250", "IMDb Top 250"}); err != nil {
		t.Fatal(err)
	}

	// MediaSource + MediaStreams on ep1
	if err := repo.ReplaceMediaSourcesAndStreams(ctx, "ep1", []MediaSourceRecord{
		{
			ID: "ms-ep1", ItemID: "ep1", Name: "main", Path: "/tv/ep1.mkv", Container: "mkv",
			Streams: []MediaStreamRecord{
				{MediaSourceID: "ms-ep1", ItemID: "ep1", StreamIndex: 0, StreamType: "Video", Codec: "hevc", Width: 1920, Height: 1080},
				{MediaSourceID: "ms-ep1", ItemID: "ep1", StreamIndex: 1, StreamType: "Audio", Codec: "aac", Language: "eng"},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	// Mark ep1 played (1 of 3 episodes in Series, 1 of 2 in Season 1)
	if _, err := repo.MarkPlayed(ctx, userID, "ep1"); err != nil {
		t.Fatal(err)
	}

	// Verify Series stats + Tags via QueryItems (even without explicit Fields=Tags)
	items, _, err := repo.QueryItems(ctx, ItemFilter{
		ServerID:         "srv1",
		UserID:           userID,
		IncludeItemTypes: []string{"Series"},
	})
	if err != nil || len(items) != 1 {
		t.Fatalf("QueryItems series: len=%d err=%v", len(items), err)
	}
	serDTO := items[0]
	if serDTO.ChildCount == nil || *serDTO.ChildCount != 2 {
		t.Fatalf("expected Series ChildCount=2, got %v", serDTO.ChildCount)
	}
	if serDTO.RecursiveItemCount == nil || *serDTO.RecursiveItemCount != 3 {
		t.Fatalf("expected Series RecursiveItemCount=3, got %v", serDTO.RecursiveItemCount)
	}
	if serDTO.UserData == nil || serDTO.UserData.UnplayedItemCount == nil || *serDTO.UserData.UnplayedItemCount != 2 {
		t.Fatalf("expected Series UnplayedItemCount=2, got %+v", serDTO.UserData)
	}
	if serDTO.UserData.PlayedPercentage == nil || math.Abs(*serDTO.UserData.PlayedPercentage-33.3333) > 0.1 {
		t.Fatalf("expected Series PlayedPercentage~33.33, got %v", serDTO.UserData.PlayedPercentage)
	}
	if len(serDTO.Tags) != 2 {
		t.Fatalf("expected 2 tags on Series DTO, got %v", serDTO.Tags)
	}

	// Verify Season stats + SeriesPrimaryImageTag + ParentBackdrop*
	seaDTO, err := repo.GetItemByID(ctx, "srv1", userID, "sea1")
	if err != nil {
		t.Fatal(err)
	}
	if seaDTO.ChildCount == nil || *seaDTO.ChildCount != 2 {
		t.Fatalf("expected Season ChildCount=2, got %v", seaDTO.ChildCount)
	}
	if seaDTO.UserData == nil || seaDTO.UserData.UnplayedItemCount == nil || *seaDTO.UserData.UnplayedItemCount != 1 {
		t.Fatalf("expected Season UnplayedItemCount=1, got %+v", seaDTO.UserData)
	}
	if seaDTO.UserData.PlayedPercentage == nil || math.Abs(*seaDTO.UserData.PlayedPercentage-50.0) > 0.01 {
		t.Fatalf("expected Season PlayedPercentage=50, got %v", seaDTO.UserData.PlayedPercentage)
	}
	if seaDTO.SeriesPrimaryImageTag != "ser-pri-tag" {
		t.Fatalf("expected SeriesPrimaryImageTag=ser-pri-tag, got %q", seaDTO.SeriesPrimaryImageTag)
	}
	if seaDTO.ParentBackdropItemId != "ser1" || len(seaDTO.ParentBackdropImageTags) != 1 || seaDTO.ParentBackdropImageTags[0] != "ser-bd-tag" {
		t.Fatalf("expected ParentBackdrop from ser1, got id=%q tags=%v", seaDTO.ParentBackdropItemId, seaDTO.ParentBackdropImageTags)
	}

	// Verify Episode MediaStreams + Chapters + ParentBackdrop*
	epDTO, err := repo.GetItemByID(ctx, "srv1", userID, "ep1")
	if err != nil {
		t.Fatal(err)
	}
	if len(epDTO.MediaStreams) != 2 {
		t.Fatalf("expected 2 top-level MediaStreams on Episode, got %d", len(epDTO.MediaStreams))
	}
	if len(epDTO.Chapters) != 2 || epDTO.Chapters[1].Name != "Main" || epDTO.Chapters[1].StartPositionTicks != 1200000000 {
		t.Fatalf("unexpected Chapters on Episode: %+v", epDTO.Chapters)
	}
	if epDTO.SeriesPrimaryImageTag != "ser-pri-tag" || epDTO.ParentBackdropItemId != "ser1" {
		t.Fatalf("unexpected Episode parent images: pri=%q bdId=%q", epDTO.SeriesPrimaryImageTag, epDTO.ParentBackdropItemId)
	}
}

func TestAlphabetFilterQueryItems(t *testing.T) {
	_, repo := openParityTestStore(t)
	ctx := context.Background()

	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{"/movies"})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []struct{ id, name string }{
		{"m1", "2001: A Space Odyssey"},
		{"m2", "Alien"},
		{"m3", "Batman"},
		{"m4", "Blade Runner"},
		{"m5", "Matrix"},
	} {
		if err := repo.UpsertItem(ctx, &ItemRecord{
			ID: m.id, LibraryID: lib.ID, ParentID: lib.ID,
			Type: "Movie", MediaType: "Video", Name: m.name,
		}); err != nil {
			t.Fatal(err)
		}
	}

	// NameStartsWith=B -> Batman, Blade Runner
	items, total, err := repo.QueryItems(ctx, ItemFilter{NameStartsWith: "B", IncludeItemTypes: []string{"Movie"}})
	if err != nil || total != 2 || len(items) != 2 {
		t.Fatalf("NameStartsWith=B: total=%d len=%d err=%v", total, len(items), err)
	}

	// NameStartsWithOrGreater=M -> Matrix
	items, total, err = repo.QueryItems(ctx, ItemFilter{NameStartsWithOrGreater: "M", IncludeItemTypes: []string{"Movie"}})
	if err != nil || total != 1 || items[0].Name != "Matrix" {
		t.Fatalf("NameStartsWithOrGreater=M: total=%d items=%+v err=%v", total, items, err)
	}

	// NameLessThan=A -> 2001: A Space Odyssey
	items, total, err = repo.QueryItems(ctx, ItemFilter{NameLessThan: "A", IncludeItemTypes: []string{"Movie"}})
	if err != nil || total != 1 || items[0].Name != "2001: A Space Odyssey" {
		t.Fatalf("NameLessThan=A: total=%d items=%+v err=%v", total, items, err)
	}
}
