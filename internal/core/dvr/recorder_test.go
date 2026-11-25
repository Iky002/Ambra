package dvr

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/pkg/eventbus"
)

func waitForStatus(t *testing.T, r *Recorder, timerID string, want Status, timeout time.Duration) RecordingState {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		st, ok := r.GetRecording(timerID)
		if ok && st.Status == want {
			return st
		}
		time.Sleep(10 * time.Millisecond)
	}
	st, _ := r.GetRecording(timerID)
	t.Fatalf("timed out waiting for timer %q to reach status %q; got %q (err=%q)", timerID, want, st.Status, st.Error)
	return RecordingState{}
}

func TestDefaultArgsBuilderAndDefaults(t *testing.T) {
	r := NewRecorder(Options{})
	if r.RecordingsDir() != filepath.Join("./data", "recordings") {
		t.Fatalf("unexpected default recordings dir: %s", r.RecordingsDir())
	}

	job := RecordingJob{
		TimerID:     "t1",
		ChannelName: "CCTV-1",
		ProgramName: "Evening News",
		StreamURL:   "http://example.com/live.m3u8",
		Duration:    90 * time.Second,
	}
	got := DefaultArgsBuilder(job, "/tmp/out.mkv")
	want := []string{"-y", "-i", "http://example.com/live.m3u8", "-t", "90", "-c", "copy", "/tmp/out.mkv"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DefaultArgsBuilder got %v, want %v", got, want)
	}

	// Sub-second positive duration rounds up to at least 1s.
	jobShort := job
	jobShort.Duration = 200 * time.Millisecond
	gotShort := DefaultArgsBuilder(jobShort, "/tmp/out.mkv")
	if gotShort[4] != "1" {
		t.Fatalf("expected 1s duration, got %v", gotShort)
	}

	// Zero duration omits -t.
	jobUnlimited := job
	jobUnlimited.Duration = 0
	gotUnlimited := DefaultArgsBuilder(jobUnlimited, "/tmp/out.mkv")
	wantUnlimited := []string{"-y", "-i", "http://example.com/live.m3u8", "-c", "copy", "/tmp/out.mkv"}
	if !reflect.DeepEqual(gotUnlimited, wantUnlimited) {
		t.Fatalf("unlimited DefaultArgsBuilder got %v, want %v", gotUnlimited, wantUnlimited)
	}
}

func TestStartRecordingSuccessAndEventBus(t *testing.T) {
	tmpDir := t.TempDir()
	bus := eventbus.New()
	eventCh := make(chan eventbus.Event, 1)
	bus.Subscribe(eventbus.EventLibraryChanged, func(_ context.Context, e eventbus.Event) {
		eventCh <- e
	})

	rec := NewRecorder(Options{
		DataDir:    tmpDir,
		FFmpegPath: "/bin/sh",
		Bus:        bus,
		ArgsBuilder: func(_ RecordingJob, outFile string) []string {
			return []string{"-c", "printf 'mkv-data' > \"$1\"", "--", outFile}
		},
	})
	defer rec.Close()

	st, err := rec.StartRecording(context.Background(), RecordingJob{
		TimerID:     "timer-001",
		ChannelName: "CCTV 5+",
		ProgramName: "足球/直播",
		StreamURL:   "http://live.local/ch5.ts",
		Duration:    5 * time.Second,
	})
	if err != nil {
		t.Fatalf("StartRecording failed: %v", err)
	}
	if st.Status != StatusRecording {
		t.Fatalf("expected initial status Recording, got %q", st.Status)
	}

	final := waitForStatus(t, rec, "timer-001", StatusCompleted, 3*time.Second)
	if final.EndedAt.IsZero() {
		t.Fatalf("expected EndedAt to be populated")
	}
	data, err := os.ReadFile(final.OutputPath)
	if err != nil {
		t.Fatalf("expected output file %q to exist: %v", final.OutputPath, err)
	}
	if string(data) != "mkv-data" {
		t.Fatalf("unexpected file content: %q", string(data))
	}

	select {
	case ev := <-eventCh:
		if ev.Type != eventbus.EventLibraryChanged {
			t.Fatalf("unexpected event type: %q", ev.Type)
		}
		payload, ok := ev.Payload.(map[string]any)
		if !ok || payload["timer_id"] != "timer-001" || payload["output_path"] != final.OutputPath {
			t.Fatalf("unexpected event payload: %#v", ev.Payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected EventLibraryChanged to be published on completion")
	}
}

func TestStartRecordingErrorsAndFailure(t *testing.T) {
	tmpDir := t.TempDir()
	rec := NewRecorder(Options{
		DataDir:    tmpDir,
		FFmpegPath: "/bin/sh",
		ArgsBuilder: func(_ RecordingJob, _ string) []string {
			return []string{"-c", "exit 2"}
		},
	})
	defer rec.Close()

	if _, err := rec.StartRecording(context.Background(), RecordingJob{}); !errors.Is(err, ErrInvalidJob) {
		t.Fatalf("expected ErrInvalidJob, got %v", err)
	}

	if _, err := rec.StartRecording(context.Background(), RecordingJob{
		TimerID:   "fail-1",
		StreamURL: "http://live.local/fail.ts",
	}); err != nil {
		t.Fatalf("unexpected start error: %v", err)
	}
	st := waitForStatus(t, rec, "fail-1", StatusFailed, 3*time.Second)
	if st.Error == "" {
		t.Fatal("expected error message on failed recording")
	}

	// Non-existent ffmpeg binary should fail synchronously on Start.
	badRec := NewRecorder(Options{
		DataDir:    tmpDir,
		FFmpegPath: filepath.Join(tmpDir, "definitely-missing-ffmpeg"),
	})
	defer badRec.Close()
	if _, err := badRec.StartRecording(context.Background(), RecordingJob{
		TimerID:   "missing-bin",
		StreamURL: "http://live.local/a.ts",
	}); err == nil {
		t.Fatal("expected error when ffmpeg binary does not exist")
	}
	if st, ok := badRec.GetRecording("missing-bin"); !ok || st.Status != StatusFailed {
		t.Fatalf("expected failed state for missing binary, got %+v (ok=%v)", st, ok)
	}
}

func TestCancelRecordingActiveAndClose(t *testing.T) {
	tmpDir := t.TempDir()
	rec := NewRecorder(Options{
		DataDir:    tmpDir,
		FFmpegPath: "/bin/sh",
		ArgsBuilder: func(_ RecordingJob, _ string) []string {
			return []string{"-c", "sleep 10"}
		},
	})

	if err := rec.CancelRecording("nonexistent"); !errors.Is(err, ErrRecordingNotFound) {
		t.Fatalf("expected ErrRecordingNotFound, got %v", err)
	}

	for _, id := range []string{"b-timer", "a-timer"} {
		if _, err := rec.StartRecording(context.Background(), RecordingJob{
			TimerID:   id,
			StreamURL: "http://live.local/" + id,
		}); err != nil {
			t.Fatalf("StartRecording %s failed: %v", id, err)
		}
	}

	// Duplicate active TimerID should be rejected.
	if _, err := rec.StartRecording(context.Background(), RecordingJob{
		TimerID:   "a-timer",
		StreamURL: "http://live.local/dup",
	}); !errors.Is(err, ErrAlreadyRecording) {
		t.Fatalf("expected ErrAlreadyRecording, got %v", err)
	}

	active := rec.ActiveRecordings()
	if len(active) != 2 || active[0].Job.TimerID != "a-timer" || active[1].Job.TimerID != "b-timer" {
		t.Fatalf("unexpected active recordings: %+v", active)
	}

	if err := rec.CancelRecording("a-timer"); err != nil {
		t.Fatalf("CancelRecording failed: %v", err)
	}
	waitForStatus(t, rec, "a-timer", StatusCancelled, 3*time.Second)

	// Close cancels remaining active recording and prevents new ones.
	if err := rec.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	// Second Close is a no-op.
	if err := rec.Close(); err != nil {
		t.Fatalf("second Close failed: %v", err)
	}
	if st, ok := rec.GetRecording("b-timer"); !ok || st.Status != StatusCancelled {
		t.Fatalf("expected b-timer to be cancelled on Close, got %+v", st)
	}
	if _, err := rec.StartRecording(context.Background(), RecordingJob{
		TimerID:   "after-close",
		StreamURL: "http://live.local/x",
	}); !errors.Is(err, ErrRecorderClosed) {
		t.Fatalf("expected ErrRecorderClosed, got %v", err)
	}
}

func TestSchedulerLoop(t *testing.T) {
	tmpDir := t.TempDir()
	rec := NewRecorder(Options{
		DataDir:    tmpDir,
		FFmpegPath: "/bin/sh",
		ArgsBuilder: func(_ RecordingJob, outFile string) []string {
			return []string{"-c", "touch \"$1\"", "--", outFile}
		},
	})
	defer rec.Close()

	// Nil callback returns immediately.
	rec.SchedulerLoop(context.Background(), 0, nil)

	var calls atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		rec.SchedulerLoop(ctx, 20*time.Millisecond, func(_ context.Context) ([]RecordingJob, error) {
			n := calls.Add(1)
			if n == 1 {
				return nil, errors.New("temporary db error")
			}
			return []RecordingJob{
				{TimerID: "sched-1", ChannelName: "HBO", ProgramName: "Movie", StreamURL: "http://live.local/hbo"},
				{TimerID: "", StreamURL: ""}, // invalid job logged and skipped
			}, nil
		})
	}()

	waitForStatus(t, rec, "sched-1", StatusCompleted, 3*time.Second)
	// Let another tick fire to verify sched-1 is not re-triggered.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SchedulerLoop did not exit after context cancellation")
	}

	if calls.Load() < 2 {
		t.Fatalf("expected at least 2 polls, got %d", calls.Load())
	}
}

func TestSetOnFinishedCallback(t *testing.T) {
	tmpDir := t.TempDir()
	rec := NewRecorder(Options{
		DataDir:    tmpDir,
		FFmpegPath: "/bin/sh",
		ArgsBuilder: func(job RecordingJob, outFile string) []string {
			switch job.TimerID {
			case "cb-ok":
				return []string{"-c", "printf 'ok' > \"$1\"", "--", outFile}
			case "cb-fail":
				return []string{"-c", "exit 3"}
			default:
				return []string{"-c", "sleep 10"}
			}
		},
	})
	defer rec.Close()

	finishedCh := make(chan RecordingState, 3)
	rec.SetOnFinished(func(_ context.Context, st RecordingState) {
		// Calling GetRecording inside callback verifies r.mu was released before invoking callback.
		if _, ok := rec.GetRecording(st.Job.TimerID); !ok {
			t.Errorf("GetRecording(%q) returned false inside OnFinished", st.Job.TimerID)
		}
		finishedCh <- st
	})

	for _, id := range []string{"cb-ok", "cb-fail", "cb-cancel"} {
		if _, err := rec.StartRecording(context.Background(), RecordingJob{
			TimerID:   id,
			StreamURL: "http://live.local/" + id,
		}); err != nil {
			t.Fatalf("StartRecording %s: %v", id, err)
		}
	}
	if err := rec.CancelRecording("cb-cancel"); err != nil {
		t.Fatalf("CancelRecording: %v", err)
	}

	got := make(map[string]Status, 3)
	for i := 0; i < 3; i++ {
		select {
		case st := <-finishedCh:
			got[st.Job.TimerID] = st.Status
		case <-time.After(3 * time.Second):
			t.Fatalf("timed out waiting for OnFinished callbacks; got %+v", got)
		}
	}
	if got["cb-ok"] != StatusCompleted || got["cb-fail"] != StatusFailed || got["cb-cancel"] != StatusCancelled {
		t.Fatalf("unexpected OnFinished statuses: %+v", got)
	}
}

func TestStartRecordingStartFailureInvokesOnFinished(t *testing.T) {
	tmpDir := t.TempDir()
	rec := NewRecorder(Options{
		DataDir:    tmpDir,
		FFmpegPath: filepath.Join(tmpDir, "missing-ffmpeg"),
	})
	defer rec.Close()

	var called atomic.Bool
	var gotState RecordingState
	rec.SetOnFinished(func(_ context.Context, st RecordingState) {
		// Verify r.mu is not held during callback
		if _, ok := rec.GetRecording(st.Job.TimerID); !ok {
			t.Errorf("GetRecording returned false inside OnFinished")
		}
		gotState = st
		called.Store(true)
	})

	if _, err := rec.StartRecording(context.Background(), RecordingJob{
		TimerID:   "start-fail-cb",
		StreamURL: "http://live.local/stream.ts",
	}); err == nil {
		t.Fatal("expected StartRecording error for missing binary")
	}

	if !called.Load() {
		t.Fatal("expected OnFinished callback to be invoked on cmd.Start failure")
	}
	if gotState.Job.TimerID != "start-fail-cb" || gotState.Status != StatusFailed || gotState.Error == "" {
		t.Fatalf("unexpected failed callback state: %+v", gotState)
	}
}
