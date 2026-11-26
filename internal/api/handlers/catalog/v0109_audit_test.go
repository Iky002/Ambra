package catalog

import (
	"context"
	"encoding/json"
	"fmt"
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
