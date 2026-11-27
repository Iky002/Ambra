package governance

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestAuditV0112_NFOLibraryBoundaryAndCleanupOrphansExtrasAndCountCache(t *testing.T) {
	ctx := context.Background()
	svc, store, repo, _, _, _ := newTestEnv(t)
	svc.SetLibraryRepo(repo)

	libRoot := filepath.Join(t.TempDir(), "movies-root")
	outsideDir := filepath.Join(t.TempDir(), "outside-secret")
	if err := os.MkdirAll(libRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Fatal(err)
	}

	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{libRoot})
	if err != nil {
		t.Fatal(err)
	}

	// 1. M-14: Symlinked parent folder inside libRoot pointing outside libRoot MUST be rejected by WriteItemNFOAtomic!
	escapeLinkDir := filepath.Join(libRoot, "symlink-escape")
	if err := os.Symlink(outsideDir, escapeLinkDir); err != nil {
		t.Fatal(err)
	}
	escapedMoviePath := filepath.Join(escapeLinkDir, "film.mkv")
	_ = os.WriteFile(escapedMoviePath, []byte("x"), 0o644)

	escapedItem := &sqlite.ItemRecord{
		ID:        guid.New(),
		LibraryID: lib.ID,
		ParentID:  lib.ID,
		Type:      "Movie",
		Name:      "Escape Attempt",
		Path:      escapedMoviePath,
	}
	if err := repo.UpsertItem(ctx, escapedItem); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.WriteItemNFOAtomic(ctx, escapedItem.ID); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument when NFO directory escapes registered library root via symlink, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(outsideDir, "film.nfo")); !os.IsNotExist(statErr) {
		t.Fatal("film.nfo was written outside library root!")
	}

	// 2. M-14: CleanupOrphans cascade-deletes child extras (extra_type != '') and invalidates LibraryRepo count cache!
	orphanMovie := &sqlite.ItemRecord{
		ID:        guid.New(),
		LibraryID: lib.ID,
		ParentID:  lib.ID,
		Type:      "Movie",
		Name:      "Deleted On Disk",
		Path:      filepath.Join(libRoot, "deleted-movie.mkv"),
	}
	orphanTrailer := &sqlite.ItemRecord{
		ID:        guid.New(),
		LibraryID: lib.ID,
		ParentID:  orphanMovie.ID,
		Type:      "Video",
		Name:      "Deleted Movie Trailer",
		Path:      filepath.Join(libRoot, "deleted-movie-trailer.mkv"),
		Extra:     sqlite.ItemExtra{ExtraType: "trailer"},
	}
	if err := repo.UpsertItem(ctx, orphanMovie); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertItem(ctx, orphanTrailer); err != nil {
		t.Fatal(err)
	}
	_ = repo.ReplaceItemAncestors(ctx, orphanTrailer.ID, []sqlite.AncestorLink{{AncestorID: orphanMovie.ID, Depth: 1}})
	_ = repo.UpsertImage(ctx, &sqlite.ImageRecord{ItemID: orphanTrailer.ID, ImageType: "Primary", ImageIndex: 0, Path: "/tmp/x.jpg", Tag: "t"})

	// Warm the LibraryRepo item count cache before CleanupOrphans!
	beforeCounts, err := repo.GetItemCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if beforeCounts.MovieCount != 2 || beforeCounts.TrailerCount != 1 {
		t.Fatalf("unexpected warm cache counts before CleanupOrphans: %+v", beforeCounts)
	}

	cleanRes, err := svc.CleanupOrphans(ctx, []string{orphanMovie.ID})
	if err != nil || cleanRes.Deleted != 1 {
		t.Fatalf("CleanupOrphans failed: res=%+v err=%v", cleanRes, err)
	}

	// Child trailer must also be purged from items, item_images, and item_ancestors!
	var remTrailer int
	_ = store.Reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM items WHERE id = ?`, guid.Normalize(orphanTrailer.ID)).Scan(&remTrailer)
	if remTrailer != 0 {
		t.Fatal("expected child extra trailer deleted when parent orphan is cleaned up")
	}

	// Cached item counts must reflect deletion immediately (cache was invalidated!).
	afterCounts, err := repo.GetItemCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if afterCounts.MovieCount != 1 || afterCounts.TrailerCount != 0 {
		t.Fatalf("expected count cache invalidated after CleanupOrphans (Movie=1, Trailer=0), got %+v", afterCounts)
	}
}
