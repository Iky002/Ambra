package scanner

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestProbeLoopbackProxyHelperProcess(t *testing.T) {
	if os.Getenv("AMBRA_FAKE_FFPROBE_PROXY_HELPER") != "1" {
		return
	}
	var targetURL string
	for i := len(os.Args) - 1; i >= 0; i-- {
		if strings.HasPrefix(os.Args[i], "http://") || strings.HasPrefix(os.Args[i], "https://") {
			targetURL = os.Args[i]
			break
		}
	}
	if targetURL == "" {
		os.Exit(2)
	}
	resp, err := http.Get(targetURL)
	if err != nil {
		os.Exit(3)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || len(body) == 0 {
		os.Exit(4)
	}
	if outLog := os.Getenv("AMBRA_FAKE_FFPROBE_BODY_LOG"); outLog != "" {
		_ = os.WriteFile(outLog, body, 0o600)
	}
	_, _ = fmt.Fprint(os.Stdout, `{"format":{"format_name":"mp4","duration":"60.0"},"streams":[{"index":0,"codec_type":"video","codec_name":"h264","width":1280,"height":720}]}`)
	os.Exit(0)
}

func TestProbeLoopbackStreamProxyBlocksRedirectSSRF(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script wrapper requires unix")
	}

	tmp := t.TempDir()
	argsLog := filepath.Join(tmp, "ffprobe_args.log")
	bodyLog := filepath.Join(tmp, "ffprobe_body.log")

	fakeProbe := filepath.Join(tmp, "fake-ffprobe-fetch.sh")
	script := fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
AMBRA_FAKE_FFPROBE_PROXY_HELPER=1 AMBRA_FAKE_FFPROBE_BODY_LOG=%q exec %q -test.run=^TestProbeLoopbackProxyHelperProcess$ -- "$@"
`, argsLog, bodyLog, os.Args[0])
	if err := os.WriteFile(fakeProbe, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect.mp4":
			http.Redirect(w, r, "http://169.254.169.254/latest/meta-data", http.StatusFound)
		case "/valid.mp4":
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte("FAKE-MP4-HEADER-BYTES"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	prober := NewProber(fakeProbe)

	// 1. 302 redirect to cloud metadata IP must be blocked by LoopbackStreamProxy before ffprobe receives media bytes.
	if res, err := prober.Probe(context.Background(), srv.URL+"/redirect.mp4"); err == nil || res != nil {
		t.Fatalf("expected 302 redirect to metadata endpoint to fail probing, got res=%+v err=%v", res, err)
	}
	if _, err := os.Stat(bodyLog); !os.IsNotExist(err) {
		t.Fatalf("expected ffprobe never to read upstream payload on blocked redirect, stat err=%v", err)
	}

	// 2. Direct 200 OK media stream succeeds through LoopbackStreamProxy with expected CLI flags.
	res, err := prober.Probe(context.Background(), srv.URL+"/valid.mp4")
	if err != nil || res == nil || res.Container != "mp4" {
		t.Fatalf("expected valid remote stream to probe via LoopbackStreamProxy, got res=%+v err=%v", res, err)
	}

	rawArgs, err := os.ReadFile(argsLog)
	if err != nil {
		t.Fatal(err)
	}
	argsStr := string(rawArgs)
	if !strings.Contains(argsStr, "-protocol_whitelist http,https,tcp,tls,crypto -follow_redirects 0 -rw_timeout 15000000") {
		t.Fatalf("expected contiguous whitelist, -follow_redirects 0, and -rw_timeout 15000000 in args:\n%s", argsStr)
	}
	if !strings.Contains(argsStr, "http://127.0.0.1:") || !strings.Contains(argsStr, "/stream/") {
		t.Fatalf("expected ffprobe target to be LoopbackStreamProxy URL, got:\n%s", argsStr)
	}
}
