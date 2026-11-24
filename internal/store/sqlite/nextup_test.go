package sqlite

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/pkg/guid"
)

func TestGetNextUpEpisodesDeduplicatesAndPrioritizes(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "nextup.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	var nilUserRepo *UserRepo
	if nilUserRepo.Store() != nil {
		t.Fatal("expected nil UserRepo.Store() to return nil")
	}

	userRepo := NewUserRepo(store)
	if userRepo.Store() != store {
		t.Fatal("expected UserRepo.Store() to return underlying store")
	}
	libRepo := NewLibraryRepo(store)

	user := &UserRecord{
		ID:        guid.New(),
		Name:      "viewer",
		Policy:    DefaultUserPolicy(false),
		Config:    DefaultUserConfig(),
		CreatedAt: time.Now().UTC(),
	}
	if err := userRepo.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	lib1, err := libRepo.CreateLibrary(ctx, "TV 1", "tvshows", []string{"/tv1"})
	if err != nil {
		t.Fatalf("CreateLibrary lib1: %v", err)
	}
	lib2, err := libRepo.CreateLibrary(ctx, "TV 2", "tvshows", []string{"/tv2"})
	if err != nil {
		t.Fatalf("CreateLibrary lib2: %v", err)
	}

	// Series A in lib1 with 30 unplayed episodes (would previously exhaust LIMIT=2).
	seriesA := guid.New()
	seasonA := guid.New()
	if err := libRepo.UpsertItem(ctx, &ItemRecord{
		ID: seriesA, LibraryID: lib1.ID, ParentID: lib1.ID,
		Type: "Series", IsFolder: true, Name: "Long Show", SortName: "long show",
		Path: "/tv1/LongShow", Etag: "sa",
	}); err != nil {
		t.Fatalf("UpsertItem seriesA: %v", err)
	}
	s1 := 1
	if err := libRepo.UpsertItem(ctx, &ItemRecord{
		ID: seasonA, LibraryID: lib1.ID, ParentID: seriesA, SeriesID: seriesA,
		Type: "Season", IsFolder: true, Name: "Season 1", SortName: "0001",
		IndexNumber: &s1, Path: "/tv1/LongShow/S01", Etag: "sea",
	}); err != nil {
		t.Fatalf("UpsertItem seasonA: %v", err)
	}
	for ep := 1; ep <= 30; ep++ {
		epNum := ep
		if err := libRepo.UpsertItem(ctx, &ItemRecord{
			ID: guid.New(), LibraryID: lib1.ID, ParentID: seasonA, SeriesID: seriesA, SeasonID: seasonA,
			Type: "Episode", MediaType: "Video",
			Name: fmt.Sprintf("Long Ep %02d", ep), SortName: fmt.Sprintf("001-%04d", ep),
			IndexNumber: &epNum, ParentIndexNumber: &s1,
			Path: fmt.Sprintf("/tv1/LongShow/S01/E%02d.mkv", ep), Etag: fmt.Sprintf("ea%d", ep),
		}); err != nil {
			t.Fatalf("UpsertItem seriesA ep %d: %v", ep, err)
		}
	}

	// Series B in lib2 with 3 episodes.
	seriesB := guid.New()
	seasonB := guid.New()
	if err := libRepo.UpsertItem(ctx, &ItemRecord{
		ID: seriesB, LibraryID: lib2.ID, ParentID: lib2.ID,
		Type: "Series", IsFolder: true, Name: "Short Show", SortName: "short show",
		Path: "/tv2/ShortShow", Etag: "sb",
	}); err != nil {
		t.Fatalf("UpsertItem seriesB: %v", err)
	}
	if err := libRepo.UpsertItem(ctx, &ItemRecord{
		ID: seasonB, LibraryID: lib2.ID, ParentID: seriesB, SeriesID: seriesB,
		Type: "Season", IsFolder: true, Name: "Season 1", SortName: "0001",
		IndexNumber: &s1, Path: "/tv2/ShortShow/S01", Etag: "seb",
	}); err != nil {
		t.Fatalf("UpsertItem seasonB: %v", err)
	}
	var seriesBEp1ID string
	for ep := 1; ep <= 3; ep++ {
		epNum := ep
		id := guid.New()
		if ep == 1 {
			seriesBEp1ID = id
		}
		if err := libRepo.UpsertItem(ctx, &ItemRecord{
			ID: id, LibraryID: lib2.ID, ParentID: seasonB, SeriesID: seriesB, SeasonID: seasonB,
			Type: "Episode", MediaType: "Video",
			Name: fmt.Sprintf("Short Ep %02d", ep), SortName: fmt.Sprintf("001-%04d", ep),
			IndexNumber: &epNum, ParentIndexNumber: &s1,
			Path: fmt.Sprintf("/tv2/ShortShow/S01/E%02d.mkv", ep), Etag: fmt.Sprintf("eb%d", ep),
		}); err != nil {
			t.Fatalf("UpsertItem seriesB ep %d: %v", ep, err)
		}
	}

	// 1. Both unplayed: Limit=2 MUST return BOTH series (1 episode each), not truncated to 1!
	items, total, err := libRepo.GetNextUpEpisodes(ctx, "srv1", user.ID, "", 2, nil)
	if err != nil {
		t.Fatalf("GetNextUpEpisodes(limit=2): %v", err)
	}
	if len(items) != 2 || total != 2 {
		t.Fatalf("expected 2 distinct series episodes, got len=%d total=%d", len(items), total)
	}
	seen := map[string]bool{}
	for _, it := range items {
		seen[it.SeriesId] = true
		if it.IndexNumber == nil || *it.IndexNumber != 1 {
			t.Fatalf("expected episode 1 for unplayed series %s, got %v", it.SeriesId, it.IndexNumber)
		}
	}
	if !seen[seriesA] || !seen[seriesB] {
		t.Fatalf("expected both seriesA and seriesB, got %+v", seen)
	}

	// 2. Mark Series B Episode 1 as played -> Series B Episode 2 must be prioritized FIRST!
	if _, err := libRepo.MarkPlayed(ctx, user.ID, seriesBEp1ID); err != nil {
		t.Fatalf("MarkPlayed: %v", err)
	}
	items, total, err = libRepo.GetNextUpEpisodes(ctx, "srv1", user.ID, "", 2, nil)
	if err != nil {
		t.Fatalf("GetNextUpEpisodes after MarkPlayed: %v", err)
	}
	if len(items) != 2 || total != 2 {
		t.Fatalf("expected 2 items, got len=%d total=%d", len(items), total)
	}
	if items[0].SeriesId != seriesB || items[0].IndexNumber == nil || *items[0].IndexNumber != 2 {
		t.Fatalf("expected started seriesB ep 2 first, got series=%s ep=%v", items[0].SeriesId, items[0].IndexNumber)
	}
	if items[1].SeriesId != seriesA || items[1].IndexNumber == nil || *items[1].IndexNumber != 1 {
		t.Fatalf("expected unplayed seriesA ep 1 second, got series=%s ep=%v", items[1].SeriesId, items[1].IndexNumber)
	}

	// 3. AllowedLibraryIDs filtering
	emptyItems, emptyTotal, err := libRepo.GetNextUpEpisodesWithAllowedLibraries(ctx, "srv1", user.ID, "", 10, nil, []string{})
	if err != nil || len(emptyItems) != 0 || emptyTotal != 0 {
		t.Fatalf("expected empty result for empty allowedLibraryIDs, got len=%d total=%d err=%v", len(emptyItems), emptyTotal, err)
	}
	lib1Items, lib1Total, err := libRepo.GetNextUpEpisodesWithAllowedLibraries(ctx, "srv1", user.ID, "", 10, nil, []string{lib1.ID})
	if err != nil || len(lib1Items) != 1 || lib1Total != 1 || lib1Items[0].SeriesId != seriesA {
		t.Fatalf("expected only seriesA for lib1, got len=%d total=%d items=%+v err=%v", len(lib1Items), lib1Total, lib1Items, err)
	}
}
