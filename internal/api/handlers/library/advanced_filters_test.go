package library

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestQueryItems_AdvancedEmbyFiltersAndTaxonomyParental(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "adv.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)

	lib, _ := repo.CreateLibrary(ctx, "Movies", "movies", []string{tmp})

	r85 := 8.5
	r60 := 6.0
	y2020 := 2020
	y2022 := 2022

	m4k := &sqlite.ItemRecord{
		ID:              guid.New(),
		LibraryID:       lib.ID,
		ParentID:        lib.ID,
		Type:            "Movie",
		MediaType:       "Video",
		Name:            "Dune 4K",
		OfficialRating:  "PG-13",
		CommunityRating: &r85,
		ProductionYear:  &y2022,
	}
	mSD := &sqlite.ItemRecord{
		ID:              guid.New(),
		LibraryID:       lib.ID,
		ParentID:        lib.ID,
		Type:            "Movie",
		MediaType:       "Video",
		Name:            "Old Classic SD",
		OfficialRating:  "G",
		CommunityRating: &r60,
		ProductionYear:  &y2020,
	}
	mR := &sqlite.ItemRecord{
		ID:              guid.New(),
		LibraryID:       lib.ID,
		ParentID:        lib.ID,
		Type:            "Movie",
		MediaType:       "Video",
		Name:            "Rated R Film",
		OfficialRating:  "R",
		CommunityRating: &r85,
		ProductionYear:  &y2022,
	}

	for _, it := range []*sqlite.ItemRecord{m4k, mSD, mR} {
		if err := repo.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
		_ = repo.ReplaceItemAncestors(ctx, it.ID, []sqlite.AncestorLink{{AncestorID: lib.ID, Depth: 1}})
	}
	_ = repo.ReplaceItemTaxonomies(ctx, m4k.ID, "genre", []string{"Sci-Fi"})
	_ = repo.ReplaceItemTaxonomies(ctx, m4k.ID, "studio", []string{"Legendary"})
	_ = repo.ReplaceItemTaxonomies(ctx, mSD.ID, "genre", []string{"Drama"})
	_ = repo.ReplaceItemTaxonomies(ctx, mSD.ID, "studio", []string{"ClassicCo"})
	_ = repo.ReplaceItemTaxonomies(ctx, mR.ID, "genre", []string{"Horror"})
	_ = repo.ReplaceItemTaxonomies(ctx, mR.ID, "studio", []string{"BloodStudio"})

	_ = repo.ReplaceMediaSourcesAndStreams(ctx, m4k.ID, []sqlite.MediaSourceRecord{
		{
			ItemID: m4k.ID,
			Name:   "dune.mkv",
			Path:   "/m/dune.mkv",
			Streams: []sqlite.MediaStreamRecord{
				{StreamIndex: 0, StreamType: "Video", Codec: "hevc", Width: 3840, Height: 2160},
				{StreamIndex: 1, StreamType: "Subtitle", Codec: "srt", Language: "chi"},
			},
		},
	})
	_ = repo.ReplaceMediaSourcesAndStreams(ctx, mSD.ID, []sqlite.MediaSourceRecord{
		{
			ItemID: mSD.ID,
			Name:   "sd.mkv",
			Path:   "/m/sd.mkv",
			Streams: []sqlite.MediaStreamRecord{
				{StreamIndex: 0, StreamType: "Video", Codec: "h264", Width: 640, Height: 480},
			},
		},
	})

	// Lookup genre & studio IDs for Sci-Fi and Legendary
	genres, _, _ := repo.ListTaxonomies(ctx, cfg.ServerID, "genre", "", 0, 50)
	var sciFiID string
	for _, g := range genres {
		if g.Name == "Sci-Fi" {
			sciFiID = g.Id
		}
	}
	studios, _, _ := repo.ListTaxonomies(ctx, cfg.ServerID, "studio", "", 0, 50)
	var legendaryID string
	for _, st := range studios {
		if st.Name == "Legendary" {
			legendaryID = st.Id
		}
	}

	mux := http.NewServeMux()
	h := New(repo, nil, cfg)
	h.SetUserRepo(userRepo)
	h.RegisterRoutes(mux, AuthFuncs{
		UserIDFromCtx: func(c context.Context) string {
			if ac, ok := middleware.AuthFromContext(c); ok && ac != nil {
				return ac.UserID()
			}
			return ""
		},
	})

	queryItems := func(rawURL string) dto.QueryResult[dto.BaseItemDto] {
		t.Helper()
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, rawURL, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", rawURL, rec.Code, rec.Body.String())
		}
		var qr dto.QueryResult[dto.BaseItemDto]
		if err := json.Unmarshal(rec.Body.Bytes(), &qr); err != nil {
			t.Fatal(err)
		}
		return qr
	}

	if qr := queryItems("/Items?Recursive=true&GenreIds=" + sciFiID); qr.TotalRecordCount != 1 || qr.Items[0].Id != m4k.ID {
		t.Fatalf("GenreIds filter got %+v", qr)
	}
	if qr := queryItems("/Items?Recursive=true&StudioIds=" + legendaryID); qr.TotalRecordCount != 1 || qr.Items[0].Id != m4k.ID {
		t.Fatalf("StudioIds filter got %+v", qr)
	}
	if qr := queryItems("/Items?Recursive=true&Studios=ClassicCo"); qr.TotalRecordCount != 1 || qr.Items[0].Id != mSD.ID {
		t.Fatalf("Studios filter got %+v", qr)
	}
	if qr := queryItems("/Items?Recursive=true&OfficialRatings=PG-13"); qr.TotalRecordCount != 1 || qr.Items[0].Id != m4k.ID {
		t.Fatalf("OfficialRatings filter got %+v", qr)
	}
	if qr := queryItems("/Items?Recursive=true&MinCommunityRating=8.0"); qr.TotalRecordCount != 2 {
		t.Fatalf("MinCommunityRating=8.0 expected 2, got %d", qr.TotalRecordCount)
	}
	if qr := queryItems("/Items?Recursive=true&Ids=" + m4k.ID + "," + mSD.ID); qr.TotalRecordCount != 2 {
		t.Fatalf("Ids filter expected 2, got %d", qr.TotalRecordCount)
	}
	if qr := queryItems("/Items?Recursive=true&ExcludeItemIds=" + m4k.ID + "," + mR.ID); qr.TotalRecordCount != 1 || qr.Items[0].Id != mSD.ID {
		t.Fatalf("ExcludeItemIds filter got %+v", qr)
	}
	if qr := queryItems("/Items?Recursive=true&IsHD=true"); qr.TotalRecordCount != 1 || qr.Items[0].Id != m4k.ID {
		t.Fatalf("IsHD=true got %+v", qr)
	}
	if qr := queryItems("/Items?Recursive=true&Is4K=true"); qr.TotalRecordCount != 1 || qr.Items[0].Id != m4k.ID {
		t.Fatalf("Is4K=true got %+v", qr)
	}
	if qr := queryItems("/Items?Recursive=true&HasSubtitles=true"); qr.TotalRecordCount != 1 || qr.Items[0].Id != m4k.ID {
		t.Fatalf("HasSubtitles=true got %+v", qr)
	}

	// Verify /Genres, /Studios, /Years respect parental controls
	maxG := 1
	kidPolicy := sqlite.DefaultUserPolicy(false)
	kidPolicy.MaxParentalRating = &maxG
	kid := &sqlite.UserRecord{
		ID:     guid.New(),
		Name:   "kid_tax",
		Policy: kidPolicy,
		Config: sqlite.DefaultUserConfig(),
	}
	_ = userRepo.CreateUser(ctx, kid)
	kidAC := &middleware.AuthContext{User: kid}

	for _, ep := range []struct {
		url      string
		wantName string
		forbid   string
	}{
		{"/Genres", "Drama", "Horror"},
		{"/Studios", "ClassicCo", "BloodStudio"},
		{"/Years", "2020", "2022"},
	} {
		req := httptest.NewRequest(http.MethodGet, ep.url, nil)
		req = req.WithContext(middleware.WithAuthContext(req.Context(), kidAC))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		var qr dto.QueryResult[dto.BaseItemDto]
		_ = json.Unmarshal(rec.Body.Bytes(), &qr)
		foundWant := false
		for _, it := range qr.Items {
			if it.Name == ep.wantName {
				foundWant = true
			}
			if it.Name == ep.forbid {
				t.Fatalf("GET %s leaked blocked taxonomy %q", ep.url, ep.forbid)
			}
		}
		if !foundWant {
			t.Fatalf("GET %s missing expected %q in %+v", ep.url, ep.wantName, qr.Items)
		}
	}
}
