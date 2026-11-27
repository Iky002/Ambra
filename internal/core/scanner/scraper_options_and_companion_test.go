package scanner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/plugins/nfo"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

type mockCustomScraper struct {
	id       string
	name     string
	priority int
	enabled  bool
	resName  string
	calls    int
}

func (m *mockCustomScraper) Manifest() plugins.Manifest {
	return plugins.Manifest{
		ID:       m.id,
		Name:     m.name,
		Version:  "1.0.0",
		Enabled:  m.enabled,
		Priority: m.priority,
		Kinds:    []plugins.Kind{plugins.KindMetadata},
	}
}

func (m *mockCustomScraper) Configure(cfg map[string]any) error { return nil }

func (m *mockCustomScraper) FetchMetadata(ctx context.Context, q plugins.MediaQuery) (*plugins.MetadataResult, error) {
	m.calls++
	return &plugins.MetadataResult{
		Name:     m.resName,
		Overview: "scraped by " + m.name,
	}, nil
}

func TestMultiVersionSplitPreventsAutoRemergeAndPromotesMtimeOnDelete(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	reg := plugins.NewRegistry()
	reg.Register(nfo.New())
	sc := New(repo, reg, eventbus.New(), &config.Config{DataDir: tmpDir})
	sc.SetProber(NewDisabledProber())

	movieRoot := filepath.Join(tmpDir, "movies")
	subDir := filepath.Join(movieRoot, "Avatar (2009)")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	v1Path := filepath.Join(subDir, "Avatar (2009) - 1080p.mp4")
	v2Path := filepath.Join(subDir, "Avatar (2009) - 2160p.mkv")
	if err := os.WriteFile(v1Path, []byte("v1-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(v2Path, []byte("v2-bytes-longer"), 0o644); err != nil {
		t.Fatal(err)
	}

	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{movieRoot})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sc.ScanLibrary(ctx, lib.ID); err != nil {
		t.Fatalf("ScanLibrary: %v", err)
	}

	items, total, err := repo.QueryItems(ctx, sqlite.ItemFilter{LibraryID: lib.ID, IncludeItemTypes: []string{"Movie"}})
	if err != nil || total != 1 {
		t.Fatalf("expected 1 auto-merged movie initially, got total=%d err=%v", total, err)
	}
	primaryID := items[0].Id

	// 1. Delete primary file via ScanPath -> alternate version must be promoted with real mtime, item preserved!
	primaryRec, _ := repo.GetItemRecordByID(ctx, primaryID)
	altPath := v2Path
	if primaryRec.Path == v2Path {
		altPath = v1Path
	}
	if err := os.Remove(primaryRec.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := sc.ScanPath(ctx, lib.ID, primaryRec.Path); err != nil {
		t.Fatalf("ScanPath delete primary version: %v", err)
	}
	afterPromote, err := repo.GetItemRecordByID(ctx, primaryID)
	if err != nil || afterPromote == nil {
		t.Fatalf("expected movie item to survive single-version deletion via promotion: %v", err)
	}
	if afterPromote.Path != altPath {
		t.Fatalf("promoted path = %q, want %q", afterPromote.Path, altPath)
	}
	altStat, _ := os.Stat(altPath)
	if afterPromote.FileMtime == nil || *afterPromote.FileMtime != altStat.ModTime().Unix() {
		t.Fatalf("promoted FileMtime = %v, want %d", afterPromote.FileMtime, altStat.ModTime().Unix())
	}

	// Re-create deleted version, re-scan to merge, then manually SplitItemVersions and verify PreventAutoMerge!
	if err := os.WriteFile(primaryRec.Path, []byte("v1-recreated"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := sc.ScanPath(ctx, lib.ID, primaryRec.Path); err != nil {
		t.Fatalf("ScanPath re-add version: %v", err)
	}
	newIDs, err := repo.SplitItemVersions(ctx, primaryID)
	if err != nil || len(newIDs) != 1 {
		t.Fatalf("SplitItemVersions: newIDs=%v err=%v", newIDs, err)
	}

	// Touch/add a 3rd file or re-scan library & ScanPath: split items MUST NOT auto-remerge!
	if _, err := sc.ScanLibrary(ctx, lib.ID); err != nil {
		t.Fatalf("rescan after split: %v", err)
	}
	_, totalAfterRescan, err := repo.QueryItems(ctx, sqlite.ItemFilter{LibraryID: lib.ID, IncludeItemTypes: []string{"Movie"}})
	if err != nil || totalAfterRescan != 2 {
		t.Fatalf("expected 2 separate movies preserved after rescan (PreventAutoMerge), got %d", totalAfterRescan)
	}

	// Delete whole directory -> ScanPath cleans up all items in directory.
	if err := os.RemoveAll(subDir); err != nil {
		t.Fatal(err)
	}
	if _, err := sc.ScanPath(ctx, lib.ID, subDir); err != nil {
		t.Fatalf("ScanPath remove directory: %v", err)
	}
	_, totalAfterDirDel, _ := repo.QueryItems(ctx, sqlite.ItemFilter{LibraryID: lib.ID, IncludeItemTypes: []string{"Movie"}})
	if totalAfterDirDel != 0 {
		t.Fatalf("expected 0 movies after directory deletion, got %d", totalAfterDirDel)
	}
}

func TestCompanionSubtitleArtworkNFOScanPathRefreshAndCustomLocks(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	reg := plugins.NewRegistry()
	reg.Register(nfo.New())
	sc := New(repo, reg, eventbus.New(), &config.Config{DataDir: tmpDir})
	sc.SetProber(NewDisabledProber())

	movieRoot := filepath.Join(tmpDir, "movies")
	mDir := filepath.Join(movieRoot, "Inception (2010)")
	if err := os.MkdirAll(mDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mPath := filepath.Join(mDir, "Inception.mkv")
	if err := os.WriteFile(mPath, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}

	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{movieRoot})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sc.ScanLibrary(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}
	rec, err := repo.FindItemByPath(ctx, lib.ID, mPath)
	if err != nil || rec == nil {
		t.Fatalf("FindItemByPath: %v", err)
	}

	// 1. Late-added external subtitle detected via ScanPath
	subPath := filepath.Join(mDir, "Inception.zh.srt")
	if err := os.WriteFile(subPath, []byte("1\n00:00:01,000 --> 00:00:02,000\n你好\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := sc.ScanPath(ctx, lib.ID, subPath); err != nil {
		t.Fatalf("ScanPath subtitle: %v", err)
	}
	srcs, _ := repo.GetMediaSourcesByItemID(ctx, rec.ID)
	hasExtSub := false
	for _, st := range srcs[0].Streams {
		if st.IsExternal && st.Path == subPath {
			hasExtSub = true
		}
	}
	if !hasExtSub {
		t.Fatalf("expected external subtitle %s indexed via ScanPath, streams=%+v", subPath, srcs[0].Streams)
	}

	// 2. Late-added local poster detected via ScanPath, then protected if custom metadata image is uploaded
	posterPath := filepath.Join(mDir, "poster.jpg")
	if err := os.WriteFile(posterPath, []byte("local-poster"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := sc.ScanPath(ctx, lib.ID, posterPath); err != nil {
		t.Fatalf("ScanPath poster: %v", err)
	}
	img, _ := repo.GetImageByItemAndType(ctx, rec.ID, "Primary", 0)
	if img == nil || img.Path != posterPath {
		t.Fatalf("expected Primary image %q, got %+v", posterPath, img)
	}

	// Simulate custom uploaded image under DataDir/metadata/images/
	customImgDir := filepath.Join(tmpDir, "metadata", "images", rec.ID)
	_ = os.MkdirAll(customImgDir, 0o755)
	customPoster := filepath.Join(customImgDir, "primary.jpg")
	_ = os.WriteFile(customPoster, []byte("custom-uploaded-poster"), 0o644)
	_ = repo.UpsertImage(ctx, &sqlite.ImageRecord{ItemID: rec.ID, ImageType: "Primary", ImageIndex: 0, Path: customPoster, Tag: "customtag"})

	// Modify local poster and ScanPath -> custom metadata image must NOT be overwritten!
	_ = os.WriteFile(posterPath, []byte("local-poster-modified-size"), 0o644)
	_, _ = sc.ScanPath(ctx, lib.ID, posterPath)
	imgAfter, _ := repo.GetImageByItemAndType(ctx, rec.ID, "Primary", 0)
	if imgAfter == nil || imgAfter.Path != customPoster {
		t.Fatalf("expected custom metadata image preserved against local poster rescan, got %+v", imgAfter)
	}

	// 3. Independent SortName lock + late NFO update + deterministic ProviderIDs save
	rec, _ = repo.GetItemRecordByID(ctx, rec.ID)
	rec.SortName = "000-custom-sort"
	rec.Extra.LockedFields = []string{"SortName"}
	_ = repo.UpsertItem(ctx, rec)

	nfoPath := filepath.Join(mDir, "Inception.nfo")
	_ = os.WriteFile(nfoPath, []byte(`<?xml version="1.0" encoding="UTF-8"?><movie><title>盗梦空间</title><plot>梦中梦</plot></movie>`), 0o644)
	if _, err := sc.ScanPath(ctx, lib.ID, nfoPath); err != nil {
		t.Fatalf("ScanPath NFO: %v", err)
	}
	recAfterNFO, _ := repo.GetItemRecordByID(ctx, rec.ID)
	if recAfterNFO.Name != "盗梦空间" {
		t.Fatalf("expected NFO title '盗梦空间', got %q", recAfterNFO.Name)
	}
	if recAfterNFO.SortName != "000-custom-sort" {
		t.Fatalf("expected locked SortName '000-custom-sort' preserved when Name changed, got %q", recAfterNFO.SortName)
	}

	// RefreshItem discovers a second external subtitle while preserving locked SortName
	sub2Path := filepath.Join(mDir, "Inception.en.srt")
	_ = os.WriteFile(sub2Path, []byte("1\n00:00:01,000 --> 00:00:02,000\nHello\n"), 0o644)
	if err := sc.RefreshItem(ctx, rec.ID, true, false); err != nil {
		t.Fatalf("RefreshItem: %v", err)
	}
	srcsAfterRefresh, _ := repo.GetMediaSourcesByItemID(ctx, rec.ID)
	foundEn := false
	for _, st := range srcsAfterRefresh[0].Streams {
		if st.IsExternal && st.Path == sub2Path {
			foundEn = true
		}
	}
	if !foundEn {
		t.Fatalf("expected RefreshItem to discover new external subtitle %s", sub2Path)
	}

	// Deterministic ProviderIDs output in SaveItemLocalNFO
	recForSave, _ := repo.GetItemRecordByID(ctx, rec.ID)
	recForSave.Extra.ProviderIDs = map[string]string{
		"Douban": "3541415",
		"Tmdb":   "27205",
		"Imdb":   "tt1375666",
		"Custom": "cust-99",
	}
	if err := repo.UpsertItem(ctx, recForSave); err != nil {
		t.Fatalf("UpsertItem recForSave: %v", err)
	}
	if err := sc.SaveItemLocalNFO(ctx, recForSave.ID, true); err != nil {
		t.Fatalf("SaveItemLocalNFO: %v", err)
	}
	nfoBytes, _ := os.ReadFile(nfoPath)
	nfoStr := string(nfoBytes)
	idxCustom := strings.Index(nfoStr, `type="custom"`)
	idxDouban := strings.Index(nfoStr, `type="douban"`)
	idxImdb := strings.Index(nfoStr, `type="imdb"`)
	idxTmdb := strings.Index(nfoStr, `type="tmdb"`)
	if !(idxCustom > 0 && idxCustom < idxDouban && idxDouban < idxImdb && idxImdb < idxTmdb) {
		t.Fatalf("expected all ProviderIDs sorted deterministically in NFO, got:\n%s", nfoStr)
	}
}

func TestPerLibraryMetadataScraperOptionsIsolationAndOrder(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	reg := plugins.NewRegistry()
	scraperA := &mockCustomScraper{id: "ext.scraper.alpha", name: "AlphaScraper", priority: 10, enabled: true, resName: "Alpha Title"}
	scraperB := &mockCustomScraper{id: "ext.scraper.beta", name: "BetaScraper", priority: 20, enabled: true, resName: "Beta Title"}
	reg.Register(scraperA)
	reg.Register(scraperB)

	sc := New(repo, reg, eventbus.New(), &config.Config{DataDir: tmpDir})
	sc.SetProber(NewDisabledProber())

	// Library 1 reverses order: BetaScraper before AlphaScraper
	lib1Dir := filepath.Join(tmpDir, "lib1")
	_ = os.MkdirAll(lib1Dir, 0o755)
	_ = os.WriteFile(filepath.Join(lib1Dir, "TestMovie.mp4"), []byte("1"), 0o644)
	lib1, err := repo.CreateLibrary(ctx, "Lib1", "movies", []string{lib1Dir})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = store.Writer.ExecContext(ctx, `UPDATE libraries SET options_json = ? WHERE id = ?`,
		`{"MetadataFetchers":["BetaScraper","AlphaScraper"],"MetadataFetcherOrder":["BetaScraper","AlphaScraper"]}`, lib1.ID)

	// Library 2 disables all online scrapers (empty MetadataFetchers)
	lib2Dir := filepath.Join(tmpDir, "lib2")
	_ = os.MkdirAll(lib2Dir, 0o755)
	_ = os.WriteFile(filepath.Join(lib2Dir, "TestMovie.mp4"), []byte("2"), 0o644)
	lib2, err := repo.CreateLibrary(ctx, "Lib2", "movies", []string{lib2Dir})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = store.Writer.ExecContext(ctx, `UPDATE libraries SET options_json = ? WHERE id = ?`,
		`{"MetadataFetchers":[]}`, lib2.ID)

	if _, err := sc.ScanLibrary(ctx, lib1.ID); err != nil {
		t.Fatalf("ScanLibrary lib1: %v", err)
	}
	if _, err := sc.ScanLibrary(ctx, lib2.ID); err != nil {
		t.Fatalf("ScanLibrary lib2: %v", err)
	}

	m1, _ := repo.FindItemByPath(ctx, lib1.ID, filepath.Join(lib1Dir, "TestMovie.mp4"))
	if m1 == nil || m1.Name != "Beta Title" {
		t.Fatalf("expected Lib1 to prioritize BetaScraper ('Beta Title'), got %+v", m1)
	}

	callsBeforeLib2Refresh := scraperA.calls + scraperB.calls
	m2, _ := repo.FindItemByPath(ctx, lib2.ID, filepath.Join(lib2Dir, "TestMovie.mp4"))
	if m2 == nil || m2.Name != "TestMovie" {
		t.Fatalf("expected Lib2 with empty MetadataFetchers not to scrape online title, got %+v", m2)
	}
	if err := sc.RefreshItem(ctx, m2.ID, true, false); err != nil {
		t.Fatalf("RefreshItem lib2: %v", err)
	}
	if scraperA.calls+scraperB.calls != callsBeforeLib2Refresh {
		t.Fatalf("expected RefreshItem on Lib2 not to call disabled online scrapers")
	}
}
