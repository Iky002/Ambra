package compatmedia

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestCompatMediaV0101SecurityAndParity(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "cm_v0101.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)

	authSvc := auth.NewService(userRepo, cfg)
	_, _ = authSvc.CreateUser(ctx, "admin", "admin123", true)
	alice, _ := authSvc.CreateUser(ctx, "alice", "alice123", false)
	bob, _ := authSvc.CreateUser(ctx, "bob", "bob123", false)

	dirA := filepath.Join(tmp, "a")
	dirB := filepath.Join(tmp, "b")
	_ = os.MkdirAll(dirA, 0o755)
	_ = os.MkdirAll(dirB, 0o755)

	libA, _ := repo.CreateLibrary(ctx, "LibA", "movies", []string{dirA})
	libB, _ := repo.CreateLibrary(ctx, "LibB", "movies", []string{dirB})

	maxPG := 5
	alicePol := alice.Policy
	alicePol.EnableAllFolders = false
	alicePol.EnabledFolders = []string{libA.ID}
	alicePol.MaxParentalRating = &maxPG
	alicePol.BlockedTags = []string{"nsfw"}
	alicePol.EnableContentDownloading = false
	if err := userRepo.UpdateUserPolicy(ctx, alice.ID, alicePol); err != nil {
		t.Fatal(err)
	}

	safeVideoPath := filepath.Join(dirA, "safe.mkv")
	strmPath := filepath.Join(dirA, "secret.strm")
	_ = os.WriteFile(safeVideoPath, []byte("video-bytes"), 0o644)
	_ = os.WriteFile(strmPath, []byte("https://user:pass@upstream.example.com/stream.m3u8\n"), 0o644)

	movieSafe := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID,
		Type: "Movie", MediaType: "Video", Name: "Safe Movie", SortName: "safe movie",
		OfficialRating: "PG", Path: safeVideoPath,
	}
	movieStrm := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID,
		Type: "Movie", MediaType: "Video", Name: "Strm Movie", SortName: "strm movie",
		OfficialRating: "PG", Path: strmPath, IsStrm: true,
	}
	movieSecret := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libB.ID, ParentID: libB.ID,
		Type: "Movie", MediaType: "Video", Name: "Secret Movie", SortName: "secret movie",
		OfficialRating: "G", Path: filepath.Join(dirB, "secret.mkv"),
	}
	artistA := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID,
		Type: "MusicArtist", Name: "Alpha Artist", SortName: "alpha artist", OfficialRating: "G",
	}
	artistSecret := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libB.ID, ParentID: libB.ID,
		Type: "MusicArtist", Name: "Zebra Secret", SortName: "zebra secret", OfficialRating: "G",
	}
	songRock := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: artistA.ID,
		Type: "Audio", MediaType: "Audio", Name: "Rock Song", SortName: "rock song",
		OfficialRating: "G",
		Extra:          sqlite.ItemExtra{Artists: []string{"Alpha Artist"}, AlbumArtist: "Alpha Artist"},
	}
	songJazz := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID,
		Type: "Audio", MediaType: "Audio", Name: "Jazz Song", SortName: "jazz song",
		OfficialRating: "G",
		Extra:          sqlite.ItemExtra{Artists: []string{"Other Artist"}, AlbumArtist: "Other Artist"},
	}
	for _, it := range []*sqlite.ItemRecord{movieSafe, movieStrm, movieSecret, artistA, artistSecret, songRock, songJazz} {
		_ = repo.UpsertItem(ctx, it)
	}
	_ = repo.ReplaceItemTaxonomies(ctx, movieSafe.ID, "genre", []string{"Family"})
	_ = repo.ReplaceItemTaxonomies(ctx, movieSecret.ID, "genre", []string{"SecretGenre"})
	_ = repo.ReplaceItemTaxonomies(ctx, songRock.ID, "genre", []string{"Rock"})
	_ = repo.ReplaceItemTaxonomies(ctx, songJazz.ID, "genre", []string{"Jazz"})

	box, _ := repo.CreateCollection(ctx, "Box", "", []string{movieSafe.ID})
	bobPl, _ := repo.CreatePlaylist(ctx, "BobPrivate", "Video", bob.ID, []string{movieSafe.ID})

	// Seed two external subtitles on movieSafe with same index=10 but different MediaSourceIds
	subPath1 := filepath.Join(dirA, "safe.en.srt")
	subPath2 := filepath.Join(dirA, "safe.zh.srt")
	_ = os.WriteFile(subPath1, []byte("1\n00:00:00,000 --> 00:00:01,000\nEN\n"), 0o644)
	_ = os.WriteFile(subPath2, []byte("1\n00:00:00,000 --> 00:00:01,000\nZH\n"), 0o644)
	ms1 := guid.New()
	ms2 := guid.New()
	for _, row := range []struct {
		msID string
		path string
	}{{ms1, subPath1}, {ms2, subPath2}} {
		_, err := store.Writer.ExecContext(ctx, `
			INSERT INTO item_media_sources (id, item_id, name, path, protocol)
			VALUES (?, ?, 'source', ?, 'File')`, row.msID, movieSafe.ID, safeVideoPath)
		if err != nil {
			t.Fatal(err)
		}
		_, err = store.Writer.ExecContext(ctx, `
			INSERT INTO item_media_streams (
				media_source_id, item_id, stream_index, stream_type, codec, language, is_external, path
			) VALUES (?, ?, 10, 'Subtitle', 'srt', 'eng', 1, ?)`, row.msID, movieSafe.ID, row.path)
		if err != nil {
			t.Fatal(err)
		}
	}

	adminAuth, _ := authSvc.AuthenticateByName(ctx, "admin", "admin123", "", auth.ClientInfo{DeviceID: "d-admin"})
	aliceAuth, _ := authSvc.AuthenticateByName(ctx, "alice", "alice123", "", auth.ClientInfo{DeviceID: "d-alice"})

	authMW := middleware.NewAuth(authSvc, nil)
	h := New(repo, nil, nil, nil, store, cfg)
	h.SetUserRepo(userRepo)
	router := h.BuildRouter(AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
		UserIDFromCtx: func(c context.Context) string {
			if ac, ok := middleware.AuthFromContext(c); ok && ac != nil {
				return ac.UserID()
			}
			return ""
		},
	})

	call := func(token, method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("X-Emby-Token", token)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	// 1. Cross-user ?UserId= IDOR on compatmedia endpoints
	idorPaths := []string{
		"/AudioBooks/NextUp?UserId=" + bob.ID,
		"/Items/" + movieSafe.ID + "/Similar?UserId=" + bob.ID,
		"/Items/" + movieSafe.ID + "/Ancestors?UserId=" + bob.ID,
		"/Persons/" + guid.New() + "/Credits?UserId=" + bob.ID,
		"/Shows/Upcoming?UserId=" + bob.ID,
		"/Artists?UserId=" + bob.ID,
		"/Artists/Prefixes?UserId=" + bob.ID,
		"/Artists/InstantMix?UserId=" + bob.ID,
		"/MusicGenres/InstantMix?UserId=" + bob.ID,
		"/Channels/Items/Latest?UserId=" + bob.ID,
		"/Collections/" + box.ID + "/ProviderItems?UserId=" + bob.ID,
		"/Playlists/" + bobPl.ID + "/AddToPlaylistInfo?UserId=" + bob.ID,
	}
	for _, p := range idorPaths {
		if rec := call(aliceAuth.AccessToken, http.MethodGet, p, ""); rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for alice GET %s, got %d", p, rec.Code)
		}
		if rec := call(adminAuth.AccessToken, http.MethodGet, p, ""); rec.Code != http.StatusOK {
			t.Fatalf("expected 200 for admin GET %s, got %d", p, rec.Code)
		}
	}

	// 2. Private Playlist IDOR bypass via /Collections/{Id}/ProviderItems -> 404
	if rec := call(aliceAuth.AccessToken, http.MethodGet, "/Collections/"+bobPl.ID+"/ProviderItems", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for Playlist ID via /Collections/{Id}/ProviderItems, got %d", rec.Code)
	}
	if rec := call(aliceAuth.AccessToken, http.MethodGet, "/Collections/"+box.ID+"/ProviderItems", ""); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for BoxSet via /Collections/{Id}/ProviderItems, got %d", rec.Code)
	}

	// 3. MakePublic / MakePrivate: non-Playlist requires admin (even if accessible!)
	if rec := call(aliceAuth.AccessToken, http.MethodPost, "/Items/"+movieSafe.ID+"/MakePublic", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin alice MakePublic on Movie, got %d", rec.Code)
	}
	if rec := call(aliceAuth.AccessToken, http.MethodPost, "/Items/"+movieSafe.ID+"/MakePrivate", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin alice MakePrivate on Movie, got %d", rec.Code)
	}
	if rec := call(adminAuth.AccessToken, http.MethodPost, "/Items/"+movieSafe.ID+"/MakePublic", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for admin MakePublic on Movie, got %d", rec.Code)
	}

	// 4. Artists/Prefixes & Taxonomies/{Name} respect RBAC
	rec := call(aliceAuth.AccessToken, http.MethodGet, "/Artists/Prefixes", "")
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `"Z"`) || !strings.Contains(rec.Body.String(), `"A"`) {
		t.Fatalf("expected only prefix A (not Z from restricted lib), got %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(aliceAuth.AccessToken, http.MethodGet, "/Artists/Zebra%20Secret", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for restricted artist lookup, got %d", rec.Code)
	}
	if rec := call(aliceAuth.AccessToken, http.MethodGet, "/Artists/Alpha%20Artist", ""); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for allowed artist lookup, got %d", rec.Code)
	}
	if rec := call(aliceAuth.AccessToken, http.MethodGet, "/Genres/SecretGenre", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for restricted genre lookup, got %d", rec.Code)
	}
	if rec := call(aliceAuth.AccessToken, http.MethodGet, "/Genres/Family", ""); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for allowed genre lookup, got %d", rec.Code)
	}

	// 5. GET /Items/{Id}/File: EnableContentDownloading + STRM leak protection
	if rec := call(aliceAuth.AccessToken, http.MethodGet, "/Items/"+movieSafe.ID+"/File", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when EnableContentDownloading=false for alice, got %d", rec.Code)
	}
	if rec := call(adminAuth.AccessToken, http.MethodGet, "/Items/"+movieSafe.ID+"/File", ""); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for admin downloading safe video, got %d", rec.Code)
	}
	if rec := call(adminAuth.AccessToken, http.MethodGet, "/Items/"+movieStrm.ID+"/File", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 downloading .strm file even for admin, got %d %s", rec.Code, rec.Body.String())
	}

	// 6. InstantMix filtering by artist & music genre
	rec = call(aliceAuth.AccessToken, http.MethodGet, "/Artists/InstantMix?Name=Alpha%20Artist", "")
	var mix dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &mix)
	if mix.TotalRecordCount != 1 || len(mix.Items) != 1 || mix.Items[0].Id != songRock.ID {
		t.Fatalf("expected only songRock for Artists/InstantMix?Name=Alpha Artist, got %+v", mix)
	}
	rec = call(aliceAuth.AccessToken, http.MethodGet, "/Artists/InstantMix?ArtistIds="+artistA.ID, "")
	mix = dto.QueryResult[dto.BaseItemDto]{}
	_ = json.Unmarshal(rec.Body.Bytes(), &mix)
	if mix.TotalRecordCount != 1 || len(mix.Items) != 1 || mix.Items[0].Id != songRock.ID {
		t.Fatalf("expected only songRock for Artists/InstantMix?ArtistIds=artistA, got %+v", mix)
	}
	rec = call(aliceAuth.AccessToken, http.MethodGet, "/MusicGenres/Jazz/InstantMix", "")
	mix = dto.QueryResult[dto.BaseItemDto]{}
	_ = json.Unmarshal(rec.Body.Bytes(), &mix)
	if mix.TotalRecordCount != 1 || len(mix.Items) != 1 || mix.Items[0].Id != songJazz.ID {
		t.Fatalf("expected only songJazz for /MusicGenres/Jazz/InstantMix, got %+v", mix)
	}
	rec = call(aliceAuth.AccessToken, http.MethodGet, "/MusicGenres/InstantMix?Name=Rock", "")
	mix = dto.QueryResult[dto.BaseItemDto]{}
	_ = json.Unmarshal(rec.Body.Bytes(), &mix)
	if mix.TotalRecordCount != 1 || len(mix.Items) != 1 || mix.Items[0].Id != songRock.ID {
		t.Fatalf("expected only songRock for /MusicGenres/InstantMix?Name=Rock, got %+v", mix)
	}

	// 7. DELETE /Items/{Id}/Subtitles/{Index} filters by MediaSourceId
	if rec := call(adminAuth.AccessToken, http.MethodDelete, "/Items/"+movieSafe.ID+"/Subtitles/10?MediaSourceId="+ms1, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 deleting ms1 subtitle, got %d %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(subPath1); !os.IsNotExist(err) {
		t.Fatalf("expected subPath1 to be deleted")
	}
	if _, err := os.Stat(subPath2); err != nil {
		t.Fatalf("expected subPath2 (different MediaSourceId) to remain intact: %v", err)
	}

	// 8. Negative image Index rejected with 400
	if rec := call(adminAuth.AccessToken, http.MethodDelete, "/Items/"+movieSafe.ID+"/Images/Backdrop/-1", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for negative image index delete, got %d", rec.Code)
	}
	if rec := call(adminAuth.AccessToken, http.MethodPost, "/Items/"+movieSafe.ID+"/Images/Backdrop/0/Index?newIndex=-2", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for negative newIndex, got %d", rec.Code)
	}

	// 9. POST /Library/VirtualFolders/Paths/Update returns 400 when both id and name are missing
	if rec := call(adminAuth.AccessToken, http.MethodPost, "/Library/VirtualFolders/Paths/Update", `{}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when id and name are missing, got %d", rec.Code)
	}

	// 10. Concurrent downloadAndSaveItemImage uses unique temp files safely
	imgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		// Minimal valid 1x1 PNG
		_, _ = w.Write([]byte{
			0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
			0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
			0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
			0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4,
			0x89, 0x00, 0x00, 0x00, 0x0A, 0x49, 0x44, 0x41,
			0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
			0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00,
			0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE,
			0x42, 0x60, 0x82,
		})
	}))
	defer imgSrv.Close()
	h.SetHTTPClient(imgSrv.Client())
	var wg sync.WaitGroup
	errCh := make(chan error, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if dErr := h.downloadAndSaveItemImage(ctx, movieSafe.ID, "Primary", 0, imgSrv.URL+"/p.png"); dErr != nil {
				errCh <- dErr
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for dErr := range errCh {
		t.Fatalf("concurrent downloadAndSaveItemImage failed: %v", dErr)
	}

	// 11. Additional parity & coverage checks: InstantMix/Similar seed RBAC, image index swap, MakePrivate
	if rec := call(aliceAuth.AccessToken, http.MethodGet, "/Items/"+movieSecret.ID+"/Similar", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for restricted seed Similar, got %d", rec.Code)
	}
	if rec := call(aliceAuth.AccessToken, http.MethodGet, "/Items/"+movieSecret.ID+"/InstantMix", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for restricted seed InstantMix, got %d", rec.Code)
	}
	if rec := call(aliceAuth.AccessToken, http.MethodGet, "/Items/"+songRock.ID+"/InstantMix", ""); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for allowed seed InstantMix, got %d", rec.Code)
	}
	if rec := call(adminAuth.AccessToken, http.MethodPost, "/Items/"+movieSafe.ID+"/MakePrivate", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for admin MakePrivate, got %d", rec.Code)
	}
	if rec := call(aliceAuth.AccessToken, http.MethodPost, "/Items/"+bobPl.ID+"/MakePublic", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for alice MakePublic on bob's playlist, got %d", rec.Code)
	}
	// Seed two backdrop images and swap their indexes
	_ = h.downloadAndSaveItemImage(ctx, movieSafe.ID, "Backdrop", 0, imgSrv.URL+"/b0.png")
	_ = h.downloadAndSaveItemImage(ctx, movieSafe.ID, "Backdrop", 1, imgSrv.URL+"/b1.png")
	if rec := call(adminAuth.AccessToken, http.MethodPost, "/Items/"+movieSafe.ID+"/Images/Backdrop/0/Index?newIndex=1", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 swapping backdrop index 0->1, got %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(adminAuth.AccessToken, http.MethodPost, "/Items/"+movieSafe.ID+"/Images/Backdrop/bad/Index?newIndex=1", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-numeric image index, got %d", rec.Code)
	}
	// Allow alice to download and verify GET /Items/{Id}/File succeeds
	alicePol.EnableContentDownloading = true
	_ = userRepo.UpdateUserPolicy(ctx, alice.ID, alicePol)
	if rec := call(aliceAuth.AccessToken, http.MethodGet, "/Items/"+movieSafe.ID+"/File", ""); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for alice downloading when enabled, got %d", rec.Code)
	}
}
