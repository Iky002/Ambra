package session

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestReportPlaybackStartHydratesNowPlayingItem(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	cfg, err := config.Load(filepath.Join(tmp, "data"))
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	store, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	userRepo := sqlite.NewUserRepo(store)
	libRepo := sqlite.NewLibraryRepo(store)
	mgr := NewManager(userRepo, cfg, eventbus.New())
	mgr.SetLibraryRepo(libRepo)

	u := &sqlite.UserRecord{
		ID:        guid.New(),
		Name:      "alice",
		Policy:    sqlite.DefaultUserPolicy(false),
		Config:    sqlite.DefaultUserConfig(),
		CreatedAt: time.Now().UTC(),
	}
	if err := userRepo.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	lib, err := libRepo.CreateLibrary(ctx, "TV", "tvshows", []string{tmp})
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}
	seriesID := guid.New()
	seasonID := guid.New()
	epID := guid.New()
	rt := int64(36_000_000_000)
	yr := 2024
	epNum := 3
	sNum := 1
	if err := libRepo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:                epID,
		LibraryID:         lib.ID,
		ParentID:          seasonID,
		SeriesID:          seriesID,
		SeasonID:          seasonID,
		Type:              "Episode",
		MediaType:         "Video",
		Name:              "The Pilot",
		SortName:          "001-0003",
		IndexNumber:       &epNum,
		ParentIndexNumber: &sNum,
		ProductionYear:    &yr,
		RuntimeTicks:      &rt,
		Container:         "mkv",
		Path:              "/tv/show/s01e03.mkv",
		Etag:              "ep3",
		Extra: sqlite.ItemExtra{
			SeriesName: "Great Show",
		},
	}); err != nil {
		t.Fatalf("UpsertItem: %v", err)
	}

	client := auth.ClientInfo{Client: "Ambra Web", Device: "Chrome", DeviceID: "dev-1"}

	// Case 1: Only ItemId provided (Item is nil)
	if err := mgr.ReportPlaybackStart(ctx, u, client, PlaybackStartRequest{
		ItemId:        epID,
		PositionTicks: 1000,
	}); err != nil {
		t.Fatalf("ReportPlaybackStart: %v", err)
	}

	sessions := mgr.ListSessions()
	if len(sessions) != 1 || sessions[0].NowPlayingItem == nil {
		t.Fatalf("expected 1 active session with NowPlayingItem, got %+v", sessions)
	}
	np := sessions[0].NowPlayingItem
	if np.Name != "The Pilot" || np.Type != "Episode" || np.SeriesName != "Great Show" {
		t.Fatalf("unexpected hydrated NowPlayingItem: %+v", np)
	}
	if np.RunTimeTicks == nil || *np.RunTimeTicks != rt {
		t.Fatalf("expected RunTimeTicks=%d, got %v", rt, np.RunTimeTicks)
	}
	if np.ProductionYear == nil || *np.ProductionYear != yr {
		t.Fatalf("expected ProductionYear=%d, got %v", yr, np.ProductionYear)
	}
	if np.IndexNumber == nil || *np.IndexNumber != epNum || np.ParentIndexNumber == nil || *np.ParentIndexNumber != sNum {
		t.Fatalf("expected S%dE%d, got S%vE%v", sNum, epNum, np.ParentIndexNumber, np.IndexNumber)
	}

	// Case 2: Partial Item provided with empty Name -> hydrates missing fields
	if err := mgr.ReportPlaybackStart(ctx, u, client, PlaybackStartRequest{
		Item: &dto.BaseItemDto{Id: epID},
	}); err != nil {
		t.Fatalf("ReportPlaybackStart partial item: %v", err)
	}
	sessions = mgr.ListSessions()
	if sessions[0].NowPlayingItem == nil || sessions[0].NowPlayingItem.Name != "The Pilot" {
		t.Fatalf("expected partial Item to be hydrated, got %+v", sessions[0].NowPlayingItem)
	}
}
