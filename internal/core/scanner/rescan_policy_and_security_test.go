package scanner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestAuditV0107_H6_ProbeWhitelistSSRFAndOutputCap(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script mocks require unix")
	}
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "ffprobe.log")

	fakeProbe := filepath.Join(tmp, "fake-ffprobe.sh")
	script := fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
printf '{"format":{"format_name":"matroska","duration":"120.0"},"streams":[{"index":0,"codec_type":"video","codec_name":"h264","width":1920,"height":1080}]}'
`, logPath)
	if err := os.WriteFile(fakeProbe, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	prober := NewProber(fakeProbe)

	// 1. SSRF private/metadata URL is rejected
	if _, err := prober.Probe(context.Background(), "http://169.254.169.254/latest/meta-data"); err == nil {
		t.Fatal("expected SSRF metadata URL to be rejected")
	}
	// 2. Control characters rejected
	if _, err := prober.Probe(context.Background(), "/tmp/bad\nfile.mkv"); err == nil {
		t.Fatal("expected control character path to be rejected")
	}

	// 3. Local path resolves to Abs and passes -protocol_whitelist file,pipe
	localFile := filepath.Join(tmp, "movie.mkv")
	_ = os.WriteFile(localFile, []byte("x"), 0o600)
	if _, err := prober.Probe(context.Background(), localFile); err != nil {
		t.Fatalf("local Probe: %v", err)
	}

	// 4. Public remote URL passes -protocol_whitelist http,https,tcp,tls,crypto -follow_redirects 0
	if _, err := prober.Probe(context.Background(), "https://93.184.216.34/movie.mkv"); err != nil {
		t.Fatalf("remote Probe: %v", err)
	}

	rawLog, _ := os.ReadFile(logPath)
	logStr := string(rawLog)
	if !strings.Contains(logStr, "-protocol_whitelist file,pipe") {
		t.Fatalf("expected local protocol_whitelist in ffprobe log, got:\n%s", logStr)
	}
	if !strings.Contains(logStr, "-protocol_whitelist http,https,tcp,tls,crypto -follow_redirects 0") {
		t.Fatalf("expected strict remote whitelist and -follow_redirects 0, got:\n%s", logStr)
	}

	// 5. Limited probe buffer truncates cleanly
	lb := &limitedProbeBuffer{max: 8}
	_, _ = lb.Write([]byte("1234"))
	_, _ = lb.Write([]byte("567890"))
	_, _ = lb.Write([]byte("extra"))
	if !lb.truncated || lb.buf.String() != "12345678" {
		t.Fatalf("unexpected limitedProbeBuffer: truncated=%v data=%q", lb.truncated, lb.buf.String())
	}
	lbZero := &limitedProbeBuffer{max: 0}
	_, _ = lbZero.Write([]byte("a"))
	if !lbZero.truncated {
		t.Fatal("expected truncated for zero max")
	}
}

func TestAuditV0107_H7_SymlinkDiscoveryAndScanPathIntermediateDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require privileges on windows")
	}
	tmp := t.TempDir()
	libRoot := filepath.Join(tmp, "library")
	outside := filepath.Join(tmp, "outside")
	movieDir := filepath.Join(libRoot, "MovieA")
	seriesDir := filepath.Join(libRoot, "ShowA")
	seasonDir := filepath.Join(seriesDir, "Season 01")
	musicDir := filepath.Join(libRoot, "Artist", "Album")
	for _, d := range []string{movieDir, seasonDir, musicDir, outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Outside secret files
	outSub := filepath.Join(outside, "secret.srt")
	outImg := filepath.Join(outside, "secret.jpg")
	outVid := filepath.Join(outside, "secret.mkv")
	_ = os.WriteFile(outSub, []byte("secret-sub"), 0o600)
	_ = os.WriteFile(outImg, []byte("secret-img"), 0o600)
	_ = os.WriteFile(outVid, []byte("secret-vid"), 0o600)

	// Safe in-root files + escaping symlinks
	movPath := filepath.Join(movieDir, "MovieA.mkv")
	_ = os.WriteFile(movPath, []byte("vid"), 0o600)
	_ = os.WriteFile(filepath.Join(movieDir, "MovieA.chs.srt"), []byte("ok-sub"), 0o600)
	_ = os.Symlink(outSub, filepath.Join(movieDir, "MovieA.eng.srt"))
	_ = os.WriteFile(filepath.Join(movieDir, "poster.jpg"), []byte("ok-img"), 0o600)
	_ = os.Symlink(outImg, filepath.Join(movieDir, "fanart.jpg"))

	subs := discoverExternalSubtitlesWithPolicy(libRoot, movPath, "ms1", "it1", 2, SymlinkFollowDirect)
	if len(subs) != 1 || filepath.Base(subs[0].Path) != "MovieA.chs.srt" {
		t.Fatalf("expected escaping subtitle symlink filtered out, got %+v", subs)
	}
	imgs := discoverLocalImagesWithPolicy(libRoot, "it1", movPath, movieDir, false, SymlinkFollowDirect)
	if len(imgs) != 1 || imgs[0].ImageType != "Primary" {
		t.Fatalf("expected escaping image symlink filtered out, got %+v", imgs)
	}

	// Season images symlink check
	_ = os.WriteFile(filepath.Join(seriesDir, "season01-poster.jpg"), []byte("ok-s1"), 0o600)
	_ = os.Symlink(outImg, filepath.Join(seriesDir, "season01-fanart.jpg"))
	sImgs := discoverSeasonImagesWithPolicy(libRoot, "s1", 1, seasonDir, seriesDir, SymlinkFollowDirect)
	if len(sImgs) != 1 || sImgs[0].ImageType != "Primary" {
		t.Fatalf("expected escaping season image symlink filtered out, got %+v", sImgs)
	}
	// Also cover wrapper discoverSeasonImages & discoverMusicDirImages
	_ = discoverSeasonImages("s1", 1, seasonDir, seriesDir)

	// Music dir images symlink check
	_ = os.WriteFile(filepath.Join(musicDir, "cover.jpg"), []byte("ok-cover"), 0o600)
	_ = os.Symlink(outImg, filepath.Join(musicDir, "backdrop.jpg"))
	mImgs := discoverMusicDirImagesWithPolicy(libRoot, "alb1", musicDir, []string{"cover"}, SymlinkFollowDirect)
	if len(mImgs) != 1 || mImgs[0].ImageType != "Primary" {
		t.Fatalf("expected escaping music image symlink filtered out, got %+v", mImgs)
	}
	_ = discoverMusicDirImages("alb1", musicDir, []string{"cover"})

	// ScanPath: regular file inside a symlinked intermediate directory pointing outside root
	symDir := filepath.Join(libRoot, "escaped_dir")
	if err := os.Symlink(outside, symDir); err != nil {
		t.Fatal(err)
	}
	escapedRegularFile := filepath.Join(symDir, "secret.mkv")

	// Also an intra-root symlinked directory to test SymlinkIgnore rejecting intermediate symlink
	innerSymDir := filepath.Join(libRoot, "inner_link")
	if err := os.Symlink(movieDir, innerSymDir); err != nil {
		t.Fatal(err)
	}
	innerFileViaSymDir := filepath.Join(innerSymDir, "MovieA.mkv")

	store, err := sqlite.Open(filepath.Join(tmp, "scan.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	repo := sqlite.NewLibraryRepo(store)
	ctx := context.Background()
	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{libRoot})
	if err != nil {
		t.Fatal(err)
	}

	sc := New(repo, plugins.NewRegistry(), eventbus.New(), config.Default(tmp))
	sc.SetProber(NewDisabledProber())

	// Under SymlinkFollowDirect, regular file under escaping symlinked dir MUST return error
	if _, err := sc.ScanPath(ctx, lib.ID, escapedRegularFile); err == nil {
		t.Fatal("expected ScanPath to reject regular file inside escaping symlinked dir")
	}

	// Under SymlinkIgnore, even an intra-root symlinked intermediate directory must be skipped (0 added)
	sc.SetSymlinkPolicy(SymlinkIgnore)
	st, err := sc.ScanPath(ctx, lib.ID, innerFileViaSymDir)
	if err != nil || st.ItemsAdded != 0 {
		t.Fatalf("expected SymlinkIgnore to skip file under symlinked intermediate dir, got st=%+v err=%v", st, err)
	}
}

func TestAuditV0107_M9_ParserEnglishGBAndExtrasTrimmedName(t *testing.T) {
	// M-9: en-GB / en_GB must be classified as English, NOT Simplified Chinese!
	for _, fn := range []string{"Movie.en-GB.srt", "Movie.en_GB.ass", "Movie.en-gb.vtt"} {
		cls := ClassifySubtitle(fn, "")
		if cls.Language != "eng" || cls.SubVariant != "" {
			t.Fatalf("ClassifySubtitle(%q) = {Lang:%q Variant:%q}, want {eng, \"\"}", fn, cls.Language, cls.SubVariant)
		}
	}
	// Genuine Chinese markers still work
	if cls := ClassifySubtitle("Movie.sc.ass", ""); cls.Language != "chi" || cls.SubVariant != "chs" {
		t.Fatalf("expected sc.ass -> chi/chs, got %+v", cls)
	}
	if cls := ClassifySubtitle("Movie.gb.srt", ""); cls.Language != "chi" || cls.SubVariant != "chs" {
		t.Fatalf("expected gb.srt -> chi/chs, got %+v", cls)
	}
	if cls := ClassifySubtitle("Movie.tc.ass", ""); cls.Language != "chi" || cls.SubVariant != "cht" {
		t.Fatalf("expected tc.ass -> chi/cht, got %+v", cls)
	}

	// Low: extras.go cleanExtraDisplayName returns trimmed name (not original base with -trailer suffix)
	if got := cleanExtraDisplayName("/movies/Inception-trailer.mp4"); got != "Inception" {
		t.Fatalf("cleanExtraDisplayName got %q, want %q", got, "Inception")
	}
	if got := cleanExtraDisplayName("/movies/BehindTheScenes.mp4"); got != "BehindTheScenes" {
		t.Fatalf("cleanExtraDisplayName non-suffix got %q", got)
	}

	// Exercise isValidSubtitleSuffixBoundary edge cases
	if isValidSubtitleSuffixBoundary("") || isValidSubtitleSuffixBoundary("abc.srt") || isValidSubtitleSuffixBoundary(" 2.srt") {
		t.Fatal("expected invalid subtitle boundaries rejected")
	}
	if !isValidSubtitleSuffixBoundary(" .srt") || !isValidSubtitleSuffixBoundary(" chs.srt") || !isValidSubtitleSuffixBoundary(".srt") {
		t.Fatal("expected valid subtitle boundaries accepted")
	}
}

func TestAuditV0107_M15_RescanPreservesLocksChapterAndRemoteImagesAndAttachedPicLock(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "m15.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	repo := sqlite.NewLibraryRepo(store)
	ctx := context.Background()

	libDir := filepath.Join(tmp, "movies")
	movDir := filepath.Join(libDir, "Dune (2021)")
	_ = os.MkdirAll(movDir, 0o755)
	movPath := filepath.Join(movDir, "Dune (2021).mkv")
	_ = os.WriteFile(movPath, []byte("v1-content"), 0o600)

	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{libDir})
	if err != nil {
		t.Fatal(err)
	}

	cfg := config.Default(tmp)
	sc := New(repo, plugins.NewRegistry(), eventbus.New(), cfg)
	sc.SetProber(NewDisabledProber())

	// Initial scan
	if _, err := sc.ScanLibrary(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}
	rec, err := repo.FindItemByMediaSourcePath(ctx, lib.ID, movPath)
	if err != nil || rec == nil {
		t.Fatalf("FindItemByMediaSourcePath: %v", err)
	}

	// User customizes Name, Overview, sets LockedFields + LockData, plus remote Primary poster & Chapter image
	rec.Name = "Custom Locked Title"
	rec.OriginalTitle = "Custom Original"
	rec.Extra.Overview = "Custom overview"
	rec.Extra.Taglines = []string{"Locked tagline"}
	rec.Extra.LockedFields = []string{"Name", "Overview", "Taglines"}
	rec.Extra.LockData = true
	rec.Extra.ProviderIDs = map[string]string{"Tmdb": "438631"}
	if err := repo.UpsertItem(ctx, rec); err != nil {
		t.Fatal(err)
	}
	remotePoster := filepath.Join(tmp, "remote_poster.jpg")
	chapThumb := filepath.Join(tmp, "chapter_0.jpg")
	_ = os.WriteFile(remotePoster, []byte("poster"), 0o600)
	_ = os.WriteFile(chapThumb, []byte("chapter"), 0o600)
	_ = repo.UpsertImage(ctx, &sqlite.ImageRecord{ItemID: rec.ID, ImageType: "Primary", ImageIndex: 0, Path: remotePoster, Tag: "t1"})
	_ = repo.UpsertImage(ctx, &sqlite.ImageRecord{ItemID: rec.ID, ImageType: "Chapter", ImageIndex: 0, Path: chapThumb, Tag: "c1"})

	// Touch file with new size + mtime to force Phase A rescan
	_ = os.WriteFile(movPath, []byte("v2-updated-file-content-larger"), 0o600)
	future := time.Now().Add(5 * time.Second)
	_ = os.Chtimes(movPath, future, future)

	if _, err := sc.ScanLibrary(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}

	after, err := repo.GetItemRecordByID(ctx, rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !after.Extra.LockData || after.Name != "Custom Locked Title" || after.Extra.Overview != "Custom overview" {
		t.Fatalf("rescan wiped LockData or locked fields: %+v", after)
	}
	if _, err := repo.GetImageByItemAndType(ctx, rec.ID, "Primary", 0); err != nil {
		t.Fatalf("rescan wiped remote Primary poster: %v", err)
	}
	if _, err := repo.GetImageByItemAndType(ctx, rec.ID, "Chapter", 0); err != nil {
		t.Fatalf("rescan wiped Chapter thumbnail: %v", err)
	}

	// Also exercise partial LockedFields (LockData=false)
	y2020 := 2020
	r8 := 8.5
	prem := int64(1600000000)
	exPartial := &sqlite.ItemRecord{
		Name: "Locked Name", SortName: "locked name", OriginalTitle: "Orig",
		ProductionYear: &y2020, PremiereDate: &prem, CommunityRating: &r8, OfficialRating: "PG-13",
		Extra: sqlite.ItemExtra{
			Overview: "Locked Ov", Taglines: []string{"Tag1"},
			LockedFields: []string{"Name", "OriginalTitle", "Overview", "Taglines", "ProductionYear", "PremiereDate", "CommunityRating", "OfficialRating"},
			ProviderIDs:  map[string]string{"tmdb": "999"},
		},
	}
	fresh := &sqlite.ItemRecord{Name: "NewName", Extra: sqlite.ItemExtra{ProviderIDs: map[string]string{"Tmdb": "888"}}}
	preserveExistingItemOnRescan(exPartial, fresh)
	if fresh.Name != "Locked Name" || fresh.Extra.Overview != "Locked Ov" || fresh.OfficialRating != "PG-13" || len(fresh.Extra.ProviderIDs) != 1 {
		t.Fatalf("preserveExistingItemOnRescan partial lock failed: %+v", fresh)
	}

	// M-15b + H-6: extractAudioAttachedPic refuses symlink dest, uses -protocol_whitelist file,pipe, atomic rename, and concurrent dedup lock
	if runtime.GOOS != "windows" {
		ffLog := filepath.Join(tmp, "pic_ff.log")
		fakeFF := filepath.Join(tmp, "pic-ff.sh")
		script := fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
for last; do true; done
printf "FAKEJPG" > "$last"
exit 0
`, ffLog)
		_ = os.WriteFile(fakeFF, []byte(script), 0o755)
		cfg.FFmpegPath = fakeFF
		scPic := New(repo, plugins.NewRegistry(), eventbus.New(), cfg)

		trackPath := filepath.Join(tmp, "song.flac")
		_ = os.WriteFile(trackPath, []byte("flac"), 0o600)
		trackRec := &sqlite.ItemRecord{ID: "track-1", LibraryID: lib.ID, Type: "Audio", MediaType: "Audio", Name: "Song", Path: trackPath}
		_ = repo.UpsertItem(ctx, trackRec)

		// Symlink destination must be rejected
		symDestDir := filepath.Join(scPic.imageStorageDir(), trackRec.ID)
		_ = os.MkdirAll(symDestDir, 0o755)
		outsideTarget := filepath.Join(tmp, "pwned.jpg")
		_ = os.WriteFile(outsideTarget, []byte("orig"), 0o600)
		_ = os.Symlink(outsideTarget, filepath.Join(symDestDir, "primary.jpg"))
		if scPic.extractAudioAttachedPic(ctx, trackRec, trackPath) {
			t.Fatal("extractAudioAttachedPic must refuse symlink destination")
		}
		if raw, _ := os.ReadFile(outsideTarget); string(raw) != "orig" {
			t.Fatal("symlink target was overwritten!")
		}

		// Regular destination succeeds atomically with concurrent callers
		_ = os.Remove(filepath.Join(symDestDir, "primary.jpg"))
		var wg sync.WaitGroup
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = scPic.extractAudioAttachedPic(ctx, trackRec, trackPath)
			}()
		}
		wg.Wait()
		rawFFLog, _ := os.ReadFile(ffLog)
		if !strings.Contains(string(rawFFLog), "-protocol_whitelist file,pipe") {
			t.Fatalf("expected -protocol_whitelist file,pipe in extractAudioAttachedPic, got:\n%s", string(rawFFLog))
		}
	}
}

type captureQueryProvider struct {
	mu      sync.Mutex
	queries []plugins.MediaQuery
}

func (c *captureQueryProvider) Manifest() plugins.Manifest {
	return plugins.Manifest{
		ID:       "test.online.capture",
		Name:     "Capture",
		Enabled:  true,
		Priority: 20,
		Kinds:    []plugins.Kind{plugins.KindMetadata},
	}
}
func (c *captureQueryProvider) Configure(map[string]any) error { return nil }
func (c *captureQueryProvider) FetchMetadata(_ context.Context, q plugins.MediaQuery) (*plugins.MetadataResult, error) {
	c.mu.Lock()
	c.queries = append(c.queries, q)
	c.mu.Unlock()
	return &plugins.MetadataResult{Name: "Episode Real Title"}, nil
}

func TestAuditV0107_M16_PhaseBEnrichReusesParentSeriesTVDBTMDBAndTitle(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "m16.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	repo := sqlite.NewLibraryRepo(store)
	ctx := context.Background()

	series := &sqlite.ItemRecord{
		ID: "ser-1", LibraryID: "lib1", Type: "Series", IsFolder: true,
		Name: "绝命毒师", OriginalTitle: "Breaking Bad",
		Extra: sqlite.ItemExtra{ProviderIDs: map[string]string{"Tmdb": "1396", "Tvdb": "81189", "Imdb": "tt0903747"}},
	}
	ep := &sqlite.ItemRecord{
		ID: "ep-1", LibraryID: "lib1", Type: "Episode", SeriesID: series.ID,
		Name: "Episode 1",
		Extra: sqlite.ItemExtra{ProviderIDs: map[string]string{"tvdb": "9999999", "tmdb": "1396"}},
	}
	_ = repo.UpsertItem(ctx, series)
	_ = repo.UpsertItem(ctx, ep)

	capProv := &captureQueryProvider{}
	reg := plugins.NewRegistry()
	reg.Register(capProv)
	sc := New(repo, reg, eventbus.New(), config.Default(tmp))
	sc.SetProber(NewDisabledProber())

	// Even when Episode query already has tmdb AND a child-level tvdb ID,
	// Phase B enrichItem MUST replace tvdb with parent Series Tvdb (81189) and sync parent titles!
	updated := sc.enrichItem(ctx, enrichTask{
		ItemID:         ep.ID,
		LibraryID:      "lib1",
		ItemType:       "Episode",
		ParentSeriesID: series.ID,
		Query: plugins.MediaQuery{
			ItemType:      "Episode",
			Title:         "Episode 1",
			SeasonNumber:  1,
			EpisodeNumber: 1,
			ProviderIDs:   map[string]string{"tvdb": "9999999", "tmdb": "1396"},
		},
		NeedOnlineMeta: true,
	})
	if !updated {
		t.Fatal("expected enrichItem to succeed")
	}
	capProv.mu.Lock()
	defer capProv.mu.Unlock()
	if len(capProv.queries) != 1 {
		t.Fatalf("expected 1 query, got %d", len(capProv.queries))
	}
	gotQ := capProv.queries[0]
	if gotQ.ProviderIDs["Tvdb"] != "81189" || gotQ.ProviderIDs["tvdb"] != "" {
		t.Fatalf("expected parent Series Tvdb=81189 replacing child tvdb, got %v", gotQ.ProviderIDs)
	}
	if gotQ.ProviderIDs["tmdb"] != "1396" || gotQ.Title != "绝命毒师" || gotQ.OriginalTitle != "Breaking Bad" {
		t.Fatalf("expected parent Series tmdb & titles synced, got %+v", gotQ)
	}
}

type auditStubMetaProvider struct {
	id       string
	priority int
	res      *plugins.MetadataResult
	err      error
	gotQuery *plugins.MediaQuery
}

func (p *auditStubMetaProvider) Manifest() plugins.Manifest {
	return plugins.Manifest{
		ID:       p.id,
		Name:     p.id,
		Enabled:  true,
		Priority: p.priority,
		Kinds:    []plugins.Kind{plugins.KindMetadata},
	}
}
func (p *auditStubMetaProvider) Configure(map[string]any) error { return nil }
func (p *auditStubMetaProvider) FetchMetadata(_ context.Context, q plugins.MediaQuery) (*plugins.MetadataResult, error) {
	cp := q
	p.gotQuery = &cp
	return p.res, p.err
}

func TestAuditV0107_ScannerLocalSaveMergeBatchAndSymlinkCoverage(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "cov.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	repo := sqlite.NewLibraryRepo(store)
	ctx := context.Background()

	// 1. local_save.go helpers & public SaveItemLocalNFO / SaveItemLocalImage
	if got := flatSeasonArtworkFilename(0, "Primary", ".jpg"); got != "season-specials-poster.jpg" {
		t.Fatalf("flatSeasonArtworkFilename(0,Primary) = %q", got)
	}
	if got := flatSeasonArtworkFilename(2, "Backdrop", ".jpg"); got != "season02-fanart.jpg" {
		t.Fatalf("flatSeasonArtworkFilename(2,Backdrop) = %q", got)
	}
	if got := flatSeasonArtworkFilename(1, "Banner", ".jpg"); got != "" {
		t.Fatalf("flatSeasonArtworkFilename(unknown) = %q", got)
	}
	if got := prefixedArtworkFilename("", "Primary", ".jpg"); got != "poster.jpg" {
		t.Fatalf("prefixedArtworkFilename(empty) = %q", got)
	}
	if got := prefixedArtworkFilename("Movie", "Backdrop", ".jpg"); got != "Movie-fanart.jpg" {
		t.Fatalf("prefixedArtworkFilename(Backdrop) = %q", got)
	}
	if got := prefixedArtworkFilename("Movie", "Logo", ".png"); got != "Movie-logo.png" {
		t.Fatalf("prefixedArtworkFilename(Logo) = %q", got)
	}
	if got := prefixedArtworkFilename("Movie", "Other", ".jpg"); got != "" {
		t.Fatalf("prefixedArtworkFilename(Other) = %q", got)
	}

	libDir := filepath.Join(tmp, "movies")
	movDir := filepath.Join(libDir, "Inception (2010)")
	seriesDir := filepath.Join(libDir, "ShowX")
	_ = os.MkdirAll(movDir, 0o755)
	_ = os.MkdirAll(seriesDir, 0o755)
	movPath := filepath.Join(movDir, "Inception (2010).mkv")
	_ = os.WriteFile(movPath, []byte("vid"), 0o600)

	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{libDir})
	if err != nil {
		t.Fatal(err)
	}

	movRec := &sqlite.ItemRecord{
		ID: "mov-ls", LibraryID: lib.ID, Type: "Movie", MediaType: "Video",
		Name: "Inception", Path: movPath,
	}
	seriesRec := &sqlite.ItemRecord{
		ID: "ser-ls", LibraryID: lib.ID, Type: "Series", IsFolder: true,
		Name: "ShowX", Path: seriesDir,
	}
	epRec := &sqlite.ItemRecord{
		ID: "ep-ls", LibraryID: lib.ID, Type: "Episode", SeriesID: seriesRec.ID,
		Name: "Ep1", Path: filepath.Join(seriesDir, "S01E01.mkv"),
	}
	_ = repo.UpsertItem(ctx, movRec)
	_ = repo.UpsertItem(ctx, seriesRec)
	_ = repo.UpsertItem(ctx, epRec)

	sc := New(repo, plugins.NewRegistry(), eventbus.New(), config.Default(tmp))
	if sc.resolveSeriesDir(ctx, epRec) != filepath.Clean(seriesDir) {
		t.Fatalf("resolveSeriesDir got %q", sc.resolveSeriesDir(ctx, epRec))
	}
	if sc.resolveSeriesDir(ctx, nil) != "" {
		t.Fatal("expected empty resolveSeriesDir for nil")
	}
	if sc.resolveSeriesDir(ctx, &sqlite.ItemRecord{ID: "orphan"}) != "" {
		t.Fatal("expected empty resolveSeriesDir without parent")
	}

	// SaveItemLocalNFO & SaveItemLocalImage
	if err := sc.SaveItemLocalNFO(ctx, movRec.ID, true); err != nil {
		t.Fatalf("SaveItemLocalNFO: %v", err)
	}
	if _, err := os.Stat(filepath.Join(movDir, "Inception (2010).nfo")); err != nil {
		t.Fatalf("expected local NFO written: %v", err)
	}
	if err := sc.SaveItemLocalNFO(ctx, "nonexistent-id", true); err == nil {
		t.Fatal("expected error for missing item in SaveItemLocalNFO")
	}
	var nilScanner *Scanner
	if err := nilScanner.SaveItemLocalNFO(ctx, movRec.ID, true); err != nil {
		t.Fatalf("nil scanner SaveItemLocalNFO should return nil, got %v", err)
	}
	if _, ok, err := nilScanner.SaveItemLocalImage(ctx, movRec.ID, "Primary", ".jpg", []byte("x")); err != nil || ok {
		t.Fatalf("nil scanner SaveItemLocalImage: ok=%v err=%v", ok, err)
	}
	savedPath, ok, err := sc.SaveItemLocalImage(ctx, movRec.ID, "Primary", ".jpg", []byte("JPGDATA"))
	if err != nil || !ok || savedPath == "" {
		t.Fatalf("SaveItemLocalImage: path=%q ok=%v err=%v", savedPath, ok, err)
	}
	if _, ok, err := sc.SaveItemLocalImage(ctx, movRec.ID, "Primary", ".jpg", nil); err != nil || ok {
		t.Fatalf("empty data SaveItemLocalImage should skip: ok=%v err=%v", ok, err)
	}
	if _, _, err := sc.SaveItemLocalImage(ctx, "nonexistent-id", "Primary", ".jpg", []byte("x")); err == nil {
		t.Fatal("expected error for missing item in SaveItemLocalImage")
	}

	// 2. fetchMergedMetadata + applyMetadataToItem
	p1 := &auditStubMetaProvider{
		id: "p1", priority: 10,
		res: &plugins.MetadataResult{
			Name:        "Title1",
			ProviderIDs: map[string]string{"Tmdb": "100"},
		},
	}
	p2 := &auditStubMetaProvider{
		id: "p2", priority: 20,
		res: &plugins.MetadataResult{
			OriginalTitle:   "OrigTitle",
			Overview:        "Overview2",
			Tagline:         "Tagline2",
			Year:            2010,
			PremiereDate:    "2010-07-16",
			CommunityRating: 8.8,
			OfficialRating:  "PG-13",
			Genres:          []string{"Sci-Fi"},
			Studios:         []string{"WB"},
			Tags:            []string{"Dream"},
			People:          []plugins.PersonCredit{{Name: "Nolan", Role: "Director", Type: "Director"}},
			ProviderIDs:     map[string]string{"tmdb": "999", "Imdb": "tt1375666"},
			PosterURL:       "https://img/p.jpg",
			BackdropURL:     "https://img/b.jpg",
			LogoURL:         "https://img/l.png",
			Collection:      &plugins.CollectionMeta{ID: "1", Name: "Col1"},
			LockData:        true,
		},
	}
	reg := plugins.NewRegistry()
	reg.Register(p1)
	reg.Register(p2)
	scMerge := New(repo, reg, eventbus.New(), config.Default(tmp))
	merged := scMerge.fetchMergedMetadata(ctx, plugins.MediaQuery{ItemType: "Movie", Title: "Inception"})
	if merged == nil || merged.Name != "Title1" || merged.Overview != "Overview2" || merged.ProviderIDs["Tmdb"] != "100" || merged.ProviderIDs["Imdb"] != "tt1375666" || !merged.LockData {
		t.Fatalf("unexpected merged metadata: %+v", merged)
	}
	if p2.gotQuery == nil || p2.gotQuery.ProviderIDs["Tmdb"] != "100" {
		t.Fatalf("expected p1 ProviderIDs propagated to p2 query, got %+v", p2.gotQuery)
	}
	target := &sqlite.ItemRecord{}
	applyMetadataToItem(target, nil)
	applyMetadataToItem(target, merged)
	if target.Name != "Title1" || target.OriginalTitle != "OrigTitle" || target.Extra.Overview != "Overview2" || target.OfficialRating != "PG-13" || target.PremiereDate == nil || target.CommunityRating == nil || !target.Extra.LockData {
		t.Fatalf("applyMetadataToItem incomplete: %+v", target)
	}

	// 3. phaseABatch collision flush & error paths
	var tasks []enrichTask
	stats := &ScanStats{}
	batch := newPhaseABatch(sc, lib.ID, stats, &tasks)
	_ = batch.addMovie(ctx, pendingPhaseAItem{
		input: sqlite.BatchItemInput{
			Item: &sqlite.ItemRecord{
				ID: "b-m1", LibraryID: lib.ID, Type: "Movie", MediaType: "Video",
				Name: "Matrix", Extra: sqlite.ItemExtra{ProviderIDs: map[string]string{"Tmdb": "603", "Imdb": "tt0133093"}},
			},
		},
		displayFileName: "Matrix.mkv",
	}, movDir, "Matrix")
	if err := batch.flushIfMovieCollides(ctx, movDir, "Matrix", nil); err != nil {
		t.Fatalf("flushIfMovieCollides by title: %v", err)
	}
	// Collide by Tmdb and Imdb
	_ = batch.addMovie(ctx, pendingPhaseAItem{
		input: sqlite.BatchItemInput{
			Item: &sqlite.ItemRecord{
				ID: "b-m2", LibraryID: lib.ID, Type: "Movie", MediaType: "Video",
				Name: "Matrix2", Extra: sqlite.ItemExtra{ProviderIDs: map[string]string{"Tmdb": "604", "Imdb": "tt0234215"}},
			},
		},
		postHook: func(context.Context) error { return fmt.Errorf("posthook err") },
	}, movDir, "Matrix2")
	if err := batch.flushIfMovieCollides(ctx, "/other", "Other", map[string]string{"tmdb": "604"}); err != nil {
		t.Fatalf("flushIfMovieCollides by tmdb: %v", err)
	}
	if stats.Errors == 0 {
		t.Fatal("expected postHook error counted in stats.Errors")
	}
	_ = batch.addEpisode(ctx, pendingPhaseAItem{
		input: sqlite.BatchItemInput{
			Item: &sqlite.ItemRecord{ID: "b-e1", LibraryID: lib.ID, Type: "Episode", SeriesID: seriesRec.ID, Name: "E1"},
		},
	}, seriesRec.ID, 1, 1)
	if err := batch.flushIfEpisodeCollides(ctx, seriesRec.ID, 1, 1); err != nil {
		t.Fatalf("flushIfEpisodeCollides: %v", err)
	}
	// Canceled ctx flush
	_ = batch.addEpisode(ctx, pendingPhaseAItem{
		input: sqlite.BatchItemInput{
			Item: &sqlite.ItemRecord{ID: "b-e2", LibraryID: lib.ID, Type: "Episode", SeriesID: seriesRec.ID, Name: "E2"},
		},
	}, seriesRec.ID, 1, 2)
	cCtx, cancel := context.WithCancel(ctx)
	cancel()
	if err := batch.flush(cCtx); err == nil {
		t.Fatal("expected canceled error on batch.flush")
	}

	// 4. symlink.go & extras.go & parser.go edge cases
	if isPathWithinRoot("", movPath) || isPathWithinRoot(libDir, "") {
		t.Fatal("expected false for empty root/target")
	}
	if allowed, _ := isSymlinkAllowedInRoot(libDir, movPath, SymlinkFollowAll); !allowed {
		t.Fatal("expected SymlinkFollowAll to allow")
	}
	if hasSymlinkComponentUnderRoot("", movPath) {
		t.Fatal("expected false for empty root")
	}
	if !containsASCIIAlpha("abc123") || containsASCIIAlpha("12345") {
		t.Fatal("containsASCIIAlpha mismatch")
	}
	if !matchesSiblingExtraPrefix("Movie-trailer", "Movie") || matchesSiblingExtraPrefix("Other-trailer", "Movie") {
		t.Fatal("matchesSiblingExtraPrefix mismatch")
	}
}

func TestAuditV0107_ScannerAdditionalBranchCoverage(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "cov2.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	repo := sqlite.NewLibraryRepo(store)
	ctx := context.Background()

	rootDir := filepath.Join(tmp, "lib")
	seriesDir := filepath.Join(rootDir, "ShowY")
	seasonDir := filepath.Join(seriesDir, "Season 01")
	artistDir := filepath.Join(rootDir, "ArtistY")
	albumDir := filepath.Join(artistDir, "AlbumY")
	for _, d := range []string{seasonDir, albumDir} {
		_ = os.MkdirAll(d, 0o755)
	}
	// Shared movie root dir (2 videos directly inside rootDir)
	m1Path := filepath.Join(rootDir, "Mov1.mkv")
	m2Path := filepath.Join(rootDir, "Mov2.mkv")
	_ = os.WriteFile(m1Path, []byte("1"), 0o600)
	_ = os.WriteFile(m2Path, []byte("2"), 0o600)

	lib, err := repo.CreateLibrary(ctx, "Mixed", "mixed", []string{rootDir})
	if err != nil {
		t.Fatal(err)
	}
	serRec := &sqlite.ItemRecord{ID: "ser-y", LibraryID: lib.ID, Type: "Series", IsFolder: true, Name: "ShowY", Path: seriesDir}
	_ = repo.UpsertItem(ctx, serRec)
	sc := New(repo, plugins.NewRegistry(), eventbus.New(), config.Default(tmp))

	// 1. resolveLocalImagePath & resolveLocalNFOTarget across all item types
	sNum := 1
	flatSeason := &sqlite.ItemRecord{ID: "s-flat", LibraryID: lib.ID, Type: "Season", SeriesID: serRec.ID, IndexNumber: &sNum, Path: seriesDir}
	if p, ok := sc.resolveLocalImagePath(ctx, lib, flatSeason, "Primary", ".jpg"); !ok || filepath.Base(p) != "season01-poster.jpg" {
		t.Fatalf("flat season poster: %q, %v", p, ok)
	}
	if _, _, ok := sc.resolveLocalNFOTarget(ctx, flatSeason); ok {
		t.Fatal("flat season should skip season.nfo")
	}
	dedSeason := &sqlite.ItemRecord{ID: "s-ded", LibraryID: lib.ID, Type: "Season", SeriesID: serRec.ID, IndexNumber: &sNum, Path: seasonDir}
	if tag, p, ok := sc.resolveLocalNFOTarget(ctx, dedSeason); !ok || tag != "season" || filepath.Base(p) != "season.nfo" {
		t.Fatalf("dedicated season nfo: %q %q %v", tag, p, ok)
	}
	epRec := &sqlite.ItemRecord{ID: "ep-y", LibraryID: lib.ID, Type: "Episode", Path: filepath.Join(seasonDir, "S01E01.mkv")}
	if p, ok := sc.resolveLocalImagePath(ctx, lib, epRec, "Primary", ".jpg"); !ok || filepath.Base(p) != "S01E01-thumb.jpg" {
		t.Fatalf("ep thumb: %q, %v", p, ok)
	}
	if p, ok := sc.resolveLocalImagePath(ctx, lib, epRec, "Backdrop", ".jpg"); !ok || filepath.Base(p) != "S01E01-fanart.jpg" {
		t.Fatalf("ep fanart: %q, %v", p, ok)
	}
	if _, ok := sc.resolveLocalImagePath(ctx, lib, epRec, "Logo", ".png"); ok {
		t.Fatal("ep Logo should return false")
	}
	if tag, _, ok := sc.resolveLocalNFOTarget(ctx, epRec); !ok || tag != "episodedetails" {
		t.Fatalf("ep nfo: %q %v", tag, ok)
	}

	// Shared movie root dir -> prefixed artwork; folder movie -> movie.nfo
	sharedMov := &sqlite.ItemRecord{ID: "m-sh", LibraryID: lib.ID, Type: "Movie", Path: m1Path}
	if p, ok := sc.resolveLocalImagePath(ctx, lib, sharedMov, "Primary", ".jpg"); !ok || filepath.Base(p) != "Mov1-poster.jpg" {
		t.Fatalf("shared root movie poster: %q, %v", p, ok)
	}
	folderMov := &sqlite.ItemRecord{ID: "m-dir", LibraryID: lib.ID, Type: "Movie", IsFolder: true, Path: seriesDir}
	if _, ok := sc.resolveLocalImagePath(ctx, lib, folderMov, "Logo", ".png"); !ok {
		t.Fatal("folder movie logo expected true")
	}
	if tag, p, ok := sc.resolveLocalNFOTarget(ctx, folderMov); !ok || tag != "movie" || filepath.Base(p) != "movie.nfo" {
		t.Fatalf("folder movie nfo: %q %q %v", tag, p, ok)
	}

	// MusicArtist & MusicAlbum & Series & Unknown
	artistRec := &sqlite.ItemRecord{ID: "art-y", LibraryID: lib.ID, Type: "MusicArtist", Path: artistDir}
	albumRec := &sqlite.ItemRecord{ID: "alb-y", LibraryID: lib.ID, Type: "MusicAlbum", Path: albumDir}
	for _, tc := range []struct {
		rec     *sqlite.ItemRecord
		imgType string
		wantOK  bool
	}{
		{serRec, "Primary", true},
		{artistRec, "Primary", true},
		{artistRec, "Backdrop", true},
		{artistRec, "Logo", false},
		{albumRec, "Primary", true},
		{albumRec, "Backdrop", true},
		{albumRec, "Logo", false},
		{&sqlite.ItemRecord{Type: "Unknown", Path: artistDir}, "Primary", false},
	} {
		if _, ok := sc.resolveLocalImagePath(ctx, lib, tc.rec, tc.imgType, ".jpg"); ok != tc.wantOK {
			t.Fatalf("resolveLocalImagePath(%s,%s) = %v, want %v", tc.rec.Type, tc.imgType, ok, tc.wantOK)
		}
	}
	if tag, _, ok := sc.resolveLocalNFOTarget(ctx, serRec); !ok || tag != "tvshow" {
		t.Fatalf("series nfo: %q %v", tag, ok)
	}
	if tag, _, ok := sc.resolveLocalNFOTarget(ctx, artistRec); !ok || tag != "artist" {
		t.Fatalf("artist nfo: %q %v", tag, ok)
	}
	if tag, _, ok := sc.resolveLocalNFOTarget(ctx, albumRec); !ok || tag != "album" {
		t.Fatalf("album nfo: %q %v", tag, ok)
	}
	if _, _, ok := sc.resolveLocalNFOTarget(ctx, &sqlite.ItemRecord{Type: "Other", Path: artistDir}); ok {
		t.Fatal("unknown nfo target should be false")
	}
	if got := replacePathExtWithNFO("/tmp/noext"); got != "/tmp/noext.nfo" {
		t.Fatalf("replacePathExtWithNFO(noext) = %q", got)
	}
	sc.saveLocalNFOIfEnabled(ctx, &sqlite.ItemRecord{Extra: sqlite.ItemExtra{LockData: true}}, nil, true)

	// 2. audio_scan.go applyAudioProbeTags branches
	if applyAudioProbeTags(nil, map[string]string{"title": "T"}) {
		t.Fatal("nil rec should return false")
	}
	idx, disc := 3, 2
	aud := &sqlite.ItemRecord{IndexNumber: &idx, ParentIndexNumber: &disc}
	if !applyAudioProbeTags(aud, map[string]string{
		"title":        "Song Title",
		"album":        "Best Album",
		"artist":       "Artist A; Artist B",
		"album_artist": "Various Artists",
		"year":         "2019",
	}) {
		t.Fatal("expected applyAudioProbeTags changed=true")
	}
	if aud.SortName != "02-0003-song title" || aud.Extra.AlbumArtist != "Various Artists" || aud.ProductionYear == nil || *aud.ProductionYear != 2019 {
		t.Fatalf("unexpected audio tags: %+v", aud)
	}
	aud2 := &sqlite.ItemRecord{}
	_ = applyAudioProbeTags(aud2, map[string]string{"albumartist": "Solo Artist"})
	if aud2.Extra.AlbumArtist != "Solo Artist" {
		t.Fatalf("expected albumartist fallback, got %q", aud2.Extra.AlbumArtist)
	}

	// 3. extras.go branches
	if et, ok := ClassifyExtraFile("", "/movies/trailers/clip.mp4"); !ok || et != "Trailer" {
		t.Fatalf("empty parentDir trailer = %q, %v", et, ok)
	}
	if _, ok := ClassifyExtraFile("/movies", "/movies/theme-music/clip.mp4"); ok {
		t.Fatal("video inside theme-music should not match ThemeSong")
	}
	if _, ok := ClassifyExtraFile("/movies", "/movies/song.mp3"); ok {
		t.Fatal("non-theme audio should not match extra")
	}
	if !isDedicatedItemDir(nil, rootDir) {
		t.Fatal("nil lib should be treated as dedicated dir")
	}
	if !matchesSiblingExtraPrefix("theme.mp3", "Movie") || !matchesSiblingExtraPrefix("any.mp4", "") || !matchesSiblingExtraPrefix("any.mp4", "   ") {
		t.Fatal("matchesSiblingExtraPrefix edge cases failed")
	}

	// scanAndIndexExtras with symlinked trailers dir + nested symlink + dotfile
	if runtime.GOOS != "windows" {
		extraParentDir := filepath.Join(rootDir, "ExtraMovie")
		realTrailers := filepath.Join(extraParentDir, "real_trailers")
		_ = os.MkdirAll(realTrailers, 0o755)
		_ = os.WriteFile(filepath.Join(realTrailers, ".hidden.mp4"), []byte("h"), 0o600)
		_ = os.WriteFile(filepath.Join(realTrailers, "t1.mp4"), []byte("v"), 0o600)
		_ = os.Symlink(filepath.Join(realTrailers, "t1.mp4"), filepath.Join(realTrailers, "nested_link.mp4"))
		_ = os.MkdirAll(filepath.Join(realTrailers, "subdir"), 0o755)
		_ = os.Symlink(realTrailers, filepath.Join(extraParentDir, "trailers"))

		parentMov := &sqlite.ItemRecord{ID: "p-extra", LibraryID: lib.ID, Type: "Movie", Path: filepath.Join(extraParentDir, "ExtraMovie.mkv")}
		_ = repo.UpsertItem(ctx, parentMov)
		sc.scanAndIndexExtras(ctx, lib, parentMov, extraParentDir, "ExtraMovie")
		sc.scanAndIndexExtras(ctx, nil, parentMov, extraParentDir, "ExtraMovie")
		sc.scanAndIndexExtras(ctx, lib, parentMov, filepath.Join(tmp, "nonexistent"), "ExtraMovie")

		// symlink.go edge cases
		if isPathWithinRoot(rootDir, filepath.Join(tmp, "does-not-exist")) {
			t.Fatal("non-existent target should not be within root")
		}
		if ok, _ := isSymlinkAllowedInRoot(rootDir, filepath.Join(tmp, "does-not-exist"), SymlinkFollowDirect); ok {
			t.Fatal("missing target should not be allowed")
		}
		symOutside := filepath.Join(tmp, "outside_link")
		_ = os.Symlink(m1Path, symOutside)
		if ok, isSym := isSymlinkAllowedInRoot(rootDir, symOutside, SymlinkIgnore); ok || !isSym {
			t.Fatalf("SymlinkIgnore on symlink: ok=%v isSym=%v", ok, isSym)
		}
		if !hasSymlinkComponentUnderRoot(rootDir, symOutside) {
			t.Fatal("expected hasSymlinkComponentUnderRoot true for external symlink")
		}
	}

	// 4. ScanAll + WaitIdle + ScanLibraryAsync coverage
	allStats, err := sc.ScanAll(ctx)
	if err != nil || len(allStats) != 1 {
		t.Fatalf("ScanAll: stats=%+v err=%v", allStats, err)
	}
	sc.WaitIdle(ctx)
	sc.ScanLibraryAsync("")
	waitCtx, waitCancel := context.WithTimeout(ctx, 2*time.Second)
	defer waitCancel()
	sc.WaitIdle(waitCtx)
}
