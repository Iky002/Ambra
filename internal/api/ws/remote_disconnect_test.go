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

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/pkg/netutil"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestDisconnectRemoteClients(t *testing.T) {
	dir := t.TempDir()
	store, err := sqlite.Open(filepath.Join(dir, "ws_remote.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	cfg := config.Default(dir)
	userRepo := sqlite.NewUserRepo(store)
	authSvc := auth.NewService(userRepo, cfg)
	bus := eventbus.New()
	sessMgr := session.NewManager(userRepo, cfg, bus)
	hub := NewHub(authSvc, sessMgr, bus)
	hub.SetConfig(cfg)
	t.Cleanup(hub.Close)

	netutil.SetTrustedProxies([]string{"127.0.0.1/32"})
	t.Cleanup(func() { netutil.SetTrustedProxies(nil) })

	ctx := context.Background()
	u, err := authSvc.CreateUser(ctx, "remoteuser", "pass1234", false)
	if err != nil {
		t.Fatal(err)
	}
	authRes, err := authSvc.AuthenticateByName(ctx, "remoteuser", "pass1234", "", auth.ClientInfo{DeviceID: "dev-1"})
	if err != nil {
		t.Fatal(err)
	}
	apiKey, err := authSvc.CreateAPIKey(ctx, "test-key")
	if err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(http.HandlerFunc(hub.ServeHTTP))
	t.Cleanup(ts.Close)
	wsBase := "ws" + strings.TrimPrefix(ts.URL, "http")

	dial := func(token, xff string) *websocket.Conn {
		t.Helper()
		hdr := http.Header{}
		if xff != "" {
			hdr.Set("X-Forwarded-For", xff)
		}
		c, _, err := websocket.Dial(ctx, wsBase+"?api_key="+token, &websocket.DialOptions{HTTPHeader: hdr})
		if err != nil {
			t.Fatalf("dial ws (xff=%s): %v", xff, err)
		}
		return c
	}

	// Connect 3 clients: WAN user, LAN user, WAN API key.
	wanUserConn := dial(authRes.AccessToken, "203.0.113.50")
	defer wanUserConn.CloseNow()
	lanUserConn := dial(authRes.AccessToken, "")
	defer lanUserConn.CloseNow()
	wanAPIKeyConn := dial(apiKey.Token, "203.0.113.51")
	defer wanAPIKeyConn.CloseNow()

	deadline := time.Now().Add(2 * time.Second)
	for hub.ClientCount() < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if hub.ClientCount() != 3 {
		t.Fatalf("expected 3 connected WS clients, got %d", hub.ClientCount())
	}

	// Disable global remote access and disconnect remote clients.
	cfg.Update(func(c *config.Config) {
		f := false
		c.EnableRemoteAccessGlobal = &f
	})
	hub.DisconnectRemoteClients()

	// WAN user client must be disconnected; LAN user and API key must remain connected.
	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	for {
		_, _, rErr := wanUserConn.Read(readCtx)
		if rErr != nil {
			break
		}
	}
	if got := hub.ClientCount(); got != 2 {
		t.Fatalf("expected 2 remaining clients (LAN user + API key), got %d", got)
	}
	_ = u
}
