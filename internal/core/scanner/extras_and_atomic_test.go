package scanner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
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
