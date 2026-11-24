package scanner

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
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

type enrichTask struct {
	ItemID           string
	LibraryID        string
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
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// CancelLibrary cancels an in-progress scan for libraryID.
func (s *Scanner) CancelLibrary(libraryID string) {
	s.CancelScan(libraryID)
}

// CancelAll cancels all in-progress library scans and clears paused state.
func (s *Scanner) CancelAll() {
	s.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(s.cancels))
	for _, cancel := range s.cancels {
		if cancel != nil {
			cancels = append(cancels, cancel)
		}
	}
	s.mu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}
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
		go func() {
			defer releaseScan()
			stats.ItemsUpdated += s.runPhaseB(scanCtx, lib.ID, tasks)
			s.notifyScanComplete(scanCtx, lib.ID, stats)
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

	root := findContainingRoot(lib.Paths, cleanTarget)
	forceTV := strings.EqualFold(lib.CollectionType, "tvshows")
	seriesCache := make(map[string]*sqlite.ItemRecord)
	seasonCache := make(map[string]*sqlite.ItemRecord)
	artistCache := make(map[string]*sqlite.ItemRecord)
	albumCache := make(map[string]*sqlite.ItemRecord)
	seenSeriesEnrich := make(map[string]struct{})

	var tasks []enrichTask
	if !info.IsDir() {
		if !strings.HasPrefix(filepath.Base(cleanTarget), ".") && (IsVideoFile(cleanTarget) || IsStrmFile(cleanTarget) || IsAudioFile(cleanTarget)) {
			if err := s.waitIfPaused(ctx); err != nil {
				return stats, err
			}
			itemTasks := s.processDiscoveredFile(ctx, lib, root, cleanTarget, forceTV, seriesCache, seasonCache, artistCache, albumCache, seenSeriesEnrich, &stats)
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
	for _, r := range roots {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		cr := filepath.Clean(r)
		if cleanTarget == cr || strings.HasPrefix(cleanTarget, cr+string(filepath.Separator)) {
			return cr
		}
	}
	return filepath.Dir(cleanTarget)
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
	policy := s.currentSymlinkPolicy()

	var tasks []enrichTask
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

		if discovered != nil {
			discovered[filepath.Clean(path)] = struct{}{}
		}
		itemTasks := s.processDiscoveredFile(ctx, lib, logicalRoot, path, forceTV, seriesCache, seasonCache, artistCache, albumCache, seenSeriesEnrich, stats)
		tasks = append(tasks, itemTasks...)
		return nil
	})
	if walkErr != nil {
		if ctx.Err() != nil {
			return tasks, true, ctx.Err()
		}
		stats.Errors++
		return tasks, true, nil
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
	stats *ScanStats,
) []enrichTask {
	var (
		changed bool
		tasks   []enrichTask
		err     error
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
			changed, tasks, err = s.indexEpisode(ctx, lib, epInfo, path, seriesCache, seasonCache, seenSeriesEnrich)
		} else {
			var task *enrichTask
			changed, task, err = s.indexMovie(ctx, lib, path)
			if task != nil {
				tasks = append(tasks, *task)
			}
		}
	}
	if err != nil {
		stats.Errors++
		s.recordDiscoveryItem(lib.ID, filepath.Base(path), false, false, true)
		return nil
	}
	if changed {
		stats.ItemsAdded++
		s.recordDiscoveryItem(lib.ID, filepath.Base(path), true, false, false)
	} else {
		stats.ItemsSkipped++
		s.recordDiscoveryItem(lib.ID, filepath.Base(path), false, true, false)
	}
	return tasks
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
		if probeRes != nil && (len(probeRes.Streams) > 0 || len(probeRes.Tags) > 0) {
			rec, err := s.repo.GetItemRecordByID(ctx, task.ItemID)
			if err == nil && rec != nil {
				if len(probeRes.Streams) > 0 {
					ms := s.buildMediaSource(rec, task.MediaPath, task.Size, task.Mtime, task.IsStrm, task.Container, probeRes)
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
				if !task.IsAltMediaSource && (strings.EqualFold(rec.Type, "Audio") || strings.EqualFold(rec.MediaType, "Audio")) && len(probeRes.Tags) > 0 {
					if applyAudioProbeTags(rec, probeRes.Tags) {
						recDirty = true
					}
				}
				if recDirty {
					if err := s.repo.UpsertItem(ctx, rec); err == nil {
						updated = true
						if strings.EqualFold(rec.Type, "Audio") || strings.EqualFold(rec.MediaType, "Audio") {
							artistStr := strings.Join(rec.Extra.Artists, " ")
							titleTokens := TokenizeForFTS(rec.Name, rec.Extra.Album, artistStr)
							_ = s.repo.UpsertSearchFTS(ctx, rec.ID, rec.Name, titleTokens, "", "", artistStr)
						}
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
		if (strings.EqualFold(task.ItemType, "Season") || strings.EqualFold(task.ItemType, "Episode")) &&
			task.ParentSeriesID != "" && getProviderID(task.Query.ProviderIDs, "tmdb") == "" {
			if parent, err := s.repo.GetItemRecordByID(ctx, task.ParentSeriesID); err == nil && parent != nil && len(parent.Extra.ProviderIDs) > 0 {
				ids := cloneStringMap(task.Query.ProviderIDs)
				if ids == nil {
					ids = make(map[string]string, len(parent.Extra.ProviderIDs)+1)
				}
				for k, v := range parent.Extra.ProviderIDs {
					ids[k] = v
				}
				if tmdbID := getProviderID(parent.Extra.ProviderIDs, "tmdb"); tmdbID != "" {
					ids["tmdb"] = tmdbID
				}
				task.Query.ProviderIDs = ids
			}
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
					}
					updated = true
				}
			}
		}
	}

	return updated
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
			if _, exists := rec.Extra.ProviderIDs[k]; !exists {
				rec.Extra.ProviderIDs[k] = v
			}
		}
	}
}
