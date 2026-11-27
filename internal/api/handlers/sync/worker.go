package sync

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/core/budget"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/pkg/netutil"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

// SetBudget attaches a background budget Pool to throttle concurrent offline sync transcodes.
func (h *Handler) SetBudget(pool *budget.Pool) {
	h.mu.Lock()
	h.budgetPool = pool
	h.mu.Unlock()
}

// SetFFmpegPath overrides the FFmpeg binary path used by the background sync transcode worker.
func (h *Handler) SetFFmpegPath(path string) {
	h.mu.Lock()
	h.ffmpegPath = strings.TrimSpace(path)
	h.mu.Unlock()
}

// SetStreamResolverWithHeaders attaches an optional stream resolver for remote sources.
func (h *Handler) SetStreamResolverWithHeaders(fn func(ctx context.Context, rawPath string) (*plugins.StreamResolveResult, error)) {
	h.mu.Lock()
	h.streamResolverWithHeaders = fn
	h.mu.Unlock()
}

func (h *Handler) getBudgetPool() *budget.Pool {
	h.mu.RLock()
	p := h.budgetPool
	h.mu.RUnlock()
	return p
}

func (h *Handler) resolveFFmpegBinary() string {
	h.mu.RLock()
	override := h.ffmpegPath
	cfg := h.cfg
	h.mu.RUnlock()

	candidate := strings.TrimSpace(override)
	if candidate == "" && cfg != nil {
		candidate = strings.TrimSpace(cfg.Snapshot().FFmpegPath)
	}
	if candidate == "" {
		candidate = "ffmpeg"
	}
	resolved, err := exec.LookPath(candidate)
	if err != nil {
		return ""
	}
	return resolved
}

func (h *Handler) isWorkerRunning() bool {
	h.workerMu.Lock()
	running := h.workerRunning
	h.workerMu.Unlock()
	return running
}

func (h *Handler) canQueueTranscode(srcPath string) bool {
	if !h.isWorkerRunning() {
		return false
	}
	srcPath = strings.TrimSpace(srcPath)
	if srcPath == "" {
		return false
	}
	if !isSyncHTTPURL(srcPath) {
		fi, err := os.Stat(srcPath)
		if err != nil || fi.IsDir() {
			return false
		}
	}
	return h.resolveFFmpegBinary() != ""
}

func needsSyncTranscode(quality string, bitrate int) (bool, int) {
	q := strings.ToLower(strings.TrimSpace(quality))
	if bitrate <= 0 && (q == "" || q == "original") {
		return false, 0
	}
	if bitrate > 0 {
		if bitrate > maxSyncBitrate {
			bitrate = maxSyncBitrate
		}
		return true, bitrate
	}
	switch q {
	case "high":
		return true, 8_000_000
	case "medium":
		return true, 4_000_000
	case "low":
		return true, 1_500_000
	default:
		if n, err := strconv.Atoi(q); err == nil && n > 0 {
			if n > maxSyncBitrate {
				n = maxSyncBitrate
			}
			return true, n
		}
		return true, 4_000_000
	}
}

func isSyncHTTPURL(raw string) bool {
	low := strings.ToLower(strings.TrimSpace(raw))
	return strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://")
}

// StartWorker launches the background offline sync transcoding worker loop.
// Safe to call multiple times; subsequent calls while running are no-ops.
func (h *Handler) StartWorker(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	h.workerMu.Lock()
	if h.workerRunning {
		h.workerMu.Unlock()
		return
	}
	wCtx, cancel := context.WithCancel(ctx)
	wake := make(chan struct{}, 1)
	h.workerRunning = true
	h.workerCancel = cancel
	h.workerWake = wake
	h.activeCancels = make(map[string]context.CancelFunc)
	h.workerWg.Add(1)
	h.workerMu.Unlock()

	// Recover any interrupted "Converting" items from a previous run back to "Queued".
	hasQueued := false
	h.mu.Lock()
	changed := false
	for i := range h.state.JobItems {
		if h.state.JobItems[i].Status == "Converting" {
			h.state.JobItems[i].Status = "Queued"
			h.state.JobItems[i].Progress = 0
			h.recomputeJobLocked(h.state.JobItems[i].JobId)
			changed = true
		}
		if h.state.JobItems[i].Status == "Queued" {
			hasQueued = true
		}
	}
	if changed {
		_ = h.saveStateLocked(wCtx)
	}
	h.mu.Unlock()

	go func() {
		defer h.workerWg.Done()
		h.runWorkerLoop(wCtx, wake)
	}()

	if hasQueued {
		h.wakeWorker()
	}
}

// StopWorker gracefully stops the background sync transcoding worker and waits for in-flight processes to exit.
func (h *Handler) StopWorker() {
	h.workerMu.Lock()
	if !h.workerRunning {
		h.workerMu.Unlock()
		return
	}
	cancel := h.workerCancel
	cancels := make([]context.CancelFunc, 0, len(h.activeCancels))
	for _, fn := range h.activeCancels {
		if fn != nil {
			cancels = append(cancels, fn)
		}
	}
	h.activeCancels = nil
	h.workerRunning = false
	h.workerCancel = nil
	h.workerWake = nil
	h.workerMu.Unlock()

	for _, fn := range cancels {
		fn()
	}
	if cancel != nil {
		cancel()
	}
	h.workerWg.Wait()
}

func (h *Handler) wakeWorker() {
	h.workerMu.Lock()
	wake := h.workerWake
	running := h.workerRunning
	h.workerMu.Unlock()
	if !running || wake == nil {
		return
	}
	select {
	case wake <- struct{}{}:
	default:
	}
}

func (h *Handler) registerActiveCancel(jobItemID string, cancel context.CancelFunc) {
	h.workerMu.Lock()
	if h.workerRunning && h.activeCancels != nil {
		h.activeCancels[jobItemID] = cancel
	} else if cancel != nil {
		cancel()
	}
	h.workerMu.Unlock()
}

func (h *Handler) unregisterActiveCancel(jobItemID string) {
	h.workerMu.Lock()
	if h.activeCancels != nil {
		delete(h.activeCancels, jobItemID)
	}
	h.workerMu.Unlock()
}

func (h *Handler) cancelActiveTranscodeItems(jobItemIDs ...string) {
	if len(jobItemIDs) == 0 {
		return
	}
	h.workerMu.Lock()
	var fns []context.CancelFunc
	for _, id := range jobItemIDs {
		if h.activeCancels != nil {
			if fn, ok := h.activeCancels[id]; ok {
				delete(h.activeCancels, id)
				if fn != nil {
					fns = append(fns, fn)
				}
			}
		}
	}
	h.workerMu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

func (h *Handler) syncCacheDir() string {
	if h.cfg == nil {
		return ""
	}
	dataDir := strings.TrimSpace(h.cfg.Snapshot().DataDir)
	if dataDir == "" {
		dataDir = strings.TrimSpace(h.cfg.DataDir)
	}
	if dataDir == "" {
		return ""
	}
	return filepath.Join(dataDir, "cache", "sync")
}

func (h *Handler) resolveSafeSyncCacheTarget(target string) (string, bool) {
	cacheDir := h.syncCacheDir()
	target = strings.TrimSpace(target)
	if cacheDir == "" || target == "" {
		return "", false
	}
	realCache, err := filepath.EvalSymlinks(cacheDir)
	if err != nil {
		realCache, err = filepath.Abs(cacheDir)
		if err != nil {
			return "", false
		}
	}
	realCache = filepath.Clean(realCache)

	realTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		parentReal, pErr := filepath.EvalSymlinks(filepath.Dir(target))
		if pErr != nil {
			return "", false
		}
		realTarget = filepath.Join(parentReal, filepath.Base(target))
	}
	realTarget = filepath.Clean(realTarget)

	rel, err := filepath.Rel(realCache, realTarget)
	if err != nil {
		return "", false
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	return realTarget, true
}

func (h *Handler) cleanupSyncCacheFiles(paths ...string) {
	for _, p := range paths {
		if safePath, ok := h.resolveSafeSyncCacheTarget(p); ok {
			_ = os.Remove(safePath)
			_ = os.Remove(safePath + ".tmp")
		}
	}
}

func (h *Handler) cleanupSyncJobCacheDir(jobID string) {
	cacheDir := h.syncCacheDir()
	jobID = strings.TrimSpace(jobID)
	if cacheDir == "" || jobID == "" || strings.ContainsAny(jobID, `/\`) || jobID == "." || jobID == ".." {
		return
	}
	jobDir := filepath.Join(cacheDir, jobID)
	if safeDir, ok := h.resolveSafeSyncCacheTarget(jobDir); ok {
		_ = os.RemoveAll(safeDir)
	}
}

type queuedSyncWork struct {
	job       SyncJob
	jobItem   SyncJobItem
	srcPath   string
	mediaType string
	bitrate   int
}

func (h *Handler) nextQueuedWork(ctx context.Context) (*queuedSyncWork, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	jobByID := make(map[string]SyncJob, len(h.state.Jobs))
	for _, j := range h.state.Jobs {
		jobByID[j.Id] = j
	}

	for i := range h.state.JobItems {
		ji := &h.state.JobItems[i]
		if ji.Status != "Queued" {
			continue
		}
		j, ok := jobByID[ji.JobId]
		if !ok || strings.EqualFold(j.Status, "Cancelled") {
			continue
		}
		srcPath := strings.TrimSpace(ji.SourcePath)
		mediaType := strings.TrimSpace(ji.MediaType)
		if srcPath == "" {
			if libRepo := h.libRepo; libRepo != nil && ji.ItemId != "" {
				if rec, err := libRepo.GetItemRecordByID(ctx, ji.ItemId); err == nil && rec != nil {
					srcPath = strings.TrimSpace(rec.Path)
					if mediaType == "" {
						mediaType = rec.MediaType
					}
				}
			}
		}
		_, targetBitrate := needsSyncTranscode(j.Quality, j.Bitrate)
		if targetBitrate <= 0 {
			targetBitrate = 4_000_000
		}
		if srcPath == "" {
			// Virtual placeholder item without physical source: mark ready directly so queue never stalls.
			ji.Status = "ReadyToTransfer"
			ji.Progress = 100
			h.recomputeJobLocked(j.Id)
			_ = h.saveStateLocked(ctx)
			continue
		}
		return &queuedSyncWork{
			job:       j,
			jobItem:   *ji,
			srcPath:   srcPath,
			mediaType: mediaType,
			bitrate:   targetBitrate,
		}, true
	}
	return nil, false
}

func (h *Handler) runWorkerLoop(ctx context.Context, wake <-chan struct{}) {
	for {
		if ctx.Err() != nil {
			return
		}
		for {
			if ctx.Err() != nil {
				return
			}
			work, ok := h.nextQueuedWork(ctx)
			if !ok {
				break
			}
			h.processQueuedSyncItem(ctx, work)
		}
		select {
		case <-ctx.Done():
			return
		case <-wake:
		}
	}
}

func (h *Handler) processQueuedSyncItem(ctx context.Context, work *queuedSyncWork) {
	if work == nil {
		return
	}
	pool := h.getBudgetPool()
	if pool != nil {
		if err := pool.Acquire(ctx, 1); err != nil {
			return
		}
		defer pool.Release(1)
	}

	ffmpegBin := h.resolveFFmpegBinary()
	if ffmpegBin == "" {
		// Smooth fallback when FFmpeg becomes unavailable at runtime.
		h.mu.Lock()
		for i := range h.state.JobItems {
			if h.state.JobItems[i].Id == work.jobItem.Id && h.state.JobItems[i].Status == "Queued" {
				h.state.JobItems[i].OutputPath = work.srcPath
				h.state.JobItems[i].Status = "ReadyToTransfer"
				h.state.JobItems[i].Progress = 100
				h.recomputeJobLocked(work.job.Id)
				_ = h.saveStateLocked(ctx)
				break
			}
		}
		h.mu.Unlock()
		return
	}

	itemCtx, itemCancel := context.WithTimeout(ctx, h.getTranscodeTimeout())
	h.registerActiveCancel(work.jobItem.Id, itemCancel)
	defer func() {
		h.unregisterActiveCancel(work.jobItem.Id)
		itemCancel()
	}()

	// Transition Queued -> Converting
	stillActive := false
	h.mu.Lock()
	for i := range h.state.JobItems {
		if h.state.JobItems[i].Id == work.jobItem.Id && h.state.JobItems[i].Status == "Queued" {
			h.state.JobItems[i].Status = "Converting"
			h.state.JobItems[i].Progress = 10
			h.recomputeJobLocked(work.job.Id)
			_ = h.saveStateLocked(itemCtx)
			stillActive = true
			break
		}
	}
	h.mu.Unlock()
	if !stillActive || itemCtx.Err() != nil {
		return
	}

	cacheDir := h.syncCacheDir()
	if cacheDir == "" {
		h.markSyncItemFailed(ctx, work.job.Id, work.jobItem.Id, "missing DataDir for sync cache")
		return
	}
	jobDir := filepath.Join(cacheDir, work.job.Id)
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		h.markSyncItemFailed(ctx, work.job.Id, work.jobItem.Id, err.Error())
		return
	}
	finalOut := filepath.Join(jobDir, work.jobItem.Id+".mp4")
	tmpOut := finalOut + ".tmp"
	_ = os.Remove(tmpOut)

	ffInput := work.srcPath
	var inputHeaders map[string]string
	if strings.EqualFold(filepath.Ext(ffInput), ".strm") {
		h.mu.RLock()
		resolver := h.streamResolverWithHeaders
		h.mu.RUnlock()
		if resolver != nil {
			if res, err := resolver(itemCtx, ffInput); err == nil && res != nil && strings.TrimSpace(res.URL) != "" {
				ffInput = strings.TrimSpace(res.URL)
				inputHeaders = res.Headers
			}
		}
	}
	if isSyncHTTPURL(ffInput) {
		if err := netutil.ValidateRemoteURL(ffInput, true); err != nil {
			h.markSyncItemFailed(ctx, work.job.Id, work.jobItem.Id, err.Error())
			return
		}
		proxy, pErr := transcode.StartLoopbackStreamProxy(transcode.LoopbackProxyOptions{
			TargetURL:       ffInput,
			Headers:         inputHeaders,
			AllowPrivateLAN: true,
		})
		if pErr != nil {
			h.markSyncItemFailed(ctx, work.job.Id, work.jobItem.Id, pErr.Error())
			return
		}
		defer proxy.Close()
		ffInput = proxy.URL()
	}

	args := netutil.FilterFFmpegArgs(ffmpegBin, buildSyncFFmpegArgs(ffInput, tmpOut, work.mediaType, work.bitrate))
	cmd := exec.CommandContext(itemCtx, ffmpegBin, args...)
	cmd.WaitDelay = 500 * time.Millisecond
	outBytes, runErr := cmd.CombinedOutput()
	if itemCtx.Err() != nil {
		_ = os.Remove(tmpOut)
		_ = os.Remove(finalOut)
		if errors.Is(itemCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
			slog.Warn("sync: background ffmpeg transcode timed out",
				"job_id", work.job.Id,
				"job_item_id", work.jobItem.Id,
			)
			h.markSyncItemFailed(ctx, work.job.Id, work.jobItem.Id, "sync transcode timed out")
		}
		return
	}
	if runErr != nil {
		_ = os.Remove(tmpOut)
		slog.Warn("sync: background ffmpeg transcode failed",
			"job_id", work.job.Id,
			"job_item_id", work.jobItem.Id,
			"err", runErr,
			"stderr", strings.TrimSpace(string(outBytes)),
		)
		h.markSyncItemFailed(ctx, work.job.Id, work.jobItem.Id, runErr.Error())
		return
	}
	if fi, stErr := os.Stat(tmpOut); stErr != nil || fi.IsDir() {
		_ = os.Remove(tmpOut)
		h.markSyncItemFailed(ctx, work.job.Id, work.jobItem.Id, "transcode produced no output file")
		return
	}
	if err := os.Rename(tmpOut, finalOut); err != nil {
		_ = os.Remove(tmpOut)
		h.markSyncItemFailed(ctx, work.job.Id, work.jobItem.Id, err.Error())
		return
	}

	// Transition Converting -> ReadyToTransfer with OutputPath pointing to <DataDir>/cache/sync/<jobId>/<jobItemId>.mp4
	updated := false
	h.mu.Lock()
	for i := range h.state.JobItems {
		if h.state.JobItems[i].Id == work.jobItem.Id {
			if h.state.JobItems[i].Status == "Removing" || h.state.JobItems[i].Status == "Cancelled" {
				break
			}
			h.state.JobItems[i].OutputPath = finalOut
			h.state.JobItems[i].Transcoded = true
			h.state.JobItems[i].Status = "ReadyToTransfer"
			h.state.JobItems[i].Progress = 100
			h.recomputeJobLocked(work.job.Id)
			_ = h.saveStateLocked(ctx)
			updated = true
			break
		}
	}
	h.mu.Unlock()
	if !updated {
		// Job or item was deleted while transcoding finished; remove orphan file immediately.
		_ = os.Remove(finalOut)
	}
}

func (h *Handler) markSyncItemFailed(ctx context.Context, jobID, jobItemID, reason string) {
	_ = reason
	h.mu.Lock()
	for i := range h.state.JobItems {
		if h.state.JobItems[i].Id == jobItemID {
			h.state.JobItems[i].Status = "Failed"
			h.state.JobItems[i].Progress = 0
			h.recomputeJobLocked(jobID)
			_ = h.saveStateLocked(ctx)
			break
		}
	}
	h.mu.Unlock()
}

func buildSyncFFmpegArgs(inputPath, outPath, mediaType string, targetBitrate int) []string {
	args := []string{"-hide_banner", "-loglevel", "error", "-y"}
	if isSyncHTTPURL(inputPath) {
		args = append(args,
			"-protocol_whitelist", "http,https,tcp,tls,crypto",
			"-max_redirects", "0",
			"-rw_timeout", "15000000",
		)
	} else {
		args = append(args, "-protocol_whitelist", "file,crypto,data")
	}
	args = append(args, "-i", inputPath)

	isAudio := strings.EqualFold(strings.TrimSpace(mediaType), "Audio")
	if isAudio {
		audioBR := targetBitrate
		if audioBR <= 0 || audioBR > 320_000 {
			audioBR = 192_000
		}
		args = append(args,
			"-vn",
			"-c:a", "aac",
			"-b:a", strconv.Itoa(audioBR),
			"-movflags", "+faststart",
			"-f", "mp4",
			outPath,
		)
		return args
	}

	const audioBitrate = 128_000
	if targetBitrate > maxSyncBitrate {
		targetBitrate = maxSyncBitrate
	}
	videoBitrate := targetBitrate - audioBitrate
	if videoBitrate < 250_000 {
		videoBitrate = 250_000
	}
	args = append(args,
		"-sn",
		"-c:v", "libx264",
		"-preset", "veryfast",
		"-pix_fmt", "yuv420p",
		"-b:v", strconv.Itoa(videoBitrate),
		"-maxrate", strconv.Itoa(videoBitrate),
		"-bufsize", strconv.Itoa(videoBitrate*2),
		"-c:a", "aac",
		"-b:a", strconv.Itoa(audioBitrate),
		"-ac", "2",
		"-movflags", "+faststart",
		"-f", "mp4",
		outPath,
	)
	return args
}

// SubscribeEvents registers an EventLibraryChanged listener on bus so SyncNewContent jobs
// automatically pick up newly scanned library items.
func (h *Handler) SubscribeEvents(bus *eventbus.Bus) {
	if bus == nil {
		return
	}
	bus.Subscribe(eventbus.EventLibraryChanged, func(ctx context.Context, e eventbus.Event) {
		ids := extractSyncEventItemIDs(e.Payload)
		h.OnLibraryItemAdded(ctx, ids...)
	})
}

func extractSyncEventItemIDs(payload any) []string {
	switch v := payload.(type) {
	case string:
		if s := strings.TrimSpace(v); s != "" {
			return []string{s}
		}
	case []string:
		return v
	case map[string]any:
		var out []string
		for _, k := range []string{"ItemId", "item_id", "Id"} {
			if s, ok := v[k].(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		for _, k := range []string{"AddedItemIds", "ItemIds", "ItemsAdded"} {
			if list, ok := v[k].([]string); ok {
				out = append(out, list...)
			} else if rawList, ok := v[k].([]any); ok {
				for _, elem := range rawList {
					if s, ok := elem.(string); ok && strings.TrimSpace(s) != "" {
						out = append(out, strings.TrimSpace(s))
					}
				}
			}
		}
		return out
	}
	return nil
}

// OnLibraryItemAdded evaluates all active SyncNewContent jobs and appends newly added library items
// matching each job's ParentId or RequestedItemIds containers, honoring UnwatchedOnly, ItemLimit, and user RBAC.
func (h *Handler) OnLibraryItemAdded(ctx context.Context, itemIDs ...string) {
	if ctx == nil {
		ctx = context.Background()
	}
	libRepo := h.getLibraryRepo()
	if libRepo == nil {
		return
	}
	userRepo := h.getUserRepo()
	serverID := h.serverID()

	explicitHintSet := make(map[string]struct{}, len(itemIDs))
	for _, raw := range itemIDs {
		if norm := guid.Normalize(raw); norm != "" {
			explicitHintSet[norm] = struct{}{}
		}
	}

	h.mu.RLock()
	var syncNewJobs []SyncJob
	existingByJob := make(map[string]map[string]struct{})
	activeCountByJob := make(map[string]int)
	for _, j := range h.state.Jobs {
		if j.SyncNewContent && !strings.EqualFold(j.Status, "Cancelled") {
			cp := j
			if len(j.RequestedItemIds) > 0 {
				cp.RequestedItemIds = append([]string(nil), j.RequestedItemIds...)
			}
			syncNewJobs = append(syncNewJobs, cp)
			existingByJob[j.Id] = make(map[string]struct{})
		}
	}
	for _, ji := range h.state.JobItems {
		if set, ok := existingByJob[ji.JobId]; ok {
			if norm := guid.Normalize(ji.ItemId); norm != "" {
				set[norm] = struct{}{}
			}
			if !strings.EqualFold(ji.Status, "Cancelled") && !strings.EqualFold(ji.Status, "Removing") {
				activeCountByJob[ji.JobId]++
			}
		}
	}
	h.mu.RUnlock()

	if len(syncNewJobs) == 0 {
		return
	}

	var allAdded []SyncJobItem
	affectedJobs := make(map[string]struct{})
	queuedAny := false

	for _, job := range syncNewJobs {
		var jobUser *sqlite.UserRecord
		if userRepo != nil && strings.TrimSpace(job.UserId) != "" {
			u, err := userRepo.GetUserByID(ctx, guid.Normalize(job.UserId))
			if err != nil || u == nil || u.IsDisabled {
				continue
			}
			if !u.IsAdmin && !u.Policy.IsAdministrator && !u.Policy.EnableContentDownloading {
				continue
			}
			jobUser = u
		}

		var candidateIDs []string
		seenExpanded := make(map[string]struct{})
		visitedContainers := make(map[string]struct{})

		expandRoot := func(rootID string) {
			rootID = strings.TrimSpace(rootID)
			if rootID == "" {
				return
			}
			if rec, err := libRepo.GetItemRecordByID(ctx, rootID); err == nil && rec != nil {
				if jobUser != nil && !jobUser.IsAdmin && !jobUser.Policy.IsAdministrator && !h.canUserAccessItemRecord(ctx, libRepo, jobUser, rec) {
					return
				}
				if isSyncContainerType(rec.Type) {
					h.expandSyncContainer(ctx, libRepo, serverID, job.UserId, jobUser, rec, visitedContainers, seenExpanded, &candidateIDs)
					return
				}
				if !rec.IsStrm && !strings.EqualFold(filepath.Ext(rec.Path), ".strm") {
					norm := guid.Normalize(rec.ID)
					if _, dup := seenExpanded[norm]; !dup {
						seenExpanded[norm] = struct{}{}
						candidateIDs = append(candidateIDs, rec.ID)
					}
				}
				return
			}
			// Root might be a Library ID (not in items table).
			if lib, libErr := libRepo.GetLibrary(ctx, rootID); libErr == nil && lib != nil {
				if jobUser != nil && !jobUser.IsAdmin && !jobUser.Policy.IsAdministrator && !libRepo.CanUserAccessLibrary(ctx, jobUser.Policy, lib.ID) {
					return
				}
				if items, _, qErr := libRepo.QueryItems(ctx, sqlite.ItemFilter{
					ServerID:  serverID,
					LibraryID: lib.ID,
					Recursive: true,
					Limit:     maxSyncJobItemIDs,
				}); qErr == nil {
					for _, it := range items {
						cRec, rErr := libRepo.GetItemRecordByID(ctx, it.Id)
						if rErr != nil || cRec == nil || isSyncContainerType(cRec.Type) || cRec.IsStrm || strings.EqualFold(filepath.Ext(cRec.Path), ".strm") {
							continue
						}
						if jobUser != nil && !jobUser.IsAdmin && !jobUser.Policy.IsAdministrator && !h.canUserAccessItemRecord(ctx, libRepo, jobUser, cRec) {
							continue
						}
						norm := guid.Normalize(cRec.ID)
						if _, dup := seenExpanded[norm]; !dup && len(candidateIDs) < maxSyncJobItemIDs {
							seenExpanded[norm] = struct{}{}
							candidateIDs = append(candidateIDs, cRec.ID)
						}
					}
				}
			}
		}

		if strings.TrimSpace(job.ParentId) != "" {
			expandRoot(job.ParentId)
		}
		for _, reqID := range job.RequestedItemIds {
			expandRoot(reqID)
		}
		// If job had neither ParentId nor container RequestedItemIds, allow explicit itemIDs within user's accessible libraries.
		if strings.TrimSpace(job.ParentId) == "" && len(job.RequestedItemIds) == 0 && len(explicitHintSet) > 0 {
			for _, rawID := range itemIDs {
				expandRoot(rawID)
			}
		}

		existingSet := existingByJob[job.Id]
		curCount := activeCountByJob[job.Id]
		wantTC, _ := needsSyncTranscode(job.Quality, job.Bitrate)

		for _, cid := range candidateIDs {
			if job.ItemLimit > 0 && curCount >= job.ItemLimit {
				break
			}
			normCID := guid.Normalize(cid)
			if normCID == "" {
				continue
			}
			if _, exists := existingSet[normCID]; exists {
				continue
			}
			itemDTO, err := libRepo.GetItemByID(ctx, serverID, job.UserId, cid)
			if err != nil || itemDTO == nil || strings.EqualFold(filepath.Ext(itemDTO.Path), ".strm") {
				continue
			}
			if job.UnwatchedOnly && h.isItemWatchedByUser(ctx, libRepo, job.UserId, cid, itemDTO) {
				continue
			}
			mediaSourceID := itemDTO.Id
			if len(itemDTO.MediaSources) > 0 && itemDTO.MediaSources[0].Id != "" {
				mediaSourceID = itemDTO.MediaSources[0].Id
			}
			itemName := itemDTO.Name
			if itemName == "" {
				itemName = fmt.Sprintf("Item %d", curCount+1)
			}
			ji := SyncJobItem{
				Id:          guid.New(),
				JobId:       job.Id,
				ItemId:      cid,
				ItemName:    itemName,
				TargetId:    job.TargetId,
				Status:      "ReadyToTransfer",
				OutputPath:  itemDTO.Path,
				SourcePath:  itemDTO.Path,
				MediaType:   itemDTO.MediaType,
				MediaSource: mediaSourceID,
			}
			if wantTC && h.canQueueTranscode(itemDTO.Path) {
				ji.Status = "Queued"
				ji.OutputPath = ""
				queuedAny = true
			}
			existingSet[normCID] = struct{}{}
			curCount++
			allAdded = append(allAdded, ji)
			affectedJobs[job.Id] = struct{}{}
		}
	}

	if len(allAdded) == 0 {
		return
	}

	h.mu.Lock()
	activeJobMap := make(map[string]bool, len(h.state.Jobs))
	existingPair := make(map[string]struct{}, len(h.state.JobItems))
	for _, j := range h.state.Jobs {
		if j.SyncNewContent && !strings.EqualFold(j.Status, "Cancelled") {
			activeJobMap[j.Id] = true
		}
	}
	for _, ji := range h.state.JobItems {
		existingPair[ji.JobId+":"+guid.Normalize(ji.ItemId)] = struct{}{}
	}
	inserted := false
	for _, ji := range allAdded {
		key := ji.JobId + ":" + guid.Normalize(ji.ItemId)
		if !activeJobMap[ji.JobId] {
			continue
		}
		if _, dup := existingPair[key]; dup {
			continue
		}
		existingPair[key] = struct{}{}
		h.state.JobItems = append(h.state.JobItems, ji)
		inserted = true
	}
	if inserted {
		for jid := range affectedJobs {
			h.recomputeJobLocked(jid)
		}
		_ = h.saveStateLocked(ctx)
	}
	h.mu.Unlock()

	if inserted && queuedAny {
		h.wakeWorker()
	}
}

func (h *Handler) isItemWatchedByUser(ctx context.Context, libRepo *sqlite.LibraryRepo, userID, itemID string, itemDTO *dto.BaseItemDto) bool {
	if itemDTO != nil && itemDTO.UserData != nil {
		return itemDTO.UserData.Played
	}
	if libRepo != nil && strings.TrimSpace(userID) != "" {
		if ud, err := libRepo.GetUserItemData(ctx, userID, itemID); err == nil && ud != nil {
			return ud.Played
		}
	}
	return false
}
