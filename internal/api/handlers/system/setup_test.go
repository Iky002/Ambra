package system

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

type spyTMDBPlugin struct {
	mu       sync.Mutex
	lastCfg  map[string]any
	calls    int
	apiKey   string
	language string
}

func (s *spyTMDBPlugin) Manifest() plugins.Manifest {
	return plugins.Manifest{
		ID:       tmdbPluginID,
		Name:     "TMDB Spy",
		Version:  "1.0.0",
		BuiltIn:  true,
		Enabled:  true,
		Priority: 20,
		Kinds:    []plugins.Kind{plugins.KindMetadata},
	}
}

func (s *spyTMDBPlugin) Configure(cfg map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.lastCfg = cfg
	if v, ok := cfg["api_key"].(string); ok {
		s.apiKey = v
	}
	if v, ok := cfg["language"].(string); ok {
		s.language = v
	}
	return nil
}

func (s *spyTMDBPlugin) snapshot() (calls int, apiKey, language string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls, s.apiKey, s.language
}

type setupTestEnv struct {
	router   chi.Router
	handler  *Handler
	cfg      *config.Config
	userRepo *sqlite.UserRepo
	authSvc  *auth.Service
	tmdbSpy  *spyTMDBPlugin
	scanCh   chan struct{}
	rankCh   chan struct{}
}

func newSetupTestEnv(t *testing.T) *setupTestEnv {
	t.Helper()
	dir := t.TempDir()
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(filepath.Join(dir, "setup.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	userRepo := sqlite.NewUserRepo(store)
	authSvc := auth.NewService(userRepo, cfg)
	reg := plugins.NewRegistry()
	spy := &spyTMDBPlugin{}
	reg.Register(spy)

	scanCh := make(chan struct{}, 1)
	rankCh := make(chan struct{}, 1)

	h := NewHandler(cfg, userRepo, reg).WithSetupDeps(SetupDeps{
		Auth: authSvc,
		TriggerScan: func(_ context.Context) {
			select {
			case scanCh <- struct{}{}:
			default:
			}
		},
		SyncRankings: func(_ context.Context) {
			select {
			case rankCh <- struct{}{}:
			default:
			}
		},
	})

	authMW := middleware.NewAuth(authSvc, nil)
	r := chi.NewRouter()
	r.Use(authMW.ParseAuth)
	RegisterRoutes(r, authMW, h)
	RegisterSetupRoutes(r, authMW, h)

	return &setupTestEnv{
		router:   r,
		handler:  h,
		cfg:      cfg,
		userRepo: userRepo,
		authSvc:  authSvc,
		tmdbSpy:  spy,
		scanCh:   scanCh,
		rankCh:   rankCh,
	}
}

func TestSetupWizardFlow(t *testing.T) {
	env := newSetupTestEnv(t)

	// 1. Initial GET /api/setup/status (no users, not completed, no TMDB key)
	env.cfg.TMDBLanguage = "" // exercise default zh-CN fallback
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/setup/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/setup/status: expected 200, got %d %s", rec.Code, rec.Body.String())
	}
	var st SetupStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.Completed || st.HasUsers || st.HasTMDBKey || st.DefaultLanguage != "zh-CN" {
		t.Fatalf("unexpected initial setup status: %+v", st)
	}

	// 2. POST /api/setup/admin validation errors
	for _, tc := range []struct {
		name string
		body string
	}{
		{"invalid json", `{bad`},
		{"empty username", `{"username":"  ","password":"secret123"}`},
		{"short password", `{"username":"admin","password":"123"}`},
	} {
		rec = httptest.NewRecorder()
		env.router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/setup/admin", bytes.NewBufferString(tc.body)))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: expected 400, got %d %s", tc.name, rec.Code, rec.Body.String())
		}
	}

	// 3. POST /api/setup/admin creates admin and returns valid AccessToken
	adminBody := `{"server_name":"My Cinema","language":"en-US","country":"US","username":"founder","password":"strongpassword"}`
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/setup/admin", bytes.NewBufferString(adminBody)))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/setup/admin: expected 200, got %d %s", rec.Code, rec.Body.String())
	}
	var authRes dto.AuthenticationResult
	if err := json.Unmarshal(rec.Body.Bytes(), &authRes); err != nil {
		t.Fatal(err)
	}
	if authRes.AccessToken == "" || authRes.ServerId != env.cfg.ServerID || authRes.User.Name != "founder" || !authRes.User.Policy.IsAdministrator {
		t.Fatalf("unexpected SetupAdmin auth result: %+v", authRes)
	}
	if env.cfg.ServerName != "My Cinema" || env.cfg.TMDBLanguage != "en-US" {
		t.Fatalf("config not updated: name=%q lang=%q", env.cfg.ServerName, env.cfg.TMDBLanguage)
	}
	// Verify country was persisted to system configuration
	cfgReq := httptest.NewRequest(http.MethodGet, "/System/Configuration", nil)
	cfgReq.Header.Set("X-Emby-Token", authRes.AccessToken)
	cfgRec := httptest.NewRecorder()
	env.router.ServeHTTP(cfgRec, cfgReq)
	if cfgRec.Code != http.StatusOK || !strings.Contains(cfgRec.Body.String(), `"MetadataCountryCode":"US"`) {
		t.Fatalf("expected country US persisted in /System/Configuration, got %d %s", cfgRec.Code, cfgRec.Body.String())
	}
	if calls, _, lang := env.tmdbSpy.snapshot(); calls != 1 || lang != "en-US" {
		t.Fatalf("expected TMDB plugin language hot-reload, got calls=%d lang=%q", calls, lang)
	}

	// 4. POST /api/setup/complete requires admin auth
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/setup/complete", bytes.NewBufferString(`{}`)))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated complete, got %d", rec.Code)
	}

	// Non-admin user -> 403
	if _, err := env.authSvc.CreateUser(context.Background(), "viewer", "viewerpass", false); err != nil {
		t.Fatal(err)
	}
	viewerAuth, err := env.authSvc.AuthenticateByName(context.Background(), "viewer", "viewerpass", "", auth.ClientInfo{DeviceID: "d-v"})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/setup/complete", bytes.NewBufferString(`{}`))
	req.Header.Set("X-Emby-Token", viewerAuth.AccessToken)
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for viewer complete, got %d", rec.Code)
	}

	// Invalid JSON -> 400
	req = httptest.NewRequest(http.MethodPost, "/api/setup/complete", bytes.NewBufferString(`{bad`))
	req.Header.Set("X-Emby-Token", authRes.AccessToken)
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid complete JSON, got %d", rec.Code)
	}

	// 5. POST /api/setup/complete with full payload persists config, hot-reloads TMDB, triggers async scan & rankings
	completeBody := `{"server_name":"Ambra Home","tmdb_api_key":"tmdb-secret-key","tmdb_language":"zh-CN","enable_strm_302":false,"trigger_scan":true,"sync_rankings":true}`
	req = httptest.NewRequest(http.MethodPost, "/api/setup/complete", bytes.NewBufferString(completeBody))
	req.Header.Set("X-Emby-Token", authRes.AccessToken)
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"completed":true`) {
		t.Fatalf("POST /api/setup/complete: expected 200 completed=true, got %d %s", rec.Code, rec.Body.String())
	}

	// Verify config persisted on disk
	reloaded, err := config.Load(env.cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.IsStartupWizardCompleted || reloaded.ServerName != "Ambra Home" || reloaded.TMDBApiKey != "tmdb-secret-key" || reloaded.TMDBLanguage != "zh-CN" || reloaded.EnableSTRM302 {
		t.Fatalf("persisted config mismatch: %+v", reloaded)
	}

	// Verify TMDB plugin hot-reloaded
	if calls, key, lang := env.tmdbSpy.snapshot(); calls != 2 || key != "tmdb-secret-key" || lang != "zh-CN" {
		t.Fatalf("expected TMDB hot-reload after complete, got calls=%d key=%q lang=%q", calls, key, lang)
	}

	// Verify async callbacks fired
	select {
	case <-env.scanCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for TriggerScan callback")
	}
	select {
	case <-env.rankCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for SyncRankings callback")
	}

	// 6. GET /api/setup/status after completion (never leaks raw TMDB key)
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/setup/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/setup/status after complete: %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "tmdb-secret-key") {
		t.Fatalf("setup status leaked raw TMDB API key: %s", rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if !st.Completed || !st.HasUsers || !st.HasTMDBKey || st.ServerName != "Ambra Home" || st.DefaultLanguage != "zh-CN" {
		t.Fatalf("unexpected post-complete status: %+v", st)
	}

	// 7. POST /api/setup/admin MUST return 403 Forbidden once completed
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/setup/admin", bytes.NewBufferString(adminBody)))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for SetupAdmin after completion, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestSetupAdminUpdatesExistingUserAndPromotes(t *testing.T) {
	env := newSetupTestEnv(t)
	ctx := context.Background()

	// Pre-seed a non-admin user (simulates existing user while wizard is incomplete)
	if _, err := env.authSvc.CreateUser(ctx, "targetuser", "oldpass", false); err != nil {
		t.Fatal(err)
	}
	nonAdminAuth, err := env.authSvc.AuthenticateByName(ctx, "targetuser", "oldpass", "", auth.ClientInfo{DeviceID: "d-na"})
	if err != nil {
		t.Fatal(err)
	}

	body := `{"username":"targetuser","password":"newpassword123"}`

	// Unauthenticated POST /api/setup/admin when users already exist MUST be rejected with 403
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/setup/admin", bytes.NewBufferString(body)))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for unauthenticated SetupAdmin when users exist, got %d %s", rec.Code, rec.Body.String())
	}

	// Non-admin authenticated POST /api/setup/admin MUST also be rejected with 403
	reqNonAdmin := httptest.NewRequest(http.MethodPost, "/api/setup/admin", bytes.NewBufferString(body))
	reqNonAdmin.Header.Set("X-Emby-Token", nonAdminAuth.AccessToken)
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, reqNonAdmin)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin SetupAdmin when users exist, got %d %s", rec.Code, rec.Body.String())
	}

	// Authenticated admin CAN update/promote an existing user while wizard is incomplete
	if _, err := env.authSvc.CreateUser(ctx, "rootadmin", "rootpass", true); err != nil {
		t.Fatal(err)
	}
	adminAuth, err := env.authSvc.AuthenticateByName(ctx, "rootadmin", "rootpass", "", auth.ClientInfo{DeviceID: "d-adm"})
	if err != nil {
		t.Fatal(err)
	}
	reqAdmin := httptest.NewRequest(http.MethodPost, "/api/setup/admin", bytes.NewBufferString(body))
	reqAdmin.Header.Set("X-Emby-Token", adminAuth.AccessToken)
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, reqAdmin)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 updating existing user as authenticated admin, got %d %s", rec.Code, rec.Body.String())
	}
	var authRes dto.AuthenticationResult
	if err := json.Unmarshal(rec.Body.Bytes(), &authRes); err != nil {
		t.Fatal(err)
	}
	if authRes.AccessToken == "" || !authRes.User.Policy.IsAdministrator {
		t.Fatalf("expected promoted admin with token, got %+v", authRes)
	}

	// Empty body complete should also succeed
	req := httptest.NewRequest(http.MethodPost, "/api/setup/complete", nil)
	req.Header.Set("X-Emby-Token", authRes.AccessToken)
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for empty body complete, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateConfigurationHotReloadsTMDB(t *testing.T) {
	env := newSetupTestEnv(t)
	ctx := context.Background()

	if _, err := env.authSvc.CreateUser(ctx, "admin", "adminpass", true); err != nil {
		t.Fatal(err)
	}
	adminAuth, err := env.authSvc.AuthenticateByName(ctx, "admin", "adminpass", "", auth.ClientInfo{DeviceID: "d-1"})
	if err != nil {
		t.Fatal(err)
	}

	// Update TMDBApiKey & TMDBLanguage via /System/Configuration
	req := httptest.NewRequest(http.MethodPost, "/System/Configuration", bytes.NewBufferString(`{"TMDBApiKey":"live-key-999","TMDBLanguage":"ja-JP"}`))
	req.Header.Set("X-Emby-Token", adminAuth.AccessToken)
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("UpdateConfiguration: expected 204, got %d %s", rec.Code, rec.Body.String())
	}

	calls, key, lang := env.tmdbSpy.snapshot()
	if calls != 1 || key != "live-key-999" || lang != "ja-JP" {
		t.Fatalf("expected TMDB hot-reload from UpdateConfiguration, got calls=%d key=%q lang=%q", calls, key, lang)
	}

	// Nil authSvc guard in SetupAdmin
	cfg := config.Default(t.TempDir())
	h := NewHandler(cfg, nil, nil)
	rec = httptest.NewRecorder()
	h.SetupAdmin(rec, httptest.NewRequest(http.MethodPost, "/api/setup/admin", bytes.NewBufferString(`{"username":"a","password":"1234"}`)))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when authSvc is nil, got %d", rec.Code)
	}
	// syncTMDBPlugin safe with nil registry
	h.syncTMDBPlugin()
}

func TestSetupErrorPaths(t *testing.T) {
	// 1. cfg.Save() failure in SetupAdmin & CompleteSetup (parent path is a regular file)
	blockingFile := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blockingFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	badDir := filepath.Join(blockingFile, "nested")
	badCfg := config.Default(badDir)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "err.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	userRepo := sqlite.NewUserRepo(store)
	authSvc := auth.NewService(userRepo, badCfg)

	hBad := NewHandler(badCfg, userRepo, nil).WithSetupDeps(SetupDeps{Auth: authSvc})

	// CompleteSetup with 0 users must return 400 (H-06)
	rec := httptest.NewRecorder()
	hBad.CompleteSetup(rec, httptest.NewRequest(http.MethodPost, "/api/setup/complete", bytes.NewBufferString(`{}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on CompleteSetup with 0 users, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	hBad.SetupAdmin(rec, httptest.NewRequest(http.MethodPost, "/api/setup/admin", bytes.NewBufferString(`{"username":"admin","password":"password123"}`)))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on SetupAdmin Save failure, got %d", rec.Code)
	}

	// Seed a user so CompleteSetup reaches cfg.Save() and returns 500 on badCfg
	if _, err := authSvc.CreateUser(context.Background(), "seedadmin", "password123", true); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	hBad.CompleteSetup(rec, httptest.NewRequest(http.MethodPost, "/api/setup/complete", bytes.NewBufferString(`{}`)))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on CompleteSetup Save failure, got %d", rec.Code)
	}

	// 2. Existing user: unauthenticated -> 403; authenticated admin with nil h.userRepo -> 500
	goodCfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dupUser, err := authSvc.CreateUser(context.Background(), "dupadmin", "oldpass", true)
	if err != nil {
		t.Fatal(err)
	}
	hNilRepo := NewHandler(goodCfg, nil, nil).WithSetupDeps(SetupDeps{Auth: authSvc})
	rec = httptest.NewRecorder()
	hNilRepo.SetupAdmin(rec, httptest.NewRequest(http.MethodPost, "/api/setup/admin", bytes.NewBufferString(`{"username":"dupadmin","password":"newpassword"}`)))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for unauthenticated duplicate user on nil userRepo, got %d", rec.Code)
	}

	adminReq := httptest.NewRequest(http.MethodPost, "/api/setup/admin", bytes.NewBufferString(`{"username":"dupadmin","password":"newpassword"}`))
	adminReq = adminReq.WithContext(middleware.WithAuthContext(adminReq.Context(), &middleware.AuthContext{User: dupUser}))
	rec = httptest.NewRecorder()
	hNilRepo.SetupAdmin(rec, adminReq)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when userRepo is nil on authenticated admin existing user, got %d", rec.Code)
	}

	// 3. Closed DB error on CreateUser -> 500
	_ = store.Close()
	hClosed := NewHandler(goodCfg, userRepo, nil).WithSetupDeps(SetupDeps{Auth: authSvc})
	rec = httptest.NewRecorder()
	hClosed.SetupAdmin(rec, httptest.NewRequest(http.MethodPost, "/api/setup/admin", bytes.NewBufferString(`{"username":"fresh","password":"newpassword"}`)))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on closed DB CreateUser, got %d", rec.Code)
	}
}

func TestSyncTMDBPluginPreservesSQLiteKeyWhenEnvOverridden(t *testing.T) {
	t.Setenv("AMBRA_TMDB_API_KEY", "env-tmdb-override-key")
	dir := t.TempDir()
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(filepath.Join(dir, "tmdb_env.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	userRepo := sqlite.NewUserRepo(store)

	// Seed existing SQLite plugin config with a saved key.
	if err := userRepo.SetServerConfig(context.Background(), "plugin:"+tmdbPluginID, `{"api_key":"saved-db-key","language":"en-US"}`); err != nil {
		t.Fatal(err)
	}

	reg := plugins.NewRegistry()
	spy := &spyTMDBPlugin{}
	reg.Register(spy)

	h := NewHandler(cfg, userRepo, reg)
	cfg.Update(func(c *config.Config) {
		c.TMDBLanguage = "fr-FR"
	})
	h.syncTMDBPlugin()

	calls, liveKey, liveLang := spy.snapshot()
	if calls != 1 || liveKey != "env-tmdb-override-key" || liveLang != "fr-FR" {
		t.Fatalf("expected live plugin configured with env key and fr-FR, got calls=%d key=%q lang=%q", calls, liveKey, liveLang)
	}

	raw, err := userRepo.GetServerConfig(context.Background(), "plugin:"+tmdbPluginID)
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	if err := json.Unmarshal([]byte(raw), &saved); err != nil {
		t.Fatal(err)
	}
	if saved["api_key"] != "saved-db-key" {
		t.Fatalf("expected SQLite api_key preserved as saved-db-key, got %v", saved["api_key"])
	}
	if saved["language"] != "fr-FR" {
		t.Fatalf("expected SQLite language updated to fr-FR, got %v", saved["language"])
	}
}

