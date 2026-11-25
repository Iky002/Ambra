package rankings

import (
	"net/http"

	"github.com/ambra-media/ambra/internal/api/httpx"
)

// AuthFuncs decouples route registration from the concrete middleware package.
type AuthFuncs struct {
	RequireAuth  func(http.Handler) http.Handler
	RequireAdmin func(http.Handler) http.Handler
}

// Handler exposes HTTP management endpoints for the rankings Service.
type Handler struct {
	svc  *Service
	auth AuthFuncs
}

// NewHandler creates a Handler backed by the given Service.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes registers rankings endpoints on mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth AuthFuncs) {
	h.auth = auth
	wrapAuth := auth.RequireAuth
	if wrapAuth == nil {
		wrapAuth = passthrough
	}
	wrapAdmin := auth.RequireAdmin
	if wrapAdmin == nil {
		wrapAdmin = wrapAuth
	}

	mux.Handle("GET /System/Rankings", wrapAuth(http.HandlerFunc(h.handleStatus)))
	mux.Handle("GET /System/Rankings/Trending", wrapAuth(http.HandlerFunc(h.handleTrending)))
	mux.Handle("POST /System/Rankings/Refresh", wrapAdmin(http.HandlerFunc(h.handleRefresh)))
}

func (h *Handler) handleStatus(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	includeEntries := false
	if b := q.Bool("includeEntries"); b != nil {
		includeEntries = *b
	}
	httpx.JSON(w, http.StatusOK, h.svc.Status(includeEntries))
}

func (h *Handler) handleTrending(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, h.svc.Trending(r.Context()))
}

func (h *Handler) handleRefresh(w http.ResponseWriter, r *http.Request) {
	summary, err := h.svc.SyncAndApply(r.Context())
	if err != nil {
		httpx.JSON(w, http.StatusBadGateway, map[string]any{
			"Error":   http.StatusText(http.StatusBadGateway),
			"Message": "failed to refresh rankings from upstream sources",
			"Summary": summary,
		})
		return
	}
	httpx.JSON(w, http.StatusOK, summary)
}

func passthrough(next http.Handler) http.Handler { return next }
