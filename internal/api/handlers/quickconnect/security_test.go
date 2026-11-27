package quickconnect

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestQuickConnectV0103Hardening(t *testing.T) {
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

	userRepo := sqlite.NewUserRepo(store)
	authSvc := auth.NewService(userRepo, cfg)
	user, err := authSvc.CreateUser(context.Background(), "alice", "alicepass", false)
	if err != nil {
		t.Fatal(err)
	}

	h := New(authSvc, userRepo, nil)
	h.SetConfig(cfg)

	// 1. Config flag Disable -> Enabled=false and Initiate returns 403
	cfg.EnableQuickConnect = false
	rec := httptest.NewRecorder()
	h.handleEnabled(rec, httptest.NewRequest(http.MethodGet, "/QuickConnect/Enabled", nil))
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "false" {
		t.Fatalf("expected false when disabled, got %d %q", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.handleInitiate(rec, httptest.NewRequest(http.MethodPost, "/QuickConnect/Initiate", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on Initiate when disabled, got %d", rec.Code)
	}

	// 2. Re-enable -> Initiate + API Key admin Authorize with explicit userId
	cfg.EnableQuickConnect = true
	initReq := httptest.NewRequest(http.MethodPost, "/QuickConnect/Initiate", nil)
	initReq.RemoteAddr = "198.51.100.1:1111"
	rec = httptest.NewRecorder()
	h.handleInitiate(rec, initReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on Initiate, got %d", rec.Code)
	}
	var state QuickConnectResult
	if err := json.Unmarshal(rec.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}

	apiKeyAC := &middleware.AuthContext{IsAPIKey: true, APIKeyName: "automation-key"}
	// Missing userId -> 400
	badAuthReq := httptest.NewRequest(http.MethodPost, "/QuickConnect/Authorize?code="+state.Code, nil)
	badAuthReq = badAuthReq.WithContext(middleware.WithAuthContext(badAuthReq.Context(), apiKeyAC))
	rec = httptest.NewRecorder()
	h.handleAuthorize(rec, badAuthReq)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when API key authorizes without userId, got %d", rec.Code)
	}

	// Explicit userId -> 200
	okAuthReq := httptest.NewRequest(http.MethodPost, "/QuickConnect/Authorize?code="+state.Code+"&userId="+user.ID, nil)
	okAuthReq = okAuthReq.WithContext(middleware.WithAuthContext(okAuthReq.Context(), apiKeyAC))
	rec = httptest.NewRecorder()
	h.handleAuthorize(rec, okAuthReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 when API key authorizes with userId, got %d (%s)", rec.Code, rec.Body.String())
	}

	// 3. Per-IP pending cap (20): advance fake clock across rate windows (< 10m TTL) -> 21st returns 429
	baseTime := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	curTime := baseTime
	h.now = func() time.Time { return curTime }
	floodIP := "198.51.100.99:2222"
	for i := 0; i < maxPendingPerIP; i++ {
		if i > 0 && i%maxInitiatesPerIPPerMin == 0 {
			curTime = curTime.Add(2 * time.Minute)
		}
		req := httptest.NewRequest(http.MethodPost, "/QuickConnect/Initiate", nil)
		req.RemoteAddr = floodIP
		rec = httptest.NewRecorder()
		h.handleInitiate(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("pending session %d expected 200, got %d (%s)", i+1, rec.Code, rec.Body.String())
		}
	}
	curTime = curTime.Add(2 * time.Minute)
	req21 := httptest.NewRequest(http.MethodPost, "/QuickConnect/Initiate", nil)
	req21.RemoteAddr = floodIP
	rec = httptest.NewRecorder()
	h.handleInitiate(rec, req21)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on 21st pending session from same IP, got %d", rec.Code)
	}
}
