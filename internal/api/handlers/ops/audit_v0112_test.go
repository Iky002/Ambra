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
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestH01M01M03M04M05And31_OpsHardeningAndAlignment(t *testing.T) {
	dir := t.TempDir()
	st, err := sqlite.Open(filepath.Join(dir, "ops_audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	ctx := context.Background()
	cfg := config.Default(dir)
	cfg.FFmpegPath = "/usr/bin/safe-ffmpeg"
	cfg.FFprobePath = "/usr/bin/safe-ffprobe"
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	userRepo := sqlite.NewUserRepo(st)
	bus := eventbus.New()
	authSvc := auth.NewService(userRepo, cfg)
	sessMgr := session.NewManager(userRepo, cfg, bus)
	authMW := middleware.NewAuth(authSvc, sessMgr)

	h := New(st, cfg, userRepo, bus)
	t.Cleanup(h.Close)
	h.SetAuthDeps(authSvc, sessMgr)
	h.SetItemAccessChecker(func(_ context.Context, user *sqlite.UserRecord, itemID string) bool {
		if user == nil {
			return false
		}
		if strings.HasPrefix(itemID, "adult-") && user.Name == "kid" {
			return false
		}
		return true
	})

	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
		UserIDFromCtx: func(c context.Context) string {
			if ac, ok := middleware.AuthFromContext(c); ok && ac != nil {
				return ac.UserID()
			}
			return ""
		},
	})

	// 1. H-01: Cross-origin browser POST to /Startup/* MUST be rejected with 403!
	for _, ep := range []string{"/Startup/Configuration", "/Startup/User", "/Startup/RemoteAccess", "/Startup/Complete"} {
		req := httptest.NewRequest(http.MethodPost, ep, bytes.NewBufferString(`{"Name":"evil","Password":"pw1234"}`))
		req.Host = "ambra.local:8096"
		req.RemoteAddr = "127.0.0.1:40000"
		req.Header.Set("Origin", "https://malicious.example")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for cross-origin %s, got %d %s", ep, rec.Code, rec.Body.String())
		}
	}

	// 2. M-05: POST /Startup/User updating existing user's password revokes existing tokens, invalidates cache, and removes sessions!
	adminUser, err := authSvc.CreateUser(ctx, "admin", "oldpass", true)
	if err != nil {
		t.Fatal(err)
	}
	adminLogin1, err := authSvc.AuthenticateByName(ctx, "admin", "oldpass", "", auth.ClientInfo{DeviceID: "d-old"})
	if err != nil {
		t.Fatal(err)
	}
	sessMgr.TouchSession(adminUser, auth.ClientInfo{DeviceID: "d-old"})
	// Second token used by admin to call POST /Startup/User
	adminLogin2, err := authSvc.AuthenticateByName(ctx, "admin", "oldpass", "", auth.ClientInfo{DeviceID: "d-caller"})
	if err != nil {
		t.Fatal(err)
	}

	reqUpdatePw := httptest.NewRequest(http.MethodPost, "/Startup/User", bytes.NewBufferString(`{"Name":"admin","Password":"newpassword99"}`))
	reqUpdatePw.Host = "ambra.local:8096"
	reqUpdatePw.RemoteAddr = "127.0.0.1:40000"
	reqUpdatePw.Header.Set("Origin", "http://ambra.local:8096")
	reqUpdatePw.Header.Set("X-Emby-Token", adminLogin2.AccessToken)
	recUpdatePw := httptest.NewRecorder()
	mux.ServeHTTP(recUpdatePw, reqUpdatePw)
	if recUpdatePw.Code != http.StatusNoContent {
		t.Fatalf("POST /Startup/User expected 204, got %d %s", recUpdatePw.Code, recUpdatePw.Body.String())
	}
	// Old cached token MUST now be rejected immediately!
	if _, err := authSvc.ValidateToken(ctx, adminLogin1.AccessToken); err == nil {
		t.Fatal("M-05 violated: old token remained valid in cache/DB after POST /Startup/User password change")
	}
	if len(sessMgr.ListSessions()) != 0 {
		t.Fatalf("M-05 violated: expected active sessions cleared, got %d", len(sessMgr.ListSessions()))
	}

	// Re-login admin & create adult + kid users for M-04 Watch Party tests
	adminAuth, err := authSvc.AuthenticateByName(ctx, "admin", "newpassword99", "", auth.ClientInfo{DeviceID: "d-adm"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authSvc.CreateUser(ctx, "adult", "adultpw", false); err != nil {
		t.Fatal(err)
	}
	adultAuth, err := authSvc.AuthenticateByName(ctx, "adult", "adultpw", "", auth.ClientInfo{DeviceID: "d-adult"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authSvc.CreateUser(ctx, "kid", "kidpw", false); err != nil {
		t.Fatal(err)
	}
	kidAuth, err := authSvc.AuthenticateByName(ctx, "kid", "kidpw", "", auth.ClientInfo{DeviceID: "d-kid"})
	if err != nil {
		t.Fatal(err)
	}

	callParty := func(tok, method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.RemoteAddr = "127.0.0.1:40001"
		req.Header.Set("X-Emby-Token", tok)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// 3. M-04: Kid cannot create party with restricted ItemId (403); Adult can create it;
	// Kid does NOT see adult party in GET /Parties, gets 403 on GET /Parties/{Id} and POST /Parties/{Id}/Join,
	// and cannot set restricted ItemId via POST /Parties/{Id}/State on a safe party!
	if rec := callParty(kidAuth.AccessToken, http.MethodPost, "/Parties", `{"Name":"Sneaky","ItemId":"adult-movie-1"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when kid creates party with restricted item, got %d", rec.Code)
	}
	recAdultCreate := callParty(adultAuth.AccessToken, http.MethodPost, "/Parties", `{"Name":"Adult Room","ItemId":"adult-movie-1","IsPublic":true}`)
	if recAdultCreate.Code != http.StatusOK {
		t.Fatalf("adult create party failed: %d %s", recAdultCreate.Code, recAdultCreate.Body.String())
	}
	var adultParty Party
	_ = json.Unmarshal(recAdultCreate.Body.Bytes(), &adultParty)

	recKidList := callParty(kidAuth.AccessToken, http.MethodGet, "/Parties", "")
	if strings.Contains(recKidList.Body.String(), adultParty.Id) || strings.Contains(recKidList.Body.String(), "adult-movie-1") {
		t.Fatalf("GET /Parties leaked restricted public party to kid: %s", recKidList.Body.String())
	}
	if rec := callParty(kidAuth.AccessToken, http.MethodGet, "/Parties/"+adultParty.Id, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on GET /Parties/{Id} for restricted item, got %d", rec.Code)
	}
	if rec := callParty(kidAuth.AccessToken, http.MethodPost, "/Parties/"+adultParty.Id+"/Join", `{}`); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on POST /Parties/{Id}/Join for restricted item, got %d", rec.Code)
	}

	recSafeCreate := callParty(adultAuth.AccessToken, http.MethodPost, "/Parties", `{"Name":"Family Room","ItemId":"kids-safe-1","IsPublic":true}`)
	var safeParty Party
	_ = json.Unmarshal(recSafeCreate.Body.Bytes(), &safeParty)
	if rec := callParty(kidAuth.AccessToken, http.MethodPost, "/Parties/"+safeParty.Id+"/Join", `{}`); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 when kid joins safe party, got %d", rec.Code)
	}
	if rec := callParty(kidAuth.AccessToken, http.MethodPost, "/Parties/"+safeParty.Id+"/State", `{"ItemId":"adult-movie-1"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when kid switches party state to restricted item, got %d", rec.Code)
	}

	// 4. 3.1-10: GET /Parties/Messages returns QueryResult envelope with StartIndex/Limit pagination!
	_ = callParty(adultAuth.AccessToken, http.MethodPost, "/Parties/Messages", `{"PartyId":"`+safeParty.Id+`","Text":"m1"}`)
	_ = callParty(kidAuth.AccessToken, http.MethodPost, "/Parties/Messages", `{"PartyId":"`+safeParty.Id+`","Text":"m2"}`)
	recMsgs := callParty(kidAuth.AccessToken, http.MethodGet, "/Parties/Messages?PartyId="+safeParty.Id+"&StartIndex=1&Limit=1", "")
	var msgQR dto.QueryResult[PartyMessage]
	if err := json.Unmarshal(recMsgs.Body.Bytes(), &msgQR); err != nil {
		t.Fatal(err)
	}
	if msgQR.TotalRecordCount != 2 || len(msgQR.Items) != 1 || msgQR.Items[0].Text != "m2" {
		t.Fatalf("unexpected paginated PartyMessages QueryResult: %+v", msgQR)
	}

	// 5. 3.1-2: Install package then CancelPackageInstall removes it and returns false on second call!
	recInst := callParty(adminAuth.AccessToken, http.MethodPost, "/Packages/Installed/ambra-tmdb", "")
	if recInst.Code != http.StatusOK {
		t.Fatalf("install package failed: %d", recInst.Code)
	}
	var instRes struct {
		Id string `json:"Id"`
	}
	_ = json.Unmarshal(recInst.Body.Bytes(), &instRes)
	if !h.CancelPackageInstall(instRes.Id) {
		t.Fatal("expected CancelPackageInstall=true for installed task")
	}
	if h.CancelPackageInstall(instRes.Id) {
		t.Fatal("expected CancelPackageInstall=false after already cancelled")
	}

	// 6. M-03: Restore backup zip containing malicious ffmpeg_path/ffprobe_path strips them and preserves host paths!
	srcDBBytes, err := os.ReadFile(filepath.Join(dir, "ops_audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	backupsDir := filepath.Join(dir, "config", "backups")
	if err := os.MkdirAll(backupsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	evilZip := filepath.Join(backupsDir, "evil-backup.zip")
	buildTestZipV0110(t, evilZip, map[string][]byte{
		"ambra.db": srcDBBytes,
		"config/ambra.json": []byte(`{
			"server_name": "Restored Server",
			"ffmpeg_path": "/tmp/evil-rce.sh",
			"ffprobe_path": "/tmp/evil-probe.sh"
		}`),
	})
	recRestore := callParty(adminAuth.AccessToken, http.MethodPost, "/BackupRestore/Restore", `{"Name":"evil-backup.zip"}`)
	if recRestore.Code != http.StatusOK {
		t.Fatalf("restore failed: %d %s", recRestore.Code, recRestore.Body.String())
	}
	stagedCfgRaw, err := os.ReadFile(filepath.Join(dir, "config", "ambra.json.restore"))
	if err != nil {
		t.Fatalf("expected staged ambra.json.restore: %v", err)
	}
	var stagedCfg map[string]any
	if err := json.Unmarshal(stagedCfgRaw, &stagedCfg); err != nil {
		t.Fatal(err)
	}
	if stagedCfg["ffmpeg_path"] != "/usr/bin/safe-ffmpeg" || stagedCfg["ffprobe_path"] != "/usr/bin/safe-ffprobe" {
		t.Fatalf("M-03 violated in backup restore: got %#v", stagedCfg)
	}

	// 7. M-01: checkStartupIPRateLimit enforces startupLoopbackRateLimit on 127.0.0.1!
	now := time.Now().UTC()
	hRate := New(nil, config.Default(t.TempDir()), nil, nil)
	for i := 0; i < startupLoopbackRateLimit; i++ {
		if !hRate.checkStartupIPRateLimit("127.0.0.1", now) {
			t.Fatalf("expected loopback startup request %d allowed", i+1)
		}
	}
	if hRate.checkStartupIPRateLimit("127.0.0.1", now) {
		t.Fatal("expected loopback startup request exceeding startupLoopbackRateLimit to be blocked")
	}
}
