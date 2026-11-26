package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
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
