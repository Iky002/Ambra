package netutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestFilterFFmpegArgsAndVersionSupport(t *testing.T) {
	// 1. Fast path when no redirect option is present
	plain := []string{"-hide_banner", "-protocol_whitelist", "file,pipe", "-i", "/tmp/a.mkv"}
	if got := FilterFFmpegArgs("ffmpeg", plain); !reflect.DeepEqual(got, plain) {
		t.Fatalf("expected unchanged plain args, got %v", got)
	}

	// 2. filterRedirectArgs behavior for FFmpeg < 8.1 vs >= 8.1
	inArgs := []string{"-y", "-protocol_whitelist", "http,https,tcp,tls,crypto", "-max_redirects", "0", "-rw_timeout", "15000000", "-i", "https://93.184.216.34/a.mkv", "-max_redirects", "keep-after-i"}
	gotOld := filterRedirectArgs(inArgs, false)
	wantOld := []string{"-y", "-protocol_whitelist", "http,https,tcp,tls,crypto", "-rw_timeout", "15000000", "-i", "https://93.184.216.34/a.mkv", "-max_redirects", "keep-after-i"}
	if !reflect.DeepEqual(gotOld, wantOld) {
		t.Fatalf("FFmpeg < 8.1 filter got %v, want %v", gotOld, wantOld)
	}

	gotNew := filterRedirectArgs([]string{"-follow_redirects", "0", "-i", "https://93.184.216.34/a.mkv"}, true)
	wantNew := []string{"-max_redirects", "0", "-i", "https://93.184.216.34/a.mkv"}
	if !reflect.DeepEqual(gotNew, wantNew) {
		t.Fatalf("FFmpeg >= 8.1 normalize got %v, want %v", gotNew, wantNew)
	}

	// 3. Help parser branches
	if !parseHTTPProtocolMaxRedirectsSupport("Protocol http:\n  -max_redirects <int>\n  -seekable <boolean>\n") {
		t.Fatal("expected true when max_redirects is present")
	}
	if parseHTTPProtocolMaxRedirectsSupport("Protocol http:\n  -seekable <boolean>\n  -user_agent <string>\n") {
		t.Fatal("expected false for FFmpeg < 8.1 http help output without max_redirects")
	}
	if !parseHTTPProtocolMaxRedirectsSupport("unrelated output") {
		t.Fatal("expected fallback true on unknown output")
	}

	// 4. Shebang script and missing binary bypass subprocess probing
	tmp := t.TempDir()
	scriptPath := filepath.Join(tmp, "mock-ffmpeg.sh")
	markerPath := filepath.Join(tmp, "invoked")
	_ = os.WriteFile(scriptPath, []byte("#!/bin/sh\ntouch \""+markerPath+"\"\n"), 0o755)
	withRedir := []string{"-max_redirects", "0", "-i", "http://127.0.0.1/a.ts"}
	if got := FilterFFmpegArgs(scriptPath, withRedir); !reflect.DeepEqual(got, withRedir) {
		t.Fatalf("expected mock script args preserved verbatim, got %v", got)
	}
	if !FFmpegSupportsMaxRedirects(scriptPath) {
		t.Fatal("expected shebang script to return true")
	}
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Fatal("FFmpegSupportsMaxRedirects must not execute shebang test scripts")
	}
	if !FFmpegSupportsMaxRedirects(filepath.Join(tmp, "missing-ffmpeg-bin")) {
		t.Fatal("expected missing binary to return true")
	}
	if !FFmpegSupportsMaxRedirects("/bin/sh") {
		t.Fatal("expected /bin/sh to return true")
	}

	// 5. Real ffmpeg/ffprobe probe + cache hit when installed
	for _, bin := range []string{"ffmpeg", "ffprobe"} {
		if p, err := exec.LookPath(bin); err == nil {
			first := FFmpegSupportsMaxRedirects(p)
			second := FFmpegSupportsMaxRedirects(p)
			if first != second {
				t.Fatalf("inconsistent cached result for %s", bin)
			}
		}
	}
}
