package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestSchedulerExtraBranches(t *testing.T) {
	mgr := New(nil)

	// Register with empty ID (falls back to Key) and IsHidden=true
	mgr.Register(TaskDefinition{
		Key:      "hidden-key",
		Name:     "Hidden Task",
		Category: "ZCat",
		IsHidden: true,
		Run: func(ctx context.Context, report func(float64)) error {
			report(-10)
			report(150)
			return errors.New("task boom")
		},
	})
	mgr.Register(TaskDefinition{
		ID:       "id-only",
		Key:      "lookup-by-key",
		Name:     "Alpha Task",
		Category: "ACat",
		Run:      nil,
	})

	if len(mgr.List(false)) != 1 {
		t.Fatalf("expected 1 visible task, got %d", len(mgr.List(false)))
	}
	if len(mgr.List(true)) != 2 {
		t.Fatalf("expected 2 total tasks, got %d", len(mgr.List(true)))
	}

	// Get by Key and missing
	if info, ok := mgr.Get("lookup-by-key"); !ok || info.Id != "id-only" {
		t.Fatalf("expected lookup by key to succeed, got %+v ok=%v", info, ok)
	}
	if _, ok := mgr.Get("does-not-exist"); ok {
		t.Fatalf("expected false for missing task")
	}

	// Run failing task with out-of-range progress
	if err := mgr.Run("hidden-key"); err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	mgr.Wait("hidden-key")
	info, _ := mgr.Get("hidden-key")
	if info.LastExecutionResult == nil || info.LastExecutionResult.Status != "Failed" || info.LastExecutionResult.ErrorMessage != "task boom" {
		t.Fatalf("expected Failed result, got %+v", info.LastExecutionResult)
	}

	// Run / Stop / SetTriggers error branches
	if err := mgr.Run("missing"); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("expected ErrTaskNotFound, got %v", err)
	}
	if err := mgr.Stop("missing"); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("expected ErrTaskNotFound on Stop, got %v", err)
	}
	if err := mgr.Stop("id-only"); !errors.Is(err, ErrTaskNotRunning) {
		t.Fatalf("expected ErrTaskNotRunning on idle Stop, got %v", err)
	}
	if err := mgr.SetTriggers("missing", nil); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("expected ErrTaskNotFound on SetTriggers, got %v", err)
	}

	// SetTriggers success
	interval := int64(3600)
	if err := mgr.SetTriggers("id-only", []TaskTriggerInfo{{Type: "IntervalTrigger", IntervalTicks: &interval}}); err != nil {
		t.Fatalf("SetTriggers failed: %v", err)
	}
	updated, _ := mgr.Get("id-only")
	if len(updated.Triggers) != 1 || updated.Triggers[0].Type != "IntervalTrigger" {
		t.Fatalf("unexpected triggers: %+v", updated.Triggers)
	}
}

func TestBuiltinTasksAllBranches(t *testing.T) {
	tmp := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(tmp, "sched.db"))
	if err != nil {
		t.Fatal(err)
	}

	// 1. OptimizeDatabase with live SQLite DB + RefreshLibrary failing + CleanTranscodeCache empty dir
	mgr := New(nil)
	mgr.RegisterBuiltinTasks(func(ctx context.Context) error {
		return errors.New("scan failed")
	}, "", db)

	_ = mgr.Run("OptimizeDatabase")
	mgr.Wait("OptimizeDatabase")
	optInfo, _ := mgr.Get("OptimizeDatabase")
	if optInfo.LastExecutionResult == nil || optInfo.LastExecutionResult.Status != "Completed" {
		t.Fatalf("expected OptimizeDatabase Completed, got %+v", optInfo.LastExecutionResult)
	}

	_ = mgr.Run("RefreshLibrary")
	mgr.Wait("RefreshLibrary")
	refInfo, _ := mgr.Get("RefreshLibrary")
	if refInfo.LastExecutionResult == nil || refInfo.LastExecutionResult.Status != "Failed" {
		t.Fatalf("expected RefreshLibrary Failed, got %+v", refInfo.LastExecutionResult)
	}

	_ = mgr.Run("CleanTranscodeCache")
	mgr.Wait("CleanTranscodeCache")
	cleanInfo, _ := mgr.Get("CleanTranscodeCache")
	if cleanInfo.LastExecutionResult == nil || cleanInfo.LastExecutionResult.Status != "Completed" {
		t.Fatalf("expected CleanTranscodeCache empty dir Completed, got %+v", cleanInfo.LastExecutionResult)
	}

	// 2. CleanTranscodeCache with nonexistent dir -> Completed
	mgr2 := New(nil)
	mgr2.RegisterBuiltinTasks(nil, filepath.Join(tmp, "nonexistent_dir"), nil)
	_ = mgr2.Run("CleanTranscodeCache")
	mgr2.Wait("CleanTranscodeCache")
	if c2, _ := mgr2.Get("CleanTranscodeCache"); c2.LastExecutionResult.Status != "Completed" {
		t.Fatalf("expected Completed for missing dir, got %+v", c2.LastExecutionResult)
	}

	// 3. CleanTranscodeCache with file instead of dir -> Failed; OptimizeDatabase with closed DB -> Failed
	notADir := filepath.Join(tmp, "afile")
	_ = os.WriteFile(notADir, []byte("x"), 0o644)
	_ = db.Close()

	mgr3 := New(nil)
	mgr3.RegisterBuiltinTasks(nil, notADir, db)
	_ = mgr3.Run("CleanTranscodeCache")
	mgr3.Wait("CleanTranscodeCache")
	if c3, _ := mgr3.Get("CleanTranscodeCache"); c3.LastExecutionResult.Status != "Failed" {
		t.Fatalf("expected Failed when transcode dir is a file, got %+v", c3.LastExecutionResult)
	}

	_ = mgr3.Run("OptimizeDatabase")
	mgr3.Wait("OptimizeDatabase")
	if o3, _ := mgr3.Get("OptimizeDatabase"); o3.LastExecutionResult.Status != "Failed" {
		t.Fatalf("expected Failed on closed DB, got %+v", o3.LastExecutionResult)
	}
}

func TestTriggerStorePersistenceAndReload(t *testing.T) {
	persisted := make(map[string][]TaskTriggerInfo)
	var saveErr error
	var loadErr error

	loadFn := func(_ context.Context) (map[string][]TaskTriggerInfo, error) {
		if loadErr != nil {
			return nil, loadErr
		}
		out := make(map[string][]TaskTriggerInfo, len(persisted))
		for k, v := range persisted {
			cp := make([]TaskTriggerInfo, len(v))
			copy(cp, v)
			out[k] = cp
		}
		return out, nil
	}
	saveFn := func(_ context.Context, triggers map[string][]TaskTriggerInfo) error {
		if saveErr != nil {
			return saveErr
		}
		persisted = make(map[string][]TaskTriggerInfo, len(triggers))
		for k, v := range triggers {
			cp := make([]TaskTriggerInfo, len(v))
			copy(cp, v)
			persisted[k] = cp
		}
		return nil
	}

	mgr := New(nil)
	// LoadPersistedTriggers is a no-op without store
	if err := mgr.LoadPersistedTriggers(context.Background()); err != nil {
		t.Fatalf("expected nil without store, got %v", err)
	}

	mgr.Register(TaskDefinition{ID: "t1", Name: "Task 1", Category: "Test"})
	mgr.SetTriggerStore(loadFn, saveFn)

	ticks := int64(7200_0000000)
	if err := mgr.SetTriggers("t1", []TaskTriggerInfo{{Type: "IntervalTrigger", IntervalTicks: &ticks}}); err != nil {
		t.Fatalf("SetTriggers failed: %v", err)
	}

	// Simulate save error -> triggers should roll back
	saveErr = errors.New("disk full")
	if err := mgr.SetTriggers("t1", []TaskTriggerInfo{}); !errors.Is(err, saveErr) {
		t.Fatalf("expected save error, got %v", err)
	}
	info, _ := mgr.Get("t1")
	if len(info.Triggers) != 1 || *info.Triggers[0].IntervalTicks != ticks {
		t.Fatalf("expected rollback on save error, got %+v", info.Triggers)
	}
	saveErr = nil

	// Simulate server restart with fresh manager
	mgr2 := New(nil)
	mgr2.Register(TaskDefinition{ID: "t1", Name: "Task 1", Category: "Test"})
	mgr2.SetTriggerStore(loadFn, saveFn)
	if err := mgr2.LoadPersistedTriggers(context.Background()); err != nil {
		t.Fatalf("LoadPersistedTriggers failed: %v", err)
	}
	restored, _ := mgr2.Get("t1")
	if len(restored.Triggers) != 1 || restored.Triggers[0].Type != "IntervalTrigger" || *restored.Triggers[0].IntervalTicks != ticks {
		t.Fatalf("expected persisted trigger restored, got %+v", restored.Triggers)
	}

	// Load error propagates
	loadErr = errors.New("read error")
	if err := mgr2.LoadPersistedTriggers(context.Background()); !errors.Is(err, loadErr) {
		t.Fatalf("expected load error, got %v", err)
	}
}
