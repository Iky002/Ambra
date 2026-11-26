package transcode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/config"
)

func TestAuditV0109_H08_NoHTTPURLInSubtitlesFilterAndStaging(t *testing.T) {
	subIdx := 2
	// 1. BuildFFmpegArgs never emits http(s):// inside -vf subtitles=
	argsExt := BuildFFmpegArgs(SessionParams{
		InputPath:      "/media/movie.mkv",
		VideoCodec:     "h264",
		SubtitleMethod: "Encode",
		SubtitlePath:   "https://example.com/sub.srt",
		SubtitleCodec:  "srt",
	}, 0, "/tmp/out", SoftwareOnlyCapabilities())
	for _, a := range argsExt {
		if strings.Contains(a, "subtitles=") && strings.Contains(a, "https://") {
			t.Fatalf("BuildFFmpegArgs leaked HTTP subtitle URL into filter: %v", argsExt)
		}
	}

	argsEmb := BuildFFmpegArgs(SessionParams{
		InputPath:             "https://example.com/movie.mkv",
		VideoCodec:            "h264",
		SubtitleMethod:        "Encode",
		SubtitleStreamIndex:   &subIdx,
		SubtitleRelativeIndex: 0,
		SubtitleCodec:         "ass",
	}, 0, "/tmp/out", SoftwareOnlyCapabilities())
	for _, a := range argsEmb {
		if strings.Contains(a, "subtitles=") && strings.Contains(a, "https://") {
			t.Fatalf("BuildFFmpegArgs leaked HTTP media URL into subtitles filter: %v", argsEmb)
		}
	}

	// 2. stageBurnInFilesWithDeps downloads remote external subtitle into workDir on success
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ok.srt" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("1\n00:00:01,000 --> 00:00:02,000\nHello\n"))
			return
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	workDir := t.TempDir()
	staged := stageBurnInFilesWithDeps(SessionParams{
		InputPath:      "/media/movie.mkv",
		SubtitleMethod: "Encode",
		SubtitlePath:   srv.URL + "/ok.srt",
		SubtitleCodec:  "srt",
	}, workDir, "ffmpeg", srv.Client())
	if !strings.HasPrefix(staged.SubtitlePath, workDir) || !fileReady(staged.SubtitlePath) {
		t.Fatalf("expected downloaded local subtitle in workDir, got %+v", staged)
	}

	// 3. On download failure, clear SubtitlePath & SubtitleMethod so URL is never passed to FFmpeg
	workDir2 := t.TempDir()
	stagedFail := stageBurnInFilesWithDeps(SessionParams{
		InputPath:      "/media/movie.mkv",
		SubtitleMethod: "Encode",
		SubtitlePath:   srv.URL + "/denied.srt",
		SubtitleCodec:  "srt",
	}, workDir2, "ffmpeg", srv.Client())
	if stagedFail.SubtitlePath != "" || stagedFail.SubtitleMethod != "" {
		t.Fatalf("expected failed remote subtitle staging to clear burn-in fields, got path=%q method=%q",
			stagedFail.SubtitlePath, stagedFail.SubtitleMethod)
	}
}

func TestAuditV0109_H13_WaitForFileExitsOnSeekRestartAndTimeout(t *testing.T) {
	workDir := t.TempDir()
	done1 := make(chan struct{})
	done2 := make(chan struct{})
	sess := &transcodeSession{
		workDir:            workDir,
		startSeg:           10, // restarted ahead at segment 10
		doneCh:             done2,
		segmentWaitTimeout: 150 * time.Millisecond,
	}
	close(done1)

	// Case 1: waiting for segment 2 when session restarted at segment 10 -> exit immediately with error
	_, err := sess.waitForFile(context.Background(), filepath.Join(workDir, "seg_2.ts"), 2, "segment 2", done1)
	if err == nil || !strings.Contains(err.Error(), "restarted at segment 10") {
		t.Fatalf("expected restart error for obsolete segment, got %v", err)
	}

	// Case 2: waiting when ffmpeg never produces segment -> exits on segmentWaitTimeout
	start := time.Now()
	_, err = sess.waitForFile(context.Background(), filepath.Join(workDir, "seg_10.ts"), 10, "segment 10", done2)
	if err == nil || !strings.Contains(err.Error(), "timed out waiting") {
		t.Fatalf("expected segment wait timeout error, got %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("waitForFile took too long: %v", time.Since(start))
	}
}

func TestAuditV0109_H14_TextBurnInSeekKeyframeExpr(t *testing.T) {
	args := BuildFFmpegArgs(SessionParams{
		InputPath:       "/media/movie.mkv",
		VideoCodec:      "h264",
		SubtitleMethod:  "Encode",
		SubtitlePath:    "/media/movie.ass",
		SubtitleCodec:   "ass",
		SegmentDuration: 6,
	}, 10, "/tmp/out", SoftwareOnlyCapabilities())

	found := false
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "-force_key_frames" {
			found = true
			want := "expr:gte(t-60,n_forced*6)"
			if args[i+1] != want {
				t.Fatalf("-force_key_frames = %q, want %q", args[i+1], want)
			}
		}
	}
	if !found {
		t.Fatalf("expected -force_key_frames in args: %v", args)
	}
}

func TestAuditV0109_H15_M29_FontExtractionSafetyAndMarker(t *testing.T) {
	// M-29: Reject control chars and overly long names
	if isSafeAttachmentFilename("font\nname.ttf") {
		t.Fatal("expected newline in attachment filename rejected")
	}
	if isSafeAttachmentFilename("font\rname.ttf") {
		t.Fatal("expected CR in attachment filename rejected")
	}
	if isSafeAttachmentFilename(strings.Repeat("a", 256) + ".ttf") {
		t.Fatal("expected >255 byte attachment filename rejected")
	}
	if !isSafeAttachmentFilename("NotoSansCJK-Regular.otf") {
		t.Fatal("expected valid font filename accepted")
	}

	// H-15: Failing ffprobe must return error and NOT write .extracted marker
	tmp := t.TempDir()
	cfg := &config.Config{
		DataDir:     tmp,
		FFmpegPath:  "/bin/sh",
		FFprobePath: filepath.Join(tmp, "nonexistent-ffprobe"),
	}
	mediaFile := filepath.Join(tmp, "sample.mkv")
	if err := os.WriteFile(mediaFile, []byte("fake"), 0o600); err != nil {
		t.Fatal(err)
	}
	ext := NewExtractor(cfg)
	if _, err := ext.ExtractFonts(context.Background(), mediaFile, "item1", "ms1"); err == nil {
		t.Fatal("expected error when ffprobe fails")
	}
	marker := filepath.Join(cfg.SubtitleCacheDir(), "fonts", "item1_ms1", ".extracted")
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("expected .extracted marker NOT to be written on probe failure")
	}
}

func TestAuditV0109_M33_GenerateBIFWithContextHonorsCancellation(t *testing.T) {
	tmp := t.TempDir()
	// Script that sleeps until killed by context cancellation.
	fakeFFmpeg := filepath.Join(tmp, "fake-ffmpeg.sh")
	if err := os.WriteFile(fakeFFmpeg, []byte("#!/bin/sh\nexec sleep 10\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	mediaFile := filepath.Join(tmp, "video.mkv")
	if err := os.WriteFile(mediaFile, []byte("fake"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{DataDir: tmp, FFmpegPath: fakeFFmpeg}
	ext := NewExtractor(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := ext.GenerateBIFWithContext(ctx, mediaFile, "bif-cancel", 320, 10)
	if err == nil {
		t.Fatal("expected cancellation error from GenerateBIFWithContext")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("GenerateBIFWithContext did not cancel promptly: %v", time.Since(start))
	}
}

func TestAuditV0109_L22_AnonymousSessionQuotaEnforced(t *testing.T) {
	cfg := &config.Config{DataDir: t.TempDir()}
	mgr := NewManager(cfg, WithMaxSessions(10), WithMaxSessionsPerUser(2))
	defer mgr.Close()

	// 3 anonymous sessions (UserID == "", DeviceID == "") must be capped to 2 in "__anonymous__" bucket.
	for _, ps := range []string{"ps1", "ps2", "ps3"} {
		if _, err := mgr.RegisterSession(SessionParams{
			PlaySessionID: ps,
			ItemID:        "item-" + ps,
			MediaSourceID: "ms-" + ps,
		}); err != nil {
			t.Fatalf("RegisterSession %s: %v", ps, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := len(mgr.ActiveSessions()); got != 2 {
		t.Fatalf("expected anonymous sessions capped at 2, got %d", got)
	}

	// Device-scoped anonymous sessions (UserID == "", DeviceID == "dev-a") get their own bucket of 2.
	for _, ps := range []string{"d1", "d2", "d3"} {
		if _, err := mgr.RegisterSession(SessionParams{
			DeviceID:      "dev-a",
			PlaySessionID: ps,
			ItemID:        "item-" + ps,
			MediaSourceID: "ms-" + ps,
		}); err != nil {
			t.Fatalf("RegisterSession %s: %v", ps, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := len(mgr.ActiveSessions()); got != 4 {
		t.Fatalf("expected 2 __anonymous__ + 2 dev-a sessions = 4, got %d", got)
	}
}
