package transcode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/store/sqlite"
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

func TestC02_MaxRedirectsSmokeAndLoopbackStreamProxy(t *testing.T) {
	// 1. Real ffmpeg smoke test: -max_redirects 0 must be accepted without "Unrecognized option".
	if ffmpegPath, err := exec.LookPath("ffmpeg"); err == nil {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		closedAddr := ln.Addr().String()
		_ = ln.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		args := BuildFFmpegArgs(SessionParams{
			InputPath:  "http://" + closedAddr + "/test.mkv",
			VideoCodec: "copy",
			AudioCodec: "aac",
		}, 0, t.TempDir(), SoftwareOnlyCapabilities())
		out, _ := exec.CommandContext(ctx, ffmpegPath, args...).CombinedOutput()
		if strings.Contains(string(out), "Unrecognized option") || strings.Contains(string(out), "Option not found") {
			t.Fatalf("ffmpeg rejected -max_redirects 0 in BuildFFmpegArgs: %s", string(out))
		}

		subArgs := BuildSubtitleExtractArgs("http://"+closedAddr+"/test.mkv", 2, "vtt", filepath.Join(t.TempDir(), "s.vtt"))
		outSub, _ := exec.CommandContext(ctx, ffmpegPath, subArgs...).CombinedOutput()
		if strings.Contains(string(outSub), "Unrecognized option") || strings.Contains(string(outSub), "Option not found") {
			t.Fatalf("ffmpeg rejected -max_redirects 0 in BuildSubtitleExtractArgs: %s", string(outSub))
		}
	}

	// 2. LoopbackStreamProxy: header injection, Range passthrough, token enforcement, SingleUse, and SSRF redirect block.
	var gotCookie, gotRef, gotRange string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect-ssrf" {
			http.Redirect(w, r, "http://169.254.169.254/latest/meta-data", http.StatusFound)
			return
		}
		gotCookie = r.Header.Get("Cookie")
		gotRef = r.Header.Get("Referer")
		gotRange = r.Header.Get("Range")
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Accept-Ranges", "bytes")
		if gotRange != "" {
			w.Header().Set("Content-Range", "bytes 0-3/10")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte("DATA"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("FULLSTREAM"))
	}))
	defer upstream.Close()

	proxy, err := StartLoopbackStreamProxy(LoopbackProxyOptions{
		TargetURL: upstream.URL + "/video.mp4",
		Headers: map[string]string{
			"Cookie":  "pan_sess=secret123",
			"Referer": "https://pan.example.com/",
		},
		AllowPrivateLAN: true,
		SingleUse:       true,
	})
	if err != nil {
		t.Fatalf("StartLoopbackStreamProxy: %v", err)
	}
	defer proxy.Close()

	// Wrong token path -> 403 without consuming single-use token
	badResp, err := http.Get(strings.TrimSuffix(proxy.URL(), "/media.mp4") + "-wrong")
	if err != nil {
		t.Fatal(err)
	}
	_ = badResp.Body.Close()
	if badResp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for wrong token, got %d", badResp.StatusCode)
	}

	// Valid Range request -> 206 + upstream custom headers injected
	req, _ := http.NewRequest(http.MethodGet, proxy.URL(), nil)
	req.Header.Set("Range", "bytes=0-3")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent || string(body) != "DATA" {
		t.Fatalf("unexpected proxy response: code=%d body=%q", resp.StatusCode, string(body))
	}
	if gotCookie != "pan_sess=secret123" || gotRef != "https://pan.example.com/" || gotRange != "bytes=0-3" {
		t.Fatalf("headers not forwarded properly: cookie=%q ref=%q range=%q", gotCookie, gotRef, gotRange)
	}

	// Second request on SingleUse proxy -> 403
	resp2, err := http.Get(proxy.URL())
	if err != nil {
		t.Fatal(err)
	}
	_ = resp2.Body.Close()
	if resp2.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 on second request to SingleUse proxy, got %d", resp2.StatusCode)
	}

	// Redirect to cloud metadata IP must be blocked -> 502
	redirProxy, err := StartLoopbackStreamProxy(LoopbackProxyOptions{
		TargetURL:       upstream.URL + "/redirect-ssrf",
		AllowPrivateLAN: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer redirProxy.Close()
	respRedir, err := http.Get(redirProxy.URL())
	if err != nil {
		t.Fatal(err)
	}
	_ = respRedir.Body.Close()
	if respRedir.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502 when upstream redirects to cloud metadata IP, got %d", respRedir.StatusCode)
	}
}

func TestH10_M10_M11_ContiguousSeekStaleCleanupFMP4ThrottleCacheAndEncodingOpts(t *testing.T) {
	// 1. highestReadySegmentLocked stops at gap (seg_0, seg_1 ready; seg_2 missing; stale seg_50 present)
	workDir := t.TempDir()
	sess := &transcodeSession{
		workDir:  workDir,
		startSeg: 0,
		params:   SessionParams{SegmentContainer: "ts", SegmentDuration: 6},
	}
	for _, idx := range []int{0, 1, 50} {
		_ = os.WriteFile(filepath.Join(workDir, fmt.Sprintf("seg_%d.ts", idx)), []byte("ts"), 0o600)
	}
	sess.mu.Lock()
	head := sess.highestReadySegmentLocked()
	sess.mu.Unlock()
	if head != 1 {
		t.Fatalf("expected contiguous head=1 (stopping before gap at 2, ignoring stale 50), got %d", head)
	}

	// 2. startProcessLocked cleans stale segments idx >= newStartSeg and promotes staged fMP4 init_gen_N.mp4 atomically
	if runtime.GOOS != "windows" {
		tmp := t.TempDir()
		fakeFMP4 := filepath.Join(tmp, "fake-fmp4-gen.sh")
		script := `#!/bin/sh
initname="init.mp4"
start=0
segpat=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "-hls_fmp4_init_filename" ]; then initname="$arg"; fi
  if [ "$prev" = "-start_number" ]; then start="$arg"; fi
  if [ "$prev" = "-hls_segment_filename" ]; then segpat="$arg"; fi
  prev="$arg"
done
dir=$(dirname "$segpat")
printf 'NEW_INIT_%s' "$start" > "$dir/$initname"
seg=$(echo "$segpat" | sed "s/%d/$start/")
printf 'SEG_%s' "$start" > "$seg"
exit 0
`
		if err := os.WriteFile(fakeFMP4, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
		cfg := config.Default(tmp)
		cfg.FFmpegPath = fakeFMP4
		mgr := NewManager(cfg, WithCapabilities(SoftwareOnlyCapabilities()))
		defer mgr.Close()

		p := SessionParams{
			PlaySessionID:    "fmp4-atomic",
			ItemID:           "item-fmp4",
			InputPath:        filepath.Join(tmp, "m.mkv"),
			SegmentContainer: "m4s",
			SegmentDuration:  6,
		}
		snap, err := mgr.RegisterSession(p)
		if err != nil {
			t.Fatal(err)
		}
		// Seed old init.mp4 (hold open fd to verify inode is not truncated in-place) + stale seg_10.m4s + seg_2.m4s
		oldInitPath := filepath.Join(snap.WorkDir, "init.mp4")
		_ = os.WriteFile(oldInitPath, []byte("ORIGINAL_OPEN_INIT_BYTES"), 0o600)
		openFD, err := os.Open(oldInitPath)
		if err != nil {
			t.Fatal(err)
		}
		defer openFD.Close()
		_ = os.WriteFile(filepath.Join(snap.WorkDir, "seg_2.m4s"), []byte("keep-before-seek"), 0o600)
		_ = os.WriteFile(filepath.Join(snap.WorkDir, "seg_10.m4s"), []byte("stale-future-seg"), 0o600)

		// Seek to segment 5 -> seg_10.m4s must be purged before restart, seg_2.m4s preserved, openFD unaffected!
		if _, err := mgr.GetSegment(context.Background(), p, 5); err != nil {
			t.Fatalf("GetSegment(5): %v", err)
		}
		if _, err := os.Stat(filepath.Join(snap.WorkDir, "seg_10.m4s")); !os.IsNotExist(err) {
			t.Fatalf("expected stale seg_10.m4s >= startSeg(5) to be removed")
		}
		if !fileReady(filepath.Join(snap.WorkDir, "seg_2.m4s")) {
			t.Fatalf("expected seg_2.m4s < startSeg(5) to be retained")
		}
		oldBytes, _ := io.ReadAll(openFD)
		if string(oldBytes) != "ORIGINAL_OPEN_INIT_BYTES" {
			t.Fatalf("open reader on init.mp4 saw in-place corruption: %q", string(oldBytes))
		}
		newInitBytes, _ := os.ReadFile(oldInitPath)
		if string(newInitBytes) != "NEW_INIT_5" {
			t.Fatalf("expected atomic rename to update init.mp4 to NEW_INIT_5, got %q", string(newInitBytes))
		}

		// 3. Lead-buffer throttling (SIGSTOP when ahead > buffer, SIGCONT when client catches up)
		throttleTmp := t.TempDir()
		fakeThrottleFF := filepath.Join(throttleTmp, "fake-throttle.sh")
		throttleScript := `#!/bin/sh
segpat=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "-hls_segment_filename" ]; then segpat="$arg"; fi
  prev="$arg"
done
for i in 0 1 2 3 4 5; do
  seg=$(echo "$segpat" | sed "s/%d/$i/")
  printf 'SEGMENT_DATA_PAYLOAD_%d' "$i" > "$seg"
done
exec sleep 10
`
		_ = os.WriteFile(fakeThrottleFF, []byte(throttleScript), 0o755)
		tCfg := config.Default(throttleTmp)
		tCfg.FFmpegPath = fakeThrottleFF
		tMgr := NewManager(tCfg,
			WithCapabilities(SoftwareOnlyCapabilities()),
			WithThrottleConfig(true, 12, 4), // 2 segments * 6s = 12s buffer
		)
		defer tMgr.Close()

		tp := SessionParams{
			PlaySessionID:   "ps-throttle",
			ItemID:          "it-throttle",
			InputPath:       filepath.Join(throttleTmp, "v.mkv"),
			SegmentDuration: 6,
		}
		if _, err := tMgr.GetSegment(context.Background(), tp, 0); err != nil {
			t.Fatal(err)
		}
		tMgr.mu.Lock()
		ts := tMgr.sessions[SessionKey(tp)]
		tMgr.mu.Unlock()

		// Wait up to 5s for throttle monitor to suspend process (head=5, lastReq=0 -> lead=30s > 12s)
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) && !ts.IsThrottled() {
			time.Sleep(30 * time.Millisecond)
		}
		if !ts.IsThrottled() {
			t.Fatal("expected transcode session to be throttled when ready segments exceed ThrottleBufferSize")
		}

		// Client advances to segment 5 (lead=0s <= 12-4=8s) -> immediately resumed!
		if _, err := tMgr.GetSegment(context.Background(), tp, 5); err != nil {
			t.Fatal(err)
		}
		if ts.IsThrottled() {
			t.Fatal("expected transcode session to resume immediately when client catches up")
		}

		// 4. Max cache bytes pruning deletes already-played segments (idx < lastReq-2)
		cMgr := NewManager(config.Default(t.TempDir()),
			WithCapabilities(SoftwareOnlyCapabilities()),
			WithMaxCacheBytes(30),
		)
		defer cMgr.Close()
		cs, _ := cMgr.RegisterSession(SessionParams{PlaySessionID: "cache1", ItemID: "ic1", InputPath: "/a.mkv"})
		for i := 0; i <= 5; i++ {
			_ = os.WriteFile(filepath.Join(cs.WorkDir, fmt.Sprintf("seg_%d.ts", i)), []byte("0123456789"), 0o600)
		}
		cMgr.mu.Lock()
		cMgr.sessions[cs.Key].lastRequestedSeg = 5
		cMgr.mu.Unlock()
		cMgr.EnforceCacheBytesLimit()
		if _, err := os.Stat(filepath.Join(cs.WorkDir, "seg_0.ts")); !os.IsNotExist(err) {
			t.Fatal("expected EnforceCacheBytesLimit to prune old played seg_0.ts")
		}
		if !fileReady(filepath.Join(cs.WorkDir, "seg_5.ts")) {
			t.Fatal("expected current seg_5.ts to be retained")
		}

		// 5. ProbeHardware 1-frame 64x64 encoder self-check filters broken HW encoders
		origProbe := probeEncoderFn
		probeEncoderFn = func(_ context.Context, _, enc, _ string) bool {
			return enc != "h264_videotoolbox" && enc != "h264_nvenc"
		}
		fakeProbeBin := filepath.Join(tmp, "fake-probe-ff.sh")
		_ = os.WriteFile(fakeProbeBin, []byte("#!/bin/sh\nif [ \"$2\" = \"-hwaccels\" ]; then\n  printf \"Hardware acceleration methods:\\nvideotoolbox\\ncuda\\n\"\nelse\n  printf \" V..... h264_videotoolbox VT\\n V..... h264_nvenc NV\\n V..... libx264 x264\\n\"\nfi\n"), 0o755)
		probedCaps := ProbeHardware(context.Background(), fakeProbeBin)
		probeEncoderFn = origProbe
		if probedCaps.PreferredH264Encoder != "libx264" {
			t.Fatalf("expected broken HW encoders filtered out in favor of libx264, got %q", probedCaps.PreferredH264Encoder)
		}
	}

	// 6. /Encoding/* store options wired into Manager
	dbTmp := t.TempDir()
	st, err := sqlite.Open(filepath.Join(dbTmp, "enc.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	customTemp := filepath.Join(dbTmp, "custom-transcode-temp")
	ffRaw, _ := json.Marshal(map[string]any{
		"HardwareAccelerationMode": 0,
		"EnableHardwareEncoding":   false,
		"AllowHevcEncoding":        false,
		"H264Crf":                  19,
		"H264Preset":               "faster",
		"TranscodingTempPath":      customTemp,
	})
	tmRaw, _ := json.Marshal(map[string]any{
		"EnableToneMapping":    true,
		"ToneMappingAlgorithm": "mobius",
		"ToneMappingPeak":      250,
	})
	_, _ = st.Writer.Exec(`INSERT INTO server_config (key, value_json, updated_at) VALUES ('encoding_ffmpeg_options', ?, 1), ('encoding_full_tonemap', ?, 1)`, string(ffRaw), string(tmRaw))

	eMgr := NewManager(config.Default(dbTmp), WithCapabilities(&HWCapabilities{
		FFmpegAvailable:      true,
		PreferredH264Encoder: "h264_nvenc",
	}))
	defer eMgr.Close()
	eMgr.SetStore(st)

	// Hardware disabled -> Capabilities returns libx264
	if got := eMgr.Capabilities(context.Background()).PreferredH264Encoder; got != "libx264" {
		t.Fatalf("expected software encoder when HardwareAccelerationMode=0, got %q", got)
	}
	snapEnc, err := eMgr.RegisterSession(SessionParams{
		PlaySessionID: "enc-sess",
		ItemID:        "enc-item",
		InputPath:     filepath.Join(dbTmp, "hdr.mkv"),
		VideoCodec:    "hevc",
		IsHDR:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(snapEnc.WorkDir, customTemp) {
		t.Fatalf("expected session workDir under custom TranscodingTempPath %q, got %q", customTemp, snapEnc.WorkDir)
	}
	applied := eMgr.applyEncodingOptionsToParams(SessionParams{
		InputPath:  "/media/hdr.mkv",
		VideoCodec: "hevc",
		IsHDR:      true,
	})
	built := strings.Join(BuildFFmpegArgs(applied, 0, "/tmp/out", eMgr.Capabilities(context.Background())), " ")
	for _, want := range []string{"-c:v libx264", "-preset faster", "-crf 19", "npl=250", "tonemap=mobius"} {
		if !strings.Contains(built, want) {
			t.Fatalf("expected %q in built args from /Encoding/* options, got: %s", want, built)
		}
	}
}
