package transcode

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/config"
)

func TestSafeRingBufferAndFilterEscape(t *testing.T) {
	// max <= 0 discards silently
	zero := newSafeRingBuffer(0)
	if n, err := zero.Write([]byte("abc")); err != nil || n != 3 || zero.String() != "" {
		t.Fatalf("zero buffer unexpected: n=%d err=%v str=%q", n, err, zero.String())
	}

	// Sliding window and single-write >= max truncation
	rb := newSafeRingBuffer(6)
	_, _ = rb.Write([]byte("1234"))
	_, _ = rb.Write([]byte("5678"))
	if got := rb.String(); got != "345678" {
		t.Fatalf("sliding window got %q, want %q", got, "345678")
	}
	_, _ = rb.Write([]byte("abcdefghij"))
	if got := rb.String(); got != "efghij" {
		t.Fatalf("overwrite >= max got %q, want %q", got, "efghij")
	}

	// escapeFFmpegFilterPath rejects control chars
	if _, err := escapeFFmpegFilterPath("/tmp/bad\npath.ass"); err == nil {
		t.Fatalf("expected error for newline in filter path")
	}
	if _, err := escapeFFmpegFilterPath("/tmp/bad\x00path.ass"); err == nil {
		t.Fatalf("expected error for NUL in filter path")
	}

	// intPtrEqual & isHardwareEncoder edge cases
	a, b := 1, 1
	if !intPtrEqual(nil, nil) || intPtrEqual(&a, nil) || intPtrEqual(nil, &b) || !intPtrEqual(&a, &b) {
		t.Fatalf("intPtrEqual unexpected result")
	}
	if isHardwareEncoder("") || isHardwareEncoder("copy") || isHardwareEncoder("libx264") {
		t.Fatalf("expected software/empty/copy encoders not to be hardware")
	}
	if !isHardwareEncoder("h264_vaapi") || !isHardwareEncoder("h264_videotoolbox") {
		t.Fatalf("expected hardware encoders to return true")
	}
}

func TestSessionKeyUserIsolationMaxSessionsLRUAndActiveWorkDirs(t *testing.T) {
	p1 := SessionParams{UserID: "user-1", DeviceID: "dev-1", PlaySessionID: "ps-1", ItemID: "item-1"}
	p2 := SessionParams{UserID: "user-2", DeviceID: "dev-1", PlaySessionID: "ps-1", ItemID: "item-1"}
	if SessionKey(p1) == SessionKey(p2) {
		t.Fatalf("expected distinct SessionKey for different users, got same %q", SessionKey(p1))
	}

	tmp := t.TempDir()
	cfg := config.Default(tmp)
	// WithMaxSessions(0) ignored, WithMaxSessions(2) enforced
	mgr := NewManager(cfg, WithCapabilities(SoftwareOnlyCapabilities()), WithMaxSessions(0), WithMaxSessions(2))
	defer mgr.Close()

	// Register 2 sessions, age session 1, register 3rd -> session 1 evicted via LRU
	s1, err := mgr.RegisterSession(SessionParams{UserID: "u1", PlaySessionID: "ps1", InputPath: filepath.Join(tmp, "1.mkv")})
	if err != nil {
		t.Fatalf("RegisterSession 1: %v", err)
	}
	s2, err := mgr.RegisterSession(SessionParams{UserID: "u2", PlaySessionID: "ps2", InputPath: filepath.Join(tmp, "2.mkv")})
	if err != nil {
		t.Fatalf("RegisterSession 2: %v", err)
	}
	mgr.mu.Lock()
	sess1 := mgr.sessions[s1.Key]
	sess2 := mgr.sessions[s2.Key]
	mgr.mu.Unlock()
	sess1.mu.Lock()
	sess1.lastAccess = time.Now().Add(-10 * time.Minute)
	sess1.mu.Unlock()
	sess2.mu.Lock()
	sess2.lastAccess = time.Now()
	sess2.mu.Unlock()

	s3, err := mgr.RegisterSession(SessionParams{UserID: "u3", PlaySessionID: "ps3", InputPath: filepath.Join(tmp, "3.mkv")})
	if err != nil {
		t.Fatalf("RegisterSession 3: %v", err)
	}
	if len(mgr.ActiveSessions()) != 2 {
		t.Fatalf("expected 2 active sessions after LRU eviction, got %d", len(mgr.ActiveSessions()))
	}
	dirs := mgr.ActiveWorkDirs()
	if len(dirs) != 2 {
		t.Fatalf("expected 2 active work dirs, got %v", dirs)
	}
	mgr.mu.Lock()
	_, ok1 := mgr.sessions[s1.Key]
	_, ok3 := mgr.sessions[s3.Key]
	mgr.mu.Unlock()
	if ok1 {
		t.Fatalf("expected oldest session %s to be evicted", s1.Key)
	}
	if !ok3 {
		t.Fatalf("expected newest session %s to be present", s3.Key)
	}

	// MkdirAll failure during RegisterSession (with eviction cleanup)
	blockedTmp := t.TempDir()
	cfgBlocked := config.Default(blockedTmp)
	_ = os.MkdirAll(filepath.Dir(cfgBlocked.TranscodeTempDir()), 0o700)
	_ = os.WriteFile(cfgBlocked.TranscodeTempDir(), []byte("not-a-dir"), 0o600)
	mgrBlocked := NewManager(cfgBlocked, WithCapabilities(SoftwareOnlyCapabilities()), WithMaxSessions(1))
	defer mgrBlocked.Close()
	// Pre-populate a dummy session so eviction also runs before MkdirAll fails
	mgrBlocked.mu.Lock()
	mgrBlocked.sessions["old"] = &transcodeSession{key: "old"}
	mgrBlocked.mu.Unlock()
	if _, err := mgrBlocked.RegisterSession(SessionParams{PlaySessionID: "new", InputPath: "/tmp/x.mkv"}); err == nil {
		t.Fatalf("expected error when transcode dir cannot be created")
	}

	// GetSegment rejects control chars in InputPath or SubtitlePath
	if _, err := mgr.GetSegment(context.Background(), SessionParams{InputPath: "/tmp/a\nb.mkv"}, 0); err == nil {
		t.Fatalf("expected GetSegment error for control char in InputPath")
	}
	if _, err := mgr.GetSegment(context.Background(), SessionParams{InputPath: "/tmp/a.mkv", SubtitlePath: "/tmp/s\rb.ass"}, 0); err == nil {
		t.Fatalf("expected GetSegment error for control char in SubtitlePath")
	}
}

func TestBuildFFmpegArgsHDRSubtitleAndPlaylistCoverage(t *testing.T) {
	// 1. HDR + VAAPI tonemap
	vaapiCaps := &HWCapabilities{
		PreferredH264Encoder: "h264_vaapi",
		VAAPIDevice:          "/dev/dri/renderD128",
		Codecs: []CodecInfo{{
			ShortName:       "h264_vaapi",
			VideoMediaType:  "h264",
			IsHardwareCodec: true,
			TonemapFilter:   "tonemap_vaapi=format=nv12:matrix=bt709",
		}},
	}
	if got := vaapiCaps.TonemapFilterForEncoder("h264_vaapi"); !strings.Contains(got, "tonemap_vaapi") {
		t.Fatalf("expected vaapi tonemap filter, got %q", got)
	}
	var nilCaps *HWCapabilities
	if got := nilCaps.TonemapFilterForEncoder("libx264"); !strings.Contains(got, "zscale=") {
		t.Fatalf("expected software tonemap fallback, got %q", got)
	}

	argsVAAPI := BuildFFmpegArgs(SessionParams{
		InputPath:  "/media/hdr.mkv",
		VideoCodec: "h264",
		AudioCodec: "aac",
		IsHDR:      true,
	}, 0, "/tmp/out", vaapiCaps)
	vfIdx := indexOf(argsVAAPI, "-vf")
	if vfIdx < 0 || !strings.Contains(argsVAAPI[vfIdx+1], "format=p010,hwupload,tonemap_vaapi") {
		t.Fatalf("expected VAAPI HDR tonemap filter, got %v", argsVAAPI)
	}

	// 2. HDR + software tonemap + text subtitle burn-in with startSec > 0 (copyts + post-input -ss) + negative si clamped to 0
	subIdx := 4
	argsSW := BuildFFmpegArgs(SessionParams{
		InputPath:             "/media/hdr.mkv",
		VideoCodec:            "h264",
		AudioCodec:            "aac",
		IsHDR:                 true,
		SubtitleMethod:        "Encode",
		SubtitleCodec:         "srt",
		SubtitleStreamIndex:   &subIdx,
		SubtitleRelativeIndex: -1,
	}, 18, "/tmp/out", SoftwareOnlyCapabilities())
	if indexOf(argsSW, "-copyts") < 0 {
		t.Fatalf("expected -copyts for text subtitle burn-in with startSec > 0: %v", argsSW)
	}
	vfSW := argsSW[indexOf(argsSW, "-vf")+1]
	if !strings.Contains(vfSW, "zscale=") || !strings.Contains(vfSW, ":si=0") {
		t.Fatalf("expected software tonemap and :si=0, got %q", vfSW)
	}

	// 3. Burn-in disabled when SubtitlePath contains control chars
	argsCtrl := BuildFFmpegArgs(SessionParams{
		InputPath:      "/media/m.mkv",
		VideoCodec:     "h264",
		AudioCodec:     "aac",
		SubtitleMethod: "Encode",
		SubtitlePath:   "/media/bad\nsub.ass",
	}, 0, "/tmp/out", SoftwareOnlyCapabilities())
	if indexOf(argsCtrl, "-vf") >= 0 {
		t.Fatalf("expected no -vf when subtitle path has control chars: %v", argsCtrl)
	}

	// 4. Bitmap subtitle overlay with scale vfParts
	argsBitmap := BuildFFmpegArgs(SessionParams{
		InputPath:           "/media/bd.mkv",
		VideoCodec:          "h264",
		AudioCodec:          "aac",
		MaxWidth:            1280,
		SubtitleMethod:      "Encode",
		SubtitleCodec:       "pgssub",
		SubtitleStreamIndex: &subIdx,
	}, 0, "/tmp/out", SoftwareOnlyCapabilities())
	fcIdx := indexOf(argsBitmap, "-filter_complex")
	if fcIdx < 0 || !strings.Contains(argsBitmap[fcIdx+1], "overlay,scale=") {
		t.Fatalf("expected overlay with scale in filter_complex, got %v", argsBitmap)
	}

	// 5. Playlist: fMP4 version 7 (master & media), zero duration, sub-second subtitle playlist
	masterFMP4 := BuildMasterPlaylist(PlaylistParams{
		DurationSec:      12,
		SegmentDuration:  6,
		SegmentContainer: "m4s",
	})
	if !strings.Contains(masterFMP4, "#EXT-X-VERSION:7") {
		t.Fatalf("expected HLS v7 master playlist for m4s, got:\n%s", masterFMP4)
	}
	fmp4Pl := BuildMediaPlaylist(PlaylistParams{
		DurationSec:      12,
		SegmentDuration:  6,
		SegmentContainer: "m4s",
	})
	if !strings.Contains(fmp4Pl, "#EXT-X-VERSION:7") || !strings.Contains(fmp4Pl, "0.m4s") {
		t.Fatalf("expected HLS v7 fMP4 playlist, got:\n%s", fmp4Pl)
	}
	if segs := SegmentDurations(0, 6); segs != nil {
		t.Fatalf("expected nil segments for 0 duration, got %v", segs)
	}
	if segs := SegmentDurations(0.0001, 6); len(segs) != 1 {
		t.Fatalf("expected 1 segment for tiny duration, got %v", segs)
	}
	subPl := BuildSubtitlePlaylist(0.4, "sub.vtt")
	if !strings.Contains(subPl, "#EXT-X-TARGETDURATION:1") {
		t.Fatalf("expected target duration 1 for 0.4s subtitle playlist, got:\n%s", subPl)
	}
}

func TestHWAccelAndExtractorSecurityCoverage(t *testing.T) {
	// hwaccel helpers
	tmp := t.TempDir()
	singleFile := filepath.Join(tmp, "dev0")
	_ = os.WriteFile(singleFile, []byte("x"), 0o600)
	if got := defaultCheckFile(singleFile); len(got) != 1 || got[0] != singleFile {
		t.Fatalf("defaultCheckFile single file got %v", got)
	}
	if got := defaultCheckFile(filepath.Join(tmp, "dev*")); len(got) != 1 || got[0] != singleFile {
		t.Fatalf("defaultCheckFile glob got %v", got)
	}

	// HardwareDetector caches on second call & ProbeHardware empty ffmpegPath
	det := NewHardwareDetector(filepath.Join(tmp, "no-ffmpeg"))
	c1 := det.Detect(context.Background())
	c2 := det.Detect(context.Background())
	if c1 != c2 {
		t.Fatalf("expected HardwareDetector.Detect to return cached instance")
	}
	_ = ProbeHardware(context.Background(), "")

	// BuildHWCapabilities when only HEVC HW encoder exists
	capsHEVCOnly := BuildHWCapabilities([]string{"videotoolbox"}, map[string]bool{"hevc_videotoolbox": true}, "darwin")
	if capsHEVCOnly.PreferredHWAccel != "videotoolbox" || capsHEVCOnly.PreferredHEVCEncoder != "hevc_videotoolbox" {
		t.Fatalf("expected HEVC-only HW accel detection, got %+v", capsHEVCOnly)
	}

	// Extractor helpers: resolveMediaInputPath, isSafeAttachmentFilename, listFontFiles
	if _, err := resolveMediaInputPath("/tmp/a\nb.mkv"); err == nil {
		t.Fatalf("expected error for control char in resolveMediaInputPath")
	}
	if got, err := resolveMediaInputPath("https://example.com/v.mkv"); err != nil || got != "https://example.com/v.mkv" {
		t.Fatalf("expected http URL passthrough, got %q err=%v", got, err)
	}
	for _, bad := range []string{"", ".hidden.ttf", "a/b.ttf", `a\b.ttf`, "a..b.ttf", "a\x00b.ttf"} {
		if isSafeAttachmentFilename(bad) {
			t.Fatalf("expected isSafeAttachmentFilename(%q) == false", bad)
		}
	}
	if !isSafeAttachmentFilename("OpenSans-Regular.ttf") {
		t.Fatalf("expected valid font filename to pass")
	}
	if _, err := listFontFiles(filepath.Join(tmp, "no-such-dir")); err == nil {
		t.Fatalf("expected error from listFontFiles on missing dir")
	}

	if runtime.GOOS == "windows" {
		return
	}

	// Fake ffmpeg script for ExtractFonts safe filtering & ExtractChapterThumbnail error/success paths
	fakeFFmpeg := filepath.Join(tmp, "fake-ffmpeg.sh")
	script := `#!/bin/sh
case "$*" in
  *-dump_attachment:t*)
    printf "font-data" > "SafeFont.ttf"
    printf "hidden" > ".hidden.ttf"
    printf "notfont" > "readme.txt"
    mkdir -p "subdir"
    ln -s "SafeFont.ttf" "symlink.ttf"
    exit 0
    ;;
  *fail-case*)
    echo "simulated ffmpeg error" >&2
    exit 1
    ;;
  *empty-case*)
    # leave output file empty
    exit 0
    ;;
  *)
    # write non-empty output to last arg
    for last; do :; done
    printf "FAKEJPEG" > "$last"
    exit 0
    ;;
esac
`
	if err := os.WriteFile(fakeFFmpeg, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default(tmp)
	cfg.FFmpegPath = fakeFFmpeg
	ext := NewExtractor(cfg)

	// ExtractFonts filters out hidden, non-font, symlink, and directories
	fonts, err := ext.ExtractFonts(context.Background(), filepath.Join(tmp, "anime.mkv"), "item1", "ms1")
	if err != nil {
		t.Fatalf("ExtractFonts: %v", err)
	}
	if len(fonts) != 1 || filepath.Base(fonts[0]) != "SafeFont.ttf" {
		t.Fatalf("expected only SafeFont.ttf, got %v", fonts)
	}

	// ExtractChapterThumbnail edge cases
	if _, err := ext.ExtractChapterThumbnail(context.Background(), "", "item1", 0, 0); err == nil {
		t.Fatalf("expected error for empty mediaPath")
	}
	if _, err := ext.ExtractChapterThumbnail(context.Background(), "/tmp/v.mkv", "item1", -1, 0); err == nil {
		t.Fatalf("expected error for negative chapterIdx")
	}
	if _, err := ext.ExtractChapterThumbnail(context.Background(), filepath.Join(tmp, "fail-case.mkv"), "item-fail", 0, -500); err == nil || !strings.Contains(err.Error(), "simulated ffmpeg error") {
		t.Fatalf("expected simulated ffmpeg error, got %v", err)
	}
	if _, err := ext.ExtractChapterThumbnail(context.Background(), filepath.Join(tmp, "empty-case.mkv"), "item-empty", 0, 0); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("expected empty thumbnail error, got %v", err)
	}
	thumb, err := ext.ExtractChapterThumbnail(context.Background(), filepath.Join(tmp, "ok.mkv"), "item-ok", 0, -100)
	if err != nil || !fileReady(thumb) {
		t.Fatalf("expected valid thumbnail, got %q err=%v", thumb, err)
	}
	// Cache hit
	thumb2, err := ext.ExtractChapterThumbnail(context.Background(), filepath.Join(tmp, "ok.mkv"), "item-ok", 0, 0)
	if err != nil || thumb2 != thumb {
		t.Fatalf("expected cached thumbnail %q, got %q err=%v", thumb, thumb2, err)
	}
}
