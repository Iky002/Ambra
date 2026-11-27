package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/go-chi/chi/v5"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/pkg/netutil"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

const (
	keepAliveSeconds    = 60
	writeTimeout        = 10 * time.Second
	defaultReadTimeout  = time.Duration(keepAliveSeconds*3) * time.Second
	defaultMaxWSClients = 1024
	defaultMaxWSPerUser = 32
)

// Message is the standard Emby WebSocket envelope.
type Message struct {
	MessageType string `json:"MessageType"`
	MessageId   string `json:"MessageId,omitempty"`
	Data        any    `json:"Data,omitempty"`
}

type incomingMessage struct {
	MessageType string          `json:"MessageType"`
	MessageId   string          `json:"MessageId,omitempty"`
	Data        json.RawMessage `json:"Data,omitempty"`
}

type clientConn struct {
	id       string
	userID   string
	user     *sqlite.UserRecord
	deviceID string
	token    string
	clientIP string
	wasLocal bool
	isAPIKey bool
	isAdmin  bool
	conn     *websocket.Conn
	sendCh   chan Message
	cancel   context.CancelFunc

	mu               sync.RWMutex
	closed           bool
	subSessions      bool
	sessionsInterval time.Duration
	lastSessionsSent time.Time
	sessionsPending  bool
	sessionsTimer    *time.Timer
	subTasks         bool
	subActivity      bool
}

func (c *clientConn) stopTimers() {
	c.mu.Lock()
	c.closed = true
	c.subSessions = false
	c.sessionsPending = false
	if c.sessionsTimer != nil {
		c.sessionsTimer.Stop()
		c.sessionsTimer = nil
	}
	c.mu.Unlock()
}

func (c *clientConn) trySend(msg Message) {
	if msg.MessageId == "" {
		msg.MessageId = guid.New()
	}
	select {
	case c.sendCh <- msg:
	default:
		// Drop if client buffer is full to prevent blocking the hub.
	}
}

// LibraryChangeFilterFunc filters or redacts an EventLibraryChanged payload for a restricted user.
// Returning (_, false) skips delivering the event to that user.
type LibraryChangeFilterFunc func(ctx context.Context, userID string, payload any) (any, bool)

// Hub manages WebSocket connections and bridges EventBus notifications to Emby clients.
type Hub struct {
	authSvc           *auth.Service
	sessionMgr        *session.Manager
	bus               *eventbus.Bus
	cfg               *config.Config
	maxClients        int
	maxClientsPerUser int
	readTimeout       time.Duration

	mu                  sync.RWMutex
	clients             map[string]*clientConn
	originsConfigured   bool
	allowedOrigins      map[string]struct{}
	allowWildcardOrigin bool
	libraryChangeFilter LibraryChangeFilterFunc
}

func NewHub(authSvc *auth.Service, sessionMgr *session.Manager, bus *eventbus.Bus) *Hub {
	var cfg *config.Config
	if authSvc != nil {
		cfg = authSvc.Config()
	}
	h := &Hub{
		authSvc:           authSvc,
		sessionMgr:        sessionMgr,
		bus:               bus,
		cfg:               cfg,
		maxClients:        defaultMaxWSClients,
		maxClientsPerUser: defaultMaxWSPerUser,
		readTimeout:       defaultReadTimeout,
		clients:           make(map[string]*clientConn),
	}
	h.subscribeEvents()
	return h
}

// SetConfig configures server-wide configuration for remote access checks.
func (h *Hub) SetConfig(cfg *config.Config) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.cfg = cfg
	h.mu.Unlock()
}

func (h *Hub) isGlobalRemoteAccessEnabled() bool {
	if h == nil {
		return true
	}
	h.mu.RLock()
	cfg := h.cfg
	h.mu.RUnlock()
	if cfg == nil && h.authSvc != nil {
		cfg = h.authSvc.Config()
	}
	if cfg == nil {
		return true
	}
	return cfg.Snapshot().IsGlobalRemoteAccessEnabled()
}

// SetAllowedOrigins configures an optional Origin allowlist for browser WebSocket handshakes.
// Empty Origin headers (native clients) and same-origin requests are always permitted.
func (h *Hub) SetAllowedOrigins(origins []string) {
	allowedSet := make(map[string]struct{}, len(origins))
	wildcard := false
	for _, raw := range origins {
		norm := strings.ToLower(strings.TrimRight(strings.TrimSpace(raw), "/"))
		if norm == "" {
			continue
		}
		if norm == "*" {
			wildcard = true
			continue
		}
		allowedSet[norm] = struct{}{}
	}
	h.mu.Lock()
	h.originsConfigured = true
	h.allowedOrigins = allowedSet
	h.allowWildcardOrigin = wildcard
	h.mu.Unlock()
}

// SetLibraryChangeFilter installs an optional callback to filter LibraryChanged events for restricted users.
func (h *Hub) SetLibraryChangeFilter(fn func(ctx context.Context, userID string, payload any) (any, bool)) {
	h.mu.Lock()
	h.libraryChangeFilter = fn
	h.mu.Unlock()
}

func escapeWSOriginHostPattern(host string) string {
	var b strings.Builder
	for i := 0; i < len(host); i++ {
		switch host[i] {
		case '[', ']', '*', '?', '\\':
			b.WriteByte('\\')
		}
		b.WriteByte(host[i])
	}
	return b.String()
}

func (h *Hub) checkWSOrigin(r *http.Request) (bool, []string) {
	if r == nil {
		return false, nil
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		// Allow native Emby clients that do not send an Origin header.
		return true, nil
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || u.User != nil {
		return false, nil
	}
	scheme := strings.ToLower(strings.TrimSpace(u.Scheme))
	if scheme != "http" && scheme != "https" {
		return false, nil
	}
	if middleware.IsSameOrigin(origin, r) {
		return true, []string{escapeWSOriginHostPattern(u.Host)}
	}

	h.mu.RLock()
	configured := h.originsConfigured
	allowedSet := h.allowedOrigins
	wildcard := h.allowWildcardOrigin
	cfg := h.cfg
	h.mu.RUnlock()

	if !configured {
		if cfg == nil && h.authSvc != nil {
			cfg = h.authSvc.Config()
		}
		if cfg != nil {
			raw := cfg.Snapshot().AllowedOrigins
			allowedSet = make(map[string]struct{}, len(raw))
			for _, item := range raw {
				norm := strings.ToLower(strings.TrimRight(strings.TrimSpace(item), "/"))
				if norm == "" {
					continue
				}
				if norm == "*" {
					wildcard = true
					continue
				}
				allowedSet[norm] = struct{}{}
			}
		}
	}

	if wildcard {
		return true, []string{"*"}
	}
	norm := strings.ToLower(strings.TrimRight(origin, "/"))
	if _, ok := allowedSet[norm]; ok {
		return true, []string{escapeWSOriginHostPattern(u.Host)}
	}
	return false, nil
}

func (h *Hub) RegisterRoutes(r chi.Router) {
	r.HandleFunc("/embywebsocket", h.ServeHTTP)
	r.HandleFunc("/socket", h.ServeHTTP)
}

func (h *Hub) subscribeEvents() {
	if h.bus == nil {
		return
	}

	h.bus.Subscribe(eventbus.EventLibraryChanged, func(ctx context.Context, e eventbus.Event) {
		h.mu.RLock()
		filter := h.libraryChangeFilter
		snapshot := make([]*clientConn, 0, len(h.clients))
		for _, c := range h.clients {
			snapshot = append(snapshot, c)
		}
		h.mu.RUnlock()

		fullMsg := Message{
			MessageType: "LibraryChanged",
			Data:        e.Payload,
		}
		for _, c := range snapshot {
			if c.isAdmin || c.isAPIKey {
				c.trySend(fullMsg)
				continue
			}
			if filter != nil {
				filteredPayload, ok := filter(ctx, c.userID, e.Payload)
				if !ok {
					continue
				}
				c.trySend(Message{
					MessageType: "LibraryChanged",
					Data:        filteredPayload,
				})
				continue
			}
			if safePayload, ok := defaultFilterLibraryChange(c.user, e.Payload); ok {
				c.trySend(Message{
					MessageType: "LibraryChanged",
					Data:        safePayload,
				})
			}
		}
	})

	h.bus.Subscribe(eventbus.EventUserDataChanged, func(_ context.Context, e eventbus.Event) {
		msg := Message{
			MessageType: "UserDataChanged",
			Data:        e.Payload,
		}
		if payload, ok := e.Payload.(session.UserDataChangedPayload); ok && payload.UserId != "" {
			h.SendToUser(payload.UserId, msg)
			return
		}
		if payload, ok := e.Payload.(*session.UserDataChangedPayload); ok && payload != nil && payload.UserId != "" {
			h.SendToUser(payload.UserId, msg)
			return
		}
		// Unknown payload or empty UserId: restrict delivery to API-key and administrator clients only.
		h.mu.RLock()
		for _, c := range h.clients {
			if c.isAPIKey || c.isAdmin {
				c.trySend(msg)
			}
		}
		h.mu.RUnlock()
	})

	h.bus.Subscribe(eventbus.EventSessionUpdated, func(_ context.Context, _ eventbus.Event) {
		if h.sessionMgr == nil {
			return
		}
		h.mu.RLock()
		snapshot := make([]*clientConn, 0, len(h.clients))
		for _, c := range h.clients {
			snapshot = append(snapshot, c)
		}
		h.mu.RUnlock()

		now := time.Now()
		var immediate []*clientConn
		for _, c := range snapshot {
			c.mu.Lock()
			if c.closed || !c.subSessions {
				c.mu.Unlock()
				continue
			}
			if c.sessionsInterval <= 0 {
				c.lastSessionsSent = now
				c.sessionsPending = false
				c.mu.Unlock()
				immediate = append(immediate, c)
				continue
			}
			elapsed := now.Sub(c.lastSessionsSent)
			if (!c.lastSessionsSent.IsZero() && elapsed < c.sessionsInterval) || c.sessionsTimer != nil {
				c.sessionsPending = true
				if c.sessionsTimer == nil {
					wait := c.sessionsInterval - elapsed
					if wait <= 0 {
						wait = c.sessionsInterval
					}
					target := c
					c.sessionsTimer = time.AfterFunc(wait, func() {
						h.flushPendingSessions(target)
					})
				}
				c.mu.Unlock()
				continue
			}
			c.lastSessionsSent = now
			c.sessionsPending = false
			c.mu.Unlock()
			immediate = append(immediate, c)
		}

		if len(immediate) == 0 {
			return
		}
		allSessions := h.sessionMgr.ListSessions()
		for _, c := range immediate {
			h.sendSessionsSnapshotToClient(c, allSessions)
		}
	})

	h.bus.Subscribe(eventbus.EventTaskProgress, func(_ context.Context, e eventbus.Event) {
		msg := Message{
			MessageType: "ScheduledTasksInfo",
			Data:        e.Payload,
		}
		h.mu.RLock()
		for _, c := range h.clients {
			if !c.isAdmin {
				continue
			}
			c.mu.RLock()
			subscribed := c.subTasks
			c.mu.RUnlock()
			if subscribed {
				c.trySend(msg)
			}
		}
		h.mu.RUnlock()
	})

	h.bus.Subscribe(eventbus.EventActivityLogCreated, func(_ context.Context, e eventbus.Event) {
		msg := Message{
			MessageType: "ActivityLogEntry",
			Data:        e.Payload,
		}
		h.mu.RLock()
		for _, c := range h.clients {
			if !c.isAdmin {
				continue
			}
			c.mu.RLock()
			subscribed := c.subActivity
			c.mu.RUnlock()
			if subscribed {
				c.trySend(msg)
			}
		}
		h.mu.RUnlock()
	})
}

func (h *Hub) sendSessionsSnapshotToClient(c *clientConn, allSessions []dto.SessionInfo) {
	if c == nil {
		return
	}
	if c.isAdmin || c.isAPIKey {
		c.trySend(Message{
			MessageType: "Sessions",
			Data:        allSessions,
		})
		return
	}
	c.trySend(Message{
		MessageType: "Sessions",
		Data:        filterSessionsForUser(allSessions, c.userID),
	})
}

func (h *Hub) flushPendingSessions(c *clientConn) {
	c.mu.Lock()
	c.sessionsTimer = nil
	if c.closed || !c.subSessions || !c.sessionsPending {
		c.sessionsPending = false
		c.mu.Unlock()
		return
	}
	c.sessionsPending = false
	c.lastSessionsSent = time.Now()
	c.mu.Unlock()

	if h.sessionMgr == nil {
		return
	}
	h.sendSessionsSnapshotToClient(c, h.sessionMgr.ListSessions())
}

func parseSubscriptionTiming(raw json.RawMessage) (time.Duration, time.Duration) {
	if len(raw) == 0 {
		return 0, 0
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		s = strings.Trim(string(raw), "\" \t\r\n")
	}
	s = strings.TrimSpace(s)
	if s == "" || s == "null" {
		return 0, 0
	}
	clampMs := func(ms int64) time.Duration {
		if ms <= 0 {
			return 0
		}
		if ms > 60_000 {
			ms = 60_000
		}
		return time.Duration(ms) * time.Millisecond
	}
	parts := strings.Split(s, ",")
	if len(parts) == 1 {
		ms, _ := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
		return 0, clampMs(ms)
	}
	initMs, _ := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	intvMs, _ := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
	return clampMs(initMs), clampMs(intvMs)
}

func filterSessionsForUser(sessions []dto.SessionInfo, userID string) []dto.SessionInfo {
	norm := guid.Normalize(userID)
	if norm == "" {
		return []dto.SessionInfo{}
	}
	out := make([]dto.SessionInfo, 0, len(sessions))
	for _, s := range sessions {
		if guid.Normalize(s.UserId) == norm {
			out = append(out, s)
			continue
		}
		for _, au := range s.AdditionalUsers {
			if guid.Normalize(au.UserId) == norm {
				out = append(out, s)
				break
			}
		}
	}
	return out
}

func defaultFilterLibraryChange(user *sqlite.UserRecord, payload any) (any, bool) {
	if user == nil || user.IsDisabled || user.Policy.IsDisabled {
		return nil, false
	}
	if m, ok := payload.(map[string]any); ok {
		if src, _ := m["source"].(string); strings.EqualFold(src, "dvr") {
			if !user.Policy.EnableLiveTvAccess {
				return nil, false
			}
			return map[string]any{"Source": "dvr"}, true
		}
		if _, hasOutputPath := m["output_path"]; hasOutputPath {
			if !user.Policy.EnableLiveTvAccess {
				return nil, false
			}
			return map[string]any{"LibraryChanged": true}, true
		}
		libID := ""
		if v, ok := m["LibraryId"].(string); ok {
			libID = guid.Normalize(v)
		} else if v, ok := m["library_id"].(string); ok {
			libID = guid.Normalize(v)
		}
		if libID != "" && !user.Policy.EnableAllFolders {
			allowed := false
			for _, f := range user.Policy.EnabledFolders {
				if guid.Normalize(f) == libID {
					allowed = true
					break
				}
			}
			if !allowed {
				return nil, false
			}
		}
		if libID != "" {
			return map[string]any{"LibraryId": libID}, true
		}
	}
	// Do not leak restricted item IDs or internal paths; emit a minimal refresh signal.
	return map[string]any{"LibraryChanged": true}, true
}

func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	allowedOrigin, originPatterns := h.checkWSOrigin(r)
	if !allowedOrigin {
		httpx.Error(w, http.StatusForbidden, "origin not allowed")
		return
	}

	clientInfo := middleware.ExtractClientInfo(r)
	token := clientInfo.Token

	id, err := h.authSvc.ValidateToken(r.Context(), token)
	if err != nil {
		httpx.Error(w, http.StatusUnauthorized, "invalid api_key")
		return
	}

	isLocal := netutil.IsLocalOrLANRequest(r)
	clientIP := netutil.ClientIP(r)
	if !id.IsAPIKey && !isLocal {
		if !h.isGlobalRemoteAccessEnabled() {
			httpx.Error(w, http.StatusForbidden, "Remote access is disabled on this server")
			return
		}
		if id.User != nil && !id.User.Policy.EnableRemoteAccess {
			httpx.Error(w, http.StatusForbidden, "Remote access is disabled for this user")
			return
		}
	}

	limit := h.maxClients
	if limit <= 0 {
		limit = defaultMaxWSClients
	}
	if h.ClientCount() >= limit {
		httpx.Error(w, http.StatusServiceUnavailable, "too many websocket connections")
		return
	}

	userID := ""
	isAdmin := id.IsAPIKey
	if id.User != nil {
		userID = id.User.ID
		isAdmin = isAdmin || id.User.IsAdmin || id.User.Policy.IsAdministrator
	}

	perUserLimit := h.maxClientsPerUser
	if perUserLimit <= 0 {
		perUserLimit = defaultMaxWSPerUser
	}
	if !id.IsAPIKey && userID != "" && h.userClientCount(userID) >= perUserLimit {
		httpx.Error(w, http.StatusTooManyRequests, "too many websocket connections for user")
		return
	}

	// Enforce token-bound DeviceID for user tokens so clients cannot spoof another device's channel.
	deviceID := id.DeviceID
	if id.IsAPIKey || deviceID == "" {
		if clientInfo.DeviceID != "" {
			deviceID = clientInfo.DeviceID
		}
	}

	// Clear the HTTP server's ReadTimeout before hijacking into a long-lived WebSocket connection.
	_ = http.NewResponseController(w).SetReadDeadline(time.Time{})

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: originPatterns,
	})
	if err != nil {
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	client := &clientConn{
		id:       guid.New(),
		userID:   userID,
		user:     id.User,
		deviceID: deviceID,
		token:    id.Token,
		clientIP: clientIP,
		wasLocal: isLocal,
		isAPIKey: id.IsAPIKey,
		isAdmin:  isAdmin,
		conn:     conn,
		sendCh:   make(chan Message, 64),
		cancel:   cancel,
	}

	if !h.tryRegister(client, limit, perUserLimit) {
		_ = conn.Close(websocket.StatusTryAgainLater, "too many websocket connections")
		return
	}
	defer func() {
		h.unregister(client.id)
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}()

	// Send ForceKeepAlive immediately upon connection establishment.
	client.trySend(Message{
		MessageType: "ForceKeepAlive",
		Data:        keepAliveSeconds,
	})

	go h.writeLoop(ctx, client)
	h.readLoop(ctx, client)
}

func (h *Hub) writeLoop(ctx context.Context, client *clientConn) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-client.sendCh:
			wctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := wsjson.Write(wctx, client.conn, msg)
			cancel()
			if err != nil {
				client.cancel()
				return
			}
		}
	}
}

func (h *Hub) readLoop(ctx context.Context, client *clientConn) {
	timeout := h.readTimeout
	if timeout <= 0 {
		timeout = defaultReadTimeout
	}
	for {
		rctx, rcancel := context.WithTimeout(ctx, timeout)
		var in incomingMessage
		err := wsjson.Read(rctx, client.conn, &in)
		rcancel()
		if err != nil {
			return
		}
		switch strings.TrimSpace(in.MessageType) {
		case "KeepAlive":
			client.trySend(Message{MessageType: "KeepAlive"})
		case "SessionsStart":
			initDelay, interval := parseSubscriptionTiming(in.Data)
			client.mu.Lock()
			if client.sessionsTimer != nil {
				client.sessionsTimer.Stop()
				client.sessionsTimer = nil
			}
			client.subSessions = true
			client.sessionsInterval = interval
			if initDelay > 0 {
				client.sessionsPending = true
				cRef := client
				client.sessionsTimer = time.AfterFunc(initDelay, func() {
					h.flushPendingSessions(cRef)
				})
				client.mu.Unlock()
			} else {
				client.sessionsPending = false
				client.lastSessionsSent = time.Now()
				client.mu.Unlock()
				if h.sessionMgr != nil {
					h.sendSessionsSnapshotToClient(client, h.sessionMgr.ListSessions())
				}
			}
		case "SessionsStop":
			client.mu.Lock()
			client.subSessions = false
			client.sessionsPending = false
			if client.sessionsTimer != nil {
				client.sessionsTimer.Stop()
				client.sessionsTimer = nil
			}
			client.mu.Unlock()
		case "ScheduledTasksInfoStart":
			if !client.isAdmin {
				continue
			}
			client.mu.Lock()
			client.subTasks = true
			client.mu.Unlock()
		case "ScheduledTasksInfoStop":
			client.mu.Lock()
			client.subTasks = false
			client.mu.Unlock()
		case "ActivityLogEntryStart":
			if !client.isAdmin {
				continue
			}
			client.mu.Lock()
			client.subActivity = true
			client.mu.Unlock()
		case "ActivityLogEntryStop":
			client.mu.Lock()
			client.subActivity = false
			client.mu.Unlock()
		}
	}
}

func (h *Hub) userClientCount(userID string) int {
	norm := guid.Normalize(userID)
	if norm == "" {
		return 0
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	count := 0
	for _, c := range h.clients {
		if !c.isAPIKey && guid.Normalize(c.userID) == norm {
			count++
		}
	}
	return count
}

func (h *Hub) tryRegister(c *clientConn, limit, perUserLimit int) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.clients) >= limit {
		return false
	}
	if !c.isAPIKey && c.userID != "" && perUserLimit > 0 {
		norm := guid.Normalize(c.userID)
		userCount := 0
		for _, existing := range h.clients {
			if !existing.isAPIKey && guid.Normalize(existing.userID) == norm {
				userCount++
			}
		}
		if userCount >= perUserLimit {
			return false
		}
	}
	h.clients[c.id] = c
	return true
}

func (h *Hub) unregister(id string) {
	h.mu.Lock()
	c := h.clients[id]
	delete(h.clients, id)
	h.mu.Unlock()
	if c != nil {
		c.stopTimers()
	}
}

func (h *Hub) Broadcast(msg Message) {
	h.mu.RLock()
	for _, c := range h.clients {
		c.trySend(msg)
	}
	h.mu.RUnlock()
}

func (h *Hub) SendToUser(userID string, msg Message) {
	norm := guid.Normalize(userID)
	if norm == "" {
		return
	}
	h.mu.RLock()
	for _, c := range h.clients {
		if !c.isAPIKey && guid.Normalize(c.userID) == norm {
			c.trySend(msg)
		}
	}
	h.mu.RUnlock()
}

// SendToUsers delivers msg to all active WebSocket connections belonging to any user in userIDs.
func (h *Hub) SendToUsers(userIDs []string, msg Message) {
	if h == nil || len(userIDs) == 0 {
		return
	}
	targets := make(map[string]struct{}, len(userIDs))
	for _, uid := range userIDs {
		if norm := guid.Normalize(uid); norm != "" {
			targets[norm] = struct{}{}
		}
	}
	if len(targets) == 0 {
		return
	}
	h.mu.RLock()
	for _, c := range h.clients {
		if c.isAPIKey {
			continue
		}
		if _, ok := targets[guid.Normalize(c.userID)]; ok {
			c.trySend(msg)
		}
	}
	h.mu.RUnlock()
}

func (h *Hub) SendToDevice(deviceID string, msg Message) {
	h.mu.RLock()
	for _, c := range h.clients {
		if c.deviceID == deviceID {
			c.trySend(msg)
		}
	}
	h.mu.RUnlock()
}

// SendToUserDevice sends msg only to WebSocket connections matching both userID and deviceID.
func (h *Hub) SendToUserDevice(userID, deviceID string, msg Message) {
	norm := guid.Normalize(userID)
	if norm == "" || deviceID == "" {
		return
	}
	h.mu.RLock()
	for _, c := range h.clients {
		if !c.isAPIKey && c.deviceID == deviceID && guid.Normalize(c.userID) == norm {
			c.trySend(msg)
		}
	}
	h.mu.RUnlock()
}

func (h *Hub) DisconnectDevice(deviceID string) {
	h.mu.Lock()
	var targets []*clientConn
	for id, c := range h.clients {
		if c.deviceID == deviceID {
			targets = append(targets, c)
			delete(h.clients, id)
		}
	}
	h.mu.Unlock()

	for _, c := range targets {
		c.stopTimers()
		c.cancel()
		_ = c.conn.Close(websocket.StatusPolicyViolation, "device revoked")
	}
}

// DisconnectUserDevice closes WebSocket connections belonging to both userID and deviceID.
func (h *Hub) DisconnectUserDevice(userID, deviceID string) {
	norm := guid.Normalize(userID)
	if norm == "" || deviceID == "" {
		return
	}
	h.mu.Lock()
	var targets []*clientConn
	for id, c := range h.clients {
		if !c.isAPIKey && c.deviceID == deviceID && guid.Normalize(c.userID) == norm {
			targets = append(targets, c)
			delete(h.clients, id)
		}
	}
	h.mu.Unlock()

	for _, c := range targets {
		c.stopTimers()
		c.cancel()
		_ = c.conn.Close(websocket.StatusPolicyViolation, "device revoked")
	}
}

// DisconnectUserExceptToken closes all active WebSocket connections for userID except keepToken.
func (h *Hub) DisconnectUserExceptToken(userID, keepToken string) {
	norm := guid.Normalize(userID)
	if norm == "" {
		return
	}
	keepToken = strings.TrimSpace(keepToken)
	h.mu.Lock()
	var targets []*clientConn
	for id, c := range h.clients {
		if !c.isAPIKey && guid.Normalize(c.userID) == norm && (keepToken == "" || c.token != keepToken) {
			targets = append(targets, c)
			delete(h.clients, id)
		}
	}
	h.mu.Unlock()

	for _, c := range targets {
		c.stopTimers()
		c.cancel()
		_ = c.conn.Close(websocket.StatusPolicyViolation, "user session revoked")
	}
}

// DisconnectUser closes all active WebSocket connections belonging to userID.
func (h *Hub) DisconnectUser(userID string) {
	norm := guid.Normalize(userID)
	if norm == "" {
		return
	}
	h.mu.Lock()
	var targets []*clientConn
	for id, c := range h.clients {
		if !c.isAPIKey && guid.Normalize(c.userID) == norm {
			targets = append(targets, c)
			delete(h.clients, id)
		}
	}
	h.mu.Unlock()

	for _, c := range targets {
		c.stopTimers()
		c.cancel()
		_ = c.conn.Close(websocket.StatusPolicyViolation, "user session revoked")
	}
}

// DisconnectToken closes all active WebSocket connections authenticated with token.
func (h *Hub) DisconnectToken(token string) {
	token = strings.TrimSpace(token)
	if token == "" {
		return
	}
	h.mu.Lock()
	var targets []*clientConn
	for id, c := range h.clients {
		if c.token == token {
			targets = append(targets, c)
			delete(h.clients, id)
		}
	}
	h.mu.Unlock()

	for _, c := range targets {
		c.stopTimers()
		c.cancel()
		_ = c.conn.Close(websocket.StatusPolicyViolation, "token revoked")
	}
}

// DisconnectRemoteClients closes active non-API-key WebSocket connections that are no longer
// permitted under current global remote access, per-user remote access, or local subnet policy.
func (h *Hub) DisconnectRemoteClients() {
	if h == nil {
		return
	}
	globalRemoteEnabled := h.isGlobalRemoteAccessEnabled()

	h.mu.Lock()
	var targets []*clientConn
	for id, c := range h.clients {
		if c.isAPIKey {
			continue
		}
		addr, err := netip.ParseAddr(strings.TrimSpace(c.clientIP))
		isRemote := !c.wasLocal || err != nil || !netutil.IsLANIP(addr)
		if !isRemote {
			continue
		}
		if !globalRemoteEnabled || (c.user != nil && !c.user.Policy.EnableRemoteAccess) {
			targets = append(targets, c)
			delete(h.clients, id)
		}
	}
	h.mu.Unlock()

	for _, c := range targets {
		c.stopTimers()
		if c.cancel != nil {
			c.cancel()
		}
		if c.conn != nil {
			_ = c.conn.Close(websocket.StatusPolicyViolation, "remote access disabled")
		}
	}
}

// Close closes all active WebSocket connections managed by the hub.
func (h *Hub) Close() {
	if h == nil {
		return
	}
	h.mu.Lock()
	targets := make([]*clientConn, 0, len(h.clients))
	for id, c := range h.clients {
		targets = append(targets, c)
		delete(h.clients, id)
	}
	h.mu.Unlock()

	for _, c := range targets {
		c.stopTimers()
		if c.cancel != nil {
			c.cancel()
		}
		if c.conn != nil {
			_ = c.conn.Close(websocket.StatusGoingAway, "server shutting down")
		}
	}
}

func (h *Hub) ClientCount() int {
	h.mu.RLock()
	n := len(h.clients)
	h.mu.RUnlock()
	return n
}
