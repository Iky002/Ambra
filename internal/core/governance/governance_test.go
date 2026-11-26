package governance

import (
	"context"
	"encoding/xml"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/budget"
	"github.com/ambra-media/ambra/internal/core/scheduler"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func newTestEnv(t *testing.T) (*Service, *sqlite.Store, *sqlite.LibraryRepo, *config.Config, *scheduler.Manager, *budget.Pool) {
	t.Helper()
	dataDir := t.TempDir()
	cfg, err := config.Load(dataDir)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	store, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	sched := scheduler.New(nil)
	pool := budget.New(4)
	svc, err := NewService(Options{
		Store:      store,
		Config:     cfg,
		Scheduler:  sched,
		BudgetPool: pool,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc, store, sqlite.NewLibraryRepo(store), cfg, sched, pool
}

func intPtr(v int) *int             { return &v }
func floatPtr(v float64) *float64   { return &v }

func TestNewServiceValidation(t *testing.T) {
	if _, err := NewService(Options{}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for nil store, got %v", err)
	}
	dataDir := t.TempDir()
	store, err := sqlite.Open(filepath.Join(dataDir, "test.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer store.Close()

	// Nil BudgetPool falls back to global pool.
	svc, err := NewService(Options{Store: store})
	if err != nil {
		t.Fatalf("NewService with nil BudgetPool: %v", err)
	}
	if svc.budgetPool == nil {
		t.Fatalf("expected fallback global budget pool")
	}
}

func TestLockedFieldsLifecycle(t *testing.T) {
	svc, _, repo, _, _, _ := newTestEnv(t)
	ctx := context.Background()

	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{"/media"})
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}
	item := &sqlite.ItemRecord{
		LibraryID: lib.ID,
		Type:      "Movie",
		Name:      "Inception",
		Extra: sqlite.ItemExtra{
			Overview:    "Dream within a dream",
			ProviderIDs: map[string]string{"Tmdb": "27205"},
		},
	}
	if err := repo.UpsertItem(ctx, item); err != nil {
		t.Fatalf("UpsertItem: %v", err)
	}

	// Empty / missing item errors
	if _, err := svc.SetLockedFields(ctx, "", []string{"Name"}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
	if _, err := svc.SetLockedFields(ctx, "nonexistent", []string{"Name"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if _, err := svc.GetLockedFields(ctx, ""); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
	if _, err := svc.GetLockedFields(ctx, "nonexistent"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	// Initially empty
	locked, err := svc.GetLockedFields(ctx, item.ID)
	if err != nil || len(locked) != 0 {
		t.Fatalf("expected empty locked fields, got %v err=%v", locked, err)
	}

	// Set with duplicates and whitespace
	res, err := svc.SetLockedFields(ctx, item.ID, []string{" Name ", "Overview", "name", "", "Genres"})
	if err != nil {
		t.Fatalf("SetLockedFields: %v", err)
	}
	if len(res.LockedFields) != 3 {
		t.Fatalf("expected 3 deduplicated fields, got %v", res.LockedFields)
	}

	// Verify readback and preservation of existing extra_json fields
	got, err := svc.GetLockedFields(ctx, item.ID)
	if err != nil {
		t.Fatalf("GetLockedFields: %v", err)
	}
	if len(got) != 3 || got[0] != "Name" || got[1] != "Overview" || got[2] != "Genres" {
		t.Fatalf("unexpected locked fields: %v", got)
	}

	var extraRaw string
	if err := svc.store.Reader.QueryRowContext(ctx, `SELECT extra_json FROM items WHERE id = ?`, item.ID).Scan(&extraRaw); err != nil {
		t.Fatalf("query extra_json: %v", err)
	}
	extra := decodeExtraMap(extraRaw)
	if extra["overview"] != "Dream within a dream" {
		t.Fatalf("expected overview preserved in extra_json, got %v", extra["overview"])
	}
	if extractProviderIDs(extra)["Tmdb"] != "27205" {
		t.Fatalf("expected provider_ids preserved in extra_json, got %v", extra["provider_ids"])
	}
}

func TestWriteItemNFOAtomic_MovieSeriesEpisode(t *testing.T) {
	svc, _, repo, _, _, _ := newTestEnv(t)
	ctx := context.Background()

	mediaDir := t.TempDir()
	lib, err := repo.CreateLibrary(ctx, "Media", "movies", []string{mediaDir})
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}

	// 1. Movie file
	moviePath := filepath.Join(mediaDir, "Inception (2010).mkv")
	if err := os.WriteFile(moviePath, []byte("video"), 0o644); err != nil {
		t.Fatalf("write movie file: %v", err)
	}
	movie := &sqlite.ItemRecord{
		LibraryID:       lib.ID,
		Type:            "Movie",
		Name:            "盗梦空间",
		OriginalTitle:   "Inception",
		ProductionYear:  intPtr(2010),
		CommunityRating: floatPtr(8.8),
		OfficialRating:  "PG-13",
		Path:            moviePath,
		Extra: sqlite.ItemExtra{
			Overview: "一名窃取梦境秘密的盗贼接到植入想法的逆向任务。",
			ProviderIDs: map[string]string{
				"Tmdb": "27205",
				"Imdb": "tt1375666",
			},
		},
	}
	if err := repo.UpsertItem(ctx, movie); err != nil {
		t.Fatalf("UpsertItem movie: %v", err)
	}
	if err := repo.ReplaceItemTaxonomies(ctx, movie.ID, "Genre", []string{"科幻", "动作"}); err != nil {
		t.Fatalf("ReplaceItemTaxonomies Genre: %v", err)
	}
	if err := repo.ReplaceItemTaxonomies(ctx, movie.ID, "Tag", []string{"诺兰", "烧脑"}); err != nil {
		t.Fatalf("ReplaceItemTaxonomies Tag: %v", err)
	}
	if err := repo.ReplaceItemPeople(ctx, movie.ID, []sqlite.PersonInput{
		{Name: "Leonardo DiCaprio", Role: "Dom Cobb", Type: "Actor", SortOrder: 0},
		{Name: "Christopher Nolan", Role: "Director", Type: "Director", SortOrder: 1},
	}); err != nil {
		t.Fatalf("ReplaceItemPeople: %v", err)
	}

	res, err := svc.WriteItemNFOAtomic(ctx, movie.ID)
	if err != nil {
		t.Fatalf("WriteItemNFOAtomic movie: %v", err)
	}
	expectedMovieNFO := filepath.Join(mediaDir, "Inception (2010).nfo")
	if res.NFOPath != expectedMovieNFO || res.RootTag != "movie" || res.BytesSize == 0 {
		t.Fatalf("unexpected movie NFO result: %+v", res)
	}
	// Verify no .tmp leftover
	if _, err := os.Stat(expectedMovieNFO + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("expected no .tmp file leftover, stat err=%v", err)
	}
	rawXML, err := os.ReadFile(expectedMovieNFO)
	if err != nil {
		t.Fatalf("read movie NFO: %v", err)
	}
	var parsed nfoDocument
	if err := xml.Unmarshal(rawXML, &parsed); err != nil {
		t.Fatalf("unmarshal movie NFO XML: %v\ncontent:\n%s", err, string(rawXML))
	}
	if parsed.XMLName.Local != "movie" || parsed.Title != "盗梦空间" || parsed.OriginalTitle != "Inception" {
		t.Fatalf("unexpected parsed NFO header/title: %+v", parsed)
	}
	if parsed.Year == nil || *parsed.Year != 2010 || parsed.Rating == nil || *parsed.Rating != 8.8 {
		t.Fatalf("unexpected year/rating: %+v", parsed)
	}
	if parsed.TMDBID != "27205" || parsed.IMDBID != "tt1375666" || parsed.MPAA != "PG-13" {
		t.Fatalf("unexpected ids/mpaa: %+v", parsed)
	}
	if len(parsed.Genres) != 2 || len(parsed.Tags) != 2 || len(parsed.Actors) != 1 || parsed.Actors[0].Name != "Leonardo DiCaprio" {
		t.Fatalf("unexpected taxonomies/actors: genres=%v tags=%v actors=%+v", parsed.Genres, parsed.Tags, parsed.Actors)
	}

	// 2. Series directory -> tvshow.nfo
	seriesDir := filepath.Join(mediaDir, "Breaking Bad")
	if err := os.MkdirAll(seriesDir, 0o755); err != nil {
		t.Fatalf("mkdir series: %v", err)
	}
	series := &sqlite.ItemRecord{
		LibraryID: lib.ID,
		Type:      "Series",
		IsFolder:  true,
		Name:      "绝命毒师",
		Path:      seriesDir,
	}
	if err := repo.UpsertItem(ctx, series); err != nil {
		t.Fatalf("UpsertItem series: %v", err)
	}
	seriesRes, err := svc.WriteItemNFOAtomic(ctx, series.ID)
	if err != nil {
		t.Fatalf("WriteItemNFOAtomic series: %v", err)
	}
	if seriesRes.RootTag != "tvshow" || seriesRes.NFOPath != filepath.Join(seriesDir, "tvshow.nfo") {
		t.Fatalf("unexpected series NFO result: %+v", seriesRes)
	}

	// 3. Episode file -> episodedetails
	epPath := filepath.Join(seriesDir, "S01E01.mkv")
	if err := os.WriteFile(epPath, []byte("ep"), 0o644); err != nil {
		t.Fatalf("write ep file: %v", err)
	}
	ep := &sqlite.ItemRecord{
		LibraryID: lib.ID,
		Type:      "Episode",
		Name:      "Pilot",
		Path:      epPath,
	}
	if err := repo.UpsertItem(ctx, ep); err != nil {
		t.Fatalf("UpsertItem ep: %v", err)
	}
	epRes, err := svc.WriteItemNFOAtomic(ctx, ep.ID)
	if err != nil {
		t.Fatalf("WriteItemNFOAtomic ep: %v", err)
	}
	if epRes.RootTag != "episodedetails" || epRes.NFOPath != filepath.Join(seriesDir, "S01E01.nfo") {
		t.Fatalf("unexpected episode NFO result: %+v", epRes)
	}

	// 4. Error branches: empty id, not found, empty path, missing parent dir
	if _, err := svc.WriteItemNFOAtomic(ctx, ""); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
	if _, err := svc.WriteItemNFOAtomic(ctx, "missing-id"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	noPathItem := &sqlite.ItemRecord{LibraryID: lib.ID, Type: "Movie", Name: "NoPath"}
	_ = repo.UpsertItem(ctx, noPathItem)
	if _, err := svc.WriteItemNFOAtomic(ctx, noPathItem.ID); !errors.Is(err, ErrItemPathEmpty) {
		t.Fatalf("expected ErrItemPathEmpty, got %v", err)
	}
	badDirItem := &sqlite.ItemRecord{
		LibraryID: lib.ID,
		Type:      "Movie",
		Name:      "BadDir",
		Path:      filepath.Join(mediaDir, "does-not-exist", "foo.mkv"),
	}
	_ = repo.UpsertItem(ctx, badDirItem)
	if _, err := svc.WriteItemNFOAtomic(ctx, badDirItem.ID); err == nil {
		t.Fatalf("expected error for unavailable parent dir")
	}
}

func TestScrapeAuditQueueFullFlow(t *testing.T) {
	svc, _, repo, _, _, _ := newTestEnv(t)
	ctx := context.Background()

	lib, _ := repo.CreateLibrary(ctx, "Movies", "movies", []string{"/media"})
	item1 := &sqlite.ItemRecord{
		LibraryID: lib.ID,
		Type:      "Movie",
		Name:      "Unknown.2020.1080p",
		Path:      "/media/Unknown.2020.1080p.mkv",
	}
	item2 := &sqlite.ItemRecord{
		LibraryID: lib.ID,
		Type:      "Movie",
		Name:      "Another.Film",
		Path:      "/media/Another.Film.mkv",
	}
	_ = repo.UpsertItem(ctx, item1)
	_ = repo.UpsertItem(ctx, item2)

	// Invalid enqueue
	if _, err := svc.EnqueueAudit(ctx, EnqueueAuditInput{}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}

	cand := ScrapeCandidate{
		ProviderID:    "550",
		Title:         "搏击俱乐部",
		OriginalTitle: "Fight Club",
		Year:          intPtr(1999),
		Overview:      "An insomniac office worker forms an underground fight club.",
		Rating:        floatPtr(8.4),
		Confidence:    0.62,
	}
	a1, err := svc.EnqueueAudit(ctx, EnqueueAuditInput{
		ItemID:     item1.ID,
		ItemName:   item1.Name,
		Path:       item1.Path,
		Confidence: 0.62,
		Candidates: []ScrapeCandidate{cand},
	})
	if err != nil {
		t.Fatalf("EnqueueAudit a1: %v", err)
	}
	if a1.Status != AuditStatusPending || a1.ItemType != "Movie" {
		t.Fatalf("unexpected a1: %+v", a1)
	}

	// Idempotent re-enqueue updates the existing pending record
	a1Updated, err := svc.EnqueueAudit(ctx, EnqueueAuditInput{
		ItemID:     item1.ID,
		ItemName:   item1.Name,
		ItemType:   "Movie",
		Path:       item1.Path,
		Confidence: 0.68,
		Candidates: []ScrapeCandidate{cand},
	})
	if err != nil {
		t.Fatalf("re-enqueue a1: %v", err)
	}
	if a1Updated.ID != a1.ID || a1Updated.Confidence != 0.68 {
		t.Fatalf("expected idempotent update of pending audit, got %+v", a1Updated)
	}

	a2, err := svc.EnqueueAudit(ctx, EnqueueAuditInput{
		ItemID:     item2.ID,
		ItemName:   item2.Name,
		ItemType:   "Movie",
		Path:       item2.Path,
		Confidence: 0.45,
	})
	if err != nil {
		t.Fatalf("EnqueueAudit a2: %v", err)
	}

	pending, err := svc.ListAudits(ctx, AuditStatusPending)
	if err != nil || len(pending) != 2 {
		t.Fatalf("expected 2 pending audits, got %d err=%v", len(pending), err)
	}

	// Approve a1 with lock fields
	approved, err := svc.ApproveAudit(ctx, a1.ID, cand, []string{"Name", "Overview", "ProviderIds"})
	if err != nil {
		t.Fatalf("ApproveAudit: %v", err)
	}
	if approved.Status != AuditStatusApproved {
		t.Fatalf("expected status approved, got %s", approved.Status)
	}

	// Verify item1 metadata & locked fields updated
	var gotName, gotOrig, gotExtra string
	var gotYear int
	var gotRating float64
	if err := svc.store.Reader.QueryRowContext(ctx,
		`SELECT name, original_title, production_year, community_rating, extra_json FROM items WHERE id = ?`,
		item1.ID,
	).Scan(&gotName, &gotOrig, &gotYear, &gotRating, &gotExtra); err != nil {
		t.Fatalf("query item1: %v", err)
	}
	if gotName != "搏击俱乐部" || gotOrig != "Fight Club" || gotYear != 1999 || gotRating != 8.4 {
		t.Fatalf("item1 metadata not applied: name=%s orig=%s year=%d rating=%v", gotName, gotOrig, gotYear, gotRating)
	}
	extra := decodeExtraMap(gotExtra)
	if extractProviderIDs(extra)["Tmdb"] != "550" {
		t.Fatalf("expected Tmdb=550, got %v", extra["provider_ids"])
	}
	locked := extractLockedFields(extra)
	if len(locked) != 3 {
		t.Fatalf("expected 3 locked fields, got %v", locked)
	}

	// Dismiss a2
	dismissed, err := svc.DismissAudit(ctx, a2.ID)
	if err != nil {
		t.Fatalf("DismissAudit: %v", err)
	}
	if dismissed.Status != AuditStatusDismissed {
		t.Fatalf("expected dismissed, got %s", dismissed.Status)
	}

	// List all vs pending
	all, err := svc.ListAudits(ctx, "all")
	if err != nil || len(all) != 2 {
		t.Fatalf("expected 2 total audits, got %d err=%v", len(all), err)
	}
	pendingAfter, err := svc.ListAudits(ctx, AuditStatusPending)
	if err != nil || len(pendingAfter) != 0 {
		t.Fatalf("expected 0 pending audits, got %d err=%v", len(pendingAfter), err)
	}

	// Error cases
	if _, err := svc.ApproveAudit(ctx, "", cand, nil); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
	if _, err := svc.ApproveAudit(ctx, "missing", cand, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if _, err := svc.DismissAudit(ctx, ""); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
	if _, err := svc.DismissAudit(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestRunLibraryAuditAndCleanupOrphans(t *testing.T) {
	svc, _, repo, _, _, _ := newTestEnv(t)
	ctx := context.Background()

	mediaDir := t.TempDir()
	lib, _ := repo.CreateLibrary(ctx, "Movies", "movies", []string{mediaDir})

	// 1. Healthy movie: exists on disk, has Primary image, has TMDB ID
	okPath := filepath.Join(mediaDir, "healthy.mkv")
	_ = os.WriteFile(okPath, []byte("ok"), 0o644)
	healthyPoster := filepath.Join(mediaDir, "poster.jpg")
	_ = os.WriteFile(healthyPoster, []byte("jpg"), 0o644)
	healthy := &sqlite.ItemRecord{
		LibraryID: lib.ID,
		Type:      "Movie",
		Name:      "Healthy Movie",
		Path:      okPath,
		Extra:     sqlite.ItemExtra{ProviderIDs: map[string]string{"Tmdb": "100"}},
	}
	_ = repo.UpsertItem(ctx, healthy)
	_ = repo.ReplaceItemImages(ctx, healthy.ID, []sqlite.ImageRecord{
		{ItemID: healthy.ID, ImageType: "Primary", Path: healthyPoster},
	})

	// 2. Missing poster + missing TMDB, exists on disk
	missPath := filepath.Join(mediaDir, "unscraped.mkv")
	_ = os.WriteFile(missPath, []byte("raw"), 0o644)
	unscraped := &sqlite.ItemRecord{
		LibraryID:      lib.ID,
		Type:           "Movie",
		Name:           "Unscraped Movie",
		ProductionYear: intPtr(2023),
		Path:           missPath,
	}
	_ = repo.UpsertItem(ctx, unscraped)

	// 3. Orphan non-STRM movie (file deleted on disk)
	orphanPoster := filepath.Join(mediaDir, "orphan-poster.jpg")
	_ = os.WriteFile(orphanPoster, []byte("jpg"), 0o644)
	orphan := &sqlite.ItemRecord{
		LibraryID: lib.ID,
		Type:      "Movie",
		Name:      "Ghost Movie",
		Path:      filepath.Join(mediaDir, "deleted-file.mkv"),
		Extra:     sqlite.ItemExtra{ProviderIDs: map[string]string{"Tmdb": "200"}},
	}
	_ = repo.UpsertItem(ctx, orphan)
	_ = repo.ReplaceItemImages(ctx, orphan.ID, []sqlite.ImageRecord{
		{ItemID: orphan.ID, ImageType: "Primary", Path: orphanPoster},
	})
	_ = repo.ReplaceItemAncestors(ctx, orphan.ID, []sqlite.AncestorLink{{AncestorID: lib.ID, Depth: 1}})
	_ = repo.UpsertSearchFTS(ctx, orphan.ID, orphan.Name, "", "", "", "")

	// 4. STRM item whose path doesn't exist locally -> must NOT be flagged as orphan
	strmPoster := filepath.Join(mediaDir, "strm-poster.jpg")
	_ = os.WriteFile(strmPoster, []byte("jpg"), 0o644)
	strmItem := &sqlite.ItemRecord{
		LibraryID: lib.ID,
		Type:      "Movie",
		IsStrm:    true,
		Name:      "Cloud STRM Movie",
		Path:      filepath.Join(mediaDir, "remote.strm"),
		Extra:     sqlite.ItemExtra{ProviderIDs: map[string]string{"Tmdb": "300"}},
	}
	_ = repo.UpsertItem(ctx, strmItem)
	_ = repo.ReplaceItemImages(ctx, strmItem.ID, []sqlite.ImageRecord{
		{ItemID: strmItem.ID, ImageType: "Primary", Path: strmPoster},
	})

	report, err := svc.RunLibraryAudit(ctx)
	if err != nil {
		t.Fatalf("RunLibraryAudit: %v", err)
	}
	if report.TotalChecked != 4 {
		t.Fatalf("expected 4 total checked, got %d", report.TotalChecked)
	}
	if len(report.MissingPosters) != 1 || report.MissingPosters[0].ID != unscraped.ID {
		t.Fatalf("unexpected MissingPosters: %+v", report.MissingPosters)
	}
	if len(report.MissingTMDB) != 1 || report.MissingTMDB[0].ID != unscraped.ID {
		t.Fatalf("unexpected MissingTMDB: %+v", report.MissingTMDB)
	}
	if len(report.OrphanItems) != 1 || report.OrphanItems[0].ID != orphan.ID {
		t.Fatalf("unexpected OrphanItems: %+v", report.OrphanItems)
	}

	// Cleanup empty list
	emptyRes, err := svc.CleanupOrphans(ctx, nil)
	if err != nil || emptyRes.Deleted != 0 {
		t.Fatalf("expected 0 deleted for empty list, got %+v err=%v", emptyRes, err)
	}

	// Cleanup orphan + duplicate + nonexistent
	cleanRes, err := svc.CleanupOrphans(ctx, []string{orphan.ID, orphan.ID, "nonexistent", ""})
	if err != nil {
		t.Fatalf("CleanupOrphans: %v", err)
	}
	if cleanRes.Requested != 2 || cleanRes.Deleted != 1 || len(cleanRes.DeletedIDs) != 1 || cleanRes.DeletedIDs[0] != orphan.ID {
		t.Fatalf("unexpected cleanup result: %+v", cleanRes)
	}

	// Verify removed from items, item_ancestors, item_search_fts
	var cnt int
	_ = svc.store.Reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM items WHERE id = ?`, orphan.ID).Scan(&cnt)
	if cnt != 0 {
		t.Fatalf("orphan still exists in items")
	}
	_ = svc.store.Reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM item_ancestors WHERE descendant_id = ?`, orphan.ID).Scan(&cnt)
	if cnt != 0 {
		t.Fatalf("orphan still exists in item_ancestors")
	}
	_ = svc.store.Reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM item_search_fts WHERE item_id = ?`, orphan.ID).Scan(&cnt)
	if cnt != 0 {
		t.Fatalf("orphan still exists in item_search_fts")
	}
}

func TestCollectHealth(t *testing.T) {
	svc, _, repo, cfg, sched, pool := newTestEnv(t)
	ctx := context.Background()

	// Seed cache files so CacheHealth is non-zero
	_ = os.WriteFile(filepath.Join(cfg.ImageCacheDir(), "img1.jpg"), make([]byte, 1024), 0o644)
	_ = os.WriteFile(filepath.Join(cfg.SubtitleCacheDir(), "sub1.vtt"), make([]byte, 512), 0o644)
	_ = os.WriteFile(filepath.Join(cfg.TranscodeTempDir(), "seg0.ts"), make([]byte, 2048), 0o644)

	// Seed items and pending audit
	lib, _ := repo.CreateLibrary(ctx, "All", "movies", []string{t.TempDir()})
	m := &sqlite.ItemRecord{LibraryID: lib.ID, Type: "Movie", Name: "M1"}
	s := &sqlite.ItemRecord{LibraryID: lib.ID, Type: "Series", Name: "S1"}
	e := &sqlite.ItemRecord{LibraryID: lib.ID, Type: "Episode", Name: "E1"}
	_ = repo.UpsertItem(ctx, m)
	_ = repo.UpsertItem(ctx, s)
	_ = repo.UpsertItem(ctx, e)
	_, _ = svc.EnqueueAudit(ctx, EnqueueAuditInput{ItemID: m.ID, ItemName: m.Name})

	// Seed a running scheduled task and acquire budget weight
	blockCh := make(chan struct{})
	sched.Register(scheduler.TaskDefinition{
		ID:   "LongTask",
		Name: "LongTask",
		Run: func(ctx context.Context, _ func(float64)) error {
			select {
			case <-blockCh:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	})
	if err := sched.Run("LongTask"); err != nil {
		t.Fatalf("sched.Run: %v", err)
	}
	defer func() {
		close(blockCh)
		sched.Wait("LongTask")
	}()
	time.Sleep(15 * time.Millisecond)

	if !pool.TryAcquire(2) {
		t.Fatalf("expected pool.TryAcquire(2) to succeed")
	}
	defer pool.Release(2)

	health, err := svc.CollectHealth(ctx)
	if err != nil {
		t.Fatalf("CollectHealth: %v", err)
	}
	if health.NumGoroutine <= 0 || health.NumCPU <= 0 || health.Memory.SysBytes == 0 {
		t.Fatalf("expected valid runtime metrics: %+v", health)
	}
	if health.Database.DBBytes <= 0 || health.Database.TotalBytes < health.Database.DBBytes {
		t.Fatalf("expected valid database size: %+v", health.Database)
	}
	if health.Cache.ImagesBytes != 1024 || health.Cache.SubtitlesBytes != 512 || health.Cache.TranscodeBytes != 2048 || health.Cache.TotalBytes != 3584 {
		t.Fatalf("unexpected cache bytes: %+v", health.Cache)
	}
	if health.Disk.TotalBytes == 0 {
		t.Fatalf("expected non-zero disk total bytes: %+v", health.Disk)
	}
	if health.Library.TotalItems != 3 || health.Library.MovieCount != 1 || health.Library.SeriesCount != 1 || health.Library.EpisodeCount != 1 || health.Library.PendingAudits != 1 {
		t.Fatalf("unexpected library stats: %+v", health.Library)
	}
	if health.Scheduler.TotalTasks != 1 || health.Scheduler.ActiveTasks != 1 {
		t.Fatalf("unexpected scheduler stats: %+v", health.Scheduler)
	}
	if health.Scheduler.BudgetInUse != 2 || health.Scheduler.BudgetCapacity != 4 || health.Scheduler.BudgetUsagePct != 50 {
		t.Fatalf("unexpected budget stats: %+v", health.Scheduler)
	}
}

func TestHelpersEdgeCases(t *testing.T) {
	if replaceExtWithNFO("/media/noext") != "/media/noext.nfo" {
		t.Fatalf("unexpected replaceExtWithNFO without ext")
	}
	if fileSizeOrZero("") != 0 || fileSizeOrZero("/definitely/missing/file") != 0 {
		t.Fatalf("expected 0 for missing file")
	}
	if dirSizeBytes("") != 0 || dirSizeBytes("/definitely/missing/dir") != 0 {
		t.Fatalf("expected 0 for missing dir")
	}
	dh := collectDiskHealth("")
	if dh.TotalBytes != 0 {
		t.Fatalf("expected zero disk health for empty path")
	}
	dhBad := collectDiskHealth("/definitely/missing/mount/path/xyz")
	if dhBad.TotalBytes != 0 {
		t.Fatalf("expected zero disk health for invalid path")
	}

	// Series non-folder path & Movie folder path
	dir := t.TempDir()
	tag, p, err := resolveNFOTarget("Series", false, filepath.Join(dir, "show.mkv"))
	if err != nil || tag != "tvshow" || p != filepath.Join(dir, "tvshow.nfo") {
		t.Fatalf("unexpected series file target: %s %s err=%v", tag, p, err)
	}
	tag, p, err = resolveNFOTarget("Movie", true, dir)
	if err != nil || tag != "movie" || p != filepath.Join(dir, "movie.nfo") {
		t.Fatalf("unexpected movie folder target: %s %s err=%v", tag, p, err)
	}

	// extractProviderIDs & lookupProviderID case-insensitive
	m := map[string]any{"provider_ids": map[string]string{"tmdb": "999"}}
	pids := extractProviderIDs(m)
	if lookupProviderID(pids, "Tmdb") != "999" {
		t.Fatalf("expected case-insensitive lookup to return 999")
	}
	if len(extractLockedFields(map[string]any{"LockedFields": []string{"Name"}})) != 1 {
		t.Fatalf("expected string slice extraction")
	}
	if len(extractLockedFields(map[string]any{"LockedFields": 123})) != 0 {
		t.Fatalf("expected empty slice for non-slice LockedFields")
	}
	if len(decodeExtraMap("null")) != 0 {
		t.Fatalf("expected empty map for null json")
	}
}

func TestErrorPathsAndClosedStore(t *testing.T) {
	svc, store, repo, _, _, _ := newTestEnv(t)
	ctx := context.Background()

	// 1. ApproveAudit when underlying item was deleted -> ErrNotFound
	lib, _ := repo.CreateLibrary(ctx, "Movies", "movies", []string{t.TempDir()})
	ghost := &sqlite.ItemRecord{LibraryID: lib.ID, Type: "Movie", Name: "Ghost"}
	_ = repo.UpsertItem(ctx, ghost)
	aud, err := svc.EnqueueAudit(ctx, EnqueueAuditInput{ItemID: ghost.ID, ItemName: ghost.Name})
	if err != nil {
		t.Fatalf("EnqueueAudit: %v", err)
	}
	_, _ = store.Writer.ExecContext(ctx, `DELETE FROM items WHERE id = ?`, ghost.ID)
	if _, err := svc.ApproveAudit(ctx, aud.ID, ScrapeCandidate{Title: "X"}, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound when item deleted, got %v", err)
	}

	// 2. atomicWriteFile failure on non-existent directory
	if err := atomicWriteFile(filepath.Join(t.TempDir(), "no-such-dir", "a.nfo"), []byte("x")); err == nil {
		t.Fatalf("expected error from atomicWriteFile in missing dir")
	}
	// atomicWriteFile failure when target is a directory (rename fails and .tmp is cleaned up)
	dirTarget := filepath.Join(t.TempDir(), "dir.nfo")
	_ = os.MkdirAll(dirTarget, 0o755)
	if err := atomicWriteFile(dirTarget, []byte("x")); err == nil {
		t.Fatalf("expected rename error when target is directory")
	}
	if _, err := os.Stat(dirTarget + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("expected .tmp cleaned up after rename failure")
	}

	// 3. Closed store triggers DB error branches cleanly across all methods
	_ = store.Close()

	if _, err := NewService(Options{Store: store}); err == nil {
		t.Fatalf("expected NewService error on closed store")
	}
	if _, err := svc.SetLockedFields(ctx, "id", []string{"Name"}); err == nil {
		t.Fatalf("expected SetLockedFields error on closed store")
	}
	if _, err := svc.GetLockedFields(ctx, "id"); err == nil {
		t.Fatalf("expected GetLockedFields error on closed store")
	}
	if _, err := svc.WriteItemNFOAtomic(ctx, "id"); err == nil {
		t.Fatalf("expected WriteItemNFOAtomic error on closed store")
	}
	if _, err := svc.EnqueueAudit(ctx, EnqueueAuditInput{ItemID: "id", ItemName: "n"}); err == nil {
		t.Fatalf("expected EnqueueAudit error on closed store")
	}
	if _, err := svc.ListAudits(ctx, "pending"); err == nil {
		t.Fatalf("expected ListAudits error on closed store")
	}
	if _, err := svc.ApproveAudit(ctx, "id", ScrapeCandidate{}, nil); err == nil {
		t.Fatalf("expected ApproveAudit error on closed store")
	}
	if _, err := svc.DismissAudit(ctx, "id"); err == nil {
		t.Fatalf("expected DismissAudit error on closed store")
	}
	if _, err := svc.RunLibraryAudit(ctx); err == nil {
		t.Fatalf("expected RunLibraryAudit error on closed store")
	}
	if _, err := svc.CleanupOrphans(ctx, []string{"id"}); err == nil {
		t.Fatalf("expected CleanupOrphans error on closed store")
	}
	if _, err := svc.CollectHealth(ctx); err == nil {
		t.Fatalf("expected CollectHealth error on closed store")
	}
}

func TestGovernanceSecurityHardeningAndAuditEdges(t *testing.T) {
	svc, store, repo, _, _, _ := newTestEnv(t)
	ctx := context.Background()

	mediaDir := t.TempDir()
	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{mediaDir})
	if err != nil {
		t.Fatal(err)
	}

	// 1. isVirtualPath coverage
	for _, vp := range []string{"", "  ", "artist://beatles", "album://abbey-road", "smb://nas/share/a.mkv", "/media/Show#Season1"} {
		if !isVirtualPath(vp) {
			t.Fatalf("expected %q to be treated as virtual path", vp)
		}
	}
	if isVirtualPath("/media/Movies/Inception.mkv") {
		t.Fatal("expected normal file path not to be virtual")
	}

	// 2. CleanupOrphans skips existing physical files, STRM items, folders, and virtual paths
	existingFile := filepath.Join(mediaDir, "still-here.mkv")
	if err := os.WriteFile(existingFile, []byte("present"), 0o644); err != nil {
		t.Fatal(err)
	}
	existingItem := &sqlite.ItemRecord{LibraryID: lib.ID, Type: "Movie", Name: "Present", Path: existingFile}
	folderItem := &sqlite.ItemRecord{LibraryID: lib.ID, Type: "Series", IsFolder: true, Name: "Folder", Path: filepath.Join(mediaDir, "missing-dir")}
	virtualItem := &sqlite.ItemRecord{LibraryID: lib.ID, Type: "MusicArtist", Name: "Artist", Path: "artist://123"}
	realOrphan := &sqlite.ItemRecord{LibraryID: lib.ID, Type: "Movie", Name: "RealOrphan", Path: filepath.Join(mediaDir, "gone.mkv")}
	for _, it := range []*sqlite.ItemRecord{existingItem, folderItem, virtualItem, realOrphan} {
		if err := repo.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	_, _ = svc.EnqueueAudit(ctx, EnqueueAuditInput{ItemID: realOrphan.ID, ItemName: realOrphan.Name})

	cleanRes, err := svc.CleanupOrphans(ctx, []string{existingItem.ID, folderItem.ID, virtualItem.ID, realOrphan.ID})
	if err != nil {
		t.Fatalf("CleanupOrphans: %v", err)
	}
	if cleanRes.Requested != 4 || cleanRes.Deleted != 1 || cleanRes.DeletedIDs[0] != realOrphan.ID {
		t.Fatalf("expected only realOrphan deleted, got %+v", cleanRes)
	}

	// 3. ApproveAudit: empty candidate, non-pending audit, custom ProviderName, Genres/Tags taxonomy replacement & FTS sync
	movie := &sqlite.ItemRecord{
		LibraryID: lib.ID,
		Type:      "Movie",
		Name:      "Raw File",
		Path:      existingFile,
	}
	if err := repo.UpsertItem(ctx, movie); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceItemPeople(ctx, movie.ID, []sqlite.PersonInput{
		{Name: "Actor One", Type: "Actor"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertSearchFTS(ctx, movie.ID, "Raw File", "", "rawfile", "rf", "Actor One"); err != nil {
		t.Fatal(err)
	}
	// Pre-seed one existing genre taxonomy to cover both existing-taxonomy lookup and new-taxonomy insert paths.
	if err := repo.ReplaceItemTaxonomies(ctx, movie.ID, "genre", []string{"Sci-Fi", "OldGenre"}); err != nil {
		t.Fatal(err)
	}

	aud, err := svc.EnqueueAudit(ctx, EnqueueAuditInput{ItemID: movie.ID, ItemName: movie.Name, Path: movie.Path})
	if err != nil {
		t.Fatal(err)
	}

	// Empty candidate must be rejected
	if _, err := svc.ApproveAudit(ctx, aud.ID, ScrapeCandidate{}, nil); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for empty candidate, got %v", err)
	}

	cand := ScrapeCandidate{
		ProviderName:  "Imdb",
		ProviderID:    "tt0133093",
		Title:         "黑客帝国",
		OriginalTitle: "The Matrix",
		Genres:        []string{"Sci-Fi", "  ", "Sci-Fi", "Action"},
		Tags:          []string{"Cyberpunk", "Classic", "Cyberpunk"},
	}
	approved, err := svc.ApproveAudit(ctx, aud.ID, cand, []string{"Name"})
	if err != nil {
		t.Fatalf("ApproveAudit with taxonomies failed: %v", err)
	}
	if approved.Status != AuditStatusApproved {
		t.Fatalf("expected approved status, got %s", approved.Status)
	}

	// Re-approving a non-pending (already approved) audit must fail
	if _, err := svc.ApproveAudit(ctx, aud.ID, cand, nil); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for non-pending audit, got %v", err)
	}

	// Verify genres, tags, lowercase taxonomy kind, sort_key, custom ProviderName, and updated FTS pinyin/tokens
	genres, _, tags, err := svc.loadItemTaxonomies(ctx, movie.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(genres) != 2 || len(tags) != 2 {
		t.Fatalf("expected 2 genres and 2 tags after taxonomy replace, got genres=%v tags=%v", genres, tags)
	}
	// Verify taxonomies.kind is strictly lowercase ("genre"/"tag")
	var badKindCount int
	if err := store.Reader.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM taxonomies t JOIN item_taxonomies it ON it.taxonomy_id = t.id WHERE it.item_id = ? AND t.kind NOT IN ('genre', 'tag')`,
		movie.ID,
	).Scan(&badKindCount); err != nil {
		t.Fatal(err)
	}
	if badKindCount != 0 {
		t.Fatalf("expected only lowercase genre/tag taxonomy kinds, found %d non-lowercase rows", badKindCount)
	}

	var sortKey string
	if err := store.Reader.QueryRowContext(ctx, `SELECT sort_key FROM items WHERE id = ?`, movie.ID).Scan(&sortKey); err != nil {
		t.Fatal(err)
	}
	if sortKey != sqlite.PinyinSortKey("黑客帝国") {
		t.Fatalf("expected sort_key=%q, got %q", sqlite.PinyinSortKey("黑客帝国"), sortKey)
	}

	var ftsTitle, ftsTokens, ftsPinyin, ftsInit, ftsPeople string
	if err := store.Reader.QueryRowContext(ctx,
		`SELECT title_raw, title_tokens, pinyin_full, pinyin_initials, people_names FROM item_search_fts WHERE item_id = ?`, movie.ID,
	).Scan(&ftsTitle, &ftsTokens, &ftsPinyin, &ftsInit, &ftsPeople); err != nil {
		t.Fatalf("query item_search_fts: %v", err)
	}
	if ftsTitle != "黑客帝国 The Matrix" || !strings.Contains(ftsTokens, "黑客") || !strings.Contains(ftsPinyin, "heikediguo") || !strings.Contains(ftsInit, "hkdg") || ftsPeople != "actor one" {
		t.Fatalf("unexpected FTS row after ApproveAudit: title=%q tokens=%q pinyin=%q init=%q people=%q", ftsTitle, ftsTokens, ftsPinyin, ftsInit, ftsPeople)
	}
	var extraRaw string
	_ = store.Reader.QueryRowContext(ctx, `SELECT extra_json FROM items WHERE id = ?`, movie.ID).Scan(&extraRaw)
	if extractProviderIDs(decodeExtraMap(extraRaw))["Imdb"] != "tt0133093" {
		t.Fatalf("expected custom ProviderName Imdb saved, got %s", extraRaw)
	}

	// 4. WriteItemNFOAtomic & atomicWriteFile refuse symlink target without modifying link destination
	secretFile := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secretFile, []byte("DO-NOT-TOUCH"), 0o644); err != nil {
		t.Fatal(err)
	}
	symlinkNFO := filepath.Join(mediaDir, "still-here.nfo")
	if err := os.Symlink(secretFile, symlinkNFO); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.WriteItemNFOAtomic(ctx, movie.ID); err == nil {
		t.Fatal("expected WriteItemNFOAtomic to refuse symlink target")
	}
	secretData, err := os.ReadFile(secretFile)
	if err != nil || string(secretData) != "DO-NOT-TOUCH" {
		t.Fatalf("symlink target file was modified: data=%q err=%v", string(secretData), err)
	}

	// 5. SetLockState with fields==nil and lockData!=nil preserves existing LockedFields
	lockTrue := true
	if _, err := svc.SetLockState(ctx, movie.ID, []string{"Overview"}, &lockTrue); err != nil {
		t.Fatal(err)
	}
	lockFalse := false
	resKeep, err := svc.SetLockState(ctx, movie.ID, nil, &lockFalse)
	if err != nil {
		t.Fatal(err)
	}
	if resKeep.LockData || len(resKeep.LockedFields) != 1 || resKeep.LockedFields[0] != "Overview" {
		t.Fatalf("expected LockedFields preserved when fields==nil && lockData!=nil, got %+v", resKeep)
	}
	if extractLockData(map[string]any{"LockData": "not-a-bool"}) {
		t.Fatal("expected false for non-bool LockData")
	}
}

func TestGovernanceRollbackAndErrorInjectionBranches(t *testing.T) {
	ctx := context.Background()

	// 1. Null candidates_json fallback across ListAudits, ApproveAudit, and DismissAudit
	svc, store, repo, _, _, _ := newTestEnv(t)
	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	m1 := &sqlite.ItemRecord{LibraryID: lib.ID, Type: "Movie", Name: "NullCand1", Path: filepath.Join(t.TempDir(), "m1.mkv")}
	m2 := &sqlite.ItemRecord{LibraryID: lib.ID, Type: "Movie", Name: "NullCand2", Path: filepath.Join(t.TempDir(), "m2.mkv")}
	_ = repo.UpsertItem(ctx, m1)
	_ = repo.UpsertItem(ctx, m2)
	a1, _ := svc.EnqueueAudit(ctx, EnqueueAuditInput{ItemID: m1.ID, ItemName: m1.Name})
	a2, _ := svc.EnqueueAudit(ctx, EnqueueAuditInput{ItemID: m2.ID, ItemName: m2.Name})
	_, _ = store.Writer.ExecContext(ctx, `UPDATE scrape_audit_queue SET candidates_json = 'null'`)

	listed, err := svc.ListAudits(ctx, "pending")
	if err != nil || len(listed) != 2 || listed[0].Candidates == nil {
		t.Fatalf("expected non-nil empty Candidates slice from ListAudits, got %+v err=%v", listed, err)
	}
	if app, err := svc.ApproveAudit(ctx, a1.ID, ScrapeCandidate{Title: "OK"}, nil); err != nil || app.Candidates == nil {
		t.Fatalf("expected ApproveAudit with null candidates_json to succeed, got %+v err=%v", app, err)
	}
	if dis, err := svc.DismissAudit(ctx, a2.ID); err != nil || dis.Candidates == nil {
		t.Fatalf("expected DismissAudit with null candidates_json to succeed, got %+v err=%v", dis, err)
	}

	// 2. EnqueueAudit JSON marshal error via NaN confidence in candidate
	if _, err := svc.EnqueueAudit(ctx, EnqueueAuditInput{
		ItemID:     m1.ID,
		ItemName:   m1.Name,
		Candidates: []ScrapeCandidate{{Title: "Bad", Confidence: math.NaN()}},
	}); err == nil {
		t.Fatal("expected JSON marshal error for NaN candidate confidence")
	}

	// 3. SQLite trigger-injected write failures verify transaction rollbacks across ApproveAudit, EnqueueAudit, DismissAudit, SetLockState, CleanupOrphans
	m3 := &sqlite.ItemRecord{LibraryID: lib.ID, Type: "Movie", Name: "RollbackMovie", Path: filepath.Join(t.TempDir(), "gone.mkv")}
	_ = repo.UpsertItem(ctx, m3)
	_ = repo.ReplaceItemAncestors(ctx, m3.ID, []sqlite.AncestorLink{{AncestorID: lib.ID, Depth: 1}})
	a3, _ := svc.EnqueueAudit(ctx, EnqueueAuditInput{ItemID: m3.ID, ItemName: m3.Name})

	mustExec := func(q string) {
		t.Helper()
		if _, err := store.Writer.ExecContext(ctx, q); err != nil {
			t.Fatalf("exec %q: %v", q, err)
		}
	}

	// 3a. SetLockState update failure & ApproveAudit update items failure
	mustExec(`CREATE TRIGGER block_items_upd BEFORE UPDATE ON items BEGIN SELECT RAISE(ABORT, 'block upd'); END;`)
	if _, err := svc.SetLockState(ctx, m3.ID, []string{"Name"}, nil); err == nil {
		t.Fatal("expected SetLockState error under items update trigger")
	}
	if _, err := svc.ApproveAudit(ctx, a3.ID, ScrapeCandidate{Title: "New"}, nil); err == nil {
		t.Fatal("expected ApproveAudit error under items update trigger")
	}
	mustExec(`DROP TRIGGER block_items_upd;`)

	// 3b. ApproveAudit taxonomy insert failure & item_taxonomies insert failure
	mustExec(`CREATE TRIGGER block_tax_ins BEFORE INSERT ON taxonomies BEGIN SELECT RAISE(ABORT, 'block tax'); END;`)
	if _, err := svc.ApproveAudit(ctx, a3.ID, ScrapeCandidate{Title: "New", Genres: []string{"BrandNewGenre"}}, nil); err == nil {
		t.Fatal("expected ApproveAudit error on genre taxonomy insert failure")
	}
	if _, err := svc.ApproveAudit(ctx, a3.ID, ScrapeCandidate{Title: "New", Tags: []string{"BrandNewTag"}}, nil); err == nil {
		t.Fatal("expected ApproveAudit error on tag taxonomy insert failure")
	}
	mustExec(`DROP TRIGGER block_tax_ins;`)

	mustExec(`CREATE TRIGGER block_itax_ins BEFORE INSERT ON item_taxonomies BEGIN SELECT RAISE(ABORT, 'block itax'); END;`)
	if _, err := svc.ApproveAudit(ctx, a3.ID, ScrapeCandidate{Title: "New", Genres: []string{"G1"}}, nil); err == nil {
		t.Fatal("expected ApproveAudit error on item_taxonomies insert failure")
	}
	mustExec(`DROP TRIGGER block_itax_ins;`)

	// 3c. CleanupOrphans delete failures on item_ancestors, scrape_audit_queue, items
	mustExec(`CREATE TRIGGER block_anc_del BEFORE DELETE ON item_ancestors BEGIN SELECT RAISE(ABORT, 'block anc'); END;`)
	if _, err := svc.CleanupOrphans(ctx, []string{m3.ID}); err == nil {
		t.Fatal("expected CleanupOrphans error on item_ancestors delete failure")
	}
	mustExec(`DROP TRIGGER block_anc_del;`)

	mustExec(`CREATE TRIGGER block_aud_del BEFORE DELETE ON scrape_audit_queue BEGIN SELECT RAISE(ABORT, 'block aud del'); END;`)
	if _, err := svc.CleanupOrphans(ctx, []string{m3.ID}); err == nil {
		t.Fatal("expected CleanupOrphans error on scrape_audit_queue delete failure")
	}
	mustExec(`DROP TRIGGER block_aud_del;`)

	mustExec(`CREATE TRIGGER block_items_del BEFORE DELETE ON items BEGIN SELECT RAISE(ABORT, 'block items del'); END;`)
	if _, err := svc.CleanupOrphans(ctx, []string{m3.ID}); err == nil {
		t.Fatal("expected CleanupOrphans error on items delete failure")
	}
	mustExec(`DROP TRIGGER block_items_del;`)

	// 3d. scrape_audit_queue update & insert failures
	mustExec(`CREATE TRIGGER block_aud_upd BEFORE UPDATE ON scrape_audit_queue BEGIN SELECT RAISE(ABORT, 'block aud upd'); END;`)
	if _, err := svc.EnqueueAudit(ctx, EnqueueAuditInput{ItemID: m3.ID, ItemName: "Updated"}); err == nil {
		t.Fatal("expected EnqueueAudit update error under trigger")
	}
	if _, err := svc.ApproveAudit(ctx, a3.ID, ScrapeCandidate{Title: "New"}, nil); err == nil {
		t.Fatal("expected ApproveAudit status update error under trigger")
	}
	if _, err := svc.DismissAudit(ctx, a3.ID); err == nil {
		t.Fatal("expected DismissAudit update error under trigger")
	}
	mustExec(`DROP TRIGGER block_aud_upd;`)

	mustExec(`CREATE TRIGGER block_aud_ins BEFORE INSERT ON scrape_audit_queue BEGIN SELECT RAISE(ABORT, 'block aud ins'); END;`)
	if _, err := svc.EnqueueAudit(ctx, EnqueueAuditInput{ItemID: "brand-new-item", ItemName: "X"}); err == nil {
		t.Fatal("expected EnqueueAudit insert error under trigger")
	}
	mustExec(`DROP TRIGGER block_aud_ins;`)

	// 4. Schema drop error paths for FTS, WriteItemNFOAtomic, ApproveAudit, RunLibraryAudit, collectLibraryStats, and closed store
	mustExec(`PRAGMA foreign_keys = OFF;`)
	nfoDir := t.TempDir()
	m4 := &sqlite.ItemRecord{LibraryID: lib.ID, Type: "Movie", Name: "NFOErr", Path: filepath.Join(nfoDir, "m4.mkv")}
	_ = repo.UpsertItem(ctx, m4)

	// Drop virtual FTS table to cover FTS delete error in both ApproveAudit and CleanupOrphans
	mustExec(`DROP TABLE item_search_fts;`)
	if _, err := svc.ApproveAudit(ctx, a3.ID, ScrapeCandidate{Title: "New"}, nil); err == nil {
		t.Fatal("expected ApproveAudit error when item_search_fts table missing")
	}
	if _, err := svc.CleanupOrphans(ctx, []string{m3.ID}); err == nil {
		t.Fatal("expected CleanupOrphans error when item_search_fts table missing")
	}

	mustExec(`DROP TABLE item_people;`)
	if _, err := svc.WriteItemNFOAtomic(ctx, m4.ID); err == nil {
		t.Fatal("expected WriteItemNFOAtomic error when item_people table missing")
	}
	mustExec(`DROP TABLE item_taxonomies;`)
	if _, err := svc.WriteItemNFOAtomic(ctx, m4.ID); err == nil {
		t.Fatal("expected WriteItemNFOAtomic error when item_taxonomies table missing")
	}
	if _, err := svc.ApproveAudit(ctx, a3.ID, ScrapeCandidate{Title: "X", Genres: []string{"Drama"}}, nil); err == nil {
		t.Fatal("expected ApproveAudit error when item_taxonomies table missing")
	}
	mustExec(`DROP TABLE scrape_audit_queue;`)
	if _, err := svc.CollectHealth(ctx); err == nil {
		t.Fatal("expected CollectHealth error when scrape_audit_queue table missing")
	}
	mustExec(`DROP TABLE users;`)
	if _, err := svc.CollectHealth(ctx); err == nil {
		t.Fatal("expected CollectHealth error when users table missing")
	}
	mustExec(`DROP TABLE items;`)
	if _, err := svc.RunLibraryAudit(ctx); err == nil {
		t.Fatal("expected RunLibraryAudit error when items table missing")
	}

	// Non-empty candidate on closed store hits BeginTx error in ApproveAudit
	_ = store.Close()
	if _, err := svc.ApproveAudit(ctx, "any-id", ScrapeCandidate{Title: "NonEmpty"}, nil); err == nil {
		t.Fatal("expected ApproveAudit BeginTx error on closed store")
	}
}
