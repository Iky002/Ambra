package system

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

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

func TestSetupAdminRateLimitFastPathIPv6AndCap(t *testing.T) {
	// 1. Post-setup fast-path: once IsStartupWizardCompleted=true, POST /api/setup/admin
	// MUST return 403 Forbidden BEFORE touching h.setupRates (map must not grow).
	cfgDone := config.Default(t.TempDir())
	cfgDone.IsStartupWizardCompleted = true
	hDone := NewHandler(cfgDone, nil, plugins.NewRegistry())

	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/setup/admin", bytes.NewBufferString(`{"username":"x","password":"y"}`))
		req.RemoteAddr = fmt.Sprintf("203.0.113.%d:44444", i+1)
		rec := httptest.NewRecorder()
		hDone.SetupAdmin(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("post-setup request %d expected 403, got %d", i+1, rec.Code)
		}
	}
	hDone.setupRateMu.Lock()
	doneBuckets := len(hDone.setupRates)
	hDone.setupRateMu.Unlock()
	if doneBuckets != 0 {
		t.Fatalf("expected setupRates to remain empty after wizard completion, got %d entries", doneBuckets)
	}

	// 2. IPv6 /64 normalization: distinct host addresses in the same /64 share one rate-limit bucket.
	hIPv6 := NewHandler(config.Default(t.TempDir()), nil, plugins.NewRegistry())
	for i := 0; i < setupAdminRateLimit; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/setup/admin", nil)
		req.RemoteAddr = fmt.Sprintf("[2001:db8:abcd:1234::%x]:55555", i+1)
		if !hIPv6.checkSetupRateLimit(req) {
			t.Fatalf("expected IPv6 request %d/%d allowed", i+1, setupAdminRateLimit)
		}
	}
	reqIPv6Over := httptest.NewRequest(http.MethodPost, "/api/setup/admin", nil)
	reqIPv6Over.RemoteAddr = "[2001:db8:abcd:1234::ffff]:55555"
	if hIPv6.checkSetupRateLimit(reqIPv6Over) {
		t.Fatal("expected IPv6 address in same /64 to hit rate limit")
	}
	hIPv6.setupRateMu.Lock()
	v6Buckets := len(hIPv6.setupRates)
	hIPv6.setupRateMu.Unlock()
	if v6Buckets != 1 {
		t.Fatalf("expected all /64 IPv6 addresses collapsed into 1 bucket, got %d", v6Buckets)
	}

	// 3. Bucket cap fail-closed: once maxSetupRateBuckets active entries exist, new IPs fail closed (429).
	hCap := NewHandler(config.Default(t.TempDir()), nil, plugins.NewRegistry())
	now := time.Now().UTC()
	hCap.setupRateMu.Lock()
	hCap.setupRates = make(map[string][]time.Time, maxSetupRateBuckets)
	for i := 0; i < maxSetupRateBuckets; i++ {
		hCap.setupRates[fmt.Sprintf("198.51.%d.%d", i/256, i%256)] = []time.Time{now}
	}
	hCap.setupRateMu.Unlock()

	reqNew := httptest.NewRequest(http.MethodPost, "/api/setup/admin", bytes.NewBufferString(`{"username":"a","password":"b"}`))
	reqNew.RemoteAddr = "203.0.113.250:12345"
	recNew := httptest.NewRecorder()
	hCap.SetupAdmin(recNew, reqNew)
	if recNew.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 fail-closed when setupRates is at cap, got %d", recNew.Code)
	}
	hCap.setupRateMu.Lock()
	capLen := len(hCap.setupRates)
	hCap.setupRateMu.Unlock()
	if capLen != maxSetupRateBuckets {
		t.Fatalf("expected setupRates to remain capped at %d, got %d", maxSetupRateBuckets, capLen)
	}
}
