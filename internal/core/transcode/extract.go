package transcode

import (
	"context"
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
	defaultExtractTimeout    = 45 * time.Second
	localProtocolWhitelist   = "file,pipe"
	remoteProtocolWhitelist  = "file,pipe,http,https,tcp,tls,crypto"
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
	cfg        *config.Config
	ffmpegPath string
	sf         singleflight.Group
}

// NewExtractor creates an Extractor backed by the given config.
func NewExtractor(cfg *config.Config) *Extractor {
	ffmpegPath := "ffmpeg"
	if cfg != nil && cfg.FFmpegPath != "" {
		ffmpegPath = cfg.FFmpegPath
	}
	return &Extractor{
		cfg:        cfg,
		ffmpegPath: ffmpegPath,
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

// ExtractSubtitle extracts an embedded subtitle track from mediaPath into cfg.SubtitleCacheDir().
// Concurrent calls for the same track share a single FFmpeg invocation via singleflight.
func (e *Extractor) ExtractSubtitle(ctx context.Context, mediaPath, itemID, mediaSourceID string, streamIndex int, targetExt string) (string, error) {
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

		args := BuildSubtitleExtractArgs(resolvedMedia, streamIndex, ext, tmpPath)
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
		args = append(args, "-follow_redirects", "0")
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

		// ffmpeg -dump_attachment:t "" dumps all attachment streams into cmd.Dir.
		// Extract into an isolated staging directory first to reject unsafe filenames.
		fontArgs := []string{
			"-hide_banner", "-loglevel", "error", "-y",
			"-protocol_whitelist", protocolWhitelistForInput(resolvedMedia),
		}
		if isHTTPInput(resolvedMedia) {
			fontArgs = append(fontArgs, "-follow_redirects", "0")
		}
		fontArgs = append(fontArgs,
			"-dump_attachment:t", "",
			"-i", resolvedMedia,
		)
		cmd := exec.CommandContext(runCtx, e.ffmpegPath, fontArgs...)
		cmd.Dir = stageDir
		_ = cmd.Run()

		if err := os.MkdirAll(fontDir, 0o700); err != nil {
			return nil, fmt.Errorf("create font cache dir: %w", err)
		}
		if entries, err := os.ReadDir(stageDir); err == nil {
			for _, ent := range entries {
				name := ent.Name()
				if ent.IsDir() || ent.Type()&os.ModeSymlink != 0 || !isSafeAttachmentFilename(name) {
					continue
				}
				if !isFontExt(filepath.Ext(name)) {
					continue
				}
				src := filepath.Join(stageDir, name)
				dst := filepath.Join(fontDir, name)
				_ = os.Rename(src, dst)
			}
		}

		_ = os.WriteFile(markerPath, []byte("ok"), 0o600)
		return listFontFiles(fontDir)
	})
	if err != nil {
		return nil, err
	}
	return res.([]string), nil
}

func isSafeAttachmentFilename(name string) bool {
	if name == "" || strings.HasPrefix(name, ".") {
		return false
	}
	if strings.Contains(name, "/") || strings.Contains(name, `\`) || strings.Contains(name, "..") || strings.ContainsRune(name, '\x00') {
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
		runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()

		args := []string{
			"-hide_banner",
			"-loglevel", "error",
			"-y",
			"-protocol_whitelist", protocolWhitelistForInput(resolvedMedia),
		}
		if isHTTPInput(resolvedMedia) {
			args = append(args, "-follow_redirects", "0")
		}
		args = append(args,
			"-ss", strconv.FormatFloat(sec, 'f', 3, 64),
			"-i", resolvedMedia,
			"-frames:v", "1",
			"-q:v", "4",
			"-vf", "scale='min(640,iw)':-2",
			tmpPath,
		)
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
