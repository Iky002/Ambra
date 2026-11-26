package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestTraktPersonalBindingAPIAndGlobalAvailability(t *testing.T) {
	ctx := context.Background()
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.EnableDLNA = false
	srv, err := New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	tokens := make(map[string]string)
	ids := make(map[string]string)
	for _, name := range []string{"admin", "alice", "bob"} {
		u, err := srv.Auth.CreateUser(ctx, name, "test-password", name == "admin")
		if err != nil {
			t.Fatal(err)
		}
		res, err := srv.Auth.AuthenticateByName(ctx, name, "test-password", "", auth.ClientInfo{DeviceID: name})
		if err != nil {
			t.Fatal(err)
		}
		tokens[name], ids[name] = res.AccessToken, u.ID
	}
	request := func(method, path, token, body string, want int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Emby-Token", token)
		w := httptest.NewRecorder()
		srv.Handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: status=%d want=%d body=%s", method, path, w.Code, want, w.Body.String())
		}
		if strings.Contains(path, "/Trakt") && w.Header().Get("Cache-Control") != "no-store" && w.Code != 401 {
			t.Fatal("personal settings must not be cached")
		}
		return w
	}
	const personal = "/emby/Users/Me/Trakt"
	const global = "/emby/Plugins/ambra.builtin.trakt/Configuration"
	request("GET", personal, "", "", 401)
	request("POST", personal, tokens["alice"], `{"ClientId":"alice-app","AccessToken":"alice-secret"}`, 403)
	request("POST", global, tokens["alice"], `{"enabled":true}`, 403)
	request("POST", global, tokens["admin"], `{"enabled":true,"access_token":"legacy-global-secret","base_url":"https://attacker.invalid"}`, 200)

	alice := request("POST", personal+"?UserId="+ids["bob"], tokens["alice"],
		`{"ClientId":"alice-app","AccessToken":"alice-secret","SyncWatched":false,"Enabled":false,"UserId":"`+ids["bob"]+`"}`, 200)
	if strings.Contains(alice.Body.String(), "alice-secret") || strings.Contains(alice.Body.String(), "AccessToken\"") {
		t.Fatal("a binding response must never return a token")
	}
	request("POST", personal, tokens["bob"], `{"ClientId":"bob-app","AccessToken":"bob-secret"}`, 200)
	repo := sqlite.NewUserRepo(srv.Store)
	for _, name := range []string{"alice", "bob"} {
		binding, err := repo.GetUserTraktConfig(ctx, ids[name])
		if err != nil || binding == nil || binding.AccessToken != name+"-secret" {
			t.Fatalf("%s binding was not isolated: %v", name, err)
		}
	}
	for _, method := range []string{"GET", "POST", "DELETE"} {
		// API keys may administer plugins, but cannot impersonate a personal account.
		key := "personal-api-key-test"
		if _, err := srv.Store.Writer.Exec(`INSERT OR IGNORE INTO api_keys VALUES (?, 'test', 1)`, key); err != nil {
			t.Fatal(err)
		}
		request(method, personal, key, `{}`, 403)
	}
	// Missing token preserves the existing secret; switching apps requires a new one.
	request("POST", personal, tokens["alice"], `{"ScrobblePlayback":false,"SyncWatched":true}`, 200)
	request("POST", personal, tokens["alice"], `{"ClientId":"other-app"}`, 400)
	request("POST", personal, tokens["alice"], `{"AccessToken":""}`, 400)
	request("POST", personal, tokens["alice"], `{"AccessToken":"********"}`, 400)
	request("POST", personal, tokens["alice"], `{"AccessToken":"bad\r\nheader"}`, 400)
	request("POST", personal, tokens["alice"], `{broken`, 400)
	request("POST", personal, tokens["alice"], `null`, 200) // no changes to an existing binding
	lower := request("GET", "/emby/users/me/trakt", tokens["alice"], "", 200)
	var status struct {
		Enabled, Configured, ScrobblePlayback, SyncWatched bool
		ClientID                                           string `json:"ClientId"`
	}
	if err := json.Unmarshal(lower.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if !status.Enabled || !status.Configured || status.ScrobblePlayback || !status.SyncWatched || status.ClientID != "alice-app" {
		t.Fatalf("unexpected personal preferences: %s", lower.Body.String())
	}
	adminStatus := request("GET", personal, tokens["admin"], "", 200)
	if strings.Contains(adminStatus.Body.String(), "alice-app") || strings.Contains(adminStatus.Body.String(), "bob-app") {
		t.Fatal("administrators also only see their own personal binding")
	}
	gl := request("GET", global, tokens["admin"], "", 200)
	if strings.Contains(gl.Body.String(), "token") || strings.Contains(gl.Body.String(), "base_url") {
		t.Fatalf("global settings still include account fields: %s", gl.Body.String())
	}
	request("POST", global, tokens["admin"], `{"enabled":false}`, 200)
	request("POST", personal, tokens["bob"], `{"SyncWatched":false}`, 403)
	// Disabled globally: status and disconnect are still available, credentials stay private.
	request("GET", personal, tokens["alice"], "", 200)
	request("DELETE", personal, tokens["alice"], "", 204)
	bob, err := repo.GetUserTraktConfig(ctx, ids["bob"])
	if err != nil || bob == nil || bob.AccessToken != "bob-secret" {
		t.Fatal("disconnecting Alice must not change Bob's account")
	}
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	restartedRepo := sqlite.NewUserRepo(restarted.Store)
	bob, err = restartedRepo.GetUserTraktConfig(ctx, ids["bob"])
	if err != nil || bob == nil || bob.AccessToken != "bob-secret" {
		t.Fatal("personal binding must survive restart")
	}
	binding, err := restartedRepo.GetUserTraktConfig(ctx, ids["alice"])
	if err != nil || binding != nil {
		t.Fatal("disconnect must survive restart")
	}
	p, _ := restarted.Registry.Get("ambra.builtin.trakt")
	if p.Manifest().Enabled {
		t.Fatal("global disable must survive restart")
	}
}

func TestTraktStartupPurgesLegacyGlobalCredentials(t *testing.T) {
	ctx := context.Background()
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Writer.Exec(`INSERT INTO server_config VALUES ('plugin:ambra.builtin.trakt', ?, 1)`,
		`{"enabled":true,"client_id":"legacy-app","access_token":"legacy-secret","user_id":"someone"}`)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	srv, err := New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	var raw string
	if err := srv.Store.Reader.QueryRow(`SELECT value_json FROM server_config WHERE key = 'plugin:ambra.builtin.trakt'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw != `{"enabled":true}` {
		t.Fatalf("legacy credentials were not removed: %s", raw)
	}
}
