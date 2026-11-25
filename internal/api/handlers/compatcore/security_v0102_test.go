package compatcore

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestDeviceIDSpoofingAndPublicRateLimitBypassBlocked(t *testing.T) {
	dir := t.TempDir()
	st, err := sqlite.Open(filepath.Join(dir, "sec_v0102.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	cfg := config.Default(dir)
	userRepo := sqlite.NewUserRepo(st)
	libRepo := sqlite.NewLibraryRepo(st)
	authSvc := auth.NewService(userRepo, cfg)
	sessMgr := session.NewManager(userRepo, cfg, nil)

	_, _ = authSvc.CreateUser(ctx, "alice", "alice123", false)
	_, _ = authSvc.CreateUser(ctx, "bob", "bob123", false)

	aliceAuth, err := authSvc.AuthenticateByName(ctx, "alice", "alice123", "", auth.ClientInfo{DeviceID: "dev-alice", Device: "AlicePhone"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = authSvc.AuthenticateByName(ctx, "bob", "bob123", "", auth.ClientInfo{DeviceID: "dev-bob", Device: "BobTV"})
	if err != nil {
		t.Fatal(err)
	}

	authMW := middleware.NewAuth(authSvc, sessMgr)
	h := New(cfg, st, userRepo, libRepo, sessMgr, nil)
	h.SetAuthService(authSvc)
	router := authMW.ParseAuth(h.BuildRouter(AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
		UserIDFromCtx: func(c context.Context) string {
			if ac, ok := middleware.AuthFromContext(c); ok && ac != nil {
				return ac.UserID()
			}
			return ""
		},
	}))

	// 1. Alice spoofs X-Emby-Device-Id: dev-bob -> must get 403 on Bob's device info & options
	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/Devices/Info?Id=dev-bob", ""},
		{http.MethodPost, "/Devices/Options?Id=dev-bob", `{"CustomName":"Hijacked"}`},
		{http.MethodGet, "/Devices/CameraUploads?DeviceId=dev-bob", ""},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req.Header.Set("X-Emby-Token", aliceAuth.AccessToken)
		req.Header.Set("X-Emby-Device-Id", "dev-bob")
		if tc.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("spoofed DeviceId %s %s expected 403, got %d (%s)", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}

	// Alice CAN still access her own device
	reqOwn := httptest.NewRequest(http.MethodGet, "/Devices/Info?Id=dev-alice", nil)
	reqOwn.Header.Set("X-Emby-Token", aliceAuth.AccessToken)
	recOwn := httptest.NewRecorder()
	router.ServeHTTP(recOwn, reqOwn)
	if recOwn.Code != http.StatusOK {
		t.Fatalf("alice own device info expected 200, got %d", recOwn.Code)
	}

	// 2. Direct public IP spoofing different X-Forwarded-For/X-Real-IP on every request MUST still hit 429
	for i := 0; i < forgotPasswordRateLimit; i++ {
		req := httptest.NewRequest(http.MethodPost, "/Users/ForgotPassword", strings.NewReader(`{"EnteredUsername":"alice"}`))
		req.RemoteAddr = "203.0.113.88:50000"
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", i+1))
		req.Header.Set("X-Real-IP", fmt.Sprintf("198.51.100.%d", i+100))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d expected 200, got %d", i+1, rec.Code)
		}
	}
	reqBlocked := httptest.NewRequest(http.MethodPost, "/Users/ForgotPassword", strings.NewReader(`{"EnteredUsername":"alice"}`))
	reqBlocked.RemoteAddr = "203.0.113.88:50001"
	reqBlocked.Header.Set("X-Forwarded-For", "198.51.100.250")
	recBlocked := httptest.NewRecorder()
	router.ServeHTTP(recBlocked, reqBlocked)
	if recBlocked.Code != http.StatusTooManyRequests {
		t.Fatalf("public IP spoofing X-Forwarded-For must still be rate-limited (429), got %d", recBlocked.Code)
	}
}
