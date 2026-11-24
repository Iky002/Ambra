package rankings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	defaultUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
	doubanPageSize   = 25
	doubanMaxPages   = 10
	maxChartEntries  = 250
	maxResponseBytes = 10 << 20 // 10 MiB
)

var (
	reDoubanSubject = regexp.MustCompile(`/subject/(\d+)/?`)
	reDoubanRank    = regexp.MustCompile(`<em[^>]*>\s*(\d+)\s*</em>`)
	reDoubanTitle   = regexp.MustCompile(`<span\s+class=["']title["'][^>]*>([^<]+)</span>`)
	reDoubanRating  = regexp.MustCompile(`<span\s+class=["']rating_num["'][^>]*>\s*([0-9.]+)\s*</span>`)
	reYear4         = regexp.MustCompile(`\b(19\d{2}|20\d{2})\b`)
	reLDJSONScript  = regexp.MustCompile(`(?is)<script[^>]+type=["']application/ld\+json["'][^>]*>(.*?)</script>`)
	reIMDbID        = regexp.MustCompile(`(?i)\b(tt\d{5,10})\b`)
)

// Fetcher downloads and parses Douban Top 250, IMDb Top 250, TMDB Trending,
// and Douban Trending Movie/TV charts.
type Fetcher struct {
	httpClient                 *http.Client
	doubanURL                  string
	imdbURL                    string
	tmdbAPIKey                 string
	tmdbBaseURL                string
	tmdbTrendingFetcher        func(ctx context.Context) ([]ChartEntry, string, error)
	doubanTrendingMovieURL     string
	doubanTrendingTVURL        string
	doubanTrendingMovieFetcher func(ctx context.Context) ([]ChartEntry, string, error)
	doubanTrendingTVFetcher    func(ctx context.Context) ([]ChartEntry, string, error)
	userAgent                  string
}

// NewFetcher creates a Fetcher with sensible defaults.
func NewFetcher(opts Options) *Fetcher {
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: DefaultHTTPTimeout}
	}
	ua := strings.TrimSpace(opts.UserAgent)
	if ua == "" {
		ua = defaultUserAgent
	}
	return &Fetcher{
		httpClient:                 client,
		doubanURL:                  strings.TrimSpace(opts.DoubanURL),
		imdbURL:                    strings.TrimSpace(opts.IMDbURL),
		tmdbAPIKey:                 strings.TrimSpace(opts.TMDBAPIKey),
		tmdbBaseURL:                strings.TrimRight(strings.TrimSpace(opts.TMDBTrendingBaseURL), "/"),
		tmdbTrendingFetcher:        opts.TMDBTrendingFetcher,
		doubanTrendingMovieURL:     strings.TrimSpace(opts.DoubanTrendingMovieURL),
		doubanTrendingTVURL:        strings.TrimSpace(opts.DoubanTrendingTVURL),
		doubanTrendingMovieFetcher: opts.DoubanTrendingMovieFetcher,
		doubanTrendingTVFetcher:    opts.DoubanTrendingTVFetcher,
		userAgent:                  ua,
	}
}

func (f *Fetcher) resolveDoubanURL() string {
	if f.doubanURL != "" {
		return f.doubanURL
	}
	if v := strings.TrimSpace(os.Getenv(EnvDouban250URL)); v != "" {
		return v
	}
	return DefaultDoubanTop250URL
}

func (f *Fetcher) resolveIMDbURL() string {
	if f.imdbURL != "" {
		return f.imdbURL
	}
	if v := strings.TrimSpace(os.Getenv(EnvIMDb250URL)); v != "" {
		return v
	}
	return DefaultIMDbTop250URL
}

func (f *Fetcher) resolveTMDBAPIKey() string {
	if f.tmdbAPIKey != "" {
		return f.tmdbAPIKey
	}
	if v := strings.TrimSpace(os.Getenv(EnvTMDBAPIKey)); v != "" {
		return v
	}
	return strings.TrimSpace(os.Getenv(EnvTMDBAPIKeyFallback))
}

func (f *Fetcher) resolveTMDBBaseURL() string {
	if f.tmdbBaseURL != "" {
		return f.tmdbBaseURL
	}
	if v := strings.TrimRight(strings.TrimSpace(os.Getenv(EnvTMDBTrendingURL)), "/"); v != "" {
		return v
	}
	return DefaultTMDBBaseURL
}

func (f *Fetcher) resolveDoubanTrendingMovieURL() string {
	if f.doubanTrendingMovieURL != "" {
		return f.doubanTrendingMovieURL
	}
	if v := strings.TrimSpace(os.Getenv(EnvDoubanTrendingMovieURL)); v != "" {
		return v
	}
	return DefaultDoubanTrendingMovieURL
}

func (f *Fetcher) resolveDoubanTrendingTVURL() string {
	if f.doubanTrendingTVURL != "" {
		return f.doubanTrendingTVURL
	}
	if v := strings.TrimSpace(os.Getenv(EnvDoubanTrendingTVURL)); v != "" {
		return v
	}
	return DefaultDoubanTrendingTVURL
}

// FetchDoubanTop250 fetches the Douban Top 250 chart from a custom JSON feed
// or by paginating Douban Top 250 HTML pages (start=0..225).
func (f *Fetcher) FetchDoubanTop250(ctx context.Context) (*ChartSnapshot, error) {
	baseURL := f.resolveDoubanURL()
	body, contentType, err := f.doGet(ctx, baseURL)
	if err != nil {
		return nil, fmt.Errorf("fetch douban top250: %w", err)
	}

	if isJSONPayload(contentType, body) {
		entries, err := parseJSONEntries(body, maxChartEntries)
		if err != nil {
			return nil, fmt.Errorf("parse douban json: %w", err)
		}
		if len(entries) == 0 {
			return nil, errors.New("douban json feed returned 0 entries")
		}
		return &ChartSnapshot{
			Chart:     ChartDoubanTop250,
			Source:    baseURL,
			UpdatedAt: time.Now().UTC(),
			Count:     len(entries),
			Entries:   entries,
		}, nil
	}

	seen := make(map[string]struct{}, maxChartEntries)
	var collected []ChartEntry
	appendUnique := func(pageEntries []ChartEntry) int {
		added := 0
		for _, e := range pageEntries {
			key := entryDedupKey(e)
			if key == "" {
				continue
			}
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			if e.Rank <= 0 {
				e.Rank = len(collected) + 1
			}
			collected = append(collected, e)
			added++
			if len(collected) >= maxChartEntries {
				break
			}
		}
		return added
	}

	firstPage := parseDoubanTop250HTML(body)
	appendUnique(firstPage)

	var lastPageErr error
	// Continue paginating if the first page looks like a full page (or we still need more).
	if len(firstPage) > 0 && len(collected) < maxChartEntries {
		for page := 1; page < doubanMaxPages && len(collected) < maxChartEntries; page++ {
			if ctx.Err() != nil {
				break
			}
			pageURL := appendQueryParam(baseURL, "start", strconv.Itoa(page*doubanPageSize))
			pageBody, _, pageErr := f.doGet(ctx, pageURL)
			if pageErr != nil {
				lastPageErr = pageErr
				continue
			}
			entries := parseDoubanTop250HTML(pageBody)
			if len(entries) == 0 {
				break
			}
			added := appendUnique(entries)
			if added == 0 {
				break
			}
		}
	}

	if len(collected) == 0 {
		if lastPageErr != nil {
			return nil, fmt.Errorf("fetch douban top250 html: %w", lastPageErr)
		}
		return nil, errors.New("parse douban top250 html: no entries found")
	}

	return &ChartSnapshot{
		Chart:     ChartDoubanTop250,
		Source:    baseURL,
		UpdatedAt: time.Now().UTC(),
		Count:     len(collected),
		Entries:   collected,
	}, nil
}

// FetchIMDbTop250 fetches the IMDb Top 250 chart from a custom JSON feed
// or from the IMDb Top 250 HTML page's embedded JSON-LD ItemList.
// When using the default IMDb URL and IMDb blocks server-side scraping
// (e.g. HTTP 202 AWS WAF challenge, non-200 status, network error, or stripped
// HTML without JSON-LD), it gracefully falls back to a curated built-in snapshot.
func (f *Fetcher) FetchIMDbTop250(ctx context.Context) (*ChartSnapshot, error) {
	targetURL := f.resolveIMDbURL()
	useFallback := targetURL == DefaultIMDbTop250URL

	body, contentType, err := f.doGetIMDb(ctx, targetURL)
	if err != nil {
		if useFallback {
			return builtinIMDbTop250Snapshot(), nil
		}
		return nil, fmt.Errorf("fetch imdb top250: %w", err)
	}

	var entries []ChartEntry
	if isJSONPayload(contentType, body) {
		entries, err = parseJSONEntries(body, maxChartEntries)
		if err != nil {
			if useFallback {
				return builtinIMDbTop250Snapshot(), nil
			}
			return nil, fmt.Errorf("parse imdb json: %w", err)
		}
	} else {
		entries, err = parseIMDbTop250HTML(body)
		if err != nil {
			if useFallback {
				return builtinIMDbTop250Snapshot(), nil
			}
			return nil, fmt.Errorf("parse imdb html: %w", err)
		}
	}

	if len(entries) == 0 {
		if useFallback {
			return builtinIMDbTop250Snapshot(), nil
		}
		return nil, errors.New("imdb top250 returned 0 entries")
	}

	return &ChartSnapshot{
		Chart:     ChartIMDbTop250,
		Source:    targetURL,
		UpdatedAt: time.Now().UTC(),
		Count:     len(entries),
		Entries:   entries,
	}, nil
}

// FetchTMDBTrending fetches weekly trending movies and TV shows from TMDB v3 API.
// When no API key is configured or TMDB API is unreachable, it gracefully falls
// back to a curated built-in snapshot so rankings sync never fails solely due to TMDB.
func (f *Fetcher) FetchTMDBTrending(ctx context.Context) (*ChartSnapshot, error) {
	if f.tmdbTrendingFetcher != nil {
		entries, source, err := f.tmdbTrendingFetcher(ctx)
		if err != nil {
			return nil, fmt.Errorf("fetch tmdb trending: %w", err)
		}
		if strings.TrimSpace(source) == "" {
			source = SourceTMDBTrendingAPI
		}
		return &ChartSnapshot{
			Chart:     ChartTMDBTrending,
			Source:    source,
			UpdatedAt: time.Now().UTC(),
			Count:     len(entries),
			Entries:   entries,
		}, nil
	}

	apiKey := f.resolveTMDBAPIKey()
	if apiKey == "" {
		return builtinTMDBTrendingSnapshot(), nil
	}

	baseURL := f.resolveTMDBBaseURL()
	movies, movieErr := f.fetchTMDBTrendingPage(ctx, baseURL, "/trending/movie/week", "movie", apiKey)
	tvShows, tvErr := f.fetchTMDBTrendingPage(ctx, baseURL, "/trending/tv/week", "tv", apiKey)

	if movieErr != nil && tvErr != nil {
		return builtinTMDBTrendingSnapshot(), nil
	}

	merged := mergeTMDBTrending(movies, tvShows)
	if len(merged) == 0 {
		return builtinTMDBTrendingSnapshot(), nil
	}

	return &ChartSnapshot{
		Chart:     ChartTMDBTrending,
		Source:    SourceTMDBTrendingAPI,
		UpdatedAt: time.Now().UTC(),
		Count:     len(merged),
		Entries:   merged,
	}, nil
}

type tmdbTrendingPayload struct {
	Results []struct {
		ID            int     `json:"id"`
		Title         string  `json:"title"`
		OriginalTitle string  `json:"original_title"`
		Name          string  `json:"name"`
		OriginalName  string  `json:"original_name"`
		ReleaseDate   string  `json:"release_date"`
		FirstAirDate  string  `json:"first_air_date"`
		Overview      string  `json:"overview"`
		VoteAverage   float64 `json:"vote_average"`
		MediaType     string  `json:"media_type"`
	} `json:"results"`
}

func isTMDBBearerToken(apiKey string) bool {
	return strings.HasPrefix(apiKey, "eyJ") || strings.Count(apiKey, ".") == 2
}

func (f *Fetcher) fetchTMDBTrendingPage(ctx context.Context, baseURL, endpoint, defaultMediaType, apiKey string) ([]ChartEntry, error) {
	params := url.Values{}
	params.Set("language", "zh-CN")
	params.Set("page", "1")
	useBearer := isTMDBBearerToken(apiKey)
	if !useBearer {
		params.Set("api_key", apiKey)
	}

	reqURL := baseURL + endpoint + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", f.userAgent)
	req.Header.Set("Accept", "application/json")
	if useBearer {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("tmdb trending %s status %d", endpoint, resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, err
	}

	var payload tmdbTrendingPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("parse tmdb trending %s: %w", endpoint, err)
	}

	entries := make([]ChartEntry, 0, len(payload.Results))
	for _, r := range payload.Results {
		title := strings.TrimSpace(r.Title)
		if title == "" {
			title = strings.TrimSpace(r.Name)
		}
		origTitle := strings.TrimSpace(r.OriginalTitle)
		if origTitle == "" {
			origTitle = strings.TrimSpace(r.OriginalName)
		}
		if title == "" && origTitle == "" && r.ID <= 0 {
			continue
		}
		mediaType := strings.TrimSpace(r.MediaType)
		if mediaType == "" {
			mediaType = defaultMediaType
		}
		dateStr := r.ReleaseDate
		if dateStr == "" {
			dateStr = r.FirstAirDate
		}
		year := 0
		if len(dateStr) >= 4 {
			year, _ = strconv.Atoi(dateStr[:4])
		}
		tmdbID := ""
		if r.ID > 0 {
			tmdbID = strconv.Itoa(r.ID)
		}
		entries = append(entries, ChartEntry{
			Title:         title,
			OriginalTitle: origTitle,
			Year:          year,
			Rating:        r.VoteAverage,
			MediaType:     mediaType,
			Overview:      strings.TrimSpace(r.Overview),
			TMDBID:        tmdbID,
		})
	}
	return entries, nil
}

func mergeTMDBTrending(movies, tvShows []ChartEntry) []ChartEntry {
	seen := make(map[string]struct{}, len(movies)+len(tvShows))
	out := make([]ChartEntry, 0, len(movies)+len(tvShows))
	appendUnique := func(list []ChartEntry) {
		for _, e := range list {
			key := entryDedupKey(e)
			if key == "" {
				continue
			}
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			e.Rank = len(out) + 1
			out = append(out, e)
		}
	}
	appendUnique(movies)
	appendUnique(tvShows)
	return out
}

func builtinTMDBTrendingSnapshot() *ChartSnapshot {
	entries := builtinTMDBTrendingEntries()
	return &ChartSnapshot{
		Chart:     ChartTMDBTrending,
		Source:    SourceTMDBTrendingBuiltin,
		UpdatedAt: time.Now().UTC(),
		Count:     len(entries),
		Entries:   entries,
	}
}

func builtinTMDBTrendingEntries() []ChartEntry {
	return []ChartEntry{
		{Rank: 1, Title: "沙丘2", OriginalTitle: "Dune: Part Two", Year: 2024, Rating: 8.2, MediaType: "movie", TMDBID: "693134"},
		{Rank: 2, Title: "奥本海默", OriginalTitle: "Oppenheimer", Year: 2023, Rating: 8.1, MediaType: "movie", TMDBID: "872585"},
		{Rank: 3, Title: "死侍与金刚狼", OriginalTitle: "Deadpool & Wolverine", Year: 2024, Rating: 7.7, MediaType: "movie", TMDBID: "533535"},
		{Rank: 4, Title: "头脑特工队2", OriginalTitle: "Inside Out 2", Year: 2024, Rating: 7.6, MediaType: "movie", TMDBID: "1022789"},
		{Rank: 5, Title: "荒野机器人", OriginalTitle: "The Wild Robot", Year: 2024, Rating: 8.4, MediaType: "movie", TMDBID: "1184918"},
		{Rank: 6, Title: "哪吒之魔童闹海", OriginalTitle: "Ne Zha 2", Year: 2025, Rating: 8.1, MediaType: "movie", TMDBID: "980477"},
		{Rank: 7, Title: "流浪地球2", OriginalTitle: "The Wandering Earth II", Year: 2023, Rating: 7.6, MediaType: "movie", TMDBID: "842675"},
		{Rank: 8, Title: "蜘蛛侠：纵横宇宙", OriginalTitle: "Spider-Man: Across the Spider-Verse", Year: 2023, Rating: 8.4, MediaType: "movie", TMDBID: "569094"},
		{Rank: 9, Title: "瞬息全宇宙", OriginalTitle: "Everything Everywhere All at Once", Year: 2022, Rating: 7.8, MediaType: "movie", TMDBID: "545611"},
		{Rank: 10, Title: "星际穿越", OriginalTitle: "Interstellar", Year: 2014, Rating: 8.4, MediaType: "movie", TMDBID: "157336"},
		{Rank: 11, Title: "盗梦空间", OriginalTitle: "Inception", Year: 2010, Rating: 8.4, MediaType: "movie", TMDBID: "27205"},
		{Rank: 12, Title: "蝙蝠侠：黑暗骑士", OriginalTitle: "The Dark Knight", Year: 2008, Rating: 8.5, MediaType: "movie", TMDBID: "155"},
		{Rank: 13, Title: "肖申克的救赎", OriginalTitle: "The Shawshank Redemption", Year: 1994, Rating: 8.7, MediaType: "movie", TMDBID: "278"},
		{Rank: 14, Title: "千与千寻", OriginalTitle: "Spirited Away", Year: 2001, Rating: 8.5, MediaType: "movie", TMDBID: "129"},
		{Rank: 15, Title: "阿凡达：水之道", OriginalTitle: "Avatar: The Way of Water", Year: 2022, Rating: 7.6, MediaType: "movie", TMDBID: "76600"},
		{Rank: 16, Title: "鱿鱼游戏", OriginalTitle: "Squid Game", Year: 2021, Rating: 7.8, MediaType: "tv", TMDBID: "93405"},
		{Rank: 17, Title: "怪奇物语", OriginalTitle: "Stranger Things", Year: 2016, Rating: 8.6, MediaType: "tv", TMDBID: "66732"},
		{Rank: 18, Title: "人生切割术", OriginalTitle: "Severance", Year: 2022, Rating: 8.4, MediaType: "tv", TMDBID: "95396"},
		{Rank: 19, Title: "最后生还者", OriginalTitle: "The Last of Us", Year: 2023, Rating: 8.6, MediaType: "tv", TMDBID: "100088"},
		{Rank: 20, Title: "双城之战", OriginalTitle: "Arcane", Year: 2021, Rating: 8.7, MediaType: "tv", TMDBID: "94605"},
		{Rank: 21, Title: "幕府将军", OriginalTitle: "Shōgun", Year: 2024, Rating: 8.5, MediaType: "tv", TMDBID: "126308"},
		{Rank: 22, Title: "辐射", OriginalTitle: "Fallout", Year: 2024, Rating: 8.3, MediaType: "tv", TMDBID: "106379"},
		{Rank: 23, Title: "三体", OriginalTitle: "3 Body Problem", Year: 2024, Rating: 7.5, MediaType: "tv", TMDBID: "108545"},
		{Rank: 24, Title: "绝命毒师", OriginalTitle: "Breaking Bad", Year: 2008, Rating: 8.9, MediaType: "tv", TMDBID: "1396"},
		{Rank: 25, Title: "权力的游戏", OriginalTitle: "Game of Thrones", Year: 2011, Rating: 8.4, MediaType: "tv", TMDBID: "1399"},
		{Rank: 26, Title: "进击的巨人", OriginalTitle: "Attack on Titan", Year: 2013, Rating: 8.7, MediaType: "tv", TMDBID: "1429"},
	}
}

type doubanTrendingPayload struct {
	Subjects []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Rate  string `json:"rate"`
		URL   string `json:"url"`
	} `json:"subjects"`
}

// FetchDoubanTrendingMovie fetches Douban's trending movies chart from search_subjects JSON.
// On network failure, anti-bot blocking, or empty results, it falls back to a curated built-in snapshot.
func (f *Fetcher) FetchDoubanTrendingMovie(ctx context.Context) (*ChartSnapshot, error) {
	if f.doubanTrendingMovieFetcher != nil {
		entries, source, err := f.doubanTrendingMovieFetcher(ctx)
		if err != nil {
			return nil, fmt.Errorf("fetch douban trending movie: %w", err)
		}
		if strings.TrimSpace(source) == "" {
			source = SourceDoubanTrendingMovieAPI
		}
		return &ChartSnapshot{
			Chart:     ChartDoubanTrendingMovie,
			Source:    source,
			UpdatedAt: time.Now().UTC(),
			Count:     len(entries),
			Entries:   entries,
		}, nil
	}
	return f.fetchDoubanTrending(
		ctx,
		f.resolveDoubanTrendingMovieURL(),
		ChartDoubanTrendingMovie,
		"movie",
		SourceDoubanTrendingMovieAPI,
		builtinDoubanTrendingMovieSnapshot,
	), nil
}

// FetchDoubanTrendingTV fetches Douban's trending TV series chart from search_subjects JSON.
// On network failure, anti-bot blocking, or empty results, it falls back to a curated built-in snapshot.
func (f *Fetcher) FetchDoubanTrendingTV(ctx context.Context) (*ChartSnapshot, error) {
	if f.doubanTrendingTVFetcher != nil {
		entries, source, err := f.doubanTrendingTVFetcher(ctx)
		if err != nil {
			return nil, fmt.Errorf("fetch douban trending tv: %w", err)
		}
		if strings.TrimSpace(source) == "" {
			source = SourceDoubanTrendingTVAPI
		}
		return &ChartSnapshot{
			Chart:     ChartDoubanTrendingTV,
			Source:    source,
			UpdatedAt: time.Now().UTC(),
			Count:     len(entries),
			Entries:   entries,
		}, nil
	}
	return f.fetchDoubanTrending(
		ctx,
		f.resolveDoubanTrendingTVURL(),
		ChartDoubanTrendingTV,
		"tv",
		SourceDoubanTrendingTVAPI,
		builtinDoubanTrendingTVSnapshot,
	), nil
}

func (f *Fetcher) fetchDoubanTrending(
	ctx context.Context,
	targetURL string,
	chart string,
	mediaType string,
	apiSource string,
	fallback func() *ChartSnapshot,
) *ChartSnapshot {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return fallback()
	}
	req.Header.Set("User-Agent", f.userAgent)
	req.Header.Set("Referer", "https://movie.douban.com/")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return fallback()
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fallback()
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fallback()
	}

	var payload doubanTrendingPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return fallback()
	}

	seen := make(map[string]struct{}, len(payload.Subjects))
	entries := make([]ChartEntry, 0, len(payload.Subjects))
	for _, subj := range payload.Subjects {
		title := strings.TrimSpace(html.UnescapeString(subj.Title))
		doubanID := strings.TrimSpace(subj.ID)
		if doubanID == "" && subj.URL != "" {
			if m := reDoubanSubject.FindStringSubmatch(subj.URL); len(m) > 1 {
				doubanID = m[1]
			}
		}
		if title == "" && doubanID == "" {
			continue
		}
		rating, _ := strconv.ParseFloat(strings.TrimSpace(subj.Rate), 64)
		entry := ChartEntry{
			Title:     title,
			Rating:    rating,
			MediaType: mediaType,
			DoubanID:  doubanID,
		}
		key := entryDedupKey(entry)
		if key == "" {
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		entry.Rank = len(entries) + 1
		entries = append(entries, entry)
	}

	if len(entries) == 0 {
		return fallback()
	}

	return &ChartSnapshot{
		Chart:     chart,
		Source:    apiSource,
		UpdatedAt: time.Now().UTC(),
		Count:     len(entries),
		Entries:   entries,
	}
}

func builtinDoubanTrendingMovieSnapshot() *ChartSnapshot {
	entries := builtinDoubanTrendingMovieEntries()
	return &ChartSnapshot{
		Chart:     ChartDoubanTrendingMovie,
		Source:    SourceDoubanTrendingMovieBuiltin,
		UpdatedAt: time.Now().UTC(),
		Count:     len(entries),
		Entries:   entries,
	}
}

func builtinDoubanTrendingMovieEntries() []ChartEntry {
	return []ChartEntry{
		{Rank: 1, Title: "哪吒之魔童闹海", OriginalTitle: "Ne Zha 2", Year: 2025, Rating: 8.5, MediaType: "movie", DoubanID: "34780991", TMDBID: "980477"},
		{Rank: 2, Title: "好东西", OriginalTitle: "Her Story", Year: 2024, Rating: 9.1, MediaType: "movie", DoubanID: "36779837", TMDBID: "1231574"},
		{Rank: 3, Title: "破·地狱", OriginalTitle: "The Last Dance", Year: 2024, Rating: 8.4, MediaType: "movie", DoubanID: "36712987", TMDBID: "1247019"},
		{Rank: 4, Title: "沙丘2", OriginalTitle: "Dune: Part Two", Year: 2024, Rating: 8.2, MediaType: "movie", DoubanID: "35651362", TMDBID: "693134"},
		{Rank: 5, Title: "荒野机器人", OriginalTitle: "The Wild Robot", Year: 2024, Rating: 8.1, MediaType: "movie", DoubanID: "36580597", TMDBID: "1184918"},
		{Rank: 6, Title: "出走的决心", OriginalTitle: "Like a Rolling Stone", Year: 2024, Rating: 8.8, MediaType: "movie", DoubanID: "36445098", TMDBID: "1312806"},
		{Rank: 7, Title: "九龙城寨之围城", OriginalTitle: "Twilight of the Warriors: Walled In", Year: 2024, Rating: 7.3, MediaType: "movie", DoubanID: "35295960", TMDBID: "923667"},
		{Rank: 8, Title: "周处除三害", OriginalTitle: "The Pig, the Snake and the Pigeon", Year: 2023, Rating: 8.1, MediaType: "movie", DoubanID: "36151692", TMDBID: "1046090"},
		{Rank: 9, Title: "奥本海默", OriginalTitle: "Oppenheimer", Year: 2023, Rating: 8.8, MediaType: "movie", DoubanID: "35593344", TMDBID: "872585"},
		{Rank: 10, Title: "抓娃娃", OriginalTitle: "Successor", Year: 2024, Rating: 7.3, MediaType: "movie", DoubanID: "36438475", TMDBID: "1255350"},
		{Rank: 11, Title: "头脑特工队2", OriginalTitle: "Inside Out 2", Year: 2024, Rating: 8.4, MediaType: "movie", DoubanID: "36087474", TMDBID: "1022789"},
		{Rank: 12, Title: "坠落的审判", OriginalTitle: "Anatomy of a Fall", Year: 2023, Rating: 8.7, MediaType: "movie", DoubanID: "35633650", TMDBID: "915935"},
		{Rank: 13, Title: "流浪地球2", OriginalTitle: "The Wandering Earth II", Year: 2023, Rating: 8.3, MediaType: "movie", DoubanID: "35267208", TMDBID: "842675"},
		{Rank: 14, Title: "长安三万里", OriginalTitle: "Chang An", Year: 2023, Rating: 8.3, MediaType: "movie", DoubanID: "36035676", TMDBID: "1108427"},
		{Rank: 15, Title: "封神第一部：朝歌风云", OriginalTitle: "Creation of the Gods I: Kingdom of Storms", Year: 2023, Rating: 7.8, MediaType: "movie", DoubanID: "10604086", TMDBID: "856289"},
		{Rank: 16, Title: "热辣滚烫", OriginalTitle: "YOLO", Year: 2024, Rating: 7.5, MediaType: "movie", DoubanID: "36081094", TMDBID: "1087388"},
		{Rank: 17, Title: "飞驰人生2", OriginalTitle: "Pegasus 2", Year: 2024, Rating: 7.6, MediaType: "movie", DoubanID: "36369452", TMDBID: "1154864"},
		{Rank: 18, Title: "第二十条", OriginalTitle: "Article 20", Year: 2024, Rating: 7.5, MediaType: "movie", DoubanID: "36208094", TMDBID: "1156483"},
	}
}

func builtinDoubanTrendingTVSnapshot() *ChartSnapshot {
	entries := builtinDoubanTrendingTVEntries()
	return &ChartSnapshot{
		Chart:     ChartDoubanTrendingTV,
		Source:    SourceDoubanTrendingTVBuiltin,
		UpdatedAt: time.Now().UTC(),
		Count:     len(entries),
		Entries:   entries,
	}
}

func builtinDoubanTrendingTVEntries() []ChartEntry {
	return []ChartEntry{
		{Rank: 1, Title: "山花烂漫时", OriginalTitle: "She and Her Girls", Year: 2024, Rating: 9.6, MediaType: "tv", DoubanID: "35503073", TMDBID: "270778"},
		{Rank: 2, Title: "我的阿勒泰", OriginalTitle: "To the Wonder", Year: 2024, Rating: 8.9, MediaType: "tv", DoubanID: "35490167", TMDBID: "253905"},
		{Rank: 3, Title: "漫长的季节", OriginalTitle: "The Long Season", Year: 2023, Rating: 9.4, MediaType: "tv", DoubanID: "35426925", TMDBID: "225171"},
		{Rank: 4, Title: "繁花", OriginalTitle: "Blossoms Shanghai", Year: 2023, Rating: 8.7, MediaType: "tv", DoubanID: "34874432", TMDBID: "114410"},
		{Rank: 5, Title: "庆余年 第二季", OriginalTitle: "Joy of Life 2", Year: 2024, Rating: 7.3, MediaType: "tv", DoubanID: "34937650", TMDBID: "95922"},
		{Rank: 6, Title: "唐朝诡事录之西行", OriginalTitle: "Strange Tales of Tang Dynasty II", Year: 2024, Rating: 8.6, MediaType: "tv", DoubanID: "36165752", TMDBID: "209551"},
		{Rank: 7, Title: "三体", OriginalTitle: "Three-Body", Year: 2023, Rating: 8.7, MediaType: "tv", DoubanID: "26647087", TMDBID: "106449"},
		{Rank: 8, Title: "狂飙", OriginalTitle: "The Knockout", Year: 2023, Rating: 8.5, MediaType: "tv", DoubanID: "35465232", TMDBID: "218145"},
		{Rank: 9, Title: "边水往事", OriginalTitle: "Escape from the Trilateral Slopes", Year: 2024, Rating: 7.9, MediaType: "tv", DoubanID: "35743125", TMDBID: "261677"},
		{Rank: 10, Title: "玫瑰的故事", OriginalTitle: "The Tale of Rose", Year: 2024, Rating: 7.4, MediaType: "tv", DoubanID: "36163414", TMDBID: "228095"},
		{Rank: 11, Title: "永夜星河", OriginalTitle: "Love Game in Eastern Fantasy", Year: 2024, Rating: 7.5, MediaType: "tv", DoubanID: "35733555", TMDBID: "232098"},
		{Rank: 12, Title: "九重紫", OriginalTitle: "Blossom", Year: 2024, Rating: 7.8, MediaType: "tv", DoubanID: "36283119", TMDBID: "248358"},
		{Rank: 13, Title: "人生切割术", OriginalTitle: "Severance", Year: 2022, Rating: 9.1, MediaType: "tv", DoubanID: "34890502", TMDBID: "95396"},
		{Rank: 14, Title: "幕府将军", OriginalTitle: "Shōgun", Year: 2024, Rating: 8.1, MediaType: "tv", DoubanID: "30290915", TMDBID: "126308"},
		{Rank: 15, Title: "鱿鱼游戏", OriginalTitle: "Squid Game", Year: 2021, Rating: 7.7, MediaType: "tv", DoubanID: "34812928", TMDBID: "93405"},
		{Rank: 16, Title: "双城之战", OriginalTitle: "Arcane", Year: 2021, Rating: 9.0, MediaType: "tv", DoubanID: "34867871", TMDBID: "94605"},
		{Rank: 17, Title: "葬送的芙莉莲", OriginalTitle: "Frieren: Beyond Journey's End", Year: 2023, Rating: 9.5, MediaType: "tv", DoubanID: "36087424", TMDBID: "209867"},
		{Rank: 18, Title: "小巷人家", OriginalTitle: "Romance in the Alley", Year: 2024, Rating: 8.3, MediaType: "tv", DoubanID: "36328496", TMDBID: "259373"},
	}
}

func (f *Fetcher) doGet(ctx context.Context, rawURL string) ([]byte, string, error) {
	return f.doGetWithHeaders(ctx, rawURL, "text/html,application/xhtml+xml,application/json;q=0.9,*/*;q=0.8", "zh-CN,zh;q=0.9,en;q=0.8", false)
}

func (f *Fetcher) doGetIMDb(ctx context.Context, rawURL string) ([]byte, string, error) {
	return f.doGetWithHeaders(ctx, rawURL, "text/html,application/xhtml+xml,application/xml;q=0.9,application/json;q=0.8,*/*;q=0.7", "en-US,en;q=0.9", true)
}

func (f *Fetcher) doGetWithHeaders(ctx context.Context, rawURL, accept, acceptLang string, strict200 bool) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", f.userAgent)
	req.Header.Set("Accept", accept)
	req.Header.Set("Accept-Language", acceptLang)

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if strict200 {
		if resp.StatusCode != http.StatusOK {
			return nil, resp.Header.Get("Content-Type"), fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)
		}
	} else if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.Header.Get("Content-Type"), fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, resp.Header.Get("Content-Type"), err
	}
	return data, resp.Header.Get("Content-Type"), nil
}

func isJSONPayload(contentType string, body []byte) bool {
	if strings.Contains(strings.ToLower(contentType), "application/json") {
		return true
	}
	trimmed := bytes.TrimSpace(body)
	return len(trimmed) > 0 && (trimmed[0] == '[' || trimmed[0] == '{')
}

func appendQueryParam(rawURL, key, val string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		sep := "?"
		if strings.Contains(rawURL, "?") {
			sep = "&"
		}
		return rawURL + sep + key + "=" + val
	}
	q := u.Query()
	q.Set(key, val)
	u.RawQuery = q.Encode()
	return u.String()
}

func parseJSONEntries(body []byte, limit int) ([]ChartEntry, error) {
	trimmed := bytes.TrimSpace(body)
	var rawEntries []ChartEntry

	if len(trimmed) == 0 {
		return nil, errors.New("empty json payload")
	}
	if trimmed[0] == '[' {
		if err := json.Unmarshal(trimmed, &rawEntries); err != nil {
			return nil, err
		}
	} else {
		var wrapper struct {
			Entries []ChartEntry `json:"entries"`
			Items   []ChartEntry `json:"items"`
		}
		if err := json.Unmarshal(trimmed, &wrapper); err != nil {
			return nil, err
		}
		if len(wrapper.Entries) > 0 {
			rawEntries = wrapper.Entries
		} else {
			rawEntries = wrapper.Items
		}
	}

	seen := make(map[string]struct{}, len(rawEntries))
	out := make([]ChartEntry, 0, min(len(rawEntries), limit))
	for _, e := range rawEntries {
		e.Title = strings.TrimSpace(e.Title)
		e.OriginalTitle = strings.TrimSpace(e.OriginalTitle)
		e.DoubanID = strings.TrimSpace(e.DoubanID)
		e.IMDbID = normalizeIMDbID(e.IMDbID)
		e.TMDBID = strings.TrimSpace(e.TMDBID)
		if e.Title == "" && e.OriginalTitle == "" && e.IMDbID == "" && e.DoubanID == "" && e.TMDBID == "" {
			continue
		}
		key := entryDedupKey(e)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		if e.Rank <= 0 {
			e.Rank = len(out) + 1
		}
		out = append(out, e)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func parseDoubanTop250HTML(body []byte) []ChartEntry {
	s := string(body)
	parts := strings.Split(s, `<div class="item">`)
	if len(parts) <= 1 {
		return nil
	}

	entries := make([]ChartEntry, 0, len(parts)-1)
	for _, block := range parts[1:] {
		var entry ChartEntry
		if m := reDoubanSubject.FindStringSubmatch(block); len(m) > 1 {
			entry.DoubanID = m[1]
		}
		if m := reDoubanRank.FindStringSubmatch(block); len(m) > 1 {
			entry.Rank, _ = strconv.Atoi(m[1])
		}
		titleMatches := reDoubanTitle.FindAllStringSubmatch(block, 2)
		if len(titleMatches) > 0 {
			entry.Title = cleanDoubanTitle(titleMatches[0][1])
		}
		if len(titleMatches) > 1 {
			entry.OriginalTitle = cleanDoubanTitle(titleMatches[1][1])
		}
		if m := reDoubanRating.FindStringSubmatch(block); len(m) > 1 {
			entry.Rating, _ = strconv.ParseFloat(m[1], 64)
		}
		// Extract 4-digit year from the metadata block (<div class="bd">...</div>).
		searchRegion := block
		if idx := strings.Index(block, `class="bd"`); idx >= 0 {
			searchRegion = block[idx:]
		}
		if m := reYear4.FindStringSubmatch(searchRegion); len(m) > 1 {
			entry.Year, _ = strconv.Atoi(m[1])
		}
		if entry.Title == "" && entry.DoubanID == "" {
			continue
		}
		entries = append(entries, entry)
	}
	return entries
}

func cleanDoubanTitle(raw string) string {
	s := html.UnescapeString(raw)
	s = strings.ReplaceAll(s, "\u00a0", " ")
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "/")
	return strings.TrimSpace(s)
}

type imdbLDItemList struct {
	Type            string `json:"@type"`
	ItemListElement []struct {
		Position int `json:"position"`
		URL      string `json:"url"`
		Name     string `json:"name"`
		Item     *struct {
			URL             string `json:"url"`
			Name            string `json:"name"`
			AlternateName   string `json:"alternateName"`
			DatePublished   string `json:"datePublished"`
			AggregateRating *struct {
				RatingValue any `json:"ratingValue"`
			} `json:"aggregateRating"`
		} `json:"item"`
		AlternateName   string `json:"alternateName"`
		DatePublished   string `json:"datePublished"`
		AggregateRating *struct {
			RatingValue any `json:"ratingValue"`
		} `json:"aggregateRating"`
	} `json:"itemListElement"`
}

func parseIMDbTop250HTML(body []byte) ([]ChartEntry, error) {
	matches := reLDJSONScript.FindAllSubmatch(body, -1)
	if len(matches) == 0 {
		return nil, errors.New("no application/ld+json script block found")
	}

	seen := make(map[string]struct{}, maxChartEntries)
	var entries []ChartEntry

	for _, m := range matches {
		raw := bytes.TrimSpace(m[1])
		if len(raw) == 0 {
			continue
		}
		var list imdbLDItemList
		if err := json.Unmarshal(raw, &list); err != nil {
			continue
		}
		if len(list.ItemListElement) == 0 {
			continue
		}

		for idx, elem := range list.ItemListElement {
			urlStr := elem.URL
			name := elem.Name
			altName := elem.AlternateName
			datePublished := elem.DatePublished
			var ratingVal any
			if elem.AggregateRating != nil {
				ratingVal = elem.AggregateRating.RatingValue
			}
			if elem.Item != nil {
				if elem.Item.URL != "" {
					urlStr = elem.Item.URL
				}
				if elem.Item.Name != "" {
					name = elem.Item.Name
				}
				if elem.Item.AlternateName != "" {
					altName = elem.Item.AlternateName
				}
				if elem.Item.DatePublished != "" {
					datePublished = elem.Item.DatePublished
				}
				if elem.Item.AggregateRating != nil {
					ratingVal = elem.Item.AggregateRating.RatingValue
				}
			}

			entry := ChartEntry{
				Rank:          elem.Position,
				Title:         strings.TrimSpace(html.UnescapeString(name)),
				OriginalTitle: strings.TrimSpace(html.UnescapeString(altName)),
				IMDbID:        extractIMDbID(urlStr),
				Rating:        parseAnyFloat(ratingVal),
			}
			if entry.Rank <= 0 {
				entry.Rank = idx + 1
			}
			if datePublished != "" {
				if ym := reYear4.FindStringSubmatch(datePublished); len(ym) > 1 {
					entry.Year, _ = strconv.Atoi(ym[1])
				}
			}
			if entry.Title == "" && entry.IMDbID == "" {
				continue
			}
			key := entryDedupKey(entry)
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			entries = append(entries, entry)
			if len(entries) >= maxChartEntries {
				return entries, nil
			}
		}
		if len(entries) > 0 {
			return entries, nil
		}
	}

	return nil, errors.New("no ItemList found in application/ld+json blocks")
}

func extractIMDbID(raw string) string {
	m := reIMDbID.FindStringSubmatch(raw)
	if len(m) > 1 {
		return strings.ToLower(m[1])
	}
	return ""
}

func normalizeIMDbID(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if extracted := extractIMDbID(s); extracted != "" {
		return extracted
	}
	return strings.ToLower(s)
}

func parseAnyFloat(v any) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(val), 64)
		return f
	default:
		return 0
	}
}

func entryDedupKey(e ChartEntry) string {
	if e.DoubanID != "" {
		return "douban:" + e.DoubanID
	}
	if e.IMDbID != "" {
		return "imdb:" + strings.ToLower(e.IMDbID)
	}
	if e.TMDBID != "" {
		if e.MediaType != "" {
			return "tmdb:" + strings.ToLower(e.MediaType) + ":" + e.TMDBID
		}
		return "tmdb:" + e.TMDBID
	}
	norm := NormalizeTitle(e.Title)
	if norm == "" {
		norm = NormalizeTitle(e.OriginalTitle)
	}
	if norm == "" {
		return ""
	}
	return fmt.Sprintf("title:%s:%d", norm, e.Year)
}
