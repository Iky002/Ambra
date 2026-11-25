package collections

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
