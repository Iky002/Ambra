package netutil

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var ffmpegMaxRedirectsCache sync.Map

// FFmpegSupportsMaxRedirects reports whether the given ffmpeg/ffprobe executable
// supports the HTTP protocol `-max_redirects` AVOption (introduced in FFmpeg 8.1).
// Shell script wrappers (starting with `#!` or standard shells) and unresolved paths
// return true without spawning a subprocess so unit-test mock scripts are unaffected.
func FFmpegSupportsMaxRedirects(binPath string) bool {
	candidate := strings.TrimSpace(binPath)
	if candidate == "" {
		candidate = "ffmpeg"
	}
	resolved, err := exec.LookPath(candidate)
	if err != nil {
		return true
	}
	fi, err := os.Stat(resolved)
	if err != nil || fi.IsDir() {
		return true
	}
	if isShellOrScriptBinary(resolved) {
		return true
	}

	cacheKey := fmt.Sprintf("%s|%d|%d", resolved, fi.Size(), fi.ModTime().UnixNano())
	if cached, ok := ffmpegMaxRedirectsCache.Load(cacheKey); ok {
		return cached.(bool)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(ctx, resolved, "-hide_banner", "-h", "protocol=http").CombinedOutput()
	supported := parseHTTPProtocolMaxRedirectsSupport(string(out))
	ffmpegMaxRedirectsCache.Store(cacheKey, supported)
	return supported
}

func isShellOrScriptBinary(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	switch base {
	case "sh", "bash", "dash", "zsh", "ash", "ksh", "cmd", "cmd.exe":
		return true
	}
	f, err := os.Open(path)
	if err != nil {
		return true
	}
	defer f.Close()
	var magic [2]byte
	n, err := f.Read(magic[:])
	if err != nil || n < 2 {
		return true
	}
	return magic[0] == '#' && magic[1] == '!'
}

func parseHTTPProtocolMaxRedirectsSupport(helpOutput string) bool {
	if strings.Contains(helpOutput, "max_redirects") {
		return true
	}
	if strings.Contains(helpOutput, "seekable") || strings.Contains(helpOutput, "user_agent") || strings.Contains(helpOutput, "Protocol http") {
		return false
	}
	return true
}

// FilterFFmpegArgs adapts FFmpeg/FFprobe CLI arguments for the target binary version.
// On FFmpeg < 8.1 (e.g., Debian Bookworm FFmpeg 5.1 and Ubuntu 24.04 FFmpeg 6.1),
// `-max_redirects` is not yet an HTTP protocol AVOption and causes immediate CLI exit;
// FilterFFmpegArgs strips it while preserving all other security flags (`-protocol_whitelist`, `-rw_timeout`, etc.).
func FilterFFmpegArgs(binPath string, args []string) []string {
	hasRedirectOpt := false
	for _, a := range args {
		if a == "-i" || a == "--" {
			break
		}
		if a == "-max_redirects" || a == "-follow_redirects" {
			hasRedirectOpt = true
			break
		}
	}
	if !hasRedirectOpt {
		return args
	}
	if isQuickScriptPath(binPath) {
		return args
	}
	return filterRedirectArgs(args, FFmpegSupportsMaxRedirects(binPath))
}

func isQuickScriptPath(binPath string) bool {
	trimmed := strings.TrimSpace(binPath)
	if trimmed == "" {
		return false
	}
	if strings.ContainsRune(trimmed, filepath.Separator) || strings.ContainsRune(trimmed, '/') {
		if fi, err := os.Stat(trimmed); err == nil && !fi.IsDir() {
			return isShellOrScriptBinary(trimmed)
		}
	}
	return false
}

func filterRedirectArgs(args []string, supportsMaxRedirects bool) []string {
	out := make([]string, 0, len(args))
	pastInputs := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "-i" || arg == "--" {
			pastInputs = true
		}
		if !pastInputs && (arg == "-max_redirects" || arg == "-follow_redirects") {
			var val string
			hasVal := i+1 < len(args)
			if hasVal {
				val = args[i+1]
				i++
			}
			if supportsMaxRedirects {
				out = append(out, "-max_redirects")
				if hasVal {
					out = append(out, val)
				}
			}
			continue
		}
		out = append(out, arg)
	}
	return out
}
