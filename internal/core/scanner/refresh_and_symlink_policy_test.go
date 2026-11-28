package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/plugins/nfo"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestRefreshItemSeasonAndEpisodeUseParentSeriesTVDBID(t *testing.T) {
	tmp := t.TempDir()
	cfg, err := config.Load(filepath.Join(tmp, "data"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	var seenQueries []plugins.MediaQuery

	tvdbMock := &zhMockOnlineProvider{
		id: "ambra.builtin.tvdb",
		fetchFn: func(_ context.Context, q plugins.MediaQuery) (*plugins.MetadataResult, error) {
			seenQueries = append(seenQueries, q)
			return &plugins.MetadataResult{
				Name:     "Updated From Series TVDB",
				Overview: "Overview",
			}, nil
		},
	}
	reg := plugins.NewRegistry()
	reg.Register(tvdbMock)
	sc := New(repo, reg, eventbus.New(), cfg)
	ctx := context.Background()

	lib, err := repo.CreateLibrary(ctx, "TV", "tvshows", []string{tmp})
	if err != nil {
		t.Fatal(err)
	}

	series := &sqlite.ItemRecord{
		ID:        "ser-tvdb",
		LibraryID: lib.ID,
		ParentID:  lib.ID,
		Type:      "Series",
		IsFolder:  true,
		Name:      "Game of Thrones",
		Extra: sqlite.ItemExtra{
			ProviderIDs: map[string]string{"Tvdb": "121361", "Tmdb": "1399"},
		},
	}
	_ = repo.UpsertItem(ctx, series)

	// Season and Episode carry their OWN episode/season-level TVDB IDs which MUST NOT
	// be sent as the series TVDB ID (otherwise TVDB returns 404).
	sNum := 0 // Season 0 specials
	season := &sqlite.ItemRecord{
		ID:          "sea-tvdb-0",
		LibraryID:   lib.ID,
		ParentID:    series.ID,
		SeriesID:    series.ID,
		Type:        "Season",
		IsFolder:    true,
		Name:        "Specials",
		IndexNumber: &sNum,
		Extra: sqlite.ItemExtra{
			ProviderIDs: map[string]string{"Tvdb": "9999991"},
		},
	}
	_ = repo.UpsertItem(ctx, season)

	eNum := 2
	ep := &sqlite.ItemRecord{
		ID:                "ep-tvdb-02",
		LibraryID:         lib.ID,
		ParentID:          season.ID,
		SeriesID:          series.ID,
		SeasonID:          season.ID,
		Type:              "Episode",
		MediaType:         "Video",
		Name:              "Special Episode",
		IndexNumber:       &eNum,
		ParentIndexNumber: &sNum,
		Extra: sqlite.ItemExtra{
			ProviderIDs: map[string]string{"Tvdb": "8888882", "Tmdb": "7777772"},
		},
	}
	_ = repo.UpsertItem(ctx, ep)

	if err := sc.RefreshItem(ctx, season.ID, true, false); err != nil {
		t.Fatalf("RefreshItem season: %v", err)
	}
	if err := sc.RefreshItem(ctx, ep.ID, true, false); err != nil {
		t.Fatalf("RefreshItem episode: %v", err)
	}

	if len(seenQueries) != 2 {
		t.Fatalf("expected 2 queries, got %d", len(seenQueries))
	}
	if getProviderID(seenQueries[0].ProviderIDs, "tvdb") != "121361" || seenQueries[0].SeasonNumber != 0 {
		t.Fatalf("Season query must use parent Series TVDB ID 121361 (Season 0), got %+v", seenQueries[0])
	}
	if getProviderID(seenQueries[1].ProviderIDs, "tvdb") != "121361" || getProviderID(seenQueries[1].ProviderIDs, "tmdb") != "1399" || seenQueries[1].SeasonNumber != 0 || seenQueries[1].EpisodeNumber != 2 {
		t.Fatalf("Episode query must use parent Series TVDB ID 121361 & TMDB ID 1399, got %+v", seenQueries[1])
	}
}

func TestRefreshPreservesRankingAndCustomTags(t *testing.T) {
	tmp := t.TempDir()
	cfg, err := config.Load(filepath.Join(tmp, "data"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	ctx := context.Background()
	lib, _ := repo.CreateLibrary(ctx, "Movies", "movies", []string{tmp})

	movie := &sqlite.ItemRecord{
		ID:        "mov-tags",
		LibraryID: lib.ID,
		ParentID:  lib.ID,
		Type:      "Movie",
		MediaType: "Video",
		Name:      "The Matrix",
	}
	_ = repo.UpsertItem(ctx, movie)
	// Seed ranking tag + user custom tag prior to metadata refresh.
	if err := repo.ReplaceItemTaxonomies(ctx, movie.ID, "tag", []string{"豆瓣Top250", "我的最爱"}); err != nil {
		t.Fatal(err)
	}

	online := &zhMockOnlineProvider{
		id: "ambra.builtin.tmdb",
		fetchFn: func(_ context.Context, _ plugins.MediaQuery) (*plugins.MetadataResult, error) {
			return &plugins.MetadataResult{
				Name:   "黑客帝国",
				Genres: []string{"科幻"},
				Tags:   []string{"赛博朋克", "我的最爱"},
			}, nil
		},
	}
	reg := plugins.NewRegistry()
	reg.Register(online)
	sc := New(repo, reg, eventbus.New(), cfg)

	if err := sc.RefreshItem(ctx, movie.ID, true, false); err != nil {
		t.Fatalf("RefreshItem: %v", err)
	}

	_, _, tags, err := repo.GetTaxonomiesByItemID(ctx, movie.ID)
	if err != nil {
		t.Fatal(err)
	}
	tagSet := make(map[string]bool, len(tags))
	for _, tg := range tags {
		tagSet[tg] = true
	}
	for _, want := range []string{"豆瓣Top250", "我的最爱", "赛博朋克"} {
		if !tagSet[want] {
			t.Fatalf("expected tag %q to be preserved/merged after refresh, got %v", want, tags)
		}
	}
}

func TestScannerSymlinkEscapeBlockedInScanPathLibraryAndExtras(t *testing.T) {
	tmp := t.TempDir()
	libRoot := filepath.Join(tmp, "library")
	outside := filepath.Join(tmp, "outside")
	if err := os.MkdirAll(filepath.Join(libRoot, "ValidMovie (2024)", "trailers"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}

	// Real movie inside library + escaping symlink in library root + escaping trailer symlink in trailers/
	validMovie := filepath.Join(libRoot, "ValidMovie (2024)", "ValidMovie (2024).mkv")
	_ = os.WriteFile(validMovie, []byte("video"), 0o644)

	outsideMovie := filepath.Join(outside, "Secret (2024).mkv")
	_ = os.WriteFile(outsideMovie, []byte("secret"), 0o644)
	escapeMovieLink := filepath.Join(libRoot, "Secret (2024).mkv")
	if err := os.Symlink(outsideMovie, escapeMovieLink); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	outsideTrailer := filepath.Join(outside, "secret-trailer.mkv")
	_ = os.WriteFile(outsideTrailer, []byte("trailer"), 0o644)
	escapeTrailerLink := filepath.Join(libRoot, "ValidMovie (2024)", "trailers", "escape-trailer.mkv")
	_ = os.Symlink(outsideTrailer, escapeTrailerLink)

	cfg, err := config.Load(filepath.Join(tmp, "data"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	sc := New(repo, plugins.NewRegistry(), eventbus.New(), cfg)
	ctx := context.Background()

	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{libRoot})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Full ScanLibrary must index ValidMovie, skip escaping symlink movie, and skip escaping trailer extra.
	if _, err := sc.ScanLibrary(ctx, lib.ID); err != nil {
		t.Fatalf("ScanLibrary: %v", err)
	}
	if item, _ := repo.FindItemByPath(ctx, lib.ID, escapeMovieLink); item != nil {
		t.Fatalf("escaping symlink movie %q must NOT be indexed", escapeMovieLink)
	}
	if item, _ := repo.FindItemByPath(ctx, lib.ID, escapeTrailerLink); item != nil {
		t.Fatalf("escaping symlink extra %q must NOT be indexed", escapeTrailerLink)
	}
	if item, err := repo.FindItemByPath(ctx, lib.ID, validMovie); err != nil || item == nil {
		t.Fatalf("expected valid movie to be indexed, err=%v", err)
	}

	// 2. ScanPath on escaping symlink must also refuse to index it.
	if _, err := sc.ScanPath(ctx, lib.ID, escapeMovieLink); err == nil {
		t.Fatalf("ScanPath must reject escaping symlink %q with an error", escapeMovieLink)
	}
	if item, _ := repo.FindItemByPath(ctx, lib.ID, escapeMovieLink); item != nil {
		t.Fatalf("ScanPath must NOT index escaping symlink %q", escapeMovieLink)
	}
}

func TestScanPathDeletesStaleItemInsideValidSymlinkedSubdir(t *testing.T) {
	tmp := t.TempDir()
	libRoot := filepath.Join(tmp, "library")
	realSub := filepath.Join(libRoot, "real_storage", "action")
	outside := filepath.Join(tmp, "outside")
	if err := os.MkdirAll(realSub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}

	validLink := filepath.Join(libRoot, "linked_action")
	if err := os.Symlink(realSub, validLink); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	escapeLink := filepath.Join(libRoot, "escaped_dir")
	if err := os.Symlink(outside, escapeLink); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	realFile := filepath.Join(realSub, "Inception (2010).mkv")
	if err := os.WriteFile(realFile, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	linkedFile := filepath.Join(validLink, "Inception (2010).mkv")

	cfg, err := config.Load(filepath.Join(tmp, "data"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	sc := New(repo, plugins.NewRegistry(), eventbus.New(), cfg)
	sc.SetProber(nil)
	ctx := context.Background()

	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{libRoot})
	if err != nil {
		t.Fatal(err)
	}

	// Index the file via ScanPath through the valid depth-1 symlinked subdirectory.
	if _, err := sc.ScanPath(ctx, lib.ID, linkedFile); err != nil {
		t.Fatalf("ScanPath index inside valid symlinked subdir failed: %v", err)
	}
	if item, err := repo.FindItemByPath(ctx, lib.ID, linkedFile); err != nil || item == nil {
		t.Fatalf("expected item at %q to be indexed, err=%v", linkedFile, err)
	}

	// Delete the underlying file and invoke ScanPath on the symlinked path.
	if err := os.Remove(realFile); err != nil {
		t.Fatal(err)
	}
	stats, err := sc.ScanPath(ctx, lib.ID, linkedFile)
	if err != nil {
		t.Fatalf("ScanPath incremental deletion inside valid symlinked subdir failed: %v", err)
	}
	if stats.ItemsDeleted != 1 {
		t.Fatalf("expected ItemsDeleted=1, got %+v", stats)
	}
	if item, _ := repo.FindItemByPath(ctx, lib.ID, linkedFile); item != nil {
		t.Fatalf("expected stale DB record for %q to be deleted, still found %+v", linkedFile, item)
	}

	// Deleting a nonexistent file inside an escaping symlinked directory must still be rejected.
	escapedDeletedFile := filepath.Join(escapeLink, "Missing (2024).mkv")
	if _, err := sc.ScanPath(ctx, lib.ID, escapedDeletedFile); err == nil {
		t.Fatalf("expected ScanPath to reject nonexistent path under escaping symlink %q", escapedDeletedFile)
	}
}

func TestScannerNFOLockDataAndSetCreatesBoxSet(t *testing.T) {
	tmp := t.TempDir()
	libRoot := filepath.Join(tmp, "movies")
	movieDir := filepath.Join(libRoot, "Harry Potter 1 (2001)")
	if err := os.MkdirAll(movieDir, 0o755); err != nil {
		t.Fatal(err)
	}
	moviePath := filepath.Join(movieDir, "Harry Potter 1 (2001).mkv")
	_ = os.WriteFile(moviePath, []byte("mkv"), 0o644)
	nfoXML := `<?xml version="1.0" encoding="UTF-8"?>
<movie>
  <title>哈利·波特与魔法石</title>
  <year>2001</year>
  <lockdata>true</lockdata>
  <set><name>哈利·波特合集</name></set>
</movie>`
	_ = os.WriteFile(filepath.Join(movieDir, "Harry Potter 1 (2001).nfo"), []byte(nfoXML), 0o644)

	cfg, err := config.Load(filepath.Join(tmp, "data"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	reg := plugins.NewRegistry()
	reg.Register(nfo.New())
	sc := New(repo, reg, eventbus.New(), cfg)
	ctx := context.Background()

	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{libRoot})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sc.ScanLibrary(ctx, lib.ID); err != nil {
		t.Fatalf("ScanLibrary: %v", err)
	}

	item, err := repo.FindItemByPath(ctx, lib.ID, moviePath)
	if err != nil || item == nil {
		t.Fatalf("expected movie indexed, err=%v", err)
	}
	if !item.Extra.LockData {
		t.Fatalf("expected NFO <lockdata>true</lockdata> to persist LockData=true")
	}

	boxsets, err := repo.FindItemsByProviderID(ctx, "collections", "BoxSet", "NfoSet", "哈利·波特合集")
	if err != nil {
		t.Fatalf("FindItemsByProviderID: %v", err)
	}
	if len(boxsets) != 1 || boxsets[0].Name != "哈利·波特合集" {
		t.Fatalf("expected NFO <set> to create BoxSet '哈利·波特合集', got %+v", boxsets)
	}
}
