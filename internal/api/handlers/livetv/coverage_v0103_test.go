package livetv

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/core/dvr"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read failure") }
func (errReader) Close() error             { return nil }

func TestLiveTVAuthAndAccessCoverage(t *testing.T) {
	h, _, st, _ := setupLiveTVTest(t)
	userRepo := sqlite.NewUserRepo(st)
	h.SetUserRepo(userRepo)

	ctx := context.Background()
	admin := &sqlite.UserRecord{ID: guid.New(), Name: "admin", IsAdmin: true, Policy: sqlite.DefaultUserPolicy(true), Config: sqlite.DefaultUserConfig()}
	mgrPol := sqlite.DefaultUserPolicy(false)
	mgrPol.EnableLiveTvManagement = true
	mgr := &sqlite.UserRecord{ID: guid.New(), Name: "mgr", Policy: mgrPol, Config: sqlite.DefaultUserConfig()}
	noTVPol := sqlite.DefaultUserPolicy(false)
	noTVPol.EnableLiveTvAccess = false
	noTVPol.EnableLiveTvManagement = false
	noTV := &sqlite.UserRecord{ID: guid.New(), Name: "notv", Policy: noTVPol, Config: sqlite.DefaultUserConfig()}
	disabled := &sqlite.UserRecord{ID: guid.New(), Name: "disabled", IsDisabled: true, Policy: mgrPol, Config: sqlite.DefaultUserConfig()}
	for _, u := range []*sqlite.UserRecord{admin, mgr, noTV, disabled} {
		if err := userRepo.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
	}

	var ctxUID string
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{
		UserIDFromCtx: func(context.Context) string { return ctxUID },
	})

	// 1. HasLiveTvManagement across nil ctx/req, DB user lookup, disabled user, APIKey without user, UserIDFromCtx
	reqMgr := httptest.NewRequest(http.MethodGet, "/", nil)
	reqMgr = reqMgr.WithContext(middleware.WithAuthContext(reqMgr.Context(), &middleware.AuthContext{
		User: &sqlite.UserRecord{ID: mgr.ID, Name: mgr.Name},
	}))
	if !h.HasLiveTvManagement(nil, reqMgr) {
		t.Fatalf("expected HasLiveTvManagement true via DB lookup with nil ctx")
	}
	ctxOnly := middleware.WithAuthContext(ctx, &middleware.AuthContext{
		User: &sqlite.UserRecord{ID: disabled.ID, Name: disabled.Name},
	})
	if h.HasLiveTvManagement(ctxOnly, nil) {
		t.Fatalf("expected HasLiveTvManagement false for disabled DB user")
	}
	ctxAPIKeyNoAdmin := middleware.WithAuthContext(ctx, &middleware.AuthContext{IsAPIKey: false, User: nil})
	if h.HasLiveTvManagement(ctxAPIKeyNoAdmin, nil) {
		t.Fatalf("expected HasLiveTvManagement false for empty AuthContext")
	}

	// Via UserIDFromCtx with ur != nil
	ctxUID = ""
	if h.HasLiveTvManagement(ctx, nil) {
		t.Fatalf("expected false for empty UserIDFromCtx")
	}
	ctxUID = disabled.ID
	if h.HasLiveTvManagement(ctx, nil) {
		t.Fatalf("expected false for disabled UserIDFromCtx")
	}
	ctxUID = mgr.ID
	if !h.HasLiveTvManagement(ctx, nil) {
		t.Fatalf("expected true for manager via UserIDFromCtx")
	}

	// Via UserIDFromCtx when userRepo == nil
	hNoRepo := New(nil, nil)
	hNoRepo.auth = AuthFuncs{UserIDFromCtx: func(context.Context) string { return mgr.ID }}
	if hNoRepo.HasLiveTvManagement(ctx, nil) {
		t.Fatalf("expected false when userRepo is nil")
	}

	// 2. requireLiveTvAccess denies users with EnableLiveTvAccess=false via both AuthContext and UserIDFromCtx
	reqNoTV := httptest.NewRequest(http.MethodGet, "/LiveTv/Channels", nil)
	reqNoTV = reqNoTV.WithContext(middleware.WithAuthContext(reqNoTV.Context(), &middleware.AuthContext{
		User: &sqlite.UserRecord{ID: noTV.ID, Name: noTV.Name, Policy: sqlite.DefaultUserPolicy(false)}, // stale policy says true, DB says false
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, reqNoTV)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for DB EnableLiveTvAccess=false via AuthContext, got %d", rec.Code)
	}

	ctxUID = noTV.ID
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/LiveTv/Channels", nil))
	if rec2.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for DB EnableLiveTvAccess=false via UserIDFromCtx, got %d", rec2.Code)
	}

	// 3. callerAuthInfo edge cases
	if uid, adm, ok := h.callerAuthInfo(nil); uid != "" || adm || ok {
		t.Fatalf("expected empty callerAuthInfo for nil request")
	}
	ctxUID = admin.ID
	if uid, adm, ok := h.callerAuthInfo(httptest.NewRequest(http.MethodGet, "/", nil)); uid != admin.ID || !adm || !ok {
		t.Fatalf("expected admin callerAuthInfo via UserIDFromCtx, got (%q, %v, %v)", uid, adm, ok)
	}
	ctxUID = "unknown-id"
	if uid, adm, ok := h.callerAuthInfo(httptest.NewRequest(http.MethodGet, "/", nil)); uid == "" || adm || !ok {
		t.Fatalf("expected non-admin callerAuthInfo for unknown uid, got (%q, %v, %v)", uid, adm, ok)
	}
}

func TestLiveTVProxyHLSAndTokenCoverage(t *testing.T) {
	h, _, _, _ := setupLiveTVTest(t)

	// verifyProxyTarget with empty secret -> false
	hEmptySecret := &Handler{}
	if hEmptySecret.verifyProxyTarget("ch1", "http://example.com", false, "9999999999", "deadbeef") {
		t.Fatalf("expected false when proxySecret is empty")
	}

	// channelAllowsPrivateLAN distinguishes public vs private LAN vs blocked loopback
	if h.channelAllowsPrivateLAN("://bad") || h.channelAllowsPrivateLAN("/relative") || h.channelAllowsPrivateLAN("https://93.184.216.34/a") {
		t.Fatalf("expected public/invalid URLs not to allow private LAN")
	}
	if !h.channelAllowsPrivateLAN("http://192.168.1.50/a.m3u8") {
		t.Fatalf("expected private LAN URL to allow private LAN")
	}
	if h.channelAllowsPrivateLAN("http://localhost/a") || h.channelAllowsPrivateLAN("http://foo.localhost/a") || h.channelAllowsPrivateLAN("http://127.0.0.1/a") {
		t.Fatalf("expected loopback URLs to be rejected")
	}

	// rewriteHLSPlaylist & rewriteHLSPlaylistWithPolicy edge cases
	pubBase, _ := url.Parse("https://93.184.216.34/live/index.m3u8")
	lanBase, _ := url.Parse("http://192.168.1.50:8080/live/index.m3u8")
	pl := "#EXTM3U\r\n#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\"\r\n#EXT-X-MAP:URI=\"\"\r\nfile:///etc/passwd\r\nhttp://[::1]:bad\r\nseg1.ts\r\n"
	outPub := h.rewriteHLSPlaylist(pl, pubBase, "ch1", "", "tok1")
	if !strings.Contains(outPub, "Stream?target=") || !strings.Contains(outPub, "&lan=0&sig=") || !strings.Contains(outPub, "api_key=tok1") {
		t.Fatalf("expected rewritten HLS URLs, got:\n%s", outPub)
	}
	outLAN := h.rewriteHLSPlaylist("#EXTM3U\nseg1.ts\n", lanBase, "ch1", "Stream", "")
	if !strings.Contains(outLAN, "Stream?target=") || !strings.Contains(outLAN, "&lan=1&sig=") {
		t.Fatalf("expected LAN HLS rewrite when base is LAN, got:\n%s", outLAN)
	}
	if got := h.rewriteHLSPlaylistWithPolicy("#EXTM3U\nseg1.ts\n", nil, "ch1", "", "", false); !strings.Contains(got, "seg1.ts") {
		t.Fatalf("expected nil baseURL to preserve URI")
	}

	// extractRequestToken & parseTokenFromAuthHeader edge cases
	if extractRequestToken(nil) != "" {
		t.Fatalf("expected empty token for nil req")
	}
	rMB := httptest.NewRequest(http.MethodGet, "/", nil)
	rMB.Header.Set("X-MediaBrowser-Token", "mb-tok")
	if got := extractRequestToken(rMB); got != "mb-tok" {
		t.Fatalf("X-MediaBrowser-Token got %q", got)
	}
	rNone := httptest.NewRequest(http.MethodGet, "/", nil)
	if got := extractRequestToken(rNone); got != "" {
		t.Fatalf("expected empty token when no auth headers")
	}
	for _, tc := range []struct {
		hdr  string
		want string
	}{
		{"Bearer bear-123", "bear-123"},
		{`MediaBrowser Client="web", Token=unquoted, Version="1"`, "unquoted"},
		{`Emby Token=trailing`, "trailing"},
		{`MediaBrowser Token="unterminated`, "unterminated"},
		{`MediaBrowser Client="web", NoEqualsHere`, ""},
		{`MediaBrowser Client="web", `, ""},
	} {
		if got := parseTokenFromAuthHeader(tc.hdr); got != tc.want {
			t.Fatalf("parseTokenFromAuthHeader(%q) = %q, want %q", tc.hdr, got, tc.want)
		}
	}

	// ProxyStreamURL & ProxyChannelStream with mocked streamClient
	var mode string
	mockClient := &http.Client{
		Transport: rtFunc(func(req *http.Request) (*http.Response, error) {
			switch mode {
			case "err":
				return nil, errors.New("dial error")
			case "hls":
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{}, // empty Content-Type, .m3u8 URL triggers HLS rewrite & default CT
					Body:       io.NopCloser(strings.NewReader("#EXTM3U\nseg0.ts\n")),
				}, nil
			case "hls-read-err":
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/vnd.apple.mpegurl"}},
					Body:       errReader{},
				}, nil
			case "hls-oversize":
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/vnd.apple.mpegurl"}},
					Body:       io.NopCloser(bytes.NewReader(bytes.Repeat([]byte("a"), maxHLSPlaylistBytes+10))),
				}, nil
			default:
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{}, // empty Content-Type -> video/mp2t
					Body:       io.NopCloser(strings.NewReader("ts-bytes")),
				}, nil
			}
		}),
	}
	h.streamClient = mockClient
	h.publicStreamClient = mockClient

	// 1. ProxyStreamURL raw TS stream with empty Content-Type -> video/mp2t
	mode = "ts"
	rec := httptest.NewRecorder()
	h.ProxyStreamURL(rec, httptest.NewRequest(http.MethodGet, "/stream", nil), "https://93.184.216.34/live.ts")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "video/mp2t" {
		t.Fatalf("ProxyStreamURL ts: code=%d ct=%q", rec.Code, rec.Header().Get("Content-Type"))
	}

	// 2. ProxyStreamURL invalid control char URL -> 400
	rec = httptest.NewRecorder()
	h.ProxyStreamURL(rec, httptest.NewRequest(http.MethodGet, "/stream", nil), "https://93.184.216.34/\x7f")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("ProxyStreamURL bad url: want 400, got %d", rec.Code)
	}

	// 3. ProxyStreamURL upstream error -> 502; canceled context -> silent
	mode = "err"
	rec = httptest.NewRecorder()
	h.ProxyStreamURL(rec, httptest.NewRequest(http.MethodGet, "/stream", nil), "https://93.184.216.34/live.ts")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("ProxyStreamURL upstream err: want 502, got %d", rec.Code)
	}
	cctx, cancel := context.WithCancel(context.Background())
	cancel()
	rec = httptest.NewRecorder()
	h.ProxyStreamURL(rec, httptest.NewRequest(http.MethodGet, "/stream", nil).WithContext(cctx), "https://93.184.216.34/live.ts")

	// 4. ProxyChannelStream HLS with /LiveStreamFiles/ path -> relative proxyBase + default CT
	mode = "hls"
	rec = httptest.NewRecorder()
	h.ProxyChannelStream(rec, httptest.NewRequest(http.MethodGet, "/LiveTv/LiveStreamFiles/abc/stream.m3u8", nil), "ch1", "https://93.184.216.34/live/index.m3u8")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/vnd.apple.mpegurl" || !strings.Contains(rec.Body.String(), "../../Channels/ch1/Stream") {
		t.Fatalf("ProxyChannelStream hls: code=%d ct=%q body=%s", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}

	// 5. ProxyChannelStream HLS read error -> 502
	mode = "hls-read-err"
	rec = httptest.NewRecorder()
	h.ProxyChannelStream(rec, httptest.NewRequest(http.MethodGet, "/stream", nil), "ch1", "https://93.184.216.34/live/index.m3u8")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("ProxyChannelStream hls read err: want 502, got %d", rec.Code)
	}

	// 6. ProxyChannelStream HLS oversize -> 502
	mode = "hls-oversize"
	rec = httptest.NewRecorder()
	h.ProxyChannelStream(rec, httptest.NewRequest(http.MethodGet, "/stream", nil), "ch1", "https://93.184.216.34/live/index.m3u8")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("ProxyChannelStream hls oversize: want 502, got %d", rec.Code)
	}

	// 7. Nil streamClient/publicStreamClient fallback path
	h.streamClient = nil
	h.publicStreamClient = nil
	rec = httptest.NewRecorder()
	h.ProxyStreamURL(rec, httptest.NewRequest(http.MethodGet, "/stream", nil).WithContext(cctx), "https://93.184.216.34/live.ts")
}

func TestLiveTVChannelsProgramsTimersAndRecordingsCoverage(t *testing.T) {
	h, mux, st, _ := setupLiveTVTest(t)
	now := time.Now().UTC().Truncate(time.Minute)

	// Populate channels with Group, ChannelType, SortIndex, Disabled
	h.mu.Lock()
	h.state.Channels = []ChannelInfo{
		{Id: "ch-1", Name: "News HD", GroupTitle: "News", ChannelType: "TV", StreamUrl: "https://93.184.216.34/1.ts", SortIndex: 2},
		{Id: "ch-2", Name: "Radio FM", GroupTitle: "Music", ChannelType: "Radio", StreamUrl: "", SortIndex: 1},
		{Id: "ch-3", Name: "Disabled TV", GroupTitle: "News", ChannelType: "TV", StreamUrl: "https://93.184.216.34/3.ts", Disabled: true},
	}
	h.state.Programs = []ProgramInfo{
		{Id: "p1", Name: "Alpha Movie", ChannelId: "ch-1", ChannelName: "News HD", StartDate: now.Add(time.Hour), EndDate: now.Add(3 * time.Hour), IsMovie: true},
		{Id: "p2", Name: "Beta Show", ChannelId: "ch-2", ChannelName: "Radio FM", StartDate: now.Add(time.Hour), EndDate: now.Add(2 * time.Hour), IsSeries: true},
	}
	h.state.Recordings = []RecordingInfo{
		{Id: "r1", Name: "Rec 1", ChannelId: "ch-1", Status: "Completed"},
		{Id: "r2", Name: "Rec 2", ChannelId: "ch-2", Status: "Failed"},
	}
	h.state.Timers = []TimerInfo{
		{Id: "t-done", Name: "Done", ChannelId: "ch-1", Status: "Completed", StartDate: now.Add(-time.Hour), EndDate: now.Add(time.Hour)},
		{Id: "t-noch", Name: "NoCh", ChannelId: "ch-missing", Status: "New", StartDate: now.Add(-time.Minute), EndDate: now.Add(time.Hour)},
		{Id: "t-nourl", Name: "NoURL", ChannelId: "ch-2", Status: "New", StartDate: now.Add(-time.Minute), EndDate: now.Add(time.Hour)},
	}
	h.mu.Unlock()

	// Channels filtering by GroupTitle & Type and SortIndex ordering
	rec := doJSON(t, mux, http.MethodGet, "/LiveTv/Channels?GroupTitle=News&Type=TV", nil)
	var chRes dto.QueryResult[ChannelInfo]
	_ = json.Unmarshal(rec.Body.Bytes(), &chRes)
	if chRes.TotalRecordCount != 2 || chRes.Items[0].Id != "ch-3" || chRes.Items[1].Id != "ch-1" {
		t.Fatalf("filtered channels unexpected: %+v", chRes)
	}
	if err := h.SetChannelDisabled(context.Background(), "ch-1", true); err != nil {
		t.Fatalf("SetChannelDisabled: %v", err)
	}

	// DueRecordingJobs with only invalid/completed timers -> returns nil, nil
	jobs, err := h.DueRecordingJobs(context.Background())
	if err != nil || len(jobs) != 0 {
		t.Fatalf("expected 0 due jobs, got %d err=%v", len(jobs), err)
	}

	// OnRecordingFinished: Cancelled, Completed with fallback channelName and fallback "Recording"
	h.mu.Lock()
	h.state.Timers = append(h.state.Timers,
		TimerInfo{Id: "t-cancel", Name: "C", ChannelId: "ch-1", ChannelName: "News HD", Status: "InProgress"},
		TimerInfo{Id: "t-noname", Name: "", ChannelId: "ch-1", ChannelName: "News HD", Status: "InProgress"},
	)
	h.mu.Unlock()
	h.OnRecordingFinished(context.Background(), dvr.RecordingState{Job: dvr.RecordingJob{TimerID: "t-cancel"}, Status: dvr.StatusCancelled})
	h.OnRecordingFinished(context.Background(), dvr.RecordingState{Job: dvr.RecordingJob{TimerID: "t-noname"}, Status: dvr.StatusCompleted})
	h.OnRecordingFinished(context.Background(), dvr.RecordingState{Job: dvr.RecordingJob{TimerID: "t-orphan"}, Status: dvr.StatusCompleted})

	// flexStringList UnmarshalJSON (array + non-string/non-array) & parseQueryTime invalid
	var fl flexStringList
	if err := json.Unmarshal([]byte(`["a,b"," c "]`), &fl); err != nil || len(fl) != 3 {
		t.Fatalf("flexStringList array got %v err=%v", fl, err)
	}
	if err := json.Unmarshal([]byte(`123`), &fl); err != nil {
		t.Fatalf("flexStringList int err=%v", err)
	}
	if parseQueryTime("not-a-time") != nil {
		t.Fatalf("expected nil from parseQueryTime on invalid time")
	}

	// Programs filtering & sorting: SearchTerm, IsMovie, IsSeries, MaxStartDate, MinEndDate, EndDate/Name/ChannelName/StartDate tie
	for _, q := range []string{
		"/LiveTv/Programs?SearchTerm=Alpha&IsMovie=true",
		"/LiveTv/Programs?IsSeries=true&MaxStartDate=" + url.QueryEscape(now.Add(2*time.Hour).Format(time.RFC3339)) + "&MinEndDate=" + url.QueryEscape(now.Format(time.RFC3339)),
		"/LiveTv/Programs?SortBy=EndDate,Name,ChannelName&SortOrder=Ascending",
		"/LiveTv/Programs?SortBy=StartDate&SortOrder=Ascending",
	} {
		r := doJSON(t, mux, http.MethodGet, q, nil)
		if r.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d", q, r.Code)
		}
	}
	if compareTimes(now, now) != 0 || compareTimes(now, now.Add(time.Second)) != -1 || compareTimes(now.Add(time.Second), now) != 1 {
		t.Fatalf("compareTimes unexpected")
	}

	// GuideInfo with populated programs
	if r := doJSON(t, mux, http.MethodGet, "/LiveTv/GuideInfo", nil); r.Code != http.StatusOK {
		t.Fatalf("GET /LiveTv/GuideInfo status=%d", r.Code)
	}

	// Recordings filtering by ChannelId & Status + resolvePathWithinDir edge cases
	rec = doJSON(t, mux, http.MethodGet, "/LiveTv/Recordings?ChannelId=ch-1&Status=Completed", nil)
	var recRes dto.QueryResult[RecordingInfo]
	_ = json.Unmarshal(rec.Body.Bytes(), &recRes)
	if recRes.TotalRecordCount != 2 {
		t.Fatalf("expected 2 filtered recordings (r1 + t-noname), got %d", recRes.TotalRecordCount)
	}
	if _, ok := resolvePathWithinDir("", "/tmp/a"); ok {
		t.Fatalf("expected false for empty dir")
	}
	if _, ok := resolvePathWithinDir("/nonexistent-dir-xyz", "/tmp/a"); ok {
		t.Fatalf("expected false for nonexistent dir")
	}
	if _, ok := resolvePathWithinDir(t.TempDir(), "/nonexistent-file-xyz"); ok {
		t.Fatalf("expected false for nonexistent target")
	}

	// Timers filtering by ChannelId, validateTimerBounds errors, updateTimer bounds error
	_ = doJSON(t, mux, http.MethodGet, "/LiveTv/Timers?ChannelId=ch-1", nil)
	if err := validateTimerBounds(now, now.Add(time.Hour), 0, -1); err == nil {
		t.Fatalf("expected error for negative PostPaddingSeconds")
	}
	if err := validateTimerBounds(now, now, 0, 0); err == nil {
		t.Fatalf("expected error for EndDate <= StartDate")
	}
	createRec := doJSON(t, mux, http.MethodPost, "/LiveTv/Timers", map[string]any{
		"Name": "ValidTimer", "ChannelId": "ch-1",
		"StartDate": now.Add(time.Hour), "EndDate": now.Add(2 * time.Hour),
	})
	var createdTimer TimerInfo
	_ = json.Unmarshal(createRec.Body.Bytes(), &createdTimer)
	if r := doJSON(t, mux, http.MethodPost, "/LiveTv/Timers/"+createdTimer.Id, map[string]any{
		"PostPaddingSeconds": 99999,
	}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid timer update bounds, got %d", r.Code)
	}

	// SeriesTimer padding validation, Days weekday filtering, empty series no-op, duplicate by (ChannelId, StartDate, Name)
	if r := doJSON(t, mux, http.MethodPost, "/LiveTv/SeriesTimers", map[string]any{"Name": "Bad", "PrePaddingSeconds": -5}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad series timer padding, got %d", r.Code)
	}
	otherDay := now.Add(24 * time.Hour).UTC().Weekday().String()
	stRec := doJSON(t, mux, http.MethodPost, "/LiveTv/SeriesTimers", map[string]any{
		"Name": "Beta Show", "ChannelId": "ch-2", "Days": []string{otherDay},
	})
	var createdST SeriesTimerInfo
	_ = json.Unmarshal(stRec.Body.Bytes(), &createdST)
	if r := doJSON(t, mux, http.MethodPost, "/LiveTv/SeriesTimers/"+createdST.Id, map[string]any{
		"Name": "Beta Show", "PostPaddingSeconds": 99999,
	}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad series timer update padding, got %d", r.Code)
	}
	h.mu.Lock()
	h.scheduleTimersForSeriesLocked(SeriesTimerInfo{}) // empty seriesID & Name -> no-op
	h.state.Timers = append(h.state.Timers, TimerInfo{
		Id: "dup-by-fields", ChannelId: "ch-2", Name: "Beta Show", StartDate: now.Add(time.Hour), EndDate: now.Add(2 * time.Hour),
	})
	h.scheduleTimersForSeriesLocked(SeriesTimerInfo{Id: "st-dup", Name: "Beta Show", RecordAnyChannel: true})
	h.mu.Unlock()

	// Closed DB persistence errors (500) across Timer, UpdateTimer, SeriesTimer, UpdateSeriesTimer, TunerHost, ListingProvider, loadState, saveStateLocked(nil store)
	_ = st.Close()
	h.loadState(context.Background())
	if r := doJSON(t, mux, http.MethodPost, "/LiveTv/Timers", map[string]any{"Name": "Fail", "ChannelId": "ch-1"}); r.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 create timer on closed DB, got %d", r.Code)
	}
	if r := doJSON(t, mux, http.MethodPost, "/LiveTv/Timers/"+createdTimer.Id, map[string]any{"Name": "FailUpd"}); r.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 update timer on closed DB, got %d", r.Code)
	}
	if r := doJSON(t, mux, http.MethodPost, "/LiveTv/SeriesTimers", map[string]any{"Name": "FailST"}); r.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 create series timer on closed DB, got %d", r.Code)
	}
	if r := doJSON(t, mux, http.MethodPost, "/LiveTv/SeriesTimers/"+createdST.Id, map[string]any{"Name": "FailSTUpd"}); r.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 update series timer on closed DB, got %d", r.Code)
	}
	if r := doJSON(t, mux, http.MethodPost, "/LiveTv/TunerHosts", map[string]any{"Type": "m3u", "Url": ""}); r.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 save tuner host on closed DB, got %d", r.Code)
	}
	if r := doJSON(t, mux, http.MethodPost, "/LiveTv/ListingProviders", map[string]any{"Type": "xmltv", "Path": ""}); r.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 save listing provider on closed DB, got %d", r.Code)
	}
	hNilStore := New(nil, nil)
	if err := hNilStore.saveStateLocked(context.Background()); err != nil {
		t.Fatalf("expected nil error for saveStateLocked with nil store, got %v", err)
	}
}

func TestLiveTVM3UXMLTVPathsAndParsersCoverage(t *testing.T) {
	h, _, st, cfg := setupLiveTVTest(t)
	ctx := context.Background()

	// Add a library so allowedLocalM3URoots iterates library paths
	libDir := t.TempDir()
	libRepo := sqlite.NewLibraryRepo(st)
	_, _ = libRepo.CreateLibrary(ctx, "TV", "tvshows", []string{libDir, ""})

	// isPathWithinRoot edge cases
	if isPathWithinRoot("", libDir) || !isPathWithinRoot(libDir, libDir) {
		t.Fatalf("isPathWithinRoot unexpected result")
	}

	// Local M3U & XMLTV validation errors (empty, bad ext, missing file, symlink to .txt)
	if _, err := h.loadM3UFromSource(ctx, "  ", ""); err == nil {
		t.Fatalf("expected error for empty m3u source")
	}
	if _, err := h.loadXMLTVFromSource(ctx, "  "); err == nil {
		t.Fatalf("expected error for empty xmltv source")
	}
	txtFile := filepath.Join(cfg.DataDir, "secret.txt")
	_ = os.WriteFile(txtFile, []byte("secret"), 0o600)
	symM3U := filepath.Join(cfg.DataDir, "evil.m3u")
	_ = os.Symlink(txtFile, symM3U)
	if _, err := h.validateLocalM3UPath(ctx, symM3U); err == nil {
		t.Fatalf("expected symlink .m3u -> .txt to be rejected")
	}
	symXML := filepath.Join(cfg.DataDir, "evil.xml")
	_ = os.Symlink(txtFile, symXML)
	if _, err := h.validateLocalXMLTVPath(ctx, filepath.Join(cfg.DataDir, "bad.txt")); err == nil {
		t.Fatalf("expected .txt to fail validateLocalXMLTVPath")
	}
	if _, err := h.validateLocalXMLTVPath(ctx, filepath.Join(cfg.DataDir, "missing.xml")); err == nil {
		t.Fatalf("expected missing .xml to fail validateLocalXMLTVPath")
	}
	if _, err := h.validateLocalXMLTVPath(ctx, symXML); err == nil {
		t.Fatalf("expected symlink .xml -> .txt to be rejected")
	}

	// HTTP client errors for M3U and XMLTV
	h.httpClient = &http.Client{
		Transport: rtFunc(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Path, "500") {
				return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader("err"))}, nil
			}
			return nil, errors.New("network error")
		}),
	}
	if _, err := h.loadM3UFromSource(ctx, "https://93.184.216.34/\x7f", ""); err == nil {
		t.Fatalf("expected bad url error for m3u")
	}
	if _, err := h.loadM3UFromSource(ctx, "https://93.184.216.34/a.m3u", "UA"); err == nil {
		t.Fatalf("expected client error for m3u")
	}
	if _, err := h.loadXMLTVFromSource(ctx, "https://93.184.216.34/\x7f"); err == nil {
		t.Fatalf("expected bad url error for xmltv")
	}
	if _, err := h.loadXMLTVFromSource(ctx, "https://93.184.216.34/a.xml"); err == nil {
		t.Fatalf("expected client error for xmltv")
	}
	if _, err := h.loadXMLTVFromSource(ctx, "https://93.184.216.34/500.xml"); err == nil {
		t.Fatalf("expected 500 status error for xmltv")
	}

	// applyXMLTVGuideLocked matching by ID, Name, display-name -> byTvgID, and unmatched
	h.mu.Lock()
	h.applyXMLTVGuideLocked(nil) // nil guide no-op
	h.state.Channels = []ChannelInfo{
		{Id: "id-ch", Name: "Channel One", TvgId: "tvg-1"},
		{Id: "id-two", Name: "Channel Two", TvgId: "tvg-2"},
	}
	now := time.Now().UTC()
	h.applyXMLTVGuideLocked(&XMLTVGuide{
		Channels: []XMLTVChannel{
			{ID: "xml-alias", DisplayNames: []string{"", "tvg-2"}},
		},
		Programmes: []XMLTVProgramme{
			{Channel: "", Title: "EmptyCh", Start: now, Stop: now.Add(time.Hour)},
			{Channel: "id-ch", Title: "ByID", Start: now, Stop: now.Add(time.Hour)},
			{Channel: "Channel One", Title: "ByName", Start: now, Stop: now.Add(time.Hour)},
			{Channel: "xml-alias", Title: "ByDisplayNameToTvgID", Start: now, Stop: now.Add(time.Hour)},
			{Channel: "unmatched", Title: "Nobody", Start: now, Stop: now.Add(time.Hour)},
		},
	})
	if len(h.state.Programs) != 3 {
		t.Fatalf("expected 3 matched programs, got %d", len(h.state.Programs))
	}
	// All unmatched -> matchedChannelIDs == 0 -> early return
	h.applyXMLTVGuideLocked(&XMLTVGuide{
		Programmes: []XMLTVProgramme{{Channel: "nope", Title: "X", Start: now, Stop: now.Add(time.Hour)}},
	})
	h.mu.Unlock()

	// M3U parser edge cases: blank lines, trailing spaces, key= without value, scanner error (>1MB line)
	chans, err := ParseM3U(strings.NewReader("\uFEFF#EXTM3U\n\n#EXTINF:-1 tvg-id=\"1\" dangling=   ,Ch 1\nhttp://example.com/1.ts\n"))
	if err != nil || len(chans) != 1 {
		t.Fatalf("ParseM3U got %d err=%v", len(chans), err)
	}
	if _, err := ParseM3U(strings.NewReader("#EXTM3U\n" + strings.Repeat("a", 2*1024*1024))); err == nil {
		t.Fatalf("expected scanner error on oversized M3U line")
	}

	// XMLTV parser edge cases: nil reader, invalid gzip, size limit, malformed channel/programme, empty fields, bad timestamps, stop <= start
	if _, err := ParseXMLTV(nil); err == nil {
		t.Fatalf("expected error for nil xmltv reader")
	}
	if _, err := ParseXMLTV(bytes.NewReader([]byte{0x1f, 0x8b, 0x00, 0x00})); err == nil {
		t.Fatalf("expected error for corrupt gzip xmltv")
	}
	if _, err := ParseXMLTV(strings.NewReader(`<tv><broken`)); err == nil {
		t.Fatalf("expected decode error for malformed xmltv")
	}
	if _, err := parseXMLTVWithLimits(strings.NewReader(`<tv><channel id="1"><display-name>ABC</display-name></channel></tv>`), 10, 100); err == nil {
		t.Fatalf("expected size limit error")
	}
	if _, err := ParseXMLTV(strings.NewReader(`<tv><channel id="1"><bad></channel></tv>`)); err == nil {
		t.Fatalf("expected channel decode error")
	}
	if _, err := ParseXMLTV(strings.NewReader(`<tv><programme channel="1"><bad></programme></tv>`)); err == nil {
		t.Fatalf("expected programme decode error")
	}
	guide, err := ParseXMLTV(strings.NewReader(`<tv>
		<channel id=""></channel>
		<programme channel="" start="20250101120000 +0000"><title>NoCh</title></programme>
		<programme channel="1" start="bad-time"><title>BadStart</title></programme>
		<programme channel="1" start="20250101120000 +0000" stop="20250101110000 +0000"><title>DefaultStop</title></programme>
	</tv>`))
	if err != nil || len(guide.Programmes) != 1 || !guide.Programmes[0].Stop.After(guide.Programmes[0].Start) {
		t.Fatalf("unexpected guide: %+v err=%v", guide, err)
	}
	if _, err := ParseXMLTVTime(""); err == nil {
		t.Fatalf("expected error for empty xmltv time")
	}
	if _, err := ParseXMLTVTime("invalid-date"); err == nil {
		t.Fatalf("expected error for unsupported xmltv time")
	}
}
