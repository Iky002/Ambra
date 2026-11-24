package compatcore

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestForgotPasswordPinRealResetAndRateLimit(t *testing.T) {
	env := newTestEnv(t)
	authSvc := auth.NewService(env.userRepo, env.cfg)
	env.handler.SetAuthService(authSvc)

	ctx := context.Background()
	// Give admin a known password and an active access token.
	if err := authSvc.ChangePassword(ctx, env.admin.ID, "", "secret123", true); err != nil {
		t.Fatalf("set initial password: %v", err)
	}
	authRes, err := authSvc.AuthenticateByName(ctx, "admin", "secret123", "", auth.ClientInfo{DeviceID: "dev-1"})
	if err != nil {
		t.Fatalf("initial auth failed: %v", err)
	}
	if _, err := authSvc.ValidateToken(ctx, authRes.AccessToken); err != nil {
		t.Fatalf("expected valid token before reset: %v", err)
	}

	// Seed an expired PIN entry + file to verify pruning on ForgotPassword.
	expiredFile := filepath.Join(env.cfg.DataDir, "passwordreset-OLDEXP.txt")
	_ = os.WriteFile(expiredFile, []byte("expired"), 0o600)
	env.handler.mu.Lock()
	env.handler.forgotPins["OLDEXP"] = forgotPinEntry{
		UserID:    env.admin.ID,
		UserName:  "admin",
		Pin:       "OLDEXP",
		PinFile:   expiredFile,
		ExpiresAt: time.Now().UTC().Add(-5 * time.Minute),
	}
	env.handler.mu.Unlock()

	// 1. Trigger ForgotPassword for existing user.
	rec := doReq(t, env.router, "POST", "/Users/ForgotPassword", `{"EnteredUsername":"admin"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("ForgotPassword got %d", rec.Code)
	}
	var fpResp struct {
		Action  string `json:"Action"`
		PinFile string `json:"PinFile"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &fpResp); err != nil {
		t.Fatal(err)
	}
	if fpResp.Action != "PinCode" || fpResp.PinFile == "" {
		t.Fatalf("unexpected ForgotPassword response: %+v", fpResp)
	}

	// Verify expired entry & file were pruned.
	if _, err := os.Stat(expiredFile); !os.IsNotExist(err) {
		t.Fatalf("expected expired pin file to be pruned, stat err=%v", err)
	}

	// Verify PIN file exists on disk with 0600 permissions and contains the PIN.
	info, err := os.Stat(fpResp.PinFile)
	if err != nil {
		t.Fatalf("expected PIN file on disk at %s: %v", fpResp.PinFile, err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("expected PIN file mode 0600, got %o", perm)
	}
	content, _ := os.ReadFile(fpResp.PinFile)
	if !strings.Contains(string(content), "User: admin") {
		t.Fatalf("unexpected PIN file content: %q", string(content))
	}

	// 2. Rate-limit test: 5 wrong PIN attempts invalidate the active PIN and remove its file.
	for i := 0; i < forgotPinMaxAttempts; i++ {
		badRec := doReq(t, env.router, "POST", "/Users/ForgotPassword/Pin", `{"Pin":"WRONG1"}`)
		if badRec.Code != http.StatusOK {
			t.Fatalf("wrong pin attempt %d got %d", i+1, badRec.Code)
		}
		var r struct {
			Success bool `json:"Success"`
		}
		_ = json.Unmarshal(badRec.Body.Bytes(), &r)
		if r.Success {
			t.Fatalf("wrong pin attempt %d unexpectedly succeeded", i+1)
		}
	}
	if _, err := os.Stat(fpResp.PinFile); !os.IsNotExist(err) {
		t.Fatalf("expected PIN file to be deleted after %d wrong attempts", forgotPinMaxAttempts)
	}
	env.handler.mu.RLock()
	remaining := len(env.handler.forgotPins)
	env.handler.mu.RUnlock()
	if remaining != 0 {
		t.Fatalf("expected 0 active PINs after rate-limit invalidation, got %d", remaining)
	}

	// 3. Issue a fresh PIN and redeem it cleanly.
	rec2 := doReq(t, env.router, "POST", "/Users/ForgotPassword", `{"EnteredUsername":"admin"}`)
	if rec2.Code != http.StatusOK {
		t.Fatalf("second ForgotPassword got %d", rec2.Code)
	}
	var fpResp2 struct {
		PinFile string `json:"PinFile"`
	}
	_ = json.Unmarshal(rec2.Body.Bytes(), &fpResp2)

	env.handler.mu.RLock()
	var validPin string
	for k := range env.handler.forgotPins {
		validPin = k
		break
	}
	env.handler.mu.RUnlock()
	if validPin == "" {
		t.Fatal("expected generated valid PIN")
	}

	redeemRec := doReq(t, env.router, "POST", "/Users/ForgotPassword/Pin", `{"Pin":"`+validPin+`"}`)
	if redeemRec.Code != http.StatusOK {
		t.Fatalf("ForgotPassword/Pin got %d body=%s", redeemRec.Code, redeemRec.Body.String())
	}
	var redeemResp struct {
		Success    bool     `json:"Success"`
		UsersReset []string `json:"UsersReset"`
	}
	_ = json.Unmarshal(redeemRec.Body.Bytes(), &redeemResp)
	if !redeemResp.Success || len(redeemResp.UsersReset) != 1 || redeemResp.UsersReset[0] != "admin" {
		t.Fatalf("unexpected redeem response: %+v", redeemResp)
	}

	// PIN file removed on disk.
	if _, err := os.Stat(fpResp2.PinFile); !os.IsNotExist(err) {
		t.Fatalf("expected PIN file removed after redeem, stat err=%v", err)
	}

	// Old password no longer works; empty password works.
	if _, err := authSvc.AuthenticateByName(ctx, "admin", "secret123", "", auth.ClientInfo{}); err == nil {
		t.Fatal("expected old password to be rejected after PIN reset")
	}
	if _, err := authSvc.AuthenticateByName(ctx, "admin", "", "", auth.ClientInfo{}); err != nil {
		t.Fatalf("expected empty password to succeed after PIN reset: %v", err)
	}

	// Previous access token was revoked in DB (and user cache invalidated).
	if _, err := env.userRepo.GetAccessToken(ctx, authRes.AccessToken); err != sqlite.ErrNotFound {
		t.Fatalf("expected old access token to be revoked, got err=%v", err)
	}

	// Activity log entry recorded.
	logs, total, err := env.userRepo.ListActivityLogs(ctx, 0, 10, nil)
	if err != nil || total == 0 || len(logs) == 0 {
		t.Fatalf("expected password reset activity log, got total=%d err=%v", total, err)
	}
	if logs[0].Type != "UserPasswordChanged" || logs[0].UserID != env.admin.ID {
		t.Fatalf("unexpected activity log entry: %+v", logs[0])
	}
}
