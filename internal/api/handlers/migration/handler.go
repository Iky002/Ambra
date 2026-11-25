package migration

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/ambra-media/ambra/internal/api/httpx"
	coremigration "github.com/ambra-media/ambra/internal/core/migration"
)

const maxUpstreamErrorBytes = 512

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
	if err := httpx.DecodeJSONLimit(r, &req, 0); err != nil {
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
	if err := httpx.DecodeJSONLimit(r, &req, 0); err != nil {
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
	httpx.JSON(w, http.StatusOK, sanitizeReportPtr(report))
}

type startRequest struct {
	coremigration.MigrationRequest
	Sync bool `json:"sync,omitempty"`
}

func (h *Handler) handleStart(w http.ResponseWriter, r *http.Request) {
	var req startRequest
	if err := httpx.DecodeJSONLimit(r, &req, 0); err != nil {
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
		httpx.JSON(w, http.StatusOK, sanitizeReportPtr(report))
		return
	}

	status, err := h.migrator.StartAsync(req.MigrationRequest)
	if err != nil {
		writeMigrationError(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, sanitizeReport(status))
}

func (h *Handler) handleStatus(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, sanitizeReport(h.migrator.Status()))
}

func (h *Handler) handleCancel(w http.ResponseWriter, _ *http.Request) {
	cancelled := h.migrator.Cancel()
	httpx.JSON(w, http.StatusOK, map[string]any{
		"cancelled": cancelled,
		"report":    sanitizeReport(h.migrator.Status()),
	})
}

func stripURLUserinfo(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return trimmed
	}
	if u.User != nil {
		u.User = nil
	}
	return u.String()
}

func truncateErrorMsg(msg string, maxBytes int) string {
	if len(msg) <= maxBytes {
		return msg
	}
	if maxBytes <= 3 {
		return msg[:maxBytes]
	}
	return msg[:maxBytes-3] + "..."
}

func sanitizeReportPtr(rep *coremigration.MigrationReport) *coremigration.MigrationReport {
	if rep == nil {
		return nil
	}
	clean := sanitizeReport(*rep)
	return &clean
}

func sanitizeReport(rep coremigration.MigrationReport) coremigration.MigrationReport {
	rep.SourceURL = stripURLUserinfo(rep.SourceURL)
	if len(rep.Errors) > 0 {
		errs := make([]string, len(rep.Errors))
		for i, e := range rep.Errors {
			errs[i] = truncateErrorMsg(e, maxUpstreamErrorBytes)
		}
		rep.Errors = errs
	}
	return rep
}

func writeMigrationError(w http.ResponseWriter, err error) {
	if errors.Is(err, coremigration.ErrMigrationRunning) {
		httpx.Error(w, http.StatusConflict, truncateErrorMsg(err.Error(), maxUpstreamErrorBytes))
		return
	}
	var apiErr *coremigration.APIError
	if errors.As(err, &apiErr) {
		if apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden {
			httpx.Error(w, http.StatusBadGateway, truncateErrorMsg("source Emby rejected credentials: "+apiErr.Error(), maxUpstreamErrorBytes))
			return
		}
		httpx.Error(w, http.StatusBadGateway, truncateErrorMsg(apiErr.Error(), maxUpstreamErrorBytes))
		return
	}
	if strings.Contains(err.Error(), "invalid source server URL") ||
		strings.Contains(err.Error(), "is required") {
		httpx.Error(w, http.StatusBadRequest, truncateErrorMsg(err.Error(), maxUpstreamErrorBytes))
		return
	}
	httpx.Error(w, http.StatusBadGateway, truncateErrorMsg(err.Error(), maxUpstreamErrorBytes))
}
