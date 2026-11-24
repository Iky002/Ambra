package watcher

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// DefaultDebounce is the default debounce window for coalescing bursts of filesystem events.
const DefaultDebounce = 3 * time.Second

// ChangeHandler is invoked after debouncing with the libraryID and deduplicated changed paths.
type ChangeHandler func(ctx context.Context, libraryID string, paths []string)

// Option configures a Watcher.
type Option func(*Watcher)

// WithDebounce overrides the debounce duration (values <= 0 are ignored).
func WithDebounce(d time.Duration) Option {
	return func(w *Watcher) {
		if d > 0 {
			w.debounce = d
		}
	}
}

type pendingBatch struct {
	timer *time.Timer
	paths map[string]struct{}
}

// Watcher monitors media library root directories recursively using fsnotify,
// dynamically tracks newly created or removed subdirectories, and debounces
// bursts of Create/Write/Rename/Remove events before invoking onChange.
type Watcher struct {
	fsw      *fsnotify.Watcher
	onChange ChangeHandler
	debounce time.Duration

	mu       sync.Mutex
	dirToLib map[string]string
	libDirs  map[string]map[string]struct{}
	pending  map[string]*pendingBatch
	started  bool
	closed   bool
	runCtx   context.Context
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// New creates a new Watcher with the given ChangeHandler and options.
func New(onChange ChangeHandler, opts ...Option) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &Watcher{
		fsw:      fsw,
		onChange: onChange,
		debounce: DefaultDebounce,
		dirToLib: make(map[string]string),
		libDirs:  make(map[string]map[string]struct{}),
		pending:  make(map[string]*pendingBatch),
		stopCh:   make(chan struct{}),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(w)
		}
	}
	return w, nil
}

// WatchLibrary recursively adds all non-hidden directories under roots to the watcher
// for the specified libraryID. Re-calling WatchLibrary replaces previous watches for libraryID.
func (w *Watcher) WatchLibrary(libraryID string, roots []string) error {
	if libraryID == "" {
		return fmt.Errorf("watcher: libraryID is required")
	}
	if err := w.UnwatchLibrary(libraryID); err != nil {
		return err
	}

	var dirs []string
	visited := make(map[string]struct{})
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		collectDirsSafe(filepath.Clean(root), visited, &dirs)
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return fmt.Errorf("watcher: closed")
	}

	if w.libDirs[libraryID] == nil {
		w.libDirs[libraryID] = make(map[string]struct{})
	}
	var firstErr error
	for _, dir := range dirs {
		if err := w.addDirLocked(libraryID, dir); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// UnwatchLibrary stops watching all directories associated with libraryID and cancels
// any pending debounced notifications for it.
func (w *Watcher) UnwatchLibrary(libraryID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if batch, ok := w.pending[libraryID]; ok {
		batch.timer.Stop()
		delete(w.pending, libraryID)
	}

	dirs, ok := w.libDirs[libraryID]
	if !ok {
		return nil
	}
	for dir := range dirs {
		_ = w.fsw.Remove(dir)
		delete(w.dirToLib, dir)
	}
	delete(w.libDirs, libraryID)
	return nil
}

// Start begins processing fsnotify events in a background goroutine until ctx is
// cancelled or Close is called. Calling Start more than once is a no-op.
func (w *Watcher) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return fmt.Errorf("watcher: closed")
	}
	if w.started {
		w.mu.Unlock()
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	w.started = true
	w.runCtx = ctx
	w.wg.Add(1)
	w.mu.Unlock()

	go w.loop(ctx)
	return nil
}

// Close stops the event loop, cancels all pending debounce timers, and closes fsnotify.
func (w *Watcher) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	for libID, batch := range w.pending {
		batch.timer.Stop()
		delete(w.pending, libID)
	}
	close(w.stopCh)
	w.mu.Unlock()

	err := w.fsw.Close()
	w.wg.Wait()
	return err
}

func (w *Watcher) loop(ctx context.Context) {
	defer w.wg.Done()
	for {
		select {
		case <-ctx.Done():
			w.cancelPendingTimers()
			return
		case <-w.stopCh:
			return
		case ev, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			w.handleEvent(ev)
		case _, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
		}
	}
}

func (w *Watcher) cancelPendingTimers() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for libID, batch := range w.pending {
		batch.timer.Stop()
		delete(w.pending, libID)
	}
}

func (w *Watcher) handleEvent(ev fsnotify.Event) {
	relevant := ev.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Rename|fsnotify.Remove) != 0
	if !relevant {
		return
	}

	cleanPath := filepath.Clean(ev.Name)
	base := filepath.Base(cleanPath)
	if strings.HasPrefix(base, ".") {
		return
	}

	w.mu.Lock()
	libID := w.lookupLibraryLocked(cleanPath)
	if libID == "" {
		w.mu.Unlock()
		return
	}

	// Dynamically manage subdirectory watches.
	if ev.Op&fsnotify.Create != 0 {
		if info, err := os.Stat(cleanPath); err == nil && info.IsDir() {
			var subdirs []string
			collectDirsSafe(cleanPath, make(map[string]struct{}), &subdirs)
			for _, d := range subdirs {
				_ = w.addDirLocked(libID, d)
			}
		}
	}
	if ev.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
		w.removeDirTreeLocked(libID, cleanPath)
	}

	w.scheduleDebounceLocked(libID, cleanPath)
	w.mu.Unlock()
}

func (w *Watcher) lookupLibraryLocked(path string) string {
	if libID, ok := w.dirToLib[path]; ok {
		return libID
	}
	parent := filepath.Dir(path)
	if libID, ok := w.dirToLib[parent]; ok {
		return libID
	}
	return ""
}

func (w *Watcher) addDirLocked(libraryID, dir string) error {
	if _, exists := w.dirToLib[dir]; exists {
		return nil
	}
	if err := w.fsw.Add(dir); err != nil {
		return err
	}
	w.dirToLib[dir] = libraryID
	if w.libDirs[libraryID] == nil {
		w.libDirs[libraryID] = make(map[string]struct{})
	}
	w.libDirs[libraryID][dir] = struct{}{}
	return nil
}

func (w *Watcher) removeDirTreeLocked(libraryID, target string) {
	dirs, ok := w.libDirs[libraryID]
	if !ok {
		return
	}
	prefix := target + string(filepath.Separator)
	for d := range dirs {
		if d == target || strings.HasPrefix(d, prefix) {
			_ = w.fsw.Remove(d)
			delete(dirs, d)
			delete(w.dirToLib, d)
		}
	}
}

func (w *Watcher) scheduleDebounceLocked(libraryID, path string) {
	if w.closed {
		return
	}
	batch, ok := w.pending[libraryID]
	if !ok {
		batch = &pendingBatch{
			paths: make(map[string]struct{}),
		}
		batch.timer = time.AfterFunc(w.debounce, func() {
			w.flush(libraryID)
		})
		w.pending[libraryID] = batch
	} else {
		batch.timer.Reset(w.debounce)
	}
	batch.paths[path] = struct{}{}
}

func (w *Watcher) flush(libraryID string) {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	batch, ok := w.pending[libraryID]
	if !ok {
		w.mu.Unlock()
		return
	}
	delete(w.pending, libraryID)
	paths := make([]string, 0, len(batch.paths))
	for p := range batch.paths {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	cb := w.onChange
	ctx := w.runCtx
	if ctx == nil {
		ctx = context.Background()
	}
	w.mu.Unlock()

	if cb != nil && len(paths) > 0 && ctx.Err() == nil {
		cb(ctx, libraryID, paths)
	}
}

// collectDirsSafe recursively collects all non-hidden directories under root,
// using Unix (Dev, Ino) deduplication with real-path fallback to prevent symlink loops.
func collectDirsSafe(root string, visited map[string]struct{}, out *[]string) {
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return
	}
	id := dirKey(root, info)
	if _, seen := visited[id]; seen {
		return
	}
	visited[id] = struct{}{}
	*out = append(*out, root)

	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		full := filepath.Join(root, entry.Name())
		subInfo, err := os.Stat(full)
		if err != nil || !subInfo.IsDir() {
			continue
		}
		collectDirsSafe(full, visited, out)
	}
}

func dirKey(path string, info os.FileInfo) string {
	if key, ok := dirInodeKey(info); ok {
		return key
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return "path:" + filepath.Clean(resolved)
	}
	return "path:" + filepath.Clean(path)
}
