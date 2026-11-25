package compatcore

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

	// Verify PIN file exists on disk with 0600 permissions, filename does NOT leak PIN,
	// and file content contains the PIN.
	env.handler.mu.RLock()
	var firstPin string
	for k := range env.handler.forgotPins {
		firstPin = k
		break
	}
	env.handler.mu.RUnlock()
	if firstPin == "" {
		t.Fatal("expected active generated PIN")
	}
	if strings.Contains(strings.ToUpper(fpResp.PinFile), strings.ToUpper(firstPin)) {
		t.Fatalf("security regression: PinFile %q must NOT contain PIN %q", fpResp.PinFile, firstPin)
	}
	if fpResp.PinFile != filepath.Base(fpResp.PinFile) || strings.ContainsAny(fpResp.PinFile, `/\`) {
		t.Fatalf("security regression: PinFile %q must only return base filename", fpResp.PinFile)
	}
	firstPinPath := filepath.Join(env.cfg.DataDir, fpResp.PinFile)
	info, err := os.Stat(firstPinPath)
	if err != nil {
		t.Fatalf("expected PIN file on disk at %s: %v", firstPinPath, err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("expected PIN file mode 0600, got %o", perm)
	}
	content, _ := os.ReadFile(firstPinPath)
	if !strings.Contains(string(content), "User: admin") || !strings.Contains(string(content), "PIN: "+firstPin) {
		t.Fatalf("unexpected PIN file content: %q", string(content))
	}

	// 2. Per-IP rate-limit test: 5 wrong PIN attempts rate-limit the offending IP (6th -> 429)
	// WITHOUT wiping active PINs for other users/IPs.
	for i := 0; i < forgotPinMaxAttempts; i++ {
		req := httptest.NewRequest(http.MethodPost, "/Users/ForgotPassword/Pin", strings.NewReader(`{"Pin":"WRONG1"}`))
		req.RemoteAddr = "198.51.100.10:12345"
		badRec := httptest.NewRecorder()
		env.router.ServeHTTP(badRec, req)
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
	reqBlocked := httptest.NewRequest(http.MethodPost, "/Users/ForgotPassword/Pin", strings.NewReader(`{"Pin":"`+firstPin+`"}`))
	reqBlocked.RemoteAddr = "198.51.100.10:12345"
	blockedRec := httptest.NewRecorder()
	env.router.ServeHTTP(blockedRec, reqBlocked)
	if blockedRec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on 6th attempt from same IP, got %d", blockedRec.Code)
	}
	if _, err := os.Stat(firstPinPath); err != nil {
		t.Fatalf("expected active PIN file to remain intact after another IP brute-force, stat err=%v", err)
	}
	env.handler.mu.RLock()
	remaining := len(env.handler.forgotPins)
	env.handler.mu.RUnlock()
	if remaining != 1 {
		t.Fatalf("expected 1 active PIN preserved after per-IP rate limit, got %d", remaining)
	}

	// 3. Redeem the active PIN cleanly from a different IP.
	redeemReq := httptest.NewRequest(http.MethodPost, "/Users/ForgotPassword/Pin", strings.NewReader(`{"Pin":"`+firstPin+`"}`))
	redeemReq.RemoteAddr = "198.51.100.20:54321"
	redeemRec := httptest.NewRecorder()
	env.router.ServeHTTP(redeemRec, redeemReq)
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
	if _, err := os.Stat(firstPinPath); !os.IsNotExist(err) {
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
