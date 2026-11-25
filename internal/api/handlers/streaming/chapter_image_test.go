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

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/imaging"
	"github.com/ambra-media/ambra/internal/core/playback"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestChapterImageAndBIFRequireScheduledTask(t *testing.T) {
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
case "$last" in
  *frame_%%06d.jpg)
    dir=$(dirname "$last")
    cp %q "$dir/frame_000001.jpg"
    ;;
  *)
    cp %q "$last"
    ;;
esac
exit 0
`, callLog, sampleJPG, sampleJPG)
	if err := os.WriteFile(fakeFFmpeg, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default(tmp)
	cfg.FFmpegPath = fakeFFmpeg

	repo := sqlite.NewLibraryRepo(store)
	engine := playback.New(repo, plugins.NewRegistry(), cfg)
	imgSvc := imaging.New(repo).WithCacheDir(cfg.ImageCacheDir())
	ext := transcode.NewExtractor(cfg)

	userRepo := sqlite.NewUserRepo(store)
	adminUser := &sqlite.UserRecord{ID: guid.New(), Name: "admin", IsAdmin: true, Policy: sqlite.DefaultUserPolicy(true), Config: sqlite.DefaultUserConfig()}
	restrictedPolicy := sqlite.DefaultUserPolicy(false)
	restrictedPolicy.EnableAllFolders = false
	restrictedPolicy.EnabledFolders = []string{"other-lib"}
	restrictedUser := &sqlite.UserRecord{ID: guid.New(), Name: "restricted", Policy: restrictedPolicy, Config: sqlite.DefaultUserConfig()}
	_ = userRepo.CreateUser(context.Background(), adminUser)
	_ = userRepo.CreateUser(context.Background(), restrictedUser)

	mux := http.NewServeMux()
	h := New(repo, engine, imgSvc)
	h.SetExtractor(ext)
	h.SetUserRepo(userRepo)
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

	// 1. Before scheduled tasks run, even authenticated admin requests for Chapter images or BIF
	// MUST return 404 and MUST NOT invoke FFmpeg on demand.
	for _, ep := range []string{
		"/Items/" + itemID + "/Images/Chapter/1",
		"/Videos/" + itemID + "/index.bif?Width=320",
	} {
		req := httptest.NewRequest(http.MethodGet, ep, nil)
		req = req.WithContext(middleware.WithAuthContext(req.Context(), &middleware.AuthContext{User: adminUser}))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for %s before scheduled task, got %d", ep, rec.Code)
		}
	}
	if _, err := os.Stat(callLog); !os.IsNotExist(err) {
		t.Fatalf("expected ffmpeg NOT to be invoked during page/API visits")
	}

	// Restricted non-admin user gets 403 on index.bif.
	reqRestrictedBif := httptest.NewRequest(http.MethodGet, "/Videos/"+itemID+"/index.bif", nil)
	reqRestrictedBif = reqRestrictedBif.WithContext(middleware.WithAuthContext(reqRestrictedBif.Context(), &middleware.AuthContext{User: restrictedUser}))
	recRestrictedBif := httptest.NewRecorder()
	mux.ServeHTTP(recRestrictedBif, reqRestrictedBif)
	if recRestrictedBif.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for restricted user on index.bif, got %d", recRestrictedBif.Code)
	}

	// 2. Run scheduled chapter extraction task -> populates dedicated cache/chapters dir & DB.
	chSum, err := transcode.ExtractAllChapterImages(ctx, repo, ext, nil)
	if err != nil || chSum.Extracted != 2 {
		t.Fatalf("ExtractAllChapterImages failed: sum=%+v err=%v", chSum, err)
	}
	imgRec, err := repo.GetImageByItemAndType(ctx, itemID, "Chapter", 1)
	if err != nil || imgRec == nil || !strings.HasPrefix(imgRec.Path, cfg.ChapterCacheDir()) {
		t.Fatalf("expected Chapter image stored under %q, got rec=%+v err=%v", cfg.ChapterCacheDir(), imgRec, err)
	}

	// Now GET /Items/{Id}/Images/Chapter/1 serves 200 OK directly from cache.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Items/"+itemID+"/Images/Chapter/1", nil))
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "image/jpeg") {
		t.Fatalf("expected 200 image/jpeg for cached chapter thumbnail, got %d ct=%q", rec.Code, rec.Header().Get("Content-Type"))
	}

	// 3. Run scheduled BIF generation task -> populates cache/bif dir.
	bifSum, err := transcode.GenerateAllBIFThumbnails(ctx, repo, ext, nil)
	if err != nil || bifSum.Generated != 1 {
		t.Fatalf("GenerateAllBIFThumbnails failed: sum=%+v err=%v", bifSum, err)
	}

	// Now GET /Videos/{Id}/index.bif serves 200 OK with valid Roku/Emby BIF magic header.
	reqBif := httptest.NewRequest(http.MethodGet, "/Videos/"+itemID+"/index.bif?Width=320", nil)
	reqBif = reqBif.WithContext(middleware.WithAuthContext(reqBif.Context(), &middleware.AuthContext{User: adminUser}))
	recBif := httptest.NewRecorder()
	mux.ServeHTTP(recBif, reqBif)
	if recBif.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for cached index.bif, got %d body=%q", recBif.Code, recBif.Body.String())
	}
	if !bytes.HasPrefix(recBif.Body.Bytes(), []byte{0x89, 'B', 'I', 'F', '\r', '\n', 0x1a, '\n'}) {
		t.Fatalf("expected valid BIF magic prefix, got %x", recBif.Body.Bytes()[:min(8, recBif.Body.Len())])
	}

	// Verify FFmpeg was only invoked by the two scheduled tasks (2 chapters + 1 BIF = 3 calls), zero by HTTP requests.
	logBytes, _ := os.ReadFile(callLog)
	calls := strings.Split(strings.TrimSpace(string(logBytes)), "\n")
	if len(calls) != 3 {
		t.Fatalf("expected 3 ffmpeg invocations total from scheduled tasks, got %d:\n%s", len(calls), string(logBytes))
	}

	// Out-of-bounds chapter index still returns 404 without running ffmpeg.
	recOOB := httptest.NewRecorder()
	mux.ServeHTTP(recOOB, httptest.NewRequest(http.MethodGet, "/Items/"+itemID+"/Images/Chapter/5", nil))
	if recOOB.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for out-of-bounds chapter index, got %d", recOOB.Code)
	}
}
