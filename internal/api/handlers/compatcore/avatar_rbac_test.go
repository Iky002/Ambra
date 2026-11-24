package compatcore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/core/auth"
)

func TestUserAvatarRBACAndPrimaryImageTag(t *testing.T) {
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

	b64Img := base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\nfakeavatar"))

	// 1. Non-admin modifying another user's avatar -> 403 Forbidden
	rec := doAuthReq(guestAuth.AccessToken, http.MethodPost, "/Users/"+env.admin.ID+"/Images/Primary", b64Img)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin POST other user avatar, got %d (%s)", rec.Code, rec.Body.String())
	}
	rec = doAuthReq(guestAuth.AccessToken, http.MethodDelete, "/Users/"+env.admin.ID+"/Images/Primary", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin DELETE other user avatar, got %d", rec.Code)
	}

	// 2. Non-admin modifying own avatar -> 204 No Content
	rec = doAuthReq(guestAuth.AccessToken, http.MethodPost, "/Users/"+env.user2.ID+"/Images/Primary", b64Img)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for self POST avatar, got %d (%s)", rec.Code, rec.Body.String())
	}

	// 3. QueryUsers returns PrimaryImageTag for user2 and empty for admin
	rec = doAuthReq(adminAuth.AccessToken, http.MethodGet, "/Users/Query", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /Users/Query: %d", rec.Code)
	}
	var qr dto.QueryResult[dto.UserDto]
	if err := json.Unmarshal(rec.Body.Bytes(), &qr); err != nil {
		t.Fatalf("unmarshal QueryUsers: %v", err)
	}
	foundUser2 := false
	for _, u := range qr.Items {
		if u.Id == env.user2.ID {
			foundUser2 = true
			if u.PrimaryImageTag == "" {
				t.Fatal("expected PrimaryImageTag to be populated for user2 after avatar upload")
			}
		}
		if u.Id == env.admin.ID && u.PrimaryImageTag != "" {
			t.Fatalf("expected empty PrimaryImageTag for admin without avatar, got %q", u.PrimaryImageTag)
		}
	}
	if !foundUser2 {
		t.Fatal("user2 not found in /Users/Query")
	}

	// 4. Admin modifying and deleting another user's avatar -> 204 No Content
	rec = doAuthReq(adminAuth.AccessToken, http.MethodPost, "/Users/"+env.user2.ID+"/Images/Primary", b64Img)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for admin POST other user avatar, got %d", rec.Code)
	}
	rec = doAuthReq(adminAuth.AccessToken, http.MethodDelete, "/Users/"+env.user2.ID+"/Images/Primary", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for admin DELETE other user avatar, got %d", rec.Code)
	}

	// 5. Fallback router without AuthContext or UserIDFromCtx -> 403 Forbidden
	bareRouter := env.handler.BuildRouter(AuthFuncs{})
	bareReq := httptest.NewRequest(http.MethodPost, "/Users/"+env.user2.ID+"/Images/Primary", strings.NewReader(b64Img))
	bareRec := httptest.NewRecorder()
	bareRouter.ServeHTTP(bareRec, bareReq)
	if bareRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when no auth context or UserIDFromCtx, got %d", bareRec.Code)
	}
}
