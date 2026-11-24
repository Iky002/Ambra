package notifications

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

const stateConfigKey = "notifications:state"

// AuthFuncs decouples route registration from the concrete middleware package.
type AuthFuncs struct {
	RequireAuth   func(http.Handler) http.Handler
	RequireAdmin  func(http.Handler) http.Handler
	UserIDFromCtx func(context.Context) string
}

type NotificationTypeInfo struct {
	Type        string `json:"Type"`
	Name        string `json:"Name"`
	Enabled     bool   `json:"Enabled"`
	Category    string `json:"Category"`
	IsBasedOn   bool   `json:"IsBasedOnUserEvent"`
}

type NameIdPair struct {
	Name string `json:"Name"`
	Id   string `json:"Id"`
}

type Notification struct {
	Id          string    `json:"Id"`
	UserId      string    `json:"UserId,omitempty"`
	Date        time.Time `json:"Date"`
	IsRead      bool      `json:"IsRead"`
	Name        string    `json:"Name"`
	Description string    `json:"Description,omitempty"`
	Url         string    `json:"Url,omitempty"`
	ImageUrl    string    `json:"ImageUrl,omitempty"`
	Level       string    `json:"Level"`
}

type NotificationResult struct {
	Notifications    []Notification `json:"Notifications"`
	TotalRecordCount int            `json:"TotalRecordCount"`
}

type NotificationsSummary struct {
	UnreadCount                int    `json:"UnreadCount"`
	MaxUnreadNotificationLevel string `json:"MaxUnreadNotificationLevel,omitempty"`
}

type notificationsState struct {
	Items []Notification `json:"items"`
}

type Handler struct {
	store *sqlite.Store
	auth  AuthFuncs

	mu    sync.RWMutex
	state notificationsState
}

func New(store *sqlite.Store) *Handler {
	h := &Handler{store: store}
	h.loadState(context.Background())
	return h
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth AuthFuncs) {
	h.auth = auth
	wrapAuth := auth.RequireAuth
	if wrapAuth == nil {
		wrapAuth = passthrough
	}
	wrapAdmin := auth.RequireAdmin
	if wrapAdmin == nil {
		wrapAdmin = wrapAuth
	}

	handleAuth := func(pattern string, fn http.HandlerFunc) {
		mux.Handle(pattern, wrapAuth(fn))
	}
	handleAdmin := func(pattern string, fn http.HandlerFunc) {
		mux.Handle(pattern, wrapAdmin(fn))
	}

	handleAuth("GET /Notifications/Types", h.handleTypes)
	handleAuth("GET /Notifications/Services", h.handleServices)
	handleAuth("GET /Notifications/{UserId}", h.handleListByUser)
	handleAuth("GET /Notifications/{UserId}/Summary", h.handleSummary)
	handleAuth("POST /Notifications/{UserId}/Read", h.handleMarkRead(true))
	handleAuth("POST /Notifications/{UserId}/Unread", h.handleMarkRead(false))
	handleAdmin("POST /Notifications/Admin", h.handleSendAdmin)
}

func (h *Handler) handleTypes(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, []NotificationTypeInfo{
		{Type: "SystemUpdateAvailable", Name: "System Update Available", Enabled: true, Category: "System"},
		{Type: "LibraryChanged", Name: "New Media Added", Enabled: true, Category: "Library"},
		{Type: "PlaybackStart", Name: "Playback Started", Enabled: true, Category: "Playback", IsBasedOn: true},
		{Type: "PlaybackStop", Name: "Playback Stopped", Enabled: true, Category: "Playback", IsBasedOn: true},
		{Type: "TaskFailed", Name: "Scheduled Task Failed", Enabled: true, Category: "System"},
		{Type: "AdminNotification", Name: "Admin Notification", Enabled: true, Category: "Admin"},
	})
}

func (h *Handler) handleServices(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, []NameIdPair{
		{Name: "Ambra Internal Notifications", Id: "ambra-internal"},
		{Name: "Webhook Notifications", Id: "ambra-webhook"},
	})
}

func (h *Handler) handleListByUser(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	userID := guid.Normalize(r.PathValue("UserId"))

	h.mu.RLock()
	matched := make([]Notification, 0, len(h.state.Items))
	for _, n := range h.state.Items {
		if n.UserId == "" || guid.Normalize(n.UserId) == userID {
			matched = append(matched, n)
		}
	}
	h.mu.RUnlock()

	page, total := paginate(matched, q.Int("StartIndex", 0), q.Int("Limit", 0))
	httpx.JSON(w, http.StatusOK, NotificationResult{
		Notifications:    page,
		TotalRecordCount: total,
	})
}

func (h *Handler) handleSummary(w http.ResponseWriter, r *http.Request) {
	userID := guid.Normalize(r.PathValue("UserId"))

	h.mu.RLock()
	unread := 0
	maxRank := 0
	maxLevel := ""
	for _, n := range h.state.Items {
		if n.UserId != "" && guid.Normalize(n.UserId) != userID {
			continue
		}
		if !n.IsRead {
			unread++
			rank := levelRank(n.Level)
			if rank > maxRank {
				maxRank = rank
				maxLevel = n.Level
			}
		}
	}
	h.mu.RUnlock()

	httpx.JSON(w, http.StatusOK, NotificationsSummary{
		UnreadCount:                unread,
		MaxUnreadNotificationLevel: maxLevel,
	})
}

func (h *Handler) handleMarkRead(read bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := httpx.ParseQuery(r)
		userID := guid.Normalize(r.PathValue("UserId"))
		ids := q.List("Ids")
		if len(ids) == 0 && r.Body != nil {
			var body struct {
				Ids []string `json:"Ids"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
				ids = body.Ids
			}
		}

		idSet := make(map[string]struct{}, len(ids))
		for _, id := range ids {
			idSet[id] = struct{}{}
		}

		h.mu.Lock()
		for i := range h.state.Items {
			n := &h.state.Items[i]
			if n.UserId != "" && guid.Normalize(n.UserId) != userID {
				continue
			}
			if len(idSet) > 0 {
				if _, ok := idSet[n.Id]; !ok {
					continue
				}
			}
			n.IsRead = read
		}
		h.saveStateLocked(r.Context())
		h.mu.Unlock()
		httpx.NoContent(w)
	}
}

func (h *Handler) handleSendAdmin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"Name"`
		Description string `json:"Description"`
		ImageUrl    string `json:"ImageUrl"`
		Url         string `json:"Url"`
		Level       string `json:"Level"`
		UserId      string `json:"UserId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid notification payload")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		httpx.Error(w, http.StatusBadRequest, "notification name is required")
		return
	}
	level := normalizeLevel(req.Level)
	n := Notification{
		Id:          guid.New(),
		UserId:      guid.Normalize(req.UserId),
		Date:        time.Now().UTC(),
		IsRead:      false,
		Name:        req.Name,
		Description: req.Description,
		Url:         req.Url,
		ImageUrl:    req.ImageUrl,
		Level:       level,
	}

	h.mu.Lock()
	h.state.Items = append([]Notification{n}, h.state.Items...)
	h.saveStateLocked(r.Context())
	h.mu.Unlock()

	httpx.JSON(w, http.StatusOK, n)
}

func normalizeLevel(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "error":
		return "Error"
	case "warning":
		return "Warning"
	default:
		return "Normal"
	}
}

func levelRank(level string) int {
	switch strings.ToLower(level) {
	case "error":
		return 3
	case "warning":
		return 2
	default:
		return 1
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
	var st notificationsState
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
