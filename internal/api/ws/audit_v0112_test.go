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

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestH01AndL02_WSOriginVerificationAndSendToUserAPIKeyIsolation(t *testing.T) {
	dir := t.TempDir()
	st, err := sqlite.Open(filepath.Join(dir, "ws_audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	cfg := config.Default(dir)
	userRepo := sqlite.NewUserRepo(st)
	authSvc := auth.NewService(userRepo, cfg)
	bus := eventbus.New()
	sessMgr := session.NewManager(userRepo, cfg, bus)
	hub := NewHub(authSvc, sessMgr, bus)

	ctx := context.Background()
	u1, err := authSvc.CreateUser(ctx, "alice", "alicepw", false)
	if err != nil {
		t.Fatal(err)
	}
	u1Auth, err := authSvc.AuthenticateByName(ctx, "alice", "alicepw", "", auth.ClientInfo{DeviceID: "d-alice"})
	if err != nil {
		t.Fatal(err)
	}
	apiKey, err := authSvc.CreateAPIKey(ctx, "IntegrationBot")
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(hub)
	t.Cleanup(srv.Close)
	wsBase := "ws" + strings.TrimPrefix(srv.URL, "http")

	readWithTimeout := func(c *websocket.Conn) Message {
		rctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		var m Message
		if err := wsjson.Read(rctx, c, &m); err != nil {
			t.Fatalf("ws read failed: %v", err)
		}
		return m
	}

	// 1. H-01: With default empty allowedOrigins, cross-origin Origin MUST be rejected with 403!
	crossHeaders := http.Header{}
	crossHeaders.Set("Origin", "https://evil.attacker.example")
	if conn, resp, err := websocket.Dial(ctx, wsBase+"?api_key="+u1Auth.AccessToken, &websocket.DialOptions{HTTPHeader: crossHeaders}); err == nil {
		_ = conn.CloseNow()
		t.Fatal("expected cross-origin WS upgrade to be rejected when allowedOrigins is empty")
	} else if resp != nil && resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 on cross-origin WS upgrade, got %d", resp.StatusCode)
	}

	// 2. H-01: Same-origin browser WS upgrade and empty-Origin native WS upgrade MUST succeed!
	sameHeaders := http.Header{}
	sameHeaders.Set("Origin", srv.URL)
	connAlice, _, err := websocket.Dial(ctx, wsBase+"?api_key="+u1Auth.AccessToken+"&deviceId=d-alice", &websocket.DialOptions{HTTPHeader: sameHeaders})
	if err != nil {
		t.Fatalf("expected same-origin WS upgrade to succeed, got %v", err)
	}
	defer connAlice.CloseNow()
	if initMsg := readWithTimeout(connAlice); initMsg.MessageType != "ForceKeepAlive" {
		t.Fatalf("expected initial ForceKeepAlive on connAlice, got %+v", initMsg)
	}

	connAPIKey, _, err := websocket.Dial(ctx, wsBase+"?api_key="+apiKey.Token+"&deviceId=d-bot", nil)
	if err != nil {
		t.Fatalf("expected empty-origin native WS upgrade to succeed, got %v", err)
	}
	defer connAPIKey.CloseNow()
	if initMsg := readWithTimeout(connAPIKey); initMsg.MessageType != "ForceKeepAlive" {
		t.Fatalf("expected initial ForceKeepAlive on connAPIKey, got %+v", initMsg)
	}

	// 3. Hot-update SetAllowedOrigins allows configured external origin!
	hub.SetAllowedOrigins([]string{"https://trusted.ui.example"})
	trustedHeaders := http.Header{}
	trustedHeaders.Set("Origin", "https://trusted.ui.example")
	connTrusted, _, err := websocket.Dial(ctx, wsBase+"?api_key="+u1Auth.AccessToken, &websocket.DialOptions{HTTPHeader: trustedHeaders})
	if err != nil {
		t.Fatalf("expected allowlisted WS origin to succeed, got %v", err)
	}
	_ = connTrusted.CloseNow()

	// 4. L-02: SendToUser must ONLY deliver to u1 (connAlice) and NEVER leak to API-key client (connAPIKey).
	hub.SendToUser(u1.ID, Message{MessageType: "PrivateUserEvent", Data: "secret-alice-data"})
	hub.Broadcast(Message{MessageType: "PublicMarker", Data: "after-private"})

	if m1 := readWithTimeout(connAlice); m1.MessageType != "PrivateUserEvent" {
		t.Fatalf("expected PrivateUserEvent for alice, got %+v", m1)
	}
	if m2 := readWithTimeout(connAlice); m2.MessageType != "PublicMarker" {
		t.Fatalf("expected PublicMarker second for alice, got %+v", m2)
	}
	// API-key client must receive PublicMarker directly without ever receiving PrivateUserEvent!
	if mBot := readWithTimeout(connAPIKey); mBot.MessageType != "PublicMarker" {
		t.Fatalf("API-key WS client leaked user-scoped event: got %+v", mBot)
	}
}
