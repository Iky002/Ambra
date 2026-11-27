package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/pkg/guid"
)

func TestAuditV0107SplitAncestorsMigrationV5ResumeNextUpCandidatesTaxonomies(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "audit_v0107.db")

	// --- M-2: Simulate pre-v5 database with case-insensitive duplicate usernames, then Open() runs v5 migration ---
	rawDB, err := sql.Open("sqlite", fmt.Sprintf("file:%s", escapeSQLiteURIPath(dbPath)))
	if err != nil {
		t.Fatal(err)
	}
	sqlBytes, err := migrationsFS.ReadFile("migrations/001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rawDB.ExecContext(ctx, string(sqlBytes)); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Unix()
	if _, err := rawDB.ExecContext(ctx, `INSERT INTO schema_migrations (version, applied_at) VALUES (1, ?), (2, ?), (4, ?)`, now, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := rawDB.ExecContext(ctx, `CREATE INDEX idx_users_name_nocase ON users(LOWER(name))`); err != nil {
		t.Fatal(err)
	}
	// Insert two users whose names differ only by case plus a pre-existing _dup_ collision candidate.
	if _, err := rawDB.ExecContext(ctx, `
		INSERT INTO users (id, name, password_hash, is_admin, is_disabled, policy_json, config_json, created_at)
		VALUES ('u1', 'Alice', '', 1, 0, '{}', '{}', 100),
		       ('u2', 'alice', '', 0, 0, '{}', '{}', 200),
		       ('u3', 'alice_dup_2', '', 0, 0, '{}', '{}', 150)`); err != nil {
		t.Fatal(err)
	}
	_ = rawDB.Close()

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open with duplicate usernames (migration v5): %v", err)
	}
	defer store.Close()

	userRepo := NewUserRepo(store)
	libRepo := NewLibraryRepo(store)

	// Verify unique index rejects case-insensitive duplicate with ErrUserExists.
	if err := userRepo.CreateUser(ctx, &UserRecord{ID: guid.New(), Name: "ALICE", Policy: DefaultUserPolicy(false), Config: DefaultUserConfig()}); !errors.Is(err, ErrUserExists) {
		t.Fatalf("expected ErrUserExists for case-insensitive duplicate, got %v", err)
	}
	if isUniqueConstraintErr(nil) {
		t.Fatal("isUniqueConstraintErr(nil) must be false")
	}

	user, err := userRepo.GetUserByID(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}

	// --- H-16: SplitItemVersions must NOT inherit (primaryID, newID, 0); deleting primary must preserve split item ---
	lib, err := libRepo.CreateLibrary(ctx, "Movies", "movies", []string{"/movies"})
	if err != nil {
		t.Fatal(err)
	}
	critic := 88.5
	m1 := &ItemRecord{
		ID:           guid.New(),
		LibraryID:    lib.ID,
		ParentID:     lib.ID,
		Type:         "Movie",
		MediaType:    "Video",
		Name:         "Split Safe Movie",
		CriticRating: &critic,
		Path:         "/movies/m1-4k.mkv",
		Container:    "mkv",
	}
	if err := libRepo.UpsertItemsBatch(ctx, []BatchItemInput{{Item: m1, FTSTitleRaw: m1.Name}}); err != nil {
		t.Fatal(err)
	}
	if err := libRepo.ReplaceMediaSourcesAndStreams(ctx, m1.ID, []MediaSourceRecord{
		{ID: guid.New(), Name: "4K", Path: "/movies/m1-4k.mkv", Container: "mkv"},
		{ID: guid.New(), Name: "1080p", Path: "/movies/m1-1080p.mkv", Container: "mkv"},
	}); err != nil {
		t.Fatal(err)
	}
	splitIDs, err := libRepo.SplitItemVersions(ctx, m1.ID)
	if err != nil || len(splitIDs) != 1 {
		t.Fatalf("SplitItemVersions: ids=%v err=%v", splitIDs, err)
	}
	splitID := splitIDs[0]

	// Delete primary item -> split item MUST survive!
	if err := libRepo.DeleteItem(ctx, m1.ID); err != nil {
		t.Fatalf("DeleteItem primary: %v", err)
	}
	splitDTO, err := libRepo.GetItemByID(ctx, "srv1", user.ID, splitID)
	if err != nil {
		t.Fatalf("split item was mistakenly cascade-deleted when deleting primary: %v", err)
	}
	// Verify CriticRating is mapped in hydrateDTOs!
	if splitDTO.CriticRating == nil || *splitDTO.CriticRating != critic {
		t.Fatalf("expected CriticRating=%v, got %v", critic, splitDTO.CriticRating)
	}

	// --- M-3: IsResumable includes rewatched (played=1, pos>0) + HideFromResume filtering + auto-unhide on new progress ---
	if _, err := libRepo.MarkPlayed(ctx, user.ID, splitID); err != nil {
		t.Fatal(err)
	}
	// Rewatch progress while played=1
	if _, err := userRepo.UpsertPlaybackProgress(ctx, user.ID, splitID, 500_000_000, false, nil, nil); err != nil {
		t.Fatal(err)
	}
	resItems, resTotal, err := libRepo.GetResumeItems(ctx, "srv1", user.ID, "", nil, 0, 10, nil)
	if err != nil || resTotal != 1 || len(resItems) != 1 || resItems[0].Id != splitID {
		t.Fatalf("expected rewatched item in resume list, got len=%d total=%d err=%v", len(resItems), resTotal, err)
	}
	// IsResumable=false excludes items with playback_position_ticks > 0
	isResFalse := false
	nonRes, _, err := libRepo.QueryItems(ctx, ItemFilter{ServerID: "srv1", UserID: user.ID, IDs: []string{splitID}, IsResumable: &isResFalse})
	if err != nil || len(nonRes) != 0 {
		t.Fatalf("expected IsResumable=false to exclude item with pos>0, got %d err=%v", len(nonRes), err)
	}

	// Hide from resume via server_config key
	hideKey := fmt.Sprintf("user_hide_resume:%s:%s", guid.Normalize(user.ID), guid.Normalize(splitID))
	if err := userRepo.SetServerConfig(ctx, hideKey, `{"Hide":true}`); err != nil {
		t.Fatal(err)
	}
	resHidden, resHiddenTotal, err := libRepo.GetResumeItems(ctx, "srv1", user.ID, "", nil, 0, 10, nil)
	if err != nil || resHiddenTotal != 0 || len(resHidden) != 0 {
		t.Fatalf("expected hidden item filtered from GetResumeItems, got len=%d total=%d err=%v", len(resHidden), resHiddenTotal, err)
	}

	// New playback progress automatically removes hide flag -> reappears in GetResumeItems!
	if _, err := userRepo.UpsertPlaybackProgress(ctx, user.ID, splitID, 600_000_000, false, nil, nil); err != nil {
		t.Fatal(err)
	}
	resUnhidden, resUnhiddenTotal, err := libRepo.GetResumeItems(ctx, "srv1", user.ID, "", nil, 0, 10, nil)
	if err != nil || resUnhiddenTotal != 1 || len(resUnhidden) != 1 {
		t.Fatalf("expected new progress to auto-unhide item in GetResumeItems, got len=%d total=%d err=%v", len(resUnhidden), resUnhiddenTotal, err)
	}

	// --- M-4: CreateCollection & CreatePlaylist Chinese FTS tokenization ---
	col, err := libRepo.CreateCollection(ctx, "漫威宇宙经典合集", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	pl, err := libRepo.CreatePlaylist(ctx, "深夜治愈纯音乐歌单", "Audio", user.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	colHits, _, err := libRepo.QueryItems(ctx, ItemFilter{ServerID: "srv1", SearchTerm: "宇宙 经典", IncludeItemTypes: []string{"BoxSet"}})
	if err != nil || len(colHits) != 1 || colHits[0].Id != col.ID {
		t.Fatalf("expected Chinese FTS search to find collection, got %+v err=%v", colHits, err)
	}
	plHits, _, err := libRepo.QueryItems(ctx, ItemFilter{ServerID: "srv1", SearchTerm: "治愈 歌单", IncludeItemTypes: []string{"Playlist"}})
	if err != nil || len(plHits) != 1 || plHits[0].Id != pl.ID {
		t.Fatalf("expected Chinese FTS search to find playlist, got %+v err=%v", plHits, err)
	}

	// --- M-6: GetNextUpEpisodesWithPolicy excludes Season 0 & returns real TotalRecordCount > limit ---
	tvLib, err := libRepo.CreateLibrary(ctx, "TV", "tvshows", []string{"/tv"})
	if err != nil {
		t.Fatal(err)
	}
	s0, s1, e1 := 0, 1, 1
	for i := 0; i < 3; i++ {
		sid := guid.New()
		_ = libRepo.UpsertItem(ctx, &ItemRecord{ID: sid, LibraryID: tvLib.ID, ParentID: tvLib.ID, Type: "Series", IsFolder: true, Name: fmt.Sprintf("Show %d", i)})
		// Season 0 special must NOT appear in NextUp
		_ = libRepo.UpsertItem(ctx, &ItemRecord{ID: guid.New(), LibraryID: tvLib.ID, ParentID: sid, SeriesID: sid, Type: "Episode", MediaType: "Video", Name: "Special 0", ParentIndexNumber: &s0, IndexNumber: &e1})
		// Season 1 Ep 1
		_ = libRepo.UpsertItem(ctx, &ItemRecord{ID: guid.New(), LibraryID: tvLib.ID, ParentID: sid, SeriesID: sid, Type: "Episode", MediaType: "Video", Name: fmt.Sprintf("Show %d S01E01", i), ParentIndexNumber: &s1, IndexNumber: &e1})
	}
	nextItems, nextTotal, err := libRepo.GetNextUpEpisodes(ctx, "srv1", user.ID, "", 1, nil)
	if err != nil {
		t.Fatalf("GetNextUpEpisodes: %v", err)
	}
	if len(nextItems) != 1 || nextTotal != 3 {
		t.Fatalf("expected len=1 and real TotalRecordCount=3, got len=%d total=%d", len(nextItems), nextTotal)
	}
	if nextItems[0].ParentIndexNumber == nil || *nextItems[0].ParentIndexNumber != 1 {
		t.Fatalf("Season 0 special leaked into NextUp: %+v", nextItems[0])
	}

	// --- M-7: ListChapterExtractionCandidates & ListBIFCandidates prefer local non-strm media source & skip empty paths ---
	hybridID := guid.New()
	if err := libRepo.UpsertItem(ctx, &ItemRecord{
		ID:        hybridID,
		LibraryID: lib.ID,
		ParentID:  lib.ID,
		Type:      "Movie",
		MediaType: "Video",
		IsStrm:    true,
		Name:      "Hybrid STRM + Local",
		Path:      "/movies/hybrid.strm",
		Extra: ItemExtra{
			Chapters: []ChapterRecord{{StartPositionTicks: 0, Name: "Intro"}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	// Insert empty path first, STRM second, local MKV third.
	if err := libRepo.ReplaceMediaSourcesAndStreams(ctx, hybridID, []MediaSourceRecord{
		{ID: guid.New(), Name: "Empty", Path: "   ", IsRemote: false},
		{ID: guid.New(), Name: "STRM", Path: "/movies/hybrid.strm", Container: "strm", IsRemote: true},
		{ID: guid.New(), Name: "Local", Path: "/movies/hybrid-local.mkv", Container: "mkv", IsRemote: false},
	}); err != nil {
		t.Fatal(err)
	}
	// Item with only empty path should be skipped cleanly.
	emptyPathID := guid.New()
	if err := libRepo.UpsertItem(ctx, &ItemRecord{
		ID:        emptyPathID,
		LibraryID: lib.ID,
		ParentID:  lib.ID,
		Type:      "Movie",
		MediaType: "Video",
		Name:      "No Path Movie",
		Path:      "",
		Extra:     ItemExtra{Chapters: []ChapterRecord{{StartPositionTicks: 0, Name: "Ch"}}},
	}); err != nil {
		t.Fatal(err)
	}

	chapCands, err := libRepo.ListChapterExtractionCandidates(ctx)
	if err != nil {
		t.Fatalf("ListChapterExtractionCandidates: %v", err)
	}
	foundHybridChap := ""
	for _, c := range chapCands {
		if c.ItemID == emptyPathID {
			t.Fatal("empty-path item must be skipped in chapter candidates")
		}
		if c.ItemID == hybridID {
			foundHybridChap = c.MediaPath
		}
	}
	if foundHybridChap != "/movies/hybrid-local.mkv" {
		t.Fatalf("expected local mkv prioritized over strm/empty in chapter candidates, got %q", foundHybridChap)
	}

	bifCands, err := libRepo.ListBIFCandidates(ctx)
	if err != nil {
		t.Fatalf("ListBIFCandidates: %v", err)
	}
	foundHybridBIF := ""
	for _, c := range bifCands {
		if c.ItemID == emptyPathID {
			t.Fatal("empty-path item must be skipped in BIF candidates")
		}
		if c.ItemID == hybridID {
			foundHybridBIF = c.MediaPath
		}
	}
	if foundHybridBIF != "/movies/hybrid-local.mkv" {
		t.Fatalf("expected local mkv prioritized over strm/empty in BIF candidates, got %q", foundHybridBIF)
	}

	// --- Low: ReplaceItemAncestors invalidates count cache + ListTaxonomiesWithPolicy excludes extras & orphans ---
	// Prime count cache for ParentID=lib.ID Recursive=true
	_, cntBefore, err := libRepo.QueryItems(ctx, ItemFilter{ServerID: "srv1", ParentID: lib.ID, Recursive: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := libRepo.ReplaceItemAncestors(ctx, hybridID, []AncestorLink{{AncestorID: hybridID, Depth: 0}, {AncestorID: lib.ID, Depth: 1}}); err != nil {
		t.Fatal(err)
	}
	_, cntAfter, err := libRepo.QueryItems(ctx, ItemFilter{ServerID: "srv1", ParentID: lib.ID, Recursive: true})
	if err != nil {
		t.Fatal(err)
	}
	if cntAfter != cntBefore+1 {
		t.Fatalf("expected count cache invalidated after ReplaceItemAncestors: before=%d after=%d", cntBefore, cntAfter)
	}

	// Attach taxonomy & person ONLY to a trailer extra item -> must NOT appear in ListTaxonomiesWithPolicy!
	extraItem := &ItemRecord{
		ID:        guid.New(),
		LibraryID: lib.ID,
		ParentID:  hybridID,
		Type:      "Video",
		MediaType: "Video",
		Name:      "Trailer",
		Extra:     ItemExtra{ExtraType: "trailer"},
	}
	if err := libRepo.UpsertItem(ctx, extraItem); err != nil {
		t.Fatal(err)
	}
	if err := libRepo.ReplaceItemTaxonomies(ctx, extraItem.ID, "genre", []string{"TrailerOnlyGenre"}); err != nil {
		t.Fatal(err)
	}
	if err := libRepo.ReplaceItemPeople(ctx, extraItem.ID, []PersonInput{{Name: "TrailerOnlyActor", Type: "Actor"}}); err != nil {
		t.Fatal(err)
	}
	// Also attach a real genre & person to hybridID, then verify only real ones appear.
	if err := libRepo.ReplaceItemTaxonomies(ctx, hybridID, "genre", []string{"RealGenre"}); err != nil {
		t.Fatal(err)
	}
	if err := libRepo.ReplaceItemPeople(ctx, hybridID, []PersonInput{{Name: "RealActor", Type: "Actor"}}); err != nil {
		t.Fatal(err)
	}
	genres, gTot, err := libRepo.ListTaxonomies(ctx, "srv1", "genre", "", 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if gTot != 1 || len(genres) != 1 || genres[0].Name != "RealGenre" {
		t.Fatalf("expected only RealGenre (excluding extra-only genre), got %+v total=%d", genres, gTot)
	}
	persons, pTot, err := libRepo.ListTaxonomies(ctx, "srv1", "person", "", 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if pTot != 1 || len(persons) != 1 || persons[0].Name != "RealActor" {
		t.Fatalf("expected only RealActor (excluding extra-only person), got %+v total=%d", persons, pTot)
	}

	// Verify extras counts batch covers trailer, themesong, and specialfeature branches via GetItemByID.
	for _, et := range []string{"themesong", "behindthescenes"} {
		if err := libRepo.UpsertItem(ctx, &ItemRecord{
			ID:        guid.New(),
			LibraryID: lib.ID,
			ParentID:  hybridID,
			Type:      "Video",
			MediaType: "Video",
			Name:      "Extra-" + et,
			Extra:     ItemExtra{ExtraType: et},
		}); err != nil {
			t.Fatal(err)
		}
	}
	hybridDTO, err := libRepo.GetItemByID(ctx, "srv1", user.ID, hybridID)
	if err != nil {
		t.Fatal(err)
	}
	if hybridDTO.LocalTrailerCount != 1 || hybridDTO.SpecialFeatureCount != 1 {
		t.Fatalf("expected LocalTrailerCount=1 SpecialFeatureCount=1, got %d / %d", hybridDTO.LocalTrailerCount, hybridDTO.SpecialFeatureCount)
	}

	// Verify SaveLocalMetadataEnabled & UpdateLibraryOptions edge cases.
	var nilLib *LibraryRecord
	if !nilLib.SaveLocalMetadataEnabled() {
		t.Fatal("nil LibraryRecord must default SaveLocalMetadataEnabled to true")
	}
	if _, err := libRepo.UpdateLibraryOptions(ctx, lib.ID, map[string]any{"SaveLocalMetadata": false}); err != nil {
		t.Fatal(err)
	}
	updLib, err := libRepo.GetLibrary(ctx, lib.ID)
	if err != nil || updLib.SaveLocalMetadataEnabled() {
		t.Fatalf("expected SaveLocalMetadataEnabled=false after update, got lib=%+v err=%v", updLib, err)
	}
	if _, err := libRepo.UpdateLibraryOptions(ctx, "nonexistent", map[string]any{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for nonexistent library options, got %v", err)
	}

	// Item with extra_json containing empty chapters slice covers len(extra.Chapters)==0 skip branch.
	emptyChapID := guid.New()
	if err := libRepo.UpsertItem(ctx, &ItemRecord{
		ID:        emptyChapID,
		LibraryID: lib.ID,
		ParentID:  lib.ID,
		Type:      "Movie",
		MediaType: "Video",
		Name:      "Empty Chapters",
		Path:      "/movies/empty-chap.mkv",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Writer.ExecContext(ctx, `UPDATE items SET extra_json = '{"chapters":[]}' WHERE id = ?`, emptyChapID); err != nil {
		t.Fatal(err)
	}
	cands2, err := libRepo.ListChapterExtractionCandidates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cands2 {
		if c.ItemID == emptyChapID {
			t.Fatal("item with empty chapters array must not be returned as chapter candidate")
		}
	}
}

func TestMigrationV2ItemImagesRebuildAndGetQueryFilters(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "mig_v2.db")
	rawDB, err := sql.Open("sqlite", fmt.Sprintf("file:%s", escapeSQLiteURIPath(dbPath)))
	if err != nil {
		t.Fatal(err)
	}
	sqlBytes, err := migrationsFS.ReadFile("migrations/001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rawDB.ExecContext(ctx, string(sqlBytes)); err != nil {
		t.Fatal(err)
	}
	// Recreate item_images with legacy REFERENCES items(id) to trigger migration v2 rebuild path.
	if _, err := rawDB.ExecContext(ctx, `
		DROP TABLE item_images;
		CREATE TABLE item_images (
			item_id     TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
			image_type  TEXT NOT NULL,
			image_index INTEGER NOT NULL DEFAULT 0,
			path        TEXT NOT NULL,
			tag         TEXT NOT NULL,
			width       INTEGER,
			height      INTEGER,
			blurhash    TEXT,
			PRIMARY KEY (item_id, image_type, image_index)
		);
		INSERT INTO schema_migrations (version, applied_at) VALUES (1, 100);
	`); err != nil {
		t.Fatal(err)
	}
	_ = rawDB.Close()

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open triggering v2->v5 migration: %v", err)
	}
	defer store.Close()

	var ddl string
	if err := store.Reader.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'item_images'`).Scan(&ddl); err != nil {
		t.Fatal(err)
	}
	if len(ddl) == 0 {
		t.Fatal("expected item_images table DDL")
	}

	// Cover GetQueryFilters & closed-store error branches for candidates.
	repo := NewLibraryRepo(store)
	lib, err := repo.CreateLibrary(ctx, "Films", "movies", []string{"/films"})
	if err != nil {
		t.Fatal(err)
	}
	y := 2022
	m := &ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", MediaType: "Video", Name: "FilterFilm", ProductionYear: &y, OfficialRating: "PG-13", Path: "/films/f.mkv"}
	if err := repo.UpsertItem(ctx, m); err != nil {
		t.Fatal(err)
	}
	_ = repo.ReplaceItemAncestors(ctx, m.ID, []AncestorLink{{AncestorID: m.ID, Depth: 0}, {AncestorID: lib.ID, Depth: 1}})
	_ = repo.ReplaceItemTaxonomies(ctx, m.ID, "genre", []string{"Drama"})
	_ = repo.ReplaceItemTaxonomies(ctx, m.ID, "tag", []string{"Indie"})
	_ = repo.ReplaceItemTaxonomies(ctx, m.ID, "studio", []string{"A24"})

	qf, err := repo.GetQueryFilters(ctx, QueryFiltersFilter{
		ParentID:          lib.ID,
		IncludeItemTypes:  []string{"Movie"},
		AllowedLibraryIDs: []string{lib.ID},
	})
	if err != nil {
		t.Fatalf("GetQueryFilters: %v", err)
	}
	if len(qf.Genres) != 1 || len(qf.Studios) != 1 || len(qf.Tags) != 1 || len(qf.OfficialRatings) != 1 || len(qf.Years) != 1 {
		t.Fatalf("unexpected GetQueryFilters: %+v", qf)
	}
	if emptyQF, err := repo.GetQueryFilters(ctx, QueryFiltersFilter{AllowedLibraryIDs: []string{}}); err != nil || len(emptyQF.Genres) != 0 {
		t.Fatalf("expected empty GetQueryFilters for empty AllowedLibraryIDs, got %+v err=%v", emptyQF, err)
	}

	// Closed store error checks
	_ = store.Close()
	if _, err := repo.ListChapterExtractionCandidates(ctx); err == nil {
		t.Fatal("expected error on closed store for ListChapterExtractionCandidates")
	}
	if _, err := repo.ListBIFCandidates(ctx); err == nil {
		t.Fatal("expected error on closed store for ListBIFCandidates")
	}
}

func TestMigrationV5PreloadsReservedNamesAndMigrationV6SkipsWhenApplied(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "mig_v5_v6.db")

	rawDB, err := sql.Open("sqlite", fmt.Sprintf("file:%s", escapeSQLiteURIPath(dbPath)))
	if err != nil {
		t.Fatal(err)
	}
	sqlBytes, err := migrationsFS.ReadFile("migrations/001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rawDB.ExecContext(ctx, string(sqlBytes)); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Unix()
	if _, err := rawDB.ExecContext(ctx, `INSERT INTO schema_migrations (version, applied_at) VALUES (1, ?), (2, ?), (4, ?)`, now, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := rawDB.ExecContext(ctx, `CREATE INDEX idx_users_name_nocase ON users(LOWER(name))`); err != nil {
		t.Fatal(err)
	}
	// Row 3 ('ALICE_dup_2') has a LATER created_at than row 2 ('ALICE'), so without preloading
	// reservedLower, renaming row 2 to 'ALICE_dup_2' would collide with row 3!
	if _, err := rawDB.ExecContext(ctx, `
		INSERT INTO users (id, name, password_hash, is_admin, is_disabled, policy_json, config_json, created_at)
		VALUES ('u1', 'alice', '', 1, 0, '{}', '{}', 100),
		       ('u2', 'ALICE', '', 0, 0, '{}', '{}', 200),
		       ('u3', 'ALICE_dup_2', '', 0, 0, '{}', '{}', 300)`); err != nil {
		t.Fatal(err)
	}
	_ = rawDB.Close()

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open (v5 + v6 migration): %v", err)
	}

	userRepo := NewUserRepo(store)
	u1, err := userRepo.GetUserByID(ctx, "u1")
	if err != nil || u1.Name != "alice" {
		t.Fatalf("expected u1='alice', got %+v err=%v", u1, err)
	}
	u2, err := userRepo.GetUserByID(ctx, "u2")
	if err != nil || u2.Name != "ALICE_dup_2_1" {
		t.Fatalf("expected u2='ALICE_dup_2_1' (avoiding reserved ALICE_dup_2), got %+v err=%v", u2, err)
	}
	u3, err := userRepo.GetUserByID(ctx, "u3")
	if err != nil || u3.Name != "ALICE_dup_2" {
		t.Fatalf("expected u3='ALICE_dup_2' preserved untouched, got %+v err=%v", u3, err)
	}

	// Verify v6 recorded and skipped on subsequent Open() (applied_at sentinel remains untouched).
	if _, err := store.Writer.ExecContext(ctx, `UPDATE schema_migrations SET applied_at = 12345 WHERE version = 6`); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("reopen with v6 already applied: %v", err)
	}
	defer store2.Close()

	var appliedAt int64
	if err := store2.Reader.QueryRowContext(ctx, `SELECT applied_at FROM schema_migrations WHERE version = 6`).Scan(&appliedAt); err != nil {
		t.Fatal(err)
	}
	if appliedAt != 12345 {
		t.Fatalf("expected migration v6 skipped when already recorded, applied_at=%d", appliedAt)
	}
}

func TestTraktRepoNormalizesUUID(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "trakt_norm.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	userRepo := NewUserRepo(store)
	rawUpperDashed := "550E8400-E29B-41D4-A716-446655440000"
	normID := guid.Normalize(rawUpperDashed)
	if err := userRepo.CreateUser(ctx, &UserRecord{ID: normID, Name: "trakt-uuid-user"}); err != nil {
		t.Fatal(err)
	}

	// Empty userID handling.
	if got, err := userRepo.GetUserTraktConfig(ctx, "  "); err != nil || got != nil {
		t.Fatalf("expected nil,nil for empty GetUserTraktConfig, got %+v %v", got, err)
	}
	if err := userRepo.SaveUserTraktConfig(ctx, "  ", TraktConfig{ClientID: "c", AccessToken: "t"}); err == nil {
		t.Fatal("expected error for empty SaveUserTraktConfig")
	}
	if err := userRepo.DeleteUserTraktConfig(ctx, "  "); err != nil {
		t.Fatalf("expected nil for empty DeleteUserTraktConfig, got %v", err)
	}

	// Save with dashed uppercase UUID -> Get with normalized -> Get with mixed dashed -> Delete with dashed uppercase.
	cfg := TraktConfig{ClientID: "cid", AccessToken: "tok", ScrobblePlayback: true, SyncWatched: true}
	if err := userRepo.SaveUserTraktConfig(ctx, rawUpperDashed, cfg); err != nil {
		t.Fatalf("SaveUserTraktConfig dashed uppercase: %v", err)
	}
	got1, err := userRepo.GetUserTraktConfig(ctx, normID)
	if err != nil || got1 == nil || got1.AccessToken != "tok" {
		t.Fatalf("GetUserTraktConfig normalized: got=%+v err=%v", got1, err)
	}
	got2, err := userRepo.GetUserTraktConfig(ctx, " 550e8400-E29B-41d4-a716-446655440000 ")
	if err != nil || got2 == nil || got2.ClientID != "cid" {
		t.Fatalf("GetUserTraktConfig mixed dashed: got=%+v err=%v", got2, err)
	}
	if err := userRepo.DeleteUserTraktConfig(ctx, rawUpperDashed); err != nil {
		t.Fatalf("DeleteUserTraktConfig dashed uppercase: %v", err)
	}
	if got3, err := userRepo.GetUserTraktConfig(ctx, normID); err != nil || got3 != nil {
		t.Fatalf("expected deleted Trakt config, got %+v err=%v", got3, err)
	}
}

func TestCountEpochAndMarkFavoriteItemExistence(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "count_fav.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	userRepo := NewUserRepo(store)
	libRepo := NewLibraryRepo(store)

	user := &UserRecord{ID: guid.New(), Name: "favuser"}
	if err := userRepo.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	lib, err := libRepo.CreateLibrary(ctx, "Movies", "movies", []string{"/movies"})
	if err != nil {
		t.Fatal(err)
	}

	// [L-02] MarkFavorite / UnmarkFavorite on non-existent item must return ErrNotFound and write no row.
	missingID := guid.New()
	if _, err := libRepo.MarkFavorite(ctx, user.ID, missingID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("MarkFavorite missing item: expected ErrNotFound, got %v", err)
	}
	if _, err := libRepo.UnmarkFavorite(ctx, user.ID, missingID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UnmarkFavorite missing item: expected ErrNotFound, got %v", err)
	}
	var orphanRows int
	if err := store.Reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_item_data WHERE item_id = ?`, missingID).Scan(&orphanRows); err != nil {
		t.Fatal(err)
	}
	if orphanRows != 0 {
		t.Fatalf("expected 0 orphan user_item_data rows, got %d", orphanRows)
	}

	// Existing item succeeds for MarkFavorite / UnmarkFavorite.
	movieID := guid.New()
	if err := libRepo.UpsertItem(ctx, &ItemRecord{
		ID: movieID, LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", MediaType: "Video", Name: "Fav Movie",
	}); err != nil {
		t.Fatal(err)
	}
	ud, err := libRepo.MarkFavorite(ctx, user.ID, movieID)
	if err != nil || !ud.IsFavorite {
		t.Fatalf("MarkFavorite existing: got %+v err=%v", ud, err)
	}
	ud, err = libRepo.UnmarkFavorite(ctx, user.ID, movieID)
	if err != nil || ud.IsFavorite {
		t.Fatalf("UnmarkFavorite existing: got %+v err=%v", ud, err)
	}

	// [M-05] countEpoch increments on InvalidateCountCache and stays race-free under concurrent reads/writes.
	libRepo.countMu.RLock()
	ep0 := libRepo.countEpoch
	libRepo.countMu.RUnlock()
	libRepo.InvalidateCountCache()
	libRepo.countMu.RLock()
	ep1 := libRepo.countEpoch
	libRepo.countMu.RUnlock()
	if ep1 <= ep0 {
		t.Fatalf("expected countEpoch incremented on InvalidateCountCache: before=%d after=%d", ep0, ep1)
	}

	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 15; i++ {
				_, _, _ = libRepo.QueryItems(ctx, ItemFilter{ServerID: "srv", LibraryID: lib.ID})
				_, _ = libRepo.GetItemCounts(ctx)
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			_ = libRepo.UpsertItem(ctx, &ItemRecord{
				ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
				Type: "Movie", MediaType: "Video", Name: fmt.Sprintf("Race %d", i),
			})
		}
	}()
	wg.Wait()

	// Final counts must match exact DB count (1 + 10 = 11), never a stale cached count.
	_, total, err := libRepo.QueryItems(ctx, ItemFilter{ServerID: "srv", LibraryID: lib.ID})
	if err != nil || total != 11 {
		t.Fatalf("expected QueryItems total=11 after concurrent writes, got %d err=%v", total, err)
	}
	counts, err := libRepo.GetItemCounts(ctx)
	if err != nil || counts.MovieCount != 11 {
		t.Fatalf("expected GetItemCounts MovieCount=11, got %+v err=%v", counts, err)
	}
}

func TestDeleteItemsByPathPrefix(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "prefix_del.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := NewLibraryRepo(store)
	lib1, err := repo.CreateLibrary(ctx, "TV1", "tvshows", []string{"/media/tv"})
	if err != nil {
		t.Fatal(err)
	}
	lib2, err := repo.CreateLibrary(ctx, "TV2", "tvshows", []string{"/media/tv"})
	if err != nil {
		t.Fatal(err)
	}

	showA := &ItemRecord{ID: guid.New(), LibraryID: lib1.ID, ParentID: lib1.ID, Type: "Series", IsFolder: true, Name: "ShowA", Path: "/media/tv/ShowA"}
	seasonA := &ItemRecord{ID: guid.New(), LibraryID: lib1.ID, ParentID: showA.ID, SeriesID: showA.ID, Type: "Season", IsFolder: true, Name: "Season 1", Path: ""}
	epA := &ItemRecord{ID: guid.New(), LibraryID: lib1.ID, ParentID: seasonA.ID, SeriesID: showA.ID, SeasonID: seasonA.ID, Type: "Episode", MediaType: "Video", Name: "Ep1", Path: "/media/tv/ShowA/Season 1/ep1.mkv"}
	siblingShowA2 := &ItemRecord{ID: guid.New(), LibraryID: lib1.ID, ParentID: lib1.ID, Type: "Movie", MediaType: "Video", Name: "ShowA2", Path: "/media/tv/ShowA2/movie.mkv"}
	otherLibItem := &ItemRecord{ID: guid.New(), LibraryID: lib2.ID, ParentID: lib2.ID, Type: "Episode", MediaType: "Video", Name: "OtherLibEp", Path: "/media/tv/ShowA/Season 1/ep1.mkv"}

	for _, rec := range []*ItemRecord{showA, seasonA, epA, siblingShowA2, otherLibItem} {
		if err := repo.UpsertItem(ctx, rec); err != nil {
			t.Fatal(err)
		}
		_ = repo.UpsertSearchFTS(ctx, rec.ID, rec.Name, rec.Name, "", "", "")
		_ = repo.UpsertImage(ctx, &ImageRecord{ItemID: rec.ID, ImageType: "Primary", Path: "/img/" + rec.ID + ".jpg", Tag: "t"})
	}
	_ = repo.ReplaceItemAncestors(ctx, showA.ID, []AncestorLink{{AncestorID: showA.ID, Depth: 0}, {AncestorID: lib1.ID, Depth: 1}})
	_ = repo.ReplaceItemAncestors(ctx, seasonA.ID, []AncestorLink{{AncestorID: seasonA.ID, Depth: 0}, {AncestorID: showA.ID, Depth: 1}, {AncestorID: lib1.ID, Depth: 2}})
	_ = repo.ReplaceItemAncestors(ctx, epA.ID, []AncestorLink{{AncestorID: epA.ID, Depth: 0}, {AncestorID: seasonA.ID, Depth: 1}, {AncestorID: showA.ID, Depth: 2}, {AncestorID: lib1.ID, Depth: 3}})

	// Prime count cache.
	_, beforeTotal, err := repo.QueryItems(ctx, ItemFilter{ServerID: "srv", LibraryID: lib1.ID})
	if err != nil || beforeTotal != 4 {
		t.Fatalf("expected 4 items in lib1 before delete, got %d err=%v", beforeTotal, err)
	}

	// Empty args -> no-op.
	if n, err := repo.DeleteItemsByPathPrefix(ctx, "", "/media/tv/ShowA"); err != nil || n != 0 {
		t.Fatalf("expected 0,nil for empty libID, got %d %v", n, err)
	}
	if n, err := repo.DeleteItemsByPathPrefix(ctx, lib1.ID, "  "); err != nil || n != 0 {
		t.Fatalf("expected 0,nil for empty dirPath, got %d %v", n, err)
	}

	// Delete ShowA subtree in lib1 (with trailing slash to verify filepath.Clean normalization).
	deleted, err := repo.DeleteItemsByPathPrefix(ctx, lib1.ID, "/media/tv/ShowA/")
	if err != nil {
		t.Fatalf("DeleteItemsByPathPrefix: %v", err)
	}
	if deleted != 3 {
		t.Fatalf("expected 3 items deleted (showA + virtual seasonA + epA), got %d", deleted)
	}

	// Count cache must be invalidated.
	_, afterTotal, err := repo.QueryItems(ctx, ItemFilter{ServerID: "srv", LibraryID: lib1.ID})
	if err != nil || afterTotal != 1 {
		t.Fatalf("expected 1 item left in lib1 after DeleteItemsByPathPrefix, got %d err=%v", afterTotal, err)
	}

	// Sibling prefix (/media/tv/ShowA2) and other library item MUST survive.
	if _, err := repo.GetItemRecordByID(ctx, siblingShowA2.ID); err != nil {
		t.Fatalf("sibling ShowA2 should not be deleted: %v", err)
	}
	if _, err := repo.GetItemRecordByID(ctx, otherLibItem.ID); err != nil {
		t.Fatalf("other library item should not be deleted: %v", err)
	}

	// Images, FTS, and ancestors for deleted items must be cleaned up.
	var leftover int
	_ = store.Reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM item_images WHERE item_id IN (?, ?, ?)`, showA.ID, seasonA.ID, epA.ID).Scan(&leftover)
	if leftover != 0 {
		t.Fatalf("expected 0 leftover item_images, got %d", leftover)
	}
	_ = store.Reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM item_search_fts WHERE item_id IN (?, ?, ?)`, showA.ID, seasonA.ID, epA.ID).Scan(&leftover)
	if leftover != 0 {
		t.Fatalf("expected 0 leftover item_search_fts, got %d", leftover)
	}
	_ = store.Reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM item_ancestors WHERE descendant_id IN (?, ?, ?)`, showA.ID, seasonA.ID, epA.ID).Scan(&leftover)
	if leftover != 0 {
		t.Fatalf("expected 0 leftover item_ancestors, got %d", leftover)
	}
}

func TestMergeItemVersionsMigratesLocalExtrasAndAncestors(t *testing.T) {
	ctx := context.Background()
	store, repo := openTestMultiVersionRepo(t)

	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{"/movies"})
	if err != nil {
		t.Fatal(err)
	}
	m1 := &ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", MediaType: "Video", Name: "Inception 4K", Path: "/movies/4k/inception.mkv"}
	m2 := &ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", MediaType: "Video", Name: "Inception 1080p", Path: "/movies/1080p/inception.mkv"}
	trailer := &ItemRecord{
		ID:        guid.New(),
		LibraryID: lib.ID,
		ParentID:  m2.ID,
		Type:      "Video",
		MediaType: "Video",
		Name:      "Official Trailer",
		Path:      "/movies/1080p/trailers/trailer.mkv",
		Extra:     ItemExtra{ExtraType: "trailer"},
	}
	featurette := &ItemRecord{
		ID:        guid.New(),
		LibraryID: lib.ID,
		ParentID:  m2.ID,
		Type:      "Video",
		MediaType: "Video",
		Name:      "Behind The Scenes",
		Path:      "/movies/1080p/featurettes/bts.mkv",
		Extra:     ItemExtra{ExtraType: "behindthescenes"},
	}
	for _, rec := range []*ItemRecord{m1, m2, trailer, featurette} {
		if err := repo.UpsertItem(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}
	_ = repo.UpsertMediaSourceAndStreams(ctx, m1.ID, MediaSourceRecord{Name: "4K", Path: m1.Path, Container: "mkv"})
	_ = repo.UpsertMediaSourceAndStreams(ctx, m2.ID, MediaSourceRecord{Name: "1080p", Path: m2.Path, Container: "mkv"})
	_ = repo.ReplaceItemAncestors(ctx, m1.ID, []AncestorLink{{AncestorID: m1.ID, Depth: 0}, {AncestorID: lib.ID, Depth: 1}})
	_ = repo.ReplaceItemAncestors(ctx, m2.ID, []AncestorLink{{AncestorID: m2.ID, Depth: 0}, {AncestorID: lib.ID, Depth: 1}})
	_ = repo.ReplaceItemAncestors(ctx, trailer.ID, []AncestorLink{{AncestorID: trailer.ID, Depth: 0}, {AncestorID: m2.ID, Depth: 1}, {AncestorID: lib.ID, Depth: 2}})
	_ = repo.ReplaceItemAncestors(ctx, featurette.ID, []AncestorLink{{AncestorID: featurette.ID, Depth: 0}, {AncestorID: m2.ID, Depth: 1}, {AncestorID: lib.ID, Depth: 2}})

	if _, merged, err := repo.MergeItemVersions(ctx, []string{m1.ID, m2.ID}); err != nil || merged != 1 {
		t.Fatalf("MergeItemVersions: merged=%d err=%v", merged, err)
	}

	// Extras must now be parented to m1.ID and have item_ancestors updated to m1.ID.
	for _, exID := range []string{trailer.ID, featurette.ID} {
		rec, err := repo.GetItemRecordByID(ctx, exID)
		if err != nil {
			t.Fatalf("extra %s missing after merge: %v", exID, err)
		}
		if rec.ParentID != m1.ID {
			t.Fatalf("expected extra %s parent_id=%s, got %s", exID, m1.ID, rec.ParentID)
		}
		var hasPrimaryAnc, hasOldAnc int
		_ = store.Reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM item_ancestors WHERE ancestor_id = ? AND descendant_id = ?`, m1.ID, exID).Scan(&hasPrimaryAnc)
		_ = store.Reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM item_ancestors WHERE ancestor_id = ? AND descendant_id = ?`, m2.ID, exID).Scan(&hasOldAnc)
		if hasPrimaryAnc != 1 || hasOldAnc != 0 {
			t.Fatalf("extra %s ancestors mismatch: primary=%d old=%d", exID, hasPrimaryAnc, hasOldAnc)
		}
	}

	dtoItem, err := repo.GetItemByID(ctx, "srv1", "", m1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if dtoItem.LocalTrailerCount != 1 || dtoItem.SpecialFeatureCount != 1 {
		t.Fatalf("expected migrated extras counts 1/1 on primary, got %d/%d", dtoItem.LocalTrailerCount, dtoItem.SpecialFeatureCount)
	}
}
