package compatcore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/pkg/netutil"
	"github.com/ambra-media/ambra/internal/store/sqlite"
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

func TestUserAvatarRemoteAccessAndHiddenDisabledIsolation(t *testing.T) {
	netutil.SetLocalNetworkSubnets([]string{"10.0.0.0/8"})
	t.Cleanup(func() { netutil.SetLocalNetworkSubnets(nil) })

	env := newTestEnv(t)
	ctx := context.Background()
	authSvc := auth.NewService(env.userRepo, env.cfg)
	env.handler.SetAuthService(authSvc)
	authMW := middleware.NewAuth(authSvc, env.sessions)

	hiddenPol := sqlite.DefaultUserPolicy(false)
	hiddenPol.IsHidden = true
	hiddenUser, err := authSvc.CreateUser(ctx, "hidden_bob", "pass123", false)
	if err != nil {
		t.Fatal(err)
	}
	_ = env.userRepo.UpdateUserPolicy(ctx, hiddenUser.ID, hiddenPol)
	authSvc.InvalidateUserCache(hiddenUser.ID)

	disabledUser, err := authSvc.CreateUser(ctx, "disabled_carol", "pass123", false)
	if err != nil {
		t.Fatal(err)
	}

	adminAuth, err := authSvc.AuthenticateByName(ctx, "admin", "admin", "", auth.ClientInfo{DeviceID: "adm-dev"})
	if err != nil {
		t.Fatal(err)
	}
	guestAuth, err := authSvc.AuthenticateByName(ctx, "9guest", "pass", "", auth.ClientInfo{DeviceID: "guest-dev"})
	if err != nil {
		t.Fatal(err)
	}
	hiddenAuth, err := authSvc.AuthenticateByName(ctx, "hidden_bob", "pass123", "", auth.ClientInfo{DeviceID: "hid-dev"})
	if err != nil {
		t.Fatal(err)
	}

	router := authMW.ParseAuth(env.handler.BuildRouter(AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
		UserIDFromCtx: func(c context.Context) string {
			if ac, ok := middleware.AuthFromContext(c); ok && ac != nil {
				return ac.UserID()
			}
			return ""
		},
	}))

	callImg := func(token, method, path, body, remoteAddr string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("X-Emby-Token", token)
		}
		if remoteAddr != "" {
			req.RemoteAddr = remoteAddr
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	b64Img := base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\navatarbytes"))
	for _, uid := range []string{env.user2.ID, hiddenUser.ID, disabledUser.ID} {
		if r := callImg(adminAuth.AccessToken, http.MethodPost, "/Users/"+uid+"/Images/Primary", b64Img, "10.0.0.5:1234"); r.Code != http.StatusNoContent {
			t.Fatalf("upload avatar for %s failed: %d", uid, r.Code)
		}
	}
	disPol := sqlite.DefaultUserPolicy(false)
	disPol.IsDisabled = true
	_ = env.userRepo.UpdateUserPolicy(ctx, disabledUser.ID, disPol)
	authSvc.InvalidateUserCache(disabledUser.ID)

	// 1. Authenticated non-admin requesting another hidden or disabled user's avatar -> 404 Not Found!
	if r := callImg(guestAuth.AccessToken, http.MethodGet, "/Users/"+hiddenUser.ID+"/Images/Primary", "", "10.0.0.5:1234"); r.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when non-admin requests IsHidden user's avatar, got %d", r.Code)
	}
	if r := callImg(guestAuth.AccessToken, http.MethodGet, "/Users/"+disabledUser.ID+"/Images/Primary", "", "10.0.0.5:1234"); r.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when non-admin requests IsDisabled user's avatar, got %d", r.Code)
	}
	// Hidden user CAN view their own avatar, and admin CAN view hidden/disabled avatars!
	if r := callImg(hiddenAuth.AccessToken, http.MethodGet, "/Users/"+hiddenUser.ID+"/Images/Primary", "", "10.0.0.5:1234"); r.Code != http.StatusOK {
		t.Fatalf("expected 200 when hidden user requests own avatar, got %d", r.Code)
	}
	if r := callImg(adminAuth.AccessToken, http.MethodGet, "/Users/"+hiddenUser.ID+"/Images/Primary", "", "10.0.0.5:1234"); r.Code != http.StatusOK {
		t.Fatalf("expected 200 when admin requests hidden user's avatar, got %d", r.Code)
	}
	if r := callImg(adminAuth.AccessToken, http.MethodGet, "/Users/"+disabledUser.ID+"/Images/Primary", "", "10.0.0.5:1234"); r.Code != http.StatusOK {
		t.Fatalf("expected 200 when admin requests disabled user's avatar, got %d", r.Code)
	}

	// 2. Per-user EnableRemoteAccess=false blocks WAN avatar access (isRequestAuthenticated=false, serveUserImage=403), while LAN works!
	lanOnlyPol := env.user2.Policy
	lanOnlyPol.EnableRemoteAccess = false
	_ = env.userRepo.UpdateUserPolicy(ctx, env.user2.ID, lanOnlyPol)
	authSvc.InvalidateUserCache(env.user2.ID)
	reloadedUser2, _ := env.userRepo.GetUserByID(ctx, env.user2.ID)

	wanReq := httptest.NewRequest(http.MethodGet, "/Users/"+env.user2.ID+"/Images/Primary", nil)
	wanReq.RemoteAddr = "198.51.100.77:5555"
	wanReq = wanReq.WithContext(middleware.WithAuthContext(wanReq.Context(), &middleware.AuthContext{User: reloadedUser2}))
	if env.handler.isRequestAuthenticated(wanReq) {
		t.Fatal("expected isRequestAuthenticated=false on WAN when user EnableRemoteAccess=false")
	}
	if r := callImg(guestAuth.AccessToken, http.MethodGet, "/Users/"+env.user2.ID+"/Images/Primary", "", "198.51.100.77:5555"); r.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on WAN avatar request when user EnableRemoteAccess=false, got %d", r.Code)
	}
	if r := callImg(guestAuth.AccessToken, http.MethodGet, "/Users/"+env.user2.ID+"/Images/Primary", "", "10.0.0.5:5555"); r.Code != http.StatusOK {
		t.Fatalf("expected 200 on LAN avatar request when user EnableRemoteAccess=false, got %d", r.Code)
	}

	// 3. Global EnableRemoteAccessGlobal=false blocks WAN user-token avatar requests while API keys remain allowed!
	f := false
	env.cfg.Update(func(c *config.Config) {
		c.EnableRemoteAccessGlobal = &f
	})
	if r := callImg(adminAuth.AccessToken, http.MethodGet, "/Users/"+env.user2.ID+"/Images/Primary", "", "198.51.100.77:5555"); r.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on WAN avatar request when global remote access disabled, got %d", r.Code)
	}
	apiKey, err := authSvc.CreateAPIKey(ctx, "Monitor")
	if err != nil {
		t.Fatal(err)
	}
	if r := callImg(apiKey.Token, http.MethodGet, "/Users/"+hiddenUser.ID+"/Images/Primary", "", "198.51.100.77:5555"); r.Code != http.StatusOK {
		t.Fatalf("expected 200 on WAN API-key avatar request even when global remote access disabled, got %d", r.Code)
	}
}

func TestAuthenticateByIDCompositeBucketEvictionNoGlobalDoS(t *testing.T) {
	env := newTestEnv(t)
	now := time.Now().UTC()

	victimIP := "203.0.113.10"
	for i := 0; i < authByIDRateLimit; i++ {
		env.handler.recordAuthByIDFailure(victimIP, env.admin.ID, now)
	}
	if !env.handler.isAuthByIDRateLimited(victimIP, env.admin.ID, now) {
		t.Fatal("expected victimIP|admin to be rate-limited")
	}

	// 75 spraying IPs trying 59 distinct UIDs each MUST cap composite sub-buckets per IP to maxSubBucketsPerIP
	// and MUST NOT globally DoS unrelated legitimate IPs!
	for ipIdx := 0; ipIdx < 75; ipIdx++ {
		atkIP := fmt.Sprintf("198.18.1.%d", ipIdx+1)
		for uIdx := 0; uIdx < authByIDIPRateLimit-1; uIdx++ {
			env.handler.recordAuthByIDFailure(atkIP, fmt.Sprintf("uid-%d", uIdx), now.Add(time.Millisecond))
		}
		if got := countIPSubBucketsLocked(env.handler.authByIDRates, atkIP); got > maxSubBucketsPerIP {
			t.Fatalf("expected <= %d composite sub-buckets for %s, got %d", maxSubBucketsPerIP, atkIP, got)
		}
	}

	// Fill beyond maxRateLimitBuckets across distinct IPs.
	for i := 0; i < maxRateLimitBuckets+50; i++ {
		ip := fmt.Sprintf("198.51.%d.%d", (i/250)%256, (i%250)+1)
		env.handler.recordAuthByIDFailure(ip, fmt.Sprintf("u-%d", i), now.Add(2*time.Millisecond))
	}
	if len(env.handler.authByIDRates) > maxRateLimitBuckets || len(env.handler.authByIDIPRates) > maxRateLimitBuckets {
		t.Fatalf("expected rate buckets capped at %d, got comp=%d ip=%d",
			maxRateLimitBuckets, len(env.handler.authByIDRates), len(env.handler.authByIDIPRates))
	}
	if !env.handler.isAuthByIDRateLimited(victimIP, env.admin.ID, now.Add(3*time.Millisecond)) {
		t.Fatal("expected active victimIP|admin lockout to remain intact")
	}
	if env.handler.isAuthByIDRateLimited("203.0.113.250", env.user2.ID, now.Add(3*time.Millisecond)) {
		t.Fatal("expected unseen legitimate IP not to suffer global 429 DoS")
	}
}
