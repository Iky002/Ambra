package compatmedia

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/imaging"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestHandleServeItemImageFullChapterLazyExtraction(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "chapter_img.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	cfg := config.Default(tmp)

	// Prepare a valid 16x16 JPEG fixture for the fake ffmpeg script to copy to its output arg.
	var jpgBuf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, color.RGBA{R: 120, G: 80, B: 200, A: 255})
		}
	}
	if err := jpeg.Encode(&jpgBuf, img, nil); err != nil {
		t.Fatal(err)
	}
	sampleJPG := filepath.Join(tmp, "sample.jpg")
	if err := os.WriteFile(sampleJPG, jpgBuf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	fakeFFmpeg := filepath.Join(tmp, "fake-ffmpeg.sh")
	script := "#!/bin/sh\n" +
		"last=\"\"\n" +
		"for arg in \"$@\"; do last=\"$arg\"; done\n" +
		"cp \"" + sampleJPG + "\" \"$last\"\n"
	if err := os.WriteFile(fakeFFmpeg, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg.FFmpegPath = fakeFFmpeg

	mediaPath := filepath.Join(tmp, "movie.mkv")
	if err := os.WriteFile(mediaPath, []byte("fake-mkv"), 0o644); err != nil {
		t.Fatal(err)
	}

	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{tmp})
	if err != nil {
		t.Fatal(err)
	}
	movie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "ChapterMovie", Path: mediaPath,
		Extra: sqlite.ItemExtra{
			Chapters: []sqlite.ChapterRecord{
				{StartPositionTicks: 60 * 10_000_000, Name: "Chapter 1"},
			},
		},
	}
	if err := repo.UpsertItem(ctx, movie); err != nil {
		t.Fatal(err)
	}

	h := New(repo, nil, imaging.New(repo), plugins.NewRegistry(), store, cfg)
	h.SetExtractor(transcode.NewExtractor(cfg))
	router := h.BuildRouter(AuthFuncs{})

	// Request full Emby image route for Chapter index 0 (not yet in DB).
	req := httptest.NewRequest(http.MethodGet, "/Items/"+movie.ID+"/Images/Chapter/0/tag123/jpg/320/180/0/0", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for lazy chapter thumbnail, got %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() == 0 {
		t.Fatal("expected non-empty JPEG response body")
	}

	// Verify ImageRecord was upserted into DB.
	imgRec, err := repo.GetImageByItemAndType(ctx, movie.ID, "Chapter", 0)
	if err != nil || imgRec == nil || imgRec.Path == "" {
		t.Fatalf("expected upserted Chapter ImageRecord in DB, got %+v err=%v", imgRec, err)
	}
	if _, err := os.Stat(imgRec.Path); err != nil {
		t.Fatalf("expected extracted chapter file on disk at %s: %v", imgRec.Path, err)
	}

	// Delete the extracted file on disk to verify missing-on-disk fallback re-extracts cleanly.
	_ = os.Remove(imgRec.Path)
	req2 := httptest.NewRequest(http.MethodGet, "/Items/"+movie.ID+"/Images/Chapter/0/tag123/jpg/320/180/0/0", nil)
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on re-extraction after missing file, got %d body=%s", rec2.Code, rec2.Body.String())
	}
}
