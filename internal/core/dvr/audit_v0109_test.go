package dvr

import (
	"context"
	"testing"
)

func TestAuditV0109_H05_DenyPrivateLANEnforced(t *testing.T) {
	rec := NewRecorder(Options{
		DataDir:    t.TempDir(),
		FFmpegPath: "/bin/sh",
		ArgsBuilder: func(_ RecordingJob, _ string) []string {
			return []string{"-c", "exit 0"}
		},
	})
	defer rec.Close()

	// When DenyPrivateLAN is true, RFC1918 private LAN addresses must be rejected.
	if _, err := rec.StartRecording(context.Background(), RecordingJob{
		TimerID:        "lan-denied",
		StreamURL:      "http://192.168.1.50:8080/live.ts",
		DenyPrivateLAN: true,
	}); err == nil {
		t.Fatal("expected private LAN URL to be rejected when DenyPrivateLAN=true")
	}

	// When DenyPrivateLAN is false (default), LAN IPTV streams remain permitted.
	if _, err := rec.StartRecording(context.Background(), RecordingJob{
		TimerID:        "lan-allowed",
		StreamURL:      "http://192.168.1.50:8080/live.ts",
		DenyPrivateLAN: false,
	}); err != nil {
		t.Fatalf("expected private LAN URL to be allowed when DenyPrivateLAN=false, got: %v", err)
	}
}
