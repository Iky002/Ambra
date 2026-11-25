package transcode

import (
	"context"
	"errors"
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

func TestAuditV0104_R01_BurnInStagingAndSingleQuote(t *testing.T) {
	tmp := t.TempDir()
	workDir := filepath.Join(tmp, "work")
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		t.Fatal(err)
	}

	// 1. External subtitle with single quote, parentheses, brackets, and spaces -> staged to workDir/burn_sub.srt
	movieDir := filepath.Join(tmp, "Schindler's List (1993) [1080p]")
	if err := os.MkdirAll(movieDir, 0o755); err != nil {
		t.Fatal(err)
	}
	extSub := filepath.Join(movieDir, "Schindler's List [zh].srt")
	subBody := "1\n00:00:00,000 --> 00:00:02,000\nSchindler's List\n"
	if err := os.WriteFile(extSub, []byte(subBody), 0o644); err != nil {
		t.Fatal(err)
	}

	stagedExt := stageBurnInFiles(SessionParams{
		InputPath:      filepath.Join(movieDir, "Schindler's List.mkv"),
		SubtitleMethod: "Encode",
		SubtitleCodec:  "srt",
		SubtitlePath:   extSub,
	}, workDir)
	wantSub := filepath.Join(workDir, "burn_sub.srt")
	if stagedExt.SubtitlePath != wantSub {
		t.Fatalf("expected external subtitle staged to %q, got %q", wantSub, stagedExt.SubtitlePath)
	}
	if gotData, err := os.ReadFile(wantSub); err != nil || string(gotData) != subBody {
		t.Fatalf("staged subtitle content mismatch: %q err=%v", string(gotData), err)
	}

	// 2. Embedded subtitle with single quote in InputPath -> symlinked to workDir/burn_input.mkv
	mediaFile := filepath.Join(movieDir, "Schindler's List.mkv")
	if err := os.WriteFile(mediaFile, []byte("stub"), 0o644); err != nil {
		t.Fatal(err)
	}
	subIdx := 2
	stagedEmb := stageBurnInFiles(SessionParams{
		InputPath:           mediaFile,
		SubtitleMethod:      "Encode",
		SubtitleCodec:       "ass",
		SubtitleStreamIndex: &subIdx,
	}, workDir)
	wantLink := filepath.Join(workDir, "burn_input.mkv")
	if stagedEmb.InputPath != wantLink {
		t.Fatalf("expected embedded input staged to %q, got %q", wantLink, stagedEmb.InputPath)
	}
	if _, err := os.Stat(wantLink); err != nil {
		t.Fatalf("staged symlink not readable: %v", err)
	}

	// 3. Real FFmpeg burn-in test with single-quoted path (both external & embedded) if ffmpeg is available
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	realMKV := filepath.Join(movieDir, "Schindler's List (1993).mkv")
	genCmd := exec.CommandContext(ctx, ffmpegPath,
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=duration=3:size=320x180:rate=24",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=3",
		"-i", extSub,
		"-map", "0:v", "-map", "1:a", "-map", "2:s",
		"-c:v", "libx264", "-preset", "ultrafast",
		"-c:a", "aac", "-c:s", "srt",
		realMKV,
	)
	if out, err := genCmd.CombinedOutput(); err != nil {
		t.Fatalf("generate real mkv: %v (%s)", err, string(out))
	}

	cfg := config.Default(tmp)
	cfg.FFmpegPath = ffmpegPath
	mgr := NewManager(cfg, WithCapabilities(SoftwareOnlyCapabilities()))
	defer mgr.Close()

	// External subtitle burn-in
	segPath, err := mgr.GetSegment(ctx, SessionParams{
		PlaySessionID:   "r01-ext",
		ItemID:          "item-r01",
		InputPath:       realMKV,
		VideoCodec:      "h264",
		AudioCodec:      "aac",
		SubtitleMethod:  "Encode",
		SubtitleCodec:   "srt",
		SubtitlePath:    extSub,
		SegmentDuration: 3,
	}, 0)
	if err != nil || !fileReady(segPath) {
		t.Fatalf("real ffmpeg external burn-in with single quote failed: seg=%q err=%v", segPath, err)
	}

	// Embedded subtitle burn-in
	embSeg, err := mgr.GetSegment(ctx, SessionParams{
		PlaySessionID:         "r01-emb",
		ItemID:                "item-r01",
		InputPath:             realMKV,
		VideoCodec:            "h264",
		AudioCodec:            "aac",
		SubtitleMethod:        "Encode",
		SubtitleCodec:         "srt",
		SubtitleStreamIndex:   &subIdx,
		SubtitleRelativeIndex: 0,
		SegmentDuration:       3,
	}, 0)
	if err != nil || !fileReady(embSeg) {
		t.Fatalf("real ffmpeg embedded burn-in with single quote failed: seg=%q err=%v", embSeg, err)
	}
}

func TestAuditV0104_H05_ProtocolWhitelistAndSSRF(t *testing.T) {
	// 1. Extract args carry explicit -protocol_whitelist
	localExtract := BuildSubtitleExtractArgs("/media/movie.mkv", 2, "vtt", "/tmp/out.vtt")
	idxWL := slices.Index(localExtract, "-protocol_whitelist")
	if idxWL < 0 || localExtract[idxWL+1] != "file,pipe" {
		t.Fatalf("expected local extract protocol_whitelist file,pipe, got %v", localExtract)
	}

	remoteExtract := BuildSubtitleExtractArgs("https://93.184.216.34/movie.mkv", 2, "vtt", "/tmp/out.vtt")
	idxWL = slices.Index(remoteExtract, "-protocol_whitelist")
	if idxWL < 0 || remoteExtract[idxWL+1] != "file,pipe,http,https,tcp,tls,crypto" {
		t.Fatalf("expected remote extract protocol_whitelist, got %v", remoteExtract)
	}
	if idxFR := slices.Index(remoteExtract, "-follow_redirects"); idxFR < 0 || remoteExtract[idxFR+1] != "0" {
		t.Fatalf("expected -follow_redirects 0 for remote extract, got %v", remoteExtract)
	}

	// 2. BuildFFmpegArgs carries -protocol_whitelist and -follow_redirects 0 for remote HTTP
	remoteArgs := BuildFFmpegArgs(SessionParams{
		InputPath:  "https://93.184.216.34/movie.mkv",
		VideoCodec: "copy",
		AudioCodec: "aac",
	}, 0, "/tmp/out", SoftwareOnlyCapabilities())
	idxFR := slices.Index(remoteArgs, "-follow_redirects")
	idxIn := slices.Index(remoteArgs, "-i")
	if idxFR < 0 || idxFR >= idxIn || remoteArgs[idxFR+1] != "0" {
		t.Fatalf("expected -follow_redirects 0 before -i, got %v", remoteArgs)
	}

	// 3. Manager rejects SSRF metadata URLs in InputPath & SubtitlePath
	tmp := t.TempDir()
	mgr := NewManager(config.Default(tmp), WithCapabilities(SoftwareOnlyCapabilities()))
	defer mgr.Close()

	if _, err := mgr.GetSegment(context.Background(), SessionParams{
		InputPath: "http://169.254.169.254/latest/meta-data",
	}, 0); err == nil {
		t.Fatal("expected SSRF InputPath to be rejected")
	}
	if _, err := mgr.GetSegment(context.Background(), SessionParams{
		InputPath:    "/media/ok.mkv",
		SubtitlePath: "http://169.254.169.254/sub.srt",
	}, 0); err == nil {
		t.Fatal("expected SSRF SubtitlePath to be rejected")
	}
	if _, err := mgr.GetInitSegment(context.Background(), SessionParams{
		InputPath: "http://169.254.169.254/latest/meta-data",
	}); err == nil {
		t.Fatal("expected GetInitSegment to reject SSRF InputPath")
	}
}

func TestAuditV0104_H06_SessionDestroyedErrSessionClosed(t *testing.T) {
	tmp := t.TempDir()
	sess := &transcodeSession{
		key:        "k1",
		ffmpegPath: "ffmpeg",
		params: SessionParams{
			InputPath:        filepath.Join(tmp, "m.mkv"),
			SegmentContainer: "ts",
			SegmentDuration:  6,
		},
		workDir: filepath.Join(tmp, "work"),
	}
	sess.destroy()

	if _, err := sess.getSegment(context.Background(), 0, SoftwareOnlyCapabilities); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("expected ErrSessionClosed from getSegment on destroyed session, got %v", err)
	}
	if _, err := sess.getInitSegment(context.Background(), SoftwareOnlyCapabilities); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("expected ErrSessionClosed from getInitSegment on destroyed session, got %v", err)
	}
	sess.mu.Lock()
	err := sess.startProcessLocked(0, SoftwareOnlyCapabilities())
	sess.mu.Unlock()
	if !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("expected ErrSessionClosed from startProcessLocked on destroyed session, got %v", err)
	}
}

func TestAuditV0104_MH10_PerUserQuota(t *testing.T) {
	tmp := t.TempDir()
	mgr := NewManager(
		config.Default(tmp),
		WithCapabilities(SoftwareOnlyCapabilities()),
		WithMaxSessions(10),
		WithMaxSessionsPerUser(2),
	)
	defer mgr.Close()

	// Victim user registers 1 session
	victim, err := mgr.RegisterSession(SessionParams{UserID: "victim", PlaySessionID: "vp1", InputPath: "/m/v.mkv"})
	if err != nil {
		t.Fatal(err)
	}
	// Attacker user registers 4 sessions -> per-user quota (2) evicts only attacker's own oldest sessions
	for i := 0; i < 4; i++ {
		_, err := mgr.RegisterSession(SessionParams{
			UserID:        "attacker",
			PlaySessionID: "ap-" + string(rune('a'+i)),
			InputPath:     "/m/a.mkv",
		})
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Millisecond)
	}

	active := mgr.ActiveSessions()
	if len(active) != 3 {
		t.Fatalf("expected 3 total sessions (1 victim + 2 attacker), got %d", len(active))
	}
	foundVictim := false
	for _, s := range active {
		if s.Key == victim.Key {
			foundVictim = true
		}
	}
	if !foundVictim {
		t.Fatal("victim session should NOT have been evicted by attacker exceeding per-user quota")
	}
}

func TestAuditV0104_StopSessionsScoped(t *testing.T) {
	tmp := t.TempDir()
	mgr := NewManager(config.Default(tmp), WithCapabilities(SoftwareOnlyCapabilities()))
	defer mgr.Close()

	reg := func(uid, psid, devid string) {
		if _, err := mgr.RegisterSession(SessionParams{
			UserID:        uid,
			PlaySessionID: psid,
			DeviceID:      devid,
			ItemID:        "it-" + psid,
			InputPath:     "/m/" + psid + ".mkv",
		}); err != nil {
			t.Fatal(err)
		}
	}
	reg("u1", "ps-u1", "dev-1")
	reg("u2", "ps-u2", "dev-2")
	reg("", "ps-apikey", "dev-key")

	// Non-admin cannot stop unowned (API-key UserID=="") or other user's session
	if n := mgr.StopSessionsScoped("u1", "ps-apikey", "", false); n != 0 {
		t.Fatalf("non-admin must not stop API-key session, stopped=%d", n)
	}
	if n := mgr.StopSessionsScoped("u1", "ps-u2", "dev-2", false); n != 0 {
		t.Fatalf("non-admin must not stop other user's session, stopped=%d", n)
	}
	if n := mgr.StopSessionsScoped("", "ps-u1", "dev-1", false); n != 0 {
		t.Fatalf("non-admin with empty userID must stop 0, stopped=%d", n)
	}
	// Non-admin can stop their own session
	if n := mgr.StopSessionsScoped("u1", "ps-u1", "", false); n != 1 {
		t.Fatalf("non-admin should stop own session, stopped=%d", n)
	}
	// Admin can stop both u2 and API-key session
	if n := mgr.StopSessionsScoped("admin", "ps-apikey", "", true); n != 1 {
		t.Fatalf("admin should stop API-key session, stopped=%d", n)
	}
	if n := mgr.StopSessionsScoped("admin", "", "dev-2", true); n != 1 {
		t.Fatalf("admin should stop u2 by deviceID, stopped=%d", n)
	}
	if len(mgr.ActiveSessions()) != 0 {
		t.Fatalf("expected 0 active sessions remaining, got %d", len(mgr.ActiveSessions()))
	}
}

func TestAuditV0104_FMP4PlaylistAndGetInitSegment(t *testing.T) {
	q := url.Values{}
	q.Set("PlaySessionId", "ps-fmp4")
	pl := BuildMediaPlaylist(PlaylistParams{
		PlaylistID:       "main",
		DurationSec:      12,
		SegmentDuration:  6,
		SegmentContainer: "m4s",
		Query:            q,
	})
	wantMap := `#EXT-X-MAP:URI="hls1/main/init.mp4?PlaySessionId=ps-fmp4"`
	if !strings.Contains(pl, wantMap) {
		t.Fatalf("expected fMP4 playlist to contain %q, got:\n%s", wantMap, pl)
	}

	// Test GetInitSegment with fake ffmpeg writing init.mp4
	tmp := t.TempDir()
	fakeFFmpeg := filepath.Join(tmp, "fake-fmp4.sh")
	script := `#!/bin/sh
prev=""
for arg in "$@"; do
  if [ "$prev" = "-hls_segment_filename" ]; then
    dir=$(dirname "$arg")
    printf 'FAKE_INIT_MP4' > "$dir/init.mp4"
    exit 0
  fi
  prev="$arg"
done
exit 1
`
	if err := os.WriteFile(fakeFFmpeg, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default(tmp)
	cfg.FFmpegPath = fakeFFmpeg
	mgr := NewManager(cfg, WithCapabilities(SoftwareOnlyCapabilities()))
	defer mgr.Close()

	initPath, err := mgr.GetInitSegment(context.Background(), SessionParams{
		PlaySessionID:    "ps-fmp4",
		ItemID:           "item-fmp4",
		InputPath:        filepath.Join(tmp, "in.mkv"),
		SegmentContainer: "m4s",
	})
	if err != nil {
		t.Fatalf("GetInitSegment failed: %v", err)
	}
	if filepath.Base(initPath) != "init.mp4" || !fileReady(initPath) {
		t.Fatalf("unexpected init.mp4 path: %q", initPath)
	}
}

func TestAuditV0104_NVENCAndQSVHDRFilters(t *testing.T) {
	nvencCaps := &HWCapabilities{
		PreferredH264Encoder: "h264_nvenc",
		Codecs: []CodecInfo{{
			ShortName:       "h264_nvenc",
			VideoMediaType:  "h264",
			IsHardwareCodec: true,
			// Even if a custom cap provides legacy tonemap_cuda, BuildFFmpegArgs must sanitize to valid filter
			TonemapFilter: "tonemap_cuda=format=yuv420p:tonemap=hable",
		}},
	}
	nvArgs := BuildFFmpegArgs(SessionParams{
		InputPath:  "/media/hdr.mkv",
		VideoCodec: "h264",
		AudioCodec: "aac",
		IsHDR:      true,
	}, 0, "/tmp/out", nvencCaps)
	nvVF := nvArgs[indexOf(nvArgs, "-vf")+1]
	if strings.Contains(nvVF, "tonemap_cuda") || !strings.Contains(nvVF, "zscale=") || !strings.Contains(nvVF, "tonemap=hable") {
		t.Fatalf("NVENC HDR filter invalid: %q", nvVF)
	}

	qsvCaps := &HWCapabilities{
		PreferredH264Encoder: "h264_qsv",
		Codecs: []CodecInfo{{
			ShortName:       "h264_qsv",
			VideoMediaType:  "h264",
			IsHardwareCodec: true,
			TonemapFilter:   "vpp_qsv=tonemap=1:format=nv12",
		}},
	}
	qsvArgs := BuildFFmpegArgs(SessionParams{
		InputPath:  "/media/hdr.mkv",
		VideoCodec: "h264",
		AudioCodec: "aac",
		IsHDR:      true,
	}, 0, "/tmp/out", qsvCaps)
	qsvVF := qsvArgs[indexOf(qsvArgs, "-vf")+1]
	if strings.Contains(qsvVF, "vpp_qsv") || !strings.Contains(qsvVF, "zscale=") || !strings.Contains(qsvVF, "format=nv12") {
		t.Fatalf("QSV HDR filter invalid: %q", qsvVF)
	}
	if idxPix := indexOf(qsvArgs, "-pix_fmt"); idxPix < 0 || qsvArgs[idxPix+1] != "nv12" {
		t.Fatalf("expected -pix_fmt nv12 for QSV, got %v", qsvArgs)
	}
}

func TestAuditV0104_SessionParamsDifferAll7NewFields(t *testing.T) {
	base := SessionParams{
		UserID:                "u1",
		ItemID:                "i1",
		MediaSourceID:         "m1",
		InputPath:             "/a.mkv",
		VideoCodec:            "h264",
		AudioCodec:            "aac",
		AudioBitrate:          192000,
		AudioChannels:         2,
		SubtitleRelativeIndex: 0,
		SubtitleCodec:         "srt",
		SubtitlePath:          "/a.srt",
		SegmentDuration:       6,
		SegmentContainer:      "ts",
	}
	if sessionParamsDiffer(base, base) {
		t.Fatal("identical params should not differ")
	}
	cases := []struct {
		name string
		mut  func(*SessionParams)
	}{
		{"AudioBitrate", func(p *SessionParams) { p.AudioBitrate = 320000 }},
		{"AudioChannels", func(p *SessionParams) { p.AudioChannels = 6 }},
		{"SubtitleRelativeIndex", func(p *SessionParams) { p.SubtitleRelativeIndex = 1 }},
		{"SubtitleCodec", func(p *SessionParams) { p.SubtitleCodec = "ass" }},
		{"SubtitlePath", func(p *SessionParams) { p.SubtitlePath = "/b.srt" }},
		{"SegmentDuration", func(p *SessionParams) { p.SegmentDuration = 4 }},
		{"SegmentContainer", func(p *SessionParams) { p.SegmentContainer = "m4s" }},
	}
	for _, tc := range cases {
		mod := base
		tc.mut(&mod)
		if !sessionParamsDiffer(base, mod) {
			t.Fatalf("expected sessionParamsDiffer to detect change in %s", tc.name)
		}
	}
}
