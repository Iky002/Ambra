package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestAuthContextHelpers(t *testing.T) {
	var nilAC *AuthContext
	if nilAC.IsAdmin() {
		t.Fatal("nil AuthContext should not be admin")
	}
	if nilAC.UserID() != "" {
		t.Fatal("nil AuthContext should have empty UserID")
	}

	emptyAC := &AuthContext{}
	if emptyAC.IsAdmin() {
		t.Fatal("empty AuthContext should not be admin")
	}
	if emptyAC.UserID() != "" {
		t.Fatal("empty AuthContext should have empty UserID")
	}

	apiAC := &AuthContext{IsAPIKey: true}
	if !apiAC.IsAdmin() {
		t.Fatal("API key AuthContext should be admin")
	}

	policyAdmin := &AuthContext{
		User: &sqlite.UserRecord{
			ID:     "u-1",
			Policy: dto.UserPolicy{IsAdministrator: true},
		},
	}
	if !policyAdmin.IsAdmin() {
		t.Fatal("policy admin should be admin")
	}
	if policyAdmin.UserID() != "u-1" {
		t.Fatalf("expected u-1, got %q", policyAdmin.UserID())
	}

	if _, ok := AuthFromContext(context.Background()); ok {
		t.Fatal("expected no AuthContext in background context")
	}
	if _, ok := AuthFromContext(WithAuthContext(context.Background(), nil)); ok {
		t.Fatal("expected nil AuthContext to return false")
	}
}

func TestParseAuthorizationHeaderEdgeCases(t *testing.T) {
	if got := ParseAuthorizationHeader("   "); len(got) != 0 {
		t.Fatalf("expected empty map, got %v", got)
	}

	bearer := ParseAuthorizationHeader("Bearer my-secret-token")
	if bearer["token"] != "my-secret-token" {
		t.Fatalf("expected bearer token, got %v", bearer)
	}

	embyScheme := ParseAuthorizationHeader(`Emby Client="Infuse", Device=AppleTV, DeviceId="d1", Version="7.6", Token="unclosed`)
	if embyScheme["client"] != "Infuse" || embyScheme["device"] != "AppleTV" || embyScheme["deviceid"] != "d1" || embyScheme["version"] != "7.6" || embyScheme["token"] != "unclosed" {
		t.Fatalf("unexpected embyScheme parse: %+v", embyScheme)
	}

	// Trailing comma, bare token without '=', empty key, unquoted last value
	weird := ParseAuthorizationHeader(` , =ignored, Key1=Val1, noequals`)
	if weird["key1"] != "Val1" {
		t.Fatalf("expected key1=Val1, got %+v", weird)
	}
	if _, exists := weird[""]; exists {
		t.Fatalf("did not expect empty key: %+v", weird)
	}

	// Trailing spaces after comma
	trailing := ParseAuthorizationHeader(`Key1="v1",   `)
	if trailing["key1"] != "v1" {
		t.Fatalf("expected key1=v1, got %+v", trailing)
	}
}

func TestExtractClientInfoAllHeadersAndQuery(t *testing.T) {
	// Authorization fallback + individual X-Emby-* overrides
	req := httptest.NewRequest(http.MethodGet, "/Items?deviceId=q-dev", nil)
	req.Header.Set("Authorization", `MediaBrowser Client="AuthClient", Device="AuthDev", DeviceId="auth-id", Version="1.0", Token="tok-a"`)
	req.Header.Set("X-Emby-Client", "OverrideClient")
	req.Header.Set("X-Emby-Device-Name", "OverrideDev")
	req.Header.Set("X-Emby-Device-Id", "override-id")
	req.Header.Set("X-Emby-Client-Version", "2.0")

	info := ExtractClientInfo(req)
	if info.Client != "OverrideClient" || info.Device != "OverrideDev" || info.DeviceID != "override-id" || info.Version != "2.0" || info.Token != "tok-a" {
		t.Fatalf("unexpected client info: %+v", info)
	}

	// Query deviceId fallback when no headers
	req2 := httptest.NewRequest(http.MethodGet, "/Items?api_key=k1&deviceId=q-dev-2", nil)
	info2 := ExtractClientInfo(req2)
	if info2.Token != "k1" || info2.DeviceID != "q-dev-2" {
		t.Fatalf("unexpected query fallback info: %+v", info2)
	}
}

func TestParseAuthAndSessionTouchAndNilService(t *testing.T) {
	// Nil auth service still passes anonymous context cleanly
	mwNil := NewAuth(nil, nil)
	called := false
	h := mwNil.ParseAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		ac, ok := AuthFromContext(r.Context())
		if !ok || ac.User != nil {
			t.Fatalf("expected anonymous AuthContext, got ok=%v ac=%+v", ok, ac)
		}
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Emby-Token", "some-tok")
	h.ServeHTTP(rec, req)
	if !called || rec.Code != http.StatusOK {
		t.Fatalf("ParseAuth with nil svc failed: called=%v code=%d", called, rec.Code)
	}

	// Real auth service + session manager + fallback client fields from token record
	dir := t.TempDir()
	store, err := sqlite.Open(filepath.Join(dir, "mw.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	cfg := config.Default(dir)
	repo := sqlite.NewUserRepo(store)
	authSvc := auth.NewService(repo, cfg)
	sessMgr := session.NewManager(repo, cfg, eventbus.New())

	ctx := context.Background()
	if _, err := authSvc.CreateUser(ctx, "alice", "secret", false); err != nil {
		t.Fatal(err)
	}
	res, err := authSvc.AuthenticateByName(ctx, "alice", "secret", "", auth.ClientInfo{
		Client:   "Infuse",
		Device:   "iPhone",
		DeviceID: "dev-iphone",
		Version:  "7.5",
	})
	if err != nil {
		t.Fatal(err)
	}

	mw := NewAuth(authSvc, sessMgr)

	// Invalid token via ParseAuth -> anonymous
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-Emby-Token", "invalid-token")
	mw.ParseAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ac, _ := AuthFromContext(r.Context())
		if ac.User != nil {
			t.Fatal("expected nil user for invalid token")
		}
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, req)

	// Valid token without client headers -> fills from token metadata and touches session
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-Emby-Token", res.AccessToken)
	mw.ParseAuth(mw.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ac, ok := AuthFromContext(r.Context())
		if !ok || ac.User == nil {
			t.Fatal("expected authenticated user")
		}
		if ac.Client.DeviceID != "dev-iphone" || ac.Client.Device != "iPhone" || ac.Client.Client != "Infuse" || ac.Client.Version != "7.5" {
			t.Fatalf("expected fallback client metadata, got %+v", ac.Client)
		}
		w.WriteHeader(http.StatusOK)
	}))).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if len(sessMgr.ListSessions()) != 1 {
		t.Fatalf("expected 1 active session touched, got %d", len(sessMgr.ListSessions()))
	}
}

func TestCORSWithoutOriginAndEmbyPrefixEdgeCases(t *testing.T) {
	// CORS without Origin sets *
	rec := httptest.NewRecorder()
	CORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ping", nil))
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("expected *, got %q", rec.Header().Get("Access-Control-Allow-Origin"))
	}

	// EmbyPrefixAndCase edge cases
	var gotPath string
	handler := EmbyPrefixAndCase(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))

	// Empty path
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.URL.Path = ""
	handler.ServeHTTP(httptest.NewRecorder(), req)
	if gotPath != "" {
		t.Fatalf("expected empty path, got %q", gotPath)
	}

	// Exact /web
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/web", nil))
	if gotPath != "/web" {
		t.Fatalf("expected /web, got %q", gotPath)
	}

	// Exact /emby -> /
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/emby", nil))
	if gotPath != "/" {
		t.Fatalf("expected /, got %q", gotPath)
	}

	// /embywebsocket preserved (not stripped)
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/EMBYWEBSOCKET", nil))
	if gotPath != "/embywebsocket" {
		t.Fatalf("expected /embywebsocket, got %q", gotPath)
	}

	// Double slash + RawPath cleared
	req2 := httptest.NewRequest(http.MethodGet, "/emby//system/ping", nil)
	req2.URL.RawPath = "/emby//system/ping"
	handler.ServeHTTP(httptest.NewRecorder(), req2)
	if gotPath != "//System/Ping" {
		t.Fatalf("expected //System/Ping, got %q", gotPath)
	}
}
