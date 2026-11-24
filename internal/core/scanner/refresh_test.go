package scanner

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func makeTestPNGBytes(t *testing.T, r, g, b uint8) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 12))
	for y := 0; y < 12; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

func TestRefreshItemMovieMetadataLocksAndForceImages(t *testing.T) {
	tmp := t.TempDir()
	cfg, err := config.Load(filepath.Join(tmp, "data"))
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	store, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	var posterVersion atomic.Int32
	posterVersion.Store(1)

	imgV1 := makeTestPNGBytes(t, 255, 0, 0)
	imgV2 := makeTestPNGBytes(t, 0, 255, 0)
	backdropBytes := makeTestPNGBytes(t, 0, 0, 255)
	avatarBytes := makeTestPNGBytes(t, 255, 255, 0)

	imgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		switch r.URL.Path {
		case "/poster.png":
			if posterVersion.Load() == 1 {
				_, _ = w.Write(imgV1)
			} else {
				_, _ = w.Write(imgV2)
			}
		case "/backdrop.png":
			_, _ = w.Write(backdropBytes)
		case "/avatar.png":
			_, _ = w.Write(avatarBytes)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer imgSrv.Close()

	online := &zhMockOnlineProvider{
		id: "ambra.builtin.tmdb",
		fetchFn: func(_ context.Context, q plugins.MediaQuery) (*plugins.MetadataResult, error) {
			return &plugins.MetadataResult{
				Name:            "沙丘",
				OriginalTitle:   "Dune",
				Overview:        "全新简介",
				Tagline:         "恐惧是思维的杀手",
				Year:            2021,
				PremiereDate:    "2021-10-22",
				CommunityRating: 8.2,
				OfficialRating:  "PG-13",
				Genres:          []string{"科幻", "冒险"},
				Studios:         []string{"Legendary"},
				Tags:            []string{"沙漠"},
				People: []plugins.PersonCredit{
					{Name: "提莫西·查拉梅", Role: "Paul", Type: "Actor", ImageURL: imgSrv.URL + "/avatar.png"},
				},
				ProviderIDs: map[string]string{"tmdb": "438631", "imdb": "tt1160419"},
				PosterURL:   imgSrv.URL + "/poster.png",
				BackdropURL: imgSrv.URL + "/backdrop.png",
			}, nil
		},
	}

	reg := plugins.NewRegistry()
	reg.Register(online)
	sc := New(repo, reg, eventbus.New(), cfg)
	sc.SetHTTPClient(imgSrv.Client())

	ctx := context.Background()
	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{tmp})
	if err != nil {
		t.Fatal(err)
	}

	origYear := 2020
	movie := &sqlite.ItemRecord{
		ID:             "mov-1",
		LibraryID:      lib.ID,
		ParentID:       lib.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Dune",
		SortName:       "dune",
		ProductionYear: &origYear,
		Extra: sqlite.ItemExtra{
			Overview: "旧简介",
		},
	}
	if err := repo.UpsertItem(ctx, movie); err != nil {
		t.Fatal(err)
	}

	// 1) Non-replace refresh: updates unlocked title, fills empty fields, preserves existing Overview/Year, downloads poster
	if err := sc.RefreshItem(ctx, movie.ID, false, false); err != nil {
		t.Fatalf("RefreshItem(false, false): %v", err)
	}
	got, err := repo.GetItemRecordByID(ctx, movie.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "沙丘" {
		t.Fatalf("expected updated Chinese title '沙丘', got %q", got.Name)
	}
	if got.Extra.Overview != "旧简介" {
		t.Fatalf("expected existing Overview preserved when replaceMetadata=false, got %q", got.Extra.Overview)
	}
	if got.OriginalTitle != "Dune" || got.OfficialRating != "PG-13" || got.Extra.ProviderIDs["tmdb"] != "438631" {
		t.Fatalf("expected empty fields filled, got %+v", got)
	}
	img1, err := repo.GetImageByItemAndType(ctx, movie.ID, "Primary", 0)
	if err != nil || img1 == nil {
		t.Fatalf("expected primary image downloaded: %v", err)
	}
	tagV1 := img1.Tag

	// 2) Change remote poster to V2, refresh with replaceImages=false -> tag should NOT change
	posterVersion.Store(2)
	if err := sc.RefreshItem(ctx, movie.ID, false, false); err != nil {
		t.Fatalf("RefreshItem(false, false) v2: %v", err)
	}
	imgStillV1, _ := repo.GetImageByItemAndType(ctx, movie.ID, "Primary", 0)
	if imgStillV1.Tag != tagV1 {
		t.Fatalf("expected poster tag unchanged when replaceImages=false, got %q != %q", imgStillV1.Tag, tagV1)
	}

	// 3) Lock Overview and Genres, then refresh with replaceMetadata=true, replaceImages=true
	got.Extra.LockedFields = []string{"Overview", "Genres"}
	if err := repo.UpsertItem(ctx, got); err != nil {
		t.Fatal(err)
	}
	if err := sc.RefreshItem(ctx, movie.ID, true, true); err != nil {
		t.Fatalf("RefreshItem(true, true): %v", err)
	}
	got2, err := repo.GetItemRecordByID(ctx, movie.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got2.Extra.Overview != "旧简介" {
		t.Fatalf("expected locked Overview preserved, got %q", got2.Extra.Overview)
	}
	if got2.ProductionYear == nil || *got2.ProductionYear != 2021 {
		t.Fatalf("expected unlocked ProductionYear overwritten to 2021, got %v", got2.ProductionYear)
	}
	imgV2Rec, err := repo.GetImageByItemAndType(ctx, movie.ID, "Primary", 0)
	if err != nil || imgV2Rec == nil || imgV2Rec.Tag == tagV1 {
		t.Fatalf("expected poster replaced when replaceImages=true; old=%q new=%+v", tagV1, imgV2Rec)
	}
	data, err := os.ReadFile(imgV2Rec.Path)
	if err != nil || !bytes.Equal(data, imgV2) {
		t.Fatalf("expected on-disk poster to match v2 bytes")
	}

	// Verify people & avatar were persisted
	people, err := repo.GetPeopleByItemID(ctx, movie.ID)
	if err != nil || len(people) != 1 || people[0].Name != "提莫西·查拉梅" {
		t.Fatalf("expected persisted people, got %+v err=%v", people, err)
	}
	avatarRec, err := repo.GetImageByItemAndType(ctx, people[0].Id, "Primary", 0)
	if err != nil || avatarRec == nil {
		t.Fatalf("expected actor avatar downloaded: %v", err)
	}

	// 4) LockData=true prevents all metadata overwrites
	got2.Name = "手动锁定片名"
	got2.Extra.LockData = true
	got2.Extra.LockedFields = nil
	if err := repo.UpsertItem(ctx, got2); err != nil {
		t.Fatal(err)
	}
	if err := sc.RefreshItem(ctx, movie.ID, true, false); err != nil {
		t.Fatalf("RefreshItem with LockData=true: %v", err)
	}
	got3, _ := repo.GetItemRecordByID(ctx, movie.ID)
	if got3.Name != "手动锁定片名" {
		t.Fatalf("expected LockData=true to protect Name, got %q", got3.Name)
	}

	// 5) Not found returns sqlite.ErrNotFound
	if err := sc.RefreshItem(ctx, "does-not-exist", true, true); !errors.Is(err, sqlite.ErrNotFound) {
		t.Fatalf("expected sqlite.ErrNotFound, got %v", err)
	}
}

func TestRefreshItemSeasonAndEpisodeInheritsSeriesTMDBID(t *testing.T) {
	tmp := t.TempDir()
	cfg, err := config.Load(filepath.Join(tmp, "data"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	var seenQueries []plugins.MediaQuery

	online := &zhMockOnlineProvider{
		id: "ambra.builtin.tmdb",
		fetchFn: func(_ context.Context, q plugins.MediaQuery) (*plugins.MetadataResult, error) {
			seenQueries = append(seenQueries, q)
			switch q.ItemType {
			case "Season":
				return &plugins.MetadataResult{
					Name:     "第一季",
					Overview: "第一季简介",
				}, nil
			case "Episode":
				return &plugins.MetadataResult{
					Name:            "试播集",
					Overview:        "第一集简介",
					CommunityRating: 8.7,
				}, nil
			}
			return nil, nil
		},
	}

	reg := plugins.NewRegistry()
	reg.Register(online)
	sc := New(repo, reg, eventbus.New(), cfg)
	ctx := context.Background()

	lib, err := repo.CreateLibrary(ctx, "TV", "tvshows", []string{tmp})
	if err != nil {
		t.Fatal(err)
	}

	series := &sqlite.ItemRecord{
		ID:        "ser-1",
		LibraryID: lib.ID,
		ParentID:  lib.ID,
		Type:      "Series",
		IsFolder:  true,
		Name:      "Breaking Bad",
		Extra: sqlite.ItemExtra{
			ProviderIDs: map[string]string{"Tmdb": "1396"},
		},
	}
	_ = repo.UpsertItem(ctx, series)

	sNum := 1
	season := &sqlite.ItemRecord{
		ID:          "sea-1",
		LibraryID:   lib.ID,
		ParentID:    series.ID,
		SeriesID:    series.ID,
		Type:        "Season",
		IsFolder:    true,
		Name:        "Season 1",
		IndexNumber: &sNum,
	}
	_ = repo.UpsertItem(ctx, season)

	eNum := 1
	ep := &sqlite.ItemRecord{
		ID:                "ep-1",
		LibraryID:         lib.ID,
		ParentID:          season.ID,
		SeriesID:          series.ID,
		SeasonID:          season.ID,
		Type:              "Episode",
		MediaType:         "Video",
		Name:              "Episode 1",
		IndexNumber:       &eNum,
		ParentIndexNumber: &sNum,
	}
	_ = repo.UpsertItem(ctx, ep)

	if err := sc.RefreshItem(ctx, season.ID, true, false); err != nil {
		t.Fatalf("RefreshItem season: %v", err)
	}
	if err := sc.RefreshItem(ctx, ep.ID, true, false); err != nil {
		t.Fatalf("RefreshItem episode: %v", err)
	}

	if len(seenQueries) != 2 {
		t.Fatalf("expected 2 provider queries, got %d", len(seenQueries))
	}
	if seenQueries[0].ProviderIDs["tmdb"] != "1396" || seenQueries[0].SeasonNumber != 1 {
		t.Fatalf("expected Season query to inherit series TMDB ID 1396 and season 1, got %+v", seenQueries[0])
	}
	if seenQueries[1].ProviderIDs["tmdb"] != "1396" || seenQueries[1].SeasonNumber != 1 || seenQueries[1].EpisodeNumber != 1 {
		t.Fatalf("expected Episode query to inherit series TMDB ID 1396 and S01E01, got %+v", seenQueries[1])
	}

	gotSeason, _ := repo.GetItemRecordByID(ctx, season.ID)
	if gotSeason.Name != "第一季" {
		t.Fatalf("expected updated season name, got %q", gotSeason.Name)
	}
	gotEp, _ := repo.GetItemRecordByID(ctx, ep.ID)
	if gotEp.Name != "试播集" {
		t.Fatalf("expected updated episode name, got %q", gotEp.Name)
	}

	// No online providers -> graceful nil return
	scEmpty := New(repo, plugins.NewRegistry(), eventbus.New(), cfg)
	if err := scEmpty.RefreshItem(ctx, ep.ID, true, true); err != nil {
		t.Fatalf("expected nil when no online provider registered, got %v", err)
	}
}
