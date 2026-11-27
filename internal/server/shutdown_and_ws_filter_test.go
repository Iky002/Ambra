package server

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestServerCloseStopsTraktWorkersNoLeak(t *testing.T) {
	ctx := context.Background()

	// Warm runtime & measure baseline goroutines.
	runtime.GC()
	before := runtime.NumGoroutine()

	for i := 0; i < 3; i++ {
		c, err := config.Load(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		c.EnableDLNA = false
		srv, err := New(ctx, c)
		if err != nil {
			t.Fatalf("server.New #%d: %v", i, err)
		}
		if srv.stopTrakt == nil {
			_ = srv.Close()
			t.Fatal("expected stopTrakt cleanup callback to be stored on Server")
		}
		if err := srv.Close(); err != nil {
			t.Fatalf("server.Close #%d: %v", i, err)
		}
	}

	// Give goroutines a brief moment to finish exiting.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		runtime.GC()
		if runtime.NumGoroutine() <= before+6 {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	after := runtime.NumGoroutine()
	if after > before+6 {
		t.Fatalf("possible goroutine leak after 3 Server New/Close cycles: before=%d after=%d", before, after)
	}
}

func TestWSLibraryChangeFilterItemsRemovedSemantics(t *testing.T) {
	ctx := context.Background()
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.EnableDLNA = false
	srv, err := New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	userRepo := sqlite.NewUserRepo(srv.Store)
	libRepo := sqlite.NewLibraryRepo(srv.Store)

	libAllowed, err := libRepo.CreateLibrary(ctx, "Allowed", "movies", []string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	libDenied, err := libRepo.CreateLibrary(ctx, "Denied", "movies", []string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}

	// User 1: library-restricted only (NO parental/tag restrictions).
	uPlain, err := srv.Auth.CreateUser(ctx, "plain", "pw", false)
	if err != nil {
		t.Fatal(err)
	}
	uPlain.Policy.EnableAllFolders = false
	uPlain.Policy.EnabledFolders = []string{libAllowed.ID}
	if err := userRepo.UpdateUserPolicy(ctx, uPlain.ID, uPlain.Policy); err != nil {
		t.Fatal(err)
	}

	// User 2: library-restricted + parental rating restriction.
	uRestricted, err := srv.Auth.CreateUser(ctx, "restricted", "pw", false)
	if err != nil {
		t.Fatal(err)
	}
	maxPG := 5
	uRestricted.Policy.EnableAllFolders = false
	uRestricted.Policy.EnabledFolders = []string{libAllowed.ID}
	uRestricted.Policy.MaxParentalRating = &maxPG
	if err := userRepo.UpdateUserPolicy(ctx, uRestricted.ID, uRestricted.Policy); err != nil {
		t.Fatal(err)
	}

	filter := buildWSLibraryChangeFilter(userRepo, libRepo)
	deletedID := guid.New() // Already deleted from DB, so itemChecker would return false!

	// 1. Plain user on allowed library -> ItemsRemoved preserved even though row is deleted.
	outPlain, ok := filter(ctx, uPlain.ID, map[string]any{
		"LibraryId":    libAllowed.ID,
		"ItemsRemoved": []string{deletedID},
	})
	if !ok {
		t.Fatal("expected ItemsRemoved event delivered to user with library access and no content policy")
	}
	mPlain := outPlain.(map[string]any)
	remPlain, _ := mPlain["ItemsRemoved"].([]string)
	if mPlain["LibraryId"] != libAllowed.ID || len(remPlain) != 1 || remPlain[0] != deletedID {
		t.Fatalf("expected preserved ItemsRemoved=[%s], got %#v", deletedID, mPlain)
	}

	// 2. Parental/tag-restricted user on allowed library -> ItemsRemoved emptied to avoid leaking IDs,
	//    but event STILL delivered with LibraryId so client refreshes!
	outRestr, ok := filter(ctx, uRestricted.ID, map[string]any{
		"LibraryId":    libAllowed.ID,
		"ItemsRemoved": []string{deletedID},
	})
	if !ok {
		t.Fatal("expected library refresh signal preserved for parental-restricted user on ItemsRemoved")
	}
	mRestr := outRestr.(map[string]any)
	remRestr, _ := mRestr["ItemsRemoved"].([]string)
	if mRestr["LibraryId"] != libAllowed.ID || len(remRestr) != 0 {
		t.Fatalf("expected LibraryId with empty ItemsRemoved for restricted user, got %#v", mRestr)
	}

	// 3. Denied library -> dropped completely.
	if _, ok := filter(ctx, uPlain.ID, map[string]any{
		"LibraryId":    libDenied.ID,
		"ItemsRemoved": []string{deletedID},
	}); ok {
		t.Fatal("expected ItemsRemoved for unauthorized library dropped")
	}

	// 4. Restricted library user with empty LibraryId -> dropped (cannot verify library scope).
	if _, ok := filter(ctx, uPlain.ID, map[string]any{
		"ItemsRemoved": []string{deletedID},
	}); ok {
		t.Fatal("expected ItemsRemoved without LibraryId dropped for folder-restricted user")
	}
}
