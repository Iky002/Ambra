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
	"github.com/ambra-media/ambra/internal/api/ws"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestUsersV0104SecurityRemediations(t *testing.T) {
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
	alice, err := authSvc.CreateUser(ctx, "alice", "alicepass1", false)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := authSvc.CreateUser(ctx, "bob", "bobpass123", false)
	if err != nil {
		t.Fatal(err)
	}

	// Give admin sensitive policy settings to verify /Users/Public strips them
	maxR := 7
	adminPol := admin.Policy
	adminPol.IsAdministrator = true
	adminPol.EnabledFolders = []string{"lib-secret-1"}
	adminPol.BlockedTags = []string{"NSFW"}
	adminPol.MaxParentalRating = &maxR
	if err := userRepo.UpdateUserPolicySafe(ctx, admin.ID, adminPol); err != nil {
		t.Fatal(err)
	}

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

	// 1. GET /Users/Public returns [] by default (EnableLoginUserList=false), and strips Policy & Configuration when enabled
	rec := doReq("", http.MethodGet, "/Users/Public", "", "")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("GET /Users/Public default expected 200 [], got %d %s", rec.Code, rec.Body.String())
	}
	cfg.Update(func(c *config.Config) {
		c.EnableLoginUserList = true
	})
	rec = doReq("", http.MethodGet, "/Users/Public", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /Users/Public got %d", rec.Code)
	}
	var pubUsers []dto.UserDto
	if err := json.Unmarshal(rec.Body.Bytes(), &pubUsers); err != nil {
		t.Fatal(err)
	}
	if len(pubUsers) != 3 {
		t.Fatalf("expected 3 public users, got %d", len(pubUsers))
	}
	for _, pu := range pubUsers {
		if pu.Policy.IsAdministrator || len(pu.Policy.EnabledFolders) > 0 || len(pu.Policy.BlockedTags) > 0 || pu.Policy.MaxParentalRating != nil {
			t.Fatalf("sensitive policy leaked on /Users/Public for %s: %+v", pu.Name, pu.Policy)
		}
		if pu.Id == "" || pu.Name == "" || !pu.HasPassword {
			t.Fatalf("basic login metadata missing on /Users/Public: %+v", pu)
		}
	}

	// 2. [R-03.4] Self password change keeps current token & session while revoking other devices
	aliceDev1, err := authSvc.AuthenticateByName(ctx, "alice", "alicepass1", "", auth.ClientInfo{DeviceID: "alice-phone"})
	if err != nil {
		t.Fatal(err)
	}
	aliceDev2, err := authSvc.AuthenticateByName(ctx, "alice", "alicepass1", "", auth.ClientInfo{DeviceID: "alice-tv"})
	if err != nil {
		t.Fatal(err)
	}
	sessMgr.TouchSession(alice, auth.ClientInfo{DeviceID: "alice-phone"})
	sessMgr.TouchSession(alice, auth.ClientInfo{DeviceID: "alice-tv"})

	rec = doReq(aliceDev1.AccessToken, http.MethodPost, "/Users/"+alice.ID+"/Password", `{"CurrentPw":"alicepass1","NewPw":"alicepass2"}`, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("self password change expected 204, got %d (%s)", rec.Code, rec.Body.String())
	}
	// Current device token MUST remain valid (no immediate 401!)
	if recCheck := doReq(aliceDev1.AccessToken, http.MethodGet, "/Users/"+alice.ID, "", ""); recCheck.Code != http.StatusOK {
		t.Fatalf("expected current token to remain valid after self password change, got %d", recCheck.Code)
	}
	// Other device token MUST be revoked immediately
	if _, err := authSvc.ValidateToken(ctx, aliceDev2.AccessToken); err == nil {
		t.Fatal("expected other device token to be revoked after self password change")
	}
	// Other device session removed, current device session preserved
	if _, ok := sessMgr.GetSessionByUserDevice(alice.ID, "alice-tv"); ok {
		t.Fatal("expected other device session to be removed")
	}
	if _, ok := sessMgr.GetSessionByUserDevice(alice.ID, "alice-phone"); !ok {
		t.Fatal("expected current device session to remain active")
	}

	// 3. Admin resetting another user's password revokes ALL of that user's tokens & sessions
	adminAuth, err := authSvc.AuthenticateByName(ctx, "admin", "adminpass123", "", auth.ClientInfo{DeviceID: "admin-pc"})
	if err != nil {
		t.Fatal(err)
	}
	rec = doReq(adminAuth.AccessToken, http.MethodPost, "/Users/"+alice.ID+"/Password", `{"NewPw":"alicepass3"}`, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("admin password reset expected 204, got %d", rec.Code)
	}
	if _, err := authSvc.ValidateToken(ctx, aliceDev1.AccessToken); err == nil {
		t.Fatal("expected alice's token to be revoked when admin resets her password")
	}
	if _, ok := sessMgr.GetSessionByUserDevice(alice.ID, "alice-phone"); ok {
		t.Fatal("expected all alice sessions removed when admin resets her password")
	}

	// 4. [MH-05] Non-admin ListSessions sanitizes NowPlayingItem.Path
	lib, _ := libRepo.CreateLibrary(ctx, "Movies", "movies", []string{filepath.Join(tmp, "movies")})
	movie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "SecretPathMovie", Path: "/srv/media/secret/movie.mkv",
	}
	_ = libRepo.UpsertItem(ctx, movie)
	sessMgr.SetLibraryRepo(libRepo)
	bobAuth, err := authSvc.AuthenticateByName(ctx, "bob", "bobpass123", "", auth.ClientInfo{DeviceID: "shared-dev"})
	if err != nil {
		t.Fatal(err)
	}
	_ = sessMgr.ReportPlaybackStart(ctx, bob, auth.ClientInfo{DeviceID: "shared-dev"}, session.PlaybackStartRequest{ItemId: movie.ID})
	rec = doReq(bobAuth.AccessToken, http.MethodGet, "/Sessions", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /Sessions got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "/srv/media/secret/movie.mkv") {
		t.Fatalf("non-admin /Sessions leaked server filesystem path: %s", rec.Body.String())
	}

	// 5. [H-01] Non-admin DeleteDevice (direct call) only revokes caller's (userID, deviceID), not admin's same deviceID
	adminSharedAuth, err := authSvc.AuthenticateByName(ctx, "admin", "adminpass123", "", auth.ClientInfo{DeviceID: "shared-dev"})
	if err != nil {
		t.Fatal(err)
	}
	sessMgr.TouchSession(admin, auth.ClientInfo{DeviceID: "shared-dev"})
	bobCtx := middleware.WithAuthContext(ctx, &middleware.AuthContext{User: bob, Token: bobAuth.AccessToken})
	delReq := httptest.NewRequest(http.MethodDelete, "/Devices?Id=shared-dev", nil).WithContext(bobCtx)
	delRec := httptest.NewRecorder()
	h.DeleteDevice(delRec, delReq)
	if delRec.Code != http.StatusNoContent {
		t.Fatalf("non-admin DeleteDevice got %d", delRec.Code)
	}
	if _, err := authSvc.ValidateToken(ctx, bobAuth.AccessToken); err == nil {
		t.Fatal("expected bob's token for shared-dev to be revoked")
	}
	if _, err := authSvc.ValidateToken(ctx, adminSharedAuth.AccessToken); err != nil {
		t.Fatalf("expected admin's token for shared-dev to remain valid, got %v", err)
	}
	if _, ok := sessMgr.GetSessionByUserDevice(admin.ID, "shared-dev"); !ok {
		t.Fatal("expected admin's session on shared-dev to remain intact")
	}

	// 6. [M-03] Login rate limiter uses composite (ClientIP, NormalizedUser) on failed attempts only,
	// with loopback exemption so external traffic cannot lock out local administrator logins.
	attackerIP := "203.0.113.200:1234"
	for i := 0; i < authByNameRateLimit; i++ {
		rec = doReq("", http.MethodPost, "/Users/AuthenticateByName", `{"Username":"admin","Pw":"bad"}`, attackerIP)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attacker attempt %d expected 401, got %d", i+1, rec.Code)
		}
	}
	// Next attempt for same (attackerIP, normalizedUser) MUST hit 429
	rec = doReq("", http.MethodPost, "/Users/AuthenticateByName", `{"Username":"  ADMIN  ","Pw":"bad"}`, attackerIP)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 for repeated failed attempts on (ClientIP, NormalizedUser), got %d", rec.Code)
	}
	// Meanwhile, local loopback admin login MUST NOT be locked out by the external attacker!
	rec = doReq("", http.MethodPost, "/Users/AuthenticateByName", `{"Username":"admin","Pw":"adminpass123"}`, "127.0.0.1:9999")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected loopback admin login to remain unaffected (200), got %d (%s)", rec.Code, rec.Body.String())
	}
	// Legitimate external login from a different IP also succeeds and does not accumulate failure counters
	for i := 0; i < 5; i++ {
		rec = doReq("", http.MethodPost, "/Users/AuthenticateByName", `{"Username":"admin","Pw":"adminpass123"}`, "198.51.100.250:9999")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected valid login from clean IP to succeed, got %d", rec.Code)
		}
	}

	// 7. [H-03] DeleteUserSafe protects last active admin via API key
	apiKey, err := authSvc.CreateAPIKey(ctx, "AdminKey")
	if err != nil {
		t.Fatal(err)
	}
	rec = doReq(apiKey.Token, http.MethodDelete, "/Users/"+admin.ID, "", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 deleting last admin via API key, got %d (%s)", rec.Code, rec.Body.String())
	}
}
