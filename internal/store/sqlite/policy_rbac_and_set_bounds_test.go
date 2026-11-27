package sqlite

import (
	"context"
	"fmt"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/pkg/guid"
)

func TestPolicyRBACFailClosedAndGetItemByIDScopedHydration(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	libRepo := NewLibraryRepo(st)
	userRepo := NewUserRepo(st)

	libA, err := libRepo.CreateLibrary(ctx, "AllowedTV", "tvshows", nil)
	if err != nil {
		t.Fatal(err)
	}
	libB, err := libRepo.CreateLibrary(ctx, "SecretTV", "tvshows", nil)
	if err != nil {
		t.Fatal(err)
	}

	series := &ItemRecord{
		ID:        guid.New(),
		LibraryID: libA.ID,
		ParentID:  libA.ID,
		Type:      "Series",
		IsFolder:  true,
		Name:      "Split Lib Show",
		SortName:  "split lib show",
	}
	seasonA := &ItemRecord{
		ID:        guid.New(),
		LibraryID: libA.ID,
		ParentID:  series.ID,
		SeriesID:  series.ID,
		Type:      "Season",
		IsFolder:  true,
		Name:      "Season 1",
		SortName:  "season 01",
	}
	seasonB := &ItemRecord{
		ID:        guid.New(),
		LibraryID: libB.ID,
		ParentID:  series.ID,
		SeriesID:  series.ID,
		Type:      "Season",
		IsFolder:  true,
		Name:      "Season 2 Secret",
		SortName:  "season 02",
	}
	epA := &ItemRecord{
		ID:        guid.New(),
		LibraryID: libA.ID,
		ParentID:  seasonA.ID,
		SeriesID:  series.ID,
		SeasonID:  seasonA.ID,
		Type:      "Episode",
		MediaType: "Video",
		Name:      "Ep Allowed",
		SortName:  "ep allowed",
	}
	epB := &ItemRecord{
		ID:        guid.New(),
		LibraryID: libB.ID,
		ParentID:  seasonB.ID,
		SeriesID:  series.ID,
		SeasonID:  seasonB.ID,
		Type:      "Episode",
		MediaType: "Video",
		Name:      "Ep Secret",
		SortName:  "ep secret",
	}
	for _, rec := range []*ItemRecord{series, seasonA, seasonB, epA, epB} {
		if err := libRepo.UpsertItem(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}

	// 1. Default "{}" policy_json keeps EnableAllFolders=true (unrestricted allowedLibs == nil).
	defaultUser := &UserRecord{ID: guid.New(), Name: "default_empty_pol", Policy: DefaultUserPolicy(false)}
	if err := userRepo.CreateUser(ctx, defaultUser); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Writer.ExecContext(ctx, `UPDATE users SET policy_json = '{}' WHERE id = ?`, defaultUser.ID); err != nil {
		t.Fatal(err)
	}
	_, defAllowed, defRestricted := libRepo.loadRestrictedUserPolicy(ctx, defaultUser.ID)
	if defAllowed != nil || defRestricted {
		t.Fatalf("expected '{}' user policy to default to unrestricted folders, got allowed=%v restricted=%v", defAllowed, defRestricted)
	}

	// 2. Explicit {"EnableAllFolders": false} with omitted/null EnabledFolders fails closed (allowedLibs = []string{}).
	failClosedUser := &UserRecord{ID: guid.New(), Name: "fail_closed_pol", Policy: DefaultUserPolicy(false)}
	if err := userRepo.CreateUser(ctx, failClosedUser); err != nil {
		t.Fatal(err)
	}
	for _, rawJSON := range []string{`{"EnableAllFolders":false}`, `{"EnableAllFolders":false,"EnabledFolders":null}`} {
		if _, err := st.Writer.ExecContext(ctx, `UPDATE users SET policy_json = ? WHERE id = ?`, rawJSON, failClosedUser.ID); err != nil {
			t.Fatal(err)
		}
		_, fcAllowed, fcRestricted := libRepo.loadRestrictedUserPolicy(ctx, failClosedUser.ID)
		if fcAllowed == nil || len(fcAllowed) != 0 || !fcRestricted {
			t.Fatalf("expected %s to fail closed with empty non-nil allowedLibs, got allowed=%v restricted=%v", rawJSON, fcAllowed, fcRestricted)
		}
	}

	// 3. IsItemBlockedByPolicy fails closed when EnableAllFolders=false and EnabledFolders==nil.
	blocked, err := libRepo.IsItemBlockedByPolicy(ctx, epA, dto.UserPolicy{
		EnableAllFolders:   false,
		EnabledFolders:     nil,
		EnableLiveTvAccess: true,
	})
	if err != nil || !blocked {
		t.Fatalf("expected IsItemBlockedByPolicy to fail closed when EnableAllFolders=false and EnabledFolders=nil, got blocked=%v err=%v", blocked, err)
	}

	// 4. GetItemByID passes allowedLibs to hydrateDTOsWithPolicy so Series ChildCount / RecursiveItemCount exclude disallowed libraries!
	scopedPol := DefaultUserPolicy(false)
	scopedPol.EnableAllFolders = false
	scopedPol.EnabledFolders = []string{libA.ID}
	scopedUser := &UserRecord{ID: guid.New(), Name: "scoped_viewer", Policy: scopedPol}
	if err := userRepo.CreateUser(ctx, scopedUser); err != nil {
		t.Fatal(err)
	}

	adminSeriesDTO, err := libRepo.GetItemByID(ctx, "srv1", "", series.ID)
	if err != nil {
		t.Fatal(err)
	}
	if adminSeriesDTO.RecursiveItemCount == nil || *adminSeriesDTO.RecursiveItemCount != 2 {
		t.Fatalf("expected unrestricted Series RecursiveItemCount=2, got %v", adminSeriesDTO.RecursiveItemCount)
	}
	if adminSeriesDTO.ChildCount == nil || *adminSeriesDTO.ChildCount != 2 {
		t.Fatalf("expected unrestricted Series ChildCount=2, got %v", adminSeriesDTO.ChildCount)
	}

	scopedSeriesDTO, err := libRepo.GetItemByID(ctx, "srv1", scopedUser.ID, series.ID)
	if err != nil {
		t.Fatal(err)
	}
	if scopedSeriesDTO.RecursiveItemCount == nil || *scopedSeriesDTO.RecursiveItemCount != 1 {
		t.Fatalf("expected scoped GetItemByID RecursiveItemCount=1 (excluding secret libB), got %v", scopedSeriesDTO.RecursiveItemCount)
	}
	if scopedSeriesDTO.ChildCount == nil || *scopedSeriesDTO.ChildCount != 1 {
		t.Fatalf("expected scoped GetItemByID ChildCount=1 (excluding secret libB), got %v", *scopedSeriesDTO.ChildCount)
	}
}

func TestIncludeItemTypesLargeSetBindLimitSafety(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	repo := NewLibraryRepo(st)

	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", nil)
	if err != nil {
		t.Fatal(err)
	}
	mov := &ItemRecord{
		ID:             guid.New(),
		LibraryID:      lib.ID,
		ParentID:       lib.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Inception",
		SortName:       "inception",
		OfficialRating: "PG-13",
		ProductionYear: intPtr(2010),
	}
	if err := repo.UpsertItemsBatch(ctx, []BatchItemInput{{
		Item:           mov,
		FTSTitleRaw:    "Inception",
		FTSTitleTokens: "inception",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceItemTaxonomies(ctx, mov.ID, "genre", []string{"Sci-Fi"}); err != nil {
		t.Fatal(err)
	}

	// Build 40,000 IncludeItemTypes (> SQLite's 32766 SQLITE_MAX_VARIABLE_NUMBER).
	hugeTypes := make([]string, 40000)
	hugeTypes[0] = "Movie"
	for i := 1; i < len(hugeTypes); i++ {
		hugeTypes[i] = fmt.Sprintf("SyntheticType_%d", i)
	}

	hints, totalHints, err := repo.SearchHints(ctx, SearchHintFilter{
		SearchTerm:       "Inception",
		IncludeItemTypes: hugeTypes,
		Limit:            10,
	})
	if err != nil {
		t.Fatalf("SearchHints with 40k IncludeItemTypes failed: %v", err)
	}
	if totalHints != 1 || len(hints) != 1 || hints[0].Id != mov.ID {
		t.Fatalf("unexpected SearchHints result: total=%d hints=%+v", totalHints, hints)
	}

	qf, err := repo.GetQueryFilters(ctx, QueryFiltersFilter{
		ParentID:         lib.ID,
		IncludeItemTypes: hugeTypes,
	})
	if err != nil {
		t.Fatalf("GetQueryFilters with 40k IncludeItemTypes failed: %v", err)
	}
	if len(qf.Genres) != 1 || qf.Genres[0].Name != "Sci-Fi" {
		t.Fatalf("unexpected GetQueryFilters genres: %+v", qf.Genres)
	}

	vals, totalVals, err := repo.ListFilterValuesWithUserPolicy(
		ctx,
		"srv1",
		"OfficialRating",
		lib.ID,
		hugeTypes,
		0,
		50,
		nil,
		DefaultUserPolicy(true),
	)
	if err != nil {
		t.Fatalf("ListFilterValuesWithUserPolicy with 40k IncludeItemTypes failed: %v", err)
	}
	if totalVals != 1 || len(vals) != 1 || vals[0].Name != "PG-13" {
		t.Fatalf("unexpected ListFilterValuesWithUserPolicy result: total=%d vals=%+v", totalVals, vals)
	}
}
