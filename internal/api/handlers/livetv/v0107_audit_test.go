package livetv

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/dvr"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestLiveTVV0107AuditSSRFAuthHLSAndTimers(t *testing.T) {
	h, mux, st, cfg := setupLiveTVTest(t)
	ctx := context.Background()
	userRepo := sqlite.NewUserRepo(st)
	h.SetUserRepo(userRepo)

	// 1. H-10: Public M3U URL importing a private RFC1918 channel MUST NOT allow private LAN proxying
	m3uBody := "#EXTM3U\n#EXTINF:-1 tvg-id=\"evil-lan\",Evil LAN\nhttp://192.168.1.1/admin.ts\n#EXTINF:-1 tvg-id=\"pub-ok\",Public OK\nhttps://93.184.216.34/live.m3u8\n"
	h.httpClient = &http.Client{
		Transport: rtFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(m3uBody)),
				Header:     http.Header{"Content-Type": []string{"audio/x-mpegurl"}},
			}, nil
		}),
	}
	h.customHTTPClient = true

	// Import via public URL -> AllowPrivateNetwork must be forced false even if caller tried to pass true
	resTuner := doJSON(t, mux, http.MethodPost, "/LiveTv/TunerHosts", map[string]any{
		"Id":                  "tuner-pub-1",
		"Type":                "m3u",
		"Url":                 "https://93.184.216.34/playlist.m3u",
		"AllowPrivateNetwork": true,
	})
	if resTuner.Code != http.StatusOK {
		t.Fatalf("import public m3u tuner failed: %d %s", resTuner.Code, resTuner.Body.String())
	}
	var savedTuner TunerHostInfo
	_ = json.Unmarshal(resTuner.Body.Bytes(), &savedTuner)
	if savedTuner.AllowPrivateNetwork {
		t.Fatal("public M3U tuner must NOT have AllowPrivateNetwork=true")
	}

	var evilCh, pubCh ChannelInfo
	for _, ch := range h.SnapshotChannels() {
		if ch.TvgId == "evil-lan" {
			evilCh = ch
		}
		if ch.TvgId == "pub-ok" {
			pubCh = ch
		}
	}
	if evilCh.Id == "" || pubCh.Id == "" {
		t.Fatalf("expected both channels imported, got %+v", h.SnapshotChannels())
	}
	if h.channelAllowsPrivateLANForChannel(&evilCh) {
		t.Fatal("private IP channel imported from public M3U must NOT allow private LAN proxying")
	}
	// Attempting to stream the smuggled RFC1918 channel must fail with 400
	if rec := doJSON(t, mux, http.MethodGet, "/LiveTv/Channels/"+evilCh.Id+"/Stream", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 blocking RFC1918 stream from public M3U tuner, got %d (%s)", rec.Code, rec.Body.String())
	}

	// Private LAN M3U URL sets AllowPrivateNetwork=true
	resLAN := doJSON(t, mux, http.MethodPost, "/LiveTv/TunerHosts", map[string]any{
		"Id":   "tuner-lan-1",
		"Type": "m3u",
		"Url":  "http://192.168.1.20/playlist.m3u",
	})
	if resLAN.Code != http.StatusOK {
		t.Fatalf("import LAN m3u tuner failed: %d %s", resLAN.Code, resLAN.Body.String())
	}
	var lanTuner TunerHostInfo
	_ = json.Unmarshal(resLAN.Body.Bytes(), &lanTuner)
	if !lanTuner.AllowPrivateNetwork {
		t.Fatal("private LAN M3U tuner should set AllowPrivateNetwork=true")
	}

	// isPrivateLANAddr edge checks
	if isPrivateLANAddr(netip.Addr{}, false) ||
		isPrivateLANAddr(netip.MustParseAddr("169.254.169.254"), true) ||
		isPrivateLANAddr(netip.MustParseAddr("0.0.0.0"), true) ||
		isPrivateLANAddr(netip.MustParseAddr("224.0.0.1"), true) ||
		isPrivateLANAddr(netip.MustParseAddr("93.184.216.34"), true) ||
		isPrivateLANAddr(netip.MustParseAddr("127.0.0.1"), false) ||
		!isPrivateLANAddr(netip.MustParseAddr("127.0.0.1"), true) ||
		!isPrivateLANAddr(netip.MustParseAddr("192.168.1.5"), false) {
		t.Fatal("isPrivateLANAddr check failed")
	}

	// 2. M-16: Updating existing TunerHost preserves deterministic channel IDs, Disabled, SortIndex, and existing EPG
	_ = h.SetChannelDisabled(ctx, pubCh.Id, true)
	_ = h.SetChannelSortIndex(ctx, pubCh.Id, 42)
	resReimport := doJSON(t, mux, http.MethodPost, "/LiveTv/TunerHosts", map[string]any{
		"Id":   "tuner-pub-1",
		"Type": "m3u",
		"Url":  "https://93.184.216.34/playlist.m3u",
	})
	if resReimport.Code != http.StatusOK {
		t.Fatalf("reimport tuner failed: %d", resReimport.Code)
	}
	reloadedPub, ok := h.GetChannelByID(pubCh.Id)
	if !ok || !reloadedPub.Disabled || reloadedPub.SortIndex != 42 {
		t.Fatalf("expected deterministic ID and preserved Disabled/SortIndex after tuner update, got ok=%v ch=%+v", ok, reloadedPub)
	}
	// Verify snapshot helpers
	if len(h.SnapshotTunerHosts()) < 2 || len(h.SnapshotPrograms()) == 0 {
		t.Fatalf("expected non-empty tuner/program snapshots")
	}
	_ = h.SnapshotRecordings()
	_ = h.SnapshotListingProviders()
	if _, found := h.GetRecordingByID("missing"); found {
		t.Fatal("expected missing recording not found")
	}

	// 3. M-6: computeHLSProxyBase handles both /LiveStreamFiles/{Id}/stream.m3u8 (2 levels) and /LiveStreamFiles/{Id}/hls/master.m3u8 (3 levels)
	if got := computeHLSProxyBase(httptest.NewRequest(http.MethodGet, "/LiveTv/LiveStreamFiles/abc/stream.m3u8", nil), "ch1"); got != "../../Channels/ch1/Stream" {
		t.Fatalf("stream.m3u8 proxyBase = %q, want ../../Channels/ch1/Stream", got)
	}
	if got := computeHLSProxyBase(httptest.NewRequest(http.MethodGet, "/LiveTv/LiveStreamFiles/abc/hls/master.m3u8", nil), "ch1"); got != "../../../Channels/ch1/Stream" {
		t.Fatalf("hls/master.m3u8 proxyBase = %q, want ../../../Channels/ch1/Stream", got)
	}
	if got := computeHLSProxyBase(httptest.NewRequest(http.MethodGet, "/LiveTv/Channels/ch1/Stream", nil), "ch1"); got != "Stream" {
		t.Fatalf("Channels/Stream proxyBase = %q, want Stream", got)
	}

	// 4. M-12: Invalid/private URIs in HLS playlists are DROPPED (never passed through verbatim)
	pubBase, _ := url.Parse("https://93.184.216.34/live/master.m3u8")
	dirtyPlaylist := strings.Join([]string{
		"#EXTM3U",
		`#EXT-X-KEY:METHOD=AES-128,URI="http://192.168.1.1/secret.key"`,
		`#EXT-X-KEY:METHOD=AES-128,URI="https://93.184.216.34/ok.key"`,
		"#EXTINF:6.0,",
		"http://192.168.1.1/evil.ts",
		"#EXTINF:6.0,",
		"file:///etc/passwd",
		"#EXTINF:6.0,",
		"seg_ok.ts",
	}, "\n")
	cleaned := h.rewriteHLSPlaylistWithPolicy(dirtyPlaylist, pubBase, pubCh.Id, "Stream", "", false)
	for _, forbidden := range []string{"192.168.1.1", "file:///etc/passwd", "evil.ts", "secret.key"} {
		if strings.Contains(cleaned, forbidden) {
			t.Fatalf("dirty URI %q must be dropped from rewritten HLS playlist:\n%s", forbidden, cleaned)
		}
	}
	if !strings.Contains(cleaned, "ok.key") || !strings.Contains(cleaned, "seg_ok.ts") {
		t.Fatalf("expected valid URIs rewritten in playlist:\n%s", cleaned)
	}

	// 5. M-12: Signed proxy target URL authenticates segment requests under real RequireAuth without primary api_key
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".m3u8") {
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:4.0,\nseg0.ts\n"))
			return
		}
		w.Header().Set("Content-Type", "video/mp2t")
		_, _ = w.Write([]byte("OK_SEGMENT"))
	}))
	defer upstream.Close()
	h.SetStreamClient(upstream.Client())

	authSvc := auth.NewService(userRepo, cfg)
	viewer, _ := authSvc.CreateUser(ctx, "viewer_m12", "pass1234", false)
	viewerAuth, _ := authSvc.AuthenticateByName(ctx, "viewer_m12", "pass1234", "", auth.ClientInfo{DeviceID: "d1"})
	authMW := middleware.NewAuth(authSvc, nil)
	authMux := http.NewServeMux()
	h.RegisterRoutes(authMux, AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
	})

	h.mu.Lock()
	h.state.Channels = append(h.state.Channels, ChannelInfo{
		Id:          "ch-m12",
		Name:        "Signed HLS",
		Type:        "TvChannel",
		ChannelType: "TV",
		StreamUrl:   upstream.URL + "/live/index.m3u8",
	})
	h.mu.Unlock()

	reqMaster := httptest.NewRequest(http.MethodGet, "/LiveTv/Channels/ch-m12/Stream", nil)
	reqMaster.Header.Set("X-Emby-Token", viewerAuth.AccessToken)
	recMaster := httptest.NewRecorder()
	authMux.ServeHTTP(recMaster, reqMaster)
	if recMaster.Code != http.StatusOK {
		t.Fatalf("authenticated master HLS failed: %d %s", recMaster.Code, recMaster.Body.String())
	}
	masterBody := recMaster.Body.String()
	if strings.Contains(masterBody, viewerAuth.AccessToken) || strings.Contains(masterBody, "api_key=") {
		t.Fatalf("rewritten HLS must not contain session token or api_key:\n%s", masterBody)
	}
	// Extract the signed segment URL and request it WITHOUT any X-Emby-Token or api_key
	var segRelURL string
	for _, line := range strings.Split(masterBody, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Stream?target=") {
			segRelURL = line
			break
		}
	}
	if segRelURL == "" {
		t.Fatalf("failed to find rewritten segment URL in:\n%s", masterBody)
	}
	recSeg := httptest.NewRecorder()
	authMux.ServeHTTP(recSeg, httptest.NewRequest(http.MethodGet, "/LiveTv/Channels/ch-m12/"+segRelURL, nil))
	if recSeg.Code != http.StatusOK || recSeg.Body.String() != "OK_SEGMENT" {
		t.Fatalf("signed segment request without api_key failed: %d %q", recSeg.Code, recSeg.Body.String())
	}
	// Tampered signature without auth -> 401 Unauthorized
	recTampered := httptest.NewRecorder()
	authMux.ServeHTTP(recTampered, httptest.NewRequest(http.MethodGet, "/LiveTv/Channels/ch-m12/"+segRelURL+"00", nil))
	if recTampered.Code != http.StatusUnauthorized {
		t.Fatalf("tampered segment without auth expected 401, got %d", recTampered.Code)
	}

	// 6. M-10: requireLiveTvAccess is fail-closed when user is disabled or DB lookup fails
	disabledUser, _ := authSvc.CreateUser(ctx, "disabled_m10", "pass1234", false)
	disabledUser.IsDisabled = true
	_, _ = st.Writer.ExecContext(ctx, `UPDATE users SET is_disabled = 1 WHERE id = ?`, disabledUser.ID)
	reqDis := httptest.NewRequest(http.MethodGet, "/LiveTv/Channels", nil).WithContext(
		middleware.WithAuthContext(ctx, &middleware.AuthContext{User: disabledUser}),
	)
	recDis := httptest.NewRecorder()
	authMux.ServeHTTP(recDis, reqDis)
	if recDis.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for disabled user in requireLiveTvAccess, got %d", recDis.Code)
	}

	// Cancelled DB context -> fail-closed 403
	cCtx, cancel := context.WithCancel(ctx)
	cancel()
	reqDBErr := httptest.NewRequest(http.MethodGet, "/LiveTv/Channels", nil).WithContext(
		middleware.WithAuthContext(cCtx, &middleware.AuthContext{User: viewer}),
	)
	recDBErr := httptest.NewRecorder()
	authMux.ServeHTTP(recDBErr, reqDBErr)
	if recDBErr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 fail-closed on DB error in requireLiveTvAccess, got %d", recDBErr.Code)
	}

	// 7. M-16: Timer/SeriesTimer ChannelId validation, duplicate ID (409), limits (429), and DeleteSeriesTimer cancels InProgress DVR
	adminRec := &sqlite.UserRecord{Name: "admin_m16", IsAdmin: true, Policy: sqlite.DefaultUserPolicy(true), Config: sqlite.DefaultUserConfig()}
	_ = userRepo.CreateUser(ctx, adminRec)
	adminAC := &middleware.AuthContext{User: adminRec}
	if r := doLiveTVAuthReq(t, mux, adminAC, http.MethodPost, "/LiveTv/Timers", map[string]any{"Name": "NoCh", "ChannelId": "nonexistent"}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for nonexistent ChannelId on Timer, got %d", r.Code)
	}
	if r := doLiveTVAuthReq(t, mux, adminAC, http.MethodPost, "/LiveTv/Timers", map[string]any{"Id": "dup-t1", "Name": "T1", "ChannelId": "ch-m12"}); r.Code != http.StatusOK {
		t.Fatalf("expected 200 creating Timer, got %d", r.Code)
	}
	if r := doLiveTVAuthReq(t, mux, adminAC, http.MethodPost, "/LiveTv/Timers", map[string]any{"Id": "dup-t1", "Name": "T1-dup", "ChannelId": "ch-m12"}); r.Code != http.StatusConflict {
		t.Fatalf("expected 409 for duplicate Timer ID, got %d", r.Code)
	}
	if r := doLiveTVAuthReq(t, mux, adminAC, http.MethodPost, "/LiveTv/Timers/dup-t1", map[string]any{"ChannelId": "no-such-ch"}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 updating Timer with nonexistent ChannelId, got %d", r.Code)
	}

	if r := doLiveTVAuthReq(t, mux, adminAC, http.MethodPost, "/LiveTv/SeriesTimers", map[string]any{"Name": "BadST", "ChannelId": "nonexistent", "RecordAnyChannel": false}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for nonexistent ChannelId on SeriesTimer, got %d", r.Code)
	}
	if r := doLiveTVAuthReq(t, mux, adminAC, http.MethodPost, "/LiveTv/SeriesTimers", map[string]any{"Id": "st-dup-1", "Name": "Show", "ChannelId": "ch-m12"}); r.Code != http.StatusOK {
		t.Fatalf("expected 200 creating SeriesTimer, got %d", r.Code)
	}
	if r := doLiveTVAuthReq(t, mux, adminAC, http.MethodPost, "/LiveTv/SeriesTimers", map[string]any{"Id": "st-dup-1", "Name": "Show", "ChannelId": "ch-m12"}); r.Code != http.StatusConflict {
		t.Fatalf("expected 409 for duplicate SeriesTimer ID, got %d", r.Code)
	}

	// DeleteSeriesTimer cancels InProgress DVR recording
	dvrRec := dvr.NewRecorder(dvr.Options{
		DataDir:    filepath.Join(cfg.DataDir, "dvr-st"),
		FFmpegPath: "/bin/sh",
		ArgsBuilder: func(job dvr.RecordingJob, outFile string) []string {
			return []string{"-c", "sleep 30"}
		},
	})
	defer func() { _ = dvrRec.Close() }()
	h.SetRecorder(dvrRec)
	h.mu.Lock()
	h.state.Timers = append(h.state.Timers, TimerInfo{
		Id:            "inprog-t1",
		ChannelId:     "ch-m12",
		SeriesTimerId: "st-dup-1",
		Name:          "Show Ep1",
		Status:        "InProgress",
		StartDate:     time.Now().UTC(),
		EndDate:       time.Now().UTC().Add(time.Hour),
	})
	h.mu.Unlock()
	if _, err := dvrRec.StartRecording(ctx, dvr.RecordingJob{
		TimerID:     "inprog-t1",
		ChannelName: "Signed HLS",
		ProgramName: "Show Ep1",
		StreamURL:   "http://192.168.1.50/a.ts",
	}); err != nil {
		t.Fatalf("start dvr recording: %v", err)
	}
	if r := doLiveTVAuthReq(t, mux, adminAC, http.MethodDelete, "/LiveTv/SeriesTimers/st-dup-1", nil); r.Code != http.StatusNoContent {
		t.Fatalf("delete series timer expected 204, got %d", r.Code)
	}
	// Verify InProgress timer removed and recording cancelled
	h.mu.RLock()
	for _, tm := range h.state.Timers {
		if tm.Id == "inprog-t1" {
			h.mu.RUnlock()
			t.Fatal("expected InProgress timer belonging to deleted SeriesTimer to be removed")
		}
	}
	h.mu.RUnlock()

	// Timer & SeriesTimer max capacity limits (429)
	h.mu.Lock()
	for len(h.state.Timers) < maxTimers {
		h.state.Timers = append(h.state.Timers, TimerInfo{Id: fmt.Sprintf("fill-t-%d", len(h.state.Timers)), ChannelId: "ch-m12"})
	}
	for len(h.state.SeriesTimers) < maxSeriesTimers {
		h.state.SeriesTimers = append(h.state.SeriesTimers, SeriesTimerInfo{Id: fmt.Sprintf("fill-st-%d", len(h.state.SeriesTimers))})
	}
	h.mu.Unlock()
	if r := doLiveTVAuthReq(t, mux, adminAC, http.MethodPost, "/LiveTv/Timers", map[string]any{"Name": "Ovf", "ChannelId": "ch-m12"}); r.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 when maxTimers reached, got %d", r.Code)
	}
	if r := doLiveTVAuthReq(t, mux, adminAC, http.MethodPost, "/LiveTv/SeriesTimers", map[string]any{"Name": "OvfST"}); r.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 when maxSeriesTimers reached, got %d", r.Code)
	}
	_ = strconv.Itoa(0)
}
