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

func TestReportPlaybackStartRejectsSpoofedItemWithEmptyID(t *testing.T) {
	mgr, _, _, _, user, _ := setupSessionTest(t)
	ctx := context.Background()
	client := auth.ClientInfo{Client: "EvilClient", Device: "PC", DeviceID: "dev-spoof-empty"}

	spoofRT := int64(999999)
	if err := mgr.ReportPlaybackStart(ctx, user, client, PlaybackStartRequest{
		ItemId: "   ",
		Item: &dto.BaseItemDto{
			Id:           "",
			Name:         "Spoofed Movie Title",
			Path:         "/etc/passwd",
			RunTimeTicks: &spoofRT,
		},
		PositionTicks: 1234,
		IsPaused:      true,
	}); err != nil {
		t.Fatalf("ReportPlaybackStart with empty itemID: %v", err)
	}

	sess, ok := mgr.GetSessionByUserDevice(user.ID, client.DeviceID)
	if !ok {
		t.Fatal("expected session created for playstate update")
	}
	if sess.NowPlayingItem != nil {
		t.Fatalf("expected spoofed req.Item ignored when hasLibraryRepo() and itemID is empty, got %+v", sess.NowPlayingItem)
	}
	if !sess.PlayState.IsPaused || sess.PlayState.PositionTicks != 1234 {
		t.Fatalf("expected PlayState updated, got %+v", sess.PlayState)
	}
}

func TestReportPlaybackProgressHydratesAndReplacesNowPlayingItem(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	cfg, err := config.Load(filepath.Join(tmp, "data"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	userRepo := sqlite.NewUserRepo(store)
	libRepo := sqlite.NewLibraryRepo(store)
	bus := eventbus.New()
	mgr := NewManager(userRepo, cfg, bus)

	user := &sqlite.UserRecord{
		ID:        guid.New(),
		Name:      "progress-user",
		Policy:    sqlite.DefaultUserPolicy(false),
		Config:    sqlite.DefaultUserConfig(),
		CreatedAt: time.Now().UTC(),
	}
	if err := userRepo.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	lib, err := libRepo.CreateLibrary(ctx, "Movies", "movies", []string{tmp})
	if err != nil {
		t.Fatal(err)
	}

	rtA := int64(50_000_000_000)
	rtB := int64(75_000_000_000)
	movieA := &sqlite.ItemRecord{
		ID:           guid.New(),
		LibraryID:    lib.ID,
		ParentID:     lib.ID,
		Type:         "Movie",
		MediaType:    "Video",
		Name:         "Movie Alpha",
		RuntimeTicks: &rtA,
		Container:    "mkv",
	}
	movieB := &sqlite.ItemRecord{
		ID:           guid.New(),
		LibraryID:    lib.ID,
		ParentID:     lib.ID,
		Type:         "Movie",
		MediaType:    "Video",
		Name:         "Movie Beta",
		RuntimeTicks: &rtB,
		Container:    "mp4",
	}
	for _, it := range []*sqlite.ItemRecord{movieA, movieB} {
		if err := libRepo.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}

	var mu sync.Mutex
	var progressPayloads []PlaybackEventPayload
	bus.Subscribe(eventbus.EventPlaybackProgress, func(_ context.Context, e eventbus.Event) {
		if p, ok := e.Payload.(PlaybackEventPayload); ok {
			mu.Lock()
			progressPayloads = append(progressPayloads, p)
			mu.Unlock()
		}
	})

	client := auth.ClientInfo{Client: "Infuse", Device: "ATV", DeviceID: "dev-prog-hydrate"}

	// 1. ReportPlaybackProgress without prior ReportPlaybackStart -> hydrates Movie Alpha.
	if err := mgr.ReportPlaybackProgress(ctx, user, client, PlaybackProgressRequest{
		ItemId:        movieA.ID,
		PositionTicks: 1_000_000,
	}); err != nil {
		t.Fatalf("ReportPlaybackProgress A: %v", err)
	}
	sess, ok := mgr.GetSessionByUserDevice(user.ID, client.DeviceID)
	if !ok || sess.NowPlayingItem == nil {
		t.Fatal("expected hydrated NowPlayingItem for Movie Alpha")
	}
	if sess.NowPlayingItem.Name != "Movie Alpha" || sess.NowPlayingItem.RunTimeTicks == nil || *sess.NowPlayingItem.RunTimeTicks != rtA {
		t.Fatalf("expected hydrated Movie Alpha (rt=%d), got %+v", rtA, sess.NowPlayingItem)
	}

	// 2. Client switches directly to Movie Beta via ReportPlaybackProgress -> replaces NowPlayingItem.
	if err := mgr.ReportPlaybackProgress(ctx, user, client, PlaybackProgressRequest{
		ItemId:        movieB.ID,
		PositionTicks: 2_000_000,
	}); err != nil {
		t.Fatalf("ReportPlaybackProgress B: %v", err)
	}
	sess2, ok := mgr.GetSessionByUserDevice(user.ID, client.DeviceID)
	if !ok || sess2.NowPlayingItem == nil {
		t.Fatal("expected hydrated NowPlayingItem for Movie Beta")
	}
	if sess2.NowPlayingItem.Id != movieB.ID || sess2.NowPlayingItem.Name != "Movie Beta" ||
		sess2.NowPlayingItem.RunTimeTicks == nil || *sess2.NowPlayingItem.RunTimeTicks != rtB {
		t.Fatalf("expected replaced NowPlayingItem Movie Beta (rt=%d), got %+v", rtB, sess2.NowPlayingItem)
	}

	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	var sawA, sawB bool
	for _, p := range progressPayloads {
		if p.ItemId == movieA.ID && p.RuntimeTicks == rtA {
			sawA = true
		}
		if p.ItemId == movieB.ID && p.RuntimeTicks == rtB {
			sawB = true
		}
	}
	if !sawA || !sawB {
		t.Fatalf("expected EventPlaybackProgress with accurate runtimeTicks for both items, got %+v", progressPayloads)
	}
}
