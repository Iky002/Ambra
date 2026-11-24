package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
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

	// Public Phase 4-6 routes via delegated mediaMux
	for _, p := range []string{"/openapi.json", "/robots.txt", "/Dlna/test-uuid/description.xml"} {
		rec = httptest.NewRecorder()
		srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
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
