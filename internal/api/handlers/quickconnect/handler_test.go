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
	"github.com/ambra-media/ambra/internal/pkg/netutil"
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

func TestQuickConnectPerIPRateLimitAndEdgeCases(t *testing.T) {
	h, mux, authSvc, user, advance := setupQuickConnectTest(t)

	// 10 initiates from same IP succeed, 11th returns 429 Too Many Requests.
	for i := 0; i < maxInitiatesPerIPPerMin; i++ {
		req := httptest.NewRequest(http.MethodPost, "/QuickConnect/Initiate", nil)
		req.RemoteAddr = "198.51.100.10:12345"
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("initiate %d from IP1 expected 200, got %d", i+1, rec.Code)
		}
	}
	req11 := httptest.NewRequest(http.MethodPost, "/QuickConnect/Initiate", nil)
	req11.RemoteAddr = "198.51.100.10:54321"
	rec11 := httptest.NewRecorder()
	mux.ServeHTTP(rec11, req11)
	if rec11.Code != http.StatusTooManyRequests {
		t.Fatalf("11th initiate from IP1 expected 429, got %d", rec11.Code)
	}

	// Different IP is unaffected.
	reqOther := httptest.NewRequest(http.MethodPost, "/QuickConnect/Initiate", nil)
	reqOther.RemoteAddr = "198.51.100.20:9999"
	recOther := httptest.NewRecorder()
	mux.ServeHTTP(recOther, reqOther)
	if recOther.Code != http.StatusOK {
		t.Fatalf("initiate from IP2 expected 200, got %d", recOther.Code)
	}

	// After 1 minute window expires, IP1 is allowed again.
	advance(initiateRateWindow + time.Second)
	reqAfter := httptest.NewRequest(http.MethodPost, "/QuickConnect/Initiate", nil)
	reqAfter.RemoteAddr = "198.51.100.10:11111"
	recAfter := httptest.NewRecorder()
	mux.ServeHTTP(recAfter, reqAfter)
	if recAfter.Code != http.StatusOK {
		t.Fatalf("initiate after window reset expected 200, got %d", recAfter.Code)
	}

	// Edge cases: Connect missing secret -> 404; Authenticate invalid JSON / empty secret -> 400.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/QuickConnect/Connect", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("Connect without secret expected 404, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Users/AuthenticateWithQuickConnect", bytes.NewBufferString("{bad")))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Authenticate bad JSON expected 400, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Users/AuthenticateWithQuickConnect", bytes.NewBufferString(`{"Secret":""}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Authenticate empty secret expected 400, got %d", rec.Code)
	}

	// Direct handleAuthorize without auth context -> 401
	rec = httptest.NewRecorder()
	h.handleAuthorize(rec, httptest.NewRequest(http.MethodPost, "/QuickConnect/Authorize?code=123456", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("handleAuthorize without user expected 401, got %d", rec.Code)
	}

	// Disabled user redemption returns 403.
	var initiated QuickConnectResult
	_ = json.Unmarshal(recAfter.Body.Bytes(), &initiated)
	loginRes, err := authSvc.AuthenticateByName(context.Background(), user.Name, "secret123", "", auth.ClientInfo{DeviceID: "phone-3"})
	if err != nil {
		t.Fatal(err)
	}
	authzReq := httptest.NewRequest(http.MethodPost, "/QuickConnect/Authorize?code="+initiated.Code, nil)
	authzReq.Header.Set("X-Emby-Token", loginRes.AccessToken)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, authzReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("authorize expected 200, got %d", rec.Code)
	}

	disabledPolicy := user.Policy
	disabledPolicy.IsDisabled = true
	_ = h.userRepo.UpdateUserPolicy(context.Background(), user.ID, disabledPolicy)
	authBody, _ := json.Marshal(map[string]string{"Secret": initiated.Secret})
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Users/AuthenticateWithQuickConnect", bytes.NewReader(authBody)))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("disabled user AuthenticateWithQuickConnect expected 403, got %d", rec.Code)
	}

	// Deleted user redemption returns 401.
	var initiated2 QuickConnectResult
	_ = json.Unmarshal(recOther.Body.Bytes(), &initiated2)
	h.mu.Lock()
	h.bySecret[initiated2.Secret].userID = "nonexistent-user-id"
	h.bySecret[initiated2.Secret].result.Authenticated = true
	h.mu.Unlock()
	authBody2, _ := json.Marshal(map[string]string{"Secret": initiated2.Secret})
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Users/AuthenticateWithQuickConnect", bytes.NewReader(authBody2)))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("deleted user AuthenticateWithQuickConnect expected 401, got %d", rec.Code)
	}

	// clientIP helper coverage
	if got := clientIP(httptest.NewRequest(http.MethodPost, "/", nil)); got == "" {
		t.Fatalf("expected non-empty clientIP")
	}
	noPortReq := httptest.NewRequest(http.MethodPost, "/", nil)
	noPortReq.RemoteAddr = "198.51.100.30"
	if got := clientIP(noPortReq); got != "198.51.100.30" {
		t.Fatalf("clientIP without port = %q", got)
	}
	emptyReq := httptest.NewRequest(http.MethodPost, "/", nil)
	emptyReq.RemoteAddr = ""
	if got := clientIP(emptyReq); got != "" {
		t.Fatalf("clientIP empty = %q", got)
	}

	// X-Forwarded-For (rightmost non-trusted proxy IP) and X-Real-IP precedence when RemoteAddr is a trusted proxy
	netutil.SetTrustedProxies([]string{"10.0.0.0/8"})
	t.Cleanup(func() { netutil.SetTrustedProxies(nil) })

	xffReq := httptest.NewRequest(http.MethodPost, "/", nil)
	xffReq.RemoteAddr = "10.0.0.1:4321"
	xffReq.Header.Set("X-Forwarded-For", "not-an-ip, 203.0.113.55:8080, 10.0.0.2")
	xffReq.Header.Set("X-Real-IP", "203.0.113.77")
	if got := clientIP(xffReq); got != "203.0.113.55" {
		t.Fatalf("clientIP X-Forwarded-For = %q, want 203.0.113.55", got)
	}

	xriReq := httptest.NewRequest(http.MethodPost, "/", nil)
	xriReq.RemoteAddr = "10.0.0.1:4321"
	xriReq.Header.Set("X-Forwarded-For", "invalid-only")
	xriReq.Header.Set("X-Real-IP", "203.0.113.77:9000")
	if got := clientIP(xriReq); got != "203.0.113.77" {
		t.Fatalf("clientIP X-Real-IP = %q, want 203.0.113.77", got)
	}
}

func TestQuickConnectAdminAuthorizeForAnotherUser(t *testing.T) {
	_, mux, authSvc, alice, _ := setupQuickConnectTest(t)
	ctx := context.Background()

	admin, err := authSvc.CreateUser(ctx, "admin", "adminpass", true)
	if err != nil {
		t.Fatal(err)
	}
	adminLogin, err := authSvc.AuthenticateByName(ctx, admin.Name, "adminpass", "", auth.ClientInfo{DeviceID: "admin-phone"})
	if err != nil {
		t.Fatal(err)
	}
	aliceLogin, err := authSvc.AuthenticateByName(ctx, alice.Name, "secret123", "", auth.ClientInfo{DeviceID: "alice-phone"})
	if err != nil {
		t.Fatal(err)
	}

	initiate := func() QuickConnectResult {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/QuickConnect/Initiate", nil))
		var res QuickConnectResult
		_ = json.Unmarshal(rec.Body.Bytes(), &res)
		return res
	}

	// 1. Non-admin trying to authorize for admin -> 403
	s1 := initiate()
	req := httptest.NewRequest(http.MethodPost, "/QuickConnect/Authorize?code="+s1.Code+"&userId="+admin.ID, nil)
	req.Header.Set("X-Emby-Token", aliceLogin.AccessToken)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin authorize for another user expected 403, got %d", rec.Code)
	}

	// 2. Admin trying to authorize for nonexistent user -> 404
	req = httptest.NewRequest(http.MethodPost, "/QuickConnect/Authorize?code="+s1.Code+"&userId=does-not-exist", nil)
	req.Header.Set("X-Emby-Token", adminLogin.AccessToken)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("admin authorize for missing user expected 404, got %d", rec.Code)
	}

	// 3. Admin authorizing on behalf of alice via JSON body -> 200 and redeems as alice
	body, _ := json.Marshal(map[string]string{"Code": s1.Code, "UserId": alice.ID})
	req = httptest.NewRequest(http.MethodPost, "/QuickConnect/Authorize", bytes.NewReader(body))
	req.Header.Set("X-Emby-Token", adminLogin.AccessToken)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin authorize for alice expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	redeemBody, _ := json.Marshal(map[string]string{"Secret": s1.Secret})
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Users/AuthenticateWithQuickConnect", bytes.NewReader(redeemBody)))
	if rec.Code != http.StatusOK {
		t.Fatalf("redeem expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var qcAuth dto.AuthenticationResult
	_ = json.Unmarshal(rec.Body.Bytes(), &qcAuth)
	if qcAuth.User.Id != alice.ID {
		t.Fatalf("expected redeemed user to be alice (%s), got %s", alice.ID, qcAuth.User.Id)
	}

	// 4. Admin authorizing via query params (?code=...&userId=...) also works
	s2 := initiate()
	req = httptest.NewRequest(http.MethodPost, "/QuickConnect/Authorize?code="+s2.Code+"&userId="+alice.ID, nil)
	req.Header.Set("X-Emby-Token", adminLogin.AccessToken)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin query authorize for alice expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	// 5. Default clock, nil RequireAuth passthrough, AuthFromContext fallback, and maxPendingSessions guard
	hDefault := New(authSvc, nil, nil)
	if hDefault.now().IsZero() {
		t.Fatal("expected non-zero default clock")
	}
	muxDefault := http.NewServeMux()
	hDefault.RegisterRoutes(muxDefault, AuthFuncs{})
	reqCtx := httptest.NewRequest(http.MethodPost, "/QuickConnect/Authorize?code=999999", nil)
	reqCtx = reqCtx.WithContext(middleware.WithAuthContext(reqCtx.Context(), &middleware.AuthContext{User: alice}))
	rec = httptest.NewRecorder()
	muxDefault.ServeHTTP(rec, reqCtx)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 via AuthFromContext fallback, got %d", rec.Code)
	}
	hDefault.mu.Lock()
	for i := 0; i < maxPendingSessions; i++ {
		k := string(rune('a'+i%26)) + time.Now().Format("150405") + string(rune(i))
		hDefault.bySecret[k] = &pendingSession{expiresAt: time.Now().UTC().Add(time.Hour)}
	}
	hDefault.mu.Unlock()
	rec = httptest.NewRecorder()
	muxDefault.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/QuickConnect/Initiate", nil))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 when maxPendingSessions reached, got %d", rec.Code)
	}
}
