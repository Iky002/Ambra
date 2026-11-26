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

// SymlinkPolicy controls how Watcher handles symbolic links during directory traversal and event handling.
type SymlinkPolicy int

const (
	// SymlinkFollowDirect follows direct symlinks (depth 1) whose targets remain inside the library root.
	SymlinkFollowDirect SymlinkPolicy = iota
	// SymlinkIgnore ignores all symbolic links.
	SymlinkIgnore
	// SymlinkFollowAll follows symbolic links at any depth whose targets remain inside the library root.
	SymlinkFollowAll
)

// DefaultDebounce is the default debounce window for coalescing bursts of filesystem events.
const (
	DefaultDebounce = 3 * time.Second
	maxWatchedDirs  = 10_000
)

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

// WithSymlinkPolicy configures how Watcher handles symbolic links.
func WithSymlinkPolicy(policy SymlinkPolicy) Option {
	return func(w *Watcher) {
		w.symlinkPolicy = policy
	}
}

type pendingBatch struct {
	timer *time.Timer
	paths map[string]struct{}
	gen   uint64
}

// Watcher monitors media library root directories recursively using fsnotify,
// dynamically tracks newly created or removed subdirectories, and debounces
// bursts of Create/Write/Rename/Remove events before invoking onChange.
type Watcher struct {
	fsw           *fsnotify.Watcher
	onChange      ChangeHandler
	debounce      time.Duration
	symlinkPolicy SymlinkPolicy

	mu        sync.Mutex
	dirToLibs map[string]map[string]struct{}
	libDirs   map[string]map[string]struct{}
	libRoots  map[string][]string
	pending   map[string]*pendingBatch
	nextGen   uint64
	started   bool
	closed    bool
	runCtx    context.Context
	stopCh    chan struct{}
	wg        sync.WaitGroup
}

// New creates a new Watcher with the given ChangeHandler and options.
func New(onChange ChangeHandler, opts ...Option) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &Watcher{
		fsw:           fsw,
		onChange:      onChange,
		debounce:      DefaultDebounce,
		symlinkPolicy: SymlinkFollowDirect,
		dirToLibs:     make(map[string]map[string]struct{}),
		libDirs:       make(map[string]map[string]struct{}),
		libRoots:      make(map[string][]string),
		pending:       make(map[string]*pendingBatch),
		stopCh:        make(chan struct{}),
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
	var cleanRoots []string
	visited := make(map[string]struct{})
	policy := w.symlinkPolicy
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		cleanRoot := filepath.Clean(root)
		cleanRoots = append(cleanRoots, cleanRoot)
		realRoot := resolveRealRoot(cleanRoot)
		collectDirsSafeWithPolicy(cleanRoot, realRoot, 0, policy, visited, &dirs)
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return fmt.Errorf("watcher: closed")
	}

	if w.libDirs[libraryID] == nil {
		w.libDirs[libraryID] = make(map[string]struct{})
	}
	w.libRoots[libraryID] = cleanRoots
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

	delete(w.libRoots, libraryID)
	dirs, ok := w.libDirs[libraryID]
	if !ok {
		return nil
	}
	for dir := range dirs {
		w.detachDirFromLibLocked(libraryID, dir)
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
	if strings.HasPrefix(base, ".") || isTempFileName(base) {
		return
	}

	w.mu.Lock()
	libIDs := w.lookupLibrariesLocked(cleanPath)
	if len(libIDs) == 0 {
		w.mu.Unlock()
		return
	}
	rootsByLib := make(map[string][]string, len(libIDs))
	for _, id := range libIDs {
		rootsByLib[id] = append([]string(nil), w.libRoots[id]...)
	}
	policy := w.symlinkPolicy
	w.mu.Unlock()

	type libAction struct {
		libID   string
		subdirs []string
	}
	var actions []libAction
	for _, libID := range libIDs {
		roots := rootsByLib[libID]
		matchedRoot := findMatchingRoot(roots, cleanPath)
		hops := countSymlinkHopsUnderRoot(matchedRoot, cleanPath)

		// Enforce SymlinkPolicy (including depth 1 limit for SymlinkFollowDirect) and EvalSymlinks containment.
		if ev.Op&(fsnotify.Create|fsnotify.Write) != 0 {
			if hops > 0 {
				if policy == SymlinkIgnore {
					continue
				}
				if policy == SymlinkFollowDirect && hops > 1 {
					continue
				}
				if !isWithinAnyRoot(roots, cleanPath) {
					continue
				}
			}
		}

		var subdirs []string
		if ev.Op&fsnotify.Create != 0 {
			if info, err := os.Stat(cleanPath); err == nil && info.IsDir() {
				if policy == SymlinkFollowDirect && hops > 1 {
					continue
				}
				realRoot := findRealRootForPath(roots, cleanPath)
				collectDirsSafeWithPolicy(cleanPath, realRoot, hops, policy, make(map[string]struct{}), &subdirs)
			}
		}
		actions = append(actions, libAction{libID: libID, subdirs: subdirs})
	}
	if len(actions) == 0 {
		return
	}

	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	for _, act := range actions {
		if !w.hasLibraryLocked(act.libID, cleanPath) {
			continue
		}
		for _, d := range act.subdirs {
			_ = w.addDirLocked(act.libID, d)
		}
		if ev.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
			w.removeDirTreeLocked(act.libID, cleanPath)
		}
		w.scheduleDebounceLocked(act.libID, cleanPath)
	}
	w.mu.Unlock()
}

func isTempFileName(base string) bool {
	lower := strings.ToLower(base)
	return strings.HasSuffix(lower, ".tmp") ||
		strings.HasSuffix(lower, ".part") ||
		strings.HasSuffix(lower, ".crdownload") ||
		strings.HasSuffix(lower, "~")
}

func (w *Watcher) lookupLibrariesLocked(path string) []string {
	seen := make(map[string]struct{})
	for _, candidate := range []string{path, filepath.Dir(path)} {
		if libs, ok := w.dirToLibs[candidate]; ok {
			for id := range libs {
				seen[id] = struct{}{}
			}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func (w *Watcher) hasLibraryLocked(libraryID, path string) bool {
	if libs, ok := w.dirToLibs[path]; ok {
		if _, exists := libs[libraryID]; exists {
			return true
		}
	}
	if libs, ok := w.dirToLibs[filepath.Dir(path)]; ok {
		if _, exists := libs[libraryID]; exists {
			return true
		}
	}
	return false
}

func (w *Watcher) addDirLocked(libraryID, dir string) error {
	libs, exists := w.dirToLibs[dir]
	if !exists {
		if len(w.dirToLibs) >= maxWatchedDirs {
			return fmt.Errorf("watcher: max watched directories limit (%d) reached", maxWatchedDirs)
		}
		if err := w.fsw.Add(dir); err != nil {
			return err
		}
		libs = make(map[string]struct{})
		w.dirToLibs[dir] = libs
	}
	libs[libraryID] = struct{}{}
	if w.libDirs[libraryID] == nil {
		w.libDirs[libraryID] = make(map[string]struct{})
	}
	w.libDirs[libraryID][dir] = struct{}{}
	return nil
}

func (w *Watcher) detachDirFromLibLocked(libraryID, dir string) {
	libs, ok := w.dirToLibs[dir]
	if !ok {
		return
	}
	delete(libs, libraryID)
	if len(libs) == 0 {
		_ = w.fsw.Remove(dir)
		delete(w.dirToLibs, dir)
	}
}

func (w *Watcher) removeDirTreeLocked(libraryID, target string) {
	dirs, ok := w.libDirs[libraryID]
	if !ok {
		return
	}
	prefix := target + string(filepath.Separator)
	for d := range dirs {
		if d == target || strings.HasPrefix(d, prefix) {
			delete(dirs, d)
			w.detachDirFromLibLocked(libraryID, d)
		}
	}
}

func (w *Watcher) scheduleDebounceLocked(libraryID, path string) {
	if w.closed {
		return
	}
	w.nextGen++
	gen := w.nextGen
	batch, ok := w.pending[libraryID]
	if !ok {
		batch = &pendingBatch{
			paths: make(map[string]struct{}),
			gen:   gen,
		}
		batch.timer = time.AfterFunc(w.debounce, func() {
			w.flushGen(libraryID, gen)
		})
		w.pending[libraryID] = batch
	} else {
		batch.timer.Stop()
		batch.gen = gen
		batch.timer = time.AfterFunc(w.debounce, func() {
			w.flushGen(libraryID, gen)
		})
	}
	batch.paths[path] = struct{}{}
}

func (w *Watcher) flush(libraryID string) {
	w.flushGen(libraryID, 0)
}

func (w *Watcher) flushGen(libraryID string, gen uint64) {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	batch, ok := w.pending[libraryID]
	if !ok || (gen != 0 && batch.gen != gen) {
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
	w.wg.Add(1)
	w.mu.Unlock()
	defer w.wg.Done()

	if cb != nil && len(paths) > 0 && ctx.Err() == nil {
		cb(ctx, libraryID, paths)
	}
}

func resolveRealRoot(root string) string {
	if r, err := filepath.EvalSymlinks(root); err == nil {
		return filepath.Clean(r)
	}
	if a, err := filepath.Abs(root); err == nil {
		return filepath.Clean(a)
	}
	return filepath.Clean(root)
}

func isPathContainedInRealRoot(realRoot, target string) bool {
	if realRoot == "" {
		return false
	}
	realTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(realRoot, filepath.Clean(realTarget))
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel))
}

func isWithinAnyRoot(roots []string, target string) bool {
	for _, r := range roots {
		if isPathContainedInRealRoot(resolveRealRoot(r), target) {
			return true
		}
	}
	return false
}

func findMatchingRoot(roots []string, target string) string {
	cleanTarget := filepath.Clean(target)
	best := ""
	for _, r := range roots {
		cr := filepath.Clean(strings.TrimSpace(r))
		if cr == "" {
			continue
		}
		if cleanTarget == cr || strings.HasPrefix(cleanTarget, cr+string(filepath.Separator)) {
			if len(cr) > len(best) {
				best = cr
			}
		}
	}
	return best
}

func countSymlinkHopsUnderRoot(root, target string) int {
	cleanRoot := filepath.Clean(strings.TrimSpace(root))
	cleanTarget := filepath.Clean(strings.TrimSpace(target))
	if cleanTarget == "" {
		return 0
	}
	if cleanRoot == "" || cleanRoot == "." {
		if linfo, err := os.Lstat(cleanTarget); err == nil && linfo.Mode()&os.ModeSymlink != 0 {
			return 1
		}
		return 0
	}
	rel, err := filepath.Rel(cleanRoot, cleanTarget)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		if linfo, lerr := os.Lstat(cleanTarget); lerr == nil && linfo.Mode()&os.ModeSymlink != 0 {
			return 1
		}
		return 0
	}
	hops := 0
	cur := cleanRoot
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if part == "" || part == "." {
			continue
		}
		cur = filepath.Join(cur, part)
		if linfo, lerr := os.Lstat(cur); lerr == nil && linfo.Mode()&os.ModeSymlink != 0 {
			hops++
		}
	}
	return hops
}

func findRealRootForPath(roots []string, target string) string {
	for _, r := range roots {
		rr := resolveRealRoot(r)
		if isPathContainedInRealRoot(rr, target) {
			return rr
		}
	}
	if len(roots) > 0 {
		return resolveRealRoot(roots[0])
	}
	return resolveRealRoot(target)
}

// collectDirsSafe recursively collects all non-hidden directories under root (up to maxWatchedDirs),
// using Unix (Dev, Ino) deduplication with real-path fallback to prevent symlink loops.
func collectDirsSafe(root string, visited map[string]struct{}, out *[]string) {
	collectDirsSafeWithPolicy(root, resolveRealRoot(root), 0, SymlinkFollowDirect, visited, out)
}

func collectDirsSafeWithPolicy(root, realRoot string, symlinkDepth int, policy SymlinkPolicy, visited map[string]struct{}, out *[]string) {
	if len(*out) >= maxWatchedDirs {
		return
	}
	if policy == SymlinkIgnore {
		linfo, err := os.Lstat(root)
		if err != nil || linfo.Mode()&os.ModeSymlink != 0 {
			return
		}
	}
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
		if len(*out) >= maxWatchedDirs {
			return
		}
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		full := filepath.Join(root, entry.Name())
		isSymlink := entry.Type()&os.ModeSymlink != 0
		nextDepth := symlinkDepth
		if isSymlink {
			if policy == SymlinkIgnore {
				continue
			}
			if policy == SymlinkFollowDirect && symlinkDepth >= 1 {
				continue
			}
			if !isPathContainedInRealRoot(realRoot, full) {
				continue
			}
			nextDepth++
		}
		subInfo, err := os.Stat(full)
		if err != nil || !subInfo.IsDir() {
			continue
		}
		collectDirsSafeWithPolicy(full, realRoot, nextDepth, policy, visited, out)
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
