package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/scheduler"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/pkg/netutil"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/plugins/tmdb"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestServerLifecycleAndRoutes(t *testing.T) {
	dataDir := t.TempDir()
	cfg, err := config.Load(dataDir)
	if err != nil {
		t.Fatal(err)
	}

	// Place an external plugin manifest to exercise ext.Loader integration
	extDir := filepath.Join(dataDir, "plugins", "demo-ext")
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"Id":"demo-ext","Name":"Demo External","Version":"1.0.0","Endpoint":"http://127.0.0.1:19999","Enabled":true,"Priority":50,"Kinds":["metadata"]}`
	if err := os.WriteFile(filepath.Join(extDir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	srv, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("server.New failed: %v", err)
	}
	defer func() {
		if err := srv.Close(); err != nil {
			t.Fatalf("server.Close failed: %v", err)
		}
	}()

	if len(srv.PluginLoader.Loaded()) != 1 {
		t.Fatalf("expected 1 external plugin loaded, got %d", len(srv.PluginLoader.Loaded()))
	}

	// Root redirect -> /web/
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/web/" {
		t.Fatalf("expected 302 to /web/, got %d %q", rec.Code, rec.Header().Get("Location"))
	}

	// Root favicon
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/favicon.ico", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<svg") {
		t.Fatalf("expected SVG favicon, got %d", rec.Code)
	}

	// Web index and SPA fallback
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/web/", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<html") {
		t.Fatalf("expected embedded index.html, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/web/nonexistent-spa-route", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<html") {
		t.Fatalf("expected SPA fallback to index.html, got %d", rec.Code)
	}

	// Public Phase 4-6 routes via delegated mediaMux (DLNA requires LAN origin)
	for _, p := range []string{"/openapi.json", "/robots.txt", "/Dlna/test-uuid/description.xml"} {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		req.RemoteAddr = "127.0.0.1:12345"
		rec = httptest.NewRecorder()
		srv.Handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 on %s, got %d", p, rec.Code)
		}
	}

	// Compat chi router delegation & fallback 404
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Auth/Providers", nil))
	if rec.Code != http.StatusOK && rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected compat router match on /Auth/Providers, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/definitely-nonexistent-endpoint-xyz", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on unknown endpoint, got %d", rec.Code)
	}

	// Admin v0.3 routes (health & migration) mounted behind RequireAdmin -> 401 without token
	for _, p := range []string{"/api/admin/health", "/api/admin/migration/status"} {
		rec = httptest.NewRecorder()
		srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 on unauthenticated %s, got %d", p, rec.Code)
		}
	}

	// Watcher initialized and can watch a library directory
	if srv.Watcher == nil {
		t.Fatalf("expected srv.Watcher to be initialized")
	}
	watchDir := t.TempDir()
	if err := srv.Watcher.WatchLibrary("lib-test", []string{watchDir}); err != nil {
		t.Fatalf("WatchLibrary failed: %v", err)
	}

	// Trigger builtin RefreshLibrary task callback via scheduler
	tasks := srv.Scheduler.List(true)
	if len(tasks) == 0 {
		t.Fatalf("expected builtin scheduled tasks")
	}
	if err := srv.Scheduler.Run(tasks[0].Id); err != nil {
		t.Fatalf("expected Run builtin task to succeed, got %v", err)
	}
}

func TestBuildWebHandlerExternalDir(t *testing.T) {
	extWeb := t.TempDir()
	if err := os.WriteFile(filepath.Join(extWeb, "index.html"), []byte("<html>custom-web</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extWeb, "app.js"), []byte("console.log(1)"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := buildWebHandler(extWeb)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/app.js", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "console.log") {
		t.Fatalf("expected external app.js, got %d %q", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/unknown", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "custom-web") {
		t.Fatalf("expected external SPA fallback, got %d %q", rec.Code, rec.Body.String())
	}
}

func TestServerNewInvalidPath(t *testing.T) {
	cfg := config.Default("/nonexistent-readonly-path-ambra/should-fail")
	if _, err := New(context.Background(), cfg); err == nil {
		t.Fatalf("expected error opening invalid db path")
	}
}

func TestServerBuiltinTMDBKeyWiring(t *testing.T) {
	t.Setenv("AMBRA_TMDB_API_KEY", "")
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	defer func() { _ = srv.Close() }()

	p, ok := srv.Registry.Get("ambra.builtin.tmdb")
	if !ok {
		t.Fatal("expected ambra.builtin.tmdb registered")
	}
	tmdbProvider, ok := p.(*tmdb.Provider)
	if !ok {
		t.Fatalf("expected *tmdb.Provider, got %T", p)
	}

	var seenKey string
	mockTMDB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenKey = r.URL.Query().Get("api_key")
		_, _ = w.Write([]byte(`{"id":550,"title":"Fight Club","release_date":"1999-10-15"}`))
	}))
	defer mockTMDB.Close()

	tmdbProvider.SetHTTPClient(mockTMDB.Client())
	if err := tmdbProvider.Configure(map[string]any{"base_url": mockTMDB.URL}); err != nil {
		t.Fatalf("Configure base_url: %v", err)
	}
	res, err := tmdbProvider.FetchMetadata(context.Background(), plugins.MediaQuery{
		ItemType:    "Movie",
		ProviderIDs: map[string]string{"tmdb": "550"},
	})
	if err != nil || res == nil {
		t.Fatalf("FetchMetadata with built-in key failed: res=%+v err=%v", res, err)
	}
	if seenKey != config.DefaultTMDBApiKey {
		t.Fatalf("expected built-in key %q, got %q", config.DefaultTMDBApiKey, seenKey)
	}

	// Custom user key overrides built-in default
	if err := tmdbProvider.Configure(map[string]any{"api_key": "custom-server-key"}); err != nil {
		t.Fatalf("Configure custom api_key: %v", err)
	}
	if _, err := tmdbProvider.FetchMetadata(context.Background(), plugins.MediaQuery{
		ItemType:    "Movie",
		ProviderIDs: map[string]string{"tmdb": "550"},
	}); err != nil {
		t.Fatalf("FetchMetadata with custom key failed: %v", err)
	}
	if seenKey != "custom-server-key" {
		t.Fatalf("expected custom-server-key, got %q", seenKey)
	}
}

func TestApplyPendingDatabaseRestore(t *testing.T) {
	dataDir := t.TempDir()
	cfg, err := config.Load(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	dbPath := cfg.DBPath()

	// 1. No pending restore -> no-op
	if err := applyPendingDatabaseRestore(dataDir, dbPath); err != nil {
		t.Fatalf("expected no-op without restore file, got %v", err)
	}

	// 2. Empty restore file -> ignored, existing DB untouched
	if err := os.WriteFile(dbPath, []byte("original-db"), 0o644); err != nil {
		t.Fatal(err)
	}
	restorePath := filepath.Join(dataDir, "ambra.db.restore")
	if err := os.WriteFile(restorePath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := applyPendingDatabaseRestore(dataDir, dbPath); err != nil {
		t.Fatalf("expected empty restore to be ignored, got %v", err)
	}
	got, _ := os.ReadFile(dbPath)
	if string(got) != "original-db" {
		t.Fatalf("expected original db untouched, got %q", string(got))
	}

	// 2b. Non-SQLite restore file (invalid magic header) -> ignored, existing DB untouched
	if err := os.WriteFile(restorePath, []byte("not-a-valid-sqlite-database-file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := applyPendingDatabaseRestore(dataDir, dbPath); err != nil {
		t.Fatalf("expected invalid magic restore to be ignored, got %v", err)
	}
	got, _ = os.ReadFile(dbPath)
	if string(got) != "original-db" {
		t.Fatalf("expected original db untouched after invalid magic restore, got %q", string(got))
	}
	_ = os.Remove(restorePath)

	// 3. Valid SQLite DB staged at ambra.db.restore -> removes -wal/-shm, backs up old DB, promotes restore, boots cleanly
	seedStore, err := sqlite.Open(restorePath)
	if err != nil {
		t.Fatalf("create seed restore db: %v", err)
	}
	seedRepo := sqlite.NewUserRepo(seedStore)
	if err := seedRepo.CreateUser(context.Background(), &sqlite.UserRecord{
		ID:      "restoreduser1",
		Name:    "restored-admin",
		IsAdmin: true,
		Policy:  sqlite.DefaultUserPolicy(true),
		Config:  sqlite.DefaultUserConfig(),
	}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := seedStore.Close(); err != nil {
		t.Fatal(err)
	}

	// Create stale -wal and -shm sidecars for dbPath
	_ = os.WriteFile(dbPath+"-wal", []byte("stale-wal"), 0o644)
	_ = os.WriteFile(dbPath+"-shm", []byte("stale-shm"), 0o644)

	srv, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("server.New with pending restore failed: %v", err)
	}
	defer srv.Close()

	if _, err := os.Stat(restorePath); !os.IsNotExist(err) {
		t.Fatalf("expected ambra.db.restore to be consumed, stat err=%v", err)
	}
	bakData, err := os.ReadFile(dbPath + ".pre-restore.bak")
	if err != nil || string(bakData) != "original-db" {
		t.Fatalf("expected backup of original db, err=%v data=%q", err, string(bakData))
	}
	userRepo := sqlite.NewUserRepo(srv.Store)
	u, err := userRepo.GetUserByID(context.Background(), "restoreduser1")
	if err != nil || u == nil || u.Name != "restored-admin" {
		t.Fatalf("expected restored user in booted server DB, got u=%+v err=%v", u, err)
	}
}

func TestServerLiveTVAndDVRWiring(t *testing.T) {
	dataDir := t.TempDir()
	cfg, err := config.Load(dataDir)
	if err != nil {
		t.Fatal(err)
	}

	srv, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("server.New failed: %v", err)
	}
	defer func() {
		if err := srv.Close(); err != nil {
			t.Fatalf("server.Close failed: %v", err)
		}
	}()

	if srv.LiveTV == nil || srv.DVR == nil {
		t.Fatalf("expected srv.LiveTV and srv.DVR to be wired")
	}

	// Unauthenticated channel stream request must return 401
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/LiveTv/Channels/any/Stream", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on unauthenticated /LiveTv/Channels/{Id}/Stream, got %d", rec.Code)
	}

	// Create an API key (admin-capable token) to test authenticated Live TV proxy end-to-end
	apiKey, err := srv.Auth.CreateAPIKey(context.Background(), "livetv-test")
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	token := apiKey.Token

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("E2E_LIVE_STREAM"))
	}))
	defer upstream.Close()

	// Add channel via /LiveTv/TunerHosts
	m3uBody := `{"Type":"m3u","M3UContent":"#EXTM3U\n#EXTINF:-1,E2E Channel\n` + upstream.URL + `/live.ts\n"}`
	req := httptest.NewRequest(http.MethodPost, "/LiveTv/TunerHosts", strings.NewReader(m3uBody))
	req.Header.Set("X-Emby-Token", token)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /LiveTv/TunerHosts: %d %s", rec.Code, rec.Body.String())
	}

	// Query channel ID
	req = httptest.NewRequest(http.MethodGet, "/LiveTv/Channels", nil)
	req.Header.Set("X-Emby-Token", token)
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "E2E Channel") {
		t.Fatalf("GET /LiveTv/Channels: %d %s", rec.Code, rec.Body.String())
	}
	// Extract channel ID from response
	idx := strings.Index(rec.Body.String(), `"Id":"`)
	if idx < 0 {
		t.Fatalf("missing Id in channels response: %s", rec.Body.String())
	}
	rest := rec.Body.String()[idx+len(`"Id":"`):]
	chID := rest[:strings.Index(rest, `"`)]

	// Loopback upstream must be rejected by default SSRF protection (H-04)
	reqBlocked := httptest.NewRequest(http.MethodGet, "/LiveTv/Channels/"+chID+"/Stream", nil)
	reqBlocked.Header.Set("X-Emby-Token", token)
	recBlocked := httptest.NewRecorder()
	srv.Handler.ServeHTTP(recBlocked, reqBlocked)
	if recBlocked.Code != http.StatusBadRequest {
		t.Fatalf("expected loopback stream blocked with 400 by default, got code=%d body=%q", recBlocked.Code, recBlocked.Body.String())
	}

	// Override stream client for loopback httptest upstream to verify E2E proxy wiring
	srv.LiveTV.SetStreamClient(upstream.Client())

	// Proxy via primary route /LiveTv/Channels/{Id}/Stream
	req = httptest.NewRequest(http.MethodGet, "/LiveTv/Channels/"+chID+"/Stream", nil)
	req.Header.Set("X-Emby-Token", token)
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "E2E_LIVE_STREAM" {
		t.Fatalf("primary stream proxy: code=%d body=%q", rec.Code, rec.Body.String())
	}

	// Proxy via compat route /LiveTv/LiveStreamFiles/{Id}/stream.ts
	req = httptest.NewRequest(http.MethodGet, "/LiveTv/LiveStreamFiles/"+chID+"/stream.ts", nil)
	req.Header.Set("X-Emby-Token", token)
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "E2E_LIVE_STREAM" {
		t.Fatalf("compat stream proxy: code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestServerSecurityOptionsAndHardening(t *testing.T) {
	t.Cleanup(func() { netutil.SetTrustedProxies(nil) })

	// 1. EnableDLNA=false disables SSDP broadcaster; TrustedProxies wires into netutil
	dataDir := t.TempDir()
	cfg, err := config.Load(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.EnableDLNA = false
	cfg.TrustedProxies = []string{"198.51.100.7"}

	srv, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("server.New with EnableDLNA=false failed: %v", err)
	}
	if srv.SSDP != nil {
		_ = srv.Close()
		t.Fatal("expected srv.SSDP to be nil when EnableDLNA=false")
	}
	if !netutil.IsTrustedProxyIP(netip.MustParseAddr("198.51.100.7")) {
		_ = srv.Close()
		t.Fatal("expected TrustedProxies to be applied to netutil")
	}
	if netutil.IsTrustedProxyIP(netip.MustParseAddr("127.0.0.1")) {
		_ = srv.Close()
		t.Fatal("expected custom TrustedProxies allowlist to exclude 127.0.0.1")
	}

	// Exercise /web redirect, /favicon.svg, EventLibraryChanged callback, scheduler trigger persistence & library watcher preload
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/web", nil))
	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("expected 301 for /web, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/favicon.svg", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for /favicon.svg, got %d", rec.Code)
	}

	srv.Bus.Publish(context.Background(), eventbus.Event{Type: eventbus.EventLibraryChanged})

	libRepo := sqlite.NewLibraryRepo(srv.Store)
	if _, err := libRepo.CreateLibrary(context.Background(), "Movies", "movies", []string{t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	interval := int64(3600 * 10_000_000)
	tasks := srv.Scheduler.List(true)
	if len(tasks) > 0 {
		if err := srv.Scheduler.SetTriggers(tasks[0].Id, []scheduler.TaskTriggerInfo{
			{Type: "IntervalTrigger", IntervalTicks: &interval},
		}); err != nil {
			t.Fatalf("SetTriggers failed: %v", err)
		}
	}
	if err := srv.Close(); err != nil {
		t.Fatalf("srv.Close: %v", err)
	}

	// Boot a second Server against the same dataDir to exercise persisted trigger loading & pre-existing library watching
	cfg.EnableDLNA = true
	srv2, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("second server.New failed: %v", err)
	}
	if srv2.SSDP == nil {
		_ = srv2.Close()
		t.Fatal("expected srv2.SSDP initialized when EnableDLNA=true")
	}

	// Trigger RefreshLibrary task (exercises ScanAll + OnScanComplete + ApplyTagsToLibrary)
	for _, tk := range srv2.Scheduler.List(true) {
		if tk.Key == "RefreshLibrary" {
			if err := srv2.Scheduler.Run(tk.Id); err != nil {
				t.Fatalf("Run RefreshLibrary: %v", err)
			}
			srv2.Scheduler.Wait(tk.Id)
		}
	}

	// Exercise TaskStopper callback via compatCore DELETE /ScheduledTasks/Running/{id}
	apiKey, err := srv2.Auth.CreateAPIKey(context.Background(), "admin-ops")
	if err != nil {
		t.Fatal(err)
	}
	stopReq := httptest.NewRequest(http.MethodDelete, "/ScheduledTasks/Running/nonexistent-task", nil)
	stopReq.Header.Set("X-Emby-Token", apiKey.Token)
	rec = httptest.NewRecorder()
	srv2.Handler.ServeHTTP(rec, stopReq)

	// Exercise resolveStreamURL closure via Audio STRM item request
	strmFile := filepath.Join(t.TempDir(), "track.strm")
	_ = os.WriteFile(strmFile, []byte("http://127.0.0.1:1/test.mp3\n"), 0o644)
	libRepo2 := sqlite.NewLibraryRepo(srv2.Store)
	audioItem := &sqlite.ItemRecord{
		ID:        "strmaudio00000000000000000000001",
		LibraryID: "music-lib",
		Type:      "Audio",
		MediaType: "Audio",
		IsStrm:    true,
		Name:      "STRM Song",
		Container: "strm",
		Path:      strmFile,
	}
	_ = libRepo2.UpsertItem(context.Background(), audioItem)
	audioReq := httptest.NewRequest(http.MethodGet, "/Audio/"+audioItem.ID+"/stream?Static=true", nil)
	audioReq.Header.Set("X-Emby-Token", apiKey.Token)
	rec = httptest.NewRecorder()
	srv2.Handler.ServeHTTP(rec, audioReq)

	// Corrupt scheduler:triggers JSON and boot a third Server to cover LoadPersistedTriggers warn path
	_, _ = srv2.Store.Writer.ExecContext(context.Background(),
		`UPDATE server_config SET value_json = '{corrupt-json' WHERE key = 'scheduler:triggers'`)
	_ = srv2.Close()

	srv3, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("third server.New with corrupt triggers should warn and boot, got %v", err)
	}
	_ = srv3.Close()

	// Closing a zero-value Server with nil Store returns nil cleanly
	var emptySrv Server
	if err := emptySrv.Close(); err != nil {
		t.Fatalf("emptySrv.Close: %v", err)
	}
}

func TestApplyPendingDatabaseRestoreSymlinkAndSidecars(t *testing.T) {
	// 1. Empty args are no-op
	if err := applyPendingDatabaseRestore("", "/tmp/x.db"); err != nil {
		t.Fatalf("expected nil for empty dataDir, got %v", err)
	}
	if err := applyPendingDatabaseRestore(t.TempDir(), ""); err != nil {
		t.Fatalf("expected nil for empty dbPath, got %v", err)
	}

	// 2. Symlink ambra.db.restore pointing to a valid SQLite file MUST be rejected
	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "ambra.db")
	if err := os.WriteFile(dbPath, []byte("original-db-content"), 0o644); err != nil {
		t.Fatal(err)
	}
	externalSQLite := filepath.Join(t.TempDir(), "external.db")
	extStore, err := sqlite.Open(externalSQLite)
	if err != nil {
		t.Fatal(err)
	}
	_ = extStore.Close()

	restoreLink := filepath.Join(dataDir, "ambra.db.restore")
	if err := os.Symlink(externalSQLite, restoreLink); err != nil {
		t.Fatal(err)
	}
	if err := applyPendingDatabaseRestore(dataDir, dbPath); err != nil {
		t.Fatalf("expected symlink restore ignored without error, got %v", err)
	}
	got, _ := os.ReadFile(dbPath)
	if string(got) != "original-db-content" {
		t.Fatalf("expected original DB untouched when restore is a symlink, got %q", string(got))
	}
	_ = os.Remove(restoreLink)

	// 3. Truncated (<16 bytes) restore file ignored
	if err := os.WriteFile(restoreLink, []byte("SQLite"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := applyPendingDatabaseRestore(dataDir, dbPath); err != nil {
		t.Fatalf("expected short restore ignored, got %v", err)
	}
	_ = os.Remove(restoreLink)

	// 4. Valid restore preserves WAL and SHM sidecars alongside .pre-restore.bak
	seedBytes, err := os.ReadFile(externalSQLite)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(restoreLink, seedBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dbPath+"-wal", []byte("orig-wal-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dbPath+"-shm", []byte("orig-shm-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := applyPendingDatabaseRestore(dataDir, dbPath); err != nil {
		t.Fatalf("applyPendingDatabaseRestore failed: %v", err)
	}
	walBak, err := os.ReadFile(dbPath + ".pre-restore.bak-wal")
	if err != nil || string(walBak) != "orig-wal-bytes" {
		t.Fatalf("expected WAL backup preserved, data=%q err=%v", string(walBak), err)
	}
	shmBak, err := os.ReadFile(dbPath + ".pre-restore.bak-shm")
	if err != nil || string(shmBak) != "orig-shm-bytes" {
		t.Fatalf("expected SHM backup preserved, data=%q err=%v", string(shmBak), err)
	}
	if _, err := os.Stat(dbPath + "-wal"); !os.IsNotExist(err) {
		t.Fatalf("expected original -wal moved to backup, stat err=%v", err)
	}

	// 5. Promote restore when no existing DB exists yet
	freshDir := t.TempDir()
	freshDB := filepath.Join(freshDir, "ambra.db")
	if err := os.WriteFile(filepath.Join(freshDir, "ambra.db.restore"), seedBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := applyPendingDatabaseRestore(freshDir, freshDB); err != nil {
		t.Fatalf("applyPendingDatabaseRestore fresh failed: %v", err)
	}
	if _, err := os.Stat(freshDB); err != nil {
		t.Fatalf("expected fresh DB promoted, err=%v", err)
	}

	// 6. Backup rename failure returns error (backupPath is a non-empty directory)
	failDir := t.TempDir()
	failDB := filepath.Join(failDir, "ambra.db")
	_ = os.WriteFile(failDB, []byte("old"), 0o644)
	_ = os.WriteFile(filepath.Join(failDir, "ambra.db.restore"), seedBytes, 0o644)
	_ = os.MkdirAll(filepath.Join(failDB+".pre-restore.bak", "blocker"), 0o755)
	if err := applyPendingDatabaseRestore(failDir, failDB); err == nil {
		t.Fatal("expected error when backup rename is blocked by non-empty directory")
	}
	// And server.New propagates applyPendingDatabaseRestore failure
	cfgFail := config.Default(failDir)
	if _, err := New(context.Background(), cfgFail); err == nil {
		t.Fatal("expected server.New to fail when pending restore fails")
	}

	// 7. Error paths in applyPendingDatabaseRestore:
	// 7a. dataDir is a regular file -> Lstat ENOTDIR error
	notDirFile := filepath.Join(t.TempDir(), "not-a-dir")
	_ = os.WriteFile(notDirFile, []byte("x"), 0o644)
	if err := applyPendingDatabaseRestore(notDirFile, filepath.Join(t.TempDir(), "ambra.db")); err == nil {
		t.Fatal("expected Lstat error when dataDir is a regular file")
	}

	// 7b. Unreadable restore file -> os.Open fails and is ignored
	unreadableDir := t.TempDir()
	unreadableRestore := filepath.Join(unreadableDir, "ambra.db.restore")
	_ = os.WriteFile(unreadableRestore, seedBytes, 0o000)
	t.Cleanup(func() { _ = os.Chmod(unreadableRestore, 0o644) })
	if err := applyPendingDatabaseRestore(unreadableDir, filepath.Join(unreadableDir, "ambra.db")); err != nil {
		t.Fatalf("expected unreadable restore ignored without error, got %v", err)
	}

	// 7c. dbPath parent is a regular file -> os.Stat(dbPath) returns ENOTDIR error
	statErrDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(statErrDir, "ambra.db.restore"), seedBytes, 0o644)
	if err := applyPendingDatabaseRestore(statErrDir, filepath.Join(notDirFile, "ambra.db")); err == nil {
		t.Fatal("expected stat existing db error when dbPath parent is a regular file")
	}

	// 7d. Promote rename failure when dbPath parent directory does not exist
	if err := applyPendingDatabaseRestore(statErrDir, filepath.Join(t.TempDir(), "missing-parent", "ambra.db")); err == nil {
		t.Fatal("expected promote rename error when dbPath parent does not exist")
	}

	// 8. buildWebHandler with non-existent dir and explicit /index.html request
	h := buildWebHandler(filepath.Join(t.TempDir(), "does-not-exist"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/index.html", nil))
	if rec.Code != http.StatusOK && rec.Code != http.StatusMovedPermanently {
		t.Fatalf("expected 200 or 301 for /index.html, got %d", rec.Code)
	}
}
