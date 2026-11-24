package users

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestReportPlaybackStartHydratesSessionEndpoint(t *testing.T) {
	env := setupUsersEnv(t)
	ctx := context.Background()

	libRepo := sqlite.NewLibraryRepo(env.store)
	lib, err := libRepo.CreateLibrary(ctx, "Movies", "movies", []string{"/movies"})
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}
	movieID := guid.New()
	rt := int64(72_000_000_000)
	yr := 2010
	if err := libRepo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:             movieID,
		LibraryID:      lib.ID,
		ParentID:       lib.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Inception",
		SortName:       "inception",
		ProductionYear: &yr,
		RuntimeTicks:   &rt,
		Container:      "mp4",
		Path:           "/movies/Inception.mp4",
		Etag:           "m1",
	}); err != nil {
		t.Fatalf("UpsertItem: %v", err)
	}

	// POST /Sessions/Playing with only ItemId
	body, _ := json.Marshal(map[string]any{
		"ItemId":        movieID,
		"PositionTicks": 5000,
	})
	req := httptest.NewRequest(http.MethodPost, "/Sessions/Playing", bytes.NewReader(body))
	req.Header.Set("X-Emby-Token", env.adminTok)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST /Sessions/Playing: got %d (%s)", rec.Code, rec.Body.String())
	}

	// GET /Sessions should return hydrated NowPlayingItem
	req = httptest.NewRequest(http.MethodGet, "/Sessions", nil)
	req.Header.Set("X-Emby-Token", env.adminTok)
	rec = httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /Sessions: got %d", rec.Code)
	}
	var sessions []dto.SessionInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &sessions); err != nil {
		t.Fatalf("unmarshal sessions: %v", err)
	}
	found := false
	for _, s := range sessions {
		if s.NowPlayingItem != nil && s.NowPlayingItem.Id == movieID {
			found = true
			if s.NowPlayingItem.Name != "Inception" || s.NowPlayingItem.Type != "Movie" {
				t.Fatalf("expected hydrated Inception Movie, got %+v", s.NowPlayingItem)
			}
			if s.NowPlayingItem.RunTimeTicks == nil || *s.NowPlayingItem.RunTimeTicks != rt {
				t.Fatalf("expected RunTimeTicks=%d, got %v", rt, s.NowPlayingItem.RunTimeTicks)
			}
		}
	}
	if !found {
		t.Fatalf("expected hydrated NowPlayingItem for %s in %+v", movieID, sessions)
	}
}
