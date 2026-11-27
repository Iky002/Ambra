package audio

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestAuditWave1_H03_H04_H07_AudioHLSApiKeyCodecsAndHEAD(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "audio_wave1.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	libRepo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)
	cfg.FFmpegPath = "/nonexistent/ffmpeg-must-not-run"
	mgr := transcode.NewManager(cfg, transcode.WithCapabilities(transcode.SoftwareOnlyCapabilities()))
	defer mgr.Close()

	musicLib, _ := libRepo.CreateLibrary(ctx, "Music", "music", []string{tmp})
	dvrLib, _ := libRepo.CreateLibrary(ctx, "LiveTV", "livetv", []string{tmp})

	audioFile := filepath.Join(tmp, "song.flac")
	_ = os.WriteFile(audioFile, []byte("fLaC-DATA"), 0o644)
	rt := int64(30 * transcode.TicksPerSecond)

	song := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: musicLib.ID, ParentID: musicLib.ID,
		Type: "Audio", MediaType: "Audio", Name: "Song", OfficialRating: "G", Path: audioFile, RuntimeTicks: &rt,
	}
	dvrAudio := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: dvrLib.ID, ParentID: dvrLib.ID,
		Type: "Audio", MediaType: "Audio", Name: "Radio Rec", OfficialRating: "G", Path: audioFile, RuntimeTicks: &rt,
	}
	for _, it := range []*sqlite.ItemRecord{song, dvrAudio} {
		_ = libRepo.UpsertItem(ctx, it)
	}

	noPlayPol := sqlite.DefaultUserPolicy(false)
	noPlayPol.EnableMediaPlayback = false
	noPlayUser := &sqlite.UserRecord{ID: guid.New(), Name: "noplay", Policy: noPlayPol, Config: sqlite.DefaultUserConfig()}

	noLivePol := sqlite.DefaultUserPolicy(false)
	noLivePol.EnableAllFolders = true
	noLivePol.EnableLiveTvAccess = false
	noLiveUser := &sqlite.UserRecord{ID: guid.New(), Name: "nolive", Policy: noLivePol, Config: sqlite.DefaultUserConfig()}

	viewer := &sqlite.UserRecord{ID: guid.New(), Name: "viewer", Policy: sqlite.DefaultUserPolicy(false), Config: sqlite.DefaultUserConfig()}
	for _, u := range []*sqlite.UserRecord{noPlayUser, noLiveUser, viewer} {
		_ = userRepo.CreateUser(ctx, u)
	}

	h := New(libRepo, cfg)
	h.SetUserRepo(userRepo)
	h.SetTranscodeManager(mgr)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	doAs := func(u *sqlite.UserRecord, method, target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, nil)
		req.Header.Set("X-Emby-Token", "tok-ctx-99")
		req = req.WithContext(middleware.WithAuthContext(req.Context(), &middleware.AuthContext{User: u}))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// 1. H-03: EnableMediaPlayback=false -> 403 on direct stream and HLS.
	if code := doAs(noPlayUser, http.MethodGet, "/Audio/"+song.ID+"/stream.flac").Code; code != http.StatusForbidden {
		t.Fatalf("H-03: expected 403 on Audio direct stream, got %d", code)
	}
	if code := doAs(noPlayUser, http.MethodGet, "/Audio/"+song.ID+"/master.m3u8").Code; code != http.StatusForbidden {
		t.Fatalf("H-03: expected 403 on Audio master.m3u8, got %d", code)
	}

	// 2. H-04: livetv library denied when EnableLiveTvAccess=false.
	if code := doAs(noLiveUser, http.MethodGet, "/Audio/"+dvrAudio.ID+"/stream.flac").Code; code != http.StatusForbidden {
		t.Fatalf("H-04: expected 403 on livetv audio item, got %d", code)
	}

	// 3. H-07: HEAD on audio HLS segment returns 200 OK headers without spawning FFmpeg or creating sessions!
	headSeg := doAs(viewer, http.MethodHead, "/Audio/"+song.ID+"/hls1/main/0.ts?AudioBitrate=99999999&AudioChannels=99&SegmentLength=999")
	if headSeg.Code != http.StatusOK || mgr.ActiveSessionCount() != 0 {
		t.Fatalf("H-07: expected HEAD audio segment -> 200 with 0 active sessions, got code=%d sessions=%d", headSeg.Code, mgr.ActiveSessionCount())
	}

	// 4. 3.1-6: Audio master.m3u8 & main.m3u8 include RFC 6381 CODECS="fLaC" and propagate api_key!
	masterRec := doAs(viewer, http.MethodGet, "/Audio/"+song.ID+"/master.m3u8?AudioCodec=flac&SegmentContainer=mp4")
	if masterRec.Code != http.StatusOK || !strings.Contains(masterRec.Body.String(), `CODECS="fLaC"`) || !strings.Contains(masterRec.Body.String(), "api_key=tok-ctx-99") {
		t.Fatalf("3.1-6: expected CODECS=\"fLaC\" and api_key propagation in master.m3u8, got %d:\n%s", masterRec.Code, masterRec.Body.String())
	}
	mainRec := doAs(viewer, http.MethodGet, "/Audio/"+song.ID+"/main.m3u8?AudioCodec=flac&SegmentContainer=mp4")
	if mainRec.Code != http.StatusOK || !strings.Contains(mainRec.Body.String(), "api_key=tok-ctx-99") {
		t.Fatalf("3.1-6: expected api_key propagation in main.m3u8, got %d:\n%s", mainRec.Code, mainRec.Body.String())
	}
}
