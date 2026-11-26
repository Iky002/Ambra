package rankings

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func openTestStore(t *testing.T) *sqlite.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func getItemTags(t *testing.T, store *sqlite.Store, itemID string) []string {
	t.Helper()
	rows, err := store.Reader.Query(`
		SELECT t.name
		FROM item_taxonomies it
		JOIN taxonomies t ON t.id = it.taxonomy_id
		WHERE it.item_id = ? AND t.kind = 'tag'
		ORDER BY t.name ASC`, itemID)
	if err != nil {
		t.Fatalf("query item tags: %v", err)
	}
	defer rows.Close()
	var tags []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan tag: %v", err)
		}
		tags = append(tags, name)
	}
	return tags
}

func TestFetchDoubanTop250HTMLPaginationAndTolerance(t *testing.T) {
	page0 := `<!doctype html><html><body><ol class="grid_view">
<li><div class="item">
  <div class="pic"><em class="">1</em><a href="https://movie.douban.com/subject/1292052/"><img/></a></div>
  <div class="info"><div class="hd">
    <a href="https://movie.douban.com/subject/1292052/">
      <span class="title">肖申克的救赎</span>
      <span class="title">&nbsp;/&nbsp;The Shawshank Redemption</span>
    </a></div>
    <div class="bd"><p class="">导演: 弗兰克·德拉邦特<br>1994&nbsp;/&nbsp;美国&nbsp;/&nbsp;犯罪 剧情</p>
      <div class="star"><span class="rating_num" property="v:average">9.7</span></div>
    </div></div>
</div></li>
<li><div class="item">
  <div class="pic"><em class="">2</em><a href="https://movie.douban.com/subject/1291546/"><img/></a></div>
  <div class="info"><div class="hd">
    <a href="https://movie.douban.com/subject/1291546/">
      <span class="title">霸王别姬</span>
      <span class="title">&nbsp;/&nbsp;Farewell My Concubine</span>
    </a></div>
    <div class="bd"><p class="">1993 / 中国大陆 / 剧情</p>
      <div class="star"><span class="rating_num">9.6</span></div>
    </div></div>
</div></li>
</ol></body></html>`

	page50 := `<!doctype html><html><body><ol class="grid_view">
<li><div class="item">
  <div class="pic"><em class="">51</em><a href="https://movie.douban.com/subject/1292720/"><img/></a></div>
  <div class="info"><div class="hd">
    <a href="https://movie.douban.com/subject/1292720/">
      <span class="title">阿甘正传</span>
      <span class="title">&nbsp;/&nbsp;Forrest Gump &amp; Friends</span>
    </a></div>
    <div class="bd"><p class="">1994 / 美国</p>
      <div class="star"><span class="rating_num">9.5</span></div>
    </div></div>
</div></li>
<li><div class="item">
  <div class="pic"><em class="">1</em><a href="https://movie.douban.com/subject/1292052/"><img/></a></div>
  <div class="info"><div class="hd"><span class="title">肖申克的救赎</span></div></div>
</div></li>
</ol></body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Errorf("expected User-Agent header")
		}
		switch r.URL.Query().Get("start") {
		case "", "0":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(page0))
		case "25":
			// Simulate transient single-page error; fetcher should tolerate and continue to start=50.
			w.WriteHeader(http.StatusInternalServerError)
		case "50":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(page50))
		default:
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<html><body>empty</body></html>`))
		}
	}))
	defer srv.Close()

	f := NewFetcher(Options{DoubanURL: srv.URL, HTTPClient: srv.Client()})
	snap, err := f.FetchDoubanTop250(context.Background())
	if err != nil {
		t.Fatalf("FetchDoubanTop250 failed: %v", err)
	}
	if snap.Chart != ChartDoubanTop250 || snap.Count != 3 || len(snap.Entries) != 3 {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}
	if snap.Entries[0].DoubanID != "1292052" || snap.Entries[0].Title != "肖申克的救赎" ||
		snap.Entries[0].OriginalTitle != "The Shawshank Redemption" || snap.Entries[0].Year != 1994 || snap.Entries[0].Rating != 9.7 {
		t.Fatalf("unexpected entry[0]: %+v", snap.Entries[0])
	}
	if snap.Entries[2].OriginalTitle != "Forrest Gump & Friends" || snap.Entries[2].Rank != 51 {
		t.Fatalf("unexpected entry[2]: %+v", snap.Entries[2])
	}
}

func TestFetchIMDbTop250JSONLD(t *testing.T) {
	imdbHTML := `<!doctype html><html><head>
<script type="application/ld+json">{"@type":"WebPage","name":"not a list"}</script>
<script type="application/ld+json">{ invalid json</script>
<script type="application/ld+json">{
  "@context": "https://schema.org",
  "@type": "ItemList",
  "itemListElement": [
    {
      "@type": "ListItem",
      "position": 1,
      "item": {
        "@type": "Movie",
        "url": "https://www.imdb.com/title/tt0111161/",
        "name": "The Shawshank Redemption",
        "alternateName": "肖申克的救赎",
        "datePublished": "1994-10-14",
        "aggregateRating": {"ratingValue": 9.3}
      }
    },
    {
      "@type": "ListItem",
      "url": "https://www.imdb.com/title/TT0068646/",
      "name": "The Godfather",
      "alternateName": "教父",
      "datePublished": "1972-03-24",
      "aggregateRating": {"ratingValue": "9.2"}
    },
    {
      "@type": "ListItem",
      "position": 3,
      "item": {
        "url": "https://www.imdb.com/title/tt0111161/",
        "name": "Duplicate Shawshank"
      }
    }
  ]
}</script></head><body></body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(imdbHTML))
	}))
	defer srv.Close()

	f := NewFetcher(Options{IMDbURL: srv.URL, HTTPClient: srv.Client()})
	snap, err := f.FetchIMDbTop250(context.Background())
	if err != nil {
		t.Fatalf("FetchIMDbTop250 failed: %v", err)
	}
	if snap.Chart != ChartIMDbTop250 || snap.Count != 2 {
		t.Fatalf("expected 2 deduplicated entries, got %d", snap.Count)
	}
	if snap.Entries[0].IMDbID != "tt0111161" || snap.Entries[0].Year != 1994 || snap.Entries[0].Rating != 9.3 {
		t.Fatalf("unexpected entry[0]: %+v", snap.Entries[0])
	}
	if snap.Entries[1].IMDbID != "tt0068646" || snap.Entries[1].Rank != 2 || snap.Entries[1].Rating != 9.2 {
		t.Fatalf("unexpected entry[1]: %+v", snap.Entries[1])
	}
}

func TestFetchCustomJSONFeedsAndEnvVars(t *testing.T) {
	doubanSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"title":"千与千寻","original_title":"Spirited Away","year":2001,"douban_id":"1291561","rating":9.4},
			{"title":"千与千寻","year":2001,"douban_id":"1291561"},
			{}
		]`))
	}))
	defer doubanSrv.Close()

	imdbSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// No Content-Type header, detected via leading '{'
		_, _ = w.Write([]byte(`{"entries":[
			{"rank":1,"title":"The Dark Knight","year":2008,"imdb_id":"TT0468569","tmdb_id":"155","rating":9.0}
		]}`))
	}))
	defer imdbSrv.Close()

	t.Setenv(EnvDouban250URL, doubanSrv.URL)
	t.Setenv(EnvIMDb250URL, imdbSrv.URL)

	f := NewFetcher(Options{HTTPClient: doubanSrv.Client()})
	dSnap, err := f.FetchDoubanTop250(context.Background())
	if err != nil {
		t.Fatalf("FetchDoubanTop250 json failed: %v", err)
	}
	if dSnap.Count != 1 || dSnap.Entries[0].Rank != 1 || dSnap.Entries[0].DoubanID != "1291561" {
		t.Fatalf("unexpected douban json snap: %+v", dSnap)
	}

	iSnap, err := f.FetchIMDbTop250(context.Background())
	if err != nil {
		t.Fatalf("FetchIMDbTop250 json failed: %v", err)
	}
	if iSnap.Count != 1 || iSnap.Entries[0].IMDbID != "tt0468569" || iSnap.Entries[0].TMDBID != "155" {
		t.Fatalf("unexpected imdb json snap: %+v", iSnap)
	}

	// Test "items" wrapper as well.
	itemsEntries, err := parseJSONEntries([]byte(`{"items":[{"title":"Inception","year":2010}]}`), 10)
	if err != nil || len(itemsEntries) != 1 || itemsEntries[0].Title != "Inception" {
		t.Fatalf("unexpected items wrapper parse: %v, %+v", err, itemsEntries)
	}
}

func TestFetcherErrorPaths(t *testing.T) {
	badHTMLSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body>nothing here</body></html>`))
	}))
	defer badHTMLSrv.Close()

	badJSONSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{bad json`))
	}))
	defer badJSONSrv.Close()

	emptyJSONSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer emptyJSONSrv.Close()

	err500Srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer err500Srv.Close()

	ctx := context.Background()
	testClient := badHTMLSrv.Client()
	if _, err := NewFetcher(Options{DoubanURL: badHTMLSrv.URL, HTTPClient: testClient}).FetchDoubanTop250(ctx); err == nil {
		t.Fatal("expected error for empty douban html")
	}
	if _, err := NewFetcher(Options{DoubanURL: badJSONSrv.URL, HTTPClient: testClient}).FetchDoubanTop250(ctx); err == nil {
		t.Fatal("expected error for invalid douban json")
	}
	if _, err := NewFetcher(Options{DoubanURL: emptyJSONSrv.URL, HTTPClient: testClient}).FetchDoubanTop250(ctx); err == nil {
		t.Fatal("expected error for empty douban json")
	}
	if _, err := NewFetcher(Options{DoubanURL: err500Srv.URL, HTTPClient: testClient}).FetchDoubanTop250(ctx); err == nil {
		t.Fatal("expected error for 502 douban response")
	}
	if _, err := NewFetcher(Options{IMDbURL: badHTMLSrv.URL, HTTPClient: testClient}).FetchIMDbTop250(ctx); err == nil {
		t.Fatal("expected error for imdb html without ld+json")
	}
	if _, err := NewFetcher(Options{IMDbURL: badJSONSrv.URL, HTTPClient: testClient}).FetchIMDbTop250(ctx); err == nil {
		t.Fatal("expected error for invalid imdb json")
	}
	if _, err := NewFetcher(Options{IMDbURL: emptyJSONSrv.URL, HTTPClient: testClient}).FetchIMDbTop250(ctx); err == nil {
		t.Fatal("expected error for empty imdb json")
	}
	if _, err := NewFetcher(Options{IMDbURL: err500Srv.URL, HTTPClient: testClient}).FetchIMDbTop250(ctx); err == nil {
		t.Fatal("expected error for 502 imdb response")
	}

	// Default URL resolution when unset
	t.Setenv(EnvDouban250URL, "")
	t.Setenv(EnvIMDb250URL, "")
	defF := NewFetcher(Options{})
	if defF.resolveDoubanURL() != DefaultDoubanTop250URL || defF.resolveIMDbURL() != DefaultIMDbTop250URL {
		t.Fatalf("unexpected default URLs: %s / %s", defF.resolveDoubanURL(), defF.resolveIMDbURL())
	}
}

func TestCachePersistenceFailureRetentionAndMatching(t *testing.T) {
	store := openTestStore(t)
	var failFetches atomic.Bool

	doubanSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failFetches.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"entries":[
			{"rank":1,"title":"肖申克的救赎","original_title":"The Shawshank Redemption","year":1994,"douban_id":"1292052","imdb_id":"tt0111161"},
			{"rank":2,"title":"千与千寻","original_title":"Spirited Away","year":2001,"douban_id":"1291561","tmdb_id":"129"},
			{"rank":3,"title":"星际穿越：重映版","original_title":"Interstellar","year":2014}
		]}`))
	}))
	defer doubanSrv.Close()

	imdbSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failFetches.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"entries":[
			{"rank":1,"title":"The Shawshank Redemption","year":1994,"imdb_id":"tt0111161"},
			{"rank":2,"title":"Breaking Bad","year":0,"imdb_id":"tt0903747"}
		]}`))
	}))
	defer imdbSrv.Close()

	opts := Options{
		DoubanURL:  doubanSrv.URL,
		IMDbURL:    imdbSrv.URL,
		HTTPClient: doubanSrv.Client(),
		TMDBTrendingFetcher: func(ctx context.Context) ([]ChartEntry, string, error) {
			if failFetches.Load() {
				return nil, "", http.ErrHandlerTimeout
			}
			return []ChartEntry{
				{Rank: 1, Title: "沙丘2", OriginalTitle: "Dune: Part Two", Year: 2024, MediaType: "movie", TMDBID: "693134", Rating: 8.2},
			}, SourceTMDBTrendingAPI, nil
		},
		DoubanTrendingMovieFetcher: func(ctx context.Context) ([]ChartEntry, string, error) {
			if failFetches.Load() {
				return nil, "", http.ErrHandlerTimeout
			}
			return []ChartEntry{
				{Rank: 1, Title: "好东西", OriginalTitle: "Her Story", Year: 2024, MediaType: "movie", DoubanID: "36779837", Rating: 9.1},
			}, SourceDoubanTrendingMovieAPI, nil
		},
		DoubanTrendingTVFetcher: func(ctx context.Context) ([]ChartEntry, string, error) {
			if failFetches.Load() {
				return nil, "", http.ErrHandlerTimeout
			}
			return []ChartEntry{
				{Rank: 1, Title: "山花烂漫时", OriginalTitle: "She and Her Girls", Year: 2024, MediaType: "tv", DoubanID: "35503073", Rating: 9.6},
			}, SourceDoubanTrendingTVAPI, nil
		},
	}
	svc := New(store, nil, opts)
	ctx := context.Background()

	// Initial sync populates memory & SQLite server_config.
	summary, err := svc.SyncAndApply(ctx)
	if err != nil {
		t.Fatalf("SyncAndApply failed: %v", err)
	}
	if summary.DoubanCount != 3 || summary.IMDbCount != 2 || summary.TMDBTrendingCount != 1 ||
		summary.DoubanTrendingMovieCount != 1 || summary.DoubanTrendingTVCount != 1 {
		t.Fatalf("unexpected counts: %+v", summary)
	}

	// Simulate total network failure -> old cache must be preserved.
	failFetches.Store(true)
	failSummary, err := svc.SyncAndApply(ctx)
	if err == nil {
		t.Fatal("expected error when all sources fail")
	}
	if failSummary.DoubanError == "" || failSummary.IMDbError == "" || failSummary.TMDBTrendingError == "" ||
		failSummary.DoubanTrendingMovieError == "" || failSummary.DoubanTrendingTVError == "" {
		t.Fatalf("expected all errors populated: %+v", failSummary)
	}
	if failSummary.DoubanCount != 3 || failSummary.IMDbCount != 2 || failSummary.TMDBTrendingCount != 1 ||
		failSummary.DoubanTrendingMovieCount != 1 || failSummary.DoubanTrendingTVCount != 1 {
		t.Fatalf("expected old cache preserved on failure, got %+v", failSummary)
	}

	// Fresh service instance reloads persisted cache from SQLite.
	svc2 := New(store, nil, opts)
	if err := svc2.LoadCache(ctx); err != nil {
		t.Fatalf("LoadCache failed: %v", err)
	}
	status := svc2.Status(true)
	if status.DoubanTop250.Count != 3 || len(status.DoubanTop250.Entries) != 3 || status.IMDbTop250.Count != 2 {
		t.Fatalf("unexpected reloaded status: %+v", status)
	}

	// Matching tests:
	// 1. IMDb ID case-insensitive match -> both Douban & IMDb
	tags := svc2.MatchTagsForItem("Random Name", "", 2020, map[string]string{"IMDb": "TT0111161"})
	if strings.Join(tags, ",") != TagDoubanTop250+","+TagIMDbTop250 {
		t.Fatalf("expected both tags by IMDb ID, got %v", tags)
	}
	// 2. Douban ID exact match
	tags = svc2.MatchTagsForItem("Other", "", 0, map[string]string{"douban": "1291561"})
	if strings.Join(tags, ",") != TagDoubanTop250 {
		t.Fatalf("expected Douban tag by Douban ID, got %v", tags)
	}
	// 3. TMDB ID exact match
	tags = svc2.MatchTagsForItem("Other", "", 0, map[string]string{"Tmdb": "129"})
	if strings.Join(tags, ",") != TagDoubanTop250 {
		t.Fatalf("expected Douban tag by TMDB ID, got %v", tags)
	}
	// 4. Normalized title + ±1 year tolerance
	tags = svc2.MatchTagsForItem("星际穿越 - 重映版!", "", 2015, nil)
	if strings.Join(tags, ",") != TagDoubanTop250 {
		t.Fatalf("expected Douban tag by normalized title + year±1, got %v", tags)
	}
	// 5. Year outside ±1 tolerance should NOT match
	tags = svc2.MatchTagsForItem("星际穿越重映版", "", 2018, nil)
	if len(tags) != 0 {
		t.Fatalf("expected no match when year differs by 4, got %v", tags)
	}
	// 6. OriginalTitle match when entry has no year (year == 0)
	tags = svc2.MatchTagsForItem("绝命毒师", "Breaking Bad!", 2008, nil)
	if strings.Join(tags, ",") != TagIMDbTop250 {
		t.Fatalf("expected IMDb tag by OriginalTitle when chart entry has no year, got %v", tags)
	}
}

func TestApplyTagsToLibraryNonDestructiveIdempotentAndEvent(t *testing.T) {
	store := openTestStore(t)
	repo := sqlite.NewLibraryRepo(store)
	bus := eventbus.New()
	ctx := context.Background()

	var eventCount atomic.Int32
	eventCh := make(chan eventbus.Event, 4)
	bus.Subscribe(eventbus.EventLibraryChanged, func(_ context.Context, e eventbus.Event) {
		eventCount.Add(1)
		eventCh <- e
	})

	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{"/movies"})
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}
	y1994 := 1994
	y2008 := 2008
	movie1 := &sqlite.ItemRecord{
		LibraryID:      lib.ID,
		Type:           "Movie",
		Name:           "肖申克的救赎",
		OriginalTitle:  "The Shawshank Redemption",
		ProductionYear: &y1994,
		Extra:          sqlite.ItemExtra{ProviderIDs: map[string]string{"Imdb": "tt0111161"}},
	}
	if err := repo.UpsertItem(ctx, movie1); err != nil {
		t.Fatalf("UpsertItem movie1: %v", err)
	}
	// Pre-existing user custom tag that MUST be preserved!
	if err := repo.ReplaceItemTaxonomies(ctx, movie1.ID, "tag", []string{"我的收藏", "经典必看"}); err != nil {
		t.Fatalf("ReplaceItemTaxonomies: %v", err)
	}

	series1 := &sqlite.ItemRecord{
		LibraryID:      lib.ID,
		Type:           "Series",
		Name:           "绝命毒师",
		OriginalTitle:  "Breaking Bad",
		ProductionYear: &y2008,
	}
	if err := repo.UpsertItem(ctx, series1); err != nil {
		t.Fatalf("UpsertItem series1: %v", err)
	}

	// Episode should NOT be tagged.
	episode1 := &sqlite.ItemRecord{
		LibraryID:      lib.ID,
		Type:           "Episode",
		Name:           "The Shawshank Redemption",
		ProductionYear: &y1994,
	}
	if err := repo.UpsertItem(ctx, episode1); err != nil {
		t.Fatalf("UpsertItem episode1: %v", err)
	}

	doubanSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"rank":1,"title":"肖申克的救赎","year":1994,"imdb_id":"tt0111161"}]`))
	}))
	defer doubanSrv.Close()

	imdbSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"rank":1,"title":"The Shawshank Redemption","year":1994,"imdb_id":"tt0111161"},{"rank":2,"title":"Breaking Bad","year":2008}]`))
	}))
	defer imdbSrv.Close()

	svc := New(store, bus, Options{
		DoubanURL:  doubanSrv.URL,
		IMDbURL:    imdbSrv.URL,
		HTTPClient: doubanSrv.Client(),
		TMDBTrendingFetcher: func(ctx context.Context) ([]ChartEntry, string, error) {
			return []ChartEntry{
				{Rank: 1, Title: "绝命毒师", OriginalTitle: "Breaking Bad", Year: 2008, MediaType: "tv", TMDBID: "1396"},
			}, SourceTMDBTrendingAPI, nil
		},
		DoubanTrendingMovieFetcher: func(ctx context.Context) ([]ChartEntry, string, error) {
			return []ChartEntry{
				{Rank: 1, Title: "好东西", Year: 2024, MediaType: "movie", DoubanID: "36779837"},
			}, SourceDoubanTrendingMovieAPI, nil
		},
		DoubanTrendingTVFetcher: func(ctx context.Context) ([]ChartEntry, string, error) {
			return []ChartEntry{
				{Rank: 1, Title: "山花烂漫时", Year: 2024, MediaType: "tv", DoubanID: "35503073"},
			}, SourceDoubanTrendingTVAPI, nil
		},
	})
	summary, err := svc.SyncAndApply(ctx)
	if err != nil {
		t.Fatalf("SyncAndApply: %v", err)
	}
	if summary.MatchedItems != 2 || summary.TagsAdded != 4 {
		t.Fatalf("expected matched=2, tagsAdded=4, got %+v", summary)
	}

	// Verify user custom tags were preserved alongside new ranking tags.
	m1Tags := getItemTags(t, store, movie1.ID)
	wantM1 := "IMDb Top250,我的收藏,经典必看,豆瓣Top250"
	if strings.Join(m1Tags, ",") != wantM1 {
		t.Fatalf("movie1 tags = %q, want %q", strings.Join(m1Tags, ","), wantM1)
	}
	// Series must NOT receive movie-only IMDb Top250 tag.
	s1Tags := getItemTags(t, store, series1.ID)
	wantS1 := TagTMDBTrendingSpecific + "," + TagTMDBTrending
	if strings.Join(s1Tags, ",") != wantS1 {
		t.Fatalf("series1 tags = %v, want %q", s1Tags, wantS1)
	}
	if epTags := getItemTags(t, store, episode1.ID); len(epTags) != 0 {
		t.Fatalf("episode should not be tagged, got %v", epTags)
	}

	// Verify EventLibraryChanged fired.
	select {
	case ev := <-eventCh:
		if ev.Type != eventbus.EventLibraryChanged {
			t.Fatalf("unexpected event type: %v", ev.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for EventLibraryChanged")
	}

	// Idempotent second run: matched=2, tagsAdded=0, no new event.
	matched, added, err := svc.ApplyTagsToLibrary(ctx)
	if err != nil {
		t.Fatalf("second ApplyTagsToLibrary: %v", err)
	}
	if matched != 2 || added != 0 {
		t.Fatalf("expected idempotent matched=2, added=0, got matched=%d added=%d", matched, added)
	}
	time.Sleep(50 * time.Millisecond)
	if eventCount.Load() != 1 {
		t.Fatalf("expected exactly 1 event, got %d", eventCount.Load())
	}

	// Nil store safety.
	nilSvc := New(nil, nil, Options{})
	if err := nilSvc.LoadCache(ctx); err != nil {
		t.Fatalf("nil store LoadCache: %v", err)
	}
	if m, a, err := nilSvc.ApplyTagsToLibrary(ctx); err != nil || m != 0 || a != 0 {
		t.Fatalf("nil store ApplyTagsToLibrary: %d %d %v", m, a, err)
	}
}

func TestStartPeriodicSyncAndGracefulShutdown(t *testing.T) {
	store := openTestStore(t)
	var hits atomic.Int32

	feedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"rank":1,"title":"Test Movie","year":2020}]`))
	}))
	defer feedSrv.Close()

	svc := New(store, nil, Options{
		DoubanURL:              feedSrv.URL,
		IMDbURL:                feedSrv.URL,
		DoubanTrendingMovieURL: feedSrv.URL,
		DoubanTrendingTVURL:    feedSrv.URL,
		HTTPClient:             feedSrv.Client(),
		InitialDelay:           10 * time.Millisecond,
		SyncInterval:           35 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	svc.StartPeriodicSync(ctx, 0)
	// Duplicate call should be a no-op while running.
	svc.StartPeriodicSync(ctx, 35*time.Millisecond)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if hits.Load() >= 4 && !svc.needsInitialSync(24*time.Hour) { // at least initial sync + 1 periodic tick & cache populated
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	cancel()

	if hits.Load() < 4 {
		t.Fatalf("expected >= 4 feed hits from initial+periodic sync, got %d", hits.Load())
	}

	// Wait briefly for goroutine exit and ensure started flag resets.
	deadline = time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) && svc.started.Load() {
		time.Sleep(10 * time.Millisecond)
	}
	if svc.started.Load() {
		t.Fatal("expected periodic sync goroutine to exit after ctx cancel")
	}

	// Fresh cache should NOT trigger needsInitialSync.
	if svc.needsInitialSync(24 * time.Hour) {
		t.Fatal("expected fresh cache not to need initial sync")
	}
}

func TestHTTPHandlersAuthAndResponses(t *testing.T) {
	store := openTestStore(t)
	var failAll atomic.Bool

	feedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failAll.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"rank":1,"title":"霸王别姬","year":1993,"douban_id":"1291546"}]`))
	}))
	defer feedSrv.Close()

	svc := New(store, nil, Options{
		DoubanURL:              feedSrv.URL,
		IMDbURL:                feedSrv.URL,
		DoubanTrendingMovieURL: feedSrv.URL,
		DoubanTrendingTVURL:    feedSrv.URL,
		HTTPClient:             feedSrv.Client(),
	})
	h := NewHandler(svc)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{
		RequireAuth: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Auth") == "" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				next.ServeHTTP(w, r)
			})
		},
		RequireAdmin: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Admin") != "1" {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				next.ServeHTTP(w, r)
			})
		},
	})

	// Unauthenticated GET -> 401
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/System/Rankings", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}

	// Non-admin POST -> 403
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/System/Rankings/Refresh", nil)
	req.Header.Set("X-Auth", "user")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}

	// Admin POST -> 200 SyncSummary
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/System/Rankings/Refresh", nil)
	req.Header.Set("X-Admin", "1")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var sum SyncSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &sum); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if sum.DoubanCount != 1 || sum.IMDbCount != 1 {
		t.Fatalf("unexpected summary: %+v", sum)
	}

	// Authenticated GET without entries
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/System/Rankings", nil)
	req.Header.Set("X-Auth", "user")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var st StatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if st.DoubanTop250.Count != 1 || len(st.DoubanTop250.Entries) != 0 {
		t.Fatalf("expected count=1 without entries, got %+v", st.DoubanTop250)
	}

	// Authenticated GET with ?includeEntries=true
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/System/Rankings?includeEntries=true", nil)
	req.Header.Set("X-Auth", "user")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("decode status with entries: %v", err)
	}
	if len(st.DoubanTop250.Entries) != 1 || st.DoubanTop250.Entries[0].Title != "霸王别姬" {
		t.Fatalf("expected entries included, got %+v", st.DoubanTop250)
	}

	// Admin POST when all fetches fail -> 502 with Summary
	failAll.Store(true)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/System/Rankings/Refresh", nil)
	req.Header.Set("X-Admin", "1")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", rec.Code, rec.Body.String())
	}

	// Default passthrough AuthFuncs{}
	mux2 := http.NewServeMux()
	NewHandler(svc).RegisterRoutes(mux2, AuthFuncs{})
	rec = httptest.NewRecorder()
	mux2.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/System/Rankings", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with passthrough auth, got %d", rec.Code)
	}
}

func TestEdgeCasesAndCoverage(t *testing.T) {
	// Empty Status before any sync
	emptySvc := New(nil, nil, Options{})
	st := emptySvc.Status(false)
	if st.DoubanTop250.Chart != ChartDoubanTop250 || st.DoubanTop250.Count != 0 || st.IMDbTop250.Chart != ChartIMDbTop250 {
		t.Fatalf("unexpected empty status: %+v", st)
	}

	// Helpers edge cases
	if extractIMDbID("https://example.com/none") != "" {
		t.Fatal("expected empty IMDb ID")
	}
	if normalizeIMDbID("  CUSTOM-ID ") != "custom-id" {
		t.Fatalf("unexpected normalizeIMDbID: %q", normalizeIMDbID("  CUSTOM-ID "))
	}
	if entryDedupKey(ChartEntry{TMDBID: "42"}) != "tmdb:42" {
		t.Fatal("expected tmdb dedup key")
	}
	if entryDedupKey(ChartEntry{OriginalTitle: "Only Orig", Year: 2000}) != "title:onlyorig:2000" {
		t.Fatal("expected original title dedup key")
	}
	if entryDedupKey(ChartEntry{}) != "" {
		t.Fatal("expected empty dedup key")
	}
	if _, err := parseJSONEntries(nil, 10); err == nil {
		t.Fatal("expected error on empty JSON payload")
	}
	capped, err := parseJSONEntries([]byte(`[{"title":"A"},{"title":"B"},{"title":"C"}]`), 2)
	if err != nil || len(capped) != 2 {
		t.Fatalf("expected capped to 2, got %d (%v)", len(capped), err)
	}
	if _, err := parseIMDbTop250HTML([]byte(`<script type="application/ld+json">{"@type":"WebPage"}</script>`)); err == nil {
		t.Fatal("expected error when no ItemList in ld+json")
	}

	// extractProviderIDs with blank value
	imdbID, doubanID, tmdbID := extractProviderIDs(map[string]string{"imdb": "  ", "douban": "123"})
	if imdbID != "" || doubanID != "123" || tmdbID != "" {
		t.Fatalf("unexpected provider IDs: %q %q %q", imdbID, doubanID, tmdbID)
	}

	// LoadCache error on corrupted JSON in server_config
	store := openTestStore(t)
	_, err = store.Writer.Exec(
		`INSERT INTO server_config (key, value_json, updated_at) VALUES (?, ?, ?)`,
		ConfigKeyDoubanTop250, "{corrupted", time.Now().Unix(),
	)
	if err != nil {
		t.Fatalf("seed corrupt config: %v", err)
	}
	corruptSvc := New(store, nil, Options{})
	if err := corruptSvc.LoadCache(context.Background()); err == nil {
		t.Fatal("expected LoadCache error on corrupted JSON")
	}

	// StartPeriodicSync canceled during InitialDelay
	ctx, cancel := context.WithCancel(context.Background())
	delaySvc := New(nil, nil, Options{InitialDelay: 5 * time.Second, SyncInterval: time.Hour})
	delaySvc.StartPeriodicSync(ctx, time.Hour)
	time.Sleep(15 * time.Millisecond)
	cancel()
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) && delaySvc.started.Load() {
		time.Sleep(10 * time.Millisecond)
	}
	if delaySvc.started.Load() {
		t.Fatal("expected periodic sync to stop during initial delay on cancel")
	}
}

func TestFetchTMDBTrendingAPIKeyAndBearer(t *testing.T) {
	t.Setenv(EnvTMDBAPIKey, "")
	t.Setenv(EnvTMDBAPIKeyFallback, "")
	t.Setenv(EnvTMDBTrendingURL, "")

	var sawBearer atomic.Bool
	var sawAPIKey atomic.Bool
	var failTV atomic.Bool

	tmdbSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
			sawBearer.Store(true)
			if r.URL.Query().Get("api_key") != "" {
				t.Errorf("did not expect api_key query param with Bearer token")
			}
		} else if r.URL.Query().Get("api_key") == "test-v3-key" {
			sawAPIKey.Store(true)
		} else {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Query().Get("language") != "zh-CN" {
			t.Errorf("expected language=zh-CN, got %q", r.URL.Query().Get("language"))
		}

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/trending/movie/week":
			_, _ = w.Write([]byte(`{"results":[
				{"id":693134,"title":"沙丘2","original_title":"Dune: Part Two","release_date":"2024-02-27","overview":"沙漠史诗","vote_average":8.2},
				{"id":693134,"title":"沙丘2","original_title":"Dune: Part Two","release_date":"2024-02-27","vote_average":8.2},
				{"id":0,"title":""}
			]}`))
		case "/trending/tv/week":
			if failTV.Load() {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			_, _ = w.Write([]byte(`{"results":[
				{"id":93405,"name":"鱿鱼游戏","original_name":"오징어 게임","first_air_date":"2021-09-17","overview":"生存游戏","vote_average":7.8,"media_type":"tv"}
			]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer tmdbSrv.Close()

	// 1. Standard v3 api_key query parameter
	f := NewFetcher(Options{
		TMDBAPIKey:          "test-v3-key",
		TMDBTrendingBaseURL: tmdbSrv.URL + "/",
		HTTPClient:          tmdbSrv.Client(),
	})
	snap, err := f.FetchTMDBTrending(context.Background())
	if err != nil {
		t.Fatalf("FetchTMDBTrending (api_key) failed: %v", err)
	}
	if !sawAPIKey.Load() {
		t.Fatal("expected api_key query param to be sent")
	}
	if snap.Chart != ChartTMDBTrending || snap.Source != SourceTMDBTrendingAPI || snap.Count != 2 {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}
	if snap.Entries[0].TMDBID != "693134" || snap.Entries[0].MediaType != "movie" || snap.Entries[0].Year != 2024 || snap.Entries[0].Rank != 1 || snap.Entries[0].Overview != "沙漠史诗" {
		t.Fatalf("unexpected movie entry: %+v", snap.Entries[0])
	}
	if snap.Entries[1].TMDBID != "93405" || snap.Entries[1].MediaType != "tv" || snap.Entries[1].Year != 2021 || snap.Entries[1].Rank != 2 {
		t.Fatalf("unexpected tv entry: %+v", snap.Entries[1])
	}

	// 2. Bearer JWT token via env vars + partial failure (TV fails, movie succeeds)
	failTV.Store(true)
	t.Setenv(EnvTMDBAPIKeyFallback, "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ0ZXN0In0.sig")
	t.Setenv(EnvTMDBTrendingURL, tmdbSrv.URL)
	fEnv := NewFetcher(Options{HTTPClient: tmdbSrv.Client()})
	snap2, err := fEnv.FetchTMDBTrending(context.Background())
	if err != nil {
		t.Fatalf("FetchTMDBTrending (bearer) failed: %v", err)
	}
	if !sawBearer.Load() {
		t.Fatal("expected Authorization: Bearer header")
	}
	if snap2.Source != SourceTMDBTrendingAPI || snap2.Count != 1 {
		t.Fatalf("expected 1 movie entry on partial tv failure, got %+v", snap2)
	}

	// 3. AMBRA_TMDB_API_KEY env var & default URL resolution
	t.Setenv(EnvTMDBAPIKey, "ambra-env-key")
	t.Setenv(EnvTMDBTrendingURL, "")
	fDef := NewFetcher(Options{})
	if fDef.resolveTMDBAPIKey() != "ambra-env-key" {
		t.Fatalf("expected ambra-env-key, got %q", fDef.resolveTMDBAPIKey())
	}
	if fDef.resolveTMDBBaseURL() != DefaultTMDBBaseURL {
		t.Fatalf("expected default TMDB base URL, got %q", fDef.resolveTMDBBaseURL())
	}

	// 4. Dynamic TMDBAPIKeyFunc & DefaultTMDBAPIKey fallback priority
	t.Setenv(EnvTMDBAPIKey, "")
	t.Setenv(EnvTMDBAPIKeyFallback, "")
	dynamicKey := ""
	fDyn := NewFetcher(Options{
		TMDBAPIKeyFunc:    func() string { return dynamicKey },
		DefaultTMDBAPIKey: "builtin-default-key",
	})
	if got := fDyn.resolveTMDBAPIKey(); got != "builtin-default-key" {
		t.Fatalf("expected DefaultTMDBAPIKey fallback, got %q", got)
	}
	// Env var takes precedence over DefaultTMDBAPIKey when dynamic user key is empty
	t.Setenv(EnvTMDBAPIKeyFallback, "env-fallback-key")
	if got := fDyn.resolveTMDBAPIKey(); got != "env-fallback-key" {
		t.Fatalf("expected env fallback over DefaultTMDBAPIKey, got %q", got)
	}
	// Dynamic user key takes precedence over env var and DefaultTMDBAPIKey
	dynamicKey = "  user-runtime-key  "
	if got := fDyn.resolveTMDBAPIKey(); got != "user-runtime-key" {
		t.Fatalf("expected dynamic user key override, got %q", got)
	}
	// Clearing dynamic user key + env falls back to DefaultTMDBAPIKey again
	dynamicKey = ""
	t.Setenv(EnvTMDBAPIKeyFallback, "")
	if got := fDyn.resolveTMDBAPIKey(); got != "builtin-default-key" {
		t.Fatalf("expected fallback to DefaultTMDBAPIKey after clear, got %q", got)
	}
}

func TestFetchTMDBTrendingFallbacksAndCustomFetcher(t *testing.T) {
	t.Setenv(EnvTMDBAPIKey, "")
	t.Setenv(EnvTMDBAPIKeyFallback, "")
	t.Setenv(EnvTMDBTrendingURL, "")

	ctx := context.Background()

	// 1. No API key -> built-in curated fallback
	snap, err := NewFetcher(Options{}).FetchTMDBTrending(ctx)
	if err != nil {
		t.Fatalf("no-key fallback failed: %v", err)
	}
	if snap.Source != SourceTMDBTrendingBuiltin || snap.Count < 20 {
		t.Fatalf("expected builtin fallback with >=20 entries, got source=%q count=%d", snap.Source, snap.Count)
	}
	var hasMovie, hasTV bool
	for _, e := range snap.Entries {
		if e.MediaType == "movie" {
			hasMovie = true
		}
		if e.MediaType == "tv" {
			hasTV = true
		}
	}
	if !hasMovie || !hasTV {
		t.Fatalf("expected builtin fallback to contain both movies and tv shows")
	}

	// 2. API key configured, but TMDB API returns 503 -> graceful fallback
	errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer errSrv.Close()
	snapErr, err := NewFetcher(Options{TMDBAPIKey: "k", TMDBTrendingBaseURL: errSrv.URL, HTTPClient: errSrv.Client()}).FetchTMDBTrending(ctx)
	if err != nil || snapErr.Source != SourceTMDBTrendingBuiltin {
		t.Fatalf("expected graceful fallback on 503, got snap=%+v err=%v", snapErr, err)
	}

	// 3. API key configured, but TMDB API returns empty results -> graceful fallback
	emptySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer emptySrv.Close()
	snapEmpty, err := NewFetcher(Options{TMDBAPIKey: "k", TMDBTrendingBaseURL: emptySrv.URL, HTTPClient: emptySrv.Client()}).FetchTMDBTrending(ctx)
	if err != nil || snapEmpty.Source != SourceTMDBTrendingBuiltin {
		t.Fatalf("expected graceful fallback on empty results, got snap=%+v err=%v", snapEmpty, err)
	}

	// 4. Invalid JSON from TMDB -> graceful fallback
	badJSONSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{bad json`))
	}))
	defer badJSONSrv.Close()
	snapBad, err := NewFetcher(Options{TMDBAPIKey: "k", TMDBTrendingBaseURL: badJSONSrv.URL, HTTPClient: badJSONSrv.Client()}).FetchTMDBTrending(ctx)
	if err != nil || snapBad.Source != SourceTMDBTrendingBuiltin {
		t.Fatalf("expected graceful fallback on invalid json, got snap=%+v err=%v", snapBad, err)
	}

	// 5. Custom TMDBTrendingFetcher with empty source defaults to SourceTMDBTrendingAPI
	customSnap, err := NewFetcher(Options{
		TMDBTrendingFetcher: func(ctx context.Context) ([]ChartEntry, string, error) {
			return []ChartEntry{{Rank: 1, Title: "Custom", MediaType: "movie", TMDBID: "1"}}, "", nil
		},
	}).FetchTMDBTrending(ctx)
	if err != nil || customSnap.Source != SourceTMDBTrendingAPI || customSnap.Count != 1 {
		t.Fatalf("unexpected custom fetcher snap: %+v err=%v", customSnap, err)
	}

	// 6. entryDedupKey distinguishes movie vs tv TMDB IDs
	if entryDedupKey(ChartEntry{TMDBID: "100", MediaType: "movie"}) == entryDedupKey(ChartEntry{TMDBID: "100", MediaType: "tv"}) {
		t.Fatal("expected distinct dedup keys for movie and tv with same TMDB ID")
	}
}

func TestTMDBTrendingPersistenceAutoTaggingAndEndpoint(t *testing.T) {
	t.Setenv(EnvTMDBAPIKey, "")
	t.Setenv(EnvTMDBAPIKeyFallback, "")

	store := openTestStore(t)
	repo := sqlite.NewLibraryRepo(store)
	ctx := context.Background()

	lib, err := repo.CreateLibrary(ctx, "Media", "mixed", []string{"/media"})
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}
	y2024 := 2024
	y2021 := 2021
	movie := &sqlite.ItemRecord{
		LibraryID:      lib.ID,
		Type:           "Movie",
		Name:           "Dune 2",
		ProductionYear: &y2024,
		Extra:          sqlite.ItemExtra{ProviderIDs: map[string]string{"tmdb": "693134"}},
	}
	if err := repo.UpsertItem(ctx, movie); err != nil {
		t.Fatalf("UpsertItem movie: %v", err)
	}
	series := &sqlite.ItemRecord{
		LibraryID:      lib.ID,
		Type:           "Series",
		Name:           "鱿鱼游戏",
		OriginalTitle:  "Squid Game",
		ProductionYear: &y2021,
	}
	if err := repo.UpsertItem(ctx, series); err != nil {
		t.Fatalf("UpsertItem series: %v", err)
	}

	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer failSrv.Close()

	tmdbSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/trending/movie/week":
			_, _ = w.Write([]byte(`{"results":[{"id":693134,"title":"沙丘2","original_title":"Dune: Part Two","release_date":"2024-02-27","vote_average":8.2}]}`))
		case "/trending/tv/week":
			_, _ = w.Write([]byte(`{"results":[{"id":93405,"name":"鱿鱼游戏","original_name":"Squid Game","first_air_date":"2021-09-17","vote_average":7.8}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer tmdbSrv.Close()

	// Even when Douban & IMDb fail, live TMDB Trending succeeds -> SyncAndApply succeeds.
	svc := New(store, nil, Options{
		DoubanURL:              failSrv.URL,
		IMDbURL:                failSrv.URL,
		TMDBAPIKey:             "live-key",
		TMDBTrendingBaseURL:    tmdbSrv.URL,
		DoubanTrendingMovieURL: failSrv.URL,
		DoubanTrendingTVURL:    failSrv.URL,
		HTTPClient:             tmdbSrv.Client(),
	})

	// Before sync, TrendingEntries returns empty non-nil slice.
	preEntries, preStatus := svc.TrendingEntries(ctx)
	if preEntries == nil || len(preEntries) != 0 || preStatus.Chart != ChartTMDBTrending {
		t.Fatalf("unexpected pre-sync trending: entries=%v status=%+v", preEntries, preStatus)
	}

	sum, err := svc.SyncAndApply(ctx)
	if err != nil {
		t.Fatalf("expected SyncAndApply to succeed when live TMDB trending succeeds: %v", err)
	}
	// movie ("沙丘2"/tmdb=693134) matches both TMDB Trending and built-in Douban Trending Movie -> Trending, TMDB热门, 豆瓣热门电影 (3 tags)
	// series ("鱿鱼游戏"/tmdb=93405) matches both TMDB Trending and built-in Douban Trending TV -> Trending, TMDB热门, 豆瓣热门剧集 (3 tags)
	if sum.TMDBTrendingCount != 2 || sum.MatchedItems != 2 || sum.TagsAdded != 6 {
		t.Fatalf("unexpected summary: %+v", sum)
	}

	wantMovieTags := TagTMDBTrendingSpecific + "," + TagTMDBTrending + "," + TagDoubanTrendingMovie
	if tags := getItemTags(t, store, movie.ID); strings.Join(tags, ",") != wantMovieTags {
		t.Fatalf("movie tags = %v, want %q", tags, wantMovieTags)
	}
	wantSeriesTags := TagTMDBTrendingSpecific + "," + TagTMDBTrending + "," + TagDoubanTrendingTV
	if tags := getItemTags(t, store, series.ID); strings.Join(tags, ",") != wantSeriesTags {
		t.Fatalf("series tags = %v, want %q", tags, wantSeriesTags)
	}

	// Reload from SQLite in a fresh service and test GET /System/Rankings/Trending.
	svc2 := New(store, nil, Options{})
	if err := svc2.LoadCache(ctx); err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	mux := http.NewServeMux()
	NewHandler(svc2).RegisterRoutes(mux, AuthFuncs{
		RequireAuth: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Auth") == "" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				next.ServeHTTP(w, r)
			})
		},
	})

	// Unauthenticated -> 401
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/System/Rankings/Trending", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}

	// Authenticated -> 200 with status + entries
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/System/Rankings/Trending", nil)
	req.Header.Set("X-Auth", "user")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp TrendingResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode trending response: %v", err)
	}
	if resp.Status.Chart != ChartTMDBTrending || resp.Status.Source != SourceTMDBTrendingAPI || resp.Status.Count != 2 || len(resp.Entries) != 2 {
		t.Fatalf("unexpected trending response: %+v", resp)
	}

	// Corrupted TMDB trending cache in SQLite -> LoadCache returns error.
	_, err = store.Writer.Exec(
		`INSERT OR REPLACE INTO server_config (key, value_json, updated_at) VALUES (?, ?, ?)`,
		ConfigKeyTMDBTrending, "{corrupted", time.Now().Unix(),
	)
	if err != nil {
		t.Fatalf("seed corrupt tmdb config: %v", err)
	}
	if err := New(store, nil, Options{}).LoadCache(ctx); err == nil {
		t.Fatal("expected LoadCache error on corrupted TMDB trending JSON")
	}
}

func TestFetchDoubanTrendingMovieAndTVAndFallbacks(t *testing.T) {
	t.Setenv(EnvDoubanTrendingMovieURL, "")
	t.Setenv(EnvDoubanTrendingTVURL, "")

	var sawMovieReferer, sawTVReferer atomic.Bool
	movieSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Errorf("expected User-Agent header")
		}
		if r.Header.Get("Referer") == "https://movie.douban.com/" {
			sawMovieReferer.Store(true)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"subjects":[
			{"id":"34780991","title":"哪吒之魔童闹海","rate":"8.5","url":"https://movie.douban.com/subject/34780991/"},
			{"id":"34780991","title":"哪吒之魔童闹海","rate":"8.5"},
			{"id":"","title":"好东西 &amp; 朋友","rate":"9.1","url":"https://movie.douban.com/subject/36779837/"},
			{"id":"","title":""}
		]}`))
	}))
	defer movieSrv.Close()

	tvSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") == "https://movie.douban.com/" {
			sawTVReferer.Store(true)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"subjects":[
			{"id":"35503073","title":"山花烂漫时","rate":"9.6","url":"https://movie.douban.com/subject/35503073/"},
			{"id":"35490167","title":"我的阿勒泰","rate":"8.9","url":"https://movie.douban.com/subject/35490167/"}
		]}`))
	}))
	defer tvSrv.Close()

	ctx := context.Background()

	// 1. Live JSON fetch via Options URLs
	f := NewFetcher(Options{
		DoubanTrendingMovieURL: movieSrv.URL,
		DoubanTrendingTVURL:    tvSrv.URL,
		HTTPClient:             movieSrv.Client(),
	})
	mSnap, err := f.FetchDoubanTrendingMovie(ctx)
	if err != nil {
		t.Fatalf("FetchDoubanTrendingMovie failed: %v", err)
	}
	if !sawMovieReferer.Load() {
		t.Fatal("expected Referer header for douban movie request")
	}
	if mSnap.Chart != ChartDoubanTrendingMovie || mSnap.Source != SourceDoubanTrendingMovieAPI || mSnap.Count != 2 {
		t.Fatalf("unexpected movie snap: %+v", mSnap)
	}
	if mSnap.Entries[0].DoubanID != "34780991" || mSnap.Entries[0].Title != "哪吒之魔童闹海" ||
		mSnap.Entries[0].Rating != 8.5 || mSnap.Entries[0].MediaType != "movie" || mSnap.Entries[0].Rank != 1 {
		t.Fatalf("unexpected movie entry[0]: %+v", mSnap.Entries[0])
	}
	if mSnap.Entries[1].DoubanID != "36779837" || mSnap.Entries[1].Title != "好东西 & 朋友" || mSnap.Entries[1].Rank != 2 {
		t.Fatalf("unexpected movie entry[1]: %+v", mSnap.Entries[1])
	}

	tvSnap, err := f.FetchDoubanTrendingTV(ctx)
	if err != nil {
		t.Fatalf("FetchDoubanTrendingTV failed: %v", err)
	}
	if !sawTVReferer.Load() {
		t.Fatal("expected Referer header for douban tv request")
	}
	if tvSnap.Chart != ChartDoubanTrendingTV || tvSnap.Source != SourceDoubanTrendingTVAPI || tvSnap.Count != 2 {
		t.Fatalf("unexpected tv snap: %+v", tvSnap)
	}
	if tvSnap.Entries[0].DoubanID != "35503073" || tvSnap.Entries[0].MediaType != "tv" || tvSnap.Entries[0].Rating != 9.6 {
		t.Fatalf("unexpected tv entry[0]: %+v", tvSnap.Entries[0])
	}

	// 2. Env var and default URL resolution
	t.Setenv(EnvDoubanTrendingMovieURL, movieSrv.URL)
	t.Setenv(EnvDoubanTrendingTVURL, tvSrv.URL)
	fEnv := NewFetcher(Options{})
	if fEnv.resolveDoubanTrendingMovieURL() != movieSrv.URL || fEnv.resolveDoubanTrendingTVURL() != tvSrv.URL {
		t.Fatalf("unexpected env URLs: %s / %s", fEnv.resolveDoubanTrendingMovieURL(), fEnv.resolveDoubanTrendingTVURL())
	}
	t.Setenv(EnvDoubanTrendingMovieURL, "")
	t.Setenv(EnvDoubanTrendingTVURL, "")
	if fEnv.resolveDoubanTrendingMovieURL() != DefaultDoubanTrendingMovieURL ||
		fEnv.resolveDoubanTrendingTVURL() != DefaultDoubanTrendingTVURL {
		t.Fatalf("unexpected default URLs: %s / %s", fEnv.resolveDoubanTrendingMovieURL(), fEnv.resolveDoubanTrendingTVURL())
	}

	// 3. Fallbacks on HTTP 403/503, empty subjects, invalid JSON, and invalid URL
	err403Srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer err403Srv.Close()

	emptySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"subjects":[]}`))
	}))
	defer emptySrv.Close()

	badJSONSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html>blocked by anti-bot</html>`))
	}))
	defer badJSONSrv.Close()

	for name, targetURL := range map[string]string{
		"http403":    err403Srv.URL,
		"empty":      emptySrv.URL,
		"badJSON":    badJSONSrv.URL,
		"invalidURL": "://bad-url",
	} {
		fbM, err := NewFetcher(Options{DoubanTrendingMovieURL: targetURL, HTTPClient: movieSrv.Client()}).FetchDoubanTrendingMovie(ctx)
		if err != nil || fbM.Source != SourceDoubanTrendingMovieBuiltin || fbM.Count < 15 {
			t.Fatalf("%s movie fallback unexpected: snap=%+v err=%v", name, fbM, err)
		}
		fbTV, err := NewFetcher(Options{DoubanTrendingTVURL: targetURL, HTTPClient: movieSrv.Client()}).FetchDoubanTrendingTV(ctx)
		if err != nil || fbTV.Source != SourceDoubanTrendingTVBuiltin || fbTV.Count < 15 {
			t.Fatalf("%s tv fallback unexpected: snap=%+v err=%v", name, fbTV, err)
		}
	}

	// 4. Custom fetchers with blank source default and error path
	customM, err := NewFetcher(Options{
		DoubanTrendingMovieFetcher: func(ctx context.Context) ([]ChartEntry, string, error) {
			return []ChartEntry{{Rank: 1, Title: "Custom Movie", MediaType: "movie", DoubanID: "1"}}, "", nil
		},
		DoubanTrendingTVFetcher: func(ctx context.Context) ([]ChartEntry, string, error) {
			return []ChartEntry{{Rank: 1, Title: "Custom TV", MediaType: "tv", DoubanID: "2"}}, "", nil
		},
	}).FetchDoubanTrendingMovie(ctx)
	if err != nil || customM.Source != SourceDoubanTrendingMovieAPI || customM.Count != 1 {
		t.Fatalf("unexpected custom movie snap: %+v err=%v", customM, err)
	}
	customTV, err := NewFetcher(Options{
		DoubanTrendingTVFetcher: func(ctx context.Context) ([]ChartEntry, string, error) {
			return []ChartEntry{{Rank: 1, Title: "Custom TV", MediaType: "tv", DoubanID: "2"}}, "", nil
		},
	}).FetchDoubanTrendingTV(ctx)
	if err != nil || customTV.Source != SourceDoubanTrendingTVAPI || customTV.Count != 1 {
		t.Fatalf("unexpected custom tv snap: %+v err=%v", customTV, err)
	}
	if _, err := NewFetcher(Options{
		DoubanTrendingMovieFetcher: func(ctx context.Context) ([]ChartEntry, string, error) {
			return nil, "", http.ErrHandlerTimeout
		},
	}).FetchDoubanTrendingMovie(ctx); err == nil {
		t.Fatal("expected error from failing custom movie fetcher")
	}
	if _, err := NewFetcher(Options{
		DoubanTrendingTVFetcher: func(ctx context.Context) ([]ChartEntry, string, error) {
			return nil, "", http.ErrHandlerTimeout
		},
	}).FetchDoubanTrendingTV(ctx); err == nil {
		t.Fatal("expected error from failing custom tv fetcher")
	}
}

func TestDoubanTrendingPersistenceTypedAutoTaggingAndEndpoints(t *testing.T) {
	t.Setenv(EnvTMDBAPIKey, "")
	t.Setenv(EnvTMDBAPIKeyFallback, "")

	store := openTestStore(t)
	repo := sqlite.NewLibraryRepo(store)
	ctx := context.Background()

	lib, err := repo.CreateLibrary(ctx, "Media", "mixed", []string{"/media"})
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}

	y2024 := 2024
	// Movie matching BOTH TMDB Trending and Douban Trending Movie
	movieBoth := &sqlite.ItemRecord{
		LibraryID:      lib.ID,
		Type:           "Movie",
		Name:           "沙丘2",
		OriginalTitle:  "Dune: Part Two",
		ProductionYear: &y2024,
		Extra:          sqlite.ItemExtra{ProviderIDs: map[string]string{"tmdb": "693134", "douban": "35651362"}},
	}
	// Movie matching ONLY Douban Trending Movie (also shares title with a TV entry to verify type isolation)
	movieOnlyDouban := &sqlite.ItemRecord{
		LibraryID:      lib.ID,
		Type:           "Movie",
		Name:           "同名作品",
		ProductionYear: &y2024,
		Extra:          sqlite.ItemExtra{ProviderIDs: map[string]string{"douban": "111111"}},
	}
	// Series matching ONLY Douban Trending TV (shares title "同名作品" with movieOnlyDouban)
	seriesOnlyDouban := &sqlite.ItemRecord{
		LibraryID:      lib.ID,
		Type:           "Series",
		Name:           "同名作品",
		ProductionYear: &y2024,
		Extra:          sqlite.ItemExtra{ProviderIDs: map[string]string{"douban": "222222"}},
	}
	for _, item := range []*sqlite.ItemRecord{movieBoth, movieOnlyDouban, seriesOnlyDouban} {
		if err := repo.UpsertItem(ctx, item); err != nil {
			t.Fatalf("UpsertItem %s: %v", item.Name, err)
		}
	}

	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer failSrv.Close()

	doubanMovieSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"subjects":[
			{"id":"35651362","title":"沙丘2","rate":"8.2"},
			{"id":"111111","title":"同名作品","rate":"8.0"}
		]}`))
	}))
	defer doubanMovieSrv.Close()

	doubanTVSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"subjects":[
			{"id":"222222","title":"同名作品","rate":"8.8"}
		]}`))
	}))
	defer doubanTVSrv.Close()

	svc := New(store, nil, Options{
		DoubanURL:              failSrv.URL,
		IMDbURL:                failSrv.URL,
		DoubanTrendingMovieURL: doubanMovieSrv.URL,
		DoubanTrendingTVURL:    doubanTVSrv.URL,
		HTTPClient:             doubanMovieSrv.Client(),
		TMDBTrendingFetcher: func(ctx context.Context) ([]ChartEntry, string, error) {
			return []ChartEntry{
				{Rank: 1, Title: "沙丘2", OriginalTitle: "Dune: Part Two", Year: 2024, MediaType: "movie", TMDBID: "693134"},
			}, SourceTMDBTrendingAPI, nil
		},
	})

	sum, err := svc.SyncAndApply(ctx)
	if err != nil {
		t.Fatalf("SyncAndApply failed: %v", err)
	}
	if sum.DoubanTrendingMovieCount != 2 || sum.DoubanTrendingTVCount != 1 || sum.TMDBTrendingCount != 1 {
		t.Fatalf("unexpected counts: %+v", sum)
	}
	if sum.MatchedItems != 3 || sum.TagsAdded != 7 {
		t.Fatalf("expected matched=3, tagsAdded=7, got %+v", sum)
	}

	// movieBoth: TMDB热门, Trending, 豆瓣热门电影 (Trending deduplicated!)
	gotBoth := strings.Join(getItemTags(t, store, movieBoth.ID), ",")
	wantBoth := TagTMDBTrendingSpecific + "," + TagTMDBTrending + "," + TagDoubanTrendingMovie
	if gotBoth != wantBoth {
		t.Fatalf("movieBoth tags = %q, want %q", gotBoth, wantBoth)
	}

	// movieOnlyDouban: Trending, 豆瓣热门电影 (must NOT receive 豆瓣热门剧集)
	gotMovie := strings.Join(getItemTags(t, store, movieOnlyDouban.ID), ",")
	wantMovie := TagTMDBTrending + "," + TagDoubanTrendingMovie
	if gotMovie != wantMovie {
		t.Fatalf("movieOnlyDouban tags = %q, want %q", gotMovie, wantMovie)
	}

	// seriesOnlyDouban: Trending, 豆瓣热门剧集 (must NOT receive 豆瓣热门电影)
	gotSeries := strings.Join(getItemTags(t, store, seriesOnlyDouban.ID), ",")
	wantSeries := TagTMDBTrending + "," + TagDoubanTrendingTV
	if gotSeries != wantSeries {
		t.Fatalf("seriesOnlyDouban tags = %q, want %q", gotSeries, wantSeries)
	}

	// Reload from SQLite in a fresh service instance
	svc2 := New(store, nil, Options{})
	if err := svc2.LoadCache(ctx); err != nil {
		t.Fatalf("LoadCache: %v", err)
	}

	// Unspecified type MatchTagsForItem matches both movie & tv charts for "同名作品" (recent release)
	untypedTags := svc2.MatchTagsForItem("同名作品", "", time.Now().Year(), nil)
	wantUntyped := strings.Join([]string{TagTMDBTrending, TagDoubanTrendingMovie, TagDoubanTrendingTV}, ",")
	if strings.Join(untypedTags, ",") != wantUntyped {
		t.Fatalf("untyped MatchTagsForItem = %v, want %q", untypedTags, wantUntyped)
	}
	// Old classic movie with same title and no matching external ID must NOT match dateless Douban Trending
	if oldTags := svc2.MatchTagsForItem("同名作品", "", 1994, nil); len(oldTags) != 0 {
		t.Fatalf("1994 movie without external ID must not match dateless Douban Trending, got %v", oldTags)
	}

	// Verify GET /System/Rankings?includeEntries=true and GET /System/Rankings/Trending
	mux := http.NewServeMux()
	NewHandler(svc2).RegisterRoutes(mux, AuthFuncs{})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/System/Rankings?includeEntries=true", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /System/Rankings status=%d", rec.Code)
	}
	var st StatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("decode StatusResponse: %v", err)
	}
	if st.DoubanTrendingMovie.Chart != ChartDoubanTrendingMovie || st.DoubanTrendingMovie.Count != 2 || len(st.DoubanTrendingMovie.Entries) != 2 {
		t.Fatalf("unexpected DoubanTrendingMovie in StatusResponse: %+v", st.DoubanTrendingMovie)
	}
	if st.DoubanTrendingTV.Chart != ChartDoubanTrendingTV || st.DoubanTrendingTV.Count != 1 || len(st.DoubanTrendingTV.Entries) != 1 {
		t.Fatalf("unexpected DoubanTrendingTV in StatusResponse: %+v", st.DoubanTrendingTV)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/System/Rankings/Trending", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /System/Rankings/Trending status=%d", rec.Code)
	}
	var tr TrendingResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &tr); err != nil {
		t.Fatalf("decode TrendingResponse: %v", err)
	}
	if tr.Status.Chart != ChartTMDBTrending || len(tr.Entries) != 1 ||
		tr.TMDB.Count != 1 || len(tr.TMDB.Entries) != 1 ||
		tr.DoubanMovie.Count != 2 || len(tr.DoubanMovie.Entries) != 2 ||
		tr.DoubanTV.Count != 1 || len(tr.DoubanTV.Entries) != 1 {
		t.Fatalf("unexpected TrendingResponse: %+v", tr)
	}

	// Corrupted Douban trending movie / TV caches in SQLite -> LoadCache returns error
	for _, key := range []string{ConfigKeyDoubanTrendingMovie, ConfigKeyDoubanTrendingTV} {
		corruptStore := openTestStore(t)
		if _, err := corruptStore.Writer.Exec(
			`INSERT INTO server_config (key, value_json, updated_at) VALUES (?, ?, ?)`,
			key, "{corrupted", time.Now().Unix(),
		); err != nil {
			t.Fatalf("seed corrupt %s: %v", key, err)
		}
		if err := New(corruptStore, nil, Options{}).LoadCache(ctx); err == nil {
			t.Fatalf("expected LoadCache error on corrupted %s", key)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}

func TestFetchIMDbTop250BuiltinFallbackAndHeaders(t *testing.T) {
	t.Setenv(EnvIMDb250URL, "")
	t.Setenv(EnvIMDb250MirrorURL, "")
	ctx := context.Background()

	cases := []struct {
		name      string
		transport roundTripFunc
	}{
		{
			name: "aws-waf-202",
			transport: func(r *http.Request) (*http.Response, error) {
				if r.URL.String() == DefaultIMDbTop250URL {
					if r.Header.Get("User-Agent") == "" {
						t.Errorf("expected User-Agent header")
					}
					if r.Header.Get("Accept") == "" {
						t.Errorf("expected Accept header")
					}
					if r.Header.Get("Accept-Language") != "en-US,en;q=0.9" {
						t.Errorf("expected Accept-Language en-US,en;q=0.9, got %q", r.Header.Get("Accept-Language"))
					}
				}
				return &http.Response{
					StatusCode: http.StatusAccepted,
					Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
					Body:       io.NopCloser(strings.NewReader(`<html><body><script src="challenge.js"></script></body></html>`)),
				}, nil
			},
		},
		{
			name: "http-403",
			transport: func(r *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusForbidden,
					Header:     http.Header{},
					Body:       io.NopCloser(strings.NewReader("forbidden")),
				}, nil
			},
		},
		{
			name: "stripped-html-no-jsonld",
			transport: func(r *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"text/html"}},
					Body:       io.NopCloser(strings.NewReader(`<html><body>IMDb Top 250</body></html>`)),
				}, nil
			},
		},
		{
			name: "invalid-json",
			transport: func(r *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{bad json`)),
				}, nil
			},
		},
		{
			name: "empty-json",
			transport: func(r *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`[]`)),
				}, nil
			},
		},
		{
			name: "network-error",
			transport: func(r *http.Request) (*http.Response, error) {
				return nil, errors.New("dial tcp: i/o timeout")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: tc.transport}
			f := NewFetcher(Options{HTTPClient: client})
			snap, err := f.FetchIMDbTop250(ctx)
			if err != nil {
				t.Fatalf("FetchIMDbTop250(%s) returned error: %v", tc.name, err)
			}
			if snap.Chart != ChartIMDbTop250 || snap.Source != SourceIMDbTop250Builtin {
				t.Fatalf("unexpected chart/source: %+v", snap)
			}
			if snap.UpdatedAt.IsZero() {
				t.Fatal("expected non-zero UpdatedAt on fallback snapshot")
			}
			if snap.Count < 100 || len(snap.Entries) != snap.Count {
				t.Fatalf("expected >=100 fallback entries, got count=%d len=%d", snap.Count, len(snap.Entries))
			}

			seenIMDb := make(map[string]struct{}, len(snap.Entries))
			for i, e := range snap.Entries {
				if e.Rank != i+1 {
					t.Fatalf("entry[%d] rank=%d, want %d", i, e.Rank, i+1)
				}
				if e.Title == "" || e.OriginalTitle == "" || e.Year <= 1900 || e.Rating <= 0 {
					t.Fatalf("entry[%d] incomplete metadata: %+v", i, e)
				}
				if !reIMDbID.MatchString(e.IMDbID) {
					t.Fatalf("entry[%d] invalid IMDbID: %q", i, e.IMDbID)
				}
				if _, dup := seenIMDb[e.IMDbID]; dup {
					t.Fatalf("duplicate IMDbID in fallback: %q", e.IMDbID)
				}
				seenIMDb[e.IMDbID] = struct{}{}
			}
		})
	}

	// Custom IMDbURL returning HTTP 202 must still return an error (no silent masking).
	wafSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer wafSrv.Close()
	if _, err := NewFetcher(Options{IMDbURL: wafSrv.URL, HTTPClient: wafSrv.Client()}).FetchIMDbTop250(ctx); err == nil {
		t.Fatal("expected error for custom IMDbURL returning HTTP 202")
	}
}

func TestSyncAndApplyIMDbFallbackAndLivePreservation(t *testing.T) {
	t.Setenv(EnvIMDb250URL, "")
	t.Setenv(EnvTMDBAPIKey, "")
	t.Setenv(EnvTMDBAPIKeyFallback, "")

	store := openTestStore(t)
	repo := sqlite.NewLibraryRepo(store)
	ctx := context.Background()

	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{"/movies"})
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}
	y1994 := 1994
	movie := &sqlite.ItemRecord{
		LibraryID:      lib.ID,
		Type:           "Movie",
		Name:           "肖申克的救赎",
		OriginalTitle:  "The Shawshank Redemption",
		ProductionYear: &y1994,
		Extra:          sqlite.ItemExtra{ProviderIDs: map[string]string{"imdb": "tt0111161"}},
	}
	if err := repo.UpsertItem(ctx, movie); err != nil {
		t.Fatalf("UpsertItem: %v", err)
	}

	var imdbLive atomic.Bool
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			switch {
			case strings.HasPrefix(r.URL.String(), DefaultIMDbTop250URL):
				if imdbLive.Load() {
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": []string{"application/json"}},
						Body: io.NopCloser(strings.NewReader(`[
							{"rank":1,"title":"The Shawshank Redemption","year":1994,"imdb_id":"tt0111161"}
						]`)),
					}, nil
				}
				return &http.Response{
					StatusCode: http.StatusAccepted,
					Header:     http.Header{"Content-Type": []string{"text/html"}},
					Body:       io.NopCloser(strings.NewReader(`<html><body>AWS WAF</body></html>`)),
				}, nil
			case strings.HasPrefix(r.URL.String(), DefaultDoubanTop250URL):
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`[{"rank":1,"title":"肖申克的救赎","year":1994,"douban_id":"1292052"}]`)),
				}, nil
			default:
				return &http.Response{
					StatusCode: http.StatusForbidden,
					Header:     http.Header{},
					Body:       io.NopCloser(strings.NewReader("blocked")),
				}, nil
			}
		}),
	}

	svc := New(store, nil, Options{HTTPClient: client})

	// 1. IMDb returns 202 WAF -> falls back to built-in snapshot, persists to SQLite, tags library.
	sum, err := svc.SyncAndApply(ctx)
	if err != nil {
		t.Fatalf("SyncAndApply with IMDb WAF fallback failed: %v", err)
	}
	if sum.IMDbError != "" || sum.IMDbCount < 100 {
		t.Fatalf("expected IMDb fallback populated without error, got %+v", sum)
	}
	st := svc.Status(false)
	if st.IMDbTop250.Source != SourceIMDbTop250Builtin || st.IMDbTop250.UpdatedAt.IsZero() || st.IMDbTop250.Count != sum.IMDbCount {
		t.Fatalf("unexpected IMDb status after fallback: %+v", st.IMDbTop250)
	}
	tags := getItemTags(t, store, movie.ID)
	if !strings.Contains(strings.Join(tags, ","), TagIMDbTop250) {
		t.Fatalf("expected %q tag on movie, got %v", TagIMDbTop250, tags)
	}

	// 2. Live IMDb succeeds -> replaces builtin fallback with live snapshot.
	imdbLive.Store(true)
	sum2, err := svc.SyncAndApply(ctx)
	if err != nil {
		t.Fatalf("SyncAndApply live IMDb failed: %v", err)
	}
	st2 := svc.Status(false)
	if st2.IMDbTop250.Source != DefaultIMDbTop250URL || sum2.IMDbCount != 1 {
		t.Fatalf("expected live IMDb snapshot, got status=%+v sum=%+v", st2.IMDbTop250, sum2)
	}

	// 3. Subsequent IMDb 202 WAF -> preserves existing live snapshot rather than downgrading.
	imdbLive.Store(false)
	sum3, err := svc.SyncAndApply(ctx)
	if err != nil {
		t.Fatalf("SyncAndApply after live IMDb failed: %v", err)
	}
	st3 := svc.Status(false)
	if st3.IMDbTop250.Source != DefaultIMDbTop250URL || sum3.IMDbCount != 1 {
		t.Fatalf("expected existing live IMDb snapshot preserved, got status=%+v sum=%+v", st3.IMDbTop250, sum3)
	}
}

func TestFetchIMDbTop250ThreeTierFallbackChain(t *testing.T) {
	t.Setenv(EnvIMDb250URL, "")
	t.Setenv(EnvIMDb250MirrorURL, "")
	ctx := context.Background()

	mirrorPayload := `[
		{
			"@type": "Movie",
			"url": "/title/tt0111161/",
			"name": "The Shawshank Redemption",
			"alternateName": "肖申克的救赎",
			"datePublished": "1994-10-14",
			"aggregateRating": {"ratingValue": 9.3}
		},
		{
			"@type": "Movie",
			"url": "https://www.imdb.com/title/TT0068646/",
			"name": "The Godfather &amp; Family",
			"datePublished": "1972-03-24",
			"aggregateRating": {"ratingValue": "9.2"}
		},
		{
			"@type": "Movie",
			"url": "/title/tt0111161/",
			"name": "Duplicate Shawshank",
			"datePublished": "1994-10-14",
			"aggregateRating": {"ratingValue": 9.3}
		}
	]`

	wafSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`<html><body>AWS WAF</body></html>`))
	}))
	defer wafSrv.Close()

	mirror1FailSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer mirror1FailSrv.Close()

	mirrorBadJSONSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{corrupted`))
	}))
	defer mirrorBadJSONSrv.Close()

	mirrorOKSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(mirrorPayload))
	}))
	defer mirrorOKSrv.Close()

	// 1. Tier 1 fails (HTTP 202 WAF) -> Tier 2 mirror succeeds (schema.org JSON array).
	t.Run("tier1-202-tier2-mirror-success", func(t *testing.T) {
		f := NewFetcher(Options{
			IMDbURL:        wafSrv.URL,
			IMDbMirrorURLs: []string{mirrorOKSrv.URL},
			HTTPClient:     wafSrv.Client(),
		})
		snap, err := f.FetchIMDbTop250(ctx)
		if err != nil {
			t.Fatalf("FetchIMDbTop250 failed: %v", err)
		}
		if snap.Chart != ChartIMDbTop250 || snap.Source != SourceIMDbTop250Mirror {
			t.Fatalf("expected SourceIMDbTop250Mirror, got %+v", snap)
		}
		if snap.Count != 2 || len(snap.Entries) != 2 {
			t.Fatalf("expected 2 deduplicated entries, got count=%d len=%d", snap.Count, len(snap.Entries))
		}
		e0 := snap.Entries[0]
		if e0.Rank != 1 || e0.IMDbID != "tt0111161" || e0.Title != "The Shawshank Redemption" ||
			e0.OriginalTitle != "肖申克的救赎" || e0.Year != 1994 || e0.Rating != 9.3 {
			t.Fatalf("unexpected entry[0]: %+v", e0)
		}
		e1 := snap.Entries[1]
		if e1.Rank != 2 || e1.IMDbID != "tt0068646" || e1.Title != "The Godfather & Family" ||
			e1.Year != 1972 || e1.Rating != 9.2 {
			t.Fatalf("unexpected entry[1]: %+v", e1)
		}
	})

	// 2. Tier 1 fails -> Mirror 1 fails (503) & Mirror 2 returns bad JSON -> Mirror 3 succeeds.
	t.Run("tier1-fail-mirror1-503-mirror2-success", func(t *testing.T) {
		f := NewFetcher(Options{
			IMDbURL:        wafSrv.URL,
			IMDbMirrorURLs: []string{mirror1FailSrv.URL, mirrorBadJSONSrv.URL, mirrorOKSrv.URL},
			HTTPClient:     wafSrv.Client(),
		})
		snap, err := f.FetchIMDbTop250(ctx)
		if err != nil {
			t.Fatalf("FetchIMDbTop250 failed: %v", err)
		}
		if snap.Source != SourceIMDbTop250Mirror || snap.Count != 2 {
			t.Fatalf("expected mirror fallback to succeed on second working mirror, got %+v", snap)
		}
	})

	// 3. Tier 1 fails -> All mirrors fail -> Tier 3 builtin fallback succeeds.
	t.Run("tier1-fail-all-mirrors-fail-tier3-builtin", func(t *testing.T) {
		f := NewFetcher(Options{
			IMDbURL:        wafSrv.URL,
			IMDbMirrorURLs: []string{mirror1FailSrv.URL, mirrorBadJSONSrv.URL},
			HTTPClient:     wafSrv.Client(),
		})
		snap, err := f.FetchIMDbTop250(ctx)
		if err != nil {
			t.Fatalf("FetchIMDbTop250 failed: %v", err)
		}
		if snap.Chart != ChartIMDbTop250 || snap.Source != SourceIMDbTop250Builtin || snap.Count < 100 {
			t.Fatalf("expected Tier 3 builtin fallback, got chart=%q source=%q count=%d", snap.Chart, snap.Source, snap.Count)
		}
	})

	// 4. Env var AMBRA_IMDB250_MIRROR_URL (comma-separated) works with default IMDb URL.
	t.Run("env-var-mirror-urls", func(t *testing.T) {
		t.Setenv(EnvIMDb250MirrorURL, mirror1FailSrv.URL+", "+mirrorOKSrv.URL)
		f := NewFetcher(Options{IMDbURL: wafSrv.URL, HTTPClient: wafSrv.Client()})
		snap, err := f.FetchIMDbTop250(ctx)
		if err != nil {
			t.Fatalf("FetchIMDbTop250 via env mirror failed: %v", err)
		}
		if snap.Source != SourceIMDbTop250Mirror || snap.Count != 2 {
			t.Fatalf("expected mirror snapshot via env var, got %+v", snap)
		}
	})

	// 5. Default IMDb URL blocked (202) -> default mirror URL succeeds via custom RoundTripper.
	t.Run("default-url-falls-back-to-default-mirror", func(t *testing.T) {
		var sawDefaultMirror atomic.Bool
		client := &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() == DefaultIMDbTop250URL {
					return &http.Response{
						StatusCode: http.StatusAccepted,
						Header:     http.Header{"Content-Type": []string{"text/html"}},
						Body:       io.NopCloser(strings.NewReader("waf")),
					}, nil
				}
				if r.URL.String() == DefaultIMDbTop250MirrorURLs[0] {
					sawDefaultMirror.Store(true)
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": []string{"application/json"}},
						Body:       io.NopCloser(strings.NewReader(mirrorPayload)),
					}, nil
				}
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Header:     http.Header{},
					Body:       io.NopCloser(strings.NewReader("not found")),
				}, nil
			}),
		}
		f := NewFetcher(Options{HTTPClient: client})
		snap, err := f.FetchIMDbTop250(ctx)
		if err != nil {
			t.Fatalf("FetchIMDbTop250 failed: %v", err)
		}
		if !sawDefaultMirror.Load() {
			t.Fatal("expected request to first default IMDb mirror URL")
		}
		if snap.Source != SourceIMDbTop250Mirror || snap.Count != 2 {
			t.Fatalf("unexpected snapshot: %+v", snap)
		}
	})
}

func TestParseIMDbMirrorJSONEdgeCases(t *testing.T) {
	// Empty payload
	if _, err := parseIMDbMirrorJSON(nil); err == nil {
		t.Fatal("expected error for empty payload")
	}
	// Non-JSON payload
	if _, err := parseIMDbMirrorJSON([]byte(`<html>not json</html>`)); err == nil {
		t.Fatal("expected error for non-JSON payload")
	}
	// Invalid JSON array / object
	if _, err := parseIMDbMirrorJSON([]byte(`[invalid`)); err == nil {
		t.Fatal("expected error for invalid JSON array")
	}
	if _, err := parseIMDbMirrorJSON([]byte(`{invalid`)); err == nil {
		t.Fatal("expected error for invalid JSON object")
	}
	// Missing IMDb IDs -> 0 valid entries -> error
	if _, err := parseIMDbMirrorJSON([]byte(`[{"name":"No ID Movie","datePublished":"2020-01-01"}]`)); err == nil {
		t.Fatal("expected error when all entries lack IMDb ID")
	}

	// {"items":[...]} with ChartEntry-style fields, missing IMDb IDs, and duplicates
	itemsPayload := `{"items":[
		{"id":"tt0468569","title":"The Dark Knight","original_title":"The Dark Knight","year":2008,"rating":9.0,"tmdb_id":"155"},
		{"title":"Missing ID","year":2000},
		{"imdb_id":"TT0468569","title":"Duplicate Dark Knight","year":2008},
		{"imdb_id":"tt0137523","title":"Fight Club","year":1999,"rating":"8.8"}
	]}`
	entries, err := parseIMDbMirrorJSON([]byte(itemsPayload))
	if err != nil {
		t.Fatalf("parseIMDbMirrorJSON items failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 valid deduplicated entries, got %d: %+v", len(entries), entries)
	}
	if entries[0].Rank != 1 || entries[0].IMDbID != "tt0468569" || entries[0].Year != 2008 || entries[0].Rating != 9.0 || entries[0].TMDBID != "155" {
		t.Fatalf("unexpected entry[0]: %+v", entries[0])
	}
	if entries[1].Rank != 2 || entries[1].IMDbID != "tt0137523" || entries[1].Title != "Fight Club" || entries[1].Rating != 8.8 {
		t.Fatalf("unexpected entry[1]: %+v", entries[1])
	}

	// {"entries":[...]} wrapper
	entriesPayload := `{"entries":[{"url":"/title/tt0109830/","name":"Forrest Gump","datePublished":"1994-07-06","aggregateRating":{"ratingValue":8.8}}]}`
	entries2, err := parseIMDbMirrorJSON([]byte(entriesPayload))
	if err != nil || len(entries2) != 1 || entries2[0].IMDbID != "tt0109830" || entries2[0].Year != 1994 || entries2[0].Rating != 8.8 {
		t.Fatalf("unexpected entries wrapper parse: err=%v entries=%+v", err, entries2)
	}
}

func TestStaleDynamicTrendingTagCleanupAndLiveCachePreservation(t *testing.T) {
	store := openTestStore(t)
	repo := sqlite.NewLibraryRepo(store)
	ctx := context.Background()

	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{"/movies"})
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}
	y2024 := 2024
	movie := &sqlite.ItemRecord{
		LibraryID:      lib.ID,
		Type:           "Movie",
		Name:           "沙丘2",
		ProductionYear: &y2024,
		Extra:          sqlite.ItemExtra{ProviderIDs: map[string]string{"tmdb": "693134"}},
	}
	if err := repo.UpsertItem(ctx, movie); err != nil {
		t.Fatalf("UpsertItem: %v", err)
	}
	if err := repo.ReplaceItemTaxonomies(ctx, movie.ID, "tag", []string{"用户自定义"}); err != nil {
		t.Fatalf("ReplaceItemTaxonomies: %v", err)
	}

	var mode atomic.Int32 // 0 = movie on chart, 1 = movie dropped off chart, 2 = builtin fallback returned
	svc := New(store, nil, Options{
		DoubanURL: "http://127.0.0.1:1",
		IMDbURL:   "http://127.0.0.1:1",
		TMDBTrendingFetcher: func(ctx context.Context) ([]ChartEntry, string, error) {
			switch mode.Load() {
			case 0:
				return []ChartEntry{{Rank: 1, Title: "沙丘2", Year: 2024, MediaType: "movie", TMDBID: "693134"}}, SourceTMDBTrendingAPI, nil
			case 1:
				return []ChartEntry{{Rank: 1, Title: "新片", Year: 2025, MediaType: "movie", TMDBID: "999999"}}, SourceTMDBTrendingAPI, nil
			default:
				return []ChartEntry{{Rank: 1, Title: "静态榜单", Year: 2020, MediaType: "movie", TMDBID: "1"}}, SourceTMDBTrendingBuiltin, nil
			}
		},
		DoubanTrendingMovieFetcher: func(ctx context.Context) ([]ChartEntry, string, error) {
			return []ChartEntry{{Rank: 1, Title: "其他电影", Year: 2025, MediaType: "movie", DoubanID: "999"}}, SourceDoubanTrendingMovieAPI, nil
		},
		DoubanTrendingTVFetcher: func(ctx context.Context) ([]ChartEntry, string, error) {
			return []ChartEntry{{Rank: 1, Title: "其他剧集", Year: 2025, MediaType: "tv", DoubanID: "888"}}, SourceDoubanTrendingTVAPI, nil
		},
	})

	if _, err := svc.SyncAndApply(ctx); err != nil {
		t.Fatalf("initial SyncAndApply: %v", err)
	}
	if got := strings.Join(getItemTags(t, store, movie.ID), ","); !strings.Contains(got, TagTMDBTrending) || !strings.Contains(got, TagTMDBTrendingSpecific) {
		t.Fatalf("expected dynamic trending tags, got %q", got)
	}

	// Movie drops off TMDB Trending -> stale dynamic trending tags must be pruned, custom tag kept.
	mode.Store(1)
	if _, err := svc.SyncAndApply(ctx); err != nil {
		t.Fatalf("second SyncAndApply: %v", err)
	}
	if got := strings.Join(getItemTags(t, store, movie.ID), ","); got != "用户自定义" {
		t.Fatalf("expected stale trending tags pruned leaving only custom tag, got %q", got)
	}

	// Builtin fallback must NOT overwrite existing live TMDB Trending cache.
	mode.Store(2)
	if _, err := svc.SyncAndApply(ctx); err != nil {
		t.Fatalf("third SyncAndApply: %v", err)
	}
	st := svc.Status(true)
	if st.TMDBTrending.Source != SourceTMDBTrendingAPI || len(st.TMDBTrending.Entries) != 1 || st.TMDBTrending.Entries[0].TMDBID != "999999" {
		t.Fatalf("expected existing live TMDB trending cache to be preserved, got %+v", st.TMDBTrending)
	}
}

