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
