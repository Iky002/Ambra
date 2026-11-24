package streaming

import (
	"context"
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
	_ = os.WriteFile(strmPath, []byte("https://cdn.example.com/direct.mkv\n"), 0o644)
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
	if rec302.Code != http.StatusFound || rec302.Header().Get("Location") != "https://cdn.example.com/direct.mkv" {
		t.Fatalf("expected 302 redirect, got code=%d location=%s", rec302.Code, rec302.Header().Get("Location"))
	}
}
