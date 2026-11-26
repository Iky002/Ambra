package governance

import (
	"context"
	"errors"
	"net/http"

	"github.com/ambra-media/ambra/internal/api/httpx"
	coregov "github.com/ambra-media/ambra/internal/core/governance"
)

// AuthFuncs decouples route registration from the concrete middleware package.
type AuthFuncs struct {
	RequireAuth   func(http.Handler) http.Handler
	RequireAdmin  func(http.Handler) http.Handler
	UserIDFromCtx func(context.Context) string
}

// Handler serves admin-only governance and system health endpoints.
type Handler struct {
	svc  *coregov.Service
	auth AuthFuncs
}

// New constructs a governance HTTP handler.
func New(svc *coregov.Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes mounts all admin governance and health routes onto mux.
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

	handleAdmin := func(pattern string, fn http.HandlerFunc) {
		mux.Handle(pattern, wrapAdmin(fn))
	}

	handleAdmin("GET /api/admin/health", h.handleHealth)
	handleAdmin("GET /api/admin/governance/audit", h.handleLibraryAudit)
	handleAdmin("POST /api/admin/governance/orphans/cleanup", h.handleCleanupOrphans)
	handleAdmin("GET /api/admin/governance/review", h.handleListReviews)
	handleAdmin("POST /api/admin/governance/review/{Id}/approve", h.handleApproveReview)
	handleAdmin("POST /api/admin/governance/review/{Id}/dismiss", h.handleDismissReview)
	handleAdmin("POST /api/admin/governance/items/{Id}/lock", h.handleLockFields)
	handleAdmin("POST /api/admin/governance/items/{Id}/nfo", h.handleWriteNFO)
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	report, err := h.svc.CollectHealth(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, report)
}

func (h *Handler) handleLibraryAudit(w http.ResponseWriter, r *http.Request) {
	report, err := h.svc.RunLibraryAudit(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, report)
}

type cleanupOrphansRequest struct {
	ItemIDs  []string `json:"ItemIds"`
	SnakeIDs []string `json:"item_ids"`
}

func (h *Handler) handleCleanupOrphans(w http.ResponseWriter, r *http.Request) {
	var req cleanupOrphansRequest
	if err := httpx.DecodeJSONLimit(r, &req, 0); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid cleanup payload")
		return
	}
	ids := req.ItemIDs
	if len(ids) == 0 {
		ids = req.SnakeIDs
	}
	if len(ids) == 0 {
		httpx.Error(w, http.StatusBadRequest, "ItemIds must not be empty")
		return
	}
	res, err := h.svc.CleanupOrphans(r.Context(), ids)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, res)
}

func (h *Handler) handleListReviews(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	status := q.Get("status")
	if status == "" {
		status = coregov.AuditStatusPending
	}
	items, err := h.svc.ListAudits(r.Context(), status)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	total := len(items)
	start := q.Int("StartIndex", 0)
	limit := q.Int("Limit", 0)
	if start < 0 {
		start = 0
	}
	page := items
	if start > 0 || limit > 0 {
		if start >= total {
			page = items[:0]
		} else {
			end := total
			if limit > 0 && start+limit < end {
				end = start + limit
			}
			page = items[start:end]
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"Items":            page,
		"TotalRecordCount": total,
	})
}

type approveReviewRequest struct {
	Candidate  coregov.ScrapeCandidate `json:"Candidate"`
	LockFields []string                `json:"LockFields"`
}

func (h *Handler) handleApproveReview(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("Id")
	var req approveReviewRequest
	if err := httpx.DecodeJSONLimit(r, &req, 0); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid approve payload")
		return
	}
	updated, err := h.svc.ApproveAudit(r.Context(), id, req.Candidate, req.LockFields)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, updated)
}

func (h *Handler) handleDismissReview(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("Id")
	updated, err := h.svc.DismissAudit(r.Context(), id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, updated)
}

type lockFieldsRequest struct {
	LockedFields  []string `json:"LockedFields"`
	Fields        []string `json:"fields"`
	LockData      *bool    `json:"LockData"`
	SnakeLockData *bool    `json:"lock_data"`
}

func (h *Handler) handleLockFields(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("Id")
	var req lockFieldsRequest
	if err := httpx.DecodeJSONLimit(r, &req, 0); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid lock payload")
		return
	}
	fields := req.LockedFields
	if fields == nil {
		fields = req.Fields
	}
	lockData := req.LockData
	if lockData == nil {
		lockData = req.SnakeLockData
	}
	res, err := h.svc.SetLockState(r.Context(), id, fields, lockData)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, res)
}

func (h *Handler) handleWriteNFO(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("Id")
	res, err := h.svc.WriteItemNFOAtomic(r.Context(), id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, res)
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, coregov.ErrNotFound):
		httpx.Error(w, http.StatusNotFound, err.Error())
	case errors.Is(err, coregov.ErrInvalidArgument), errors.Is(err, coregov.ErrItemPathEmpty):
		httpx.Error(w, http.StatusBadRequest, err.Error())
	default:
		httpx.Error(w, http.StatusInternalServerError, err.Error())
	}
}

func passthrough(next http.Handler) http.Handler { return next }
