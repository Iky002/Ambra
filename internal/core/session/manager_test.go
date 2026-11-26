package session

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func setupSessionTest(t *testing.T) (*Manager, *sqlite.Store, *sqlite.UserRepo, *eventbus.Bus, *sqlite.UserRecord, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := sqlite.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	cfg := config.Default(dir)
	cfg.PlayedThreshold = 0.90
	repo := sqlite.NewUserRepo(store)
	bus := eventbus.New()
	mgr := NewManager(repo, cfg, bus)

	user := &sqlite.UserRecord{
		ID:     guid.New(),
		Name:   "alice",
		Policy: sqlite.DefaultUserPolicy(false),
		Config: sqlite.DefaultUserConfig(),
	}
	if err := repo.CreateUser(context.Background(), user); err != nil {
		t.Fatalf("create user: %v", err)
	}

	itemID := guid.New()
	now := time.Now().Unix()
	_, err = store.Writer.Exec(`
		INSERT INTO items (id, library_id, type, name, sort_name, runtime_ticks, etag, date_created, date_modified)
		VALUES (?, 'lib1', 'Movie', 'Test Movie', 'test movie', ?, 'etag1', ?, ?)`,
		itemID, int64(10000), now, now,
	)
	if err != nil {
		t.Fatalf("insert test item: %v", err)
	}

	return mgr, store, repo, bus, user, itemID
}

func TestPlaybackLifecycleAndAutoPlayed(t *testing.T) {
	mgr, _, repo, bus, user, itemID := setupSessionTest(t)
	ctx := context.Background()
	client := auth.ClientInfo{Client: "Ambra Web", Device: "Chrome", DeviceID: "dev-1", Version: "1.0"}

	var mu sync.Mutex
	seenEvents := map[eventbus.EventType]int{}
	for _, et := range []eventbus.EventType{
		eventbus.EventPlaybackStart,
		eventbus.EventPlaybackStop,
		eventbus.EventUserDataChanged,
		eventbus.EventSessionUpdated,
	} {
		bus.Subscribe(et, func(_ context.Context, e eventbus.Event) {
			mu.Lock()
			seenEvents[e.Type]++
			mu.Unlock()
		})
	}

	// 1. Start playback
	if err := mgr.ReportPlaybackStart(ctx, user, client, PlaybackStartRequest{
		ItemId:        itemID,
		MediaSourceId: "ms-1",
		PlayMethod:    "DirectPlay",
		PositionTicks: 1000,
	}); err != nil {
		t.Fatalf("ReportPlaybackStart: %v", err)
	}

	sessions := mgr.ListSessions()
	if len(sessions) != 1 || sessions[0].NowPlayingItem == nil || sessions[0].NowPlayingItem.Id != itemID {
		t.Fatalf("expected active NowPlayingItem, got %+v", sessions)
	}

	// 2. Progress with pause toggle triggers DB persist
	if err := mgr.ReportPlaybackProgress(ctx, user, client, PlaybackProgressRequest{
		ItemId:        itemID,
		PositionTicks: 5000,
		IsPaused:      true,
	}); err != nil {
		t.Fatalf("ReportPlaybackProgress: %v", err)
	}
	ud, err := repo.GetUserItemData(ctx, user.ID, itemID)
	if err != nil {
		t.Fatalf("GetUserItemData after progress: %v", err)
	}
	if ud.PlaybackPositionTicks != 5000 || ud.Played {
		t.Fatalf("expected position 5000 & played=false, got %+v", ud)
	}

	// 3. Stop at 50% -> keeps resume position, not marked played
	if err := mgr.ReportPlaybackStopped(ctx, user, client, PlaybackStoppedRequest{
		ItemId:        itemID,
		PositionTicks: 5000,
	}); err != nil {
		t.Fatalf("ReportPlaybackStopped partial: %v", err)
	}
	ud, err = repo.GetUserItemData(ctx, user.ID, itemID)
	if err != nil {
		t.Fatalf("GetUserItemData partial stop: %v", err)
	}
	if ud.Played || ud.PlaybackPositionTicks != 5000 {
		t.Fatalf("expected unplayed with position 5000, got %+v", ud)
	}

	// 4. Stop at 92% (>= 90% threshold) -> auto marks played and resets position to 0
	if err := mgr.ReportPlaybackStopped(ctx, user, client, PlaybackStoppedRequest{
		ItemId:        itemID,
		PositionTicks: 9200,
	}); err != nil {
		t.Fatalf("ReportPlaybackStopped completed: %v", err)
	}
	ud, err = repo.GetUserItemData(ctx, user.ID, itemID)
	if err != nil {
		t.Fatalf("GetUserItemData completed stop: %v", err)
	}
	if !ud.Played || ud.PlayCount != 1 || ud.PlaybackPositionTicks != 0 {
		t.Fatalf("expected played=true playCount=1 pos=0, got %+v", ud)
	}

	// Capabilities update
	mgr.UpdateCapabilities(user, client, CapabilitiesRequest{
		SupportsRemoteControl: true,
		PlayableMediaTypes:    []string{"Video", "Audio"},
		SupportedCommands:     []string{"PlayState", "DisplayMessage"},
	})
	s, ok := mgr.GetSessionByUserDevice(user.ID, client.DeviceID)
	if !ok || !s.SupportsRemoteControl || len(s.PlayableMediaTypes) != 2 {
		t.Fatalf("unexpected session after capabilities: ok=%v s=%+v", ok, s)
	}

	// Allow async eventbus goroutines to run
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	for _, et := range []eventbus.EventType{
		eventbus.EventPlaybackStart,
		eventbus.EventPlaybackStop,
		eventbus.EventUserDataChanged,
		eventbus.EventSessionUpdated,
	} {
		if seenEvents[et] == 0 {
			t.Errorf("expected event %s to be published", et)
		}
	}
}

func TestAdditionalUsersCoWatchPlayedAndRename(t *testing.T) {
	mgr, _, repo, _, alice, itemID := setupSessionTest(t)
	ctx := context.Background()
	client := auth.ClientInfo{Client: "Ambra Web", Device: "TV", DeviceID: "dev-tv", Version: "1.0"}

	bob := &sqlite.UserRecord{
		ID:     guid.New(),
		Name:   "bob",
		Policy: sqlite.DefaultUserPolicy(false),
		Config: sqlite.DefaultUserConfig(),
	}
	if err := repo.CreateUser(ctx, bob); err != nil {
		t.Fatal(err)
	}

	sess := mgr.TouchSession(alice, client)
	found, err := mgr.AddAdditionalUser(ctx, sess.Id, bob.ID)
	if err != nil || !found {
		t.Fatalf("AddAdditionalUser: found=%v err=%v", found, err)
	}

	got, ok := mgr.GetSession(sess.Id)
	if !ok || len(got.AdditionalUsers) != 1 || got.AdditionalUsers[0].UserId != bob.ID || got.AdditionalUsers[0].UserName != "bob" {
		t.Fatalf("expected bob in AdditionalUsers, got %+v", got.AdditionalUsers)
	}

	// Rename both primary and co-watching users (L-03)
	mgr.UpdateUserName(alice.ID, "AliceRenamed")
	mgr.UpdateUserName(bob.ID, "BobRenamed")
	got, _ = mgr.GetSession(sess.Id)
	if got.UserName != "AliceRenamed" || got.AdditionalUsers[0].UserName != "BobRenamed" {
		t.Fatalf("expected updated names in session, got user=%q additional=%+v", got.UserName, got.AdditionalUsers)
	}

	// Completing playback marks item Played for BOTH primary user and AdditionalUsers member
	if err := mgr.ReportPlaybackStopped(ctx, alice, client, PlaybackStoppedRequest{
		ItemId:        itemID,
		PositionTicks: 9500,
	}); err != nil {
		t.Fatalf("ReportPlaybackStopped: %v", err)
	}
	bobUD, err := repo.GetUserItemData(ctx, bob.ID, itemID)
	if err != nil || !bobUD.Played || bobUD.PlayCount != 1 {
		t.Fatalf("expected co-watching user bob to be marked played, got %+v err=%v", bobUD, err)
	}

	if !mgr.RemoveAdditionalUser(sess.Id, bob.ID) {
		t.Fatal("expected RemoveAdditionalUser to succeed")
	}
	got, _ = mgr.GetSession(sess.Id)
	if len(got.AdditionalUsers) != 0 {
		t.Fatalf("expected empty AdditionalUsers after remove, got %+v", got.AdditionalUsers)
	}
}
