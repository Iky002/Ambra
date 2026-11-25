package compatmedia

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

func TestCompatMedia_PersonIDFallbackAndPolicyEnforcement(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "cm_rbac.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)

	libAllowed, _ := repo.CreateLibrary(ctx, "Allowed", "movies", []string{filepath.Join(tmp, "a")})
	libDenied, _ := repo.CreateLibrary(ctx, "Denied", "movies", []string{filepath.Join(tmp, "d")})

	mAllowed := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      libAllowed.ID,
		ParentID:       libAllowed.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Allowed Movie",
		OfficialRating: "G",
	}
	mRatedR := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      libAllowed.ID,
		ParentID:       libAllowed.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Rated R Movie",
		OfficialRating: "R",
	}
	mDenied := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      libDenied.ID,
		ParentID:       libDenied.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Denied Movie",
		OfficialRating: "G",
	}
	for _, it := range []*sqlite.ItemRecord{mAllowed, mRatedR, mDenied} {
		if err := repo.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
		_ = repo.ReplaceItemTaxonomies(ctx, it.ID, "genre", []string{"Action"})
		_ = repo.ReplaceItemPeople(ctx, it.ID, []sqlite.PersonInput{{Name: "Keanu Reeves", Type: "Actor"}})
	}

	people, err := repo.GetPeopleByItemID(ctx, mAllowed.ID)
	if err != nil || len(people) == 0 {
		t.Fatalf("expected seeded person, got %+v err=%v", people, err)
	}
	personID := people[0].Id

	maxPG := 5
	kidPolicy := sqlite.DefaultUserPolicy(false)
	kidPolicy.EnableAllFolders = false
	kidPolicy.EnabledFolders = []string{libAllowed.ID}
	kidPolicy.MaxParentalRating = &maxPG
	kid := &sqlite.UserRecord{
		ID:     guid.New(),
		Name:   "kid",
		Policy: kidPolicy,
		Config: sqlite.DefaultUserConfig(),
	}
	_ = userRepo.CreateUser(ctx, kid)

	h := New(repo, nil, nil, nil, store, cfg)
	h.SetUserRepo(userRepo)
	router := h.BuildRouter(AuthFuncs{
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
		router.ServeHTTP(rec, req)
		return rec
	}

	// 1. GET /Persons/{Id} fallback by normalized Person ID
	rec := doGet("/Persons/" + personID)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /Persons/%s status=%d body=%s", personID, rec.Code, rec.Body.String())
	}
	var personDTO dto.BaseItemDto
	_ = json.Unmarshal(rec.Body.Bytes(), &personDTO)
	if personDTO.Id != personID || personDTO.Name != "Keanu Reeves" {
		t.Fatalf("unexpected person DTO: %+v", personDTO)
	}

	// 2. GET /Persons/{Id}/Credits enforces AllowedLibraryIDs + MaxParentalRating
	rec = doGet("/Persons/" + personID + "/Credits")
	var credits dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &credits)
	if credits.TotalRecordCount != 1 || credits.Items[0].Id != mAllowed.ID {
		t.Fatalf("PersonCredits expected only mAllowed, got %+v", credits)
	}

	// 3. GET /Items/{Id}/Similar enforces AllowedLibraryIDs + MaxParentalRating
	rec = doGet("/Items/" + mAllowed.ID + "/Similar")
	var sim dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &sim)
	if sim.TotalRecordCount != 0 {
		t.Fatalf("Similar expected 0 (mRatedR and mDenied filtered out), got %+v", sim)
	}

	// 4. GET /Items/{Id}/InstantMix enforces AllowedLibraryIDs + MaxParentalRating
	rec = doGet("/Items/" + mAllowed.ID + "/InstantMix")
	var mix dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &mix)
	for _, it := range mix.Items {
		if it.Id == mRatedR.ID || it.Id == mDenied.ID {
			t.Fatalf("InstantMix leaked blocked item %s", it.Name)
		}
	}
}
