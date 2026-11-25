package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ambra-media/ambra/internal/core/scheduler"
)

func TestTasksHandlerExtraBranches(t *testing.T) {
	mgr := scheduler.New(nil)
	started := make(chan struct{})
	mgr.Register(scheduler.TaskDefinition{
		ID:       "blocking",
		Name:     "Blocking",
		Category: "Test",
		Run: func(ctx context.Context, report func(float64)) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		},
	})
	mgr.Register(scheduler.TaskDefinition{
		ID:       "hidden",
		Name:     "Hidden",
		Category: "Test",
		IsHidden: true,
	})

	mux := http.NewServeMux()
	h := New(mgr)
	h.RegisterRoutes(mux, AuthFuncs{})

	// GET /ScheduledTasks?IsHidden=true
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ScheduledTasks?IsHidden=true", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET IsHidden=true status=%d", rec.Code)
	}

	// GET missing task -> 404
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ScheduledTasks/missing", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing task, got %d", rec.Code)
	}

	// Unknown POST dispatch -> 404
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/ScheduledTasks/Unknown/Action", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown POST dispatch, got %d", rec.Code)
	}

	// Run missing task -> 404
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/ScheduledTasks/Running/missing", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for running missing task, got %d", rec.Code)
	}

	// Stop missing -> 404; Stop idle -> 409
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/ScheduledTasks/Running/missing", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for stopping missing task, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/ScheduledTasks/Running/hidden", nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for stopping idle task, got %d", rec.Code)
	}

	// Start blocking task -> 204, start again -> 409, stop -> 204
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/ScheduledTasks/Running/blocking", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 starting blocking, got %d", rec.Code)
	}
	<-started

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/ScheduledTasks/Running/blocking", nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for already running task, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/ScheduledTasks/Running/blocking", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 stopping blocking, got %d", rec.Code)
	}
	mgr.Wait("blocking")

	// SetTriggers: invalid JSON -> 400; missing task -> 404
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/ScheduledTasks/blocking/Triggers", bytes.NewReader([]byte(`not-json`))))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid triggers JSON, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/ScheduledTasks/missing/Triggers", bytes.NewReader([]byte(`[]`))))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing task triggers, got %d", rec.Code)
	}
}

func TestTasksFilteringAndAdminAuth(t *testing.T) {
	mgr := scheduler.New(nil)
	ticks := int64(3600_0000000)
	mgr.Register(scheduler.TaskDefinition{
		ID:       "vis-enabled",
		Name:     "Visible Enabled",
		Category: "A",
		Triggers: []scheduler.TaskTriggerInfo{{Type: "IntervalTrigger", IntervalTicks: &ticks}},
	})
	mgr.Register(scheduler.TaskDefinition{
		ID:       "vis-disabled",
		Name:     "Visible Disabled",
		Category: "A",
		Triggers: []scheduler.TaskTriggerInfo{},
	})
	mgr.Register(scheduler.TaskDefinition{
		ID:       "hid-enabled",
		Name:     "Hidden Enabled",
		Category: "B",
		IsHidden: true,
		Triggers: []scheduler.TaskTriggerInfo{{Type: "IntervalTrigger", IntervalTicks: &ticks}},
	})

	mux := http.NewServeMux()
	h := New(mgr)
	h.RegisterRoutes(mux, AuthFuncs{
		RequireAdmin: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Admin") != "yes" {
					http.Error(w, "forbidden", http.StatusForbidden)
					return
				}
				next.ServeHTTP(w, r)
			})
		},
	})

	// Non-admin must be rejected on GET /ScheduledTasks and GET /ScheduledTasks/{Id}
	for _, path := range []string{"/ScheduledTasks", "/ScheduledTasks/vis-enabled"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for non-admin on %s, got %d", path, rec.Code)
		}
	}

	getList := func(query string) []scheduler.TaskInfo {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/ScheduledTasks"+query, nil)
		req.Header.Set("X-Admin", "yes")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /ScheduledTasks%s status=%d", query, rec.Code)
		}
		var out []scheduler.TaskInfo
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		return out
	}

	// Omitted IsHidden -> non-hidden only (2)
	if got := getList(""); len(got) != 2 {
		t.Fatalf("omitted IsHidden expected 2, got %+v", got)
	}
	// IsHidden=true -> hidden only (1)
	if got := getList("?IsHidden=true"); len(got) != 1 || got[0].Id != "hid-enabled" {
		t.Fatalf("IsHidden=true expected only hid-enabled, got %+v", got)
	}
	// IsHidden=false -> non-hidden only (2)
	if got := getList("?IsHidden=false"); len(got) != 2 {
		t.Fatalf("IsHidden=false expected 2, got %+v", got)
	}
	// IsEnabled=true (with default non-hidden) -> vis-enabled (1)
	if got := getList("?IsEnabled=true"); len(got) != 1 || got[0].Id != "vis-enabled" {
		t.Fatalf("IsEnabled=true expected vis-enabled, got %+v", got)
	}
	// IsEnabled=false -> vis-disabled (1)
	if got := getList("?IsEnabled=false"); len(got) != 1 || got[0].Id != "vis-disabled" {
		t.Fatalf("IsEnabled=false expected vis-disabled, got %+v", got)
	}
	// IsHidden=true&IsEnabled=false -> 0
	if got := getList("?IsHidden=true&IsEnabled=false"); len(got) != 0 {
		t.Fatalf("IsHidden=true&IsEnabled=false expected 0, got %+v", got)
	}
}
