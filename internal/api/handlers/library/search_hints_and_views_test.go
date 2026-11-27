package library

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestLibraryV0107AuditM9SearchHintsAndViewCoverParental(t *testing.T) {
	tmp := t.TempDir()
	st, err := sqlite.Open(filepath.Join(tmp, "lib_v0107.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(st)
	userRepo := sqlite.NewUserRepo(st)
	cfg := config.Default(tmp)

	lib, _ := repo.CreateLibrary(ctx, "Movies", "movies", []string{tmp})

	alice := &sqlite.UserRecord{ID: guid.New(), Name: "alice", Policy: sqlite.DefaultUserPolicy(false), Config: sqlite.DefaultUserConfig()}
	bob := &sqlite.UserRecord{ID: guid.New(), Name: "bob", Policy: sqlite.DefaultUserPolicy(false), Config: sqlite.DefaultUserConfig()}
	maxG := 1
	kid := &sqlite.UserRecord{
		ID: guid.New(), Name: "kid",
		Policy: dto.UserPolicy{
			EnableAllFolders:  true,
			MaxParentalRating: &maxG,
			BlockedTags:       []string{"adult"},
		},
		Config: sqlite.DefaultUserConfig(),
	}
	alice.Policy.EnableAllFolders = true
	bob.Policy.EnableAllFolders = true
	for _, u := range []*sqlite.UserRecord{alice, bob, kid} {
		_ = userRepo.CreateUser(ctx, u)
	}

	// Create 3 Bob private playlists matching "Alpha" AND 2 Movies matching "Alpha"
	for _, name := range []string{"Alpha Private 1", "Alpha Private 2", "Alpha Private 3"} {
		pl, err := repo.CreatePlaylist(ctx, name, "Video", bob.ID, nil)
		if err != nil {
			t.Fatal(err)
		}
		_ = repo.UpsertSearchFTS(ctx, pl.ID, name, name, "", "", "")
	}
	movie1 := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "Alpha One", OfficialRating: "R",
	}
	movie2 := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "Alpha Two", OfficialRating: "G",
	}
	for _, m := range []*sqlite.ItemRecord{movie1, movie2} {
		_ = repo.UpsertItem(ctx, m)
		_ = repo.UpsertSearchFTS(ctx, m.ID, m.Name, m.Name, "", "", "")
	}
	// Give R-rated movie1 a Backdrop tag and G-rated movie2 a Primary tag (no backdrop on movie2)
	_ = repo.UpsertImage(ctx, &sqlite.ImageRecord{
		ItemID: movie1.ID, ImageType: "Backdrop", ImageIndex: 0, Path: filepath.Join(tmp, "r_backdrop.jpg"), Tag: "r-rated-tag",
	})
	_ = repo.UpsertImage(ctx, &sqlite.ImageRecord{
		ItemID: movie2.ID, ImageType: "Primary", ImageIndex: 0, Path: filepath.Join(tmp, "g_primary.jpg"), Tag: "g-safe-tag",
	})

	h := New(repo, nil, cfg)
	h.SetUserRepo(userRepo)
	h.SetStore(st)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	doAs := func(u *sqlite.UserRecord, target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req = req.WithContext(middleware.WithAuthContext(req.Context(), &middleware.AuthContext{User: u}))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// 1. [M-9] SearchHints with Limit=2 must NOT have Bob's 3 private playlists crowd out Alice's 2 movies or leak count
	recHints := doAs(alice, "/Search/Hints?SearchTerm=Alpha&Limit=2")
	if recHints.Code != http.StatusOK {
		t.Fatalf("SearchHints got %d body=%s", recHints.Code, recHints.Body.String())
	}
	var hintRes dto.SearchHintResult
	_ = json.Unmarshal(recHints.Body.Bytes(), &hintRes)
	if len(hintRes.SearchHints) != 2 || hintRes.TotalRecordCount != 2 {
		t.Fatalf("M-9 expected 2 visible hints and TotalRecordCount=2, got len=%d total=%d hints=%+v",
			len(hintRes.SearchHints), hintRes.TotalRecordCount, hintRes.SearchHints)
	}
	for _, sh := range hintRes.SearchHints {
		if sh.Type == "Playlist" {
			t.Fatalf("M-9 leaked Bob's private playlist in SearchHints: %+v", sh)
		}
	}

	// 2. [M-9] libraryToViewDto must respect parental policy so kid gets "g-safe-tag", NOT "r-rated-tag"
	recViews := doAs(kid, "/Users/"+kid.ID+"/Views")
	if recViews.Code != http.StatusOK {
		t.Fatalf("kid Views got %d", recViews.Code)
	}
	var viewsRes dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(recViews.Body.Bytes(), &viewsRes)
	if len(viewsRes.Items) != 1 {
		t.Fatalf("expected 1 view, got %d", len(viewsRes.Items))
	}
	if tag := viewsRes.Items[0].ImageTags["Primary"]; tag != "g-safe-tag" {
		t.Fatalf("M-9 kid library view fallback cover leaked R-rated tag: got %q, want %q", tag, "g-safe-tag")
	}

	// Also verify GET /Items/{LibId} for kid respects parental policy on fallback cover
	recLibItem := doAs(kid, "/Items/"+lib.ID)
	if recLibItem.Code != http.StatusOK {
		t.Fatalf("kid GET /Items/{libID} got %d", recLibItem.Code)
	}
	var libDto dto.BaseItemDto
	_ = json.Unmarshal(recLibItem.Body.Bytes(), &libDto)
	if libDto.ImageTags["Primary"] != "g-safe-tag" {
		t.Fatalf("M-9 kid GET /Items/{libID} fallback cover got %q, want %q", libDto.ImageTags["Primary"], "g-safe-tag")
	}
}

func TestV0109Audit_SearchHintsLimitClampedTo200(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "library_v0109.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	libRepo := sqlite.NewLibraryRepo(store)
	cfg := config.Default(tmp)
	lib, err := libRepo.CreateLibrary(ctx, "Movies", "movies", []string{tmp})
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 215; i++ {
		id := guid.New()
		name := fmt.Sprintf("ClampMovie %03d", i)
		if err := libRepo.UpsertItem(ctx, &sqlite.ItemRecord{
			ID: id, LibraryID: lib.ID, ParentID: lib.ID,
			Type: "Movie", MediaType: "Video", Name: name,
		}); err != nil {
			t.Fatal(err)
		}
		_ = libRepo.UpsertSearchFTS(ctx, id, name, "clampmovie", "clampmovie", "cm", "")
	}

	h := New(libRepo, nil, cfg)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Search/Hints?SearchTerm=ClampMovie&Limit=500", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /Search/Hints expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var res dto.SearchHintResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.SearchHints) != 200 {
		t.Fatalf("expected SearchHints clamped to 200, got %d", len(res.SearchHints))
	}
}
