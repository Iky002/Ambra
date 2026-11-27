package compatcore

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/api/ws"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

type fullMockWSHub struct {
	mu              sync.Mutex
	sentUserDevices []struct {
		userID, deviceID string
		msg              ws.Message
	}
	disconnectedDevices  []string
	disconnectedUserDevs []struct{ userID, deviceID string }
	disconnectedUsers    []string
}

func (m *fullMockWSHub) SendToUserDevice(userID, deviceID string, msg ws.Message) {
	m.mu.Lock()
	m.sentUserDevices = append(m.sentUserDevices, struct {
		userID, deviceID string
		msg              ws.Message
	}{userID, deviceID, msg})
	m.mu.Unlock()
}

func (m *fullMockWSHub) DisconnectDevice(deviceID string) {
	m.mu.Lock()
	m.disconnectedDevices = append(m.disconnectedDevices, deviceID)
	m.mu.Unlock()
}

func (m *fullMockWSHub) DisconnectUserDevice(userID, deviceID string) {
	m.mu.Lock()
	m.disconnectedUserDevs = append(m.disconnectedUserDevs, struct{ userID, deviceID string }{userID, deviceID})
	m.mu.Unlock()
}

func (m *fullMockWSHub) DisconnectUser(userID string) {
	m.mu.Lock()
	m.disconnectedUsers = append(m.disconnectedUsers, userID)
	m.mu.Unlock()
}

func TestCompatCoreV0104SecurityRemediations(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	authSvc := auth.NewService(env.userRepo, env.cfg)
	authMW := middleware.NewAuth(authSvc, env.sessions)
	env.handler.SetAuthService(authSvc)
	mockWS := &fullMockWSHub{}
	env.handler.SetWSHub(mockWS)

	// Add a third non-admin user (bob)
	bob, err := authSvc.CreateUser(ctx, "bob_v0104", "bobpass", false)
	if err != nil {
		t.Fatal(err)
	}

	adminAuth, err := authSvc.AuthenticateByName(ctx, "admin", "admin", "", auth.ClientInfo{DeviceID: "shared-tv"})
	if err != nil {
		t.Fatal(err)
	}
	guestAuth, err := authSvc.AuthenticateByName(ctx, "9guest", "pass", "", auth.ClientInfo{DeviceID: "shared-tv"})
	if err != nil {
		t.Fatal(err)
	}
	bobAuth, err := authSvc.AuthenticateByName(ctx, "bob_v0104", "bobpass", "", auth.ClientInfo{DeviceID: "bob-phone"})
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

	doAuth := func(token, method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("X-Emby-Token", token)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	// 1. [H-01] Remote control binds strictly to (sess.UserId, sess.DeviceId)
	guestSess := env.sessions.TouchSession(env.user2, auth.ClientInfo{DeviceID: "shared-tv"})
	_ = env.sessions.TouchSession(env.admin, auth.ClientInfo{DeviceID: "shared-tv"})
	if rec := doAuth(guestAuth.AccessToken, http.MethodPost, "/Sessions/"+guestSess.Id+"/Playing/Pause", `{}`); rec.Code != http.StatusNoContent {
		t.Fatalf("guest Pause expected 204, got %d", rec.Code)
	}
	mockWS.mu.Lock()
	if len(mockWS.sentUserDevices) != 1 || mockWS.sentUserDevices[0].userID != env.user2.ID || mockWS.sentUserDevices[0].deviceID != "shared-tv" {
		t.Fatalf("expected SendToUserDevice(%s, shared-tv), got %+v", env.user2.ID, mockWS.sentUserDevices)
	}
	mockWS.mu.Unlock()

	// Non-admin direct call to postDevicesDelete only disconnects (callerID, deviceID)
	guestCtx := middleware.WithAuthContext(ctx, &middleware.AuthContext{
		User:   env.user2,
		Token:  guestAuth.AccessToken,
		Client: auth.ClientInfo{DeviceID: "shared-tv"},
	})
	delDevReq := httptest.NewRequest(http.MethodPost, "/Devices/Delete?Id=shared-tv", nil).WithContext(guestCtx)
	delDevRec := httptest.NewRecorder()
	env.handler.postDevicesDelete(delDevRec, delDevReq)
	if delDevRec.Code != http.StatusNoContent {
		t.Fatalf("non-admin postDevicesDelete expected 204, got %d", delDevRec.Code)
	}
	mockWS.mu.Lock()
	if len(mockWS.disconnectedUserDevs) != 1 || mockWS.disconnectedUserDevs[0].userID != env.user2.ID || mockWS.disconnectedUserDevs[0].deviceID != "shared-tv" {
		t.Fatalf("expected DisconnectUserDevice(%s, shared-tv), got %+v", env.user2.ID, mockWS.disconnectedUserDevs)
	}
	if len(mockWS.disconnectedDevices) != 0 {
		t.Fatalf("non-admin must NOT call global DisconnectDevice, got %+v", mockWS.disconnectedDevices)
	}
	mockWS.mu.Unlock()
	if _, err := authSvc.ValidateToken(ctx, adminAuth.AccessToken); err != nil {
		t.Fatalf("expected admin's token on shared-tv to survive guest's device delete: %v", err)
	}

	// Re-login guest for subsequent tests
	guestAuth, err = authSvc.AuthenticateByName(ctx, "9guest", "pass", "", auth.ClientInfo{DeviceID: "guest-dev-new"})
	if err != nil {
		t.Fatal(err)
	}

	// 2. [H-02 & MH-05] Private playlist filtering in Sections, Suggestions, canAccessItem, and Path sanitization
	bobPrivPl, err := env.libRepo.CreatePlaylist(ctx, "BobSecretPlaylist", "Audio", bob.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	bobPubPl, err := env.libRepo.CreatePlaylist(ctx, "BobPublicPlaylist", "Audio", bob.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = env.userRepo.SetServerConfig(ctx, "item_visibility_"+bobPubPl.ID, `{"IsPublic":true}`)

	// Seed a movie with server filesystem path + trailer extra with path
	movieRec, _ := env.libRepo.GetItemRecordByID(ctx, env.itemID)
	movieRec.Path = "/srv/media/secret_movie.mkv"
	_ = env.libRepo.UpsertItem(ctx, movieRec)
	trRec := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: movieRec.LibraryID, ParentID: movieRec.ID,
		Type: "Video", MediaType: "Video", Name: "SecretTrailer", Path: "/srv/media/secret_trailer.mkv",
		Extra: sqlite.ItemExtra{ExtraType: "Trailer"},
	}
	_ = env.libRepo.UpsertItem(ctx, trRec)

	// Sections/latest/Items must NOT include bobPrivPl and must NOT leak Paths to guest
	rec := doAuth(guestAuth.AccessToken, http.MethodGet, "/Users/"+env.user2.ID+"/Sections/latest/Items?Fields=Path&Limit=50", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("Sections latest got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), bobPrivPl.ID) || strings.Contains(rec.Body.String(), "BobSecretPlaylist") {
		t.Fatalf("Sections leaked bob's private playlist to guest: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "/srv/media/") {
		t.Fatalf("Sections leaked server path to non-admin: %s", rec.Body.String())
	}

	// Suggestions must NOT include bobPrivPl and must NOT leak Paths to guest
	rec = doAuth(guestAuth.AccessToken, http.MethodGet, "/Users/"+env.user2.ID+"/Suggestions?Limit=50", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("Suggestions got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), bobPrivPl.ID) {
		t.Fatalf("Suggestions leaked bob's private playlist to guest: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "/srv/media/") {
		t.Fatalf("Suggestions leaked server path to non-admin: %s", rec.Body.String())
	}

	// LocalTrailers must NOT leak path to guest and must block bobPrivPl
	rec = doAuth(guestAuth.AccessToken, http.MethodGet, "/Users/"+env.user2.ID+"/Items/"+movieRec.ID+"/LocalTrailers", "")
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "/srv/media/") {
		t.Fatalf("LocalTrailers status=%d leaked path=%v", rec.Code, strings.Contains(rec.Body.String(), "/srv/media/"))
	}
	rec = doAuth(guestAuth.AccessToken, http.MethodGet, "/Users/"+env.user2.ID+"/Items/"+bobPrivPl.ID+"/LocalTrailers", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("LocalTrailers on bob private playlist expected 403, got %d", rec.Code)
	}
	rec = doAuth(guestAuth.AccessToken, http.MethodPost, "/Users/"+env.user2.ID+"/Items/"+bobPrivPl.ID+"/Rating?Likes=true", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("Rating on bob private playlist expected 403, got %d", rec.Code)
	}

	// PlayQueue must NOT leak NowPlayingItem.Path to non-admin
	env.sessions.SetLibraryRepo(env.libRepo)
	_ = env.sessions.ReportPlaybackStart(ctx, env.user2, auth.ClientInfo{DeviceID: "guest-dev-new"}, session.PlaybackStartRequest{ItemId: movieRec.ID})
	rec = doAuth(guestAuth.AccessToken, http.MethodGet, "/Sessions/PlayQueue", "")
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "/srv/media/") {
		t.Fatalf("PlayQueue status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 3. [H-03] Last active admin protection & revocation in POST /Users/{Id}, CopyData, Delete
	// Demoting/disabling last admin via POST /Users/{Id} -> 400
	if r := doAuth(adminAuth.AccessToken, http.MethodPost, "/Users/"+env.admin.ID, `{"Policy":{"IsAdministrator":false,"IsDisabled":false}}`); r.Code != http.StatusBadRequest {
		t.Fatalf("demoting last admin via POST /Users/{Id} expected 400, got %d", r.Code)
	}
	if r := doAuth(adminAuth.AccessToken, http.MethodPost, "/Users/"+env.admin.ID, `{"Policy":{"IsAdministrator":true,"IsDisabled":true}}`); r.Code != http.StatusBadRequest {
		t.Fatalf("disabling last admin via POST /Users/{Id} expected 400, got %d", r.Code)
	}
	// CopyData copying non-admin policy onto last admin -> 400
	if r := doAuth(adminAuth.AccessToken, http.MethodPost, "/Users/"+bob.ID+"/CopyData", `{"ToUserIds":["`+env.admin.ID+`"],"CopyOptions":["UserPolicy"]}`); r.Code != http.StatusBadRequest {
		t.Fatalf("CopyData demoting last admin expected 400, got %d (%s)", r.Code, r.Body.String())
	}
	// API key deleting last admin via POST /Users/{Id}/Delete -> 400
	apiKey, _ := authSvc.CreateAPIKey(ctx, "AdminKey")
	if r := doAuth(apiKey.Token, http.MethodPost, "/Users/"+env.admin.ID+"/Delete", ""); r.Code != http.StatusBadRequest {
		t.Fatalf("API key deleting last admin expected 400, got %d", r.Code)
	}

	// Disabling bob via POST /Users/{Id} revokes tokens, removes sessions, disconnects WS
	env.sessions.TouchSession(bob, auth.ClientInfo{DeviceID: "bob-phone"})
	if r := doAuth(adminAuth.AccessToken, http.MethodPost, "/Users/"+bob.ID, `{"Policy":{"IsDisabled":true}}`); r.Code != http.StatusNoContent {
		t.Fatalf("disable bob expected 204, got %d", r.Code)
	}
	if _, err := authSvc.ValidateToken(ctx, bobAuth.AccessToken); err == nil {
		t.Fatal("expected bob's token revoked on disable via POST /Users/{Id}")
	}
	if _, ok := env.sessions.GetSessionByUserDevice(bob.ID, "bob-phone"); ok {
		t.Fatal("expected bob's session removed on disable")
	}
	mockWS.mu.Lock()
	foundBobWS := false
	for _, uid := range mockWS.disconnectedUsers {
		if uid == bob.ID {
			foundBobWS = true
		}
	}
	mockWS.mu.Unlock()
	if !foundBobWS {
		t.Fatal("expected DisconnectUser(bob) on disable")
	}

	// Deleting bob via POST /Users/{Id}/Delete succeeds and disconnects
	if r := doAuth(adminAuth.AccessToken, http.MethodPost, "/Users/"+bob.ID+"/Delete", ""); r.Code != http.StatusNoContent {
		t.Fatalf("delete bob expected 204, got %d", r.Code)
	}

	// 4. KV limits on HomeSections (500 entries + 64KiB body) and SearchedItems (64KiB body + 50 entries)
	oversized := `{"Name":"` + strings.Repeat("A", maxKVJSONBodyBytes+1024) + `"}`
	if r := doAuth(guestAuth.AccessToken, http.MethodPost, "/Users/"+env.user2.ID+"/HomeSections", oversized); r.Code != http.StatusRequestEntityTooLarge && r.Code != http.StatusBadRequest {
		t.Fatalf("oversized HomeSections expected 413/400, got %d", r.Code)
	}
	if r := doAuth(guestAuth.AccessToken, http.MethodPost, "/Users/"+env.user2.ID+"/SearchedItems", oversized); r.Code != http.StatusRequestEntityTooLarge && r.Code != http.StatusBadRequest {
		t.Fatalf("oversized SearchedItems expected 413/400, got %d", r.Code)
	}
	// Pre-populate 500 home sections and verify 501st is rejected with 400
	fullSections := make([]homeSection, maxHomeSectionsEntries)
	for i := range fullSections {
		fullSections[i] = homeSection{Id: guid.New(), Name: "S"}
	}
	_ = env.handler.saveKV(ctx, "user_home_sections:"+env.user2.ID, fullSections)
	if r := doAuth(guestAuth.AccessToken, http.MethodPost, "/Users/"+env.user2.ID+"/HomeSections", `{"Id":"overflow","Name":"TooMany"}`); r.Code != http.StatusBadRequest {
		t.Fatalf("501st HomeSection expected 400, got %d (%s)", r.Code, r.Body.String())
	}

	// 5. [M-03] Composite (ClientIP, UserID) failure-only rate limit on authenticateUserByID + loopback exemption
	for i := 0; i < authByIDRateLimit; i++ {
		req := httptest.NewRequest(http.MethodPost, "/Users/"+env.user2.ID+"/Authenticate", strings.NewReader(`{"Pw":"wrong"}`))
		req.RemoteAddr = "198.51.100.42:1234"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("authByID attempt %d expected 401, got %d", i+1, w.Code)
		}
	}
	req429 := httptest.NewRequest(http.MethodPost, "/Users/"+env.user2.ID+"/Authenticate", strings.NewReader(`{"Pw":"wrong"}`))
	req429.RemoteAddr = "198.51.100.42:1234"
	w429 := httptest.NewRecorder()
	router.ServeHTTP(w429, req429)
	if w429.Code != http.StatusTooManyRequests {
		t.Fatalf("authByID composite rate limit expected 429 for offending IP, got %d", w429.Code)
	}
	// Legitimate login from loopback (or another IP) for the same user MUST NOT be locked out!
	reqLoop := httptest.NewRequest(http.MethodPost, "/Users/"+env.user2.ID+"/Authenticate", strings.NewReader(`{"Pw":"pass"}`))
	reqLoop.RemoteAddr = "127.0.0.1:9999"
	wLoop := httptest.NewRecorder()
	router.ServeHTTP(wLoop, reqLoop)
	if wLoop.Code != http.StatusOK {
		t.Fatalf("M-03: loopback login must not be locked out by external brute-force, got %d", wLoop.Code)
	}
	var dummy dto.UserDto
	_ = json.Unmarshal([]byte(`{}`), &dummy)
}
