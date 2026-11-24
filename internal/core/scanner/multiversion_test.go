package scanner_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/handlers/compatmedia"
	"github.com/ambra-media/ambra/internal/api/handlers/streaming"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/imaging"
	"github.com/ambra-media/ambra/internal/core/playback"
	"github.com/ambra-media/ambra/internal/core/scanner"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/plugins/nfo"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func setupMultiVersionEnv(t *testing.T) (string, *sqlite.Store, *sqlite.LibraryRepo, *scanner.Scanner, *config.Config, *plugins.Registry) {
	t.Helper()
	tmp := t.TempDir()
	cfg, err := config.Load(filepath.Join(tmp, "data"))
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	store, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	repo := sqlite.NewLibraryRepo(store)
	reg := plugins.NewRegistry()
	reg.Register(nfo.New())
	bus := eventbus.New()
	sc := scanner.New(repo, reg, bus, cfg)
	sc.SetProber(scanner.NewDisabledProber())
	return tmp, store, repo, sc, cfg, reg
}

func TestCrossDirectoryMovieAutoMergeByTMDBAndPlaybackInfo(t *testing.T) {
	ctx := context.Background()
	tmp, _, repo, sc, cfg, _ := setupMultiVersionEnv(t)

	// Two completely different directories sharing the same tmdbid=535167
	dir4k := filepath.Join(tmp, "movies_4k", "The Wandering Earth (2019)")
	dir1080 := filepath.Join(tmp, "movies_strm", "Wandering Earth")
	_ = os.MkdirAll(dir4k, 0o755)
	_ = os.MkdirAll(dir1080, 0o755)

	file4k := filepath.Join(dir4k, "The.Wandering.Earth.2019.2160p.DV.mkv")
	file1080 := filepath.Join(dir1080, "The.Wandering.Earth.2019.1080p.strm")
	_ = os.WriteFile(file4k, []byte("4k-video-content"), 0o644)
	_ = os.WriteFile(filepath.Join(dir4k, "The.Wandering.Earth.2019.2160p.DV.nfo"),
		[]byte(`<movie><title>流浪地球</title><year>2019</year><tmdbid>535167</tmdbid></movie>`), 0o644)

	_ = os.WriteFile(file1080, []byte("https://cdn.example.com/wandering-1080p.mkv\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir1080, "The.Wandering.Earth.2019.1080p.nfo"),
		[]byte(`<movie><title>流浪地球</title><year>2019</year><tmdbid>535167</tmdbid></movie>`), 0o644)

	lib, err := repo.CreateLibrary(ctx, "电影", "movies", []string{filepath.Join(tmp, "movies_4k"), filepath.Join(tmp, "movies_strm")})
	if err != nil {
		t.Fatal(err)
	}

	stats, err := sc.ScanLibrary(ctx, lib.ID)
	if err != nil {
		t.Fatalf("ScanLibrary: %v", err)
	}
	if stats.ItemsAdded != 2 || stats.Errors != 0 {
		t.Fatalf("expected 2 processed files with 0 errors, got %+v", stats)
	}

	// Must result in 1 Movie Item with 2 MediaSources
	items, total, err := repo.QueryItems(ctx, sqlite.ItemFilter{
		ServerID:         cfg.ServerID,
		LibraryID:        lib.ID,
		IncludeItemTypes: []string{"Movie"},
		Fields:           []string{"MediaSources", "ProviderIds"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("expected 1 merged movie item, got total=%d len=%d", total, len(items))
	}
	movie := items[0]
	if len(movie.MediaSources) != 2 {
		t.Fatalf("expected 2 MediaSources, got %d: %+v", len(movie.MediaSources), movie.MediaSources)
	}
	if !strings.Contains(movie.MediaSources[0].Name, "2160p DolbyVision · MKV") {
		t.Fatalf("expected clean version name for 4K DV mkv, got %q", movie.MediaSources[0].Name)
	}
	if !strings.Contains(movie.MediaSources[1].Name, "1080p · STRM") {
		t.Fatalf("expected clean version name for 1080p strm, got %q", movie.MediaSources[1].Name)
	}

	// Verify PlaybackInfo returns both versions
	mux := http.NewServeMux()
	pb := playback.New(repo, nil, cfg)
	sh := streaming.New(repo, pb, nil)
	sh.RegisterRoutes(mux, streaming.AuthFuncs{})

	req := httptest.NewRequest(http.MethodPost, "/Items/"+movie.Id+"/PlaybackInfo", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PlaybackInfo status=%d body=%s", w.Code, w.Body.String())
	}
	var pbResp struct {
		MediaSources []dto.MediaSourceInfo `json:"MediaSources"`
	}
	if err := json.NewDecoder(w.Body).Decode(&pbResp); err != nil {
		t.Fatalf("decode PlaybackInfo: %v", err)
	}
	if len(pbResp.MediaSources) != 2 {
		t.Fatalf("expected PlaybackInfo to return 2 MediaSources, got %d", len(pbResp.MediaSources))
	}

	// Incremental re-scan must skip BOTH primary and alternate version files!
	stats2, err := sc.ScanLibrary(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stats2.ItemsAdded != 0 || stats2.ItemsSkipped != 2 {
		t.Fatalf("expected 2 skipped on re-scan (primary + alt), got %+v", stats2)
	}

	// Modify alternate file (size + mtime) -> re-scan updates only that alternate MediaSource, preserving both versions
	_ = os.WriteFile(file1080, []byte("https://cdn.example.com/wandering-1080p-v2.mkv\n"), 0o644)
	future := time.Now().Add(5 * time.Second)
	_ = os.Chtimes(file1080, future, future)

	stats3, err := sc.ScanLibrary(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stats3.ItemsAdded != 1 || stats3.ItemsSkipped != 1 {
		t.Fatalf("expected 1 updated alt + 1 skipped primary, got %+v", stats3)
	}
	srcsAfterUpdate, err := repo.GetMediaSourcesByItemID(ctx, movie.Id)
	if err != nil || len(srcsAfterUpdate) != 2 {
		t.Fatalf("expected 2 media sources after alt update, got %d err=%v", len(srcsAfterUpdate), err)
	}
}

func TestCrossDirectoryMovieAutoMergeByIMDB(t *testing.T) {
	ctx := context.Background()
	tmp, _, repo, sc, cfg, _ := setupMultiVersionEnv(t)

	d1 := filepath.Join(tmp, "a")
	d2 := filepath.Join(tmp, "b")
	_ = os.MkdirAll(d1, 0o755)
	_ = os.MkdirAll(d2, 0o755)

	_ = os.WriteFile(filepath.Join(d1, "Inception.2010.720p.HDR10.mp4"), []byte("v1"), 0o644)
	_ = os.WriteFile(filepath.Join(d1, "Inception.2010.720p.HDR10.nfo"), []byte(`<movie><title>Inception</title><imdbid>tt1375666</imdbid></movie>`), 0o644)
	_ = os.WriteFile(filepath.Join(d2, "Inception.2010.480p.HDR.mkv"), []byte("v2"), 0o644)
	_ = os.WriteFile(filepath.Join(d2, "Inception.2010.480p.HDR.nfo"), []byte(`<movie><title>盗梦空间</title><imdbid>tt1375666</imdbid></movie>`), 0o644)

	lib, _ := repo.CreateLibrary(ctx, "IMDB Movies", "movies", []string{d1, d2})
	if _, err := sc.ScanLibrary(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}

	items, total, err := repo.QueryItems(ctx, sqlite.ItemFilter{
		ServerID:         cfg.ServerID,
		LibraryID:        lib.ID,
		IncludeItemTypes: []string{"Movie"},
		Fields:           []string{"MediaSources"},
	})
	if err != nil || total != 1 || len(items[0].MediaSources) != 2 {
		t.Fatalf("expected 1 IMDB-merged movie with 2 sources, got total=%d items=%+v err=%v", total, items, err)
	}
}

func TestCrossDirectorySeriesAndEpisodeAutoMergeByTMDB(t *testing.T) {
	ctx := context.Background()
	tmp, _, repo, sc, cfg, _ := setupMultiVersionEnv(t)

	// Two separate TV directories with the same tmdbid=203419 in tvshow.nfo
	s1Dir := filepath.Join(tmp, "tv_4k", "Three Body (2023)")
	s1EpDir := filepath.Join(s1Dir, "Season 01")
	s2Dir := filepath.Join(tmp, "tv_1080p", "三体")
	s2EpDir := filepath.Join(s2Dir, "Season 01")
	_ = os.MkdirAll(s1EpDir, 0o755)
	_ = os.MkdirAll(s2EpDir, 0o755)

	_ = os.WriteFile(filepath.Join(s1Dir, "tvshow.nfo"), []byte(`<tvshow><title>三体</title><year>2023</year><tmdbid>203419</tmdbid></tvshow>`), 0o644)
	_ = os.WriteFile(filepath.Join(s2Dir, "tvshow.nfo"), []byte(`<tvshow><title>Three Body</title><year>2023</year><tmdbid>203419</tmdbid></tvshow>`), 0o644)

	ep1v1 := filepath.Join(s1EpDir, "Three.Body.S01E01.2160p.HDR10+.mkv")
	ep1v2 := filepath.Join(s2EpDir, "Three.Body.S01E01.1080p.strm")
	_ = os.WriteFile(ep1v1, []byte("ep1-4k"), 0o644)
	_ = os.WriteFile(ep1v2, []byte("https://cdn.example.com/s01e01.mkv\n"), 0o644)

	tvLib, err := repo.CreateLibrary(ctx, "剧集", "tvshows", []string{filepath.Join(tmp, "tv_4k"), filepath.Join(tmp, "tv_1080p")})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := sc.ScanLibrary(ctx, tvLib.ID); err != nil {
		t.Fatalf("ScanLibrary tv: %v", err)
	}

	// Must have 1 Series, 1 Season, 1 Episode with 2 MediaSources
	seriesList, sTotal, err := repo.QueryItems(ctx, sqlite.ItemFilter{
		ServerID:         cfg.ServerID,
		LibraryID:        tvLib.ID,
		IncludeItemTypes: []string{"Series"},
	})
	if err != nil || sTotal != 1 || len(seriesList) != 1 {
		t.Fatalf("expected 1 merged Series, got total=%d list=%+v err=%v", sTotal, seriesList, err)
	}

	seasons, seaTotal, err := repo.QueryItems(ctx, sqlite.ItemFilter{
		ServerID:         cfg.ServerID,
		SeriesID:         seriesList[0].Id,
		IncludeItemTypes: []string{"Season"},
	})
	if err != nil || seaTotal != 1 || len(seasons) != 1 {
		t.Fatalf("expected 1 merged Season, got total=%d err=%v", seaTotal, err)
	}

	eps, epTotal, err := repo.QueryItems(ctx, sqlite.ItemFilter{
		ServerID:         cfg.ServerID,
		SeriesID:         seriesList[0].Id,
		IncludeItemTypes: []string{"Episode"},
		Fields:           []string{"MediaSources"},
	})
	if err != nil || epTotal != 1 || len(eps) != 1 {
		t.Fatalf("expected 1 merged Episode, got total=%d eps=%+v err=%v", epTotal, eps, err)
	}
	if len(eps[0].MediaSources) != 2 {
		t.Fatalf("expected 2 MediaSources on merged Episode, got %d: %+v", len(eps[0].MediaSources), eps[0].MediaSources)
	}

	// Incremental re-scan skips both episode versions
	st2, err := sc.ScanLibrary(ctx, tvLib.ID)
	if err != nil || st2.ItemsAdded != 0 || st2.ItemsSkipped != 2 {
		t.Fatalf("expected 2 skipped episodes on re-scan, got %+v err=%v", st2, err)
	}

	// Modify alternate episode file -> updates only that alternate MediaSource
	_ = os.WriteFile(ep1v2, []byte("https://cdn.example.com/s01e01-v2.mkv\n"), 0o644)
	future := time.Now().Add(5 * time.Second)
	_ = os.Chtimes(ep1v2, future, future)
	st3, err := sc.ScanLibrary(ctx, tvLib.ID)
	if err != nil || st3.ItemsAdded != 1 || st3.ItemsSkipped != 1 {
		t.Fatalf("expected 1 updated alt episode + 1 skipped, got %+v err=%v", st3, err)
	}
}

func TestSameDirectoryMovieAutoMergeWithoutTMDB(t *testing.T) {
	ctx := context.Background()
	tmp, _, repo, sc, cfg, _ := setupMultiVersionEnv(t)

	dir := filepath.Join(tmp, "movies", "片名 (2024)")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "片名 (2024) - 4K.mkv"), []byte("4k-file"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "片名 (2024) - 1080p.mkv"), []byte("1080p-file"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "片名 (2024) - Director's Cut.mkv"), []byte("dc-file"), 0o644)

	lib, err := repo.CreateLibrary(ctx, "NoTMDB", "movies", []string{filepath.Join(tmp, "movies")})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := sc.ScanLibrary(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}

	items, total, err := repo.QueryItems(ctx, sqlite.ItemFilter{
		ServerID:         cfg.ServerID,
		LibraryID:        lib.ID,
		IncludeItemTypes: []string{"Movie"},
		Fields:           []string{"MediaSources"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("expected 1 merged movie in same dir, got total=%d", total)
	}
	if len(items[0].MediaSources) != 3 {
		t.Fatalf("expected 3 MediaSources merged in same dir, got %d: %+v", len(items[0].MediaSources), items[0].MediaSources)
	}
}

func TestHTTPMergeVersionsAndSplitAlternateSources(t *testing.T) {
	ctx := context.Background()
	tmp, store, repo, sc, cfg, reg := setupMultiVersionEnv(t)

	lib, err := repo.CreateLibrary(ctx, "Manual", "movies", []string{tmp})
	if err != nil {
		t.Fatal(err)
	}
	m1 := &sqlite.ItemRecord{ID: "manual1", LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", MediaType: "Video", Name: "Blade Runner", Path: "/m/br-4k.mkv", Container: "mkv"}
	m2 := &sqlite.ItemRecord{ID: "manual2", LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", MediaType: "Video", Name: "Blade Runner", Path: "/m/br-1080p.mp4", Container: "mp4"}
	for _, m := range []*sqlite.ItemRecord{m1, m2} {
		_ = repo.UpsertItem(ctx, m)
		_ = repo.ReplaceItemAncestors(ctx, m.ID, []sqlite.AncestorLink{{AncestorID: lib.ID, Depth: 1}})
		_ = repo.UpsertMediaSourceAndStreams(ctx, m.ID, sqlite.MediaSourceRecord{
			Name:      m.Container,
			Path:      m.Path,
			Container: m.Container,
			Size:      1024,
			Streams:   []sqlite.MediaStreamRecord{{StreamIndex: 0, StreamType: "Video", Codec: "hevc"}},
		})
	}

	img := imaging.New(repo).WithCacheDir(cfg.ImageCacheDir())
	h := compatmedia.New(repo, sc, img, reg, store, cfg)
	router := h.BuildRouter(compatmedia.AuthFuncs{
		UserIDFromCtx: func(context.Context) string { return "admin" },
	})

	// 1. POST /Videos/MergeVersions?Ids=manual1,manual2
	req := httptest.NewRequest(http.MethodPost, "/Videos/MergeVersions?Ids=manual1,manual2", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("POST /Videos/MergeVersions status=%d body=%s", w.Code, w.Body.String())
	}

	srcs, err := repo.GetMediaSourcesByItemID(ctx, "manual1")
	if err != nil || len(srcs) != 2 {
		t.Fatalf("expected 2 merged sources on manual1, got %d err=%v", len(srcs), err)
	}
	if _, err := repo.GetItemRecordByID(ctx, "manual2"); err == nil {
		t.Fatalf("expected manual2 deleted after HTTP merge")
	}

	// 2. DELETE /Videos/manual1/AlternateSources -> splits back into 2 items
	req = httptest.NewRequest(http.MethodDelete, "/Videos/manual1/AlternateSources", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DELETE /Videos/manual1/AlternateSources status=%d body=%s", w.Code, w.Body.String())
	}

	srcsAfterSplit, err := repo.GetMediaSourcesByItemID(ctx, "manual1")
	if err != nil || len(srcsAfterSplit) != 1 {
		t.Fatalf("expected 1 source remaining on manual1 after split, got %d err=%v", len(srcsAfterSplit), err)
	}
	allMovies, total, err := repo.QueryItems(ctx, sqlite.ItemFilter{
		ServerID:         cfg.ServerID,
		LibraryID:        lib.ID,
		IncludeItemTypes: []string{"Movie"},
	})
	if err != nil || total != 2 || len(allMovies) != 2 {
		t.Fatalf("expected 2 independent movies after HTTP split, got total=%d err=%v", total, err)
	}
}
