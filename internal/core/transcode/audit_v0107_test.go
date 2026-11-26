package transcode

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
)

func TestAuditV0107_C1_ExtractFontsSafeTwoStep(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script mocks require unix")
	}
	tmp := t.TempDir()
	ffmpegLog := filepath.Join(tmp, "ffmpeg.log")
	ffprobeLog := filepath.Join(tmp, "ffprobe.log")

	fakeProbe := filepath.Join(tmp, "ffprobe.sh")
	probeScript := fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
cat <<'EOF'
{"streams":[
  {"index":2,"tags":{"filename":"../../etc/cron.d/pwn.ttf"}},
  {"index":3,"tags":{"filename":"/abs/path/evil.otf"}},
  {"index":4,"tags":{"filename":".hidden.ttf"}},
  {"index":5,"tags":{"filename":"notes.txt"}},
  {"index":6,"tags":{"FILENAME":"Main.ttf"}},
  {"index":7,"tags":{"filename":"main.ttf"}},
  {"index":8,"tags":{"filename":"Main_7.ttf"}}
]}
EOF
`, ffprobeLog)
	if err := os.WriteFile(fakeProbe, []byte(probeScript), 0o755); err != nil {
		t.Fatal(err)
	}

	fakeFFmpeg := filepath.Join(tmp, "ffmpeg.sh")
	ffmpegScript := fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
prev=""
for arg in "$@"; do
  case "$prev" in
    -dump_attachment:*) printf "FONT" > "$arg" ;;
  esac
  prev="$arg"
done
exit 0
`, ffmpegLog)
	if err := os.WriteFile(fakeFFmpeg, []byte(ffmpegScript), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default(tmp)
	cfg.FFmpegPath = fakeFFmpeg
	cfg.FFprobePath = fakeProbe
	ext := NewExtractor(cfg)

	fonts, err := ext.ExtractFonts(context.Background(), filepath.Join(tmp, "movie.mkv"), "item-c1", "ms-c1")
	if err != nil {
		t.Fatalf("ExtractFonts: %v", err)
	}
	if len(fonts) != 3 {
		t.Fatalf("expected 3 deduplicated safe fonts, got %d: %v", len(fonts), fonts)
	}

	rawFFmpegLog, err := os.ReadFile(ffmpegLog)
	if err != nil {
		t.Fatalf("read ffmpeg log: %v", err)
	}
	ffLogStr := string(rawFFmpegLog)
	if strings.Contains(ffLogStr, "-dump_attachment:t") {
		t.Fatalf("must NEVER pass -dump_attachment:t: %s", ffLogStr)
	}
	if strings.Contains(ffLogStr, "..") || strings.Contains(ffLogStr, "evil.otf") || strings.Contains(ffLogStr, "notes.txt") {
		t.Fatalf("unsafe attachment leaked into ffmpeg args: %s", ffLogStr)
	}
	if !strings.Contains(ffLogStr, "-dump_attachment:6") || !strings.Contains(ffLogStr, "-dump_attachment:7") || !strings.Contains(ffLogStr, "-dump_attachment:8") {
		t.Fatalf("expected explicit stream-indexed -dump_attachment args, got: %s", ffLogStr)
	}

	// Missing ffprobe returns an error and does NOT write .extracted marker (H-15).
	cfgNoProbe := config.Default(t.TempDir())
	cfgNoProbe.FFmpegPath = fakeFFmpeg
	cfgNoProbe.FFprobePath = filepath.Join(tmp, "does-not-exist-ffprobe")
	extNoProbe := NewExtractor(cfgNoProbe)
	emptyFonts, err := extNoProbe.ExtractFonts(context.Background(), filepath.Join(tmp, "movie.mkv"), "item-noprobe", "ms-noprobe")
	if err == nil {
		t.Fatalf("expected error without ffprobe, got fonts=%v", emptyFonts)
	}
	marker := filepath.Join(cfgNoProbe.SubtitleCacheDir(), "fonts", "item-noprobe_ms-noprobe", ".extracted")
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("expected .extracted marker NOT written on probe failure: %v", statErr)
	}

	// Remote HTTP media uses strict remoteProtocolWhitelist (no file,pipe) and -follow_redirects 0
	if strings.Contains(remoteProtocolWhitelist, "file") || strings.Contains(remoteProtocolWhitelist, "pipe") {
		t.Fatalf("remoteProtocolWhitelist must not include file or pipe: %q", remoteProtocolWhitelist)
	}
	if _, err := ext.ExtractFonts(context.Background(), "https://93.184.216.34/anime.mkv", "item-remote", "ms-remote"); err != nil {
		t.Fatalf("remote ExtractFonts: %v", err)
	}
	rawProbeLog, _ := os.ReadFile(ffprobeLog)
	if !strings.Contains(string(rawProbeLog), "-protocol_whitelist http,https,tcp,tls,crypto -follow_redirects 0") {
		t.Fatalf("expected strict remote whitelist and -follow_redirects 0 in ffprobe args: %s", string(rawProbeLog))
	}
}

func TestAuditV0107_H8_BIFStreamingAndLimits(t *testing.T) {
	tmp := t.TempDir()
	jpg := createTinyJPEG(t)
	f1 := filepath.Join(tmp, "frame_000001.jpg")
	f2 := filepath.Join(tmp, "frame_000002.jpg")
	link := filepath.Join(tmp, "frame_000003.jpg")
	empty := filepath.Join(tmp, "frame_000004.jpg")
	_ = os.WriteFile(f1, jpg, 0o600)
	_ = os.WriteFile(f2, jpg, 0o600)
	_ = os.Symlink(f1, link)
	_ = os.WriteFile(empty, nil, 0o600)

	outBIF := filepath.Join(tmp, "streamed.bif")
	if err := writeBIFFromFiles(outBIF, 10000, []string{f1, f2, link, empty}, maxBIFTotalBytes); err != nil {
		t.Fatalf("writeBIFFromFiles: %v", err)
	}
	meta, err := ReadBIFMetadata(outBIF)
	if err != nil || meta.ImageCount != 2 {
		t.Fatalf("expected 2 valid frames (skipping symlink and empty), got meta=%+v err=%v", meta, err)
	}

	// Exceeding total byte limit fails cleanly without leaving output file
	tooBigBIF := filepath.Join(tmp, "toobig.bif")
	if err := writeBIFFromFiles(tooBigBIF, 10000, []string{f1, f2}, int64(len(jpg))); err == nil {
		t.Fatal("expected error when total JPEG bytes exceed limit")
	}
	if _, err := os.Stat(tooBigBIF); !os.IsNotExist(err) {
		t.Fatalf("expected no output file on byte limit error")
	}

	// Verify GenerateBIF passes -frames:v 10000
	if runtime.GOOS != "windows" {
		callLog := filepath.Join(tmp, "bif_args.log")
		sampleJPG := filepath.Join(tmp, "sample.jpg")
		_ = os.WriteFile(sampleJPG, jpg, 0o644)
		fakeFF := filepath.Join(tmp, "fake-ff.sh")
		script := fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
for last; do true; done
cp %q "$(dirname "$last")/frame_000001.jpg"
exit 0
`, callLog, sampleJPG)
		_ = os.WriteFile(fakeFF, []byte(script), 0o755)
		cfg := config.Default(tmp)
		cfg.FFmpegPath = fakeFF
		ext := NewExtractor(cfg)
		media := filepath.Join(tmp, "m.mkv")
		_ = os.WriteFile(media, []byte("x"), 0o644)

		// Also verify WithoutCancel: already-canceled ctx still completes inside singleflight
		canceledCtx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := ext.GenerateBIF(canceledCtx, media, "item-bif-cap", 320, 10); err != nil {
			t.Fatalf("GenerateBIF with canceled ctx failed: %v", err)
		}
		raw, _ := os.ReadFile(callLog)
		if !strings.Contains(string(raw), "-frames:v 10000") {
			t.Fatalf("expected -frames:v 10000 in BIF ffmpeg args, got: %s", string(raw))
		}
	}
}

func TestAuditV0107_M10_UniqueWorkDirAndGetCapsUnlocked(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Default(tmp)
	mgr := NewManager(cfg, WithCapabilities(SoftwareOnlyCapabilities()))
	defer mgr.Close()

	s1, err := mgr.RegisterSession(SessionParams{
		UserID:        "u1",
		PlaySessionID: "ps1",
		ItemID:        "item1",
		InputPath:     filepath.Join(tmp, "a.mkv"),
		VideoBitrate:  2_000_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Replace same key with different params -> must allocate a distinct workDir
	s2, err := mgr.RegisterSession(SessionParams{
		UserID:        "u1",
		PlaySessionID: "ps1",
		ItemID:        "item1",
		InputPath:     filepath.Join(tmp, "a.mkv"),
		VideoBitrate:  4_000_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if s1.WorkDir == s2.WorkDir {
		t.Fatalf("expected distinct workDir per session instance, got identical %q", s1.WorkDir)
	}

	// Verify getCaps is invoked without holding s.mu (calling s.snapshot() inside getCaps would deadlock if locked)
	sess := &transcodeSession{
		key:        "k-unlock",
		ffmpegPath: filepath.Join(tmp, "nonexistent-ffmpeg"),
		params: SessionParams{
			InputPath:        filepath.Join(tmp, "a.mkv"),
			SegmentContainer: "ts",
			SegmentDuration:  6,
		},
		workDir: filepath.Join(tmp, "work-unlock"),
	}
	called := false
	_, _ = sess.getSegment(context.Background(), 0, func() *HWCapabilities {
		called = true
		_ = sess.snapshot() // acquires s.mu; would deadlock if getCaps held s.mu
		return SoftwareOnlyCapabilities()
	})
	if !called {
		t.Fatal("expected getCaps to be called")
	}
}

func TestAuditV0107_M11_HardwareDetectorNoCancelAndNoNegativeCache(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script mocks require unix")
	}
	tmp := t.TempDir()
	binPath := filepath.Join(tmp, "toggle-ffmpeg.sh")

	det := NewHardwareDetector(binPath)
	// First call: binary does not exist yet -> FFmpegAvailable=false and NOT cached
	c1 := det.Detect(context.Background())
	if c1.FFmpegAvailable {
		t.Fatal("expected FFmpegAvailable=false when binary missing")
	}

	// Now create working mock ffmpeg
	script := `#!/bin/sh
if [ "$2" = "-hwaccels" ]; then
  printf "Hardware acceleration methods:\n"
else
  printf " V..... libx264 H.264\n"
fi
`
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	// Call with an already-canceled ctx: WithoutCancel ensures probe succeeds and caches!
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	c2 := det.Detect(canceledCtx)
	if !c2.FFmpegAvailable {
		t.Fatal("expected Detect to recover and succeed even with canceled caller ctx")
	}
	c3 := det.Detect(context.Background())
	if c2 != c3 {
		t.Fatal("expected successful HardwareDetector probe to be cached")
	}
}

func TestAuditV0107_Low_SegmentDurationsCapAndLimitedBuffer(t *testing.T) {
	if d := SegmentDurations(math.NaN(), 6); d != nil {
		t.Fatalf("expected nil for NaN duration, got len=%d", len(d))
	}
	if d := SegmentDurations(math.Inf(1), 6); d != nil {
		t.Fatalf("expected nil for +Inf duration, got len=%d", len(d))
	}
	d := SegmentDurations(1e12, 6)
	if len(d) != maxPlaylistSegments {
		t.Fatalf("expected capped %d segments, got %d", maxPlaylistSegments, len(d))
	}
	if d[len(d)-1] != 6 {
		t.Fatalf("expected capped trailing segment duration 6, got %v", d[len(d)-1])
	}

	lb := &limitedBuffer{max: 4}
	_, _ = lb.Write([]byte("ab"))
	_, _ = lb.Write([]byte("cdef"))
	if !lb.truncated || lb.buf.String() != "abcd" {
		t.Fatalf("unexpected limitedBuffer state: truncated=%v data=%q", lb.truncated, lb.buf.String())
	}
	lbZero := &limitedBuffer{max: 0}
	_, _ = lbZero.Write([]byte("x"))
	if !lbZero.truncated {
		t.Fatal("expected zero-max limitedBuffer to mark truncated")
	}

	// Cover safeBurnSubExt / safeMediaExt / deduplicateAttachmentName edge cases
	if got := safeBurnSubExt("/a/b.ass", "srt"); got != ".ass" {
		t.Fatalf("safeBurnSubExt(.ass) = %q", got)
	}
	if got := safeBurnSubExt("/a/b.unknown", "ssa"); got != ".ssa" {
		t.Fatalf("safeBurnSubExt(ssa codec) = %q", got)
	}
	if got := safeBurnSubExt("/a/b.unknown", "webvtt"); got != ".vtt" {
		t.Fatalf("safeBurnSubExt(webvtt codec) = %q", got)
	}
	if got := safeBurnSubExt("/a/b.unknown", "ass"); got != ".ass" {
		t.Fatalf("safeBurnSubExt(ass codec) = %q", got)
	}
	if got := safeBurnSubExt("/a/b.unknown", "other"); got != ".srt" {
		t.Fatalf("safeBurnSubExt(default) = %q", got)
	}
	if got := safeMediaExt("/a/video.mp4"); got != ".mp4" {
		t.Fatalf("safeMediaExt(.mp4) = %q", got)
	}
	if got := safeMediaExt("/a/video.weird-ext!"); got != ".mkv" {
		t.Fatalf("safeMediaExt(invalid) = %q", got)
	}
	used := map[string]struct{}{"font.ttf": {}, "font_2.ttf": {}}
	if got := deduplicateAttachmentName("font.ttf", 2, used); got != "font_2_1.ttf" {
		t.Fatalf("deduplicateAttachmentName collision fallback got %q", got)
	}
	if err := copyFileAtomic(filepath.Join(t.TempDir(), "missing"), filepath.Join(t.TempDir(), "dst")); err == nil {
		t.Fatal("expected copyFileAtomic error for missing source")
	}
}

func TestAuditV0107_H2_StopByDeviceForUserUnownedAndInitSegmentUnlocked(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Default(tmp)
	mgr := NewManager(cfg, WithCapabilities(SoftwareOnlyCapabilities()))
	defer mgr.Close()

	// Register an unowned session (UserID == "") on device "dev-shared"
	_, err := mgr.RegisterSession(SessionParams{
		UserID:        "",
		DeviceID:      "dev-shared",
		PlaySessionID: "ps-unowned",
		ItemID:        "item-unowned",
		InputPath:     filepath.Join(tmp, "a.mkv"),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Non-admin user MUST NOT be able to stop unowned session
	mgr.StopByDeviceForUser("user-normal", "ps-unowned", "dev-shared", false)
	if len(mgr.ActiveSessions()) != 1 {
		t.Fatalf("non-admin must not stop unowned session, remaining=%d", len(mgr.ActiveSessions()))
	}
	// Admin CAN stop unowned session
	mgr.StopByDeviceForUser("admin-1", "ps-unowned", "dev-shared", true)
	if len(mgr.ActiveSessions()) != 0 {
		t.Fatalf("admin should stop unowned session, remaining=%d", len(mgr.ActiveSessions()))
	}

	// Exercise getInitSegment calling getCaps without holding s.mu
	sess := &transcodeSession{
		key:        "k-init-unlock",
		ffmpegPath: filepath.Join(tmp, "nonexistent-ffmpeg"),
		params: SessionParams{
			InputPath:        filepath.Join(tmp, "a.mkv"),
			SegmentContainer: "mp4",
			SegmentDuration:  6,
		},
		workDir: filepath.Join(tmp, "work-init-unlock"),
	}
	initCalled := false
	_, _ = sess.getInitSegment(context.Background(), func() *HWCapabilities {
		initCalled = true
		_ = sess.snapshot()
		return SoftwareOnlyCapabilities()
	})
	if !initCalled {
		t.Fatal("expected getCaps to be called from getInitSegment")
	}

	// Cover writeBIFFromFiles defaults & empty frame error
	if err := writeBIFFromFiles(filepath.Join(tmp, "empty.bif"), 0, nil, 0); err == nil {
		t.Fatal("expected error for empty frames list")
	}
	// Already-truncated limitedBuffer discards further writes cleanly
	lb := &limitedBuffer{max: 2}
	_, _ = lb.Write([]byte("abcd"))
	n, err := lb.Write([]byte("efg"))
	if err != nil || n != 3 || lb.buf.String() != "ab" {
		t.Fatalf("unexpected truncated write: n=%d err=%v buf=%q", n, err, lb.buf.String())
	}
}
