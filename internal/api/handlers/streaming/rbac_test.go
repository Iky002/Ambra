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

func TestStreamingParentalControlEnforcement(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "stream_parental.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)

	lib, _ := repo.CreateLibrary(ctx, "Movies", "movies", []string{tmp})

	vidPG := filepath.Join(tmp, "pg.mp4")
	vidR := filepath.Join(tmp, "r.mp4")
	vidNSFW := filepath.Join(tmp, "nsfw.mp4")
	for _, p := range []string{vidPG, vidR, vidNSFW} {
		_ = os.WriteFile(p, []byte("video-data"), 0o644)
	}

	pgMovie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "FamilyMovie", SortName: "familymovie",
		OfficialRating: "PG", Path: vidPG,
	}
	rMovie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "AdultMovie", SortName: "adultmovie",
		OfficialRating: "R", Path: vidR,
	}
	nsfwMovie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "TaggedMovie", SortName: "taggedmovie",
		OfficialRating: "PG", Path: vidNSFW,
	}
	for _, m := range []*sqlite.ItemRecord{pgMovie, rMovie, nsfwMovie} {
		_ = repo.UpsertItem(ctx, m)
		_ = repo.ReplaceMediaSourcesAndStreams(ctx, m.ID, []sqlite.MediaSourceRecord{{
			ID: guid.New(), ItemID: m.ID, Path: m.Path, Container: "mp4",
		}})
	}
	_ = repo.ReplaceItemTaxonomies(ctx, nsfwMovie.ID, "tag", []string{"NSFW"})

	maxRating := 7 // PG-13
	kidPolicy := sqlite.DefaultUserPolicy(false)
	kidPolicy.EnableAllFolders = true
	kidPolicy.MaxParentalRating = &maxRating
	kidPolicy.BlockedTags = []string{"NSFW"}
	kidUser := &sqlite.UserRecord{
		ID: guid.New(), Name: "kid", Policy: kidPolicy, Config: sqlite.DefaultUserConfig(),
	}
	_ = userRepo.CreateUser(ctx, kidUser)

	adminPolicy := sqlite.DefaultUserPolicy(true)
	adminPolicy.MaxParentalRating = &maxRating
	adminPolicy.BlockedTags = []string{"NSFW"}
	adminUser := &sqlite.UserRecord{
		ID: guid.New(), Name: "admin", IsAdmin: true, Policy: adminPolicy, Config: sqlite.DefaultUserConfig(),
	}
	_ = userRepo.CreateUser(ctx, adminUser)

	currentCaller := kidUser.ID
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

	// Kid user blocked by rating (R > 7) and blocked tag (NSFW) on PlaybackInfo, stream, Download -> 403
	for _, id := range []string{rMovie.ID, nsfwMovie.ID} {
		for _, path := range []string{
			"/Items/" + id + "/PlaybackInfo",
			"/Videos/" + id + "/stream",
			"/Items/" + id + "/Download",
		} {
			if rec := get(path); rec.Code != http.StatusForbidden {
				t.Fatalf("kid GET %s expected 403, got %d", path, rec.Code)
			}
		}
	}

	// Kid user allowed for PG movie -> 200
	for _, path := range []string{
		"/Items/" + pgMovie.ID + "/PlaybackInfo",
		"/Videos/" + pgMovie.ID + "/stream",
		"/Items/" + pgMovie.ID + "/Download",
	} {
		if rec := get(path); rec.Code != http.StatusOK {
			t.Fatalf("kid GET %s expected 200, got %d (%s)", path, rec.Code, rec.Body.String())
		}
	}

	// Admin bypasses parental restrictions -> 200
	currentCaller = adminUser.ID
	for _, id := range []string{rMovie.ID, nsfwMovie.ID} {
		for _, path := range []string{
			"/Items/" + id + "/PlaybackInfo",
			"/Videos/" + id + "/stream",
			"/Items/" + id + "/Download",
		} {
			if rec := get(path); rec.Code != http.StatusOK {
				t.Fatalf("admin GET %s expected 200, got %d", path, rec.Code)
			}
		}
	}
}
