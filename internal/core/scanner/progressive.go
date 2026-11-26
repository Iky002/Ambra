package scanner

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ambra-media/ambra/internal/core/budget"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

const phaseABatchSize = 500

type pendingPhaseAItem struct {
	input           sqlite.BatchItemInput
	postHook        func(ctx context.Context) error
	displayFileName string
	tasks           []enrichTask
}

type phaseABatch struct {
	scanner *Scanner
	libID   string
	maxSize int
	items   []pendingPhaseAItem
	stats   *ScanStats
	tasks   *[]enrichTask

	// In-memory collision sets for unflushed items so multi-version auto-merge
	// within the same batch flushes before querying SQLite.
	movieDirTitle map[string]struct{}
	movieProvider map[string]struct{}
	episodeKeys   map[string]struct{}
}

func newPhaseABatch(s *Scanner, libID string, stats *ScanStats, tasks *[]enrichTask) *phaseABatch {
	return &phaseABatch{
		scanner:       s,
		libID:         libID,
		maxSize:       phaseABatchSize,
		items:         make([]pendingPhaseAItem, 0, phaseABatchSize),
		stats:         stats,
		tasks:         tasks,
		movieDirTitle: make(map[string]struct{}),
		movieProvider: make(map[string]struct{}),
		episodeKeys:   make(map[string]struct{}),
	}
}

func (b *phaseABatch) reset() {
	b.items = b.items[:0]
	clear(b.movieDirTitle)
	clear(b.movieProvider)
	clear(b.episodeKeys)
}

func movieDirTitleKey(dir, title string) string {
	return filepath.Clean(dir) + "\x00" + strings.ToLower(strings.TrimSpace(title))
}

func episodeMergeKey(seriesID string, seasonNum, episodeNum int) string {
	return fmt.Sprintf("%s#%d#%d", seriesID, seasonNum, episodeNum)
}

func (b *phaseABatch) flushIfMovieCollides(ctx context.Context, dir, title string, providerIDs map[string]string) error {
	if len(b.items) == 0 {
		return nil
	}
	collides := false
	if title != "" {
		if _, ok := b.movieDirTitle[movieDirTitleKey(dir, title)]; ok {
			collides = true
		}
	}
	if !collides && len(providerIDs) > 0 {
		if tmdbID := getProviderID(providerIDs, "Tmdb"); tmdbID != "" {
			if _, ok := b.movieProvider["tmdb:"+strings.ToLower(tmdbID)]; ok {
				collides = true
			}
		}
		if !collides {
			if imdbID := getProviderID(providerIDs, "Imdb"); imdbID != "" {
				if _, ok := b.movieProvider["imdb:"+strings.ToLower(imdbID)]; ok {
					collides = true
				}
			}
		}
	}
	if collides {
		return b.flush(ctx)
	}
	return nil
}

func (b *phaseABatch) flushIfEpisodeCollides(ctx context.Context, seriesID string, seasonNum, episodeNum int) error {
	if len(b.items) == 0 {
		return nil
	}
	if _, ok := b.episodeKeys[episodeMergeKey(seriesID, seasonNum, episodeNum)]; ok {
		return b.flush(ctx)
	}
	return nil
}

func (b *phaseABatch) addMovie(ctx context.Context, item pendingPhaseAItem, dir, parsedTitle string) error {
	if parsedTitle != "" {
		b.movieDirTitle[movieDirTitleKey(dir, parsedTitle)] = struct{}{}
	}
	if item.input.Item != nil {
		if item.input.Item.Name != "" {
			b.movieDirTitle[movieDirTitleKey(dir, item.input.Item.Name)] = struct{}{}
		}
		if len(item.input.Item.Extra.ProviderIDs) > 0 {
			if tmdbID := getProviderID(item.input.Item.Extra.ProviderIDs, "Tmdb"); tmdbID != "" {
				b.movieProvider["tmdb:"+strings.ToLower(tmdbID)] = struct{}{}
			}
			if imdbID := getProviderID(item.input.Item.Extra.ProviderIDs, "Imdb"); imdbID != "" {
				b.movieProvider["imdb:"+strings.ToLower(imdbID)] = struct{}{}
			}
		}
	}
	b.items = append(b.items, item)
	if len(b.items) >= b.maxSize {
		return b.flush(ctx)
	}
	return nil
}

func (b *phaseABatch) addEpisode(ctx context.Context, item pendingPhaseAItem, seriesID string, seasonNum, episodeNum int) error {
	b.episodeKeys[episodeMergeKey(seriesID, seasonNum, episodeNum)] = struct{}{}
	b.items = append(b.items, item)
	if len(b.items) >= b.maxSize {
		return b.flush(ctx)
	}
	return nil
}

func (b *phaseABatch) flush(ctx context.Context) error {
	if len(b.items) == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		b.reset()
		return err
	}

	pending := make([]pendingPhaseAItem, len(b.items))
	copy(pending, b.items)
	b.reset()

	inputs := make([]sqlite.BatchItemInput, len(pending))
	for i, item := range pending {
		inputs[i] = item.input
	}

	if err := b.scanner.repo.UpsertItemsBatch(ctx, inputs); err != nil {
		for _, item := range pending {
			b.stats.Errors++
			b.scanner.recordDiscoveryItem(b.libID, item.displayFileName, false, false, true)
		}
		return err
	}

	for _, item := range pending {
		if err := ctx.Err(); err != nil {
			return err
		}
		if item.postHook != nil {
			if err := item.postHook(ctx); err != nil {
				b.stats.Errors++
				b.scanner.recordDiscoveryItem(b.libID, item.displayFileName, false, false, true)
				continue
			}
		}
		b.stats.ItemsAdded++
		b.scanner.recordDiscoveryItem(b.libID, item.displayFileName, true, false, false)
		if len(item.tasks) > 0 {
			*b.tasks = append(*b.tasks, item.tasks...)
		}
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	runtime.Gosched()
	return nil
}

type enrichTask struct {
	ItemID           string
	LibraryID        string
	LibRoot          string
	ItemType         string
	ParentSeriesID   string
	MediaPath        string
	Size             int64
	Mtime            int64
	IsStrm           bool
	Container        string
	Query            plugins.MediaQuery
	NeedProbe        bool
	NeedOnlineMeta   bool
	IsAltMediaSource bool
	HasLocalTitle    bool
	DisplayName      string
}

// SetBudget sets the shared background concurrency budget pool used by Phase B enrichment.
func (s *Scanner) SetBudget(p *budget.Pool) {
	s.mu.Lock()
	s.budget = p
	s.mu.Unlock()
}

func (s *Scanner) budgetPool() *budget.Pool {
	s.mu.Lock()
	p := s.budget
	s.mu.Unlock()
	if p != nil {
		return p
	}
	return budget.Global()
}

// SetSymlinkPolicy configures how directory traversal handles symbolic links.
func (s *Scanner) SetSymlinkPolicy(policy SymlinkPolicy) {
	s.mu.Lock()
	s.symlinkPolicy = policy
	s.mu.Unlock()
}

func (s *Scanner) currentSymlinkPolicy() SymlinkPolicy {
	s.mu.Lock()
	p := s.symlinkPolicy
	s.mu.Unlock()
	return p
}

// Pause pauses active scans at safe checkpoints (between files and enrichment tasks).
func (s *Scanner) Pause() {
	s.pauseMu.Lock()
	defer s.pauseMu.Unlock()
	if s.paused {
		return
	}
	s.paused = true
	s.resumeCh = make(chan struct{})
}

// Resume unblocks any paused scans.
func (s *Scanner) Resume() {
	s.pauseMu.Lock()
	defer s.pauseMu.Unlock()
	if !s.paused {
		return
	}
	s.paused = false
	close(s.resumeCh)
	s.resumeCh = nil
}

// IsPaused reports whether scanning is currently paused.
func (s *Scanner) IsPaused() bool {
	s.pauseMu.Lock()
	defer s.pauseMu.Unlock()
	return s.paused
}

func (s *Scanner) waitIfPaused(ctx context.Context) error {
	for {
		s.pauseMu.Lock()
		if !s.paused {
			s.pauseMu.Unlock()
			return nil
		}
		ch := s.resumeCh
		s.pauseMu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ch:
		}
	}
}

// SetOnScanComplete registers a callback invoked after Phase A/B finishes when items were added or updated.
func (s *Scanner) SetOnScanComplete(fn func(ctx context.Context, libraryID string, stats ScanStats)) {
	s.mu.Lock()
	s.onScanComplete = fn
	s.mu.Unlock()
}

func (s *Scanner) notifyScanComplete(ctx context.Context, libraryID string, stats ScanStats) {
	if ctx.Err() != nil {
		return
	}
	if stats.ItemsAdded == 0 && stats.ItemsUpdated == 0 {
		return
	}
	s.mu.Lock()
	fn := s.onScanComplete
	s.mu.Unlock()
	if fn != nil {
		fn(ctx, libraryID, stats)
	}
}

// CancelScan cancels an in-progress scan (Phase A or Phase B) for libraryID.
func (s *Scanner) CancelScan(libraryID string) {
	libraryID = guid.Normalize(libraryID)
	s.mu.Lock()
	cancel := s.cancels[libraryID]
	if cancel != nil {
		cancel()
	}
	s.mu.Unlock()
}

// CancelLibrary cancels an in-progress scan for libraryID.
func (s *Scanner) CancelLibrary(libraryID string) {
	s.CancelScan(libraryID)
}

// CancelAll cancels all in-progress library scans and clears paused state.
func (s *Scanner) CancelAll() {
	s.mu.Lock()
	for _, cancel := range s.cancels {
		if cancel != nil {
			cancel()
		}
	}
	s.mu.Unlock()
	s.Resume()
}

// ScanLibraryProgressive runs Phase A (fast discovery + local NFO/images/subtitles)
// synchronously and returns immediately once the poster wall is populated, while
// launching Phase B (budget-constrained ffprobe + online metadata enrichment)
// asynchronously in the background.
func (s *Scanner) ScanLibraryProgressive(ctx context.Context, libraryID string) (ScanStats, error) {
	return s.scanLibrary(ctx, libraryID, true)
}

func (s *Scanner) scanLibrary(ctx context.Context, libraryID string, asyncPhaseB bool) (ScanStats, error) {
	libraryID = guid.Normalize(libraryID)
	stats := ScanStats{LibraryID: libraryID}

	s.mu.Lock()
	if s.scanning[libraryID] {
		s.mu.Unlock()
		return stats, nil
	}
	scanCtx, cancel := context.WithCancel(ctx)
	s.scanning[libraryID] = true
	s.cancels[libraryID] = cancel
	s.mu.Unlock()

	releaseScan := func() {
		cancel()
		s.mu.Lock()
		delete(s.scanning, libraryID)
		delete(s.cancels, libraryID)
		delete(s.progress, libraryID)
		s.mu.Unlock()
	}

	lib, err := s.repo.GetLibrary(scanCtx, libraryID)
	if err != nil {
		releaseScan()
		return stats, err
	}

	s.startLibraryProgress(lib.ID, lib.Name)

	discovered := make(map[string]struct{})
	tasks, walkHadError, err := s.runPhaseA(scanCtx, lib, lib.Paths, discovered, &stats)
	if err != nil {
		releaseScan()
		return stats, err
	}

	if !walkHadError && len(lib.Paths) > 0 && scanCtx.Err() == nil {
		s.pruneDeletedLibraryItems(scanCtx, lib, discovered, &stats)
	}

	s.publishLibraryChanged(scanCtx, lib.ID, stats, "discovery")

	if len(tasks) == 0 {
		s.notifyScanComplete(scanCtx, lib.ID, stats)
		releaseScan()
		return stats, nil
	}

	if asyncPhaseB {
		s.mu.Lock()
		if scanCtx.Err() != nil {
			s.mu.Unlock()
			releaseScan()
			return stats, scanCtx.Err()
		}
		bgCtx, bgCancel := context.WithCancel(context.WithoutCancel(ctx))
		s.cancels[libraryID] = bgCancel
		s.mu.Unlock()
		cancel()
		bgStats := stats
		go func() {
			defer func() {
				bgCancel()
				s.mu.Lock()
				delete(s.scanning, libraryID)
				delete(s.cancels, libraryID)
				delete(s.progress, libraryID)
				s.mu.Unlock()
			}()
			bgStats.ItemsUpdated += s.runPhaseB(bgCtx, lib.ID, tasks)
			s.notifyScanComplete(bgCtx, lib.ID, bgStats)
		}()
		return stats, nil
	}

	defer releaseScan()
	stats.ItemsUpdated += s.runPhaseB(scanCtx, lib.ID, tasks)
	s.notifyScanComplete(scanCtx, lib.ID, stats)
	return stats, nil
}

func (s *Scanner) pruneDeletedLibraryItems(ctx context.Context, lib *sqlite.LibraryRecord, discovered map[string]struct{}, stats *ScanStats) {
	if lib == nil {
		return
	}
	existingPaths, err := s.repo.ListLibraryItemPaths(ctx, lib.ID)
	if err != nil || len(existingPaths) == 0 {
		return
	}

	// Circuit breaker for unmounted NAS / empty mount points:
	// If a library root has existing DB items but os.ReadDir(root) returns 0 entries (or fails),
	// skip pruning items under that root so a transient storage disconnect never wipes the DB.
	skipRoots := make([]string, 0)
	for _, r := range lib.Paths {
		cr := filepath.Clean(strings.TrimSpace(r))
		if cr == "" || cr == "." {
			continue
		}
		hasDBItemsUnderRoot := false
		for p := range existingPaths {
			cp := filepath.Clean(p)
			if cp == cr || strings.HasPrefix(cp, cr+string(filepath.Separator)) {
				hasDBItemsUnderRoot = true
				break
			}
		}
		if !hasDBItemsUnderRoot {
			continue
		}
		entries, readErr := os.ReadDir(cr)
		if readErr != nil || len(entries) == 0 {
			slog.Warn("skipping prune for empty library root (possible unmounted storage)", "library_id", lib.ID, "root", cr)
			skipRoots = append(skipRoots, cr)
		}
	}

	for p := range existingPaths {
		if ctx.Err() != nil {
			return
		}
		cp := filepath.Clean(p)
		if _, ok := discovered[cp]; ok {
			continue
		}
		skipped := false
		for _, sr := range skipRoots {
			if cp == sr || strings.HasPrefix(cp, sr+string(filepath.Separator)) {
				skipped = true
				break
			}
		}
		if skipped {
			continue
		}
		if _, statErr := os.Stat(p); os.IsNotExist(statErr) {
			if deleted, delErr := s.repo.DeleteItemByPath(ctx, lib.ID, p); delErr == nil && deleted {
				stats.ItemsDeleted++
			}
		}
	}
}

// ScanPath performs an incremental scan of a single file or subdirectory within libraryID.
// Designed for real-time filesystem watcher callbacks.
func (s *Scanner) ScanPath(ctx context.Context, libraryID, targetPath string) (ScanStats, error) {
	libraryID = guid.Normalize(libraryID)
	stats := ScanStats{LibraryID: libraryID}

	targetPath = strings.TrimSpace(targetPath)
	if targetPath == "" {
		return stats, nil
	}
	lib, err := s.repo.GetLibrary(ctx, libraryID)
	if err != nil {
		return stats, err
	}

	cleanTarget := filepath.Clean(targetPath)
	root := findContainingRoot(lib.Paths, cleanTarget)
	if root == "" {
		return stats, fmt.Errorf("scanner: path %q is outside library %s roots", cleanTarget, lib.ID)
	}
	policy := s.currentSymlinkPolicy()
	linfo, lerr := os.Lstat(cleanTarget)
	if lerr == nil {
		if policy == SymlinkIgnore && (linfo.Mode()&os.ModeSymlink != 0 || hasSymlinkComponentUnderRoot(root, cleanTarget)) {
			return stats, nil
		}
		if !isPathWithinRoot(root, cleanTarget) {
			return stats, fmt.Errorf("scanner: path %q escapes library %s root", cleanTarget, lib.ID)
		}
	} else if !os.IsNotExist(lerr) {
		return stats, lerr
	} else if hasSymlinkComponentUnderRoot(root, cleanTarget) {
		if policy == SymlinkIgnore {
			return stats, nil
		}
		if !isPathWithinRoot(root, cleanTarget) {
			return stats, fmt.Errorf("scanner: path %q escapes library %s root", cleanTarget, lib.ID)
		}
	}
	info, err := os.Stat(cleanTarget)
	if err != nil {
		if os.IsNotExist(err) {
			deleted, delErr := s.repo.DeleteItemByPath(ctx, lib.ID, cleanTarget)
			if delErr != nil {
				return stats, delErr
			}
			if deleted {
				stats.ItemsDeleted++
				s.publishLibraryChanged(ctx, lib.ID, stats, "cleanup")
			}
			return stats, nil
		}
		return stats, err
	}
	forceTV := strings.EqualFold(lib.CollectionType, "tvshows")
	seriesCache := make(map[string]*sqlite.ItemRecord)
	seasonCache := make(map[string]*sqlite.ItemRecord)
	artistCache := make(map[string]*sqlite.ItemRecord)
	albumCache := make(map[string]*sqlite.ItemRecord)
	seenSeriesEnrich := make(map[string]struct{})
	seenSeriesExtras := make(map[string]struct{})

	var tasks []enrichTask
	if !info.IsDir() {
		if !strings.HasPrefix(filepath.Base(cleanTarget), ".") && (IsVideoFile(cleanTarget) || IsStrmFile(cleanTarget) || IsAudioFile(cleanTarget)) {
			if !strings.EqualFold(lib.CollectionType, "music") {
				if _, isExtra := ClassifyExtraFile(root, cleanTarget); isExtra {
					if err := s.waitIfPaused(ctx); err != nil {
						return stats, err
					}
					if s.reindexExtraParent(ctx, lib, root, cleanTarget) {
						stats.ItemsUpdated++
						s.publishLibraryChanged(ctx, lib.ID, stats, "discovery")
						s.notifyScanComplete(ctx, lib.ID, stats)
					}
					return stats, nil
				}
			}
			if err := s.waitIfPaused(ctx); err != nil {
				return stats, err
			}
			itemTasks, procErr := s.processDiscoveredFile(ctx, lib, root, cleanTarget, forceTV, seriesCache, seasonCache, artistCache, albumCache, seenSeriesEnrich, seenSeriesExtras, nil, &stats)
			if procErr != nil {
				return stats, procErr
			}
			tasks = append(tasks, itemTasks...)
		}
	} else {
		dirTasks, _, err := s.runPhaseAWithRoot(ctx, lib, root, cleanTarget, nil, &stats)
		if err != nil {
			return stats, err
		}
		tasks = append(tasks, dirTasks...)
	}

	if stats.ItemsAdded > 0 || stats.ItemsUpdated > 0 {
		s.publishLibraryChanged(ctx, lib.ID, stats, "discovery")
	}
	if len(tasks) > 0 {
		stats.ItemsUpdated += s.runPhaseB(ctx, lib.ID, tasks)
	}
	s.notifyScanComplete(ctx, lib.ID, stats)
	return stats, nil
}

func findContainingRoot(roots []string, target string) string {
	cleanTarget := filepath.Clean(target)
	best := ""
	for _, r := range roots {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		cr := filepath.Clean(r)
		if cleanTarget == cr || strings.HasPrefix(cleanTarget, cr+string(filepath.Separator)) {
			if len(cr) > len(best) {
				best = cr
			}
		}
	}
	return best
}

func resolveExtraParentDir(root, cleanTarget string) string {
	dir := filepath.Clean(filepath.Dir(cleanTarget))
	cleanRoot := filepath.Clean(strings.TrimSpace(root))
	if dir != cleanRoot {
		if _, isExtraSubdir := extraSubdirTypes[strings.ToLower(strings.TrimSpace(filepath.Base(dir)))]; isExtraSubdir {
			return filepath.Clean(filepath.Dir(dir))
		}
	}
	return dir
}

func (s *Scanner) findExtraParentItems(ctx context.Context, libraryID, parentDir string) []*sqlite.ItemRecord {
	if s.repo == nil || strings.TrimSpace(parentDir) == "" {
		return nil
	}
	cleanParentDir := filepath.Clean(parentDir)
	seen := make(map[string]struct{})
	var parents []*sqlite.ItemRecord

	addIfParent := func(rec *sqlite.ItemRecord) {
		if rec == nil {
			return
		}
		if _, dup := seen[rec.ID]; dup {
			return
		}
		if strings.EqualFold(rec.Type, "Series") || strings.EqualFold(rec.Type, "Movie") {
			seen[rec.ID] = struct{}{}
			parents = append(parents, rec)
		}
	}

	if rec, err := s.repo.FindItemByPath(ctx, libraryID, cleanParentDir); err == nil && rec != nil {
		addIfParent(rec)
	}

	if itemPaths, err := s.repo.ListLibraryItemPaths(ctx, libraryID); err == nil {
		for p, itemID := range itemPaths {
			if _, dup := seen[itemID]; dup {
				continue
			}
			cp := filepath.Clean(p)
			if cp == cleanParentDir || filepath.Dir(cp) == cleanParentDir {
				if rec, recErr := s.repo.GetItemRecordByID(ctx, itemID); recErr == nil && rec != nil {
					if strings.EqualFold(rec.Type, "Movie") {
						addIfParent(rec)
					}
				}
			}
		}
	}

	return parents
}

func (s *Scanner) reindexExtraParent(ctx context.Context, lib *sqlite.LibraryRecord, root, cleanTarget string) bool {
	parentDir := resolveExtraParentDir(root, cleanTarget)
	parents := s.findExtraParentItems(ctx, lib.ID, parentDir)
	if len(parents) == 0 {
		return false
	}
	for _, parentRec := range parents {
		if ctx.Err() != nil {
			return false
		}
		mediaBaseName := ""
		if strings.EqualFold(parentRec.Type, "Movie") && strings.TrimSpace(parentRec.Path) != "" {
			mediaBaseName = strings.TrimSuffix(filepath.Base(parentRec.Path), filepath.Ext(parentRec.Path))
		}
		s.scanAndIndexExtras(ctx, lib, parentRec, parentDir, mediaBaseName)
	}
	return true
}

func (s *Scanner) runPhaseA(ctx context.Context, lib *sqlite.LibraryRecord, roots []string, discovered map[string]struct{}, stats *ScanStats) ([]enrichTask, bool, error) {
	var allTasks []enrichTask
	walkHadError := false
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		tasks, hadErr, err := s.runPhaseAWithRoot(ctx, lib, root, root, discovered, stats)
		if err != nil {
			return allTasks, true, err
		}
		if hadErr {
			walkHadError = true
		}
		allTasks = append(allTasks, tasks...)
	}
	return allTasks, walkHadError, nil
}

func (s *Scanner) runPhaseAWithRoot(ctx context.Context, lib *sqlite.LibraryRecord, logicalRoot, walkRoot string, discovered map[string]struct{}, stats *ScanStats) ([]enrichTask, bool, error) {
	forceTV := strings.EqualFold(lib.CollectionType, "tvshows")
	seriesCache := make(map[string]*sqlite.ItemRecord)
	seasonCache := make(map[string]*sqlite.ItemRecord)
	artistCache := make(map[string]*sqlite.ItemRecord)
	albumCache := make(map[string]*sqlite.ItemRecord)
	seenSeriesEnrich := make(map[string]struct{})
	seenSeriesExtras := make(map[string]struct{})
	policy := s.currentSymlinkPolicy()

	var tasks []enrichTask
	batch := newPhaseABatch(s, lib.ID, stats, &tasks)
	walkErr := WalkSafe(walkRoot, policy, func(path string, info os.FileInfo) error {
		if err := s.waitIfPaused(ctx); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		name := info.Name()
		if path != filepath.Clean(walkRoot) {
			if linfo, lerr := os.Lstat(path); lerr == nil && linfo.Mode()&os.ModeSymlink != 0 {
				if !isPathWithinRoot(logicalRoot, path) {
					if info.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
			}
		}
		if info.IsDir() {
			if path != filepath.Clean(walkRoot) && strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, ".") {
			return nil
		}
		if !IsVideoFile(path) && !IsStrmFile(path) && !IsAudioFile(path) {
			return nil
		}
		if !strings.EqualFold(lib.CollectionType, "music") {
			if _, isExtra := ClassifyExtraFile(logicalRoot, path); isExtra {
				if discovered != nil {
					discovered[filepath.Clean(path)] = struct{}{}
				}
				return nil
			}
		}

		if discovered != nil {
			discovered[filepath.Clean(path)] = struct{}{}
		}
		itemTasks, err := s.processDiscoveredFile(ctx, lib, logicalRoot, path, forceTV, seriesCache, seasonCache, artistCache, albumCache, seenSeriesEnrich, seenSeriesExtras, batch, stats)
		if err != nil {
			return err
		}
		tasks = append(tasks, itemTasks...)
		return nil
	})
	if walkErr != nil {
		if ctx.Err() != nil {
			batch.reset()
			return tasks, true, ctx.Err()
		}
		stats.Errors++
		_ = batch.flush(ctx)
		return tasks, true, nil
	}
	if err := batch.flush(ctx); err != nil {
		if ctx.Err() != nil {
			return tasks, true, ctx.Err()
		}
		return tasks, false, nil
	}
	return tasks, false, nil
}

func (s *Scanner) processDiscoveredFile(
	ctx context.Context,
	lib *sqlite.LibraryRecord,
	root, path string,
	forceTV bool,
	seriesCache, seasonCache, artistCache, albumCache map[string]*sqlite.ItemRecord,
	seenSeriesEnrich map[string]struct{},
	seenSeriesExtras map[string]struct{},
	batch *phaseABatch,
	stats *ScanStats,
) ([]enrichTask, error) {
	var (
		changed  bool
		deferred bool
		tasks    []enrichTask
		err      error
	)
	if IsAudioFile(path) {
		var task *enrichTask
		changed, task, err = s.indexAudio(ctx, lib, root, path, artistCache, albumCache)
		if task != nil {
			tasks = append(tasks, *task)
		}
	} else {
		epInfo := ParseEpisodePath(root, path, forceTV)
		if epInfo.IsEpisode {
			changed, deferred, tasks, err = s.indexEpisode(ctx, lib, epInfo, path, seriesCache, seasonCache, seenSeriesEnrich, seenSeriesExtras, batch)
		} else {
			var task *enrichTask
			changed, deferred, task, err = s.indexMovie(ctx, lib, path, batch)
			if task != nil {
				tasks = append(tasks, *task)
			}
		}
	}
	if deferred {
		if err != nil && ctx.Err() != nil {
			return nil, err
		}
		return nil, nil
	}
	if err != nil {
		stats.Errors++
		s.recordDiscoveryItem(lib.ID, filepath.Base(path), false, false, true)
		return nil, nil
	}
	if changed {
		stats.ItemsAdded++
		s.recordDiscoveryItem(lib.ID, filepath.Base(path), true, false, false)
	} else {
		stats.ItemsSkipped++
		s.recordDiscoveryItem(lib.ID, filepath.Base(path), false, true, false)
	}
	return tasks, nil
}

func (s *Scanner) runPhaseB(ctx context.Context, libraryID string, tasks []enrichTask) int {
	if len(tasks) == 0 {
		return 0
	}

	s.startEnrichPhase(libraryID, len(tasks))

	var wave1, wave2 []enrichTask
	for _, t := range tasks {
		if strings.EqualFold(t.ItemType, "Season") || strings.EqualFold(t.ItemType, "Episode") {
			wave2 = append(wave2, t)
		} else {
			wave1 = append(wave1, t)
		}
	}

	pool := s.budgetPool()
	var enriched atomic.Int64

	// Wave 1: Series and Movie tasks (populates parent Series TMDB IDs in SQLite).
	s.runEnrichWave(ctx, libraryID, pool, wave1, &enriched)

	// Wave 2: Season and Episode tasks (can read parent Series TMDB IDs from SQLite).
	if ctx.Err() == nil {
		s.runEnrichWave(ctx, libraryID, pool, wave2, &enriched)
	}

	n := int(enriched.Load())
	if n > 0 {
		s.publishLibraryChanged(ctx, libraryID, ScanStats{
			LibraryID:    libraryID,
			ItemsUpdated: n,
		}, "enrichment")
	}
	return n
}

func (s *Scanner) runEnrichWave(ctx context.Context, libraryID string, pool *budget.Pool, tasks []enrichTask, enriched *atomic.Int64) {
	if len(tasks) == 0 {
		return
	}
	workers := int(pool.Capacity())
	if workers < 1 {
		workers = 1
	}
	if workers > len(tasks) {
		workers = len(tasks)
	}

	taskCh := make(chan enrichTask, len(tasks))
	for _, t := range tasks {
		taskCh <- t
	}
	close(taskCh)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range taskCh {
				if err := s.waitIfPaused(ctx); err != nil {
					return
				}
				if ctx.Err() != nil {
					return
				}
				err := pool.Do(ctx, 1, func(workCtx context.Context) error {
					itemUpdated := s.enrichItem(workCtx, task)
					if itemUpdated {
						enriched.Add(1)
					}
					s.recordEnrichDone(libraryID, itemUpdated)
					return nil
				})
				if err != nil {
					return
				}
			}
		}()
	}
	wg.Wait()
}

func (s *Scanner) enrichItem(ctx context.Context, task enrichTask) bool {
	if ctx.Err() != nil {
		return false
	}

	itemLabel := task.DisplayName
	if itemLabel == "" {
		itemLabel = task.Query.Title
	}
	if itemLabel == "" {
		itemLabel = filepath.Base(task.MediaPath)
	}

	var updated bool

	var probeRes *ProbeResult
	if task.NeedProbe {
		s.updateEnrichCurrent(task.LibraryID, itemLabel, "探测媒体流信息")
		probeRes = s.probeMedia(ctx, task.MediaPath, task.IsStrm)
		if probeRes != nil && (len(probeRes.Streams) > 0 || len(probeRes.Tags) > 0 || probeRes.HasAttachedPic || len(probeRes.Chapters) > 0) {
			rec, err := s.repo.GetItemRecordByID(ctx, task.ItemID)
			if err == nil && rec != nil {
				if len(probeRes.Streams) > 0 {
					ms := s.buildMediaSourceInRoot(task.LibRoot, rec, task.MediaPath, task.Size, task.Mtime, task.IsStrm, task.Container, probeRes)
					if err := s.repo.UpsertMediaSourceAndStreams(ctx, rec.ID, ms); err == nil {
						updated = true
					}
				}
				recDirty := false
				if !task.IsAltMediaSource && probeRes.RuntimeTicks > 0 {
					rt := probeRes.RuntimeTicks
					rec.RuntimeTicks = &rt
					recDirty = true
				}
				if !task.IsAltMediaSource && len(probeRes.Chapters) > 0 {
					chapters := make([]sqlite.ChapterRecord, len(probeRes.Chapters))
					for idx, ch := range probeRes.Chapters {
						chapters[idx] = sqlite.ChapterRecord{
							StartPositionTicks: ch.StartPositionTicks,
							Name:               ch.Name,
						}
					}
					rec.Extra.Chapters = chapters
					recDirty = true
				}
				isAudio := strings.EqualFold(rec.Type, "Audio") || strings.EqualFold(rec.MediaType, "Audio")
				if !task.IsAltMediaSource && isAudio && len(probeRes.Tags) > 0 {
					if applyAudioProbeTags(rec, probeRes.Tags) {
						recDirty = true
					}
				}
				if recDirty {
					if err := s.repo.UpsertItem(ctx, rec); err == nil {
						updated = true
						if isAudio {
							artistStr := strings.Join(rec.Extra.Artists, " ")
							titleTokens := TokenizeForFTS(rec.Name, rec.Extra.Album, artistStr)
							_ = s.repo.UpsertSearchFTS(ctx, rec.ID, rec.Name, titleTokens, "", "", artistStr)
						}
					}
				}
				if !task.IsAltMediaSource && !task.IsStrm && isAudio && probeRes.HasAttachedPic {
					if s.extractAudioAttachedPic(ctx, rec, task.MediaPath) {
						updated = true
					}
				}
			}
		}
	}

	if task.NeedOnlineMeta && !task.IsAltMediaSource {
		existingRec, err := s.repo.GetItemRecordByID(ctx, task.ItemID)
		if err == nil && existingRec != nil && existingRec.Extra.LockData {
			return updated
		}
		s.updateEnrichCurrent(task.LibraryID, itemLabel, "刮削元数据与下载海报")
		if (strings.EqualFold(task.ItemType, "Season") || strings.EqualFold(task.ItemType, "Episode")) && task.ParentSeriesID != "" {
			s.applyParentSeriesQueryContext(ctx, task.ParentSeriesID, &task.Query)
		}
		onlineMeta := s.fetchMetadataFiltered(ctx, task.Query, false)
		if onlineMeta != nil {
			rec, err := s.repo.GetItemRecordByID(ctx, task.ItemID)
			if err == nil && rec != nil && !rec.Extra.LockData {
				applyOnlineMetadataToItem(rec, onlineMeta, task.HasLocalTitle)
				if err := s.repo.UpsertItem(ctx, rec); err == nil {
					mergedAway := false
					if strings.EqualFold(rec.Type, "Movie") && !rec.Extra.LockData {
						mergedAway = s.tryAutoMergeMovieByProviderID(ctx, rec)
					}
					if !mergedAway {
						relMeta := filterLockedRelations(rec, onlineMeta)
						s.persistMetaRelationsAndFTS(ctx, rec, relMeta)
						s.downloadAndSaveRemoteImages(ctx, rec, onlineMeta)
						s.saveLocalNFOIfEnabled(ctx, rec, relMeta, false)
						if strings.EqualFold(rec.Type, "Movie") && onlineMeta.Collection != nil {
							s.ensureAutoBoxSet(ctx, rec, onlineMeta.Collection)
						}
					}
					updated = true
				}
			}
		}
	}

	return updated
}

func (s *Scanner) extractAudioAttachedPic(ctx context.Context, rec *sqlite.ItemRecord, mediaPath string) bool {
	if rec == nil || strings.TrimSpace(mediaPath) == "" || s.repo == nil {
		return false
	}

	needTrackImg := s.isMissingPrimaryImage(ctx, rec.ID)
	var albumRec *sqlite.ItemRecord
	albumID := strings.TrimSpace(rec.Extra.AlbumID)
	if albumID == "" {
		albumID = strings.TrimSpace(rec.ParentID)
	}
	if albumID != "" {
		if parent, err := s.repo.GetItemRecordByID(ctx, albumID); err == nil && parent != nil && strings.EqualFold(parent.Type, "MusicAlbum") {
			albumRec = parent
		}
	}
	needAlbumImg := albumRec != nil && s.isMissingPrimaryImage(ctx, albumRec.ID)
	if !needTrackImg && !needAlbumImg {
		return false
	}

	var destPath string
	if needAlbumImg && albumRec != nil {
		if lib, saveLocal := s.shouldSaveLocalMetadata(ctx, albumRec.LibraryID); saveLocal {
			if localPath, ok := s.resolveLocalImagePath(ctx, lib, albumRec, "Primary", ".jpg"); ok && localPath != "" {
				destPath = localPath
			}
		}
		if destPath == "" && !needTrackImg {
			destPath = filepath.Join(s.imageStorageDir(), albumRec.ID, "primary.jpg")
		}
	}
	if destPath == "" {
		destPath = filepath.Join(s.imageStorageDir(), rec.ID, "primary.jpg")
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return false
	}

	absMedia, err := filepath.Abs(strings.TrimSpace(mediaPath))
	if err != nil {
		return false
	}

	unlock := s.lockPicDest(destPath)
	defer unlock()

	if linfo, lerr := os.Lstat(destPath); lerr == nil {
		if linfo.IsDir() || linfo.Mode()&os.ModeSymlink != 0 {
			return false
		}
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(destPath), ".ambra-pic-*.jpg")
	if err != nil {
		return false
	}
	tmpPath := tmpFile.Name()
	_ = tmpFile.Close()
	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	ffmpegPath := "ffmpeg"
	if s.cfg != nil && strings.TrimSpace(s.cfg.FFmpegPath) != "" {
		ffmpegPath = s.cfg.FFmpegPath
	}

	extractCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(
		extractCtx,
		ffmpegPath,
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-protocol_whitelist", probeLocalProtocolWhitelist,
		"-i", absMedia,
		"-map", "0:v:0",
		"-frames:v", "1",
		tmpPath,
	)
	if err := cmd.Run(); err != nil {
		return false
	}
	tmpInfo, err := os.Stat(tmpPath)
	if err != nil || tmpInfo.IsDir() || tmpInfo.Size() == 0 {
		return false
	}
	if linfo, lerr := os.Lstat(destPath); lerr == nil && (linfo.IsDir() || linfo.Mode()&os.ModeSymlink != 0) {
		return false
	}
	if err := os.Rename(tmpPath, destPath); err != nil {
		return false
	}
	cleanupTmp = false

	info, err := os.Stat(destPath)
	if err != nil || info.IsDir() || info.Size() == 0 {
		return false
	}

	tag := computeEtag(destPath, info.Size(), info.ModTime().Unix())
	saved := false
	if needTrackImg {
		if err := s.repo.UpsertImage(ctx, &sqlite.ImageRecord{
			ItemID:     rec.ID,
			ImageType:  "Primary",
			ImageIndex: 0,
			Path:       destPath,
			Tag:        tag,
		}); err == nil {
			saved = true
		}
	}
	if needAlbumImg && albumRec != nil {
		if err := s.repo.UpsertImage(ctx, &sqlite.ImageRecord{
			ItemID:     albumRec.ID,
			ImageType:  "Primary",
			ImageIndex: 0,
			Path:       destPath,
			Tag:        tag,
		}); err == nil {
			saved = true
		}
	}
	return saved
}

func (s *Scanner) tryAutoMergeMovieByProviderID(ctx context.Context, rec *sqlite.ItemRecord) bool {
	tmdbID := getProviderID(rec.Extra.ProviderIDs, "Tmdb")
	if tmdbID == "" {
		return false
	}
	s.mergeMu.Lock()
	defer s.mergeMu.Unlock()

	matches, err := s.repo.FindItemsByProviderID(ctx, rec.LibraryID, "Movie", "Tmdb", tmdbID)
	if err != nil || len(matches) < 2 {
		return false
	}

	ids := make([]string, 0, len(matches))
	ids = append(ids, matches[0].ID)
	for _, m := range matches[1:] {
		if m.Extra.LockData {
			continue
		}
		ids = append(ids, m.ID)
	}
	if len(ids) < 2 {
		return false
	}

	primaryID, mergedCount, err := s.repo.MergeItemVersions(ctx, ids)
	if err != nil || mergedCount == 0 {
		return false
	}
	return primaryID != rec.ID
}

func (s *Scanner) ensureAutoBoxSet(ctx context.Context, movie *sqlite.ItemRecord, col *plugins.CollectionMeta) {
	if movie == nil || col == nil || s.repo == nil {
		return
	}
	colID := strings.TrimSpace(col.ID)
	colName := strings.TrimSpace(col.Name)
	if colName == "" {
		return
	}

	providerKey := "Tmdb"
	providerVal := colID
	if providerVal == "" {
		providerKey = "NfoSet"
		providerVal = strings.ToLower(colName)
	}

	var boxSet *sqlite.ItemRecord
	s.mergeMu.Lock()
	matches, err := s.repo.FindItemsByProviderID(ctx, "collections", "BoxSet", providerKey, providerVal)
	if err == nil && len(matches) > 0 {
		boxSet = matches[0]
	} else if err == nil {
		now := time.Now().Unix()
		rec := &sqlite.ItemRecord{
			ID:           guid.New(),
			LibraryID:    "collections",
			Type:         "BoxSet",
			IsFolder:     true,
			Name:         colName,
			SortName:     strings.ToLower(colName),
			Etag:         guid.New()[:16],
			Extra:        sqlite.ItemExtra{ProviderIDs: map[string]string{providerKey: providerVal}},
			DateCreated:  now,
			DateModified: now,
		}
		if upsertErr := s.repo.UpsertItem(ctx, rec); upsertErr == nil {
			_ = s.repo.UpsertSearchFTS(ctx, rec.ID, rec.Name, TokenizeForFTS(rec.Name), "", "", "")
			boxSet = rec
		}
	}
	if boxSet != nil {
		_, _ = s.repo.AddItemsToContainer(ctx, boxSet.ID, []string{movie.ID}, true)
	}
	s.mergeMu.Unlock()

	if boxSet == nil {
		return
	}
	if col.PosterURL != "" {
		s.downloadAndSaveRemoteImage(ctx, boxSet.ID, "Primary", col.PosterURL)
	}
	if col.BackdropURL != "" {
		s.downloadAndSaveRemoteImage(ctx, boxSet.ID, "Backdrop", col.BackdropURL)
	}
}

func (s *Scanner) publishLibraryChanged(ctx context.Context, libraryID string, stats ScanStats, phase string) {
	if s.bus == nil {
		return
	}
	s.bus.Publish(ctx, eventbus.Event{
		Type: eventbus.EventLibraryChanged,
		Payload: map[string]any{
			"LibraryId":    libraryID,
			"ItemsAdded":   stats.ItemsAdded,
			"ItemsUpdated": stats.ItemsUpdated,
			"ItemsDeleted": stats.ItemsDeleted,
			"Phase":        phase,
		},
	})
}

func isLocalMetadataProvider(p plugins.MetadataProvider) bool {
	id := strings.ToLower(p.Manifest().ID)
	return id == "ambra.builtin.nfo" || strings.Contains(id, ".nfo") || strings.Contains(id, "local")
}

func (s *Scanner) hasOnlineMetadataProviders() bool {
	if s.registry == nil {
		return false
	}
	for _, p := range s.registry.MetadataProviders() {
		if !isLocalMetadataProvider(p) {
			return true
		}
	}
	return false
}

func (s *Scanner) fetchMetadataFiltered(ctx context.Context, q plugins.MediaQuery, localOnly bool) *plugins.MetadataResult {
	if s.registry == nil {
		return nil
	}
	providers := s.registry.MetadataProviders()
	if len(providers) == 0 {
		return nil
	}
	var merged *plugins.MetadataResult
	for _, p := range providers {
		if localOnly != isLocalMetadataProvider(p) {
			continue
		}
		res, err := p.FetchMetadata(ctx, q)
		if err != nil || res == nil {
			continue
		}
		if merged == nil {
			cp := *res
			if cp.ProviderIDs == nil {
				cp.ProviderIDs = make(map[string]string)
			}
			merged = &cp
			if len(merged.ProviderIDs) > 0 {
				if q.ProviderIDs == nil {
					q.ProviderIDs = make(map[string]string)
				}
				for k, v := range merged.ProviderIDs {
					q.ProviderIDs[k] = v
				}
			}
			continue
		}
		mergeMetadata(merged, res)
	}
	return merged
}

func isFieldLocked(rec *sqlite.ItemRecord, field string) bool {
	if rec == nil {
		return false
	}
	if rec.Extra.LockData {
		return true
	}
	for _, f := range rec.Extra.LockedFields {
		if strings.EqualFold(strings.TrimSpace(f), field) {
			return true
		}
	}
	return false
}

// applyOnlineMetadataToItem applies online metadata fields while respecting local NFO titles and locked fields.
func applyOnlineMetadataToItem(rec *sqlite.ItemRecord, meta *plugins.MetadataResult, hasLocalTitle bool) {
	if rec == nil || meta == nil || rec.Extra.LockData {
		return
	}
	if meta.Name != "" && !hasLocalTitle && !isFieldLocked(rec, "Name") {
		rec.Name = meta.Name
		rec.SortName = strings.ToLower(meta.Name)
	}
	if meta.OriginalTitle != "" && rec.OriginalTitle == "" && !isFieldLocked(rec, "OriginalTitle") {
		rec.OriginalTitle = meta.OriginalTitle
	}
	if meta.Overview != "" && rec.Extra.Overview == "" && !isFieldLocked(rec, "Overview") {
		rec.Extra.Overview = meta.Overview
	}
	if meta.Tagline != "" && len(rec.Extra.Taglines) == 0 && !isFieldLocked(rec, "Taglines") {
		rec.Extra.Taglines = []string{meta.Tagline}
	}
	if meta.Year > 0 && rec.ProductionYear == nil && !isFieldLocked(rec, "ProductionYear") {
		y := meta.Year
		rec.ProductionYear = &y
	}
	if meta.PremiereDate != "" && rec.PremiereDate == nil && !isFieldLocked(rec, "PremiereDate") {
		if t, err := time.Parse("2006-01-02", meta.PremiereDate); err == nil {
			unix := t.Unix()
			rec.PremiereDate = &unix
		}
	}
	if meta.CommunityRating > 0 && rec.CommunityRating == nil && !isFieldLocked(rec, "CommunityRating") {
		r := meta.CommunityRating
		rec.CommunityRating = &r
	}
	if meta.OfficialRating != "" && rec.OfficialRating == "" && !isFieldLocked(rec, "OfficialRating") {
		rec.OfficialRating = meta.OfficialRating
	}
	if len(meta.ProviderIDs) > 0 {
		if rec.Extra.ProviderIDs == nil {
			rec.Extra.ProviderIDs = make(map[string]string)
		}
		for k, v := range meta.ProviderIDs {
			if getProviderID(rec.Extra.ProviderIDs, k) == "" && strings.TrimSpace(v) != "" {
				rec.Extra.ProviderIDs[k] = strings.TrimSpace(v)
			}
		}
	}
	if meta.LockData {
		rec.Extra.LockData = true
	}
}
