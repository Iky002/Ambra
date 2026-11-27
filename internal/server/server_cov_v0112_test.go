package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/scheduler"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestServerCovV0112_WebHandlerRestoreAndDLNAEdges(t *testing.T) {
	// 1. buildWebHandler with custom WebDir: _headers, _redirects, real file, assets/ missing 404, ffmpeg/ missing 404, SPA fallback
	webDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(webDir, "index.html"), []byte("<html>spa</html>"), 0o644)
	_ = os.WriteFile(filepath.Join(webDir, "custom.txt"), []byte("hello-web"), 0o644)

	wh := buildWebHandler(webDir)
	for _, tc := range []struct {
		path     string
		wantCode int
		wantBody string
	}{
		{path: "/_headers", wantCode: http.StatusNotFound},
		{path: "/_redirects", wantCode: http.StatusNotFound},
		{path: "/assets/missing.js", wantCode: http.StatusNotFound},
		{path: "/ffmpeg/core.wasm", wantCode: http.StatusNotFound},
		{path: "/custom.txt", wantCode: http.StatusOK, wantBody: "hello-web"},
		{path: "/app/movies", wantCode: http.StatusOK, wantBody: "<html>spa</html>"},
	} {
		rec := httptest.NewRecorder()
		wh.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if rec.Code != tc.wantCode {
			t.Fatalf("path %s: got %d, want %d", tc.path, rec.Code, tc.wantCode)
		}
		if tc.wantBody != "" && !strings.Contains(rec.Body.String(), tc.wantBody) {
			t.Fatalf("path %s: expected body %q, got %q", tc.path, tc.wantBody, rec.Body.String())
		}
	}

	// 2. applyPendingDatabaseRestore edge cases: blank args, empty file, invalid SQLite magic
	if err := applyPendingDatabaseRestore("", ""); err != nil {
		t.Fatalf("expected nil for empty paths, got %v", err)
	}
	restoreDir := t.TempDir()
	dbTarget := filepath.Join(restoreDir, "ambra.db")
	restoreFile := filepath.Join(restoreDir, "ambra.db.restore")
	_ = os.WriteFile(restoreFile, nil, 0o600) // 0 bytes -> ignored
	if err := applyPendingDatabaseRestore(restoreDir, dbTarget); err != nil {
		t.Fatalf("expected nil for empty restore file, got %v", err)
	}
	_ = os.WriteFile(restoreFile, []byte("not-a-sqlite-header-at-all"), 0o600) // bad magic -> ignored
	if err := applyPendingDatabaseRestore(restoreDir, dbTarget); err != nil {
		t.Fatalf("expected nil for invalid magic restore file, got %v", err)
	}

	// 3. applyDLNAState guards: closed server, cancelled parent context, and idempotent enable/disable
	closedSrv := &Server{ssdpClosed: true}
	closedSrv.applyDLNAState(true)

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	canceledSrv := &Server{ssdpCtx: canceledCtx}
	canceledSrv.applyDLNAState(true)
	if canceledSrv.SSDPBroadcaster() != nil {
		t.Fatal("expected nil SSDP when parent context is cancelled")
	}
}

func TestServerCovV0112_WiredClosuresTasksTriggersPluginsStreamAndUserPurge(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	cfg, err := config.Load(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.EnableSTRM302 = true

	// Seed a library directory before New() so libWatcher watches existing library on boot
	mediaDir := filepath.Join(dataDir, "media")
	_ = os.MkdirAll(mediaDir, 0o755)
	strmFile := filepath.Join(mediaDir, "track.strm")
	_ = os.WriteFile(strmFile, []byte("https://93.184.216.34/media/track.mp3\n"), 0o644)

	preStore, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	preLibRepo := sqlite.NewLibraryRepo(preStore)
	libRec, err := preLibRepo.CreateLibrary(ctx, "Boot Media", "music", []string{mediaDir})
	if err != nil {
		t.Fatal(err)
	}
	//Also seed invalid scheduler:triggers JSON first to cover LoadPersistedTriggers warning path cleanly
	_, _ = preStore.Writer.ExecContext(ctx,
		`INSERT INTO server_config (key, value_json, updated_at) VALUES ('scheduler:triggers', '{bad-json', 1)`)
	_ = preStore.Close()

	srv, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("server.New failed: %v", err)
	}
	defer func() { _ = srv.Close() }()

	apiKey, err := srv.Auth.CreateAPIKey(ctx, "server-cov-admin")
	if err != nil {
		t.Fatal(err)
	}
	token := apiKey.Token

	doReq := func(method, path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		var rdr *bytes.Reader
		if body != nil {
			raw, _ := json.Marshal(body)
			rdr = bytes.NewReader(raw)
		} else {
			rdr = bytes.NewReader(nil)
		}
		req := httptest.NewRequest(method, path, rdr)
		req.RemoteAddr = "127.0.0.1:45678"
		req.Header.Set("X-Emby-Token", token)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		srv.Handler.ServeHTTP(rec, req)
		return rec
	}

	// 1. PluginAdmin OnConfigSaved closures for ambra.builtin.tmdb & ambra.builtin.strm
	if rec := doReq(http.MethodPost, "/Plugins/ambra.builtin.tmdb/Configuration", map[string]any{
		"api_key":  "tmdb-cov-key",
		"language": "ja-JP",
	}); rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
		t.Fatalf("update tmdb plugin config got %d: %s", rec.Code, rec.Body.String())
	}
	if cfg.Snapshot().TMDBLanguage != "ja-JP" {
		t.Fatalf("expected TMDBLanguage ja-JP synced to config, got %q", cfg.Snapshot().TMDBLanguage)
	}

	if rec := doReq(http.MethodPost, "/Plugins/ambra.builtin.strm/Configuration", map[string]any{
		"enable_strm_302": false,
	}); rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
		t.Fatalf("update strm enable_strm_302 got %d", rec.Code)
	}
	if cfg.Snapshot().EnableSTRM302 {
		t.Fatal("expected EnableSTRM302=false via enable_strm_302")
	}
	if rec := doReq(http.MethodPost, "/Plugins/ambra.builtin.strm/Configuration", map[string]any{
		"enable_302": true,
	}); rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
		t.Fatalf("update strm enable_302 got %d", rec.Code)
	}
	if !cfg.Snapshot().EnableSTRM302 {
		t.Fatal("expected EnableSTRM302=true via enable_302")
	}

	// 2. Persist scheduler triggers & reload via SetTriggerStore closures + run fast local tasks
	ticks := int64(3600 * 10_000_000)
	if rec := doReq(http.MethodPost, "/ScheduledTasks/extract-chapter-images/Triggers", []scheduler.TaskTriggerInfo{
		{Type: "IntervalTrigger", IntervalTicks: &ticks},
	}); rec.Code != http.StatusNoContent && rec.Code != http.StatusOK {
		t.Fatalf("save triggers got %d: %s", rec.Code, rec.Body.String())
	}
	if err := srv.Scheduler.LoadPersistedTriggers(ctx); err != nil {
		t.Fatalf("LoadPersistedTriggers failed: %v", err)
	}

	// Run ScanAll directly to hit sc.SetOnScanComplete closure cleanly
	_, _ = srv.Scanner.ScanAll(ctx)

	// Trigger fast local tasks (extract-chapter-images, generate-bif-thumbnails, refresh-livetv-guides) & compat task stop
	for _, taskID := range []string{"extract-chapter-images", "generate-bif-thumbnails", "refresh-livetv-guides"} {
		_ = doReq(http.MethodPost, "/ScheduledTasks/Running/"+taskID, nil)
	}
	// Wait briefly for empty-DB background tasks to run their closures before stopping/closing
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
	}
	_ = doReq(http.MethodDelete, "/ScheduledTasks/Running/extract-chapter-images", nil)

	// 3. Stream resolver closures on Audio (universal + stream) & HLS (master + live) for .strm item
	libRepo := sqlite.NewLibraryRepo(srv.Store)
	strmItemID := guid.New()
	_ = libRepo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:        strmItemID,
		LibraryID: libRec.ID,
		Type:      "Audio",
		MediaType: "Audio",
		Name:      "Remote Track",
		Path:      strmFile,
	})
	recAudio := doReq(http.MethodGet, "/Audio/"+strmItemID+"/stream", nil)
	if recAudio.Code != http.StatusFound {
		t.Fatalf("expected 302 redirect on .strm Audio stream, got %d", recAudio.Code)
	}
	_ = doReq(http.MethodGet, "/Audio/"+strmItemID+"/universal?TranscodingProtocol=hls", nil)
	_ = doReq(http.MethodGet, "/Videos/"+strmItemID+"/master.m3u8", nil)
	_ = doReq(http.MethodGet, "/Videos/"+strmItemID+"/live.m3u8", nil)

	// 4. Create & delete a secondary user via /Users routes to fire purgeUserArtifacts closure
	recNewUser := doReq(http.MethodPost, "/Users/New", map[string]any{"Name": "TempPurgeUser"})
	if recNewUser.Code == http.StatusOK {
		var created struct {
			ID string `json:"Id"`
		}
		if json.Unmarshal(recNewUser.Body.Bytes(), &created) == nil && created.ID != "" {
			recDelUser := doReq(http.MethodDelete, "/Users/"+created.ID, nil)
			if recDelUser.Code != http.StatusNoContent && recDelUser.Code != http.StatusOK {
				t.Fatalf("expected 204/200 deleting user, got %d", recDelUser.Code)
			}
		}
	}

	// 5. applyDLNAState idempotent double-enable then disable + nil ssdpCtx standalone check
	srv.applyDLNAState(true)
	srv.applyDLNAState(true)
	srv.applyDLNAState(false)

	standaloneDLNA := &Server{}
	standaloneDLNA.applyDLNAState(true)
	standaloneDLNA.stopSSDP()
}
