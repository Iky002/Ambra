package tmdb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	defaultBaseURL             = "https://api.themoviedb.org/3"
	defaultImageBaseURL        = "https://image.tmdb.org/t/p/original"
	defaultProfileImageBaseURL = "https://image.tmdb.org/t/p/w185"
	defaultPosterThumbBaseURL  = "https://image.tmdb.org/t/p/w342"
	defaultBackdropThumbURL    = "https://image.tmdb.org/t/p/w500"
	maxSearchCandidates        = 10
	maxResponseBytes           = 16 << 20 // 16 MiB
)

// RemoteImageCandidate represents a single remote artwork candidate from TMDB.
type RemoteImageCandidate struct {
	ProviderName    string  `json:"ProviderName"`
	Url             string  `json:"Url"`
	ThumbnailUrl    string  `json:"ThumbnailUrl,omitempty"`
	Width           int     `json:"Width,omitempty"`
	Height          int     `json:"Height,omitempty"`
	Language        string  `json:"Language,omitempty"`
	VoteAverage     float64 `json:"VoteAverage,omitempty"`
	CommunityRating float64 `json:"CommunityRating,omitempty"`
	VoteCount       int     `json:"VoteCount,omitempty"`
	Type            string  `json:"Type"`
}

// Provider implements plugins.MetadataProvider using TMDB v3 REST API.
type Provider struct {
	mu           sync.RWMutex
	apiKey       string
	language     string
	baseURL      string
	imageBaseURL string
	enabled      bool
	client       *http.Client
}

type providerConfig struct {
	apiKey       string
	language     string
	baseURL      string
	imageBaseURL string
	enabled      bool
	client       *http.Client
}

func (p *Provider) snapshot() providerConfig {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return providerConfig{
		apiKey:       p.apiKey,
		language:     p.language,
		baseURL:      p.baseURL,
		imageBaseURL: p.imageBaseURL,
		enabled:      p.enabled,
		client:       p.client,
	}
}

func New(apiKey, language string) *Provider {
	if language == "" {
		language = "zh-CN"
	}
	return &Provider{
		apiKey:       strings.TrimSpace(apiKey),
		language:     language,
		baseURL:      defaultBaseURL,
		imageBaseURL: defaultImageBaseURL,
		enabled:      true,
		client:       netutil.NewSafeHTTPClient(10*time.Second, false),
	}
}

// SetHTTPClient overrides the HTTP client used for TMDB requests (useful in tests).
func (p *Provider) SetHTTPClient(c *http.Client) {
	if c == nil {
		return
	}
	p.mu.Lock()
	p.client = c
	p.mu.Unlock()
}

func (p *Provider) Manifest() plugins.Manifest {
	snap := p.snapshot()
	return plugins.Manifest{
		ID:          "ambra.builtin.tmdb",
		Name:        "TMDB Metadata Provider",
		Version:     "1.0.0",
		Description: "Fetches Movie/Series metadata and artwork from The Movie Database (v3 API)",
		Author:      "Ambra",
		BuiltIn:     true,
		Enabled:     snap.enabled,
		Priority:    20,
		Kinds:       []plugins.Kind{plugins.KindMetadata, plugins.KindImage},
	}
}

func validateHTTPBaseURL(raw string) (string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	u, err := url.Parse(trimmed)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("invalid url %q: must use http or https scheme with host", raw)
	}
	return trimmed, nil
}

func (p *Provider) Configure(cfg map[string]any) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if v, ok := cfg["api_key"].(string); ok {
		p.apiKey = strings.TrimSpace(v)
	}
	if v, ok := cfg["language"].(string); ok && v != "" {
		p.language = v
	}
	if v, ok := cfg["base_url"].(string); ok && strings.TrimSpace(v) != "" {
		valid, err := validateHTTPBaseURL(v)
		if err != nil {
			return fmt.Errorf("tmdb base_url: %w", err)
		}
		p.baseURL = valid
	}
	if v, ok := cfg["image_base_url"].(string); ok && strings.TrimSpace(v) != "" {
		valid, err := validateHTTPBaseURL(v)
		if err != nil {
			return fmt.Errorf("tmdb image_base_url: %w", err)
		}
		p.imageBaseURL = valid
	}
	if v, ok := cfg["enabled"].(bool); ok {
		p.enabled = v
	}
	return nil
}

func (p *Provider) FetchMetadata(ctx context.Context, q plugins.MediaQuery) (*plugins.MetadataResult, error) {
	snap := p.snapshot()
	if snap.apiKey == "" {
		// Gracefully skip when no API key is configured.
		return nil, nil
	}
	lang := q.Language
	if lang == "" {
		lang = snap.language
	}

	switch strings.ToLower(q.ItemType) {
	case "series":
		return p.fetchSeries(ctx, q, lang)
	case "season":
		return p.fetchSeason(ctx, q, lang)
	case "episode":
		return p.fetchEpisode(ctx, q, lang)
	default:
		return p.fetchMovie(ctx, q, lang)
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

func (p *Provider) profileImageURL(profilePath string) string {
	if profilePath == "" {
		return ""
	}
	imgBase := p.snapshot().imageBaseURL
	if imgBase == defaultImageBaseURL {
		return defaultProfileImageBaseURL + profilePath
	}
	return imgBase + profilePath
}

func (p *Provider) findTMDBIDByIMDb(ctx context.Context, imdbID, lang, mediaKind string) string {
	imdbID = strings.TrimSpace(imdbID)
	if imdbID == "" {
		return ""
	}
	snap := p.snapshot()
	params := url.Values{}
	params.Set("api_key", snap.apiKey)
	params.Set("language", lang)
	params.Set("external_source", "imdb_id")
	var fr struct {
		MovieResults []struct {
			ID int `json:"id"`
		} `json:"movie_results"`
		TVResults []struct {
			ID int `json:"id"`
		} `json:"tv_results"`
	}
	if err := p.getJSON(ctx, "/find/"+url.PathEscape(imdbID), params, &fr); err != nil {
		return ""
	}
	if mediaKind == "tv" && len(fr.TVResults) > 0 {
		return strconv.Itoa(fr.TVResults[0].ID)
	}
	if mediaKind == "movie" && len(fr.MovieResults) > 0 {
		return strconv.Itoa(fr.MovieResults[0].ID)
	}
	return ""
}

type tmdbSearchCandidate struct {
	ID           int    `json:"id"`
	ReleaseDate  string `json:"release_date"`
	FirstAirDate string `json:"first_air_date"`
}

func pickBestTMDBCandidate(results []tmdbSearchCandidate, targetYear int) int {
	if len(results) == 0 {
		return 0
	}
	if targetYear <= 0 {
		return results[0].ID
	}
	// Prefer exact year match, then ±1 year tolerance (common for festival vs wide release).
	nearID := 0
	for _, r := range results {
		dateStr := r.ReleaseDate
		if dateStr == "" {
			dateStr = r.FirstAirDate
		}
		if len(dateStr) >= 4 {
			if y, err := strconv.Atoi(dateStr[:4]); err == nil {
				if y == targetYear {
					return r.ID
				}
				if nearID == 0 && (y == targetYear-1 || y == targetYear+1) {
					nearID = r.ID
				}
			}
		}
	}
	if nearID > 0 {
		return nearID
	}
	return results[0].ID
}

func (p *Provider) searchTMDBWithFallbacks(ctx context.Context, endpoint, yearParam, lang string, q plugins.MediaQuery) (string, error) {
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

	snap := p.snapshot()
	var lastErr error
	for _, a := range attempts {
		params := url.Values{}
		params.Set("api_key", snap.apiKey)
		params.Set("language", lang)
		params.Set("query", a.title)
		if a.year > 0 {
			params.Set(yearParam, strconv.Itoa(a.year))
		}
		var sr struct {
			Results []tmdbSearchCandidate `json:"results"`
		}
		if err := p.getJSON(ctx, endpoint, params, &sr); err != nil {
			lastErr = err
			continue
		}
		if len(sr.Results) > 0 {
			return strconv.Itoa(pickBestTMDBCandidate(sr.Results, q.Year)), nil
		}
	}
	return "", lastErr
}

func (p *Provider) fetchMovie(ctx context.Context, q plugins.MediaQuery, lang string) (*plugins.MetadataResult, error) {
	tmdbID := providerID(q.ProviderIDs, "tmdb")
	if tmdbID == "" {
		if imdbID := providerID(q.ProviderIDs, "imdb"); imdbID != "" {
			tmdbID = p.findTMDBIDByIMDb(ctx, imdbID, lang, "movie")
		}
	}
	if tmdbID == "" {
		if strings.TrimSpace(q.Title) == "" && strings.TrimSpace(q.OriginalTitle) == "" {
			return nil, nil
		}
		var err error
		tmdbID, err = p.searchTMDBWithFallbacks(ctx, "/search/movie", "year", lang, q)
		if err != nil {
			return nil, err
		}
		if tmdbID == "" {
			return nil, nil
		}
	}

	snap := p.snapshot()
	params := url.Values{}
	params.Set("api_key", snap.apiKey)
	params.Set("language", lang)
	params.Set("append_to_response", "credits,external_ids")

	var detail struct {
		ID            int     `json:"id"`
		Title         string  `json:"title"`
		OriginalTitle string  `json:"original_title"`
		Overview      string  `json:"overview"`
		Tagline       string  `json:"tagline"`
		ReleaseDate   string  `json:"release_date"`
		VoteAverage   float64 `json:"vote_average"`
		PosterPath    string  `json:"poster_path"`
		BackdropPath  string  `json:"backdrop_path"`
		Genres        []struct {
			Name string `json:"name"`
		} `json:"genres"`
		ProductionCompanies []struct {
			Name string `json:"name"`
		} `json:"production_companies"`
		Credits struct {
			Cast []struct {
				Name        string `json:"name"`
				Character   string `json:"character"`
				ProfilePath string `json:"profile_path"`
			} `json:"cast"`
			Crew []struct {
				Name        string `json:"name"`
				Job         string `json:"job"`
				ProfilePath string `json:"profile_path"`
			} `json:"crew"`
		} `json:"credits"`
		ExternalIDs struct {
			IMDbID string `json:"imdb_id"`
		} `json:"external_ids"`
		BelongsToCollection *struct {
			ID           int    `json:"id"`
			Name         string `json:"name"`
			PosterPath   string `json:"poster_path"`
			BackdropPath string `json:"backdrop_path"`
		} `json:"belongs_to_collection"`
	}
	if err := p.getJSON(ctx, "/movie/"+url.PathEscape(tmdbID), params, &detail); err != nil {
		return nil, err
	}

	res := &plugins.MetadataResult{
		Name:            detail.Title,
		OriginalTitle:   detail.OriginalTitle,
		Overview:        detail.Overview,
		Tagline:         detail.Tagline,
		PremiereDate:    detail.ReleaseDate,
		CommunityRating: detail.VoteAverage,
		ProviderIDs:     map[string]string{"tmdb": strconv.Itoa(detail.ID)},
	}
	if detail.BelongsToCollection != nil && detail.BelongsToCollection.ID > 0 && strings.TrimSpace(detail.BelongsToCollection.Name) != "" {
		col := &plugins.CollectionMeta{
			ID:   strconv.Itoa(detail.BelongsToCollection.ID),
			Name: strings.TrimSpace(detail.BelongsToCollection.Name),
		}
		if detail.BelongsToCollection.PosterPath != "" {
			col.PosterURL = snap.imageBaseURL + detail.BelongsToCollection.PosterPath
		}
		if detail.BelongsToCollection.BackdropPath != "" {
			col.BackdropURL = snap.imageBaseURL + detail.BelongsToCollection.BackdropPath
		}
		res.Collection = col
	}
	if len(detail.ReleaseDate) >= 4 {
		if y, err := strconv.Atoi(detail.ReleaseDate[:4]); err == nil {
			res.Year = y
		}
	}
	if detail.ExternalIDs.IMDbID != "" {
		res.ProviderIDs["imdb"] = detail.ExternalIDs.IMDbID
	}
	if detail.PosterPath != "" {
		res.PosterURL = snap.imageBaseURL + detail.PosterPath
	}
	if detail.BackdropPath != "" {
		res.BackdropURL = snap.imageBaseURL + detail.BackdropPath
	}
	for _, g := range detail.Genres {
		res.Genres = append(res.Genres, g.Name)
	}
	for _, s := range detail.ProductionCompanies {
		res.Studios = append(res.Studios, s.Name)
	}
	for _, c := range detail.Credits.Crew {
		if c.Job == "Director" {
			res.People = append(res.People, plugins.PersonCredit{
				Name:     c.Name,
				Type:     "Director",
				ImageURL: p.profileImageURL(c.ProfilePath),
			})
		} else if c.Job == "Writer" || c.Job == "Screenplay" {
			res.People = append(res.People, plugins.PersonCredit{
				Name:     c.Name,
				Type:     "Writer",
				ImageURL: p.profileImageURL(c.ProfilePath),
			})
		}
	}
	for i, c := range detail.Credits.Cast {
		if i >= 15 {
			break
		}
		res.People = append(res.People, plugins.PersonCredit{
			Name:     c.Name,
			Role:     c.Character,
			Type:     "Actor",
			ImageURL: p.profileImageURL(c.ProfilePath),
		})
	}
	return res, nil
}

func (p *Provider) fetchSeries(ctx context.Context, q plugins.MediaQuery, lang string) (*plugins.MetadataResult, error) {
	tmdbID := providerID(q.ProviderIDs, "tmdb")
	if tmdbID == "" {
		if imdbID := providerID(q.ProviderIDs, "imdb"); imdbID != "" {
			tmdbID = p.findTMDBIDByIMDb(ctx, imdbID, lang, "tv")
		}
	}
	if tmdbID == "" {
		if strings.TrimSpace(q.Title) == "" && strings.TrimSpace(q.OriginalTitle) == "" {
			return nil, nil
		}
		var err error
		tmdbID, err = p.searchTMDBWithFallbacks(ctx, "/search/tv", "first_air_date_year", lang, q)
		if err != nil {
			return nil, err
		}
		if tmdbID == "" {
			return nil, nil
		}
	}

	snap := p.snapshot()
	params := url.Values{}
	params.Set("api_key", snap.apiKey)
	params.Set("language", lang)
	params.Set("append_to_response", "credits,external_ids")

	var detail struct {
		ID           int     `json:"id"`
		Name         string  `json:"name"`
		OriginalName string  `json:"original_name"`
		Overview     string  `json:"overview"`
		Tagline      string  `json:"tagline"`
		FirstAirDate string  `json:"first_air_date"`
		VoteAverage  float64 `json:"vote_average"`
		PosterPath   string  `json:"poster_path"`
		BackdropPath string  `json:"backdrop_path"`
		Genres       []struct {
			Name string `json:"name"`
		} `json:"genres"`
		Networks []struct {
			Name string `json:"name"`
		} `json:"networks"`
		Credits struct {
			Cast []struct {
				Name        string `json:"name"`
				Character   string `json:"character"`
				ProfilePath string `json:"profile_path"`
			} `json:"cast"`
			Crew []struct {
				Name        string `json:"name"`
				Job         string `json:"job"`
				ProfilePath string `json:"profile_path"`
			} `json:"crew"`
		} `json:"credits"`
		ExternalIDs struct {
			IMDbID string `json:"imdb_id"`
			TVDBID int    `json:"tvdb_id"`
		} `json:"external_ids"`
	}
	if err := p.getJSON(ctx, "/tv/"+url.PathEscape(tmdbID), params, &detail); err != nil {
		return nil, err
	}

	res := &plugins.MetadataResult{
		Name:            detail.Name,
		OriginalTitle:   detail.OriginalName,
		Overview:        detail.Overview,
		Tagline:         detail.Tagline,
		PremiereDate:    detail.FirstAirDate,
		CommunityRating: detail.VoteAverage,
		ProviderIDs:     map[string]string{"tmdb": strconv.Itoa(detail.ID)},
	}
	if len(detail.FirstAirDate) >= 4 {
		if y, err := strconv.Atoi(detail.FirstAirDate[:4]); err == nil {
			res.Year = y
		}
	}
	if detail.ExternalIDs.IMDbID != "" {
		res.ProviderIDs["imdb"] = detail.ExternalIDs.IMDbID
	}
	if detail.ExternalIDs.TVDBID > 0 {
		res.ProviderIDs["tvdb"] = strconv.Itoa(detail.ExternalIDs.TVDBID)
	}
	if detail.PosterPath != "" {
		res.PosterURL = snap.imageBaseURL + detail.PosterPath
	}
	if detail.BackdropPath != "" {
		res.BackdropURL = snap.imageBaseURL + detail.BackdropPath
	}
	for _, g := range detail.Genres {
		res.Genres = append(res.Genres, g.Name)
	}
	for _, n := range detail.Networks {
		res.Studios = append(res.Studios, n.Name)
	}
	for _, c := range detail.Credits.Crew {
		if c.Job == "Director" {
			res.People = append(res.People, plugins.PersonCredit{
				Name:     c.Name,
				Type:     "Director",
				ImageURL: p.profileImageURL(c.ProfilePath),
			})
		} else if c.Job == "Writer" || c.Job == "Screenplay" {
			res.People = append(res.People, plugins.PersonCredit{
				Name:     c.Name,
				Type:     "Writer",
				ImageURL: p.profileImageURL(c.ProfilePath),
			})
		}
	}
	for i, c := range detail.Credits.Cast {
		if i >= 15 {
			break
		}
		res.People = append(res.People, plugins.PersonCredit{
			Name:     c.Name,
			Role:     c.Character,
			Type:     "Actor",
			ImageURL: p.profileImageURL(c.ProfilePath),
		})
	}
	return res, nil
}

func (p *Provider) fetchSeason(ctx context.Context, q plugins.MediaQuery, lang string) (*plugins.MetadataResult, error) {
	tmdbID := providerID(q.ProviderIDs, "tmdb")
	if tmdbID == "" || q.SeasonNumber < 0 {
		return nil, nil
	}
	snap := p.snapshot()
	params := url.Values{}
	params.Set("api_key", snap.apiKey)
	params.Set("language", lang)

	endpoint := fmt.Sprintf("/tv/%s/season/%d", url.PathEscape(tmdbID), q.SeasonNumber)
	var season struct {
		Name       string `json:"name"`
		Overview   string `json:"overview"`
		AirDate    string `json:"air_date"`
		PosterPath string `json:"poster_path"`
	}
	if err := p.getJSON(ctx, endpoint, params, &season); err != nil {
		return nil, err
	}
	res := &plugins.MetadataResult{
		Name:         season.Name,
		Overview:     season.Overview,
		PremiereDate: season.AirDate,
	}
	if len(season.AirDate) >= 4 {
		if y, err := strconv.Atoi(season.AirDate[:4]); err == nil {
			res.Year = y
		}
	}
	if season.PosterPath != "" {
		res.PosterURL = snap.imageBaseURL + season.PosterPath
	}
	return res, nil
}

func (p *Provider) fetchEpisode(ctx context.Context, q plugins.MediaQuery, lang string) (*plugins.MetadataResult, error) {
	tmdbID := providerID(q.ProviderIDs, "tmdb")
	if tmdbID == "" || q.EpisodeNumber <= 0 {
		return nil, nil
	}
	snap := p.snapshot()
	params := url.Values{}
	params.Set("api_key", snap.apiKey)
	params.Set("language", lang)

	endpoint := fmt.Sprintf("/tv/%s/season/%d/episode/%d", url.PathEscape(tmdbID), q.SeasonNumber, q.EpisodeNumber)
	var ep struct {
		Name        string  `json:"name"`
		Overview    string  `json:"overview"`
		AirDate     string  `json:"air_date"`
		VoteAverage float64 `json:"vote_average"`
		StillPath   string  `json:"still_path"`
	}
	if err := p.getJSON(ctx, endpoint, params, &ep); err != nil {
		return nil, err
	}
	res := &plugins.MetadataResult{
		Name:            ep.Name,
		Overview:        ep.Overview,
		PremiereDate:    ep.AirDate,
		CommunityRating: ep.VoteAverage,
	}
	if ep.StillPath != "" {
		res.PosterURL = snap.imageBaseURL + ep.StillPath
	}
	return res, nil
}

// SearchCandidates searches TMDB /search/movie or /search/tv and returns up to 10 candidates.
func (p *Provider) SearchCandidates(ctx context.Context, itemType, query string, year int, lang string) ([]plugins.MetadataResult, error) {
	snap := p.snapshot()
	if snap.apiKey == "" || strings.TrimSpace(query) == "" {
		return nil, nil
	}
	if lang == "" {
		lang = snap.language
	}

	isSeries := strings.EqualFold(itemType, "series") || strings.EqualFold(itemType, "tv")
	params := url.Values{}
	params.Set("api_key", snap.apiKey)
	params.Set("language", lang)
	params.Set("query", strings.TrimSpace(query))

	if isSeries {
		if year > 0 {
			params.Set("first_air_date_year", strconv.Itoa(year))
		}
		var sr struct {
			Results []struct {
				ID           int     `json:"id"`
				Name         string  `json:"name"`
				OriginalName string  `json:"original_name"`
				Overview     string  `json:"overview"`
				FirstAirDate string  `json:"first_air_date"`
				VoteAverage  float64 `json:"vote_average"`
				PosterPath   string  `json:"poster_path"`
				BackdropPath string  `json:"backdrop_path"`
			} `json:"results"`
		}
		if err := p.getJSON(ctx, "/search/tv", params, &sr); err != nil {
			return nil, err
		}
		limit := len(sr.Results)
		if limit > maxSearchCandidates {
			limit = maxSearchCandidates
		}
		out := make([]plugins.MetadataResult, 0, limit)
		for _, item := range sr.Results[:limit] {
			mr := plugins.MetadataResult{
				Name:            item.Name,
				OriginalTitle:   item.OriginalName,
				Overview:        item.Overview,
				PremiereDate:    item.FirstAirDate,
				CommunityRating: item.VoteAverage,
				ProviderIDs:     map[string]string{"tmdb": strconv.Itoa(item.ID)},
			}
			if len(item.FirstAirDate) >= 4 {
				if y, err := strconv.Atoi(item.FirstAirDate[:4]); err == nil {
					mr.Year = y
				}
			}
			if item.PosterPath != "" {
				mr.PosterURL = snap.imageBaseURL + item.PosterPath
			}
			if item.BackdropPath != "" {
				mr.BackdropURL = snap.imageBaseURL + item.BackdropPath
			}
			out = append(out, mr)
		}
		return out, nil
	}

	if year > 0 {
		params.Set("year", strconv.Itoa(year))
	}
	var sr struct {
		Results []struct {
			ID            int     `json:"id"`
			Title         string  `json:"title"`
			OriginalTitle string  `json:"original_title"`
			Overview      string  `json:"overview"`
			ReleaseDate   string  `json:"release_date"`
			VoteAverage   float64 `json:"vote_average"`
			PosterPath    string  `json:"poster_path"`
			BackdropPath  string  `json:"backdrop_path"`
		} `json:"results"`
	}
	if err := p.getJSON(ctx, "/search/movie", params, &sr); err != nil {
		return nil, err
	}
	limit := len(sr.Results)
	if limit > maxSearchCandidates {
		limit = maxSearchCandidates
	}
	out := make([]plugins.MetadataResult, 0, limit)
	for _, item := range sr.Results[:limit] {
		mr := plugins.MetadataResult{
			Name:            item.Title,
			OriginalTitle:   item.OriginalTitle,
			Overview:        item.Overview,
			PremiereDate:    item.ReleaseDate,
			CommunityRating: item.VoteAverage,
			ProviderIDs:     map[string]string{"tmdb": strconv.Itoa(item.ID)},
		}
		if len(item.ReleaseDate) >= 4 {
			if y, err := strconv.Atoi(item.ReleaseDate[:4]); err == nil {
				mr.Year = y
			}
		}
		if item.PosterPath != "" {
			mr.PosterURL = snap.imageBaseURL + item.PosterPath
		}
		if item.BackdropPath != "" {
			mr.BackdropURL = snap.imageBaseURL + item.BackdropPath
		}
		out = append(out, mr)
	}
	return out, nil
}

// FetchImages queries TMDB /movie/{id}/images or /tv/{id}/images and returns Primary and Backdrop candidates.
func (p *Provider) FetchImages(ctx context.Context, itemType, tmdbID string) ([]RemoteImageCandidate, error) {
	tmdbID = strings.TrimSpace(tmdbID)
	snap := p.snapshot()
	if snap.apiKey == "" || tmdbID == "" {
		return nil, nil
	}
	endpoint := "/movie/" + url.PathEscape(tmdbID) + "/images"
	if strings.EqualFold(itemType, "series") || strings.EqualFold(itemType, "tv") {
		endpoint = "/tv/" + url.PathEscape(tmdbID) + "/images"
	}
	params := url.Values{}
	params.Set("api_key", snap.apiKey)
	params.Set("include_image_language", "zh,en,null")

	type tmdbImageEntry struct {
		FilePath    string  `json:"file_path"`
		Width       int     `json:"width"`
		Height      int     `json:"height"`
		ISO6391     string  `json:"iso_639_1"`
		VoteAverage float64 `json:"vote_average"`
		VoteCount   int     `json:"vote_count"`
	}
	var resp struct {
		Posters   []tmdbImageEntry `json:"posters"`
		Backdrops []tmdbImageEntry `json:"backdrops"`
	}
	if err := p.getJSON(ctx, endpoint, params, &resp); err != nil {
		return nil, err
	}

	thumbURL := func(imgType, filePath string) string {
		if snap.imageBaseURL != defaultImageBaseURL {
			return snap.imageBaseURL + filePath
		}
		if imgType == "Backdrop" {
			return defaultBackdropThumbURL + filePath
		}
		return defaultPosterThumbBaseURL + filePath
	}

	out := make([]RemoteImageCandidate, 0, len(resp.Posters)+len(resp.Backdrops))
	for _, img := range resp.Posters {
		if img.FilePath == "" {
			continue
		}
		out = append(out, RemoteImageCandidate{
			ProviderName:    "TheMovieDb",
			Url:             snap.imageBaseURL + img.FilePath,
			ThumbnailUrl:    thumbURL("Primary", img.FilePath),
			Width:           img.Width,
			Height:          img.Height,
			Language:        img.ISO6391,
			VoteAverage:     img.VoteAverage,
			CommunityRating: img.VoteAverage,
			VoteCount:       img.VoteCount,
			Type:            "Primary",
		})
	}
	for _, img := range resp.Backdrops {
		if img.FilePath == "" {
			continue
		}
		out = append(out, RemoteImageCandidate{
			ProviderName:    "TheMovieDb",
			Url:             snap.imageBaseURL + img.FilePath,
			ThumbnailUrl:    thumbURL("Backdrop", img.FilePath),
			Width:           img.Width,
			Height:          img.Height,
			Language:        img.ISO6391,
			VoteAverage:     img.VoteAverage,
			CommunityRating: img.VoteAverage,
			VoteCount:       img.VoteCount,
			Type:            "Backdrop",
		})
	}
	return out, nil
}

func sanitizeAPIKeyError(err error, apiKey string) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if apiKey != "" {
		msg = strings.ReplaceAll(msg, apiKey, "REDACTED")
		msg = strings.ReplaceAll(msg, url.QueryEscape(apiKey), "REDACTED")
	}
	return fmt.Errorf("%s", msg)
}

func (p *Provider) getJSON(ctx context.Context, endpoint string, params url.Values, dst any) error {
	snap := p.snapshot()
	reqURL := snap.baseURL + endpoint + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return sanitizeAPIKeyError(err, snap.apiKey)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := snap.client.Do(req)
	if err != nil {
		return sanitizeAPIKeyError(err, snap.apiKey)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("tmdb status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return sanitizeAPIKeyError(err, snap.apiKey)
	}
	if len(body) > maxResponseBytes {
		return fmt.Errorf("tmdb response exceeds maximum size of %d bytes", maxResponseBytes)
	}
	return json.Unmarshal(body, dst)
}
