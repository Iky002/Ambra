package system

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/pkg/netutil"
)

const (
	setupAdminRateLimit  = 30
	setupAdminRateWindow = time.Minute
)

func (h *Handler) checkSetupRateLimit(r *http.Request) bool {
	ip := strings.TrimSpace(netutil.ClientIP(r))
	if ip == "" {
		return true
	}
	if addr, err := netip.ParseAddr(ip); err == nil && addr.Unmap().IsLoopback() {
		return true
	}
	now := time.Now().UTC()
	cutoff := now.Add(-setupAdminRateWindow)

	h.setupRateMu.Lock()
	defer h.setupRateMu.Unlock()
	if h.setupRates == nil {
		h.setupRates = make(map[string][]time.Time)
	}
	for k, ts := range h.setupRates {
		kept := ts[:0]
		for _, t := range ts {
			if t.After(cutoff) {
				kept = append(kept, t)
			}
		}
		if len(kept) == 0 {
			delete(h.setupRates, k)
		} else {
			h.setupRates[k] = kept
		}
	}
	if len(h.setupRates[ip]) >= setupAdminRateLimit {
		return false
	}
	h.setupRates[ip] = append(h.setupRates[ip], now)
	return true
}

const (
	tmdbPluginID = "ambra.builtin.tmdb"
	strmPluginID = "ambra.builtin.strm"
)

// SetupDeps holds optional dependencies used by the Setup Wizard endpoints.
type SetupDeps struct {
	Auth         *auth.Service
	TriggerScan  func(ctx context.Context)
	SyncRankings func(ctx context.Context)
}

type SetupStatusResponse struct {
	Completed       bool   `json:"completed"`
	ServerName      string `json:"server_name"`
	DefaultLanguage string `json:"default_language"`
	HasUsers        bool   `json:"has_users"`
	HasTMDBKey      bool   `json:"has_tmdb_key"`
}

type SetupAdminRequest struct {
	ServerName string `json:"server_name"`
	Language   string `json:"language"`
	Country    string `json:"country"`
	Username   string `json:"username"`
	Password   string `json:"password"`
}

type SetupCompleteRequest struct {
	ServerName    string  `json:"server_name"`
	TMDBApiKey    *string `json:"tmdb_api_key"`
	TMDBLanguage  string  `json:"tmdb_language"`
	EnableSTRM302 *bool   `json:"enable_strm_302"`
	TriggerScan   bool    `json:"trigger_scan"`
	SyncRankings  bool    `json:"sync_rankings"`
}

// RegisterSetupRoutes registers the Web UI Setup Wizard endpoints under /api/setup/*.
func RegisterSetupRoutes(r chi.Router, authMW *middleware.Auth, h *Handler) {
	r.Get("/api/setup/status", h.GetSetupStatus)
	r.Post("/api/setup/admin", h.SetupAdmin)

	r.Group(func(ar chi.Router) {
		ar.Use(authMW.RequireAdmin)
		ar.Post("/api/setup/complete", h.CompleteSetup)
	})
}

func (h *Handler) syncTMDBPlugin() {
	if h.registry == nil || h.cfg == nil {
		return
	}
	snap := h.cfg.Snapshot()
	apiKey := snap.TMDBApiKey
	lang := snap.TMDBLanguage
	if p, ok := h.registry.Get(tmdbPluginID); ok {
		_ = p.Configure(map[string]any{
			"api_key":  apiKey,
			"language": lang,
		})
	}
}

func (h *Handler) syncSTRMPlugin() {
	if h.registry == nil || h.cfg == nil {
		return
	}
	enable302 := h.cfg.Snapshot().EnableSTRM302
	if p, ok := h.registry.Get(strmPluginID); ok {
		_ = p.Configure(map[string]any{
			"enable_302": enable302,
		})
	}
}

func (h *Handler) GetSetupStatus(w http.ResponseWriter, r *http.Request) {
	hasUsers := false
	if h.userRepo != nil {
		if users, err := h.userRepo.ListUsers(r.Context()); err == nil && len(users) > 0 {
			hasUsers = true
		}
	}
	snap := h.cfg.Snapshot()
	lang := strings.TrimSpace(snap.TMDBLanguage)
	completed := snap.IsStartupWizardCompleted
	serverName := snap.ServerName
	hasTMDBKey := strings.TrimSpace(snap.TMDBApiKey) != ""
	if lang == "" {
		lang = "zh-CN"
	}
	httpx.JSON(w, http.StatusOK, SetupStatusResponse{
		Completed:       completed,
		ServerName:      serverName,
		DefaultLanguage: lang,
		HasUsers:        hasUsers,
		HasTMDBKey:      hasTMDBKey,
	})
}

func (h *Handler) SetupAdmin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	if !h.checkSetupRateLimit(r) {
		httpx.Error(w, http.StatusTooManyRequests, "too many setup attempts")
		return
	}
	h.setupMu.Lock()
	defer h.setupMu.Unlock()

	if h.cfg.Snapshot().IsStartupWizardCompleted {
		httpx.Error(w, http.StatusForbidden, "setup wizard is already completed")
		return
	}
	if h.setup.Auth == nil {
		httpx.Error(w, http.StatusInternalServerError, "auth service unavailable")
		return
	}

	ac, hasAC := middleware.AuthFromContext(r.Context())
	isAuthAdmin := hasAC && ac != nil && ac.IsAdmin()

	if h.userRepo != nil {
		count, err := h.userRepo.CountUsers(r.Context())
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "failed to check existing users")
			return
		}
		if count > 0 && !isAuthAdmin {
			httpx.Error(w, http.StatusForbidden, "admin user already exists")
			return
		}
	}

	var req SetupAdminRequest
	if err := httpx.DecodeJSONLimit(r, &req, 0); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid json body")
		return
	}

	if auth.ContainsControlChar(req.Username) || auth.ContainsControlChar(req.ServerName) {
		httpx.Error(w, http.StatusBadRequest, "invalid control characters in username or server name")
		return
	}
	username := strings.TrimSpace(req.Username)
	if username == "" || len([]rune(username)) > 64 {
		httpx.Error(w, http.StatusBadRequest, "username is required")
		return
	}
	if len(req.Password) < 4 {
		httpx.Error(w, http.StatusBadRequest, "password must be at least 4 characters")
		return
	}

	tmdbLangChanged := false
	h.cfg.Update(func(c *config.Config) {
		if name := strings.TrimSpace(req.ServerName); name != "" {
			c.ServerName = name
		}
		if lang := strings.TrimSpace(req.Language); lang != "" {
			c.TMDBLanguage = lang
			tmdbLangChanged = true
		}
	})
	if saveErr := h.cfg.Save(); saveErr != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to save configuration")
		return
	}
	if country := strings.TrimSpace(req.Country); country != "" && h.userRepo != nil {
		h.extraConfigMu.Lock()
		extra := h.loadExtraConfigLocked(r.Context())
		extra["MetadataCountryCode"] = strings.ToUpper(country)
		_ = h.saveExtraConfigLocked(r.Context(), extra)
		h.extraConfigMu.Unlock()
	}
	if tmdbLangChanged {
		h.syncTMDBPlugin()
	}
	h.syncSTRMPlugin()

	ctx := r.Context()
	if _, err := h.setup.Auth.CreateUser(ctx, username, req.Password, true); err != nil {
		if !errors.Is(err, auth.ErrUserExists) {
			httpx.Error(w, http.StatusInternalServerError, "failed to create admin user")
			return
		}
		if !isAuthAdmin {
			httpx.Error(w, http.StatusForbidden, "admin user already exists")
			return
		}
		if h.userRepo == nil {
			httpx.Error(w, http.StatusInternalServerError, "user repository unavailable")
			return
		}
		existing, lookupErr := h.userRepo.GetUserByName(ctx, username)
		if lookupErr != nil {
			httpx.Error(w, http.StatusInternalServerError, "failed to load existing user")
			return
		}
		if pwErr := h.setup.Auth.ChangePassword(ctx, existing.ID, "", req.Password, true); pwErr != nil {
			httpx.Error(w, http.StatusInternalServerError, "failed to update admin password")
			return
		}
		if !existing.IsAdmin || !existing.Policy.IsAdministrator || existing.IsDisabled || existing.Policy.IsDisabled {
			policy := existing.Policy
			policy.IsAdministrator = true
			policy.IsDisabled = false
			if polErr := h.userRepo.UpdateUserPolicy(ctx, existing.ID, policy); polErr != nil {
				httpx.Error(w, http.StatusInternalServerError, "failed to promote admin user")
				return
			}
			h.setup.Auth.InvalidateUserCache(existing.ID)
		}
	}

	clientInfo := middleware.ExtractClientInfo(r)
	if clientInfo.Client == "" {
		clientInfo.Client = "Ambra Web Setup"
	}
	authRes, err := h.setup.Auth.AuthenticateByName(ctx, username, req.Password, "", clientInfo)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to authenticate admin user")
		return
	}

	httpx.JSON(w, http.StatusOK, authRes)
}

func (h *Handler) CompleteSetup(w http.ResponseWriter, r *http.Request) {
	if h.userRepo != nil {
		count, err := h.userRepo.CountUsers(r.Context())
		if err == nil && count == 0 {
			httpx.Error(w, http.StatusBadRequest, "at least one administrator account must be created before completing setup")
			return
		}
	}
	var req SetupCompleteRequest
	if r.Body != nil {
		if err := httpx.DecodeJSONLimit(r, &req, 0); err != nil && !errors.Is(err, io.EOF) {
			httpx.Error(w, http.StatusBadRequest, "invalid json body")
			return
		}
	}

	h.cfg.Update(func(c *config.Config) {
		if name := strings.TrimSpace(req.ServerName); name != "" && !auth.ContainsControlChar(name) {
			c.ServerName = name
		}
		if req.TMDBApiKey != nil {
			if trimmed := strings.TrimSpace(*req.TMDBApiKey); trimmed != maskedSecretPlaceholder {
				c.TMDBApiKey = trimmed
			}
		}
		if lang := strings.TrimSpace(req.TMDBLanguage); lang != "" {
			c.TMDBLanguage = lang
		}
		if req.EnableSTRM302 != nil {
			c.EnableSTRM302 = *req.EnableSTRM302
		}
		c.IsStartupWizardCompleted = true
	})

	if saveErr := h.cfg.Save(); saveErr != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to save configuration")
		return
	}
	h.syncTMDBPlugin()
	h.syncSTRMPlugin()

	if req.TriggerScan && h.setup.TriggerScan != nil {
		go h.setup.TriggerScan(context.Background())
	}
	if req.SyncRankings && h.setup.SyncRankings != nil {
		go h.setup.SyncRankings(context.Background())
	}

	httpx.JSON(w, http.StatusOK, map[string]bool{
		"completed": true,
	})
}
