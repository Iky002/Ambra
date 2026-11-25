package system

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestSystemConfigConcurrentSnapshotAndUpdateRaceFree(t *testing.T) {
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
	admin, err := authSvc.CreateUser(context.Background(), "admin", "admin1234", true)
	if err != nil {
		t.Fatal(err)
	}

	h := NewHandler(cfg, userRepo, nil).WithSetupDeps(SetupDeps{Auth: authSvc})
	adminCtx := middleware.WithAuthContext(context.Background(), &middleware.AuthContext{User: admin})

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				if idx%2 == 0 {
					body := `{"ServerName":"Ambra-Race","TMDBLanguage":"en-US","EnableSTRM302":false,"PlayedThreshold":0.85}`
					req := httptest.NewRequest(http.MethodPost, "/System/Configuration", strings.NewReader(body)).WithContext(adminCtx)
					rec := httptest.NewRecorder()
					h.UpdateConfiguration(rec, req)
					if rec.Code != http.StatusNoContent {
						t.Errorf("UpdateConfiguration status=%d body=%s", rec.Code, rec.Body.String())
					}
				} else {
					for _, fn := range []http.HandlerFunc{h.GetSystemInfo, h.GetConfiguration, h.GetPublicSystemInfo, h.GetSetupStatus} {
						req := httptest.NewRequest(http.MethodGet, "/System/Info", nil).WithContext(adminCtx)
						rec := httptest.NewRecorder()
						fn(rec, req)
						if rec.Code != http.StatusOK {
							t.Errorf("GET status=%d", rec.Code)
						}
					}
				}
			}
		}(i)
	}
	wg.Wait()

	// CompleteSetup updates config via cfg.Update/Save
	req := httptest.NewRequest(http.MethodPost, "/api/setup/complete", strings.NewReader(`{"server_name":"FinalName","tmdb_language":"ja-JP"}`)).WithContext(adminCtx)
	rec := httptest.NewRecorder()
	h.CompleteSetup(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("CompleteSetup got %d body=%s", rec.Code, rec.Body.String())
	}
	snap := cfg.Snapshot()
	if !snap.IsStartupWizardCompleted || snap.ServerName != "FinalName" || snap.TMDBLanguage != "ja-JP" {
		t.Fatalf("unexpected final config snapshot: %+v", snap)
	}
}
