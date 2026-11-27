package livetv

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestCovV0112_LANHDHomeRunAndRepeatHelpers(t *testing.T) {
	// isStrictPrivateLANURL
	for _, bad := range []string{"", "://bad", "ftp://10.0.0.1/x", "http:///nohost", "http://8.8.8.8/live.ts", "http://localhost/live.ts", "http://127.0.0.1/live.ts"} {
		if isStrictPrivateLANURL(bad, false) {
			t.Fatalf("expected isStrictPrivateLANURL(%q, false) == false", bad)
		}
	}
	if !isStrictPrivateLANURL("http://10.20.30.40:8080/live.ts", false) {
		t.Fatal("expected RFC1918 IP treated as private LAN")
	}
	if !isStrictPrivateLANURL("http://127.0.0.1:9000/live.ts", true) {
		t.Fatal("expected loopback allowed when allowLoopback=true")
	}

	// normalizeHDHomeRunBaseURL & sameHTTPOrigin
	for _, bad := range []string{"", "://bad", "ftp://192.168.1.2", "http:///lineup.json"} {
		if _, err := normalizeHDHomeRunBaseURL(bad); err == nil {
			t.Fatalf("expected normalizeHDHomeRunBaseURL(%q) error", bad)
		}
	}
	if got, err := normalizeHDHomeRunBaseURL("192.168.1.50/discover.json?x=1#frag"); err != nil || got != "http://192.168.1.50" {
		t.Fatalf("unexpected normalized HDHomeRun URL: (%q, %v)", got, err)
	}
	uA, _ := url.Parse("http://hdhr.local.:80")
	uB, _ := url.Parse("http://HDHR.LOCAL")
	uHTTPS, _ := url.Parse("https://hdhr.local")
	uHTTPS443, _ := url.Parse("https://hdhr.local:443")
	uOther, _ := url.Parse("http://other.local")
	if sameHTTPOrigin(nil, uA) || !sameHTTPOrigin(uA, uB) || !sameHTTPOrigin(uHTTPS, uHTTPS443) || sameHTTPOrigin(uA, uHTTPS) || sameHTTPOrigin(uA, uOther) {
		t.Fatal("unexpected sameHTTPOrigin results")
	}

	// programEpisodeIdentity & shouldSkipSeriesProgramAsRepeatLocked
	if programEpisodeIdentity("  ", "1", "t", "o") != "" {
		t.Fatal("expected empty identity for blank name")
	}
	if !strings.Contains(programEpisodeIdentity("Show", "S01E02", "Pilot", "Desc"), "epnum:") {
		t.Fatal("expected epnum priority")
	}
	if !strings.Contains(programEpisodeIdentity("Show", "", "Pilot", "Desc"), "eptitle:") {
		t.Fatal("expected eptitle priority")
	}
	if !strings.Contains(programEpisodeIdentity("Show", "", "", "Desc"), "ov:") {
		t.Fatal("expected overview fallback")
	}
	if programEpisodeIdentity("Show", "", "", "") != "" {
		t.Fatal("expected empty identity when epnum/eptitle/overview all empty")
	}

	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "livetv_cov_v0112.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	h := New(store, config.Default(tmp))
	defer h.Close()

	// Client selectors on default handler (before custom HTTP/stream clients are set)
	if h.selectStreamHTTPClient(true) == nil || h.selectStreamHTTPClient(false) == nil {
		t.Fatal("expected non-nil default stream HTTP clients")
	}
	if h.selectMetadataHTTPClient(true) == nil || h.selectMetadataHTTPClient(false) == nil {
		t.Fatal("expected non-nil default metadata HTTP clients")
	}

	st := SeriesTimerInfo{Id: "st-1", Name: "News"}
	h.mu.Lock()
	h.state.Programs = []ProgramInfo{
		{Id: "p-rec-ep", Name: "Drama", EpisodeTitle: "Finale"},
		{Id: "p-tm-ep", Name: "Comedy", EpisodeNumber: "S02E05"},
	}
	h.state.Recordings = []RecordingInfo{
		{Id: "r-inprog", Status: "InProgress", Name: "News"},
		{Id: "r-done-ep", Status: "Completed", ProgramId: "p-rec-ep", Name: "Drama"},
		{Id: "r-done-title", Status: "Completed", Name: "Morning News"},
	}
	h.state.Timers = []TimerInfo{
		{Id: "t-canc", Status: "Cancelled", ProgramId: "p-tm-ep"},
		{Id: "t-act-ep", Status: "New", ProgramId: "p-tm-ep", Name: "Comedy"},
		{Id: "t-act-ov", Status: "New", Name: "Docu", Overview: "Part One"},
		{Id: "t-act-series", Status: "New", SeriesTimerId: "st-1", Name: "Nightly News"},
	}
	if !h.shouldSkipSeriesProgramAsRepeatLocked(st, ProgramInfo{IsRepeat: true}) {
		t.Fatal("expected IsRepeat skipped")
	}
	if !h.shouldSkipSeriesProgramAsRepeatLocked(st, ProgramInfo{Id: "p-rec-ep", Name: "Drama"}) {
		t.Fatal("expected exact recorded ProgramId skipped")
	}
	if !h.shouldSkipSeriesProgramAsRepeatLocked(st, ProgramInfo{Id: "p-new1", Name: "Drama", EpisodeTitle: "Finale"}) {
		t.Fatal("expected matching recorded episode identity skipped")
	}
	if !h.shouldSkipSeriesProgramAsRepeatLocked(st, ProgramInfo{Id: "p-new2", Name: "Morning News"}) {
		t.Fatal("expected completed title-only recording skipped")
	}
	if !h.shouldSkipSeriesProgramAsRepeatLocked(st, ProgramInfo{Id: "p-new3", Name: "Comedy", EpisodeNumber: "S02E05"}) {
		t.Fatal("expected scheduled timer episode identity skipped")
	}
	if !h.shouldSkipSeriesProgramAsRepeatLocked(st, ProgramInfo{Id: "p-new4", Name: "Docu", Overview: "Part One"}) {
		t.Fatal("expected scheduled timer overview identity skipped")
	}
	if !h.shouldSkipSeriesProgramAsRepeatLocked(st, ProgramInfo{Id: "p-new5", Name: "Nightly News"}) {
		t.Fatal("expected scheduled series title-only timer skipped")
	}
	if h.shouldSkipSeriesProgramAsRepeatLocked(st, ProgramInfo{Id: "p-brand-new", Name: "Brand New", EpisodeNumber: "S01E01"}) {
		t.Fatal("expected brand new episode not skipped")
	}
	h.mu.Unlock()

	// GetRecordingByID & tunerAllowsPrivateNetwork & channelAllowsPrivateLANForChannel
	if rec, ok := h.GetRecordingByID("r-done-ep"); !ok || rec == nil || rec.Name != "Drama" {
		t.Fatalf("expected recording found, got (%+v, %v)", rec, ok)
	}
	if _, ok := h.GetRecordingByID("missing-rec"); ok {
		t.Fatal("expected missing recording not found")
	}
	if allow, found := h.tunerAllowsPrivateNetwork(""); allow || found {
		t.Fatal("expected empty tunerHostID not found")
	}
	h.mu.Lock()
	h.state.TunerHosts = []TunerHostInfo{{Id: "th-public", Type: "m3u", AllowPrivateNetwork: false}}
	h.mu.Unlock()
	if allow, found := h.tunerAllowsPrivateNetwork("th-public"); !found || allow {
		t.Fatalf("expected (false, true) for th-public, got (%v, %v)", allow, found)
	}
	if h.channelAllowsPrivateLANForChannel(nil) {
		t.Fatal("expected nil channel false")
	}
	if h.channelAllowsPrivateLANForChannel(&ChannelInfo{TunerHostId: "th-public", StreamUrl: "http://10.0.0.5/ch1.ts"}) {
		t.Fatal("expected tuner AllowPrivateNetwork=false to block LAN")
	}

	// loadManualChannelMappings
	var noStoreH Handler
	if noStoreH.loadManualChannelMappings(nil) != nil {
		t.Fatal("expected nil mappings when store is nil")
	}
	ctx := context.Background()
	h.mu.Lock()
	h.state.ListingProviders = []ListingProviderInfo{{Id: "prov1", Type: "xmltv"}}
	h.mu.Unlock()
	_, _ = store.Writer.ExecContext(ctx,
		`INSERT OR REPLACE INTO server_config(key, value_json, updated_at) VALUES('compatext:state', ?, 1)`,
		`{"channel_mappings":{"prov1":[{"Name":"ch-1","Value":"XML.1"},{"Name":"CH-1","Value":"xml.1"},{"Name":"","Value":"x"}],"prov-orphan":[{"Name":"ch-2","Value":"xml.2"}]}}`,
	)
	mappings := h.loadManualChannelMappings(nil)
	if len(mappings["xml.1"]) != 1 || mappings["xml.1"][0] != "ch-1" || len(mappings["xml.2"]) != 0 {
		t.Fatalf("unexpected manual channel mappings: %+v", mappings)
	}
}

func TestCovV0112_HDHomeRunProxyAuthAndStreamHub(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "livetv_proxy_cov.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	userRepo := sqlite.NewUserRepo(store)

	noPlayPol := sqlite.DefaultUserPolicy(false)
	noPlayPol.EnableLiveTvAccess = true
	noPlayPol.EnableMediaPlayback = false
	noPlayUser := &sqlite.UserRecord{ID: guid.New(), Name: "np_cov", Policy: noPlayPol, Config: sqlite.DefaultUserConfig()}

	playPol := sqlite.DefaultUserPolicy(false)
	playPol.EnableLiveTvAccess = true
	playPol.EnableMediaPlayback = true
	playUser := &sqlite.UserRecord{ID: guid.New(), Name: "play_cov", Policy: playPol, Config: sqlite.DefaultUserConfig()}

	disabledUser := &sqlite.UserRecord{ID: guid.New(), Name: "dis_cov", IsDisabled: true, Policy: playPol, Config: sqlite.DefaultUserConfig()}
	for _, u := range []*sqlite.UserRecord{noPlayUser, playUser, disabledUser} {
		_ = userRepo.CreateUser(ctx, u)
	}

	var srvURL string
	lineupMode := "ok"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/discover.json":
			switch lineupMode {
			case "cross_origin":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"FriendlyName": "EvilHD", "DeviceID": "DEV99", "TunerCount": 2,
					"LineupURL": "http://evil.example.com/lineup.json",
				})
			case "bad_scheme":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"FriendlyName": "BadScheme", "LineupURL": "ftp://127.0.0.1/lineup.json",
				})
			default:
				_ = json.NewEncoder(w).Encode(map[string]any{
					"FriendlyName": "", "DeviceID": "DEV1234", "TunerCount": 4,
					"LineupURL": srvURL + "/lineup.json",
				})
			}
		case "/lineup.json":
			if lineupMode == "err500" {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"GuideNumber": "10.1", "GuideName": "CBS HD", "URL": srvURL + "/ch10.ts", "Favorite": 1, "HD": 1},
				{"GuideNumber": "", "GuideName": "", "URL": srvURL + "/ch2.ts", "Favorite": 0, "HD": 0},
				{"GuideNumber": "99", "GuideName": "EmptyURL", "URL": ""},
			})
		case "/ch10.ts":
			w.Header().Set("Content-Type", "video/mp2t")
			_, _ = w.Write([]byte("MPEGTS-DATA-1234"))
		case "/playlist_in_hub":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:6.0,\n" + srvURL + "/ch10.ts\n"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	srvURL = srv.URL

	h := New(store, config.Default(tmp))
	defer h.Close()
	h.SetUserRepo(userRepo)
	h.SetHTTPClient(srv.Client())
	h.SetStreamClient(srv.Client())

	// HDHomeRun fetch, load, error branches, and DiscoverTuners
	var disc map[string]any
	if err := h.fetchHDHomeRunJSON(ctx, srvURL+"/discover.json", "AmbraTest/1.0", &disc); err != nil {
		t.Fatalf("fetchHDHomeRunJSON failed: %v", err)
	}
	chsAll, _, err := h.loadHDHomeRunFromSource(ctx, srvURL, "AmbraTest/1.0", false)
	if err != nil || len(chsAll) != 2 {
		t.Fatalf("expected 2 HDHomeRun channels, got len=%d err=%v", len(chsAll), err)
	}
	chsFav, _, err := h.loadHDHomeRunFromSource(ctx, srvURL, "", true)
	if err != nil || len(chsFav) != 1 || chsFav[0].GroupTitle != "HD" {
		t.Fatalf("expected 1 favorite HD channel, got %+v err=%v", chsFav, err)
	}
	lineupMode = "cross_origin"
	if _, _, err := h.loadHDHomeRunFromSourceWithPolicy(ctx, srvURL, "", false, true); err == nil {
		t.Fatal("expected cross-origin LineupURL rejected")
	}
	lineupMode = "bad_scheme"
	if _, _, err := h.loadHDHomeRunFromSourceWithPolicy(ctx, srvURL, "", false, true); err == nil {
		t.Fatal("expected ftp LineupURL rejected")
	}
	lineupMode = "err500"
	if _, _, err := h.loadHDHomeRunFromSourceWithPolicy(ctx, srvURL, "", false, true); err == nil {
		t.Fatal("expected 500 lineup error")
	}
	lineupMode = "ok"

	// DiscoverTuners: enrich existing + probe unconfigured URL
	h.mu.Lock()
	h.state.TunerHosts = []TunerHostInfo{{
		Id: "th-hdhr", Type: "hdhomerun", Url: srvURL + "/ existing", AllowPrivateNetwork: true,
	}}
	h.mu.Unlock()
	discovered := h.DiscoverTuners(nil, srvURL)
	if len(discovered) < 1 {
		t.Fatalf("expected discovered tuners, got %+v", discovered)
	}

	// canCallerPlayMedia branches (nil req, APIKey, disabled user, UserIDFromCtx fallback)
	if !h.canCallerPlayMedia(nil) {
		t.Fatal("expected nil request allowed")
	}
	apiKeyReq := httptest.NewRequest(http.MethodGet, "/", nil)
	apiKeyReq = apiKeyReq.WithContext(middleware.WithAuthContext(ctx, &middleware.AuthContext{IsAPIKey: true}))
	if !h.canCallerPlayMedia(apiKeyReq) {
		t.Fatal("expected API key allowed to play media")
	}
	disReq := httptest.NewRequest(http.MethodGet, "/", nil)
	disReq = disReq.WithContext(middleware.WithAuthContext(ctx, &middleware.AuthContext{User: disabledUser}))
	if h.canCallerPlayMedia(disReq) {
		t.Fatal("expected disabled user denied media playback")
	}
	h.auth.UserIDFromCtx = func(context.Context) string { return disabledUser.ID }
	if h.canCallerPlayMedia(httptest.NewRequest(http.MethodGet, "/", nil)) {
		t.Fatal("expected disabled UserIDFromCtx denied")
	}
	h.auth.UserIDFromCtx = func(context.Context) string { return playUser.ID }
	if !h.canCallerPlayMedia(httptest.NewRequest(http.MethodGet, "/", nil)) {
		t.Fatal("expected playUser via UserIDFromCtx allowed")
	}
	h.auth.UserIDFromCtx = nil

	// ProxyStreamURL, ProxyChannelStream, proxyChannelStream & multiplexed HLS snapshot
	h.mu.Lock()
	h.state.Channels = []ChannelInfo{
		{Id: "ch-ts", Name: "TS Ch", StreamUrl: srvURL + "/ch10.ts"},
		{Id: "ch-hub-hls", Name: "Hub HLS", StreamUrl: srvURL + "/playlist_in_hub"},
	}
	h.mu.Unlock()

	noPlayReq := httptest.NewRequest(http.MethodGet, "/stream", nil)
	noPlayReq = noPlayReq.WithContext(middleware.WithAuthContext(ctx, &middleware.AuthContext{User: noPlayUser}))
	rec1 := httptest.NewRecorder()
	h.ProxyStreamURL(rec1, noPlayReq, srvURL+"/ch10.ts")
	if rec1.Code != http.StatusForbidden {
		t.Fatalf("expected 403 from ProxyStreamURL for noPlayUser, got %d", rec1.Code)
	}
	rec2 := httptest.NewRecorder()
	h.proxyChannelStream(rec2, noPlayReq, "ch-ts", srvURL+"/ch10.ts")
	if rec2.Code != http.StatusForbidden {
		t.Fatalf("expected 403 from proxyChannelStream for noPlayUser, got %d", rec2.Code)
	}

	okReq := httptest.NewRequest(http.MethodGet, "/LiveTv/Channels/ch-ts/Stream", nil)
	okReq = okReq.WithContext(middleware.WithAuthContext(ctx, &middleware.AuthContext{User: playUser}))
	rec3 := httptest.NewRecorder()
	h.ProxyStreamURL(rec3, okReq, srvURL+"/ch10.ts")
	if rec3.Code != http.StatusOK || !strings.Contains(rec3.Body.String(), "MPEGTS-DATA") {
		t.Fatalf("expected 200 from ProxyStreamURL, got %d %q", rec3.Code, rec3.Body.String())
	}

	rec4 := httptest.NewRecorder()
	h.proxyChannelStream(rec4, okReq, "ch-hub-hls", srvURL+"/playlist_in_hub")
	if rec4.Code != http.StatusOK || !strings.Contains(rec4.Body.String(), "#EXTM3U") {
		t.Fatalf("expected 200 rewritten HLS from multiplexed non-.m3u8 URL, got %d %q", rec4.Code, rec4.Body.String())
	}

	// LiveStreamHub ring overflow, slow subscriber drop, grace=0 removal, and Close with active broadcaster
	var nilHub *LiveStreamHub
	nilHub.Close()

	hub := newLiveStreamHub()
	hub.setIdleTimeout(2 * time.Second)
	hub.setGracePeriod(0)
	b := &liveStreamBroadcaster{
		hub:         hub,
		key:         "k1",
		readyCh:     make(chan struct{}),
		doneCh:      make(chan struct{}),
		subs:        make(map[*liveStreamSubscriber]struct{}),
		gracePeriod: 0,
	}
	close(b.doneCh)
	sub, ok := b.addSubscriber()
	if !ok {
		t.Fatal("expected addSubscriber ok")
	}
	for i := 0; i < streamRingMaxChunks+streamSubQueueDepth+10; i++ {
		b.broadcastChunk([]byte("chunk"))
	}
	b.removeSubscriber(sub)
	if _, ok := b.addSubscriber(); ok {
		t.Fatal("expected broadcaster closed after last subscriber removed with gracePeriod=0")
	}

	b2 := &liveStreamBroadcaster{
		hub:     hub,
		key:     "k2",
		readyCh: make(chan struct{}),
		doneCh:  make(chan struct{}),
		subs:    make(map[*liveStreamSubscriber]struct{}),
	}
	close(b2.doneCh)
	hub.streams["k2"] = b2
	hub.Close()
	if _, _, err := hub.acquireOrCreate("k3", srvURL+"/ch10.ts", "", srv.Client()); err == nil {
		t.Fatal("expected acquireOrCreate on closed hub to error")
	}
}
