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
