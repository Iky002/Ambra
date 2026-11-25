package tmdb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ambra-media/ambra/internal/plugins"
)

func TestFetchMovie_MultiFallbackAndYearMatching(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/search/movie":
			q := r.URL.Query().Get("query")
			y := r.URL.Query().Get("year")
			if q == "海上钢琴师" && y == "1998" {
				// Simulate Chinese title + year miss to verify fallback to OriginalTitle + Year
				_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
				return
			}
			if q == "The Legend of 1900" && y == "1998" {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"results": []map[string]any{
						{"id": 99999, "release_date": "2010-01-01"},
						{"id": 10376, "release_date": "1998-10-28"},
					},
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
		case "/movie/10376":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":             10376,
				"title":          "海上钢琴师",
				"original_title": "La leggenda del pianista sull'oceano",
				"release_date":   "1998-10-28",
				"poster_path":    "/legend1900.jpg",
			})
		case "/find/tt1856101":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"movie_results": []map[string]any{{"id": 335984}},
			})
		case "/movie/335984":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":             335984,
				"title":          "银翼杀手2049",
				"original_title": "Blade Runner 2049",
				"release_date":   "2017-10-04",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := New("test-key", "zh-CN")
	p.baseURL = srv.URL

	// 1. Falls back to OriginalTitle + exact year candidate matching
	res, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{
		ItemType:      "Movie",
		Title:         "海上钢琴师",
		OriginalTitle: "The Legend of 1900",
		Year:          1998,
	})
	if err != nil || res == nil {
		t.Fatalf("FetchMetadata fallback failed: res=%+v err=%v", res, err)
	}
	if res.Name != "海上钢琴师" || res.ProviderIDs["tmdb"] != "10376" || res.Year != 1998 {
		t.Fatalf("unexpected fallback result: %+v", res)
	}

	// 2. Resolves directly via IMDb external ID (/find/{imdbID})
	res2, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{
		ItemType:    "Movie",
		Title:       "Blade Runner 2049",
		ProviderIDs: map[string]string{"imdb": "tt1856101"},
	})
	if err != nil || res2 == nil {
		t.Fatalf("FetchMetadata by IMDb failed: res=%+v err=%v", res2, err)
	}
	if res2.Name != "银翼杀手2049" || res2.ProviderIDs["tmdb"] != "335984" {
		t.Fatalf("unexpected IMDb lookup result: %+v", res2)
	}
}
