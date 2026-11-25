package scanner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestWriteFileAtomicConcurrent(t *testing.T) {
	tmp := t.TempDir()
	destPath := filepath.Join(tmp, "primary.jpg")

	const workers = 16
	var wg sync.WaitGroup
	errCh := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			payload := []byte(fmt.Sprintf("jpeg-data-worker-%02d", idx))
			if err := writeFileAtomic(destPath, payload); err != nil {
				errCh <- err
			}
		}(i)
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("concurrent writeFileAtomic failed: %v", err)
	}

	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("read final file: %v", err)
	}
	if len(got) == 0 {
		t.Fatalf("expected non-empty final file")
	}

	// Ensure no leftover .tmp files remain.
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "primary.jpg" {
		t.Fatalf("expected only primary.jpg, got %d entries", len(entries))
	}
}

func TestClassifyExtraFile(t *testing.T) {
	root := "/media/movies/Inception (2010)"
	cases := []struct {
		path     string
		wantType string
		wantOK   bool
	}{
		{filepath.Join(root, "Inception (2010).mkv"), "", false},
		{filepath.Join(root, "trailers", "official.mp4"), "Trailer", true},
		{filepath.Join(root, "extras", "bonus.mkv"), "Clip", true},
		{filepath.Join(root, "featurettes", "making-of.mkv"), "Featurette", true},
		{filepath.Join(root, "behind the scenes", "bts1.mp4"), "BehindTheScenes", true},
		{filepath.Join(root, "deleted scenes", "cut1.mkv"), "DeletedScene", true},
		{filepath.Join(root, "interviews", "nolan.mkv"), "Interview", true},
		{filepath.Join(root, "scenes", "hallway.mkv"), "Scene", true},
		{filepath.Join(root, "shorts", "short1.mp4"), "Short", true},
		{filepath.Join(root, "theme-music", "main.mp3"), "ThemeSong", true},
		{filepath.Join(root, "backdrops", "loop.mp4"), "ThemeVideo", true},
		{filepath.Join(root, "Inception (2010)-trailer.mp4"), "Trailer", true},
		{filepath.Join(root, "Inception (2010)-featurette.mkv"), "Featurette", true},
		{filepath.Join(root, "Inception (2010)-behindthescenes.mkv"), "BehindTheScenes", true},
		{filepath.Join(root, "Inception (2010)-deleted.mkv"), "DeletedScene", true},
		{filepath.Join(root, "theme.mp3"), "ThemeSong", true},
		{filepath.Join(root, "theme.flac"), "ThemeSong", true},
		{filepath.Join(root, "theme.m4a"), "ThemeSong", true},
		{filepath.Join(root, "poster.jpg"), "", false},
		{filepath.Join(root, "trailers", "readme.txt"), "", false},
	}

	for _, tc := range cases {
		gotType, gotOK := ClassifyExtraFile(root, tc.path)
		if gotOK != tc.wantOK || gotType != tc.wantType {
			t.Errorf("ClassifyExtraFile(%q, %q) = (%q, %v), want (%q, %v)",
				root, tc.path, gotType, gotOK, tc.wantType, tc.wantOK)
		}
	}
}

func TestScanLocalTrailersSpecialFeaturesAndThemeSongs(t *testing.T) {
	tmp := t.TempDir()
	st, err := sqlite.Open(filepath.Join(tmp, "extras.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	repo := sqlite.NewLibraryRepo(st)
	ctx := context.Background()

	// Movie library layout with subfolders & sibling extras
	movieRoot := filepath.Join(tmp, "movies")
	movieDir := filepath.Join(movieRoot, "Inception (2010)")
	for _, d := range []string{
		movieDir,
		filepath.Join(movieDir, "trailers"),
		filepath.Join(movieDir, "featurettes"),
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite := func(p string) {
		t.Helper()
		if err := os.WriteFile(p, []byte("fake-media"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(filepath.Join(movieDir, "Inception (2010).mkv"))
	mustWrite(filepath.Join(movieDir, "trailers", "Official Trailer.mp4"))
	mustWrite(filepath.Join(movieDir, "featurettes", "Making Of.mkv"))
	mustWrite(filepath.Join(movieDir, "Inception (2010)-behindthescenes.mkv"))
	mustWrite(filepath.Join(movieDir, "theme.mp3"))

	// TV library layout with Series theme-music & deleted scenes
	tvRoot := filepath.Join(tmp, "tv")
	seriesDir := filepath.Join(tvRoot, "Andor (2022)")
	seasonDir := filepath.Join(seriesDir, "Season 01")
	for _, d := range []string{
		seasonDir,
		filepath.Join(seriesDir, "theme-music"),
		filepath.Join(seriesDir, "deleted scenes"),
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(filepath.Join(seasonDir, "Andor S01E01.mkv"))
	mustWrite(filepath.Join(seriesDir, "theme-music", "Andor Theme.flac"))
	mustWrite(filepath.Join(seriesDir, "deleted scenes", "Cut Scene 1.mkv"))

	movLib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{movieRoot})
	if err != nil {
		t.Fatal(err)
	}
	tvLib, err := repo.CreateLibrary(ctx, "TV", "tvshows", []string{tvRoot})
	if err != nil {
		t.Fatal(err)
	}

	sc := New(repo, plugins.NewRegistry(), nil, config.Default(tmp))
	sc.SetProber(NewDisabledProber())

	if _, err := sc.ScanLibrary(ctx, movLib.ID); err != nil {
		t.Fatalf("ScanLibrary movies: %v", err)
	}
	if _, err := sc.ScanLibrary(ctx, tvLib.ID); err != nil {
		t.Fatalf("ScanLibrary tv: %v", err)
	}

	// Poster wall for Movies library must contain ONLY the 1 Movie (no extras polluting it)
	movItems, movTotal, err := repo.QueryItems(ctx, sqlite.ItemFilter{
		ServerID:  "srv1",
		LibraryID: movLib.ID,
	})
	if err != nil || movTotal != 1 || len(movItems) != 1 || movItems[0].Type != "Movie" {
		t.Fatalf("expected 1 Movie on poster wall, got total=%d items=%+v err=%v", movTotal, movItems, err)
	}
	movieID := movItems[0].Id

	// Full movie detail should report LocalTrailerCount=1 and SpecialFeatureCount=2
	movieDetail, err := repo.GetItemByID(ctx, "srv1", "", movieID)
	if err != nil {
		t.Fatal(err)
	}
	if movieDetail.LocalTrailerCount != 1 || movieDetail.SpecialFeatureCount != 2 {
		t.Fatalf("expected LocalTrailerCount=1 SpecialFeatureCount=2, got %d / %d",
			movieDetail.LocalTrailerCount, movieDetail.SpecialFeatureCount)
	}

	// Verify trailers, special features, and theme songs are indexed and playable (have MediaSources)
	trailers, err := repo.GetItemExtras(ctx, "srv1", "", movieID, []string{"Trailer"})
	if err != nil || len(trailers) != 1 || len(trailers[0].MediaSources) != 1 {
		t.Fatalf("expected 1 playable trailer, got %+v err=%v", trailers, err)
	}
	specials, err := repo.GetItemExtras(ctx, "srv1", "", movieID, []string{"Featurette", "BehindTheScenes"})
	if err != nil || len(specials) != 2 {
		t.Fatalf("expected 2 special features, got %d err=%v", len(specials), err)
	}
	themeSongs, err := repo.GetItemExtras(ctx, "srv1", "", movieID, []string{"ThemeSong"})
	if err != nil || len(themeSongs) != 1 || themeSongs[0].Type != "Audio" || len(themeSongs[0].MediaSources) != 1 {
		t.Fatalf("expected 1 playable theme song, got %+v err=%v", themeSongs, err)
	}

	// TV poster wall should contain only 1 Series (no extras polluting it)
	seriesItems, serTotal, err := repo.QueryItems(ctx, sqlite.ItemFilter{
		ServerID:         "srv1",
		LibraryID:        tvLib.ID,
		IncludeItemTypes: []string{"Series"},
	})
	if err != nil || serTotal != 1 || len(seriesItems) != 1 {
		t.Fatalf("expected 1 Series, got total=%d err=%v", serTotal, err)
	}
	seriesID := seriesItems[0].Id

	tvThemes, err := repo.GetItemExtras(ctx, "srv1", "", seriesID, []string{"ThemeSong"})
	if err != nil || len(tvThemes) != 1 || len(tvThemes[0].MediaSources) != 1 {
		t.Fatalf("expected 1 series theme song, got %+v err=%v", tvThemes, err)
	}
	tvDeleted, err := repo.GetItemExtras(ctx, "srv1", "", seriesID, []string{"DeletedScene"})
	if err != nil || len(tvDeleted) != 1 || len(tvDeleted[0].MediaSources) != 1 {
		t.Fatalf("expected 1 series deleted scene, got %+v err=%v", tvDeleted, err)
	}
}

func TestScanPathSkipsSingleExtraFile(t *testing.T) {
	tmp := t.TempDir()
	st, err := sqlite.Open(filepath.Join(tmp, "scanpath_extra.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	repo := sqlite.NewLibraryRepo(st)
	ctx := context.Background()

	movieRoot := filepath.Join(tmp, "movies")
	movieDir := filepath.Join(movieRoot, "Dune (2021)")
	trailerDir := filepath.Join(movieDir, "trailers")
	if err := os.MkdirAll(trailerDir, 0o755); err != nil {
		t.Fatal(err)
	}
	trailerPath := filepath.Join(trailerDir, "Official Trailer.mp4")
	siblingExtra := filepath.Join(movieDir, "Dune (2021)-featurette.mkv")
	themeAudio := filepath.Join(movieDir, "theme.mp3")
	for _, p := range []string{trailerPath, siblingExtra, themeAudio} {
		if err := os.WriteFile(p, []byte("fake"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	movLib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{movieRoot})
	if err != nil {
		t.Fatal(err)
	}
	sc := New(repo, plugins.NewRegistry(), nil, config.Default(tmp))
	sc.SetProber(NewDisabledProber())

	for _, p := range []string{trailerPath, siblingExtra, themeAudio} {
		stats, err := sc.ScanPath(ctx, movLib.ID, p)
		if err != nil {
			t.Fatalf("ScanPath(%s): %v", p, err)
		}
		if stats.ItemsAdded != 0 {
			t.Fatalf("ScanPath(%s) added %d items, want 0", p, stats.ItemsAdded)
		}
	}

	items, total, err := repo.QueryItems(ctx, sqlite.ItemFilter{ServerID: "srv1", LibraryID: movLib.ID})
	if err != nil || total != 0 || len(items) != 0 {
		t.Fatalf("expected 0 standalone movies on poster wall, got total=%d items=%+v err=%v", total, items, err)
	}
}

func TestFlatMovieRootSkipsSharedExtraSubdirs(t *testing.T) {
	tmp := t.TempDir()
	st, err := sqlite.Open(filepath.Join(tmp, "flat_extras.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	repo := sqlite.NewLibraryRepo(st)
	ctx := context.Background()

	movieRoot := filepath.Join(tmp, "movies")
	if err := os.MkdirAll(filepath.Join(movieRoot, "trailers"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(movieRoot, "extras"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite := func(p string) {
		t.Helper()
		if err := os.WriteFile(p, []byte("fake"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(filepath.Join(movieRoot, "Alpha (2020).mkv"))
	mustWrite(filepath.Join(movieRoot, "Alpha (2020)-trailer.mp4"))
	mustWrite(filepath.Join(movieRoot, "Beta (2021).mkv"))
	mustWrite(filepath.Join(movieRoot, "trailers", "SharedRootTrailer.mp4"))
	mustWrite(filepath.Join(movieRoot, "extras", "SharedRootExtra.mkv"))

	movLib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{movieRoot})
	if err != nil {
		t.Fatal(err)
	}
	sc := New(repo, plugins.NewRegistry(), nil, config.Default(tmp))
	sc.SetProber(NewDisabledProber())

	if _, err := sc.ScanLibrary(ctx, movLib.ID); err != nil {
		t.Fatalf("ScanLibrary: %v", err)
	}

	items, total, err := repo.QueryItems(ctx, sqlite.ItemFilter{ServerID: "srv1", LibraryID: movLib.ID})
	if err != nil || total != 2 {
		t.Fatalf("expected 2 movies, got total=%d err=%v", total, err)
	}
	byName := map[string]string{}
	for _, it := range items {
		byName[it.Name] = it.Id
	}

	alphaTrailers, err := repo.GetItemExtras(ctx, "srv1", "", byName["Alpha"], []string{"Trailer"})
	if err != nil || len(alphaTrailers) != 1 {
		t.Fatalf("expected Alpha to have only its stem-prefixed trailer, got %+v err=%v", alphaTrailers, err)
	}
	alphaClips, err := repo.GetItemExtras(ctx, "srv1", "", byName["Alpha"], []string{"Clip"})
	if err != nil || len(alphaClips) != 0 {
		t.Fatalf("expected Alpha to NOT steal root extras/, got %+v err=%v", alphaClips, err)
	}
	betaTrailers, err := repo.GetItemExtras(ctx, "srv1", "", byName["Beta"], []string{"Trailer"})
	if err != nil || len(betaTrailers) != 0 {
		t.Fatalf("expected Beta to have 0 trailers, got %+v err=%v", betaTrailers, err)
	}
}

func TestExtraItemProbingAndSeriesFastPathExtras(t *testing.T) {
	tmp := t.TempDir()
	st, err := sqlite.Open(filepath.Join(tmp, "probe_fastpath.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	repo := sqlite.NewLibraryRepo(st)
	ctx := context.Background()

	// Create a fake ffprobe script returning deterministic duration (120s) + video/audio streams.
	fakeFFprobe := filepath.Join(tmp, "fake-ffprobe.sh")
	script := `#!/bin/sh
cat <<'EOF'
{
  "format": {"format_name": "mov,mp4", "duration": "120.0", "bit_rate": "5000000", "size": "12345"},
  "streams": [
    {"index": 0, "codec_type": "video", "codec_name": "hevc", "width": 1920, "height": 1080},
    {"index": 1, "codec_type": "audio", "codec_name": "eac3", "channels": 6, "sample_rate": "48000"}
  ]
}
EOF
`
	if err := os.WriteFile(fakeFFprobe, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	tvRoot := filepath.Join(tmp, "tv")
	seriesDir := filepath.Join(tvRoot, "Silo (2023)")
	seasonDir := filepath.Join(seriesDir, "Season 01")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	epPath := filepath.Join(seasonDir, "Silo S01E01.mkv")
	if err := os.WriteFile(epPath, []byte("episode-1"), 0o644); err != nil {
		t.Fatal(err)
	}

	tvLib, err := repo.CreateLibrary(ctx, "TV", "tvshows", []string{tvRoot})
	if err != nil {
		t.Fatal(err)
	}
	sc := New(repo, plugins.NewRegistry(), nil, config.Default(tmp))
	sc.SetProber(NewProber(fakeFFprobe))

	// First scan: no extras yet.
	if _, err := sc.ScanLibrary(ctx, tvLib.ID); err != nil {
		t.Fatalf("first ScanLibrary: %v", err)
	}

	seriesItems, _, err := repo.QueryItems(ctx, sqlite.ItemFilter{
		ServerID:         "srv1",
		LibraryID:        tvLib.ID,
		IncludeItemTypes: []string{"Series"},
	})
	if err != nil || len(seriesItems) != 1 {
		t.Fatalf("expected 1 series, got %+v err=%v", seriesItems, err)
	}
	seriesID := seriesItems[0].Id

	// Now add a trailer under the series dir WITHOUT touching the episode file (exercises indexEpisode fast path).
	trailerDir := filepath.Join(seriesDir, "trailers")
	if err := os.MkdirAll(trailerDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(trailerDir, "Season 1 Trailer.mp4"), []byte("trailer-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Second scan: episode is unchanged, yet series extras MUST be discovered and probed!
	if _, err := sc.ScanLibrary(ctx, tvLib.ID); err != nil {
		t.Fatalf("second ScanLibrary: %v", err)
	}

	trailers, err := repo.GetItemExtras(ctx, "srv1", "", seriesID, []string{"Trailer"})
	if err != nil || len(trailers) != 1 {
		t.Fatalf("expected 1 series trailer after fast-path rescan, got %+v err=%v", trailers, err)
	}
	const wantTicks int64 = 120 * 10_000_000
	if trailers[0].RunTimeTicks == nil || *trailers[0].RunTimeTicks != wantTicks {
		t.Fatalf("expected probed RunTimeTicks=%d on extra, got %v", wantTicks, trailers[0].RunTimeTicks)
	}
	if len(trailers[0].MediaSources) != 1 || len(trailers[0].MediaSources[0].MediaStreams) != 2 {
		t.Fatalf("expected 1 MediaSource with 2 probed MediaStreams, got %+v", trailers[0].MediaSources)
	}
	if trailers[0].MediaSources[0].MediaStreams[0].Codec != "hevc" {
		t.Fatalf("expected probed video codec hevc, got %q", trailers[0].MediaSources[0].MediaStreams[0].Codec)
	}
}

func TestScanPathSingleExtraReindexesParent(t *testing.T) {
	tmp := t.TempDir()
	st, err := sqlite.Open(filepath.Join(tmp, "scanpath_extra_parent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	repo := sqlite.NewLibraryRepo(st)
	ctx := context.Background()

	// Set up Movie library with an already-scanned Movie and no extras yet.
	movieRoot := filepath.Join(tmp, "movies")
	movieDir := filepath.Join(movieRoot, "Interstellar (2014)")
	if err := os.MkdirAll(movieDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(movieDir, "Interstellar (2014).mkv"), []byte("movie-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Set up TV library with an already-scanned Series and no extras yet.
	tvRoot := filepath.Join(tmp, "tv")
	seriesDir := filepath.Join(tvRoot, "Severance (2022)")
	seasonDir := filepath.Join(seriesDir, "Season 01")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seasonDir, "Severance S01E01.mkv"), []byte("ep-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	movLib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{movieRoot})
	if err != nil {
		t.Fatal(err)
	}
	tvLib, err := repo.CreateLibrary(ctx, "TV", "tvshows", []string{tvRoot})
	if err != nil {
		t.Fatal(err)
	}

	bus := eventbus.New()
	sc := New(repo, plugins.NewRegistry(), bus, config.Default(tmp))
	sc.SetProber(NewDisabledProber())

	if _, err := sc.ScanLibrary(ctx, movLib.ID); err != nil {
		t.Fatalf("ScanLibrary movies: %v", err)
	}
	if _, err := sc.ScanLibrary(ctx, tvLib.ID); err != nil {
		t.Fatalf("ScanLibrary tv: %v", err)
	}

	movItems, movTotal, err := repo.QueryItems(ctx, sqlite.ItemFilter{ServerID: "srv1", LibraryID: movLib.ID})
	if err != nil || movTotal != 1 || len(movItems) != 1 {
		t.Fatalf("expected 1 movie before trailer drop, got total=%d err=%v", movTotal, err)
	}
	movieID := movItems[0].Id

	seriesItems, serTotal, err := repo.QueryItems(ctx, sqlite.ItemFilter{
		ServerID:         "srv1",
		LibraryID:        tvLib.ID,
		IncludeItemTypes: []string{"Series"},
	})
	if err != nil || serTotal != 1 || len(seriesItems) != 1 {
		t.Fatalf("expected 1 series before theme drop, got total=%d err=%v", serTotal, err)
	}
	seriesID := seriesItems[0].Id

	var (
		evMu   sync.Mutex
		events []eventbus.Event
	)
	bus.Subscribe(eventbus.EventLibraryChanged, func(_ context.Context, ev eventbus.Event) {
		evMu.Lock()
		events = append(events, ev)
		evMu.Unlock()
	})

	// Drop a new trailers/trailer.mp4 into the already-scanned Movie folder and invoke ScanPath.
	trailerDir := filepath.Join(movieDir, "trailers")
	if err := os.MkdirAll(trailerDir, 0o755); err != nil {
		t.Fatal(err)
	}
	trailerPath := filepath.Join(trailerDir, "trailer.mp4")
	if err := os.WriteFile(trailerPath, []byte("trailer-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	stats, err := sc.ScanPath(ctx, movLib.ID, trailerPath)
	if err != nil {
		t.Fatalf("ScanPath(trailer): %v", err)
	}
	if stats.ItemsAdded != 0 {
		t.Fatalf("expected ItemsAdded=0 for extra file, got %d", stats.ItemsAdded)
	}
	if stats.ItemsUpdated != 1 {
		t.Fatalf("expected ItemsUpdated=1 for parent re-index, got %d", stats.ItemsUpdated)
	}

	// Poster wall must still contain ONLY the 1 Movie (no standalone Movie created).
	movItemsAfter, movTotalAfter, err := repo.QueryItems(ctx, sqlite.ItemFilter{ServerID: "srv1", LibraryID: movLib.ID})
	if err != nil || movTotalAfter != 1 || len(movItemsAfter) != 1 || movItemsAfter[0].Id != movieID {
		t.Fatalf("expected still 1 Movie on poster wall, got total=%d items=%+v err=%v", movTotalAfter, movItemsAfter, err)
	}

	// Trailer must be attached to the existing Movie as a playable Trailer extra.
	trailers, err := repo.GetItemExtras(ctx, "srv1", "", movieID, []string{"Trailer"})
	if err != nil || len(trailers) != 1 || len(trailers[0].MediaSources) != 1 {
		t.Fatalf("expected 1 playable Trailer extra attached to Movie, got %+v err=%v", trailers, err)
	}
	movieDetail, err := repo.GetItemByID(ctx, "srv1", "", movieID)
	if err != nil {
		t.Fatal(err)
	}
	if movieDetail.LocalTrailerCount != 1 {
		t.Fatalf("expected LocalTrailerCount=1, got %d", movieDetail.LocalTrailerCount)
	}

	// Drop a new theme-music/theme.mp3 into the already-scanned Series folder and invoke ScanPath.
	themeDir := filepath.Join(seriesDir, "theme-music")
	if err := os.MkdirAll(themeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	themePath := filepath.Join(themeDir, "theme.mp3")
	if err := os.WriteFile(themePath, []byte("theme-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	tvStats, err := sc.ScanPath(ctx, tvLib.ID, themePath)
	if err != nil {
		t.Fatalf("ScanPath(theme): %v", err)
	}
	if tvStats.ItemsAdded != 0 || tvStats.ItemsUpdated != 1 {
		t.Fatalf("expected ItemsAdded=0 ItemsUpdated=1 for series extra, got %+v", tvStats)
	}
	themes, err := repo.GetItemExtras(ctx, "srv1", "", seriesID, []string{"ThemeSong"})
	if err != nil || len(themes) != 1 || len(themes[0].MediaSources) != 1 {
		t.Fatalf("expected 1 playable ThemeSong extra attached to Series, got %+v err=%v", themes, err)
	}

	// Verify LibraryChanged discovery events were published for both re-indexes.
	evMu.Lock()
	gotEvents := len(events)
	evMu.Unlock()
	if gotEvents < 2 {
		t.Fatalf("expected at least 2 EventLibraryChanged events, got %d", gotEvents)
	}
}
