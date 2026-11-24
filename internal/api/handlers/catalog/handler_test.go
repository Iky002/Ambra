package catalog

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestCatalogFiltersAndCategories(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "cat.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	cfg := config.Default(tmp)

	lib, _ := repo.CreateLibrary(ctx, "All", "movies", []string{tmp})

	// Seed a Movie with streams, rating, container, genre, tag
	movie := &sqlite.ItemRecord{
		LibraryID:      lib.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Dune",
		OfficialRating: "PG-13",
		Container:      "mkv",
	}
	if err := repo.UpsertItem(ctx, movie); err != nil {
		t.Fatal(err)
	}
	_ = repo.ReplaceItemTaxonomies(ctx, movie.ID, "genre", []string{"Sci-Fi"})
	_ = repo.ReplaceItemTaxonomies(ctx, movie.ID, "tag", []string{"IMAX"})
	_ = repo.ReplaceMediaSourcesAndStreams(ctx, movie.ID, []sqlite.MediaSourceRecord{
		{
			ItemID:    movie.ID,
			Name:      "Dune.mkv",
			Path:      "/movies/Dune.mkv",
			Container: "mkv",
			Streams: []sqlite.MediaStreamRecord{
				{StreamIndex: 0, StreamType: "Video", Codec: "hevc", VideoRange: "HDR10", Width: 3840, Height: 2160},
				{StreamIndex: 1, StreamType: "Audio", Codec: "truehd", AudioLayout: "7.1", Language: "eng"},
				{StreamIndex: 2, StreamType: "Subtitle", Codec: "ass", Language: "chi"},
			},
		},
	})

	// Seed a Song and a Game for MusicGenres / GameGenres
	song := &sqlite.ItemRecord{LibraryID: lib.ID, Type: "Audio", MediaType: "Audio", Name: "Track 1", Container: "flac"}
	_ = repo.UpsertItem(ctx, song)
	_ = repo.ReplaceItemTaxonomies(ctx, song.ID, "genre", []string{"Jazz"})

	game := &sqlite.ItemRecord{LibraryID: lib.ID, Type: "Game", Name: "Elden Ring"}
	_ = repo.UpsertItem(ctx, game)
	_ = repo.ReplaceItemTaxonomies(ctx, game.ID, "genre", []string{"RPG"})

	mux := http.NewServeMux()
	h := New(repo, cfg)
	h.RegisterRoutes(mux, AuthFuncs{})

	checks := []struct {
		endpoint string
		wantName string
	}{
		{"/VideoCodecs", "hevc"},
		{"/AudioCodecs", "truehd"},
		{"/AudioLayouts", "7.1"},
		{"/SubtitleCodecs", "ass"},
		{"/StreamLanguages", "chi"},
		{"/ExtendedVideoTypes", "HDR10"},
		{"/Containers", "mkv"},
		{"/OfficialRatings", "PG-13"},
		{"/Tags", "IMAX"},
		{"/ItemTypes", "Movie"},
		{"/MusicGenres", "Jazz"},
		{"/GameGenres", "RPG"},
		{"/Games", "Elden Ring"},
	}

	for _, tc := range checks {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.endpoint, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", tc.endpoint, rec.Code, rec.Body.String())
		}
		var qr dto.QueryResult[dto.BaseItemDto]
		if err := json.Unmarshal(rec.Body.Bytes(), &qr); err != nil {
			t.Fatalf("GET %s invalid JSON: %v", tc.endpoint, err)
		}
		found := false
		for _, it := range qr.Items {
			if it.Name == tc.wantName {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("GET %s expected %q in %+v", tc.endpoint, tc.wantName, qr.Items)
		}
	}

	// Verify /Movies/Recommendations
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Movies/Recommendations", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /Movies/Recommendations status=%d", rec.Code)
	}
	var cats []sqlite.RecommendationCategory
	if err := json.Unmarshal(rec.Body.Bytes(), &cats); err != nil || len(cats) == 0 {
		t.Fatalf("expected non-empty recommendations, got %+v err=%v", cats, err)
	}

	// Verify /Songs/{Id}/InstantMix
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Songs/"+song.ID+"/InstantMix", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /Songs/{Id}/InstantMix status=%d", rec.Code)
	}
}
