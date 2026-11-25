package compatext

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
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

func (h *Handler) handleDeleteSyncJobPost(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "Id")
	var st sharedSyncState
	if h.readConfigJSON(r.Context(), syncConfigKey, &st) {
		jobs := make([]sharedSyncJob, 0, len(st.Jobs))
		for _, j := range st.Jobs {
			if j.Id != id {
				jobs = append(jobs, j)
			}
		}
		items := make([]sharedSyncJobItem, 0, len(st.JobItems))
		for _, it := range st.JobItems {
			if it.JobId != id {
				items = append(items, it)
			}
		}
		st.Jobs = jobs
		st.JobItems = items
		h.writeConfigJSON(r.Context(), syncConfigKey, st)
	}
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
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
			targetID = body.TargetId
		}
	}

	var st sharedSyncState
	_ = h.readConfigJSON(r.Context(), syncConfigKey, &st)
	for _, it := range st.JobItems {
		if it.ItemId == itemID && (targetID == "" || it.TargetId == targetID) {
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

		h.mu.Lock()
		h.ensureMapsLocked()
		if mark {
			h.state.RemovedJobItems[id] = true
		} else {
			delete(h.state.RemovedJobItems, id)
		}
		h.saveStateLocked(r.Context())
		h.mu.Unlock()

		var st sharedSyncState
		if h.readConfigJSON(r.Context(), syncConfigKey, &st) {
			changed := false
			for i := range st.JobItems {
				if st.JobItems[i].Id == id {
					if mark {
						st.JobItems[i].Status = "Removing"
					} else {
						st.JobItems[i].Status = "Synced"
					}
					changed = true
				}
			}
			if changed {
				h.writeConfigJSON(r.Context(), syncConfigKey, st)
			}
		}

		httpx.NoContent(w)
	}
}

func (h *Handler) handleDeleteSyncJobItemPost(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "Id")
	var st sharedSyncState
	if h.readConfigJSON(r.Context(), syncConfigKey, &st) {
		items := make([]sharedSyncJobItem, 0, len(st.JobItems))
		for _, it := range st.JobItems {
			if it.Id != id {
				items = append(items, it)
			}
		}
		st.JobItems = items
		h.writeConfigJSON(r.Context(), syncConfigKey, st)
	}
	httpx.NoContent(w)
}

func (h *Handler) handleCancelSyncItems(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	targetID := chi.URLParam(r, "TargetId")
	if targetID == "" {
		targetID = q.Get("TargetId")
	}
	itemIDs := q.List("ItemIds")
	if len(itemIDs) == 0 && r.Body != nil {
		var body struct {
			TargetId string   `json:"TargetId"`
			ItemIds  []string `json:"ItemIds"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
			if targetID == "" {
				targetID = body.TargetId
			}
			itemIDs = body.ItemIds
		}
	}

	idSet := make(map[string]struct{}, len(itemIDs))
	for _, id := range itemIDs {
		idSet[id] = struct{}{}
	}

	var st sharedSyncState
	if h.readConfigJSON(r.Context(), syncConfigKey, &st) {
		remaining := make([]sharedSyncJobItem, 0, len(st.JobItems))
		for _, it := range st.JobItems {
			targetMatch := targetID == "" || it.TargetId == targetID
			_, itemMatch := idSet[it.ItemId]
			if targetMatch && (len(idSet) == 0 || itemMatch) {
				continue
			}
			remaining = append(remaining, it)
		}
		st.JobItems = remaining
		h.writeConfigJSON(r.Context(), syncConfigKey, st)
	}
	httpx.NoContent(w)
}

func (h *Handler) handleSyncJobItemFile(headOnly bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "Id")
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

		callerID, isAdmin := h.callerIdentity(r)
		if !isAdmin && callerID != "" {
			var ownerID string
			for _, j := range st.Jobs {
				if j.Id == found.JobId {
					ownerID = guid.Normalize(j.UserId)
					break
				}
			}
			if ownerID == "" || ownerID != callerID {
				if headOnly {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				httpx.Error(w, http.StatusForbidden, "access denied")
				return
			}
		}

		outPath := strings.TrimSpace(found.OutputPath)
		if outPath == "" {
			if headOnly {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			httpx.Error(w, http.StatusNotFound, "sync file not available")
			return
		}
		fi, err := os.Stat(outPath)
		if err != nil || fi.IsDir() {
			if headOnly {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			httpx.Error(w, http.StatusNotFound, "sync file not found")
			return
		}
		http.ServeFile(w, r, outPath)
	}
}

func (h *Handler) handleSyncJobItemAdditionalFiles(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	name := strings.ToLower(q.Get("Name"))
	if strings.HasSuffix(name, ".vtt") {
		w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("WEBVTT\n\n"))
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) handleSyncOfflineActions(w http.ResponseWriter, r *http.Request) {
	if r.Body != nil {
		var raw json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&raw); err == nil && len(raw) > 0 {
			now := time.Now().UTC().Unix()
			var actions []OfflineAction
			if err := json.Unmarshal(raw, &actions); err != nil {
				var single OfflineAction
				if err2 := json.Unmarshal(raw, &single); err2 == nil && (single.ItemId != "" || single.Type != "") {
					actions = []OfflineAction{single}
				}
			}
			if len(actions) > 0 {
				for i := range actions {
					actions[i].RecordedAtUnix = now
				}
				h.mu.Lock()
				h.state.OfflineActions = append(h.state.OfflineActions, actions...)
				h.saveStateLocked(r.Context())
				libRepo := h.libRepo
				userRepo := h.userRepo
				h.mu.Unlock()

				if libRepo != nil {
					callerID, isAdmin := h.callerIdentity(r)
					if userRepo == nil && h.store != nil {
						userRepo = sqlite.NewUserRepo(h.store)
					}
					for _, act := range actions {
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
		out = append(out, svc)
	}
	h.mu.RUnlock()

	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) handleSaveConfiguredNotification(w http.ResponseWriter, r *http.Request) {
	var svc NotificationServiceConfig
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&svc); err != nil {
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
	updated := false
	for i := range h.state.NotificationServices {
		if h.state.NotificationServices[i].Id == svc.Id {
			if !isAdmin && callerID != "" && guid.Normalize(h.state.NotificationServices[i].UserId) != callerID {
				h.mu.Unlock()
				httpx.Error(w, http.StatusForbidden, "access denied")
				return
			}
			h.state.NotificationServices[i] = svc
			updated = true
			break
		}
	}
	if !updated {
		h.state.NotificationServices = append(h.state.NotificationServices, svc)
	}
	h.saveStateLocked(r.Context())
	h.mu.Unlock()

	httpx.JSON(w, http.StatusOK, svc)
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
	h.mu.Lock()
	h.state.LastTestNotifUnix = time.Now().UTC().Unix()
	h.saveStateLocked(r.Context())
	h.mu.Unlock()
	httpx.NoContent(w)
}

func (h *Handler) handleDeleteConfiguredNotification(w http.ResponseWriter, r *http.Request) {
	id := extractIDParam(r)
	if id != "" {
		callerID, isAdmin := h.callerIdentity(r)
		h.mu.Lock()
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
		h.saveStateLocked(r.Context())
		h.mu.Unlock()
	}
	httpx.NoContent(w)
}
