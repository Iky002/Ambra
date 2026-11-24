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

	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/pkg/guid"
)

const (
	keepAliveSeconds = 60
	writeTimeout     = 10 * time.Second
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
	isAPIKey    bool
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
	authSvc    *auth.Service
	sessionMgr *session.Manager
	bus        *eventbus.Bus

	mu      sync.RWMutex
	clients map[string]*clientConn
}

func NewHub(authSvc *auth.Service, sessionMgr *session.Manager, bus *eventbus.Bus) *Hub {
	h := &Hub{
		authSvc:    authSvc,
		sessionMgr: sessionMgr,
		bus:        bus,
		clients:    make(map[string]*clientConn),
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
		h.Broadcast(msg)
	})

	h.bus.Subscribe(eventbus.EventSessionUpdated, func(_ context.Context, _ eventbus.Event) {
		if h.sessionMgr == nil {
			return
		}
		sessions := h.sessionMgr.ListSessions()
		msg := Message{
			MessageType: "Sessions",
			Data:        sessions,
		}
		h.mu.RLock()
		for _, c := range h.clients {
			c.mu.RLock()
			subscribed := c.subSessions
			c.mu.RUnlock()
			if subscribed {
				c.trySend(msg)
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
			c.mu.RLock()
			subscribed := c.subTasks
			c.mu.RUnlock()
			if subscribed {
				c.trySend(msg)
			}
		}
		h.mu.RUnlock()
	})
}

func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	token := q.Get("api_key")
	if token == "" {
		token = r.Header.Get("X-Emby-Token")
	}
	if token == "" {
		token = r.Header.Get("X-MediaBrowser-Token")
	}

	id, err := h.authSvc.ValidateToken(r.Context(), token)
	if err != nil {
		httpx.Error(w, http.StatusUnauthorized, "invalid api_key")
		return
	}

	deviceID := q.Get("deviceId")
	if deviceID == "" {
		deviceID = id.DeviceID
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
	if id.User != nil {
		userID = id.User.ID
	}

	client := &clientConn{
		id:       guid.New(),
		userID:   userID,
		deviceID: deviceID,
		isAPIKey: id.IsAPIKey,
		conn:     conn,
		sendCh:   make(chan Message, 64),
		cancel:   cancel,
	}

	h.register(client)
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
	for {
		var in incomingMessage
		if err := wsjson.Read(ctx, client.conn, &in); err != nil {
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
				client.trySend(Message{
					MessageType: "Sessions",
					Data:        h.sessionMgr.ListSessions(),
				})
			}
		case "SessionsStop":
			client.mu.Lock()
			client.subSessions = false
			client.mu.Unlock()
		case "ScheduledTasksInfoStart":
			client.mu.Lock()
			client.subTasks = true
			client.mu.Unlock()
		case "ScheduledTasksInfoStop":
			client.mu.Lock()
			client.subTasks = false
			client.mu.Unlock()
		case "ActivityLogEntryStart":
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

func (h *Hub) register(c *clientConn) {
	h.mu.Lock()
	h.clients[c.id] = c
	h.mu.Unlock()
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

func (h *Hub) ClientCount() int {
	h.mu.RLock()
	n := len(h.clients)
	h.mu.RUnlock()
	return n
}
