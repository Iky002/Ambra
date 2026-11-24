package imaging

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func makeTestImage(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8((x * 255) / max(1, w-1)),
				G: uint8((y * 255) / max(1, h-1)),
				B: 128,
				A: 255,
			})
		}
	}
	return img
}

func encodeJPEGBytes(t *testing.T, img image.Image, q int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: q}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

func encodePNGBytes(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func encodeGIFBytes(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := gif.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode gif: %v", err)
	}
	return buf.Bytes()
}

func TestOptionsNormalizationAndSignature(t *testing.T) {
	o := Options{
		MaxWidth:   -10,
		MaxHeight:  -5,
		Width:      -1,
		Height:     -2,
		FillWidth:  -3,
		FillHeight: -4,
		Quality:    -20,
		Format:     "  WEBP ",
	}
	n := o.Normalized()
	if !n.IsZero() {
		t.Fatalf("expected negative/unsupported options to normalize to zero, got %+v", n)
	}

	o2 := Options{MaxWidth: 300, Quality: 150, Format: "JPG"}
	n2 := o2.Normalized()
	if n2.Quality != 100 || n2.Format != "jpeg" {
		t.Fatalf("unexpected normalized options: %+v", n2)
	}
	if n2.IsZero() {
		t.Fatalf("expected non-zero options")
	}

	o3 := Options{MaxWidth: 300, Quality: 100, Format: "jpeg"}
	if o2.CacheSignature() != o3.CacheSignature() {
		t.Fatalf("expected identical cache signature, got %q vs %q", o2.CacheSignature(), o3.CacheSignature())
	}
	if (Options{Format: "PNG"}).Normalized().Format != "png" {
		t.Fatalf("expected PNG format normalized to png")
	}
}

func TestResizeBilinearProportionalAndQuality(t *testing.T) {
	srcBytes := encodeJPEGBytes(t, makeTestImage(400, 200), 95)

	// MaxWidth=100 -> 100x50
	out, ct, transformed, err := ProcessImage(srcBytes, "image/jpeg", Options{MaxWidth: 100})
	if err != nil {
		t.Fatalf("ProcessImage MaxWidth failed: %v", err)
	}
	if !transformed || ct != "image/jpeg" {
		t.Fatalf("expected transformed jpeg, got transformed=%v ct=%s", transformed, ct)
	}
	decoded, _, err := image.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if decoded.Bounds().Dx() != 100 || decoded.Bounds().Dy() != 50 {
		t.Fatalf("expected 100x50, got %dx%d", decoded.Bounds().Dx(), decoded.Bounds().Dy())
	}

	// MaxHeight=50 -> 100x50
	outH, _, _, err := ProcessImage(srcBytes, "image/jpeg", Options{MaxHeight: 50})
	if err != nil {
		t.Fatalf("ProcessImage MaxHeight failed: %v", err)
	}
	decH, _, _ := image.Decode(bytes.NewReader(outH))
	if decH.Bounds().Dx() != 100 || decH.Bounds().Dy() != 50 {
		t.Fatalf("expected 100x50, got %dx%d", decH.Bounds().Dx(), decH.Bounds().Dy())
	}

	// Width=200 & MaxWidth=120 & Height=100 -> minPositive(200,120)=120 -> 120x60
	outMin, _, _, err := ProcessImage(srcBytes, "image/jpeg", Options{Width: 200, MaxWidth: 120, Height: 100})
	if err != nil {
		t.Fatalf("ProcessImage combined failed: %v", err)
	}
	decMin, _, _ := image.Decode(bytes.NewReader(outMin))
	if decMin.Bounds().Dx() != 120 || decMin.Bounds().Dy() != 60 {
		t.Fatalf("expected 120x60, got %dx%d", decMin.Bounds().Dx(), decMin.Bounds().Dy())
	}

	// Quality comparison: Q=20 should be significantly smaller than Q=95
	outLowQ, _, _, err := ProcessImage(srcBytes, "image/jpeg", Options{MaxWidth: 200, Quality: 20})
	if err != nil {
		t.Fatalf("low quality encode: %v", err)
	}
	outHighQ, _, _, err := ProcessImage(srcBytes, "image/jpeg", Options{MaxWidth: 200, Quality: 95})
	if err != nil {
		t.Fatalf("high quality encode: %v", err)
	}
	if len(outLowQ) >= len(outHighQ) {
		t.Fatalf("expected quality=20 (%d bytes) < quality=95 (%d bytes)", len(outLowQ), len(outHighQ))
	}
}

func TestFillCenterCrop(t *testing.T) {
	// 1. Wide image (200x100): left 50px red, center 100px green, right 50px blue.
	wide := image.NewNRGBA(image.Rect(0, 0, 200, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 200; x++ {
			switch {
			case x < 50:
				wide.SetNRGBA(x, y, color.NRGBA{255, 0, 0, 255})
			case x < 150:
				wide.SetNRGBA(x, y, color.NRGBA{0, 255, 0, 255})
			default:
				wide.SetNRGBA(x, y, color.NRGBA{0, 0, 255, 255})
			}
		}
	}
	wideBytes := encodePNGBytes(t, wide)

	// Fill 50x50 -> crops center 100x100 (all green) and scales to 50x50
	outWide, ct, transformed, err := ProcessImage(wideBytes, "image/png", Options{FillWidth: 50, FillHeight: 50, Format: "png"})
	if err != nil {
		t.Fatalf("fill wide failed: %v", err)
	}
	if !transformed || ct != "image/png" {
		t.Fatalf("expected transformed png, got transformed=%v ct=%s", transformed, ct)
	}
	decWide, _, err := image.Decode(bytes.NewReader(outWide))
	if err != nil {
		t.Fatalf("decode wide fill: %v", err)
	}
	if decWide.Bounds().Dx() != 50 || decWide.Bounds().Dy() != 50 {
		t.Fatalf("expected 50x50, got %dx%d", decWide.Bounds().Dx(), decWide.Bounds().Dy())
	}
	r, g, b, _ := decWide.At(25, 25).RGBA()
	if g < 0xf000 || r > 0x1000 || b > 0x1000 {
		t.Fatalf("expected green center after crop, got r=%x g=%x b=%x", r, g, b)
	}

	// 2. Tall image (100x200): top 50px red, center 100px blue, bottom 50px red.
	tall := image.NewNRGBA(image.Rect(0, 0, 100, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 100; x++ {
			if y >= 50 && y < 150 {
				tall.SetNRGBA(x, y, color.NRGBA{0, 0, 255, 255})
			} else {
				tall.SetNRGBA(x, y, color.NRGBA{255, 0, 0, 255})
			}
		}
	}
	tallBytes := encodePNGBytes(t, tall)
	outTall, _, _, err := ProcessImage(tallBytes, "image/png", Options{FillWidth: 50, FillHeight: 50})
	if err != nil {
		t.Fatalf("fill tall failed: %v", err)
	}
	decTall, _, _ := image.Decode(bytes.NewReader(outTall))
	if decTall.Bounds().Dx() != 50 || decTall.Bounds().Dy() != 50 {
		t.Fatalf("expected 50x50, got %dx%d", decTall.Bounds().Dx(), decTall.Bounds().Dy())
	}
	r, g, b, _ = decTall.At(25, 25).RGBA()
	if b < 0xf000 || r > 0x1000 || g > 0x1000 {
		t.Fatalf("expected blue center after vertical crop, got r=%x g=%x b=%x", r, g, b)
	}

	// 3. Exact same dimensions as Fill -> no-op passthrough
	sameOut, _, transformedSame, err := ProcessImage(wideBytes, "image/png", Options{FillWidth: 200, FillHeight: 100})
	if err != nil {
		t.Fatalf("same fill failed: %v", err)
	}
	if transformedSame || !bytes.Equal(sameOut, wideBytes) {
		t.Fatalf("expected passthrough when source already matches fill dimensions")
	}
}

func TestProcessImagePassthroughFormatsAndEdgeCases(t *testing.T) {
	smallPNG := encodePNGBytes(t, makeTestImage(80, 60))

	// Zero options -> passthrough without decoding
	out, ct, transformed, err := ProcessImage(smallPNG, "image/png", Options{})
	if err != nil || transformed || ct != "image/png" || !bytes.Equal(out, smallPNG) {
		t.Fatalf("expected zero-options passthrough")
	}

	// Larger target than source -> passthrough (avoid recompression)
	out, ct, transformed, err = ProcessImage(smallPNG, "image/png", Options{MaxWidth: 200, MaxHeight: 200})
	if err != nil || transformed || ct != "image/png" || !bytes.Equal(out, smallPNG) {
		t.Fatalf("expected passthrough when source smaller than max dimensions")
	}

	// Format conversion PNG -> JPEG without resize
	out, ct, transformed, err = ProcessImage(smallPNG, "image/png", Options{Format: "jpeg"})
	if err != nil || !transformed || ct != "image/jpeg" {
		t.Fatalf("expected png->jpeg transcode, got transformed=%v ct=%s err=%v", transformed, ct, err)
	}

	// GIF decoding + resize -> JPEG
	gifBytes := encodeGIFBytes(t, makeTestImage(120, 80))
	outGif, ctGif, transformedGif, err := ProcessImage(gifBytes, "image/gif", Options{MaxWidth: 60})
	if err != nil || !transformedGif || ctGif != "image/jpeg" {
		t.Fatalf("expected gif resize to jpeg, got transformed=%v ct=%s err=%v", transformedGif, ctGif, err)
	}
	decGif, _, _ := image.Decode(bytes.NewReader(outGif))
	if decGif.Bounds().Dx() != 60 || decGif.Bounds().Dy() != 40 {
		t.Fatalf("expected 60x40 from gif, got %dx%d", decGif.Bounds().Dx(), decGif.Bounds().Dy())
	}

	// Corrupted bytes -> error
	if _, _, _, err := ProcessImage([]byte("not-an-image"), "image/jpeg", Options{MaxWidth: 50}); err == nil {
		t.Fatalf("expected decode error for corrupted bytes")
	}

	// Edge cases: non-zero bounds / zero dimensions
	empty := image.NewNRGBA(image.Rect(0, 0, 0, 0))
	if r := ResizeBilinear(empty, 0, 0); r.Bounds().Dx() != 1 || r.Bounds().Dy() != 1 {
		t.Fatalf("unexpected empty ResizeBilinear bounds: %v", r.Bounds())
	}
	if r := CropAndResizeBilinear(empty, 0, 0); r.Bounds().Dx() != 1 || r.Bounds().Dy() != 1 {
		t.Fatalf("unexpected empty CropAndResizeBilinear bounds: %v", r.Bounds())
	}
	if w, h, crop := computeTargetDimensions(0, 0, Options{MaxWidth: 100}); w != 0 || h != 0 || crop {
		t.Fatalf("unexpected zero source dimensions: %d %d %v", w, h, crop)
	}
	if minPositive(5, 10) != 5 || minPositive(10, 5) != 5 || minPositive(0, 7) != 7 || minPositive(7, 0) != 7 {
		t.Fatalf("minPositive failed")
	}
}

func TestServiceDiskCacheSingleflightDegradationAndETag(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "svc_cache.db"))
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
		Name:      "Cache Movie",
	})

	imgPath := filepath.Join(tmp, "poster.jpg")
	origBytes := encodeJPEGBytes(t, makeTestImage(300, 150), 90)
	_ = os.WriteFile(imgPath, origBytes, 0o644)

	badItemID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:        badItemID,
		LibraryID: "lib1",
		Type:      "Movie",
		Name:      "Corrupt Movie",
	})
	badPath := filepath.Join(tmp, "corrupt.jpg")
	badBytes := []byte("corrupted-jpeg-payload")
	_ = os.WriteFile(badPath, badBytes, 0o644)

	_ = repo.ReplaceItemImages(ctx, itemID, []sqlite.ImageRecord{{
		ItemID:     itemID,
		ImageType:  "Primary",
		ImageIndex: 0,
		Path:       imgPath,
		Tag:        "tagv1",
	}})
	_ = repo.ReplaceItemImages(ctx, badItemID, []sqlite.ImageRecord{{
		ItemID:     badItemID,
		ImageType:  "Primary",
		ImageIndex: 0,
		Path:       badPath,
		Tag:        "badtag",
	}})

	cacheDir := filepath.Join(tmp, "imgcache")
	svc := New(repo).WithCacheDir(cacheDir)
	if svc.CacheDir() != cacheDir {
		t.Fatalf("expected cacheDir=%s, got %s", cacheDir, svc.CacheDir())
	}

	// 1. Original passthrough via GetImage
	origPayload, err := svc.GetImage(ctx, itemID, "Primary", 0)
	if err != nil {
		t.Fatalf("GetImage failed: %v", err)
	}
	if !bytes.Equal(origPayload.Data, origBytes) || origPayload.ETag != `"tagv1"` {
		t.Fatalf("unexpected original payload")
	}

	// 2. Concurrent singleflight requests for MaxWidth=100&Quality=75 -> processed once and cached on disk
	opts := Options{MaxWidth: 100, Quality: 75}
	const workers = 16
	var wg sync.WaitGroup
	results := make([]*ImagePayload, workers)
	errs := make([]error, workers)
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			results[idx], errs[idx] = svc.GetImageWithOptions(ctx, itemID, "Primary", 0, opts)
		}(i)
	}
	close(start)
	wg.Wait()

	for i := 0; i < workers; i++ {
		if errs[i] != nil {
			t.Fatalf("worker %d failed: %v", i, errs[i])
		}
		if !bytes.Equal(results[i].Data, results[0].Data) || results[i].ETag != results[0].ETag {
			t.Fatalf("worker %d got inconsistent payload", i)
		}
	}
	if svc.processCount.Load() != 1 {
		t.Fatalf("expected singleflight to process image exactly once, got %d", svc.processCount.Load())
	}
	if !strings.HasPrefix(results[0].ETag, `"tagv1-`) {
		t.Fatalf("expected ETag with tag and options hash, got %s", results[0].ETag)
	}

	// Verify disk cache file exists at <cacheDir>/<key[:2]>/<key>.jpg
	key := computeCacheKey(itemID, "Primary", 0, "tagv1", opts.Normalized())
	cachedFile := filepath.Join(cacheDir, key[:2], key+".jpg")
	if _, err := os.Stat(cachedFile); err != nil {
		t.Fatalf("expected disk cache file %s to exist: %v", cachedFile, err)
	}

	// 3. Second request hits disk cache directly without reprocessing (even if source is removed!)
	_ = os.Remove(imgPath)
	second, err := svc.GetImageWithOptions(ctx, itemID, "Primary", 0, opts)
	if err != nil {
		t.Fatalf("expected disk cache hit after source removal, got err: %v", err)
	}
	if !bytes.Equal(second.Data, results[0].Data) {
		t.Fatalf("disk cache hit data mismatch")
	}
	if svc.processCount.Load() != 1 {
		t.Fatalf("expected disk cache hit not to reprocess, processCount=%d", svc.processCount.Load())
	}

	// Restore source for PNG cache + HTTP 304 tests
	_ = os.WriteFile(imgPath, origBytes, 0o644)

	// 4. PNG output disk cache hit
	pngOpts := Options{FillWidth: 64, FillHeight: 64, Format: "png"}
	pngRes1, err := svc.GetImageWithOptions(ctx, itemID, "Primary", 0, pngOpts)
	if err != nil || pngRes1.ContentType != "image/png" {
		t.Fatalf("expected png result, got ct=%s err=%v", pngRes1.ContentType, err)
	}
	beforeCount := svc.processCount.Load()
	pngRes2, err := svc.GetImageWithOptions(ctx, itemID, "Primary", 0, pngOpts)
	if err != nil || !bytes.Equal(pngRes1.Data, pngRes2.Data) || svc.processCount.Load() != beforeCount {
		t.Fatalf("expected PNG disk cache hit without reprocessing")
	}

	// 5. Corrupted image graceful degradation (returns original bytes, no error)
	badRes, err := svc.GetImageWithOptions(ctx, badItemID, "Primary", 0, Options{MaxWidth: 50})
	if err != nil {
		t.Fatalf("expected graceful degradation for corrupted image, got err: %v", err)
	}
	if !bytes.Equal(badRes.Data, badBytes) {
		t.Fatalf("expected original corrupted bytes on fallback")
	}

	// 6. ServeHTTPWithOptions: 200 OK + ETag + 304 Not Modified
	req := httptest.NewRequest(http.MethodGet, "/Items/"+itemID+"/Images/Primary?maxWidth=100&quality=75", nil)
	rec := httptest.NewRecorder()
	if err := svc.ServeHTTPWithOptions(rec, req, itemID, "Primary", 0, opts); err != nil {
		t.Fatalf("ServeHTTPWithOptions failed: %v", err)
	}
	if rec.Code != http.StatusOK || rec.Header().Get("ETag") != results[0].ETag {
		t.Fatalf("unexpected 200 response: code=%d etag=%s", rec.Code, rec.Header().Get("ETag"))
	}

	req304 := httptest.NewRequest(http.MethodGet, "/Items/"+itemID+"/Images/Primary?maxWidth=100&quality=75", nil)
	req304.Header.Set("If-None-Match", results[0].ETag)
	rec304 := httptest.NewRecorder()
	if err := svc.ServeHTTPWithOptions(rec304, req304, itemID, "Primary", 0, opts); err != nil {
		t.Fatalf("ServeHTTPWithOptions 304 failed: %v", err)
	}
	if rec304.Code != http.StatusNotModified {
		t.Fatalf("expected 304, got %d", rec304.Code)
	}

	// 7. SetCacheDir("") fallback to os.TempDir()
	svc.SetCacheDir("")
	if !strings.HasPrefix(svc.CacheDir(), os.TempDir()) {
		t.Fatalf("expected fallback under os.TempDir(), got %s", svc.CacheDir())
	}

	// 8. Missing file on disk with resize options -> error
	_ = os.Remove(imgPath)
	if _, err := svc.GetImageWithOptions(ctx, itemID, "Primary", 0, Options{MaxWidth: 77}); err == nil {
		t.Fatalf("expected error when source missing on disk and cache miss")
	}
}
