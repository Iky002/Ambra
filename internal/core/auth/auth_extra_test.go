package auth

import (
	"context"
	"testing"
	"time"
)

func TestVerifyPasswordErrorBranches(t *testing.T) {
	hash, err := HashPassword("validpass")
	if err != nil {
		t.Fatal(err)
	}
	// Empty password against non-empty hash
	ok, err := VerifyPassword("", hash)
	if err != nil || ok {
		t.Fatalf("expected false, nil; got ok=%v err=%v", ok, err)
	}

	badHashes := []string{
		"not-a-hash",
		"$bcrypt$v=19$m=65536,t=3,p=2$aaaa$bbbb",
		"$argon2id$v=bad$m=65536,t=3,p=2$aaaa$bbbb",
		"$argon2id$v=19$badparams$aaaa$bbbb",
		"$argon2id$v=19$m=65536,t=3,p=2$!!!notb64!!!$bbbb",
		"$argon2id$v=19$m=65536,t=3,p=2$AAAA$!!!notb64!!!",
	}
	for _, bh := range badHashes {
		if ok, err := VerifyPassword("pw", bh); err == nil || ok {
			t.Errorf("VerifyPassword(%q) expected error, got ok=%v err=%v", bh, ok, err)
		}
	}
}

func TestEnsureDefaultAdminFallbackAndUserErrors(t *testing.T) {
	svc, repo := setupAuthTest(t)
	ctx := context.Background()

	// Unset env vars so default "admin" / "admin" are used.
	t.Setenv("AMBRA_ADMIN_USER", "")
	// Ensure AMBRA_ADMIN_PASS is not set by testing with empty/set or just authenticating
	if err := svc.EnsureDefaultAdmin(ctx); err != nil {
		t.Fatalf("EnsureDefaultAdmin default: %v", err)
	}
	u, err := repo.GetUserByName(ctx, "admin")
	if err != nil || !u.IsAdmin {
		t.Fatalf("expected default admin user, got u=%+v err=%v", u, err)
	}

	// Empty username error
	if _, err := svc.CreateUser(ctx, "   ", "pw", false); err == nil {
		t.Fatal("expected error for empty username")
	}

	// Duplicate username error
	if _, err := svc.CreateUser(ctx, "admin", "pw", false); err != ErrUserExists {
		t.Fatalf("expected ErrUserExists, got %v", err)
	}
}

func TestAuthenticateAndValidateEdgeCases(t *testing.T) {
	svc, repo := setupAuthTest(t)
	ctx := context.Background()

	// Unknown user -> ErrInvalidCredentials
	if _, err := svc.AuthenticateByName(ctx, "nobody", "pw", "", ClientInfo{}); err != ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}

	// User with empty password + legacy SHA1("") + empty ClientInfo defaults
	emptyUser, err := svc.CreateUser(ctx, "nopass", "", false)
	if err != nil {
		t.Fatal(err)
	}
	res, err := svc.AuthenticateByName(ctx, "nopass", "", sha1Hex(""), ClientInfo{})
	if err != nil {
		t.Fatalf("AuthenticateByName empty pw + legacy sha1: %v", err)
	}
	if res.SessionInfo.DeviceName != "Unknown Device" || res.SessionInfo.Client != "Ambra Client" || res.SessionInfo.ApplicationVersion != "1.0.0" {
		t.Fatalf("expected default client fields, got %+v", res.SessionInfo)
	}

	// ValidateToken empty token -> ErrUnauthorized
	if _, err := svc.ValidateToken(ctx, "   "); err != ErrUnauthorized {
		t.Fatalf("expected ErrUnauthorized for empty token, got %v", err)
	}

	// Trigger activityFlushWindow branch on cached token
	svc.mu.Lock()
	svc.cache[res.AccessToken].lastFlush = time.Now().UTC().Add(-time.Minute)
	svc.mu.Unlock()
	if _, err := svc.ValidateToken(ctx, res.AccessToken); err != nil {
		t.Fatalf("ValidateToken flush branch: %v", err)
	}
	time.Sleep(30 * time.Millisecond)

	// Cached disabled user -> ErrUserDisabled
	svc.mu.Lock()
	svc.cache[res.AccessToken].identity.User.IsDisabled = true
	svc.mu.Unlock()
	if _, err := svc.ValidateToken(ctx, res.AccessToken); err != ErrUserDisabled {
		t.Fatalf("expected ErrUserDisabled for cached disabled user, got %v", err)
	}

	// Disable user in DB and test AuthenticateByName + cache-miss ValidateToken
	emptyUser.IsDisabled = true
	emptyUser.Policy.IsDisabled = true
	if err := repo.UpdateUserPolicy(ctx, emptyUser.ID, emptyUser.Policy); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AuthenticateByName(ctx, "nopass", "", "", ClientInfo{}); err != ErrUserDisabled {
		t.Fatalf("expected ErrUserDisabled on AuthenticateByName, got %v", err)
	}
	if _, err := svc.ValidateToken(ctx, res.AccessToken); err != ErrUserDisabled {
		t.Fatalf("expected ErrUserDisabled on DB cache-miss ValidateToken, got %v", err)
	}

	// Corrupt password hash in DB -> VerifyPassword error -> ErrInvalidCredentials
	corruptUser, err := svc.CreateUser(ctx, "corrupt", "pw", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateUserPassword(ctx, corruptUser.ID, "bad-hash-format"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AuthenticateByName(ctx, "corrupt", "pw", "", ClientInfo{}); err != ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials for corrupt hash, got %v", err)
	}

	// Token whose user was deleted -> ErrUnauthorized
	delUser, err := svc.CreateUser(ctx, "tempuser", "pw", false)
	if err != nil {
		t.Fatal(err)
	}
	delRes, err := svc.AuthenticateByName(ctx, "tempuser", "pw", "", ClientInfo{DeviceID: "d-del"})
	if err != nil {
		t.Fatal(err)
	}
	svc.InvalidateUserCache(delUser.ID)
	if err := repo.DeleteUser(ctx, delUser.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ValidateToken(ctx, delRes.AccessToken); err != ErrUnauthorized {
		t.Fatalf("expected ErrUnauthorized for deleted user token, got %v", err)
	}
}

func TestAPIKeysAndChangePassword(t *testing.T) {
	svc, _ := setupAuthTest(t)
	ctx := context.Background()

	// CreateAPIKey with empty appName defaults to "API Client"
	key, err := svc.CreateAPIKey(ctx, "   ")
	if err != nil {
		t.Fatal(err)
	}
	if key.AppName != "API Client" {
		t.Fatalf("expected API Client, got %q", key.AppName)
	}
	keys, err := svc.ListAPIKeys(ctx)
	if err != nil || len(keys) != 1 {
		t.Fatalf("ListAPIKeys: len=%d err=%v", len(keys), err)
	}

	// Validate API key twice (miss then cache hit)
	if _, err := svc.ValidateToken(ctx, key.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ValidateToken(ctx, key.Token); err != nil {
		t.Fatal(err)
	}

	// ChangePassword: user not found
	if err := svc.ChangePassword(ctx, "nonexistent", "", "newpw", true); err != ErrUserNotFound {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}

	u, err := svc.CreateUser(ctx, "bob", "oldpw", false)
	if err != nil {
		t.Fatal(err)
	}
	// Non-admin wrong current password
	if err := svc.ChangePassword(ctx, u.ID, "wrongpw", "newpw", false); err != ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
	// Non-admin valid current password
	if err := svc.ChangePassword(ctx, u.ID, "oldpw", "midpw", false); err != nil {
		t.Fatalf("ChangePassword non-admin: %v", err)
	}
	// Admin override without current password
	if err := svc.ChangePassword(ctx, u.ID, "", "finalpw", true); err != nil {
		t.Fatalf("ChangePassword admin: %v", err)
	}
	if _, err := svc.AuthenticateByName(ctx, "bob", "finalpw", "", ClientInfo{}); err != nil {
		t.Fatalf("AuthenticateByName with new password failed: %v", err)
	}
}
