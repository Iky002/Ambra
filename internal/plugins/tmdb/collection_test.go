package tmdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ambra-media/ambra/internal/plugins"
)

func TestFetchMovieBelongsToCollection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/movie/11":
			_, _ = w.Write([]byte(`{
				"id": 11,
				"title": "Star Wars",
				"release_date": "1977-05-25",
				"belongs_to_collection": {
					"id": 10,
					"name": "Star Wars Collection",
					"poster_path": "/col_poster.jpg",
					"backdrop_path": "/col_backdrop.jpg"
				}
			}`))
		case "/movie/27205":
			_, _ = w.Write([]byte(`{
				"id": 27205,
				"title": "Inception",
				"release_date": "2010-07-16",
				"belongs_to_collection": null
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := New("test-key", "zh-CN")
	p.SetHTTPClient(srv.Client())
	_ = p.Configure(map[string]any{
		"base_url":       srv.URL,
		"image_base_url": "https://img.example.com",
	})

	res, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{
		ItemType:    "Movie",
		ProviderIDs: map[string]string{"tmdb": "11"},
	})
	if err != nil || res == nil {
		t.Fatalf("FetchMetadata(11): res=%+v err=%v", res, err)
	}
	if res.Collection == nil {
		t.Fatalf("expected Collection to be populated")
	}
	if res.Collection.ID != "10" || res.Collection.Name != "Star Wars Collection" {
		t.Fatalf("unexpected Collection: %+v", res.Collection)
	}
	if res.Collection.PosterURL != "https://img.example.com/col_poster.jpg" ||
		res.Collection.BackdropURL != "https://img.example.com/col_backdrop.jpg" {
		t.Fatalf("unexpected Collection URLs: %+v", res.Collection)
	}

	res2, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{
		ItemType:    "Movie",
		ProviderIDs: map[string]string{"tmdb": "27205"},
	})
	if err != nil || res2 == nil {
		t.Fatalf("FetchMetadata(27205): res=%+v err=%v", res2, err)
	}
	if res2.Collection != nil {
		t.Fatalf("expected nil Collection for standalone movie, got %+v", res2.Collection)
	}
}
