package library

import (
	"context"
	"encoding/json"
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
