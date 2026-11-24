package system

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

type Handler struct {
	cfg      *config.Config
	userRepo *sqlite.UserRepo
	registry *plugins.Registry
}

func NewHandler(cfg *config.Config, userRepo *sqlite.UserRepo, registry *plugins.Registry) *Handler {
	return &Handler{
		cfg:      cfg,
		userRepo: userRepo,
		registry: registry,
	}
}

func RegisterRoutes(r chi.Router, authMW *middleware.Auth, h *Handler) {
	// Public system & discovery routes
	r.Get("/System/Info/Public", h.GetPublicSystemInfo)
	r.Get("/System/Ping", h.Ping)
	r.Head("/System/Ping", h.Ping)
	r.Post("/System/Ping", h.Ping)

	r.Get("/Branding/Configuration", h.GetBrandingConfiguration)
	r.Get("/Branding/Css", h.GetBrandingCSS)
	r.Get("/Branding/Css.css", h.GetBrandingCSS)

	r.Get("/Localization/Cultures", h.GetCultures)
	r.Get("/Localization/Countries", h.GetCountries)
	r.Get("/Localization/Options", h.GetLocalizationOptions)
	r.Get("/Localization/ParentalRatings", h.GetParentalRatings)

	r.Get("/Registrations/{Name}", h.GetRegistration)
	r.Get("/Features", h.GetFeatures)

	// Authenticated system routes
	r.Group(func(pr chi.Router) {
		pr.Use(authMW.RequireAuth)
		pr.Get("/System/Info", h.GetSystemInfo)
		pr.Get("/System/Configuration", h.GetConfiguration)
		pr.Get("/System/Configuration/{Key}", h.GetNamedConfiguration)
		pr.Get("/System/ActivityLog/Entries", h.GetActivityLogEntries)
		pr.Get("/Plugins", h.GetPlugins)
	})

	// Admin-only system mutation routes
	r.Group(func(ar chi.Router) {
		ar.Use(authMW.RequireAdmin)
		ar.Post("/System/Configuration", h.UpdateConfiguration)
		ar.Post("/System/Configuration/{Key}", h.UpdateNamedConfiguration)
	})
}

func (h *Handler) GetPublicSystemInfo(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, h.buildPublicInfo(r))
}

func (h *Handler) GetSystemInfo(w http.ResponseWriter, r *http.Request) {
	port := parsePort(h.cfg.HTTPAddr, 8096)
	info := dto.SystemInfo{
		PublicSystemInfo:           h.buildPublicInfo(r),
		OperatingSystem:            runtime.GOOS,
		OperatingSystemDisplayName: osDisplayName(runtime.GOOS),
		HasPendingRestart:          false,
		IsShuttingDown:             false,
		SupportsLibraryMonitor:     true,
		WebSocketPortNumber:        port,
		CanSelfRestart:             false,
		CanSelfUpdate:              false,
		CanLaunchWebBrowser:        false,
		ProgramDataPath:            h.cfg.DataDir,
		CachePath:                  filepath.Join(h.cfg.DataDir, "cache"),
		LogPath:                    filepath.Join(h.cfg.DataDir, "logs"),
		InternalMetadataPath:       filepath.Join(h.cfg.DataDir, "metadata"),
		TranscodingTempPath:        h.cfg.TranscodeTempDir(),
		HttpServerPortNumber:       port,
		SupportsHttps:              false,
		HasUpdateAvailable:         false,
		EncoderLocationType:        "System",
		SystemArchitecture:         runtime.GOARCH,
		StartupWizardCompleted:     h.cfg.IsStartupWizardCompleted,
	}
	httpx.JSON(w, http.StatusOK, info)
}

func (h *Handler) Ping(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write([]byte("Ambra Server"))
	}
}

type ServerConfigurationDto struct {
	ServerName               string  `json:"ServerName"`
	IsStartupWizardCompleted bool    `json:"IsStartupWizardCompleted"`
	TMDBApiKey               string  `json:"TMDBApiKey,omitempty"`
	TMDBLanguage             string  `json:"TMDBLanguage,omitempty"`
	EnableSTRM302            bool    `json:"EnableSTRM302"`
	PlayedThreshold          float64 `json:"PlayedThreshold"`
	PreferredMetadataLanguage string `json:"PreferredMetadataLanguage"`
	MetadataCountryCode      string  `json:"MetadataCountryCode"`
	UICulture                string  `json:"UICulture"`
}

func (h *Handler) GetConfiguration(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, ServerConfigurationDto{
		ServerName:                h.cfg.ServerName,
		IsStartupWizardCompleted:  h.cfg.IsStartupWizardCompleted,
		TMDBApiKey:                h.cfg.TMDBApiKey,
		TMDBLanguage:              h.cfg.TMDBLanguage,
		EnableSTRM302:             h.cfg.EnableSTRM302,
		PlayedThreshold:           h.cfg.PlayedThreshold,
		PreferredMetadataLanguage: "zh",
		MetadataCountryCode:       "CN",
		UICulture:                 "zh-cn",
	})
}

func (h *Handler) UpdateConfiguration(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if v, ok := body["ServerName"].(string); ok && strings.TrimSpace(v) != "" {
		h.cfg.ServerName = strings.TrimSpace(v)
	}
	if v, ok := body["IsStartupWizardCompleted"].(bool); ok {
		h.cfg.IsStartupWizardCompleted = v
	}
	if v, ok := body["TMDBApiKey"].(string); ok {
		h.cfg.TMDBApiKey = strings.TrimSpace(v)
	}
	if v, ok := body["TMDBLanguage"].(string); ok && strings.TrimSpace(v) != "" {
		h.cfg.TMDBLanguage = strings.TrimSpace(v)
	}
	if v, ok := body["EnableSTRM302"].(bool); ok {
		h.cfg.EnableSTRM302 = v
	}
	if v, ok := body["PlayedThreshold"].(float64); ok && v > 0 && v <= 1 {
		h.cfg.PlayedThreshold = v
	}
	if err := h.cfg.Save(); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to save configuration")
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) GetNamedConfiguration(w http.ResponseWriter, r *http.Request) {
	key := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "Key")))
	if key == "" {
		httpx.Error(w, http.StatusBadRequest, "missing configuration key")
		return
	}
	if h.userRepo != nil {
		raw, err := h.userRepo.GetServerConfig(r.Context(), "named:"+key)
		if err == nil && raw != "" {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(raw))
			return
		}
		if err != nil && !errors.Is(err, sqlite.ErrNotFound) {
			httpx.Error(w, http.StatusInternalServerError, "failed to load configuration")
			return
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{})
}

func (h *Handler) UpdateNamedConfiguration(w http.ResponseWriter, r *http.Request) {
	key := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "Key")))
	if key == "" {
		httpx.Error(w, http.StatusBadRequest, "missing configuration key")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	if !json.Valid(raw) {
		httpx.Error(w, http.StatusBadRequest, "invalid json payload")
		return
	}
	if h.userRepo != nil {
		if err := h.userRepo.SetServerConfig(r.Context(), "named:"+key, string(raw)); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "failed to save configuration")
			return
		}
	}
	httpx.NoContent(w)
}

type ActivityLogEntryDto struct {
	Id            int64     `json:"Id"`
	Name          string    `json:"Name"`
	Overview      string    `json:"Overview,omitempty"`
	ShortOverview string    `json:"ShortOverview,omitempty"`
	Type          string    `json:"Type"`
	ItemId        string    `json:"ItemId,omitempty"`
	Date          time.Time `json:"Date"`
	UserId        string    `json:"UserId,omitempty"`
	Severity      string    `json:"Severity"`
}

func (h *Handler) GetActivityLogEntries(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	startIndex := q.Int("StartIndex", 0)
	limit := q.Int("Limit", 50)
	var minDate *time.Time
	if rawDate := q.Get("MinDate"); rawDate != "" {
		if t, err := time.Parse(time.RFC3339, rawDate); err == nil {
			minDate = &t
		}
	}

	if h.userRepo == nil {
		httpx.JSON(w, http.StatusOK, dto.QueryResult[ActivityLogEntryDto]{
			Items:            []ActivityLogEntryDto{},
			TotalRecordCount: 0,
		})
		return
	}

	recs, total, err := h.userRepo.ListActivityLogs(r.Context(), startIndex, limit, minDate)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to query activity logs")
		return
	}
	items := make([]ActivityLogEntryDto, 0, len(recs))
	for _, rec := range recs {
		items = append(items, ActivityLogEntryDto{
			Id:            rec.ID,
			Name:          rec.Name,
			Overview:      rec.Overview,
			ShortOverview: rec.ShortOverview,
			Type:          rec.Type,
			ItemId:        rec.ItemID,
			Date:          rec.CreatedAt,
			UserId:        rec.UserID,
			Severity:      rec.Severity,
		})
	}
	httpx.JSON(w, http.StatusOK, dto.QueryResult[ActivityLogEntryDto]{
		Items:            items,
		TotalRecordCount: total,
	})
}

type BrandingConfigDto struct {
	LoginDisclaimer string `json:"LoginDisclaimer"`
	CustomCss       string `json:"CustomCss"`
}

func (h *Handler) GetBrandingConfiguration(w http.ResponseWriter, r *http.Request) {
	branding := BrandingConfigDto{}
	if h.userRepo != nil {
		if raw, err := h.userRepo.GetServerConfig(r.Context(), "named:branding"); err == nil && raw != "" {
			_ = json.Unmarshal([]byte(raw), &branding)
		}
	}
	httpx.JSON(w, http.StatusOK, branding)
}

func (h *Handler) GetBrandingCSS(w http.ResponseWriter, r *http.Request) {
	branding := BrandingConfigDto{}
	if h.userRepo != nil {
		if raw, err := h.userRepo.GetServerConfig(r.Context(), "named:branding"); err == nil && raw != "" {
			_ = json.Unmarshal([]byte(raw), &branding)
		}
	}
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(branding.CustomCss))
}

type CultureDto struct {
	Name                     string   `json:"Name"`
	DisplayName              string   `json:"DisplayName"`
	TwoLetterISOLanguageName string   `json:"TwoLetterISOLanguageName"`
	ThreeLetterISOLanguageName string `json:"ThreeLetterISOLanguageName"`
	ThreeLetterISOLanguageNames []string `json:"ThreeLetterISOLanguageNames"`
}

func (h *Handler) GetCultures(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, []CultureDto{
		{Name: "Chinese", DisplayName: "中文", TwoLetterISOLanguageName: "zh", ThreeLetterISOLanguageName: "zho", ThreeLetterISOLanguageNames: []string{"zho", "chi"}},
		{Name: "English", DisplayName: "English", TwoLetterISOLanguageName: "en", ThreeLetterISOLanguageName: "eng", ThreeLetterISOLanguageNames: []string{"eng"}},
		{Name: "Japanese", DisplayName: "日本語", TwoLetterISOLanguageName: "ja", ThreeLetterISOLanguageName: "jpn", ThreeLetterISOLanguageNames: []string{"jpn"}},
		{Name: "Korean", DisplayName: "한국어", TwoLetterISOLanguageName: "ko", ThreeLetterISOLanguageName: "kor", ThreeLetterISOLanguageNames: []string{"kor"}},
		{Name: "French", DisplayName: "Français", TwoLetterISOLanguageName: "fr", ThreeLetterISOLanguageName: "fra", ThreeLetterISOLanguageNames: []string{"fra", "fre"}},
		{Name: "German", DisplayName: "Deutsch", TwoLetterISOLanguageName: "de", ThreeLetterISOLanguageName: "deu", ThreeLetterISOLanguageNames: []string{"deu", "ger"}},
		{Name: "Spanish", DisplayName: "Español", TwoLetterISOLanguageName: "es", ThreeLetterISOLanguageName: "spa", ThreeLetterISOLanguageNames: []string{"spa"}},
	})
}

type CountryInfoDto struct {
	Name                     string `json:"Name"`
	DisplayName              string `json:"DisplayName"`
	EnglishName              string `json:"EnglishName"`
	TwoLetterISORegionName   string `json:"TwoLetterISORegionName"`
	ThreeLetterISORegionName string `json:"ThreeLetterISORegionName"`
}

func (h *Handler) GetCountries(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, []CountryInfoDto{
		{Name: "CN", DisplayName: "中国", EnglishName: "China", TwoLetterISORegionName: "CN", ThreeLetterISORegionName: "CHN"},
		{Name: "HK", DisplayName: "中国香港", EnglishName: "Hong Kong", TwoLetterISORegionName: "HK", ThreeLetterISORegionName: "HKG"},
		{Name: "TW", DisplayName: "中国台湾", EnglishName: "Taiwan", TwoLetterISORegionName: "TW", ThreeLetterISORegionName: "TWN"},
		{Name: "US", DisplayName: "United States", EnglishName: "United States", TwoLetterISORegionName: "US", ThreeLetterISORegionName: "USA"},
		{Name: "GB", DisplayName: "United Kingdom", EnglishName: "United Kingdom", TwoLetterISORegionName: "GB", ThreeLetterISORegionName: "GBR"},
		{Name: "JP", DisplayName: "日本", EnglishName: "Japan", TwoLetterISORegionName: "JP", ThreeLetterISORegionName: "JPN"},
		{Name: "KR", DisplayName: "대한민국", EnglishName: "South Korea", TwoLetterISORegionName: "KR", ThreeLetterISORegionName: "KOR"},
	})
}

type LocalizationOptionDto struct {
	Name  string `json:"Name"`
	Value string `json:"Value"`
}

func (h *Handler) GetLocalizationOptions(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, []LocalizationOptionDto{
		{Name: "Chinese (Simplified)", Value: "zh-cn"},
		{Name: "Chinese (Traditional)", Value: "zh-tw"},
		{Name: "English (US)", Value: "en-us"},
		{Name: "Japanese", Value: "ja"},
		{Name: "Korean", Value: "ko"},
	})
}

type ParentalRatingDto struct {
	Name  string `json:"Name"`
	Value int    `json:"Value"`
}

func (h *Handler) GetParentalRatings(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, []ParentalRatingDto{
		{Name: "G", Value: 1},
		{Name: "TV-Y", Value: 1},
		{Name: "TV-G", Value: 1},
		{Name: "PG", Value: 5},
		{Name: "TV-PG", Value: 5},
		{Name: "PG-13", Value: 7},
		{Name: "TV-14", Value: 8},
		{Name: "R", Value: 9},
		{Name: "TV-MA", Value: 9},
		{Name: "NC-17", Value: 10},
	})
}

func (h *Handler) GetRegistration(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "Name")
	httpx.JSON(w, http.StatusOK, dto.RegistrationInfo{
		Name:           name,
		ExpirationDate: "2099-12-31T00:00:00.0000000Z",
		IsTrial:        false,
		IsRegistered:   true,
	})
}

func (h *Handler) GetFeatures(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, []map[string]any{})
}

func (h *Handler) GetPlugins(w http.ResponseWriter, _ *http.Request) {
	if h.registry == nil {
		httpx.JSON(w, http.StatusOK, []plugins.Manifest{})
		return
	}
	httpx.JSON(w, http.StatusOK, h.registry.List())
}

type ScheduledTaskDto struct {
	Name        string `json:"Name"`
	State       string `json:"State"`
	Id          string `json:"Id"`
	Description string `json:"Description"`
	Category    string `json:"Category"`
	IsHidden    bool   `json:"IsHidden"`
	Key         string `json:"Key"`
}

func (h *Handler) GetScheduledTasks(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, []ScheduledTaskDto{
		{
			Name:        "Scan media library",
			State:       "Idle",
			Id:          "refresh-library",
			Description: "Scans your media libraries for new and updated files.",
			Category:    "Library",
			IsHidden:    false,
			Key:         "RefreshLibrary",
		},
		{
			Name:        "Clean Cache Directory",
			State:       "Idle",
			Id:          "clean-cache",
			Description: "Deletes temporary transcoding and image cache files.",
			Category:    "Maintenance",
			IsHidden:    false,
			Key:         "CleanCacheDirectory",
		},
	})
}

func (h *Handler) buildPublicInfo(r *http.Request) dto.PublicSystemInfo {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	host := r.Host
	if host == "" {
		host = "localhost" + h.cfg.HTTPAddr
	}
	return dto.PublicSystemInfo{
		LocalAddress: scheme + "://" + host,
		ServerName:   h.cfg.ServerName,
		Version:      config.ServerVersion,
		ProductName:  config.ProductName,
		Id:           h.cfg.ServerID,
	}
}

func parsePort(addr string, def int) int {
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return def
	}
	p, err := strconv.Atoi(portStr)
	if err != nil || p <= 0 {
		return def
	}
	return p
}

func osDisplayName(goos string) string {
	switch goos {
	case "darwin":
		return "macOS"
	case "linux":
		return "Linux"
	case "windows":
		return "Windows"
	default:
		return goos
	}
}
