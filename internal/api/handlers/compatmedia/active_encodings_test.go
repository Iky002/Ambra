package compatmedia

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/imaging"
	"github.com/ambra-media/ambra/internal/core/scanner"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestActiveEncodingsDeleteStopsTranscodeSession(t *testing.T) {
	tmpDir := t.TempDir()
	cfg, err := config.Load(filepath.Join(tmpDir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	repo := sqlite.NewLibraryRepo(store)
	reg := plugins.NewRegistry()
	bus := eventbus.New()
	sc := scanner.New(repo, reg, bus, cfg)
	img := imaging.New(repo).WithCacheDir(cfg.ImageCacheDir())
	mgr := transcode.NewManager(cfg, transcode.WithCapabilities(&transcode.HWCapabilities{}))
	t.Cleanup(mgr.Close)

	h := New(repo, sc, img, reg, store, cfg)
	h.SetTranscodeManager(mgr)
	router := h.BuildRouter(AuthFuncs{
		UserIDFromCtx: func(context.Context) string { return "admin" },
	})

	// Register two active transcode sessions on different devices/play sessions.
	if _, err := mgr.RegisterSession(transcode.SessionParams{
		DeviceID:      "dev-1",
		PlaySessionID: "ps-1",
		ItemID:        "item-1",
		MediaSourceID: "ms-1",
		InputPath:     filepath.Join(tmpDir, "a.mkv"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.RegisterSession(transcode.SessionParams{
		DeviceID:      "dev-2",
		PlaySessionID: "ps-2",
		ItemID:        "item-2",
		MediaSourceID: "ms-2",
		InputPath:     filepath.Join(tmpDir, "b.mkv"),
	}); err != nil {
		t.Fatal(err)
	}
	if got := len(mgr.ActiveSessions()); got != 2 {
		t.Fatalf("expected 2 active sessions, got %d", got)
	}

	adminCtx := func(r *http.Request) *http.Request {
		return r.WithContext(middleware.WithAuthContext(r.Context(), &middleware.AuthContext{
			User: &sqlite.UserRecord{ID: "admin", Name: "admin", IsAdmin: true},
		}))
	}

	// Stop only dev-1 / ps-1 via POST /Videos/ActiveEncodings/Delete.
	req := adminCtx(httptest.NewRequest(http.MethodPost, "/Videos/ActiveEncodings/Delete?DeviceId=dev-1&PlaySessionId=ps-1", nil))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}

	remaining := mgr.ActiveSessions()
	if len(remaining) != 1 || remaining[0].DeviceID != "dev-2" {
		t.Fatalf("expected only dev-2 remaining, got %+v", remaining)
	}

	// Stop dev-2 by DeviceId alone.
	req2 := adminCtx(httptest.NewRequest(http.MethodPost, "/Videos/ActiveEncodings/Delete?DeviceId=dev-2", nil))
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec2.Code)
	}
	if got := len(mgr.ActiveSessions()); got != 0 {
		t.Fatalf("expected 0 active sessions, got %d", got)
	}
}
