package library

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestAlphabetFilterHTTP(t *testing.T) {
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	repo := sqlite.NewLibraryRepo(st)
	ctx := context.Background()

	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{"/movies"})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []struct{ id, name string }{
		{"m1", "1917"},
		{"m2", "Avatar"},
		{"m3", "Batman"},
		{"m4", "Zodiac"},
	} {
		if err := repo.UpsertItem(ctx, &sqlite.ItemRecord{
			ID: m.id, LibraryID: lib.ID, ParentID: lib.ID,
			Type: "Movie", MediaType: "Video", Name: m.name,
		}); err != nil {
			t.Fatal(err)
		}
	}

	h := New(repo, nil, &config.Config{ServerID: "srv1"})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	query := func(rawURL string) dto.QueryResult[dto.BaseItemDto] {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, rawURL, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status=%d body=%s", rawURL, rec.Code, rec.Body.String())
		}
		var out dto.QueryResult[dto.BaseItemDto]
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode %s: %v", rawURL, err)
		}
		return out
	}

	res := query("/Users/u1/Items?IncludeItemTypes=Movie&NameStartsWith=B")
	if res.TotalRecordCount != 1 || len(res.Items) != 1 || res.Items[0].Name != "Batman" {
		t.Fatalf("NameStartsWith=B got %+v", res)
	}

	res = query("/Users/u1/Items?IncludeItemTypes=Movie&NameStartsWithOrGreater=M")
	if res.TotalRecordCount != 1 || len(res.Items) != 1 || res.Items[0].Name != "Zodiac" {
		t.Fatalf("NameStartsWithOrGreater=M got %+v", res)
	}

	res = query("/Users/u1/Items?IncludeItemTypes=Movie&NameLessThan=A")
	if res.TotalRecordCount != 1 || len(res.Items) != 1 || res.Items[0].Name != "1917" {
		t.Fatalf("NameLessThan=A got %+v", res)
	}
}
