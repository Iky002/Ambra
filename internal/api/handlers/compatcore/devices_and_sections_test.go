package compatcore

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestCompatCoreV097HardeningUsersAndSections(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	authSvc := auth.NewService(env.userRepo, env.cfg)
	authMW := middleware.NewAuth(authSvc, env.sessions)
	env.handler.SetAuthService(authSvc)

	libA, err := env.libRepo.CreateLibrary(ctx, "LibA", "movies", []string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	libB, err := env.libRepo.CreateLibrary(ctx, "LibB", "tvshows", []string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}

	s1, e1, e2 := 1, 1, 2
	rt := int64(10_000_000_000)
	movieResume := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID,
		Type: "Movie", MediaType: "Video", Name: "ResumeMovie", SortName: "resumemovie",
		RuntimeTicks: &rt,
	}
	movieFav := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID,
		Type: "Movie", MediaType: "Video", Name: "FavMovie", SortName: "favmovie",
	}
	series := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libB.ID, ParentID: libB.ID,
		Type: "Series", MediaType: "Video", Name: "Show1", SortName: "show1",
	}
	ep1 := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libB.ID, ParentID: series.ID, SeriesID: series.ID,
		Type: "Episode", MediaType: "Video", Name: "Ep1", SortName: "ep1",
		ParentIndexNumber: &s1, IndexNumber: &e1,
	}
	ep2 := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libB.ID, ParentID: series.ID, SeriesID: series.ID,
		Type: "Episode", MediaType: "Video", Name: "Ep2", SortName: "ep2",
		ParentIndexNumber: &s1, IndexNumber: &e2,
	}
	for _, it := range []*sqlite.ItemRecord{movieResume, movieFav, series, ep1, ep2} {
		if err := env.libRepo.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}

	// Mark movieResume partially played, movieFav favorite, ep1 played (so ep2 is nextup)
	_, _ = env.userRepo.UpsertPlaybackProgress(ctx, env.admin.ID, movieResume.ID, 3_000_000_000, false, nil, nil)
	_, _ = env.libRepo.MarkFavorite(ctx, env.admin.ID, movieFav.ID)
	_, _ = env.libRepo.MarkPlayed(ctx, env.admin.ID, ep1.ID)

	// Restrict user2 to libA only
	pol := sqlite.DefaultUserPolicy(false)
	pol.EnableAllFolders = false
	pol.EnabledFolders = []string{libA.ID}
	_ = env.userRepo.UpdateUserPolicy(ctx, env.user2.ID, pol)

	adminAuth, err := authSvc.AuthenticateByName(ctx, "admin", "admin", "", auth.ClientInfo{DeviceID: "admin-dev"})
	if err != nil {
		t.Fatal(err)
	}
	guestAuth, err := authSvc.AuthenticateByName(ctx, "9guest", "pass", "", auth.ClientInfo{DeviceID: "guest-dev"})
	if err != nil {
		t.Fatal(err)
	}

	router := env.handler.BuildRouter(AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
		UserIDFromCtx: func(c context.Context) string {
			if ac, ok := middleware.AuthFromContext(c); ok && ac != nil {
				return ac.UserID()
			}
			return ""
		},
	})

	doAuthReq := func(token, method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		if token != "" {
			req.Header.Set("X-Emby-Token", token)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	// 1. Cross-user IDOR blocked (403) on Sections, Suggestions, LocalTrailers, SpecialFeatures, GroupingOptions
	for _, ep := range []string{
		"/Users/" + env.admin.ID + "/Sections/latest/Items",
		"/Users/" + env.admin.ID + "/Suggestions",
		"/Users/" + env.admin.ID + "/Items/" + movieResume.ID + "/LocalTrailers",
		"/Users/" + env.admin.ID + "/Items/" + movieResume.ID + "/SpecialFeatures",
		"/Users/" + env.admin.ID + "/GroupingOptions",
	} {
		if rec := doAuthReq(guestAuth.AccessToken, http.MethodGet, ep, ""); rec.Code != http.StatusForbidden {
			t.Fatalf("IDOR GET %s expected 403, got %d", ep, rec.Code)
		}
	}

	// 2. GroupingOptions enforces EnabledFolders
	rec := doAuthReq(guestAuth.AccessToken, http.MethodGet, "/Users/"+env.user2.ID+"/GroupingOptions", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("guest GroupingOptions expected 200, got %d", rec.Code)
	}
	var groups []dto.NameIdPair
	_ = json.Unmarshal(rec.Body.Bytes(), &groups)
	if len(groups) != 1 || groups[0].Id != libA.ID {
		t.Fatalf("expected only libA in GroupingOptions for restricted user, got %+v", groups)
	}

	// 3. Sections/{SectionId}/Items respects resume, latestmedia, favorites, nextup, default
	for _, tc := range []struct {
		section string
		wantID  string
	}{
		{"resume", movieResume.ID},
		{"favorites", movieFav.ID},
		{"nextup", ep2.ID},
		{"latestmedia", movieResume.ID},
		{"custom-default", movieResume.ID},
	} {
		r := doAuthReq(adminAuth.AccessToken, http.MethodGet, "/Users/"+env.admin.ID+"/Sections/"+tc.section+"/Items?Limit=20", "")
		if r.Code != http.StatusOK {
			t.Fatalf("section %s expected 200, got %d (%s)", tc.section, r.Code, r.Body.String())
		}
		var qr dto.QueryResult[dto.BaseItemDto]
		_ = json.Unmarshal(r.Body.Bytes(), &qr)
		found := false
		for _, it := range qr.Items {
			if it.Id == tc.wantID {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("section %s expected item %s in results, got %+v", tc.section, tc.wantID, qr.Items)
		}
	}

	// 4. GET /Users/Query filters & sorting
	hiddenUser, err := authSvc.CreateUser(ctx, "charlie", "pw", false)
	if err != nil {
		t.Fatal(err)
	}
	hp := hiddenUser.Policy
	hp.IsHidden = true
	hp.IsDisabled = true
	_ = env.userRepo.UpdateUserPolicy(ctx, hiddenUser.ID, hp)

	rec = doAuthReq(adminAuth.AccessToken, http.MethodGet, "/Users/Query?IsAdministrator=true", "")
	var qRes dto.QueryResult[dto.UserDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &qRes)
	if qRes.TotalRecordCount != 1 || qRes.Items[0].Id != env.admin.ID {
		t.Fatalf("IsAdministrator=true expected only admin, got %+v", qRes)
	}

	rec = doAuthReq(adminAuth.AccessToken, http.MethodGet, "/Users/Query?IsHidden=true&IsDisabled=true&SearchTerm=harl&NameStartsWith=ch", "")
	_ = json.Unmarshal(rec.Body.Bytes(), &qRes)
	if qRes.TotalRecordCount != 1 || qRes.Items[0].Id != hiddenUser.ID {
		t.Fatalf("hidden/disabled/search filter expected charlie, got %+v", qRes)
	}

	rec = doAuthReq(adminAuth.AccessToken, http.MethodGet, "/Users/Query?SortBy=SortName&SortOrder=Descending", "")
	_ = json.Unmarshal(rec.Body.Bytes(), &qRes)
	if len(qRes.Items) < 3 || qRes.Items[0].Name != "charlie" {
		t.Fatalf("expected charlie first in descending SortName, got %+v", qRes.Items)
	}

	rec = doAuthReq(adminAuth.AccessToken, http.MethodGet, "/Users/Query?SortBy=IsAdministrator&SortOrder=Descending", "")
	_ = json.Unmarshal(rec.Body.Bytes(), &qRes)
	if len(qRes.Items) == 0 || qRes.Items[0].Id != env.admin.ID {
		t.Fatalf("expected admin first in descending IsAdministrator, got %+v", qRes.Items)
	}

	// 5. POST /Users/{Id} returns 400 on duplicate username
	rec = doAuthReq(adminAuth.AccessToken, http.MethodPost, "/Users/"+env.user2.ID, `{"Name":"admin"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on duplicate username rename, got %d (%s)", rec.Code, rec.Body.String())
	}

	// 6. POST /Users/{UserId}/CopyData honors CopyOptions (Policy + Config) and invalidates cache
	srcCfg := env.admin.Config
	srcCfg.SubtitleMode = "OnlyForced"
	_ = env.userRepo.UpdateUserConfig(ctx, env.admin.ID, srcCfg)
	// Prime cache for user2
	if _, err := authSvc.ValidateToken(ctx, guestAuth.AccessToken); err != nil {
		t.Fatal(err)
	}
	rec = doAuthReq(adminAuth.AccessToken, http.MethodPost, "/Users/"+env.admin.ID+"/CopyData",
		`{"ToUserIds":["`+env.user2.ID+`"],"CopyOptions":["UserConfiguration","UserPolicy"]}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("CopyData expected 204, got %d", rec.Code)
	}
	reloaded2, _ := env.userRepo.GetUserByID(ctx, env.user2.ID)
	if reloaded2.Config.SubtitleMode != "OnlyForced" || !reloaded2.Policy.IsAdministrator {
		t.Fatalf("CopyData did not copy config & policy: cfg=%+v pol=%+v", reloaded2.Config, reloaded2.Policy)
	}

	// 7. POST/DELETE Rating returns Likes accurately
	rec = doAuthReq(adminAuth.AccessToken, http.MethodPost, "/Users/"+env.admin.ID+"/Items/"+movieFav.ID+"/Rating?Likes=true", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("POST Rating expected 200, got %d", rec.Code)
	}
	var ratingMap map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &ratingMap)
	if ratingMap["Likes"] != true {
		t.Fatalf("expected Likes=true in rating response, got %#v", ratingMap)
	}
	rec = doAuthReq(adminAuth.AccessToken, http.MethodPost, "/Users/"+env.admin.ID+"/Items/"+movieFav.ID+"/Rating?Likes=false", "")
	ratingMap = map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &ratingMap)
	if ratingMap["Likes"] != false {
		t.Fatalf("expected Likes=false in rating response, got %#v", ratingMap)
	}
	rec = doAuthReq(adminAuth.AccessToken, http.MethodDelete, "/Users/"+env.admin.ID+"/Items/"+movieFav.ID+"/Rating", "")
	ratingMap = map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &ratingMap)
	if _, exists := ratingMap["Likes"]; exists {
		t.Fatalf("expected Likes omitted after DELETE Rating, got %#v", ratingMap)
	}

	// 8. HideFromResume does NOT zero out PlaybackPositionTicks
	rec = doAuthReq(adminAuth.AccessToken, http.MethodPost, "/Users/"+env.admin.ID+"/Items/"+movieResume.ID+"/HideFromResume?Hide=true", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("HideFromResume=true expected 200, got %d", rec.Code)
	}
	var ud dto.UserItemDataDto
	_ = json.Unmarshal(rec.Body.Bytes(), &ud)
	if ud.PlaybackPositionTicks != 3_000_000_000 {
		t.Fatalf("HideFromResume must preserve PlaybackPositionTicks=3000000000, got %d", ud.PlaybackPositionTicks)
	}
	rec = doAuthReq(adminAuth.AccessToken, http.MethodPost, "/Users/"+env.admin.ID+"/Items/"+movieResume.ID+"/HideFromResume?Hide=false", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("HideFromResume=false expected 200, got %d", rec.Code)
	}
}

func TestCompatCoreV097HardeningDevicesSessionsAndSystemOps(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	authSvc := auth.NewService(env.userRepo, env.cfg)
	authMW := middleware.NewAuth(authSvc, env.sessions)
	env.handler.SetAuthService(authSvc)

	adminAuth, _ := authSvc.AuthenticateByName(ctx, "admin", "admin", "", auth.ClientInfo{DeviceID: "admin-dev", Device: "AdminPC"})
	guestAuth, _ := authSvc.AuthenticateByName(ctx, "9guest", "pass", "", auth.ClientInfo{DeviceID: "guest-dev", Device: "GuestPhone"})

	adminSess := env.sessions.TouchSession(env.admin, auth.ClientInfo{DeviceID: "admin-dev"})
	_ = env.sessions.TouchSession(env.user2, auth.ClientInfo{DeviceID: "guest-dev"})
	_ = env.sessions.ReportPlaybackStart(ctx, env.admin, auth.ClientInfo{DeviceID: "admin-dev"}, session.PlaybackStartRequest{ItemId: env.itemID})
	_ = env.sessions.ReportPlaybackStart(ctx, env.user2, auth.ClientInfo{DeviceID: "guest-dev"}, session.PlaybackStartRequest{ItemId: env.itemID})

	router := env.handler.BuildRouter(AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
		UserIDFromCtx: func(c context.Context) string {
			if ac, ok := middleware.AuthFromContext(c); ok && ac != nil {
				return ac.UserID()
			}
			return ""
		},
	})

	doAuthReq := func(token, method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		if token != "" {
			req.Header.Set("X-Emby-Token", token)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	// 1. GET /Devices/Options & GET/POST /Devices/CameraUploads enforce canAccessDevice
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/Devices/Options?Id=admin-dev"},
		{http.MethodGet, "/Devices/CameraUploads?DeviceId=admin-dev"},
		{http.MethodPost, "/Devices/CameraUploads?DeviceId=admin-dev&Id=f1&Name=a.jpg"},
	} {
		if rec := doAuthReq(guestAuth.AccessToken, tc.method, tc.path, ""); rec.Code != http.StatusForbidden {
			t.Fatalf("guest %s %s expected 403, got %d", tc.method, tc.path, rec.Code)
		}
	}

	// 2. GET /Devices/Info surfaces CustomName and returns 404 for unknown device
	_ = doAuthReq(adminAuth.AccessToken, http.MethodPost, "/Devices/Options?Id=admin-dev", `{"CustomName":"Studio Mac"}`)
	rec := doAuthReq(adminAuth.AccessToken, http.MethodGet, "/Devices/Info?Id=admin-dev", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"CustomName":"Studio Mac"`) {
		t.Fatalf("expected CustomName in Devices/Info, got %d %s", rec.Code, rec.Body.String())
	}
	rec = doAuthReq(adminAuth.AccessToken, http.MethodGet, "/Devices/Info?Id=nonexistent-dev", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown device, got %d %s", rec.Code, rec.Body.String())
	}

	// 3. GET /Sessions/PlayQueue respects Id, StartIndex, Limit
	rec = doAuthReq(adminAuth.AccessToken, http.MethodGet, "/Sessions/PlayQueue?Id="+adminSess.Id, "")
	var pq dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &pq)
	if pq.TotalRecordCount != 1 || len(pq.Items) != 1 {
		t.Fatalf("PlayQueue filtered by Id expected 1 item, got %+v", pq)
	}
	rec = doAuthReq(adminAuth.AccessToken, http.MethodGet, "/Sessions/PlayQueue?StartIndex=1&Limit=1", "")
	_ = json.Unmarshal(rec.Body.Bytes(), &pq)
	if pq.TotalRecordCount != 2 || len(pq.Items) != 1 {
		t.Fatalf("PlayQueue paginated expected total=2 len=1, got total=%d len=%d", pq.TotalRecordCount, len(pq.Items))
	}

	// 4. Lifecycle hooks on /System/Restart and /System/Shutdown
	restartCh := make(chan struct{}, 1)
	shutdownCh := make(chan struct{}, 1)
	env.handler.SetLifecycleHooks(
		func() { restartCh <- struct{}{} },
		func() { shutdownCh <- struct{}{} },
	)
	if rec := doAuthReq(adminAuth.AccessToken, http.MethodPost, "/System/Restart", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("Restart expected 204, got %d", rec.Code)
	}
	select {
	case <-restartCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for onRestart hook")
	}
	if rec := doAuthReq(adminAuth.AccessToken, http.MethodPost, "/System/Shutdown", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("Shutdown expected 204, got %d", rec.Code)
	}
	select {
	case <-shutdownCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for onShutdown hook")
	}

	// 5. TaskStopper wired on /ScheduledTasks/Running/{Id}/Delete
	var stoppedID atomic.Value
	env.handler.SetTaskStopper(func(id string) bool {
		stoppedID.Store(id)
		return true
	})
	if rec := doAuthReq(adminAuth.AccessToken, http.MethodPost, "/ScheduledTasks/Running/scan-123/Delete", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("stop task expected 204, got %d", rec.Code)
	}
	if got, _ := stoppedID.Load().(string); got != "scan-123" {
		t.Fatalf("expected taskStopper called with scan-123, got %q", got)
	}

	// 6. GET /System/Info/Storage populates real FreeSpace / UsedSpace
	rec = doAuthReq(adminAuth.AccessToken, http.MethodGet, "/System/Info/Storage", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("Storage info expected 200, got %d", rec.Code)
	}
	var storage map[string]map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &storage); err != nil {
		t.Fatal(err)
	}
	pd := storage["ProgramDataFolder"]
	if _, hasFree := pd["FreeSpace"]; !hasFree {
		t.Fatalf("expected FreeSpace in ProgramDataFolder, got %#v", pd)
	}
	if _, hasUsed := pd["UsedSpace"]; !hasUsed {
		t.Fatalf("expected UsedSpace in ProgramDataFolder, got %#v", pd)
	}
}
