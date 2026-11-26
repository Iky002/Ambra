package migration

import (
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/ambra-media/ambra/internal/api/httpx"
	coremigration "github.com/ambra-media/ambra/internal/core/migration"
)

const maxUpstreamErrorBytes = 512

var urlTokenRe = regexp.MustCompile(`(?i)https?://[^\s"'<>]+`)

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

// Close cancels any in-flight migration job and waits for it to exit during server shutdown.
func (h *Handler) Close() {
	if h != nil && h.migrator != nil {
		h.migrator.Cancel()
		h.migrator.Wait()
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
		return redactURLCredentials(trimmed)
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func redactURLCredentials(msg string) string {
	if msg == "" || !strings.Contains(msg, "@") {
		return msg
	}
	return urlTokenRe.ReplaceAllStringFunc(msg, func(rawURL string) string {
		schemeIdx := strings.Index(rawURL, "://")
		if schemeIdx < 0 {
			return rawURL
		}
		prefix := rawURL[:schemeIdx+3]
		rest := rawURL[schemeIdx+3:]
		// Isolate the authority component up to the first path/query/fragment delimiter.
		authEnd := strings.IndexAny(rest, "/?#")
		authority := rest
		suffix := ""
		if authEnd >= 0 {
			authority = rest[:authEnd]
			suffix = rest[authEnd:]
		}
		if lastAt := strings.LastIndex(authority, "@"); lastAt >= 0 {
			return prefix + "***@" + authority[lastAt+1:] + suffix
		}
		if u, err := url.Parse(rawURL); err == nil && u.User != nil {
			u.User = nil
			return u.String()
		}
		return rawURL
	})
}

func truncateErrorMsg(msg string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(msg)
	if len(runes) <= maxRunes {
		return msg
	}
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-3]) + "..."
}

func sanitizeErrorMessage(msg string) string {
	return truncateErrorMsg(redactURLCredentials(msg), maxUpstreamErrorBytes)
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
			errs[i] = sanitizeErrorMessage(e)
		}
		rep.Errors = errs
	}
	return rep
}

func writeMigrationError(w http.ResponseWriter, err error) {
	if errors.Is(err, coremigration.ErrMigrationRunning) {
		httpx.Error(w, http.StatusConflict, sanitizeErrorMessage(err.Error()))
		return
	}
	var apiErr *coremigration.APIError
	if errors.As(err, &apiErr) {
		if apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden {
			httpx.Error(w, http.StatusBadGateway, sanitizeErrorMessage("source Emby rejected credentials: "+apiErr.Error()))
			return
		}
		httpx.Error(w, http.StatusBadGateway, sanitizeErrorMessage(apiErr.Error()))
		return
	}
	if strings.Contains(err.Error(), "invalid source server URL") ||
		strings.Contains(err.Error(), "is required") {
		httpx.Error(w, http.StatusBadRequest, sanitizeErrorMessage(err.Error()))
		return
	}
	httpx.Error(w, http.StatusBadGateway, sanitizeErrorMessage(err.Error()))
}
