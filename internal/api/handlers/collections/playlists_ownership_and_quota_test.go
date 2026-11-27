package collections

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
		`{"Ids":["`+mSafe2.ID+`","`+mR.ID+`","`+mSecret.ID+`"]}`); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 adding items, got %d", rec.Code)
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

func TestAuditV0112_PlaylistQuotaAndItemAddedCountResponse(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "data")
	movDir := filepath.Join(tmp, "movies")
	_ = os.MkdirAll(dataDir, 0o755)
	_ = os.MkdirAll(movDir, 0o755)

	store, err := sqlite.Open(filepath.Join(dataDir, "col_v0112.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(dataDir)

	lib, _ := repo.CreateLibrary(ctx, "Movies", "movies", []string{movDir})
	m1 := &sqlite.ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", Name: "M1", OfficialRating: "G"}
	m2 := &sqlite.ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", Name: "M2", OfficialRating: "G"}
	_ = repo.UpsertItem(ctx, m1)
	_ = repo.UpsertItem(ctx, m2)

	alice := &sqlite.UserRecord{ID: guid.New(), Name: "alice_q", Policy: sqlite.DefaultUserPolicy(false), Config: sqlite.DefaultUserConfig()}
	admin := &sqlite.UserRecord{ID: guid.New(), Name: "admin_q", IsAdmin: true, Policy: sqlite.DefaultUserPolicy(true), Config: sqlite.DefaultUserConfig()}
	_ = userRepo.CreateUser(ctx, alice)
	_ = userRepo.CreateUser(ctx, admin)

	h := New(repo, cfg)
	h.SetUserRepo(userRepo)
	h.SetStore(store)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{
		UserIDFromCtx: func(c context.Context) string {
			if ac, ok := middleware.AuthFromContext(c); ok && ac != nil {
				return ac.UserID()
			}
			return ""
		},
	})

	doAs := func(u *sqlite.UserRecord, method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(middleware.WithAuthContext(req.Context(), &middleware.AuthContext{User: u}))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// 1. 3.1-10: POST /Playlists/{Id}/Items returns 200 OK with {"ItemAddedCount": 2}!
	createRec := doAs(alice, http.MethodPost, "/Playlists", `{"Name":"Alice First"}`)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create playlist failed: %d", createRec.Code)
	}
	var created creationResult
	_ = json.Unmarshal(createRec.Body.Bytes(), &created)

	addRec := doAs(alice, http.MethodPost, "/Playlists/"+created.Id+"/Items", `{"Ids":["`+m1.ID+`","`+m2.ID+`"]}`)
	if addRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on POST /Playlists/{Id}/Items, got %d: %s", addRec.Code, addRec.Body.String())
	}
	var addBody struct {
		ItemAddedCount int `json:"ItemAddedCount"`
	}
	if err := json.Unmarshal(addRec.Body.Bytes(), &addBody); err != nil || addBody.ItemAddedCount != 2 {
		t.Fatalf("expected {\"ItemAddedCount\":2}, got body=%s err=%v", addRec.Body.String(), err)
	}

	// 2. H-10: Seed 499 additional playlists for Alice (total 500) -> 501st POST /Playlists returns 429 Too Many Requests!
	tx, err := store.Writer.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	normAlice := guid.Normalize(alice.ID)
	for i := 0; i < maxPlaylistsPerUser-1; i++ {
		pid := fmt.Sprintf("aaaa00000000000000000000%08x", i+1)
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO items (id, library_id, parent_id, type, media_type, is_folder, name, sort_name, etag, extra_json, date_created, date_modified)
			 VALUES (?, 'playlists', ?, 'Playlist', 'Video', 1, ?, ?, 'e', '{}', 1, 1)`,
			pid, normAlice, fmt.Sprintf("P%d", i), fmt.Sprintf("p%d", i),
		); err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	overQuotaRec := doAs(alice, http.MethodPost, "/Playlists", `{"Name":"Over Quota 501"}`)
	if overQuotaRec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 Too Many Requests when non-admin exceeds %d playlists, got %d: %s", maxPlaylistsPerUser, overQuotaRec.Code, overQuotaRec.Body.String())
	}

	// Admin is not blocked by non-admin user playlist quota.
	adminRec := doAs(admin, http.MethodPost, "/Playlists", `{"Name":"Admin Playlist"}`)
	if adminRec.Code != http.StatusOK {
		t.Fatalf("expected admin POST /Playlists to succeed, got %d", adminRec.Code)
	}
}
