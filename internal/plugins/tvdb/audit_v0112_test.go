package tvdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/plugins/tmdb"
)

func TestAuditV0112_TMDBAndTVDBSatisfyImageProviderInRegistry(t *testing.T) {
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			_, _ = w.Write([]byte(`{"status":"success","data":{"token":"tvdb-tok-1"}}`))
		case "/series/81189/extended":
			_, _ = w.Write([]byte(`{
				"status":"success",
				"data":{
					"id":81189,
					"image":"https://artworks.thetvdb.com/fallback.jpg",
					"artworks":[
						{"image":"https://artworks.thetvdb.com/poster.jpg","thumbnail":"https://artworks.thetvdb.com/poster_t.jpg","type":2,"score":9.5,"language":"eng","width":680,"height":1000},
						{"image":"https://artworks.thetvdb.com/fanart.jpg","type":3,"score":9.1,"language":"eng","width":1920,"height":1080},
						{"image":"https://artworks.thetvdb.com/logo.png","type":23,"score":8.8,"language":"eng","width":800,"height":310}
					]
				}
			}`))
		case "/movies/500/extended":
			_, _ = w.Write([]byte(`{
				"status":"success",
				"data":{
					"id":500,
					"image":"https://artworks.thetvdb.com/movie_fallback.jpg",
					"artworks":[]
				}
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	tvdbProv := New()
	tvdbProv.SetHTTPClient(srv.Client())
	if err := tvdbProv.Configure(map[string]any{
		"enabled":  true,
		"api_key":  "tvdb-key",
		"base_url": srv.URL,
	}); err != nil {
		t.Fatal(err)
	}

	tmdbProv := tmdb.New("tmdb-key", "en-US")

	reg := plugins.NewRegistry()
	reg.Register(tmdbProv)
	reg.Register(tvdbProv)

	imgProvs := reg.ImageProviders()
	if len(imgProvs) != 2 {
		t.Fatalf("expected 2 ImageProviders (TMDB + TVDB) in registry.ImageProviders(), got %d", len(imgProvs))
	}

	seriesHits, err := tvdbProv.FetchImages(ctx, plugins.MediaQuery{
		ItemType:    "Series",
		ProviderIDs: map[string]string{"Tvdb": "81189"},
	})
	if err != nil || len(seriesHits) != 3 {
		t.Fatalf("expected 3 TVDB series artwork hits, got (%+v, %v)", seriesHits, err)
	}
	if seriesHits[0].Type != "Primary" || seriesHits[1].Type != "Backdrop" || seriesHits[2].Type != "Logo" {
		t.Fatalf("unexpected mapped artwork types: %+v", seriesHits)
	}

	movieHits, err := tvdbProv.FetchImages(ctx, plugins.MediaQuery{
		ItemType:    "Movie",
		ProviderIDs: map[string]string{"Tvdb": "500"},
	})
	if err != nil || len(movieHits) != 1 || movieHits[0].URL != "https://artworks.thetvdb.com/movie_fallback.jpg" {
		t.Fatalf("expected fallback primary image for movie without artworks list, got (%+v, %v)", movieHits, err)
	}
}
