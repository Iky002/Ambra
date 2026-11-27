package system

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestSetupAdminNoDefaultTakeoverAndZeroUserBootstrapOnly(t *testing.T) {
	tmp := t.TempDir()
	cfg, err := config.Load(filepath.Join(tmp, "data"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.IsStartupWizardCompleted = false

	store, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	userRepo := sqlite.NewUserRepo(store)
	authSvc := auth.NewService(userRepo, cfg)
	h := NewHandler(cfg, userRepo, nil).WithSetupDeps(SetupDeps{Auth: authSvc})

	// 1. Fresh instance (0 users): anonymous SetupAdmin succeeds and creates the first administrator.
	freshReq := httptest.NewRequest(http.MethodPost, "/api/setup/admin", bytes.NewBufferString(`{"username":"owner","password":"strongpassword123"}`))
	freshReq.RemoteAddr = "192.168.1.50:23456"
	freshRec := httptest.NewRecorder()
	h.SetupAdmin(freshRec, freshReq)
	if freshRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for initial 0-user setup, got %d (%s)", freshRec.Code, freshRec.Body.String())
	}
	var authResp struct {
		AccessToken string `json:"AccessToken"`
	}
	if err := json.Unmarshal(freshRec.Body.Bytes(), &authResp); err != nil || authResp.AccessToken == "" {
		t.Fatalf("expected access token in setup response, got %s", freshRec.Body.String())
	}

	// 2. Once any user exists, anonymous SetupAdmin is ALWAYS rejected (public and LAN alike).
	pubReq := httptest.NewRequest(http.MethodPost, "/api/setup/admin", bytes.NewBufferString(`{"username":"hacker","password":"password123"}`))
	pubReq.RemoteAddr = "203.0.113.10:12345"
	pubRec := httptest.NewRecorder()
	h.SetupAdmin(pubRec, pubReq)
	if pubRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for public IP setup takeover, got %d (%s)", pubRec.Code, pubRec.Body.String())
	}

	lanReq := httptest.NewRequest(http.MethodPost, "/api/setup/admin", bytes.NewBufferString(`{"username":"owner2","password":"anotherpassword"}`))
	lanReq.RemoteAddr = "192.168.1.50:23457"
	lanRec := httptest.NewRecorder()
	h.SetupAdmin(lanRec, lanReq)
	if lanRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for unauthenticated LAN setup when users already exist, got %d (%s)", lanRec.Code, lanRec.Body.String())
	}

	// 3. Even if an instance has a legacy default admin/admin user, anonymous LAN takeover is completely removed.
	tmp2 := t.TempDir()
	cfg2, err := config.Load(filepath.Join(tmp2, "data"))
	if err != nil {
		t.Fatal(err)
	}
	cfg2.IsStartupWizardCompleted = false
	store2, err := sqlite.Open(cfg2.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store2.Close() })
	userRepo2 := sqlite.NewUserRepo(store2)
	authSvc2 := auth.NewService(userRepo2, cfg2)
	if _, err := authSvc2.CreateUser(context.Background(), "admin", "admin", true); err != nil {
		t.Fatal(err)
	}
	h2 := NewHandler(cfg2, userRepo2, nil).WithSetupDeps(SetupDeps{Auth: authSvc2})
	takeoverReq := httptest.NewRequest(http.MethodPost, "/api/setup/admin", bytes.NewBufferString(`{"username":"hijack","password":"newpassword123"}`))
	takeoverReq.RemoteAddr = "127.0.0.1:23458"
	takeoverRec := httptest.NewRecorder()
	h2.SetupAdmin(takeoverRec, takeoverReq)
	if takeoverRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when admin/admin exists (no default takeover backdoor), got %d (%s)", takeoverRec.Code, takeoverRec.Body.String())
	}
}
