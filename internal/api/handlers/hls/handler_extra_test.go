package hls

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestHLSHandlerExtraBranches(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "hls_extra.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	cfg := config.Default(tmp)
	cfg.FFmpegPath = "/nonexistent/ffmpeg-hls-test"
	mgr := transcode.NewManager(cfg, transcode.WithCapabilities(&transcode.HWCapabilities{}))
	defer mgr.Close()

	mux := http.NewServeMux()
	h := New(repo, mgr, cfg)
	h.RegisterRoutes(mux, AuthFuncs{})

	// 1. Nil manager & nil repo edge cases
	hNil := New(nil, nil, cfg)
	muxNil := http.NewServeMux()
	hNil.RegisterRoutes(muxNil, AuthFuncs{})

	rec := httptest.NewRecorder()
	muxNil.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Videos/abc/hls1/main/0.ts", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when mgr is nil, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	muxNil.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Encoding/CodecInformation/Video", nil))
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("expected [] for nil mgr CodecInfo, got %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	muxNil.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Encoding/CodecConfiguration/Defaults", nil))
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("expected [] for nil mgr CodecDefaults, got %d %s", rec.Code, rec.Body.String())
	}
	if _, _, err := hNil.resolveItemAndSource(ctx, "x", ""); err != sqlite.ErrNotFound {
		t.Fatalf("expected ErrNotFound for nil repo")
	}

	// 2. Seed items:
	// - itemA with 2 media sources + STRM path
	// - itemNoMS with item.RuntimeTicks only (no MediaSources)
	strmFile := filepath.Join(tmp, "remote.strm")
	_ = os.WriteFile(strmFile, []byte("# comment\n\nhttps://cdn.example.com/live.mkv\n"), 0o644)

	itemA := guid.New()
	ms1 := guid.New()
	ms2 := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{ID: itemA, LibraryID: "lib1", Type: "Movie", Name: "A", Path: strmFile})
	_ = repo.ReplaceMediaSourcesAndStreams(ctx, itemA, []sqlite.MediaSourceRecord{
		{ID: ms1, ItemID: itemA, Name: "S1", Path: strmFile, Container: "strm"},
		{ID: ms2, ItemID: itemA, Name: "S2", Path: filepath.Join(tmp, "b.mkv"), Container: "mkv"},
	})

	rt := int64(60 * transcode.TicksPerSecond)
	itemNoMS := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{ID: itemNoMS, LibraryID: "lib1", Type: "Movie", Name: "NoMS", Path: filepath.Join(tmp, "c.mkv"), RuntimeTicks: &rt})

	// Master playlist with MaxWidth/MaxHeight, MaxStreamingBitrate, AudioStreamIndex, SegmentLength=0, MediaSourceId=ms2, STRM item
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Videos/"+itemA+"/master.m3u8?MediaSourceId="+ms1+"&MaxWidth=1280&MaxHeight=720&MaxStreamingBitrate=2000000&AudioStreamIndex=1&SegmentLength=0", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "RESOLUTION=1280x720") {
		t.Fatalf("master.m3u8 failed: %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Videos/"+itemA+"/master.m3u8?MediaSourceId="+ms2, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("master.m3u8 ms2 failed: %d", rec.Code)
	}

	// Media playlist on itemNoMS (exercises item.RuntimeTicks)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Videos/"+itemNoMS+"/main.m3u8", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("main.m3u8 itemNoMS failed: %d", rec.Code)
	}

	// 404s on master, main, segment
	missingID := guid.New()
	for _, path := range []string{
		"/Videos/" + missingID + "/master.m3u8",
		"/Videos/" + missingID + "/main.m3u8",
		"/Videos/" + missingID + "/hls1/main/0.ts",
	} {
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s expected 404, got %d", path, rec.Code)
		}
	}

	// Segment errors: invalid segment filename (400), unsupported container (400), ffmpeg missing (500), cached m4s (200 video/mp4)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Videos/"+itemA+"/hls1/main/badseg", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad segment, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Videos/"+itemA+"/hls1/main/0.mkv", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unsupported container, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Videos/"+itemA+"/hls1/main/0.ts?PlaySessionId=fail-ps", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when ffmpeg missing, got %d", rec.Code)
	}

	snap, err := mgr.RegisterSession(transcode.SessionParams{
		PlaySessionID:    "m4s-ps",
		ItemID:           itemA,
		MediaSourceID:    ms1,
		InputPath:        "https://cdn.example.com/live.mkv",
		SegmentContainer: "m4s",
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(snap.WorkDir, "seg_0.m4s"), []byte("fmp4-bytes"), 0o644)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Videos/"+itemA+"/hls1/main/0.m4s?PlaySessionId=m4s-ps", nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "video/mp4" {
		t.Fatalf("expected 200 video/mp4 for m4s segment, got %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}

	// LiveStreams: open via query for itemNoMS, open fallback for unknown item, MediaInfo via JSON body & 404, Close via JSON body
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/LiveStreams/Open?ItemId="+itemNoMS+"&MediaSourceId=&PlaySessionId=ls-ps", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("LiveStreams/Open itemNoMS failed: %d", rec.Code)
	}
	var openRes1 map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &openRes1)
	liveID1, _ := openRes1["LiveStreamId"].(string)

	// Open with unknown item -> synthetic IsInfiniteStream
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/LiveStreams/Open", bytes.NewReader([]byte(`{}`))))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"IsInfiniteStream":true`) {
		t.Fatalf("LiveStreams/Open synthetic failed: %d %s", rec.Code, rec.Body.String())
	}

	// MediaInfo via JSON body & 404
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/LiveStreams/MediaInfo", bytes.NewReader([]byte(`{"LiveStreamId":"`+liveID1+`"}`))))
	if rec.Code != http.StatusOK {
		t.Fatalf("LiveStreams/MediaInfo body failed: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/LiveStreams/MediaInfo", bytes.NewReader([]byte(`{"LiveStreamId":"missing"}`))))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing LiveStreamId, got %d", rec.Code)
	}

	// Close via JSON body
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/LiveStreams/Close", bytes.NewReader([]byte(`{"LiveStreamId":"`+liveID1+`"}`))))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("LiveStreams/Close body failed: %d", rec.Code)
	}

	// Direct helper edge cases
	if readFirstLineURL(filepath.Join(tmp, "nope.strm")) != "" {
		t.Fatalf("expected empty URL for missing strm")
	}
	emptyStrm := filepath.Join(tmp, "empty.strm")
	_ = os.WriteFile(emptyStrm, []byte("# only comment\n"), 0o644)
	if readFirstLineURL(emptyStrm) != "" {
		t.Fatalf("expected empty URL for comment-only strm")
	}
	if w, ht := extractVideoDimensions(nil); w != 0 || ht != 0 {
		t.Fatalf("expected 0,0 for nil src")
	}
	if resolveRuntimeTicks(nil, nil) != 0 {
		t.Fatalf("expected 0 runtime ticks for nil inputs")
	}

	// Closed DB -> 500 paths
	_ = store.Close()
	for _, path := range []string{
		"/Videos/" + itemA + "/master.m3u8",
		"/Videos/" + itemA + "/main.m3u8",
		"/Videos/" + itemA + "/hls1/main/0.ts",
	} {
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("closed DB GET %s expected 500, got %d", path, rec.Code)
		}
	}
}
