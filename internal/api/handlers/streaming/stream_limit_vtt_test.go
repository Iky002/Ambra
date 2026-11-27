package streaming

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
	"github.com/ambra-media/ambra/internal/core/imaging"
	"github.com/ambra-media/ambra/internal/core/playback"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestAuditWave1_H03_H04_H05_StreamLimitAndVTTTimeMap(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "streaming_wave1.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	libRepo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)
	engine := playback.New(libRepo, plugins.NewRegistry(), cfg)
	engine.SetUserRepo(userRepo)
	imgSvc := imaging.New(libRepo).WithCacheDir(filepath.Join(tmp, "imgcache"))
	tcMgr := transcode.NewManager(cfg, transcode.WithCapabilities(transcode.SoftwareOnlyCapabilities()))
	defer tcMgr.Close()

	moviesLib, _ := libRepo.CreateLibrary(ctx, "Movies", "movies", []string{tmp})
	dvrLib, _ := libRepo.CreateLibrary(ctx, "Recordings", "livetv", []string{tmp})

	videoFile := filepath.Join(tmp, "movie.mp4")
	_ = os.WriteFile(videoFile, []byte("MP4-STREAM-BYTES"), 0o644)
	subFile := filepath.Join(tmp, "movie.en.srt")
	_ = os.WriteFile(subFile, []byte("1\n00:00:01,000 --> 00:00:03,000\nSub line\n"), 0o644)

	safeMovie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: moviesLib.ID, ParentID: moviesLib.ID,
		Type: "Movie", MediaType: "Video", Name: "Safe", OfficialRating: "G", Path: videoFile,
	}
	dvrRecItem := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: dvrLib.ID, ParentID: dvrLib.ID,
		Type: "Video", MediaType: "Video", Name: "Recorded Show", OfficialRating: "G", Path: videoFile,
	}
	unratedMovie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: moviesLib.ID, ParentID: moviesLib.ID,
		Type: "Movie", MediaType: "Video", Name: "Unrated", OfficialRating: "", Path: videoFile,
	}
	for _, it := range []*sqlite.ItemRecord{safeMovie, dvrRecItem, unratedMovie} {
		if err := libRepo.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
		_ = libRepo.ReplaceMediaSourcesAndStreams(ctx, it.ID, []sqlite.MediaSourceRecord{{
			ID: it.ID, ItemID: it.ID, Name: it.Name, Path: videoFile, Protocol: "File", Container: "mp4",
			Streams: []sqlite.MediaStreamRecord{{
				StreamIndex: 2, StreamType: "Subtitle", Codec: "srt", Language: "eng", IsExternal: true, Path: subFile,
			}},
		}})
	}

	noPlayPol := sqlite.DefaultUserPolicy(false)
	noPlayPol.EnableMediaPlayback = false
	noPlayUser := &sqlite.UserRecord{ID: guid.New(), Name: "noplay", Policy: noPlayPol, Config: sqlite.DefaultUserConfig()}

	noLivePol := sqlite.DefaultUserPolicy(false)
	noLivePol.EnableAllFolders = true
	noLivePol.EnableLiveTvAccess = false
	noLivePol.BlockUnratedItems = []string{"Movie"}
	noLiveUser := &sqlite.UserRecord{ID: guid.New(), Name: "nolive", Policy: noLivePol, Config: sqlite.DefaultUserConfig()}

	viewerUser := &sqlite.UserRecord{ID: guid.New(), Name: "viewer", Policy: sqlite.DefaultUserPolicy(false), Config: sqlite.DefaultUserConfig()}

	for _, u := range []*sqlite.UserRecord{noPlayUser, noLiveUser, viewerUser} {
		if err := userRepo.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
	}

	h := New(libRepo, engine, imgSvc)
	h.SetUserRepo(userRepo)
	h.SetTranscodeManager(tcMgr)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	doAs := func(u *sqlite.UserRecord, method, target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, nil)
		ac := &middleware.AuthContext{User: u}
		req = req.WithContext(middleware.WithAuthContext(req.Context(), ac))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// 1. H-03: EnableMediaPlayback=false denies PlaybackInfo and DirectStream with 403.
	if code := doAs(noPlayUser, http.MethodGet, "/Items/"+safeMovie.ID+"/PlaybackInfo").Code; code != http.StatusForbidden {
		t.Fatalf("H-03: expected 403 on PlaybackInfo when EnableMediaPlayback=false, got %d", code)
	}
	if code := doAs(noPlayUser, http.MethodGet, "/Videos/"+safeMovie.ID+"/stream.mp4").Code; code != http.StatusForbidden {
		t.Fatalf("H-03: expected 403 on DirectStream when EnableMediaPlayback=false, got %d", code)
	}

	// 2. H-04 & H-05: livetv library denied when EnableLiveTvAccess=false (even with EnableAllFolders=true), and BlockUnratedItems enforced.
	if code := doAs(noLiveUser, http.MethodGet, "/Videos/"+dvrRecItem.ID+"/stream.mp4").Code; code != http.StatusForbidden {
		t.Fatalf("H-04: expected 403 for livetv library item when EnableLiveTvAccess=false, got %d", code)
	}
	if code := doAs(noLiveUser, http.MethodGet, "/Videos/"+unratedMovie.ID+"/stream.mp4").Code; code != http.StatusForbidden {
		t.Fatalf("H-05: expected 403 for unrated movie when BlockUnratedItems=[Movie], got %d", code)
	}

	// 3. 3.1-3: SimultaneousStreamLimit enforced on DirectStream when another slot is held!
	cfg.SimultaneousStreamLimit = 1
	rel, ok := tcMgr.TryAcquireStreamSlot(1, "other-user:active-1")
	if !ok {
		t.Fatal("failed to reserve test stream slot")
	}
	if code := doAs(viewerUser, http.MethodGet, "/Videos/"+safeMovie.ID+"/stream.mp4?PlaySessionId=ps-new").Code; code != http.StatusTooManyRequests {
		rel()
		t.Fatalf("3.1-3: expected 429 on DirectStream when SimultaneousStreamLimit=1 is full, got %d", code)
	}
	rel()
	if rec := doAs(viewerUser, http.MethodGet, "/Videos/"+safeMovie.ID+"/stream.mp4?PlaySessionId=ps-new"); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on DirectStream once slot released, got %d", rec.Code)
	}

	// 4. 3.1-7: Subtitle endpoint honors addVttTimeMap=true.
	subRec := doAs(viewerUser, http.MethodGet, "/Videos/"+safeMovie.ID+"/"+safeMovie.ID+"/Subtitles/2/Stream.vtt?addVttTimeMap=true")
	if subRec.Code != http.StatusOK || !strings.Contains(subRec.Body.String(), "X-TIMESTAMP-MAP=MPEGTS:900000,LOCAL:00:00:00.000") {
		t.Fatalf("3.1-7: expected X-TIMESTAMP-MAP in WebVTT subtitle, got %d:\n%s", subRec.Code, subRec.Body.String())
	}
}
