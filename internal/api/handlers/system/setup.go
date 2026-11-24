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

const tmdbPluginID = "ambra.builtin.tmdb"

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
	if p, ok := h.registry.Get(tmdbPluginID); ok {
		_ = p.Configure(map[string]any{
			"api_key":  h.cfg.TMDBApiKey,
			"language": h.cfg.TMDBLanguage,
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
	lang := strings.TrimSpace(h.cfg.TMDBLanguage)
	if lang == "" {
		lang = "zh-CN"
	}
	httpx.JSON(w, http.StatusOK, SetupStatusResponse{
		Completed:       h.cfg.IsStartupWizardCompleted,
		ServerName:      h.cfg.ServerName,
		DefaultLanguage: lang,
		HasUsers:        hasUsers,
		HasTMDBKey:      strings.TrimSpace(h.cfg.TMDBApiKey) != "",
	})
}

func (h *Handler) SetupAdmin(w http.ResponseWriter, r *http.Request) {
	if h.cfg.IsStartupWizardCompleted {
		httpx.Error(w, http.StatusForbidden, "setup wizard is already completed")
		return
	}
	if h.setup.Auth == nil {
		httpx.Error(w, http.StatusInternalServerError, "auth service unavailable")
		return
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
	if name := strings.TrimSpace(req.ServerName); name != "" {
		h.cfg.ServerName = name
	}
	if lang := strings.TrimSpace(req.Language); lang != "" {
		h.cfg.TMDBLanguage = lang
		tmdbLangChanged = true
	}
	if err := h.cfg.Save(); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to save configuration")
		return
	}
	if tmdbLangChanged {
		h.syncTMDBPlugin()
	}

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

	if err := h.cfg.Save(); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to save configuration")
		return
	}
	h.syncTMDBPlugin()

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
