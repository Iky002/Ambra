package streaming

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
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/imaging"
	"github.com/ambra-media/ambra/internal/core/playback"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/plugins/strm"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestStreamingHandlerExtraBranches(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "stream_extra.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	reg := plugins.NewRegistry()
	reg.Register(strm.New(true))
	cfg := config.Default(tmp)
	engine := playback.New(repo, reg, cfg)
	imgSvc := imaging.New(repo)

	mux := http.NewServeMux()
	h := New(repo, engine, imgSvc)
	h.RegisterRoutes(mux, AuthFuncs{
		UserIDFromCtx: func(context.Context) string { return "ctx-user" },
	})

	ctx := context.Background()

	// Seed item with 2 media sources, 1 subtitle (pointing to missing file), and 1 image
	v1 := filepath.Join(tmp, "v1.mkv")
	v2 := filepath.Join(tmp, "v2.mp4")
	_ = os.WriteFile(v1, []byte("mkv-bytes"), 0o644)
	_ = os.WriteFile(v2, []byte("mp4-bytes"), 0o644)
	posterPath := filepath.Join(tmp, "poster.jpg")
	_ = os.WriteFile(posterPath, []byte{0xff, 0xd8, 0xff, 0xe0}, 0o644)

	itemID := guid.New()
	ms1 := guid.New()
	ms2 := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID: itemID, LibraryID: "lib1", Type: "Movie", Name: "Multi", Container: "mkv", Path: v1,
	})
	_ = repo.ReplaceMediaSourcesAndStreams(ctx, itemID, []sqlite.MediaSourceRecord{
		{
			ID: ms1, ItemID: itemID, Name: "V1", Path: v1, Container: "mkv",
			Streams: []sqlite.MediaStreamRecord{
				{MediaSourceID: ms1, ItemID: itemID, StreamIndex: 2, StreamType: "Subtitle", Codec: "subrip", IsExternal: true, Path: filepath.Join(tmp, "missing.srt")},
			},
		},
		{ID: ms2, ItemID: itemID, Name: "V2", Path: v2, Container: "mp4"},
	})
	_ = repo.ReplaceItemImages(ctx, itemID, []sqlite.ImageRecord{
		{ItemID: itemID, ImageType: "Primary", ImageIndex: 0, Path: posterPath, Tag: "ptag"},
	})

	// 1. PlaybackInfo GET with query params + POST with JSON body + 404
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Items/"+itemID+"/PlaybackInfo?MediaSourceId="+ms2+"&MaxStreamingBitrate=8000000&StartTimeTicks=1000&AudioStreamIndex=1&SubtitleStreamIndex=2", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET PlaybackInfo failed: %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Items/"+itemID+"/PlaybackInfo", bytes.NewReader([]byte(`{"MaxStreamingBitrate":4000000}`))))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST PlaybackInfo failed: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Items/"+guid.New()+"/PlaybackInfo", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 PlaybackInfo, got %d", rec.Code)
	}

	// 2. Unsupported stream endpoint -> 404; original.mkv with MediaSourceId=ms2 -> 200; Download -> 200 with Content-Disposition
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Videos/"+itemID+"/invalid.bin", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for invalid stream file, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Videos/"+itemID+"/original.mp4?MediaSourceId="+ms2, nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "mp4-bytes" {
		t.Fatalf("expected mp4-bytes for ms2, got code=%d body=%q", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Items/"+itemID+"/Download", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("Download failed: code=%d cd=%s", rec.Code, rec.Header().Get("Content-Disposition"))
	}

	// 3. Fallback to item.Path when no MediaSources; empty item.Path -> 404; missing file on disk -> 404; local .strm -> serves local target
	noMSID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{ID: noMSID, LibraryID: "lib1", Type: "Movie", Name: "Fallback", Path: v2})
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Videos/"+noMSID+"/stream", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("fallback item.Path failed: %d", rec.Code)
	}

	emptyPathID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{ID: emptyPathID, LibraryID: "lib1", Type: "Movie", Name: "NoPath"})
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Videos/"+emptyPathID+"/stream", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for empty item.Path, got %d", rec.Code)
	}

	missingFileID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{ID: missingFileID, LibraryID: "lib1", Type: "Movie", Name: "MissingDisk", Path: filepath.Join(tmp, "gone.mp4")})
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Videos/"+missingFileID+"/stream", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing file on disk, got %d", rec.Code)
	}

	localStrm := filepath.Join(tmp, "local.strm")
	_ = os.WriteFile(localStrm, []byte(v2+"\n"), 0o644)
	localStrmID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{ID: localStrmID, LibraryID: "lib1", Type: "Movie", IsStrm: true, Name: "LocalStrm", Path: localStrm})
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Videos/"+localStrmID+"/stream", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected non-http local strm target to be blocked with 502, got code=%d body=%q", rec.Code, rec.Body.String())
	}

	// 4. Subtitle error branches: invalid index (400), wrong msID (404), missing file on disk (404)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Videos/"+itemID+"/"+ms1+"/Subtitles/abc/Stream.vtt", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid subtitle index, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Videos/"+itemID+"/"+ms2+"/Subtitles/2/Stream.vtt", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for wrong msID subtitle, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Videos/"+itemID+"/"+ms1+"/Subtitles/2/Stream.vtt", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing subtitle file on disk, got %d", rec.Code)
	}

	// 5. Image handler: 200 with index, 404 missing
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Items/"+itemID+"/Images/Primary/0", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET image failed: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Items/"+itemID+"/Images/Backdrop", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing image, got %d", rec.Code)
	}

	// 6. videoContentType all branches
	for _, tc := range [][2]string{
		{"a.mp4", "video/mp4"},
		{"a.m4v", "video/mp4"},
		{"a.mkv", "video/x-matroska"},
		{"a.webm", "video/webm"},
		{"a.mov", "video/quicktime"},
		{"a.avi", "video/x-msvideo"},
		{"a.ts", "video/mp2t"},
		{"a.m2ts", "video/mp2t"},
		{"a.bin", "application/octet-stream"},
	} {
		if got := videoContentType(tc[0]); got != tc[1] {
			t.Errorf("videoContentType(%q) = %q, want %q", tc[0], got, tc[1])
		}
	}

	// 7. Closed DB -> 500 paths
	_ = store.Close()
	for _, path := range []string{
		"/Items/" + itemID + "/PlaybackInfo",
		"/Videos/" + itemID + "/stream",
		"/Videos/" + itemID + "/" + ms1 + "/Subtitles/2/Stream.vtt",
		"/Items/" + itemID + "/Images/Primary",
	} {
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("closed DB GET %s expected 500, got %d", path, rec.Code)
		}
	}
}

func TestStreamingHandlerImageResizeAnd304(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "stream_img.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	reg := plugins.NewRegistry()
	cfg := config.Default(tmp)
	engine := playback.New(repo, reg, cfg)
	imgSvc := imaging.New(repo).WithCacheDir(filepath.Join(tmp, "imgcache"))

	mux := http.NewServeMux()
	h := New(repo, engine, imgSvc)
	h.RegisterRoutes(mux, AuthFuncs{})

	ctx := context.Background()
	itemID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID: itemID, LibraryID: "lib1", Type: "Movie", Name: "Resize Movie",
	})

	// Generate a real 200x100 JPEG poster
	img := image.NewNRGBA(image.Rect(0, 0, 200, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 200; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y * 2), B: 100, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	posterPath := filepath.Join(tmp, "poster_real.jpg")
	_ = os.WriteFile(posterPath, buf.Bytes(), 0o644)

	_ = repo.ReplaceItemImages(ctx, itemID, []sqlite.ImageRecord{
		{ItemID: itemID, ImageType: "Primary", ImageIndex: 0, Path: posterPath, Tag: "realtag"},
	})

	// 1. Dynamic resize via ?tag=realtag&maxWidth=100&quality=75
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Items/"+itemID+"/Images/Primary?tag=realtag&maxWidth=100&quality=75", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for resized image, got %d", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("expected image/jpeg, got %s", rec.Header().Get("Content-Type"))
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "private, max-age=86400" {
		t.Fatalf("expected Cache-Control private, max-age=86400, got %q", cc)
	}
	etag := rec.Header().Get("ETag")
	if !strings.HasPrefix(etag, `"realtag-`) {
		t.Fatalf("expected strong ETag with resize hash, got %s", etag)
	}
	decoded, _, err := image.Decode(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("failed to decode resized response: %v", err)
	}
	if decoded.Bounds().Dx() != 100 || decoded.Bounds().Dy() != 50 {
		t.Fatalf("expected 100x50 resized image, got %dx%d", decoded.Bounds().Dx(), decoded.Bounds().Dy())
	}

	// 2. If-None-Match with returned ETag -> 304 Not Modified
	req304 := httptest.NewRequest(http.MethodGet, "/Items/"+itemID+"/Images/Primary?tag=realtag&maxWidth=100&quality=75", nil)
	req304.Header.Set("If-None-Match", etag)
	rec304 := httptest.NewRecorder()
	mux.ServeHTTP(rec304, req304)
	if rec304.Code != http.StatusNotModified || rec304.Body.Len() != 0 {
		t.Fatalf("expected 304 Not Modified, got code=%d len=%d", rec304.Code, rec304.Body.Len())
	}

	// 3. Fill center crop + PNG format -> 60x60 PNG with distinct ETag
	recFill := httptest.NewRecorder()
	mux.ServeHTTP(recFill, httptest.NewRequest(http.MethodGet, "/Items/"+itemID+"/Images/Primary?tag=realtag&fillWidth=60&fillHeight=60&format=png", nil))
	if recFill.Code != http.StatusOK || recFill.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("expected 200 image/png for fill crop, got code=%d ct=%s", recFill.Code, recFill.Header().Get("Content-Type"))
	}
	if recFill.Header().Get("ETag") == etag {
		t.Fatalf("expected distinct ETag for different resize options")
	}
	decFill, _, err := image.Decode(bytes.NewReader(recFill.Body.Bytes()))
	if err != nil {
		t.Fatalf("failed to decode fill png: %v", err)
	}
	if decFill.Bounds().Dx() != 60 || decFill.Bounds().Dy() != 60 {
		t.Fatalf("expected 60x60 fill image, got %dx%d", decFill.Bounds().Dx(), decFill.Bounds().Dy())
	}

	// 4. Original image (with valid tag, no resize params) -> passthrough bytes & original ETag; missing/wrong tag -> 404
	recNoTag := httptest.NewRecorder()
	mux.ServeHTTP(recNoTag, httptest.NewRequest(http.MethodGet, "/Items/"+itemID+"/Images/Primary", nil))
	if recNoTag.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for anonymous image without tag, got %d", recNoTag.Code)
	}
	recOrig := httptest.NewRecorder()
	mux.ServeHTTP(recOrig, httptest.NewRequest(http.MethodGet, "/Items/"+itemID+"/Images/Primary?tag=realtag", nil))
	if recOrig.Code != http.StatusOK || recOrig.Header().Get("ETag") != `"realtag"` || !bytes.Equal(recOrig.Body.Bytes(), buf.Bytes()) {
		t.Fatalf("expected original passthrough image")
	}
}
