package imaging

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func buildVP8XPixelBombWithVP8L(canvasW, canvasH, frameW, frameH int) []byte {
	var vp8x [10]byte
	cwm1 := uint32(canvasW - 1)
	chm1 := uint32(canvasH - 1)
	vp8x[4] = byte(cwm1)
	vp8x[5] = byte(cwm1 >> 8)
	vp8x[6] = byte(cwm1 >> 16)
	vp8x[7] = byte(chm1)
	vp8x[8] = byte(chm1 >> 8)
	vp8x[9] = byte(chm1 >> 16)

	var vp8l [5]byte
	vp8l[0] = 0x2f
	bits := uint32((frameW-1)&0x3fff) | (uint32((frameH-1)&0x3fff) << 14)
	binary.LittleEndian.PutUint32(vp8l[1:5], bits)

	var body bytes.Buffer
	body.WriteString("WEBP")
	// VP8X chunk (10 bytes)
	body.WriteString("VP8X")
	var sz [4]byte
	binary.LittleEndian.PutUint32(sz[:], 10)
	body.Write(sz[:])
	body.Write(vp8x[:])
	// VP8L sub-chunk (5 bytes + 1 pad byte)
	body.WriteString("VP8L")
	binary.LittleEndian.PutUint32(sz[:], 5)
	body.Write(sz[:])
	body.Write(vp8l[:])
	body.WriteByte(0x00)

	var out bytes.Buffer
	out.WriteString("RIFF")
	binary.LittleEndian.PutUint32(sz[:], uint32(body.Len()))
	out.Write(sz[:])
	out.Write(body.Bytes())
	return out.Bytes()
}

func buildVP8XPixelBombWithANMF(canvasW, canvasH, anmfW, anmfH int) []byte {
	var vp8x [10]byte
	vp8x[0] = 0x02 // animation flag
	cwm1 := uint32(canvasW - 1)
	chm1 := uint32(canvasH - 1)
	vp8x[4] = byte(cwm1)
	vp8x[5] = byte(cwm1 >> 8)
	vp8x[6] = byte(cwm1 >> 16)
	vp8x[7] = byte(chm1)
	vp8x[8] = byte(chm1 >> 8)
	vp8x[9] = byte(chm1 >> 16)

	var anmf [16]byte
	awm1 := uint32(anmfW - 1)
	ahm1 := uint32(anmfH - 1)
	anmf[6] = byte(awm1)
	anmf[7] = byte(awm1 >> 8)
	anmf[8] = byte(awm1 >> 16)
	anmf[9] = byte(ahm1)
	anmf[10] = byte(ahm1 >> 8)
	anmf[11] = byte(ahm1 >> 16)

	var body bytes.Buffer
	body.WriteString("WEBP")
	var sz [4]byte
	body.WriteString("VP8X")
	binary.LittleEndian.PutUint32(sz[:], 10)
	body.Write(sz[:])
	body.Write(vp8x[:])

	body.WriteString("ANMF")
	binary.LittleEndian.PutUint32(sz[:], 16)
	body.Write(sz[:])
	body.Write(anmf[:])

	var out bytes.Buffer
	out.WriteString("RIFF")
	binary.LittleEndian.PutUint32(sz[:], uint32(body.Len()))
	out.Write(sz[:])
	out.Write(body.Bytes())
	return out.Bytes()
}

func TestAuditV0112_WebPVP8XPixelBombAndNoRawWebPFallback(t *testing.T) {
	// 1. Fake 100x100 VP8X wrapping a 16383x16383 VP8L bitstream must report 16383x16383!
	bombVP8L := buildVP8XPixelBombWithVP8L(100, 100, 16383, 16383)
	w, h, _, err := ParseWebPDimensions(bytes.NewReader(bombVP8L))
	if err != nil {
		t.Fatalf("ParseWebPDimensions(bombVP8L) unexpected err: %v", err)
	}
	if w != 16383 || h != 16383 {
		t.Fatalf("expected max(canvas, VP8L) = 16383x16383, got %dx%d", w, h)
	}
	if _, err := DecodeWebP(bytes.NewReader(bombVP8L)); err == nil {
		t.Fatal("expected DecodeWebP to reject VP8X->VP8L pixel bomb exceeding maxSourcePixels")
	}

	// 2. Fake 100x100 VP8X wrapping a 16000x16000 ANMF frame must report 16000x16000!
	bombANMF := buildVP8XPixelBombWithANMF(100, 100, 16000, 16000)
	aw, ah, _, err := ParseWebPDimensions(bytes.NewReader(bombANMF))
	if err != nil || aw != 16000 || ah != 16000 {
		t.Fatalf("expected max(canvas, ANMF) = 16000x16000, got %dx%d err=%v", aw, ah, err)
	}

	// 3. boundedStdoutBuffer enforces hard byte cap.
	canceled := false
	bs := &boundedStdoutBuffer{
		maxBytes: 16,
		cancel:   func() { canceled = true },
	}
	if _, err := bs.Write(make([]byte, 32)); err == nil || !bs.exceeded || !canceled {
		t.Fatalf("expected boundedStdoutBuffer to fail and cancel when limit exceeded, exceeded=%v canceled=%v err=%v", bs.exceeded, canceled, err)
	}

	// 4. Service must return an error (NOT raw unvalidated WebP bytes) when WebP is a pixel bomb or corrupt!
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "img_v0112.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	ctx := context.Background()
	mediaDir := filepath.Join(tmp, "media")
	_ = os.MkdirAll(mediaDir, 0o755)
	lib, _ := repo.CreateLibrary(ctx, "Movies", "movies", []string{mediaDir})

	bombPath := filepath.Join(mediaDir, "bomb.webp")
	_ = os.WriteFile(bombPath, bombVP8L, 0o644)
	corruptPath := filepath.Join(mediaDir, "corrupt.webp")
	_ = os.WriteFile(corruptPath, []byte("RIFF\x10\x00\x00\x00WEBPBAD!"), 0o644)

	itBomb := &sqlite.ItemRecord{ID: "it-bomb", LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", Name: "Bomb", SortName: "bomb"}
	itCorrupt := &sqlite.ItemRecord{ID: "it-corrupt", LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", Name: "Corrupt", SortName: "corrupt"}
	_ = repo.UpsertItem(ctx, itBomb)
	_ = repo.UpsertItem(ctx, itCorrupt)
	_ = repo.UpsertImage(ctx, &sqlite.ImageRecord{ItemID: itBomb.ID, ImageType: "Primary", ImageIndex: 0, Path: bombPath, Tag: "t1"})
	_ = repo.UpsertImage(ctx, &sqlite.ImageRecord{ItemID: itCorrupt.ID, ImageType: "Primary", ImageIndex: 0, Path: corruptPath, Tag: "t2"})

	svc := New(repo).WithCacheDir(filepath.Join(tmp, "cache"))
	if _, err := svc.GetImage(ctx, itBomb.ID, "Primary", 0); err == nil {
		t.Fatal("expected GetImage (zero opts) to reject WebP pixel bomb")
	}
	if _, err := svc.GetImageWithOptions(ctx, itBomb.ID, "Primary", 0, Options{MaxWidth: 200}); err == nil {
		t.Fatal("expected GetImageWithOptions to reject WebP pixel bomb instead of serving raw WebP bytes")
	}
	if _, err := svc.GetImageWithOptions(ctx, itCorrupt.ID, "Primary", 0, Options{MaxWidth: 200}); err == nil {
		t.Fatal("expected GetImageWithOptions to reject corrupt WebP instead of falling back to raw bytes")
	}
}
