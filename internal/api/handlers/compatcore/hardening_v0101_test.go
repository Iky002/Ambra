package compatcore

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestSystemStorageInfoRequiresAdmin(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	authSvc := auth.NewService(env.userRepo, env.cfg)

	if _, err := authSvc.CreateUser(ctx, "viewer_storage", "viewerpw", false); err != nil {
		t.Fatal(err)
	}
	viewerAuth, err := authSvc.AuthenticateByName(ctx, "viewer_storage", "viewerpw", "", auth.ClientInfo{DeviceID: "d-v"})
	if err != nil {
		t.Fatal(err)
	}
	adminAuth, err := authSvc.AuthenticateByName(ctx, "admin", "admin", "", auth.ClientInfo{DeviceID: "d-a"})
	if err != nil {
		t.Fatal(err)
	}

	mw := middleware.NewAuth(authSvc, env.sessions)
	router := env.handler.BuildRouter(AuthFuncs{
		RequireAuth:  mw.RequireAuth,
		RequireAdmin: mw.RequireAdmin,
		UserIDFromCtx: func(c context.Context) string {
			if ac, ok := middleware.AuthFromContext(c); ok && ac != nil {
				return ac.UserID()
			}
			return ""
		},
	})

	// Non-admin -> 403
	req := httptest.NewRequest(http.MethodGet, "/System/Info/Storage", nil)
	req.Header.Set("X-Emby-Token", viewerAuth.AccessToken)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin GET /System/Info/Storage expected 403, got %d", rec.Code)
	}

	// Admin -> 200
	req = httptest.NewRequest(http.MethodGet, "/System/Info/Storage", nil)
	req.Header.Set("X-Emby-Token", adminAuth.AccessToken)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin GET /System/Info/Storage expected 200, got %d", rec.Code)
	}
}

func TestForgotPasswordRateLimitAndFilePruning(t *testing.T) {
	env := newTestEnv(t)

	// 1. Pre-create 12 old passwordreset-*.txt files to verify pruning caps at 10 newest.
	baseTime := time.Now().UTC().Add(-2 * time.Hour)
	for i := 0; i < 12; i++ {
		p := filepath.Join(env.cfg.DataDir, fmt.Sprintf("passwordreset-old%02d.txt", i))
		if err := os.WriteFile(p, []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
		ts := baseTime.Add(time.Duration(i) * time.Minute)
		_ = os.Chtimes(p, ts, ts)
	}

	doFP := func(remoteAddr, xff, xri string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/Users/ForgotPassword", bytes.NewBufferString(`{"EnteredUsername":"admin"}`))
		req.RemoteAddr = remoteAddr
		if xff != "" {
			req.Header.Set("X-Forwarded-For", xff)
		}
		if xri != "" {
			req.Header.Set("X-Real-IP", xri)
		}
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)
		return rec
	}

	// 5 requests from same IP succeed; 6th returns 429 Too Many Requests.
	for i := 0; i < forgotPasswordRateLimit; i++ {
		rec := doFP("198.51.100.1:1234", "", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d expected 200, got %d", i+1, rec.Code)
		}
	}
	rec6 := doFP("198.51.100.1:5678", "", "")
	if rec6.Code != http.StatusTooManyRequests {
		t.Fatalf("6th request from same IP expected 429, got %d", rec6.Code)
	}

	// Different IP via X-Forwarded-For is still allowed.
	recXFF := doFP("10.0.0.1:1111", "bad-ip, 203.0.113.42, 10.0.0.1", "")
	if recXFF.Code != http.StatusOK {
		t.Fatalf("request with different X-Forwarded-For IP expected 200, got %d", recXFF.Code)
	}

	// Different IP via X-Real-IP is still allowed.
	recXRI := doFP("10.0.0.1:1111", "", "203.0.113.99")
	if recXRI.Code != http.StatusOK {
		t.Fatalf("request with different X-Real-IP expected 200, got %d", recXRI.Code)
	}

	// Verify passwordreset-*.txt count on disk is capped at forgotPasswordMaxPinFiles (10).
	entries, err := os.ReadDir(env.cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "passwordreset-") && strings.HasSuffix(e.Name(), ".txt") {
			count++
		}
	}
	if count > forgotPasswordMaxPinFiles {
		t.Fatalf("expected at most %d passwordreset files on disk, got %d", forgotPasswordMaxPinFiles, count)
	}

	// Cover expired rate-limit entry pruning and nil forgotRates re-init.
	env.handler.mu.Lock()
	env.handler.forgotRates["198.51.100.250"] = []time.Time{time.Now().UTC().Add(-2 * time.Minute)}
	env.handler.mu.Unlock()
	recPrune := doFP("198.51.100.250:1234", "", "")
	if recPrune.Code != http.StatusOK {
		t.Fatalf("expired window IP expected 200, got %d", recPrune.Code)
	}

	// Cover forgotClientIP edge cases.
	for _, tc := range []struct {
		name   string
		remote string
		xff    string
		xri    string
		want   string
	}{
		{"xff empty part and host:port", "10.0.0.1:1", " , 203.0.113.5:8080", "", "203.0.113.5"},
		{"xri host:port", "10.0.0.1:1", "invalid", "203.0.113.6:9090", "203.0.113.6"},
		{"remote without port", "198.51.100.7", "", "invalid", "198.51.100.7"},
		{"empty remote", "", "", "", ""},
	} {
		req := httptest.NewRequest(http.MethodPost, "/Users/ForgotPassword", nil)
		req.RemoteAddr = tc.remote
		if tc.xff != "" {
			req.Header.Set("X-Forwarded-For", tc.xff)
		}
		if tc.xri != "" {
			req.Header.Set("X-Real-IP", tc.xri)
		}
		if got := forgotClientIP(req); got != tc.want {
			t.Fatalf("forgotClientIP(%s) = %q, want %q", tc.name, got, tc.want)
		}
	}

	// Cover prunePasswordResetFiles edge cases (empty dir, missing dir, subdir, equal mtimes).
	prunePasswordResetFiles("", 10)
	prunePasswordResetFiles(filepath.Join(env.cfg.DataDir, "nonexistent-dir"), 10)
	sameDir := t.TempDir()
	_ = os.Mkdir(filepath.Join(sameDir, "passwordreset-subdir.txt"), 0o755)
	sameTime := time.Now().UTC().Truncate(time.Second)
	for i := 0; i < 3; i++ {
		p := filepath.Join(sameDir, fmt.Sprintf("passwordreset-eq%d.txt", i))
		_ = os.WriteFile(p, []byte("x"), 0o600)
		_ = os.Chtimes(p, sameTime, sameTime)
	}
	prunePasswordResetFiles(sameDir, 1)
}

func TestUserDataPreservesPlayedWhenPlaybackPositionTicksProvided(t *testing.T) {
	env := newTestEnv(t)

	// POST UserData with both Played=true and PlaybackPositionTicks>0 must keep Played=true.
	rec := doReq(t, env.router, http.MethodPost, "/Users/"+env.admin.ID+"/Items/"+env.itemID+"/UserData", `{"Played":true,"PlaybackPositionTicks":123456}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST UserData expected 204, got %d %s", rec.Code, rec.Body.String())
	}

	ud, err := env.libRepo.GetUserItemData(context.Background(), env.admin.ID, env.itemID)
	if err != nil {
		t.Fatalf("GetUserItemData failed: %v", err)
	}
	if !ud.Played {
		t.Fatalf("expected Played=true to be preserved when PlaybackPositionTicks is also set, got %+v", ud)
	}
}

func TestAuthenticateUserByIDDelegatesToAuthService(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	authSvc := auth.NewService(env.userRepo, env.cfg)
	env.handler.SetAuthService(authSvc)

	// 1. Wrong password -> 401 + AuthenticationFailed activity log
	rec := doReq(t, env.router, http.MethodPost, "/Users/"+env.admin.ID+"/Authenticate", `{"Pw":"wrong"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong password, got %d", rec.Code)
	}

	// 2. Valid password via Pw and via Password -> 200 + AuthenticationSucceeded activity log
	rec = doReq(t, env.router, http.MethodPost, "/Users/"+env.admin.ID+"/Authenticate", `{"Pw":"admin"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid password, got %d %s", rec.Code, rec.Body.String())
	}
	var res dto.AuthenticationResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.AccessToken == "" || res.User.Id != env.admin.ID {
		t.Fatalf("unexpected auth result: %+v", res)
	}
	rec = doReq(t, env.router, http.MethodPost, "/Users/"+env.admin.ID+"/Authenticate", `{"Password":"admin"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid Password field, got %d %s", rec.Code, rec.Body.String())
	}

	// 3. Disabled user -> 403 + AuthenticationFailed activity log
	disabledUser, err := authSvc.CreateUser(ctx, "disabled_byid", "pw123", false)
	if err != nil {
		t.Fatal(err)
	}
	pol := sqlite.DefaultUserPolicy(false)
	pol.IsDisabled = true
	if err := env.userRepo.UpdateUserPolicy(ctx, disabledUser.ID, pol); err != nil {
		t.Fatal(err)
	}
	authSvc.InvalidateUserCache(disabledUser.ID)
	rec = doReq(t, env.router, http.MethodPost, "/Users/"+disabledUser.ID+"/Authenticate", `{"Pw":"pw123"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for disabled user, got %d", rec.Code)
	}

	// 4. Empty-password user with legacy SHA1("") in Password field -> 200
	nopwUser, err := authSvc.CreateUser(ctx, "legacy_nopw", "", false)
	if err != nil {
		t.Fatal(err)
	}
	emptySum := sha1.Sum([]byte(""))
	emptySHA1 := hex.EncodeToString(emptySum[:])
	rec = doReq(t, env.router, http.MethodPost, "/Users/"+nopwUser.ID+"/Authenticate", `{"Password":"`+emptySHA1+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for empty-password user with legacy SHA1, got %d %s", rec.Code, rec.Body.String())
	}

	// 5. Verify activity logs recorded both succeeded and failed events
	logs, _, err := env.userRepo.ListActivityLogs(ctx, 0, 50, nil)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for _, l := range logs {
		seen[l.Type]++
	}
	if seen["AuthenticationSucceeded"] < 2 || seen["AuthenticationFailed"] < 2 {
		t.Fatalf("expected >=2 AuthenticationSucceeded and >=2 AuthenticationFailed logs, got %+v", seen)
	}
}
