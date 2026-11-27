package compatmedia

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/imaging"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestHandleServeItemImageFullChapterCacheOnlyAndThumbnailSet(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "chapter_img.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	cfg := config.Default(tmp)

	// Prepare a valid 16x16 JPEG fixture.
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

	callLog := filepath.Join(tmp, "calls.log")
	fakeFFmpeg := filepath.Join(tmp, "fake-ffmpeg.sh")
	script := "#!/bin/sh\n" +
		"echo \"$@\" >> \"" + callLog + "\"\n" +
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

	ext := transcode.NewExtractor(cfg)
	h := New(repo, nil, imaging.New(repo), plugins.NewRegistry(), store, cfg)
	h.SetExtractor(ext)
	router := h.BuildRouter(AuthFuncs{})

	adminCtx := &middleware.AuthContext{User: &sqlite.UserRecord{ID: guid.New(), Name: "admin", IsAdmin: true}}

	// 1. Before scheduled task runs, even authenticated request MUST return 404 without invoking FFmpeg.
	reqBefore := httptest.NewRequest(http.MethodGet, "/Items/"+movie.ID+"/Images/Chapter/0/tag123/jpg/320/180/0/0", nil)
	reqBefore = reqBefore.WithContext(middleware.WithAuthContext(reqBefore.Context(), adminCtx))
	recBefore := httptest.NewRecorder()
	router.ServeHTTP(recBefore, reqBefore)
	if recBefore.Code != http.StatusNotFound {
		t.Fatalf("expected 404 before scheduled chapter extraction, got %d", recBefore.Code)
	}
	if _, err := os.Stat(callLog); !os.IsNotExist(err) {
		t.Fatal("expected HTTP chapter request NOT to invoke ffmpeg")
	}

	// 2. Run scheduled chapter extraction task -> persists to cache/chapters and DB.
	sum, err := transcode.ExtractAllChapterImages(ctx, repo, ext, nil)
	if err != nil || sum.Extracted != 1 {
		t.Fatalf("ExtractAllChapterImages: sum=%+v err=%v", sum, err)
	}

	// 3. After scheduled task runs, full Emby image route serves 200 OK from cache.
	reqAfter := httptest.NewRequest(http.MethodGet, "/Items/"+movie.ID+"/Images/Chapter/0/tag123/jpg/320/180/0/0", nil)
	reqAfter = reqAfter.WithContext(middleware.WithAuthContext(reqAfter.Context(), adminCtx))
	recAfter := httptest.NewRecorder()
	router.ServeHTTP(recAfter, reqAfter)
	if recAfter.Code != http.StatusOK || recAfter.Body.Len() == 0 {
		t.Fatalf("expected 200 OK with JPEG body after scheduled extraction, got %d len=%d", recAfter.Code, recAfter.Body.Len())
	}

	// 4. ThumbnailSet returns empty before BIF exists, and returns BIF frames once cached BIF exists.
	reqTS1 := httptest.NewRequest(http.MethodGet, "/Items/"+movie.ID+"/ThumbnailSet?Width=320", nil)
	reqTS1 = reqTS1.WithContext(middleware.WithAuthContext(reqTS1.Context(), adminCtx))
	recTS1 := httptest.NewRecorder()
	router.ServeHTTP(recTS1, reqTS1)
	if recTS1.Code != http.StatusOK {
		t.Fatalf("expected 200 on ThumbnailSet, got %d", recTS1.Code)
	}
	var tsResp1 struct {
		Thumbnails []map[string]any `json:"Thumbnails"`
	}
	_ = json.Unmarshal(recTS1.Body.Bytes(), &tsResp1)
	if len(tsResp1.Thumbnails) != 0 {
		t.Fatalf("expected 0 thumbnails before BIF exists, got %d", len(tsResp1.Thumbnails))
	}

	// Write a 2-frame cached BIF directly into cache/bif
	bifPath := ext.BifCachePath(movie.ID, 320)
	if err := transcode.WriteBIF(bifPath, 10000, [][]byte{jpgBuf.Bytes(), jpgBuf.Bytes()}); err != nil {
		t.Fatalf("WriteBIF: %v", err)
	}
	reqTS2 := httptest.NewRequest(http.MethodGet, "/Items/"+movie.ID+"/ThumbnailSet?Width=320", nil)
	reqTS2 = reqTS2.WithContext(middleware.WithAuthContext(reqTS2.Context(), adminCtx))
	recTS2 := httptest.NewRecorder()
	router.ServeHTTP(recTS2, reqTS2)
	var tsResp2 struct {
		Thumbnails []map[string]any `json:"Thumbnails"`
	}
	_ = json.Unmarshal(recTS2.Body.Bytes(), &tsResp2)
	if len(tsResp2.Thumbnails) != 2 {
		t.Fatalf("expected 2 thumbnails from cached BIF, got %+v", tsResp2.Thumbnails)
	}
}
