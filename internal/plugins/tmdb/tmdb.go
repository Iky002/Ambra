package tmdb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ambra-media/ambra/internal/plugins"
)

const (
	defaultBaseURL             = "https://api.themoviedb.org/3"
	defaultImageBaseURL        = "https://image.tmdb.org/t/p/original"
	defaultProfileImageBaseURL = "https://image.tmdb.org/t/p/w185"
)

// Provider implements plugins.MetadataProvider using TMDB v3 REST API.
type Provider struct {
	apiKey       string
	language     string
	baseURL      string
	imageBaseURL string
	enabled      bool
	client       *http.Client
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
		client:       &http.Client{Timeout: 10 * time.Second},
	}
}

func (p *Provider) Manifest() plugins.Manifest {
	return plugins.Manifest{
		ID:          "ambra.builtin.tmdb",
		Name:        "TMDB Metadata Provider",
		Version:     "1.0.0",
		Description: "Fetches Movie/Series metadata and artwork from The Movie Database (v3 API)",
		Author:      "Ambra",
		BuiltIn:     true,
		Enabled:     p.enabled,
		Priority:    20,
		Kinds:       []plugins.Kind{plugins.KindMetadata, plugins.KindImage},
	}
}

func (p *Provider) Configure(cfg map[string]any) error {
	if v, ok := cfg["api_key"].(string); ok {
		p.apiKey = strings.TrimSpace(v)
	}
	if v, ok := cfg["language"].(string); ok && v != "" {
		p.language = v
	}
	if v, ok := cfg["base_url"].(string); ok && v != "" {
		p.baseURL = strings.TrimRight(v, "/")
	}
	if v, ok := cfg["image_base_url"].(string); ok && v != "" {
		p.imageBaseURL = strings.TrimRight(v, "/")
	}
	if v, ok := cfg["enabled"].(bool); ok {
		p.enabled = v
	}
	return nil
}

func (p *Provider) FetchMetadata(ctx context.Context, q plugins.MediaQuery) (*plugins.MetadataResult, error) {
	if p.apiKey == "" {
		// Gracefully skip when no API key is configured.
		return nil, nil
	}
	lang := q.Language
	if lang == "" {
		lang = p.language
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
	if p.imageBaseURL == defaultImageBaseURL {
		return defaultProfileImageBaseURL + profilePath
	}
	return p.imageBaseURL + profilePath
}

func (p *Provider) fetchMovie(ctx context.Context, q plugins.MediaQuery, lang string) (*plugins.MetadataResult, error) {
	tmdbID := providerID(q.ProviderIDs, "tmdb")
	if tmdbID == "" {
		if q.Title == "" {
			return nil, nil
		}
		params := url.Values{}
		params.Set("api_key", p.apiKey)
		params.Set("language", lang)
		params.Set("query", q.Title)
		if q.Year > 0 {
			params.Set("year", strconv.Itoa(q.Year))
		}
		var sr struct {
			Results []struct {
				ID int `json:"id"`
			} `json:"results"`
		}
		if err := p.getJSON(ctx, "/search/movie", params, &sr); err != nil {
			return nil, err
		}
		if len(sr.Results) == 0 {
			return nil, nil
		}
		tmdbID = strconv.Itoa(sr.Results[0].ID)
	}

	params := url.Values{}
	params.Set("api_key", p.apiKey)
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
	}
	if err := p.getJSON(ctx, "/movie/"+tmdbID, params, &detail); err != nil {
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
	if len(detail.ReleaseDate) >= 4 {
		if y, err := strconv.Atoi(detail.ReleaseDate[:4]); err == nil {
			res.Year = y
		}
	}
	if detail.ExternalIDs.IMDbID != "" {
		res.ProviderIDs["imdb"] = detail.ExternalIDs.IMDbID
	}
	if detail.PosterPath != "" {
		res.PosterURL = p.imageBaseURL + detail.PosterPath
	}
	if detail.BackdropPath != "" {
		res.BackdropURL = p.imageBaseURL + detail.BackdropPath
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
		if q.Title == "" {
			return nil, nil
		}
		params := url.Values{}
		params.Set("api_key", p.apiKey)
		params.Set("language", lang)
		params.Set("query", q.Title)
		if q.Year > 0 {
			params.Set("first_air_date_year", strconv.Itoa(q.Year))
		}
		var sr struct {
			Results []struct {
				ID int `json:"id"`
			} `json:"results"`
		}
		if err := p.getJSON(ctx, "/search/tv", params, &sr); err != nil {
			return nil, err
		}
		if len(sr.Results) == 0 {
			return nil, nil
		}
		tmdbID = strconv.Itoa(sr.Results[0].ID)
	}

	params := url.Values{}
	params.Set("api_key", p.apiKey)
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
	if err := p.getJSON(ctx, "/tv/"+tmdbID, params, &detail); err != nil {
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
		res.PosterURL = p.imageBaseURL + detail.PosterPath
	}
	if detail.BackdropPath != "" {
		res.BackdropURL = p.imageBaseURL + detail.BackdropPath
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
	params := url.Values{}
	params.Set("api_key", p.apiKey)
	params.Set("language", lang)

	endpoint := fmt.Sprintf("/tv/%s/season/%d", tmdbID, q.SeasonNumber)
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
		res.PosterURL = p.imageBaseURL + season.PosterPath
	}
	return res, nil
}

func (p *Provider) fetchEpisode(ctx context.Context, q plugins.MediaQuery, lang string) (*plugins.MetadataResult, error) {
	tmdbID := providerID(q.ProviderIDs, "tmdb")
	if tmdbID == "" || q.EpisodeNumber <= 0 {
		return nil, nil
	}
	params := url.Values{}
	params.Set("api_key", p.apiKey)
	params.Set("language", lang)

	endpoint := fmt.Sprintf("/tv/%s/season/%d/episode/%d", tmdbID, q.SeasonNumber, q.EpisodeNumber)
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
		res.PosterURL = p.imageBaseURL + ep.StillPath
	}
	return res, nil
}

func (p *Provider) getJSON(ctx context.Context, endpoint string, params url.Values, dst any) error {
	reqURL := p.baseURL + endpoint + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("tmdb status %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}
