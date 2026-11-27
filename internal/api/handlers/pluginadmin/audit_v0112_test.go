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

type dummyExtPlugin struct {
	id      string
	enabled bool
}

func (d *dummyExtPlugin) Manifest() plugins.Manifest {
	return plugins.Manifest{
		ID:      d.id,
		Name:    d.id,
		Version: "1.0.0",
		BuiltIn: false,
		Enabled: d.enabled,
		Kinds:   []plugins.Kind{plugins.KindMetadata},
	}
}

func (d *dummyExtPlugin) Configure(cfg map[string]any) error {
	if v, ok := cfg["enabled"].(bool); ok {
		d.enabled = v
	}
	return nil
}

func TestAuditV0112_UninstallPluginRemovesBundleSafelyAndBlocksTraversal(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "pa_v0112.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	pluginsDir := filepath.Join(tmp, "plugins")
	outsideDir := filepath.Join(tmp, "outside-keep")
	bundleDir := filepath.Join(pluginsDir, "custom-folder-name")
	keepSiblingDir := filepath.Join(pluginsDir, "other-plugin")
	for _, d := range []string{bundleDir, keepSiblingDir, outsideDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Write manifest.json inside bundleDir with Id="com.example.uninstallme"
	_ = os.WriteFile(filepath.Join(bundleDir, "manifest.json"), []byte(`{"Id":"com.example.uninstallme","Name":"Uninstall Me","WasmFile":"p.wasm"}`), 0o644)
	_ = os.WriteFile(filepath.Join(bundleDir, "p.wasm"), []byte("wasm"), 0o644)
	_ = os.WriteFile(filepath.Join(keepSiblingDir, "manifest.json"), []byte(`{"Id":"com.example.keep","Name":"Keep"}`), 0o644)

	// Symlink entry inside pluginsDir pointing to outsideDir MUST NEVER be deleted!
	outsideSentinel := filepath.Join(outsideDir, "sentinel.txt")
	_ = os.WriteFile(outsideSentinel, []byte("keep-safe"), 0o644)
	_ = os.Symlink(outsideDir, filepath.Join(pluginsDir, "com.example.symlink"))

	reg := plugins.NewRegistry()
	p1 := &dummyExtPlugin{id: "com.example.uninstallme", enabled: true}
	pSym := &dummyExtPlugin{id: "com.example.symlink", enabled: true}
	pDelRoute := &dummyExtPlugin{id: "com.example.keep", enabled: true}
	reg.Register(p1)
	reg.Register(pSym)
	reg.Register(pDelRoute)

	h := New(reg, store)
	h.SetPluginsDir(pluginsDir)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	// 1. Traversal plugin IDs rejected with 400 / ErrInvalidPluginID!
	if err := h.UninstallPlugin(ctx, "../outside-keep"); !errors.Is(err, ErrInvalidPluginID) {
		t.Fatalf("expected ErrInvalidPluginID for traversal ID, got %v", err)
	}

	// 2. POST /Plugins/{Id}/Uninstall removes bundleDir on disk, unregisters from Registry, and leaves keepSiblingDir intact!
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Plugins/com.example.uninstallme/Uninstall", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST /Uninstall status=%d body=%s", rec.Code, rec.Body.String())
	}
	if _, ok := reg.Get("com.example.uninstallme"); ok {
		t.Fatal("expected com.example.uninstallme unregistered from Registry")
	}
	if _, err := os.Stat(bundleDir); !os.IsNotExist(err) {
		t.Fatalf("expected plugin bundle directory %s deleted from disk, stat err=%v", bundleDir, err)
	}
	if _, err := os.Stat(keepSiblingDir); err != nil {
		t.Fatalf("expected sibling plugin dir untouched, got %v", err)
	}

	// 3. Uninstalling com.example.symlink unregisters plugin WITHOUT deleting symlink target outsideDir!
	recSym := httptest.NewRecorder()
	mux.ServeHTTP(recSym, httptest.NewRequest(http.MethodPost, "/Plugins/com.example.symlink/Delete", nil))
	if recSym.Code != http.StatusNoContent {
		t.Fatalf("POST /Delete on symlink plugin status=%d", recSym.Code)
	}
	if _, err := os.Stat(outsideSentinel); err != nil {
		t.Fatalf("symlink escape deleted outside target! err=%v", err)
	}

	// 4. POST /Plugins/{Id}/Delete also removes keepSiblingDir cleanly; unknown plugin returns 404!
	recDel := httptest.NewRecorder()
	mux.ServeHTTP(recDel, httptest.NewRequest(http.MethodPost, "/Plugins/com.example.keep/Delete", nil))
	if recDel.Code != http.StatusNoContent {
		t.Fatalf("POST /Delete status=%d", recDel.Code)
	}
	if _, err := os.Stat(keepSiblingDir); !os.IsNotExist(err) {
		t.Fatal("expected keepSiblingDir removed on POST /Delete")
	}

	rec404 := httptest.NewRecorder()
	mux.ServeHTTP(rec404, httptest.NewRequest(http.MethodPost, "/Plugins/does.not.exist/Uninstall", nil))
	if rec404.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on unknown plugin Uninstall, got %d", rec404.Code)
	}
}
