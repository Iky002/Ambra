package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestEmbyPrefixAndCase(t *testing.T) {
	r := chi.NewRouter()
	r.Use(EmbyPrefixAndCase)

	r.Get("/System/Info/Public", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("public-info"))
	})
	r.Get("/Users/{Id}/Items", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("uid:" + chi.URLParam(r, "Id")))
	})
	r.Get("/web/index.html", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("web-ui"))
	})
	r.Get("/api/admin/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ambra-api"))
	})

	cases := []struct {
		path string
		want string
	}{
		{"/System/Info/Public", "public-info"},
		{"/emby/System/Info/Public", "public-info"},
		{"/emby/system/info/public", "public-info"},
		{"/EMBY/SYSTEM/INFO/PUBLIC", "public-info"},
		{"/emby/users/AbC123Def/items", "uid:AbC123Def"},
		{"/web/index.html", "web-ui"},
		{"/api/admin/health", "ambra-api"},
	}

	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || rec.Body.String() != tc.want {
			t.Errorf("GET %s: got status=%d body=%q, want 200 %q", tc.path, rec.Code, rec.Body.String(), tc.want)
		}
	}
}

func TestParseAuthorizationHeaderAndTokenSources(t *testing.T) {
	// 1. X-Emby-Authorization header
	req := httptest.NewRequest(http.MethodGet, "/Users?api_key=query-tok", nil)
	req.Header.Set("X-Emby-Authorization", `MediaBrowser Client="Emby Web", Device="Chrome", DeviceId="dev-123", Version="4.8.0", Token="auth-tok"`)
	info := ExtractClientInfo(req)
	if info.Client != "Emby Web" || info.Device != "Chrome" || info.DeviceID != "dev-123" || info.Version != "4.8.0" || info.Token != "auth-tok" {
		t.Fatalf("unexpected parsed auth header: %+v", info)
	}

	// 2. X-MediaBrowser-Token takes precedence over Authorization
	req.Header.Set("X-MediaBrowser-Token", "mb-tok")
	if got := ExtractClientInfo(req).Token; got != "mb-tok" {
		t.Fatalf("expected mb-tok, got %q", got)
	}

	// 3. X-Emby-Token has highest precedence
	req.Header.Set("X-Emby-Token", "emby-tok")
	if got := ExtractClientInfo(req).Token; got != "emby-tok" {
		t.Fatalf("expected emby-tok, got %q", got)
	}

	// 4. Query api_key fallback
	req2 := httptest.NewRequest(http.MethodGet, "/Users?API_KEY=qs-tok", nil)
	if got := ExtractClientInfo(req2).Token; got != "qs-tok" {
		t.Fatalf("expected qs-tok, got %q", got)
	}
}

func TestAuthMiddlewareRBAC(t *testing.T) {
	dir := t.TempDir()
	store, err := sqlite.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer store.Close()

	cfg := config.Default(dir)
	repo := sqlite.NewUserRepo(store)
	authSvc := auth.NewService(repo, cfg)
	ctx := context.Background()

	if _, err := authSvc.CreateUser(ctx, "admin", "pw", true); err != nil {
		t.Fatalf("create admin: %v", err)
	}
	if _, err := authSvc.CreateUser(ctx, "viewer", "pw", false); err != nil {
		t.Fatalf("create viewer: %v", err)
	}

	adminRes, err := authSvc.AuthenticateByName(ctx, "admin", "pw", "", auth.ClientInfo{DeviceID: "d-admin"})
	if err != nil {
		t.Fatalf("auth admin: %v", err)
	}
	viewerRes, err := authSvc.AuthenticateByName(ctx, "viewer", "pw", "", auth.ClientInfo{DeviceID: "d-viewer"})
	if err != nil {
		t.Fatalf("auth viewer: %v", err)
	}
	apiKey, err := authSvc.CreateAPIKey(ctx, "Sonarr")
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}

	mw := NewAuth(authSvc, nil)
	r := chi.NewRouter()
	r.Use(CORS)
	r.With(mw.RequireAuth).Get("/me", func(w http.ResponseWriter, r *http.Request) {
		ac, ok := AuthFromContext(r.Context())
		if !ok {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		if ac.User != nil {
			_, _ = w.Write([]byte(ac.User.Name))
		} else {
			_, _ = w.Write([]byte("apikey:" + ac.APIKeyName))
		}
	})
	r.With(mw.RequireAdmin).Get("/admin-only", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Unauthenticated -> 401
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/me", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}

	// Viewer on /me -> 200
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("X-Emby-Token", viewerRes.AccessToken)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "viewer" {
		t.Fatalf("viewer /me: status=%d body=%q", rec.Code, rec.Body.String())
	}

	// Viewer on /admin-only -> 403
	req = httptest.NewRequest(http.MethodGet, "/admin-only", nil)
	req.Header.Set("X-Emby-Token", viewerRes.AccessToken)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer /admin-only: expected 403, got %d", rec.Code)
	}

	// Admin on /admin-only -> 200
	req = httptest.NewRequest(http.MethodGet, "/admin-only", nil)
	req.Header.Set("X-Emby-Token", adminRes.AccessToken)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin /admin-only: expected 200, got %d", rec.Code)
	}

	// API Key on /admin-only -> 200
	req = httptest.NewRequest(http.MethodGet, "/admin-only?api_key="+apiKey.Token, nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("apikey /admin-only: expected 200, got %d", rec.Code)
	}

	// CORS preflight OPTIONS -> 204
	req = httptest.NewRequest(http.MethodOptions, "/me", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent || rec.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
		t.Fatalf("CORS preflight failed: status=%d header=%q", rec.Code, rec.Header().Get("Access-Control-Allow-Origin"))
	}
}
