package library

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/scanner"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/plugins/nfo"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestMediaUpdatedRejectsEscapingSymlinkAndAllowsDeletedPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require privileges on windows")
	}

	ctx := context.Background()
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "data")
	moviesDir := filepath.Join(tmp, "movies")
	outsideDir := filepath.Join(tmp, "outside")
	for _, d := range []string{dataDir, moviesDir, outsideDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	store, err := sqlite.Open(filepath.Join(dataDir, "media_updated_symlink.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(dataDir)
	reg := plugins.NewRegistry()
	reg.Register(nfo.New())
	sc := scanner.New(repo, reg, eventbus.New(), cfg)

	movLib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{moviesDir})
	if err != nil {
		t.Fatal(err)
	}

	admin := &sqlite.UserRecord{
		ID:      guid.New(),
		Name:    "admin_symlink",
		IsAdmin: true,
		Policy:  sqlite.DefaultUserPolicy(true),
		Config:  sqlite.DefaultUserConfig(),
	}
	if err := userRepo.CreateUser(ctx, admin); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	h := New(repo, sc, cfg)
	h.SetUserRepo(userRepo)
	h.SetStore(store)
	h.RegisterRoutes(mux, AuthFuncs{
		UserIDFromCtx: func(c context.Context) string {
			if ac, ok := middleware.AuthFromContext(c); ok && ac != nil {
				return ac.UserID()
			}
			return ""
		},
	})

	postUpdated := func(pathVal, updateType string) *httptest.ResponseRecorder {
		t.Helper()
		payload := `{"Updates":[{"Path":` + `"` + pathVal + `"` + `,"UpdateType":"` + updateType + `"}]}`
		req := httptest.NewRequest(http.MethodPost, "/Library/Media/Updated", strings.NewReader(payload))
		req = req.WithContext(middleware.WithAuthContext(ctx, &middleware.AuthContext{User: admin}))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// 1. Symlink inside moviesDir pointing outside moviesDir must be rejected by handleMediaUpdated.
	outsideMovie := filepath.Join(outsideDir, "SecretOutside (2024).mkv")
	if err := os.WriteFile(outsideMovie, []byte("secret-video"), 0o600); err != nil {
		t.Fatal(err)
	}
	escapeLink := filepath.Join(moviesDir, "EscapedLink (2024).mkv")
	if err := os.Symlink(outsideMovie, escapeLink); err != nil {
		t.Fatal(err)
	}

	if rec := postUpdated(escapeLink, "Created"); rec.Code != http.StatusNoContent {
		t.Fatalf("POST /Library/Media/Updated status=%d", rec.Code)
	}
	waitCtx1, cancel1 := context.WithTimeout(ctx, 5*time.Second)
	defer cancel1()
	sc.WaitIdle(waitCtx1)

	if _, err := repo.FindItemByMediaSourcePath(ctx, movLib.ID, escapeLink); err == nil {
		t.Fatal("expected escaping symlink NOT to be scanned via /Library/Media/Updated")
	}
	if _, err := repo.FindItemByMediaSourcePath(ctx, movLib.ID, outsideMovie); err == nil {
		t.Fatal("expected outside target NOT to be scanned via /Library/Media/Updated")
	}

	// Remove escaping symlink so subsequent legitimate scans of moviesDir remain clean.
	_ = os.Remove(escapeLink)

	// 2. Valid file inside moviesDir is scanned, and deleted-file notification removes it cleanly!
	realMovie := filepath.Join(moviesDir, "LegitMovie (2024).mkv")
	if err := os.WriteFile(realMovie, []byte("legit-video"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rec := postUpdated(realMovie, "Created"); rec.Code != http.StatusNoContent {
		t.Fatalf("POST /Library/Media/Updated Created status=%d", rec.Code)
	}
	waitCtx2, cancel2 := context.WithTimeout(ctx, 5*time.Second)
	defer cancel2()
	sc.WaitIdle(waitCtx2)

	if _, err := repo.FindItemByMediaSourcePath(ctx, movLib.ID, realMovie); err != nil {
		t.Fatalf("expected realMovie to be indexed, err=%v", err)
	}

	if err := os.Remove(realMovie); err != nil {
		t.Fatal(err)
	}
	if rec := postUpdated(realMovie, "Deleted"); rec.Code != http.StatusNoContent {
		t.Fatalf("POST /Library/Media/Updated Deleted status=%d", rec.Code)
	}
	waitCtx3, cancel3 := context.WithTimeout(ctx, 5*time.Second)
	defer cancel3()
	sc.WaitIdle(waitCtx3)

	if _, err := repo.FindItemByMediaSourcePath(ctx, movLib.ID, realMovie); err == nil {
		t.Fatal("expected deleted realMovie to be removed after Deleted notification")
	}
}
