package tmdb

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/plugins"
)

func TestTMDBManifestConfigureAndDefaults(t *testing.T) {
	p := New("  key1  ", "")
	if p.language != "zh-CN" || p.apiKey != "key1" {
		t.Fatalf("unexpected defaults: lang=%s key=%s", p.language, p.apiKey)
	}
	m := p.Manifest()
	if m.ID != "ambra.builtin.tmdb" || !m.Enabled || !m.BuiltIn {
		t.Fatalf("unexpected manifest: %+v", m)
	}
	if err := p.Configure(map[string]any{
		"api_key":        " key2 ",
		"language":       "en-US",
		"base_url":       "http://localhost:9999/",
		"image_base_url": "http://img.local/",
		"enabled":        false,
	}); err != nil {
		t.Fatalf("Configure failed: %v", err)
	}
	if p.apiKey != "key2" || p.language != "en-US" || p.baseURL != "http://localhost:9999" || p.imageBaseURL != "http://img.local" || p.Manifest().Enabled {
		t.Fatalf("Configure did not apply all fields")
	}
}

func TestTMDBMovieExtraBranches(t *testing.T) {
	// Build >15 cast members to test truncation
	var castItems []string
	for i := 0; i < 18; i++ {
		castItems = append(castItems, fmt.Sprintf(`{"name":"Actor%d","character":"Role%d"}`, i, i))
	}
	movieDetail := fmt.Sprintf(`{
		"id": 100,
		"title": "MovieTitle",
		"original_title": "OrigTitle",
		"overview": "Plot",
		"tagline": "Tag",
		"release_date": "2020-07-01",
		"vote_average": 8.1,
		"poster_path": "/p.jpg",
		"backdrop_path": "/b.jpg",
		"genres": [{"name": "Action"}],
		"production_companies": [{"name": "Warner"}],
		"credits": {
			"crew": [
				{"name": "Dir1", "job": "Director"},
				{"name": "Wri1", "job": "Writer"},
				{"name": "Scr1", "job": "Screenplay"}
			],
			"cast": [%s]
		},
		"external_ids": {"imdb_id": "tt100"}
	}`, strings.Join(castItems, ","))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/movie/100":
			_, _ = w.Write([]byte(movieDetail))
		case "/search/movie":
			if r.URL.Query().Get("query") == "empty" {
				_, _ = w.Write([]byte(`{"results":[]}`))
				return
			}
			if r.URL.Query().Get("query") == "fail" {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			_, _ = w.Write([]byte(`{"results":[{"id":404}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	p := New("k", "zh-CN")
	p.SetHTTPClient(srv.Client())
	_ = p.Configure(map[string]any{"base_url": srv.URL, "api_key": "k"})
	ctx := context.Background()

	// Direct fetch by ProviderIDs + explicit language
	res, err := p.FetchMetadata(ctx, plugins.MediaQuery{
		ItemType:    "Movie",
		Language:    "en-US",
		ProviderIDs: map[string]string{"tmdb": "100"},
	})
	if err != nil || res == nil {
		t.Fatalf("expected movie result, got res=%v err=%v", res, err)
	}
	if len(res.Studios) != 1 || res.Studios[0] != "Warner" {
		t.Fatalf("unexpected studios: %v", res.Studios)
	}
	// 3 crew + 15 capped cast = 18
	if len(res.People) != 18 {
		t.Fatalf("expected 18 people (3 crew + 15 cast), got %d", len(res.People))
	}

	// Empty title and no ProviderID -> nil, nil
	if res, err := p.FetchMetadata(ctx, plugins.MediaQuery{ItemType: "Movie"}); err != nil || res != nil {
		t.Fatalf("expected nil,nil for empty movie query")
	}

	// Search returns empty results -> nil, nil
	if res, err := p.FetchMetadata(ctx, plugins.MediaQuery{ItemType: "Movie", Title: "empty"}); err != nil || res != nil {
		t.Fatalf("expected nil,nil for empty search results")
	}

	// Search returns 500 -> error
	if _, err := p.FetchMetadata(ctx, plugins.MediaQuery{ItemType: "Movie", Title: "fail"}); err == nil {
		t.Fatalf("expected error on 500 search")
	}

	// Detail returns 404 -> error
	if _, err := p.FetchMetadata(ctx, plugins.MediaQuery{ItemType: "Movie", Title: "missingdetail"}); err == nil {
		t.Fatalf("expected error on 404 detail")
	}
}

func TestTMDBSeriesAndEpisode(t *testing.T) {
	var castItems []string
	for i := 0; i < 17; i++ {
		castItems = append(castItems, fmt.Sprintf(`{"name":"TVActor%d","character":"Char%d"}`, i, i))
	}
	tvDetail := fmt.Sprintf(`{
		"id": 200,
		"name": "三体",
		"original_name": "Three-Body",
		"overview": "Sci-Fi",
		"tagline": "Do not answer",
		"first_air_date": "2023-01-15",
		"vote_average": 8.7,
		"poster_path": "/tvp.jpg",
		"backdrop_path": "/tvb.jpg",
		"genres": [{"name": "科幻"}],
		"networks": [{"name": "CCTV"}],
		"credits": {"cast": [%s]},
		"external_ids": {"imdb_id": "tt200", "tvdb_id": 555}
	}`, strings.Join(castItems, ","))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/tv":
			switch r.URL.Query().Get("query") {
			case "empty":
				_, _ = w.Write([]byte(`{"results":[]}`))
			case "fail":
				w.WriteHeader(http.StatusBadGateway)
			case "bad_detail":
				_, _ = w.Write([]byte(`{"results":[{"id":999}]}`))
			default:
				_, _ = w.Write([]byte(`{"results":[{"id":200}]}`))
			}
		case "/tv/200":
			_, _ = w.Write([]byte(tvDetail))
		case "/tv/200/season/1/episode/2":
			_, _ = w.Write([]byte(`{
				"name": "第二集",
				"overview": "剧情",
				"air_date": "2023-01-16",
				"vote_average": 8.4,
				"still_path": "/still.jpg"
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	p := New("k", "zh-CN")
	p.SetHTTPClient(srv.Client())
	_ = p.Configure(map[string]any{"base_url": srv.URL, "api_key": "k"})
	ctx := context.Background()

	// Series via search + year
	res, err := p.FetchMetadata(ctx, plugins.MediaQuery{ItemType: "Series", Title: "三体", Year: 2023})
	if err != nil || res == nil {
		t.Fatalf("FetchMetadata series failed: res=%v err=%v", res, err)
	}
	if res.Name != "三体" || res.Year != 2023 || res.ProviderIDs["tvdb"] != "555" || res.ProviderIDs["imdb"] != "tt200" {
		t.Fatalf("unexpected series result: %+v", res)
	}
	if len(res.Studios) != 1 || len(res.People) != 15 || res.PosterURL == "" || res.BackdropURL == "" {
		t.Fatalf("unexpected series details: studios=%v people=%d", res.Studios, len(res.People))
	}

	// Series via ProviderIDs
	res2, err := p.FetchMetadata(ctx, plugins.MediaQuery{ItemType: "Series", ProviderIDs: map[string]string{"tmdb": "200"}})
	if err != nil || res2 == nil || res2.Name != "三体" {
		t.Fatalf("FetchMetadata series by id failed: res=%v err=%v", res2, err)
	}

	// Series edge cases: empty title, empty search, search error, detail error
	if r, err := p.FetchMetadata(ctx, plugins.MediaQuery{ItemType: "Series"}); err != nil || r != nil {
		t.Fatalf("expected nil,nil for empty series title")
	}
	if r, err := p.FetchMetadata(ctx, plugins.MediaQuery{ItemType: "Series", Title: "empty"}); err != nil || r != nil {
		t.Fatalf("expected nil,nil for empty series search")
	}
	if _, err := p.FetchMetadata(ctx, plugins.MediaQuery{ItemType: "Series", Title: "fail"}); err == nil {
		t.Fatalf("expected error for series search 502")
	}
	if _, err := p.FetchMetadata(ctx, plugins.MediaQuery{ItemType: "Series", Title: "bad_detail"}); err == nil {
		t.Fatalf("expected error for series detail 404")
	}

	// Episode: missing provider IDs / invalid episode number
	if r, err := p.FetchMetadata(ctx, plugins.MediaQuery{ItemType: "Episode"}); err != nil || r != nil {
		t.Fatalf("expected nil,nil for episode without provider IDs")
	}
	if r, err := p.FetchMetadata(ctx, plugins.MediaQuery{ItemType: "Episode", ProviderIDs: map[string]string{"tmdb": "200"}, EpisodeNumber: 0}); err != nil || r != nil {
		t.Fatalf("expected nil,nil for episode 0")
	}

	// Episode success
	ep, err := p.FetchMetadata(ctx, plugins.MediaQuery{
		ItemType:      "Episode",
		ProviderIDs:   map[string]string{"tmdb": "200"},
		SeasonNumber:  1,
		EpisodeNumber: 2,
	})
	if err != nil || ep == nil || ep.Name != "第二集" || ep.PosterURL == "" {
		t.Fatalf("unexpected episode result: ep=%+v err=%v", ep, err)
	}

	// Episode error (404)
	if _, err := p.FetchMetadata(ctx, plugins.MediaQuery{
		ItemType:      "Episode",
		ProviderIDs:   map[string]string{"tmdb": "200"},
		SeasonNumber:  1,
		EpisodeNumber: 99,
	}); err == nil {
		t.Fatalf("expected error for missing episode")
	}

	// getJSON invalid URL and network error
	pBad := New("k", "zh-CN")
	_ = pBad.Configure(map[string]any{"base_url": "http://[::1]:namedport", "api_key": "k"})
	if _, err := pBad.FetchMetadata(ctx, plugins.MediaQuery{ItemType: "Movie", ProviderIDs: map[string]string{"tmdb": "1"}}); err == nil {
		t.Fatalf("expected error for invalid base URL")
	}

	srvClosed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srvClosed.Close()
	pBad.SetHTTPClient(srvClosed.Client())
	_ = pBad.Configure(map[string]any{"base_url": srvClosed.URL, "api_key": "k"})
	if _, err := pBad.FetchMetadata(ctx, plugins.MediaQuery{ItemType: "Movie", ProviderIDs: map[string]string{"tmdb": "1"}}); err == nil {
		t.Fatalf("expected error for closed server")
	}
}
