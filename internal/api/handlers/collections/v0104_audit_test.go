package collections

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
