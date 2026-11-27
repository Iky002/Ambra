package imaging

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func makeValidPNG(t *testing.T, w, h int, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

func TestImageServiceSecurityHeadersAndNonRasterFallback(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "img-sec.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	ctx := context.Background()

	// 1. Raster PNG gets X-Content-Type-Options: nosniff on both 200 and 304.
	pngItemID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:        pngItemID,
		LibraryID: "lib1",
		Type:      "Movie",
		Name:      "Raster PNG",
	})
	pngPath := filepath.Join(tmp, "poster.png")
	_ = os.WriteFile(pngPath, makeValidPNG(t, 32, 48, color.RGBA{R: 10, G: 20, B: 30, A: 255}), 0o644)
	_ = repo.ReplaceItemImages(ctx, pngItemID, []sqlite.ImageRecord{{
		ItemID:     pngItemID,
		ImageType:  "Primary",
		ImageIndex: 0,
		Path:       pngPath,
		Tag:        "pngtag",
	}})

	svc := New(repo).WithCacheDir(filepath.Join(tmp, "cache"))

	req := httptest.NewRequest(http.MethodGet, "/Items/"+pngItemID+"/Images/Primary", nil)
	rec := httptest.NewRecorder()
	if err := svc.ServeHTTP(rec, req, pngItemID, "Primary", 0); err != nil {
		t.Fatalf("ServeHTTP png: %v", err)
	}
	if rec.Code != http.StatusOK || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("expected 200 with nosniff, got code=%d nosniff=%q", rec.Code, rec.Header().Get("X-Content-Type-Options"))
	}

	req304 := httptest.NewRequest(http.MethodGet, "/Items/"+pngItemID+"/Images/Primary", nil)
	req304.Header.Set("If-None-Match", `"pngtag"`)
	rec304 := httptest.NewRecorder()
	if err := svc.ServeHTTP(rec304, req304, pngItemID, "Primary", 0); err != nil {
		t.Fatalf("ServeHTTP 304: %v", err)
	}
	if rec304.Code != http.StatusNotModified || rec304.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("expected 304 with nosniff, got code=%d nosniff=%q", rec304.Code, rec304.Header().Get("X-Content-Type-Options"))
	}

	// 2. Non-raster SVG / HTML payload must NEVER be served as image/svg+xml or text/html.
	svgItemID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:        svgItemID,
		LibraryID: "lib1",
		Type:      "Movie",
		Name:      "Malicious SVG",
	})
	svgPath := filepath.Join(tmp, "evil.svg")
	_ = os.WriteFile(svgPath, []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`), 0o644)
	_ = repo.ReplaceItemImages(ctx, svgItemID, []sqlite.ImageRecord{{
		ItemID:     svgItemID,
		ImageType:  "Primary",
		ImageIndex: 0,
		Path:       svgPath,
		Tag:        "svgtag",
	}})

	reqSVG := httptest.NewRequest(http.MethodGet, "/Items/"+svgItemID+"/Images/Primary", nil)
	recSVG := httptest.NewRecorder()
	if err := svc.ServeHTTP(recSVG, reqSVG, svgItemID, "Primary", 0); err != nil {
		t.Fatalf("ServeHTTP svg: %v", err)
	}
	if ct := recSVG.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Fatalf("expected non-raster SVG to fall back to application/octet-stream, got %q", ct)
	}
	if recSVG.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("expected nosniff on non-raster fallback")
	}
	if csp := recSVG.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "sandbox") {
		t.Fatalf("expected sandbox CSP on non-raster fallback, got %q", csp)
	}
}

func TestImageServiceConcurrencySemaphoreAndCachePruning(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "img-cap.db"))
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
		Name:      "Big Poster",
	})
	pngPath := filepath.Join(tmp, "big.png")
	_ = os.WriteFile(pngPath, makeValidPNG(t, 120, 180, color.RGBA{R: 100, G: 150, B: 200, A: 255}), 0o644)
	_ = repo.ReplaceItemImages(ctx, itemID, []sqlite.ImageRecord{{
		ItemID:     itemID,
		ImageType:  "Primary",
		ImageIndex: 0,
		Path:       pngPath,
		Tag:        "bigtag",
	}})

	cacheDir := filepath.Join(tmp, "cache")
	svc := New(repo).WithCacheDir(cacheDir).WithMaxConcurrency(1).WithMaxCacheBytes(1200)

	// 1. Occupy the single semaphore slot and confirm a cancelled context aborts cleanly.
	svc.sem <- struct{}{}
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.GetImageWithOptions(canceledCtx, itemID, "Primary", 0, Options{MaxWidth: 64}); err == nil {
		<-svc.sem
		t.Fatal("expected context error while semaphore is occupied")
	}
	<-svc.sem

	// 2. Seed an older cached file and then write multiple resized variants to trigger PruneCache.
	oldFile := svc.cachePath("0000oldkey", "jpg")
	if err := os.MkdirAll(filepath.Dir(oldFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldFile, bytes.Repeat([]byte("A"), 900), 0o600); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-2 * time.Hour)
	_ = os.Chtimes(oldFile, oldTime, oldTime)

	// Trigger resized renders that exceed maxCacheBytes=1200.
	if _, err := svc.GetImageWithOptions(ctx, itemID, "Primary", 0, Options{MaxWidth: 60}); err != nil {
		t.Fatalf("GetImageWithOptions 60: %v", err)
	}
	if _, err := svc.GetImageWithOptions(ctx, itemID, "Primary", 0, Options{MaxWidth: 80}); err != nil {
		t.Fatalf("GetImageWithOptions 80: %v", err)
	}
	svc.PruneCache()

	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Fatalf("expected oldest cached file %q to be pruned when exceeding maxCacheBytes, stat err=%v", oldFile, err)
	}
}

func TestAuditV0107_SingleflightWithoutCancelAndClampOutputPixels(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "img.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	repo := sqlite.NewLibraryRepo(store)

	var buf bytes.Buffer
	src := image.NewRGBA(image.Rect(0, 0, 64, 64))
	src.Set(0, 0, color.RGBA{R: 200, G: 50, B: 50, A: 255})
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	imgPath := filepath.Join(tmp, "poster.png")
	if err := os.WriteFile(imgPath, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := repo.UpsertImage(ctx, &sqlite.ImageRecord{
		ItemID: "item-sf", ImageType: "Primary", ImageIndex: 0, Path: imgPath, Tag: "tag1",
	}); err != nil {
		t.Fatal(err)
	}

	svc := New(repo).WithCacheDir(filepath.Join(tmp, "cache")).WithMaxConcurrency(2)
	rec, err := repo.GetImageByItemAndType(ctx, "item-sf", "Primary", 0)
	if err != nil {
		t.Fatal(err)
	}

	// Even with a canceled caller ctx, singleflight internal decode slot uses context.WithoutCancel(ctx).
	canceledCtx, cancel := context.WithCancel(ctx)
	cancel()
	payload, err := svc.getImageFromRecord(canceledCtx, rec, Options{MaxWidth: 32, Format: "jpg"})
	if err != nil || len(payload.Data) == 0 {
		t.Fatalf("expected singleflight decode to ignore canceled caller ctx, got err=%v", err)
	}

	// Exercise acquireDecodeSlot edge paths directly
	rel, err := svc.acquireDecodeSlot(nil)
	if err != nil {
		t.Fatalf("acquireDecodeSlot(nil): %v", err)
	}
	rel()

	// Exercise clampOutputPixels when w*h exceeds maxOutputPixels
	w, h := clampOutputPixels(20000, 20000)
	if int64(w)*int64(h) > maxOutputPixels || w <= 0 || h <= 0 {
		t.Fatalf("clampOutputPixels failed: %dx%d", w, h)
	}
	w0, h0 := clampOutputPixels(0, 100)
	if w0 != 0 || h0 != 100 {
		t.Fatalf("unexpected non-positive clampOutputPixels: %dx%d", w0, h0)
	}
}

func TestAuditV0109_L25_StartupCacheSizeAsyncPruneAndPascalCaseTag(t *testing.T) {
	tmp := t.TempDir()
	cacheDir := filepath.Join(tmp, "cache")
	if err := os.MkdirAll(filepath.Join(cacheDir, "aa", "bb"), 0o700); err != nil {
		t.Fatal(err)
	}
	// Pre-populate disk cache before Service initialization.
	existing := make([]byte, 4096)
	if err := os.WriteFile(filepath.Join(cacheDir, "aa", "bb", "aabbccdd.jpg"), existing, 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := sqlite.Open(filepath.Join(tmp, "img.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	repo := sqlite.NewLibraryRepo(store)

	svc := New(repo).WithCacheDir(cacheDir).WithMaxCacheBytes(2048)

	// Wait briefly for async startup scan to count pre-existing cache bytes.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if svc.approxCacheBytes.Load() >= 4096 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := svc.approxCacheBytes.Load(); got < 4096 {
		t.Fatalf("expected startup async scan to observe >=4096 bytes, got %d", got)
	}

	// PascalCase Tag query param must produce immutable Cache-Control just like lowercase tag.
	imgPath := filepath.Join(tmp, "poster.png")
	if err := os.WriteFile(imgPath, []byte("\x89PNG\r\n\x1a\nfake-png-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertImage(context.Background(), &sqlite.ImageRecord{
		ItemID:     "item-tag",
		ImageType:  "Primary",
		ImageIndex: 0,
		Path:       imgPath,
		Tag:        "tag123",
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/Items/item-tag/Images/Primary?Tag=tag123", nil)
	rec := httptest.NewRecorder()
	if err := svc.ServeHTTP(rec, req, "item-tag", "Primary", 0); err != nil {
		t.Fatalf("ServeHTTP: %v", err)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Fatalf("expected immutable Cache-Control for PascalCase Tag, got %q", cc)
	}
}
