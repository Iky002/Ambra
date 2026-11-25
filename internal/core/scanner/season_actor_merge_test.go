package scanner

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/plugins/tmdb"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestPhaseBWaveOrderSeasonEpisodeAndActorAvatars(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "wave_season_actor.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	var (
		orderMu    sync.Mutex
		fetchOrder []string
		avatarHits atomic.Int32
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/tv":
			_, _ = w.Write([]byte(`{"results":[{"id":1396}]}`))
		case "/tv/1396":
			orderMu.Lock()
			fetchOrder = append(fetchOrder, "Series")
			orderMu.Unlock()
			_, _ = w.Write([]byte(`{
				"id": 1396,
				"name": "绝命毒师",
				"original_name": "Breaking Bad",
				"overview": "高中化学老师制毒故事",
				"first_air_date": "2008-01-20",
				"vote_average": 8.9,
				"poster_path": "/img/series_poster.jpg",
				"backdrop_path": "/img/series_backdrop.jpg",
				"genres": [{"name": "剧情"}],
				"networks": [{"name": "AMC"}],
				"credits": {
					"crew": [
						{"name": "Vince Gilligan", "job": "Director", "profile_path": "/img/vince.jpg"}
					],
					"cast": [
						{"name": "Bryan Cranston", "character": "Walter White", "profile_path": "/img/bryan.jpg"},
						{"name": "Aaron Paul", "character": "Jesse Pinkman", "profile_path": "/img/aaron.jpg"}
					]
				}
			}`))
		case "/tv/1396/season/1":
			orderMu.Lock()
			fetchOrder = append(fetchOrder, "Season")
			orderMu.Unlock()
			_, _ = w.Write([]byte(`{
				"name": "第 1 季",
				"overview": "第一季完整简介",
				"air_date": "2008-01-20",
				"poster_path": "/img/season1_poster.jpg"
			}`))
		case "/tv/1396/season/1/episode/1":
			orderMu.Lock()
			fetchOrder = append(fetchOrder, "Episode")
			orderMu.Unlock()
			_, _ = w.Write([]byte(`{
				"name": "试播集",
				"overview": "沃尔特确诊肺癌并开始制毒。",
				"air_date": "2008-01-20",
				"vote_average": 8.4,
				"still_path": "/img/ep1_still.jpg"
			}`))
		default:
			if strings.HasPrefix(r.URL.Path, "/img/") {
				if strings.Contains(r.URL.Path, "vince") || strings.Contains(r.URL.Path, "bryan") || strings.Contains(r.URL.Path, "aaron") {
					avatarHits.Add(1)
				}
				w.Header().Set("Content-Type", "image/jpeg")
				_, _ = w.Write([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F'})
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	repo := sqlite.NewLibraryRepo(store)
	reg := plugins.NewRegistry()
	tmdbProv := tmdb.New("test-api-key", "zh-CN")
	tmdbProv.SetHTTPClient(srv.Client())
	_ = tmdbProv.Configure(map[string]any{
		"base_url":       srv.URL,
		"image_base_url": srv.URL,
	})
	reg.Register(tmdbProv)

	cfg := config.Default(tmp)
	sc := New(repo, reg, eventbus.New(), cfg)
	sc.SetProber(nil)
	sc.SetHTTPClient(srv.Client())

	mediaDir := filepath.Join(tmp, "tv")
	epDir := filepath.Join(mediaDir, "Breaking Bad (2008)", "Season 01")
	_ = os.MkdirAll(epDir, 0o755)
	epPath := filepath.Join(epDir, "Breaking Bad - S01E01.mkv")
	_ = os.WriteFile(epPath, []byte("fake-episode"), 0o644)

	lib, err := repo.CreateLibrary(context.Background(), "剧集", "tvshows", []string{mediaDir})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := sc.ScanLibrary(context.Background(), lib.ID); err != nil {
		t.Fatalf("ScanLibrary failed: %v", err)
	}

	// Verify Wave 1 (Series) ran strictly before Wave 2 (Season, Episode)
	orderMu.Lock()
	if len(fetchOrder) != 3 || fetchOrder[0] != "Series" {
		t.Fatalf("expected Series first in Wave 1 before Season/Episode in Wave 2, got %v", fetchOrder)
	}
	orderMu.Unlock()

	epRec, err := repo.FindItemByMediaSourcePath(context.Background(), lib.ID, epPath)
	if err != nil {
		t.Fatalf("find episode: %v", err)
	}
	if epRec.Name != "试播集" || epRec.Extra.Overview != "沃尔特确诊肺癌并开始制毒。" {
		t.Fatalf("expected scraped episode metadata, got name=%q overview=%q", epRec.Name, epRec.Extra.Overview)
	}
	if sc.isMissingPrimaryImage(context.Background(), epRec.ID) {
		t.Fatalf("expected episode still image to be saved as Primary")
	}

	seasonRec, err := repo.GetItemRecordByID(context.Background(), epRec.SeasonID)
	if err != nil {
		t.Fatalf("get season: %v", err)
	}
	if seasonRec.Name != "第 1 季" || seasonRec.Extra.Overview != "第一季完整简介" {
		t.Fatalf("expected scraped season metadata, got name=%q overview=%q", seasonRec.Name, seasonRec.Extra.Overview)
	}
	if sc.isMissingPrimaryImage(context.Background(), seasonRec.ID) {
		t.Fatalf("expected season poster image to be saved as Primary")
	}

	seriesDTO, err := repo.GetItemByID(context.Background(), "srv1", "", epRec.SeriesID)
	if err != nil {
		t.Fatalf("get series DTO: %v", err)
	}
	if seriesDTO.Name != "绝命毒师" || len(seriesDTO.People) != 3 {
		t.Fatalf("expected series '绝命毒师' with 3 people, got name=%q people=%d", seriesDTO.Name, len(seriesDTO.People))
	}
	for _, person := range seriesDTO.People {
		if person.PrimaryImageTag == "" {
			t.Fatalf("expected PrimaryImageTag populated for person %q", person.Name)
		}
		imgRec, err := repo.GetImageByItemAndType(context.Background(), person.Id, "Primary", 0)
		if err != nil || imgRec == nil || imgRec.Path == "" {
			t.Fatalf("expected saved avatar image for person %q: err=%v", person.Name, err)
		}
	}

	// Verify ListTaxonomies("person") also exposes ImageTags["Primary"]
	persons, total, err := repo.ListTaxonomies(context.Background(), "srv1", "person", "", 0, 50)
	if err != nil || total != 3 {
		t.Fatalf("ListTaxonomies(person) failed: total=%d err=%v", total, err)
	}
	for _, p := range persons {
		if p.ImageTags["Primary"] == "" {
			t.Fatalf("expected ImageTags[Primary] for taxonomy person %q", p.Name)
		}
	}
	if avatarHits.Load() != 3 {
		t.Fatalf("expected 3 avatar downloads, got %d", avatarHits.Load())
	}

	// Verify cap of 12 avatars per item
	manyPeople := make([]plugins.PersonCredit, 15)
	for i := range manyPeople {
		manyPeople[i] = plugins.PersonCredit{
			Name:     fmt.Sprintf("Actor %02d", i),
			Type:     "Actor",
			ImageURL: fmt.Sprintf("%s/img/actor_%02d.jpg", srv.URL, i),
		}
	}
	sc.persistMetaRelationsAndFTS(context.Background(), seasonRec, &plugins.MetadataResult{People: manyPeople})
	persisted, err := repo.GetPeopleByItemID(context.Background(), seasonRec.ID)
	if err != nil || len(persisted) != 15 {
		t.Fatalf("expected 15 people persisted, got %d err=%v", len(persisted), err)
	}
	withTag := 0
	for _, p := range persisted {
		if p.PrimaryImageTag != "" {
			withTag++
		}
	}
	if withTag != maxPersonAvatarsPerItem {
		t.Fatalf("expected avatar downloads capped at %d, got %d", maxPersonAvatarsPerItem, withTag)
	}
}

func TestPhaseBAutoMergeMoviesByTMDBID(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "phaseb_merge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	reg := plugins.NewRegistry()
	reg.Register(&zhMockOnlineProvider{
		id: "ambra.builtin.tmdb",
		fetchFn: func(_ context.Context, q plugins.MediaQuery) (*plugins.MetadataResult, error) {
			return &plugins.MetadataResult{
				Name:          "沙丘",
				OriginalTitle: "Dune",
				Year:          2021,
				ProviderIDs:   map[string]string{"tmdb": "438631"},
			}, nil
		},
	})

	cfg := config.Default(tmp)
	sc := New(repo, reg, eventbus.New(), cfg)
	sc.SetProber(nil)

	// Two different directories so Phase A does NOT merge them by same-directory title
	mediaDir := filepath.Join(tmp, "movies")
	dirA := filepath.Join(mediaDir, "4K")
	dirB := filepath.Join(mediaDir, "1080p")
	_ = os.MkdirAll(dirA, 0o755)
	_ = os.MkdirAll(dirB, 0o755)

	fileA := filepath.Join(dirA, "Dune.2021.2160p.HDR.mkv")
	fileB := filepath.Join(dirB, "Dune.2021.1080p.mkv")
	_ = os.WriteFile(fileA, []byte("4k-data"), 0o644)
	_ = os.WriteFile(fileB, []byte("1080p-data"), 0o644)

	lib, err := repo.CreateLibrary(context.Background(), "电影", "movies", []string{mediaDir})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := sc.ScanLibrary(context.Background(), lib.ID); err != nil {
		t.Fatalf("ScanLibrary failed: %v", err)
	}

	movies, total, err := repo.QueryItems(context.Background(), sqlite.ItemFilter{
		LibraryID:        lib.ID,
		IncludeItemTypes: []string{"Movie"},
		Fields:           []string{"MediaSources", "ProviderIds"},
	})
	if err != nil {
		t.Fatalf("QueryItems failed: %v", err)
	}
	if total != 1 || len(movies) != 1 {
		t.Fatalf("expected 1 merged Movie after Phase B TMDB merge, got total=%d len=%d", total, len(movies))
	}
	if len(movies[0].MediaSources) != 2 {
		t.Fatalf("expected 2 MediaSources attached to merged Movie, got %d", len(movies[0].MediaSources))
	}
	if movies[0].Name != "沙丘" {
		t.Fatalf("expected merged Movie name '沙丘', got %q", movies[0].Name)
	}
}
