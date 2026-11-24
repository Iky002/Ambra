package tmdb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTMDBSearchCandidatesMovieAndSeries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/movie":
			if r.URL.Query().Get("query") == "fail" {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			if r.URL.Query().Get("year") != "2021" {
				t.Errorf("expected year=2021, got %q", r.URL.Query().Get("year"))
			}
			results := make([]map[string]any, 0, 12)
			for i := 1; i <= 12; i++ {
				results = append(results, map[string]any{
					"id":             1000 + i,
					"title":          fmt.Sprintf("沙丘 %d", i),
					"original_title": fmt.Sprintf("Dune %d", i),
					"overview":       fmt.Sprintf("简介 %d", i),
					"release_date":   "2021-10-22",
					"vote_average":   8.1,
					"poster_path":    fmt.Sprintf("/p%d.jpg", i),
					"backdrop_path":  fmt.Sprintf("/b%d.jpg", i),
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"results": results})
		case "/search/tv":
			if r.URL.Query().Get("query") == "fail" {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			if r.URL.Query().Get("first_air_date_year") != "2008" {
				t.Errorf("expected first_air_date_year=2008, got %q", r.URL.Query().Get("first_air_date_year"))
			}
			results := make([]map[string]any, 0, 11)
			for i := 1; i <= 11; i++ {
				results = append(results, map[string]any{
					"id":             2000 + i,
					"name":           fmt.Sprintf("绝命毒师 %d", i),
					"original_name":  fmt.Sprintf("Breaking Bad %d", i),
					"overview":       fmt.Sprintf("剧集简介 %d", i),
					"first_air_date": "2008-01-20",
					"vote_average":   8.9,
					"poster_path":    fmt.Sprintf("/tvp%d.jpg", i),
					"backdrop_path":  fmt.Sprintf("/tvb%d.jpg", i),
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"results": results})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	// Empty key / empty query
	emptyProv := New("", "zh-CN")
	if res, err := emptyProv.SearchCandidates(context.Background(), "Movie", "Dune", 2021, ""); err != nil || res != nil {
		t.Fatalf("expected nil, nil for empty apiKey, got %v, %v", res, err)
	}

	p := New("test-key", "zh-CN")
	_ = p.Configure(map[string]any{"base_url": srv.URL})

	if res, err := p.SearchCandidates(context.Background(), "Movie", "   ", 0, ""); err != nil || res != nil {
		t.Fatalf("expected nil, nil for empty query, got %v, %v", res, err)
	}

	// Movie candidates (capped at 10)
	movies, err := p.SearchCandidates(context.Background(), "Movie", "Dune", 2021, "")
	if err != nil {
		t.Fatalf("SearchCandidates movie: %v", err)
	}
	if len(movies) != 10 {
		t.Fatalf("expected 10 movie candidates, got %d", len(movies))
	}
	if movies[0].Name != "沙丘 1" || movies[0].OriginalTitle != "Dune 1" || movies[0].Year != 2021 {
		t.Fatalf("unexpected first movie candidate: %+v", movies[0])
	}
	if movies[0].ProviderIDs["tmdb"] != "1001" || movies[0].CommunityRating != 8.1 {
		t.Fatalf("unexpected movie provider/rating: %+v", movies[0])
	}
	if !strings.HasSuffix(movies[0].PosterURL, "/p1.jpg") || !strings.HasSuffix(movies[0].BackdropURL, "/b1.jpg") {
		t.Fatalf("unexpected movie artwork URLs: poster=%q backdrop=%q", movies[0].PosterURL, movies[0].BackdropURL)
	}

	// Series candidates (capped at 10)
	series, err := p.SearchCandidates(context.Background(), "Series", "Breaking Bad", 2008, "zh-CN")
	if err != nil {
		t.Fatalf("SearchCandidates series: %v", err)
	}
	if len(series) != 10 {
		t.Fatalf("expected 10 series candidates, got %d", len(series))
	}
	if series[0].Name != "绝命毒师 1" || series[0].OriginalTitle != "Breaking Bad 1" || series[0].Year != 2008 {
		t.Fatalf("unexpected first series candidate: %+v", series[0])
	}
	if series[0].ProviderIDs["tmdb"] != "2001" || series[0].CommunityRating != 8.9 {
		t.Fatalf("unexpected series provider/rating: %+v", series[0])
	}

	// Error cases
	if _, err := p.SearchCandidates(context.Background(), "Movie", "fail", 2021, ""); err == nil {
		t.Fatal("expected error for movie 500 response")
	}
	if _, err := p.SearchCandidates(context.Background(), "tv", "fail", 2008, ""); err == nil {
		t.Fatal("expected error for tv 502 response")
	}
}

func TestTMDBFetchImages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("include_image_language") != "zh,en,null" {
			t.Errorf("expected include_image_language=zh,en,null, got %q", r.URL.Query().Get("include_image_language"))
		}
		switch r.URL.Path {
		case "/movie/438631/images":
			_, _ = w.Write([]byte(`{
				"posters": [
					{"file_path": "/poster_zh.jpg", "width": 1000, "height": 1500, "iso_639_1": "zh", "vote_average": 5.5, "vote_count": 20},
					{"file_path": "", "width": 0, "height": 0}
				],
				"backdrops": [
					{"file_path": "/bd_en.jpg", "width": 1920, "height": 1080, "iso_639_1": "en", "vote_average": 6.2, "vote_count": 15},
					{"file_path": ""}
				]
			}`))
		case "/tv/1396/images":
			_, _ = w.Write([]byte(`{
				"posters": [
					{"file_path": "/tv_poster.jpg", "width": 800, "height": 1200, "iso_639_1": "en", "vote_average": 7.1, "vote_count": 9}
				],
				"backdrops": []
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	// Empty key / empty tmdbID
	emptyProv := New("", "zh-CN")
	if imgs, err := emptyProv.FetchImages(context.Background(), "Movie", "438631"); err != nil || imgs != nil {
		t.Fatalf("expected nil, nil for empty apiKey, got %v, %v", imgs, err)
	}

	p := New("test-key", "zh-CN")
	_ = p.Configure(map[string]any{"base_url": srv.URL})

	if imgs, err := p.FetchImages(context.Background(), "Movie", ""); err != nil || imgs != nil {
		t.Fatalf("expected nil, nil for empty tmdbID, got %v, %v", imgs, err)
	}

	// Default imageBaseURL -> w342 for poster thumb, w500 for backdrop thumb
	imgs, err := p.FetchImages(context.Background(), "Movie", "438631")
	if err != nil {
		t.Fatalf("FetchImages movie: %v", err)
	}
	if len(imgs) != 2 {
		t.Fatalf("expected 2 images (empty paths skipped), got %d: %+v", len(imgs), imgs)
	}
	if imgs[0].Type != "Primary" || imgs[0].ProviderName != "TheMovieDb" ||
		imgs[0].Url != defaultImageBaseURL+"/poster_zh.jpg" ||
		imgs[0].ThumbnailUrl != defaultPosterThumbBaseURL+"/poster_zh.jpg" ||
		imgs[0].Width != 1000 || imgs[0].Height != 1500 || imgs[0].Language != "zh" || imgs[0].VoteCount != 20 {
		t.Fatalf("unexpected primary image: %+v", imgs[0])
	}
	if imgs[1].Type != "Backdrop" || imgs[1].ProviderName != "TheMovieDb" ||
		imgs[1].Url != defaultImageBaseURL+"/bd_en.jpg" ||
		imgs[1].ThumbnailUrl != defaultBackdropThumbURL+"/bd_en.jpg" ||
		imgs[1].Width != 1920 || imgs[1].Height != 1080 || imgs[1].Language != "en" {
		t.Fatalf("unexpected backdrop image: %+v", imgs[1])
	}

	// Series with custom image_base_url
	_ = p.Configure(map[string]any{"image_base_url": "https://img.local/tmdb"})
	tvImgs, err := p.FetchImages(context.Background(), "Series", "1396")
	if err != nil {
		t.Fatalf("FetchImages series: %v", err)
	}
	if len(tvImgs) != 1 || tvImgs[0].Url != "https://img.local/tmdb/tv_poster.jpg" || tvImgs[0].ThumbnailUrl != "https://img.local/tmdb/tv_poster.jpg" {
		t.Fatalf("unexpected series image: %+v", tvImgs)
	}

	// 404 error
	if _, err := p.FetchImages(context.Background(), "Movie", "999999"); err == nil {
		t.Fatal("expected error for 404 images endpoint")
	}
}
