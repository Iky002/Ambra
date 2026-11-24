package scanner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/plugins/nfo"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

type localSaveStubProvider struct {
	fn func(q plugins.MediaQuery) *plugins.MetadataResult
}

func (p *localSaveStubProvider) Manifest() plugins.Manifest {
	return plugins.Manifest{
		ID:       "ambra.test.online",
		Name:     "Stub Online Scraper",
		Enabled:  true,
		Priority: 20,
		Kinds:    []plugins.Kind{plugins.KindMetadata},
	}
}

func (p *localSaveStubProvider) Configure(map[string]any) error { return nil }

func (p *localSaveStubProvider) FetchMetadata(_ context.Context, q plugins.MediaQuery) (*plugins.MetadataResult, error) {
	if p.fn == nil {
		return nil, nil
	}
	return p.fn(q), nil
}

func newLocalSaveTestImageServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, ".png"):
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("\x89PNG\r\n\x1a\npng-bytes"))
		default:
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("\xff\xd8\xff\xe0jpeg-bytes-" + r.URL.Path))
		}
	}))
}

func TestSaveLocalMetadata_MovieDefaultEnabled(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	mediaRoot := t.TempDir()
	movieDir := filepath.Join(mediaRoot, "Inception (2010)")
	if err := os.MkdirAll(movieDir, 0o755); err != nil {
		t.Fatal(err)
	}
	moviePath := filepath.Join(movieDir, "Inception (2010).mkv")
	if err := os.WriteFile(moviePath, []byte("fake-mkv"), 0o644); err != nil {
		t.Fatal(err)
	}

	imgSrv := newLocalSaveTestImageServer(t)
	defer imgSrv.Close()

	store, err := sqlite.Open(filepath.Join(dataDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{mediaRoot})
	if err != nil {
		t.Fatal(err)
	}
	if !lib.SaveLocalMetadataEnabled() {
		t.Fatalf("expected SaveLocalMetadata to default to true")
	}

	reg := plugins.NewRegistry()
	reg.Register(&localSaveStubProvider{
		fn: func(q plugins.MediaQuery) *plugins.MetadataResult {
			return &plugins.MetadataResult{
				Name:            "盗梦空间",
				OriginalTitle:   "Inception",
				Overview:        "梦境窃贼的故事",
				Tagline:         "你的思维就是犯罪现场",
				Year:            2010,
				PremiereDate:    "2010-07-16",
				CommunityRating: 8.8,
				OfficialRating:  "PG-13",
				Genres:          []string{"科幻", "动作"},
				Studios:         []string{"Warner Bros."},
				Tags:            []string{"梦境"},
				People: []plugins.PersonCredit{
					{Name: "Christopher Nolan", Type: "Director"},
					{Name: "Leonardo DiCaprio", Role: "Cobb", Type: "Actor"},
				},
				ProviderIDs: map[string]string{"Tmdb": "27205", "Imdb": "tt1375666"},
				PosterURL:   imgSrv.URL + "/poster.jpg",
				BackdropURL: imgSrv.URL + "/fanart.jpg",
				LogoURL:     imgSrv.URL + "/logo.png",
			}
		},
	})

	sc := New(repo, reg, nil, &config.Config{DataDir: dataDir})
	sc.SetHTTPClient(imgSrv.Client())

	if _, err := sc.ScanLibrary(ctx, lib.ID); err != nil {
		t.Fatalf("ScanLibrary: %v", err)
	}

	// Verify .nfo, poster.jpg, fanart.jpg, clearlogo.png are saved in movieDir
	nfoPath := filepath.Join(movieDir, "Inception (2010).nfo")
	nfoBytes, err := os.ReadFile(nfoPath)
	if err != nil {
		t.Fatalf("expected NFO saved at %s: %v", nfoPath, err)
	}
	parsed, err := nfo.ParseNFO(nfoBytes)
	if err != nil {
		t.Fatalf("ParseNFO failed on generated NFO: %v\n%s", err, string(nfoBytes))
	}
	if parsed.Name != "盗梦空间" || parsed.OriginalTitle != "Inception" || parsed.Year != 2010 {
		t.Fatalf("unexpected parsed NFO: %+v", parsed)
	}
	if getProviderID(parsed.ProviderIDs, "tmdb") != "27205" || getProviderID(parsed.ProviderIDs, "imdb") != "tt1375666" {
		t.Fatalf("unexpected provider IDs in NFO: %+v", parsed.ProviderIDs)
	}

	posterPath := filepath.Join(movieDir, "poster.jpg")
	fanartPath := filepath.Join(movieDir, "fanart.jpg")
	logoPath := filepath.Join(movieDir, "clearlogo.png")
	for _, p := range []string{posterPath, fanartPath, logoPath} {
		if info, statErr := os.Stat(p); statErr != nil || info.Size() == 0 {
			t.Fatalf("expected artwork file at %s, err=%v", p, statErr)
		}
	}

	// Verify DB image records point to the media folder paths
	items, _, err := repo.QueryItems(ctx, sqlite.ItemFilter{LibraryID: lib.ID, IncludeItemTypes: []string{"Movie"}})
	if err != nil || len(items) != 1 {
		t.Fatalf("expected 1 movie, got %d err=%v", len(items), err)
	}
	imgRec, err := repo.GetImageByItemAndType(ctx, items[0].Id, "Primary", 0)
	if err != nil || imgRec.Path != posterPath {
		t.Fatalf("expected Primary image path %s, got %+v err=%v", posterPath, imgRec, err)
	}
	bdRec, err := repo.GetImageByItemAndType(ctx, items[0].Id, "Backdrop", 0)
	if err != nil || bdRec.Path != fanartPath {
		t.Fatalf("expected Backdrop image path %s, got %+v err=%v", fanartPath, bdRec, err)
	}
}

func TestSaveLocalMetadata_DisabledDoesNotTouchMediaDir(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	mediaRoot := t.TempDir()
	movieDir := filepath.Join(mediaRoot, "Matrix (1999)")
	if err := os.MkdirAll(movieDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(movieDir, "Matrix (1999).mkv"), []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	imgSrv := newLocalSaveTestImageServer(t)
	defer imgSrv.Close()

	store, err := sqlite.Open(filepath.Join(dataDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{mediaRoot})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpdateLibraryOptions(ctx, lib.ID, map[string]any{"SaveLocalMetadata": false}); err != nil {
		t.Fatal(err)
	}

	reg := plugins.NewRegistry()
	reg.Register(&localSaveStubProvider{
		fn: func(q plugins.MediaQuery) *plugins.MetadataResult {
			return &plugins.MetadataResult{
				Name:        "黑客帝国",
				Year:        1999,
				ProviderIDs: map[string]string{"Tmdb": "603"},
				PosterURL:   imgSrv.URL + "/poster.jpg",
				BackdropURL: imgSrv.URL + "/fanart.jpg",
			}
		},
	})

	sc := New(repo, reg, nil, &config.Config{DataDir: dataDir})
	sc.SetHTTPClient(imgSrv.Client())

	if _, err := sc.ScanLibrary(ctx, lib.ID); err != nil {
		t.Fatalf("ScanLibrary: %v", err)
	}

	// Ensure NO .nfo, poster.jpg, or fanart.jpg were written to movieDir
	for _, name := range []string{"Matrix (1999).nfo", "poster.jpg", "fanart.jpg"} {
		if _, statErr := os.Stat(filepath.Join(movieDir, name)); !os.IsNotExist(statErr) {
			t.Fatalf("expected %s NOT to exist when SaveLocalMetadata=false", name)
		}
	}

	// Ensure image was saved in DataDir/metadata/images instead
	items, _, err := repo.QueryItems(ctx, sqlite.ItemFilter{LibraryID: lib.ID, IncludeItemTypes: []string{"Movie"}})
	if err != nil || len(items) != 1 {
		t.Fatalf("expected 1 movie, got %d err=%v", len(items), err)
	}
	imgRec, err := repo.GetImageByItemAndType(ctx, items[0].Id, "Primary", 0)
	if err != nil || !strings.HasPrefix(imgRec.Path, filepath.Join(dataDir, "metadata", "images")) {
		t.Fatalf("expected Primary image in DataDir cache, got %+v err=%v", imgRec, err)
	}
}

func TestSaveLocalMetadata_SeriesSeasonEpisodeAndRefresh(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	mediaRoot := t.TempDir()
	seriesDir := filepath.Join(mediaRoot, "Breaking Bad (2008)")
	seasonDir := filepath.Join(seriesDir, "Season 01")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	epPath := filepath.Join(seasonDir, "Breaking Bad S01E01.mkv")
	if err := os.WriteFile(epPath, []byte("fake-ep"), 0o644); err != nil {
		t.Fatal(err)
	}

	imgSrv := newLocalSaveTestImageServer(t)
	defer imgSrv.Close()

	store, err := sqlite.Open(filepath.Join(dataDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	lib, err := repo.CreateLibrary(ctx, "TV", "tvshows", []string{mediaRoot})
	if err != nil {
		t.Fatal(err)
	}

	reg := plugins.NewRegistry()
	reg.Register(&localSaveStubProvider{
		fn: func(q plugins.MediaQuery) *plugins.MetadataResult {
			switch strings.ToLower(q.ItemType) {
			case "series":
				return &plugins.MetadataResult{
					Name:        "绝命毒师",
					Year:        2008,
					Overview:    "化学老师的转变",
					ProviderIDs: map[string]string{"Tmdb": "1396"},
					PosterURL:   imgSrv.URL + "/series.jpg",
					BackdropURL: imgSrv.URL + "/series-bg.jpg",
				}
			case "season":
				return &plugins.MetadataResult{
					Name:        "第 1 季",
					Overview:    "第一季简介",
					PosterURL:   imgSrv.URL + "/s01.jpg",
					ProviderIDs: map[string]string{"Tmdb": "1396"},
				}
			case "episode":
				return &plugins.MetadataResult{
					Name:         "试播集",
					Overview:     "第一集剧情",
					PremiereDate: "2008-01-20",
					PosterURL:    imgSrv.URL + "/ep1.jpg",
					ProviderIDs:  map[string]string{"Tmdb": "1396"},
				}
			}
			return nil
		},
	})

	sc := New(repo, reg, nil, &config.Config{DataDir: dataDir})
	sc.SetHTTPClient(imgSrv.Client())

	if _, err := sc.ScanLibrary(ctx, lib.ID); err != nil {
		t.Fatalf("ScanLibrary: %v", err)
	}

	expectedFiles := []string{
		filepath.Join(seriesDir, "tvshow.nfo"),
		filepath.Join(seriesDir, "poster.jpg"),
		filepath.Join(seriesDir, "fanart.jpg"),
		filepath.Join(seasonDir, "season.nfo"),
		filepath.Join(seasonDir, "poster.jpg"),
		filepath.Join(seasonDir, "Breaking Bad S01E01.nfo"),
		filepath.Join(seasonDir, "Breaking Bad S01E01-thumb.jpg"),
	}
	for _, p := range expectedFiles {
		if info, statErr := os.Stat(p); statErr != nil || info.Size() == 0 {
			t.Fatalf("expected file %s to exist and be non-empty, err=%v", p, statErr)
		}
	}

	// Verify RefreshItem(replaceMetadata=true) overwrites NFO
	seriesItems, _, err := repo.QueryItems(ctx, sqlite.ItemFilter{LibraryID: lib.ID, IncludeItemTypes: []string{"Series"}})
	if err != nil || len(seriesItems) != 1 {
		t.Fatalf("expected 1 series, got %d", len(seriesItems))
	}
	if err := os.WriteFile(filepath.Join(seriesDir, "tvshow.nfo"), []byte("<tvshow><title>Old</title></tvshow>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := sc.RefreshItem(ctx, seriesItems[0].Id, true, true); err != nil {
		t.Fatalf("RefreshItem: %v", err)
	}
	refreshedNFO, err := os.ReadFile(filepath.Join(seriesDir, "tvshow.nfo"))
	if err != nil || !strings.Contains(string(refreshedNFO), "绝命毒师") {
		t.Fatalf("expected refreshed tvshow.nfo to contain 绝命毒师, got: %s", string(refreshedNFO))
	}
}

func TestSaveLocalMetadata_SharedFlatMovieRootUsesPrefixedArt(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	mediaRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(mediaRoot, "MovieA (2020).mkv"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mediaRoot, "MovieB (2021).mkv"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}

	imgSrv := newLocalSaveTestImageServer(t)
	defer imgSrv.Close()

	store, err := sqlite.Open(filepath.Join(dataDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	lib, err := repo.CreateLibrary(ctx, "FlatMovies", "movies", []string{mediaRoot})
	if err != nil {
		t.Fatal(err)
	}

	reg := plugins.NewRegistry()
	reg.Register(&localSaveStubProvider{
		fn: func(q plugins.MediaQuery) *plugins.MetadataResult {
			return &plugins.MetadataResult{
				Name:        q.Title,
				PosterURL:   imgSrv.URL + "/" + q.Title + ".jpg",
				BackdropURL: imgSrv.URL + "/" + q.Title + "-bg.jpg",
			}
		},
	})

	sc := New(repo, reg, nil, &config.Config{DataDir: dataDir})
	sc.SetHTTPClient(imgSrv.Client())

	if _, err := sc.ScanLibrary(ctx, lib.ID); err != nil {
		t.Fatalf("ScanLibrary: %v", err)
	}

	for _, name := range []string{
		"MovieA (2020).nfo",
		"MovieA (2020)-poster.jpg",
		"MovieA (2020)-fanart.jpg",
		"MovieB (2021).nfo",
		"MovieB (2021)-poster.jpg",
		"MovieB (2021)-fanart.jpg",
	} {
		if info, statErr := os.Stat(filepath.Join(mediaRoot, name)); statErr != nil || info.Size() == 0 {
			t.Fatalf("expected flat movie file %s to exist, err=%v", name, statErr)
		}
	}
}
