package pluginadmin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

type covFilteringPlugin struct {
	id      string
	builtIn bool
	enabled bool
	failCfg bool
	cfg     map[string]any
}

func (p *covFilteringPlugin) Manifest() plugins.Manifest {
	return plugins.Manifest{
		ID:      p.id,
		Name:    p.id,
		Version: "1.0.0",
		BuiltIn: p.builtIn,
		Enabled: p.enabled,
	}
}

func (p *covFilteringPlugin) Configure(cfg map[string]any) error {
	if p.failCfg {
		return errors.New("configure failed")
	}
	if v, ok := cfg["enabled"].(bool); ok {
		p.enabled = v
	}
	p.cfg = cfg
	return nil
}

func (p *covFilteringPlugin) FilterServerConfig(cfg map[string]any) map[string]any {
	out := make(map[string]any, len(cfg))
	for k, v := range cfg {
		if k != "legacy_user_token" {
			out[k] = v
		}
	}
	return out
}

func TestPluginAdminCovV0112_HelperFunctionsAndSliceMerge(t *testing.T) {
	// 1. extractURLHostLower & isURLOrHostConfigKey
	for _, tc := range []struct {
		in   any
		want string
	}{
		{in: 123, want: ""},
		{in: "   ", want: ""},
		{in: "https://API.Example.COM:8443/v1", want: "api.example.com:8443"},
		{in: "Sub.Host.Local:9000", want: "sub.host.local:9000"},
		{in: "has spaces/slash", want: ""},
	} {
		if got := extractURLHostLower(tc.in); got != tc.want {
			t.Fatalf("extractURLHostLower(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if isURLOrHostConfigKey("   ") {
		t.Fatal("blank key must not be URL/host config key")
	}
	if !isURLOrHostConfigKey("custom_service_endpoint") || !isURLOrHostConfigKey("mirror_host") {
		t.Fatal("expected suffix URL/host keys to match")
	}

	// 2. stripSensitiveKeys with nested maps & non-empty secrets
	cleaned, cleared := stripSensitiveKeys(map[string]any{
		"api_key": "sec-1",
		"tvdbpin": "1234",
		"nested": map[string]any{
			"auth_token": "tok-2",
			"safe_flag":  true,
		},
		"title": "Demo",
	})
	if len(cleared) != 2 {
		t.Fatalf("expected 2 top-level cleared keys, got %v", cleared)
	}
	nestedCleaned, _ := cleaned["nested"].(map[string]any)
	if _, exists := nestedCleaned["auth_token"]; exists || nestedCleaned["safe_flag"] != true {
		t.Fatalf("expected nested sensitive key stripped, got %+v", nestedCleaned)
	}

	// 3. mergePluginSlice: sensitive masked strings (preserving existing vs clearing missing), nested maps, nested slices, primitives
	prevSlice := []any{
		"real-secret-0",
		map[string]any{"api_key": "nested-secret", "name": "old"},
		[]any{"sub-secret-0"},
		42,
	}
	inSlice := []any{
		maskedSecret, // preserves "real-secret-0"
		map[string]any{"api_key": maskedSecret, "name": "new"},
		[]any{maskedSecret, maskedSecret}, // index 0 preserves "sub-secret-0", index 1 becomes ""
		99,
		maskedSecret, // no prev element -> ""
		map[string]any{"extra": "v"},
		[]any{"fresh"},
	}
	mergedSlice := mergePluginSlice(prevSlice, inSlice, true)
	if mergedSlice[0] != "real-secret-0" || mergedSlice[4] != "" || mergedSlice[3] != 99 {
		t.Fatalf("unexpected mergedSlice primitives/secrets: %+v", mergedSlice)
	}
	subMap := mergedSlice[1].(map[string]any)
	if subMap["api_key"] != "nested-secret" || subMap["name"] != "new" {
		t.Fatalf("unexpected mergedSlice map: %+v", subMap)
	}
	subArr := mergedSlice[2].([]any)
	if subArr[0] != "sub-secret-0" || subArr[1] != "" {
		t.Fatalf("unexpected mergedSlice nested slice: %+v", subArr)
	}

	// Also exercise maskSensitiveMap & mergePluginConfig with []any slices
	masked := maskSensitiveMap(map[string]any{
		"tokens": []any{"tokA", ""},
	})
	maskedToks := masked["tokens"].([]any)
	if maskedToks[0] != maskedSecret || maskedToks[1] != "" {
		t.Fatalf("unexpected masked slice: %+v", maskedToks)
	}
	mergedCfg := mergePluginConfig(
		map[string]any{"tokens": []any{"tokA"}},
		map[string]any{"tokens": []any{maskedSecret}, "new_list": []any{"x"}},
	)
	if mergedCfg["tokens"].([]any)[0] != "tokA" {
		t.Fatalf("expected slice token preserved via mergePluginConfig, got %+v", mergedCfg)
	}

	// 4. isValidPluginLookupID & isDirectChildDir edge cases
	for _, badID := range []string{"", ".", "..", "a..b", "a/b", "a\\b", "bad\x00id", "bad\x7fid"} {
		if isValidPluginLookupID(badID) {
			t.Fatalf("expected invalid plugin ID for %q", badID)
		}
	}
	root := t.TempDir()
	if isDirectChildDir(root, root) || isDirectChildDir(filepath.Join(root, "child"), root) || isDirectChildDir(root, filepath.Join(root, "a", "b")) {
		t.Fatal("expected isDirectChildDir false for root, parent, and grandchild")
	}
}

func TestPluginAdminCovV0112_BundleMatchUninstallAndApplySavedConfigs(t *testing.T) {
	tmp := t.TempDir()
	st, err := sqlite.Open(filepath.Join(tmp, "pa.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	reg := plugins.NewRegistry()

	extPlug := &covFilteringPlugin{id: "ext.demo.bundle", builtIn: false, enabled: true}
	failPlug := &covFilteringPlugin{id: "ext.fail.cfg", builtIn: false, enabled: true}
	reg.Register(extPlug)
	reg.Register(failPlug)

	pluginsDir := filepath.Join(tmp, "plugins")
	// 1) Matching bundle dir with manifest omitting Id (falls back to dirName)
	bundle1 := filepath.Join(pluginsDir, "ext.demo.bundle")
	_ = os.MkdirAll(bundle1, 0o755)
	_ = os.WriteFile(filepath.Join(bundle1, "manifest.json"), []byte(`{"Name":"Demo"}`), 0o644)
	// 2) Non-regular manifest.json (directory) -> refused by pluginDirMatchesID
	bundleBadManifest := filepath.Join(pluginsDir, "bad-manifest-dir")
	_ = os.MkdirAll(filepath.Join(bundleBadManifest, "manifest.json"), 0o755)
	// 3) Dir without manifest matching by dirName
	bundleNoManifest := filepath.Join(pluginsDir, "no-manifest-other")
	_ = os.MkdirAll(bundleNoManifest, 0o755)
	// 4) Symlink entry inside pluginsDir -> skipped safely
	_ = os.Symlink(bundleNoManifest, filepath.Join(pluginsDir, "sym-plugin"))

	if pluginDirMatchesID(bundleBadManifest, "ext.demo.bundle", "ext.demo.bundle", "ext.demo.bundle") {
		t.Fatal("directory manifest.json must not match")
	}
	if !pluginDirMatchesID(bundleNoManifest, "no-manifest-other", "no-manifest-other", "no-manifest-other") {
		t.Fatal("missing manifest.json should fall back to dirName match")
	}

	h := New(reg, st)
	h.SetPluginsDir(pluginsDir)

	// Seed server_config with valid filtered entry, invalid JSON entry (skipped), and failing plugin entry
	_, _ = st.Writer.ExecContext(ctx,
		`INSERT INTO server_config (key, value_json, updated_at) VALUES (?, ?, 1), (?, ?, 1), (?, ?, 1)`,
		"plugin:ext.demo.bundle", `{"enabled":true,"legacy_user_token":"strip-me","keep":"yes"}`,
		"plugin:corrupt", `{not-json`,
		"plugin:ext.fail.cfg", `{"enabled":true}`,
	)
	failPlug.failCfg = true
	if err := h.ApplySavedConfigs(ctx); err == nil {
		t.Fatal("expected joined error from failing plugin Configure in ApplySavedConfigs")
	}
	if _, leaked := extPlug.cfg["legacy_user_token"]; leaked || extPlug.cfg["keep"] != "yes" {
		t.Fatalf("expected ServerConfigFilter to strip legacy_user_token, got %+v", extPlug.cfg)
	}

	// HTTP routes: DELETE invalid ID (400), missing ID (404), failing Configure on uninstall (500), and case-insensitive external uninstall (204)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	reqBad := httptest.NewRequest(http.MethodDelete, "/Plugins/bad..id", nil)
	recBad := httptest.NewRecorder()
	mux.ServeHTTP(recBad, reqBad)
	if recBad.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid plugin id, got %d", recBad.Code)
	}

	reqNotFound := httptest.NewRequest(http.MethodDelete, "/Plugins/does.not.exist", nil)
	recNotFound := httptest.NewRecorder()
	mux.ServeHTTP(recNotFound, reqNotFound)
	if recNotFound.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown plugin, got %d", recNotFound.Code)
	}

	reqFail := httptest.NewRequest(http.MethodDelete, "/Plugins/ext.fail.cfg", nil)
	recFail := httptest.NewRecorder()
	mux.ServeHTTP(recFail, reqFail)
	if recFail.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when Configure fails during UninstallPlugin, got %d", recFail.Code)
	}

	// Case-insensitive uninstall of ext.demo.bundle removes bundle1 from disk!
	reqDel := httptest.NewRequest(http.MethodPost, "/Plugins/EXT.DEMO.BUNDLE/Uninstall", nil)
	recDel := httptest.NewRecorder()
	mux.ServeHTTP(recDel, reqDel)
	if recDel.Code != http.StatusNoContent {
		t.Fatalf("expected 204 on external plugin uninstall, got %d: %s", recDel.Code, recDel.Body.String())
	}
	if _, err := os.Stat(bundle1); !os.IsNotExist(err) {
		t.Fatalf("expected external bundle directory removed, stat err=%v", err)
	}

	// removeExternalPluginBundle when pluginsDir does not exist or is a regular file returns nil cleanly
	h.SetPluginsDir(filepath.Join(tmp, "missing-plugins-dir"))
	if err := h.removeExternalPluginBundle("x", "x"); err != nil {
		t.Fatalf("expected nil for missing pluginsDir, got %v", err)
	}
	regFile := filepath.Join(tmp, "not-a-dir")
	_ = os.WriteFile(regFile, []byte("x"), 0o644)
	h.SetPluginsDir(regFile)
	if err := h.removeExternalPluginBundle("x", "x"); err != nil {
		t.Fatalf("expected nil when pluginsDir is a regular file, got %v", err)
	}
}
