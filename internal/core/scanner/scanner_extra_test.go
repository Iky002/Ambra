package scanner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

type stubMetaProvider struct {
	id       string
	priority int
	res      *plugins.MetadataResult
	err      error
}

func (p *stubMetaProvider) Manifest() plugins.Manifest {
	return plugins.Manifest{
		ID:       p.id,
		Enabled:  true,
		Priority: p.priority,
		Kinds:    []plugins.Kind{plugins.KindMetadata},
	}
}
func (p *stubMetaProvider) Configure(map[string]any) error { return nil }
func (p *stubMetaProvider) FetchMetadata(context.Context, plugins.MediaQuery) (*plugins.MetadataResult, error) {
	return p.res, p.err
}

func TestParserExtraBranches(t *testing.T) {
	// ParseMovieName with bare year and quality noise
	pm := ParseMovieName("Interstellar.2014.2160p.UHD.BluRay.mkv")
	if pm.Title != "Interstellar" || pm.Year != 2014 {
		t.Fatalf("unexpected ParseMovieName: %+v", pm)
	}

	// ParseEpisodePath with 1x02 format
	ep := ParseEpisodePath("/tv", "/tv/ShowName (2020)/ShowName.1x02.Pilot.1080p.mkv", false)
	if !ep.IsEpisode || ep.SeasonNumber != 1 || ep.EpisodeNumber != 2 || ep.EpisodeTitle != "Pilot" || ep.SeriesYear != 2020 {
		t.Fatalf("unexpected 1x02 episode: %+v", ep)
	}

	// ParseEpisodePath with forceTV inside Season dir without SxxExx
	ep2 := ParseEpisodePath("/tv", "/tv/MyShow/Season 02/SpecialClip.mkv", true)
	if !ep2.IsEpisode || ep2.SeasonNumber != 2 || ep2.SeriesName != "MyShow" {
		t.Fatalf("unexpected forceTV season dir episode: %+v", ep2)
	}

	// ParseEpisodePath with forceTV but no season dir and no SxxExx -> IsEpisode=false
	ep3 := ParseEpisodePath("/tv", "/tv/RandomMovie.mkv", true)
	if ep3.IsEpisode {
		t.Fatalf("expected IsEpisode=false, got %+v", ep3)
	}

	// ParseEpisodePath where seriesDir == cleanRoot and S00E03 -> seasonNum defaults to 1
	ep4 := ParseEpisodePath("/tv", "/tv/S00E03.mkv", false)
	if !ep4.IsEpisode || ep4.SeasonNumber != 1 || ep4.EpisodeNumber != 3 || ep4.EpisodeTitle != "Episode 3" {
		t.Fatalf("unexpected root episode: %+v", ep4)
	}

	// ClassifySubtitle extra branches
	cases := []struct {
		file, codec, wantLang, wantVar, wantTitle string
	}{
		{"a.chs.bilingual.ssa", "", "chi", "chs", "简体双语 (SSA)"},
		{"a.cht.bilingual.ass", "", "chi", "cht", "繁体双语 (ASS)"},
		{"a.中文.srt", "", "chi", "", "中文 (SRT)"},
		{"a.jpn.srt", "", "jpn", "", "日本語 (SRT)"},
		{"a.kor.srt", "", "kor", "", "한국어 (SRT)"},
		{"a.unknown.xyz", "", "und", "", "Subtitle (SUB)"},
		{"a.unknown.xyz", "mov_text", "und", "", "Subtitle (MOV_TEXT)"},
	}
	for _, tc := range cases {
		got := ClassifySubtitle(tc.file, tc.codec)
		if got.Language != tc.wantLang || got.SubVariant != tc.wantVar || got.DisplayTitle != tc.wantTitle {
			t.Errorf("ClassifySubtitle(%q,%q) = (%s,%s,%s), want (%s,%s,%s)",
				tc.file, tc.codec, got.Language, got.SubVariant, got.DisplayTitle, tc.wantLang, tc.wantVar, tc.wantTitle)
		}
	}

	if !containsToken("chs", "chs") {
		t.Fatalf("expected exact containsToken match")
	}
	if tok := TokenizeForFTS("", "测试", "test"); tok == "" {
		t.Fatalf("expected non-empty FTS tokens")
	}
}

func TestScannerExtraBranchesAndMetadataMerge(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "scan2.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	ctx := context.Background()

	// Registry with 3 providers: 1 erroring, 1 partial high-priority, 1 complete low-priority -> exercises mergeMetadata & applyMetadataToItem
	reg := plugins.NewRegistry()
	reg.Register(&stubMetaProvider{id: "p0", priority: 1, err: errors.New("provider err")})
	reg.Register(&stubMetaProvider{
		id:       "p1",
		priority: 10,
		res: &plugins.MetadataResult{
			ProviderIDs: map[string]string{"tmdb": "123"},
		},
	})
	reg.Register(&stubMetaProvider{
		id:       "p2",
		priority: 20,
		res: &plugins.MetadataResult{
			Name:            "Merged Name",
			OriginalTitle:   "Orig Name",
			Overview:        "Overview text",
			Tagline:         "Great Tagline",
			Year:            2022,
			PremiereDate:    "2022-05-10",
			CommunityRating: 8.6,
			OfficialRating:  "PG-13",
			Genres:          []string{"Drama"},
			Studios:         []string{"StudioA"},
			Tags:            []string{"Tag1"},
			People:          []plugins.PersonCredit{{Name: "ActorA", Role: "Hero", Type: "Actor"}},
			ProviderIDs:     map[string]string{"imdb": "tt123"},
			PosterURL:       "http://img/p.jpg",
			BackdropURL:     "http://img/b.jpg",
			LogoURL:         "http://img/l.png",
		},
	})

	mediaDir := filepath.Join(tmp, "media")
	movieDir := filepath.Join(mediaDir, "Movie1")
	_ = os.MkdirAll(filepath.Join(movieDir, ".hidden_dir"), 0o755)
	_ = os.WriteFile(filepath.Join(movieDir, ".hidden_dir", "ignored.mkv"), []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(movieDir, ".DS_Store"), []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(movieDir, "readme.txt"), []byte("x"), 0o644)

	// .webm movie + .strm movie + multiple local images (poster, folder, backdrop, logo, thumb)
	webmPath := filepath.Join(movieDir, "clip.webm")
	strmPath := filepath.Join(movieDir, "remote.strm")
	_ = os.WriteFile(webmPath, []byte("webm-bytes"), 0o644)
	_ = os.WriteFile(strmPath, []byte("https://cdn.example.com/r.mkv"), 0o644)
	_ = os.WriteFile(filepath.Join(movieDir, "poster.jpg"), []byte("p1"), 0o644)
	_ = os.WriteFile(filepath.Join(movieDir, "folder.jpg"), []byte("p2"), 0o644) // triggers duplicate Primary skip
	_ = os.WriteFile(filepath.Join(movieDir, "backdrop.jpg"), []byte("b1"), 0o644)
	_ = os.WriteFile(filepath.Join(movieDir, "logo.png"), []byte("l1"), 0o644)
	_ = os.WriteFile(filepath.Join(movieDir, "thumb.jpg"), []byte("t1"), 0o644)

	// TV show without Season subdir (seasonDir == "") + episode thumb + series poster
	tvDir := filepath.Join(tmp, "tv")
	showDir := filepath.Join(tvDir, "FlatShow (2021)")
	_ = os.MkdirAll(showDir, 0o755)
	ep1Path := filepath.Join(showDir, "FlatShow.S01E01.mkv")
	ep2Path := filepath.Join(showDir, "FlatShow.S01E02.mkv")
	_ = os.WriteFile(ep1Path, []byte("ep1"), 0o644)
	_ = os.WriteFile(ep2Path, []byte("ep2"), 0o644)
	_ = os.WriteFile(filepath.Join(showDir, "FlatShow.S01E01-thumb.jpg"), []byte("epthumb"), 0o644)
	_ = os.WriteFile(filepath.Join(showDir, "poster.jpg"), []byte("showposter"), 0o644)

	lib1, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{"", mediaDir, filepath.Join(tmp, "nonexistent")})
	if err != nil {
		t.Fatal(err)
	}
	lib2, err := repo.CreateLibrary(ctx, "TV", "tvshows", []string{tvDir})
	if err != nil {
		t.Fatal(err)
	}

	// Pass nil cfg to test s.language() default fallback
	sc := New(repo, reg, nil, nil)
	if sc.language() != "zh-CN" {
		t.Fatalf("expected default zh-CN when cfg is nil")
	}

	// ScanAll
	allStats, err := sc.ScanAll(ctx)
	if err != nil || len(allStats) != 2 {
		t.Fatalf("ScanAll failed: stats=%+v err=%v", allStats, err)
	}
	if allStats[0].ItemsAdded != 2 || allStats[0].Errors == 0 {
		t.Fatalf("unexpected lib1 stats: %+v", allStats[0])
	}
	if allStats[1].ItemsAdded != 2 {
		t.Fatalf("unexpected lib2 stats: %+v", allStats[1])
	}

	// Modify webm and ep1 to trigger update path (existing != nil)
	_ = os.WriteFile(webmPath, []byte("webm-bytes-updated"), 0o644)
	_ = os.WriteFile(ep1Path, []byte("ep1-updated"), 0o644)
	stUpd1, err := sc.ScanLibrary(ctx, lib1.ID)
	if err != nil || stUpd1.ItemsAdded != 1 || stUpd1.ItemsSkipped != 1 {
		t.Fatalf("expected 1 updated and 1 skipped in lib1, got %+v err=%v", stUpd1, err)
	}
	stUpd2, err := sc.ScanLibrary(ctx, lib2.ID)
	if err != nil || stUpd2.ItemsAdded != 1 || stUpd2.ItemsSkipped != 1 {
		t.Fatalf("expected 1 updated and 1 skipped in lib2, got %+v err=%v", stUpd2, err)
	}

	// Missing library ID returns error
	if _, err := sc.ScanLibrary(ctx, "nonexistent-lib"); err == nil {
		t.Fatalf("expected error for missing library")
	}

	// Concurrent lock guard: mark scanning=true and verify immediate return
	sc.mu.Lock()
	sc.scanning[lib1.ID] = true
	sc.mu.Unlock()
	if st, err := sc.ScanLibrary(ctx, lib1.ID); err != nil || st.ItemsAdded != 0 {
		t.Fatalf("expected immediate no-op when already scanning, got %+v err=%v", st, err)
	}
	sc.mu.Lock()
	delete(sc.scanning, lib1.ID)
	sc.mu.Unlock()

	// ScanLibraryAsync (both single and all)
	sc.ScanLibraryAsync(lib1.ID)
	sc.ScanLibraryAsync("")
	time.Sleep(80 * time.Millisecond)

	// Nil registry / empty registry fetchMergedMetadata
	scNilReg := New(repo, nil, nil, config.Default(tmp))
	if m := scNilReg.fetchMergedMetadata(ctx, plugins.MediaQuery{}); m != nil {
		t.Fatalf("expected nil for nil registry")
	}
	scEmptyReg := New(repo, plugins.NewRegistry(), nil, config.Default(tmp))
	if m := scEmptyReg.fetchMergedMetadata(ctx, plugins.MediaQuery{}); m != nil {
		t.Fatalf("expected nil for empty registry")
	}
	if imgs := discoverLocalImages("id", "", filepath.Join(tmp, "nope"), false); imgs != nil {
		t.Fatalf("expected nil images for missing dir")
	}
	if subs := discoverExternalSubtitles(filepath.Join(tmp, "nope", "a.mkv"), "ms", "id", 2); subs != nil {
		t.Fatalf("expected nil subs for missing dir")
	}
}
