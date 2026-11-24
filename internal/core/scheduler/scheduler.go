package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/ambra-media/ambra/internal/pkg/eventbus"
)

var (
	ErrTaskNotFound       = errors.New("scheduled task not found")
	ErrTaskAlreadyRunning = errors.New("scheduled task is already running")
	ErrTaskNotRunning     = errors.New("scheduled task is not running")
)

// TaskState represents the execution state of a scheduled task.
type TaskState string

const (
	StateIdle       TaskState = "Idle"
	StateCancelling TaskState = "Cancelling"
	StateRunning    TaskState = "Running"
)

// TaskTriggerInfo matches Emby's TaskTriggerInfo schema.
type TaskTriggerInfo struct {
	Type            string `json:"Type"`
	TimeOfDayTicks  *int64 `json:"TimeOfDayTicks,omitempty"`
	IntervalTicks   *int64 `json:"IntervalTicks,omitempty"`
	DayOfWeek       string `json:"DayOfWeek,omitempty"`
	MaxRuntimeTicks *int64 `json:"MaxRuntimeTicks,omitempty"`
}

// TaskResult matches Emby's TaskResult schema.
type TaskResult struct {
	StartTimeUtc     time.Time `json:"StartTimeUtc"`
	EndTimeUtc       time.Time `json:"EndTimeUtc"`
	Status           string    `json:"Status"` // Completed / Failed / Cancelled / Aborted
	Name             string    `json:"Name"`
	Key              string    `json:"Key"`
	Id               string    `json:"Id"`
	ErrorMessage     string    `json:"ErrorMessage,omitempty"`
	LongErrorMessage string    `json:"LongErrorMessage,omitempty"`
}

// TaskInfo matches Emby's TaskInfo DTO returned by /ScheduledTasks.
type TaskInfo struct {
	Name                      string            `json:"Name"`
	State                     TaskState         `json:"State"`
	CurrentProgressPercentage *float64          `json:"CurrentProgressPercentage,omitempty"`
	Id                        string            `json:"Id"`
	LastExecutionResult       *TaskResult       `json:"LastExecutionResult,omitempty"`
	Triggers                  []TaskTriggerInfo `json:"Triggers"`
	Description               string            `json:"Description"`
	Category                  string            `json:"Category"`
	IsHidden                  bool              `json:"IsHidden"`
	Key                       string            `json:"Key"`
}

// TaskFunc is executed asynchronously when a scheduled task runs.
type TaskFunc func(ctx context.Context, reportProgress func(pct float64)) error

// TaskDefinition describes a task registered with the Manager.
type TaskDefinition struct {
	ID          string
	Name        string
	Key         string
	Category    string
	Description string
	IsHidden    bool
	Triggers    []TaskTriggerInfo
	Run         TaskFunc
}

type managedTask struct {
	def      TaskDefinition
	state    TaskState
	progress *float64
	lastRes  *TaskResult
	cancel   context.CancelFunc
	doneCh   chan struct{}
}

// Manager orchestrates scheduled and manually-triggered background tasks.
type Manager struct {
	mu    sync.RWMutex
	tasks map[string]*managedTask
	bus   *eventbus.Bus
}

func New(bus *eventbus.Bus) *Manager {
	return &Manager{
		tasks: make(map[string]*managedTask),
		bus:   bus,
	}
}

// Register adds or replaces a task definition in the scheduler.
func (m *Manager) Register(def TaskDefinition) {
	if def.ID == "" {
		def.ID = def.Key
	}
	if def.Key == "" {
		def.Key = def.ID
	}
	if def.Triggers == nil {
		def.Triggers = []TaskTriggerInfo{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks[def.ID] = &managedTask{
		def:   def,
		state: StateIdle,
	}
}

// RegisterBuiltinTasks registers standard Ambra maintenance tasks:
// library scan, transcode cache cleanup, and SQLite database optimization.
func (m *Manager) RegisterBuiltinTasks(scanAll func(ctx context.Context) error, transcodeTempDir string, db *sql.DB) {
	interval12h := int64(12 * time.Hour / (100 * time.Nanosecond))
	interval24h := int64(24 * time.Hour / (100 * time.Nanosecond))

	m.Register(TaskDefinition{
		ID:          "RefreshLibrary",
		Name:        "扫描媒体库",
		Key:         "RefreshLibrary",
		Category:    "媒体库",
		Description: "扫描所有已配置的媒体库目录并更新媒体索引与元数据。",
		Triggers: []TaskTriggerInfo{
			{Type: "IntervalTrigger", IntervalTicks: &interval12h},
		},
		Run: func(ctx context.Context, report func(float64)) error {
			report(5)
			if scanAll != nil {
				if err := scanAll(ctx); err != nil {
					return err
				}
			}
			report(100)
			return nil
		},
	})

	m.Register(TaskDefinition{
		ID:          "CleanTranscodeCache",
		Name:        "清理转码缓存",
		Key:         "CleanTranscodeCache",
		Category:    "维护",
		Description: "清理过期的 HLS/FFmpeg 临时转码切片文件以释放磁盘空间。",
		Triggers: []TaskTriggerInfo{
			{Type: "IntervalTrigger", IntervalTicks: &interval24h},
		},
		Run: func(ctx context.Context, report func(float64)) error {
			report(10)
			if transcodeTempDir == "" {
				report(100)
				return nil
			}
			entries, err := os.ReadDir(transcodeTempDir)
			if err != nil {
				if os.IsNotExist(err) {
					report(100)
					return nil
				}
				return err
			}
			total := len(entries)
			for i, entry := range entries {
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}
				_ = os.RemoveAll(filepath.Join(transcodeTempDir, entry.Name()))
				if total > 0 {
					report(10 + float64(i+1)/float64(total)*90)
				}
			}
			report(100)
			return nil
		},
	})

	m.Register(TaskDefinition{
		ID:          "OptimizeDatabase",
		Name:        "优化 SQLite 数据库",
		Key:         "OptimizeDatabase",
		Category:    "维护",
		Description: "执行 SQLite WAL 检查点与统计信息优化，提升查询性能。",
		Triggers: []TaskTriggerInfo{
			{Type: "IntervalTrigger", IntervalTicks: &interval24h},
		},
		Run: func(ctx context.Context, report func(float64)) error {
			report(20)
			if db != nil {
				if _, err := db.ExecContext(ctx, `PRAGMA optimize`); err != nil {
					return err
				}
				report(60)
				if _, err := db.ExecContext(ctx, `PRAGMA wal_checkpoint(PASSIVE)`); err != nil {
					return err
				}
			}
			report(100)
			return nil
		},
	})
}

// List returns snapshots of all registered tasks sorted by category and name.
func (m *Manager) List(includeHidden bool) []TaskInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]TaskInfo, 0, len(m.tasks))
	for _, mt := range m.tasks {
		if !includeHidden && mt.def.IsHidden {
			continue
		}
		out = append(out, snapshotLocked(mt))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Category != out[j].Category {
			return out[i].Category < out[j].Category
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Get returns a snapshot of a single task by ID or Key.
func (m *Manager) Get(id string) (TaskInfo, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	mt := m.findLocked(id)
	if mt == nil {
		return TaskInfo{}, false
	}
	return snapshotLocked(mt), true
}

// Run starts a task asynchronously.
func (m *Manager) Run(id string) error {
	m.mu.Lock()
	mt := m.findLocked(id)
	if mt == nil {
		m.mu.Unlock()
		return ErrTaskNotFound
	}
	if mt.state != StateIdle {
		m.mu.Unlock()
		return ErrTaskAlreadyRunning
	}

	ctx, cancel := context.WithCancel(context.Background())
	startPct := 0.0
	mt.state = StateRunning
	mt.progress = &startPct
	mt.cancel = cancel
	doneCh := make(chan struct{})
	mt.doneCh = doneCh

	runFn := mt.def.Run
	snap := snapshotLocked(mt)
	m.mu.Unlock()

	m.publishProgress(snap)

	go func() {
		defer close(doneCh)
		startTime := time.Now().UTC()

		reportProgress := func(pct float64) {
			if pct < 0 {
				pct = 0
			}
			if pct > 100 {
				pct = 100
			}
			m.mu.Lock()
			p := pct
			mt.progress = &p
			s := snapshotLocked(mt)
			m.mu.Unlock()
			m.publishProgress(s)
		}

		var runErr error
		if runFn != nil {
			runErr = runFn(ctx, reportProgress)
		}
		cancel()

		endTime := time.Now().UTC()
		status := "Completed"
		errMsg := ""
		if runErr != nil {
			if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
				status = "Cancelled"
			} else {
				status = "Failed"
				errMsg = runErr.Error()
			}
		}

		m.mu.Lock()
		mt.state = StateIdle
		mt.progress = nil
		mt.cancel = nil
		mt.lastRes = &TaskResult{
			StartTimeUtc: startTime,
			EndTimeUtc:   endTime,
			Status:       status,
			Name:         mt.def.Name,
			Key:          mt.def.Key,
			Id:           mt.def.ID,
			ErrorMessage: errMsg,
		}
		finalSnap := snapshotLocked(mt)
		m.mu.Unlock()

		m.publishProgress(finalSnap)
	}()

	return nil
}

// Stop requests cancellation of a currently running task.
func (m *Manager) Stop(id string) error {
	m.mu.Lock()
	mt := m.findLocked(id)
	if mt == nil {
		m.mu.Unlock()
		return ErrTaskNotFound
	}
	if mt.state == StateIdle || mt.cancel == nil {
		m.mu.Unlock()
		return ErrTaskNotRunning
	}
	mt.state = StateCancelling
	cancel := mt.cancel
	snap := snapshotLocked(mt)
	m.mu.Unlock()

	cancel()
	m.publishProgress(snap)
	return nil
}

// SetTriggers replaces the trigger schedule for a task.
func (m *Manager) SetTriggers(id string, triggers []TaskTriggerInfo) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	mt := m.findLocked(id)
	if mt == nil {
		return ErrTaskNotFound
	}
	cp := make([]TaskTriggerInfo, len(triggers))
	copy(cp, triggers)
	mt.def.Triggers = cp
	return nil
}

// Wait blocks until the given task finishes its current execution (returns immediately if idle).
func (m *Manager) Wait(id string) {
	m.mu.RLock()
	mt := m.findLocked(id)
	var ch chan struct{}
	if mt != nil {
		ch = mt.doneCh
	}
	m.mu.RUnlock()
	if ch != nil {
		<-ch
	}
}

func (m *Manager) findLocked(id string) *managedTask {
	if mt, ok := m.tasks[id]; ok {
		return mt
	}
	for _, mt := range m.tasks {
		if mt.def.Key == id {
			return mt
		}
	}
	return nil
}

func (m *Manager) publishProgress(info TaskInfo) {
	if m.bus == nil {
		return
	}
	m.bus.Publish(context.Background(), eventbus.Event{
		Type:    eventbus.EventTaskProgress,
		Payload: info,
	})
}

func snapshotLocked(mt *managedTask) TaskInfo {
	var pct *float64
	if mt.progress != nil {
		v := *mt.progress
		pct = &v
	}
	var last *TaskResult
	if mt.lastRes != nil {
		cp := *mt.lastRes
		last = &cp
	}
	triggers := make([]TaskTriggerInfo, len(mt.def.Triggers))
	copy(triggers, mt.def.Triggers)
	return TaskInfo{
		Name:                      mt.def.Name,
		State:                     mt.state,
		CurrentProgressPercentage: pct,
		Id:                        mt.def.ID,
		LastExecutionResult:       last,
		Triggers:                  triggers,
		Description:               mt.def.Description,
		Category:                  mt.def.Category,
		IsHidden:                  mt.def.IsHidden,
		Key:                       mt.def.Key,
	}
}
