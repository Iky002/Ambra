package hls

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestHLSSubtitleTracksAndPlaylistEndpoint(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "hls_subs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	cfg := config.Default(tmp)
	mgr := transcode.NewManager(cfg, transcode.WithCapabilities(&transcode.HWCapabilities{}))
	defer mgr.Close()

	itemID := guid.New()
	msID := guid.New()
	rt := int64(125_500_000_0) // 125.5s
	if err := repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:           itemID,
		LibraryID:    "lib1",
		Type:         "Movie",
		MediaType:    "Video",
		Name:         "Subtitled Movie",
		Path:         filepath.Join(tmp, "movie.mkv"),
		RuntimeTicks: &rt,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceMediaSourcesAndStreams(ctx, itemID, []sqlite.MediaSourceRecord{{
		ID:           msID,
		ItemID:       itemID,
		Name:         "movie.mkv",
		Path:         filepath.Join(tmp, "movie.mkv"),
		Container:    "mkv",
		RuntimeTicks: rt,
		Streams: []sqlite.MediaStreamRecord{
			{StreamIndex: 0, StreamType: "Video", Codec: "h264", Width: 1920, Height: 1080},
			{StreamIndex: 1, StreamType: "Audio", Codec: "aac", Channels: 2},
			{StreamIndex: 2, StreamType: "Subtitle", Codec: "subrip", Language: "chi", DisplayTitle: "Chinese (Simplified)", IsDefault: true},
			{StreamIndex: 3, StreamType: "Subtitle", Codec: "ass", Language: "eng", Title: "English ASS", IsForced: true},
			{StreamIndex: 4, StreamType: "Subtitle", Codec: "vtt", Language: "jpn"},
			{StreamIndex: 6, StreamType: "Subtitle", Codec: "mov_text", Language: "spa", Title: "Spanish MP4"},
			{StreamIndex: 7, StreamType: "Subtitle", Codec: "tx3g", Language: "kor", Title: "Korean TX3G"},
			// Bitmap subtitle should be skipped from WebVTT HLS tracks.
			{StreamIndex: 5, StreamType: "Subtitle", Codec: "hdmv_pgs_subtitle", Language: "fre", Title: "French PGS"},
		},
	}}); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	h := New(repo, mgr, cfg)
	h.RegisterRoutes(mux, AuthFuncs{})

	// 1. master.m3u8 includes #EXT-X-MEDIA:TYPE=SUBTITLES for text tracks only (relative URIs) and preserves api_key.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Videos/"+itemID+"/master.m3u8?MediaSourceId="+msID+"&api_key=secret123", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("master.m3u8 status=%d body=%s", rec.Code, rec.Body.String())
	}
	master := rec.Body.String()
	for _, want := range []string{
		`#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID="subs",NAME="Chinese (Simplified)",LANGUAGE="chi",DEFAULT=YES,AUTOSELECT=YES,FORCED=NO,URI="` + msID + `/Subtitles/2/subtitles.m3u8?api_key=secret123"`,
		`#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID="subs",NAME="English ASS",LANGUAGE="eng",DEFAULT=NO,AUTOSELECT=YES,FORCED=YES,URI="` + msID + `/Subtitles/3/subtitles.m3u8?api_key=secret123"`,
		`#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID="subs",NAME="jpn",LANGUAGE="jpn",DEFAULT=NO,AUTOSELECT=YES,FORCED=NO,URI="` + msID + `/Subtitles/4/subtitles.m3u8?api_key=secret123"`,
		`#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID="subs",NAME="Spanish MP4",LANGUAGE="spa",DEFAULT=NO,AUTOSELECT=YES,FORCED=NO,URI="` + msID + `/Subtitles/6/subtitles.m3u8?api_key=secret123"`,
		`#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID="subs",NAME="Korean TX3G",LANGUAGE="kor",DEFAULT=NO,AUTOSELECT=YES,FORCED=NO,URI="` + msID + `/Subtitles/7/subtitles.m3u8?api_key=secret123"`,
		`,SUBTITLES="subs"`,
	} {
		if !strings.Contains(master, want) {
			t.Errorf("master.m3u8 missing %q:\n%s", want, master)
		}
	}
	if strings.Contains(master, "French PGS") || strings.Contains(master, "/5/subtitles.m3u8") {
		t.Errorf("master.m3u8 should exclude bitmap subtitle track:\n%s", master)
	}

	// 1b. Token propagation from X-Emby-Token header and AuthContext when query api_key is empty.
	rec = httptest.NewRecorder()
	reqHdr := httptest.NewRequest(http.MethodGet, "/Videos/"+itemID+"/master.m3u8?MediaSourceId="+msID, nil)
	reqHdr.Header.Set("X-Emby-Token", "hdr-tok-42")
	mux.ServeHTTP(rec, reqHdr)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "api_key=hdr-tok-42") {
		t.Fatalf("master.m3u8 did not propagate X-Emby-Token: status=%d\n%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	reqCtx := httptest.NewRequest(http.MethodGet, "/Videos/"+itemID+"/main.m3u8?MediaSourceId="+msID, nil)
	reqCtx = reqCtx.WithContext(middleware.WithAuthContext(reqCtx.Context(), &middleware.AuthContext{Token: "ctx-tok-99"}))
	mux.ServeHTTP(rec, reqCtx)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "api_key=ctx-tok-99") {
		t.Fatalf("main.m3u8 did not propagate AuthContext token: status=%d\n%s", rec.Code, rec.Body.String())
	}

	// 2. subtitles.m3u8 returns VOD WebVTT playlist pointing to relative Stream.vtt with api_key preserved.
	rec = httptest.NewRecorder()
	reqSub := httptest.NewRequest(http.MethodGet, "/Videos/"+itemID+"/"+msID+"/Subtitles/2/subtitles.m3u8", nil)
	reqSub.Header.Set("Authorization", `MediaBrowser Client="Test", Token="auth-tok-77"`)
	mux.ServeHTTP(rec, reqSub)
	if rec.Code != http.StatusOK {
		t.Fatalf("subtitles.m3u8 status=%d body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/x-mpegURL") {
		t.Fatalf("expected application/x-mpegURL, got %q", ct)
	}
	subM3U8 := rec.Body.String()
	for _, want := range []string{
		"#EXTM3U",
		"#EXT-X-VERSION:3",
		"#EXT-X-TARGETDURATION:126",
		"#EXTINF:125.500000,",
		"Stream.vtt?api_key=auth-tok-77",
		"#EXT-X-ENDLIST",
	} {
		if !strings.Contains(subM3U8, want) {
			t.Errorf("subtitles.m3u8 missing %q:\n%s", want, subM3U8)
		}
	}
	if strings.Contains(subM3U8, "/Videos/") {
		t.Errorf("subtitles.m3u8 should use relative Stream.vtt URI, not root-absolute:\n%s", subM3U8)
	}

	// 3. Invalid index -> 400, missing item -> 404, non-existent or bitmap subtitle index -> 404.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Videos/"+itemID+"/"+msID+"/Subtitles/bad/subtitles.m3u8", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad subtitle index, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Videos/"+guid.New()+"/"+msID+"/Subtitles/2/subtitles.m3u8", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing item, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Videos/"+itemID+"/"+msID+"/Subtitles/99/subtitles.m3u8", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for non-existent subtitle index, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Videos/"+itemID+"/"+msID+"/Subtitles/5/subtitles.m3u8", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for bitmap PGS subtitle index, got %d", rec.Code)
	}

	// 4. Empty normalized MediaSourceId fallback ("---" normalizes to "").
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Videos/"+itemID+"/---/Subtitles/2/subtitles.m3u8", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Stream.vtt") {
		t.Fatalf("expected fallback to src.ID, got %d:\n%s", rec.Code, rec.Body.String())
	}

	// 5. Item with no MediaSources returns 404 for subtitle playlist.
	itemNoMS := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{ID: itemNoMS, LibraryID: "lib1", Type: "Movie", Name: "NoMS"})
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Videos/"+itemNoMS+"/---/Subtitles/0/subtitles.m3u8", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when item has no MediaSources, got %d:\n%s", rec.Code, rec.Body.String())
	}

	// 6. Helper edge case: empty src.ID falls back to item.ID with relative URI.
	subs := extractTextSubtitles(&sqlite.ItemRecord{ID: itemID}, &sqlite.MediaSourceRecord{
		Streams: []sqlite.MediaStreamRecord{{StreamIndex: 1, StreamType: "Subtitle", Codec: "srt", Language: "en"}},
	}, "")
	if len(subs) != 1 || subs[0].URI != itemID+"/Subtitles/1/subtitles.m3u8" {
		t.Fatalf("unexpected extractTextSubtitles fallback: %+v", subs)
	}

	// 7. Closed DB -> 500.
	_ = store.Close()
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Videos/"+itemID+"/"+msID+"/Subtitles/2/subtitles.m3u8", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on closed DB, got %d", rec.Code)
	}
}
