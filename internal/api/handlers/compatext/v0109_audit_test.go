package compatext

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	syncapi "github.com/ambra-media/ambra/internal/api/handlers/sync"
	"github.com/ambra-media/ambra/internal/api/handlers/livetv"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestCompatExtV0109AuditM19M25L12L18(t *testing.T) {
	tmp := t.TempDir()
	libDir := filepath.Join(tmp, "library")
	outsideDir := filepath.Join(tmp, "outside")
	_ = os.MkdirAll(libDir, 0o755)
	_ = os.MkdirAll(outsideDir, 0o755)

	store, err := sqlite.Open(filepath.Join(tmp, "ce_v109.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	cfg := config.Default(tmp)
	userRepo := sqlite.NewUserRepo(store)
	libRepo := sqlite.NewLibraryRepo(store)
	authSvc := auth.NewService(userRepo, cfg)

	alice, _ := authSvc.CreateUser(ctx, "alice_109", "pass1234", false)
	bob, _ := authSvc.CreateUser(ctx, "bob_109", "pass1234", false)
	_, _ = authSvc.CreateUser(ctx, "admin_109", "pass1234", true)
	aliceAuth, _ := authSvc.AuthenticateByName(ctx, "alice_109", "pass1234", "", auth.ClientInfo{DeviceID: "d-a"})
	bobAuth, _ := authSvc.AuthenticateByName(ctx, "bob_109", "pass1234", "", auth.ClientInfo{DeviceID: "d-b"})
	adminAuth, _ := authSvc.AuthenticateByName(ctx, "admin_109", "pass1234", "", auth.ClientInfo{DeviceID: "d-adm"})

	lib, _ := libRepo.CreateLibrary(ctx, "Movies", "movies", []string{libDir})

	okFile := filepath.Join(libDir, "ok.mp4")
	_ = os.WriteFile(okFile, []byte("MP4OK"), 0o644)
	outsideFile := filepath.Join(outsideDir, "secret.txt")
	_ = os.WriteFile(outsideFile, []byte("OUTSIDE"), 0o644)
	symToOutside := filepath.Join(libDir, "escape.mp4")
	_ = os.Symlink(outsideFile, symToOutside)
	strmReal := filepath.Join(libDir, "real.strm")
	_ = os.WriteFile(strmReal, []byte("https://cdn.example/video.mp4"), 0o644)
	symToStrm := filepath.Join(libDir, "alias.mp4")
	_ = os.Symlink(strmReal, symToStrm)

	okItem := &sqlite.ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", MediaType: "Video", Name: "OK", Path: okFile}
	escapeItem := &sqlite.ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", MediaType: "Video", Name: "Escape", Path: symToOutside}
	strmAliasItem := &sqlite.ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", MediaType: "Video", Name: "Alias", Path: symToStrm}
	mismatchItem := &sqlite.ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", MediaType: "Video", Name: "Mismatch", Path: filepath.Join(libDir, "other.mp4")}
	_ = os.WriteFile(mismatchItem.Path, []byte("OTHER"), 0o644)
	for _, rec := range []*sqlite.ItemRecord{okItem, escapeItem, strmAliasItem, mismatchItem} {
		_ = libRepo.UpsertItem(ctx, rec)
	}

	h := New(store, cfg)
	h.SetLibraryRepo(libRepo)
	h.SetUserRepo(userRepo)
	authMW := middleware.NewAuth(authSvc, nil)
	router := h.BuildRouter(AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
	})

	call := func(tok, method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if tok != "" {
			req.Header.Set("X-Emby-Token", tok)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	// Seed sharedSyncState directly to test symlink escape, .strm symlink, path mismatch, and valid file headers (M-19)
	st := sharedSyncState{
		Jobs: []sharedSyncJob{{Id: "j-a", UserId: alice.ID, TargetId: "t1"}},
		JobItems: []sharedSyncJobItem{
			{Id: "ji-ok", JobId: "j-a", ItemId: okItem.ID, OutputPath: okFile},
			{Id: "ji-escape", JobId: "j-a", ItemId: escapeItem.ID, OutputPath: symToOutside},
			{Id: "ji-strm-sym", JobId: "j-a", ItemId: strmAliasItem.ID, OutputPath: symToStrm},
			{Id: "ji-mismatch", JobId: "j-a", ItemId: mismatchItem.ID, OutputPath: okFile},
		},
	}
	if err := h.writeConfigJSON(ctx, syncConfigKey, st); err != nil {
		t.Fatal(err)
	}

	// 1. M-19: Valid file sets security headers
	recOK := call(aliceAuth.AccessToken, http.MethodGet, "/Sync/JobItems/ji-ok/File", "")
	if recOK.Code != http.StatusOK || recOK.Body.String() != "MP4OK" {
		t.Fatalf("M-19 valid file failed: %d %q", recOK.Code, recOK.Body.String())
	}
	if recOK.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("M-19 missing nosniff header: %q", recOK.Header().Get("X-Content-Type-Options"))
	}
	if recOK.Header().Get("Content-Security-Policy") != "sandbox" {
		t.Fatalf("M-19 missing sandbox CSP header: %q", recOK.Header().Get("Content-Security-Policy"))
	}
	if !strings.HasPrefix(recOK.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("M-19 missing attachment Content-Disposition: %q", recOK.Header().Get("Content-Disposition"))
	}

	// M-19: Symlink outside library, symlink to .strm, and path mismatch with rec.Path are all blocked (403)
	for _, id := range []string{"ji-escape", "ji-strm-sym", "ji-mismatch"} {
		if rec := call(aliceAuth.AccessToken, http.MethodGet, "/Sync/JobItems/"+id+"/File", ""); rec.Code != http.StatusForbidden {
			t.Fatalf("M-19 expected 403 for %s, got %d (%s)", id, rec.Code, rec.Body.String())
		}
	}

	// 2. L-18: isHLSStreamURL aligns with livetv HLS detection
	for _, hlsURL := range []string{
		"https://cdn.example/live.m3u8",
		"https://cdn.example/playlist.m3u",
		"https://cdn.example/stream?format=m3u8",
		"https://cdn.example/stream?type=m3u8&id=1",
	} {
		if !isHLSStreamURL(hlsURL) {
			t.Fatalf("L-18: expected %q detected as HLS", hlsURL)
		}
	}
	if isHLSStreamURL("https://cdn.example/stream.ts") {
		t.Fatal("L-18: .ts stream must not be detected as HLS")
	}

	// 3. L-12: Configured notification service Options sensitive keys are masked, and Test enforces ownership + does not write DB
	recSave := call(aliceAuth.AccessToken, http.MethodPost, "/Notifications/Services/Configured", `{
		"Id": "svc-alice",
		"Name": "Alice Hook",
		"Enabled": true,
		"Options": {"api_key": "secret-123", "token": "tok-456", "channel": "#general"}
	}`)
	if recSave.Code != http.StatusOK {
		t.Fatalf("save notif service failed: %d %s", recSave.Code, recSave.Body.String())
	}
	var savedResp NotificationServiceConfig
	_ = json.Unmarshal(recSave.Body.Bytes(), &savedResp)
	if savedResp.Options["api_key"] != maskedOptionValue || savedResp.Options["token"] != maskedOptionValue || savedResp.Options["channel"] != "#general" {
		t.Fatalf("L-12: expected sensitive options masked in save response, got %+v", savedResp.Options)
	}

	recGet := call(aliceAuth.AccessToken, http.MethodGet, "/Notifications/Services/Configured", "")
	if recGet.Code != http.StatusOK {
		t.Fatalf("get notif services failed: %d", recGet.Code)
	}
	if strings.Contains(recGet.Body.String(), "secret-123") || strings.Contains(recGet.Body.String(), "tok-456") {
		t.Fatalf("L-12: raw secrets leaked in GET /Notifications/Services/Configured: %s", recGet.Body.String())
	}

	// Bob cannot trigger Test on Alice's notification service or Alice's UserId
	if rec := call(bobAuth.AccessToken, http.MethodPost, "/Notifications/Services/Test?Id=svc-alice", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("L-12: expected 403 when Bob tests Alice's service, got %d", rec.Code)
	}
	if rec := call(bobAuth.AccessToken, http.MethodPost, "/Notifications/Services/Test?UserId="+alice.ID, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("L-12: expected 403 when Bob tests Alice's UserId, got %d", rec.Code)
	}
	// Owner (Alice) and Admin can trigger Test
	if rec := call(aliceAuth.AccessToken, http.MethodPost, "/Notifications/Services/Test?Id=svc-alice", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("L-12: expected 204 when Alice tests own service, got %d", rec.Code)
	}
	if rec := call(adminAuth.AccessToken, http.MethodPost, "/Notifications/Services/Test?Id=svc-alice", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("L-12: expected 204 when Admin tests Alice's service, got %d", rec.Code)
	}
	_ = bob

	// 4. M-25: DB write failure rolls back compatext memory and returns 500 (including via SyncHandler delegation)
	sh := syncapi.New(store, cfg)
	h.SetSyncHandler(sh)
	// Seed a job item in syncHandler before closing DB
	shMux := http.NewServeMux()
	sh.RegisterRoutes(shMux, syncapi.AuthFuncs{})
	jobRec := httptest.NewRecorder()
	shMux.ServeHTTP(jobRec, httptest.NewRequest(http.MethodPost, "/Sync/Jobs", strings.NewReader(`{"ItemIds":["item-m25"]}`)).WithContext(middleware.WithAuthContext(ctx, &middleware.AuthContext{User: alice})))
	var created syncapi.SyncJobCreationResult
	_ = json.Unmarshal(jobRec.Body.Bytes(), &created)

	_ = store.Close()
	// Test endpoint does NOT write to SQLite, so it still returns 204 even with closed store (proves L-12 no unconditional disk write)!
	if rec := call(aliceAuth.AccessToken, http.MethodPost, "/Notifications/Services/Test?Id=svc-alice", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("L-12: expected Test not to write DB unconditionally, got %d", rec.Code)
	}
	// Mutating endpoints return 500 and roll back memory when DB fails (M-25)
	if rec := call(adminAuth.AccessToken, http.MethodPost, "/LiveTv/Tuners/t1/Reset", ""); rec.Code != http.StatusInternalServerError {
		t.Fatalf("M-25: expected 500 on ResetTuner DB failure, got %d", rec.Code)
	}
	if rec := call(aliceAuth.AccessToken, http.MethodPost, "/Sync/Jobs/"+created.Job.Id+"/Delete", ""); rec.Code != http.StatusInternalServerError {
		t.Fatalf("M-25: expected 500 on DeleteSyncJob DB failure, got %d", rec.Code)
	}
	if rec := call(aliceAuth.AccessToken, http.MethodPost, "/Notifications/Services/Configured", `{"Id":"svc-fail","Name":"Fail"}`); rec.Code != http.StatusInternalServerError {
		t.Fatalf("M-25: expected 500 on SaveConfiguredNotification DB failure, got %d", rec.Code)
	}
	h.mu.RLock()
	for _, svc := range h.state.NotificationServices {
		if svc.Id == "svc-fail" {
			h.mu.RUnlock()
			t.Fatal("M-25: failed notification service save was not rolled back")
		}
	}
	h.mu.RUnlock()
}

func TestCompatExtSyncCacheDownloadSubtitlesAndRecordingHLS(t *testing.T) {
	tmp := t.TempDir()
	libDir := filepath.Join(tmp, "library")
	recDir := filepath.Join(tmp, "recordings")
	_ = os.MkdirAll(libDir, 0o755)
	_ = os.MkdirAll(recDir, 0o755)

	store, err := sqlite.Open(filepath.Join(tmp, "ce_real.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	cfg := config.Default(tmp)

	// Mock ffmpeg that generates segment 0 when invoked by transcode.Manager
	mockFFmpeg := filepath.Join(tmp, "mock-ffmpeg.sh")
	ffScript := `#!/bin/sh
for last; do true; done
dir=$(dirname "$last")
printf 'TS-SEGMENT-ZERO' > "$dir/seg_0.ts"
`
	if err := os.WriteFile(mockFFmpeg, []byte(ffScript), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg.FFmpegPath = mockFFmpeg

	tcMgr := transcode.NewManager(cfg)
	defer tcMgr.Close()

	userRepo := sqlite.NewUserRepo(store)
	libRepo := sqlite.NewLibraryRepo(store)
	authSvc := auth.NewService(userRepo, cfg)
	alice, _ := authSvc.CreateUser(ctx, "alice_real", "pass1234", false)
	aliceAuth, _ := authSvc.AuthenticateByName(ctx, "alice_real", "pass1234", "", auth.ClientInfo{DeviceID: "d-ar"})

	lib, _ := libRepo.CreateLibrary(ctx, "Movies", "movies", []string{libDir})
	srcMovie := filepath.Join(libDir, "film.mkv")
	extSrt := filepath.Join(libDir, "film.zh.srt")
	_ = os.WriteFile(srcMovie, []byte("MKV-RAW"), 0o644)
	_ = os.WriteFile(extSrt, []byte("1\n00:00:01,250 --> 00:00:03,500\n你好世界\n"), 0o644)

	movieItem := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "Film", Path: srcMovie,
	}
	_ = libRepo.UpsertItem(ctx, movieItem)
	_ = libRepo.ReplaceMediaSourcesAndStreams(ctx, movieItem.ID, []sqlite.MediaSourceRecord{
		{
			ID: movieItem.ID, ItemID: movieItem.ID, Name: "film.mkv", Path: srcMovie, Container: "mkv",
			Streams: []sqlite.MediaStreamRecord{
				{StreamIndex: 2, StreamType: "Subtitle", Codec: "srt", Language: "zh", IsExternal: true, Path: extSrt},
			},
		},
	})

	// Stage a transcoded sync output inside <DataDir>/cache/sync/job-1/ji-tc.mp4
	syncCacheFile := filepath.Join(tmp, "cache", "sync", "job-1", "ji-tc.mp4")
	_ = os.MkdirAll(filepath.Dir(syncCacheFile), 0o755)
	_ = os.WriteFile(syncCacheFile, []byte("TRANSCODED-CACHE-MP4"), 0o644)

	// Stage a completed Live TV recording inside <DataDir>/recordings/rec1.ts
	recFile := filepath.Join(recDir, "rec1.ts")
	_ = os.WriteFile(recFile, []byte("RAW-RECORDING-TS"), 0o644)

	lt := livetv.New(store, cfg)
	defer lt.Close()
	addedRec := lt.AddRecording(ctx, livetv.RecordingInfo{
		Id:   "rec-hls-1",
		Name: "Recorded Match",
		Path: recFile,
	})

	h := New(store, cfg)
	h.SetLibraryRepo(libRepo)
	h.SetUserRepo(userRepo)
	h.SetLiveTV(lt)
	h.SetTranscodeManager(tcMgr)

	_ = h.writeConfigJSON(ctx, syncConfigKey, sharedSyncState{
		Jobs: []sharedSyncJob{{Id: "job-1", UserId: alice.ID, TargetId: "dev-1"}},
		JobItems: []sharedSyncJobItem{
			{Id: "ji-tc", JobId: "job-1", ItemId: movieItem.ID, OutputPath: syncCacheFile, Status: "ReadyToTransfer"},
		},
	})

	authMW := middleware.NewAuth(authSvc, nil)
	router := h.BuildRouter(AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
	})
	doReq := func(method, target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, nil)
		req.Header.Set("X-Emby-Token", aliceAuth.AccessToken)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	// 1. Download transcoded sync output from <DataDir>/cache/sync/ succeeds (200)
	recFileDL := doReq(http.MethodGet, "/Sync/JobItems/ji-tc/File")
	if recFileDL.Code != http.StatusOK || recFileDL.Body.String() != "TRANSCODED-CACHE-MP4" {
		t.Fatalf("expected 200 downloading transcoded sync cache file, got %d %q", recFileDL.Code, recFileDL.Body.String())
	}

	// 2. AdditionalFiles serves real external subtitle and converts SRT timestamps to WebVTT when .vtt requested
	recSubVTT := doReq(http.MethodGet, "/Sync/JobItems/ji-tc/AdditionalFiles?Name=sub.zh.vtt&StreamIndex=2")
	if recSubVTT.Code != http.StatusOK || !strings.HasPrefix(recSubVTT.Body.String(), "WEBVTT") || !strings.Contains(recSubVTT.Body.String(), "00:00:01.250 --> 00:00:03.500") || !strings.Contains(recSubVTT.Body.String(), "你好世界") {
		t.Fatalf("expected converted WebVTT subtitle from AdditionalFiles, got %d %q", recSubVTT.Code, recSubVTT.Body.String())
	}

	// 3. LiveRecordings HLS serves real master.m3u8, live.m3u8, and segment 0.ts via transcode.Manager!
	recMaster := doReq(http.MethodGet, "/LiveTv/LiveRecordings/"+addedRec.Id+"/hls/master.m3u8")
	if recMaster.Code != http.StatusOK || !strings.Contains(recMaster.Body.String(), "#EXTM3U") || !strings.Contains(recMaster.Body.String(), "live.m3u8") {
		t.Fatalf("expected 200 HLS master playlist for recording, got %d %q", recMaster.Code, recMaster.Body.String())
	}
	recLive := doReq(http.MethodGet, "/LiveTv/LiveRecordings/"+addedRec.Id+"/hls/live.m3u8")
	if recLive.Code != http.StatusOK || !strings.Contains(recLive.Body.String(), "0.ts") {
		t.Fatalf("expected 200 HLS media playlist for recording, got %d %q", recLive.Code, recLive.Body.String())
	}
	recSeg := doReq(http.MethodGet, "/LiveTv/LiveRecordings/"+addedRec.Id+"/hls/0.ts")
	if recSeg.Code != http.StatusOK || recSeg.Body.String() != "TS-SEGMENT-ZERO" {
		t.Fatalf("expected 200 HLS segment 0.ts via transcode.Manager, got %d %q", recSeg.Code, recSeg.Body.String())
	}
}
