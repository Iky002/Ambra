package system

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/core/auth"
)

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
	h.cfgMu.RLock()
	apiKey := h.cfg.TMDBApiKey
	lang := h.cfg.TMDBLanguage
	h.cfgMu.RUnlock()
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
	h.cfgMu.RLock()
	enable302 := h.cfg.EnableSTRM302
	h.cfgMu.RUnlock()
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
	h.cfgMu.RLock()
	lang := strings.TrimSpace(h.cfg.TMDBLanguage)
	completed := h.cfg.IsStartupWizardCompleted
	serverName := h.cfg.ServerName
	hasTMDBKey := strings.TrimSpace(h.cfg.TMDBApiKey) != ""
	h.cfgMu.RUnlock()
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
	h.cfgMu.RLock()
	completed := h.cfg.IsStartupWizardCompleted
	h.cfgMu.RUnlock()
	if completed {
		httpx.Error(w, http.StatusForbidden, "setup wizard is already completed")
		return
	}
	if h.setup.Auth == nil {
		httpx.Error(w, http.StatusInternalServerError, "auth service unavailable")
		return
	}
	if h.userRepo != nil {
		count, err := h.userRepo.CountUsers(r.Context())
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "failed to check existing users")
			return
		}
		if count > 0 {
			ac, ok := middleware.AuthFromContext(r.Context())
			if !ok || ac == nil || !ac.IsAdmin() {
				httpx.Error(w, http.StatusForbidden, "admin user already exists")
				return
			}
		}
	}

	var req SetupAdminRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid json body")
		return
	}

	username := strings.TrimSpace(req.Username)
	if username == "" {
		httpx.Error(w, http.StatusBadRequest, "username is required")
		return
	}
	if len(req.Password) < 4 {
		httpx.Error(w, http.StatusBadRequest, "password must be at least 4 characters")
		return
	}

	tmdbLangChanged := false
	h.cfgMu.Lock()
	if name := strings.TrimSpace(req.ServerName); name != "" {
		h.cfg.ServerName = name
	}
	if lang := strings.TrimSpace(req.Language); lang != "" {
		h.cfg.TMDBLanguage = lang
		tmdbLangChanged = true
	}
	saveErr := h.cfg.Save()
	h.cfgMu.Unlock()
	if saveErr != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to save configuration")
		return
	}
	if country := strings.TrimSpace(req.Country); country != "" && h.userRepo != nil {
		extra := h.loadExtraConfig(r.Context())
		extra["MetadataCountryCode"] = strings.ToUpper(country)
		_ = h.saveExtraConfig(r.Context(), extra)
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
	var req SetupCompleteRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			httpx.Error(w, http.StatusBadRequest, "invalid json body")
			return
		}
	}

	h.cfgMu.Lock()
	if name := strings.TrimSpace(req.ServerName); name != "" {
		h.cfg.ServerName = name
	}
	if req.TMDBApiKey != nil {
		h.cfg.TMDBApiKey = strings.TrimSpace(*req.TMDBApiKey)
	}
	if lang := strings.TrimSpace(req.TMDBLanguage); lang != "" {
		h.cfg.TMDBLanguage = lang
	}
	if req.EnableSTRM302 != nil {
		h.cfg.EnableSTRM302 = *req.EnableSTRM302
	}
	h.cfg.IsStartupWizardCompleted = true
	saveErr := h.cfg.Save()
	h.cfgMu.Unlock()

	if saveErr != nil {
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
