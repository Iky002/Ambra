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

type capturePlugin struct {
	id      string
	lastCfg map[string]any
}

func (p *capturePlugin) Manifest() plugins.Manifest {
	return plugins.Manifest{ID: p.id, Name: "Capture", Enabled: true, Priority: 10}
}

func (p *capturePlugin) Configure(cfg map[string]any) error {
	p.lastCfg = cfg
	return nil
}

func TestPluginAdminDeepMaskMergeAndClearSecret(t *testing.T) {
	tmp := t.TempDir()
	st, err := sqlite.Open(filepath.Join(tmp, "pa_v0104.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	cp := &capturePlugin{id: "test.nested"}
	h := New(nil, st)
	h.Track(cp)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	// 1. Save deeply nested config containing top-level, nested map, and slice secrets
	initial := map[string]any{
		"api_key":  "top-secret-1",
		"language": "zh-CN",
		"auth": map[string]any{
			"client_secret": "nested-secret-2",
			"endpoint":      "https://api.example.com",
		},
		"providers": []any{
			map[string]any{
				"name":      "primary",
				"api_token": "slice-token-3",
			},
			map[string]any{
				"name":     "backup",
				"password": "slice-password-4",
			},
		},
		"backup_keys": []any{"k-one", "k-two"},
	}
	rawInit, _ := json.Marshal(initial)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Plugins/test.nested/Configuration", bytes.NewReader(rawInit)))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST initial config failed: %d %s", rec.Code, rec.Body.String())
	}

	// Verify response recursively masked every secret without leaking plaintext
	respStr := rec.Body.String()
	for _, secret := range []string{"top-secret-1", "nested-secret-2", "slice-token-3", "slice-password-4", "k-one", "k-two"} {
		if bytes.Contains([]byte(respStr), []byte(secret)) {
			t.Fatalf("expected secret %q to be masked in POST response: %s", secret, respStr)
		}
	}

	// GET Configuration also returns deeply masked secrets
	recGet := httptest.NewRecorder()
	mux.ServeHTTP(recGet, httptest.NewRequest(http.MethodGet, "/Plugins/test.nested/Configuration", nil))
	if recGet.Code != http.StatusOK {
		t.Fatalf("GET config failed: %d", recGet.Code)
	}
	var got map[string]any
	_ = json.Unmarshal(recGet.Body.Bytes(), &got)
	if got["api_key"] != maskedSecret {
		t.Fatalf("expected top-level api_key masked, got %v", got["api_key"])
	}
	authMap := got["auth"].(map[string]any)
	if authMap["client_secret"] != maskedSecret || authMap["endpoint"] != "https://api.example.com" {
		t.Fatalf("unexpected nested auth map: %+v", authMap)
	}
	provs := got["providers"].([]any)
	if provs[0].(map[string]any)["api_token"] != maskedSecret || provs[1].(map[string]any)["password"] != maskedSecret {
		t.Fatalf("expected slice secrets masked: %+v", provs)
	}
	bkeys := got["backup_keys"].([]any)
	if bkeys[0] != maskedSecret || bkeys[1] != maskedSecret {
		t.Fatalf("expected backup_keys elements masked: %+v", bkeys)
	}

	// 2. Update with "********" sentinel preserves old secrets recursively, while explicit "" CLEARS secrets!
	update := map[string]any{
		"api_key":  "", // explicit empty string MUST clear top-secret-1!
		"language": "en-US",
		"auth": map[string]any{
			"client_secret": maskedSecret, // sentinel MUST preserve nested-secret-2 when host is unchanged!
			"endpoint":      "https://api.example.com/v2",
		},
		"providers": []any{
			map[string]any{
				"name":      "primary",
				"api_token": maskedSecret, // preserve slice-token-3
			},
			map[string]any{
				"name":     "backup",
				"password": "", // explicit empty string MUST clear slice-password-4!
			},
		},
		"backup_keys": []any{maskedSecret, ""}, // preserve k-one, clear k-two
	}
	rawUpd, _ := json.Marshal(update)
	recUpd := httptest.NewRecorder()
	mux.ServeHTTP(recUpd, httptest.NewRequest(http.MethodPost, "/Plugins/test.nested/Configuration", bytes.NewReader(rawUpd)))
	if recUpd.Code != http.StatusOK {
		t.Fatalf("POST update failed: %d %s", recUpd.Code, recUpd.Body.String())
	}

	saved, err := h.loadSavedConfig(context.Background(), "test.nested")
	if err != nil {
		t.Fatalf("loadSavedConfig: %v", err)
	}
	if saved["api_key"] != "" {
		t.Fatalf("expected explicit empty api_key to clear secret, got %q", saved["api_key"])
	}
	savedAuth := saved["auth"].(map[string]any)
	if savedAuth["client_secret"] != "nested-secret-2" {
		t.Fatalf("expected sentinel to preserve nested client_secret, got %v", savedAuth["client_secret"])
	}
	if savedAuth["endpoint"] != "https://api.example.com/v2" {
		t.Fatalf("expected updated endpoint, got %v", savedAuth["endpoint"])
	}
	savedProvs := saved["providers"].([]any)
	if savedProvs[0].(map[string]any)["api_token"] != "slice-token-3" {
		t.Fatalf("expected sentinel to preserve slice api_token, got %+v", savedProvs[0])
	}
	if savedProvs[1].(map[string]any)["password"] != "" {
		t.Fatalf("expected empty string to clear slice password, got %+v", savedProvs[1])
	}
	savedBKeys := saved["backup_keys"].([]any)
	if savedBKeys[0] != "k-one" || savedBKeys[1] != "" {
		t.Fatalf("unexpected backup_keys after merge: %+v", savedBKeys)
	}

	// 3. Deep clone safety: mutating plugin's received map must not corrupt handler's in-memory copy
	cp.lastCfg["auth"].(map[string]any)["client_secret"] = "MUTATED"
	h.mu.RLock()
	inMemSecret := h.configs["test.nested"]["auth"].(map[string]any)["client_secret"]
	h.mu.RUnlock()
	if inMemSecret != "nested-secret-2" {
		t.Fatalf("expected deep clone isolation, got %v", inMemSecret)
	}
}

func TestPluginAdminV0107AuditConcurrentConfigAndDeterministicMerge(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "pluginadmin_v107.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	reg := plugins.NewRegistry()
	p := tmdb.New("", "zh-CN")
	reg.Register(p)
	h := New(reg, store)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	// 1. Concurrent GET and POST /Plugins/{Id}/Configuration has no race under cfgMu
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Plugins/"+p.Manifest().ID+"/Configuration", nil))
			if rec.Code != http.StatusOK {
				t.Errorf("concurrent GET config failed: %d", rec.Code)
			}
		}()
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Plugins/"+p.Manifest().ID+"/Configuration", bytes.NewReader([]byte(`{"api_key":"secret123","timeout":10}`))))
			if rec.Code != http.StatusOK {
				t.Errorf("concurrent POST config failed: %d", rec.Code)
			}
		}()
	}
	wg.Wait()

	// 2. Deterministic key ordering in mergePluginConfigInternal when colliding aliases appear in incoming map
	prev := map[string]any{}
	incoming := map[string]any{
		"apiKey":  "from-camel",
		"api_key": "from-snake",
	}
	first := mergePluginConfig(prev, incoming)
	for i := 0; i < 20; i++ {
		again := mergePluginConfig(prev, incoming)
		if len(again) != len(first) || again["apiKey"] != first["apiKey"] {
			t.Fatalf("non-deterministic mergePluginConfig: first=%+v again=%+v", first, again)
		}
	}
}

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
