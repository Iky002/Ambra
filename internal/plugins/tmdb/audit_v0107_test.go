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
