package catalog

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestCatalogV0107AuditTrailersParentalFilterAndSanitizedErrors(t *testing.T) {
	tmp := t.TempDir()
	st, err := sqlite.Open(filepath.Join(tmp, "catalog_v0107.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(st)
	userRepo := sqlite.NewUserRepo(st)
	cfg := config.Default(tmp)

	lib, _ := repo.CreateLibrary(ctx, "Movies", "movies", []string{tmp})

	// G-rated parent movie with two local trailer extras: one G-rated, one R-rated (or blocked tag)
	parentMovie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "Family Film", OfficialRating: "G",
	}
	_ = repo.UpsertItem(ctx, parentMovie)

	safeExtra := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: parentMovie.ID,
		Type: "Video", MediaType: "Video", Name: "Family Film - Safe Trailer", OfficialRating: "G",
		Extra: sqlite.ItemExtra{ExtraType: "Trailer"},
	}
	rRatedExtra := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: parentMovie.ID,
		Type: "Video", MediaType: "Video", Name: "Family Film - Red Band Trailer", OfficialRating: "R",
		Extra: sqlite.ItemExtra{ExtraType: "Trailer"},
	}
	taggedExtra := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: parentMovie.ID,
		Type: "Video", MediaType: "Video", Name: "Family Film - Gore Trailer", OfficialRating: "G",
		Extra: sqlite.ItemExtra{ExtraType: "Trailer"},
	}
	for _, it := range []*sqlite.ItemRecord{safeExtra, rRatedExtra, taggedExtra} {
		_ = repo.UpsertItem(ctx, it)
	}
	_ = repo.ReplaceItemTaxonomies(ctx, taggedExtra.ID, "tag", []string{"nsfw"})

	// Extra movie with NO trailers (verifies pre-filter skips it)
	noTrailerMovie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "No Trailer Movie", OfficialRating: "G",
	}
	_ = repo.UpsertItem(ctx, noTrailerMovie)

	maxPG := 5
	kid := &sqlite.UserRecord{
		ID: guid.New(), Name: "kid",
		Policy: dto.UserPolicy{
			EnableAllFolders:  true,
			MaxParentalRating: &maxPG,
			BlockedTags:       []string{"nsfw"},
		},
		Config: sqlite.DefaultUserConfig(),
	}
	admin := &sqlite.UserRecord{
		ID: guid.New(), Name: "admin", IsAdmin: true,
		Policy: sqlite.DefaultUserPolicy(true), Config: sqlite.DefaultUserConfig(),
	}
	_ = userRepo.CreateUser(ctx, kid)
	_ = userRepo.CreateUser(ctx, admin)

	h := New(repo, cfg)
	h.SetUserRepo(userRepo)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	doGet := func(u *sqlite.UserRecord, target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req = req.WithContext(middleware.WithAuthContext(req.Context(), &middleware.AuthContext{User: u}))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// Kid sees ONLY safeExtra, NOT rRatedExtra or taggedExtra
	recKid := doGet(kid, "/Trailers")
	if recKid.Code != http.StatusOK {
		t.Fatalf("kid GET /Trailers got %d body=%s", recKid.Code, recKid.Body.String())
	}
	var kidRes dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(recKid.Body.Bytes(), &kidRes)
	if len(kidRes.Items) != 1 || kidRes.Items[0].Id != safeExtra.ID {
		t.Fatalf("expected kid to only see safe trailer extra, got %+v", kidRes.Items)
	}

	// Admin sees all 3 trailer extras
	recAdmin := doGet(admin, "/Trailers")
	if recAdmin.Code != http.StatusOK {
		t.Fatalf("admin GET /Trailers got %d", recAdmin.Code)
	}
	var adminRes dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(recAdmin.Body.Bytes(), &adminRes)
	if len(adminRes.Items) != 3 {
		t.Fatalf("expected admin to see all 3 trailer extras, got %d", len(adminRes.Items))
	}

	// Non-admin cross-user UserId forbidden on Channels & Trailers; allowed for self
	if rec := doGet(kid, "/Channels?UserId="+admin.ID); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 cross-user /Channels, got %d", rec.Code)
	}
	if rec := doGet(kid, "/Channels"); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 self /Channels, got %d", rec.Code)
	}
	if rec := doGet(kid, "/Channels/"+parentMovie.ID+"/Items?UserId="+admin.ID); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 cross-user /Channels/{Id}/Items, got %d", rec.Code)
	}
	if rec := doGet(kid, "/Channels/"+parentMovie.ID+"/Items"); rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 non-channel /Channels/{Id}/Items, got %d", rec.Code)
	}

	// DB error is sanitized (no raw sqlite error leaked) across catalog endpoints
	st.Close()
	for _, ep := range []string{
		"/Trailers",
		"/Channels",
		"/Channels/" + parentMovie.ID + "/Items",
		"/Items/Filters",
		"/Items/Filters2",
		"/Movies/" + parentMovie.ID + "/Similar",
		"/Albums/" + parentMovie.ID + "/InstantMix",
	} {
		recErr := doGet(admin, ep)
		if recErr.Code != http.StatusInternalServerError || strings.Contains(recErr.Body.String(), "sql:") {
			t.Fatalf("%s expected sanitized 500 on closed DB, got %d %q", ep, recErr.Code, recErr.Body.String())
		}
	}
}
