package scanner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/plugins/nfo"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestLibraryScraperOptionsAndProviderFilteringV0110(t *testing.T) {
	// Unit-test alias normalization and option slice extractors across all branches.
	aliasCases := map[string]string{
		"TheMovieDB":        "tmdb",
		"ambra.builtin.tmdb": "tmdb",
		"TheTVDB Scraper":   "tvdb",
		"ambra.builtin.tvdb": "tvdb",
		"Local NFO":         "nfo",
		"kodi nfo":          "nfo",
		"CustomScraper":     "customscraper",
	}
	for in, want := range aliasCases {
		if got := normalizeFetcherAlias(in); got != want {
			t.Fatalf("normalizeFetcherAlias(%q) = %q, want %q", in, got, want)
		}
	}

	if en, dis, ord, ok := extractFetcherListFromAny(nil); ok || en != nil || dis != nil || ord != nil {
		t.Fatalf("expected nil extractFetcherListFromAny to return false, got %v %v %v %v", en, dis, ord, ok)
	}
	if en, _, _, ok := extractFetcherListFromAny(12345); ok || en != nil {
		t.Fatalf("expected unsupported int extractFetcherListFromAny to return false")
	}
	if en, dis, ord, ok := extractFetcherListFromAny([]string{" tmdb ", "", "tvdb"}); !ok || len(en) != 2 || dis != nil || ord != nil {
		t.Fatalf("unexpected []string extractFetcherListFromAny: %v %v %v %v", en, dis, ord, ok)
	}
	if en, dis, ord, ok := extractFetcherListFromAny([]any{" tmdb ", ""}); !ok || len(en) != 1 || dis != nil || ord != nil {
		t.Fatalf("unexpected []any string extractFetcherListFromAny: %v %v %v %v", en, dis, ord, ok)
	}
	objFetchers := []any{
		map[string]any{"Name": "TheTVDB", "Enabled": true},
		map[string]any{"Id": "ambra.builtin.tmdb", "Enabled": false},
		map[string]any{"Name": "   "},
	}
	en, dis, ord, ok := extractFetcherListFromAny(objFetchers)
	if !ok || len(en) != 1 || en[0] != "TheTVDB" || len(dis) != 1 || dis[0] != "ambra.builtin.tmdb" || len(ord) != 2 {
		t.Fatalf("unexpected object []any extractFetcherListFromAny: en=%v dis=%v ord=%v ok=%v", en, dis, ord, ok)
	}

	if got := extractStringSliceAny(nil); got != nil {
		t.Fatalf("expected nil extractStringSliceAny, got %v", got)
	}
	if got := extractStringSliceAny([]string{" a ", "", "b"}); len(got) != 2 {
		t.Fatalf("unexpected []string extractStringSliceAny: %v", got)
	}
	if got := extractStringSliceAny([]any{" x ", 42, "", "y"}); len(got) != 2 {
		t.Fatalf("unexpected []any extractStringSliceAny: %v", got)
	}

	if findTypeOptionMap(map[string]any{}, "Movie") != nil {
		t.Fatal("expected nil findTypeOptionMap on empty options")
	}
	typeOpts := map[string]any{
		"TypeOptions": []any{
			"invalid-entry",
			map[string]any{"Type": "Movie", "MetadataFetcherOrder": []any{"tmdb"}},
			map[string]any{"Type": "Series", "MetadataFetcherOrder": []any{"tvdb", "tmdb"}},
		},
	}
	if m := findTypeOptionMap(typeOpts, "Movie"); m == nil || m["Type"] != "Movie" {
		t.Fatalf("expected Movie TypeOption match, got %v", m)
	}
	// Season and Episode inherit Series TypeOptions when not explicitly listed.
	if m := findTypeOptionMap(typeOpts, "Episode"); m == nil || m["Type"] != "Series" {
		t.Fatalf("expected Episode to fall back to Series TypeOption, got %v", m)
	}
	if m := findTypeOptionMap(typeOpts, "Season"); m == nil || m["Type"] != "Series" {
		t.Fatalf("expected Season to fall back to Series TypeOption, got %v", m)
	}
	if m := findTypeOptionMap(typeOpts, "Audio"); m != nil {
		t.Fatalf("expected Audio TypeOption lookup to return nil, got %v", m)
	}

	// Nil registry scanner guard branches.
	scNilReg := New(nil, nil, nil, nil)
	if scNilReg.metadataProvidersForLibrary(nil, "Movie") != nil {
		t.Fatal("expected nil providers when registry is nil")
	}
	if scNilReg.hasOnlineMetadataProviders() {
		t.Fatal("expected hasOnlineMetadataProviders=false when registry is nil")
	}
	if scNilReg.fetchMetadataFiltered(context.Background(), plugins.MediaQuery{ItemType: "Movie"}, false) != nil {
		t.Fatal("expected fetchMetadataFiltered=nil when registry is nil")
	}

	emptyReg := plugins.NewRegistry()
	scEmptyReg := New(nil, emptyReg, nil, nil)
	if scEmptyReg.hasOnlineMetadataProviders() {
		t.Fatal("expected hasOnlineMetadataProviders=false on empty registry")
	}
	if scEmptyReg.fetchMetadataFiltered(context.Background(), plugins.MediaQuery{ItemType: "Movie"}, false) != nil {
		t.Fatal("expected fetchMetadataFiltered=nil on empty registry")
	}

	// Registry with NFO + 3 online providers (one erroring, two merging).
	reg := plugins.NewRegistry()
	reg.Register(nfo.New())
	reg.Register(&zhMockOnlineProvider{
		id: "ext.err.provider",
		fetchFn: func(_ context.Context, _ plugins.MediaQuery) (*plugins.MetadataResult, error) {
			return nil, errors.New("upstream error")
		},
	})
	reg.Register(&zhMockOnlineProvider{
		id: "ambra.builtin.tmdb",
		fetchFn: func(_ context.Context, _ plugins.MediaQuery) (*plugins.MetadataResult, error) {
			return &plugins.MetadataResult{
				Name:        "TMDB Title",
				ProviderIDs: map[string]string{"Tmdb": "101"},
			}, nil
		},
	})
	reg.Register(&zhMockOnlineProvider{
		id: "ambra.builtin.tvdb",
		fetchFn: func(_ context.Context, q plugins.MediaQuery) (*plugins.MetadataResult, error) {
			return &plugins.MetadataResult{
				Overview: "TVDB Overview for " + q.ProviderIDs["Tmdb"],
				Tagline:  "TVDB Tagline",
			}, nil
		},
	})

	sc := New(nil, reg, nil, nil)
	if !sc.hasOnlineMetadataProviders() {
		t.Fatal("expected hasOnlineMetadataProviders=true")
	}
	mergedOnline := sc.fetchMetadataFiltered(context.Background(), plugins.MediaQuery{ItemType: "Movie", Title: "Test"}, false)
	if mergedOnline == nil || mergedOnline.Name != "TMDB Title" || !strings.Contains(mergedOnline.Overview, "101") {
		t.Fatalf("unexpected fetchMetadataFiltered merge result: %+v", mergedOnline)
	}

	// Default library options with no fetcher overrides returns all 4 enabled providers.
	defLib := &sqlite.LibraryRecord{ID: "lib-def", OptionsJSON: `{"SaveLocalMetadata":true}`}
	if got := sc.metadataProvidersForLibrary(defLib, "Movie"); len(got) != 4 {
		t.Fatalf("expected 4 providers for default options, got %d", len(got))
	}

	// Explicit MetadataFetchers + TypeOptions override keeps NFO active unless DisabledLocalMetadataReaders disables it.
	customLib := &sqlite.LibraryRecord{
		ID: "lib-custom",
		OptionsJSON: `{
			"MetadataFetchers": [{"Name":"TheMovieDb","Enabled":true}],
			"MetadataFetcherOrder": ["tmdb"],
			"TypeOptions": [{
				"Type": "Series",
				"MetadataFetchers": [{"Name":"TheTVDB","Enabled":true},{"Name":"TheMovieDb","Enabled":true}],
				"DisabledMetadataFetchers": ["ext.err.provider"],
				"MetadataFetcherOrder": ["tvdb","tmdb"]
			}]
		}`,
	}
	epProviders := sc.metadataProvidersForLibrary(customLib, "Episode")
	if len(epProviders) != 3 || epProviders[0].Manifest().ID != "ambra.builtin.tvdb" || epProviders[1].Manifest().ID != "ambra.builtin.tmdb" {
		t.Fatalf("unexpected Episode providers with Series TypeOptions override: %+v", epProviders)
	}

	// Partial DisabledMetadataFetchers without explicit MetadataFetchers keeps remaining online + NFO providers.
	partialLib := &sqlite.LibraryRecord{
		ID:          "lib-partial",
		OptionsJSON: `{"DisabledMetadataFetchers":["tvdb","ext.err.provider"]}`,
	}
	if got := sc.metadataProvidersForLibrary(partialLib, "Movie"); len(got) != 2 {
		t.Fatalf("expected 2 providers (nfo + tmdb) when tvdb & ext.err disabled, got %d", len(got))
	}

	// Disable all online providers and local NFO reader -> 0 providers & hasLibraryOnlineMetadataProviders=false.
	offlineLib := &sqlite.LibraryRecord{
		ID:          "lib-offline",
		OptionsJSON: `{"DisabledMetadataFetchers":["tmdb","tvdb","ext.err.provider"],"DisabledLocalMetadataReaders":["nfo"]}`,
	}
	if sc.hasLibraryOnlineMetadataProviders(offlineLib, "Movie") {
		t.Fatal("expected hasLibraryOnlineMetadataProviders=false when all online providers are disabled")
	}
	if got := sc.metadataProvidersForLibrary(offlineLib, "Movie"); len(got) != 0 {
		t.Fatalf("expected 0 providers when all online + local NFO are disabled, got %d", len(got))
	}
}

func TestCompanionSidecarsLocalArtworkAndSubtitlesV0110(t *testing.T) {
	if isExtraFieldLocked(nil, "Name") {
		t.Fatal("expected nil ItemExtra not locked")
	}
	if !isExtraFieldLocked(&sqlite.ItemExtra{LockData: true}, "Anything") {
		t.Fatal("expected LockData=true to lock all fields")
	}
	if !isExtraFieldLocked(&sqlite.ItemExtra{LockedFields: []string{" Overview "}}, "overview") {
		t.Fatal("expected case-insensitive LockedFields match")
	}

	s1 := []sqlite.MediaStreamRecord{{Path: "/a.srt", Codec: "srt", Language: "eng", StreamIndex: 2}}
	s2 := []sqlite.MediaStreamRecord{{Path: "/a.srt", Codec: "srt", Language: "zh", StreamIndex: 2}}
	if sameSubtitleStreams(s1, nil) || sameSubtitleStreams(s1, s2) || !sameSubtitleStreams(s1, s1) {
		t.Fatal("unexpected sameSubtitleStreams comparison result")
	}

	tmp := t.TempDir()
	cfg, err := config.Load(filepath.Join(tmp, "data"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	repo := sqlite.NewLibraryRepo(store)
	reg := plugins.NewRegistry()
	reg.Register(nfo.New())
	reg.Register(&zhMockOnlineProvider{
		id: "ambra.local.music",
		fetchFn: func(_ context.Context, q plugins.MediaQuery) (*plugins.MetadataResult, error) {
			if strings.EqualFold(q.ItemType, "MusicArtist") {
				return &plugins.MetadataResult{Name: "Jay Chou NFO", Overview: "Bio"}, nil
			}
			if strings.EqualFold(q.ItemType, "MusicAlbum") {
				return &plugins.MetadataResult{Name: "Fantasy NFO", Year: 2001}, nil
			}
			return nil, nil
		},
	})
	sc := New(repo, reg, eventbus.New(), cfg)
	sc.SetProber(NewDisabledProber())
	ctx := context.Background()

	// Custom metadata image path detection.
	if sc.isCustomMetadataImagePath("") || sc.isCustomMetadataImagePath(filepath.Join(tmp, "missing.jpg")) {
		t.Fatal("expected empty/missing path not custom image")
	}
	customImgPath := filepath.Join(sc.imageStorageDir(), "item-x", "primary.jpg")
	_ = os.MkdirAll(filepath.Dir(customImgPath), 0o755)
	_ = os.WriteFile(customImgPath, []byte("custom-jpeg-bytes"), 0o644)
	if !sc.isCustomMetadataImagePath(customImgPath) {
		t.Fatalf("expected %s to be recognized as custom metadata image", customImgPath)
	}
	altCustom := filepath.Join(tmp, "metadata", "custom", "poster.jpg")
	_ = os.MkdirAll(filepath.Dir(altCustom), 0o755)
	_ = os.WriteFile(altCustom, []byte("custom-alt"), 0o644)
	if !sc.isCustomMetadataImagePath(altCustom) {
		t.Fatal("expected /metadata/custom/ path to be recognized as custom image")
	}

	// Create Movie, TV (nested Season + flat Series), and Music libraries.
	moviesRoot := filepath.Join(tmp, "movies")
	movieDir := filepath.Join(moviesRoot, "Hero (2002)")
	_ = os.MkdirAll(movieDir, 0o755)
	movieFile := filepath.Join(movieDir, "hero.mkv")
	_ = os.WriteFile(movieFile, []byte("video-hero"), 0o644)

	tvRoot := filepath.Join(tmp, "tv")
	seriesDir := filepath.Join(tvRoot, "ShowA (2024)")
	seasonDir := filepath.Join(seriesDir, "Season 01")
	flatSeriesDir := filepath.Join(tvRoot, "FlatShow")
	_ = os.MkdirAll(seasonDir, 0o755)
	_ = os.MkdirAll(flatSeriesDir, 0o755)
	epFile := filepath.Join(seasonDir, "ShowA.S01E01.mkv")
	flatEpFile := filepath.Join(flatSeriesDir, "FlatShow.S01E01.mkv")
	_ = os.WriteFile(epFile, []byte("ep-1"), 0o644)
	_ = os.WriteFile(flatEpFile, []byte("ep-flat"), 0o644)

	musicRoot := filepath.Join(tmp, "music")
	artistDir := filepath.Join(musicRoot, "Jay Chou")
	albumDir := filepath.Join(artistDir, "Fantasy (2001)")
	_ = os.MkdirAll(albumDir, 0o755)
	trackFile := filepath.Join(albumDir, "01 - Simple Love.flac")
	_ = os.WriteFile(trackFile, []byte("flac-bytes"), 0o644)

	movieLib, _ := repo.CreateLibrary(ctx, "Movies", "movies", []string{moviesRoot})
	tvLib, _ := repo.CreateLibrary(ctx, "TV", "tvshows", []string{tvRoot})
	musicLib, _ := repo.CreateLibrary(ctx, "Music", "music", []string{musicRoot})

	// Exercise processDiscoveredFile directly for Movie, Episode, and Audio!
	stats := ScanStats{}
	seriesCache := make(map[string]*sqlite.ItemRecord)
	seasonCache := make(map[string]*sqlite.ItemRecord)
	artistCache := make(map[string]*sqlite.ItemRecord)
	albumCache := make(map[string]*sqlite.ItemRecord)
	seenEnrich := make(map[string]struct{})
	seenExtras := make(map[string]struct{})

	if _, err := sc.processDiscoveredFile(ctx, movieLib, moviesRoot, movieFile, false, seriesCache, seasonCache, artistCache, albumCache, seenEnrich, seenExtras, nil, &stats); err != nil {
		t.Fatalf("processDiscoveredFile movie: %v", err)
	}
	if _, err := sc.processDiscoveredFile(ctx, tvLib, tvRoot, epFile, true, seriesCache, seasonCache, artistCache, albumCache, seenEnrich, seenExtras, nil, &stats); err != nil {
		t.Fatalf("processDiscoveredFile episode: %v", err)
	}
	if _, err := sc.processDiscoveredFile(ctx, tvLib, tvRoot, flatEpFile, true, seriesCache, seasonCache, artistCache, albumCache, seenEnrich, seenExtras, nil, &stats); err != nil {
		t.Fatalf("processDiscoveredFile flat episode: %v", err)
	}
	if _, err := sc.processDiscoveredFile(ctx, musicLib, musicRoot, trackFile, false, seriesCache, seasonCache, artistCache, albumCache, seenEnrich, seenExtras, nil, &stats); err != nil {
		t.Fatalf("processDiscoveredFile audio: %v", err)
	}

	movieRec, err := repo.FindItemByMediaSourcePath(ctx, movieLib.ID, movieFile)
	if err != nil || movieRec == nil {
		t.Fatalf("FindItemByMediaSourcePath movie: %v", err)
	}
	audioRec, err := repo.FindItemByMediaSourcePath(ctx, musicLib.ID, trackFile)
	if err != nil || audioRec == nil {
		t.Fatalf("FindItemByMediaSourcePath audio: %v", err)
	}

	// Audio/folder skip in rescanItemExternalSubtitles; unchanged movie returns false.
	if changed, err := sc.rescanItemExternalSubtitles(ctx, musicLib, audioRec); err != nil || changed {
		t.Fatalf("expected audio rescanItemExternalSubtitles=(false,nil), got %v %v", changed, err)
	}
	if changed, err := sc.rescanItemExternalSubtitles(ctx, movieLib, movieRec); err != nil || changed {
		t.Fatalf("expected unchanged movie rescanItemExternalSubtitles=(false,nil), got %v %v", changed, err)
	}

	// Add external subtitle -> ScanPath updates streams; delete subtitle -> ScanPath removes external stream!
	subPath := filepath.Join(movieDir, "hero.eng.srt")
	_ = os.WriteFile(subPath, []byte("1\n00:00:01,000 --> 00:00:02,000\nHi\n"), 0o644)
	subStats, err := sc.ScanPath(ctx, movieLib.ID, subPath)
	if err != nil || subStats.ItemsUpdated != 1 {
		t.Fatalf("ScanPath add subtitle expected ItemsUpdated=1, got %+v err=%v", subStats, err)
	}
	_ = os.Remove(subPath)
	delSubStats, err := sc.ScanPath(ctx, movieLib.ID, subPath)
	if err != nil || delSubStats.ItemsUpdated != 1 {
		t.Fatalf("ScanPath delete subtitle expected ItemsUpdated=1, got %+v err=%v", delSubStats, err)
	}

	// Custom image preservation vs forceOverwriteCustom in upsertLocalImagesWithOption.
	_ = repo.UpsertImage(ctx, &sqlite.ImageRecord{
		ItemID: movieRec.ID, ImageType: "Primary", ImageIndex: 0, Path: customImgPath, Tag: "cust1",
	})
	localPoster := filepath.Join(movieDir, "poster.jpg")
	_ = os.WriteFile(localPoster, []byte("local-poster-bytes"), 0o644)
	if changed, err := sc.rescanItemLocalImages(ctx, movieLib, movieRec, false); err != nil || changed {
		t.Fatalf("expected custom image preserved when forceOverwriteCustom=false, got changed=%v err=%v", changed, err)
	}
	if changed, err := sc.rescanItemLocalImages(ctx, movieLib, movieRec, true); err != nil || !changed {
		t.Fatalf("expected custom image overwritten when forceOverwriteCustom=true, got changed=%v err=%v", changed, err)
	}

	// Companion local artwork discovery for Series, Season (nested + flat #Season01), Episode, MusicArtist, MusicAlbum!
	seriesPoster := filepath.Join(seriesDir, "poster.jpg")
	seasonPoster := filepath.Join(seriesDir, "season01-poster.jpg")
	epThumb := filepath.Join(seasonDir, "ShowA.S01E01-thumb.jpg")
	flatSeasonPoster := filepath.Join(flatSeriesDir, "season01-poster.jpg")
	artistPoster := filepath.Join(artistDir, "artist.jpg")
	albumCover := filepath.Join(albumDir, "cover.jpg")
	for _, p := range []string{seriesPoster, seasonPoster, epThumb, flatSeasonPoster, artistPoster, albumCover} {
		_ = os.WriteFile(p, []byte("img-bytes-"+filepath.Base(p)), 0o644)
	}
	for _, target := range []struct {
		lib  *sqlite.LibraryRecord
		path string
	}{
		{tvLib, seriesPoster},
		{tvLib, epThumb},
		{tvLib, flatSeasonPoster},
		{musicLib, artistPoster},
		{musicLib, albumCover},
	} {
		if changed, err := sc.handleCompanionFileChange(ctx, target.lib, target.path, false); err != nil || !changed {
			t.Fatalf("handleCompanionFileChange image %s expected changed=true, got %v err=%v", target.path, changed, err)
		}
	}
	// Deleted image or non-companion .txt returns (false, nil).
	if changed, _ := sc.handleCompanionFileChange(ctx, tvLib, seriesPoster, true); changed {
		t.Fatal("expected deleted image companion change to return false")
	}
	if changed, _ := sc.handleCompanionFileChange(ctx, tvLib, filepath.Join(seriesDir, "notes.txt"), false); changed {
		t.Fatal("expected .txt file not to be treated as companion sidecar")
	}

	// Companion NFO incremental updates across Series, Season, Episode, MusicArtist, MusicAlbum, and Movie (with BoxSet <set>)!
	nfoTargets := []struct {
		lib     *sqlite.LibraryRecord
		path    string
		content string
	}{
		{tvLib, filepath.Join(seriesDir, "tvshow.nfo"), `<tvshow><title>ShowA NFO</title><plot>Series plot</plot></tvshow>`},
		{tvLib, filepath.Join(seasonDir, "season.nfo"), `<season><title>Season One NFO</title><seasonnumber>1</seasonnumber></season>`},
		{tvLib, filepath.Join(seasonDir, "ShowA.S01E01.nfo"), `<episodedetails><title>Pilot NFO</title><season>1</season><episode>1</episode></episodedetails>`},
		{musicLib, filepath.Join(artistDir, "artist.nfo"), `<artist><name>Jay Chou NFO</name><biography>Bio</biography></artist>`},
		{musicLib, filepath.Join(albumDir, "album.nfo"), `<album><title>Fantasy NFO</title><year>2001</year></album>`},
		{movieLib, filepath.Join(movieDir, "movie.nfo"), `<movie><title>英雄</title><set><name>张艺谋武侠系列</name></set></movie>`},
	}
	for _, nt := range nfoTargets {
		_ = os.WriteFile(nt.path, []byte(nt.content), 0o644)
		if changed, err := sc.handleCompanionFileChange(ctx, nt.lib, nt.path, false); err != nil || !changed {
			t.Fatalf("handleCompanionFileChange NFO %s expected changed=true, got %v err=%v", nt.path, changed, err)
		}
	}
	updatedMovie, _ := repo.GetItemRecordByID(ctx, movieRec.ID)
	if updatedMovie.Name != "英雄" {
		t.Fatalf("expected movie title updated from movie.nfo to 英雄, got %q", updatedMovie.Name)
	}
	cols, colTotal, err := repo.ListCollections(ctx, sqlite.ItemFilter{})
	if err != nil || colTotal != 1 || cols[0].Name != "张艺谋武侠系列" {
		t.Fatalf("expected auto BoxSet created from movie.nfo <set>, got total=%d cols=%+v err=%v", colTotal, cols, err)
	}
}

func TestRescanLocksAudioRelationsVersionNamesAndLocalSaveV0110(t *testing.T) {
	// 1. preserveExistingItemOnRescan: nil guard, full LockData, and granular LockedFields.
	preserveExistingItemOnRescan(nil, nil)

	yr2020, yr2025 := 2020, 2025
	premOld, premNew := int64(1600000000), int64(1700000000)
	commOld, commNew := 8.8, 6.1
	critOld := 92.0

	fullLockedExisting := &sqlite.ItemRecord{
		Name:            "Locked Title",
		OriginalTitle:   "Locked Orig",
		SortName:        "locked sort",
		ProductionYear:  &yr2020,
		PremiereDate:    &premOld,
		CommunityRating: &commOld,
		CriticRating:    &critOld,
		OfficialRating:  "PG-13",
		DateCreated:     1500000000,
		Extra: sqlite.ItemExtra{
			LockData:         true,
			PreventAutoMerge: true,
			Overview:         "Locked Overview",
			Taglines:         []string{"Locked Tagline"},
			ProviderIDs:      map[string]string{"Tmdb": "500"},
			SeriesName:       "Locked Series",
			SeasonName:       "Locked Season",
			Album:            "Locked Album",
			AlbumID:          "alb-1",
			AlbumArtist:      "Locked AlbumArtist",
			Artists:          []string{"Locked Artist"},
			ExtraType:        "trailer",
			Chapters:         []sqlite.ChapterRecord{{StartPositionTicks: 100, Name: "Intro"}},
		},
	}
	rec1 := &sqlite.ItemRecord{Name: "New Scan", SortName: "new scan", ProductionYear: &yr2025}
	preserveExistingItemOnRescan(fullLockedExisting, rec1)
	if rec1.Name != "Locked Title" || rec1.Extra.SeriesName != "Locked Series" || rec1.Extra.SeasonName != "Locked Season" ||
		rec1.Extra.AlbumID != "alb-1" || rec1.Extra.ExtraType != "trailer" || len(rec1.Extra.Chapters) != 1 {
		t.Fatalf("LockData=true failed to preserve all fields: %+v", rec1)
	}

	granularExisting := &sqlite.ItemRecord{
		Name:            "Pinned Name",
		OriginalTitle:   "Pinned Orig",
		SortName:        "pinned sort",
		ProductionYear:  &yr2020,
		PremiereDate:    &premOld,
		CommunityRating: &commOld,
		CriticRating:    &critOld,
		OfficialRating:  "R",
		Extra: sqlite.ItemExtra{
			LockedFields: []string{
				"Name", "SortName", "OriginalTitle", "Overview", "Taglines",
				"ProductionYear", "PremiereDate", "CommunityRating", "OfficialRating",
				"Album", "AlbumArtist", "Artists",
			},
			Overview:    "Pinned Overview",
			Taglines:    []string{"Pinned Tagline"},
			Album:       "Pinned Album",
			AlbumArtist: "Pinned AlbumArtist",
			Artists:     []string{"Pinned Artist"},
			ProviderIDs: map[string]string{"Imdb": "tt9999999"},
		},
	}
	rec2 := &sqlite.ItemRecord{
		Name:            "Overwritten",
		OriginalTitle:   "Overwritten Orig",
		SortName:        "overwritten",
		ProductionYear:  &yr2025,
		PremiereDate:    &premNew,
		CommunityRating: &commNew,
		OfficialRating:  "G",
		Extra: sqlite.ItemExtra{
			Overview:    "New Overview",
			Taglines:    []string{"New Tagline"},
			Album:       "New Album",
			AlbumArtist: "New AA",
			Artists:     []string{"New Artist"},
		},
	}
	preserveExistingItemOnRescan(granularExisting, rec2)
	if rec2.Name != "Pinned Name" || *rec2.ProductionYear != 2020 || rec2.OfficialRating != "R" ||
		rec2.Extra.Album != "Pinned Album" || rec2.Extra.ProviderIDs["Imdb"] != "tt9999999" || *rec2.CriticRating != 92.0 {
		t.Fatalf("granular LockedFields failed to preserve fields: %+v", rec2)
	}

	// 2. buildVersionDisplayName matrix (with probeRes and filename token fallbacks).
	if got := buildVersionDisplayName("/m/Film.mkv", "mkv", false, &ProbeResult{
		Streams: []ProbedStream{{StreamType: "Video", Height: 2160, VideoRange: "DOVI"}},
	}); got != "2160p DolbyVision · MKV" {
		t.Fatalf("unexpected 2160p DOVI label: %q", got)
	}
	if got := buildVersionDisplayName("/m/Film.mp4", "mp4", false, &ProbeResult{
		Streams: []ProbedStream{{StreamType: "Video", Height: 720, VideoRange: "HDR10"}},
	}); got != "720p HDR10 · MP4" {
		t.Fatalf("unexpected 720p HDR10 label: %q", got)
	}
	if got := buildVersionDisplayName("/m/Film.avi", "avi", false, &ProbeResult{
		Streams: []ProbedStream{{StreamType: "Video", Height: 480, VideoRange: "SDR"}},
	}); got != "480p · AVI" {
		t.Fatalf("unexpected 480p SDR label: %q", got)
	}
	tokenCases := map[string]struct {
		container string
		isStrm    bool
		want      string
	}{
		"/m/Movie.4K.DV.mkv":               {"mkv", false, "2160p DolbyVision · MKV"},
		"/m/Movie.720p.HDR10+.mp4":         {"mp4", false, "720p HDR10+ · MP4"},
		"/m/Movie.480p.HDR10.mkv":          {"mkv", false, "480p HDR10 · MKV"},
		"/m/Movie.1080p.HDR.strm":          {"strm", true, "1080p HDR · STRM"},
		"/m/Movie - Director's Cut.mkv":    {"mkv", false, "Director's Cut · MKV"},
		"/m/PlainTitle.mp4":                {"mp4", false, "PlainTitle · MP4"},
	}
	for path, tc := range tokenCases {
		if got := buildVersionDisplayName(path, tc.container, tc.isStrm, nil); got != tc.want {
			t.Fatalf("buildVersionDisplayName(%q) = %q, want %q", path, got, tc.want)
		}
	}

	// 3. applyAudioProbeTags, applyAudioProbeRelations, SaveItemLocalImage/NFO, and RefreshItemWithOptions for Season/Episode.
	tmp := t.TempDir()
	cfg, _ := config.Load(filepath.Join(tmp, "data"))
	store, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repo := sqlite.NewLibraryRepo(store)
	reg := plugins.NewRegistry()
	reg.Register(nfo.New())

	var lastEpisodeQuery plugins.MediaQuery
	reg.Register(&zhMockOnlineProvider{
		id: "ambra.builtin.tvdb",
		fetchFn: func(_ context.Context, q plugins.MediaQuery) (*plugins.MetadataResult, error) {
			if strings.EqualFold(q.ItemType, "Episode") {
				lastEpisodeQuery = q
				lastEpisodeQuery.ProviderIDs = cloneStringMap(q.ProviderIDs)
				return &plugins.MetadataResult{
					Name:            "Online Episode Title",
					OriginalTitle:   "Online Orig",
					Overview:        "Online Ep Plot",
					Tagline:         "Ep Tagline",
					Year:            2024,
					PremiereDate:    "2024-03-15",
					CommunityRating: 9.1,
					OfficialRating:  "TV-14",
					ProviderIDs:     map[string]string{"Tvdb": "ep-999"},
					Genres:          []string{"Sci-Fi", "sci-fi"},
					Studios:         []string{"Ambra Studio"},
					Tags:            []string{"featured"},
					People: []plugins.PersonCredit{
						{Name: "Dir One", Type: "Director"},
						{Name: "Writer One", Type: "Writer"},
						{Name: "Actor One", Role: "Hero", Type: "Actor"},
					},
				}, nil
			}
			if strings.EqualFold(q.ItemType, "Season") {
				return &plugins.MetadataResult{Name: "Online Season 1", Overview: "Season Plot"}, nil
			}
			if strings.EqualFold(q.ItemType, "Movie") {
				return &plugins.MetadataResult{Name: "Online Movie", Overview: "Online Movie Plot", ProviderIDs: map[string]string{"Tmdb": "555"}}, nil
			}
			return nil, nil
		},
	})

	sc := New(repo, reg, eventbus.New(), cfg)
	ctx := context.Background()

	audioRec := &sqlite.ItemRecord{
		ID:        guid.New(),
		LibraryID: "lib-audio",
		Type:      "Audio",
		MediaType: "Audio",
		Name:      "raw",
		SortName:  "raw",
	}
	if err := repo.UpsertItem(ctx, audioRec); err != nil {
		t.Fatal(err)
	}
	probeTags := map[string]string{
		"title":        "Tagged Song",
		"album":        "Tagged Album",
		"artist":       "Artist One; Artist Two / Artist Three",
		"album_artist": "Main Artist",
		"date":         "2021-08-09",
		"track":        "2/10",
		"disc":         "1/2",
		"genre":        "Rock; Synthpop, Electronic / Indie",
	}
	if !applyAudioProbeTags(audioRec, probeTags) {
		t.Fatal("expected applyAudioProbeTags to report changes")
	}
	if !sc.applyAudioProbeRelations(ctx, audioRec, probeTags) {
		t.Fatal("expected applyAudioProbeRelations to persist genres and artist people")
	}
	genres, _, _, _ := repo.GetTaxonomiesByItemID(ctx, audioRec.ID)
	people, _ := repo.GetPeopleByItemID(ctx, audioRec.ID)
	if len(genres) != 4 || len(people) != 3 {
		t.Fatalf("unexpected audio relations: genres=%v people=%+v", genres, people)
	}
	audioRec.Extra.LockedFields = []string{"Genres", "Artists"}
	if sc.applyAudioProbeRelations(ctx, audioRec, probeTags) {
		t.Fatal("expected locked Genres/Artists to skip applyAudioProbeRelations")
	}

	// Shared flat movie root (2 movies in root -> prefixedArtworkFilename) + TV Series/Season/Episode local save & refresh!
	flatMoviesDir := filepath.Join(tmp, "flatmovies")
	_ = os.MkdirAll(flatMoviesDir, 0o755)
	mA := filepath.Join(flatMoviesDir, "Alpha (2020) [tmdbid=555].mkv")
	mB := filepath.Join(flatMoviesDir, "Beta (2021).mkv")
	_ = os.WriteFile(mA, []byte("a"), 0o644)
	_ = os.WriteFile(mB, []byte("b"), 0o644)
	_ = os.WriteFile(filepath.Join(flatMoviesDir, "Alpha (2020) [tmdbid=555].nfo"), []byte(`<movie><title>Alpha Local</title></movie>`), 0o644)
	flatLib, _ := repo.CreateLibrary(ctx, "FlatMovies", "movies", []string{flatMoviesDir})
	mARec := &sqlite.ItemRecord{ID: guid.New(), LibraryID: flatLib.ID, Type: "Movie", Name: "Alpha", SortName: "alpha", Path: mA}
	_ = repo.UpsertItem(ctx, mARec)

	savedImg, ok, err := sc.SaveItemLocalImage(ctx, mARec.ID, "Logo", ".png", []byte("png-logo"))
	if err != nil || !ok || filepath.Base(savedImg) != "Alpha (2020) [tmdbid=555]-logo.png" {
		t.Fatalf("expected prefixed logo filename in shared movie root, got path=%q ok=%v err=%v", savedImg, ok, err)
	}
	// Refresh movie with ReplaceMetadata=false merges local NFO + online metadata and reparses [tmdbid=555]!
	if err := sc.RefreshItem(ctx, mARec.ID, false, false); err != nil {
		t.Fatalf("RefreshItem movie (replace=false): %v", err)
	}

	// Disable SaveLocalMetadata on flatLib -> SaveItemLocalImage returns ("", false, nil).
	_, _ = repo.UpdateLibraryOptions(ctx, flatLib.ID, map[string]any{"SaveLocalMetadata": false})
	if p, saved, err := sc.SaveItemLocalImage(ctx, mARec.ID, "Primary", ".jpg", []byte("jpg")); err != nil || saved || p != "" {
		t.Fatalf("expected SaveLocalMetadata=false to skip local image save, got %q %v %v", p, saved, err)
	}

	// Series -> Season -> Episode RefreshItemWithOptions (both ReplaceMetadata=false and true) + SaveItemLocalNFO.
	tvRoot := filepath.Join(tmp, "tvrefresh")
	sDir := filepath.Join(tvRoot, "Expanse")
	snDir := filepath.Join(sDir, "Season 01")
	_ = os.MkdirAll(snDir, 0o755)
	epPath := filepath.Join(snDir, "Expanse.S01E02.mkv")
	_ = os.WriteFile(epPath, []byte("ep"), 0o644)
	tvLib, _ := repo.CreateLibrary(ctx, "TVRefresh", "tvshows", []string{tvRoot})

	sRec := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: tvLib.ID, Type: "Series", IsFolder: true, Name: "The Expanse", OriginalTitle: "Expanse Orig", SortName: "the expanse", Path: sDir,
		Extra: sqlite.ItemExtra{ProviderIDs: map[string]string{"Tvdb": "280619", "Tmdb": "63639", "Imdb": "tt3230854"}},
	}
	sNum, eNum := 1, 2
	snRec := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: tvLib.ID, ParentID: sRec.ID, SeriesID: sRec.ID, Type: "Season", IsFolder: true, Name: "Season 1", SortName: "0001", IndexNumber: &sNum, Path: snDir,
	}
	epRec := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: tvLib.ID, ParentID: snRec.ID, SeriesID: sRec.ID, SeasonID: snRec.ID, Type: "Episode", MediaType: "Video",
		Name: "S01E02", SortName: "001-0002", IndexNumber: &eNum, ParentIndexNumber: &sNum, Path: epPath,
		Extra: sqlite.ItemExtra{ProviderIDs: map[string]string{"Tvdb": "wrong-child-id"}},
	}
	for _, r := range []*sqlite.ItemRecord{sRec, snRec, epRec} {
		if err := repo.UpsertItem(ctx, r); err != nil {
			t.Fatal(err)
		}
	}

	if err := sc.RefreshItem(ctx, snRec.ID, false, false); err != nil {
		t.Fatalf("RefreshItem Season (replace=false): %v", err)
	}
	if err := sc.RefreshItem(ctx, epRec.ID, true, false); err != nil {
		t.Fatalf("RefreshItem Episode (replace=true): %v", err)
	}
	if lastEpisodeQuery.ProviderIDs["Tvdb"] != "280619" || lastEpisodeQuery.ProviderIDs["tmdb"] != "63639" {
		t.Fatalf("expected parent Series ProviderIDs propagated to Episode query, got %+v", lastEpisodeQuery.ProviderIDs)
	}
	if err := sc.SaveItemLocalNFO(ctx, epRec.ID, true); err != nil {
		t.Fatalf("SaveItemLocalNFO episode: %v", err)
	}
	if _, err := os.Stat(filepath.Join(snDir, "Expanse.S01E02.nfo")); err != nil {
		t.Fatalf("expected episode NFO written alongside media file: %v", err)
	}
}

func TestIncrementalBackfillMultiVersionMergeAndSymlinkEdgesV0110(t *testing.T) {
	// 1. Symlink & root containment helpers.
	if isPathWithinRoot("", "/a") || isPathWithinRoot("/a", "") {
		t.Fatal("expected empty root/target to return false")
	}
	if isPathWithinRoot("/media/movies", "/media/tv/show.mkv") {
		t.Fatal("expected sibling dir outside root to return false")
	}

	tmp := t.TempDir()
	cfg, _ := config.Load(filepath.Join(tmp, "data"))
	store, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repo := sqlite.NewLibraryRepo(store)
	reg := plugins.NewRegistry()
	reg.Register(nfo.New())
	reg.Register(&zhMockOnlineProvider{
		id: "ambra.builtin.tmdb",
		fetchFn: func(_ context.Context, q plugins.MediaQuery) (*plugins.MetadataResult, error) {
			if strings.EqualFold(q.ItemType, "Movie") && strings.Contains(strings.ToLower(q.Title), "dupmerge") {
				return &plugins.MetadataResult{
					Name:        "DupMerge Unified",
					ProviderIDs: map[string]string{"Tmdb": "77777", "Imdb": "tt77777"},
				}, nil
			}
			return &plugins.MetadataResult{
				Name:        q.Title + " Enriched",
				Overview:    "Enriched overview",
				ProviderIDs: map[string]string{"Tmdb": "99999"},
			}, nil
		},
	})
	sc := New(repo, reg, eventbus.New(), cfg)
	sc.SetProber(NewDisabledProber())
	ctx := context.Background()

	// 2. TV library initial scan + second episode scan (hits ensureSeries/ensureSeason/indexEpisode online backfill when missing primary image!).
	tvRoot := filepath.Join(tmp, "tv_backfill")
	showDir := filepath.Join(tvRoot, "Andor (2022)")
	s1Dir := filepath.Join(showDir, "Season 01")
	_ = os.MkdirAll(s1Dir, 0o755)
	ep1 := filepath.Join(s1Dir, "Andor.S01E01.mkv")
	ep2 := filepath.Join(s1Dir, "Andor.S01E02.mkv")
	_ = os.WriteFile(ep1, []byte("ep1"), 0o644)
	tvLib, _ := repo.CreateLibrary(ctx, "TVBackfill", "tvshows", []string{tvRoot})

	if _, err := sc.ScanLibrary(ctx, tvLib.ID); err != nil {
		t.Fatalf("ScanLibrary tv initial: %v", err)
	}
	// Rescan unchanged library while Series/Season/Episode still have no Primary image -> exercises backfill task generation!
	if _, err := sc.ScanLibrary(ctx, tvLib.ID); err != nil {
		t.Fatalf("ScanLibrary tv backfill rescan: %v", err)
	}
	// Add ep2 and ScanPath on ep2 so ensureSeries & ensureSeason find existing DB records without cache!
	_ = os.WriteFile(ep2, []byte("ep2"), 0o644)
	if _, err := sc.ScanPath(ctx, tvLib.ID, ep2); err != nil {
		t.Fatalf("ScanPath ep2: %v", err)
	}

	// 3. Music flat root track (no artist/album) + artist-only track + artwork backfill on second track!
	musicRoot := filepath.Join(tmp, "music_edges")
	artistOnlyDir := filepath.Join(musicRoot, "SoloArtist")
	_ = os.MkdirAll(artistOnlyDir, 0o755)
	flatTrack := filepath.Join(musicRoot, "LooseSong.mp3")
	artistTrack1 := filepath.Join(artistOnlyDir, "SongOne.mp3")
	artistTrack2 := filepath.Join(artistOnlyDir, "SongTwo.mp3")
	_ = os.WriteFile(flatTrack, []byte("mp3-1"), 0o644)
	_ = os.WriteFile(artistTrack1, []byte("mp3-2"), 0o644)
	musicLib, _ := repo.CreateLibrary(ctx, "MusicEdges", "music", []string{musicRoot})
	if _, err := sc.ScanLibrary(ctx, musicLib.ID); err != nil {
		t.Fatalf("ScanLibrary music: %v", err)
	}
	// Add artist.jpg and second track, then ScanPath to hit ensureMusicArtist missing-primary-image backfill!
	_ = os.WriteFile(filepath.Join(artistOnlyDir, "artist.jpg"), []byte("jpg"), 0o644)
	_ = os.WriteFile(artistTrack2, []byte("mp3-3"), 0o644)
	if _, err := sc.ScanPath(ctx, musicLib.ID, artistTrack2); err != nil {
		t.Fatalf("ScanPath artistTrack2: %v", err)
	}

	// 4. Multi-version movie auto-merge by scraped Tmdb ID across separate directories + SymlinkIgnore ScanPath!
	movRoot := filepath.Join(tmp, "mov_merge")
	d1 := filepath.Join(movRoot, "FolderA")
	d2 := filepath.Join(movRoot, "FolderB")
	_ = os.MkdirAll(d1, 0o755)
	_ = os.MkdirAll(d2, 0o755)
	f1 := filepath.Join(d1, "DupMerge.1080p.mkv")
	f2 := filepath.Join(d2, "DupMerge.2160p.mkv")
	_ = os.WriteFile(f1, []byte("v1"), 0o644)
	_ = os.WriteFile(f2, []byte("v2"), 0o644)
	// Also place two Imdb-tagged movies in a batch to exercise flushIfMovieCollides on Imdb ID!
	d3 := filepath.Join(movRoot, "ImdbPair")
	_ = os.MkdirAll(d3, 0o755)
	_ = os.WriteFile(filepath.Join(d3, "FilmOne [imdbid=tt1234567] - 1080p.mkv"), []byte("i1"), 0o644)
	_ = os.WriteFile(filepath.Join(d3, "FilmOne [imdbid=tt1234567] - 4K.mkv"), []byte("i2"), 0o644)

	movLib, _ := repo.CreateLibrary(ctx, "MovMerge", "movies", []string{movRoot})
	if _, err := sc.ScanLibrary(ctx, movLib.ID); err != nil {
		t.Fatalf("ScanLibrary mov_merge: %v", err)
	}

	symFile := filepath.Join(movRoot, "sym.mkv")
	_ = os.Symlink(f1, symFile)
	sc.SetSymlinkPolicy(SymlinkIgnore)
	if st, err := sc.ScanPath(ctx, movLib.ID, symFile); err != nil || st.ItemsAdded != 0 {
		t.Fatalf("expected SymlinkIgnore ScanPath to ignore symlink, got %+v err=%v", st, err)
	}

	// 5. Pause/Resume/Cancel controls + season/local/music artwork & subtitle suffix helpers for extra safety margin.
	sc.Pause()
	sc.Pause() // idempotent
	if !sc.IsPaused() {
		t.Fatal("expected IsPaused=true")
	}
	sc.Resume()
	sc.Resume() // idempotent
	sc.CancelLibrary(movLib.ID)
	sc.CancelAll()

	var completeCalled bool
	sc.SetOnScanComplete(func(_ context.Context, _ string, _ ScanStats) {
		completeCalled = true
	})
	sc.SetSymlinkPolicy(SymlinkFollowDirect)
	_ = os.WriteFile(filepath.Join(d1, "ExtraAdded.mkv"), []byte("new"), 0o644)
	if _, err := sc.ScanLibraryProgressive(ctx, movLib.ID); err != nil {
		t.Fatalf("ScanLibraryProgressive: %v", err)
	}
	sc.WaitIdle(ctx)
	if !completeCalled {
		t.Fatal("expected OnScanComplete callback to fire")
	}

	artDir := filepath.Join(tmp, "art_helpers")
	subSeasonDir := filepath.Join(artDir, "Season 00")
	_ = os.MkdirAll(subSeasonDir, 0o755)
	for _, fn := range []string{
		"season-specials-poster.jpg", "season-specials-fanart.jpg",
		"season-specials-landscape.jpg", "season-specials-banner.jpg",
		"clearlogo.png", "landscape.jpg", "Movie 2.srt", "Movie.chs.srt",
	} {
		_ = os.WriteFile(filepath.Join(artDir, fn), []byte("data"), 0o644)
	}
	_ = os.WriteFile(filepath.Join(subSeasonDir, "poster.jpg"), []byte("sp"), 0o644)
	if imgs := discoverSeasonImages("s0", 0, subSeasonDir, artDir); len(imgs) < 3 {
		t.Fatalf("expected specials season artwork discovered, got %+v", imgs)
	}
	if imgs := discoverLocalImages("m0", filepath.Join(artDir, "Movie.mkv"), artDir, false); len(imgs) < 2 {
		t.Fatalf("expected clearlogo/landscape discovered, got %+v", imgs)
	}
	if imgs := discoverMusicDirImages("mu0", artDir, []string{"clearlogo"}); len(imgs) == 0 {
		t.Fatal("expected discoverMusicDirImages to find clearlogo")
	}
	if subs := discoverExternalSubtitles(filepath.Join(artDir, "Movie.mkv"), "ms0", "m0", 1); len(subs) != 1 {
		t.Fatalf("expected sequel 'Movie 2.srt' skipped and 'Movie.chs.srt' matched, got %+v", subs)
	}
}
