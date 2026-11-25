package library

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

func TestParentalRatingScoreAndExceeding(t *testing.T) {
	cases := []struct {
		rating string
		want   int
		ok     bool
	}{
		{"G", 1, true},
		{"tv-y", 1, true},
		{"TV-G", 1, true},
		{"U", 1, true},
		{"0", 1, true},
		{"TV-Y7", 4, true},
		{"PG", 5, true},
		{"TV-PG", 5, true},
		{"PG-13", 7, true},
		{"12", 7, true},
		{"12A", 7, true},
		{"TV-14", 8, true},
		{"15", 8, true},
		{"IIB", 8, true},
		{"R", 9, true},
		{"TV-MA", 9, true},
		{"16", 9, true},
		{"18", 9, true},
		{"III", 9, true},
		{"NC-17", 10, true},
		{"X", 10, true},
		{"", 0, false},
		{"UNKNOWN-RATING", 0, false},
	}
	for _, tc := range cases {
		got, ok := sqlite.ParentalRatingScore(tc.rating)
		if ok != tc.ok || got != tc.want {
			t.Errorf("ParentalRatingScore(%q) = (%d, %v), want (%d, %v)", tc.rating, got, ok, tc.want, tc.ok)
		}
	}

	exceeding5 := sqlite.ExceedingParentalRatings(5)
	has := make(map[string]bool, len(exceeding5))
	for _, r := range exceeding5 {
		has[r] = true
	}
	for _, shouldExceed := range []string{"PG-13", "TV-14", "R", "TV-MA", "NC-17", "12A", "IIB", "III"} {
		if !has[shouldExceed] {
			t.Errorf("expected %q in ExceedingParentalRatings(5)", shouldExceed)
		}
	}
	for _, shouldAllow := range []string{"G", "TV-Y", "TV-Y7", "PG", "TV-PG"} {
		if has[shouldAllow] {
			t.Errorf("did not expect %q in ExceedingParentalRatings(5)", shouldAllow)
		}
	}
}

func TestParentalControlsEnforcement(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "parental.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)
	ctx := context.Background()

	lib, err := repo.CreateLibrary(ctx, "MoviesAndTV", "movies", []string{tmp})
	if err != nil {
		t.Fatal(err)
	}

	movieG := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      lib.ID,
		ParentID:       lib.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Family Adventure",
		SortName:       "family adventure",
		OfficialRating: "G",
		Path:           filepath.Join(tmp, "family.mkv"),
	}
	movieR := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      lib.ID,
		ParentID:       lib.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Dark Thriller",
		SortName:       "dark thriller",
		OfficialRating: "R",
		Path:           filepath.Join(tmp, "thriller.mkv"),
	}
	movieTagged := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      lib.ID,
		ParentID:       lib.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Tagged PG Movie",
		SortName:       "tagged pg movie",
		OfficialRating: "PG",
		Path:           filepath.Join(tmp, "tagged.mkv"),
	}
	movieUnrated := &sqlite.ItemRecord{
		ID:        guid.New(),
		LibraryID: lib.ID,
		ParentID:  lib.ID,
		Type:      "Movie",
		MediaType: "Video",
		Name:      "Unrated Documentary",
		SortName:  "unrated documentary",
		Path:      filepath.Join(tmp, "unrated.mkv"),
	}

	// Series with blocked tag -> child season & episode should be blocked too!
	seriesBlocked := &sqlite.ItemRecord{
		ID:             guid.New(),
		LibraryID:      lib.ID,
		ParentID:       lib.ID,
		Type:           "Series",
		IsFolder:       true,
		Name:           "Horror Show",
		SortName:       "horror show",
		OfficialRating: "PG",
	}
	seasonBlocked := &sqlite.ItemRecord{
		ID:          guid.New(),
		LibraryID:   lib.ID,
		ParentID:    seriesBlocked.ID,
		SeriesID:    seriesBlocked.ID,
		Type:        "Season",
		IsFolder:    true,
		Name:        "Season 1",
		SortName:    "season 01",
		IndexNumber: intPtr(1),
	}
	epBlocked := &sqlite.ItemRecord{
		ID:                guid.New(),
		LibraryID:         lib.ID,
		ParentID:          seasonBlocked.ID,
		SeriesID:          seriesBlocked.ID,
		SeasonID:          seasonBlocked.ID,
		Type:              "Episode",
		MediaType:         "Video",
		Name:              "Horror Episode 1",
		SortName:          "horror episode 1",
		ParentIndexNumber: intPtr(1),
		IndexNumber:       intPtr(1),
		OfficialRating:    "PG",
	}

	for _, it := range []*sqlite.ItemRecord{movieG, movieR, movieTagged, movieUnrated, seriesBlocked, seasonBlocked, epBlocked} {
		if err := repo.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
		if err := repo.UpsertSearchFTS(ctx, it.ID, it.Name+" Movie", "", "", "", ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.ReplaceItemTaxonomies(ctx, movieTagged.ID, "tag", []string{"Horror", "Classic"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceItemTaxonomies(ctx, seriesBlocked.ID, "tag", []string{"Gore"}); err != nil {
		t.Fatal(err)
	}

	maxPG := 5
	kidPolicy := sqlite.DefaultUserPolicy(false)
	kidPolicy.MaxParentalRating = &maxPG
	kidPolicy.BlockedTags = []string{"horror", "GORE"}
	kidUser := &sqlite.UserRecord{
		ID:     guid.New(),
		Name:   "kid",
		Policy: kidPolicy,
		Config: sqlite.DefaultUserConfig(),
	}
	if err := userRepo.CreateUser(ctx, kidUser); err != nil {
		t.Fatal(err)
	}

	// Admin user with MaxParentalRating + BlockedTags set should still be EXEMPT.
	adminPolicy := sqlite.DefaultUserPolicy(true)
	adminPolicy.MaxParentalRating = &maxPG
	adminPolicy.BlockedTags = []string{"horror", "gore"}
	adminUser := &sqlite.UserRecord{
		ID:      guid.New(),
		Name:    "admin",
		IsAdmin: true,
		Policy:  adminPolicy,
		Config:  sqlite.DefaultUserConfig(),
	}
	if err := userRepo.CreateUser(ctx, adminUser); err != nil {
		t.Fatal(err)
	}

	// Mark movieR and movieG as resumable for kidUser to verify ResumeItems filters movieR out.
	_, err = store.Writer.ExecContext(ctx,
		`INSERT INTO user_item_data (user_id, item_id, played, play_count, playback_position_ticks, last_played_date)
		 VALUES (?, ?, 0, 0, 50000000, 1700000000), (?, ?, 0, 0, 50000000, 1700000001)`,
		kidUser.ID, movieG.ID, kidUser.ID, movieR.ID,
	)
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	h := New(repo, nil, cfg)
	h.SetUserRepo(userRepo)
	h.RegisterRoutes(mux, AuthFuncs{
		UserIDFromCtx: func(c context.Context) string {
			if ac, ok := middleware.AuthFromContext(c); ok && ac != nil {
				return ac.UserID()
			}
			return ""
		},
	})

	doGet := func(path string, ac *middleware.AuthContext) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if ac != nil {
			req = req.WithContext(middleware.WithAuthContext(req.Context(), ac))
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	kidAC := &middleware.AuthContext{User: kidUser}
	adminAC := &middleware.AuthContext{User: adminUser}
	apiKeyAC := &middleware.AuthContext{IsAPIKey: true, APIKeyName: "automation"}

	// 1. QueryItems for kid: only movieG and movieUnrated among Movies
	rec := doGet("/Users/"+kidUser.ID+"/Items?Recursive=true&IncludeItemTypes=Movie", kidAC)
	if rec.Code != http.StatusOK {
		t.Fatalf("kid items status=%d body=%s", rec.Code, rec.Body.String())
	}
	var itemsRes dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &itemsRes)
	if itemsRes.TotalRecordCount != 2 {
		t.Fatalf("kid expected 2 movies (G + unrated), got %d: %+v", itemsRes.TotalRecordCount, itemsRes.Items)
	}
	for _, it := range itemsRes.Items {
		if it.Id == movieR.ID || it.Id == movieTagged.ID {
			t.Fatalf("kid should not see blocked movie %s (%s)", it.Name, it.Id)
		}
	}

	// 2. LatestItems for kid: excludes movieR, movieTagged, seriesBlocked
	rec = doGet("/Users/"+kidUser.ID+"/Items/Latest", kidAC)
	if rec.Code != http.StatusOK {
		t.Fatalf("kid latest status=%d", rec.Code)
	}
	var latest []dto.BaseItemDto
	_ = json.Unmarshal(rec.Body.Bytes(), &latest)
	for _, it := range latest {
		if it.Id == movieR.ID || it.Id == movieTagged.ID || it.Id == seriesBlocked.ID {
			t.Fatalf("kid latest should not include blocked item %s", it.Name)
		}
	}

	// 3. ResumeItems for kid: only movieG (movieR excluded)
	rec = doGet("/Users/"+kidUser.ID+"/Items/Resume", kidAC)
	if rec.Code != http.StatusOK {
		t.Fatalf("kid resume status=%d", rec.Code)
	}
	var resumeRes dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &resumeRes)
	if resumeRes.TotalRecordCount != 1 || resumeRes.Items[0].Id != movieG.ID {
		t.Fatalf("kid resume expected only movieG, got %+v", resumeRes)
	}

	// 4. SearchHints for kid: excludes movieR, movieTagged, seriesBlocked, epBlocked
	rec = doGet("/Search/Hints?SearchTerm=Movie", kidAC)
	if rec.Code != http.StatusOK {
		t.Fatalf("kid hints status=%d", rec.Code)
	}
	var hintsRes dto.SearchHintResult
	_ = json.Unmarshal(rec.Body.Bytes(), &hintsRes)
	for _, hint := range hintsRes.SearchHints {
		if hint.Id == movieR.ID || hint.Id == movieTagged.ID || hint.Id == seriesBlocked.ID || hint.Id == epBlocked.ID {
			t.Fatalf("kid search hints should not include blocked item %s", hint.Name)
		}
	}

	// 5. ShowSeasons, ShowEpisodes, NextUp for kid: seriesBlocked has blocked tag "Gore"
	rec = doGet("/Shows/"+seriesBlocked.ID+"/Seasons", kidAC)
	var seasonsRes dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &seasonsRes)
	if seasonsRes.TotalRecordCount != 0 {
		t.Fatalf("kid expected 0 seasons for tag-blocked series, got %d", seasonsRes.TotalRecordCount)
	}

	rec = doGet("/Shows/"+seriesBlocked.ID+"/Episodes", kidAC)
	var epsRes dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &epsRes)
	if epsRes.TotalRecordCount != 0 {
		t.Fatalf("kid expected 0 episodes for tag-blocked series, got %d", epsRes.TotalRecordCount)
	}

	rec = doGet("/Shows/NextUp?UserId="+kidUser.ID, kidAC)
	var nextUpRes dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &nextUpRes)
	if nextUpRes.TotalRecordCount != 0 {
		t.Fatalf("kid expected 0 nextup episodes for tag-blocked series, got %d", nextUpRes.TotalRecordCount)
	}

	// 6. Single-item GetItem for kid: 200 on movieG/movieUnrated, 403 on movieR, movieTagged, epBlocked
	for _, allowedID := range []string{movieG.ID, movieUnrated.ID} {
		rec = doGet("/Users/"+kidUser.ID+"/Items/"+allowedID, kidAC)
		if rec.Code != http.StatusOK {
			t.Fatalf("kid expected 200 for allowed item %s, got %d", allowedID, rec.Code)
		}
	}
	for _, blockedID := range []string{movieR.ID, movieTagged.ID, epBlocked.ID} {
		rec = doGet("/Users/"+kidUser.ID+"/Items/"+blockedID, kidAC)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("kid expected 403 for blocked item %s, got %d", blockedID, rec.Code)
		}
	}

	// 7. Admin and API key exemption: see all 4 movies and get 200 on movieR & movieTagged
	for name, ac := range map[string]*middleware.AuthContext{"admin": adminAC, "apikey": apiKeyAC} {
		rec = doGet("/Items?Recursive=true&IncludeItemTypes=Movie", ac)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s items status=%d", name, rec.Code)
		}
		var res dto.QueryResult[dto.BaseItemDto]
		_ = json.Unmarshal(rec.Body.Bytes(), &res)
		if res.TotalRecordCount != 4 {
			t.Fatalf("%s expected all 4 movies, got %d", name, res.TotalRecordCount)
		}
		for _, id := range []string{movieR.ID, movieTagged.ID, epBlocked.ID} {
			rec = doGet("/Items/"+id, ac)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s expected 200 for item %s, got %d", name, id, rec.Code)
			}
		}
	}
}

func intPtr(v int) *int {
	return &v
}
