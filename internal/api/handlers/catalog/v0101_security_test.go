package catalog

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
