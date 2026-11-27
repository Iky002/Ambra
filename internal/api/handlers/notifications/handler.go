package notifications

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/pkg/i18n"
	"github.com/ambra-media/ambra/internal/pkg/netutil"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

const (
	stateConfigKey           = "notifications:state"
	compatExtStateConfigKey  = "compatext:state"
	maxNotifications         = 500
	maxAdminNotifications    = 200
	notifHTTPDeliveryTimeout = 10 * time.Second
)

// AuthFuncs decouples route registration from the concrete middleware package.
type AuthFuncs struct {
	RequireAuth   func(http.Handler) http.Handler
	RequireAdmin  func(http.Handler) http.Handler
	UserIDFromCtx func(context.Context) string
}

type NotificationTypeInfo struct {
	Type      string `json:"Type"`
	Name      string `json:"Name"`
	Enabled   bool   `json:"Enabled"`
	Category  string `json:"Category"`
	IsBasedOn bool   `json:"IsBasedOnUserEvent"`
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
	AdminOnly   bool      `json:"AdminOnly,omitempty"`
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
	store            *sqlite.Store
	auth             AuthFuncs
	httpClient       *http.Client
	customHTTPClient bool

	dispatchMu     sync.Mutex
	dispatchClosed bool
	dispatchCtx    context.Context
	dispatchCancel context.CancelFunc
	dispatchWG     sync.WaitGroup

	mu                   sync.RWMutex
	state                notificationsState
	notifiedTaskFailures map[string]struct{}
}

func New(store *sqlite.Store) *Handler {
	ctx, cancel := context.WithCancel(context.Background())
	h := &Handler{
		store:                store,
		httpClient:           netutil.NewSafeHTTPClientNoLoopback(notifHTTPDeliveryTimeout, true),
		dispatchCtx:          ctx,
		dispatchCancel:       cancel,
		notifiedTaskFailures: make(map[string]struct{}),
	}
	h.loadState(context.Background())
	return h
}

// SetHTTPClient overrides the HTTP client used for outbound notification service delivery (primarily for testing).
func (h *Handler) SetHTTPClient(client *http.Client) {
	if client == nil {
		return
	}
	h.mu.Lock()
	h.httpClient = client
	h.customHTTPClient = true
	h.mu.Unlock()
}

// Close cancels any in-flight outbound HTTP notification deliveries and waits for completion.
func (h *Handler) Close() {
	h.dispatchMu.Lock()
	if h.dispatchClosed {
		h.dispatchMu.Unlock()
		return
	}
	h.dispatchClosed = true
	cancel := h.dispatchCancel
	h.dispatchCancel = nil
	h.dispatchMu.Unlock()

	if cancel != nil {
		cancel()
	}
	h.dispatchWG.Wait()
}

// PurgeUser removes in-memory and persisted notifications targeted to userID and strips userID from ReadBy lists.
func (h *Handler) PurgeUser(ctx context.Context, userID string) {
	norm := guid.Normalize(userID)
	if norm == "" {
		return
	}
	h.mu.Lock()
	changed := false
	kept := make([]Notification, 0, len(h.state.Items))
	for _, n := range h.state.Items {
		if n.UserId != "" && guid.Normalize(n.UserId) == norm {
			changed = true
			continue
		}
		if len(n.ReadBy) > 0 && containsString(n.ReadBy, norm) {
			n.ReadBy = setReadBy(n.ReadBy, norm, false)
			changed = true
		}
		kept = append(kept, n)
	}
	if changed {
		h.state.Items = kept
		_ = h.saveStateLocked(ctx)
	}
	h.mu.Unlock()
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

func (h *Handler) canAccessUserNotifications(ctx context.Context, targetUserID string) bool {
	ac, ok := middleware.AuthFromContext(ctx)
	if ok && ac != nil && (ac.User != nil || ac.IsAPIKey) {
		if ac.IsAdmin() {
			return true
		}
		callerID := guid.Normalize(ac.UserID())
		return callerID != "" && callerID == guid.Normalize(targetUserID)
	}
	if h.auth.UserIDFromCtx != nil {
		callerID := guid.Normalize(h.auth.UserIDFromCtx(ctx))
		return callerID != "" && callerID == guid.Normalize(targetUserID)
	}
	if h.auth.RequireAuth != nil {
		return false
	}
	// Fail-closed if an empty/invalid AuthContext was explicitly attached.
	if ok {
		return false
	}
	return true
}

func isSafeNotificationURL(raw string) bool {
	s := strings.TrimSpace(raw)
	if s == "" {
		return true
	}
	if strings.HasPrefix(s, "/") {
		return !strings.HasPrefix(s, "//") && !strings.HasPrefix(s, "/\\")
	}
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	return (scheme == "http" || scheme == "https") && u.Host != ""
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

func (h *Handler) isCallerAdmin(ctx context.Context) bool {
	ac, ok := middleware.AuthFromContext(ctx)
	if ok && ac != nil && (ac.User != nil || ac.IsAPIKey) {
		return ac.IsAdmin()
	}
	if ok {
		return false
	}
	if h.auth.RequireAuth != nil || h.auth.UserIDFromCtx != nil {
		return false
	}
	return true
}

func (h *Handler) handleListByUser(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	userID := guid.Normalize(r.PathValue("UserId"))
	if !h.canAccessUserNotifications(r.Context(), userID) {
		httpx.Error(w, http.StatusForbidden, "cannot access another user's notifications")
		return
	}
	isAdmin := h.isCallerAdmin(r.Context())
	isReadFilter := q.Bool("IsRead")
	acceptLang := strings.TrimSpace(r.Header.Get("Accept-Language"))
	locale := ""
	if acceptLang != "" {
		locale = i18n.ResolveLocale(acceptLang, "")
	}

	h.mu.RLock()
	matched := make([]Notification, 0, len(h.state.Items))
	for _, n := range h.state.Items {
		if n.AdminOnly && !isAdmin {
			continue
		}
		if n.UserId == "" || guid.Normalize(n.UserId) == userID {
			item := n
			item.IsRead = isReadForUser(n, userID)
			item.ReadBy = nil
			if isReadFilter != nil && item.IsRead != *isReadFilter {
				continue
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
	if !h.canAccessUserNotifications(r.Context(), userID) {
		httpx.Error(w, http.StatusForbidden, "cannot access another user's notifications")
		return
	}
	isAdmin := h.isCallerAdmin(r.Context())

	h.mu.RLock()
	unread := 0
	maxRank := 0
	maxLevel := ""
	for _, n := range h.state.Items {
		if n.AdminOnly && !isAdmin {
			continue
		}
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
		if !h.canAccessUserNotifications(r.Context(), userID) {
			httpx.Error(w, http.StatusForbidden, "cannot access another user's notifications")
			return
		}
		isAdmin := h.isCallerAdmin(r.Context())
		ids := q.List("Ids")
		if len(ids) == 0 && r.Body != nil {
			var body struct {
				Ids []string `json:"Ids"`
			}
			if err := httpx.DecodeJSONLimit(r, &body, 0); err != nil {
				if !errors.Is(err, io.EOF) {
					httpx.Error(w, http.StatusBadRequest, "invalid notification read payload")
					return
				}
			} else {
				ids = body.Ids
			}
		}

		idSet := make(map[string]struct{}, len(ids))
		for _, id := range ids {
			idSet[id] = struct{}{}
		}

		h.mu.Lock()
		snapshot := cloneNotificationsState(h.state)
		for i := range h.state.Items {
			n := &h.state.Items[i]
			if n.AdminOnly && !isAdmin {
				continue
			}
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
		err := h.saveStateLocked(r.Context())
		if err != nil {
			h.state = snapshot
		}
		h.mu.Unlock()
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "failed to persist notification read state")
			return
		}
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
		AdminOnly   *bool  `json:"AdminOnly"`
	}
	if err := httpx.DecodeJSONLimit(r, &req, 0); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid notification payload")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		httpx.Error(w, http.StatusBadRequest, "notification name is required")
		return
	}
	if !isSafeNotificationURL(req.Url) {
		httpx.Error(w, http.StatusBadRequest, "invalid notification Url scheme")
		return
	}
	if !isSafeNotificationURL(req.ImageUrl) {
		httpx.Error(w, http.StatusBadRequest, "invalid notification ImageUrl scheme")
		return
	}
	targetUserID := guid.Normalize(req.UserId)
	adminOnly := targetUserID == ""
	if req.AdminOnly != nil {
		adminOnly = *req.AdminOnly
	}
	level := normalizeLevel(req.Level)
	n := Notification{
		Id:          guid.New(),
		UserId:      targetUserID,
		Date:        time.Now().UTC(),
		IsRead:      false,
		Name:        req.Name,
		Description: req.Description,
		Url:         strings.TrimSpace(req.Url),
		ImageUrl:    strings.TrimSpace(req.ImageUrl),
		Level:       level,
		AdminOnly:   adminOnly,
	}

	if err := h.appendNotification(r.Context(), n); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to persist notification")
		return
	}

	httpx.JSON(w, http.StatusOK, n)
}

func (h *Handler) appendNotification(ctx context.Context, n Notification) error {
	return h.appendNotificationWithEvent(ctx, n, "")
}

// SendTestNotification records a test notification for userID (or AdminOnly when userID is empty)
// and dispatches any matching configured HTTP notification channels.
func (h *Handler) SendTestNotification(ctx context.Context, userID, title, description string) error {
	normUID := guid.Normalize(userID)
	n := Notification{
		Id:          guid.New(),
		UserId:      normUID,
		Date:        time.Now().UTC(),
		IsRead:      false,
		Name:        title,
		Description: description,
		Level:       "Normal",
		AdminOnly:   normUID == "",
	}
	return h.appendNotificationWithEvent(ctx, n, "NotificationTest")
}

func (h *Handler) appendNotificationWithEvent(ctx context.Context, n Notification, eventName string) error {
	if n.Id == "" {
		n.Id = guid.New()
	}
	if n.Date.IsZero() {
		n.Date = time.Now().UTC()
	}
	n.Level = normalizeLevel(n.Level)

	h.mu.Lock()
	snapshot := cloneNotificationsState(h.state)
	h.state.Items = append([]Notification{n}, h.state.Items...)
	h.trimNotificationsLocked()
	err := h.saveStateLocked(ctx)
	if err != nil {
		h.state = snapshot
	}
	h.mu.Unlock()
	if err == nil {
		h.dispatchConfiguredNotificationServices(ctx, n, eventName)
	}
	return err
}

type configuredNotifService struct {
	Id          string            `json:"Id"`
	Name        string            `json:"Name"`
	ServiceType string            `json:"ServiceType"`
	UserId      string            `json:"UserId"`
	Enabled     bool              `json:"Enabled"`
	Events      []string          `json:"Events"`
	Options     map[string]string `json:"Options"`
}

func configuredServiceURL(opts map[string]string) string {
	for _, k := range []string{"Url", "url", "WebhookUrl", "webhookUrl", "Endpoint", "endpoint"} {
		if v := strings.TrimSpace(opts[k]); v != "" {
			return v
		}
	}
	return ""
}

func matchesConfiguredEvents(events []string, eventName, notifName string) bool {
	if len(events) == 0 {
		return true
	}
	for _, ev := range events {
		ev = strings.TrimSpace(ev)
		if ev == "*" || (eventName != "" && strings.EqualFold(ev, eventName)) || strings.EqualFold(ev, notifName) {
			return true
		}
	}
	return false
}

func (h *Handler) dispatchConfiguredNotificationServices(ctx context.Context, n Notification, eventName string) {
	if h.store == nil || h.store.Reader == nil {
		return
	}
	var raw string
	err := h.store.Reader.QueryRowContext(ctx,
		`SELECT value_json FROM server_config WHERE key = ?`, compatExtStateConfigKey,
	).Scan(&raw)
	if err != nil || raw == "" {
		return
	}
	var st struct {
		NotificationServices []configuredNotifService `json:"notification_services"`
	}
	if err := json.Unmarshal([]byte(raw), &st); err != nil || len(st.NotificationServices) == 0 {
		return
	}

	h.mu.RLock()
	client := h.httpClient
	customClient := h.customHTTPClient
	h.mu.RUnlock()
	if client == nil {
		client = netutil.NewSafeHTTPClientNoLoopback(notifHTTPDeliveryTimeout, true)
	}

	var targets []configuredNotifService
	for _, svc := range st.NotificationServices {
		if !svc.Enabled {
			continue
		}
		targetURL := configuredServiceURL(svc.Options)
		if targetURL == "" {
			continue
		}
		if n.AdminOnly && svc.UserId != "" {
			continue
		}
		if n.UserId != "" && svc.UserId != "" && guid.Normalize(svc.UserId) != guid.Normalize(n.UserId) {
			continue
		}
		if !matchesConfiguredEvents(svc.Events, eventName, n.Name) {
			continue
		}
		if customClient {
			if err := netutil.ValidateRemoteURL(targetURL, true); err != nil {
				continue
			}
		} else {
			if err := netutil.ValidateRemoteURLNoLoopback(targetURL, true); err != nil {
				continue
			}
		}
		targets = append(targets, svc)
	}
	if len(targets) == 0 {
		return
	}

	evType := eventName
	if evType == "" {
		evType = "AdminNotification"
	}
	payloadBytes, err := json.Marshal(map[string]any{
		"Event":       evType,
		"Id":          n.Id,
		"Title":       n.Name,
		"Name":        n.Name,
		"Description": n.Description,
		"Level":       n.Level,
		"Date":        n.Date.Format(time.RFC3339),
		"Url":         n.Url,
	})
	if err != nil {
		return
	}

	h.dispatchMu.Lock()
	if h.dispatchClosed {
		h.dispatchMu.Unlock()
		return
	}
	baseCtx := h.dispatchCtx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	h.dispatchWG.Add(len(targets))
	h.dispatchMu.Unlock()

	for _, svc := range targets {
		svcCopy := svc
		go func() {
			defer h.dispatchWG.Done()
			reqCtx, cancel := context.WithTimeout(baseCtx, notifHTTPDeliveryTimeout)
			defer cancel()
			targetURL := configuredServiceURL(svcCopy.Options)
			req, reqErr := http.NewRequestWithContext(reqCtx, http.MethodPost, targetURL, bytes.NewReader(payloadBytes))
			if reqErr != nil {
				return
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Ambra-Notification", evType)
			if secret := strings.TrimSpace(svcCopy.Options["Secret"]); secret != "" {
				mac := hmac.New(sha256.New, []byte(secret))
				mac.Write(payloadBytes)
				req.Header.Set("X-Ambra-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
			} else if tok := strings.TrimSpace(svcCopy.Options["Token"]); tok != "" {
				req.Header.Set("Authorization", "Bearer "+tok)
			}
			resp, doErr := client.Do(req)
			if doErr != nil {
				return
			}
			defer resp.Body.Close()
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		}()
	}
}

func (h *Handler) trimNotificationsLocked() {
	if len(h.state.Items) <= maxNotifications {
		// Still enforce maxAdminNotifications cap for AdminOnly bucket.
		adminCount := 0
		for _, item := range h.state.Items {
			if item.AdminOnly {
				adminCount++
			}
		}
		if adminCount <= maxAdminNotifications {
			return
		}
	}
	// First reserve up to maxAdminNotifications AdminOnly items so user events cannot evict admin alerts.
	adminKept := 0
	for _, item := range h.state.Items {
		if item.AdminOnly && adminKept < maxAdminNotifications {
			adminKept++
		}
	}
	userQuota := maxNotifications - adminKept
	if userQuota < 0 {
		userQuota = 0
	}
	kept := make([]Notification, 0, maxNotifications)
	adminSeen := 0
	userSeen := 0
	for _, item := range h.state.Items {
		if item.AdminOnly {
			if adminSeen < maxAdminNotifications {
				kept = append(kept, item)
				adminSeen++
			}
		} else {
			if userSeen < userQuota {
				kept = append(kept, item)
				userSeen++
			}
		}
	}
	h.state.Items = kept
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
		_ = h.appendNotificationWithEvent(ctx, Notification{
			Name:        "Library Updated",
			Description: desc,
			Level:       "Normal",
			AdminOnly:   true,
		}, string(eventbus.EventLibraryChanged))
	})

	bus.Subscribe(eventbus.EventTaskProgress, func(ctx context.Context, e eventbus.Event) {
		raw, err := json.Marshal(e.Payload)
		if err != nil {
			return
		}
		var task struct {
			Id                  string `json:"Id"`
			Name                string `json:"Name"`
			State               string `json:"State"`
			LastExecutionResult *struct {
				Status       string `json:"Status"`
				ErrorMessage string `json:"ErrorMessage"`
				EndTimeUtc   string `json:"EndTimeUtc"`
			} `json:"LastExecutionResult"`
		}
		if err := json.Unmarshal(raw, &task); err != nil {
			return
		}
		if !strings.EqualFold(strings.TrimSpace(task.State), "Idle") {
			return
		}
		if task.LastExecutionResult != nil && strings.EqualFold(task.LastExecutionResult.Status, "Failed") {
			dedupTaskID := strings.TrimSpace(task.Id)
			dedupEnd := strings.TrimSpace(task.LastExecutionResult.EndTimeUtc)
			if dedupTaskID != "" || dedupEnd != "" {
				dedupKey := dedupTaskID + "|" + dedupEnd
				h.mu.Lock()
				if h.notifiedTaskFailures == nil {
					h.notifiedTaskFailures = make(map[string]struct{})
				}
				if _, seen := h.notifiedTaskFailures[dedupKey]; seen {
					h.mu.Unlock()
					return
				}
				if len(h.notifiedTaskFailures) >= 1000 {
					h.notifiedTaskFailures = make(map[string]struct{})
				}
				h.notifiedTaskFailures[dedupKey] = struct{}{}
				h.mu.Unlock()
			}
			name := strings.TrimSpace(task.Name)
			if name == "" {
				name = "Scheduled Task"
			}
			desc := strings.TrimSpace(task.LastExecutionResult.ErrorMessage)
			if desc == "" {
				desc = name + " failed"
			}
			_ = h.appendNotificationWithEvent(ctx, Notification{
				Name:        name + " Failed",
				Description: desc,
				Level:       "Error",
				AdminOnly:   true,
			}, "TaskFailed")
		}
	})

	bus.Subscribe(eventbus.EventUserDataChanged, func(ctx context.Context, e eventbus.Event) {
		switch p := e.Payload.(type) {
		case session.UserDataChangedPayload:
			h.recordUserDataChangedNotification(ctx, p)
		case *session.UserDataChangedPayload:
			if p != nil {
				h.recordUserDataChangedNotification(ctx, *p)
			}
		case map[string]any:
			uid, _ := p["UserId"].(string)
			normUID := guid.Normalize(uid)
			if normUID == "" {
				return
			}
			name, _ := p["Name"].(string)
			if strings.TrimSpace(name) == "" {
				return
			}
			desc, _ := p["Description"].(string)
			level, _ := p["Level"].(string)
			_ = h.appendNotificationWithEvent(ctx, Notification{
				UserId:      normUID,
				Name:        strings.TrimSpace(name),
				Description: strings.TrimSpace(desc),
				Level:       level,
			}, string(eventbus.EventUserDataChanged))
		}
	})
}

func (h *Handler) recordUserDataChangedNotification(ctx context.Context, p session.UserDataChangedPayload) {
	normUID := guid.Normalize(p.UserId)
	if normUID == "" || len(p.UserDataList) == 0 {
		return
	}
	name := "User Data Updated"
	desc := fmt.Sprintf("Updated user data for %d items", len(p.UserDataList))
	if len(p.UserDataList) == 1 {
		entry := p.UserDataList[0]
		switch {
		case entry.Played:
			name = "Item Marked Played"
			desc = fmt.Sprintf("Item %s marked as played", entry.ItemId)
		case entry.IsFavorite:
			name = "Item Added to Favorites"
			desc = fmt.Sprintf("Item %s marked as favorite", entry.ItemId)
		default:
			desc = fmt.Sprintf("Updated user data for item %s", entry.ItemId)
		}
	}
	_ = h.appendNotificationWithEvent(ctx, Notification{
		UserId:      normUID,
		Name:        name,
		Description: desc,
		Level:       "Normal",
	}, string(eventbus.EventUserDataChanged))
}

func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case []string:
		return len(n)
	case []any:
		return len(n)
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

func cloneNotificationsState(st notificationsState) notificationsState {
	if st.Items == nil {
		return notificationsState{}
	}
	out := notificationsState{
		Items: make([]Notification, len(st.Items)),
	}
	copy(out.Items, st.Items)
	for i := range out.Items {
		if st.Items[i].ReadBy != nil {
			out.Items[i].ReadBy = append([]string(nil), st.Items[i].ReadBy...)
		}
	}
	return out
}

func (h *Handler) saveStateLocked(ctx context.Context) error {
	if h.store == nil {
		return nil
	}
	raw, err := json.Marshal(h.state)
	if err != nil {
		slog.Error("notifications: marshal state failed", "err", err)
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
		slog.Error("notifications: save state failed", "err", err)
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
