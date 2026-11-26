package quickconnect

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ambra-media/ambra/internal/api/middleware"
)

func TestQuickConnectV0109AuditL17AdminRemoteAccessAndGetInitiate(t *testing.T) {
	h, mux, authSvc, _, _ := setupQuickConnectTest(t)
	userRepo := h.userRepo
	ctx := context.Background()

	// 1. L-17: GET /QuickConnect/Initiate works alongside POST /QuickConnect/Initiate
	reqGet := httptest.NewRequest(http.MethodGet, "/QuickConnect/Initiate", nil)
	reqGet.RemoteAddr = "127.0.0.1:41000"
	recGet := httptest.NewRecorder()
	mux.ServeHTTP(recGet, reqGet)
	if recGet.Code != http.StatusOK {
		t.Fatalf("L-17: GET /QuickConnect/Initiate expected 200, got %d (%s)", recGet.Code, recGet.Body.String())
	}
	var initRes QuickConnectResult
	if err := json.Unmarshal(recGet.Body.Bytes(), &initRes); err != nil || initRes.Secret == "" || initRes.Code == "" {
		t.Fatalf("L-17: unexpected GET initiate result: %+v err=%v", initRes, err)
	}

	// 2. L-17: Admin with EnableRemoteAccess=false is NOT exempted from remote access restriction
	adminUser, err := authSvc.CreateUser(ctx, "admin_lan_only", "pass1234", true)
	if err != nil {
		t.Fatal(err)
	}
	adminUser.Policy.EnableRemoteAccess = false
	if err := userRepo.UpdateUserPolicy(ctx, adminUser.ID, adminUser.Policy); err != nil {
		t.Fatal(err)
	}

	reqAuthz := httptest.NewRequest(http.MethodPost, "/QuickConnect/Authorize?code="+initRes.Code, nil)
	reqAuthz.RemoteAddr = "127.0.0.1:41001"
	reqAuthz = reqAuthz.WithContext(middleware.WithAuthContext(ctx, &middleware.AuthContext{User: adminUser}))
	recAuthz := httptest.NewRecorder()
	mux.ServeHTTP(recAuthz, reqAuthz)
	if recAuthz.Code != http.StatusOK {
		t.Fatalf("authorize expected 200, got %d (%s)", recAuthz.Code, recAuthz.Body.String())
	}

	// Redeem from public IP must return 403 even for admin when EnableRemoteAccess=false
	redeemBody, _ := json.Marshal(map[string]string{"Secret": initRes.Secret})
	reqRedeem := httptest.NewRequest(http.MethodPost, "/Users/AuthenticateWithQuickConnect", bytes.NewReader(redeemBody))
	reqRedeem.RemoteAddr = "198.51.100.42:50000"
	recRedeem := httptest.NewRecorder()
	mux.ServeHTTP(recRedeem, reqRedeem)
	if recRedeem.Code != http.StatusForbidden {
		t.Fatalf("L-17: expected 403 for admin with EnableRemoteAccess=false from public IP, got %d (%s)", recRedeem.Code, recRedeem.Body.String())
	}
	_ = h
}
