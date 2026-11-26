package migration

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestAuditV0107_M19_PasswordlessAdminCreatedDisabledWithNotice(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "mig.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	userRepo := sqlite.NewUserRepo(store)
	m := NewMigrator(store, userRepo)

	uid, matched, created, conflict, notice, err := m.resolveOrCreateUser(context.Background(), SourceUser{
		ID:          "src-admin-nopass",
		Name:        "migrated_admin",
		HasPassword: false,
		Policy: dto.UserPolicy{
			IsAdministrator: true,
			IsDisabled:      false,
		},
	}, true, false)
	if err != nil {
		t.Fatalf("resolveOrCreateUser: %v", err)
	}
	if matched || !created || conflict != nil {
		t.Fatalf("expected created=true matched=false conflict=nil, got matched=%v created=%v conflict=%v", matched, created, conflict)
	}
	if !strings.Contains(strings.ToLower(notice), "disabled") {
		t.Fatalf("expected notice explaining disabled passwordless admin, got %q", notice)
	}

	u, err := userRepo.GetUserByID(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	if !u.IsAdmin || !u.IsDisabled || !u.Policy.IsDisabled {
		t.Fatalf("expected passwordless migrated admin to be IsAdmin=true & IsDisabled=true, got IsAdmin=%v IsDisabled=%v Policy.IsDisabled=%v",
			u.IsAdmin, u.IsDisabled, u.Policy.IsDisabled)
	}
}
