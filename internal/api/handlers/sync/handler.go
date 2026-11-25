package sync

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

const stateConfigKey = "sync:state"

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
	store    *sqlite.Store
	cfg      *config.Config
	libRepo  *sqlite.LibraryRepo
	userRepo *sqlite.UserRepo
	auth     AuthFuncs

	mu    gosync.RWMutex
	state syncState
}

func New(store *sqlite.Store, cfg *config.Config) *Handler {
	h := &Handler{
		store: store,
		cfg:   cfg,
	}
	h.loadState(context.Background())
	return h
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

func (h *Handler) resolveTargets(ctx context.Context) []SyncTarget {
	if userRepo := h.getUserRepo(); userRepo != nil {
		if devices, err := userRepo.ListDevices(ctx); err == nil && len(devices) > 0 {
			targets := make([]SyncTarget, 0, len(devices))
			for _, d := range devices {
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
	httpx.JSON(w, http.StatusOK, h.resolveTargets(r.Context()))
}

func (h *Handler) handleOptions(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, SyncDialogOptions{
		Targets: h.resolveTargets(r.Context()),
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid sync job payload")
		return
	}
	if req.TargetId == "" {
		req.TargetId = "local-device"
	}
	callerID, isAdmin, hasAuth := callerAuth(r)
	if hasAuth && !isAdmin {
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
	createdItems := make([]SyncJobItem, 0, len(req.ItemIds))
	for i, itemID := range req.ItemIds {
		itemName := "Item " + strconv.Itoa(i+1)
		outputPath := ""
		mediaSourceID := ""
		if libRepo != nil {
			if itemDTO, err := libRepo.GetItemByID(r.Context(), serverID, job.UserId, itemID); err == nil && itemDTO != nil {
				if itemDTO.Name != "" {
					itemName = itemDTO.Name
				}
				if itemDTO.Path != "" {
					outputPath = itemDTO.Path
				}
				if len(itemDTO.MediaSources) > 0 && itemDTO.MediaSources[0].Id != "" {
					mediaSourceID = itemDTO.MediaSources[0].Id
				} else if itemDTO.Id != "" {
					mediaSourceID = itemDTO.Id
				}
			}
		}
		createdItems = append(createdItems, SyncJobItem{
			Id:          guid.New(),
			JobId:       job.Id,
			ItemId:      itemID,
			ItemName:    itemName,
			TargetId:    job.TargetId,
			Status:      "ReadyToTransfer",
			OutputPath:  outputPath,
			MediaSource: mediaSourceID,
		})
	}

	h.mu.Lock()
	h.state.Jobs = append(h.state.Jobs, job)
	h.state.JobItems = append(h.state.JobItems, createdItems...)
	h.saveStateLocked(r.Context())
	h.mu.Unlock()

	httpx.JSON(w, http.StatusOK, SyncJobCreationResult{
		Job:      job,
		JobItems: createdItems,
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid sync job update payload")
		return
	}

	callerID, isAdmin, hasAuth := callerAuth(r)

	h.mu.Lock()
	for i := range h.state.Jobs {
		if h.state.Jobs[i].Id == id {
			if !canAccessJob(h.state.Jobs[i], callerID, isAdmin, hasAuth) {
				h.mu.Unlock()
				httpx.Error(w, http.StatusForbidden, "forbidden")
				return
			}
			if req.Name != "" {
				h.state.Jobs[i].Name = req.Name
			}
			if req.Quality != "" {
				h.state.Jobs[i].Quality = req.Quality
			}
			if req.Bitrate != nil {
				h.state.Jobs[i].Bitrate = *req.Bitrate
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
			h.saveStateLocked(r.Context())
			h.mu.Unlock()
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
	h.state.Jobs = keptJobs

	keptItems := make([]SyncJobItem, 0, len(h.state.JobItems))
	for _, ji := range h.state.JobItems {
		if ji.JobId != id {
			keptItems = append(keptItems, ji)
		}
	}
	h.state.JobItems = keptItems
	h.saveStateLocked(r.Context())
	h.mu.Unlock()
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
			h.state.JobItems = append(h.state.JobItems[:i], h.state.JobItems[i+1:]...)
			h.recomputeJobLocked(jobID)
			h.saveStateLocked(r.Context())
			h.mu.Unlock()
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
			h.state.JobItems[i].Status = "Synced"
			h.state.JobItems[i].Progress = 100
			h.recomputeJobLocked(h.state.JobItems[i].JobId)
			h.saveStateLocked(r.Context())
			h.mu.Unlock()
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
			h.state.JobItems[i].Status = "ReadyToTransfer"
			h.state.JobItems[i].Progress = 0
			h.recomputeJobLocked(h.state.JobItems[i].JobId)
			h.saveStateLocked(r.Context())
			h.mu.Unlock()
			httpx.NoContent(w)
			return
		}
	}
	h.mu.Unlock()
	httpx.Error(w, http.StatusNotFound, "sync job item not found")
}

func (h *Handler) handleReadyItems(w http.ResponseWriter, r *http.Request) {
	targetID := httpx.ParseQuery(r).Get("TargetId")
	callerID, isAdmin, hasAuth := callerAuth(r)

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
		if ji.Status == "ReadyToTransfer" || ji.Status == "Queued" {
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
	ready := make([]SyncedItem, len(entries))
	for i, e := range entries {
		ready[i] = e.item
		if libRepo != nil {
			if itemDTO, err := libRepo.GetItemByID(r.Context(), serverID, e.userID, e.item.ItemId); err == nil && itemDTO != nil {
				ready[i].Item = itemDTO
				if ready[i].OriginalFile == "" && itemDTO.Path != "" {
					ready[i].OriginalFile = filepath.Base(itemDTO.Path)
				}
			}
		}
	}
	httpx.JSON(w, http.StatusOK, ready)
}

func (h *Handler) handleSyncData(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LocalItemIds []string `json:"LocalItemIds"`
		TargetId     string   `json:"TargetId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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
		active[ji.ItemId] = struct{}{}
	}
	h.mu.RUnlock()

	toRemove := make([]string, 0)
	for _, localID := range req.LocalItemIds {
		if _, ok := active[localID]; !ok {
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
	for _, ji := range h.state.JobItems {
		if ji.JobId == jobID {
			total++
			if ji.Status == "Synced" {
				synced++
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
				if synced == total {
					h.state.Jobs[i].Status = "Completed"
				} else {
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

func (h *Handler) saveStateLocked(ctx context.Context) {
	if h.store == nil {
		return
	}
	raw, err := json.Marshal(h.state)
	if err != nil {
		return
	}
	_, _ = h.store.Writer.ExecContext(ctx, `
		INSERT INTO server_config (key, value_json, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			value_json = excluded.value_json,
			updated_at = excluded.updated_at`,
		stateConfigKey, string(raw), time.Now().UTC().Unix(),
	)
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
