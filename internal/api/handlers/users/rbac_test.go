package users

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestSessionsDevicesAndDisplayPreferencesRBAC(t *testing.T) {
	tmp := t.TempDir()
	cfg, err := config.Load(filepath.Join(tmp, "data"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	userRepo := sqlite.NewUserRepo(store)
	authSvc := auth.NewService(userRepo, cfg)
	sessMgr := session.NewManager(userRepo, cfg, eventbus.New())
	authMW := middleware.NewAuth(authSvc, sessMgr)

	admin, err := authSvc.CreateUser(ctx, "admin", "admin123", true)
	if err != nil {
		t.Fatal(err)
	}
	guest, err := authSvc.CreateUser(ctx, "guest", "guest123", false)
	if err != nil {
		t.Fatal(err)
	}

	adminAuth, err := authSvc.AuthenticateByName(ctx, "admin", "admin123", "", auth.ClientInfo{DeviceID: "dev-admin", Device: "AdminPC", Client: "Web"})
	if err != nil {
		t.Fatal(err)
	}
	guestAuth, err := authSvc.AuthenticateByName(ctx, "guest", "guest123", "", auth.ClientInfo{DeviceID: "dev-guest", Device: "GuestPhone", Client: "Web"})
	if err != nil {
		t.Fatal(err)
	}

	sessMgr.TouchSession(admin, auth.ClientInfo{DeviceID: "dev-admin", Device: "AdminPC", Client: "Web"})
	sessMgr.TouchSession(guest, auth.ClientInfo{DeviceID: "dev-guest", Device: "GuestPhone", Client: "Web"})

	r := chi.NewRouter()
	r.Use(authMW.ParseAuth)
	RegisterRoutes(r, authMW, NewHandler(authSvc, userRepo, sessMgr, nil, cfg))

	doReq := func(token, method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("X-Emby-Token", token)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	// 1. GET /Sessions: non-admin sees only own session; admin sees all
	rec := doReq(guestAuth.AccessToken, http.MethodGet, "/Sessions", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("guest GET /Sessions status=%d", rec.Code)
	}
	var guestSessions []dto.SessionInfo
	_ = json.Unmarshal(rec.Body.Bytes(), &guestSessions)
	if len(guestSessions) != 1 || guestSessions[0].UserId != guest.ID {
		t.Fatalf("guest should only see own session, got %+v", guestSessions)
	}

	rec = doReq(adminAuth.AccessToken, http.MethodGet, "/Sessions", "")
	var adminSessions []dto.SessionInfo
	_ = json.Unmarshal(rec.Body.Bytes(), &adminSessions)
	if len(adminSessions) < 2 {
		t.Fatalf("admin should see all sessions, got %d", len(adminSessions))
	}

	// 2. GET /Devices: non-admin sees only own devices; admin sees all
	rec = doReq(guestAuth.AccessToken, http.MethodGet, "/Devices", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("guest GET /Devices status=%d", rec.Code)
	}
	var guestDevices dto.QueryResult[DeviceInfoDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &guestDevices)
	if guestDevices.TotalRecordCount != 1 || guestDevices.Items[0].LastUserId != guest.ID {
		t.Fatalf("guest should only see own device, got %+v", guestDevices)
	}

	rec = doReq(adminAuth.AccessToken, http.MethodGet, "/Devices", "")
	var adminDevices dto.QueryResult[DeviceInfoDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &adminDevices)
	if adminDevices.TotalRecordCount < 2 {
		t.Fatalf("admin should see all devices, got %d", adminDevices.TotalRecordCount)
	}

	// 3. GET /DisplayPreferences/{Id}: non-admin cannot read another user's prefs -> 403
	if rec := doReq(guestAuth.AccessToken, http.MethodGet, "/DisplayPreferences/usersettings?userId="+admin.ID, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin reading admin DisplayPreferences, got %d", rec.Code)
	}
	// Self read -> 200
	if rec := doReq(guestAuth.AccessToken, http.MethodGet, "/DisplayPreferences/usersettings?userId="+guest.ID, ""); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for self DisplayPreferences read, got %d", rec.Code)
	}
	// Admin reading guest -> 200
	if rec := doReq(adminAuth.AccessToken, http.MethodGet, "/DisplayPreferences/usersettings?userId="+guest.ID, ""); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for admin DisplayPreferences read, got %d", rec.Code)
	}

	// 4. POST /DisplayPreferences/{Id}: non-admin cannot overwrite another user's prefs -> 403
	if rec := doReq(guestAuth.AccessToken, http.MethodPost, "/DisplayPreferences/usersettings?userId="+admin.ID, `{"SortBy":"DateCreated"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin overwriting admin DisplayPreferences, got %d", rec.Code)
	}
	// Self write -> 204
	if rec := doReq(guestAuth.AccessToken, http.MethodPost, "/DisplayPreferences/usersettings?userId="+guest.ID, `{"SortBy":"DateCreated"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for self DisplayPreferences write, got %d", rec.Code)
	}
	// Admin writing for guest -> 204
	if rec := doReq(adminAuth.AccessToken, http.MethodPost, "/DisplayPreferences/usersettings?userId="+guest.ID, `{"SortBy":"Name"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for admin DisplayPreferences write, got %d", rec.Code)
	}
}

func TestUserAuditActivityLogs(t *testing.T) {
	tmp := t.TempDir()
	cfg, err := config.Load(filepath.Join(tmp, "data"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	userRepo := sqlite.NewUserRepo(store)
	authSvc := auth.NewService(userRepo, cfg)
	sessMgr := session.NewManager(userRepo, cfg, eventbus.New())
	authMW := middleware.NewAuth(authSvc, sessMgr)

	if _, err := authSvc.CreateUser(ctx, "admin", "admin123", true); err != nil {
		t.Fatal(err)
	}
	adminAuth, err := authSvc.AuthenticateByName(ctx, "admin", "admin123", "", auth.ClientInfo{DeviceID: "dev-admin"})
	if err != nil {
		t.Fatal(err)
	}

	r := chi.NewRouter()
	r.Use(authMW.ParseAuth)
	RegisterRoutes(r, authMW, NewHandler(authSvc, userRepo, sessMgr, nil, cfg))

	doReq := func(token, method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		if token != "" {
			req.Header.Set("X-Emby-Token", token)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	// 1. Failed login -> AuthenticationFailed
	if rec := doReq("", http.MethodPost, "/Users/AuthenticateByName", `{"Username":"admin","Pw":"wrong"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong password, got %d", rec.Code)
	}

	// 2. Create user -> UserCreated
	createRec := doReq(adminAuth.AccessToken, http.MethodPost, "/Users/New", `{"Name":"audituser","Password":"pw1","IsAdministrator":false}`)
	if createRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for CreateUser, got %d", createRec.Code)
	}
	var created dto.UserDto
	_ = json.Unmarshal(createRec.Body.Bytes(), &created)

	// 3. Change password -> UserPasswordChanged
	if rec := doReq(adminAuth.AccessToken, http.MethodPost, "/Users/"+created.Id+"/Password", `{"NewPw":"pw2"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for UpdateUserPassword, got %d", rec.Code)
	}

	// 4. Disable user and attempt login -> AuthenticationFailed (disabled)
	disabledPol := sqlite.DefaultUserPolicy(false)
	disabledPol.IsDisabled = true
	_ = userRepo.UpdateUserPolicy(ctx, created.Id, disabledPol)
	authSvc.InvalidateUserCache(created.Id)
	if rec := doReq("", http.MethodPost, "/Users/AuthenticateByName", `{"Username":"audituser","Pw":"pw2"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for disabled user login, got %d", rec.Code)
	}

	// 5. Delete user -> UserDeleted
	if rec := doReq(adminAuth.AccessToken, http.MethodDelete, "/Users/"+created.Id, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for DeleteUser, got %d", rec.Code)
	}

	logs, _, err := userRepo.ListActivityLogs(ctx, 0, 50, nil)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for _, l := range logs {
		seen[l.Type]++
	}
	for _, want := range []string{"AuthenticationFailed", "UserCreated", "UserPasswordChanged", "UserDeleted"} {
		if seen[want] == 0 {
			t.Errorf("expected activity log event %q, got counts %+v", want, seen)
		}
	}
	if seen["AuthenticationFailed"] < 2 {
		t.Errorf("expected at least 2 AuthenticationFailed events, got %d", seen["AuthenticationFailed"])
	}
}

func TestSessionsActiveWithinAndDevicesQueryFilters(t *testing.T) {
	tmp := t.TempDir()
	cfg, err := config.Load(filepath.Join(tmp, "data"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	userRepo := sqlite.NewUserRepo(store)
	authSvc := auth.NewService(userRepo, cfg)
	sessMgr := session.NewManager(userRepo, cfg, eventbus.New())
	authMW := middleware.NewAuth(authSvc, sessMgr)

	admin, err := authSvc.CreateUser(ctx, "admin", "admin123", true)
	if err != nil {
		t.Fatal(err)
	}
	guest, err := authSvc.CreateUser(ctx, "guest", "guest123", false)
	if err != nil {
		t.Fatal(err)
	}

	adminAuth, err := authSvc.AuthenticateByName(ctx, "admin", "admin123", "", auth.ClientInfo{DeviceID: "dev-admin", Device: "AdminPC", Client: "Web"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = authSvc.AuthenticateByName(ctx, "guest", "guest123", "", auth.ClientInfo{DeviceID: "dev-guest", Device: "GuestPhone", Client: "Web"})
	if err != nil {
		t.Fatal(err)
	}
	sessMgr.TouchSession(admin, auth.ClientInfo{DeviceID: "dev-admin", Device: "AdminPC", Client: "Web"})
	sessMgr.TouchSession(guest, auth.ClientInfo{DeviceID: "dev-guest", Device: "GuestPhone", Client: "Web"})

	r := chi.NewRouter()
	r.Use(authMW.ParseAuth)
	RegisterRoutes(r, authMW, NewHandler(authSvc, userRepo, sessMgr, nil, cfg))

	doReq := func(target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.Header.Set("X-Emby-Token", adminAuth.AccessToken)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	// ActiveWithinSeconds=60 includes recently active sessions
	rec := doReq("/Sessions?ActiveWithinSeconds=60")
	if rec.Code != http.StatusOK {
		t.Fatalf("ActiveWithinSeconds=60 status=%d", rec.Code)
	}
	var active []dto.SessionInfo
	_ = json.Unmarshal(rec.Body.Bytes(), &active)
	if len(active) < 2 {
		t.Fatalf("expected >=2 active sessions within 60s, got %d", len(active))
	}

	// GET /Devices?UserId=<guest> returns only guest's device
	rec = doReq("/Devices?UserId=" + guest.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /Devices?UserId status=%d", rec.Code)
	}
	var devByUser dto.QueryResult[DeviceInfoDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &devByUser)
	if devByUser.TotalRecordCount != 1 || devByUser.Items[0].LastUserId != guest.ID {
		t.Fatalf("expected 1 device for guest, got %+v", devByUser)
	}

	// GET /Devices?SupportsSync=true returns 0 devices (sync not supported)
	rec = doReq("/Devices?SupportsSync=true")
	var devSyncTrue dto.QueryResult[DeviceInfoDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &devSyncTrue)
	if devSyncTrue.TotalRecordCount != 0 {
		t.Fatalf("expected 0 sync devices, got %d", devSyncTrue.TotalRecordCount)
	}

	// GET /Devices?SupportsSync=false returns all devices
	rec = doReq("/Devices?SupportsSync=false")
	var devSyncFalse dto.QueryResult[DeviceInfoDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &devSyncFalse)
	if devSyncFalse.TotalRecordCount < 2 {
		t.Fatalf("expected >=2 devices for SupportsSync=false, got %d", devSyncFalse.TotalRecordCount)
	}
}
