package governance

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestSetLockStatePersistsAndPreservesFields(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "lockstate.db"))
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
		ID: itemID, LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", MediaType: "Video", Name: "Test",
	}); err != nil {
		t.Fatal(err)
	}

	svc, err := NewService(Options{Store: store, Config: config.Default(tmp)})
	if err != nil {
		t.Fatal(err)
	}

	// Invalid / missing ID
	if _, err := svc.SetLockState(ctx, "", nil, nil); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
	if _, err := svc.SetLockState(ctx, guid.New(), nil, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	// Set both LockedFields and LockData=true
	tr := true
	res, err := svc.SetLockState(ctx, itemID, []string{"Name", "name", " Overview "}, &tr)
	if err != nil {
		t.Fatalf("SetLockState: %v", err)
	}
	if !res.LockData || len(res.LockedFields) != 2 {
		t.Fatalf("unexpected result: %+v", res)
	}

	// Calling SetLockedFields keeps LockData=true
	res2, err := svc.SetLockedFields(ctx, itemID, []string{"Taglines"})
	if err != nil {
		t.Fatalf("SetLockedFields: %v", err)
	}
	if !res2.LockData || len(res2.LockedFields) != 1 || res2.LockedFields[0] != "Taglines" {
		t.Fatalf("expected LockData preserved, got %+v", res2)
	}

	// Calling SetLockState with nil fields and nil lockData clears LockedFields while preserving LockData
	res3, err := svc.SetLockState(ctx, itemID, nil, nil)
	if err != nil {
		t.Fatalf("SetLockState(nil, nil): %v", err)
	}
	if len(res3.LockedFields) != 0 || !res3.LockData {
		t.Fatalf("unexpected res3: %+v", res3)
	}

	// Calling SetLockState with lockData=false disables LockData
	fa := false
	res4, err := svc.SetLockState(ctx, itemID, nil, &fa)
	if err != nil {
		t.Fatalf("SetLockState(nil, false): %v", err)
	}
	if res4.LockData {
		t.Fatalf("expected LockData=false, got %+v", res4)
	}
}
