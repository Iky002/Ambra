package compatext

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestConfiguredNotificationsSSRFOriginStripAndPurgeUser(t *testing.T) {
	dir := t.TempDir()
	store, err := sqlite.Open(filepath.Join(dir, "compatext_notif.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	cfg := config.Default(dir)
	userRepo := sqlite.NewUserRepo(store)
	ctx := context.Background()

	alice := &sqlite.UserRecord{
		ID:     "alice000000000000000000000000001",
		Name:   "alice",
		Policy: sqlite.DefaultUserPolicy(false),
		Config: sqlite.DefaultUserConfig(),
	}
	admin := &sqlite.UserRecord{
		ID:      "admin000000000000000000000000001",
		Name:    "admin",
		IsAdmin: true,
		Policy:  sqlite.DefaultUserPolicy(true),
		Config:  sqlite.DefaultUserConfig(),
	}
	if err := userRepo.CreateUser(ctx, alice); err != nil {
		t.Fatal(err)
	}
	if err := userRepo.CreateUser(ctx, admin); err != nil {
		t.Fatal(err)
	}

	h := New(store, cfg)
	h.SetUserRepo(userRepo)
	router := h.BuildRouter(AuthFuncs{})

	callAs := func(u *sqlite.UserRecord, method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req = req.WithContext(middleware.WithAuthContext(req.Context(), &middleware.AuthContext{User: u}))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	// 1. Non-admin cannot save private LAN or loopback URLs.
	for _, badURL := range []string{"http://127.0.0.1:8096/hook", "http://192.168.1.10:8080/hook", "http://10.0.0.5/hook"} {
		rec := callAs(alice, http.MethodPost, "/Notifications/Services/Configured", `{
			"Id":"svc-lan","Name":"LAN","Enabled":true,"Options":{"Url":"`+badURL+`"}
		}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for non-admin URL %s, got %d %s", badURL, rec.Code, rec.Body.String())
		}
	}

	// 2. Admin cannot save loopback URL, but can save private LAN URL.
	if rec := callAs(admin, http.MethodPost, "/Notifications/Services/Configured", `{
		"Id":"svc-adm-loop","Name":"Loop","Enabled":true,"Options":{"Url":"http://127.0.0.1:8096/hook"}
	}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for admin loopback URL, got %d", rec.Code)
	}
	if rec := callAs(admin, http.MethodPost, "/Notifications/Services/Configured", `{
		"Id":"svc-adm-lan","Name":"LAN","Enabled":true,"Options":{"Url":"http://192.168.1.10:8080/hook"}
	}`); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for admin private LAN URL, got %d %s", rec.Code, rec.Body.String())
	}

	// 3. mergeNotificationOptions preserves masked secret on same origin, strips it on origin change.
	prev := map[string]string{
		"Url":    "https://hooks.example.com/v1/a",
		"Token":  "top-secret-token",
		"Secret": "hmac-secret",
	}
	sameOrigin := mergeNotificationOptions(prev, map[string]string{
		"Url":    "https://hooks.example.com:443/v1/b",
		"Token":  maskedOptionValue,
		"Secret": maskedOptionValue,
	})
	if sameOrigin["Token"] != "top-secret-token" || sameOrigin["Secret"] != "hmac-secret" {
		t.Fatalf("expected masked secrets preserved on same origin, got %+v", sameOrigin)
	}
	diffOrigin := mergeNotificationOptions(prev, map[string]string{
		"Url":    "https://attacker.example.org/steal",
		"Token":  maskedOptionValue,
		"Secret": maskedOptionValue,
	})
	if diffOrigin["Token"] != "" || diffOrigin["Secret"] != "" {
		t.Fatalf("expected masked secrets stripped on origin change, got %+v", diffOrigin)
	}

	// 4. PurgeUser removes Alice's configured notification services and persists.
	if rec := callAs(alice, http.MethodPost, "/Notifications/Services/Configured", `{
		"Id":"svc-alice-1","Name":"Alice Hook","Enabled":true,"Options":{"channel":"#alerts"}
	}`); rec.Code != http.StatusOK {
		t.Fatalf("save alice service: %d %s", rec.Code, rec.Body.String())
	}
	h.PurgeUser(ctx, alice.ID)

	recGet := callAs(alice, http.MethodGet, "/Notifications/Services/Configured", "")
	var aliceSvcs []NotificationServiceConfig
	_ = json.Unmarshal(recGet.Body.Bytes(), &aliceSvcs)
	if len(aliceSvcs) != 0 {
		t.Fatalf("expected 0 services for Alice after PurgeUser, got %+v", aliceSvcs)
	}
	// Reload from SQLite to confirm persistence.
	h2 := New(store, cfg)
	h2.mu.RLock()
	remaining := len(h2.state.NotificationServices)
	h2.mu.RUnlock()
	if remaining != 1 {
		t.Fatalf("expected only admin service persisted after PurgeUser, got %d", remaining)
	}
}
