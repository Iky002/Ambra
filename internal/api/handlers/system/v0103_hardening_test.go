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

func TestSetupAdminDefaultBootstrapLANOnlyAndNoTakeover(t *testing.T) {
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
	// Simulate EnsureDefaultAdmin creating default admin/admin
	if _, err := authSvc.CreateUser(context.Background(), "admin", "admin", true); err != nil {
		t.Fatal(err)
	}

	h := NewHandler(cfg, userRepo, nil).WithSetupDeps(SetupDeps{Auth: authSvc})

	// 1. Public IP attempting to hijack default admin/admin must get 403
	pubReq := httptest.NewRequest(http.MethodPost, "/api/setup/admin", bytes.NewBufferString(`{"username":"hacker","password":"password123"}`))
	pubReq.RemoteAddr = "203.0.113.10:12345"
	pubRec := httptest.NewRecorder()
	h.SetupAdmin(pubRec, pubReq)
	if pubRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for public IP setup takeover, got %d (%s)", pubRec.Code, pubRec.Body.String())
	}

	// 2. LAN IP with unchanged default admin/admin succeeds and updates username/password
	lanReq := httptest.NewRequest(http.MethodPost, "/api/setup/admin", bytes.NewBufferString(`{"username":"owner","password":"strongpassword123"}`))
	lanReq.RemoteAddr = "192.168.1.50:23456"
	lanRec := httptest.NewRecorder()
	h.SetupAdmin(lanRec, lanReq)
	if lanRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for LAN default-admin setup bootstrap, got %d (%s)", lanRec.Code, lanRec.Body.String())
	}
	var authResp struct {
		AccessToken string `json:"AccessToken"`
	}
	if err := json.Unmarshal(lanRec.Body.Bytes(), &authResp); err != nil || authResp.AccessToken == "" {
		t.Fatalf("expected access token in setup response, got %s", lanRec.Body.String())
	}

	// 3. Once password is changed, even LAN IP without admin auth gets 403
	lanReq2 := httptest.NewRequest(http.MethodPost, "/api/setup/admin", bytes.NewBufferString(`{"username":"owner2","password":"anotherpassword"}`))
	lanReq2.RemoteAddr = "192.168.1.50:23457"
	lanRec2 := httptest.NewRecorder()
	h.SetupAdmin(lanRec2, lanReq2)
	if lanRec2.Code != http.StatusForbidden {
		t.Fatalf("expected 403 after default admin password was changed, got %d (%s)", lanRec2.Code, lanRec2.Body.String())
	}
}
