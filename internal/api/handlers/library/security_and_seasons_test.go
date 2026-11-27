package library

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestLibraryV097HardeningAndParity(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "lib_v097.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)

	authSvc := auth.NewService(userRepo, cfg)
	admin, _ := authSvc.CreateUser(ctx, "admin", "admin123", true)
	alice, _ := authSvc.CreateUser(ctx, "alice", "alice123", false)
	bob, _ := authSvc.CreateUser(ctx, "bob", "bob123", false)

	libA, _ := repo.CreateLibrary(ctx, "LibA", "movies", []string{tmp})
	libB, _ := repo.CreateLibrary(ctx, "LibB", "movies", []string{tmp})

	maxPG := 5
	alicePol := alice.Policy
	alicePol.EnableAllFolders = false
	alicePol.EnabledFolders = []string{libA.ID}
	alicePol.MaxParentalRating = &maxPG
	alicePol.BlockedTags = []string{"blocked"}
	if err := userRepo.UpdateUserPolicy(ctx, alice.ID, alicePol); err != nil {
		t.Fatal(err)
	}

	mSafe := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID,
		Type: "Movie", MediaType: "Video", Name: "Alpha Safe", SortName: "alpha safe", OfficialRating: "PG",
	}
	mR := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID,
		Type: "Movie", MediaType: "Video", Name: "Beta R", SortName: "beta r", OfficialRating: "R",
	}
	mBlocked := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID,
		Type: "Movie", MediaType: "Video", Name: "Gamma Blocked", SortName: "gamma blocked", OfficialRating: "G",
	}
	mOtherLib := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libB.ID, ParentID: libB.ID,
		Type: "Movie", MediaType: "Video", Name: "Delta Secret", SortName: "delta secret", OfficialRating: "G",
	}
	for _, m := range []*sqlite.ItemRecord{mSafe, mR, mBlocked, mOtherLib} {
		_ = repo.UpsertItem(ctx, m)
	}
	_ = repo.ReplaceItemTaxonomies(ctx, mSafe.ID, "genre", []string{"Action", "Adventure", "Comedy"})
	_ = repo.ReplaceItemTaxonomies(ctx, mBlocked.ID, "tag", []string{"blocked"})
	_, _ = repo.MarkFavorite(ctx, alice.ID, mSafe.ID)

	// Series + Seasons + Episodes for /Shows/{Id}/Episodes parity
	seriesID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID: seriesID, LibraryID: libA.ID, ParentID: libA.ID,
		Type: "Series", Name: "Test Show", SortName: "test show", OfficialRating: "PG",
	})
	s1ID, s2ID := guid.New(), guid.New()
	s1Num, s2Num := 1, 2
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID: s1ID, LibraryID: libA.ID, ParentID: seriesID, SeriesID: seriesID,
		Type: "Season", Name: "Season 1", SortName: "season 1", IndexNumber: &s1Num,
	})
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID: s2ID, LibraryID: libA.ID, ParentID: seriesID, SeriesID: seriesID,
		Type: "Season", Name: "Season 2", SortName: "season 2", IndexNumber: &s2Num,
	})
	var epIDs []string
	for i := 1; i <= 4; i++ {
		epNum := i
		ep := &sqlite.ItemRecord{
			ID: guid.New(), LibraryID: libA.ID, ParentID: s1ID, SeriesID: seriesID, SeasonID: s1ID,
			Type: "Episode", MediaType: "Video", Name: fmt.Sprintf("S1E%d", i), SortName: fmt.Sprintf("s1e%d", i),
			IndexNumber: &epNum, ParentIndexNumber: &s1Num, OfficialRating: "PG",
		}
		_ = repo.UpsertItem(ctx, ep)
		epIDs = append(epIDs, ep.ID)
	}
	ep21Num := 1
	epS2 := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: s2ID, SeriesID: seriesID, SeasonID: s2ID,
		Type: "Episode", MediaType: "Video", Name: "S2E1", SortName: "s2e1",
		IndexNumber: &ep21Num, ParentIndexNumber: &s2Num, OfficialRating: "PG",
	}
	_ = repo.UpsertItem(ctx, epS2)

	adminAuth, _ := authSvc.AuthenticateByName(ctx, "admin", "admin123", "", auth.ClientInfo{DeviceID: "d-admin"})
	aliceAuth, _ := authSvc.AuthenticateByName(ctx, "alice", "alice123", "", auth.ClientInfo{DeviceID: "d-alice"})

	authMW := middleware.NewAuth(authSvc, nil)
	mux := http.NewServeMux()
	h := New(repo, nil, cfg)
	h.SetUserRepo(userRepo)
	h.RegisterRoutes(mux, AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
		UserIDFromCtx: func(c context.Context) string {
			if ac, ok := middleware.AuthFromContext(c); ok && ac != nil {
				return ac.UserID()
			}
			return ""
		},
	})

	doGet := func(token, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if token != "" {
			req.Header.Set("X-Emby-Token", token)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// 1. Caller-to-UserId ownership (403 for alice accessing bob's user routes; 200 for self & admin)
	bobPaths := []string{
		"/Users/" + bob.ID + "/Views",
		"/Users/" + bob.ID + "/Items",
		"/Users/" + bob.ID + "/Items/Latest",
		"/Users/" + bob.ID + "/Items/Resume",
		"/Users/" + bob.ID + "/Items/" + mSafe.ID,
		"/Items?UserId=" + bob.ID,
		"/Items/" + mSafe.ID + "?UserId=" + bob.ID,
		"/Items/Counts?UserId=" + bob.ID,
	}
	for _, p := range bobPaths {
		if rec := doGet(aliceAuth.AccessToken, p); rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for alice GET %s, got %d", p, rec.Code)
		}
		if rec := doGet(adminAuth.AccessToken, p); rec.Code != http.StatusOK {
			t.Fatalf("expected 200 for admin GET %s, got %d", p, rec.Code)
		}
	}
	if rec := doGet(aliceAuth.AccessToken, "/Users/"+alice.ID+"/Views"); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for alice self Views, got %d", rec.Code)
	}

	// 2. GET /Items/Counts respects AllowedLibraryIDs + parental controls + IsFavorite
	rec := doGet(aliceAuth.AccessToken, "/Items/Counts")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for alice /Items/Counts, got %d", rec.Code)
	}
	var counts sqlite.ItemCounts
	_ = json.Unmarshal(rec.Body.Bytes(), &counts)
	if counts.MovieCount != 1 {
		t.Fatalf("expected MovieCount=1 for alice (only mSafe), got %d", counts.MovieCount)
	}

	rec = doGet(aliceAuth.AccessToken, "/Items/Counts?IsFavorite=true")
	var favCounts sqlite.ItemCounts
	_ = json.Unmarshal(rec.Body.Bytes(), &favCounts)
	if favCounts.MovieCount != 1 || favCounts.EpisodeCount != 0 {
		t.Fatalf("expected favorite MovieCount=1 EpisodeCount=0, got %+v", favCounts)
	}

	// 3. GET /Shows/{Id}/Episodes: Season, AdjacentTo, StartItemId, SortBy/SortOrder
	rec = doGet(aliceAuth.AccessToken, "/Shows/"+seriesID+"/Episodes?Season=2")
	var eps dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &eps)
	if eps.TotalRecordCount != 1 || len(eps.Items) != 1 || eps.Items[0].Id != epS2.ID {
		t.Fatalf("expected 1 episode in Season=2, got %+v", eps)
	}

	rec = doGet(aliceAuth.AccessToken, "/Shows/"+seriesID+"/Episodes?Season=1&AdjacentTo="+epIDs[1])
	eps = dto.QueryResult[dto.BaseItemDto]{}
	_ = json.Unmarshal(rec.Body.Bytes(), &eps)
	if eps.TotalRecordCount != 3 || len(eps.Items) != 3 || eps.Items[0].Id != epIDs[0] || eps.Items[1].Id != epIDs[1] || eps.Items[2].Id != epIDs[2] {
		t.Fatalf("expected AdjacentTo ep2 to return [ep1, ep2, ep3], got %+v", eps.Items)
	}

	rec = doGet(aliceAuth.AccessToken, "/Shows/"+seriesID+"/Episodes?Season=1&StartItemId="+epIDs[2])
	eps = dto.QueryResult[dto.BaseItemDto]{}
	_ = json.Unmarshal(rec.Body.Bytes(), &eps)
	if eps.TotalRecordCount != 2 || len(eps.Items) != 2 || eps.Items[0].Id != epIDs[2] || eps.Items[1].Id != epIDs[3] {
		t.Fatalf("expected StartItemId ep3 to return [ep3, ep4], got %+v", eps.Items)
	}

	rec = doGet(aliceAuth.AccessToken, "/Shows/"+seriesID+"/Episodes?Season=1&SortBy=IndexNumber&SortOrder=Descending")
	eps = dto.QueryResult[dto.BaseItemDto]{}
	_ = json.Unmarshal(rec.Body.Bytes(), &eps)
	if len(eps.Items) != 4 || eps.Items[0].Id != epIDs[3] {
		t.Fatalf("expected descending sort to start with ep4, got %+v", eps.Items)
	}

	// 4. GET /Genres with SearchTerm, NameStartsWith, SortBy, SortOrder
	rec = doGet(aliceAuth.AccessToken, "/Genres?NameStartsWith=A&SortBy=SortName&SortOrder=Descending")
	var genres dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &genres)
	if genres.TotalRecordCount != 2 || len(genres.Items) != 2 || genres.Items[0].Name != "Adventure" || genres.Items[1].Name != "Action" {
		t.Fatalf("expected [Adventure, Action], got %+v", genres.Items)
	}

	rec = doGet(aliceAuth.AccessToken, "/Genres?SearchTerm=med")
	genres = dto.QueryResult[dto.BaseItemDto]{}
	_ = json.Unmarshal(rec.Body.Bytes(), &genres)
	if genres.TotalRecordCount != 1 || len(genres.Items) != 1 || genres.Items[0].Name != "Comedy" {
		t.Fatalf("expected [Comedy] for SearchTerm=med, got %+v", genres.Items)
	}

	_ = admin
}

func TestLibraryV0101SecurityAndSeasonsParity(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "lib_v0101.db"))
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

	libA, _ := repo.CreateLibrary(ctx, "LibA", "movies", []string{tmp})
	libB, _ := repo.CreateLibrary(ctx, "LibB", "movies", []string{tmp})

	maxPG := 5
	alicePol := alice.Policy
	alicePol.EnableAllFolders = false
	alicePol.EnabledFolders = []string{libA.ID}
	alicePol.MaxParentalRating = &maxPG
	alicePol.BlockedTags = []string{"blocked"}
	if err := userRepo.UpdateUserPolicy(ctx, alice.ID, alicePol); err != nil {
		t.Fatal(err)
	}

	mSafe := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID,
		Type: "Movie", MediaType: "Video", Name: "Safe Movie", SortName: "safe movie", OfficialRating: "PG",
	}
	mR := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID,
		Type: "Movie", MediaType: "Video", Name: "Adult Movie", SortName: "adult movie", OfficialRating: "R",
	}
	mBlockedTag := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID,
		Type: "Movie", MediaType: "Video", Name: "Tagged Movie", SortName: "tagged movie", OfficialRating: "G",
	}
	mSecretLib := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libB.ID, ParentID: libB.ID,
		Type: "Movie", MediaType: "Video", Name: "Secret Movie", SortName: "secret movie", OfficialRating: "G",
	}
	for _, m := range []*sqlite.ItemRecord{mSafe, mR, mBlockedTag, mSecretLib} {
		_ = repo.UpsertItem(ctx, m)
	}
	_ = repo.ReplaceItemTaxonomies(ctx, mBlockedTag.ID, "tag", []string{"blocked"})

	// Series + Special season (0) + Seasons 1, 2, 3
	seriesID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID: seriesID, LibraryID: libA.ID, ParentID: libA.ID,
		Type: "Series", Name: "Show", SortName: "show", OfficialRating: "PG",
	})
	for i := 0; i <= 3; i++ {
		num := i
		name := "Specials"
		if i > 0 {
			name = "Season " + string(rune('0'+i))
		}
		_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
			ID: guid.New(), LibraryID: libA.ID, ParentID: seriesID, SeriesID: seriesID,
			Type: "Season", Name: name, SortName: name, IndexNumber: &num,
		})
	}

	adminAuth, _ := authSvc.AuthenticateByName(ctx, "admin", "admin123", "", auth.ClientInfo{DeviceID: "d-admin"})
	aliceAuth, _ := authSvc.AuthenticateByName(ctx, "alice", "alice123", "", auth.ClientInfo{DeviceID: "d-alice"})

	authMW := middleware.NewAuth(authSvc, nil)
	mux := http.NewServeMux()
	h := New(repo, nil, cfg)
	h.SetUserRepo(userRepo)
	h.SetStore(store)
	h.RegisterRoutes(mux, AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
		UserIDFromCtx: func(c context.Context) string {
			if ac, ok := middleware.AuthFromContext(c); ok && ac != nil {
				return ac.UserID()
			}
			return ""
		},
	})

	doReq := func(token, method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		if token != "" {
			req.Header.Set("X-Emby-Token", token)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// 1. Cross-user ?UserId= IDOR protection on Shows/Search/Taxonomies
	idorPaths := []string{
		"/Shows/" + seriesID + "/Seasons?UserId=" + bob.ID,
		"/Shows/" + seriesID + "/Episodes?UserId=" + bob.ID,
		"/Shows/NextUp?UserId=" + bob.ID,
		"/Search/Hints?SearchTerm=Safe&UserId=" + bob.ID,
		"/Genres?UserId=" + bob.ID,
		"/Studios?UserId=" + bob.ID,
		"/Persons?UserId=" + bob.ID,
		"/Years?UserId=" + bob.ID,
	}
	for _, p := range idorPaths {
		if rec := doReq(aliceAuth.AccessToken, http.MethodGet, p); rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for alice GET %s, got %d", p, rec.Code)
		}
		if rec := doReq(adminAuth.AccessToken, http.MethodGet, p); rec.Code != http.StatusOK {
			t.Fatalf("expected 200 for admin GET %s, got %d", p, rec.Code)
		}
	}
	if rec := doReq(aliceAuth.AccessToken, http.MethodGet, "/Shows/"+seriesID+"/Seasons?UserId="+alice.ID); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for alice self Seasons, got %d", rec.Code)
	}

	// 2. PlayedItems & FavoriteItems enforce canAccessItem (library RBAC + parental controls)
	for _, blockedID := range []string{mR.ID, mBlockedTag.ID, mSecretLib.ID} {
		for _, tc := range []struct {
			method string
			path   string
		}{
			{http.MethodPost, "/Users/" + alice.ID + "/PlayedItems/" + blockedID},
			{http.MethodDelete, "/Users/" + alice.ID + "/PlayedItems/" + blockedID},
			{http.MethodPost, "/Users/" + alice.ID + "/FavoriteItems/" + blockedID},
			{http.MethodDelete, "/Users/" + alice.ID + "/FavoriteItems/" + blockedID},
		} {
			if rec := doReq(aliceAuth.AccessToken, tc.method, tc.path); rec.Code != http.StatusForbidden {
				t.Fatalf("expected 403 for alice %s %s, got %d", tc.method, tc.path, rec.Code)
			}
		}
	}

	// 3. DatePlayed query param honored in MarkPlayed
	rec := doReq(aliceAuth.AccessToken, http.MethodPost, "/Users/"+alice.ID+"/PlayedItems/"+mSafe.ID+"?DatePlayed=20240501123045")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for alice MarkPlayed on mSafe, got %d %s", rec.Code, rec.Body.String())
	}
	var ud dto.UserItemDataDto
	_ = json.Unmarshal(rec.Body.Bytes(), &ud)
	wantTS := time.Date(2024, 5, 1, 12, 30, 45, 0, time.UTC)
	if ud.LastPlayedDate == nil || !ud.LastPlayedDate.UTC().Equal(wantTS) {
		t.Fatalf("expected LastPlayedDate=%v, got %v", wantTS, ud.LastPlayedDate)
	}
	persisted, err := repo.GetUserItemData(ctx, alice.ID, mSafe.ID)
	if err != nil || persisted.LastPlayedDate == nil || !persisted.LastPlayedDate.UTC().Equal(wantTS) {
		t.Fatalf("expected persisted LastPlayedDate=%v, got %+v err=%v", wantTS, persisted, err)
	}

	// 4. GET /Shows/{Id}/Seasons: IsSpecialSeason, StartIndex, Limit
	rec = doReq(aliceAuth.AccessToken, http.MethodGet, "/Shows/"+seriesID+"/Seasons?IsSpecialSeason=true")
	var seasons dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &seasons)
	if seasons.TotalRecordCount != 1 || len(seasons.Items) != 1 || seasons.Items[0].IndexNumber == nil || *seasons.Items[0].IndexNumber != 0 {
		t.Fatalf("expected 1 special season (index 0), got %+v", seasons)
	}

	rec = doReq(aliceAuth.AccessToken, http.MethodGet, "/Shows/"+seriesID+"/Seasons?IsSpecialSeason=false&StartIndex=1&Limit=1")
	seasons = dto.QueryResult[dto.BaseItemDto]{}
	_ = json.Unmarshal(rec.Body.Bytes(), &seasons)
	if seasons.TotalRecordCount != 3 || len(seasons.Items) != 1 || seasons.Items[0].IndexNumber == nil || *seasons.Items[0].IndexNumber != 2 {
		t.Fatalf("expected total=3 and page=[Season 2], got %+v", seasons)
	}
}

func TestLibraryV0104PlaylistIDORAndFailClosed(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "lib_v0104.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)
	authSvc := auth.NewService(userRepo, cfg)

	admin, _ := authSvc.CreateUser(ctx, "admin", "admin123", true)
	alice, _ := authSvc.CreateUser(ctx, "alice", "alice123", false)
	bob, _ := authSvc.CreateUser(ctx, "bob", "bob123", false)

	// Bob creates a private playlist (EnableAllFolders is true for alice by default)
	bobPl, err := repo.CreatePlaylist(ctx, "BobSecret", "Audio", bob.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Public playlist owned by bob
	bobPubPl, err := repo.CreatePlaylist(ctx, "BobPublic", "Audio", bob.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = userRepo.SetServerConfig(ctx, "item_visibility_"+bobPubPl.ID, `{"IsPublic":true}`)

	adminAuth, _ := authSvc.AuthenticateByName(ctx, "admin", "admin123", "", auth.ClientInfo{DeviceID: "d-adm"})
	aliceAuth, _ := authSvc.AuthenticateByName(ctx, "alice", "alice123", "", auth.ClientInfo{DeviceID: "d-ali"})

	authMW := middleware.NewAuth(authSvc, nil)
	mux := http.NewServeMux()
	h := New(repo, nil, cfg)
	h.SetUserRepo(userRepo)
	h.SetStore(store)
	h.RegisterRoutes(mux, AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
		UserIDFromCtx: func(c context.Context) string {
			if ac, ok := middleware.AuthFromContext(c); ok && ac != nil {
				return ac.UserID()
			}
			return ""
		},
	})

	doReq := func(tok, method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		if tok != "" {
			req.Header.Set("X-Emby-Token", tok)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// 1. [H-02] Alice MUST NOT mark/favorite Bob's private playlist (canAccessItem enforces canViewPlaylist)
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/Users/" + alice.ID + "/PlayedItems/" + bobPl.ID},
		{http.MethodDelete, "/Users/" + alice.ID + "/PlayedItems/" + bobPl.ID},
		{http.MethodPost, "/Users/" + alice.ID + "/FavoriteItems/" + bobPl.ID},
		{http.MethodDelete, "/Users/" + alice.ID + "/FavoriteItems/" + bobPl.ID},
	} {
		rec := doReq(aliceAuth.AccessToken, tc.method, tc.path)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("alice %s %s on bob private playlist expected 403, got %d (%s)", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}

	// Public playlist & admin access succeed
	if rec := doReq(aliceAuth.AccessToken, http.MethodPost, "/Users/"+alice.ID+"/FavoriteItems/"+bobPubPl.ID); rec.Code != http.StatusOK {
		t.Fatalf("alice favorite bob public playlist expected 200, got %d", rec.Code)
	}
	if rec := doReq(adminAuth.AccessToken, http.MethodPost, "/Users/"+admin.ID+"/FavoriteItems/"+bobPl.ID); rec.Code != http.StatusOK {
		t.Fatalf("admin favorite bob private playlist expected 200, got %d", rec.Code)
	}

	// 2. [MH-08] Fail-closed on unknown user or cancelled DB context
	if got := h.allowedLibrariesForUser(ctx, guid.New()); got == nil || len(got) != 0 {
		t.Fatalf("expected non-nil empty slice (fail-closed) for unknown user, got %#v", got)
	}
	cancelledCtx, cancel := context.WithCancel(ctx)
	cancel()
	if got := h.allowedLibrariesForUser(cancelledCtx, alice.ID); got == nil || len(got) != 0 {
		t.Fatalf("expected fail-closed empty slice on DB error, got %#v", got)
	}
	reqCancelled := httptest.NewRequest(http.MethodGet, "/Items", nil).WithContext(
		middleware.WithAuthContext(cancelledCtx, &middleware.AuthContext{User: alice}),
	)
	maxR, _ := h.userParentalPolicyForRequest(reqCancelled, alice.ID)
	if maxR == nil || *maxR != 0 {
		t.Fatalf("expected fail-closed maxRating=0 on DB error, got %v", maxR)
	}
}
