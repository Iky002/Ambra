package users

import (
	"bytes"
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
	"github.com/ambra-media/ambra/internal/api/ws"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

type usersTestEnv struct {
	router     chi.Router
	handler    *Handler
	store      *sqlite.Store
	userRepo   *sqlite.UserRepo
	authSvc    *auth.Service
	sessionMgr *session.Manager
	admin      *sqlite.UserRecord
	viewer     *sqlite.UserRecord
	adminTok   string
	viewerTok  string
}

func setupUsersEnv(t *testing.T) *usersTestEnv {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default(dir)
	store, err := sqlite.Open(filepath.Join(dir, "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	userRepo := sqlite.NewUserRepo(store)
	authSvc := auth.NewService(userRepo, cfg)
	bus := eventbus.New()
	sessMgr := session.NewManager(userRepo, cfg, bus)
	wsHub := ws.NewHub(authSvc, sessMgr, bus)

	ctx := context.Background()
	admin, err := authSvc.CreateUser(ctx, "admin", "adminpw", true)
	if err != nil {
		t.Fatal(err)
	}
	viewer, err := authSvc.CreateUser(ctx, "viewer", "viewerpw", false)
	if err != nil {
		t.Fatal(err)
	}
	adminRes, err := authSvc.AuthenticateByName(ctx, "admin", "adminpw", "", auth.ClientInfo{Client: "Web", Device: "Mac", DeviceID: "dev-admin", Version: "1.0"})
	if err != nil {
		t.Fatal(err)
	}
	viewerRes, err := authSvc.AuthenticateByName(ctx, "viewer", "viewerpw", "", auth.ClientInfo{Client: "Infuse", Device: "ATV", DeviceID: "dev-viewer", Version: "8.0"})
	if err != nil {
		t.Fatal(err)
	}

	h := NewHandler(authSvc, userRepo, sessMgr, wsHub, cfg)
	r := chi.NewRouter()
	mw := middleware.NewAuth(authSvc, sessMgr)
	RegisterRoutes(r, mw, h)

	return &usersTestEnv{
		router:     r,
		handler:    h,
		store:      store,
		userRepo:   userRepo,
		authSvc:    authSvc,
		sessionMgr: sessMgr,
		admin:      admin,
		viewer:     viewer,
		adminTok:   adminRes.AccessToken,
		viewerTok:  viewerRes.AccessToken,
	}
}

func doReq(r http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	var bodyReader *bytes.Buffer
	if body != "" {
		bodyReader = bytes.NewBufferString(body)
	} else {
		bodyReader = bytes.NewBuffer(nil)
	}
	req := httptest.NewRequest(method, path, bodyReader)
	if token != "" {
		req.Header.Set("X-Emby-Token", token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestUsersAuthAndCRUD(t *testing.T) {
	env := setupUsersEnv(t)

	// GET /Users/Public defaults to hidden empty array [] when EnableLoginUserList is false
	rec := doReq(env.router, http.MethodGet, "/Users/Public", "", "")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("ListPublicUsers (default hidden): expected 200 [], got %d %s", rec.Code, rec.Body.String())
	}
	// Once enabled by admin, GET /Users/Public exposes non-hidden accounts
	env.handler.cfg.Update(func(c *config.Config) {
		c.EnableLoginUserList = true
	})
	rec = doReq(env.router, http.MethodGet, "/Users/Public", "", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "admin") {
		t.Fatalf("ListPublicUsers (enabled): %d %s", rec.Code, rec.Body.String())
	}

	// POST /Users/AuthenticateByName: bad JSON -> 400
	if rec := doReq(env.router, http.MethodPost, "/Users/AuthenticateByName", "", `{bad`); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	// Invalid credentials -> 401
	if rec := doReq(env.router, http.MethodPost, "/Users/AuthenticateByName", "", `{"Username":"admin","Pw":"wrong"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	// Valid login -> 200
	rec = doReq(env.router, http.MethodPost, "/Users/AuthenticateByName", "", `{"Username":"admin","Pw":"adminpw"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "AccessToken") {
		t.Fatalf("AuthenticateByName valid: %d %s", rec.Code, rec.Body.String())
	}

	// GET /Users: admin gets all, viewer gets only self
	rec = doReq(env.router, http.MethodGet, "/Users", env.adminTok, "")
	var allUsers []dto.UserDto
	_ = json.Unmarshal(rec.Body.Bytes(), &allUsers)
	if rec.Code != http.StatusOK || len(allUsers) != 2 {
		t.Fatalf("admin ListUsers: %d len=%d", rec.Code, len(allUsers))
	}
	rec = doReq(env.router, http.MethodGet, "/Users", env.viewerTok, "")
	var selfUsers []dto.UserDto
	_ = json.Unmarshal(rec.Body.Bytes(), &selfUsers)
	if rec.Code != http.StatusOK || len(selfUsers) != 1 || selfUsers[0].Name != "viewer" {
		t.Fatalf("viewer ListUsers: %d %+v", rec.Code, selfUsers)
	}

	// GET /Users/{Id}: viewer accessing admin -> 403; viewer accessing self -> 200; admin accessing nonexistent -> 404
	if rec := doReq(env.router, http.MethodGet, "/Users/"+env.admin.ID, env.viewerTok, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
	if rec := doReq(env.router, http.MethodGet, "/Users/"+env.viewer.ID, env.viewerTok, ""); rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec := doReq(env.router, http.MethodGet, "/Users/nonexistent", env.adminTok, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}

	// POST /Users/New: bad JSON -> 400; empty name -> 400; duplicate -> 409; valid -> 200
	if rec := doReq(env.router, http.MethodPost, "/Users/New", env.adminTok, `{bad`); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	if rec := doReq(env.router, http.MethodPost, "/Users/New", env.adminTok, `{"Name":""}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 empty name, got %d", rec.Code)
	}
	if rec := doReq(env.router, http.MethodPost, "/Users/New", env.adminTok, `{"Name":"admin","Password":"pw"}`); rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 duplicate, got %d", rec.Code)
	}
	rec = doReq(env.router, http.MethodPost, "/Users/New", env.adminTok, `{"Name":"charlie","Password":"cpw","IsAdministrator":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("CreateUser charlie: %d %s", rec.Code, rec.Body.String())
	}
	var charlie dto.UserDto
	_ = json.Unmarshal(rec.Body.Bytes(), &charlie)

	// POST /Users/{Id}/Policy: bad json -> 400; not found -> 404; disable charlie -> 204 -> login returns 403
	if rec := doReq(env.router, http.MethodPost, "/Users/"+charlie.Id+"/Policy", env.adminTok, `{bad`); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	if rec := doReq(env.router, http.MethodPost, "/Users/nonexistent/Policy", env.adminTok, `{"IsDisabled":true}`); rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	if rec := doReq(env.router, http.MethodPost, "/Users/"+charlie.Id+"/Policy", env.adminTok, `{"IsDisabled":true}`); rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	if rec := doReq(env.router, http.MethodPost, "/Users/AuthenticateByName", "", `{"Username":"charlie","Pw":"cpw"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for disabled user login, got %d", rec.Code)
	}

	// POST /Users/{Id}/Configuration: viewer editing admin -> 403; bad json -> 400; not found -> 404; valid -> 204
	if rec := doReq(env.router, http.MethodPost, "/Users/"+env.admin.ID+"/Configuration", env.viewerTok, `{}`); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
	if rec := doReq(env.router, http.MethodPost, "/Users/"+env.viewer.ID+"/Configuration", env.viewerTok, `{bad`); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	if rec := doReq(env.router, http.MethodPost, "/Users/nonexistent/Configuration", env.adminTok, `{}`); rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	if rec := doReq(env.router, http.MethodPost, "/Users/"+env.viewer.ID+"/Configuration", env.viewerTok, `{"SubtitleMode":"Always"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}

	// POST /Users/{Id}/Password: forbidden -> 403; bad json -> 400; wrong current -> 401; not found -> 404; reset -> 204; self update -> 204
	if rec := doReq(env.router, http.MethodPost, "/Users/"+env.admin.ID+"/Password", env.viewerTok, `{}`); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
	if rec := doReq(env.router, http.MethodPost, "/Users/"+env.viewer.ID+"/Password", env.viewerTok, `{bad`); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	if rec := doReq(env.router, http.MethodPost, "/Users/"+env.viewer.ID+"/Password", env.viewerTok, `{"CurrentPw":"wrong","NewPw":"npw"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	if rec := doReq(env.router, http.MethodPost, "/Users/nonexistent/Password", env.adminTok, `{"NewPw":"npw"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	if rec := doReq(env.router, http.MethodPost, "/Users/"+charlie.Id+"/Password", env.adminTok, `{"ResetPassword":true}`); rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 reset, got %d", rec.Code)
	}
	if rec := doReq(env.router, http.MethodPost, "/Users/"+env.viewer.ID+"/Password", env.viewerTok, `{"CurrentPw":"viewerpw","NewPw":"viewerpw2"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 self pw change, got %d", rec.Code)
	}

	// DELETE /Users/{Id}: self -> 400; nonexistent -> 404; charlie -> 204
	if rec := doReq(env.router, http.MethodDelete, "/Users/"+env.admin.ID, env.adminTok, ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 self delete, got %d", rec.Code)
	}
	if rec := doReq(env.router, http.MethodDelete, "/Users/nonexistent", env.adminTok, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 delete, got %d", rec.Code)
	}
	if rec := doReq(env.router, http.MethodDelete, "/Users/"+charlie.Id, env.adminTok, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 delete charlie, got %d", rec.Code)
	}
}

func TestAPIKeysSessionsDevicesAndDisplayPrefs(t *testing.T) {
	env := setupUsersEnv(t)

	// API Keys: create via query, via body App, via body AppName, list, delete
	rec := doReq(env.router, http.MethodPost, "/Auth/Keys?App=Sonarr", env.adminTok, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Sonarr") {
		t.Fatalf("CreateAPIKey query: %d %s", rec.Code, rec.Body.String())
	}
	var key1 APIKeyDto
	_ = json.Unmarshal(rec.Body.Bytes(), &key1)

	rec = doReq(env.router, http.MethodPost, "/Auth/Keys", env.adminTok, `{"App":"Radarr"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Radarr") {
		t.Fatalf("CreateAPIKey body App: %d %s", rec.Code, rec.Body.String())
	}
	rec = doReq(env.router, http.MethodPost, "/Auth/Keys", env.adminTok, `{"AppName":"Bazarr"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Bazarr") {
		t.Fatalf("CreateAPIKey body AppName: %d %s", rec.Code, rec.Body.String())
	}
	var key3 APIKeyDto
	_ = json.Unmarshal(rec.Body.Bytes(), &key3)
	if key1.Id == "" || key3.Id == "" {
		t.Fatalf("expected non-empty KeyID on created API keys: %+v %+v", key1, key3)
	}
	rec = doReq(env.router, http.MethodGet, "/Auth/Keys", env.adminTok, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Sonarr") {
		t.Fatalf("ListAPIKeys: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), key1.AccessToken) {
		t.Fatalf("GET /Auth/Keys must mask plaintext AccessToken (M-08), got %s", rec.Body.String())
	}
	if rec := doReq(env.router, http.MethodDelete, "/Auth/Keys/"+key1.AccessToken, env.adminTok, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("DeleteAPIKey by token: %d", rec.Code)
	}
	if rec := doReq(env.router, http.MethodDelete, "/Auth/Keys/"+key3.Id, env.adminTok, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("DeleteAPIKey by KeyID: %d", rec.Code)
	}

	// Sessions & Playback
	if rec := doReq(env.router, http.MethodPost, "/Sessions/Capabilities", env.viewerTok, `{"SupportsRemoteControl":true}`); rec.Code != http.StatusNoContent {
		t.Fatalf("Capabilities: %d", rec.Code)
	}
	if rec := doReq(env.router, http.MethodPost, "/Sessions/Playing", env.viewerTok, `{bad`); rec.Code != http.StatusBadRequest {
		t.Fatalf("Playing bad json: %d", rec.Code)
	}
	if rec := doReq(env.router, http.MethodPost, "/Sessions/Playing", env.viewerTok, `{"ItemId":"item1","PositionTicks":100}`); rec.Code != http.StatusNoContent {
		t.Fatalf("Playing valid: %d", rec.Code)
	}
	if rec := doReq(env.router, http.MethodPost, "/Sessions/Playing/Progress", env.viewerTok, `{bad`); rec.Code != http.StatusBadRequest {
		t.Fatalf("Progress bad json: %d", rec.Code)
	}
	if rec := doReq(env.router, http.MethodPost, "/Sessions/Playing/Progress", env.viewerTok, `{"ItemId":"item1","PositionTicks":200}`); rec.Code != http.StatusNoContent {
		t.Fatalf("Progress valid: %d", rec.Code)
	}
	if rec := doReq(env.router, http.MethodPost, "/Sessions/Playing/Stopped", env.viewerTok, `{bad`); rec.Code != http.StatusBadRequest {
		t.Fatalf("Stopped bad json: %d", rec.Code)
	}
	if rec := doReq(env.router, http.MethodPost, "/Sessions/Playing/Stopped", env.viewerTok, `{"ItemId":"item1","PositionTicks":300}`); rec.Code != http.StatusNoContent {
		t.Fatalf("Stopped valid: %d", rec.Code)
	}

	// ListSessions with filters
	rec = doReq(env.router, http.MethodGet, "/Sessions?DeviceId=dev-viewer&ControllableByUserId="+env.viewer.ID, env.adminTok, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "dev-viewer") {
		t.Fatalf("ListSessions filtered: %d %s", rec.Code, rec.Body.String())
	}
	rec = doReq(env.router, http.MethodGet, "/Sessions?DeviceId=other&ControllableByUserId=other", env.adminTok, "")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("ListSessions empty filter: %d %s", rec.Code, rec.Body.String())
	}

	// ListSessions when sessionMgr is nil
	hNilSess := NewHandler(env.authSvc, env.userRepo, nil, nil, nil)
	recNil := httptest.NewRecorder()
	hNilSess.ListSessions(recNil, httptest.NewRequest(http.MethodGet, "/Sessions", nil))
	if recNil.Code != http.StatusOK || strings.TrimSpace(recNil.Body.String()) != "[]" {
		t.Fatalf("ListSessions nil mgr: %d %s", recNil.Code, recNil.Body.String())
	}

	// ListDevices & DeleteDevice
	rec = doReq(env.router, http.MethodGet, "/Devices", env.adminTok, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "dev-viewer") {
		t.Fatalf("ListDevices: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doReq(env.router, http.MethodDelete, "/Devices", env.adminTok, ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("DeleteDevice missing Id: expected 400, got %d", rec.Code)
	}
	if rec := doReq(env.router, http.MethodDelete, "/Devices?Id=dev-viewer", env.adminTok, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("DeleteDevice: expected 204, got %d", rec.Code)
	}

	// DisplayPreferences: default -> invalid json 400 -> empty body 204 -> custom json 204 -> get custom json
	rec = doReq(env.router, http.MethodGet, "/DisplayPreferences/usersettings", env.adminTok, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "SortName") {
		t.Fatalf("GetDisplayPreferences default: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doReq(env.router, http.MethodPost, "/DisplayPreferences/usersettings", env.adminTok, `{bad`); rec.Code != http.StatusBadRequest {
		t.Fatalf("UpdateDisplayPreferences bad json: %d", rec.Code)
	}
	if rec := doReq(env.router, http.MethodPost, "/DisplayPreferences/usersettings", env.adminTok, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("UpdateDisplayPreferences empty: %d", rec.Code)
	}
	if rec := doReq(env.router, http.MethodPost, "/DisplayPreferences/usersettings", env.adminTok, `{"Id":"usersettings","SortBy":"DateCreated"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("UpdateDisplayPreferences custom: %d", rec.Code)
	}
	rec = doReq(env.router, http.MethodGet, "/DisplayPreferences/usersettings", env.adminTok, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "DateCreated") {
		t.Fatalf("GetDisplayPreferences custom: %d %s", rec.Code, rec.Body.String())
	}

	// Logout
	if rec := doReq(env.router, http.MethodPost, "/Sessions/Logout", env.adminTok, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("Logout: expected 204, got %d", rec.Code)
	}
	// Token is now revoked -> 401
	if rec := doReq(env.router, http.MethodGet, "/Users", env.adminTok, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 after logout, got %d", rec.Code)
	}
}

func TestUsersHandlerEdgeAndErrorBranches(t *testing.T) {
	env := setupUsersEnv(t)

	// ListUsers with non-admin & nil User -> returns []
	req := httptest.NewRequest(http.MethodGet, "/Users", nil)
	req = req.WithContext(middleware.WithAuthContext(req.Context(), &middleware.AuthContext{}))
	rec := httptest.NewRecorder()
	env.handler.ListUsers(rec, req)
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("ListUsers empty ctx: %d %s", rec.Code, rec.Body.String())
	}

	// DisplayPreferences fallback client="emby" when query and ac.Client.Client are empty
	reqDP := httptest.NewRequest(http.MethodGet, "/DisplayPreferences/home", nil)
	reqDP = reqDP.WithContext(middleware.WithAuthContext(reqDP.Context(), &middleware.AuthContext{User: env.admin}))
	recDP := httptest.NewRecorder()
	env.handler.GetDisplayPreferences(recDP, reqDP)
	if recDP.Code != http.StatusOK || !strings.Contains(recDP.Body.String(), `"Client":"emby"`) {
		t.Fatalf("GetDisplayPreferences fallback client: %d %s", recDP.Code, recDP.Body.String())
	}
	reqDP2 := httptest.NewRequest(http.MethodPost, "/DisplayPreferences/home", bytes.NewBufferString(`{"SortBy":"Name"}`))
	reqDP2 = reqDP2.WithContext(middleware.WithAuthContext(reqDP2.Context(), &middleware.AuthContext{User: env.admin}))
	recDP2 := httptest.NewRecorder()
	env.handler.UpdateDisplayPreferences(recDP2, reqDP2)
	if recDP2.Code != http.StatusNoContent {
		t.Fatalf("UpdateDisplayPreferences fallback client: %d", recDP2.Code)
	}

	// Close underlying SQLite store to exercise 500 internal error paths cleanly.
	env.handler.cfg.Update(func(c *config.Config) {
		c.EnableLoginUserList = true
	})
	_ = env.store.Close()

	adminCtx := &middleware.AuthContext{User: env.admin, IsAPIKey: true}
	withAdmin := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		r = r.WithContext(middleware.WithAuthContext(r.Context(), adminCtx))
		w := httptest.NewRecorder()
		return func() *httptest.ResponseRecorder {
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("Id", env.viewer.ID)
			rctx.URLParams.Add("Key", "k1")
			r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
			switch {
			case method == http.MethodGet && path == "/Users/Public":
				env.handler.ListPublicUsers(w, r)
			case method == http.MethodPost && path == "/Users/AuthenticateByName":
				env.handler.AuthenticateByName(w, r)
			case method == http.MethodGet && path == "/Users":
				env.handler.ListUsers(w, r)
			case method == http.MethodGet && path == "/Users/id":
				env.handler.GetUser(w, r)
			case method == http.MethodDelete && path == "/Users/id":
				env.handler.DeleteUser(w, r)
			case method == http.MethodPost && path == "/Users/id/Password":
				env.handler.UpdateUserPassword(w, r)
			case method == http.MethodPost && path == "/Users/id/Policy":
				env.handler.UpdateUserPolicy(w, r)
			case method == http.MethodPost && path == "/Users/id/Configuration":
				env.handler.UpdateUserConfiguration(w, r)
			case method == http.MethodGet && path == "/Auth/Keys":
				env.handler.ListAPIKeys(w, r)
			case method == http.MethodPost && path == "/Auth/Keys":
				env.handler.CreateAPIKey(w, r)
			case method == http.MethodDelete && path == "/Auth/Keys/k1":
				env.handler.DeleteAPIKey(w, r)
			case method == http.MethodGet && path == "/Devices":
				env.handler.ListDevices(w, r)
			case method == http.MethodDelete && strings.HasPrefix(path, "/Devices"):
				env.handler.DeleteDevice(w, r)
			case method == http.MethodGet && path == "/DisplayPreferences/id":
				env.handler.GetDisplayPreferences(w, r)
			case method == http.MethodPost && path == "/DisplayPreferences/id":
				env.handler.UpdateDisplayPreferences(w, r)
			}
			return w
		}()
	}

	checks := []struct {
		method, path, body string
	}{
		{http.MethodGet, "/Users/Public", ""},
		{http.MethodPost, "/Users/AuthenticateByName", `{"Username":"admin","Pw":"adminpw"}`},
		{http.MethodGet, "/Users", ""},
		{http.MethodGet, "/Users/id", ""},
		{http.MethodDelete, "/Users/id", ""},
		{http.MethodPost, "/Users/id/Password", `{"NewPw":"x"}`},
		{http.MethodPost, "/Users/id/Policy", `{}`},
		{http.MethodPost, "/Users/id/Configuration", `{}`},
		{http.MethodGet, "/Auth/Keys", ""},
		{http.MethodPost, "/Auth/Keys", `{"App":"X"}`},
		{http.MethodDelete, "/Auth/Keys/k1", ""},
		{http.MethodGet, "/Devices", ""},
		{http.MethodDelete, "/Devices?Id=d1", ""},
		{http.MethodGet, "/DisplayPreferences/id", ""},
		{http.MethodPost, "/DisplayPreferences/id", `{}`},
	}
	for _, c := range checks {
		w := withAdmin(c.method, c.path, c.body)
		if w.Code != http.StatusInternalServerError {
			t.Errorf("%s %s with closed DB: expected 500, got %d (%s)", c.method, c.path, w.Code, w.Body.String())
		}
	}
}
