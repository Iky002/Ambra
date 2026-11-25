package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
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
	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/pkg/netutil"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestTrustedProxiesUnconditionalNoneAndReset(t *testing.T) {
	t.Cleanup(func() { netutil.SetTrustedProxies(nil) })

	// 1. TrustedProxies = ["none"] disables loopback proxy trust.
	cfgNone, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfgNone.EnableDLNA = false
	cfgNone.TrustedProxies = []string{"none"}

	srv1, err := New(context.Background(), cfgNone)
	if err != nil {
		t.Fatalf("New with TrustedProxies=none: %v", err)
	}
	if netutil.IsTrustedProxyIP(netip.MustParseAddr("127.0.0.1")) {
		_ = srv1.Close()
		t.Fatal("expected TrustedProxies=[none] to disable loopback proxy trust")
	}
	_ = srv1.Close()

	// 2. Empty TrustedProxies unconditionally resets netutil back to default (loopback trusted).
	cfgDefault, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfgDefault.EnableDLNA = false
	cfgDefault.TrustedProxies = nil

	srv2, err := New(context.Background(), cfgDefault)
	if err != nil {
		t.Fatalf("New with TrustedProxies=nil: %v", err)
	}
	defer srv2.Close()
	if !netutil.IsTrustedProxyIP(netip.MustParseAddr("127.0.0.1")) {
		t.Fatal("expected empty TrustedProxies to reset netutil back to default loopback trust")
	}
}

func TestBuildWebHandlerSecurityHeadersAndStaticRouteBlocking(t *testing.T) {
	extWeb := t.TempDir()
	_ = os.MkdirAll(filepath.Join(extWeb, "assets"), 0o755)
	_ = os.MkdirAll(filepath.Join(extWeb, "ffmpeg"), 0o755)
	_ = os.WriteFile(filepath.Join(extWeb, "index.html"), []byte("<html>spa-root</html>"), 0o644)
	_ = os.WriteFile(filepath.Join(extWeb, "_headers"), []byte("/*\n  X-Test: leak"), 0o644)
	_ = os.WriteFile(filepath.Join(extWeb, "_redirects"), []byte("/* /index.html 200"), 0o644)
	_ = os.WriteFile(filepath.Join(extWeb, "assets", "main.js"), []byte("console.log('ok')"), 0o644)

	h := buildWebHandler(extWeb)

	assertSecHeaders := func(t *testing.T, rec *httptest.ResponseRecorder, path string) {
		t.Helper()
		if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Fatalf("%s: X-Content-Type-Options=%q", path, got)
		}
		if got := rec.Header().Get("X-Frame-Options"); got != "SAMEORIGIN" {
			t.Fatalf("%s: X-Frame-Options=%q", path, got)
		}
		if got := rec.Header().Get("Referrer-Policy"); got != "strict-origin-when-cross-origin" {
			t.Fatalf("%s: Referrer-Policy=%q", path, got)
		}
		csp := rec.Header().Get("Content-Security-Policy")
		for _, want := range []string{"default-src 'self'", "'wasm-unsafe-eval'", "frame-ancestors 'self'", "object-src 'none'"} {
			if !strings.Contains(csp, want) {
				t.Fatalf("%s: CSP missing %q: %q", path, want, csp)
			}
		}
	}

	// /_headers and /_redirects must return 404 even when present on disk.
	for _, blocked := range []string{"/_headers", "/_redirects"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, blocked, nil))
		assertSecHeaders(t, rec, blocked)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for %s, got %d body=%q", blocked, rec.Code, rec.Body.String())
		}
	}

	// Existing static asset returns 200 + security headers.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/main.js", nil))
	assertSecHeaders(t, rec, "/assets/main.js")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "console.log") {
		t.Fatalf("expected 200 for existing asset, got %d", rec.Code)
	}

	// Missing /assets/* and /ffmpeg/* files must return 404 (NEVER SPA fallback 200 index.html).
	for _, missing := range []string{"/assets/nonexistent.js", "/assets/chunks/missing.css", "/ffmpeg/ffmpeg-core.wasm"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, missing, nil))
		assertSecHeaders(t, rec, missing)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for missing static file %s, got %d body=%q", missing, rec.Code, rec.Body.String())
		}
	}

	// Regular SPA routes still fall back to index.html with security headers.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/movies/12345", nil))
	assertSecHeaders(t, rec, "/movies/12345")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "spa-root") {
		t.Fatalf("expected 200 SPA fallback for /movies/12345, got %d body=%q", rec.Code, rec.Body.String())
	}
}

func TestServerWiringWSOriginsLibraryFilterAndSessionRBAC(t *testing.T) {
	t.Cleanup(func() { netutil.SetTrustedProxies(nil) })

	dataDir := t.TempDir()
	cfg, err := config.Load(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.EnableDLNA = false
	cfg.AllowedOrigins = []string{"https://app.ambra.local"}

	ctx := context.Background()
	srv, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	defer srv.Close()

	userRepo := sqlite.NewUserRepo(srv.Store)
	libRepo := sqlite.NewLibraryRepo(srv.Store)

	libKids, err := libRepo.CreateLibrary(ctx, "Kids", "movies", []string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	libAdults, err := libRepo.CreateLibrary(ctx, "Adults", "movies", []string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}

	itemKid := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libKids.ID, ParentID: libKids.ID, Type: "Movie", Name: "Cartoon", OfficialRating: "G"}
	itemAdult := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libAdults.ID, ParentID: libAdults.ID, Type: "Movie", Name: "Thriller", OfficialRating: "R"}
	itemRated := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libKids.ID, ParentID: libKids.ID, Type: "Movie", Name: "Misplaced R", OfficialRating: "R"}
	for _, it := range []*sqlite.ItemRecord{itemKid, itemAdult, itemRated} {
		if err := libRepo.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}

	kid, err := srv.Auth.CreateUser(ctx, "kid", "pw", false)
	if err != nil {
		t.Fatal(err)
	}
	maxPG := 5
	kid.Policy.EnableAllFolders = false
	kid.Policy.EnabledFolders = []string{libKids.ID}
	kid.Policy.MaxParentalRating = &maxPG
	kid.Policy.EnableLiveTvAccess = false
	if err := userRepo.UpdateUserPolicy(ctx, kid.ID, kid.Policy); err != nil {
		t.Fatal(err)
	}
	kid, _ = userRepo.GetUserByID(ctx, kid.ID)
	kidAuth, err := srv.Auth.AuthenticateByName(ctx, "kid", "pw", "", auth.ClientInfo{DeviceID: "kid-ipad"})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Session Manager RBAC wired via server.New.
	client := auth.ClientInfo{DeviceID: "kid-ipad"}
	if err := srv.Sessions.ReportPlaybackStart(ctx, kid, client, session.PlaybackStartRequest{ItemId: itemAdult.ID}); !errors.Is(err, session.ErrItemAccessDenied) {
		t.Fatalf("expected unauthorized library item denied by wired checker, got %v", err)
	}
	if err := srv.Sessions.ReportPlaybackStart(ctx, kid, client, session.PlaybackStartRequest{ItemId: itemRated.ID}); !errors.Is(err, session.ErrItemAccessDenied) {
		t.Fatalf("expected parental-blocked item denied by wired checker, got %v", err)
	}
	if err := srv.Sessions.ReportPlaybackStart(ctx, kid, client, session.PlaybackStartRequest{ItemId: itemKid.ID}); err != nil {
		t.Fatalf("expected allowed item permitted by wired checker, got %v", err)
	}

	// 2. WSHub AllowedOrigins + LibraryChangeFilter wired via server.New.
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")

	badHdr := http.Header{}
	badHdr.Set("Origin", "https://evil.example")
	if _, resp, err := websocket.Dial(ctx, wsURL+"/embywebsocket?api_key="+kidAuth.AccessToken, &websocket.DialOptions{HTTPHeader: badHdr}); err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for disallowed WS origin, resp=%v err=%v", resp, err)
	}

	okHdr := http.Header{}
	okHdr.Set("Origin", "https://app.ambra.local")
	conn, _, err := websocket.Dial(ctx, wsURL+"/embywebsocket?api_key="+kidAuth.AccessToken, &websocket.DialOptions{HTTPHeader: okHdr})
	if err != nil {
		t.Fatalf("dial with allowed origin failed: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	readWS := func() ws.Message {
		rctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		var msg ws.Message
		if err := wsjson.Read(rctx, conn, &msg); err != nil {
			t.Fatalf("wsjson.Read: %v", err)
		}
		return msg
	}
	_ = readWS() // ForceKeepAlive

	// Unauthorized library & DVR (EnableLiveTvAccess=false) must be dropped.
	srv.Bus.Publish(ctx, eventbus.Event{
		Type:    eventbus.EventLibraryChanged,
		Payload: map[string]any{"LibraryId": libAdults.ID, "ItemsAdded": 3},
	})
	srv.Bus.Publish(ctx, eventbus.Event{
		Type:    eventbus.EventLibraryChanged,
		Payload: map[string]any{"source": "dvr", "output_path": "/data/dvr/rec.ts"},
	})
	// Explicit item ID list containing both allowed and blocked IDs -> only allowed ID survives.
	srv.Bus.Publish(ctx, eventbus.Event{
		Type: eventbus.EventLibraryChanged,
		Payload: map[string]any{
			"LibraryId":  libKids.ID,
			"ItemsAdded": []string{itemKid.ID, itemAdult.ID, itemRated.ID},
		},
	})

	msg := readWS()
	if msg.MessageType != "LibraryChanged" {
		t.Fatalf("expected filtered LibraryChanged, got %+v", msg)
	}
	data, _ := msg.Data.(map[string]any)
	added, _ := data["ItemsAdded"].([]any)
	if len(added) != 1 || added[0] != itemKid.ID {
		t.Fatalf("expected only itemKid.ID in filtered ItemsAdded, got %#v", data)
	}

	// Scan summary on allowed library for parental-restricted user -> emits LibraryId & Phase without leaking exact counts.
	srv.Bus.Publish(ctx, eventbus.Event{
		Type: eventbus.EventLibraryChanged,
		Payload: map[string]any{
			"LibraryId":  libKids.ID,
			"ItemsAdded": 5,
			"Phase":      "complete",
		},
	})
	msg2 := readWS()
	data2, _ := msg2.Data.(map[string]any)
	if data2["LibraryId"] != libKids.ID || data2["Phase"] != "complete" || data2["ItemsAdded"] != nil {
		t.Fatalf("expected sanitized scan summary for parental-restricted user, got %#v", data2)
	}

	// Direct unit coverage of buildSessionItemAccessChecker and buildWSLibraryChangeFilter edge branches.
	checker := buildSessionItemAccessChecker(libRepo)
	if !checker(ctx, kid, "") {
		t.Fatal("empty itemID should be allowed")
	}
	if checker(ctx, kid, guid.New()) {
		t.Fatal("non-existent itemID should be denied")
	}
	adminUser, err := srv.Auth.CreateUser(ctx, "adm2", "pw", true)
	if err != nil {
		t.Fatal(err)
	}
	if !checker(ctx, adminUser, itemAdult.ID) {
		t.Fatal("admin should access all existing items")
	}
	if !userCanAccessLibrary(kid, "collections") || !userCanAccessLibrary(kid, "playlists") || !userCanAccessLibrary(kid, "") {
		t.Fatal("collections/playlists/empty library should be accessible")
	}

	filter := buildWSLibraryChangeFilter(userRepo, libRepo)
	if _, ok := filter(ctx, "", map[string]any{"LibraryId": libKids.ID}); ok {
		t.Fatal("empty userID should be rejected")
	}
	if _, ok := filter(ctx, guid.New(), map[string]any{"LibraryId": libKids.ID}); ok {
		t.Fatal("unknown userID should be rejected")
	}
	if out, ok := filter(ctx, adminUser.ID, map[string]any{"LibraryId": libAdults.ID}); !ok || out.(map[string]any)["LibraryId"] != libAdults.ID {
		t.Fatalf("admin should receive unmodified payload, got %#v ok=%v", out, ok)
	}
	if out, ok := filter(ctx, kid.ID, "non-map-payload"); !ok || out.(map[string]any)["LibraryChanged"] != true {
		t.Fatalf("expected safe refresh signal for non-map payload, got %#v ok=%v", out, ok)
	}
	if _, ok := filter(ctx, kid.ID, map[string]any{"output_path": "/secret/path.ts"}); ok {
		t.Fatal("output_path event should be blocked when EnableLiveTvAccess=false")
	}
	if _, ok := filter(ctx, kid.ID, map[string]any{"library_id": libAdults.ID}); ok {
		t.Fatal("lowercase library_id for unauthorized library should be blocked")
	}
	if _, ok := filter(ctx, kid.ID, map[string]any{"LibraryId": libKids.ID, "ItemsAdded": []string{itemAdult.ID}}); ok {
		t.Fatal("event whose item IDs are all filtered out should be skipped")
	}
	if out, ok := filter(ctx, kid.ID, map[string]any{"Source": "rankings"}); !ok || out.(map[string]any)["LibraryChanged"] != true {
		t.Fatalf("expected safe refresh signal for generic map payload, got %#v ok=%v", out, ok)
	}

	// Non-admin with library restriction ONLY (no parental/tag restriction) and EnableLiveTvAccess=true.
	tvUser, err := srv.Auth.CreateUser(ctx, "tvuser", "pw", false)
	if err != nil {
		t.Fatal(err)
	}
	tvUser.Policy.EnableAllFolders = false
	tvUser.Policy.EnabledFolders = []string{libKids.ID}
	tvUser.Policy.EnableLiveTvAccess = true
	if err := userRepo.UpdateUserPolicy(ctx, tvUser.ID, tvUser.Policy); err != nil {
		t.Fatal(err)
	}
	if out, ok := filter(ctx, tvUser.ID, map[string]any{"source": "dvr", "output_path": "/secret/dvr.ts"}); !ok || out.(map[string]any)["Source"] != "dvr" || out.(map[string]any)["output_path"] != nil {
		t.Fatalf("expected sanitized DVR event for liveTV user, got %#v ok=%v", out, ok)
	}
	if out, ok := filter(ctx, tvUser.ID, map[string]any{"output_path": "/secret/other.ts"}); !ok || out.(map[string]any)["output_path"] != nil {
		t.Fatalf("expected output_path stripped for liveTV user, got %#v ok=%v", out, ok)
	}
	scanSummary := map[string]any{"LibraryId": libKids.ID, "ItemsAdded": 2, "Phase": "complete"}
	if out, ok := filter(ctx, tvUser.ID, scanSummary); !ok || out.(map[string]any)["ItemsAdded"] != 2 {
		t.Fatalf("expected full scan summary for library-only restricted user on allowed library, got %#v ok=%v", out, ok)
	}
}
