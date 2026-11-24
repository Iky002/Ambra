package scanner

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

type stubOnlineMetaProvider struct {
	delay time.Duration
	calls atomic.Int32
}

func (p *stubOnlineMetaProvider) Manifest() plugins.Manifest {
	return plugins.Manifest{
		ID:      "ambra.test.online",
		Name:    "Test Online",
		Version: "1.0.0",
		Enabled: true,
		Kinds:   []plugins.Kind{plugins.KindMetadata},
	}
}

func (p *stubOnlineMetaProvider) Configure(map[string]any) error { return nil }

func (p *stubOnlineMetaProvider) FetchMetadata(ctx context.Context, q plugins.MediaQuery) (*plugins.MetadataResult, error) {
	p.calls.Add(1)
	if p.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(p.delay):
		}
	}
	return &plugins.MetadataResult{
		Name:     q.Title,
		Overview: "Online overview for " + q.Title,
	}, nil
}

func TestScanPathDeletesStaleItemAndPublishesEvent(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "scanpath_del.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	bus := eventbus.New()
	sc := New(repo, nil, bus, config.Default(tmp))
	sc.SetProber(nil)

	ctx := context.Background()
	mediaDir := filepath.Join(tmp, "movies")
	_ = os.MkdirAll(mediaDir, 0o755)
	pathA := filepath.Join(mediaDir, "Alpha (2020).mkv")
	pathB := filepath.Join(mediaDir, "Beta (2021).mkv")
	_ = os.WriteFile(pathA, []byte("alpha"), 0o644)
	_ = os.WriteFile(pathB, []byte("beta"), 0o644)

	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{mediaDir})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sc.ScanLibrary(ctx, lib.ID); err != nil {
		t.Fatalf("initial ScanLibrary: %v", err)
	}

	cleanupCh := make(chan eventbus.Event, 4)
	bus.Subscribe(eventbus.EventLibraryChanged, func(_ context.Context, ev eventbus.Event) {
		if payload, ok := ev.Payload.(map[string]any); ok && payload["Phase"] == "cleanup" {
			cleanupCh <- ev
		}
	})

	// Remove pathA from disk and trigger incremental ScanPath.
	if err := os.Remove(pathA); err != nil {
		t.Fatal(err)
	}
	st, err := sc.ScanPath(ctx, lib.ID, pathA)
	if err != nil {
		t.Fatalf("ScanPath(deleted): %v", err)
	}
	if st.ItemsDeleted != 1 {
		t.Fatalf("expected ItemsDeleted=1, got %+v", st)
	}

	if _, err := repo.FindItemByMediaSourcePath(ctx, lib.ID, pathA); err == nil {
		t.Fatal("expected pathA to be removed from DB")
	}
	if _, err := repo.FindItemByMediaSourcePath(ctx, lib.ID, pathB); err != nil {
		t.Fatalf("expected pathB to remain in DB, got %v", err)
	}

	select {
	case <-cleanupCh:
	case <-time.After(2 * time.Second):
		t.Fatal("expected 1 cleanup EventLibraryChanged, timed out")
	}

	// Re-scanning already-deleted path is a clean no-op.
	st2, err := sc.ScanPath(ctx, lib.ID, pathA)
	if err != nil || st2.ItemsDeleted != 0 {
		t.Fatalf("expected no-op on second ScanPath(deleted), got st=%+v err=%v", st2, err)
	}
}

func TestScanLibraryPrunesDeletedFilesAndFiresOnScanComplete(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "prune.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	reg := plugins.NewRegistry()
	online := &stubOnlineMetaProvider{}
	reg.Register(online)
	sc := New(repo, reg, nil, config.Default(tmp))
	sc.SetProber(nil)

	var hookCalls atomic.Int32
	var lastStats ScanStats
	var hookMu sync.Mutex
	sc.SetOnScanComplete(func(_ context.Context, _ string, st ScanStats) {
		hookMu.Lock()
		lastStats = st
		hookMu.Unlock()
		hookCalls.Add(1)
	})

	ctx := context.Background()
	mediaDir := filepath.Join(tmp, "movies")
	_ = os.MkdirAll(mediaDir, 0o755)
	m1 := filepath.Join(mediaDir, "One (2020).mkv")
	m2 := filepath.Join(mediaDir, "Two (2021).mkv")
	_ = os.WriteFile(m1, []byte("one"), 0o644)
	_ = os.WriteFile(m2, []byte("two"), 0o644)
	_ = os.WriteFile(filepath.Join(mediaDir, "One (2020)-poster.jpg"), []byte("img1"), 0o644)
	_ = os.WriteFile(filepath.Join(mediaDir, "Two (2021)-poster.jpg"), []byte("img2"), 0o644)

	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{mediaDir})
	if err != nil {
		t.Fatal(err)
	}

	// Progressive scan: Phase A discovers 2 items, async Phase B enriches and fires OnScanComplete.
	st, err := sc.ScanLibraryProgressive(ctx, lib.ID)
	if err != nil {
		t.Fatalf("ScanLibraryProgressive: %v", err)
	}
	if st.ItemsAdded != 2 {
		t.Fatalf("expected ItemsAdded=2, got %+v", st)
	}
	sc.WaitIdle(ctx)
	if hookCalls.Load() != 1 {
		t.Fatalf("expected OnScanComplete called once after progressive scan, got %d", hookCalls.Load())
	}
	hookMu.Lock()
	if lastStats.ItemsAdded != 2 || lastStats.ItemsUpdated != 2 {
		t.Fatalf("unexpected hook stats: %+v", lastStats)
	}
	hookMu.Unlock()

	// Re-scan with no changes: OnScanComplete must NOT fire.
	if _, err := sc.ScanLibrary(ctx, lib.ID); err != nil {
		t.Fatalf("second ScanLibrary: %v", err)
	}
	if hookCalls.Load() != 1 {
		t.Fatalf("expected OnScanComplete not to fire on unchanged scan, got %d", hookCalls.Load())
	}

	// Delete m1 from disk, then run ScanLibrary: m1 must be pruned, m2 preserved.
	if err := os.Remove(m1); err != nil {
		t.Fatal(err)
	}
	stPrune, err := sc.ScanLibrary(ctx, lib.ID)
	if err != nil {
		t.Fatalf("prune ScanLibrary: %v", err)
	}
	if stPrune.ItemsDeleted != 1 {
		t.Fatalf("expected ItemsDeleted=1, got %+v", stPrune)
	}
	if _, err := repo.FindItemByMediaSourcePath(ctx, lib.ID, m1); err == nil {
		t.Fatal("expected m1 to be pruned from DB")
	}
	if _, err := repo.FindItemByMediaSourcePath(ctx, lib.ID, m2); err != nil {
		t.Fatalf("expected m2 to remain in DB, got %v", err)
	}

	// If library root is missing (walk error), do NOT prune remaining items.
	if err := os.RemoveAll(mediaDir); err != nil {
		t.Fatal(err)
	}
	stMissingRoot, err := sc.ScanLibrary(ctx, lib.ID)
	if err != nil {
		t.Fatalf("ScanLibrary(missing root): %v", err)
	}
	if stMissingRoot.Errors == 0 || stMissingRoot.ItemsDeleted != 0 {
		t.Fatalf("expected walk error and 0 deletions when root missing, got %+v", stMissingRoot)
	}
	if _, err := repo.FindItemByMediaSourcePath(ctx, lib.ID, m2); err != nil {
		t.Fatalf("expected m2 to survive missing-root scan, got %v", err)
	}
}

func TestScannerCancelAllAndCancelLibrary(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "cancel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	sc := New(repo, nil, nil, config.Default(tmp))
	sc.SetProber(nil)

	ctx := context.Background()
	mediaDir := filepath.Join(tmp, "movies")
	_ = os.MkdirAll(mediaDir, 0o755)
	_ = os.WriteFile(filepath.Join(mediaDir, "Slow (2022).mkv"), []byte("slow"), 0o644)

	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{mediaDir})
	if err != nil {
		t.Fatal(err)
	}

	// Pause scanner before starting scan so Phase A blocks at waitIfPaused.
	sc.Pause()
	if !sc.Progress().Paused {
		t.Fatal("expected Progress().Paused == true")
	}

	done := make(chan error, 1)
	go func() {
		_, err := sc.ScanLibrary(ctx, lib.ID)
		done <- err
	}()

	// Wait until scan registers in progress map.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if sc.Progress().Running {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	sc.CancelAll()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected context canceled error after CancelAll")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for scan to cancel after CancelAll")
	}

	if sc.IsPaused() {
		t.Fatal("expected CancelAll to clear paused state")
	}

	// CancelLibrary on idle library is a safe no-op.
	sc.CancelLibrary(lib.ID)
}

func TestScanLibrarySkipsPruneOnEmptyUnmountedRoot(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "unmounted.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	sc := New(repo, nil, nil, config.Default(tmp))
	sc.SetProber(nil)

	ctx := context.Background()
	rootA := filepath.Join(tmp, "nas_a")
	rootB := filepath.Join(tmp, "nas_b")
	_ = os.MkdirAll(rootA, 0o755)
	_ = os.MkdirAll(rootB, 0o755)

	movieA := filepath.Join(rootA, "NASMovie (2020).mkv")
	movieB1 := filepath.Join(rootB, "LocalKeep (2021).mkv")
	movieB2 := filepath.Join(rootB, "LocalDelete (2022).mkv")
	_ = os.WriteFile(movieA, []byte("nas"), 0o644)
	_ = os.WriteFile(movieB1, []byte("keep"), 0o644)
	_ = os.WriteFile(movieB2, []byte("del"), 0o644)

	lib, err := repo.CreateLibrary(ctx, "MultiRoot", "movies", []string{rootA, rootB})
	if err != nil {
		t.Fatal(err)
	}
	st, err := sc.ScanLibrary(ctx, lib.ID)
	if err != nil || st.ItemsAdded != 3 {
		t.Fatalf("initial scan: st=%+v err=%v", st, err)
	}

	// Simulate NAS unmount on rootA (mount point exists as empty dir) AND normal deletion of movieB2 in rootB.
	if err := os.Remove(movieA); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(movieB2); err != nil {
		t.Fatal(err)
	}

	st2, err := sc.ScanLibrary(ctx, lib.ID)
	if err != nil {
		t.Fatalf("rescan: %v", err)
	}
	// Only movieB2 should be pruned; movieA under empty rootA must be protected by circuit breaker!
	if st2.ItemsDeleted != 1 {
		t.Fatalf("expected ItemsDeleted=1 (only movieB2), got %+v", st2)
	}
	if _, err := repo.FindItemByMediaSourcePath(ctx, lib.ID, movieA); err != nil {
		t.Fatalf("expected movieA under empty rootA to survive prune circuit breaker, got %v", err)
	}
	if _, err := repo.FindItemByMediaSourcePath(ctx, lib.ID, movieB1); err != nil {
		t.Fatalf("expected movieB1 to survive, got %v", err)
	}
	if _, err := repo.FindItemByMediaSourcePath(ctx, lib.ID, movieB2); err == nil {
		t.Fatal("expected movieB2 under non-empty rootB to be pruned")
	}
}
