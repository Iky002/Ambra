package transcode

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestAuditWave1_H06_LoopbackStreamProxyHLSManifestRewriteAndSSRF(t *testing.T) {
	// Secret local service simulating an internal loopback endpoint (e.g. 127.0.0.1:9999/secret).
	var secretHits atomic.Int32
	secretSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secretHits.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("TOP-SECRET-KEY"))
	}))
	defer secretSrv.Close()

	// Cross-origin CDN server to verify custom InputHeaders are NOT leaked cross-origin.
	var cdnAuthHeader atomic.Value
	cdnSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cdnAuthHeader.Store(r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "video/mp2t")
		_, _ = w.Write([]byte("CDN-SEG"))
	}))
	defer cdnSrv.Close()

	var originURL string
	var sawOriginAuth atomic.Int32
	originSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer strm-token-123" {
			sawOriginAuth.Add(1)
		}
		switch r.URL.Path {
		case "/evil.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			fmt.Fprintf(w, "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:6\n"+
				"#EXT-X-KEY:METHOD=AES-128,URI=%q\n"+
				"#EXT-X-MAP:URI=\"http://169.254.169.254/latest/meta-data/\"\n"+
				"#EXTINF:6.0,\nseg0.ts\n#EXT-X-ENDLIST\n", secretSrv.URL+"/secret")
		case "/file_scheme.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"file:///etc/passwd\"\n#EXTINF:6.0,\nseg0.ts\n"))
		case "/live/master.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=2000000\nvariant/index.m3u8\n"))
		case "/live/variant/index.m3u8":
			w.Header().Set("Content-Type", "application/x-mpegURL")
			fmt.Fprintf(w, "#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:4\n"+
				"#EXT-X-MAP:URI=\"init.mp4\"\n"+
				"#EXTINF:4.0,\nseg0.ts\n"+
				"#EXTINF:4.0,\n%s/live/seg1.ts\n"+
				"#EXT-X-ENDLIST\n", originURL)
		case "/live/variant/init.mp4":
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte("INIT-MP4-BYTES"))
		case "/live/variant/seg0.ts":
			w.Header().Set("Content-Type", "video/mp2t")
			_, _ = w.Write([]byte("SEG-ZERO-BYTES"))
		case "/live/seg1.ts":
			w.Header().Set("Content-Type", "video/mp2t")
			_, _ = w.Write([]byte("SEG-ONE-BYTES"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer originSrv.Close()
	originURL = originSrv.URL

	// (a) SSRF key/map URIs (127.0.0.1:<otherPort> and 169.254.169.254) are rewritten to /sub and blocked with 403!
	evilProxy, err := StartLoopbackStreamProxy(LoopbackProxyOptions{
		TargetURL: originSrv.URL + "/evil.m3u8",
		Client:    originSrv.Client(),
	})
	if err != nil {
		t.Fatalf("StartLoopbackStreamProxy(evil): %v", err)
	}
	defer evilProxy.Close()

	resp, err := http.Get(evilProxy.URL())
	if err != nil {
		t.Fatalf("GET evil manifest: %v", err)
	}
	evilBodyBytes, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 rewritten manifest, got %d (%s)", resp.StatusCode, string(evilBodyBytes))
	}
	evilBody := string(evilBodyBytes)
	if strings.Contains(evilBody, secretSrv.URL) || strings.Contains(evilBody, "169.254.169.254") {
		t.Fatalf("raw upstream SSRF URIs leaked in rewritten manifest:\n%s", evilBody)
	}

	var subURLs []string
	sc := bufio.NewScanner(strings.NewReader(evilBody))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if idx := strings.Index(line, `URI="`); idx >= 0 {
			rest := line[idx+5:]
			if end := strings.IndexByte(rest, '"'); end > 0 {
				subURLs = append(subURLs, rest[:end])
			}
		}
	}
	if len(subURLs) != 2 {
		t.Fatalf("expected 2 rewritten URI attributes (EXT-X-KEY & EXT-X-MAP), got %v\nmanifest:\n%s", subURLs, evilBody)
	}
	for _, su := range subURLs {
		subResp, sErr := http.Get(su)
		if sErr != nil {
			t.Fatalf("GET /sub %s: %v", su, sErr)
		}
		_, _ = io.ReadAll(subResp.Body)
		_ = subResp.Body.Close()
		if subResp.StatusCode != http.StatusForbidden {
			t.Fatalf("expected 403 blocking SSRF /sub URL %s, got %d", su, subResp.StatusCode)
		}
	}
	if secretHits.Load() != 0 {
		t.Fatalf("SSRF target server was contacted %d times!", secretHits.Load())
	}

	// Non-http(s) scheme (file:///etc/passwd) inside manifest is rejected immediately (502).
	fileProxy, err := StartLoopbackStreamProxy(LoopbackProxyOptions{
		TargetURL: originSrv.URL + "/file_scheme.m3u8",
		Client:    originSrv.Client(),
	})
	if err != nil {
		t.Fatalf("StartLoopbackStreamProxy(file_scheme): %v", err)
	}
	defer fileProxy.Close()
	fileResp, err := http.Get(fileProxy.URL())
	if err != nil {
		t.Fatalf("GET file_scheme: %v", err)
	}
	_, _ = io.ReadAll(fileResp.Body)
	_ = fileResp.Body.Close()
	if fileResp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502 rejecting file:// URI in manifest, got %d", fileResp.StatusCode)
	}

	// (b) Legitimate multi-segment master -> variant -> init.mp4 + seg0.ts + seg1.ts with custom InputHeaders!
	goodProxy, err := StartLoopbackStreamProxy(LoopbackProxyOptions{
		TargetURL: originSrv.URL + "/live/master.m3u8",
		Headers:   map[string]string{"Authorization": "Bearer strm-token-123"},
		Client:    originSrv.Client(),
	})
	if err != nil {
		t.Fatalf("StartLoopbackStreamProxy(good): %v", err)
	}
	defer goodProxy.Close()

	masterResp, err := http.Get(goodProxy.URL())
	if err != nil {
		t.Fatalf("GET master.m3u8: %v", err)
	}
	masterBytes, _ := io.ReadAll(masterResp.Body)
	_ = masterResp.Body.Close()
	if masterResp.StatusCode != http.StatusOK {
		t.Fatalf("master status=%d", masterResp.StatusCode)
	}
	var variantSubURL string
	for _, line := range strings.Split(string(masterBytes), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "http://127.0.0.1:") {
			variantSubURL = line
			break
		}
	}
	if variantSubURL == "" {
		t.Fatalf("variant sub-manifest URL not rewritten in master:\n%s", string(masterBytes))
	}

	varResp, err := http.Get(variantSubURL)
	if err != nil {
		t.Fatalf("GET variant index.m3u8: %v", err)
	}
	varBytes, _ := io.ReadAll(varResp.Body)
	_ = varResp.Body.Close()
	if varResp.StatusCode != http.StatusOK {
		t.Fatalf("variant status=%d body=%s", varResp.StatusCode, string(varBytes))
	}

	var mapURL string
	var segURLs []string
	for _, line := range strings.Split(string(varBytes), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#EXT-X-MAP:") {
			if idx := strings.Index(line, `URI="`); idx >= 0 {
				rest := line[idx+5:]
				if end := strings.IndexByte(rest, '"'); end > 0 {
					mapURL = rest[:end]
				}
			}
		} else if strings.HasPrefix(line, "http://127.0.0.1:") {
			segURLs = append(segURLs, line)
		}
	}
	if mapURL == "" || len(segURLs) != 2 {
		t.Fatalf("expected rewritten init map + 2 segments, got map=%q segs=%v\n%s", mapURL, segURLs, string(varBytes))
	}

	mustFetchBody := func(u string) string {
		t.Helper()
		r, err := http.Get(u)
		if err != nil {
			t.Fatalf("GET %s: %v", u, err)
		}
		b, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		if r.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", u, r.StatusCode, string(b))
		}
		return string(b)
	}
	if got := mustFetchBody(mapURL); got != "INIT-MP4-BYTES" {
		t.Fatalf("init.mp4 body=%q", got)
	}
	if got := mustFetchBody(segURLs[0]); got != "SEG-ZERO-BYTES" {
		t.Fatalf("seg0 body=%q", got)
	}
	if got := mustFetchBody(segURLs[1]); got != "SEG-ONE-BYTES" {
		t.Fatalf("seg1 body=%q", got)
	}
	if sawOriginAuth.Load() < 5 {
		t.Fatalf("expected custom Authorization header on all 5 same-origin requests, got %d", sawOriginAuth.Load())
	}
}

func TestAuditWave1_H07_L04_L05_L07_ClampsSlotsTempPathFontsAndSeek(t *testing.T) {
	// 1. H-07: VideoBitrate & AudioBitrate clamps prevent 32-bit INT_MAX overflow in -bufsize (vBitrate*2).
	args := BuildFFmpegArgs(SessionParams{
		InputPath:       "/media/input.mkv",
		VideoCodec:      "h264",
		AudioCodec:      "aac",
		VideoBitrate:    2_000_000_000, // 2 Gbps -> clamped to 200 Mbps (bufsize 400 Mbps)
		AudioBitrate:    9_000_000,     // -> clamped to 1.536 Mbps
		AudioChannels:   99,
		MaxWidth:        999999,
		MaxHeight:       999999,
		SegmentDuration: 999,
		StartTimeTicks:  15 * TicksPerSecond,
	}, 0, "/tmp/out", SoftwareOnlyCapabilities())

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-b:v 200000000") || !strings.Contains(joined, "-bufsize 400000000") {
		t.Fatalf("expected clamped VideoBitrate 200000000 and bufsize 400000000, got: %s", joined)
	}
	if !strings.Contains(joined, "-b:a 1536000") {
		t.Fatalf("expected clamped AudioBitrate 1536000, got: %s", joined)
	}
	if !strings.Contains(joined, "-ss 15 ") || !strings.Contains(joined, "-output_ts_offset 15 ") {
		t.Fatalf("expected -ss 15 and -output_ts_offset 15 from StartTimeTicks on segIndex=0, got: %s", joined)
	}

	// 2. 3.1-6: BuildMasterPlaylist emits RFC 6381 fLaC for AudioOnly FLAC HLS.
	masterFLAC := BuildMasterPlaylist(PlaylistParams{
		ItemID:     "item1",
		PlaylistID: "main",
		VideoCodec: "none",
		AudioCodec: "flac",
		Bandwidth:  512_000,
	})
	if !strings.Contains(masterFLAC, `CODECS="fLaC"`) {
		t.Fatalf("expected CODECS=\"fLaC\" in audio FLAC master playlist, got:\n%s", masterFLAC)
	}

	// 3. L-04: Concurrent TryAcquireStreamSlot(1, ...) never allows >1 concurrent reservation.
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "data")
	cfg := config.Default(dataDir)
	mgr := NewManager(cfg, WithCapabilities(SoftwareOnlyCapabilities()))
	defer mgr.Close()

	const workers = 24
	var startGate sync.WaitGroup
	startGate.Add(1)
	var doneWG sync.WaitGroup
	var activeNow atomic.Int32
	var maxObserved atomic.Int32
	var acquiredTotal atomic.Int32

	for i := 0; i < workers; i++ {
		doneWG.Add(1)
		go func(idx int) {
			defer doneWG.Done()
			startGate.Wait()
			rel, ok := mgr.TryAcquireStreamSlot(1, fmt.Sprintf("u1:ps-%d", idx))
			if !ok {
				return
			}
			acquiredTotal.Add(1)
			cur := activeNow.Add(1)
			for {
				prev := maxObserved.Load()
				if cur <= prev || maxObserved.CompareAndSwap(prev, cur) {
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
			activeNow.Add(-1)
			rel()
		}(i)
	}
	startGate.Done()
	doneWG.Wait()
	if maxObserved.Load() != 1 || acquiredTotal.Load() != 1 {
		t.Fatalf("L-04 stream slot race: expected exactly 1 concurrent holder, got maxObserved=%d acquiredTotal=%d",
			maxObserved.Load(), acquiredTotal.Load())
	}

	// 4. L-05: ValidateTranscodingTempPath & CurrentTempDir reload.
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	symEtc := filepath.Join(tmp, "etc-link")
	_ = os.Symlink("/etc", symEtc)
	for _, bad := range []string{
		"relative/path",
		"/",
		"/etc",
		"/etc/ambra-temp",
		"/usr/local/tmp-ambra",
		symEtc,
		dataDir,
		tmp, // parent of dataDir
	} {
		if _, err := ValidateTranscodingTempPath(bad, dataDir); err == nil {
			t.Fatalf("expected ValidateTranscodingTempPath(%q) to fail", bad)
		}
	}
	dedicatedTemp := filepath.Join(tmp, "dedicated-transcode-temp")
	cleaned, err := ValidateTranscodingTempPath(dedicatedTemp, dataDir)
	if err != nil || cleaned == "" {
		t.Fatalf("expected dedicated temp dir to pass validation, got cleaned=%q err=%v", cleaned, err)
	}

	store, err := sqlite.Open(filepath.Join(dataDir, "tc_l05.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	rawOpt, _ := json.Marshal(map[string]any{"TranscodingTempPath": dedicatedTemp})
	_, err = store.Writer.ExecContext(context.Background(),
		`INSERT INTO server_config (key, value_json, updated_at) VALUES ('encoding_ffmpeg_options', ?, 1)`, string(rawOpt))
	if err != nil {
		t.Fatal(err)
	}
	mgr.SetStore(store)
	if got := mgr.CurrentTempDir(); got != cleaned {
		t.Fatalf("expected CurrentTempDir()=%q, got %q", cleaned, got)
	}

	// 5. L-07: ExtractFontsWithHeaders forwards custom STRM headers via LoopbackStreamProxy.
	lanIP := ""
	if addrs, aErr := net.InterfaceAddrs(); aErr == nil {
		for _, a := range addrs {
			if ipNet, ok := a.(*net.IPNet); ok && ipNet.IP != nil && !ipNet.IP.IsLoopback() {
				if v4 := ipNet.IP.To4(); v4 != nil {
					lanIP = v4.String()
					break
				}
			}
		}
	}
	if lanIP != "" {
		ln, lErr := net.Listen("tcp", lanIP+":0")
		if lErr != nil {
			t.Fatalf("listen LAN IP %s: %v", lanIP, lErr)
		}
		var fontSrvSawHeader atomic.Bool
		fontSrv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Strm-Token") == "secret-font-hdr" {
				fontSrvSawHeader.Store(true)
			}
			w.Header().Set("Content-Type", "video/x-matroska")
			_, _ = w.Write([]byte("FAKE-MKV"))
		})}
		go func() { _ = fontSrv.Serve(ln) }()
		defer fontSrv.Close()

		fakeFFprobe := filepath.Join(tmp, "fake-ffprobe.sh")
		probeScript := "#!/bin/sh\n" +
			"last=''\n" +
			"for arg in \"$@\"; do last=\"$arg\"; done\n" +
			"curl -fsS \"$last\" >/dev/null || exit 1\n" +
			"printf '{\"streams\":[{\"index\":3,\"codec_type\":\"attachment\",\"tags\":{\"filename\":\"TestFont.ttf\"}}]}\\n'\n"
		if err := os.WriteFile(fakeFFprobe, []byte(probeScript), 0o755); err != nil {
			t.Fatal(err)
		}
		fakeFFmpeg := filepath.Join(tmp, "fake-ffmpeg.sh")
		script := "#!/bin/sh\n" +
			"prev=''\ninput=''\n" +
			"for arg in \"$@\"; do\n" +
			"  if [ \"$prev\" = \"-i\" ]; then input=\"$arg\"; fi\n" +
			"  prev=\"$arg\"\n" +
			"done\n" +
			"curl -fsS \"$input\" >/dev/null || exit 1\n" +
			"printf 'TTF-DATA' > ./TestFont.ttf\n"
		if err := os.WriteFile(fakeFFmpeg, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
		cfg.FFprobePath = fakeFFprobe
		cfg.FFmpegPath = fakeFFmpeg
		ext := NewExtractor(cfg)
		fonts, err := ext.ExtractFontsWithHeaders(
			context.Background(),
			"http://"+ln.Addr().String()+"/anime.mkv",
			map[string]string{"X-Strm-Token": "secret-font-hdr"},
			"item-font-1",
			"ms-font-1",
		)
		if err != nil || len(fonts) != 1 || !fontSrvSawHeader.Load() {
			t.Fatalf("ExtractFontsWithHeaders failed: fonts=%v err=%v sawHeader=%v", fonts, err, fontSrvSawHeader.Load())
		}
	}
}

func TestLoopbackStreamProxyChunkedManifestSniffingAndSSRF(t *testing.T) {
	// Upstream server sends UTF-8 BOM, "#", and "EXTM3U..." in separate flushed chunks
	// on an extensionless path with application/octet-stream so only payload sniffing detects HLS.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("expected http.Flusher")
			return
		}
		_, _ = w.Write([]byte("\xef\xbb\xbf"))
		flusher.Flush()
		time.Sleep(5 * time.Millisecond)

		_, _ = w.Write([]byte("#"))
		flusher.Flush()
		time.Sleep(5 * time.Millisecond)

		_, _ = w.Write([]byte("EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:6\n" +
			"#EXT-X-KEY:METHOD=AES-128,URI=\"http://169.254.169.254/latest\"\n" +
			"#EXTINF:6.0,\nseg0.ts\n#EXT-X-ENDLIST\n"))
		flusher.Flush()
	}))
	defer upstream.Close()

	proxy, err := StartLoopbackStreamProxy(LoopbackProxyOptions{
		TargetURL: upstream.URL + "/stream",
		Client:    upstream.Client(),
	})
	if err != nil {
		t.Fatalf("StartLoopbackStreamProxy: %v", err)
	}
	defer proxy.Close()

	resp, err := http.Get(proxy.URL())
	if err != nil {
		t.Fatalf("GET proxy: %v", err)
	}
	bodyBytes, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 rewritten manifest, got %d (%s)", resp.StatusCode, string(bodyBytes))
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "mpegurl") {
		t.Fatalf("expected mpegurl content-type after chunked sniffing, got %q", ct)
	}
	body := string(bodyBytes)
	if strings.Contains(body, "169.254.169.254") {
		t.Fatalf("SSRF metadata URI leaked in unrewritten manifest:\n%s", body)
	}
	var keySubURL string
	sc := bufio.NewScanner(strings.NewReader(body))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "#EXT-X-KEY:") {
			if idx := strings.Index(line, `URI="`); idx >= 0 {
				rest := line[idx+5:]
				if end := strings.IndexByte(rest, '"'); end > 0 {
					keySubURL = rest[:end]
				}
			}
		}
	}
	if keySubURL == "" {
		t.Fatalf("expected rewritten /sub URL for #EXT-X-KEY, got manifest:\n%s", body)
	}
	subResp, err := http.Get(keySubURL)
	if err != nil {
		t.Fatalf("GET rewritten /sub key URL: %v", err)
	}
	_, _ = io.ReadAll(subResp.Body)
	_ = subResp.Body.Close()
	if subResp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 blocking SSRF metadata key URL, got %d", subResp.StatusCode)
	}
}

func TestLoopbackStreamProxyCrossHostRedirectSecurity(t *testing.T) {
	customHeaders := sanitizeUpstreamHeaders(map[string]string{
		"X-Custom-Cloud-Token": "cloud-secret-xyz",
		"User-Agent":           "AmbraCustomUA/1.0",
	})

	// (a) Verify X-Custom-Cloud-Token is sent to the initial host via LoopbackStreamProxy.
	var sawInitialToken atomic.Bool
	initialSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Custom-Cloud-Token") == "cloud-secret-xyz" {
			sawInitialToken.Store(true)
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("MEDIA-BYTES-PAYLOAD-0123456789"))
	}))
	defer initialSrv.Close()

	proxy, err := StartLoopbackStreamProxy(LoopbackProxyOptions{
		TargetURL: initialSrv.URL + "/video.mp4",
		Headers: map[string]string{
			"X-Custom-Cloud-Token": "cloud-secret-xyz",
			"User-Agent":           "AmbraCustomUA/1.0",
		},
		Client: initialSrv.Client(),
	})
	if err != nil {
		t.Fatalf("StartLoopbackStreamProxy: %v", err)
	}
	defer proxy.Close()

	resp, err := http.Get(proxy.URL())
	if err != nil {
		t.Fatalf("GET proxy: %v", err)
	}
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !sawInitialToken.Load() {
		t.Fatal("expected X-Custom-Cloud-Token to be sent to initial upstream host")
	}

	// (b) Verify both newPinnedSafeStreamClient and newSafeSubResourceClient:
	//     1. Strip custom upstream headers (except User-Agent) on cross-host redirect.
	//     2. Enforce strict public-only DialContext on cross-host dials (blocking private LAN & loopback
	//        even when allowPrivateLAN=true and disallowLoopback=false).
	ctx := context.Background()
	streamClient, err := newPinnedSafeStreamClient("192.168.1.10", true, false, customHeaders)
	if err != nil {
		t.Fatalf("newPinnedSafeStreamClient: %v", err)
	}
	subClient, err := newSafeSubResourceClient(ctx, "192.168.1.10", true, true, customHeaders)
	if err != nil {
		t.Fatalf("newSafeSubResourceClient: %v", err)
	}

	for name, c := range map[string]*http.Client{
		"pinnedStreamClient": streamClient,
		"subResourceClient":  subClient,
	} {
		// Same-host redirect preserves X-Custom-Cloud-Token.
		prevReq, _ := http.NewRequest(http.MethodGet, "http://192.168.1.10/start", nil)
		sameHostReq, _ := http.NewRequest(http.MethodGet, "http://192.168.1.10/next", nil)
		sameHostReq.Header.Set("X-Custom-Cloud-Token", "cloud-secret-xyz")
		sameHostReq.Header.Set("User-Agent", "AmbraCustomUA/1.0")
		if err := c.CheckRedirect(sameHostReq, []*http.Request{prevReq}); err != nil {
			t.Fatalf("%s: expected same-host redirect allowed, got %v", name, err)
		}
		if sameHostReq.Header.Get("X-Custom-Cloud-Token") != "cloud-secret-xyz" {
			t.Fatalf("%s: expected X-Custom-Cloud-Token preserved on same-host redirect", name)
		}

		// Cross-host redirect to public IP strips X-Custom-Cloud-Token while preserving User-Agent.
		crossReq, _ := http.NewRequest(http.MethodGet, "http://93.184.216.34/cdn/video.mp4", nil)
		crossReq.Header.Set("X-Custom-Cloud-Token", "cloud-secret-xyz")
		crossReq.Header.Set("User-Agent", "AmbraCustomUA/1.0")
		if err := c.CheckRedirect(crossReq, []*http.Request{prevReq}); err != nil {
			t.Fatalf("%s: expected cross-host public redirect allowed, got %v", name, err)
		}
		if crossReq.Header.Get("X-Custom-Cloud-Token") != "" {
			t.Fatalf("%s: expected X-Custom-Cloud-Token stripped on cross-host redirect, got %q", name, crossReq.Header.Get("X-Custom-Cloud-Token"))
		}
		if crossReq.Header.Get("User-Agent") != "AmbraCustomUA/1.0" {
			t.Fatalf("%s: expected User-Agent preserved on cross-host redirect, got %q", name, crossReq.Header.Get("User-Agent"))
		}

		// Cross-host DialContext must strictly reject private LAN & loopback even when allowPrivateLAN=true.
		tr, ok := c.Transport.(*http.Transport)
		if !ok || tr == nil || tr.DialContext == nil {
			t.Fatalf("%s: expected *http.Transport with DialContext", name)
		}
		if _, err := tr.DialContext(ctx, "tcp", "10.0.0.1:80"); err == nil {
			t.Fatalf("%s: expected cross-host private LAN dial 10.0.0.1:80 to be blocked", name)
		}
		if _, err := tr.DialContext(ctx, "tcp", "127.0.0.1:80"); err == nil {
			t.Fatalf("%s: expected cross-host loopback dial 127.0.0.1:80 to be blocked", name)
		}
	}
}
