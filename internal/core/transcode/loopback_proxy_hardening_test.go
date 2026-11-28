package transcode

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLoopbackProxyHLSNewlinesWhitespaceAndXMLRejection(t *testing.T) {
	baseURL, _ := url.Parse("https://cdn.example.com/live/master.m3u8")

	// 1. Bare \r and \r\n line endings plus whitespace around '=' in URI = "..." must all be rewritten.
	raw := "#EXTM3U\r#EXT-X-KEY:METHOD=AES-128,URI = \"https://cdn.example.com/key.bin\"\r\n#EXT-X-MAP:URI = init.mp4\rseg0.ts\r"
	rewritten, err := rewriteHLSManifest([]byte(raw), baseURL, "http://127.0.0.1:9999", []byte("secret-key"))
	if err != nil {
		t.Fatalf("rewriteHLSManifest failed: %v", err)
	}
	out := string(rewritten)
	if strings.Contains(out, "\r") {
		t.Fatalf("expected bare CR normalized out, got %q", out)
	}
	if strings.Count(out, "http://127.0.0.1:9999/sub?u=") != 3 {
		t.Fatalf("expected 3 rewritten URIs, got:\n%s", out)
	}

	// 2. Fail closed if a malformed URI= attribute cannot be rewritten.
	if _, err := rewriteHLSManifest([]byte("#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI = \"\"\n"), baseURL, "http://127.0.0.1:9999", []byte("k")); err == nil {
		t.Fatal("expected error for empty URI attribute")
	}
	if _, err := rewriteHLSManifest([]byte("#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI = \"unclosed\n"), baseURL, "http://127.0.0.1:9999", []byte("k")); err == nil {
		t.Fatal("expected error for malformed unclosed URI attribute")
	}

	// 3. XML manifests (DASH MPD / SmoothStreaming, including UTF-8 and UTF-16 BOMs) rejected with 502.
	xmlPayloads := [][]byte{
		[]byte(`<?xml version="1.0"?><MPD><BaseURL>http://127.0.0.1/</BaseURL></MPD>`),
		[]byte("\xef\xbb\xbf   <MPD xmlns=\"urn:mpeg:dash:schema:mpd:2011\"></MPD>"),
		[]byte("  \t\n<SmoothStreamingMedia MajorVersion=\"2\"></SmoothStreamingMedia>"),
		// UTF-16 LE BOM + "<?xml"
		{0xFF, 0xFE, '<', 0, '?', 0, 'x', 0, 'm', 0, 'l', 0, ' ', 0},
		// UTF-16 BE BOM + "<MPD"
		{0xFE, 0xFF, 0, '<', 0, 'M', 0, 'P', 0, 'D', 0, '>'},
	}
	for i, payload := range xmlPayloads {
		up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(payload)
		}))
		proxy, err := StartLoopbackStreamProxy(LoopbackProxyOptions{
			TargetURL:       up.URL + "/manifest.bin",
			AllowPrivateLAN: true,
			Client:          up.Client(),
		})
		if err != nil {
			up.Close()
			t.Fatalf("case %d: StartLoopbackStreamProxy: %v", i, err)
		}
		resp, err := http.Get(proxy.URL())
		if err != nil {
			proxy.Close()
			up.Close()
			t.Fatalf("case %d: GET proxy: %v", i, err)
		}
		_, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
		proxy.Close()
		up.Close()
		if resp.StatusCode != http.StatusBadGateway {
			t.Fatalf("case %d: expected 502 Bad Gateway for XML manifest, got %d", i, resp.StatusCode)
		}
	}
}

func TestLoopbackProxySameOriginSchemeAndPostRedirectOrigin(t *testing.T) {
	uHTTPS, _ := url.Parse("https://media.example.com:8443/live.m3u8")
	uHTTP, _ := url.Parse("http://media.example.com:8443/live.m3u8")
	if isSameOriginHost(uHTTPS, uHTTP) {
		t.Fatal("expected isSameOriginHost to reject scheme mismatch on identical host:port")
	}

	// Cross-origin redirect to a manifest that points back at the initial target must NOT forward custom headers.
	var initialGotAuth bool
	var initialServerURL string
	var redirectSrv *httptest.Server

	initialSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/secret.ts" {
			if r.Header.Get("X-Secret-Token") != "" {
				initialGotAuth = true
			}
			_, _ = w.Write([]byte("SECRET"))
			return
		}
		http.Redirect(w, r, redirectSrv.URL+"/evil.m3u8", http.StatusFound)
	}))
	defer initialSrv.Close()
	initialServerURL = initialSrv.URL

	redirectSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:4.0,\n" + initialServerURL + "/secret.ts\n"))
	}))
	defer redirectSrv.Close()

	proxy, err := StartLoopbackStreamProxy(LoopbackProxyOptions{
		TargetURL:       initialSrv.URL + "/start.m3u8",
		Headers:         map[string]string{"X-Secret-Token": "top-secret"},
		AllowPrivateLAN: true,
		Client:          initialSrv.Client(),
	})
	if err != nil {
		t.Fatalf("StartLoopbackStreamProxy: %v", err)
	}
	defer proxy.Close()

	resp, err := http.Get(proxy.URL())
	if err != nil {
		t.Fatalf("GET manifest: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	var subURL string
	for _, ln := range strings.Split(string(body), "\n") {
		if strings.Contains(ln, "/sub?u=") {
			subURL = strings.TrimSpace(ln)
			break
		}
	}
	if subURL == "" {
		t.Fatalf("expected rewritten /sub URL, got:\n%s", string(body))
	}
	subResp, err := http.Get(subURL)
	if err != nil {
		t.Fatalf("GET sub: %v", err)
	}
	_, _ = io.ReadAll(subResp.Body)
	subResp.Body.Close()
	if initialGotAuth {
		t.Fatal("cross-origin redirected manifest must NOT forward custom headers back to initial target")
	}
}

func TestValidateTranscodingTempPathStrictDataDirAndSystemChecks(t *testing.T) {
	dataDir := t.TempDir()
	allowedDefault := filepath.Join(dataDir, "cache", "transcoding-temp")
	allowedDefaultSub := filepath.Join(allowedDefault, "worker-1")
	disallowedDataSub := filepath.Join(dataDir, "recordings")
	allowedExternal := filepath.Join(t.TempDir(), "custom-tc")

	if _, err := ValidateTranscodingTempPath(allowedDefault, dataDir); err != nil {
		t.Fatalf("expected default transcoding-temp to be allowed, got %v", err)
	}
	if _, err := ValidateTranscodingTempPath(allowedDefaultSub, dataDir); err != nil {
		t.Fatalf("expected subdir of default transcoding-temp to be allowed, got %v", err)
	}
	if _, err := ValidateTranscodingTempPath(allowedExternal, dataDir); err != nil {
		t.Fatalf("expected dedicated temp subdir to be allowed, got %v", err)
	}
	if _, err := ValidateTranscodingTempPath(disallowedDataSub, dataDir); err == nil {
		t.Fatal("expected arbitrary DataDir subdirectory to be rejected")
	}
	if _, err := ValidateTranscodingTempPath(os.TempDir(), dataDir); err == nil {
		t.Fatal("expected exact os.TempDir() to be rejected")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if _, err := ValidateTranscodingTempPath(home, dataDir); err == nil {
			t.Fatal("expected exact os.UserHomeDir() to be rejected")
		}
	}
}

func TestTranscodeSessionAtomicLastAccessLockFree(t *testing.T) {
	sess := &transcodeSession{}
	sess.lastAccess.Store(time.Now().UnixNano())

	// Hold s.mu while calling touch() and lastAccessTime() from another goroutine: must not deadlock or race.
	sess.mu.Lock()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			sess.touch()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_ = sess.lastAccessTime()
		}
	}()
	wg.Wait()
	sess.mu.Unlock()
}

func TestLoopbackStreamProxyRejectsXMLManifestBypasses(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
		body        string
	}{
		{
			name:        "comment_before_mpd",
			contentType: "application/octet-stream",
			body:        `<!-- leading comment padding --><MPD xmlns="urn:mpeg:dash:schema:mpd:2011"><BaseURL>http://127.0.0.1/</BaseURL></MPD>`,
		},
		{
			name:        "doctype_before_mpd",
			contentType: "application/octet-stream",
			body:        `<!DOCTYPE mpd SYSTEM "http://127.0.0.1/evil.dtd"><MPD></MPD>`,
		},
		{
			name:        "dash_xml_content_type",
			contentType: "application/dash+xml; charset=utf-8",
			body:        `not-even-xml-prefix-bytes-0123456789`,
		},
		{
			name:        "leading_whitespace_exceeding_sniff_window",
			contentType: "application/octet-stream",
			body:        strings.Repeat(" ", 600) + `<MPD xmlns="urn:mpeg:dash:schema:mpd:2011"></MPD>`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer upstream.Close()

			proxy, err := StartLoopbackStreamProxy(LoopbackProxyOptions{
				TargetURL:       upstream.URL + "/stream.bin",
				AllowPrivateLAN: true,
				Client:          upstream.Client(),
			})
			if err != nil {
				t.Fatalf("StartLoopbackStreamProxy: %v", err)
			}
			defer proxy.Close()

			resp, err := http.Get(proxy.URL())
			if err != nil {
				t.Fatalf("GET proxy: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadGateway {
				t.Fatalf("expected 502 Bad Gateway for %s, got %d", tc.name, resp.StatusCode)
			}
		})
	}
}

