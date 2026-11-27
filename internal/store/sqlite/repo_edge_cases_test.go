package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/pkg/guid"
)

func floatPtr(v float64) *float64 { return &v }

func TestDBHelpersCustomFuncsAndMigrationsV3V4Coverage(t *testing.T) {
	ctx := context.Background()

	// envIntWithDefault: invalid int & below minVal
	t.Setenv("AMBRA_TEST_ENV_INT_BAD", "notanumber")
	if got := envIntWithDefault("AMBRA_TEST_ENV_INT_BAD", 7, 2); got != 7 {
		t.Fatalf("expected default 7 for bad int, got %d", got)
	}
	t.Setenv("AMBRA_TEST_ENV_INT_LOW", "-3")
	if got := envIntWithDefault("AMBRA_TEST_ENV_INT_LOW", 7, 2); got != 2 {
		t.Fatalf("expected minVal 2 for low int, got %d", got)
	}

	// Open with AMBRA_SQLITE_READER_CONNS=1 triggers maxReaders < 4 clamp
	t.Setenv("AMBRA_SQLITE_READER_CONNS", "1")
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "special#name?q=1%20.db")
	st, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Exercise registered custom SQLite functions with NULL, []byte (BLOB), and integer args
	var skNull, skBlob, skInt string
	if err := st.Reader.QueryRowContext(ctx, `SELECT pinyin_sort_key(NULL), pinyin_sort_key(CAST('电影' AS BLOB)), pinyin_sort_key(123)`).Scan(&skNull, &skBlob, &skInt); err != nil {
		t.Fatalf("custom pinyin sort key func: %v", err)
	}
	if skNull != "" || skBlob == "" || skInt == "" {
		t.Fatalf("unexpected custom pinyin results: %q %q %q", skNull, skBlob, skInt)
	}
	var prNull, prBlob, prInt int
	if err := st.Reader.QueryRowContext(ctx, `SELECT parental_rating_rank(NULL), parental_rating_rank(CAST('PG-13' AS BLOB)), parental_rating_rank(18)`).Scan(&prNull, &prBlob, &prInt); err != nil {
		t.Fatalf("custom parental rating func: %v", err)
	}
	if prNull != 0 || prBlob == 0 {
		t.Fatalf("unexpected custom parental rating results: %d %d %d", prNull, prBlob, prInt)
	}
	_ = st.Close()

	// Test v3 + v4 migration on a legacy v2 database (built from 001_init.sql without v3 columns)
	legacyPath := filepath.Join(dir, "legacy_v2.db")
	rawDB, err := sql.Open("sqlite", "file:"+escapeSQLiteURIPath(legacyPath))
	if err != nil {
		t.Fatal(err)
	}
	initSQL, err := migrationsFS.ReadFile("migrations/001_init.sql")
	if err != nil {
		rawDB.Close()
		t.Fatal(err)
	}
	if _, err := rawDB.ExecContext(ctx, string(initSQL)); err != nil {
		rawDB.Close()
		t.Fatalf("exec 001_init.sql: %v", err)
	}
	_, err = rawDB.ExecContext(ctx, `
		INSERT OR IGNORE INTO schema_migrations (version, applied_at) VALUES (1, 1), (2, 2);
		INSERT INTO items (id, library_id, type, name, sort_name, official_rating, etag, extra_json, date_created, date_modified)
		VALUES ('m1', 'lib1', 'Movie', '测试电影', '测试电影', 'PG-13', 'e1', '{"extra_type":"trailer"}', 1, 1);
	`)
	if err != nil {
		rawDB.Close()
		t.Fatalf("seed legacy v2 db: %v", err)
	}
	_ = rawDB.Close()

	migrated, err := Open(legacyPath)
	if err != nil {
		t.Fatalf("Open legacy v2 db for v3+v4 migration: %v", err)
	}
	defer migrated.Close()
	var sk, et string
	var prv int
	if err := migrated.Reader.QueryRowContext(ctx, `SELECT sort_key, extra_type, parental_rating_value FROM items WHERE id = 'm1'`).Scan(&sk, &et, &prv); err != nil {
		t.Fatalf("verify v3 backfill: %v", err)
	}
	if sk == "" || et != "trailer" || prv <= 0 {
		t.Fatalf("unexpected v3 backfilled values: sort_key=%q extra_type=%q prv=%d", sk, et, prv)
	}

	// Open on an impossible path (parent is a regular file) returns error
	blocker := filepath.Join(dir, "blocker")
	rawFile, _ := sql.Open("sqlite", "file:"+escapeSQLiteURIPath(blocker))
	if rawFile != nil {
		_, _ = rawFile.Exec(`CREATE TABLE t(x INT)`)
		_ = rawFile.Close()
	}
	if _, err := Open(filepath.Join(blocker, "cannot-open.db")); err == nil {
		t.Fatalf("expected Open to fail when parent path is a file")
	}
}

func TestUserRepoTokensAndNullSlicesAndPinyinCoverage(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "user_cov.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ur := NewUserRepo(st)
	u := &UserRecord{ID: guid.New(), Name: "alice", Policy: DefaultUserPolicy(false), Config: DefaultUserConfig()}
	if err := ur.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}

	// DeleteTokensByUserExcept: keep one token, delete others; empty keepToken deletes all.
	for _, tok := range []string{"tok-1", "tok-2", "tok-3"} {
		if err := ur.CreateAccessToken(ctx, &AccessTokenRecord{
			Token: tok, UserID: u.ID, DeviceID: "dev-" + tok, DeviceName: "Dev", AppName: "App", AppVersion: "1",
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := ur.DeleteTokensByUserExcept(ctx, u.ID, "tok-2"); err != nil {
		t.Fatalf("DeleteTokensByUserExcept(tok-2): %v", err)
	}
	if _, err := ur.GetAccessToken(ctx, "tok-1"); err == nil {
		t.Fatalf("expected tok-1 deleted")
	}
	if _, err := ur.GetAccessToken(ctx, "tok-2"); err != nil {
		t.Fatalf("expected tok-2 kept: %v", err)
	}
	if err := ur.DeleteTokensByUserExcept(ctx, u.ID, "  "); err != nil {
		t.Fatalf("DeleteTokensByUserExcept(empty): %v", err)
	}
	if _, err := ur.GetAccessToken(ctx, "tok-2"); err == nil {
		t.Fatalf("expected tok-2 deleted on empty keepToken")
	}

	// scanUser initializes nil slices when policy_json & config_json are "{}"
	_, err = st.Writer.ExecContext(ctx, `UPDATE users SET policy_json = '{}', config_json = '{}' WHERE id = ?`, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := ur.GetUserByID(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Policy.EnabledFolders == nil || loaded.Policy.BlockedTags == nil ||
		loaded.Config.OrderedViews == nil || loaded.Config.LatestItemsExcludes == nil || loaded.Config.MyMediaExcludes == nil {
		t.Fatalf("expected non-nil slices from scanUser: policy=%+v cfg=%+v", loaded.Policy, loaded.Config)
	}

	// PinyinSortKey empty & whitespace-only
	if PinyinSortKey("") != "" || PinyinSortKey("   ") != "" {
		t.Fatalf("expected empty PinyinSortKey for blank strings")
	}
}

func TestLibraryRepoQueryFiltersBatchExtrasAndTaxonomiesCoverage(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "lib_cov.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	repo := NewLibraryRepo(st)
	ur := NewUserRepo(st)
	user := &UserRecord{ID: guid.New(), Name: "viewer", Policy: DefaultUserPolicy(false), Config: DefaultUserConfig()}
	_ = ur.CreateUser(ctx, user)

	// ParseLibraryOptions, SaveLocalMetadataEnabled, UpdateLibraryOptions, ListLibraries/GetLibrary null paths
	opts := ParseLibraryOptions(`{}`)
	if opts["SaveLocalMetadata"] != true {
		t.Fatalf("expected default SaveLocalMetadata=true")
	}
	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{"/movies"})
	if err != nil {
		t.Fatal(err)
	}
	lib.OptionsJSON = `{}`
	if !lib.SaveLocalMetadataEnabled() {
		t.Fatalf("expected SaveLocalMetadataEnabled true by default")
	}
	if _, err := repo.UpdateLibraryOptions(ctx, lib.ID, map[string]any{"Custom": 1}); err != nil {
		t.Fatalf("UpdateLibraryOptions: %v", err)
	}
	_, _ = st.Writer.ExecContext(ctx, `UPDATE libraries SET paths_json = 'null' WHERE id = ?`, lib.ID)
	if gotLib, err := repo.GetLibrary(ctx, lib.ID); err != nil || gotLib.Paths == nil {
		t.Fatalf("GetLibrary null paths: %+v err=%v", gotLib, err)
	}
	if libs, err := repo.ListLibraries(ctx); err != nil || len(libs) != 1 || libs[0].Paths == nil {
		t.Fatalf("ListLibraries null paths: %+v err=%v", libs, err)
	}

	// ParentalRatingScore variants & negative maxVal clamping & firstNonEmpty
	for _, tc := range []struct {
		in   string
		want int
		ok   bool
	}{
		{"TV--PG-DLV", 5, true},
		{"TV-Y7-FV", 4, true},
		{"TV-G-1", 1, true},
		{"TV-Y-1", 1, true},
		{"UNKNOWN-RATING", 0, false},
	} {
		score, ok := ParentalRatingScore(tc.in)
		if score != tc.want || ok != tc.ok {
			t.Fatalf("ParentalRatingScore(%q) = (%d,%v), want (%d,%v)", tc.in, score, ok, tc.want, tc.ok)
		}
	}
	if firstNonEmpty("  ", "b") != "b" {
		t.Fatalf("firstNonEmpty failed")
	}
	clauses, _ := appendParentalFilterSQL("i", intPtr(-5), nil)
	if len(clauses) == 0 {
		t.Fatalf("expected parental clauses for negative maxVal")
	}

	// Create rich dataset: BoxSet, Series -> Season (no SeriesID) -> Episodes, 4K Movie with subtitles/extras/taxonomies/people, SD Movie without subtitles
	boxSet := &ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "BoxSet", IsFolder: true, Name: "Trilogy"}
	_ = repo.UpsertItem(ctx, boxSet)

	folderID := guid.New()
	_ = repo.UpsertItem(ctx, &ItemRecord{ID: folderID, LibraryID: lib.ID, ParentID: lib.ID, Type: "Folder", IsFolder: true, Name: "SubFolder"})

	m4k := &ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: folderID,
		Type: "Movie", MediaType: "Video", Name: "Alpha 4K", SortName: "alpha 4k",
		OfficialRating: "PG-13", CommunityRating: floatPtr(8.5), Path: "/movies/alpha.mkv",
	}
	mSD := &ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: folderID,
		Type: "Movie", MediaType: "Video", Name: "Beta SD", SortName: "beta sd",
		OfficialRating: "G", CommunityRating: floatPtr(5.0), Path: "/movies/beta.mkv",
	}
	_ = repo.UpsertItem(ctx, m4k)
	_ = repo.UpsertItem(ctx, mSD)
	_ = repo.ReplaceItemAncestors(ctx, m4k.ID, []AncestorLink{{AncestorID: lib.ID, Depth: 2}, {AncestorID: folderID, Depth: 1}})
	_ = repo.ReplaceItemAncestors(ctx, mSD.ID, []AncestorLink{{AncestorID: lib.ID, Depth: 2}, {AncestorID: folderID, Depth: 1}})

	// Blocked by negative maxRating
	if blocked, err := repo.IsItemBlockedByParentalPolicy(ctx, m4k, intPtr(-1), nil); err != nil || !blocked {
		t.Fatalf("expected m4k blocked by negative maxRating, got blocked=%v err=%v", blocked, err)
	}

	// Media sources & streams: 4K + subtitle on m4k; SD without subtitle on mSD
	_ = repo.ReplaceMediaSourcesAndStreams(ctx, m4k.ID, []MediaSourceRecord{{
		ID: guid.New(), ItemID: m4k.ID, Path: m4k.Path, Container: "mkv",
		Streams: []MediaStreamRecord{
			{StreamIndex: 0, StreamType: "Video", Codec: "hevc", Width: 3840, Height: 2160},
			{StreamIndex: 1, StreamType: "Subtitle", Codec: "srt"},
		},
	}})
	_ = repo.ReplaceMediaSourcesAndStreams(ctx, mSD.ID, []MediaSourceRecord{{
		ID: guid.New(), ItemID: mSD.ID, Path: mSD.Path, Container: "mkv",
		Streams: []MediaStreamRecord{
			{StreamIndex: 0, StreamType: "Video", Codec: "h264", Width: 640, Height: 480},
		},
	}})
	// Empty media sources for boxSet
	if srcs, err := repo.GetMediaSourcesByItemID(ctx, boxSet.ID); err != nil || len(srcs) != 0 {
		t.Fatalf("expected 0 media sources for boxSet, got %d err=%v", len(srcs), err)
	}

	// Taxonomies & People
	_ = repo.ReplaceItemTaxonomies(ctx, m4k.ID, "genre", []string{"Sci-Fi"})
	_ = repo.ReplaceItemTaxonomies(ctx, m4k.ID, "studio", []string{"A24"})
	_ = repo.ReplaceItemTaxonomies(ctx, m4k.ID, "tag", []string{"classic", "adult"})
	_ = repo.ReplaceItemPeople(ctx, m4k.ID, []PersonInput{{Name: "Keanu Reeves", Type: "Actor", Role: "Neo"}})
	_ = repo.ReplaceItemTaxonomies(ctx, mSD.ID, "genre", []string{"Family"})
	_ = repo.ReplaceItemTaxonomies(ctx, mSD.ID, "tag", []string{"kids"})

	// Extras (trailer, themevideo, behindthescenes) under m4k
	for _, et := range []string{"trailer", "themevideo", "behindthescenes"} {
		_ = repo.UpsertItem(ctx, &ItemRecord{
			ID: guid.New(), LibraryID: lib.ID, ParentID: m4k.ID,
			Type: "Video", MediaType: "Video", Name: "Extra " + et, Path: "/movies/" + et + ".mp4",
			Extra: ItemExtra{ExtraType: et},
		})
	}

	// UpsertImage nil & auto-generated tag
	if err := repo.UpsertImage(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertImage(ctx, &ImageRecord{ItemID: m4k.ID, ImageType: "Primary", ImageIndex: 0, Path: "/p.jpg", Tag: ""}); err != nil {
		t.Fatal(err)
	}

	// UpsertItemsBatch edge cases: empty batch, nil entry, existing item unchanged FTS, existing item changed FTS, missing FTS row, legacy rowid collision
	if err := repo.UpsertItemsBatch(ctx, nil); err != nil {
		t.Fatal(err)
	}
	batchItem := &ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", Name: "BatchOne", Path: "/movies/b1.mkv"}
	if err := repo.UpsertItemsBatch(ctx, []BatchItemInput{
		{Item: nil},
		{Item: batchItem, AncestorIDs: []string{"", lib.ID}, FTSTitleRaw: "BatchOne", FTSTitleTokens: "batchone", FTSPinyinFull: "batchone", FTSPinyinInit: "b"},
	}); err != nil {
		t.Fatalf("UpsertItemsBatch create: %v", err)
	}
	// Second batch upsert with unchanged FTS -> hits continue
	if err := repo.UpsertItemsBatch(ctx, []BatchItemInput{
		{Item: batchItem, AncestorIDs: []string{lib.ID}, FTSTitleRaw: "BatchOne", FTSTitleTokens: "batchone", FTSPinyinFull: "batchone", FTSPinyinInit: "b"},
	}); err != nil {
		t.Fatalf("UpsertItemsBatch unchanged FTS: %v", err)
	}
	// Third batch upsert with changed FTS -> hits delFTSByRowIDStmt + insFTSWithRowIDStmt
	batchItem.Name = "BatchOneRenamed"
	if err := repo.UpsertItemsBatch(ctx, []BatchItemInput{
		{Item: batchItem, AncestorIDs: []string{lib.ID}, FTSTitleRaw: "BatchOneRenamed", FTSTitleTokens: "batchonerenamed", FTSPinyinFull: "batchonerenamed", FTSPinyinInit: "b"},
	}); err != nil {
		t.Fatalf("UpsertItemsBatch changed FTS: %v", err)
	}
	// Delete FTS by rowid and re-insert with auto rowid so rowid lookup misses -> hits existedBefore selFTSByItemStmt branch (both unchanged & changed)
	_, _ = st.Writer.ExecContext(ctx, `DELETE FROM item_search_fts WHERE item_id = ?`, batchItem.ID)
	_, _ = st.Writer.ExecContext(ctx, `INSERT INTO item_search_fts (rowid, item_id, title_raw, title_tokens, pinyin_full, pinyin_initials, people_names) VALUES (999991, ?, 'BatchOneRenamed', 'batchonerenamed', 'batchonerenamed', 'b', '')`, batchItem.ID)
	if err := repo.UpsertItemsBatch(ctx, []BatchItemInput{
		{Item: batchItem, FTSTitleRaw: "BatchOneRenamed", FTSTitleTokens: "batchonerenamed", FTSPinyinFull: "batchonerenamed", FTSPinyinInit: "b"},
	}); err != nil {
		t.Fatalf("UpsertItemsBatch existedBefore unchanged FTS: %v", err)
	}
	if err := repo.UpsertItemsBatch(ctx, []BatchItemInput{
		{Item: batchItem, FTSTitleRaw: "BatchOneV3", FTSTitleTokens: "batchonev3", FTSPinyinFull: "batchonev3", FTSPinyinInit: "b"},
	}); err != nil {
		t.Fatalf("UpsertItemsBatch existedBefore changed FTS: %v", err)
	}
	// Legacy rowid collision: place another item_id at batchItem's rowid
	var batchRowID int64
	_ = st.Reader.QueryRowContext(ctx, `SELECT rowid FROM items WHERE id = ?`, batchItem.ID).Scan(&batchRowID)
	_, _ = st.Writer.ExecContext(ctx, `DELETE FROM item_search_fts WHERE rowid = ?`, batchRowID)
	_, _ = st.Writer.ExecContext(ctx, `INSERT INTO item_search_fts (rowid, item_id, title_raw, title_tokens, pinyin_full, pinyin_initials, people_names) VALUES (?, 'other-item', 'Other', 'other', 'other', 'o', '')`, batchRowID)
	if err := repo.UpsertItemsBatch(ctx, []BatchItemInput{
		{Item: batchItem, FTSTitleRaw: "BatchOneV4", FTSTitleTokens: "batchonev4", FTSPinyinFull: "batchonev4", FTSPinyinInit: "b"},
	}); err != nil {
		t.Fatalf("UpsertItemsBatch collision FTS: %v", err)
	}
	// Collision unchanged FTS
	if err := repo.UpsertItemsBatch(ctx, []BatchItemInput{
		{Item: batchItem, FTSTitleRaw: "BatchOneV4", FTSTitleTokens: "batchonev4", FTSPinyinFull: "batchonev4", FTSPinyinInit: "b"},
	}); err != nil {
		t.Fatalf("UpsertItemsBatch collision unchanged FTS: %v", err)
	}
	// UpsertSearchFTS unchanged early return & collision fallback
	if err := repo.UpsertSearchFTS(ctx, batchItem.ID, "BatchOneV4", "batchonev4", "batchonev4", "b", ""); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertSearchFTS(ctx, batchItem.ID, "BatchOneV5", "batchonev5", "batchonev5", "b", ""); err != nil {
		t.Fatal(err)
	}

	// Fetch real genre & studio IDs via ListTaxonomiesWithPolicy (also tests allowedLibraryIDs & parental filters!)
	if empty, cnt, err := repo.ListTaxonomiesWithPolicy(ctx, "srv", "genre", "", 0, 10, []string{}, nil, nil); err != nil || cnt != 0 || len(empty) != 0 {
		t.Fatalf("expected empty taxonomies for empty allowedLibraryIDs")
	}
	genres, _, err := repo.ListTaxonomiesWithPolicy(ctx, "srv", "genre", "", 0, 10, []string{lib.ID}, intPtr(10), []string{"blocked"})
	if err != nil || len(genres) == 0 {
		t.Fatalf("ListTaxonomiesWithPolicy genre: %v err=%v", genres, err)
	}
	studios, _, err := repo.ListTaxonomiesWithPolicy(ctx, "srv", "studio", "", 0, 10, []string{lib.ID}, intPtr(10), nil)
	if err != nil || len(studios) == 0 {
		t.Fatalf("ListTaxonomiesWithPolicy studio: %v err=%v", studios, err)
	}
	_, _, _ = repo.ListTaxonomiesWithPolicy(ctx, "srv", "person", "", 0, 10, []string{lib.ID}, intPtr(10), []string{"blocked"})
	_, _, _ = repo.ListTaxonomiesWithPolicy(ctx, "srv", "person", "", 0, 10, []string{guid.New()}, intPtr(1), nil) // empty result slice
	_, _, _ = repo.ListTaxonomiesWithPolicy(ctx, "srv", "genre", "", 0, 10, []string{guid.New()}, intPtr(1), nil)  // empty result slice
	_, _, _ = repo.ListTaxonomiesWithPolicy(ctx, "srv", "artist", "", 0, 10, []string{lib.ID}, intPtr(10), []string{"blocked"})

	// QueryItems with all rich filter branches
	for _, f := range []ItemFilter{
		{IDs: []string{m4k.ID, "", mSD.ID}, IncludeItemTypes: []string{"Movie"}, ExcludeItemTypes: []string{"BoxSet"}},
		{OfficialRatings: []string{"PG-13", " "}, MinCommunityRating: floatPtr(7.0), GenreIDs: []string{genres[0].Id, ""}, StudioIDs: []string{studios[0].Id, ""}, Persons: []string{"Keanu Reeves"}},
		{IsHD: boolPtr(true), Is4K: boolPtr(true), HasSubtitles: boolPtr(true)},
		{IsHD: boolPtr(false), Is4K: boolPtr(false), HasSubtitles: boolPtr(false)},
		{NameStartsWith: "###"},
		{NameStartsWith: "\xff\xff"},
		{NameStartsWithOrGreater: "###", NameLessThan: "###"},
	} {
		if _, _, err := repo.QueryItems(ctx, f); err != nil {
			t.Fatalf("QueryItems(%+v): %v", f, err)
		}
	}
	// Reset countCache to nil so lazy init branch triggers
	repo.countMu.Lock()
	repo.countCache = nil
	repo.countMu.Unlock()
	if _, _, err := repo.QueryItems(ctx, ItemFilter{IncludeItemTypes: []string{"Movie"}, ExcludeItemTypes: []string{"Episode"}}); err != nil {
		t.Fatal(err)
	}

	// GetLatestItemsWithPolicy with non-library folder ParentID
	if latest, err := repo.GetLatestItemsWithPolicy(ctx, "srv", user.ID, folderID, []string{"Movie"}, 10, nil, []string{lib.ID}, intPtr(10), nil); err != nil || len(latest) == 0 {
		t.Fatalf("GetLatestItemsWithPolicy folder parent: len=%d err=%v", len(latest), err)
	}

	// GetItemExtras: empty parent, with userID, and no extras
	if ex, err := repo.GetItemExtras(ctx, "srv", user.ID, "", nil); err != nil || len(ex) != 0 {
		t.Fatalf("GetItemExtras empty parent: %v err=%v", ex, err)
	}
	if ex, err := repo.GetItemExtras(ctx, "srv", user.ID, m4k.ID, []string{"trailer", "themevideo", "behindthescenes"}); err != nil || len(ex) != 3 {
		t.Fatalf("GetItemExtras m4k: len=%d err=%v", len(ex), err)
	}
	if ex, err := repo.GetItemExtras(ctx, "srv", user.ID, mSD.ID, nil); err != nil || len(ex) != 0 {
		t.Fatalf("GetItemExtras mSD: len=%d err=%v", len(ex), err)
	}

	// Series + Season (SeriesID empty -> ParentID fallback in hydrateDTOs) + Episodes -> NextUpWithPolicy with parental policy
	seriesID := guid.New()
	seasonID := guid.New()
	ep1ID := guid.New()
	ep2ID := guid.New()
	_ = repo.UpsertItem(ctx, &ItemRecord{ID: seriesID, LibraryID: lib.ID, ParentID: lib.ID, Type: "Series", IsFolder: true, Name: "Show1", OfficialRating: "TV-PG"})
	_ = repo.UpsertItem(ctx, &ItemRecord{ID: seasonID, LibraryID: lib.ID, ParentID: seriesID, SeriesID: "", Type: "Season", IsFolder: true, Name: "Season 1", IndexNumber: intPtr(1)})
	_ = repo.UpsertItem(ctx, &ItemRecord{ID: ep1ID, LibraryID: lib.ID, ParentID: seasonID, SeriesID: seriesID, SeasonID: seasonID, Type: "Episode", Name: "Ep1", ParentIndexNumber: intPtr(1), IndexNumber: intPtr(1)})
	_ = repo.UpsertItem(ctx, &ItemRecord{ID: ep2ID, LibraryID: lib.ID, ParentID: seasonID, SeriesID: seriesID, SeasonID: seasonID, Type: "Episode", Name: "Ep2", ParentIndexNumber: intPtr(1), IndexNumber: intPtr(2)})
	_, _ = repo.MarkPlayed(ctx, user.ID, ep1ID)
	if _, _, err := repo.GetNextUpEpisodesWithPolicy(ctx, "srv", user.ID, seriesID, 10, nil, []string{lib.ID}, intPtr(10), []string{"blocked"}); err != nil {
		t.Fatalf("GetNextUpEpisodesWithPolicy: %v", err)
	}
	// Hydrate Season without userID (covers SeriesID=="" -> ParentID and UserData nil init)
	if _, _, err := repo.QueryItems(ctx, ItemFilter{IDs: []string{seasonID, seriesID}}); err != nil {
		t.Fatal(err)
	}

	// SearchHints with parental filter & no match
	if hints, total, err := repo.SearchHints(ctx, SearchHintFilter{SearchTerm: "NoSuchTitleXYZ", MaxParentalRating: intPtr(10), BlockedTags: []string{"adult"}}); err != nil || total != 0 || len(hints) != 0 {
		t.Fatalf("SearchHints no match: len=%d total=%d err=%v", len(hints), total, err)
	}

	// GetItemCountsWithAllowedLibraries: empty slice, nil (cached on 2nd call), and specific libraryID
	if cnts, err := repo.GetItemCountsWithAllowedLibraries(ctx, []string{}); err != nil || cnts.MovieCount != 0 {
		t.Fatalf("expected 0 counts for empty allowedLibraryIDs")
	}
	c1, err := repo.GetItemCountsWithAllowedLibraries(ctx, nil)
	if err != nil || c1.BoxSetCount != 1 {
		t.Fatalf("GetItemCountsWithAllowedLibraries(nil) first: %+v err=%v", c1, err)
	}
	c2, err := repo.GetItemCountsWithAllowedLibraries(ctx, nil) // cache hit!
	if err != nil || c2.BoxSetCount != 1 {
		t.Fatalf("GetItemCountsWithAllowedLibraries(nil) cached: %+v err=%v", c2, err)
	}
	if c3, err := repo.GetItemCountsWithAllowedLibraries(ctx, []string{lib.ID}); err != nil || c3.BoxSetCount != 1 {
		t.Fatalf("GetItemCountsWithAllowedLibraries(lib.ID): %+v err=%v", c3, err)
	}

	// Batch loaders with empty slices & collectCascadeItemIDs with missing ID
	if m, err := repo.loadImageTagsBatch(ctx, nil); err != nil || len(m) != 0 {
		t.Fatal(err)
	}
	if m, err := repo.loadMediaSourcesBatch(ctx, nil); err != nil || len(m) != 0 {
		t.Fatal(err)
	}
	if g, s, tg, err := repo.loadTaxonomiesBatch(ctx, nil); err != nil || len(g) != 0 || len(s) != 0 || len(tg) != 0 {
		t.Fatal(err)
	}
	if p, err := repo.loadPeopleBatch(ctx, nil); err != nil || len(p) != 0 {
		t.Fatal(err)
	}
	tx, _ := st.Writer.BeginTx(ctx, nil)
	_, _ = repo.collectCascadeItemIDs(ctx, tx, guid.New())
	_ = tx.Rollback()
	_ = BuildFTSQuery("   ")
	if _, ok := prefixUpperBound("\xff\xff"); ok {
		t.Fatalf("expected prefixUpperBound false for all-0xff bytes")
	}
}

func TestCollectionsAndMultiversionEdgeCoverage(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "coll_mv_cov.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	repo := NewLibraryRepo(st)
	ur := NewUserRepo(st)
	user := &UserRecord{ID: guid.New(), Name: "u", Policy: DefaultUserPolicy(false), Config: DefaultUserConfig()}
	_ = ur.CreateUser(ctx, user)
	lib, _ := repo.CreateLibrary(ctx, "Movies", "movies", []string{"/movies"})

	// Create 4 movies with genres/tags
	mkMovie := func(name, path string, genres, tags []string) *ItemRecord {
		m := &ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", MediaType: "Video", Name: name, Path: path, Container: "mkv"}
		_ = repo.UpsertItem(ctx, m)
		_ = repo.ReplaceMediaSourcesAndStreams(ctx, m.ID, []MediaSourceRecord{{ID: guid.New(), ItemID: m.ID, Path: path, Container: "mkv"}})
		if len(genres) > 0 {
			_ = repo.ReplaceItemTaxonomies(ctx, m.ID, "genre", genres)
		}
		if len(tags) > 0 {
			_ = repo.ReplaceItemTaxonomies(ctx, m.ID, "tag", tags)
		}
		return m
	}
	m1 := mkMovie("Movie 1", "/movies/m1.mkv", []string{"Action", "Sci-Fi"}, []string{"top"})
	m2 := mkMovie("Movie 2", "/movies/m2.mkv", []string{"Action"}, []string{"top"})
	m3 := mkMovie("Movie 3", "/movies/m3.mkv", []string{"Action"}, nil)
	mNoGenre := mkMovie("NoGenre", "/movies/ng.mkv", nil, nil)
	mUniqueGenre := mkMovie("UniqueGenre", "/movies/ug.mkv", []string{"ExperimentalOnlyOne"}, nil)

	// Playlist (Video) + GetContainerItemsWithPolicy empty allowedLibraryIDs
	pl, err := repo.CreatePlaylist(ctx, "Video Mix", "Video", user.ID, []string{m1.ID, m2.ID})
	if err != nil {
		t.Fatal(err)
	}
	if items, total, err := repo.GetContainerItemsWithPolicy(ctx, "srv", user.ID, pl.ID, 0, 10, nil, []string{}, nil, nil); err != nil || total != 0 || len(items) != 0 {
		t.Fatalf("GetContainerItemsWithPolicy empty allowed libs: %d %d err=%v", len(items), total, err)
	}

	// isSeedAccessibleUnderPolicy for nil seedRec
	if ok, err := repo.isSeedAccessibleUnderPolicy(ctx, nil, []string{lib.ID}, intPtr(10), nil); err != nil || ok {
		t.Fatalf("expected nil seedRec not accessible, got ok=%v err=%v", ok, err)
	}

	// GetSimilarItemsWithPolicy: empty allowedLibraryIDs, limit=1 hit on genre & fallback, and empty result
	if items, total, err := repo.GetSimilarItemsWithPolicy(ctx, "srv", user.ID, m1.ID, 5, nil, []string{}, nil, nil); err != nil || total != 0 || len(items) != 0 {
		t.Fatal(err)
	}
	if items, _, err := repo.GetSimilarItemsWithPolicy(ctx, "srv", user.ID, m1.ID, 1, nil, []string{lib.ID}, intPtr(10), nil); err != nil || len(items) != 1 {
		t.Fatalf("GetSimilarItemsWithPolicy limit=1 genre: len=%d err=%v", len(items), err)
	}
	if items, _, err := repo.GetSimilarItemsWithPolicy(ctx, "srv", user.ID, mNoGenre.ID, 1, nil, []string{lib.ID}, intPtr(10), nil); err != nil || len(items) != 1 {
		t.Fatalf("GetSimilarItemsWithPolicy limit=1 fallback: len=%d err=%v", len(items), err)
	}

	// GetInstantMixWithPolicy: empty allowedLibraryIDs, Video Playlist seed, allowedLibraryIDs + parental filter, limit=1, and empty mix
	if items, total, err := repo.GetInstantMixWithPolicy(ctx, "srv", user.ID, pl.ID, 5, nil, []string{}, nil, nil); err != nil || total != 0 || len(items) != 0 {
		t.Fatal(err)
	}
	if items, _, err := repo.GetInstantMixWithPolicy(ctx, "srv", user.ID, pl.ID, 1, nil, []string{lib.ID}, intPtr(10), []string{"blocked"}); err != nil || len(items) != 1 {
		t.Fatalf("GetInstantMixWithPolicy video playlist limit=1: len=%d err=%v", len(items), err)
	}
	if items, _, err := repo.GetInstantMixWithPolicy(ctx, "srv", user.ID, mUniqueGenre.ID, 5, nil, []string{lib.ID}, intPtr(10), nil); err != nil || items == nil {
		t.Fatalf("GetInstantMixWithPolicy unique genre: %v err=%v", items, err)
	}

	// GetMovieRecommendationsWithPolicy: empty allowedLibraryIDs, fav seed, no-genre seed, unique-genre seed, categoryLimit=1
	if recs, err := repo.GetMovieRecommendationsWithPolicy(ctx, "srv", user.ID, 3, 5, nil, []string{}, nil, nil); err != nil || len(recs) != 0 {
		t.Fatal(err)
	}
	_, _ = repo.MarkFavorite(ctx, user.ID, m1.ID)
	_, _ = repo.MarkPlayed(ctx, user.ID, mNoGenre.ID)
	_, _ = repo.MarkPlayed(ctx, user.ID, mUniqueGenre.ID)
	_, _ = repo.MarkPlayed(ctx, user.ID, m2.ID)
	if recs, err := repo.GetMovieRecommendationsWithPolicy(ctx, "srv", user.ID, 1, 5, nil, []string{lib.ID}, intPtr(10), []string{"blocked"}); err != nil || len(recs) == 0 {
		t.Fatalf("GetMovieRecommendationsWithPolicy categoryLimit=1: len=%d err=%v", len(recs), err)
	}
	// Without user seeds -> genre fallback with categoryLimit=1
	if recs, err := repo.GetMovieRecommendationsWithPolicy(ctx, "srv", guid.New(), 1, 5, nil, []string{lib.ID}, intPtr(10), nil); err != nil || len(recs) == 0 {
		t.Fatalf("GetMovieRecommendationsWithPolicy genre fallback: len=%d err=%v", len(recs), err)
	}

	// ListFilterValuesWithPolicy: empty allowedLibraryIDs, scoped with parental policy, unscoped tag
	if vals, cnt, err := repo.ListFilterValuesWithPolicy(ctx, "srv", "Genres", "", nil, 0, 10, []string{}, nil, nil); err != nil || cnt != 0 || len(vals) != 0 {
		t.Fatal(err)
	}
	if _, _, err := repo.ListFilterValuesWithPolicy(ctx, "srv", "Tags", "", nil, 0, 10, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.ListFilterValuesWithPolicy(ctx, "srv", "Tags", "", []string{"Movie"}, 0, 10, []string{lib.ID}, intPtr(10), []string{"blocked"}); err != nil {
		t.Fatal(err)
	}

	// MergeItemVersions & SplitItemVersions edge cases
	folder := &ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Folder", IsFolder: true, Name: "Dir"}
	_ = repo.UpsertItem(ctx, folder)
	if _, _, err := repo.MergeItemVersions(ctx, []string{m1.ID, folder.ID}); err == nil {
		t.Fatalf("expected error merging folder item")
	}
	// Duplicate media source path across two items -> dedup branch in MergeItemVersions
	mDup := mkMovie("Movie 1 Dup", "/movies/m1.mkv", nil, nil)
	if _, _, err := repo.MergeItemVersions(ctx, []string{m1.ID, mDup.ID}); err != nil {
		t.Fatalf("MergeItemVersions duplicate path: %v", err)
	}
	// Merge m2 and m3 (with empty container on media source to cover container fallback in SplitItemVersions)
	_, _ = st.Writer.ExecContext(ctx, `UPDATE item_media_sources SET container = '' WHERE item_id IN (?, ?)`, m2.ID, m3.ID)
	primaryID, cnt, err := repo.MergeItemVersions(ctx, []string{m2.ID, m3.ID})
	if err != nil || cnt != 1 {
		t.Fatalf("MergeItemVersions m2+m3: primary=%s cnt=%d err=%v", primaryID, cnt, err)
	}
	splitIDs, err := repo.SplitItemVersions(ctx, primaryID)
	if err != nil || len(splitIDs) != 1 {
		t.Fatalf("SplitItemVersions: %v err=%v", splitIDs, err)
	}

	// DeleteItemByPath: non-existent path -> false, nil; multi-version single-path delete; single-version full delete
	if deleted, err := repo.DeleteItemByPath(ctx, lib.ID, "/movies/does-not-exist.mkv"); err != nil || deleted {
		t.Fatalf("DeleteItemByPath nonexistent: deleted=%v err=%v", deleted, err)
	}
	primaryID2, _, err := repo.MergeItemVersions(ctx, []string{primaryID, splitIDs[0]})
	if err != nil {
		t.Fatal(err)
	}
	if deleted, err := repo.DeleteItemByPath(ctx, lib.ID, "/movies/m3.mkv"); err != nil || !deleted {
		t.Fatalf("DeleteItemByPath multi-version: deleted=%v err=%v", deleted, err)
	}
	if deleted, err := repo.DeleteItemByPath(ctx, lib.ID, "/movies/m2.mkv"); err != nil || !deleted {
		t.Fatalf("DeleteItemByPath final version: deleted=%v err=%v", deleted, err)
	}
	if _, err := repo.GetItemRecordByID(ctx, primaryID2); err == nil {
		t.Fatalf("expected primaryID2 deleted after last version removed")
	}

	// ItemExtra.unmarshal invalid json branch
	var ex ItemExtra
	if err := ex.unmarshal("{invalid"); err == nil {
		t.Fatalf("expected error unmarshaling invalid ItemExtra")
	}
	_ = time.Now()
}
