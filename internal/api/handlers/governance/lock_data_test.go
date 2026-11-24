package governance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	coregov "github.com/ambra-media/ambra/internal/core/governance"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestHandleLockFieldsPersistsLockData(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "gov_lock.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	lib, err := repo.CreateLibrary(ctx, "Movies", "movies", []string{tmp})
	if err != nil {
		t.Fatal(err)
	}
	itemID := guid.New()
	if err := repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID: itemID, LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", MediaType: "Video", Name: "Locked Movie",
	}); err != nil {
		t.Fatal(err)
	}

	svc, err := coregov.NewService(coregov.Options{Store: store, Config: config.Default(tmp)})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	New(svc).RegisterRoutes(mux, AuthFuncs{})

	// 1. Set LockedFields + LockData=true
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/admin/governance/items/"+itemID+"/lock",
		strings.NewReader(`{"LockedFields":["Name","Overview"],"LockData":true}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("lock status=%d body=%s", rec.Code, rec.Body.String())
	}
	var res coregov.LockedFieldsResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if !res.LockData || len(res.LockedFields) != 2 {
		t.Fatalf("unexpected lock result: %+v", res)
	}

	item, err := repo.GetItemRecordByID(ctx, itemID)
	if err != nil {
		t.Fatal(err)
	}
	if !item.Extra.LockData || len(item.Extra.LockedFields) != 2 {
		t.Fatalf("expected LockData=true and 2 LockedFields persisted in SQLite, got %+v", item.Extra)
	}

	// 2. Toggle lock_data=false via snake_case without clobbering existing LockedFields
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, httptest.NewRequest(http.MethodPost, "/api/admin/governance/items/"+itemID+"/lock",
		strings.NewReader(`{"lock_data":false}`)))
	if rec2.Code != http.StatusOK {
		t.Fatalf("unlock status=%d body=%s", rec2.Code, rec2.Body.String())
	}
	var res2 coregov.LockedFieldsResult
	_ = json.Unmarshal(rec2.Body.Bytes(), &res2)
	if res2.LockData || len(res2.LockedFields) != 2 {
		t.Fatalf("expected LockData=false with preserved LockedFields, got %+v", res2)
	}
}
