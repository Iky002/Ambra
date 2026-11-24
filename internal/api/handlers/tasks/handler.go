package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/core/scheduler"
)

// AuthFuncs decouples route registration from the concrete middleware package.
type AuthFuncs struct {
	RequireAuth   func(http.Handler) http.Handler
	RequireAdmin  func(http.Handler) http.Handler
	UserIDFromCtx func(context.Context) string
}

type Handler struct {
	mgr  *scheduler.Manager
	auth AuthFuncs
}

func New(mgr *scheduler.Manager) *Handler {
	return &Handler{mgr: mgr}
}

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

	handleAuth := func(pattern string, fn http.HandlerFunc) {
		mux.Handle(pattern, wrapAuth(fn))
	}
	handleAdmin := func(pattern string, fn http.HandlerFunc) {
		mux.Handle(pattern, wrapAdmin(fn))
	}

	handleAuth("GET /ScheduledTasks", h.handleListTasks)
	handleAuth("GET /ScheduledTasks/{Id}", h.handleGetTask)
	// Dispatch both "POST /ScheduledTasks/Running/{Id}" and "POST /ScheduledTasks/{Id}/Triggers"
	// through a single pattern to avoid net/http.ServeMux precedence conflict on /ScheduledTasks/Running/Triggers.
	handleAdmin("POST /ScheduledTasks/{Seg1}/{Seg2}", h.handlePostDispatch)
	handleAdmin("DELETE /ScheduledTasks/Running/{Id}", h.handleStopTask)
}

func (h *Handler) handleListTasks(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	includeHidden := false
	if b := q.Bool("IsHidden"); b != nil {
		includeHidden = *b
	}
	httpx.JSON(w, http.StatusOK, h.mgr.List(includeHidden))
}

func (h *Handler) handleGetTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("Id")
	info, ok := h.mgr.Get(id)
	if !ok {
		httpx.Error(w, http.StatusNotFound, "scheduled task not found")
		return
	}
	httpx.JSON(w, http.StatusOK, info)
}

func (h *Handler) handlePostDispatch(w http.ResponseWriter, r *http.Request) {
	seg1 := r.PathValue("Seg1")
	seg2 := r.PathValue("Seg2")
	switch {
	case seg1 == "Running" && seg2 != "":
		h.runTaskByID(w, seg2)
	case seg2 == "Triggers" && seg1 != "":
		h.setTriggersByID(w, r, seg1)
	default:
		httpx.Error(w, http.StatusNotFound, "unknown scheduled task endpoint")
	}
}

func (h *Handler) runTaskByID(w http.ResponseWriter, id string) {
	if err := h.mgr.Run(id); err != nil {
		if errors.Is(err, scheduler.ErrTaskNotFound) {
			httpx.Error(w, http.StatusNotFound, err.Error())
			return
		}
		if errors.Is(err, scheduler.ErrTaskAlreadyRunning) {
			httpx.Error(w, http.StatusConflict, err.Error())
			return
		}
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) handleStopTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("Id")
	if err := h.mgr.Stop(id); err != nil {
		if errors.Is(err, scheduler.ErrTaskNotFound) {
			httpx.Error(w, http.StatusNotFound, err.Error())
			return
		}
		if errors.Is(err, scheduler.ErrTaskNotRunning) {
			httpx.Error(w, http.StatusConflict, err.Error())
			return
		}
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) setTriggersByID(w http.ResponseWriter, r *http.Request, id string) {
	var triggers []scheduler.TaskTriggerInfo
	if err := json.NewDecoder(r.Body).Decode(&triggers); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid triggers payload")
		return
	}
	if err := h.mgr.SetTriggers(id, triggers); err != nil {
		if errors.Is(err, scheduler.ErrTaskNotFound) {
			httpx.Error(w, http.StatusNotFound, err.Error())
			return
		}
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.NoContent(w)
}

func passthrough(next http.Handler) http.Handler { return next }
