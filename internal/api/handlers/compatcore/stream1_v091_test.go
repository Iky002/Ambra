package compatcore

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/api/ws"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

type captureWSHub struct {
	mu   sync.Mutex
	msgs []ws.Message
}

func (c *captureWSHub) SendToUserDevice(_, _ string, msg ws.Message) {
	c.mu.Lock()
	c.msgs = append(c.msgs, msg)
	c.mu.Unlock()
}

func TestUserItemExtrasAndLibraryRBAC(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	libAllowed, err := env.libRepo.CreateLibrary(ctx, "AllowedMovies", "movies", []string{filepath.Join(t.TempDir(), "a")})
	if err != nil {
		t.Fatal(err)
	}
	libDenied, err := env.libRepo.CreateLibrary(ctx, "DeniedMovies", "movies", []string{filepath.Join(t.TempDir(), "b")})
	if err != nil {
		t.Fatal(err)
	}

	seedMovie := func(libID, name string) string {
		m := &sqlite.ItemRecord{
			ID: guid.New(), LibraryID: libID, ParentID: libID,
			Type: "Movie", MediaType: "Video", Name: name, SortName: strings.ToLower(name),
		}
		if err := env.libRepo.UpsertItem(ctx, m); err != nil {
			t.Fatal(err)
		}
		extras := []struct{ name, extraType string }{
			{name + " Trailer", "Trailer"},
			{name + " Making Of", "Featurette"},
			{name + " Deleted", "DeletedScene"},
		}
		for _, ex := range extras {
			rec := &sqlite.ItemRecord{
				ID: guid.New(), LibraryID: libID, ParentID: m.ID,
				Type: "Video", MediaType: "Video", Name: ex.name, SortName: strings.ToLower(ex.name),
				Extra: sqlite.ItemExtra{ExtraType: ex.extraType},
			}
			if err := env.libRepo.UpsertItem(ctx, rec); err != nil {
				t.Fatal(err)
			}
		}
		return m.ID
	}

	allowedMovieID := seedMovie(libAllowed.ID, "Allowed")
	deniedMovieID := seedMovie(libDenied.ID, "Denied")

	pol := sqlite.DefaultUserPolicy(false)
	pol.EnableAllFolders = false
	pol.EnabledFolders = []string{libAllowed.ID}
	restricted := &sqlite.UserRecord{
		ID: guid.New(), Name: "restricted", Policy: pol, Config: sqlite.DefaultUserConfig(),
	}
	if err := env.userRepo.CreateUser(ctx, restricted); err != nil {
		t.Fatal(err)
	}

	getAs := func(u *sqlite.UserRecord, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req = req.WithContext(middleware.WithAuthContext(req.Context(), &middleware.AuthContext{User: u}))
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)
		return rec
	}

	// 1. Allowed library -> LocalTrailers returns 1 trailer, SpecialFeatures returns 2 features.
	rec := getAs(restricted, "/Users/"+restricted.ID+"/Items/"+allowedMovieID+"/LocalTrailers")
	if rec.Code != http.StatusOK {
		t.Fatalf("allowed LocalTrailers got %d body=%s", rec.Code, rec.Body.String())
	}
	var trailers []dto.BaseItemDto
	if err := json.Unmarshal(rec.Body.Bytes(), &trailers); err != nil || len(trailers) != 1 || trailers[0].ExtraType != "Trailer" {
		t.Fatalf("unexpected LocalTrailers: %+v err=%v", trailers, err)
	}

	rec = getAs(restricted, "/Users/"+restricted.ID+"/Items/"+allowedMovieID+"/SpecialFeatures")
	if rec.Code != http.StatusOK {
		t.Fatalf("allowed SpecialFeatures got %d body=%s", rec.Code, rec.Body.String())
	}
	var specials []dto.BaseItemDto
	if err := json.Unmarshal(rec.Body.Bytes(), &specials); err != nil || len(specials) != 2 {
		t.Fatalf("unexpected SpecialFeatures: %+v err=%v", specials, err)
	}

	// 2. Denied library -> 403 Forbidden (even if trying to spoof admin UserId in path).
	for _, ep := range []string{
		"/Users/" + restricted.ID + "/Items/" + deniedMovieID + "/LocalTrailers",
		"/Users/" + restricted.ID + "/Items/" + deniedMovieID + "/SpecialFeatures",
		"/Users/" + env.admin.ID + "/Items/" + deniedMovieID + "/LocalTrailers",
	} {
		if r := getAs(restricted, ep); r.Code != http.StatusForbidden {
			t.Fatalf("GET %s as restricted user: got %d, want 403", ep, r.Code)
		}
	}

	// 3. Nonexistent item -> 404 Not Found.
	missingID := guid.New()
	if r := getAs(env.admin, "/Users/"+env.admin.ID+"/Items/"+missingID+"/LocalTrailers"); r.Code != http.StatusNotFound {
		t.Fatalf("missing item LocalTrailers: got %d, want 404", r.Code)
	}
	if r := getAs(env.admin, "/Users/"+env.admin.ID+"/Items/"+missingID+"/SpecialFeatures"); r.Code != http.StatusNotFound {
		t.Fatalf("missing item SpecialFeatures: got %d, want 404", r.Code)
	}
}

func TestSessionPlayingCommandSeekPositionTicksInt64(t *testing.T) {
	env := newTestEnv(t)
	hub := &captureWSHub{}
	env.handler.SetWSHub(hub)

	sess := env.sessions.TouchSession(env.admin, auth.ClientInfo{
		DeviceID: "tv-device-1",
		Device:   "Living Room TV",
		Client:   "EmbyTV",
		Version:  "1.0",
	})

	rec := doReq(t, env.router, http.MethodPost, "/Sessions/"+sess.Id+"/Playing/Seek?SeekPositionTicks=123450000", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST Seek got %d body=%s", rec.Code, rec.Body.String())
	}

	hub.mu.Lock()
	if len(hub.msgs) != 1 {
		hub.mu.Unlock()
		t.Fatalf("expected 1 WS message, got %d", len(hub.msgs))
	}
	msg := hub.msgs[0]
	hub.mu.Unlock()

	if msg.MessageType != "Playstate" {
		t.Fatalf("expected Playstate WS message, got %q", msg.MessageType)
	}
	payload, ok := msg.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map payload, got %T", msg.Data)
	}
	ticksVal, ok := payload["SeekPositionTicks"].(int64)
	if !ok || ticksVal != 123450000 {
		t.Fatalf("expected SeekPositionTicks int64(123450000), got %T(%v)", payload["SeekPositionTicks"], payload["SeekPositionTicks"])
	}

	// Invalid non-numeric SeekPositionTicks should not inject a string.
	recBad := doReq(t, env.router, http.MethodPost, "/Sessions/"+sess.Id+"/Playing/Seek?SeekPositionTicks=notanumber", "")
	if recBad.Code != http.StatusNoContent {
		t.Fatalf("POST invalid Seek got %d", recBad.Code)
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if len(hub.msgs) != 2 {
		t.Fatalf("expected 2 WS messages, got %d", len(hub.msgs))
	}
	badPayload, _ := hub.msgs[1].Data.(map[string]any)
	if _, exists := badPayload["SeekPositionTicks"]; exists {
		t.Fatalf("expected invalid SeekPositionTicks to be omitted, got %T(%v)", badPayload["SeekPositionTicks"], badPayload["SeekPositionTicks"])
	}
}
