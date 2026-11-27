package collections

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestCollectionsAndPlaylistsExtraBranches(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "col_extra.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	cfg := config.Default(tmp)

	if New(repo, nil).serverID() != "" {
		t.Fatalf("expected empty serverID for nil cfg")
	}

	lib, _ := repo.CreateLibrary(ctx, "Movies", "movies", []string{tmp})
	m1 := &sqlite.ItemRecord{LibraryID: lib.ID, Type: "Movie", MediaType: "Video", Name: "M1"}
	_ = repo.UpsertItem(ctx, m1)

	userID := guid.New()
	mux := http.NewServeMux()
	h := New(repo, cfg)
	h.RegisterRoutes(mux, AuthFuncs{
		UserIDFromCtx: func(context.Context) string { return userID },
	})

	// Create collection: missing name -> 400; via JSON body -> 200
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Collections", bytes.NewReader([]byte(`{}`))))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty collection name, got %d", rec.Code)
	}

	colBody, _ := json.Marshal(map[string]any{"Name": "BodyBox", "ParentId": lib.ID, "Ids": []string{m1.ID}})
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Collections", bytes.NewReader(colBody)))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /Collections body failed: %d", rec.Code)
	}
	var colRes creationResult
	_ = json.Unmarshal(rec.Body.Bytes(), &colRes)

	// GET /Collections
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Collections?UserId="+userID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /Collections failed: %d", rec.Code)
	}

	// Create playlist: missing name -> 400; via JSON body -> 200
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Playlists", bytes.NewReader([]byte(`{}`))))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty playlist name, got %d", rec.Code)
	}

	plBody, _ := json.Marshal(map[string]any{"Name": "BodyPL", "MediaType": "Video", "UserId": userID, "Ids": []string{m1.ID}})
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Playlists", bytes.NewReader(plBody)))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /Playlists body failed: %d", rec.Code)
	}
	var plRes creationResult
	_ = json.Unmarshal(rec.Body.Bytes(), &plRes)

	// GET /Playlists
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Playlists", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /Playlists failed: %d", rec.Code)
	}

	// POST /Playlists/{Id}/Items via JSON body
	addBody, _ := json.Marshal(map[string]any{"Ids": []string{m1.ID}})
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Playlists/"+plRes.Id+"/Items", bytes.NewReader(addBody)))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /Playlists/{Id}/Items failed: %d", rec.Code)
	}

	// Move playlist item: invalid index -> 400; nonexistent playlist -> 404
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Playlists/"+plRes.Id+"/Items/"+m1.ID+"/Move/notanint", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid NewIndex, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Playlists/"+guid.New()+"/Items/"+m1.ID+"/Move/0", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing playlist move, got %d", rec.Code)
	}

	// 404 branches for GetContainerItems, AddItems, RemoveItems, DeleteContainer
	missingID := guid.New()
	for _, tc := range [][2]string{
		{http.MethodGet, "/Collections/" + missingID + "/Items"},
		{http.MethodPost, "/Collections/" + missingID + "/Items?Ids=" + m1.ID},
		{http.MethodDelete, "/Collections/" + missingID + "/Items?Ids=" + m1.ID},
		{http.MethodDelete, "/Collections/" + missingID},
	} {
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(tc[0], tc[1], nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s expected 404, got %d", tc[0], tc[1], rec.Code)
		}
	}

	// DELETE collection & playlist success
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/Collections/"+colRes.Id, nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE collection failed: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/Playlists/"+plRes.Id, nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE playlist failed: %d", rec.Code)
	}

	// Closed DB -> 500 paths
	_ = store.Close()
	for _, tc := range [][2]string{
		{http.MethodGet, "/Collections"},
		{http.MethodPost, "/Collections?Name=Fail"},
		{http.MethodGet, "/Playlists"},
		{http.MethodPost, "/Playlists?Name=Fail"},
		{http.MethodGet, "/Playlists/x/InstantMix"},
		{http.MethodGet, "/Collections/x/Items"},
		{http.MethodPost, "/Collections/x/Items?Ids=1"},
		{http.MethodDelete, "/Collections/x/Items?Ids=1"},
		{http.MethodDelete, "/Collections/x"},
		{http.MethodPost, "/Playlists/x/Items/y/Move/0"},
	} {
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(tc[0], tc[1], nil))
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("closed DB %s %s expected 500, got %d", tc[0], tc[1], rec.Code)
		}
	}
}
