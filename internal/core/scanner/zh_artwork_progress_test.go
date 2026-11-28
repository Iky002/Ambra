package scanner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/budget"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/plugins/nfo"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

type zhMockOnlineProvider struct {
	id      string
	calls   atomic.Int32
	blockCh chan struct{}
	enterCh chan struct{}
	fetchFn func(ctx context.Context, q plugins.MediaQuery) (*plugins.MetadataResult, error)
}

func (m *zhMockOnlineProvider) Manifest() plugins.Manifest {
	return plugins.Manifest{
		ID:       m.id,
		Name:     "Mock TMDB ZH",
		Version:  "1.0.0",
		BuiltIn:  true,
		Enabled:  true,
		Priority: 20,
		Kinds:    []plugins.Kind{plugins.KindMetadata},
	}
}

func (m *zhMockOnlineProvider) Configure(map[string]any) error { return nil }

func (m *zhMockOnlineProvider) FetchMetadata(ctx context.Context, q plugins.MediaQuery) (*plugins.MetadataResult, error) {
	m.calls.Add(1)
	if m.enterCh != nil {
		select {
		case m.enterCh <- struct{}{}:
		default:
		}
	}
	if m.blockCh != nil {
		select {
		case <-m.blockCh:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if m.fetchFn != nil {
		return m.fetchFn(ctx, q)
	}
	return nil, nil
}

func TestOnlineChineseTitleOverwriteAndLocks(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "zh_title.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	reg := plugins.NewRegistry()
	reg.Register(nfo.New())

	online := &zhMockOnlineProvider{
		id: "ambra.builtin.tmdb",
		fetchFn: func(_ context.Context, q plugins.MediaQuery) (*plugins.MetadataResult, error) {
			if strings.Contains(strings.ToLower(q.Title), "reloaded") {
				return &plugins.MetadataResult{
					Name:          "黑客帝国2：重装上阵",
					OriginalTitle: "The Matrix Reloaded",
					Year:          2003,
					ProviderIDs:   map[string]string{"Tmdb": "604"},
				}, nil
			}
			return &plugins.MetadataResult{
				Name:            "黑客帝国",
				OriginalTitle:   "The Matrix",
				Overview:        "一名黑客发现了现实世界的真相。",
				Tagline:         "欢迎来到真实世界",
				Year:            1999,
				PremiereDate:    "1999-03-31",
				CommunityRating: 8.7,
				OfficialRating:  "R",
				ProviderIDs:     map[string]string{"Tmdb": "603"},
			}, nil
		},
	}
	reg.Register(online)

	cfg := config.Default(tmp)
	sc := New(repo, reg, eventbus.New(), cfg)
	sc.SetProber(nil)

	mediaDir := filepath.Join(tmp, "movies")
	_ = os.MkdirAll(mediaDir, 0o755)
	// Movie 1: no NFO -> Chinese TMDB title should overwrite English filename title
	movieNoNFO := filepath.Join(mediaDir, "The Matrix (1999).mkv")
	_ = os.WriteFile(movieNoNFO, []byte("video1"), 0o644)

	// Movie 2: has NFO title -> NFO title should be preserved
	movieWithNFO := filepath.Join(mediaDir, "Matrix Reloaded (2003).mkv")
	_ = os.WriteFile(movieWithNFO, []byte("video2"), 0o644)
	_ = os.WriteFile(filepath.Join(mediaDir, "Matrix Reloaded (2003).nfo"),
		[]byte(`<movie><title>自定义本地标题</title><year>2003</year></movie>`), 0o644)

	lib, err := repo.CreateLibrary(context.Background(), "电影", "movies", []string{mediaDir})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := sc.ScanLibrary(context.Background(), lib.ID); err != nil {
		t.Fatalf("ScanLibrary failed: %v", err)
	}

	rec1, err := repo.FindItemByMediaSourcePath(context.Background(), lib.ID, movieNoNFO)
	if err != nil {
		t.Fatalf("find movie1: %v", err)
	}
	if rec1.Name != "黑客帝国" {
		t.Fatalf("expected Chinese title '黑客帝国', got %q", rec1.Name)
	}
	if rec1.SortName != "黑客帝国" {
		t.Fatalf("expected SortName '黑客帝国', got %q", rec1.SortName)
	}
	if rec1.OriginalTitle != "The Matrix" {
		t.Fatalf("expected OriginalTitle 'The Matrix', got %q", rec1.OriginalTitle)
	}
	if rec1.Extra.Overview == "" || rec1.CommunityRating == nil || *rec1.CommunityRating != 8.7 {
		t.Fatalf("expected online metadata fields populated, got %+v", rec1)
	}

	rec2, err := repo.FindItemByMediaSourcePath(context.Background(), lib.ID, movieWithNFO)
	if err != nil {
		t.Fatalf("find movie2: %v", err)
	}
	if rec2.Name != "自定义本地标题" {
		t.Fatalf("expected NFO title preserved, got %q", rec2.Name)
	}

	// Direct lock checks on applyOnlineMetadataToItem
	applyOnlineMetadataToItem(nil, &plugins.MetadataResult{Name: "x"}, false)
	applyOnlineMetadataToItem(&sqlite.ItemRecord{}, nil, false)
	if isFieldLocked(nil, "Name") {
		t.Fatalf("nil record should not be locked")
	}

	lockedRec := &sqlite.ItemRecord{
		Name: "Locked English",
		Extra: sqlite.ItemExtra{
			LockedFields: []string{"name", "Overview"},
		},
	}
	applyOnlineMetadataToItem(lockedRec, &plugins.MetadataResult{
		Name:          "新中文名",
		OriginalTitle: "Orig",
		Overview:      "新简介",
	}, false)
	if lockedRec.Name != "Locked English" {
		t.Fatalf("expected locked Name to remain unchanged, got %q", lockedRec.Name)
	}
	if lockedRec.Extra.Overview != "" {
		t.Fatalf("expected locked Overview to remain empty, got %q", lockedRec.Extra.Overview)
	}
	if lockedRec.OriginalTitle != "Orig" {
		t.Fatalf("expected unlocked OriginalTitle to update, got %q", lockedRec.OriginalTitle)
	}

	lockAllRec := &sqlite.ItemRecord{
		Name:  "All Locked",
		Extra: sqlite.ItemExtra{LockData: true},
	}
	applyOnlineMetadataToItem(lockAllRec, &plugins.MetadataResult{
		Name:          "Should Not Change",
		OriginalTitle: "Nope",
		Year:          2020,
	}, false)
	if lockAllRec.Name != "All Locked" || lockAllRec.OriginalTitle != "" || lockAllRec.ProductionYear != nil {
		t.Fatalf("expected LockData=true to block all updates, got %+v", lockAllRec)
	}
}

func TestRemoteArtworkDownloadPersistAndSkipCached(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "artwork.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	var hits atomic.Int32
	var seenUA atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		seenUA.Store(r.Header.Get("User-Agent"))
		switch r.URL.Path {
		case "/poster.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("\xff\xd8\xff\xe0fake-jpeg-bytes"))
		case "/backdrop.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("\x89PNG\r\n\x1a\nfake-png-bytes"))
		case "/logo.webp":
			w.Header().Set("Content-Type", "image/webp")
			_, _ = w.Write([]byte("RIFF\x18\x00\x00\x00WEBPVP8 fake-webp"))
		case "/html.jpg":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html>error</html>"))
		case "/empty.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	repo := sqlite.NewLibraryRepo(store)
	reg := plugins.NewRegistry()
	reg.Register(&zhMockOnlineProvider{
		id: "ambra.builtin.tmdb",
		fetchFn: func(_ context.Context, _ plugins.MediaQuery) (*plugins.MetadataResult, error) {
			return &plugins.MetadataResult{
				Name:        "星际穿越",
				PosterURL:   srv.URL + "/poster.jpg",
				BackdropURL: srv.URL + "/backdrop.png",
				LogoURL:     srv.URL + "/logo.webp",
			}, nil
		},
	})

	cfg := config.Default(tmp)
	sc := New(repo, reg, eventbus.New(), cfg)
	sc.SetProber(nil)
	sc.SetHTTPClient(srv.Client())

	mediaDir := filepath.Join(tmp, "movies")
	_ = os.MkdirAll(mediaDir, 0o755)
	moviePath := filepath.Join(mediaDir, "Interstellar (2014).mkv")
	_ = os.WriteFile(moviePath, []byte("video"), 0o644)

	lib, err := repo.CreateLibrary(context.Background(), "电影", "movies", []string{mediaDir})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := sc.ScanLibrary(context.Background(), lib.ID); err != nil {
		t.Fatalf("ScanLibrary failed: %v", err)
	}

	if hits.Load() != 3 {
		t.Fatalf("expected 3 artwork downloads, got %d", hits.Load())
	}
	if ua, _ := seenUA.Load().(string); ua != remoteImageUA {
		t.Fatalf("expected User-Agent %q, got %q", remoteImageUA, ua)
	}

	rec, err := repo.FindItemByMediaSourcePath(context.Background(), lib.ID, moviePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		imgType string
		ext     string
	}{
		{"Primary", ".jpg"},
		{"Backdrop", ".png"},
		{"Logo", ".webp"},
	} {
		img, err := repo.GetImageByItemAndType(context.Background(), rec.ID, tc.imgType, 0)
		if err != nil || img == nil {
			t.Fatalf("expected %s image persisted, err=%v", tc.imgType, err)
		}
		if !strings.HasSuffix(img.Path, tc.ext) {
			t.Fatalf("expected %s path suffix %s, got %s", tc.imgType, tc.ext, img.Path)
		}
		if len(img.Tag) != 16 {
			t.Fatalf("expected 16-char tag, got %q", img.Tag)
		}
		if _, err := os.Stat(img.Path); err != nil {
			t.Fatalf("expected %s file on disk at %s: %v", tc.imgType, img.Path, err)
		}
	}

	// Subsequent call skips already cached images
	savedAgain := sc.downloadAndSaveRemoteImages(context.Background(), rec, &plugins.MetadataResult{
		PosterURL:   srv.URL + "/poster.jpg",
		BackdropURL: srv.URL + "/backdrop.png",
		LogoURL:     srv.URL + "/logo.webp",
	})
	if savedAgain {
		t.Fatalf("expected cached images to be skipped")
	}
	if hits.Load() != 3 {
		t.Fatalf("expected no additional HTTP downloads for cached images, got %d", hits.Load())
	}

	// Edge cases: nil args, non-HTTP URL, 404, text/html, empty body
	if sc.downloadAndSaveRemoteImages(context.Background(), nil, &plugins.MetadataResult{}) {
		t.Fatalf("expected false for nil rec")
	}
	if sc.downloadAndSaveRemoteImages(context.Background(), rec, nil) {
		t.Fatalf("expected false for nil meta")
	}
	dummyRec := &sqlite.ItemRecord{ID: "nonexistent-item"}
	if sc.downloadAndSaveRemoteImages(context.Background(), dummyRec, &plugins.MetadataResult{
		PosterURL:   "ftp://invalid/poster.jpg",
		BackdropURL: srv.URL + "/404.jpg",
		LogoURL:     srv.URL + "/html.jpg",
	}) {
		t.Fatalf("expected false for invalid/404/html artwork URLs")
	}
	if sc.downloadAndSaveRemoteImages(context.Background(), dummyRec, &plugins.MetadataResult{
		PosterURL: srv.URL + "/empty.jpg",
	}) {
		t.Fatalf("expected false for empty image body")
	}

	// Extension detection fallbacks
	if got := detectImageExtension("", "https://img.example/a.png?v=1"); got != ".png" {
		t.Fatalf("expected .png, got %s", got)
	}
	if got := detectImageExtension("", "https://img.example/a.webp"); got != ".webp" {
		t.Fatalf("expected .webp, got %s", got)
	}
	if got := detectImageExtension("", "https://img.example/a.jpeg"); got != ".jpg" {
		t.Fatalf("expected .jpg, got %s", got)
	}
	if got := detectImageExtension("application/octet-stream", "https://img.example/noext"); got != ".jpg" {
		t.Fatalf("expected default .jpg, got %s", got)
	}

	// Fallback storage dir when cfg == nil
	scNoCfg := New(repo, reg, nil, nil)
	if !strings.Contains(scNoCfg.imageStorageDir(), "ambra-metadata-images") {
		t.Fatalf("unexpected fallback imageStorageDir: %s", scNoCfg.imageStorageDir())
	}
}

func TestRescanUnchangedBackfillsMissingPosterAndTitle(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "backfill.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("\xff\xd8\xff\xe0poster-bytes"))
	}))
	defer srv.Close()

	repo := sqlite.NewLibraryRepo(store)
	reg := plugins.NewRegistry()
	cfg := config.Default(tmp)
	sc := New(repo, reg, eventbus.New(), cfg)
	sc.SetProber(nil)
	sc.SetHTTPClient(srv.Client())

	mediaDir := filepath.Join(tmp, "media")
	seriesDir := filepath.Join(mediaDir, "Breaking Bad", "Season 1")
	_ = os.MkdirAll(seriesDir, 0o755)
	moviePath := filepath.Join(mediaDir, "Inception (2010).mkv")
	epPath := filepath.Join(seriesDir, "Breaking Bad S01E01.mkv")
	_ = os.WriteFile(moviePath, []byte("movie-data"), 0o644)
	_ = os.WriteFile(epPath, []byte("ep-data"), 0o644)

	lib, err := repo.CreateLibrary(context.Background(), "综合库", "mixed", []string{mediaDir})
	if err != nil {
		t.Fatal(err)
	}

	// Pass 1: scan WITHOUT online provider -> English titles, no Primary images
	if _, err := sc.ScanLibrary(context.Background(), lib.ID); err != nil {
		t.Fatalf("initial scan failed: %v", err)
	}
	m1, err := repo.FindItemByMediaSourcePath(context.Background(), lib.ID, moviePath)
	if err != nil {
		t.Fatal(err)
	}
	if m1.Name != "Inception" {
		t.Fatalf("expected filename title 'Inception', got %q", m1.Name)
	}
	if !sc.isMissingPrimaryImage(context.Background(), m1.ID) {
		t.Fatalf("expected movie to be missing Primary image initially")
	}

	// Pass 2: enable online provider and re-scan unchanged files -> should backfill Chinese titles + posters!
	online := &zhMockOnlineProvider{
		id: "ambra.builtin.tmdb",
		fetchFn: func(_ context.Context, q plugins.MediaQuery) (*plugins.MetadataResult, error) {
			switch q.ItemType {
			case "Movie":
				return &plugins.MetadataResult{
					Name:        "盗梦空间",
					PosterURL:   srv.URL + "/inception.jpg",
					ProviderIDs: map[string]string{"Tmdb": "27205"},
				}, nil
			case "Series":
				return &plugins.MetadataResult{
					Name:        "绝命毒师",
					PosterURL:   srv.URL + "/bb.jpg",
					ProviderIDs: map[string]string{"Tmdb": "1396"},
				}, nil
			case "Season":
				return &plugins.MetadataResult{
					Name:      "第 1 季",
					PosterURL: srv.URL + "/bb_s01.jpg",
				}, nil
			case "Episode":
				return &plugins.MetadataResult{
					Name:      "试播集",
					PosterURL: srv.URL + "/bb_s01e01.jpg",
				}, nil
			}
			return nil, nil
		},
	}
	reg.Register(online)

	if _, err := sc.ScanLibrary(context.Background(), lib.ID); err != nil {
		t.Fatalf("backfill scan failed: %v", err)
	}

	m2, _ := repo.FindItemByMediaSourcePath(context.Background(), lib.ID, moviePath)
	if m2.Name != "盗梦空间" {
		t.Fatalf("expected backfilled Chinese movie title '盗梦空间', got %q", m2.Name)
	}
	if sc.isMissingPrimaryImage(context.Background(), m2.ID) {
		t.Fatalf("expected movie Primary image after backfill")
	}

	ep2, _ := repo.FindItemByMediaSourcePath(context.Background(), lib.ID, epPath)
	if ep2.Name != "试播集" {
		t.Fatalf("expected backfilled Chinese episode title '试播集', got %q", ep2.Name)
	}
	if sc.isMissingPrimaryImage(context.Background(), ep2.ID) {
		t.Fatalf("expected episode Primary image after backfill")
	}
	series2, _ := repo.GetItemRecordByID(context.Background(), ep2.SeriesID)
	if series2.Name != "绝命毒师" {
		t.Fatalf("expected backfilled Chinese series title '绝命毒师', got %q", series2.Name)
	}
	if sc.isMissingPrimaryImage(context.Background(), series2.ID) {
		t.Fatalf("expected series Primary image after backfill")
	}

	// Pass 3: scan again when unchanged and posters exist -> online provider should NOT be called
	callsBefore := online.calls.Load()
	if _, err := sc.ScanLibrary(context.Background(), lib.ID); err != nil {
		t.Fatalf("third scan failed: %v", err)
	}
	if online.calls.Load() != callsBefore {
		t.Fatalf("expected no online calls when unchanged and posters exist, before=%d after=%d", callsBefore, online.calls.Load())
	}
}

func TestScanProgressLifecycleAndMultiLibrary(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "progress.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	reg := plugins.NewRegistry()

	blockCh := make(chan struct{})
	enterCh := make(chan struct{}, 2)
	reg.Register(&zhMockOnlineProvider{
		id:      "ambra.builtin.tmdb",
		blockCh: blockCh,
		enterCh: enterCh,
		fetchFn: func(_ context.Context, q plugins.MediaQuery) (*plugins.MetadataResult, error) {
			return &plugins.MetadataResult{Name: q.Title + " 中文"}, nil
		},
	})

	cfg := config.Default(tmp)
	sc := New(repo, reg, eventbus.New(), cfg)
	sc.SetProber(nil)
	sc.SetBudget(budget.New(1))

	// Idle state
	idle := sc.Progress()
	if idle.Running || idle.Phase != "idle" {
		t.Fatalf("expected idle progress, got %+v", idle)
	}

	mediaDir := filepath.Join(tmp, "movies")
	_ = os.MkdirAll(mediaDir, 0o755)
	_ = os.WriteFile(filepath.Join(mediaDir, "Alpha (2021).mkv"), []byte("a"), 0o644)
	_ = os.WriteFile(filepath.Join(mediaDir, "Beta (2022).mkv"), []byte("b"), 0o644)

	lib, err := repo.CreateLibrary(context.Background(), "测试电影库", "movies", []string{mediaDir})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := sc.ScanLibraryProgressive(context.Background(), lib.ID); err != nil {
		t.Fatalf("ScanLibraryProgressive failed: %v", err)
	}

	// Wait for Phase B worker to enter FetchMetadata
	select {
	case <-enterCh:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for Phase B to start")
	}

	live := sc.Progress()
	if !live.Running || live.Phase != "enriching" {
		t.Fatalf("expected running enriching progress, got %+v", live)
	}
	if live.Total != 2 || live.LibraryID != lib.ID || live.LibraryName != "测试电影库" {
		t.Fatalf("unexpected live progress metadata: %+v", live)
	}
	if live.CurrentItem == "" || live.CurrentAction == "" || live.StartedAt == nil {
		t.Fatalf("expected CurrentItem, CurrentAction, StartedAt set, got %+v", live)
	}
	if len(live.Libraries) != 1 {
		t.Fatalf("expected 1 library in breakdown, got %d", len(live.Libraries))
	}

	// Unblock Phase B and wait for idle
	close(blockCh)
	waitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sc.WaitIdle(waitCtx)

	done := sc.Progress()
	if done.Running || done.Phase != "idle" {
		t.Fatalf("expected idle after WaitIdle, got %+v", done)
	}

	// Multi-library aggregation test
	sc.startLibraryProgress("lib-a", "电影库A")
	sc.recordDiscoveryItem("lib-a", "a.mkv", true, false, false)
	sc.recordDiscoveryItem("lib-a", "b.mkv", false, true, false)
	sc.recordDiscoveryItem("lib-a", "c.mkv", false, false, true)

	sc.startLibraryProgress("lib-b", "剧集库B")
	sc.startEnrichPhase("lib-b", 4)
	sc.updateEnrichCurrent("lib-b", "绝命毒师", "刮削元数据与下载海报")
	sc.recordEnrichDone("lib-b", true)
	sc.recordEnrichDone("lib-b", true)

	multi := sc.Progress()
	if !multi.Running || multi.Phase != "discovering" || len(multi.Libraries) != 2 {
		t.Fatalf("unexpected multi-library progress: %+v", multi)
	}
	if multi.Added != 1 || multi.Skipped != 1 || multi.Failed != 1 || multi.Updated != 2 {
		t.Fatalf("unexpected aggregated counts: %+v", multi)
	}

	sc.finishLibraryProgress("lib-a")
	onlyB := sc.Progress()
	if !onlyB.Running || onlyB.Phase != "enriching" || onlyB.Percentage != 50 {
		t.Fatalf("expected enriching 50%% for lib-b, got %+v", onlyB)
	}
	sc.finishLibraryProgress("lib-b")
}

func TestFetchRemoteImageValidatesRasterMagicBytes(t *testing.T) {
	payloads := map[string][]byte{
		"/jpeg":   []byte("\xff\xd8\xff\xe0\x00\x10JFIF"),
		"/png":    []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"),
		"/webp":   []byte("RIFF\x18\x00\x00\x00WEBPVP8 "),
		"/gif":    []byte("GIF89a\x01\x00\x01\x00"),
		"/bmp":    []byte("BM\x36\x00\x00\x00\x00\x00"),
		"/avif":   []byte("\x00\x00\x00\x1cftypavif\x00\x00\x00\x00"),
		"/avis":   []byte("\x00\x00\x00\x1cftypavis\x00\x00\x00\x00"),
		"/mif1":   []byte("\x00\x00\x00\x1cftypmif1\x00\x00\x00\x00"),
		"/html":   []byte("<html><body><script>alert(1)</script></body></html>"),
		"/svg":    []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`),
		"/binary": []byte{0x7f, 'E', 'L', 'F', 0x02, 0x01, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := payloads[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		// Spoof image/jpeg on all responses so acceptance depends on magic-byte verification.
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	ctx := context.Background()
	for _, path := range []string{"/jpeg", "/png", "/webp", "/gif", "/bmp", "/avif", "/avis", "/mif1"} {
		data, _, ok := fetchRemoteImage(ctx, srv.Client(), srv.URL+path)
		if !ok || len(data) == 0 {
			t.Fatalf("expected valid raster payload %s to be accepted", path)
		}
	}
	for _, path := range []string{"/html", "/svg", "/binary"} {
		data, _, ok := fetchRemoteImage(ctx, srv.Client(), srv.URL+path)
		if ok || data != nil {
			t.Fatalf("expected non-image payload %s to be rejected, got ok=%v len=%d", path, ok, len(data))
		}
	}
}

