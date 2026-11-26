package hls

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestV0110_HLSPolicyInitSegmentRemoteBitrateAndLiveStreamLimits(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "hls_v0110.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	libRepo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)
	cfg.RemoteClientBitrateLimit = 1_500_000
	cfg.LocalNetworkSubnets = []string{"203.0.113.0/24", "198.51.100.8"}
	cfg.FFmpegPath = "/nonexistent/ffmpeg-v0110"

	authSvc := auth.NewService(userRepo, cfg)
	mgr := transcode.NewManager(cfg, transcode.WithCapabilities(transcode.SoftwareOnlyCapabilities()))
	defer mgr.Close()

	libA, _ := libRepo.CreateLibrary(ctx, "Movies", "movies", []string{tmp})
	libB, _ := libRepo.CreateLibrary(ctx, "Secret", "movies", []string{tmp})

	videoPath := filepath.Join(tmp, "movie.mkv")
	_ = os.WriteFile(videoPath, []byte("MKV-CONTENT"), 0o644)
	extSubPath := filepath.Join(tmp, "movie.en.srt")
	_ = os.WriteFile(extSubPath, []byte("1\n00:00:01,000 --> 00:00:02,000\nHi\n"), 0o644)
	strmPath := filepath.Join(tmp, "cloud.strm")
	_ = os.WriteFile(strmPath, []byte("https://93.184.216.34/live/video.mp4\n"), 0o644)

	rt := int64(120 * transcode.TicksPerSecond)
	seriesRated := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID,
		Type: "Series", Name: "Show", OfficialRating: "PG", IsFolder: true,
	}
	epInherited := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: seriesRated.ID, SeriesID: seriesRated.ID,
		Type: "Episode", MediaType: "Video", Name: "Ep 1", OfficialRating: "", Path: videoPath, RuntimeTicks: &rt,
	}
	movieUnrated := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID,
		Type: "Movie", MediaType: "Video", Name: "Unrated", OfficialRating: "", Path: videoPath, RuntimeTicks: &rt,
	}
	movieUntagged := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID,
		Type: "Movie", MediaType: "Video", Name: "Untagged", OfficialRating: "G", Path: videoPath, RuntimeTicks: &rt,
	}
	audioTrack := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID,
		Type: "Audio", MediaType: "Audio", Name: "Song", OfficialRating: "G", Path: videoPath, RuntimeTicks: &rt,
	}
	strmMovie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID,
		Type: "Movie", MediaType: "Video", Name: "STRM Movie", OfficialRating: "G", Path: strmPath, IsStrm: true, RuntimeTicks: &rt,
	}
	secretMovie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libB.ID, ParentID: libB.ID,
		Type: "Movie", MediaType: "Video", Name: "Secret Movie", OfficialRating: "G", Path: videoPath,
	}
	for _, it := range []*sqlite.ItemRecord{seriesRated, epInherited, movieUnrated, movieUntagged, audioTrack, strmMovie, secretMovie} {
		if err := libRepo.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	_ = libRepo.ReplaceItemTaxonomies(ctx, seriesRated.ID, "tag", []string{"allow-me"})
	_ = libRepo.ReplaceItemTaxonomies(ctx, audioTrack.ID, "tag", []string{"allow-me"})
	_ = libRepo.ReplaceItemTaxonomies(ctx, strmMovie.ID, "tag", []string{"allow-me"})

	_ = libRepo.ReplaceMediaSourcesAndStreams(ctx, epInherited.ID, []sqlite.MediaSourceRecord{
		{
			ID: epInherited.ID, ItemID: epInherited.ID, Path: videoPath, Container: "mkv", RuntimeTicks: rt,
			Streams: []sqlite.MediaStreamRecord{
				{StreamIndex: 0, StreamType: "Video", Codec: "hevc", Width: 1920, Height: 1080, VideoRange: "DOVI"},
				{StreamIndex: 1, StreamType: "Audio", Codec: "eac3", Channels: 6},
				{StreamIndex: 2, StreamType: "Subtitle", Codec: "subrip", Language: "eng", IsExternal: false},
				{StreamIndex: 3, StreamType: "Subtitle", Codec: "srt", Language: "eng", IsExternal: true, Path: extSubPath},
				{StreamIndex: 4, StreamType: "Subtitle", Codec: "vtt", Language: "jpn", IsExternal: true, Path: "http://169.254.169.254/bad.vtt"},
			},
		},
	})

	admin, _ := authSvc.CreateUser(ctx, "admin", "adminpass", true)
	strictUser, _ := authSvc.CreateUser(ctx, "strict", "pass1234", false)
	remuxOnlyUser, _ := authSvc.CreateUser(ctx, "remuxer", "pass1234", false)

	strictPol := sqlite.DefaultUserPolicy(false)
	strictPol.EnableAllFolders = false
	strictPol.EnabledFolders = []string{libA.ID}
	strictPol.BlockUnratedItems = []string{"Movie", "Series", "Music"}
	strictPol.AllowedTags = []string{"allow-me"}
	strictPol.EnableAudioPlaybackTranscoding = false
	strictPol.EnableVideoPlaybackTranscoding = false
	strictPol.EnablePlaybackRemuxing = false
	_ = userRepo.UpdateUserPolicy(ctx, strictUser.ID, strictPol)
	strictUser.Policy = strictPol

	remuxPol := sqlite.DefaultUserPolicy(false)
	remuxPol.EnableVideoPlaybackTranscoding = false
	remuxPol.EnablePlaybackRemuxing = true
	_ = userRepo.UpdateUserPolicy(ctx, remuxOnlyUser.ID, remuxPol)
	remuxOnlyUser.Policy = remuxPol

	// Public vs Private playlist visibility
	pubPl, _ := libRepo.CreatePlaylist(ctx, "Public PL", "Video", admin.ID, nil)
	privPl, _ := libRepo.CreatePlaylist(ctx, "Private PL", "Video", admin.ID, nil)
	_ = userRepo.SetServerConfig(ctx, "item_visibility_"+guid.Normalize(pubPl.ID), `{"IsPublic":true}`)

	var fallbackCtxUID string
	h := New(libRepo, mgr, cfg)
	h.SetUserRepo(userRepo)
	var strmFail bool
	h.SetStreamResolverWithHeaders(func(ctx context.Context, rawPath string) (string, map[string]string, error) {
		if strmFail {
			return "", nil, errors.New("strm resolve failed")
		}
		return "https://93.184.216.34/live/video.mp4", map[string]string{"Authorization": "Bearer hls-tok"}, nil
	})

	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{
		UserIDFromCtx: func(context.Context) string { return fallbackCtxUID },
	})

	adminAC := &middleware.AuthContext{User: admin, Token: "tok-adm", Client: auth.ClientInfo{DeviceID: "dev-adm"}}
	strictAC := &middleware.AuthContext{User: strictUser, Token: "tok-str", Client: auth.ClientInfo{DeviceID: "dev-str"}}
	remuxAC := &middleware.AuthContext{User: remuxOnlyUser, Token: "tok-rmx"}
	apiKeyAC := &middleware.AuthContext{IsAPIKey: true, Token: "apikey-tok"}

	doAs := func(ac *middleware.AuthContext, method, target, body, remoteAddr string) *httptest.ResponseRecorder {
		var rdr *bytes.Buffer
		if body != "" {
			rdr = bytes.NewBufferString(body)
		} else {
			rdr = &bytes.Buffer{}
		}
		req := httptest.NewRequest(method, target, rdr)
		if remoteAddr != "" {
			req.RemoteAddr = remoteAddr
		}
		if ac != nil {
			req = req.WithContext(middleware.WithAuthContext(req.Context(), ac))
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// 1. Extended policy & playlist visibility checks
	if matchesUnratedType("Folder", "", []string{"*"}) {
		t.Fatalf("Folder should be exempt from matchesUnratedType")
	}
	if !matchesUnratedType("Audio", "Audio", []string{"", "Music"}) {
		t.Fatalf("Audio should match Music in matchesUnratedType")
	}
	if !itemHasAnyAllowedTag(ctx, libRepo, movieUntagged, []string{"  "}) {
		t.Fatalf("whitespace-only allowedTags should return true")
	}

	for _, deniedID := range []string{movieUnrated.ID, movieUntagged.ID, secretMovie.ID, privPl.ID} {
		if code := doAs(strictAC, http.MethodGet, "/Videos/"+deniedID+"/master.m3u8", "", "127.0.0.1:1234").Code; code != http.StatusForbidden {
			t.Fatalf("expected 403 for denied item %s, got %d", deniedID, code)
		}
	}
	// Public playlist passes canAccessItem (fails transcode auth later with 403 transcoding disabled, proving canAccessItem succeeded!)
	recPubPl := doAs(remuxAC, http.MethodGet, "/Videos/"+pubPl.ID+"/master.m3u8?VideoCodec=copy", "", "127.0.0.1:1234")
	if recPubPl.Code != http.StatusOK {
		t.Fatalf("expected remux user to access public playlist master.m3u8, got %d %s", recPubPl.Code, recPubPl.Body.String())
	}

	// 2. authorizeAndAcquireHLS: audio transcode disabled, remux-only allowed vs burn-in/video transcode denied, stream limit 429
	if code := doAs(strictAC, http.MethodGet, "/Videos/"+audioTrack.ID+"/master.m3u8", "", "127.0.0.1:1234").Code; code != http.StatusForbidden {
		t.Fatalf("expected 403 when EnableAudioPlaybackTranscoding=false, got %d", code)
	}
	if code := doAs(strictAC, http.MethodGet, "/Videos/"+epInherited.ID+"/master.m3u8?VideoCodec=copy", "", "127.0.0.1:1234").Code; code != http.StatusForbidden {
		t.Fatalf("expected 403 when both remuxing and transcoding are false, got %d", code)
	}
	if code := doAs(remuxAC, http.MethodGet, "/Videos/"+epInherited.ID+"/master.m3u8?VideoCodec=copy", "", "127.0.0.1:1234").Code; code != http.StatusOK {
		t.Fatalf("expected 200 for remux-only (VideoCodec=copy) when EnablePlaybackRemuxing=true, got %d", code)
	}
	if code := doAs(remuxAC, http.MethodGet, "/Videos/"+epInherited.ID+"/master.m3u8?VideoCodec=copy&SubtitleMethod=Encode", "", "127.0.0.1:1234").Code; code != http.StatusForbidden {
		t.Fatalf("expected 403 when SubtitleMethod=Encode requires video transcoding, got %d", code)
	}
	if code := doAs(remuxAC, http.MethodGet, "/Videos/"+epInherited.ID+"/master.m3u8?VideoCodec=h264", "", "127.0.0.1:1234").Code; code != http.StatusForbidden {
		t.Fatalf("expected 403 when VideoCodec=h264 requires video transcoding, got %d", code)
	}

	// APIKey caller & fallback UserIDFromCtx callerIdentity
	if code := doAs(apiKeyAC, http.MethodGet, "/Videos/"+epInherited.ID+"/master.m3u8", "", "127.0.0.1:1234").Code; code != http.StatusOK {
		t.Fatalf("expected 200 for APIKey caller, got %d", code)
	}
	fallbackCtxUID = admin.ID
	if code := doAs(nil, http.MethodGet, "/Videos/"+epInherited.ID+"/master.m3u8", "", "127.0.0.1:1234").Code; code != http.StatusOK {
		t.Fatalf("expected 200 via fallbackCtxUID admin, got %d", code)
	}
	fallbackCtxUID = remuxOnlyUser.ID
	if code := doAs(nil, http.MethodGet, "/Videos/"+epInherited.ID+"/master.m3u8?VideoCodec=copy", "", "127.0.0.1:1234").Code; code != http.StatusOK {
		t.Fatalf("expected 200 via fallbackCtxUID remuxOnlyUser, got %d", code)
	}
	fallbackCtxUID = ""

	// 3. Remote bitrate clamping, custom subnets, buildSessionParamsChecked clamps & subtitle streams
	// Public IP 8.8.8.8 clamps bandwidth to RemoteClientBitrateLimit (1,500,000)
	recRemote := doAs(adminAC, http.MethodGet, "/Videos/"+epInherited.ID+"/master.m3u8?VideoBitrate=8000000&AudioBitrate=9999999&SegmentLength=99&TranscodingMaxAudioChannels=99&SubtitleStreamIndex=2", "", "8.8.8.8:50000")
	if recRemote.Code != http.StatusOK || !strings.Contains(recRemote.Body.String(), "BANDWIDTH=") {
		t.Fatalf("remote master.m3u8 failed: %d %s", recRemote.Code, recRemote.Body.String())
	}
	// Custom subnet 203.0.113.55 and single IP 198.51.100.8 are treated as local
	for _, lanAddr := range []string{"203.0.113.55:50000", "198.51.100.8:50000"} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = lanAddr
		if isRemoteClientRequest(r, cfg) {
			t.Fatalf("expected %s to be treated as local subnet", lanAddr)
		}
	}
	// Verify external subtitle vs unsafe HTTP subtitle in buildSessionParamsChecked
	rExtSub := httptest.NewRequest(http.MethodGet, "/Videos/"+epInherited.ID+"/main.m3u8?SubtitleStreamIndex=3&SegmentLength=1&AudioCodec=weirdcodec&TranscodingMaxAudioChannels=0", nil)
	rExtSub = rExtSub.WithContext(middleware.WithAuthContext(ctx, remuxAC))
	pExt := h.buildSessionParams(rExtSub, epInherited, &sqlite.MediaSourceRecord{
		ID: epInherited.ID, Path: videoPath,
		Streams: []sqlite.MediaStreamRecord{
			{StreamIndex: 3, StreamType: "Subtitle", Codec: "srt", IsExternal: true, Path: extSubPath},
		},
	}, httpx.ParseQuery(rExtSub))
	if pExt.SubtitlePath != extSubPath || pExt.SegmentDuration != minHLSSegmentLength || pExt.AudioCodec != "aac" || pExt.AudioChannels != 2 {
		t.Fatalf("unexpected buildSessionParams: %+v", pExt)
	}
	rBadSub := httptest.NewRequest(http.MethodGet, "/Videos/"+epInherited.ID+"/main.m3u8?SubtitleStreamIndex=4", nil)
	pBadSub := h.buildSessionParams(rBadSub, epInherited, &sqlite.MediaSourceRecord{
		ID: epInherited.ID, Path: videoPath,
		Streams: []sqlite.MediaStreamRecord{
			{StreamIndex: 4, StreamType: "Subtitle", Codec: "vtt", IsExternal: true, Path: "http://169.254.169.254/bad.vtt"},
		},
	}, httpx.ParseQuery(rBadSub))
	if pBadSub.SubtitlePath != "" {
		t.Fatalf("expected SSRF subtitle URL to be cleared, got %q", pBadSub.SubtitlePath)
	}

	// 4. STRM with headers & failure 502, plus handleInitSegment (200 cached, 404, 403, 502, 503)
	recStrmOK := doAs(adminAC, http.MethodGet, "/Videos/"+strmMovie.ID+"/main.m3u8?PlaySessionId=strm-ps&SegmentContainer=m4s", "", "127.0.0.1:1234")
	if recStrmOK.Code != http.StatusOK {
		t.Fatalf("STRM main.m3u8 failed: %d %s", recStrmOK.Code, recStrmOK.Body.String())
	}
	// Pre-create init.mp4 in session WorkDir so handleInitSegment serves 200 video/mp4!
	for _, s := range mgr.ActiveSessions() {
		if s.PlaySessionID == "strm-ps" {
			_ = os.WriteFile(filepath.Join(s.WorkDir, "init.mp4"), []byte("FMP4-VIDEO-INIT"), 0o644)
		}
	}
	recInit := doAs(adminAC, http.MethodGet, "/Videos/"+strmMovie.ID+"/hls1/main/init.mp4?PlaySessionId=strm-ps&SegmentContainer=m4s", "", "127.0.0.1:1234")
	if recInit.Code != http.StatusOK || recInit.Header().Get("Content-Type") != "video/mp4" || recInit.Body.String() != "FMP4-VIDEO-INIT" {
		t.Fatalf("handleInitSegment failed: %d ct=%s body=%q", recInit.Code, recInit.Header().Get("Content-Type"), recInit.Body.String())
	}
	// Also test /Videos/{Id}/init.mp4 without SegmentContainer query (defaults to m4s)
	recInitShort := doAs(adminAC, http.MethodGet, "/Videos/"+strmMovie.ID+"/init.mp4?PlaySessionId=strm-ps", "", "127.0.0.1:1234")
	if recInitShort.Code != http.StatusOK || recInitShort.Body.String() != "FMP4-VIDEO-INIT" {
		t.Fatalf("short /Videos/{Id}/init.mp4 failed: %d", recInitShort.Code)
	}
	if code := doAs(adminAC, http.MethodGet, "/Videos/"+guid.New()+"/init.mp4", "", "127.0.0.1:1234").Code; code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing item on init.mp4, got %d", code)
	}
	if code := doAs(strictAC, http.MethodGet, "/Videos/"+secretMovie.ID+"/init.mp4", "", "127.0.0.1:1234").Code; code != http.StatusForbidden {
		t.Fatalf("expected 403 for forbidden item on init.mp4, got %d", code)
	}

	strmFail = true
	for _, path := range []string{
		"/Videos/" + strmMovie.ID + "/master.m3u8",
		"/Videos/" + strmMovie.ID + "/main.m3u8",
		"/Videos/" + strmMovie.ID + "/init.mp4",
		"/Videos/" + strmMovie.ID + "/hls1/main/0.ts",
	} {
		if code := doAs(adminAC, http.MethodGet, path, "", "127.0.0.1:1234").Code; code != http.StatusBadGateway {
			t.Fatalf("expected 502 on %s when STRM fails, got %d", path, code)
		}
	}
	strmFail = false

	// serveHLSFile missing & directory branches
	wMissing := httptest.NewRecorder()
	serveHLSFile(wMissing, httptest.NewRequest(http.MethodGet, "/", nil), filepath.Join(tmp, "missing.ts"))
	if wMissing.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing hls file, got %d", wMissing.Code)
	}
	wDir := httptest.NewRecorder()
	serveHLSFile(wDir, httptest.NewRequest(http.MethodGet, "/", nil), tmp)
	if wDir.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for dir in serveHLSFile, got %d", wDir.Code)
	}

	// 5. Per-user LiveStreams limit (16 -> 429) & pruneLiveStreamsLocked eviction
	for i := 0; i < maxLiveStreamsPerUser; i++ {
		w := doAs(remuxAC, http.MethodPost, "/LiveStreams/Open", `{}`, "127.0.0.1:1234")
		if w.Code != http.StatusOK {
			t.Fatalf("LiveStream #%d expected 200, got %d", i, w.Code)
		}
	}
	if code := doAs(remuxAC, http.MethodPost, "/LiveStreams/Open", `{}`, "127.0.0.1:1234").Code; code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on 17th LiveStream for non-admin user, got %d", code)
	}

	h.liveMu.Lock()
	h.liveStreams["expired-1"] = &liveStreamEntry{LiveStreamID: "expired-1", CreatedAt: time.Now().UTC().Add(-liveStreamTTL - time.Minute)}
	for i := 0; i < maxLiveStreams+2; i++ {
		id := fmt.Sprintf("cap-%d", i)
		h.liveStreams[id] = &liveStreamEntry{LiveStreamID: id, CreatedAt: time.Now().UTC().Add(time.Duration(i) * time.Millisecond)}
	}
	h.pruneLiveStreamsLocked(time.Now().UTC())
	remCount := len(h.liveStreams)
	h.liveMu.Unlock()
	if remCount >= maxLiveStreams {
		t.Fatalf("expected pruneLiveStreamsLocked to cap below %d, got %d", maxLiveStreams, remCount)
	}

	// Helper edge cases
	if isHDRVideoRange("SDR") || isHDRVideoRange("UNKNOWN_RANGE") || !isHDRVideoRange("HLG") {
		t.Fatalf("unexpected isHDRVideoRange behavior")
	}
	if isSafeStrmURL("ftp://example.com/a.mp4") || isSafeStrmURL("http://metadata.google.internal/x") || !isBlockedStrmHost("") {
		t.Fatalf("unexpected isSafeStrmURL / isBlockedStrmHost behavior")
	}
}
