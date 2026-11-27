package ops

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/api/ws"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/pkg/netutil"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestOpsStartupWizardUnifiedStateMachineAndRateLimit(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	st, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	userRepo := sqlite.NewUserRepo(st)
	h := New(st, cfg, userRepo, nil)
	authMW := middleware.NewAuth(nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
	})

	// 1. H-06: /Startup/Complete MUST fail with 400 when 0 users exist!
	if rec := doAuthReq(t, mux, nil, http.MethodPost, "/Startup/Complete", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 completing startup wizard with 0 users, got %d body=%s", rec.Code, rec.Body.String())
	}
	if cfg.Snapshot().IsStartupWizardCompleted {
		t.Fatal("IsStartupWizardCompleted must remain false when 0 users exist")
	}

	// 2. L-03: /Startup/User rejects control characters in username
	if rec := doAuthReq(t, mux, nil, http.MethodPost, "/Startup/User", map[string]any{
		"Name":     "bad\x00admin",
		"Password": "password123",
	}); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for control char in startup username, got %d", rec.Code)
	}

	// 3. H-06: Source-IP rate limiting on /Startup/* for non-loopback IPs
	for i := 0; i < startupRateLimit; i++ {
		req := httptest.NewRequest(http.MethodGet, "/Startup/Configuration", nil)
		req.RemoteAddr = "203.0.113.77:12345"
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("attempt %d expected 200, got %d", i+1, rec.Code)
		}
	}
	reqBlocked := httptest.NewRequest(http.MethodGet, "/Startup/Configuration", nil)
	reqBlocked.RemoteAddr = "203.0.113.77:12345"
	recBlocked := httptest.NewRecorder()
	mux.ServeHTTP(recBlocked, reqBlocked)
	if recBlocked.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on startup wizard rate limit, got %d", recBlocked.Code)
	}

	// 4. H-13: /Startup/Configuration & /Startup/RemoteAccess sync with system config
	if rec := doAuthReq(t, mux, nil, http.MethodPost, "/Startup/Configuration", map[string]any{
		"ServerName":                "Unified Ambra",
		"UICulture":                 "ja-JP",
		"MetadataCountryCode":       "JP",
		"PreferredMetadataLanguage": "ja-JP",
	}); rec.Code != http.StatusNoContent {
		t.Fatalf("POST /Startup/Configuration got %d", rec.Code)
	}
	if rec := doAuthReq(t, mux, nil, http.MethodPost, "/Startup/RemoteAccess", map[string]any{
		"EnableRemoteAccess":         false,
		"EnableAutomaticPortMapping": false,
	}); rec.Code != http.StatusNoContent {
		t.Fatalf("POST /Startup/RemoteAccess got %d", rec.Code)
	}
	snap := cfg.Snapshot()
	if snap.ServerName != "Unified Ambra" || snap.TMDBLanguage != "ja-JP" || snap.IsGlobalRemoteAccessEnabled() {
		t.Fatalf("expected global config updated from Startup endpoints, got %+v", snap)
	}
	getCfg := doAuthReq(t, mux, nil, http.MethodGet, "/Startup/Configuration", nil)
	var gotStartup StartupConfiguration
	_ = json.Unmarshal(getCfg.Body.Bytes(), &gotStartup)
	if gotStartup.UICulture != "ja-JP" || gotStartup.MetadataCountryCode != "JP" {
		t.Fatalf("expected system:config_extra synced, got %+v", gotStartup)
	}

	// 5. H-06: Even if 0 users exist, once IsStartupWizardCompleted is true, /Startup/* requires Admin (401 unauthenticated)
	cfg.Update(func(c *config.Config) { c.IsStartupWizardCompleted = true })
	if rec := doAuthReq(t, mux, nil, http.MethodGet, "/Startup/Configuration", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 when IsStartupWizardCompleted=true even with 0 users, got %d", rec.Code)
	}
}

func TestOpsAdminRBACConvergence(t *testing.T) {
	h, _, _, _, _ := setupOpsTest(t)
	authMW := middleware.NewAuth(nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
	})

	viewer := makeAuthCtx("viewer-rbac", "viewer", false)
	admin := makeAuthCtx("admin-rbac", "admin", true)

	for _, tc := range []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/Packages/Updates", nil},
		{http.MethodGet, "/UI/View", nil},
		{http.MethodPost, "/UI/Command", map[string]any{"PageId": "p", "CommandId": "save"}},
	} {
		if r := doAuthReq(t, mux, viewer, tc.method, tc.path, tc.body); r.Code != http.StatusForbidden {
			t.Fatalf("H-09: expected 403 for non-admin %s %s, got %d", tc.method, tc.path, r.Code)
		}
		if r := doAuthReq(t, mux, admin, tc.method, tc.path, tc.body); r.Code != http.StatusOK {
			t.Fatalf("H-09: expected 200 for admin %s %s, got %d", tc.method, tc.path, r.Code)
		}
	}
}

func TestOpsWebhooksSQLiteMigrationCapAndPathSanitization(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	st, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	// Seed legacy ops:state with a webhook when webhooks table is empty
	legacyJSON := `{"webhooks":[{"Id":"legacy-wh-1","Name":"LegacyHook","Url":"https://93.184.216.34/legacy","Events":["*"],"Secret":"s1","Enabled":true}]}`
	_, err = st.Writer.Exec(`INSERT INTO server_config(key, value_json, updated_at) VALUES(?, ?, 1)`, stateConfigKey, legacyJSON)
	if err != nil {
		t.Fatal(err)
	}

	bus := eventbus.New()
	h := New(st, cfg, sqlite.NewUserRepo(st), bus)
	t.Cleanup(h.Close)

	var dbCount int
	if err := st.Reader.QueryRow(`SELECT COUNT(*) FROM webhooks WHERE id = 'legacy-wh-1'`).Scan(&dbCount); err != nil || dbCount != 1 {
		t.Fatalf("expected legacy webhook migrated to SQLite webhooks table, count=%d err=%v", dbCount, err)
	}

	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	// Cap at maxWebhooks (50): already have 1, create 49 more -> 51st fails with 400
	for i := 1; i < maxWebhooks; i++ {
		rec := doReq(t, mux, http.MethodPost, "/Webhooks", map[string]any{
			"Id":      fmt.Sprintf("wh-%d", i),
			"Url":     "https://93.184.216.34/hook",
			"Enabled": false,
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("webhook #%d expected 200, got %d %s", i+1, rec.Code, rec.Body.String())
		}
	}
	if recOver := doReq(t, mux, http.MethodPost, "/Webhooks", map[string]any{
		"Id":      "wh-51",
		"Url":     "https://93.184.216.34/overflow",
		"Enabled": false,
	}); recOver.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on 51st webhook, got %d %s", recOver.Code, recOver.Body.String())
	}

	// Outbound payload strips host filesystem paths
	var gotPayload atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		gotPayload.Store(string(raw))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	h.SetHTTPClient(netutil.NewSafeHTTPClient(5*time.Second, true))

	// Update wh-1 to point to srv.URL and enable it
	if rec := doReq(t, mux, http.MethodPost, "/Webhooks", map[string]any{
		"Id":      "wh-1",
		"Url":     srv.URL,
		"Events":  []string{"LibraryChanged"},
		"Enabled": true,
	}); rec.Code != http.StatusOK {
		t.Fatalf("enable wh-1: %d", rec.Code)
	}

	bus.Publish(context.Background(), eventbus.Event{
		Type: eventbus.EventLibraryChanged,
		Payload: map[string]any{
			"ItemsAdded":  2,
			"path":        "/var/lib/ambra/secret-movie.mkv",
			"output_path": "/tmp/transcode/out.ts",
			"Nested": map[string]any{
				"Path":  "/mnt/nas/private",
				"Title": "Safe Title",
			},
		},
	})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s, _ := gotPayload.Load().(string); s != "" {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	bodyStr, _ := gotPayload.Load().(string)
	if bodyStr == "" {
		t.Fatal("expected webhook delivery")
	}
	if strings.Contains(bodyStr, "/var/lib/ambra") || strings.Contains(bodyStr, "/tmp/transcode") || strings.Contains(bodyStr, "/mnt/nas/private") {
		t.Fatalf("M-04: outbound webhook payload leaked host paths: %s", bodyStr)
	}
	if !strings.Contains(bodyStr, "Safe Title") {
		t.Fatalf("expected non-path fields preserved, got %s", bodyStr)
	}
}

func writeCustomZip(t *testing.T, zipPath string, build func(zw *zip.Writer)) {
	t.Helper()
	f, err := os.OpenFile(zipPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	build(zw)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOpsZipBackupAndRestoreSecurityAndLegacyCompat(t *testing.T) {
	h, mux, _, cfg, _ := setupOpsTest(t)

	// Seed user avatar metadata + symlink inside metadata/users (symlink must be skipped on backup)
	avatarDir := filepath.Join(cfg.DataDir, "metadata", "users", "user1")
	if err := os.MkdirAll(avatarDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(avatarDir, "primary_0.img"), []byte("PNGDATA"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.Symlink("/etc/hosts", filepath.Join(avatarDir, "evil_link"))

	// 1. Create full ZIP backup
	rec := doReq(t, mux, http.MethodPost, "/BackupRestore/Backup", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("Backup failed: %d %s", rec.Code, rec.Body.String())
	}
	var bk BackupFileInfo
	_ = json.Unmarshal(rec.Body.Bytes(), &bk)
	if !strings.HasSuffix(bk.Name, ".zip") {
		t.Fatalf("expected .zip backup archive, got %q", bk.Name)
	}

	// Verify ZIP contains ambra.db, config/ambra.json, metadata/users/user1/primary_0.img, and NO evil_link
	zr, err := zip.OpenReader(bk.Path)
	if err != nil {
		t.Fatal(err)
	}
	seenEntries := map[string]bool{}
	for _, zf := range zr.File {
		seenEntries[zf.Name] = true
	}
	_ = zr.Close()
	for _, want := range []string{"ambra.db", "config/ambra.json", "metadata/users/user1/primary_0.img"} {
		if !seenEntries[want] {
			t.Fatalf("expected %q in backup zip, got %v", want, seenEntries)
		}
	}
	if seenEntries["metadata/users/user1/evil_link"] {
		t.Fatal("backup zip must skip symlinks inside metadata/users")
	}

	// 2. Restore from ZIP stages ambra.db.restore, config/ambra.json.restore, and metadata.restore
	resRec := doReq(t, mux, http.MethodPost, "/BackupRestore/Restore", map[string]any{"Name": bk.Name})
	if resRec.Code != http.StatusOK {
		t.Fatalf("Restore zip failed: %d %s", resRec.Code, resRec.Body.String())
	}
	for _, staged := range []string{
		filepath.Join(cfg.DataDir, "ambra.db.restore"),
		filepath.Join(cfg.DataDir, "config", "ambra.json.restore"),
		filepath.Join(cfg.DataDir, "metadata.restore", "users", "user1", "primary_0.img"),
	} {
		if _, err := os.Stat(staged); err != nil {
			t.Fatalf("expected staged artifact %s: %v", staged, err)
		}
	}

	// 3. Reject malicious ZIPs: ZipSlip, symlink entry, missing ambra.db, disallowed path
	bDir := h.backupsDir()
	zipSlipPath := filepath.Join(bDir, "zipslip.zip")
	writeCustomZip(t, zipSlipPath, func(zw *zip.Writer) {
		w, _ := zw.Create("../../evil.txt")
		_, _ = w.Write([]byte("pwned"))
	})
	if r := doReq(t, mux, http.MethodPost, "/BackupRestore/Restore", map[string]any{"Name": "zipslip.zip"}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for ZipSlip archive, got %d", r.Code)
	}

	symlinkZipPath := filepath.Join(bDir, "symlink.zip")
	writeCustomZip(t, symlinkZipPath, func(zw *zip.Writer) {
		hdr := &zip.FileHeader{Name: "metadata/users/u1/link"}
		hdr.SetMode(os.ModeSymlink | 0o777)
		w, _ := zw.CreateHeader(hdr)
		_, _ = w.Write([]byte("/etc/passwd"))
	})
	if r := doReq(t, mux, http.MethodPost, "/BackupRestore/Restore", map[string]any{"Name": "symlink.zip"}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for symlink zip entry, got %d", r.Code)
	}

	// 4. Legacy single-file .db backup backward compatibility
	legacyDB := filepath.Join(bDir, "legacy-v010.db")
	tmpLegacyStore, err := sqlite.Open(legacyDB)
	if err != nil {
		t.Fatal(err)
	}
	_ = tmpLegacyStore.Close()
	if r := doReq(t, mux, http.MethodPost, "/BackupRestore/Restore", map[string]any{"Name": "legacy-v010.db"}); r.Code != http.StatusOK {
		t.Fatalf("expected 200 restoring legacy .db backup, got %d %s", r.Code, r.Body.String())
	}
}

func TestOpsPartiesFullWatchTogetherLoopAndPurgeUser(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	st, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	ctx := context.Background()
	userRepo := sqlite.NewUserRepo(st)
	authSvc := auth.NewService(userRepo, cfg)
	bus := eventbus.New()
	sessMgr := session.NewManager(userRepo, cfg, bus)
	wsHub := ws.NewHub(authSvc, sessMgr, bus)

	alice, _ := authSvc.CreateUser(ctx, "alice", "pass1234", false)
	bob, _ := authSvc.CreateUser(ctx, "bob", "pass1234", false)
	bobAuth, err := authSvc.AuthenticateByName(ctx, "bob", "pass1234", "", auth.ClientInfo{DeviceID: "bob-tv", Client: "Web"})
	if err != nil {
		t.Fatal(err)
	}

	h := New(st, cfg, userRepo, bus)
	h.SetWSHub(wsHub)
	t.Cleanup(h.Close)

	authMW := middleware.NewAuth(authSvc, sessMgr)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
	})
	mux.Handle("/embywebsocket", wsHub)

	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Connect Bob via WebSocket
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/embywebsocket?api_key=" + bobAuth.AccessToken + "&deviceId=bob-tv"
	dialCtx, dialCancel := context.WithTimeout(ctx, 5*time.Second)
	defer dialCancel()
	wsConn, _, err := websocket.Dial(dialCtx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	defer wsConn.Close(websocket.StatusNormalClosure, "done")
	{
		initCtx, initCancel := context.WithTimeout(ctx, 2*time.Second)
		var initMsg ws.Message
		if err := wsjson.Read(initCtx, wsConn, &initMsg); err != nil {
			initCancel()
			t.Fatalf("read initial ws greeting: %v", err)
		}
		initCancel()
	}

	readWSMsg := func() ws.Message {
		t.Helper()
		for {
			rCtx, rCancel := context.WithTimeout(ctx, 2*time.Second)
			var msg ws.Message
			err := wsjson.Read(rCtx, wsConn, &msg)
			rCancel()
			if err != nil {
				t.Fatalf("read ws json: %v", err)
			}
			if msg.MessageType == "ForceKeepAlive" || msg.MessageType == "KeepAlive" {
				continue
			}
			return msg
		}
	}

	aliceAC := makeAuthCtx(alice.ID, alice.Name, false)
	bobAC := makeAuthCtx(bob.ID, bob.Name, false)

	// Initially GET /Parties/Info shows InParty=false
	infoBefore := doAuthReq(t, mux, aliceAC, http.MethodGet, "/Parties/Info", nil)
	if infoBefore.Code != http.StatusOK || !strings.Contains(infoBefore.Body.String(), `"InParty":false`) {
		t.Fatalf("expected InParty=false, got %d %s", infoBefore.Code, infoBefore.Body.String())
	}

	// Alice creates party, Bob joins
	createRec := doAuthReq(t, mux, aliceAC, http.MethodPost, "/Parties", map[string]any{
		"Name":   "SciFi Together",
		"ItemId": "movie-42",
	})
	var party Party
	_ = json.Unmarshal(createRec.Body.Bytes(), &party)

	if r := doAuthReq(t, mux, bobAC, http.MethodPost, "/Parties/"+party.Id+"/Join", nil); r.Code != http.StatusOK {
		t.Fatalf("Bob join failed: %d", r.Code)
	}

	// Drain PartyUpdated frame on Bob's WS
	if joinMsg := readWSMsg(); joinMsg.MessageType != "PartyUpdated" {
		t.Fatalf("expected PartyUpdated WS message on join, got %+v", joinMsg)
	}

	// Alice updates party playback state -> Bob receives Playstate WS push
	pos := int64(900_000_000)
	if r := doAuthReq(t, mux, aliceAC, http.MethodPost, "/Parties/"+party.Id+"/State", map[string]any{
		"PositionTicks": pos,
		"IsPaused":      false,
	}); r.Code != http.StatusOK {
		t.Fatalf("State update failed: %d", r.Code)
	}
	if stateMsg := readWSMsg(); stateMsg.MessageType != "Playstate" {
		t.Fatalf("expected Playstate WS push to Bob, got %+v", stateMsg)
	}

	// Alice posts chat message via POST /Parties/Messages -> Bob receives DisplayMessage WS push and GET /Parties/Messages lists it
	if r := doAuthReq(t, mux, aliceAC, http.MethodPost, "/Parties/Messages", map[string]any{
		"Text": "Ready to play!",
	}); r.Code != http.StatusNoContent {
		t.Fatalf("POST /Parties/Messages failed: %d %s", r.Code, r.Body.String())
	}
	if chatMsg := readWSMsg(); chatMsg.MessageType != "GeneralCommand" {
		t.Fatalf("expected GeneralCommand DisplayMessage WS push, got %+v", chatMsg)
	}
	msgListRec := doAuthReq(t, mux, bobAC, http.MethodGet, "/Parties/Messages", nil)
	if msgListRec.Code != http.StatusOK || !strings.Contains(msgListRec.Body.String(), "Ready to play!") {
		t.Fatalf("GET /Parties/Messages unexpected: %d %s", msgListRec.Code, msgListRec.Body.String())
	}

	// Bob leaves via static POST /Parties/Leave; then PurgeUser(alice) removes remaining empty room
	if r := doAuthReq(t, mux, bobAC, http.MethodPost, "/Parties/Leave", nil); r.Code != http.StatusNoContent {
		t.Fatalf("POST /Parties/Leave failed: %d", r.Code)
	}
	h.PurgeUser(ctx, alice.ID)
	infoAfter := doAuthReq(t, mux, aliceAC, http.MethodGet, "/Parties/Info", nil)
	if !strings.Contains(infoAfter.Body.String(), `"InParty":false`) {
		t.Fatalf("expected room cleaned up after PurgeUser, got %s", infoAfter.Body.String())
	}
}

func TestBackupRestoreCleansStaleStagedArtifacts(t *testing.T) {
	h, mux, st, cfg, _ := setupOpsTest(t)

	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	avatarDir := filepath.Join(cfg.DataDir, "metadata", "users", "u1")
	if err := os.MkdirAll(avatarDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(avatarDir, "primary_0.img"), []byte("avatar"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 1. Create and stage a full zip backup -> ambra.db.restore, config/ambra.json.restore, and metadata.restore all exist.
	bkRes := doReq(t, mux, http.MethodPost, "/BackupRestore/Backup", nil)
	if bkRes.Code != http.StatusOK {
		t.Fatalf("create backup failed: %d %s", bkRes.Code, bkRes.Body.String())
	}
	var bk BackupFileInfo
	_ = json.Unmarshal(bkRes.Body.Bytes(), &bk)
	if r := doReq(t, mux, http.MethodPost, "/BackupRestore/Restore", map[string]any{"Name": bk.Name}); r.Code != http.StatusOK {
		t.Fatalf("restore full zip failed: %d %s", r.Code, r.Body.String())
	}

	stagedCfg := filepath.Join(cfg.DataDir, "config", "ambra.json.restore")
	stagedMeta := filepath.Join(cfg.DataDir, "metadata.restore")
	if _, err := os.Stat(stagedCfg); err != nil {
		t.Fatalf("expected staged config restore file: %v", err)
	}
	if _, err := os.Stat(stagedMeta); err != nil {
		t.Fatalf("expected staged metadata restore dir: %v", err)
	}

	// 2. Stage a DB-only zip backup (!hasConfig, !hasMetadata) -> stale config/ambra.json.restore and metadata.restore MUST be removed!
	rawSnap := filepath.Join(h.backupsDir(), "dbonly-source.db")
	escaped := strings.ReplaceAll(rawSnap, "'", "''")
	if _, err := st.Writer.Exec(fmt.Sprintf("VACUUM INTO '%s'", escaped)); err != nil {
		t.Fatal(err)
	}
	validDBBytes, err := os.ReadFile(rawSnap)
	if err != nil {
		t.Fatal(err)
	}
	dbOnlyZip := filepath.Join(h.backupsDir(), "db-only.zip")
	buildTestZipV0110(t, dbOnlyZip, map[string][]byte{"ambra.db": validDBBytes})
	if r := doReq(t, mux, http.MethodPost, "/BackupRestore/Restore", map[string]any{"Name": "db-only.zip"}); r.Code != http.StatusOK {
		t.Fatalf("restore db-only zip failed: %d %s", r.Code, r.Body.String())
	}
	if _, err := os.Stat(stagedCfg); !os.IsNotExist(err) {
		t.Fatalf("expected stale ambra.json.restore removed on !hasConfig zip restore, err=%v", err)
	}
	if _, err := os.Stat(stagedMeta); !os.IsNotExist(err) {
		t.Fatalf("expected stale metadata.restore removed on !hasMetadata zip restore, err=%v", err)
	}

	// 3. Re-stage full zip backup, then stage raw SQLite .db backup -> stale ambra.json.restore and metadata.restore MUST be removed!
	if r := doReq(t, mux, http.MethodPost, "/BackupRestore/Restore", map[string]any{"Name": bk.Name}); r.Code != http.StatusOK {
		t.Fatalf("re-restore full zip failed: %d", r.Code)
	}
	if _, err := os.Stat(stagedCfg); err != nil {
		t.Fatalf("expected staged config restore recreated: %v", err)
	}
	if _, err := os.Stat(stagedMeta); err != nil {
		t.Fatalf("expected staged metadata restore recreated: %v", err)
	}
	if r := doReq(t, mux, http.MethodPost, "/BackupRestore/Restore", map[string]any{"Name": "dbonly-source.db"}); r.Code != http.StatusOK {
		t.Fatalf("restore raw sqlite db failed: %d %s", r.Code, r.Body.String())
	}
	if _, err := os.Stat(stagedCfg); !os.IsNotExist(err) {
		t.Fatalf("expected stale ambra.json.restore removed on raw .db restore, err=%v", err)
	}
	if _, err := os.Stat(stagedMeta); !os.IsNotExist(err) {
		t.Fatalf("expected stale metadata.restore removed on raw .db restore, err=%v", err)
	}
}

func TestPartyJoinTOCTOUAndPlaystateWSMemberScrubbing(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	st, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	ctx := context.Background()
	userRepo := sqlite.NewUserRepo(st)
	authSvc := auth.NewService(userRepo, cfg)
	bus := eventbus.New()
	sessMgr := session.NewManager(userRepo, cfg, bus)
	wsHub := ws.NewHub(authSvc, sessMgr, bus)

	hostUser, _ := authSvc.CreateUser(ctx, "host_adult", "pass1234", false)
	allowedMember, _ := authSvc.CreateUser(ctx, "member_adult", "pass1234", false)
	restrictedKid, _ := authSvc.CreateUser(ctx, "member_kid", "pass1234", false)

	adultAuth, err := authSvc.AuthenticateByName(ctx, "member_adult", "pass1234", "", auth.ClientInfo{DeviceID: "adult-tv", Client: "Web"})
	if err != nil {
		t.Fatal(err)
	}
	kidAuth, err := authSvc.AuthenticateByName(ctx, "member_kid", "pass1234", "", auth.ClientInfo{DeviceID: "kid-tablet", Client: "Web"})
	if err != nil {
		t.Fatal(err)
	}

	h := New(st, cfg, userRepo, bus)
	h.SetWSHub(wsHub)
	t.Cleanup(h.Close)

	var swapTriggered atomic.Bool
	var targetPartyID atomic.Value
	h.SetItemAccessChecker(func(_ context.Context, user *sqlite.UserRecord, itemID string) bool {
		if user == nil {
			return false
		}
		// Simulate concurrent host State.ItemId mutation while handleJoinParty released h.mu!
		if user.Name == "member_kid" && itemID == "safe-cartoon" && swapTriggered.CompareAndSwap(false, true) {
			if pid, _ := targetPartyID.Load().(string); pid != "" {
				h.mu.Lock()
				for i := range h.state.Parties {
					if h.state.Parties[i].Id == pid {
						h.state.Parties[i].State.ItemId = "restricted-r-movie"
					}
				}
				h.mu.Unlock()
			}
			return true
		}
		if strings.HasPrefix(itemID, "restricted-") && user.Name == "member_kid" {
			return false
		}
		return true
	})

	authMW := middleware.NewAuth(authSvc, sessMgr)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
	})
	mux.Handle("/embywebsocket", wsHub)

	srv := httptest.NewServer(mux)
	defer srv.Close()

	dialWS := func(tok, devID string) *websocket.Conn {
		t.Helper()
		wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/embywebsocket?api_key=" + tok + "&deviceId=" + devID
		dCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		conn, _, dialErr := websocket.Dial(dCtx, wsURL, nil)
		if dialErr != nil {
			t.Fatalf("dial ws %s: %v", devID, dialErr)
		}
		t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "done") })
		var greeting ws.Message
		rCtx, rCancel := context.WithTimeout(ctx, 2*time.Second)
		defer rCancel()
		if err := wsjson.Read(rCtx, conn, &greeting); err != nil {
			t.Fatalf("ws greeting %s: %v", devID, err)
		}
		return conn
	}
	readNextNonKeepAlive := func(conn *websocket.Conn) ws.Message {
		t.Helper()
		for {
			rCtx, rCancel := context.WithTimeout(ctx, 2*time.Second)
			var msg ws.Message
			err := wsjson.Read(rCtx, conn, &msg)
			rCancel()
			if err != nil {
				t.Fatalf("read ws msg: %v", err)
			}
			if msg.MessageType == "ForceKeepAlive" || msg.MessageType == "KeepAlive" {
				continue
			}
			return msg
		}
	}

	hostAC := makeAuthCtx(hostUser.ID, hostUser.Name, false)
	adultAC := makeAuthCtx(allowedMember.ID, allowedMember.Name, false)
	kidAC := makeAuthCtx(restrictedKid.ID, restrictedKid.Name, false)

	// 1. TOCTOU Join test: party starts on "safe-cartoon", but concurrently switches to "restricted-r-movie"
	// while kid's Join item check has unlocked h.mu -> Join MUST re-verify and reject with 403!
	cRec := doAuthReq(t, mux, hostAC, http.MethodPost, "/Parties", map[string]any{
		"Name":   "Race Room",
		"ItemId": "safe-cartoon",
	})
	var raceParty Party
	_ = json.Unmarshal(cRec.Body.Bytes(), &raceParty)
	targetPartyID.Store(raceParty.Id)

	joinRaceRec := doAuthReq(t, mux, kidAC, http.MethodPost, "/Parties/"+raceParty.Id+"/Join", nil)
	if joinRaceRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when party ItemId concurrently switched to restricted item during Join, got %d (%s)",
			joinRaceRec.Code, joinRaceRec.Body.String())
	}

	// 2. Playstate WS broadcast scrubbing: create a party on "family-movie" where both adult and kid join,
	// then host switches State.ItemId to "restricted-thriller" -> adult WS gets ItemId, kid WS gets ItemId==""!
	cRec2 := doAuthReq(t, mux, hostAC, http.MethodPost, "/Parties", map[string]any{
		"Name":   "Mixed Room",
		"ItemId": "family-movie",
	})
	var mixedParty Party
	_ = json.Unmarshal(cRec2.Body.Bytes(), &mixedParty)

	adultConn := dialWS(adultAuth.AccessToken, "adult-tv")
	kidConn := dialWS(kidAuth.AccessToken, "kid-tablet")

	if r := doAuthReq(t, mux, adultAC, http.MethodPost, "/Parties/"+mixedParty.Id+"/Join", nil); r.Code != http.StatusOK {
		t.Fatalf("adult join failed: %d", r.Code)
	}
	_ = readNextNonKeepAlive(adultConn) // PartyUpdated

	if r := doAuthReq(t, mux, kidAC, http.MethodPost, "/Parties/"+mixedParty.Id+"/Join", nil); r.Code != http.StatusOK {
		t.Fatalf("kid join failed: %d", r.Code)
	}
	_ = readNextNonKeepAlive(adultConn) // PartyUpdated
	_ = readNextNonKeepAlive(kidConn)   // PartyUpdated

	pos := int64(123456)
	updRec := doAuthReq(t, mux, hostAC, http.MethodPost, "/Parties/"+mixedParty.Id+"/State", map[string]any{
		"ItemId":        "restricted-thriller",
		"PositionTicks": pos,
		"IsPaused":      false,
	})
	if updRec.Code != http.StatusOK {
		t.Fatalf("host update state failed: %d %s", updRec.Code, updRec.Body.String())
	}

	adultPlay := readNextNonKeepAlive(adultConn)
	kidPlay := readNextNonKeepAlive(kidConn)
	adultData, _ := adultPlay.Data.(map[string]any)
	kidData, _ := kidPlay.Data.(map[string]any)
	if adultPlay.MessageType != "Playstate" || adultData["ItemId"] != "restricted-thriller" {
		t.Fatalf("expected adult member to receive Playstate with ItemId=restricted-thriller, got %+v", adultPlay)
	}
	if kidPlay.MessageType != "Playstate" || kidData["ItemId"] != "" {
		t.Fatalf("expected restricted kid member to receive Playstate with scrubbed ItemId=\"\", got %+v", kidPlay)
	}
}
