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
)

const (
	keepAliveSeconds    = 60
	writeTimeout        = 10 * time.Second
	defaultReadTimeout  = time.Duration(keepAliveSeconds*3) * time.Second
	defaultMaxWSClients = 1024
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

// Hub manages WebSocket connections and bridges EventBus notifications to Emby clients.
type Hub struct {
	authSvc     *auth.Service
	sessionMgr  *session.Manager
	bus         *eventbus.Bus
	maxClients  int
	readTimeout time.Duration

	mu      sync.RWMutex
	clients map[string]*clientConn
}

func NewHub(authSvc *auth.Service, sessionMgr *session.Manager, bus *eventbus.Bus) *Hub {
	h := &Hub{
		authSvc:     authSvc,
		sessionMgr:  sessionMgr,
		bus:         bus,
		maxClients:  defaultMaxWSClients,
		readTimeout: defaultReadTimeout,
		clients:     make(map[string]*clientConn),
	}
	h.subscribeEvents()
	return h
}

func (h *Hub) RegisterRoutes(r chi.Router) {
	r.HandleFunc("/embywebsocket", h.ServeHTTP)
	r.HandleFunc("/socket", h.ServeHTTP)
}

func (h *Hub) subscribeEvents() {
	if h.bus == nil {
		return
	}

	h.bus.Subscribe(eventbus.EventLibraryChanged, func(_ context.Context, e eventbus.Event) {
		h.Broadcast(Message{
			MessageType: "LibraryChanged",
			Data:        e.Payload,
		})
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

func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	clientInfo := middleware.ExtractClientInfo(r)
	token := clientInfo.Token

	id, err := h.authSvc.ValidateToken(r.Context(), token)
	if err != nil {
		httpx.Error(w, http.StatusUnauthorized, "invalid api_key")
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

	userID := ""
	isAdmin := id.IsAPIKey
	if id.User != nil {
		userID = id.User.ID
		isAdmin = isAdmin || id.User.IsAdmin || id.User.Policy.IsAdministrator
	}

	client := &clientConn{
		id:       guid.New(),
		userID:   userID,
		deviceID: deviceID,
		token:    id.Token,
		isAPIKey: id.IsAPIKey,
		isAdmin:  isAdmin,
		conn:     conn,
		sendCh:   make(chan Message, 64),
		cancel:   cancel,
	}

	if !h.tryRegister(client, limit) {
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

func (h *Hub) tryRegister(c *clientConn, limit int) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.clients) >= limit {
		return false
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
