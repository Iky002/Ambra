package trakt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

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
