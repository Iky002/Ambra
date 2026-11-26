package trakt

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

type capturedRequest struct {
	Method  string
	Path    string
	Headers http.Header
	Body    map[string]any
}

func newMockTraktServer(t *testing.T, status int, respBody string) (*httptest.Server, chan capturedRequest) {
	t.Helper()
	ch := make(chan capturedRequest, 32)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		ch <- capturedRequest{
			Method:  r.Method,
			Path:    r.URL.Path,
			Headers: r.Header.Clone(),
			Body:    body,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if respBody != "" {
			_, _ = w.Write([]byte(respBody))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, ch
}

func openTestLibraryRepo(t *testing.T) *sqlite.LibraryRepo {
	t.Helper()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "trakt_test.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return sqlite.NewLibraryRepo(store)
}

func intPtr(v int) *int       { return &v }
func int64Ptr(v int64) *int64 { return &v }

func waitReq(t *testing.T, ch <-chan capturedRequest) capturedRequest {
	t.Helper()
	select {
	case req := <-ch:
		return req
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for Trakt HTTP request")
		return capturedRequest{}
	}
}

func assertNoReq(t *testing.T, ch <-chan capturedRequest, d time.Duration) {
	t.Helper()
	select {
	case req := <-ch:
		t.Fatalf("expected no Trakt request, got %s %s", req.Method, req.Path)
	case <-time.After(d):
	}
}

func assertTraktHeaders(t *testing.T, h http.Header, clientID, token string) {
	t.Helper()
	if got := h.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if got := h.Get("trakt-api-version"); got != "2" {
		t.Fatalf("trakt-api-version = %q, want 2", got)
	}
	if got := h.Get("trakt-api-key"); got != clientID {
		t.Fatalf("trakt-api-key = %q, want %q", got, clientID)
	}
	if got := h.Get("Authorization"); got != "Bearer "+token {
		t.Fatalf("Authorization = %q, want %q", got, "Bearer "+token)
	}
}

func TestManifestDefaults(t *testing.T) {
	p := New()
	m := p.Manifest()
	if m.ID != "ambra.builtin.trakt" {
		t.Fatalf("ID = %q", m.ID)
	}
	if m.Name != "Trakt Scrobble & Sync" {
		t.Fatalf("Name = %q", m.Name)
	}
	if m.Version != "1.0.0" {
		t.Fatalf("Version = %q", m.Version)
	}
	if m.Author != "Ambra" {
		t.Fatalf("Author = %q", m.Author)
	}
	if !m.BuiltIn {
		t.Fatal("expected BuiltIn=true")
	}
	if m.Enabled {
		t.Fatal("expected Enabled=false by default")
	}
	if m.Priority != 40 {
		t.Fatalf("Priority = %d, want 40", m.Priority)
	}
	if len(m.Kinds) != 1 || m.Kinds[0] != plugins.KindNotifier {
		t.Fatalf("Kinds = %v, want [notifier]", m.Kinds)
	}
}

func TestConfigureAndConcurrency(t *testing.T) {
	p := New()

	// nil cfg is a no-op
	if err := p.Configure(nil); err != nil {
		t.Fatalf("Configure(nil): %v", err)
	}

	// Test alternate key aliases
	if err := p.Configure(map[string]any{
		"Enabled":          true,
		"ClientId":         "  cid-1  ",
		"AccessToken":      "  tok-1  ",
		"ScrobblePlayback": false,
		"SyncWatched":      false,
		"Priority":         float64(25),
		"base_url":         "https://example.trakt.local/",
	}); err != nil {
		t.Fatalf("Configure aliases: %v", err)
	}
	m := p.Manifest()
	if !m.Enabled || m.Priority != 25 {
		t.Fatalf("unexpected manifest after Configure: %+v", m)
	}
	snap := p.snapshot()
	if snap.clientID != "cid-1" || snap.accessToken != "tok-1" || snap.scrobblePlayback || snap.syncWatched || snap.baseURL != "https://example.trakt.local" {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}

	// Test trakt_* and snake_case keys + int/int64 priority
	if err := p.Configure(map[string]any{
		"enabled":           true,
		"trakt_client_id":   "cid-2",
		"trakt_access_token": "tok-2",
		"scrobble_playback": true,
		"sync_watched":      true,
		"priority":          int64(55),
	}); err != nil {
		t.Fatalf("Configure snake_case: %v", err)
	}
	if p.Manifest().Priority != 55 {
		t.Fatalf("Priority = %d, want 55", p.Manifest().Priority)
	}

	// Concurrent Configure + Manifest + snapshot under -race
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(2)
		idx := i
		go func() {
			defer wg.Done()
			_ = p.Configure(map[string]any{
				"enabled":      idx%2 == 0,
				"client_id":    "cid",
				"access_token": "tok",
				"priority":     idx,
			})
		}()
		go func() {
			defer wg.Done()
			_ = p.Manifest()
			_ = p.snapshot()
		}()
	}
	wg.Wait()
}

func TestMovieScrobbleAndSyncWatched(t *testing.T) {
	srv, ch := newMockTraktServer(t, http.StatusCreated, `{"id":1}`)
	p := New()
	p.SetHTTPClient(srv.Client())
	_ = p.Configure(map[string]any{
		"enabled":      true,
		"client_id":    "my-client-id",
		"access_token": "my-access-token",
		"base_url":     srv.URL,
	})

	movie := &sqlite.ItemRecord{
		ID:             "mov-1",
		Type:           "Movie",
		Name:           "Inception",
		ProductionYear: intPtr(2010),
		Extra: sqlite.ItemExtra{
			ProviderIDs: map[string]string{
				"Tmdb": "27205",
				"Imdb": "tt1375666",
				"Tvdb": "999",
			},
		},
	}

	ctx := context.Background()

	// Start with negative progress -> clamped to 0
	if err := p.ScrobbleStart(ctx, movie, nil, -15.5); err != nil {
		t.Fatalf("ScrobbleStart: %v", err)
	}
	req := waitReq(t, ch)
	if req.Method != http.MethodPost || req.Path != "/scrobble/start" {
		t.Fatalf("unexpected start request: %s %s", req.Method, req.Path)
	}
	assertTraktHeaders(t, req.Headers, "my-client-id", "my-access-token")
	if req.Body["progress"] != float64(0) {
		t.Fatalf("progress = %v, want 0", req.Body["progress"])
	}
	mObj := req.Body["movie"].(map[string]any)
	if mObj["title"] != "Inception" || mObj["year"] != float64(2010) {
		t.Fatalf("movie payload = %v", mObj)
	}
	ids := mObj["ids"].(map[string]any)
	if ids["tmdb"] != float64(27205) || ids["imdb"] != "tt1375666" || ids["tvdb"] != float64(999) {
		t.Fatalf("movie ids = %v", ids)
	}

	// Pause at 42.5%
	if err := p.ScrobblePause(ctx, movie, nil, 42.5); err != nil {
		t.Fatalf("ScrobblePause: %v", err)
	}
	req = waitReq(t, ch)
	if req.Path != "/scrobble/pause" || req.Body["progress"] != 42.5 {
		t.Fatalf("unexpected pause: path=%s body=%v", req.Path, req.Body)
	}

	// Stop at >100% -> clamped to 100
	if err := p.ScrobbleStop(ctx, movie, nil, 150.0); err != nil {
		t.Fatalf("ScrobbleStop: %v", err)
	}
	req = waitReq(t, ch)
	if req.Path != "/scrobble/stop" || req.Body["progress"] != float64(100) {
		t.Fatalf("unexpected stop: path=%s body=%v", req.Path, req.Body)
	}

	// NaN progress -> clamped to 0
	if got := clampProgress(math.NaN()); got != 0 {
		t.Fatalf("clampProgress(NaN) = %v, want 0", got)
	}

	// SyncWatched -> POST /sync/history
	if err := p.SyncWatched(ctx, movie, nil); err != nil {
		t.Fatalf("SyncWatched: %v", err)
	}
	req = waitReq(t, ch)
	if req.Path != "/sync/history" {
		t.Fatalf("Path = %q, want /sync/history", req.Path)
	}
	movies := req.Body["movies"].([]any)
	if len(movies) != 1 {
		t.Fatalf("movies len = %d", len(movies))
	}
	firstMovie := movies[0].(map[string]any)
	if firstMovie["title"] != "Inception" {
		t.Fatalf("history movie = %v", firstMovie)
	}
}

func TestEpisodeScrobbleAndSyncWatched(t *testing.T) {
	srv, ch := newMockTraktServer(t, http.StatusCreated, `{}`)
	p := New()
	p.SetHTTPClient(srv.Client())
	_ = p.Configure(map[string]any{
		"enabled":      true,
		"client_id":    "cid",
		"access_token": "tok",
		"base_url":     srv.URL,
	})

	series := &sqlite.ItemRecord{
		ID:             "show-1",
		Type:           "Series",
		Name:           "Breaking Bad",
		ProductionYear: intPtr(2008),
		Extra: sqlite.ItemExtra{
			ProviderIDs: map[string]string{
				"tmdb": "1396",
				"imdb": "tt0903747",
				"tvdb": "81189",
			},
		},
	}
	ep := &sqlite.ItemRecord{
		ID:                "ep-1",
		SeriesID:          "show-1",
		Type:              "Episode",
		Name:              "Pilot",
		ParentIndexNumber: intPtr(1),
		IndexNumber:       intPtr(1),
		Extra: sqlite.ItemExtra{
			SeriesName: "Breaking Bad",
			ProviderIDs: map[string]string{
				"tmdb": "62085",
				"tvdb": "349232",
			},
		},
	}

	ctx := context.Background()
	if err := p.ScrobbleStart(ctx, ep, series, 12.5); err != nil {
		t.Fatalf("ScrobbleStart episode: %v", err)
	}
	req := waitReq(t, ch)
	if req.Path != "/scrobble/start" {
		t.Fatalf("Path = %q", req.Path)
	}
	showObj := req.Body["show"].(map[string]any)
	if showObj["title"] != "Breaking Bad" || showObj["year"] != float64(2008) {
		t.Fatalf("show payload = %v", showObj)
	}
	showIDs := showObj["ids"].(map[string]any)
	if showIDs["tvdb"] != float64(81189) || showIDs["tmdb"] != float64(1396) || showIDs["imdb"] != "tt0903747" {
		t.Fatalf("show ids = %v", showIDs)
	}
	epObj := req.Body["episode"].(map[string]any)
	if epObj["season"] != float64(1) || epObj["number"] != float64(1) {
		t.Fatalf("episode payload = %v", epObj)
	}
	epIDs := epObj["ids"].(map[string]any)
	if epIDs["tmdb"] != float64(62085) || epIDs["tvdb"] != float64(349232) {
		t.Fatalf("episode ids = %v", epIDs)
	}

	// SyncWatched for episode -> /sync/history with shows -> seasons -> episodes
	if err := p.SyncWatched(ctx, ep, series); err != nil {
		t.Fatalf("SyncWatched episode: %v", err)
	}
	req = waitReq(t, ch)
	if req.Path != "/sync/history" {
		t.Fatalf("Path = %q", req.Path)
	}
	shows := req.Body["shows"].([]any)
	if len(shows) != 1 {
		t.Fatalf("shows len = %d", len(shows))
	}
	hShow := shows[0].(map[string]any)
	seasons := hShow["seasons"].([]any)
	s0 := seasons[0].(map[string]any)
	eps := s0["episodes"].([]any)
	e0 := eps[0].(map[string]any)
	if s0["number"] != float64(1) || e0["number"] != float64(1) {
		t.Fatalf("unexpected history season/episode: %v", hShow)
	}

	// Episode with nil series falls back to item.Extra.SeriesName
	if err := p.ScrobblePause(ctx, ep, nil, 50); err != nil {
		t.Fatalf("ScrobblePause nil series: %v", err)
	}
	req = waitReq(t, ch)
	if req.Body["show"].(map[string]any)["title"] != "Breaking Bad" {
		t.Fatalf("expected fallback series name, got %v", req.Body["show"])
	}
}

func TestNoOpWhenDisabledOrMissingCredsOrUnsupportedType(t *testing.T) {
	srv, ch := newMockTraktServer(t, http.StatusOK, `{}`)
	p := New()
	p.SetHTTPClient(srv.Client())
	ctx := context.Background()
	movie := &sqlite.ItemRecord{ID: "m1", Type: "Movie", Name: "Test"}

	// Disabled by default
	if err := p.ScrobbleStart(ctx, movie, nil, 10); err != nil {
		t.Fatalf("unexpected error when disabled: %v", err)
	}
	if err := p.SyncWatched(ctx, movie, nil); err != nil {
		t.Fatalf("unexpected error when disabled: %v", err)
	}

	// Enabled but missing creds
	_ = p.Configure(map[string]any{"enabled": true, "base_url": srv.URL})
	if err := p.ScrobbleStart(ctx, movie, nil, 10); err != nil {
		t.Fatalf("unexpected error with missing creds: %v", err)
	}

	// Creds present, but scrobble_playback=false and sync_watched=false
	_ = p.Configure(map[string]any{
		"client_id":         "cid",
		"access_token":      "tok",
		"scrobble_playback": false,
		"sync_watched":      false,
	})
	_ = p.ScrobbleStart(ctx, movie, nil, 10)
	_ = p.SyncWatched(ctx, movie, nil)

	// Enable both, test nil item and unsupported item type (Audio)
	_ = p.Configure(map[string]any{
		"scrobble_playback": true,
		"sync_watched":      true,
	})
	_ = p.ScrobbleStart(ctx, nil, nil, 10)
	_ = p.SyncWatched(ctx, nil, nil)
	audio := &sqlite.ItemRecord{ID: "a1", Type: "Audio", Name: "Song"}
	_ = p.ScrobbleStart(ctx, audio, nil, 10)
	_ = p.SyncWatched(ctx, audio, nil)

	assertNoReq(t, ch, 100*time.Millisecond)
}

func TestUpstreamHTTPError(t *testing.T) {
	srv, _ := newMockTraktServer(t, http.StatusUnauthorized, `{"error":"invalid_token"}`)
	p := New()
	p.SetHTTPClient(srv.Client())
	_ = p.Configure(map[string]any{
		"enabled":      true,
		"client_id":    "cid",
		"access_token": "expired",
		"base_url":     srv.URL,
	})
	movie := &sqlite.ItemRecord{ID: "m1", Type: "Movie", Name: "Matrix"}

	err := p.ScrobbleStart(context.Background(), movie, nil, 5)
	if err == nil || !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "invalid_token") {
		t.Fatalf("expected 401 error with body, got: %v", err)
	}
}

func TestSubscribeEventsIntegration(t *testing.T) {
	srv, ch := newMockTraktServer(t, http.StatusCreated, `{}`)
	repo := openTestLibraryRepo(t)
	ctx := context.Background()

	lib, err := repo.CreateLibrary(ctx, "Media", "tvshows", []string{"/media"})
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}

	movie := &sqlite.ItemRecord{
		ID:             "movie-100",
		LibraryID:      lib.ID,
		ParentID:       lib.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Interstellar",
		ProductionYear: intPtr(2014),
		RuntimeTicks:   int64Ptr(10_000_000_000),
		Extra: sqlite.ItemExtra{
			ProviderIDs: map[string]string{"tmdb": "157336", "imdb": "tt0816692"},
		},
	}
	series := &sqlite.ItemRecord{
		ID:             "series-200",
		LibraryID:      lib.ID,
		ParentID:       lib.ID,
		Type:           "Series",
		Name:           "Severance",
		ProductionYear: intPtr(2022),
		Extra: sqlite.ItemExtra{
			ProviderIDs: map[string]string{"tmdb": "95396", "tvdb": "371980"},
		},
	}
	ep := &sqlite.ItemRecord{
		ID:                "ep-201",
		LibraryID:         lib.ID,
		ParentID:          series.ID,
		SeriesID:          series.ID,
		Type:              "Episode",
		MediaType:         "Video",
		Name:              "Good News About Hell",
		ParentIndexNumber: intPtr(1),
		IndexNumber:       intPtr(1),
		RuntimeTicks:      int64Ptr(3_600_000_000),
		Extra: sqlite.ItemExtra{
			ProviderIDs: map[string]string{"tmdb": "2545677"},
		},
	}
	for _, rec := range []*sqlite.ItemRecord{movie, series, ep} {
		if err := repo.UpsertItem(ctx, rec); err != nil {
			t.Fatalf("UpsertItem %s: %v", rec.ID, err)
		}
	}

	bus := eventbus.New()
	p := New()
	p.SetHTTPClient(srv.Client())
	// Nil bus returns safe no-op unsubscribe
	p.SubscribeEvents(nil, repo)()

	unsub := p.SubscribeEvents(bus, repo)

	// While disabled, events are skipped immediately
	bus.Publish(ctx, eventbus.Event{
		Type: eventbus.EventPlaybackStart,
		Payload: session.PlaybackEventPayload{
			ItemId:        movie.ID,
			PositionTicks: 1_000_000_000,
		},
	})
	assertNoReq(t, ch, 100*time.Millisecond)

	// Enable plugin (explicitly bound to user u1)
	_ = p.Configure(map[string]any{
		"enabled":      true,
		"client_id":    "bus-cid",
		"access_token": "bus-tok",
		"base_url":     srv.URL,
		"user_id":      "u1",
	})

	// 1. EventPlaybackStart (session.PlaybackEventPayload) for Movie at 25%
	bus.Publish(ctx, eventbus.Event{
		Type: eventbus.EventPlaybackStart,
		Payload: session.PlaybackEventPayload{
			UserId:        "u1",
			ItemId:        movie.ID,
			PositionTicks: 2_500_000_000,
		},
	})
	req := waitReq(t, ch)
	if req.Path != "/scrobble/start" || req.Body["progress"] != float64(25) {
		t.Fatalf("start event: path=%s body=%v", req.Path, req.Body)
	}
	assertTraktHeaders(t, req.Headers, "bus-cid", "bus-tok")

	// 2. EventPlaybackProgress paused=true (session.PlaybackEventPayload) for Episode at 50%
	bus.Publish(ctx, eventbus.Event{
		Type: eventbus.EventPlaybackProgress,
		Payload: session.PlaybackEventPayload{
			UserId:        "u1",
			ItemId:        ep.ID,
			PositionTicks: 1_800_000_000,
			RuntimeTicks:  3_600_000_000,
			IsPaused:      true,
		},
	})
	req = waitReq(t, ch)
	if req.Path != "/scrobble/pause" || req.Body["progress"] != float64(50) {
		t.Fatalf("pause event: path=%s body=%v", req.Path, req.Body)
	}
	// Verify parent series was resolved from SQLite
	showObj := req.Body["show"].(map[string]any)
	if showObj["title"] != "Severance" || showObj["ids"].(map[string]any)["tvdb"] != float64(371980) {
		t.Fatalf("expected resolved parent series, got %v", showObj)
	}

	// Non-paused progress should not trigger a request
	bus.Publish(ctx, eventbus.Event{
		Type: eventbus.EventPlaybackProgress,
		Payload: map[string]any{
			"user_id":        "u1",
			"item_id":        ep.ID,
			"position_ticks": int64(2_000_000_000),
			"is_paused":      false,
		},
	})
	assertNoReq(t, ch, 100*time.Millisecond)

	// 3. EventPlaybackStop with Played=true and PositionTicks=0 -> 100%
	bus.Publish(ctx, eventbus.Event{
		Type: eventbus.EventPlaybackStop,
		Payload: &session.PlaybackEventPayload{
			UserId:        "u1",
			ItemId:        movie.ID,
			PositionTicks: 0,
			Played:        true,
		},
	})
	req = waitReq(t, ch)
	if req.Path != "/scrobble/stop" || req.Body["progress"] != float64(100) {
		t.Fatalf("stop event: path=%s body=%v", req.Path, req.Body)
	}

	// 4. EventUserDataChanged with Played=true (session.UserDataChangedPayload) for Episode
	bus.Publish(ctx, eventbus.Event{
		Type: eventbus.EventUserDataChanged,
		Payload: session.UserDataChangedPayload{
			UserId: "u1",
			UserDataList: []session.UserDataChangedEntry{
				{ItemId: movie.ID, Played: false}, // ignored
				{ItemId: ep.ID, Played: true},     // synced
			},
		},
	})
	req = waitReq(t, ch)
	if req.Path != "/sync/history" {
		t.Fatalf("user data changed path = %q, want /sync/history", req.Path)
	}
	shows := req.Body["shows"].([]any)
	if len(shows) != 1 || shows[0].(map[string]any)["title"] != "Severance" {
		t.Fatalf("unexpected sync history shows: %v", shows)
	}

	// 5. EventUserDataChanged as map[string]any direct payload for movie.ID right after
	//    /scrobble/stop >= 80% is deduplicated (no double-counting).
	bus.Publish(ctx, eventbus.Event{
		Type: eventbus.EventUserDataChanged,
		Payload: map[string]any{
			"user_id": "u1",
			"item_id": movie.ID,
			"played":  true,
		},
	})
	assertNoReq(t, ch, 120*time.Millisecond)

	// A separate movie marked watched via map payload still syncs to /sync/history.
	movie2 := &sqlite.ItemRecord{
		ID:             "movie-101",
		LibraryID:      lib.ID,
		ParentID:       lib.ID,
		Type:           "Movie",
		MediaType:      "Video",
		Name:           "Tenet",
		ProductionYear: intPtr(2020),
	}
	if err := repo.UpsertItem(ctx, movie2); err != nil {
		t.Fatal(err)
	}
	bus.Publish(ctx, eventbus.Event{
		Type: eventbus.EventUserDataChanged,
		Payload: map[string]any{
			"user_id": "u1",
			"item_id": movie2.ID,
			"played":  true,
		},
	})
	req = waitReq(t, ch)
	if req.Path != "/sync/history" || len(req.Body["movies"].([]any)) != 1 {
		t.Fatalf("map user data changed: path=%s body=%v", req.Path, req.Body)
	}

	// 6. Unsubscribe stops further processing
	unsub()
	bus.Publish(ctx, eventbus.Event{
		Type: eventbus.EventPlaybackStart,
		Payload: session.PlaybackEventPayload{
			ItemId:        movie.ID,
			PositionTicks: 1_000_000_000,
		},
	})
	assertNoReq(t, ch, 100*time.Millisecond)
}

func TestEventPayloadParsersEdgeCases(t *testing.T) {
	// Nil pointers and nil payloads
	if _, ok := parsePlaybackEvent(nil); ok {
		t.Fatal("expected ok=false for nil")
	}
	if _, ok := parsePlaybackEvent((*session.PlaybackEventPayload)(nil)); ok {
		t.Fatal("expected ok=false for nil pointer")
	}
	if got := parseUserDataChangedEvent(nil); len(got) != 0 {
		t.Fatalf("expected empty for nil, got %v", got)
	}
	if got := parseUserDataChangedEvent((*session.UserDataChangedPayload)(nil)); len(got) != 0 {
		t.Fatalf("expected empty for nil pointer, got %v", got)
	}

	// Pointer UserDataChangedPayload
	entries := parseUserDataChangedEvent(&session.UserDataChangedPayload{
		UserId: "u1",
		UserDataList: []session.UserDataChangedEntry{
			{ItemId: "i1", Played: true},
		},
	})
	if len(entries) != 1 || entries[0].itemID != "i1" || !entries[0].played {
		t.Fatalf("unexpected pointer entries: %+v", entries)
	}

	// Custom struct fallback via JSON
	type customPlayback struct {
		ItemID        string `json:"item_id"`
		PositionTicks int64  `json:"position_ticks"`
		RuntimeTicks  int64  `json:"runtime_ticks"`
		IsPaused      bool   `json:"is_paused"`
	}
	ev, ok := parsePlaybackEvent(customPlayback{
		ItemID:        "c1",
		PositionTicks: 500,
		RuntimeTicks:  1000,
		IsPaused:      true,
	})
	if !ok || ev.itemID != "c1" || ev.positionTicks != 500 || ev.runtimeTicks != 1000 || !ev.isPaused {
		t.Fatalf("unexpected custom playback: %+v ok=%v", ev, ok)
	}

	// Custom UserDataChanged struct fallback via JSON
	type customUD struct {
		UserDataList []map[string]any `json:"user_data_list"`
	}
	uEntries := parseUserDataChangedEvent(customUD{
		UserDataList: []map[string]any{
			{"ItemId": "x1", "Played": true},
		},
	})
	if len(uEntries) != 1 || uEntries[0].itemID != "x1" || !uEntries[0].played {
		t.Fatalf("unexpected custom user data entries: %+v", uEntries)
	}

	// computeProgress edge cases
	if got := computeProgress(0, 100, nil); got != 0 {
		t.Fatalf("got %v, want 0", got)
	}
	if got := computeProgress(50, 100, nil); got != 50 {
		t.Fatalf("got %v, want 50", got)
	}
}

func TestTraktMultiUserIsolationPauseThrottleAndDedup(t *testing.T) {
	srv, ch := newMockTraktServer(t, http.StatusCreated, `{}`)
	repo := openTestLibraryRepo(t)
	ctx := context.Background()

	lib, _ := repo.CreateLibrary(ctx, "Movies", "movies", []string{"/media"})
	movie := &sqlite.ItemRecord{
		ID:           "mov-iso",
		LibraryID:    lib.ID,
		Type:         "Movie",
		Name:         "Arrival",
		RuntimeTicks: int64Ptr(10_000_000_000),
	}
	if err := repo.UpsertItem(ctx, movie); err != nil {
		t.Fatal(err)
	}

	bus := eventbus.New()
	p := New()
	p.SetHTTPClient(srv.Client())
	_ = p.Configure(map[string]any{
		"enabled":         true,
		"client_id":       "cid",
		"access_token":    "tok",
		"base_url":        srv.URL,
		"allowed_user_id": "alice",
	})
	unsub := p.SubscribeEvents(bus, repo)
	defer unsub()

	// 1. Events from bob or anonymous user are ignored when allowed_user_id=alice
	bus.Publish(ctx, eventbus.Event{
		Type: eventbus.EventPlaybackStart,
		Payload: session.PlaybackEventPayload{
			UserId:        "bob",
			ItemId:        movie.ID,
			PositionTicks: 1_000_000_000,
		},
	})
	bus.Publish(ctx, eventbus.Event{
		Type: eventbus.EventPlaybackStart,
		Payload: session.PlaybackEventPayload{
			ItemId:        movie.ID,
			PositionTicks: 1_000_000_000,
		},
	})
	assertNoReq(t, ch, 120*time.Millisecond)

	// 2. Alice's pause heartbeats are throttled (only 1 /scrobble/pause sent for repeated paused ticks)
	for i := 0; i < 3; i++ {
		bus.Publish(ctx, eventbus.Event{
			Type: eventbus.EventPlaybackProgress,
			Payload: session.PlaybackEventPayload{
				UserId:        "alice",
				ItemId:        movie.ID,
				PositionTicks: 3_000_000_000,
				IsPaused:      true,
			},
		})
	}
	req := waitReq(t, ch)
	if req.Path != "/scrobble/pause" {
		t.Fatalf("expected /scrobble/pause, got %s", req.Path)
	}
	assertNoReq(t, ch, 120*time.Millisecond)

	// Resuming clears pause throttle so a subsequent pause sends again
	bus.Publish(ctx, eventbus.Event{
		Type: eventbus.EventPlaybackProgress,
		Payload: session.PlaybackEventPayload{
			UserId:        "alice",
			ItemId:        movie.ID,
			PositionTicks: 4_000_000_000,
			IsPaused:      false,
		},
	})
	time.Sleep(40 * time.Millisecond)
	bus.Publish(ctx, eventbus.Event{
		Type: eventbus.EventPlaybackProgress,
		Payload: session.PlaybackEventPayload{
			UserId:        "alice",
			ItemId:        movie.ID,
			PositionTicks: 5_000_000_000,
			IsPaused:      true,
		},
	})
	req = waitReq(t, ch)
	if req.Path != "/scrobble/pause" {
		t.Fatalf("expected second /scrobble/pause after resume, got %s", req.Path)
	}

	// 3. Stop >= 80% triggers /scrobble/stop and deduplicates immediate EventUserDataChanged
	bus.Publish(ctx, eventbus.Event{
		Type: eventbus.EventPlaybackStop,
		Payload: session.PlaybackEventPayload{
			UserId:        "alice",
			ItemId:        movie.ID,
			PositionTicks: 8_500_000_000,
			RuntimeTicks:  10_000_000_000,
		},
	})
	req = waitReq(t, ch)
	if req.Path != "/scrobble/stop" {
		t.Fatalf("expected /scrobble/stop, got %s", req.Path)
	}
	bus.Publish(ctx, eventbus.Event{
		Type: eventbus.EventUserDataChanged,
		Payload: session.UserDataChangedPayload{
			UserId: "alice",
			UserDataList: []session.UserDataChangedEntry{
				{ItemId: movie.ID, Played: true},
			},
		},
	})
	assertNoReq(t, ch, 120*time.Millisecond)
}
