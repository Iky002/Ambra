package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/handlers/quickconnect"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/server"
)

func TestServerQuickConnectRoutingAndEmbyPrefix(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Default(tmp)
	srv, err := server.New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	defer srv.Close()

	// Login as default admin to authorize QuickConnect
	loginRes, err := srv.Auth.AuthenticateByName(context.Background(), "admin", "admin", "", auth.ClientInfo{DeviceID: "admin-phone"})
	if err != nil {
		t.Fatalf("login admin: %v", err)
	}

	// 1. /emby/quickconnect/enabled (lowercase + /emby prefix)
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/emby/quickconnect/enabled", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("enabled status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 2. POST /emby/quickconnect/initiate
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/emby/quickconnect/initiate", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("initiate status=%d body=%s", rec.Code, rec.Body.String())
	}
	var qc quickconnect.QuickConnectResult
	if err := json.Unmarshal(rec.Body.Bytes(), &qc); err != nil {
		t.Fatal(err)
	}

	// 3. POST /emby/quickconnect/authorize with JSON body
	authzBody, _ := json.Marshal(map[string]string{"Code": qc.Code})
	authzReq := httptest.NewRequest(http.MethodPost, "/emby/quickconnect/authorize", bytes.NewReader(authzBody))
	authzReq.Header.Set("X-Emby-Token", loginRes.AccessToken)
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, authzReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("authorize status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 4. POST /emby/users/authenticatewithquickconnect (public, through chi -> mediaMux)
	authBody, _ := json.Marshal(map[string]string{"Secret": qc.Secret})
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/emby/users/authenticatewithquickconnect", bytes.NewReader(authBody)))
	if rec.Code != http.StatusOK {
		t.Fatalf("authenticatewithquickconnect status=%d body=%s", rec.Code, rec.Body.String())
	}
	var res dto.AuthenticationResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.AccessToken == "" || res.User.Name != "admin" {
		t.Fatalf("unexpected auth result: %+v", res)
	}
}
