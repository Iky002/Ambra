package tasks

import (
	"bytes"
	"context"
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
