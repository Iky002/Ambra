package collections

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

func TestCollectionsPlaylistsOwnershipPolicyAndTypeValidation(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "col_v097.db"))
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

	libAllowed, err := repo.CreateLibrary(ctx, "AllowedLib", "movies", []string{tmp})
	if err != nil {
		t.Fatal(err)
	}
	libSecret, err := repo.CreateLibrary(ctx, "SecretLib", "movies", []string{tmp})
	if err != nil {
		t.Fatal(err)
	}

	// Restrict alice to libAllowed, MaxParentalRating=5 (PG), BlockedTags=["gore"].
	maxPG := 5
	alicePolicy := alice.Policy
	alicePolicy.EnableAllFolders = false
	alicePolicy.EnabledFolders = []string{libAllowed.ID}
	alicePolicy.MaxParentalRating = &maxPG
	alicePolicy.BlockedTags = []string{"gore"}
	if err := userRepo.UpdateUserPolicy(ctx, alice.ID, alicePolicy); err != nil {
		t.Fatal(err)
	}

	movieSafe := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      libAllowed.ID,
		ParentID:       libAllowed.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Safe PG Movie",
		SortName:       "safe pg movie",
		OfficialRating: "PG",
	}
	movieR := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      libAllowed.ID,
		ParentID:       libAllowed.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Adult R Movie",
		SortName:       "adult r movie",
		OfficialRating: "R",
	}
	movieGore := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      libAllowed.ID,
		ParentID:       libAllowed.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Tagged Gore Movie",
		SortName:       "tagged gore movie",
		OfficialRating: "PG",
	}
	movieSecret := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      libSecret.ID,
		ParentID:       libSecret.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Secret Lib Movie",
		SortName:       "secret lib movie",
		OfficialRating: "G",
	}
	for _, m := range []*sqlite.ItemRecord{movieSafe, movieR, movieGore, movieSecret} {
		if err := repo.UpsertItem(ctx, m); err != nil {
			t.Fatal(err)
		}
		_ = repo.ReplaceItemTaxonomies(ctx, m.ID, "genre", []string{"Action"})
	}
	if err := repo.ReplaceItemTaxonomies(ctx, movieGore.ID, "tag", []string{"gore"}); err != nil {
		t.Fatal(err)
	}

	adminAuth, _ := authSvc.AuthenticateByName(ctx, "admin", "admin123", "", auth.ClientInfo{DeviceID: "d-admin"})
	aliceAuth, _ := authSvc.AuthenticateByName(ctx, "alice", "alice123", "", auth.ClientInfo{DeviceID: "d-alice"})
	bobAuth, _ := authSvc.AuthenticateByName(ctx, "bob", "bob123", "", auth.ClientInfo{DeviceID: "d-bob"})

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

	doReq := func(token, method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		if token != "" {
			req.Header.Set("X-Emby-Token", token)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// Create BoxSet containing all 4 movies
	allIDs := []string{movieSafe.ID, movieR.ID, movieGore.ID, movieSecret.ID}
	boxRec, err := repo.CreateCollection(ctx, "MixedBox", "", allIDs)
	if err != nil {
		t.Fatal(err)
	}
	// Create playlists: alice's private, bob's private, bob's public
	alicePl, err := repo.CreatePlaylist(ctx, "AlicePrivate", "Video", alice.ID, allIDs)
	if err != nil {
		t.Fatal(err)
	}
	bobPrivatePl, err := repo.CreatePlaylist(ctx, "BobPrivate", "Video", bob.ID, []string{movieSafe.ID})
	if err != nil {
		t.Fatal(err)
	}
	bobPublicPl, err := repo.CreatePlaylist(ctx, "BobPublic", "Video", bob.ID, allIDs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Writer.ExecContext(ctx,
		`INSERT INTO server_config (key, value_json, updated_at) VALUES (?, ?, ?)`,
		"item_visibility_"+bobPublicPl.ID, `{"IsPublic":true}`, time.Now().Unix(),
	); err != nil {
		t.Fatal(err)
	}

	// 1. Container type validation: Playlist ID via /Collections/{Id}/Items -> 404, BoxSet ID via /Playlists/{Id}/Items -> 404
	if rec := doReq(adminAuth.AccessToken, http.MethodGet, fmt.Sprintf("/Collections/%s/Items", alicePl.ID)); rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for Playlist ID via /Collections/{Id}/Items, got %d", rec.Code)
	}
	if rec := doReq(adminAuth.AccessToken, http.MethodGet, fmt.Sprintf("/Playlists/%s/Items", boxRec.ID)); rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for BoxSet ID via /Playlists/{Id}/Items, got %d", rec.Code)
	}
	if rec := doReq(adminAuth.AccessToken, http.MethodGet, fmt.Sprintf("/Playlists/%s/InstantMix", boxRec.ID)); rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for BoxSet ID via /Playlists/{Id}/InstantMix, got %d", rec.Code)
	}

	// 2. GET /Playlists ownership: Alice sees AlicePrivate + BobPublic, NOT BobPrivate; Admin sees all 3
	rec := doReq(aliceAuth.AccessToken, http.MethodGet, "/Playlists")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for alice GET /Playlists, got %d", rec.Code)
	}
	var alicePlList dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &alicePlList)
	if alicePlList.TotalRecordCount != 2 || len(alicePlList.Items) != 2 {
		t.Fatalf("expected alice to see 2 playlists (own + public), got total=%d len=%d", alicePlList.TotalRecordCount, len(alicePlList.Items))
	}
	for _, it := range alicePlList.Items {
		if it.Id == bobPrivatePl.ID {
			t.Fatalf("alice should never see bob's private playlist")
		}
	}

	rec = doReq(adminAuth.AccessToken, http.MethodGet, "/Playlists")
	var adminPlList dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &adminPlList)
	if adminPlList.TotalRecordCount != 3 {
		t.Fatalf("expected admin to see all 3 playlists, got %d", adminPlList.TotalRecordCount)
	}

	// 3. Direct access to Bob's private playlist -> 403 for Alice, 200 for Bob and Admin
	if rec := doReq(aliceAuth.AccessToken, http.MethodGet, fmt.Sprintf("/Playlists/%s/Items", bobPrivatePl.ID)); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for alice GET bob's private playlist items, got %d", rec.Code)
	}
	if rec := doReq(aliceAuth.AccessToken, http.MethodGet, fmt.Sprintf("/Playlists/%s/InstantMix", bobPrivatePl.ID)); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for alice GET bob's private playlist instant mix, got %d", rec.Code)
	}
	if rec := doReq(bobAuth.AccessToken, http.MethodGet, fmt.Sprintf("/Playlists/%s/Items", bobPrivatePl.ID)); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for bob GET own playlist items, got %d", rec.Code)
	}
	if rec := doReq(adminAuth.AccessToken, http.MethodGet, fmt.Sprintf("/Playlists/%s/Items", bobPrivatePl.ID)); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for admin GET bob's private playlist items, got %d", rec.Code)
	}

	// 4. AllowedLibraryIDs + Parental Policy on Collections/{Id}/Items and Playlists/{Id}/Items
	for _, url := range []string{
		fmt.Sprintf("/Collections/%s/Items", boxRec.ID),
		fmt.Sprintf("/Playlists/%s/Items", alicePl.ID),
		fmt.Sprintf("/Playlists/%s/Items", bobPublicPl.ID),
	} {
		rec = doReq(aliceAuth.AccessToken, http.MethodGet, url)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 for alice GET %s, got %d", url, rec.Code)
		}
		var res dto.QueryResult[sqlite.PlaylistItemDto]
		_ = json.Unmarshal(rec.Body.Bytes(), &res)
		if res.TotalRecordCount != 1 || len(res.Items) != 1 || res.Items[0].Id != movieSafe.ID {
			t.Fatalf("GET %s for alice should only return movieSafe, got total=%d items=%+v", url, res.TotalRecordCount, res.Items)
		}
	}

	// Admin still sees all 4 items in MixedBox
	rec = doReq(adminAuth.AccessToken, http.MethodGet, fmt.Sprintf("/Collections/%s/Items", boxRec.ID))
	var adminBoxItems dto.QueryResult[sqlite.PlaylistItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &adminBoxItems)
	if adminBoxItems.TotalRecordCount != 4 {
		t.Fatalf("expected admin to see 4 items in box, got %d", adminBoxItems.TotalRecordCount)
	}

	// 5. Playlist InstantMix enforces AllowedLibraryIDs + Parental Policy
	rec = doReq(aliceAuth.AccessToken, http.MethodGet, fmt.Sprintf("/Playlists/%s/InstantMix", alicePl.ID))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for alice InstantMix, got %d", rec.Code)
	}
	var mixRes dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &mixRes)
	for _, it := range mixRes.Items {
		if it.Id == movieR.ID || it.Id == movieGore.ID || it.Id == movieSecret.ID {
			t.Fatalf("InstantMix leaked blocked/forbidden item %s (%s)", it.Id, it.Name)
		}
	}

	_ = admin
}
