package library

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
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestV0109Audit_SearchHintsLimitClampedTo200(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "library_v0109.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	libRepo := sqlite.NewLibraryRepo(store)
	cfg := config.Default(tmp)
	lib, err := libRepo.CreateLibrary(ctx, "Movies", "movies", []string{tmp})
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 215; i++ {
		id := guid.New()
		name := fmt.Sprintf("ClampMovie %03d", i)
		if err := libRepo.UpsertItem(ctx, &sqlite.ItemRecord{
			ID: id, LibraryID: lib.ID, ParentID: lib.ID,
			Type: "Movie", MediaType: "Video", Name: name,
		}); err != nil {
			t.Fatal(err)
		}
		_ = libRepo.UpsertSearchFTS(ctx, id, name, "clampmovie", "clampmovie", "cm", "")
	}

	h := New(libRepo, nil, cfg)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Search/Hints?SearchTerm=ClampMovie&Limit=500", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /Search/Hints expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var res dto.SearchHintResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.SearchHints) != 200 {
		t.Fatalf("expected SearchHints clamped to 200, got %d", len(res.SearchHints))
	}
}
