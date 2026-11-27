package imaging

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func buildChunk(fourCC string, payload []byte) []byte {
	var out []byte
	out = append(out, fourCC...)
	var sz [4]byte
	binary.LittleEndian.PutUint32(sz[:], uint32(len(payload)))
	out = append(out, sz[:]...)
	out = append(out, payload...)
	if len(payload)%2 != 0 {
		out = append(out, 0x00)
	}
	return out
}

func buildRIFFWebP(chunks ...[]byte) []byte {
	var body []byte
	body = append(body, "WEBP"...)
	for _, c := range chunks {
		body = append(body, c...)
	}
	var out []byte
	out = append(out, "RIFF"...)
	var sz [4]byte
	binary.LittleEndian.PutUint32(sz[:], uint32(len(body)))
	out = append(out, sz[:]...)
	out = append(out, body...)
	return out
}

func buildVP8KeyframePayload(w, h uint16, extra []byte) []byte {
	var hdr [10]byte
	hdr[0] = 0x00 // keyframe bit 0 == 0
	hdr[3] = 0x9d
	hdr[4] = 0x01
	hdr[5] = 0x2a
	binary.LittleEndian.PutUint16(hdr[6:8], w)
	binary.LittleEndian.PutUint16(hdr[8:10], h)
	return append(hdr[:], extra...)
}

func buildVP8LPayload(w, h uint32, alpha bool, extra []byte) []byte {
	var hdr [5]byte
	hdr[0] = 0x2f
	bits := ((w - 1) & 0x3fff) | (((h - 1) & 0x3fff) << 14)
	if alpha {
		bits |= 1 << 28
	}
	binary.LittleEndian.PutUint32(hdr[1:5], bits)
	return append(hdr[:], extra...)
}

func buildVP8XPayload(w, h uint32, alpha bool, extra []byte) []byte {
	var hdr [10]byte
	if alpha {
		hdr[0] = 0x10
	}
	cw := w - 1
	ch := h - 1
	hdr[4], hdr[5], hdr[6] = byte(cw), byte(cw>>8), byte(cw>>16)
	hdr[7], hdr[8], hdr[9] = byte(ch), byte(ch>>8), byte(ch>>16)
	return append(hdr[:], extra...)
}

func TestImagingCovV0112_WebPParserAndANMFSubchunks(t *testing.T) {
	// 1. Short/invalid RIFF signatures and empty chunks
	if _, _, _, err := ParseWebPDimensions(strings.NewReader("RIFF")); err == nil {
		t.Fatal("expected short RIFF error")
	}
	if _, _, _, err := ParseWebPDimensions(strings.NewReader("RIFF\x04\x00\x00\x00NOPE")); err == nil {
		t.Fatal("expected invalid signature error")
	}
	if _, _, _, err := ParseWebPDimensions(bytes.NewReader(buildRIFFWebP())); err == nil {
		t.Fatal("expected missing image header error")
	}

	// 2. VP8 & VP8L header validation errors
	if _, _, err := parseVP8BitstreamHeader(bytes.NewReader([]byte{1, 2}), 5); err == nil {
		t.Fatal("expected truncated VP8 chunk error")
	}
	if _, _, err := parseVP8BitstreamHeader(bytes.NewReader([]byte{1, 2}), 12); err == nil {
		t.Fatal("expected short read VP8 error")
	}
	if _, _, err := parseVP8BitstreamHeader(bytes.NewReader(make([]byte, 10)), 10); err == nil {
		t.Fatal("expected invalid VP8 start code error")
	}
	if _, _, err := parseVP8BitstreamHeader(bytes.NewReader(buildVP8KeyframePayload(0, 10, nil)), 10); err == nil {
		t.Fatal("expected zero dimension VP8 error")
	}

	if _, _, _, err := parseVP8LBitstreamHeader(bytes.NewReader([]byte{0x2f}), 3); err == nil {
		t.Fatal("expected truncated VP8L error")
	}
	if _, _, _, err := parseVP8LBitstreamHeader(bytes.NewReader([]byte{0x2f}), 5); err == nil {
		t.Fatal("expected short read VP8L error")
	}
	if _, _, _, err := parseVP8LBitstreamHeader(bytes.NewReader([]byte{0x00, 1, 2, 3, 4}), 5); err == nil {
		t.Fatal("expected bad VP8L signature error")
	}

	// 3. VP8X + ANMF containing ALPH, VP8, VP8L, and custom META subchunks (with odd padding)
	var anmfHdr [16]byte
	// Frame canvas 30x40 (minus 1 = 29, 39)
	anmfHdr[6] = 29
	anmfHdr[9] = 39
	var anmfBody []byte
	anmfBody = append(anmfBody, anmfHdr[:]...)
	anmfBody = append(anmfBody, buildChunk("ALPH", []byte{0x01, 0x02, 0x03})...)
	anmfBody = append(anmfBody, buildChunk("META", []byte{0xaa})...)
	anmfBody = append(anmfBody, buildChunk("VP8 ", buildVP8KeyframePayload(64, 48, []byte{0x55}))...)
	anmfBody = append(anmfBody, buildChunk("VP8L", buildVP8LPayload(80, 90, true, []byte{0x77}))...)

	animWebP := buildRIFFWebP(
		buildChunk("VP8X", buildVP8XPayload(20, 20, false, []byte{0x99})), // odd size VP8X tail
		buildChunk("EXIF", []byte{1, 2, 3}),
		buildChunk("ANMF", anmfBody), // odd/even handled
	)
	cfg, err := DecodeWebPConfig(bytes.NewReader(animWebP))
	if err != nil {
		t.Fatalf("DecodeWebPConfig animated failed: %v", err)
	}
	if cfg.Width != 80 || cfg.Height != 90 || cfg.ColorModel != color.NRGBAModel {
		t.Fatalf("expected 80x90 NRGBAModel from ANMF max subchunks, got %+v", cfg)
	}

	// 4. VP8X + top-level VP8 and VP8L chunks
	vp8xPlusVP8 := buildRIFFWebP(
		buildChunk("VP8X", buildVP8XPayload(16, 16, false, nil)),
		buildChunk("VP8 ", buildVP8KeyframePayload(32, 24, []byte{1, 2, 3})),
	)
	if w, h, alpha, err := ParseWebPDimensions(bytes.NewReader(vp8xPlusVP8)); err != nil || w != 32 || h != 24 || alpha {
		t.Fatalf("unexpected VP8X+VP8 dims: %dx%d alpha=%v err=%v", w, h, alpha, err)
	}

	vp8xPlusVP8L := buildRIFFWebP(
		buildChunk("VP8X", buildVP8XPayload(16, 16, false, nil)),
		buildChunk("VP8L", buildVP8LPayload(40, 50, true, []byte{1, 2, 3})),
	)
	if w, h, alpha, err := ParseWebPDimensions(bytes.NewReader(vp8xPlusVP8L)); err != nil || w != 40 || h != 50 || !alpha {
		t.Fatalf("unexpected VP8X+VP8L dims: %dx%d alpha=%v err=%v", w, h, alpha, err)
	}

	// 5. ANMF error paths: ANMF without VP8X, truncated ANMF, invalid VP8 inside ANMF, oversized EXIF before VP8X
	if _, _, _, err := ParseWebPDimensions(bytes.NewReader(buildRIFFWebP(buildChunk("ANMF", make([]byte, 16))))); err == nil {
		t.Fatal("expected ANMF without VP8X error")
	}
	if _, _, _, err := ParseWebPDimensions(bytes.NewReader(buildRIFFWebP(
		buildChunk("VP8X", buildVP8XPayload(10, 10, false, nil)),
		buildChunk("ANMF", make([]byte, 8)),
	))); err == nil {
		t.Fatal("expected truncated ANMF error")
	}
	badSubANMF := append(anmfHdr[:], buildChunk("VP8 ", []byte{1, 2, 3})...)
	if _, _, _, err := ParseWebPDimensions(bytes.NewReader(buildRIFFWebP(
		buildChunk("VP8X", buildVP8XPayload(10, 10, false, nil)),
		buildChunk("ANMF", badSubANMF),
	))); err == nil {
		t.Fatal("expected invalid VP8 subchunk in ANMF error")
	}
	hugeExif := buildRIFFWebP(buildChunk("EXIF", make([]byte, maxWebPHeaderScan+16)))
	if _, _, _, err := ParseWebPDimensions(bytes.NewReader(hugeExif)); err == nil {
		t.Fatal("expected scan limit error for huge EXIF before bitstream")
	}
}

func TestImagingCovV0112_DecodeWebPAndFakeFFmpeg(t *testing.T) {
	t.Cleanup(func() { SetFFmpegPath("") })

	// Create a valid 2x2 PNG in memory for our fake ffmpeg script to emit.
	srcImg := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	srcImg.Set(0, 0, color.NRGBA{R: 255, A: 255})
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, srcImg); err != nil {
		t.Fatal(err)
	}
	pngB64 := base64.StdEncoding.EncodeToString(pngBuf.Bytes())

	dir := t.TempDir()
	writeScript := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}

	okScript := writeScript("ffmpeg-ok.sh", `printf '%s' '`+pngB64+`' | base64 -d`)
	errStderrScript := writeScript("ffmpeg-stderr.sh", `echo "corrupt webp stream" >&2; exit 1`)
	errSilentScript := writeScript("ffmpeg-silent.sh", `exit 1`)
	badPNGScript := writeScript("ffmpeg-badpng.sh", `printf 'not-a-png'`)

	validWebP := buildRIFFWebP(buildChunk("VP8 ", buildVP8KeyframePayload(2, 2, nil)))

	// 1. SetFFmpegPath to nonexistent path + AMBRA_FFMPEG_PATH fallback to okScript
	svc := New(nil)
	svc.SetFFmpegPath(filepath.Join(dir, "does-not-exist"))
	t.Setenv("AMBRA_FFMPEG_PATH", okScript)

	decoded, err := DecodeWebP(bytes.NewReader(validWebP))
	if err != nil || decoded == nil || decoded.Bounds().Dx() != 2 || decoded.Bounds().Dy() != 2 {
		t.Fatalf("DecodeWebP with fake okScript failed: img=%v err=%v", decoded, err)
	}

	// 2. WithFFmpegPath directly pointing to error/bad-png scripts
	svc.WithFFmpegPath(errStderrScript)
	t.Setenv("AMBRA_FFMPEG_PATH", "")
	if _, err := DecodeWebP(bytes.NewReader(validWebP)); err == nil || !strings.Contains(err.Error(), "corrupt webp stream") {
		t.Fatalf("expected stderr error message, got %v", err)
	}

	svc.SetFFmpegPath(errSilentScript)
	if _, err := DecodeWebP(bytes.NewReader(validWebP)); err == nil || !strings.Contains(err.Error(), "ffmpeg decode failed") {
		t.Fatalf("expected silent failure error, got %v", err)
	}

	svc.SetFFmpegPath(badPNGScript)
	if _, err := DecodeWebP(bytes.NewReader(validWebP)); err == nil || !strings.Contains(err.Error(), "decode intermediate png config") {
		t.Fatalf("expected bad png config error, got %v", err)
	}

	// 3. Oversized dimensions in VP8X rejected before invoking ffmpeg
	hugeDimWebP := buildRIFFWebP(buildChunk("VP8X", buildVP8XPayload(10000, 10000, false, nil)))
	if _, err := DecodeWebP(bytes.NewReader(hugeDimWebP)); err == nil || !strings.Contains(err.Error(), "exceed safety limit") {
		t.Fatalf("expected pixel bomb safety error, got %v", err)
	}

	// 4. boundedStdoutBuffer exceeding maxBytes cancels context and returns error
	canceled := false
	bsb := &boundedStdoutBuffer{
		maxBytes: 6,
		cancel:   func() { canceled = true },
	}
	if _, err := bsb.Write([]byte("1234")); err != nil {
		t.Fatal(err)
	}
	if _, err := bsb.Write([]byte("56789")); err == nil || !bsb.exceeded || !canceled {
		t.Fatalf("expected boundedStdoutBuffer overflow & cancel, got err=%v exceeded=%v canceled=%v", err, bsb.exceeded, canceled)
	}
}

func TestImagingCovV0112_ServiceHelpersContentTypesAndResizeEdges(t *testing.T) {
	// 1. contentTypeForImage extensions & magic-byte fallback
	for ext, want := range map[string]string{
		".jpg":  "image/jpeg",
		".jpeg": "image/jpeg",
		".png":  "image/png",
		".webp": "image/webp",
		".gif":  "image/gif",
		".bmp":  "image/bmp",
		".avif": "image/avif",
	} {
		if got := contentTypeForImage("/media/poster"+ext, nil); got != want {
			t.Fatalf("ext %s: got %q, want %q", ext, got, want)
		}
	}
	if got := contentTypeForImage("/media/raw.bin", []byte("plain text not an image")); got != "application/octet-stream" {
		t.Fatalf("expected application/octet-stream for non-image unknown ext, got %q", got)
	}

	// 2. Service methods: Load, SetCacheDir(""), acquireDecodeSlot(nil / cancelled / nil sem), CSP header, HEAD, Tag=
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "cov.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	ctx := context.Background()
	itemID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:        itemID,
		LibraryID: "lib1",
		Type:      "Movie",
		Name:      "Cov Movie",
	})

	binPath := filepath.Join(tmp, "custom.bin")
	_ = os.WriteFile(binPath, []byte("custom-binary-art"), 0o644)
	_ = repo.ReplaceItemImages(ctx, itemID, []sqlite.ImageRecord{{
		ItemID:     itemID,
		ImageType:  "Primary",
		ImageIndex: 0,
		Path:       binPath,
		Tag:        "tag999",
	}})

	svc := New(repo).WithCacheDir(filepath.Join(tmp, "cache")).WithMaxConcurrency(1).WithMaxCacheBytes(1024)
	if payload, err := svc.Load(ctx, itemID, "Primary", 0); err != nil || payload.ContentType != "application/octet-stream" {
		t.Fatalf("Load failed: payload=%+v err=%v", payload, err)
	}

	// HEAD request with uppercase ?Tag=tag999 on application/octet-stream sets CSP + immutable Cache-Control + empty body
	reqHead := httptest.NewRequest(http.MethodHead, "/Items/"+itemID+"/Images/Primary?Tag=tag999", nil)
	recHead := httptest.NewRecorder()
	if err := svc.ServeHTTP(recHead, reqHead, itemID, "Primary", 0); err != nil {
		t.Fatal(err)
	}
	if recHead.Header().Get("Content-Security-Policy") == "" || !strings.Contains(recHead.Header().Get("Cache-Control"), "immutable") || recHead.Body.Len() != 0 {
		t.Fatalf("unexpected HEAD headers/body: hdr=%+v len=%d", recHead.Header(), recHead.Body.Len())
	}

	// acquireDecodeSlot branches: nil ctx, full sem + cancelled ctx, nil sem
	rel, err := svc.acquireDecodeSlot(nil) //nolint:staticcheck // testing nil context guard
	if err != nil {
		t.Fatal(err)
	}
	canceledCtx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := svc.acquireDecodeSlot(canceledCtx); err == nil {
		t.Fatal("expected error when semaphore is full and context cancelled")
	}
	rel()

	svcNoSem := &Service{repo: repo}
	relNoop, err := svcNoSem.acquireDecodeSlot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	relNoop()

	// cachePath / legacyCachePaths short key guards & SetCacheDir("") fallback
	_ = svc.cachePath("ab", "jpg")
	_ = svc.legacyCachePaths("a", "jpg")
	svc.SetCacheDir("")
	if !strings.Contains(svc.CacheDir(), "ambra-image-cache") {
		t.Fatalf("expected fallback temp cache dir, got %q", svc.CacheDir())
	}

	// 3. Resize / clamp / Options edge cases
	if clampDimension(-10) != 0 || clampDimension(maxImageDimension+500) != maxImageDimension {
		t.Fatal("clampDimension bounds failed")
	}
	normNeg := (Options{Quality: -5, Format: "JPG"}).Normalized()
	normHigh := (Options{Quality: 200, Format: "webp"}).Normalized()
	if normNeg.Quality != 0 || normNeg.Format != "jpeg" || normHigh.Quality != 100 || normHigh.Format != "" {
		t.Fatalf("unexpected Normalized options: neg=%+v high=%+v", normNeg, normHigh)
	}
	wClamp, hClamp := clampOutputPixels(6000, 6000)
	if int64(wClamp)*int64(hClamp) > maxOutputPixels {
		t.Fatalf("clampOutputPixels exceeded maxOutputPixels: %dx%d", wClamp, hClamp)
	}
	emptyImg := image.NewNRGBA(image.Rect(0, 0, 0, 0))
	if r := ResizeBilinear(emptyImg, 0, 0); r.Bounds().Dx() != 1 || r.Bounds().Dy() != 1 {
		t.Fatalf("unexpected empty ResizeBilinear bounds: %v", r.Bounds())
	}
	if r := CropAndResizeBilinear(emptyImg, -1, -1); r.Bounds().Dx() != 1 || r.Bounds().Dy() != 1 {
		t.Fatalf("unexpected empty CropAndResizeBilinear bounds: %v", r.Bounds())
	}
	tallImg := image.NewRGBA(image.Rect(2, 2, 12, 32)) // non-zero Min + taller than wide
	if r := CropAndResizeBilinear(tallImg, 8, 8); r.Bounds().Dx() != 8 || r.Bounds().Dy() != 8 {
		t.Fatalf("unexpected tall CropAndResizeBilinear bounds: %v", r.Bounds())
	}

	// 4. Real PNG resize -> PNG disk cache write + PruneCache + resized 304 Not Modified + legacy cache fallback
	realPNGPath := filepath.Join(tmp, "real.png")
	var realPNGBuf bytes.Buffer
	_ = png.Encode(&realPNGBuf, tallImg)
	_ = os.WriteFile(realPNGPath, realPNGBuf.Bytes(), 0o644)

	pngItemID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{ID: pngItemID, LibraryID: "lib1", Type: "Movie", Name: "PNG Movie"})
	_ = repo.ReplaceItemImages(ctx, pngItemID, []sqlite.ImageRecord{{
		ItemID: pngItemID, ImageType: "Primary", ImageIndex: 0, Path: realPNGPath, Tag: "ptag1",
	}})

	cacheDir2 := filepath.Join(tmp, "cache2")
	svc2 := New(repo).WithCacheDir(cacheDir2).WithMaxCacheBytes(32) // tiny max bytes triggers prune
	optsPNG := Options{Width: 6, Format: "png"}
	reqResize := httptest.NewRequest(http.MethodGet, "/Items/"+pngItemID+"/Images/Primary", nil)
	recResize := httptest.NewRecorder()
	if err := svc2.ServeHTTPWithOptions(recResize, reqResize, pngItemID, "Primary", 0, optsPNG); err != nil {
		t.Fatalf("ServeHTTPWithOptions PNG failed: %v", err)
	}
	resizedETag := recResize.Header().Get("ETag")
	if recResize.Code != http.StatusOK || recResize.Header().Get("Content-Type") != "image/png" || resizedETag == "" {
		t.Fatalf("unexpected PNG resize response: code=%d ct=%s etag=%s", recResize.Code, recResize.Header().Get("Content-Type"), resizedETag)
	}

	// Resized 304 Not Modified
	reqResize304 := httptest.NewRequest(http.MethodGet, "/Items/"+pngItemID+"/Images/Primary", nil)
	reqResize304.Header.Set("If-None-Match", strings.Trim(resizedETag, `"`))
	recResize304 := httptest.NewRecorder()
	if err := svc2.ServeHTTPWithOptions(recResize304, reqResize304, pngItemID, "Primary", 0, optsPNG); err != nil {
		t.Fatal(err)
	}
	if recResize304.Code != http.StatusNotModified {
		t.Fatalf("expected 304 on resized raw ETag match, got %d", recResize304.Code)
	}

	// Legacy 1-level cache path fallback
	legacyKey := "abcdef0123456789"
	legacyFile := filepath.Join(cacheDir2, legacyKey[:2], legacyKey+".jpg")
	_ = os.MkdirAll(filepath.Dir(legacyFile), 0o700)
	_ = os.WriteFile(legacyFile, []byte("legacy-jpg"), 0o600)
	if p, ok := svc2.readDiskCache(legacyKey, Options{Width: 5}, `"e1"`); !ok || string(p.Data) != "legacy-jpg" {
		t.Fatalf("expected legacy cache hit, got ok=%v p=%+v", ok, p)
	}
	svc2.PruneCache()
}
