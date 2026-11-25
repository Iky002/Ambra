package library

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

func TestLibraryV0104PlaylistIDORAndFailClosed(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "lib_v0104.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)
	authSvc := auth.NewService(userRepo, cfg)

	admin, _ := authSvc.CreateUser(ctx, "admin", "admin123", true)
	alice, _ := authSvc.CreateUser(ctx, "alice", "alice123", false)
	bob, _ := authSvc.CreateUser(ctx, "bob", "bob123", false)

	// Bob creates a private playlist (EnableAllFolders is true for alice by default)
	bobPl, err := repo.CreatePlaylist(ctx, "BobSecret", "Audio", bob.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Public playlist owned by bob
	bobPubPl, err := repo.CreatePlaylist(ctx, "BobPublic", "Audio", bob.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = userRepo.SetServerConfig(ctx, "item_visibility_"+bobPubPl.ID, `{"IsPublic":true}`)

	adminAuth, _ := authSvc.AuthenticateByName(ctx, "admin", "admin123", "", auth.ClientInfo{DeviceID: "d-adm"})
	aliceAuth, _ := authSvc.AuthenticateByName(ctx, "alice", "alice123", "", auth.ClientInfo{DeviceID: "d-ali"})

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

	doReq := func(tok, method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		if tok != "" {
			req.Header.Set("X-Emby-Token", tok)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// 1. [H-02] Alice MUST NOT mark/favorite Bob's private playlist (canAccessItem enforces canViewPlaylist)
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/Users/" + alice.ID + "/PlayedItems/" + bobPl.ID},
		{http.MethodDelete, "/Users/" + alice.ID + "/PlayedItems/" + bobPl.ID},
		{http.MethodPost, "/Users/" + alice.ID + "/FavoriteItems/" + bobPl.ID},
		{http.MethodDelete, "/Users/" + alice.ID + "/FavoriteItems/" + bobPl.ID},
	} {
		rec := doReq(aliceAuth.AccessToken, tc.method, tc.path)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("alice %s %s on bob private playlist expected 403, got %d (%s)", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}

	// Public playlist & admin access succeed
	if rec := doReq(aliceAuth.AccessToken, http.MethodPost, "/Users/"+alice.ID+"/FavoriteItems/"+bobPubPl.ID); rec.Code != http.StatusOK {
		t.Fatalf("alice favorite bob public playlist expected 200, got %d", rec.Code)
	}
	if rec := doReq(adminAuth.AccessToken, http.MethodPost, "/Users/"+admin.ID+"/FavoriteItems/"+bobPl.ID); rec.Code != http.StatusOK {
		t.Fatalf("admin favorite bob private playlist expected 200, got %d", rec.Code)
	}

	// 2. [MH-08] Fail-closed on unknown user or cancelled DB context
	if got := h.allowedLibrariesForUser(ctx, guid.New()); got == nil || len(got) != 0 {
		t.Fatalf("expected non-nil empty slice (fail-closed) for unknown user, got %#v", got)
	}
	cancelledCtx, cancel := context.WithCancel(ctx)
	cancel()
	if got := h.allowedLibrariesForUser(cancelledCtx, alice.ID); got == nil || len(got) != 0 {
		t.Fatalf("expected fail-closed empty slice on DB error, got %#v", got)
	}
	reqCancelled := httptest.NewRequest(http.MethodGet, "/Items", nil).WithContext(
		middleware.WithAuthContext(cancelledCtx, &middleware.AuthContext{User: alice}),
	)
	maxR, _ := h.userParentalPolicyForRequest(reqCancelled, alice.ID)
	if maxR == nil || *maxR != 0 {
		t.Fatalf("expected fail-closed maxRating=0 on DB error, got %v", maxR)
	}
}
