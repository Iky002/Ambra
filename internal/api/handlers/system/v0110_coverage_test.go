package system

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/api/ws"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/pkg/netutil"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

type spySTRMPlugin struct {
	configured bool
}

func (s *spySTRMPlugin) Manifest() plugins.Manifest {
	return plugins.Manifest{
		ID:      strmPluginID,
		Name:    "STRM Spy",
		Version: "1.0.0",
		BuiltIn: true,
		Enabled: true,
		Kinds:   []plugins.Kind{plugins.KindStream},
	}
}

func (s *spySTRMPlugin) Configure(_ map[string]any) error {
	s.configured = true
	return nil
}

func TestProcessLifecycleAndRestartShutdownEndpointsV0110(t *testing.T) {
	// Clean up global lifecycle state after test.
	t.Cleanup(func() {
		unreg := RegisterProcessLifecycleController(nil, nil)
		unreg()
	})

	// Unregistered callbacks return false while marking state flags.
	unregInit := RegisterProcessLifecycleController(nil, nil)
	unregInit()
	if TriggerProcessRestart() {
		t.Fatal("expected TriggerProcessRestart=false when no hook is registered")
	}
	if TriggerProcessShutdown() {
		t.Fatal("expected TriggerProcessShutdown=false when no hook is registered")
	}
	pRst, pShut, canRst := ProcessLifecycleState()
	if !pRst || !pShut || canRst {
		t.Fatalf("unexpected ProcessLifecycleState without hooks: pending=%v shutting=%v canRestart=%v", pRst, pShut, canRst)
	}

	procRestartCh := make(chan struct{}, 2)
	procShutdownCh := make(chan struct{}, 2)
	unreg := RegisterProcessLifecycleController(
		func() { procRestartCh <- struct{}{} },
		func() { procShutdownCh <- struct{}{} },
	)
	defer unreg()

	pRst, pShut, canRst = ProcessLifecycleState()
	if pRst || pShut || !canRst {
		t.Fatalf("unexpected ProcessLifecycleState after registration: pending=%v shutting=%v canRestart=%v", pRst, pShut, canRst)
	}
	if !TriggerProcessRestart() {
		t.Fatal("expected TriggerProcessRestart=true")
	}
	if !TriggerProcessShutdown() {
		t.Fatal("expected TriggerProcessShutdown=true")
	}
	select {
	case <-procRestartCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for process restart callback")
	}
	select {
	case <-procShutdownCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for process shutdown callback")
	}

	// Reset global flags before testing HTTP handlers.
	unregReset := RegisterProcessLifecycleController(
		func() { procRestartCh <- struct{}{} },
		func() { procShutdownCh <- struct{}{} },
	)
	defer unregReset()

	r, h, _, userRepo, adminTok, viewerTok := setupSystemHandler(t)
	bus := eventbus.New()
	wsHub := ws.NewHub(nil, nil, bus)
	h.SetWSHub(wsHub)

	// GET /web/ConfigurationPages authenticated -> 200 []
	req := httptest.NewRequest(http.MethodGet, "/web/ConfigurationPages", nil)
	req.Header.Set("X-Emby-Token", viewerTok)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("GET /web/ConfigurationPages failed: %d %s", rec.Code, rec.Body.String())
	}

	// Non-admin cannot restart or shutdown.
	for _, ep := range []string{"/System/Restart", "/System/Shutdown"} {
		req = httptest.NewRequest(http.MethodPost, ep, nil)
		req.Header.Set("X-Emby-Token", viewerTok)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("viewer POST %s expected 403, got %d", ep, rec.Code)
		}
	}

	// Instance-level lifecycle hooks take precedence over process-level hooks.
	instRestartCh := make(chan struct{}, 1)
	instShutdownCh := make(chan struct{}, 1)
	h.SetLifecycleHooks(
		func() { instRestartCh <- struct{}{} },
		func() { instShutdownCh <- struct{}{} },
	)
	h.SetPendingRestart(true)
	req = httptest.NewRequest(http.MethodGet, "/System/Info", nil)
	req.Header.Set("X-Emby-Token", adminTok)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"HasPendingRestart":true`) || !strings.Contains(rec.Body.String(), `"CanSelfRestart":true`) {
		t.Fatalf("expected HasPendingRestart & CanSelfRestart true, got %d %s", rec.Code, rec.Body.String())
	}
	h.SetPendingRestart(false)

	req = httptest.NewRequest(http.MethodPost, "/System/Restart", nil)
	req.Header.Set("X-Emby-Token", adminTok)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("admin POST /System/Restart expected 204, got %d", rec.Code)
	}
	select {
	case <-instRestartCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for instance restart hook")
	}

	req = httptest.NewRequest(http.MethodPost, "/System/Shutdown", nil)
	req.Header.Set("X-Emby-Token", adminTok)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("admin POST /System/Shutdown expected 204, got %d", rec.Code)
	}
	select {
	case <-instShutdownCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for instance shutdown hook")
	}

	// Verify activity logs recorded both ServerRestart and ServerShutdown.
	logs, total, err := userRepo.ListActivityLogs(context.Background(), 0, 20, nil)
	if err != nil || total < 2 || len(logs) < 2 {
		t.Fatalf("expected restart/shutdown activity logs, total=%d err=%v", total, err)
	}

	// Clear instance hooks so RestartServer / ShutdownServer fall back to TriggerProcessRestart / TriggerProcessShutdown.
	h.SetLifecycleHooks(nil, nil)
	req = httptest.NewRequest(http.MethodPost, "/System/Restart", nil)
	req.Header.Set("X-Emby-Token", adminTok)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST /System/Restart fallback expected 204, got %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodPost, "/System/Shutdown", nil)
	req.Header.Set("X-Emby-Token", adminTok)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST /System/Shutdown fallback expected 204, got %d", rec.Code)
	}
	select {
	case <-procRestartCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for fallback process restart hook")
	}
	select {
	case <-procShutdownCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for fallback process shutdown hook")
	}
}

func TestConfigurationFieldsHostSanitizationAndHelpersV0110(t *testing.T) {
	t.Cleanup(func() {
		netutil.SetTrustedProxies(nil)
		netutil.SetLocalNetworkSubnets(nil)
	})

	// Unit test helper parsers directly for all type branches.
	if s, ok := parseStringSliceField(nil); !ok || len(s) != 0 {
		t.Fatalf("parseStringSliceField(nil) = %v, %v", s, ok)
	}
	if s, ok := parseStringSliceField([]string{" a ", "", "b"}); !ok || len(s) != 2 || s[0] != "a" || s[1] != "b" {
		t.Fatalf("parseStringSliceField([]string) = %v, %v", s, ok)
	}
	if s, ok := parseStringSliceField([]any{" x ", "", "y"}); !ok || len(s) != 2 {
		t.Fatalf("parseStringSliceField([]any) = %v, %v", s, ok)
	}
	if _, ok := parseStringSliceField([]any{"valid", 42}); ok {
		t.Fatal("expected parseStringSliceField([]any with non-string) to fail")
	}
	if s, ok := parseStringSliceField("10.0.0.0/8, 192.168.0.0/16 , "); !ok || len(s) != 2 {
		t.Fatalf("parseStringSliceField(string) = %v, %v", s, ok)
	}
	if _, ok := parseStringSliceField(12345); ok {
		t.Fatal("expected parseStringSliceField(int) to fail")
	}

	if n, ok := parseIntField(float64(7)); !ok || n != 7 {
		t.Fatalf("parseIntField(float64) = %d, %v", n, ok)
	}
	if n, ok := parseIntField(int(9)); !ok || n != 9 {
		t.Fatalf("parseIntField(int) = %d, %v", n, ok)
	}
	if n, ok := parseIntField(int64(11)); !ok || n != 11 {
		t.Fatalf("parseIntField(int64) = %d, %v", n, ok)
	}
	if _, ok := parseIntField("bad"); ok {
		t.Fatal("expected parseIntField(string) to fail")
	}
	if out := nonNilStrings([]string{"https://ambra.local"}); len(out) != 1 || out[0] != "https://ambra.local" {
		t.Fatalf("nonNilStrings non-empty = %v", out)
	}

	// Host sanitization & DNS validation matrix.
	longLabel := strings.Repeat("a", 64) + ".com"
	longHost := strings.Repeat("a.", 130) + "com"
	hostCases := map[string]string{
		"":                        "localhost:8096",
		longHost:                  "localhost:8096",
		"bad\x00host":             "localhost:8096",
		"bad?host":                "localhost:8096",
		"example.com:abc":         "localhost:8096",
		"example.com:70000":       "localhost:8096",
		"[::1]:8443":              "[::1]:8443",
		"192.168.1.20:8096":       "192.168.1.20:8096",
		"media.example.com:9090":  "media.example.com:9090",
		"-invalid.com:8096":       "localhost:8096",
		"[2001:db8::1]":           "[2001:db8::1]",
		"[not-an-ip]":             "localhost:8096",
		"10.1.2.3":                "10.1.2.3",
		"ambra.local":             "ambra.local",
		".leading.dot":            "localhost:8096",
		"trailing.dot.":           "localhost:8096",
		"empty..label":            "localhost:8096",
		longLabel:                 "localhost:8096",
		"bad_char.example.com":    "localhost:8096",
		"trailing-.example.com":   "localhost:8096",
	}
	for raw, want := range hostCases {
		if got := sanitizeRequestHost(raw, ":8096"); got != want {
			t.Errorf("sanitizeRequestHost(%q) = %q, want %q", raw, got, want)
		}
	}
	if isValidDNSName("") {
		t.Fatal("expected empty DNS name to be invalid")
	}

	r, h, store, userRepo, adminTok, _ := setupSystemHandler(t)
	strmSpy := &spySTRMPlugin{}
	h.registry.Register(strmSpy)
	wsHub := ws.NewHub(nil, nil, eventbus.New())
	h.SetWSHub(wsHub)

	// TLS request branch in buildPublicInfo.
	tlsReq := httptest.NewRequest(http.MethodGet, "/System/Info/Public", nil)
	tlsReq.Host = "media.ambra.lan:8443"
	tlsReq.TLS = &tls.ConnectionState{}
	tlsRec := httptest.NewRecorder()
	h.GetPublicSystemInfo(tlsRec, tlsReq)
	if tlsRec.Code != http.StatusOK || !strings.Contains(tlsRec.Body.String(), `"LocalAddress":"https://media.ambra.lan:8443"`) {
		t.Fatalf("TLS PublicSystemInfo unexpected: %d %s", tlsRec.Code, tlsRec.Body.String())
	}

	// Seed legacy typed key in extra config via saveExtraConfig, then update via /System/Configuration/partial (named key="partial" branch).
	if err := h.saveExtraConfig(context.Background(), map[string]any{
		"ServerName": "LegacyExtraServer",
		"CustomFlag": true,
	}); err != nil {
		t.Fatalf("saveExtraConfig: %v", err)
	}
	// Nil userRepo saveExtraConfigLocked branch.
	nilRepoHandler := NewHandler(config.Default(t.TempDir()), nil, nil)
	if err := nilRepoHandler.saveExtraConfig(context.Background(), map[string]any{"x": 1}); err != nil {
		t.Fatalf("nil repo saveExtraConfig: %v", err)
	}

	updateBody := map[string]any{
		"ServerName":               "Updated Ambra",
		"TMDBApiKey":               "",
		"EnableSTRM302":            true,
		"EnableDLNA":               true,
		"DLNADefaultUserID":        "u-dlna",
		"EnableQuickConnect":       true,
		"EnableRemoteAccess":       false,
		"AllowedOrigins":           []any{"https://app.ambra.lan"},
		"TrustedProxies":           "127.0.0.1, 10.0.0.1",
		"LocalNetworkSubnets":      []any{"192.168.0.0/16"},
		"SimultaneousStreamLimit":  3,
		"RemoteClientBitrateLimit": 20000000,
		"FFmpegPath":               "/usr/local/bin/ffmpeg",
		"FFprobePath":              "/usr/local/bin/ffprobe",
	}
	rawBody, _ := json.Marshal(updateBody)
	// Route /System/Configuration/{Key} with Key="PARTIAL" (uppercase) hits UpdateNamedConfiguration -> key=="partial" -> UpdateConfigurationPartial!
	req := httptest.NewRequest(http.MethodPost, "/System/Configuration/PARTIAL", bytes.NewReader(rawBody))
	req.Header.Set("X-Emby-Token", adminTok)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST /System/Configuration/PARTIAL expected 204, got %d %s", rec.Code, rec.Body.String())
	}
	if !strmSpy.configured {
		t.Fatal("expected STRM plugin Configure to be invoked when EnableSTRM302 changed")
	}

	// Verify admin GET /System/Configuration from LAN/loopback reflects all updated network/DLNA/FFmpeg fields and cleared TMDBApiKey.
	req = httptest.NewRequest(http.MethodGet, "/System/Configuration", nil)
	req.RemoteAddr = "192.168.1.10:12345"
	req.Header.Set("X-Emby-Token", adminTok)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /System/Configuration expected 200, got %d", rec.Code)
	}
	var gotCfg map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &gotCfg); err != nil {
		t.Fatal(err)
	}
	if _, hasKey := gotCfg["TMDBApiKey"]; hasKey {
		t.Fatalf("expected empty TMDBApiKey to be omitted, got %v", gotCfg["TMDBApiKey"])
	}
	if gotCfg["DLNADefaultUserID"] != "u-dlna" || gotCfg["FFmpegPath"] != "ffmpeg" || gotCfg["FFprobePath"] != "ffprobe" || gotCfg["EnableRemoteAccess"] != false {
		t.Fatalf("unexpected updated configuration: %#v", gotCfg)
	}

	// ActivityLog query fallback parameter ?q=
	_ = userRepo.CreateActivityLog(context.Background(), &sqlite.ActivityLogRecord{
		Name: "QuickSearchHit", Type: "System", Severity: "Info",
	})
	req = httptest.NewRequest(http.MethodGet, "/System/ActivityLog/Entries?q=QuickSearchHit", nil)
	req.RemoteAddr = "192.168.1.10:12345"
	req.Header.Set("X-Emby-Token", adminTok)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "QuickSearchHit") {
		t.Fatalf("GET /System/ActivityLog/Entries?q= failed: %d %s", rec.Code, rec.Body.String())
	}

	// Close underlying SQLite store to cover DB error branches in GetNamedConfiguration, UpdateNamedConfiguration, and GetActivityLogEntries.
	_ = store.Close()
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/System/Configuration/branding", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("Key", "branding")
	req = req.WithContext(context.WithValue(context.Background(), chi.RouteCtxKey, rctx))
	h.GetNamedConfiguration(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on closed DB GetNamedConfiguration, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/System/Configuration/branding", bytes.NewBufferString(`{"a":1}`))
	req = req.WithContext(context.WithValue(context.Background(), chi.RouteCtxKey, rctx))
	h.UpdateNamedConfiguration(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on closed DB UpdateNamedConfiguration, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.GetActivityLogEntries(rec, httptest.NewRequest(http.MethodGet, "/System/ActivityLog/Entries", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on closed DB GetActivityLogEntries, got %d", rec.Code)
	}
}

func TestSetupRateLimitAndExistingUserPromotionV0110(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(filepath.Join(dir, "setup_cov.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	userRepo := sqlite.NewUserRepo(store)
	authSvc := auth.NewService(userRepo, cfg)
	reg := plugins.NewRegistry()
	h := NewHandler(cfg, userRepo, reg).WithSetupDeps(SetupDeps{Auth: authSvc})

	// Missing Auth service branch -> 500
	hNoAuth := NewHandler(cfg, userRepo, nil)
	rec := httptest.NewRecorder()
	hNoAuth.SetupAdmin(rec, httptest.NewRequest(http.MethodPost, "/api/setup/admin", bytes.NewBufferString(`{}`)))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when Auth service nil, got %d", rec.Code)
	}

	// Validation branches on SetupAdmin: invalid JSON, control chars, empty username, short password.
	for _, body := range []string{
		`{invalid`,
		`{"username":"bad\u0000user","password":"secret"}`,
		`{"username":"   ","password":"secret"}`,
		`{"username":"admin","password":"123"}`,
	} {
		rec = httptest.NewRecorder()
		h.SetupAdmin(rec, httptest.NewRequest(http.MethodPost, "/api/setup/admin", bytes.NewBufferString(body)))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for body %s, got %d", body, rec.Code)
		}
	}

	// Rate limit check for non-loopback IP with stale entry pruning.
	h.setupRates = map[string][]time.Time{
		"198.51.100.1": {time.Now().UTC().Add(-5 * time.Minute)},
	}
	for i := 0; i < setupAdminRateLimit; i++ {
		rq := httptest.NewRequest(http.MethodPost, "/api/setup/admin", nil)
		rq.RemoteAddr = "203.0.113.55:40000"
		if !h.checkSetupRateLimit(rq) {
			t.Fatalf("expected rate limit check %d to pass", i)
		}
	}
	blockedReq := httptest.NewRequest(http.MethodPost, "/api/setup/admin", bytes.NewBufferString(`{}`))
	blockedReq.RemoteAddr = "203.0.113.55:40000"
	rec = httptest.NewRecorder()
	h.SetupAdmin(rec, blockedReq)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on exceeded setup rate limit, got %d", rec.Code)
	}

	// Create a non-admin user first, plus an authenticated admin context, then invoke SetupAdmin for the existing non-admin user
	// to cover ErrUserExists password reset + admin promotion branch!
	ctx := context.Background()
	adminRec, err := authSvc.CreateUser(ctx, "rootadmin", "rootpass", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authSvc.CreateUser(ctx, "promote_me", "oldpass", false); err != nil {
		t.Fatal(err)
	}
	authMW := middleware.NewAuth(authSvc, nil)
	router := chi.NewRouter()
	router.Use(authMW.ParseAuth)
	RegisterSetupRoutes(router, authMW, h)

	adminLogin, err := authSvc.AuthenticateByName(ctx, adminRec.Name, "rootpass", "", auth.ClientInfo{DeviceID: "d1"})
	if err != nil {
		t.Fatal(err)
	}

	promoReq := httptest.NewRequest(http.MethodPost, "/api/setup/admin", bytes.NewBufferString(`{
		"server_name":"Promoted Server",
		"language":"ja-JP",
		"country":"jp",
		"username":"promote_me",
		"password":"newpassword"
	}`))
	promoReq.RemoteAddr = "127.0.0.1:12345"
	promoReq.Header.Set("X-Emby-Token", adminLogin.AccessToken)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, promoReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("SetupAdmin existing user promotion failed: %d %s", rec.Code, rec.Body.String())
	}
	promoted, err := userRepo.GetUserByName(ctx, "promote_me")
	if err != nil || !promoted.IsAdmin || !promoted.Policy.IsAdministrator {
		t.Fatalf("expected promote_me to become admin, got %+v err=%v", promoted, err)
	}
}
