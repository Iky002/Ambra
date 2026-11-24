package scanner

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/plugins/nfo"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestClassifySubtitle(t *testing.T) {
	cases := []struct {
		file        string
		wantVariant string
		wantDisplay string
	}{
		{"Movie.chs.ass", "chs", "简体中文 (ASS)"},
		{"Movie.zh-Hans.srt", "chs", "简体中文 (SRT)"},
		{"Movie.简体.ass", "chs", "简体中文 (ASS)"},
		{"Movie.cht.sup", "cht", "繁体中文 (PGS)"},
		{"Movie.tc.vtt", "cht", "繁体中文 (VTT)"},
		{"Movie.繁體.ass", "cht", "繁体中文 (ASS)"},
		{"Movie.双语.ass", "bilingual", "双语中文 (ASS)"},
		{"Movie.chs.cht.srt", "bilingual", "简繁双语 (SRT)"},
		{"Movie.eng.srt", "", "English (SRT)"},
	}

	for _, tc := range cases {
		got := ClassifySubtitle(tc.file, "")
		if got.SubVariant != tc.wantVariant || got.DisplayTitle != tc.wantDisplay {
			t.Errorf("ClassifySubtitle(%q) = (variant=%q, title=%q); want (%q, %q)",
				tc.file, got.SubVariant, got.DisplayTitle, tc.wantVariant, tc.wantDisplay)
		}
	}
}

func TestScanLibraryAndChineseFTS5Search(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "test.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	reg := plugins.NewRegistry()
	reg.Register(nfo.New())
	bus := eventbus.New()
	cfg := config.Default(tmp)

	var eventCount atomic.Int32
	bus.Subscribe(eventbus.EventLibraryChanged, func(_ context.Context, _ eventbus.Event) {
		eventCount.Add(1)
	})

	// Build a movie directory with NFO, external CHS/CHT subtitles, and poster
	moviesDir := filepath.Join(tmp, "movies")
	m1Dir := filepath.Join(moviesDir, "The Wandering Earth (2019)")
	m2Dir := filepath.Join(moviesDir, "Inception (2010)")
	_ = os.MkdirAll(m1Dir, 0o755)
	_ = os.MkdirAll(m2Dir, 0o755)

	_ = os.WriteFile(filepath.Join(m1Dir, "wandering.mkv"), []byte("dummy-video-1"), 0o644)
	_ = os.WriteFile(filepath.Join(m1Dir, "wandering.nfo"), []byte(`<movie><title>流浪地球</title><originaltitle>The Wandering Earth</originaltitle><year>2019</year><genre>科幻</genre><director>郭帆</director></movie>`), 0o644)
	_ = os.WriteFile(filepath.Join(m1Dir, "wandering.chs.ass"), []byte("[Script Info]\n"), 0o644)
	_ = os.WriteFile(filepath.Join(m1Dir, "wandering.cht.sup"), []byte("PGS"), 0o644)
	_ = os.WriteFile(filepath.Join(m1Dir, "poster.jpg"), []byte("\xff\xd8\xff\xe0jpg"), 0o644)

	_ = os.WriteFile(filepath.Join(m2Dir, "inception.mp4"), []byte("dummy-video-2"), 0o644)
	_ = os.WriteFile(filepath.Join(m2Dir, "inception.nfo"), []byte(`<movie><title>盗梦空间</title><originaltitle>Inception</originaltitle><year>2010</year><genre>悬疑</genre><director>克里斯托弗·诺兰</director></movie>`), 0o644)

	// Build a TV show directory: Series/Season 01/S01E01...
	tvDir := filepath.Join(tmp, "tvshows")
	epDir := filepath.Join(tvDir, "Three Body (2023)", "Season 01")
	_ = os.MkdirAll(epDir, 0o755)
	_ = os.WriteFile(filepath.Join(tvDir, "Three Body (2023)", "tvshow.nfo"), []byte(`<tvshow><title>三体</title><year>2023</year></tvshow>`), 0o644)
	_ = os.WriteFile(filepath.Join(epDir, "Three.Body.S01E01.mkv"), []byte("ep1"), 0o644)

	ctx := context.Background()
	movieLib, err := repo.CreateLibrary(ctx, "电影", "movies", []string{moviesDir})
	if err != nil {
		t.Fatal(err)
	}
	tvLib, err := repo.CreateLibrary(ctx, "剧集", "tvshows", []string{tvDir})
	if err != nil {
		t.Fatal(err)
	}

	sc := New(repo, reg, bus, cfg)
	stats, err := sc.ScanLibrary(ctx, movieLib.ID)
	if err != nil {
		t.Fatalf("ScanLibrary movies: %v", err)
	}
	if stats.ItemsAdded != 2 {
		t.Fatalf("expected 2 movies added, got %+v", stats)
	}

	tvStats, err := sc.ScanLibrary(ctx, tvLib.ID)
	if err != nil {
		t.Fatalf("ScanLibrary tv: %v", err)
	}
	if tvStats.ItemsAdded != 1 {
		t.Fatalf("expected 1 episode added, got %+v", tvStats)
	}

	// Verify incremental re-scan skips unchanged files
	stats2, err := sc.ScanLibrary(ctx, movieLib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stats2.ItemsAdded != 0 || stats2.ItemsSkipped != 2 {
		t.Fatalf("expected 2 skipped on re-scan, got %+v", stats2)
	}

	// Verify Chinese out-of-order multi-keyword FTS5 search: "地球 流浪" -> 流浪地球
	items, total, err := repo.QueryItems(ctx, sqlite.ItemFilter{
		ServerID:   cfg.ServerID,
		SearchTerm: "地球 流浪",
		Fields:     []string{"MediaSources", "Genres", "People"},
	})
	if err != nil {
		t.Fatalf("FTS5 query failed: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].Name != "流浪地球" {
		t.Fatalf("expected 流浪地球 for '地球 流浪', got total=%d items=%+v", total, items)
	}
	// Verify external CHS/CHT subtitles were persisted
	if len(items[0].MediaSources) != 1 || len(items[0].MediaSources[0].MediaStreams) < 4 {
		t.Fatalf("expected video+audio+2 external subtitles, got %+v", items[0].MediaSources)
	}
	foundCHS, foundCHT := false, false
	for _, st := range items[0].MediaSources[0].MediaStreams {
		if st.SubVariant == "chs" && st.DisplayTitle == "简体中文 (ASS)" {
			foundCHS = true
		}
		if st.SubVariant == "cht" && st.DisplayTitle == "繁体中文 (PGS)" {
			foundCHT = true
		}
	}
	if !foundCHS || !foundCHT {
		t.Fatalf("expected both CHS and CHT subtitles persisted, got %+v", items[0].MediaSources[0].MediaStreams)
	}

	// Verify cross-column search (director + title): "诺兰 盗梦" -> 盗梦空间
	hints, hintTotal, err := repo.SearchHints(ctx, sqlite.SearchHintFilter{
		SearchTerm: "诺兰 盗梦",
	})
	if err != nil {
		t.Fatalf("SearchHints failed: %v", err)
	}
	if hintTotal != 1 || len(hints) != 1 || hints[0].Name != "盗梦空间" {
		t.Fatalf("expected 盗梦空间 for '诺兰 盗梦', got total=%d hints=%+v", hintTotal, hints)
	}

	// Verify Series -> Season -> Episode hierarchy & recursive ancestors
	eps, epTotal, err := repo.QueryItems(ctx, sqlite.ItemFilter{
		ServerID:         cfg.ServerID,
		ParentID:         tvLib.ID,
		Recursive:        true,
		IncludeItemTypes: []string{"Episode"},
	})
	if err != nil || epTotal != 1 || len(eps) != 1 || eps[0].SeriesName != "三体" {
		t.Fatalf("expected recursive episode under tvLib, got total=%d eps=%+v err=%v", epTotal, eps, err)
	}

	// Wait briefly for async eventbus handler
	time.Sleep(30 * time.Millisecond)
	if eventCount.Load() < 2 {
		t.Fatalf("expected EventLibraryChanged to fire, got %d", eventCount.Load())
	}
}
