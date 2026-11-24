package catalog

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestCatalogHandlerExtraBranches(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "cat_extra.db"))
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

	lib, _ := repo.CreateLibrary(ctx, "Media", "movies", []string{tmp})
	m1 := &sqlite.ItemRecord{LibraryID: lib.ID, Type: "Movie", MediaType: "Video", Name: "Movie 1"}
	m2 := &sqlite.ItemRecord{LibraryID: lib.ID, Type: "Movie", MediaType: "Video", Name: "Movie 2"}
	ch := &sqlite.ItemRecord{LibraryID: lib.ID, Type: "Channel", IsFolder: true, Name: "LiveCh"}
	_ = repo.UpsertItem(ctx, m1)
	_ = repo.UpsertItem(ctx, m2)
	_ = repo.UpsertItem(ctx, ch)
	_ = repo.ReplaceItemTaxonomies(ctx, m1.ID, "genre", []string{"Action"})
	_ = repo.ReplaceItemTaxonomies(ctx, m2.ID, "genre", []string{"Action"})

	chItem := &sqlite.ItemRecord{LibraryID: lib.ID, ParentID: ch.ID, Type: "Video", MediaType: "Video", Name: "ChItem1"}
	_ = repo.UpsertItem(ctx, chItem)

	userID := guid.New()
	mux := http.NewServeMux()
	h := New(repo, cfg)
	h.RegisterRoutes(mux, AuthFuncs{
		UserIDFromCtx: func(context.Context) string { return userID },
	})

	// Similar (Movies & Albums) using ctx userID and query UserId
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Movies/"+m1.ID+"/Similar?Limit=5", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /Movies/{Id}/Similar status=%d", rec.Code)
	}
	var simRes dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &simRes)
	if simRes.TotalRecordCount == 0 {
		t.Fatalf("expected similar movies")
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Albums/"+m1.ID+"/Similar?UserId="+userID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /Albums/{Id}/Similar status=%d", rec.Code)
	}

	// Channels/{Id}/Items
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Channels/"+ch.ID+"/Items", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /Channels/{Id}/Items status=%d", rec.Code)
	}
	var chRes dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &chRes)
	if chRes.TotalRecordCount != 1 {
		t.Fatalf("expected 1 channel item, got %d", chRes.TotalRecordCount)
	}

	// Closed DB -> 500 error branches
	_ = store.Close()
	for _, path := range []string{
		"/Movies/Recommendations",
		"/Movies/" + m1.ID + "/Similar",
		"/Albums/" + m1.ID + "/InstantMix",
		"/Trailers",
		"/Channels/" + ch.ID + "/Items",
		"/Tags",
	} {
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("closed DB GET %s expected 500, got %d", path, rec.Code)
		}
	}
}
