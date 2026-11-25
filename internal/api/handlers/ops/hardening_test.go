package ops

import (
	"bytes"
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
	"time"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func doAuthReq(t *testing.T, mux *http.ServeMux, ac *middleware.AuthContext, method, target string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, target, &buf)
	req.Header.Set("Content-Type", "application/json")
	if ac != nil {
		req = req.WithContext(middleware.WithAuthContext(req.Context(), ac))
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func makeAuthCtx(id, name string, isAdmin bool) *middleware.AuthContext {
	return &middleware.AuthContext{
		User: &sqlite.UserRecord{
			ID:      guid.Normalize(id),
			Name:    name,
			IsAdmin: isAdmin,
			Policy:  sqlite.DefaultUserPolicy(isAdmin),
		},
	}
}

func TestStartupSetupOrAdminAndRealUserCreation(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	st, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	userRepo := sqlite.NewUserRepo(st)
	h := New(st, cfg, userRepo, nil)
	mux := http.NewServeMux()
	authMW := middleware.NewAuth(nil, nil)
	h.RegisterRoutes(mux, AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
	})

	// 1. Zero users exist -> unauthenticated /Startup/* access is allowed.
	if r := doAuthReq(t, mux, nil, http.MethodGet, "/Startup/Configuration", nil); r.Code != http.StatusOK {
		t.Fatalf("expected 200 before setup, got %d", r.Code)
	}

	// 2. POST /Startup/User creates a real admin user in userRepo with Argon2id password hash.
	if r := doAuthReq(t, mux, nil, http.MethodPost, "/Startup/User", map[string]any{
		"Name":     "firstadmin",
		"Password": "initial-password",
	}); r.Code != http.StatusNoContent {
		t.Fatalf("POST /Startup/User status=%d body=%s", r.Code, r.Body.String())
	}

	u, err := userRepo.GetUserByName(context.Background(), "firstadmin")
	if err != nil {
		t.Fatalf("expected user in repo: %v", err)
	}
	if !u.IsAdmin || !u.Policy.IsAdministrator {
		t.Fatalf("expected admin user, got %+v", u)
	}
	ok, err := auth.VerifyPassword("initial-password", u.PasswordHash)
	if err != nil || !ok {
		t.Fatalf("expected password hash to verify, ok=%v err=%v", ok, err)
	}

	// 3. Now that a user exists, unauthenticated /Startup/* MUST be rejected (401).
	if r := doAuthReq(t, mux, nil, http.MethodGet, "/Startup/Configuration", nil); r.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 after user exists, got %d", r.Code)
	}
	if r := doAuthReq(t, mux, nil, http.MethodPost, "/Startup/Complete", nil); r.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on /Startup/Complete after user exists, got %d", r.Code)
	}

	// 4. Non-admin user MUST be rejected (403).
	viewer := makeAuthCtx("viewer-1", "viewer", false)
	if r := doAuthReq(t, mux, viewer, http.MethodGet, "/Startup/Configuration", nil); r.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin, got %d", r.Code)
	}

	// 5. Admin user is allowed and can update the startup user password.
	admin := makeAuthCtx(u.ID, u.Name, true)
	if r := doAuthReq(t, mux, admin, http.MethodPost, "/Startup/User", map[string]any{
		"Name":     "firstadmin",
		"Password": "updated-password",
	}); r.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for admin updating password, got %d", r.Code)
	}
	u2, err := userRepo.GetUserByName(context.Background(), "firstadmin")
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	ok, err = auth.VerifyPassword("updated-password", u2.PasswordHash)
	if err != nil || !ok {
		t.Fatalf("expected updated password hash to verify, ok=%v err=%v", ok, err)
	}
}

func TestPartiesAuthSpoofingRBACAndEventBus(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default(dir)
	st, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	bus := eventbus.New()
	var busEvents atomic.Int32
	bus.Subscribe(eventbus.EventSessionUpdated, func(ctx context.Context, e eventbus.Event) {
		if _, ok := e.Payload.(Party); ok {
			busEvents.Add(1)
		}
	})

	h := New(st, cfg, sqlite.NewUserRepo(st), bus)
	mux := http.NewServeMux()
	authMW := middleware.NewAuth(nil, nil)
	h.RegisterRoutes(mux, AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
	})

	host := makeAuthCtx("host-user", "host", false)
	member := makeAuthCtx("member-user", "member", false)
	outsider := makeAuthCtx("outsider-user", "outsider", false)
	admin := makeAuthCtx("admin-user", "admin", true)

	// Create party attempting to spoof HostUserId -> must use authenticated host ID.
	rec := doAuthReq(t, mux, host, http.MethodPost, "/Parties", map[string]any{
		"Name":       "Movie Night",
		"HostUserId": "spoofed-victim",
		"ItemId":     "movie-1",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /Parties status=%d", rec.Code)
	}
	var party Party
	_ = json.Unmarshal(rec.Body.Bytes(), &party)
	if party.HostUserId != host.UserID() {
		t.Fatalf("expected HostUserId=%q, got %q (spoofing not blocked!)", host.UserID(), party.HostUserId)
	}

	// Member joins attempting to spoof UserId -> must add member's own ID.
	rec = doAuthReq(t, mux, member, http.MethodPost, "/Parties/"+party.Id+"/Join", map[string]any{
		"UserId": "spoofed-other",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /Parties/Join status=%d", rec.Code)
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &party)
	if len(party.Members) != 2 || party.Members[1] != member.UserID() {
		t.Fatalf("expected member ID %q, got %+v", member.UserID(), party.Members)
	}

	// Outsider cannot see party in GET /Parties or GET /Parties/{Id} (403).
	if r := doAuthReq(t, mux, outsider, http.MethodGet, "/Parties", nil); r.Code != http.StatusOK || strings.Contains(r.Body.String(), party.Id) {
		t.Fatalf("expected outsider GET /Parties not to leak party, got %d %s", r.Code, r.Body.String())
	}
	if r := doAuthReq(t, mux, outsider, http.MethodGet, "/Parties/"+party.Id, nil); r.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for outsider GET /Parties/{Id}, got %d", r.Code)
	}
	// Member and Admin CAN view GET /Parties and GET /Parties/{Id}.
	if r := doAuthReq(t, mux, member, http.MethodGet, "/Parties", nil); r.Code != http.StatusOK || !strings.Contains(r.Body.String(), party.Id) {
		t.Fatalf("expected member GET /Parties to include party, got %d %s", r.Code, r.Body.String())
	}
	if r := doAuthReq(t, mux, member, http.MethodGet, "/Parties/"+party.Id, nil); r.Code != http.StatusOK {
		t.Fatalf("expected 200 for member GET /Parties/{Id}, got %d", r.Code)
	}
	if r := doAuthReq(t, mux, admin, http.MethodGet, "/Parties/"+party.Id, nil); r.Code != http.StatusOK {
		t.Fatalf("expected 200 for admin GET /Parties/{Id}, got %d", r.Code)
	}

	// Outsider cannot update party state (403).
	pos := int64(5000)
	if r := doAuthReq(t, mux, outsider, http.MethodPost, "/Parties/"+party.Id+"/State", map[string]any{
		"PositionTicks": pos,
	}); r.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for outsider updating state, got %d", r.Code)
	}

	// Member CAN update party state and it publishes to eventbus.
	if r := doAuthReq(t, mux, member, http.MethodPost, "/Parties/"+party.Id+"/State", map[string]any{
		"PositionTicks": pos,
	}); r.Code != http.StatusOK {
		t.Fatalf("expected 200 for member updating state, got %d", r.Code)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && busEvents.Load() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if busEvents.Load() != 1 {
		t.Fatalf("expected 1 eventbus notification for party state update, got %d", busEvents.Load())
	}

	// Member (non-host) cannot delete party (403).
	if r := doAuthReq(t, mux, member, http.MethodDelete, "/Parties/"+party.Id, nil); r.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-host deleting party, got %d", r.Code)
	}

	// Admin CAN delete party (204).
	if r := doAuthReq(t, mux, admin, http.MethodDelete, "/Parties/"+party.Id, nil); r.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for admin deleting party, got %d", r.Code)
	}
}

func TestUIViewConcurrentRaceFree(t *testing.T) {
	_, mux, _, _, _ := setupOpsTest(t)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = doReq(t, mux, http.MethodPost, "/Startup/Configuration", map[string]any{
				"ServerName": "Concurrent Server",
			})
		}()
		go func() {
			defer wg.Done()
			r := doReq(t, mux, http.MethodGet, "/UI/View", nil)
			if r.Code != http.StatusOK {
				t.Errorf("GET /UI/View status=%d", r.Code)
			}
		}()
	}
	wg.Wait()
}

func TestOpsSecurityHardeningV0103(t *testing.T) {
	h, mux, _, cfg, _ := setupOpsTest(t)
	authMW := middleware.NewAuth(nil, nil)
	authMux := http.NewServeMux()
	h.RegisterRoutes(authMux, AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
	})

	host := makeAuthCtx("host-1", "host", false)
	guest := makeAuthCtx("guest-1", "guest", false)
	admin := makeAuthCtx("admin-1", "admin", true)

	// 1. GET /Connect/Pending requires admin
	if r := doAuthReq(t, authMux, guest, http.MethodGet, "/Connect/Pending", nil); r.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin GET /Connect/Pending, got %d", r.Code)
	}
	if r := doAuthReq(t, authMux, admin, http.MethodGet, "/Connect/Pending", nil); r.Code != http.StatusOK {
		t.Fatalf("expected 200 for admin GET /Connect/Pending, got %d", r.Code)
	}

	// 2. POST /Startup/User rejects empty password
	if r := doReq(t, mux, http.MethodPost, "/Startup/User", map[string]any{"Name": "admin", "Password": "   "}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty password on /Startup/User, got %d", r.Code)
	}

	// 3. Private party requires matching InviteCode, validates PlaybackRate, auto-cleans when empty
	rec := doAuthReq(t, authMux, host, http.MethodPost, "/Parties", map[string]any{
		"Name":       "Secret Room",
		"InviteCode": "XYZ123",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create private party: %d", rec.Code)
	}
	var p Party
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	if p.IsPublic {
		t.Fatalf("expected IsPublic=false when InviteCode set")
	}
	if r := doAuthReq(t, authMux, guest, http.MethodPost, "/Parties/"+p.Id+"/Join", map[string]any{"InviteCode": "WRONG"}); r.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for wrong InviteCode, got %d", r.Code)
	}
	if r := doAuthReq(t, authMux, guest, http.MethodPost, "/Parties/"+p.Id+"/Join", map[string]any{"InviteCode": "XYZ123"}); r.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid InviteCode, got %d", r.Code)
	}
	if r := doAuthReq(t, authMux, host, http.MethodPost, "/Parties/"+p.Id+"/State", map[string]any{"PlaybackRate": 9.9}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for out-of-range PlaybackRate, got %d", r.Code)
	}
	// Both leave -> empty party automatically deleted
	_ = doAuthReq(t, authMux, guest, http.MethodPost, "/Parties/"+p.Id+"/Leave", nil)
	_ = doAuthReq(t, authMux, host, http.MethodPost, "/Parties/"+p.Id+"/Leave", nil)
	if r := doAuthReq(t, authMux, admin, http.MethodGet, "/Parties/"+p.Id, nil); r.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after all members left party, got %d", r.Code)
	}

	// 4. Webhook secret redacted in responses and preserved on update with empty secret
	whRec := doReq(t, mux, http.MethodPost, "/Webhooks", map[string]any{
		"Url":     "https://93.184.216.34/hook",
		"Secret":  "my-super-secret",
		"Enabled": true,
	})
	if whRec.Code != http.StatusOK {
		t.Fatalf("create webhook: %d %s", whRec.Code, whRec.Body.String())
	}
	var whResp WebhookConfig
	_ = json.Unmarshal(whRec.Body.Bytes(), &whResp)
	if whResp.Secret != "" || !whResp.HasSecret {
		t.Fatalf("expected Secret redacted & HasSecret=true, got %+v", whResp)
	}
	// Update with empty secret keeps old secret
	_ = doReq(t, mux, http.MethodPost, "/Webhooks", map[string]any{
		"Id":      whResp.Id,
		"Name":    "Renamed",
		"Url":     "https://93.184.216.34/hook",
		"Secret":  "",
		"Enabled": true,
	})
	h.mu.RLock()
	storedSecret := h.state.Webhooks[0].Secret
	h.mu.RUnlock()
	if storedSecret != "my-super-secret" {
		t.Fatalf("expected secret preserved on empty update, got %q", storedSecret)
	}

	// 5. Restore rejects symlinks and non-SQLite files
	bDir := h.backupsDir()
	_ = os.MkdirAll(bDir, 0o700)
	badFile := filepath.Join(bDir, "not-sqlite.db")
	_ = os.WriteFile(badFile, []byte("plain text not sqlite"), 0o600)
	if r := doReq(t, mux, http.MethodPost, "/BackupRestore/Restore", map[string]any{"Name": "not-sqlite.db"}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-SQLite backup restore, got %d", r.Code)
	}
	symPath := filepath.Join(bDir, "link.db")
	_ = os.Symlink(cfg.DBPath(), symPath)
	if r := doReq(t, mux, http.MethodPost, "/BackupRestore/Restore", map[string]any{"Name": "link.db"}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for symlink backup restore, got %d", r.Code)
	}
}
