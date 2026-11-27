package system

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestH01M01M03_SetupOriginGuardLoopbackRateLimitFFmpegProtectionAndPendingRestart(t *testing.T) {
	dir := t.TempDir()
	st, err := sqlite.Open(filepath.Join(dir, "sys_audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	cfg := config.Default(dir)
	cfg.FFmpegPath = "/opt/safe/ffmpeg"
	cfg.FFprobePath = "/opt/safe/ffprobe"
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	userRepo := sqlite.NewUserRepo(st)
	authSvc := auth.NewService(userRepo, cfg)
	sessMgr := session.NewManager(userRepo, cfg, eventbus.New())
	authMW := middleware.NewAuth(authSvc, sessMgr)
	reg := plugins.NewRegistry()

	h := NewHandler(cfg, userRepo, reg).WithSetupDeps(SetupDeps{Auth: authSvc})
	r := chi.NewRouter()
	RegisterRoutes(r, authMW, h)
	RegisterSetupRoutes(r, authMW, h)

	// 1. H-01: Cross-origin POST /api/setup/admin MUST be rejected with 403 Forbidden!
	reqEvil := httptest.NewRequest(http.MethodPost, "/api/setup/admin", bytes.NewBufferString(`{"username":"hacked","password":"pw1234"}`))
	reqEvil.Host = "ambra.local:8096"
	reqEvil.RemoteAddr = "127.0.0.1:45678"
	reqEvil.Header.Set("Origin", "https://evil-site.example")
	recEvil := httptest.NewRecorder()
	r.ServeHTTP(recEvil, reqEvil)
	if recEvil.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for cross-origin /api/setup/admin, got %d %s", recEvil.Code, recEvil.Body.String())
	}

	// Same-origin POST /api/setup/admin succeeds!
	reqSame := httptest.NewRequest(http.MethodPost, "/api/setup/admin", bytes.NewBufferString(`{"username":"admin","password":"adminpassword"}`))
	reqSame.Host = "ambra.local:8096"
	reqSame.RemoteAddr = "127.0.0.1:45678"
	reqSame.Header.Set("Origin", "http://ambra.local:8096")
	recSame := httptest.NewRecorder()
	r.ServeHTTP(recSame, reqSame)
	if recSame.Code != http.StatusOK {
		t.Fatalf("expected 200 for same-origin /api/setup/admin, got %d %s", recSame.Code, recSame.Body.String())
	}
	var loginRes struct {
		AccessToken string `json:"AccessToken"`
	}
	_ = json.Unmarshal(recSame.Body.Bytes(), &loginRes)

	// Cross-origin POST /api/setup/complete MUST also be rejected with 403 Forbidden!
	reqEvilComp := httptest.NewRequest(http.MethodPost, "/api/setup/complete", bytes.NewBufferString(`{}`))
	reqEvilComp.Host = "ambra.local:8096"
	reqEvilComp.RemoteAddr = "127.0.0.1:45678"
	reqEvilComp.Header.Set("X-Emby-Token", loginRes.AccessToken)
	reqEvilComp.Header.Set("Origin", "https://evil-site.example")
	recEvilComp := httptest.NewRecorder()
	r.ServeHTTP(recEvilComp, reqEvilComp)
	if recEvilComp.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for cross-origin /api/setup/complete, got %d", recEvilComp.Code)
	}

	// 2. M-03: POST /System/Configuration ignores FFmpegPath/FFprobePath overwrites,
	// sets HasPendingRestart when DLNA changes without dynamic callback, and calls SetOnDLNAConfigChanged when wired!
	reqCfg := httptest.NewRequest(http.MethodPost, "/System/Configuration", bytes.NewBufferString(`{
		"FFmpegPath": "/tmp/malicious-ffmpeg",
		"FFprobePath": "/tmp/malicious-ffprobe",
		"EnableDLNA": true
	}`))
	reqCfg.Header.Set("X-Emby-Token", loginRes.AccessToken)
	recCfg := httptest.NewRecorder()
	r.ServeHTTP(recCfg, reqCfg)
	if recCfg.Code != http.StatusNoContent {
		t.Fatalf("POST /System/Configuration expected 204, got %d", recCfg.Code)
	}
	snap := cfg.Snapshot()
	if snap.FFmpegPath != "/opt/safe/ffmpeg" || snap.FFprobePath != "/opt/safe/ffprobe" {
		t.Fatalf("M-03 violated: FFmpeg/FFprobe overwritten: ffmpeg=%q ffprobe=%q", snap.FFmpegPath, snap.FFprobePath)
	}

	reqInfo := httptest.NewRequest(http.MethodGet, "/System/Info", nil)
	reqInfo.Header.Set("X-Emby-Token", loginRes.AccessToken)
	recInfo := httptest.NewRecorder()
	r.ServeHTTP(recInfo, reqInfo)
	var sysInfo map[string]any
	_ = json.Unmarshal(recInfo.Body.Bytes(), &sysInfo)
	if sysInfo["HasPendingRestart"] != true {
		t.Fatalf("expected HasPendingRestart=true when EnableDLNA changed without dynamic hook, got %v", sysInfo["HasPendingRestart"])
	}

	// Wire dynamic DLNA callback, clear pending restart, and verify hook fires without setting pendingRestart;
	// then change HTTPAddr and verify HasPendingRestart becomes true!
	h.SetPendingRestart(false)
	var dlnaHookCalled bool
	var hookEnabled bool
	var hookUser string
	h.SetOnDLNAConfigChanged(func(enabled bool, defaultUserID string) {
		dlnaHookCalled = true
		hookEnabled = enabled
		hookUser = defaultUserID
	})
	reqDLNA := httptest.NewRequest(http.MethodPost, "/System/Configuration", bytes.NewBufferString(`{"EnableDLNA":false,"DLNADefaultUserID":"dlna-u1"}`))
	reqDLNA.Header.Set("X-Emby-Token", loginRes.AccessToken)
	recDLNA := httptest.NewRecorder()
	r.ServeHTTP(recDLNA, reqDLNA)
	if !dlnaHookCalled || hookEnabled != false || hookUser != "dlna-u1" {
		t.Fatalf("expected onDLNAConfigChanged(false, dlna-u1), got called=%v enabled=%v user=%q", dlnaHookCalled, hookEnabled, hookUser)
	}
	if h.pendingRestart.Load() {
		t.Fatal("expected HasPendingRestart=false when dynamic DLNA hook handled DLNA update")
	}

	reqPort := httptest.NewRequest(http.MethodPost, "/System/Configuration", bytes.NewBufferString(`{"HTTPAddr":":9096"}`))
	reqPort.Header.Set("X-Emby-Token", loginRes.AccessToken)
	recPort := httptest.NewRecorder()
	r.ServeHTTP(recPort, reqPort)
	if !h.pendingRestart.Load() {
		t.Fatal("expected HasPendingRestart=true when HTTPAddr changed")
	}

	// 3. M-01: Loopback setup rate limiter enforces setupAdminLoopbackRateLimit (180/min) instead of infinite bypass.
	hRate := NewHandler(config.Default(t.TempDir()), sqlite.NewUserRepo(st), reg).WithSetupDeps(SetupDeps{Auth: authSvc})
	for i := 0; i < setupAdminLoopbackRateLimit; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/setup/admin", nil)
		req.RemoteAddr = "127.0.0.1:33333"
		if !hRate.checkSetupRateLimit(req) {
			t.Fatalf("expected loopback request %d/%d allowed", i+1, setupAdminLoopbackRateLimit)
		}
	}
	reqOver := httptest.NewRequest(http.MethodPost, "/api/setup/admin", nil)
	reqOver.RemoteAddr = "127.0.0.1:33333"
	if hRate.checkSetupRateLimit(reqOver) {
		t.Fatal("expected loopback setup request exceeding setupAdminLoopbackRateLimit to be rate-limited")
	}
	_ = context.Background()
}
