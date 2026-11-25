package pluginadmin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/plugins/strm"
	"github.com/ambra-media/ambra/internal/plugins/tmdb"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestPluginAdminPersistenceAndRestore(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "plugins.db")
	store, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	reg := plugins.NewRegistry()
	tmdbPlugin := tmdb.New("", "zh-CN")
	strmPlugin := strm.New(true)
	reg.Register(tmdbPlugin)
	reg.Register(strmPlugin)

	mux := http.NewServeMux()
	h := New(reg, store)
	h.RegisterRoutes(mux, AuthFuncs{})

	// 1. Update TMDB config
	payload, _ := json.Marshal(map[string]any{
		"api_key":  "secret-tmdb-key",
		"language": "en-US",
		"enabled":  true,
	})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Plugins/ambra.builtin.tmdb/Configuration", bytes.NewReader(payload)))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST Configuration status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 2. Get TMDB config
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Plugins/ambra.builtin.tmdb/Configuration", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET Configuration status=%d", rec.Code)
	}
	var gotCfg map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &gotCfg)
	if gotCfg["api_key"] != "secret-tmdb-key" || gotCfg["language"] != "en-US" {
		t.Fatalf("unexpected persisted config: %+v", gotCfg)
	}

	// 3. Disable STRM via DELETE /Plugins/{Id}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/Plugins/ambra.builtin.strm", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE Plugin status=%d", rec.Code)
	}
	if strmPlugin.Manifest().Enabled {
		t.Fatal("expected strm plugin to be disabled after DELETE")
	}

	// 4. Re-enable STRM via POST /Plugins/{Id}/Enable
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Plugins/ambra.builtin.strm/Enable?enabled=true", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST Enable status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strmPlugin.Manifest().Enabled {
		t.Fatal("expected strm plugin to be re-enabled")
	}

	// Disable TMDB before simulating restart
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Plugins/ambra.builtin.tmdb/Enable?enabled=false", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST disable tmdb status=%d", rec.Code)
	}

	// 5. Simulate server restart: fresh registry + fresh plugin instances + ApplySavedConfigs
	reg2 := plugins.NewRegistry()
	tmdb2 := tmdb.New("", "zh-CN")
	strm2 := strm.New(true)
	reg2.Register(tmdb2)
	reg2.Register(strm2)

	h2 := New(reg2, store)
	if err := h2.ApplySavedConfigs(context.Background()); err != nil {
		t.Fatalf("ApplySavedConfigs failed: %v", err)
	}
	if tmdb2.Manifest().Enabled {
		t.Fatal("expected restarted tmdb plugin to restore enabled=false")
	}
	if !strm2.Manifest().Enabled {
		t.Fatal("expected restarted strm plugin to restore enabled=true")
	}

	// Verify restored config via GET
	mux2 := http.NewServeMux()
	h2.RegisterRoutes(mux2, AuthFuncs{})
	rec = httptest.NewRecorder()
	mux2.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Plugins/ambra.builtin.tmdb/Configuration", nil))
	var restored map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &restored)
	if restored["api_key"] != "secret-tmdb-key" || restored["enabled"] != false {
		t.Fatalf("unexpected restored config: %+v", restored)
	}
	// Verify PascalCase metadata keys are present alongside lowercase keys
	if restored["Id"] != "ambra.builtin.tmdb" || restored["Enabled"] != false || restored["Name"] == "" || restored["Priority"] == nil {
		t.Fatalf("expected PascalCase metadata keys in GET Configuration, got: %+v", restored)
	}

	// 6. DELETE /Plugins/{Id} must preserve existing config keys (never wipe api_key/language)
	rec = httptest.NewRecorder()
	mux2.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Plugins/ambra.builtin.tmdb/Enable?enabled=true", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("re-enable tmdb failed: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	mux2.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/Plugins/ambra.builtin.tmdb", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE tmdb failed: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	mux2.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Plugins/ambra.builtin.tmdb/Configuration", nil))
	var afterDel map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &afterDel)
	if afterDel["enabled"] != false || afterDel["Enabled"] != false {
		t.Fatalf("expected enabled=false after DELETE, got %+v", afterDel)
	}
	if afterDel["api_key"] != "secret-tmdb-key" || afterDel["language"] != "en-US" {
		t.Fatalf("expected DELETE to preserve existing config keys, got %+v", afterDel)
	}
}
