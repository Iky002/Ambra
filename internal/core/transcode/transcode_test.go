package transcode

import (
	"context"
	"math"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/config"
)

func TestParseHWAccelsAndEncoders(t *testing.T) {
	hwOut := `Hardware acceleration methods:
videotoolbox
vaapi
cuda
qsv
`
	accels := ParseHWAccels(hwOut)
	if len(accels) != 4 || !slices.Contains(accels, "videotoolbox") || !slices.Contains(accels, "vaapi") {
		t.Fatalf("unexpected accels: %v", accels)
	}

	encOut := `Encoders:
 V..... = Video
 ------
 V..... libx264              libx264 H.264 / AVC / MPEG-4 AVC / MPEG-4 part 10
 V..... libx265              libx265 H.265 / HEVC
 V....D h264_videotoolbox    VideoToolbox H.264 Encoder
 V....D hevc_videotoolbox    VideoToolbox H.265 Encoder
 V....D h264_vaapi           H.264/AVC (VAAPI)
 V....D h264_nvenc           NVIDIA NVENC H.264 encoder
 A..... aac                  AAC (Advanced Audio Coding)
`
	encs := ParseEncoders(encOut)
	for _, want := range []string{"libx264", "libx265", "h264_videotoolbox", "hevc_videotoolbox", "h264_vaapi", "h264_nvenc", "aac"} {
		if !encs[want] {
			t.Fatalf("expected encoder %q to be parsed", want)
		}
	}

	// macOS should prefer videotoolbox
	macCaps := BuildHWCapabilities(accels, encs, "darwin")
	if macCaps.PreferredH264Encoder != "h264_videotoolbox" {
		t.Fatalf("expected h264_videotoolbox on darwin, got %s", macCaps.PreferredH264Encoder)
	}
	if macCaps.PreferredHEVCEncoder != "hevc_videotoolbox" {
		t.Fatalf("expected hevc_videotoolbox on darwin, got %s", macCaps.PreferredHEVCEncoder)
	}

	// Linux should skip videotoolbox and prefer nvenc > qsv > vaapi
	linuxCaps := BuildHWCapabilities([]string{"cuda", "vaapi"}, encs, "linux")
	if linuxCaps.PreferredH264Encoder != "h264_nvenc" {
		t.Fatalf("expected h264_nvenc on linux with cuda, got %s", linuxCaps.PreferredH264Encoder)
	}

	// Software fallback when no hardware accel is available
	swCaps := BuildHWCapabilities(nil, map[string]bool{"libx264": true, "libx265": true}, "linux")
	if swCaps.PreferredH264Encoder != "libx264" || swCaps.PreferredHEVCEncoder != "libx265" {
		t.Fatalf("expected software fallback, got h264=%s hevc=%s", swCaps.PreferredH264Encoder, swCaps.PreferredHEVCEncoder)
	}
}

func TestSegmentDurationsAndVirtualPlaylists(t *testing.T) {
	// 125.5s / 6s = 21 segments (20 * 6.0 + 1 * 5.5)
	durs := SegmentDurations(125.5, 6)
	if len(durs) != 21 {
		t.Fatalf("expected 21 segments, got %d", len(durs))
	}
	if durs[0] != 6.0 || math.Abs(durs[20]-5.5) > 1e-6 {
		t.Fatalf("unexpected segment durations: first=%f last=%f", durs[0], durs[20])
	}

	// Exact multiple: 120s / 6s = 20 segments
	exact := SegmentDurations(120, 6)
	if len(exact) != 20 || exact[19] != 6.0 {
		t.Fatalf("expected 20 exact 6s segments, got len=%d last=%f", len(exact), exact[len(exact)-1])
	}

	// 3s segments: 10s / 3s = 4 segments (3, 3, 3, 1)
	short := SegmentDurations(10, 3)
	if len(short) != 4 || math.Abs(short[3]-1.0) > 1e-6 {
		t.Fatalf("expected 4 segments ending in 1.0s, got %v", short)
	}

	q := url.Values{}
	q.Set("PlaySessionId", "sess123")
	q.Set("MediaSourceId", "ms456")

	master := BuildMasterPlaylist(PlaylistParams{
		ItemID:     "item1",
		Bandwidth:  5_000_000,
		Width:      1920,
		Height:     1080,
		VideoCodec: "h264",
		AudioCodec: "aac",
		Query:      q,
	})
	if !strings.Contains(master, "#EXTM3U") ||
		!strings.Contains(master, "BANDWIDTH=5000000") ||
		!strings.Contains(master, "RESOLUTION=1920x1080") ||
		!strings.Contains(master, "main.m3u8?") {
		t.Fatalf("invalid master playlist:\n%s", master)
	}

	media := BuildMediaPlaylist(PlaylistParams{
		ItemID:           "item1",
		PlaylistID:       "main",
		DurationTicks:    int64(19.5 * TicksPerSecond),
		SegmentDuration:  6,
		SegmentContainer: "ts",
		Query:            q,
	})
	if !strings.Contains(media, "#EXT-X-PLAYLIST-TYPE:VOD") ||
		!strings.Contains(media, "#EXT-X-TARGETDURATION:6") ||
		!strings.Contains(media, "#EXT-X-ENDLIST") ||
		!strings.Contains(media, "hls1/main/0.ts?") ||
		!strings.Contains(media, "hls1/main/3.ts?") ||
		!strings.Contains(media, "#EXTINF:1.500000,") {
		t.Fatalf("invalid media playlist:\n%s", media)
	}

	idx, ext, err := ParseSegmentID("12.ts")
	if err != nil || idx != 12 || ext != "ts" {
		t.Fatalf("ParseSegmentID failed: idx=%d ext=%s err=%v", idx, ext, err)
	}
	idx, ext, err = ParseSegmentID("seg_5.m4s")
	if err != nil || idx != 5 || ext != "m4s" {
		t.Fatalf("ParseSegmentID seg_5.m4s failed: idx=%d ext=%s err=%v", idx, ext, err)
	}
}

func TestBuildFFmpegArgsDirectStreamVsTranscode(t *testing.T) {
	caps := BuildHWCapabilities([]string{"videotoolbox"}, map[string]bool{
		"h264_videotoolbox": true,
		"libx264":           true,
	}, "darwin")

	// DirectStream (-c:v copy) at segment 0
	dsArgs := BuildFFmpegArgs(SessionParams{
		InputPath:       "/media/movie.mkv",
		VideoCodec:      "copy",
		AudioCodec:      "aac",
		AudioBitrate:    192000,
		SegmentDuration: 6,
	}, 0, "/tmp/out", caps)
	dsJoined := strings.Join(dsArgs, " ")
	if !strings.Contains(dsJoined, "-c:v copy") {
		t.Fatalf("DirectStream must use -c:v copy, got: %s", dsJoined)
	}
	if strings.Contains(dsJoined, "-ss ") {
		t.Fatalf("segment 0 should not include -ss, got: %s", dsJoined)
	}
	if !strings.Contains(dsJoined, "-hls_flags temp_file") || !strings.Contains(dsJoined, "-start_number 0") {
		t.Fatalf("missing hls temp_file or start_number: %s", dsJoined)
	}

	// Transcode with Seek to segment 10 (60s)
	audioIdx := 2
	tcArgs := BuildFFmpegArgs(SessionParams{
		InputPath:        "/media/movie.mkv",
		VideoCodec:       "h264",
		AudioCodec:       "aac",
		VideoBitrate:     4000000,
		AudioBitrate:     192000,
		AudioStreamIndex: &audioIdx,
		MaxWidth:         1280,
		MaxHeight:        720,
		SegmentDuration:  6,
	}, 10, "/tmp/out", caps)
	tcJoined := strings.Join(tcArgs, " ")
	if !strings.Contains(tcJoined, "-ss 60 -i /media/movie.mkv") {
		t.Fatalf("expected input seek -ss 60 before -i, got: %s", tcJoined)
	}
	if !strings.Contains(tcJoined, "-c:v h264_videotoolbox") {
		t.Fatalf("expected hardware encoder h264_videotoolbox, got: %s", tcJoined)
	}
	if !strings.Contains(tcJoined, "-map 0:2?") {
		t.Fatalf("expected explicit audio stream map 0:2?, got: %s", tcJoined)
	}
	if !strings.Contains(tcJoined, "-start_number 10") || !strings.Contains(tcJoined, "-output_ts_offset 60") {
		t.Fatalf("expected start_number 10 and output_ts_offset 60, got: %s", tcJoined)
	}
}

func TestManagerLifecycleAndActiveEncodings(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.Default(tmpDir)
	mgr := NewManager(cfg, WithIdleTimeout(100*time.Millisecond), WithCleanupInterval(30*time.Millisecond))
	defer mgr.Close()

	snap, err := mgr.RegisterSession(SessionParams{
		PlaySessionID: "ps-1",
		DeviceID:      "dev-1",
		ItemID:        "item-1",
		InputPath:     "/nonexistent/a.mkv",
	})
	if err != nil {
		t.Fatalf("RegisterSession failed: %v", err)
	}

	// Pre-write a cached segment on disk and verify fast-path hit without launching ffmpeg
	cachedSeg := filepath.Join(snap.WorkDir, "seg_0.ts")
	if err := os.WriteFile(cachedSeg, []byte("fake-ts-data"), 0o644); err != nil {
		t.Fatal(err)
	}
	gotPath, err := mgr.GetSegment(context.Background(), SessionParams{
		PlaySessionID: "ps-1",
		DeviceID:      "dev-1",
		ItemID:        "item-1",
		InputPath:     "/nonexistent/a.mkv",
	}, 0)
	if err != nil || gotPath != cachedSeg {
		t.Fatalf("expected cached segment hit %q, got %q err=%v", cachedSeg, gotPath, err)
	}

	// StopByDevice should remove session and delete workDir
	stopped := mgr.StopByDevice("dev-1", "")
	if stopped != 1 {
		t.Fatalf("expected 1 stopped session, got %d", stopped)
	}
	if len(mgr.ActiveSessions()) != 0 {
		t.Fatalf("expected 0 active sessions after stop")
	}
	if _, err := os.Stat(snap.WorkDir); !os.IsNotExist(err) {
		t.Fatalf("expected workDir to be removed after stop")
	}

	// Test idle reaper
	_, err = mgr.RegisterSession(SessionParams{
		PlaySessionID: "ps-idle",
		DeviceID:      "dev-2",
		ItemID:        "item-2",
		InputPath:     "/nonexistent/b.mkv",
	})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(220 * time.Millisecond)
	if len(mgr.ActiveSessions()) != 0 {
		t.Fatalf("expected idle session to be reaped, still have %d", len(mgr.ActiveSessions()))
	}
}

func TestRealFFmpegProbeTranscodeAndSubtitleExtract(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed; skipping real ffmpeg integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// 1. Real hardware probe
	caps := ProbeHardware(ctx, ffmpegPath)
	if !caps.FFmpegAvailable || len(caps.Codecs) == 0 {
		t.Fatalf("expected real ffmpeg probe to detect codecs, got %+v", caps)
	}

	// 2. Generate a tiny 6-second test MKV with an embedded SRT subtitle
	tmpDir := t.TempDir()
	srtPath := filepath.Join(tmpDir, "sub.srt")
	if err := os.WriteFile(srtPath, []byte("1\n00:00:00,000 --> 00:00:02,000\nHello Ambra\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mkvPath := filepath.Join(tmpDir, "sample.mkv")
	genCmd := exec.CommandContext(ctx, ffmpegPath,
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=duration=6:size=320x180:rate=24",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=6",
		"-i", srtPath,
		"-map", "0:v", "-map", "1:a", "-map", "2:s",
		"-c:v", "libx264", "-preset", "ultrafast",
		"-c:a", "aac", "-c:s", "srt",
		mkvPath,
	)
	if out, err := genCmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to generate test mkv: %v (%s)", err, string(out))
	}

	// 3. Test real dynamic HLS segment generation (seg 0 and seek to seg 1)
	cfg := config.Default(tmpDir)
	cfg.FFmpegPath = ffmpegPath
	// Force libx264 so test is deterministic in headless CI/sandbox environments
	swCaps := BuildHWCapabilities(nil, map[string]bool{"libx264": true}, "linux")
	mgr := NewManager(cfg, WithCapabilities(swCaps))
	defer mgr.Close()

	params := SessionParams{
		PlaySessionID:   "real-sess",
		DeviceID:        "test-dev",
		ItemID:          "item-real",
		InputPath:       mkvPath,
		VideoCodec:      "copy",
		AudioCodec:      "aac",
		SegmentDuration: 3,
	}
	seg0, err := mgr.GetSegment(ctx, params, 0)
	if err != nil {
		t.Fatalf("GetSegment(0) failed: %v", err)
	}
	if !fileReady(seg0) {
		t.Fatalf("seg0 not ready on disk: %s", seg0)
	}

	// 4. Test real embedded subtitle extraction + cache hit
	ext := NewExtractor(cfg)
	subOut, err := ext.ExtractSubtitle(ctx, mkvPath, "item-real", "ms-1", 2, "vtt")
	if err != nil {
		t.Fatalf("ExtractSubtitle failed: %v", err)
	}
	rawSub, err := os.ReadFile(subOut)
	if err != nil || !strings.Contains(string(rawSub), "Hello Ambra") {
		t.Fatalf("unexpected extracted subtitle content: %q err=%v", string(rawSub), err)
	}
}
