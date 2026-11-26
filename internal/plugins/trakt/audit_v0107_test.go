package trakt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestAuditV0107_H9_HostChangeClearsCredsAndM17_NoAutoBindAndDedupRace(t *testing.T) {
	// 1. H-9: Host change without new creds clears clientID and accessToken
	p := New()
	p.SetHTTPClient(nil)
	if err := p.Configure(map[string]any{
		"enabled":      true,
		"client_id":    "cid-secret",
		"access_token": "tok-secret",
		"base_url":     "https://api.trakt.tv",
	}); err != nil {
		t.Fatal(err)
	}
	if err := p.Configure(map[string]any{
		"base_url": "https://attacker.example.com",
	}); err != nil {
		t.Fatal(err)
	}
	snap := p.snapshot()
	if snap.clientID != "" || snap.accessToken != "" {
		t.Fatalf("expected clientID and accessToken cleared on host change, got cid=%q tok=%q", snap.clientID, snap.accessToken)
	}
	if urlHostLower("://bad") != "" {
		t.Fatal("expected empty host on invalid URL")
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
	_ = p2.Configure(map[string]any{
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
