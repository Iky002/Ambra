package dvr

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestStartRecordingFailureNotifiesOnFinished(t *testing.T) {
	rec := NewRecorder(Options{
		DataDir:    t.TempDir(),
		FFmpegPath: "/nonexistent/ffmpeg-binary",
	})
	defer rec.Close()

	var (
		mu       sync.Mutex
		finished []RecordingState
	)
	rec.SetOnFinished(func(_ context.Context, st RecordingState) {
		mu.Lock()
		finished = append(finished, st)
		mu.Unlock()
	})

	// 1. Empty StreamURL fails and triggers onFinished(StatusFailed)
	if _, err := rec.StartRecording(context.Background(), RecordingJob{
		TimerID:   "timer-empty-url",
		StreamURL: "",
	}); !errors.Is(err, ErrInvalidJob) {
		t.Fatalf("expected ErrInvalidJob for empty URL, got %v", err)
	}

	// 2. Disallowed loopback StreamURL fails and triggers onFinished(StatusFailed)
	if _, err := rec.StartRecording(context.Background(), RecordingJob{
		TimerID:   "timer-loopback-url",
		StreamURL: "http://127.0.0.1:8080/live.ts",
		UserAgent: "CustomTuner/1.0",
		Duration:  time.Minute,
	}); !errors.Is(err, ErrInvalidJob) {
		t.Fatalf("expected ErrInvalidJob for loopback URL, got %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(finished) != 2 {
		t.Fatalf("expected 2 onFinished callbacks, got %d", len(finished))
	}
	for _, st := range finished {
		if st.Status != StatusFailed {
			t.Fatalf("expected StatusFailed for %s, got %s", st.Job.TimerID, st.Status)
		}
	}
}
