package audio

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestAudioLibraryAccessControl(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "audio_rbac.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)

	libA, _ := repo.CreateLibrary(ctx, "MusicA", "music", []string{tmp})
	libB, _ := repo.CreateLibrary(ctx, "MusicB", "music", []string{tmp})

	mp3A := filepath.Join(tmp, "a.mp3")
	mp3B := filepath.Join(tmp, "b.mp3")
	_ = os.WriteFile(mp3A, []byte("ID3audioA"), 0o644)
	_ = os.WriteFile(mp3B, []byte("ID3audioB"), 0o644)
	_ = os.WriteFile(filepath.Join(tmp, "a.lrc"), []byte("[00:01.00]Lyric A\n"), 0o644)
	_ = os.WriteFile(filepath.Join(tmp, "b.lrc"), []byte("[00:01.00]Lyric B\n"), 0o644)

	songA := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libA.ID, ParentID: libA.ID, Type: "Audio", MediaType: "Audio", Name: "SongA", SortName: "songa", Path: mp3A}
	songB := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libB.ID, ParentID: libB.ID, Type: "Audio", MediaType: "Audio", Name: "SongB", SortName: "songb", Path: mp3B}
	_ = repo.UpsertItem(ctx, songA)
	_ = repo.UpsertItem(ctx, songB)

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
	h := New(repo, cfg)
	h.SetUserRepo(userRepo)
	h.RegisterRoutes(mux, AuthFuncs{
		UserIDFromCtx: func(context.Context) string { return currentCaller },
	})

	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	// Restricted user denied on libB -> 403
	for _, path := range []string{
		"/Audio/" + songB.ID + "/stream",
		"/Audio/" + songB.ID + "/universal?Container=mp3",
		"/Audio/" + songB.ID + "/Lyrics",
	} {
		if rec := get(path); rec.Code != http.StatusForbidden {
			t.Fatalf("restricted user GET %s expected 403, got %d", path, rec.Code)
		}
	}

	// Restricted user allowed on libA -> 200
	for _, path := range []string{
		"/Audio/" + songA.ID + "/stream",
		"/Audio/" + songA.ID + "/universal?Container=mp3",
		"/Audio/" + songA.ID + "/Lyrics",
	} {
		if rec := get(path); rec.Code != http.StatusOK {
			t.Fatalf("restricted user GET %s expected 200, got %d", path, rec.Code)
		}
	}

	// Admin allowed on libB -> 200
	currentCaller = adminUser.ID
	for _, path := range []string{
		"/Audio/" + songB.ID + "/stream",
		"/Audio/" + songB.ID + "/universal?Container=mp3",
		"/Audio/" + songB.ID + "/Lyrics",
	} {
		if rec := get(path); rec.Code != http.StatusOK {
			t.Fatalf("admin GET %s expected 200, got %d", path, rec.Code)
		}
	}
}
