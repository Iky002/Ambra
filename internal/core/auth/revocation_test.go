package auth

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestRevokeOrderEpochAndConcurrency(t *testing.T) {
	svc, repo := setupAuthTest(t)
	ctx := context.Background()

	u, err := svc.CreateUser(ctx, "epochuser", "Pass123!", false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	res, err := svc.AuthenticateByName(ctx, u.Name, "Pass123!", "", ClientInfo{DeviceID: "dev-epoch"})
	if err != nil {
		t.Fatalf("AuthenticateByName: %v", err)
	}
	tok := res.AccessToken

	// 1. Stale putCacheWithEpoch must NOT repopulate cache after revocation bumps revokeEpoch.
	svc.mu.RLock()
	oldEpoch := svc.revokeEpoch
	svc.mu.RUnlock()

	if err := svc.RevokeToken(ctx, tok); err != nil {
		t.Fatalf("RevokeToken: %v", err)
	}
	svc.putCacheWithEpoch(tok, &TokenIdentity{Token: tok, User: u, DeviceID: "dev-epoch"}, time.Now().UTC(), oldEpoch)

	if _, err := svc.ValidateToken(ctx, tok); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("stale putCacheWithEpoch resurrected revoked token: err=%v", err)
	}

	// 2. Concurrent ValidateToken vs RevokeToken / RevokeDevice / RevokeUserTokens / RevokeAPIKey under -race.
	for iter := 0; iter < 10; iter++ {
		login, err := svc.AuthenticateByName(ctx, u.Name, "Pass123!", "", ClientInfo{DeviceID: "dev-race"})
		if err != nil {
			t.Fatalf("login #%d: %v", iter, err)
		}
		apiKey, err := svc.CreateAPIKey(ctx, "race-app")
		if err != nil {
			t.Fatalf("CreateAPIKey #%d: %v", iter, err)
		}

		// Evict cache so ValidateToken hits SQLite concurrently with revocation.
		svc.InvalidateTokenCache(login.AccessToken)
		svc.InvalidateTokenCache(apiKey.Token)

		var wg sync.WaitGroup
		for g := 0; g < 4; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = svc.ValidateToken(ctx, login.AccessToken)
				_, _ = svc.ValidateToken(ctx, apiKey.Token)
			}()
		}
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			switch i % 3 {
			case 0:
				_ = svc.RevokeToken(ctx, login.AccessToken)
			case 1:
				_ = svc.RevokeDevice(ctx, "dev-race")
			case 2:
				_ = svc.RevokeUserTokens(ctx, u.ID)
			}
		}(iter)
		go func() {
			defer wg.Done()
			_ = svc.RevokeAPIKey(ctx, apiKey.Token)
		}()
		wg.Wait()

		// After all concurrent operations complete, both tokens MUST be permanently unauthorized.
		if _, err := svc.ValidateToken(ctx, login.AccessToken); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("iter %d: revoked access token remained valid: %v", iter, err)
		}
		if _, err := svc.ValidateToken(ctx, apiKey.Token); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("iter %d: revoked API key remained valid: %v", iter, err)
		}
	}

	// 3. DB delete failure must NOT prematurely evict cache.
	freshLogin, err := svc.AuthenticateByName(ctx, u.Name, "Pass123!", "", ClientInfo{DeviceID: "dev-dbfail"})
	if err != nil {
		t.Fatalf("fresh login: %v", err)
	}
	freshKey, err := svc.CreateAPIKey(ctx, "dbfail-key")
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	if _, err := svc.ValidateToken(ctx, freshKey.Token); err != nil {
		t.Fatalf("warm API key cache: %v", err)
	}
	// Close underlying store so SQLite writes fail.
	if err := repo.Store().Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	if err := svc.RevokeToken(ctx, freshLogin.AccessToken); err == nil {
		t.Fatal("expected RevokeToken error on closed DB")
	}
	if err := svc.RevokeAPIKey(ctx, freshKey.Token); err == nil {
		t.Fatal("expected RevokeAPIKey error on closed DB")
	}
	if err := svc.RevokeDevice(ctx, "dev-dbfail"); err == nil {
		t.Fatal("expected RevokeDevice error on closed DB")
	}
	if err := svc.RevokeUserTokens(ctx, u.ID); err == nil {
		t.Fatal("expected RevokeUserTokens error on closed DB")
	}
	// Cache entries must still be intact because DB deletion failed.
	svc.mu.RLock()
	_, tokCached := svc.cache[freshLogin.AccessToken]
	_, keyCached := svc.cache[freshKey.Token]
	svc.mu.RUnlock()
	if !tokCached || !keyCached {
		t.Fatalf("expected cache preserved when DB delete fails: tokCached=%v keyCached=%v", tokCached, keyCached)
	}
}

func TestToUserDtoNilConfigDoesNotPanic(t *testing.T) {
	svc := NewService(nil, nil)
	u := &sqlite.UserRecord{
		ID:           "u-nil-cfg",
		Name:         "nilcfg",
		PasswordHash: "hashed",
		Policy:       sqlite.DefaultUserPolicy(false),
		Config:       sqlite.DefaultUserConfig(),
	}
	dtoUser := svc.ToUserDto(u)
	if dtoUser.ServerId != "" || dtoUser.PrimaryImageTag != "" || dtoUser.Name != "nilcfg" || !dtoUser.HasPassword {
		t.Fatalf("unexpected UserDto with nil cfg: %+v", dtoUser)
	}
}
