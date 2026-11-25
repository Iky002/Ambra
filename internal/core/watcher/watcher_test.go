package watcher

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

func TestWatcherDebounceAndNewSubdir(t *testing.T) {
	root := t.TempDir()

	var (
		mu      sync.Mutex
		batches []struct {
			libID string
			paths []string
		}
		notifyCh = make(chan struct{}, 10)
	)

	w, err := New(func(_ context.Context, libraryID string, paths []string) {
		mu.Lock()
		batches = append(batches, struct {
			libID string
			paths []string
		}{libID: libraryID, paths: paths})
		mu.Unlock()
		select {
		case notifyCh <- struct{}{}:
		default:
		}
	}, WithDebounce(60*time.Millisecond))
	if err != nil {
		t.Fatalf("New watcher: %v", err)
	}
	defer w.Close()

	if err := w.WatchLibrary("lib-1", []string{root, ""}); err != nil {
		t.Fatalf("WatchLibrary: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Second Start is idempotent.
	if err := w.Start(ctx); err != nil {
		t.Fatalf("second Start: %v", err)
	}

	// Burst of writes to the same file should coalesce into one callback.
	f1 := filepath.Join(root, "movie.mkv")
	for i := 0; i < 4; i++ {
		if err := os.WriteFile(f1, []byte("chunk"), 0o644); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	select {
	case <-notifyCh:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for debounced file event")
	}

	mu.Lock()
	if len(batches) != 1 || batches[0].libID != "lib-1" {
		t.Fatalf("expected 1 coalesced batch for lib-1, got %+v", batches)
	}
	mu.Unlock()

	// Create a new subdirectory and then write a file inside it; watcher should auto-watch it.
	subDir := filepath.Join(root, "NewMovie (2025)")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Wait for subdir creation batch
	select {
	case <-notifyCh:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for subdir create event")
	}

	nestedFile := filepath.Join(subDir, "film.mkv")
	if err := os.WriteFile(nestedFile, []byte("nested"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case <-notifyCh:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for nested file event")
	}

	mu.Lock()
	foundNested := false
	for _, b := range batches {
		for _, p := range b.paths {
			if p == nestedFile {
				foundNested = true
			}
		}
	}
	mu.Unlock()
	if !foundNested {
		t.Fatalf("expected nested file %q in debounced batches, got %+v", nestedFile, batches)
	}

	// Remove subdirectory and ensure removal is handled cleanly.
	if err := os.RemoveAll(subDir); err != nil {
		t.Fatal(err)
	}
	select {
	case <-notifyCh:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for remove event")
	}

	// UnwatchLibrary stops future notifications.
	if err := w.UnwatchLibrary("lib-1"); err != nil {
		t.Fatalf("UnwatchLibrary: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "after_unwatch.mkv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case <-notifyCh:
		t.Fatalf("unexpected notification after UnwatchLibrary")
	case <-time.After(150 * time.Millisecond):
	}
}

func TestWatcherEdgeCasesAndSymlinkLoop(t *testing.T) {
	root := t.TempDir()
	// Hidden dir should be skipped, and symlink loop should not hang WatchLibrary.
	_ = os.MkdirAll(filepath.Join(root, ".hidden"), 0o755)
	sub := filepath.Join(root, "sub")
	_ = os.MkdirAll(sub, 0o755)
	_ = os.Symlink(root, filepath.Join(sub, "loop"))

	w, err := New(nil, WithDebounce(30*time.Millisecond), nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := w.WatchLibrary("", []string{root}); err == nil {
		t.Fatalf("expected error for empty libraryID")
	}
	if err := w.WatchLibrary("lib-loop", []string{root, filepath.Join(root, "nonexistent")}); err != nil {
		t.Fatalf("WatchLibrary with symlink loop failed: %v", err)
	}
	// Re-watching same library replaces old watches cleanly.
	if err := w.WatchLibrary("lib-loop", []string{root}); err != nil {
		t.Fatalf("re-WatchLibrary failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Hidden file should be ignored.
	_ = os.WriteFile(filepath.Join(root, ".DS_Store"), []byte("x"), 0o644)
	time.Sleep(80 * time.Millisecond)

	// Cancel context and close (idempotent).
	cancel()
	time.Sleep(30 * time.Millisecond)
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	// Operations after Close should return errors.
	if err := w.WatchLibrary("lib-after", []string{root}); err == nil {
		t.Fatalf("expected error from WatchLibrary after Close")
	}
	if err := w.Start(context.Background()); err == nil {
		t.Fatalf("expected error from Start after Close")
	}
}

type fakeFileInfo struct {
	os.FileInfo
}

func (f fakeFileInfo) Sys() any { return nil }

func TestWatcherInternalEdgeBranches(t *testing.T) {
	root := t.TempDir()
	w, err := New(func(context.Context, string, []string) {}, WithDebounce(20*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// UnwatchLibrary on unknown lib is a no-op.
	if err := w.UnwatchLibrary("unknown"); err != nil {
		t.Fatalf("UnwatchLibrary unknown: %v", err)
	}
	// flush on unknown lib is a no-op.
	w.flush("unknown")

	// handleEvent with irrelevant op (Chmod) or unwatched path is ignored.
	w.handleEvent(fsnotify.Event{Name: filepath.Join(root, "a.mkv"), Op: fsnotify.Chmod})
	w.handleEvent(fsnotify.Event{Name: filepath.Join(root, "a.mkv"), Op: fsnotify.Create})

	// UnwatchLibrary cancels pending debounce timer.
	if err := w.WatchLibrary("lib-pending", []string{root}); err != nil {
		t.Fatal(err)
	}
	w.mu.Lock()
	w.scheduleDebounceLocked("lib-pending", filepath.Join(root, "pending.mkv"))
	w.mu.Unlock()
	if err := w.UnwatchLibrary("lib-pending"); err != nil {
		t.Fatal(err)
	}

	// Start with nil ctx defaults to context.Background().
	if err := w.Start(nil); err != nil {
		t.Fatalf("Start(nil): %v", err)
	}

	// Close cancels pending timers cleanly.
	if err := w.WatchLibrary("lib-close", []string{root}); err != nil {
		t.Fatal(err)
	}
	w.mu.Lock()
	w.scheduleDebounceLocked("lib-close", filepath.Join(root, "closing.mkv"))
	w.mu.Unlock()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	// flush and scheduleDebounceLocked after Close are no-ops.
	w.flush("lib-close")
	w.mu.Lock()
	w.scheduleDebounceLocked("lib-close", filepath.Join(root, "after.mkv"))
	w.mu.Unlock()

	// dirKey fallback when Sys() is nil.
	fi, _ := os.Stat(root)
	if k := dirKey(root, fakeFileInfo{fi}); k == "" {
		t.Fatalf("expected non-empty fallback dirKey")
	}
	if k := dirKey(filepath.Join(root, "missing"), fakeFileInfo{fi}); k == "" {
		t.Fatalf("expected non-empty fallback dirKey for missing path")
	}
}

func TestWatcherSymlinkPolicyAndEscapeBlocked(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	// Create an internal dir and an escaping symlink pointing outside library root.
	insideDir := filepath.Join(root, "inside")
	if err := os.MkdirAll(insideDir, 0o755); err != nil {
		t.Fatal(err)
	}
	escapeLink := filepath.Join(root, "escape_dir")
	if err := os.Symlink(outside, escapeLink); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	// Also create a valid intra-root symlink.
	intraLink := filepath.Join(root, "intra_link")
	if err := os.Symlink(insideDir, intraLink); err != nil {
		t.Fatal(err)
	}

	// 1. Default policy (SymlinkFollowDirect) blocks escaping symlink target outside root.
	var mu sync.Mutex
	var events []string
	w, err := New(func(_ context.Context, _ string, paths []string) {
		mu.Lock()
		events = append(events, paths...)
		mu.Unlock()
	}, WithDebounce(30*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	if err := w.WatchLibrary("lib-sec", []string{root}); err != nil {
		t.Fatalf("WatchLibrary: %v", err)
	}
	w.mu.Lock()
	_, watchingEscape := w.dirToLib[escapeLink]
	_, watchingOutside := w.dirToLib[outside]
	w.mu.Unlock()
	if watchingEscape || watchingOutside {
		t.Fatalf("watcher must NOT watch symlink escaping root: escape=%v outside=%v", watchingEscape, watchingOutside)
	}

	// handleEvent on escaping symlink file/dir must be ignored and never schedule debounce.
	escapeFile := filepath.Join(root, "escape_file.mkv")
	outsideFile := filepath.Join(outside, "secret.mkv")
	_ = os.WriteFile(outsideFile, []byte("secret"), 0o644)
	_ = os.Symlink(outsideFile, escapeFile)

	w.handleEvent(fsnotify.Event{Name: escapeFile, Op: fsnotify.Create})
	w.handleEvent(fsnotify.Event{Name: escapeLink, Op: fsnotify.Create})
	time.Sleep(80 * time.Millisecond)

	mu.Lock()
	gotEvents := append([]string(nil), events...)
	mu.Unlock()
	if len(gotEvents) != 0 {
		t.Fatalf("expected escaping symlink events to be ignored, got %v", gotEvents)
	}

	// 2. SymlinkIgnore policy ignores even intra-root symlinks.
	wIgnore, err := New(func(_ context.Context, _ string, paths []string) {
		mu.Lock()
		events = append(events, paths...)
		mu.Unlock()
	}, WithDebounce(30*time.Millisecond), WithSymlinkPolicy(SymlinkIgnore))
	if err != nil {
		t.Fatal(err)
	}
	defer wIgnore.Close()

	if err := wIgnore.WatchLibrary("lib-ign", []string{root}); err != nil {
		t.Fatal(err)
	}
	wIgnore.mu.Lock()
	_, watchingIntra := wIgnore.dirToLib[intraLink]
	wIgnore.mu.Unlock()
	if watchingIntra {
		t.Fatalf("SymlinkIgnore policy must not watch intra-root symlink %q", intraLink)
	}
	wIgnore.handleEvent(fsnotify.Event{Name: intraLink, Op: fsnotify.Create})
	time.Sleep(80 * time.Millisecond)
	mu.Lock()
	gotEvents2 := append([]string(nil), events...)
	mu.Unlock()
	if len(gotEvents2) != 0 {
		t.Fatalf("expected SymlinkIgnore to drop symlink event, got %v", gotEvents2)
	}
}
