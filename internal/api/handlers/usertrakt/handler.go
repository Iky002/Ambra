// Package usertrakt manages private Trakt bindings for the authenticated user.
package usertrakt

import (
	"net/http"
	"strings"
	"sync"

	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/plugins/trakt"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

type Handler struct {
	plugin *trakt.Plugin
	users  *sqlite.UserRepo
	mu     sync.Mutex
}

type Status struct {
	Enabled               bool   `json:"Enabled"`
	Configured            bool   `json:"Configured"`
	ClientID              string `json:"ClientId"`
	AccessTokenConfigured bool   `json:"AccessTokenConfigured"`
	ScrobblePlayback      bool   `json:"ScrobblePlayback"`
	SyncWatched           bool   `json:"SyncWatched"`
}

func New(p *trakt.Plugin, users *sqlite.UserRepo) *Handler {
	return &Handler{plugin: p, users: users}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, requireAuth func(http.Handler) http.Handler) {
	for _, method := range []string{"GET", "POST", "DELETE"} {
		mux.Handle(method+" /Users/Me/Trakt", requireAuth(http.HandlerFunc(h.handle)))
	}
}

func (h *Handler) handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	ac, ok := middleware.AuthFromContext(r.Context())
	// API keys represent server applications, not a personal user account.
	if !ok || ac.User == nil || ac.IsAPIKey {
		httpx.Error(w, http.StatusForbidden, "A personal user session is required")
		return
	}
	userID := ac.User.ID
	if r.Method == http.MethodDelete {
		h.mu.Lock()
		err := h.users.DeleteUserTraktConfig(r.Context(), userID)
		h.mu.Unlock()
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "Unable to remove Trakt binding")
			return
		}
		httpx.NoContent(w)
		return
	}
	if r.Method == http.MethodPost && !h.plugin.Manifest().Enabled {
		httpx.Error(w, http.StatusForbidden, "Trakt is disabled by the administrator")
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	cfg, err := h.users.GetUserTraktConfig(r.Context(), userID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "Unable to load Trakt binding")
		return
	}
	if cfg == nil {
		cfg = &sqlite.TraktConfig{ScrobblePlayback: true, SyncWatched: true}
	}
	if r.Method == http.MethodPost {
		var input struct {
			ClientID         *string `json:"ClientId"`
			AccessToken      *string `json:"AccessToken"`
			ScrobblePlayback *bool   `json:"ScrobblePlayback"`
			SyncWatched      *bool   `json:"SyncWatched"`
		}
		if err := httpx.DecodeJSONLimit(r, &input, 16<<10); err != nil {
			httpx.Error(w, http.StatusBadRequest, "Invalid Trakt configuration")
			return
		}
		if input.ClientID != nil {
			clientID := strings.TrimSpace(*input.ClientID)
			if clientID != cfg.ClientID && input.AccessToken == nil {
				httpx.Error(w, http.StatusBadRequest, "A new Client ID requires a new Access Token")
				return
			}
			cfg.ClientID = clientID
		}
		if input.AccessToken != nil {
			cfg.AccessToken = strings.TrimSpace(*input.AccessToken)
		}
		if input.ScrobblePlayback != nil {
			cfg.ScrobblePlayback = *input.ScrobblePlayback
		}
		if input.SyncWatched != nil {
			cfg.SyncWatched = *input.SyncWatched
		}
		if cfg.ClientID == "" || cfg.AccessToken == "" || cfg.AccessToken == "********" ||
			len(cfg.ClientID) > 256 || len(cfg.AccessToken) > 4096 ||
			strings.ContainsAny(cfg.ClientID+cfg.AccessToken, "\r\n\x00") {
			httpx.Error(w, http.StatusBadRequest, "A valid Client ID and Access Token are required")
			return
		}
		if err := h.users.SaveUserTraktConfig(r.Context(), userID, *cfg); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "Unable to save Trakt binding")
			return
		}
	}
	httpx.JSON(w, http.StatusOK, Status{
		Enabled: h.plugin.Manifest().Enabled, Configured: cfg.ClientID != "" && cfg.AccessToken != "",
		ClientID: cfg.ClientID, AccessTokenConfigured: cfg.AccessToken != "",
		ScrobblePlayback: cfg.ScrobblePlayback, SyncWatched: cfg.SyncWatched,
	})
}
