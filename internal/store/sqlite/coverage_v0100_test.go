package sqlite

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSQLiteFiltersParentalOptionsAndTaxonomies(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "cov_v0100.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := NewLibraryRepo(store)

	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{"/movies"})
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}
	if !lib.SaveLocalMetadataEnabled() {
		t.Fatalf("expected SaveLocalMetadataEnabled default true")
	}
	if (*LibraryRecord)(nil).SaveLocalMetadataEnabled() != true {
		t.Fatalf("expected nil LibraryRecord SaveLocalMetadataEnabled true")
	}

	updatedLib, err := repo.UpdateLibraryOptions(ctx, lib.ID, map[string]any{"SaveLocalMetadata": false, "CustomKey": "v"})
	if err != nil {
		t.Fatalf("UpdateLibraryOptions: %v", err)
	}
	if updatedLib.SaveLocalMetadataEnabled() {
		t.Fatalf("expected SaveLocalMetadataEnabled false after update")
	}
	if opts := ParseLibraryOptions(updatedLib.OptionsJSON); opts["CustomKey"] != "v" {
		t.Fatalf("expected CustomKey=v, got %+v", opts)
	}

	y2024 := 2024
	rItem := &ItemRecord{
		ID:             "m-r-rated",
		LibraryID:      lib.ID,
		ParentID:       lib.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Restricted Movie",
		OfficialRating: "R",
		ProductionYear: &y2024,
	}
	if err := repo.UpsertItem(ctx, rItem); err != nil {
		t.Fatalf("UpsertItem: %v", err)
	}
	if err := repo.ReplaceItemTaxonomies(ctx, rItem.ID, "genre", []string{"Sci-Fi"}); err != nil {
		t.Fatalf("ReplaceItemTaxonomies genre: %v", err)
	}
	if err := repo.ReplaceItemTaxonomies(ctx, rItem.ID, "studio", []string{"A24"}); err != nil {
		t.Fatalf("ReplaceItemTaxonomies studio: %v", err)
	}
	if err := repo.ReplaceItemTaxonomies(ctx, rItem.ID, "tag", []string{"NSFW", "Trending"}); err != nil {
		t.Fatalf("ReplaceItemTaxonomies tag: %v", err)
	}

	genres, studios, tags, err := repo.GetTaxonomiesByItemID(ctx, rItem.ID)
	if err != nil || len(genres) != 1 || len(studios) != 1 || len(tags) != 2 {
		t.Fatalf("GetTaxonomiesByItemID: g=%v s=%v t=%v err=%v", genres, studios, tags, err)
	}

	// Parental helpers
	if len(ExceedingParentalRatings(7)) == 0 {
		t.Fatalf("expected ratings exceeding 7")
	}
	maxPG13 := 7
	blocked, err := repo.IsItemBlockedByParentalPolicy(ctx, rItem, &maxPG13, nil)
	if err != nil || !blocked {
		t.Fatalf("expected R movie blocked by maxRating=7, got blocked=%v err=%v", blocked, err)
	}
	blocked, err = repo.IsItemBlockedByParentalPolicy(ctx, rItem, nil, []string{"nsfw", "NSFW", " "})
	if err != nil || !blocked {
		t.Fatalf("expected movie blocked by NSFW tag, got blocked=%v err=%v", blocked, err)
	}
	blocked, err = repo.IsItemBlockedByParentalPolicy(ctx, nil, &maxPG13, []string{"nsfw"})
	if err != nil || blocked {
		t.Fatalf("expected nil record not blocked, got blocked=%v err=%v", blocked, err)
	}

	// GetQueryFilters (unrestricted + restricted + empty allowed)
	qf, err := repo.GetQueryFilters(ctx, QueryFiltersFilter{
		ParentID:         lib.ID,
		IncludeItemTypes: []string{"Movie"},
	})
	if err != nil || len(qf.Genres) != 1 || len(qf.Studios) != 1 || len(qf.Tags) != 2 || len(qf.Years) != 1 || len(qf.OfficialRatings) != 1 {
		t.Fatalf("GetQueryFilters unexpected: %+v err=%v", qf, err)
	}
	qfEmpty, err := repo.GetQueryFilters(ctx, QueryFiltersFilter{AllowedLibraryIDs: []string{}})
	if err != nil || len(qfEmpty.Genres) != 0 {
		t.Fatalf("GetQueryFilters empty allowed unexpected: %+v err=%v", qfEmpty, err)
	}
	qfBlocked, err := repo.GetQueryFilters(ctx, QueryFiltersFilter{
		AllowedLibraryIDs: []string{lib.ID},
		MaxParentalRating: &maxPG13,
		BlockedTags:       []string{"NSFW"},
	})
	if err != nil || len(qfBlocked.Genres) != 0 {
		t.Fatalf("GetQueryFilters parental blocked unexpected: %+v err=%v", qfBlocked, err)
	}

	// GetResumeItemsWithAllowedLibraries
	items, total, err := repo.GetResumeItemsWithAllowedLibraries(ctx, "srv1", "u1", "", []string{"Movie"}, 0, 10, nil, []string{lib.ID})
	if err != nil || total != 0 || len(items) != 0 {
		t.Fatalf("GetResumeItemsWithAllowedLibraries unexpected: total=%d len=%d err=%v", total, len(items), err)
	}
}
