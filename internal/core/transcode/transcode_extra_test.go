package transcode

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/config"
)

func writeMockScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPlaylistAndSegmentHelpersExtra(t *testing.T) {
	// SegmentDurations edge cases
	if d := SegmentDurations(0, 6); d != nil {
		t.Fatalf("expected nil for duration 0, got %v", d)
	}
	if d := SegmentDurations(12, 0); len(d) != 2 {
		t.Fatalf("expected default segDur 6 -> 2 segments, got %v", d)
	}
	// Trailing near-zero remainder (<0.001) should be dropped
	if d := SegmentDurations(12.0001, 6); len(d) != 2 {
		t.Fatalf("expected near-zero trailing segment dropped, got %v", d)
	}

	// BuildMasterPlaylist with defaults and empty query
	m := BuildMasterPlaylist(PlaylistParams{VideoCodec: "hevc", AudioCodec: "ac3"})
	if !strings.Contains(m, "BANDWIDTH=4000000") || !strings.Contains(m, `CODECS="hvc1.1.6.L120.90,ac-3"`) || strings.Contains(m, "?") {
		t.Fatalf("unexpected master playlist:\n%s", m)
	}
	// All hlsCodecsAttr combinations
	if got := hlsCodecsAttr("av1", "eac3"); got != "av01.0.08M.08,ec-3" {
		t.Fatalf("unexpected av1/eac3: %s", got)
	}
	if got := hlsCodecsAttr("h264", "mp3"); got != "avc1.640028,mp4a.40.34" {
		t.Fatalf("unexpected h264/mp3: %s", got)
	}
	if got := hlsCodecsAttr("h264", "opus"); got != "avc1.640028,opus" {
		t.Fatalf("unexpected h264/opus: %s", got)
	}

	// BuildMediaPlaylist fallback when duration=0, playlistID="", segDur=0
	media := BuildMediaPlaylist(PlaylistParams{SegmentContainer: "fmp4"})
	if !strings.Contains(media, "hls1/main/0.mp4") || !strings.Contains(media, "#EXT-X-ENDLIST") {
		t.Fatalf("unexpected fallback media playlist:\n%s", media)
	}
	if normalizeSegmentExt("m4s") != "m4s" || normalizeSegmentExt("mp4") != "mp4" || normalizeSegmentExt("ts") != "ts" {
		t.Fatalf("unexpected normalizeSegmentExt")
	}
	if encodeQuery(url.Values{}) != "" {
		t.Fatalf("expected empty encoded query")
	}

	// ParseSegmentID error cases
	for _, bad := range []string{"", "nodot", ".ts", "12.", "abc.ts", "-1.ts"} {
		if _, _, err := ParseSegmentID(bad); err == nil {
			t.Errorf("expected ParseSegmentID(%q) error", bad)
		}
	}
}

func TestHWAccelAndDetectorExtra(t *testing.T) {
	var nilCaps *HWCapabilities
	if nilCaps.SelectVideoEncoder("h264") != "libx264" || nilCaps.SelectVideoEncoder("hevc") != "libx265" {
		t.Fatalf("expected software fallback on nil caps")
	}
	caps := &HWCapabilities{PreferredH264Encoder: "h264_nvenc", PreferredHEVCEncoder: "hevc_nvenc"}
	if caps.SelectVideoEncoder("h265") != "hevc_nvenc" || caps.SelectVideoEncoder("h264") != "h264_nvenc" {
		t.Fatalf("expected preferred hw encoders")
	}

	// NewHardwareDetector with empty path + Detect caching
	det := NewHardwareDetector("")
	if det.ffmpegPath != "ffmpeg" {
		t.Fatalf("expected default ffmpeg path")
	}
	det.SetCapabilities(caps)
	if got := det.Detect(context.Background()); got != caps {
		t.Fatalf("expected cached capabilities")
	}

	// ProbeHardware with nonexistent binary -> FFmpegAvailable=false
	detMiss := NewHardwareDetector("/nonexistent/ffmpeg-bin-xyz")
	probed := detMiss.Detect(context.Background())
	if probed.FFmpegAvailable || probed.PreferredH264Encoder != "libx264" {
		t.Fatalf("expected unavailable fallback, got %+v", probed)
	}

	// ParseHWAccels with header noise and duplicate lines
	accels := ParseHWAccels("noise line\nHardware acceleration methods:\ncuda\ncuda\n\nvaapi\n")
	if len(accels) != 2 {
		t.Fatalf("expected 2 unique accels, got %v", accels)
	}
	// ParseEncoders with short / invalid lines
	encs := ParseEncoders("short\n X..... badflag\n  V.. only3\n S..... srt SubRip\n")
	if !encs["srt"] || len(encs) != 1 {
		t.Fatalf("unexpected parsed encoders: %v", encs)
	}
	// BuildHWCapabilities with nil encoders and nvdec alias
	nvCaps := BuildHWCapabilities([]string{"nvdec"}, map[string]bool{"h264_nvenc": true, "hevc_nvenc": true}, "linux")
	if nvCaps.PreferredH264Encoder != "h264_nvenc" || nvCaps.PreferredHEVCEncoder != "hevc_nvenc" {
		t.Fatalf("expected nvdec to enable nvenc, got %+v", nvCaps)
	}
	// Windows AMD AMF hardware encoders (d3d11va / dxva2 / amf)
	for _, accel := range []string{"d3d11va", "dxva2", "amf"} {
		amfCaps := BuildHWCapabilities([]string{accel}, map[string]bool{"h264_amf": true, "hevc_amf": true, "libx264": true}, "windows")
		if amfCaps.PreferredHWAccel != "d3d11va" || amfCaps.PreferredH264Encoder != "h264_amf" || amfCaps.PreferredHEVCEncoder != "hevc_amf" {
			t.Fatalf("expected Windows AMF for accel %q, got %+v", accel, amfCaps)
		}
		if amfCaps.SelectVideoEncoder("h264") != "h264_amf" || amfCaps.SelectVideoEncoder("hevc") != "hevc_amf" {
			t.Fatalf("expected SelectVideoEncoder to return AMF encoders")
		}
	}
	// AMF should not be selected on non-Windows platforms
	linuxAMF := BuildHWCapabilities([]string{"d3d11va"}, map[string]bool{"h264_amf": true, "hevc_amf": true}, "linux")
	if linuxAMF.PreferredH264Encoder != "libx264" || linuxAMF.PreferredHEVCEncoder != "libx265" {
		t.Fatalf("expected AMF ignored on linux, got %+v", linuxAMF)
	}
	if emptyCaps := BuildHWCapabilities(nil, nil, "linux"); len(emptyCaps.Codecs) != 2 {
		t.Fatalf("expected 2 software fallback codecs, got %d", len(emptyCaps.Codecs))
	}
}

func TestBuildFFmpegArgsAndSessionKeysExtra(t *testing.T) {
	// SessionKey branches
	if k := SessionKey(SessionParams{PlaySessionID: "ps"}); k != "ps" {
		t.Fatalf("unexpected key: %s", k)
	}
	if k := SessionKey(SessionParams{DeviceID: "d", ItemID: "i"}); k != "d:i" {
		t.Fatalf("unexpected key: %s", k)
	}
	if k := SessionKey(SessionParams{ItemID: "i", MediaSourceID: "m"}); k != "i:m" {
		t.Fatalf("unexpected key: %s", k)
	}
	if sanitizePathSegment("") != "session" || sanitizePathSegment("a/b:c") != "a_b_c" {
		t.Fatalf("unexpected sanitizePathSegment")
	}

	// BuildFFmpegArgs: startSeg<0, MaxWidth only, MaxHeight only, libx264, nvenc, audio copy/mp3, fmp4
	swArgs := strings.Join(BuildFFmpegArgs(SessionParams{
		InputPath:        "/in.mkv",
		VideoCodec:       "h264",
		AudioCodec:       "mp3",
		MaxWidth:         1280,
		SegmentContainer: "mp4",
	}, -5, "/out", nil), " ")
	if !strings.Contains(swArgs, "-preset veryfast") || !strings.Contains(swArgs, "libmp3lame") || !strings.Contains(swArgs, "-hls_segment_type fmp4") {
		t.Fatalf("unexpected swArgs: %s", swArgs)
	}

	nvCaps := &HWCapabilities{PreferredH264Encoder: "h264_nvenc"}
	nvArgs := strings.Join(BuildFFmpegArgs(SessionParams{
		InputPath:  "/in.mkv",
		VideoCodec: "h264",
		AudioCodec: "copy",
		MaxHeight:  720,
	}, 0, "/out", nvCaps), " ")
	if !strings.Contains(nvArgs, "-preset p4") || !strings.Contains(nvArgs, "-c:a copy") {
		t.Fatalf("unexpected nvArgs: %s", nvArgs)
	}
}

func TestExtractorExtraAndFonts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script mocks require unix")
	}
	tmp := t.TempDir()
	cfg := config.Default(tmp)

	// BuildSubtitleExtractArgs + normalizeSubtitleExt
	for _, ext := range []string{"srt", "vtt", "ass", "ssa", "sup", "pgs", "unknown"} {
		args := BuildSubtitleExtractArgs("/in.mkv", 2, ext, "/out."+ext)
		if len(args) == 0 {
			t.Fatalf("empty args for %s", ext)
		}
	}

	// Empty mediaPath errors
	ext := NewExtractor(cfg)
	if _, err := ext.ExtractSubtitle(context.Background(), "", "i", "m", 0, "vtt"); err == nil {
		t.Fatalf("expected error for empty mediaPath")
	}
	if _, err := ext.ExtractFonts(context.Background(), "", "i", "m"); err == nil {
		t.Fatalf("expected error for empty mediaPath in ExtractFonts")
	}

	// Mock ffmpeg that succeeds and writes non-empty output (last arg) + fonts
	okScript := writeMockScript(t, tmp, "ffmpeg_ok.sh", `
for arg in "$@"; do last="$arg"; done
if [ "$1" = "-hide_banner" ] && echo "$@" | grep -q "dump_attachment"; then
  echo "fontdata" > Arial.ttf
  echo "fontdata2" > Noto.otf
  echo "notfont" > readme.txt
  mkdir -p subdir
  exit 0
fi
echo "WEBVTT" > "$last"
`)
	cfg.FFmpegPath = okScript
	extOK := NewExtractor(cfg)

	subPath, err := extOK.ExtractSubtitle(context.Background(), "/fake.mkv", "item1", "ms1", 2, "vtt")
	if err != nil || !fileReady(subPath) {
		t.Fatalf("ExtractSubtitle mock failed: path=%s err=%v", subPath, err)
	}
	// Cache hit
	subPath2, err := extOK.ExtractSubtitle(context.Background(), "/fake.mkv", "item1", "ms1", 2, "vtt")
	if err != nil || subPath2 != subPath {
		t.Fatalf("ExtractSubtitle cache hit failed: %s err=%v", subPath2, err)
	}

	// ExtractFonts + marker cache hit
	fonts, err := extOK.ExtractFonts(context.Background(), "/fake.mkv", "item1", "ms1")
	if err != nil || len(fonts) != 2 {
		t.Fatalf("expected 2 fonts, got %v err=%v", fonts, err)
	}
	fonts2, err := extOK.ExtractFonts(context.Background(), "/fake.mkv", "item1", "ms1")
	if err != nil || len(fonts2) != 2 {
		t.Fatalf("expected 2 cached fonts, got %v err=%v", fonts2, err)
	}

	// Mock ffmpeg that creates an empty file -> error
	emptyScript := writeMockScript(t, tmp, "ffmpeg_empty.sh", `
for arg in "$@"; do last="$arg"; done
: > "$last"
`)
	cfgEmpty := config.Default(t.TempDir())
	cfgEmpty.FFmpegPath = emptyScript
	if _, err := NewExtractor(cfgEmpty).ExtractSubtitle(context.Background(), "/fake.mkv", "i", "m", 2, "srt"); err == nil {
		t.Fatalf("expected error for empty extracted subtitle")
	}

	// Mock ffmpeg that exits non-zero -> error
	failScript := writeMockScript(t, tmp, "ffmpeg_fail.sh", `echo "bad stream" >&2; exit 1`)
	cfgFail := config.Default(t.TempDir())
	cfgFail.FFmpegPath = failScript
	if _, err := NewExtractor(cfgFail).ExtractSubtitle(context.Background(), "/fake.mkv", "i", "m", 2, "srt"); err == nil {
		t.Fatalf("expected error for failing ffmpeg")
	}
}

func TestManagerSeekErrorsAndEdgeCases(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script mocks require unix")
	}
	tmp := t.TempDir()

	// Script that writes seg_<start>.ts after brief pause and stays alive for 2s
	mockFF := writeMockScript(t, tmp, "ffmpeg_seg.sh", `
start=0
out=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "-start_number" ]; then start="$arg"; fi
  if [ "$prev" = "-hls_segment_filename" ]; then out="$arg"; fi
  prev="$arg"
done
seg=$(echo "$out" | sed "s/%d/$start/")
echo "ts-payload-$start" > "$seg"
sleep 2
`)
	cfg := config.Default(tmp)
	cfg.FFmpegPath = mockFF
	mgr := NewManager(cfg, WithCapabilities(BuildHWCapabilities(nil, map[string]bool{"libx264": true}, "linux")))

	// Invalid args
	if _, err := mgr.GetSegment(context.Background(), SessionParams{InputPath: "/a.mkv"}, -1); err == nil {
		t.Fatalf("expected error for negative segIndex")
	}
	if _, err := mgr.GetSegment(context.Background(), SessionParams{}, 0); err == nil {
		t.Fatalf("expected error for empty InputPath")
	}
	if mgr.StopByDevice("", "") != 0 {
		t.Fatalf("expected 0 when both deviceID and playSessionID empty")
	}

	params := SessionParams{
		PlaySessionID: "seek-sess",
		DeviceID:      "dev-seek",
		ItemID:        "item-seek",
		InputPath:     "/fake.mkv",
	}
	// Pre-create stale .tmp file after registering
	snap, err := mgr.RegisterSession(params)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(snap.WorkDir, "stale.tmp"), []byte("tmp"), 0o644)

	// Start at seg 0
	p0, err := mgr.GetSegment(context.Background(), params, 0)
	if err != nil || !fileReady(p0) {
		t.Fatalf("GetSegment(0) failed: %s err=%v", p0, err)
	}
	if len(mgr.ActiveSessions()) != 1 {
		t.Fatalf("expected 1 active session")
	}

	// Seek far ahead to seg 10 -> triggers highestReadySegmentLocked + stopProcessLocked + restart
	p10, err := mgr.GetSegment(context.Background(), params, 10)
	if err != nil || !fileReady(p10) {
		t.Fatalf("GetSegment(10) seek failed: %s err=%v", p10, err)
	}

	// Stop by PlaySessionID
	if n := mgr.StopByDevice("", "seek-sess"); n != 1 {
		t.Fatalf("expected 1 stopped by playSessionID, got %d", n)
	}

	// Manager closed -> errors
	mgr.Close()
	if _, err := mgr.RegisterSession(params); err == nil {
		t.Fatalf("expected error after Close")
	}
	if _, err := mgr.GetSegment(context.Background(), params, 0); err == nil {
		t.Fatalf("expected error after Close")
	}

	// Test ffmpeg exiting without producing segment (exit 0 and exit 1) + ctx canceled
	noopScript := writeMockScript(t, tmp, "ffmpeg_noop.sh", `exit 0`)
	cfgNoop := config.Default(t.TempDir())
	cfgNoop.FFmpegPath = noopScript
	mgrNoop := NewManager(cfgNoop, WithCapabilities(&HWCapabilities{}))
	defer mgrNoop.Close()
	if _, err := mgrNoop.GetSegment(context.Background(), SessionParams{PlaySessionID: "n1", InputPath: "/x.mkv"}, 0); err == nil {
		t.Fatalf("expected error when ffmpeg exits 0 without producing segment")
	}

	errScript := writeMockScript(t, tmp, "ffmpeg_err.sh", `echo "boom" >&2; exit 1`)
	cfgErr := config.Default(t.TempDir())
	cfgErr.FFmpegPath = errScript
	mgrErr := NewManager(cfgErr, WithCapabilities(&HWCapabilities{}))
	defer mgrErr.Close()
	if _, err := mgrErr.GetSegment(context.Background(), SessionParams{PlaySessionID: "e1", InputPath: "/x.mkv"}, 0); err == nil {
		t.Fatalf("expected error when ffmpeg exits 1")
	}

	sleepScript := writeMockScript(t, tmp, "ffmpeg_sleep.sh", `sleep 5`)
	cfgSleep := config.Default(t.TempDir())
	cfgSleep.FFmpegPath = sleepScript
	mgrSleep := NewManager(cfgSleep, WithCapabilities(&HWCapabilities{}))
	defer mgrSleep.Close()
	ctxCancel, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	if _, err := mgrSleep.GetSegment(ctxCancel, SessionParams{PlaySessionID: "c1", InputPath: "/x.mkv"}, 0); err == nil {
		t.Fatalf("expected context deadline error")
	}

	// Nonexistent binary startProcessLocked failure
	cfgBadBin := config.Default(t.TempDir())
	cfgBadBin.FFmpegPath = "/nonexistent/ffmpeg-missing"
	mgrBad := NewManager(cfgBadBin, WithCapabilities(&HWCapabilities{}))
	defer mgrBad.Close()
	if _, err := mgrBad.GetSegment(context.Background(), SessionParams{PlaySessionID: "b1", InputPath: "/x.mkv"}, 0); err == nil {
		t.Fatalf("expected error starting missing ffmpeg binary")
	}
}
