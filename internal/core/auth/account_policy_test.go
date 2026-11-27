package auth

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestDisabledUserNoOracleAndCaseInsensitiveUniqueCreate(t *testing.T) {
	svc, repo := setupAuthTest(t)
	ctx := context.Background()

	u, err := svc.CreateUser(ctx, "Alice", "RealPass!1", false)
	if err != nil {
		t.Fatalf("CreateUser Alice: %v", err)
	}

	// Case-insensitive duplicate returns ErrUserExists.
	if _, err := svc.CreateUser(ctx, "alice", "other", false); !errors.Is(err, ErrUserExists) {
		t.Fatalf("expected ErrUserExists for case-insensitive duplicate, got %v", err)
	}

	// Concurrent creation of same username (different cases): exactly 1 succeeds, others get ErrUserExists.
	const workers = 6
	var wg sync.WaitGroup
	errs := make([]error, workers)
	names := []string{"Bob", "bob", "BOB", "bOb", "boB", "BoB"}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, errs[idx] = svc.CreateUser(ctx, names[idx], "pw", false)
		}(i)
	}
	wg.Wait()
	okCount := 0
	for i, e := range errs {
		if e == nil {
			okCount++
		} else if !errors.Is(e, ErrUserExists) {
			t.Fatalf("worker %d unexpected error: %v", i, e)
		}
	}
	if okCount != 1 {
		t.Fatalf("expected exactly 1 concurrent CreateUser to succeed, got %d", okCount)
	}

	// Disable Alice: wrong, empty, and valid passwords MUST all return ErrUserDisabled
	// without verifying the stored password hash (no disabled-account password oracle).
	u.IsDisabled = true
	u.Policy.IsDisabled = true
	if err := repo.UpdateUserPolicy(ctx, u.ID, u.Policy); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AuthenticateByName(ctx, "Alice", "wrong-pass", "", ClientInfo{}); !errors.Is(err, ErrUserDisabled) {
		t.Fatalf("disabled user with wrong password must return ErrUserDisabled (no password oracle), got %v", err)
	}
	if _, err := svc.AuthenticateByName(ctx, "Alice", "", "", ClientInfo{}); !errors.Is(err, ErrUserDisabled) {
		t.Fatalf("disabled user with empty password must return ErrUserDisabled (no password oracle), got %v", err)
	}
	if _, err := svc.AuthenticateByName(ctx, "Alice", "RealPass!1", "", ClientInfo{}); !errors.Is(err, ErrUserDisabled) {
		t.Fatalf("disabled user with valid password must return ErrUserDisabled, got %v", err)
	}

	// Empty-password user with wrong password returns ErrInvalidCredentials.
	if _, err := svc.CreateUser(ctx, "emptyu", "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AuthenticateByName(ctx, "emptyu", "not-empty", "", ClientInfo{}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("empty-password user with non-empty password must return ErrInvalidCredentials, got %v", err)
	}
}
