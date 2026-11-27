package collections

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestCollectionsAndPlaylistsRBACAndTypeSafety(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "col_rbac.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)

	authSvc := auth.NewService(userRepo, cfg)
	admin, err := authSvc.CreateUser(ctx, "admin", "admin123", true)
	if err != nil {
		t.Fatal(err)
	}
	alice, err := authSvc.CreateUser(ctx, "alice", "alice123", false)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := authSvc.CreateUser(ctx, "bob", "bob123", false)
	if err != nil {
		t.Fatal(err)
	}

	adminAuth, _ := authSvc.AuthenticateByName(ctx, "admin", "admin123", "", auth.ClientInfo{DeviceID: "d-admin"})
	aliceAuth, _ := authSvc.AuthenticateByName(ctx, "alice", "alice123", "", auth.ClientInfo{DeviceID: "d-alice"})
	bobAuth, _ := authSvc.AuthenticateByName(ctx, "bob", "bob123", "", auth.ClientInfo{DeviceID: "d-bob"})

	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{tmp})
	if err != nil {
		t.Fatal(err)
	}
	movie := &sqlite.ItemRecord{
		ID:        guid.New(),
		LibraryID: lib.ID,
		ParentID:  lib.ID,
		Type:      "Movie",
		MediaType: "Video",
		Name:      "Protected Movie",
		SortName:  "protected movie",
	}
	if err := repo.UpsertItem(ctx, movie); err != nil {
		t.Fatal(err)
	}

	authMW := middleware.NewAuth(authSvc, nil)
	mux := http.NewServeMux()
	h := New(repo, cfg)
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

	// 1. Non-admin cannot create collection -> 403
	if rec := doReq(aliceAuth.AccessToken, http.MethodPost, "/Collections?Name=AliceBox"); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin POST /Collections, got %d", rec.Code)
	}

	// 2. Admin can create collection -> 200
	rec := doReq(adminAuth.AccessToken, http.MethodPost, fmt.Sprintf("/Collections?Name=AdminBox&Ids=%s", movie.ID))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for admin POST /Collections, got %d %s", rec.Code, rec.Body.String())
	}
	var box creationResult
	_ = json.Unmarshal(rec.Body.Bytes(), &box)

	// 3. Non-admin cannot add/remove items or delete collection -> 403
	if rec := doReq(aliceAuth.AccessToken, http.MethodPost, fmt.Sprintf("/Collections/%s/Items?Ids=%s", box.Id, movie.ID)); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin POST /Collections/{Id}/Items, got %d", rec.Code)
	}
	if rec := doReq(aliceAuth.AccessToken, http.MethodDelete, fmt.Sprintf("/Collections/%s/Items?Ids=%s", box.Id, movie.ID)); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin DELETE /Collections/{Id}/Items, got %d", rec.Code)
	}
	if rec := doReq(aliceAuth.AccessToken, http.MethodDelete, fmt.Sprintf("/Collections/%s", box.Id)); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin DELETE /Collections/{Id}, got %d", rec.Code)
	}

	// 4. DELETE /Collections/{movieID} and DELETE /Playlists/{movieID} must NEVER delete a Movie
	if rec := doReq(adminAuth.AccessToken, http.MethodDelete, fmt.Sprintf("/Collections/%s", movie.ID)); rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 deleting Movie via /Collections, got %d", rec.Code)
	}
	if rec := doReq(adminAuth.AccessToken, http.MethodDelete, fmt.Sprintf("/Playlists/%s", movie.ID)); rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 deleting Movie via /Playlists, got %d", rec.Code)
	}
	if _, err := repo.GetItemRecordByID(ctx, movie.ID); err != nil {
		t.Fatalf("Movie should still exist after container delete attempts: %v", err)
	}

	// 5. Alice creates her own Playlist -> 200; cannot create playlist for Bob -> 403
	if rec := doReq(aliceAuth.AccessToken, http.MethodPost, fmt.Sprintf("/Playlists?Name=ForBob&UserId=%s", bob.ID)); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 creating playlist for another user, got %d", rec.Code)
	}
	rec = doReq(aliceAuth.AccessToken, http.MethodPost, fmt.Sprintf("/Playlists?Name=AliceList&MediaType=Video&Ids=%s", movie.ID))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for alice POST /Playlists, got %d %s", rec.Code, rec.Body.String())
	}
	var pl creationResult
	_ = json.Unmarshal(rec.Body.Bytes(), &pl)

	// 6. DELETE /Collections/{playlistID} must reject wrong container type -> 404
	if rec := doReq(adminAuth.AccessToken, http.MethodDelete, fmt.Sprintf("/Collections/%s", pl.Id)); rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 deleting Playlist via /Collections, got %d", rec.Code)
	}

	// 7. Bob cannot modify or delete Alice's Playlist -> 403
	if rec := doReq(bobAuth.AccessToken, http.MethodPost, fmt.Sprintf("/Playlists/%s/Items?Ids=%s", pl.Id, movie.ID)); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for bob adding to alice's playlist, got %d", rec.Code)
	}
	if rec := doReq(bobAuth.AccessToken, http.MethodPost, fmt.Sprintf("/Playlists/%s/Items/%s/Move/0", pl.Id, movie.ID)); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for bob moving in alice's playlist, got %d", rec.Code)
	}
	if rec := doReq(bobAuth.AccessToken, http.MethodDelete, fmt.Sprintf("/Playlists/%s/Items?Ids=%s", pl.Id, movie.ID)); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for bob removing from alice's playlist, got %d", rec.Code)
	}
	if rec := doReq(bobAuth.AccessToken, http.MethodDelete, fmt.Sprintf("/Playlists/%s", pl.Id)); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for bob deleting alice's playlist, got %d", rec.Code)
	}

	// 8. Alice can modify and delete her own Playlist -> 204
	if rec := doReq(aliceAuth.AccessToken, http.MethodPost, fmt.Sprintf("/Playlists/%s/Items?Ids=%s", pl.Id, movie.ID)); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for alice adding to own playlist, got %d", rec.Code)
	}
	if rec := doReq(aliceAuth.AccessToken, http.MethodDelete, fmt.Sprintf("/Playlists/%s", pl.Id)); rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for alice deleting own playlist, got %d", rec.Code)
	}

	// 9. Admin can delete another user's playlist and own collection
	rec = doReq(aliceAuth.AccessToken, http.MethodPost, "/Playlists?Name=AliceList2")
	var pl2 creationResult
	_ = json.Unmarshal(rec.Body.Bytes(), &pl2)
	if rec := doReq(adminAuth.AccessToken, http.MethodDelete, fmt.Sprintf("/Playlists/%s", pl2.Id)); rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for admin deleting alice's playlist, got %d", rec.Code)
	}
	if rec := doReq(adminAuth.AccessToken, http.MethodDelete, fmt.Sprintf("/Collections/%s", box.Id)); rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for admin deleting collection, got %d", rec.Code)
	}
	_ = admin
	_ = alice
}
