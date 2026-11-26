package pluginadmin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/plugins/tmdb"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

type rollbackMockPlugin struct {
	mu          sync.Mutex
	manifest    plugins.Manifest
	cfg         map[string]any
	failNext    bool
	failOnValue string
}

func (m *rollbackMockPlugin) Manifest() plugins.Manifest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.manifest
}

func (m *rollbackMockPlugin) Configure(cfg map[string]any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cfg == nil {
		m.cfg = make(map[string]any)
	}
	// Simulate partial mutation before returning an error (M-27).
	if v, ok := cfg["enabled"].(bool); ok {
		m.manifest.Enabled = v
	}
	if v, ok := cfg["tag"].(string); ok {
		m.cfg["tag"] = v
	}
	if v, ok := cfg["api_key"].(string); ok {
		m.cfg["api_key"] = v
	}
	if m.failNext {
		m.failNext = false
		return errors.New("configure rejected")
	}
	if m.failOnValue != "" {
		if v, ok := cfg["tag"].(string); ok && v == m.failOnValue {
			return errors.New("bad tag")
		}
	}
	return nil
}

func (m *rollbackMockPlugin) snapshot() (plugins.Manifest, map[string]any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.manifest, cloneMap(m.cfg)
}

func TestPluginAdminV0109AuditH03M18M27L19(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "pluginadmin_v109.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	reg := plugins.NewRegistry()
	tp := tmdb.New("", "zh-CN")
	reg.Register(tp)
	h := New(reg, store)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	// 1. H-03: Changing base_url host without new secret clears old api_key (even if masked "********" is submitted)
	postCfg := func(pluginID string, payload map[string]any) *httptest.ResponseRecorder {
		t.Helper()
		b, _ := json.Marshal(payload)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Plugins/"+pluginID+"/Configuration", bytes.NewReader(b)))
		return rec
	}

	if rec := postCfg(tp.Manifest().ID, map[string]any{
		"api_key":  "super-secret-tmdb-key",
		"base_url": "https://api.themoviedb.org/3",
	}); rec.Code != http.StatusOK {
		t.Fatalf("initial config failed: %d %s", rec.Code, rec.Body.String())
	}

	// Change host and omit api_key -> old api_key must be cleared from saved config and plugin memory.
	if rec := postCfg(tp.Manifest().ID, map[string]any{
		"base_url": "https://93.184.216.34/3",
	}); rec.Code != http.StatusOK {
		t.Fatalf("host change config failed: %d %s", rec.Code, rec.Body.String())
	}
	saved, err := h.loadSavedConfig(context.Background(), tp.Manifest().ID)
	if err != nil {
		t.Fatalf("loadSavedConfig: %v", err)
	}
	if v, ok := saved["api_key"].(string); ok && v != "" {
		t.Fatalf("H-03 violated: api_key survived host change without new key: %q", v)
	}

	// Re-seed api_key on new host, then change host again while sending masked "********" -> must NOT keep old key or write "********".
	if rec := postCfg(tp.Manifest().ID, map[string]any{
		"api_key": "mirror-secret-key",
	}); rec.Code != http.StatusOK {
		t.Fatalf("re-seed api_key failed: %d", rec.Code)
	}
	if rec := postCfg(tp.Manifest().ID, map[string]any{
		"BaseUrl": "https://1.1.1.1/3",
		"api_key": maskedSecret,
	}); rec.Code != http.StatusOK {
		t.Fatalf("host change with masked secret failed: %d %s", rec.Code, rec.Body.String())
	}
	saved, _ = h.loadSavedConfig(context.Background(), tp.Manifest().ID)
	if v, ok := saved["api_key"].(string); ok && v != "" {
		t.Fatalf("H-03 violated: api_key survived host change with masked value: %q", v)
	}

	// Changing host WITH an explicit new non-masked secret preserves the new secret.
	if rec := postCfg(tp.Manifest().ID, map[string]any{
		"base_url": "https://8.8.8.8/3",
		"api_key":  "brand-new-secret",
	}); rec.Code != http.StatusOK {
		t.Fatalf("host change with explicit new key failed: %d", rec.Code)
	}
	saved, _ = h.loadSavedConfig(context.Background(), tp.Manifest().ID)
	if saved["api_key"] != "brand-new-secret" {
		t.Fatalf("expected explicit new api_key preserved on host change, got %+v", saved)
	}

	// 2. M-18: isSensitiveConfigKey covers pin/Pin/tvdb_pin/TVDBPin/credential/authorization/cookie without false positives.
	for _, sensitive := range []string{"pin", "Pin", "tvdb_pin", "TVDBPin", "userPin", "credential", "client_credentials", "authorization", "AuthorizationHeader", "cookie", "session_cookie"} {
		if !isSensitiveConfigKey(sensitive) {
			t.Fatalf("M-18: expected %q to be treated as sensitive", sensitive)
		}
	}
	for _, benign := range []string{"language", "mapping", "spin_speed", "timeout", "enabled"} {
		if isSensitiveConfigKey(benign) {
			t.Fatalf("M-18: expected %q NOT to be treated as sensitive", benign)
		}
	}

	// 3. M-27: p.Configure error rolls back partial in-memory mutations across update/enable/delete.
	mp := &rollbackMockPlugin{
		manifest:    plugins.Manifest{ID: "mock.rollback", Name: "Mock", Enabled: true},
		failOnValue: "boom",
	}
	reg.Register(mp)
	if rec := postCfg("mock.rollback", map[string]any{"tag": "good", "enabled": true}); rec.Code != http.StatusOK {
		t.Fatalf("seed mock config failed: %d", rec.Code)
	}
	if rec := postCfg("mock.rollback", map[string]any{"tag": "boom", "enabled": false}); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on failing Configure, got %d", rec.Code)
	}
	man, cfg := mp.snapshot()
	if !man.Enabled || cfg["tag"] != "good" {
		t.Fatalf("M-27: expected Configure failure rolled back to enabled=true tag=good, got enabled=%v cfg=%+v", man.Enabled, cfg)
	}

	mp.mu.Lock()
	mp.failNext = true
	mp.mu.Unlock()
	recEnable := httptest.NewRecorder()
	mux.ServeHTTP(recEnable, httptest.NewRequest(http.MethodPost, "/Plugins/mock.rollback/Enable?enabled=false", nil))
	if recEnable.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on failing Enable, got %d", recEnable.Code)
	}
	man, _ = mp.snapshot()
	if !man.Enabled {
		t.Fatalf("M-27: expected Enable Configure failure rolled back to enabled=true")
	}

	mp.mu.Lock()
	mp.failNext = true
	mp.mu.Unlock()
	recDel := httptest.NewRecorder()
	mux.ServeHTTP(recDel, httptest.NewRequest(http.MethodDelete, "/Plugins/mock.rollback", nil))
	if recDel.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on failing Delete, got %d", recDel.Code)
	}
	man, _ = mp.snapshot()
	if !man.Enabled {
		t.Fatalf("M-27: expected Delete Configure failure rolled back to enabled=true")
	}

	// 4. L-19: Submitting masked "********" when prior secret is empty must NOT persist literal "********".
	merged := mergePluginConfig(map[string]any{}, map[string]any{
		"api_key":  maskedSecret,
		"tvdb_pin": maskedSecret,
		"language": "en-US",
	})
	if v, exists := merged["api_key"]; exists && v == maskedSecret {
		t.Fatalf("L-19: must not write literal masked placeholder for empty api_key: %+v", merged)
	}
	if v, exists := merged["tvdb_pin"]; exists && v == maskedSecret {
		t.Fatalf("L-19: must not write literal masked placeholder for empty tvdb_pin: %+v", merged)
	}
	if merged["language"] != "en-US" {
		t.Fatalf("expected non-sensitive key preserved, got %+v", merged)
	}

	// L-19: findPlugin prefers latest instance from registry and updates h.tracked.
	replacement := &rollbackMockPlugin{
		manifest: plugins.Manifest{ID: "mock.rollback", Name: "MockV2", Enabled: true},
	}
	reg.Register(replacement)
	got, ok := h.findPlugin("mock.rollback")
	if !ok || got.Manifest().Name != "MockV2" {
		t.Fatalf("L-19: expected findPlugin to return latest registry instance MockV2, got ok=%v plugin=%+v", ok, got)
	}
	h.mu.RLock()
	trackedInst := h.tracked["mock.rollback"]
	h.mu.RUnlock()
	if trackedInst != plugins.Plugin(replacement) {
		t.Fatalf("L-19: expected h.tracked synchronized with latest registry instance")
	}
}
