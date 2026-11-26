package tvdb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ambra-media/ambra/internal/pkg/netutil"
	"github.com/ambra-media/ambra/internal/plugins"
)

const (
	defaultBaseURL   = "https://api4.thetvdb.com/v4"
	defaultLanguage  = "zh-CN"
	defaultPriority  = 30
	tokenTTL         = 24 * time.Hour
	maxActors        = 15
	maxResponseBytes = 16 << 20 // 16 MiB
	maxEpisodePages  = 25
)

var (
	errUnauthorized                 = errors.New("tvdb unauthorized")
	_               plugins.DefaultConfigExporter = (*Provider)(nil)
)

// Provider implements plugins.MetadataProvider using TheTVDB v4 REST API.
type Provider struct {
	mu           sync.RWMutex
	enabled      bool
	apiKey       string
	pin          string
	language     string
	priority     int
	baseURL      string
	token        string
	tokenExpiry  time.Time
	tokenEpoch   uint64
	customClient bool

	loginMu sync.Mutex
	client  *http.Client
}

type providerConfig struct {
	enabled  bool
	apiKey   string
	pin      string
	language string
	priority int
	baseURL  string
	client   *http.Client
}

func (p *Provider) snapshot() providerConfig {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return providerConfig{
		enabled:  p.enabled,
		apiKey:   p.apiKey,
		pin:      p.pin,
		language: p.language,
		priority: p.priority,
		baseURL:  p.baseURL,
		client:   p.client,
	}
}

// New creates a new TheTVDB v4 metadata scraper plugin (disabled by default).
func New() *Provider {
	return &Provider{
		enabled:  false,
		language: defaultLanguage,
		priority: defaultPriority,
		baseURL:  defaultBaseURL,
		client:   netutil.NewSafeHTTPClient(10*time.Second, false),
	}
}

// SetHTTPClient overrides the HTTP client used for TVDB requests (useful in tests).
func (p *Provider) SetHTTPClient(c *http.Client) {
	if c == nil {
		return
	}
	p.mu.Lock()
	p.client = c
	p.customClient = true
	p.mu.Unlock()
}

func (p *Provider) DefaultConfig() map[string]any {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := map[string]any{
		"enabled":  p.enabled,
		"priority": p.priority,
		"language": p.language,
		"base_url": p.baseURL,
	}
	if p.apiKey != "" {
		out["api_key"] = p.apiKey
	}
	if p.pin != "" {
		out["pin"] = p.pin
	}
	return out
}

func (p *Provider) Manifest() plugins.Manifest {
	snap := p.snapshot()
	return plugins.Manifest{
		ID:          "ambra.builtin.tvdb",
		Name:        "TheTVDB Scraper",
		Version:     "1.0.0",
		Description: "Fetches TV series, episodes, movies, and artwork from TheTVDB v4 API (disabled by default)",
		Author:      "Ambra",
		BuiltIn:     true,
		Enabled:     snap.enabled,
		Priority:    snap.priority,
		Kinds:       []plugins.Kind{plugins.KindMetadata, plugins.KindImage},
	}
}

func urlHostLower(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(u.Host))
}

func (p *Provider) Configure(cfg map[string]any) error {
	if cfg == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	nextEnabled := p.enabled
	nextAPIKey := p.apiKey
	nextPin := p.pin
	nextLanguage := p.language
	nextPriority := p.priority
	nextBaseURL := p.baseURL

	if v, ok := getBool(cfg, "enabled", "Enabled"); ok {
		nextEnabled = v
	}

	authChanged := false
	hasNewKey := false
	hasNewPin := false
	if v, ok := getString(cfg, "api_key", "ApiKey", "tvdb_api_key"); ok {
		hasNewKey = true
		trimmed := strings.TrimSpace(v)
		if trimmed != nextAPIKey {
			nextAPIKey = trimmed
			authChanged = true
		}
	}
	if v, ok := getString(cfg, "pin", "Pin", "tvdb_pin"); ok {
		hasNewPin = true
		trimmed := strings.TrimSpace(v)
		if trimmed != nextPin {
			nextPin = trimmed
			authChanged = true
		}
	}
	if v, ok := getString(cfg, "language", "Language"); ok && strings.TrimSpace(v) != "" {
		nextLanguage = strings.TrimSpace(v)
	}
	if v, ok := getInt(cfg, "priority", "Priority"); ok {
		nextPriority = v
	}
	if v, ok := getString(cfg, "base_url", "BaseURL", "BaseUrl"); ok && strings.TrimSpace(v) != "" {
		trimmed := strings.TrimRight(strings.TrimSpace(v), "/")
		u, err := url.Parse(trimmed)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("tvdb: invalid base_url %q: must use http or https scheme with host", v)
		}
		if trimmed != p.baseURL {
			if err := netutil.ValidateRemoteURL(trimmed, p.customClient); err != nil {
				var dnsErr *net.DNSError
				if !errors.As(err, &dnsErr) {
					return fmt.Errorf("tvdb: unsafe base_url %q: %w", v, err)
				}
			}
		}
		if urlHostLower(trimmed) != urlHostLower(p.baseURL) {
			if !hasNewKey {
				if nextAPIKey != "" {
					authChanged = true
				}
				nextAPIKey = ""
			}
			if !hasNewPin {
				if nextPin != "" {
					authChanged = true
				}
				nextPin = ""
			}
		}
		if trimmed != p.baseURL {
			nextBaseURL = trimmed
			authChanged = true
		}
	}

	p.enabled = nextEnabled
	p.apiKey = nextAPIKey
	p.pin = nextPin
	p.language = nextLanguage
	p.priority = nextPriority
	p.baseURL = nextBaseURL
	if authChanged {
		p.token = ""
		p.tokenExpiry = time.Time{}
		p.tokenEpoch++
	}
	return nil
}

func getString(cfg map[string]any, keys ...string) (string, bool) {
	for _, k := range keys {
		if raw, exists := cfg[k]; exists {
			if s, ok := raw.(string); ok {
				return s, true
			}
		}
	}
	return "", false
}

func getBool(cfg map[string]any, keys ...string) (bool, bool) {
	for _, k := range keys {
		if raw, exists := cfg[k]; exists {
			if b, ok := raw.(bool); ok {
				return b, true
			}
		}
	}
	return false, false
}

func getInt(cfg map[string]any, keys ...string) (int, bool) {
	for _, k := range keys {
		if raw, exists := cfg[k]; exists {
			switch v := raw.(type) {
			case int:
				return v, true
			case int64:
				return int(v), true
			case float64:
				return int(v), true
			}
		}
	}
	return 0, false
}

// MapLanguage converts a BCP-47 language tag (e.g. "zh-CN", "en-US") to a
// TheTVDB v4 3-letter ISO-639-2 language code.
func MapLanguage(lang string) string {
	l := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(lang, "_", "-")))
	if l == "" {
		return "zho"
	}
	switch l {
	case "zh-tw", "zh-hk", "zh-hant", "zhtw":
		return "zhtw"
	case "zh-cn", "zh-sg", "zh-hans", "zh", "zho":
		return "zho"
	}
	prefix := l
	if idx := strings.IndexByte(l, '-'); idx > 0 {
		prefix = l[:idx]
	}
	switch prefix {
	case "zh":
		return "zho"
	case "en":
		return "eng"
	case "ja":
		return "jpn"
	case "ko":
		return "kor"
	case "es":
		return "spa"
	case "de":
		return "deu"
	case "fr":
		return "fra"
	case "pt":
		return "por"
	case "th":
		return "tha"
	}
	if len(l) == 3 {
		return l
	}
	return "eng"
}

func (p *Provider) FetchMetadata(ctx context.Context, q plugins.MediaQuery) (*plugins.MetadataResult, error) {
	snap := p.snapshot()
	if !snap.enabled || snap.apiKey == "" {
		return nil, nil
	}

	lang := q.Language
	if strings.TrimSpace(lang) == "" {
		lang = snap.language
	}
	tvdbLang := MapLanguage(lang)

	switch strings.ToLower(strings.TrimSpace(q.ItemType)) {
	case "series":
		return p.fetchSeries(ctx, q, tvdbLang)
	case "season":
		return p.fetchSeason(ctx, q, tvdbLang)
	case "episode":
		return p.fetchEpisode(ctx, q, tvdbLang)
	case "movie", "":
		return p.fetchMovie(ctx, q, tvdbLang)
	default:
		return nil, nil
	}
}

func providerID(ids map[string]string, key string) string {
	if len(ids) == 0 {
		return ""
	}
	if v := strings.TrimSpace(ids[key]); v != "" {
		return v
	}
	for k, v := range ids {
		if strings.EqualFold(k, key) {
			if tv := strings.TrimSpace(v); tv != "" {
				return tv
			}
		}
	}
	return ""
}

func (p *Provider) getToken(ctx context.Context) (string, error) {
	p.mu.RLock()
	if p.token != "" && time.Now().Before(p.tokenExpiry) {
		tok := p.token
		p.mu.RUnlock()
		return tok, nil
	}
	p.mu.RUnlock()

	p.loginMu.Lock()
	defer p.loginMu.Unlock()

	p.mu.RLock()
	if p.token != "" && time.Now().Before(p.tokenExpiry) {
		tok := p.token
		p.mu.RUnlock()
		return tok, nil
	}
	apiKey := p.apiKey
	pin := p.pin
	baseURL := p.baseURL
	epoch := p.tokenEpoch
	p.mu.RUnlock()

	if apiKey == "" {
		return "", errors.New("tvdb api_key is not configured")
	}

	payload := struct {
		APIKey string `json:"apikey"`
		PIN    string `json:"pin,omitempty"`
	}{
		APIKey: apiKey,
		PIN:    pin,
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/login", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := p.snapshot().client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("tvdb login status %d", resp.StatusCode)
	}

	var loginResp struct {
		Status string `json:"status"`
		Data   struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return "", err
	}
	if len(body) > maxResponseBytes {
		return "", fmt.Errorf("tvdb login response exceeds %d bytes", maxResponseBytes)
	}
	if err := json.Unmarshal(body, &loginResp); err != nil {
		return "", err
	}
	tok := strings.TrimSpace(loginResp.Data.Token)
	if tok == "" {
		return "", errors.New("tvdb login returned empty token")
	}

	p.mu.Lock()
	if p.tokenEpoch == epoch {
		p.token = tok
		p.tokenExpiry = time.Now().Add(tokenTTL)
	}
	p.mu.Unlock()
	return tok, nil
}

func (p *Provider) invalidateToken(failedToken string) {
	p.mu.Lock()
	if p.token == failedToken {
		p.token = ""
		p.tokenExpiry = time.Time{}
	}
	p.mu.Unlock()
}

func (p *Provider) getJSON(ctx context.Context, endpoint string, params url.Values, dst any) error {
	for attempt := 0; attempt < 2; attempt++ {
		tok, err := p.getToken(ctx)
		if err != nil {
			return err
		}
		err = p.doGetJSON(ctx, tok, endpoint, params, dst)
		if errors.Is(err, errUnauthorized) && attempt == 0 {
			p.invalidateToken(tok)
			continue
		}
		return err
	}
	return errUnauthorized
}

func (p *Provider) doGetJSON(ctx context.Context, token, endpoint string, params url.Values, dst any) error {
	snap := p.snapshot()
	reqURL := snap.baseURL + endpoint
	if len(params) > 0 {
		reqURL += "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := snap.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return errUnauthorized
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("tvdb status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return err
	}
	if len(body) > maxResponseBytes {
		return fmt.Errorf("tvdb response exceeds %d bytes", maxResponseBytes)
	}
	return json.Unmarshal(body, dst)
}

// flexString decodes either a JSON string or a JSON number into a string.
type flexString string

func (f *flexString) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		*f = ""
		return nil
	}
	if strings.HasPrefix(trimmed, `"`) {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*f = flexString(s)
		return nil
	}
	*f = flexString(trimmed)
	return nil
}

type tvdbSearchItem struct {
	ObjectID     string     `json:"objectID"`
	ID           string     `json:"id"`
	TVDBID       flexString `json:"tvdb_id"`
	Name         string     `json:"name"`
	Year         flexString `json:"year"`
	FirstAirTime string     `json:"first_air_time"`
}

func (s tvdbSearchItem) extractID() string {
	if id := strings.TrimSpace(string(s.TVDBID)); id != "" {
		return id
	}
	for _, raw := range []string{s.ID, s.ObjectID} {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if idx := strings.LastIndexByte(raw, '-'); idx >= 0 && idx+1 < len(raw) {
			return raw[idx+1:]
		}
		return raw
	}
	return ""
}

func (s tvdbSearchItem) extractYear() int {
	if y, err := strconv.Atoi(strings.TrimSpace(string(s.Year))); err == nil && y > 0 {
		return y
	}
	if len(s.FirstAirTime) >= 4 {
		if y, err := strconv.Atoi(s.FirstAirTime[:4]); err == nil && y > 0 {
			return y
		}
	}
	return 0
}

func pickBestSearchItem(items []tvdbSearchItem, targetYear int) string {
	if len(items) == 0 {
		return ""
	}
	if targetYear <= 0 {
		return items[0].extractID()
	}
	nearID := ""
	for _, item := range items {
		y := item.extractYear()
		if y == targetYear {
			if id := item.extractID(); id != "" {
				return id
			}
		}
		if nearID == "" && (y == targetYear-1 || y == targetYear+1) {
			nearID = item.extractID()
		}
	}
	if nearID != "" {
		return nearID
	}
	return items[0].extractID()
}

func (p *Provider) searchTVDBWithFallbacks(ctx context.Context, mediaType string, q plugins.MediaQuery) (string, error) {
	type attempt struct {
		title string
		year  int
	}
	var attempts []attempt
	addAttempt := func(t string, y int) {
		t = strings.TrimSpace(t)
		if t == "" {
			return
		}
		for _, a := range attempts {
			if strings.EqualFold(a.title, t) && a.year == y {
				return
			}
		}
		attempts = append(attempts, attempt{title: t, year: y})
	}

	if q.Year > 0 {
		addAttempt(q.Title, q.Year)
		addAttempt(q.OriginalTitle, q.Year)
	}
	addAttempt(q.Title, 0)
	addAttempt(q.OriginalTitle, 0)

	var lastErr error
	for _, a := range attempts {
		params := url.Values{}
		params.Set("query", a.title)
		params.Set("type", mediaType)
		if a.year > 0 {
			params.Set("year", strconv.Itoa(a.year))
		}
		var sr struct {
			Data []tvdbSearchItem `json:"data"`
		}
		if err := p.getJSON(ctx, "/search", params, &sr); err != nil {
			lastErr = err
			continue
		}
		if id := pickBestSearchItem(sr.Data, q.Year); id != "" {
			return id, nil
		}
	}
	return "", lastErr
}

type tvdbGenre struct {
	Name string `json:"name"`
}

type tvdbCompany struct {
	Name string `json:"name"`
}

type tvdbCharacter struct {
	Name         string `json:"name"`
	PeopleType   string `json:"peopleType"`
	PersonName   string `json:"personName"`
	PersonImgURL string `json:"personImgURL"`
	Image        string `json:"image"`
}

type tvdbRemoteID struct {
	ID         string `json:"id"`
	Type       int    `json:"type"`
	SourceName string `json:"sourceName"`
}

type tvdbArtwork struct {
	Image    string  `json:"image"`
	Type     int     `json:"type"`
	Score    float64 `json:"score"`
	Language string  `json:"language"`
}

type tvdbNameTranslation struct {
	Name     string `json:"name"`
	Language string `json:"language"`
}

type tvdbOverviewTranslation struct {
	Overview string `json:"overview"`
	Language string `json:"language"`
}

type tvdbInlineTranslations struct {
	NameTranslations     []tvdbNameTranslation     `json:"nameTranslations"`
	OverviewTranslations []tvdbOverviewTranslation `json:"overviewTranslations"`
}

type tvdbTranslationResp struct {
	Data struct {
		Name     string `json:"name"`
		Overview string `json:"overview"`
	} `json:"data"`
}

func (p *Provider) resolveLocalization(
	ctx context.Context,
	inline tvdbInlineTranslations,
	translationEndpoint string,
	tvdbLang, defaultName, defaultOverview string,
) (name, overview string) {
	for _, nt := range inline.NameTranslations {
		if strings.EqualFold(nt.Language, tvdbLang) && strings.TrimSpace(nt.Name) != "" {
			name = strings.TrimSpace(nt.Name)
			break
		}
	}
	for _, ot := range inline.OverviewTranslations {
		if strings.EqualFold(ot.Language, tvdbLang) && strings.TrimSpace(ot.Overview) != "" {
			overview = strings.TrimSpace(ot.Overview)
			break
		}
	}

	if (name == "" || overview == "") && translationEndpoint != "" && tvdbLang != "" {
		var tr tvdbTranslationResp
		if err := p.getJSON(ctx, translationEndpoint, nil, &tr); err == nil {
			if name == "" && strings.TrimSpace(tr.Data.Name) != "" {
				name = strings.TrimSpace(tr.Data.Name)
			}
			if overview == "" && strings.TrimSpace(tr.Data.Overview) != "" {
				overview = strings.TrimSpace(tr.Data.Overview)
			}
		}
	}

	if name == "" {
		name = strings.TrimSpace(defaultName)
	}
	if overview == "" {
		overview = strings.TrimSpace(defaultOverview)
	}
	return name, overview
}

func extractRemoteIDs(tvdbID string, remotes []tvdbRemoteID) map[string]string {
	ids := map[string]string{}
	if strings.TrimSpace(tvdbID) != "" {
		ids["Tvdb"] = strings.TrimSpace(tvdbID)
	}
	for _, r := range remotes {
		val := strings.TrimSpace(r.ID)
		if val == "" {
			continue
		}
		src := strings.ToLower(strings.TrimSpace(r.SourceName))
		switch {
		case src == "imdb" || r.Type == 2 || strings.HasPrefix(strings.ToLower(val), "tt"):
			if _, exists := ids["Imdb"]; !exists {
				ids["Imdb"] = val
			}
		case strings.Contains(src, "themoviedb") || src == "tmdb" || r.Type == 10 || r.Type == 12:
			if _, exists := ids["Tmdb"]; !exists {
				ids["Tmdb"] = val
			}
		}
	}
	return ids
}

func extractPeople(chars []tvdbCharacter) []plugins.PersonCredit {
	var people []plugins.PersonCredit
	actorCount := 0
	for _, c := range chars {
		personName := strings.TrimSpace(c.PersonName)
		if personName == "" {
			continue
		}
		img := strings.TrimSpace(c.PersonImgURL)
		if img == "" {
			img = strings.TrimSpace(c.Image)
		}
		pType := strings.ToLower(strings.TrimSpace(c.PeopleType))
		switch pType {
		case "director":
			people = append(people, plugins.PersonCredit{
				Name:     personName,
				Type:     "Director",
				ImageURL: img,
			})
		case "writer":
			people = append(people, plugins.PersonCredit{
				Name:     personName,
				Type:     "Writer",
				ImageURL: img,
			})
		case "actor", "":
			if actorCount >= maxActors {
				continue
			}
			actorCount++
			people = append(people, plugins.PersonCredit{
				Name:     personName,
				Role:     strings.TrimSpace(c.Name),
				Type:     "Actor",
				ImageURL: img,
			})
		}
	}
	return people
}

func pickArtworkURL(artworks []tvdbArtwork, artworkType int, tvdbLang string) string {
	bestURL := ""
	bestRank := -1
	bestScore := -1.0
	for _, a := range artworks {
		if a.Type != artworkType || strings.TrimSpace(a.Image) == "" {
			continue
		}
		rank := 0
		lang := strings.ToLower(strings.TrimSpace(a.Language))
		if lang == strings.ToLower(tvdbLang) {
			rank = 2
		} else if lang == "" || lang == "eng" {
			rank = 1
		}
		if rank > bestRank || (rank == bestRank && a.Score > bestScore) {
			bestRank = rank
			bestScore = a.Score
			bestURL = strings.TrimSpace(a.Image)
		}
	}
	return bestURL
}

func appendUniqueStrings(dst []string, items ...string) []string {
	seen := make(map[string]struct{}, len(dst))
	for _, s := range dst {
		seen[strings.ToLower(s)] = struct{}{}
	}
	for _, item := range items {
		t := strings.TrimSpace(item)
		if t == "" {
			continue
		}
		key := strings.ToLower(t)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		dst = append(dst, t)
	}
	return dst
}

func parseYear(yearStr flexString, dateStr string) int {
	if y, err := strconv.Atoi(strings.TrimSpace(string(yearStr))); err == nil && y > 0 {
		return y
	}
	if len(dateStr) >= 4 {
		if y, err := strconv.Atoi(dateStr[:4]); err == nil && y > 0 {
			return y
		}
	}
	return 0
}

func (p *Provider) fetchSeries(ctx context.Context, q plugins.MediaQuery, tvdbLang string) (*plugins.MetadataResult, error) {
	tvdbID := providerID(q.ProviderIDs, "Tvdb")
	if tvdbID == "" {
		if strings.TrimSpace(q.Title) == "" && strings.TrimSpace(q.OriginalTitle) == "" {
			return nil, nil
		}
		var err error
		tvdbID, err = p.searchTVDBWithFallbacks(ctx, "series", q)
		if err != nil {
			return nil, err
		}
		if tvdbID == "" {
			return nil, nil
		}
	}

	var resp struct {
		Data struct {
			ID              int                    `json:"id"`
			Name            string                 `json:"name"`
			Overview        string                 `json:"overview"`
			FirstAired      string                 `json:"firstAired"`
			Year            flexString             `json:"year"`
			Image           string                 `json:"image"`
			Score           float64                `json:"score"`
			Genres          []tvdbGenre            `json:"genres"`
			Companies       []tvdbCompany          `json:"companies"`
			OriginalNetwork *tvdbCompany           `json:"originalNetwork"`
			Characters      []tvdbCharacter        `json:"characters"`
			RemoteIDs       []tvdbRemoteID         `json:"remoteIds"`
			Artworks        []tvdbArtwork          `json:"artworks"`
			Translations    tvdbInlineTranslations `json:"translations"`
		} `json:"data"`
	}
	endpoint := "/series/" + url.PathEscape(tvdbID) + "/extended"
	if err := p.getJSON(ctx, endpoint, nil, &resp); err != nil {
		return nil, err
	}

	d := resp.Data
	idStr := tvdbID
	if d.ID > 0 {
		idStr = strconv.Itoa(d.ID)
	}

	trEndpoint := "/series/" + url.PathEscape(idStr) + "/translations/" + url.PathEscape(tvdbLang)
	name, overview := p.resolveLocalization(ctx, d.Translations, trEndpoint, tvdbLang, d.Name, d.Overview)

	res := &plugins.MetadataResult{
		Name:            name,
		OriginalTitle:   strings.TrimSpace(d.Name),
		Overview:        overview,
		Year:            parseYear(d.Year, d.FirstAired),
		PremiereDate:    strings.TrimSpace(d.FirstAired),
		CommunityRating: d.Score,
		ProviderIDs:     extractRemoteIDs(idStr, d.RemoteIDs),
		People:          extractPeople(d.Characters),
		PosterURL:       pickArtworkURL(d.Artworks, 2, tvdbLang),
		BackdropURL:     pickArtworkURL(d.Artworks, 3, tvdbLang),
		LogoURL:         pickArtworkURL(d.Artworks, 23, tvdbLang),
	}
	if res.PosterURL == "" {
		res.PosterURL = strings.TrimSpace(d.Image)
	}
	for _, g := range d.Genres {
		res.Genres = appendUniqueStrings(res.Genres, g.Name)
	}
	if d.OriginalNetwork != nil {
		res.Studios = appendUniqueStrings(res.Studios, d.OriginalNetwork.Name)
	}
	for _, c := range d.Companies {
		res.Studios = appendUniqueStrings(res.Studios, c.Name)
	}
	return res, nil
}

func (p *Provider) fetchSeason(ctx context.Context, q plugins.MediaQuery, tvdbLang string) (*plugins.MetadataResult, error) {
	if q.SeasonNumber < 0 {
		return nil, nil
	}
	seriesID := providerID(q.ProviderIDs, "Tvdb")
	if seriesID == "" {
		if strings.TrimSpace(q.Title) == "" && strings.TrimSpace(q.OriginalTitle) == "" {
			return nil, nil
		}
		var err error
		seriesID, err = p.searchTVDBWithFallbacks(ctx, "series", q)
		if err != nil {
			return nil, err
		}
		if seriesID == "" {
			return nil, nil
		}
	}

	var seriesResp struct {
		Data struct {
			Seasons []struct {
				ID     int    `json:"id"`
				Number int    `json:"number"`
				Name   string `json:"name"`
				Image  string `json:"image"`
				Type   *struct {
					ID   int    `json:"id"`
					Type string `json:"type"`
				} `json:"type"`
			} `json:"seasons"`
		} `json:"data"`
	}
	if err := p.getJSON(ctx, "/series/"+url.PathEscape(seriesID)+"/extended", nil, &seriesResp); err != nil {
		return nil, err
	}

	var matchedID int
	var fallbackName, fallbackImage string
	for _, s := range seriesResp.Data.Seasons {
		if s.Number != q.SeasonNumber {
			continue
		}
		// Prefer official/default season order (type.id == 1 or type.type == "official").
		isOfficial := s.Type == nil || s.Type.ID == 1 || strings.EqualFold(s.Type.Type, "official")
		if matchedID == 0 || isOfficial {
			matchedID = s.ID
			fallbackName = s.Name
			fallbackImage = s.Image
			if isOfficial {
				break
			}
		}
	}
	if matchedID <= 0 {
		return nil, nil
	}

	seasonIDStr := strconv.Itoa(matchedID)
	var seasonResp struct {
		Data struct {
			ID           int                    `json:"id"`
			Name         string                 `json:"name"`
			Overview     string                 `json:"overview"`
			Image        string                 `json:"image"`
			Year         flexString             `json:"year"`
			Artwork      []tvdbArtwork          `json:"artwork"`
			Translations tvdbInlineTranslations `json:"translations"`
		} `json:"data"`
	}
	if err := p.getJSON(ctx, "/seasons/"+url.PathEscape(seasonIDStr)+"/extended", nil, &seasonResp); err != nil {
		// Fall back gracefully to summary season info if extended endpoint fails.
		return &plugins.MetadataResult{
			Name:        strings.TrimSpace(fallbackName),
			PosterURL:   strings.TrimSpace(fallbackImage),
			ProviderIDs: map[string]string{"Tvdb": seasonIDStr},
		}, nil
	}

	d := seasonResp.Data
	defName := strings.TrimSpace(d.Name)
	if defName == "" {
		defName = strings.TrimSpace(fallbackName)
	}
	trEndpoint := "/seasons/" + url.PathEscape(seasonIDStr) + "/translations/" + url.PathEscape(tvdbLang)
	name, overview := p.resolveLocalization(ctx, d.Translations, trEndpoint, tvdbLang, defName, d.Overview)

	posterURL := pickArtworkURL(d.Artwork, 7, tvdbLang)
	if posterURL == "" {
		posterURL = strings.TrimSpace(d.Image)
	}
	if posterURL == "" {
		posterURL = strings.TrimSpace(fallbackImage)
	}
	return &plugins.MetadataResult{
		Name:          name,
		OriginalTitle: defName,
		Overview:      overview,
		Year:          parseYear(d.Year, ""),
		PosterURL:     posterURL,
		ProviderIDs:   map[string]string{"Tvdb": seasonIDStr},
	}, nil
}

func (p *Provider) fetchEpisode(ctx context.Context, q plugins.MediaQuery, tvdbLang string) (*plugins.MetadataResult, error) {
	if q.SeasonNumber < 0 || q.EpisodeNumber <= 0 {
		return nil, nil
	}
	seriesID := providerID(q.ProviderIDs, "Tvdb")
	if seriesID == "" {
		if strings.TrimSpace(q.Title) == "" && strings.TrimSpace(q.OriginalTitle) == "" {
			return nil, nil
		}
		var err error
		seriesID, err = p.searchTVDBWithFallbacks(ctx, "series", q)
		if err != nil {
			return nil, err
		}
		if seriesID == "" {
			return nil, nil
		}
	}

	endpoint := "/series/" + url.PathEscape(seriesID) + "/episodes/default"
	for page := 0; page < maxEpisodePages; page++ {
		params := url.Values{}
		params.Set("season", strconv.Itoa(q.SeasonNumber))
		if page > 0 {
			params.Set("page", strconv.Itoa(page))
		}

		var resp struct {
			Data struct {
				Episodes []struct {
					ID           int    `json:"id"`
					Name         string `json:"name"`
					Overview     string `json:"overview"`
					Aired        string `json:"aired"`
					Image        string `json:"image"`
					SeasonNumber int    `json:"seasonNumber"`
					Number       int    `json:"number"`
				} `json:"episodes"`
			} `json:"data"`
			Links struct {
				Next string `json:"next"`
			} `json:"links"`
		}
		if err := p.getJSON(ctx, endpoint, params, &resp); err != nil {
			return nil, err
		}

		for _, ep := range resp.Data.Episodes {
			if ep.SeasonNumber != q.SeasonNumber || ep.Number != q.EpisodeNumber {
				continue
			}
			epID := strconv.Itoa(ep.ID)
			trEndpoint := ""
			if ep.ID > 0 {
				trEndpoint = "/episodes/" + url.PathEscape(epID) + "/translations/" + url.PathEscape(tvdbLang)
			}
			name, overview := p.resolveLocalization(ctx, tvdbInlineTranslations{}, trEndpoint, tvdbLang, ep.Name, ep.Overview)
			res := &plugins.MetadataResult{
				Name:          name,
				OriginalTitle: strings.TrimSpace(ep.Name),
				Overview:      overview,
				Year:          parseYear("", ep.Aired),
				PremiereDate:  strings.TrimSpace(ep.Aired),
				PosterURL:     strings.TrimSpace(ep.Image),
				ProviderIDs:   map[string]string{},
			}
			if ep.ID > 0 {
				res.ProviderIDs["Tvdb"] = epID
			}
			return res, nil
		}

		if len(resp.Data.Episodes) == 0 || strings.TrimSpace(resp.Links.Next) == "" {
			break
		}
	}
	return nil, nil
}

func (p *Provider) fetchMovie(ctx context.Context, q plugins.MediaQuery, tvdbLang string) (*plugins.MetadataResult, error) {
	tvdbID := providerID(q.ProviderIDs, "Tvdb")
	if tvdbID == "" {
		if strings.TrimSpace(q.Title) == "" && strings.TrimSpace(q.OriginalTitle) == "" {
			return nil, nil
		}
		var err error
		tvdbID, err = p.searchTVDBWithFallbacks(ctx, "movie", q)
		if err != nil {
			return nil, err
		}
		if tvdbID == "" {
			return nil, nil
		}
	}

	var resp struct {
		Data struct {
			ID           int        `json:"id"`
			Name         string     `json:"name"`
			Overview     string     `json:"overview"`
			Year         flexString `json:"year"`
			FirstRelease *struct {
				Date string `json:"date"`
			} `json:"first_release"`
			Releases []struct {
				Date string `json:"date"`
			} `json:"releases"`
			Image     string        `json:"image"`
			Score     float64       `json:"score"`
			Genres    []tvdbGenre   `json:"genres"`
			Studios   []tvdbCompany `json:"studios"`
			Companies *struct {
				Studio     []tvdbCompany `json:"studio"`
				Production []tvdbCompany `json:"production"`
			} `json:"companies"`
			Characters   []tvdbCharacter        `json:"characters"`
			RemoteIDs    []tvdbRemoteID         `json:"remoteIds"`
			Artworks     []tvdbArtwork          `json:"artworks"`
			Translations tvdbInlineTranslations `json:"translations"`
		} `json:"data"`
	}
	endpoint := "/movies/" + url.PathEscape(tvdbID) + "/extended"
	if err := p.getJSON(ctx, endpoint, nil, &resp); err != nil {
		return nil, err
	}

	d := resp.Data
	idStr := tvdbID
	if d.ID > 0 {
		idStr = strconv.Itoa(d.ID)
	}

	premiereDate := ""
	if d.FirstRelease != nil && strings.TrimSpace(d.FirstRelease.Date) != "" {
		premiereDate = strings.TrimSpace(d.FirstRelease.Date)
	} else if len(d.Releases) > 0 {
		premiereDate = strings.TrimSpace(d.Releases[0].Date)
	}

	trEndpoint := "/movies/" + url.PathEscape(idStr) + "/translations/" + url.PathEscape(tvdbLang)
	name, overview := p.resolveLocalization(ctx, d.Translations, trEndpoint, tvdbLang, d.Name, d.Overview)

	res := &plugins.MetadataResult{
		Name:            name,
		OriginalTitle:   strings.TrimSpace(d.Name),
		Overview:        overview,
		Year:            parseYear(d.Year, premiereDate),
		PremiereDate:    premiereDate,
		CommunityRating: d.Score,
		ProviderIDs:     extractRemoteIDs(idStr, d.RemoteIDs),
		People:          extractPeople(d.Characters),
		PosterURL:       pickArtworkURL(d.Artworks, 14, tvdbLang),
		BackdropURL:     pickArtworkURL(d.Artworks, 15, tvdbLang),
		LogoURL:         pickArtworkURL(d.Artworks, 25, tvdbLang),
	}
	if res.PosterURL == "" {
		res.PosterURL = strings.TrimSpace(d.Image)
	}
	for _, g := range d.Genres {
		res.Genres = appendUniqueStrings(res.Genres, g.Name)
	}
	for _, s := range d.Studios {
		res.Studios = appendUniqueStrings(res.Studios, s.Name)
	}
	if d.Companies != nil {
		for _, s := range d.Companies.Studio {
			res.Studios = appendUniqueStrings(res.Studios, s.Name)
		}
		for _, s := range d.Companies.Production {
			res.Studios = appendUniqueStrings(res.Studios, s.Name)
		}
	}
	return res, nil
}
