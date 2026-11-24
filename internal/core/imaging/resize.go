package imaging

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"math"
	"strings"
)

const defaultJPEGQuality = 85

// Options configures dynamic image resizing, cropping, quality, and output format.
type Options struct {
	MaxWidth   int
	MaxHeight  int
	Width      int
	Height     int
	FillWidth  int
	FillHeight int
	Quality    int
	Format     string
}

// Normalized returns a sanitized copy of Options with bounds clamped and format canonicalized.
func (o Options) Normalized() Options {
	out := o
	if out.MaxWidth < 0 {
		out.MaxWidth = 0
	}
	if out.MaxHeight < 0 {
		out.MaxHeight = 0
	}
	if out.Width < 0 {
		out.Width = 0
	}
	if out.Height < 0 {
		out.Height = 0
	}
	if out.FillWidth < 0 {
		out.FillWidth = 0
	}
	if out.FillHeight < 0 {
		out.FillHeight = 0
	}
	if out.Quality < 0 {
		out.Quality = 0
	} else if out.Quality > 100 {
		out.Quality = 100
	}
	switch strings.ToLower(strings.TrimSpace(out.Format)) {
	case "jpg", "jpeg":
		out.Format = "jpeg"
	case "png":
		out.Format = "png"
	default:
		out.Format = ""
	}
	return out
}

// IsZero reports whether no resize, crop, quality, or format transformation is requested.
func (o Options) IsZero() bool {
	n := o.Normalized()
	return n.MaxWidth == 0 &&
		n.MaxHeight == 0 &&
		n.Width == 0 &&
		n.Height == 0 &&
		n.FillWidth == 0 &&
		n.FillHeight == 0 &&
		n.Quality == 0 &&
		n.Format == ""
}

// CacheSignature returns a deterministic canonical key component for normalized options.
func (o Options) CacheSignature() string {
	n := o.Normalized()
	return fmt.Sprintf("mw=%d&mh=%d&w=%d&h=%d&fw=%d&fh=%d&q=%d&f=%s",
		n.MaxWidth, n.MaxHeight, n.Width, n.Height,
		n.FillWidth, n.FillHeight, n.Quality, n.Format)
}

// computeTargetDimensions resolves target dimensions and whether center-crop fill mode is active.
func computeTargetDimensions(srcW, srcH int, opts Options) (dstW, dstH int, crop bool) {
	if srcW <= 0 || srcH <= 0 {
		return srcW, srcH, false
	}

	// FillWidth + FillHeight requests exact center-cropped fill dimensions.
	if opts.FillWidth > 0 && opts.FillHeight > 0 {
		if srcW == opts.FillWidth && srcH == opts.FillHeight {
			return srcW, srcH, false
		}
		return opts.FillWidth, opts.FillHeight, true
	}

	limW := minPositive(opts.Width, opts.MaxWidth)
	limH := minPositive(opts.Height, opts.MaxHeight)
	if limW == 0 && limH == 0 {
		return srcW, srcH, false
	}

	scale := 1.0
	if limW > 0 && srcW > limW {
		if s := float64(limW) / float64(srcW); s < scale {
			scale = s
		}
	}
	if limH > 0 && srcH > limH {
		if s := float64(limH) / float64(srcH); s < scale {
			scale = s
		}
	}
	if scale >= 1.0 {
		return srcW, srcH, false
	}

	dstW = max(1, int(math.Round(float64(srcW)*scale)))
	dstH = max(1, int(math.Round(float64(srcH)*scale)))
	return dstW, dstH, false
}

func minPositive(a, b int) int {
	if a <= 0 {
		return b
	}
	if b <= 0 {
		return a
	}
	if a < b {
		return a
	}
	return b
}

// toNRGBA converts any image.Image into a tightly-originated *image.NRGBA for fast pixel access.
func toNRGBA(src image.Image) *image.NRGBA {
	b := src.Bounds()
	if nrgba, ok := src.(*image.NRGBA); ok && b.Min.X == 0 && b.Min.Y == 0 {
		return nrgba
	}
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
	return dst
}

// ResizeBilinear resizes src to (dstW, dstH) using pure-Go bilinear interpolation.
func ResizeBilinear(src image.Image, dstW, dstH int) *image.NRGBA {
	nrgba := toNRGBA(src)
	srcW := nrgba.Bounds().Dx()
	srcH := nrgba.Bounds().Dy()
	if dstW <= 0 {
		dstW = 1
	}
	if dstH <= 0 {
		dstH = 1
	}
	if srcW == 0 || srcH == 0 {
		return image.NewNRGBA(image.Rect(0, 0, dstW, dstH))
	}

	dst := image.NewNRGBA(image.Rect(0, 0, dstW, dstH))
	xRatio := float64(srcW) / float64(dstW)
	yRatio := float64(srcH) / float64(dstH)
	maxSrcX := srcW - 1
	maxSrcY := srcH - 1

	for y := 0; y < dstH; y++ {
		sy := (float64(y)+0.5)*yRatio - 0.5
		if sy < 0 {
			sy = 0
		} else if sy > float64(maxSrcY) {
			sy = float64(maxSrcY)
		}
		y0 := int(math.Floor(sy))
		y1 := min(y0+1, maxSrcY)
		fy := sy - float64(y0)
		wy0 := 1.0 - fy
		wy1 := fy

		row0 := y0 * nrgba.Stride
		row1 := y1 * nrgba.Stride
		dstRow := y * dst.Stride

		for x := 0; x < dstW; x++ {
			sx := (float64(x)+0.5)*xRatio - 0.5
			if sx < 0 {
				sx = 0
			} else if sx > float64(maxSrcX) {
				sx = float64(maxSrcX)
			}
			x0 := int(math.Floor(sx))
			x1 := min(x0+1, maxSrcX)
			fx := sx - float64(x0)
			wx0 := 1.0 - fx
			wx1 := fx

			w00 := wx0 * wy0
			w10 := wx1 * wy0
			w01 := wx0 * wy1
			w11 := wx1 * wy1

			i00 := row0 + x0*4
			i10 := row0 + x1*4
			i01 := row1 + x0*4
			i11 := row1 + x1*4
			di := dstRow + x*4

			for c := 0; c < 4; c++ {
				v := float64(nrgba.Pix[i00+c])*w00 +
					float64(nrgba.Pix[i10+c])*w10 +
					float64(nrgba.Pix[i01+c])*w01 +
					float64(nrgba.Pix[i11+c])*w11
				if v < 0 {
					v = 0
				} else if v > 255 {
					v = 255
				}
				dst.Pix[di+c] = uint8(math.Round(v))
			}
		}
	}
	return dst
}

// CropAndResizeBilinear center-crops src to match the target aspect ratio and resizes to (dstW, dstH).
func CropAndResizeBilinear(src image.Image, dstW, dstH int) *image.NRGBA {
	nrgba := toNRGBA(src)
	srcW := nrgba.Bounds().Dx()
	srcH := nrgba.Bounds().Dy()
	if dstW <= 0 {
		dstW = 1
	}
	if dstH <= 0 {
		dstH = 1
	}
	if srcW == 0 || srcH == 0 {
		return image.NewNRGBA(image.Rect(0, 0, dstW, dstH))
	}

	srcRatio := float64(srcW) / float64(srcH)
	dstRatio := float64(dstW) / float64(dstH)

	var cropW, cropH, cropX, cropY int
	if srcRatio > dstRatio {
		// Source is wider than target: crop left and right.
		cropH = srcH
		cropW = max(1, int(math.Round(float64(srcH)*dstRatio)))
		if cropW > srcW {
			cropW = srcW
		}
		cropX = (srcW - cropW) / 2
		cropY = 0
	} else {
		// Source is taller than (or equal to) target: crop top and bottom.
		cropW = srcW
		cropH = max(1, int(math.Round(float64(srcW)/dstRatio)))
		if cropH > srcH {
			cropH = srcH
		}
		cropX = 0
		cropY = (srcH - cropH) / 2
	}

	sub := nrgba.SubImage(image.Rect(cropX, cropY, cropX+cropW, cropY+cropH))
	return ResizeBilinear(sub, dstW, dstH)
}

// ProcessImage decodes, optionally resizes/crops, and encodes image bytes according to opts.
// When no resizing or transcoding is required, it returns the original bytes untouched to avoid recompression loss.
func ProcessImage(srcData []byte, srcContentType string, opts Options) (outData []byte, outContentType string, transformed bool, err error) {
	opts = opts.Normalized()
	if opts.IsZero() {
		return srcData, srcContentType, false, nil
	}

	img, decodedFormat, err := image.Decode(bytes.NewReader(srcData))
	if err != nil {
		return nil, "", false, fmt.Errorf("decode image: %w", err)
	}

	b := img.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	dstW, dstH, crop := computeTargetDimensions(srcW, srcH, opts)

	outFormat := opts.Format
	if outFormat == "" {
		if decodedFormat == "png" && opts.Quality == 0 {
			outFormat = "png"
		} else {
			outFormat = "jpeg"
		}
	}

	needsResize := crop || dstW != srcW || dstH != srcH
	needsTranscode := (opts.Format != "" && opts.Format != decodedFormat) ||
		(opts.Quality > 0 && outFormat == "jpeg")

	if !needsResize && !needsTranscode {
		return srcData, srcContentType, false, nil
	}

	var result image.Image = img
	if crop {
		result = CropAndResizeBilinear(img, dstW, dstH)
	} else if dstW != srcW || dstH != srcH {
		result = ResizeBilinear(img, dstW, dstH)
	}

	var buf bytes.Buffer
	if outFormat == "png" {
		if err := png.Encode(&buf, result); err != nil {
			return nil, "", false, fmt.Errorf("encode png: %w", err)
		}
		return buf.Bytes(), "image/png", true, nil
	}

	q := opts.Quality
	if q <= 0 {
		q = defaultJPEGQuality
	}
	if err := jpeg.Encode(&buf, result, &jpeg.Options{Quality: q}); err != nil {
		return nil, "", false, fmt.Errorf("encode jpeg: %w", err)
	}
	return buf.Bytes(), "image/jpeg", true, nil
}
