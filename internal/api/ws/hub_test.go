package ws

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/go-chi/chi/v5"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func setupWSHub(t *testing.T) (*Hub, *auth.Service, *session.Manager, *eventbus.Bus, *httptest.Server) {
	t.Helper()
	dir := t.TempDir()
	store, err := sqlite.Open(filepath.Join(dir, "ws.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	cfg := config.Default(dir)
	repo := sqlite.NewUserRepo(store)
	authSvc := auth.NewService(repo, cfg)
	bus := eventbus.New()
	sessMgr := session.NewManager(repo, cfg, bus)
	hub := NewHub(authSvc, sessMgr, bus)

	r := chi.NewRouter()
	hub.RegisterRoutes(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	return hub, authSvc, sessMgr, bus, srv
}

func readMsg(t *testing.T, conn *websocket.Conn) Message {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var msg Message
	if err := wsjson.Read(ctx, conn, &msg); err != nil {
		t.Fatalf("wsjson.Read failed: %v", err)
	}
	return msg
}

func TestHubNilBusAndAuthFailures(t *testing.T) {
	// NewHub with nil bus should not panic
	hNil := NewHub(nil, nil, nil)
	if hNil.ClientCount() != 0 {
		t.Fatalf("expected 0 clients, got %d", hNil.ClientCount())
	}

	_, authSvc, _, _, srv := setupWSHub(t)

	// Missing token -> 401
	resp, err := http.Get(srv.URL + "/embywebsocket")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}

	// Valid token over plain HTTP (not websocket upgrade) -> Accept fails gracefully
	ctx := context.Background()
	if _, err := authSvc.CreateUser(ctx, "u1", "pw", false); err != nil {
		t.Fatal(err)
	}
	res, err := authSvc.AuthenticateByName(ctx, "u1", "pw", "", auth.ClientInfo{DeviceID: "d1"})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/socket", nil)
	req.Header.Set("X-Emby-Token", res.AccessToken)
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode == http.StatusUnauthorized {
		t.Fatalf("expected websocket upgrade error, not 401")
	}

	// Also test X-MediaBrowser-Token path
	req3, _ := http.NewRequest(http.MethodGet, srv.URL+"/socket", nil)
	req3.Header.Set("X-MediaBrowser-Token", res.AccessToken)
	resp3, err := http.DefaultClient.Do(req3)
	if err != nil {
		t.Fatal(err)
	}
	resp3.Body.Close()
	if resp3.StatusCode == http.StatusUnauthorized {
		t.Fatalf("expected websocket upgrade error for X-MediaBrowser-Token, not 401")
	}
}

func TestHubFullLifecycleAndEvents(t *testing.T) {
	hub, authSvc, _, bus, srv := setupWSHub(t)
	ctx := context.Background()

	u1, err := authSvc.CreateUser(ctx, "alice", "pw", true)
	if err != nil {
		t.Fatal(err)
	}
	res1, err := authSvc.AuthenticateByName(ctx, "alice", "pw", "", auth.ClientInfo{DeviceID: "dev-alice"})
	if err != nil {
		t.Fatal(err)
	}
	apiKey, err := authSvc.CreateAPIKey(ctx, "AdminTool")
	if err != nil {
		t.Fatal(err)
	}

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	// Connect user client (via api_key query, no deviceId -> uses token DeviceID)
	conn1, _, err := websocket.Dial(ctx, wsURL+"/embywebsocket?api_key="+res1.AccessToken, nil)
	if err != nil {
		t.Fatalf("dial user ws: %v", err)
	}
	defer conn1.Close(websocket.StatusNormalClosure, "")

	// Initial ForceKeepAlive
	msg := readMsg(t, conn1)
	if msg.MessageType != "ForceKeepAlive" {
		t.Fatalf("expected ForceKeepAlive, got %q", msg.MessageType)
	}

	// Connect API key client with explicit deviceId
	conn2, _, err := websocket.Dial(ctx, wsURL+"/socket?api_key="+apiKey.Token+"&deviceId=api-dev", nil)
	if err != nil {
		t.Fatalf("dial apikey ws: %v", err)
	}
	defer conn2.Close(websocket.StatusNormalClosure, "")
	if m := readMsg(t, conn2); m.MessageType != "ForceKeepAlive" {
		t.Fatalf("expected ForceKeepAlive on conn2, got %q", m.MessageType)
	}

	if hub.ClientCount() != 2 {
		t.Fatalf("expected 2 clients, got %d", hub.ClientCount())
	}

	// KeepAlive roundtrip
	if err := wsjson.Write(ctx, conn1, Message{MessageType: "KeepAlive"}); err != nil {
		t.Fatal(err)
	}
	if m := readMsg(t, conn1); m.MessageType != "KeepAlive" {
		t.Fatalf("expected KeepAlive, got %q", m.MessageType)
	}

	// SessionsStart -> receives immediate Sessions message
	if err := wsjson.Write(ctx, conn1, Message{MessageType: "SessionsStart"}); err != nil {
		t.Fatal(err)
	}
	if m := readMsg(t, conn1); m.MessageType != "Sessions" {
		t.Fatalf("expected Sessions, got %q", m.MessageType)
	}

	// ScheduledTasksInfoStart & ActivityLogEntryStart
	if err := wsjson.Write(ctx, conn1, Message{MessageType: "ScheduledTasksInfoStart"}); err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Write(ctx, conn1, Message{MessageType: "ActivityLogEntryStart"}); err != nil {
		t.Fatal(err)
	}
	// Give readLoop a moment to process subscriptions
	time.Sleep(50 * time.Millisecond)

	// EventBus: EventLibraryChanged -> broadcast to both
	bus.Publish(ctx, eventbus.Event{Type: eventbus.EventLibraryChanged, Payload: map[string]string{"id": "1"}})
	if m := readMsg(t, conn1); m.MessageType != "LibraryChanged" {
		t.Fatalf("expected LibraryChanged on conn1, got %q", m.MessageType)
	}
	if m := readMsg(t, conn2); m.MessageType != "LibraryChanged" {
		t.Fatalf("expected LibraryChanged on conn2, got %q", m.MessageType)
	}

	// EventBus: EventUserDataChanged (value struct) -> user + apikey
	bus.Publish(ctx, eventbus.Event{
		Type:    eventbus.EventUserDataChanged,
		Payload: session.UserDataChangedPayload{UserId: u1.ID},
	})
	if m := readMsg(t, conn1); m.MessageType != "UserDataChanged" {
		t.Fatalf("expected UserDataChanged on conn1, got %q", m.MessageType)
	}
	if m := readMsg(t, conn2); m.MessageType != "UserDataChanged" {
		t.Fatalf("expected UserDataChanged on conn2, got %q", m.MessageType)
	}

	// EventBus: EventUserDataChanged (pointer struct)
	bus.Publish(ctx, eventbus.Event{
		Type:    eventbus.EventUserDataChanged,
		Payload: &session.UserDataChangedPayload{UserId: u1.ID},
	})
	if m := readMsg(t, conn1); m.MessageType != "UserDataChanged" {
		t.Fatalf("expected UserDataChanged ptr on conn1, got %q", m.MessageType)
	}
	if m := readMsg(t, conn2); m.MessageType != "UserDataChanged" {
		t.Fatalf("expected UserDataChanged ptr on conn2, got %q", m.MessageType)
	}

	// EventBus: EventUserDataChanged (unknown payload -> broadcast)
	bus.Publish(ctx, eventbus.Event{
		Type:    eventbus.EventUserDataChanged,
		Payload: "generic",
	})
	if m := readMsg(t, conn1); m.MessageType != "UserDataChanged" {
		t.Fatalf("expected broadcast UserDataChanged on conn1, got %q", m.MessageType)
	}
	if m := readMsg(t, conn2); m.MessageType != "UserDataChanged" {
		t.Fatalf("expected broadcast UserDataChanged on conn2, got %q", m.MessageType)
	}

	// EventBus: EventSessionUpdated -> only conn1 subscribed
	bus.Publish(ctx, eventbus.Event{Type: eventbus.EventSessionUpdated})
	if m := readMsg(t, conn1); m.MessageType != "Sessions" {
		t.Fatalf("expected Sessions on conn1, got %q", m.MessageType)
	}

	// EventBus: EventTaskProgress -> only conn1 subscribed
	bus.Publish(ctx, eventbus.Event{Type: eventbus.EventTaskProgress, Payload: "50%"})
	if m := readMsg(t, conn1); m.MessageType != "ScheduledTasksInfo" {
		t.Fatalf("expected ScheduledTasksInfo on conn1, got %q", m.MessageType)
	}

	// Unsubscribe messages
	_ = wsjson.Write(ctx, conn1, Message{MessageType: "SessionsStop"})
	_ = wsjson.Write(ctx, conn1, Message{MessageType: "ScheduledTasksInfoStop"})
	_ = wsjson.Write(ctx, conn1, Message{MessageType: "ActivityLogEntryStop"})
	time.Sleep(50 * time.Millisecond)

	// SendToDevice
	hub.SendToDevice("api-dev", Message{MessageType: "GeneralCommand", Data: "ping"})
	if m := readMsg(t, conn2); m.MessageType != "GeneralCommand" {
		t.Fatalf("expected GeneralCommand on conn2, got %q", m.MessageType)
	}

	// DisconnectDevice
	hub.DisconnectDevice("dev-alice")
	if hub.ClientCount() != 1 {
		t.Fatalf("expected 1 client after DisconnectDevice, got %d", hub.ClientCount())
	}

	// Test trySend dropping when channel is full
	fullClient := &clientConn{sendCh: make(chan Message, 1)}
	fullClient.trySend(Message{MessageType: "One"})
	fullClient.trySend(Message{MessageType: "Two"}) // dropped, should not block
	if len(fullClient.sendCh) != 1 {
		t.Fatalf("expected 1 buffered message, got %d", len(fullClient.sendCh))
	}
}

func TestHubSecurityDeviceIDBindingAndAdminSubscriptions(t *testing.T) {
	hub, authSvc, sessMgr, bus, srv := setupWSHub(t)
	ctx := context.Background()

	adminUser, _ := authSvc.CreateUser(ctx, "admin", "pw", true)
	bobUser, _ := authSvc.CreateUser(ctx, "bob", "pw", false)
	_, _ = authSvc.AuthenticateByName(ctx, "admin", "pw", "", auth.ClientInfo{DeviceID: "dev-admin"})
	bobAuth, _ := authSvc.AuthenticateByName(ctx, "bob", "pw", "", auth.ClientInfo{DeviceID: "dev-bob"})

	sessMgr.TouchSession(adminUser, auth.ClientInfo{DeviceID: "dev-admin", Device: "AdminPC", Client: "Web"})
	sessMgr.TouchSession(bobUser, auth.ClientInfo{DeviceID: "dev-bob", Device: "BobPhone", Client: "iOS"})

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	// 1. Non-admin Bob attempts to spoof ?deviceId=dev-admin -> must stay bound to dev-bob
	bobConn, _, err := websocket.Dial(ctx, wsURL+"/embywebsocket?api_key="+bobAuth.AccessToken+"&deviceId=dev-admin", nil)
	if err != nil {
		t.Fatalf("dial bob ws: %v", err)
	}
	defer bobConn.Close(websocket.StatusNormalClosure, "")
	_ = readMsg(t, bobConn) // ForceKeepAlive

	// SendToDevice("dev-admin") must NOT reach Bob; SendToDevice("dev-bob") MUST reach Bob
	hub.SendToDevice("dev-admin", Message{MessageType: "GeneralCommand", Data: "admin-secret"})
	hub.SendToDevice("dev-bob", Message{MessageType: "GeneralCommand", Data: "bob-msg"})
	if m := readMsg(t, bobConn); m.MessageType != "GeneralCommand" || m.Data != "bob-msg" {
		t.Fatalf("expected only bob-msg (not admin-secret), got %+v", m)
	}

	// 2. Non-admin Bob sending SessionsStart receives ONLY Bob's session (not admin's)
	if err := wsjson.Write(ctx, bobConn, Message{MessageType: "SessionsStart"}); err != nil {
		t.Fatal(err)
	}
	m := readMsg(t, bobConn)
	if m.MessageType != "Sessions" {
		t.Fatalf("expected Sessions, got %+v", m)
	}
	list, ok := m.Data.([]any)
	if !ok || len(list) != 1 {
		t.Fatalf("expected exactly 1 session (bob only), got %#v", m.Data)
	}
	first, _ := list[0].(map[string]any)
	if first["UserId"] != bobUser.ID {
		t.Fatalf("expected bob's session UserId=%s, got %#v", bobUser.ID, first)
	}

	// 3. Non-admin Bob sending ScheduledTasksInfoStart & ActivityLogEntryStart must NOT receive EventTaskProgress
	_ = wsjson.Write(ctx, bobConn, Message{MessageType: "ScheduledTasksInfoStart"})
	_ = wsjson.Write(ctx, bobConn, Message{MessageType: "ActivityLogEntryStart"})
	time.Sleep(40 * time.Millisecond)
	bus.Publish(ctx, eventbus.Event{Type: eventbus.EventTaskProgress, Payload: "secret-task-progress"})
	_ = wsjson.Write(ctx, bobConn, Message{MessageType: "KeepAlive"})
	if m := readMsg(t, bobConn); m.MessageType != "KeepAlive" {
		t.Fatalf("non-admin must not receive ScheduledTasksInfo; expected KeepAlive, got %+v", m)
	}
}
