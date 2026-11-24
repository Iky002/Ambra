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
