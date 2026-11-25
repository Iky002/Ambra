package quickconnect

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
)

func TestQuickConnectDisabledBlocksConnectAndAllEndpoints(t *testing.T) {
	h, mux, authSvc, _, _ := setupQuickConnectTest(t)

	aliceAuth, err := authSvc.AuthenticateByName(context.Background(), "alice", "secret123", "", auth.ClientInfo{DeviceID: "qc-dev"})
	if err != nil {
		t.Fatal(err)
	}

	// Default with nil cfg -> enabled
	if !h.isEnabled() {
		t.Fatal("expected enabled when cfg is nil")
	}

	cfg := config.Default(t.TempDir())
	cfg.EnableQuickConnect = true
	h.SetConfig(cfg)

	// 1. Initiate while enabled succeeds
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/QuickConnect/Initiate", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("Initiate when enabled got %d: %s", rec.Code, rec.Body.String())
	}
	var initRes QuickConnectResult
	if err := json.Unmarshal(rec.Body.Bytes(), &initRes); err != nil {
		t.Fatal(err)
	}

	// Connect succeeds while enabled
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/QuickConnect/Connect?secret="+initRes.Secret, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("Connect when enabled got %d", rec.Code)
	}

	// 2. Disable QuickConnect dynamically via cfg.Update
	cfg.Update(func(c *config.Config) {
		c.EnableQuickConnect = false
	})

	// GET /QuickConnect/Enabled returns false
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/QuickConnect/Enabled", nil))
	if rec.Code != http.StatusOK || string(bytes.TrimSpace(rec.Body.Bytes())) != "false" {
		t.Fatalf("Enabled when disabled got %d %q", rec.Code, rec.Body.String())
	}

	// GET /QuickConnect/Connect MUST return 403 when disabled
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/QuickConnect/Connect?secret="+initRes.Secret, nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("Connect when disabled expected 403, got %d (%s)", rec.Code, rec.Body.String())
	}

	// POST /QuickConnect/Initiate MUST return 403 when disabled
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/QuickConnect/Initiate", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("Initiate when disabled expected 403, got %d", rec.Code)
	}

	// POST /QuickConnect/Authorize MUST return 403 when disabled
	authReq := httptest.NewRequest(http.MethodPost, "/QuickConnect/Authorize?code="+initRes.Code, nil)
	authReq.Header.Set("X-Emby-Token", aliceAuth.AccessToken)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, authReq)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("Authorize when disabled expected 403, got %d (%s)", rec.Code, rec.Body.String())
	}

	// POST /Users/AuthenticateWithQuickConnect MUST return 403 when disabled
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Users/AuthenticateWithQuickConnect", bytes.NewBufferString(`{"Secret":"`+initRes.Secret+`"}`)))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("Authenticate when disabled expected 403, got %d", rec.Code)
	}

	// Re-enable and verify Connect with missing secret -> 404, and Authenticate with bad JSON / empty secret -> 400
	cfg.Update(func(c *config.Config) {
		c.EnableQuickConnect = true
	})
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/QuickConnect/Connect", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("Connect with missing secret expected 404, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Users/AuthenticateWithQuickConnect", bytes.NewBufferString(`{bad`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Authenticate bad JSON expected 400, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Users/AuthenticateWithQuickConnect", bytes.NewBufferString(`{"Secret":"   "}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Authenticate empty secret expected 400, got %d", rec.Code)
	}
}
