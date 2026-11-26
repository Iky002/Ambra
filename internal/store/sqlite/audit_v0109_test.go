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
