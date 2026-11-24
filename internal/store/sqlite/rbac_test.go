package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/pkg/guid"
)

func TestAllowedLibraryIDsFiltering(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "rbac_repo.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := NewLibraryRepo(store)
	ctx := context.Background()

	libA, err := repo.CreateLibrary(ctx, "LibA", "movies", []string{"/a"})
	if err != nil {
		t.Fatal(err)
	}
	libB, err := repo.CreateLibrary(ctx, "LibB", "movies", []string{"/b"})
	if err != nil {
		t.Fatal(err)
	}

	itemA := &ItemRecord{ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID, Type: "Movie", Name: "Space Odyssey A"}
	itemB := &ItemRecord{ID: guid.New(), LibraryID: libB.ID, ParentID: libB.ID, Type: "Movie", Name: "Space Odyssey B"}
	if err := repo.UpsertItem(ctx, itemA); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertItem(ctx, itemB); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertSearchFTS(ctx, itemA.ID, itemA.Name, "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertSearchFTS(ctx, itemB.ID, itemB.Name, "", "", "", ""); err != nil {
		t.Fatal(err)
	}

	// nil AllowedLibraryIDs -> unrestricted (2 items)
	items, total, err := repo.QueryItems(ctx, ItemFilter{Recursive: true})
	if err != nil || total != 2 || len(items) != 2 {
		t.Fatalf("unrestricted QueryItems got len=%d total=%d err=%v", len(items), total, err)
	}

	// non-nil empty AllowedLibraryIDs -> 0 items
	items, total, err = repo.QueryItems(ctx, ItemFilter{Recursive: true, AllowedLibraryIDs: []string{}})
	if err != nil || total != 0 || len(items) != 0 {
		t.Fatalf("empty AllowedLibraryIDs QueryItems got len=%d total=%d err=%v", len(items), total, err)
	}

	// restricted to libA -> 1 item (itemA)
	items, total, err = repo.QueryItems(ctx, ItemFilter{Recursive: true, AllowedLibraryIDs: []string{libA.ID}})
	if err != nil || total != 1 || items[0].Id != itemA.ID {
		t.Fatalf("restricted QueryItems got len=%d total=%d err=%v", len(items), total, err)
	}

	// GetLatestItemsWithAllowedLibraries
	latest, err := repo.GetLatestItemsWithAllowedLibraries(ctx, "srv", "", "", nil, 10, nil, []string{libA.ID})
	if err != nil || len(latest) != 1 || latest[0].Id != itemA.ID {
		t.Fatalf("restricted GetLatestItems got len=%d err=%v", len(latest), err)
	}
	latest, err = repo.GetLatestItemsWithAllowedLibraries(ctx, "srv", "", "", nil, 10, nil, []string{})
	if err != nil || len(latest) != 0 {
		t.Fatalf("empty GetLatestItems got len=%d err=%v", len(latest), err)
	}

	// SearchHints
	hints, hTotal, err := repo.SearchHints(ctx, SearchHintFilter{SearchTerm: "Space", AllowedLibraryIDs: nil})
	if err != nil || hTotal != 2 || len(hints) != 2 {
		t.Fatalf("unrestricted SearchHints got len=%d total=%d err=%v", len(hints), hTotal, err)
	}
	hints, hTotal, err = repo.SearchHints(ctx, SearchHintFilter{SearchTerm: "Space", AllowedLibraryIDs: []string{}})
	if err != nil || hTotal != 0 || len(hints) != 0 {
		t.Fatalf("empty SearchHints got len=%d total=%d err=%v", len(hints), hTotal, err)
	}
	hints, hTotal, err = repo.SearchHints(ctx, SearchHintFilter{SearchTerm: "Space", AllowedLibraryIDs: []string{libB.ID}})
	if err != nil || hTotal != 1 || hints[0].Id != itemB.ID {
		t.Fatalf("restricted SearchHints got len=%d total=%d err=%v", len(hints), hTotal, err)
	}
}
