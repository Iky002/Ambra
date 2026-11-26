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
	"path/filepath"
	"strconv"
	"strings"
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
			return []string{"-c", "sleep 30"}
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
	deadline := time.Now().Add(2 * time.Second)
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
