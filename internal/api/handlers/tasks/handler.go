package tasks

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/core/scheduler"
	"github.com/ambra-media/ambra/internal/pkg/i18n"
)

const ticksPerDay int64 = 86400 * 10_000_000

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

	handleAdmin := func(pattern string, fn http.HandlerFunc) {
		mux.Handle(pattern, wrapAdmin(fn))
	}

	handleAdmin("GET /ScheduledTasks", h.handleListTasks)
	handleAdmin("GET /ScheduledTasks/{Id}", h.handleGetTask)
	// Dispatch both "POST /ScheduledTasks/Running/{Id}" and "POST /ScheduledTasks/{Id}/Triggers"
	// through a single pattern to avoid net/http.ServeMux precedence conflict on /ScheduledTasks/Running/Triggers.
	handleAdmin("POST /ScheduledTasks/{Seg1}/{Seg2}", h.handlePostDispatch)
	handleAdmin("DELETE /ScheduledTasks/Running/{Id}", h.handleStopTask)
}

func (h *Handler) handleListTasks(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	isHidden := q.Bool("IsHidden")
	isEnabled := q.Bool("IsEnabled")
	acceptLang := strings.TrimSpace(r.Header.Get("Accept-Language"))
	locale := ""
	if acceptLang != "" {
		locale = i18n.ResolveLocale(acceptLang, "")
	}

	all := h.mgr.List(true)
	filtered := make([]scheduler.TaskInfo, 0, len(all))
	for _, t := range all {
		if isHidden != nil {
			if t.IsHidden != *isHidden {
				continue
			}
		} else if t.IsHidden {
			continue
		}
		if isEnabled != nil {
			enabled := len(t.Triggers) > 0
			if enabled != *isEnabled {
				continue
			}
		}
		if locale != "" {
			t = localizeTaskInfo(t, locale)
		}
		filtered = append(filtered, t)
	}
	httpx.JSON(w, http.StatusOK, filtered)
}

func (h *Handler) handleGetTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("Id")
	info, ok := h.mgr.Get(id)
	if !ok {
		httpx.Error(w, http.StatusNotFound, "scheduled task not found")
		return
	}
	if acceptLang := strings.TrimSpace(r.Header.Get("Accept-Language")); acceptLang != "" {
		info = localizeTaskInfo(info, i18n.ResolveLocale(acceptLang, ""))
	}
	httpx.JSON(w, http.StatusOK, info)
}

func localizeTaskInfo(info scheduler.TaskInfo, locale string) scheduler.TaskInfo {
	key := info.Key
	if key == "" {
		key = info.Id
	}
	var nameKey, descKey string
	switch key {
	case "RefreshLibrary":
		nameKey, descKey = "task.scan_library.name", "task.scan_library.desc"
	case "CleanTranscode", "CleanTranscodeCache":
		nameKey, descKey = "task.clean_transcode.name", "task.clean_transcode.desc"
	case "OptimizeDatabase":
		nameKey, descKey = "task.vacuum_db.name", "task.vacuum_db.desc"
	case "SyncRankings":
		nameKey, descKey = "task.sync_rankings.name", "task.sync_rankings.desc"
	default:
		return info
	}
	info.Name = i18n.T(locale, nameKey)
	info.Description = i18n.T(locale, descKey)
	return info
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

func validateTriggers(triggers []scheduler.TaskTriggerInfo) error {
	for i, tr := range triggers {
		switch strings.TrimSpace(tr.Type) {
		case "IntervalTrigger", "DailyTrigger", "WeeklyTrigger", "StartupTrigger", "SystemEventTrigger":
		default:
			return fmt.Errorf("trigger[%d]: unsupported trigger type %q", i, tr.Type)
		}
		if tr.TimeOfDayTicks != nil && (*tr.TimeOfDayTicks < 0 || *tr.TimeOfDayTicks >= ticksPerDay) {
			return fmt.Errorf("trigger[%d]: TimeOfDayTicks must be between 0 and %d", i, ticksPerDay-1)
		}
		if tr.IntervalTicks != nil && *tr.IntervalTicks <= 0 {
			return fmt.Errorf("trigger[%d]: IntervalTicks must be positive", i)
		}
		if tr.MaxRuntimeTicks != nil && *tr.MaxRuntimeTicks < 0 {
			return fmt.Errorf("trigger[%d]: MaxRuntimeTicks must be non-negative", i)
		}
	}
	return nil
}

func (h *Handler) setTriggersByID(w http.ResponseWriter, r *http.Request, id string) {
	var triggers []scheduler.TaskTriggerInfo
	if err := httpx.DecodeJSONLimit(r, &triggers, 0); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid triggers payload")
		return
	}
	if err := validateTriggers(triggers); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
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
