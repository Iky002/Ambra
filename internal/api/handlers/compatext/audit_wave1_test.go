package compatext

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ambra-media/ambra/internal/api/handlers/livetv"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestAuditWave1_CompatExtHardeningAndRBAC(t *testing.T) {
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "data")
	libDir := filepath.Join(tmp, "library")
	outsideDir := filepath.Join(tmp, "outside")
	recDir := filepath.Join(dataDir, "recordings")
	for _, d := range []string{dataDir, libDir, outsideDir, recDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	cfg := config.Default(dataDir)
	cfg.FFmpegPath = "/secret/internal/ffmpeg-binary-path"
	cfg.RemoteClientBitrateLimit = 1_200_000

	store, err := sqlite.Open(filepath.Join(dataDir, "ce_wave1.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	libRepo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	authSvc := auth.NewService(userRepo, cfg)

	_, _ = authSvc.CreateUser(ctx, "admin", "admin123", true)
	dvrMgr, _ := authSvc.CreateUser(ctx, "dvrmgr", "dvrmgr123", false)
	noPlay, _ := authSvc.CreateUser(ctx, "noplay", "noplay123", false)
	noTC, _ := authSvc.CreateUser(ctx, "notc", "notc123", false)
	viewer, _ := authSvc.CreateUser(ctx, "viewer", "viewer123", false)

	dvrPol := dvrMgr.Policy
	dvrPol.EnableLiveTvManagement = true
	_ = userRepo.UpdateUserPolicy(ctx, dvrMgr.ID, dvrPol)

	npPol := noPlay.Policy
	npPol.EnableMediaPlayback = false
	_ = userRepo.UpdateUserPolicy(ctx, noPlay.ID, npPol)

	ntcPol := noTC.Policy
	ntcPol.EnableVideoPlaybackTranscoding = false
	ntcPol.EnablePlaybackRemuxing = true
	_ = userRepo.UpdateUserPolicy(ctx, noTC.ID, ntcPol)

	vPol := viewer.Policy
	vPol.EnableAllFolders = true
	vPol.EnableLiveTvAccess = false
	vPol.BlockUnratedItems = []string{"Series"}
	_ = userRepo.UpdateUserPolicy(ctx, viewer.ID, vPol)

	dvrMgrLogin, _ := authSvc.AuthenticateByName(ctx, "dvrmgr", "dvrmgr123", "", auth.ClientInfo{DeviceID: "d-mgr"})
	noPlayLogin, _ := authSvc.AuthenticateByName(ctx, "noplay", "noplay123", "", auth.ClientInfo{DeviceID: "d-np"})
	noTCLogin, _ := authSvc.AuthenticateByName(ctx, "notc", "notc123", "", auth.ClientInfo{DeviceID: "d-ntc"})

	moviesLib, _ := libRepo.CreateLibrary(ctx, "Movies", "movies", []string{libDir})
	dvrLib, _ := libRepo.CreateLibrary(ctx, "Recordings", "livetv", []string{recDir})

	recFile := filepath.Join(recDir, "show.ts")
	_ = os.WriteFile(recFile, []byte("REC-TS"), 0o644)

	outsideMKV := filepath.Join(outsideDir, "secret.mkv")
	_ = os.WriteFile(outsideMKV, []byte("OUTSIDE-MKV"), 0o644)
	symlinkInLib := filepath.Join(libDir, "escaped_link.mkv")
	_ = os.Symlink(outsideMKV, symlinkInLib)

	escapedItem := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: moviesLib.ID, ParentID: moviesLib.ID,
		Type: "Movie", MediaType: "Video", Name: "Escaped", OfficialRating: "G", Path: symlinkInLib,
	}
	_ = libRepo.UpsertItem(ctx, escapedItem)
	_ = libRepo.ReplaceMediaSourcesAndStreams(ctx, escapedItem.ID, []sqlite.MediaSourceRecord{{
		ID: escapedItem.ID, ItemID: escapedItem.ID, Name: "Escaped", Path: symlinkInLib,
		Streams: []sqlite.MediaStreamRecord{{
			StreamIndex: 2, StreamType: "Subtitle", Codec: "subrip", Language: "eng", IsExternal: false,
		}},
	}})

	dvrLibItem := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: dvrLib.ID, ParentID: dvrLib.ID,
		Type: "Video", MediaType: "Video", Name: "DVR Lib Item", OfficialRating: "G", Path: recFile,
	}
	unratedSeries := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: moviesLib.ID, ParentID: moviesLib.ID,
		Type: "Series", Name: "Unrated Series", OfficialRating: "", IsFolder: true,
	}
	unratedEp := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: moviesLib.ID, ParentID: unratedSeries.ID, SeriesID: unratedSeries.ID,
		Type: "Episode", MediaType: "Video", Name: "Ep 1", OfficialRating: "", Path: recFile,
	}
	for _, it := range []*sqlite.ItemRecord{dvrLibItem, unratedSeries, unratedEp} {
		_ = libRepo.UpsertItem(ctx, it)
	}

	lt := livetv.New(store, cfg)
	lt.SetUserRepo(userRepo)
	addedRec := lt.AddRecording(ctx, livetv.RecordingInfo{Id: "rec-hls-1", Name: "Rec HLS", Path: recFile})

	tcMgr := transcode.NewManager(cfg, transcode.WithCapabilities(transcode.SoftwareOnlyCapabilities()))
	defer tcMgr.Close()

	h := New(store, cfg)
	h.SetLibraryRepo(libRepo)
	h.SetUserRepo(userRepo)
	h.SetLiveTV(lt)
	h.SetTranscodeManager(tcMgr)

	_ = h.writeConfigJSON(ctx, syncConfigKey, sharedSyncState{
		Jobs:     []sharedSyncJob{{Id: "job-esc", UserId: dvrMgr.ID, TargetId: "dev-1"}},
		JobItems: []sharedSyncJobItem{{Id: "ji-esc", JobId: "job-esc", ItemId: escapedItem.ID, Status: "ReadyToTransfer"}},
	})

	authMW := middleware.NewAuth(authSvc, nil)
	router := h.BuildRouter(AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
	})

	call := func(token, remoteAddr, method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("X-Emby-Token", token)
		if remoteAddr != "" {
			req.RemoteAddr = remoteAddr
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	// 1. H-03: EnableMediaPlayback=false -> 403 on LiveRecordings stream & HLS!
	if code := call(noPlayLogin.AccessToken, "", http.MethodGet, "/LiveTv/LiveRecordings/"+addedRec.Id+"/stream", "").Code; code != http.StatusForbidden {
		t.Fatalf("H-03: expected 403 on LiveRecording stream when EnableMediaPlayback=false, got %d", code)
	}
	if code := call(noPlayLogin.AccessToken, "", http.MethodGet, "/LiveTv/LiveRecordings/"+addedRec.Id+"/hls/master.m3u8", "").Code; code != http.StatusForbidden {
		t.Fatalf("H-03: expected 403 on LiveRecording HLS when EnableMediaPlayback=false, got %d", code)
	}

	// 2. H-04 & H-05: canAccessItemID blocks livetv library items and Episode of unrated Series!
	reqViewer := httptest.NewRequest(http.MethodGet, "/", nil)
	reqViewer = reqViewer.WithContext(middleware.WithAuthContext(ctx, &middleware.AuthContext{User: viewer}))
	if h.canAccessItemID(reqViewer, dvrLibItem.ID) {
		t.Fatal("H-04: expected viewer with EnableLiveTvAccess=false to be denied livetv library item")
	}
	if h.canAccessItemID(reqViewer, unratedEp.ID) {
		t.Fatal("H-05: expected viewer with BlockUnratedItems=[Series] to be denied Episode of unrated Series")
	}

	// 3. H-07: LiveRecording dynamic HLS HEAD early-return, transcode/remux policy, remote bitrate clamp, and sanitized 502!
	for _, ep := range []string{"master.m3u8", "live.m3u8", "init.mp4", "0.ts"} {
		rec := call(dvrMgrLogin.AccessToken, "", http.MethodHead, "/LiveTv/LiveRecordings/"+addedRec.Id+"/hls/"+ep, "")
		if rec.Code != http.StatusOK || tcMgr.ActiveSessionCount() != 0 {
			t.Fatalf("H-07: HEAD %s expected 200 with 0 sessions, got code=%d sessions=%d", ep, rec.Code, tcMgr.ActiveSessionCount())
		}
	}
	// Transcoding disabled user gets 403 on h264 transcode, 200 on VideoCodec=copy (since EnablePlaybackRemuxing=true)!
	if code := call(noTCLogin.AccessToken, "", http.MethodGet, "/LiveTv/LiveRecordings/"+addedRec.Id+"/hls/master.m3u8?VideoCodec=h264", "").Code; code != http.StatusForbidden {
		t.Fatalf("H-07: expected 403 when EnableVideoPlaybackTranscoding=false, got %d", code)
	}
	if code := call(noTCLogin.AccessToken, "", http.MethodGet, "/LiveTv/LiveRecordings/"+addedRec.Id+"/hls/master.m3u8?VideoCodec=copy", "").Code; code != http.StatusOK {
		t.Fatalf("H-07: expected 200 when VideoCodec=copy and EnablePlaybackRemuxing=true, got %d", code)
	}
	// Remote non-LAN IP clamps master.m3u8 BANDWIDTH to RemoteClientBitrateLimit (1,200,000) and never overflows on huge VideoBitrate!
	remoteMaster := call(dvrMgrLogin.AccessToken, "8.8.8.8:51234", http.MethodGet,
		"/LiveTv/LiveRecordings/"+addedRec.Id+"/hls/master.m3u8?VideoBitrate=2000000000&AudioBitrate=192000", "")
	if remoteMaster.Code != http.StatusOK || !strings.Contains(remoteMaster.Body.String(), "BANDWIDTH=1200000") {
		t.Fatalf("H-07: expected remote master playlist BANDWIDTH clamped to 1200000, got %d:\n%s", remoteMaster.Code, remoteMaster.Body.String())
	}
	// Failing FFmpeg returns sanitized 502 without leaking internal binary path!
	seg502 := call(dvrMgrLogin.AccessToken, "", http.MethodGet, "/LiveTv/LiveRecordings/"+addedRec.Id+"/hls/0.ts?PlaySessionId=fail-ps", "")
	if seg502.Code != http.StatusBadGateway || strings.Contains(seg502.Body.String(), "ffmpeg-binary-path") {
		t.Fatalf("H-07: expected sanitized 502 without internal path leak, got %d: %s", seg502.Code, seg502.Body.String())
	}

	// 4. M-10: Embedded subtitle extraction in AdditionalFiles rejects symlink escaping outside registered libraries!
	var probeMarker atomic.Int32
	fakeExtractFFmpeg := filepath.Join(tmp, "spy-ffmpeg.sh")
	markerFile := filepath.Join(tmp, "ffmpeg_ran_marker")
	_ = os.WriteFile(fakeExtractFFmpeg, []byte("#!/bin/sh\ntouch "+markerFile+"\nexit 1\n"), 0o755)
	cfg.FFmpegPath = fakeExtractFFmpeg
	_ = probeMarker

	addRec := call(dvrMgrLogin.AccessToken, "", http.MethodGet, "/Sync/JobItems/ji-esc/AdditionalFiles?Name=sub.srt&StreamIndex=2", "")
	if addRec.Code != http.StatusOK || addRec.Body.Len() != 0 {
		t.Fatalf("M-10: expected empty fallback response for escaped symlink mediaPath, got %d %q", addRec.Code, addRec.Body.String())
	}
	if _, mErr := os.Stat(markerFile); !os.IsNotExist(mErr) {
		t.Fatal("M-10: FFmpeg was invoked on symlink-escaped file outside registered library roots!")
	}

	// 5. 3.1-8: SetNotificationTester dispatches real test notification and propagates errors as 502!
	var notifCalls atomic.Int32
	h.SetNotificationTester(func(_ context.Context, title, desc string) error {
		notifCalls.Add(1)
		if title == "" || desc == "" {
			return errors.New("empty payload")
		}
		return nil
	})
	if code := call(dvrMgrLogin.AccessToken, "", http.MethodPost, "/Notifications/Services/Test", `{}`).Code; code != http.StatusNoContent || notifCalls.Load() != 1 {
		t.Fatalf("3.1-8: expected 204 and 1 notification tester invocation, got code=%d calls=%d", code, notifCalls.Load())
	}
	h.SetNotificationTester(func(_ context.Context, _, _ string) error {
		return errors.New("webhook unreachable")
	})
	if code := call(dvrMgrLogin.AccessToken, "", http.MethodPost, "/Notifications/Services/Test", `{}`).Code; code != http.StatusBadGateway {
		t.Fatalf("3.1-8: expected 502 when notification tester fails, got %d", code)
	}

	// 6. 3.2-5: EnableLiveTvManagement non-admin user can access ChannelMappingOptions & ChannelMappings; regular viewer cannot!
	if code := call(dvrMgrLogin.AccessToken, "", http.MethodGet, "/LiveTv/ChannelMappingOptions?ProviderId=p1", "").Code; code != http.StatusOK {
		t.Fatalf("3.2-5: expected 200 on ChannelMappingOptions for EnableLiveTvManagement user, got %d", code)
	}
	if code := call(dvrMgrLogin.AccessToken, "", http.MethodPost, "/LiveTv/ChannelMappings",
		`{"ProviderId":"p1","TunerChannelId":"ch1","ProviderChannelId":"epg1"}`).Code; code != http.StatusOK {
		t.Fatalf("3.2-5: expected 200 on ChannelMappings for EnableLiveTvManagement user, got %d", code)
	}
	if code := call(noTCLogin.AccessToken, "", http.MethodGet, "/LiveTv/ChannelMappingOptions?ProviderId=p1", "").Code; code != http.StatusForbidden {
		t.Fatalf("3.2-5: expected 403 on ChannelMappingOptions for user without EnableLiveTvManagement, got %d", code)
	}
}
