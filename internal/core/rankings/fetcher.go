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

// Fetcher downloads and parses Douban Top 250 and IMDb Top 250 charts.
type Fetcher struct {
	httpClient *http.Client
	doubanURL  string
	imdbURL    string
	userAgent  string
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
		httpClient: client,
		doubanURL:  strings.TrimSpace(opts.DoubanURL),
		imdbURL:    strings.TrimSpace(opts.IMDbURL),
		userAgent:  ua,
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
func (f *Fetcher) FetchIMDbTop250(ctx context.Context) (*ChartSnapshot, error) {
	targetURL := f.resolveIMDbURL()
	body, contentType, err := f.doGet(ctx, targetURL)
	if err != nil {
		return nil, fmt.Errorf("fetch imdb top250: %w", err)
	}

	var entries []ChartEntry
	if isJSONPayload(contentType, body) {
		entries, err = parseJSONEntries(body, maxChartEntries)
		if err != nil {
			return nil, fmt.Errorf("parse imdb json: %w", err)
		}
	} else {
		entries, err = parseIMDbTop250HTML(body)
		if err != nil {
			return nil, fmt.Errorf("parse imdb html: %w", err)
		}
	}

	if len(entries) == 0 {
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

func (f *Fetcher) doGet(ctx context.Context, rawURL string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", f.userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/json;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
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
