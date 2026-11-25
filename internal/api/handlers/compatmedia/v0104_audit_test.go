package compatmedia

import (
	"context"
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

func TestCompatMediaV0104PlaylistIDORAndFailClosed(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "cm_v0104.db"))
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

	maxPG := 5
	alicePol := alice.Policy
	alicePol.MaxParentalRating = &maxPG
	_ = userRepo.UpdateUserPolicySafe(ctx, alice.ID, alicePol)

	lib, _ := repo.CreateLibrary(ctx, "Music", "music", []string{tmp})
	song := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Audio", MediaType: "Audio", Name: "Track1", OfficialRating: "G",
	}
	_ = repo.UpsertItem(ctx, song)
	_ = repo.ReplaceItemTaxonomies(ctx, song.ID, "genre", []string{"Jazz"})

	// Bob's private & public playlists
	bobPrivPl, _ := repo.CreatePlaylist(ctx, "BobSecretMix", "Audio", bob.ID, []string{song.ID})
	bobPubPl, _ := repo.CreatePlaylist(ctx, "BobPublicMix", "Audio", bob.ID, []string{song.ID})
	_ = userRepo.SetServerConfig(ctx, "item_visibility_"+bobPubPl.ID, `{"IsPublic":true}`)

	// R-rated BoxSet containing G-rated song
	rBox, _ := repo.CreateCollection(ctx, "RBox", "", []string{song.ID})
	rBox.OfficialRating = "R"
	_ = repo.UpsertItem(ctx, rBox)

	adminAuth, _ := authSvc.AuthenticateByName(ctx, "admin", "admin123", "", auth.ClientInfo{DeviceID: "d-adm"})
	aliceAuth, _ := authSvc.AuthenticateByName(ctx, "alice", "alice123", "", auth.ClientInfo{DeviceID: "d-ali"})

	authMW := middleware.NewAuth(authSvc, nil)
	h := New(repo, nil, nil, nil, store, cfg)
	h.SetUserRepo(userRepo)
	router := h.BuildRouter(AuthFuncs{
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
		router.ServeHTTP(rec, req)
		return rec
	}

	// 1. [H-02] Private playlist blocked on /Items/{Id}/InstantMix, /Items/{Id}/Similar, /Artists/InstantMix?Id=...
	for _, ep := range []string{
		"/Items/" + bobPrivPl.ID + "/InstantMix",
		"/Items/" + bobPrivPl.ID + "/Similar",
		"/Artists/InstantMix?Id=" + bobPrivPl.ID,
		"/Items/" + bobPrivPl.ID + "/Ancestors",
	} {
		if r := doGet(aliceAuth.AccessToken, ep); r.Code != http.StatusForbidden {
			t.Fatalf("alice GET %s on bob private playlist expected 403, got %d (%s)", ep, r.Code, r.Body.String())
		}
		if r := doGet(adminAuth.AccessToken, ep); r.Code != http.StatusOK {
			t.Fatalf("admin GET %s expected 200, got %d", ep, r.Code)
		}
	}
	// Public playlist allowed
	if r := doGet(aliceAuth.AccessToken, "/Items/"+bobPubPl.ID+"/InstantMix"); r.Code != http.StatusOK {
		t.Fatalf("alice GET public playlist InstantMix expected 200, got %d", r.Code)
	}

	// 2. BoxSet container parental check on /Collections/{Id}/ProviderItems
	if r := doGet(aliceAuth.AccessToken, "/Collections/"+rBox.ID+"/ProviderItems"); r.Code != http.StatusForbidden {
		t.Fatalf("alice GET R-rated BoxSet ProviderItems expected 403, got %d", r.Code)
	}
	if r := doGet(adminAuth.AccessToken, "/Collections/"+rBox.ID+"/ProviderItems"); r.Code != http.StatusOK {
		t.Fatalf("admin GET R-rated BoxSet ProviderItems expected 200, got %d", r.Code)
	}

	// 3. [MH-08] Fail-closed on unknown user or DB error
	if got := h.allowedLibrariesForUser(ctx, guid.New()); got == nil || len(got) != 0 {
		t.Fatalf("expected fail-closed empty slice for unknown user, got %#v", got)
	}
	cancelledCtx, cancel := context.WithCancel(ctx)
	cancel()
	if got := h.allowedLibrariesForUser(cancelledCtx, alice.ID); got == nil || len(got) != 0 {
		t.Fatalf("expected fail-closed empty slice on DB error, got %#v", got)
	}
	reqCancelled := httptest.NewRequest(http.MethodGet, "/Items/"+song.ID+"/InstantMix", nil).WithContext(
		middleware.WithAuthContext(cancelledCtx, &middleware.AuthContext{User: alice}),
	)
	if maxR, _ := h.userParentalPolicyForRequest(reqCancelled, alice.ID); maxR == nil || *maxR != 0 {
		t.Fatalf("expected fail-closed maxRating=0 on DB error, got %v", maxR)
	}
	if h.canAccessItemID(reqCancelled, song.ID) {
		t.Fatal("expected canAccessItemID fail-closed (false) on DB error")
	}
	if h.canDownloadContent(reqCancelled) {
		t.Fatal("expected canDownloadContent fail-closed (false) on DB error")
	}
}
