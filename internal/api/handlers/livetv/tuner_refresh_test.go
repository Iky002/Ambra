package livetv

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestRefreshTunerHostM3ULosslessMergeAndFallback(t *testing.T) {
	h, mux, _, _ := setupLiveTVTest(t)
	defer h.Close()
	ctx := context.Background()

	var (
		mu       sync.Mutex
		playlist = `#EXTM3U
#EXTINF:-1 tvg-id="cctv1" tvg-chno="1" group-title="News",CCTV-1
http://192.168.10.2/live/cctv1-v1.m3u8
#EXTINF:-1 group-title="Sports",Unique NoTvg
http://192.168.10.2/live/unique-v1.ts
#EXTINF:-1 group-title="Misc",Dup Name
http://192.168.10.2/live/dup-a-v1.ts
#EXTINF:-1 group-title="Misc",Dup Name
http://192.168.10.2/live/dup-b-v1.ts
`
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		body := playlist
		mu.Unlock()
		w.Header().Set("Content-Type", "audio/x-mpegurl")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	h.SetHTTPClient(srv.Client())

	// Save initial remote M3U tuner host.
	rec := doJSON(t, mux, http.MethodPost, "/LiveTv/TunerHosts", map[string]any{
		"Id":   "tuner-m3u-1",
		"Type": "m3u",
		"Url":  srv.URL + "/list.m3u",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("initial save tuner host failed: %d %s", rec.Code, rec.Body.String())
	}

	var cctvID, uniqueID, dupAID string
	for _, ch := range h.SnapshotChannels() {
		switch ch.Name {
		case "CCTV-1":
			cctvID = ch.Id
		case "Unique NoTvg":
			uniqueID = ch.Id
		case "Dup Name":
			if dupAID == "" {
				dupAID = ch.Id
			}
		}
	}
	if cctvID == "" || uniqueID == "" || dupAID == "" {
		t.Fatalf("expected seeded channels, got cctv=%q unique=%q dup=%q", cctvID, uniqueID, dupAID)
	}

	// Customize Disabled, SortIndex, Programs, and Timers.
	if err := h.SetChannelDisabled(ctx, cctvID, true); err != nil {
		t.Fatal(err)
	}
	if err := h.SetChannelSortIndex(ctx, cctvID, 42); err != nil {
		t.Fatal(err)
	}
	if err := h.SetChannelDisabled(ctx, uniqueID, true); err != nil {
		t.Fatal(err)
	}
	if err := h.SetChannelSortIndex(ctx, uniqueID, 77); err != nil {
		t.Fatal(err)
	}
	if err := h.SetChannelDisabled(ctx, dupAID, true); err != nil {
		t.Fatal(err)
	}
	if err := h.SetChannelSortIndex(ctx, dupAID, 99); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Minute)
	h.mu.Lock()
	h.state.Programs = append(h.state.Programs,
		ProgramInfo{
			Id:        "prog-epg-cctv1",
			Name:      "Evening News",
			Type:      "Program",
			ChannelId: cctvID,
			StartDate: now,
			EndDate:   now.Add(time.Hour),
		},
		ProgramInfo{
			Id:        "prog-epg-unique",
			Name:      "Championship Live",
			Type:      "Program",
			ChannelId: uniqueID,
			StartDate: now,
			EndDate:   now.Add(time.Hour),
		},
	)
	h.state.Timers = append(h.state.Timers,
		TimerInfo{
			Id:        "timer-cctv1",
			ChannelId: cctvID,
			ProgramId: "prog-epg-cctv1",
			Name:      "Evening News",
			Status:    "New",
			StartDate: now.Add(time.Hour),
			EndDate:   now.Add(2 * time.Hour),
		},
		TimerInfo{
			Id:        "timer-unique",
			ChannelId: uniqueID,
			ProgramId: "prog-epg-unique",
			Name:      "Championship Live",
			Status:    "New",
			StartDate: now.Add(time.Hour),
			EndDate:   now.Add(2 * time.Hour),
		},
	)
	h.mu.Unlock()

	// Update upstream playlist: all stream URLs rotate.
	mu.Lock()
	playlist = `#EXTM3U
#EXTINF:-1 tvg-id="cctv1" tvg-chno="1" group-title="News",CCTV-1 HD
http://192.168.10.2/live/cctv1-v2.m3u8
#EXTINF:-1 group-title="Sports",  unique notvg  
http://192.168.10.2/live/unique-v2.ts
#EXTINF:-1 group-title="Misc",Dup Name
http://192.168.10.2/live/dup-a-v2.ts
#EXTINF:-1 group-title="Misc",Dup Name
http://192.168.10.2/live/dup-b-v2.ts
`
	mu.Unlock()

	updated, err := h.RefreshTunerHost(ctx, "tuner-m3u-1")
	if err != nil {
		t.Fatalf("RefreshTunerHost failed: %v", err)
	}
	if updated.ImportedChannelCount != 4 {
		t.Fatalf("expected ImportedChannelCount=4, got %d", updated.ImportedChannelCount)
	}

	// Verify tvg-id channel preserved ID, Disabled, SortIndex, Programs, and Timer link while updating URL.
	cctvCh, ok := h.GetChannelByID(cctvID)
	if !ok {
		t.Fatalf("expected CCTV-1 to keep deterministic ID %q after URL rotation", cctvID)
	}
	if cctvCh.StreamUrl != "http://192.168.10.2/live/cctv1-v2.m3u8" {
		t.Fatalf("expected updated StreamUrl, got %q", cctvCh.StreamUrl)
	}
	if !cctvCh.Disabled || cctvCh.SortIndex != 42 {
		t.Fatalf("expected Disabled=true and SortIndex=42 preserved, got disabled=%v sort=%d", cctvCh.Disabled, cctvCh.SortIndex)
	}

	var foundEPGProgram, foundUniqueProgram bool
	for _, p := range h.SnapshotPrograms() {
		if p.Id == "prog-epg-cctv1" && p.ChannelId == cctvID {
			foundEPGProgram = true
		}
		if p.Id == "prog-epg-unique" && p.ChannelId == uniqueID {
			foundUniqueProgram = true
		}
	}
	if !foundEPGProgram {
		t.Fatal("expected existing EPG program on CCTV-1 to be preserved across refresh")
	}
	if !foundUniqueProgram {
		t.Fatal("expected existing EPG program on Unique NoTvg channel to survive URL rotation")
	}

	h.mu.RLock()
	var foundTimer, foundUniqueTimer bool
	for _, tm := range h.state.Timers {
		if tm.Id == "timer-cctv1" && tm.ChannelId == cctvID {
			foundTimer = true
		}
		if tm.Id == "timer-unique" && tm.ChannelId == uniqueID {
			foundUniqueTimer = true
		}
	}
	h.mu.RUnlock()
	if !foundTimer {
		t.Fatal("expected DVR timer linked to CCTV-1 to remain intact")
	}
	if !foundUniqueTimer {
		t.Fatal("expected DVR timer linked to Unique NoTvg channel to remain valid after URL rotation")
	}

	// Verify fallback ID & metadata preservation for unique name without tvg-id when URL changed.
	var foundUnique bool
	var dupInherited int
	for _, ch := range h.SnapshotChannels() {
		if strings.EqualFold(strings.TrimSpace(ch.Name), "Unique NoTvg") {
			foundUnique = true
			if ch.Id != uniqueID {
				t.Fatalf("expected channel ID %q preserved when unique name has no tvg-id and URL changed, got %q", uniqueID, ch.Id)
			}
			if ch.StreamUrl != "http://192.168.10.2/live/unique-v2.ts" {
				t.Fatalf("expected updated unique stream URL, got %q", ch.StreamUrl)
			}
			if !ch.Disabled || ch.SortIndex != 77 {
				t.Fatalf("expected fallback inheritance Disabled=true SortIndex=77, got disabled=%v sort=%d", ch.Disabled, ch.SortIndex)
			}
		}
		if ch.Name == "Dup Name" && (ch.Disabled || ch.SortIndex != 0) {
			dupInherited++
		}
	}
	if !foundUnique {
		t.Fatal("expected Unique NoTvg channel after refresh")
	}
	if dupInherited != 0 {
		t.Fatalf("expected ambiguous duplicate-named channels not to inherit Disabled/SortIndex, got %d", dupInherited)
	}
}

func TestRefreshTunerHostEmptyAndUpstreamErrorPreserveChannels(t *testing.T) {
	h, mux, _, _ := setupLiveTVTest(t)
	defer h.Close()
	ctx := context.Background()

	var mode atomic.Int32 // 0=valid, 1=empty, 2=500
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch mode.Load() {
		case 0:
			w.Header().Set("Content-Type", "audio/x-mpegurl")
			_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:-1 tvg-id=\"ch1\",Ch One\nhttp://192.168.10.2/1.ts\n"))
		case 1:
			w.Header().Set("Content-Type", "audio/x-mpegurl")
			_, _ = w.Write([]byte("#EXTM3U\n"))
		case 2:
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("upstream down"))
		}
	}))
	defer srv.Close()
	h.SetHTTPClient(srv.Client())

	if rec := doJSON(t, mux, http.MethodPost, "/LiveTv/TunerHosts", map[string]any{
		"Id":   "tuner-guard",
		"Type": "m3u",
		"Url":  srv.URL + "/channels.m3u",
	}); rec.Code != http.StatusOK {
		t.Fatalf("seed tuner failed: %d %s", rec.Code, rec.Body.String())
	}
	initialChannels := h.SnapshotChannels()
	if len(initialChannels) != 1 {
		t.Fatalf("expected 1 initial channel, got %d", len(initialChannels))
	}

	// Case 1: 200 OK with 0 channels must return error and NOT wipe existing channels.
	mode.Store(1)
	if _, err := h.RefreshTunerHost(ctx, "tuner-guard"); !errors.Is(err, errTunerHostEmptyRefresh) {
		t.Fatalf("expected errTunerHostEmptyRefresh on empty M3U, got %v", err)
	}
	afterEmpty := h.SnapshotChannels()
	if len(afterEmpty) != 1 || afterEmpty[0].Id != initialChannels[0].Id {
		t.Fatalf("expected existing channel preserved after empty M3U, got %+v", afterEmpty)
	}

	// Case 2: upstream 500 error must return error and NOT wipe existing channels.
	mode.Store(2)
	if _, err := h.RefreshTunerHost(ctx, "tuner-guard"); err == nil {
		t.Fatal("expected error on upstream 500")
	}
	afterErr := h.SnapshotChannels()
	if len(afterErr) != 1 || afterErr[0].Id != initialChannels[0].Id {
		t.Fatalf("expected existing channel preserved after upstream 500, got %+v", afterErr)
	}
}

func TestRefreshAllTunerHostsSkipsInlineAndContinuesOnFailure(t *testing.T) {
	h, mux, _, _ := setupLiveTVTest(t)
	defer h.Close()
	ctx := context.Background()

	var healthyV2 atomic.Bool
	var brokenDown atomic.Bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthy.m3u":
			w.Header().Set("Content-Type", "audio/x-mpegurl")
			if healthyV2.Load() {
				_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:-1 tvg-id=\"h1\",Healthy\nhttp://192.168.10.2/healthy-v2.ts\n"))
			} else {
				_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:-1 tvg-id=\"h1\",Healthy\nhttp://192.168.10.2/healthy-v1.ts\n"))
			}
		case "/broken.m3u":
			if brokenDown.Load() {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			w.Header().Set("Content-Type", "audio/x-mpegurl")
			_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:-1 tvg-id=\"b1\",Broken\nhttp://192.168.10.2/broken-v1.ts\n"))
		}
	}))
	defer srv.Close()
	h.SetHTTPClient(srv.Client())

	// 1. Inline pasted M3U (Url == "")
	if rec := doJSON(t, mux, http.MethodPost, "/LiveTv/TunerHosts", map[string]any{
		"Id":         "tuner-inline",
		"Type":       "m3u",
		"M3UContent": "#EXTM3U\n#EXTINF:-1 tvg-id=\"in1\",Inline Ch\nhttp://192.168.10.2/inline.ts\n",
	}); rec.Code != http.StatusOK {
		t.Fatalf("save inline tuner: %d", rec.Code)
	}
	// 2. Broken remote M3U (initially works, then fails during refresh)
	if rec := doJSON(t, mux, http.MethodPost, "/LiveTv/TunerHosts", map[string]any{
		"Id":   "tuner-broken",
		"Type": "m3u",
		"Url":  srv.URL + "/broken.m3u",
	}); rec.Code != http.StatusOK {
		t.Fatalf("save broken tuner: %d", rec.Code)
	}
	// 3. Healthy remote M3U
	if rec := doJSON(t, mux, http.MethodPost, "/LiveTv/TunerHosts", map[string]any{
		"Id":   "tuner-healthy",
		"Type": "m3u",
		"Url":  srv.URL + "/healthy.m3u",
	}); rec.Code != http.StatusOK {
		t.Fatalf("save healthy tuner: %d", rec.Code)
	}

	// First test RefreshAllTunerHosts when only inline + healthy succeed: inline must be skipped without error.
	if err := h.RefreshAllTunerHosts(ctx); err != nil {
		t.Fatalf("expected RefreshAllTunerHosts to skip inline host cleanly, got err=%v", err)
	}

	// Now break tuner-broken and update tuner-healthy to v2.
	brokenDown.Store(true)
	healthyV2.Store(true)

	err := h.RefreshAllTunerHosts(ctx)
	if err == nil {
		t.Fatal("expected RefreshAllTunerHosts to report error from broken host")
	}

	// Verify: inline channel untouched, broken channel untouched, healthy channel updated to v2.
	byTvg := make(map[string]ChannelInfo)
	for _, ch := range h.SnapshotChannels() {
		byTvg[ch.TvgId] = ch
	}
	if byTvg["in1"].StreamUrl != "http://192.168.10.2/inline.ts" {
		t.Fatalf("inline channel altered: %+v", byTvg["in1"])
	}
	if byTvg["b1"].StreamUrl != "http://192.168.10.2/broken-v1.ts" {
		t.Fatalf("broken host channel should be preserved: %+v", byTvg["b1"])
	}
	if byTvg["h1"].StreamUrl != "http://192.168.10.2/healthy-v2.ts" {
		t.Fatalf("healthy host should have updated despite broken host error: %+v", byTvg["h1"])
	}
}

func TestRefreshTunerHostHDHomeRun(t *testing.T) {
	h, mux, _, _ := setupLiveTVTest(t)
	defer h.Close()
	ctx := context.Background()

	var version atomic.Int32
	hdSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/discover.json":
			_, _ = w.Write([]byte(`{"FriendlyName":"HDHR Prime","DeviceID":"HDHR-7777","TunerCount":3}`))
		case "/lineup.json":
			if version.Load() == 0 {
				_, _ = w.Write([]byte(`[
					{"GuideNumber":"4.1","GuideName":"CBS HD","URL":"http://192.168.1.50:5004/auto/v4.1","Favorite":1,"HD":1}
				]`))
			} else {
				_, _ = w.Write([]byte(`[
					{"GuideNumber":"4.1","GuideName":"CBS HD","URL":"http://192.168.1.50:5004/auto/v4.1-new","Favorite":1,"HD":1},
					{"GuideNumber":"7.1","GuideName":"ABC HD","URL":"http://192.168.1.50:5004/auto/v7.1","Favorite":1,"HD":1}
				]`))
			}
		}
	}))
	defer hdSrv.Close()
	h.SetHTTPClient(hdSrv.Client())

	if rec := doJSON(t, mux, http.MethodPost, "/LiveTv/TunerHosts", map[string]any{
		"Id":   "th-hdhr-ref",
		"Type": "hdhomerun",
		"Url":  hdSrv.URL,
	}); rec.Code != http.StatusOK {
		t.Fatalf("save hdhomerun: %d %s", rec.Code, rec.Body.String())
	}

	// Clear metadata in state to verify refresh populates empty metadata fields from discover.json.
	h.mu.Lock()
	for i := range h.state.TunerHosts {
		if h.state.TunerHosts[i].Id == "th-hdhr-ref" {
			h.state.TunerHosts[i].FriendlyName = ""
			h.state.TunerHosts[i].DeviceId = ""
			h.state.TunerHosts[i].TunerCount = 0
		}
	}
	h.mu.Unlock()

	version.Store(1)
	updated, err := h.RefreshTunerHost(ctx, "th-hdhr-ref")
	if err != nil {
		t.Fatalf("RefreshTunerHost HDHomeRun failed: %v", err)
	}
	if updated.ImportedChannelCount != 2 || updated.FriendlyName != "HDHR Prime" || updated.DeviceId != "HDHR-7777" || updated.TunerCount != 3 {
		t.Fatalf("unexpected refreshed HDHomeRun metadata: %+v", updated)
	}
	if len(h.SnapshotChannels()) != 2 {
		t.Fatalf("expected 2 HDHomeRun channels after refresh, got %d", len(h.SnapshotChannels()))
	}
}

func TestHandleRefreshTunerHostHTTPStatusesAndRBAC(t *testing.T) {
	h, _, st, cfg := setupLiveTVTest(t)
	defer h.Close()
	ctx := context.Background()

	userRepo := sqlite.NewUserRepo(st)
	h.SetUserRepo(userRepo)
	authSvc := auth.NewService(userRepo, cfg)
	if _, err := authSvc.CreateUser(ctx, "adm_ref", "pass1234", true); err != nil {
		t.Fatal(err)
	}
	if _, err := authSvc.CreateUser(ctx, "usr_ref", "pass1234", false); err != nil {
		t.Fatal(err)
	}
	admAuth, err := authSvc.AuthenticateByName(ctx, "adm_ref", "pass1234", "", auth.ClientInfo{DeviceID: "d-adm"})
	if err != nil {
		t.Fatal(err)
	}
	usrAuth, err := authSvc.AuthenticateByName(ctx, "usr_ref", "pass1234", "", auth.ClientInfo{DeviceID: "d-usr"})
	if err != nil {
		t.Fatal(err)
	}

	authMW := middleware.NewAuth(authSvc, nil)
	authMux := http.NewServeMux()
	h.RegisterRoutes(authMux, AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
	})

	var failUpstream atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failUpstream.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "audio/x-mpegurl")
		_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:-1 tvg-id=\"k1\",Ch K1\nhttp://192.168.10.2/k1.ts\n"))
	}))
	defer srv.Close()
	h.SetHTTPClient(srv.Client())

	h.mu.Lock()
	h.state.TunerHosts = []TunerHostInfo{
		{Id: "th-ok", Type: "m3u", Url: srv.URL + "/ok.m3u", AllowPrivateNetwork: true},
		{Id: "th-inline", Type: "m3u", Url: ""},
	}
	h.mu.Unlock()

	doRefresh := func(id, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/LiveTv/TunerHosts/"+id+"/Refresh", nil)
		if token != "" {
			req.Header.Set("X-Emby-Token", token)
		}
		rec := httptest.NewRecorder()
		authMux.ServeHTTP(rec, req)
		return rec
	}

	// Non-admin -> 403 Forbidden
	if rec := doRefresh("th-ok", usrAuth.AccessToken); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin, got %d %s", rec.Code, rec.Body.String())
	}

	// Admin 200 OK
	recOK := doRefresh("th-ok", admAuth.AccessToken)
	if recOK.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid refresh, got %d %s", recOK.Code, recOK.Body.String())
	}
	var gotHost TunerHostInfo
	if err := json.Unmarshal(recOK.Body.Bytes(), &gotHost); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if gotHost.Id != "th-ok" || gotHost.ImportedChannelCount != 1 {
		t.Fatalf("unexpected 200 payload: %+v", gotHost)
	}

	// 404 Not Found
	if rec := doRefresh("does-not-exist", admAuth.AccessToken); rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown tuner host, got %d %s", rec.Code, rec.Body.String())
	}

	// 400 Bad Request for manual inline M3U host (no Url)
	if rec := doRefresh("th-inline", admAuth.AccessToken); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for inline tuner host without Url, got %d %s", rec.Code, rec.Body.String())
	}

	// 502 Bad Gateway on upstream failure
	failUpstream.Store(true)
	if rec := doRefresh("th-ok", admAuth.AccessToken); rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 on upstream failure, got %d %s", rec.Code, rec.Body.String())
	}

	// Verify concurrent deletion during refresh returns os.ErrNotExist without mutating state.
	failUpstream.Store(false)
	gate := make(chan struct{})
	delSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-gate
		w.Header().Set("Content-Type", "audio/x-mpegurl")
		_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:-1 tvg-id=\"z1\",Ghost\nhttp://192.168.10.2/z1.ts\n"))
	}))
	defer delSrv.Close()
	h.SetHTTPClient(delSrv.Client())
	h.mu.Lock()
	h.state.TunerHosts = append(h.state.TunerHosts, TunerHostInfo{Id: "th-race", Type: "m3u", Url: delSrv.URL + "/race.m3u"})
	h.mu.Unlock()

	errCh := make(chan error, 1)
	go func() {
		_, err := h.RefreshTunerHost(ctx, "th-race")
		errCh <- err
	}()
	time.Sleep(30 * time.Millisecond)
	h.DeleteTunerHost(ctx, "th-race")
	close(gate)
	if err := <-errCh; !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist when tuner deleted concurrently, got %v", err)
	}
	for _, ch := range h.SnapshotChannels() {
		if ch.TunerHostId == "th-race" {
			t.Fatalf("unexpected ghost channel persisted for deleted tuner: %+v", ch)
		}
	}
}

func TestRefreshTunerHostConcurrentConfigChangeAbortsWithoutClobber(t *testing.T) {
	h, mux, _, _ := setupLiveTVTest(t)
	defer h.Close()

	enteredFetch := make(chan struct{})
	releaseFetch := make(chan struct{})
	var fetchOnce sync.Once

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/x-mpegurl")
		switch r.URL.Path {
		case "/old.m3u":
			fetchOnce.Do(func() { close(enteredFetch) })
			<-releaseFetch
			_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:-1 tvg-id=\"stale\",Stale Ch\nhttp://192.168.10.2/stale.ts\n"))
		case "/new.m3u":
			_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:-1 tvg-id=\"fresh\",Fresh Ch\nhttp://192.168.10.2/fresh.ts\n"))
		}
	}))
	defer srv.Close()
	h.SetHTTPClient(srv.Client())

	h.mu.Lock()
	h.state.TunerHosts = []TunerHostInfo{{
		Id:                  "th-toctou",
		Type:                "m3u",
		Url:                 srv.URL + "/old.m3u",
		AllowPrivateNetwork: true,
	}}
	h.mu.Unlock()

	recCh := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := httptest.NewRequest(http.MethodPost, "/LiveTv/TunerHosts/th-toctou/Refresh", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		recCh <- rec
	}()

	<-enteredFetch

	// Concurrently update the tuner host URL to /new.m3u while old refresh is in-flight.
	saveRec := doJSON(t, mux, http.MethodPost, "/LiveTv/TunerHosts", map[string]any{
		"Id":   "th-toctou",
		"Type": "m3u",
		"Url":  srv.URL + "/new.m3u",
	})
	if saveRec.Code != http.StatusOK {
		close(releaseFetch)
		t.Fatalf("concurrent save failed: %d %s", saveRec.Code, saveRec.Body.String())
	}

	close(releaseFetch)
	refreshRec := <-recCh
	if refreshRec.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict when tuner host config changed during refresh, got %d %s", refreshRec.Code, refreshRec.Body.String())
	}

	channels := h.SnapshotChannels()
	if len(channels) != 1 || channels[0].TvgId != "fresh" {
		t.Fatalf("expected concurrent save's Fresh Ch to remain un-clobbered, got %+v", channels)
	}
}

func TestLiveTVM3UAndXMLTVAllowPrivateNetworkAndLegacyCompat(t *testing.T) {
	h, mux, _, _ := setupLiveTVTest(t)
	defer h.Close()
	ctx := context.Background()

	// 1. Default handler (without custom HTTP client) blocks private LAN URL when AllowPrivateNetwork=false.
	if _, err := h.loadM3UFromSourceWithPolicy(ctx, "http://192.168.1.50/playlist.m3u", "", false); err == nil {
		t.Fatal("expected loadM3UFromSourceWithPolicy(..., allowPrivateLAN=false) to block 192.168.1.50")
	}
	if _, err := h.loadXMLTVFromSourceWithPolicy(ctx, "http://192.168.1.50/epg.xml", false); err == nil {
		t.Fatal("expected loadXMLTVFromSourceWithPolicy(..., allowPrivateLAN=false) to block 192.168.1.50")
	}

	// 2. Public metadata client CheckRedirect blocks redirect to RFC1918 private LAN,
	//    and post-redirect validation blocks smuggled private LAN final URLs even under custom RoundTrippers.
	pubClient := h.selectMetadataHTTPClient(false)
	if pubClient == nil || pubClient.CheckRedirect == nil {
		t.Fatal("expected public metadata HTTP client with CheckRedirect")
	}
	originReq, _ := http.NewRequest(http.MethodGet, "http://93.184.216.34/public.m3u", nil)
	lanRedirReq, _ := http.NewRequest(http.MethodGet, "http://192.168.1.50/internal.m3u", nil)
	if err := pubClient.CheckRedirect(lanRedirReq, []*http.Request{originReq}); err == nil {
		t.Fatal("expected public metadata client CheckRedirect to block redirect to 192.168.1.50")
	}

	redirSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/guide.xml" {
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><tv><channel id="c1"><display-name>C1</display-name></channel><programme start="20300101120000 +0000" stop="20300101130000 +0000" channel="c1"><title>News</title></programme></tv>`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer redirSrv.Close()

	smuggledLANReq, _ := http.NewRequest(http.MethodGet, "http://192.168.1.50/internal", nil)
	h.SetHTTPClient(&http.Client{
		Transport: rtFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       http.NoBody,
				Request:    smuggledLANReq,
			}, nil
		}),
	})
	if _, err := h.loadM3UFromSourceWithPolicy(ctx, "http://93.184.216.34/public.m3u", "", false); err == nil {
		t.Fatal("expected M3U post-redirect validation to block private LAN when allowPrivateLAN=false")
	}
	if _, err := h.loadXMLTVFromSourceWithPolicy(ctx, "http://93.184.216.34/public.xml", false); err == nil {
		t.Fatal("expected XMLTV post-redirect validation to block private LAN when allowPrivateLAN=false")
	}
	h.SetHTTPClient(redirSrv.Client())

	// 3. Saving XMLTV ListingProvider persists AllowPrivateNetwork=true & NetworkPolicySet=true on private/test URL.
	saveLP := doJSON(t, mux, http.MethodPost, "/LiveTv/ListingProviders", map[string]any{
		"Id":   "lp-lan-1",
		"Type": "xmltv",
		"Path": redirSrv.URL + "/guide.xml",
	})
	if saveLP.Code != http.StatusOK {
		t.Fatalf("save listing provider failed: %d %s", saveLP.Code, saveLP.Body.String())
	}
	providers := h.SnapshotListingProviders()
	if len(providers) != 1 || !providers[0].AllowPrivateNetwork || !providers[0].NetworkPolicySet {
		t.Fatalf("expected AllowPrivateNetwork=true and NetworkPolicySet=true persisted, got %+v", providers)
	}

	// 4. Legacy ListingProviderInfo row (NetworkPolicySet=false, AllowPrivateNetwork=false) on LAN URL still refreshes cleanly,
	//    while explicitly policy-set public row (NetworkPolicySet=true, AllowPrivateNetwork=false) blocks LAN URL!
	h.mu.Lock()
	h.state.Channels = []ChannelInfo{{Id: "ch-c1", Name: "C1", TvgId: "c1"}}
	h.state.ListingProviders = []ListingProviderInfo{
		{Id: "lp-legacy-lan", Type: "xmltv", Path: redirSrv.URL + "/guide.xml", AllowPrivateNetwork: false, NetworkPolicySet: false},
		{Id: "lp-public-pinned", Type: "xmltv", Path: "http://192.168.1.50/epg.xml", AllowPrivateNetwork: false, NetworkPolicySet: true},
	}
	h.mu.Unlock()

	err := h.RefreshAllGuides(ctx)
	if err == nil {
		t.Fatal("expected RefreshAllGuides to report error from lp-public-pinned blocking 192.168.1.50")
	}
	if len(h.SnapshotPrograms()) == 0 {
		t.Fatal("expected legacy LAN listing provider (NetworkPolicySet=false) to refresh EPG programs successfully")
	}
}

func TestLiveTVHLSCaseInsensitiveAndUnquotedURIAndStreamSlots(t *testing.T) {
	h, mux, _, cfg := setupLiveTVTest(t)
	defer h.Close()

	// 1. Case-insensitive uri="..." and unquoted URI=... / uri=... rewriting + unsafe URI stripping.
	pubBase, _ := url.Parse("https://93.184.216.34/live/index.m3u8")
	rawPlaylist := strings.Join([]string{
		"#EXTM3U",
		`#EXT-X-KEY:METHOD=AES-128,uri="keys/k1.bin",IV=0x01`,
		`#EXT-X-MAP:URI=init.mp4,BYTERANGE="720@0"`,
		`#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="aud",NAME="Eng",uri=audio/eng.m3u8`,
		`#EXT-X-KEY:METHOD=AES-128,uri=http://169.254.169.254/latest/meta-data/`,
		`#EXT-X-MAP:URI=file:///etc/passwd`,
		"seg0.ts",
	}, "\n")
	rewritten := h.rewriteHLSPlaylistWithPolicy(rawPlaylist, pubBase, "ch-uri", "Stream", "", false)
	if strings.Contains(rewritten, "169.254.169.254") || strings.Contains(rewritten, "etc/passwd") {
		t.Fatalf("unsafe unquoted/lowercase URIs leaked in rewritten playlist:\n%s", rewritten)
	}
	if strings.Count(rewritten, "Stream?target=") != 4 {
		t.Fatalf("expected 4 rewritten URIs (lowercase quoted key, unquoted map, lowercase unquoted media, segment), got:\n%s", rewritten)
	}

	// 2. SimultaneousStreamLimit enforced on non-HEAD initial LiveTV stream requests without leaking on HLS polls or sub-targets.
	mgr := transcode.NewManager(cfg, transcode.WithCapabilities(transcode.SoftwareOnlyCapabilities()))
	defer mgr.Close()
	h.SetTranscodeManager(mgr)
	cfg.SimultaneousStreamLimit = 1

	var hlsUpstreamURL string
	hlsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/live.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:4.0,\n" + hlsUpstreamURL + "/seg0.ts\n"))
		case "/seg0.ts":
			w.Header().Set("Content-Type", "video/mp2t")
			_, _ = w.Write([]byte("HLS-SEG-DATA"))
		}
	}))
	defer hlsSrv.Close()
	hlsUpstreamURL = hlsSrv.URL
	h.SetStreamClient(hlsSrv.Client())

	h.mu.Lock()
	h.state.TunerHosts = []TunerHostInfo{{Id: "th-slot", Type: "m3u", AllowPrivateNetwork: true}}
	h.state.Channels = []ChannelInfo{{
		Id:          "ch-slot-hls",
		Name:        "Slot HLS",
		Type:        "TvChannel",
		ChannelType: "TV",
		StreamUrl:   hlsSrv.URL + "/live.m3u8",
		TunerHostId: "th-slot",
	}}
	h.mu.Unlock()

	// Multiple sequential HLS manifest polls must NOT leak stream slots when SimultaneousStreamLimit == 1!
	var signedSegPath string
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/LiveTv/Channels/ch-slot-hls/Stream", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("HLS manifest poll #%d expected 200, got %d: %s", i, rec.Code, rec.Body.String())
		}
		for _, ln := range strings.Split(rec.Body.String(), "\n") {
			ln = strings.TrimSpace(ln)
			if strings.HasPrefix(ln, "Stream?target=") {
				signedSegPath = "/LiveTv/Channels/ch-slot-hls/" + ln
			}
		}
	}
	if signedSegPath == "" {
		t.Fatal("expected rewritten signed segment URL in HLS manifest")
	}

	// Occupy the single global stream slot -> new initial GET returns 429, while HEAD and signed sub-target segment still succeed!
	relOccupied, ok := mgr.TryAcquireStreamSlot(1, "other-user-video-transcode")
	if !ok {
		t.Fatal("expected to acquire free stream slot after HLS manifest polls completed")
	}

	recBlocked := httptest.NewRecorder()
	mux.ServeHTTP(recBlocked, httptest.NewRequest(http.MethodGet, "/LiveTv/Channels/ch-slot-hls/Stream", nil))
	if recBlocked.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on initial LiveTV GET when SimultaneousStreamLimit=1 full, got %d", recBlocked.Code)
	}

	recHead := httptest.NewRecorder()
	mux.ServeHTTP(recHead, httptest.NewRequest(http.MethodHead, "/LiveTv/Channels/ch-slot-hls/Stream", nil))
	if recHead.Code != http.StatusOK {
		t.Fatalf("expected 200 on LiveTV HEAD even when SimultaneousStreamLimit=1 full, got %d", recHead.Code)
	}

	recSeg := httptest.NewRecorder()
	mux.ServeHTTP(recSeg, httptest.NewRequest(http.MethodGet, signedSegPath, nil))
	if recSeg.Code != http.StatusOK || recSeg.Body.String() != "HLS-SEG-DATA" {
		t.Fatalf("expected signed HLS sub-target segment not to consume extra slot (200), got %d %q", recSeg.Code, recSeg.Body.String())
	}

	relOccupied()
}

