package system

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/api/ws"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/netutil"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

const maskedSecretPlaceholder = "********"

var (
	processLifecycleMu sync.RWMutex
	processRestartFn   func()
	processShutdownFn  func()
	globalPendingRst   atomic.Bool
	globalShuttingDown atomic.Bool
)

// RegisterProcessLifecycleController wires process-level hot-restart and graceful-shutdown callbacks
// (used by cmd/ambra/main.go). Returns an unregister function that clears the callbacks.
func RegisterProcessLifecycleController(onRestart, onShutdown func()) func() {
	processLifecycleMu.Lock()
	processRestartFn = onRestart
	processShutdownFn = onShutdown
	globalPendingRst.Store(false)
	globalShuttingDown.Store(false)
	processLifecycleMu.Unlock()

	return func() {
		processLifecycleMu.Lock()
		processRestartFn = nil
		processShutdownFn = nil
		processLifecycleMu.Unlock()
	}
}

// TriggerProcessRestart marks pending restart and asynchronously invokes any registered process restart hook.
func TriggerProcessRestart() bool {
	globalPendingRst.Store(true)
	processLifecycleMu.RLock()
	fn := processRestartFn
	processLifecycleMu.RUnlock()
	if fn == nil {
		return false
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		fn()
	}()
	return true
}

// TriggerProcessShutdown marks shutting-down state and asynchronously invokes any registered process shutdown hook.
func TriggerProcessShutdown() bool {
	globalShuttingDown.Store(true)
	processLifecycleMu.RLock()
	fn := processShutdownFn
	processLifecycleMu.RUnlock()
	if fn == nil {
		return false
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		fn()
	}()
	return true
}

// ProcessLifecycleState reports global (hasPendingRestart, isShuttingDown, canSelfRestart).
func ProcessLifecycleState() (bool, bool, bool) {
	processLifecycleMu.RLock()
	canRestart := processRestartFn != nil
	processLifecycleMu.RUnlock()
	return globalPendingRst.Load(), globalShuttingDown.Load(), canRestart
}

type Handler struct {
	setupMu       sync.Mutex
	extraConfigMu sync.Mutex
	cfg           *config.Config
	userRepo      *sqlite.UserRepo
	registry      *plugins.Registry
	setup         SetupDeps

	lifecycleMu         sync.RWMutex
	wsHub               *ws.Hub
	onRestart           func()
	onShutdown          func()
	onDLNAConfigChanged func(enabled bool, defaultUserID string)
	pendingRestart      atomic.Bool
	shuttingDown        atomic.Bool

	setupRateMu sync.Mutex
	setupRates  map[string][]time.Time
}

func NewHandler(cfg *config.Config, userRepo *sqlite.UserRepo, registry *plugins.Registry) *Handler {
	return &Handler{
		cfg:        cfg,
		userRepo:   userRepo,
		registry:   registry,
		setupRates: make(map[string][]time.Time),
	}
}

func (h *Handler) WithSetupDeps(deps SetupDeps) *Handler {
	h.setup = deps
	return h
}

// SetWSHub attaches the WebSocket hub for broadcasting ServerRestarting/ServerShuttingDown events
// and hot-updating AllowedOrigins.
func (h *Handler) SetWSHub(wsHub *ws.Hub) {
	h.lifecycleMu.Lock()
	h.wsHub = wsHub
	h.lifecycleMu.Unlock()
}

// SetLifecycleHooks installs optional instance-level restart and shutdown hooks.
func (h *Handler) SetLifecycleHooks(onRestart, onShutdown func()) {
	h.lifecycleMu.Lock()
	h.onRestart = onRestart
	h.onShutdown = onShutdown
	h.lifecycleMu.Unlock()
}

// SetOnDLNAConfigChanged installs an optional hook invoked when EnableDLNA or DLNADefaultUserID changes.
// When unset, changing DLNA settings sets HasPendingRestart=true.
func (h *Handler) SetOnDLNAConfigChanged(fn func(enabled bool, defaultUserID string)) {
	h.lifecycleMu.Lock()
	h.onDLNAConfigChanged = fn
	h.lifecycleMu.Unlock()
}

// SetPendingRestart toggles the instance-level HasPendingRestart flag.
func (h *Handler) SetPendingRestart(v bool) {
	h.pendingRestart.Store(v)
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

	// Authenticated system routes
	r.Group(func(pr chi.Router) {
		pr.Use(authMW.RequireAuth)
		pr.Get("/System/Info", h.GetSystemInfo)
		pr.Get("/System/Configuration", h.GetConfiguration)
		pr.Get("/web/ConfigurationPages", h.GetConfigurationPages)
	})

	// Admin-only system routes
	r.Group(func(ar chi.Router) {
		ar.Use(authMW.RequireAdmin)
		ar.Get("/Features", h.GetFeatures)
		ar.Get("/Plugins", h.GetPlugins)
		ar.Get("/System/ActivityLog/Entries", h.GetActivityLogEntries)
		ar.Post("/System/Configuration", h.UpdateConfiguration)
		ar.Post("/System/Configuration/Partial", h.UpdateConfigurationPartial)
		ar.Get("/System/Configuration/{Key}", h.GetNamedConfiguration)
		ar.Post("/System/Configuration/{Key}", h.UpdateNamedConfiguration)
		ar.Post("/System/Restart", h.RestartServer)
		ar.Post("/System/Shutdown", h.ShutdownServer)
	})
}

type systemInfoResponse struct {
	dto.PublicSystemInfo
	OperatingSystem            string `json:"OperatingSystem"`
	OperatingSystemDisplayName string `json:"OperatingSystemDisplayName"`
	HasPendingRestart          bool   `json:"HasPendingRestart"`
	IsShuttingDown             bool   `json:"IsShuttingDown"`
	SupportsLibraryMonitor     bool   `json:"SupportsLibraryMonitor"`
	WebSocketPortNumber        int    `json:"WebSocketPortNumber"`
	CanSelfRestart             bool   `json:"CanSelfRestart"`
	CanSelfUpdate              bool   `json:"CanSelfUpdate"`
	CanLaunchWebBrowser        bool   `json:"CanLaunchWebBrowser"`
	ProgramDataPath            string `json:"ProgramDataPath,omitempty"`
	CachePath                  string `json:"CachePath,omitempty"`
	ChapterCachePath           string `json:"ChapterCachePath,omitempty"`
	BifCachePath               string `json:"BifCachePath,omitempty"`
	LogPath                    string `json:"LogPath,omitempty"`
	InternalMetadataPath       string `json:"InternalMetadataPath,omitempty"`
	TranscodingTempPath        string `json:"TranscodingTempPath,omitempty"`
	HttpServerPortNumber       int    `json:"HttpServerPortNumber"`
	SupportsHttps              bool   `json:"SupportsHttps"`
	HasUpdateAvailable         bool   `json:"HasUpdateAvailable"`
	EncoderLocationType        string `json:"EncoderLocationType"`
	SystemArchitecture         string `json:"SystemArchitecture"`
	StartupWizardCompleted     bool   `json:"StartupWizardCompleted"`
}

func (h *Handler) GetPublicSystemInfo(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, h.buildPublicInfo(r))
}

func (h *Handler) GetSystemInfo(w http.ResponseWriter, r *http.Request) {
	ac, ok := middleware.AuthFromContext(r.Context())
	isAdmin := ok && ac != nil && ac.IsAdmin()

	snap := h.cfg.Snapshot()
	httpAddr := snap.HTTPAddr
	dataDir := snap.DataDir
	transcodeTempDir := (&snap).TranscodeTempDir()
	wizardCompleted := snap.IsStartupWizardCompleted

	procPending, procShutting, procCanRestart := ProcessLifecycleState()
	h.lifecycleMu.RLock()
	hasRestartHook := h.onRestart != nil
	h.lifecycleMu.RUnlock()

	port := parsePort(httpAddr, 8096)
	info := systemInfoResponse{
		PublicSystemInfo:           h.buildPublicInfo(r),
		OperatingSystem:            runtime.GOOS,
		OperatingSystemDisplayName: osDisplayName(runtime.GOOS),
		HasPendingRestart:          h.pendingRestart.Load() || procPending,
		IsShuttingDown:             h.shuttingDown.Load() || procShutting,
		SupportsLibraryMonitor:     true,
		WebSocketPortNumber:        port,
		CanSelfRestart:             hasRestartHook || procCanRestart,
		CanSelfUpdate:              false,
		CanLaunchWebBrowser:        false,
		HttpServerPortNumber:       port,
		SupportsHttps:              false,
		HasUpdateAvailable:         false,
		EncoderLocationType:        "System",
		SystemArchitecture:         runtime.GOARCH,
		StartupWizardCompleted:     wizardCompleted,
	}
	if isAdmin {
		info.ProgramDataPath = dataDir
		info.CachePath = filepath.Join(dataDir, "cache")
		info.ChapterCachePath = (&snap).ChapterCacheDir()
		info.BifCachePath = (&snap).BifCacheDir()
		info.LogPath = filepath.Join(dataDir, "logs")
		info.InternalMetadataPath = filepath.Join(dataDir, "metadata")
		info.TranscodingTempPath = transcodeTempDir
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

const systemExtraConfigKey = "system:config_extra"

type ServerConfigurationDto struct {
	ServerName                string   `json:"ServerName"`
	IsStartupWizardCompleted  bool     `json:"IsStartupWizardCompleted"`
	TMDBApiKey                string   `json:"TMDBApiKey,omitempty"`
	HasTMDBApiKey             bool     `json:"HasTMDBApiKey,omitempty"`
	TMDBLanguage              string   `json:"TMDBLanguage,omitempty"`
	EnableSTRM302             bool     `json:"EnableSTRM302"`
	EnableDLNA                bool     `json:"EnableDLNA"`
	DLNADefaultUserID         string   `json:"DLNADefaultUserID,omitempty"`
	EnableQuickConnect        bool     `json:"EnableQuickConnect"`
	EnableLoginUserList       bool     `json:"EnableLoginUserList"`
	EnableRemoteAccess        bool     `json:"EnableRemoteAccess"`
	AllowedOrigins            []string `json:"AllowedOrigins,omitempty"`
	TrustedProxies            []string `json:"TrustedProxies,omitempty"`
	LocalNetworkSubnets       []string `json:"LocalNetworkSubnets,omitempty"`
	SimultaneousStreamLimit   int      `json:"SimultaneousStreamLimit,omitempty"`
	RemoteClientBitrateLimit  int      `json:"RemoteClientBitrateLimit,omitempty"`
	FFmpegPath                string   `json:"FFmpegPath,omitempty"`
	FFprobePath               string   `json:"FFprobePath,omitempty"`
	PlayedThreshold           float64  `json:"PlayedThreshold"`
	PreferredMetadataLanguage string   `json:"PreferredMetadataLanguage"`
	MetadataCountryCode       string   `json:"MetadataCountryCode"`
	UICulture                 string   `json:"UICulture"`
}

func (h *Handler) loadExtraConfigLocked(ctx context.Context) map[string]any {
	extra := map[string]any{}
	if h.userRepo == nil {
		return extra
	}
	if raw, err := h.userRepo.GetServerConfig(ctx, systemExtraConfigKey); err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &extra)
	}
	return extra
}

func (h *Handler) saveExtraConfigLocked(ctx context.Context, extra map[string]any) error {
	if h.userRepo == nil {
		return nil
	}
	raw, err := json.Marshal(extra)
	if err != nil {
		return err
	}
	return h.userRepo.SetServerConfig(ctx, systemExtraConfigKey, string(raw))
}

func (h *Handler) loadExtraConfig(ctx context.Context) map[string]any {
	h.extraConfigMu.Lock()
	defer h.extraConfigMu.Unlock()
	return h.loadExtraConfigLocked(ctx)
}

func (h *Handler) saveExtraConfig(ctx context.Context, extra map[string]any) error {
	h.extraConfigMu.Lock()
	defer h.extraConfigMu.Unlock()
	return h.saveExtraConfigLocked(ctx, extra)
}

func stringFromMap(m map[string]any, key, fallback string) string {
	if v, ok := m[key].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return fallback
}

func nonNilStrings(in []string) []string {
	if len(in) == 0 {
		return []string{}
	}
	return append([]string(nil), in...)
}

func parseStringSliceField(raw any) ([]string, bool) {
	switch v := raw.(type) {
	case nil:
		return []string{}, true
	case []string:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s := strings.TrimSpace(item); s != "" {
				out = append(out, s)
			}
		}
		return out, true
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, false
			}
			if trimmed := strings.TrimSpace(s); trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out, true
	case string:
		out := make([]string, 0)
		for _, part := range strings.Split(v, ",") {
			if s := strings.TrimSpace(part); s != "" {
				out = append(out, s)
			}
		}
		return out, true
	default:
		return nil, false
	}
}

func parseIntField(raw any) (int, bool) {
	switch v := raw.(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	case int64:
		return int(v), true
	default:
		return 0, false
	}
}

func (h *Handler) GetConfiguration(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	isAdmin := false
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && ac.IsAdmin() {
		isAdmin = true
	}

	snap := h.cfg.Snapshot()
	serverName := snap.ServerName
	wizardCompleted := snap.IsStartupWizardCompleted
	hasTMDBKey := strings.TrimSpace(snap.TMDBApiKey) != ""
	tmdbLang := snap.TMDBLanguage
	enableSTRM302 := snap.EnableSTRM302
	playedThreshold := snap.PlayedThreshold

	extra := h.loadExtraConfig(r.Context())
	out := make(map[string]any, 24)
	if isAdmin {
		for k, v := range extra {
			out[k] = v
		}
	}
	out["ServerName"] = serverName
	out["IsStartupWizardCompleted"] = wizardCompleted
	if isAdmin {
		out["HasTMDBApiKey"] = hasTMDBKey
		if hasTMDBKey {
			out["TMDBApiKey"] = maskedSecretPlaceholder
		} else {
			delete(out, "TMDBApiKey")
		}
		out["EnableDLNA"] = snap.EnableDLNA
		out["DLNADefaultUserID"] = snap.DLNADefaultUserID
		out["EnableQuickConnect"] = snap.EnableQuickConnect
		out["EnableLoginUserList"] = snap.EnableLoginUserList
		out["EnableRemoteAccess"] = snap.IsGlobalRemoteAccessEnabled()
		out["AllowedOrigins"] = nonNilStrings(snap.AllowedOrigins)
		out["TrustedProxies"] = nonNilStrings(snap.TrustedProxies)
		out["LocalNetworkSubnets"] = nonNilStrings(snap.LocalNetworkSubnets)
		out["SimultaneousStreamLimit"] = snap.SimultaneousStreamLimit
		out["RemoteClientBitrateLimit"] = snap.RemoteClientBitrateLimit
		out["FFmpegPath"] = snap.FFmpegPath
		out["FFprobePath"] = snap.FFprobePath
	} else {
		delete(out, "TMDBApiKey")
		delete(out, "HasTMDBApiKey")
	}
	if tmdbLang != "" {
		out["TMDBLanguage"] = tmdbLang
	}
	out["EnableSTRM302"] = enableSTRM302
	out["PlayedThreshold"] = playedThreshold
	out["PreferredMetadataLanguage"] = stringFromMap(extra, "PreferredMetadataLanguage", "zh")
	out["MetadataCountryCode"] = stringFromMap(extra, "MetadataCountryCode", "CN")
	out["UICulture"] = stringFromMap(extra, "UICulture", "zh-cn")
	httpx.JSON(w, http.StatusOK, out)
}

func isTypedServerConfigKey(k string) bool {
	switch k {
	case "ServerName", "IsStartupWizardCompleted", "TMDBApiKey", "HasTMDBApiKey", "TMDBLanguage",
		"EnableSTRM302", "PlayedThreshold", "EnableDLNA", "DLNADefaultUserID", "EnableQuickConnect",
		"EnableLoginUserList", "EnableRemoteAccess", "AllowedOrigins", "TrustedProxies", "LocalNetworkSubnets",
		"SimultaneousStreamLimit", "RemoteClientBitrateLimit", "FFmpegPath", "FFprobePath",
		"HTTPAddr", "HttpServerPortNumber":
		return true
	default:
		return false
	}
}

func (h *Handler) UpdateConfiguration(w http.ResponseWriter, r *http.Request) {
	h.applyConfigurationUpdate(w, r)
}

// UpdateConfigurationPartial handles POST /System/Configuration/Partial with incremental merge semantics.
func (h *Handler) UpdateConfigurationPartial(w http.ResponseWriter, r *http.Request) {
	h.applyConfigurationUpdate(w, r)
}

func (h *Handler) applyConfigurationUpdate(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := httpx.DecodeJSONLimit(r, &body, 0); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid json body")
		return
	}
	tmdbChanged := false
	strmChanged := false
	proxiesChanged := false
	subnetsChanged := false
	originsChanged := false
	httpAddrChanged := false
	dlnaChanged := false
	var newEnableDLNA bool
	var newDLNADefaultUserID string
	var newProxies, newSubnets, newOrigins []string

	h.cfg.Update(func(c *config.Config) {
		if v, ok := body["ServerName"].(string); ok && strings.TrimSpace(v) != "" && !containsControl(v) {
			c.ServerName = strings.TrimSpace(v)
		}
		if v, ok := body["IsStartupWizardCompleted"].(bool); ok {
			c.IsStartupWizardCompleted = v
		}
		if v, ok := body["TMDBApiKey"].(string); ok {
			trimmed := strings.TrimSpace(v)
			if trimmed != maskedSecretPlaceholder {
				c.TMDBApiKey = trimmed
				tmdbChanged = true
			}
		}
		if v, ok := body["TMDBLanguage"].(string); ok && strings.TrimSpace(v) != "" {
			c.TMDBLanguage = strings.TrimSpace(v)
			tmdbChanged = true
		}
		if v, ok := body["EnableSTRM302"].(bool); ok {
			c.EnableSTRM302 = v
			strmChanged = true
		}
		if v, ok := body["PlayedThreshold"].(float64); ok && v > 0 && v <= 1 {
			c.PlayedThreshold = v
		}
		if v, ok := body["HTTPAddr"].(string); ok {
			trimmed := strings.TrimSpace(v)
			if trimmed != "" && !containsControl(trimmed) && trimmed != c.HTTPAddr {
				c.HTTPAddr = trimmed
				httpAddrChanged = true
			}
		}
		if raw, exists := body["HttpServerPortNumber"]; exists {
			if port, ok := parseIntField(raw); ok && port > 0 && port <= 65535 {
				if port != parsePort(c.HTTPAddr, 8096) {
					host := ""
					if hPart, _, err := net.SplitHostPort(c.HTTPAddr); err == nil {
						host = hPart
					}
					c.HTTPAddr = net.JoinHostPort(host, strconv.Itoa(port))
					httpAddrChanged = true
				}
			}
		}
		if v, ok := body["EnableDLNA"].(bool); ok {
			if c.EnableDLNA != v {
				c.EnableDLNA = v
				dlnaChanged = true
			}
		}
		if v, ok := body["DLNADefaultUserID"].(string); ok && !containsControl(v) {
			trimmed := strings.TrimSpace(v)
			if c.DLNADefaultUserID != trimmed {
				c.DLNADefaultUserID = trimmed
				dlnaChanged = true
			}
		}
		newEnableDLNA = c.EnableDLNA
		newDLNADefaultUserID = c.DLNADefaultUserID
		if v, ok := body["EnableQuickConnect"].(bool); ok {
			c.EnableQuickConnect = v
		}
		if v, ok := body["EnableLoginUserList"].(bool); ok {
			c.EnableLoginUserList = v
		}
		if v, ok := body["EnableRemoteAccess"].(bool); ok {
			b := v
			c.EnableRemoteAccessGlobal = &b
		}
		if raw, exists := body["AllowedOrigins"]; exists {
			if list, ok := parseStringSliceField(raw); ok {
				c.AllowedOrigins = list
				newOrigins = append([]string(nil), list...)
				originsChanged = true
			}
		}
		if raw, exists := body["TrustedProxies"]; exists {
			if list, ok := parseStringSliceField(raw); ok {
				c.TrustedProxies = list
				newProxies = append([]string(nil), list...)
				proxiesChanged = true
			}
		}
		if raw, exists := body["LocalNetworkSubnets"]; exists {
			if list, ok := parseStringSliceField(raw); ok {
				c.LocalNetworkSubnets = list
				newSubnets = append([]string(nil), list...)
				subnetsChanged = true
			}
		}
		if raw, exists := body["SimultaneousStreamLimit"]; exists {
			if n, ok := parseIntField(raw); ok && n >= 0 {
				c.SimultaneousStreamLimit = n
			}
		}
		if raw, exists := body["RemoteClientBitrateLimit"]; exists {
			if n, ok := parseIntField(raw); ok && n >= 0 {
				c.RemoteClientBitrateLimit = n
			}
		}
		// Security (M-03): FFmpegPath and FFprobePath are intentionally read-only via HTTP API
		// (only configurable via CLI flags, environment variables, or local config file).
	})
	saveErr := h.cfg.Save()

	if saveErr != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to save configuration")
		return
	}

	if httpAddrChanged {
		h.SetPendingRestart(true)
	}
	if dlnaChanged {
		h.lifecycleMu.RLock()
		dlnaFn := h.onDLNAConfigChanged
		h.lifecycleMu.RUnlock()
		if dlnaFn != nil {
			dlnaFn(newEnableDLNA, newDLNADefaultUserID)
		} else {
			h.SetPendingRestart(true)
		}
	}
	if proxiesChanged {
		netutil.SetTrustedProxies(newProxies)
	}
	if subnetsChanged {
		netutil.SetLocalNetworkSubnets(newSubnets)
	}
	if originsChanged {
		h.lifecycleMu.RLock()
		wsHub := h.wsHub
		h.lifecycleMu.RUnlock()
		if wsHub != nil {
			wsHub.SetAllowedOrigins(newOrigins)
		}
	}

	h.extraConfigMu.Lock()
	extra := h.loadExtraConfigLocked(r.Context())
	extraDirty := false
	for k, v := range body {
		if isTypedServerConfigKey(k) {
			if _, hadLegacy := extra[k]; hadLegacy {
				delete(extra, k)
				extraDirty = true
			}
			continue
		}
		switch k {
		case "PreferredMetadataLanguage", "MetadataCountryCode", "UICulture":
			if s, ok := v.(string); ok {
				extra[k] = strings.TrimSpace(s)
				extraDirty = true
			}
		default:
			extra[k] = v
			extraDirty = true
		}
	}
	var extraErr error
	if extraDirty {
		extraErr = h.saveExtraConfigLocked(r.Context(), extra)
	}
	h.extraConfigMu.Unlock()
	if extraErr != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to save extended configuration")
		return
	}

	if tmdbChanged {
		h.syncTMDBPlugin()
	}
	if strmChanged {
		h.syncSTRMPlugin()
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
	if key == "partial" {
		h.UpdateConfigurationPartial(w, r)
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

	severity := strings.TrimSpace(q.Get("Severity"))
	searchTerm := strings.TrimSpace(q.Get("SearchTerm"))
	if searchTerm == "" {
		searchTerm = strings.TrimSpace(q.Get("q"))
	}

	if h.userRepo == nil {
		httpx.JSON(w, http.StatusOK, dto.QueryResult[ActivityLogEntryDto]{
			Items:            []ActivityLogEntryDto{},
			TotalRecordCount: 0,
		})
		return
	}

	recs, total, err := h.userRepo.ListActivityLogsFiltered(r.Context(), startIndex, limit, minDate, severity, searchTerm)
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
	Name                        string   `json:"Name"`
	DisplayName                 string   `json:"DisplayName"`
	TwoLetterISOLanguageName    string   `json:"TwoLetterISOLanguageName"`
	ThreeLetterISOLanguageName  string   `json:"ThreeLetterISOLanguageName"`
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
		{Name: "Portuguese", DisplayName: "Português", TwoLetterISOLanguageName: "pt", ThreeLetterISOLanguageName: "por", ThreeLetterISOLanguageNames: []string{"por"}},
		{Name: "Thai", DisplayName: "ไทย", TwoLetterISOLanguageName: "th", ThreeLetterISOLanguageName: "tha", ThreeLetterISOLanguageNames: []string{"tha"}},
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
		{Name: "ES", DisplayName: "España", EnglishName: "Spain", TwoLetterISORegionName: "ES", ThreeLetterISORegionName: "ESP"},
		{Name: "DE", DisplayName: "Deutschland", EnglishName: "Germany", TwoLetterISORegionName: "DE", ThreeLetterISORegionName: "DEU"},
		{Name: "FR", DisplayName: "France", EnglishName: "France", TwoLetterISORegionName: "FR", ThreeLetterISORegionName: "FRA"},
		{Name: "BR", DisplayName: "Brasil", EnglishName: "Brazil", TwoLetterISORegionName: "BR", ThreeLetterISORegionName: "BRA"},
		{Name: "TH", DisplayName: "ประเทศไทย", EnglishName: "Thailand", TwoLetterISORegionName: "TH", ThreeLetterISORegionName: "THA"},
	})
}

type LocalizationOptionDto struct {
	Name  string `json:"Name"`
	Value string `json:"Value"`
}

func (h *Handler) GetLocalizationOptions(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, []LocalizationOptionDto{
		{Name: "Chinese (Simplified)", Value: "zh-CN"},
		{Name: "Chinese (Traditional)", Value: "zh-TW"},
		{Name: "English (US)", Value: "en-US"},
		{Name: "Japanese", Value: "ja-JP"},
		{Name: "Korean", Value: "ko-KR"},
		{Name: "Spanish", Value: "es-ES"},
		{Name: "German", Value: "de-DE"},
		{Name: "French", Value: "fr-FR"},
		{Name: "Portuguese (Brazil)", Value: "pt-BR"},
		{Name: "Thai", Value: "th-TH"},
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

// GetConfigurationPages serves GET /web/ConfigurationPages with a valid empty JSON array.
func (h *Handler) GetConfigurationPages(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, []map[string]any{})
}

// RestartServer handles POST /System/Restart: marks pending restart, broadcasts ServerRestarting,
// and triggers any registered instance or process hot-reload callback.
func (h *Handler) RestartServer(w http.ResponseWriter, r *http.Request) {
	h.pendingRestart.Store(true)
	if h.userRepo != nil {
		_ = h.userRepo.CreateActivityLog(r.Context(), &sqlite.ActivityLogRecord{
			Name:     "Server restart requested",
			Type:     "ServerRestart",
			Severity: "Info",
		})
	}
	h.lifecycleMu.RLock()
	wsHub := h.wsHub
	onRestart := h.onRestart
	h.lifecycleMu.RUnlock()
	if wsHub != nil {
		wsHub.Broadcast(ws.Message{MessageType: "ServerRestarting"})
	}
	if onRestart != nil {
		go func() {
			time.Sleep(100 * time.Millisecond)
			onRestart()
		}()
	} else {
		TriggerProcessRestart()
	}
	httpx.NoContent(w)
}

// ShutdownServer handles POST /System/Shutdown: marks shutting-down state, broadcasts ServerShuttingDown,
// and triggers any registered instance or process shutdown callback.
func (h *Handler) ShutdownServer(w http.ResponseWriter, r *http.Request) {
	h.shuttingDown.Store(true)
	if h.userRepo != nil {
		_ = h.userRepo.CreateActivityLog(r.Context(), &sqlite.ActivityLogRecord{
			Name:     "Server shutdown requested",
			Type:     "ServerShutdown",
			Severity: "Info",
		})
	}
	h.lifecycleMu.RLock()
	wsHub := h.wsHub
	onShutdown := h.onShutdown
	h.lifecycleMu.RUnlock()
	if wsHub != nil {
		wsHub.Broadcast(ws.Message{MessageType: "ServerShuttingDown"})
	}
	if onShutdown != nil {
		go func() {
			time.Sleep(100 * time.Millisecond)
			onShutdown()
		}()
	} else {
		TriggerProcessShutdown()
	}
	httpx.NoContent(w)
}

func (h *Handler) GetPlugins(w http.ResponseWriter, _ *http.Request) {
	if h.registry == nil {
		httpx.JSON(w, http.StatusOK, []plugins.Manifest{})
		return
	}
	httpx.JSON(w, http.StatusOK, h.registry.List())
}

func containsControl(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func isValidDNSName(h string) bool {
	if len(h) == 0 || len(h) > 253 {
		return false
	}
	if strings.HasPrefix(h, ".") || strings.HasSuffix(h, ".") {
		return false
	}
	labels := strings.Split(h, ".")
	for _, lbl := range labels {
		if len(lbl) == 0 || len(lbl) > 63 {
			return false
		}
		if lbl[0] == '-' || lbl[len(lbl)-1] == '-' {
			return false
		}
		for i := 0; i < len(lbl); i++ {
			ch := lbl[i]
			if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-') {
				return false
			}
		}
	}
	return true
}

func sanitizeRequestHost(rawHost, fallbackHTTPAddr string) string {
	fallback := "localhost:" + strconv.Itoa(parsePort(fallbackHTTPAddr, 8096))
	s := strings.TrimSpace(rawHost)
	if s == "" || len(s) > 255 || containsControl(s) {
		return fallback
	}
	if strings.ContainsAny(s, " \t/@?#\\%'\"<>") {
		return fallback
	}

	if hostPart, portPart, err := net.SplitHostPort(s); err == nil {
		portNum, pErr := strconv.Atoi(portPart)
		if pErr != nil || portNum < 1 || portNum > 65535 {
			return fallback
		}
		if addr, ipErr := netip.ParseAddr(hostPart); ipErr == nil {
			if addr.Is6() {
				return "[" + addr.String() + "]:" + strconv.Itoa(portNum)
			}
			return addr.String() + ":" + strconv.Itoa(portNum)
		}
		if isValidDNSName(hostPart) {
			return hostPart + ":" + strconv.Itoa(portNum)
		}
		return fallback
	}

	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		inner := s[1 : len(s)-1]
		if addr, ipErr := netip.ParseAddr(inner); ipErr == nil && addr.Is6() {
			return "[" + addr.String() + "]"
		}
		return fallback
	}
	if addr, ipErr := netip.ParseAddr(s); ipErr == nil && addr.Is4() {
		return addr.String()
	}
	if isValidDNSName(s) {
		return s
	}
	return fallback
}

func (h *Handler) buildPublicInfo(r *http.Request) dto.PublicSystemInfo {
	snap := h.cfg.Snapshot()
	httpAddr := snap.HTTPAddr
	serverName := snap.ServerName
	serverID := snap.ServerID

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	} else if strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		remoteHost := strings.TrimSpace(r.RemoteAddr)
		if hPart, _, err := net.SplitHostPort(remoteHost); err == nil {
			remoteHost = hPart
		}
		remoteHost = strings.Trim(remoteHost, "[]")
		if addr, err := netip.ParseAddr(remoteHost); err == nil && netutil.IsTrustedProxyIP(addr) {
			scheme = "https"
		}
	}
	host := sanitizeRequestHost(r.Host, httpAddr)
	return dto.PublicSystemInfo{
		LocalAddress: scheme + "://" + host,
		ServerName:   serverName,
		Version:      config.ServerVersion,
		AmbraVersion: config.AmbraVersion,
		ProductName:  config.ProductName,
		Id:           serverID,
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
