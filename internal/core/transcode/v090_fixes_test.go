package transcode

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/config"
)

func TestGetOrCreateSessionRecreatesOnParamChange(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Default(tmp)
	mgr := NewManager(cfg, WithCapabilities(SoftwareOnlyCapabilities()))
	defer mgr.Close()

	a0, a1 := 1, 2
	s0, s1 := 3, 4

	base := SessionParams{
		PlaySessionID:       "ps-recreate",
		ItemID:              "item-1",
		MediaSourceID:       "ms-1",
		InputPath:           filepath.Join(tmp, "movie.mkv"),
		VideoCodec:          "h264",
		AudioCodec:          "aac",
		VideoBitrate:        4_000_000,
		MaxWidth:            1920,
		MaxHeight:           1080,
		AudioStreamIndex:    &a0,
		SubtitleStreamIndex: &s0,
		SubtitleMethod:      "External",
	}

	sess1, err := mgr.getOrCreateSession(base)
	if err != nil {
		t.Fatalf("initial getOrCreateSession: %v", err)
	}

	// Same params -> reuses exact same session pointer.
	sameCopy := base
	a0Copy, s0Copy := 1, 3
	sameCopy.AudioStreamIndex = &a0Copy
	sameCopy.SubtitleStreamIndex = &s0Copy
	sessSame, err := mgr.getOrCreateSession(sameCopy)
	if err != nil {
		t.Fatalf("same-params getOrCreateSession: %v", err)
	}
	if sessSame != sess1 {
		t.Fatalf("expected identical session pointer when params unchanged")
	}

	mutations := []struct {
		name   string
		mutate func(*SessionParams)
	}{
		{"VideoBitrate", func(p *SessionParams) { p.VideoBitrate = 2_000_000 }},
		{"MaxWidth", func(p *SessionParams) { p.MaxWidth = 1280 }},
		{"MaxHeight", func(p *SessionParams) { p.MaxHeight = 720 }},
		{"AudioStreamIndex", func(p *SessionParams) { p.AudioStreamIndex = &a1 }},
		{"SubtitleStreamIndex", func(p *SessionParams) { p.SubtitleStreamIndex = &s1 }},
		{"SubtitleMethod", func(p *SessionParams) { p.SubtitleMethod = "Encode" }},
	}

	prev := sess1
	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			next := prev.params
			tc.mutate(&next)
			got, err := mgr.getOrCreateSession(next)
			if err != nil {
				t.Fatalf("getOrCreateSession(%s): %v", tc.name, err)
			}
			if got == prev {
				t.Fatalf("expected new session when %s changed", tc.name)
			}
			prev = got
		})
	}
}

func TestDirectStreamBurnInHWFallbackToSoftware(t *testing.T) {
	tmp := t.TempDir()
	fakeFFmpeg := filepath.Join(tmp, "fake-ffmpeg.sh")
	logFile := filepath.Join(tmp, "ffmpeg.log")

	// Fake ffmpeg fails when invoked with a hardware encoder (h264_videotoolbox)
	// and writes seg_0.ts when invoked with software libx264.
	script := fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
for arg in "$@"; do
  if [ "$arg" = "h264_videotoolbox" ]; then
    echo "hw encoder unavailable" >&2
    exit 1
  fi
done
# Find -hls_segment_filename pattern and write seg_0.ts
prev=""
for arg in "$@"; do
  if [ "$prev" = "-hls_segment_filename" ]; then
    out=$(echo "$arg" | sed 's/%%d/0/')
    printf "FAKE_TS_DATA" > "$out"
    exit 0
  fi
  prev="$arg"
done
exit 1
`, logFile)
	if err := os.WriteFile(fakeFFmpeg, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default(tmp)
	cfg.FFmpegPath = fakeFFmpeg
	caps := BuildHWCapabilities([]string{"videotoolbox"}, map[string]bool{
		"h264_videotoolbox": true,
		"libx264":           true,
	}, "darwin")
	mgr := NewManager(cfg, WithCapabilities(caps))
	defer mgr.Close()

	subIdx := 2
	segPath, err := mgr.GetSegment(context.Background(), SessionParams{
		PlaySessionID:       "ps-burnin-hw",
		ItemID:              "item-burnin",
		MediaSourceID:       "ms-burnin",
		InputPath:           filepath.Join(tmp, "movie.mkv"),
		VideoCodec:          "copy", // DirectStream video copy, overridden by SubtitleMethod=Encode
		AudioCodec:          "aac",
		SubtitleStreamIndex: &subIdx,
		SubtitleMethod:      "Encode",
		SubtitleCodec:       "ass",
	}, 0)
	if err != nil {
		t.Fatalf("expected HW->SW fallback to succeed for DirectStream+burn-in, got: %v", err)
	}
	data, err := os.ReadFile(segPath)
	if err != nil || string(data) != "FAKE_TS_DATA" {
		t.Fatalf("unexpected segment data: %q err=%v", string(data), err)
	}

	invocationsRaw, _ := os.ReadFile(logFile)
	invocations := strings.TrimSpace(string(invocationsRaw))
	if !strings.Contains(invocations, "h264_videotoolbox") || !strings.Contains(invocations, "libx264") {
		t.Fatalf("expected both HW attempt and SW fallback in ffmpeg log, got:\n%s", invocations)
	}
}

func TestExtractChapterThumbnailAndSingleflight(t *testing.T) {
	tmp := t.TempDir()
	fakeFFmpeg := filepath.Join(tmp, "fake-ffmpeg.sh")
	countFile := filepath.Join(tmp, "calls.log")

	script := fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
sleep 0.1
# Last argument is the output tmp jpg path
for last; do true; done
printf '\xff\xd8\xff\xe0FAKEJPEG' > "$last"
exit 0
`, countFile)
	if err := os.WriteFile(fakeFFmpeg, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default(tmp)
	cfg.FFmpegPath = fakeFFmpeg
	ext := NewExtractor(cfg)

	const concurrency = 4
	var wg sync.WaitGroup
	paths := make([]string, concurrency)
	errs := make([]error, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			paths[idx], errs[idx] = ext.ExtractChapterThumbnail(ctx, filepath.Join(tmp, "movie.mkv"), "item123", 2, 125_000_000_0)
		}(i)
	}
	wg.Wait()

	wantPath := filepath.Join(cfg.ImageCacheDir(), "chapters", "item123", "chapter_2.jpg")
	for i := 0; i < concurrency; i++ {
		if errs[i] != nil {
			t.Fatalf("goroutine %d error: %v", i, errs[i])
		}
		if paths[i] != wantPath {
			t.Fatalf("goroutine %d path=%q, want %q", i, paths[i], wantPath)
		}
	}

	rawLog, err := os.ReadFile(countFile)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(rawLog)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected singleflight to invoke ffmpeg once, got %d calls:\n%s", len(lines), string(rawLog))
	}
	for _, want := range []string{"-ss 125.000", "-frames:v 1", "-q:v 4", "scale='min(640,iw)':-2"} {
		if !strings.Contains(lines[0], want) {
			t.Fatalf("expected %q in ffmpeg args %q", want, lines[0])
		}
	}
}
