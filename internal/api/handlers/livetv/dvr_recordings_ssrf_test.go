package livetv

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestLiveTVV0104ChannelFormatSSRFHeadersDVRAndRecordings(t *testing.T) {
	h, mux, st, _ := setupLiveTVTest(t)
	userRepo := sqlite.NewUserRepo(st)
	h.SetUserRepo(userRepo)

	ctx := context.Background()
	adminU := &sqlite.UserRecord{Name: "admin_v104", IsAdmin: true, Policy: sqlite.DefaultUserPolicy(true), Config: sqlite.DefaultUserConfig()}
	viewerU := &sqlite.UserRecord{Name: "viewer_v104", IsAdmin: false, Policy: sqlite.DefaultUserPolicy(false), Config: sqlite.DefaultUserConfig()}
	_ = userRepo.CreateUser(ctx, adminU)
	_ = userRepo.CreateUser(ctx, viewerU)
	adminAC := &middleware.AuthContext{User: adminU}
	viewerAC := &middleware.AuthContext{User: viewerU}

	// 1. Channel DTO exposes IsHls & StreamFormat for non-admin even when StreamUrl is hidden
	h.mu.Lock()
	h.state.Channels = []ChannelInfo{
		{Id: "ch-hls", Name: "HLS News", Type: "TvChannel", ChannelType: "TV", StreamUrl: "https://93.184.216.34/live/index.m3u8"},
		{Id: "ch-ts", Name: "TS Sports", Type: "TvChannel", ChannelType: "TV", StreamUrl: "http://192.168.1.50:8080/live/stream.ts"},
		{Id: "ch-unk", Name: "Unknown Feed", Type: "TvChannel", ChannelType: "TV", StreamUrl: "https://93.184.216.34/live/feed"},
		{Id: "ch-disabled", Name: "Disabled Feed", Type: "TvChannel", ChannelType: "TV", StreamUrl: "http://192.168.1.50:8080/off.ts", Disabled: true},
		{Id: "ch-loop", Name: "Loopback Evil", Type: "TvChannel", ChannelType: "TV", StreamUrl: "http://127.0.0.1:8096/secret.ts"},
	}
	h.mu.Unlock()

	listRec := doLiveTVAuthReq(t, mux, viewerAC, http.MethodGet, "/LiveTv/Channels", nil)
	if listRec.Code != http.StatusOK {
		t.Fatalf("GET /LiveTv/Channels status=%d", listRec.Code)
	}
	var chList dto.QueryResult[ChannelInfo]
	_ = json.Unmarshal(listRec.Body.Bytes(), &chList)
	byID := map[string]ChannelInfo{}
	for _, ch := range chList.Items {
		if ch.StreamUrl != "" {
			t.Fatalf("non-admin must not receive StreamUrl, got %q for %s", ch.StreamUrl, ch.Id)
		}
		byID[ch.Id] = ch
	}
	if !byID["ch-hls"].IsHls || byID["ch-hls"].StreamFormat != "hls" {
		t.Fatalf("expected ch-hls IsHls=true StreamFormat=hls, got %+v", byID["ch-hls"])
	}
	if byID["ch-ts"].IsHls || byID["ch-ts"].StreamFormat != "mpegts" {
		t.Fatalf("expected ch-ts IsHls=false StreamFormat=mpegts, got %+v", byID["ch-ts"])
	}
	if byID["ch-unk"].IsHls || byID["ch-unk"].StreamFormat != "unknown" {
		t.Fatalf("expected ch-unk IsHls=false StreamFormat=unknown, got %+v", byID["ch-unk"])
	}

	// Single channel GET also populates IsHls/StreamFormat while hiding StreamUrl for non-admin
	oneRec := doLiveTVAuthReq(t, mux, viewerAC, http.MethodGet, "/LiveTv/Channels/ch-hls", nil)
	var oneCh ChannelInfo
	_ = json.Unmarshal(oneRec.Body.Bytes(), &oneCh)
	if oneCh.StreamUrl != "" || !oneCh.IsHls || oneCh.StreamFormat != "hls" {
		t.Fatalf("unexpected single channel for non-admin: %+v", oneCh)
	}

	// 2. Loopback SSRF blocked on Stream proxy by default (127.0.0.1)
	if r := doLiveTVAuthReq(t, mux, adminAC, http.MethodGet, "/LiveTv/Channels/ch-loop/Stream", nil); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 blocking loopback channel stream, got %d body=%s", r.Code, r.Body.String())
	}

	// 3. Public channel cannot sign or proxy RFC1918 LAN targets, and rejects lan=1 escalation
	pubBase, _ := url.Parse("https://93.184.216.34/live/index.m3u8")
	evilM3U := "#EXTM3U\nhttp://192.168.1.1/admin\nhttp://10.0.0.1/secret\nhttp://127.0.0.1:8096/System/Info\nhttp://localhost:8096/\nhttps://93.184.216.34/seg0.ts\n"
	rewrittenPub := h.rewriteHLSPlaylist(evilM3U, pubBase, "ch-hls", "Stream", "")
	for _, forbidden := range []string{"192.168.1.1", "10.0.0.1", "127.0.0.1", "localhost"} {
		if strings.Contains(rewrittenPub, "target=http%3A%2F%2F"+forbidden) {
			t.Fatalf("public channel must not sign private/loopback URI %s:\n%s", forbidden, rewrittenPub)
		}
	}
	if !strings.Contains(rewrittenPub, "lan=0&sig=") {
		t.Fatalf("expected public segment signed with lan=0:\n%s", rewrittenPub)
	}

	// Attempt to escalate public channel ch-hls to lan=1 with a valid signature -> 403
	exp := time.Now().UTC().Add(time.Hour).Unix()
	expStr := strconv.FormatInt(exp, 10)
	lanTarget := "http://192.168.1.1/admin"
	escSig := h.signProxyTarget("ch-hls", lanTarget, true, exp)
	escRec := doLiveTVAuthReq(t, mux, adminAC, http.MethodGet, "/LiveTv/Channels/ch-hls/Stream?target="+url.QueryEscape(lanTarget)+"&exp="+expStr+"&lan=1&sig="+escSig, nil)
	if escRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 blocking lan=1 escalation on public channel, got %d body=%s", escRec.Code, escRec.Body.String())
	}

	// Attempt to proxy RFC1918 target even with lan=0 valid signature -> 400
	lan0Sig := h.signProxyTarget("ch-hls", lanTarget, false, exp)
	lan0Rec := doLiveTVAuthReq(t, mux, adminAC, http.MethodGet, "/LiveTv/Channels/ch-hls/Stream?target="+url.QueryEscape(lanTarget)+"&exp="+expStr+"&lan=0&sig="+lan0Sig, nil)
	if lan0Rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 blocking private LAN target under lan=0, got %d body=%s", lan0Rec.Code, lan0Rec.Body.String())
	}

	// 4. LAN channel allows RFC1918 LAN targets with lan=1, but STILL blocks loopback!
	lanBase, _ := url.Parse("http://192.168.1.50/live/index.m3u8")
	rewrittenLAN := h.rewriteHLSPlaylist("#EXTM3U\nhttp://127.0.0.1:8096/a.ts\nhttp://localhost/b.ts\nseg1.ts\n", lanBase, "ch-ts", "Stream", "")
	if strings.Contains(rewrittenLAN, "127.0.0.1") && strings.Contains(rewrittenLAN, "target=http%3A%2F%2F127.0.0.1") {
		t.Fatalf("LAN channel must still block loopback URI:\n%s", rewrittenLAN)
	}
	if !strings.Contains(rewrittenLAN, "lan=1&sig=") {
		t.Fatalf("expected LAN segment signed with lan=1:\n%s", rewrittenLAN)
	}

	// 5. Proxy response headers hardening (MH-06): nosniff, sandbox CSP, unsafe MIME -> application/octet-stream
	h.publicStreamClient = &http.Client{
		Transport: rtFunc(func(req *http.Request) (*http.Response, error) {
			if strings.HasSuffix(req.URL.Path, ".m3u8") {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
					Body:       io.NopCloser(strings.NewReader("#EXTM3U\nseg0.ts\n")),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"image/svg+xml"}},
				Body:       io.NopCloser(strings.NewReader("<svg onload=alert(1)>")),
			}, nil
		}),
	}
	recM3U := doLiveTVAuthReq(t, mux, adminAC, http.MethodGet, "/LiveTv/Channels/ch-hls/Stream", nil)
	if recM3U.Code != http.StatusOK {
		t.Fatalf("expected 200 for mocked public HLS, got %d", recM3U.Code)
	}
	if recM3U.Header().Get("X-Content-Type-Options") != "nosniff" || recM3U.Header().Get("Content-Security-Policy") != "sandbox" {
		t.Fatalf("missing security headers on HLS proxy: %+v", recM3U.Header())
	}
	if recM3U.Header().Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("expected text/html rewritten to application/octet-stream, got %q", recM3U.Header().Get("Content-Type"))
	}

	recSVG := doLiveTVAuthReq(t, mux, adminAC, http.MethodGet, "/LiveTv/Channels/ch-unk/Stream", nil)
	if recSVG.Code != http.StatusOK {
		t.Fatalf("expected 200 for mocked raw stream, got %d", recSVG.Code)
	}
	if recSVG.Header().Get("X-Content-Type-Options") != "nosniff" || recSVG.Header().Get("Content-Security-Policy") != "sandbox" {
		t.Fatalf("missing security headers on raw stream proxy: %+v", recSVG.Header())
	}
	if recSVG.Header().Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("expected image/svg+xml rewritten to application/octet-stream, got %q", recSVG.Header().Get("Content-Type"))
	}

	// 6. DVR bug: disabled channels skipped in DueRecordingJobs and scheduleTimersForSeriesLocked
	now := time.Now().UTC()
	h.mu.Lock()
	h.state.Timers = []TimerInfo{
		{Id: "tm-disabled", ChannelId: "ch-disabled", Name: "Disabled Show", Status: "New", StartDate: now.Add(-time.Minute), EndDate: now.Add(time.Hour)},
		{Id: "tm-loop", ChannelId: "ch-loop", Name: "Loopback Show", Status: "New", StartDate: now.Add(-time.Minute), EndDate: now.Add(time.Hour)},
		{Id: "tm-ok", ChannelId: "ch-ts", Name: "Enabled Show", Status: "New", StartDate: now.Add(-time.Minute), EndDate: now.Add(time.Hour)},
	}
	h.state.Programs = []ProgramInfo{
		{Id: "p-disabled", ChannelId: "ch-disabled", Name: "Daily News", StartDate: now.Add(time.Hour), EndDate: now.Add(2 * time.Hour)},
		{Id: "p-enabled", ChannelId: "ch-ts", Name: "Daily News", StartDate: now.Add(time.Hour), EndDate: now.Add(2 * time.Hour)},
	}
	h.scheduleTimersForSeriesLocked(SeriesTimerInfo{Id: "st-1", Name: "Daily News", RecordAnyChannel: true})
	var scheduledForDisabled, scheduledForEnabled bool
	for _, tm := range h.state.Timers {
		if tm.SeriesTimerId == "st-1" {
			if tm.ChannelId == "ch-disabled" {
				scheduledForDisabled = true
			}
			if tm.ChannelId == "ch-ts" {
				scheduledForEnabled = true
			}
		}
	}
	h.mu.Unlock()
	if scheduledForDisabled || !scheduledForEnabled {
		t.Fatalf("expected series timer to skip disabled channel (disabled=%v enabled=%v)", scheduledForDisabled, scheduledForEnabled)
	}

	jobs, err := h.DueRecordingJobs(ctx)
	if err != nil {
		t.Fatalf("DueRecordingJobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].TimerID != "tm-ok" {
		t.Fatalf("expected only enabled non-loopback timer tm-ok, got %+v", jobs)
	}

	// 7. Recordings path sanitization for non-admin vs admin
	recItem := h.AddRecording(ctx, RecordingInfo{
		Name:      "Recorded Show",
		ChannelId: "ch-ts",
		Path:      "/srv/ambra/recordings/show.mkv",
	})
	recViewerList := doLiveTVAuthReq(t, mux, viewerAC, http.MethodGet, "/LiveTv/Recordings", nil)
	var vrList dto.QueryResult[RecordingInfo]
	_ = json.Unmarshal(recViewerList.Body.Bytes(), &vrList)
	if len(vrList.Items) != 1 || vrList.Items[0].Path != "" {
		t.Fatalf("expected non-admin Recordings list to hide Path, got %+v", vrList.Items)
	}
	recViewerOne := doLiveTVAuthReq(t, mux, viewerAC, http.MethodGet, "/LiveTv/Recordings/"+recItem.Id, nil)
	var vrOne RecordingInfo
	_ = json.Unmarshal(recViewerOne.Body.Bytes(), &vrOne)
	if vrOne.Path != "" {
		t.Fatalf("expected non-admin GET Recording to hide Path, got %+v", vrOne)
	}
	recAdminOne := doLiveTVAuthReq(t, mux, adminAC, http.MethodGet, "/LiveTv/Recordings/"+recItem.Id, nil)
	var arOne RecordingInfo
	_ = json.Unmarshal(recAdminOne.Body.Bytes(), &arOne)
	if arOne.Path != "/srv/ambra/recordings/show.mkv" {
		t.Fatalf("expected admin GET Recording to retain Path, got %+v", arOne)
	}
}
