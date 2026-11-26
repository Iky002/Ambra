package collections

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestCollectionsV0107AuditH3PlaylistNestingAndPrivateFilter(t *testing.T) {
	tmp := t.TempDir()
	st, err := sqlite.Open(filepath.Join(tmp, "col_v0107.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(st)
	userRepo := sqlite.NewUserRepo(st)
	cfg := config.Default(tmp)

	lib, _ := repo.CreateLibrary(ctx, "Movies", "movies", []string{tmp})
	movie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "Public Movie",
	}
	_ = repo.UpsertItem(ctx, movie)

	// Non-admin users WITH EnableAllFolders=true and no parental restrictions
	alice := &sqlite.UserRecord{ID: guid.New(), Name: "alice", Policy: sqlite.DefaultUserPolicy(false), Config: sqlite.DefaultUserConfig()}
	bob := &sqlite.UserRecord{ID: guid.New(), Name: "bob", Policy: sqlite.DefaultUserPolicy(false), Config: sqlite.DefaultUserConfig()}
	admin := &sqlite.UserRecord{ID: guid.New(), Name: "admin", IsAdmin: true, Policy: sqlite.DefaultUserPolicy(true), Config: sqlite.DefaultUserConfig()}
	alice.Policy.EnableAllFolders = true
	bob.Policy.EnableAllFolders = true
	for _, u := range []*sqlite.UserRecord{alice, bob, admin} {
		_ = userRepo.CreateUser(ctx, u)
	}

	bobPrivPl, _ := repo.CreatePlaylist(ctx, "BobPrivate", "Video", bob.ID, []string{movie.ID})
	aliceOwnPl, _ := repo.CreatePlaylist(ctx, "AliceOwn", "Video", alice.ID, []string{movie.ID})

	// Admin creates a BoxSet containing movie + bobPrivPl (edge case for handleGetContainerItems)
	box, _ := repo.CreateCollection(ctx, "MixedBox", "", []string{movie.ID, bobPrivPl.ID})

	h := New(repo, cfg)
	h.SetUserRepo(userRepo)
	h.SetStore(st)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	doAs := func(u *sqlite.UserRecord, method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req = req.WithContext(middleware.WithAuthContext(req.Context(), &middleware.AuthContext{User: u}))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// 1. [H-3] Alice (EnableAllFolders=true) creating playlist with bobPrivPl + aliceOwnPl + movie:
	//    only movie must be added; private playlist and nested playlist must be excluded.
	recCreate := doAs(alice, http.MethodPost, "/Playlists?Name=AliceNew&Ids="+bobPrivPl.ID+","+aliceOwnPl.ID+","+movie.ID, "")
	if recCreate.Code != http.StatusOK {
		t.Fatalf("create playlist got %d body=%q", recCreate.Code, recCreate.Body.String())
	}
	var created creationResult
	_ = json.Unmarshal(recCreate.Body.Bytes(), &created)

	recItems := doAs(alice, http.MethodGet, "/Playlists/"+created.Id+"/Items", "")
	var itemsRes dto.QueryResult[sqlite.PlaylistItemDto]
	_ = json.Unmarshal(recItems.Body.Bytes(), &itemsRes)
	if len(itemsRes.Items) != 1 || itemsRes.Items[0].Id != movie.ID {
		t.Fatalf("H-3 expected only movie in newly created playlist (no private/nested playlist), got %+v", itemsRes.Items)
	}

	// 2. [H-3] Adding bobPrivPl or nested aliceOwnPl to existing playlist is also rejected
	recAdd := doAs(alice, http.MethodPost, "/Playlists/"+created.Id+"/Items?Ids="+bobPrivPl.ID+","+aliceOwnPl.ID, "")
	if recAdd.Code != http.StatusNoContent {
		t.Fatalf("add playlist items got %d", recAdd.Code)
	}
	recItems2 := doAs(alice, http.MethodGet, "/Playlists/"+created.Id+"/Items", "")
	var itemsRes2 dto.QueryResult[sqlite.PlaylistItemDto]
	_ = json.Unmarshal(recItems2.Body.Bytes(), &itemsRes2)
	if len(itemsRes2.Items) != 1 {
		t.Fatalf("H-3 expected nested/private playlists not added, got %d items", len(itemsRes2.Items))
	}

	// 3. [H-3] handleGetContainerItems filters invisible Playlist child for non-admin with EnableAllFolders=true
	recBoxAlice := doAs(alice, http.MethodGet, "/Collections/"+box.ID+"/Items", "")
	if recBoxAlice.Code != http.StatusOK {
		t.Fatalf("alice GET BoxSet items got %d", recBoxAlice.Code)
	}
	var boxResAlice dto.QueryResult[sqlite.PlaylistItemDto]
	_ = json.Unmarshal(recBoxAlice.Body.Bytes(), &boxResAlice)
	for _, it := range boxResAlice.Items {
		if it.Id == bobPrivPl.ID {
			t.Fatalf("H-3 BoxSet items leaked bob's private playlist to alice!")
		}
	}

	// Admin still sees both items in BoxSet
	recBoxAdmin := doAs(admin, http.MethodGet, "/Collections/"+box.ID+"/Items", "")
	var boxResAdmin dto.QueryResult[sqlite.PlaylistItemDto]
	_ = json.Unmarshal(recBoxAdmin.Body.Bytes(), &boxResAdmin)
	if len(boxResAdmin.Items) != 2 {
		t.Fatalf("expected admin to see both items in BoxSet, got %d", len(boxResAdmin.Items))
	}
}
