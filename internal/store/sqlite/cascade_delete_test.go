package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/pkg/guid"
)

func TestDeleteItemCascadesDescendantsImagesAndFTS(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "cascade.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	repo := NewLibraryRepo(store)
	ctx := context.Background()

	lib, err := repo.CreateLibrary(ctx, "TV", "tvshows", []string{"/tv"})
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}

	now := time.Now().Unix()
	seriesID := guid.New()
	seasonID := guid.New()
	ep1ID := guid.New()
	ep2ID := guid.New()
	otherMovieID := guid.New()

	records := []*ItemRecord{
		{
			ID: seriesID, LibraryID: lib.ID, ParentID: lib.ID,
			Type: "Series", MediaType: "", IsFolder: true,
			Name: "Cascade Show", SortName: "cascade show",
			DateCreated: now, DateModified: now,
		},
		{
			ID: seasonID, LibraryID: lib.ID, ParentID: seriesID, SeriesID: seriesID,
			Type: "Season", MediaType: "", IsFolder: true,
			Name: "Season 1", SortName: "season 1",
			DateCreated: now, DateModified: now,
		},
		{
			ID: ep1ID, LibraryID: lib.ID, ParentID: seasonID, SeriesID: seriesID, SeasonID: seasonID,
			Type: "Episode", MediaType: "Video",
			Name: "Episode 1", SortName: "episode 1", Path: "/tv/show/s01e01.mkv",
			DateCreated: now, DateModified: now,
		},
		{
			ID: ep2ID, LibraryID: lib.ID, ParentID: seasonID, SeriesID: seriesID, SeasonID: seasonID,
			Type: "Episode", MediaType: "Video",
			Name: "Episode 2", SortName: "episode 2", Path: "/tv/show/s01e02.mkv",
			DateCreated: now, DateModified: now,
		},
		{
			ID: otherMovieID, LibraryID: lib.ID, ParentID: lib.ID,
			Type: "Movie", MediaType: "Video",
			Name: "Unrelated Movie", SortName: "unrelated movie", Path: "/tv/other.mkv",
			DateCreated: now, DateModified: now,
		},
	}

	for _, rec := range records {
		if err := repo.UpsertItem(ctx, rec); err != nil {
			t.Fatalf("UpsertItem(%s): %v", rec.Name, err)
		}
		if err := repo.UpsertImage(ctx, &ImageRecord{
			ItemID:    rec.ID,
			ImageType: "Primary",
			Path:      "/img/" + rec.ID + ".jpg",
			Tag:       "tag-" + rec.ID,
		}); err != nil {
			t.Fatalf("UpsertImage(%s): %v", rec.Name, err)
		}
		if err := repo.UpsertSearchFTS(ctx, rec.ID, rec.Name, rec.Name, "", "", ""); err != nil {
			t.Fatalf("UpsertSearchFTS(%s): %v", rec.Name, err)
		}
	}

	if err := repo.ReplaceItemAncestors(ctx, seasonID, []AncestorLink{{AncestorID: seriesID, Depth: 1}, {AncestorID: lib.ID, Depth: 2}}); err != nil {
		t.Fatalf("ReplaceItemAncestors(season): %v", err)
	}
	if err := repo.ReplaceItemAncestors(ctx, ep1ID, []AncestorLink{{AncestorID: seasonID, Depth: 1}, {AncestorID: seriesID, Depth: 2}}); err != nil {
		t.Fatalf("ReplaceItemAncestors(ep1): %v", err)
	}

	// Delete the Series: must cascade to Season, both Episodes, their item_images, item_search_fts, and item_ancestors.
	if err := repo.DeleteItem(ctx, seriesID); err != nil {
		t.Fatalf("DeleteItem(series): %v", err)
	}

	for _, deletedID := range []string{seriesID, seasonID, ep1ID, ep2ID} {
		if _, err := repo.GetItemRecordByID(ctx, deletedID); !errors.Is(err, ErrNotFound) {
			t.Errorf("expected item %s to be deleted, got err=%v", deletedID, err)
		}
		var imgCount int
		_ = store.Reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM item_images WHERE item_id = ?`, deletedID).Scan(&imgCount)
		if imgCount != 0 {
			t.Errorf("expected 0 item_images for %s, got %d", deletedID, imgCount)
		}
		var ftsCount int
		_ = store.Reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM item_search_fts WHERE item_id = ?`, deletedID).Scan(&ftsCount)
		if ftsCount != 0 {
			t.Errorf("expected 0 item_search_fts for %s, got %d", deletedID, ftsCount)
		}
		var ancCount int
		_ = store.Reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM item_ancestors WHERE ancestor_id = ? OR descendant_id = ?`, deletedID, deletedID).Scan(&ancCount)
		if ancCount != 0 {
			t.Errorf("expected 0 item_ancestors for %s, got %d", deletedID, ancCount)
		}
	}

	// Unrelated movie and its image/FTS must remain intact.
	if _, err := repo.GetItemRecordByID(ctx, otherMovieID); err != nil {
		t.Fatalf("expected unrelated movie to remain, got err=%v", err)
	}
	var remainImgs int
	_ = store.Reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM item_images WHERE item_id = ?`, otherMovieID).Scan(&remainImgs)
	if remainImgs != 1 {
		t.Fatalf("expected unrelated movie image to remain, got %d", remainImgs)
	}

	// Deleting missing item returns ErrNotFound.
	if err := repo.DeleteItem(ctx, guid.New()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing item, got %v", err)
	}
}

func TestListLibraryItemPathsAndDeleteItemByPath(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "paths.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	repo := NewLibraryRepo(store)
	ctx := context.Background()

	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{"/movies"})
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}

	now := time.Now().Unix()
	singleID := guid.New()
	multiID := guid.New()

	if err := repo.UpsertItem(ctx, &ItemRecord{
		ID: singleID, LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video",
		Name: "Single Movie", SortName: "single movie",
		Path: "/movies/single.mkv", Container: "mkv",
		DateCreated: now, DateModified: now,
	}); err != nil {
		t.Fatalf("UpsertItem(single): %v", err)
	}

	if err := repo.UpsertItem(ctx, &ItemRecord{
		ID: multiID, LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video",
		Name: "Multi Version", SortName: "multi version",
		Path: "/movies/multi.1080p.mkv", Container: "mkv",
		DateCreated: now, DateModified: now,
	}); err != nil {
		t.Fatalf("UpsertItem(multi): %v", err)
	}
	if err := repo.UpsertMediaSourceAndStreams(ctx, multiID, MediaSourceRecord{
		ID: guid.New(), ItemID: multiID, Name: "1080p",
		Path: "/movies/multi.1080p.mkv", Container: "mkv", Size: 1000,
	}); err != nil {
		t.Fatalf("UpsertMediaSource(1080p): %v", err)
	}
	if err := repo.UpsertMediaSourceAndStreams(ctx, multiID, MediaSourceRecord{
		ID: guid.New(), ItemID: multiID, Name: "4K",
		Path: "/movies/multi.4k.mp4", Container: "mp4", Size: 4000,
	}); err != nil {
		t.Fatalf("UpsertMediaSource(4k): %v", err)
	}

	paths, err := repo.ListLibraryItemPaths(ctx, lib.ID)
	if err != nil {
		t.Fatalf("ListLibraryItemPaths: %v", err)
	}
	if len(paths) != 3 {
		t.Fatalf("expected 3 paths, got %d: %+v", len(paths), paths)
	}
	if paths["/movies/single.mkv"] != singleID || paths["/movies/multi.1080p.mkv"] != multiID || paths["/movies/multi.4k.mp4"] != multiID {
		t.Fatalf("unexpected path map: %+v", paths)
	}

	// Non-existent path -> false, nil.
	deleted, err := repo.DeleteItemByPath(ctx, lib.ID, "/movies/nope.mkv")
	if err != nil || deleted {
		t.Fatalf("DeleteItemByPath(nope) = (%v, %v), want (false, nil)", deleted, err)
	}

	// Delete secondary (non-primary) media source first to cover non-promotion path.
	if err := repo.UpsertMediaSourceAndStreams(ctx, multiID, MediaSourceRecord{
		ID: guid.New(), ItemID: multiID, Name: "720p",
		Path: "/movies/multi.720p.mkv", Container: "mkv", Size: 500,
	}); err != nil {
		t.Fatalf("UpsertMediaSource(720p): %v", err)
	}
	deleted, err = repo.DeleteItemByPath(ctx, lib.ID, "/movies/multi.720p.mkv")
	if err != nil || !deleted {
		t.Fatalf("DeleteItemByPath(multi 720p) = (%v, %v), want (true, nil)", deleted, err)
	}

	// Delete primary media source of multi-version movie -> removes only that source and promotes 4K path.
	deleted, err = repo.DeleteItemByPath(ctx, lib.ID, "/movies/multi.1080p.mkv")
	if err != nil || !deleted {
		t.Fatalf("DeleteItemByPath(multi 1080p) = (%v, %v), want (true, nil)", deleted, err)
	}
	rec, err := repo.GetItemRecordByID(ctx, multiID)
	if err != nil {
		t.Fatalf("expected multi movie to survive with 1 source, got err=%v", err)
	}
	if rec.Path != "/movies/multi.4k.mp4" || rec.Container != "mp4" {
		t.Fatalf("expected promoted path /movies/multi.4k.mp4 (mp4), got %s (%s)", rec.Path, rec.Container)
	}
	sources, err := repo.GetMediaSourcesByItemID(ctx, multiID)
	if err != nil || len(sources) != 1 {
		t.Fatalf("expected 1 remaining media source, got len=%d err=%v", len(sources), err)
	}

	// Delete last remaining source -> deletes the entire item.
	deleted, err = repo.DeleteItemByPath(ctx, lib.ID, "/movies/multi.4k.mp4")
	if err != nil || !deleted {
		t.Fatalf("DeleteItemByPath(multi 4k) = (%v, %v), want (true, nil)", deleted, err)
	}
	if _, err := repo.GetItemRecordByID(ctx, multiID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected multi movie to be deleted, got err=%v", err)
	}

	// Delete single-version movie -> deletes item.
	deleted, err = repo.DeleteItemByPath(ctx, lib.ID, "/movies/single.mkv")
	if err != nil || !deleted {
		t.Fatalf("DeleteItemByPath(single) = (%v, %v), want (true, nil)", deleted, err)
	}
	if _, err := repo.GetItemRecordByID(ctx, singleID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected single movie to be deleted, got err=%v", err)
	}

	// Closed DB error paths.
	_ = store.Close()
	if err := repo.DeleteItem(ctx, singleID); err == nil {
		t.Error("expected DeleteItem error on closed DB")
	}
	if _, err := repo.ListLibraryItemPaths(ctx, lib.ID); err == nil {
		t.Error("expected ListLibraryItemPaths error on closed DB")
	}
	if _, err := repo.DeleteItemByPath(ctx, lib.ID, "/movies/single.mkv"); err == nil {
		t.Error("expected DeleteItemByPath error on closed DB")
	}
}
