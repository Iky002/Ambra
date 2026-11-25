package scanner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/plugins/nfo"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestBuildFTSFieldsEpisodeSlimming(t *testing.T) {
	people := []string{"张鲁一", "于和伟", "Christopher Nolan"}

	epRec := &sqlite.ItemRecord{
		ID:            "ep1",
		Type:          "Episode",
		Name:          "第一集",
		OriginalTitle: "Pilot",
		Extra:         sqlite.ItemExtra{SeriesName: "三体"},
	}
	titleRaw, titleTokens, peopleTokens := buildFTSFields(epRec, people)
	if peopleTokens != "" {
		t.Fatalf("expected empty peopleTokens for Episode, got %q", peopleTokens)
	}
	if !strings.Contains(titleRaw, "第一集") || titleTokens == "" {
		t.Fatalf("expected title fields preserved for Episode, got raw=%q tokens=%q", titleRaw, titleTokens)
	}

	for _, itemType := range []string{"Movie", "Series", "MusicAlbum"} {
		rec := &sqlite.ItemRecord{
			ID:   "item-" + itemType,
			Type: itemType,
			Name: "三体",
		}
		_, _, pt := buildFTSFields(rec, people)
		if pt == "" {
			t.Fatalf("expected non-empty peopleTokens for %s, got empty string", itemType)
		}
	}
}

func TestEpisodeFTS5SlimmingEndToEnd(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "fts_slim.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	reg := plugins.NewRegistry()
	reg.Register(nfo.New())
	sc := New(repo, reg, eventbus.New(), config.Default(tmp))
	sc.SetProber(NewDisabledProber())

	// Movie with actor NFO
	movieDir := filepath.Join(tmp, "movies", "Inception (2010)")
	_ = os.MkdirAll(movieDir, 0o755)
	_ = os.WriteFile(filepath.Join(movieDir, "Inception.mkv"), []byte("mov"), 0o644)
	_ = os.WriteFile(filepath.Join(movieDir, "Inception.nfo"), []byte(`
		<movie>
			<title>盗梦空间</title>
			<actor><name>莱昂纳多</name><role>Cobb</role></actor>
		</movie>`), 0o644)

	// Series + Episode with actor NFOs
	seriesDir := filepath.Join(tmp, "tv", "Three Body (2023)")
	epDir := filepath.Join(seriesDir, "Season 01")
	_ = os.MkdirAll(epDir, 0o755)
	_ = os.WriteFile(filepath.Join(seriesDir, "tvshow.nfo"), []byte(`
		<tvshow>
			<title>三体</title>
			<actor><name>张鲁一</name><role>汪淼</role></actor>
		</tvshow>`), 0o644)
	epFile := filepath.Join(epDir, "Three.Body.S01E01.mkv")
	_ = os.WriteFile(epFile, []byte("ep1"), 0o644)
	_ = os.WriteFile(filepath.Join(epDir, "Three.Body.S01E01.nfo"), []byte(`
		<episodedetails>
			<title>倒计时</title>
			<actor><name>客串演员甲</name><role>Guest</role></actor>
		</episodedetails>`), 0o644)

	ctx := context.Background()
	movieLib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{filepath.Join(tmp, "movies")})
	if err != nil {
		t.Fatal(err)
	}
	tvLib, err := repo.CreateLibrary(ctx, "TV", "tvshows", []string{filepath.Join(tmp, "tv")})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := sc.ScanLibrary(ctx, movieLib.ID); err != nil {
		t.Fatalf("ScanLibrary movies: %v", err)
	}
	if _, err := sc.ScanLibrary(ctx, tvLib.ID); err != nil {
		t.Fatalf("ScanLibrary tv: %v", err)
	}

	// Verify Movie FTS5 has people_names populated
	movieRec, err := repo.FindItemByPath(ctx, movieLib.ID, filepath.Join(movieDir, "Inception.mkv"))
	if err != nil {
		t.Fatalf("FindItemByPath movie: %v", err)
	}
	var moviePeopleFTS string
	if err := store.Reader.QueryRowContext(ctx, `SELECT people_names FROM item_search_fts WHERE item_id = ?`, movieRec.ID).Scan(&moviePeopleFTS); err != nil {
		t.Fatalf("query movie FTS: %v", err)
	}
	if strings.TrimSpace(moviePeopleFTS) == "" {
		t.Fatalf("expected Movie FTS people_names to be populated")
	}

	// Verify Series FTS5 has people_names populated
	seriesRec, err := repo.FindItemByPath(ctx, tvLib.ID, seriesDir)
	if err != nil {
		t.Fatalf("FindItemByPath series: %v", err)
	}
	var seriesPeopleFTS string
	if err := store.Reader.QueryRowContext(ctx, `SELECT people_names FROM item_search_fts WHERE item_id = ?`, seriesRec.ID).Scan(&seriesPeopleFTS); err != nil {
		t.Fatalf("query series FTS: %v", err)
	}
	if strings.TrimSpace(seriesPeopleFTS) == "" {
		t.Fatalf("expected Series FTS people_names to be populated")
	}

	// Verify Episode FTS5 has EMPTY people_names, while title search and item_people relation remain intact
	epRec, err := repo.FindItemByPath(ctx, tvLib.ID, epFile)
	if err != nil {
		t.Fatalf("FindItemByPath episode: %v", err)
	}
	var epTitleFTS, epPeopleFTS string
	if err := store.Reader.QueryRowContext(ctx, `SELECT title_raw, people_names FROM item_search_fts WHERE item_id = ?`, epRec.ID).Scan(&epTitleFTS, &epPeopleFTS); err != nil {
		t.Fatalf("query episode FTS: %v", err)
	}
	if epPeopleFTS != "" {
		t.Fatalf("expected Episode FTS people_names to be empty, got %q", epPeopleFTS)
	}
	if !strings.Contains(epTitleFTS, "倒计时") {
		t.Fatalf("expected Episode FTS title_raw to contain episode title, got %q", epTitleFTS)
	}

	epPeople, err := repo.GetPeopleByItemID(ctx, epRec.ID)
	if err != nil || len(epPeople) != 1 || epPeople[0].Name != "客串演员甲" {
		t.Fatalf("expected Episode item_people relation preserved, got %+v err=%v", epPeople, err)
	}
}

func TestPhaseABatchCommitChunkingAndIncrementalSkip(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "batch.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	sc := New(repo, plugins.NewRegistry(), eventbus.New(), config.Default(tmp))
	sc.SetProber(NewDisabledProber())

	// Create 505 movies to exercise > phaseABatchSize (500) chunk flush + remainder flush.
	const totalFiles = phaseABatchSize + 5
	mediaDir := filepath.Join(tmp, "movies")
	if err := os.MkdirAll(mediaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= totalFiles; i++ {
		name := fmt.Sprintf("Movie_%04d (2024).mkv", i)
		if err := os.WriteFile(filepath.Join(mediaDir, name), []byte("v"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ctx := context.Background()
	lib, err := repo.CreateLibrary(ctx, "Batch Movies", "movies", []string{mediaDir})
	if err != nil {
		t.Fatal(err)
	}

	stats, err := sc.ScanLibrary(ctx, lib.ID)
	if err != nil {
		t.Fatalf("ScanLibrary batch: %v", err)
	}
	if stats.ItemsAdded != totalFiles || stats.Errors != 0 {
		t.Fatalf("expected %d added and 0 errors, got %+v", totalFiles, stats)
	}

	items, total, err := repo.QueryItems(ctx, sqlite.ItemFilter{
		LibraryID:        lib.ID,
		IncludeItemTypes: []string{"Movie"},
		Limit:            10,
		Fields:           []string{"MediaSources"},
	})
	if err != nil || total != totalFiles {
		t.Fatalf("expected total=%d movies in DB, got total=%d err=%v", totalFiles, total, err)
	}
	if len(items) == 0 || len(items[0].MediaSources) != 1 {
		t.Fatalf("expected MediaSources persisted after batch commit, got %+v", items)
	}

	// Incremental re-scan must skip all 505 unchanged items.
	stats2, err := sc.ScanLibrary(ctx, lib.ID)
	if err != nil {
		t.Fatalf("incremental ScanLibrary: %v", err)
	}
	if stats2.ItemsAdded != 0 || stats2.ItemsSkipped != totalFiles || stats2.Errors != 0 {
		t.Fatalf("expected %d skipped and 0 added on re-scan, got %+v", totalFiles, stats2)
	}
}
