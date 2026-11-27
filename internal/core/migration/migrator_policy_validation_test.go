package migration

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestMigratorUsernameValidationAndPolicyTighteningRevocation(t *testing.T) {
	dir := t.TempDir()
	store, err := sqlite.Open(filepath.Join(dir, "mig_val.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	userRepo := sqlite.NewUserRepo(store)
	m := NewMigrator(store, userRepo)
	ctx := context.Background()

	// 1. Control character in source username -> conflict
	_, _, _, conflict, _, err := m.resolveOrCreateUser(ctx, SourceUser{ID: "u1", Name: "bad\nname"}, true, false)
	if err != nil || conflict == nil {
		t.Fatalf("expected conflict for control char username, got conflict=%+v err=%v", conflict, err)
	}

	// 2. Excessively long username (>128 runes) -> conflict
	_, _, _, conflict, _, err = m.resolveOrCreateUser(ctx, SourceUser{ID: "u2", Name: strings.Repeat("a", 129)}, true, false)
	if err != nil || conflict == nil {
		t.Fatalf("expected conflict for >128 char username, got conflict=%+v err=%v", conflict, err)
	}

	// 3. Policy tightening on existing non-admin user triggers onUserRevoked
	existing := &sqlite.UserRecord{
		ID:     "user0000000000000000000000000001",
		Name:   "viewer",
		Policy: sqlite.DefaultUserPolicy(false),
		Config: sqlite.DefaultUserConfig(),
	}
	existing.Policy.EnableRemoteAccess = true
	existing.Policy.EnableAllFolders = true
	if err := userRepo.CreateUser(ctx, existing); err != nil {
		t.Fatal(err)
	}

	var revoked []string
	m.SetOnUserRevoked(func(_ context.Context, userID string) {
		revoked = append(revoked, userID)
	})

	tightened := sqlite.DefaultUserPolicy(false)
	tightened.EnableRemoteAccess = false
	_, matched, _, _, _, err := m.resolveOrCreateUser(ctx, SourceUser{
		ID:            "src-viewer",
		Name:          "viewer",
		Policy:        tightened,
		Configuration: sqlite.DefaultUserConfig(),
	}, true, false)
	if err != nil || !matched {
		t.Fatalf("expected matched user merge, got matched=%v err=%v", matched, err)
	}
	if len(revoked) != 1 || revoked[0] != existing.ID {
		t.Fatalf("expected onUserRevoked called with %s, got %+v", existing.ID, revoked)
	}
}
