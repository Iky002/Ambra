package tvdb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ambra-media/ambra/internal/plugins"
)

func TestDefaultDisabledManifestAndSkip(t *testing.T) {
	p := New()
	m := p.Manifest()
	if m.ID != "ambra.builtin.tvdb" {
		t.Fatalf("unexpected ID: %q", m.ID)
	}
	if m.Name != "TheTVDB Scraper" {
		t.Fatalf("unexpected Name: %q", m.Name)
	}
	if m.Version != "1.0.0" || !m.BuiltIn || m.Author != "Ambra" {
		t.Fatalf("unexpected manifest metadata: %+v", m)
	}
	if m.Enabled {
		t.Fatalf("expected Enabled=false by default")
	}
	if m.Priority != 30 {
		t.Fatalf("expected Priority=30, got %d", m.Priority)
	}
	if len(m.Kinds) != 2 || m.Kinds[0] != plugins.KindMetadata || m.Kinds[1] != plugins.KindImage {
		t.Fatalf("unexpected Kinds: %+v", m.Kinds)
	}

	// Disabled -> returns nil, nil
	res, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{
		ItemType: "Series",
		Title:    "Breaking Bad",
	})
	if err != nil || res != nil {
		t.Fatalf("expected nil, nil when disabled; got res=%v err=%v", res, err)
	}

	// Enabled without api_key -> returns nil, nil
	_ = p.Configure(map[string]any{"enabled": true})
	res, err = p.FetchMetadata(context.Background(), plugins.MediaQuery{
		ItemType: "Series",
		Title:    "Breaking Bad",
	})
	if err != nil || res != nil {
		t.Fatalf("expected nil, nil when api_key empty; got res=%v err=%v", res, err)
	}

	// Unsupported ItemType -> returns nil, nil
	_ = p.Configure(map[string]any{"api_key": "k"})
	res, err = p.FetchMetadata(context.Background(), plugins.MediaQuery{
		ItemType: "Season",
		Title:    "Breaking Bad",
	})
	if err != nil || res != nil {
		t.Fatalf("expected nil, nil for unsupported ItemType; got res=%v err=%v", res, err)
	}
}

func TestMapLanguage(t *testing.T) {
	cases := map[string]string{
		"":        "zho",
		"zh-CN":   "zho",
		"zh_CN":   "zho",
		"zh-SG":   "zho",
		"zh-Hans": "zho",
		"zh":      "zho",
		"zh-TW":   "zhtw",
		"zh-HK":   "zhtw",
		"zh-Hant": "zhtw",
		"zhtw":    "zhtw",
		"en":      "eng",
		"en-US":   "eng",
		"en-GB":   "eng",
		"ja":      "jpn",
		"ja-JP":   "jpn",
		"ko":      "kor",
		"ko-KR":   "kor",
		"es-ES":   "spa",
		"de-DE":   "deu",
		"fr-FR":   "fra",
		"pt-BR":   "por",
		"th-TH":   "tha",
		"ita":     "ita",
		"xx":      "eng",
	}
	for in, want := range cases {
		if got := MapLanguage(in); got != want {
			t.Errorf("MapLanguage(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestConfigureVariantsAndTokenInvalidation(t *testing.T) {
	var loginCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			n := loginCount.Add(1)
			var body struct {
				APIKey string `json:"apikey"`
				PIN    string `json:"pin"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data":   map[string]any{"token": body.APIKey + "-" + body.PIN + "-" + string(rune('0'+n))},
			})
		case "/series/81189/extended":
			if r.Header.Get("Authorization") == "" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"status":"success","data":{"id":81189,"name":"Breaking Bad"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	p := New()
	if err := p.Configure(map[string]any{
		"Enabled":  true,
		"ApiKey":   "key1",
		"Pin":      "pin1",
		"Language": "en-US",
		"Priority": float64(45),
		"base_url": srv.URL + "/",
	}); err != nil {
		t.Fatalf("Configure failed: %v", err)
	}
	if m := p.Manifest(); !m.Enabled || m.Priority != 45 {
		t.Fatalf("unexpected manifest after Configure: %+v", m)
	}

	// First call logs in once; second call reuses cached JWT token.
	for i := 0; i < 2; i++ {
		res, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{
			ItemType:    "Series",
			ProviderIDs: map[string]string{"Tvdb": "81189"},
		})
		if err != nil || res == nil || res.Name != "Breaking Bad" {
			t.Fatalf("fetch #%d failed: res=%v err=%v", i, res, err)
		}
	}
	if got := loginCount.Load(); got != 1 {
		t.Fatalf("expected 1 login call with caching, got %d", got)
	}

	// Changing api_key invalidates cached JWT token.
	_ = p.Configure(map[string]any{
		"tvdb_api_key": "key2",
		"tvdb_pin":     "pin2",
		"priority":     35,
	})
	if p.Manifest().Priority != 35 {
		t.Fatalf("expected priority=35, got %d", p.Manifest().Priority)
	}
	res, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{
		ItemType:    "Series",
		ProviderIDs: map[string]string{"tvdb": "81189"},
	})
	if err != nil || res == nil {
		t.Fatalf("fetch after key change failed: res=%v err=%v", res, err)
	}
	if got := loginCount.Load(); got != 2 {
		t.Fatalf("expected 2 login calls after api_key change, got %d", got)
	}
}

func TestConcurrentTokenCachingAndConfigureRace(t *testing.T) {
	var loginCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			loginCount.Add(1)
			_, _ = w.Write([]byte(`{"status":"success","data":{"token":"shared-jwt"}}`))
		case "/series/100/extended":
			if r.Header.Get("Authorization") != "Bearer shared-jwt" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"status":"success","data":{"id":100,"name":"Race Show"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	p := New()
	_ = p.Configure(map[string]any{
		"enabled":  true,
		"api_key":  "race-key",
		"base_url": srv.URL,
	})

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if idx%5 == 0 {
				_ = p.Configure(map[string]any{"language": "zh-CN"})
				_ = p.Manifest()
			}
			res, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{
				ItemType:    "Series",
				ProviderIDs: map[string]string{"Tvdb": "100"},
			})
			if err != nil || res == nil || res.Name != "Race Show" {
				t.Errorf("goroutine %d failed: res=%v err=%v", idx, res, err)
			}
		}(i)
	}
	wg.Wait()

	if got := loginCount.Load(); got != 1 {
		t.Fatalf("expected exactly 1 login call across concurrent requests, got %d", got)
	}
}

func TestFetchSeriesBySearchAndByID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			_, _ = w.Write([]byte(`{"status":"success","data":{"token":"tok-series"}}`))
		case "/search":
			q := r.URL.Query().Get("query")
			if q == "NotFound" {
				_, _ = w.Write([]byte(`{"status":"success","data":[]}`))
				return
			}
			_, _ = w.Write([]byte(`{
				"status": "success",
				"data": [
					{"objectID": "series-999", "tvdb_id": "999", "name": "Breaking Bad Remake", "year": "2020"},
					{"objectID": "series-81189", "tvdb_id": "81189", "name": "Breaking Bad", "year": "2008"}
				]
			}`))
		case "/series/81189/extended":
			_, _ = w.Write([]byte(`{
				"status": "success",
				"data": {
					"id": 81189,
					"name": "Breaking Bad",
					"overview": "Default EN overview",
					"firstAired": "2008-01-20",
					"year": "2008",
					"image": "https://img.tvdb/fallback.jpg",
					"score": 9.4,
					"genres": [{"name": "Drama"}, {"name": "Crime"}, {"name": "drama"}],
					"originalNetwork": {"name": "AMC"},
					"companies": [{"name": "AMC"}, {"name": "Sony Pictures Television"}],
					"characters": [
						{"name": "Walter White", "peopleType": "Actor", "personName": "Bryan Cranston", "personImgURL": "https://img.tvdb/bryan.jpg"},
						{"peopleType": "Director", "personName": "Vince Gilligan", "image": "https://img.tvdb/vince.jpg"},
						{"peopleType": "Writer", "personName": "Peter Gould"},
						{"peopleType": "Actor", "personName": ""}
					],
					"remoteIds": [
						{"id": "tt0903747", "type": 2, "sourceName": "IMDB"},
						{"id": "1396", "type": 12, "sourceName": "TheMovieDB.com"}
					],
					"artworks": [
						{"image": "https://img.tvdb/poster-en.jpg", "type": 2, "score": 900, "language": "eng"},
						{"image": "https://img.tvdb/poster-zh.jpg", "type": 2, "score": 500, "language": "zho"},
						{"image": "https://img.tvdb/backdrop.jpg", "type": 3, "score": 800, "language": "eng"},
						{"image": "https://img.tvdb/logo.png", "type": 23, "score": 700, "language": "zho"}
					],
					"translations": {
						"nameTranslations": [{"name": "绝命毒师", "language": "zho"}],
						"overviewTranslations": []
					}
				}
			}`))
		case "/series/81189/translations/zho":
			_, _ = w.Write([]byte(`{"status":"success","data":{"name":"绝命毒师","overview":"高中化学老师制毒故事"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	p := New()
	_ = p.Configure(map[string]any{
		"enabled":  true,
		"api_key":  "test-key",
		"language": "zh-CN",
		"base_url": srv.URL,
	})

	// Search with year matching second result (81189)
	res, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{
		ItemType: "Series",
		Title:    "Breaking Bad",
		Year:     2008,
	})
	if err != nil || res == nil {
		t.Fatalf("FetchMetadata series failed: res=%v err=%v", res, err)
	}
	if res.Name != "绝命毒师" || res.OriginalTitle != "Breaking Bad" || res.Overview != "高中化学老师制毒故事" {
		t.Fatalf("unexpected localized fields: %+v", res)
	}
	if res.Year != 2008 || res.PremiereDate != "2008-01-20" || res.CommunityRating != 9.4 {
		t.Fatalf("unexpected dates/rating: %+v", res)
	}
	if res.ProviderIDs["Tvdb"] != "81189" || res.ProviderIDs["Imdb"] != "tt0903747" || res.ProviderIDs["Tmdb"] != "1396" {
		t.Fatalf("unexpected ProviderIDs: %+v", res.ProviderIDs)
	}
	if res.PosterURL != "https://img.tvdb/poster-zh.jpg" || res.BackdropURL != "https://img.tvdb/backdrop.jpg" || res.LogoURL != "https://img.tvdb/logo.png" {
		t.Fatalf("unexpected artwork URLs: poster=%q backdrop=%q logo=%q", res.PosterURL, res.BackdropURL, res.LogoURL)
	}
	if len(res.Genres) != 2 || len(res.Studios) != 2 {
		t.Fatalf("expected deduplicated genres/studios, got genres=%v studios=%v", res.Genres, res.Studios)
	}
	if len(res.People) != 3 {
		t.Fatalf("expected 3 people, got %+v", res.People)
	}
	if res.People[0].Type != "Actor" || res.People[0].Role != "Walter White" || res.People[0].ImageURL != "https://img.tvdb/bryan.jpg" {
		t.Fatalf("unexpected actor: %+v", res.People[0])
	}
	if res.People[1].Type != "Director" || res.People[1].ImageURL != "https://img.tvdb/vince.jpg" {
		t.Fatalf("unexpected director: %+v", res.People[1])
	}
	if res.People[2].Type != "Writer" {
		t.Fatalf("unexpected writer: %+v", res.People[2])
	}

	// Direct lookup by TVDB ID with language override (falls back to default EN overview)
	resEN, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{
		ItemType:    "Series",
		ProviderIDs: map[string]string{"Tvdb": "81189"},
		Language:    "en-US",
	})
	if err != nil || resEN == nil {
		t.Fatalf("FetchMetadata by ID failed: res=%v err=%v", resEN, err)
	}
	if resEN.Name != "Breaking Bad" || resEN.Overview != "Default EN overview" {
		t.Fatalf("expected EN fallback, got name=%q overview=%q", resEN.Name, resEN.Overview)
	}

	// Empty title / not found
	if r, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{ItemType: "Series"}); err != nil || r != nil {
		t.Fatalf("expected nil for empty title, got r=%v err=%v", r, err)
	}
	if r, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{ItemType: "Series", Title: "NotFound"}); err != nil || r != nil {
		t.Fatalf("expected nil for not found series, got r=%v err=%v", r, err)
	}
}

func TestFetchEpisode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			_, _ = w.Write([]byte(`{"status":"success","data":{"token":"tok-ep"}}`))
		case "/search":
			_, _ = w.Write([]byte(`{"status":"success","data":[{"id":"series-81189","name":"Breaking Bad"}]}`))
		case "/series/81189/episodes/default":
			if r.URL.Query().Get("season") != "1" {
				_, _ = w.Write([]byte(`{"status":"success","data":{"episodes":[]}}`))
				return
			}
			_, _ = w.Write([]byte(`{
				"status": "success",
				"data": {
					"episodes": [
						{"id": 349232, "name": "Pilot", "overview": "EN pilot", "aired": "2008-01-20", "image": "https://img.tvdb/s01e01.jpg", "seasonNumber": 1, "number": 1},
						{"id": 349233, "name": "Cat's in the Bag...", "overview": "EN ep2", "aired": "2008-01-27", "image": "https://img.tvdb/s01e02.jpg", "seasonNumber": 1, "number": 2}
					]
				}
			}`))
		case "/episodes/349232/translations/zho":
			_, _ = w.Write([]byte(`{"status":"success","data":{"name":"试播集","overview":"沃尔特确诊肺癌。"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	p := New()
	_ = p.Configure(map[string]any{
		"enabled":  true,
		"api_key":  "test-key",
		"language": "zh-CN",
		"base_url": srv.URL,
	})

	// By TVDB ID
	ep, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{
		ItemType:      "Episode",
		SeasonNumber:  1,
		EpisodeNumber: 1,
		ProviderIDs:   map[string]string{"Tvdb": "81189"},
	})
	if err != nil || ep == nil {
		t.Fatalf("FetchMetadata episode failed: ep=%v err=%v", ep, err)
	}
	if ep.Name != "试播集" || ep.OriginalTitle != "Pilot" || ep.Overview != "沃尔特确诊肺癌。" ||
		ep.PremiereDate != "2008-01-20" || ep.Year != 2008 || ep.PosterURL != "https://img.tvdb/s01e01.jpg" ||
		ep.ProviderIDs["Tvdb"] != "349232" {
		t.Fatalf("unexpected episode result: %+v", ep)
	}

	// By search fallback (no ProviderIDs) & missing translation fallback
	ep2, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{
		ItemType:      "Episode",
		Title:         "Breaking Bad",
		SeasonNumber:  1,
		EpisodeNumber: 2,
	})
	if err != nil || ep2 == nil || ep2.Name != "Cat's in the Bag..." || ep2.ProviderIDs["Tvdb"] != "349233" {
		t.Fatalf("unexpected ep2 result: ep=%+v err=%v", ep2, err)
	}

	// Non-matching episode number -> nil, nil
	if r, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{
		ItemType:      "Episode",
		SeasonNumber:  1,
		EpisodeNumber: 99,
		ProviderIDs:   map[string]string{"tvdb": "81189"},
	}); err != nil || r != nil {
		t.Fatalf("expected nil for missing episode, got r=%v err=%v", r, err)
	}

	// Invalid season/episode number -> nil, nil
	if r, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{
		ItemType:      "Episode",
		SeasonNumber:  0,
		EpisodeNumber: 1,
		ProviderIDs:   map[string]string{"Tvdb": "81189"},
	}); err != nil || r != nil {
		t.Fatalf("expected nil for season <= 0, got r=%v err=%v", r, err)
	}
}

func TestFetchMovieWithOriginalTitleFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			_, _ = w.Write([]byte(`{"status":"success","data":{"token":"tok-movie"}}`))
		case "/search":
			q := r.URL.Query().Get("query")
			y := r.URL.Query().Get("year")
			// Fail Chinese title and year-scoped OriginalTitle, succeed on OriginalTitle without year.
			if q == "Dune" && y == "" {
				_, _ = w.Write([]byte(`{"status":"success","data":[{"tvdb_id":132245,"name":"Dune","first_air_time":"2021-10-22"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"status":"success","data":[]}`))
		case "/movies/132245/extended":
			_, _ = w.Write([]byte(`{
				"status": "success",
				"data": {
					"id": 132245,
					"name": "Dune",
					"overview": "Paul Atreides...",
					"first_release": {"date": "2021-10-22"},
					"image": "https://img.tvdb/dune-fallback.jpg",
					"score": 8.2,
					"genres": [{"name": "Science Fiction"}],
					"studios": [{"name": "Legendary Pictures"}],
					"companies": {
						"studio": [{"name": "Legendary Pictures"}],
						"production": [{"name": "Warner Bros."}]
					},
					"characters": [
						{"peopleType": "Director", "personName": "Denis Villeneuve"},
						{"name": "Paul", "peopleType": "Actor", "personName": "Timothee Chalamet"}
					],
					"remoteIds": [
						{"id": "tt1160419", "type": 2, "sourceName": "IMDB"},
						{"id": "438631", "type": 10, "sourceName": "TheMovieDB.com"}
					],
					"artworks": [
						{"image": "https://img.tvdb/dune-bg.jpg", "type": 15, "score": 100, "language": "eng"}
					],
					"translations": {
						"nameTranslations": [{"name": "沙丘", "language": "zho"}],
						"overviewTranslations": [{"overview": "保罗·厄崔迪的传奇旅程", "language": "zho"}]
					}
				}
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	p := New()
	_ = p.Configure(map[string]any{
		"enabled":  true,
		"api_key":  "test-key",
		"language": "zh-CN",
		"base_url": srv.URL,
	})

	res, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{
		ItemType:      "Movie",
		Title:         "沙丘（中文别名）",
		OriginalTitle: "Dune",
		Year:          2021,
	})
	if err != nil || res == nil {
		t.Fatalf("FetchMetadata movie failed: res=%v err=%v", res, err)
	}
	if res.Name != "沙丘" || res.OriginalTitle != "Dune" || res.Overview != "保罗·厄崔迪的传奇旅程" ||
		res.Year != 2021 || res.PremiereDate != "2021-10-22" {
		t.Fatalf("unexpected movie metadata: %+v", res)
	}
	if res.PosterURL != "https://img.tvdb/dune-fallback.jpg" || res.BackdropURL != "https://img.tvdb/dune-bg.jpg" {
		t.Fatalf("unexpected movie artworks: poster=%q backdrop=%q", res.PosterURL, res.BackdropURL)
	}
	if res.ProviderIDs["Tvdb"] != "132245" || res.ProviderIDs["Imdb"] != "tt1160419" || res.ProviderIDs["Tmdb"] != "438631" {
		t.Fatalf("unexpected movie ProviderIDs: %+v", res.ProviderIDs)
	}
	if len(res.Studios) != 2 || len(res.People) != 2 {
		t.Fatalf("unexpected studios/people: studios=%v people=%+v", res.Studios, res.People)
	}

	// Empty movie title -> nil, nil
	if r, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{ItemType: "Movie"}); err != nil || r != nil {
		t.Fatalf("expected nil for empty movie title, got r=%v err=%v", r, err)
	}
}

func TestErrorHandlingAndUnauthorizedRetry(t *testing.T) {
	t.Run("auth failure", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer srv.Close()

		p := New()
		_ = p.Configure(map[string]any{"enabled": true, "api_key": "bad", "base_url": srv.URL})
		if _, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{
			ItemType:    "Series",
			ProviderIDs: map[string]string{"Tvdb": "81189"},
		}); err == nil {
			t.Fatalf("expected auth error")
		}
	})

	t.Run("empty token from login", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"status":"success","data":{"token":""}}`))
		}))
		defer srv.Close()

		p := New()
		_ = p.Configure(map[string]any{"enabled": true, "api_key": "k", "base_url": srv.URL})
		if _, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{
			ItemType:    "Movie",
			ProviderIDs: map[string]string{"Tvdb": "1"},
		}); err == nil {
			t.Fatalf("expected empty token error")
		}
	})

	t.Run("404 on extended endpoint", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/login" {
				_, _ = w.Write([]byte(`{"status":"success","data":{"token":"tok"}}`))
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		p := New()
		_ = p.Configure(map[string]any{"enabled": true, "api_key": "k", "base_url": srv.URL})
		if _, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{
			ItemType:    "Series",
			ProviderIDs: map[string]string{"Tvdb": "404"},
		}); err == nil {
			t.Fatalf("expected 404 error")
		}
		if _, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{
			ItemType:    "Movie",
			ProviderIDs: map[string]string{"Tvdb": "404"},
		}); err == nil {
			t.Fatalf("expected 404 error for movie")
		}
		if _, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{
			ItemType:      "Episode",
			SeasonNumber:  1,
			EpisodeNumber: 1,
			ProviderIDs:   map[string]string{"Tvdb": "404"},
		}); err == nil {
			t.Fatalf("expected 404 error for episode")
		}
	})

	t.Run("401 retry refreshes token", func(t *testing.T) {
		var loginCalls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/login" {
				n := loginCalls.Add(1)
				if n == 1 {
					_, _ = w.Write([]byte(`{"status":"success","data":{"token":"stale-tok"}}`))
				} else {
					_, _ = w.Write([]byte(`{"status":"success","data":{"token":"fresh-tok"}}`))
				}
				return
			}
			if r.Header.Get("Authorization") != "Bearer fresh-tok" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"status":"success","data":{"id":7,"name":"Recovered"}}`))
		}))
		defer srv.Close()

		p := New()
		_ = p.Configure(map[string]any{"enabled": true, "api_key": "k", "base_url": srv.URL})
		res, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{
			ItemType:    "Series",
			ProviderIDs: map[string]string{"Tvdb": "7"},
		})
		if err != nil || res == nil || res.Name != "Recovered" {
			t.Fatalf("expected 401 retry to succeed, got res=%v err=%v", res, err)
		}
		if loginCalls.Load() != 2 {
			t.Fatalf("expected 2 login calls after 401 refresh, got %d", loginCalls.Load())
		}
	})
}
