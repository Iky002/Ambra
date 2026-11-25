package library

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestLibraryV0103Hardening(t *testing.T) {
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
	libRepo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	authSvc := auth.NewService(userRepo, cfg)

	admin, _ := authSvc.CreateUser(ctx, "admin", "admin123", true)
	alice, _ := authSvc.CreateUser(ctx, "alice", "alice123", false)
	bob, _ := authSvc.CreateUser(ctx, "bob", "bob123", false)

	libMovies, _ := libRepo.CreateLibrary(ctx, "Movies", "movies", []string{"/srv/secret/movies"})
	// Restrict alice to only libMovies (NOT "playlists") and max rating PG (5)
	ap := alice.Policy
	ap.EnableAllFolders = false
	ap.EnabledFolders = []string{libMovies.ID}
	maxPG := 5
	ap.MaxParentalRating = &maxPG
	_ = userRepo.UpdateUserPolicy(ctx, alice.ID, ap)
	authSvc.InvalidateUserCache(alice.ID)

	adminAuth, _ := authSvc.AuthenticateByName(ctx, "admin", "admin123", "", auth.ClientInfo{DeviceID: "d-adm"})
	aliceAuth, _ := authSvc.AuthenticateByName(ctx, "alice", "alice123", "", auth.ClientInfo{DeviceID: "d-ali"})

	// Create movie with physical path
	movie := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      libMovies.ID,
		ParentID:       libMovies.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Family Film",
		SortName:       "family film",
		Path:           "/srv/secret/movies/family.mkv",
		OfficialRating: "G",
	}
	_ = libRepo.UpsertItem(ctx, movie)

	// Create R-rated Series -> Season -> Episode (episode has no rating of its own)
	seriesR := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      libMovies.ID,
		ParentID:       libMovies.ID,
		Type:           "Series",
		Name:           "Mature Show",
		SortName:       "mature show",
		OfficialRating: "R",
	}
	seasonR := &sqlite.ItemRecord{
		ID:        guid.New(),
		LibraryID: libMovies.ID,
		ParentID:  seriesR.ID,
		SeriesID:  seriesR.ID,
		Type:      "Season",
		Name:      "Season 1",
	}
	epR := &sqlite.ItemRecord{
		ID:        guid.New(),
		LibraryID: libMovies.ID,
		ParentID:  seasonR.ID,
		SeriesID:  seriesR.ID,
		SeasonID:  seasonR.ID,
		Type:      "Episode",
		MediaType: "Video",
		Name:      "Episode 1",
	}
	_ = libRepo.UpsertItem(ctx, seriesR)
	_ = libRepo.UpsertItem(ctx, seasonR)
	_ = libRepo.UpsertItem(ctx, epR)

	// Create playlists: alice's own, bob's private, bob's public
	alicePL, err := libRepo.CreatePlaylist(ctx, "Alice Private", "Video", alice.ID, []string{movie.ID})
	if err != nil {
		t.Fatal(err)
	}
	bobPrivPL, err := libRepo.CreatePlaylist(ctx, "Bob Secret Playlist", "Video", bob.ID, []string{movie.ID})
	if err != nil {
		t.Fatal(err)
	}
	bobPubPL, err := libRepo.CreatePlaylist(ctx, "Bob Public Playlist", "Video", bob.ID, []string{movie.ID})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = store.Writer.ExecContext(ctx,
		`INSERT INTO server_config (key, value_json, updated_at) VALUES (?, '{"IsPublic":true}', 1)`,
		"item_visibility_"+bobPubPL.ID,
	)

	h := New(libRepo, nil, cfg)
	h.SetUserRepo(userRepo)
	h.SetStore(store)
	mux := http.NewServeMux()
	authMW := middleware.NewAuth(authSvc, nil)
	h.RegisterRoutes(mux, AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
		UserIDFromCtx: func(ctx context.Context) string {
			if ac, ok := middleware.AuthFromContext(ctx); ok && ac != nil {
				return ac.UserID()
			}
			return ""
		},
	})

	doReq := func(tok, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-Emby-Token", tok)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// 1. Cross-user private playlist IDOR blocked (403); own & public playlists allowed for restricted user!
	if rec := doReq(aliceAuth.AccessToken, "/Items/"+bobPrivPL.ID); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for alice reading bob's private playlist, got %d", rec.Code)
	}
	if rec := doReq(aliceAuth.AccessToken, "/Items/"+alicePL.ID); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for restricted alice reading own playlist, got %d (%s)", rec.Code, rec.Body.String())
	}
	if rec := doReq(aliceAuth.AccessToken, "/Items/"+bobPubPL.ID); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for restricted alice reading bob's public playlist, got %d (%s)", rec.Code, rec.Body.String())
	}

	// 2. QueryItems & SearchHints must include alice's + bob's public playlists, and exclude bob's private playlist
	rec := doReq(aliceAuth.AccessToken, "/Items?IncludeItemTypes=Playlist&Recursive=true")
	if rec.Code != http.StatusOK {
		t.Fatalf("QueryItems playlists got %d", rec.Code)
	}
	var qr dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &qr)
	foundIDs := map[string]bool{}
	for _, it := range qr.Items {
		foundIDs[it.Id] = true
	}
	if !foundIDs[alicePL.ID] || !foundIDs[bobPubPL.ID] || foundIDs[bobPrivPL.ID] {
		t.Fatalf("unexpected playlists returned for alice: %+v (alice=%s bobPub=%s bobPriv=%s)", foundIDs, alicePL.ID, bobPubPL.ID, bobPrivPL.ID)
	}

	rec = doReq(aliceAuth.AccessToken, "/Search/Hints?SearchTerm=Bob")
	if rec.Code != http.StatusOK {
		t.Fatalf("SearchHints got %d", rec.Code)
	}
	if bytesContains(rec.Body.Bytes(), bobPrivPL.ID) {
		t.Fatalf("SearchHints leaked bob's private playlist: %s", rec.Body.String())
	}

	// 3. ShowSeasons & ShowEpisodes block parent Series failing parental control
	if rec := doReq(aliceAuth.AccessToken, "/Shows/"+seriesR.ID+"/Seasons"); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for alice ShowSeasons on R-rated series, got %d", rec.Code)
	}
	if rec := doReq(aliceAuth.AccessToken, "/Shows/"+seriesR.ID+"/Episodes"); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for alice ShowEpisodes on R-rated series, got %d", rec.Code)
	}

	// 4. Non-admin item Path cleared; VirtualFolders Locations hidden
	rec = doReq(aliceAuth.AccessToken, "/Items/"+movie.ID)
	var itemDTO dto.BaseItemDto
	_ = json.Unmarshal(rec.Body.Bytes(), &itemDTO)
	if itemDTO.Path != "" {
		t.Fatalf("expected empty Path for non-admin, got %q", itemDTO.Path)
	}
	rec = doReq(adminAuth.AccessToken, "/Items/"+movie.ID)
	var adminItemDTO dto.BaseItemDto
	_ = json.Unmarshal(rec.Body.Bytes(), &adminItemDTO)
	if adminItemDTO.Path == "" {
		t.Fatalf("expected admin to see item Path, got empty (admin=%s)", admin.ID)
	}

	rec = doReq(aliceAuth.AccessToken, "/Library/VirtualFolders")
	if rec.Code != http.StatusOK {
		t.Fatalf("VirtualFolders got %d", rec.Code)
	}
	if bytesContains(rec.Body.Bytes(), "/srv/secret/movies") {
		t.Fatalf("VirtualFolders leaked physical path to non-admin: %s", rec.Body.String())
	}
}

func bytesContains(b []byte, sub string) bool {
	return len(sub) > 0 && len(b) >= len(sub) && stringIndex(string(b), sub) >= 0
}

func stringIndex(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
