package ext

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/plugins"
)

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
