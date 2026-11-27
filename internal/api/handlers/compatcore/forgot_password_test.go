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
	"unicode"

	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/pkg/netutil"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestForgotPasswordPinRealResetAndRateLimit(t *testing.T) {
	netutil.SetLocalNetworkSubnets([]string{"10.0.0.0/8", "192.168.0.0/16"})
	t.Cleanup(func() { netutil.SetLocalNetworkSubnets(nil) })

	env := newTestEnv(t)
	authSvc := auth.NewService(env.userRepo, env.cfg)
	env.handler.SetAuthService(authSvc)

	ctx := context.Background()
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

	// 0. [M-02] Non-LAN public IP requests MUST be rejected with 403 Forbidden
	pubReq := httptest.NewRequest(http.MethodPost, "/Users/ForgotPassword", strings.NewReader(`{"EnteredUsername":"admin"}`))
	pubReq.RemoteAddr = "198.51.100.10:12345"
	pubRec := httptest.NewRecorder()
	env.router.ServeHTTP(pubRec, pubReq)
	if pubRec.Code != http.StatusForbidden {
		t.Fatalf("M-02: expected 403 for public IP on ForgotPassword, got %d", pubRec.Code)
	}
	pubPinReq := httptest.NewRequest(http.MethodPost, "/Users/ForgotPassword/Pin", strings.NewReader(`{"Pin":"123456"}`))
	pubPinReq.RemoteAddr = "198.51.100.10:12345"
	pubPinRec := httptest.NewRecorder()
	env.router.ServeHTTP(pubPinRec, pubPinReq)
	if pubPinRec.Code != http.StatusForbidden {
		t.Fatalf("M-02: expected 403 for public IP on ForgotPassword/Pin, got %d", pubPinRec.Code)
	}

	// Seed an expired PIN entry + file to verify pruning on ForgotPassword.
	expiredFile := filepath.Join(env.cfg.DataDir, "passwordreset-OLDEXP.txt")
	_ = os.WriteFile(expiredFile, []byte("expired"), 0o600)
	env.handler.mu.Lock()
	env.handler.forgotPins["000001"] = forgotPinEntry{
		UserID:    "other-user",
		UserName:  "other",
		Pin:       "000001",
		PinFile:   expiredFile,
		ExpiresAt: time.Now().UTC().Add(-5 * time.Minute),
	}
	env.handler.mu.Unlock()

	// 1. Trigger ForgotPassword from LAN for existing user -> generates 6-digit pure decimal PIN!
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

	if _, err := os.Stat(expiredFile); !os.IsNotExist(err) {
		t.Fatalf("expected expired pin file to be pruned, stat err=%v", err)
	}

	env.handler.mu.RLock()
	var firstPin string
	for k := range env.handler.forgotPins {
		firstPin = k
		break
	}
	env.handler.mu.RUnlock()
	if len(firstPin) != 6 {
		t.Fatalf("M-02: expected 6-digit PIN, got %q", firstPin)
	}
	for _, r := range firstPin {
		if !unicode.IsDigit(r) {
			t.Fatalf("M-02: expected pure decimal PIN, got %q", firstPin)
		}
	}
	if strings.Contains(strings.ToUpper(fpResp.PinFile), strings.ToUpper(firstPin)) {
		t.Fatalf("security regression: PinFile %q must NOT contain PIN %q", fpResp.PinFile, firstPin)
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

	// 2. [M-02] Strict per-IP rate-limiting WITHOUT collateral PIN destruction:
	// 5 wrong guesses from LAN IP 10.0.0.10 trigger 429 on the 6th attempt,
	// while admin's valid PIN remains intact for legitimate redemption!
	wrongPin := "999999"
	if firstPin == wrongPin {
		wrongPin = "888888"
	}
	for i := 0; i < forgotPinMaxAttempts; i++ {
		req := httptest.NewRequest(http.MethodPost, "/Users/ForgotPassword/Pin", strings.NewReader(`{"Pin":"`+wrongPin+`"}`))
		req.RemoteAddr = "10.0.0.10:12345"
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
	reqBlocked.RemoteAddr = "10.0.0.10:12345"
	blockedRec := httptest.NewRecorder()
	env.router.ServeHTTP(blockedRec, reqBlocked)
	if blockedRec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on 6th attempt from same LAN IP, got %d", blockedRec.Code)
	}

	// Valid PIN MUST NOT be destroyed by another IP's wrong guesses!
	if _, err := os.Stat(firstPinPath); err != nil {
		t.Fatalf("M-02: valid PIN file must survive another IP's wrong guesses: %v", err)
	}

	// 3. Legitimate LAN host 10.0.0.20 redeems the intact 6-digit PIN cleanly!
	redeemReq := httptest.NewRequest(http.MethodPost, "/Users/ForgotPassword/Pin", strings.NewReader(`{"Pin":"`+firstPin+`"}`))
	redeemReq.RemoteAddr = "10.0.0.20:54321"
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

	if _, err := os.Stat(firstPinPath); !os.IsNotExist(err) {
		t.Fatalf("expected PIN file removed after redeem, stat err=%v", err)
	}
	if _, err := authSvc.AuthenticateByName(ctx, "admin", "secret123", "", auth.ClientInfo{}); err == nil {
		t.Fatal("expected old password to be rejected after PIN reset")
	}
	if _, err := authSvc.AuthenticateByName(ctx, "admin", "", "", auth.ClientInfo{}); err != nil {
		t.Fatalf("expected empty password to succeed after PIN reset: %v", err)
	}
	if _, err := env.userRepo.GetAccessToken(ctx, authRes.AccessToken); err != sqlite.ErrNotFound {
		t.Fatalf("expected old access token to be revoked, got err=%v", err)
	}
	logs, total, err := env.userRepo.ListActivityLogs(ctx, 0, 10, nil)
	if err != nil || total == 0 || len(logs) == 0 {
		t.Fatalf("expected password reset activity log, got total=%d err=%v", total, err)
	}
	if logs[0].Type != "UserPasswordChanged" || logs[0].UserID != env.admin.ID {
		t.Fatalf("unexpected activity log entry: %+v", logs[0])
	}
}
