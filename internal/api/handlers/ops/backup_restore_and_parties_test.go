package ops

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/netutil"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func buildTestZipV0110(t *testing.T, dst string, entries map[string][]byte) {
	t.Helper()
	f, err := os.Create(dst)
	if err != nil {
		t.Fatalf("create zip %s: %v", dst, err)
	}
	zw := zip.NewWriter(f)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create entry %s: %v", name, err)
		}
		if _, err := w.Write(content); err != nil {
			t.Fatalf("zip write entry %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}
}

func TestBackupCreateAndRestoreValidationV0110(t *testing.T) {
	h, mux, st, cfg, _ := setupOpsTest(t)

	// Populate config/ambra.json and metadata/users/u1/avatar.jpg (plus a symlink that must be skipped).
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save: %v", err)
	}
	userMetaDir := filepath.Join(cfg.DataDir, "metadata", "users", "u1")
	if err := os.MkdirAll(userMetaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	avatarPath := filepath.Join(userMetaDir, "primary_0.img")
	if err := os.WriteFile(avatarPath, []byte("fake-avatar-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.Symlink(avatarPath, filepath.Join(userMetaDir, "symlink.img"))

	// Create full backup archive containing ambra.db + config/ambra.json + metadata/users/...
	res := doReq(t, mux, http.MethodPost, "/BackupRestore/Backup", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("POST /BackupRestore/Backup expected 200, got %d %s", res.Code, res.Body.String())
	}
	var bk BackupFileInfo
	if err := json.Unmarshal(res.Body.Bytes(), &bk); err != nil {
		t.Fatal(err)
	}

	// Restore via exported HandleRestoreBackup using BackupName field.
	restoreBody, _ := json.Marshal(map[string]any{"BackupName": bk.Name})
	restoreReq := httptest.NewRequest(http.MethodPost, "/BackupRestore/Restore", bytes.NewReader(restoreBody))
	restoreRec := httptest.NewRecorder()
	h.HandleRestoreBackup(restoreRec, restoreReq)
	if restoreRec.Code != http.StatusOK {
		t.Fatalf("HandleRestoreBackup full zip expected 200, got %d %s", restoreRec.Code, restoreRec.Body.String())
	}
	for _, staged := range []string{
		filepath.Join(cfg.DataDir, "ambra.db.restore"),
		filepath.Join(cfg.DataDir, "config", "ambra.json.restore"),
		filepath.Join(cfg.DataDir, "metadata.restore", "users", "u1", "primary_0.img"),
	} {
		if _, err := os.Stat(staged); err != nil {
			t.Fatalf("expected staged file %s after zip restore: %v", staged, err)
		}
	}

	// Create a valid raw SQLite snapshot in backupsDir and restore non-zip .db backup.
	rawDBBackup := filepath.Join(h.backupsDir(), "manual-snapshot.db")
	escaped := strings.ReplaceAll(rawDBBackup, "'", "''")
	if _, err := st.Writer.Exec(fmt.Sprintf("VACUUM INTO '%s'", escaped)); err != nil {
		t.Fatalf("vacuum raw db: %v", err)
	}
	validSQLiteBytes, err := os.ReadFile(rawDBBackup)
	if err != nil {
		t.Fatal(err)
	}
	if r := doReq(t, mux, http.MethodPost, "/BackupRestore/Restore", map[string]any{"Name": "manual-snapshot.db"}); r.Code != http.StatusOK {
		t.Fatalf("restore raw sqlite db expected 200, got %d %s", r.Code, r.Body.String())
	}

	// BackupInfo sorts multiple backups by DateCreated desc and skips hidden/dirs.
	_ = os.MkdirAll(filepath.Join(h.backupsDir(), "subdir"), 0o700)
	_ = os.WriteFile(filepath.Join(h.backupsDir(), ".hidden"), []byte("x"), 0o600)
	if r := doReq(t, mux, http.MethodGet, "/BackupRestore/BackupInfo", nil); r.Code != http.StatusOK {
		t.Fatalf("BackupInfo failed: %d", r.Code)
	}

	// Directory target -> 404; symlink target -> 400.
	if r := doReq(t, mux, http.MethodPost, "/BackupRestore/Restore", map[string]any{"Name": "subdir"}); r.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when backup target is directory, got %d", r.Code)
	}
	symBackup := filepath.Join(h.backupsDir(), "symlink-backup.db")
	if err := os.Symlink(rawDBBackup, symBackup); err == nil {
		if r := doReq(t, mux, http.MethodPost, "/BackupRestore/Restore", map[string]any{"Name": "symlink-backup.db"}); r.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 when backup is symlink, got %d", r.Code)
		}
	}

	// Raw non-SQLite file and corrupted SQLite file -> 400.
	_ = os.WriteFile(filepath.Join(h.backupsDir(), "short.db"), []byte("tiny"), 0o600)
	_ = os.WriteFile(filepath.Join(h.backupsDir(), "badmagic.db"), []byte("NOT_SQLITE_HEADER_12345"), 0o600)
	corruptSQLite := append(append([]byte(nil), sqliteMagicHeader...), bytes.Repeat([]byte{0xFF}, 4096)...)
	_ = os.WriteFile(filepath.Join(h.backupsDir(), "corrupt.db"), corruptSQLite, 0o600)
	_ = os.WriteFile(filepath.Join(h.backupsDir(), "notazip.zip"), []byte("plain text with zip ext"), 0o600)

	for _, badName := range []string{"short.db", "badmagic.db", "corrupt.db", "notazip.zip"} {
		if r := doReq(t, mux, http.MethodPost, "/BackupRestore/Restore", map[string]any{"Name": badName}); r.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for %s, got %d %s", badName, r.Code, r.Body.String())
		}
	}

	// Zip validation negative cases.
	emptyZip := filepath.Join(h.backupsDir(), "empty.zip")
	buildTestZipV0110(t, emptyZip, map[string][]byte{})
	if r := doReq(t, mux, http.MethodPost, "/BackupRestore/Restore", map[string]any{"Name": "empty.zip"}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty.zip, got %d", r.Code)
	}

	zipCases := map[string]map[string][]byte{
		"abs.zip":          {"/etc/passwd": []byte("x")},
		"trav.zip":         {"config/../../evil": []byte("x")},
		"nul.zip":          {"bad\x00name": []byte("x")},
		"dot.zip":          {".": []byte("x")},
		"disallowed.zip":   {"ambra.db": validSQLiteBytes, "evil.sh": []byte("echo 1")},
		"missingdb.zip":    {"config/ambra.json": []byte(`{"ServerName":"ok"}`)},
		"badcfg.zip":       {"ambra.db": validSQLiteBytes, "config/ambra.json": []byte(`{bad-json`)},
		"badzipdbhdr.zip":  {"ambra.db": []byte("not-sqlite-at-all-1234567890")},
		"corruptzipdb.zip": {"ambra.db": corruptSQLite},
	}
	for zName, entries := range zipCases {
		buildTestZipV0110(t, filepath.Join(h.backupsDir(), zName), entries)
		if r := doReq(t, mux, http.MethodPost, "/BackupRestore/Restore", map[string]any{"Name": zName}); r.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for %s, got %d %s", zName, r.Code, r.Body.String())
		}
	}

	// Duplicate ambra.db entry in zip -> 400.
	dupZipPath := filepath.Join(h.backupsDir(), "dupdb.zip")
	zf, err := os.Create(dupZipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(zf)
	for i := 0; i < 2; i++ {
		w, _ := zw.Create("ambra.db")
		_, _ = w.Write(validSQLiteBytes)
	}
	_ = zw.Close()
	_ = zf.Close()
	if r := doReq(t, mux, http.MethodPost, "/BackupRestore/Restore", map[string]any{"Name": "dupdb.zip"}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for dupdb.zip, got %d", r.Code)
	}

	// writeFileIntoZip returns nil on directory.
	var memBuf bytes.Buffer
	memZW := zip.NewWriter(&memBuf)
	if err := writeFileIntoZip(memZW, "dir", cfg.DataDir); err != nil {
		t.Fatalf("expected nil on directory writeFileIntoZip, got %v", err)
	}
	_ = memZW.Close()
}

func TestWebhooksMigrationSanitizationAndSecretRulesV0110(t *testing.T) {
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

	// Seed legacy ops:state with webhooks in server_config while webhooks table is empty.
	legacyJSON := `{"webhooks":[{"Name":"LegacyHook","Url":"https://example.com/hook","Secret":"s1","Enabled":true}]}`
	if _, err := st.Writer.Exec(`INSERT INTO server_config (key, value_json, updated_at) VALUES (?, ?, ?)`,
		stateConfigKey, legacyJSON, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}

	h := New(st, cfg, sqlite.NewUserRepo(st), nil)
	t.Cleanup(func() { h.Close(); h.Close() }) // Verify idempotent Close()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	// Verify legacy webhook was migrated into SQLite webhooks table and sanitized on GET.
	res := doReq(t, mux, http.MethodGet, "/Webhooks", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("GET /Webhooks: %d", res.Code)
	}
	var list []WebhookConfig
	_ = json.Unmarshal(res.Body.Bytes(), &list)
	if len(list) != 1 || !list[0].HasSecret || list[0].Secret != "" || list[0].Id == "" {
		t.Fatalf("unexpected migrated webhooks: %+v", list)
	}

	// Unit test sanitizeWebhookPayloadData & stripSensitivePathsValue across types.
	if sanitizeWebhookPayloadData(nil) != nil {
		t.Fatal("expected nil payload to stay nil")
	}
	var nilParty *Party
	if sanitizeWebhookPayloadData(nilParty) != nil {
		t.Fatal("expected typed nil *Party to return nil")
	}
	pVal := Party{Id: "p1", InviteCode: "secret", Members: []string{"u1"}, Messages: []PartyMessage{{Id: "m1", Text: "hi"}}}
	sanP := sanitizeWebhookPayloadData(pVal).(Party)
	if sanP.InviteCode != "" {
		t.Fatal("expected InviteCode stripped from Party value")
	}
	sanPtr := sanitizeWebhookPayloadData(&pVal).(*Party)
	if sanPtr.InviteCode != "" {
		t.Fatal("expected InviteCode stripped from *Party")
	}
	strippedStrMap := stripSensitivePathsValue(map[string]string{
		"Path": "/secret/path",
		"Name": "Movie",
	}).(map[string]string)
	if _, has := strippedStrMap["Path"]; has || strippedStrMap["Name"] != "Movie" {
		t.Fatalf("unexpected map[string]string strip result: %#v", strippedStrMap)
	}
	nestedStruct := struct {
		Name  string         `json:"Name"`
		Path  string         `json:"Path"`
		Items []any          `json:"Items"`
		Extra map[string]any `json:"Extra"`
	}{
		Name:  "Item",
		Path:  "/mnt/media/a.mkv",
		Items: []any{map[string]any{"FilePath": "/secret/b.mkv", "Title": "Ep1"}},
		Extra: map[string]any{"ProgramDataPath": "/var/lib/ambra", "Safe": "yes"},
	}
	sanGeneric := sanitizeWebhookPayloadData(nestedStruct).(map[string]any)
	if _, has := sanGeneric["Path"]; has {
		t.Fatalf("expected Path stripped: %#v", sanGeneric)
	}

	// SetHTTPClient(nil) is a no-op; nil httpClient fallback in getHTTPClient works.
	h.SetHTTPClient(nil)
	h.mu.Lock()
	h.httpClient = nil
	h.mu.Unlock()
	if h.getHTTPClient() == nil {
		t.Fatal("expected getHTTPClient fallback to return non-nil client")
	}

	// Allow loopback test servers for secret retention & HTTP 502 test cases.
	h.SetHTTPClient(netutil.NewSafeHTTPClient(10*time.Second, true))
	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer failSrv.Close()

	// Create webhook with secret, then update without secret on same origin+path (retains secret),
	// then update with different path (drops secret), then ClearSecret=true.
	cRes := doReq(t, mux, http.MethodPost, "/Webhooks", map[string]any{
		"Url":     failSrv.URL + "/hook1",
		"Secret":  "keep-me",
		"Enabled": true,
	})
	if cRes.Code != http.StatusOK {
		t.Fatalf("create webhook: %d %s", cRes.Code, cRes.Body.String())
	}
	var created WebhookConfig
	_ = json.Unmarshal(cRes.Body.Bytes(), &created)

	// Same origin & path, empty Secret -> retains secret.
	u1 := doReq(t, mux, http.MethodPost, "/Webhooks", map[string]any{
		"Id":      created.Id,
		"Url":     failSrv.URL + "/hook1",
		"Enabled": true,
	})
	var afterSame WebhookConfig
	_ = json.Unmarshal(u1.Body.Bytes(), &afterSame)
	if !afterSame.HasSecret {
		t.Fatal("expected secret retained when URL origin and path are unchanged")
	}

	// Different path, empty Secret -> clears secret to prevent leaking to new endpoint.
	u2 := doReq(t, mux, http.MethodPost, "/Webhooks", map[string]any{
		"Id":      created.Id,
		"Url":     failSrv.URL + "/hook2",
		"Enabled": true,
	})
	var afterDiff WebhookConfig
	_ = json.Unmarshal(u2.Body.Bytes(), &afterDiff)
	if afterDiff.HasSecret {
		t.Fatal("expected secret cleared when URL path changed without new Secret")
	}

	// Re-set secret then ClearSecret=true.
	_ = doReq(t, mux, http.MethodPost, "/Webhooks", map[string]any{
		"Id":      created.Id,
		"Url":     failSrv.URL + "/hook2",
		"Secret":  "temp-secret",
		"Enabled": true,
	})
	u3 := doReq(t, mux, http.MethodPost, "/Webhooks", map[string]any{
		"Id":          created.Id,
		"Url":         failSrv.URL + "/hook2",
		"ClearSecret": true,
		"Enabled":     true,
	})
	var afterClear WebhookConfig
	_ = json.Unmarshal(u3.Body.Bytes(), &afterClear)
	if afterClear.HasSecret {
		t.Fatal("expected HasSecret=false after ClearSecret=true")
	}

	// Test webhook against HTTP 502 server -> 200 with Success=false, Error="HTTP 502".
	tRes := doReq(t, mux, http.MethodPost, "/Webhooks/Test", map[string]any{"Id": created.Id})
	if tRes.Code != http.StatusOK || !strings.Contains(tRes.Body.String(), `"HTTP 502"`) {
		t.Fatalf("expected HTTP 502 error in WebhookTestResult, got %d %s", tRes.Code, tRes.Body.String())
	}
}

func TestPartiesCurrentInfoMessagesBruteForceAndPurgeV0110(t *testing.T) {
	h, _, _, _, _ := setupOpsTest(t)
	authMW := middleware.NewAuth(nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
	})

	host := makeAuthCtx("host-u1", "HostAlice", false)
	member := makeAuthCtx("member-u2", "MemberBob", false)
	outsider := makeAuthCtx("outsider-u3", "OutsiderEve", false)

	// Exported wrappers when user is not in any party.
	infoReq := httptest.NewRequest(http.MethodGet, "/Parties/Info", nil)
	infoReq = infoReq.WithContext(middleware.WithAuthContext(infoReq.Context(), host))
	infoRec := httptest.NewRecorder()
	h.HandleGetCurrentPartyInfo(infoRec, infoReq)
	if infoRec.Code != http.StatusOK || !strings.Contains(infoRec.Body.String(), `"InParty":false`) {
		t.Fatalf("HandleGetCurrentPartyInfo (not in party): %d %s", infoRec.Code, infoRec.Body.String())
	}

	listMsgReq := httptest.NewRequest(http.MethodGet, "/Parties/Messages", nil)
	listMsgReq = listMsgReq.WithContext(middleware.WithAuthContext(listMsgReq.Context(), host))
	listMsgRec := httptest.NewRecorder()
	h.HandleListPartyMessages(listMsgRec, listMsgReq)
	var emptyMsgRes dto.QueryResult[PartyMessage]
	_ = json.Unmarshal(listMsgRec.Body.Bytes(), &emptyMsgRes)
	if listMsgRec.Code != http.StatusOK || emptyMsgRes.TotalRecordCount != 0 || len(emptyMsgRes.Items) != 0 {
		t.Fatalf("HandleListPartyMessages (not in party): %d %s", listMsgRec.Code, listMsgRec.Body.String())
	}

	postMsgReq := httptest.NewRequest(http.MethodPost, "/Parties/Messages", bytes.NewBufferString(`{"Text":"nobody"}`))
	postMsgReq = postMsgReq.WithContext(middleware.WithAuthContext(postMsgReq.Context(), host))
	postMsgRec := httptest.NewRecorder()
	h.HandlePostPartyMessage(postMsgRec, postMsgReq)
	if postMsgRec.Code != http.StatusNoContent {
		t.Fatalf("HandlePostPartyMessage (not in party) expected 204, got %d", postMsgRec.Code)
	}

	// Create validation errors: long Name, long InviteCode, long ItemId, negative PositionTicks.
	for _, bad := range []map[string]any{
		{"Name": strings.Repeat("x", maxPartyNameLen+1)},
		{"InviteCode": strings.Repeat("c", maxPartyInviteCodeLen+1)},
		{"ItemId": strings.Repeat("i", maxPartyItemIDLen+1)},
		{"PositionTicks": -5},
	} {
		if r := doAuthReq(t, mux, host, http.MethodPost, "/Parties", bad); r.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 on invalid create party %v, got %d", bad, r.Code)
		}
	}

	// Create private party with InviteCode.
	cRec := doAuthReq(t, mux, host, http.MethodPost, "/Parties", map[string]any{
		"Name":       "Movie Night",
		"InviteCode": "PASS-777",
		"ItemId":     "item-42",
	})
	if cRec.Code != http.StatusOK {
		t.Fatalf("create party failed: %d %s", cRec.Code, cRec.Body.String())
	}
	var party Party
	_ = json.Unmarshal(cRec.Body.Bytes(), &party)

	// Brute-force Join rate limit: 5 wrong invite codes -> 403, 6th -> 429.
	for i := 0; i < maxPartyJoinFails; i++ {
		r := doAuthReq(t, mux, outsider, http.MethodPost, "/Parties/"+party.Id+"/Join", map[string]any{"InviteCode": "WRONG"})
		if r.Code != http.StatusForbidden {
			t.Fatalf("attempt %d expected 403, got %d", i+1, r.Code)
		}
	}
	if r := doAuthReq(t, mux, outsider, http.MethodPost, "/Parties/"+party.Id+"/Join", map[string]any{"InviteCode": "WRONG"}); r.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after maxPartyJoinFails, got %d", r.Code)
	}

	// Brute-force missing party ID enumeration rate limit: 5 -> 404, 6th -> 429.
	for i := 0; i < maxPartyJoinFails; i++ {
		r := doAuthReq(t, mux, outsider, http.MethodPost, "/Parties/nonexistent-room/Join", map[string]any{})
		if r.Code != http.StatusNotFound {
			t.Fatalf("missing attempt %d expected 404, got %d", i+1, r.Code)
		}
	}
	if r := doAuthReq(t, mux, outsider, http.MethodPost, "/Parties/nonexistent-room/Join", map[string]any{}); r.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on missing party brute-force, got %d", r.Code)
	}

	// Member joins with valid InviteCode via query parameter.
	if r := doAuthReq(t, mux, member, http.MethodPost, "/Parties/"+party.Id+"/Join?InviteCode=PASS-777", nil); r.Code != http.StatusOK {
		t.Fatalf("member join failed: %d %s", r.Code, r.Body.String())
	}

	// Member GET /Parties/Info sees InParty=true with redacted InviteCode.
	mInfo := doAuthReq(t, mux, member, http.MethodGet, "/Parties/Info", nil)
	if mInfo.Code != http.StatusOK || !strings.Contains(mInfo.Body.String(), `"InParty":true`) || strings.Contains(mInfo.Body.String(), "PASS-777") {
		t.Fatalf("unexpected member /Parties/Info: %d %s", mInfo.Code, mInfo.Body.String())
	}

	// RBAC checks for outsider on Messages, Leave, Delete, and State.
	if r := doAuthReq(t, mux, outsider, http.MethodGet, "/Parties/Messages?PartyId="+party.Id, nil); r.Code != http.StatusForbidden {
		t.Fatalf("outsider list messages expected 403, got %d", r.Code)
	}
	if r := doAuthReq(t, mux, member, http.MethodGet, "/Parties/Messages?PartyId=missing", nil); r.Code != http.StatusNotFound {
		t.Fatalf("missing party list messages expected 404, got %d", r.Code)
	}
	if r := doAuthReq(t, mux, outsider, http.MethodPost, "/Parties/Messages", map[string]any{"PartyId": party.Id, "Text": "hack"}); r.Code != http.StatusForbidden {
		t.Fatalf("outsider post message expected 403, got %d", r.Code)
	}
	if r := doAuthReq(t, mux, member, http.MethodPost, "/Parties/Messages", map[string]any{"PartyId": "missing", "Text": "hi"}); r.Code != http.StatusNotFound {
		t.Fatalf("missing party post message expected 404, got %d", r.Code)
	}
	if r := doAuthReq(t, mux, outsider, http.MethodPost, "/Parties/"+party.Id+"/Leave", map[string]any{}); r.Code != http.StatusForbidden {
		t.Fatalf("outsider leave expected 403, got %d", r.Code)
	}
	if r := doAuthReq(t, mux, outsider, http.MethodDelete, "/Parties/"+party.Id, nil); r.Code != http.StatusForbidden {
		t.Fatalf("outsider delete expected 403, got %d", r.Code)
	}
	if r := doAuthReq(t, mux, outsider, http.MethodPost, "/Parties/"+party.Id+"/State", map[string]any{"IsPaused": true}); r.Code != http.StatusForbidden {
		t.Fatalf("outsider state update expected 403, got %d", r.Code)
	}

	// Oversized, malformed, blank, and >100 capped messages.
	oversizedReq := httptest.NewRequest(http.MethodPost, "/Parties/Messages", strings.NewReader(`{}`))
	oversizedReq.ContentLength = maxPartyMessageBytes + 10
	oversizedRec := httptest.NewRecorder()
	h.HandlePostPartyMessage(oversizedRec, oversizedReq)
	if oversizedRec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 on oversized message, got %d", oversizedRec.Code)
	}
	badMsgReq := httptest.NewRequest(http.MethodPost, "/Parties/Messages", strings.NewReader(`{bad`))
	badMsgRec := httptest.NewRecorder()
	h.HandlePostPartyMessage(badMsgRec, badMsgReq)
	if badMsgRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on malformed message JSON, got %d", badMsgRec.Code)
	}
	// Blank text -> 204 no-op.
	if r := doAuthReq(t, mux, member, http.MethodPost, "/Parties/Messages", map[string]any{"Message": "   "}); r.Code != http.StatusNoContent {
		t.Fatalf("blank message expected 204, got %d", r.Code)
	}
	// Send 102 messages to exercise ring-buffer cap at maxPartyMessagesPerRoom (100).
	for i := 0; i < maxPartyMessagesPerRoom+2; i++ {
		r := doAuthReq(t, mux, member, http.MethodPost, "/Parties/Messages", map[string]any{"Message": fmt.Sprintf("msg-%d", i)})
		if r.Code != http.StatusNoContent {
			t.Fatalf("post message %d failed: %d", i, r.Code)
		}
	}
	msgsRes := doAuthReq(t, mux, member, http.MethodGet, "/Parties/Messages", nil)
	var msgsQR dto.QueryResult[PartyMessage]
	_ = json.Unmarshal(msgsRes.Body.Bytes(), &msgsQR)
	if msgsQR.TotalRecordCount != maxPartyMessagesPerRoom || len(msgsQR.Items) != maxPartyMessagesPerRoom || msgsQR.Items[0].UserName != "MemberBob" {
		t.Fatalf("expected %d capped messages with UserName=MemberBob, got total=%d len=%d first=%+v", maxPartyMessagesPerRoom, msgsQR.TotalRecordCount, len(msgsQR.Items), msgsQR.Items[0])
	}

	// Host leaves via exported HandleLeaveCurrentParty -> ownership transfers to member!
	leaveReq := httptest.NewRequest(http.MethodPost, "/Parties/Leave", bytes.NewBufferString(`{"PartyId":"`+party.Id+`"}`))
	leaveReq = leaveReq.WithContext(middleware.WithAuthContext(leaveReq.Context(), host))
	leaveRec := httptest.NewRecorder()
	h.HandleLeaveCurrentParty(leaveRec, leaveReq)
	if leaveRec.Code != http.StatusNoContent {
		t.Fatalf("HandleLeaveCurrentParty host expected 204, got %d", leaveRec.Code)
	}
	// Member leaves last -> party removed.
	if r := doAuthReq(t, mux, member, http.MethodPost, "/Parties/Leave", map[string]any{}); r.Code != http.StatusNoContent {
		t.Fatalf("member leave expected 204, got %d", r.Code)
	}

	// PurgeUser: host handoff & empty party cleanup.
	p2Rec := doAuthReq(t, mux, host, http.MethodPost, "/Parties", map[string]any{"Name": "PurgeRoom"})
	var p2 Party
	_ = json.Unmarshal(p2Rec.Body.Bytes(), &p2)
	_ = doAuthReq(t, mux, member, http.MethodPost, "/Parties/"+p2.Id+"/Join", nil)

	h.PurgeUser(context.Background(), "")
	h.PurgeUser(context.Background(), "host-u1")
	getP2 := doAuthReq(t, mux, member, http.MethodGet, "/Parties/"+p2.Id, nil)
	if getP2.Code != http.StatusOK {
		t.Fatalf("expected party to survive host purge with remaining member, got %d", getP2.Code)
	}
	h.PurgeUser(context.Background(), "member-u2")
	if r := doAuthReq(t, mux, member, http.MethodGet, "/Parties/"+p2.Id, nil); r.Code != http.StatusNotFound {
		t.Fatalf("expected party deleted after last member purged, got %d", r.Code)
	}
}

func TestStartupIPv6RateLimitAndEdgeCasesV0110(t *testing.T) {
	h, mux, _, _, _ := setupOpsTest(t)

	// Empty IP & loopback always pass.
	now := time.Now().UTC()
	if !h.checkStartupIPRateLimit("", now) || !h.checkStartupIPRateLimit("127.0.0.1", now) {
		t.Fatal("expected empty and loopback IPs to bypass startup rate limit")
	}

	// Stale bucket cleanup + IPv6 /64 prefix aggregation across distinct interface IDs.
	h.startupRates["198.51.100.9"] = []time.Time{now.Add(-5 * time.Minute)}
	for i := 0; i < startupRateLimit; i++ {
		ip := fmt.Sprintf("2001:db8:abcd:1234::%x", i+1)
		if !h.checkStartupIPRateLimit(ip, now) {
			t.Fatalf("expected IPv6 request %d in /64 to pass", i)
		}
	}
	if h.checkStartupIPRateLimit("2001:db8:abcd:1234::ffff", now) {
		t.Fatal("expected 31st request from same IPv6 /64 subnet to be rate limited")
	}

	// Trigger HTTP 429 through requireSetupOrAdmin middleware.
	authMux := http.NewServeMux()
	h.RegisterRoutes(authMux, AuthFuncs{})
	req := httptest.NewRequest(http.MethodGet, "/Startup/Configuration", nil)
	req.RemoteAddr = "[2001:db8:abcd:1234::9999]:54321"
	rec := httptest.NewRecorder()
	authMux.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 from requireSetupOrAdmin, got %d", rec.Code)
	}

	// StartupComplete before creating any user -> 400; nil userRepo -> 400.
	if r := doReq(t, mux, http.MethodPost, "/Startup/Complete", nil); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when completing startup with 0 users, got %d", r.Code)
	}
	nilRepoH := New(nil, nil, nil, nil)
	nilMux := http.NewServeMux()
	nilRepoH.RegisterRoutes(nilMux, AuthFuncs{})
	if r := doReq(t, nilMux, http.MethodPost, "/Startup/Complete", nil); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when userRepo is nil, got %d", r.Code)
	}

	// StartupUser validation: control char, empty password, short password, create then update existing password.
	for _, badUser := range []map[string]any{
		{"Name": "bad\x00user", "Password": "secret"},
		{"Name": "admin", "Password": "   "},
		{"Name": "admin", "Password": "123"},
	} {
		if r := doReq(t, mux, http.MethodPost, "/Startup/User", badUser); r.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for Startup/User %v, got %d", badUser, r.Code)
		}
	}
	if r := doReq(t, mux, http.MethodPost, "/Startup/User", map[string]any{"Name": "setupadmin", "Password": "password1"}); r.Code != http.StatusNoContent {
		t.Fatalf("create setupadmin failed: %d", r.Code)
	}
	if r := doReq(t, mux, http.MethodPost, "/Startup/User", map[string]any{"Name": "setupadmin", "Password": "password2"}); r.Code != http.StatusNoContent {
		t.Fatalf("update existing setupadmin password failed: %d", r.Code)
	}
}

func TestBackupFileFallbackPackagesPartyStateAndClosedDBRollbacksV0110(t *testing.T) {
	// 1. Backup file-copy fallback when store == nil: nil store & nil cfg -> 500; missing ambra.db -> 500; valid ambra.db -> 200!
	noStoreNoCfgH := New(nil, nil, nil, nil)
	noMux := http.NewServeMux()
	noStoreNoCfgH.RegisterRoutes(noMux, AuthFuncs{})
	if r := doReq(t, noMux, http.MethodPost, "/BackupRestore/Backup", nil); r.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when both store and cfg are nil, got %d", r.Code)
	}

	fileOnlyDir := t.TempDir()
	fileOnlyCfg, err := config.Load(fileOnlyDir)
	if err != nil {
		t.Fatal(err)
	}
	fileOnlyH := New(nil, fileOnlyCfg, nil, nil)
	fileOnlyMux := http.NewServeMux()
	fileOnlyH.RegisterRoutes(fileOnlyMux, AuthFuncs{})
	if r := doReq(t, fileOnlyMux, http.MethodPost, "/BackupRestore/Backup", nil); r.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when ambra.db does not exist for file backup, got %d", r.Code)
	}
	seedStore, err := sqlite.Open(fileOnlyCfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	_ = seedStore.Close()
	if r := doReq(t, fileOnlyMux, http.MethodPost, "/BackupRestore/Backup", nil); r.Code != http.StatusOK {
		t.Fatalf("expected 200 on file-copy backup fallback, got %d %s", r.Code, r.Body.String())
	}

	// 2. Startup RemoteAccess/Config/Complete, Packages lifecycle, Party State validation, and closed-DB snapshot rollbacks.
	h, mux, st, _, _ := setupOpsTest(t)
	h.SetHTTPClient(netutil.NewSafeHTTPClient(5*time.Second, true))

	if r := doReq(t, mux, http.MethodPost, "/Startup/Configuration", map[string]any{
		"ServerName":                "OpsAmbra",
		"UICulture":                 "zh-CN",
		"MetadataCountryCode":       "CN",
		"PreferredMetadataLanguage": "zh-CN",
	}); r.Code != http.StatusNoContent {
		t.Fatalf("POST /Startup/Configuration failed: %d", r.Code)
	}
	if r := doReq(t, mux, http.MethodGet, "/Startup/Configuration", nil); r.Code != http.StatusOK || !strings.Contains(r.Body.String(), `"UICulture":"zh-CN"`) {
		t.Fatalf("GET /Startup/Configuration failed: %d %s", r.Code, r.Body.String())
	}
	if r := doReq(t, mux, http.MethodGet, "/Startup/User", nil); r.Code != http.StatusOK {
		t.Fatalf("GET /Startup/User failed: %d", r.Code)
	}
	if r := doReq(t, mux, http.MethodPost, "/Startup/RemoteAccess", map[string]any{
		"EnableRemoteAccess":         false,
		"EnableAutomaticPortMapping": true,
	}); r.Code != http.StatusNoContent {
		t.Fatalf("POST /Startup/RemoteAccess failed: %d", r.Code)
	}
	_ = doReq(t, mux, http.MethodPost, "/Startup/User", map[string]any{"Name": "wizadmin", "Password": "password1"})
	if r := doReq(t, mux, http.MethodPost, "/Startup/Complete", nil); r.Code != http.StatusNoContent {
		t.Fatalf("POST /Startup/Complete failed: %d", r.Code)
	}

	if r := doReq(t, mux, http.MethodGet, "/Packages?packagetype=Metadata", nil); r.Code != http.StatusOK {
		t.Fatalf("GET /Packages failed: %d", r.Code)
	}
	if r := doReq(t, mux, http.MethodGet, "/Packages/ambra-tmdb", nil); r.Code != http.StatusOK {
		t.Fatalf("GET /Packages/ambra-tmdb failed: %d", r.Code)
	}
	if r := doReq(t, mux, http.MethodGet, "/Packages/missing-pkg", nil); r.Code != http.StatusNotFound {
		t.Fatalf("GET /Packages/missing-pkg expected 404, got %d", r.Code)
	}
	if r := doReq(t, mux, http.MethodGet, "/Packages/Updates", nil); r.Code != http.StatusOK {
		t.Fatalf("GET /Packages/Updates failed: %d", r.Code)
	}
	instRes := doReq(t, mux, http.MethodPost, "/Packages/Installed/ambra-tmdb?version=1.2.3", nil)
	if instRes.Code != http.StatusOK {
		t.Fatalf("POST /Packages/Installed/ambra-tmdb failed: %d", instRes.Code)
	}
	var inst InstallationInfo
	_ = json.Unmarshal(instRes.Body.Bytes(), &inst)
	instRes2 := doReq(t, mux, http.MethodPost, "/Packages/Installed/ambra-webhooks", nil)
	var inst2 InstallationInfo
	_ = json.Unmarshal(instRes2.Body.Bytes(), &inst2)
	if r := doReq(t, mux, http.MethodDelete, "/Packages/Installing/"+inst.Id, nil); r.Code != http.StatusNoContent {
		t.Fatalf("DELETE /Packages/Installing/%s failed: %d", inst.Id, r.Code)
	}
	if r := doReq(t, mux, http.MethodDelete, "/Packages/Installing/missing-inst", nil); r.Code != http.StatusNotFound {
		t.Fatalf("DELETE missing installation expected 404, got %d", r.Code)
	}

	// Create a party and webhook before closing DB so rollback branches on state mutations are exercised.
	pRes := doReq(t, mux, http.MethodPost, "/Parties", map[string]any{"Name": "StateRoom", "HostUserId": "u-host"})
	var p Party
	_ = json.Unmarshal(pRes.Body.Bytes(), &p)
	_ = doReq(t, mux, http.MethodPost, "/Parties/"+p.Id+"/Join", map[string]any{"UserId": "u-mem"})

	for _, badState := range []map[string]any{
		{"ItemId": strings.Repeat("x", maxPartyItemIDLen+1)},
		{"PositionTicks": -10},
		{"PlaybackRate": 0.1},
		{"PlaybackRate": 9.0},
	} {
		if r := doReq(t, mux, http.MethodPost, "/Parties/"+p.Id+"/State", badState); r.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for party state %v, got %d", badState, r.Code)
		}
	}
	if r := doReq(t, mux, http.MethodPost, "/Parties/"+p.Id+"/State", map[string]any{"PositionTicks": 5000, "IsPaused": false, "PlaybackRate": 1.5}); r.Code != http.StatusOK {
		t.Fatalf("valid Seek state update failed: %d", r.Code)
	}
	if r := doReq(t, mux, http.MethodPost, "/Parties/"+p.Id+"/State", map[string]any{"IsPaused": true}); r.Code != http.StatusOK {
		t.Fatalf("valid Pause state update failed: %d", r.Code)
	}

	whRes := doReq(t, mux, http.MethodPost, "/Webhooks", map[string]any{"Url": "http://127.0.0.1:9999/rb", "Enabled": true})
	var wh WebhookConfig
	_ = json.Unmarshal(whRes.Body.Bytes(), &wh)

	// Close SQLite store -> all mutating endpoints must roll back in-memory state and return 500!
	_ = st.Close()
	if r := doReq(t, mux, http.MethodPost, "/Parties/Messages", map[string]any{"PartyId": p.Id, "UserId": "u-host", "Text": "fail"}); r.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on closed DB post message, got %d", r.Code)
	}
	if r := doReq(t, mux, http.MethodPost, "/Parties/Leave", map[string]any{"PartyId": p.Id, "UserId": "u-mem"}); r.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on closed DB leave current party, got %d", r.Code)
	}
	if r := doReq(t, mux, http.MethodPost, "/Parties/"+p.Id+"/Join", map[string]any{"UserId": "u-new"}); r.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on closed DB join party, got %d", r.Code)
	}
	if r := doReq(t, mux, http.MethodPost, "/Parties/"+p.Id+"/Leave", map[string]any{"UserId": "u-mem"}); r.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on closed DB leave party, got %d", r.Code)
	}
	if r := doReq(t, mux, http.MethodPost, "/Parties/"+p.Id+"/State", map[string]any{"IsPaused": false}); r.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on closed DB update state, got %d", r.Code)
	}
	if r := doReq(t, mux, http.MethodDelete, "/Parties/"+p.Id, nil); r.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on closed DB delete party, got %d", r.Code)
	}
	if r := doReq(t, mux, http.MethodPost, "/Packages/Installed/ambra-tmdb", nil); r.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on closed DB install package, got %d", r.Code)
	}
	if r := doReq(t, mux, http.MethodDelete, "/Packages/Installing/"+inst2.Id, nil); r.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on closed DB cancel install, got %d", r.Code)
	}
	if r := doReq(t, mux, http.MethodPost, "/Startup/User", map[string]any{"Name": "failuser", "Password": "password1"}); r.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on closed DB Startup/User, got %d", r.Code)
	}
	if r := doReq(t, mux, http.MethodPost, "/Startup/Complete", nil); r.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on closed DB Startup/Complete, got %d", r.Code)
	}
	if r := doReq(t, mux, http.MethodPost, "/Webhooks", map[string]any{"Url": "http://127.0.0.1:9999/rb2", "Enabled": true}); r.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on closed DB create webhook, got %d", r.Code)
	}
	if r := doReq(t, mux, http.MethodDelete, "/Webhooks/"+wh.Id, nil); r.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on closed DB delete webhook, got %d", r.Code)
	}
}
