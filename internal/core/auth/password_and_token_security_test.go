package auth

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestVerifyPasswordParameterBounds(t *testing.T) {
	salt := base64.RawStdEncoding.EncodeToString([]byte("0123456789abcdef"))
	hash32 := base64.RawStdEncoding.EncodeToString(make([]byte, 32))
	hashShort := base64.RawStdEncoding.EncodeToString(make([]byte, 8))

	bad := []string{
		fmt.Sprintf("$argon2id$v=18$m=65536,t=3,p=2$%s$%s", salt, hash32),    // wrong version
		fmt.Sprintf("$argon2id$v=20$m=65536,t=3,p=2$%s$%s", salt, hash32),    // future version
		fmt.Sprintf("$argon2id$v=19$m=65536,t=3,p=0$%s$%s", salt, hash32),    // p < 1
		fmt.Sprintf("$argon2id$v=19$m=65536,t=3,p=17$%s$%s", salt, hash32),   // p > 16
		fmt.Sprintf("$argon2id$v=19$m=65536,t=0,p=2$%s$%s", salt, hash32),    // t < 1
		fmt.Sprintf("$argon2id$v=19$m=65536,t=17,p=2$%s$%s", salt, hash32),   // t > 16
		fmt.Sprintf("$argon2id$v=19$m=4096,t=3,p=2$%s$%s", salt, hash32),     // m < 8192
		fmt.Sprintf("$argon2id$v=19$m=1048576,t=3,p=2$%s$%s", salt, hash32),  // m > 512*1024
		fmt.Sprintf("$argon2id$v=19$m=65536,t=3,p=2$%s$%s", salt, hashShort), // hash < 16 bytes
		fmt.Sprintf("$argon2id$v=19x$m=65536,t=3,p=2$%s$%s", salt, hash32),   // trailing junk in v
		fmt.Sprintf("$argon2id$v=19$m=65536,t=3,p=2x$%s$%s", salt, hash32),   // trailing junk in params
		fmt.Sprintf("prefix$argon2id$v=19$m=65536,t=3,p=2$%s$%s", salt, hash32),
	}
	for _, h := range bad {
		ok, err := VerifyPassword("secret", h)
		if ok || err == nil {
			t.Errorf("VerifyPassword(%q) expected error, got ok=%v err=%v", h, ok, err)
		}
	}
}

func TestChangePasswordRevokesTokensAndKeepToken(t *testing.T) {
	svc, _ := setupAuthTest(t)
	ctx := context.Background()

	u, err := svc.CreateUser(ctx, "carol", "pw1", false)
	if err != nil {
		t.Fatal(err)
	}
	r1, err := svc.AuthenticateByName(ctx, "carol", "pw1", "", ClientInfo{DeviceID: "d1"})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := svc.AuthenticateByName(ctx, "carol", "pw1", "", ClientInfo{DeviceID: "d2"})
	if err != nil {
		t.Fatal(err)
	}

	// ChangePasswordKeepToken revokes r2 but preserves r1.
	if err := svc.ChangePasswordKeepToken(ctx, u.ID, "pw1", "pw2", false, r1.AccessToken); err != nil {
		t.Fatalf("ChangePasswordKeepToken: %v", err)
	}
	if _, err := svc.ValidateToken(ctx, r1.AccessToken); err != nil {
		t.Fatalf("expected kept token to remain valid, got %v", err)
	}
	if _, err := svc.ValidateToken(ctx, r2.AccessToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected other token to be revoked, got %v", err)
	}

	// Full ChangePassword revokes all remaining tokens including r1.
	if err := svc.ChangePassword(ctx, u.ID, "pw2", "pw3", false); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	if _, err := svc.ValidateToken(ctx, r1.AccessToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected all tokens revoked after ChangePassword, got %v", err)
	}

	// RevokeUserTokens revokes newly issued tokens.
	r3, err := svc.AuthenticateByName(ctx, "carol", "pw3", "", ClientInfo{DeviceID: "d3"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RevokeUserTokens(ctx, u.ID); err != nil {
		t.Fatalf("RevokeUserTokens: %v", err)
	}
	if _, err := svc.ValidateToken(ctx, r3.AccessToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected token revoked after RevokeUserTokens, got %v", err)
	}
}

func TestValidateTokenConcurrentLastFlushRaceAndCacheCap(t *testing.T) {
	svc, _ := setupAuthTest(t)
	ctx := context.Background()

	_, err := svc.CreateUser(ctx, "dave", "pw", false)
	if err != nil {
		t.Fatal(err)
	}
	res, err := svc.AuthenticateByName(ctx, "dave", "pw", "", ClientInfo{DeviceID: "d-race"})
	if err != nil {
		t.Fatal(err)
	}

	// Force lastFlush stale and hammer ValidateToken concurrently under -race.
	svc.mu.Lock()
	svc.cache[res.AccessToken].lastFlush = time.Now().UTC().Add(-time.Minute)
	svc.mu.Unlock()

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := svc.ValidateToken(ctx, res.AccessToken); err != nil {
				t.Errorf("concurrent ValidateToken failed: %v", err)
			}
		}()
	}
	wg.Wait()
	time.Sleep(40 * time.Millisecond)

	// Verify cache capacity cap & expired eviction.
	now := time.Now().UTC()
	for i := 0; i < maxTokenCacheSize+50; i++ {
		tok := fmt.Sprintf("tok-%05d", i)
		svc.putCache(tok, &TokenIdentity{Token: tok, User: &sqlite.UserRecord{ID: "u1"}}, now.Add(time.Duration(i)*time.Millisecond))
	}
	svc.mu.RLock()
	cacheLen := len(svc.cache)
	svc.mu.RUnlock()
	if cacheLen > maxTokenCacheSize {
		t.Fatalf("cache exceeded maxTokenCacheSize (%d): got %d", maxTokenCacheSize, cacheLen)
	}

	// Expired entry is purged on ValidateToken.
	svc.mu.Lock()
	svc.cache["expired-tok"] = &cachedToken{
		identity:  &TokenIdentity{Token: "expired-tok"},
		expiresAt: time.Now().UTC().Add(-time.Second),
	}
	svc.mu.Unlock()
	if _, err := svc.ValidateToken(ctx, "expired-tok"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized for expired cache token, got %v", err)
	}
	svc.mu.RLock()
	_, stillThere := svc.cache["expired-tok"]
	svc.mu.RUnlock()
	if stillThere {
		t.Fatal("expected expired cache entry to be purged")
	}
}

func TestEnsureDefaultAdminSkipEnv(t *testing.T) {
	svc, repo := setupAuthTest(t)
	ctx := context.Background()

	t.Setenv("AMBRA_SKIP_DEFAULT_ADMIN", "true")
	if err := svc.EnsureDefaultAdmin(ctx); err != nil {
		t.Fatalf("EnsureDefaultAdmin skipped: %v", err)
	}
	cnt, err := repo.CountUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cnt != 0 {
		t.Fatalf("expected 0 users when AMBRA_SKIP_DEFAULT_ADMIN=true, got %d", cnt)
	}
}
