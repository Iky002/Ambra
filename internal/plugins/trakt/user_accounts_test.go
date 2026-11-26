package trakt

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

type testUserConfigStore struct {
	mu      sync.RWMutex
	configs map[string]sqlite.TraktConfig
	err     error
}

func (s *testUserConfigStore) GetUserTraktConfig(_ context.Context, id string) (*sqlite.TraktConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.err != nil {
		return nil, s.err
	}
	c, ok := s.configs[id]
	if !ok {
		return nil, nil
	}
	return &c, nil
}

// Adapt the protocol/payload tests to configure credentials in the user store,
// while Configure itself only changes global plugin settings.
func configureTestPlugin(t *testing.T, p *Plugin, cfg map[string]any) error {
	t.Helper()
	if err := p.Configure(cfg); err != nil {
		return err
	}
	store, ok := p.userConfigs.(*testUserConfigStore)
	if !ok {
		store = &testUserConfigStore{configs: make(map[string]sqlite.TraktConfig)}
		p.SetUserConfigStore(store)
	}
	id := "test-user"
	if v, ok := firstString(cfg, "user_id", "allowed_user_id"); ok {
		id = v
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	c, exists := store.configs[id]
	if !exists {
		c.ScrobblePlayback, c.SyncWatched = true, true
	}
	if v, ok := firstString(cfg, "client_id", "ClientId"); ok {
		c.ClientID = v
	}
	if v, ok := firstString(cfg, "access_token", "AccessToken"); ok {
		c.AccessToken = v
	}
	if v, ok := firstBool(cfg, "scrobble_playback"); ok {
		c.ScrobblePlayback = v
	}
	if v, ok := firstBool(cfg, "sync_watched"); ok {
		c.SyncWatched = v
	}
	store.configs[id] = c
	return nil
}

func TestPersonalAccountsRouteEventsAndHonorIndependentPreferences(t *testing.T) {
	srv, ch := newMockTraktServer(t, 201, `{}`)
	repo := openTestLibraryRepo(t)
	ctx := context.Background()
	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{"/media"})
	if err != nil {
		t.Fatal(err)
	}
	movie := &sqlite.ItemRecord{ID: "personal-movie", LibraryID: lib.ID, Type: "Movie",
		Name: "Arrival", RuntimeTicks: int64Ptr(100)}
	if err := repo.UpsertItem(ctx, movie); err != nil {
		t.Fatal(err)
	}
	p := New()
	p.SetHTTPClient(srv.Client())
	for _, account := range []struct{ id, token string }{{"alice", "alice-token"}, {"bob", "bob-token"}} {
		if err := configureTestPlugin(t, p, map[string]any{
			"enabled": true, "base_url": srv.URL, "client_id": account.id + "-client",
			"access_token": account.token, "user_id": account.id,
		}); err != nil {
			t.Fatal(err)
		}
	}
	bus := eventbus.New()
	unsub := p.SubscribeEvents(bus, repo)
	defer unsub()
	for _, id := range []string{"alice", "bob", "unbound", ""} {
		bus.Publish(ctx, eventbus.Event{Type: eventbus.EventPlaybackStart,
			Payload: session.PlaybackEventPayload{UserId: id, ItemId: movie.ID}})
	}
	tokens := make(map[string]bool)
	for i := 0; i < 2; i++ {
		req := waitReq(t, ch)
		id := req.Headers.Get("trakt-api-key")
		want := map[string]string{"alice-client": "alice-token", "bob-client": "bob-token"}[id]
		if want == "" || req.Headers.Get("Authorization") != "Bearer "+want {
			t.Fatalf("credentials crossed user boundary: %+v", req.Headers)
		}
		tokens[want] = true
	}
	if len(tokens) != 2 {
		t.Fatal("both personal accounts must receive their own event")
	}
	assertNoReq(t, ch, 100*time.Millisecond)

	// Disabling scrobbling must not suppress Alice's independent watched sync.
	_ = configureTestPlugin(t, p, map[string]any{"user_id": "alice", "scrobble_playback": false})
	bus.Publish(ctx, eventbus.Event{Type: eventbus.EventPlaybackStop,
		Payload: session.PlaybackEventPayload{UserId: "alice", ItemId: movie.ID, Played: true}})
	bus.Publish(ctx, eventbus.Event{Type: eventbus.EventUserDataChanged,
		Payload: session.UserDataChangedPayload{UserId: "alice",
			UserDataList: []session.UserDataChangedEntry{{ItemId: movie.ID, Played: true}}}})
	req := waitReq(t, ch)
	if req.Path != "/sync/history" || req.Headers.Get("Authorization") != "Bearer alice-token" {
		t.Fatalf("Alice's watched sync = %s, %v", req.Path, req.Headers)
	}
	assertNoReq(t, ch, 100*time.Millisecond)
	_ = p.Configure(map[string]any{"enabled": false})
	for _, id := range []string{"alice", "bob"} {
		_ = p.ScrobbleStart(ctx, id, movie, nil, 20)
		_ = p.SyncWatched(ctx, id, movie, nil)
	}
	assertNoReq(t, ch, 100*time.Millisecond)
}

func TestUserStoreFailuresAndLegacyGlobalCredentialsNeverReport(t *testing.T) {
	p := New()
	_ = p.Configure(map[string]any{"enabled": true, "client_id": "legacy-client",
		"access_token": "legacy-token", "user_id": "alice"})
	if p.shouldReportForUser("alice") {
		t.Fatal("legacy global account must not be used")
	}
	p.SetUserConfigStore(&testUserConfigStore{err: errors.New("store unavailable")})
	if err := p.ScrobbleStart(context.Background(), "alice", nil, nil, 0); err == nil {
		t.Fatal("store failures must propagate without falling back to a global token")
	}
	if err := p.SyncWatched(context.Background(), "alice", nil, nil); err == nil {
		t.Fatal("store failures must propagate")
	}
	filtered := p.FilterServerConfig(map[string]any{
		"Enabled": true, "Priority": 42, "ClientId": "legacy", "AccessToken": "secret",
		"user_id": "alice", "base_url": "https://attacker.invalid",
	})
	if len(filtered) != 2 || filtered["enabled"] != true || filtered["priority"] != 42 {
		t.Fatalf("global config must only contain availability and priority: %v", filtered)
	}
}
