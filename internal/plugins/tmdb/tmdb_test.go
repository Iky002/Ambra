package tmdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/plugins"
)

func TestTMDBSkipWithoutKey(t *testing.T) {
	p := New("", "")
	if p.language != "zh-CN" {
		t.Fatalf("expected default language zh-CN, got %q", p.language)
	}
	m := p.Manifest()
	if m.ID != "ambra.builtin.tmdb" || !m.Enabled {
		t.Fatalf("unexpected manifest: %+v", m)
	}
	res, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{
		ItemType: "Movie",
		Title:    "Dune",
	})
	if err != nil || res != nil {
		t.Fatalf("expected nil, nil when apiKey is empty; got res=%v err=%v", res, err)
	}
}

func TestTMDBFetchMovieAndPersonAvatars(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/movie":
			if r.URL.Query().Get("query") == "NotFound" {
				_, _ = w.Write([]byte(`{"results":[]}`))
				return
			}
			_, _ = w.Write([]byte(`{"results":[{"id":438631}]}`))
		case "/movie/438631":
			_, _ = w.Write([]byte(`{
				"id": 438631,
				"title": "沙丘",
				"original_title": "Dune",
				"overview": "保罗·厄崔迪的传奇旅程",
				"tagline": "恐惧是思维的杀手",
				"release_date": "2021-10-22",
				"vote_average": 7.8,
				"poster_path": "/poster.jpg",
				"backdrop_path": "/backdrop.jpg",
				"genres": [{"name": "科幻"}],
				"production_companies": [{"name": "Legendary Pictures"}],
				"credits": {
					"crew": [
						{"name": "丹尼斯·维伦纽瓦", "job": "Director", "profile_path": "/denis.jpg"},
						{"name": "乔·斯派茨", "job": "Screenplay", "profile_path": "/jon.jpg"}
					],
					"cast": [
						{"name": "提莫西·查拉梅", "character": "Paul", "profile_path": "/timothee.jpg"},
						{"name": "无头像演员", "character": "Extra", "profile_path": ""}
					]
				},
				"external_ids": {"imdb_id": "tt1160419"}
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	// Default imageBaseURL -> w185 for profile images
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
	if res.PosterURL == "" || res.BackdropURL == "" || len(res.Studios) != 1 {
		t.Fatalf("expected poster/backdrop/studios: %+v", res)
	}
	if len(res.People) != 4 {
		t.Fatalf("expected 4 people, got %d", len(res.People))
	}
	if res.People[0].ImageURL != "https://image.tmdb.org/t/p/w185/denis.jpg" {
		t.Fatalf("expected w185 profile URL for director, got %q", res.People[0].ImageURL)
	}
	if res.People[1].Type != "Writer" || res.People[1].ImageURL != "https://image.tmdb.org/t/p/w185/jon.jpg" {
		t.Fatalf("expected Writer with w185 profile URL, got %+v", res.People[1])
	}
	if res.People[2].ImageURL != "https://image.tmdb.org/t/p/w185/timothee.jpg" {
		t.Fatalf("expected w185 profile URL for actor, got %q", res.People[2].ImageURL)
	}
	if res.People[3].ImageURL != "" {
		t.Fatalf("expected empty profile URL when profile_path is empty, got %q", res.People[3].ImageURL)
	}

	// Custom imageBaseURL + case-insensitive ProviderIDs ("Tmdb")
	_ = p.Configure(map[string]any{
		"api_key":        "test-key",
		"language":       "en-US",
		"image_base_url": "http://img.local/t/p/",
		"enabled":        false,
	})
	if p.Manifest().Enabled {
		t.Fatalf("expected enabled=false after Configure")
	}
	res2, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{
		ItemType:    "Movie",
		ProviderIDs: map[string]string{"Tmdb": "438631"},
	})
	if err != nil || res2 == nil {
		t.Fatalf("FetchMetadata by Tmdb ID failed: res=%v err=%v", res2, err)
	}
	if res2.People[0].ImageURL != "http://img.local/t/p/denis.jpg" {
		t.Fatalf("expected custom image_base_url for profile, got %q", res2.People[0].ImageURL)
	}

	// Empty title / not found
	if r, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{ItemType: "Movie"}); err != nil || r != nil {
		t.Fatalf("expected nil for empty title, got r=%v err=%v", r, err)
	}
	if r, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{ItemType: "Movie", Title: "NotFound"}); err != nil || r != nil {
		t.Fatalf("expected nil for empty search results, got r=%v err=%v", r, err)
	}
}

func TestTMDBFetchSeriesSeasonAndEpisode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/tv":
			if r.URL.Query().Get("query") == "NotFound" {
				_, _ = w.Write([]byte(`{"results":[]}`))
				return
			}
			_, _ = w.Write([]byte(`{"results":[{"id":1396}]}`))
		case "/tv/1396":
			_, _ = w.Write([]byte(`{
				"id": 1396,
				"name": "绝命毒师",
				"original_name": "Breaking Bad",
				"overview": "高中化学老师制毒故事",
				"tagline": "All Hail the King",
				"first_air_date": "2008-01-20",
				"vote_average": 8.9,
				"poster_path": "/bb_poster.jpg",
				"backdrop_path": "/bb_backdrop.jpg",
				"genres": [{"name": "剧情"}],
				"networks": [{"name": "AMC"}],
				"credits": {
					"crew": [
						{"name": "Vince Gilligan", "job": "Director", "profile_path": "/vince.jpg"},
						{"name": "Peter Gould", "job": "Writer", "profile_path": "/peter.jpg"}
					],
					"cast": [
						{"name": "Bryan Cranston", "character": "Walter White", "profile_path": "/bryan.jpg"}
					]
				},
				"external_ids": {"imdb_id": "tt0903747", "tvdb_id": 81189}
			}`))
		case "/tv/1396/season/1":
			_, _ = w.Write([]byte(`{
				"name": "第 1 季",
				"overview": "第一季简介",
				"air_date": "2008-01-20",
				"poster_path": "/bb_s01.jpg"
			}`))
		case "/tv/1396/season/1/episode/1":
			_, _ = w.Write([]byte(`{
				"name": "试播集",
				"overview": "沃尔特确诊肺癌。",
				"air_date": "2008-01-20",
				"vote_average": 8.3,
				"still_path": "/bb_s01e01.jpg"
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	p := New("test-key", "zh-CN")
	_ = p.Configure(map[string]any{"base_url": srv.URL})

	// Series
	series, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{
		ItemType: "Series",
		Title:    "Breaking Bad",
		Year:     2008,
	})
	if err != nil || series == nil {
		t.Fatalf("fetchSeries failed: res=%v err=%v", series, err)
	}
	if series.Name != "绝命毒师" || series.Year != 2008 || series.ProviderIDs["tmdb"] != "1396" ||
		series.ProviderIDs["imdb"] != "tt0903747" || series.ProviderIDs["tvdb"] != "81189" {
		t.Fatalf("unexpected series result: %+v", series)
	}
	if len(series.People) != 3 {
		t.Fatalf("expected 3 series people (director, writer, actor), got %+v", series.People)
	}
	if series.People[0].Type != "Director" || !strings.HasSuffix(series.People[0].ImageURL, "/vince.jpg") {
		t.Fatalf("unexpected director: %+v", series.People[0])
	}
	if series.People[1].Type != "Writer" || !strings.HasSuffix(series.People[1].ImageURL, "/peter.jpg") {
		t.Fatalf("unexpected writer: %+v", series.People[1])
	}
	if series.People[2].Type != "Actor" || !strings.HasSuffix(series.People[2].ImageURL, "/bryan.jpg") {
		t.Fatalf("unexpected actor: %+v", series.People[2])
	}

	// Series edge cases
	if r, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{ItemType: "Series"}); err != nil || r != nil {
		t.Fatalf("expected nil for empty series title")
	}
	if r, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{ItemType: "Series", Title: "NotFound"}); err != nil || r != nil {
		t.Fatalf("expected nil for not found series")
	}

	// Season with mixed-case "Tmdb" key
	season, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{
		ItemType:     "Season",
		SeasonNumber: 1,
		ProviderIDs:  map[string]string{"Tmdb": "1396"},
	})
	if err != nil || season == nil {
		t.Fatalf("fetchSeason failed: res=%v err=%v", season, err)
	}
	if season.Name != "第 1 季" || season.Overview != "第一季简介" || season.PremiereDate != "2008-01-20" || season.Year != 2008 {
		t.Fatalf("unexpected season result: %+v", season)
	}
	if !strings.HasSuffix(season.PosterURL, "/bb_s01.jpg") {
		t.Fatalf("expected season poster URL, got %q", season.PosterURL)
	}

	// Season missing TMDB ID / negative season / 404
	if r, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{ItemType: "Season", SeasonNumber: 1}); err != nil || r != nil {
		t.Fatalf("expected nil when season tmdbID missing")
	}
	if _, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{
		ItemType:     "Season",
		SeasonNumber: 99,
		ProviderIDs:  map[string]string{"tmdb": "1396"},
	}); err == nil {
		t.Fatalf("expected 404 error for nonexistent season")
	}

	// Episode with mixed-case "Tmdb" key
	ep, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{
		ItemType:      "Episode",
		SeasonNumber:  1,
		EpisodeNumber: 1,
		ProviderIDs:   map[string]string{"Tmdb": "1396"},
	})
	if err != nil || ep == nil {
		t.Fatalf("fetchEpisode failed: res=%v err=%v", ep, err)
	}
	if ep.Name != "试播集" || ep.Overview != "沃尔特确诊肺癌。" || !strings.HasSuffix(ep.PosterURL, "/bb_s01e01.jpg") {
		t.Fatalf("unexpected episode result: %+v", ep)
	}

	// Episode edge cases
	if r, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{ItemType: "Episode", SeasonNumber: 1, EpisodeNumber: 0, ProviderIDs: map[string]string{"tmdb": "1396"}}); err != nil || r != nil {
		t.Fatalf("expected nil for episodeNumber <= 0")
	}
	if _, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{ItemType: "Episode", SeasonNumber: 1, EpisodeNumber: 99, ProviderIDs: map[string]string{"tmdb": "1396"}}); err == nil {
		t.Fatalf("expected 404 error for nonexistent episode")
	}
}
