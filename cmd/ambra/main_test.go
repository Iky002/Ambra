package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/handlers/system"
	"github.com/ambra-media/ambra/internal/api/ws"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func allocateFreeLoopbackAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen 127.0.0.1:0: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

func TestM06_PreValidatePendingDBRestoreBeforeReplacingConfigOrMetadata(t *testing.T) {
	dataDir := t.TempDir()
	cfgDir := filepath.Join(dataDir, "config")
	usersDir := filepath.Join(dataDir, "metadata", "users", "u-orig")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(usersDir, 0o755); err != nil {
		t.Fatal(err)
	}

	origCfg := `{"server_id":"orig-srv","server_name":"Original Ambra","ffmpeg_path":"/opt/ffmpeg"}`
	if err := os.WriteFile(filepath.Join(cfgDir, "ambra.json"), []byte(origCfg), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(usersDir, "avatar.png"), []byte("orig-avatar-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Stage valid config + metadata restore alongside an INVALID ambra.db.restore (bad magic header)
	if err := os.WriteFile(filepath.Join(cfgDir, "ambra.json.restore"), []byte(`{"server_name":"ShouldNotApply"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	stagedAvatarDir := filepath.Join(dataDir, "metadata.restore", "users", "u-new")
	if err := os.MkdirAll(stagedAvatarDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stagedAvatarDir, "avatar.png"), []byte("new-avatar"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "ambra.db.restore"), []byte("NOT-SQLITE-HEADER-000000"), 0o600); err != nil {
		t.Fatal(err)
	}

	st := stagePendingRestore(dataDir)
	if st != nil {
		t.Fatalf("expected nil pendingRestoreState when ambra.db.restore has invalid header, got %+v", st)
	}

	// Verify live ambra.json and metadata/users were NOT replaced!
	gotCfg, err := os.ReadFile(filepath.Join(cfgDir, "ambra.json"))
	if err != nil || !strings.Contains(string(gotCfg), "Original Ambra") {
		t.Fatalf("expected original ambra.json untouched, got %q err=%v", string(gotCfg), err)
	}
	gotAvatar, err := os.ReadFile(filepath.Join(usersDir, "avatar.png"))
	if err != nil || string(gotAvatar) != "orig-avatar-bytes" {
		t.Fatalf("expected original user metadata untouched, got %q err=%v", string(gotAvatar), err)
	}

	// Also verify symlink ambra.db.restore pointing to a real SQLite DB is rejected by pre-validation!
	realExternalDB := filepath.Join(t.TempDir(), "ext.db")
	extStore, err := sqlite.Open(realExternalDB)
	if err != nil {
		t.Fatal(err)
	}
	_ = extStore.Close()
	_ = os.Remove(filepath.Join(dataDir, "ambra.db.restore"))
	if err := os.Symlink(realExternalDB, filepath.Join(dataDir, "ambra.db.restore")); err != nil {
		t.Fatal(err)
	}
	if stSym := stagePendingRestore(dataDir); stSym != nil {
		t.Fatalf("expected nil pendingRestoreState when ambra.db.restore is a symlink, got %+v", stSym)
	}
}

func TestM03_SanitizeRestoredConfigStripsFFmpegAndPreservesHostPaths(t *testing.T) {
	// 1. Existing ambra.json with host ffmpeg/ffprobe paths -> preserved while attacker paths are stripped.
	dir1 := t.TempDir()
	existingPath := filepath.Join(dir1, "ambra.json")
	if err := os.WriteFile(existingPath, []byte(`{
		"server_name": "Host Server",
		"ffmpeg_path": "/usr/local/bin/host-ffmpeg",
		"ffprobe_path": "/usr/local/bin/host-ffprobe"
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	maliciousRestore := []byte(`{
		"server_name": "Restored Server",
		"ffmpeg_path": "/tmp/evil/ffmpeg",
		"ffprobe_path": "/tmp/evil/ffprobe",
		"FFmpegPath": "/tmp/evil/ffmpeg2",
		"FFprobePath": "/tmp/evil/ffprobe2"
	}`)
	sanitized, ok := sanitizeRestoredConfigJSON(maliciousRestore, existingPath)
	if !ok {
		t.Fatal("expected sanitizeRestoredConfigJSON to succeed")
	}
	var parsed map[string]any
	if err := json.Unmarshal(sanitized, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["server_name"] != "Restored Server" {
		t.Fatalf("expected server_name=Restored Server, got %v", parsed["server_name"])
	}
	if parsed["ffmpeg_path"] != "/usr/local/bin/host-ffmpeg" || parsed["ffprobe_path"] != "/usr/local/bin/host-ffprobe" {
		t.Fatalf("expected host ffmpeg/ffprobe paths preserved, got %+v", parsed)
	}
	if _, hasAlt := parsed["FFmpegPath"]; hasAlt {
		t.Fatalf("expected FFmpegPath key stripped, got %+v", parsed)
	}

	// 2. Fresh install (no existing ambra.json) -> strips ffmpeg_path/ffprobe_path completely.
	dir2 := t.TempDir()
	sanitizedFresh, ok := sanitizeRestoredConfigJSON(maliciousRestore, filepath.Join(dir2, "ambra.json"))
	if !ok {
		t.Fatal("expected sanitizeRestoredConfigJSON on fresh dir to succeed")
	}
	var parsedFresh map[string]any
	if err := json.Unmarshal(sanitizedFresh, &parsedFresh); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"ffmpeg_path", "ffprobe_path", "FFmpegPath", "FFprobePath"} {
		if _, exists := parsedFresh[forbidden]; exists {
			t.Fatalf("expected %s stripped when no prior ambra.json existed, got %+v", forbidden, parsedFresh)
		}
	}
}

func TestM06_AtomicRestoreRollbackOnServerNewFailureAndCommitOnSuccess(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	cfgDir := filepath.Join(dataDir, "config")
	origUserAvatarDir := filepath.Join(dataDir, "metadata", "users", "orig-u1")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(origUserAvatarDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Seed valid live database, config, and user avatar metadata
	dbPath := filepath.Join(dataDir, "ambra.db")
	origStore, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("seed live db: %v", err)
	}
	origUserRepo := sqlite.NewUserRepo(origStore)
	if err := origUserRepo.CreateUser(ctx, &sqlite.UserRecord{
		ID:      "origadmin00000000000000000000001",
		Name:    "original-admin",
		IsAdmin: true,
		Policy:  sqlite.DefaultUserPolicy(true),
		Config:  sqlite.DefaultUserConfig(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := origStore.Close(); err != nil {
		t.Fatal(err)
	}

	origCfgJSON := `{
  "server_id": "origsrv0000000000000000000000001",
  "server_name": "Original Live Server",
  "http_addr": "127.0.0.1:0",
  "enable_dlna": false,
  "ffmpeg_path": "/opt/live/ffmpeg"
}`
	cfgPath := filepath.Join(cfgDir, "ambra.json")
	if err := os.WriteFile(cfgPath, []byte(origCfgJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	origAvatarPath := filepath.Join(origUserAvatarDir, "avatar.png")
	if err := os.WriteFile(origAvatarPath, []byte("original-avatar-png"), 0o644); err != nil {
		t.Fatal(err)
	}

	// --- PART 1: Staged restore with valid 16-byte SQLite header but corrupted B-tree pages ---
	// Pre-validation and applyPendingDatabaseRestore pass, then sqlite.Open inside server.New fails -> FULL ROLLBACK!
	corruptSQLite := make([]byte, 4096)
	copy(corruptSQLite, []byte(sqliteRestoreMagicHeader))
	for i := len(sqliteRestoreMagicHeader); i < len(corruptSQLite); i++ {
		corruptSQLite[i] = 0xFF
	}
	if err := os.WriteFile(filepath.Join(dataDir, "ambra.db.restore"), corruptSQLite, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "ambra.json.restore"), []byte(`{
		"server_id": "brokensrv00000000000000000000002",
		"server_name": "Broken Restore Server",
		"enable_dlna": false,
		"ffmpeg_path": "/tmp/attacker/ffmpeg"
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	stagedBrokenMeta := filepath.Join(dataDir, "metadata.restore", "users", "broken-u2")
	if err := os.MkdirAll(stagedBrokenMeta, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stagedBrokenMeta, "avatar.png"), []byte("broken-avatar"), 0o644); err != nil {
		t.Fatal(err)
	}

	restartCh := make(chan struct{}, 1)
	shutdownCh := make(chan struct{}, 1)
	shouldRestart, exitCode := runServerInstance(ctx, dataDir, allocateFreeLoopbackAddr(t), "", restartCh, shutdownCh)
	if shouldRestart || exitCode != 1 {
		t.Fatalf("expected failed restore boot to return (false, 1), got (%v, %d)", shouldRestart, exitCode)
	}

	// Verify config, user avatars, and database ALL rolled back to Original Live Server!
	rolledBackCfg, err := os.ReadFile(cfgPath)
	if err != nil || !strings.Contains(string(rolledBackCfg), "Original Live Server") {
		t.Fatalf("expected ambra.json rolled back to Original Live Server, got %q err=%v", string(rolledBackCfg), err)
	}
	rolledBackAvatar, err := os.ReadFile(origAvatarPath)
	if err != nil || string(rolledBackAvatar) != "original-avatar-png" {
		t.Fatalf("expected metadata/users rolled back to original-avatar-png, got %q err=%v", string(rolledBackAvatar), err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "metadata", "users", "broken-u2")); !os.IsNotExist(err) {
		t.Fatalf("expected broken-u2 metadata removed on rollback, stat err=%v", err)
	}
	verifyStore, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("open rolled-back ambra.db: %v", err)
	}
	uOrig, err := sqlite.NewUserRepo(verifyStore).GetUserByID(ctx, "origadmin00000000000000000000001")
	_ = verifyStore.Close()
	if err != nil || uOrig == nil || uOrig.Name != "original-admin" {
		t.Fatalf("expected rolled-back ambra.db to contain original-admin, got u=%+v err=%v", uOrig, err)
	}

	// --- PART 2: Valid restore succeeds and commits (removing metadata/users.pre-restore.bak) ---
	validRestoreSeed := filepath.Join(t.TempDir(), "valid-restore.db")
	validStore, err := sqlite.Open(validRestoreSeed)
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlite.NewUserRepo(validStore).CreateUser(ctx, &sqlite.UserRecord{
		ID:      "restoredadmin0000000000000000009",
		Name:    "restored-admin",
		IsAdmin: true,
		Policy:  sqlite.DefaultUserPolicy(true),
		Config:  sqlite.DefaultUserConfig(),
	}); err != nil {
		t.Fatal(err)
	}
	_ = validStore.Close()
	seedBytes, err := os.ReadFile(validRestoreSeed)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "ambra.db.restore"), seedBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "ambra.json.restore"), []byte(`{
		"server_id": "restoredsrv000000000000000000009",
		"server_name": "Restored Live Server",
		"enable_dlna": false,
		"ffmpeg_path": "/tmp/attacker/evil-ffmpeg"
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	stagedGoodMeta := filepath.Join(dataDir, "metadata.restore", "users", "restored-u9")
	if err := os.MkdirAll(stagedGoodMeta, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stagedGoodMeta, "avatar.png"), []byte("restored-avatar-png"), 0o644); err != nil {
		t.Fatal(err)
	}

	addr := allocateFreeLoopbackAddr(t)
	shutdownCh2 := make(chan struct{}, 1)
	doneCh := make(chan struct {
		restart bool
		code    int
	}, 1)
	go func() {
		r, c := runServerInstance(ctx, dataDir, addr, "", make(chan struct{}, 1), shutdownCh2)
		doneCh <- struct {
			restart bool
			code    int
		}{r, c}
	}()

	// Poll /System/Ping until server is up, verify restored ServerName, then signal shutdownCh2
	baseURL := "http://" + addr
	deadline := time.Now().Add(5 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		resp, gErr := http.Get(baseURL + "/System/Info/Public")
		if gErr == nil {
			var pub dto.PublicSystemInfo
			_ = json.NewDecoder(resp.Body).Decode(&pub)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK && pub.ServerName == "Restored Live Server" {
				ready = true
				break
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	shutdownCh2 <- struct{}{}
	res := <-doneCh
	if !ready {
		t.Fatal("server did not come up with Restored Live Server")
	}
	if res.restart || res.code != 0 {
		t.Fatalf("expected clean shutdown (false, 0), got (%v, %d)", res.restart, res.code)
	}

	// Verify metadata/users.pre-restore.bak was cleaned up after server.New succeeded
	if _, err := os.Stat(filepath.Join(dataDir, "metadata", "users.pre-restore.bak")); !os.IsNotExist(err) {
		t.Fatalf("expected metadata/users.pre-restore.bak removed after successful commit, stat err=%v", err)
	}
	// Verify M-03 preserved host /opt/live/ffmpeg in promoted ambra.json
	finalCfgRaw, err := os.ReadFile(cfgPath)
	if err != nil || !strings.Contains(string(finalCfgRaw), "/opt/live/ffmpeg") || strings.Contains(string(finalCfgRaw), "evil-ffmpeg") {
		t.Fatalf("expected M-03 host ffmpeg preserved and attacker ffmpeg stripped in final ambra.json: %s", string(finalCfgRaw))
	}
}

func TestMain_SingleWSLifecycleBroadcastAndLogBridgeResetOrder(t *testing.T) {
	t.Setenv("AMBRA_ADMIN_USER", "admin")
	t.Setenv("AMBRA_ADMIN_PASS", "admin-pass-123")

	dataDir := t.TempDir()
	addr := allocateFreeLoopbackAddr(t)
	ctx := context.Background()

	restartCh := make(chan struct{}, 1)
	shutdownCh := make(chan struct{}, 1)
	unregister := system.RegisterProcessLifecycleController(
		func() {
			select {
			case restartCh <- struct{}{}:
			default:
			}
		},
		func() {
			select {
			case shutdownCh <- struct{}{}:
			default:
			}
		},
	)
	defer unregister()

	type runOutcome struct {
		restart bool
		code    int
	}
	outCh := make(chan runOutcome, 1)
	go func() {
		r, c := runServerInstance(ctx, dataDir, addr, "", restartCh, shutdownCh)
		outCh <- runOutcome{r, c}
	}()

	baseURL := "http://" + addr
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if resp, err := http.Get(baseURL + "/System/Ping"); err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Authenticate admin
	authBody := bytes.NewBufferString(`{"Username":"admin","Pw":"admin-pass-123"}`)
	authReq, _ := http.NewRequest(http.MethodPost, baseURL+"/Users/AuthenticateByName", authBody)
	authReq.Header.Set("Content-Type", "application/json")
	authReq.Header.Set("X-Emby-Authorization", `MediaBrowser Client="Test", Device="Go", DeviceId="d-main-ws", Version="1.0"`)
	authResp, err := http.DefaultClient.Do(authReq)
	if err != nil || authResp.StatusCode != http.StatusOK {
		t.Fatalf("auth failed: resp=%v err=%v", authResp, err)
	}
	var authRes dto.AuthenticationResult
	_ = json.NewDecoder(authResp.Body).Decode(&authRes)
	_ = authResp.Body.Close()

	// Connect WebSocket client
	wsURL := fmt.Sprintf("ws://%s/embywebsocket?api_key=%s", addr, authRes.AccessToken)
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket.Dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Read initial ForceKeepAlive
	readCtx, cancelRead := context.WithTimeout(ctx, 2*time.Second)
	var initMsg ws.Message
	if err := wsjson.Read(readCtx, conn, &initMsg); err != nil {
		cancelRead()
		t.Fatalf("read initial WS message: %v", err)
	}
	cancelRead()

	// Collect all WS messages until server closes connection during restart
	wsCountCh := make(chan int, 1)
	go func() {
		count := 0
		for {
			rctx, cancel := context.WithTimeout(ctx, 3*time.Second)
			var m ws.Message
			rErr := wsjson.Read(rctx, conn, &m)
			cancel()
			if rErr != nil {
				break
			}
			if m.MessageType == "ServerRestarting" {
				count++
			}
		}
		wsCountCh <- count
	}()

	// Trigger POST /System/Restart
	rstReq, _ := http.NewRequest(http.MethodPost, baseURL+"/System/Restart", nil)
	rstReq.Header.Set("X-Emby-Token", authRes.AccessToken)
	rstResp, err := http.DefaultClient.Do(rstReq)
	if err != nil || rstResp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST /System/Restart: resp=%v err=%v", rstResp, err)
	}
	_ = rstResp.Body.Close()

	outcome := <-outCh
	if !outcome.restart || outcome.code != 0 {
		t.Fatalf("expected (true, 0) on restart, got (%v, %d)", outcome.restart, outcome.code)
	}
	restartingMsgs := <-wsCountCh
	if restartingMsgs != 1 {
		t.Fatalf("expected ServerRestarting broadcast EXACTLY ONCE, got %d", restartingMsgs)
	}

	// Verify slog.Default() was reset BEFORE logBridge.Close() so post-instance reload log does not hit closed logBridge
	logDir := filepath.Join(dataDir, "logs")
	entriesBefore, _ := os.ReadDir(logDir)
	var sizeBefore int64
	if len(entriesBefore) > 0 {
		if fi, sErr := entriesBefore[0].Info(); sErr == nil {
			sizeBefore = fi.Size()
		}
	}
	slog.Info("reloading Ambra Media Server configuration and database...")
	if err := slog.Default().Handler().Handle(ctx, slog.NewRecord(time.Now(), slog.LevelInfo, "post-close check", 0)); err != nil {
		t.Fatalf("expected default slog handler after runServerInstance to succeed without closed FD error, got %v", err)
	}
	if len(entriesBefore) > 0 {
		if fi, sErr := os.Stat(filepath.Join(logDir, entriesBefore[0].Name())); sErr == nil && fi.Size() != sizeBefore {
			t.Fatalf("expected closed logBridge detached from slog.Default(), size changed %d -> %d", sizeBefore, fi.Size())
		}
	}
}
