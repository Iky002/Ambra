package transcode

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultBIFWidth is the standard Emby/Roku BIF thumbnail width in pixels.
	DefaultBIFWidth = 320
	// DefaultBIFIntervalSec is the default frame interval in seconds (10s = 10000ms).
	DefaultBIFIntervalSec = 10

	bifHeaderSize     = 64
	bifIndexEntrySize = 8
	bifEndIndexMarker = 0xffffffff
	defaultBIFTimeout = 10 * time.Minute
	maxBIFFrames      = 10_000
	maxBIFTotalBytes  = int64(256 << 20) // 256 MiB
)

var bifMagic = [8]byte{0x89, 'B', 'I', 'F', '\r', '\n', 0x1a, '\n'}

// BIFMetadata describes the header and frame count of a valid BIF file.
type BIFMetadata struct {
	Version               uint32
	ImageCount            uint32
	TimestampMultiplierMs uint32
	FrameTimestampsMs     []uint32
}

// BifCachePath returns the deterministic cache path for a generated BIF file:
// <BifCacheDir>/<itemID>/index_<width>.bif.
func (e *Extractor) BifCachePath(itemID string, width int) string {
	if width <= 0 {
		width = DefaultBIFWidth
	}
	baseDir := filepath.Join(os.TempDir(), "ambra-bif")
	if e.cfg != nil {
		baseDir = e.cfg.BifCacheDir()
	}
	return filepath.Join(baseDir, sanitizePathSegment(itemID), fmt.Sprintf("index_%d.bif", width))
}

// FindExistingBIF looks up an already-available BIF file without triggering any extraction.
// Search order:
//  1. Sidecar BIF next to a local media file (<base>-<width>-10.bif, <base>-320-10.bif, <base>.bif)
//  2. Cached BIF at <BifCacheDir>/<itemID>/index_<width>.bif
//  3. Cached default-width BIF at <BifCacheDir>/<itemID>/index_320.bif
func (e *Extractor) FindExistingBIF(mediaPath, itemID string, width int) string {
	if width <= 0 {
		width = DefaultBIFWidth
	}
	trimmedMedia := strings.TrimSpace(mediaPath)
	if trimmedMedia != "" && !isHTTPInput(trimmedMedia) && !strings.EqualFold(filepath.Ext(trimmedMedia), ".strm") {
		base := strings.TrimSuffix(trimmedMedia, filepath.Ext(trimmedMedia))
		sidecars := []string{
			fmt.Sprintf("%s-%d-10.bif", base, width),
			fmt.Sprintf("%s-%d-10.bif", base, DefaultBIFWidth),
			base + ".bif",
		}
		for _, candidate := range sidecars {
			if fileReady(candidate) {
				return candidate
			}
		}
	}
	if strings.TrimSpace(itemID) == "" {
		return ""
	}
	if p := e.BifCachePath(itemID, width); fileReady(p) {
		return p
	}
	if width != DefaultBIFWidth {
		if p := e.BifCachePath(itemID, DefaultBIFWidth); fileReady(p) {
			return p
		}
	}
	return ""
}

// GenerateBIF extracts periodic JPEG frames from a local media file with a single FFmpeg run
// and packs them into a standard Roku/Emby BIF v0 archive under <BifCacheDir>/<itemID>/index_<width>.bif.
// Remote HTTP/STRM inputs are intentionally skipped.
func (e *Extractor) GenerateBIF(ctx context.Context, mediaPath, itemID string, width, intervalSec int) (string, error) {
	if width <= 0 {
		width = DefaultBIFWidth
	}
	if intervalSec <= 0 {
		intervalSec = DefaultBIFIntervalSec
	}
	trimmedMedia := strings.TrimSpace(mediaPath)
	if trimmedMedia == "" {
		return "", fmt.Errorf("media path is empty")
	}
	if isHTTPInput(trimmedMedia) || strings.EqualFold(filepath.Ext(trimmedMedia), ".strm") {
		return "", fmt.Errorf("skipping remote/strm media for BIF generation")
	}
	resolvedMedia, err := resolveMediaInputPath(trimmedMedia)
	if err != nil {
		return "", err
	}
	if info, statErr := os.Stat(resolvedMedia); statErr != nil || info.IsDir() {
		return "", fmt.Errorf("local media file unavailable for BIF generation: %s", resolvedMedia)
	}

	outPath := e.BifCachePath(itemID, width)
	if fileReady(outPath) {
		return outPath, nil
	}

	sfKey := "bif:" + outPath
	res, err, _ := e.sf.Do(sfKey, func() (any, error) {
		if fileReady(outPath) {
			return outPath, nil
		}
		if err := os.MkdirAll(filepath.Dir(outPath), 0o700); err != nil {
			return "", fmt.Errorf("create bif cache dir: %w", err)
		}

		stageDir, err := os.MkdirTemp(filepath.Dir(outPath), ".ambra-bif-frames-*")
		if err != nil {
			return "", fmt.Errorf("create bif staging dir: %w", err)
		}
		defer os.RemoveAll(stageDir)

		runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), defaultBIFTimeout)
		defer cancel()

		framePattern := filepath.Join(stageDir, "frame_%06d.jpg")
		vf := fmt.Sprintf("fps=1/%d,scale='min(%d,iw)':-2", intervalSec, width)
		args := []string{
			"-hide_banner",
			"-loglevel", "error",
			"-y",
			"-protocol_whitelist", localProtocolWhitelist,
			"-i", resolvedMedia,
			"-an",
			"-sn",
			"-dn",
			"-vf", vf,
			"-frames:v", strconv.Itoa(maxBIFFrames),
			"-q:v", "6",
			framePattern,
		}
		cmd := exec.CommandContext(runCtx, e.ffmpegPath, args...)
		stderr := newSafeRingBuffer(maxStderrBytes)
		cmd.Stderr = stderr
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("extract bif frames: %w (%s)", err, strings.TrimSpace(stderr.String()))
		}

		entries, err := os.ReadDir(stageDir)
		if err != nil {
			return "", fmt.Errorf("read bif frame dir: %w", err)
		}
		var names []string
		for _, ent := range entries {
			if !ent.IsDir() && strings.HasSuffix(strings.ToLower(ent.Name()), ".jpg") {
				names = append(names, ent.Name())
			}
		}
		sort.Strings(names)
		if len(names) == 0 {
			return "", fmt.Errorf("ffmpeg produced 0 frames for BIF")
		}
		if len(names) > maxBIFFrames {
			names = names[:maxBIFFrames]
		}

		framePaths := make([]string, 0, len(names))
		for _, name := range names {
			framePaths = append(framePaths, filepath.Join(stageDir, name))
		}
		if err := writeBIFFromFiles(outPath, uint32(intervalSec*1000), framePaths, maxBIFTotalBytes); err != nil {
			return "", err
		}
		return outPath, nil
	})
	if err != nil {
		return "", err
	}
	return res.(string), nil
}

type bifFrameFile struct {
	path string
	size int64
}

// writeBIFFromFiles streams JPEG frame files into a standard BIF v0 archive in two passes
// (Pass 1: stat sizes & write header/index; Pass 2: stream-copy each frame) so full JPEG payloads
// are never retained in memory simultaneously.
func writeBIFFromFiles(outPath string, intervalMs uint32, paths []string, maxTotalBytes int64) error {
	if intervalMs == 0 {
		intervalMs = DefaultBIFIntervalSec * 1000
	}
	if maxTotalBytes <= 0 {
		maxTotalBytes = maxBIFTotalBytes
	}

	valid := make([]bifFrameFile, 0, len(paths))
	var totalBytes int64
	for _, p := range paths {
		info, err := os.Lstat(p)
		if err != nil || info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 {
			continue
		}
		totalBytes += info.Size()
		if totalBytes > maxTotalBytes {
			return fmt.Errorf("bif total frame bytes %d exceeds limit %d", totalBytes, maxTotalBytes)
		}
		valid = append(valid, bifFrameFile{path: p, size: info.Size()})
	}
	if len(valid) == 0 {
		return fmt.Errorf("no non-empty frames available for BIF")
	}

	dataOffset64 := uint64(bifHeaderSize) + uint64(len(valid)+1)*uint64(bifIndexEntrySize)
	if dataOffset64+uint64(totalBytes) > math.MaxUint32 {
		return fmt.Errorf("bif archive size exceeds 32-bit offset limit")
	}

	if err := os.MkdirAll(filepath.Dir(outPath), 0o700); err != nil {
		return fmt.Errorf("create bif output dir: %w", err)
	}
	tmpFile, err := os.CreateTemp(filepath.Dir(outPath), ".ambra-bif-*.bif.tmp")
	if err != nil {
		return fmt.Errorf("create bif temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	var header [bifHeaderSize]byte
	copy(header[0:8], bifMagic[:])
	binary.LittleEndian.PutUint32(header[8:12], 0)
	binary.LittleEndian.PutUint32(header[12:16], uint32(len(valid)))
	binary.LittleEndian.PutUint32(header[16:20], intervalMs)

	if _, err := tmpFile.Write(header[:]); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("write bif header: %w", err)
	}

	indexBuf := make([]byte, (len(valid)+1)*bifIndexEntrySize)
	curOffset := uint32(dataOffset64)
	for i, ff := range valid {
		pos := i * bifIndexEntrySize
		binary.LittleEndian.PutUint32(indexBuf[pos:pos+4], uint32(i))
		binary.LittleEndian.PutUint32(indexBuf[pos+4:pos+8], curOffset)
		curOffset += uint32(ff.size)
	}
	endPos := len(valid) * bifIndexEntrySize
	binary.LittleEndian.PutUint32(indexBuf[endPos:endPos+4], bifEndIndexMarker)
	binary.LittleEndian.PutUint32(indexBuf[endPos+4:endPos+8], curOffset)

	if _, err := tmpFile.Write(indexBuf); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("write bif index table: %w", err)
	}

	copyBuf := make([]byte, 32*1024)
	for _, ff := range valid {
		in, openErr := os.Open(ff.path)
		if openErr != nil {
			_ = tmpFile.Close()
			return fmt.Errorf("open bif frame %s: %w", filepath.Base(ff.path), openErr)
		}
		written, copyErr := io.CopyBuffer(tmpFile, io.LimitReader(in, ff.size), copyBuf)
		_ = in.Close()
		if copyErr != nil {
			_ = tmpFile.Close()
			return fmt.Errorf("copy bif frame %s: %w", filepath.Base(ff.path), copyErr)
		}
		if written != ff.size {
			_ = tmpFile.Close()
			return fmt.Errorf("short read on bif frame %s: wrote %d of %d", filepath.Base(ff.path), written, ff.size)
		}
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close bif temp file: %w", err)
	}
	if err := os.Rename(tmpPath, outPath); err != nil {
		return fmt.Errorf("commit bif file: %w", err)
	}
	cleanup = false
	return nil
}

// WriteBIF atomically writes a standard Roku/Emby BIF v0 archive to outPath.
func WriteBIF(outPath string, intervalMs uint32, frames [][]byte) error {
	if len(frames) == 0 {
		return fmt.Errorf("cannot write BIF with 0 frames")
	}
	if intervalMs == 0 {
		intervalMs = DefaultBIFIntervalSec * 1000
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o700); err != nil {
		return fmt.Errorf("create bif output dir: %w", err)
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(outPath), ".ambra-bif-*.bif.tmp")
	if err != nil {
		return fmt.Errorf("create bif temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	var header [bifHeaderSize]byte
	copy(header[0:8], bifMagic[:])
	binary.LittleEndian.PutUint32(header[8:12], 0) // BIF version 0
	binary.LittleEndian.PutUint32(header[12:16], uint32(len(frames)))
	binary.LittleEndian.PutUint32(header[16:20], intervalMs)

	if _, err := tmpFile.Write(header[:]); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("write bif header: %w", err)
	}

	dataOffset := uint32(bifHeaderSize + (len(frames)+1)*bifIndexEntrySize)
	indexBuf := make([]byte, (len(frames)+1)*bifIndexEntrySize)
	curOffset := dataOffset
	for i, frame := range frames {
		pos := i * bifIndexEntrySize
		binary.LittleEndian.PutUint32(indexBuf[pos:pos+4], uint32(i))
		binary.LittleEndian.PutUint32(indexBuf[pos+4:pos+8], curOffset)
		curOffset += uint32(len(frame))
	}
	// Sentinel end-of-table entry
	endPos := len(frames) * bifIndexEntrySize
	binary.LittleEndian.PutUint32(indexBuf[endPos:endPos+4], bifEndIndexMarker)
	binary.LittleEndian.PutUint32(indexBuf[endPos+4:endPos+8], curOffset)

	if _, err := tmpFile.Write(indexBuf); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("write bif index table: %w", err)
	}
	for _, frame := range frames {
		if _, err := tmpFile.Write(frame); err != nil {
			_ = tmpFile.Close()
			return fmt.Errorf("write bif frame: %w", err)
		}
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close bif temp file: %w", err)
	}
	if err := os.Rename(tmpPath, outPath); err != nil {
		return fmt.Errorf("commit bif file: %w", err)
	}
	cleanup = false
	return nil
}

// ReadBIFMetadata parses a BIF v0 header and index timestamps without loading JPEG frame bodies.
func ReadBIFMetadata(bifPath string) (*BIFMetadata, error) {
	f, err := os.Open(bifPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var header [bifHeaderSize]byte
	if _, err := io.ReadFull(f, header[:]); err != nil {
		return nil, fmt.Errorf("read bif header: %w", err)
	}
	if !bytes.Equal(header[0:8], bifMagic[:]) {
		return nil, fmt.Errorf("invalid bif magic header")
	}
	version := binary.LittleEndian.Uint32(header[8:12])
	count := binary.LittleEndian.Uint32(header[12:16])
	multMs := binary.LittleEndian.Uint32(header[16:20])
	if multMs == 0 {
		multMs = 1000
	}
	// Guard against malformed huge counts (a 24h video at 1fps is 86,400 frames).
	const maxFrames = 100_000
	if count > maxFrames {
		return nil, fmt.Errorf("bif frame count %d exceeds safety limit", count)
	}

	idxBytes := make([]byte, int(count)*bifIndexEntrySize)
	if _, err := io.ReadFull(f, idxBytes); err != nil {
		return nil, fmt.Errorf("read bif index table: %w", err)
	}
	timestamps := make([]uint32, 0, count)
	for i := uint32(0); i < count; i++ {
		pos := int(i) * bifIndexEntrySize
		tsIdx := binary.LittleEndian.Uint32(idxBytes[pos : pos+4])
		if tsIdx == bifEndIndexMarker {
			break
		}
		timestamps = append(timestamps, tsIdx*multMs)
	}
	return &BIFMetadata{
		Version:               version,
		ImageCount:            uint32(len(timestamps)),
		TimestampMultiplierMs: multMs,
		FrameTimestampsMs:     timestamps,
	}, nil
}

// ParseBIFWidthParam parses a query width parameter into a safe BIF width.
func ParseBIFWidthParam(raw string) int {
	w, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || w <= 0 {
		return DefaultBIFWidth
	}
	if w < 120 {
		return 120
	}
	if w > 1280 {
		return 1280
	}
	return w
}
