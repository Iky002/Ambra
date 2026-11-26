package ops

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestOpsV0107AuditPartiesStartupWebhooksAndBackup(t *testing.T) {
	h, mux, _, _, _ := setupOpsTest(t)
	userRepo := h.userRepo
	defer h.Close()

	// 1. M-11: Party length limits, negative PositionTicks, InviteCode brute-force throttling, and host handover
	hostUser := &middleware.AuthContext{User: &sqlite.UserRecord{ID: "u-host", Name: "host", Policy: sqlite.DefaultUserPolicy(false)}}
	guestUser := &middleware.AuthContext{User: &sqlite.UserRecord{ID: "u-guest", Name: "guest", Policy: sqlite.DefaultUserPolicy(false)}}

	if r := doAuthReq(t, mux, hostUser, http.MethodPost, "/Parties", map[string]any{"Name": strings.Repeat("x", maxPartyNameLen+1)}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for overlong party name, got %d", r.Code)
	}
	if r := doAuthReq(t, mux, hostUser, http.MethodPost, "/Parties", map[string]any{"Name": "OK", "InviteCode": strings.Repeat("c", maxPartyInviteCodeLen+1)}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for overlong invite code, got %d", r.Code)
	}
	if r := doAuthReq(t, mux, hostUser, http.MethodPost, "/Parties", map[string]any{"Name": "OK", "ItemId": strings.Repeat("i", maxPartyItemIDLen+1)}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for overlong ItemId, got %d", r.Code)
	}
	if r := doAuthReq(t, mux, hostUser, http.MethodPost, "/Parties", map[string]any{"Name": "OK", "PositionTicks": -1}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for negative PositionTicks, got %d", r.Code)
	}

	recCreate := doAuthReq(t, mux, hostUser, http.MethodPost, "/Parties", map[string]any{
		"Name":       "Private Room",
		"InviteCode": "secret99",
		"IsPublic":   false,
	})
	if recCreate.Code != http.StatusOK {
		t.Fatalf("create party failed: %d %s", recCreate.Code, recCreate.Body.String())
	}
	var party Party
	_ = json.Unmarshal(recCreate.Body.Bytes(), &party)

	// Negative PositionTicks & overlong ItemId on UpdatePartyState
	if r := doAuthReq(t, mux, hostUser, http.MethodPost, "/Parties/"+party.Id+"/State", map[string]any{"PositionTicks": -100}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for negative PositionTicks on state update, got %d", r.Code)
	}
	if r := doAuthReq(t, mux, hostUser, http.MethodPost, "/Parties/"+party.Id+"/State", map[string]any{"ItemId": strings.Repeat("z", maxPartyItemIDLen+1)}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for overlong ItemId on state update, got %d", r.Code)
	}

	// JoinParty invite code length limit
	if r := doAuthReq(t, mux, guestUser, http.MethodPost, "/Parties/"+party.Id+"/Join", map[string]any{"InviteCode": strings.Repeat("a", maxPartyInviteCodeLen+1)}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for overlong InviteCode on join, got %d", r.Code)
	}

	// JoinParty failed attempts rate limit: 5 failures (403), 6th attempt -> 429
	for i := 0; i < maxPartyJoinFails; i++ {
		r := doAuthReq(t, mux, guestUser, http.MethodPost, "/Parties/"+party.Id+"/Join", map[string]any{"InviteCode": "wrong"})
		if r.Code != http.StatusForbidden {
			t.Fatalf("attempt %d expected 403, got %d", i, r.Code)
		}
	}
	if r := doAuthReq(t, mux, guestUser, http.MethodPost, "/Parties/"+party.Id+"/Join", map[string]any{"InviteCode": "secret99"}); r.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after 5 failed invite attempts, got %d", r.Code)
	}

	// Another guest joins successfully, then host leaves -> HostUserId transfers to remaining member
	guest2 := &middleware.AuthContext{User: &sqlite.UserRecord{ID: "u-guest-2", Name: "guest2", Policy: sqlite.DefaultUserPolicy(false)}}
	if r := doAuthReq(t, mux, guest2, http.MethodPost, "/Parties/"+party.Id+"/Join", map[string]any{"InviteCode": "secret99"}); r.Code != http.StatusOK {
		t.Fatalf("guest2 join expected 200, got %d", r.Code)
	}
	if r := doAuthReq(t, mux, hostUser, http.MethodPost, "/Parties/"+party.Id+"/Leave", nil); r.Code != http.StatusNoContent {
		t.Fatalf("host leave expected 204, got %d", r.Code)
	}
	recGet := doAuthReq(t, mux, guest2, http.MethodGet, "/Parties/"+party.Id, nil)
	if recGet.Code != http.StatusOK {
		t.Fatalf("get party after host leave: %d", recGet.Code)
	}
	var afterLeave Party
	_ = json.Unmarshal(recGet.Body.Bytes(), &afterLeave)
	if afterLeave.HostUserId != "uguest2" && afterLeave.HostUserId != "u-guest-2" {
		t.Fatalf("expected HostUserId transferred to guest2, got %q", afterLeave.HostUserId)
	}

	// 2. M-15: StartupUser validation (password >= 4, username <= 128) & concurrent anonymous race protection
	authMux := http.NewServeMux()
	h.RegisterRoutes(authMux, AuthFuncs{
		RequireAuth: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if ac, ok := middleware.AuthFromContext(r.Context()); !ok || ac == nil {
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}
				next.ServeHTTP(w, r)
			})
		},
		RequireAdmin: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if ac, ok := middleware.AuthFromContext(r.Context()); !ok || ac == nil || !ac.IsAdmin() {
					http.Error(w, "forbidden", http.StatusForbidden)
					return
				}
				next.ServeHTTP(w, r)
			})
		},
	})

	if r := doAuthReq(t, authMux, nil, http.MethodPost, "/Startup/User", map[string]any{"Name": "admin", "Password": "123"}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for password < 4 chars, got %d", r.Code)
	}
	if r := doAuthReq(t, authMux, nil, http.MethodPost, "/Startup/User", map[string]any{"Name": strings.Repeat("a", 129), "Password": "secret123"}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for overlong username, got %d", r.Code)
	}

	var wg sync.WaitGroup
	var okCount atomic.Int32
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			r := doAuthReq(t, authMux, nil, http.MethodPost, "/Startup/User", map[string]any{
				"Name":     "firstadmin",
				"Password": "password123",
			})
			if r.Code == http.StatusNoContent {
				okCount.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if okCount.Load() != 1 {
		t.Fatalf("expected exactly 1 anonymous startup user creation to succeed under concurrency, got %d", okCount.Load())
	}
	count, _ := userRepo.CountUsers(context.Background())
	if count != 1 {
		t.Fatalf("expected 1 user created, got %d", count)
	}

	// 3. M-18: RestoreBackup rejects corrupted file with valid 16-byte SQLite header via PRAGMA quick_check
	bDir := h.backupsDir()
	_ = os.MkdirAll(bDir, 0o700)
	corruptPath := filepath.Join(bDir, "corrupt.db")
	corruptBytes := append(append([]byte{}, sqliteMagicHeader...), make([]byte, 4096)...)
	for i := len(sqliteMagicHeader); i < len(corruptBytes); i++ {
		corruptBytes[i] = 0xFF
	}
	_ = os.WriteFile(corruptPath, corruptBytes, 0o600)
	if r := doReq(t, mux, http.MethodPost, "/BackupRestore/Restore", map[string]any{"Name": "corrupt.db"}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 rejecting corrupted sqlite backup via PRAGMA quick_check, got %d (%s)", r.Code, r.Body.String())
	}

	// 4. M-18: Close() shuts down webhook workers cleanly, is idempotent, and post-Close dispatch is safe
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()
	h.SetHTTPClient(ts.Client())
	h.mu.Lock()
	h.state.Webhooks = []WebhookConfig{{Id: "wh-1", Name: "Hook", Url: ts.URL, Events: []string{"*"}, Enabled: true}}
	h.mu.Unlock()
	h.dispatchWebhooks(context.Background(), eventbus.Event{Type: eventbus.EventPlaybackStart})
	h.Close()
	h.Close() // idempotent
	h.dispatchWebhooks(context.Background(), eventbus.Event{Type: eventbus.EventPlaybackStop})
}
