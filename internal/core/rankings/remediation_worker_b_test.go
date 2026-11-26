package rankings

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/store/sqlite"
)

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
