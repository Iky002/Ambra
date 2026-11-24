package scanner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/ticks"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestParseFFprobeJSONChapters(t *testing.T) {
	raw := []byte(`{
		"format": {"format_name": "matroska,webm", "duration": "3600.0"},
		"streams": [{"index": 0, "codec_type": "video", "codec_name": "h264", "width": 1920, "height": 1080}],
		"chapters": [
			{"id": 0, "time_base": "1/1000", "start": 0, "start_time": "0.000000", "tags": {"title": "Opening"}},
			{"id": 1, "time_base": "1/1000", "start": 120500, "start_time": "120.500000", "tags": {"title": "Act I"}},
			{"id": 2, "time_base": "1/1000", "start": 300000, "tags": {}}
		]
	}`)

	res, err := ParseFFprobeJSON(raw)
	if err != nil {
		t.Fatalf("ParseFFprobeJSON: %v", err)
	}
	if len(res.Chapters) != 3 {
		t.Fatalf("expected 3 chapters, got %d", len(res.Chapters))
	}
	if res.Chapters[0].Name != "Opening" || res.Chapters[0].StartPositionTicks != 0 {
		t.Fatalf("unexpected chapter 0: %+v", res.Chapters[0])
	}
	if res.Chapters[1].Name != "Act I" || res.Chapters[1].StartPositionTicks != ticks.FromSeconds(120.5) {
		t.Fatalf("unexpected chapter 1: %+v", res.Chapters[1])
	}
	if res.Chapters[2].Name != "Chapter 3" || res.Chapters[2].StartPositionTicks != ticks.FromSeconds(300) {
		t.Fatalf("unexpected chapter 2: %+v", res.Chapters[2])
	}
}

type mockCollectionProvider struct {
	results map[string]*plugins.MetadataResult
}

func (m *mockCollectionProvider) Manifest() plugins.Manifest {
	return plugins.Manifest{
		ID:       "mock.tmdb",
		Name:     "Mock TMDB",
		Enabled:  true,
		Priority: 10,
		Kinds:    []plugins.Kind{plugins.KindMetadata},
	}
}
func (m *mockCollectionProvider) Configure(map[string]any) error { return nil }
func (m *mockCollectionProvider) FetchMetadata(_ context.Context, q plugins.MediaQuery) (*plugins.MetadataResult, error) {
	if res, ok := m.results[q.Title]; ok {
		return res, nil
	}
	return nil, nil
}

func TestAutoBoxSetCreationAndDeduplication(t *testing.T) {
	tmp := t.TempDir()
	st, err := sqlite.Open(filepath.Join(tmp, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	repo := sqlite.NewLibraryRepo(st)
	ctx := context.Background()

	// Mock image server for collection poster & backdrop
	imgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("\xff\xd8\xff\xe0fake-jpeg-data"))
	}))
	defer imgSrv.Close()

	col := &plugins.CollectionMeta{
		ID:          "10",
		Name:        "Star Wars Collection",
		PosterURL:   imgSrv.URL + "/poster.jpg",
		BackdropURL: imgSrv.URL + "/backdrop.jpg",
	}

	reg := plugins.NewRegistry()
	reg.Register(&mockCollectionProvider{
		results: map[string]*plugins.MetadataResult{
			"Star Wars": {
				Name:        "Star Wars",
				ProviderIDs: map[string]string{"Tmdb": "11"},
				Collection:  col,
			},
			"The Empire Strikes Back": {
				Name:        "The Empire Strikes Back",
				ProviderIDs: map[string]string{"Tmdb": "1891"},
				Collection:  col,
			},
		},
	})

	mediaDir := filepath.Join(tmp, "movies")
	if err := os.MkdirAll(mediaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	f1 := filepath.Join(mediaDir, "Star Wars (1977).mkv")
	f2 := filepath.Join(mediaDir, "The Empire Strikes Back (1980).mkv")
	_ = os.WriteFile(f1, []byte("v1"), 0o644)
	_ = os.WriteFile(f2, []byte("v2"), 0o644)

	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{mediaDir})
	if err != nil {
		t.Fatal(err)
	}

	sc := New(repo, reg, nil, &config.Config{DataDir: filepath.Join(tmp, "data")})
	sc.SetProber(NewDisabledProber())
	sc.SetHTTPClient(imgSrv.Client())

	if _, err := sc.ScanLibrary(ctx, lib.ID); err != nil {
		t.Fatalf("ScanLibrary: %v", err)
	}

	// Verify exactly 1 deduplicated BoxSet was created with TMDB ID "10"
	boxSets, err := repo.FindItemsByProviderID(ctx, "collections", "BoxSet", "Tmdb", "10")
	if err != nil || len(boxSets) != 1 {
		t.Fatalf("expected 1 BoxSet, got len=%d err=%v", len(boxSets), err)
	}
	bs := boxSets[0]
	if bs.Name != "Star Wars Collection" {
		t.Fatalf("unexpected BoxSet name: %q", bs.Name)
	}

	// Verify both movies were added to the BoxSet
	members, total, err := repo.GetContainerItems(ctx, "srv1", "", bs.ID, 0, 10, nil)
	if err != nil || total != 2 || len(members) != 2 {
		t.Fatalf("expected 2 movies in BoxSet, got total=%d len=%d err=%v", total, len(members), err)
	}

	// Verify poster & backdrop were downloaded for the BoxSet
	if img, err := repo.GetImageByItemAndType(ctx, bs.ID, "Primary", 0); err != nil || img == nil {
		t.Fatalf("expected Primary image on BoxSet, err=%v", err)
	}
	if img, err := repo.GetImageByItemAndType(ctx, bs.ID, "Backdrop", 0); err != nil || img == nil {
		t.Fatalf("expected Backdrop image on BoxSet, err=%v", err)
	}

	// Verify concurrent ensureAutoBoxSet calls don't duplicate BoxSets
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sc.ensureAutoBoxSet(ctx, &sqlite.ItemRecord{ID: members[0].Id, Type: "Movie"}, &plugins.CollectionMeta{
				ID:   "999",
				Name: "Concurrent Collection",
			})
		}()
	}
	wg.Wait()
	concurrentSets, err := repo.FindItemsByProviderID(ctx, "collections", "BoxSet", "Tmdb", "999")
	if err != nil || len(concurrentSets) != 1 {
		t.Fatalf("expected 1 concurrent BoxSet, got len=%d err=%v", len(concurrentSets), err)
	}
}
