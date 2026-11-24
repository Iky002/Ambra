package rankings

import (
	"context"
	"encoding/json"
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

	f := NewFetcher(Options{DoubanURL: srv.URL})
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

	f := NewFetcher(Options{IMDbURL: srv.URL})
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

	f := NewFetcher(Options{})
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
	if _, err := NewFetcher(Options{DoubanURL: badHTMLSrv.URL}).FetchDoubanTop250(ctx); err == nil {
		t.Fatal("expected error for empty douban html")
	}
	if _, err := NewFetcher(Options{DoubanURL: badJSONSrv.URL}).FetchDoubanTop250(ctx); err == nil {
		t.Fatal("expected error for invalid douban json")
	}
	if _, err := NewFetcher(Options{DoubanURL: emptyJSONSrv.URL}).FetchDoubanTop250(ctx); err == nil {
		t.Fatal("expected error for empty douban json")
	}
	if _, err := NewFetcher(Options{DoubanURL: err500Srv.URL}).FetchDoubanTop250(ctx); err == nil {
		t.Fatal("expected error for 502 douban response")
	}
	if _, err := NewFetcher(Options{IMDbURL: badHTMLSrv.URL}).FetchIMDbTop250(ctx); err == nil {
		t.Fatal("expected error for imdb html without ld+json")
	}
	if _, err := NewFetcher(Options{IMDbURL: badJSONSrv.URL}).FetchIMDbTop250(ctx); err == nil {
		t.Fatal("expected error for invalid imdb json")
	}
	if _, err := NewFetcher(Options{IMDbURL: emptyJSONSrv.URL}).FetchIMDbTop250(ctx); err == nil {
		t.Fatal("expected error for empty imdb json")
	}
	if _, err := NewFetcher(Options{IMDbURL: err500Srv.URL}).FetchIMDbTop250(ctx); err == nil {
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

	opts := Options{DoubanURL: doubanSrv.URL, IMDbURL: imdbSrv.URL}
	svc := New(store, nil, opts)
	ctx := context.Background()

	// Initial sync populates memory & SQLite server_config.
	summary, err := svc.SyncAndApply(ctx)
	if err != nil {
		t.Fatalf("SyncAndApply failed: %v", err)
	}
	if summary.DoubanCount != 3 || summary.IMDbCount != 2 {
		t.Fatalf("unexpected counts: %+v", summary)
	}

	// Simulate total network failure -> old cache must be preserved.
	failFetches.Store(true)
	failSummary, err := svc.SyncAndApply(ctx)
	if err == nil {
		t.Fatal("expected error when both sources fail")
	}
	if failSummary.DoubanError == "" || failSummary.IMDbError == "" {
		t.Fatalf("expected both errors populated: %+v", failSummary)
	}
	if failSummary.DoubanCount != 3 || failSummary.IMDbCount != 2 {
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

	svc := New(store, bus, Options{DoubanURL: doubanSrv.URL, IMDbURL: imdbSrv.URL})
	summary, err := svc.SyncAndApply(ctx)
	if err != nil {
		t.Fatalf("SyncAndApply: %v", err)
	}
	if summary.MatchedItems != 2 || summary.TagsAdded != 3 {
		t.Fatalf("expected matched=2, tagsAdded=3, got %+v", summary)
	}

	// Verify user custom tags were preserved alongside new ranking tags.
	m1Tags := getItemTags(t, store, movie1.ID)
	wantM1 := "IMDb Top250,我的收藏,经典必看,豆瓣Top250"
	if strings.Join(m1Tags, ",") != wantM1 {
		t.Fatalf("movie1 tags = %q, want %q", strings.Join(m1Tags, ","), wantM1)
	}
	s1Tags := getItemTags(t, store, series1.ID)
	if strings.Join(s1Tags, ",") != TagIMDbTop250 {
		t.Fatalf("series1 tags = %v, want [%s]", s1Tags, TagIMDbTop250)
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
		DoubanURL:    feedSrv.URL,
		IMDbURL:      feedSrv.URL,
		InitialDelay: 10 * time.Millisecond,
		SyncInterval: 35 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	svc.StartPeriodicSync(ctx, 0)
	// Duplicate call should be a no-op while running.
	svc.StartPeriodicSync(ctx, 35*time.Millisecond)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hits.Load() >= 4 { // at least initial sync (2 hits) + 1 periodic tick (2 hits)
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

	svc := New(store, nil, Options{DoubanURL: feedSrv.URL, IMDbURL: feedSrv.URL})
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

