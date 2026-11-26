package quickconnect

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestQuickConnectV0107AuditRemoteAccessAndRateLimits(t *testing.T) {
	h, mux, authSvc, _, _ := setupQuickConnectTest(t)
	userRepo := h.userRepo
	ctx := context.Background()

	// 1. H-13: User with EnableRemoteAccess=false cannot redeem QuickConnect from a public IP
	lanOnlyUser, err := authSvc.CreateUser(ctx, "lanonly", "pass1234", false)
	if err != nil {
		t.Fatal(err)
	}
	lanOnlyUser.Policy.EnableRemoteAccess = false
	if err := userRepo.UpdateUserPolicy(ctx, lanOnlyUser.ID, lanOnlyUser.Policy); err != nil {
		t.Fatal(err)
	}

	initiateAndAuthorize := func(remoteAddr string, target *sqlite.UserRecord) QuickConnectResult {
		t.Helper()
		reqInit := httptest.NewRequest(http.MethodPost, "/QuickConnect/Initiate", nil)
		reqInit.RemoteAddr = remoteAddr
		recInit := httptest.NewRecorder()
		mux.ServeHTTP(recInit, reqInit)
		if recInit.Code != http.StatusOK {
			t.Fatalf("initiate (%s): status=%d body=%s", remoteAddr, recInit.Code, recInit.Body.String())
		}
		var qr QuickConnectResult
		_ = json.Unmarshal(recInit.Body.Bytes(), &qr)

		reqAuth := httptest.NewRequest(http.MethodPost, "/QuickConnect/Authorize?code="+qr.Code, nil)
		reqAuth.RemoteAddr = "127.0.0.1:12345"
		reqAuth = reqAuth.WithContext(middleware.WithAuthContext(reqAuth.Context(), &middleware.AuthContext{User: target}))
		recAuth := httptest.NewRecorder()
		mux.ServeHTTP(recAuth, reqAuth)
		if recAuth.Code != http.StatusOK {
			t.Fatalf("authorize: status=%d body=%s", recAuth.Code, recAuth.Body.String())
		}
		return qr
	}

	// Redeem from public IP -> 403 Forbidden
	qrPub := initiateAndAuthorize("203.0.113.10:12345", lanOnlyUser)
	bodyPub, _ := json.Marshal(map[string]string{"Secret": qrPub.Secret})
	reqPub := httptest.NewRequest(http.MethodPost, "/Users/AuthenticateWithQuickConnect", bytes.NewReader(bodyPub))
	reqPub.RemoteAddr = "203.0.113.10:12345"
	recPub := httptest.NewRecorder()
	mux.ServeHTTP(recPub, reqPub)
	if recPub.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for remote-disabled user from public IP, got %d (%s)", recPub.Code, recPub.Body.String())
	}

	// Redeem from LAN IP -> 200 OK
	qrLAN := initiateAndAuthorize("192.168.1.50:12345", lanOnlyUser)
	bodyLAN, _ := json.Marshal(map[string]string{"Secret": qrLAN.Secret})
	reqLAN := httptest.NewRequest(http.MethodPost, "/Users/AuthenticateWithQuickConnect", bytes.NewReader(bodyLAN))
	reqLAN.RemoteAddr = "192.168.1.50:12345"
	recLAN := httptest.NewRecorder()
	mux.ServeHTTP(recLAN, reqLAN)
	if recLAN.Code != http.StatusOK {
		t.Fatalf("expected 200 for remote-disabled user from LAN IP, got %d (%s)", recLAN.Code, recLAN.Body.String())
	}

	// 2. M-9: IPv6 /64 normalization shares rate limit bucket across addresses in same /64 subnet
	if got := normalizeRateLimitIP("2001:db8:abcd:0012::1"); got != "2001:db8:abcd:12::/64" {
		t.Fatalf("unexpected IPv6 /64 key: %q", got)
	}
	if got := normalizeRateLimitIP("2001:db8:abcd:0012:ffff::99"); got != "2001:db8:abcd:12::/64" {
		t.Fatalf("expected same /64 prefix, got %q", got)
	}
	if got := normalizeRateLimitIP(""); got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
	if got := normalizeRateLimitIP("not-an-ip"); got != "not-an-ip" {
		t.Fatalf("expected passthrough for invalid ip, got %q", got)
	}

	for i := 0; i < maxInitiatesPerIPPerMin; i++ {
		req := httptest.NewRequest(http.MethodPost, "/QuickConnect/Initiate", nil)
		req.RemoteAddr = fmt.Sprintf("[2001:db8:1111:2222::%x]:5555", i+1)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("ipv6 initiate #%d expected 200, got %d", i, rec.Code)
		}
	}
	reqIPv6Ovf := httptest.NewRequest(http.MethodPost, "/QuickConnect/Initiate", nil)
	reqIPv6Ovf.RemoteAddr = "[2001:db8:1111:2222::ffff]:5555"
	recIPv6Ovf := httptest.NewRecorder()
	mux.ServeHTTP(recIPv6Ovf, reqIPv6Ovf)
	if recIPv6Ovf.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 for 11th request from same IPv6 /64, got %d", recIPv6Ovf.Code)
	}

	// 3. M-9: maxPendingSessions checked BEFORE writing initiateRates
	now := h.now()
	h.mu.Lock()
	h.bySecret = make(map[string]*pendingSession, maxPendingSessions)
	h.initiateRates = make(map[string]*failWindow)
	for i := 0; i < maxPendingSessions; i++ {
		h.bySecret[fmt.Sprintf("s-%d", i)] = &pendingSession{expiresAt: now.Add(time.Hour)}
	}
	h.mu.Unlock()

	reqGlobalFull := httptest.NewRequest(http.MethodPost, "/QuickConnect/Initiate", nil)
	reqGlobalFull.RemoteAddr = "198.51.100.77:9999"
	recGlobalFull := httptest.NewRecorder()
	mux.ServeHTTP(recGlobalFull, reqGlobalFull)
	if recGlobalFull.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 when maxPendingSessions reached, got %d", recGlobalFull.Code)
	}
	h.mu.Lock()
	_, wroteRate := h.initiateRates["198.51.100.77"]
	h.bySecret = make(map[string]*pendingSession)
	// Fill initiateRates & failedAttempts to maxRateLimitBuckets (4096)
	for i := 0; i < maxRateLimitBuckets; i++ {
		h.initiateRates[fmt.Sprintf("ip-%d", i)] = &failWindow{count: 1, resetAt: now.Add(time.Hour)}
		h.failedAttempts[fmt.Sprintf("u-%d", i)] = &failWindow{count: 1, resetAt: now.Add(time.Hour)}
	}
	h.mu.Unlock()
	if wroteRate {
		t.Fatal("initiateRates must not be mutated when global maxPendingSessions is already full")
	}

	// 4097th unique IP on initiate -> 429
	reqBucketFull := httptest.NewRequest(http.MethodPost, "/QuickConnect/Initiate", nil)
	reqBucketFull.RemoteAddr = "198.51.100.88:9999"
	recBucketFull := httptest.NewRecorder()
	mux.ServeHTTP(recBucketFull, reqBucketFull)
	if recBucketFull.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 when initiateRates reaches 4096 buckets, got %d", recBucketFull.Code)
	}

	// 4097th unique user failing authorize -> 429
	reqFailBucket := httptest.NewRequest(http.MethodPost, "/QuickConnect/Authorize?code=000000", nil)
	reqFailBucket.RemoteAddr = "127.0.0.1:12345"
	reqFailBucket = reqFailBucket.WithContext(middleware.WithAuthContext(reqFailBucket.Context(), &middleware.AuthContext{User: lanOnlyUser}))
	recFailBucket := httptest.NewRecorder()
	mux.ServeHTTP(recFailBucket, reqFailBucket)
	if recFailBucket.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 when failedAttempts reaches 4096 buckets, got %d", recFailBucket.Code)
	}
}
