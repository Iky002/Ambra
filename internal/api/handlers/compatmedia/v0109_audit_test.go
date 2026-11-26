package compatmedia

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestV0109Audit_CompatMediaSecurityAndReliability(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	userRepo := sqlite.NewUserRepo(env.store)
	authSvc := auth.NewService(userRepo, env.cfg)
	authMW := middleware.NewAuth(authSvc, nil)
	mgr := transcode.NewManager(env.cfg, transcode.WithCapabilities(transcode.SoftwareOnlyCapabilities()))
	defer mgr.Close()

	env.handler.SetUserRepo(userRepo)
	env.handler.SetTranscodeManager(mgr)
	router := env.handler.BuildRouter(AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
		UserIDFromCtx: func(c context.Context) string {
			if ac, ok := middleware.AuthFromContext(c); ok && ac != nil {
				return ac.UserID()
			}
			return ""
		},
	})

	admin, _ := authSvc.CreateUser(ctx, "admin", "adminpass", true)
	alice, _ := authSvc.CreateUser(ctx, "alice", "alicepass", false)
	bob, _ := authSvc.CreateUser(ctx, "bob", "bobpass", false)
	kid, _ := authSvc.CreateUser(ctx, "kid", "kidpass", false)

	maxPG := 5
	kidPol := sqlite.DefaultUserPolicy(false)
	kidPol.MaxParentalRating = &maxPG
	kidPol.BlockedTags = []string{"explicit"}
	_ = userRepo.UpdateUserPolicy(ctx, kid.ID, kidPol)
	kid.Policy = kidPol

	adminAC := &middleware.AuthContext{User: admin, Token: "t-admin", Client: auth.ClientInfo{DeviceID: "dev-admin"}}
	aliceAC := &middleware.AuthContext{User: alice, Token: "t-alice", Client: auth.ClientInfo{DeviceID: "dev-alice"}}
	bobAC := &middleware.AuthContext{User: bob, Token: "t-bob", Client: auth.ClientInfo{DeviceID: "dev-bob"}}
	kidAC := &middleware.AuthContext{User: kid, Token: "t-kid", Client: auth.ClientInfo{DeviceID: "dev-kid"}}

	doAs := func(ac *middleware.AuthContext, method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		req = req.WithContext(middleware.WithAuthContext(req.Context(), ac))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	// --- 1. [H-09] LocalTrailers, SpecialFeatures, ThemeSongs, ThemeVideos, ThemeMedia filter extras by parental policy ---
	lib, _ := env.repo.CreateLibrary(ctx, "KidsSafe", "movies", []string{t.TempDir()})
	movie := &sqlite.ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", MediaType: "Video", Name: "Family Film", OfficialRating: "G"}
	trOK := &sqlite.ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: movie.ID, Type: "Video", MediaType: "Video", Name: "Clean Trailer", OfficialRating: "G", Extra: sqlite.ItemExtra{ExtraType: "Trailer"}}
	trR := &sqlite.ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: movie.ID, Type: "Video", MediaType: "Video", Name: "R Trailer", OfficialRating: "R", Extra: sqlite.ItemExtra{ExtraType: "Trailer"}}
	sfOK := &sqlite.ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: movie.ID, Type: "Video", MediaType: "Video", Name: "Clean Extra", OfficialRating: "G", Extra: sqlite.ItemExtra{ExtraType: "Featurette"}}
	sfTag := &sqlite.ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: movie.ID, Type: "Video", MediaType: "Video", Name: "Tagged Extra", OfficialRating: "G", Extra: sqlite.ItemExtra{ExtraType: "Clip"}}
	tsOK := &sqlite.ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: movie.ID, Type: "Audio", MediaType: "Audio", Name: "Clean Theme Song", OfficialRating: "G", Extra: sqlite.ItemExtra{ExtraType: "ThemeSong"}}
	tsR := &sqlite.ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: movie.ID, Type: "Audio", MediaType: "Audio", Name: "R Theme Song", OfficialRating: "R", Extra: sqlite.ItemExtra{ExtraType: "ThemeSong"}}
	tvOK := &sqlite.ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: movie.ID, Type: "Video", MediaType: "Video", Name: "Clean Theme Video", OfficialRating: "G", Extra: sqlite.ItemExtra{ExtraType: "ThemeVideo"}}
	tvR := &sqlite.ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: movie.ID, Type: "Video", MediaType: "Video", Name: "R Theme Video", OfficialRating: "R", Extra: sqlite.ItemExtra{ExtraType: "ThemeVideo"}}
	for _, rec := range []*sqlite.ItemRecord{movie, trOK, trR, sfOK, sfTag, tsOK, tsR, tvOK, tvR} {
		_ = env.repo.UpsertItem(ctx, rec)
	}
	_ = env.repo.ReplaceItemTaxonomies(ctx, sfTag.ID, "tag", []string{"explicit"})

	// LocalTrailers
	recLT := doAs(kidAC, http.MethodGet, "/Items/"+movie.ID+"/LocalTrailers", "")
	var lt []dto.BaseItemDto
	_ = json.Unmarshal(recLT.Body.Bytes(), &lt)
	if recLT.Code != http.StatusOK || len(lt) != 1 || lt[0].Id != trOK.ID {
		t.Fatalf("LocalTrailers expected only trOK, got %d %+v", recLT.Code, lt)
	}
	// SpecialFeatures
	recSF := doAs(kidAC, http.MethodGet, "/Items/"+movie.ID+"/SpecialFeatures", "")
	var sf []dto.BaseItemDto
	_ = json.Unmarshal(recSF.Body.Bytes(), &sf)
	if recSF.Code != http.StatusOK || len(sf) != 1 || sf[0].Id != sfOK.ID {
		t.Fatalf("SpecialFeatures expected only sfOK, got %d %+v", recSF.Code, sf)
	}
	// ThemeSongs
	recTS := doAs(kidAC, http.MethodGet, "/Items/"+movie.ID+"/ThemeSongs", "")
	var tsResp struct {
		Items []dto.BaseItemDto `json:"Items"`
	}
	_ = json.Unmarshal(recTS.Body.Bytes(), &tsResp)
	if recTS.Code != http.StatusOK || len(tsResp.Items) != 1 || tsResp.Items[0].Id != tsOK.ID {
		t.Fatalf("ThemeSongs expected only tsOK, got %d %+v", recTS.Code, tsResp.Items)
	}
	// ThemeVideos
	recTV := doAs(kidAC, http.MethodGet, "/Items/"+movie.ID+"/ThemeVideos", "")
	var tvResp struct {
		Items []dto.BaseItemDto `json:"Items"`
	}
	_ = json.Unmarshal(recTV.Body.Bytes(), &tvResp)
	if recTV.Code != http.StatusOK || len(tvResp.Items) != 1 || tvResp.Items[0].Id != tvOK.ID {
		t.Fatalf("ThemeVideos expected only tvOK, got %d %+v", recTV.Code, tvResp.Items)
	}
	// ThemeMedia (all)
	recAll := doAs(kidAC, http.MethodGet, "/Items/"+movie.ID+"/ThemeMedia", "")
	var allResp struct {
		ThemeSongsResult  struct{ Items []dto.BaseItemDto } `json:"ThemeSongsResult"`
		ThemeVideosResult struct{ Items []dto.BaseItemDto } `json:"ThemeVideosResult"`
	}
	_ = json.Unmarshal(recAll.Body.Bytes(), &allResp)
	if recAll.Code != http.StatusOK ||
		len(allResp.ThemeSongsResult.Items) != 1 || allResp.ThemeSongsResult.Items[0].Id != tsOK.ID ||
		len(allResp.ThemeVideosResult.Items) != 1 || allResp.ThemeVideosResult.Items[0].Id != tvOK.ID {
		t.Fatalf("ThemeMedia expected only safe theme songs/videos, got %d %+v", recAll.Code, allResp)
	}

	// --- 2. [M-10] ActiveEncodings/Delete preserves sibling tab & honors admin DeviceId ---
	_, _ = mgr.RegisterSession(transcode.SessionParams{
		UserID: alice.ID, PlaySessionID: alice.ID + ":tab1", DeviceID: "dev-alice", ItemID: "i1", InputPath: "/m/1.mkv",
	})
	_, _ = mgr.RegisterSession(transcode.SessionParams{
		UserID: alice.ID, PlaySessionID: alice.ID + ":tab2", DeviceID: "dev-alice", ItemID: "i2", InputPath: "/m/2.mkv",
	})
	_, _ = mgr.RegisterSession(transcode.SessionParams{
		UserID: admin.ID, PlaySessionID: "adm-tab", DeviceID: "dev-admin", ItemID: "i3", InputPath: "/m/3.mkv",
	})
	_, _ = mgr.RegisterSession(transcode.SessionParams{
		UserID: bob.ID, PlaySessionID: bob.ID + ":bob-tab", DeviceID: "dev-bob", ItemID: "i4", InputPath: "/m/4.mkv",
	})
	hasSess := func(psid string) bool {
		for _, s := range mgr.ActiveSessions() {
			if s.PlaySessionID == psid {
				return true
			}
		}
		return false
	}
	_ = doAs(aliceAC, http.MethodPost, "/Videos/ActiveEncodings/Delete?PlaySessionId=tab1", "")
	if hasSess(alice.ID+":tab1") || !hasSess(alice.ID+":tab2") {
		t.Fatalf("PlaySessionId-only delete must stop tab1 and keep tab2 alive")
	}
	_ = doAs(adminAC, http.MethodPost, "/Videos/ActiveEncodings/Delete?DeviceId=dev-bob", "")
	if hasSess(bob.ID+":bob-tab") || !hasSess("adm-tab") {
		t.Fatalf("admin explicit DeviceId must stop dev-bob and keep dev-admin alive")
	}

	// --- 3. [M-15] Collection ProviderItems filters nested private Playlist for non-admin with EnableAllFolders=true ---
	alicePl, _ := env.repo.CreatePlaylist(ctx, "Alice Private Pl", "Video", alice.ID, []string{movie.ID})
	boxSet, _ := env.repo.CreateCollection(ctx, "Box With Nested Pl", "", []string{movie.ID, alicePl.ID})
	recPI := doAs(bobAC, http.MethodGet, "/Collections/"+boxSet.ID+"/ProviderItems", "")
	var piResp dto.QueryResult[sqlite.PlaylistItemDto]
	_ = json.Unmarshal(recPI.Body.Bytes(), &piResp)
	if recPI.Code != http.StatusOK || len(piResp.Items) != 1 || piResp.Items[0].Id != movie.ID {
		t.Fatalf("ProviderItems must filter nested private playlist for non-admin bob, got %d %+v", recPI.Code, piResp.Items)
	}

	// --- 4. [M-29] handleVideoAttachmentStream uses http.ServeContent (supports Range 206) ---
	mediaDir := lib.Paths[0]
	animePath := filepath.Join(mediaDir, "anime.mkv")
	_ = os.WriteFile(animePath, []byte("mkv"), 0o644)
	animeID := guid.New()
	animeMS := guid.New()
	_ = env.repo.UpsertItem(ctx, &sqlite.ItemRecord{ID: animeID, LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", MediaType: "Video", Name: "Anime", Path: animePath})
	_ = env.repo.ReplaceMediaSourcesAndStreams(ctx, animeID, []sqlite.MediaSourceRecord{{ID: animeMS, ItemID: animeID, Path: animePath}})
	fontDir := filepath.Join(env.cfg.SubtitleCacheDir(), "fonts", animeID+"_"+animeMS)
	_ = os.MkdirAll(fontDir, 0o755)
	_ = os.WriteFile(filepath.Join(fontDir, "TestFont.ttf"), []byte("0123456789ABCDEF"), 0o644)
	_ = os.WriteFile(filepath.Join(fontDir, ".extracted"), []byte("ok"), 0o644)
	reqRange := httptest.NewRequest(http.MethodGet, "/Videos/"+animeID+"/"+animeMS+"/Attachments/0", nil)
	reqRange.Header.Set("Range", "bytes=0-3")
	reqRange = reqRange.WithContext(middleware.WithAuthContext(reqRange.Context(), aliceAC))
	recRange := httptest.NewRecorder()
	router.ServeHTTP(recRange, reqRange)
	if recRange.Code != http.StatusPartialContent || recRange.Body.String() != "0123" {
		t.Fatalf("expected 206 Partial Content '0123' via http.ServeContent, got %d %q", recRange.Code, recRange.Body.String())
	}

	// --- 5. [L-05] Video/Audio HLS segment handlers return uniform 404 (no existence oracle) ---
	rMovie := &sqlite.ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", MediaType: "Video", Name: "Blocked R", OfficialRating: "R"}
	_ = env.repo.UpsertItem(ctx, rMovie)
	recVUnauth := doAs(kidAC, http.MethodGet, "/Videos/"+rMovie.ID+"/hls/p1/0.ts", "")
	recVMissing := doAs(kidAC, http.MethodGet, "/Videos/"+guid.New()+"/hls/p1/0.ts", "")
	if recVUnauth.Code != http.StatusNotFound || recVMissing.Code != http.StatusNotFound || recVUnauth.Body.String() != recVMissing.Body.String() {
		t.Fatalf("Video HLS segment oracle detected: unauth=%d/%q missing=%d/%q", recVUnauth.Code, recVUnauth.Body.String(), recVMissing.Code, recVMissing.Body.String())
	}
	recAUnauth := doAs(kidAC, http.MethodGet, "/Audio/"+rMovie.ID+"/hls1/p1/0.ts", "")
	recAMissing := doAs(kidAC, http.MethodGet, "/Audio/"+guid.New()+"/hls1/p1/0.ts", "")
	if recAUnauth.Code != http.StatusNotFound || recAMissing.Code != http.StatusNotFound || recAUnauth.Body.String() != recAMissing.Body.String() {
		t.Fatalf("Audio HLS segment oracle detected: unauth=%d/%q missing=%d/%q", recAUnauth.Code, recAUnauth.Body.String(), recAMissing.Code, recAMissing.Body.String())
	}

	// --- 6. [L-08] AddToPlaylistInfo validates target exists, is Playlist, and checks parental policy ---
	if r := doAs(aliceAC, http.MethodGet, "/Playlists/"+guid.New()+"/AddToPlaylistInfo?Ids="+movie.ID, ""); r.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for nonexistent playlist in AddToPlaylistInfo, got %d", r.Code)
	}
	if r := doAs(aliceAC, http.MethodGet, "/Playlists/"+boxSet.ID+"/AddToPlaylistInfo?Ids="+movie.ID, ""); r.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for non-Playlist (BoxSet) in AddToPlaylistInfo, got %d", r.Code)
	}
	kidPl, _ := env.repo.CreatePlaylist(ctx, "Kid Tagged Pl", "Video", kid.ID, []string{movie.ID})
	_ = env.repo.ReplaceItemTaxonomies(ctx, kidPl.ID, "tag", []string{"explicit"})
	if r := doAs(kidAC, http.MethodGet, "/Playlists/"+kidPl.ID+"/AddToPlaylistInfo?Ids="+movie.ID, ""); r.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for parental-blocked Playlist in AddToPlaylistInfo, got %d", r.Code)
	}

	// --- 7. [L-10] Concurrent handleUploadSubtitle assigns unique stream_index in transaction ---
	subB64 := base64.StdEncoding.EncodeToString([]byte("1\n00:00:01,000 --> 00:00:02,000\nHello\n"))
	var wg sync.WaitGroup
	langs := []string{"eng", "zho", "jpn", "fra", "deu"}
	for _, lang := range langs {
		wg.Add(1)
		go func(l string) {
			defer wg.Done()
			payload := fmt.Sprintf(`{"Language":"%s","Format":"srt","Data":"%s"}`, l, subB64)
			rec := doAs(adminAC, http.MethodPost, "/Videos/"+animeID+"/Subtitles?MediaSourceId="+animeMS, payload)
			if rec.Code != http.StatusNoContent {
				t.Errorf("concurrent upload subtitle(%s) got %d body=%s", l, rec.Code, rec.Body.String())
			}
		}(lang)
	}
	wg.Wait()
	sources, err := env.repo.GetMediaSourcesByItemID(ctx, animeID)
	if err != nil || len(sources) != 1 || len(sources[0].Streams) != len(langs) {
		t.Fatalf("expected %d subtitle streams after concurrent upload, got sources=%+v err=%v", len(langs), sources, err)
	}
	seenIdx := map[int]bool{}
	for _, st := range sources[0].Streams {
		if seenIdx[st.StreamIndex] {
			t.Fatalf("duplicate stream_index %d in concurrent subtitle uploads", st.StreamIndex)
		}
		seenIdx[st.StreamIndex] = true
	}

	// --- 8. [L-11] POST /Items/{Id}/RemoteSearch/Subtitles/{SubtitleId} requires admin ---
	if r := doAs(aliceAC, http.MethodPost, "/Items/"+movie.ID+"/RemoteSearch/Subtitles/sub123", ""); r.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin RemoteSearch/Subtitles download, got %d", r.Code)
	}
	if r := doAs(adminAC, http.MethodPost, "/Items/"+movie.ID+"/RemoteSearch/Subtitles/sub123", ""); r.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for admin RemoteSearch/Subtitles download, got %d", r.Code)
	}
}
