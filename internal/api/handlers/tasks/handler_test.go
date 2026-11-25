package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ambra-media/ambra/internal/core/scheduler"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
)

func TestTasksHandler(t *testing.T) {
	mgr := scheduler.New(eventbus.New())
	done := make(chan struct{})
	mgr.Register(scheduler.TaskDefinition{
		ID:          "demo",
		Name:        "Demo Task",
		Category:    "Maintenance",
		Description: "Test task",
		Run: func(ctx context.Context, report func(float64)) error {
			report(100)
			close(done)
			return nil
		},
	})

	mux := http.NewServeMux()
	h := New(mgr)
	h.RegisterRoutes(mux, AuthFuncs{})

	// GET /ScheduledTasks
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ScheduledTasks", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /ScheduledTasks status=%d", rec.Code)
	}
	var list []scheduler.TaskInfo
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 1 || list[0].Id != "demo" {
		t.Fatalf("unexpected tasks list: %+v", list)
	}

	// POST /ScheduledTasks/Running/demo
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/ScheduledTasks/Running/demo", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST /ScheduledTasks/Running/demo status=%d body=%s", rec.Code, rec.Body.String())
	}
	<-done
	mgr.Wait("demo")

	// GET /ScheduledTasks/demo
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ScheduledTasks/demo", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /ScheduledTasks/demo status=%d", rec.Code)
	}
	var info scheduler.TaskInfo
	_ = json.Unmarshal(rec.Body.Bytes(), &info)
	if info.State != scheduler.StateIdle || info.LastExecutionResult == nil || info.LastExecutionResult.Status != "Completed" {
		t.Fatalf("unexpected task info after run: %+v", info)
	}

	// POST /ScheduledTasks/demo/Triggers
	interval := int64(36000000000)
	trigBody, _ := json.Marshal([]scheduler.TaskTriggerInfo{
		{Type: "IntervalTrigger", IntervalTicks: &interval},
	})
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/ScheduledTasks/demo/Triggers", bytes.NewReader(trigBody)))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST Triggers status=%d", rec.Code)
	}

	updated, _ := mgr.Get("demo")
	if len(updated.Triggers) != 1 || updated.Triggers[0].Type != "IntervalTrigger" {
		t.Fatalf("expected trigger to be updated, got %+v", updated.Triggers)
	}

	// Invalid trigger types and negative ticks must return 400
	neg := int64(-100)
	badPayloads := [][]scheduler.TaskTriggerInfo{
		{{Type: "EvilTrigger"}},
		{{Type: "IntervalTrigger", IntervalTicks: &neg}},
		{{Type: "DailyTrigger", TimeOfDayTicks: &neg}},
		{{Type: "StartupTrigger", MaxRuntimeTicks: &neg}},
	}
	for _, bp := range badPayloads {
		b, _ := json.Marshal(bp)
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/ScheduledTasks/demo/Triggers", bytes.NewReader(b)))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for invalid trigger %+v, got %d", bp, rec.Code)
		}
	}
}
