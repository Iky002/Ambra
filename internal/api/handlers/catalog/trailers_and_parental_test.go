package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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

func TestV0109Audit_TrailersPreFilterPushesLibraryParentAndParentalPolicy(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "catalog_v0109.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	libRepo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)
	authSvc := auth.NewService(userRepo, cfg)

	libA, _ := libRepo.CreateLibrary(ctx, "Library A", "movies", []string{filepath.Join(tmp, "a")})
	libB, _ := libRepo.CreateLibrary(ctx, "Library B", "movies", []string{filepath.Join(tmp, "b")})

	// Seed 210 R-rated movies with local trailers in Library A (> maxTrailerParentsScan=200)
	for i := 0; i < 210; i++ {
		mid := guid.New()
		tid := guid.New()
		_ = libRepo.UpsertItem(ctx, &sqlite.ItemRecord{
			ID: mid, LibraryID: libA.ID, ParentID: libA.ID,
			Type: "Movie", MediaType: "Video", Name: fmt.Sprintf("A-Movie-%03d", i), OfficialRating: "R",
		})
		_ = libRepo.ReplaceItemAncestors(ctx, mid, []sqlite.AncestorLink{{AncestorID: libA.ID, Depth: 1}})
		_ = libRepo.UpsertItem(ctx, &sqlite.ItemRecord{
			ID: tid, LibraryID: libA.ID, ParentID: mid,
			Type: "Video", MediaType: "Video", Name: fmt.Sprintf("A-Trailer-%03d", i), OfficialRating: "R",
			Extra: sqlite.ItemExtra{ExtraType: "Trailer"},
		})
	}
	// Seed 1 G-rated movie with trailer in Library A (after 210 R-rated ones)
	gMovieA := guid.New()
	gTrailerA := guid.New()
	_ = libRepo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID: gMovieA, LibraryID: libA.ID, ParentID: libA.ID,
		Type: "Movie", MediaType: "Video", Name: "ZZZ Family Movie A", OfficialRating: "G",
	})
	_ = libRepo.ReplaceItemAncestors(ctx, gMovieA, []sqlite.AncestorLink{{AncestorID: libA.ID, Depth: 1}})
	_ = libRepo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID: gTrailerA, LibraryID: libA.ID, ParentID: gMovieA,
		Type: "Video", MediaType: "Video", Name: "ZZZ Family Trailer A", OfficialRating: "G",
		Extra: sqlite.ItemExtra{ExtraType: "Trailer"},
	})

	// Seed 2 movies with local trailers in Library B
	var libBTrailers []string
	for i := 0; i < 2; i++ {
		mid := guid.New()
		tid := guid.New()
		libBTrailers = append(libBTrailers, tid)
		_ = libRepo.UpsertItem(ctx, &sqlite.ItemRecord{
			ID: mid, LibraryID: libB.ID, ParentID: libB.ID,
			Type: "Movie", MediaType: "Video", Name: fmt.Sprintf("B-Movie-%d", i), OfficialRating: "PG",
		})
		_ = libRepo.ReplaceItemAncestors(ctx, mid, []sqlite.AncestorLink{{AncestorID: libB.ID, Depth: 1}})
		_ = libRepo.UpsertItem(ctx, &sqlite.ItemRecord{
			ID: tid, LibraryID: libB.ID, ParentID: mid,
			Type: "Video", MediaType: "Video", Name: fmt.Sprintf("B-Trailer-%d", i), OfficialRating: "PG",
			Extra: sqlite.ItemExtra{ExtraType: "Trailer"},
		})
	}

	admin, _ := authSvc.CreateUser(ctx, "admin", "adminpass", true)
	userLibB, _ := authSvc.CreateUser(ctx, "userb", "userbpass", false)
	polB := sqlite.DefaultUserPolicy(false)
	polB.EnableAllFolders = false
	polB.EnabledFolders = []string{libB.ID}
	_ = userRepo.UpdateUserPolicy(ctx, userLibB.ID, polB)
	userLibB.Policy = polB

	kidLibA, _ := authSvc.CreateUser(ctx, "kida", "kidapass", false)
	maxPG := 5
	polKid := sqlite.DefaultUserPolicy(false)
	polKid.EnableAllFolders = false
	polKid.EnabledFolders = []string{libA.ID}
	polKid.MaxParentalRating = &maxPG
	_ = userRepo.UpdateUserPolicy(ctx, kidLibA.ID, polKid)
	kidLibA.Policy = polKid

	h := New(libRepo, cfg)
	h.SetUserRepo(userRepo)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	queryAs := func(u *sqlite.UserRecord, urlPath string) dto.QueryResult[dto.BaseItemDto] {
		req := httptest.NewRequest(http.MethodGet, urlPath, nil)
		req = req.WithContext(middleware.WithAuthContext(req.Context(), &middleware.AuthContext{User: u, Token: "tok"}))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s expected 200, got %d body=%s", urlPath, rec.Code, rec.Body.String())
		}
		var res dto.QueryResult[dto.BaseItemDto]
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatal(err)
		}
		return res
	}

	// 1. Restricted user with access only to Library B finds both Library B trailers despite >200 trailers in Lib A
	resB := queryAs(userLibB, "/Trailers")
	if resB.TotalRecordCount != 2 || len(resB.Items) != 2 {
		t.Fatalf("restricted user in Lib B expected 2 trailers, got total=%d len=%d", resB.TotalRecordCount, len(resB.Items))
	}

	// 2. Admin querying with ParentId=libB finds both Library B trailers
	resParentB := queryAs(admin, "/Trailers?ParentId="+libB.ID)
	if resParentB.TotalRecordCount != 2 || len(resParentB.Items) != 2 {
		t.Fatalf("ParentId=libB expected 2 trailers, got total=%d len=%d", resParentB.TotalRecordCount, len(resParentB.Items))
	}

	// 3. Kid user with MaxParentalRating=5 in Lib A finds the G-rated trailer despite 210 R-rated trailers ahead of it
	resKid := queryAs(kidLibA, "/Trailers")
	if resKid.TotalRecordCount != 1 || len(resKid.Items) != 1 || resKid.Items[0].Id != gTrailerA {
		t.Fatalf("kid user expected 1 G-rated trailer (%s), got total=%d items=%+v", gTrailerA, resKid.TotalRecordCount, resKid.Items)
	}
}
