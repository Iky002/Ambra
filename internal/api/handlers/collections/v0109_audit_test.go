package collections

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestV0109Audit_CreateCollectionFiltersPlaylistIDs(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "collections_v0109.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	libRepo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)
	authSvc := auth.NewService(userRepo, cfg)

	admin, err := authSvc.CreateUser(ctx, "admin", "adminpass", true)
	if err != nil {
		t.Fatal(err)
	}
	alice, err := authSvc.CreateUser(ctx, "alice", "alicepass", false)
	if err != nil {
		t.Fatal(err)
	}

	lib, _ := libRepo.CreateLibrary(ctx, "Movies", "movies", []string{tmp})
	movie := &sqlite.ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", MediaType: "Video", Name: "Inception"}
	if err := libRepo.UpsertItem(ctx, movie); err != nil {
		t.Fatal(err)
	}
	privPl, err := libRepo.CreatePlaylist(ctx, "Alice Secret Playlist", "Video", alice.ID, []string{movie.ID})
	if err != nil {
		t.Fatal(err)
	}

	h := New(libRepo, cfg)
	h.SetUserRepo(userRepo)
	h.SetStore(store)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	adminAC := &middleware.AuthContext{User: admin, Token: "admin-tok"}
	body := `{"Name":"Mixed Collection","Ids":["` + movie.ID + `","` + privPl.ID + `"]}`
	req := httptest.NewRequest(http.MethodPost, "/Collections", strings.NewReader(body))
	req = req.WithContext(middleware.WithAuthContext(req.Context(), adminAC))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /Collections expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var created creationResult
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	items, total, err := libRepo.GetContainerItems(ctx, cfg.ServerID, admin.ID, created.Id, 0, 50, nil)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 || items[0].Id != movie.ID {
		t.Fatalf("expected Playlist ID stripped from CreateCollection, got total=%d items=%+v", total, items)
	}
}
