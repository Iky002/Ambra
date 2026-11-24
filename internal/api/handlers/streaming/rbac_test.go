package streaming

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/imaging"
	"github.com/ambra-media/ambra/internal/core/playback"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestStreamingLibraryAccessControl(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "stream_rbac.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)

	libA, _ := repo.CreateLibrary(ctx, "AllowedLib", "movies", []string{tmp})
	libB, _ := repo.CreateLibrary(ctx, "SecretLib", "movies", []string{tmp})

	vidA := filepath.Join(tmp, "a.mp4")
	vidB := filepath.Join(tmp, "b.mp4")
	subA := filepath.Join(tmp, "a.vtt")
	subB := filepath.Join(tmp, "b.vtt")
	_ = os.WriteFile(vidA, []byte("video-a-data"), 0o644)
	_ = os.WriteFile(vidB, []byte("video-b-data"), 0o644)
	_ = os.WriteFile(subA, []byte("WEBVTT\n\n00:00:01.000 --> 00:00:02.000\nHello A\n"), 0o644)
	_ = os.WriteFile(subB, []byte("WEBVTT\n\n00:00:01.000 --> 00:00:02.000\nHello B\n"), 0o644)

	movieA := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID, Type: "Movie", MediaType: "Video", Name: "MovieA", SortName: "moviea", Path: vidA}
	movieB := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libB.ID, ParentID: libB.ID, Type: "Movie", MediaType: "Video", Name: "MovieB", SortName: "movieb", Path: vidB}
	_ = repo.UpsertItem(ctx, movieA)
	_ = repo.UpsertItem(ctx, movieB)

	msA := guid.New()
	msB := guid.New()
	_ = repo.ReplaceMediaSourcesAndStreams(ctx, movieA.ID, []sqlite.MediaSourceRecord{{
		ID: msA, ItemID: movieA.ID, Path: vidA, Container: "mp4",
		Streams: []sqlite.MediaStreamRecord{{MediaSourceID: msA, ItemID: movieA.ID, StreamIndex: 1, StreamType: "Subtitle", Codec: "vtt", IsExternal: true, Path: subA}},
	}})
	_ = repo.ReplaceMediaSourcesAndStreams(ctx, movieB.ID, []sqlite.MediaSourceRecord{{
		ID: msB, ItemID: movieB.ID, Path: vidB, Container: "mp4",
		Streams: []sqlite.MediaStreamRecord{{MediaSourceID: msB, ItemID: movieB.ID, StreamIndex: 1, StreamType: "Subtitle", Codec: "vtt", IsExternal: true, Path: subB}},
	}})

	restrictedPolicy := sqlite.DefaultUserPolicy(false)
	restrictedPolicy.EnableAllFolders = false
	restrictedPolicy.EnabledFolders = []string{libA.ID}
	restrictedUser := &sqlite.UserRecord{
		ID: guid.New(), Name: "restricted",
		Policy: restrictedPolicy, Config: sqlite.DefaultUserConfig(),
	}
	_ = userRepo.CreateUser(ctx, restrictedUser)

	adminUser := &sqlite.UserRecord{
		ID: guid.New(), Name: "admin", IsAdmin: true,
		Policy: sqlite.DefaultUserPolicy(true), Config: sqlite.DefaultUserConfig(),
	}
	_ = userRepo.CreateUser(ctx, adminUser)

	currentCaller := restrictedUser.ID
	mux := http.NewServeMux()
	h := New(repo, playback.New(repo, plugins.NewRegistry(), cfg), imaging.New(repo))
	h.SetUserRepo(userRepo)
	h.RegisterRoutes(mux, AuthFuncs{
		UserIDFromCtx: func(context.Context) string { return currentCaller },
	})

	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	// Restricted user denied on libB across all 4 endpoints -> 403
	for _, path := range []string{
		"/Items/" + movieB.ID + "/PlaybackInfo",
		"/Videos/" + movieB.ID + "/stream",
		"/Items/" + movieB.ID + "/Download",
		"/Videos/" + movieB.ID + "/" + msB + "/Subtitles/1/Stream.vtt",
	} {
		if rec := get(path); rec.Code != http.StatusForbidden {
			t.Fatalf("restricted user GET %s expected 403, got %d", path, rec.Code)
		}
	}

	// Restricted user allowed on libA -> 200
	for _, path := range []string{
		"/Items/" + movieA.ID + "/PlaybackInfo",
		"/Videos/" + movieA.ID + "/stream",
		"/Items/" + movieA.ID + "/Download",
		"/Videos/" + movieA.ID + "/" + msA + "/Subtitles/1/Stream.vtt",
	} {
		if rec := get(path); rec.Code != http.StatusOK {
			t.Fatalf("restricted user GET %s expected 200, got %d (%s)", path, rec.Code, rec.Body.String())
		}
	}

	// Admin allowed on libB -> 200
	currentCaller = adminUser.ID
	for _, path := range []string{
		"/Items/" + movieB.ID + "/PlaybackInfo",
		"/Videos/" + movieB.ID + "/stream",
		"/Items/" + movieB.ID + "/Download",
		"/Videos/" + movieB.ID + "/" + msB + "/Subtitles/1/Stream.vtt",
	} {
		if rec := get(path); rec.Code != http.StatusOK {
			t.Fatalf("admin GET %s expected 200, got %d", path, rec.Code)
		}
	}
}
