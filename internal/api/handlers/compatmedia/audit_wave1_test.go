package compatmedia

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/imaging"
	"github.com/ambra-media/ambra/internal/core/scanner"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestAuditWave1_CompatMediaHardeningAndParity(t *testing.T) {
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "data")
	mediaRoot := filepath.Join(tmp, "media")
	_ = os.MkdirAll(mediaRoot, 0o755)

	cfg, err := config.Load(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.FFmpegPath = "/nonexistent/ffmpeg-must-not-run"

	store, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	authSvc := auth.NewService(userRepo, cfg)

	_, _ = authSvc.CreateUser(ctx, "admin", "admin123", true)
	viewer, _ := authSvc.CreateUser(ctx, "viewer", "viewer123", false)
	noPlay, _ := authSvc.CreateUser(ctx, "noplay", "noplay123", false)

	noPlayPol := noPlay.Policy
	noPlayPol.EnableMediaPlayback = false
	_ = userRepo.UpdateUserPolicy(ctx, noPlay.ID, noPlayPol)

	viewerPol := viewer.Policy
	viewerPol.EnableAllFolders = true
	viewerPol.EnableLiveTvAccess = false
	viewerPol.BlockUnratedItems = []string{"Movie"}
	viewerPol.AllowedTags = []string{"family"}
	_ = userRepo.UpdateUserPolicy(ctx, viewer.ID, viewerPol)

	adminLogin, _ := authSvc.AuthenticateByName(ctx, "admin", "admin123", "", auth.ClientInfo{DeviceID: "d-adm"})
	viewerLogin, _ := authSvc.AuthenticateByName(ctx, "viewer", "viewer123", "", auth.ClientInfo{DeviceID: "d-view"})
	noPlayLogin, _ := authSvc.AuthenticateByName(ctx, "noplay", "noplay123", "", auth.ClientInfo{DeviceID: "d-np"})

	moviesLib, _ := repo.CreateLibrary(ctx, "Movies", "movies", []string{mediaRoot})
	dvrLib, _ := repo.CreateLibrary(ctx, "Recordings", "livetv", []string{mediaRoot})

	videoFile := filepath.Join(mediaRoot, "film.mp4")
	_ = os.WriteFile(videoFile, []byte("MP4"), 0o644)

	yr := 2024
	comm := 8.4
	crit := 91.0
	seedMovie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: moviesLib.ID, ParentID: moviesLib.ID,
		Type: "Movie", MediaType: "Video", Name: "Seed Safe", OfficialRating: "G",
		ProductionYear: &yr, CommunityRating: &comm, CriticRating: &crit, Path: videoFile,
	}
	simAllowed := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: moviesLib.ID, ParentID: moviesLib.ID,
		Type: "Movie", MediaType: "Video", Name: "Allowed Similar", OfficialRating: "PG", Path: videoFile,
	}
	simUnrated := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: moviesLib.ID, ParentID: moviesLib.ID,
		Type: "Movie", MediaType: "Video", Name: "Unrated Similar", OfficialRating: "", Path: videoFile,
	}
	simUntagged := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: moviesLib.ID, ParentID: moviesLib.ID,
		Type: "Movie", MediaType: "Video", Name: "Untagged Similar", OfficialRating: "G", Path: videoFile,
	}
	dvrItem := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: dvrLib.ID, ParentID: dvrLib.ID,
		Type: "Movie", MediaType: "Video", Name: "DVR Movie", OfficialRating: "G", Path: videoFile,
	}
	for _, it := range []*sqlite.ItemRecord{seedMovie, simAllowed, simUnrated, simUntagged, dvrItem} {
		if err := repo.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
		_ = repo.ReplaceItemTaxonomies(ctx, it.ID, "genre", []string{"SciFi"})
	}
	_ = repo.ReplaceItemTaxonomies(ctx, seedMovie.ID, "tag", []string{"family"})
	_ = repo.ReplaceItemTaxonomies(ctx, simAllowed.ID, "tag", []string{"family"})
	_ = repo.ReplaceItemTaxonomies(ctx, simUnrated.ID, "tag", []string{"family"})
	_ = repo.ReplaceItemTaxonomies(ctx, dvrItem.ID, "tag", []string{"family"})

	reg := plugins.NewRegistry()
	sc := scanner.New(repo, reg, eventbus.New(), cfg)
	imgSvc := imaging.New(repo).WithCacheDir(cfg.ImageCacheDir())
	tcMgr := transcode.NewManager(cfg, transcode.WithCapabilities(transcode.SoftwareOnlyCapabilities()))
	defer tcMgr.Close()

	h := New(repo, sc, imgSvc, reg, store, cfg)
	h.SetUserRepo(userRepo)
	h.SetTranscodeManager(tcMgr)

	authMW := middleware.NewAuth(authSvc, nil)
	router := h.BuildRouter(AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
	})

	call := func(token, method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("X-Emby-Token", token)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	// 1. H-03: EnableMediaPlayback=false returns 403 on compat dynamic HLS endpoints.
	if code := call(noPlayLogin.AccessToken, http.MethodGet, "/Videos/"+seedMovie.ID+"/hls/p1/0.ts", "").Code; code != http.StatusForbidden {
		t.Fatalf("H-03: expected 403 on /Videos/.../hls when EnableMediaPlayback=false, got %d", code)
	}
	if code := call(noPlayLogin.AccessToken, http.MethodGet, "/Audio/"+seedMovie.ID+"/hls/p1/0.ts", "").Code; code != http.StatusForbidden {
		t.Fatalf("H-03: expected 403 on /Audio/.../hls when EnableMediaPlayback=false, got %d", code)
	}

	// 2. H-07: HEAD on /Videos/{Id}/hls/... and /Audio/{Id}/hls/... returns 200 without spawning FFmpeg!
	if rec := call(viewerLogin.AccessToken, http.MethodHead, "/Videos/"+seedMovie.ID+"/hls/p1/0.ts", ""); rec.Code != http.StatusOK || tcMgr.ActiveSessionCount() != 0 {
		t.Fatalf("H-07: expected HEAD video HLS -> 200 with 0 sessions, got code=%d sessions=%d", rec.Code, tcMgr.ActiveSessionCount())
	}
	if rec := call(viewerLogin.AccessToken, http.MethodHead, "/Audio/"+seedMovie.ID+"/hls/p1/0.ts", ""); rec.Code != http.StatusOK || tcMgr.ActiveSessionCount() != 0 {
		t.Fatalf("H-07: expected HEAD audio HLS -> 200 with 0 sessions, got code=%d sessions=%d", rec.Code, tcMgr.ActiveSessionCount())
	}

	// 3. H-04 & H-05: Similar & InstantMix post-filter unrated, untagged, and livetv items!
	simRec := call(viewerLogin.AccessToken, http.MethodGet, "/Items/"+seedMovie.ID+"/Similar", "")
	if simRec.Code != http.StatusOK {
		t.Fatalf("Similar status=%d body=%s", simRec.Code, simRec.Body.String())
	}
	var simRes dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(simRec.Body.Bytes(), &simRes)
	if simRes.TotalRecordCount != 1 || len(simRes.Items) != 1 || simRes.Items[0].Id != simAllowed.ID {
		t.Fatalf("H-04/H-05: expected only simAllowed in Similar results, got total=%d items=%+v", simRes.TotalRecordCount, simRes.Items)
	}

	mixRec := call(viewerLogin.AccessToken, http.MethodGet, "/Items/"+seedMovie.ID+"/InstantMix", "")
	var mixRes dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(mixRec.Body.Bytes(), &mixRes)
	for _, it := range mixRes.Items {
		if it.Id == simUnrated.ID || it.Id == simUntagged.ID || it.Id == dvrItem.ID {
			t.Fatalf("H-04/H-05: InstantMix leaked blocked item %s (%s)", it.Id, it.Name)
		}
	}

	// 4. M-10: GET /Providers/Subtitles/Subtitles/{Id} requires admin!
	if code := call(viewerLogin.AccessToken, http.MethodGet, "/Providers/Subtitles/Subtitles/sub-1", "").Code; code != http.StatusForbidden {
		t.Fatalf("M-10: expected 403 for non-admin on /Providers/Subtitles/Subtitles/{Id}, got %d", code)
	}
	if code := call(adminLogin.AccessToken, http.MethodGet, "/Providers/Subtitles/Subtitles/sub-1", "").Code; code != http.StatusOK {
		t.Fatalf("M-10: expected 200 for admin on /Providers/Subtitles/Subtitles/{Id}, got %d", code)
	}

	// 5. H-09 & L-05: VirtualFolders/Paths, Paths/Update, and TranscodingTempPath reject unsafe paths!
	for _, badPath := range []string{"/", "/etc", "relative/dir", dataDir} {
		if code := call(adminLogin.AccessToken, http.MethodPost, "/Library/VirtualFolders/Paths",
			`{"Name":"Movies","PathInfo":{"Path":"`+badPath+`"}}`).Code; code != http.StatusBadRequest {
			t.Fatalf("H-09: expected 400 adding unsafe library path %q, got %d", badPath, code)
		}
		if code := call(adminLogin.AccessToken, http.MethodPost, "/Library/VirtualFolders/Paths/Update",
			`{"Name":"Movies","PathInfo":{"Path":"`+badPath+`"}}`).Code; code != http.StatusBadRequest {
			t.Fatalf("H-09: expected 400 updating unsafe library path %q, got %d", badPath, code)
		}
	}
	if code := call(adminLogin.AccessToken, http.MethodPost, "/Encoding/FfmpegOptions",
		`{"TranscodingTempPath":"/etc"}`).Code; code != http.StatusBadRequest {
		t.Fatalf("L-05: expected 400 for unsafe TranscodingTempPath=/etc, got %d", code)
	}

	// 6. H-13: handleSetEncodingConfig merge-patches incoming fields without wiping existing keys!
	if code := call(adminLogin.AccessToken, http.MethodPost, "/Encoding/FullToneMapOptions",
		`{"ToneMappingPeak":250,"ToneMappingAlgorithm":"mobius"}`).Code; code != http.StatusNoContent {
		t.Fatalf("FullToneMapOptions save 1 failed: %d", code)
	}
	if code := call(adminLogin.AccessToken, http.MethodPost, "/Encoding/FullToneMapOptions",
		`{"EnableToneMapping":false}`).Code; code != http.StatusNoContent {
		t.Fatalf("FullToneMapOptions partial save 2 failed: %d", code)
	}
	getTM := call(adminLogin.AccessToken, http.MethodGet, "/Encoding/FullToneMapOptions", "")
	var tmMap map[string]any
	_ = json.Unmarshal(getTM.Body.Bytes(), &tmMap)
	if tmMap["ToneMappingPeak"] != float64(250) || tmMap["ToneMappingAlgorithm"] != "mobius" || tmMap["EnableToneMapping"] != false {
		t.Fatalf("H-13: expected merge-patched encoding config preserving ToneMappingPeak=250 & mobius, got %+v", tmMap)
	}

	// 7. 3.2-6: handleUpdateItem clears ProductionYear, CommunityRating, CriticRating, and OfficialRating on null/0/""!
	clearPayload := `{"Name":"Seed Safe","ProductionYear":null,"CommunityRating":0,"CriticRating":null,"OfficialRating":""}`
	if code := call(adminLogin.AccessToken, http.MethodPost, "/Items/"+seedMovie.ID, clearPayload).Code; code != http.StatusNoContent {
		t.Fatalf("3.2-6: expected 204 clearing item fields, got %d", code)
	}
	updatedSeed, _ := repo.GetItemRecordByID(ctx, seedMovie.ID)
	if updatedSeed.ProductionYear != nil || updatedSeed.CommunityRating != nil || updatedSeed.CriticRating != nil || updatedSeed.OfficialRating != "" {
		t.Fatalf("3.2-6: expected ProductionYear/CommunityRating/CriticRating/OfficialRating to be cleared, got yr=%v comm=%v crit=%v official=%q",
			updatedSeed.ProductionYear, updatedSeed.CommunityRating, updatedSeed.CriticRating, updatedSeed.OfficialRating)
	}
}
