package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/go-chi/chi/v5"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/api/middleware"
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
	id          string
	userID      string
	user        *sqlite.UserRecord
	deviceID    string
	token       string
	isAPIKey    bool
	isAdmin     bool
	conn        *websocket.Conn
	sendCh      chan Message
	cancel      context.CancelFunc

	mu          sync.RWMutex
	subSessions bool
	subTasks    bool
	subActivity bool
}

func (c *clientConn) hasLibraryRestrictions() bool {
	if c == nil || c.isAdmin || c.isAPIKey || c.user == nil {
		return false
	}
	return !c.user.Policy.EnableAllFolders ||
		c.user.Policy.MaxParentalRating != nil ||
		len(c.user.Policy.BlockedTags) > 0
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
	maxClients        int
	maxClientsPerUser int
	readTimeout       time.Duration

	mu                  sync.RWMutex
	clients             map[string]*clientConn
	allowedOrigins      map[string]struct{}
	allowWildcardOrigin bool
	libraryChangeFilter LibraryChangeFilterFunc
}

func NewHub(authSvc *auth.Service, sessionMgr *session.Manager, bus *eventbus.Bus) *Hub {
	h := &Hub{
		authSvc:           authSvc,
		sessionMgr:        sessionMgr,
		bus:               bus,
		maxClients:        defaultMaxWSClients,
		maxClientsPerUser: defaultMaxWSPerUser,
		readTimeout:       defaultReadTimeout,
		clients:           make(map[string]*clientConn),
	}
	h.subscribeEvents()
	return h
}

// SetAllowedOrigins configures an optional Origin allowlist for browser WebSocket handshakes.
// Empty Origin headers (native clients) are always permitted.
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

func (h *Hub) isOriginAllowed(origin string) bool {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		// Allow native Emby clients that do not send an Origin header.
		return true
	}
	h.mu.RLock()
	allowedSet := h.allowedOrigins
	wildcard := h.allowWildcardOrigin
	h.mu.RUnlock()

	if len(allowedSet) == 0 || wildcard {
		return true
	}
	norm := strings.ToLower(strings.TrimRight(origin, "/"))
	_, ok := allowedSet[norm]
	return ok
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
			if !c.hasLibraryRestrictions() {
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
		allSessions := h.sessionMgr.ListSessions()
		adminMsg := Message{
			MessageType: "Sessions",
			Data:        allSessions,
		}
		h.mu.RLock()
		for _, c := range h.clients {
			c.mu.RLock()
			subscribed := c.subSessions
			c.mu.RUnlock()
			if !subscribed {
				continue
			}
			if c.isAdmin {
				c.trySend(adminMsg)
			} else {
				c.trySend(Message{
					MessageType: "Sessions",
					Data:        filterSessionsForUser(allSessions, c.userID),
				})
			}
		}
		h.mu.RUnlock()
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

func filterSessionsForUser(sessions []dto.SessionInfo, userID string) []dto.SessionInfo {
	norm := guid.Normalize(userID)
	if norm == "" {
		return []dto.SessionInfo{}
	}
	out := make([]dto.SessionInfo, 0, len(sessions))
	for _, s := range sessions {
		if guid.Normalize(s.UserId) == norm {
			out = append(out, s)
		}
	}
	return out
}

func defaultFilterLibraryChange(user *sqlite.UserRecord, payload any) (any, bool) {
	if user == nil {
		return nil, false
	}
	if m, ok := payload.(map[string]any); ok {
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
	if !h.isOriginAllowed(r.Header.Get("Origin")) {
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

	if !id.IsAPIKey && id.User != nil && !id.User.Policy.EnableRemoteAccess && !netutil.IsLocalOrLANRequest(r) {
		httpx.Error(w, http.StatusForbidden, "Remote access is disabled for this user")
		return
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

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
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
			client.mu.Lock()
			client.subSessions = true
			client.mu.Unlock()
			if h.sessionMgr != nil {
				sessions := h.sessionMgr.ListSessions()
				if !client.isAdmin {
					sessions = filterSessionsForUser(sessions, client.userID)
				}
				client.trySend(Message{
					MessageType: "Sessions",
					Data:        sessions,
				})
			}
		case "SessionsStop":
			client.mu.Lock()
			client.subSessions = false
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
	delete(h.clients, id)
	h.mu.Unlock()
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
	h.mu.RLock()
	for _, c := range h.clients {
		if c.isAPIKey || guid.Normalize(c.userID) == norm {
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
		c.cancel()
		_ = c.conn.Close(websocket.StatusPolicyViolation, "token revoked")
	}
}

func (h *Hub) ClientCount() int {
	h.mu.RLock()
	n := len(h.clients)
	h.mu.RUnlock()
	return n
}
