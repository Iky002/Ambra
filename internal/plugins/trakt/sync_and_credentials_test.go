package trakt

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestAuditV0107_H9_HostChangeClearsCredsAndM17_NoAutoBindAndDedupRace(t *testing.T) {
	// 1. M-32: Configure ignores runtime base_url overrides so user OAuth tokens cannot be redirected.
	p := New()
	p.SetHTTPClient(nil)
	if err := p.Configure(map[string]any{
		"enabled":      true,
		"client_id":    "cid-secret",
		"access_token": "tok-secret",
		"base_url":     "https://attacker.example.com",
	}); err != nil {
		t.Fatal(err)
	}
	snap := p.snapshot()
	if snap.clientID != "" || snap.accessToken != "" || snap.baseURL != defaultBaseURL {
		t.Fatalf("expected default baseURL and no global creds, got %+v", snap)
	}

	// 2. M-17a: Without explicit user_id, plugin does NOT silently bind to random first user
	if p.shouldReportForUser("random-guest") {
		t.Fatal("must not silently bind or report when user_id is unconfigured")
	}
	if p.shouldReportForUser("") {
		t.Fatal("must not report anonymous events when user_id is unconfigured")
	}

	// 3. M-17b: EventPlaybackStop (without payload RuntimeTicks, relying on DB lookup) racing
	//    with immediate EventUserDataChanged must NOT double-scrobble via /sync/history!
	ch := make(chan capturedRequest, 16)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		ch <- capturedRequest{Method: r.Method, Path: r.URL.Path, Headers: r.Header.Clone(), Body: body}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "trakt.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	repo := sqlite.NewLibraryRepo(store)
	ctx := context.Background()
	mov := &sqlite.ItemRecord{
		ID: "mov-race", LibraryID: "lib1", Type: "Movie", MediaType: "Video",
		Name: "Interstellar", RuntimeTicks: int64Ptr(10_000_000_000),
	}
	if err := repo.UpsertItem(ctx, mov); err != nil {
		t.Fatal(err)
	}

	bus := eventbus.New()
	p2 := New()
	p2.SetHTTPClient(srv.Client())
	_ = configureTestPlugin(t, p2, map[string]any{
		"enabled":      true,
		"client_id":    "cid",
		"access_token": "tok",
		"base_url":     srv.URL,
		"user_id":      "owner-1",
	})
	unsub := p2.SubscribeEvents(bus, repo)
	defer unsub()

	// Publish Stop with RuntimeTicks=0 (so completion >= 80% is only discovered in worker after DB lookup)
	// and IMMEDIATELY publish EventUserDataChanged(Played=true).
	bus.Publish(ctx, eventbus.Event{
		Type: eventbus.EventPlaybackStop,
		Payload: session.PlaybackEventPayload{
			UserId:        "owner-1",
			ItemId:        mov.ID,
			PositionTicks: 9_000_000_000, // 90% of DB RuntimeTicks
			RuntimeTicks:  0,
			Played:        false,
		},
	})
	bus.Publish(ctx, eventbus.Event{
		Type: eventbus.EventUserDataChanged,
		Payload: session.UserDataChangedPayload{
			UserId: "owner-1",
			UserDataList: []session.UserDataChangedEntry{
				{ItemId: mov.ID, Played: true},
			},
		},
	})

	req := waitReq(t, ch)
	if req.Path != "/scrobble/stop" {
		t.Fatalf("expected /scrobble/stop, got %s", req.Path)
	}
	// Must NOT send duplicate /sync/history!
	assertNoReq(t, ch, 150*time.Millisecond)

	// 4. Exercise beginInFlightStop / waitInFlightStop cancellation and unsub during settle delay
	done1 := p2.beginInFlightStop("owner-1", "item-wait")
	done2 := p2.beginInFlightStop("owner-1", "item-wait")
	cCtx, cancel := context.WithCancel(ctx)
	cancel()
	p2.waitInFlightStop(cCtx, "owner-1", "item-wait")
	done2()
	done1()

	// Publish UserDataChanged and immediately unsubscribe -> covers <-stopCh branch in settle timer
	bus.Publish(ctx, eventbus.Event{
		Type: eventbus.EventUserDataChanged,
		Payload: session.UserDataChangedPayload{
			UserId: "owner-1",
			UserDataList: []session.UserDataChangedEntry{
				{ItemId: "mov-unsub", Played: true},
			},
		},
	})
	time.Sleep(10 * time.Millisecond)
	unsub()
	assertNoReq(t, ch, 120*time.Millisecond)
}

type countingUserConfigStore struct {
	inner UserConfigStore
	calls atomic.Int32
}

func (c *countingUserConfigStore) GetUserTraktConfig(ctx context.Context, userID string) (*sqlite.TraktConfig, error) {
	c.calls.Add(1)
	return c.inner.GetUserTraktConfig(ctx, userID)
}

func TestAuditV0109_M32_ConfigureIgnoresBaseURL(t *testing.T) {
	p := New()
	if err := p.Configure(map[string]any{
		"enabled":  true,
		"priority": 15,
		"base_url": "http://evil.example.com",
		"BaseUrl":  "http://evil2.example.com",
	}); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	snap := p.snapshot()
	if snap.baseURL != defaultBaseURL {
		t.Fatalf("expected baseURL unchanged (%q), got %q", defaultBaseURL, snap.baseURL)
	}
	if !snap.enabled || snap.priority != 15 {
		t.Fatalf("expected enabled=true priority=15, got %+v", snap)
	}
}

func TestAuditV0109_M37_FailedScrobbleStopAllowsSyncWatchedAndSingleLookup(t *testing.T) {
	var failStop atomic.Bool
	failStop.Store(true)
	reqCh := make(chan capturedRequest, 16)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		reqCh <- capturedRequest{Method: r.Method, Path: r.URL.Path, Headers: r.Header.Clone(), Body: body}
		if r.URL.Path == "/scrobble/stop" && failStop.Load() {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"rate_limited"}`))
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	repo := openTestLibraryRepo(t)
	ctx := context.Background()
	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{"/media"})
	if err != nil {
		t.Fatal(err)
	}
	movie := &sqlite.ItemRecord{
		ID:           "mov-m37",
		LibraryID:    lib.ID,
		Type:         "Movie",
		MediaType:    "Video",
		Name:         "Dune",
		RuntimeTicks: int64Ptr(10_000_000_000),
	}
	if err := repo.UpsertItem(ctx, movie); err != nil {
		t.Fatal(err)
	}

	p := New()
	p.SetHTTPClient(srv.Client())
	p.SetBaseURL(srv.URL)
	if err := p.Configure(map[string]any{"enabled": true}); err != nil {
		t.Fatal(err)
	}
	baseStore := &testUserConfigStore{
		configs: map[string]sqlite.TraktConfig{
			"alice": {
				ClientID:         "cid-alice",
				AccessToken:      "tok-alice",
				ScrobblePlayback: true,
				SyncWatched:      true,
			},
		},
	}
	counter := &countingUserConfigStore{inner: baseStore}
	p.SetUserConfigStore(counter)

	bus := eventbus.New()
	unsub := p.SubscribeEvents(bus, repo)

	// 1. Non-paused progress must NOT query user config store at all.
	bus.Publish(ctx, eventbus.Event{
		Type: eventbus.EventPlaybackProgress,
		Payload: session.PlaybackEventPayload{
			UserId:        "alice",
			ItemId:        movie.ID,
			PositionTicks: 1_000_000_000,
			IsPaused:      false,
		},
	})
	time.Sleep(40 * time.Millisecond)
	if got := counter.calls.Load(); got != 0 {
		t.Fatalf("expected 0 config lookups for non-paused progress, got %d", got)
	}

	// 2. EventPlaybackStop fails with 429 -> must NOT mark completed, so subsequent EventUserDataChanged syncs!
	bus.Publish(ctx, eventbus.Event{
		Type: eventbus.EventPlaybackStop,
		Payload: session.PlaybackEventPayload{
			UserId:        "alice",
			ItemId:        movie.ID,
			PositionTicks: 9_000_000_000,
			Played:        true,
		},
	})
	req1 := waitReq(t, reqCh)
	if req1.Path != "/scrobble/stop" {
		t.Fatalf("expected /scrobble/stop, got %s", req1.Path)
	}
	// Single GetUserTraktConfig call for the stop event.
	time.Sleep(30 * time.Millisecond)
	if got := counter.calls.Load(); got != 1 {
		t.Fatalf("expected exactly 1 config lookup for EventPlaybackStop, got %d", got)
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
	req2 := waitReq(t, reqCh)
	if req2.Path != "/sync/history" {
		t.Fatalf("expected /sync/history fallback after failed /scrobble/stop, got %s", req2.Path)
	}

	// 3. Unsubscribe safely closes any residual stopInFlight channels so waiters unblock immediately.
	_ = p.beginInFlightStop("alice", "stuck-item")
	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		p.waitInFlightStop(context.Background(), "alice", "stuck-item")
	}()
	time.Sleep(20 * time.Millisecond)
	unsub()
	select {
	case <-waitDone:
	case <-time.After(1 * time.Second):
		t.Fatal("waitInFlightStop remained blocked after unsubscribe")
	}
}

func TestTraktBulkUserDataAggregationAndWriteRateLimiting(t *testing.T) {
	srv, ch := newMockTraktServer(t, http.StatusCreated, `{}`)
	repo := openTestLibraryRepo(t)
	ctx := context.Background()

	lib, err := repo.CreateLibrary(ctx, "TV", "tvshows", []string{"/tv"})
	if err != nil {
		t.Fatal(err)
	}
	series := &sqlite.ItemRecord{
		ID:             "series-bulk",
		LibraryID:      lib.ID,
		ParentID:       lib.ID,
		Type:           "Series",
		Name:           "One Piece",
		ProductionYear: intPtr(1999),
		Extra:          sqlite.ItemExtra{ProviderIDs: map[string]string{"tvdb": "81797"}},
	}
	if err := repo.UpsertItem(ctx, series); err != nil {
		t.Fatal(err)
	}

	const totalEpisodes = 220 // > old queue size of 128!
	udList := make([]session.UserDataChangedEntry, 0, totalEpisodes)
	for i := 1; i <= totalEpisodes; i++ {
		epID := fmt.Sprintf("ep-bulk-%03d", i)
		ep := &sqlite.ItemRecord{
			ID:                epID,
			LibraryID:         lib.ID,
			ParentID:          series.ID,
			SeriesID:          series.ID,
			Type:              "Episode",
			Name:              fmt.Sprintf("Episode %d", i),
			ParentIndexNumber: intPtr(1),
			IndexNumber:       intPtr(i),
		}
		if err := repo.UpsertItem(ctx, ep); err != nil {
			t.Fatal(err)
		}
		udList = append(udList, session.UserDataChangedEntry{
			ItemId: epID,
			Played: true,
		})
	}

	bus := eventbus.New()
	p := New()
	p.SetHTTPClient(srv.Client())
	p.SetMinWriteInterval(150 * time.Millisecond)
	_ = configureTestPlugin(t, p, map[string]any{
		"enabled":      true,
		"client_id":    "bulk-cid",
		"access_token": "bulk-tok",
		"base_url":     srv.URL,
		"user_id":      "u-bulk",
	})
	unsub := p.SubscribeEvents(bus, repo)
	defer unsub()

	// Publish 220 episodes marked watched in a single EventUserDataChanged event:
	// MUST be aggregated into a SINGLE /sync/history HTTP request containing all 220 episodes!
	bus.Publish(ctx, eventbus.Event{
		Type: eventbus.EventUserDataChanged,
		Payload: session.UserDataChangedPayload{
			UserId:       "u-bulk",
			UserDataList: udList,
		},
	})

	req := waitReq(t, ch)
	if req.Path != "/sync/history" {
		t.Fatalf("expected /sync/history, got %s", req.Path)
	}
	shows, ok := req.Body["shows"].([]any)
	if !ok || len(shows) != 1 {
		t.Fatalf("expected 1 aggregated show in payload, got %+v", req.Body["shows"])
	}
	seasons := shows[0].(map[string]any)["seasons"].([]any)
	eps := seasons[0].(map[string]any)["episodes"].([]any)
	if len(eps) != totalEpisodes {
		t.Fatalf("expected all %d episodes aggregated without dropping, got %d", totalEpisodes, len(eps))
	}
	assertNoReq(t, ch, 120*time.Millisecond)

	// Verify write rate limiting (~minWriteInterval between consecutive writes)
	m := &sqlite.ItemRecord{ID: "m-rate", LibraryID: lib.ID, Type: "Movie", Name: "RateMovie"}
	_ = repo.UpsertItem(ctx, m)

	t0 := time.Now()
	_ = p.ScrobbleStart(ctx, "u-bulk", m, nil, 10)
	_ = waitReq(t, ch)
	_ = p.ScrobbleStop(ctx, "u-bulk", m, nil, 90)
	_ = waitReq(t, ch)
	if elapsed := time.Since(t0); elapsed < 140*time.Millisecond {
		t.Fatalf("expected write rate limiter to space consecutive writes by >=150ms, took %v", elapsed)
	}

	defCfg := p.DefaultConfig()
	if defCfg["enabled"] != true || defCfg["priority"] != 40 {
		t.Fatalf("unexpected DefaultConfig: %+v", defCfg)
	}
}
