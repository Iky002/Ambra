package users

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/api/ws"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestUsersV0103Hardening(t *testing.T) {
	tmp := t.TempDir()
	cfg, err := config.Load(filepath.Join(tmp, "data"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	userRepo := sqlite.NewUserRepo(store)
	libRepo := sqlite.NewLibraryRepo(store)
	authSvc := auth.NewService(userRepo, cfg)
	bus := eventbus.New()
	sessMgr := session.NewManager(userRepo, cfg, bus)
	wsHub := ws.NewHub(authSvc, sessMgr, bus)

	admin, err := authSvc.CreateUser(ctx, "admin", "adminpass123", true)
	if err != nil {
		t.Fatal(err)
	}
	viewer, err := authSvc.CreateUser(ctx, "viewer", "viewerpass123", false)
	if err != nil {
		t.Fatal(err)
	}
	adminAuth, err := authSvc.AuthenticateByName(ctx, "admin", "adminpass123", "", auth.ClientInfo{DeviceID: "d-adm"})
	if err != nil {
		t.Fatal(err)
	}
	viewerAuth, err := authSvc.AuthenticateByName(ctx, "viewer", "viewerpass123", "", auth.ClientInfo{DeviceID: "d-vw"})
	if err != nil {
		t.Fatal(err)
	}

	// Setup libraries: viewer allowed only in libAllowed, blocked from libSecret
	libAllowed, _ := libRepo.CreateLibrary(ctx, "Allowed", "movies", []string{filepath.Join(tmp, "allowed")})
	libSecret, _ := libRepo.CreateLibrary(ctx, "Secret", "movies", []string{filepath.Join(tmp, "secret")})
	allowedItem := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libAllowed.ID, ParentID: libAllowed.ID, Type: "Movie", MediaType: "Video", Name: "Safe", OfficialRating: "G"}
	secretItem := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libSecret.ID, ParentID: libSecret.ID, Type: "Movie", MediaType: "Video", Name: "Secret", OfficialRating: "R"}
	_ = libRepo.UpsertItem(ctx, allowedItem)
	_ = libRepo.UpsertItem(ctx, secretItem)

	vp := viewer.Policy
	vp.EnableAllFolders = false
	vp.EnabledFolders = []string{libAllowed.ID}
	if err := userRepo.UpdateUserPolicy(ctx, viewer.ID, vp); err != nil {
		t.Fatal(err)
	}
	authSvc.InvalidateUserCache(viewer.ID)

	h := NewHandler(authSvc, userRepo, sessMgr, wsHub, cfg)
	h.SetLibraryRepo(libRepo)

	r := chi.NewRouter()
	authMW := middleware.NewAuth(authSvc, sessMgr)
	RegisterRoutes(r, authMW, h)

	doReq := func(tok, method, path, body, remoteAddr string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if tok != "" {
			req.Header.Set("X-Emby-Token", tok)
		}
		if remoteAddr != "" {
			req.RemoteAddr = remoteAddr
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	// 1. Playback reporting enforces library RBAC
	for _, ep := range []string{"/Sessions/Playing", "/Sessions/Playing/Progress", "/Sessions/Playing/Stopped"} {
		rec := doReq(viewerAuth.AccessToken, http.MethodPost, ep, `{"ItemId":"`+secretItem.ID+`","PositionTicks":1000}`, "")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for viewer %s on secret item, got %d (%s)", ep, rec.Code, rec.Body.String())
		}
		rec = doReq(viewerAuth.AccessToken, http.MethodPost, ep, `{"ItemId":"`+allowedItem.ID+`","PositionTicks":1000}`, "")
		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected 204 for viewer %s on allowed item, got %d (%s)", ep, rec.Code, rec.Body.String())
		}
	}

	// 2. Last active admin cannot be demoted or disabled via UpdateUserPolicy
	demoteJSON := `{"IsAdministrator":false,"IsDisabled":false}`
	rec := doReq(adminAuth.AccessToken, http.MethodPost, "/Users/"+admin.ID+"/Policy", demoteJSON, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when demoting last admin, got %d (%s)", rec.Code, rec.Body.String())
	}
	disableJSON := `{"IsAdministrator":true,"IsDisabled":true}`
	rec = doReq(adminAuth.AccessToken, http.MethodPost, "/Users/"+admin.ID+"/Policy", disableJSON, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when disabling last admin, got %d (%s)", rec.Code, rec.Body.String())
	}

	// 3. Disabling viewer removes viewer sessions & revokes viewer tokens
	rec = doReq(adminAuth.AccessToken, http.MethodPost, "/Users/"+viewer.ID+"/Policy", `{"IsDisabled":true}`, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 disabling viewer, got %d (%s)", rec.Code, rec.Body.String())
	}
	for _, s := range sessMgr.ListSessions() {
		if s.UserId == viewer.ID {
			t.Fatalf("expected viewer session to be removed on disable, still found %+v", s)
		}
	}
	if _, err := userRepo.GetAccessToken(ctx, viewerAuth.AccessToken); err != sqlite.ErrNotFound {
		t.Fatalf("expected disabled user's tokens to be revoked, got err=%v", err)
	}

	// 4. Re-enable viewer, change password, verify tokens revoked & sessions removed
	_ = userRepo.UpdateUserPolicy(ctx, viewer.ID, dto.UserPolicy{EnableAllFolders: true})
	authSvc.InvalidateUserCache(viewer.ID)
	viewerAuth2, err := authSvc.AuthenticateByName(ctx, "viewer", "viewerpass123", "", auth.ClientInfo{DeviceID: "d-vw2"})
	if err != nil {
		t.Fatal(err)
	}
	_ = doReq(viewerAuth2.AccessToken, http.MethodGet, "/Users/"+viewer.ID, "", "")
	rec = doReq(adminAuth.AccessToken, http.MethodPost, "/Users/"+viewer.ID+"/Password", `{"NewPw":"newpass456"}`, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 on password update, got %d", rec.Code)
	}
	if _, err := userRepo.GetAccessToken(ctx, viewerAuth2.AccessToken); err != sqlite.ErrNotFound {
		t.Fatalf("expected tokens revoked after password change, got err=%v", err)
	}
	for _, s := range sessMgr.ListSessions() {
		if s.UserId == viewer.ID {
			t.Fatalf("expected viewer session to be removed on password change, still found %+v", s)
		}
	}

	// 5. AuthenticateByName rate limiting (>30 requests/min from same IP -> 429)
	ip := "198.51.100.88:5555"
	for i := 0; i < authByNameRateLimit; i++ {
		rec = doReq("", http.MethodPost, "/Users/AuthenticateByName", `{"Username":"nobody","Pw":"wrong"}`, ip)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d expected 401, got %d", i+1, rec.Code)
		}
	}
	rec = doReq("", http.MethodPost, "/Users/AuthenticateByName", `{"Username":"nobody","Pw":"wrong"}`, ip)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after rate limit exceeded, got %d", rec.Code)
	}
}

func TestAuthenticateByNameIPAggregateAndDeterministicCap(t *testing.T) {
	tmp := t.TempDir()
	st, err := sqlite.Open(filepath.Join(tmp, "users_spray.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	cfg := config.Default(tmp)
	userRepo := sqlite.NewUserRepo(st)
	authSvc := auth.NewService(userRepo, cfg)
	bus := eventbus.New()
	sessMgr := session.NewManager(userRepo, cfg, bus)
	wsHub := ws.NewHub(authSvc, sessMgr, bus)
	h := NewHandler(authSvc, userRepo, sessMgr, wsHub, cfg)

	r := chi.NewRouter()
	authMW := middleware.NewAuth(authSvc, sessMgr)
	RegisterRoutes(r, authMW, h)

	// 1. Single IP spraying arbitrary distinct usernames MUST hit 429 at authByNameIPRateLimit (60).
	sprayIP := "198.51.100.201:40000"
	for i := 0; i < authByNameIPRateLimit; i++ {
		body := fmt.Sprintf(`{"Username":"spray-%d","Pw":"wrong"}`, i)
		req := httptest.NewRequest(http.MethodPost, "/Users/AuthenticateByName", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = sprayIP
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("spray attempt %d expected 401, got %d", i+1, rec.Code)
		}
	}
	reqBlocked := httptest.NewRequest(http.MethodPost, "/Users/AuthenticateByName", strings.NewReader(`{"Username":"brand-new-user","Pw":"wrong"}`))
	reqBlocked.Header.Set("Content-Type", "application/json")
	reqBlocked.RemoteAddr = sprayIP
	recBlocked := httptest.NewRecorder()
	r.ServeHTTP(recBlocked, reqBlocked)
	if recBlocked.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after %d username-spray attempts from single IP, got %d", authByNameIPRateLimit, recBlocked.Code)
	}

	// 2. Bucket cap eviction without global DoS: filling authRates to authRateMaxBuckets MUST NOT evict an existing
	// ip|admin lockout bucket, and unseen legitimate (ip, user) pairs MUST NOT be blocked with 429!
	now := time.Now().UTC()
	hCap := NewHandler(authSvc, userRepo, sessMgr, wsHub, cfg)
	adminIP := "203.0.113.55"
	for i := 0; i < authByNameRateLimit; i++ {
		hCap.recordLoginFailure(adminIP, "admin", now)
	}
	if !hCap.isLoginRateLimited(adminIP, "admin", now) {
		t.Fatal("expected ip|admin to be locked out")
	}
	// 70 spraying IPs trying 59 usernames each MUST be capped to maxSubBucketsPerIP per IP
	// and MUST NOT block an unrelated legitimate user/IP!
	for ipIdx := 0; ipIdx < 70; ipIdx++ {
		atkIP := fmt.Sprintf("198.18.0.%d", ipIdx+1)
		for uIdx := 0; uIdx < authByNameIPRateLimit-1; uIdx++ {
			hCap.recordLoginFailure(atkIP, fmt.Sprintf("spray-user-%d", uIdx), now.Add(time.Millisecond))
		}
		if gotSub := countIPSubBucketsLocked(hCap.authRates, atkIP); gotSub > maxSubBucketsPerIP {
			t.Fatalf("expected <= %d composite sub-buckets for %s, got %d", maxSubBucketsPerIP, atkIP, gotSub)
		}
	}
	if hCap.isLoginRateLimited("203.0.113.249", "legit-after-70-sprayers", now.Add(time.Millisecond)) {
		t.Fatal("expected legitimate IP not to suffer global DoS after 70 username-spraying IPs")
	}
	// Fill remaining buckets beyond authRateMaxBuckets with distinct IPs/users.
	for i := 0; i < authRateMaxBuckets+50; i++ {
		ip := fmt.Sprintf("198.51.%d.%d", (i/250)%256, (i%250)+1)
		hCap.recordLoginFailure(ip, fmt.Sprintf("u%d", i), now.Add(2*time.Millisecond))
	}
	if len(hCap.authRates) > authRateMaxBuckets || len(hCap.authIPRates) > authRateMaxBuckets {
		t.Fatalf("expected rate maps bounded at %d, got authRates=%d authIPRates=%d", authRateMaxBuckets, len(hCap.authRates), len(hCap.authIPRates))
	}
	// Existing ip|admin lockout bucket MUST still be intact and rate-limited!
	if !hCap.isLoginRateLimited(adminIP, "admin", now.Add(3*time.Millisecond)) {
		t.Fatal("expected ip|admin lockout bucket to remain intact and never be evicted")
	}
	// Unseen legitimate (ip, user) MUST NOT fail closed with a global 429 DoS!
	if hCap.isLoginRateLimited("203.0.113.250", "freshuser", now.Add(3*time.Millisecond)) {
		t.Fatal("expected unseen legitimate (ip, user) not to be globally rate-limited when authRates is at cap")
	}

	// 3. Deterministic eviction in pruneRateBucketsLocked: if map exceeds cap, evicts oldest last-attempt first.
	over := make(map[string][]time.Time, authRateMaxBuckets+2)
	over["oldest-bucket"] = []time.Time{now.Add(-30 * time.Second)}
	over["recent-admin"] = []time.Time{now}
	for i := 0; i < authRateMaxBuckets; i++ {
		over[fmt.Sprintf("mid-%04d", i)] = []time.Time{now.Add(-10 * time.Second)}
	}
	pruneRateBucketsLocked(over, now.Add(-time.Minute))
	if len(over) != authRateMaxBuckets {
		t.Fatalf("expected len=%d after deterministic prune, got %d", authRateMaxBuckets, len(over))
	}
	if _, exists := over["oldest-bucket"]; exists {
		t.Fatal("expected oldest-bucket to be evicted deterministically")
	}
	if _, exists := over["recent-admin"]; !exists {
		t.Fatal("expected recent-admin bucket to be preserved")
	}
}
