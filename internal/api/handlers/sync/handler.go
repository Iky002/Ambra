package sync

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	gosync "sync"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/httpx"
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
	ServerId      string `json:"ServerId"`
	SyncJobId     string `json:"SyncJobId"`
	SyncJobName   string `json:"SyncJobName"`
	SyncJobItemId string `json:"SyncJobItemId"`
	ItemId        string `json:"ItemId"`
	OriginalFile  string `json:"OriginalFileName,omitempty"`
}

type syncState struct {
	Jobs     []SyncJob     `json:"jobs"`
	JobItems []SyncJobItem `json:"job_items"`
}

type Handler struct {
	store *sqlite.Store
	cfg   *config.Config
	auth  AuthFuncs

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

func defaultTargets() []SyncTarget {
	return []SyncTarget{
		{Id: "local-device", Name: "This Device"},
	}
}

func (h *Handler) handleTargets(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, defaultTargets())
}

func (h *Handler) handleOptions(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, SyncDialogOptions{
		Targets: defaultTargets(),
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
	if req.UserId == "" && h.auth.UserIDFromCtx != nil {
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

	createdItems := make([]SyncJobItem, 0, len(req.ItemIds))
	for i, itemID := range req.ItemIds {
		createdItems = append(createdItems, SyncJobItem{
			Id:       guid.New(),
			JobId:    job.Id,
			ItemId:   itemID,
			ItemName: "Item " + strconv.Itoa(i+1),
			TargetId: job.TargetId,
			Status:   "ReadyToTransfer",
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
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, j := range h.state.Jobs {
		if j.Id == id {
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

	h.mu.Lock()
	for i := range h.state.Jobs {
		if h.state.Jobs[i].Id == id {
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
	h.mu.Lock()
	found := false
	keptJobs := make([]SyncJob, 0, len(h.state.Jobs))
	for _, j := range h.state.Jobs {
		if j.Id == id {
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

	h.mu.RLock()
	filtered := make([]SyncJobItem, 0, len(h.state.JobItems))
	for _, ji := range h.state.JobItems {
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
	h.mu.Lock()
	for i, ji := range h.state.JobItems {
		if ji.Id == id {
			jobID := ji.JobId
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
	h.mu.Lock()
	for i := range h.state.JobItems {
		if h.state.JobItems[i].Id == id {
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
	h.mu.Lock()
	for i := range h.state.JobItems {
		if h.state.JobItems[i].Id == id {
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
	h.mu.RLock()
	jobNames := make(map[string]string, len(h.state.Jobs))
	for _, j := range h.state.Jobs {
		jobNames[j.Id] = j.Name
	}
	ready := make([]SyncedItem, 0)
	for _, ji := range h.state.JobItems {
		if targetID != "" && ji.TargetId != targetID {
			continue
		}
		if ji.Status == "ReadyToTransfer" || ji.Status == "Queued" {
			ready = append(ready, SyncedItem{
				ServerId:      h.serverID(),
				SyncJobId:     ji.JobId,
				SyncJobName:   jobNames[ji.JobId],
				SyncJobItemId: ji.Id,
				ItemId:        ji.ItemId,
			})
		}
	}
	h.mu.RUnlock()
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

	h.mu.RLock()
	active := make(map[string]struct{})
	for _, ji := range h.state.JobItems {
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
