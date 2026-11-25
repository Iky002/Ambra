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
