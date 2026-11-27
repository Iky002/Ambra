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
	webpDecodeTimeout           = 15 * time.Second
	maxWebPHeaderScan           = 64 << 10 // scan up to 64 KiB for RIFF metadata chunks before VP8/VP8L/VP8X
	maxWebPIntermediatePNGBytes = 64 << 20 // 64 MiB hard cap on ffmpeg intermediate PNG stdout
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

func parseVP8BitstreamHeader(r io.Reader, chunkSize int64) (w, h int, err error) {
	if chunkSize < 10 {
		return 0, 0, errors.New("webp: truncated VP8 chunk")
	}
	var buf [10]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return 0, 0, fmt.Errorf("webp: read VP8 frame header: %w", err)
	}
	// Bit 0 of frame tag == 0 indicates a keyframe; bytes 3..5 must be 0x9d 0x01 0x2a.
	if (buf[0]&0x01) != 0 || buf[3] != 0x9d || buf[4] != 0x01 || buf[5] != 0x2a {
		return 0, 0, errors.New("webp: invalid VP8 keyframe start code")
	}
	w = int(binary.LittleEndian.Uint16(buf[6:8]) & 0x3fff)
	h = int(binary.LittleEndian.Uint16(buf[8:10]) & 0x3fff)
	if w <= 0 || h <= 0 {
		return 0, 0, errors.New("webp: invalid VP8 dimensions")
	}
	return w, h, nil
}

func parseVP8LBitstreamHeader(r io.Reader, chunkSize int64) (w, h int, alphaUsed bool, err error) {
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
	w = int(bits&0x3fff) + 1
	h = int((bits>>14)&0x3fff) + 1
	alphaUsed = ((bits >> 28) & 0x01) != 0
	return w, h, alphaUsed, nil
}

func parseANMFSubchunks(r io.Reader, payloadBytes int64) (maxW, maxH int, hasAlpha bool, err error) {
	lr := io.LimitReader(r, payloadBytes)
	for {
		var subHdr [8]byte
		if _, err := io.ReadFull(lr, subHdr[:]); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				break
			}
			return 0, 0, false, err
		}
		subCC := string(subHdr[0:4])
		subSize := int64(binary.LittleEndian.Uint32(subHdr[4:8]))
		paddedSub := subSize
		if subSize%2 != 0 {
			paddedSub++
		}
		switch subCC {
		case "VP8 ":
			fw, fh, ferr := parseVP8BitstreamHeader(lr, subSize)
			if ferr != nil {
				return 0, 0, false, ferr
			}
			if fw > maxW {
				maxW = fw
			}
			if fh > maxH {
				maxH = fh
			}
			if rem := paddedSub - 10; rem > 0 {
				if _, sErr := io.CopyN(io.Discard, lr, rem); sErr != nil {
					return maxW, maxH, hasAlpha, nil
				}
			}
		case "VP8L":
			fw, fh, fAlpha, ferr := parseVP8LBitstreamHeader(lr, subSize)
			if ferr != nil {
				return 0, 0, false, ferr
			}
			if fw > maxW {
				maxW = fw
			}
			if fh > maxH {
				maxH = fh
			}
			hasAlpha = hasAlpha || fAlpha
			if rem := paddedSub - 5; rem > 0 {
				if _, sErr := io.CopyN(io.Discard, lr, rem); sErr != nil {
					return maxW, maxH, hasAlpha, nil
				}
			}
		case "ALPH":
			hasAlpha = true
			if paddedSub > 0 {
				if _, sErr := io.CopyN(io.Discard, lr, paddedSub); sErr != nil {
					return maxW, maxH, hasAlpha, nil
				}
			}
		default:
			if paddedSub > 0 {
				if _, sErr := io.CopyN(io.Discard, lr, paddedSub); sErr != nil {
					return maxW, maxH, hasAlpha, nil
				}
			}
		}
	}
	// Drain any remaining bytes in the ANMF payload.
	_, _ = io.Copy(io.Discard, lr)
	return maxW, maxH, hasAlpha, nil
}

// ParseWebPDimensions parses a RIFF WebP header (supporting VP8, VP8L, and VP8X + sub-chunks)
// in pure Go without external tools, returning max(canvas, bitstream frame) pixel dimensions and alpha presence.
func ParseWebPDimensions(r io.Reader) (width, height int, hasAlpha bool, err error) {
	var riff [12]byte
	if _, err := io.ReadFull(r, riff[:]); err != nil {
		return 0, 0, false, fmt.Errorf("webp: short RIFF header: %w", err)
	}
	if string(riff[0:4]) != "RIFF" || string(riff[8:12]) != "WEBP" {
		return 0, 0, false, errors.New("webp: invalid RIFF WEBP signature")
	}

	var (
		scanned  int64 = 12
		seenVP8X bool
		maxW     int
		maxH     int
	)

	for {
		scanLimit := int64(maxWebPHeaderScan)
		if seenVP8X {
			scanLimit = maxImageFileSize
		}
		if scanned >= scanLimit {
			break
		}

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
		paddedSize := chunkSize
		if chunkSize%2 != 0 {
			paddedSize++
		}

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
			hasAlpha = hasAlpha || (flags&0x10) != 0
			wMinus1 := uint32(buf[4]) | uint32(buf[5])<<8 | uint32(buf[6])<<16
			hMinus1 := uint32(buf[7]) | uint32(buf[8])<<8 | uint32(buf[9])<<16
			cw := int(wMinus1) + 1
			ch := int(hMinus1) + 1
			if cw <= 0 || ch <= 0 {
				return 0, 0, false, errors.New("webp: invalid VP8X dimensions")
			}
			seenVP8X = true
			if cw > maxW {
				maxW = cw
			}
			if ch > maxH {
				maxH = ch
			}
			scanned += 10
			if rem := paddedSize - 10; rem > 0 {
				if _, err := io.CopyN(io.Discard, r, rem); err != nil {
					if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
						return maxW, maxH, hasAlpha, nil
					}
					return 0, 0, false, fmt.Errorf("webp: skip VP8X tail: %w", err)
				}
				scanned += rem
			}

		case "VP8 ":
			fw, fh, ferr := parseVP8BitstreamHeader(r, chunkSize)
			if ferr != nil {
				return 0, 0, false, ferr
			}
			if !seenVP8X {
				return fw, fh, false, nil
			}
			if fw > maxW {
				maxW = fw
			}
			if fh > maxH {
				maxH = fh
			}
			scanned += 10
			if rem := paddedSize - 10; rem > 0 {
				if _, sErr := io.CopyN(io.Discard, r, rem); sErr != nil {
					return maxW, maxH, hasAlpha, nil
				}
				scanned += rem
			}

		case "VP8L":
			fw, fh, fAlpha, ferr := parseVP8LBitstreamHeader(r, chunkSize)
			if ferr != nil {
				return 0, 0, false, ferr
			}
			if !seenVP8X {
				return fw, fh, fAlpha, nil
			}
			if fw > maxW {
				maxW = fw
			}
			if fh > maxH {
				maxH = fh
			}
			hasAlpha = hasAlpha || fAlpha
			scanned += 5
			if rem := paddedSize - 5; rem > 0 {
				if _, sErr := io.CopyN(io.Discard, r, rem); sErr != nil {
					return maxW, maxH, hasAlpha, nil
				}
				scanned += rem
			}

		case "ANMF":
			if !seenVP8X {
				return 0, 0, false, errors.New("webp: ANMF chunk without VP8X header")
			}
			if chunkSize < 16 {
				return 0, 0, false, errors.New("webp: truncated ANMF chunk")
			}
			var anmfHdr [16]byte
			if _, err := io.ReadFull(r, anmfHdr[:]); err != nil {
				return 0, 0, false, fmt.Errorf("webp: read ANMF header: %w", err)
			}
			awMinus1 := uint32(anmfHdr[6]) | uint32(anmfHdr[7])<<8 | uint32(anmfHdr[8])<<16
			ahMinus1 := uint32(anmfHdr[9]) | uint32(anmfHdr[10])<<8 | uint32(anmfHdr[11])<<16
			aw := int(awMinus1) + 1
			ah := int(ahMinus1) + 1
			if aw <= 0 || ah <= 0 {
				return 0, 0, false, errors.New("webp: invalid ANMF dimensions")
			}
			if aw > maxW {
				maxW = aw
			}
			if ah > maxH {
				maxH = ah
			}
			subW, subH, subAlpha, subErr := parseANMFSubchunks(r, chunkSize-16)
			if subErr != nil {
				return 0, 0, false, subErr
			}
			if subW > maxW {
				maxW = subW
			}
			if subH > maxH {
				maxH = subH
			}
			hasAlpha = hasAlpha || subAlpha
			scanned += chunkSize
			if chunkSize%2 != 0 {
				_, _ = io.CopyN(io.Discard, r, 1)
				scanned++
			}

		default:
			if paddedSize < 0 || (!seenVP8X && scanned+paddedSize > maxWebPHeaderScan) || scanned+paddedSize > maxImageFileSize {
				return 0, 0, false, errors.New("webp: metadata before bitstream exceeds scan limit")
			}
			if _, err := io.CopyN(io.Discard, r, paddedSize); err != nil {
				if seenVP8X && (errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)) {
					return maxW, maxH, hasAlpha, nil
				}
				return 0, 0, false, fmt.Errorf("webp: skip %q chunk: %w", fourCC, err)
			}
			scanned += paddedSize
		}
	}

	if seenVP8X && maxW > 0 && maxH > 0 {
		return maxW, maxH, hasAlpha, nil
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

type boundedStdoutBuffer struct {
	buf      bytes.Buffer
	maxBytes int
	exceeded bool
	cancel   context.CancelFunc
}

func (b *boundedStdoutBuffer) Write(p []byte) (int, error) {
	rem := b.maxBytes - b.buf.Len()
	if len(p) > rem {
		if rem > 0 {
			_, _ = b.buf.Write(p[:rem])
		}
		b.exceeded = true
		if b.cancel != nil {
			b.cancel()
		}
		return 0, errors.New("webp: intermediate png output exceeds byte limit")
	}
	return b.buf.Write(p)
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
	stdout := &boundedStdoutBuffer{
		maxBytes: maxWebPIntermediatePNGBytes,
		cancel:   cancel,
	}
	var stderr bytes.Buffer
	cmd.Stdout = stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	if stdout.exceeded {
		return nil, fmt.Errorf("webp: intermediate png output exceeds %d bytes", maxWebPIntermediatePNGBytes)
	}
	if runErr != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg != "" {
			return nil, fmt.Errorf("webp: ffmpeg decode failed (%s): %w", errMsg, runErr)
		}
		return nil, fmt.Errorf("webp: ffmpeg decode failed: %w", runErr)
	}

	pngBytes := stdout.buf.Bytes()
	pngCfg, err := png.DecodeConfig(bytes.NewReader(pngBytes))
	if err != nil {
		return nil, fmt.Errorf("webp: decode intermediate png config: %w", err)
	}
	if pngCfg.Width <= 0 || pngCfg.Height <= 0 || int64(pngCfg.Width)*int64(pngCfg.Height) > maxSourcePixels {
		return nil, fmt.Errorf("webp: decoded frame dimensions %dx%d exceed safety limit", pngCfg.Width, pngCfg.Height)
	}

	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return nil, fmt.Errorf("webp: decode intermediate png: %w", err)
	}
	return img, nil
}
