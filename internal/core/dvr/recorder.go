package dvr

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ambra-media/ambra/internal/pkg/eventbus"
)

// Status represents the lifecycle state of a DVR recording job.
type Status string

const (
	StatusRecording Status = "Recording"
	StatusCompleted Status = "Completed"
	StatusFailed    Status = "Failed"
	StatusCancelled Status = "Cancelled"
)

var (
	ErrInvalidJob        = errors.New("dvr: invalid recording job")
	ErrAlreadyRecording  = errors.New("dvr: timer is already recording")
	ErrRecordingNotFound = errors.New("dvr: active recording not found")
	ErrRecorderClosed    = errors.New("dvr: recorder is closed")
)

// RecordingJob describes a Live TV recording task to execute.
type RecordingJob struct {
	TimerID     string
	ChannelName string
	ProgramName string
	StreamURL   string
	Duration    time.Duration
}

// RecordingState captures the runtime state of a recording task.
type RecordingState struct {
	Job        RecordingJob
	OutputPath string
	Status     Status
	StartedAt  time.Time
	EndedAt    time.Time
	Error      string
}

// Options configures a Recorder instance.
type Options struct {
	DataDir     string
	FFmpegPath  string
	Bus         *eventbus.Bus
	Logger      *slog.Logger
	ArgsBuilder func(job RecordingJob, outFile string) []string
}

type recordingEntry struct {
	state     RecordingState
	cancel    context.CancelFunc
	cancelled bool
	done      chan struct{}
}

// Recorder manages active and historical Live TV recording tasks backed by ffmpeg.
type Recorder struct {
	recordingsDir string
	ffmpegPath    string
	bus           *eventbus.Bus
	logger        *slog.Logger
	argsBuilder   func(job RecordingJob, outFile string) []string

	mu         sync.RWMutex
	entries    map[string]*recordingEntry
	onFinished func(ctx context.Context, state RecordingState)
	closed     bool
	wg         sync.WaitGroup
}

// DefaultArgsBuilder builds the standard ffmpeg remux command arguments.
func DefaultArgsBuilder(job RecordingJob, outFile string) []string {
	args := []string{"-y", "-i", job.StreamURL}
	if job.Duration > 0 {
		sec := int(math.Ceil(job.Duration.Seconds()))
		if sec < 1 {
			sec = 1
		}
		args = append(args, "-t", strconv.Itoa(sec))
	}
	args = append(args, "-c", "copy", outFile)
	return args
}

// NewRecorder creates a new DVR Recorder.
func NewRecorder(opts Options) *Recorder {
	dataDir := opts.DataDir
	if dataDir == "" {
		dataDir = "./data"
	}
	ffmpegPath := opts.FFmpegPath
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	argsBuilder := opts.ArgsBuilder
	if argsBuilder == nil {
		argsBuilder = DefaultArgsBuilder
	}
	return &Recorder{
		recordingsDir: filepath.Join(dataDir, "recordings"),
		ffmpegPath:    ffmpegPath,
		bus:           opts.Bus,
		logger:        logger,
		argsBuilder:   argsBuilder,
		entries:       make(map[string]*recordingEntry),
	}
}

// RecordingsDir returns the directory where recorded files are written.
func (r *Recorder) RecordingsDir() string {
	return r.recordingsDir
}

// SetOnFinished registers a callback invoked after a recording process finishes
// (Completed, Failed, or Cancelled). The callback is invoked without holding r.mu.
func (r *Recorder) SetOnFinished(fn func(ctx context.Context, state RecordingState)) {
	r.mu.Lock()
	r.onFinished = fn
	r.mu.Unlock()
}

// StartRecording launches an ffmpeg process to record the given job.
func (r *Recorder) StartRecording(ctx context.Context, job RecordingJob) (*RecordingState, error) {
	if strings.TrimSpace(job.TimerID) == "" || strings.TrimSpace(job.StreamURL) == "" {
		return nil, ErrInvalidJob
	}
	if err := os.MkdirAll(r.recordingsDir, 0o755); err != nil {
		return nil, fmt.Errorf("dvr: create recordings dir: %w", err)
	}

	now := time.Now().UTC()
	outFile := filepath.Join(r.recordingsDir, buildOutputFilename(job, now))

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, ErrRecorderClosed
	}
	if existing, ok := r.entries[job.TimerID]; ok && existing.state.Status == StatusRecording {
		r.mu.Unlock()
		return nil, ErrAlreadyRecording
	}

	recCtx, cancel := context.WithCancel(ctx)
	args := r.argsBuilder(job, outFile)
	cmd := exec.CommandContext(recCtx, r.ffmpegPath, args...)

	entry := &recordingEntry{
		state: RecordingState{
			Job:        job,
			OutputPath: outFile,
			Status:     StatusRecording,
			StartedAt:  now,
		},
		cancel: cancel,
		done:   make(chan struct{}),
	}

	if err := cmd.Start(); err != nil {
		cancel()
		entry.state.Status = StatusFailed
		entry.state.EndedAt = time.Now().UTC()
		entry.state.Error = err.Error()
		close(entry.done)
		r.entries[job.TimerID] = entry
		r.mu.Unlock()
		return nil, fmt.Errorf("dvr: start ffmpeg: %w", err)
	}

	r.entries[job.TimerID] = entry
	r.wg.Add(1)
	snapshot := entry.state
	r.mu.Unlock()

	go r.waitRecording(recCtx, cmd, entry)

	return &snapshot, nil
}

func (r *Recorder) waitRecording(recCtx context.Context, cmd *exec.Cmd, entry *recordingEntry) {
	defer r.wg.Done()
	defer close(entry.done)
	defer entry.cancel()

	err := cmd.Wait()
	endedAt := time.Now().UTC()

	r.mu.Lock()
	entry.state.EndedAt = endedAt
	wasCancelled := entry.cancelled || recCtx.Err() != nil
	switch {
	case wasCancelled:
		entry.state.Status = StatusCancelled
		if err != nil {
			entry.state.Error = err.Error()
		}
	case err != nil:
		entry.state.Status = StatusFailed
		entry.state.Error = err.Error()
	default:
		entry.state.Status = StatusCompleted
	}
	finalState := entry.state
	onFinished := r.onFinished
	r.mu.Unlock()

	if onFinished != nil {
		onFinished(context.Background(), finalState)
	}

	if finalState.Status == StatusCompleted && r.bus != nil {
		r.bus.Publish(context.Background(), eventbus.Event{
			Type: eventbus.EventLibraryChanged,
			Payload: map[string]any{
				"source":      "dvr",
				"timer_id":    finalState.Job.TimerID,
				"output_path": finalState.OutputPath,
			},
		})
	}
}

// CancelRecording terminates an active recording by TimerID.
func (r *Recorder) CancelRecording(timerID string) error {
	r.mu.Lock()
	entry, ok := r.entries[timerID]
	if !ok || entry.state.Status != StatusRecording {
		r.mu.Unlock()
		return ErrRecordingNotFound
	}
	entry.cancelled = true
	cancel := entry.cancel
	r.mu.Unlock()

	cancel()
	return nil
}

// GetRecording returns a snapshot of the recording state for the given TimerID.
func (r *Recorder) GetRecording(timerID string) (RecordingState, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.entries[timerID]
	if !ok {
		return RecordingState{}, false
	}
	return entry.state, true
}

// ActiveRecordings returns snapshots of all currently active recordings sorted by TimerID.
func (r *Recorder) ActiveRecordings() []RecordingState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]RecordingState, 0)
	for _, entry := range r.entries {
		if entry.state.Status == StatusRecording {
			out = append(out, entry.state)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Job.TimerID < out[j].Job.TimerID
	})
	return out
}

// Close cancels all active recordings and waits for all background processes to exit.
func (r *Recorder) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	cancels := make([]context.CancelFunc, 0, len(r.entries))
	for _, entry := range r.entries {
		if entry.state.Status == StatusRecording {
			entry.cancelled = true
			cancels = append(cancels, entry.cancel)
		}
	}
	r.mu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}
	r.wg.Wait()
	return nil
}

// SchedulerLoop polls fetchDueJobs at pollInterval and starts recordings for due jobs
// that have not yet been started. It exits gracefully when ctx is cancelled.
func (r *Recorder) SchedulerLoop(
	ctx context.Context,
	pollInterval time.Duration,
	fetchDueJobs func(context.Context) ([]RecordingJob, error),
) {
	if pollInterval <= 0 {
		pollInterval = 30 * time.Second
	}
	if fetchDueJobs == nil {
		return
	}

	poll := func() {
		jobs, err := fetchDueJobs(ctx)
		if err != nil {
			r.logger.Warn("dvr scheduler fetch due jobs failed", "err", err)
			return
		}
		for _, job := range jobs {
			if _, exists := r.GetRecording(job.TimerID); exists {
				continue
			}
			if _, err := r.StartRecording(ctx, job); err != nil {
				r.logger.Warn("dvr scheduler start recording failed", "timer_id", job.TimerID, "err", err)
			}
		}
	}

	poll()

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			poll()
		}
	}
}

func buildOutputFilename(job RecordingJob, now time.Time) string {
	ch := sanitizeFilenamePart(job.ChannelName)
	prog := sanitizeFilenamePart(job.ProgramName)
	id := sanitizeFilenamePart(job.TimerID)
	parts := make([]string, 0, 4)
	if ch != "" {
		parts = append(parts, ch)
	}
	if prog != "" {
		parts = append(parts, prog)
	}
	parts = append(parts, now.Format("20060102-150405"), id)
	return strings.Join(parts, "_") + ".mkv"
}

func sanitizeFilenamePart(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r > 127:
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}
