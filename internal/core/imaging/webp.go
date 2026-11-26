package imaging

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	webpDecodeTimeout = 15 * time.Second
	maxWebPHeaderScan = 64 << 10 // scan up to 64 KiB for RIFF metadata chunks before VP8/VP8L/VP8X
)

var (
	ffmpegPathMu       sync.RWMutex
	overrideFFmpegPath string
)

func init() {
	image.RegisterFormat("webp", "RIFF????WEBP", DecodeWebP, DecodeWebPConfig)
}

// SetFFmpegPath sets a custom ffmpeg binary path used to decode WebP images into memory.
func SetFFmpegPath(path string) {
	ffmpegPathMu.Lock()
	overrideFFmpegPath = strings.TrimSpace(path)
	ffmpegPathMu.Unlock()
}

func resolveFFmpegBinary() (string, error) {
	ffmpegPathMu.RLock()
	configured := overrideFFmpegPath
	ffmpegPathMu.RUnlock()

	candidates := make([]string, 0, 6)
	if configured != "" {
		candidates = append(candidates, configured)
	}
	if env := strings.TrimSpace(os.Getenv("AMBRA_FFMPEG_PATH")); env != "" {
		candidates = append(candidates, env)
	}
	if p, err := exec.LookPath("ffmpeg"); err == nil && p != "" {
		candidates = append(candidates, p)
	}
	candidates = append(candidates,
		"/opt/homebrew/bin/ffmpeg",
		"/usr/local/bin/ffmpeg",
		"/usr/bin/ffmpeg",
	)

	for _, c := range candidates {
		if c == "" {
			continue
		}
		if p, err := exec.LookPath(c); err == nil && p != "" {
			return p, nil
		}
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c, nil
		}
	}
	return "", errors.New("ffmpeg binary not found for WebP decoding")
}

// ParseWebPDimensions parses a RIFF WebP header (supporting VP8, VP8L, and VP8X chunks)
// in pure Go without external tools, returning exact pixel dimensions and alpha presence.
func ParseWebPDimensions(r io.Reader) (width, height int, hasAlpha bool, err error) {
	var riff [12]byte
	if _, err := io.ReadFull(r, riff[:]); err != nil {
		return 0, 0, false, fmt.Errorf("webp: short RIFF header: %w", err)
	}
	if string(riff[0:4]) != "RIFF" || string(riff[8:12]) != "WEBP" {
		return 0, 0, false, errors.New("webp: invalid RIFF WEBP signature")
	}

	scanned := 12
	for scanned < maxWebPHeaderScan {
		var chunkHdr [8]byte
		if _, err := io.ReadFull(r, chunkHdr[:]); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return 0, 0, false, fmt.Errorf("webp: read chunk header: %w", err)
		}
		fourCC := string(chunkHdr[0:4])
		chunkSize := int64(binary.LittleEndian.Uint32(chunkHdr[4:8]))
		scanned += 8

		switch fourCC {
		case "VP8X":
			if chunkSize < 10 {
				return 0, 0, false, errors.New("webp: truncated VP8X chunk")
			}
			var buf [10]byte
			if _, err := io.ReadFull(r, buf[:]); err != nil {
				return 0, 0, false, fmt.Errorf("webp: read VP8X payload: %w", err)
			}
			flags := buf[0]
			hasAlpha = (flags & 0x10) != 0
			wMinus1 := uint32(buf[4]) | uint32(buf[5])<<8 | uint32(buf[6])<<16
			hMinus1 := uint32(buf[7]) | uint32(buf[8])<<8 | uint32(buf[9])<<16
			w := int(wMinus1) + 1
			h := int(hMinus1) + 1
			if w <= 0 || h <= 0 {
				return 0, 0, false, errors.New("webp: invalid VP8X dimensions")
			}
			return w, h, hasAlpha, nil

		case "VP8 ":
			if chunkSize < 10 {
				return 0, 0, false, errors.New("webp: truncated VP8 chunk")
			}
			var buf [10]byte
			if _, err := io.ReadFull(r, buf[:]); err != nil {
				return 0, 0, false, fmt.Errorf("webp: read VP8 frame header: %w", err)
			}
			// Bit 0 of frame tag == 0 indicates a keyframe; bytes 3..5 must be 0x9d 0x01 0x2a.
			if (buf[0]&0x01) != 0 || buf[3] != 0x9d || buf[4] != 0x01 || buf[5] != 0x2a {
				return 0, 0, false, errors.New("webp: invalid VP8 keyframe start code")
			}
			w := int(binary.LittleEndian.Uint16(buf[6:8]) & 0x3fff)
			h := int(binary.LittleEndian.Uint16(buf[8:10]) & 0x3fff)
			if w <= 0 || h <= 0 {
				return 0, 0, false, errors.New("webp: invalid VP8 dimensions")
			}
			return w, h, false, nil

		case "VP8L":
			if chunkSize < 5 {
				return 0, 0, false, errors.New("webp: truncated VP8L chunk")
			}
			var buf [5]byte
			if _, err := io.ReadFull(r, buf[:]); err != nil {
				return 0, 0, false, fmt.Errorf("webp: read VP8L header: %w", err)
			}
			if buf[0] != 0x2f {
				return 0, 0, false, errors.New("webp: invalid VP8L signature byte")
			}
			bits := binary.LittleEndian.Uint32(buf[1:5])
			w := int(bits&0x3fff) + 1
			h := int((bits>>14)&0x3fff) + 1
			alphaUsed := ((bits >> 28) & 0x01) != 0
			return w, h, alphaUsed, nil

		default:
			skip := chunkSize
			if chunkSize%2 != 0 {
				skip++
			}
			if skip < 0 || int64(scanned)+skip > maxWebPHeaderScan {
				return 0, 0, false, errors.New("webp: metadata before bitstream exceeds scan limit")
			}
			if _, err := io.CopyN(io.Discard, r, skip); err != nil {
				return 0, 0, false, fmt.Errorf("webp: skip %q chunk: %w", fourCC, err)
			}
			scanned += int(skip)
		}
	}

	return 0, 0, false, errors.New("webp: no VP8/VP8L/VP8X image header found")
}

// DecodeWebPConfig returns the exact dimensions and color model of a WebP image without spawning external processes.
func DecodeWebPConfig(r io.Reader) (image.Config, error) {
	w, h, hasAlpha, err := ParseWebPDimensions(r)
	if err != nil {
		return image.Config{}, err
	}
	cm := color.YCbCrModel
	if hasAlpha {
		cm = color.NRGBAModel
	}
	return image.Config{
		ColorModel: cm,
		Width:      w,
		Height:     h,
	}, nil
}

// DecodeWebP decodes a RIFF WebP image into an in-memory image.Image via a single bounded ffmpeg pipe invocation.
func DecodeWebP(r io.Reader) (image.Image, error) {
	raw, err := io.ReadAll(io.LimitReader(r, maxImageFileSize+1))
	if err != nil {
		return nil, fmt.Errorf("webp: read input: %w", err)
	}
	if int64(len(raw)) > maxImageFileSize {
		return nil, fmt.Errorf("webp: input exceeds %d bytes", maxImageFileSize)
	}

	cfg, err := DecodeWebPConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > maxSourcePixels {
		return nil, fmt.Errorf("webp: dimensions %dx%d exceed safety limit", cfg.Width, cfg.Height)
	}

	ffmpegBin, err := resolveFFmpegBinary()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), webpDecodeTimeout)
	defer cancel()

	cmd := exec.CommandContext(
		ctx,
		ffmpegBin,
		"-nostdin",
		"-hide_banner",
		"-loglevel", "error",
		"-i", "pipe:0",
		"-frames:v", "1",
		"-f", "image2pipe",
		"-vcodec", "png",
		"pipe:1",
	)
	cmd.Stdin = bytes.NewReader(raw)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg != "" {
			return nil, fmt.Errorf("webp: ffmpeg decode failed (%s): %w", errMsg, err)
		}
		return nil, fmt.Errorf("webp: ffmpeg decode failed: %w", err)
	}

	img, err := png.Decode(&stdout)
	if err != nil {
		return nil, fmt.Errorf("webp: decode intermediate png: %w", err)
	}
	return img, nil
}
