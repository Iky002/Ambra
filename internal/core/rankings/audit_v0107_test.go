package rankings

import (
	"slices"
	"testing"
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
