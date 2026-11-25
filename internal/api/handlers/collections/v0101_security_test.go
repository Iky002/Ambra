package collections

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestCollectionsV0101SecurityAndBodyDelete(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "col_v0101.db"))
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

	mSafe1 := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID,
		Type: "Movie", MediaType: "Video", Name: "Safe1", SortName: "safe1", OfficialRating: "PG",
	}
	mSafe2 := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID,
		Type: "Movie", MediaType: "Video", Name: "Safe2", SortName: "safe2", OfficialRating: "G",
	}
	mR := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID,
		Type: "Movie", MediaType: "Video", Name: "Adult", SortName: "adult", OfficialRating: "R",
	}
	mSecret := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libB.ID, ParentID: libB.ID,
		Type: "Movie", MediaType: "Video", Name: "Secret", SortName: "secret", OfficialRating: "G",
	}
	for _, m := range []*sqlite.ItemRecord{mSafe1, mSafe2, mR, mSecret} {
		_ = repo.UpsertItem(ctx, m)
	}

	box, _ := repo.CreateCollection(ctx, "Box", "", []string{mSafe1.ID, mSafe2.ID})
	alicePl, _ := repo.CreatePlaylist(ctx, "AlicePl", "Video", alice.ID, []string{mSafe1.ID})

	adminAuth, _ := authSvc.AuthenticateByName(ctx, "admin", "admin123", "", auth.ClientInfo{DeviceID: "d-admin"})
	aliceAuth, _ := authSvc.AuthenticateByName(ctx, "alice", "alice123", "", auth.ClientInfo{DeviceID: "d-alice"})

	authMW := middleware.NewAuth(authSvc, nil)
	mux := http.NewServeMux()
	h := New(repo, cfg)
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

	doReq := func(token, method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("X-Emby-Token", token)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// 1. Cross-user ?UserId= IDOR on Collections & Playlists read endpoints
	idorPaths := []string{
		"/Collections?UserId=" + bob.ID,
		"/Playlists?UserId=" + bob.ID,
		"/Playlists/" + alicePl.ID + "/InstantMix?UserId=" + bob.ID,
		"/Collections/" + box.ID + "/Items?UserId=" + bob.ID,
		"/Playlists/" + alicePl.ID + "/Items?UserId=" + bob.ID,
	}
	for _, p := range idorPaths {
		if rec := doReq(aliceAuth.AccessToken, http.MethodGet, p, ""); rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for alice GET %s, got %d", p, rec.Code)
		}
		if rec := doReq(adminAuth.AccessToken, http.MethodGet, p, ""); rec.Code != http.StatusOK {
			t.Fatalf("expected 200 for admin GET %s, got %d", p, rec.Code)
		}
	}

	// 2. POST /Playlists and POST /Playlists/{Id}/Items filter out blocked/inaccessible item IDs
	rec := doReq(aliceAuth.AccessToken, http.MethodPost, "/Playlists",
		`{"Name":"FilteredPl","MediaType":"Video","Ids":["`+mSafe1.ID+`","`+mR.ID+`","`+mSecret.ID+`"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 creating playlist, got %d %s", rec.Code, rec.Body.String())
	}
	var created creationResult
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	items, _, _ := repo.GetContainerItems(ctx, cfg.ServerID, alice.ID, created.Id, 0, 0, nil)
	if len(items) != 1 || items[0].Id != mSafe1.ID {
		t.Fatalf("expected only mSafe1 in created playlist, got %+v", items)
	}

	if rec := doReq(aliceAuth.AccessToken, http.MethodPost, "/Playlists/"+created.Id+"/Items",
		`{"Ids":["`+mSafe2.ID+`","`+mR.ID+`","`+mSecret.ID+`"]}`); rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 adding items, got %d", rec.Code)
	}
	items, _, _ = repo.GetContainerItems(ctx, cfg.ServerID, alice.ID, created.Id, 0, 0, nil)
	if len(items) != 2 {
		t.Fatalf("expected 2 safe items in playlist (blocked items filtered), got %+v", items)
	}

	// 3. DELETE /Collections/{Id}/Items and DELETE /Playlists/{Id}/Items parse JSON body (EntryIds / Ids)
	if rec := doReq(adminAuth.AccessToken, http.MethodDelete, "/Collections/"+box.ID+"/Items",
		`{"Ids":["`+mSafe1.ID+`"]}`); rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 removing collection item via JSON body, got %d", rec.Code)
	}
	boxItems, _, _ := repo.GetContainerItems(ctx, cfg.ServerID, alice.ID, box.ID, 0, 0, nil)
	if len(boxItems) != 1 || boxItems[0].Id != mSafe2.ID {
		t.Fatalf("expected only mSafe2 remaining in box, got %+v", boxItems)
	}

	if rec := doReq(aliceAuth.AccessToken, http.MethodDelete, "/Playlists/"+created.Id+"/Items",
		`{"EntryIds":["`+items[0].PlaylistItemId+`"]}`); rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 removing playlist item via JSON body EntryIds, got %d", rec.Code)
	}
	var plRes dto.QueryResult[sqlite.PlaylistItemDto]
	rec = doReq(aliceAuth.AccessToken, http.MethodGet, "/Playlists/"+created.Id+"/Items", "")
	_ = json.Unmarshal(rec.Body.Bytes(), &plRes)
	if plRes.TotalRecordCount != 1 {
		t.Fatalf("expected 1 remaining playlist item after JSON body delete, got %d", plRes.TotalRecordCount)
	}

	// 4. Move playlist item, non-owner 403, and delete container
	bobAuth, _ := authSvc.AuthenticateByName(ctx, "bob", "bob123", "", auth.ClientInfo{DeviceID: "d-bob"})
	if rec := doReq(bobAuth.AccessToken, http.MethodDelete, "/Playlists/"+created.Id, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for bob deleting alice's playlist, got %d", rec.Code)
	}
	if rec := doReq(aliceAuth.AccessToken, http.MethodPost, "/Playlists/"+created.Id+"/Items/"+plRes.Items[0].PlaylistItemId+"/Move/0", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 moving playlist item, got %d", rec.Code)
	}
	if rec := doReq(aliceAuth.AccessToken, http.MethodPost, "/Playlists/"+created.Id+"/Items/"+plRes.Items[0].PlaylistItemId+"/Move/bad", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid move index, got %d", rec.Code)
	}
	if rec := doReq(aliceAuth.AccessToken, http.MethodDelete, "/Playlists/"+created.Id, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for alice deleting own playlist, got %d", rec.Code)
	}
	if rec := doReq(adminAuth.AccessToken, http.MethodDelete, "/Collections/"+box.ID, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for admin deleting collection, got %d", rec.Code)
	}
}
