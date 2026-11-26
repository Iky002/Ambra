package imaging

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/store/sqlite"
)

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
