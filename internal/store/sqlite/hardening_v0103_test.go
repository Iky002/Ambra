package sqlite

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/pkg/guid"
)

func TestOpenEscapedURIPathAndMigrationV4Indexes(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	specialPath := filepath.Join(dir, "ambra%20#1?v=2.db")
	store, err := Open(specialPath)
	if err != nil {
		t.Fatalf("Open with special URI characters failed: %v", err)
	}
	defer store.Close()

	var v4 int
	if err := store.Reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = 4`).Scan(&v4); err != nil || v4 != 1 {
		t.Fatalf("expected schema_migrations v4=1, got %d err=%v", v4, err)
	}

	for _, idxName := range []string{
		"idx_users_name_nocase",
		"idx_activity_logs_created_at",
		"idx_item_collections_item_id",
	} {
		var cnt int
		if err := store.Reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, idxName).Scan(&cnt); err != nil || cnt != 1 {
			t.Errorf("expected index %s to exist, got cnt=%d err=%v", idxName, cnt, err)
		}
	}
}

func TestNextUpNumericOrderingAndExtrasExclusion(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "nextup_v0103.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	userRepo := NewUserRepo(store)
	libRepo := NewLibraryRepo(store)
	user := &UserRecord{ID: guid.New(), Name: "u", Policy: DefaultUserPolicy(false), Config: DefaultUserConfig()}
	if err := userRepo.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	lib, err := libRepo.CreateLibrary(ctx, "TV", "tvshows", []string{"/tv"})
	if err != nil {
		t.Fatal(err)
	}

	s1 := 1
	ep1Num, ep2Num := 1, 2
	mkShow := func(name string) (seriesID, ep1ID, ep2ID string) {
		seriesID = guid.New()
		seasonID := guid.New()
		ep1ID = guid.New()
		ep2ID = guid.New()
		_ = libRepo.UpsertItem(ctx, &ItemRecord{ID: seriesID, LibraryID: lib.ID, ParentID: lib.ID, Type: "Series", IsFolder: true, Name: name})
		_ = libRepo.UpsertItem(ctx, &ItemRecord{ID: seasonID, LibraryID: lib.ID, ParentID: seriesID, SeriesID: seriesID, Type: "Season", IsFolder: true, Name: "S1", IndexNumber: &s1})
		_ = libRepo.UpsertItem(ctx, &ItemRecord{ID: ep1ID, LibraryID: lib.ID, ParentID: seasonID, SeriesID: seriesID, SeasonID: seasonID, Type: "Episode", Name: name + " E1", ParentIndexNumber: &s1, IndexNumber: &ep1Num})
		_ = libRepo.UpsertItem(ctx, &ItemRecord{ID: ep2ID, LibraryID: lib.ID, ParentID: seasonID, SeriesID: seriesID, SeasonID: seasonID, Type: "Episode", Name: name + " E2", ParentIndexNumber: &s1, IndexNumber: &ep2Num})
		return
	}

	showNullTs, nullEp1, _ := mkShow("ShowNullDate")
	showOldTs, oldEp1, _ := mkShow("ShowOld")
	showNewTs, newEp1, _ := mkShow("ShowNew")

	// Also insert an extra Episode that must be excluded from NextUp.
	_ = libRepo.UpsertItem(ctx, &ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: showNewTs, SeriesID: showNewTs,
		Type: "Episode", Name: "Trailer Extra", ParentIndexNumber: &s1, IndexNumber: &ep1Num,
		Extra: ItemExtra{ExtraType: "trailer"},
	})

	// Insert user_item_data directly with controlled last_played_date (NULL, 1000, 2000).
	_, err = store.Writer.ExecContext(ctx, `
		INSERT INTO user_item_data (user_id, item_id, played, play_count, playback_position_ticks, last_played_date)
		VALUES (?, ?, 1, 1, 0, NULL), (?, ?, 1, 1, 0, 1000), (?, ?, 1, 1, 0, 2000)`,
		user.ID, nullEp1, user.ID, oldEp1, user.ID, newEp1,
	)
	if err != nil {
		t.Fatal(err)
	}

	items, _, err := libRepo.GetNextUpEpisodes(ctx, "srv", user.ID, "", 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 next-up episodes, got %d", len(items))
	}
	if items[0].SeriesId != showNewTs || items[1].SeriesId != showOldTs || items[2].SeriesId != showNullTs {
		t.Fatalf("wrong NextUp ordering: got [%s, %s, %s], want [%s, %s, %s]",
			items[0].SeriesId, items[1].SeriesId, items[2].SeriesId,
			showNewTs, showOldTs, showNullTs)
	}
	for _, it := range items {
		if it.ExtraType != "" {
			t.Fatalf("extra item leaked into NextUp: %+v", it)
		}
	}
}

func TestFTSLimitsCountCacheCapTaxonomiesTagAndBatchMediaSources(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "misc_v0103.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	repo := NewLibraryRepo(store)

	// 1. BuildFTSQuery caps runes (256) and terms (32).
	longQuery := strings.Repeat("word ", 100)
	fts := BuildFTSQuery(longQuery)
	if strings.Count(fts, " AND ")+1 > maxFTSQueryTerms {
		t.Fatalf("expected <= %d FTS terms, got %q", maxFTSQueryTerms, fts)
	}
	manyDistinct := make([]string, 60)
	for i := range manyDistinct {
		manyDistinct[i] = fmt.Sprintf("t%02d", i)
	}
	ftsDistinct := BuildFTSQuery(strings.Join(manyDistinct, " "))
	if parts := strings.Split(ftsDistinct, " AND "); len(parts) != maxFTSQueryTerms {
		t.Fatalf("expected exactly %d capped FTS terms, got %d", maxFTSQueryTerms, len(parts))
	}

	// 2. countCache capacity cap.
	lib, _ := repo.CreateLibrary(ctx, "Movies", "movies", []string{"/m"})
	for i := 0; i < maxCountCacheEntries+10; i++ {
		_, _, err := repo.QueryItems(ctx, ItemFilter{
			LibraryID:        lib.ID,
			IncludeItemTypes: []string{fmt.Sprintf("Type%d", i)},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	repo.countMu.RLock()
	cLen := len(repo.countCache)
	repo.countMu.RUnlock()
	if cLen > maxCountCacheEntries {
		t.Fatalf("countCache exceeded cap %d: got %d", maxCountCacheEntries, cLen)
	}

	// 3. ListTaxonomiesWithPolicy returns Type="Tag" for "tag"/"tags" and ListFilterValues excludes extras.
	m1 := &ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", Name: "Main", Container: "mkv"}
	m2 := &ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", Name: "Second", Container: "mp4"}
	extraItem := &ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: m1.ID, Type: "Movie", Name: "Bonus", Container: "flv",
		Extra: ItemExtra{ExtraType: "trailer"},
	}
	for _, it := range []*ItemRecord{m1, m2, extraItem} {
		if err := repo.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	_ = repo.ReplaceItemTaxonomies(ctx, m1.ID, "tag", []string{"4K"})
	_ = repo.ReplaceMediaSourcesAndStreams(ctx, m1.ID, []MediaSourceRecord{{
		Name: "m1-src", Path: "/m/1.mkv", Container: "mkv",
		Streams: []MediaStreamRecord{{StreamIndex: 0, StreamType: "Video", Codec: "hevc"}},
	}})
	_ = repo.ReplaceMediaSourcesAndStreams(ctx, m2.ID, []MediaSourceRecord{{
		Name: "m2-src", Path: "/m/2.mp4", Container: "mp4",
		Streams: []MediaStreamRecord{{StreamIndex: 0, StreamType: "Video", Codec: "h264"}},
	}})

	for _, kind := range []string{"tag", "tags"} {
		tags, total, err := repo.ListTaxonomiesWithPolicy(ctx, "srv", kind, "", 0, 10, nil, nil, nil)
		if err != nil || total != 1 || len(tags) != 1 || tags[0].Type != "Tag" {
			t.Fatalf("ListTaxonomiesWithPolicy(%q): got %+v total=%d err=%v", kind, tags, total, err)
		}
	}

	containers, _, err := repo.ListFilterValuesWithPolicy(ctx, "srv", "Container", "", nil, 0, 20, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range containers {
		if c.Name == "flv" {
			t.Fatalf("extra item container leaked into ListFilterValuesWithPolicy: %+v", containers)
		}
	}

	// 4. Batch MediaSources hydration in QueryItems
	items, _, err := repo.QueryItems(ctx, ItemFilter{
		LibraryID: lib.ID,
		Fields:    []string{"MediaSources"},
	})
	if err != nil || len(items) != 2 {
		t.Fatalf("QueryItems with MediaSources: len=%d err=%v", len(items), err)
	}
	for _, it := range items {
		if len(it.MediaSources) != 1 || len(it.MediaStreams) != 1 {
			t.Fatalf("expected batch-loaded MediaSources & MediaStreams on %s, got %+v", it.Id, it.MediaSources)
		}
	}
}

func TestRecursiveParentIDExcludesSelfAndMarkPlayedNotFound(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "anc_v0103.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := NewLibraryRepo(store)
	lib, _ := repo.CreateLibrary(ctx, "TV", "tvshows", []string{"/tv"})
	seriesID := guid.New()
	epID := guid.New()

	if err := repo.UpsertItemsBatch(ctx, []BatchItemInput{
		{
			Item: &ItemRecord{ID: seriesID, LibraryID: lib.ID, ParentID: lib.ID, Type: "Series", IsFolder: true, Name: "Dark"},
		},
		{
			Item:        &ItemRecord{ID: epID, LibraryID: lib.ID, ParentID: seriesID, SeriesID: seriesID, Type: "Episode", Name: "Secrets"},
			AncestorIDs: []string{lib.ID},
		},
	}); err != nil {
		t.Fatal(err)
	}

	// Recursive query with ParentID=seriesID must return ONLY epID (NOT seriesID itself!).
	underSeries, total, err := repo.QueryItems(ctx, ItemFilter{ParentID: seriesID, Recursive: true})
	if err != nil || total != 1 || len(underSeries) != 1 || underSeries[0].Id != epID {
		t.Fatalf("expected only epID under seriesID, got total=%d items=%+v err=%v", total, underSeries, err)
	}

	// Recursive query with ParentID=lib.ID must return BOTH seriesID and epID.
	underLib, libTotal, err := repo.QueryItems(ctx, ItemFilter{ParentID: lib.ID, Recursive: true})
	if err != nil || libTotal != 2 || len(underLib) != 2 {
		t.Fatalf("expected 2 items under lib.ID, got total=%d len=%d err=%v", libTotal, len(underLib), err)
	}

	// MarkPlayed / MarkUnplayed on missing item -> ErrNotFound
	if _, err := repo.MarkPlayed(ctx, "user-1", "nonexistent-item"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound from MarkPlayed, got %v", err)
	}
	if _, err := repo.MarkUnplayed(ctx, "user-1", "nonexistent-item"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound from MarkUnplayed, got %v", err)
	}
}

func TestParentalRatingExpansionAndEpisodeSeriesInheritance(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "parental_v0103.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	repo := NewLibraryRepo(store)

	// Verify expanded ratings
	for label, wantMin := range map[string]int{
		"R-18":      9,
		"NC-17":     10,
		"TV-MA-LSV": 9,
		"TV-14-DLV": 8,
		"US:R":      9,
		"DE:FSK 16": 9,
	} {
		got := ParseParentalRatingValue(label)
		if got < wantMin {
			t.Errorf("ParseParentalRatingValue(%q) = %d, want >= %d", label, got, wantMin)
		}
	}

	lib, _ := repo.CreateLibrary(ctx, "TV", "tvshows", []string{"/tv"})
	adultSeries := &ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Series", IsFolder: true, Name: "Adult Show", OfficialRating: "TV-MA-LSV"}
	adultEp := &ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: adultSeries.ID, SeriesID: adultSeries.ID, Type: "Episode", Name: "Unrated Ep Of Adult Show"}
	kidsEp := &ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Episode", Name: "Kids Ep", OfficialRating: "TV-Y"}
	for _, it := range []*ItemRecord{adultSeries, adultEp, kidsEp} {
		if err := repo.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}

	maxPG := 5
	items, total, err := repo.QueryItems(ctx, ItemFilter{
		LibraryID:         lib.ID,
		IncludeItemTypes:  []string{"Episode"},
		MaxParentalRating: &maxPG,
	})
	if err != nil || total != 1 || len(items) != 1 || items[0].Id != kidsEp.ID {
		t.Fatalf("episode under TV-MA series must be filtered out; got total=%d items=%+v err=%v", total, items, err)
	}

	blocked, err := repo.IsItemBlockedByParentalPolicy(ctx, adultEp, &maxPG, nil)
	if err != nil || !blocked {
		t.Fatalf("expected IsItemBlockedByParentalPolicy=true for episode of TV-MA series, got blocked=%v err=%v", blocked, err)
	}
}

func TestContainerSimilarInstantMixPolicyAndDeleteItemByPath(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "col_v0103.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	repo := NewLibraryRepo(store)

	libA, _ := repo.CreateLibrary(ctx, "LibA", "movies", []string{"/a"})
	libB, _ := repo.CreateLibrary(ctx, "LibB", "movies", []string{"/b"})

	mA := &ItemRecord{ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID, Type: "Movie", MediaType: "Video", Name: "Kid Movie", OfficialRating: "G"}
	mB := &ItemRecord{ID: guid.New(), LibraryID: libB.ID, ParentID: libB.ID, Type: "Movie", MediaType: "Video", Name: "Secret Movie", OfficialRating: "G"}
	mR := &ItemRecord{ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID, Type: "Movie", MediaType: "Video", Name: "Adult Movie", OfficialRating: "R-18"}
	for _, it := range []*ItemRecord{mA, mB, mR} {
		if err := repo.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
		_ = repo.ReplaceItemTaxonomies(ctx, it.ID, "genre", []string{"Action"})
	}

	col, err := repo.CreateCollection(ctx, "Mixed BoxSet", "", []string{mA.ID, mB.ID, mR.ID})
	if err != nil {
		t.Fatal(err)
	}
	maxPG := 5
	got, total, err := repo.GetContainerItemsWithPolicy(ctx, "srv", "", col.ID, 0, 10, nil, []string{libA.ID}, &maxPG, nil)
	if err != nil || total != 1 || len(got) != 1 || got[0].Id != mA.ID {
		t.Fatalf("GetContainerItemsWithPolicy failed: total=%d got=%+v err=%v", total, got, err)
	}

	// Similar & InstantMix with restricted seed (mB in disallowed libB, or mR exceeding rating) -> empty!
	for _, restrictedSeed := range []string{mB.ID, mR.ID} {
		sim, sTot, err := repo.GetSimilarItemsWithPolicy(ctx, "srv", "", restrictedSeed, 10, nil, []string{libA.ID}, &maxPG, nil)
		if err != nil || sTot != 0 || len(sim) != 0 {
			t.Fatalf("GetSimilarItemsWithPolicy with restricted seed %s leaked %d items (err=%v)", restrictedSeed, len(sim), err)
		}
		mix, mTot, err := repo.GetInstantMixWithPolicy(ctx, "srv", "", restrictedSeed, 10, nil, []string{libA.ID}, &maxPG, nil)
		if err != nil || mTot != 0 || len(mix) != 0 {
			t.Fatalf("GetInstantMixWithPolicy with restricted seed %s leaked %d items (err=%v)", restrictedSeed, len(mix), err)
		}
	}

	// DeleteItemByPath promotes remaining source's is_strm, etag, runtime_ticks atomically.
	rt1 := int64(1000)
	mv := &ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID, Type: "Movie", MediaType: "Video",
		Name: "MultiSrc", Path: "/a/v1.mkv", Container: "mkv", IsStrm: false, RuntimeTicks: &rt1, Etag: "old-etag",
	}
	_ = repo.UpsertItem(ctx, mv)
	_ = repo.UpsertMediaSourceAndStreams(ctx, mv.ID, MediaSourceRecord{Name: "v1", Path: "/a/v1.mkv", Container: "mkv", Size: 100, RuntimeTicks: 1000})
	_ = repo.UpsertMediaSourceAndStreams(ctx, mv.ID, MediaSourceRecord{Name: "v2", Path: "/a/v2.strm", Container: "strm", Size: 50, IsRemote: true, RuntimeTicks: 2500})

	removed, err := repo.DeleteItemByPath(ctx, libA.ID, "/a/v1.mkv")
	if err != nil || !removed {
		t.Fatalf("DeleteItemByPath: removed=%v err=%v", removed, err)
	}
	updated, err := repo.GetItemRecordByID(ctx, mv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Path != "/a/v2.strm" || !updated.IsStrm || updated.Etag == "old-etag" || updated.RuntimeTicks == nil || *updated.RuntimeTicks != 2500 {
		t.Fatalf("DeleteItemByPath did not promote is_strm/etag/runtime_ticks: %+v", updated)
	}
}

func TestMergeSplitPreservationAndMultiVersionEscaping(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "mv_v0103.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	userRepo := NewUserRepo(store)
	repo := NewLibraryRepo(store)
	user := &UserRecord{ID: guid.New(), Name: "alice", Policy: DefaultUserPolicy(false), Config: DefaultUserConfig()}
	_ = userRepo.CreateUser(ctx, user)
	lib, _ := repo.CreateLibrary(ctx, "Movies", "movies", []string{"/movies"})

	// Folder & mismatched type rejection
	folder := &ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Series", IsFolder: true, Name: "Folder"}
	ep := &ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: folder.ID, Type: "Episode", Name: "Ep"}
	m1 := &ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", MediaType: "Video", Name: "Matrix", Path: "/movies/100%_raw/m1.mkv", Container: "mkv"}
	m2 := &ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", MediaType: "Video", Name: "Matrix", Path: "/movies/100%_raw/m2.mp4", Container: "mp4"}
	for _, it := range []*ItemRecord{folder, ep, m1, m2} {
		_ = repo.UpsertItem(ctx, it)
	}
	if _, _, err := repo.MergeItemVersions(ctx, []string{folder.ID, m1.ID}); err == nil {
		t.Fatal("expected error merging folder item")
	}
	if _, _, err := repo.MergeItemVersions(ctx, []string{m1.ID, ep.ID}); err == nil {
		t.Fatal("expected error merging different item types")
	}

	// Setup media sources, images, taxonomies, people, user_item_data, and collection on m1 & m2.
	_ = repo.UpsertMediaSourceAndStreams(ctx, m1.ID, MediaSourceRecord{Name: "4K", Path: m1.Path, Container: "mkv", Size: 4000, RuntimeTicks: 8000})
	_ = repo.UpsertMediaSourceAndStreams(ctx, m2.ID, MediaSourceRecord{Name: "1080p", Path: m2.Path, Container: "mp4", Size: 2000, RuntimeTicks: 8000})
	_ = repo.ReplaceItemImages(ctx, m1.ID, []ImageRecord{{ItemID: m1.ID, ImageType: "Primary", Path: "/img/poster.jpg", Tag: "ptag"}})
	_ = repo.ReplaceItemTaxonomies(ctx, m1.ID, "genre", []string{"Sci-Fi"})
	_ = repo.ReplaceItemPeople(ctx, m1.ID, []PersonInput{{Name: "Keanu Reeves", Type: "Actor"}})

	// Put user_item_data and collection on secondary item m2!
	_, _ = repo.MarkPlayed(ctx, user.ID, m2.ID)
	_, _ = repo.MarkFavorite(ctx, user.ID, m2.ID)
	col, _ := repo.CreateCollection(ctx, "Favorites", "", []string{m2.ID})

	// Merge m2 into m1 -> user_item_data and collection must be preserved on m1!
	if _, cnt, err := repo.MergeItemVersions(ctx, []string{m1.ID, m2.ID}); err != nil || cnt != 1 {
		t.Fatalf("MergeItemVersions: cnt=%d err=%v", cnt, err)
	}
	ud, err := repo.GetUserItemData(ctx, user.ID, m1.ID)
	if err != nil || !ud.Played || !ud.IsFavorite {
		t.Fatalf("expected m2 user_item_data preserved on m1, got %+v err=%v", ud, err)
	}
	colItems, colTotal, err := repo.GetContainerItems(ctx, "srv", user.ID, col.ID, 0, 10, nil)
	if err != nil || colTotal != 1 || colItems[0].Id != m1.ID {
		t.Fatalf("expected collection membership preserved on m1, got total=%d items=%+v err=%v", colTotal, colItems, err)
	}

	// Corrupt primary path before split to verify SplitItemVersions updates primary path to sources[0].Path.
	_, _ = store.Writer.ExecContext(ctx, `UPDATE items SET path = '/stale/path.mkv' WHERE id = ?`, m1.ID)

	created, err := repo.SplitItemVersions(ctx, m1.ID)
	if err != nil || len(created) != 1 {
		t.Fatalf("SplitItemVersions: created=%v err=%v", created, err)
	}
	primaryAfter, _ := repo.GetItemRecordByID(ctx, m1.ID)
	if primaryAfter.Path != m1.Path {
		t.Fatalf("expected primary path reset to sources[0].Path (%q), got %q", m1.Path, primaryAfter.Path)
	}
	splitID := created[0]
	if img, err := repo.GetImageByItemAndType(ctx, splitID, "Primary", 0); err != nil || img.Tag != "ptag" {
		t.Fatalf("expected item_images copied to split item, got %+v err=%v", img, err)
	}
	genres, _, _, err := repo.GetTaxonomiesByItemID(ctx, splitID)
	if err != nil || len(genres) != 1 || genres[0] != "Sci-Fi" {
		t.Fatalf("expected item_taxonomies copied to split item, got %v err=%v", genres, err)
	}
	people, err := repo.GetPeopleByItemID(ctx, splitID)
	if err != nil || len(people) != 1 || people[0].Name != "Keanu Reeves" {
		t.Fatalf("expected item_people copied to split item, got %+v err=%v", people, err)
	}

	// Verify UTF-8 providerKeyVariants, json_extract escaping, and LIKE escaping with %/_ in dir.
	if v := providerKeyVariants("豆瓣.id"); len(v) == 0 {
		t.Fatal("expected non-empty variants for UTF-8 provider key")
	}
	if v := providerKeyVariants(""); len(v) != 0 {
		t.Fatalf("expected nil/empty for blank key, got %v", v)
	}
	mSpecial := &ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", Name: "Special",
		Path:  "/movies/100%_raw/special.mkv",
		Extra: ItemExtra{ProviderIDs: map[string]string{`my."weird".key`: "val-99"}},
	}
	_ = repo.UpsertItem(ctx, mSpecial)
	if found, err := repo.FindItemByProviderID(ctx, lib.ID, "Movie", `my."weird".key`, "val-99"); err != nil || found.ID != mSpecial.ID {
		t.Fatalf("FindItemByProviderID with special key failed: found=%+v err=%v", found, err)
	}
	if found, err := repo.FindMovieInDirByBaseTitle(ctx, lib.ID, "/movies/100%_raw", "special"); err != nil || found.ID != mSpecial.ID {
		t.Fatalf("FindMovieInDirByBaseTitle with %%/_ in dir failed: found=%+v err=%v", found, err)
	}
	// Ensure wildcard dir does NOT falsely match "/movies/100%_raw"
	if _, err := repo.FindMovieInDirByBaseTitle(ctx, lib.ID, "/movies/100X_raw", "special"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for non-matching literal dir, got %v", err)
	}

	// Ensure time import is used cleanly
	_ = time.Second
}
