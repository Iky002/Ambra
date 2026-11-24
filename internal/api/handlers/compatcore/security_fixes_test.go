package compatcore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/api/ws"
	"github.com/ambra-media/ambra/internal/core/auth"
)

func TestUserPrivilegeEscalationAndIDORBlocked(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	authSvc := auth.NewService(env.userRepo, env.cfg)
	authMW := middleware.NewAuth(authSvc, env.sessions)

	adminAuth, err := authSvc.AuthenticateByName(ctx, "admin", "admin", "", auth.ClientInfo{DeviceID: "admin-dev"})
	if err != nil {
		t.Fatalf("auth admin: %v", err)
	}
	guestAuth, err := authSvc.AuthenticateByName(ctx, "9guest", "pass", "", auth.ClientInfo{DeviceID: "guest-dev"})
	if err != nil {
		t.Fatalf("auth guest: %v", err)
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

	// 1. Non-admin attempting privilege escalation via POST /Users/{self} with Policy -> 403
	rec := doAuthReq(guestAuth.AccessToken, http.MethodPost, "/Users/"+env.user2.ID,
		`{"Policy":{"IsAdministrator":true,"EnableAllFolders":true}}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin Policy escalation, got %d (%s)", rec.Code, rec.Body.String())
	}
	reloaded, err := env.userRepo.GetUserByID(ctx, env.user2.ID)
	if err != nil {
		t.Fatalf("reload user2: %v", err)
	}
	if reloaded.IsAdmin || reloaded.Policy.IsAdministrator {
		t.Fatal("user2 must NOT have been promoted to admin")
	}

	// 2. Non-admin updating own Configuration (without Policy) -> 204
	rec = doAuthReq(guestAuth.AccessToken, http.MethodPost, "/Users/"+env.user2.ID,
		`{"Configuration":{"PlayDefaultAudioTrack":true}}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for self config update, got %d (%s)", rec.Code, rec.Body.String())
	}

	// 3. Non-admin IDOR targeting admin user on all protected user mutation routes -> 403
	idorCases := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/Users/" + env.admin.ID, `{"Name":"hacked"}`},
		{http.MethodPost, "/Users/" + env.admin.ID + "/Configuration/Partial", `{"SubtitleMode":"Always"}`},
		{http.MethodPost, "/UserSettings/" + env.admin.ID, `{"theme":"hacked"}`},
		{http.MethodPost, "/UserSettings/" + env.admin.ID + "/Partial", `{"lang":"en"}`},
		{http.MethodPost, "/Users/" + env.admin.ID + "/Items/" + env.itemID + "/UserData", `{"Played":true}`},
		{http.MethodDelete, "/Users/" + env.admin.ID + "/TrackSelections/audio", ``},
		{http.MethodPost, "/Users/" + env.admin.ID + "/TrackSelections/subtitle/Delete", ``},
		{http.MethodDelete, "/Users/" + env.admin.ID + "/RecentlySearched", ``},
		{http.MethodPost, "/Users/" + env.admin.ID + "/RecentlySearched/Delete", ``},
		{http.MethodPost, "/Users/" + env.admin.ID + "/SearchedItems", `{"Name":"x"}`},
		{http.MethodPost, "/Users/" + env.admin.ID + "/TypedSettings/theme", `{"k":"v"}`},
		{http.MethodPost, "/Users/" + env.admin.ID + "/HomeSections", `{"Name":"hacked"}`},
		{http.MethodPost, "/Users/" + env.admin.ID + "/HomeSections/Delete", `{"Ids":["resume"]}`},
		{http.MethodPost, "/Users/" + env.admin.ID + "/HomeSections/Move", `{"Id":"resume","NewIndex":0}`},
		{http.MethodPost, "/Users/" + env.admin.ID + "/FavoriteItems/" + env.itemID + "/Delete", ``},
		{http.MethodPost, "/Users/" + env.admin.ID + "/Items/" + env.itemID + "/Rating?Likes=true", ``},
		{http.MethodDelete, "/Users/" + env.admin.ID + "/Items/" + env.itemID + "/Rating", ``},
		{http.MethodPost, "/Users/" + env.admin.ID + "/Items/" + env.itemID + "/Rating/Delete", ``},
		{http.MethodPost, "/Users/" + env.admin.ID + "/Items/" + env.itemID + "/HideFromResume", ``},
		{http.MethodPost, "/Users/" + env.admin.ID + "/PlayedItems/" + env.itemID + "/Delete", ``},
		{http.MethodPost, "/Users/" + env.admin.ID + "/PlayingItems/" + env.itemID, ``},
		{http.MethodPost, "/Users/" + env.admin.ID + "/PlayingItems/" + env.itemID + "/Progress", ``},
		{http.MethodDelete, "/Users/" + env.admin.ID + "/PlayingItems/" + env.itemID, ``},
		{http.MethodPost, "/Users/" + env.admin.ID + "/PlayingItems/" + env.itemID + "/Delete", ``},
	}
	for _, tc := range idorCases {
		res := doAuthReq(guestAuth.AccessToken, tc.method, tc.path, tc.body)
		if res.Code != http.StatusForbidden {
			t.Fatalf("IDOR %s %s: expected 403, got %d (%s)", tc.method, tc.path, res.Code, res.Body.String())
		}
	}

	// 4. GET /Connect/Exchange requires auth (401 without token) and returns caller's own ID (not users[0])
	if rec := doAuthReq("", http.MethodGet, "/Connect/Exchange?ConnectUserId=c1", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated GET /Connect/Exchange, got %d", rec.Code)
	}
	exRec := doAuthReq(guestAuth.AccessToken, http.MethodGet, "/Connect/Exchange?ConnectUserId=c1", "")
	if exRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for authenticated GET /Connect/Exchange, got %d", exRec.Code)
	}
	var exBody map[string]any
	_ = json.Unmarshal(exRec.Body.Bytes(), &exBody)
	if exBody["LocalUserId"] != env.user2.ID {
		t.Fatalf("expected LocalUserId=%s, got %v", env.user2.ID, exBody["LocalUserId"])
	}

	// 5. GET /Users/Query returns only caller's own record for non-admin, all users for admin
	qGuest := doAuthReq(guestAuth.AccessToken, http.MethodGet, "/Users/Query", "")
	if qGuest.Code != http.StatusOK {
		t.Fatalf("guest GET /Users/Query status=%d", qGuest.Code)
	}
	var guestUsers dto.QueryResult[dto.UserDto]
	_ = json.Unmarshal(qGuest.Body.Bytes(), &guestUsers)
	if guestUsers.TotalRecordCount != 1 || guestUsers.Items[0].Id != env.user2.ID {
		t.Fatalf("expected non-admin /Users/Query to return only self, got %+v", guestUsers)
	}

	qAdmin := doAuthReq(adminAuth.AccessToken, http.MethodGet, "/Users/Query", "")
	var adminUsers dto.QueryResult[dto.UserDto]
	_ = json.Unmarshal(qAdmin.Body.Bytes(), &adminUsers)
	if adminUsers.TotalRecordCount < 2 {
		t.Fatalf("expected admin /Users/Query to return all users, got %d", adminUsers.TotalRecordCount)
	}

	// 6. Admin updating another user's Policy -> 204
	rec = doAuthReq(adminAuth.AccessToken, http.MethodPost, "/Users/"+env.user2.ID,
		`{"Policy":{"IsAdministrator":false,"EnableAllFolders":true}}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for admin updating user policy, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestUserAvatarXSSProtection(t *testing.T) {
	env := newTestEnv(t)

	// 1. Uploading HTML or SVG as avatar must be rejected with 400 Bad Request
	for _, payload := range []string{
		"<html><body><script>alert(1)</script></body></html>",
		base64.StdEncoding.EncodeToString([]byte("<html><script>alert(1)</script></html>")),
		`<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"></svg>`,
		"plain text not an image",
		"",
	} {
		rec := doReq(t, env.router, http.MethodPost, "/Users/"+env.admin.ID+"/Images/Primary", payload)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for non-raster avatar payload %q, got %d", payload, rec.Code)
		}
	}

	// 2. Valid PNG upload succeeds and serves with nosniff + sandbox CSP headers
	pngData := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDRfake")
	rec := doReq(t, env.router, http.MethodPost, "/Users/"+env.admin.ID+"/Images/Primary", base64.StdEncoding.EncodeToString(pngData))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for valid PNG avatar, got %d (%s)", rec.Code, rec.Body.String())
	}

	getRec := doReq(t, env.router, http.MethodGet, "/Users/"+env.admin.ID+"/Images/Primary", "")
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET avatar status=%d", getRec.Code)
	}
	if getRec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("expected image/png, got %q", getRec.Header().Get("Content-Type"))
	}
	if getRec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("expected X-Content-Type-Options=nosniff, got %q", getRec.Header().Get("X-Content-Type-Options"))
	}
	if getRec.Header().Get("Content-Security-Policy") != "sandbox" {
		t.Fatalf("expected Content-Security-Policy=sandbox, got %q", getRec.Header().Get("Content-Security-Policy"))
	}

	// 3. Even if a legacy non-image file exists on disk, serveUserImage forces application/octet-stream + nosniff + sandbox
	imgPath := env.handler.userImagePath(env.admin.ID, "Primary", "0")
	_ = os.MkdirAll(filepath.Dir(imgPath), 0o755)
	if err := os.WriteFile(imgPath, []byte("<html><script>alert(1)</script></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	legacyRec := doReq(t, env.router, http.MethodGet, "/Users/"+env.admin.ID+"/Images/Primary", "")
	if legacyRec.Code != http.StatusOK {
		t.Fatalf("GET legacy avatar status=%d", legacyRec.Code)
	}
	if legacyRec.Header().Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("expected application/octet-stream fallback for non-raster file, got %q", legacyRec.Header().Get("Content-Type"))
	}
	if legacyRec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("expected nosniff header")
	}
}

type fakeWSSender struct {
	sent []struct {
		deviceID string
		msg      ws.Message
	}
}

func (f *fakeWSSender) SendToDevice(deviceID string, msg ws.Message) {
	f.sent = append(f.sent, struct {
		deviceID string
		msg      ws.Message
	}{deviceID: deviceID, msg: msg})
}

func TestSessionRemoteControlWSForwardingAndRBAC(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	authSvc := auth.NewService(env.userRepo, env.cfg)
	authMW := middleware.NewAuth(authSvc, env.sessions)

	adminAuth, err := authSvc.AuthenticateByName(ctx, "admin", "admin", "", auth.ClientInfo{DeviceID: "admin-tv"})
	if err != nil {
		t.Fatalf("auth admin: %v", err)
	}
	guestAuth, err := authSvc.AuthenticateByName(ctx, "9guest", "pass", "", auth.ClientInfo{DeviceID: "guest-phone"})
	if err != nil {
		t.Fatalf("auth guest: %v", err)
	}

	adminSess := env.sessions.TouchSession(env.admin, auth.ClientInfo{DeviceID: "admin-tv"})
	guestSess := env.sessions.TouchSession(env.user2, auth.ClientInfo{DeviceID: "guest-phone"})

	fakeWS := &fakeWSSender{}
	env.handler.SetWSHub(fakeWS)

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

	// 1. Non-admin attempting remote control against admin's session -> 403 Forbidden and NO WS push
	blockedPaths := []struct {
		path string
		body string
	}{
		{"/Sessions/" + adminSess.Id + "/Viewing?ItemId=" + env.itemID + "&ItemType=Movie", ""},
		{"/Sessions/" + adminSess.Id + "/Playing?ItemIds=" + env.itemID + "&PlayCommand=PlayNow", `{}`},
		{"/Sessions/" + adminSess.Id + "/Playing/Pause", `{}`},
		{"/Sessions/" + adminSess.Id + "/System/Restart", ""},
		{"/Sessions/" + adminSess.Id + "/Command/GoHome", `{}`},
		{"/Sessions/" + adminSess.Id + "/Message?Header=Hi&Text=Pwned", `{}`},
	}
	for _, tc := range blockedPaths {
		rec := doAuthReq(guestAuth.AccessToken, http.MethodPost, tc.path, tc.body)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for guest controlling admin session on %s, got %d", tc.path, rec.Code)
		}
	}
	if len(fakeWS.sent) != 0 {
		t.Fatalf("expected 0 WS messages when unauthorized, got %d", len(fakeWS.sent))
	}

	// 2. Non-admin controlling own session -> 204 and WS messages pushed to guest-phone
	ownCases := []struct {
		path     string
		body     string
		wantType string
	}{
		{"/Sessions/" + guestSess.Id + "/Playing?ItemIds=" + env.itemID + "&PlayCommand=PlayNow", `{}`, "Play"},
		{"/Sessions/" + guestSess.Id + "/Playing/Pause", `{}`, "Playstate"},
		{"/Sessions/" + guestSess.Id + "/Command/GoHome", `{}`, "GeneralCommand"},
		{"/Sessions/" + guestSess.Id + "/Message?Header=Hi&Text=Self", `{}`, "GeneralCommand"},
		{"/Sessions/" + guestSess.Id + "/Viewing?ItemId=" + env.itemID + "&ItemType=Movie", ``, "GeneralCommand"},
		{"/Sessions/" + guestSess.Id + "/System/DisplayMessage", ``, "GeneralCommand"},
	}
	for i, tc := range ownCases {
		rec := doAuthReq(guestAuth.AccessToken, http.MethodPost, tc.path, tc.body)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected 204 for own session %s, got %d (%s)", tc.path, rec.Code, rec.Body.String())
		}
		if len(fakeWS.sent) != i+1 {
			t.Fatalf("expected %d WS messages, got %d", i+1, len(fakeWS.sent))
		}
		got := fakeWS.sent[i]
		if got.deviceID != "guest-phone" || got.msg.MessageType != tc.wantType {
			t.Fatalf("case %s: expected device=guest-phone type=%s, got device=%s type=%s", tc.path, tc.wantType, got.deviceID, got.msg.MessageType)
		}
	}

	// 3. Admin controlling guest's session -> 204 and WS message pushed to guest-phone
	before := len(fakeWS.sent)
	rec := doAuthReq(adminAuth.AccessToken, http.MethodPost, "/Sessions/"+guestSess.Id+"/Playing/Stop", `{}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for admin controlling guest session, got %d", rec.Code)
	}
	if len(fakeWS.sent) != before+1 || fakeWS.sent[before].deviceID != "guest-phone" || fakeWS.sent[before].msg.MessageType != "Playstate" {
		t.Fatalf("expected Playstate pushed to guest-phone, got %+v", fakeWS.sent[before:])
	}
}
