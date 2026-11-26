package watcher

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestAuditV0107_DebounceGenerationTokenAndCoverage(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	var calls atomic.Int32
	w, err := New(func(_ context.Context, _ string, _ []string) {
		calls.Add(1)
	}, WithDebounce(200*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	if err := w.WatchLibrary("lib-gen", []string{root}); err != nil {
		t.Fatal(err)
	}

	// Schedule first batch (gen1), then reset with second event (gen2).
	w.mu.Lock()
	w.scheduleDebounceLocked("lib-gen", filepath.Join(root, "a.mkv"))
	gen1 := w.pending["lib-gen"].gen
	w.scheduleDebounceLocked("lib-gen", filepath.Join(root, "b.mkv"))
	gen2 := w.pending["lib-gen"].gen
	w.mu.Unlock()

	if gen1 == gen2 {
		t.Fatalf("expected generation token to advance on timer reset: gen1=%d gen2=%d", gen1, gen2)
	}

	// Stale timer callback carrying gen1 must be ignored!
	w.flushGen("lib-gen", gen1)
	if calls.Load() != 0 {
		t.Fatalf("stale generation flush must be ignored, got calls=%d", calls.Load())
	}

	// Current generation flush delivers both paths.
	w.flushGen("lib-gen", gen2)
	if calls.Load() != 1 {
		t.Fatalf("expected current generation flush to invoke callback once, got %d", calls.Load())
	}

	// Exercise collectDirsSafe, findRealRootForPath, cancelPendingTimers
	var collected []string
	collectDirsSafe(root, make(map[string]struct{}), &collected)
	if len(collected) < 2 {
		t.Fatalf("expected collectDirsSafe to find root and sub, got %v", collected)
	}
	if rr := findRealRootForPath([]string{root}, sub); rr == "" {
		t.Fatal("expected non-empty real root")
	}
	if rr := findRealRootForPath(nil, sub); rr == "" {
		t.Fatal("expected fallback real root for empty roots slice")
	}
	if isPathContainedInRealRoot("", sub) {
		t.Fatal("expected false for empty realRoot")
	}

	w.mu.Lock()
	w.scheduleDebounceLocked("lib-gen", filepath.Join(root, "c.mkv"))
	w.mu.Unlock()
	w.cancelPendingTimers()
	w.mu.Lock()
	remaining := len(w.pending)
	w.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("expected 0 pending batches after cancelPendingTimers, got %d", remaining)
	}
}
