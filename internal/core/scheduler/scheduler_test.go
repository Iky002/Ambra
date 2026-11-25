package scheduler

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/pkg/eventbus"
)

func TestSchedulerRunProgressAndCancel(t *testing.T) {
	bus := eventbus.New()
	var progressEvents atomic.Int32
	bus.Subscribe(eventbus.EventTaskProgress, func(_ context.Context, _ eventbus.Event) {
		progressEvents.Add(1)
	})

	mgr := New(bus)
	mgr.Register(TaskDefinition{
		ID:          "quick",
		Name:        "Quick Task",
		Category:    "Test",
		Description: "Completes quickly",
		Run: func(ctx context.Context, report func(float64)) error {
			report(50)
			time.Sleep(10 * time.Millisecond)
			report(100)
			return nil
		},
	})

	if err := mgr.Run("quick"); err != nil {
		t.Fatalf("Run quick failed: %v", err)
	}
	mgr.Wait("quick")

	info, ok := mgr.Get("quick")
	if !ok {
		t.Fatal("expected task quick to exist")
	}
	if info.State != StateIdle {
		t.Fatalf("expected StateIdle, got %s", info.State)
	}
	if info.LastExecutionResult == nil || info.LastExecutionResult.Status != "Completed" {
		t.Fatalf("unexpected LastExecutionResult: %+v", info.LastExecutionResult)
	}

	// Test cancellation of a blocking task
	started := make(chan struct{})
	mgr.Register(TaskDefinition{
		ID:       "blocking",
		Name:     "Blocking Task",
		Category: "Test",
		Run: func(ctx context.Context, report func(float64)) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		},
	})
	if err := mgr.Run("blocking"); err != nil {
		t.Fatalf("Run blocking failed: %v", err)
	}
	<-started

	if err := mgr.Run("blocking"); err != ErrTaskAlreadyRunning {
		t.Fatalf("expected ErrTaskAlreadyRunning, got %v", err)
	}
	if err := mgr.Stop("blocking"); err != nil {
		t.Fatalf("Stop blocking failed: %v", err)
	}
	mgr.Wait("blocking")

	bInfo, _ := mgr.Get("blocking")
	if bInfo.LastExecutionResult == nil || bInfo.LastExecutionResult.Status != "Cancelled" {
		t.Fatalf("expected Cancelled status, got %+v", bInfo.LastExecutionResult)
	}

	// Allow async eventbus goroutines to flush
	time.Sleep(20 * time.Millisecond)
	if progressEvents.Load() == 0 {
		t.Fatal("expected EventTaskProgress events to be published")
	}
}

func TestBuiltinTasksCleanTranscodeCache(t *testing.T) {
	tmp := t.TempDir()
	dummy := filepath.Join(tmp, "seg001.ts")
	if err := os.WriteFile(dummy, []byte("tsdata"), 0o644); err != nil {
		t.Fatal(err)
	}

	var scanned atomic.Bool
	mgr := New(nil)
	mgr.RegisterBuiltinTasks(func(ctx context.Context) error {
		scanned.Store(true)
		return nil
	}, tmp, nil)

	if len(mgr.List(false)) != 3 {
		t.Fatalf("expected 3 builtin tasks, got %d", len(mgr.List(false)))
	}

	if err := mgr.Run("CleanTranscodeCache"); err != nil {
		t.Fatal(err)
	}
	mgr.Wait("CleanTranscodeCache")
	if _, err := os.Stat(dummy); !os.IsNotExist(err) {
		t.Fatalf("expected transcode temp file to be deleted, stat err=%v", err)
	}

	if err := mgr.Run("RefreshLibrary"); err != nil {
		t.Fatal(err)
	}
	mgr.Wait("RefreshLibrary")
	if !scanned.Load() {
		t.Fatal("expected scanAll callback to be invoked")
	}
}

func TestSchedulerStartTriggersMaxRuntimeAndActiveDirProtection(t *testing.T) {
	i64 := func(v int64) *int64 { return &v }

	// 1. Start loop fires IntervalTrigger and enforces MaxRuntimeTicks
	var intervalRuns atomic.Int32
	mgr := New(nil)
	mgr.SetTickInterval(10 * time.Millisecond)

	mgr.Register(TaskDefinition{
		ID:       "interval-task",
		Name:     "Interval Task",
		Category: "Test",
		Triggers: []TaskTriggerInfo{
			{Type: "IntervalTrigger", IntervalTicks: i64((20 * time.Millisecond).Nanoseconds() / 100)},
		},
		Run: func(ctx context.Context, _ func(float64)) error {
			intervalRuns.Add(1)
			return nil
		},
	})

	mgr.Register(TaskDefinition{
		ID:       "timeout-task",
		Name:     "Timeout Task",
		Category: "Test",
		Triggers: []TaskTriggerInfo{
			{Type: "IntervalTrigger", IntervalTicks: i64(time.Hour.Nanoseconds() / 100), MaxRuntimeTicks: i64((30 * time.Millisecond).Nanoseconds() / 100)},
		},
		Run: func(ctx context.Context, _ func(float64)) error {
			<-ctx.Done()
			return ctx.Err()
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	mgr.Start(ctx)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && intervalRuns.Load() < 1 {
		time.Sleep(15 * time.Millisecond)
	}
	cancel()
	if intervalRuns.Load() < 1 {
		t.Fatalf("expected interval-task to be triggered by Start loop")
	}

	// Verify MaxRuntimeTicks terminates long-running task
	if err := mgr.Run("timeout-task"); err != nil {
		t.Fatalf("Run timeout-task: %v", err)
	}
	mgr.Wait("timeout-task")
	tInfo, _ := mgr.Get("timeout-task")
	if tInfo.LastExecutionResult == nil || tInfo.LastExecutionResult.Status != "Cancelled" {
		t.Fatalf("expected Cancelled status on MaxRuntimeTicks timeout, got %+v", tInfo.LastExecutionResult)
	}

	// 2. CleanTranscodeCache skips recent (<2h) dirs and active dirs, removes old inactive dirs
	tmp := t.TempDir()
	recentDir := filepath.Join(tmp, "recent-sess")
	activeOldDir := filepath.Join(tmp, "active-old-sess")
	staleOldDir := filepath.Join(tmp, "stale-old-sess")
	for _, d := range []string{recentDir, activeOldDir, staleOldDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "seg0.ts"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	oldTime := time.Now().Add(-4 * time.Hour)
	_ = os.Chtimes(activeOldDir, oldTime, oldTime)
	_ = os.Chtimes(staleOldDir, oldTime, oldTime)

	mgr.SetActiveTranscodeDirsFn(func() []string { return []string{activeOldDir} })
	mgr.RegisterBuiltinTasks(nil, tmp, nil)
	if err := mgr.Run("CleanTranscodeCache"); err != nil {
		t.Fatal(err)
	}
	mgr.Wait("CleanTranscodeCache")

	if _, err := os.Stat(recentDir); err != nil {
		t.Fatalf("expected recent dir to be preserved, err=%v", err)
	}
	if _, err := os.Stat(activeOldDir); err != nil {
		t.Fatalf("expected active dir to be preserved, err=%v", err)
	}
	if _, err := os.Stat(staleOldDir); !os.IsNotExist(err) {
		t.Fatalf("expected stale inactive dir to be removed, stat err=%v", err)
	}

	// 3. Unit check for DailyTrigger / WeeklyTrigger schedule evaluation
	now := time.Date(2026, 3, 15, 9, 2, 0, 0, time.UTC) // Sunday 09:02
	dailyMT := &managedTask{
		def: TaskDefinition{
			Triggers: []TaskTriggerInfo{{Type: "DailyTrigger", TimeOfDayTicks: i64((9 * time.Hour).Nanoseconds() / 100)}},
		},
	}
	if !taskIsDueLocked(dailyMT, now) {
		t.Fatal("expected DailyTrigger to fire within 5m window with no prior run")
	}
	dailyMT.lastScheduledAt = now.Add(-1 * time.Minute)
	if taskIsDueLocked(dailyMT, now) {
		t.Fatal("expected DailyTrigger not to re-fire after already running for today's slot")
	}

	weeklyMT := &managedTask{
		def: TaskDefinition{
			Triggers: []TaskTriggerInfo{{Type: "WeeklyTrigger", DayOfWeek: "Sunday", TimeOfDayTicks: i64((9 * time.Hour).Nanoseconds() / 100)}},
		},
	}
	if !taskIsDueLocked(weeklyMT, now) {
		t.Fatal("expected WeeklyTrigger to fire on matching Sunday")
	}
	weeklyMT.def.Triggers[0].DayOfWeek = "Monday"
	if taskIsDueLocked(weeklyMT, now) {
		t.Fatal("expected WeeklyTrigger not to fire on non-matching weekday")
	}
}
