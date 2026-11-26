package imaging

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func buildSyntheticVP8Header(w, h int) []byte {
	var chunk [10]byte
	chunk[0] = 0x00 // keyframe (bit 0 == 0)
	chunk[3] = 0x9d
	chunk[4] = 0x01
	chunk[5] = 0x2a
	binary.LittleEndian.PutUint16(chunk[6:8], uint16(w&0x3fff))
	binary.LittleEndian.PutUint16(chunk[8:10], uint16(h&0x3fff))

	out := make([]byte, 20)
	copy(out[0:4], "RIFF")
	binary.LittleEndian.PutUint32(out[4:8], uint32(12+len(chunk)))
	copy(out[8:12], "WEBP")
	copy(out[12:16], "VP8 ")
	binary.LittleEndian.PutUint32(out[16:20], uint32(len(chunk)))
	out = append(out, chunk[:]...)
	return out
}

func buildSyntheticVP8LHeader(w, h int, alpha bool) []byte {
	var chunk [5]byte
	chunk[0] = 0x2f
	bits := uint32((w-1)&0x3fff) | (uint32((h-1)&0x3fff) << 14)
	if alpha {
		bits |= 1 << 28
	}
	binary.LittleEndian.PutUint32(chunk[1:5], bits)

	out := make([]byte, 20)
	copy(out[0:4], "RIFF")
	binary.LittleEndian.PutUint32(out[4:8], uint32(12+len(chunk)+1))
	copy(out[8:12], "WEBP")
	copy(out[12:16], "VP8L")
	binary.LittleEndian.PutUint32(out[16:20], uint32(len(chunk)))
	out = append(out, chunk[:]...)
	out = append(out, 0x00) // pad byte
	return out
}

func buildSyntheticVP8XHeader(w, h int, alpha bool) []byte {
	var chunk [10]byte
	if alpha {
		chunk[0] = 0x10
	}
	wm1 := uint32(w - 1)
	hm1 := uint32(h - 1)
	chunk[4] = byte(wm1)
	chunk[5] = byte(wm1 >> 8)
	chunk[6] = byte(wm1 >> 16)
	chunk[7] = byte(hm1)
	chunk[8] = byte(hm1 >> 8)
	chunk[9] = byte(hm1 >> 16)

	out := make([]byte, 20)
	copy(out[0:4], "RIFF")
	binary.LittleEndian.PutUint32(out[4:8], uint32(12+len(chunk)))
	copy(out[8:12], "WEBP")
	copy(out[12:16], "VP8X")
	binary.LittleEndian.PutUint32(out[16:20], uint32(len(chunk)))
	out = append(out, chunk[:]...)
	return out
}

func TestWebPRIFFHeaderExactDimensionsParsing(t *testing.T) {
	// 1. VP8 lossy
	cfgVP8, fmtName, err := image.DecodeConfig(bytes.NewReader(buildSyntheticVP8Header(1920, 1080)))
	if err != nil || fmtName != "webp" || cfgVP8.Width != 1920 || cfgVP8.Height != 1080 {
		t.Fatalf("VP8 DecodeConfig got (%dx%d, %q, %v), want 1920x1080 webp", cfgVP8.Width, cfgVP8.Height, fmtName, err)
	}

	// 2. VP8L lossless
	cfgVP8L, fmtName, err := image.DecodeConfig(bytes.NewReader(buildSyntheticVP8LHeader(1280, 720, true)))
	if err != nil || fmtName != "webp" || cfgVP8L.Width != 1280 || cfgVP8L.Height != 720 {
		t.Fatalf("VP8L DecodeConfig got (%dx%d, %q, %v), want 1280x720 webp", cfgVP8L.Width, cfgVP8L.Height, fmtName, err)
	}

	// 3. VP8X extended
	cfgVP8X, fmtName, err := image.DecodeConfig(bytes.NewReader(buildSyntheticVP8XHeader(3840, 2160, false)))
	if err != nil || fmtName != "webp" || cfgVP8X.Width != 3840 || cfgVP8X.Height != 2160 {
		t.Fatalf("VP8X DecodeConfig got (%dx%d, %q, %v), want 3840x2160 webp", cfgVP8X.Width, cfgVP8X.Height, fmtName, err)
	}
}

func TestWebPDecodeResizeAndDiskCacheHit(t *testing.T) {
	ffmpegBin, err := resolveFFmpegBinary()
	if err != nil {
		t.Skip("ffmpeg not available in environment:", err)
	}

	tmpDir := t.TempDir()
	// Generate a real 400x300 PNG and encode it to a real .webp file via ffmpeg
	srcImg := image.NewNRGBA(image.Rect(0, 0, 400, 300))
	for y := 0; y < 300; y++ {
		for x := 0; x < 400; x++ {
			srcImg.SetNRGBA(x, y, color.NRGBA{R: uint8(x % 256), G: uint8(y % 256), B: 140, A: 255})
		}
	}
	pngPath := filepath.Join(tmpDir, "source.png")
	f, err := os.Create(pngPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, srcImg); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	webpPath := filepath.Join(tmpDir, "poster.webp")
	cmd := exec.Command(ffmpegBin, "-y", "-hide_banner", "-loglevel", "error", "-i", pngPath, "-c:v", "libwebp", webpPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		// Fallback to default webp encoder if libwebp is named differently
		cmd2 := exec.Command(ffmpegBin, "-y", "-hide_banner", "-loglevel", "error", "-i", pngPath, webpPath)
		if out2, err2 := cmd2.CombinedOutput(); err2 != nil {
			t.Fatalf("failed to generate test webp (%v: %s / %v: %s)", err, string(out), err2, string(out2))
		}
	}

	webpBytes, err := os.ReadFile(webpPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, fmtName, err := image.DecodeConfig(bytes.NewReader(webpBytes))
	if err != nil || fmtName != "webp" || cfg.Width != 400 || cfg.Height != 300 {
		t.Fatalf("real webp DecodeConfig = (%dx%d, %q, %v), want 400x300 webp", cfg.Width, cfg.Height, fmtName, err)
	}

	store, err := sqlite.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	ctx := context.Background()
	lib, _ := repo.CreateLibrary(ctx, "Movies", "movies", []string{tmpDir})
	item := &sqlite.ItemRecord{ID: "item-webp-1", LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", Name: "WebP Movie", SortName: "webp movie"}
	_ = repo.UpsertItem(ctx, item)
	_ = repo.UpsertImage(ctx, &sqlite.ImageRecord{
		ItemID: item.ID, ImageType: "Primary", ImageIndex: 0, Path: webpPath, Tag: "webptag1", Width: 400, Height: 300,
	})

	cacheDir := filepath.Join(tmpDir, "img-cache")
	svc := New(repo).WithCacheDir(cacheDir).WithFFmpegPath(ffmpegBin)

	// 1st request: resize to MaxWidth=200 -> outputs JPEG, writes to disk cache
	p1, err := svc.GetImageWithOptions(ctx, item.ID, "Primary", 0, Options{MaxWidth: 200})
	if err != nil {
		t.Fatalf("GetImageWithOptions (1st) failed: %v", err)
	}
	if p1.ContentType != "image/jpeg" {
		t.Fatalf("expected resized WebP ContentType 'image/jpeg', got %q", p1.ContentType)
	}
	outCfg, outFmt, err := image.DecodeConfig(bytes.NewReader(p1.Data))
	if err != nil || outFmt != "jpeg" || outCfg.Width != 200 || outCfg.Height != 150 {
		t.Fatalf("expected resized 200x150 jpeg, got (%dx%d, %q, %v)", outCfg.Width, outCfg.Height, outFmt, err)
	}
	if svc.processCount.Load() != 1 {
		t.Fatalf("expected processCount=1, got %d", svc.processCount.Load())
	}

	// Remove the original .webp file to prove the 2nd request hits the disk cache directly without reading or re-decoding source!
	if err := os.Remove(webpPath); err != nil {
		t.Fatal(err)
	}

	p2, err := svc.GetImageWithOptions(ctx, item.ID, "Primary", 0, Options{MaxWidth: 200})
	if err != nil {
		t.Fatalf("GetImageWithOptions (2nd from disk cache) failed: %v", err)
	}
	if p2.ContentType != "image/jpeg" || !bytes.Equal(p1.Data, p2.Data) {
		t.Fatalf("expected identical cached JPEG payload on 2nd request")
	}
	if svc.processCount.Load() != 1 {
		t.Fatalf("expected disk cache hit without re-processing, processCount=%d", svc.processCount.Load())
	}
}
