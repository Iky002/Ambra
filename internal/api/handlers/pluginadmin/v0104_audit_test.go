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
			"client_secret": maskedSecret, // sentinel MUST preserve nested-secret-2!
			"endpoint":      "https://api2.example.com",
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
	if savedAuth["endpoint"] != "https://api2.example.com" {
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
