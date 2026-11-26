package wasmhost

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ambra-media/ambra/internal/plugins"
)

func TestAuditV0109_M31_RejectsManifestSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink test requires unix")
	}
	ctx := context.Background()
	host, err := NewHost(ctx)
	if err != nil {
		t.Fatalf("NewHost: %v", err)
	}
	defer host.Close(ctx)

	tmp := t.TempDir()
	pluginsDir := filepath.Join(tmp, "plugins")
	outsideDir := filepath.Join(tmp, "outside")
	escapedPluginDir := filepath.Join(pluginsDir, "escaped-manifest")
	if err := os.MkdirAll(escapedPluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Fatal(err)
	}

	outsideManifest := filepath.Join(outsideDir, "manifest.json")
	if err := os.WriteFile(outsideManifest, []byte(`{"Id":"escaped","Name":"Escaped","Enabled":true,"WasmFile":"ok.wasm"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(escapedPluginDir, "ok.wasm"), buildNoopModule(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideManifest, filepath.Join(escapedPluginDir, "manifest.json")); err != nil {
		t.Fatal(err)
	}

	reg := plugins.NewRegistry()
	n, err := host.LoadFromDir(ctx, pluginsDir, reg)
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected symlink-escaped manifest to be rejected (loaded=0), got %d", n)
	}
}
