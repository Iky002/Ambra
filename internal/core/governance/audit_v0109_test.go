package governance

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestAuditV0109_M38_CleanupImagesDismissPendingAndFTSPeople(t *testing.T) {
	svc, _, repo, _, _, _ := newTestEnv(t)
	ctx := context.Background()
	mediaDir := t.TempDir()
	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{mediaDir})
	if err != nil {
		t.Fatal(err)
	}

	// 1. CleanupOrphans deletes item_images rows too.
	orphan := &sqlite.ItemRecord{
		LibraryID: lib.ID,
		Type:      "Movie",
		Name:      "Orphaned",
		Path:      filepath.Join(mediaDir, "gone.mkv"),
	}
	if err := repo.UpsertItem(ctx, orphan); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceItemImages(ctx, orphan.ID, []sqlite.ImageRecord{
		{ItemID: orphan.ID, ImageType: "Primary", Path: filepath.Join(mediaDir, "missing.jpg")},
	}); err != nil {
		t.Fatal(err)
	}
	res, err := svc.CleanupOrphans(ctx, []string{orphan.ID})
	if err != nil || res.Deleted != 1 {
		t.Fatalf("CleanupOrphans: res=%+v err=%v", res, err)
	}
	var imgCount int
	if err := svc.store.Reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM item_images WHERE item_id = ?`, orphan.ID).Scan(&imgCount); err != nil {
		t.Fatal(err)
	}
	if imgCount != 0 {
		t.Fatalf("expected item_images cleaned up for orphan, got %d", imgCount)
	}

	// 2. DismissAudit rejects already-dismissed or already-approved items.
	moviePath := filepath.Join(mediaDir, "inception.mkv")
	if err := os.WriteFile(moviePath, []byte("vid"), 0o644); err != nil {
		t.Fatal(err)
	}
	mov := &sqlite.ItemRecord{
		LibraryID: lib.ID,
		Type:      "Movie",
		Name:      "Inception",
		Path:      moviePath,
	}
	if err := repo.UpsertItem(ctx, mov); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceItemPeople(ctx, mov.ID, []sqlite.PersonInput{
		{Name: "克里斯托弗·诺兰", Type: "Director"},
	}); err != nil {
		t.Fatal(err)
	}

	auditItem, err := svc.EnqueueAudit(ctx, EnqueueAuditInput{
		ItemID:   mov.ID,
		ItemName: mov.Name,
		ItemType: "Movie",
		Path:     moviePath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DismissAudit(ctx, auditItem.ID); err != nil {
		t.Fatalf("first DismissAudit: %v", err)
	}
	if _, err := svc.DismissAudit(ctx, auditItem.ID); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument when dismissing non-pending audit, got %v", err)
	}

	// 3. ApproveAudit rebuilds FTS people_names from raw item_people JOIN people (no token inflation).
	audit2, err := svc.EnqueueAudit(ctx, EnqueueAuditInput{
		ItemID:   mov.ID,
		ItemName: mov.Name,
		ItemType: "Movie",
		Path:     moviePath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApproveAudit(ctx, audit2.ID, ScrapeCandidate{Title: "盗梦空间"}, nil); err != nil {
		t.Fatalf("ApproveAudit: %v", err)
	}
	var peopleNames, pinyinFull string
	if err := svc.store.Reader.QueryRowContext(ctx,
		`SELECT people_names, pinyin_full FROM item_search_fts WHERE item_id = ?`, mov.ID,
	).Scan(&peopleNames, &pinyinFull); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(peopleNames, "诺兰") || !strings.Contains(pinyinFull, "nuolan") {
		t.Fatalf("expected FTS rebuilt from item_people, got people=%q pinyin=%q", peopleNames, pinyinFull)
	}
}

func TestAuditV0109_M39_L30_NFOTypesAiredAndPhysicalPosterAudit(t *testing.T) {
	svc, _, repo, _, _, _ := newTestEnv(t)
	ctx := context.Background()
	mediaDir := t.TempDir()
	lib, err := repo.CreateLibrary(ctx, "Mixed", "tvshows", []string{mediaDir})
	if err != nil {
		t.Fatal(err)
	}

	// Season, MusicArtist, MusicAlbum, Episode (<aired>), and unsupported BoxSet
	seasonDir := filepath.Join(mediaDir, "Show", "Season 01")
	artistDir := filepath.Join(mediaDir, "Artist")
	albumDir := filepath.Join(artistDir, "Album")
	for _, d := range []string{seasonDir, albumDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	epPath := filepath.Join(seasonDir, "S01E01.mkv")
	if err := os.WriteFile(epPath, []byte("ep"), 0o644); err != nil {
		t.Fatal(err)
	}
	airedUnix := time.Date(2024, 5, 10, 0, 0, 0, 0, time.UTC).Unix()
	sNum := 1
	eNum := 1

	seasonRec := &sqlite.ItemRecord{LibraryID: lib.ID, Type: "Season", IsFolder: true, Name: "Season 1", Path: seasonDir, IndexNumber: &sNum}
	epRec := &sqlite.ItemRecord{LibraryID: lib.ID, Type: "Episode", Name: "Pilot", Path: epPath, ParentIndexNumber: &sNum, IndexNumber: &eNum, PremiereDate: &airedUnix}
	artistRec := &sqlite.ItemRecord{LibraryID: lib.ID, Type: "MusicArtist", IsFolder: true, Name: "Pink Floyd", Path: artistDir}
	albumRec := &sqlite.ItemRecord{LibraryID: lib.ID, Type: "MusicAlbum", IsFolder: true, Name: "The Wall", Path: albumDir}
	unsupported := &sqlite.ItemRecord{LibraryID: lib.ID, Type: "BoxSet", IsFolder: true, Name: "Trilogy", Path: mediaDir}
	for _, r := range []*sqlite.ItemRecord{seasonRec, epRec, artistRec, albumRec, unsupported} {
		if err := repo.UpsertItem(ctx, r); err != nil {
			t.Fatal(err)
		}
	}

	sRes, err := svc.WriteItemNFOAtomic(ctx, seasonRec.ID)
	if err != nil || sRes.RootTag != "season" || filepath.Base(sRes.NFOPath) != "season.nfo" {
		t.Fatalf("season NFO: res=%+v err=%v", sRes, err)
	}
	arRes, err := svc.WriteItemNFOAtomic(ctx, artistRec.ID)
	if err != nil || arRes.RootTag != "artist" || filepath.Base(arRes.NFOPath) != "artist.nfo" {
		t.Fatalf("artist NFO: res=%+v err=%v", arRes, err)
	}
	alRes, err := svc.WriteItemNFOAtomic(ctx, albumRec.ID)
	if err != nil || alRes.RootTag != "album" || filepath.Base(alRes.NFOPath) != "album.nfo" {
		t.Fatalf("album NFO: res=%+v err=%v", alRes, err)
	}
	epRes, err := svc.WriteItemNFOAtomic(ctx, epRec.ID)
	if err != nil || epRes.RootTag != "episodedetails" {
		t.Fatalf("episode NFO: res=%+v err=%v", epRes, err)
	}
	epXML, _ := os.ReadFile(epRes.NFOPath)
	if !strings.Contains(string(epXML), "<aired>2024-05-10</aired>") {
		t.Fatalf("expected <aired> tag in episode NFO, got:\n%s", string(epXML))
	}
	if _, err := svc.WriteItemNFOAtomic(ctx, unsupported.ID); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for unsupported NFO item type, got %v", err)
	}

	// L-30: RunLibraryAudit detects missing or zero-byte physical poster files.
	movPath := filepath.Join(mediaDir, "movie.mkv")
	emptyPoster := filepath.Join(mediaDir, "empty-poster.jpg")
	_ = os.WriteFile(movPath, []byte("m"), 0o644)
	_ = os.WriteFile(emptyPoster, nil, 0o644)
	mov := &sqlite.ItemRecord{
		LibraryID: lib.ID,
		Type:      "Movie",
		Name:      "Empty Poster Movie",
		Path:      movPath,
		Extra:     sqlite.ItemExtra{ProviderIDs: map[string]string{"Tmdb": "123"}},
	}
	_ = repo.UpsertItem(ctx, mov)
	_ = repo.ReplaceItemImages(ctx, mov.ID, []sqlite.ImageRecord{
		{ItemID: mov.ID, ImageType: "Primary", Path: emptyPoster},
	})

	report, err := svc.RunLibraryAudit(ctx)
	if err != nil {
		t.Fatalf("RunLibraryAudit: %v", err)
	}
	foundMissing := false
	for _, item := range report.MissingPosters {
		if item.ID == mov.ID {
			foundMissing = true
			break
		}
	}
	if !foundMissing {
		t.Fatalf("expected 0-byte physical poster to be flagged in MissingPosters, got %+v", report.MissingPosters)
	}
}
