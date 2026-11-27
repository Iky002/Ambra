package sync

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	gosync "sync"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/budget"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

const (
	stateConfigKey              = "sync:state"
	maxSyncJobItemIDs           = 500
	maxSyncJobsPerUser          = 100
	maxSyncJobNameLen           = 256
	maxSyncFieldLen             = 128
	maxSyncQualityLen           = 64
	maxSyncBitrate              = 200_000_000
	defaultSyncTranscodeTimeout = 2 * time.Hour
)

func isValidSyncJobStatus(status string) bool {
	switch status {
	case "Queued", "Converting", "ReadyToTransfer", "Transferring", "Synced", "Completed", "Cancelled", "Failed", "Removing":
		return true
	default:
		return false
	}
}

func cloneSyncState(st syncState) syncState {
	out := syncState{
		Jobs:     make([]SyncJob, len(st.Jobs)),
		JobItems: make([]SyncJobItem, len(st.JobItems)),
	}
	copy(out.Jobs, st.Jobs)
	for i := range out.Jobs {
		if len(out.Jobs[i].RequestedItemIds) > 0 {
			out.Jobs[i].RequestedItemIds = append([]string(nil), out.Jobs[i].RequestedItemIds...)
		}
	}
	copy(out.JobItems, st.JobItems)
	return out
}

// ErrForbidden is returned when a caller attempts to mutate or access another user's sync resources.
var ErrForbidden = errors.New("forbidden")

// AuthFuncs decouples route registration from the concrete middleware package.
type AuthFuncs struct {
	RequireAuth   func(http.Handler) http.Handler
	RequireAdmin  func(http.Handler) http.Handler
	UserIDFromCtx func(context.Context) string
}

type SyncTarget struct {
	Name string `json:"Name"`
	Id   string `json:"Id"`
}

type SyncQualityOption struct {
	Name        string `json:"Name"`
	Description string `json:"Description,omitempty"`
	Id          string `json:"Id"`
	IsDefault   bool   `json:"IsDefault,omitempty"`
	IsOriginal  bool   `json:"IsOriginalQuality,omitempty"`
}

type SyncProfileOption struct {
	Name        string `json:"Name"`
	Description string `json:"Description,omitempty"`
	Id          string `json:"Id"`
	IsDefault   bool   `json:"IsDefault,omitempty"`
}

type SyncDialogOptions struct {
	Targets        []SyncTarget        `json:"Targets"`
	Options        []string            `json:"Options"`
	QualityOptions []SyncQualityOption `json:"QualityOptions"`
	ProfileOptions []SyncProfileOption `json:"ProfileOptions"`
}

type SyncJob struct {
	Id               string    `json:"Id"`
	TargetId         string    `json:"TargetId"`
	TargetName       string    `json:"TargetName,omitempty"`
	Quality          string    `json:"Quality,omitempty"`
	Bitrate          int       `json:"Bitrate,omitempty"`
	Profile          string    `json:"Profile,omitempty"`
	Category         string    `json:"Category,omitempty"`
	ParentId         string    `json:"ParentId,omitempty"`
	Progress         float64   `json:"Progress"`
	Name             string    `json:"Name"`
	Status           string    `json:"Status"`
	UserId           string    `json:"UserId,omitempty"`
	UnwatchedOnly    bool      `json:"UnwatchedOnly"`
	SyncNewContent   bool      `json:"SyncNewContent"`
	ItemLimit        int       `json:"ItemLimit,omitempty"`
	RequestedItemIds []string  `json:"RequestedItemIds"`
	DateCreated      time.Time `json:"DateCreated"`
	DateLastModified time.Time `json:"DateLastModified"`
	ItemCount        int       `json:"ItemCount"`
}

type SyncJobItem struct {
	Id          string  `json:"Id"`
	JobId       string  `json:"JobId"`
	ItemId      string  `json:"ItemId"`
	ItemName    string  `json:"ItemName"`
	TargetId    string  `json:"TargetId"`
	Status      string  `json:"Status"`
	Progress    float64 `json:"Progress"`
	OutputPath  string  `json:"OutputPath,omitempty"`
	SourcePath  string  `json:"SourcePath,omitempty"`
	MediaType   string  `json:"MediaType,omitempty"`
	Transcoded  bool    `json:"Transcoded,omitempty"`
	MediaSource string  `json:"MediaSourceId,omitempty"`
}

type SyncJobCreationResult struct {
	Job      SyncJob       `json:"Job"`
	JobItems []SyncJobItem `json:"JobItems"`
}

type SyncedItem struct {
	ServerId      string           `json:"ServerId"`
	SyncJobId     string           `json:"SyncJobId"`
	SyncJobName   string           `json:"SyncJobName"`
	SyncJobItemId string           `json:"SyncJobItemId"`
	ItemId        string           `json:"ItemId"`
	Item          *dto.BaseItemDto `json:"Item,omitempty"`
	OriginalFile  string           `json:"OriginalFileName,omitempty"`
}

type syncState struct {
	Jobs     []SyncJob     `json:"jobs"`
	JobItems []SyncJobItem `json:"job_items"`
}

type Handler struct {
	store                     *sqlite.Store
	cfg                       *config.Config
	libRepo                   *sqlite.LibraryRepo
	userRepo                  *sqlite.UserRepo
	budgetPool                *budget.Pool
	ffmpegPath                string
	transcodeTimeout          time.Duration
	streamResolverWithHeaders func(ctx context.Context, rawPath string) (*plugins.StreamResolveResult, error)
	auth                      AuthFuncs

	mu    gosync.RWMutex
	state syncState

	workerMu      gosync.Mutex
	workerRunning bool
	workerCancel  context.CancelFunc
	workerWake    chan struct{}
	workerWg      gosync.WaitGroup
	activeCancels map[string]context.CancelFunc
}

func New(store *sqlite.Store, cfg *config.Config) *Handler {
	h := &Handler{
		store:            store,
		cfg:              cfg,
		transcodeTimeout: defaultSyncTranscodeTimeout,
	}
	h.loadState(context.Background())
	return h
}

// SetTranscodeTimeout configures the per-item FFmpeg execution timeout for offline sync transcodes.
func (h *Handler) SetTranscodeTimeout(d time.Duration) {
	h.mu.Lock()
	h.transcodeTimeout = d
	h.mu.Unlock()
}

func (h *Handler) getTranscodeTimeout() time.Duration {
	h.mu.RLock()
	d := h.transcodeTimeout
	h.mu.RUnlock()
	if d <= 0 {
		return defaultSyncTranscodeTimeout
	}
	return d
}

// PurgeUser removes all sync jobs, job items, active transcodes, and cached sync files belonging to userID.
func (h *Handler) PurgeUser(ctx context.Context, userID string) {
	normUID := guid.Normalize(userID)
	if normUID == "" {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	h.mu.Lock()
	removedJobIDs := make(map[string]struct{})
	keptJobs := make([]SyncJob, 0, len(h.state.Jobs))
	for _, j := range h.state.Jobs {
		if guid.Normalize(j.UserId) == normUID {
			removedJobIDs[j.Id] = struct{}{}
			continue
		}
		keptJobs = append(keptJobs, j)
	}
	if len(removedJobIDs) == 0 {
		h.mu.Unlock()
		return
	}

	snapshot := cloneSyncState(h.state)
	h.state.Jobs = keptJobs

	keptItems := make([]SyncJobItem, 0, len(h.state.JobItems))
	var removedItemIDs []string
	var removedPaths []string
	for _, ji := range h.state.JobItems {
		if _, drop := removedJobIDs[ji.JobId]; !drop {
			keptItems = append(keptItems, ji)
			continue
		}
		removedItemIDs = append(removedItemIDs, ji.Id)
		if ji.OutputPath != "" {
			removedPaths = append(removedPaths, ji.OutputPath)
		}
	}
	h.state.JobItems = keptItems
	if err := h.saveStateLocked(ctx); err != nil {
		h.state = snapshot
		h.mu.Unlock()
		return
	}
	h.mu.Unlock()

	h.cancelActiveTranscodeItems(removedItemIDs...)
	h.cleanupSyncCacheFiles(removedPaths...)
	for jid := range removedJobIDs {
		h.cleanupSyncJobCacheDir(jid)
	}
}

// SetLibraryRepo attaches a LibraryRepo so sync jobs and ready items can hydrate real library metadata.
func (h *Handler) SetLibraryRepo(r *sqlite.LibraryRepo) {
	h.mu.Lock()
	h.libRepo = r
	h.mu.Unlock()
}

// SetUserRepo attaches a UserRepo so sync targets can list real registered devices.
func (h *Handler) SetUserRepo(r *sqlite.UserRepo) {
	h.mu.Lock()
	h.userRepo = r
	h.mu.Unlock()
}

func (h *Handler) getLibraryRepo() *sqlite.LibraryRepo {
	h.mu.RLock()
	r := h.libRepo
	h.mu.RUnlock()
	return r
}

func (h *Handler) getUserRepo() *sqlite.UserRepo {
	h.mu.RLock()
	r := h.userRepo
	h.mu.RUnlock()
	return r
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth AuthFuncs) {
	h.auth = auth
	wrapAuth := auth.RequireAuth
	if wrapAuth == nil {
		wrapAuth = passthrough
	}
	handleAuth := func(pattern string, fn http.HandlerFunc) {
		mux.Handle(pattern, wrapAuth(fn))
	}

	handleAuth("GET /Sync/Targets", h.handleTargets)
	handleAuth("GET /Sync/Options", h.handleOptions)
	handleAuth("GET /Sync/Jobs", h.handleListJobs)
	handleAuth("POST /Sync/Jobs", h.handleCreateJob)
	handleAuth("GET /Sync/Jobs/{Id}", h.handleGetJob)
	handleAuth("POST /Sync/Jobs/{Id}", h.handleUpdateJob)
	handleAuth("DELETE /Sync/Jobs/{Id}", h.handleDeleteJob)

	handleAuth("GET /Sync/JobItems", h.handleListJobItems)
	handleAuth("DELETE /Sync/JobItems/{Id}", h.handleDeleteJobItem)
	handleAuth("POST /Sync/JobItems/{Id}/Transferred", h.handleMarkTransferred)
	handleAuth("POST /Sync/JobItems/{Id}/Enable", h.handleEnableJobItem)

	handleAuth("GET /Sync/Items/Ready", h.handleReadyItems)
	handleAuth("POST /Sync/Data", h.handleSyncData)
}

func (h *Handler) serverID() string {
	if h.cfg != nil {
		return h.cfg.ServerID
	}
	return ""
}

func callerAuth(r *http.Request) (callerID string, isAdmin bool, hasAuth bool) {
	ac, ok := middleware.AuthFromContext(r.Context())
	if !ok || ac == nil || (ac.User == nil && !ac.IsAPIKey) {
		return "", false, false
	}
	return guid.Normalize(ac.UserID()), ac.IsAdmin(), true
}

func canAccessJob(job SyncJob, callerID string, isAdmin, hasAuth bool) bool {
	if !hasAuth || isAdmin {
		return true
	}
	return callerID != "" && guid.Normalize(job.UserId) == callerID
}

func callerCanAccessJob(job SyncJob, callerID string, isAdmin bool) bool {
	if isAdmin {
		return true
	}
	normCaller := guid.Normalize(callerID)
	if normCaller == "" {
		return false
	}
	return guid.Normalize(job.UserId) == normCaller
}

// JobItemIDsForJob returns all job item IDs belonging to jobID.
func (h *Handler) JobItemIDsForJob(jobID string) []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	var ids []string
	for _, ji := range h.state.JobItems {
		if ji.JobId == jobID {
			ids = append(ids, ji.Id)
		}
	}
	return ids
}

// DeleteJobForCaller removes a sync job and its job items if the caller owns the job or is admin.
func (h *Handler) DeleteJobForCaller(ctx context.Context, jobID, callerID string, isAdmin bool) (found, allowed bool, err error) {
	h.mu.Lock()
	idx := -1
	for i, j := range h.state.Jobs {
		if j.Id == jobID {
			idx = i
			break
		}
	}
	if idx < 0 {
		h.mu.Unlock()
		return false, true, nil
	}
	if !callerCanAccessJob(h.state.Jobs[idx], callerID, isAdmin) {
		h.mu.Unlock()
		return true, false, nil
	}
	snapshot := cloneSyncState(h.state)
	h.state.Jobs = append(h.state.Jobs[:idx], h.state.Jobs[idx+1:]...)
	keptItems := make([]SyncJobItem, 0, len(h.state.JobItems))
	var removedItemIDs []string
	var removedPaths []string
	for _, ji := range h.state.JobItems {
		if ji.JobId != jobID {
			keptItems = append(keptItems, ji)
		} else {
			removedItemIDs = append(removedItemIDs, ji.Id)
			if ji.OutputPath != "" {
				removedPaths = append(removedPaths, ji.OutputPath)
			}
		}
	}
	h.state.JobItems = keptItems
	if err := h.saveStateLocked(ctx); err != nil {
		h.state = snapshot
		h.mu.Unlock()
		return true, true, err
	}
	h.mu.Unlock()

	h.cancelActiveTranscodeItems(removedItemIDs...)
	h.cleanupSyncCacheFiles(removedPaths...)
	h.cleanupSyncJobCacheDir(jobID)
	return true, true, nil
}

// DeleteJobItemForCaller removes a sync job item if the caller owns its parent job or is admin.
func (h *Handler) DeleteJobItemForCaller(ctx context.Context, itemID, callerID string, isAdmin bool) (found, allowed bool, err error) {
	h.mu.Lock()
	for i, ji := range h.state.JobItems {
		if ji.Id == itemID {
			jobID := ji.JobId
			if !isAdmin {
				j, ok := h.findJobLocked(jobID)
				if !ok || !callerCanAccessJob(j, callerID, isAdmin) {
					h.mu.Unlock()
					return true, false, nil
				}
			}
			outPath := ji.OutputPath
			snapshot := cloneSyncState(h.state)
			h.state.JobItems = append(h.state.JobItems[:i], h.state.JobItems[i+1:]...)
			h.recomputeJobLocked(jobID)
			if err := h.saveStateLocked(ctx); err != nil {
				h.state = snapshot
				h.mu.Unlock()
				return true, true, err
			}
			h.mu.Unlock()
			h.cancelActiveTranscodeItems(itemID)
			h.cleanupSyncCacheFiles(outPath)
			return true, true, nil
		}
	}
	h.mu.Unlock()
	return false, true, nil
}

// SetJobItemRemovalForCaller marks or unmarks a sync job item for removal if the caller owns its parent job or is admin.
func (h *Handler) SetJobItemRemovalForCaller(ctx context.Context, itemID string, remove bool, callerID string, isAdmin bool) (found, allowed bool, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for i := range h.state.JobItems {
		if h.state.JobItems[i].Id == itemID {
			jobID := h.state.JobItems[i].JobId
			if !isAdmin {
				j, ok := h.findJobLocked(jobID)
				if !ok || !callerCanAccessJob(j, callerID, isAdmin) {
					return true, false, nil
				}
			}
			snapshot := cloneSyncState(h.state)
			if remove {
				h.state.JobItems[i].Status = "Removing"
			} else {
				h.state.JobItems[i].Status = "Synced"
			}
			h.recomputeJobLocked(jobID)
			if err := h.saveStateLocked(ctx); err != nil {
				h.state = snapshot
				return true, true, err
			}
			return true, true, nil
		}
	}
	return false, true, nil
}

// CancelItemsForCaller cancels matching sync job items owned by the caller (or any user when isAdmin).
// If both targetID and itemIDs are empty, this is a safe no-op and never wipes all sync items.
func (h *Handler) CancelItemsForCaller(ctx context.Context, targetID string, itemIDs []string, callerID string, isAdmin bool) error {
	targetID = strings.TrimSpace(targetID)
	idSet := make(map[string]struct{}, len(itemIDs))
	for _, id := range itemIDs {
		if norm := guid.Normalize(id); norm != "" {
			idSet[norm] = struct{}{}
		}
	}
	if targetID == "" && len(idSet) == 0 {
		return nil
	}

	h.mu.Lock()
	jobByID := make(map[string]SyncJob, len(h.state.Jobs))
	for _, j := range h.state.Jobs {
		jobByID[j.Id] = j
	}

	remaining := make([]SyncJobItem, 0, len(h.state.JobItems))
	affectedJobs := make(map[string]struct{})
	removedCount := 0
	deniedCount := 0
	var removedItemIDs []string
	var removedPaths []string

	for _, it := range h.state.JobItems {
		targetMatch := targetID == "" || it.TargetId == targetID
		_, itemMatch := idSet[guid.Normalize(it.ItemId)]
		if targetMatch && (len(idSet) == 0 || itemMatch) {
			if !isAdmin {
				j, ok := jobByID[it.JobId]
				if !ok || !callerCanAccessJob(j, callerID, isAdmin) {
					deniedCount++
					remaining = append(remaining, it)
					continue
				}
			}
			removedCount++
			removedItemIDs = append(removedItemIDs, it.Id)
			if it.OutputPath != "" {
				removedPaths = append(removedPaths, it.OutputPath)
			}
			affectedJobs[it.JobId] = struct{}{}
			continue
		}
		remaining = append(remaining, it)
	}

	if removedCount > 0 {
		snapshot := cloneSyncState(h.state)
		h.state.JobItems = remaining
		for jid := range affectedJobs {
			h.recomputeJobLocked(jid)
		}
		if err := h.saveStateLocked(ctx); err != nil {
			h.state = snapshot
			h.mu.Unlock()
			return err
		}
	}
	h.mu.Unlock()
	if removedCount > 0 {
		h.cancelActiveTranscodeItems(removedItemIDs...)
		h.cleanupSyncCacheFiles(removedPaths...)
	}
	if removedCount == 0 && deniedCount > 0 {
		return ErrForbidden
	}
	return nil
}

// GetJobItemForCaller returns a copy of a SyncJobItem by ID if the caller owns its parent job or is admin.
func (h *Handler) GetJobItemForCaller(itemID, callerID string, isAdmin bool) (item *SyncJobItem, found, allowed bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, ji := range h.state.JobItems {
		if ji.Id == itemID {
			if !isAdmin {
				j, ok := h.findJobLocked(ji.JobId)
				if !ok || !callerCanAccessJob(j, callerID, isAdmin) {
					return nil, true, false
				}
			}
			cp := ji
			return &cp, true, true
		}
	}
	return nil, false, true
}

// GetItemStatusForCaller looks up a SyncJobItem matching itemID (and optional targetID) visible to the caller.
func (h *Handler) GetItemStatusForCaller(itemID, targetID, callerID string, isAdmin bool) (*SyncJobItem, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	jobByID := make(map[string]SyncJob, len(h.state.Jobs))
	for _, j := range h.state.Jobs {
		jobByID[j.Id] = j
	}
	normItemID := guid.Normalize(itemID)
	for _, ji := range h.state.JobItems {
		if guid.Normalize(ji.ItemId) == normItemID && (targetID == "" || ji.TargetId == targetID) {
			if !isAdmin {
				j, ok := jobByID[ji.JobId]
				if !ok || !callerCanAccessJob(j, callerID, isAdmin) {
					continue
				}
			}
			cp := ji
			return &cp, true
		}
	}
	return nil, false
}

func (h *Handler) findJobLocked(jobID string) (SyncJob, bool) {
	for _, j := range h.state.Jobs {
		if j.Id == jobID {
			return j, true
		}
	}
	return SyncJob{}, false
}

func defaultTargets() []SyncTarget {
	return []SyncTarget{
		{Id: "local-device", Name: "This Device"},
	}
}

func (h *Handler) resolveTargets(ctx context.Context, callerID string, isAdmin, hasAuth bool) []SyncTarget {
	if userRepo := h.getUserRepo(); userRepo != nil {
		if devices, err := userRepo.ListDevices(ctx); err == nil && len(devices) > 0 {
			targets := make([]SyncTarget, 0, len(devices))
			for _, d := range devices {
				if hasAuth && !isAdmin && guid.Normalize(d.LastUserID) != callerID {
					continue
				}
				id := strings.TrimSpace(d.ID)
				if id == "" {
					continue
				}
				name := strings.TrimSpace(d.Name)
				if name == "" {
					name = strings.TrimSpace(d.AppName)
				}
				if name == "" {
					name = id
				}
				targets = append(targets, SyncTarget{Id: id, Name: name})
			}
			if len(targets) > 0 {
				return targets
			}
		}
	}
	return defaultTargets()
}

func (h *Handler) handleTargets(w http.ResponseWriter, r *http.Request) {
	callerID, isAdmin, hasAuth := callerAuth(r)
	httpx.JSON(w, http.StatusOK, h.resolveTargets(r.Context(), callerID, isAdmin, hasAuth))
}

func (h *Handler) handleOptions(w http.ResponseWriter, r *http.Request) {
	callerID, isAdmin, hasAuth := callerAuth(r)
	httpx.JSON(w, http.StatusOK, SyncDialogOptions{
		Targets: h.resolveTargets(r.Context(), callerID, isAdmin, hasAuth),
		Options: []string{"Name", "Quality", "UnwatchedOnly", "SyncNewContent", "ItemLimit"},
		QualityOptions: []SyncQualityOption{
			{Id: "original", Name: "Original", Description: "Keep original quality", IsDefault: true, IsOriginal: true},
			{Id: "high", Name: "High (8 Mbps)", Description: "1080p 8Mbps"},
			{Id: "medium", Name: "Medium (4 Mbps)", Description: "720p 4Mbps"},
			{Id: "low", Name: "Low (1.5 Mbps)", Description: "480p 1.5Mbps"},
		},
		ProfileOptions: []SyncProfileOption{
			{Id: "universal", Name: "Universal", Description: "MP4 / H.264 / AAC", IsDefault: true},
			{Id: "mobile", Name: "Mobile", Description: "Optimized for mobile devices"},
		},
	})
}

func (h *Handler) handleListJobs(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	targetID := q.Get("TargetId")
	userID := guid.Normalize(q.Get("UserId"))
	callerID, isAdmin, hasAuth := callerAuth(r)
	if hasAuth && !isAdmin {
		userID = callerID
	}

	h.mu.RLock()
	filtered := make([]SyncJob, 0, len(h.state.Jobs))
	for _, j := range h.state.Jobs {
		if targetID != "" && j.TargetId != targetID {
			continue
		}
		if userID != "" && guid.Normalize(j.UserId) != userID {
			continue
		}
		filtered = append(filtered, j)
	}
	h.mu.RUnlock()

	page, total := paginate(filtered, q.Int("StartIndex", 0), q.Int("Limit", 0))
	httpx.JSON(w, http.StatusOK, dto.QueryResult[SyncJob]{
		Items:            page,
		TotalRecordCount: total,
	})
}

func (h *Handler) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TargetId       string   `json:"TargetId"`
		ItemIds        []string `json:"ItemIds"`
		Category       string   `json:"Category"`
		ParentId       string   `json:"ParentId"`
		Quality        string   `json:"Quality"`
		Profile        string   `json:"Profile"`
		Name           string   `json:"Name"`
		UserId         string   `json:"UserId"`
		UnwatchedOnly  bool     `json:"UnwatchedOnly"`
		SyncNewContent bool     `json:"SyncNewContent"`
		ItemLimit      int      `json:"ItemLimit"`
		Bitrate        int      `json:"Bitrate"`
	}
	if err := httpx.DecodeJSONLimit(r, &req, 0); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid sync job payload")
		return
	}
	if len(req.ItemIds) > maxSyncJobItemIDs {
		httpx.Error(w, http.StatusBadRequest, fmt.Sprintf("ItemIds exceeds maximum limit of %d per sync job", maxSyncJobItemIDs))
		return
	}
	if len([]rune(strings.TrimSpace(req.Name))) > maxSyncJobNameLen ||
		len([]rune(strings.TrimSpace(req.TargetId))) > maxSyncFieldLen ||
		len([]rune(strings.TrimSpace(req.Category))) > maxSyncFieldLen ||
		len([]rune(strings.TrimSpace(req.ParentId))) > maxSyncFieldLen ||
		len([]rune(strings.TrimSpace(req.Quality))) > maxSyncQualityLen ||
		len([]rune(strings.TrimSpace(req.Profile))) > maxSyncQualityLen {
		httpx.Error(w, http.StatusBadRequest, "sync job field exceeds maximum length")
		return
	}
	if req.Bitrate < 0 || req.ItemLimit < 0 {
		httpx.Error(w, http.StatusBadRequest, "Bitrate and ItemLimit must be non-negative")
		return
	}
	if req.Bitrate > maxSyncBitrate {
		req.Bitrate = maxSyncBitrate
	}
	if req.TargetId == "" {
		req.TargetId = "local-device"
	}
	callerID, isAdmin, hasAuth := callerAuth(r)
	var callerUser *sqlite.UserRecord
	if hasAuth && !isAdmin {
		if userRepo := h.getUserRepo(); userRepo != nil {
			if callerID == "" {
				httpx.Error(w, http.StatusForbidden, "forbidden")
				return
			}
			u, err := userRepo.GetUserByID(r.Context(), callerID)
			if err != nil || u == nil || u.IsDisabled {
				httpx.Error(w, http.StatusForbidden, "forbidden")
				return
			}
			callerUser = u
		} else if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && ac.User != nil {
			callerUser = ac.User
		} else {
			httpx.Error(w, http.StatusForbidden, "forbidden")
			return
		}
		if !callerUser.IsAdmin && !callerUser.Policy.IsAdministrator && !callerUser.Policy.EnableContentDownloading {
			httpx.Error(w, http.StatusForbidden, "content downloading is disabled")
			return
		}
		req.UserId = callerID
	} else if hasAuth && req.UserId == "" {
		req.UserId = callerID
	} else if req.UserId == "" && h.auth.UserIDFromCtx != nil {
		req.UserId = h.auth.UserIDFromCtx(r.Context())
	}
	if req.Name == "" {
		req.Name = "Sync Job"
	}
	if req.Quality == "" {
		req.Quality = "original"
	}
	if req.ItemIds == nil {
		req.ItemIds = []string{}
	}
	dedupedItemIDs := make([]string, 0, len(req.ItemIds))
	seenReqIDs := make(map[string]struct{}, len(req.ItemIds))
	for _, rawID := range req.ItemIds {
		normID := guid.Normalize(rawID)
		if normID == "" {
			continue
		}
		if _, dup := seenReqIDs[normID]; dup {
			continue
		}
		seenReqIDs[normID] = struct{}{}
		dedupedItemIDs = append(dedupedItemIDs, normID)
	}
	req.ItemIds = dedupedItemIDs

	now := time.Now().UTC()
	job := SyncJob{
		Id:               guid.New(),
		TargetId:         req.TargetId,
		TargetName:       req.TargetId,
		Quality:          req.Quality,
		Bitrate:          req.Bitrate,
		Profile:          req.Profile,
		Category:         req.Category,
		ParentId:         req.ParentId,
		Name:             req.Name,
		Status:           "ReadyToTransfer",
		UserId:           guid.Normalize(req.UserId),
		UnwatchedOnly:    req.UnwatchedOnly,
		SyncNewContent:   req.SyncNewContent,
		ItemLimit:        req.ItemLimit,
		RequestedItemIds: req.ItemIds,
		DateCreated:      now,
		DateLastModified: now,
		ItemCount:        len(req.ItemIds),
	}
	if len(req.ItemIds) == 0 {
		job.Status = "Queued"
	}

	libRepo := h.getLibraryRepo()
	serverID := h.serverID()
	wantTranscode, _ := needsSyncTranscode(job.Quality, job.Bitrate)
	queuedAny := false
	matchedAnyLibraryItem := false
	createdItems := make([]SyncJobItem, 0, len(req.ItemIds))
	if libRepo == nil {
		itemList := req.ItemIds
		if job.ItemLimit > 0 && len(itemList) > job.ItemLimit {
			itemList = itemList[:job.ItemLimit]
		}
		for i, itemID := range itemList {
			createdItems = append(createdItems, SyncJobItem{
				Id:       guid.New(),
				JobId:    job.Id,
				ItemId:   itemID,
				ItemName: "Item " + strconv.Itoa(i+1),
				TargetId: job.TargetId,
				Status:   "ReadyToTransfer",
			})
		}
	} else {
		expandedIDs := make([]string, 0, len(req.ItemIds))
		seenExpanded := make(map[string]struct{}, len(req.ItemIds))
		visitedContainers := make(map[string]struct{})
		for _, itemID := range req.ItemIds {
			rec, recErr := libRepo.GetItemRecordByID(r.Context(), itemID)
			if recErr != nil || rec == nil {
				continue
			}
			if callerUser != nil && !callerUser.IsAdmin && !callerUser.Policy.IsAdministrator {
				if !h.canUserAccessItemRecord(r.Context(), libRepo, callerUser, rec) {
					httpx.Error(w, http.StatusForbidden, "access denied to requested item")
					return
				}
			}
			if isSyncContainerType(rec.Type) {
				h.expandSyncContainer(r.Context(), libRepo, serverID, job.UserId, callerUser, rec, visitedContainers, seenExpanded, &expandedIDs)
				continue
			}
			if rec.IsStrm || strings.EqualFold(filepath.Ext(rec.Path), ".strm") {
				httpx.Error(w, http.StatusBadRequest, "STRM items cannot be synced")
				return
			}
			normRecID := guid.Normalize(rec.ID)
			if _, exists := seenExpanded[normRecID]; !exists && len(expandedIDs) < maxSyncJobItemIDs {
				seenExpanded[normRecID] = struct{}{}
				expandedIDs = append(expandedIDs, rec.ID)
			}
		}

		// Also support ParentId container expansion when ItemIds is empty.
		if len(req.ItemIds) == 0 && strings.TrimSpace(req.ParentId) != "" {
			pID := strings.TrimSpace(req.ParentId)
			if pRec, pErr := libRepo.GetItemRecordByID(r.Context(), pID); pErr == nil && pRec != nil {
				if callerUser != nil && !callerUser.IsAdmin && !callerUser.Policy.IsAdministrator && !h.canUserAccessItemRecord(r.Context(), libRepo, callerUser, pRec) {
					httpx.Error(w, http.StatusForbidden, "access denied to requested parent container")
					return
				}
				if isSyncContainerType(pRec.Type) {
					h.expandSyncContainer(r.Context(), libRepo, serverID, job.UserId, callerUser, pRec, visitedContainers, seenExpanded, &expandedIDs)
				}
			} else if pLib, lErr := libRepo.GetLibrary(r.Context(), pID); lErr == nil && pLib != nil {
				if callerUser != nil && !callerUser.IsAdmin && !callerUser.Policy.IsAdministrator && !libRepo.CanUserAccessLibrary(r.Context(), callerUser.Policy, pLib.ID) {
					httpx.Error(w, http.StatusForbidden, "access denied to requested library")
					return
				}
				if libItems, _, qErr := libRepo.QueryItems(r.Context(), sqlite.ItemFilter{
					ServerID:  serverID,
					LibraryID: pLib.ID,
					Recursive: true,
					Limit:     maxSyncJobItemIDs,
				}); qErr == nil {
					for _, li := range libItems {
						cRec, rErr := libRepo.GetItemRecordByID(r.Context(), li.Id)
						if rErr != nil || cRec == nil || isSyncContainerType(cRec.Type) || cRec.IsStrm || strings.EqualFold(filepath.Ext(cRec.Path), ".strm") {
							continue
						}
						if callerUser != nil && !callerUser.IsAdmin && !callerUser.Policy.IsAdministrator && !h.canUserAccessItemRecord(r.Context(), libRepo, callerUser, cRec) {
							continue
						}
						norm := guid.Normalize(cRec.ID)
						if _, dup := seenExpanded[norm]; !dup && len(expandedIDs) < maxSyncJobItemIDs {
							seenExpanded[norm] = struct{}{}
							expandedIDs = append(expandedIDs, cRec.ID)
						}
					}
				}
			}
		}

		matchedAnyLibraryItem = len(expandedIDs) > 0
		for i, itemID := range expandedIDs {
			if job.ItemLimit > 0 && len(createdItems) >= job.ItemLimit {
				break
			}
			itemName := "Item " + strconv.Itoa(i+1)
			srcPath := ""
			mediaSourceID := ""
			itemDTO, err := libRepo.GetItemByID(r.Context(), serverID, job.UserId, itemID)
			if err != nil || itemDTO == nil {
				continue
			}
			if strings.EqualFold(filepath.Ext(itemDTO.Path), ".strm") {
				httpx.Error(w, http.StatusBadRequest, "STRM items cannot be synced")
				return
			}
			if job.UnwatchedOnly && h.isItemWatchedByUser(r.Context(), libRepo, job.UserId, itemID, itemDTO) {
				continue
			}
			if itemDTO.Name != "" {
				itemName = itemDTO.Name
			}
			if itemDTO.Path != "" {
				srcPath = itemDTO.Path
			}
			if len(itemDTO.MediaSources) > 0 && itemDTO.MediaSources[0].Id != "" {
				mediaSourceID = itemDTO.MediaSources[0].Id
			} else if itemDTO.Id != "" {
				mediaSourceID = itemDTO.Id
			}
			ji := SyncJobItem{
				Id:          guid.New(),
				JobId:       job.Id,
				ItemId:      itemID,
				ItemName:    itemName,
				TargetId:    job.TargetId,
				Status:      "ReadyToTransfer",
				OutputPath:  srcPath,
				SourcePath:  srcPath,
				MediaType:   itemDTO.MediaType,
				MediaSource: mediaSourceID,
			}
			if wantTranscode && h.canQueueTranscode(srcPath) {
				ji.Status = "Queued"
				ji.OutputPath = ""
				queuedAny = true
			}
			createdItems = append(createdItems, ji)
		}
	}
	if libRepo != nil && len(req.ItemIds) > 0 && !matchedAnyLibraryItem && len(createdItems) == 0 {
		httpx.Error(w, http.StatusNotFound, "no matching library items found")
		return
	}
	job.ItemCount = len(createdItems)
	if queuedAny {
		job.Status = "Queued"
	} else if len(createdItems) == 0 && matchedAnyLibraryItem {
		job.Status = "Completed"
		job.Progress = 100
	} else if len(createdItems) == 0 {
		job.Status = "Queued"
	} else {
		job.Status = "ReadyToTransfer"
	}

	h.mu.Lock()
	if job.UserId != "" {
		userJobCount := 0
		for _, existing := range h.state.Jobs {
			if guid.Normalize(existing.UserId) == job.UserId {
				userJobCount++
			}
		}
		if userJobCount >= maxSyncJobsPerUser {
			h.mu.Unlock()
			httpx.Error(w, http.StatusTooManyRequests, fmt.Sprintf("maximum number of sync jobs (%d) per user reached", maxSyncJobsPerUser))
			return
		}
	}
	snapshot := cloneSyncState(h.state)
	h.state.Jobs = append(h.state.Jobs, job)
	h.state.JobItems = append(h.state.JobItems, createdItems...)
	err := h.saveStateLocked(r.Context())
	if err != nil {
		h.state = snapshot
	}
	h.mu.Unlock()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to persist sync job")
		return
	}
	if queuedAny {
		h.wakeWorker()
	}

	respItems := make([]SyncJobItem, len(createdItems))
	copy(respItems, createdItems)
	if hasAuth && !isAdmin {
		for i := range respItems {
			respItems[i].OutputPath = ""
			respItems[i].SourcePath = ""
		}
	}

	httpx.JSON(w, http.StatusOK, SyncJobCreationResult{
		Job:      job,
		JobItems: respItems,
	})
}

func (h *Handler) handleGetJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("Id")
	callerID, isAdmin, hasAuth := callerAuth(r)
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, j := range h.state.Jobs {
		if j.Id == id {
			if !canAccessJob(j, callerID, isAdmin, hasAuth) {
				httpx.Error(w, http.StatusForbidden, "forbidden")
				return
			}
			httpx.JSON(w, http.StatusOK, j)
			return
		}
	}
	httpx.Error(w, http.StatusNotFound, "sync job not found")
}

func (h *Handler) handleUpdateJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("Id")
	var req struct {
		Name           string `json:"Name"`
		Quality        string `json:"Quality"`
		Bitrate        *int   `json:"Bitrate"`
		UnwatchedOnly  *bool  `json:"UnwatchedOnly"`
		SyncNewContent *bool  `json:"SyncNewContent"`
		ItemLimit      *int   `json:"ItemLimit"`
		Status         string `json:"Status"`
	}
	if err := httpx.DecodeJSONLimit(r, &req, 0); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid sync job update payload")
		return
	}
	if len([]rune(strings.TrimSpace(req.Name))) > maxSyncJobNameLen ||
		len([]rune(strings.TrimSpace(req.Quality))) > maxSyncQualityLen {
		httpx.Error(w, http.StatusBadRequest, "sync job field exceeds maximum length")
		return
	}
	if req.Bitrate != nil && *req.Bitrate < 0 {
		httpx.Error(w, http.StatusBadRequest, "Bitrate must be non-negative")
		return
	}
	if req.ItemLimit != nil && *req.ItemLimit < 0 {
		httpx.Error(w, http.StatusBadRequest, "ItemLimit must be non-negative")
		return
	}
	if req.Status != "" && !isValidSyncJobStatus(req.Status) {
		httpx.Error(w, http.StatusBadRequest, "invalid sync job Status")
		return
	}

	callerID, isAdmin, hasAuth := callerAuth(r)
	if hasAuth && !isAdmin {
		callerUser, ok := h.resolveCallerUserForSync(r, callerID)
		if !ok {
			httpx.Error(w, http.StatusForbidden, "forbidden")
			return
		}
		if !callerUser.IsAdmin && !callerUser.Policy.IsAdministrator && !callerUser.Policy.EnableContentDownloading {
			httpx.Error(w, http.StatusForbidden, "content downloading is disabled")
			return
		}
	}

	h.mu.Lock()
	for i := range h.state.Jobs {
		if h.state.Jobs[i].Id == id {
			if !canAccessJob(h.state.Jobs[i], callerID, isAdmin, hasAuth) {
				h.mu.Unlock()
				httpx.Error(w, http.StatusForbidden, "forbidden")
				return
			}
			prevJob := h.state.Jobs[i]
			if req.Name != "" {
				h.state.Jobs[i].Name = req.Name
			}
			if req.Quality != "" {
				h.state.Jobs[i].Quality = req.Quality
			}
			if req.Bitrate != nil {
				br := *req.Bitrate
				if br > maxSyncBitrate {
					br = maxSyncBitrate
				}
				h.state.Jobs[i].Bitrate = br
			}
			if req.UnwatchedOnly != nil {
				h.state.Jobs[i].UnwatchedOnly = *req.UnwatchedOnly
			}
			if req.SyncNewContent != nil {
				h.state.Jobs[i].SyncNewContent = *req.SyncNewContent
			}
			if req.ItemLimit != nil {
				h.state.Jobs[i].ItemLimit = *req.ItemLimit
			}
			if req.Status != "" {
				h.state.Jobs[i].Status = req.Status
			}
			h.state.Jobs[i].DateLastModified = time.Now().UTC()
			updated := h.state.Jobs[i]
			err := h.saveStateLocked(r.Context())
			if err != nil {
				h.state.Jobs[i] = prevJob
			}
			h.mu.Unlock()
			if err != nil {
				httpx.Error(w, http.StatusInternalServerError, "failed to persist sync job")
				return
			}
			httpx.JSON(w, http.StatusOK, updated)
			return
		}
	}
	h.mu.Unlock()
	httpx.Error(w, http.StatusNotFound, "sync job not found")
}

func (h *Handler) handleDeleteJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("Id")
	callerID, isAdmin, hasAuth := callerAuth(r)
	h.mu.Lock()
	found := false
	keptJobs := make([]SyncJob, 0, len(h.state.Jobs))
	for _, j := range h.state.Jobs {
		if j.Id == id {
			if !canAccessJob(j, callerID, isAdmin, hasAuth) {
				h.mu.Unlock()
				httpx.Error(w, http.StatusForbidden, "forbidden")
				return
			}
			found = true
			continue
		}
		keptJobs = append(keptJobs, j)
	}
	if !found {
		h.mu.Unlock()
		httpx.Error(w, http.StatusNotFound, "sync job not found")
		return
	}
	snapshot := cloneSyncState(h.state)
	h.state.Jobs = keptJobs

	keptItems := make([]SyncJobItem, 0, len(h.state.JobItems))
	var removedItemIDs []string
	var removedPaths []string
	for _, ji := range h.state.JobItems {
		if ji.JobId != id {
			keptItems = append(keptItems, ji)
		} else {
			removedItemIDs = append(removedItemIDs, ji.Id)
			if ji.OutputPath != "" {
				removedPaths = append(removedPaths, ji.OutputPath)
			}
		}
	}
	h.state.JobItems = keptItems
	err := h.saveStateLocked(r.Context())
	if err != nil {
		h.state = snapshot
	}
	h.mu.Unlock()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to persist sync state")
		return
	}
	h.cancelActiveTranscodeItems(removedItemIDs...)
	h.cleanupSyncCacheFiles(removedPaths...)
	h.cleanupSyncJobCacheDir(id)
	httpx.NoContent(w)
}

func (h *Handler) handleListJobItems(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	jobID := q.Get("JobId")
	targetID := q.Get("TargetId")
	statuses := q.List("Statuses")
	statusSet := make(map[string]struct{}, len(statuses))
	for _, s := range statuses {
		statusSet[strings.ToLower(s)] = struct{}{}
	}
	callerID, isAdmin, hasAuth := callerAuth(r)

	h.mu.RLock()
	jobByID := make(map[string]SyncJob, len(h.state.Jobs))
	for _, j := range h.state.Jobs {
		jobByID[j.Id] = j
	}
	if jobID != "" && hasAuth && !isAdmin {
		if j, ok := jobByID[jobID]; ok && !canAccessJob(j, callerID, isAdmin, hasAuth) {
			h.mu.RUnlock()
			httpx.Error(w, http.StatusForbidden, "forbidden")
			return
		}
	}
	filtered := make([]SyncJobItem, 0, len(h.state.JobItems))
	for _, ji := range h.state.JobItems {
		if hasAuth && !isAdmin {
			j, ok := jobByID[ji.JobId]
			if !ok || !canAccessJob(j, callerID, isAdmin, hasAuth) {
				continue
			}
		}
		if jobID != "" && ji.JobId != jobID {
			continue
		}
		if targetID != "" && ji.TargetId != targetID {
			continue
		}
		if len(statusSet) > 0 {
			if _, ok := statusSet[strings.ToLower(ji.Status)]; !ok {
				continue
			}
		}
		if hasAuth && !isAdmin {
			ji.OutputPath = ""
			ji.SourcePath = ""
		}
		filtered = append(filtered, ji)
	}
	h.mu.RUnlock()

	page, total := paginate(filtered, q.Int("StartIndex", 0), q.Int("Limit", 0))
	httpx.JSON(w, http.StatusOK, dto.QueryResult[SyncJobItem]{
		Items:            page,
		TotalRecordCount: total,
	})
}

func (h *Handler) handleDeleteJobItem(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("Id")
	callerID, isAdmin, hasAuth := callerAuth(r)
	h.mu.Lock()
	for i, ji := range h.state.JobItems {
		if ji.Id == id {
			jobID := ji.JobId
			if hasAuth && !isAdmin {
				j, ok := h.findJobLocked(jobID)
				if !ok || !canAccessJob(j, callerID, isAdmin, hasAuth) {
					h.mu.Unlock()
					httpx.Error(w, http.StatusForbidden, "forbidden")
					return
				}
			}
			outPath := ji.OutputPath
			snapshot := cloneSyncState(h.state)
			h.state.JobItems = append(h.state.JobItems[:i], h.state.JobItems[i+1:]...)
			h.recomputeJobLocked(jobID)
			err := h.saveStateLocked(r.Context())
			if err != nil {
				h.state = snapshot
			}
			h.mu.Unlock()
			if err != nil {
				httpx.Error(w, http.StatusInternalServerError, "failed to persist sync state")
				return
			}
			h.cancelActiveTranscodeItems(id)
			h.cleanupSyncCacheFiles(outPath)
			httpx.NoContent(w)
			return
		}
	}
	h.mu.Unlock()
	httpx.Error(w, http.StatusNotFound, "sync job item not found")
}

func (h *Handler) handleMarkTransferred(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("Id")
	callerID, isAdmin, hasAuth := callerAuth(r)
	h.mu.Lock()
	for i := range h.state.JobItems {
		if h.state.JobItems[i].Id == id {
			if hasAuth && !isAdmin {
				j, ok := h.findJobLocked(h.state.JobItems[i].JobId)
				if !ok || !canAccessJob(j, callerID, isAdmin, hasAuth) {
					h.mu.Unlock()
					httpx.Error(w, http.StatusForbidden, "forbidden")
					return
				}
			}
			prevOutPath := h.state.JobItems[i].OutputPath
			_, isTempCacheFile := h.resolveSafeSyncCacheTarget(prevOutPath)
			snapshot := cloneSyncState(h.state)
			h.state.JobItems[i].Status = "Synced"
			h.state.JobItems[i].Progress = 100
			if isTempCacheFile {
				h.state.JobItems[i].OutputPath = ""
			}
			h.recomputeJobLocked(h.state.JobItems[i].JobId)
			err := h.saveStateLocked(r.Context())
			if err != nil {
				h.state = snapshot
			}
			h.mu.Unlock()
			if err != nil {
				httpx.Error(w, http.StatusInternalServerError, "failed to persist sync state")
				return
			}
			if isTempCacheFile {
				h.cleanupSyncCacheFiles(prevOutPath)
			}
			httpx.NoContent(w)
			return
		}
	}
	h.mu.Unlock()
	httpx.Error(w, http.StatusNotFound, "sync job item not found")
}

func (h *Handler) handleEnableJobItem(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("Id")
	callerID, isAdmin, hasAuth := callerAuth(r)
	h.mu.Lock()
	for i := range h.state.JobItems {
		if h.state.JobItems[i].Id == id {
			if hasAuth && !isAdmin {
				j, ok := h.findJobLocked(h.state.JobItems[i].JobId)
				if !ok || !canAccessJob(j, callerID, isAdmin, hasAuth) {
					h.mu.Unlock()
					httpx.Error(w, http.StatusForbidden, "forbidden")
					return
				}
			}
			snapshot := cloneSyncState(h.state)
			h.state.JobItems[i].Status = "ReadyToTransfer"
			h.state.JobItems[i].Progress = 0
			h.recomputeJobLocked(h.state.JobItems[i].JobId)
			err := h.saveStateLocked(r.Context())
			if err != nil {
				h.state = snapshot
			}
			h.mu.Unlock()
			if err != nil {
				httpx.Error(w, http.StatusInternalServerError, "failed to persist sync state")
				return
			}
			httpx.NoContent(w)
			return
		}
	}
	h.mu.Unlock()
	httpx.Error(w, http.StatusNotFound, "sync job item not found")
}

func (h *Handler) resolveCallerUserForSync(r *http.Request, callerID string) (*sqlite.UserRecord, bool) {
	if userRepo := h.getUserRepo(); userRepo != nil {
		if callerID == "" {
			return nil, false
		}
		u, err := userRepo.GetUserByID(r.Context(), callerID)
		if err != nil || u == nil || u.IsDisabled {
			return nil, false
		}
		return u, true
	}
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && ac.User != nil {
		if ac.User.IsDisabled {
			return nil, false
		}
		return ac.User, true
	}
	return nil, false
}

func (h *Handler) isPlaylistPublic(ctx context.Context, itemID string) bool {
	if h.store == nil {
		return false
	}
	normID := guid.Normalize(itemID)
	if normID == "" {
		return false
	}
	var raw string
	err := h.store.Reader.QueryRowContext(ctx,
		`SELECT value_json FROM server_config WHERE key = ?`,
		"item_visibility_"+normID,
	).Scan(&raw)
	if err != nil {
		return false
	}
	var vis struct {
		IsPublic bool `json:"IsPublic"`
	}
	return json.Unmarshal([]byte(raw), &vis) == nil && vis.IsPublic
}

func (h *Handler) canUserAccessItemRecord(ctx context.Context, libRepo *sqlite.LibraryRepo, callerUser *sqlite.UserRecord, rec *sqlite.ItemRecord) bool {
	if callerUser == nil || rec == nil || callerUser.IsAdmin || callerUser.Policy.IsAdministrator {
		return true
	}
	if strings.EqualFold(rec.Type, "Playlist") {
		ownerID := guid.Normalize(rec.ParentID)
		callerID := guid.Normalize(callerUser.ID)
		if ownerID != callerID {
			if !h.isPlaylistPublic(ctx, rec.ID) {
				return false
			}
		}
	} else if rec.LibraryID != "" {
		if libRepo != nil {
			if !libRepo.CanUserAccessLibrary(ctx, callerUser.Policy, rec.LibraryID) {
				return false
			}
		} else if !callerUser.Policy.EnableAllFolders {
			normLib := guid.Normalize(rec.LibraryID)
			allowed := false
			for _, fid := range callerUser.Policy.EnabledFolders {
				if guid.Normalize(fid) == normLib {
					allowed = true
					break
				}
			}
			if !allowed {
				return false
			}
		}
	}
	if libRepo != nil {
		blocked, err := libRepo.IsItemBlockedByPolicy(ctx, rec, callerUser.Policy)
		if err != nil || blocked {
			return false
		}
	}
	return true
}

func (h *Handler) handleReadyItems(w http.ResponseWriter, r *http.Request) {
	targetID := httpx.ParseQuery(r).Get("TargetId")
	callerID, isAdmin, hasAuth := callerAuth(r)

	var callerUser *sqlite.UserRecord
	if hasAuth && !isAdmin {
		u, ok := h.resolveCallerUserForSync(r, callerID)
		if !ok {
			httpx.Error(w, http.StatusForbidden, "forbidden")
			return
		}
		if !u.IsAdmin && !u.Policy.IsAdministrator && !u.Policy.EnableContentDownloading {
			httpx.Error(w, http.StatusForbidden, "content downloading is disabled")
			return
		}
		callerUser = u
	}

	type readyEntry struct {
		item   SyncedItem
		userID string
	}

	h.mu.RLock()
	jobByID := make(map[string]SyncJob, len(h.state.Jobs))
	for _, j := range h.state.Jobs {
		jobByID[j.Id] = j
	}
	entries := make([]readyEntry, 0)
	for _, ji := range h.state.JobItems {
		j, hasJob := jobByID[ji.JobId]
		if hasAuth && !isAdmin {
			if !hasJob || !canAccessJob(j, callerID, isAdmin, hasAuth) {
				continue
			}
		}
		if targetID != "" && ji.TargetId != targetID {
			continue
		}
		if ji.Status == "ReadyToTransfer" || ji.Status == "Synced" {
			origFile := ""
			if ji.OutputPath != "" {
				origFile = filepath.Base(ji.OutputPath)
			}
			entries = append(entries, readyEntry{
				item: SyncedItem{
					ServerId:      h.serverID(),
					SyncJobId:     ji.JobId,
					SyncJobName:   j.Name,
					SyncJobItemId: ji.Id,
					ItemId:        ji.ItemId,
					OriginalFile:  origFile,
				},
				userID: j.UserId,
			})
		}
	}
	h.mu.RUnlock()

	libRepo := h.getLibraryRepo()
	serverID := h.serverID()
	ready := make([]SyncedItem, 0, len(entries))
	for _, e := range entries {
		entry := e.item
		if libRepo != nil {
			if hasAuth && !isAdmin {
				rec, recErr := libRepo.GetItemRecordByID(r.Context(), entry.ItemId)
				if recErr != nil || rec == nil || !h.canUserAccessItemRecord(r.Context(), libRepo, callerUser, rec) {
					continue
				}
			}
			if itemDTO, err := libRepo.GetItemByID(r.Context(), serverID, e.userID, entry.ItemId); err == nil && itemDTO != nil {
				if entry.OriginalFile == "" && itemDTO.Path != "" {
					entry.OriginalFile = filepath.Base(itemDTO.Path)
				}
				if hasAuth && !isAdmin {
					dto.SanitizeItemPaths(itemDTO)
				}
				entry.Item = itemDTO
			}
		}
		ready = append(ready, entry)
	}
	httpx.JSON(w, http.StatusOK, ready)
}

func isSyncContainerType(itemType string) bool {
	switch strings.ToLower(strings.TrimSpace(itemType)) {
	case "series", "season", "playlist", "boxset", "folder", "musicartist", "musicalbum", "collectionfolder":
		return true
	default:
		return false
	}
}

func (h *Handler) expandSyncContainer(
	ctx context.Context,
	libRepo *sqlite.LibraryRepo,
	serverID, userID string,
	callerUser *sqlite.UserRecord,
	container *sqlite.ItemRecord,
	visited map[string]struct{},
	seenExpanded map[string]struct{},
	out *[]string,
) {
	if container == nil || len(*out) >= maxSyncJobItemIDs {
		return
	}
	normContainerID := guid.Normalize(container.ID)
	if normContainerID == "" {
		return
	}
	if _, seen := visited[normContainerID]; seen {
		return
	}
	visited[normContainerID] = struct{}{}

	var childIDs []string
	seenChild := make(map[string]struct{})
	addChildID := func(id string) {
		norm := guid.Normalize(id)
		if norm == "" || norm == normContainerID {
			return
		}
		if _, ok := seenChild[norm]; ok {
			return
		}
		seenChild[norm] = struct{}{}
		childIDs = append(childIDs, id)
	}

	switch strings.ToLower(strings.TrimSpace(container.Type)) {
	case "playlist", "boxset":
		if entries, _, err := libRepo.GetContainerItems(ctx, serverID, userID, container.ID, 0, maxSyncJobItemIDs, nil); err == nil {
			for _, e := range entries {
				addChildID(e.Id)
			}
		}
	case "series":
		if items, _, err := libRepo.QueryItems(ctx, sqlite.ItemFilter{
			ServerID: serverID,
			SeriesID: container.ID,
			Limit:    maxSyncJobItemIDs,
		}); err == nil {
			for _, it := range items {
				addChildID(it.Id)
			}
		}
		if items, _, err := libRepo.QueryItems(ctx, sqlite.ItemFilter{
			ServerID:  serverID,
			ParentID:  container.ID,
			Recursive: true,
			Limit:     maxSyncJobItemIDs,
		}); err == nil {
			for _, it := range items {
				addChildID(it.Id)
			}
		}
	case "season":
		if items, _, err := libRepo.QueryItems(ctx, sqlite.ItemFilter{
			ServerID: serverID,
			SeasonID: container.ID,
			Limit:    maxSyncJobItemIDs,
		}); err == nil {
			for _, it := range items {
				addChildID(it.Id)
			}
		}
		if items, _, err := libRepo.QueryItems(ctx, sqlite.ItemFilter{
			ServerID:  serverID,
			ParentID:  container.ID,
			Recursive: true,
			Limit:     maxSyncJobItemIDs,
		}); err == nil {
			for _, it := range items {
				addChildID(it.Id)
			}
		}
	default:
		if items, _, err := libRepo.QueryItems(ctx, sqlite.ItemFilter{
			ServerID:  serverID,
			ParentID:  container.ID,
			Recursive: true,
			Limit:     maxSyncJobItemIDs,
		}); err == nil {
			for _, it := range items {
				addChildID(it.Id)
			}
		}
	}

	for _, cid := range childIDs {
		if len(*out) >= maxSyncJobItemIDs {
			return
		}
		childRec, err := libRepo.GetItemRecordByID(ctx, cid)
		if err != nil || childRec == nil {
			continue
		}
		if callerUser != nil && !callerUser.IsAdmin && !callerUser.Policy.IsAdministrator {
			if !h.canUserAccessItemRecord(ctx, libRepo, callerUser, childRec) {
				continue
			}
		}
		if isSyncContainerType(childRec.Type) {
			h.expandSyncContainer(ctx, libRepo, serverID, userID, callerUser, childRec, visited, seenExpanded, out)
			continue
		}
		if childRec.IsStrm || strings.EqualFold(filepath.Ext(childRec.Path), ".strm") {
			continue
		}
		normChildID := guid.Normalize(childRec.ID)
		if _, dup := seenExpanded[normChildID]; dup {
			continue
		}
		seenExpanded[normChildID] = struct{}{}
		*out = append(*out, childRec.ID)
	}
}

func (h *Handler) handleSyncData(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LocalItemIds []string `json:"LocalItemIds"`
		TargetId     string   `json:"TargetId"`
	}
	if err := httpx.DecodeJSONLimit(r, &req, 0); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid sync data payload")
		return
	}
	callerID, isAdmin, hasAuth := callerAuth(r)

	h.mu.RLock()
	jobByID := make(map[string]SyncJob, len(h.state.Jobs))
	for _, j := range h.state.Jobs {
		jobByID[j.Id] = j
	}
	active := make(map[string]struct{})
	for _, ji := range h.state.JobItems {
		if hasAuth && !isAdmin {
			j, ok := jobByID[ji.JobId]
			if !ok || !canAccessJob(j, callerID, isAdmin, hasAuth) {
				continue
			}
		}
		if req.TargetId != "" && ji.TargetId != req.TargetId {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(ji.Status)) {
		case "removing", "cancelled", "failed":
			continue
		}
		if normID := guid.Normalize(ji.ItemId); normID != "" {
			active[normID] = struct{}{}
		}
	}
	h.mu.RUnlock()

	toRemove := make([]string, 0)
	for _, localID := range req.LocalItemIds {
		normLocal := guid.Normalize(localID)
		if _, ok := active[normLocal]; !ok {
			toRemove = append(toRemove, localID)
		}
	}

	httpx.JSON(w, http.StatusOK, map[string]any{
		"ItemIdsToRemove": toRemove,
	})
}

func (h *Handler) recomputeJobLocked(jobID string) {
	total := 0
	synced := 0
	converting := 0
	queued := 0
	failed := 0
	for _, ji := range h.state.JobItems {
		if ji.JobId == jobID {
			total++
			switch ji.Status {
			case "Synced":
				synced++
			case "Converting":
				converting++
			case "Queued":
				queued++
			case "Failed":
				failed++
			}
		}
	}
	for i := range h.state.Jobs {
		if h.state.Jobs[i].Id == jobID {
			h.state.Jobs[i].ItemCount = total
			h.state.Jobs[i].DateLastModified = time.Now().UTC()
			if total == 0 {
				h.state.Jobs[i].Progress = 100
				h.state.Jobs[i].Status = "Completed"
			} else {
				h.state.Jobs[i].Progress = float64(synced) * 100.0 / float64(total)
				switch {
				case synced == total:
					h.state.Jobs[i].Status = "Completed"
				case converting > 0:
					h.state.Jobs[i].Status = "Converting"
				case queued > 0:
					h.state.Jobs[i].Status = "Queued"
				case failed == total:
					h.state.Jobs[i].Status = "Failed"
				default:
					h.state.Jobs[i].Status = "ReadyToTransfer"
				}
			}
			break
		}
	}
}

func (h *Handler) loadState(ctx context.Context) {
	if h.store == nil {
		return
	}
	var raw string
	err := h.store.Reader.QueryRowContext(ctx,
		`SELECT value_json FROM server_config WHERE key = ?`, stateConfigKey,
	).Scan(&raw)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return
		}
		return
	}
	var st syncState
	if err := json.Unmarshal([]byte(raw), &st); err == nil {
		h.mu.Lock()
		h.state = st
		h.mu.Unlock()
	}
}

func (h *Handler) saveStateLocked(ctx context.Context) error {
	if h.store == nil {
		return nil
	}
	raw, err := json.Marshal(h.state)
	if err != nil {
		slog.Error("sync: marshal state failed", "err", err)
		return err
	}
	_, err = h.store.Writer.ExecContext(ctx, `
		INSERT INTO server_config (key, value_json, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			value_json = excluded.value_json,
			updated_at = excluded.updated_at`,
		stateConfigKey, string(raw), time.Now().UTC().Unix(),
	)
	if err != nil {
		slog.Error("sync: save state failed", "err", err)
	}
	return err
}

func paginate[T any](items []T, startIndex, limit int) ([]T, int) {
	total := len(items)
	if startIndex < 0 {
		startIndex = 0
	}
	if startIndex >= total {
		return []T{}, total
	}
	end := total
	if limit > 0 && startIndex+limit < end {
		end = startIndex + limit
	}
	out := make([]T, end-startIndex)
	copy(out, items[startIndex:end])
	return out, total
}

func passthrough(next http.Handler) http.Handler { return next }
