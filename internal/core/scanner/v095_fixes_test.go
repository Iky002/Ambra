package scanner

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestParseMovieName_LeadingBracketYear(t *testing.T) {
	cases := []struct {
		input     string
		wantTitle string
		wantYear  int
	}{
		{"[2019] Parasite.mkv", "Parasite", 2019},
		{"(2020) Tenet.mkv", "Tenet", 2020},
		{"【2021】 Dune.2160p.mkv", "Dune", 2021},
		{"（2018） Burning.mkv", "Burning", 2018},
		{"[SubGroup][2019] Parasite.1080p.mkv", "Parasite", 2019},
		{"[2099] Future.mkv", "Future", 0},
	}
	for _, tc := range cases {
		got := ParseMovieName(tc.input)
		if got.Title != tc.wantTitle || got.Year != tc.wantYear {
			t.Errorf("ParseMovieName(%q) = (Title=%q, Year=%d), want (%q, %d)",
				tc.input, got.Title, got.Year, tc.wantTitle, tc.wantYear)
		}
	}
}

func TestParseEpisodePath_SeriesOriginalTitleAndProviderIDs(t *testing.T) {
	root := "/media/tv"
	path := "/media/tv/权力的游戏 Game of Thrones (2011) [tmdbid=1399]/Season 01/S01E01.mkv"
	ep := ParseEpisodePath(root, path, true)
	if !ep.IsEpisode {
		t.Fatalf("expected IsEpisode=true")
	}
	if ep.SeriesName != "权力的游戏" {
		t.Errorf("SeriesName = %q, want 权力的游戏", ep.SeriesName)
	}
	if ep.SeriesOriginalTitle != "Game of Thrones" {
		t.Errorf("SeriesOriginalTitle = %q, want Game of Thrones", ep.SeriesOriginalTitle)
	}
	if ep.SeriesYear != 2011 {
		t.Errorf("SeriesYear = %d, want 2011", ep.SeriesYear)
	}
	if ep.SeriesProviderIDs["tmdb"] != "1399" {
		t.Errorf("SeriesProviderIDs[tmdb] = %q, want 1399", ep.SeriesProviderIDs["tmdb"])
	}
}

type captureSeriesMetaProvider struct {
	mu      sync.Mutex
	queries []plugins.MediaQuery
	done    chan struct{}
	once    sync.Once
}

func (c *captureSeriesMetaProvider) Manifest() plugins.Manifest {
	return plugins.Manifest{
		ID:       "test.capture.online",
		Name:     "Capture Online",
		Enabled:  true,
		Priority: 20,
		Kinds:    []plugins.Kind{plugins.KindMetadata},
	}
}

func (c *captureSeriesMetaProvider) Configure(map[string]any) error { return nil }

func (c *captureSeriesMetaProvider) FetchMetadata(ctx context.Context, q plugins.MediaQuery) (*plugins.MetadataResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	c.mu.Lock()
	c.queries = append(c.queries, q)
	c.mu.Unlock()
	if c.done != nil && q.ItemType == "Series" {
		c.once.Do(func() { close(c.done) })
	}
	return &plugins.MetadataResult{
		Name:        q.Title,
		Overview:    "Scraped overview",
		ProviderIDs: q.ProviderIDs,
	}, nil
}

func TestEnsureSeries_PassesOriginalTitleAndProviderIDsToMetadataQuery(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "series.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	reg := plugins.NewRegistry()
	prov := &captureSeriesMetaProvider{}
	reg.Register(prov)

	tvDir := filepath.Join(tmp, "tv")
	epDir := filepath.Join(tvDir, "权力的游戏 Game of Thrones (2011) [tmdbid=1399]", "Season 01")
	if err := os.MkdirAll(epDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(epDir, "S01E01.mkv"), []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	lib, err := repo.CreateLibrary(ctx, "TV", "tvshows", []string{tvDir})
	if err != nil {
		t.Fatal(err)
	}
	sc := New(repo, reg, eventbus.New(), config.Default(tmp))
	sc.SetProber(nil)

	if _, err := sc.ScanLibrary(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}

	prov.mu.Lock()
	defer prov.mu.Unlock()
	var found bool
	for _, q := range prov.queries {
		if q.ItemType == "Series" {
			found = true
			if q.OriginalTitle != "Game of Thrones" {
				t.Errorf("Series MediaQuery.OriginalTitle = %q, want Game of Thrones", q.OriginalTitle)
			}
			if q.ProviderIDs["tmdb"] != "1399" {
				t.Errorf("Series MediaQuery.ProviderIDs[tmdb] = %q, want 1399", q.ProviderIDs["tmdb"])
			}
		}
	}
	if !found {
		t.Fatalf("expected Series MediaQuery to be issued, got %+v", prov.queries)
	}
}

func TestClassifySubtitle_NoFalsePositiveOnTitleCJK(t *testing.T) {
	cases := []struct {
		file        string
		wantLang    string
		wantVariant string
		wantDisplay string
	}{
		{"繁花.S01E01.eng.srt", "eng", "", "English (SRT)"},
		{"简爱.1943.eng.srt", "eng", "", "English (SRT)"},
		{"繁花.S01E01.srt", "und", "", "Subtitle (SRT)"},
		{"繁花.S01E01.简.srt", "chi", "chs", "简体中文 (SRT)"},
		{"简爱.1943.[繁].ass", "chi", "cht", "繁体中文 (ASS)"},
		{"繁花.S01E01.简体.srt", "chi", "chs", "简体中文 (SRT)"},
		{"简爱.1943.繁體.ass", "chi", "cht", "繁体中文 (ASS)"},
	}
	for _, tc := range cases {
		got := ClassifySubtitle(tc.file, "")
		if got.Language != tc.wantLang || got.SubVariant != tc.wantVariant || got.DisplayTitle != tc.wantDisplay {
			t.Errorf("ClassifySubtitle(%q) = (lang=%q, var=%q, title=%q), want (%q, %q, %q)",
				tc.file, got.Language, got.SubVariant, got.DisplayTitle, tc.wantLang, tc.wantVariant, tc.wantDisplay)
		}
	}
}

type slowOnlineProvider struct {
	started chan struct{}
	release chan struct{}
	ran     atomic.Int32
}

func (s *slowOnlineProvider) Manifest() plugins.Manifest {
	return plugins.Manifest{
		ID:       "test.slow.online",
		Name:     "Slow Online",
		Enabled:  true,
		Priority: 20,
		Kinds:    []plugins.Kind{plugins.KindMetadata},
	}
}

func (s *slowOnlineProvider) Configure(map[string]any) error { return nil }

func (s *slowOnlineProvider) FetchMetadata(ctx context.Context, q plugins.MediaQuery) (*plugins.MetadataResult, error) {
	select {
	case s.started <- struct{}{}:
	default:
	}
	select {
	case <-s.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	s.ran.Add(1)
	return &plugins.MetadataResult{
		Name:     q.Title,
		Overview: "Enriched after caller ctx canceled",
	}, nil
}

func TestScanLibraryProgressive_SurvivesCallerContextCancel(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "prog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	reg := plugins.NewRegistry()
	prov := &slowOnlineProvider{
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	reg.Register(prov)

	moviesDir := filepath.Join(tmp, "movies")
	if err := os.MkdirAll(moviesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moviesDir, "Tenet (2020).mkv"), []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}

	bgCtx := context.Background()
	lib, err := repo.CreateLibrary(bgCtx, "Movies", "movies", []string{moviesDir})
	if err != nil {
		t.Fatal(err)
	}

	sc := New(repo, reg, eventbus.New(), config.Default(tmp))
	sc.SetProber(nil)

	reqCtx, reqCancel := context.WithCancel(bgCtx)
	stats, err := sc.ScanLibraryProgressive(reqCtx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stats.ItemsAdded != 1 {
		t.Fatalf("expected ItemsAdded=1, got %d", stats.ItemsAdded)
	}

	// Simulate HTTP request completion immediately canceling reqCtx.
	reqCancel()

	// Wait for Phase B to start, then release it.
	select {
	case <-prov.started:
	case <-time.After(3 * time.Second):
		t.Fatal("Phase B never started")
	}
	close(prov.release)

	waitCtx, waitCancel := context.WithTimeout(bgCtx, 3*time.Second)
	defer waitCancel()
	sc.WaitIdle(waitCtx)

	if prov.ran.Load() == 0 {
		t.Fatalf("expected Phase B enrichment to complete after caller ctx cancel")
	}
}
