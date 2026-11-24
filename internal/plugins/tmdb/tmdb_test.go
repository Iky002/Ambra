package tmdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ambra-media/ambra/internal/plugins"
)

func TestTMDBSkipWithoutKey(t *testing.T) {
	p := New("", "zh-CN")
	res, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{
		ItemType: "Movie",
		Title:    "Dune",
	})
	if err != nil || res != nil {
		t.Fatalf("expected nil, nil when apiKey is empty; got res=%v err=%v", res, err)
	}
}

func TestTMDBFetchMovie(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/movie":
			_, _ = w.Write([]byte(`{"results":[{"id":438631}]}`))
		case "/movie/438631":
			_, _ = w.Write([]byte(`{
				"id": 438631,
				"title": "沙丘",
				"original_title": "Dune",
				"overview": "保罗·厄崔迪的传奇旅程",
				"release_date": "2021-10-22",
				"vote_average": 7.8,
				"poster_path": "/poster.jpg",
				"backdrop_path": "/backdrop.jpg",
				"genres": [{"name": "科幻"}],
				"credits": {"crew": [{"name": "丹尼斯·维伦纽瓦", "job": "Director"}], "cast": [{"name": "提莫西·查拉梅", "character": "Paul"}]},
				"external_ids": {"imdb_id": "tt1160419"}
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	p := New("test-key", "zh-CN")
	_ = p.Configure(map[string]any{"base_url": srv.URL})

	res, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{
		ItemType: "Movie",
		Title:    "Dune",
		Year:     2021,
	})
	if err != nil || res == nil {
		t.Fatalf("FetchMetadata failed: res=%v err=%v", res, err)
	}
	if res.Name != "沙丘" || res.Year != 2021 || res.ProviderIDs["tmdb"] != "438631" || res.ProviderIDs["imdb"] != "tt1160419" {
		t.Fatalf("unexpected result: %+v", res)
	}
	if res.PosterURL == "" || res.BackdropURL == "" {
		t.Fatalf("expected poster/backdrop URLs")
	}
}
