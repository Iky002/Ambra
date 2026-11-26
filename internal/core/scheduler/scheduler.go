package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ambra-media/ambra/internal/pkg/eventbus"
)

const (
	ticksPerSecond               = 10_000_000
	defaultSchedulerTickInterval = 15 * time.Second
	transcodeCleanMinAge         = 2 * time.Hour
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
	def             TaskDefinition
	state           TaskState
	progress        *float64
	lastRes         *TaskResult
	cancel          context.CancelFunc
	doneCh          chan struct{}
	createdAt       time.Time
	lastScheduledAt time.Time
}

// Manager orchestrates scheduled and manually-triggered background tasks.
type Manager struct {
	mu                    sync.RWMutex
	triggerMu             sync.Mutex
	tasks                 map[string]*managedTask
	bus                   *eventbus.Bus
	loadTriggers          func(ctx context.Context) (map[string][]TaskTriggerInfo, error)
	saveTriggers          func(ctx context.Context, triggers map[string][]TaskTriggerInfo) error
	activeTranscodeDirsFn func() []string
	tickInterval          time.Duration
	nowFn                 func() time.Time
	started               atomic.Bool
	taskWG                sync.WaitGroup
}

func New(bus *eventbus.Bus) *Manager {
	return &Manager{
		tasks:        make(map[string]*managedTask),
		bus:          bus,
		tickInterval: defaultSchedulerTickInterval,
		nowFn:        time.Now,
	}
}

// SetActiveTranscodeDirsFn registers a callback returning active transcoding session directories
// so CleanTranscodeCache never deletes directories currently in use.
func (m *Manager) SetActiveTranscodeDirsFn(fn func() []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activeTranscodeDirsFn = fn
}

// SetTickInterval configures how frequently Start polls task triggers (defaults to 15s).
func (m *Manager) SetTickInterval(d time.Duration) {
	if d <= 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tickInterval = d
}

// Start launches the background scheduling loop that fires StartupTrigger tasks on launch
// and periodically evaluates IntervalTrigger, DailyTrigger, and WeeklyTrigger definitions
// until ctx is cancelled.
func (m *Manager) Start(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	if !m.started.CompareAndSwap(false, true) {
		return
	}
	m.mu.RLock()
	interval := m.tickInterval
	m.mu.RUnlock()
	if interval <= 0 {
		interval = defaultSchedulerTickInterval
	}

	go func() {
		defer m.started.Store(false)
		if ctx.Err() != nil {
			m.StopAll()
			return
		}
		m.runStartupTasks()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				m.StopAll()
				return
			case <-ticker.C:
				m.evaluateAndRunDue()
			}
		}
	}()
}

func (m *Manager) runStartupTasks() {
	m.mu.RLock()
	nowFn := m.nowFn
	m.mu.RUnlock()
	now := nowFn()

	var startupIDs []string
	m.mu.Lock()
	for _, mt := range m.tasks {
		if mt.state != StateIdle {
			continue
		}
		for _, tr := range mt.def.Triggers {
			if strings.TrimSpace(tr.Type) == "StartupTrigger" {
				mt.lastScheduledAt = now
				startupIDs = append(startupIDs, mt.def.ID)
				break
			}
		}
	}
	m.mu.Unlock()

	for _, id := range startupIDs {
		_ = m.Run(id)
	}
}

// StopAll cancels the context of every currently running scheduled task.
func (m *Manager) StopAll() int {
	var (
		cancels []context.CancelFunc
		snaps   []TaskInfo
	)
	m.mu.Lock()
	for _, mt := range m.tasks {
		if mt.state != StateIdle && mt.cancel != nil {
			mt.state = StateCancelling
			cancels = append(cancels, mt.cancel)
			snaps = append(snaps, snapshotLocked(mt))
		}
	}
	m.mu.Unlock()

	for _, c := range cancels {
		c()
	}
	for _, s := range snaps {
		m.publishProgress(s)
	}
	return len(cancels)
}

// Shutdown cancels all running scheduled tasks and waits for them to finish or ctx to expire.
func (m *Manager) Shutdown(ctx context.Context) error {
	m.StopAll()
	done := make(chan struct{})
	go func() {
		m.taskWG.Wait()
		close(done)
	}()
	if ctx == nil {
		<-done
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) evaluateAndRunDue() {
	m.mu.RLock()
	nowFn := m.nowFn
	m.mu.RUnlock()
	now := nowFn()

	var dueIDs []string
	m.mu.Lock()
	for _, mt := range m.tasks {
		if mt.state != StateIdle {
			continue
		}
		if taskIsDueLocked(mt, now) {
			mt.lastScheduledAt = now
			dueIDs = append(dueIDs, mt.def.ID)
		}
	}
	m.mu.Unlock()

	for _, id := range dueIDs {
		_ = m.Run(id)
	}
}

func taskIsDueLocked(mt *managedTask, now time.Time) bool {
	lastRun := mt.lastScheduledAt
	if mt.lastRes != nil && mt.lastRes.EndTimeUtc.After(lastRun) {
		lastRun = mt.lastRes.EndTimeUtc
	}
	for _, tr := range mt.def.Triggers {
		switch strings.TrimSpace(tr.Type) {
		case "IntervalTrigger":
			if tr.IntervalTicks == nil || *tr.IntervalTicks <= 0 {
				continue
			}
			interval := ticksToDuration(*tr.IntervalTicks)
			if interval <= 0 {
				continue
			}
			anchor := lastRun
			if anchor.IsZero() {
				anchor = mt.createdAt
			}
			if now.Sub(anchor) >= interval {
				return true
			}
		case "DailyTrigger":
			if tr.TimeOfDayTicks == nil || *tr.TimeOfDayTicks < 0 {
				continue
			}
			for _, day := range []time.Time{now, now.AddDate(0, 0, -1)} {
				scheduled := dailyTriggerTime(day, *tr.TimeOfDayTicks)
				if !now.Before(scheduled) && now.Sub(scheduled) < 5*time.Minute && lastRun.Before(scheduled) {
					return true
				}
			}
		case "WeeklyTrigger":
			if tr.TimeOfDayTicks == nil || *tr.TimeOfDayTicks < 0 {
				continue
			}
			wantDay := strings.TrimSpace(tr.DayOfWeek)
			for _, day := range []time.Time{now, now.AddDate(0, 0, -1)} {
				if !strings.EqualFold(wantDay, day.Weekday().String()) {
					continue
				}
				scheduled := dailyTriggerTime(day, *tr.TimeOfDayTicks)
				if !now.Before(scheduled) && now.Sub(scheduled) < 5*time.Minute && lastRun.Before(scheduled) {
					return true
				}
			}
		}
	}
	return false
}

func dailyTriggerTime(now time.Time, timeOfDayTicks int64) time.Time {
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return dayStart.Add(ticksToDuration(timeOfDayTicks))
}

func ticksToDuration(ticks int64) time.Duration {
	if ticks <= 0 {
		return 0
	}
	// 1 tick = 100 nanoseconds; guard against int64 overflow when multiplying.
	const maxTicks = int64(^uint64(0)>>1) / 100
	if ticks > maxTicks {
		return time.Duration(maxTicks) * 100 * time.Nanosecond
	}
	return time.Duration(ticks) * 100 * time.Nanosecond
}

// SetTriggerStore configures optional callbacks used to persist custom task triggers across restarts.
func (m *Manager) SetTriggerStore(
	load func(ctx context.Context) (map[string][]TaskTriggerInfo, error),
	save func(ctx context.Context, triggers map[string][]TaskTriggerInfo) error,
) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loadTriggers = load
	m.saveTriggers = save
}

// LoadPersistedTriggers loads saved custom triggers from the configured trigger store
// and applies them to registered tasks.
func (m *Manager) LoadPersistedTriggers(ctx context.Context) error {
	m.mu.RLock()
	loadFn := m.loadTriggers
	m.mu.RUnlock()
	if loadFn == nil {
		return nil
	}
	persisted, err := loadFn(ctx)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, triggers := range persisted {
		mt := m.findLocked(id)
		if mt == nil {
			continue
		}
		cp := make([]TaskTriggerInfo, len(triggers))
		copy(cp, triggers)
		mt.def.Triggers = cp
	}
	return nil
}

// Register adds or replaces a task definition in the scheduler.
// If replacing an existing running task, its previous run context is cancelled cleanly.
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
	var oldCancel context.CancelFunc
	if existing, ok := m.tasks[def.ID]; ok && existing != nil {
		if existing.state != StateIdle && existing.cancel != nil {
			existing.state = StateCancelling
			oldCancel = existing.cancel
		}
	}
	now := m.nowFn()
	m.tasks[def.ID] = &managedTask{
		def:       def,
		state:     StateIdle,
		createdAt: now,
	}
	m.mu.Unlock()
	if oldCancel != nil {
		oldCancel()
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
			m.mu.RLock()
			activeFn := m.activeTranscodeDirsFn
			m.mu.RUnlock()
			activeDirs := make(map[string]struct{})
			if activeFn != nil {
				for _, d := range activeFn() {
					if clean := filepath.Clean(strings.TrimSpace(d)); clean != "" && clean != "." {
						activeDirs[clean] = struct{}{}
					}
				}
			}

			total := len(entries)
			for i, entry := range entries {
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}
				fullPath := filepath.Join(transcodeTempDir, entry.Name())
				if _, active := activeDirs[filepath.Clean(fullPath)]; active {
					if total > 0 {
						report(10 + float64(i+1)/float64(total)*90)
					}
					continue
				}
				if entry.IsDir() {
					if info, statErr := entry.Info(); statErr == nil && time.Since(info.ModTime()) < transcodeCleanMinAge {
						if total > 0 {
							report(10 + float64(i+1)/float64(total)*90)
						}
						continue
					}
				}
				_ = os.RemoveAll(fullPath)
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
		Description: "执行 SQLite PRAGMA optimize、ANALYZE 与 WAL 检查点，刷新查询规划器统计信息以提升查询性能。",
		Triggers: []TaskTriggerInfo{
			{Type: "IntervalTrigger", IntervalTicks: &interval24h},
		},
		Run: func(ctx context.Context, report func(float64)) error {
			report(15)
			if db != nil {
				if _, err := db.ExecContext(ctx, `PRAGMA optimize;`); err != nil {
					return err
				}
				report(40)
				if _, err := db.ExecContext(ctx, `ANALYZE;`); err != nil {
					return err
				}
				report(75)
				if _, err := db.ExecContext(ctx, `PRAGMA wal_checkpoint(PASSIVE);`); err != nil {
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

	var maxRuntime time.Duration
	for _, tr := range mt.def.Triggers {
		if tr.MaxRuntimeTicks != nil && *tr.MaxRuntimeTicks > 0 {
			d := ticksToDuration(*tr.MaxRuntimeTicks)
			if d > 0 && (maxRuntime == 0 || d < maxRuntime) {
				maxRuntime = d
			}
		}
	}

	var ctx context.Context
	var cancel context.CancelFunc
	if maxRuntime > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), maxRuntime)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	startPct := 0.0
	mt.state = StateRunning
	mt.progress = &startPct
	mt.cancel = cancel
	doneCh := make(chan struct{})
	mt.doneCh = doneCh

	runFn := mt.def.Run
	snap := snapshotLocked(mt)
	m.taskWG.Add(1)
	m.mu.Unlock()

	m.publishProgress(snap)

	go func() {
		defer m.taskWG.Done()
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
		var longErr string
		if runFn != nil {
			func() {
				defer func() {
					if r := recover(); r != nil {
						stack := string(debug.Stack())
						slog.Error("scheduled task panicked", "task_id", mt.def.ID, "panic", r, "stack", stack)
						runErr = fmt.Errorf("task panicked: %v", r)
						longErr = stack
					}
				}()
				runErr = runFn(ctx, reportProgress)
			}()
		}
		cancel()

		endTime := time.Now().UTC()
		status := "Completed"
		errMsg := ""
		if runErr != nil {
			if longErr == "" && (errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded)) {
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
			StartTimeUtc:     startTime,
			EndTimeUtc:       endTime,
			Status:           status,
			Name:             mt.def.Name,
			Key:              mt.def.Key,
			Id:               mt.def.ID,
			ErrorMessage:     errMsg,
			LongErrorMessage: longErr,
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

// SetTriggers replaces the trigger schedule for a task and persists it when a trigger store is configured.
func (m *Manager) SetTriggers(id string, triggers []TaskTriggerInfo) error {
	m.triggerMu.Lock()
	defer m.triggerMu.Unlock()

	cp := make([]TaskTriggerInfo, len(triggers))
	copy(cp, triggers)

	m.mu.RLock()
	mt := m.findLocked(id)
	if mt == nil {
		m.mu.RUnlock()
		return ErrTaskNotFound
	}
	targetID := mt.def.ID
	saveFn := m.saveTriggers
	var snapshot map[string][]TaskTriggerInfo
	if saveFn != nil {
		snapshot = make(map[string][]TaskTriggerInfo, len(m.tasks))
		for taskID, task := range m.tasks {
			if taskID == targetID {
				tCopy := make([]TaskTriggerInfo, len(cp))
				copy(tCopy, cp)
				snapshot[taskID] = tCopy
				continue
			}
			tCopy := make([]TaskTriggerInfo, len(task.def.Triggers))
			copy(tCopy, task.def.Triggers)
			snapshot[taskID] = tCopy
		}
	}
	m.mu.RUnlock()

	if saveFn != nil {
		if err := saveFn(context.Background(), snapshot); err != nil {
			return err
		}
	}

	m.mu.Lock()
	if cur := m.findLocked(targetID); cur != nil {
		cur.def.Triggers = cp
	}
	m.mu.Unlock()
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
