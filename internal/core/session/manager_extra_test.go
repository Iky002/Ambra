package session

import (
	"context"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestSessionManagerEdgeCases(t *testing.T) {
	mgr, _, _, _, user, itemID := setupSessionTest(t)
	ctx := context.Background()

	// Nil user no-ops
	if s := mgr.TouchSession(nil, auth.ClientInfo{}); s.Id != "" {
		t.Fatalf("expected empty session for nil user, got %+v", s)
	}
	mgr.UpdateCapabilities(nil, auth.ClientInfo{}, CapabilitiesRequest{})
	if err := mgr.ReportPlaybackStart(ctx, nil, auth.ClientInfo{}, PlaybackStartRequest{}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.ReportPlaybackProgress(ctx, nil, auth.ClientInfo{}, PlaybackProgressRequest{}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.ReportPlaybackStopped(ctx, nil, auth.ClientInfo{}, PlaybackStoppedRequest{}); err != nil {
		t.Fatal(err)
	}

	// TouchSession with empty DeviceID defaults to default-device, then update client metadata
	s1 := mgr.TouchSession(user, auth.ClientInfo{})
	if s1.DeviceId != "default-device" || s1.Client != "Ambra Client" || s1.DeviceName != "Unknown Device" || s1.ApplicationVersion != "1.0.0" {
		t.Fatalf("unexpected default session: %+v", s1)
	}
	s1Updated := mgr.TouchSession(user, auth.ClientInfo{
		Client:  "Infuse",
		Device:  "iPad",
		Version: "8.1",
	})
	if s1Updated.Client != "Infuse" || s1Updated.DeviceName != "iPad" || s1Updated.ApplicationVersion != "8.1" {
		t.Fatalf("expected updated session metadata: %+v", s1Updated)
	}

	// Second device to test ListSessions sorting
	time.Sleep(5 * time.Millisecond)
	client2 := auth.ClientInfo{Client: "Web", Device: "Chrome", DeviceID: "dev-2", Version: "2.0"}
	mgr.TouchSession(user, client2)
	list := mgr.ListSessions()
	if len(list) != 2 || list[0].DeviceId != "dev-2" {
		t.Fatalf("expected 2 sessions sorted by LastActivityDate desc, got %+v", list)
	}

	// GetSession not found
	if _, ok := mgr.GetSession("nonexistent"); ok {
		t.Fatal("expected GetSession nonexistent to return false")
	}

	// ReportPlaybackStart with explicit Item dto (missing Id & ServerId)
	rt := int64(20000)
	audioIdx := 1
	subIdx := 2
	if err := mgr.ReportPlaybackStart(ctx, user, client2, PlaybackStartRequest{
		ItemId: itemID,
		Item: &dto.BaseItemDto{
			Name:         "Explicit Movie",
			RunTimeTicks: &rt,
		},
		PlaySessionId:       "ps-1",
		PlayMethod:          "Transcode",
		AudioStreamIndex:    &audioIdx,
		SubtitleStreamIndex: &subIdx,
		RepeatMode:          "RepeatAll",
	}); err != nil {
		t.Fatal(err)
	}

	// ReportPlaybackProgress with all optional fields, negative PositionTicks, and empty ItemId (falls back to NowPlayingItem)
	audioIdx2 := 2
	subIdx2 := 3
	if err := mgr.ReportPlaybackProgress(ctx, user, client2, PlaybackProgressRequest{
		ItemId:              "",
		PlaySessionId:       "ps-2",
		PositionTicks:       -100,
		IsPaused:            true,
		VolumeLevel:         80,
		AudioStreamIndex:    &audioIdx2,
		SubtitleStreamIndex: &subIdx2,
		MediaSourceId:       "ms-2",
		PlayMethod:          "DirectStream",
		RepeatMode:          "RepeatOne",
	}); err != nil {
		t.Fatal(err)
	}

	// Progress when NowPlayingItem is nil sets NowPlayingItem
	client3 := auth.ClientInfo{DeviceID: "dev-3"}
	if err := mgr.ReportPlaybackProgress(ctx, user, client3, PlaybackProgressRequest{
		ItemId:        itemID,
		PositionTicks: 500,
	}); err != nil {
		t.Fatal(err)
	}

	// ReportPlaybackStopped with empty ItemId (uses NowPlayingItem & its RunTimeTicks) and negative PositionTicks
	if err := mgr.ReportPlaybackStopped(ctx, user, client2, PlaybackStoppedRequest{
		ItemId:        "",
		PositionTicks: -50,
	}); err != nil {
		t.Fatal(err)
	}

	// RemoveSession & RemoveSessionByDevice
	mgr.RemoveSession(user.ID, "nonexistent")
	mgr.RemoveSession(user.ID, "default-device")
	mgr.RemoveSessionByDevice("nonexistent")
	mgr.RemoveSessionByDevice("dev-2")
	mgr.RemoveSessionByDevice("dev-3")
	if len(mgr.ListSessions()) != 0 {
		t.Fatalf("expected 0 sessions after removals, got %d", len(mgr.ListSessions()))
	}
}

func TestSessionManagerNilRepoAndNilBus(t *testing.T) {
	cfg := config.Default(t.TempDir())
	cfg.PlayedThreshold = 0 // triggers defaultPlayedThreshold fallback
	mgr := NewManager(nil, cfg, nil)

	user := &sqlite.UserRecord{ID: guid.New(), Name: "guest"}
	client := auth.ClientInfo{DeviceID: "d-nil"}
	ctx := context.Background()

	// Stop with nil repo triggers the `else if itemID != ""` event branch, and nil bus is safely ignored.
	if err := mgr.ReportPlaybackStopped(ctx, user, client, PlaybackStoppedRequest{
		ItemId:        guid.New(),
		PositionTicks: 9500,
		RuntimeTicks:  10000,
	}); err != nil {
		t.Fatal(err)
	}
}
