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
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestCatalogItemsFiltersAndRBAC(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "cat_rbac.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)

	libKids, _ := repo.CreateLibrary(ctx, "Kids", "movies", []string{filepath.Join(tmp, "kids")})
	libAdult, _ := repo.CreateLibrary(ctx, "Adult", "movies", []string{filepath.Join(tmp, "adult")})

	y2021 := 2021
	y2023 := 2023
	mKids := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      libKids.ID,
		ParentID:       libKids.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Sunny Day",
		OfficialRating: "G",
		ProductionYear: &y2021,
	}
	mKidsR := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      libKids.ID,
		ParentID:       libKids.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Dark Night",
		OfficialRating: "R",
		ProductionYear: &y2023,
	}
	mAdult := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      libAdult.ID,
		ParentID:       libAdult.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Secret Thriller",
		OfficialRating: "PG",
		ProductionYear: &y2023,
	}
	trailerBlocked := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      libKids.ID,
		ParentID:       libKids.ID,
		Type:           "Trailer",
		MediaType:      "Video",
		Name:           "Horror Trailer",
		OfficialRating: "PG",
	}
	songAllowed := &sqlite.ItemRecord{
		ID:        guid.New(),
		LibraryID: libKids.ID,
		ParentID:  libKids.ID,
		Type:      "Audio",
		MediaType: "Audio",
		Name:      "Happy Song",
	}
	songBlocked := &sqlite.ItemRecord{
		ID:        guid.New(),
		LibraryID: libAdult.ID,
		ParentID:  libAdult.ID,
		Type:      "Audio",
		MediaType: "Audio",
		Name:      "Restricted Song",
	}

	for _, it := range []*sqlite.ItemRecord{mKids, mKidsR, mAdult, trailerBlocked, songAllowed, songBlocked} {
		if err := repo.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
		_ = repo.ReplaceItemAncestors(ctx, it.ID, []sqlite.AncestorLink{{AncestorID: it.LibraryID, Depth: 1}})
	}
	_ = repo.ReplaceItemTaxonomies(ctx, mKids.ID, "genre", []string{"Animation"})
	_ = repo.ReplaceItemTaxonomies(ctx, mKids.ID, "studio", []string{"Pixar"})
	_ = repo.ReplaceItemTaxonomies(ctx, mKids.ID, "tag", []string{"Family"})

	_ = repo.ReplaceItemTaxonomies(ctx, mKidsR.ID, "genre", []string{"Animation", "Slasher"})
	_ = repo.ReplaceItemTaxonomies(ctx, mKidsR.ID, "studio", []string{"RStudio"})
	_ = repo.ReplaceItemTaxonomies(ctx, mKidsR.ID, "tag", []string{"Violent"})

	_ = repo.ReplaceItemTaxonomies(ctx, mAdult.ID, "genre", []string{"Thriller"})
	_ = repo.ReplaceItemTaxonomies(ctx, mAdult.ID, "studio", []string{"AdultStudio"})
	_ = repo.ReplaceItemTaxonomies(ctx, mAdult.ID, "tag", []string{"SecretTag"})

	_ = repo.ReplaceItemTaxonomies(ctx, trailerBlocked.ID, "tag", []string{"BlockedTag"})
	_ = repo.ReplaceItemTaxonomies(ctx, songAllowed.ID, "genre", []string{"Pop"})
	_ = repo.ReplaceItemTaxonomies(ctx, songBlocked.ID, "genre", []string{"Pop"})

	maxPG := 5
	kidPolicy := sqlite.DefaultUserPolicy(false)
	kidPolicy.EnableAllFolders = false
	kidPolicy.EnabledFolders = []string{libKids.ID}
	kidPolicy.MaxParentalRating = &maxPG
	kidPolicy.BlockedTags = []string{"blockedtag"}
	kid := &sqlite.UserRecord{
		ID:     guid.New(),
		Name:   "kid",
		Policy: kidPolicy,
		Config: sqlite.DefaultUserConfig(),
	}
	if err := userRepo.CreateUser(ctx, kid); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	h := New(repo, cfg)
	h.SetUserRepo(userRepo)
	h.RegisterRoutes(mux, AuthFuncs{
		UserIDFromCtx: func(c context.Context) string {
			if ac, ok := middleware.AuthFromContext(c); ok && ac != nil {
				return ac.UserID()
			}
			return ""
		},
	})

	kidAC := &middleware.AuthContext{User: kid}
	doGet := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req = req.WithContext(middleware.WithAuthContext(req.Context(), kidAC))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// 1. GET /Items/Filters respects AllowedLibraryIDs, MaxParentalRating, BlockedTags
	rec := doGet("/Items/Filters?IncludeItemTypes=Movie")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /Items/Filters status=%d body=%s", rec.Code, rec.Body.String())
	}
	var f1 queryFiltersLegacyResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &f1); err != nil {
		t.Fatal(err)
	}
	if len(f1.Genres) != 1 || f1.Genres[0] != "Animation" {
		t.Fatalf("Filters Genres = %v, want [Animation]", f1.Genres)
	}
	if len(f1.Tags) != 1 || f1.Tags[0] != "Family" {
		t.Fatalf("Filters Tags = %v, want [Family]", f1.Tags)
	}
	if len(f1.OfficialRatings) != 1 || f1.OfficialRatings[0] != "G" {
		t.Fatalf("Filters OfficialRatings = %v, want [G]", f1.OfficialRatings)
	}
	if len(f1.Years) != 1 || f1.Years[0] != 2021 {
		t.Fatalf("Filters Years = %v, want [2021]", f1.Years)
	}

	// 2. GET /Items/Filters2 returns NameIdPair for Genres and Studios
	rec = doGet("/Items/Filters2?IncludeItemTypes=Movie&ParentId=" + libKids.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /Items/Filters2 status=%d body=%s", rec.Code, rec.Body.String())
	}
	var f2 queryFilters2Response
	if err := json.Unmarshal(rec.Body.Bytes(), &f2); err != nil {
		t.Fatal(err)
	}
	if len(f2.Genres) != 1 || f2.Genres[0].Name != "Animation" || f2.Genres[0].Id == "" {
		t.Fatalf("Filters2 Genres = %+v", f2.Genres)
	}
	if len(f2.Studios) != 1 || f2.Studios[0].Name != "Pixar" || f2.Studios[0].Id == "" {
		t.Fatalf("Filters2 Studios = %+v", f2.Studios)
	}

	// 3. /Movies/{Id}/Similar excludes mKidsR (R rating) and mAdult (restricted lib)
	rec = doGet("/Movies/" + mKids.ID + "/Similar")
	var simRes dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &simRes)
	for _, it := range simRes.Items {
		if it.Id == mKidsR.ID || it.Id == mAdult.ID {
			t.Fatalf("Similar leaked blocked item %s", it.Name)
		}
	}

	// 4. /Movies/Recommendations excludes mKidsR and mAdult
	rec = doGet("/Movies/Recommendations")
	var recCats []sqlite.RecommendationCategory
	_ = json.Unmarshal(rec.Body.Bytes(), &recCats)
	for _, cat := range recCats {
		for _, it := range cat.Items {
			if it.Id == mKidsR.ID || it.Id == mAdult.ID {
				t.Fatalf("Recommendations leaked blocked item %s", it.Name)
			}
		}
	}

	// 5. /Songs/{Id}/InstantMix excludes songBlocked
	rec = doGet("/Songs/" + songAllowed.ID + "/InstantMix")
	var mixRes dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &mixRes)
	for _, it := range mixRes.Items {
		if it.Id == songBlocked.ID {
			t.Fatalf("InstantMix leaked restricted song")
		}
	}

	// 6. /Trailers excludes trailerBlocked (BlockedTags)
	rec = doGet("/Trailers")
	var trRes dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &trRes)
	if trRes.TotalRecordCount != 0 {
		t.Fatalf("expected 0 trailers for kid, got %d", trRes.TotalRecordCount)
	}

	// 7. /Tags and /OfficialRatings exclude blocked/restricted values
	rec = doGet("/Tags")
	var tagsRes dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &tagsRes)
	for _, it := range tagsRes.Items {
		if it.Name == "SecretTag" || it.Name == "Violent" || it.Name == "BlockedTag" {
			t.Fatalf("/Tags leaked restricted tag %q", it.Name)
		}
	}

	rec = doGet("/OfficialRatings")
	var ratingsRes dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &ratingsRes)
	for _, it := range ratingsRes.Items {
		if it.Name == "R" {
			t.Fatalf("/OfficialRatings leaked R rating to kid")
		}
	}
}
