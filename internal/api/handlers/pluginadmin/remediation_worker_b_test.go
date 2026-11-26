package pluginadmin

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/plugins/ext"
	"github.com/ambra-media/ambra/internal/plugins/nfo"
	"github.com/ambra-media/ambra/internal/plugins/strm"
	"github.com/ambra-media/ambra/internal/plugins/tmdb"
	"github.com/ambra-media/ambra/internal/plugins/tvdb"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestDefaultConfigExportExternalUninstallAndOnConfigSavedHook(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "pa_remediation.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	reg := plugins.NewRegistry()
	tp := tmdb.New("initial-user-secret", "zh-CN")
	sp := strm.New(true)
	np := nfo.New()
	vp := tvdb.New()
	ep, err := ext.NewHTTPPlugin(ext.ExternalManifest{
		ID:       "ext.removable",
		Name:     "Removable External Plugin",
		Endpoint: "https://plugin.example.com/api",
		APIKey:   "ext-secret",
		Priority: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	reg.Register(tp)
	reg.Register(sp)
	reg.Register(np)
	reg.Register(vp)
	reg.Register(ep)

	h := New(reg, store)
	var savedEvents []string
	var lastSavedCfg map[string]any
	h.SetOnConfigSaved(func(pluginID string, saved map[string]any) {
		savedEvents = append(savedEvents, pluginID)
		lastSavedCfg = saved
	})

	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	// 1. GET /Plugins/{Id}/Configuration BEFORE any DB write returns masked DefaultConfig()!
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Plugins/ambra.builtin.tmdb/Configuration", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET tmdb config status=%d", rec.Code)
	}
	var gotTMDB map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &gotTMDB)
	if gotTMDB["language"] != "zh-CN" || gotTMDB["api_key"] != maskedSecret || gotTMDB["base_url"] == "" {
		t.Fatalf("expected DefaultConfig merged and masked before DB save, got %+v", gotTMDB)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Plugins/ambra.builtin.strm/Configuration", nil))
	var gotSTRM map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &gotSTRM)
	if gotSTRM["enable_302"] != true {
		t.Fatalf("expected strm DefaultConfig enable_302=true, got %+v", gotSTRM)
	}

	// 2. TMDB & TVDB SSRF protection in Configure rejects loopback / IMDS when no test HTTPClient is set
	for _, id := range []string{"ambra.builtin.tmdb", "ambra.builtin.tvdb"} {
		body, _ := json.Marshal(map[string]any{"base_url": "http://127.0.0.1:8096/evil"})
		r := httptest.NewRecorder()
		mux.ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/Plugins/"+id+"/Configuration", bytes.NewReader(body)))
		if r.Code != http.StatusBadRequest {
			t.Fatalf("expected %s SSRF base_url rejected with 400, got %d", id, r.Code)
		}
	}

	// 3. Dynamic priority update across built-in plugins + OnConfigSaved callback trigger
	updBody, _ := json.Marshal(map[string]any{"priority": 5, "language": "en-US"})
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Plugins/ambra.builtin.tmdb/Configuration", bytes.NewReader(updBody)))
	if rec.Code != http.StatusOK || tp.Manifest().Priority != 5 {
		t.Fatalf("expected tmdb priority updated to 5, code=%d prio=%d", rec.Code, tp.Manifest().Priority)
	}
	if len(savedEvents) != 1 || savedEvents[0] != "ambra.builtin.tmdb" || lastSavedCfg["language"] != "en-US" {
		t.Fatalf("expected SetOnConfigSaved fired for ambra.builtin.tmdb, events=%v cfg=%+v", savedEvents, lastSavedCfg)
	}

	_ = np.Configure(map[string]any{"priority": float64(3)})
	_ = sp.Configure(map[string]any{"priority": 8})
	if np.Manifest().Priority != 3 || sp.Manifest().Priority != 8 {
		t.Fatalf("expected nfo/strm dynamic priority updated, got %d / %d", np.Manifest().Priority, sp.Manifest().Priority)
	}

	// 4. Persist config for external plugin, then DELETE /Plugins/ext.removable -> real Unregister + purge server_config!
	extBody, _ := json.Marshal(map[string]any{"priority": 45})
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Plugins/ext.removable/Configuration", bytes.NewReader(extBody)))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST ext.removable config failed: %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/Plugins/ext.removable", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE ext.removable status=%d, want 204", rec.Code)
	}
	if _, stillInReg := reg.Get("ext.removable"); stillInReg {
		t.Fatal("expected external plugin removed from Registry via Unregister")
	}
	var leftover string
	err = store.Reader.QueryRowContext(context.Background(), `SELECT value_json FROM server_config WHERE key = 'plugin:ext.removable'`).Scan(&leftover)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected plugin:ext.removable purged from server_config, got leftover=%q err=%v", leftover, err)
	}
	// Subsequent GET returns 404
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Plugins/ext.removable/Configuration", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after external plugin uninstall, got %d", rec.Code)
	}
}
