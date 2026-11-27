package livetv

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/dvr"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestLiveTVV0109AuditH05H06M20M28L18(t *testing.T) {
	h, mux, st, cfg := setupLiveTVTest(t)
	ctx := context.Background()
	userRepo := sqlite.NewUserRepo(st)
	h.SetUserRepo(userRepo)

	adminRec := &sqlite.UserRecord{Name: "admin_v109", IsAdmin: true, Policy: sqlite.DefaultUserPolicy(true), Config: sqlite.DefaultUserConfig()}
	_ = userRepo.CreateUser(ctx, adminRec)
	adminAC := &middleware.AuthContext{User: adminRec}

	// 1. H-05: DueRecordingJobs checks channelAllowsPrivateLANForChannel and sets DenyPrivateLAN
	now := time.Now().UTC()
	h.mu.Lock()
	h.state.TunerHosts = []TunerHostInfo{
		{Id: "tuner-pub", Type: "m3u", AllowPrivateNetwork: false, UserAgent: "AmbraCustomUA/1.0"},
		{Id: "tuner-lan", Type: "m3u", AllowPrivateNetwork: true},
	}
	h.state.Channels = []ChannelInfo{
		{Id: "ch-pub-ok", Name: "Pub OK", StreamUrl: "https://93.184.216.34/live.ts", TunerHostId: "tuner-pub"},
		{Id: "ch-pub-smuggle-lan", Name: "Smuggled LAN", StreamUrl: "http://192.168.1.50/secret.ts", TunerHostId: "tuner-pub"},
		{Id: "ch-lan-ok", Name: "LAN OK", StreamUrl: "http://192.168.1.51/lan.ts", TunerHostId: "tuner-lan"},
	}
	h.state.Timers = []TimerInfo{
		{Id: "tm-pub-ok", ChannelId: "ch-pub-ok", Name: "Show Pub", Status: "New", StartDate: now.Add(-time.Minute), EndDate: now.Add(time.Hour)},
		{Id: "tm-pub-smuggle", ChannelId: "ch-pub-smuggle-lan", Name: "Show Smuggle", Status: "New", StartDate: now.Add(-time.Minute), EndDate: now.Add(time.Hour)},
		{Id: "tm-lan-ok", ChannelId: "ch-lan-ok", Name: "Show LAN", Status: "New", StartDate: now.Add(-time.Minute), EndDate: now.Add(time.Hour)},
	}
	h.mu.Unlock()

	jobs, err := h.DueRecordingJobs(ctx)
	if err != nil {
		t.Fatalf("DueRecordingJobs: %v", err)
	}
	byTimer := make(map[string]dvr.RecordingJob, len(jobs))
	for _, j := range jobs {
		byTimer[j.TimerID] = j
	}
	if _, leaked := byTimer["tm-pub-smuggle"]; leaked {
		t.Fatal("H-05: private LAN channel under public tuner must NOT be scheduled for DVR recording")
	}
	pubJob, okPub := byTimer["tm-pub-ok"]
	if !okPub || !pubJob.DenyPrivateLAN {
		t.Fatalf("H-05: expected public channel job with DenyPrivateLAN=true, got ok=%v job=%+v", okPub, pubJob)
	}
	lanJob, okLAN := byTimer["tm-lan-ok"]
	if !okLAN || lanJob.DenyPrivateLAN {
		t.Fatalf("H-05: expected private LAN channel job with DenyPrivateLAN=false, got ok=%v job=%+v", okLAN, lanJob)
	}

	// 2. H-06 + L-18: Signed HLS sub-playlist exp is strictly monotonic (never extends past request exp) + TunerHost UserAgent forwarded
	if proxyTargetTTL > 15*time.Minute {
		t.Fatalf("H-06: expected proxyTargetTTL shortened (<=15m), got %v", proxyTargetTTL)
	}
	var seenUA atomic.Value
	hlsUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenUA.Store(r.Header.Get("User-Agent"))
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		switch r.URL.Path {
		case "/master.m3u8":
			_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=800000\nsub.m3u8\n"))
		case "/sub.m3u8":
			_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:4.0,\nseg1.ts\n"))
		default:
			w.Header().Set("Content-Type", "video/mp2t")
			_, _ = w.Write([]byte("TS"))
		}
	}))
	defer hlsUpstream.Close()

	h.SetStreamClient(hlsUpstream.Client())
	h.mu.Lock()
	h.state.TunerHosts = append(h.state.TunerHosts, TunerHostInfo{
		Id:                  "tuner-hls",
		Type:                "m3u",
		AllowPrivateNetwork: true,
		UserAgent:           "AmbraLiveTV/9.9",
	})
	h.state.Channels = append(h.state.Channels, ChannelInfo{
		Id:          "ch-hls-mono",
		Name:        "Mono HLS",
		StreamUrl:   hlsUpstream.URL + "/master.m3u8",
		TunerHostId: "tuner-hls",
	})
	h.mu.Unlock()

	authSvc := auth.NewService(userRepo, cfg)
	viewer, _ := authSvc.CreateUser(ctx, "viewer_v109", "pass1234", false)
	viewerAuth, _ := authSvc.AuthenticateByName(ctx, "viewer_v109", "pass1234", "", auth.ClientInfo{DeviceID: "d-v109"})
	authMW := middleware.NewAuth(authSvc, nil)
	authMux := http.NewServeMux()
	h.RegisterRoutes(authMux, AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
	})

	// Request master playlist via authenticated endpoint -> should forward User-Agent
	reqM := httptest.NewRequest(http.MethodGet, "/LiveTv/Channels/ch-hls-mono/Stream", nil)
	reqM.Header.Set("X-Emby-Token", viewerAuth.AccessToken)
	recM := httptest.NewRecorder()
	authMux.ServeHTTP(recM, reqM)
	if recM.Code != http.StatusOK {
		t.Fatalf("master HLS status=%d body=%s", recM.Code, recM.Body.String())
	}
	if ua, _ := seenUA.Load().(string); ua != "AmbraLiveTV/9.9" {
		t.Fatalf("L-18: expected TunerHost UserAgent forwarded, got %q", ua)
	}

	// Sign a sub-playlist target with a short exp (now + 30s), then request it unauthenticated via signed target.
	// The rewritten segment URLs inside sub.m3u8 MUST have exp <= shortExp (no renewal!).
	shortExp := time.Now().UTC().Add(30 * time.Second).Unix()
	subTarget := hlsUpstream.URL + "/sub.m3u8"
	subSig := h.signProxyTarget("ch-hls-mono", subTarget, true, shortExp)
	subReqURL := fmt.Sprintf("/LiveTv/Channels/ch-hls-mono/Stream?target=%s&exp=%d&lan=1&sig=%s", url.QueryEscape(subTarget), shortExp, subSig)
	recSub := httptest.NewRecorder()
	authMux.ServeHTTP(recSub, httptest.NewRequest(http.MethodGet, subReqURL, nil))
	if recSub.Code != http.StatusOK {
		t.Fatalf("signed sub-playlist status=%d body=%s", recSub.Code, recSub.Body.String())
	}
	var segURL string
	for _, line := range strings.Split(recSub.Body.String(), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Stream?target=") {
			segURL = line
			break
		}
	}
	if segURL == "" {
		t.Fatalf("expected rewritten segment URL in sub-playlist:\n%s", recSub.Body.String())
	}
	parsedSeg, err := url.Parse(segURL)
	if err != nil {
		t.Fatalf("parse segURL: %v", err)
	}
	segExp, err := strconv.ParseInt(parsedSeg.Query().Get("exp"), 10, 64)
	if err != nil {
		t.Fatalf("parse seg exp: %v", err)
	}
	if segExp > shortExp {
		t.Fatalf("H-06: rewritten sub-playlist exp=%d exceeded parent signed request exp=%d", segExp, shortExp)
	}
	_ = viewer

	// 3. M-20: scheduleTimersForSeriesLocked respects maxTimers cap and skips <=0 or >24h programs
	h.mu.Lock()
	h.state.Timers = nil
	h.state.Programs = []ProgramInfo{
		{Id: "p-bad-zero", ChannelId: "ch-hls-mono", Name: "SeriesCap", StartDate: now.Add(time.Hour), EndDate: now.Add(time.Hour)},
		{Id: "p-bad-neg", ChannelId: "ch-hls-mono", Name: "SeriesCap", StartDate: now.Add(2 * time.Hour), EndDate: now.Add(time.Hour)},
		{Id: "p-bad-long", ChannelId: "ch-hls-mono", Name: "SeriesCap", StartDate: now.Add(time.Hour), EndDate: now.Add(26 * time.Hour)},
		{Id: "p-good-1", ChannelId: "ch-hls-mono", Name: "SeriesCap", StartDate: now.Add(time.Hour), EndDate: now.Add(2 * time.Hour)},
		{Id: "p-good-2", ChannelId: "ch-hls-mono", Name: "SeriesCap", StartDate: now.Add(3 * time.Hour), EndDate: now.Add(4 * time.Hour)},
	}
	for i := 0; i < maxTimers-1; i++ {
		h.state.Timers = append(h.state.Timers, TimerInfo{Id: fmt.Sprintf("pre-%d", i), ChannelId: "ch-hls-mono", Status: "New"})
	}
	h.scheduleTimersForSeriesLocked(SeriesTimerInfo{Id: "st-cap", Name: "SeriesCap", ChannelId: "ch-hls-mono"})
	gotTimerCount := len(h.state.Timers)
	lastTimer := h.state.Timers[len(h.state.Timers)-1]
	h.mu.Unlock()
	if gotTimerCount != maxTimers {
		t.Fatalf("M-20: expected timer count capped at %d, got %d", maxTimers, gotTimerCount)
	}
	if lastTimer.ProgramId != "p-good-1" {
		t.Fatalf("M-20: expected invalid-duration programs skipped and p-good-1 scheduled, got %+v", lastTimer)
	}

	// 4. M-28: handleUpdateSeriesTimer removes stale "New" timers before rescheduling
	h.mu.Lock()
	h.state.Timers = []TimerInfo{
		{Id: "old-new-1", SeriesTimerId: "st-upd", ChannelId: "ch-hls-mono", ProgramId: "p-old", Name: "OldShow", Status: "New", StartDate: now.Add(time.Hour), EndDate: now.Add(2 * time.Hour)},
		{Id: "old-inprog", SeriesTimerId: "st-upd", ChannelId: "ch-hls-mono", ProgramId: "p-run", Name: "OldShow", Status: "InProgress", StartDate: now.Add(-time.Minute), EndDate: now.Add(time.Hour)},
	}
	h.state.SeriesTimers = []SeriesTimerInfo{{Id: "st-upd", Name: "OldShow", ChannelId: "ch-hls-mono"}}
	h.state.Programs = []ProgramInfo{
		{Id: "p-new-show", ChannelId: "ch-hls-mono", Name: "NewShow", StartDate: now.Add(2 * time.Hour), EndDate: now.Add(3 * time.Hour)},
	}
	h.mu.Unlock()
	if rec := doLiveTVAuthReq(t, mux, adminAC, http.MethodPost, "/LiveTv/SeriesTimers/st-upd", map[string]any{
		"Name":      "NewShow",
		"ChannelId": "ch-hls-mono",
	}); rec.Code != http.StatusOK {
		t.Fatalf("update series timer failed: %d %s", rec.Code, rec.Body.String())
	}
	h.mu.RLock()
	var sawOldNew, sawOldInProg, sawRescheduled bool
	for _, tm := range h.state.Timers {
		if tm.Id == "old-new-1" {
			sawOldNew = true
		}
		if tm.Id == "old-inprog" {
			sawOldInProg = true
		}
		if tm.ProgramId == "p-new-show" && tm.SeriesTimerId == "st-upd" {
			sawRescheduled = true
		}
	}
	h.mu.RUnlock()
	if sawOldNew || !sawOldInProg || !sawRescheduled {
		t.Fatalf("M-28 update series timer: sawOldNew=%v sawOldInProg=%v sawRescheduled=%v", sawOldNew, sawOldInProg, sawRescheduled)
	}

	// 5. M-28: handleSaveTunerHost with empty M3U clears stale channels and programs
	h.httpClient = &http.Client{
		Transport: rtFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("#EXTM3U\n")),
				Header:     http.Header{"Content-Type": []string{"audio/x-mpegurl"}},
			}, nil
		}),
	}
	h.customHTTPClient = true
	if rec := doJSON(t, mux, http.MethodPost, "/LiveTv/TunerHosts", map[string]any{
		"Id":         "tuner-empty-test",
		"Type":       "m3u",
		"M3UContent": "#EXTM3U\n#EXTINF:-1,Ch A\nhttps://93.184.216.34/a.ts\n",
	}); rec.Code != http.StatusOK {
		t.Fatalf("seed tuner-empty-test: %d", rec.Code)
	}
	if rec := doJSON(t, mux, http.MethodPost, "/LiveTv/TunerHosts", map[string]any{
		"Id":         "tuner-empty-test",
		"Type":       "m3u",
		"M3UContent": "#EXTM3U\n",
	}); rec.Code != http.StatusOK {
		t.Fatalf("clear tuner-empty-test with empty M3U: %d", rec.Code)
	}
	for _, ch := range h.SnapshotChannels() {
		if ch.TunerHostId == "tuner-empty-test" {
			t.Fatalf("M-28: stale channel remained after empty M3U update: %+v", ch)
		}
	}

	// 6. M-28: DeleteTunerHost cascades pending timers and cancels active DVR recordings
	dvrRec := dvr.NewRecorder(dvr.Options{
		DataDir:    filepath.Join(cfg.DataDir, "dvr-tuner-del"),
		FFmpegPath: "/bin/sh",
		ArgsBuilder: func(job dvr.RecordingJob, outFile string) []string {
			return []string{"-c", "exec sleep 30"}
		},
	})
	defer func() { _ = dvrRec.Close() }()
	h.SetRecorder(dvrRec)

	h.mu.Lock()
	h.state.TunerHosts = append(h.state.TunerHosts, TunerHostInfo{Id: "tuner-del-1", Type: "m3u"})
	h.state.Channels = append(h.state.Channels, ChannelInfo{Id: "ch-del-1", Name: "Del Ch", TunerHostId: "tuner-del-1", StreamUrl: "https://93.184.216.34/del.ts"})
	h.state.Timers = append(h.state.Timers,
		TimerInfo{Id: "tm-del-new", ChannelId: "ch-del-1", Status: "New", StartDate: now.Add(time.Hour), EndDate: now.Add(2 * time.Hour)},
		TimerInfo{Id: "tm-del-inprog", ChannelId: "ch-del-1", Status: "InProgress", StartDate: now, EndDate: now.Add(time.Hour)},
	)
	h.mu.Unlock()
	if _, err := dvrRec.StartRecording(ctx, dvr.RecordingJob{
		TimerID:     "tm-del-inprog",
		ChannelName: "Del Ch",
		ProgramName: "Live",
		StreamURL:   "https://93.184.216.34/del.ts",
	}); err != nil {
		t.Fatalf("StartRecording: %v", err)
	}
	if !h.DeleteTunerHost(ctx, "tuner-del-1") {
		t.Fatal("DeleteTunerHost returned false")
	}
	h.mu.RLock()
	for _, tm := range h.state.Timers {
		if tm.ChannelId == "ch-del-1" {
			h.mu.RUnlock()
			t.Fatalf("M-28: timer for deleted tuner channel remained: %+v", tm)
		}
	}
	h.mu.RUnlock()
	// Active recording should have been cancelled (CancelRecording returns error once removed/cancelled).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		active := dvrRec.ActiveRecordings()
		found := false
		for _, rec := range active {
			if rec.Job.TimerID == "tm-del-inprog" {
				found = true
			}
		}
		if !found {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	for _, rec := range dvrRec.ActiveRecordings() {
		if rec.Job.TimerID == "tm-del-inprog" {
			t.Fatal("M-28: active DVR recording was not cancelled when tuner host was deleted")
		}
	}

	// 7. M-28: handleSaveTunerHost & handleSaveListingProvider rollback state when saveStateLocked fails
	h2, mux2, st2, _ := setupLiveTVTest(t)
	_ = st2.Close() // Force DB write error on saveStateLocked
	beforeTuners := len(h2.SnapshotTunerHosts())
	beforeLPs := len(h2.SnapshotListingProviders())

	reqT := httptest.NewRequest(http.MethodPost, "/LiveTv/TunerHosts", bytes.NewReader([]byte(`{"Type":"m3u","M3UContent":"#EXTM3U\n#EXTINF:-1,RB\nhttps://93.184.216.34/rb.ts\n"}`)))
	recT := httptest.NewRecorder()
	mux2.ServeHTTP(recT, reqT)
	if recT.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when tuner host persistence fails, got %d", recT.Code)
	}
	if len(h2.SnapshotTunerHosts()) != beforeTuners || len(h2.SnapshotChannels()) != 0 {
		t.Fatalf("M-28: expected tuner host & channels rolled back on persistence error")
	}

	reqLP := httptest.NewRequest(http.MethodPost, "/LiveTv/ListingProviders", bytes.NewReader([]byte(`{"Type":"xmltv"}`)))
	recLP := httptest.NewRecorder()
	mux2.ServeHTTP(recLP, reqLP)
	if recLP.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when listing provider persistence fails, got %d", recLP.Code)
	}
	if len(h2.SnapshotListingProviders()) != beforeLPs {
		t.Fatalf("M-28: expected listing provider rolled back on persistence error")
	}
	_ = json.Marshal
}

func TestLiveTVStreamHubHDHomeRunMappingsRefreshRecordNewOnlyAndMediaImport(t *testing.T) {
	h, mux, st, cfg := setupLiveTVTest(t)
	defer h.Close()
	ctx := context.Background()
	libRepo := sqlite.NewLibraryRepo(st)
	userRepo := sqlite.NewUserRepo(st)
	h.SetLibraryRepo(libRepo)
	h.SetUserRepo(userRepo)
	h.SetStreamGracePeriod(40 * time.Millisecond)

	adminRec := &sqlite.UserRecord{Name: "adm_hub", IsAdmin: true, Policy: sqlite.DefaultUserPolicy(true), Config: sqlite.DefaultUserConfig()}
	_ = userRepo.CreateUser(ctx, adminRec)
	adminAC := &middleware.AuthContext{User: adminRec}

	// 1. maxM3UChannels == 10,000 cap
	if maxM3UChannels != 10000 {
		t.Fatalf("expected maxM3UChannels == 10000, got %d", maxM3UChannels)
	}
	var bigM3U strings.Builder
	bigM3U.WriteString("#EXTM3U\n")
	for i := 0; i < 10050; i++ {
		bigM3U.WriteString("#EXTINF:-1,Ch ")
		bigM3U.WriteString(strconv.Itoa(i))
		bigM3U.WriteString("\nhttp://tv.example/")
		bigM3U.WriteString(strconv.Itoa(i))
		bigM3U.WriteString(".ts\n")
	}
	cappedChs, err := ParseM3UString(bigM3U.String())
	if err != nil || len(cappedChs) != 10000 {
		t.Fatalf("expected ParseM3U capped at 10000 channels, got len=%d err=%v", len(cappedChs), err)
	}

	// 2. LiveStreamHub multiplexes concurrent viewers onto ONE upstream HTTP connection + disconnects upstream after viewers leave + idle timeout
	var upstreamConns atomic.Int32
	upstreamClosed := make(chan struct{})
	gate := make(chan struct{})
	tsUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/shared.ts":
			n := upstreamConns.Add(1)
			w.Header().Set("Content-Type", "video/mp2t")
			w.WriteHeader(http.StatusOK)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			<-gate
			for i := 0; i < 5; i++ {
				_, _ = w.Write([]byte("MPEGTS-PKT;"))
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
				time.Sleep(10 * time.Millisecond)
			}
			// Hold open until LiveStreamHub closes upstream after viewers disconnect.
			<-r.Context().Done()
			if n == 1 {
				close(upstreamClosed)
			}
		case "/stalled.ts":
			w.Header().Set("Content-Type", "video/mp2t")
			w.WriteHeader(http.StatusOK)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			_, _ = w.Write([]byte("INIT;"))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			<-r.Context().Done()
		case "/discover.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"FriendlyName":"HDHomeRun CONNECT","DeviceID":"1040ABCD","TunerCount":4}`))
		case "/lineup.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{"GuideNumber":"2.1","GuideName":"PBS HD","URL":"` + r.Host + `","Favorite":1,"HD":1},
				{"GuideNumber":"5.1","GuideName":"Shopping","URL":"http://192.168.1.99:5004/auto/v5.1","Favorite":0,"HD":0}
			]`))
		}
	}))
	defer tsUpstream.Close()

	h.SetHTTPClient(tsUpstream.Client())
	h.SetStreamClient(tsUpstream.Client())

	h.mu.Lock()
	h.state.TunerHosts = []TunerHostInfo{{Id: "th-hub", Type: "m3u", AllowPrivateNetwork: true}}
	h.state.Channels = []ChannelInfo{
		{Id: "ch-shared", Name: "Shared TS", StreamUrl: tsUpstream.URL + "/shared.ts", TunerHostId: "th-hub"},
		{Id: "ch-stall", Name: "Stall TS", StreamUrl: tsUpstream.URL + "/stalled.ts", TunerHostId: "th-hub"},
	}
	h.mu.Unlock()

	viewerSrv := httptest.NewServer(mux)
	defer viewerSrv.Close()

	const numViewers = 3
	var wg sync.WaitGroup
	errCh := make(chan error, numViewers)
	for i := 0; i < numViewers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			vCtx, vCancel := context.WithTimeout(ctx, 3*time.Second)
			defer vCancel()
			req, _ := http.NewRequestWithContext(vCtx, http.MethodGet, viewerSrv.URL+"/LiveTv/Channels/ch-shared/Stream", nil)
			resp, err := viewerSrv.Client().Do(req)
			if err != nil {
				errCh <- fmt.Errorf("viewer %d request: %w", idx, err)
				return
			}
			defer resp.Body.Close()
			buf := make([]byte, len("MPEGTS-PKT;"))
			if _, err := io.ReadFull(resp.Body, buf); err != nil {
				errCh <- fmt.Errorf("viewer %d read: %w", idx, err)
				return
			}
			if string(buf) != "MPEGTS-PKT;" {
				errCh <- fmt.Errorf("viewer %d got %q", idx, string(buf))
			}
		}(i)
	}
	time.Sleep(40 * time.Millisecond)
	close(gate)
	wg.Wait()
	close(errCh)
	for vErr := range errCh {
		t.Fatal(vErr)
	}
	if got := upstreamConns.Load(); got != 1 {
		t.Fatalf("expected exactly 1 shared upstream HTTP connection for %d concurrent viewers, got %d", numViewers, got)
	}
	select {
	case <-upstreamClosed:
	case <-time.After(2 * time.Second):
		t.Fatal("expected shared upstream connection closed automatically after all viewers disconnected")
	}

	// Verify idle read timeout disconnects stalled upstream stream automatically
	h.SetStreamIdleTimeout(120 * time.Millisecond)
	stallStart := time.Now()
	stallRec := doJSON(t, mux, http.MethodGet, "/LiveTv/Channels/ch-stall/Stream", nil)
	if stallRec.Code != http.StatusOK || !strings.HasPrefix(stallRec.Body.String(), "INIT;") || time.Since(stallStart) > 2*time.Second {
		t.Fatalf("expected stalled upstream to terminate cleanly on idle timeout, code=%d elapsed=%v body=%q", stallRec.Code, time.Since(stallStart), stallRec.Body.String())
	}
	h.SetStreamIdleTimeout(defaultStreamIdleTimeout)

	// 3. HDHomeRun tuner import (discover.json + lineup.json + ImportFavoritesOnly) & DiscoverTuners
	hdSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/discover.json":
			_, _ = w.Write([]byte(`{"FriendlyName":"LivingRoom HDHR","DeviceID":"HDHR-9988","TunerCount":2}`))
		case "/lineup.json":
			_, _ = w.Write([]byte(`[
				{"GuideNumber":"10.1","GuideName":"NBC HD","URL":"http://192.168.1.88:5004/auto/v10.1","Favorite":1,"HD":1},
				{"GuideNumber":"10.2","GuideName":"SubCh","URL":"http://192.168.1.88:5004/auto/v10.2","Favorite":0,"HD":0}
			]`))
		}
	}))
	defer hdSrv.Close()
	h.SetHTTPClient(hdSrv.Client())

	discoveredProbe := h.DiscoverTuners(ctx, hdSrv.URL)
	foundProbe := false
	for _, d := range discoveredProbe {
		if d.DeviceId == "HDHR-9988" && d.FriendlyName == "LivingRoom HDHR" && d.TunerCount == 2 {
			foundProbe = true
		}
	}
	if !foundProbe {
		t.Fatalf("expected DiscoverTuners to discover HDHR-9988, got %+v", discoveredProbe)
	}

	recHD := doJSON(t, mux, http.MethodPost, "/LiveTv/TunerHosts", map[string]any{
		"Id":                  "th-hdhr",
		"Type":                "hdhomerun",
		"Url":                 hdSrv.URL,
		"ImportFavoritesOnly": true,
	})
	if recHD.Code != http.StatusOK {
		t.Fatalf("save hdhomerun tuner failed: %d %s", recHD.Code, recHD.Body.String())
	}
	var savedHD TunerHostInfo
	_ = json.Unmarshal(recHD.Body.Bytes(), &savedHD)
	if savedHD.ImportedChannelCount != 1 || savedHD.DeviceId != "HDHR-9988" || savedHD.TunerCount != 2 {
		t.Fatalf("unexpected saved HDHomeRun tuner: %+v", savedHD)
	}
	var hdChID string
	for _, ch := range h.SnapshotChannels() {
		if ch.TunerHostId == "th-hdhr" {
			hdChID = ch.Id
		}
	}
	if hdChID == "" {
		t.Fatal("expected imported HDHomeRun favorite channel")
	}

	// 4. Manual ChannelMappings priority + RefreshAllGuides (>24h expiry pruning) + SeriesTimer.RecordNewOnly
	// Seed manual mapping in server_config ("compatext:state"): map hdChID -> provider channel "epg-custom-nbc"
	_, err = st.Writer.ExecContext(ctx,
		`INSERT INTO server_config (key, value_json, updated_at) VALUES (?, ?, ?)`,
		"compatext:state",
		fmt.Sprintf(`{"channel_mappings":{"lp-1":[{"Name":%q,"Value":"epg-custom-nbc"}]}}`, hdChID),
		time.Now().UTC().Unix(),
	)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Minute)
	fmtXMLTime := func(t time.Time) string { return t.UTC().Format("20060102150405 +0000") }
	xmlPath := filepath.Join(cfg.DataDir, "mapped.xml")
	xmlDoc := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<tv>
  <channel id="epg-custom-nbc"><display-name>Custom EPG</display-name></channel>
  <programme start="%s" stop="%s" channel="epg-custom-nbc">
    <title>Old Expired News</title>
  </programme>
  <programme start="%s" stop="%s" channel="epg-custom-nbc">
    <title>SciFi Odyssey</title>
    <sub-title>Pilot</sub-title>
    <episode-num system="xmltv_ns">0.0.</episode-num>
  </programme>
  <programme start="%s" stop="%s" channel="epg-custom-nbc">
    <title>SciFi Odyssey</title>
    <sub-title>Pilot</sub-title>
    <episode-num system="xmltv_ns">0.0.</episode-num>
  </programme>
  <programme start="%s" stop="%s" channel="epg-custom-nbc">
    <title>SciFi Odyssey</title>
    <sub-title>Rerun Episode</sub-title>
    <previously-shown />
  </programme>
</tv>`,
		fmtXMLTime(now.Add(-30*time.Hour)), fmtXMLTime(now.Add(-29*time.Hour)),
		fmtXMLTime(now.Add(1*time.Hour)), fmtXMLTime(now.Add(2*time.Hour)),
		fmtXMLTime(now.Add(5*time.Hour)), fmtXMLTime(now.Add(6*time.Hour)),
		fmtXMLTime(now.Add(7*time.Hour)), fmtXMLTime(now.Add(8*time.Hour)),
	)
	if err := os.WriteFile(xmlPath, []byte(xmlDoc), 0o644); err != nil {
		t.Fatal(err)
	}

	if recLP := doJSON(t, mux, http.MethodPost, "/LiveTv/ListingProviders", map[string]any{
		"Id":   "lp-1",
		"Type": "xmltv",
		"Path": xmlPath,
	}); recLP.Code != http.StatusOK {
		t.Fatalf("save xmltv provider: %d %s", recLP.Code, recLP.Body.String())
	}

	// RefreshAllGuides reloads XMLTV and prunes "Old Expired News" (ended 29h ago > 24h cutoff)
	if err := h.RefreshAllGuides(ctx); err != nil {
		t.Fatalf("RefreshAllGuides failed: %v", err)
	}
	for _, p := range h.SnapshotPrograms() {
		if p.Name == "Old Expired News" {
			t.Fatal("expected RefreshAllGuides to prune >24h expired program")
		}
	}

	// Create SeriesTimer with RecordNewOnly=true for "SciFi Odyssey":
	// - First "Pilot" (0.0.) should be scheduled;
	// - Second airing of "Pilot" (0.0.) 4h later must be deduplicated;
	// - "Rerun Episode" with <previously-shown /> must be skipped!
	recST := doLiveTVAuthReq(t, mux, adminAC, http.MethodPost, "/LiveTv/SeriesTimers", map[string]any{
		"Id":            "st-new-only",
		"Name":          "SciFi Odyssey",
		"ChannelId":     hdChID,
		"RecordNewOnly": true,
	})
	if recST.Code != http.StatusOK {
		t.Fatalf("create series timer failed: %d %s", recST.Code, recST.Body.String())
	}
	h.mu.RLock()
	var scheduledForSeries []TimerInfo
	for _, tm := range h.state.Timers {
		if tm.SeriesTimerId == "st-new-only" {
			scheduledForSeries = append(scheduledForSeries, tm)
		}
	}
	h.mu.RUnlock()
	if len(scheduledForSeries) != 1 {
		t.Fatalf("expected RecordNewOnly to schedule only 1 new episode (skipping duplicate epnum & <previously-shown>), got %d: %+v", len(scheduledForSeries), scheduledForSeries)
	}

	// 5. DVR OnRecordingFinished automatically registers completed recording into LibraryRepo
	recOutPath := filepath.Join(cfg.DataDir, "recordings", "odyssey_s01e01.ts")
	_ = os.MkdirAll(filepath.Dir(recOutPath), 0o755)
	_ = os.WriteFile(recOutPath, []byte("RECORDED-TS-DATA"), 0o644)

	h.OnRecordingFinished(ctx, dvr.RecordingState{
		Job: dvr.RecordingJob{
			TimerID:     scheduledForSeries[0].Id,
			ChannelName: "NBC HD",
			ProgramName: "SciFi Odyssey",
		},
		Status:     dvr.StatusCompleted,
		OutputPath: recOutPath,
		StartedAt:  now,
		EndedAt:    now.Add(30 * time.Minute),
	})

	recs := h.SnapshotRecordings()
	if len(recs) == 0 {
		t.Fatal("expected completed recording in LiveTV state")
	}
	lastRec := recs[len(recs)-1]
	libItem, err := libRepo.GetItemRecordByID(ctx, lastRec.Id)
	if err != nil || libItem == nil || libItem.Path != recOutPath {
		t.Fatalf("expected completed DVR recording imported into LibraryRepo, got item=%+v err=%v", libItem, err)
	}
}

func makeTSPacket(seq byte) []byte {
	pkt := make([]byte, tsPacketSize)
	pkt[0] = tsSyncByte
	pkt[1] = seq
	for i := 2; i < tsPacketSize; i++ {
		pkt[i] = byte((int(seq) + i) % 251)
		if pkt[i] == tsSyncByte {
			pkt[i] = 0x48
		}
	}
	return pkt
}

func joinChunks(chunks [][]byte) []byte {
	var out []byte
	for _, c := range chunks {
		out = append(out, c...)
	}
	return out
}

func TestAlignRingReplayToTSSync(t *testing.T) {
	t.Run("strips leading partial TS packet garbage across chunks to exact 188-byte boundary", func(t *testing.T) {
		garbage := bytes.Repeat([]byte{0x11}, 73)
		p1 := makeTSPacket(1)
		p2 := makeTSPacket(2)
		p3 := makeTSPacket(3)
		full := append(append(append(append([]byte{}, garbage...), p1...), p2...), p3...)

		// Split across multiple chunks so the first chunk is entirely garbage+partial and second chunk continues p1.
		ring := [][]byte{
			full[:50],
			full[50:150],
			full[150:400],
			full[400:],
		}
		aligned := alignRingReplayToTSSync(ring)
		if len(aligned) > len(ring) {
			t.Fatalf("aligned chunk count %d exceeded input ring count %d", len(aligned), len(ring))
		}
		got := joinChunks(aligned)
		want := append(append(append([]byte{}, p1...), p2...), p3...)
		if !bytes.Equal(got, want) {
			t.Fatalf("aligned replay mismatch: got len=%d firstBytes=%x, want len=%d firstBytes=%x", len(got), got[:min(8, len(got))], len(want), want[:min(8, len(want))])
		}
	})

	t.Run("skips false 0x47 inside leading garbage before real 3-packet sync sequence", func(t *testing.T) {
		garbage := bytes.Repeat([]byte{0x22}, 60)
		garbage[5] = tsSyncByte  // false 0x47
		garbage[29] = tsSyncByte // another false 0x47
		p1 := makeTSPacket(10)
		p2 := makeTSPacket(20)
		p3 := makeTSPacket(30)
		full := append(append(append(append([]byte{}, garbage...), p1...), p2...), p3...)

		ring := [][]byte{full[:120], full[120:]}
		aligned := alignRingReplayToTSSync(ring)
		got := joinChunks(aligned)
		want := append(append(append([]byte{}, p1...), p2...), p3...)
		if !bytes.Equal(got, want) {
			t.Fatalf("expected false 0x47 bytes skipped; got len=%d first=%x want len=%d first=%x", len(got), got[:min(8, len(got))], len(want), want[:min(8, len(want))])
		}
	})

	t.Run("falls back to 2 consecutive sync bytes when buffer is shorter than 377 bytes", func(t *testing.T) {
		garbage := bytes.Repeat([]byte{0x33}, 25)
		garbage[3] = tsSyncByte // false 0x47
		p1 := makeTSPacket(41)
		p2Prefix := makeTSPacket(42)[:10] // total = 25 + 188 + 10 = 223 (< 377)
		full := append(append(append([]byte{}, garbage...), p1...), p2Prefix...)

		aligned := alignRingReplayToTSSync([][]byte{full})
		got := joinChunks(aligned)
		want := append(append([]byte{}, p1...), p2Prefix...)
		if !bytes.Equal(got, want) {
			t.Fatalf("expected 2-sync fallback alignment; got len=%d want len=%d", len(got), len(want))
		}
	})

	t.Run("non-TS stream data passes through unchanged", func(t *testing.T) {
		chunk1 := bytes.Repeat([]byte("MPEGTS-NONSYNC-DATA-"), 15) // 300 bytes
		chunk2 := bytes.Repeat([]byte("ANOTHER-CHUNK-PAYLOAD"), 15)
		ring := [][]byte{chunk1, chunk2}

		aligned := alignRingReplayToTSSync(ring)
		if !bytes.Equal(joinChunks(aligned), joinChunks(ring)) {
			t.Fatal("expected non-TS stream data to pass through unchanged")
		}
	})

	t.Run("addSubscriber delivers TS-aligned replay to mid-stream joiner", func(t *testing.T) {
		garbage := bytes.Repeat([]byte{0xAA}, 41)
		garbage[7] = tsSyncByte
		p1 := makeTSPacket(7)
		p2 := makeTSPacket(8)
		p3 := makeTSPacket(9)
		full := append(append(append(append([]byte{}, garbage...), p1...), p2...), p3...)

		b := &liveStreamBroadcaster{
			subs: make(map[*liveStreamSubscriber]struct{}),
			ring: [][]byte{full[:100], full[100:]},
		}
		sub, ok := b.addSubscriber()
		if !ok || sub == nil {
			t.Fatal("expected addSubscriber to succeed")
		}
		var replayed []byte
		for len(sub.ch) > 0 {
			replayed = append(replayed, <-sub.ch...)
		}
		want := append(append(append([]byte{}, p1...), p2...), p3...)
		if !bytes.Equal(replayed, want) {
			t.Fatalf("subscriber did not receive aligned TS replay: got len=%d first=%x want len=%d first=%x", len(replayed), replayed[:min(4, len(replayed))], len(want), want[:min(4, len(want))])
		}
	})
}

