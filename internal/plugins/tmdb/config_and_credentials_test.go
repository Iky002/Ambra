package tmdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ambra-media/ambra/internal/plugins"
)

func TestAuditV0107_H9_HostChangeClearsKeyAndSingleSnapshotRedaction(t *testing.T) {
	p := New("secret-user-key-123", "zh-CN")

	// Changing host without providing new api_key clears existing p.apiKey
	if err := p.Configure(map[string]any{"base_url": "https://93.184.216.34/3"}); err != nil {
		t.Fatalf("Configure host change: %v", err)
	}
	if got := p.snapshot().apiKey; got != "" {
		t.Fatalf("expected apiKey cleared on host change without new key, got %q", got)
	}

	// Same host path change preserves api_key
	if err := p.Configure(map[string]any{"api_key": "keep-key", "base_url": "https://93.184.216.34/v3"}); err != nil {
		t.Fatal(err)
	}
	if err := p.Configure(map[string]any{"base_url": "https://93.184.216.34/v3-alt"}); err != nil {
		t.Fatal(err)
	}
	if got := p.snapshot().apiKey; got != "keep-key" {
		t.Fatalf("expected apiKey preserved on same-host change, got %q", got)
	}

	// Concurrent Configure key rotation + failing getJSON must never leak either key in errors
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closedURL := srv.URL
	srv.Close()

	p2 := New("secret-alpha-key", "zh-CN")
	p2.SetHTTPClient(srv.Client())
	_ = p2.Configure(map[string]any{"base_url": closedURL, "api_key": "secret-alpha-key"})

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 40; i++ {
			k := "secret-alpha-key"
			if i%2 == 1 {
				k = "secret-beta-key"
			}
			_ = p2.Configure(map[string]any{"api_key": k})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 40; i++ {
			_, err := p2.FetchMetadata(context.Background(), plugins.MediaQuery{
				ItemType:    "Movie",
				ProviderIDs: map[string]string{"tmdb": "550"},
			})
			if err != nil {
				msg := err.Error()
				if strings.Contains(msg, "secret-alpha-key") || strings.Contains(msg, "secret-beta-key") {
					t.Errorf("API key leaked in error message: %s", msg)
				}
			}
		}
	}()
	wg.Wait()
}

func TestAuditV0107_M14_PickBestLogoDemotesSVG(t *testing.T) {
	logos := []tmdbLogoEntry{
		{FilePath: "/zh_logo.svg", ISO6391: "zh", VoteAverage: 9.9},
		{FilePath: "/en_logo.webp", ISO6391: "en", VoteAverage: 5.0},
		{FilePath: "/en_logo.jpg", ISO6391: "en", VoteAverage: 6.0},
	}
	if got := pickBestLogoPath(logos, "zh-CN"); got != "/en_logo.webp" {
		t.Fatalf("expected raster .webp to beat .svg, got %q", got)
	}
	logosWithPNG := append(logos, tmdbLogoEntry{FilePath: "/en_logo.png", ISO6391: "en", VoteAverage: 3.0})
	if got := pickBestLogoPath(logosWithPNG, "zh-CN"); got != "/en_logo.png" {
		t.Fatalf("expected .png to beat .webp/.jpg/.svg, got %q", got)
	}
}

func TestAuditV0109_H04_DefaultAPIKeyNotLeakedOnCustomBaseURL(t *testing.T) {
	var seenKeys []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenKeys = append(seenKeys, r.URL.Query().Get("api_key"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":550,"title":"Fight Club"}`))
	}))
	defer srv.Close()

	p := New("", "zh-CN").WithDefaultAPIKey("builtin-secret-tmdb-key")
	p.SetHTTPClient(srv.Client())

	// Default host allows fallback to built-in key.
	if got := p.snapshot().apiKey; got != "builtin-secret-tmdb-key" {
		t.Fatalf("expected default key on default host, got %q", got)
	}

	// Switching base_url to another host must NOT fall back to built-in defaultAPIKey.
	if err := p.Configure(map[string]any{"base_url": srv.URL}); err != nil {
		t.Fatalf("Configure custom base_url: %v", err)
	}
	if got := p.snapshot().apiKey; got != "" {
		t.Fatalf("expected empty apiKey on non-default host, got %q", got)
	}
	res, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{
		ItemType:    "Movie",
		ProviderIDs: map[string]string{"tmdb": "550"},
	})
	if err != nil || res != nil {
		t.Fatalf("expected graceful skip without leaking default key, got res=%+v err=%v", res, err)
	}
	if len(seenKeys) != 0 {
		t.Fatalf("expected 0 requests sent to custom host without user key, got %v", seenKeys)
	}

	// Explicitly providing a user key for custom host works as expected.
	if err := p.Configure(map[string]any{"api_key": "explicit-proxy-key"}); err != nil {
		t.Fatalf("Configure explicit key: %v", err)
	}
	if got := p.snapshot().apiKey; got != "explicit-proxy-key" {
		t.Fatalf("expected explicit-proxy-key, got %q", got)
	}
}

func TestAuditV0109_M27_AtomicConfigureAndL28_PascalCaseEnabled(t *testing.T) {
	p := New("orig-key", "zh-CN")
	// Invalid image_base_url must not leave api_key / base_url / language / enabled partially mutated.
	err := p.Configure(map[string]any{
		"api_key":        "mutated-key",
		"language":       "en-US",
		"base_url":       "https://proxy.example.com/3",
		"image_base_url": "ftp://invalid.example.com",
		"enabled":        false,
	})
	if err == nil {
		t.Fatal("expected error for invalid image_base_url")
	}
	snap := p.snapshot()
	if snap.apiKey != "orig-key" || snap.language != "zh-CN" || snap.baseURL != defaultBaseURL || snap.imageBaseURL != defaultImageBaseURL || !snap.enabled {
		t.Fatalf("Configure partially mutated provider state on error: %+v", snap)
	}

	// L-28: PascalCase Enabled
	if err := p.Configure(map[string]any{"Enabled": false}); err != nil {
		t.Fatalf("Configure Enabled=false: %v", err)
	}
	if p.Manifest().Enabled {
		t.Fatal("expected Enabled=false via PascalCase key")
	}
	if err := p.Configure(map[string]any{"Enabled": true}); err != nil {
		t.Fatalf("Configure Enabled=true: %v", err)
	}
	if !p.Manifest().Enabled {
		t.Fatal("expected Enabled=true via PascalCase key")
	}
}
