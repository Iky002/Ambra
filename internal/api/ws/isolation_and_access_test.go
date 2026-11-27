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

func TestHubUserScopedDeviceIsolationAndDisconnectExceptToken(t *testing.T) {
	hub, authSvc, _, _, srv := setupWSHub(t)
	ctx := context.Background()

	alice, err := authSvc.CreateUser(ctx, "alice", "pw", false)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := authSvc.CreateUser(ctx, "bob", "pw", false)
	if err != nil {
		t.Fatal(err)
	}

	// Both users authenticate from a device sharing the exact same DeviceID.
	aliceRes1, err := authSvc.AuthenticateByName(ctx, "alice", "pw", "", auth.ClientInfo{DeviceID: "shared-tv"})
	if err != nil {
		t.Fatal(err)
	}
	aliceRes2, err := authSvc.AuthenticateByName(ctx, "alice", "pw", "", auth.ClientInfo{DeviceID: "alice-phone"})
	if err != nil {
		t.Fatal(err)
	}
	bobRes, err := authSvc.AuthenticateByName(ctx, "bob", "pw", "", auth.ClientInfo{DeviceID: "shared-tv"})
	if err != nil {
		t.Fatal(err)
	}

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	aliceSharedConn, _, err := websocket.Dial(ctx, wsURL+"/embywebsocket?api_key="+aliceRes1.AccessToken, nil)
	if err != nil {
		t.Fatalf("dial alice shared: %v", err)
	}
	defer aliceSharedConn.Close(websocket.StatusNormalClosure, "")
	_ = readMsg(t, aliceSharedConn)

	alicePhoneConn, _, err := websocket.Dial(ctx, wsURL+"/embywebsocket?api_key="+aliceRes2.AccessToken, nil)
	if err != nil {
		t.Fatalf("dial alice phone: %v", err)
	}
	defer alicePhoneConn.Close(websocket.StatusNormalClosure, "")
	_ = readMsg(t, alicePhoneConn)

	bobSharedConn, _, err := websocket.Dial(ctx, wsURL+"/embywebsocket?api_key="+bobRes.AccessToken, nil)
	if err != nil {
		t.Fatalf("dial bob shared: %v", err)
	}
	defer bobSharedConn.Close(websocket.StatusNormalClosure, "")
	_ = readMsg(t, bobSharedConn)

	// 1. SendToUserDevice targets ONLY (alice, shared-tv); Bob on same DeviceID must NOT receive it.
	hub.SendToUserDevice("", "shared-tv", Message{MessageType: "Ignored"})
	hub.SendToUserDevice(alice.ID, "", Message{MessageType: "Ignored"})
	hub.SendToUserDevice(alice.ID, "shared-tv", Message{MessageType: "Play", Data: "alice-only"})

	if m := readMsg(t, aliceSharedConn); m.MessageType != "Play" || m.Data != "alice-only" {
		t.Fatalf("expected Play alice-only on aliceSharedConn, got %+v", m)
	}
	// Verify Bob did not receive alice-only by sending a KeepAlive and reading it first.
	if err := wsjson.Write(ctx, bobSharedConn, Message{MessageType: "KeepAlive"}); err != nil {
		t.Fatal(err)
	}
	if m := readMsg(t, bobSharedConn); m.MessageType != "KeepAlive" {
		t.Fatalf("bob on shared DeviceID received cross-user message: %+v", m)
	}

	// 2. DisconnectUserExceptToken keeps aliceRes1 and disconnects aliceRes2 without touching Bob.
	hub.DisconnectUserExceptToken("", aliceRes1.AccessToken)
	hub.DisconnectUserExceptToken(alice.ID, aliceRes1.AccessToken)
	if hub.ClientCount() != 2 {
		t.Fatalf("expected 2 clients remaining (alice shared + bob shared), got %d", hub.ClientCount())
	}
	// Verify alicePhoneConn is closed.
	rctx, rcancel := context.WithTimeout(ctx, 500*time.Millisecond)
	var dummy Message
	if err := wsjson.Read(rctx, alicePhoneConn, &dummy); err == nil {
		rcancel()
		t.Fatalf("expected alicePhoneConn to be disconnected")
	}
	rcancel()

	// 3. DisconnectUserDevice(alice, shared-tv) disconnects ONLY Alice; Bob stays connected.
	hub.DisconnectUserDevice("", "shared-tv")
	hub.DisconnectUserDevice(alice.ID, "")
	hub.DisconnectUserDevice(alice.ID, "shared-tv")
	if hub.ClientCount() != 1 {
		t.Fatalf("expected 1 client remaining (bob), got %d", hub.ClientCount())
	}
	if err := wsjson.Write(ctx, bobSharedConn, Message{MessageType: "KeepAlive"}); err != nil {
		t.Fatalf("bob connection should remain open: %v", err)
	}
	if m := readMsg(t, bobSharedConn); m.MessageType != "KeepAlive" {
		t.Fatalf("expected KeepAlive on bobSharedConn, got %+v", m)
	}
	hub.DisconnectUserDevice(bob.ID, "shared-tv")
	if hub.ClientCount() != 0 {
		t.Fatalf("expected 0 clients remaining, got %d", hub.ClientCount())
	}
}

func TestHubRemoteAccessOriginAllowlistAndPerUserCap(t *testing.T) {
	dir := t.TempDir()
	store, err := sqlite.Open(filepath.Join(dir, "ws_v0104.db"))
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
	hub.maxClientsPerUser = 2

	ctx := context.Background()
	lanUser, err := authSvc.CreateUser(ctx, "lanonly", "pw", false)
	if err != nil {
		t.Fatal(err)
	}
	lanUser.Policy.EnableRemoteAccess = false
	if err := repo.UpdateUserPolicy(ctx, lanUser.ID, lanUser.Policy); err != nil {
		t.Fatal(err)
	}
	lanAuth, err := authSvc.AuthenticateByName(ctx, "lanonly", "pw", "", auth.ClientInfo{DeviceID: "d-lan"})
	if err != nil {
		t.Fatal(err)
	}

	otherUser, err := authSvc.CreateUser(ctx, "other", "pw", false)
	if err != nil {
		t.Fatal(err)
	}
	otherAuth, err := authSvc.AuthenticateByName(ctx, "other", "pw", "", auth.ClientInfo{DeviceID: "d-other"})
	if err != nil {
		t.Fatal(err)
	}
	apiKey, err := authSvc.CreateAPIKey(ctx, "Automation")
	if err != nil {
		t.Fatal(err)
	}

	// 1. RemoteAccess disabled user from public IP -> 403 Forbidden BEFORE websocket.Accept.
	reqPublic := httptest.NewRequest(http.MethodGet, "/embywebsocket?api_key="+lanAuth.AccessToken, nil)
	reqPublic.RemoteAddr = "198.51.100.20:43210"
	recPublic := httptest.NewRecorder()
	hub.ServeHTTP(recPublic, reqPublic)
	if recPublic.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for public IP with EnableRemoteAccess=false, got %d", recPublic.Code)
	}

	// API key from public IP is not rejected by EnableRemoteAccess (proceeds to upgrade check).
	reqAPI := httptest.NewRequest(http.MethodGet, "/embywebsocket?api_key="+apiKey.Token, nil)
	reqAPI.RemoteAddr = "198.51.100.20:43210"
	recAPI := httptest.NewRecorder()
	hub.ServeHTTP(recAPI, reqAPI)
	if recAPI.Code == http.StatusForbidden || recAPI.Code == http.StatusUnauthorized {
		t.Fatalf("API key should bypass EnableRemoteAccess check, got %d", recAPI.Code)
	}

	// 2. Origin allowlist enforcement.
	hub.SetAllowedOrigins([]string{" https://App.Ambra.Local/ ", ""})
	r := chi.NewRouter()
	hub.RegisterRoutes(r)
	srv := httptest.NewServer(r)
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	// Disallowed Origin -> 403 Forbidden.
	badHdr := http.Header{}
	badHdr.Set("Origin", "https://evil.example")
	_, badResp, err := websocket.Dial(ctx, wsURL+"/embywebsocket?api_key="+lanAuth.AccessToken, &websocket.DialOptions{HTTPHeader: badHdr})
	if err == nil || badResp == nil || badResp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for disallowed origin, resp=%v err=%v", badResp, err)
	}

	// Allowed Origin (case-insensitive, trailing slash normalized) -> succeeds (1st connection for lanUser).
	okHdr := http.Header{}
	okHdr.Set("Origin", "https://app.ambra.local")
	c1, _, err := websocket.Dial(ctx, wsURL+"/embywebsocket?api_key="+lanAuth.AccessToken, &websocket.DialOptions{HTTPHeader: okHdr})
	if err != nil {
		t.Fatalf("dial with allowed origin failed: %v", err)
	}
	defer c1.Close(websocket.StatusNormalClosure, "")
	_ = readMsg(t, c1)

	// Empty Origin (native client) -> succeeds (2nd connection for lanUser).
	c2, _, err := websocket.Dial(ctx, wsURL+"/embywebsocket?api_key="+lanAuth.AccessToken, nil)
	if err != nil {
		t.Fatalf("dial with empty origin failed: %v", err)
	}
	defer c2.Close(websocket.StatusNormalClosure, "")
	_ = readMsg(t, c2)

	// 3. Per-user cap (maxClientsPerUser=2): 3rd connection for lanUser -> 429 Too Many Requests.
	_, capResp, err := websocket.Dial(ctx, wsURL+"/embywebsocket?api_key="+lanAuth.AccessToken, nil)
	if err == nil || capResp == nil || capResp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 when per-user WS limit reached, resp=%v err=%v", capResp, err)
	}

	// Another user and API keys can still connect.
	cOther, _, err := websocket.Dial(ctx, wsURL+"/embywebsocket?api_key="+otherAuth.AccessToken, nil)
	if err != nil {
		t.Fatalf("other user should not be blocked by lanUser cap: %v", err)
	}
	defer cOther.Close(websocket.StatusNormalClosure, "")
	_ = readMsg(t, cOther)
	_ = otherUser

	cAPI1, _, err := websocket.Dial(ctx, wsURL+"/embywebsocket?api_key="+apiKey.Token, nil)
	if err != nil {
		t.Fatalf("apikey 1 dial failed: %v", err)
	}
	defer cAPI1.Close(websocket.StatusNormalClosure, "")
	_ = readMsg(t, cAPI1)

	// Wildcard origin allows arbitrary Origin.
	hub.SetAllowedOrigins([]string{"*"})
	cWild, _, err := websocket.Dial(ctx, wsURL+"/embywebsocket?api_key="+apiKey.Token, &websocket.DialOptions{HTTPHeader: badHdr})
	if err != nil {
		t.Fatalf("wildcard origin should allow any origin: %v", err)
	}
	defer cWild.Close(websocket.StatusNormalClosure, "")
	_ = readMsg(t, cWild)

	// Verify tryRegister atomic per-user limit guard directly.
	if hub.tryRegister(&clientConn{id: "extra", userID: lanUser.ID}, 100, 2) {
		t.Fatal("tryRegister should reject connection exceeding perUserLimit")
	}
}

func TestHubLibraryChangedFilteringForRestrictedUsers(t *testing.T) {
	dir := t.TempDir()
	store, err := sqlite.Open(filepath.Join(dir, "ws_libfilter.db"))
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
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	ctx := context.Background()
	adminUser, _ := authSvc.CreateUser(ctx, "admin", "pw", true)
	kidUser, _ := authSvc.CreateUser(ctx, "kid", "pw", false)
	kidUser.Policy.EnableAllFolders = false
	kidUser.Policy.EnabledFolders = []string{"lib-kids"}
	if err := repo.UpdateUserPolicy(ctx, kidUser.ID, kidUser.Policy); err != nil {
		t.Fatal(err)
	}
	_ = adminUser

	adminAuth, _ := authSvc.AuthenticateByName(ctx, "admin", "pw", "", auth.ClientInfo{DeviceID: "d-adm"})
	kidAuth, _ := authSvc.AuthenticateByName(ctx, "kid", "pw", "", auth.ClientInfo{DeviceID: "d-kid"})

	adminConn, _, err := websocket.Dial(ctx, wsURL+"/embywebsocket?api_key="+adminAuth.AccessToken, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer adminConn.Close(websocket.StatusNormalClosure, "")
	_ = readMsg(t, adminConn)

	kidConn, _, err := websocket.Dial(ctx, wsURL+"/embywebsocket?api_key="+kidAuth.AccessToken, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer kidConn.Close(websocket.StatusNormalClosure, "")
	_ = readMsg(t, kidConn)

	// 1. Default built-in fallback filter (no custom filter set):
	// Restricted library event -> admin gets full payload, kid gets nothing.
	bus.Publish(ctx, eventbus.Event{
		Type: eventbus.EventLibraryChanged,
		Payload: map[string]any{
			"LibraryId":  "lib-adults",
			"ItemsAdded": []string{"secret-item-1"},
		},
	})
	if m := readMsg(t, adminConn); m.MessageType != "LibraryChanged" {
		t.Fatalf("expected admin to receive LibraryChanged, got %+v", m)
	}
	if err := wsjson.Write(ctx, kidConn, Message{MessageType: "KeepAlive"}); err != nil {
		t.Fatal(err)
	}
	if m := readMsg(t, kidConn); m.MessageType != "KeepAlive" {
		t.Fatalf("restricted user leaked unauthorized LibraryChanged event: %+v", m)
	}

	// Allowed library event with default fallback -> strips raw item IDs, sends safe LibraryId summary.
	bus.Publish(ctx, eventbus.Event{
		Type: eventbus.EventLibraryChanged,
		Payload: map[string]any{
			"LibraryId":  "lib-kids",
			"ItemsAdded": []string{"kid-item-1"},
		},
	})
	_ = readMsg(t, adminConn)
	km := readMsg(t, kidConn)
	if km.MessageType != "LibraryChanged" {
		t.Fatalf("expected LibraryChanged for allowed library, got %+v", km)
	}
	kmData, _ := km.Data.(map[string]any)
	if kmData["LibraryId"] != "libkids" || kmData["ItemsAdded"] != nil {
		t.Fatalf("expected safe summary without raw ItemsAdded, got %#v", km.Data)
	}

	// Non-library payload with default fallback -> safe refresh signal without sensitive fields.
	bus.Publish(ctx, eventbus.Event{
		Type:    eventbus.EventLibraryChanged,
		Payload: map[string]any{"output_path": "/secret/dvr/file.ts"},
	})
	_ = readMsg(t, adminConn)
	km2 := readMsg(t, kidConn)
	km2Data, _ := km2.Data.(map[string]any)
	if km2Data["output_path"] != nil {
		t.Fatalf("expected output_path stripped for restricted user, got %#v", km2Data)
	}

	// 2. Custom SetLibraryChangeFilter callback.
	hub.SetLibraryChangeFilter(func(_ context.Context, userID string, payload any) (any, bool) {
		m, _ := payload.(map[string]any)
		if m["LibraryId"] == "lib-adults" {
			return nil, false
		}
		return map[string]any{"LibraryId": m["LibraryId"], "FilteredFor": userID}, true
	})

	bus.Publish(ctx, eventbus.Event{
		Type:    eventbus.EventLibraryChanged,
		Payload: map[string]any{"LibraryId": "lib-adults"},
	})
	_ = readMsg(t, adminConn)
	_ = wsjson.Write(ctx, kidConn, Message{MessageType: "KeepAlive"})
	if m := readMsg(t, kidConn); m.MessageType != "KeepAlive" {
		t.Fatalf("custom filter returned false, kid must not receive event: %+v", m)
	}

	bus.Publish(ctx, eventbus.Event{
		Type:    eventbus.EventLibraryChanged,
		Payload: map[string]any{"LibraryId": "lib-kids"},
	})
	_ = readMsg(t, adminConn)
	km3 := readMsg(t, kidConn)
	km3Data, _ := km3.Data.(map[string]any)
	if km3Data["FilteredFor"] != kidUser.ID {
		t.Fatalf("expected custom filtered payload, got %#v", km3Data)
	}
}
