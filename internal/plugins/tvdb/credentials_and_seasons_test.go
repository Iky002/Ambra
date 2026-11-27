package tvdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/plugins/tmdb"
)

func TestAuditV0107_H9_HostChangeClearsCredentialsAndFetchSeason(t *testing.T) {
	p := New()
	if err := p.Configure(map[string]any{
		"enabled":  true,
		"api_key":  "tvdb-secret-key",
		"pin":      "tvdb-secret-pin",
		"base_url": "https://api4.thetvdb.com/v4",
	}); err != nil {
		t.Fatal(err)
	}

	// Host change without new credentials must clear apiKey and pin
	if err := p.Configure(map[string]any{
		"base_url": "https://93.184.216.34/v4",
	}); err != nil {
		t.Fatal(err)
	}
	snap := p.snapshot()
	if snap.apiKey != "" || snap.pin != "" {
		t.Fatalf("expected apiKey and pin cleared on host change, got key=%q pin=%q", snap.apiKey, snap.pin)
	}

	// Same-host change preserves credentials
	if err := p.Configure(map[string]any{
		"api_key":  "k2",
		"pin":      "p2",
		"base_url": "https://93.184.216.34/v4",
	}); err != nil {
		t.Fatal(err)
	}
	if err := p.Configure(map[string]any{
		"base_url": "https://93.184.216.34/v4-alt",
	}); err != nil {
		t.Fatal(err)
	}
	snap2 := p.snapshot()
	if snap2.apiKey != "k2" || snap2.pin != "p2" {
		t.Fatalf("expected credentials preserved on same-host change, got key=%q pin=%q", snap2.apiKey, snap2.pin)
	}

	// Exercise fetchSeason & SetHTTPClient(nil)
	p.SetHTTPClient(nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			_, _ = w.Write([]byte(`{"status":"success","data":{"token":"tok"}}`))
		case "/series/100/extended":
			_, _ = w.Write([]byte(`{"data":{"id":100,"seasons":[{"id":501,"number":1,"type":{"id":1},"name":"S1","image":"https://img/s1.jpg"}]}}`))
		case "/seasons/501/extended":
			_, _ = w.Write([]byte(`{"data":{"id":501,"number":1,"name":"Season 1","image":"https://img/s1_ext.jpg","translations":{"nameTranslations":[{"name":"第一季","language":"zho"}]}}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	p.SetHTTPClient(srv.Client())
	_ = p.Configure(map[string]any{"enabled": true, "api_key": "k2", "base_url": srv.URL})
	res, err := p.FetchMetadata(context.Background(), plugins.MediaQuery{
		ItemType:     "Season",
		SeasonNumber: 1,
		ProviderIDs:  map[string]string{"Tvdb": "100"},
	})
	if err != nil || res == nil || res.Name != "第一季" {
		t.Fatalf("expected localized season metadata, got res=%+v err=%v", res, err)
	}
	if urlHostLower("://bad") != "" {
		t.Fatal("expected empty host for invalid url")
	}
}

func TestAuditV0107_TVDBHelpersAndSeasonEdgeCases(t *testing.T) {
	// flexString.UnmarshalJSON
	var f flexString
	if err := f.UnmarshalJSON([]byte("null")); err != nil || f != "" {
		t.Fatalf("null -> %q, %v", f, err)
	}
	if err := f.UnmarshalJSON([]byte(`12345`)); err != nil || f != "12345" {
		t.Fatalf("number -> %q, %v", f, err)
	}
	if err := f.UnmarshalJSON([]byte(`"abc"`)); err != nil || f != "abc" {
		t.Fatalf("string -> %q, %v", f, err)
	}
	if err := f.UnmarshalJSON([]byte(`"unterminated`)); err == nil {
		t.Fatal("expected error for malformed JSON string")
	}

	// extractID & extractYear & pickBestSearchItem
	if id := (tvdbSearchItem{ID: "series-42"}).extractID(); id != "42" {
		t.Fatalf("extractID(series-42) = %q", id)
	}
	if id := (tvdbSearchItem{ObjectID: "raw99"}).extractID(); id != "raw99" {
		t.Fatalf("extractID(raw99) = %q", id)
	}
	if id := (tvdbSearchItem{}).extractID(); id != "" {
		t.Fatalf("extractID(empty) = %q", id)
	}
	if y := (tvdbSearchItem{FirstAirTime: "2022-05-01"}).extractYear(); y != 2022 {
		t.Fatalf("extractYear(FirstAirTime) = %d", y)
	}
	if y := (tvdbSearchItem{}).extractYear(); y != 0 {
		t.Fatalf("extractYear(empty) = %d", y)
	}
	if pickBestSearchItem(nil, 2020) != "" {
		t.Fatal("expected empty for nil items")
	}
	items := []tvdbSearchItem{
		{TVDBID: "1", Year: "2010"},
		{TVDBID: "2", Year: "2021"},
		{TVDBID: "3", Year: "2020"},
	}
	if got := pickBestSearchItem(items, 0); got != "1" {
		t.Fatalf("targetYear=0 -> %q", got)
	}
	if got := pickBestSearchItem(items, 2020); got != "3" {
		t.Fatalf("exact year -> %q", got)
	}
	if got := pickBestSearchItem(items[:2], 2020); got != "2" {
		t.Fatalf("near year -> %q", got)
	}
	if got := pickBestSearchItem(items[:1], 2020); got != "1" {
		t.Fatalf("fallback first -> %q", got)
	}

	// fetchSeason edge cases
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			_, _ = w.Write([]byte(`{"status":"success","data":{"token":"tok"}}`))
		case "/search":
			if r.URL.Query().Get("query") == "FoundShow" {
				_, _ = w.Write([]byte(`{"data":[{"tvdb_id":"77","name":"FoundShow"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":[]}`))
		case "/series/77/extended":
			_, _ = w.Write([]byte(`{"data":{"id":77,"seasons":[{"id":701,"number":2,"type":{"id":2,"type":"dvd"},"name":"DVD S2","image":"https://img/s2.jpg"}]}}`))
		case "/seasons/701/extended":
			_, _ = w.Write([]byte(`{"data":{"id":701,"number":2,"name":"DVD S2"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	p := New()
	p.SetHTTPClient(srv.Client())
	_ = p.Configure(map[string]any{"enabled": true, "api_key": "k", "base_url": srv.URL})
	ctx := context.Background()

	// negative season number -> nil, nil
	if res, err := p.FetchMetadata(ctx, plugins.MediaQuery{ItemType: "Season", SeasonNumber: -1}); err != nil || res != nil {
		t.Fatalf("negative season -> %+v, %v", res, err)
	}
	// missing provider ID & empty title -> nil, nil
	if res, err := p.FetchMetadata(ctx, plugins.MediaQuery{ItemType: "Season", SeasonNumber: 1}); err != nil || res != nil {
		t.Fatalf("empty title season -> %+v, %v", res, err)
	}
	// search miss -> nil, nil
	if res, err := p.FetchMetadata(ctx, plugins.MediaQuery{ItemType: "Season", Title: "MissingShow", SeasonNumber: 1}); err != nil || res != nil {
		t.Fatalf("search miss -> %+v, %v", res, err)
	}
	// search hit + non-official season fallback
	if res, err := p.FetchMetadata(ctx, plugins.MediaQuery{ItemType: "Season", Title: "FoundShow", SeasonNumber: 2}); err != nil || res == nil || res.Name != "DVD S2" {
		t.Fatalf("non-official season fallback -> %+v, %v", res, err)
	}
	// season number not present -> nil, nil
	if res, err := p.FetchMetadata(ctx, plugins.MediaQuery{ItemType: "Season", Title: "FoundShow", SeasonNumber: 99}); err != nil || res != nil {
		t.Fatalf("missing season number -> %+v, %v", res, err)
	}
}

func TestAuditV0109_M27_AtomicConfigure(t *testing.T) {
	p := New()
	if err := p.Configure(map[string]any{
		"enabled":  true,
		"api_key":  "orig-key",
		"pin":      "orig-pin",
		"language": "zh-CN",
		"priority": 25,
	}); err != nil {
		t.Fatalf("initial Configure: %v", err)
	}

	// Invalid base_url must not mutate enabled, api_key, pin, language, priority, or tokenEpoch.
	p.mu.RLock()
	origEpoch := p.tokenEpoch
	p.mu.RUnlock()

	err := p.Configure(map[string]any{
		"enabled":  false,
		"api_key":  "new-key",
		"pin":      "new-pin",
		"language": "en-US",
		"priority": 99,
		"base_url": "ftp://bad.example.com",
	})
	if err == nil {
		t.Fatal("expected error for invalid base_url")
	}

	snap := p.snapshot()
	p.mu.RLock()
	afterEpoch := p.tokenEpoch
	p.mu.RUnlock()
	if !snap.enabled || snap.apiKey != "orig-key" || snap.pin != "orig-pin" || snap.language != "zh-CN" || snap.priority != 25 || snap.baseURL != defaultBaseURL || afterEpoch != origEpoch {
		t.Fatalf("Configure partially mutated tvdb state on error: %+v epoch=%d->%d", snap, origEpoch, afterEpoch)
	}
}

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
