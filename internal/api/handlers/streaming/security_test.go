package streaming

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/imaging"
	"github.com/ambra-media/ambra/internal/core/playback"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestPlaybackInfoUserIsolationAndDownloadPolicy(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "stream_sec.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)

	lib, _ := repo.CreateLibrary(ctx, "Movies", "movies", []string{tmp})
	vidPath := filepath.Join(tmp, `evil"name.mp4`)
	if err := os.WriteFile(vidPath, []byte("movie-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	movie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "TestMovie", SortName: "testmovie", Path: vidPath,
	}
	_ = repo.UpsertItem(ctx, movie)
	_ = repo.ReplaceMediaSourcesAndStreams(ctx, movie.ID, []sqlite.MediaSourceRecord{{
		ID: guid.New(), ItemID: movie.ID, Path: vidPath, Container: "mp4",
	}})

	alicePolicy := sqlite.DefaultUserPolicy(false)
	alicePolicy.EnableContentDownloading = false
	alice := &sqlite.UserRecord{ID: guid.New(), Name: "alice", Policy: alicePolicy, Config: sqlite.DefaultUserConfig()}

	bobPolicy := sqlite.DefaultUserPolicy(false)
	bobPolicy.EnableContentDownloading = true
	bob := &sqlite.UserRecord{ID: guid.New(), Name: "bob", Policy: bobPolicy, Config: sqlite.DefaultUserConfig()}

	admin := &sqlite.UserRecord{ID: guid.New(), Name: "admin", IsAdmin: true, Policy: sqlite.DefaultUserPolicy(true), Config: sqlite.DefaultUserConfig()}
	_ = userRepo.CreateUser(ctx, alice)
	_ = userRepo.CreateUser(ctx, bob)
	_ = userRepo.CreateUser(ctx, admin)

	mux := http.NewServeMux()
	h := New(repo, playback.New(repo, plugins.NewRegistry(), cfg), imaging.New(repo))
	h.SetUserRepo(userRepo)
	h.RegisterRoutes(mux, AuthFuncs{})

	doReq := func(method, path, body string, u *sqlite.UserRecord) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if u != nil {
			req = req.WithContext(middleware.WithAuthContext(req.Context(), &middleware.AuthContext{User: u}))
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// 1. PlaybackInfo cross-user UserId isolation (GET query & POST JSON body)
	if rec := doReq(http.MethodGet, "/Items/"+movie.ID+"/PlaybackInfo?UserId="+bob.ID, "", alice); rec.Code != http.StatusForbidden {
		t.Fatalf("alice GET PlaybackInfo for bob: want 403, got %d", rec.Code)
	}
	if rec := doReq(http.MethodPost, "/Items/"+movie.ID+"/PlaybackInfo", `{"UserId":"`+bob.ID+`"}`, alice); rec.Code != http.StatusForbidden {
		t.Fatalf("alice POST PlaybackInfo for bob: want 403, got %d", rec.Code)
	}
	if rec := doReq(http.MethodGet, "/Items/"+movie.ID+"/PlaybackInfo?UserId="+alice.ID, "", alice); rec.Code != http.StatusOK {
		t.Fatalf("alice GET PlaybackInfo for self: want 200, got %d", rec.Code)
	}
	if rec := doReq(http.MethodPost, "/Items/"+movie.ID+"/PlaybackInfo", `{"UserId":"`+bob.ID+`"}`, admin); rec.Code != http.StatusOK {
		t.Fatalf("admin POST PlaybackInfo for bob: want 200, got %d", rec.Code)
	}

	// 2. Download enforces EnableContentDownloading for non-admins
	if rec := doReq(http.MethodGet, "/Items/"+movie.ID+"/Download", "", alice); rec.Code != http.StatusForbidden {
		t.Fatalf("alice Download with EnableContentDownloading=false: want 403, got %d", rec.Code)
	}
	recBob := doReq(http.MethodGet, "/Items/"+movie.ID+"/Download", "", bob)
	if recBob.Code != http.StatusOK {
		t.Fatalf("bob Download with EnableContentDownloading=true: want 200, got %d", recBob.Code)
	}
	cd := recBob.Header().Get("Content-Disposition")
	if !strings.Contains(cd, `evil\"name.mp4`) {
		t.Fatalf("expected escaped quote in Content-Disposition, got %q", cd)
	}
	if rec := doReq(http.MethodGet, "/Items/"+movie.ID+"/Download", "", admin); rec.Code != http.StatusOK {
		t.Fatalf("admin Download: want 200, got %d", rec.Code)
	}

	// 3. Direct unit test for sanitizeContentDispositionFilename (newlines, quotes, backslashes, empty)
	if got := sanitizeContentDispositionFilename("a\r\nb\"c\\d.mkv"); got != `ab\"c\\d.mkv` {
		t.Fatalf("sanitizeContentDispositionFilename got %q", got)
	}
	if got := sanitizeContentDispositionFilename("\r\n"); got != "media" {
		t.Fatalf("sanitizeContentDispositionFilename empty fallback got %q", got)
	}
}
