package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestSeasonArtworkFromSeriesDirectory(t *testing.T) {
	ctx := context.Background()
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

	libRepo := sqlite.NewLibraryRepo(store)
	reg := plugins.NewRegistry()
	bus := eventbus.New()
	sc := New(libRepo, reg, bus, cfg)

	tvRoot := filepath.Join(tmp, "tv")
	seriesDir := filepath.Join(tvRoot, "Severance (2022)")
	s01Dir := filepath.Join(seriesDir, "Season 01")
	s00Dir := filepath.Join(seriesDir, "Season 00")
	for _, d := range []string{s01Dir, s00Dir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", d, err)
		}
	}

	// Write Kodi/Emby season artwork in the parent Series directory
	artFiles := map[string][]byte{
		filepath.Join(seriesDir, "poster.jpg"):                []byte("series-poster"),
		filepath.Join(seriesDir, "season01-poster.jpg"):       []byte("s01-poster"),
		filepath.Join(seriesDir, "season01-fanart.jpg"):       []byte("s01-fanart"),
		filepath.Join(seriesDir, "season01-landscape.jpg"):    []byte("s01-thumb"),
		filepath.Join(seriesDir, "season01-banner.jpg"):       []byte("s01-banner"),
		filepath.Join(seriesDir, "season-specials-poster.png"): []byte("specials-poster"),
	}
	for p, data := range artFiles {
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", p, err)
		}
	}

	// Write episode files
	for _, ep := range []string{
		filepath.Join(s01Dir, "Severance S01E01.mkv"),
		filepath.Join(s00Dir, "Severance S00E01.mkv"),
	} {
		if err := os.WriteFile(ep, []byte("fake-video"), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", ep, err)
		}
	}

	lib, err := libRepo.CreateLibrary(ctx, "TV", "tvshows", []string{tvRoot})
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}
	if _, err := sc.ScanLibrary(ctx, lib.ID); err != nil {
		t.Fatalf("ScanLibrary: %v", err)
	}

	seriesRec, err := libRepo.FindItemByPath(ctx, lib.ID, seriesDir)
	if err != nil || seriesRec == nil {
		t.Fatalf("FindItemByPath series: %v", err)
	}

	// Season 1 must have Primary (season01-poster.jpg), Backdrop (season01-fanart.jpg), Thumb, Banner
	s01Rec, err := libRepo.FindSeasonByNumber(ctx, seriesRec.ID, 1)
	if err != nil || s01Rec == nil {
		t.Fatalf("FindSeasonByNumber(1): %v", err)
	}
	wantS01 := map[string]string{
		"Primary":  "season01-poster.jpg",
		"Backdrop": "season01-fanart.jpg",
		"Thumb":    "season01-landscape.jpg",
		"Banner":   "season01-banner.jpg",
	}
	for imgType, wantBase := range wantS01 {
		img, err := libRepo.GetImageByItemAndType(ctx, s01Rec.ID, imgType, 0)
		if err != nil || img == nil {
			t.Fatalf("GetImageByItemAndType(s01, %s): %v", imgType, err)
		}
		if filepath.Base(img.Path) != wantBase {
			t.Fatalf("expected Season 1 %s=%s, got %s", imgType, wantBase, filepath.Base(img.Path))
		}
	}

	// Season 0 (Specials) must have Primary=season-specials-poster.png
	s00Rec, err := libRepo.FindSeasonByNumber(ctx, seriesRec.ID, 0)
	if err != nil || s00Rec == nil {
		t.Fatalf("FindSeasonByNumber(0): %v", err)
	}
	s00Img, err := libRepo.GetImageByItemAndType(ctx, s00Rec.ID, "Primary", 0)
	if err != nil || s00Img == nil {
		t.Fatalf("GetImageByItemAndType(s00, Primary): %v", err)
	}
	if filepath.Base(s00Img.Path) != "season-specials-poster.png" {
		t.Fatalf("expected Season 0 Primary=season-specials-poster.png, got %s", filepath.Base(s00Img.Path))
	}
}
