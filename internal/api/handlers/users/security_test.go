package users

import (
	"context"
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
