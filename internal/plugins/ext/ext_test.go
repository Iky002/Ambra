package ext

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/plugins"
)

func boolPtr(b bool) *bool { return &b }

func TestLoaderLoadFromDir(t *testing.T) {
	// 1. Missing directory returns (0, nil)
	loader := NewLoader()
	reg := plugins.NewRegistry()
	n, err := loader.LoadFromDir(filepath.Join(t.TempDir(), "does-not-exist"), reg)
	if err != nil || n != 0 {
		t.Fatalf("missing dir: got (%d, %v), want (0, nil)", n, err)
	}

	// 2. Populated directory with valid, disabled, no-manifest subdir, plain file, and broken manifest
	root := t.TempDir()

	// Plain file (ignored)
	_ = os.WriteFile(filepath.Join(root, "README.txt"), []byte("ignore"), 0o644)
	// Subdir without manifest.json (ignored)
	_ = os.MkdirAll(filepath.Join(root, "empty-plugin"), 0o755)

	writeManifest := func(sub string, content string) {
		dir := filepath.Join(root, sub)
		_ = os.MkdirAll(dir, 0o755)
		_ = os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(content), 0o644)
	}

	writeManifest("alpha", `{
		"Id": "ext.alpha",
		"Name": "Alpha Scraper",
		"Version": "1.2.0",
		"Description": "External metadata scraper",
		"Endpoint": "http://127.0.0.1:9001/",
		"TimeoutMs": 2000,
		"Priority": 10,
		"Enabled": true,
		"Kinds": ["metadata"]
	}`)
	writeManifest("beta", `{
		"Id": "ext.beta",
		"Name": "Beta Resolver",
		"Version": "2.0.0",
		"Description": "External 302 stream resolver",
		"Endpoint": "http://127.0.0.1:9002",
		"Priority": 5,
		"Enabled": false,
		"Kinds": ["stream"]
	}`)
	writeManifest("broken-json", `{not valid json`)
	writeManifest("invalid-endpoint", `{
		"Id": "ext.bad",
		"Name": "Bad Endpoint",
		"Endpoint": "ftp://invalid"
	}`)

	n, err = loader.LoadFromDir(root, reg)
	if n != 2 {
		t.Fatalf("expected 2 valid plugins loaded, got %d (err=%v)", n, err)
	}
	if err == nil {
		t.Fatalf("expected joined error for broken manifests, got nil")
	}

	loaded := loader.Loaded()
	if len(loaded) != 2 || loaded[0].Manifest().ID != "ext.beta" || loaded[1].Manifest().ID != "ext.alpha" {
		t.Fatalf("unexpected Loaded() ordering: %+v", loaded)
	}

	// Registry should contain both manifests, only enabled metadata provider active
	all := reg.List()
	if len(all) != 2 {
		t.Fatalf("expected 2 registered plugins, got %d", len(all))
	}
	if got := reg.MetadataProviders(); len(got) != 1 || got[0].Manifest().ID != "ext.alpha" {
		t.Fatalf("unexpected MetadataProviders: %+v", got)
	}
	// ext.beta is disabled; ext.alpha is enabled and implements StreamResolver but skips non-stream kind
	resolvers := reg.StreamResolvers()
	if len(resolvers) != 1 || resolvers[0].Manifest().ID != "ext.alpha" {
		t.Fatalf("unexpected StreamResolvers: %+v", resolvers)
	}
	if sr, err := resolvers[0].ResolveStream(context.Background(), "/a.strm"); err != nil || sr != nil {
		t.Fatalf("metadata-only plugin should skip ResolveStream, got (%+v, %v)", sr, err)
	}
}

func TestHTTPPluginMetadataAndStreamResolve(t *testing.T) {
	var lastAuth, lastAPIKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastAuth = r.Header.Get("Authorization")
		lastAPIKey = r.Header.Get("X-Api-Key")
		switch r.URL.Path {
		case "/metadata":
			var q plugins.MediaQuery
			if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
				http.Error(w, "bad json", http.StatusBadRequest)
				return
			}
			if q.Title == "NoMatch" {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			if q.Title == "EmptyBody" {
				w.WriteHeader(http.StatusOK)
				return
			}
			if q.Title == "BadJSON" {
				_, _ = w.Write([]byte("{corrupt"))
				return
			}
			_ = json.NewEncoder(w).Encode(plugins.MetadataResult{
				Name:     "Resolved " + q.Title,
				Year:     2025,
				Overview: "From external plugin",
			})
		case "/resolve":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			switch body["path"] {
			case "/media/nomatch.strm":
				w.WriteHeader(http.StatusNoContent)
			case "/media/emptyurl.strm":
				_ = json.NewEncoder(w).Encode(plugins.StreamResolveResult{URL: ""})
			default:
				_ = json.NewEncoder(w).Encode(plugins.StreamResolveResult{
					URL:     "https://93.184.216.34/video.mp4",
					Headers: map[string]string{"User-Agent": "Ambra"},
					Is302:   true,
				})
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p, err := NewHTTPPlugin(ExternalManifest{
		ID:       "ext.dual",
		Name:     "Dual Plugin",
		Version:  "1.0.0",
		Endpoint: srv.URL,
		APIKey:   "secret-123",
		Priority: 15,
	})
	if err != nil {
		t.Fatalf("NewHTTPPlugin: %v", err)
	}

	// Default kinds include both metadata & stream
	m := p.Manifest()
	if m.BuiltIn || !m.Enabled || len(m.Kinds) != 2 {
		t.Fatalf("unexpected manifest: %+v", m)
	}

	// FetchMetadata success
	res, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{Title: "Dune", ItemType: "Movie"})
	if err != nil || res == nil || res.Name != "Resolved Dune" || res.Year != 2025 {
		t.Fatalf("FetchMetadata got (%+v, %v)", res, err)
	}
	if lastAuth != "Bearer secret-123" || lastAPIKey != "secret-123" {
		t.Fatalf("unexpected auth headers: auth=%q key=%q", lastAuth, lastAPIKey)
	}

	// FetchMetadata 204 & empty body -> (nil, nil)
	if res, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{Title: "NoMatch"}); err != nil || res != nil {
		t.Fatalf("expected (nil, nil) for 204, got (%+v, %v)", res, err)
	}
	if res, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{Title: "EmptyBody"}); err != nil || res != nil {
		t.Fatalf("expected (nil, nil) for empty body, got (%+v, %v)", res, err)
	}
	if _, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{Title: "BadJSON"}); err == nil {
		t.Fatalf("expected error for corrupt JSON")
	}

	// ResolveStream success, 204, empty URL
	sres, err := p.ResolveStream(context.Background(), "/media/movie.strm")
	if err != nil || sres == nil || !sres.Is302 || sres.URL != "https://93.184.216.34/video.mp4" {
		t.Fatalf("ResolveStream got (%+v, %v)", sres, err)
	}
	if sres, err := p.ResolveStream(context.Background(), "/media/nomatch.strm"); err != nil || sres != nil {
		t.Fatalf("expected (nil, nil) for 204 resolve, got (%+v, %v)", sres, err)
	}
	if sres, err := p.ResolveStream(context.Background(), "/media/emptyurl.strm"); err != nil || sres != nil {
		t.Fatalf("expected (nil, nil) for empty url, got (%+v, %v)", sres, err)
	}

	// Disable via Configure -> skips requests
	if err := p.Configure(map[string]any{"enabled": false}); err != nil {
		t.Fatalf("Configure disable: %v", err)
	}
	if res, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{Title: "Dune"}); err != nil || res != nil {
		t.Fatalf("disabled plugin should skip FetchMetadata")
	}
	if sres, err := p.ResolveStream(context.Background(), "/media/movie.strm"); err != nil || sres != nil {
		t.Fatalf("disabled plugin should skip ResolveStream")
	}
}

func TestHTTPPluginTimeoutAndCircuitBreaker(t *testing.T) {
	var hits atomic.Int32
	var shouldFail atomic.Bool
	shouldFail.Store(true)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/slow/metadata" {
			time.Sleep(150 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
			return
		}
		if shouldFail.Load() {
			http.Error(w, "upstream down", http.StatusBadGateway)
			return
		}
		_ = json.NewEncoder(w).Encode(plugins.MetadataResult{Name: "Recovered"})
	}))
	defer srv.Close()

	// 1. Strict HTTP timeout test
	slowPlugin, err := NewHTTPPlugin(ExternalManifest{
		ID:        "ext.slow",
		Name:      "Slow Plugin",
		Endpoint:  srv.URL + "/slow",
		TimeoutMs: 30,
		Kinds:     []plugins.Kind{plugins.KindMetadata},
	})
	if err != nil {
		t.Fatalf("NewHTTPPlugin slow: %v", err)
	}
	if _, err := slowPlugin.FetchMetadata(context.Background(), plugins.MediaQuery{Title: "Test"}); err == nil {
		t.Fatalf("expected timeout error")
	}

	// 2. Circuit breaker trips after 3 consecutive failures and recovers after resetTimeout
	p, err := NewHTTPPlugin(ExternalManifest{
		ID:        "ext.cb",
		Name:      "CB Plugin",
		Endpoint:  srv.URL,
		TimeoutMs: 500,
		Kinds:     []plugins.Kind{plugins.KindMetadata},
	})
	if err != nil {
		t.Fatalf("NewHTTPPlugin cb: %v", err)
	}
	fakeNow := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	p.nowFunc = func() time.Time { return fakeNow }

	hits.Store(0)
	for i := 0; i < 3; i++ {
		if _, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{Title: "Fail"}); err == nil {
			t.Fatalf("expected failure #%d", i+1)
		}
	}
	open, fails := p.CircuitState()
	if !open || fails != 3 {
		t.Fatalf("expected circuit open with 3 fails, got open=%v fails=%d", open, fails)
	}

	// Next call must fail fast with ErrCircuitOpen without hitting server
	beforeHits := hits.Load()
	_, err = p.FetchMetadata(context.Background(), plugins.MediaQuery{Title: "FailFast"})
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}
	if hits.Load() != beforeHits {
		t.Fatalf("circuit open should not hit remote server")
	}

	// Advance clock past resetTimeout and let server recover
	fakeNow = fakeNow.Add(defaultResetTimeout + time.Second)
	shouldFail.Store(false)

	res, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{Title: "Probe"})
	if err != nil || res == nil || res.Name != "Recovered" {
		t.Fatalf("expected recovery after cooldown, got (%+v, %v)", res, err)
	}
	open, fails = p.CircuitState()
	if open || fails != 0 {
		t.Fatalf("expected circuit closed and 0 fails after recovery, got open=%v fails=%d", open, fails)
	}
}

func TestHTTPPluginConfigureAndValidation(t *testing.T) {
	// Constructor validation
	if _, err := NewHTTPPlugin(ExternalManifest{Name: "NoID", Endpoint: "http://127.0.0.1:8080"}); err == nil {
		t.Fatalf("expected error for missing Id")
	}
	if _, err := NewHTTPPlugin(ExternalManifest{ID: "id", Endpoint: "http://127.0.0.1:8080"}); err == nil {
		t.Fatalf("expected error for missing Name")
	}
	if _, err := NewHTTPPlugin(ExternalManifest{ID: "id", Name: "n", Endpoint: ""}); err == nil {
		t.Fatalf("expected error for empty Endpoint")
	}

	p, err := NewHTTPPlugin(ExternalManifest{
		ID:       "ext.cfg",
		Name:     "Cfg Plugin",
		Endpoint: "http://127.0.0.1:8080",
		Enabled:  boolPtr(true),
		Kinds:    []plugins.Kind{plugins.KindStream},
	})
	if err != nil {
		t.Fatalf("NewHTTPPlugin: %v", err)
	}

	// Nil configure is a no-op
	if err := p.Configure(nil); err != nil {
		t.Fatalf("nil Configure: %v", err)
	}

	// Update all supported fields (including float64 from JSON)
	err = p.Configure(map[string]any{
		"Endpoint":  "https://plugin.example.com/",
		"APIKey":    "k-999",
		"Enabled":   false,
		"Priority":  float64(42),
		"TimeoutMs": int64(1500),
	})
	if err != nil {
		t.Fatalf("Configure valid: %v", err)
	}
	m := p.Manifest()
	if m.Enabled || m.Priority != 42 {
		t.Fatalf("unexpected manifest after Configure: %+v", m)
	}

	// Int priority
	if err := p.Configure(map[string]any{"priority": 7, "enabled": true}); err != nil {
		t.Fatalf("Configure int priority: %v", err)
	}
	if p.Manifest().Priority != 7 || !p.Manifest().Enabled {
		t.Fatalf("expected priority=7 enabled=true")
	}

	// Kind mismatch skips Metadata call
	if res, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{Title: "Skip"}); err != nil || res != nil {
		t.Fatalf("stream-only plugin should skip metadata")
	}

	// Invalid Configure inputs
	for _, bad := range []map[string]any{
		{"endpoint": "ftp://bad"},
		{"priority": "not-an-int"},
		{"timeout_ms": "not-an-int"},
		{"timeout_ms": 0},
	} {
		if err := p.Configure(bad); err == nil {
			t.Fatalf("expected error for invalid cfg %+v", bad)
		}
	}
}

func TestHTTPPluginAuditV0104Hardening(t *testing.T) {
	// 1. Reject ambra.builtin.* prefix
	if _, err := NewHTTPPlugin(ExternalManifest{
		ID:       "ambra.builtin.hijack",
		Name:     "Hijack",
		Endpoint: "http://127.0.0.1:9000",
	}); err == nil {
		t.Fatal("expected ambra.builtin.* HTTPPlugin to be rejected")
	}

	// 2. TimeoutMs upper bound clamped to 60000ms in constructor and Configure
	p, err := NewHTTPPlugin(ExternalManifest{
		ID:        "ext.clamp",
		Name:      "Clamp",
		Endpoint:  "http://127.0.0.1:9000",
		TimeoutMs: 999_999,
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.timeout != maxTimeout {
		t.Fatalf("expected constructor timeout clamped to %v, got %v", maxTimeout, p.timeout)
	}
	if err := p.Configure(map[string]any{"TimeoutMs": 500_000}); err != nil {
		t.Fatal(err)
	}
	if p.timeout != maxTimeout {
		t.Fatalf("expected Configure timeout clamped to %v, got %v", maxTimeout, p.timeout)
	}

	// 3. ResolveStream SSRF validation & half-open single-probe concurrency control
	probeStarted := make(chan struct{})
	releaseProbe := make(chan struct{})
	var probeHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/resolve":
			_ = json.NewEncoder(w).Encode(plugins.StreamResolveResult{
				URL:   "http://169.254.169.254/latest/meta-data",
				Is302: true,
			})
		case "/metadata":
			n := probeHits.Add(1)
			if n <= 3 {
				http.Error(w, "fail", http.StatusBadGateway)
				return
			}
			// Half-open probe blocks until released
			close(probeStarted)
			<-releaseProbe
			_ = json.NewEncoder(w).Encode(plugins.MetadataResult{Name: "OK"})
		}
	}))
	defer srv.Close()

	hp, err := NewHTTPPlugin(ExternalManifest{
		ID:        "ext.sec",
		Name:      "Sec",
		Endpoint:  srv.URL,
		TimeoutMs: 2000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hp.ResolveStream(context.Background(), "/a.strm"); err == nil {
		t.Fatal("expected SSRF stream URL to be rejected")
	}

	fakeNow := time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)
	hp.nowFunc = func() time.Time { return fakeNow }
	for i := 0; i < 3; i++ {
		_, _ = hp.FetchMetadata(context.Background(), plugins.MediaQuery{Title: "Fail"})
	}
	// Advance past cooldown -> half-open
	fakeNow = fakeNow.Add(defaultResetTimeout + time.Second)
	done := make(chan error, 1)
	go func() {
		_, err := hp.FetchMetadata(context.Background(), plugins.MediaQuery{Title: "Probe1"})
		done <- err
	}()
	<-probeStarted
	// Second concurrent request while probe is in flight MUST get ErrCircuitOpen
	if _, err := hp.FetchMetadata(context.Background(), plugins.MediaQuery{Title: "Probe2"}); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen for concurrent half-open call, got %v", err)
	}
	close(releaseProbe)
	if err := <-done; err != nil {
		t.Fatalf("expected single probe to succeed, got %v", err)
	}
}
