package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func explainQueryPlan(t *testing.T, db *sql.DB, query string, args ...any) string {
	t.Helper()
	rows, err := db.Query("EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN failed for %q: %v", query, err)
	}
	defer rows.Close()

	var lines []string
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatalf("scan EXPLAIN QUERY PLAN: %v", err)
		}
		lines = append(lines, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows EXPLAIN QUERY PLAN: %v", err)
	}
	return strings.Join(lines, "\n")
}

func TestExplainQueryPlanIndexes(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "explain_v100.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	repo := NewLibraryRepo(store)
	ctx := context.Background()

	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{"/movies"})
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}

	// Seed enough rows (normal items + extras) so SQLite's query planner prefers partial indexes after ANALYZE.
	batch := make([]BatchItemInput, 0, 220)
	now := time.Now().Unix()
	for i := 0; i < 200; i++ {
		name := fmt.Sprintf("Movie %03d", i)
		switch i % 4 {
		case 0:
			name = fmt.Sprintf("肖申克的救赎 %03d", i)
		case 1:
			name = fmt.Sprintf("霸王别姬 %03d", i)
		case 2:
			name = fmt.Sprintf("2001太空漫游 %03d", i)
		}
		batch = append(batch, BatchItemInput{
			Item: &ItemRecord{
				ID:          fmt.Sprintf("movie-%03d", i),
				LibraryID:   lib.ID,
				ParentID:    lib.ID,
				Type:        "Movie",
				MediaType:   "Video",
				Name:        name,
				DateCreated: now + int64(i),
			},
		})
	}
	for i := 0; i < 20; i++ {
		batch = append(batch, BatchItemInput{
			Item: &ItemRecord{
				ID:        fmt.Sprintf("extra-%03d", i),
				LibraryID: lib.ID,
				ParentID:  "movie-000",
				Type:      "Video",
				MediaType: "Video",
				Name:      fmt.Sprintf("Trailer %02d", i),
				Extra:     ItemExtra{ExtraType: "Trailer"},
			},
		})
	}
	if err := repo.UpsertItemsBatch(ctx, batch); err != nil {
		t.Fatalf("UpsertItemsBatch: %v", err)
	}
	if _, err := store.Writer.ExecContext(ctx, `ANALYZE`); err != nil {
		t.Fatalf("ANALYZE: %v", err)
	}

	t.Run("PosterWallSortKey", func(t *testing.T) {
		plan := explainQueryPlan(t, store.Reader,
			`SELECT id FROM items WHERE library_id = ? AND type = 'Movie' AND extra_type = '' ORDER BY sort_key ASC LIMIT 50`,
			lib.ID,
		)
		if !strings.Contains(plan, "idx_items_lib_type_sortkey") {
			t.Fatalf("expected idx_items_lib_type_sortkey, got:\n%s", plan)
		}
		if strings.Contains(plan, "SCAN items") {
			t.Fatalf("unexpected SCAN items:\n%s", plan)
		}
		if strings.Contains(plan, "USE TEMP B-TREE") {
			t.Fatalf("unexpected USE TEMP B-TREE:\n%s", plan)
		}
	})

	t.Run("ChineseAZNameStartsWith", func(t *testing.T) {
		plan := explainQueryPlan(t, store.Reader,
			`SELECT id FROM items WHERE library_id = ? AND type = 'Movie' AND extra_type = '' AND sort_key >= 'x' AND sort_key < 'y' ORDER BY sort_key ASC LIMIT 50`,
			lib.ID,
		)
		if !strings.Contains(plan, "idx_items_lib_type_sortkey") {
			t.Fatalf("expected idx_items_lib_type_sortkey, got:\n%s", plan)
		}
		if strings.Contains(plan, "SCAN items") {
			t.Fatalf("unexpected SCAN items:\n%s", plan)
		}
	})

	t.Run("HashNameLessThan", func(t *testing.T) {
		plan := explainQueryPlan(t, store.Reader,
			`SELECT id FROM items WHERE library_id = ? AND type = 'Movie' AND extra_type = '' AND sort_key < 'a' ORDER BY sort_key ASC LIMIT 50`,
			lib.ID,
		)
		if !strings.Contains(plan, "idx_items_lib_type_sortkey") {
			t.Fatalf("expected idx_items_lib_type_sortkey, got:\n%s", plan)
		}
		if strings.Contains(plan, "SCAN items") {
			t.Fatalf("unexpected SCAN items:\n%s", plan)
		}
	})

	t.Run("GetLatestItems", func(t *testing.T) {
		plan := explainQueryPlan(t, store.Reader,
			`SELECT id FROM items WHERE library_id = ? AND type = 'Movie' AND extra_type = '' ORDER BY date_created DESC LIMIT 20`,
			lib.ID,
		)
		if !strings.Contains(plan, "idx_items_lib_type_created") {
			t.Fatalf("expected idx_items_lib_type_created, got:\n%s", plan)
		}
		if strings.Contains(plan, "USE TEMP B-TREE") {
			t.Fatalf("unexpected USE TEMP B-TREE:\n%s", plan)
		}
	})

	t.Run("GetExtras", func(t *testing.T) {
		plan := explainQueryPlan(t, store.Reader,
			`SELECT id FROM items WHERE parent_id = ? AND extra_type != ''`,
			"movie-000",
		)
		if !strings.Contains(plan, "idx_items_parent_extras") {
			t.Fatalf("expected idx_items_parent_extras, got:\n%s", plan)
		}
		if strings.Contains(plan, "SCAN items") {
			t.Fatalf("unexpected SCAN items:\n%s", plan)
		}
	})
}

func TestMigrationV3BackfillAndBatchAndCountCache(t *testing.T) {
	t.Setenv("AMBRA_SQLITE_CACHE_MB", "32")
	t.Setenv("AMBRA_SQLITE_MMAP_MB", "128")

	dbPath := filepath.Join(t.TempDir(), "migrate_v2_to_v3.db")

	// 1. Create a v2 schema database directly using 001_init.sql without v3 columns.
	rawDB, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)", dbPath))
	if err != nil {
		t.Fatalf("open raw v2 db: %v", err)
	}
	initSQL, err := migrationsFS.ReadFile("migrations/001_init.sql")
	if err != nil {
		t.Fatalf("read 001_init.sql: %v", err)
	}
	if _, err := rawDB.Exec(string(initSQL)); err != nil {
		t.Fatalf("exec 001_init.sql: %v", err)
	}
	now := time.Now().Unix()
	if _, err := rawDB.Exec(`INSERT INTO schema_migrations (version, applied_at) VALUES (1, ?), (2, ?)`, now, now); err != nil {
		t.Fatalf("insert v1/v2 migrations: %v", err)
	}
	libID := "libv2001"
	if _, err := rawDB.Exec(`INSERT INTO libraries (id, name, collection_type, paths_json, options_json, created_at, updated_at) VALUES (?, 'Movies', 'movies', '["/m"]', '{}', ?, ?)`, libID, now, now); err != nil {
		t.Fatalf("insert library: %v", err)
	}
	insertLegacy := `
		INSERT INTO items (
			id, library_id, parent_id, type, media_type, is_folder, is_strm,
			name, sort_name, official_rating, etag, extra_json, date_created, date_modified
		) VALUES (?, ?, ?, ?, 'Video', 0, 0, ?, ?, ?, 'etag', ?, ?, ?)`
	if _, err := rawDB.Exec(insertLegacy, "legacy-cn", libID, libID, "Movie", "肖申克的救赎", "肖申克的救赎", "R", `{}`, now, now); err != nil {
		t.Fatalf("insert legacy-cn: %v", err)
	}
	if _, err := rawDB.Exec(insertLegacy, "legacy-pg13", libID, libID, "Movie", "Inception", "inception", "PG-13", `{}`, now, now); err != nil {
		t.Fatalf("insert legacy-pg13: %v", err)
	}
	if _, err := rawDB.Exec(insertLegacy, "legacy-unrated", libID, libID, "Movie", "2001太空漫游", "2001太空漫游", "", `{}`, now, now); err != nil {
		t.Fatalf("insert legacy-unrated: %v", err)
	}
	if _, err := rawDB.Exec(insertLegacy, "legacy-extra", libID, "legacy-cn", "Video", "Trailer 1", "trailer 1", "", `{"extra_type":"Trailer"}`, now, now); err != nil {
		t.Fatalf("insert legacy-extra: %v", err)
	}
	if err := rawDB.Close(); err != nil {
		t.Fatalf("close raw db: %v", err)
	}

	// 2. Reopen via Open() -> must apply migration v3 and backfill all 3 columns.
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open v2->v3: %v", err)
	}
	defer store.Close()

	var v3Count int
	if err := store.Reader.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = 3`).Scan(&v3Count); err != nil || v3Count != 1 {
		t.Fatalf("expected schema_migrations v3=1, got %d err=%v", v3Count, err)
	}

	// Verify backfilled columns directly.
	var sortKey, extraType string
	var ratingVal int
	if err := store.Reader.QueryRow(`SELECT sort_key, extra_type, parental_rating_value FROM items WHERE id = 'legacy-cn'`).Scan(&sortKey, &extraType, &ratingVal); err != nil {
		t.Fatalf("query legacy-cn: %v", err)
	}
	if !strings.HasPrefix(sortKey, "xiaoshenke") || extraType != "" || ratingVal != 9 {
		t.Fatalf("unexpected legacy-cn backfill: sort_key=%q extra_type=%q parental_rating_value=%d", sortKey, extraType, ratingVal)
	}
	if err := store.Reader.QueryRow(`SELECT extra_type FROM items WHERE id = 'legacy-extra'`).Scan(&extraType); err != nil {
		t.Fatalf("query legacy-extra: %v", err)
	}
	if extraType != "trailer" {
		t.Fatalf("expected legacy-extra extra_type=trailer, got %q", extraType)
	}

	repo := NewLibraryRepo(store)
	ctx := context.Background()

	// Parental rating filter: maxRating=7 (PG-13) allows PG-13 (7) and unrated (0), excludes R (9).
	maxPG13 := 7
	items, total, err := repo.QueryItems(ctx, ItemFilter{
		LibraryID:         libID,
		IncludeItemTypes:  []string{"Movie"},
		MaxParentalRating: &maxPG13,
	})
	if err != nil || total != 2 || len(items) != 2 {
		t.Fatalf("expected 2 items under PG-13, got total=%d len=%d err=%v", total, len(items), err)
	}

	// 3. Test UpsertItemsBatch with ancestors and FTS5.
	err = repo.UpsertItemsBatch(ctx, []BatchItemInput{
		{
			Item: &ItemRecord{
				ID:             "batch1",
				LibraryID:      libID,
				ParentID:       libID,
				Type:           "Movie",
				MediaType:      "Video",
				Name:           "流浪地球",
				OfficialRating: "PG-13",
			},
			AncestorIDs:    []string{"folderscifi"},
			FTSTitleRaw:    "流浪地球",
			FTSPeopleNames: "吴京",
		},
		{
			Item: &ItemRecord{
				ID:        "batch2",
				LibraryID: libID,
				ParentID:  libID,
				Type:      "Movie",
				MediaType: "Video",
				Name:      "Interstellar",
			},
			FTSTitleRaw: "Interstellar",
		},
	})
	if err != nil {
		t.Fatalf("UpsertItemsBatch: %v", err)
	}

	// Verify ancestor link and FTS5 search from batch.
	ancItems, ancTotal, err := repo.QueryItems(ctx, ItemFilter{
		ParentID:  "folderscifi",
		Recursive: true,
	})
	if err != nil || ancTotal != 1 || ancItems[0].Id != "batch1" {
		t.Fatalf("expected batch1 under folderscifi, got total=%d items=%+v err=%v", ancTotal, ancItems, err)
	}
	ftsItems, ftsTotal, err := repo.QueryItems(ctx, ItemFilter{
		SearchTerm: "流浪地球",
	})
	if err != nil || ftsTotal != 1 || ftsItems[0].Id != "batch1" {
		t.Fatalf("expected FTS5 match batch1, got total=%d items=%+v err=%v", ftsTotal, ftsItems, err)
	}

	// 4. Test COUNT(*) cache hit and invalidation on writes.
	_, total1, err := repo.QueryItems(ctx, ItemFilter{LibraryID: libID, IncludeItemTypes: []string{"Movie"}})
	if err != nil || total1 != 5 {
		t.Fatalf("expected 5 movies, got %d err=%v", total1, err)
	}
	counts1, err := repo.GetItemCounts(ctx)
	if err != nil || counts1.MovieCount != 5 {
		t.Fatalf("expected MovieCount=5, got %+v err=%v", counts1, err)
	}

	// Verify cache actually populated.
	repo.countMu.RLock()
	cacheLen := len(repo.countCache)
	hasItemCountsCache := repo.cachedItemCounts != nil
	repo.countMu.RUnlock()
	if cacheLen == 0 || !hasItemCountsCache {
		t.Fatalf("expected countCache and cachedItemCounts to be populated, got cacheLen=%d hasItemCounts=%v", cacheLen, hasItemCountsCache)
	}

	// UpsertItem must invalidate cache.
	if err := repo.UpsertItem(ctx, &ItemRecord{
		ID:        "single-6",
		LibraryID: libID,
		ParentID:  libID,
		Type:      "Movie",
		MediaType: "Video",
		Name:      "Tenet",
	}); err != nil {
		t.Fatalf("UpsertItem: %v", err)
	}
	_, total2, err := repo.QueryItems(ctx, ItemFilter{LibraryID: libID, IncludeItemTypes: []string{"Movie"}})
	if err != nil || total2 != 6 {
		t.Fatalf("expected 6 movies after UpsertItem cache invalidation, got %d err=%v", total2, err)
	}
	counts2, err := repo.GetItemCounts(ctx)
	if err != nil || counts2.MovieCount != 6 {
		t.Fatalf("expected MovieCount=6 after UpsertItem cache invalidation, got %+v err=%v", counts2, err)
	}

	// DeleteItem must invalidate cache.
	if err := repo.DeleteItem(ctx, "single-6"); err != nil {
		t.Fatalf("DeleteItem: %v", err)
	}
	_, total3, err := repo.QueryItems(ctx, ItemFilter{LibraryID: libID, IncludeItemTypes: []string{"Movie"}})
	if err != nil || total3 != 5 {
		t.Fatalf("expected 5 movies after DeleteItem cache invalidation, got %d err=%v", total3, err)
	}

	// 5. Test FTS5 dirty-check no-op (total_changes() on Writer must NOT advance when payload is identical).
	var changesBefore int64
	if err := store.Writer.QueryRow(`SELECT total_changes()`).Scan(&changesBefore); err != nil {
		t.Fatalf("select total_changes before: %v", err)
	}
	if err := repo.UpsertSearchFTS(ctx, "batch1", "流浪地球", "", "", "", "吴京"); err != nil {
		t.Fatalf("UpsertSearchFTS identical: %v", err)
	}
	var changesSame int64
	if err := store.Writer.QueryRow(`SELECT total_changes()`).Scan(&changesSame); err != nil {
		t.Fatalf("select total_changes same: %v", err)
	}
	if changesSame != changesBefore {
		t.Fatalf("expected FTS5 dirty-check no-op to cause 0 writes (before=%d after=%d)", changesBefore, changesSame)
	}

	// Changing FTS5 payload must delete + re-insert and make the new title searchable.
	if err := repo.UpsertSearchFTS(ctx, "batch1", "流浪地球2", "", "", "", "吴京 刘德华"); err != nil {
		t.Fatalf("UpsertSearchFTS changed: %v", err)
	}
	var changesAfter int64
	if err := store.Writer.QueryRow(`SELECT total_changes()`).Scan(&changesAfter); err != nil {
		t.Fatalf("select total_changes after: %v", err)
	}
	if changesAfter <= changesBefore {
		t.Fatalf("expected FTS5 update to modify rows (before=%d after=%d)", changesBefore, changesAfter)
	}
	updatedHits, updatedTotal, err := repo.QueryItems(ctx, ItemFilter{SearchTerm: "刘德华"})
	if err != nil || updatedTotal != 1 || updatedHits[0].Id != "batch1" {
		t.Fatalf("expected updated FTS5 to match 刘德华 -> batch1, got total=%d hits=%+v err=%v", updatedTotal, updatedHits, err)
	}
}

func BenchmarkLargeLibraryQueries(b *testing.B) {
	store, err := Open(filepath.Join(b.TempDir(), "bench_100k.db"))
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	defer store.Close()

	repo := NewLibraryRepo(store)
	ctx := context.Background()

	lib, err := repo.CreateLibrary(ctx, "LargeMovies", "movies", []string{"/movies"})
	if err != nil {
		b.Fatalf("CreateLibrary: %v", err)
	}

	const totalItems = 100_000
	const chunkSize = 1_000
	prefixes := []string{
		"肖申克的救赎", "霸王别姬", "海上钢琴师", "阿甘正传", "三体",
		"流浪地球", "星际穿越", "盗梦空间", "Batman", "Inception",
		"Interstellar", "Avatar", "Matrix", "Dune", "Oppenheimer",
		"2001太空漫游",
	}

	baseTime := time.Now().Unix() - totalItems
	for start := 0; start < totalItems; start += chunkSize {
		chunk := make([]BatchItemInput, chunkSize)
		for j := 0; j < chunkSize; j++ {
			idx := start + j
			title := fmt.Sprintf("%s %06d", prefixes[idx%len(prefixes)], idx)
			// Only populate FTS5 for every 10th item or all items? Populate all for realistic FTS5 benchmark!
			chunk[j] = BatchItemInput{
				Item: &ItemRecord{
					ID:          fmt.Sprintf("item-%06d", idx),
					LibraryID:   lib.ID,
					ParentID:    lib.ID,
					Type:        "Movie",
					MediaType:   "Video",
					Name:        title,
					DateCreated: baseTime + int64(idx),
				},
				FTSTitleRaw: title,
			}
		}
		if err := repo.UpsertItemsBatch(ctx, chunk); err != nil {
			b.Fatalf("UpsertItemsBatch at %d: %v", start, err)
		}
	}
	if _, err := store.Writer.ExecContext(ctx, `ANALYZE`); err != nil {
		b.Fatalf("ANALYZE: %v", err)
	}

	b.ResetTimer()

	b.Run("HomeLatest", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			items, err := repo.GetLatestItems(ctx, "srv-1", "", lib.ID, []string{"Movie"}, 20, nil)
			if err != nil || len(items) != 20 {
				b.Fatalf("GetLatestItems: len=%d err=%v", len(items), err)
			}
		}
	})

	b.Run("SortNamePagination", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			items, total, err := repo.QueryItems(ctx, ItemFilter{
				ServerID:         "srv-1",
				LibraryID:        lib.ID,
				IncludeItemTypes: []string{"Movie"},
				SortBy:           []string{"SortName"},
				SortOrder:        "Ascending",
				StartIndex:       500,
				Limit:            50,
			})
			if err != nil || len(items) != 50 || total != totalItems {
				b.Fatalf("SortNamePagination: len=%d total=%d err=%v", len(items), total, err)
			}
		}
	})

	b.Run("ChineseAZIndex", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			items, _, err := repo.QueryItems(ctx, ItemFilter{
				ServerID:         "srv-1",
				LibraryID:        lib.ID,
				IncludeItemTypes: []string{"Movie"},
				NameStartsWith:   "x",
				SortBy:           []string{"SortName"},
				SortOrder:        "Ascending",
				Limit:            50,
			})
			if err != nil || len(items) != 50 {
				b.Fatalf("ChineseAZIndex: len=%d err=%v", len(items), err)
			}
		}
	})

	b.Run("FTS5Search", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			items, _, err := repo.QueryItems(ctx, ItemFilter{
				ServerID:         "srv-1",
				LibraryID:        lib.ID,
				IncludeItemTypes: []string{"Movie"},
				SearchTerm:       "流浪地球 0500",
				Limit:            20,
			})
			if err != nil || len(items) == 0 {
				b.Fatalf("FTS5Search: len=%d err=%v", len(items), err)
			}
		}
	})
}
