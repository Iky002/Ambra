package session

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestSessionConcurrentTOCTOUAndGetSessionRace(t *testing.T) {
	mgr, _, _, _, user, itemID := setupSessionTest(t)
	ctx := context.Background()
	client := auth.ClientInfo{Client: "Web", Device: "Browser", DeviceID: "race-dev"}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				_ = mgr.ReportPlaybackStart(ctx, user, client, PlaybackStartRequest{ItemId: itemID, PositionTicks: 100})
				_ = mgr.ReportPlaybackProgress(ctx, user, client, PlaybackProgressRequest{ItemId: itemID, PositionTicks: 500, IsPaused: j%2 == 0})
				_, _ = mgr.GetSessionByUserDevice(user.ID, client.DeviceID)
				_ = mgr.ListSessions()
				mgr.RemoveSession(user.ID, client.DeviceID)
				_ = mgr.ReportPlaybackStopped(ctx, user, client, PlaybackStoppedRequest{ItemId: itemID, PositionTicks: 600})
				mgr.RemoveSessionsByUser(user.ID)
			}
		}()
	}
	wg.Wait()
}

func TestPlaybackEventPayloadPausedAndRuntimeAndDBRuntimeAuthority(t *testing.T) {
	mgr, _, repo, bus, user, itemID := setupSessionTest(t)
	ctx := context.Background()
	client := auth.ClientInfo{Client: "Infuse", Device: "ATV", DeviceID: "dev-scrobble"}

	var mu sync.Mutex
	var events []struct {
		typ     eventbus.EventType
		payload PlaybackEventPayload
	}
	for _, et := range []eventbus.EventType{
		eventbus.EventPlaybackStart,
		eventbus.EventPlaybackProgress,
		eventbus.EventPlaybackStop,
	} {
		bus.Subscribe(et, func(_ context.Context, e eventbus.Event) {
			if p, ok := e.Payload.(PlaybackEventPayload); ok {
				mu.Lock()
				events = append(events, struct {
					typ     eventbus.EventType
					payload PlaybackEventPayload
				}{typ: e.Type, payload: p})
				mu.Unlock()
			}
		})
	}

	if err := mgr.ReportPlaybackStart(ctx, user, client, PlaybackStartRequest{
		ItemId:        itemID,
		PositionTicks: 100,
		IsPaused:      false,
	}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.ReportPlaybackProgress(ctx, user, client, PlaybackProgressRequest{
		ItemId:        itemID,
		PositionTicks: 500,
		IsPaused:      true,
	}); err != nil {
		t.Fatal(err)
	}
	// Client tries to spoof tiny RuntimeTicks=100 so PositionTicks=500 would mark item played.
	// DB runtime_ticks is 10000, so 500/10000 = 5% must NOT mark item played!
	if err := mgr.ReportPlaybackStopped(ctx, user, client, PlaybackStoppedRequest{
		ItemId:        itemID,
		PositionTicks: 500,
		RuntimeTicks:  100,
	}); err != nil {
		t.Fatal(err)
	}

	ud, err := repo.GetUserItemData(ctx, user.ID, itemID)
	if err != nil {
		t.Fatal(err)
	}
	if ud.Played {
		t.Fatalf("client-spoofed RuntimeTicks must not override DB runtime_ticks; got Played=true")
	}

	time.Sleep(60 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	sawPausedProgress := false
	sawStopRuntime := false
	for _, ev := range events {
		if ev.typ == eventbus.EventPlaybackProgress && ev.payload.IsPaused && ev.payload.RuntimeTicks == 10000 {
			sawPausedProgress = true
		}
		if ev.typ == eventbus.EventPlaybackStop && ev.payload.RuntimeTicks == 10000 && !ev.payload.Played {
			sawStopRuntime = true
		}
	}
	if !sawPausedProgress {
		t.Fatalf("expected EventPlaybackProgress with IsPaused=true and RuntimeTicks=10000, got %+v", events)
	}
	if !sawStopRuntime {
		t.Fatalf("expected EventPlaybackStop with authoritative RuntimeTicks=10000, got %+v", events)
	}
}

func TestHydrateNowPlayingItemEnforcesRBACAndParentalPolicy(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	cfg := config.Default(tmp)
	store, err := sqlite.Open(filepath.Join(tmp, "rbac_np.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	userRepo := sqlite.NewUserRepo(store)
	libRepo := sqlite.NewLibraryRepo(store)
	mgr := NewManager(userRepo, cfg, eventbus.New())

	libAllowed, _ := libRepo.CreateLibrary(ctx, "Kids", "movies", []string{"/kids"})
	libSecret, _ := libRepo.CreateLibrary(ctx, "Adults", "movies", []string{"/adults"})

	rt := int64(50000)
	secretMovie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libSecret.ID, ParentID: libSecret.ID,
		Type: "Movie", MediaType: "Video", Name: "Secret Movie", RuntimeTicks: &rt,
	}
	ratedMovie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libAllowed.ID, ParentID: libAllowed.ID,
		Type: "Movie", MediaType: "Video", Name: "R-Rated Movie", OfficialRating: "R", RuntimeTicks: &rt,
	}
	taggedMovie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libAllowed.ID, ParentID: libAllowed.ID,
		Type: "Movie", MediaType: "Video", Name: "Tagged Movie", RuntimeTicks: &rt,
	}
	okMovie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libAllowed.ID, ParentID: libAllowed.ID,
		Type: "Movie", MediaType: "Video", Name: "Cartoon", OfficialRating: "G", RuntimeTicks: &rt,
	}
	for _, it := range []*sqlite.ItemRecord{secretMovie, ratedMovie, taggedMovie, okMovie} {
		if err := libRepo.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	if err := libRepo.ReplaceItemTaxonomies(ctx, taggedMovie.ID, "tag", []string{"nsfw"}); err != nil {
		t.Fatal(err)
	}

	maxPG := 5
	kidPolicy := sqlite.DefaultUserPolicy(false)
	kidPolicy.EnableAllFolders = false
	kidPolicy.EnabledFolders = []string{libAllowed.ID}
	kidPolicy.MaxParentalRating = &maxPG
	kidPolicy.BlockedTags = []string{"nsfw"}

	kid := &sqlite.UserRecord{
		ID:     guid.New(),
		Name:   "kid",
		Policy: kidPolicy,
		Config: sqlite.DefaultUserConfig(),
	}
	if err := userRepo.CreateUser(ctx, kid); err != nil {
		t.Fatal(err)
	}

	// 1. Disallowed library -> must NOT hydrate Name/RuntimeTicks
	for _, tc := range []struct {
		label string
		id    string
	}{
		{"disallowed_library", secretMovie.ID},
		{"exceeds_parental_rating", ratedMovie.ID},
		{"blocked_tag", taggedMovie.ID},
	} {
		client := auth.ClientInfo{DeviceID: "dev-" + tc.label}
		if err := mgr.ReportPlaybackStart(ctx, kid, client, PlaybackStartRequest{ItemId: tc.id}); err != nil {
			t.Fatal(err)
		}
		sess, ok := mgr.GetSessionByUserDevice(kid.ID, client.DeviceID)
		if !ok || sess.NowPlayingItem == nil {
			t.Fatalf("%s: expected session", tc.label)
		}
		if sess.NowPlayingItem.Name != "" || sess.NowPlayingItem.RunTimeTicks != nil {
			t.Fatalf("%s: restricted metadata leaked into NowPlayingItem: %+v", tc.label, sess.NowPlayingItem)
		}
	}

	// 2. Allowed item -> hydrates properly
	okClient := auth.ClientInfo{DeviceID: "dev-ok"}
	if err := mgr.ReportPlaybackStart(ctx, kid, okClient, PlaybackStartRequest{ItemId: okMovie.ID}); err != nil {
		t.Fatal(err)
	}
	sess, ok := mgr.GetSessionByUserDevice(kid.ID, okClient.DeviceID)
	if !ok || sess.NowPlayingItem == nil || sess.NowPlayingItem.Name != "Cartoon" {
		t.Fatalf("expected allowed item to hydrate, got %+v", sess.NowPlayingItem)
	}
}

func TestRemoveSessionsByUserAndStalePruning(t *testing.T) {
	mgr, _, _, _, user1, _ := setupSessionTest(t)
	user2 := &sqlite.UserRecord{ID: guid.New(), Name: "bob", Policy: sqlite.DefaultUserPolicy(false)}

	mgr.TouchSession(user1, auth.ClientInfo{DeviceID: "u1-d1"})
	mgr.TouchSession(user1, auth.ClientInfo{DeviceID: "u1-d2"})
	mgr.TouchSession(user2, auth.ClientInfo{DeviceID: "u2-d1"})

	// Mark u2-d1 as stale (>24h ago) and verify next TouchSession prunes it.
	u2SID := auth.SessionID(user2.ID, "u2-d1")
	mgr.mu.Lock()
	mgr.sessions[u2SID].info.LastActivityDate = time.Now().UTC().Add(-25 * time.Hour)
	mgr.mu.Unlock()

	mgr.TouchSession(user1, auth.ClientInfo{DeviceID: "u1-d3"})
	if _, ok := mgr.GetSession(u2SID); ok {
		t.Fatal("expected stale >24h session to be pruned")
	}

	// Add fresh session for user2, then RemoveSessionsByUser(user1)
	mgr.TouchSession(user2, auth.ClientInfo{DeviceID: "u2-d2"})
	mgr.RemoveSessionsByUser(user1.ID)
	remaining := mgr.ListSessions()
	if len(remaining) != 1 || remaining[0].UserId != user2.ID {
		t.Fatalf("expected only user2 session remaining, got %+v", remaining)
	}

	// Ensure unused dto import isn't needed if not referenced; test explicit dto copy safety
	_ = dto.BaseItemDto{}
}
