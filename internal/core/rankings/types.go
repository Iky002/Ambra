package rankings

import (
	"net/http"
	"time"
)

const (
	ChartDoubanTop250 = "douban_top250"
	ChartIMDbTop250   = "imdb_top250"

	ConfigKeyDoubanTop250 = "rankings:douban_top250"
	ConfigKeyIMDbTop250   = "rankings:imdb_top250"

	TagDoubanTop250 = "豆瓣Top250"
	TagIMDbTop250   = "IMDb Top250"

	EnvDouban250URL = "AMBRA_DOUBAN250_URL"
	EnvIMDb250URL   = "AMBRA_IMDB250_URL"

	DefaultDoubanTop250URL = "https://movie.douban.com/top250"
	DefaultIMDbTop250URL   = "https://www.imdb.com/chart/top/"

	DefaultSyncInterval = 24 * time.Hour
	DefaultInitialDelay = 5 * time.Second
	DefaultHTTPTimeout  = 20 * time.Second
)

// ChartEntry represents a single movie or series entry in a ranking chart.
type ChartEntry struct {
	Rank          int     `json:"rank"`
	Title         string  `json:"title"`
	OriginalTitle string  `json:"original_title,omitempty"`
	Year          int     `json:"year,omitempty"`
	Rating        float64 `json:"rating,omitempty"`
	DoubanID      string  `json:"douban_id,omitempty"`
	IMDbID        string  `json:"imdb_id,omitempty"`
	TMDBID        string  `json:"tmdb_id,omitempty"`
}

// ChartSnapshot represents a cached snapshot of a ranking chart.
type ChartSnapshot struct {
	Chart     string       `json:"chart"`
	Source    string       `json:"source"`
	UpdatedAt time.Time    `json:"updated_at"`
	Count     int          `json:"count"`
	Entries   []ChartEntry `json:"entries,omitempty"`
}

// SyncSummary describes the outcome of a SyncAndApply run.
type SyncSummary struct {
	DoubanCount  int       `json:"douban_count"`
	DoubanError  string    `json:"douban_error,omitempty"`
	IMDbCount    int       `json:"imdb_count"`
	IMDbError    string    `json:"imdb_error,omitempty"`
	MatchedItems int       `json:"matched_items"`
	TagsAdded    int       `json:"tags_added"`
	SyncedAt     time.Time `json:"synced_at"`
}

// StatusResponse is returned by GET /System/Rankings.
type StatusResponse struct {
	DoubanTop250 ChartSnapshot `json:"douban_top250"`
	IMDbTop250   ChartSnapshot `json:"imdb_top250"`
}

// Options configures the rankings Service and Fetcher.
type Options struct {
	HTTPClient   *http.Client
	DoubanURL    string
	IMDbURL      string
	UserAgent    string
	SyncInterval time.Duration
	InitialDelay time.Duration
}
