package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestAuditV0107_StartupTrigger(t *testing.T) {
	mgr := New(nil)
	mgr.SetTickInterval(50 * time.Millisecond)

	ranCh := make(chan struct{}, 1)
	var runCount atomic.Int32
	mgr.Register(TaskDefinition{
		ID:       "startup-task",
		Name:     "Startup Maintenance",
		Key:      "StartupMaintenance",
		Category: "Maintenance",
		Triggers: []TaskTriggerInfo{
			{Type: "StartupTrigger"},
		},
		Run: func(ctx context.Context, report func(float64)) error {
			report(100)
			if runCount.Add(1) == 1 {
				ranCh <- struct{}{}
			}
			return nil
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.Start(ctx)

	select {
	case <-ranCh:
	case <-time.After(2 * time.Second):
		t.Fatal("expected StartupTrigger task to run on Manager.Start")
	}
	mgr.Wait("startup-task")
	cancel()
	// Verify it ran only once on startup
	time.Sleep(80 * time.Millisecond)
	if got := runCount.Load(); got != 1 {
		t.Fatalf("expected StartupTrigger task to run once, got %d", got)
	}
}
