package rankings

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestAuditV0107_M12_NamespaceIsolationAndMovieOnlyTop250(t *testing.T) {
	svc := New(nil, nil, Options{})
	svc.mu.Lock()
	svc.doubanIdx = buildChartIndex([]ChartEntry{
		{Rank: 1, Title: "The Office", Year: 2005, TMDBID: "500", MediaType: "movie"},
	})
	svc.imdbIdx = buildChartIndex([]ChartEntry{
		{Rank: 1, Title: "The Office", Year: 2005, TMDBID: "500", MediaType: "movie"},
	})
	svc.tmdbTrendingIdx = buildChartIndex([]ChartEntry{
		{Rank: 1, Title: "Collision Movie", Year: 2024, TMDBID: "777", MediaType: "movie"},
		{Rank: 2, Title: "Collision TV", Year: 2024, TMDBID: "888", MediaType: "tv"},
	})
	svc.mu.Unlock()

	// Series with matching TMDB ID 777 (which is a MOVIE in TMDB Trending) must NOT match!
	tvCollision := svc.MatchTagsForTypedItem("Series", "Unrelated TV", "", 2024, map[string]string{"Tmdb": "777"})
	if len(tvCollision) != 0 {
		t.Fatalf("TV series should not match movie-namespace TMDB ID 777, got %v", tvCollision)
	}
	// Movie with matching TMDB ID 888 (which is a TV show in TMDB Trending) must NOT match!
	movieCollision := svc.MatchTagsForTypedItem("Movie", "Unrelated Movie", "", 2024, map[string]string{"Tmdb": "888"})
	if len(movieCollision) != 0 {
		t.Fatalf("Movie should not match tv-namespace TMDB ID 888, got %v", movieCollision)
	}
	// Series with same title/year/tmdbID as Douban/IMDb Top 250 movie must NOT receive movie Top 250 tags!
	tvTop250 := svc.MatchTagsForTypedItem("Series", "The Office", "", 2005, map[string]string{"Tmdb": "500"})
	if slices.Contains(tvTop250, TagDoubanTop250) || slices.Contains(tvTop250, TagIMDbTop250) {
		t.Fatalf("Series must not receive DoubanTop250/IMDbTop250 tags, got %v", tvTop250)
	}
	// Movie matches movie-namespace charts cleanly.
	movMatched := svc.MatchTagsForTypedItem("Movie", "Collision Movie", "", 2024, map[string]string{"Tmdb": "777"})
	if !slices.Contains(movMatched, TagTMDBTrendingSpecific) {
		t.Fatalf("expected Movie to match movie-namespace TMDB Trending, got %v", movMatched)
	}
	// TV matches tv-namespace charts cleanly.
	tvMatched := svc.MatchTagsForTypedItem("Series", "Collision TV", "", 2024, map[string]string{"Tmdb": "888"})
	if !slices.Contains(tvMatched, TagTMDBTrendingSpecific) {
		t.Fatalf("expected Series to match tv-namespace TMDB Trending, got %v", tvMatched)
	}
}

func TestAuditV0109_L23_TMDBBearerCheckAndErrorRedaction(t *testing.T) {
	// 1. isTMDBBearerToken requires BOTH eyJ prefix AND 2 dots (JWT).
	if isTMDBBearerToken("eyJNotAJWTWithoutDots") {
		t.Fatal("eyJ prefix without 2 dots must not be treated as JWT bearer token")
	}
	if isTMDBBearerToken("abc.def.ghi") {
		t.Fatal("non-eyJ string with 2 dots must not be treated as JWT bearer token")
	}
	if !isTMDBBearerToken("eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.sig") {
		t.Fatal("valid JWT should be recognized as bearer token")
	}

	// 2. fetchTMDBTrendingPage must redact apiKey from returned transport errors.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closedURL := srv.URL
	srv.Close()

	f := NewFetcher(Options{HTTPClient: srv.Client()})
	secretKey := "super-secret-tmdb-v3-key-98765"
	_, err := f.fetchTMDBTrendingPage(context.Background(), closedURL, "/trending/movie/week", "movie", secretKey)
	if err == nil {
		t.Fatal("expected transport error against closed server")
	}
	if strings.Contains(err.Error(), secretKey) {
		t.Fatalf("TMDB apiKey leaked in error message: %s", err.Error())
	}
}

func TestRankingsSkipsLockedTagsAndDatelessTrendingRequiresRecentYear(t *testing.T) {
	store := openTestStore(t)
	repo := sqlite.NewLibraryRepo(store)
	ctx := context.Background()

	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{"/movies"})
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}

	curYear := time.Now().Year()
	prevYear := curYear - 1
	oldYear := 1994

	recentMovie := &sqlite.ItemRecord{
		LibraryID:      lib.ID,
		Type:           "Movie",
		Name:           "热门同名片",
		ProductionYear: &prevYear,
	}
	classicHomonym := &sqlite.ItemRecord{
		LibraryID:      lib.ID,
		Type:           "Movie",
		Name:           "热门同名片",
		ProductionYear: &oldYear,
	}
	lockedDataMovie := &sqlite.ItemRecord{
		LibraryID:      lib.ID,
		Type:           "Movie",
		Name:           "热门同名片",
		ProductionYear: &curYear,
		Extra:          sqlite.ItemExtra{LockData: true},
	}
	lockedTagsFieldMovie := &sqlite.ItemRecord{
		LibraryID:      lib.ID,
		Type:           "Movie",
		Name:           "热门同名片",
		ProductionYear: &curYear,
		Extra:          sqlite.ItemExtra{LockedFields: []string{"Tags"}},
	}

	for _, rec := range []*sqlite.ItemRecord{recentMovie, classicHomonym, lockedDataMovie, lockedTagsFieldMovie} {
		if err := repo.UpsertItem(ctx, rec); err != nil {
			t.Fatalf("UpsertItem: %v", err)
		}
	}
	// Pre-populate a trending tag on lockedTagsFieldMovie to verify stale pruning also skips locked items.
	if err := repo.ReplaceItemTaxonomies(ctx, lockedTagsFieldMovie.ID, "tag", []string{TagTMDBTrending, "手工标签"}); err != nil {
		t.Fatalf("seed locked tags: %v", err)
	}

	svc := New(store, nil, Options{
		DoubanURL: "http://127.0.0.1:1",
		IMDbURL:   "http://127.0.0.1:1",
		TMDBTrendingFetcher: func(ctx context.Context) ([]ChartEntry, string, error) {
			return []ChartEntry{{Rank: 1, Title: "无关电影", Year: curYear, MediaType: "movie", TMDBID: "999999"}}, SourceTMDBTrendingAPI, nil
		},
		DoubanTrendingMovieFetcher: func(ctx context.Context) ([]ChartEntry, string, error) {
			return []ChartEntry{{Rank: 1, Title: "热门同名片", Year: 0, MediaType: "movie", DoubanID: "88888888"}}, SourceDoubanTrendingMovieAPI, nil
		},
		DoubanTrendingTVFetcher: func(ctx context.Context) ([]ChartEntry, string, error) {
			return []ChartEntry{{Rank: 1, Title: "无关剧集", Year: curYear, MediaType: "tv", DoubanID: "77777777"}}, SourceDoubanTrendingTVAPI, nil
		},
	})

	if _, err := svc.SyncAndApply(ctx); err != nil {
		t.Fatalf("SyncAndApply: %v", err)
	}

	recentTags := getItemTags(t, store, recentMovie.ID)
	if !slices.Contains(recentTags, TagDoubanTrendingMovie) {
		t.Fatalf("expected recent movie (%d) to match dateless Douban Trending, got %v", prevYear, recentTags)
	}
	if oldTags := getItemTags(t, store, classicHomonym.ID); len(oldTags) != 0 {
		t.Fatalf("expected 1994 homonym without external ID NOT to match dateless Douban Trending, got %v", oldTags)
	}
	if ldTags := getItemTags(t, store, lockedDataMovie.ID); len(ldTags) != 0 {
		t.Fatalf("expected LockData movie to be skipped by rankings, got %v", ldTags)
	}

	// Now change chart so lockedTagsFieldMovie is not on chart; its pre-existing Trending tag must NOT be pruned.
	svc2 := New(store, nil, Options{
		DoubanURL: "http://127.0.0.1:1",
		IMDbURL:   "http://127.0.0.1:1",
		TMDBTrendingFetcher: func(ctx context.Context) ([]ChartEntry, string, error) {
			return []ChartEntry{{Rank: 1, Title: "别的电影", Year: curYear, MediaType: "movie", TMDBID: "111111"}}, SourceTMDBTrendingAPI, nil
		},
		DoubanTrendingMovieFetcher: func(ctx context.Context) ([]ChartEntry, string, error) {
			return []ChartEntry{{Rank: 1, Title: "别的电影2", Year: curYear, MediaType: "movie", DoubanID: "222222"}}, SourceDoubanTrendingMovieAPI, nil
		},
		DoubanTrendingTVFetcher: func(ctx context.Context) ([]ChartEntry, string, error) {
			return []ChartEntry{{Rank: 1, Title: "别的剧集", Year: curYear, MediaType: "tv", DoubanID: "333333"}}, SourceDoubanTrendingTVAPI, nil
		},
	})
	if _, err := svc2.SyncAndApply(ctx); err != nil {
		t.Fatalf("second SyncAndApply: %v", err)
	}
	ltTags := strings.Join(getItemTags(t, store, lockedTagsFieldMovie.ID), ",")
	if ltTags != TagTMDBTrending+",手工标签" {
		t.Fatalf("expected LockedFields=[Tags] item tags untouched, got %q", ltTags)
	}
}
