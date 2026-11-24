package pluginadmin

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/plugins/nfo"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

type failingPlugin struct {
	id      string
	enabled bool
	failErr error
}

func (f *failingPlugin) Manifest() plugins.Manifest {
	return plugins.Manifest{ID: f.id, Name: "FailPlug", Enabled: f.enabled, Priority: 50}
}
func (f *failingPlugin) Configure(cfg map[string]any) error {
	if f.failErr != nil {
		return f.failErr
	}
	if v, ok := cfg["enabled"].(bool); ok {
		f.enabled = v
	}
	return nil
}

func TestPluginAdminExtraBranches(t *testing.T) {
	// 1. Nil registry and nil store (in-memory only) + Track()
	hMem := New(nil, nil)
	if err := hMem.ApplySavedConfigs(context.Background()); err != nil {
		t.Fatalf("ApplySavedConfigs nil store failed: %v", err)
	}
	fp := &failingPlugin{id: "custom.fail", enabled: true}
	hMem.Track(nil, fp)

	muxMem := http.NewServeMux()
	hMem.RegisterRoutes(muxMem, AuthFuncs{})

	// Update config with null JSON (incoming == nil) and verify in-memory save/load
	rec := httptest.NewRecorder()
	muxMem.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Plugins/custom.fail/Configuration", bytes.NewReader([]byte(`null`))))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST null config status=%d", rec.Code)
	}

	// Enable via JSON body {"enabled":false}
	rec = httptest.NewRecorder()
	muxMem.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Plugins/custom.fail/Enable", bytes.NewReader([]byte(`{"enabled":false}`))))
	if rec.Code != http.StatusOK || fp.enabled {
		t.Fatalf("expected enabled=false via body, code=%d enabled=%v", rec.Code, fp.enabled)
	}

	// Now make Configure fail -> 400 on Update/Enable, 500 on Delete
	fp.failErr = errors.New("bad config")
	rec = httptest.NewRecorder()
	muxMem.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Plugins/custom.fail/Configuration", bytes.NewReader([]byte(`{"a":1}`))))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when Configure fails on update, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	muxMem.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Plugins/custom.fail/Enable?enabled=true", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when Configure fails on enable, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	muxMem.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/Plugins/custom.fail", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when Configure fails on delete, got %d", rec.Code)
	}

	// 404 not found on all 4 endpoints + invalid JSON on update
	for _, tc := range [][2]string{
		{http.MethodGet, "/Plugins/missing/Configuration"},
		{http.MethodPost, "/Plugins/missing/Configuration"},
		{http.MethodPost, "/Plugins/missing/Enable"},
		{http.MethodDelete, "/Plugins/missing"},
	} {
		rec = httptest.NewRecorder()
		muxMem.ServeHTTP(rec, httptest.NewRequest(tc[0], tc[1], bytes.NewReader([]byte(`{}`))))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s expected 404, got %d", tc[0], tc[1], rec.Code)
		}
	}
	rec = httptest.NewRecorder()
	muxMem.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Plugins/custom.fail/Configuration", bytes.NewReader([]byte(`{bad`))))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid JSON, got %d", rec.Code)
	}

	// 2. Store-backed tests: late-registered plugin discovery via findPlugin, corrupted JSON skipped in ApplySavedConfigs, Configure error in ApplySavedConfigs, closed DB errors
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "pa_extra.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	reg := plugins.NewRegistry()
	hDB := New(reg, store)
	// Register after New() so findPlugin exercises syncFromRegistry fallback
	reg.Register(nfo.New())
	muxDB := http.NewServeMux()
	hDB.RegisterRoutes(muxDB, AuthFuncs{})

	rec = httptest.NewRecorder()
	muxDB.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Plugins/ambra.builtin.nfo/Configuration", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected late-registered nfo plugin found, got %d", rec.Code)
	}

	// Insert corrupted JSON and a config for failing plugin into server_config
	_, _ = store.Writer.Exec(`INSERT INTO server_config (key, value_json, updated_at) VALUES ('plugin:corrupt', '{bad', 1)`)
	_, _ = store.Writer.Exec(`INSERT INTO server_config (key, value_json, updated_at) VALUES ('plugin:custom.fail', '{"enabled":true}', 1)`)
	hDB.Track(fp)
	if err := hDB.ApplySavedConfigs(context.Background()); err == nil {
		t.Fatalf("expected ApplySavedConfigs error when tracked plugin Configure fails")
	}

	// Close store and check 500 error paths
	fp.failErr = nil
	_ = store.Close()
	if err := hDB.ApplySavedConfigs(context.Background()); err == nil {
		t.Fatalf("expected ApplySavedConfigs error on closed DB")
	}
	for _, tc := range [][2]string{
		{http.MethodPost, "/Plugins/ambra.builtin.nfo/Configuration"},
		{http.MethodPost, "/Plugins/ambra.builtin.nfo/Enable?enabled=true"},
		{http.MethodDelete, "/Plugins/ambra.builtin.nfo"},
	} {
		rec = httptest.NewRecorder()
		muxDB.ServeHTTP(rec, httptest.NewRequest(tc[0], tc[1], bytes.NewReader([]byte(`{"enabled":true}`))))
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("closed DB %s %s expected 500, got %d", tc[0], tc[1], rec.Code)
		}
	}
}
