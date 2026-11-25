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
	"sync/atomic"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func setupOpsTest(t *testing.T) (*Handler, *http.ServeMux, *sqlite.Store, *config.Config, *eventbus.Bus) {
	t.Helper()
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

	bus := eventbus.New()
	h := New(st, cfg, sqlite.NewUserRepo(st), bus)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{
		UserIDFromCtx: func(ctx context.Context) string { return "admin-1" },
	})
	return h, mux, st, cfg, bus
}

func doReq(t *testing.T, mux *http.ServeMux, method, target string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		switch v := body.(type) {
		case string:
			buf.WriteString(v)
		default:
			_ = json.NewEncoder(&buf).Encode(v)
		}
	}
	req := httptest.NewRequest(method, target, &buf)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestStartupAndPackages(t *testing.T) {
	_, mux, st, cfg, _ := setupOpsTest(t)

	// Startup Configuration
	res := doReq(t, mux, http.MethodGet, "/Startup/Configuration", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("GET /Startup/Configuration status=%d", res.Code)
	}
	if r := doReq(t, mux, http.MethodPost, "/Startup/Configuration", "{bad"); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", r.Code)
	}
	if r := doReq(t, mux, http.MethodPost, "/Startup/Configuration", map[string]any{
		"ServerName":                "My Ambra",
		"UICulture":                 "en-US",
		"MetadataCountryCode":       "US",
		"PreferredMetadataLanguage": "en-US",
	}); r.Code != http.StatusNoContent {
		t.Fatalf("POST /Startup/Configuration status=%d", r.Code)
	}
	if cfg.ServerName != "My Ambra" || cfg.TMDBLanguage != "en-US" {
		t.Fatalf("config not updated: %+v", cfg)
	}

	// Startup User
	res = doReq(t, mux, http.MethodGet, "/Startup/User", nil)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"admin"`) {
		t.Fatalf("GET /Startup/User unexpected: %d %s", res.Code, res.Body.String())
	}
	if r := doReq(t, mux, http.MethodPost, "/Startup/User", "{bad"); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", r.Code)
	}
	if r := doReq(t, mux, http.MethodPost, "/Startup/User", map[string]any{"Name": "  "}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on empty name, got %d", r.Code)
	}
	if r := doReq(t, mux, http.MethodPost, "/Startup/User", map[string]any{"Name": "root", "Password": "secret"}); r.Code != http.StatusNoContent {
		t.Fatalf("POST /Startup/User status=%d", r.Code)
	}
	res = doReq(t, mux, http.MethodGet, "/Startup/User", nil)
	if !strings.Contains(res.Body.String(), `"root"`) {
		t.Fatalf("expected root user, got %s", res.Body.String())
	}

	// RemoteAccess & Complete
	if r := doReq(t, mux, http.MethodPost, "/Startup/RemoteAccess", "{bad"); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", r.Code)
	}
	if r := doReq(t, mux, http.MethodPost, "/Startup/RemoteAccess", map[string]any{
		"EnableRemoteAccess":         true,
		"EnableAutomaticPortMapping": true,
	}); r.Code != http.StatusNoContent {
		t.Fatalf("POST /Startup/RemoteAccess status=%d", r.Code)
	}
	if r := doReq(t, mux, http.MethodPost, "/Startup/Complete", nil); r.Code != http.StatusNoContent {
		t.Fatalf("POST /Startup/Complete status=%d", r.Code)
	}
	if !cfg.IsStartupWizardCompleted {
		t.Fatalf("expected IsStartupWizardCompleted=true")
	}

	// Packages
	if r := doReq(t, mux, http.MethodGet, "/Packages", nil); r.Code != http.StatusOK {
		t.Fatalf("GET /Packages status=%d", r.Code)
	}
	if r := doReq(t, mux, http.MethodGet, "/Packages?packagetype=Metadata", nil); r.Code != http.StatusOK || !strings.Contains(r.Body.String(), "TMDB") {
		t.Fatalf("GET /Packages filtered unexpected: %d %s", r.Code, r.Body.String())
	}
	if r := doReq(t, mux, http.MethodGet, "/Packages/Updates", nil); r.Code != http.StatusOK {
		t.Fatalf("GET /Packages/Updates status=%d", r.Code)
	}
	if r := doReq(t, mux, http.MethodGet, "/Packages/ambra-tmdb", nil); r.Code != http.StatusOK {
		t.Fatalf("GET /Packages/{Name} status=%d", r.Code)
	}
	if r := doReq(t, mux, http.MethodGet, "/Packages/nonexistent", nil); r.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", r.Code)
	}
	if r := doReq(t, mux, http.MethodPost, "/Packages/Installed/nonexistent", nil); r.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", r.Code)
	}
	res = doReq(t, mux, http.MethodPost, "/Packages/Installed/ambra-tmdb", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("POST /Packages/Installed/{Name} status=%d", res.Code)
	}
	var inst struct {
		Id string `json:"Id"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &inst)
	if r := doReq(t, mux, http.MethodDelete, "/Packages/Installing/"+inst.Id, nil); r.Code != http.StatusNoContent {
		t.Fatalf("DELETE /Packages/Installing/{Id} status=%d", r.Code)
	}
	if r := doReq(t, mux, http.MethodDelete, "/Packages/Installing/"+inst.Id, nil); r.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", r.Code)
	}

	// Reload state from SQLite
	reloaded := New(st, cfg, nil, nil)
	if reloaded.state.Startup.ServerName != "My Ambra" {
		t.Fatalf("expected persisted ServerName, got %s", reloaded.state.Startup.ServerName)
	}
}

func TestWebhooksAndEventBusDispatch(t *testing.T) {
	_, mux, _, _, bus := setupOpsTest(t)

	var received atomic.Int32
	var lastSig atomic.Value
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		lastSig.Store(r.Header.Get("X-Ambra-Signature"))
		w.WriteHeader(http.StatusOK)
	}))
	defer receiver.Close()

	// List empty
	if r := doReq(t, mux, http.MethodGet, "/Webhooks", nil); r.Code != http.StatusOK {
		t.Fatalf("GET /Webhooks status=%d", r.Code)
	}

	// Validation
	if r := doReq(t, mux, http.MethodPost, "/Webhooks", "{bad"); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", r.Code)
	}
	if r := doReq(t, mux, http.MethodPost, "/Webhooks", map[string]any{"Name": "NoURL"}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on missing Url, got %d", r.Code)
	}

	// Create webhook
	res := doReq(t, mux, http.MethodPost, "/Webhooks", map[string]any{
		"Url":     receiver.URL,
		"Secret":  "top-secret",
		"Enabled": true,
	})
	if res.Code != http.StatusOK {
		t.Fatalf("POST /Webhooks status=%d", res.Code)
	}
	var wh WebhookConfig
	_ = json.Unmarshal(res.Body.Bytes(), &wh)

	// Update webhook (specific event filter)
	res = doReq(t, mux, http.MethodPost, "/Webhooks", map[string]any{
		"Id":      wh.Id,
		"Name":    "Playback Hook",
		"Url":     receiver.URL,
		"Secret":  "top-secret",
		"Events":  []string{"PlaybackStart"},
		"Enabled": true,
	})
	if res.Code != http.StatusOK {
		t.Fatalf("update webhook status=%d", res.Code)
	}

	// Test webhook by Id
	res = doReq(t, mux, http.MethodPost, "/Webhooks/Test", map[string]any{"Id": wh.Id})
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"Success":true`) {
		t.Fatalf("POST /Webhooks/Test unexpected: %d %s", res.Code, res.Body.String())
	}
	if sig, _ := lastSig.Load().(string); !strings.HasPrefix(sig, "sha256=") {
		t.Fatalf("expected sha256 signature header, got %q", sig)
	}

	// Test webhook errors
	if r := doReq(t, mux, http.MethodPost, "/Webhooks/Test", "{bad"); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", r.Code)
	}
	if r := doReq(t, mux, http.MethodPost, "/Webhooks/Test", map[string]any{}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on missing target, got %d", r.Code)
	}
	if r := doReq(t, mux, http.MethodPost, "/Webhooks/Test", map[string]any{"Url": "http://127.0.0.1:1"}); r.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 on unreachable url, got %d", r.Code)
	}

	// Publish matching & non-matching events via EventBus
	before := received.Load()
	bus.Publish(context.Background(), eventbus.Event{Type: eventbus.EventLibraryChanged, Payload: "ignored"})
	bus.Publish(context.Background(), eventbus.Event{Type: eventbus.EventPlaybackStart, Payload: map[string]string{"item": "1"}})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && received.Load() <= before {
		time.Sleep(15 * time.Millisecond)
	}
	if received.Load() != before+1 {
		t.Fatalf("expected 1 async webhook delivery, got %d", received.Load()-before)
	}

	// Delete webhook & 404
	if r := doReq(t, mux, http.MethodDelete, "/Webhooks/"+wh.Id, nil); r.Code != http.StatusNoContent {
		t.Fatalf("DELETE /Webhooks/{Id} status=%d", r.Code)
	}
	if r := doReq(t, mux, http.MethodDelete, "/Webhooks/"+wh.Id, nil); r.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", r.Code)
	}
}

func TestBackupRestore(t *testing.T) {
	_, mux, _, cfg, _ := setupOpsTest(t)

	// Info before backups
	if r := doReq(t, mux, http.MethodGet, "/BackupRestore/BackupInfo", nil); r.Code != http.StatusOK {
		t.Fatalf("GET /BackupRestore/BackupInfo status=%d", r.Code)
	}

	// Create SQLite hot backup (VACUUM INTO)
	res := doReq(t, mux, http.MethodPost, "/BackupRestore/Backup", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("POST /BackupRestore/Backup status=%d body=%s", res.Code, res.Body.String())
	}
	var bk BackupFileInfo
	_ = json.Unmarshal(res.Body.Bytes(), &bk)
	if bk.Name == "" || bk.Size == 0 {
		t.Fatalf("unexpected backup file info: %+v", bk)
	}

	// Info now lists backup
	res = doReq(t, mux, http.MethodGet, "/BackupRestore/BackupInfo", nil)
	var info BackupInfoResponse
	_ = json.Unmarshal(res.Body.Bytes(), &info)
	if len(info.Backups) != 1 {
		t.Fatalf("expected 1 backup, got %d", len(info.Backups))
	}

	// Restore validation & success
	if r := doReq(t, mux, http.MethodPost, "/BackupRestore/Restore", "{bad"); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", r.Code)
	}
	if r := doReq(t, mux, http.MethodPost, "/BackupRestore/Restore", map[string]any{"Name": "../escape.db"}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on path traversal, got %d", r.Code)
	}
	if r := doReq(t, mux, http.MethodPost, "/BackupRestore/Restore", map[string]any{"Name": "missing.db"}); r.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", r.Code)
	}
	res = doReq(t, mux, http.MethodPost, "/BackupRestore/Restore", map[string]any{"Path": bk.Path})
	if res.Code != http.StatusOK {
		t.Fatalf("POST /BackupRestore/Restore status=%d body=%s", res.Code, res.Body.String())
	}
	if _, err := os.Stat(filepath.Join(cfg.DataDir, "ambra.db.restore")); err != nil {
		t.Fatalf("staged restore file missing: %v", err)
	}

	// File-copy fallback when store == nil
	fileDir := t.TempDir()
	fileCfg := config.Default(fileDir)
	_ = os.WriteFile(fileCfg.DBPath(), []byte("sqlite-fallback"), 0o644)
	fileH := New(nil, fileCfg, nil, nil)
	fileMux := http.NewServeMux()
	fileH.RegisterRoutes(fileMux, AuthFuncs{})
	if r := doReq(t, fileMux, http.MethodPost, "/BackupRestore/Backup", nil); r.Code != http.StatusOK {
		t.Fatalf("file-copy backup status=%d body=%s", r.Code, r.Body.String())
	}

	// Error when neither store nor cfg is configured
	nilH := New(nil, nil, nil, nil)
	nilMux := http.NewServeMux()
	nilH.RegisterRoutes(nilMux, AuthFuncs{})
	if r := doReq(t, nilMux, http.MethodPost, "/BackupRestore/Backup", nil); r.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when no store/cfg, got %d", r.Code)
	}
}

func TestOpenAPIGenericUIPartiesAndConnect(t *testing.T) {
	_, mux, _, _, _ := setupOpsTest(t)

	// OpenAPI / Swagger / Robots
	for _, path := range []string{"/openapi.json", "/swagger.json", "/openapi", "/swagger", "/robots.txt"} {
		if r := doReq(t, mux, http.MethodGet, path, nil); r.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d", path, r.Code)
		}
	}

	// GenericUI
	if r := doReq(t, mux, http.MethodGet, "/UI/View?PageId=settings", nil); r.Code != http.StatusOK {
		t.Fatalf("GET /UI/View status=%d", r.Code)
	}
	if r := doReq(t, mux, http.MethodGet, "/UI/View", nil); r.Code != http.StatusOK {
		t.Fatalf("GET /UI/View default status=%d", r.Code)
	}
	if r := doReq(t, mux, http.MethodPost, "/UI/Command", "{bad"); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", r.Code)
	}
	if r := doReq(t, mux, http.MethodPost, "/UI/Command", map[string]any{"PageId": "p"}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on missing CommandId, got %d", r.Code)
	}
	if r := doReq(t, mux, http.MethodPost, "/UI/Command", map[string]any{"PageId": "p", "CommandId": "save"}); r.Code != http.StatusOK {
		t.Fatalf("POST /UI/Command status=%d", r.Code)
	}

	// Parties
	if r := doReq(t, mux, http.MethodGet, "/Parties", nil); r.Code != http.StatusOK {
		t.Fatalf("GET /Parties status=%d", r.Code)
	}
	if r := doReq(t, mux, http.MethodPost, "/Parties", "{bad"); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", r.Code)
	}
	res := doReq(t, mux, http.MethodPost, "/Parties", map[string]any{
		"ItemId": "movie-1",
	})
	if res.Code != http.StatusOK {
		t.Fatalf("POST /Parties status=%d", res.Code)
	}
	var party Party
	_ = json.Unmarshal(res.Body.Bytes(), &party)
	if party.Id == "" || party.Name != "Watch Party" || len(party.Members) != 1 {
		t.Fatalf("unexpected party: %+v", party)
	}

	if r := doReq(t, mux, http.MethodGet, "/Parties/"+party.Id, nil); r.Code != http.StatusOK {
		t.Fatalf("GET /Parties/{Id} status=%d", r.Code)
	}
	if r := doReq(t, mux, http.MethodGet, "/Parties/missing", nil); r.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", r.Code)
	}

	// Join (new member + duplicate + 404)
	res = doReq(t, mux, http.MethodPost, "/Parties/"+party.Id+"/Join", map[string]any{"UserId": "guest-2"})
	if res.Code != http.StatusOK {
		t.Fatalf("POST Join status=%d", res.Code)
	}
	_ = json.Unmarshal(res.Body.Bytes(), &party)
	if len(party.Members) != 2 {
		t.Fatalf("expected 2 members, got %+v", party.Members)
	}
	// Duplicate join
	res = doReq(t, mux, http.MethodPost, "/Parties/"+party.Id+"/Join", map[string]any{"UserId": "guest-2"})
	_ = json.Unmarshal(res.Body.Bytes(), &party)
	if len(party.Members) != 2 {
		t.Fatalf("expected deduplicated 2 members, got %+v", party.Members)
	}
	if r := doReq(t, mux, http.MethodPost, "/Parties/missing/Join", map[string]any{"UserId": "guest-2"}); r.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", r.Code)
	}

	// State update
	if r := doReq(t, mux, http.MethodPost, "/Parties/"+party.Id+"/State", "{bad"); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", r.Code)
	}
	pos := int64(123456)
	paused := true
	rate := 1.25
	res = doReq(t, mux, http.MethodPost, "/Parties/"+party.Id+"/State", map[string]any{
		"ItemId":        "movie-2",
		"PositionTicks": pos,
		"IsPaused":      paused,
		"PlaybackRate":  rate,
	})
	if res.Code != http.StatusOK {
		t.Fatalf("POST State status=%d", res.Code)
	}
	_ = json.Unmarshal(res.Body.Bytes(), &party)
	if party.State.PositionTicks != pos || !party.State.IsPaused || party.State.PlaybackRate != rate {
		t.Fatalf("unexpected party state: %+v", party.State)
	}
	if r := doReq(t, mux, http.MethodPost, "/Parties/missing/State", map[string]any{"ItemId": "x"}); r.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", r.Code)
	}

	// Leave
	if r := doReq(t, mux, http.MethodPost, "/Parties/"+party.Id+"/Leave", map[string]any{"UserId": "guest-2"}); r.Code != http.StatusNoContent {
		t.Fatalf("POST Leave status=%d", r.Code)
	}
	if r := doReq(t, mux, http.MethodPost, "/Parties/missing/Leave", map[string]any{"UserId": "guest-2"}); r.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", r.Code)
	}

	// Delete party
	if r := doReq(t, mux, http.MethodDelete, "/Parties/"+party.Id, nil); r.Code != http.StatusNoContent {
		t.Fatalf("DELETE Party status=%d", r.Code)
	}
	if r := doReq(t, mux, http.MethodDelete, "/Parties/"+party.Id, nil); r.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", r.Code)
	}

	// Connect
	if r := doReq(t, mux, http.MethodGet, "/Connect/Pending", nil); r.Code != http.StatusOK {
		t.Fatalf("GET /Connect/Pending status=%d", r.Code)
	}
	if r := doReq(t, mux, http.MethodPost, "/Connect/Exchange", map[string]any{}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", r.Code)
	}
	res = doReq(t, mux, http.MethodPost, "/Connect/Exchange", map[string]any{"ConnectUserId": "connect-u1"})
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("POST /Connect/Exchange expected 401, got %d", res.Code)
	}

	// Join/Leave/Exchange without UserIDFromCtx
	noAuthH := New(nil, nil, nil, nil)
	noAuthMux := http.NewServeMux()
	noAuthH.RegisterRoutes(noAuthMux, AuthFuncs{})
	if r := doReq(t, noAuthMux, http.MethodPost, "/Parties/p1/Join", map[string]any{}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when no UserId, got %d", r.Code)
	}
	if r := doReq(t, noAuthMux, http.MethodPost, "/Parties/p1/Leave", map[string]any{}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when no UserId, got %d", r.Code)
	}
	if r := doReq(t, noAuthMux, http.MethodPost, "/Connect/Exchange?ConnectUserId=cu-99", nil); r.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on unlinked ConnectUserId, got %d", r.Code)
	}
}

func TestWebhooksSSRFHardening(t *testing.T) {
	_, mux, _, _, _ := setupOpsTest(t)

	blockedURLs := []string{
		"http://169.254.169.254/latest/meta-data/",
		"http://[fe80::1]/hook",
		"http://0.0.0.0/hook",
		"http://224.0.0.1/hook",
		"file:///etc/passwd",
		"ftp://example.com/hook",
	}

	for _, u := range blockedURLs {
		t.Run("create_"+u, func(t *testing.T) {
			rec := doReq(t, mux, http.MethodPost, "/Webhooks", map[string]any{
				"Name":    "Evil Hook",
				"Url":     u,
				"Enabled": true,
			})
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("POST /Webhooks with %q: got %d, want 400 (body=%s)", u, rec.Code, rec.Body.String())
			}
		})
		t.Run("test_"+u, func(t *testing.T) {
			rec := doReq(t, mux, http.MethodPost, "/Webhooks/Test", map[string]any{
				"Url": u,
			})
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("POST /Webhooks/Test with %q: got %d, want 400 (body=%s)", u, rec.Code, rec.Body.String())
			}
		})
	}

	// Redirect from allowed loopback server to cloud metadata must be blocked by SafeHTTPClient
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer redirector.Close()

	rec := doReq(t, mux, http.MethodPost, "/Webhooks/Test", map[string]any{
		"Url": redirector.URL,
	})
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 when webhook redirects to metadata IP, got %d body=%s", rec.Code, rec.Body.String())
	}
}
