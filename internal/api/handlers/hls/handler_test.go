package hls_test

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

	"github.com/ambra-media/ambra/internal/api/handlers/hls"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func setupHLSTest(t *testing.T) (*http.ServeMux, *transcode.Manager, string, string) {
	t.Helper()
	tmpDir := t.TempDir()
	cfg := config.Default(tmpDir)
	st, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	repo := sqlite.NewLibraryRepo(st)
	ctx := context.Background()
	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{tmpDir})
	if err != nil {
		t.Fatal(err)
	}

	mediaPath := filepath.Join(tmpDir, "demo.mkv")
	if err := os.WriteFile(mediaPath, []byte("dummy"), 0o644); err != nil {
		t.Fatal(err)
	}

	runtimeTicks := int64(125_500_000_0) // 125.5s
	item := &sqlite.ItemRecord{
		LibraryID:    lib.ID,
		Type:         "Movie",
		MediaType:    "Video",
		Name:         "Demo Movie",
		Container:    "mkv",
		Path:         mediaPath,
		RuntimeTicks: &runtimeTicks,
	}
	if err := repo.UpsertItem(ctx, item); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceMediaSourcesAndStreams(ctx, item.ID, []sqlite.MediaSourceRecord{{
		ID:                   "ms001",
		ItemID:               item.ID,
		Name:                 "demo.mkv",
		Path:                 mediaPath,
		Protocol:             "File",
		Container:            "mkv",
		RuntimeTicks:         runtimeTicks,
		SupportsDirectPlay:   true,
		SupportsDirectStream: true,
		SupportsTranscoding:  true,
		Streams: []sqlite.MediaStreamRecord{
			{StreamIndex: 0, StreamType: "Video", Codec: "h264", Width: 1920, Height: 1080},
			{StreamIndex: 1, StreamType: "Audio", Codec: "aac", Channels: 2},
		},
	}}); err != nil {
		t.Fatal(err)
	}

	caps := transcode.BuildHWCapabilities([]string{"videotoolbox"}, map[string]bool{
		"h264_videotoolbox": true,
		"libx264":           true,
	}, "darwin")
	mgr := transcode.NewManager(cfg, transcode.WithCapabilities(caps))
	t.Cleanup(mgr.Close)

	mux := http.NewServeMux()
	// Coexist with wildcard GET /Videos/{Id}/{File} (which handles both GET and HEAD in Go 1.22+ ServeMux)
	mux.HandleFunc("GET /Videos/{Id}/{File}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := hls.New(repo, mgr, cfg)
	h.RegisterRoutes(mux, hls.AuthFuncs{})
	return mux, mgr, item.ID, "ms001"
}

func TestHLSPlaylistsSegmentAndActiveEncodings(t *testing.T) {
	mux, mgr, itemID, msID := setupHLSTest(t)

	// 1. master.m3u8
	req := httptest.NewRequest(http.MethodGet, "/Videos/"+itemID+"/master.m3u8?MediaSourceId="+msID+"&PlaySessionId=ps1&DeviceId=dev1&VideoCodec=copy&AudioCodec=aac", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("master.m3u8 status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "#EXTM3U") || !strings.Contains(rec.Body.String(), "main.m3u8?") {
		t.Fatalf("unexpected master.m3u8:\n%s", rec.Body.String())
	}

	// 2. main.m3u8
	req = httptest.NewRequest(http.MethodGet, "/Videos/"+itemID+"/main.m3u8?MediaSourceId="+msID+"&PlaySessionId=ps1&DeviceId=dev1&SegmentLength=6", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("main.m3u8 status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "#EXT-X-ENDLIST") || !strings.Contains(body, "hls1/main/20.ts?") {
		t.Fatalf("unexpected main.m3u8:\n%s", body)
	}

	// 3. Pre-populate cached segment 0 and fetch via /Videos/{Id}/hls1/main/0.ts
	sessions := mgr.ActiveSessions()
	if len(sessions) != 1 {
		t.Fatalf("expected 1 registered session, got %d", len(sessions))
	}
	if err := os.WriteFile(filepath.Join(sessions[0].WorkDir, "seg_0.ts"), []byte("TS_PACKET_DATA"), 0o644); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodGet, "/Videos/"+itemID+"/hls1/main/0.ts?MediaSourceId="+msID+"&PlaySessionId=ps1", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "TS_PACKET_DATA" {
		t.Fatalf("segment fetch failed: code=%d body=%q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "video/mp2t" {
		t.Fatalf("expected video/mp2t, got %q", ct)
	}

	// 4. DELETE /Videos/ActiveEncodings (authenticated admin/API key stops unowned session)
	req = httptest.NewRequest(http.MethodDelete, "/Videos/ActiveEncodings?DeviceId=dev1", nil)
	req = req.WithContext(middleware.WithAuthContext(req.Context(), &middleware.AuthContext{IsAPIKey: true}))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE ActiveEncodings status=%d", rec.Code)
	}
	if len(mgr.ActiveSessions()) != 0 {
		t.Fatalf("expected 0 active sessions after DELETE ActiveEncodings")
	}
}

func TestEncodingAndLiveStreamsEndpoints(t *testing.T) {
	mux, _, itemID, msID := setupHLSTest(t)

	// /Encoding/CodecInformation/Video
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Encoding/CodecInformation/Video", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("CodecInformation status=%d", rec.Code)
	}
	var codecs []transcode.CodecInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &codecs); err != nil || len(codecs) == 0 {
		t.Fatalf("unexpected codecs response: err=%v body=%s", err, rec.Body.String())
	}

	// /Encoding/CodecConfiguration/Defaults
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Encoding/CodecConfiguration/Defaults", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "h264_videotoolbox") {
		t.Fatalf("unexpected defaults response: code=%d body=%s", rec.Code, rec.Body.String())
	}

	// /Encoding/FullSubtitleDeliveryEncode
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Encoding/FullSubtitleDeliveryEncode", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("FullSubtitleDeliveryEncode status=%d", rec.Code)
	}

	// /LiveStreams/Open -> MediaInfo -> Close
	openPayload := `{"ItemId":"` + itemID + `","MediaSourceId":"` + msID + `","PlaySessionId":"live-ps"}`
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/LiveStreams/Open", bytes.NewBufferString(openPayload)))
	if rec.Code != http.StatusOK {
		t.Fatalf("LiveStreams/Open status=%d body=%s", rec.Code, rec.Body.String())
	}
	var openResp struct {
		LiveStreamId string `json:"LiveStreamId"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &openResp); err != nil || openResp.LiveStreamId == "" {
		t.Fatalf("invalid LiveStreams/Open response: %s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/LiveStreams/MediaInfo?LiveStreamId="+openResp.LiveStreamId, nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "demo.mkv") {
		t.Fatalf("LiveStreams/MediaInfo failed: code=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/LiveStreams/Close?LiveStreamId="+openResp.LiveStreamId, nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("LiveStreams/Close status=%d", rec.Code)
	}

	// MediaInfo after Close should return 404
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/LiveStreams/MediaInfo?LiveStreamId="+openResp.LiveStreamId, nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after LiveStreams/Close, got %d", rec.Code)
	}
}
