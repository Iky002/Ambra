package catalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestCatalogUserIdOwnershipEnforcement(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "cat_v097.db"))
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

	lib, _ := repo.CreateLibrary(ctx, "Movies", "movies", []string{tmp})
	m := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "Movie1", SortName: "movie1",
	}
	ch := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Channel", IsFolder: true, Name: "TestChannel", SortName: "testchannel",
	}
	_ = repo.UpsertItem(ctx, m)
	_ = repo.UpsertItem(ctx, ch)

	adminAuth, _ := authSvc.AuthenticateByName(ctx, "admin", "admin123", "", auth.ClientInfo{DeviceID: "d-admin"})
	aliceAuth, _ := authSvc.AuthenticateByName(ctx, "alice", "alice123", "", auth.ClientInfo{DeviceID: "d-alice"})

	authMW := middleware.NewAuth(authSvc, nil)
	mux := http.NewServeMux()
	h := New(repo, cfg)
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

	endpoints := []string{
		"/Items/Filters?UserId=" + bob.ID,
		"/Items/Filters2?UserId=" + bob.ID,
		"/Movies/Recommendations?UserId=" + bob.ID,
		"/Movies/" + m.ID + "/Similar?UserId=" + bob.ID,
		"/Albums/" + m.ID + "/InstantMix?UserId=" + bob.ID,
		"/Trailers?UserId=" + bob.ID,
		"/Channels/" + ch.ID + "/Items?UserId=" + bob.ID,
		"/Tags?UserId=" + bob.ID,
	}
	for _, ep := range endpoints {
		if rec := doGet(aliceAuth.AccessToken, ep); rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for alice GET %s, got %d", ep, rec.Code)
		}
		if rec := doGet(adminAuth.AccessToken, ep); rec.Code != http.StatusOK {
			t.Fatalf("expected 200 for admin GET %s, got %d", ep, rec.Code)
		}
	}

	// Self access succeeds
	if rec := doGet(aliceAuth.AccessToken, "/Items/Filters?UserId="+alice.ID); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for alice self /Items/Filters, got %d", rec.Code)
	}
}

func TestCatalogV0101SeedItemRBACAndParental(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "cat_v0101.db"))
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

	mSafe := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID, Type: "Movie", MediaType: "Video", Name: "Safe", SortName: "safe", OfficialRating: "PG"}
	mR := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID, Type: "Movie", MediaType: "Video", Name: "Adult", SortName: "adult", OfficialRating: "R"}
	mSecret := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libB.ID, ParentID: libB.ID, Type: "MusicAlbum", Name: "SecretAlbum", SortName: "secretalbum", OfficialRating: "G"}
	songTagged := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID, Type: "Audio", MediaType: "Audio", Name: "TaggedSong", SortName: "taggedsong", OfficialRating: "G"}
	for _, it := range []*sqlite.ItemRecord{mSafe, mR, mSecret, songTagged} {
		_ = repo.UpsertItem(ctx, it)
	}
	_ = repo.ReplaceItemTaxonomies(ctx, songTagged.ID, "tag", []string{"blocked"})

	adminAuth, _ := authSvc.AuthenticateByName(ctx, "admin", "admin123", "", auth.ClientInfo{DeviceID: "d-admin"})
	aliceAuth, _ := authSvc.AuthenticateByName(ctx, "alice", "alice123", "", auth.ClientInfo{DeviceID: "d-alice"})

	authMW := middleware.NewAuth(authSvc, nil)
	mux := http.NewServeMux()
	h := New(repo, cfg)
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

	blockedPaths := []string{
		"/Movies/" + mR.ID + "/Similar",
		"/Albums/" + mSecret.ID + "/Similar",
		"/Albums/" + mSecret.ID + "/InstantMix",
		"/Songs/" + songTagged.ID + "/InstantMix",
	}
	for _, p := range blockedPaths {
		if rec := doGet(aliceAuth.AccessToken, p); rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for alice GET %s, got %d", p, rec.Code)
		}
		if rec := doGet(adminAuth.AccessToken, p); rec.Code != http.StatusOK {
			t.Fatalf("expected 200 for admin GET %s, got %d", p, rec.Code)
		}
	}

	// Allowed seed succeeds for alice
	if rec := doGet(aliceAuth.AccessToken, "/Movies/"+mSafe.ID+"/Similar"); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for alice GET safe Similar, got %d", rec.Code)
	}
}

func TestCatalogV0104PlaylistIDORChannelValidationSanitizeAndFailClosed(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "cat_v0104.db"))
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

	lib, _ := repo.CreateLibrary(ctx, "Media", "movies", []string{tmp})
	m1 := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "AlphaMovie", SortName: "alphamovie",
		Path: "/srv/secret/alpha.mkv",
	}
	m2 := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "BetaMovie", SortName: "betamovie",
		Path: "/srv/secret/beta.mkv",
	}
	trailer := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: m1.ID,
		Type: "Trailer", MediaType: "Video", Name: "AlphaTrailer", SortName: "alphatrailer",
		Path: "/srv/secret/trailer.mkv",
	}
	extraTrailer := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: m1.ID,
		Type: "Video", MediaType: "Video", Name: "ExtraTrailer", SortName: "extratrailer",
		Path: "/srv/secret/extra_trailer.mkv", Extra: sqlite.ItemExtra{ExtraType: "Trailer"},
	}
	game := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Game", MediaType: "Game", Name: "SpaceGame", SortName: "spacegame",
		Path: "/srv/secret/game.rom",
	}
	ch := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Channel", IsFolder: true, Name: "NewsCh", SortName: "newsch",
	}
	chItem := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: ch.ID,
		Type: "Video", MediaType: "Video", Name: "NewsClip", SortName: "newsclip",
		Path: "/srv/secret/news.mkv",
	}
	song := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Audio", MediaType: "Audio", Name: "Song1", SortName: "song1",
		Path: "/srv/secret/song1.mp3",
	}
	for _, rec := range []*sqlite.ItemRecord{m1, m2, trailer, extraTrailer, game, ch, chItem, song} {
		if err := repo.UpsertItem(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}
	_ = repo.ReplaceItemTaxonomies(ctx, m1.ID, "genre", []string{"Sci-Fi"})
	_ = repo.ReplaceItemTaxonomies(ctx, m2.ID, "genre", []string{"Sci-Fi"})
	_ = repo.ReplaceItemTaxonomies(ctx, song.ID, "genre", []string{"Rock"})

	// Bob's private playlist and public playlist
	bobPrivPl, err := repo.CreatePlaylist(ctx, "BobPrivate", "Audio", bob.ID, []string{song.ID})
	if err != nil {
		t.Fatal(err)
	}
	bobPubPl, err := repo.CreatePlaylist(ctx, "BobPublic", "Audio", bob.ID, []string{song.ID})
	if err != nil {
		t.Fatal(err)
	}
	_ = userRepo.SetServerConfig(ctx, "item_visibility_"+bobPubPl.ID, `{"IsPublic":true}`)

	adminAuth, _ := authSvc.AuthenticateByName(ctx, "admin", "admin123", "", auth.ClientInfo{DeviceID: "d-adm"})
	aliceAuth, _ := authSvc.AuthenticateByName(ctx, "alice", "alice123", "", auth.ClientInfo{DeviceID: "d-ali"})

	authMW := middleware.NewAuth(authSvc, nil)
	mux := http.NewServeMux()
	h := New(repo, cfg)
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

	doGet := func(tok, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-Emby-Token", tok)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// 1. [H-02] GET /Channels/{Id}/Items MUST reject UserId, Playlist ID, Movie ID with 404
	for _, fakeID := range []string{bob.ID, bobPrivPl.ID, m1.ID, guid.New()} {
		rec := doGet(aliceAuth.AccessToken, "/Channels/"+fakeID+"/Items")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("GET /Channels/%s/Items expected 404 for non-Channel ID, got %d (%s)", fakeID, rec.Code, rec.Body.String())
		}
	}
	// Real Channel succeeds for alice
	rec := doGet(aliceAuth.AccessToken, "/Channels/"+ch.ID+"/Items?Fields=Path")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /Channels/%s/Items expected 200, got %d", ch.ID, rec.Code)
	}
	if strings.Contains(rec.Body.String(), "/srv/secret/") {
		t.Fatalf("non-admin /Channels/{Id}/Items leaked path: %s", rec.Body.String())
	}

	// 2. [H-02] Private playlist IDOR blocked on InstantMix & Similar for alice; public playlist & admin allowed
	for _, ep := range []string{
		"/Albums/" + bobPrivPl.ID + "/InstantMix",
		"/Songs/" + bobPrivPl.ID + "/InstantMix",
		"/Movies/" + bobPrivPl.ID + "/Similar",
		"/Albums/" + bobPrivPl.ID + "/Similar",
	} {
		if r := doGet(aliceAuth.AccessToken, ep); r.Code != http.StatusForbidden {
			t.Fatalf("alice GET %s on bob private playlist expected 403, got %d (%s)", ep, r.Code, r.Body.String())
		}
		if r := doGet(adminAuth.AccessToken, ep); r.Code != http.StatusOK {
			t.Fatalf("admin GET %s expected 200, got %d", ep, r.Code)
		}
	}
	if r := doGet(aliceAuth.AccessToken, "/Albums/"+bobPubPl.ID+"/InstantMix"); r.Code != http.StatusOK {
		t.Fatalf("alice GET public playlist InstantMix expected 200, got %d", r.Code)
	}

	// 3. [MH-05] Non-admin path sanitization across all 6 catalog item endpoints
	for _, ep := range []string{
		"/Movies/Recommendations?Fields=Path&CategoryLimit=5&ItemLimit=8",
		"/Movies/" + m1.ID + "/Similar?Fields=Path",
		"/Songs/" + song.ID + "/InstantMix?Fields=Path",
		"/Trailers?Fields=Path&SearchTerm=Trailer&StartIndex=0&Limit=10",
		"/Games?Fields=Path",
		"/Channels/" + ch.ID + "/Items?Fields=Path",
	} {
		r := doGet(aliceAuth.AccessToken, ep)
		if r.Code != http.StatusOK {
			t.Fatalf("alice GET %s expected 200, got %d (%s)", ep, r.Code, r.Body.String())
		}
		if strings.Contains(r.Body.String(), "/srv/secret/") {
			t.Fatalf("non-admin GET %s leaked server path: %s", ep, r.Body.String())
		}
	}
	// Admin still receives Path when requested
	rAdm := doGet(adminAuth.AccessToken, "/Games?Fields=Path")
	if rAdm.Code != http.StatusOK || !strings.Contains(rAdm.Body.String(), "/srv/secret/game.rom") {
		t.Fatalf("admin GET /Games expected path preserved, got %d %s", rAdm.Code, rAdm.Body.String())
	}

	// 4. [MH-08] Fail-closed on unknown user or cancelled DB context + helper edge cases
	if got := h.allowedLibrariesForUser(ctx, guid.New()); got == nil || len(got) != 0 {
		t.Fatalf("expected fail-closed empty slice for unknown user, got %#v", got)
	}
	cancelledCtx, cancel := context.WithCancel(ctx)
	cancel()
	if got := h.allowedLibrariesForUser(cancelledCtx, alice.ID); got == nil || len(got) != 0 {
		t.Fatalf("expected fail-closed empty slice on DB error, got %#v", got)
	}
	reqCancelled := httptest.NewRequest(http.MethodGet, "/Games", nil).WithContext(
		middleware.WithAuthContext(cancelledCtx, &middleware.AuthContext{User: alice}),
	)
	if maxR, _ := h.userParentalPolicyForRequest(reqCancelled, alice.ID); maxR == nil || *maxR != 0 {
		t.Fatalf("expected fail-closed maxRating=0 on DB error, got %v", maxR)
	}
	if h.isPlaylistPublic(ctx, "") {
		t.Fatal("expected false for empty playlist ID")
	}
	_ = userRepo.SetServerConfig(ctx, "item_visibility_"+bobPrivPl.ID, `{corrupt`)
	if h.isPlaylistPublic(ctx, bobPrivPl.ID) {
		t.Fatal("expected false for corrupt visibility JSON")
	}

	// Extra bounds coverage for Recommendations and Trailers pagination
	if r := doGet(aliceAuth.AccessToken, "/Movies/Recommendations?CategoryLimit=0&ItemLimit=0"); r.Code != http.StatusOK {
		t.Fatalf("Recommendations zero bounds got %d", r.Code)
	}
	if r := doGet(aliceAuth.AccessToken, "/Movies/Recommendations?CategoryLimit=999&ItemLimit=999"); r.Code != http.StatusOK {
		t.Fatalf("Recommendations high bounds got %d", r.Code)
	}
	if r := doGet(aliceAuth.AccessToken, "/Trailers?StartIndex=-1&Limit=999"); r.Code != http.StatusOK {
		t.Fatalf("Trailers bounds got %d", r.Code)
	}
	if r := doGet(aliceAuth.AccessToken, "/Trailers?StartIndex=999&Limit=5"); r.Code != http.StatusOK {
		t.Fatalf("Trailers past-end got %d", r.Code)
	}
	// UserIDFromCtx-only path (no AuthContext)
	muxUID := http.NewServeMux()
	hUID := New(repo, cfg)
	hUID.SetUserRepo(userRepo)
	hUID.RegisterRoutes(muxUID, AuthFuncs{
		UserIDFromCtx: func(context.Context) string { return alice.ID },
	})
	recUID := httptest.NewRecorder()
	muxUID.ServeHTTP(recUID, httptest.NewRequest(http.MethodGet, "/Albums/"+bobPrivPl.ID+"/InstantMix", nil))
	if recUID.Code != http.StatusForbidden {
		t.Fatalf("UserIDFromCtx-only alice InstantMix on bob private playlist expected 403, got %d", recUID.Code)
	}
	// UserIDFromCtx-only admin & cross-user canAccessUserData checks
	adminRec, _ := userRepo.GetUserByName(ctx, "admin")
	hAdmUID := New(repo, cfg)
	hAdmUID.SetUserRepo(userRepo)
	hAdmUID.auth.UserIDFromCtx = func(context.Context) string { return adminRec.ID }
	if !hAdmUID.canAccessUserData(httptest.NewRequest(http.MethodGet, "/", nil), bob.ID) {
		t.Fatal("expected admin via UserIDFromCtx to access bob data")
	}
	if hAdmUID.isNonAdminCaller(httptest.NewRequest(http.MethodGet, "/", nil)) {
		t.Fatal("expected admin via UserIDFromCtx not to be non-admin")
	}
	if !hAdmUID.canViewPlaylist(httptest.NewRequest(http.MethodGet, "/", nil), bobPrivPl.ID, bob.ID) {
		t.Fatal("expected admin via UserIDFromCtx to view private playlist")
	}
	if hUID.canAccessUserData(httptest.NewRequest(http.MethodGet, "/", nil), bob.ID) {
		t.Fatal("expected alice via UserIDFromCtx to be denied bob data")
	}
	if !hUID.canAccessUserData(httptest.NewRequest(http.MethodGet, "/", nil), alice.ID) {
		t.Fatal("expected alice via UserIDFromCtx to access own data")
	}
	// Forbidden channel access when user's EnabledFolders excludes the channel's library
	aliceRestricted := alice.Policy
	aliceRestricted.EnableAllFolders = false
	aliceRestricted.EnabledFolders = []string{guid.New()}
	_ = userRepo.UpdateUserPolicySafe(ctx, alice.ID, aliceRestricted)
	authSvc.InvalidateUserCache(alice.ID)
	if r := doGet(aliceAuth.AccessToken, "/Channels/"+ch.ID+"/Items"); r.Code != http.StatusForbidden {
		t.Fatalf("restricted alice GET /Channels/%s/Items expected 403, got %d", ch.ID, r.Code)
	}
}
