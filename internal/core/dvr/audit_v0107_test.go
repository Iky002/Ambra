package dvr

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestAuditV0107_H5_WallClockFallbackAndWatchCancelTrackedInWG(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script mocks require unix")
	}
	tmp := t.TempDir()
	fakeFF := filepath.Join(tmp, "sleep-ffmpeg.sh")
	if err := os.WriteFile(fakeFF, []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := NewRecorder(Options{
		DataDir:    tmp,
		FFmpegPath: fakeFF,
	})
	r.gracefulTimeout = 100 * time.Millisecond

	_, err := r.StartRecording(context.Background(), RecordingJob{
		TimerID:     "h5-timer",
		ChannelName: "Ch1",
		ProgramName: "Show",
		StreamURL:   "https://93.184.216.34/live.m3u8",
		Duration:    5 * time.Second,
	})
	if err != nil {
		t.Fatalf("StartRecording: %v", err)
	}

	// Close() waits for BOTH waitRecording and watchCancel via r.wg without leaking.
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	st, ok := r.GetRecording("h5-timer")
	if !ok || st.Status != StatusCancelled {
		t.Fatalf("expected Cancelled after Close, got ok=%v status=%v", ok, st.Status)
	}
}
