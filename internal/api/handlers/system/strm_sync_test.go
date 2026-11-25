package system

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/plugins/strm"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestHotSyncEnableSTRM302ToBuiltinPlugin(t *testing.T) {
	tmp := t.TempDir()
	cfg, err := config.Load(filepath.Join(tmp, "data"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.EnableSTRM302 = false

	store, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	userRepo := sqlite.NewUserRepo(store)
	authSvc := auth.NewService(userRepo, cfg)
	if _, err := authSvc.CreateUser(context.Background(), "admin", "admin123", true); err != nil {
		t.Fatal(err)
	}
	authRes, err := authSvc.AuthenticateByName(context.Background(), "admin", "admin123", "", auth.ClientInfo{DeviceID: "dev"})
	if err != nil {
		t.Fatal(err)
	}

	reg := plugins.NewRegistry()
	strmPlugin := strm.New(cfg.EnableSTRM302)
	reg.Register(strmPlugin)

	strmFile := filepath.Join(tmp, "movie.strm")
	if err := os.WriteFile(strmFile, []byte("https://93.184.216.34/movie.mkv\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := NewHandler(cfg, userRepo, reg).WithSetupDeps(SetupDeps{Auth: authSvc})
	r := chi.NewRouter()
	authMW := middleware.NewAuth(authSvc, session.NewManager(userRepo, cfg, eventbus.New()))
	r.Use(authMW.ParseAuth)
	RegisterRoutes(r, authMW, h)
	RegisterSetupRoutes(r, authMW, h)

	// Initially false
	res, err := strmPlugin.ResolveStream(context.Background(), strmFile)
	if err != nil || res == nil || res.Is302 {
		t.Fatalf("expected Is302=false initially, got %+v err=%v", res, err)
	}

	// 1. UpdateConfiguration EnableSTRM302=true -> hot-syncs plugin immediately
	req := httptest.NewRequest(http.MethodPost, "/System/Configuration", strings.NewReader(`{"EnableSTRM302":true}`))
	req.Header.Set("X-Emby-Token", authRes.AccessToken)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("UpdateConfiguration status=%d body=%s", rec.Code, rec.Body.String())
	}

	res, err = strmPlugin.ResolveStream(context.Background(), strmFile)
	if err != nil || res == nil || !res.Is302 {
		t.Fatalf("expected Is302=true after UpdateConfiguration, got %+v err=%v", res, err)
	}

	// 2. CompleteSetup enable_strm_302=false -> hot-syncs plugin immediately
	req2 := httptest.NewRequest(http.MethodPost, "/api/setup/complete", strings.NewReader(`{"enable_strm_302":false}`))
	req2.Header.Set("X-Emby-Token", authRes.AccessToken)
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("CompleteSetup status=%d body=%s", rec2.Code, rec2.Body.String())
	}

	res, err = strmPlugin.ResolveStream(context.Background(), strmFile)
	if err != nil || res == nil || res.Is302 {
		t.Fatalf("expected Is302=false after CompleteSetup, got %+v err=%v", res, err)
	}
}
