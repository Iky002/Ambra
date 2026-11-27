package scanner

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/plugins/nfo"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestAuditV0109_H16_MergedAwayReturnsTrueWhenDeletedConcurrently(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "h16.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	ctx := context.Background()
	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{tmp})
	if err != nil {
		t.Fatal(err)
	}

	survivor := &sqlite.ItemRecord{
		ID:        "mov-survivor",
		LibraryID: lib.ID,
		Type:      "Movie",
		Name:      "Dune",
		Path:      filepath.Join(tmp, "dune_4k.mkv"),
		Extra:     sqlite.ItemExtra{ProviderIDs: map[string]string{"Tmdb": "438631"}},
	}
	if err := repo.UpsertItem(ctx, survivor); err != nil {
		t.Fatal(err)
	}

	// Simulate rec already deleted by a concurrent worker's MergeItemVersions.
	deletedRec := &sqlite.ItemRecord{
		ID:        "mov-deleted-by-peer",
		LibraryID: lib.ID,
		Type:      "Movie",
		Name:      "Dune",
		Path:      filepath.Join(tmp, "dune_1080p.mkv"),
		Extra:     sqlite.ItemExtra{ProviderIDs: map[string]string{"Tmdb": "438631"}},
	}

	sc := New(repo, nil, nil, config.Default(tmp))
	if mergedAway := sc.tryAutoMergeMovieByProviderID(ctx, deletedRec); !mergedAway {
		t.Fatal("expected tryAutoMergeMovieByProviderID to return true when rec.ID no longer exists")
	}
	// Survivor itself still exists -> mergedAway must be false.
	if mergedAway := sc.tryAutoMergeMovieByProviderID(ctx, survivor); mergedAway {
		t.Fatal("expected tryAutoMergeMovieByProviderID to return false for surviving record")
	}
}

func TestAuditV0109_M30_LocalSaveRootContainmentAndAtomicSymlinkReject(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink test requires unix")
	}
	tmp := t.TempDir()
	libRoot := filepath.Join(tmp, "library")
	outsideDir := filepath.Join(tmp, "outside")
	if err := os.MkdirAll(libRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Fatal(err)
	}

	store, err := sqlite.Open(filepath.Join(tmp, "m30.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	repo := sqlite.NewLibraryRepo(store)
	ctx := context.Background()
	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{libRoot})
	if err != nil {
		t.Fatal(err)
	}

	sc := New(repo, nil, nil, config.Default(tmp))
	escapedRec := &sqlite.ItemRecord{
		ID:        "esc-1",
		LibraryID: lib.ID,
		Type:      "Movie",
		Name:      "Escaped",
		Path:      filepath.Join(outsideDir, "escaped.mkv"),
	}
	if _, ok := sc.resolveLocalImagePath(ctx, lib, escapedRec, "Primary", ".jpg"); ok {
		t.Fatal("expected resolveLocalImagePath to reject target outside library root")
	}
	if _, _, ok := sc.resolveLocalNFOTarget(ctx, escapedRec); ok {
		t.Fatal("expected resolveLocalNFOTarget to reject target outside library root")
	}

	// writeFileAtomic must refuse to overwrite a symlink target.
	secretFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(secretFile, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(libRoot, "poster.jpg")
	if err := os.Symlink(secretFile, linkPath); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(linkPath, []byte("overwrite")); err == nil {
		t.Fatal("expected writeFileAtomic to reject symlink target")
	}
	got, _ := os.ReadFile(secretFile)
	if string(got) != "original" {
		t.Fatalf("symlink destination was modified: %q", string(got))
	}
}

func TestAuditV0109_M36_L27_ScanPathDirCleanupProgressAndSymlinkDepth(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink test requires unix")
	}
	tmp := t.TempDir()
	libRoot := filepath.Join(tmp, "lib")
	subDir := filepath.Join(libRoot, "ShowA")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	epFile := filepath.Join(subDir, "ShowA.S01E01.mkv")
	if err := os.WriteFile(epFile, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := sqlite.Open(filepath.Join(tmp, "m36.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	repo := sqlite.NewLibraryRepo(store)
	ctx := context.Background()
	lib, err := repo.CreateLibrary(ctx, "TV", "tvshows", []string{libRoot})
	if err != nil {
		t.Fatal(err)
	}

	sc := New(repo, nil, nil, config.Default(tmp))
	sc.SetProber(NewDisabledProber())

	// Seed a full-scan progress snapshot and ensure ScanPath does NOT clobber it.
	sc.startLibraryProgress(lib.ID, lib.Name)
	sc.mu.Lock()
	sc.progress[lib.ID].Phase = "discovering"
	sc.progress[lib.ID].Total = 999
	sc.mu.Unlock()

	if _, err := sc.ScanPath(ctx, lib.ID, subDir); err != nil {
		t.Fatalf("ScanPath dir: %v", err)
	}
	sc.mu.Lock()
	gotTotal := sc.progress[lib.ID].Total
	gotPhase := sc.progress[lib.ID].Phase
	sc.mu.Unlock()
	if gotTotal != 999 || gotPhase != "discovering" {
		t.Fatalf("ScanPath mutated full-scan progress snapshot: total=%d phase=%q", gotTotal, gotPhase)
	}

	// Remove the whole ShowA directory and invoke ScanPath(subDir) -> cleans up child items by prefix.
	if err := os.RemoveAll(subDir); err != nil {
		t.Fatal(err)
	}
	delStats, err := sc.ScanPath(ctx, lib.ID, subDir)
	if err != nil {
		t.Fatalf("ScanPath deleted dir: %v", err)
	}
	if delStats.ItemsDeleted == 0 {
		t.Fatalf("expected deleted directory child items to be removed, got %+v", delStats)
	}

	// L-27: SymlinkFollowDirect enforces depth 1 relative to library root.
	realTarget := filepath.Join(libRoot, "real")
	if err := os.MkdirAll(realTarget, 0o755); err != nil {
		t.Fatal(err)
	}
	link1 := filepath.Join(libRoot, "link1")
	if err := os.Symlink(realTarget, link1); err != nil {
		t.Fatal(err)
	}
	deepTarget := filepath.Join(libRoot, "deep")
	if err := os.MkdirAll(deepTarget, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deepTarget, "Nested.2024.mkv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link2 := filepath.Join(realTarget, "link2")
	if err := os.Symlink(deepTarget, link2); err != nil {
		t.Fatal(err)
	}
	// Path through link1/link2/Nested.2024.mkv has 2 symlink hops -> skipped under SymlinkFollowDirect.
	twoHopFile := filepath.Join(link1, "link2", "Nested.2024.mkv")
	sc.SetSymlinkPolicy(SymlinkFollowDirect)
	stHop, err := sc.ScanPath(ctx, lib.ID, twoHopFile)
	if err != nil {
		t.Fatalf("ScanPath 2-hop symlink: %v", err)
	}
	if stHop.ItemsAdded != 0 {
		t.Fatalf("expected 2-hop symlink skipped under SymlinkFollowDirect, got %+v", stHop)
	}
}

func TestAuditV0112_ProviderIdsLockCompanionNFOReplaceFalseAndScanPathAsync(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "data")
	moviesDir := filepath.Join(tmp, "movies")
	movDir := filepath.Join(moviesDir, "Inception (2010)")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(movDir, 0o755); err != nil {
		t.Fatal(err)
	}

	store, err := sqlite.Open(filepath.Join(dataDir, "scanner_v0112.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	reg := plugins.NewRegistry()
	reg.Register(nfo.New())
	bus := eventbus.New()
	cfg := config.Default(dataDir)

	mkvPath := filepath.Join(movDir, "inception.mkv")
	nfoPath := filepath.Join(movDir, "inception.nfo")
	_ = os.WriteFile(mkvPath, []byte("video-bytes"), 0o644)
	_ = os.WriteFile(nfoPath, []byte(`<movie><title>Inception</title><tmdbid>27205</tmdbid></movie>`), 0o644)

	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{moviesDir})
	if err != nil {
		t.Fatal(err)
	}

	sc := New(repo, reg, bus, cfg)

	// 1. ScanPathAsync + WaitIdle scans targeted directory cleanly.
	sc.ScanPathAsync(lib.ID, movDir)
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	sc.WaitIdle(waitCtx)

	rec, err := repo.FindItemByMediaSourcePath(ctx, lib.ID, mkvPath)
	if err != nil || rec == nil {
		t.Fatalf("expected item created by ScanPathAsync, got (%+v, %v)", rec, err)
	}

	// 2. L-09: User customizes Overview and locks ProviderIds (with manually curated Tmdb="999999").
	rec.Extra.Overview = "Custom user curated overview"
	rec.Extra.ProviderIDs = map[string]string{"Tmdb": "999999"}
	rec.Extra.LockedFields = []string{"ProviderIds"}
	if err := repo.UpsertItem(ctx, rec); err != nil {
		t.Fatal(err)
	}

	// Modify companion .nfo with a DIFFERENT tmdbid ("111111") and NO <plot>, then trigger companion NFO incremental change!
	_ = os.WriteFile(nfoPath, []byte(`<movie><title>Inception Director Cut</title><tmdbid>111111</tmdbid></movie>`), 0o644)
	if _, err := sc.ScanPath(ctx, lib.ID, nfoPath); err != nil {
		t.Fatalf("companion ScanPath(nfo) failed: %v", err)
	}

	afterCompanion, err := repo.GetItemRecordByID(ctx, rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Because handleCompanionFileChange uses replaceMetadata=false, existing non-empty Overview MUST NOT be wiped,
	// AND because ProviderIds is in LockedFields, Tmdb MUST stay "999999"!
	if afterCompanion.Extra.Overview != "Custom user curated overview" {
		t.Fatalf("expected existing Overview preserved on incremental companion NFO update (replace=false), got %q", afterCompanion.Extra.Overview)
	}
	if afterCompanion.Extra.ProviderIDs["Tmdb"] != "999999" {
		t.Fatalf("expected locked ProviderIds['Tmdb']='999999' preserved on companion refresh, got %+v", afterCompanion.Extra.ProviderIDs)
	}

	// Even a full replaceMetadata=true RefreshItem MUST still honor LockedFields=["ProviderIds"]!
	if err := sc.RefreshItem(ctx, rec.ID, true, false); err != nil {
		t.Fatalf("RefreshItem failed: %v", err)
	}
	afterFullRefresh, err := repo.GetItemRecordByID(ctx, rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterFullRefresh.Extra.ProviderIDs["Tmdb"] != "999999" {
		t.Fatalf("expected locked ProviderIds['Tmdb']='999999' preserved on ReplaceAllMetadata refresh, got %+v", afterFullRefresh.Extra.ProviderIDs)
	}
}

func TestScannerConfigSnapshotConcurrency(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Default(tmp)
	sc := New(nil, nil, nil, cfg)

	var wg sync.WaitGroup
	// Writer goroutines mutating config fields concurrently under cfg.Update
	for w := 0; w < 2; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			langs := []string{"zh-CN", "en-US", "ja-JP", ""}
			for i := 0; i < 500; i++ {
				lang := langs[(i+id)%len(langs)]
				dir := filepath.Join(tmp, "data", lang)
				cfg.Update(func(c *config.Config) {
					c.TMDBLanguage = lang
					c.DataDir = dir
					c.FFmpegPath = "/usr/bin/ffmpeg"
					c.FFprobePath = "/usr/bin/ffprobe"
				})
			}
		}(w)
	}

	// Reader goroutines calling Scanner.language() and Scanner.imageStorageDir() concurrently
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				if lang := sc.language(); lang == "" {
					t.Errorf("language() must never return empty string")
				}
				if dir := sc.imageStorageDir(); dir == "" {
					t.Errorf("imageStorageDir() must never return empty string")
				}
			}
		}()
	}

	wg.Wait()
}

