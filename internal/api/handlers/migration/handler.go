package migration

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/ambra-media/ambra/internal/api/httpx"
	coremigration "github.com/ambra-media/ambra/internal/core/migration"
)

// AuthFuncs decouples route registration from the concrete middleware package.
type AuthFuncs struct {
	RequireAuth  func(http.Handler) http.Handler
	RequireAdmin func(http.Handler) http.Handler
}

// Handler exposes admin-only HTTP endpoints for legacy Emby migration.
type Handler struct {
	migrator *coremigration.Migrator
	auth     AuthFuncs
}

// New constructs a migration HTTP handler backed by the given Migrator.
func New(migrator *coremigration.Migrator) *Handler {
	return &Handler{
		migrator: migrator,
	}
}

// RegisterRoutes mounts the migration admin endpoints onto mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth AuthFuncs) {
	h.auth = auth

	wrap := func(fn http.HandlerFunc) http.Handler {
		var handler http.Handler = fn
		if auth.RequireAdmin != nil {
			handler = auth.RequireAdmin(handler)
		}
		if auth.RequireAuth != nil {
			handler = auth.RequireAuth(handler)
		}
		return handler
	}

	mux.Handle("POST /api/admin/migration/test", wrap(h.handleTest))
	mux.Handle("POST /api/admin/migration/dry-run", wrap(h.handleDryRun))
	mux.Handle("POST /api/admin/migration/start", wrap(h.handleStart))
	mux.Handle("GET /api/admin/migration/status", wrap(h.handleStatus))
	mux.Handle("POST /api/admin/migration/cancel", wrap(h.handleCancel))
}

type testRequest struct {
	SourceURL string `json:"source_url"`
	APIKey    string `json:"api_key"`
}

func (h *Handler) handleTest(w http.ResponseWriter, r *http.Request) {
	var req testRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid JSON request body")
		return
	}
	if strings.TrimSpace(req.SourceURL) == "" || strings.TrimSpace(req.APIKey) == "" {
		httpx.Error(w, http.StatusBadRequest, "source_url and api_key are required")
		return
	}

	res, err := h.migrator.TestSource(r.Context(), req.SourceURL, req.APIKey)
	if err != nil {
		writeMigrationError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, res)
}

func (h *Handler) handleDryRun(w http.ResponseWriter, r *http.Request) {
	var req coremigration.MigrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid JSON request body")
		return
	}
	if strings.TrimSpace(req.SourceURL) == "" || strings.TrimSpace(req.APIKey) == "" {
		httpx.Error(w, http.StatusBadRequest, "source_url and api_key are required")
		return
	}
	req.DryRun = true

	report, err := h.migrator.Run(r.Context(), req)
	if err != nil {
		writeMigrationError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, report)
}

type startRequest struct {
	coremigration.MigrationRequest
	Sync bool `json:"sync,omitempty"`
}

func (h *Handler) handleStart(w http.ResponseWriter, r *http.Request) {
	var req startRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid JSON request body")
		return
	}
	if strings.TrimSpace(req.SourceURL) == "" || strings.TrimSpace(req.APIKey) == "" {
		httpx.Error(w, http.StatusBadRequest, "source_url and api_key are required")
		return
	}
	req.DryRun = false

	q := httpx.ParseQuery(r)
	syncMode := req.Sync
	if b := q.Bool("sync"); b != nil {
		syncMode = *b
	}

	if syncMode {
		report, err := h.migrator.Run(r.Context(), req.MigrationRequest)
		if err != nil {
			writeMigrationError(w, err)
			return
		}
		httpx.JSON(w, http.StatusOK, report)
		return
	}

	status, err := h.migrator.StartAsync(req.MigrationRequest)
	if err != nil {
		writeMigrationError(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, status)
}

func (h *Handler) handleStatus(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, h.migrator.Status())
}

func (h *Handler) handleCancel(w http.ResponseWriter, _ *http.Request) {
	cancelled := h.migrator.Cancel()
	httpx.JSON(w, http.StatusOK, map[string]any{
		"cancelled": cancelled,
		"report":    h.migrator.Status(),
	})
}

func writeMigrationError(w http.ResponseWriter, err error) {
	if errors.Is(err, coremigration.ErrMigrationRunning) {
		httpx.Error(w, http.StatusConflict, err.Error())
		return
	}
	var apiErr *coremigration.APIError
	if errors.As(err, &apiErr) {
		if apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden {
			httpx.Error(w, http.StatusBadGateway, "source Emby rejected credentials: "+apiErr.Error())
			return
		}
		httpx.Error(w, http.StatusBadGateway, apiErr.Error())
		return
	}
	if strings.Contains(err.Error(), "invalid source server URL") ||
		strings.Contains(err.Error(), "is required") {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.Error(w, http.StatusBadGateway, err.Error())
}
