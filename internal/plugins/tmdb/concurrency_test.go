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

func TestProvider_ConcurrentConfigureAndFetch_NoRace(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/movie/") && strings.HasSuffix(r.URL.Path, "/images"):
			_, _ = w.Write([]byte(`{"posters":[{"file_path":"/p.jpg","width":500,"height":750}],"backdrops":[]}`))
		case strings.HasPrefix(r.URL.Path, "/movie/"):
			_, _ = w.Write([]byte(`{"id":100,"title":"Test Movie","original_title":"Test Movie","release_date":"2020-01-01","poster_path":"/p.jpg"}`))
		case strings.HasPrefix(r.URL.Path, "/tv/"):
			_, _ = w.Write([]byte(`{"id":200,"name":"Test Show","original_name":"Test Show","first_air_date":"2021-01-01","poster_path":"/s.jpg"}`))
		case strings.HasPrefix(r.URL.Path, "/search/movie"):
			_, _ = w.Write([]byte(`{"results":[{"id":100,"title":"Test Movie","release_date":"2020-01-01"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := New("key-initial", "zh-CN")
	p.SetHTTPClient(srv.Client())
	_ = p.Configure(map[string]any{
		"base_url":       srv.URL,
		"image_base_url": "https://img.example/original",
		"enabled":        true,
	})

	ctx := context.Background()
	var wg sync.WaitGroup

	// Concurrent writers calling Configure
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 25; j++ {
				lang := "zh-CN"
				if (n+j)%2 == 0 {
					lang = "en-US"
				}
				_ = p.Configure(map[string]any{
					"api_key":        "key-updated",
					"language":       lang,
					"base_url":       srv.URL,
					"image_base_url": "https://img.example/original",
					"enabled":        j%2 == 0,
				})
			}
		}(i)
	}

	// Concurrent readers calling FetchMetadata, SearchCandidates, FetchImages, Manifest
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				_ = p.Manifest()
				switch (n + j) % 4 {
				case 0:
					_, _ = p.FetchMetadata(ctx, plugins.MediaQuery{
						ItemType:    "Movie",
						ProviderIDs: map[string]string{"tmdb": "100"},
					})
				case 1:
					_, _ = p.FetchMetadata(ctx, plugins.MediaQuery{
						ItemType:    "Series",
						ProviderIDs: map[string]string{"tmdb": "200"},
					})
				case 2:
					_, _ = p.SearchCandidates(ctx, "Movie", "Test", 2020, "")
				case 3:
					_, _ = p.FetchImages(ctx, "Movie", "100")
				}
			}
		}(i)
	}

	wg.Wait()
}

func TestProvider_PathEscapesTMDBID(t *testing.T) {
	var (
		mu       sync.Mutex
		rawPaths []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		rawPaths = append(rawPaths, r.URL.EscapedPath())
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":1,"title":"Safe","name":"Safe"}`))
	}))
	defer srv.Close()

	p := New("test-key", "en-US")
	p.SetHTTPClient(srv.Client())
	_ = p.Configure(map[string]any{"base_url": srv.URL})

	ctx := context.Background()
	_, err := p.FetchMetadata(ctx, plugins.MediaQuery{
		ItemType:    "Movie",
		ProviderIDs: map[string]string{"tmdb": "123/../evil?x=1"},
	})
	if err != nil {
		t.Fatalf("FetchMetadata movie: %v", err)
	}
	_, err = p.FetchMetadata(ctx, plugins.MediaQuery{
		ItemType:    "Series",
		ProviderIDs: map[string]string{"tmdb": "456/../../secret"},
	})
	if err != nil {
		t.Fatalf("FetchMetadata series: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(rawPaths) != 2 {
		t.Fatalf("expected 2 requests, got %d: %v", len(rawPaths), rawPaths)
	}
	if rawPaths[0] != "/movie/123%2F..%2Fevil%3Fx=1" {
		t.Errorf("movie path = %q, want escaped path", rawPaths[0])
	}
	if rawPaths[1] != "/tv/456%2F..%2F..%2Fsecret" {
		t.Errorf("series path = %q, want escaped path", rawPaths[1])
	}
}
