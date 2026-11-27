package users

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestV0109Audit_RemoteAccessEnforcementAndCacheControl(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "users_v0109.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)
	authSvc := auth.NewService(userRepo, cfg)
	sessMgr := session.NewManager(userRepo, cfg, nil)
	authMW := middleware.NewAuth(authSvc, sessMgr)

	admin, err := authSvc.CreateUser(ctx, "admin", "adminpass", true)
	if err != nil {
		t.Fatal(err)
	}
	lanOnly, err := authSvc.CreateUser(ctx, "lanonly", "secret123", false)
	if err != nil {
		t.Fatal(err)
	}
	pol := sqlite.DefaultUserPolicy(false)
	pol.EnableRemoteAccess = false
	if err := userRepo.UpdateUserPolicy(ctx, lanOnly.ID, pol); err != nil {
		t.Fatal(err)
	}

	h := NewHandler(authSvc, userRepo, sessMgr, nil, cfg)
	r := chi.NewRouter()
	RegisterRoutes(r, authMW, h)

	// 1. [M-11] Wrong password from public IP -> 401 (no username/policy enumeration)
	reqWrong := httptest.NewRequest(http.MethodPost, "/Users/AuthenticateByName", bytes.NewBufferString(`{"Username":"lanonly","Pw":"wrong"}`))
	reqWrong.RemoteAddr = "203.0.113.10:12345"
	recWrong := httptest.NewRecorder()
	r.ServeHTTP(recWrong, reqWrong)
	if recWrong.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong password on remote-disabled user, got %d", recWrong.Code)
	}

	// 2. [M-02] Correct password from public IP for per-user remote-disabled user -> uniform 401 (no password oracle) AND no token minted
	reqRemote := httptest.NewRequest(http.MethodPost, "/Users/AuthenticateByName", bytes.NewBufferString(`{"Username":"lanonly","Pw":"secret123"}`))
	reqRemote.RemoteAddr = "203.0.113.10:12345"
	recRemote := httptest.NewRecorder()
	r.ServeHTTP(recRemote, reqRemote)
	if recRemote.Code != http.StatusUnauthorized {
		t.Fatalf("expected uniform 401 for remote-disabled user from public IP, got %d body=%s", recRemote.Code, recRemote.Body.String())
	}
	if cc := recRemote.Header().Get("Cache-Control"); cc != "private, no-store" {
		t.Fatalf("expected Cache-Control=private, no-store, got %q", cc)
	}
	var tokenCount int
	if err := store.Reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM access_tokens WHERE user_id = ?`, lanOnly.ID).Scan(&tokenCount); err != nil {
		t.Fatal(err)
	}
	if tokenCount != 0 {
		t.Fatalf("expected 0 tokens minted for rejected remote login, got %d", tokenCount)
	}

	// 3. [M-11] Correct password from LAN IP -> 200 OK + Cache-Control: private, no-store
	reqLAN := httptest.NewRequest(http.MethodPost, "/Users/AuthenticateByName", bytes.NewBufferString(`{"Username":"lanonly","Pw":"secret123"}`))
	reqLAN.RemoteAddr = "192.168.1.50:54321"
	recLAN := httptest.NewRecorder()
	r.ServeHTTP(recLAN, reqLAN)
	if recLAN.Code != http.StatusOK {
		t.Fatalf("expected 200 for remote-disabled user from LAN IP, got %d body=%s", recLAN.Code, recLAN.Body.String())
	}
	if cc := recLAN.Header().Get("Cache-Control"); cc != "private, no-store" {
		t.Fatalf("expected Cache-Control=private, no-store on login, got %q", cc)
	}

	// 4. [M-14] GET /Auth/Keys and POST /Auth/Keys set Cache-Control: private, no-store
	adminAuth, err := authSvc.AuthenticateByName(ctx, admin.Name, "adminpass", "", auth.ClientInfo{DeviceID: "d1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPost, "/Auth/Keys?App=TestApp", ""},
		{http.MethodGet, "/Auth/Keys", ""},
	} {
		req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
		req.Header.Set("X-Emby-Token", adminAuth.AccessToken)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s expected 200, got %d", tc.method, tc.path, rec.Code)
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "private, no-store" {
			t.Fatalf("%s %s expected Cache-Control=private, no-store, got %q", tc.method, tc.path, cc)
		}
	}
}
