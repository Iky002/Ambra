package catalog

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestAuditV0112_CatalogLiveTvAndExtendedPolicySQLPushdown(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "data")
	movDir := filepath.Join(tmp, "movies")
	liveDir := filepath.Join(tmp, "livetv")
	for _, d := range []string{dataDir, movDir, liveDir} {
		_ = os.MkdirAll(d, 0o755)
	}

	store, err := sqlite.Open(filepath.Join(dataDir, "cat_v0112.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(dataDir)

	movLib, _ := repo.CreateLibrary(ctx, "Movies", "movies", []string{movDir})
	liveLib, _ := repo.CreateLibrary(ctx, "LiveTV", "livetv", []string{liveDir})

	yr2022, yr2023 := 2022, 2023
	movOK := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: movLib.ID, ParentID: movLib.ID,
		Type: "Movie", MediaType: "Video", Name: "Kid Safe", SortName: "kid safe",
		OfficialRating: "G", ProductionYear: &yr2022,
	}
	movOK2 := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: movLib.ID, ParentID: movLib.ID,
		Type: "Movie", MediaType: "Video", Name: "Kid Safe 2", SortName: "kid safe 2",
		OfficialRating: "PG", ProductionYear: &yr2022,
	}
	movUnrated := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: movLib.ID, ParentID: movLib.ID,
		Type: "Movie", MediaType: "Video", Name: "Unrated Secret", SortName: "unrated secret",
		ProductionYear: &yr2023,
	}
	liveProg := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: liveLib.ID, ParentID: liveLib.ID,
		Type: "Program", MediaType: "Video", Name: "Live News", SortName: "live news",
		OfficialRating: "G", ProductionYear: &yr2023,
	}
	for _, it := range []*sqlite.ItemRecord{movOK, movOK2, movUnrated, liveProg} {
		_ = repo.UpsertItem(ctx, it)
	}
	_ = repo.ReplaceItemTaxonomies(ctx, movOK.ID, "tag", []string{"kid-tag"})
	_ = repo.ReplaceItemTaxonomies(ctx, movOK2.ID, "tag", []string{"kid-tag"})
	_ = repo.ReplaceItemTaxonomies(ctx, movUnrated.ID, "tag", []string{"kid-tag"})
	_ = repo.ReplaceItemTaxonomies(ctx, liveProg.ID, "tag", []string{"kid-tag"})

	_ = repo.ReplaceItemTaxonomies(ctx, movOK.ID, "genre", []string{"Family"})
	_ = repo.ReplaceItemTaxonomies(ctx, movOK2.ID, "genre", []string{"Family"})
	_ = repo.ReplaceItemTaxonomies(ctx, movUnrated.ID, "genre", []string{"UnratedGenre"})
	_ = repo.ReplaceItemTaxonomies(ctx, liveProg.ID, "genre", []string{"LiveGenre"})

	maxPG := 5
	kidPol := dto.UserPolicy{
		EnableAllFolders:   true,
		EnableLiveTvAccess: false,
		MaxParentalRating:  &maxPG,
		BlockUnratedItems:  []string{"Movie"},
		AllowedTags:        []string{"kid-tag"},
	}
	kid := &sqlite.UserRecord{ID: guid.New(), Name: "kid_cat", Policy: kidPol, Config: sqlite.DefaultUserConfig()}
	_ = userRepo.CreateUser(ctx, kid)

	h := New(repo, cfg)
	h.SetUserRepo(userRepo)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{
		UserIDFromCtx: func(c context.Context) string {
			if ac, ok := middleware.AuthFromContext(c); ok && ac != nil {
				return ac.UserID()
			}
			return ""
		},
	})

	doGet := func(target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req = req.WithContext(middleware.WithAuthContext(req.Context(), &middleware.AuthContext{User: kid}))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// 1. /Items/Filters excludes UnratedGenre and LiveGenre and year 2023 directly in SQL!
	fRec := doGet("/Items/Filters")
	if fRec.Code != http.StatusOK {
		t.Fatalf("Filters status=%d", fRec.Code)
	}
	var qf queryFiltersLegacyResponse
	_ = json.Unmarshal(fRec.Body.Bytes(), &qf)
	if len(qf.Genres) != 1 || qf.Genres[0] != "Family" || len(qf.Years) != 1 || qf.Years[0] != 2022 {
		t.Fatalf("expected only Family / 2022 in /Items/Filters, got %+v", qf)
	}

	// 2. /Movies/{Id}/Similar on liveProg or movUnrated returns 403; on movOK returns only movOK2!
	if r := doGet("/Movies/" + liveProg.ID + "/Similar"); r.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on LiveTV item /Movies/{Id}/Similar when EnableLiveTvAccess=false, got %d", r.Code)
	}
	if r := doGet("/Movies/" + movUnrated.ID + "/Similar"); r.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on unrated item /Movies/{Id}/Similar, got %d", r.Code)
	}
	simRec := doGet("/Movies/" + movOK.ID + "/Similar?Limit=999999")
	var simRes dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(simRec.Body.Bytes(), &simRes)
	if simRes.TotalRecordCount != 1 || len(simRes.Items) != 1 || guid.Normalize(simRes.Items[0].Id) != guid.Normalize(movOK2.ID) {
		t.Fatalf("expected only movOK2 in /Movies/{Id}/Similar, got %+v", simRes)
	}
}
