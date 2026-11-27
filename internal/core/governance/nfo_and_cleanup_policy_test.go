package governance

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestAuditV0107_M13_ProviderCaseDedupMultiVersionOrphanAndTxEnqueue(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "gov.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	svc, err := NewService(Options{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	repo := sqlite.NewLibraryRepo(store)
	ctx := context.Background()

	// 1. ApproveAudit removes case-insensitive duplicate provider_ids key ("tmdb" -> "Tmdb")
	mov := &sqlite.ItemRecord{
		ID: "mov-case", LibraryID: "lib1", Type: "Movie", MediaType: "Video",
		Name: "Case Test", Path: filepath.Join(tmp, "case.mkv"),
		Extra: sqlite.ItemExtra{ProviderIDs: map[string]string{"tmdb": "111", "Imdb": "tt111"}},
	}
	if err := repo.UpsertItem(ctx, mov); err != nil {
		t.Fatal(err)
	}
	aud, err := svc.EnqueueAudit(ctx, EnqueueAuditInput{ItemID: mov.ID, ItemName: mov.Name, ItemType: "Movie"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApproveAudit(ctx, aud.ID, ScrapeCandidate{
		Title: "Case Test Fixed", ProviderName: "Tmdb", ProviderID: "222",
	}, nil); err != nil {
		t.Fatalf("ApproveAudit: %v", err)
	}
	var extraRaw string
	if err := store.Reader.QueryRowContext(ctx, `SELECT extra_json FROM items WHERE id = ?`, mov.ID).Scan(&extraRaw); err != nil {
		t.Fatal(err)
	}
	var extraMap map[string]any
	_ = json.Unmarshal([]byte(extraRaw), &extraMap)
	provMap := extractProviderIDs(extraMap)
	if len(provMap) != 2 || provMap["Tmdb"] != "222" || provMap["tmdb"] != "" {
		t.Fatalf("expected case-insensitive deduplicated provider_ids, got %v", provMap)
	}

	// 2. Concurrent EnqueueAudit inside single Writer transaction never creates duplicate pending rows
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = svc.EnqueueAudit(ctx, EnqueueAuditInput{
				ItemID: "mov-concurrent", ItemName: "Concurrent", ItemType: "Movie", Confidence: 0.4,
			})
		}()
	}
	wg.Wait()
	pending, err := svc.ListAudits(ctx, AuditStatusPending)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected exactly 1 deduplicated pending audit row, got %d", len(pending))
	}

	// 3. Multi-version item: primary path missing, secondary media_source exists -> NOT an orphan!
	aliveVersion := filepath.Join(tmp, "multi_4k.mkv")
	if err := os.WriteFile(aliveVersion, []byte("4k"), 0o600); err != nil {
		t.Fatal(err)
	}
	missingPrimary := filepath.Join(tmp, "multi_1080p.mkv")
	multiMov := &sqlite.ItemRecord{
		ID: "mov-multi", LibraryID: "lib1", Type: "Movie", MediaType: "Video",
		Name: "Multi Version", Path: missingPrimary,
	}
	if err := repo.UpsertItem(ctx, multiMov); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertMediaSourceAndStreams(ctx, multiMov.ID, sqlite.MediaSourceRecord{
		ID: "ms-missing", ItemID: multiMov.ID, Name: "1080p", Path: missingPrimary, Protocol: "File",
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertMediaSourceAndStreams(ctx, multiMov.ID, sqlite.MediaSourceRecord{
		ID: "ms-alive", ItemID: multiMov.ID, Name: "4K", Path: aliveVersion, Protocol: "File",
	}); err != nil {
		t.Fatal(err)
	}

	// Truly orphan item: both item path and media_source path missing
	deadPath := filepath.Join(tmp, "gone.mkv")
	deadMov := &sqlite.ItemRecord{
		ID: "mov-dead", LibraryID: "lib1", Type: "Movie", MediaType: "Video",
		Name: "Dead Movie", Path: deadPath,
	}
	if err := repo.UpsertItem(ctx, deadMov); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertMediaSourceAndStreams(ctx, deadMov.ID, sqlite.MediaSourceRecord{
		ID: "ms-dead", ItemID: deadMov.ID, Name: "1080p", Path: deadPath, Protocol: "File",
	}); err != nil {
		t.Fatal(err)
	}

	// Remote media_source keeps item alive even if item.Path is missing
	remoteMov := &sqlite.ItemRecord{
		ID: "mov-remote-ms", LibraryID: "lib1", Type: "Movie", MediaType: "Video",
		Name: "Remote MS", Path: filepath.Join(tmp, "missing-local.mkv"),
	}
	if err := repo.UpsertItem(ctx, remoteMov); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertMediaSourceAndStreams(ctx, remoteMov.ID, sqlite.MediaSourceRecord{
		ID: "ms-remote", ItemID: remoteMov.ID, Name: "STRM", Path: "https://cdn.example.com/m.mkv", Protocol: "Http", IsRemote: true,
	}); err != nil {
		t.Fatal(err)
	}

	report, err := svc.RunLibraryAudit(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range report.OrphanItems {
		if o.ID == multiMov.ID || o.ID == remoteMov.ID {
			t.Fatalf("item %s with live media_source must NOT be reported as orphan", o.ID)
		}
	}

	cleanRes, err := svc.CleanupOrphans(ctx, []string{multiMov.ID, remoteMov.ID, deadMov.ID})
	if err != nil {
		t.Fatal(err)
	}
	if cleanRes.Deleted != 1 || len(cleanRes.DeletedIDs) != 1 || cleanRes.DeletedIDs[0] != deadMov.ID {
		t.Fatalf("expected only truly orphan item deleted, got %+v", cleanRes)
	}
}

func TestAuditV0107_M19_NFOVirtualPathRejectionAndCoreFieldsPreserved(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "nfo.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	svc, err := NewService(Options{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	repo := sqlite.NewLibraryRepo(store)
	ctx := context.Background()

	// 1. Virtual path rejected by WriteItemNFOAtomic
	virt := &sqlite.ItemRecord{
		ID: "virt-1", LibraryID: "lib1", Type: "MusicArtist", Name: "Artist", Path: "artist://lib1/artist",
	}
	if err := repo.UpsertItem(ctx, virt); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.WriteItemNFOAtomic(ctx, virt.ID); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for virtual path, got %v", err)
	}

	// 2. Episode NFO writes premiered, tagline, studio, director, writer, tvdbid, season, episode
	epPath := filepath.Join(tmp, "Show.S02E05.mkv")
	if err := os.WriteFile(epPath, []byte("mkv"), 0o600); err != nil {
		t.Fatal(err)
	}
	prem := time.Date(2024, 5, 10, 0, 0, 0, 0, time.UTC).Unix()
	sNum := 2
	eNum := 5
	ep := &sqlite.ItemRecord{
		ID: "ep-nfo-1", LibraryID: "lib1", Type: "Episode", MediaType: "Video",
		Name: "The Finale", Path: epPath,
		PremiereDate: &prem, ParentIndexNumber: &sNum, IndexNumber: &eNum,
		Extra: sqlite.ItemExtra{
			Overview:    "Plot text",
			Taglines:    []string{"One last ride"},
			ProviderIDs: map[string]string{"Tmdb": "99", "Tvdb": "7788"},
		},
	}
	if err := repo.UpsertItem(ctx, ep); err != nil {
		t.Fatal(err)
	}
	_ = repo.ReplaceItemTaxonomies(ctx, ep.ID, "studio", []string{"HBO"})
	_ = repo.ReplaceItemPeople(ctx, ep.ID, []sqlite.PersonInput{
		{Name: "Jane Director", Type: "Director", SortOrder: 0},
		{Name: "John Writer", Type: "Writer", SortOrder: 1},
		{Name: "Alice Actor", Role: "Hero", Type: "Actor", SortOrder: 2},
	})

	res, err := svc.WriteItemNFOAtomic(ctx, ep.ID)
	if err != nil {
		t.Fatalf("WriteItemNFOAtomic: %v", err)
	}
	raw, err := os.ReadFile(res.NFOPath)
	if err != nil {
		t.Fatal(err)
	}
	xmlStr := string(raw)
	for _, want := range []string{
		"<aired>2024-05-10</aired>",
		"<tagline>One last ride</tagline>",
		"<studio>HBO</studio>",
		"<director>Jane Director</director>",
		"<writer>John Writer</writer>",
		"<tvdbid>7788</tvdbid>",
		"<season>2</season>",
		"<episode>5</episode>",
	} {
		if !strings.Contains(xmlStr, want) {
			t.Fatalf("expected NFO XML to contain %q, got:\n%s", want, xmlStr)
		}
	}

	// 3. Existing NFO fields preserved when DB fields are sparse
	movPath := filepath.Join(tmp, "Preserve.mkv")
	_ = os.WriteFile(movPath, []byte("mkv"), 0o600)
	existingNFO := filepath.Join(tmp, "Preserve.nfo")
	_ = os.WriteFile(existingNFO, []byte(`<?xml version="1.0" encoding="UTF-8"?>
<movie>
  <title>Old</title>
  <premiered>2020-01-02</premiered>
  <tagline>Keep me</tagline>
  <tvdbid>4242</tvdbid>
  <season>1</season>
  <episode>3</episode>
  <studio>A24</studio>
  <director>Old Dir</director>
  <writer>Old Writer</writer>
</movie>`), 0o600)
	mov := &sqlite.ItemRecord{
		ID: "mov-preserve", LibraryID: "lib1", Type: "Movie", MediaType: "Video",
		Name: "New Title", Path: movPath,
	}
	_ = repo.UpsertItem(ctx, mov)
	res2, err := svc.WriteItemNFOAtomic(ctx, mov.ID)
	if err != nil {
		t.Fatalf("WriteItemNFOAtomic preserve: %v", err)
	}
	raw2, _ := os.ReadFile(res2.NFOPath)
	s2 := string(raw2)
	for _, want := range []string{
		"<title>New Title</title>",
		"<premiered>2020-01-02</premiered>",
		"<tagline>Keep me</tagline>",
		"<tvdbid>4242</tvdbid>",
		"<studio>A24</studio>",
		"<director>Old Dir</director>",
		"<writer>Old Writer</writer>",
	} {
		if !strings.Contains(s2, want) {
			t.Fatalf("expected preserved NFO field %q in:\n%s", want, s2)
		}
	}

	// Exercise extractTagline fallback variants
	if got := extractTagline(map[string]any{"tagline": " direct "}); got != "direct" {
		t.Fatalf("extractTagline string got %q", got)
	}
	if got := extractTagline(map[string]any{"taglines": []string{"", " from-slice "}}); got != "from-slice" {
		t.Fatalf("extractTagline []string got %q", got)
	}
}

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
