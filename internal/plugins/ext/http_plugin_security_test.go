package ext

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func TestAuditV0109_M27_AtomicConfigure(t *testing.T) {
	p, err := NewHTTPPlugin(ExternalManifest{
		ID:        "ext.atomic",
		Name:      "Atomic Test",
		Endpoint:  "https://orig.example.com/api",
		APIKey:    "orig-key",
		Priority:  10,
		TimeoutMs: 5000,
	})
	if err != nil {
		t.Fatalf("NewHTTPPlugin: %v", err)
	}

	// Invalid TimeoutMs at the end must not leave APIKey, Endpoint, Enabled, or Priority mutated.
	err = p.Configure(map[string]any{
		"APIKey":    "new-key",
		"Endpoint":  "https://other.example.com/api",
		"Enabled":   false,
		"Priority":  99,
		"TimeoutMs": -1,
	})
	if err == nil {
		t.Fatal("expected error for negative TimeoutMs")
	}

	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.apiKey != "orig-key" || p.endpoint != "https://orig.example.com/api" || !p.enabled || p.priority != 10 {
		t.Fatalf("Configure partially mutated HTTPPlugin state: apiKey=%q endpoint=%q enabled=%v priority=%d",
			p.apiKey, p.endpoint, p.enabled, p.priority)
	}
}

func TestHTTPPluginNoLoopbackRuntimeConfigureSubtitlesNotifierAndWasmSkip(t *testing.T) {
	ctx := context.Background()

	// 1. Loader silently skips manifest.json that declares WasmFile without Endpoint (no error!)
	root := t.TempDir()
	wasmSubDir := filepath.Join(root, "wasm-only")
	_ = os.MkdirAll(wasmSubDir, 0o755)
	_ = os.WriteFile(filepath.Join(wasmSubDir, "manifest.json"), []byte(`{
		"Id": "ext.wasm.only",
		"Name": "WASM Scraper",
		"WasmFile": "plugin.wasm",
		"Enabled": true
	}`), 0o644)

	loader := NewLoader()
	reg := plugins.NewRegistry()
	n, err := loader.LoadFromDir(root, reg)
	if err != nil || n != 0 {
		t.Fatalf("expected WasmFile manifest with empty Endpoint silently skipped (0, nil), got (%d, %v)", n, err)
	}

	// 2. Runtime Configure MUST reject loopback / cloud metadata endpoints (127.0.0.1, localhost, ::1, 169.254.169.254)
	var notifiedEvent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search_subtitles":
			_ = json.NewEncoder(w).Encode([]plugins.RemoteSubtitleInfo{
				{ID: "ext-sub-1", Name: "Arrival.zh.srt", ProviderName: "ExtSub", Language: "zh", Format: "srt"},
			})
		case "/download_subtitle":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"language": "zh-CN",
				"format":   "srt",
				"content":  "1\n00:00:01,000 --> 00:00:02,000\n外挂字幕\n",
			})
		case "/notify":
			var np plugins.NotificationPayload
			_ = json.NewDecoder(r.Body).Decode(&np)
			notifiedEvent = np.Event
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	// Initial load from manifest allows explicit local endpoint
	hp, err := NewHTTPPlugin(ExternalManifest{
		ID:       "ext.sub.notify",
		Name:     "Sub & Notify Plugin",
		Endpoint: srv.URL,
		Priority: 25,
		Kinds:    []plugins.Kind{plugins.KindSubtitle, plugins.KindNotifier},
	})
	if err != nil {
		t.Fatalf("NewHTTPPlugin: %v", err)
	}

	// SubtitleProvider & Notifier forwarding
	subs, err := hp.SearchSubtitles(ctx, plugins.SubtitleSearchQuery{Title: "Arrival", Language: "zh"})
	if err != nil || len(subs) != 1 || subs[0].ID != "ext-sub-1" {
		t.Fatalf("SearchSubtitles got (%+v, %v)", subs, err)
	}
	dl, err := hp.DownloadSubtitle(ctx, "ext-sub-1")
	if err != nil || dl == nil || dl.Format != "srt" || len(dl.Content) == 0 {
		t.Fatalf("DownloadSubtitle got (%+v, %v)", dl, err)
	}
	if err := hp.Notify(ctx, plugins.NotificationPayload{Event: "library.new", Title: "Added"}); err != nil || notifiedEvent != "library.new" {
		t.Fatalf("Notify failed: err=%v event=%q", err, notifiedEvent)
	}

	// Runtime Configure attempting to redirect to another loopback port, localhost, or IMDS MUST be rejected!
	for _, unsafeURL := range []string{
		"http://127.0.0.1:6379/evil",
		"http://localhost:8096/System/Info",
		"http://[::1]:9000/api",
		"http://169.254.169.254/latest/meta-data",
	} {
		if err := hp.Configure(map[string]any{"Endpoint": unsafeURL}); err == nil {
			t.Fatalf("expected runtime Configure to block unsafe Endpoint %q", unsafeURL)
		}
	}

	defCfg := hp.DefaultConfig()
	if defCfg["priority"] != 25 || defCfg["enabled"] != true {
		t.Fatalf("unexpected DefaultConfig: %+v", defCfg)
	}
}
