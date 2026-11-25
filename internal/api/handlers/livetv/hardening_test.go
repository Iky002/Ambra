package livetv

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func doLiveTVAuthReq(t *testing.T, mux *http.ServeMux, ac *middleware.AuthContext, method, target string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, target, &buf)
	req.Header.Set("Content-Type", "application/json")
	if ac != nil {
		req = req.WithContext(middleware.WithAuthContext(req.Context(), ac))
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func makeLiveTVAuth(id, name string, isAdmin, manageLiveTV bool) *middleware.AuthContext {
	pol := sqlite.DefaultUserPolicy(isAdmin)
	pol.EnableLiveTvManagement = manageLiveTV
	return &middleware.AuthContext{
		User: &sqlite.UserRecord{
			ID:      guid.Normalize(id),
			Name:    name,
			IsAdmin: isAdmin,
			Policy:  pol,
		},
	}
}

func TestLiveTVTimerRBAC(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default(dir)
	st, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	h := New(st, cfg)
	mux := http.NewServeMux()
	authMW := middleware.NewAuth(nil, nil)
	h.RegisterRoutes(mux, AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
	})

	viewer := makeLiveTVAuth("viewer-1", "viewer", false, false)
	dvrMgr := makeLiveTVAuth("dvrmgr-1", "dvrmgr", false, true)
	admin := makeLiveTVAuth("admin-1", "admin", true, true)

	// Viewer without EnableLiveTvManagement is blocked from creating timers / series timers.
	if r := doLiveTVAuthReq(t, mux, viewer, http.MethodPost, "/LiveTv/Timers", map[string]any{"Name": "T1"}); r.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for viewer POST /LiveTv/Timers, got %d", r.Code)
	}
	if r := doLiveTVAuthReq(t, mux, viewer, http.MethodPost, "/LiveTv/SeriesTimers", map[string]any{"Name": "S1"}); r.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for viewer POST /LiveTv/SeriesTimers, got %d", r.Code)
	}

	// User with EnableLiveTvManagement=true can create & update & delete timers.
	rec := doLiveTVAuthReq(t, mux, dvrMgr, http.MethodPost, "/LiveTv/Timers", map[string]any{"Name": "T1"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for dvrMgr POST /LiveTv/Timers, got %d", rec.Code)
	}
	var timer TimerInfo
	_ = json.Unmarshal(rec.Body.Bytes(), &timer)

	// Viewer still cannot update or delete the timer.
	if r := doLiveTVAuthReq(t, mux, viewer, http.MethodPost, "/LiveTv/Timers/"+timer.Id, map[string]any{"Name": "Hacked"}); r.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for viewer POST /LiveTv/Timers/{Id}, got %d", r.Code)
	}
	if r := doLiveTVAuthReq(t, mux, viewer, http.MethodDelete, "/LiveTv/Timers/"+timer.Id, nil); r.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for viewer DELETE /LiveTv/Timers/{Id}, got %d", r.Code)
	}

	// Admin can delete the timer.
	if r := doLiveTVAuthReq(t, mux, admin, http.MethodDelete, "/LiveTv/Timers/"+timer.Id, nil); r.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for admin DELETE /LiveTv/Timers/{Id}, got %d", r.Code)
	}

	// SeriesTimer RBAC: dvrMgr creates, viewer blocked from update/delete, dvrMgr deletes.
	rec = doLiveTVAuthReq(t, mux, dvrMgr, http.MethodPost, "/LiveTv/SeriesTimers", map[string]any{"Name": "S1"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for dvrMgr POST /LiveTv/SeriesTimers, got %d", rec.Code)
	}
	var stTimer SeriesTimerInfo
	_ = json.Unmarshal(rec.Body.Bytes(), &stTimer)
	if r := doLiveTVAuthReq(t, mux, viewer, http.MethodDelete, "/LiveTv/SeriesTimers/"+stTimer.Id, nil); r.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for viewer DELETE /LiveTv/SeriesTimers/{Id}, got %d", r.Code)
	}
	if r := doLiveTVAuthReq(t, mux, dvrMgr, http.MethodDelete, "/LiveTv/SeriesTimers/"+stTimer.Id, nil); r.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for dvrMgr DELETE /LiveTv/SeriesTimers/{Id}, got %d", r.Code)
	}
}

func TestLiveTVInfoEnabledUsers(t *testing.T) {
	h, mux, st, _ := setupLiveTVTest(t)
	userRepo := sqlite.NewUserRepo(st)
	h.SetUserRepo(userRepo)

	ctx := context.Background()
	admin := &sqlite.UserRecord{Name: "admin", IsAdmin: true, Policy: sqlite.DefaultUserPolicy(true), Config: sqlite.DefaultUserConfig()}
	tvUser := &sqlite.UserRecord{Name: "tvuser", IsAdmin: false, Policy: sqlite.DefaultUserPolicy(false), Config: sqlite.DefaultUserConfig()}
	noTVPol := sqlite.DefaultUserPolicy(false)
	noTVPol.EnableLiveTvAccess = false
	noTVUser := &sqlite.UserRecord{Name: "notv", IsAdmin: false, Policy: noTVPol, Config: sqlite.DefaultUserConfig()}
	disabledUser := &sqlite.UserRecord{Name: "disabled", IsDisabled: true, Policy: sqlite.DefaultUserPolicy(false), Config: sqlite.DefaultUserConfig()}

	for _, u := range []*sqlite.UserRecord{admin, tvUser, noTVUser, disabledUser} {
		if err := userRepo.CreateUser(ctx, u); err != nil {
			t.Fatalf("CreateUser %s: %v", u.Name, err)
		}
	}

	rec := doJSON(t, mux, http.MethodGet, "/LiveTv/Info", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /LiveTv/Info status=%d", rec.Code)
	}
	var info LiveTvInfo
	_ = json.Unmarshal(rec.Body.Bytes(), &info)
	if len(info.EnabledUsers) != 2 {
		t.Fatalf("expected 2 enabled users (admin + tvuser), got %+v", info.EnabledUsers)
	}
}

func TestLiveTVProgramsFilteringSortingAndSeriesTimerAutoSchedule(t *testing.T) {
	h, mux, _, _ := setupLiveTVTest(t)
	now := time.Now().UTC().Truncate(time.Minute)

	h.mu.Lock()
	h.state.Programs = []ProgramInfo{
		{
			Id:          "prog-past",
			Name:        "Morning News",
			Type:        "Program",
			ChannelId:   "ch-1",
			ChannelName: "News 1",
			SeriesId:    "series-news",
			StartDate:   now.Add(-3 * time.Hour),
			EndDate:     now.Add(-2 * time.Hour),
			IsSeries:    true,
		},
		{
			Id:          "prog-now",
			Name:        "Evening News",
			Type:        "Program",
			ChannelId:   "ch-1",
			ChannelName: "News 1",
			SeriesId:    "series-news",
			StartDate:   now.Add(-15 * time.Minute),
			EndDate:     now.Add(45 * time.Minute),
			IsSeries:    true,
		},
		{
			Id:          "prog-future",
			Name:        "Evening News",
			Type:        "Program",
			ChannelId:   "ch-1",
			ChannelName: "News 1",
			SeriesId:    "series-news",
			StartDate:   now.Add(2 * time.Hour),
			EndDate:     now.Add(3 * time.Hour),
			IsSeries:    true,
		},
		{
			Id:          "prog-other-ch",
			Name:        "Evening News",
			Type:        "Program",
			ChannelId:   "ch-2",
			ChannelName: "News 2",
			SeriesId:    "series-news",
			StartDate:   now.Add(4 * time.Hour),
			EndDate:     now.Add(5 * time.Hour),
			IsSeries:    true,
		},
	}
	h.mu.Unlock()

	// Filter HasAired=true
	rec := doJSON(t, mux, http.MethodGet, "/LiveTv/Programs?HasAired=true", nil)
	var res dto.QueryResult[ProgramInfo]
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if res.TotalRecordCount != 1 || res.Items[0].Id != "prog-past" {
		t.Fatalf("HasAired=true unexpected: %+v", res)
	}

	// Filter IsAiring=true
	rec = doJSON(t, mux, http.MethodGet, "/LiveTv/Programs?IsAiring=true", nil)
	res = dto.QueryResult[ProgramInfo]{}
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if res.TotalRecordCount != 1 || res.Items[0].Id != "prog-now" {
		t.Fatalf("IsAiring=true unexpected: %+v", res)
	}

	// POST /LiveTv/Programs with MinStartDate, MaxEndDate, SortBy=StartDate, SortOrder=Descending
	rec = doJSON(t, mux, http.MethodPost, "/LiveTv/Programs", map[string]any{
		"MinStartDate": now.Add(-30 * time.Minute).Format(time.RFC3339),
		"MaxEndDate":   now.Add(4 * time.Hour).Format(time.RFC3339),
		"SortBy":       "StartDate",
		"SortOrder":    "Descending",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /LiveTv/Programs status=%d", rec.Code)
	}
	res = dto.QueryResult[ProgramInfo]{}
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if res.TotalRecordCount != 2 || res.Items[0].Id != "prog-future" || res.Items[1].Id != "prog-now" {
		t.Fatalf("POST filtered/sorted unexpected: %+v", res)
	}

	// POST /LiveTv/SeriesTimers auto-schedules matching non-ended EPG programs on ch-1 (prog-now + prog-future, NOT prog-past, NOT prog-other-ch).
	rec = doJSON(t, mux, http.MethodPost, "/LiveTv/SeriesTimers", map[string]any{
		"Name":             "Evening News",
		"ChannelId":        "ch-1",
		"RecordAnyChannel": false,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /LiveTv/SeriesTimers status=%d", rec.Code)
	}
	var stTimer SeriesTimerInfo
	_ = json.Unmarshal(rec.Body.Bytes(), &stTimer)

	rec = doJSON(t, mux, http.MethodGet, "/LiveTv/Timers?SeriesTimerId="+stTimer.Id, nil)
	var timers dto.QueryResult[TimerInfo]
	_ = json.Unmarshal(rec.Body.Bytes(), &timers)
	if timers.TotalRecordCount != 2 {
		t.Fatalf("expected 2 auto-scheduled timers, got %d (%+v)", timers.TotalRecordCount, timers.Items)
	}

	// Updating the same series timer with RecordAnyChannel=true adds prog-other-ch without duplicating prog-now/prog-future.
	rec = doJSON(t, mux, http.MethodPost, "/LiveTv/SeriesTimers/"+stTimer.Id, map[string]any{
		"Name":             "Evening News",
		"SeriesId":         "series-news",
		"RecordAnyChannel": true,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /LiveTv/SeriesTimers/{Id} status=%d", rec.Code)
	}
	rec = doJSON(t, mux, http.MethodGet, "/LiveTv/Timers?SeriesTimerId="+stTimer.Id, nil)
	timers = dto.QueryResult[TimerInfo]{}
	_ = json.Unmarshal(rec.Body.Bytes(), &timers)
	if timers.TotalRecordCount != 3 {
		t.Fatalf("expected 3 deduplicated timers across channels, got %d", timers.TotalRecordCount)
	}
}

func TestLiveTVAccessPolicyEnforcement(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default(dir)
	st, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	userRepo := sqlite.NewUserRepo(st)
	h := New(st, cfg)
	h.SetUserRepo(userRepo)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp2t")
		_, _ = w.Write([]byte("OK_STREAM"))
	}))
	defer upstream.Close()

	h.mu.Lock()
	h.state.Channels = []ChannelInfo{{Id: "ch-1", Name: "Chan 1", Type: "TvChannel", ChannelType: "TV", StreamUrl: upstream.URL + "/live.ts"}}
	h.mu.Unlock()

	mux := http.NewServeMux()
	authMW := middleware.NewAuth(nil, nil)
	h.RegisterRoutes(mux, AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
	})

	noAccessPol := sqlite.DefaultUserPolicy(false)
	noAccessPol.EnableLiveTvAccess = false
	noAccessUser := &middleware.AuthContext{
		User: &sqlite.UserRecord{
			ID:      guid.Normalize("no-tv-1"),
			Name:    "notv",
			IsAdmin: false,
			Policy:  noAccessPol,
		},
	}

	okUser := makeLiveTVAuth("tv-ok-1", "tvok", false, false)

	adminNoFlagPol := sqlite.DefaultUserPolicy(true)
	adminNoFlagPol.EnableLiveTvAccess = false
	adminUser := &middleware.AuthContext{
		User: &sqlite.UserRecord{
			ID:      guid.Normalize("admin-tv-1"),
			Name:    "admin",
			IsAdmin: true,
			Policy:  adminNoFlagPol,
		},
	}

	endpoints := []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/LiveTv/Channels", nil},
		{http.MethodGet, "/LiveTv/Channels/ch-1", nil},
		{http.MethodGet, "/LiveTv/Channels/ch-1/Stream", nil},
		{http.MethodGet, "/LiveTv/Programs", nil},
		{http.MethodPost, "/LiveTv/Programs", map[string]any{}},
		{http.MethodGet, "/LiveTv/Recordings", nil},
		{http.MethodGet, "/LiveTv/Timers", nil},
		{http.MethodGet, "/LiveTv/SeriesTimers", nil},
	}

	for _, ep := range endpoints {
		if r := doLiveTVAuthReq(t, mux, noAccessUser, ep.method, ep.path, ep.body); r.Code != http.StatusForbidden {
			t.Fatalf("noAccessUser %s %s: expected 403, got %d", ep.method, ep.path, r.Code)
		}
		if r := doLiveTVAuthReq(t, mux, okUser, ep.method, ep.path, ep.body); r.Code != http.StatusOK {
			t.Fatalf("okUser %s %s: expected 200, got %d body=%s", ep.method, ep.path, r.Code, r.Body.String())
		}
		if r := doLiveTVAuthReq(t, mux, adminUser, ep.method, ep.path, ep.body); r.Code != http.StatusOK {
			t.Fatalf("adminUser %s %s: expected 200, got %d body=%s", ep.method, ep.path, r.Code, r.Body.String())
		}
	}
}

func TestLiveTVSeriesTimerCascadeXMLTVRefreshAndExportedHelpers(t *testing.T) {
	h, mux, _, _ := setupLiveTVTest(t)

	futureStart := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Minute)
	futureEnd := futureStart.Add(time.Hour)
	xmltvPayload := `<?xml version="1.0" encoding="UTF-8"?>
<tv>
  <channel id="epg-1"><display-name>News 1</display-name></channel>
  <programme start="` + futureStart.Format("20060102150405 -0700") + `" stop="` + futureEnd.Format("20060102150405 -0700") + `" channel="epg-1">
    <title>Nightly Brief</title>
    <desc>Daily news recap</desc>
  </programme>
</tv>`

	lanSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/playlist.m3u":
			_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:-1 tvg-id=\"epg-1\",News 1\nhttp://192.168.1.50/news.ts\n"))
		case "/guide.xml":
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(xmltvPayload))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer lanSrv.Close()

	// 1. LAN M3U import succeeds with default safe HTTP client
	rec := doJSON(t, mux, http.MethodPost, "/LiveTv/TunerHosts", map[string]any{
		"Type": "m3u",
		"Url":  lanSrv.URL + "/playlist.m3u",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("LAN M3U import failed: %d %s", rec.Code, rec.Body.String())
	}

	chRec := doJSON(t, mux, http.MethodGet, "/LiveTv/Channels", nil)
	var chRes dto.QueryResult[ChannelInfo]
	_ = json.Unmarshal(chRec.Body.Bytes(), &chRes)
	if len(chRes.Items) != 1 {
		t.Fatalf("expected 1 channel, got %d", len(chRes.Items))
	}
	chID := chRes.Items[0].Id

	// 2. Create SeriesTimer BEFORE XMLTV refresh
	stRec := doJSON(t, mux, http.MethodPost, "/LiveTv/SeriesTimers", map[string]any{
		"Name":             "Nightly Brief",
		"ChannelId":        chID,
		"RecordAnyChannel": false,
	})
	if stRec.Code != http.StatusOK {
		t.Fatalf("POST /LiveTv/SeriesTimers: %d", stRec.Code)
	}
	var stTimer SeriesTimerInfo
	_ = json.Unmarshal(stRec.Body.Bytes(), &stTimer)

	// 3. Refresh XMLTV from LAN URL -> must automatically schedule pending timer for SeriesTimer
	lpRec := doJSON(t, mux, http.MethodPost, "/LiveTv/ListingProviders", map[string]any{
		"Type": "xmltv",
		"Path": lanSrv.URL + "/guide.xml",
	})
	if lpRec.Code != http.StatusOK {
		t.Fatalf("LAN XMLTV import failed: %d %s", lpRec.Code, lpRec.Body.String())
	}

	// Add a completed timer belonging to the same series timer + an unrelated timer to verify selective cascade.
	h.mu.Lock()
	h.state.Timers = append(h.state.Timers,
		TimerInfo{Id: "tm-done", SeriesTimerId: stTimer.Id, Name: "Old Ep", Status: "Completed"},
		TimerInfo{Id: "tm-other", Name: "Standalone", Status: "New"},
	)
	h.mu.Unlock()

	tRec := doJSON(t, mux, http.MethodGet, "/LiveTv/Timers?SeriesTimerId="+stTimer.Id, nil)
	var timers dto.QueryResult[TimerInfo]
	_ = json.Unmarshal(tRec.Body.Bytes(), &timers)
	if timers.TotalRecordCount != 2 {
		t.Fatalf("expected 2 timers for series (1 auto-scheduled New + 1 Completed), got %d (%+v)", timers.TotalRecordCount, timers.Items)
	}

	// 4. DELETE /LiveTv/SeriesTimers/{Id} cascade-deletes pending ("New") single timers, keeping Completed & unrelated timers.
	delRec := doJSON(t, mux, http.MethodDelete, "/LiveTv/SeriesTimers/"+stTimer.Id, nil)
	if delRec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /LiveTv/SeriesTimers/{Id}: %d", delRec.Code)
	}
	tRec = doJSON(t, mux, http.MethodGet, "/LiveTv/Timers", nil)
	timers = dto.QueryResult[TimerInfo]{}
	_ = json.Unmarshal(tRec.Body.Bytes(), &timers)
	if timers.TotalRecordCount != 2 {
		t.Fatalf("expected 2 remaining timers (tm-done + tm-other), got %d (%+v)", timers.TotalRecordCount, timers.Items)
	}
	for _, tm := range timers.Items {
		if tm.SeriesTimerId == stTimer.Id && tm.Status == "New" {
			t.Fatalf("pending timer %s was not cascade-deleted", tm.Id)
		}
	}

	// 5. Exported channel state helpers update in-memory state cleanly
	if err := h.SetChannelDisabled(context.Background(), chID, true); err != nil {
		t.Fatalf("SetChannelDisabled: %v", err)
	}
	if err := h.SetChannelSortIndex(context.Background(), chID, 7); err != nil {
		t.Fatalf("SetChannelSortIndex: %v", err)
	}
	ch, ok := h.GetChannelByID(chID)
	if !ok || !ch.Disabled || ch.SortIndex != 7 {
		t.Fatalf("expected Disabled=true SortIndex=7, got %+v", ch)
	}
}
