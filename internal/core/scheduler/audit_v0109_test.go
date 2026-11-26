package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestAuditV0109_L26_CrossMidnightTriggersRegisterOverrideAndShutdown(t *testing.T) {
	// 1. Cross-midnight DailyTrigger & WeeklyTrigger check previous day's trigger window.
	// Scheduled at 23:59:00; evaluated at 00:01:00 the next day (2 minutes later < 5m window).
	ticks2359 := int64((23*time.Hour + 59*time.Minute) / (100 * time.Nanosecond))
	nowNextDay := time.Date(2026, 6, 15, 0, 1, 0, 0, time.UTC) // Monday 00:01 (yesterday was Sunday)

	dailyTask := &managedTask{
		def: TaskDefinition{
			ID:       "daily-midnight",
			Triggers: []TaskTriggerInfo{{Type: "DailyTrigger", TimeOfDayTicks: &ticks2359}},
		},
		state: StateIdle,
	}
	if !taskIsDueLocked(dailyTask, nowNextDay) {
		t.Fatal("expected DailyTrigger at 23:59 to be due at 00:01 next day")
	}

	weeklyTask := &managedTask{
		def: TaskDefinition{
			ID:       "weekly-midnight",
			Triggers: []TaskTriggerInfo{{Type: "WeeklyTrigger", DayOfWeek: "Sunday", TimeOfDayTicks: &ticks2359}},
		},
		state: StateIdle,
	}
	if !taskIsDueLocked(weeklyTask, nowNextDay) {
		t.Fatal("expected WeeklyTrigger (Sunday 23:59) to be due at Monday 00:01")
	}

	// 2. Register overriding a running task cancels the old run context cleanly.
	mgr := New(nil)
	oldCancelled := make(chan struct{})
	started := make(chan struct{})
	mgr.Register(TaskDefinition{
		ID: "replace-me",
		Run: func(ctx context.Context, _ func(float64)) error {
			close(started)
			<-ctx.Done()
			close(oldCancelled)
			return ctx.Err()
		},
	})
	if err := mgr.Run("replace-me"); err != nil {
		t.Fatal(err)
	}
	<-started
	mgr.Register(TaskDefinition{
		ID: "replace-me",
		Run: func(_ context.Context, _ func(float64)) error {
			return nil
		},
	})
	select {
	case <-oldCancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("expected overridden running task to be cancelled")
	}

	// 3. Shutdown cancels active tasks and waits for convergence.
	var finished atomic.Bool
	longStarted := make(chan struct{})
	mgr.Register(TaskDefinition{
		ID: "shutdown-task",
		Run: func(ctx context.Context, _ func(float64)) error {
			close(longStarted)
			<-ctx.Done()
			time.Sleep(30 * time.Millisecond)
			finished.Store(true)
			return ctx.Err()
		},
	})
	if err := mgr.Run("shutdown-task"); err != nil {
		t.Fatal(err)
	}
	<-longStarted

	shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := mgr.Shutdown(shutCtx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if !finished.Load() {
		t.Fatal("expected Shutdown to wait until running task converged")
	}
}
