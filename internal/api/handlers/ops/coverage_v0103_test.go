package ops

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

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
)

func TestOpsWebhooksLoopbackAndDispatchCoverage(t *testing.T) {
	h, mux, st, _, _ := setupOpsTest(t)

	// SetHTTPClient(nil) is a no-op; getHTTPClient returns safe default when unset.
	h.SetHTTPClient(nil)
	if h.getHTTPClient() == nil {
		t.Fatalf("expected non-nil default HTTP client")
	}

	// validateWebhookURL blocks loopback hostnames/IPs when no custom client is set.
	for _, blocked := range []string{
		"http://localhost:8080/hook",
		"http://sub.localhost/hook",
		"http://127.0.0.1/hook",
		"http://[::1]/hook",
		"https://nonexistent.invalid/hook",
	} {
		if err := h.validateWebhookURL(blocked); err == nil {
			t.Fatalf("expected validateWebhookURL to reject %q", blocked)
		}
	}
	if err := h.validateWebhookURL("https://93.184.216.34/hook"); err != nil {
		t.Fatalf("expected public IP webhook URL to pass, got %v", err)
	}

	// matchesWebhookEvent with empty events list matches all.
	if !matchesWebhookEvent(nil, "PlaybackStart") {
		t.Fatalf("expected empty events slice to match any event")
	}

	// sendWebhook error paths: unmarshalable payload & invalid URL
	if _, err := h.sendWebhook(context.Background(), WebhookConfig{Url: "https://93.184.216.34/hook"}, WebhookPayload{
		Event: "Test",
		Data:  func() {},
	}); err == nil {
		t.Fatalf("expected marshal error from sendWebhook")
	}
	if _, err := h.sendWebhook(context.Background(), WebhookConfig{Url: "http://\x7f"}, WebhookPayload{Event: "Test"}); err == nil {
		t.Fatalf("expected request creation error from sendWebhook")
	}

	// Create webhooks (one enabled with secret, one disabled) and verify GET /Webhooks redacts secret.
	rec := doReq(t, mux, http.MethodPost, "/Webhooks", map[string]any{
		"Url":     "https://93.184.216.34/hook1",
		"Secret":  "top-secret",
		"Enabled": true,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create webhook 1: %d %s", rec.Code, rec.Body.String())
	}
	rec = doReq(t, mux, http.MethodPost, "/Webhooks", map[string]any{
		"Url":     "https://93.184.216.34/hook2",
		"Enabled": false,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create webhook 2: %d", rec.Code)
	}

	listRec := doReq(t, mux, http.MethodGet, "/Webhooks", nil)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list webhooks: %d", listRec.Code)
	}
	var listed []WebhookConfig
	if err := json.Unmarshal(listRec.Body.Bytes(), &listed); err != nil || len(listed) != 2 {
		t.Fatalf("expected 2 webhooks, got %d err=%v", len(listed), err)
	}
	if listed[0].Secret != "" || !listed[0].HasSecret {
		t.Fatalf("expected secret redacted in list response: %+v", listed[0])
	}

	// dispatchWebhooks: skips disabled webhook and drops gracefully when queue is full.
	h.webhookOnce.Do(func() {
		h.webhookCh = make(chan webhookDispatchJob, 1)
	})
	h.webhookCh <- webhookDispatchJob{} // fill queue so dispatch hits default/drop branch
	h.dispatchWebhooks(context.Background(), eventbus.Event{Type: eventbus.EventPlaybackStart})
	<-h.webhookCh // drain

	// Closed DB triggers 500 on webhook persistence.
	_ = st.Close()
	failRec := doReq(t, mux, http.MethodPost, "/Webhooks", map[string]any{
		"Url":     "https://93.184.216.34/hook3",
		"Enabled": true,
	})
	if failRec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when DB closed on create webhook, got %d", failRec.Code)
	}
}

func TestOpsPartiesAndHandlerEdgeCoverage(t *testing.T) {
	// Handler with nil store/cfg/userRepo exercises nil-safe fallbacks.
	hNil := New(nil, nil, nil, nil)
	if hNil.serverID() != "" {
		t.Fatalf("expected empty serverID when cfg is nil")
	}
	if hNil.dataDir() != "./data" {
		t.Fatalf("expected ./data when cfg is nil, got %q", hNil.dataDir())
	}
	if err := hNil.saveStateLocked(context.Background()); err != nil {
		t.Fatalf("expected nil error from saveStateLocked when store is nil, got %v", err)
	}
	muxNil := http.NewServeMux()
	authMW := middleware.NewAuth(nil, nil)
	hNil.RegisterRoutes(muxNil, AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
	})
	// wrapSetupOrAdmin passes through when userRepo == nil.
	if r := doReq(t, muxNil, http.MethodGet, "/Startup/Configuration", nil); r.Code != http.StatusOK {
		t.Fatalf("expected 200 when userRepo is nil, got %d", r.Code)
	}

	// isPartyMember empty userID
	if isPartyMember([]string{"u1"}, "") {
		t.Fatalf("expected isPartyMember false for empty userID")
	}

	// resolveUserID with API key auth context (User == nil, IsAPIKey == true)
	reqAPI := httptest.NewRequest(http.MethodPost, "/Parties", nil)
	reqAPI = reqAPI.WithContext(middleware.WithAuthContext(reqAPI.Context(), &middleware.AuthContext{IsAPIKey: true}))
	if got := hNil.resolveUserID(reqAPI, "explicit-user"); got == "" {
		t.Fatalf("expected normalized explicit user for API key context")
	}
	if got := hNil.resolveUserID(reqAPI, ""); got != "" {
		t.Fatalf("expected empty user when API key context has no explicit user, got %q", got)
	}

	// Create party with explicit IsPublic=false, maxParties limit (429), and DB closed (500).
	h, mux, st, _, _ := setupOpsTest(t)
	isPub := false
	rec := doReq(t, mux, http.MethodPost, "/Parties", map[string]any{
		"Name":     "Explicit Private",
		"IsPublic": isPub,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create party explicit IsPublic=false: %d", rec.Code)
	}
	var created Party
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.IsPublic {
		t.Fatalf("expected IsPublic=false")
	}

	// Fill up to maxParties and verify 429 Too Many Requests.
	h.mu.Lock()
	for len(h.state.Parties) < maxParties {
		h.state.Parties = append(h.state.Parties, Party{Id: "fill"})
	}
	h.mu.Unlock()
	if r := doReq(t, mux, http.MethodPost, "/Parties", map[string]any{"Name": "Overflow"}); r.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 when maxParties reached, got %d", r.Code)
	}

	// Clear parties, close DB -> 500 on create party & 500 on /Startup/User lookup & loadState error path.
	h.mu.Lock()
	h.state.Parties = nil
	h.mu.Unlock()
	_ = st.Close()
	if r := doReq(t, mux, http.MethodPost, "/Parties", map[string]any{"Name": "FailPersist"}); r.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on create party with closed DB, got %d", r.Code)
	}
	if r := doReq(t, mux, http.MethodPost, "/Startup/User", map[string]any{"Name": "admin", "Password": "pw"}); r.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on /Startup/User with closed DB, got %d", r.Code)
	}
	// loadState on closed DB safely returns without panic.
	h.loadState(context.Background())
}

func TestOpsBackupAndRestoreEdgeCoverage(t *testing.T) {
	// 1. BackupInfo skips directories and sorts multiple backups newest-first.
	h, mux, st, cfg, _ := setupOpsTest(t)
	bDir := h.backupsDir()
	_ = os.MkdirAll(filepath.Join(bDir, "subdir"), 0o700)
	f1 := filepath.Join(bDir, "old.db")
	f2 := filepath.Join(bDir, "new.db")
	_ = os.WriteFile(f1, append(append([]byte{}, sqliteMagicHeader...), []byte("old")...), 0o600)
	_ = os.WriteFile(f2, append(append([]byte{}, sqliteMagicHeader...), []byte("new")...), 0o600)
	now := time.Now()
	_ = os.Chtimes(f1, now.Add(-time.Hour), now.Add(-time.Hour))
	_ = os.Chtimes(f2, now, now)

	infoRec := doReq(t, mux, http.MethodGet, "/BackupRestore/BackupInfo", nil)
	if infoRec.Code != http.StatusOK {
		t.Fatalf("GET /BackupRestore/BackupInfo: %d", infoRec.Code)
	}
	var infoResp BackupInfoResponse
	_ = json.Unmarshal(infoRec.Body.Bytes(), &infoResp)
	if len(infoResp.Backups) != 2 || infoResp.Backups[0].Name != "new.db" {
		t.Fatalf("expected 2 sorted backups with new.db first, got %+v", infoResp.Backups)
	}

	// 2. Restore rejects directory (404) and too-short file (400).
	if r := doReq(t, mux, http.MethodPost, "/BackupRestore/Restore", map[string]any{"Name": "subdir"}); r.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for directory backup restore, got %d", r.Code)
	}
	shortFile := filepath.Join(bDir, "short.db")
	_ = os.WriteFile(shortFile, []byte("short"), 0o600)
	if r := doReq(t, mux, http.MethodPost, "/BackupRestore/Restore", map[string]any{"Name": "short.db"}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for short backup file, got %d", r.Code)
	}
	if err := verifySQLiteHeader(filepath.Join(bDir, "missing.db")); err == nil {
		t.Fatalf("expected error from verifySQLiteHeader on missing file")
	}

	// 3. Closed DB causes VACUUM INTO failure -> 500.
	_ = st.Close()
	if r := doReq(t, mux, http.MethodPost, "/BackupRestore/Backup", nil); r.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when VACUUM INTO fails on closed DB, got %d", r.Code)
	}

	// 4. File-copy fallback when h.store == nil (both success and missing source DB failure).
	hFile := New(nil, cfg, nil, nil)
	muxFile := http.NewServeMux()
	hFile.RegisterRoutes(muxFile, AuthFuncs{})
	if r := doReq(t, muxFile, http.MethodPost, "/BackupRestore/Backup", nil); r.Code != http.StatusOK {
		t.Fatalf("expected 200 for file-copy backup fallback, got %d %s", r.Code, r.Body.String())
	}

	cfgMissing := config.Default(t.TempDir())
	hMissing := New(nil, cfgMissing, nil, nil)
	muxMissing := http.NewServeMux()
	hMissing.RegisterRoutes(muxMissing, AuthFuncs{})
	if r := doReq(t, muxMissing, http.MethodPost, "/BackupRestore/Backup", nil); r.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for file-copy backup with missing db file, got %d", r.Code)
	}

	// 5. os.MkdirAll failure when backupsDir collides with a regular file.
	dirBlocked := t.TempDir()
	cfgBlocked := config.Default(dirBlocked)
	_ = os.WriteFile(filepath.Join(dirBlocked, "backups"), []byte("not-a-dir"), 0o600)
	hBlocked := New(nil, cfgBlocked, nil, nil)
	muxBlocked := http.NewServeMux()
	hBlocked.RegisterRoutes(muxBlocked, AuthFuncs{})
	if r := doReq(t, muxBlocked, http.MethodPost, "/BackupRestore/Backup", nil); r.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when backup dir is blocked by file, got %d", r.Code)
	}

	// 6. copyFileAtomic edge errors (missing source, dst dir blocked by file, dst is existing dir).
	tmp := t.TempDir()
	if err := copyFileAtomic(filepath.Join(tmp, "no-such-file"), filepath.Join(tmp, "dst"), 0o600); err == nil {
		t.Fatalf("expected error for missing source")
	}
	srcOK := filepath.Join(tmp, "src.db")
	_ = os.WriteFile(srcOK, []byte("hello"), 0o600)
	blocker := filepath.Join(tmp, "blocker")
	_ = os.WriteFile(blocker, []byte("x"), 0o600)
	if err := copyFileAtomic(srcOK, filepath.Join(blocker, "nested", "dst"), 0o600); err == nil {
		t.Fatalf("expected error when parent dir is a file")
	}
	dstDir := filepath.Join(tmp, "existing-dir")
	_ = os.MkdirAll(dstDir, 0o700)
	if err := copyFileAtomic(srcOK, dstDir, 0o600); err == nil || !strings.Contains(err.Error(), "") {
		t.Fatalf("expected rename error when dst is a directory")
	}
}
