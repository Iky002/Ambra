package watcher

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestAuditV0109_L27_OverlappingLibrariesAndSymlinkHopLimit(t *testing.T) {
	sharedDir := t.TempDir()

	var mu sync.Mutex
	seen := make(map[string][]string)
	w, err := New(func(_ context.Context, libID string, paths []string) {
		mu.Lock()
		seen[libID] = append(seen[libID], paths...)
		mu.Unlock()
	}, WithDebounce(40*time.Millisecond), WithSymlinkPolicy(SymlinkFollowDirect))
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// Watch the same directory under two overlapping libraries.
	if err := w.WatchLibrary("lib-a", []string{sharedDir}); err != nil {
		t.Fatalf("WatchLibrary lib-a: %v", err)
	}
	if err := w.WatchLibrary("lib-b", []string{sharedDir}); err != nil {
		t.Fatalf("WatchLibrary lib-b: %v", err)
	}

	// Unwatching lib-a must NOT remove the fsnotify watch needed by lib-b.
	if err := w.UnwatchLibrary("lib-a"); err != nil {
		t.Fatalf("UnwatchLibrary lib-a: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	newFile := filepath.Join(sharedDir, "movie.mkv")
	if err := os.WriteFile(newFile, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		gotB := len(seen["lib-b"])
		gotA := len(seen["lib-a"])
		mu.Unlock()
		if gotB > 0 {
			if gotA != 0 {
				t.Fatalf("expected lib-a not to receive events after UnwatchLibrary, got %d", gotA)
			}
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	gotB := len(seen["lib-b"])
	mu.Unlock()
	if gotB == 0 {
		t.Fatal("expected lib-b to continue receiving events after overlapping lib-a was unwatched")
	}

	// Verify countSymlinkHopsUnderRoot accurately enforces depth 1 under SymlinkFollowDirect.
	if runtime.GOOS != "windows" {
		root := t.TempDir()
		real1 := filepath.Join(root, "real1")
		real2 := filepath.Join(root, "real2")
		_ = os.MkdirAll(real1, 0o755)
		_ = os.MkdirAll(real2, 0o755)
		_ = os.Symlink(real1, filepath.Join(root, "hop1"))
		_ = os.Symlink(real2, filepath.Join(real1, "hop2"))
		if hops := countSymlinkHopsUnderRoot(root, filepath.Join(root, "hop1")); hops != 1 {
			t.Fatalf("expected 1 hop, got %d", hops)
		}
		if hops := countSymlinkHopsUnderRoot(root, filepath.Join(root, "hop1", "hop2")); hops != 2 {
			t.Fatalf("expected 2 hops, got %d", hops)
		}
	}
}
