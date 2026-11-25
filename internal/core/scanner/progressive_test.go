package scanner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/budget"
	"github.com/ambra-media/ambra/internal/core/watcher"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/plugins/nfo"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

type fakeFileInfo struct {
	os.FileInfo
}

func (f fakeFileInfo) Sys() any { return nil }

func TestWalkSafeSymlinkPoliciesAndCyclePrevention(t *testing.T) {
	root := t.TempDir()
	realSub := filepath.Join(root, "real")
	targetDir := filepath.Join(t.TempDir(), "external")
	nestedDir := filepath.Join(t.TempDir(), "nested_external")
	_ = os.MkdirAll(realSub, 0o755)
	_ = os.MkdirAll(targetDir, 0o755)
	_ = os.MkdirAll(nestedDir, 0o755)

	_ = os.WriteFile(filepath.Join(realSub, "a.mkv"), []byte("a"), 0o644)
	_ = os.WriteFile(filepath.Join(targetDir, "b.mkv"), []byte("b"), 0o644)
	_ = os.WriteFile(filepath.Join(nestedDir, "c.mkv"), []byte("c"), 0o644)

	// Direct symlink from root -> targetDir
	_ = os.Symlink(targetDir, filepath.Join(root, "link_direct"))
	// Nested symlink inside targetDir -> nestedDir
	_ = os.Symlink(nestedDir, filepath.Join(targetDir, "link_nested"))
	// Cycle symlink back to root and self!
	_ = os.Symlink(root, filepath.Join(realSub, "loop_to_root"))
	_ = os.Symlink(realSub, filepath.Join(realSub, "loop_to_self"))
	// Broken symlink
	_ = os.Symlink(filepath.Join(root, "does_not_exist"), filepath.Join(root, "broken_link"))

	// Nil callback is a no-op.
	if err := WalkSafe(root, SymlinkFollowDirect, nil); err != nil {
		t.Fatalf("WalkSafe nil fn: %v", err)
	}

	collectFiles := func(policy SymlinkPolicy) map[string]bool {
		found := make(map[string]bool)
		err := WalkSafe(root, policy, func(path string, info os.FileInfo) error {
			if !info.IsDir() {
				found[info.Name()] = true
			}
			return nil
		})
		if err != nil {
			t.Fatalf("WalkSafe(policy=%d) error: %v", policy, err)
		}
		return found
	}

	// SymlinkIgnore: only a.mkv
	gotIgnore := collectFiles(SymlinkIgnore)
	if !gotIgnore["a.mkv"] || gotIgnore["b.mkv"] || gotIgnore["c.mkv"] {
		t.Fatalf("SymlinkIgnore unexpected files: %+v", gotIgnore)
	}

	// SymlinkFollowDirect: a.mkv + b.mkv, skips nested symlink c.mkv, no infinite loop!
	gotDirect := collectFiles(SymlinkFollowDirect)
	if !gotDirect["a.mkv"] || !gotDirect["b.mkv"] || gotDirect["c.mkv"] {
		t.Fatalf("SymlinkFollowDirect unexpected files: %+v", gotDirect)
	}

	// SymlinkFollowAll: a.mkv + b.mkv + c.mkv, no infinite loop!
	gotAll := collectFiles(SymlinkFollowAll)
	if !gotAll["a.mkv"] || !gotAll["b.mkv"] || !gotAll["c.mkv"] {
		t.Fatalf("SymlinkFollowAll unexpected files: %+v", gotAll)
	}

	// Symlink root ignored when SymlinkIgnore
	symRoot := filepath.Join(t.TempDir(), "symroot")
	_ = os.Symlink(realSub, symRoot)
	visited := 0
	if err := WalkSafe(symRoot, SymlinkIgnore, func(_ string, _ os.FileInfo) error {
		visited++
		return nil
	}); err != nil {
		t.Fatalf("WalkSafe symRoot ignore: %v", err)
	}
	if visited != 0 {
		t.Fatalf("expected 0 visits for symlink root under SymlinkIgnore, got %d", visited)
	}

	// Single file root
	singleFile := filepath.Join(realSub, "a.mkv")
	fileCount := 0
	if err := WalkSafe(singleFile, SymlinkFollowDirect, func(_ string, info os.FileInfo) error {
		if !info.IsDir() {
			fileCount++
		}
		return nil
	}); err != nil {
		t.Fatalf("WalkSafe single file: %v", err)
	}
	if fileCount != 1 {
		t.Fatalf("expected 1 file visit, got %d", fileCount)
	}

	// SkipDir & custom error propagation
	sentinel := errors.New("stop")
	err := WalkSafe(root, SymlinkFollowDirect, func(path string, info os.FileInfo) error {
		if path == root {
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil when root returns SkipDir, got %v", err)
	}
	err = WalkSafe(root, SymlinkFollowDirect, func(path string, info os.FileInfo) error {
		if info.IsDir() && info.Name() == "real" {
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil when subdir returns SkipDir, got %v", err)
	}
	err = WalkSafe(root, SymlinkFollowDirect, func(path string, info os.FileInfo) error {
		if !info.IsDir() {
			return sentinel
		}
		return nil
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}

	// Non-existent root returns error
	if err := WalkSafe(filepath.Join(root, "missing"), SymlinkFollowDirect, func(string, os.FileInfo) error { return nil }); err == nil {
		t.Fatalf("expected error for missing root")
	}
	if err := WalkSafe(filepath.Join(root, "missing"), SymlinkIgnore, func(string, os.FileInfo) error { return nil }); err == nil {
		t.Fatalf("expected error for missing root under SymlinkIgnore")
	}

	// dirIdentity fallback when Sys() is not *syscall.Stat_t
	fi, _ := os.Stat(realSub)
	if id := dirIdentity(realSub, fakeFileInfo{fi}); id == "" {
		t.Fatalf("expected non-empty fallback dirIdentity")
	}
	if id := dirIdentity(filepath.Join(root, "nonexistent_dir"), nil); id == "" {
		t.Fatalf("expected non-empty fallback dirIdentity for nonexistent path")
	}
}

type mockOnlineProvider struct {
	id          string
	delay       time.Duration
	blockCh     chan struct{}
	calls       atomic.Int32
	active      atomic.Int32
	maxObserved atomic.Int32
}

func (m *mockOnlineProvider) Manifest() plugins.Manifest {
	return plugins.Manifest{
		ID:       m.id,
		Name:     "Mock Online Provider",
		Version:  "1.0.0",
		Enabled:  true,
		Priority: 20,
		Kinds:    []plugins.Kind{plugins.KindMetadata},
	}
}

func (m *mockOnlineProvider) Configure(map[string]any) error { return nil }

func (m *mockOnlineProvider) FetchMetadata(ctx context.Context, q plugins.MediaQuery) (*plugins.MetadataResult, error) {
	cur := m.active.Add(1)
	for {
		prev := m.maxObserved.Load()
		if cur <= prev || m.maxObserved.CompareAndSwap(prev, cur) {
			break
		}
	}
	defer m.active.Add(-1)
	m.calls.Add(1)

	if m.blockCh != nil {
		select {
		case <-m.blockCh:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return &plugins.MetadataResult{
		Name:            q.Title,
		OriginalTitle:   q.Title + " Original",
		Overview:        "Enriched overview for " + q.Title,
		Tagline:         "Tagline " + q.Title,
		Year:            2024,
		PremiereDate:    "2024-05-01",
		CommunityRating: 8.7,
		OfficialRating:  "PG-13",
		Genres:          []string{"Sci-Fi"},
		ProviderIDs:     map[string]string{"Tmdb": "999" + q.Title},
	}, nil
}

func writeFakeFFprobe(t *testing.T, dir string) string {
	t.Helper()
	script := filepath.Join(dir, "ffprobe_mock.sh")
	body := `#!/bin/sh
echo '{"format":{"duration":"120.5","bit_rate":"4000000","size":"123456","format_name":"matroska,webm"},"streams":[{"index":0,"codec_type":"video","codec_name":"hevc","width":3840,"height":2160,"disposition":{"default":1}},{"index":1,"codec_type":"audio","codec_name":"aac","channels":2,"channel_layout":"stereo","sample_rate":"48000","tags":{"language":"eng"},"disposition":{"default":1}}]}'
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake ffprobe: %v", err)
	}
	return script
}

func TestProgressiveTwoPhaseScanAndBudgetIsolation(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	reg := plugins.NewRegistry()
	reg.Register(nfo.New())

	blockCh := make(chan struct{})
	online := &mockOnlineProvider{
		id:      "ambra.builtin.tmdb",
		delay:   25 * time.Millisecond,
		blockCh: blockCh,
	}
	reg.Register(online)

	bus := eventbus.New()
	cfg := config.Default(tmp)

	var phasesMu sync.Mutex
	var phases []string
	bus.Subscribe(eventbus.EventLibraryChanged, func(_ context.Context, ev eventbus.Event) {
		if m, ok := ev.Payload.(map[string]any); ok {
			if ph, ok := m["Phase"].(string); ok {
				phasesMu.Lock()
				phases = append(phases, ph)
				phasesMu.Unlock()
			}
		}
	})

	mediaDir := filepath.Join(tmp, "movies")
	_ = os.MkdirAll(mediaDir, 0o755)
	for _, name := range []string{"Alpha (2020).mkv", "Beta (2021).mkv", "Gamma (2022).mkv", "Delta (2023).mkv"} {
		_ = os.WriteFile(filepath.Join(mediaDir, name), []byte("video"), 0o644)
	}
	// Also add a TV series with 1 episode to cover Series/Episode enrichment.
	tvDir := filepath.Join(tmp, "tv")
	epDir := filepath.Join(tvDir, "Cosmos (2020)", "Season 01")
	_ = os.MkdirAll(epDir, 0o755)
	_ = os.WriteFile(filepath.Join(epDir, "Cosmos.S01E01.mkv"), []byte("ep1"), 0o644)

	ctx := context.Background()
	movieLib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{mediaDir})
	if err != nil {
		t.Fatal(err)
	}
	tvLib, err := repo.CreateLibrary(ctx, "TV", "tvshows", []string{tvDir})
	if err != nil {
		t.Fatal(err)
	}

	sc := New(repo, reg, bus, cfg)
	sc.SetBudget(budget.New(2))
	sc.SetSymlinkPolicy(SymlinkFollowDirect)
	sc.SetProber(NewProber(writeFakeFFprobe(t, tmp)))

	// Phase A must return immediately even while online enrichment is blocked!
	stats, err := sc.ScanLibraryProgressive(ctx, movieLib.ID)
	if err != nil {
		t.Fatalf("ScanLibraryProgressive: %v", err)
	}
	if stats.ItemsAdded != 4 {
		t.Fatalf("expected 4 movies discovered in Phase A, got %+v", stats)
	}

	// Items are already visible in SQLite immediately after Phase A!
	items, total, err := repo.QueryItems(ctx, sqlite.ItemFilter{
		LibraryID:        movieLib.ID,
		IncludeItemTypes: []string{"Movie"},
	})
	if err != nil || total != 4 || len(items) != 4 {
		t.Fatalf("expected 4 movies immediately after Phase A, got total=%d err=%v", total, err)
	}

	// Duplicate concurrent scan for same library is a no-op.
	dupStats, err := sc.ScanLibraryProgressive(ctx, movieLib.ID)
	if err != nil || dupStats.ItemsAdded != 0 {
		t.Fatalf("expected concurrent duplicate scan to be skipped, got %+v err=%v", dupStats, err)
	}

	// Unblock Phase B and wait for idle.
	close(blockCh)
	waitCtx, waitCancel := context.WithTimeout(ctx, 5*time.Second)
	defer waitCancel()
	sc.WaitIdle(waitCtx)

	if online.maxObserved.Load() > 2 {
		t.Fatalf("Phase B concurrency exceeded budget limit 2: observed %d", online.maxObserved.Load())
	}
	if online.calls.Load() != 4 {
		t.Fatalf("expected 4 online enrichment calls, got %d", online.calls.Load())
	}

	// Verify enriched overview & ffprobe runtime ticks persisted.
	rec, err := repo.FindItemByPath(ctx, movieLib.ID, filepath.Join(mediaDir, "Alpha (2020).mkv"))
	if err != nil {
		t.Fatalf("FindItemByPath: %v", err)
	}
	if rec.Extra.Overview == "" || rec.RuntimeTicks == nil || *rec.RuntimeTicks <= 0 {
		t.Fatalf("expected enriched overview and probed runtime ticks, got overview=%q ticks=%v", rec.Extra.Overview, rec.RuntimeTicks)
	}

	// Also run synchronous ScanLibrary on TV library to verify Series + Episode enrichment.
	online.blockCh = nil
	online.delay = 0
	tvStats, err := sc.ScanLibrary(ctx, tvLib.ID)
	if err != nil || tvStats.ItemsAdded != 1 {
		t.Fatalf("ScanLibrary TV: stats=%+v err=%v", tvStats, err)
	}

	phasesMu.Lock()
	hasDiscovery, hasEnrichment := false, false
	for _, p := range phases {
		if p == "discovery" {
			hasDiscovery = true
		}
		if p == "enrichment" {
			hasEnrichment = true
		}
	}
	phasesMu.Unlock()
	if !hasDiscovery || !hasEnrichment {
		t.Fatalf("expected both discovery and enrichment events, got %v", phases)
	}
}

func TestScannerPauseResumeAndCancel(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	reg := plugins.NewRegistry()
	online := &mockOnlineProvider{id: "ambra.builtin.tmdb", delay: 30 * time.Millisecond}
	reg.Register(online)

	mediaDir := filepath.Join(tmp, "movies")
	_ = os.MkdirAll(mediaDir, 0o755)
	for _, name := range []string{"One.mkv", "Two.mkv", "Three.mkv", "Four.mkv"} {
		_ = os.WriteFile(filepath.Join(mediaDir, name), []byte("v"), 0o644)
	}

	ctx := context.Background()
	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{mediaDir})
	if err != nil {
		t.Fatal(err)
	}

	sc := New(repo, reg, eventbus.New(), config.Default(tmp))
	sc.SetProber(NewDisabledProber())
	sc.SetBudget(budget.New(1))

	// Pause idempotency check
	sc.Pause()
	sc.Pause()
	if !sc.IsPaused() {
		t.Fatalf("expected IsPaused() == true")
	}

	doneCh := make(chan error, 1)
	go func() {
		_, err := sc.ScanLibrary(ctx, lib.ID)
		doneCh <- err
	}()

	// Should remain blocked while paused.
	select {
	case err := <-doneCh:
		t.Fatalf("scan finished unexpectedly while paused: %v", err)
	case <-time.After(80 * time.Millisecond):
	}

	// Resume unblocks the scan.
	sc.Resume()
	sc.Resume()
	if sc.IsPaused() {
		t.Fatalf("expected IsPaused() == false after Resume")
	}
	select {
	case err := <-doneCh:
		if err != nil {
			t.Fatalf("ScanLibrary after Resume failed: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for resumed scan")
	}

	// Now test CancelScan during a paused Phase B.
	lib2Dir := filepath.Join(tmp, "movies2")
	_ = os.MkdirAll(lib2Dir, 0o755)
	for _, name := range []string{"A.mkv", "B.mkv", "C.mkv"} {
		_ = os.WriteFile(filepath.Join(lib2Dir, name), []byte("v"), 0o644)
	}
	lib2, err := repo.CreateLibrary(ctx, "Movies2", "movies", []string{lib2Dir})
	if err != nil {
		t.Fatal(err)
	}

	online.blockCh = make(chan struct{})
	if _, err := sc.ScanLibraryProgressive(ctx, lib2.ID); err != nil {
		t.Fatalf("ScanLibraryProgressive lib2: %v", err)
	}
	sc.CancelScan(lib2.ID)

	waitCtx, waitCancel := context.WithTimeout(ctx, 2*time.Second)
	defer waitCancel()
	sc.WaitIdle(waitCtx)
	if waitCtx.Err() != nil {
		t.Fatalf("scanner did not become idle after CancelScan")
	}
}

func TestScanPathAndWatcherEndToEnd(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	reg := plugins.NewRegistry()
	reg.Register(nfo.New())
	sc := New(repo, reg, eventbus.New(), config.Default(tmp))
	sc.SetProber(NewDisabledProber())

	mediaDir := filepath.Join(tmp, "movies")
	_ = os.MkdirAll(mediaDir, 0o755)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{mediaDir})
	if err != nil {
		t.Fatal(err)
	}

	// Edge cases for ScanPath
	if st, err := sc.ScanPath(ctx, lib.ID, ""); err != nil || st.ItemsAdded != 0 {
		t.Fatalf("empty ScanPath: st=%+v err=%v", st, err)
	}
	if st, err := sc.ScanPath(ctx, lib.ID, filepath.Join(mediaDir, "deleted.mkv")); err != nil || st.ItemsAdded != 0 {
		t.Fatalf("missing ScanPath: st=%+v err=%v", st, err)
	}
	if _, err := sc.ScanPath(ctx, "nonexistent-lib", mediaDir); err == nil {
		t.Fatalf("expected error for unknown libraryID")
	}

	// Wire watcher -> sc.ScanPath
	indexedCh := make(chan string, 5)
	w, err := watcher.New(func(cbCtx context.Context, libraryID string, paths []string) {
		for _, p := range paths {
			st, err := sc.ScanPath(cbCtx, libraryID, p)
			if err == nil && st.ItemsAdded > 0 {
				indexedCh <- p
			}
		}
	}, watcher.WithDebounce(60*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	if err := w.WatchLibrary(lib.ID, []string{mediaDir}); err != nil {
		t.Fatal(err)
	}
	if err := w.Start(ctx); err != nil {
		t.Fatal(err)
	}

	newMovie := filepath.Join(mediaDir, "Interstellar (2014).mkv")
	if err := os.WriteFile(newMovie, []byte("interstellar-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	select {
	case <-indexedCh:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for watcher -> ScanPath auto-indexing")
	}

	rec, err := repo.FindItemByPath(ctx, lib.ID, newMovie)
	if err != nil || rec == nil {
		t.Fatalf("expected Interstellar to be indexed by watcher+ScanPath, err=%v", err)
	}

	// ScanPath on a subdirectory directly
	subDir := filepath.Join(mediaDir, "Dune (2021)")
	_ = os.MkdirAll(subDir, 0o755)
	duneFile := filepath.Join(subDir, "Dune.2021.mkv")
	_ = os.WriteFile(duneFile, []byte("dune"), 0o644)

	st, err := sc.ScanPath(ctx, lib.ID, subDir)
	if err != nil || st.ItemsAdded != 1 {
		t.Fatalf("ScanPath subdir expected 1 added, got st=%+v err=%v", st, err)
	}

	// ScanPath with outside path must return empty root and error
	outside := filepath.Join(tmp, "outside.mkv")
	if got := findContainingRoot(lib.Paths, outside); got != "" {
		t.Fatalf("expected empty root for outside path, got %q", got)
	}
	_ = os.WriteFile(outside, []byte("out"), 0o644)
	if _, err := sc.ScanPath(ctx, lib.ID, outside); err == nil {
		t.Fatalf("expected error for ScanPath outside library roots")
	}
}
