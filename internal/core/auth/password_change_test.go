package auth

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestChangePasswordEmptyInitialPasswordAndAvatarTag(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	cfg, err := config.Load(filepath.Join(tmp, "data"))
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	store, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	userRepo := sqlite.NewUserRepo(store)
	svc := NewService(userRepo, cfg)

	// Create user with empty password
	u, err := svc.CreateUser(ctx, "nopass", "", false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if u.PasswordHash != "" {
		t.Fatalf("expected empty password hash, got %q", u.PasswordHash)
	}

	// Non-admin with wrong non-empty currentPw should fail
	if err := svc.ChangePassword(ctx, u.ID, "wrong", "newpass123", false); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials for non-empty currentPw on empty-pw user, got %v", err)
	}

	// Non-admin with empty currentPw should succeed
	if err := svc.ChangePassword(ctx, u.ID, "", "newpass123", false); err != nil {
		t.Fatalf("ChangePassword from empty password failed: %v", err)
	}

	// Verify new password works for login
	if _, err := svc.AuthenticateByName(ctx, "nopass", "newpass123", "", ClientInfo{DeviceID: "d1"}); err != nil {
		t.Fatalf("AuthenticateByName with new password failed: %v", err)
	}

	// PrimaryImageTag empty before avatar exists
	dtoBefore := svc.ToUserDto(u)
	if dtoBefore.PrimaryImageTag != "" {
		t.Fatalf("expected empty PrimaryImageTag before avatar upload, got %q", dtoBefore.PrimaryImageTag)
	}
	if UserPrimaryImageTag("", u.ID) != "" || UserPrimaryImageTag(cfg.DataDir, "") != "" {
		t.Fatal("expected empty tag for blank dataDir or userID")
	}

	// Fallback path <DataDir>/users/<id>/primary.jpg
	fallbackPath := filepath.Join(cfg.DataDir, "users", u.ID, "primary.jpg")
	if err := os.MkdirAll(filepath.Dir(fallbackPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(fallbackPath, []byte("fake-jpg"), 0o644); err != nil {
		t.Fatalf("WriteFile fallback: %v", err)
	}
	dtoFallback := svc.ToUserDto(u)
	if dtoFallback.PrimaryImageTag == "" {
		t.Fatal("expected PrimaryImageTag to be populated from fallback users/<id>/primary.jpg")
	}

	// Primary compatcore path <DataDir>/metadata/users/<id>/primary_0.img takes precedence
	primaryPath := filepath.Join(cfg.DataDir, "metadata", "users", u.ID, "primary_0.img")
	if err := os.MkdirAll(filepath.Dir(primaryPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(primaryPath, []byte("fake-png-bytes-primary"), 0o644); err != nil {
		t.Fatalf("WriteFile primary: %v", err)
	}
	dtoPrimary := svc.ToUserDto(u)
	if dtoPrimary.PrimaryImageTag == "" || dtoPrimary.PrimaryImageTag == dtoFallback.PrimaryImageTag {
		t.Fatalf("expected PrimaryImageTag to update from metadata/users/<id>/primary_0.img, got %q", dtoPrimary.PrimaryImageTag)
	}
}
