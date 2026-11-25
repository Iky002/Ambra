package streaming

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/imaging"
	"github.com/ambra-media/ambra/internal/core/playback"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/plugins/strm"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestStreamingHandlerRange302AndSubtitle(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "stream_api.db"))
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
	h.RegisterRoutes(mux, AuthFuncs{})

	ctx := context.Background()

	// 1. Local video file + SRT subtitle -> Range 206 & VTT conversion
	videoPath := filepath.Join(tmp, "demo.mp4")
	_ = os.WriteFile(videoPath, []byte("0123456789abcdefghij"), 0o644)
	subPath := filepath.Join(tmp, "demo.chs.srt")
	_ = os.WriteFile(subPath, []byte("1\n00:00:01,000 --> 00:00:03,000\n测试字幕\n"), 0o644)

	itemID := guid.New()
	msID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:        itemID,
		LibraryID: "lib1",
		Type:      "Movie",
		Name:      "Demo",
		Container: "mp4",
		Path:      videoPath,
	})
	_ = repo.ReplaceMediaSourcesAndStreams(ctx, itemID, []sqlite.MediaSourceRecord{{
		ID:        msID,
		ItemID:    itemID,
		Name:      "Demo",
		Path:      videoPath,
		Container: "mp4",
		Streams: []sqlite.MediaStreamRecord{
			{MediaSourceID: msID, ItemID: itemID, StreamIndex: 2, StreamType: "Subtitle", Codec: "subrip", IsExternal: true, Path: subPath},
		},
	}})

	// HTTP Range request on /Videos/{Id}/stream.mp4
	rangeReq := httptest.NewRequest(http.MethodGet, "/Videos/"+itemID+"/stream.mp4", nil)
	rangeReq.Header.Set("Range", "bytes=0-9")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, rangeReq)
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "0123456789" {
		t.Fatalf("expected 206 Partial Content '0123456789', got code=%d body=%q", rec.Code, rec.Body.String())
	}

	// Subtitle SRT -> VTT
	subReq := httptest.NewRequest(http.MethodGet, "/Videos/"+itemID+"/"+msID+"/Subtitles/2/Stream.vtt", nil)
	recSub := httptest.NewRecorder()
	mux.ServeHTTP(recSub, subReq)
	if recSub.Code != http.StatusOK || !strings.Contains(recSub.Body.String(), "WEBVTT") || !strings.Contains(recSub.Body.String(), "测试字幕") {
		t.Fatalf("expected VTT subtitle, got code=%d body=%q", recSub.Code, recSub.Body.String())
	}

	// 2. STRM file -> 302 Found
	strmPath := filepath.Join(tmp, "cloud.strm")
	_ = os.WriteFile(strmPath, []byte("https://93.184.216.34/direct.mkv\n"), 0o644)
	strmID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:        strmID,
		LibraryID: "lib1",
		Type:      "Movie",
		IsStrm:    true,
		Name:      "Cloud Movie",
		Container: "strm",
		Path:      strmPath,
	})
	_ = repo.ReplaceMediaSourcesAndStreams(ctx, strmID, []sqlite.MediaSourceRecord{{
		ID:        guid.New(),
		ItemID:    strmID,
		Name:      "Cloud Movie",
		Path:      strmPath,
		Protocol:  "Http",
		Container: "strm",
		IsRemote:  true,
	}})

	rec302 := httptest.NewRecorder()
	mux.ServeHTTP(rec302, httptest.NewRequest(http.MethodGet, "/Videos/"+strmID+"/stream", nil))
	if rec302.Code != http.StatusFound || rec302.Header().Get("Location") != "https://93.184.216.34/direct.mkv" {
		t.Fatalf("expected 302 redirect, got code=%d location=%s", rec302.Code, rec302.Header().Get("Location"))
	}
}

func TestStreamingSTRMReverseProxyRange(t *testing.T) {
	payload := []byte("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ")
	modTime := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)

	var lastRange, lastIfRange, lastUA string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastRange = r.Header.Get("Range")
		lastIfRange = r.Header.Get("If-Range")
		lastUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "video/mp4")
		http.ServeContent(w, r, "remote.mp4", modTime, bytes.NewReader(payload))
	}))
	defer upstream.Close()

	setup := func(enable302 bool) (*http.ServeMux, string, string) {
		tmp := t.TempDir()
		store, err := sqlite.Open(filepath.Join(tmp, "proxy.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })

		repo := sqlite.NewLibraryRepo(store)
		reg := plugins.NewRegistry()
		reg.Register(strm.New(enable302))
		cfg := config.Default(tmp)
		engine := playback.New(repo, reg, cfg)
		imgSvc := imaging.New(repo)

		mux := http.NewServeMux()
		h := New(repo, engine, imgSvc)
		h.RegisterRoutes(mux, AuthFuncs{})

		strmPath := filepath.Join(tmp, "remote.strm")
		if err := os.WriteFile(strmPath, []byte(upstream.URL+"/movie/remote.mp4\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		itemID := guid.New()
		ctx := context.Background()
		_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
			ID:        itemID,
			LibraryID: "lib1",
			Type:      "Movie",
			IsStrm:    true,
			Name:      "Remote Movie",
			Container: "strm",
			Path:      strmPath,
		})
		_ = repo.ReplaceMediaSourcesAndStreams(ctx, itemID, []sqlite.MediaSourceRecord{{
			ID:        guid.New(),
			ItemID:    itemID,
			Name:      "Remote Movie",
			Path:      strmPath,
			Protocol:  "Http",
			Container: "strm",
			IsRemote:  true,
		}})
		return mux, itemID, strmPath
	}

	// Case 1: Is302=true with explicit proxy=1 -> 206 Partial Content with Range/If-Range/UA forwarded
	mux302, itemID, _ := setup(true)
	req := httptest.NewRequest(http.MethodGet, "/Videos/"+itemID+"/stream.mp4?proxy=1", nil)
	req.Header.Set("Range", "bytes=10-19")
	req.Header.Set("If-Range", modTime.Format(http.TimeFormat))
	rec := httptest.NewRecorder()
	mux302.ServeHTTP(rec, req)

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("expected 206 Partial Content, got %d body=%q", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "ABCDEFGHIJ" {
		t.Fatalf("unexpected ranged body: %q", rec.Body.String())
	}
	if lastRange != "bytes=10-19" {
		t.Fatalf("expected Range header forwarded, got %q", lastRange)
	}
	if lastIfRange != modTime.Format(http.TimeFormat) {
		t.Fatalf("expected If-Range header forwarded, got %q", lastIfRange)
	}
	if !strings.Contains(lastUA, "Mozilla/5.0") {
		t.Fatalf("expected default browser User-Agent, got %q", lastUA)
	}
	if cr := rec.Header().Get("Content-Range"); cr != "bytes 10-19/36" {
		t.Fatalf("expected Content-Range 'bytes 10-19/36', got %q", cr)
	}
	if ar := rec.Header().Get("Accept-Ranges"); ar != "bytes" {
		t.Fatalf("expected Accept-Ranges 'bytes', got %q", ar)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "video/mp4" {
		t.Fatalf("expected Content-Type 'video/mp4', got %q", ct)
	}

	// Case 2: Download with proxy=true -> Content-Disposition attachment
	dlReq := httptest.NewRequest(http.MethodGet, "/Items/"+itemID+"/Download?proxy=true", nil)
	dlRec := httptest.NewRecorder()
	mux302.ServeHTTP(dlRec, dlReq)
	if dlRec.Code != http.StatusOK || dlRec.Body.String() != string(payload) {
		t.Fatalf("expected 200 full payload for download, got %d len=%d", dlRec.Code, dlRec.Body.Len())
	}
	if cd := dlRec.Header().Get("Content-Disposition"); !strings.Contains(cd, `filename="remote.mp4"`) {
		t.Fatalf("expected attachment Content-Disposition, got %q", cd)
	}

	// Case 3: Is302=false without proxy param -> automatically reverse-proxies
	muxNo302, itemID2, strmPath2 := setup(false)
	req2 := httptest.NewRequest(http.MethodGet, "/Videos/"+itemID2+"/stream", nil)
	req2.Header.Set("Range", "bytes=0-4")
	rec2 := httptest.NewRecorder()
	muxNo302.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusPartialContent || rec2.Body.String() != "01234" {
		t.Fatalf("expected 206 '01234' for Is302=false, got code=%d body=%q", rec2.Code, rec2.Body.String())
	}

	// Case 4: Unreachable upstream -> 502 Bad Gateway
	deadSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := deadSrv.URL + "/dead.mp4"
	deadSrv.Close()
	_ = os.WriteFile(strmPath2, []byte(deadURL+"\n"), 0o644)
	recDead := httptest.NewRecorder()
	muxNo302.ServeHTTP(recDead, httptest.NewRequest(http.MethodGet, "/Videos/"+itemID2+"/stream", nil))
	if recDead.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 Bad Gateway for dead upstream, got %d", recDead.Code)
	}
}

func TestStreamingLibraryCoverFallback(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "libcover.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	cfg := config.Default(tmp)
	engine := playback.New(repo, plugins.NewRegistry(), cfg)
	imgSvc := imaging.New(repo).WithCacheDir(filepath.Join(tmp, "imgcache"))

	mux := http.NewServeMux()
	h := New(repo, engine, imgSvc)
	h.RegisterRoutes(mux, AuthFuncs{})

	ctx := context.Background()
	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{tmp})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Empty library -> 404
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Items/"+lib.ID+"/Images/Primary", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for empty library cover, got %d", rec.Code)
	}

	// 2. Child item with Primary image -> serves Primary
	primaryPath := filepath.Join(tmp, "poster.jpg")
	primaryBytes := []byte("\xff\xd8\xff\xe0PRIMARY_IMG")
	_ = os.WriteFile(primaryPath, primaryBytes, 0o644)

	movieID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:        movieID,
		LibraryID: lib.ID,
		ParentID:  lib.ID,
		Type:      "Movie",
		Name:      "Cover Movie",
		Path:      filepath.Join(tmp, "movie.mkv"),
	})
	_ = repo.ReplaceItemImages(ctx, movieID, []sqlite.ImageRecord{{
		ItemID:     movieID,
		ImageType:  "Primary",
		ImageIndex: 0,
		Path:       primaryPath,
		Tag:        "primtag1",
	}})

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Items/"+lib.ID+"/Images/Primary", nil))
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), primaryBytes) {
		t.Fatalf("expected 200 Primary fallback, got code=%d body=%q", rec.Code, rec.Body.String())
	}

	// 3. Child item also gets a Backdrop image -> Backdrop is preferred for library card
	backdropPath := filepath.Join(tmp, "fanart.jpg")
	backdropBytes := []byte("\xff\xd8\xff\xe0BACKDROP_IMG")
	_ = os.WriteFile(backdropPath, backdropBytes, 0o644)
	_ = repo.ReplaceItemImages(ctx, movieID, []sqlite.ImageRecord{
		{ItemID: movieID, ImageType: "Primary", ImageIndex: 0, Path: primaryPath, Tag: "primtag1"},
		{ItemID: movieID, ImageType: "Backdrop", ImageIndex: 0, Path: backdropPath, Tag: "bdtag1"},
	})

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Items/"+lib.ID+"/Images/Primary", nil))
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), backdropBytes) {
		t.Fatalf("expected 200 Backdrop fallback, got code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestStreamingEmbeddedSubtitleExtractionAndVTT(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "embsub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Create a fake ffmpeg executable that writes extracted subtitle content to its last argument
	fakeFFmpeg := filepath.Join(tmp, "fake-ffmpeg.sh")
	script := `#!/bin/sh
out=""
for arg in "$@"; do out="$arg"; done
case "$*" in
  *fail_item*)
    echo "simulated ffmpeg error" >&2
    exit 1
    ;;
esac
case "$out" in
  *.vtt)
    printf 'WEBVTT\n\n00:00:01.000 --> 00:00:03.000\nEmbedded VTT Line\n' > "$out"
    ;;
  *.ass)
    printf '[Script Info]\nTitle: Test\n[Events]\nDialogue: 0,0:00:01.00,0:00:03.00,Default,,0,0,0,,Embedded ASS Line\n' > "$out"
    ;;
  *)
    printf '1\n00:00:01,000 --> 00:00:03,000\nEmbedded SRT Line\n' > "$out"
    ;;
esac
`
	if err := os.WriteFile(fakeFFmpeg, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	repo := sqlite.NewLibraryRepo(store)
	cfg := config.Default(tmp)
	cfg.FFmpegPath = fakeFFmpeg
	engine := playback.New(repo, plugins.NewRegistry(), cfg)
	imgSvc := imaging.New(repo)

	mux := http.NewServeMux()
	h := New(repo, engine, imgSvc)
	h.RegisterRoutes(mux, AuthFuncs{})

	ctx := context.Background()
	mkvPath := filepath.Join(tmp, "movie.mkv")
	_ = os.WriteFile(mkvPath, []byte("fake-mkv"), 0o644)

	itemID := guid.New()
	msID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:        itemID,
		LibraryID: "lib1",
		Type:      "Movie",
		Name:      "Embedded Sub Movie",
		Container: "mkv",
		Path:      mkvPath,
	})
	_ = repo.ReplaceMediaSourcesAndStreams(ctx, itemID, []sqlite.MediaSourceRecord{{
		ID:        msID,
		ItemID:    itemID,
		Name:      "Embedded Sub Movie",
		Path:      mkvPath,
		Container: "mkv",
		Streams: []sqlite.MediaStreamRecord{
			{MediaSourceID: msID, ItemID: itemID, StreamIndex: 2, StreamType: "Subtitle", Codec: "subrip", IsExternal: false, Path: ""},
		},
	}})

	// 1. Without extractor configured -> 404
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Videos/"+itemID+"/"+msID+"/Subtitles/2/Stream.vtt", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when extractor is nil, got %d", rec.Code)
	}

	// 2. Wire extractor -> VTT, ASS, and JS JSON conversion all succeed
	h.SetExtractor(transcode.NewExtractor(cfg))

	recVTT := httptest.NewRecorder()
	mux.ServeHTTP(recVTT, httptest.NewRequest(http.MethodGet, "/Videos/"+itemID+"/"+msID+"/Subtitles/2/Stream.vtt", nil))
	if recVTT.Code != http.StatusOK || !strings.Contains(recVTT.Body.String(), "Embedded VTT Line") || !strings.HasPrefix(recVTT.Header().Get("Content-Type"), "text/vtt") {
		t.Fatalf("expected 200 VTT, got code=%d ct=%q body=%q", recVTT.Code, recVTT.Header().Get("Content-Type"), recVTT.Body.String())
	}

	recASS := httptest.NewRecorder()
	mux.ServeHTTP(recASS, httptest.NewRequest(http.MethodGet, "/Videos/"+itemID+"/"+msID+"/Subtitles/2/Stream.ass", nil))
	if recASS.Code != http.StatusOK || !strings.Contains(recASS.Body.String(), "Embedded ASS Line") || !strings.HasPrefix(recASS.Header().Get("Content-Type"), "text/x-ssa") {
		t.Fatalf("expected 200 ASS, got code=%d ct=%q body=%q", recASS.Code, recASS.Header().Get("Content-Type"), recASS.Body.String())
	}

	recJS := httptest.NewRecorder()
	mux.ServeHTTP(recJS, httptest.NewRequest(http.MethodGet, "/Videos/"+itemID+"/"+msID+"/Subtitles/2/Stream.js", nil))
	if recJS.Code != http.StatusOK || !strings.Contains(recJS.Body.String(), "Embedded SRT Line") || !strings.HasPrefix(recJS.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("expected 200 JS JSON, got code=%d ct=%q body=%q", recJS.Code, recJS.Header().Get("Content-Type"), recJS.Body.String())
	}

	// 3. Extraction error -> 500
	failID := "fail_item_01"
	failMS := "ms_fail_01"
	failMKV := filepath.Join(tmp, "fail_item.mkv")
	_ = os.WriteFile(failMKV, []byte("fake-mkv"), 0o644)
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID: failID, LibraryID: "lib1", Type: "Movie", Name: "Fail", Container: "mkv", Path: failMKV,
	})
	_ = repo.ReplaceMediaSourcesAndStreams(ctx, failID, []sqlite.MediaSourceRecord{{
		ID: failMS, ItemID: failID, Name: "Fail", Path: failMKV, Container: "mkv",
		Streams: []sqlite.MediaStreamRecord{
			{MediaSourceID: failMS, ItemID: failID, StreamIndex: 2, StreamType: "Subtitle", Codec: "subrip", IsExternal: false, Path: ""},
		},
	}})
	recErr := httptest.NewRecorder()
	mux.ServeHTTP(recErr, httptest.NewRequest(http.MethodGet, "/Videos/"+failID+"/"+failMS+"/Subtitles/2/Stream.vtt", nil))
	if recErr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on ffmpeg failure, got %d", recErr.Code)
	}
}
