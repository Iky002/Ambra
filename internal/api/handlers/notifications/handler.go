package notifications

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/pkg/i18n"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

const (
	stateConfigKey   = "notifications:state"
	maxNotifications = 500
)

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
	ReadBy      []string  `json:"ReadBy,omitempty"`
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

func canAccessUserNotifications(ctx context.Context, targetUserID string) bool {
	ac, ok := middleware.AuthFromContext(ctx)
	if !ok || ac == nil || (ac.User == nil && !ac.IsAPIKey) {
		return true
	}
	if ac.IsAdmin() {
		return true
	}
	return guid.Normalize(ac.UserID()) == guid.Normalize(targetUserID)
}

func containsString(items []string, target string) bool {
	for _, s := range items {
		if s == target {
			return true
		}
	}
	return false
}

func isReadForUser(n Notification, userID string) bool {
	if n.UserId == "" {
		return containsString(n.ReadBy, userID)
	}
	return n.IsRead || containsString(n.ReadBy, userID)
}

func setReadBy(readBy []string, userID string, read bool) []string {
	if userID == "" {
		return readBy
	}
	if read {
		if containsString(readBy, userID) {
			return readBy
		}
		return append(readBy, userID)
	}
	out := make([]string, 0, len(readBy))
	for _, u := range readBy {
		if u != userID {
			out = append(out, u)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (h *Handler) handleListByUser(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	userID := guid.Normalize(r.PathValue("UserId"))
	if !canAccessUserNotifications(r.Context(), userID) {
		httpx.Error(w, http.StatusForbidden, "cannot access another user's notifications")
		return
	}
	isReadFilter := q.Bool("IsRead")
	acceptLang := strings.TrimSpace(r.Header.Get("Accept-Language"))
	locale := ""
	if acceptLang != "" {
		locale = i18n.ResolveLocale(acceptLang, "")
	}

	h.mu.RLock()
	matched := make([]Notification, 0, len(h.state.Items))
	for _, n := range h.state.Items {
		if n.UserId == "" || guid.Normalize(n.UserId) == userID {
			item := n
			item.IsRead = isReadForUser(n, userID)
			if isReadFilter != nil && item.IsRead != *isReadFilter {
				continue
			}
			if len(n.ReadBy) > 0 {
				item.ReadBy = append([]string(nil), n.ReadBy...)
			}
			if locale != "" {
				item = localizeNotification(item, locale)
			}
			matched = append(matched, item)
		}
	}
	h.mu.RUnlock()

	page, total := paginate(matched, q.Int("StartIndex", 0), q.Int("Limit", 0))
	httpx.JSON(w, http.StatusOK, NotificationResult{
		Notifications:    page,
		TotalRecordCount: total,
	})
}

func localizeNotification(n Notification, locale string) Notification {
	switch n.Name {
	case "Library Updated", "notif.library_updated.name":
		n.Name = i18n.T(locale, "notif.library_updated.name")
		if n.Description == "" || n.Description == "Media library scan completed" || n.Description == "notif.library_updated.desc" {
			n.Description = i18n.T(locale, "notif.library_updated.desc")
		}
	case "Scheduled Task Failed", "Task Failed", "notif.task_failed.name":
		n.Name = i18n.T(locale, "notif.task_failed.name")
		if n.Description == "" || n.Description == "Scheduled Task failed" || n.Description == "notif.task_failed.desc" {
			n.Description = i18n.T(locale, "notif.task_failed.desc")
		}
	default:
		if n.UserId == "" && strings.EqualFold(n.Level, "Error") && strings.HasSuffix(n.Name, " Failed") {
			prefix := strings.TrimSuffix(n.Name, " Failed")
			n.Name = i18n.T(locale, "notif.task_failed.name") + " (" + prefix + ")"
			if n.Description == "" || n.Description == prefix+" failed" || n.Description == "notif.task_failed.desc" {
				n.Description = i18n.T(locale, "notif.task_failed.desc")
			}
		}
	}
	return n
}

func (h *Handler) handleSummary(w http.ResponseWriter, r *http.Request) {
	userID := guid.Normalize(r.PathValue("UserId"))
	if !canAccessUserNotifications(r.Context(), userID) {
		httpx.Error(w, http.StatusForbidden, "cannot access another user's notifications")
		return
	}

	h.mu.RLock()
	unread := 0
	maxRank := 0
	maxLevel := ""
	for _, n := range h.state.Items {
		if n.UserId != "" && guid.Normalize(n.UserId) != userID {
			continue
		}
		if !isReadForUser(n, userID) {
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
		if !canAccessUserNotifications(r.Context(), userID) {
			httpx.Error(w, http.StatusForbidden, "cannot access another user's notifications")
			return
		}
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
			n.ReadBy = setReadBy(n.ReadBy, userID, read)
			if n.UserId != "" {
				n.IsRead = read
			}
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

	h.appendNotification(r.Context(), n)

	httpx.JSON(w, http.StatusOK, n)
}

func (h *Handler) appendNotification(ctx context.Context, n Notification) {
	if n.Id == "" {
		n.Id = guid.New()
	}
	if n.Date.IsZero() {
		n.Date = time.Now().UTC()
	}
	n.Level = normalizeLevel(n.Level)

	h.mu.Lock()
	h.state.Items = append([]Notification{n}, h.state.Items...)
	if len(h.state.Items) > maxNotifications {
		h.state.Items = h.state.Items[:maxNotifications]
	}
	h.saveStateLocked(ctx)
	h.mu.Unlock()
}

// SubscribeEvents subscribes the notifications handler to system eventbus events
// (library changes, scheduled task failures, and user data events).
func (h *Handler) SubscribeEvents(bus *eventbus.Bus) {
	if bus == nil {
		return
	}
	bus.Subscribe(eventbus.EventLibraryChanged, func(ctx context.Context, e eventbus.Event) {
		desc := "Media library scan completed"
		if m, ok := e.Payload.(map[string]any); ok {
			added := toInt(m["ItemsAdded"])
			updated := toInt(m["ItemsUpdated"])
			deleted := toInt(m["ItemsDeleted"])
			if added > 0 || updated > 0 || deleted > 0 {
				desc = fmt.Sprintf("Library updated: %d added, %d updated, %d deleted", added, updated, deleted)
			}
		}
		h.appendNotification(ctx, Notification{
			Name:        "Library Updated",
			Description: desc,
			Level:       "Normal",
		})
	})

	bus.Subscribe(eventbus.EventTaskProgress, func(ctx context.Context, e eventbus.Event) {
		raw, err := json.Marshal(e.Payload)
		if err != nil {
			return
		}
		var task struct {
			Name                string `json:"Name"`
			State               string `json:"State"`
			LastExecutionResult *struct {
				Status       string `json:"Status"`
				ErrorMessage string `json:"ErrorMessage"`
			} `json:"LastExecutionResult"`
		}
		if err := json.Unmarshal(raw, &task); err != nil {
			return
		}
		if task.LastExecutionResult != nil && strings.EqualFold(task.LastExecutionResult.Status, "Failed") {
			name := strings.TrimSpace(task.Name)
			if name == "" {
				name = "Scheduled Task"
			}
			desc := strings.TrimSpace(task.LastExecutionResult.ErrorMessage)
			if desc == "" {
				desc = name + " failed"
			}
			h.appendNotification(ctx, Notification{
				Name:        name + " Failed",
				Description: desc,
				Level:       "Error",
			})
		}
	})

	bus.Subscribe(eventbus.EventUserDataChanged, func(ctx context.Context, e eventbus.Event) {
		m, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		uid, _ := m["UserId"].(string)
		name, _ := m["Name"].(string)
		if strings.TrimSpace(name) == "" {
			return
		}
		desc, _ := m["Description"].(string)
		level, _ := m["Level"].(string)
		h.appendNotification(ctx, Notification{
			UserId:      guid.Normalize(uid),
			Name:        strings.TrimSpace(name),
			Description: strings.TrimSpace(desc),
			Level:       level,
		})
	})
}

func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
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
