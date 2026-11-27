package compatext

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	syncapi "github.com/ambra-media/ambra/internal/api/handlers/sync"
	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

const (
	maxOfflineActions              = 500
	maxRemovedJobItems             = 500
	maxNotificationServices        = 500
	maxNotificationServicesPerUser = 50
	maxNotificationBodyBytes       = 64 << 10
	maskedOptionValue              = "********"
)

// Mirror structures for reading/updating shared sync:state stored in server_config.
type sharedSyncJob struct {
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

type sharedSyncJobItem struct {
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

type sharedSyncState struct {
	Jobs     []sharedSyncJob     `json:"jobs"`
	JobItems []sharedSyncJobItem `json:"job_items"`
}

func (h *Handler) registerSyncRoutes(r chi.Router, wrapAuth func(http.Handler) http.Handler) {
	r.Group(func(r chi.Router) {
		r.Use(wrapAuth)

		// Sync (12 routes)
		r.Post("/Sync/Jobs/{Id}/Delete", h.handleDeleteSyncJobPost)
		r.Post("/Sync/{ItemId}/Status", h.handleSyncItemStatus)
		r.Post("/Sync/JobItems/{Id}/MarkForRemoval", h.handleMarkJobItemRemoval(true))
		r.Post("/Sync/JobItems/{Id}/UnmarkForRemoval", h.handleMarkJobItemRemoval(false))
		r.Post("/Sync/JobItems/{Id}/Delete", h.handleDeleteSyncJobItemPost)
		r.Post("/Sync/Items/Cancel", h.handleCancelSyncItems)
		r.Post("/Sync/{TargetId}/Items/Delete", h.handleCancelSyncItems)
		r.Delete("/Sync/{TargetId}/Items", h.handleCancelSyncItems)
		r.Head("/Sync/JobItems/{Id}/File", h.handleSyncJobItemFile(true))
		r.Get("/Sync/JobItems/{Id}/File", h.handleSyncJobItemFile(false))
		r.Get("/Sync/JobItems/{Id}/AdditionalFiles", h.handleSyncJobItemAdditionalFiles)
		r.Post("/Sync/OfflineActions", h.handleSyncOfflineActions)
	})
}

func (h *Handler) registerNotificationRoutes(r chi.Router, wrapAuth func(http.Handler) http.Handler) {
	r.Group(func(r chi.Router) {
		r.Use(wrapAuth)

		// Notifications (5 routes)
		r.Get("/Notifications/Services/Configured", h.handleGetConfiguredNotifications)
		r.Post("/Notifications/Services/Configured", h.handleSaveConfiguredNotification)
		r.Get("/Notifications/Services/Defaults", h.handleNotificationServiceDefaults)
		r.Post("/Notifications/Services/Test", h.handleTestNotificationService)
		r.Delete("/Notifications/Services/Configured", h.handleDeleteConfiguredNotification)
	})
}

func (h *Handler) syncCallerIdentity(r *http.Request) (callerID string, isAdmin bool, hasAuth bool) {
	callerID, isAdmin = h.callerIdentity(r)
	hasAuth = callerID != "" || isAdmin
	return callerID, isAdmin, hasAuth
}

func sharedJobOwner(jobs []sharedSyncJob, jobID string) string {
	for _, j := range jobs {
		if j.Id == jobID {
			return guid.Normalize(j.UserId)
		}
	}
	return ""
}

func (h *Handler) cleanupRemovedJobItems(r *http.Request, ids []string) {
	if len(ids) == 0 {
		return
	}
	h.mu.Lock()
	snapshot := cloneCompatExtState(h.state)
	changed := false
	for _, id := range ids {
		if h.state.RemovedJobItems[id] {
			delete(h.state.RemovedJobItems, id)
			changed = true
		}
	}
	if changed {
		if err := h.saveStateLocked(r.Context()); err != nil {
			h.state = snapshot
		}
	}
	h.mu.Unlock()
}

func (h *Handler) handleDeleteSyncJobPost(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "Id")
	callerID, isAdmin, hasAuth := h.syncCallerIdentity(r)
	if sh := h.getSyncHandler(); sh != nil {
		removedItemIDs := sh.JobItemIDsForJob(id)
		found, allowed, err := sh.DeleteJobForCaller(r.Context(), id, callerID, isAdmin || !hasAuth)
		if !allowed {
			httpx.Error(w, http.StatusForbidden, "access denied")
			return
		}
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "failed to persist sync job deletion")
			return
		}
		if found {
			h.cleanupRemovedJobItems(r, removedItemIDs)
		}
		httpx.NoContent(w)
		return
	}

	var removedItemIDs []string
	var removedCachePaths []string
	var st sharedSyncState
	if h.readConfigJSON(r.Context(), syncConfigKey, &st) {
		jobs := make([]sharedSyncJob, 0, len(st.Jobs))
		for _, j := range st.Jobs {
			if j.Id == id {
				if hasAuth && !isAdmin && guid.Normalize(j.UserId) != callerID {
					httpx.Error(w, http.StatusForbidden, "access denied")
					return
				}
				continue
			}
			jobs = append(jobs, j)
		}
		items := make([]sharedSyncJobItem, 0, len(st.JobItems))
		for _, it := range st.JobItems {
			if it.JobId != id {
				items = append(items, it)
			} else {
				removedItemIDs = append(removedItemIDs, it.Id)
				if it.OutputPath != "" {
					removedCachePaths = append(removedCachePaths, it.OutputPath)
				}
			}
		}
		st.Jobs = jobs
		st.JobItems = items
		if err := h.writeConfigJSON(r.Context(), syncConfigKey, st); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "failed to persist sync job deletion")
			return
		}
	}
	h.cleanupSyncCacheFiles(removedCachePaths...)
	h.cleanupSyncJobCacheDir(id)
	h.cleanupRemovedJobItems(r, removedItemIDs)
	httpx.NoContent(w)
}

func (h *Handler) handleSyncItemStatus(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "ItemId")
	q := httpx.ParseQuery(r)
	targetID := q.Get("TargetId")
	if targetID == "" && r.Body != nil {
		var body struct {
			TargetId string `json:"TargetId"`
		}
		if err := httpx.DecodeJSONLimit(r, &body, 0); err == nil {
			targetID = body.TargetId
		}
	}

	callerID, isAdmin, hasAuth := h.syncCallerIdentity(r)
	if sh := h.getSyncHandler(); sh != nil {
		if it, ok := sh.GetItemStatusForCaller(itemID, targetID, callerID, isAdmin || !hasAuth); ok && it != nil {
			httpx.JSON(w, http.StatusOK, map[string]any{
				"Status":        it.Status,
				"SyncJobItemId": it.Id,
				"ItemId":        it.ItemId,
				"TargetId":      it.TargetId,
				"Progress":      it.Progress,
			})
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{
			"Status":   "None",
			"ItemId":   itemID,
			"TargetId": targetID,
		})
		return
	}

	var st sharedSyncState
	_ = h.readConfigJSON(r.Context(), syncConfigKey, &st)
	for _, it := range st.JobItems {
		if it.ItemId == itemID && (targetID == "" || it.TargetId == targetID) {
			if hasAuth && !isAdmin {
				ownerID := sharedJobOwner(st.Jobs, it.JobId)
				if ownerID == "" || ownerID != callerID {
					continue
				}
			}
			httpx.JSON(w, http.StatusOK, map[string]any{
				"Status":        it.Status,
				"SyncJobItemId": it.Id,
				"ItemId":        it.ItemId,
				"TargetId":      it.TargetId,
				"Progress":      it.Progress,
			})
			return
		}
	}

	httpx.JSON(w, http.StatusOK, map[string]any{
		"Status":   "None",
		"ItemId":   itemID,
		"TargetId": targetID,
	})
}

func (h *Handler) handleMarkJobItemRemoval(mark bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "Id")
		callerID, isAdmin, hasAuth := h.syncCallerIdentity(r)
		found := false

		if sh := h.getSyncHandler(); sh != nil {
			var allowed bool
			var err error
			found, allowed, err = sh.SetJobItemRemovalForCaller(r.Context(), id, mark, callerID, isAdmin || !hasAuth)
			if !allowed {
				httpx.Error(w, http.StatusForbidden, "access denied")
				return
			}
			if err != nil {
				httpx.Error(w, http.StatusInternalServerError, "failed to persist sync job item status")
				return
			}
		} else {
			var st sharedSyncState
			if h.readConfigJSON(r.Context(), syncConfigKey, &st) {
				for i := range st.JobItems {
					if st.JobItems[i].Id == id {
						if hasAuth && !isAdmin {
							ownerID := sharedJobOwner(st.Jobs, st.JobItems[i].JobId)
							if ownerID == "" || ownerID != callerID {
								httpx.Error(w, http.StatusForbidden, "access denied")
								return
							}
						}
						if mark {
							st.JobItems[i].Status = "Removing"
						} else {
							st.JobItems[i].Status = "Synced"
						}
						found = true
					}
				}
				if found {
					if err := h.writeConfigJSON(r.Context(), syncConfigKey, st); err != nil {
						httpx.Error(w, http.StatusInternalServerError, "failed to persist sync job item status")
						return
					}
				}
			}
		}

		h.mu.Lock()
		h.ensureMapsLocked()
		snapshot := cloneCompatExtState(h.state)
		var saveErr error
		if mark {
			if found {
				if !h.state.RemovedJobItems[id] && len(h.state.RemovedJobItems) >= maxRemovedJobItems {
					h.mu.Unlock()
					httpx.Error(w, http.StatusBadRequest, "removed job items limit reached")
					return
				}
				h.state.RemovedJobItems[id] = true
				saveErr = h.saveStateLocked(r.Context())
			}
		} else {
			if h.state.RemovedJobItems[id] {
				delete(h.state.RemovedJobItems, id)
				saveErr = h.saveStateLocked(r.Context())
			}
		}
		if saveErr != nil {
			h.state = snapshot
		}
		h.mu.Unlock()
		if saveErr != nil {
			httpx.Error(w, http.StatusInternalServerError, "failed to persist removal mark")
			return
		}

		httpx.NoContent(w)
	}
}

func (h *Handler) handleDeleteSyncJobItemPost(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "Id")
	callerID, isAdmin, hasAuth := h.syncCallerIdentity(r)
	if sh := h.getSyncHandler(); sh != nil {
		_, allowed, err := sh.DeleteJobItemForCaller(r.Context(), id, callerID, isAdmin || !hasAuth)
		if !allowed {
			httpx.Error(w, http.StatusForbidden, "access denied")
			return
		}
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "failed to persist sync job item deletion")
			return
		}
		h.cleanupRemovedJobItems(r, []string{id})
		httpx.NoContent(w)
		return
	}

	var removedCachePath string
	var st sharedSyncState
	if h.readConfigJSON(r.Context(), syncConfigKey, &st) {
		items := make([]sharedSyncJobItem, 0, len(st.JobItems))
		for _, it := range st.JobItems {
			if it.Id == id {
				if hasAuth && !isAdmin {
					ownerID := sharedJobOwner(st.Jobs, it.JobId)
					if ownerID == "" || ownerID != callerID {
						httpx.Error(w, http.StatusForbidden, "access denied")
						return
					}
				}
				removedCachePath = it.OutputPath
				continue
			}
			items = append(items, it)
		}
		st.JobItems = items
		if err := h.writeConfigJSON(r.Context(), syncConfigKey, st); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "failed to persist sync job item deletion")
			return
		}
	}
	if removedCachePath != "" {
		h.cleanupSyncCacheFiles(removedCachePath)
	}
	h.cleanupRemovedJobItems(r, []string{id})
	httpx.NoContent(w)
}

func (h *Handler) handleCancelSyncItems(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	targetID := strings.TrimSpace(chi.URLParam(r, "TargetId"))
	if targetID == "" {
		targetID = strings.TrimSpace(q.Get("TargetId"))
	}
	itemIDs := q.List("ItemIds")
	if len(itemIDs) == 0 && r.Body != nil {
		var body struct {
			TargetId string   `json:"TargetId"`
			ItemIds  []string `json:"ItemIds"`
		}
		if err := httpx.DecodeJSONLimit(r, &body, 0); err == nil {
			if targetID == "" {
				targetID = strings.TrimSpace(body.TargetId)
			}
			itemIDs = body.ItemIds
		}
	}

	cleanedItemIDs := make([]string, 0, len(itemIDs))
	idSet := make(map[string]struct{}, len(itemIDs))
	for _, id := range itemIDs {
		if norm := guid.Normalize(id); norm != "" {
			cleanedItemIDs = append(cleanedItemIDs, norm)
			idSet[norm] = struct{}{}
		}
	}
	// Never wipe all sync items when neither TargetId nor ItemIds is specified.
	if targetID == "" && len(idSet) == 0 {
		httpx.NoContent(w)
		return
	}

	callerID, isAdmin, hasAuth := h.syncCallerIdentity(r)
	if sh := h.getSyncHandler(); sh != nil {
		if err := sh.CancelItemsForCaller(r.Context(), targetID, cleanedItemIDs, callerID, isAdmin || !hasAuth); err != nil {
			if errors.Is(err, syncapi.ErrForbidden) {
				httpx.Error(w, http.StatusForbidden, "access denied")
				return
			}
			httpx.Error(w, http.StatusInternalServerError, "failed to persist sync cancellation")
			return
		}
		httpx.NoContent(w)
		return
	}

	var st sharedSyncState
	if h.readConfigJSON(r.Context(), syncConfigKey, &st) {
		remaining := make([]sharedSyncJobItem, 0, len(st.JobItems))
		removedCount := 0
		deniedCount := 0
		var removedCachePaths []string
		for _, it := range st.JobItems {
			targetMatch := targetID == "" || it.TargetId == targetID
			_, itemMatch := idSet[guid.Normalize(it.ItemId)]
			if targetMatch && (len(idSet) == 0 || itemMatch) {
				if hasAuth && !isAdmin {
					ownerID := sharedJobOwner(st.Jobs, it.JobId)
					if ownerID == "" || ownerID != callerID {
						deniedCount++
						remaining = append(remaining, it)
						continue
					}
				}
				removedCount++
				if it.OutputPath != "" {
					removedCachePaths = append(removedCachePaths, it.OutputPath)
				}
				continue
			}
			remaining = append(remaining, it)
		}
		if removedCount == 0 && deniedCount > 0 {
			httpx.Error(w, http.StatusForbidden, "access denied")
			return
		}
		if removedCount > 0 {
			st.JobItems = remaining
			if err := h.writeConfigJSON(r.Context(), syncConfigKey, st); err != nil {
				httpx.Error(w, http.StatusInternalServerError, "failed to persist sync cancellation")
				return
			}
			h.cleanupSyncCacheFiles(removedCachePaths...)
		}
	}
	httpx.NoContent(w)
}

func (h *Handler) handleSyncJobItemFile(headOnly bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.canDownloadContent(r) {
			if headOnly {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			httpx.Error(w, http.StatusForbidden, "content downloading is disabled")
			return
		}
		id := chi.URLParam(r, "Id")
		callerID, isAdmin, hasAuth := h.syncCallerIdentity(r)

		var outPath string
		var targetItemID string
		if sh := h.getSyncHandler(); sh != nil {
			item, found, allowed := sh.GetJobItemForCaller(id, callerID, isAdmin || !hasAuth)
			if !found {
				if headOnly {
					w.Header().Set("Content-Type", "application/json; charset=utf-8")
					w.WriteHeader(http.StatusNotFound)
					return
				}
				httpx.Error(w, http.StatusNotFound, "sync job item not found")
				return
			}
			if !allowed {
				if headOnly {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				httpx.Error(w, http.StatusForbidden, "access denied")
				return
			}
			outPath = strings.TrimSpace(item.OutputPath)
			targetItemID = strings.TrimSpace(item.ItemId)
		} else {
			var st sharedSyncState
			if !h.readConfigJSON(r.Context(), syncConfigKey, &st) {
				if headOnly {
					w.Header().Set("Content-Type", "application/json; charset=utf-8")
					w.WriteHeader(http.StatusNotFound)
					return
				}
				httpx.Error(w, http.StatusNotFound, "sync job item not found")
				return
			}

			var found *sharedSyncJobItem
			for i := range st.JobItems {
				if st.JobItems[i].Id == id {
					found = &st.JobItems[i]
					break
				}
			}
			if found == nil {
				if headOnly {
					w.Header().Set("Content-Type", "application/json; charset=utf-8")
					w.WriteHeader(http.StatusNotFound)
					return
				}
				httpx.Error(w, http.StatusNotFound, "sync job item not found")
				return
			}

			if hasAuth && !isAdmin {
				ownerID := sharedJobOwner(st.Jobs, found.JobId)
				if ownerID == "" || ownerID != callerID {
					if headOnly {
						w.WriteHeader(http.StatusForbidden)
						return
					}
					httpx.Error(w, http.StatusForbidden, "access denied")
					return
				}
			}
			outPath = strings.TrimSpace(found.OutputPath)
			targetItemID = strings.TrimSpace(found.ItemId)
		}

		if targetItemID != "" && !h.canAccessItemID(r, targetItemID) {
			if headOnly {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			httpx.Error(w, http.StatusForbidden, "access denied")
			return
		}
		if strings.EqualFold(filepath.Ext(outPath), ".strm") {
			if headOnly {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			httpx.Error(w, http.StatusForbidden, "downloading .strm files is not allowed")
			return
		}
		var itemRec *sqlite.ItemRecord
		var libRepo *sqlite.LibraryRepo
		if targetItemID != "" {
			h.mu.RLock()
			libRepo = h.libRepo
			h.mu.RUnlock()
			if libRepo != nil {
				rec, err := libRepo.GetItemRecordByID(r.Context(), guid.Normalize(targetItemID))
				if err != nil || rec == nil {
					if headOnly {
						w.WriteHeader(http.StatusNotFound)
						return
					}
					httpx.Error(w, http.StatusNotFound, "sync item not found")
					return
				}
				if rec.IsStrm || strings.EqualFold(filepath.Ext(rec.Path), ".strm") {
					if headOnly {
						w.WriteHeader(http.StatusForbidden)
						return
					}
					httpx.Error(w, http.StatusForbidden, "downloading .strm files is not allowed")
					return
				}
				itemRec = rec
			}
		}

		if outPath == "" {
			if headOnly {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			httpx.Error(w, http.StatusNotFound, "sync file not available")
			return
		}
		realOut, err := filepath.EvalSymlinks(outPath)
		if err != nil {
			if os.IsNotExist(err) {
				if headOnly {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				httpx.Error(w, http.StatusNotFound, "sync file not found")
				return
			}
			if headOnly {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			httpx.Error(w, http.StatusForbidden, "sync file path is not accessible")
			return
		}
		realOut = filepath.Clean(realOut)
		if strings.EqualFold(filepath.Ext(realOut), ".strm") {
			if headOnly {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			httpx.Error(w, http.StatusForbidden, "downloading .strm files is not allowed")
			return
		}
		if libRepo != nil && itemRec != nil && !h.isPathWithinSyncCache(realOut) {
			recPath := strings.TrimSpace(itemRec.Path)
			if recPath == "" {
				if headOnly {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				httpx.Error(w, http.StatusForbidden, "sync file path mismatch")
				return
			}
			realRecPath, recErr := filepath.EvalSymlinks(recPath)
			if recErr != nil || filepath.Clean(realRecPath) != realOut {
				if headOnly {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				httpx.Error(w, http.StatusForbidden, "sync file path mismatch")
				return
			}
			if !isPathWithinRegisteredLibraries(r.Context(), libRepo, realOut) {
				if headOnly {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				httpx.Error(w, http.StatusForbidden, "sync file path is outside registered media libraries")
				return
			}
		}

		fi, err := os.Stat(realOut)
		if err != nil || fi.IsDir() || !fi.Mode().IsRegular() {
			if headOnly {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			httpx.Error(w, http.StatusNotFound, "sync file not found")
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "sandbox")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filepath.Base(realOut)}))
		http.ServeFile(w, r, realOut)
	}
}

func isPathWithinRegisteredLibraries(ctx context.Context, libRepo *sqlite.LibraryRepo, realTarget string) bool {
	if libRepo == nil || realTarget == "" {
		return false
	}
	libs, err := libRepo.ListLibraries(ctx)
	if err != nil {
		return false
	}
	cleanTarget := filepath.Clean(realTarget)
	for _, lib := range libs {
		for _, rawRoot := range lib.Paths {
			rawRoot = strings.TrimSpace(rawRoot)
			if rawRoot == "" {
				continue
			}
			realRoot, err := filepath.EvalSymlinks(rawRoot)
			if err != nil {
				realRoot, err = filepath.Abs(rawRoot)
				if err != nil {
					continue
				}
			}
			realRoot = filepath.Clean(realRoot)
			rel, err := filepath.Rel(realRoot, cleanTarget)
			if err != nil {
				continue
			}
			if rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
				return true
			}
		}
	}
	return false
}

func (h *Handler) handleSyncJobItemAdditionalFiles(w http.ResponseWriter, r *http.Request) {
	if !h.canDownloadContent(r) {
		httpx.Error(w, http.StatusForbidden, "content downloading is disabled")
		return
	}
	id := chi.URLParam(r, "Id")
	callerID, isAdmin, hasAuth := h.syncCallerIdentity(r)
	var targetItemID string

	if sh := h.getSyncHandler(); sh != nil {
		item, found, allowed := sh.GetJobItemForCaller(id, callerID, isAdmin || !hasAuth)
		if !found {
			httpx.Error(w, http.StatusNotFound, "sync job item not found")
			return
		}
		if !allowed {
			httpx.Error(w, http.StatusForbidden, "access denied")
			return
		}
		targetItemID = strings.TrimSpace(item.ItemId)
	} else {
		var st sharedSyncState
		if !h.readConfigJSON(r.Context(), syncConfigKey, &st) {
			httpx.Error(w, http.StatusNotFound, "sync job item not found")
			return
		}
		var found *sharedSyncJobItem
		for i := range st.JobItems {
			if st.JobItems[i].Id == id {
				found = &st.JobItems[i]
				break
			}
		}
		if found == nil {
			httpx.Error(w, http.StatusNotFound, "sync job item not found")
			return
		}
		if hasAuth && !isAdmin {
			ownerID := sharedJobOwner(st.Jobs, found.JobId)
			if ownerID == "" || ownerID != callerID {
				httpx.Error(w, http.StatusForbidden, "access denied")
				return
			}
		}
		targetItemID = strings.TrimSpace(found.ItemId)
	}

	if targetItemID != "" && !h.canAccessItemID(r, targetItemID) {
		httpx.Error(w, http.StatusForbidden, "access denied")
		return
	}
	var itemRec *sqlite.ItemRecord
	var libRepo *sqlite.LibraryRepo
	if targetItemID != "" {
		h.mu.RLock()
		libRepo = h.libRepo
		h.mu.RUnlock()
		if libRepo != nil {
			rec, err := libRepo.GetItemRecordByID(r.Context(), guid.Normalize(targetItemID))
			if err != nil || rec == nil {
				httpx.Error(w, http.StatusNotFound, "sync item not found")
				return
			}
			itemRec = rec
		}
	}

	q := httpx.ParseQuery(r)
	rawName := strings.TrimSpace(q.Get("Name"))
	lowerName := strings.ToLower(rawName)
	wantVTT := strings.HasSuffix(lowerName, ".vtt")

	if libRepo != nil && itemRec != nil {
		if body, ct, ok := h.resolveSyncAdditionalSubtitle(r.Context(), libRepo, itemRec, rawName, q.Get("StreamIndex"), q.Get("Index")); ok {
			w.Header().Set("Content-Type", ct)
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Content-Security-Policy", "sandbox")
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(body)
			return
		}
	}

	if wantVTT {
		w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("WEBVTT\n\n"))
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusOK)
}

var srtTimestampLineRe = regexp.MustCompile(`(\d{2}:\d{2}:\d{2}),(\d{3})\s*-->\s*(\d{2}:\d{2}:\d{2}),(\d{3})`)

func convertSRTBytesToWebVTT(raw []byte) []byte {
	text := strings.TrimPrefix(string(raw), "\uFEFF")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if strings.HasPrefix(strings.TrimSpace(text), "WEBVTT") {
		return []byte(text)
	}
	converted := srtTimestampLineRe.ReplaceAllString(text, "$1.$2 --> $3.$4")
	return []byte("WEBVTT\n\n" + converted)
}

func subtitleContentTypeForExt(ext string) string {
	switch strings.ToLower(strings.TrimPrefix(ext, ".")) {
	case "vtt", "webvtt":
		return "text/vtt; charset=utf-8"
	case "srt", "subrip":
		return "application/x-subrip; charset=utf-8"
	case "ass", "ssa":
		return "text/x-ssa; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}

func (h *Handler) syncCacheDirPath() string {
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

func (h *Handler) isPathWithinSyncCache(realTarget string) bool {
	cacheDir := h.syncCacheDirPath()
	if cacheDir == "" || realTarget == "" {
		return false
	}
	realCache, err := filepath.EvalSymlinks(cacheDir)
	if err != nil {
		return false
	}
	realCache = filepath.Clean(realCache)
	cleanTarget := filepath.Clean(realTarget)
	rel, err := filepath.Rel(realCache, cleanTarget)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func (h *Handler) isPathWithinSubtitleCache(realTarget string) bool {
	if h.cfg == nil || realTarget == "" {
		return false
	}
	dataDir := strings.TrimSpace(h.cfg.Snapshot().DataDir)
	if dataDir == "" {
		dataDir = strings.TrimSpace(h.cfg.DataDir)
	}
	if dataDir == "" {
		return false
	}
	subDir := filepath.Join(dataDir, "cache", "subtitles")
	realSub, err := filepath.EvalSymlinks(subDir)
	if err != nil {
		return false
	}
	realSub = filepath.Clean(realSub)
	rel, err := filepath.Rel(realSub, filepath.Clean(realTarget))
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func (h *Handler) cleanupSyncCacheFiles(paths ...string) {
	for _, raw := range paths {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		realTarget, err := filepath.EvalSymlinks(raw)
		if err != nil {
			continue
		}
		if h.isPathWithinSyncCache(realTarget) {
			_ = os.Remove(realTarget)
			_ = os.Remove(realTarget + ".tmp")
		}
	}
}

func (h *Handler) cleanupSyncJobCacheDir(jobID string) {
	cacheDir := h.syncCacheDirPath()
	jobID = strings.TrimSpace(jobID)
	if cacheDir == "" || jobID == "" || strings.ContainsAny(jobID, `/\`) || jobID == "." || jobID == ".." {
		return
	}
	jobDir := filepath.Join(cacheDir, jobID)
	realJobDir, err := filepath.EvalSymlinks(jobDir)
	if err != nil {
		return
	}
	if h.isPathWithinSyncCache(realJobDir) {
		_ = os.RemoveAll(realJobDir)
	}
}

func (h *Handler) resolveSyncAdditionalSubtitle(
	ctx context.Context,
	libRepo *sqlite.LibraryRepo,
	itemRec *sqlite.ItemRecord,
	reqName string,
	rawStreamIdx string,
	rawIdx string,
) ([]byte, string, bool) {
	const maxSubtitleBytes = 10 << 20

	lowerReq := strings.ToLower(strings.TrimSpace(reqName))
	reqExt := strings.ToLower(strings.TrimPrefix(filepath.Ext(lowerReq), "."))
	// Only intercept subtitle requests or explicit stream index requests.
	if rawStreamIdx == "" && rawIdx == "" {
		switch reqExt {
		case "vtt", "srt", "ass", "ssa", "sub":
		default:
			return nil, "", false
		}
	}

	var wantIdx *int
	for _, raw := range []string{rawStreamIdx, rawIdx} {
		if s := strings.TrimSpace(raw); s != "" {
			if n, err := strconv.Atoi(s); err == nil && n >= 0 {
				wantIdx = &n
				break
			}
		}
	}

	sources, _ := libRepo.GetMediaSourcesByItemID(ctx, itemRec.ID)
	var subStreams []sqlite.MediaStreamRecord
	var mediaPath string
	for _, src := range sources {
		if mediaPath == "" && strings.TrimSpace(src.Path) != "" {
			mediaPath = src.Path
		}
		for _, st := range src.Streams {
			if strings.EqualFold(st.StreamType, "Subtitle") {
				subStreams = append(subStreams, st)
			}
		}
	}
	if mediaPath == "" {
		mediaPath = itemRec.Path
	}

	var chosen *sqlite.MediaStreamRecord
	if wantIdx != nil {
		for i := range subStreams {
			if subStreams[i].StreamIndex == *wantIdx {
				chosen = &subStreams[i]
				break
			}
		}
	}
	if chosen == nil && lowerReq != "" {
		reqBase := strings.TrimSuffix(filepath.Base(lowerReq), filepath.Ext(lowerReq))
		for i := range subStreams {
			stBase := strings.ToLower(filepath.Base(subStreams[i].Path))
			if stBase != "" && (stBase == filepath.Base(lowerReq) || strings.TrimSuffix(stBase, filepath.Ext(stBase)) == reqBase) {
				chosen = &subStreams[i]
				break
			}
			if lang := strings.ToLower(strings.TrimSpace(subStreams[i].Language)); lang != "" && strings.Contains(reqBase, lang) {
				chosen = &subStreams[i]
				break
			}
		}
	}
	if chosen == nil && len(subStreams) > 0 {
		chosen = &subStreams[0]
	}
	if chosen == nil {
		return nil, "", false
	}

	wantVTT := reqExt == "vtt"
	if chosen.IsExternal && strings.TrimSpace(chosen.Path) != "" {
		realSub, err := filepath.EvalSymlinks(chosen.Path)
		if err != nil {
			return nil, "", false
		}
		realSub = filepath.Clean(realSub)
		if !isPathWithinRegisteredLibraries(ctx, libRepo, realSub) && !h.isPathWithinSubtitleCache(realSub) {
			return nil, "", false
		}
		f, err := os.Open(realSub)
		if err != nil {
			return nil, "", false
		}
		data, err := io.ReadAll(io.LimitReader(f, maxSubtitleBytes))
		_ = f.Close()
		if err != nil {
			return nil, "", false
		}
		srcExt := strings.ToLower(strings.TrimPrefix(filepath.Ext(realSub), "."))
		if wantVTT && srcExt != "vtt" {
			return convertSRTBytesToWebVTT(data), "text/vtt; charset=utf-8", true
		}
		outExt := reqExt
		if outExt == "" {
			outExt = srcExt
		}
		return data, subtitleContentTypeForExt(outExt), true
	}

	// Embedded subtitle extraction via transcode.Extractor.
	if trimmedMedia := strings.TrimSpace(mediaPath); trimmedMedia != "" && h.cfg != nil {
		lowerMedia := strings.ToLower(trimmedMedia)
		if strings.EqualFold(filepath.Ext(trimmedMedia), ".strm") ||
			strings.HasPrefix(lowerMedia, "http://") ||
			strings.HasPrefix(lowerMedia, "https://") {
			return nil, "", false
		}
		realMedia, err := filepath.EvalSymlinks(trimmedMedia)
		if err != nil {
			return nil, "", false
		}
		realMedia = filepath.Clean(realMedia)
		if !isPathWithinRegisteredLibraries(ctx, libRepo, realMedia) {
			return nil, "", false
		}
		format := "webvtt"
		ct := "text/vtt; charset=utf-8"
		if reqExt == "srt" {
			format = "srt"
			ct = "application/x-subrip; charset=utf-8"
		} else if reqExt == "ass" || reqExt == "ssa" {
			format = "ass"
			ct = "text/x-ssa; charset=utf-8"
		}
		mediaSourceID := itemRec.ID
		if len(sources) > 0 && strings.TrimSpace(sources[0].ID) != "" {
			mediaSourceID = strings.TrimSpace(sources[0].ID)
		}
		ext := transcode.NewExtractor(h.cfg)
		if outFile, err := ext.ExtractSubtitle(ctx, realMedia, itemRec.ID, mediaSourceID, chosen.StreamIndex, format); err == nil && strings.TrimSpace(outFile) != "" {
			if f, oErr := os.Open(outFile); oErr == nil {
				data, rErr := io.ReadAll(io.LimitReader(f, maxSubtitleBytes))
				_ = f.Close()
				if rErr == nil && len(data) > 0 {
					return data, ct, true
				}
			}
		}
	}
	return nil, "", false
}

func (h *Handler) handleSyncOfflineActions(w http.ResponseWriter, r *http.Request) {
	if r.Body != nil {
		var raw json.RawMessage
		if err := httpx.DecodeJSONLimit(r, &raw, 0); err == nil && len(raw) > 0 {
			now := time.Now().UTC().Unix()
			var actions []OfflineAction
			if err := json.Unmarshal(raw, &actions); err != nil {
				var single OfflineAction
				if err2 := json.Unmarshal(raw, &single); err2 == nil && (single.ItemId != "" || single.Type != "") {
					actions = []OfflineAction{single}
				}
			}
			if len(actions) > maxOfflineActions {
				httpx.Error(w, http.StatusBadRequest, "too many offline actions in a single request")
				return
			}
			if len(actions) > 0 {
				allowedActions := make([]OfflineAction, 0, len(actions))
				for i := range actions {
					actions[i].RecordedAtUnix = now
					itemID := strings.TrimSpace(actions[i].ItemId)
					if itemID != "" && !h.canAccessItemID(r, itemID) {
						continue
					}
					allowedActions = append(allowedActions, actions[i])
				}
				if len(allowedActions) == 0 {
					httpx.NoContent(w)
					return
				}
				h.mu.Lock()
				snapshot := cloneCompatExtState(h.state)
				h.state.OfflineActions = append(h.state.OfflineActions, allowedActions...)
				if len(h.state.OfflineActions) > maxOfflineActions {
					h.state.OfflineActions = append([]OfflineAction(nil), h.state.OfflineActions[len(h.state.OfflineActions)-maxOfflineActions:]...)
				}
				saveErr := h.saveStateLocked(r.Context())
				if saveErr != nil {
					h.state = snapshot
				}
				libRepo := h.libRepo
				userRepo := h.userRepo
				h.mu.Unlock()
				if saveErr != nil {
					httpx.Error(w, http.StatusInternalServerError, "failed to persist offline actions")
					return
				}

				if libRepo != nil {
					callerID, isAdmin := h.callerIdentity(r)
					if userRepo == nil && h.store != nil {
						userRepo = sqlite.NewUserRepo(h.store)
					}
					for _, act := range allowedActions {
						itemID := strings.TrimSpace(act.ItemId)
						if itemID == "" {
							continue
						}
						uid := callerID
						if uid == "" || (isAdmin && strings.TrimSpace(act.UserId) != "") {
							if normActUser := guid.Normalize(act.UserId); normActUser != "" {
								uid = normActUser
							}
						}
						if uid == "" {
							continue
						}

						markedPlayed := false
						if act.Played != nil {
							if *act.Played {
								_, _ = libRepo.MarkPlayed(r.Context(), uid, itemID)
								markedPlayed = true
							} else {
								_, _ = libRepo.MarkUnplayed(r.Context(), uid, itemID)
							}
						} else if strings.EqualFold(act.Type, "Played") {
							_, _ = libRepo.MarkPlayed(r.Context(), uid, itemID)
							markedPlayed = true
						} else if strings.EqualFold(act.Type, "Unplayed") {
							_, _ = libRepo.MarkUnplayed(r.Context(), uid, itemID)
						}

						pos := act.PlaybackPositionTicks
						if pos <= 0 {
							pos = act.PositionTicks
						}
						if pos > 0 && !markedPlayed && userRepo != nil {
							_, _ = userRepo.UpsertPlaybackProgress(r.Context(), uid, itemID, pos, false, nil, nil)
						}

						if act.IsFavorite != nil {
							if *act.IsFavorite {
								_, _ = libRepo.MarkFavorite(r.Context(), uid, itemID)
							} else {
								_, _ = libRepo.UnmarkFavorite(r.Context(), uid, itemID)
							}
						} else if strings.EqualFold(act.Type, "Favorite") {
							_, _ = libRepo.MarkFavorite(r.Context(), uid, itemID)
						} else if strings.EqualFold(act.Type, "Unfavorite") {
							_, _ = libRepo.UnmarkFavorite(r.Context(), uid, itemID)
						}
					}
				}
			}
		}
	}
	httpx.NoContent(w)
}

func isSensitiveNotificationOptionKey(k string) bool {
	lk := strings.ToLower(strings.TrimSpace(k))
	if strings.Contains(lk, "key") ||
		strings.Contains(lk, "secret") ||
		strings.Contains(lk, "token") ||
		strings.Contains(lk, "password") ||
		strings.Contains(lk, "credential") ||
		strings.Contains(lk, "authorization") ||
		strings.Contains(lk, "cookie") ||
		strings.Contains(lk, "webhook") {
		return true
	}
	return lk == "pin" || strings.HasSuffix(lk, "_pin") || strings.HasSuffix(lk, "-pin")
}

func maskNotificationServiceConfig(svc NotificationServiceConfig) NotificationServiceConfig {
	cp := svc
	if svc.Events != nil {
		cp.Events = append([]string(nil), svc.Events...)
	}
	if svc.Options != nil {
		masked := make(map[string]string, len(svc.Options))
		for k, v := range svc.Options {
			if isSensitiveNotificationOptionKey(k) && strings.TrimSpace(v) != "" {
				masked[k] = maskedOptionValue
			} else {
				masked[k] = v
			}
		}
		cp.Options = masked
	}
	return cp
}

func mergeNotificationOptions(prev, incoming map[string]string) map[string]string {
	if incoming == nil {
		return nil
	}
	out := make(map[string]string, len(incoming))
	for k, v := range incoming {
		if isSensitiveNotificationOptionKey(k) && strings.TrimSpace(v) == maskedOptionValue {
			if prev != nil {
				if oldVal := prev[k]; strings.TrimSpace(oldVal) != "" {
					out[k] = oldVal
				}
			}
			continue
		}
		out[k] = v
	}
	return out
}

func (h *Handler) handleGetConfiguredNotifications(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	userID := guid.Normalize(q.Get("UserId"))
	callerID, isAdmin := h.callerIdentity(r)

	if !isAdmin && callerID != "" {
		if userID != "" && userID != callerID {
			httpx.Error(w, http.StatusForbidden, "access denied")
			return
		}
		userID = callerID
	}

	h.mu.RLock()
	out := make([]NotificationServiceConfig, 0, len(h.state.NotificationServices))
	for _, svc := range h.state.NotificationServices {
		if userID != "" && guid.Normalize(svc.UserId) != userID {
			continue
		}
		out = append(out, maskNotificationServiceConfig(svc))
	}
	h.mu.RUnlock()

	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) handleSaveConfiguredNotification(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength > maxNotificationBodyBytes {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "notification service config too large")
		return
	}
	var svc NotificationServiceConfig
	if r.Body != nil {
		if err := httpx.DecodeJSONLimit(r, &svc, maxNotificationBodyBytes); err != nil {
			httpx.Error(w, http.StatusBadRequest, "invalid notification service config")
			return
		}
	}

	callerID, isAdmin := h.callerIdentity(r)
	if !isAdmin && callerID != "" {
		if strings.TrimSpace(svc.UserId) != "" && guid.Normalize(svc.UserId) != callerID {
			httpx.Error(w, http.StatusForbidden, "access denied")
			return
		}
		svc.UserId = callerID
	} else if strings.TrimSpace(svc.UserId) == "" && callerID != "" {
		svc.UserId = callerID
	}

	if strings.TrimSpace(svc.Id) == "" {
		svc.Id = guid.New()
	}
	if strings.TrimSpace(svc.Name) == "" {
		svc.Name = "Notification Service"
	}

	h.mu.Lock()
	snapshot := cloneCompatExtState(h.state)
	updated := false
	for i := range h.state.NotificationServices {
		if h.state.NotificationServices[i].Id == svc.Id {
			if !isAdmin && callerID != "" && guid.Normalize(h.state.NotificationServices[i].UserId) != callerID {
				h.mu.Unlock()
				httpx.Error(w, http.StatusForbidden, "access denied")
				return
			}
			svc.Options = mergeNotificationOptions(h.state.NotificationServices[i].Options, svc.Options)
			h.state.NotificationServices[i] = svc
			updated = true
			break
		}
	}
	if !updated {
		if len(h.state.NotificationServices) >= maxNotificationServices {
			h.mu.Unlock()
			httpx.Error(w, http.StatusBadRequest, "notification services limit reached")
			return
		}
		if ownerID := guid.Normalize(svc.UserId); ownerID != "" && !isAdmin {
			userCount := 0
			for _, existing := range h.state.NotificationServices {
				if guid.Normalize(existing.UserId) == ownerID {
					userCount++
				}
			}
			if userCount >= maxNotificationServicesPerUser {
				h.mu.Unlock()
				httpx.Error(w, http.StatusTooManyRequests, "per-user notification services limit reached")
				return
			}
		}
		svc.Options = mergeNotificationOptions(nil, svc.Options)
		h.state.NotificationServices = append(h.state.NotificationServices, svc)
	}
	saveErr := h.saveStateLocked(r.Context())
	if saveErr != nil {
		h.state = snapshot
	}
	h.mu.Unlock()
	if saveErr != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to persist notification service")
		return
	}

	httpx.JSON(w, http.StatusOK, maskNotificationServiceConfig(svc))
}

func (h *Handler) handleNotificationServiceDefaults(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, NotificationServiceConfig{
		Id:           "default",
		Name:         "Ambra Internal Notifications",
		FriendlyName: "Ambra Internal Notifications",
		ServiceType:  "ambra-internal",
		Enabled:      true,
		Events:       []string{"LibraryChanged", "PlaybackStart", "PlaybackStop", "TaskFailed"},
		Options:      map[string]string{},
	})
}

func (h *Handler) handleTestNotificationService(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	targetID := strings.TrimSpace(q.Get("Id"))
	targetUserID := strings.TrimSpace(q.Get("UserId"))
	if r.Body != nil {
		var body struct {
			Id     string `json:"Id"`
			UserId string `json:"UserId"`
		}
		if err := httpx.DecodeJSONLimit(r, &body, maxNotificationBodyBytes); err == nil {
			if targetID == "" {
				targetID = strings.TrimSpace(body.Id)
			}
			if targetUserID == "" {
				targetUserID = strings.TrimSpace(body.UserId)
			}
		}
	}

	callerID, isAdmin := h.callerIdentity(r)
	h.mu.Lock()
	if !isAdmin && callerID != "" {
		if targetUserID != "" && guid.Normalize(targetUserID) != callerID {
			h.mu.Unlock()
			httpx.Error(w, http.StatusForbidden, "access denied")
			return
		}
		if targetID != "" {
			found := false
			for _, svc := range h.state.NotificationServices {
				if svc.Id == targetID {
					found = true
					if guid.Normalize(svc.UserId) != callerID {
						h.mu.Unlock()
						httpx.Error(w, http.StatusForbidden, "access denied")
						return
					}
					break
				}
			}
			if !found {
				h.mu.Unlock()
				httpx.Error(w, http.StatusNotFound, "notification service not found")
				return
			}
		}
	}
	// Record last test timestamp in memory without unconditional SQLite write on every test call.
	h.state.LastTestNotifUnix = time.Now().UTC().Unix()
	tester := h.notifTester
	h.mu.Unlock()

	if tester != nil {
		if err := tester(r.Context(), "Ambra Test Notification", "This is a test notification from Ambra."); err != nil {
			slog.Error("notification test delivery failed", "service_id", targetID, "error", err)
			httpx.Error(w, http.StatusBadGateway, "notification test delivery failed")
			return
		}
	}
	httpx.NoContent(w)
}

func (h *Handler) handleDeleteConfiguredNotification(w http.ResponseWriter, r *http.Request) {
	id := extractIDParam(r)
	if id != "" {
		callerID, isAdmin := h.callerIdentity(r)
		h.mu.Lock()
		snapshot := cloneCompatExtState(h.state)
		filtered := make([]NotificationServiceConfig, 0, len(h.state.NotificationServices))
		for _, svc := range h.state.NotificationServices {
			if svc.Id == id {
				if !isAdmin && callerID != "" && guid.Normalize(svc.UserId) != callerID {
					h.mu.Unlock()
					httpx.Error(w, http.StatusForbidden, "access denied")
					return
				}
				continue
			}
			filtered = append(filtered, svc)
		}
		h.state.NotificationServices = filtered
		saveErr := h.saveStateLocked(r.Context())
		if saveErr != nil {
			h.state = snapshot
		}
		h.mu.Unlock()
		if saveErr != nil {
			httpx.Error(w, http.StatusInternalServerError, "failed to persist notification service deletion")
			return
		}
	}
	httpx.NoContent(w)
}
