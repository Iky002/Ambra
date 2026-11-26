package dvr

import (
	"context"
	"net"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/pkg/netutil"
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

func TestC02_RealFFmpegAcceptsMaxRedirects(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not found in PATH")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	args := netutil.FilterFFmpegArgs(ffmpegPath, DefaultArgsBuilder(RecordingJob{
		TimerID:   "smoke",
		StreamURL: "http://" + addr + "/closed.ts",
		Duration:  time.Second,
	}, filepath.Join(t.TempDir(), "out.mkv")))
	out, _ := exec.CommandContext(ctx, ffmpegPath, args...).CombinedOutput()
	if strings.Contains(string(out), "Unrecognized option") || strings.Contains(string(out), "Option not found") {
		t.Fatalf("ffmpeg rejected DVR DefaultArgsBuilder options: %s", string(out))
	}
}
