package system

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

type dummyPlugin struct{}

func (dummyPlugin) Manifest() plugins.Manifest {
	return plugins.Manifest{ID: "dummy", Name: "Dummy", Version: "1.0", Enabled: true}
}
func (dummyPlugin) Configure(_ map[string]any) error { return nil }

func setupSystemHandler(t *testing.T) (chi.Router, *Handler, *sqlite.Store, *sqlite.UserRepo, string, string) {
	t.Helper()
	dir := t.TempDir()
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(filepath.Join(dir, "sys.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	userRepo := sqlite.NewUserRepo(store)
	authSvc := auth.NewService(userRepo, cfg)
	ctx := context.Background()

	if _, err := authSvc.CreateUser(ctx, "admin", "pw", true); err != nil {
		t.Fatal(err)
	}
	if _, err := authSvc.CreateUser(ctx, "viewer", "pw", false); err != nil {
		t.Fatal(err)
	}
	adminAuth, err := authSvc.AuthenticateByName(ctx, "admin", "pw", "", auth.ClientInfo{DeviceID: "d-a"})
	if err != nil {
		t.Fatal(err)
	}
	viewerAuth, err := authSvc.AuthenticateByName(ctx, "viewer", "pw", "", auth.ClientInfo{DeviceID: "d-v"})
	if err != nil {
		t.Fatal(err)
	}

	reg := plugins.NewRegistry()
	reg.Register(dummyPlugin{})

	h := NewHandler(cfg, userRepo, reg)
	r := chi.NewRouter()
	RegisterRoutes(r, middleware.NewAuth(authSvc, nil), h)
	return r, h, store, userRepo, adminAuth.AccessToken, viewerAuth.AccessToken
}

func TestPublicSystemRoutes(t *testing.T) {
	r, _, _, _, _, _ := setupSystemHandler(t)

	// /System/Info/Public with X-Forwarded-Proto and empty Host
	req := httptest.NewRequest(http.MethodGet, "/System/Info/Public", nil)
	req.Host = ""
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "https://localhost:8096") {
		t.Fatalf("PublicSystemInfo failed: %d %s", rec.Code, rec.Body.String())
	}

	// /System/Ping GET, HEAD, POST
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(method, "/System/Ping", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("Ping %s: expected 200, got %d", method, rec.Code)
		}
		if method == http.MethodHead && rec.Body.Len() != 0 {
			t.Fatalf("Ping HEAD expected empty body, got %q", rec.Body.String())
		}
		if method != http.MethodHead && rec.Body.String() != "Ambra Server" {
			t.Fatalf("Ping %s body = %q", method, rec.Body.String())
		}
	}

	// Localization, Registrations, Features
	for _, path := range []string{
		"/Branding/Configuration",
		"/Branding/Css",
		"/Branding/Css.css",
		"/Localization/Cultures",
		"/Localization/Countries",
		"/Localization/Options",
		"/Localization/ParentalRatings",
		"/Registrations/Ambra",
		"/Features",
	} {
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s: expected 200, got %d", path, rec.Code)
		}
	}
}

func TestAuthenticatedAndAdminSystemRoutes(t *testing.T) {
	r, _, _, userRepo, adminTok, viewerTok := setupSystemHandler(t)
	ctx := context.Background()

	// Unauthenticated -> 401
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/System/Info", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}

	// GET /System/Info
	req := httptest.NewRequest(http.MethodGet, "/System/Info", nil)
	req.Header.Set("X-Emby-Token", viewerTok)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "HttpServerPortNumber") {
		t.Fatalf("GetSystemInfo failed: %d %s", rec.Code, rec.Body.String())
	}

	// GET /System/Configuration
	req = httptest.NewRequest(http.MethodGet, "/System/Configuration", nil)
	req.Header.Set("X-Emby-Token", viewerTok)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GetConfiguration failed: %d", rec.Code)
	}

	// POST /System/Configuration as viewer -> 403
	req = httptest.NewRequest(http.MethodPost, "/System/Configuration", bytes.NewBufferString(`{"ServerName":"Hacked"}`))
	req.Header.Set("X-Emby-Token", viewerTok)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for viewer UpdateConfiguration, got %d", rec.Code)
	}

	// POST /System/Configuration invalid JSON -> 400
	req = httptest.NewRequest(http.MethodPost, "/System/Configuration", bytes.NewBufferString(`{bad`))
	req.Header.Set("X-Emby-Token", adminTok)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}

	// POST /System/Configuration valid -> 204
	payload := `{"ServerName":"My Ambra","IsStartupWizardCompleted":true,"TMDBApiKey":"k123","TMDBLanguage":"en-US","EnableSTRM302":false,"PlayedThreshold":0.85}`
	req = httptest.NewRequest(http.MethodPost, "/System/Configuration", bytes.NewBufferString(payload))
	req.Header.Set("X-Emby-Token", adminTok)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("UpdateConfiguration valid: expected 204, got %d %s", rec.Code, rec.Body.String())
	}

	// GET /System/Configuration/{Key} default empty {}
	req = httptest.NewRequest(http.MethodGet, "/System/Configuration/branding", nil)
	req.Header.Set("X-Emby-Token", adminTok)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "{}" {
		t.Fatalf("GetNamedConfiguration default: %d %s", rec.Code, rec.Body.String())
	}

	// POST /System/Configuration/{Key} invalid JSON -> 400
	req = httptest.NewRequest(http.MethodPost, "/System/Configuration/branding", bytes.NewBufferString(`not-json`))
	req.Header.Set("X-Emby-Token", adminTok)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid named config, got %d", rec.Code)
	}

	// POST /System/Configuration/{Key} empty body -> defaults to {} -> 204
	req = httptest.NewRequest(http.MethodPost, "/System/Configuration/emptykey", nil)
	req.Header.Set("X-Emby-Token", adminTok)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for empty body named config, got %d", rec.Code)
	}

	// POST /System/Configuration/branding with actual branding JSON -> then verify Branding endpoints
	brandingJSON := `{"LoginDisclaimer":"Welcome!","CustomCss":"body{color:red;}"}`
	req = httptest.NewRequest(http.MethodPost, "/System/Configuration/branding", bytes.NewBufferString(brandingJSON))
	req.Header.Set("X-Emby-Token", adminTok)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("UpdateNamedConfiguration branding: expected 204, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/System/Configuration/branding", nil)
	req.Header.Set("X-Emby-Token", adminTok)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Welcome!") {
		t.Fatalf("GetNamedConfiguration branding: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Branding/Configuration", nil))
	if !strings.Contains(rec.Body.String(), "Welcome!") {
		t.Fatalf("Branding/Configuration mismatch: %s", rec.Body.String())
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Branding/Css", nil))
	if rec.Body.String() != "body{color:red;}" {
		t.Fatalf("Branding/Css mismatch: %q", rec.Body.String())
	}

	// ActivityLogEntries
	_ = userRepo.CreateActivityLog(ctx, &sqlite.ActivityLogRecord{
		Name: "Login", Type: "Auth", Severity: "Info",
	})
	minDate := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	req = httptest.NewRequest(http.MethodGet, "/System/ActivityLog/Entries?StartIndex=0&Limit=10&MinDate="+minDate, nil)
	req.Header.Set("X-Emby-Token", adminTok)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Login") {
		t.Fatalf("GetActivityLogEntries failed: %d %s", rec.Code, rec.Body.String())
	}

	// GET /Plugins
	req = httptest.NewRequest(http.MethodGet, "/Plugins", nil)
	req.Header.Set("X-Emby-Token", adminTok)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	var plist []plugins.Manifest
	if err := json.Unmarshal(rec.Body.Bytes(), &plist); err != nil || len(plist) != 1 {
		t.Fatalf("GetPlugins failed: %d %s (%v)", rec.Code, rec.Body.String(), err)
	}
}

func TestSystemHandlerHelpersAndNilDeps(t *testing.T) {
	// Nil userRepo and nil registry
	cfg := config.Default(t.TempDir())
	h := NewHandler(cfg, nil, nil)

	rec := httptest.NewRecorder()
	h.GetPlugins(rec, httptest.NewRequest(http.MethodGet, "/Plugins", nil))
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("GetPlugins nil registry: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.GetActivityLogEntries(rec, httptest.NewRequest(http.MethodGet, "/System/ActivityLog/Entries", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GetActivityLogEntries nil repo: %d", rec.Code)
	}

	// Missing Key param directly
	rec = httptest.NewRecorder()
	h.GetNamedConfiguration(rec, httptest.NewRequest(http.MethodGet, "/System/Configuration/", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty Key, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.UpdateNamedConfiguration(rec, httptest.NewRequest(http.MethodPost, "/System/Configuration/", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty Key on update, got %d", rec.Code)
	}

	// Helper unit tests
	if got := parsePort(":9000", 8096); got != 9000 {
		t.Fatalf("parsePort(:9000) = %d", got)
	}
	if got := parsePort("invalid", 8096); got != 8096 {
		t.Fatalf("parsePort(invalid) = %d", got)
	}
	if got := parsePort(":0", 8096); got != 8096 {
		t.Fatalf("parsePort(:0) = %d", got)
	}
	if got := parsePort(":abc", 8096); got != 8096 {
		t.Fatalf("parsePort(:abc) = %d", got)
	}

	for goos, want := range map[string]string{
		"darwin":  "macOS",
		"linux":   "Linux",
		"windows": "Windows",
		"freebsd": "freebsd",
	} {
		if got := osDisplayName(goos); got != want {
			t.Errorf("osDisplayName(%q) = %q, want %q", goos, got, want)
		}
	}
}

func TestSystemConfigMaskingAndActivityLogAdminOnly(t *testing.T) {
	r, _, _, userRepo, adminTok, viewerTok := setupSystemHandler(t)
	ctx := context.Background()

	// Configure a TMDB API key as admin.
	req := httptest.NewRequest(http.MethodPost, "/System/Configuration", bytes.NewBufferString(`{"TMDBApiKey":"secret-tmdb-key"}`))
	req.Header.Set("X-Emby-Token", adminTok)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("UpdateConfiguration expected 204, got %d", rec.Code)
	}

	// Non-admin GET /System/Configuration must NOT receive TMDBApiKey.
	req = httptest.NewRequest(http.MethodGet, "/System/Configuration", nil)
	req.Header.Set("X-Emby-Token", viewerTok)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("viewer GetConfiguration expected 200, got %d", rec.Code)
	}
	var viewerCfg ServerConfigurationDto
	if err := json.Unmarshal(rec.Body.Bytes(), &viewerCfg); err != nil {
		t.Fatal(err)
	}
	if viewerCfg.TMDBApiKey != "" {
		t.Fatalf("expected TMDBApiKey masked for non-admin, got %q", viewerCfg.TMDBApiKey)
	}

	// Admin GET /System/Configuration receives the configured TMDBApiKey.
	req = httptest.NewRequest(http.MethodGet, "/System/Configuration", nil)
	req.Header.Set("X-Emby-Token", adminTok)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin GetConfiguration expected 200, got %d", rec.Code)
	}
	var adminCfg ServerConfigurationDto
	if err := json.Unmarshal(rec.Body.Bytes(), &adminCfg); err != nil {
		t.Fatal(err)
	}
	if adminCfg.TMDBApiKey != "secret-tmdb-key" {
		t.Fatalf("expected TMDBApiKey=secret-tmdb-key for admin, got %q", adminCfg.TMDBApiKey)
	}

	// ActivityLog must be admin-only: viewer -> 403, admin -> 200.
	_ = userRepo.CreateActivityLog(ctx, &sqlite.ActivityLogRecord{
		Name: "AuditEvent", Type: "Auth", Severity: "Info",
	})
	req = httptest.NewRequest(http.MethodGet, "/System/ActivityLog/Entries", nil)
	req.Header.Set("X-Emby-Token", viewerTok)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer GET /System/ActivityLog/Entries expected 403, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/System/ActivityLog/Entries", nil)
	req.Header.Set("X-Emby-Token", adminTok)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "AuditEvent") {
		t.Fatalf("admin GET /System/ActivityLog/Entries expected 200 with AuditEvent, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestSystemConfigurationPersistenceAndExtraFields(t *testing.T) {
	r, _, _, _, adminTok, viewerTok := setupSystemHandler(t)

	// Default fallbacks when not yet set
	req := httptest.NewRequest(http.MethodGet, "/System/Configuration", nil)
	req.Header.Set("X-Emby-Token", adminTok)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET default config: %d", rec.Code)
	}
	var def map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &def); err != nil {
		t.Fatal(err)
	}
	if def["PreferredMetadataLanguage"] != "zh" || def["MetadataCountryCode"] != "CN" || def["UICulture"] != "zh-cn" {
		t.Fatalf("unexpected default localization config: %#v", def)
	}

	// Update localization and extra fields
	body := `{"PreferredMetadataLanguage":"en","MetadataCountryCode":"US","UICulture":"en-us","EnableRemoteAccess":true,"MaxResumePct":90}`
	req = httptest.NewRequest(http.MethodPost, "/System/Configuration", bytes.NewBufferString(body))
	req.Header.Set("X-Emby-Token", adminTok)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST config expected 204, got %d %s", rec.Code, rec.Body.String())
	}

	// Verify persisted on GET
	req = httptest.NewRequest(http.MethodGet, "/System/Configuration", nil)
	req.Header.Set("X-Emby-Token", viewerTok)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET updated config: %d", rec.Code)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["PreferredMetadataLanguage"] != "en" || got["MetadataCountryCode"] != "US" || got["UICulture"] != "en-us" {
		t.Fatalf("localization not persisted: %#v", got)
	}
	if got["EnableRemoteAccess"] != true || got["MaxResumePct"] != float64(90) {
		t.Fatalf("extra config fields not persisted: %#v", got)
	}
}

func TestLocalizationEndpoints10Locales(t *testing.T) {
	r, _, _, _, _, _ := setupSystemHandler(t)

	// 1. /Localization/Cultures must include pt/por and th/tha
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Localization/Cultures", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /Localization/Cultures status=%d", rec.Code)
	}
	var cultures []CultureDto
	if err := json.Unmarshal(rec.Body.Bytes(), &cultures); err != nil {
		t.Fatalf("unmarshal cultures: %v", err)
	}
	cultureMap := make(map[string]string, len(cultures))
	for _, c := range cultures {
		cultureMap[c.TwoLetterISOLanguageName] = c.ThreeLetterISOLanguageName
	}
	for two, three := range map[string]string{
		"zh": "zho", "en": "eng", "ja": "jpn", "ko": "kor",
		"fr": "fra", "de": "deu", "es": "spa", "pt": "por", "th": "tha",
	} {
		if cultureMap[two] != three {
			t.Fatalf("expected culture %s -> %s, got %q", two, three, cultureMap[two])
		}
	}

	// 2. /Localization/Countries must include ES, DE, FR, BR, TH
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Localization/Countries", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /Localization/Countries status=%d", rec.Code)
	}
	var countries []CountryInfoDto
	if err := json.Unmarshal(rec.Body.Bytes(), &countries); err != nil {
		t.Fatalf("unmarshal countries: %v", err)
	}
	countrySet := make(map[string]bool, len(countries))
	for _, c := range countries {
		countrySet[c.TwoLetterISORegionName] = true
	}
	for _, code := range []string{"CN", "HK", "TW", "US", "GB", "JP", "KR", "ES", "DE", "FR", "BR", "TH"} {
		if !countrySet[code] {
			t.Fatalf("expected country %s in /Localization/Countries", code)
		}
	}

	// 3. /Localization/Options must return all 10 canonical UI locales
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Localization/Options", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /Localization/Options status=%d", rec.Code)
	}
	var opts []LocalizationOptionDto
	if err := json.Unmarshal(rec.Body.Bytes(), &opts); err != nil {
		t.Fatalf("unmarshal options: %v", err)
	}
	wantLocales := []string{"zh-CN", "zh-TW", "en-US", "ja-JP", "ko-KR", "es-ES", "de-DE", "fr-FR", "pt-BR", "th-TH"}
	if len(opts) != len(wantLocales) {
		t.Fatalf("expected %d localization options, got %d: %+v", len(wantLocales), len(opts), opts)
	}
	optSet := make(map[string]bool, len(opts))
	for _, o := range opts {
		optSet[o.Value] = true
	}
	for _, loc := range wantLocales {
		if !optSet[loc] {
			t.Fatalf("missing locale %s in /Localization/Options", loc)
		}
	}
}

func TestSystemInfoPathMaskingNamedConfigRBACAndConcurrency(t *testing.T) {
	r, _, _, _, adminTok, viewerTok := setupSystemHandler(t)

	// 1. GET /System/Configuration/{Key} must be admin-only: viewer -> 403, admin -> 200.
	req := httptest.NewRequest(http.MethodGet, "/System/Configuration/branding", nil)
	req.Header.Set("X-Emby-Token", viewerTok)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer GET /System/Configuration/branding expected 403, got %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/System/Configuration/branding", nil)
	req.Header.Set("X-Emby-Token", adminTok)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin GET /System/Configuration/branding expected 200, got %d", rec.Code)
	}

	// 2. GET /System/Info: non-admin must NOT receive internal filesystem paths.
	req = httptest.NewRequest(http.MethodGet, "/System/Info", nil)
	req.Header.Set("X-Emby-Token", viewerTok)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("viewer GET /System/Info expected 200, got %d", rec.Code)
	}
	var viewerInfo map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &viewerInfo); err != nil {
		t.Fatal(err)
	}
	for _, hidden := range []string{"ProgramDataPath", "CachePath", "LogPath", "InternalMetadataPath", "TranscodingTempPath"} {
		if v, exists := viewerInfo[hidden]; exists && v != "" {
			t.Fatalf("non-admin /System/Info leaked %s=%v", hidden, v)
		}
	}
	for _, kept := range []string{"ServerName", "Version", "Id", "ProductName", "LocalAddress"} {
		if v, _ := viewerInfo[kept].(string); v == "" {
			t.Fatalf("non-admin /System/Info missing required field %s", kept)
		}
	}

	// Admin GET /System/Info receives all internal filesystem paths.
	req = httptest.NewRequest(http.MethodGet, "/System/Info", nil)
	req.Header.Set("X-Emby-Token", adminTok)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin GET /System/Info expected 200, got %d", rec.Code)
	}
	var adminInfo map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &adminInfo); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"ProgramDataPath", "CachePath", "LogPath", "InternalMetadataPath", "TranscodingTempPath"} {
		if v, _ := adminInfo[field].(string); v == "" {
			t.Fatalf("admin /System/Info missing %s", field)
		}
	}

	// 3. Concurrent config reads & writes must be race-free under -race.
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				w := httptest.NewRecorder()
				rq := httptest.NewRequest(http.MethodPost, "/System/Configuration", bytes.NewBufferString(`{"ServerName":"RaceTest","TMDBLanguage":"en-US"}`))
				rq.Header.Set("X-Emby-Token", adminTok)
				r.ServeHTTP(w, rq)
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				w := httptest.NewRecorder()
				rq := httptest.NewRequest(http.MethodGet, "/System/Configuration", nil)
				rq.Header.Set("X-Emby-Token", adminTok)
				r.ServeHTTP(w, rq)
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				w := httptest.NewRecorder()
				rq := httptest.NewRequest(http.MethodGet, "/System/Info", nil)
				rq.Header.Set("X-Emby-Token", viewerTok)
				r.ServeHTTP(w, rq)
			}
		}()
	}
	wg.Wait()
}
