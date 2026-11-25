package streaming

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/imaging"
	"github.com/ambra-media/ambra/internal/core/playback"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/plugins/strm"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestPlaybackInfoAppendsAPIKeyAndDirectStreamWorksWithoutAuthHeaders(t *testing.T) {
	dir := t.TempDir()
	store, err := sqlite.Open(filepath.Join(dir, "playbackinfo_compat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	cfg := config.Default(dir)
	cfg.EnableSTRM302 = true
	userRepo := sqlite.NewUserRepo(store)
	libRepo := sqlite.NewLibraryRepo(store)
	authSvc := auth.NewService(userRepo, cfg)
	authMW := middleware.NewAuth(authSvc, nil)

	ctx := context.Background()
	viewer, err := authSvc.CreateUser(ctx, "viewer", "secret123", false)
	if err != nil {
		t.Fatal(err)
	}
	loginRes, err := authSvc.AuthenticateByName(ctx, "viewer", "secret123", "", auth.ClientInfo{DeviceID: "mpv-client"})
	if err != nil {
		t.Fatal(err)
	}
	token := loginRes.AccessToken

	lib, err := libRepo.CreateLibrary(ctx, "Movies", "movies", []string{dir})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Local video file + external subtitle
	videoPath := filepath.Join(dir, "sample.mkv")
	subPath := filepath.Join(dir, "sample.chs.srt")
	if err := os.WriteFile(videoPath, []byte("0123456789abcdefVIDEO_PAYLOAD"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(subPath, []byte("1\n00:00:01,000 --> 00:00:03,000\nHello\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	localItem := &sqlite.ItemRecord{
		ID:        "local-movie-1",
		LibraryID: lib.ID,
		Type:      "Movie",
		MediaType: "Video",
		Name:      "Local Movie",
		Container: "mkv",
		Path:      videoPath,
	}
	if err := libRepo.UpsertItem(ctx, localItem); err != nil {
		t.Fatal(err)
	}
	if err := libRepo.ReplaceMediaSourcesAndStreams(ctx, localItem.ID, []sqlite.MediaSourceRecord{{
		ID:                   "src-local-1",
		ItemID:               localItem.ID,
		Name:                 "sample.mkv",
		Path:                 videoPath,
		Protocol:             "File",
		Container:            "mkv",
		SupportsDirectPlay:   true,
		SupportsDirectStream: true,
		SupportsTranscoding:  true,
		Streams: []sqlite.MediaStreamRecord{
			{MediaSourceID: "src-local-1", ItemID: localItem.ID, StreamIndex: 0, StreamType: "Video", Codec: "hevc", Width: 1920, Height: 1080},
			{MediaSourceID: "src-local-1", ItemID: localItem.ID, StreamIndex: 1, StreamType: "Audio", Codec: "aac", Language: "eng", IsDefault: true},
			{MediaSourceID: "src-local-1", ItemID: localItem.ID, StreamIndex: 2, StreamType: "Subtitle", Codec: "srt", Language: "chi", Path: subPath, IsExternal: true},
		},
	}}); err != nil {
		t.Fatal(err)
	}

	// 2. STRM 302 movie
	strmPath := filepath.Join(dir, "cloud.strm")
	remoteTarget := "https://93.184.216.34/media/cloud-movie.mkv"
	if err := os.WriteFile(strmPath, []byte(remoteTarget+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	strmItem := &sqlite.ItemRecord{
		ID:        "strm-movie-1",
		LibraryID: lib.ID,
		Type:      "Movie",
		MediaType: "Video",
		Name:      "Cloud Movie",
		Container: "strm",
		IsStrm:    true,
		Path:      strmPath,
	}
	if err := libRepo.UpsertItem(ctx, strmItem); err != nil {
		t.Fatal(err)
	}
	if err := libRepo.ReplaceMediaSourcesAndStreams(ctx, strmItem.ID, []sqlite.MediaSourceRecord{{
		ID:                   "src-strm-1",
		ItemID:               strmItem.ID,
		Name:                 "cloud.strm",
		Path:                 strmPath,
		Protocol:             "File",
		Container:            "strm",
		IsRemote:             true,
		SupportsDirectPlay:   true,
		SupportsDirectStream: true,
		SupportsTranscoding:  true,
	}}); err != nil {
		t.Fatal(err)
	}

	reg := plugins.NewRegistry()
	strmP := strm.New(true)
	reg.Register(strmP)

	pb := playback.New(libRepo, reg, cfg)
	pb.SetUserRepo(userRepo)
	imgSvc := imaging.New(libRepo)
	h := New(libRepo, pb, imgSvc)
	h.SetUserRepo(userRepo)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
		UserIDFromCtx: func(c context.Context) string {
			if ac, ok := middleware.AuthFromContext(c); ok && ac != nil {
				return ac.UserID()
			}
			return ""
		},
	})
	handler := authMW.ParseAuth(mux)

	// A. DirectPlay request with X-Emby-Token: DirectStreamUrl has .mkv + api_key, DeliveryUrl has api_key, local Path hidden for viewer
	req := httptest.NewRequest(http.MethodPost, "/Items/"+localItem.ID+"/PlaybackInfo", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Emby-Token", token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PlaybackInfo status=%d body=%s", rec.Code, rec.Body.String())
	}

	var pi dto.PlaybackInfoResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &pi); err != nil {
		t.Fatal(err)
	}
	if len(pi.MediaSources) != 1 {
		t.Fatalf("expected 1 MediaSource, got %d", len(pi.MediaSources))
	}
	ms := pi.MediaSources[0]
	if ms.Path != "" {
		t.Fatalf("expected local video Path hidden for non-admin viewer, got %q", ms.Path)
	}
	if !strings.Contains(ms.DirectStreamUrl, "/Videos/"+localItem.ID+"/stream.mkv?") {
		t.Fatalf("expected container suffix .mkv in DirectStreamUrl, got %q", ms.DirectStreamUrl)
	}
	wantQuery := "api_key=" + url.QueryEscape(token)
	if !strings.Contains(ms.DirectStreamUrl, wantQuery) {
		t.Fatalf("expected api_key in DirectStreamUrl, got %q", ms.DirectStreamUrl)
	}
	var subDeliveryURL string
	for _, st := range ms.MediaStreams {
		if st.Type == "Subtitle" {
			subDeliveryURL = st.DeliveryUrl
			if st.Path != "" {
				t.Fatalf("expected local subtitle Path hidden for viewer, got %q", st.Path)
			}
		}
	}
	if !strings.Contains(subDeliveryURL, wantQuery) {
		t.Fatalf("expected api_key in subtitle DeliveryUrl, got %q", subDeliveryURL)
	}

	// Using DirectStreamUrl directly WITHOUT any Auth header must succeed (200/206, NOT 401!)
	streamReq := httptest.NewRequest(http.MethodGet, ms.DirectStreamUrl, nil)
	streamRec := httptest.NewRecorder()
	handler.ServeHTTP(streamRec, streamReq)
	if streamRec.Code != http.StatusOK && streamRec.Code != http.StatusPartialContent {
		t.Fatalf("direct stream with api_key URL failed: status=%d body=%q", streamRec.Code, streamRec.Body.String())
	}
	if streamRec.Body.String() != "0123456789abcdefVIDEO_PAYLOAD" {
		t.Fatalf("unexpected stream body %q", streamRec.Body.String())
	}

	// B. Transcode request: TranscodingUrl must ALSO contain api_key=<token>
	transcodeBody := `{
		"EnableDirectPlay": false,
		"EnableDirectStream": false,
		"EnableTranscoding": true,
		"DeviceProfile": {
			"Name": "ExoPlayer",
			"DirectPlayProfiles": [{"Type":"Video","Container":"mp4","VideoCodec":"h264","AudioCodec":"aac"}]
		}
	}`
	reqTC := httptest.NewRequest(http.MethodPost, "/Items/"+localItem.ID+"/PlaybackInfo", bytes.NewBufferString(transcodeBody))
	reqTC.Header.Set("Content-Type", "application/json")
	reqTC.Header.Set("X-Emby-Token", token)
	recTC := httptest.NewRecorder()
	handler.ServeHTTP(recTC, reqTC)
	if recTC.Code != http.StatusOK {
		t.Fatalf("PlaybackInfo transcode status=%d body=%s", recTC.Code, recTC.Body.String())
	}
	var piTC dto.PlaybackInfoResponse
	if err := json.Unmarshal(recTC.Body.Bytes(), &piTC); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(piTC.MediaSources[0].TranscodingUrl, wantQuery) {
		t.Fatalf("expected api_key in TranscodingUrl, got %q", piTC.MediaSources[0].TranscodingUrl)
	}

	// C. Non-admin STRM 302: Protocol=="Http", Path preserved as remote HTTP direct URL, DirectStreamUrl includes api_key & falls back to /stream (not .strm)
	reqStrm := httptest.NewRequest(http.MethodPost, "/Items/"+strmItem.ID+"/PlaybackInfo", bytes.NewBufferString(`{}`))
	reqStrm.Header.Set("Content-Type", "application/json")
	reqStrm.Header.Set("X-Emby-Token", token)
	recStrm := httptest.NewRecorder()
	handler.ServeHTTP(recStrm, reqStrm)
	if recStrm.Code != http.StatusOK {
		t.Fatalf("STRM PlaybackInfo status=%d body=%s", recStrm.Code, recStrm.Body.String())
	}
	var piStrm dto.PlaybackInfoResponse
	if err := json.Unmarshal(recStrm.Body.Bytes(), &piStrm); err != nil {
		t.Fatal(err)
	}
	msStrm := piStrm.MediaSources[0]
	if msStrm.Protocol != "Http" {
		t.Fatalf("expected STRM Protocol=Http, got %q", msStrm.Protocol)
	}
	if msStrm.Path != remoteTarget {
		t.Fatalf("expected non-admin STRM 302 Path=%q, got %q", remoteTarget, msStrm.Path)
	}
	if !strings.Contains(msStrm.DirectStreamUrl, "/Videos/"+strmItem.ID+"/stream?") || !strings.Contains(msStrm.DirectStreamUrl, wantQuery) {
		t.Fatalf("unexpected STRM DirectStreamUrl: %q", msStrm.DirectStreamUrl)
	}

	// Helper edge cases for 100% coverage: already has api_key, absolute URL, empty URL, nil resp
	appendAPIKeyToPlaybackInfo(nil, token)
	if got := appendAPIKeyQuery("/Videos/1/stream?api_key=existing", token); got != "/Videos/1/stream?api_key=existing" {
		t.Fatalf("must not duplicate api_key, got %q", got)
	}
	if got := appendAPIKeyQuery("https://example.com/a.mkv", token); got != "https://example.com/a.mkv" {
		t.Fatalf("must not attach api_key to external URL, got %q", got)
	}
	if got := appendAPIKeyQuery("/Videos/1/Subtitles/2/Stream.vtt", token); !strings.HasSuffix(got, "?api_key="+url.QueryEscape(token)) {
		t.Fatalf("expected ?api_key= on queryless URL, got %q", got)
	}
	_ = viewer
}
