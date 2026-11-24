package auth

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func setupAuthTest(t *testing.T) (*Service, *sqlite.UserRepo) {
	t.Helper()
	dir := t.TempDir()
	store, err := sqlite.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	cfg := config.Default(dir)
	repo := sqlite.NewUserRepo(store)
	return NewService(repo, cfg), repo
}

func TestArgon2idPasswordAndEmptyPassword(t *testing.T) {
	hash, err := HashPassword("s3cr3t-pass!")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == "" {
		t.Fatalf("expected non-empty hash")
	}
	ok, err := VerifyPassword("s3cr3t-pass!", hash)
	if err != nil || !ok {
		t.Fatalf("VerifyPassword valid: ok=%v err=%v", ok, err)
	}
	ok, err = VerifyPassword("wrong", hash)
	if err != nil || ok {
		t.Fatalf("VerifyPassword invalid: ok=%v err=%v", ok, err)
	}

	// Empty password compatibility
	emptyHash, err := HashPassword("")
	if err != nil || emptyHash != "" {
		t.Fatalf("expected empty hash for empty password, got %q err=%v", emptyHash, err)
	}
	ok, err = VerifyPassword("", "")
	if err != nil || !ok {
		t.Fatalf("expected empty password to verify against empty hash")
	}
	ok, err = VerifyPassword("nonempty", "")
	if err != nil || ok {
		t.Fatalf("expected non-empty password to fail against empty hash")
	}
}

func TestEnsureDefaultAdminAndAuthLifecycle(t *testing.T) {
	svc, _ := setupAuthTest(t)
	ctx := context.Background()

	t.Setenv("AMBRA_ADMIN_USER", "rootadmin")
	t.Setenv("AMBRA_ADMIN_PASS", "rootpass123")

	if err := svc.EnsureDefaultAdmin(ctx); err != nil {
		t.Fatalf("EnsureDefaultAdmin: %v", err)
	}
	// Calling again should be a no-op
	if err := svc.EnsureDefaultAdmin(ctx); err != nil {
		t.Fatalf("EnsureDefaultAdmin second call: %v", err)
	}

	// Authenticate with valid credentials
	res, err := svc.AuthenticateByName(ctx, "rootadmin", "rootpass123", "", ClientInfo{
		Client:   "Infuse",
		Device:   "Apple TV",
		DeviceID: "dev-atv-1",
		Version:  "8.0",
	})
	if err != nil {
		t.Fatalf("AuthenticateByName: %v", err)
	}
	if res.AccessToken == "" || res.User.Name != "rootadmin" || !res.User.Policy.IsAdministrator {
		t.Fatalf("unexpected auth result: %+v", res)
	}

	// ValidateToken (cache hit)
	id, err := svc.ValidateToken(ctx, res.AccessToken)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if id.User == nil || id.User.Name != "rootadmin" || id.DeviceID != "dev-atv-1" {
		t.Fatalf("unexpected token identity: %+v", id)
	}

	// Invalidate cache and validate again (SQLite fallback)
	svc.InvalidateTokenCache(res.AccessToken)
	id2, err := svc.ValidateToken(ctx, res.AccessToken)
	if err != nil || id2.User.ID != res.User.Id {
		t.Fatalf("ValidateToken after cache clear: id=%+v err=%v", id2, err)
	}

	// Wrong password fails
	if _, err := svc.AuthenticateByName(ctx, "rootadmin", "badpass", "", ClientInfo{}); err != ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}

	// RevokeToken invalidates access
	if err := svc.RevokeToken(ctx, res.AccessToken); err != nil {
		t.Fatalf("RevokeToken: %v", err)
	}
	if _, err := svc.ValidateToken(ctx, res.AccessToken); err != ErrUnauthorized {
		t.Fatalf("expected ErrUnauthorized after revoke, got %v", err)
	}
}

func TestAPIKeyAndRevokeDevice(t *testing.T) {
	svc, _ := setupAuthTest(t)
	ctx := context.Background()

	// Create API Key
	key, err := svc.CreateAPIKey(ctx, "MoviePilot")
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	id, err := svc.ValidateToken(ctx, key.Token)
	if err != nil || !id.IsAPIKey || id.APIKeyName != "MoviePilot" {
		t.Fatalf("ValidateToken api key: id=%+v err=%v", id, err)
	}

	// Revoke API Key
	if err := svc.RevokeAPIKey(ctx, key.Token); err != nil {
		t.Fatalf("RevokeAPIKey: %v", err)
	}
	if _, err := svc.ValidateToken(ctx, key.Token); err != ErrUnauthorized {
		t.Fatalf("expected ErrUnauthorized after api key revoke, got %v", err)
	}

	// Empty-password local user + RevokeDevice
	if _, err := svc.CreateUser(ctx, "guest", "", false); err != nil {
		t.Fatalf("CreateUser guest: %v", err)
	}
	res, err := svc.AuthenticateByName(ctx, "guest", "", "", ClientInfo{DeviceID: "dev-guest-1"})
	if err != nil {
		t.Fatalf("AuthenticateByName empty password: %v", err)
	}
	if err := svc.RevokeDevice(ctx, "dev-guest-1"); err != nil {
		t.Fatalf("RevokeDevice: %v", err)
	}
	if _, err := svc.ValidateToken(ctx, res.AccessToken); err != ErrUnauthorized {
		t.Fatalf("expected ErrUnauthorized after RevokeDevice, got %v", err)
	}
}
