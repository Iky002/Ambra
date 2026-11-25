package quickconnect

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func setupQuickConnectTest(t *testing.T) (*Handler, *http.ServeMux, *auth.Service, *sqlite.UserRecord, func(time.Duration)) {
	t.Helper()
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "qc.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	cfg := config.Default(tmp)
	userRepo := sqlite.NewUserRepo(store)
	authSvc := auth.NewService(userRepo, cfg)
	sessMgr := session.NewManager(userRepo, cfg, eventbus.New())

	user, err := authSvc.CreateUser(context.Background(), "alice", "secret123", false)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	h := New(authSvc, userRepo, sessMgr)
	h.now = func() time.Time { return now }

	authMW := middleware.NewAuth(authSvc, sessMgr)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{
		RequireAuth: authMW.RequireAuth,
		UserIDFromCtx: func(c context.Context) string {
			if ac, ok := middleware.AuthFromContext(c); ok && ac != nil {
				return ac.UserID()
			}
			return ""
		},
	})

	advance := func(d time.Duration) {
		now = now.Add(d)
	}
	return h, mux, authSvc, user, advance
}

func TestQuickConnectFullFlowAndReplayRejection(t *testing.T) {
	_, mux, authSvc, user, _ := setupQuickConnectTest(t)

	// 1. GET /QuickConnect/Enabled -> true
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/QuickConnect/Enabled", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("enabled status=%d", rec.Code)
	}
	var enabled bool
	if err := json.Unmarshal(rec.Body.Bytes(), &enabled); err != nil || !enabled {
		t.Fatalf("expected enabled=true, got %v (err=%v)", enabled, err)
	}

	// 2. POST /QuickConnect/Initiate -> returns secret (64 hex chars) & 6-digit code
	initReq := httptest.NewRequest(http.MethodPost, "/QuickConnect/Initiate", nil)
	initReq.Header.Set("X-Emby-Authorization", `MediaBrowser Client="Apple TV", Device="Living Room TV", DeviceId="tv-001", Version="2.0.0"`)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, initReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("initiate status=%d body=%s", rec.Code, rec.Body.String())
	}
	var initiated QuickConnectResult
	if err := json.Unmarshal(rec.Body.Bytes(), &initiated); err != nil {
		t.Fatal(err)
	}
	if initiated.Authenticated {
		t.Fatalf("expected Authenticated=false initially")
	}
	if len(initiated.Secret) != 64 {
		t.Fatalf("expected 64-char hex secret, got len=%d", len(initiated.Secret))
	}
	if len(initiated.Code) != 6 {
		t.Fatalf("expected 6-digit code, got %q", initiated.Code)
	}
	if initiated.DeviceId != "tv-001" || initiated.AppName != "Apple TV" {
		t.Fatalf("unexpected client metadata: %+v", initiated)
	}

	// 3. GET /QuickConnect/Connect?secret=... -> Authenticated: false
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/QuickConnect/Connect?secret="+initiated.Secret, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("connect status=%d", rec.Code)
	}
	var polled QuickConnectResult
	_ = json.Unmarshal(rec.Body.Bytes(), &polled)
	if polled.Authenticated {
		t.Fatalf("expected Authenticated=false before authorize")
	}

	// 4. POST /Users/AuthenticateWithQuickConnect before authorize -> 401
	authBody, _ := json.Marshal(map[string]string{"Secret": initiated.Secret})
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Users/AuthenticateWithQuickConnect", bytes.NewReader(authBody)))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 before authorize, got %d", rec.Code)
	}

	// 5. POST /QuickConnect/Authorize without auth -> 401
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/QuickConnect/Authorize?code="+initiated.Code, nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without auth, got %d", rec.Code)
	}

	// 6. Authenticate alice to get a token, then POST /QuickConnect/Authorize
	loginRes, err := authSvc.AuthenticateByName(context.Background(), user.Name, "secret123", "", auth.ClientInfo{DeviceID: "phone-1"})
	if err != nil {
		t.Fatal(err)
	}
	authzReq := httptest.NewRequest(http.MethodPost, "/QuickConnect/Authorize?code="+initiated.Code, nil)
	authzReq.Header.Set("X-Emby-Token", loginRes.AccessToken)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, authzReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("authorize status=%d body=%s", rec.Code, rec.Body.String())
	}
	var okResp bool
	if err := json.Unmarshal(rec.Body.Bytes(), &okResp); err != nil || !okResp {
		t.Fatalf("expected true from authorize, got %v", okResp)
	}

	// 7. Poll Connect -> Authenticated: true
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/QuickConnect/Connect?secret="+initiated.Secret, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("connect after authorize status=%d", rec.Code)
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &polled)
	if !polled.Authenticated {
		t.Fatalf("expected Authenticated=true after authorize")
	}

	// 8. POST /Users/AuthenticateWithQuickConnect -> 200 with valid AuthenticationResult
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Users/AuthenticateWithQuickConnect", bytes.NewReader(authBody)))
	if rec.Code != http.StatusOK {
		t.Fatalf("authenticate status=%d body=%s", rec.Code, rec.Body.String())
	}
	var qcAuth dto.AuthenticationResult
	if err := json.Unmarshal(rec.Body.Bytes(), &qcAuth); err != nil {
		t.Fatal(err)
	}
	if qcAuth.AccessToken == "" || qcAuth.User.Id != user.ID || qcAuth.SessionInfo.DeviceId != "tv-001" {
		t.Fatalf("unexpected auth result: %+v", qcAuth)
	}
	id, err := authSvc.ValidateToken(context.Background(), qcAuth.AccessToken)
	if err != nil || id.User.ID != user.ID {
		t.Fatalf("issued quickconnect token failed validation: err=%v id=%+v", err, id)
	}

	// 9. Replay with same secret -> 401
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Users/AuthenticateWithQuickConnect", bytes.NewReader(authBody)))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on replay, got %d", rec.Code)
	}
}

func TestQuickConnectRateLimitAndExpiry(t *testing.T) {
	_, mux, authSvc, user, advance := setupQuickConnectTest(t)

	loginRes, err := authSvc.AuthenticateByName(context.Background(), user.Name, "secret123", "", auth.ClientInfo{DeviceID: "phone-2"})
	if err != nil {
		t.Fatal(err)
	}

	// Initiate a session
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/QuickConnect/Initiate", nil))
	var initiated QuickConnectResult
	_ = json.Unmarshal(rec.Body.Bytes(), &initiated)

	// 5 wrong codes -> 404, 6th -> 429
	for i := 0; i < maxFailedAttempts; i++ {
		body, _ := json.Marshal(map[string]string{"Code": "000000"})
		req := httptest.NewRequest(http.MethodPost, "/QuickConnect/Authorize", bytes.NewReader(body))
		req.Header.Set("X-Emby-Token", loginRes.AccessToken)
		r := httptest.NewRecorder()
		mux.ServeHTTP(r, req)
		if r.Code != http.StatusNotFound {
			t.Fatalf("attempt %d expected 404, got %d", i+1, r.Code)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/QuickConnect/Authorize?code="+initiated.Code, nil)
	req.Header.Set("X-Emby-Token", loginRes.AccessToken)
	r := httptest.NewRecorder()
	mux.ServeHTTP(r, req)
	if r.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after %d failed attempts, got %d", maxFailedAttempts, r.Code)
	}

	// Advance clock past TTL -> Connect returns 404
	advance(defaultTTL + time.Second)
	r = httptest.NewRecorder()
	mux.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/QuickConnect/Connect?secret="+initiated.Secret, nil))
	if r.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for expired secret, got %d", r.Code)
	}
}
