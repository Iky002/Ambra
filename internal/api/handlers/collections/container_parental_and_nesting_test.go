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

func TestCollectionsV0103ContainerItemFiltering(t *testing.T) {
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

	libSafe, _ := repo.CreateLibrary(ctx, "Safe", "movies", []string{"/safe"})
	libSecret, _ := repo.CreateLibrary(ctx, "Secret", "movies", []string{"/secret"})

	ap := alice.Policy
	ap.EnableAllFolders = false
	ap.EnabledFolders = []string{libSafe.ID}
	maxPG := 5
	ap.MaxParentalRating = &maxPG
	_ = userRepo.UpdateUserPolicy(ctx, alice.ID, ap)
	authSvc.InvalidateUserCache(alice.ID)

	safeMovie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libSafe.ID, ParentID: libSafe.ID,
		Type: "Movie", MediaType: "Video", Name: "Safe Movie", Path: "/safe/movie.mkv", OfficialRating: "G",
	}
	rMovie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libSafe.ID, ParentID: libSafe.ID,
		Type: "Movie", MediaType: "Video", Name: "R Movie", Path: "/safe/r.mkv", OfficialRating: "R",
	}
	secretMovie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libSecret.ID, ParentID: libSecret.ID,
		Type: "Movie", MediaType: "Video", Name: "Secret Movie", Path: "/secret/s.mkv", OfficialRating: "G",
	}
	_ = repo.UpsertItem(ctx, safeMovie)
	_ = repo.UpsertItem(ctx, rMovie)
	_ = repo.UpsertItem(ctx, secretMovie)

	col, err := repo.CreateCollection(ctx, "Mixed BoxSet", "", []string{safeMovie.ID, rMovie.ID, secretMovie.ID})
	if err != nil {
		t.Fatal(err)
	}

	h := New(repo, cfg)
	h.SetUserRepo(userRepo)
	h.SetStore(store)
	authMW := middleware.NewAuth(authSvc, nil)
	mux := http.NewServeMux()
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

	adminAuth, _ := authSvc.AuthenticateByName(ctx, "admin", "admin123", "", auth.ClientInfo{DeviceID: "d0"})
	aliceAuth, _ := authSvc.AuthenticateByName(ctx, "alice", "alice123", "", auth.ClientInfo{DeviceID: "d1"})

	// Non-admin alice only sees safeMovie and with physical Path stripped
	req := httptest.NewRequest(http.MethodGet, "/Collections/"+col.ID+"/Items", nil)
	req.Header.Set("X-Emby-Token", aliceAuth.AccessToken)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("alice Collections/Items got %d: %s", rec.Code, rec.Body.String())
	}
	var aliceQR dto.QueryResult[sqlite.PlaylistItemDto]
	if err := json.Unmarshal(rec.Body.Bytes(), &aliceQR); err != nil {
		t.Fatal(err)
	}
	if aliceQR.TotalRecordCount != 1 || len(aliceQR.Items) != 1 || aliceQR.Items[0].Id != safeMovie.ID {
		t.Fatalf("expected only safeMovie for alice, got total=%d items=%+v", aliceQR.TotalRecordCount, aliceQR.Items)
	}
	if aliceQR.Items[0].Path != "" {
		t.Fatalf("expected empty Path for non-admin collection member, got %q", aliceQR.Items[0].Path)
	}

	// Admin sees all 3 items
	reqAdm := httptest.NewRequest(http.MethodGet, "/Collections/"+col.ID+"/Items", nil)
	reqAdm.Header.Set("X-Emby-Token", adminAuth.AccessToken)
	recAdm := httptest.NewRecorder()
	mux.ServeHTTP(recAdm, reqAdm)
	var adminQR dto.QueryResult[sqlite.PlaylistItemDto]
	_ = json.Unmarshal(recAdm.Body.Bytes(), &adminQR)
	if adminQR.TotalRecordCount != 3 || len(adminQR.Items) != 3 {
		t.Fatalf("expected all 3 items for admin (id=%s), got total=%d len=%d", admin.ID, adminQR.TotalRecordCount, len(adminQR.Items))
	}
}

func TestCollectionsV0104ContainerParentalAndSanitizationAndFailClosed(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "col_v0104.db"))
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
	kid, _ := authSvc.CreateUser(ctx, "kid", "kid12345", false)
	maxPG := 5
	kidPol := kid.Policy
	kidPol.MaxParentalRating = &maxPG
	kidPol.BlockedTags = []string{"AdultOnly"}
	_ = userRepo.UpdateUserPolicySafe(ctx, kid.ID, kidPol)

	lib, _ := repo.CreateLibrary(ctx, "Movies", "movies", []string{tmp})
	safeMovie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "SafeMovie", OfficialRating: "G",
		Path: "/secret/server/safe.mkv",
	}
	_ = repo.UpsertItem(ctx, safeMovie)

	// R-rated BoxSet containing a G-rated movie: container itself MUST be blocked for kid!
	rBox, err := repo.CreateCollection(ctx, "RestrictedBox", "", []string{safeMovie.ID})
	if err != nil {
		t.Fatal(err)
	}
	rBox.OfficialRating = "R"
	rBox.Path = "/secret/server/rbox"
	_ = repo.UpsertItem(ctx, rBox)

	// Blocked-tag BoxSet containing a G-rated movie
	tagBox, err := repo.CreateCollection(ctx, "TaggedBox", "", []string{safeMovie.ID})
	if err != nil {
		t.Fatal(err)
	}
	_ = repo.ReplaceItemTaxonomies(ctx, tagBox.ID, "tag", []string{"AdultOnly"})

	// Safe BoxSet with a path to verify non-admin path sanitization
	safeBox, err := repo.CreateCollection(ctx, "SafeBox", "", []string{safeMovie.ID})
	if err != nil {
		t.Fatal(err)
	}
	safeBox.Path = "/secret/server/safebox"
	_ = repo.UpsertItem(ctx, safeBox)

	// Kid's playlist with path
	kidPl, err := repo.CreatePlaylist(ctx, "KidPl", "Video", kid.ID, []string{safeMovie.ID})
	if err != nil {
		t.Fatal(err)
	}
	kidPl.Path = "/secret/server/kidpl"
	_ = repo.UpsertItem(ctx, kidPl)

	adminAuth, _ := authSvc.AuthenticateByName(ctx, "admin", "admin123", "", auth.ClientInfo{DeviceID: "d-adm"})
	kidAuth, _ := authSvc.AuthenticateByName(ctx, "kid", "kid12345", "", auth.ClientInfo{DeviceID: "d-kid"})

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

	doGet := func(tok, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-Emby-Token", tok)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// 1. BoxSet container itself blocked by MaxParentalRating and BlockedTags -> 403 for kid, 200 for admin
	for _, boxID := range []string{rBox.ID, tagBox.ID} {
		if rec := doGet(kidAuth.AccessToken, "/Collections/"+boxID+"/Items"); rec.Code != http.StatusForbidden {
			t.Fatalf("kid GET /Collections/%s/Items expected 403, got %d (%s)", boxID, rec.Code, rec.Body.String())
		}
		if rec := doGet(adminAuth.AccessToken, "/Collections/"+boxID+"/Items"); rec.Code != http.StatusOK {
			t.Fatalf("admin GET /Collections/%s/Items expected 200, got %d", boxID, rec.Code)
		}
	}

	// 2. [MH-05] Non-admin path sanitization on Collections, Playlists, InstantMix, ContainerItems
	for _, ep := range []string{
		"/Collections?Fields=Path",
		"/Playlists?Fields=Path",
		"/Collections/" + safeBox.ID + "/Items?Fields=Path",
		"/Playlists/" + kidPl.ID + "/InstantMix?Fields=Path",
	} {
		rec := doGet(kidAuth.AccessToken, ep)
		if rec.Code != http.StatusOK {
			t.Fatalf("kid GET %s expected 200, got %d (%s)", ep, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "/secret/server/") {
			t.Fatalf("non-admin GET %s leaked server path: %s", ep, rec.Body.String())
		}
	}

	// 3. [MH-08] Fail-closed on DB error
	cancelledCtx, cancel := context.WithCancel(ctx)
	cancel()
	if got := h.allowedLibrariesForUser(cancelledCtx, kid.ID); got == nil || len(got) != 0 {
		t.Fatalf("expected fail-closed empty slice on DB error, got %#v", got)
	}
	reqCancelled := httptest.NewRequest(http.MethodGet, "/Collections", nil).WithContext(
		middleware.WithAuthContext(cancelledCtx, &middleware.AuthContext{User: kid}),
	)
	if maxR, _ := h.userParentalPolicyForRequest(reqCancelled, kid.ID); maxR == nil || *maxR != 0 {
		t.Fatalf("expected fail-closed maxRating=0 on DB error, got %v", maxR)
	}
}

func TestCollectionsV0107AuditH3PlaylistNestingAndPrivateFilter(t *testing.T) {
	tmp := t.TempDir()
	st, err := sqlite.Open(filepath.Join(tmp, "col_v0107.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(st)
	userRepo := sqlite.NewUserRepo(st)
	cfg := config.Default(tmp)

	lib, _ := repo.CreateLibrary(ctx, "Movies", "movies", []string{tmp})
	movie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "Public Movie",
	}
	_ = repo.UpsertItem(ctx, movie)

	// Non-admin users WITH EnableAllFolders=true and no parental restrictions
	alice := &sqlite.UserRecord{ID: guid.New(), Name: "alice", Policy: sqlite.DefaultUserPolicy(false), Config: sqlite.DefaultUserConfig()}
	bob := &sqlite.UserRecord{ID: guid.New(), Name: "bob", Policy: sqlite.DefaultUserPolicy(false), Config: sqlite.DefaultUserConfig()}
	admin := &sqlite.UserRecord{ID: guid.New(), Name: "admin", IsAdmin: true, Policy: sqlite.DefaultUserPolicy(true), Config: sqlite.DefaultUserConfig()}
	alice.Policy.EnableAllFolders = true
	bob.Policy.EnableAllFolders = true
	for _, u := range []*sqlite.UserRecord{alice, bob, admin} {
		_ = userRepo.CreateUser(ctx, u)
	}

	bobPrivPl, _ := repo.CreatePlaylist(ctx, "BobPrivate", "Video", bob.ID, []string{movie.ID})
	aliceOwnPl, _ := repo.CreatePlaylist(ctx, "AliceOwn", "Video", alice.ID, []string{movie.ID})

	// Admin creates a BoxSet containing movie + bobPrivPl (edge case for handleGetContainerItems)
	box, _ := repo.CreateCollection(ctx, "MixedBox", "", []string{movie.ID, bobPrivPl.ID})

	h := New(repo, cfg)
	h.SetUserRepo(userRepo)
	h.SetStore(st)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	doAs := func(u *sqlite.UserRecord, method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req = req.WithContext(middleware.WithAuthContext(req.Context(), &middleware.AuthContext{User: u}))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// 1. [H-3] Alice (EnableAllFolders=true) creating playlist with bobPrivPl + aliceOwnPl + movie:
	//    only movie must be added; private playlist and nested playlist must be excluded.
	recCreate := doAs(alice, http.MethodPost, "/Playlists?Name=AliceNew&Ids="+bobPrivPl.ID+","+aliceOwnPl.ID+","+movie.ID, "")
	if recCreate.Code != http.StatusOK {
		t.Fatalf("create playlist got %d body=%q", recCreate.Code, recCreate.Body.String())
	}
	var created creationResult
	_ = json.Unmarshal(recCreate.Body.Bytes(), &created)

	recItems := doAs(alice, http.MethodGet, "/Playlists/"+created.Id+"/Items", "")
	var itemsRes dto.QueryResult[sqlite.PlaylistItemDto]
	_ = json.Unmarshal(recItems.Body.Bytes(), &itemsRes)
	if len(itemsRes.Items) != 1 || itemsRes.Items[0].Id != movie.ID {
		t.Fatalf("H-3 expected only movie in newly created playlist (no private/nested playlist), got %+v", itemsRes.Items)
	}

	// 2. [H-3] Adding bobPrivPl or nested aliceOwnPl to existing playlist is also rejected
	recAdd := doAs(alice, http.MethodPost, "/Playlists/"+created.Id+"/Items?Ids="+bobPrivPl.ID+","+aliceOwnPl.ID, "")
	if recAdd.Code != http.StatusOK {
		t.Fatalf("add playlist items got %d", recAdd.Code)
	}
	recItems2 := doAs(alice, http.MethodGet, "/Playlists/"+created.Id+"/Items", "")
	var itemsRes2 dto.QueryResult[sqlite.PlaylistItemDto]
	_ = json.Unmarshal(recItems2.Body.Bytes(), &itemsRes2)
	if len(itemsRes2.Items) != 1 {
		t.Fatalf("H-3 expected nested/private playlists not added, got %d items", len(itemsRes2.Items))
	}

	// 3. [H-3] handleGetContainerItems filters invisible Playlist child for non-admin with EnableAllFolders=true
	recBoxAlice := doAs(alice, http.MethodGet, "/Collections/"+box.ID+"/Items", "")
	if recBoxAlice.Code != http.StatusOK {
		t.Fatalf("alice GET BoxSet items got %d", recBoxAlice.Code)
	}
	var boxResAlice dto.QueryResult[sqlite.PlaylistItemDto]
	_ = json.Unmarshal(recBoxAlice.Body.Bytes(), &boxResAlice)
	for _, it := range boxResAlice.Items {
		if it.Id == bobPrivPl.ID {
			t.Fatalf("H-3 BoxSet items leaked bob's private playlist to alice!")
		}
	}

	// Admin still sees both items in BoxSet
	recBoxAdmin := doAs(admin, http.MethodGet, "/Collections/"+box.ID+"/Items", "")
	var boxResAdmin dto.QueryResult[sqlite.PlaylistItemDto]
	_ = json.Unmarshal(recBoxAdmin.Body.Bytes(), &boxResAdmin)
	if len(boxResAdmin.Items) != 2 {
		t.Fatalf("expected admin to see both items in BoxSet, got %d", len(boxResAdmin.Items))
	}
}

func TestV0109Audit_CreateCollectionFiltersPlaylistIDs(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "collections_v0109.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	libRepo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)
	authSvc := auth.NewService(userRepo, cfg)

	admin, err := authSvc.CreateUser(ctx, "admin", "adminpass", true)
	if err != nil {
		t.Fatal(err)
	}
	alice, err := authSvc.CreateUser(ctx, "alice", "alicepass", false)
	if err != nil {
		t.Fatal(err)
	}

	lib, _ := libRepo.CreateLibrary(ctx, "Movies", "movies", []string{tmp})
	movie := &sqlite.ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", MediaType: "Video", Name: "Inception"}
	if err := libRepo.UpsertItem(ctx, movie); err != nil {
		t.Fatal(err)
	}
	privPl, err := libRepo.CreatePlaylist(ctx, "Alice Secret Playlist", "Video", alice.ID, []string{movie.ID})
	if err != nil {
		t.Fatal(err)
	}

	h := New(libRepo, cfg)
	h.SetUserRepo(userRepo)
	h.SetStore(store)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	adminAC := &middleware.AuthContext{User: admin, Token: "admin-tok"}
	body := `{"Name":"Mixed Collection","Ids":["` + movie.ID + `","` + privPl.ID + `"]}`
	req := httptest.NewRequest(http.MethodPost, "/Collections", strings.NewReader(body))
	req = req.WithContext(middleware.WithAuthContext(req.Context(), adminAC))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /Collections expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var created creationResult
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	items, total, err := libRepo.GetContainerItems(ctx, cfg.ServerID, admin.ID, created.Id, 0, 50, nil)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 || items[0].Id != movie.ID {
		t.Fatalf("expected Playlist ID stripped from CreateCollection, got total=%d items=%+v", total, items)
	}
}
