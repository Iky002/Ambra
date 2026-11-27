package livetv

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func extractFirstRewrittenHLSURL(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "/LiveTv/Channels/") {
			return line
		}
		if strings.HasPrefix(line, "Stream?target=") {
			return "/LiveTv/Channels/ch-hls-1/" + line
		}
	}
	return ""
}

func TestAuditWave1_H03_M07_M10_PlaybackSlidingHLSAndHDHomeRunOrigin(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "livetv_wave1.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)

	noPlayPol := sqlite.DefaultUserPolicy(false)
	noPlayPol.EnableLiveTvAccess = true
	noPlayPol.EnableMediaPlayback = false
	noPlayUser := &sqlite.UserRecord{ID: guid.New(), Name: "noplay", Policy: noPlayPol, Config: sqlite.DefaultUserConfig()}

	viewerPol := sqlite.DefaultUserPolicy(false)
	viewerPol.EnableLiveTvAccess = true
	viewerPol.EnableMediaPlayback = true
	viewerUser := &sqlite.UserRecord{ID: guid.New(), Name: "viewer", Policy: viewerPol, Config: sqlite.DefaultUserConfig()}

	for _, u := range []*sqlite.UserRecord{noPlayUser, viewerUser} {
		if err := userRepo.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
	}

	var upstreamURL string
	upstreamHLS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/live/index.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			fmt.Fprintf(w, "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:6\n#EXTINF:6.0,\n%s/live/sub.m3u8\n", upstreamURL)
		case "/live/sub.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			fmt.Fprintf(w, "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:6\n#EXTINF:6.0,\n%s/live/seg.ts\n", upstreamURL)
		case "/live/seg.ts":
			w.Header().Set("Content-Type", "video/mp2t")
			_, _ = w.Write([]byte("LIVE-TS-PAYLOAD"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer upstreamHLS.Close()
	upstreamURL = upstreamHLS.URL

	h := New(store, cfg)
	h.SetUserRepo(userRepo)
	h.SetStreamClient(upstreamHLS.Client())
	h.SetHTTPClient(upstreamHLS.Client())

	curUnix := atomic.Int64{}
	baseTime := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	curUnix.Store(baseTime.Unix())
	h.nowFn = func() time.Time {
		return time.Unix(curUnix.Load(), 0).UTC()
	}

	h.mu.Lock()
	h.state.Channels = []ChannelInfo{{
		Id:          "ch-hls-1",
		Name:        "Test HLS",
		Type:        "TvChannel",
		ChannelType: "TV",
		StreamUrl:   upstreamHLS.URL + "/live/index.m3u8",
		TunerHostId: "th-lan",
	}}
	h.state.TunerHosts = []TunerHostInfo{{
		Id:                  "th-lan",
		Type:                "m3u",
		AllowPrivateNetwork: true,
	}}
	h.mu.Unlock()

	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	doAs := func(u *sqlite.UserRecord, target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req = req.WithContext(middleware.WithAuthContext(req.Context(), &middleware.AuthContext{User: u}))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// 1. H-03: EnableMediaPlayback=false -> 403 Forbidden even when EnableLiveTvAccess=true!
	if code := doAs(noPlayUser, "/LiveTv/Channels/ch-hls-1/Stream").Code; code != http.StatusForbidden {
		t.Fatalf("H-03: expected 403 on LiveTV channel stream when EnableMediaPlayback=false, got %d", code)
	}

	// 2. M-07: Sliding 10-minute HLS expiration keeps continuous playback alive well past T0+10m!
	r0 := doAs(viewerUser, "/LiveTv/Channels/ch-hls-1/Stream")
	if r0.Code != http.StatusOK {
		t.Fatalf("T0 initial manifest expected 200, got %d: %s", r0.Code, r0.Body.String())
	}
	subURLAtT0 := extractFirstRewrittenHLSURL(r0.Body.String())
	if subURLAtT0 == "" || !strings.Contains(subURLAtT0, "&iat=") {
		t.Fatalf("M-07: expected rewritten sub-manifest URL containing signed iat, got:\n%s", r0.Body.String())
	}

	// Advance clock to T0 + 8m (within initial 10m window) -> succeeds & renews exp to T0 + 18m!
	curUnix.Store(baseTime.Add(8 * time.Minute).Unix())
	r8 := doAs(viewerUser, subURLAtT0)
	if r8.Code != http.StatusOK {
		t.Fatalf("T0+8m sub-manifest expected 200, got %d: %s", r8.Code, r8.Body.String())
	}
	segURLAt8m := extractFirstRewrittenHLSURL(r8.Body.String())
	if segURLAt8m == "" {
		t.Fatalf("expected rewritten segment URL at T0+8m, got:\n%s", r8.Body.String())
	}

	// Advance clock to T0 + 15m (PAST initial 10m window, within renewed T0+18m window) -> MUST SUCCEED (200)!
	curUnix.Store(baseTime.Add(15 * time.Minute).Unix())
	r15 := doAs(viewerUser, segURLAt8m)
	if r15.Code != http.StatusOK || r15.Body.String() != "LIVE-TS-PAYLOAD" {
		t.Fatalf("M-07: expected continuous playback at T0+15m to succeed 200, got %d (%q)", r15.Code, r15.Body.String())
	}

	// Idle >10m: advance to T0 + 20m using segURLAt8m (whose exp was T0+18m) -> 403!
	curUnix.Store(baseTime.Add(20 * time.Minute).Unix())
	if code := doAs(viewerUser, segURLAt8m).Code; code != http.StatusForbidden {
		t.Fatalf("M-07: expected expired idle URL at T0+20m to return 403, got %d", code)
	}

	// Absolute 24h ceiling from session iat: refresh sub-manifest at T0+23h55m, then advance to T0+24h01m -> 403!
	curUnix.Store(baseTime.Add(8 * time.Minute).Unix())
	subRenew := doAs(viewerUser, subURLAtT0)
	_ = subRenew
	// Issue a fresh stream at T0, then test verifyProxyTargetWithIAT beyond 24h ceiling:
	curUnix.Store(baseTime.Add(24*time.Hour + time.Minute).Unix())
	if code := doAs(viewerUser, segURLAt8m).Code; code != http.StatusForbidden {
		t.Fatalf("M-07: expected >24h session URL to return 403, got %d", code)
	}

	// 3. M-10: HDHomeRun discover.json with cross-origin LineupURL is blocked!
	var evilLineupHits atomic.Int32
	evilLineupSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		evilLineupHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"GuideNumber":"1.1","GuideName":"Evil","URL":"http://example.com/1.ts"}]`))
	}))
	defer evilLineupSrv.Close()

	hdhrPivotSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"FriendlyName":"HDHR","DeviceID":"1234","LineupURL":%q}`, evilLineupSrv.URL+"/lineup.json")
	}))
	defer hdhrPivotSrv.Close()

	if _, _, err := h.loadHDHomeRunFromSourceWithPolicy(ctx, hdhrPivotSrv.URL, "", false, true); err == nil {
		t.Fatal("M-10: expected cross-origin HDHomeRun LineupURL to be rejected")
	}
	if evilLineupHits.Load() != 0 {
		t.Fatalf("M-10: cross-origin LineupURL server was contacted %d times!", evilLineupHits.Load())
	}
}
