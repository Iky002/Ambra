package transcode

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func buildMP4Box(boxType string, payload []byte, ext64 bool) []byte {
	var out bytes.Buffer
	if ext64 {
		var hdr [16]byte
		binary.BigEndian.PutUint32(hdr[0:4], 1)
		copy(hdr[4:8], []byte(boxType))
		binary.BigEndian.PutUint64(hdr[8:16], uint64(16+len(payload)))
		out.Write(hdr[:])
	} else {
		var hdr [8]byte
		binary.BigEndian.PutUint32(hdr[0:4], uint32(8+len(payload)))
		copy(hdr[4:8], []byte(boxType))
		out.Write(hdr[:])
	}
	out.Write(payload)
	return out.Bytes()
}

func TestV0110_EncodingOptionsTypeConversionsAndHotReload(t *testing.T) {
	// 1. Nil manager safety
	var nilMgr *Manager
	nilMgr.SetStore(nil)
	nilMgr.ReloadEncodingOptions(nil)
	if nilMgr.EncodingOptionsSnapshot().H264Preset != "veryfast" {
		t.Fatal("expected DefaultEncodingOptions from nil Manager")
	}
	if nilMgr.HasSession("x") || nilMgr.ActiveSessionCount() != 0 || nilMgr.TotalActiveTranscodes() != 0 {
		t.Fatal("expected zero counts from nil Manager")
	}
	rel, ok := nilMgr.TryAcquireStreamSlot(2, "")
	if !ok {
		t.Fatal("expected TryAcquireStreamSlot on nil Manager to allow")
	}
	rel()

	// 2. anyToString / anyToBool / anyToInt / anyToFloat / tonemap helpers
	if anyToString("abc", "d") != "abc" || anyToString(float64(12.5), "d") != "12.5" ||
		anyToString(7, "d") != "7" || anyToString(int64(9), "d") != "9" ||
		anyToString(true, "d") != "true" || anyToString(false, "d") != "false" ||
		anyToString(struct{}{}, "def") != "def" {
		t.Fatal("anyToString branch mismatch")
	}
	if !anyToBool(true, false) || !anyToBool(float64(1), false) || !anyToBool(1, false) ||
		!anyToBool("yes", false) || !anyToBool("on", false) ||
		anyToBool("false", true) || anyToBool("no", true) || anyToBool("off", true) ||
		!anyToBool("unknown", true) {
		t.Fatal("anyToBool branch mismatch")
	}
	if v, ok := anyToInt(float64(42)); !ok || v != 42 {
		t.Fatal("anyToInt float64 failed")
	}
	if v, ok := anyToInt(11); !ok || v != 11 {
		t.Fatal("anyToInt int failed")
	}
	if v, ok := anyToInt(int64(12)); !ok || v != 12 {
		t.Fatal("anyToInt int64 failed")
	}
	if v, ok := anyToInt(" 23 "); !ok || v != 23 {
		t.Fatal("anyToInt string failed")
	}
	if _, ok := anyToInt("bad"); ok {
		t.Fatal("expected anyToInt failure on non-numeric string")
	}
	if v, ok := anyToFloat(float64(2.5)); !ok || v != 2.5 {
		t.Fatal("anyToFloat float64 failed")
	}
	if v, ok := anyToFloat(10); !ok || v != 10 {
		t.Fatal("anyToFloat int failed")
	}
	if v, ok := anyToFloat(int64(20)); !ok || v != 20 {
		t.Fatal("anyToFloat int64 failed")
	}
	if v, ok := anyToFloat(" 150.5 "); !ok || v != 150.5 {
		t.Fatal("anyToFloat string failed")
	}
	if _, ok := anyToFloat("nan-str"); ok {
		t.Fatal("expected anyToFloat failure on invalid string")
	}

	if normalizeTonemapAlgorithm("reinhard") != "reinhard" || normalizeTonemapAlgorithm("invalid-algo") != "hable" {
		t.Fatal("normalizeTonemapAlgorithm mismatch")
	}
	tmFilter := buildSoftwareTonemapFilter("unknown", math.NaN(), "")
	if !strings.Contains(tmFilter, "npl=100") || !strings.Contains(tmFilter, "tonemap=hable") || !strings.Contains(tmFilter, "format=yuv420p") {
		t.Fatalf("unexpected fallback software tonemap filter: %s", tmFilter)
	}

	// 3. Hot-reload /Encoding/* from SQLite (public + full tonemap + disabling EnableThrottling wakes throttled session)
	tmp := t.TempDir()
	st, err := sqlite.Open(filepath.Join(tmp, "enc_hot.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ffRaw, _ := json.Marshal(map[string]any{
		"HardwareAccelerationMode": "1",
		"EnableHardwareEncoding":   "true",
		"AllowHevcEncoding":        "true",
		"EnableThrottling":         "true",
		"ThrottleBufferSize":       "18",
		"ThrottleHysteresis":       "6",
		"H264Crf":                  "21",
		"H264Preset":               "fast",
	})
	pubTM, _ := json.Marshal(map[string]any{
		"EnableToneMapping":    "false",
		"ToneMappingAlgorithm": "gamma",
		"ToneMappingPeak":      "120.5",
	})
	_, _ = st.Writer.Exec(`INSERT INTO server_config (key, value_json, updated_at) VALUES ('encoding_ffmpeg_options', ?, 1), ('encoding_public_tonemap', ?, 1)`, string(ffRaw), string(pubTM))

	mgr := NewManager(config.Default(tmp), WithCapabilities(SoftwareOnlyCapabilities()))
	defer mgr.Close()
	mgr.SetStore(st)
	mgr.ReloadEncodingOptions(nil)

	snap := mgr.EncodingOptionsSnapshot()
	if !snap.EnableThrottling || snap.ThrottleBufferSize != 18 || snap.ThrottleHysteresis != 6 ||
		snap.H264Crf != 21 || snap.H264Preset != "fast" || snap.EnableToneMapping ||
		snap.ToneMappingAlgorithm != "gamma" || snap.ToneMappingPeak != 120.5 {
		t.Fatalf("unexpected reloaded EncodingOptions: %+v", snap)
	}

	if runtime.GOOS != "windows" {
		fakeFF := filepath.Join(tmp, "fake-hot-throttle.sh")
		script := `#!/bin/sh
segpat=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "-hls_segment_filename" ]; then segpat="$arg"; fi
  prev="$arg"
done
for i in 0 1 2 3 4 5; do
  seg=$(echo "$segpat" | sed "s/%d/$i/")
  printf 'SEG_%d' "$i" > "$seg"
done
exec sleep 10
`
		_ = os.WriteFile(fakeFF, []byte(script), 0o755)
		mgr.cfg.FFmpegPath = fakeFF

		p := SessionParams{
			PlaySessionID:   "hot-throttle",
			ItemID:          "item-ht",
			InputPath:       filepath.Join(tmp, "v.mkv"),
			SegmentDuration: 6,
		}
		if _, err := mgr.GetSegment(context.Background(), p, 0); err != nil {
			t.Fatal(err)
		}
		mgr.mu.Lock()
		ts := mgr.sessions[SessionKey(p)]
		mgr.mu.Unlock()

		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) && !ts.IsThrottled() {
			time.Sleep(25 * time.Millisecond)
		}
		if !ts.IsThrottled() {
			t.Fatal("expected session throttled with ThrottleBufferSize=18 and head=5 (lead=30s)")
		}

		// Hot-update DB: EnableThrottling=false -> ReloadEncodingOptions immediately resumes throttled session!
		ffOff, _ := json.Marshal(map[string]any{"EnableThrottling": false})
		_, _ = st.Writer.Exec(`UPDATE server_config SET value_json = ? WHERE key = 'encoding_ffmpeg_options'`, string(ffOff))
		mgr.ReloadEncodingOptions(context.Background())
		if ts.IsThrottled() {
			t.Fatal("expected ReloadEncodingOptions(EnableThrottling=false) to immediately unthrottle session")
		}
	}
}

func TestV0110_LoopbackProxyPinningHeadersAndValidation(t *testing.T) {
	var nilProxy *LoopbackStreamProxy
	if nilProxy.URL() != "" {
		t.Fatal("expected empty URL for nil proxy")
	}
	if err := nilProxy.Close(); err != nil {
		t.Fatalf("expected nil Close error for nil proxy, got %v", err)
	}

	// StartLoopbackStreamProxy validation error branches
	for _, badURL := range []string{
		"",
		"http://example.com/bad\nurl",
		"ftp://example.com/file.mp4",
		"http:///missing-host",
	} {
		if _, err := StartLoopbackStreamProxy(LoopbackProxyOptions{TargetURL: badURL}); err == nil {
			t.Fatalf("expected error for invalid target URL %q", badURL)
		}
	}

	// resolveAndPinHost: literal IPs & localhost checks
	ctx := context.Background()
	if _, err := resolveAndPinHost(ctx, "127.0.0.1", true, false); err == nil {
		t.Fatal("expected 127.0.0.1 blocked when allowLoopback=false")
	}
	if ips, err := resolveAndPinHost(ctx, "127.0.0.1", true, true); err != nil || len(ips) != 1 || ips[0] != "127.0.0.1" {
		t.Fatalf("expected 127.0.0.1 allowed when allowLoopback=true, got %v %v", ips, err)
	}
	if _, err := resolveAndPinHost(ctx, "192.168.1.50", false, false); err == nil {
		t.Fatal("expected private LAN IP blocked when allowPrivateLAN=false")
	}
	if ips, err := resolveAndPinHost(ctx, "192.168.1.50", true, false); err != nil || len(ips) != 1 {
		t.Fatalf("expected private LAN IP allowed when allowPrivateLAN=true, got %v %v", ips, err)
	}
	if _, err := resolveAndPinHost(ctx, "localhost", true, false); err == nil {
		t.Fatal("expected localhost blocked when allowLoopback=false")
	}
	if _, err := resolveAndPinHost(ctx, "api.localhost.", true, false); err == nil {
		t.Fatal("expected .localhost subdomain blocked when allowLoopback=false")
	}
	if ips, err := resolveAndPinHost(ctx, "localhost", true, true); err != nil || len(ips) == 0 {
		t.Fatalf("expected localhost resolved when allowLoopback=true, got %v %v", ips, err)
	}

	// sanitizeUpstreamHeaders filters hop-by-hop and control chars
	san := sanitizeUpstreamHeaders(map[string]string{
		"Host":              "evil.example",
		"Connection":        "keep-alive",
		"Transfer-Encoding": "chunked",
		"X-Bad":             "line\r\ninject",
		"X-Custom-Token":    " ok-val ",
	})
	if len(san) != 1 || san["X-Custom-Token"] != "ok-val" {
		t.Fatalf("unexpected sanitized headers: %+v", san)
	}

	// HTTP Method HEAD (200 empty body) and POST (405) on LoopbackStreamProxy
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v1"`)
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = w.Write([]byte("PAYLOAD"))
		}
	}))
	defer upstream.Close()

	// DisallowLoopback=true must reject 127.0.0.1 upstream
	if _, err := StartLoopbackStreamProxy(LoopbackProxyOptions{
		TargetURL:        upstream.URL + "/stream",
		AllowPrivateLAN:  true,
		DisallowLoopback: true,
	}); err == nil {
		t.Fatal("expected DisallowLoopback=true to reject 127.0.0.1 upstream")
	}

	proxy, err := StartLoopbackStreamProxy(LoopbackProxyOptions{
		TargetURL:       upstream.URL + "/stream",
		AllowPrivateLAN: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()

	headResp, err := http.Head(proxy.URL())
	if err != nil {
		t.Fatal(err)
	}
	_ = headResp.Body.Close()
	if headResp.StatusCode != http.StatusOK || headResp.Header.Get("ETag") != `"v1"` {
		t.Fatalf("unexpected HEAD response: %d etag=%q", headResp.StatusCode, headResp.Header.Get("ETag"))
	}

	postResp, err := http.Post(proxy.URL(), "text/plain", strings.NewReader("x"))
	if err != nil {
		t.Fatal(err)
	}
	_ = postResp.Body.Close()
	if postResp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for POST to loopback proxy, got %d", postResp.StatusCode)
	}

	// Direct newPinnedSafeStreamClient DialContext & CheckRedirect branches
	safeClient, err := newPinnedSafeStreamClient("127.0.0.1", true, false, nil)
	if err != nil {
		t.Fatalf("newPinnedSafeStreamClient: %v", err)
	}
	tr, ok := safeClient.Transport.(*http.Transport)
	if !ok || tr.DialContext == nil {
		t.Fatal("expected *http.Transport with DialContext")
	}
	if _, err := tr.DialContext(ctx, "tcp", "missing-port-addr"); err == nil {
		t.Fatal("expected error for dial addr without port")
	}
	if _, err := tr.DialContext(ctx, "tcp", "127.0.0.1:1"); err == nil {
		t.Fatal("expected dial error on closed port 1")
	}
	if safeClient.CheckRedirect != nil {
		if err := safeClient.CheckRedirect(&http.Request{URL: nil}, nil); err == nil {
			t.Fatal("expected error when redirect target URL is nil")
		}
		prevReq, _ := http.NewRequest(http.MethodGet, "http://example.com/a", nil)
		nextReq, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:8080/b", nil)
		nextReq.Header.Set("Referer", "http://example.com/a")
		if err := safeClient.CheckRedirect(nextReq, []*http.Request{prevReq}); err == nil {
			t.Fatal("expected cross-host redirect to loopback to be blocked")
		}
		if nextReq.Header.Get("Referer") != "" {
			t.Fatal("expected Referer stripped on cross-host redirect")
		}
	}
}

func TestV0110_ManagerStreamSlotsMP4InitHWSWFallbackCacheEvictAndMediaTasks(t *testing.T) {
	// 1. isMP4InitComplete binary box parser tests (32-bit, 64-bit extended size, incomplete/corrupt)
	mp4Dir := t.TempDir()
	if isMP4InitComplete(filepath.Join(mp4Dir, "missing.mp4")) || isMP4InitComplete(mp4Dir) {
		t.Fatal("expected false on missing file or directory")
	}
	shortPath := filepath.Join(mp4Dir, "short.mp4")
	_ = os.WriteFile(shortPath, []byte("short"), 0o600)
	if isMP4InitComplete(shortPath) {
		t.Fatal("expected false on <16 byte file")
	}

	ftypOnlyPath := filepath.Join(mp4Dir, "ftyp_only.mp4")
	_ = os.WriteFile(ftypOnlyPath, buildMP4Box("ftyp", []byte("isom0000"), false), 0o600)
	if isMP4InitComplete(ftypOnlyPath) {
		t.Fatal("expected false when moov box is missing")
	}

	zeroBoxPath := filepath.Join(mp4Dir, "zero_box.mp4")
	var zeroBuf bytes.Buffer
	zeroBuf.Write(buildMP4Box("ftyp", []byte("isom0000"), false))
	zeroBuf.Write([]byte{0, 0, 0, 0, 'm', 'o', 'o', 'v'})
	_ = os.WriteFile(zeroBoxPath, zeroBuf.Bytes(), 0o600)
	if isMP4InitComplete(zeroBoxPath) {
		t.Fatal("expected false when boxSize == 0")
	}

	validMP4Path := filepath.Join(mp4Dir, "valid_init.mp4")
	var validBuf bytes.Buffer
	validBuf.Write(buildMP4Box("ftyp", []byte("isom0000"), false))
	validBuf.Write(buildMP4Box("free", []byte("padding!"), true)) // 64-bit extended size box!
	validBuf.Write(buildMP4Box("moov", []byte("mvhd00001111"), false))
	_ = os.WriteFile(validMP4Path, validBuf.Bytes(), 0o600)
	if !isMP4InitComplete(validMP4Path) {
		t.Fatal("expected true for valid ftyp + 64-bit free + moov MP4 init file")
	}

	// 2. HasSession, ActiveSessionCount, TotalActiveTranscodes, TryAcquireStreamSlot, headersDiffer, Stage 2 Cache LRU Eviction
	tmp := t.TempDir()
	cfg := config.Default(tmp)
	mgr := NewManager(cfg,
		WithCapabilities(SoftwareOnlyCapabilities()),
		WithMaxCacheBytes(25), // tiny byte limit to trigger Stage 2 LRU session eviction
	)
	defer mgr.Close()

	if headersDiffer(map[string]string{"A": "1"}, map[string]string{"A": "1"}) {
		t.Fatal("expected identical headers not to differ")
	}
	if !headersDiffer(map[string]string{"A": "1"}, map[string]string{"A": "2"}) {
		t.Fatal("expected modified header value to differ")
	}

	snap1, err := mgr.RegisterSession(SessionParams{
		UserID: "u1", PlaySessionID: "ps-1", ItemID: "i1", MediaSourceID: "m1", InputPath: "/a.mkv",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !mgr.HasSession(snap1.Key) || mgr.ActiveSessionCount() != 1 || mgr.TotalActiveTranscodes() != 1 {
		t.Fatalf("unexpected session counts: has=%v active=%d total=%d", mgr.HasSession(snap1.Key), mgr.ActiveSessionCount(), mgr.TotalActiveTranscodes())
	}

	// Existing session key passes TryAcquireStreamSlot even when limit == 1
	relExist, ok := mgr.TryAcquireStreamSlot(1, snap1.Key)
	if !ok {
		t.Fatal("expected existing HLS session to pass TryAcquireStreamSlot at limit=1")
	}
	relExist()
	if _, ok := mgr.TryAcquireStreamSlot(1, "non-existent-session-key"); ok {
		t.Fatal("expected new session key to be rejected when limit=1 is reached")
	}

	// External stream slot reservation & idempotent release
	relExt, ok := mgr.TryAcquireStreamSlot(2, "")
	if !ok || mgr.TotalActiveTranscodes() != 2 {
		t.Fatalf("expected external slot acquired (total=2), ok=%v total=%d", ok, mgr.TotalActiveTranscodes())
	}
	if _, ok := mgr.TryAcquireStreamSlot(2, ""); ok {
		t.Fatal("expected third slot denied when limit=2")
	}
	relExt()
	relExt() // idempotent second call
	if mgr.TotalActiveTranscodes() != 1 {
		t.Fatalf("expected total=1 after release, got %d", mgr.TotalActiveTranscodes())
	}

	// Stage 2 EnforceCacheBytesLimit: create second newer session; write non-old files (seg_0.ts with lastReq=0) > 25 bytes
	time.Sleep(10 * time.Millisecond)
	snap2, err := mgr.RegisterSession(SessionParams{
		UserID: "u1", PlaySessionID: "ps-2", ItemID: "i2", MediaSourceID: "m2", InputPath: "/b.mkv",
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(snap1.WorkDir, "seg_0.ts"), []byte("01234567890123456789"), 0o600)
	_ = os.WriteFile(filepath.Join(snap2.WorkDir, "seg_0.ts"), []byte("01234567890123456789"), 0o600)
	mgr.EnforceCacheBytesLimit()
	if mgr.HasSession(snap1.Key) || !mgr.HasSession(snap2.Key) {
		t.Fatalf("expected older session snap1 evicted by Stage 2 cache limit and newer snap2 retained")
	}

	if runtime.GOOS == "windows" {
		return
	}

	// 3. HW->SW automatic fallback in waitForFile + GetInitSegment + extractRemoteEmbeddedSubtitle + local special-char staging
	hwTmp := t.TempDir()
	fakeHWFallback := filepath.Join(hwTmp, "fake-hw-sw-ff.sh")
	hwScript := fmt.Sprintf(`#!/bin/sh
encoder=""
initname="init.mp4"
segpat=""
prev=""
last=""
for arg in "$@"; do
  if [ "$prev" = "-c:v" ]; then encoder="$arg"; fi
  if [ "$prev" = "-hls_fmp4_init_filename" ]; then initname="$arg"; fi
  if [ "$prev" = "-hls_segment_filename" ]; then segpat="$arg"; fi
  prev="$arg"
  last="$arg"
done
if [ "$encoder" = "h264_nvenc" ]; then
  echo "nvenc init failed" >&2
  exit 1
fi
if [ -n "$segpat" ]; then
  dir=$(dirname "$segpat")
  printf 'SW_INIT_OK_0123456789' > "$dir/$initname"
  seg0=$(echo "$segpat" | sed "s/%%d/0/")
  printf 'SW_SEG_0_OK' > "$seg0"
  exit 0
fi
printf '1\n00:00:01,000 --> 00:00:02,000\nRemoteEmbedded\n' > "$last"
exit 0
`)
	if err := os.WriteFile(fakeHWFallback, []byte(hwScript), 0o755); err != nil {
		t.Fatal(err)
	}

	hwCfg := config.Default(hwTmp)
	hwCfg.FFmpegPath = fakeHWFallback
	hwMgr := NewManager(hwCfg, WithCapabilities(&HWCapabilities{
		FFmpegAvailable:      true,
		PreferredH264Encoder: "h264_nvenc",
	}))
	defer hwMgr.Close()

	fmp4Params := SessionParams{
		PlaySessionID:    "hw-fallback-fmp4",
		ItemID:           "it-hw",
		InputPath:        filepath.Join(hwTmp, "movie.mkv"),
		VideoCodec:       "h264",
		SegmentContainer: "mp4",
	}
	initPath, err := hwMgr.GetInitSegment(context.Background(), fmp4Params)
	if err != nil {
		t.Fatalf("expected HW->SW fallback to succeed on GetInitSegment, got %v", err)
	}
	initData, _ := os.ReadFile(initPath)
	if string(initData) != "SW_INIT_OK_0123456789" {
		t.Fatalf("unexpected init.mp4 content after HW->SW fallback: %q", string(initData))
	}

	// Remote embedded subtitle burn-in staging (extractRemoteEmbeddedSubtitle) + local special-char symlink staging
	remoteMediaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/x-matroska")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("FAKE-MKV-BYTES"))
	}))
	defer remoteMediaSrv.Close()

	subIdx := 2
	burnDir := t.TempDir()
	stagedRemote := stageBurnInFilesWithDeps(SessionParams{
		InputPath:           remoteMediaSrv.URL + "/film.mkv",
		SubtitleMethod:      "Encode",
		SubtitleStreamIndex: &subIdx,
		SubtitleCodec:       "subrip",
	}, burnDir, fakeHWFallback, nil)
	if !strings.HasPrefix(stagedRemote.SubtitlePath, burnDir) || !fileReady(stagedRemote.SubtitlePath) {
		t.Fatalf("expected remote embedded subtitle extracted into burnDir, got %+v", stagedRemote)
	}

	specialMedia := filepath.Join(hwTmp, "My Movie (2025) [1080p].weird_ext!")
	_ = os.WriteFile(specialMedia, []byte("MKV"), 0o600)
	localBurnDir := t.TempDir()
	stagedLocal := stageBurnInFiles(SessionParams{
		InputPath:           specialMedia,
		SubtitleMethod:      "Encode",
		SubtitleStreamIndex: &subIdx,
		SubtitleCodec:       "ass",
	}, localBurnDir)
	if !strings.HasPrefix(stagedLocal.InputPath, localBurnDir) || filepath.Ext(stagedLocal.InputPath) != ".mkv" {
		t.Fatalf("expected special-char local media symlinked to burn_input.mkv, got %q", stagedLocal.InputPath)
	}

	// 4. defaultProbeEncoder branches (vaapi, qsv, default)
	probeDummy := filepath.Join(hwTmp, "fake-probe-ok.sh")
	_ = os.WriteFile(probeDummy, []byte("#!/bin/sh\nexit 0\n"), 0o755)
	if !defaultProbeEncoder(context.Background(), probeDummy, "h264_vaapi", "") ||
		!defaultProbeEncoder(context.Background(), probeDummy, "h264_qsv", "") ||
		!defaultProbeEncoder(context.Background(), probeDummy, "h264_nvenc", "") {
		t.Fatal("expected defaultProbeEncoder to succeed across vaapi/qsv/default branches")
	}

	// 5. MediaTasks (ExtractAllChapterImages & GenerateAllBIFThumbnails) + BIF error/sidecar branches
	if _, err := ExtractAllChapterImages(context.Background(), nil, nil, func(float64) {}); err != nil {
		t.Fatal(err)
	}
	if _, err := GenerateAllBIFThumbnails(context.Background(), nil, nil, func(float64) {}); err != nil {
		t.Fatal(err)
	}
	if err := WriteBIF(filepath.Join(hwTmp, "empty.bif"), 10000, nil); err == nil {
		t.Fatal("expected error writing BIF with 0 frames")
	}
	badBIF := filepath.Join(hwTmp, "bad.bif")
	_ = os.WriteFile(badBIF, make([]byte, 80), 0o600)
	if _, err := ReadBIFMetadata(badBIF); err == nil {
		t.Fatal("expected error reading invalid BIF magic")
	}

	taskDB, err := sqlite.Open(filepath.Join(hwTmp, "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer taskDB.Close()
	repo := sqlite.NewLibraryRepo(taskDB)
	lib, _ := repo.CreateLibrary(context.Background(), "Movies", "movies", []string{hwTmp})

	localVid := filepath.Join(hwTmp, "local_vid.mp4")
	_ = os.WriteFile(localVid, []byte("VIDEO"), 0o644)
	sidecarVid := filepath.Join(hwTmp, "with_sidecar.mp4")
	_ = os.WriteFile(sidecarVid, []byte("VIDEO"), 0o644)
	_ = WriteBIF(filepath.Join(hwTmp, "with_sidecar.bif"), 10000, [][]byte{[]byte("JPEG1")})

	itLocal := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", MediaType: "Video", Name: "Local", Path: localVid,
		Extra: sqlite.ItemExtra{Chapters: []sqlite.ChapterRecord{{Name: "Ch1", StartPositionTicks: 0}}},
	}
	itSidecar := &sqlite.ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", MediaType: "Video", Name: "Sidecar", Path: sidecarVid}
	itRemote := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", MediaType: "Video", Name: "Remote", Path: "https://example.com/r.mp4",
		Extra: sqlite.ItemExtra{Chapters: []sqlite.ChapterRecord{{Name: "Ch1", StartPositionTicks: 0}}},
	}
	for _, it := range []*sqlite.ItemRecord{itLocal, itSidecar, itRemote} {
		_ = repo.UpsertItem(context.Background(), it)
	}

	// Fake ffmpeg for chapter + BIF extraction
	fakeTaskFF := filepath.Join(hwTmp, "fake-task-ff.sh")
	taskScript := `#!/bin/sh
last=""
for arg in "$@"; do last="$arg"; done
case "$last" in
  *%06d.jpg)
    dir=$(dirname "$last")
    printf 'JPG_FRAME_1' > "$dir/frame_000001.jpg"
    printf 'JPG_FRAME_2' > "$dir/frame_000002.jpg"
    exit 0
    ;;
  *)
    printf 'CHAPTER_JPG' > "$last"
    exit 0
    ;;
esac
`
	_ = os.WriteFile(fakeTaskFF, []byte(taskScript), 0o755)
	taskCfg := config.Default(hwTmp)
	taskCfg.FFmpegPath = fakeTaskFF
	ext := NewExtractor(taskCfg)

	chapSum, err := ExtractAllChapterImages(context.Background(), repo, ext, func(float64) {})
	if err != nil || chapSum.Extracted != 1 || chapSum.Skipped != 1 {
		t.Fatalf("unexpected ChapterTaskSummary: %+v err=%v", chapSum, err)
	}
	// Second run hits DB+disk cache skip branch
	chapSum2, err := ExtractAllChapterImages(context.Background(), repo, ext, nil)
	if err != nil || chapSum2.Skipped != 2 {
		t.Fatalf("expected 2 skipped chapters on second run, got %+v err=%v", chapSum2, err)
	}

	bifSum, err := GenerateAllBIFThumbnails(context.Background(), repo, ext, func(float64) {})
	if err != nil || bifSum.Generated != 1 || bifSum.Skipped != 2 {
		t.Fatalf("unexpected BIFTaskSummary: %+v err=%v", bifSum, err)
	}
	// Non-default width falls back to cached 320 BIF in FindExistingBIF
	if found := ext.FindExistingBIF("", itLocal.ID, 240); found == "" {
		t.Fatal("expected FindExistingBIF(width=240) to fall back to cached 320 BIF")
	}
	if p := NewExtractor(nil).BifCachePath("item-nil-cfg", 0); !strings.Contains(p, "index_320.bif") {
		t.Fatalf("unexpected nil-cfg BifCachePath: %q", p)
	}

	// Additional BIF & subtitle staging edge branches (writeBIFFromFiles limits, ReadBIFMetadata limits, failed tasks)
	emptyFrame := filepath.Join(hwTmp, "empty_frame.jpg")
	_ = os.WriteFile(emptyFrame, nil, 0o600)
	if err := writeBIFFromFiles(filepath.Join(hwTmp, "none.bif"), 0, []string{emptyFrame}, 0); err == nil {
		t.Fatal("expected error when all BIF frame files are empty")
	}
	nonEmptyFrame := filepath.Join(hwTmp, "big_frame.jpg")
	_ = os.WriteFile(nonEmptyFrame, []byte("0123456789ABCDEF"), 0o600)
	if err := writeBIFFromFiles(filepath.Join(hwTmp, "exceed.bif"), 0, []string{nonEmptyFrame}, 4); err == nil {
		t.Fatal("expected error when BIF frames exceed maxTotalBytes")
	}

	var hugeHdr [bifHeaderSize]byte
	copy(hugeHdr[0:8], bifMagic[:])
	binary.LittleEndian.PutUint32(hugeHdr[12:16], 200_000) // exceeds maxFrames safety limit
	hugeBIF := filepath.Join(hwTmp, "huge_count.bif")
	_ = os.WriteFile(hugeBIF, hugeHdr[:], 0o600)
	if _, err := ReadBIFMetadata(hugeBIF); err == nil {
		t.Fatal("expected error on huge BIF frame count")
	}

	if err := copyFileAtomic(filepath.Join(hwTmp, "no_such_src.srt"), filepath.Join(hwTmp, "dst.srt")); err == nil {
		t.Fatal("expected error from copyFileAtomic on missing source")
	}
	emptySubSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK) // 0-byte body
	}))
	defer emptySubSrv.Close()
	if err := downloadRemoteSubtitleAtomic(emptySubSrv.URL+"/empty.srt", filepath.Join(hwTmp, "empty_out.srt"), emptySubSrv.Client()); err == nil {
		t.Fatal("expected error on 0-byte remote subtitle response")
	}
	if err := downloadRemoteSubtitleAtomic("ftp://bad-scheme/sub.srt", filepath.Join(hwTmp, "bad_out.srt"), nil); err == nil {
		t.Fatal("expected error on disallowed URL scheme in downloadRemoteSubtitleAtomic")
	}
	failFF := filepath.Join(hwTmp, "fail-ff.sh")
	_ = os.WriteFile(failFF, []byte("#!/bin/sh\nexit 1\n"), 0o755)
	stagedFailEmb := stageBurnInFilesWithDeps(SessionParams{
		InputPath:           remoteMediaSrv.URL + "/film.mkv",
		SubtitleMethod:      "Encode",
		SubtitleStreamIndex: &subIdx,
		SubtitleCodec:       "srt",
	}, t.TempDir(), failFF, nil)
	if stagedFailEmb.SubtitleMethod != "" {
		t.Fatalf("expected failed remote embedded subtitle extraction to clear SubtitleMethod, got %q", stagedFailEmb.SubtitleMethod)
	}

	// Failed extraction branches in ExtractAllChapterImages & GenerateAllBIFThumbnails
	failTaskDB, _ := sqlite.Open(filepath.Join(hwTmp, "fail_tasks.db"))
	defer failTaskDB.Close()
	failRepo := sqlite.NewLibraryRepo(failTaskDB)
	failItem := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, Type: "Movie", MediaType: "Video", Name: "FailVid", Path: localVid,
		Extra: sqlite.ItemExtra{Chapters: []sqlite.ChapterRecord{{Name: "C0", StartPositionTicks: 0}}},
	}
	_ = failRepo.UpsertItem(context.Background(), failItem)
	failExt := NewExtractor(&config.Config{DataDir: t.TempDir(), FFmpegPath: failFF})
	if cs, err := ExtractAllChapterImages(context.Background(), failRepo, failExt, nil); err != nil || cs.Failed != 1 {
		t.Fatalf("expected 1 failed chapter extraction, got %+v err=%v", cs, err)
	}
	if bs, err := GenerateAllBIFThumbnails(context.Background(), failRepo, failExt, nil); err != nil || bs.Failed != 1 {
		t.Fatalf("expected 1 failed BIF generation, got %+v err=%v", bs, err)
	}
}
