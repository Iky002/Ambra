package server

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/ambra-media/ambra/internal/api/ws"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
)

func TestParseHTTPPortAndPendingRestoreOrphanSidecarsAndCloseWSHub(t *testing.T) {
	cases := map[string]int{
		"":                8096,
		":8096":           8096,
		":9090":           9090,
		"0.0.0.0:18080":   18080,
		"[::]:7777":       7777,
		"invalid":         8096,
		":0":              8096,
		":99999":          8096,
		":notaport":       8096,
	}
	for addr, want := range cases {
		if got := parseHTTPPort(addr); got != want {
			t.Errorf("parseHTTPPort(%q) = %d, want %d", addr, got, want)
		}
	}

	// Pending restore when main ambra.db does NOT exist, but stale -wal/-shm sidecars exist -> must be backed up/cleared!
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "ambra.db")
	restorePath := filepath.Join(dir, "ambra.db.restore")
	validHeader := append([]byte("SQLite format 3\x00"), make([]byte, 100)...)
	if err := os.WriteFile(restorePath, validHeader, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dbPath+"-wal", []byte("stale-wal"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dbPath+"-shm", []byte("stale-shm"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := applyPendingDatabaseRestore(dir, dbPath); err != nil {
		t.Fatalf("applyPendingDatabaseRestore: %v", err)
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("expected promoted dbPath to exist: %v", err)
	}
	for _, suf := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(dbPath + suf); !os.IsNotExist(err) {
			t.Fatalf("expected stale sidecar %s removed/moved, stat err=%v", suf, err)
		}
		if _, err := os.Stat(dbPath + ".pre-restore.bak" + suf); err != nil {
			t.Fatalf("expected stale sidecar %s backed up, err=%v", suf, err)
		}
	}

	// Server.Close() closes active WebSocket connections via WSHub.Close().
	dataDir := t.TempDir()
	cfg, err := config.Load(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.HTTPAddr = ":18096"
	cfg.EnableDLNA = true
	ctx := context.Background()
	srv, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	if srv.SSDP == nil {
		t.Fatal("expected SSDP broadcaster initialized")
	}

	u, err := srv.Auth.CreateUser(ctx, "wsuser", "pw", false)
	if err != nil {
		t.Fatal(err)
	}
	res, err := srv.Auth.AuthenticateByName(ctx, u.Name, "pw", "", auth.ClientInfo{DeviceID: "d-close"})
	if err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/embywebsocket?api_key=" + res.AccessToken
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket.Dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	var hello ws.Message
	rctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	if err := wsjson.Read(rctx, conn, &hello); err != nil {
		cancel()
		t.Fatalf("read ForceKeepAlive: %v", err)
	}
	cancel()

	if err := srv.Close(); err != nil {
		t.Fatalf("srv.Close: %v", err)
	}
	if srv.WSHub.ClientCount() != 0 {
		t.Fatalf("expected 0 WS clients after srv.Close(), got %d", srv.WSHub.ClientCount())
	}
}
