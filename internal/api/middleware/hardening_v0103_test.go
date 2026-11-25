package middleware

import (
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestRequireAuthResolvedSkipAndRemoteAccessPolicy(t *testing.T) {
	dir := t.TempDir()
	store, err := sqlite.Open(filepath.Join(dir, "mw_v0103.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	cfg := config.Default(dir)
	repo := sqlite.NewUserRepo(store)
	authSvc := auth.NewService(repo, cfg)
	ctx := context.Background()

	u, err := authSvc.CreateUser(ctx, "lanonly", "pw", false)
	if err != nil {
		t.Fatal(err)
	}
	u.Policy.EnableRemoteAccess = false
	if err := repo.UpdateUserPolicy(ctx, u.ID, u.Policy); err != nil {
		t.Fatal(err)
	}
	authRes, err := authSvc.AuthenticateByName(ctx, "lanonly", "pw", "", auth.ClientInfo{DeviceID: "dev-lan"})
	if err != nil {
		t.Fatal(err)
	}
	apiKey, err := authSvc.CreateAPIKey(ctx, "Bot")
	if err != nil {
		t.Fatal(err)
	}

	mw := NewAuth(authSvc, nil)
	chain := mw.ParseAuth(mw.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	// 1. Public IP with EnableRemoteAccess=false -> 403 Forbidden
	req := httptest.NewRequest(http.MethodGet, "/Items", nil)
	req.RemoteAddr = "198.51.100.10:44321"
	req.Header.Set("X-Emby-Token", authRes.AccessToken)
	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for remote user with EnableRemoteAccess=false, got %d", rec.Code)
	}

	// 2. LAN IP with EnableRemoteAccess=false -> 200 OK
	reqLAN := httptest.NewRequest(http.MethodGet, "/Items", nil)
	reqLAN.RemoteAddr = "192.168.1.42:50000"
	reqLAN.Header.Set("X-Emby-Token", authRes.AccessToken)
	recLAN := httptest.NewRecorder()
	chain.ServeHTTP(recLAN, reqLAN)
	if recLAN.Code != http.StatusOK {
		t.Fatalf("expected 200 for LAN user with EnableRemoteAccess=false, got %d", recLAN.Code)
	}

	// 3. API Key from public IP -> 200 OK
	reqKey := httptest.NewRequest(http.MethodGet, "/Items?api_key="+apiKey.Token, nil)
	reqKey.RemoteAddr = "198.51.100.10:44321"
	recKey := httptest.NewRecorder()
	chain.ServeHTTP(recKey, reqKey)
	if recKey.Code != http.StatusOK {
		t.Fatalf("expected 200 for API key from public IP, got %d", recKey.Code)
	}

	// 4. ParseAuth marks resolved=true so RequireAuth does not double-query on invalid token.
	var sawResolved bool
	mw.ParseAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ac, ok := AuthFromContext(r.Context())
		if ok && ac.resolved {
			sawResolved = true
		}
		mw.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})).ServeHTTP(w, r)
	})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/Items?api_key=bad-token", nil))
	if !sawResolved {
		t.Fatal("expected ParseAuth to mark AuthContext.resolved = true")
	}
}

func TestExtractClientInfoDeviceIDQueryFallbackAllBranches(t *testing.T) {
	cases := []struct {
		name   string
		header string
		value  string
	}{
		{"X-Emby-Token", "X-Emby-Token", "tok-1"},
		{"X-MediaBrowser-Token", "X-MediaBrowser-Token", "tok-2"},
		{"Authorization", "Authorization", `MediaBrowser Client="Test", Token="tok-3"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/Sessions?deviceId=qs-dev-99", nil)
			req.Header.Set(tc.header, tc.value)
			info := ExtractClientInfo(req)
			if info.DeviceID != "qs-dev-99" {
				t.Fatalf("expected DeviceID=qs-dev-99 from query fallback, got %+v", info)
			}
		})
	}
}

func TestCORSAndCORSWithOriginsCredentialsPolicy(t *testing.T) {
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Default CORS reflects Origin WITHOUT Access-Control-Allow-Credentials.
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	CORS(okHandler).ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != "https://evil.example" {
		t.Fatalf("expected reflected origin, got %q", rec.Header().Get("Access-Control-Allow-Origin"))
	}
	if rec.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatalf("default CORS must NOT set Allow-Credentials, got %q", rec.Header().Get("Access-Control-Allow-Credentials"))
	}

	// Explicit allowlist sets Allow-Credentials ONLY for allowed origins.
	allowedMW := CORSWithOrigins([]string{"https://app.ambra.local/"})

	reqAllowed := httptest.NewRequest(http.MethodGet, "/ping", nil)
	reqAllowed.Header.Set("Origin", "https://app.ambra.local")
	recAllowed := httptest.NewRecorder()
	allowedMW(okHandler).ServeHTTP(recAllowed, reqAllowed)
	if recAllowed.Header().Get("Access-Control-Allow-Origin") != "https://app.ambra.local" ||
		recAllowed.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatalf("expected allowlisted origin with credentials, got origin=%q creds=%q",
			recAllowed.Header().Get("Access-Control-Allow-Origin"),
			recAllowed.Header().Get("Access-Control-Allow-Credentials"))
	}

	reqDenied := httptest.NewRequest(http.MethodGet, "/ping", nil)
	reqDenied.Header.Set("Origin", "https://untrusted.example")
	recDenied := httptest.NewRecorder()
	allowedMW(okHandler).ServeHTTP(recDenied, reqDenied)
	if recDenied.Header().Get("Access-Control-Allow-Origin") != "" ||
		recDenied.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatalf("expected unlisted origin to receive no CORS allow headers, got origin=%q creds=%q",
			recDenied.Header().Get("Access-Control-Allow-Origin"),
			recDenied.Header().Get("Access-Control-Allow-Credentials"))
	}
}

func TestGzipSkipsHeadRequest(t *testing.T) {
	h := Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "123")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodHead, "/api/info", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Header().Get("Content-Encoding") != "" {
		t.Fatalf("HEAD must not set Content-Encoding=gzip, got %q", rec.Header().Get("Content-Encoding"))
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("HEAD must have empty body (no 20-byte gzip footer), got %d bytes", rec.Body.Len())
	}
}

func TestEmbyPrefixWebLowercaseAPIAfterStripAndEscapedSlash(t *testing.T) {
	r := chi.NewRouter()
	r.Use(EmbyPrefixAndCase)

	r.Get("/web/index.html", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("web-ok"))
	})
	r.Get("/web", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("web-root"))
	})
	r.Get("/api/admin/health", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("api-ok"))
	})
	r.Get("/Genres/{Name}/Items", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.URL.EscapedPath()))
	})

	// 1. /WEB/ -> /web/
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/WEB/index.html", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "web-ok" {
		t.Fatalf("GET /WEB/index.html: code=%d body=%q", rec.Code, rec.Body.String())
	}

	// 2. /WEB -> /web
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/WEB", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "web-root" {
		t.Fatalf("GET /WEB: code=%d body=%q", rec.Code, rec.Body.String())
	}

	// 3. /emby/api/admin/health -> /api/admin/health (admin NOT turned into Admin)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/emby/api/admin/health", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "api-ok" {
		t.Fatalf("GET /emby/api/admin/health: code=%d body=%q", rec.Code, rec.Body.String())
	}

	// 4. Escaped %2F preserved in RawPath & EscapedPath
	req := httptest.NewRequest(http.MethodGet, "/emby/genres/Sci%2FFi/items", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "/Genres/Sci%2FFi/Items" {
		t.Fatalf("GET /emby/genres/Sci%%2FFi/items: code=%d body=%q", rec.Code, rec.Body.String())
	}

	// 5. /emby/web and /emby/web/... strip /emby without canonicalizing web asset paths
	r.Get("/web/assets/index.js", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("asset-ok:" + r.URL.Path))
	})
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/emby/web", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "web-root" {
		t.Fatalf("GET /emby/web: code=%d body=%q", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/emby/web/assets/index.js", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "asset-ok:/web/assets/index.js" {
		t.Fatalf("GET /emby/web/assets/index.js: code=%d body=%q", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/EMBY/WEB/index.html", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "web-ok" {
		t.Fatalf("GET /EMBY/WEB/index.html: code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestGzipSkipsSSEAndWeakensStrongETag(t *testing.T) {
	// 1. text/event-stream must NOT be gzip-compressed.
	sseHandler := Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: hello\n\n"))
	}))
	reqSSE := httptest.NewRequest(http.MethodGet, "/events", nil)
	reqSSE.Header.Set("Accept-Encoding", "gzip")
	recSSE := httptest.NewRecorder()
	sseHandler.ServeHTTP(recSSE, reqSSE)
	if recSSE.Header().Get("Content-Encoding") != "" {
		t.Fatalf("expected SSE not compressed, got Content-Encoding=%q", recSSE.Header().Get("Content-Encoding"))
	}
	if recSSE.Body.String() != "data: hello\n\n" {
		t.Fatalf("unexpected SSE body: %q", recSSE.Body.String())
	}

	// 2. Strong ETag is weakened to W/... when compressed.
	jsonHandler := Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", `"abc123"`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	reqJSON := httptest.NewRequest(http.MethodGet, "/Items", nil)
	reqJSON.Header.Set("Accept-Encoding", "gzip")
	recJSON := httptest.NewRecorder()
	jsonHandler.ServeHTTP(recJSON, reqJSON)
	if recJSON.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("expected gzip Content-Encoding, got %q", recJSON.Header().Get("Content-Encoding"))
	}
	if etag := recJSON.Header().Get("ETag"); etag != `W/"abc123"` {
		t.Fatalf("expected weakened ETag W/\"abc123\", got %q", etag)
	}
	gr, err := gzip.NewReader(recJSON.Body)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	plain, _ := io.ReadAll(gr)
	_ = gr.Close()
	if string(plain) != `{"ok":true}` {
		t.Fatalf("unexpected decompressed body: %q", string(plain))
	}

	// 3. Already-weak ETag stays unchanged.
	weakHandler := Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", `W/"already-weak"`)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	recWeak := httptest.NewRecorder()
	weakHandler.ServeHTTP(recWeak, reqJSON)
	if etag := recWeak.Header().Get("ETag"); etag != `W/"already-weak"` {
		t.Fatalf("expected already-weak ETag preserved, got %q", etag)
	}
}

func TestParseAuthDoesNotTouchSessionForDeniedRemoteUser(t *testing.T) {
	dir := t.TempDir()
	store, err := sqlite.Open(filepath.Join(dir, "mw_remote_touch.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	cfg := config.Default(dir)
	repo := sqlite.NewUserRepo(store)
	authSvc := auth.NewService(repo, cfg)
	sessMgr := session.NewManager(repo, cfg, nil)
	ctx := context.Background()

	u, err := authSvc.CreateUser(ctx, "lanuser", "pw", false)
	if err != nil {
		t.Fatal(err)
	}
	u.Policy.EnableRemoteAccess = false
	if err := repo.UpdateUserPolicy(ctx, u.ID, u.Policy); err != nil {
		t.Fatal(err)
	}
	authRes, err := authSvc.AuthenticateByName(ctx, "lanuser", "pw", "", auth.ClientInfo{DeviceID: "dev-remote-check"})
	if err != nil {
		t.Fatal(err)
	}

	mw := NewAuth(authSvc, sessMgr)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Public IP request through ParseAuth must NOT touch/create an active session.
	reqPublic := httptest.NewRequest(http.MethodGet, "/Items", nil)
	reqPublic.RemoteAddr = "198.51.100.77:55555"
	reqPublic.Header.Set("X-Emby-Token", authRes.AccessToken)
	mw.ParseAuth(next).ServeHTTP(httptest.NewRecorder(), reqPublic)
	if sessions := sessMgr.ListSessions(); len(sessions) != 0 {
		t.Fatalf("expected 0 sessions for remote-denied user, got %+v", sessions)
	}

	// LAN IP request through ParseAuth MUST touch session.
	reqLAN := httptest.NewRequest(http.MethodGet, "/Items", nil)
	reqLAN.RemoteAddr = "192.168.1.99:55555"
	reqLAN.Header.Set("X-Emby-Token", authRes.AccessToken)
	mw.ParseAuth(next).ServeHTTP(httptest.NewRecorder(), reqLAN)
	if sessions := sessMgr.ListSessions(); len(sessions) != 1 || sessions[0].UserId != u.ID {
		t.Fatalf("expected 1 session for LAN request, got %+v", sessions)
	}
}
