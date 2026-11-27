package transcode

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestCovV0112_LoopbackProxyHelpersAndHTTPBranches(t *testing.T) {
	// nil proxy methods
	var nilProxy *LoopbackStreamProxy
	if nilProxy.URL() != "" {
		t.Fatal("expected empty URL on nil proxy")
	}
	if err := nilProxy.Close(); err != nil {
		t.Fatalf("expected nil Close error on nil proxy, got %v", err)
	}

	// StartLoopbackStreamProxy input validation errors
	for _, badTarget := range []string{"", "http://example.com/\x00bad", "://bad", "ftp://example.com/a.mp4", "http:///nohost"} {
		if _, err := StartLoopbackStreamProxy(LoopbackProxyOptions{TargetURL: badTarget}); err == nil {
			t.Fatalf("expected StartLoopbackStreamProxy(%q) to fail", badTarget)
		}
	}

	// effectiveURLPort & isSameOriginHost
	if got := effectiveURLPort(nil); got != "" {
		t.Fatalf("expected empty port for nil url, got %q", got)
	}
	uHTTPS, _ := url.Parse("https://EXAMPLE.com./live/master.m3u8")
	uHTTPSExplicit, _ := url.Parse("https://example.com:443/seg.ts")
	uHTTP, _ := url.Parse("http://example.com/seg.ts")
	uCustom, _ := url.Parse("http://example.com:8080/seg.ts")
	uFTP, _ := url.Parse("ftp://example.com/seg.ts")
	if effectiveURLPort(uHTTPS) != "443" || effectiveURLPort(uHTTP) != "80" || effectiveURLPort(uCustom) != "8080" || effectiveURLPort(uFTP) != "" {
		t.Fatal("unexpected effectiveURLPort results")
	}
	if isSameOriginHost(nil, uHTTPS) || !isSameOriginHost(uHTTPS, uHTTPSExplicit) || isSameOriginHost(uHTTPS, uHTTP) {
		t.Fatal("unexpected isSameOriginHost results")
	}

	// decodeBase64URLParam & verifyLoopbackSubSig
	padded := base64.URLEncoding.EncodeToString([]byte("http://example.com/a"))
	if dec, err := decodeBase64URLParam(padded); err != nil || string(dec) != "http://example.com/a" {
		t.Fatalf("expected padded URLEncoding decoded, got (%q, %v)", string(dec), err)
	}
	if _, err := decodeBase64URLParam("%%%invalid%%%"); err == nil {
		t.Fatal("expected invalid base64 error")
	}
	if verifyLoopbackSubSig([]byte("key"), "abc", "not-hex!!") {
		t.Fatal("expected invalid hex signature rejected")
	}

	// resolveHLSReferenceURI & rewriteHLSManifest & rewriteHLSTagURIs
	for _, badRef := range []string{"", "a\x00b", "://bad", "file:///etc/passwd", "http:///missinghost"} {
		if _, err := resolveHLSReferenceURI(uHTTPS, badRef); err == nil {
			t.Fatalf("expected resolveHLSReferenceURI(%q) to fail", badRef)
		}
	}
	if _, err := resolveHLSReferenceURI(uFTP, "seg.ts"); err == nil {
		t.Fatal("expected non-http baseURL scheme rejected")
	}
	if _, err := rewriteHLSManifest([]byte("#EXTM3U\n"), nil, "http://127.0.0.1:9999", []byte("k")); err == nil {
		t.Fatal("expected nil baseURL error in rewriteHLSManifest")
	}
	rawM3U8 := "\xef\xbb\xbf#EXTM3U\r\n\r\n#EXT-X-VERSION:7\r\n#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\"\r\n#EXT-X-MAP:URI=init.mp4\r\nseg0.ts\r\n"
	rewritten, err := rewriteHLSManifest([]byte(rawM3U8), uHTTPS, "http://127.0.0.1:1234", []byte("secret-key"))
	if err != nil || strings.Count(string(rewritten), "/sub?u=") != 3 {
		t.Fatalf("expected 3 rewritten URIs in manifest, got err=%v body=%s", err, string(rewritten))
	}
	for _, badManifest := range []string{
		"#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"file:///etc/passwd\",URI=\"ok.bin\"\n",
		"#EXTM3U\n#EXT-X-MAP:URI=file:///etc/passwd\n",
		"#EXTM3U\nfile:///etc/passwd\n",
	} {
		if _, err := rewriteHLSManifest([]byte(badManifest), uHTTPS, "http://127.0.0.1:1234", []byte("k")); err == nil {
			t.Fatalf("expected rewriteHLSManifest to fail on %q", badManifest)
		}
	}

	// Manifest content sniffing helpers
	if !isHLSManifestURLOrContentType(nil, "application/vnd.apple.mpegurl") {
		t.Fatal("expected mpegurl content-type detected")
	}
	if !isHLSManifestPayload(uHTTPS, "", []byte("#EXT-X-TARGETDURATION:6\n")) {
		t.Fatal("expected .m3u8 URL starting with #EXT detected as HLS")
	}
	if isHLSManifestPayload(uHTTP, "video/mp2t", []byte{0x47, 0xff, 0x00, 0x10}) {
		t.Fatal("expected MPEG-TS sync byte not detected as HLS")
	}

	// newSafeSubResourceClient CheckRedirect
	subClient := newSafeSubResourceClient(true)
	prevReq, _ := http.NewRequest(http.MethodGet, "http://192.168.1.10/a.ts", nil)
	if err := subClient.CheckRedirect(&http.Request{URL: nil}, []*http.Request{prevReq}); err == nil {
		t.Fatal("expected nil redirect URL rejected")
	}
	crossLANReq, _ := http.NewRequest(http.MethodGet, "http://10.1.2.3:8080/secret", nil)
	crossLANReq.Header.Set("Referer", "http://192.168.1.10/")
	if err := subClient.CheckRedirect(crossLANReq, []*http.Request{prevReq}); err == nil {
		t.Fatal("expected cross-host private LAN redirect blocked")
	}
	if crossLANReq.Header.Get("Referer") != "" {
		t.Fatal("expected Referer stripped on cross-host redirect")
	}
	crossPubReq, _ := http.NewRequest(http.MethodGet, "http://93.184.216.34/seg.ts", nil)
	crossPubReq.Header.Set("Referer", "http://192.168.1.10/")
	if err := subClient.CheckRedirect(crossPubReq, []*http.Request{prevReq}); err != nil {
		t.Fatalf("expected cross-host public IP redirect allowed, got %v", err)
	}

	// resolveAndPinHost branches
	ctx := context.Background()
	if _, err := resolveAndPinHost(ctx, "127.0.0.1", false, false); err == nil {
		t.Fatal("expected loopback IP blocked when allowLoopback=false")
	}
	if ips, err := resolveAndPinHost(ctx, "127.0.0.1", false, true); err != nil || len(ips) != 1 {
		t.Fatalf("expected loopback IP allowed when allowLoopback=true, got (%v, %v)", ips, err)
	}
	if _, err := resolveAndPinHost(ctx, "10.1.2.3", false, false); err == nil {
		t.Fatal("expected RFC1918 IP blocked when allowPrivateLAN=false")
	}
	if ips, err := resolveAndPinHost(ctx, "10.1.2.3", true, false); err != nil || len(ips) != 1 {
		t.Fatalf("expected RFC1918 IP allowed when allowPrivateLAN=true, got (%v, %v)", ips, err)
	}
	if _, err := resolveAndPinHost(ctx, "sub.localhost.", false, false); err == nil {
		t.Fatal("expected .localhost hostname blocked when allowLoopback=false")
	}

	// sanitizeUpstreamHeaders & safeURLMediaSuffix
	if sanitizeUpstreamHeaders(nil) != nil {
		t.Fatal("expected nil headers for empty input")
	}
	hdrs := sanitizeUpstreamHeaders(map[string]string{
		"Host":          "evil.example",
		"Bad\r\nHeader": "val",
		"X-Custom":      " token123 ",
		"Empty":         "   ",
	})
	if len(hdrs) != 1 || hdrs["X-Custom"] != "token123" {
		t.Fatalf("unexpected sanitized headers: %v", hdrs)
	}
	if safeURLMediaSuffix("/path/video.mkv") != "/media.mkv" || safeURLMediaSuffix("/path/unknown.bin") != "" {
		t.Fatal("unexpected safeURLMediaSuffix result")
	}

	// Live HTTP proxy server tests: HEAD, POST 405, SingleUse, oversized manifest, invalid manifest, /sub validation
	mode := "manifest_ok"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch mode {
		case "manifest_ok":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			w.Header().Set("Content-Length", "999")
			w.Header().Set("ETag", `"v1"`)
			if r.Method == http.MethodHead {
				w.WriteHeader(http.StatusOK)
				return
			}
			_, _ = w.Write([]byte("#EXTM3U\nseg0.ts\n"))
		case "manifest_huge":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write([]byte("#EXTM3U\n"))
			_, _ = w.Write(bytes.Repeat([]byte("a"), maxHLSManifestBytes+16))
		case "manifest_bad":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write([]byte("#EXTM3U\nfile:///etc/passwd\n"))
		case "manifest_sub":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write([]byte("#EXTM3U\n/seg0.ts\nhttp://127.0.0.1:1/blocked.ts\n"))
		case "binary":
			w.Header()["Content-Type"] = nil
			w.Header().Set("ETag", `"bin1"`)
			w.WriteHeader(http.StatusOK)
			if r.Method == http.MethodHead {
				return
			}
			_, _ = w.Write([]byte("raw-ts-bytes"))
		}
		if r.URL.Path == "/seg0.ts" {
			w.Header().Set("Content-Type", "video/mp2t")
			_, _ = w.Write([]byte("sub-seg-bytes"))
		}
	}))
	defer upstream.Close()

	proxy, err := StartLoopbackStreamProxy(LoopbackProxyOptions{
		TargetURL: upstream.URL + "/live.m3u8",
		Headers:   map[string]string{"X-Token": "abc"},
		Client:    upstream.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()

	// POST -> 405
	postReq, _ := http.NewRequest(http.MethodPost, proxy.URL(), nil)
	if resp, err := http.DefaultClient.Do(postReq); err != nil || resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 on POST, got %v err=%v", resp, err)
	}
	// Wrong path -> 403
	parsedProxy, _ := url.Parse(proxy.URL())
	proxyOrigin := parsedProxy.Scheme + "://" + parsedProxy.Host
	if resp, err := http.Get(proxyOrigin + "/wrong"); err != nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 on wrong path, got %v err=%v", resp, err)
	}
	// HEAD on manifest
	headReq, _ := http.NewRequest(http.MethodHead, proxy.URL(), nil)
	if resp, err := http.DefaultClient.Do(headReq); err != nil || resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "application/vnd.apple.mpegurl" {
		t.Fatalf("expected 200 mpegurl on HEAD manifest, got %v err=%v", resp, err)
	}
	// Signed /sub same-host 200 + cross-origin loopback 403
	mode = "manifest_sub"
	if mResp, err := http.Get(proxy.URL()); err != nil || mResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on manifest_sub, got %v err=%v", mResp, err)
	} else {
		var subURLs []string
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(mResp.Body)
		_ = mResp.Body.Close()
		for _, ln := range strings.Split(buf.String(), "\n") {
			ln = strings.TrimSpace(ln)
			if strings.Contains(ln, "/sub?u=") {
				subURLs = append(subURLs, ln)
			}
		}
		if len(subURLs) != 2 {
			t.Fatalf("expected 2 signed /sub URLs, got %v", subURLs)
		}
		if rSub1, sErr := http.Get(subURLs[0]); sErr != nil || rSub1.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 on same-host /sub, got %v err=%v", rSub1, sErr)
		} else {
			_ = rSub1.Body.Close()
		}
		if rSub2, sErr := http.Get(subURLs[1]); sErr != nil || rSub2.StatusCode != http.StatusForbidden {
			t.Fatalf("expected 403 on cross-origin loopback /sub, got %v err=%v", rSub2, sErr)
		} else {
			_ = rSub2.Body.Close()
		}
	}
	// HEAD on binary without Content-Type -> application/octet-stream
	mode = "binary"
	if resp, err := http.DefaultClient.Do(headReq); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on HEAD, got %v err=%v", resp, err)
	}
	// Pinned safe stream client path (Client == nil)
	pinnedProxy, err := StartLoopbackStreamProxy(LoopbackProxyOptions{
		TargetURL:        upstream.URL + "/video.ts",
		AllowPrivateLAN:  true,
		DisallowLoopback: false,
	})
	if err != nil {
		t.Fatalf("StartLoopbackStreamProxy pinned failed: %v", err)
	}
	defer pinnedProxy.Close()
	if resp, err := http.Get(pinnedProxy.URL()); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 via pinned safe stream client, got %v err=%v", resp, err)
	} else {
		_ = resp.Body.Close()
	}
	// Oversized manifest -> 502
	mode = "manifest_huge"
	if resp, err := http.Get(proxy.URL()); err != nil || resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502 on huge manifest, got %v err=%v", resp, err)
	}
	// Invalid manifest -> 502
	mode = "manifest_bad"
	if resp, err := http.Get(proxy.URL()); err != nil || resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502 on invalid manifest, got %v err=%v", resp, err)
	}
	// Binary HEAD & GET without upstream Content-Type -> application/octet-stream
	binProxy, err := StartLoopbackStreamProxy(LoopbackProxyOptions{
		TargetURL: upstream.URL + "/video.ts",
		SingleUse: true,
		Client:    upstream.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer binProxy.Close()
	mode = "binary"
	if resp, err := http.Get(binProxy.URL()); err != nil || resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("expected 200 octet-stream on first SingleUse GET, got %v err=%v", resp, err)
	}
	// Second request on SingleUse -> 403
	if resp, err := http.Get(binProxy.URL()); err != nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 on second SingleUse GET, got %v err=%v", resp, err)
	}
	// /sub missing params -> 403
	if resp, err := http.Get(proxyOrigin + "/sub"); err != nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 on /sub without params, got %v err=%v", resp, err)
	}

	// Manager pure/file helpers
	if formatFFmpegSeekSeconds(12.0) != "12" || formatFFmpegSeekSeconds(12.345) != "12.345" {
		t.Fatal("unexpected formatFFmpegSeekSeconds")
	}
	if headersDiffer(map[string]string{"A": "1"}, map[string]string{"A": "1"}) ||
		!headersDiffer(map[string]string{"A": "1"}, map[string]string{"A": "2"}) ||
		!headersDiffer(map[string]string{"A": "1"}, nil) {
		t.Fatal("unexpected headersDiffer")
	}
	if safeMediaExt("video.mp4") != ".mp4" || safeMediaExt("video.bad_ext!") != ".mkv" || safeMediaExt("noext") != ".mkv" {
		t.Fatal("unexpected safeMediaExt")
	}
	_ = suspendProcess(nil)
	_ = resumeProcess(nil)
	cpTmp := t.TempDir()
	srcFile := filepath.Join(cpTmp, "src.srt")
	_ = os.WriteFile(srcFile, []byte("1\n00:00:01,000 --> 00:00:02,000\nHi\n"), 0o644)
	if copyFileAtomic(filepath.Join(cpTmp, "missing.srt"), filepath.Join(cpTmp, "out.srt")) == nil {
		t.Fatal("expected missing src error")
	}
	if copyFileAtomic(srcFile, filepath.Join(cpTmp, "nodir", "out.srt")) == nil {
		t.Fatal("expected missing dst dir error")
	}
	if err := copyFileAtomic(srcFile, filepath.Join(cpTmp, "out.srt")); err != nil {
		t.Fatalf("copyFileAtomic failed: %v", err)
	}
}

func TestCovV0112_EncodingOptionsPlaylistSlotsAndBIF(t *testing.T) {
	// EncodingOptions & pure helpers
	eo := DefaultEncodingOptions()
	if !eo.isHardwareAllowed() {
		t.Fatal("expected default hardware allowed")
	}
	eo.HardwareAccelerationMode = "none"
	if eo.isHardwareAllowed() {
		t.Fatal("expected mode=none to disable hardware")
	}
	eo.HardwareAccelerationMode = "1"
	eo.EnableHardwareEncoding = false
	if eo.isHardwareAllowed() {
		t.Fatal("expected EnableHardwareEncoding=false to disable hardware")
	}
	if normalizeX264Preset("SLOW") != "slow" || normalizeX264Preset("bogus") != "veryfast" {
		t.Fatal("unexpected normalizeX264Preset")
	}
	if normalizeTonemapAlgorithm("BT2390") != "bt2390" || normalizeTonemapAlgorithm("bogus") != "hable" {
		t.Fatal("unexpected normalizeTonemapAlgorithm")
	}
	if f := buildSoftwareTonemapFilter("", math.NaN(), ""); !strings.Contains(f, "npl=100") || !strings.Contains(f, "format=yuv420p") {
		t.Fatalf("unexpected software tonemap filter: %q", f)
	}

	// anyTo* type conversions
	if anyToString("s", "d") != "s" || anyToString(float64(12), "d") != "12" || anyToString(7, "d") != "7" ||
		anyToString(int64(9), "d") != "9" || anyToString(true, "d") != "true" || anyToString(false, "d") != "false" ||
		anyToString(struct{}{}, "def") != "def" {
		t.Fatal("unexpected anyToString results")
	}
	if !anyToBool(true, false) || !anyToBool(float64(1), false) || !anyToBool(1, false) ||
		!anyToBool("yes", false) || anyToBool("off", true) || !anyToBool(struct{}{}, true) {
		t.Fatal("unexpected anyToBool results")
	}
	if v, ok := anyToInt(float64(5)); !ok || v != 5 {
		t.Fatal("expected float64 -> int")
	}
	if v, ok := anyToInt(int64(8)); !ok || v != 8 {
		t.Fatal("expected int64 -> int")
	}
	if v, ok := anyToInt("42"); !ok || v != 42 {
		t.Fatal("expected string -> int")
	}
	if _, ok := anyToInt("bad"); ok {
		t.Fatal("expected bad string -> !ok")
	}
	if v, ok := anyToFloat(10); !ok || v != 10 {
		t.Fatal("expected int -> float64")
	}
	if v, ok := anyToFloat(int64(11)); !ok || v != 11 {
		t.Fatal("expected int64 -> float64")
	}
	if v, ok := anyToFloat("2.5"); !ok || v != 2.5 {
		t.Fatal("expected string -> float64")
	}
	if _, ok := anyToFloat("bad"); ok {
		t.Fatal("expected bad string -> !ok")
	}

	// nil Manager safety
	var nilMgr *Manager
	nilMgr.SetStore(nil)
	_ = nilMgr.EncodingOptionsSnapshot()
	nilMgr.ReloadEncodingOptions(nil)
	if nilMgr.CurrentTempDir() == "" {
		t.Fatal("expected non-empty fallback temp dir for nil manager")
	}
	if nilMgr.ActiveSessionCount() != 0 || nilMgr.TotalActiveTranscodes() != 0 {
		t.Fatal("expected 0 counts on nil manager")
	}
	if rel, ok := nilMgr.TryAcquireStreamSlot(1, "k"); !ok || rel == nil {
		t.Fatal("expected nil manager TryAcquireStreamSlot allowed")
	}

	// ValidateTranscodingTempPath
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "data")
	_ = os.MkdirAll(dataDir, 0o755)
	for _, bad := range []string{"", "/tmp/bad\x00path", "relative/dir", "/", "/etc/nginx", dataDir, tmp} {
		if _, err := ValidateTranscodingTempPath(bad, dataDir); err == nil {
			t.Fatalf("expected ValidateTranscodingTempPath(%q) to fail", bad)
		}
	}
	validTemp := filepath.Join(tmp, "custom-tc", "nested")
	if got, err := ValidateTranscodingTempPath(validTemp, dataDir); err != nil || got == "" {
		t.Fatalf("expected valid nested temp path accepted, got (%q, %v)", got, err)
	}

	// ReloadEncodingOptions from SQLite with throttleOverridden + tonemap + applyEncodingOptionsToParams
	cfg, err := config.Load(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	_, _ = store.Writer.ExecContext(ctx,
		`INSERT OR REPLACE INTO server_config(key, value_json, updated_at) VALUES
		 ('encoding_ffmpeg_options', ?, 1),
		 ('encoding_public_tonemap', '{"EnableToneMapping":false,"ToneMappingAlgorithm":"reinhard","ToneMappingPeak":150}', 1),
		 ('encoding_full_tonemap', 'not-valid-json', 1)`,
		`{"HardwareAccelerationMode":"0","EnableHardwareEncoding":false,"AllowHevcEncoding":false,"H264Crf":21,"H264Preset":"fast","TranscodingTempPath":"`+validTemp+`"}`,
	)

	mgr := NewManager(cfg, WithCapabilities(SoftwareOnlyCapabilities()), WithThrottleConfig(true, 90, 6))
	defer mgr.Close()
	mgr.SetStore(store)
	mgr.ReloadEncodingOptions(nil)

	snap := mgr.EncodingOptionsSnapshot()
	if snap.AllowHevcEncoding || snap.H264Crf != 21 || snap.H264Preset != "fast" || snap.EnableToneMapping || snap.ThrottleBufferSize != 90 {
		t.Fatalf("unexpected reloaded EncodingOptionsSnapshot: %+v", snap)
	}
	if !strings.HasPrefix(mgr.CurrentTempDir(), filepath.Join(tmp, "custom-tc")) {
		t.Fatalf("expected CurrentTempDir under custom-tc, got %q", mgr.CurrentTempDir())
	}
	applied := mgr.applyEncodingOptionsToParams(SessionParams{VideoCodec: "hevc"})
	if applied.VideoCodec != "h264" || applied.SoftwareCRF != 21 || applied.SoftwarePreset != "fast" || !applied.DisableToneMapping {
		t.Fatalf("unexpected applied SessionParams: %+v", applied)
	}

	// TryAcquireStreamSlot: session key re-entrancy, limit enforcement, and external stream slots
	rel1, ok1 := mgr.TryAcquireStreamSlot(2, "sess-A")
	rel1Dup, ok1Dup := mgr.TryAcquireStreamSlot(2, "sess-A") // same session key does not consume 2nd slot
	relExt, okExt := mgr.TryAcquireStreamSlot(2, "")         // consumes 2nd slot
	if !ok1 || !ok1Dup || !okExt {
		t.Fatalf("expected slots acquired: %v %v %v", ok1, ok1Dup, okExt)
	}
	if mgr.TotalActiveTranscodes() != 2 {
		t.Fatalf("expected TotalActiveTranscodes=2, got %d", mgr.TotalActiveTranscodes())
	}
	if _, okBlocked := mgr.TryAcquireStreamSlot(2, "sess-B"); okBlocked {
		t.Fatal("expected 3rd distinct stream blocked at limit=2")
	}
	if _, okBlockedExt := mgr.TryAcquireStreamSlot(2, ""); okBlockedExt {
		t.Fatal("expected external stream blocked at limit=2")
	}
	rel1Dup()
	rel1()
	relExt()
	if mgr.TotalActiveTranscodes() != 0 {
		t.Fatalf("expected TotalActiveTranscodes=0 after release, got %d", mgr.TotalActiveTranscodes())
	}

	// Playlist helpers: SegmentDurations, BuildMasterPlaylist, BuildSubtitlePlaylist, BuildMediaPlaylist, ParseSegmentID
	if SegmentDurations(math.NaN(), 0) != nil || SegmentDurations(-5, 6) != nil {
		t.Fatal("expected nil SegmentDurations for invalid duration")
	}
	capped := SegmentDurations(float64(maxPlaylistSegments+50)*6.0, 6)
	if len(capped) != maxPlaylistSegments {
		t.Fatalf("expected maxPlaylistSegments=%d, got %d", maxPlaylistSegments, len(capped))
	}
	master := BuildMasterPlaylist(PlaylistParams{
		SegmentContainer: "fmp4",
		Width:            1920,
		Height:           1080,
		VideoCodec:       "av1",
		AudioCodec:       "eac3",
		Query:            url.Values{"api_key": []string{"tok"}},
		Subtitles: []SubtitleTrackInfo{
			{Index: 2, Language: "", Title: "Forced \"Quote\"\n", IsDefault: true, IsForced: true, URI: "sub2.m3u8"},
			{Index: 3, Language: "", Title: "", URI: "sub3.m3u8"},
		},
	})
	if !strings.Contains(master, "#EXT-X-VERSION:7") || !strings.Contains(master, "av01.0.08M.08,ec-3") || !strings.Contains(master, "Track 3") {
		t.Fatalf("unexpected master playlist:\n%s", master)
	}
	for _, ac := range []string{"ac3", "mp3", "opus", "flac"} {
		_ = hlsCodecsAttr("hevc", ac)
	}
	if hlsCodecsAttr("none", "opus") != "opus" {
		t.Fatal("expected audio-only codec attr")
	}
	subPL := BuildSubtitlePlaylist(0, "0.vtt")
	if !strings.Contains(subPL, "#EXT-X-PLAYLIST-TYPE:VOD") {
		t.Fatalf("unexpected subtitle playlist: %s", subPL)
	}
	mediaPL := BuildMediaPlaylist(PlaylistParams{
		SegmentContainer: ".m4s",
		Query:            url.Values{"k": []string{"v"}},
	})
	if !strings.Contains(mediaPL, "#EXT-X-MAP:URI=\"hls1/main/init.mp4?k=v\"") {
		t.Fatalf("unexpected fMP4 media playlist:\n%s", mediaPL)
	}
	if idx, ext, err := ParseSegmentID("seg_14.M4S"); err != nil || idx != 14 || ext != "m4s" {
		t.Fatalf("unexpected ParseSegmentID: %d %s %v", idx, ext, err)
	}
	for _, badSeg := range []string{"nodot", ".ts", "12.", "-3.ts", "abc.ts"} {
		if _, _, err := ParseSegmentID(badSeg); err == nil {
			t.Fatalf("expected ParseSegmentID(%q) to fail", badSeg)
		}
	}

	// BIF WriteBIF & ReadBIFMetadata
	bifOut := filepath.Join(tmp, "bif-out", "movie-320.bif")
	if err := WriteBIF(bifOut, 0, nil); err == nil {
		t.Fatal("expected WriteBIF with 0 frames to fail")
	}
	if err := WriteBIF(bifOut, 0, [][]byte{[]byte("jpeg0"), []byte("jpeg1")}); err != nil {
		t.Fatalf("WriteBIF failed: %v", err)
	}
	meta, err := ReadBIFMetadata(bifOut)
	if err != nil || meta.ImageCount != 2 || meta.TimestampMultiplierMs != DefaultBIFIntervalSec*1000 {
		t.Fatalf("unexpected ReadBIFMetadata: %+v err=%v", meta, err)
	}
	if _, err := ReadBIFMetadata(filepath.Join(tmp, "missing.bif")); err == nil {
		t.Fatal("expected missing BIF error")
	}
	shortBIF := filepath.Join(tmp, "short.bif")
	_ = os.WriteFile(shortBIF, []byte("short"), 0o644)
	if _, err := ReadBIFMetadata(shortBIF); err == nil {
		t.Fatal("expected short BIF header error")
	}
	hugeCountBIF := filepath.Join(tmp, "huge.bif")
	var hugeHdr [bifHeaderSize]byte
	copy(hugeHdr[0:8], bifMagic[:])
	binary.LittleEndian.PutUint32(hugeHdr[12:16], 999_999)
	_ = os.WriteFile(hugeCountBIF, hugeHdr[:], 0o644)
	if _, err := ReadBIFMetadata(hugeCountBIF); err == nil {
		t.Fatal("expected huge BIF frame count rejected")
	}
}
