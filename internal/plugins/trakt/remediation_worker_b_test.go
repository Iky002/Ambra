package trakt

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

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
