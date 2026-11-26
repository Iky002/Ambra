package transcode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/netutil"
)

const (
	defaultExtractTimeout   = 45 * time.Second
	localProtocolWhitelist  = "file,pipe"
	remoteProtocolWhitelist = "http,https,tcp,tls,crypto"
	maxProbeAttachmentBytes = 4 << 20 // 4 MiB
	maxFontAttachments      = 64
	maxSingleFontBytes      = int64(32 << 20)  // 32 MiB
	maxTotalFontBytes       = int64(128 << 20) // 128 MiB
	maxAttachmentNameBytes  = 255
)

func protocolWhitelistForInput(mediaPath string) string {
	if isHTTPInput(mediaPath) {
		return remoteProtocolWhitelist
	}
	return localProtocolWhitelist
}

func resolveMediaInputPath(mediaPath string) (string, error) {
	p := strings.TrimSpace(mediaPath)
	if p == "" {
		return "", fmt.Errorf("media path is empty")
	}
	if hasControlChars(p) {
		return "", fmt.Errorf("media path contains forbidden control characters")
	}
	if isHTTPInput(p) {
		if err := netutil.ValidateRemoteURL(p, true); err != nil {
			return "", fmt.Errorf("invalid media url: %w", err)
		}
		return p, nil
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("resolve media path: %w", err)
	}
	return abs, nil
}

// Extractor extracts embedded subtitle tracks and font attachments from media containers
// on demand, caching results under cfg.SubtitleCacheDir() and deduplicating concurrent requests.
type Extractor struct {
	cfg         *config.Config
	ffmpegPath  string
	ffprobePath string
	sf          singleflight.Group
}

// NewExtractor creates an Extractor backed by the given config.
func NewExtractor(cfg *config.Config) *Extractor {
	ffmpegPath := "ffmpeg"
	ffprobePath := "ffprobe"
	if cfg != nil {
		if strings.TrimSpace(cfg.FFmpegPath) != "" {
			ffmpegPath = cfg.FFmpegPath
		}
		if strings.TrimSpace(cfg.FFprobePath) != "" {
			ffprobePath = cfg.FFprobePath
		}
	}
	return &Extractor{
		cfg:         cfg,
		ffmpegPath:  ffmpegPath,
		ffprobePath: ffprobePath,
	}
}

// SubtitleCachePath computes the deterministic cache path for an extracted embedded subtitle.
func (e *Extractor) SubtitleCachePath(itemID, mediaSourceID string, streamIndex int, targetExt string) string {
	baseDir := filepath.Join(os.TempDir(), "ambra-subtitles")
	if e.cfg != nil {
		baseDir = e.cfg.SubtitleCacheDir()
	}
	ext := normalizeSubtitleExt(targetExt)
	name := fmt.Sprintf("%s_%s_%d.%s", sanitizePathSegment(itemID), sanitizePathSegment(mediaSourceID), streamIndex, ext)
	return filepath.Join(baseDir, name)
}

func startExtractorInputProxy(mediaPath string, headers map[string]string) (string, func(), error) {
	if !isHTTPInput(mediaPath) {
		return mediaPath, func() {}, nil
	}
	lowerNoQuery := strings.ToLower(strings.SplitN(mediaPath, "?", 2)[0])
	if strings.HasSuffix(lowerNoQuery, ".m3u8") {
		return mediaPath, func() {}, nil
	}
	proxy, err := StartLoopbackStreamProxy(LoopbackProxyOptions{
		TargetURL:       mediaPath,
		Headers:         headers,
		AllowPrivateLAN: true,
	})
	if err != nil {
		return "", nil, err
	}
	return proxy.URL(), func() { _ = proxy.Close() }, nil
}

// ExtractSubtitle extracts an embedded subtitle track from mediaPath into cfg.SubtitleCacheDir().
// Concurrent calls for the same track share a single FFmpeg invocation via singleflight.
func (e *Extractor) ExtractSubtitle(ctx context.Context, mediaPath, itemID, mediaSourceID string, streamIndex int, targetExt string) (string, error) {
	return e.ExtractSubtitleWithHeaders(ctx, mediaPath, nil, itemID, mediaSourceID, streamIndex, targetExt)
}

// ExtractSubtitleWithHeaders extracts an embedded subtitle track, routing remote HTTP(S) inputs
// through a pinned-IP loopback stream proxy with optional upstream headers.
func (e *Extractor) ExtractSubtitleWithHeaders(ctx context.Context, mediaPath string, headers map[string]string, itemID, mediaSourceID string, streamIndex int, targetExt string) (string, error) {
	resolvedMedia, err := resolveMediaInputPath(mediaPath)
	if err != nil {
		return "", err
	}
	ext := normalizeSubtitleExt(targetExt)
	outPath := e.SubtitleCachePath(itemID, mediaSourceID, streamIndex, ext)
	if fileReady(outPath) {
		return outPath, nil
	}

	sfKey := "sub:" + outPath
	res, err, _ := e.sf.Do(sfKey, func() (any, error) {
		if fileReady(outPath) {
			return outPath, nil
		}
		if err := os.MkdirAll(filepath.Dir(outPath), 0o700); err != nil {
			return "", fmt.Errorf("create subtitle cache dir: %w", err)
		}

		tmpFile, err := os.CreateTemp(filepath.Dir(outPath), ".ambra-sub-*."+ext)
		if err != nil {
			return "", fmt.Errorf("create subtitle temp file: %w", err)
		}
		tmpPath := tmpFile.Name()
		_ = tmpFile.Close()
		defer os.Remove(tmpPath)

		runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), defaultExtractTimeout)
		defer cancel()

		ffInput, closeProxy, proxyErr := startExtractorInputProxy(resolvedMedia, headers)
		if proxyErr != nil {
			return "", proxyErr
		}
		defer closeProxy()

		args := netutil.FilterFFmpegArgs(e.ffmpegPath, BuildSubtitleExtractArgs(ffInput, streamIndex, ext, tmpPath))
		cmd := exec.CommandContext(runCtx, e.ffmpegPath, args...)
		stderr := newSafeRingBuffer(maxStderrBytes)
		cmd.Stderr = stderr
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("extract subtitle stream %d: %w (%s)", streamIndex, err, strings.TrimSpace(stderr.String()))
		}
		if !fileReady(tmpPath) {
			return "", fmt.Errorf("extracted subtitle stream %d is empty", streamIndex)
		}
		if err := os.Rename(tmpPath, outPath); err != nil {
			return "", fmt.Errorf("commit extracted subtitle: %w", err)
		}
		return outPath, nil
	})
	if err != nil {
		return "", err
	}
	return res.(string), nil
}

// BuildSubtitleExtractArgs builds the FFmpeg arguments for extracting a single subtitle stream.
func BuildSubtitleExtractArgs(mediaPath string, streamIndex int, targetExt, outPath string) []string {
	ext := normalizeSubtitleExt(targetExt)
	subCodec := "copy"
	switch ext {
	case "srt":
		subCodec = "srt"
	case "vtt":
		subCodec = "webvtt"
	case "ass":
		subCodec = "ass"
	case "ssa":
		subCodec = "ass"
	case "sup":
		subCodec = "copy"
	}
	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-protocol_whitelist", protocolWhitelistForInput(mediaPath),
	}
	if isHTTPInput(mediaPath) {
		args = append(args, "-max_redirects", "0")
	}
	args = append(args,
		"-i", mediaPath,
		"-map", "0:"+strconv.Itoa(streamIndex),
		"-c:s", subCodec,
		outPath,
	)
	return args
}

// ExtractFonts extracts embedded font attachments (e.g. TTF/OTF in MKV) for client-side ASS rendering (JASSUB).
// Returns the list of extracted font file paths cached under cfg.SubtitleCacheDir()/fonts/<itemID>_<mediaSourceID>/.
func (e *Extractor) ExtractFonts(ctx context.Context, mediaPath, itemID, mediaSourceID string) ([]string, error) {
	resolvedMedia, err := resolveMediaInputPath(mediaPath)
	if err != nil {
		return nil, err
	}
	baseDir := filepath.Join(os.TempDir(), "ambra-subtitles")
	if e.cfg != nil {
		baseDir = e.cfg.SubtitleCacheDir()
	}
	fontsRoot := filepath.Join(baseDir, "fonts")
	fontDir := filepath.Join(fontsRoot, fmt.Sprintf("%s_%s", sanitizePathSegment(itemID), sanitizePathSegment(mediaSourceID)))
	markerPath := filepath.Join(fontDir, ".extracted")

	if _, err := os.Stat(markerPath); err == nil {
		return listFontFiles(fontDir)
	}

	sfKey := "fonts:" + fontDir
	res, err, _ := e.sf.Do(sfKey, func() (any, error) {
		if _, err := os.Stat(markerPath); err == nil {
			return listFontFiles(fontDir)
		}
		if err := os.MkdirAll(fontsRoot, 0o700); err != nil {
			return nil, fmt.Errorf("create fonts root dir: %w", err)
		}
		stageDir, err := os.MkdirTemp(fontsRoot, ".ambra-fonts-*")
		if err != nil {
			return nil, fmt.Errorf("create font staging dir: %w", err)
		}
		defer os.RemoveAll(stageDir)

		runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), defaultExtractTimeout)
		defer cancel()

		ffInput, closeProxy, proxyErr := startExtractorInputProxy(resolvedMedia, nil)
		if proxyErr != nil {
			return nil, proxyErr
		}
		defer closeProxy()

		// Step 1: Probe attachment streams with ffprobe and filter safe font filenames in Go.
		// Never use `ffmpeg -dump_attachment:t ""` because untrusted container attachment filenames
		// can contain path traversal sequences that FFmpeg writes outside the staging directory.
		targets, probeErr := e.probeSafeFontAttachments(runCtx, ffInput)
		if probeErr != nil {
			return nil, fmt.Errorf("probe font attachments: %w", probeErr)
		}

		if err := os.MkdirAll(fontDir, 0o700); err != nil {
			return nil, fmt.Errorf("create font cache dir: %w", err)
		}

		// Step 2: Explicitly dump each validated font attachment stream to a safe path inside stageDir.
		extractedCount := 0
		if len(targets) > 0 {
			fontArgs := []string{
				"-hide_banner", "-loglevel", "error", "-y",
				"-protocol_whitelist", protocolWhitelistForInput(ffInput),
			}
			if isHTTPInput(ffInput) {
				fontArgs = append(fontArgs, "-max_redirects", "0")
			}
			for _, t := range targets {
				fontArgs = append(fontArgs,
					fmt.Sprintf("-dump_attachment:%d", t.streamIndex),
					filepath.Join(stageDir, t.safeName),
				)
			}
			fontArgs = append(fontArgs, "-i", ffInput)
			fontArgs = netutil.FilterFFmpegArgs(e.ffmpegPath, fontArgs)
			cmd := exec.CommandContext(runCtx, e.ffmpegPath, fontArgs...)
			cmd.Dir = stageDir
			_ = cmd.Run()

			var totalBytes int64
			for _, t := range targets {
				src := filepath.Join(stageDir, t.safeName)
				info, statErr := os.Lstat(src)
				if statErr != nil || info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 {
					continue
				}
				if info.Size() > maxSingleFontBytes || totalBytes+info.Size() > maxTotalFontBytes {
					continue
				}
				dst := filepath.Join(fontDir, t.safeName)
				if err := os.Rename(src, dst); err == nil {
					totalBytes += info.Size()
					extractedCount++
				}
			}
		}

		if runCtx.Err() != nil {
			return nil, fmt.Errorf("extract font attachments cancelled: %w", runCtx.Err())
		}
		if len(targets) > 0 && extractedCount == 0 {
			return nil, fmt.Errorf("failed to extract any font attachments")
		}
		_ = os.WriteFile(markerPath, []byte("ok"), 0o600)
		return listFontFiles(fontDir)
	})
	if err != nil {
		return nil, err
	}
	return res.([]string), nil
}

type fontAttachmentTarget struct {
	streamIndex int
	safeName    string
}

type limitedBuffer struct {
	buf       bytes.Buffer
	max       int
	truncated bool
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if l.max <= 0 {
		l.truncated = true
		return len(p), nil
	}
	rem := l.max - l.buf.Len()
	if rem <= 0 {
		l.truncated = true
		return len(p), nil
	}
	if len(p) > rem {
		_, _ = l.buf.Write(p[:rem])
		l.truncated = true
		return len(p), nil
	}
	_, _ = l.buf.Write(p)
	return len(p), nil
}

func (e *Extractor) probeSafeFontAttachments(ctx context.Context, resolvedMedia string) ([]fontAttachmentTarget, error) {
	if strings.TrimSpace(e.ffprobePath) == "" {
		return nil, fmt.Errorf("ffprobe path is empty")
	}
	probeArgs := []string{
		"-v", "error",
		"-protocol_whitelist", protocolWhitelistForInput(resolvedMedia),
	}
	if isHTTPInput(resolvedMedia) {
		probeArgs = append(probeArgs, "-max_redirects", "0")
	}
	probeArgs = append(probeArgs,
		"-select_streams", "t",
		"-show_entries", "stream=index:stream_tags=filename",
		"-of", "json",
		resolvedMedia,
	)

	probeArgs = netutil.FilterFFmpegArgs(e.ffprobePath, probeArgs)
	cmd := exec.CommandContext(ctx, e.ffprobePath, probeArgs...)
	stdout := &limitedBuffer{max: maxProbeAttachmentBytes}
	cmd.Stdout = stdout
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffprobe attachments failed: %w", err)
	}
	if stdout.truncated {
		return nil, fmt.Errorf("ffprobe attachments output exceeded %d bytes", maxProbeAttachmentBytes)
	}

	var parsed struct {
		Streams []struct {
			Index int               `json:"index"`
			Tags  map[string]string `json:"tags"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(stdout.buf.Bytes(), &parsed); err != nil {
		return nil, fmt.Errorf("parse ffprobe attachments json: %w", err)
	}

	usedNames := make(map[string]struct{}, len(parsed.Streams))
	targets := make([]fontAttachmentTarget, 0, min(len(parsed.Streams), maxFontAttachments))
	for _, s := range parsed.Streams {
		if len(targets) >= maxFontAttachments {
			break
		}
		if s.Index < 0 {
			continue
		}
		var filename string
		for k, v := range s.Tags {
			if strings.EqualFold(k, "filename") {
				filename = strings.TrimSpace(v)
				break
			}
		}
		if !isSafeAttachmentFilename(filename) || !isFontExt(filepath.Ext(filename)) {
			continue
		}
		safeName := deduplicateAttachmentName(filename, s.Index, usedNames)
		usedNames[strings.ToLower(safeName)] = struct{}{}
		targets = append(targets, fontAttachmentTarget{
			streamIndex: s.Index,
			safeName:    safeName,
		})
	}
	return targets, nil
}

func deduplicateAttachmentName(name string, streamIdx int, used map[string]struct{}) string {
	if _, exists := used[strings.ToLower(name)]; !exists {
		return name
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	candidate := fmt.Sprintf("%s_%d%s", stem, streamIdx, ext)
	if _, exists := used[strings.ToLower(candidate)]; !exists {
		return candidate
	}
	for n := 1; ; n++ {
		alt := fmt.Sprintf("%s_%d_%d%s", stem, streamIdx, n, ext)
		if _, exists := used[strings.ToLower(alt)]; !exists {
			return alt
		}
	}
}

func isSafeAttachmentFilename(name string) bool {
	if name == "" || len(name) > maxAttachmentNameBytes || strings.HasPrefix(name, ".") {
		return false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	if strings.Contains(name, "/") || strings.Contains(name, `\`) || strings.Contains(name, "..") {
		return false
	}
	return filepath.Base(name) == name
}

func isFontExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".ttf", ".otf", ".ttc", ".woff", ".woff2":
		return true
	default:
		return false
	}
}

// ChapterThumbnailCachePath computes the deterministic cache path for a chapter thumbnail
// inside the dedicated persistent chapter cache directory (<DataDir>/cache/chapters/<itemID>/chapter_<idx>.jpg).
func (e *Extractor) ChapterThumbnailCachePath(itemID string, chapterIdx int) string {
	baseDir := filepath.Join(os.TempDir(), "ambra-chapters")
	if e.cfg != nil {
		baseDir = e.cfg.ChapterCacheDir()
	}
	return filepath.Join(baseDir, sanitizePathSegment(itemID), fmt.Sprintf("chapter_%d.jpg", chapterIdx))
}

// ExtractChapterThumbnail extracts a JPEG thumbnail for chapterIdx at startTicks into
// <ChapterCacheDir>/<itemID>/chapter_<idx>.jpg using singleflight deduplication,
// atomic temp-file rename, and a 15-second FFmpeg timeout.
func (e *Extractor) ExtractChapterThumbnail(ctx context.Context, mediaPath, itemID string, chapterIdx int, startTicks int64) (string, error) {
	resolvedMedia, err := resolveMediaInputPath(mediaPath)
	if err != nil {
		return "", err
	}
	if chapterIdx < 0 {
		return "", fmt.Errorf("chapter index must be non-negative")
	}
	outPath := e.ChapterThumbnailCachePath(itemID, chapterIdx)
	if fileReady(outPath) {
		return outPath, nil
	}

	sfKey := "chapter:" + outPath
	res, err, _ := e.sf.Do(sfKey, func() (any, error) {
		if fileReady(outPath) {
			return outPath, nil
		}
		if err := os.MkdirAll(filepath.Dir(outPath), 0o700); err != nil {
			return "", fmt.Errorf("create chapter thumbnail cache dir: %w", err)
		}

		tmpFile, err := os.CreateTemp(filepath.Dir(outPath), ".ambra-chap-*.jpg")
		if err != nil {
			return "", fmt.Errorf("create chapter thumbnail temp file: %w", err)
		}
		tmpPath := tmpFile.Name()
		_ = tmpFile.Close()
		defer os.Remove(tmpPath)

		sec := float64(startTicks) / float64(TicksPerSecond)
		if sec < 0 {
			sec = 0
		}
		runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()

		ffInput, closeProxy, proxyErr := startExtractorInputProxy(resolvedMedia, nil)
		if proxyErr != nil {
			return "", proxyErr
		}
		defer closeProxy()

		args := []string{
			"-hide_banner",
			"-loglevel", "error",
			"-y",
			"-protocol_whitelist", protocolWhitelistForInput(ffInput),
		}
		if isHTTPInput(ffInput) {
			args = append(args, "-max_redirects", "0")
		}
		args = append(args,
			"-ss", strconv.FormatFloat(sec, 'f', 3, 64),
			"-i", ffInput,
			"-frames:v", "1",
			"-q:v", "4",
			"-vf", "scale='min(640,iw)':-2",
			tmpPath,
		)
		args = netutil.FilterFFmpegArgs(e.ffmpegPath, args)
		cmd := exec.CommandContext(runCtx, e.ffmpegPath, args...)
		stderr := newSafeRingBuffer(maxStderrBytes)
		cmd.Stderr = stderr
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("extract chapter %d thumbnail: %w (%s)", chapterIdx, err, strings.TrimSpace(stderr.String()))
		}
		if !fileReady(tmpPath) {
			return "", fmt.Errorf("extracted chapter %d thumbnail is empty", chapterIdx)
		}
		if err := os.Rename(tmpPath, outPath); err != nil {
			return "", fmt.Errorf("commit chapter thumbnail: %w", err)
		}
		return outPath, nil
	})
	if err != nil {
		return "", err
	}
	return res.(string), nil
}

func listFontFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		switch ext {
		case ".ttf", ".otf", ".ttc", ".woff", ".woff2":
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out, nil
}

func normalizeSubtitleExt(ext string) string {
	switch strings.ToLower(strings.TrimPrefix(strings.TrimSpace(ext), ".")) {
	case "ass":
		return "ass"
	case "ssa":
		return "ssa"
	case "vtt", "webvtt":
		return "vtt"
	case "sup", "pgs", "pgssub", "hdmv_pgs_subtitle":
		return "sup"
	default:
		return "srt"
	}
}
