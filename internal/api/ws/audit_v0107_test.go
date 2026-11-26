package ws

import (
	"context"
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

func TestHubNonAdminAlwaysFilteredAndClose(t *testing.T) {
	dir := t.TempDir()
	store, err := sqlite.Open(filepath.Join(dir, "ws_h4.db"))
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
	if _, err := authSvc.CreateUser(ctx, "admin", "pw", true); err != nil {
		t.Fatal(err)
	}
	// Regular non-admin with EnableAllFolders=true (no folder/parental restrictions!)
	regular, err := authSvc.CreateUser(ctx, "regular", "pw", false)
	if err != nil {
		t.Fatal(err)
	}
	regular.Policy.EnableLiveTvAccess = false
	if err := repo.UpdateUserPolicy(ctx, regular.ID, regular.Policy); err != nil {
		t.Fatal(err)
	}

	adminAuth, _ := authSvc.AuthenticateByName(ctx, "admin", "pw", "", auth.ClientInfo{DeviceID: "d-adm"})
	regAuth, _ := authSvc.AuthenticateByName(ctx, "regular", "pw", "", auth.ClientInfo{DeviceID: "d-reg"})

	adminConn, _, err := websocket.Dial(ctx, wsURL+"/embywebsocket?api_key="+adminAuth.AccessToken, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer adminConn.Close(websocket.StatusNormalClosure, "")
	_ = readMsg(t, adminConn)

	regConn, _, err := websocket.Dial(ctx, wsURL+"/embywebsocket?api_key="+regAuth.AccessToken, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer regConn.Close(websocket.StatusNormalClosure, "")
	_ = readMsg(t, regConn)

	// 1. Default filter: non-admin with EnableAllFolders=true and EnableLiveTvAccess=false must NOT receive DVR output_path.
	bus.Publish(ctx, eventbus.Event{
		Type:    eventbus.EventLibraryChanged,
		Payload: map[string]any{"source": "dvr", "output_path": "/secret/dvr/rec.ts"},
	})
	admMsg := readMsg(t, adminConn)
	if admData, _ := admMsg.Data.(map[string]any); admData["output_path"] != "/secret/dvr/rec.ts" {
		t.Fatalf("admin should receive full payload, got %#v", admMsg.Data)
	}
	if err := wsjson.Write(ctx, regConn, Message{MessageType: "KeepAlive"}); err != nil {
		t.Fatal(err)
	}
	if m := readMsg(t, regConn); m.MessageType != "KeepAlive" {
		t.Fatalf("unrestricted non-admin leaked DVR output_path: %+v", m)
	}

	// 2. Custom filter is ALWAYS invoked for non-admin even when EnableAllFolders=true (prevents online policy tightening bypass).
	filterCalled := false
	hub.SetLibraryChangeFilter(func(_ context.Context, userID string, payload any) (any, bool) {
		if userID == regular.ID {
			filterCalled = true
		}
		return map[string]any{"Sanitized": true}, true
	})
	bus.Publish(ctx, eventbus.Event{
		Type:    eventbus.EventLibraryChanged,
		Payload: map[string]any{"LibraryId": "lib-1", "output_path": "/internal/path.ts"},
	})
	_ = readMsg(t, adminConn)
	regMsg := readMsg(t, regConn)
	if !filterCalled {
		t.Fatal("expected libraryChangeFilter to be invoked for non-admin with EnableAllFolders=true")
	}
	if regData, _ := regMsg.Data.(map[string]any); regData["Sanitized"] != true || regData["output_path"] != nil {
		t.Fatalf("expected filtered payload for non-admin, got %#v", regMsg.Data)
	}

	// 3. defaultFilterLibraryChange direct edge cases: disabled user, DVR with LiveTV allowed, output_path with LiveTV allowed.
	if _, ok := defaultFilterLibraryChange(nil, map[string]any{}); ok {
		t.Fatal("nil user must return false")
	}
	disabledUser := &sqlite.UserRecord{IsDisabled: true}
	if _, ok := defaultFilterLibraryChange(disabledUser, map[string]any{}); ok {
		t.Fatal("disabled user must return false")
	}
	liveUser := &sqlite.UserRecord{Policy: sqlite.DefaultUserPolicy(false)}
	if out, ok := defaultFilterLibraryChange(liveUser, map[string]any{"source": "dvr", "output_path": "/x.ts"}); !ok || out.(map[string]any)["output_path"] != nil {
		t.Fatalf("expected sanitized DVR event for liveTV user, got %#v ok=%v", out, ok)
	}
	if out, ok := defaultFilterLibraryChange(liveUser, map[string]any{"output_path": "/x.ts"}); !ok || out.(map[string]any)["output_path"] != nil {
		t.Fatalf("expected sanitized output_path event for liveTV user, got %#v ok=%v", out, ok)
	}
	noLiveUser := &sqlite.UserRecord{Policy: sqlite.DefaultUserPolicy(false)}
	noLiveUser.Policy.EnableLiveTvAccess = false
	if _, ok := defaultFilterLibraryChange(noLiveUser, map[string]any{"output_path": "/x.ts"}); ok {
		t.Fatal("output_path must be dropped when EnableLiveTvAccess=false")
	}

	// 4. Hub.Close() terminates all active connections (and nil hub is safe).
	var nilHub *Hub
	nilHub.Close()
	if hub.ClientCount() != 2 {
		t.Fatalf("expected 2 clients before Close, got %d", hub.ClientCount())
	}
	hub.Close()
	if hub.ClientCount() != 0 {
		t.Fatalf("expected 0 clients after Close, got %d", hub.ClientCount())
	}
	rctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	var dummy Message
	if err := wsjson.Read(rctx, regConn, &dummy); err == nil {
		t.Fatal("expected regConn closed after Hub.Close()")
	}
}
