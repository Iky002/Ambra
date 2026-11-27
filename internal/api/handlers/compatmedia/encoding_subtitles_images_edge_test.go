package compatmedia

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image/color"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/imaging"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

type cmCtxKey string

const testCMUIDKey cmCtxKey = "cm_uid"

type cmFailReader struct{}

func (cmFailReader) Read(_ []byte) (int, error) {
	return 0, errors.New("read failed")
}

func TestCompatMediaCoverageBoostV0103(t *testing.T) {
	tmp := t.TempDir()
	cfg, err := config.Load(filepath.Join(tmp, "data"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	authSvc := auth.NewService(userRepo, cfg)

	admin, _ := authSvc.CreateUser(ctx, "admin", "admin123", true)
	alice, _ := authSvc.CreateUser(ctx, "alice", "alice123", false)
	bob, _ := authSvc.CreateUser(ctx, "bob", "bob123", false)

	safeDir := filepath.Join(tmp, "safe-lib")
	secretDir := filepath.Join(tmp, "secret-lib")
	_ = os.MkdirAll(safeDir, 0o755)
	_ = os.MkdirAll(secretDir, 0o755)

	libSafe, _ := repo.CreateLibrary(ctx, "SafeLib", "movies", []string{safeDir})
	libSecret, _ := repo.CreateLibrary(ctx, "SecretLib", "movies", []string{secretDir})

	ap := alice.Policy
	ap.EnableAllFolders = false
	ap.EnabledFolders = []string{libSafe.ID}
	ap.EnableContentDownloading = false
	maxPG := 5
	ap.MaxParentalRating = &maxPG
	_ = userRepo.UpdateUserPolicy(ctx, alice.ID, ap)
	authSvc.InvalidateUserCache(alice.ID)

	safeVideo := filepath.Join(safeDir, "safe.mkv")
	_ = os.WriteFile(safeVideo, []byte("VIDEO"), 0o600)
	noMSVideo := filepath.Join(safeDir, "noms.mkv")
	_ = os.WriteFile(noMSVideo, []byte("VIDEO2"), 0o600)
	outsideVideo := filepath.Join(tmp, "outside.mkv")
	_ = os.WriteFile(outsideVideo, []byte("OUTSIDE"), 0o600)

	safeMovie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libSafe.ID, ParentID: libSafe.ID,
		Type: "Movie", MediaType: "Video", Name: "Safe Movie", Path: safeVideo, OfficialRating: "G",
	}
	noMSMovie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libSafe.ID, ParentID: libSafe.ID,
		Type: "Movie", MediaType: "Video", Name: "No MS Movie", Path: noMSVideo, OfficialRating: "G",
	}
	noPathMovie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libSafe.ID, ParentID: libSafe.ID,
		Type: "Movie", MediaType: "Video", Name: "No Path", Path: "", OfficialRating: "G",
	}
	outsideMovie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libSafe.ID, ParentID: libSafe.ID,
		Type: "Movie", MediaType: "Video", Name: "Outside Path", Path: outsideVideo, OfficialRating: "G",
	}
	missingSubDirMovie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libSafe.ID, ParentID: libSafe.ID,
		Type: "Movie", MediaType: "Video", Name: "Readonly Subdir", Path: filepath.Join(safeDir, "ro-sub", "vid.mkv"), OfficialRating: "G",
	}
	roSubDir := filepath.Join(safeDir, "ro-sub")
	_ = os.MkdirAll(roSubDir, 0o755)
	_ = os.WriteFile(filepath.Join(roSubDir, "vid.mkv"), []byte("V"), 0o600)
	_ = os.Chmod(roSubDir, 0o555)
	t.Cleanup(func() { _ = os.Chmod(roSubDir, 0o755) })
	secretMovie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libSecret.ID, ParentID: libSecret.ID,
		Type: "Movie", MediaType: "Video", Name: "Secret Movie", Path: filepath.Join(secretDir, "secret.mkv"), OfficialRating: "R",
	}
	for _, it := range []*sqlite.ItemRecord{safeMovie, noMSMovie, noPathMovie, outsideMovie, missingSubDirMovie, secretMovie} {
		if err := repo.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	_ = repo.ReplaceMediaSourcesAndStreams(ctx, safeMovie.ID, []sqlite.MediaSourceRecord{{
		ID: guid.New(), ItemID: safeMovie.ID, Path: safeVideo, Container: "mkv",
	}})

	// Artist, MusicGenre, Audio items for handleInstantMixByQuery
	artist := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libSafe.ID, ParentID: libSafe.ID,
		Type: "MusicArtist", Name: "Daft Punk",
	}
	audio1 := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libSafe.ID, ParentID: artist.ID,
		Type: "Audio", MediaType: "Audio", Name: "One More Time", OfficialRating: "G",
		Extra: sqlite.ItemExtra{AlbumArtist: "Daft Punk", Artists: []string{"Daft Punk"}},
	}
	audio2 := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libSafe.ID, ParentID: libSafe.ID,
		Type: "Audio", MediaType: "Audio", Name: "Around The World", OfficialRating: "G",
		Extra: sqlite.ItemExtra{Artists: []string{"Daft Punk"}},
	}
	for _, it := range []*sqlite.ItemRecord{artist, audio1, audio2} {
		_ = repo.UpsertItem(ctx, it)
	}
	_, _ = store.Writer.ExecContext(ctx, `INSERT OR IGNORE INTO item_ancestors(ancestor_id, descendant_id, depth) VALUES(?, ?, 1)`, artist.ID, audio1.ID)
	_ = repo.ReplaceItemTaxonomies(ctx, audio1.ID, "Genre", []string{"Electronic"})
	_ = repo.ReplaceItemTaxonomies(ctx, audio2.ID, "Genre", []string{"Electronic"})

	var electronicTaxID string
	_ = store.Reader.QueryRowContext(ctx, `SELECT id FROM taxonomies WHERE LOWER(name) = 'electronic' LIMIT 1`).Scan(&electronicTaxID)

	// Channels and ChannelItems for handleChannelLatestItems
	ch1 := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libSafe.ID, ParentID: libSafe.ID, Type: "Channel", Name: "Ch1"}
	ch2 := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libSafe.ID, ParentID: libSafe.ID, Type: "Channel", Name: "Ch2"}
	t1 := time.Now().UTC().Add(-2 * time.Hour)
	t2 := time.Now().UTC().Add(-1 * time.Hour)
	ci1 := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libSafe.ID, ParentID: ch1.ID, Type: "ChannelVideoItem", MediaType: "Video", Name: "CI1", DateCreated: t1.Unix()}
	ci2 := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libSafe.ID, ParentID: ch2.ID, Type: "ChannelVideoItem", MediaType: "Video", Name: "CI2", DateCreated: t2.Unix()}
	ci3 := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libSafe.ID, ParentID: ch2.ID, Type: "ChannelVideoItem", MediaType: "Video", Name: "CI3", DateCreated: 0}
	for _, it := range []*sqlite.ItemRecord{ch1, ch2, ci1, ci2, ci3} {
		_ = repo.UpsertItem(ctx, it)
	}

	// BoxSet with 3 safe items + 1 secret item for handleCollectionProviderItems pagination
	safe2 := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libSafe.ID, ParentID: libSafe.ID, Type: "Movie", MediaType: "Video", Name: "Safe 2", OfficialRating: "G"}
	safe3 := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libSafe.ID, ParentID: libSafe.ID, Type: "Movie", MediaType: "Video", Name: "Safe 3", OfficialRating: "G"}
	_ = repo.UpsertItem(ctx, safe2)
	_ = repo.UpsertItem(ctx, safe3)
	boxSet, _ := repo.CreateCollection(ctx, "ProviderBox", "", []string{safeMovie.ID, safe2.ID, safe3.ID, secretMovie.ID})

	// Upcoming episodes for handleShowsUpcoming pagination
	future1 := time.Now().UTC().Add(24 * time.Hour).Unix()
	future2 := time.Now().UTC().Add(48 * time.Hour).Unix()
	up1 := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libSafe.ID, ParentID: libSafe.ID, Type: "Episode", MediaType: "Video", Name: "Up1", PremiereDate: &future1}
	up2 := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libSafe.ID, ParentID: libSafe.ID, Type: "Episode", MediaType: "Video", Name: "Up2", PremiereDate: &future2}
	_ = repo.UpsertItem(ctx, up1)
	_ = repo.UpsertItem(ctx, up2)

	pngBytes := makeValidPNG(t, 8, 8, color.RGBA{R: 50, G: 100, B: 200, A: 255})
	remoteSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/badbytes":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("not actually a png"))
		case "/ok.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pngBytes)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(remoteSrv.Close)

	h := New(repo, nil, imaging.New(repo), plugins.NewRegistry(), store, cfg)
	h.SetUserRepo(userRepo)
	h.SetHTTPClient(remoteSrv.Client())
	authMW := middleware.NewAuth(authSvc, nil)
	router := h.BuildRouter(AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
		UserIDFromCtx: func(c context.Context) string {
			if ac, ok := middleware.AuthFromContext(c); ok && ac != nil {
				return ac.UserID()
			}
			if v, ok := c.Value(testCMUIDKey).(string); ok {
				return v
			}
			return ""
		},
	})

	adminAuth, _ := authSvc.AuthenticateByName(ctx, "admin", "admin123", "", auth.ClientInfo{DeviceID: "d0"})
	aliceAuth, _ := authSvc.AuthenticateByName(ctx, "alice", "alice123", "", auth.ClientInfo{DeviceID: "d1"})

	call := func(tok, method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if tok != "" {
			req.Header.Set("X-Emby-Token", tok)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	// 1. handler.go RBAC & helper branches
	t.Run("handler_helpers_branches", func(t *testing.T) {
		reqBase := httptest.NewRequest(http.MethodGet, "/", nil)
		reqAdminUID := reqBase.WithContext(context.WithValue(ctx, testCMUIDKey, admin.ID))
		reqAliceUID := reqBase.WithContext(context.WithValue(ctx, testCMUIDKey, alice.ID))
		reqBobUID := reqBase.WithContext(context.WithValue(ctx, testCMUIDKey, bob.ID))

		// canAccessUserData
		if !h.canAccessUserData(reqBase, "") {
			t.Fatal("empty target should be allowed")
		}
		if !h.canAccessUserData(reqAdminUID, alice.ID) {
			t.Fatal("admin via UserIDFromCtx should access alice")
		}
		if h.canAccessUserData(reqBobUID, alice.ID) {
			t.Fatal("bob via UserIDFromCtx should be denied alice")
		}
		hBare := New(repo, nil, nil, nil, store, cfg)
		if !hBare.canAccessUserData(reqBase, alice.ID) {
			t.Fatal("unit test fallback should return true")
		}

		// canDownloadContent: admin in DB via non-admin AuthContext, userRepo==nil fallback, UserIDFromCtx admin & non-admin
		nonAdminACForAdmin := &middleware.AuthContext{User: &sqlite.UserRecord{ID: admin.ID}}
		if !h.canDownloadContent(reqBase.WithContext(middleware.WithAuthContext(ctx, nonAdminACForAdmin))) {
			t.Fatal("admin in DB should be allowed to download")
		}
		aliceFresh, _ := userRepo.GetUserByID(ctx, alice.ID)
		if hBare.canDownloadContent(reqBase.WithContext(middleware.WithAuthContext(ctx, &middleware.AuthContext{User: aliceFresh}))) {
			t.Fatal("expected userRepo==nil fallback to deny download for alice")
		}
		if !h.canDownloadContent(reqAdminUID) {
			t.Fatal("admin via UserIDFromCtx should be allowed to download")
		}
		if h.canDownloadContent(reqAliceUID) {
			t.Fatal("alice via UserIDFromCtx should be denied download")
		}

		// allowedLibrariesForUser & userParentalPolicyForRequest edge cases (fail-closed for unknown user)
		if got := h.allowedLibrariesForUser(ctx, ""); got != nil {
			t.Fatalf("expected nil for empty userID, got %v", got)
		}
		if got := h.allowedLibrariesForUser(ctx, guid.New()); got == nil || len(got) != 0 {
			t.Fatalf("expected non-nil empty slice (fail-closed) for unknown userID, got %#v", got)
		}
		if maxR, _ := h.userParentalPolicyForRequest(reqBase, alice.ID); maxR == nil || *maxR != maxPG {
			t.Fatalf("expected maxPG via fallbackUserID, got %v", maxR)
		}
		if maxR, _ := h.userParentalPolicyForRequest(reqBase, guid.New()); maxR == nil || *maxR != 0 {
			t.Fatalf("expected fail-closed maxRating=0 for unknown fallbackUserID, got %v", maxR)
		}
		if maxR, _ := h.userParentalPolicyForRequest(reqAdminUID, ""); maxR != nil {
			t.Fatalf("expected nil for admin via UserIDFromCtx, got %v", maxR)
		}

		// getConfigJSON corrupt JSON + closed DB
		_, _ = store.Writer.ExecContext(ctx, `INSERT OR REPLACE INTO server_config(key, value_json, updated_at) VALUES('cm_corrupt', '{bad', 1)`)
		var dst map[string]any
		if ok, err := h.getConfigJSON(ctx, "cm_corrupt", &dst); ok || err == nil {
			t.Fatalf("expected error on corrupt JSON, got (%v, %v)", ok, err)
		}
		cancelledCtx, cancel := context.WithCancel(ctx)
		cancel()
		if ok, err := h.getConfigJSON(cancelledCtx, "cm_corrupt", &dst); ok || err == nil {
			t.Fatalf("expected error on cancelled ctx, got (%v, %v)", ok, err)
		}

		// dto.SanitizeItemPaths nil & nested MediaStreams
		dto.SanitizeItemPaths(nil)
		it := &dto.BaseItemDto{
			Path: "/secret/a.mkv",
			MediaSources: []dto.MediaSourceInfo{{
				Path: "/secret/a.mkv",
				MediaStreams: []dto.MediaStream{{Path: "/secret/a.srt"}},
			}},
		}
		dto.SanitizeItemPaths(it)
		if it.Path != "" || it.MediaSources[0].Path != "" || it.MediaSources[0].MediaStreams[0].Path != "" {
			t.Fatalf("expected all paths sanitized: %+v", it)
		}
	})

	// 2. items.go: handleUploadSubtitle, handleDeleteSubtitle, handleSubtitleStream, isPathWithinLibraryRoots, extras/theme/mix 403/404 paths
	t.Run("subtitles_and_item_edge_cases", func(t *testing.T) {
		rawSub := "1\n00:00:01,000 --> 00:00:02,000\nHello\n"
		unpaddedB64 := base64.RawStdEncoding.EncodeToString([]byte(rawSub))

		// Direct call with empty Id -> 400
		reqEmptyID := httptest.NewRequest(http.MethodPost, "/Videos//Subtitles", strings.NewReader(`{}`))
		recEmptyID := httptest.NewRecorder()
		h.handleUploadSubtitle(recEmptyID, reqEmptyID)
		if recEmptyID.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for empty Id, got %d", recEmptyID.Code)
		}

		// Missing item -> 404
		if rec := call(adminAuth.AccessToken, http.MethodPost, "/Videos/"+guid.New()+"/Subtitles", `{}`); rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 missing item subtitle upload, got %d", rec.Code)
		}
		// Forbidden item (direct call as alice) -> 403
		reqSubForb := httptest.NewRequest(http.MethodPost, "/Videos/"+secretMovie.ID+"/Subtitles", strings.NewReader(`{}`))
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("Id", secretMovie.ID)
		aliceFresh, _ := userRepo.GetUserByID(ctx, alice.ID)
		reqSubForb = reqSubForb.WithContext(context.WithValue(middleware.WithAuthContext(ctx, &middleware.AuthContext{User: aliceFresh}), chi.RouteCtxKey, rctx))
		recSubForb := httptest.NewRecorder()
		h.handleUploadSubtitle(recSubForb, reqSubForb)
		if recSubForb.Code != http.StatusForbidden {
			t.Fatalf("expected 403 uploading subtitle to forbidden item, got %d", recSubForb.Code)
		}
		// Nil body -> 400
		reqNilBody := httptest.NewRequest(http.MethodPost, "/Videos/"+safeMovie.ID+"/Subtitles", nil)
		reqNilBody.Body = nil
		rctx2 := chi.NewRouteContext()
		rctx2.URLParams.Add("Id", safeMovie.ID)
		reqNilBody = reqNilBody.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx2))
		recNilBody := httptest.NewRecorder()
		h.handleUploadSubtitle(recNilBody, reqNilBody)
		if recNilBody.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for nil body, got %d", recNilBody.Code)
		}

		// Invalid JSON, empty Data, invalid base64, no media path, outside library roots, nonexistent subdir
		for _, tc := range []struct {
			id   string
			body string
			want int
		}{
			{safeMovie.ID, `{bad-json`, http.StatusBadRequest},
			{safeMovie.ID, `{"Format":"srt","Data":""}`, http.StatusBadRequest},
			{safeMovie.ID, `{"Format":"srt","Data":"!!!not-base64!!!"}`, http.StatusBadRequest},
			{noPathMovie.ID, `{"Format":"srt","Language":"en","Data":"` + unpaddedB64 + `"}`, http.StatusBadRequest},
			{outsideMovie.ID, `{"Format":"srt","Language":"en","Data":"` + unpaddedB64 + `"}`, http.StatusForbidden},
			{missingSubDirMovie.ID, `{"Format":"srt","Language":"en","Data":"` + unpaddedB64 + `"}`, http.StatusInternalServerError},
			// No item_media_sources row -> mediaSourceID falls back to itemID and FK insert returns 500
			{noMSMovie.ID, `{"Format":"srt","Language":"en","Data":"` + unpaddedB64 + `"}`, http.StatusInternalServerError},
			// Success with RawStdEncoding and valid media source + second upload overwrites existing stream
			{safeMovie.ID, `{"Format":"srt","Language":"en","Data":"` + unpaddedB64 + `"}`, http.StatusNoContent},
			{safeMovie.ID, `{"Format":"srt","Language":"en","Data":"` + unpaddedB64 + `"}`, http.StatusNoContent},
		} {
			rec := call(adminAuth.AccessToken, http.MethodPost, "/Videos/"+tc.id+"/Subtitles", tc.body)
			if rec.Code != tc.want {
				t.Fatalf("upload sub id=%s body=%s: want %d, got %d (%s)", tc.id, tc.body, tc.want, rec.Code, rec.Body.String())
			}
		}

		// handleDeleteSubtitle: forbidden item (403), nil store (404)
		reqDelForb := httptest.NewRequest(http.MethodDelete, "/Videos/"+secretMovie.ID+"/Subtitles/0", nil)
		rctxD := chi.NewRouteContext()
		rctxD.URLParams.Add("Id", secretMovie.ID)
		rctxD.URLParams.Add("Index", "0")
		reqDelForb = reqDelForb.WithContext(context.WithValue(middleware.WithAuthContext(ctx, &middleware.AuthContext{User: aliceFresh}), chi.RouteCtxKey, rctxD))
		recDelForb := httptest.NewRecorder()
		h.handleDeleteSubtitle(recDelForb, reqDelForb)
		if recDelForb.Code != http.StatusForbidden {
			t.Fatalf("expected 403 delete subtitle on forbidden item, got %d", recDelForb.Code)
		}

		hNoStore := New(repo, nil, nil, nil, nil, cfg)
		reqDelNS := httptest.NewRequest(http.MethodDelete, "/Videos/"+safeMovie.ID+"/Subtitles/0", nil)
		rctxDNS := chi.NewRouteContext()
		rctxDNS.URLParams.Add("Id", safeMovie.ID)
		rctxDNS.URLParams.Add("Index", "0")
		reqDelNS = reqDelNS.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctxDNS))
		recDelNS := httptest.NewRecorder()
		hNoStore.handleDeleteSubtitle(recDelNS, reqDelNS)
		if recDelNS.Code != http.StatusNotFound {
			t.Fatalf("expected 404 delete subtitle with nil store, got %d", recDelNS.Code)
		}

		// handleSubtitleStream: nil store (404), external stream with empty path (404), rowMSID fallback
		reqSubNS := httptest.NewRequest(http.MethodGet, "/Videos/"+safeMovie.ID+"/ms/Subtitles/0/Stream.vtt", nil)
		rctxSNS := chi.NewRouteContext()
		rctxSNS.URLParams.Add("Id", safeMovie.ID)
		rctxSNS.URLParams.Add("Index", "0")
		rctxSNS.URLParams.Add("StreamFile", "Stream.vtt")
		reqSubNS = reqSubNS.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctxSNS))
		recSubNS := httptest.NewRecorder()
		hNoStore.handleSubtitleStream(recSubNS, reqSubNS)
		if recSubNS.Code != http.StatusNotFound {
			t.Fatalf("expected 404 subtitle stream with nil store, got %d", recSubNS.Code)
		}
		// Insert external subtitle with empty path to hit L1331-1333 & L1356-1359
		_, _ = store.Writer.ExecContext(ctx, `
			INSERT INTO item_media_streams(media_source_id, item_id, stream_index, stream_type, codec, is_external, path)
			VALUES('ms-empty-path', ?, 77, 'Subtitle', 'srt', 1, '')`, safeMovie.ID)
		reqSubEmpty := httptest.NewRequest(http.MethodGet, "/Videos/"+safeMovie.ID+"/Subtitles/77/Stream.vtt", nil)
		rctxSE := chi.NewRouteContext()
		rctxSE.URLParams.Add("Id", safeMovie.ID)
		rctxSE.URLParams.Add("Index", "77")
		rctxSE.URLParams.Add("StreamFile", "Stream.vtt")
		reqSubEmpty = reqSubEmpty.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctxSE))
		recSubEmpty := httptest.NewRecorder()
		h.handleSubtitleStream(recSubEmpty, reqSubEmpty)
		if recSubEmpty.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for empty subPath, got %d", recSubEmpty.Code)
		}

		// isPathWithinLibraryRoots edge cases: nil repo, empty path, cancelled ctx
		hNilRepo := New(nil, nil, nil, nil, nil, cfg)
		if hNilRepo.isPathWithinLibraryRoots(ctx, safeVideo) {
			t.Fatal("nil repo should return false")
		}
		if h.isPathWithinLibraryRoots(ctx, "") {
			t.Fatal("empty target should return false")
		}
		cancelledCtx, cancel := context.WithCancel(ctx)
		cancel()
		if h.isPathWithinLibraryRoots(cancelledCtx, safeVideo) {
			t.Fatal("cancelled ctx should return false")
		}

		// Forbidden & NotFound paths on LocalTrailers, SpecialFeatures, ThemeSongs, ThemeMedia, InstantMix, DeleteInfo
		for _, ep := range []string{
			"/Items/" + secretMovie.ID + "/LocalTrailers",
			"/Items/" + secretMovie.ID + "/SpecialFeatures",
			"/Items/" + secretMovie.ID + "/ThemeSongs",
			"/Items/" + secretMovie.ID + "/ThemeMedia",
			"/Items/" + secretMovie.ID + "/InstantMix",
			"/Items/" + secretMovie.ID + "/InstantMix?UserId=" + bob.ID,
			"/Items/" + safeMovie.ID + "/LocalTrailers?UserId=" + bob.ID,
			"/Items/" + safeMovie.ID + "/SpecialFeatures?UserId=" + bob.ID,
			"/Items/" + safeMovie.ID + "/ThemeSongs?UserId=" + bob.ID,
			"/Items/" + safeMovie.ID + "/ThemeMedia?UserId=" + bob.ID,
		} {
			if rec := call(aliceAuth.AccessToken, http.MethodGet, ep, ""); rec.Code != http.StatusForbidden {
				t.Fatalf("expected 403 for alice on %s, got %d", ep, rec.Code)
			}
		}
		if rec := call(adminAuth.AccessToken, http.MethodGet, "/Items/"+guid.New()+"/DeleteInfo", ""); rec.Code != http.StatusNotFound && rec.Code != http.StatusOK {
			t.Fatalf("unexpected DeleteInfo code: %d", rec.Code)
		}
		// Non-admin DeleteInfo -> 403
		if rec := call(aliceAuth.AccessToken, http.MethodGet, "/Items/"+safeMovie.ID+"/DeleteInfo", ""); rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 non-admin DeleteInfo, got %d", rec.Code)
		}
		// handleUpdateItem: empty ItemId -> 400, explicit SortName -> 204
		reqUpdEmpty := httptest.NewRequest(http.MethodPost, "/Items/", strings.NewReader(`{}`))
		recUpdEmpty := httptest.NewRecorder()
		h.handleUpdateItem(recUpdEmpty, reqUpdEmpty)
		if recUpdEmpty.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 empty ItemId on handleUpdateItem, got %d", recUpdEmpty.Code)
		}
		if rec := call(adminAuth.AccessToken, http.MethodPost, "/Items/"+safeMovie.ID, `{"SortName":"custom sort"}`); rec.Code != http.StatusNoContent {
			t.Fatalf("expected 204 handleUpdateItem SortName, got %d", rec.Code)
		}
		// handleMetadataReset with comma-separated ItemIds string & duplicate ID
		if rec := call(adminAuth.AccessToken, http.MethodPost, "/Items/Metadata/Reset", `{"ItemIds":"`+safeMovie.ID+`,`+safeMovie.ID+`"}`); rec.Code != http.StatusNoContent {
			t.Fatalf("expected 204 handleMetadataReset string, got %d", rec.Code)
		}
		// handleItemVisibility missing/not-found & cross-user
		if rec := call(adminAuth.AccessToken, http.MethodPost, "/Items/"+guid.New()+"/MakePublic", ""); rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 MakePublic missing, got %d", rec.Code)
		}
		// handleItemFile forbidden item -> 403
		if rec := call(aliceAuth.AccessToken, http.MethodGet, "/Items/"+secretMovie.ID+"/File", ""); rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 ItemFile forbidden, got %d", rec.Code)
		}
		// handleSimilar forbidden user -> 403
		if rec := call(aliceAuth.AccessToken, http.MethodGet, "/Items/"+safeMovie.ID+"/Similar?UserId="+bob.ID, ""); rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Similar cross-user, got %d", rec.Code)
		}
	})

	// 3. encoding.go: handleInstantMixByQuery (Id forbidden, ArtistIds + Name, MusicGenreIds + Name),
	// handleUpdateVirtualFolderPath (all switch branches + 404), handleChannelLatestItems (single + multi + pagination),
	// handleCollectionProviderItems (pagination), handleShowsUpcoming (pagination), isTaxonomyAccessible / isPersonAccessible
	t.Run("encoding_instantmix_vfpath_channels_collections", func(t *testing.T) {
		// InstantMix forbidden seed Id -> 403
		if rec := call(aliceAuth.AccessToken, http.MethodGet, "/Artists/InstantMix?Id="+secretMovie.ID, ""); rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 InstantMix forbidden seed, got %d", rec.Code)
		}
		// Artist InstantMix with ArtistIds (valid + forbidden + missing) & Limit=1
		recArtMix := call(aliceAuth.AccessToken, http.MethodGet, "/Artists/InstantMix?ArtistIds="+artist.ID+","+secretMovie.ID+","+guid.New()+"&Limit=1", "")
		if recArtMix.Code != http.StatusOK {
			t.Fatalf("Artists/InstantMix got %d: %s", recArtMix.Code, recArtMix.Body.String())
		}
		// Artist InstantMix by Name matching Artists array
		recArtName := call(aliceAuth.AccessToken, http.MethodGet, "/Artists/InstantMix?Name=Daft%20Punk&Limit=1", "")
		if recArtName.Code != http.StatusOK {
			t.Fatalf("Artists/InstantMix?Name got %d: %s", recArtName.Code, recArtName.Body.String())
		}
		// MusicGenres InstantMix by MusicGenreIds & Limit=1
		recGenMix := call(aliceAuth.AccessToken, http.MethodGet, "/MusicGenres/InstantMix?MusicGenreIds="+electronicTaxID+"&Limit=1", "")
		if recGenMix.Code != http.StatusOK {
			t.Fatalf("MusicGenres/InstantMix got %d: %s", recGenMix.Code, recGenMix.Body.String())
		}
		// MusicGenres InstantMix by Name (unmatched IDs -> falls back to it.Genres scan)
		recGenName := call(aliceAuth.AccessToken, http.MethodGet, "/MusicGenres/Electronic/InstantMix?Limit=1", "")
		if recGenName.Code != http.StatusOK {
			t.Fatalf("MusicGenres/Electronic/InstantMix got %d: %s", recGenName.Code, recGenName.Body.String())
		}

		// handleUpdateVirtualFolderPath: 404, body.Paths, oldPath not found -> append, newPath single vs multi, default no-op
		if rec := call(adminAuth.AccessToken, http.MethodPost, "/Library/VirtualFolders/Paths/Update", `{"Id":"`+guid.New()+`","Paths":["/a"]}`); rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 updating nonexistent VF path, got %d", rec.Code)
		}
		vfLib, _ := repo.CreateLibrary(ctx, "VFPathTest", "movies", []string{"/p1"})
		for _, body := range []string{
			`{"Id":"` + vfLib.ID + `"}`,                                                // default no-op
			`{"Id":"` + vfLib.ID + `","NewPath":"/p2"}`,                                // len==1 replace
			`{"Id":"` + vfLib.ID + `","Paths":["/pA","/pB","/pA"]}`,                    // body.Paths dedup
			`{"Id":"` + vfLib.ID + `","OldPath":"/not-present","NewPath":"/pC"}`,       // oldPath missing -> append
			`{"Id":"` + vfLib.ID + `","NewPath":"/pA"}`,                                // len>1 existing -> skip dup
			`{"Id":"` + vfLib.ID + `","NewPath":"/pD"}`,                                // len>1 new -> append
		} {
			if rec := call(adminAuth.AccessToken, http.MethodPost, "/Library/VirtualFolders/Paths/Update", body); rec.Code != http.StatusNoContent {
				t.Fatalf("UpdateVirtualFolderPath %s got %d: %s", body, rec.Code, rec.Body.String())
			}
		}

		// handleChannelLatestItems: single channel + multi channel with pagination
		for _, qs := range []string{
			"ChannelIds=" + ch1.ID,
			"ChannelIds=" + ch1.ID + "," + ch2.ID + "&StartIndex=1&Limit=1",
			"ChannelIds=" + ch1.ID + "," + ch2.ID + "&StartIndex=99",
		} {
			if rec := call(aliceAuth.AccessToken, http.MethodGet, "/Channels/Items/Latest?"+qs, ""); rec.Code != http.StatusOK {
				t.Fatalf("Channels/Items/Latest?%s got %d: %s", qs, rec.Code, rec.Body.String())
			}
		}

		// handleCollectionProviderItems pagination for filtered non-admin
		for _, qs := range []string{"StartIndex=1&Limit=1", "StartIndex=99"} {
			if rec := call(aliceAuth.AccessToken, http.MethodGet, "/Collections/"+boxSet.ID+"/ProviderItems?"+qs, ""); rec.Code != http.StatusOK {
				t.Fatalf("Collections/%s/ProviderItems?%s got %d: %s", boxSet.ID, qs, rec.Code, rec.Body.String())
			}
		}

		// handleShowsUpcoming pagination
		for _, qs := range []string{"StartIndex=1&Limit=1", "StartIndex=99"} {
			if rec := call(aliceAuth.AccessToken, http.MethodGet, "/Shows/Upcoming?"+qs, ""); rec.Code != http.StatusOK {
				t.Fatalf("Shows/Upcoming?%s got %d: %s", qs, rec.Code, rec.Body.String())
			}
		}

		// isTaxonomyAccessible & isPersonAccessible nil store/repo guards
		reqBase := httptest.NewRequest(http.MethodGet, "/", nil)
		hNilAll := New(nil, nil, nil, nil, nil, cfg)
		if hNilAll.isTaxonomyAccessible(reqBase, electronicTaxID, []string{libSafe.ID}, nil, nil) {
			t.Fatal("expected false when store is nil")
		}
		if hNilAll.isPersonAccessible(reqBase, guid.New(), []string{libSafe.ID}, nil, nil) {
			t.Fatal("expected false when store is nil")
		}
		hStoreOnly := New(nil, nil, nil, nil, store, cfg)
		if hStoreOnly.isTaxonomyAccessible(reqBase, electronicTaxID, []string{libSafe.ID}, nil, nil) {
			t.Fatal("expected false when repo is nil")
		}
		if hStoreOnly.isPersonAccessible(reqBase, guid.New(), []string{libSafe.ID}, nil, nil) {
			t.Fatal("expected false when repo is nil")
		}

		// handleSetEncodingConfig / handleSetCodecParameters null/bad JSON normalize to {} and return 204
		if rec := call(adminAuth.AccessToken, http.MethodPost, "/Encoding/FullToneMapOptions", `null`); rec.Code != http.StatusNoContent {
			t.Fatalf("expected 204 null encoding JSON, got %d", rec.Code)
		}
		if rec := call(adminAuth.AccessToken, http.MethodPost, "/Encoding/CodecParameters", `null`); rec.Code != http.StatusNoContent {
			t.Fatalf("expected 204 null codec JSON, got %d", rec.Code)
		}
		// Cross-user UserId forbidden on taxonomy/person/media folders/artists/upcoming
		for _, ep := range []string{
			"/Library/SelectableMediaFolders?UserId=" + bob.ID,
			"/Library/MediaFolders?UserId=" + bob.ID,
			"/Library/VirtualFolders/Query?UserId=" + bob.ID,
			"/Persons/Safe%20Actor?UserId=" + bob.ID,
			"/Genres/Electronic?UserId=" + bob.ID,
		} {
			if rec := call(aliceAuth.AccessToken, http.MethodGet, ep, ""); rec.Code != http.StatusForbidden {
				t.Fatalf("expected 403 cross-user on %s, got %d", ep, rec.Code)
			}
		}
	})

	// 4. images.go: downloadAndSaveItemImage, handleUploadItemImage, handleSetImageURL, validatePublicHTTPURL, resolveItemMediaPath, tryExtractAndServeChapterImage
	t.Run("images_error_and_edge_cases", func(t *testing.T) {
		// downloadAndSaveItemImage edge cases
		if err := h.downloadAndSaveItemImage(ctx, safeMovie.ID, "InvalidType", 0, remoteSrv.URL+"/ok.png"); err == nil {
			t.Fatal("expected error for unsupported image type")
		}
		if err := h.downloadAndSaveItemImage(ctx, safeMovie.ID, "Primary", 0, "ftp://example.com/a.png"); err == nil {
			t.Fatal("expected error for non-http URL")
		}
		if err := h.downloadAndSaveItemImage(ctx, safeMovie.ID, "Primary", 0, remoteSrv.URL+"/badbytes"); err == nil {
			t.Fatal("expected error for non-raster bytes")
		}
		// TempDir fallback when h.cfg == nil
		hNoCfg := New(repo, nil, imaging.New(repo), plugins.NewRegistry(), store, nil)
		hNoCfg.SetHTTPClient(remoteSrv.Client())
		if err := hNoCfg.downloadAndSaveItemImage(ctx, safeMovie.ID, "Thumb", 0, remoteSrv.URL+"/ok.png"); err != nil {
			t.Fatalf("expected downloadAndSaveItemImage with nil cfg to succeed: %v", err)
		}

		// handleUploadItemImage: invalid index, failing body reader, nil cfg fallback
		if rec := call(adminAuth.AccessToken, http.MethodPost, "/Items/"+safeMovie.ID+"/Images/Primary/-1", string(pngBytes)); rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for negative image index, got %d", rec.Code)
		}
		reqBadRead := httptest.NewRequest(http.MethodPost, "/Items/"+safeMovie.ID+"/Images/Primary", cmFailReader{})
		rctxImg := chi.NewRouteContext()
		rctxImg.URLParams.Add("Id", safeMovie.ID)
		rctxImg.URLParams.Add("Type", "Primary")
		reqBadRead = reqBadRead.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctxImg))
		recBadRead := httptest.NewRecorder()
		h.handleUploadItemImage(recBadRead, reqBadRead)
		if recBadRead.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 on body read error, got %d", recBadRead.Code)
		}
		reqNoCfgImg := httptest.NewRequest(http.MethodPost, "/Items/"+safeMovie.ID+"/Images/Banner", bytes.NewReader(pngBytes))
		rctxNC := chi.NewRouteContext()
		rctxNC.URLParams.Add("Id", safeMovie.ID)
		rctxNC.URLParams.Add("Type", "Banner")
		reqNoCfgImg = reqNoCfgImg.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctxNC))
		recNoCfgImg := httptest.NewRecorder()
		hNoCfg.handleUploadItemImage(recNoCfgImg, reqNoCfgImg)
		if recNoCfgImg.Code != http.StatusNoContent {
			t.Fatalf("expected 204 upload with nil cfg, got %d (%s)", recNoCfgImg.Code, recNoCfgImg.Body.String())
		}

		// handleSetImageURL: invalid index (-1) -> 400; empty Type defaults to Primary
		if rec := call(adminAuth.AccessToken, http.MethodPost, "/Items/"+safeMovie.ID+"/Images/Primary/-1/Url?Url="+remoteSrv.URL+"/ok.png", ""); rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 SetImageURL negative index, got %d", rec.Code)
		}
		reqEmptyType := httptest.NewRequest(http.MethodPost, "/Items/"+safeMovie.ID+"/Images//Url?Url="+remoteSrv.URL+"/ok.png", nil)
		rctxET := chi.NewRouteContext()
		rctxET.URLParams.Add("Id", safeMovie.ID)
		reqEmptyType = reqEmptyType.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctxET))
		recET := httptest.NewRecorder()
		h.handleSetImageURL(recET, reqEmptyType)
		if recET.Code != http.StatusNoContent {
			t.Fatalf("expected 204 SetImageURL with empty Type default Primary, got %d (%s)", recET.Code, recET.Body.String())
		}

		// validatePublicHTTPURL public IP ok & empty hostname err
		if err := validatePublicHTTPURL("https://93.184.216.34/a.png", false); err != nil {
			t.Fatalf("expected public IP URL to be valid, got %v", err)
		}
		if err := validatePublicHTTPURL("http://:80/a.png", false); err == nil {
			t.Fatal("expected error for empty hostname")
		}

		// resolveItemMediaPath edge case
		if _, err := h.resolveItemMediaPath(ctx, noPathMovie.ID, noPathMovie); err == nil {
			t.Fatal("expected ErrNotFound for empty item path")
		}

		// Closed store triggers BeginTx 500 in handleUpdateImageIndex & GetItemRecordByID 500 in handleSetImageURL
		closedStore, err := sqlite.Open(filepath.Join(tmp, "closed_cm.db"))
		if err != nil {
			t.Fatal(err)
		}
		closedRepo := sqlite.NewLibraryRepo(closedStore)
		_ = closedStore.Close()
		hClosed := New(closedRepo, nil, nil, nil, closedStore, cfg)
		hClosed.SetHTTPClient(remoteSrv.Client())

		reqIdxErr := httptest.NewRequest(http.MethodPost, "/Items/"+safeMovie.ID+"/Images/Backdrop/0/Index?newIndex=1", nil)
		rctxIE := chi.NewRouteContext()
		rctxIE.URLParams.Add("Id", safeMovie.ID)
		rctxIE.URLParams.Add("Type", "Backdrop")
		rctxIE.URLParams.Add("Index", "0")
		reqIdxErr = reqIdxErr.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctxIE))
		recIdxErr := httptest.NewRecorder()
		hClosed.handleUpdateImageIndex(recIdxErr, reqIdxErr)
		if recIdxErr.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 for handleUpdateImageIndex on closed store, got %d", recIdxErr.Code)
		}

		reqSetErr := httptest.NewRequest(http.MethodPost, "/Items/"+safeMovie.ID+"/Images/Primary/Url?Url="+remoteSrv.URL+"/ok.png", nil)
		reqSetErr = reqSetErr.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctxET))
		recSetErr := httptest.NewRecorder()
		hClosed.handleSetImageURL(recSetErr, reqSetErr)
		if recSetErr.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 for handleSetImageURL on closed store, got %d", recSetErr.Code)
		}

		// Closed DB 500 error branches across encoding & item handlers
		closedRouter := hClosed.BuildRouter(AuthFuncs{})
		for _, tc := range []struct {
			method string
			path   string
			body   string
		}{
			{http.MethodGet, "/Encoding/FullToneMapOptions", ""},
			{http.MethodPost, "/Encoding/FullToneMapOptions", `{"a":1}`},
			{http.MethodPost, "/Encoding/CodecParameters", `{"a":1}`},
			{http.MethodGet, "/Library/PhysicalPaths", ""},
			{http.MethodGet, "/Library/SelectableMediaFolders", ""},
			{http.MethodGet, "/Library/MediaFolders", ""},
			{http.MethodGet, "/Library/VirtualFolders/Query", ""},
			{http.MethodPost, "/Library/VirtualFolders/Paths", `{"Id":"` + libSafe.ID + `","Path":"/x"}`},
			{http.MethodPost, "/Library/VirtualFolders/Paths/Update", `{"Id":"` + libSafe.ID + `","Paths":["/x"]}`},
			{http.MethodDelete, "/Library/VirtualFolders/Paths?id=" + libSafe.ID + "&path=/x", ""},
			{http.MethodPost, "/Library/VirtualFolders/LibraryOptions", `{"Id":"` + libSafe.ID + `","LibraryOptions":{"a":1}}`},
			{http.MethodGet, "/Artists", ""},
			{http.MethodGet, "/Artists/Prefixes", ""},
			{http.MethodGet, "/Artists/InstantMix?Id=" + safeMovie.ID, ""},
			{http.MethodGet, "/Artists/InstantMix?Name=Daft%20Punk", ""},
			{http.MethodGet, "/MusicGenres/InstantMix?Name=Electronic", ""},
			{http.MethodGet, "/Artists/InstantMix", ""},
			{http.MethodGet, "/Channels/Items/Latest?ChannelIds=" + ch1.ID, ""},
			{http.MethodGet, "/Channels/Items/Latest?ChannelIds=" + ch1.ID + "," + ch2.ID, ""},
			{http.MethodGet, "/Collections/" + boxSet.ID + "/ProviderItems", ""},
			{http.MethodPost, "/Collections/" + boxSet.ID + "/Items/Delete?Ids=" + safeMovie.ID, ""},
			{http.MethodGet, "/Shows/Upcoming", ""},
			{http.MethodGet, "/AudioBooks/NextUp", ""},
			{http.MethodGet, "/Persons/Daft%20Punk", ""},
			{http.MethodPost, "/Items/" + safeMovie.ID, `{"Name":"X"}`},
			{http.MethodDelete, "/Items/" + safeMovie.ID, ""},
			{http.MethodPost, "/Videos/" + safeMovie.ID + "/Subtitles", `{"Format":"srt","Data":"QQ=="}`},
			{http.MethodDelete, "/Videos/" + safeMovie.ID + "/Subtitles/0", ""},
			{http.MethodGet, "/Items/" + safeMovie.ID + "/LocalTrailers", ""},
			{http.MethodGet, "/Items/" + safeMovie.ID + "/SpecialFeatures", ""},
			{http.MethodGet, "/Items/" + safeMovie.ID + "/ThemeSongs", ""},
			{http.MethodGet, "/Items/" + safeMovie.ID + "/ThemeMedia", ""},
			{http.MethodGet, "/Items/" + safeMovie.ID + "/InstantMix", ""},
			{http.MethodGet, "/Items/" + safeMovie.ID + "/Similar", ""},
			{http.MethodGet, "/Items/" + safeMovie.ID + "/Images", ""},
		} {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			closedRouter.ServeHTTP(rec, req)
			if rec.Code != http.StatusInternalServerError && rec.Code != http.StatusForbidden {
				t.Fatalf("closed DB %s %s: want 500 or 403 (fail-closed), got %d (%s)", tc.method, tc.path, rec.Code, rec.Body.String())
			}
		}
	})
}
