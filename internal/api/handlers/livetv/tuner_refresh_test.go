package livetv

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/core/auth"
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
	h.state.Programs = append(h.state.Programs, ProgramInfo{
		Id:        "prog-epg-cctv1",
		Name:      "Evening News",
		Type:      "Program",
		ChannelId: cctvID,
		StartDate: now,
		EndDate:   now.Add(time.Hour),
	})
	h.state.Timers = append(h.state.Timers, TimerInfo{
		Id:        "timer-cctv1",
		ChannelId: cctvID,
		ProgramId: "prog-epg-cctv1",
		Name:      "Evening News",
		Status:    "New",
		StartDate: now.Add(time.Hour),
		EndDate:   now.Add(2 * time.Hour),
	})
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

	var foundEPGProgram bool
	for _, p := range h.SnapshotPrograms() {
		if p.Id == "prog-epg-cctv1" && p.ChannelId == cctvID {
			foundEPGProgram = true
		}
	}
	if !foundEPGProgram {
		t.Fatal("expected existing EPG program on CCTV-1 to be preserved across refresh")
	}

	h.mu.RLock()
	var foundTimer bool
	for _, tm := range h.state.Timers {
		if tm.Id == "timer-cctv1" && tm.ChannelId == cctvID {
			foundTimer = true
		}
	}
	h.mu.RUnlock()
	if !foundTimer {
		t.Fatal("expected DVR timer linked to CCTV-1 to remain intact")
	}

	// Verify fallback inheritance for unique name without tvg-id when URL changed.
	var foundUnique bool
	var dupInherited int
	for _, ch := range h.SnapshotChannels() {
		if strings.EqualFold(strings.TrimSpace(ch.Name), "Unique NoTvg") {
			foundUnique = true
			if ch.Id == uniqueID {
				t.Fatalf("expected channel ID to change when no tvg-id and URL changed")
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
