package library

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestLibraryHandlerPerUserFolderIsolation(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "rbac.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)

	ctx := context.Background()
	libA, err := repo.CreateLibrary(ctx, "LibA", "movies", []string{filepath.Join(tmp, "a")})
	if err != nil {
		t.Fatal(err)
	}
	libB, err := repo.CreateLibrary(ctx, "LibB", "movies", []string{filepath.Join(tmp, "b")})
	if err != nil {
		t.Fatal(err)
	}

	movieA := &sqlite.ItemRecord{
		ID:        guid.New(),
		LibraryID: libA.ID,
		ParentID:  libA.ID,
		Type:      "Movie",
		MediaType: "Video",
		Name:      "Alpha Movie",
		SortName:  "alpha movie",
		Path:      filepath.Join(tmp, "a", "alpha.mkv"),
	}
	movieB := &sqlite.ItemRecord{
		ID:        guid.New(),
		LibraryID: libB.ID,
		ParentID:  libB.ID,
		Type:      "Movie",
		MediaType: "Video",
		Name:      "Beta Movie",
		SortName:  "beta movie",
		Path:      filepath.Join(tmp, "b", "beta.mkv"),
	}
	if err := repo.UpsertItem(ctx, movieA); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertItem(ctx, movieB); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertSearchFTS(ctx, movieA.ID, movieA.Name, "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertSearchFTS(ctx, movieB.ID, movieB.Name, "", "", "", ""); err != nil {
		t.Fatal(err)
	}

	// Users:
	// 1. Admin (unrestricted even if EnableAllFolders is false)
	adminPolicy := sqlite.DefaultUserPolicy(true)
	adminPolicy.EnableAllFolders = false
	adminUser := &sqlite.UserRecord{
		ID:     guid.New(),
		Name:   "admin",
		IsAdmin: true,
		Policy: adminPolicy,
		Config: sqlite.DefaultUserConfig(),
	}
	if err := userRepo.CreateUser(ctx, adminUser); err != nil {
		t.Fatal(err)
	}

	// 2. Non-admin with EnableAllFolders: true
	allUser := &sqlite.UserRecord{
		ID:     guid.New(),
		Name:   "alluser",
		Policy: sqlite.DefaultUserPolicy(false),
		Config: sqlite.DefaultUserConfig(),
	}
	if err := userRepo.CreateUser(ctx, allUser); err != nil {
		t.Fatal(err)
	}

	// 3. Non-admin restricted to libA only
	restrictedPolicy := sqlite.DefaultUserPolicy(false)
	restrictedPolicy.EnableAllFolders = false
	restrictedPolicy.EnabledFolders = []string{libA.ID}
	restrictedUser := &sqlite.UserRecord{
		ID:     guid.New(),
		Name:   "restricted",
		Policy: restrictedPolicy,
		Config: sqlite.DefaultUserConfig(),
	}
	if err := userRepo.CreateUser(ctx, restrictedUser); err != nil {
		t.Fatal(err)
	}

	// 4. Non-admin restricted to 0 libraries
	nonePolicy := sqlite.DefaultUserPolicy(false)
	nonePolicy.EnableAllFolders = false
	nonePolicy.EnabledFolders = []string{}
	noneUser := &sqlite.UserRecord{
		ID:     guid.New(),
		Name:   "none",
		Policy: nonePolicy,
		Config: sqlite.DefaultUserConfig(),
	}
	if err := userRepo.CreateUser(ctx, noneUser); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	h := New(repo, nil, cfg)
	h.SetUserRepo(userRepo)
	h.RegisterRoutes(mux, AuthFuncs{})

	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	// Admin and EnableAllFolders=true user see both libraries
	for _, uid := range []string{adminUser.ID, allUser.ID} {
		rec := get("/Users/" + uid + "/Views")
		if rec.Code != http.StatusOK {
			t.Fatalf("views status=%d", rec.Code)
		}
		var views dto.QueryResult[dto.BaseItemDto]
		_ = json.Unmarshal(rec.Body.Bytes(), &views)
		if views.TotalRecordCount != 2 {
			t.Fatalf("user %s expected 2 views, got %d", uid, views.TotalRecordCount)
		}

		rec = get("/Users/" + uid + "/Items/" + movieB.ID)
		if rec.Code != http.StatusOK {
			t.Fatalf("user %s expected 200 on movieB, got %d", uid, rec.Code)
		}
	}

	// Restricted user (libA only):
	// 1. Views -> only libA
	rec := get("/Users/" + restrictedUser.ID + "/Views")
	if rec.Code != http.StatusOK {
		t.Fatalf("restricted views status=%d", rec.Code)
	}
	var views dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &views)
	if views.TotalRecordCount != 1 || views.Items[0].Id != libA.ID {
		t.Fatalf("restricted user expected only libA, got %+v", views)
	}

	// 2. Items -> only movieA
	rec = get("/Users/" + restrictedUser.ID + "/Items?Recursive=true")
	if rec.Code != http.StatusOK {
		t.Fatalf("restricted items status=%d", rec.Code)
	}
	var itemsRes dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &itemsRes)
	if itemsRes.TotalRecordCount != 1 || itemsRes.Items[0].Id != movieA.ID {
		t.Fatalf("restricted user expected only movieA, got %+v", itemsRes)
	}

	// 3. Latest -> only movieA
	rec = get("/Users/" + restrictedUser.ID + "/Items/Latest")
	if rec.Code != http.StatusOK {
		t.Fatalf("restricted latest status=%d", rec.Code)
	}
	var latestRes []dto.BaseItemDto
	_ = json.Unmarshal(rec.Body.Bytes(), &latestRes)
	if len(latestRes) != 1 || latestRes[0].Id != movieA.ID {
		t.Fatalf("restricted user expected only movieA in Latest, got %+v", latestRes)
	}

	// 4. SearchHints -> only movieA
	rec = get("/Search/Hints?SearchTerm=Movie&UserId=" + restrictedUser.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("restricted search hints status=%d", rec.Code)
	}
	var hintsRes dto.SearchHintResult
	_ = json.Unmarshal(rec.Body.Bytes(), &hintsRes)
	if hintsRes.TotalRecordCount != 1 || hintsRes.SearchHints[0].Id != movieA.ID {
		t.Fatalf("restricted user expected only movieA in SearchHints, got %+v", hintsRes)
	}

	// 5. GetItem: movieA & libA allowed (200), movieB & libB denied (403)
	if rec := get("/Users/" + restrictedUser.ID + "/Items/" + movieA.ID); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for movieA, got %d", rec.Code)
	}
	if rec := get("/Users/" + restrictedUser.ID + "/Items/" + libA.ID); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for libA, got %d", rec.Code)
	}
	if rec := get("/Users/" + restrictedUser.ID + "/Items/" + movieB.ID); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for movieB, got %d", rec.Code)
	}
	if rec := get("/Users/" + restrictedUser.ID + "/Items/" + libB.ID); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for libB, got %d", rec.Code)
	}

	// Zero-library user: empty views/items/latest/search and 403 on movieA
	rec = get("/Users/" + noneUser.ID + "/Views")
	_ = json.Unmarshal(rec.Body.Bytes(), &views)
	if views.TotalRecordCount != 0 {
		t.Fatalf("none user expected 0 views, got %d", views.TotalRecordCount)
	}
	rec = get("/Users/" + noneUser.ID + "/Items?Recursive=true")
	_ = json.Unmarshal(rec.Body.Bytes(), &itemsRes)
	if itemsRes.TotalRecordCount != 0 {
		t.Fatalf("none user expected 0 items, got %d", itemsRes.TotalRecordCount)
	}
	rec = get("/Users/" + noneUser.ID + "/Items/Latest")
	_ = json.Unmarshal(rec.Body.Bytes(), &latestRes)
	if len(latestRes) != 0 {
		t.Fatalf("none user expected 0 latest, got %d", len(latestRes))
	}
	rec = get("/Search/Hints?SearchTerm=Movie&UserId=" + noneUser.ID)
	_ = json.Unmarshal(rec.Body.Bytes(), &hintsRes)
	if hintsRes.TotalRecordCount != 0 {
		t.Fatalf("none user expected 0 search hints, got %d", hintsRes.TotalRecordCount)
	}
	if rec := get("/Users/" + noneUser.ID + "/Items/" + movieA.ID); rec.Code != http.StatusForbidden {
		t.Fatalf("none user expected 403 for movieA, got %d", rec.Code)
	}

	// Unknown user ID falls back gracefully (no panic)
	if rec := get("/Users/" + guid.New() + "/Views"); rec.Code != http.StatusOK {
		t.Fatalf("unknown user views expected 200, got %d", rec.Code)
	}
}

func TestSpoofedUserIDCannotBypassEnabledFolders(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "spoof.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)
	ctx := context.Background()

	libA, _ := repo.CreateLibrary(ctx, "AllowedLib", "movies", []string{filepath.Join(tmp, "a")})
	libB, _ := repo.CreateLibrary(ctx, "SecretLib", "tvshows", []string{filepath.Join(tmp, "b")})

	movieA := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID, Type: "Movie", MediaType: "Video", Name: "Allowed Movie", SortName: "allowed"}
	movieB := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libB.ID, ParentID: libB.ID, Type: "Movie", MediaType: "Video", Name: "Secret Movie", SortName: "secret"}
	seriesA := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID, Type: "Series", MediaType: "Video", Name: "Allowed Show", SortName: "allowed show"}
	seriesB := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libB.ID, ParentID: libB.ID, Type: "Series", MediaType: "Video", Name: "Secret Show", SortName: "secret show"}
	seasonB := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libB.ID, ParentID: seriesB.ID, SeriesID: seriesB.ID, Type: "Season", MediaType: "Video", Name: "Season 1", SortName: "season 1"}
	epB := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libB.ID, ParentID: seasonB.ID, SeriesID: seriesB.ID, SeasonID: seasonB.ID, Type: "Episode", MediaType: "Video", Name: "Ep 1", SortName: "ep 1"}
	for _, it := range []*sqlite.ItemRecord{movieA, movieB, seriesA, seriesB, seasonB, epB} {
		if err := repo.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}

	adminUser := &sqlite.UserRecord{
		ID: guid.New(), Name: "admin", IsAdmin: true,
		Policy: sqlite.DefaultUserPolicy(true), Config: sqlite.DefaultUserConfig(),
	}
	_ = userRepo.CreateUser(ctx, adminUser)

	restrictedPolicy := sqlite.DefaultUserPolicy(false)
	restrictedPolicy.EnableAllFolders = false
	restrictedPolicy.EnabledFolders = []string{libA.ID}
	restrictedUser := &sqlite.UserRecord{
		ID: guid.New(), Name: "restricted",
		Policy: restrictedPolicy, Config: sqlite.DefaultUserConfig(),
	}
	_ = userRepo.CreateUser(ctx, restrictedUser)

	// Mark both movieA and movieB as resumable for adminUser
	_, _ = userRepo.UpsertPlaybackProgress(ctx, adminUser.ID, movieA.ID, 50000, false, nil, nil)
	_, _ = userRepo.UpsertPlaybackProgress(ctx, adminUser.ID, movieB.ID, 50000, false, nil, nil)

	// Wire AuthFuncs so authenticated caller in context is ALWAYS restrictedUser
	mux := http.NewServeMux()
	h := New(repo, nil, cfg)
	h.SetUserRepo(userRepo)
	h.RegisterRoutes(mux, AuthFuncs{
		UserIDFromCtx: func(context.Context) string { return restrictedUser.ID },
	})

	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	// Spoofing adminUser.ID in /Users/{UserId}/... paths must be rejected with 403!
	if rec := get("/Users/" + adminUser.ID + "/Views"); rec.Code != http.StatusForbidden {
		t.Fatalf("spoofed Views expected 403, got %d", rec.Code)
	}
	if rec := get("/Users/" + adminUser.ID + "/Items?Recursive=true&IncludeItemTypes=Movie"); rec.Code != http.StatusForbidden {
		t.Fatalf("spoofed Items expected 403, got %d", rec.Code)
	}
	if rec := get("/Users/" + adminUser.ID + "/Items/Resume"); rec.Code != http.StatusForbidden {
		t.Fatalf("spoofed Resume expected 403, got %d", rec.Code)
	}

	// Self-access still respects restrictedUser's EnabledFolders
	rec := get("/Users/" + restrictedUser.ID + "/Views")
	var views dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &views)
	if views.TotalRecordCount != 1 || views.Items[0].Id != libA.ID {
		t.Fatalf("restricted Views expected libA only: %+v", views)
	}

	rec = get("/Library/VirtualFolders?UserId=" + adminUser.ID)
	var vfs []virtualFolderDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &vfs)
	if len(vfs) != 1 || vfs[0].ItemId != libA.ID {
		t.Fatalf("spoofed VirtualFolders bypassed EnabledFolders: %+v", vfs)
	}

	if rec := get("/Users/" + adminUser.ID + "/Items/" + movieB.ID); rec.Code != http.StatusForbidden {
		t.Fatalf("spoofed GetItem on movieB expected 403, got %d", rec.Code)
	}
	if rec := get("/Shows/" + seriesB.ID + "/Seasons?UserId=" + adminUser.ID); rec.Code != http.StatusForbidden {
		t.Fatalf("spoofed ShowSeasons on seriesB expected 403, got %d", rec.Code)
	}
	if rec := get("/Shows/" + seriesB.ID + "/Episodes?UserId=" + adminUser.ID); rec.Code != http.StatusForbidden {
		t.Fatalf("spoofed ShowEpisodes on seriesB expected 403, got %d", rec.Code)
	}
	if rec := get("/Shows/" + seriesA.ID + "/Seasons?UserId=" + adminUser.ID); rec.Code != http.StatusForbidden {
		t.Fatalf("spoofed ShowSeasons on seriesA expected 403, got %d", rec.Code)
	}
	if rec := get("/Shows/" + seriesA.ID + "/Episodes?UserId=" + adminUser.ID); rec.Code != http.StatusForbidden {
		t.Fatalf("spoofed ShowEpisodes on seriesA expected 403, got %d", rec.Code)
	}
	if rec := get("/Shows/" + seriesA.ID + "/Seasons?UserId=" + restrictedUser.ID); rec.Code != http.StatusOK {
		t.Fatalf("allowed ShowSeasons on seriesA expected 200, got %d", rec.Code)
	}
	if rec := get("/Shows/" + seriesA.ID + "/Episodes?UserId=" + restrictedUser.ID); rec.Code != http.StatusOK {
		t.Fatalf("allowed ShowEpisodes on seriesA expected 200, got %d", rec.Code)
	}
}

func TestPlayedFavoriteIDORAndCountsTaxonomiesScoping(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "idor.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)
	ctx := context.Background()

	libA, _ := repo.CreateLibrary(ctx, "LibA", "movies", []string{filepath.Join(tmp, "a")})
	libB, _ := repo.CreateLibrary(ctx, "LibB", "movies", []string{filepath.Join(tmp, "b")})

	y2020 := 2020
	y2025 := 2025
	movieA := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID,
		Type: "Movie", MediaType: "Video", Name: "Alpha", SortName: "alpha",
		ProductionYear: &y2020,
	}
	movieB := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libB.ID, ParentID: libB.ID,
		Type: "Movie", MediaType: "Video", Name: "Beta", SortName: "beta",
		ProductionYear: &y2025,
	}
	_ = repo.UpsertItem(ctx, movieA)
	_ = repo.UpsertItem(ctx, movieB)
	_ = repo.ReplaceItemTaxonomies(ctx, movieA.ID, "genre", []string{"Action"})
	_ = repo.ReplaceItemTaxonomies(ctx, movieB.ID, "genre", []string{"SecretGenre"})
	_ = repo.ReplaceItemTaxonomies(ctx, movieA.ID, "studio", []string{"StudioA"})
	_ = repo.ReplaceItemTaxonomies(ctx, movieB.ID, "studio", []string{"StudioB"})

	adminUser := &sqlite.UserRecord{
		ID: guid.New(), Name: "admin", IsAdmin: true,
		Policy: sqlite.DefaultUserPolicy(true), Config: sqlite.DefaultUserConfig(),
	}
	_ = userRepo.CreateUser(ctx, adminUser)

	restrictedPolicy := sqlite.DefaultUserPolicy(false)
	restrictedPolicy.EnableAllFolders = false
	restrictedPolicy.EnabledFolders = []string{libA.ID}
	restrictedUser := &sqlite.UserRecord{
		ID: guid.New(), Name: "restricted",
		Policy: restrictedPolicy, Config: sqlite.DefaultUserConfig(),
	}
	_ = userRepo.CreateUser(ctx, restrictedUser)

	mux := http.NewServeMux()
	h := New(repo, nil, cfg)
	h.SetUserRepo(userRepo)
	h.RegisterRoutes(mux, AuthFuncs{
		UserIDFromCtx: func(context.Context) string { return restrictedUser.ID },
	})

	do := func(method, path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
		return rec
	}

	// 1. IDOR on PlayedItems and FavoriteItems targeting adminUser -> 403
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/Users/" + adminUser.ID + "/PlayedItems/" + movieA.ID},
		{http.MethodDelete, "/Users/" + adminUser.ID + "/PlayedItems/" + movieA.ID},
		{http.MethodPost, "/Users/" + adminUser.ID + "/FavoriteItems/" + movieA.ID},
		{http.MethodDelete, "/Users/" + adminUser.ID + "/FavoriteItems/" + movieA.ID},
	} {
		if rec := do(tc.method, tc.path); rec.Code != http.StatusForbidden {
			t.Fatalf("%s %s expected 403, got %d", tc.method, tc.path, rec.Code)
		}
	}

	// 2. Self on PlayedItems and FavoriteItems -> 200
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/Users/" + restrictedUser.ID + "/PlayedItems/" + movieA.ID},
		{http.MethodDelete, "/Users/" + restrictedUser.ID + "/PlayedItems/" + movieA.ID},
		{http.MethodPost, "/Users/" + restrictedUser.ID + "/FavoriteItems/" + movieA.ID},
		{http.MethodDelete, "/Users/" + restrictedUser.ID + "/FavoriteItems/" + movieA.ID},
	} {
		if rec := do(tc.method, tc.path); rec.Code != http.StatusOK {
			t.Fatalf("self %s %s expected 200, got %d", tc.method, tc.path, rec.Code)
		}
	}

	// 3. /Items/Counts only counts libA (1 movie, not 2)
	rec := do(http.MethodGet, "/Items/Counts")
	if rec.Code != http.StatusOK {
		t.Fatalf("counts status=%d", rec.Code)
	}
	var counts sqlite.ItemCounts
	_ = json.Unmarshal(rec.Body.Bytes(), &counts)
	if counts.MovieCount != 1 {
		t.Fatalf("expected restricted MovieCount=1, got %d", counts.MovieCount)
	}

	// 4. /Genres, /Studios, /Years only include libA metadata
	rec = do(http.MethodGet, "/Genres")
	var genres dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &genres)
	if genres.TotalRecordCount != 1 || genres.Items[0].Name != "Action" {
		t.Fatalf("expected only Action genre, got %+v", genres)
	}

	rec = do(http.MethodGet, "/Studios")
	var studios dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &studios)
	if studios.TotalRecordCount != 1 || studios.Items[0].Name != "StudioA" {
		t.Fatalf("expected only StudioA, got %+v", studios)
	}

	rec = do(http.MethodGet, "/Years")
	var years dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &years)
	if years.TotalRecordCount != 1 || years.Items[0].Name != "2020" {
		t.Fatalf("expected only year 2020, got %+v", years)
	}
}
