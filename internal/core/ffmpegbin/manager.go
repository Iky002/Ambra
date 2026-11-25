package ffmpegbin

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ambra-media/ambra/internal/pkg/netutil"
)

const maxStaticBinarySize = 256 << 20 // 256 MiB

var (
	executablePathFn = os.Executable
	downloadClient   = netutil.NewSafeHTTPClient(5*time.Minute, false)
)

func isAllowedStaticBinaryName(name string) bool {
	switch name {
	case "ffmpeg", "ffprobe", "ffmpeg.exe", "ffprobe.exe":
		return true
	default:
		return false
	}
}

// ResolveOptions configures how ffmpeg and ffprobe binary paths are resolved.
type ResolveOptions struct {
	DataDir         string
	ConfigFFmpeg    string
	ConfigFFprobe   string
	EnvFFmpeg       string
	EnvFFprobe      string
	ExecutablePath  string
	GOOS            string
	EmbeddedFFmpeg  []byte
	EmbeddedFFprobe []byte
}

// ExtractEmbedded atomically extracts embedded ffmpeg/ffprobe binaries into targetDir
// with 0755 permissions. If a target file already exists with matching byte size,
// writing is skipped to avoid redundant disk I/O.
func ExtractEmbedded(targetDir string, ffmpegBytes, ffprobeBytes []byte) (ffmpegPath, ffprobePath string, err error) {
	return extractEmbeddedForOS(targetDir, ffmpegBytes, ffprobeBytes, runtime.GOOS)
}

func extractEmbeddedForOS(targetDir string, ffmpegBytes, ffprobeBytes []byte, goos string) (ffmpegPath, ffprobePath string, err error) {
	if len(ffmpegBytes) == 0 && len(ffprobeBytes) == 0 {
		return "", "", nil
	}
	targetDir = strings.TrimSpace(targetDir)
	if targetDir == "" {
		return "", "", errors.New("ffmpegbin: target directory is empty")
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", "", fmt.Errorf("ffmpegbin: create target directory: %w", err)
	}
	if len(ffmpegBytes) > 0 {
		ffmpegPath, err = extractOne(targetDir, binaryName("ffmpeg", goos), ffmpegBytes)
		if err != nil {
			return "", "", err
		}
	}
	if len(ffprobeBytes) > 0 {
		ffprobePath, err = extractOne(targetDir, binaryName("ffprobe", goos), ffprobeBytes)
		if err != nil {
			return "", "", err
		}
	}
	return ffmpegPath, ffprobePath, nil
}

func extractOne(targetDir, name string, data []byte) (string, error) {
	dest := filepath.Join(targetDir, name)
	if info, err := os.Stat(dest); err == nil && !info.IsDir() && info.Size() == int64(len(data)) {
		return dest, nil
	}
	if err := atomicWriteExecutable(targetDir, dest, name, func(w io.Writer) (int64, error) {
		n, err := w.Write(data)
		return int64(n), err
	}); err != nil {
		return "", err
	}
	return dest, nil
}

// Resolve determines the effective paths for ffmpeg and ffprobe using strict priority:
//  1. Explicit environment variables (AMBRA_FFMPEG_PATH / AMBRA_FFPROBE_PATH)
//  2. Non-default custom path in config
//  3. Embedded binary extracted to <DataDir>/bin/ (via -tags embed_ffmpeg or options)
//  4. Sibling directory next to executable (./ffmpeg(.exe) and ./bin/ffmpeg(.exe))
//  5. Managed data directory (<DataDir>/bin/ffmpeg(.exe))
//  6. System PATH fallback ("ffmpeg" / "ffprobe")
func Resolve(opts ResolveOptions) (ffmpegPath, ffprobePath string) {
	goos := strings.TrimSpace(opts.GOOS)
	if goos == "" {
		goos = runtime.GOOS
	}

	envFFmpeg := strings.TrimSpace(opts.EnvFFmpeg)
	if envFFmpeg == "" {
		envFFmpeg = strings.TrimSpace(os.Getenv("AMBRA_FFMPEG_PATH"))
	}
	envFFprobe := strings.TrimSpace(opts.EnvFFprobe)
	if envFFprobe == "" {
		envFFprobe = strings.TrimSpace(os.Getenv("AMBRA_FFPROBE_PATH"))
	}

	exePath := strings.TrimSpace(opts.ExecutablePath)
	if exePath == "" {
		if p, err := executablePathFn(); err == nil {
			exePath = p
		}
	}

	embFFmpeg := opts.EmbeddedFFmpeg
	if len(embFFmpeg) == 0 {
		embFFmpeg = EmbeddedFFmpeg
	}
	embFFprobe := opts.EmbeddedFFprobe
	if len(embFFprobe) == 0 {
		embFFprobe = EmbeddedFFprobe
	}

	ffmpegPath = resolveOne("ffmpeg", envFFmpeg, opts.ConfigFFmpeg, opts.DataDir, exePath, goos, embFFmpeg)
	ffprobePath = resolveOne("ffprobe", envFFprobe, opts.ConfigFFprobe, opts.DataDir, exePath, goos, embFFprobe)
	return ffmpegPath, ffprobePath
}

func resolveOne(baseName, envVal, cfgVal, dataDir, exePath, goos string, embedded []byte) string {
	// 1. Explicit environment variable
	if envVal != "" {
		return envVal
	}

	// 2. Non-default custom path in config
	if trimmed := strings.TrimSpace(cfgVal); !isDefaultName(trimmed, baseName) {
		return trimmed
	}

	dataBinDir := ""
	if strings.TrimSpace(dataDir) != "" {
		dataBinDir = filepath.Join(strings.TrimSpace(dataDir), "bin")
	}

	// 3. Embedded binary extracted into <DataDir>/bin/
	if len(embedded) > 0 && dataBinDir != "" {
		var extracted string
		var err error
		if baseName == "ffmpeg" {
			extracted, _, err = extractEmbeddedForOS(dataBinDir, embedded, nil, goos)
		} else {
			_, extracted, err = extractEmbeddedForOS(dataBinDir, nil, embedded, goos)
		}
		if err == nil && extracted != "" {
			return extracted
		}
	}

	candidates := candidateNames(baseName, goos)

	// 4. Executable sibling directory: ./<bin> and ./bin/<bin>
	if exePath != "" {
		exeDir := filepath.Dir(exePath)
		for _, dir := range []string{exeDir, filepath.Join(exeDir, "bin")} {
			if found := findInDir(dir, candidates); found != "" {
				return found
			}
		}
	}

	// 5. Managed data directory: <DataDir>/bin/<bin>
	if dataBinDir != "" {
		if found := findInDir(dataBinDir, candidates); found != "" {
			return found
		}
	}

	// 6. System PATH fallback
	return baseName
}

// DownloadStatic downloads a static binary from an HTTPS rawURL and atomically installs it
// into targetDir with executable permissions (0755).
func DownloadStatic(ctx context.Context, targetDir, rawURL string) (string, error) {
	return DownloadStaticVerified(ctx, targetDir, rawURL, "")
}

// DownloadStaticVerified downloads a static binary over HTTPS, optionally verifies its SHA-256
// hex digest when expectedSHA256 is non-empty, and atomically installs it into targetDir (0755).
func DownloadStaticVerified(ctx context.Context, targetDir, rawURL, expectedSHA256 string) (string, error) {
	targetDir = strings.TrimSpace(targetDir)
	if targetDir == "" {
		return "", errors.New("ffmpegbin: target directory is empty")
	}
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Host == "" {
		return "", fmt.Errorf("ffmpegbin: invalid download url %q (https required)", rawURL)
	}
	expectedHash := strings.ToLower(strings.TrimSpace(expectedSHA256))
	if expectedHash != "" && len(expectedHash) != sha256.Size*2 {
		return "", fmt.Errorf("ffmpegbin: invalid expected SHA-256 digest length %d", len(expectedHash))
	}
	fileName := path.Base(parsed.Path)
	if fileName == "" || fileName == "." || fileName == "/" {
		fileName = binaryName("ffmpeg", runtime.GOOS)
	}
	if !isAllowedStaticBinaryName(fileName) {
		return "", fmt.Errorf("ffmpegbin: disallowed binary name %q", fileName)
	}

	if ctx == nil {
		ctx = context.Background()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return "", fmt.Errorf("ffmpegbin: build request: %w", err)
	}
	resp, err := downloadClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("ffmpegbin: download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ffmpegbin: unexpected HTTP status %d", resp.StatusCode)
	}

	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", fmt.Errorf("ffmpegbin: create target directory: %w", err)
	}

	dest := filepath.Join(targetDir, fileName)

	if err := atomicWriteExecutable(targetDir, dest, fileName, func(w io.Writer) (int64, error) {
		hasher := sha256.New()
		mw := io.MultiWriter(w, hasher)
		n, copyErr := io.Copy(mw, io.LimitReader(resp.Body, maxStaticBinarySize+1))
		if copyErr != nil {
			return n, copyErr
		}
		if n == 0 {
			return 0, errors.New("ffmpegbin: downloaded file is empty")
		}
		if n > maxStaticBinarySize {
			return n, fmt.Errorf("ffmpegbin: downloaded binary exceeds max size of %d bytes", maxStaticBinarySize)
		}
		if expectedHash != "" {
			actualHash := hex.EncodeToString(hasher.Sum(nil))
			if subtle.ConstantTimeCompare([]byte(actualHash), []byte(expectedHash)) != 1 {
				return n, fmt.Errorf("ffmpegbin: sha256 mismatch: got %s, want %s", actualHash, expectedHash)
			}
		}
		return n, nil
	}); err != nil {
		return "", err
	}
	return dest, nil
}

func atomicWriteExecutable(targetDir, dest, name string, writeFn func(w io.Writer) (int64, error)) error {
	tmp, err := os.CreateTemp(targetDir, "."+name+".tmp-*")
	if err != nil {
		return fmt.Errorf("ffmpegbin: create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := writeFn(tmp); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("ffmpegbin: close temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return fmt.Errorf("ffmpegbin: chmod temp file: %w", err)
	}
	if err := os.Rename(tmpPath, dest); err != nil {
		return fmt.Errorf("ffmpegbin: rename temp file: %w", err)
	}
	cleanup = false
	return nil
}

func binaryName(base, goos string) string {
	if goos == "windows" {
		return base + ".exe"
	}
	return base
}

func candidateNames(base, goos string) []string {
	if goos == "windows" {
		return []string{base + ".exe", base}
	}
	return []string{base}
}

func isDefaultName(val, base string) bool {
	return val == "" || val == base || val == base+".exe"
}

func findInDir(dir string, names []string) string {
	for _, name := range names {
		candidate := filepath.Join(dir, name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}
