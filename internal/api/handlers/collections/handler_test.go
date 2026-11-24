package collections

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestCollectionsAndPlaylists(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "col.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	cfg := config.Default(tmp)

	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{tmp})
	if err != nil {
		t.Fatal(err)
	}
	m1 := &sqlite.ItemRecord{LibraryID: lib.ID, Type: "Movie", MediaType: "Video", Name: "Movie A"}
	m2 := &sqlite.ItemRecord{LibraryID: lib.ID, Type: "Movie", MediaType: "Video", Name: "Movie B"}
	m3 := &sqlite.ItemRecord{LibraryID: lib.ID, Type: "Movie", MediaType: "Video", Name: "Movie C"}
	for _, m := range []*sqlite.ItemRecord{m1, m2, m3} {
		if err := repo.UpsertItem(ctx, m); err != nil {
			t.Fatal(err)
		}
		_ = repo.ReplaceItemTaxonomies(ctx, m.ID, "genre", []string{"Sci-Fi"})
	}

	mux := http.NewServeMux()
	h := New(repo, cfg)
	h.RegisterRoutes(mux, AuthFuncs{})

	// --- Collections ---
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, fmt.Sprintf("/Collections?Name=SciFiBox&Ids=%s,%s", m1.ID, m2.ID), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /Collections failed: %d %s", rec.Code, rec.Body.String())
	}
	var colRes creationResult
	_ = json.Unmarshal(rec.Body.Bytes(), &colRes)
	if colRes.Id == "" || colRes.Name != "SciFiBox" {
		t.Fatalf("unexpected collection result: %+v", colRes)
	}

	// Add m3 to collection
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, fmt.Sprintf("/Collections/%s/Items?Ids=%s,%s", colRes.Id, m2.ID, m3.ID), nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST /Collections/{Id}/Items failed: %d", rec.Code)
	}

	// Verify deduplication: should have 3 items (m1, m2, m3)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/Collections/%s/Items", colRes.Id), nil))
	var colItems dto.QueryResult[sqlite.PlaylistItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &colItems)
	if colItems.TotalRecordCount != 3 {
		t.Fatalf("expected 3 items in collection, got %d", colItems.TotalRecordCount)
	}

	// Remove m2 from collection
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/Collections/%s/Items?Ids=%s", colRes.Id, m2.ID), nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /Collections/{Id}/Items failed: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/Collections/%s/Items", colRes.Id), nil))
	colItems = dto.QueryResult[sqlite.PlaylistItemDto]{}
	_ = json.Unmarshal(rec.Body.Bytes(), &colItems)
	if colItems.TotalRecordCount != 2 {
		t.Fatalf("expected 2 items after delete, got %d", colItems.TotalRecordCount)
	}

	// --- Playlists ---
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, fmt.Sprintf("/Playlists?Name=Watchlist&MediaType=Video&Ids=%s,%s,%s", m1.ID, m2.ID, m3.ID), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /Playlists failed: %d %s", rec.Code, rec.Body.String())
	}
	var plRes creationResult
	_ = json.Unmarshal(rec.Body.Bytes(), &plRes)

	// Move m3 (index 2) to index 0
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, fmt.Sprintf("/Playlists/%s/Items/%s/Move/0", plRes.Id, m3.ID), nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("Move playlist item failed: %d %s", rec.Code, rec.Body.String())
	}

	// Verify order is [m3, m1, m2]
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/Playlists/%s/Items", plRes.Id), nil))
	var plItems dto.QueryResult[sqlite.PlaylistItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &plItems)
	if len(plItems.Items) != 3 || plItems.Items[0].Id != m3.ID || plItems.Items[1].Id != m1.ID || plItems.Items[2].Id != m2.ID {
		t.Fatalf("unexpected reordered playlist items: %+v", plItems.Items)
	}

	// InstantMix
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/Playlists/%s/InstantMix?Limit=5", plRes.Id), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("InstantMix failed: %d", rec.Code)
	}
	var mix dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &mix)
	if mix.TotalRecordCount == 0 {
		t.Fatalf("expected non-empty instant mix")
	}

	// Delete entry by PlaylistItemId
	entryToDel := plItems.Items[0].PlaylistItemId
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/Playlists/%s/Items?EntryIds=%s", plRes.Id, entryToDel), nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE playlist entry failed: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/Playlists/%s/Items", plRes.Id), nil))
	plItems = dto.QueryResult[sqlite.PlaylistItemDto]{}
	_ = json.Unmarshal(rec.Body.Bytes(), &plItems)
	if plItems.TotalRecordCount != 2 {
		t.Fatalf("expected 2 playlist items after entry delete, got %d", plItems.TotalRecordCount)
	}
}
