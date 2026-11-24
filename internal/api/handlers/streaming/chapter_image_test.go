package streaming

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/imaging"
	"github.com/ambra-media/ambra/internal/core/playback"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestChapterImageLazyExtraction(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "chapter_img.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Create a real tiny valid JPEG fixture so imaging service can serve/resize it cleanly.
	var jpegBuf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	img.Set(0, 0, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	if err := jpeg.Encode(&jpegBuf, img, nil); err != nil {
		t.Fatal(err)
	}
	sampleJPG := filepath.Join(tmp, "sample.jpg")
	if err := os.WriteFile(sampleJPG, jpegBuf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	callLog := filepath.Join(tmp, "ffmpeg_calls.log")
	fakeFFmpeg := filepath.Join(tmp, "fake-ffmpeg.sh")
	script := fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
for last; do true; done
cp %q "$last"
exit 0
`, callLog, sampleJPG)
	if err := os.WriteFile(fakeFFmpeg, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default(tmp)
	cfg.FFmpegPath = fakeFFmpeg

	repo := sqlite.NewLibraryRepo(store)
	engine := playback.New(repo, plugins.NewRegistry(), cfg)
	imgSvc := imaging.New(repo).WithCacheDir(cfg.ImageCacheDir())
	ext := transcode.NewExtractor(cfg)

	mux := http.NewServeMux()
	h := New(repo, engine, imgSvc)
	h.SetExtractor(ext)
	h.RegisterRoutes(mux, AuthFuncs{})

	ctx := context.Background()
	itemID := guid.New()
	msID := guid.New()
	mediaPath := filepath.Join(tmp, "movie.mkv")
	_ = os.WriteFile(mediaPath, []byte("dummy"), 0o644)

	if err := repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:        itemID,
		LibraryID: "lib1",
		Type:      "Movie",
		MediaType: "Video",
		Name:      "Chaptered Movie",
		Path:      mediaPath,
		Extra: sqlite.ItemExtra{
			Chapters: []sqlite.ChapterRecord{
				{Name: "Intro", StartPositionTicks: 0},
				{Name: "Act 1", StartPositionTicks: 600_000_000},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertMediaSourceAndStreams(ctx, itemID, sqlite.MediaSourceRecord{
		ID:        msID,
		ItemID:    itemID,
		Name:      "movie.mkv",
		Path:      mediaPath,
		Protocol:  "File",
		Container: "mkv",
	}); err != nil {
		t.Fatal(err)
	}

	// First request triggers lazy extraction and persists item_images record.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Items/"+itemID+"/Images/Chapter/1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for lazy chapter thumbnail, got %d body=%q", rec.Code, rec.Body.String())
	}
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "image/jpeg") {
		t.Fatalf("expected image/jpeg Content-Type, got %q", rec.Header().Get("Content-Type"))
	}

	// Verify item_images record was upserted.
	imgRec, err := repo.GetImageByItemAndType(ctx, itemID, "Chapter", 1)
	if err != nil || imgRec == nil || imgRec.Path == "" {
		t.Fatalf("expected upserted Chapter image record, got rec=%+v err=%v", imgRec, err)
	}

	// Second request hits cached DB+disk record without re-invoking ffmpeg.
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/Items/"+itemID+"/Images/Chapter/1", nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on cached chapter thumbnail, got %d", rec2.Code)
	}

	logBytes, _ := os.ReadFile(callLog)
	calls := strings.Split(strings.TrimSpace(string(logBytes)), "\n")
	if len(calls) != 1 {
		t.Fatalf("expected 1 ffmpeg invocation, got %d", len(calls))
	}

	// Out-of-bounds chapter index returns 404.
	recOOB := httptest.NewRecorder()
	mux.ServeHTTP(recOOB, httptest.NewRequest(http.MethodGet, "/Items/"+itemID+"/Images/Chapter/5", nil))
	if recOOB.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for out-of-bounds chapter index, got %d", recOOB.Code)
	}
}
