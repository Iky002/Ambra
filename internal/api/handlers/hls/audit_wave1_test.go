package hls

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

func TestAuditWave1_H03_H04_H07_HEADAndSubtitleVTTTimeMap(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "hls_wave1.db"))
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

	moviesLib, _ := libRepo.CreateLibrary(ctx, "Movies", "movies", []string{tmp})
	dvrLib, _ := libRepo.CreateLibrary(ctx, "DVR", "livetv", []string{tmp})

	videoPath := filepath.Join(tmp, "vid.mkv")
	_ = os.WriteFile(videoPath, []byte("MKV"), 0o644)
	srtPath := filepath.Join(tmp, "vid.srt")
	_ = os.WriteFile(srtPath, []byte("1\n00:00:01,000 --> 00:00:02,000\nHello\n"), 0o644)

	rt := int64(60 * transcode.TicksPerSecond)
	movie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: moviesLib.ID, ParentID: moviesLib.ID,
		Type: "Movie", MediaType: "Video", Name: "Movie", OfficialRating: "G", Path: videoPath, RuntimeTicks: &rt,
	}
	dvrItem := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: dvrLib.ID, ParentID: dvrLib.ID,
		Type: "Video", MediaType: "Video", Name: "Rec", OfficialRating: "G", Path: videoPath, RuntimeTicks: &rt,
	}
	for _, it := range []*sqlite.ItemRecord{movie, dvrItem} {
		_ = libRepo.UpsertItem(ctx, it)
		_ = libRepo.ReplaceMediaSourcesAndStreams(ctx, it.ID, []sqlite.MediaSourceRecord{{
			ID: it.ID, ItemID: it.ID, Name: it.Name, Path: videoPath, Protocol: "File", Container: "mkv", RuntimeTicks: rt,
			Streams: []sqlite.MediaStreamRecord{{
				StreamIndex: 1, StreamType: "Subtitle", Codec: "srt", Language: "eng", IsExternal: true, Path: srtPath,
			}},
		}})
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

	h := New(libRepo, mgr, cfg)
	h.SetUserRepo(userRepo)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	doAs := func(u *sqlite.UserRecord, method, target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, nil)
		req = req.WithContext(middleware.WithAuthContext(req.Context(), &middleware.AuthContext{User: u}))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// 1. H-03: EnableMediaPlayback=false returns 403 on HLS master playlist & segment.
	if code := doAs(noPlayUser, http.MethodGet, "/Videos/"+movie.ID+"/master.m3u8").Code; code != http.StatusForbidden {
		t.Fatalf("H-03: expected 403 on HLS master.m3u8 when EnableMediaPlayback=false, got %d", code)
	}
	if code := doAs(noPlayUser, http.MethodGet, "/Videos/"+movie.ID+"/hls1/main/0.ts").Code; code != http.StatusForbidden {
		t.Fatalf("H-03: expected 403 on HLS segment when EnableMediaPlayback=false, got %d", code)
	}

	// 2. H-04: livetv library item denied when EnableLiveTvAccess=false even with EnableAllFolders=true.
	if code := doAs(noLiveUser, http.MethodGet, "/Videos/"+dvrItem.ID+"/master.m3u8").Code; code != http.StatusForbidden {
		t.Fatalf("H-04: expected 403 on livetv library item in HLS, got %d", code)
	}

	// 3. H-07: HEAD on init.mp4 and 0.ts returns 200 OK headers immediately without spawning FFmpeg or registering sessions!
	headInit := doAs(viewer, http.MethodHead, "/Videos/"+movie.ID+"/hls1/main/init.mp4")
	if headInit.Code != http.StatusOK || mgr.ActiveSessionCount() != 0 {
		t.Fatalf("H-07: expected HEAD init.mp4 -> 200 with 0 active sessions, got code=%d sessions=%d", headInit.Code, mgr.ActiveSessionCount())
	}
	headSeg := doAs(viewer, http.MethodHead, "/Videos/"+movie.ID+"/hls1/main/0.ts")
	if headSeg.Code != http.StatusOK || mgr.ActiveSessionCount() != 0 {
		t.Fatalf("H-07: expected HEAD 0.ts -> 200 with 0 active sessions, got code=%d sessions=%d", headSeg.Code, mgr.ActiveSessionCount())
	}

	// 4. 3.1-7: Subtitle HLS playlist appends addVttTimeMap=true to Stream.vtt URI.
	subPL := doAs(viewer, http.MethodGet, "/Videos/"+movie.ID+"/"+movie.ID+"/Subtitles/1/subtitles.m3u8?api_key=tok1")
	if subPL.Code != http.StatusOK || !strings.Contains(subPL.Body.String(), "addVttTimeMap=true") {
		t.Fatalf("3.1-7: expected addVttTimeMap=true in subtitles.m3u8, got %d:\n%s", subPL.Code, subPL.Body.String())
	}
}
