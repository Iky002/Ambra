package ws

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
)

func TestHubAuthHeadersActivityLogUnknownUserDataAndDisconnects(t *testing.T) {
	hub, authSvc, _, bus, srv := setupWSHub(t)
	ctx := context.Background()

	adminUser, _ := authSvc.CreateUser(ctx, "admin", "pw", true)
	bobUser, _ := authSvc.CreateUser(ctx, "bob", "pw", false)
	adminRes, _ := authSvc.AuthenticateByName(ctx, "admin", "pw", "", auth.ClientInfo{DeviceID: "d-admin"})
	bobRes1, _ := authSvc.AuthenticateByName(ctx, "bob", "pw", "", auth.ClientInfo{DeviceID: "d-bob-1"})
	bobRes2, _ := authSvc.AuthenticateByName(ctx, "bob", "pw", "", auth.ClientInfo{DeviceID: "d-bob-2"})

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	// 1. Connect admin using X-Emby-Authorization header
	adminHdr := http.Header{}
	adminHdr.Set("X-Emby-Authorization", `MediaBrowser Client="Test", Device="Mac", DeviceId="d-admin", Version="1.0", Token="`+adminRes.AccessToken+`"`)
	adminConn, _, err := websocket.Dial(ctx, wsURL+"/embywebsocket", &websocket.DialOptions{HTTPHeader: adminHdr})
	if err != nil {
		t.Fatalf("dial admin via X-Emby-Authorization: %v", err)
	}
	defer adminConn.Close(websocket.StatusNormalClosure, "")
	_ = readMsg(t, adminConn) // ForceKeepAlive

	// 2. Connect bob conn1 via Authorization Bearer header, bob conn2 via api_key query
	bobHdr := http.Header{}
	bobHdr.Set("Authorization", "Bearer "+bobRes1.AccessToken)
	bobConn1, _, err := websocket.Dial(ctx, wsURL+"/embywebsocket", &websocket.DialOptions{HTTPHeader: bobHdr})
	if err != nil {
		t.Fatalf("dial bob via Authorization Bearer: %v", err)
	}
	defer bobConn1.Close(websocket.StatusNormalClosure, "")
	_ = readMsg(t, bobConn1)

	bobConn2, _, err := websocket.Dial(ctx, wsURL+"/socket?api_key="+bobRes2.AccessToken, nil)
	if err != nil {
		t.Fatalf("dial bob2: %v", err)
	}
	defer bobConn2.Close(websocket.StatusNormalClosure, "")
	_ = readMsg(t, bobConn2)

	// 3. Subscribe admin to ActivityLogEntry; bob also tries (must be ignored)
	_ = wsjson.Write(ctx, adminConn, Message{MessageType: "ActivityLogEntryStart"})
	_ = wsjson.Write(ctx, bobConn1, Message{MessageType: "ActivityLogEntryStart"})
	time.Sleep(40 * time.Millisecond)

	bus.Publish(ctx, eventbus.Event{
		Type:    eventbus.EventActivityLogCreated,
		Payload: map[string]any{"Name": "User logged in"},
	})
	if m := readMsg(t, adminConn); m.MessageType != "ActivityLogEntry" {
		t.Fatalf("expected ActivityLogEntry on adminConn, got %+v", m)
	}

	// 4. Unknown UserDataChanged payload must ONLY go to admin/API-key, NOT to ordinary user bob!
	bus.Publish(ctx, eventbus.Event{
		Type:    eventbus.EventUserDataChanged,
		Payload: "unknown-payload",
	})
	if m := readMsg(t, adminConn); m.MessageType != "UserDataChanged" {
		t.Fatalf("expected admin to receive unknown UserDataChanged, got %+v", m)
	}
	// Send KeepAlive on bobConn1: bob must get KeepAlive (not ActivityLogEntry and not UserDataChanged).
	_ = wsjson.Write(ctx, bobConn1, Message{MessageType: "KeepAlive"})
	if m := readMsg(t, bobConn1); m.MessageType != "KeepAlive" {
		t.Fatalf("ordinary user must not receive ActivityLogEntry or unknown UserDataChanged; got %+v", m)
	}

	// 5. DisconnectToken disconnects only bobRes1
	hub.DisconnectToken(bobRes1.AccessToken)
	if hub.ClientCount() != 2 {
		t.Fatalf("expected 2 clients after DisconnectToken, got %d", hub.ClientCount())
	}

	// 6. DisconnectUser disconnects remaining bob connection, leaving admin connected
	hub.DisconnectUser(bobUser.ID)
	if hub.ClientCount() != 1 {
		t.Fatalf("expected 1 client (admin) after DisconnectUser(bob), got %d", hub.ClientCount())
	}
	hub.DisconnectUser(adminUser.ID)
	if hub.ClientCount() != 0 {
		t.Fatalf("expected 0 clients after DisconnectUser(admin), got %d", hub.ClientCount())
	}
}

func TestHubMaxClientsAndReadTimeout(t *testing.T) {
	hub, authSvc, _, _, srv := setupWSHub(t)
	hub.maxClients = 1
	hub.readTimeout = 150 * time.Millisecond

	ctx := context.Background()
	_, _ = authSvc.CreateUser(ctx, "u1", "pw", false)
	res, _ := authSvc.AuthenticateByName(ctx, "u1", "pw", "", auth.ClientInfo{DeviceID: "d1"})
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	conn1, _, err := websocket.Dial(ctx, wsURL+"/embywebsocket?api_key="+res.AccessToken, nil)
	if err != nil {
		t.Fatalf("dial conn1: %v", err)
	}
	defer conn1.Close(websocket.StatusNormalClosure, "")
	_ = readMsg(t, conn1)

	// Second connection exceeds maxClients=1 -> 503 Service Unavailable
	_, resp, err := websocket.Dial(ctx, wsURL+"/embywebsocket?api_key="+res.AccessToken, nil)
	if err == nil {
		t.Fatal("expected dial to fail when maxClients reached")
	}
	if resp == nil || resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 Service Unavailable when maxClients reached, got resp=%v err=%v", resp, err)
	}

	// Wait past readTimeout without sending KeepAlive -> server closes idle client
	deadline := time.Now().Add(2 * time.Second)
	for hub.ClientCount() > 0 && time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
	}
	if hub.ClientCount() != 0 {
		t.Fatalf("expected idle client to be disconnected after readTimeout, ClientCount=%d", hub.ClientCount())
	}
}
