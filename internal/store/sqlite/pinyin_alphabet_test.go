package sqlite

import (
	"context"
	"path/filepath"
	"testing"
)

func TestChinesePinyinAlphabetIndex(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "pinyin_az.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	repo := NewLibraryRepo(store)
	ctx := context.Background()

	lib, err := repo.CreateLibrary(ctx, "电影", "movies", []string{"/movies"})
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}

	samples := []struct {
		id       string
		name     string
		sortName string
	}{
		{"m-x", "肖申克的救赎", "肖申克的救赎"},
		{"m-b", "霸王别姬", "霸王别姬"},
		{"m-h", "海上钢琴师", "海上钢琴师"},
		{"m-a", "阿甘正传", "阿甘正传"},
		{"m-s", "《三体》", "《三体》"},
		{"m-num", "2001太空漫游", "2001太空漫游"},
		{"m-en", "Batman Begins", "batman begins"},
	}
	for _, s := range samples {
		if err := repo.UpsertItem(ctx, &ItemRecord{
			ID:        s.id,
			LibraryID: lib.ID,
			ParentID:  lib.ID,
			Type:      "Movie",
			MediaType: "Video",
			Name:      s.name,
			SortName:  s.sortName,
		}); err != nil {
			t.Fatalf("UpsertItem %s: %v", s.id, err)
		}
	}

	// X -> 肖申克的救赎
	items, total, err := repo.QueryItems(ctx, ItemFilter{
		LibraryID:        lib.ID,
		IncludeItemTypes: []string{"Movie"},
		NameStartsWith:   "x",
	})
	if err != nil || total != 1 || len(items) != 1 || items[0].Name != "肖申克的救赎" {
		t.Fatalf("NameStartsWith=x expected 肖申克的救赎, got total=%d items=%+v err=%v", total, items, err)
	}

	// B -> 霸王别姬 + Batman Begins
	items, total, err = repo.QueryItems(ctx, ItemFilter{
		LibraryID:        lib.ID,
		IncludeItemTypes: []string{"Movie"},
		NameStartsWith:   "b",
	})
	if err != nil || total != 2 {
		t.Fatalf("NameStartsWith=b expected 2 items (霸王别姬 + Batman Begins), got total=%d items=%+v err=%v", total, items, err)
	}

	// S -> 《三体》 (leading CJK book-title brackets stripped)
	items, total, err = repo.QueryItems(ctx, ItemFilter{
		LibraryID:        lib.ID,
		IncludeItemTypes: []string{"Movie"},
		NameStartsWith:   "s",
	})
	if err != nil || total != 1 || items[0].Name != "《三体》" {
		t.Fatalf("NameStartsWith=s expected 《三体》, got total=%d items=%+v err=%v", total, items, err)
	}

	// '#' (NameLessThan="a") -> 2001太空漫游 only (Chinese titles must NOT leak into '#')
	items, total, err = repo.QueryItems(ctx, ItemFilter{
		LibraryID:        lib.ID,
		IncludeItemTypes: []string{"Movie"},
		NameLessThan:     "a",
	})
	if err != nil || total != 1 || items[0].Name != "2001太空漫游" {
		t.Fatalf("NameLessThan=a expected 2001太空漫游 only, got total=%d items=%+v err=%v", total, items, err)
	}

	// SortName ascending orders Chinese + English by Pinyin: 2001 -> 阿甘(a) -> 霸王(b)/Batman(b) -> 海上(h) -> 三体(s) -> 肖申克(x)
	items, _, err = repo.QueryItems(ctx, ItemFilter{
		LibraryID:        lib.ID,
		IncludeItemTypes: []string{"Movie"},
		SortBy:           []string{"SortName"},
		SortOrder:        "Ascending",
	})
	if err != nil || len(items) != len(samples) {
		t.Fatalf("SortName query failed: len=%d err=%v", len(items), err)
	}
	if items[0].Name != "2001太空漫游" || items[1].Name != "阿甘正传" || items[len(items)-1].Name != "肖申克的救赎" {
		t.Fatalf("unexpected pinyin SortName order: first=%q second=%q last=%q", items[0].Name, items[1].Name, items[len(items)-1].Name)
	}
}
