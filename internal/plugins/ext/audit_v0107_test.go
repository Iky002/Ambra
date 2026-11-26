package ext

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ambra-media/ambra/internal/plugins"
)

func TestAuditV0107_H9_HostChangeClearsKeyAndLoaderSecurityLimits(t *testing.T) {
	// 1. H-9: Endpoint host change without new APIKey clears existing apiKey
	p, err := NewHTTPPlugin(ExternalManifest{
		ID:       "ext.hostchange",
		Name:     "Host Change Plugin",
		Endpoint: "https://93.184.216.34/api",
		APIKey:   "secret-ext-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	p.SetHTTPClient(nil)
	p.SetHTTPClient(&http.Client{})

	if err := p.Configure(map[string]any{"Endpoint": "https://1.1.1.1/api"}); err != nil {
		t.Fatalf("Configure host change: %v", err)
	}
	p.mu.RLock()
	gotKey := p.apiKey
	p.mu.RUnlock()
	if gotKey != "" {
		t.Fatalf("expected apiKey cleared on Endpoint host change, got %q", gotKey)
	}
	if urlHostLower("://bad") != "" {
		t.Fatal("expected empty host on invalid URL")
	}

	// 2. Low: Loader enforces 1 MiB manifest limit & blocks symlinks escaping plugins dir
	tmp := t.TempDir()
	pluginsDir := filepath.Join(tmp, "plugins")
	outsideDir := filepath.Join(tmp, "outside")
	_ = os.MkdirAll(filepath.Join(pluginsDir, "oversized"), 0o755)
	_ = os.MkdirAll(outsideDir, 0o755)

	oversized := bytes.Repeat([]byte("a"), maxManifestBytes+128)
	_ = os.WriteFile(filepath.Join(pluginsDir, "oversized", "manifest.json"), oversized, 0o600)

	if runtime.GOOS != "windows" {
		_ = os.MkdirAll(filepath.Join(pluginsDir, "escaped"), 0o755)
		outsideManifest := filepath.Join(outsideDir, "manifest.json")
		_ = os.WriteFile(outsideManifest, []byte(`{"Id":"ext.escaped","Name":"Escaped","Endpoint":"https://example.com"}`), 0o600)
		_ = os.Symlink(outsideManifest, filepath.Join(pluginsDir, "escaped", "manifest.json"))
	}

	loader := NewLoader()
	reg := plugins.NewRegistry()
	n, loadErr := loader.LoadFromDir(pluginsDir, reg)
	if n != 0 || loadErr == nil {
		t.Fatalf("expected 0 loaded and error for oversized/escaping manifests, got n=%d err=%v", n, loadErr)
	}
}
