package transcode

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sync/singleflight"

	"github.com/ambra-media/ambra/internal/config"
)

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
	if mediaPath == "" {
		return "", fmt.Errorf("media path is empty")
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
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return "", fmt.Errorf("create subtitle cache dir: %w", err)
		}

		tmpPath := outPath + ".tmp." + ext
		_ = os.Remove(tmpPath)

		args := BuildSubtitleExtractArgs(mediaPath, streamIndex, ext, tmpPath)
		cmd := exec.CommandContext(ctx, e.ffmpegPath, args...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			_ = os.Remove(tmpPath)
			return "", fmt.Errorf("extract subtitle stream %d: %w (%s)", streamIndex, err, strings.TrimSpace(stderr.String()))
		}
		if !fileReady(tmpPath) {
			_ = os.Remove(tmpPath)
			return "", fmt.Errorf("extracted subtitle stream %d is empty", streamIndex)
		}
		if err := os.Rename(tmpPath, outPath); err != nil {
			_ = os.Remove(tmpPath)
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
	return []string{
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-i", mediaPath,
		"-map", "0:" + strconv.Itoa(streamIndex),
		"-c:s", subCodec,
		outPath,
	}
}

// ExtractFonts extracts embedded font attachments (e.g. TTF/OTF in MKV) for client-side ASS rendering (JASSUB).
// Returns the list of extracted font file paths cached under cfg.SubtitleCacheDir()/fonts/<itemID>_<mediaSourceID>/.
func (e *Extractor) ExtractFonts(ctx context.Context, mediaPath, itemID, mediaSourceID string) ([]string, error) {
	if mediaPath == "" {
		return nil, fmt.Errorf("media path is empty")
	}
	baseDir := filepath.Join(os.TempDir(), "ambra-subtitles")
	if e.cfg != nil {
		baseDir = e.cfg.SubtitleCacheDir()
	}
	fontDir := filepath.Join(baseDir, "fonts", fmt.Sprintf("%s_%s", sanitizePathSegment(itemID), sanitizePathSegment(mediaSourceID)))
	markerPath := filepath.Join(fontDir, ".extracted")

	if _, err := os.Stat(markerPath); err == nil {
		return listFontFiles(fontDir)
	}

	sfKey := "fonts:" + fontDir
	res, err, _ := e.sf.Do(sfKey, func() (any, error) {
		if _, err := os.Stat(markerPath); err == nil {
			return listFontFiles(fontDir)
		}
		if err := os.MkdirAll(fontDir, 0o755); err != nil {
			return nil, fmt.Errorf("create font cache dir: %w", err)
		}

		// ffmpeg -dump_attachment:t "" dumps all attachment streams into cmd.Dir.
		// Note: ffmpeg exits non-zero when no output file is specified even after dumping attachments.
		cmd := exec.CommandContext(ctx, e.ffmpegPath,
			"-hide_banner", "-loglevel", "error", "-y",
			"-dump_attachment:t", "",
			"-i", mediaPath,
		)
		cmd.Dir = fontDir
		_ = cmd.Run()

		_ = os.WriteFile(markerPath, []byte("ok"), 0o644)
		return listFontFiles(fontDir)
	})
	if err != nil {
		return nil, err
	}
	return res.([]string), nil
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
