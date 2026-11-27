package dvr

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/pkg/eventbus"
)

func TestAuditWave1_L06_DurationDeadlineMarksCompletedNotCancelled(t *testing.T) {
	tmpDir := t.TempDir()
	bus := eventbus.New()
	var libChangedEvents atomic.Int32
	bus.Subscribe(eventbus.EventLibraryChanged, func(_ context.Context, _ eventbus.Event) {
		libChangedEvents.Add(1)
	})

	fakeFFmpeg := filepath.Join(tmpDir, "fake-dvr-ffmpeg.sh")
	script := "#!/bin/sh\n" +
		"out=\"$1\"\n" +
		"printf 'MATROSKA-RECORDING-PAYLOAD' > \"$out\"\n" +
		"trap 'exit 0' INT TERM\n" +
		"read -r _line || true\n" +
		"exit 0\n"
	if err := os.WriteFile(fakeFFmpeg, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	var capturedInput atomic.Value
	rec := NewRecorder(Options{
		DataDir:    tmpDir,
		FFmpegPath: fakeFFmpeg,
		Bus:        bus,
		ArgsBuilder: func(job RecordingJob, outFile string) []string {
			capturedInput.Store(job.StreamURL)
			return []string{outFile}
		},
	})
	rec.deadlinePad = 50 * time.Millisecond
	defer rec.Close()

	// 1. H-06 + L-06: Remote .m3u8 recording uses LoopbackStreamProxy URL and completes as StatusCompleted when Duration expires!
	upstreamM3U8 := "http://192.168.1.50:8080/live.m3u8"
	_, err := rec.StartRecording(context.Background(), RecordingJob{
		TimerID:        "deadline-timer-1",
		ChannelName:    "News HD",
		ProgramName:    "Scheduled Show",
		StreamURL:      upstreamM3U8,
		Duration:       150 * time.Millisecond,
		DenyPrivateLAN: false,
	})
	if err != nil {
		t.Fatalf("StartRecording: %v", err)
	}

	st := waitForStatus(t, rec, "deadline-timer-1", StatusCompleted, 5*time.Second)
	if st.Status != StatusCompleted {
		t.Fatalf("expected StatusCompleted on scheduled duration deadline, got %s (err=%s)", st.Status, st.Error)
	}
	gotInput, _ := capturedInput.Load().(string)
	if !strings.HasPrefix(gotInput, "http://127.0.0.1:") || !strings.Contains(gotInput, "/stream/") || gotInput == upstreamM3U8 {
		t.Fatalf("H-06: expected .m3u8 stream to be routed via LoopbackStreamProxy, got %q", gotInput)
	}
	if libChangedEvents.Load() != 1 {
		t.Fatalf("expected EventLibraryChanged on completed deadline recording, got %d", libChangedEvents.Load())
	}

	// 2. Explicit CancelRecording still marks StatusCancelled.
	_, err = rec.StartRecording(context.Background(), RecordingJob{
		TimerID:        "cancel-timer-1",
		ChannelName:    "News HD",
		ProgramName:    "Cancelled Show",
		StreamURL:      upstreamM3U8,
		Duration:       30 * time.Second,
		DenyPrivateLAN: false,
	})
	if err != nil {
		t.Fatalf("StartRecording(cancel): %v", err)
	}
	time.Sleep(40 * time.Millisecond)
	if err := rec.CancelRecording("cancel-timer-1"); err != nil {
		t.Fatalf("CancelRecording: %v", err)
	}
	stCancel := waitForStatus(t, rec, "cancel-timer-1", StatusCancelled, 5*time.Second)
	if stCancel.Status != StatusCancelled {
		t.Fatalf("expected StatusCancelled on explicit cancellation, got %s", stCancel.Status)
	}
}
