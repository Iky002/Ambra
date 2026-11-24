package compatcore

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/api/middleware"
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
	}
	for _, tc := range idorCases {
		res := doAuthReq(guestAuth.AccessToken, tc.method, tc.path, tc.body)
		if res.Code != http.StatusForbidden {
			t.Fatalf("IDOR %s %s: expected 403, got %d (%s)", tc.method, tc.path, res.Code, res.Body.String())
		}
	}

	// 4. Admin updating another user's Policy -> 204
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
