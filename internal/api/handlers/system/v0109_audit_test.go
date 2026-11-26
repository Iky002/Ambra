package system

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestV0109Audit_ConfigCacheControlAndExtraConfigMutex(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "system_v0109.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)
	authSvc := auth.NewService(userRepo, cfg)
	authMW := middleware.NewAuth(authSvc, nil)

	h := NewHandler(cfg, userRepo, plugins.NewRegistry()).WithSetupDeps(SetupDeps{Auth: authSvc})
	r := chi.NewRouter()
	RegisterRoutes(r, authMW, h)
	RegisterSetupRoutes(r, authMW, h)

	// 1. [M-14] POST /api/setup/admin sets Cache-Control: private, no-store
	setupReq := httptest.NewRequest(http.MethodPost, "/api/setup/admin", bytes.NewBufferString(`{
		"server_name":"AmbraTest",
		"username":"admin",
		"password":"password123",
		"country":"US"
	}`))
	setupRec := httptest.NewRecorder()
	r.ServeHTTP(setupRec, setupReq)
	if setupRec.Code != http.StatusOK {
		t.Fatalf("setup admin expected 200, got %d body=%s", setupRec.Code, setupRec.Body.String())
	}
	if cc := setupRec.Header().Get("Cache-Control"); cc != "private, no-store" {
		t.Fatalf("setup admin expected Cache-Control=private, no-store, got %q", cc)
	}
	var setupRes struct {
		AccessToken string `json:"AccessToken"`
	}
	_ = json.Unmarshal(setupRec.Body.Bytes(), &setupRes)

	// 2. [M-14] GET /System/Configuration sets Cache-Control: private, no-store
	cfgReq := httptest.NewRequest(http.MethodGet, "/System/Configuration", nil)
	cfgReq.Header.Set("X-Emby-Token", setupRes.AccessToken)
	cfgRec := httptest.NewRecorder()
	r.ServeHTTP(cfgRec, cfgReq)
	if cfgRec.Code != http.StatusOK {
		t.Fatalf("GET /System/Configuration expected 200, got %d", cfgRec.Code)
	}
	if cc := cfgRec.Header().Get("Cache-Control"); cc != "private, no-store" {
		t.Fatalf("GET /System/Configuration expected Cache-Control=private, no-store, got %q", cc)
	}

	// 3. [L-09] Concurrent UpdateConfiguration read-modify-write preserves disjoint extra keys
	var wg sync.WaitGroup
	keys := []string{"ExtraKeyA", "ExtraKeyB", "ExtraKeyC", "ExtraKeyD", "ExtraKeyE"}
	for _, k := range keys {
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			payload, _ := json.Marshal(map[string]any{key: "val-" + key})
			req := httptest.NewRequest(http.MethodPost, "/System/Configuration", bytes.NewReader(payload))
			req.Header.Set("X-Emby-Token", setupRes.AccessToken)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != http.StatusNoContent {
				t.Errorf("POST /System/Configuration(%s) got %d", key, rec.Code)
			}
		}(k)
	}
	wg.Wait()

	extra := h.loadExtraConfig(context.Background())
	for _, k := range keys {
		if extra[k] != "val-"+k {
			t.Fatalf("lost concurrent extra config key %s: got %#v (all=%#v)", k, extra[k], extra)
		}
	}
}
