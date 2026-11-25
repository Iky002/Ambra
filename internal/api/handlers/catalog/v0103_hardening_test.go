package catalog

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

func TestCatalogV0103LimitsAndLocalTrailers(t *testing.T) {
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
	adminAuth, _ := authSvc.AuthenticateByName(ctx, "admin", "admin123", "", auth.ClientInfo{DeviceID: "d0"})

	lib, _ := repo.CreateLibrary(ctx, "Movies", "movies", []string{"/movies"})
	parentMovie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "Blockbuster", SortName: "blockbuster",
	}
	standaloneTrailer := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Trailer", MediaType: "Video", Name: "Standalone Trailer", SortName: "standalone trailer",
	}
	localExtraTrailer := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: parentMovie.ID,
		Type: "Video", MediaType: "Video", Name: "Blockbuster Local Trailer", SortName: "blockbuster local trailer",
		Extra: sqlite.ItemExtra{ExtraType: "Trailer"},
	}
	_ = repo.UpsertItem(ctx, parentMovie)
	_ = repo.UpsertItem(ctx, standaloneTrailer)
	_ = repo.UpsertItem(ctx, localExtraTrailer)

	h := New(repo, cfg)
	h.SetUserRepo(userRepo)
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

	// 1. MovieRecommendations clamps excessive CategoryLimit/ItemLimit safely
	reqRec := httptest.NewRequest(http.MethodGet, "/Movies/Recommendations?CategoryLimit=9999&ItemLimit=9999", nil)
	reqRec.Header.Set("X-Emby-Token", adminAuth.AccessToken)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, reqRec)
	if rec.Code != http.StatusOK {
		t.Fatalf("Recommendations with huge limits got %d: %s", rec.Code, rec.Body.String())
	}

	// 2. GET /Trailers returns BOTH standalone Type=Trailer AND local ExtraType=Trailer items
	reqTr := httptest.NewRequest(http.MethodGet, "/Trailers", nil)
	reqTr.Header.Set("X-Emby-Token", adminAuth.AccessToken)
	recTr := httptest.NewRecorder()
	mux.ServeHTTP(recTr, reqTr)
	if recTr.Code != http.StatusOK {
		t.Fatalf("GET /Trailers got %d: %s", recTr.Code, recTr.Body.String())
	}
	var trQR dto.QueryResult[dto.BaseItemDto]
	if err := json.Unmarshal(recTr.Body.Bytes(), &trQR); err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, it := range trQR.Items {
		found[it.Id] = true
	}
	if !found[standaloneTrailer.ID] || !found[localExtraTrailer.ID] {
		t.Fatalf("GET /Trailers (admin=%s) must return both standalone and extra_type=trailer items, got %+v", admin.ID, found)
	}
}
