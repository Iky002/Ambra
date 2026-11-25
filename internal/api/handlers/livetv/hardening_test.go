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
