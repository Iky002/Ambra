package session

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestPlaybackItemAccessCheckerAndMissingItemRejection(t *testing.T) {
	mgr, _, repo, _, user, itemID := setupSessionTest(t)
	ctx := context.Background()
	client := auth.ClientInfo{Client: "Web", Device: "Browser", DeviceID: "dev-rbac"}

	// 1. Non-existent itemID is rejected even before setting a custom checker (because libRepo exists).
	missingID := guid.New()
	if err := mgr.ReportPlaybackStart(ctx, user, client, PlaybackStartRequest{ItemId: missingID}); !errors.Is(err, ErrItemAccessDenied) {
		t.Fatalf("expected ErrItemAccessDenied on Start for missing item, got %v", err)
	}
	if err := mgr.ReportPlaybackProgress(ctx, user, client, PlaybackProgressRequest{ItemId: missingID, PositionTicks: 1000, IsPaused: true}); !errors.Is(err, ErrItemAccessDenied) {
		t.Fatalf("expected ErrItemAccessDenied on Progress for missing item, got %v", err)
	}
	if err := mgr.ReportPlaybackStopped(ctx, user, client, PlaybackStoppedRequest{ItemId: missingID, PositionTicks: 5000}); !errors.Is(err, ErrItemAccessDenied) {
		t.Fatalf("expected ErrItemAccessDenied on Stopped for missing item, got %v", err)
	}

	// Verify neither user_item_data nor activity_logs were polluted.
	if _, err := repo.GetUserItemData(ctx, user.ID, missingID); !errors.Is(err, sqlite.ErrNotFound) {
		t.Fatalf("missing item must not write user_item_data, err=%v", err)
	}
	logs, total, err := repo.ListActivityLogs(ctx, 0, 50, nil)
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 || len(logs) != 0 {
		t.Fatalf("missing item must not write activity_logs, got total=%d logs=%+v", total, logs)
	}

	// 2. SetItemAccessChecker denies unauthorized existing item across Start/Progress/Stopped.
	denyAll := true
	mgr.SetItemAccessChecker(func(_ context.Context, u *sqlite.UserRecord, id string) bool {
		return !denyAll && u != nil && id == itemID
	})

	if err := mgr.ReportPlaybackStart(ctx, user, client, PlaybackStartRequest{ItemId: itemID, PositionTicks: 100}); !errors.Is(err, ErrItemAccessDenied) {
		t.Fatalf("expected ErrItemAccessDenied on Start when checker denies, got %v", err)
	}
	if err := mgr.ReportPlaybackProgress(ctx, user, client, PlaybackProgressRequest{ItemId: itemID, PositionTicks: 200, IsPaused: true}); !errors.Is(err, ErrItemAccessDenied) {
		t.Fatalf("expected ErrItemAccessDenied on Progress when checker denies, got %v", err)
	}
	if err := mgr.ReportPlaybackStopped(ctx, user, client, PlaybackStoppedRequest{ItemId: itemID, PositionTicks: 300}); !errors.Is(err, ErrItemAccessDenied) {
		t.Fatalf("expected ErrItemAccessDenied on Stopped when checker denies, got %v", err)
	}
	if _, err := repo.GetUserItemData(ctx, user.ID, itemID); !errors.Is(err, sqlite.ErrNotFound) {
		t.Fatalf("denied item must not write user_item_data, err=%v", err)
	}
	if _, total, _ := repo.ListActivityLogs(ctx, 0, 50, nil); total != 0 {
		t.Fatalf("denied item must not write activity_logs, got total=%d", total)
	}

	// 3. When checker allows, Start -> Progress (using NowPlayingItem fallback) -> Stopped all succeed.
	denyAll = false
	if err := mgr.ReportPlaybackStart(ctx, user, client, PlaybackStartRequest{ItemId: itemID, PositionTicks: 100}); err != nil {
		t.Fatalf("allowed Start failed: %v", err)
	}
	if err := mgr.ReportPlaybackProgress(ctx, user, client, PlaybackProgressRequest{PositionTicks: 500, IsPaused: true}); err != nil {
		t.Fatalf("allowed Progress failed: %v", err)
	}
	// If access is revoked mid-playback, Progress/Stopped using empty ItemId fallback must also be denied!
	denyAll = true
	if err := mgr.ReportPlaybackProgress(ctx, user, client, PlaybackProgressRequest{PositionTicks: 600, IsPaused: false}); !errors.Is(err, ErrItemAccessDenied) {
		t.Fatalf("expected Progress denied after access revocation, got %v", err)
	}
	if err := mgr.ReportPlaybackStopped(ctx, user, client, PlaybackStoppedRequest{PositionTicks: 700}); !errors.Is(err, ErrItemAccessDenied) {
		t.Fatalf("expected Stopped denied after access revocation, got %v", err)
	}

	denyAll = false
	if err := mgr.ReportPlaybackStopped(ctx, user, client, PlaybackStoppedRequest{PositionTicks: 500}); err != nil {
		t.Fatalf("allowed Stopped failed: %v", err)
	}

	// Checker allowing a non-existent ID still fails existence check when libRepo is set.
	mgr.SetItemAccessChecker(func(_ context.Context, _ *sqlite.UserRecord, _ string) bool { return true })
	if err := mgr.ReportPlaybackStart(ctx, user, client, PlaybackStartRequest{ItemId: missingID}); !errors.Is(err, ErrItemAccessDenied) {
		t.Fatalf("expected non-existent item rejected even if checker returns true, got %v", err)
	}
}

func TestActivityLogThrottlingAndCap(t *testing.T) {
	mgr, _, repo, _, user, itemID := setupSessionTest(t)
	ctx := context.Background()
	client := auth.ClientInfo{Client: "Web", Device: "Browser", DeviceID: "dev-throttle"}

	// Spam 10 Start and 10 Stopped calls within the 30s cooldown window.
	for i := 0; i < 10; i++ {
		if err := mgr.ReportPlaybackStart(ctx, user, client, PlaybackStartRequest{ItemId: itemID, PositionTicks: int64(i * 10)}); err != nil {
			t.Fatalf("start #%d: %v", i, err)
		}
		if err := mgr.ReportPlaybackStopped(ctx, user, client, PlaybackStoppedRequest{ItemId: itemID, PositionTicks: int64(i * 10)}); err != nil {
			t.Fatalf("stop #%d: %v", i, err)
		}
	}

	// Only 1 VideoPlayback + 1 VideoPlaybackStopped should be written within cooldown.
	_, total, err := repo.ListActivityLogs(ctx, 0, 50, nil)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Fatalf("expected 2 throttled activity logs (1 start + 1 stop), got %d", total)
	}

	// After cooldown elapses, new entries are allowed again.
	mgr.mu.Lock()
	mgr.activityLogCooldown = 10 * time.Millisecond
	mgr.mu.Unlock()
	time.Sleep(20 * time.Millisecond)

	if err := mgr.ReportPlaybackStart(ctx, user, client, PlaybackStartRequest{ItemId: itemID, PositionTicks: 100}); err != nil {
		t.Fatal(err)
	}
	_, total, err = repo.ListActivityLogs(ctx, 0, 50, nil)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 {
		t.Fatalf("expected 3 activity logs after cooldown elapsed, got %d", total)
	}

	// Verify map capacity cap bounds memory usage even under high cardinality.
	mgr.mu.Lock()
	mgr.activityLogCooldown = time.Hour
	mgr.activityLogThrottle = make(map[activityThrottleKey]time.Time, maxActivityLogThrottleKeys)
	now := time.Now().UTC()
	for i := 0; i < maxActivityLogThrottleKeys; i++ {
		mgr.activityLogThrottle[activityThrottleKey{
			userID:    user.ID,
			itemID:    fmt.Sprintf("item-%d", i),
			eventType: "VideoPlayback",
		}] = now
	}
	mgr.mu.Unlock()

	if !mgr.shouldRecordActivityLog(user.ID, "brand-new-item", "VideoPlayback", now.Add(time.Second)) {
		t.Fatal("expected new key to be recorded after cap eviction")
	}
	mgr.mu.RLock()
	throttleLen := len(mgr.activityLogThrottle)
	mgr.mu.RUnlock()
	if throttleLen > maxActivityLogThrottleKeys {
		t.Fatalf("activityLogThrottle exceeded cap %d: got %d", maxActivityLogThrottleKeys, throttleLen)
	}
}
