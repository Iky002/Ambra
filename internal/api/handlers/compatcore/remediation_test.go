package compatcore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/session"
)

type tokenTrackingWSHub struct {
	fullMockWSHub
	disconnectedTokens []string
}

func (m *tokenTrackingWSHub) DisconnectToken(tok string) {
	m.mu.Lock()
	m.disconnectedTokens = append(m.disconnectedTokens, tok)
	m.mu.Unlock()
}

func TestCompatCoreAuditRemediationsFullSuite(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	authSvc := auth.NewService(env.userRepo, env.cfg)
	authMW := middleware.NewAuth(authSvc, env.sessions)
	authMW.SetConfig(env.cfg)
	env.handler.SetAuthService(authSvc)
	mockWS := &tokenTrackingWSHub{}
	env.handler.SetWSHub(mockWS)

	var deletedHookUsers []string
	var delMu sync.Mutex
	env.handler.SetOnUserDeleted(func(_ context.Context, uid string) {
		delMu.Lock()
		deletedHookUsers = append(deletedHookUsers, uid)
		delMu.Unlock()
	})

	adminAuth, err := authSvc.AuthenticateByName(ctx, "admin", "admin", "", auth.ClientInfo{DeviceID: "adm-dev", Client: "Web"})
	if err != nil {
		t.Fatal(err)
	}
	guestAuth, err := authSvc.AuthenticateByName(ctx, "9guest", "pass", "", auth.ClientInfo{DeviceID: "guest-dev", Client: "Web"})
	if err != nil {
		t.Fatal(err)
	}

	router := env.handler.BuildRouter(AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
		UserIDFromCtx: func(c context.Context) string {
			if ac, ok := middleware.AuthFromContext(c); ok && ac != nil {
				return ac.UserID()
			}
			return ""
		},
	})

	call := func(token, method, path, body, remoteAddr string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("X-Emby-Token", token)
		}
		if remoteAddr != "" {
			req.RemoteAddr = remoteAddr
		} else {
			req.RemoteAddr = "127.0.0.1:12345"
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	// 1. [C-01] Global remote access toggle blocks public IP AuthenticateUserByID even when user EnableRemoteAccess=true
	env.cfg.Update(func(c *config.Config) {
		b := false
		c.EnableRemoteAccessGlobal = &b
	})
	if r := call("", http.MethodPost, "/Users/"+env.user2.ID+"/Authenticate", `{"Pw":"pass"}`, "203.0.113.50:1234"); r.Code != http.StatusForbidden {
		t.Fatalf("C-01: expected 403 when global EnableRemoteAccess=false on public IP, got %d", r.Code)
	}
	env.cfg.Update(func(c *config.Config) {
		b := true
		c.EnableRemoteAccessGlobal = &b
	})

	// 2. [H-09] Admin RBAC convergence on Auth/Providers, Webhooks/Sample, Connect/Link
	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/Auth/Providers"},
		{http.MethodGet, "/Webhooks/Sample"},
		{http.MethodPost, "/Users/" + env.user2.ID + "/Connect/Link?ConnectUsername=u"},
		{http.MethodDelete, "/Users/" + env.user2.ID + "/Connect/Link"},
		{http.MethodPost, "/Users/" + env.user2.ID + "/Connect/Link/Delete"},
	} {
		if r := call(guestAuth.AccessToken, tc.method, tc.path, "", ""); r.Code != http.StatusForbidden {
			t.Fatalf("H-09: expected 403 for non-admin %s %s, got %d", tc.method, tc.path, r.Code)
		}
	}
	if r := call(adminAuth.AccessToken, http.MethodGet, "/Auth/Providers", "", ""); r.Code != http.StatusOK {
		t.Fatalf("expected 200 for admin GET /Auth/Providers, got %d", r.Code)
	}
	if r := call(adminAuth.AccessToken, http.MethodGet, "/Webhooks/Sample", "", ""); r.Code != http.StatusOK {
		t.Fatalf("expected 200 for admin GET /Webhooks/Sample, got %d", r.Code)
	}

	// 3. [H-08] DeleteAPIKey by KeyID invalidates auth cache and disconnects WS token
	apiKeyRec, err := authSvc.CreateAPIKey(ctx, "TestAutomation")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authSvc.ValidateToken(ctx, apiKeyRec.Token); err != nil {
		t.Fatal(err)
	}
	if r := call(adminAuth.AccessToken, http.MethodPost, "/Auth/Keys/"+apiKeyRec.KeyID+"/Delete", "", ""); r.Code != http.StatusNoContent {
		t.Fatalf("delete API key by KeyID failed: %d", r.Code)
	}
	if _, err := authSvc.ValidateToken(ctx, apiKeyRec.Token); err == nil {
		t.Fatal("H-08: expected API key invalidated in authSvc cache after Delete by KeyID")
	}
	mockWS.mu.Lock()
	if len(mockWS.disconnectedTokens) != 1 || mockWS.disconnectedTokens[0] != apiKeyRec.Token {
		t.Fatalf("H-08: expected DisconnectToken(%q), got %v", apiKeyRec.Token, mockWS.disconnectedTokens)
	}
	mockWS.mu.Unlock()

	// 4. [H-08] Policy reduction (EnableLiveTvAccess -> false) triggers wsHub.DisconnectUser
	reducedPol := env.user2.Policy
	reducedPol.EnableLiveTvAccess = false
	polBody, _ := json.Marshal(map[string]any{"Policy": reducedPol})
	if r := call(adminAuth.AccessToken, http.MethodPost, "/Users/"+env.user2.ID, string(polBody), ""); r.Code != http.StatusNoContent {
		t.Fatalf("update user policy failed: %d", r.Code)
	}
	mockWS.mu.Lock()
	foundWS := false
	for _, uid := range mockWS.disconnectedUsers {
		if uid == env.user2.ID {
			foundWS = true
		}
	}
	mockWS.mu.Unlock()
	if !foundWS {
		t.Fatal("H-08: expected wsHub.DisconnectUser on EnableLiveTvAccess reduction")
	}

	// 5. [M-01] Nonexistent session target: 403 for non-admin, 404 for admin, 413/400 for >64KiB payload
	if r := call(guestAuth.AccessToken, http.MethodPost, "/Sessions/nonexistent-sid/Playing/Pause", `{}`, ""); r.Code != http.StatusForbidden {
		t.Fatalf("M-01: expected 403 for non-admin on nonexistent session, got %d", r.Code)
	}
	if r := call(adminAuth.AccessToken, http.MethodPost, "/Sessions/nonexistent-sid/Playing/Pause", `{}`, ""); r.Code != http.StatusNotFound {
		t.Fatalf("M-01: expected 404 for admin on nonexistent session, got %d", r.Code)
	}
	admSess := env.sessions.TouchSession(env.admin, auth.ClientInfo{DeviceID: "adm-dev", Client: "Web"})
	bigCmd := `{"Data":"` + strings.Repeat("A", maxSessionCommandBodyBytes+512) + `"}`
	if r := call(adminAuth.AccessToken, http.MethodPost, "/Sessions/"+admSess.Id+"/Command", bigCmd, ""); r.Code != http.StatusRequestEntityTooLarge && r.Code != http.StatusBadRequest {
		t.Fatalf("M-01: expected 413/400 on >64KiB session command body, got %d", r.Code)
	}

	// 6. [M-06] Environment/DirectoryContents: reject relative path, detect symlink dir, cap at 2000 entries
	if r := call(adminAuth.AccessToken, http.MethodPost, "/Environment/DirectoryContents", `{"Path":"relative/sub"}`, ""); r.Code != http.StatusBadRequest {
		t.Fatalf("M-06: expected 400 for non-absolute Path, got %d", r.Code)
	}
	dirTest := t.TempDir()
	realSub := filepath.Join(t.TempDir(), "real-target-dir")
	_ = os.MkdirAll(realSub, 0o755)
	_ = os.Symlink(realSub, filepath.Join(dirTest, "0000-symlink-dir"))
	for i := 0; i < 2050; i++ {
		_ = os.WriteFile(filepath.Join(dirTest, fmt.Sprintf("f-%04d.txt", i)), []byte("x"), 0o644)
	}
	recDir := call(adminAuth.AccessToken, http.MethodPost, "/Environment/DirectoryContents",
		fmt.Sprintf(`{"Path":%q,"IncludeFiles":true,"IncludeDirectories":true}`, dirTest), "")
	if recDir.Code != http.StatusOK {
		t.Fatalf("DirectoryContents failed: %d %s", recDir.Code, recDir.Body.String())
	}
	var dirEntries []fileSystemEntry
	_ = json.Unmarshal(recDir.Body.Bytes(), &dirEntries)
	if len(dirEntries) != maxDirectoryContentsEntries {
		t.Fatalf("M-06: expected DirectoryContents capped at %d, got %d", maxDirectoryContentsEntries, len(dirEntries))
	}
	if dirEntries[0].Name != "0000-symlink-dir" || dirEntries[0].Type != "Directory" {
		t.Fatalf("M-06: expected symlink directory recognized as Directory, got %+v", dirEntries[0])
	}

	// 7. [Task 6.2] CameraUploads: reject empty stream, reject bad ext/traversal, persist real upload & isolate by user
	if r := call(guestAuth.AccessToken, http.MethodPost, "/Devices/CameraUploads?DeviceId=guest-dev&Name=empty.jpg", "", ""); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for 0-byte camera upload, got %d", r.Code)
	}
	if r := call(guestAuth.AccessToken, http.MethodPost, "/Devices/CameraUploads?DeviceId=guest-dev&Name=shell.php", "<?php", ""); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for disallowed camera upload extension, got %d", r.Code)
	}
	if r := call(guestAuth.AccessToken, http.MethodPost, "/Devices/CameraUploads?DeviceId=guest-dev&Name=../escape.jpg", "bytes", ""); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for traversal in camera upload Name, got %d", r.Code)
	}
	if r := call(guestAuth.AccessToken, http.MethodPost, "/Devices/CameraUploads?DeviceId=guest-dev&Name=photo.jpg&Album=../bad", "bytes", ""); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for traversal in camera upload Album, got %d", r.Code)
	}
	imgBytes := "\x89PNG\r\n\x1a\nreal-camera-photo"
	if r := call(guestAuth.AccessToken, http.MethodPost, "/Devices/CameraUploads?DeviceId=guest-dev&Id=cam-1&Name=vacation.png&Album=Summer2026", imgBytes, ""); r.Code != http.StatusNoContent {
		t.Fatalf("valid camera upload failed: %d %s", r.Code, r.Body.String())
	}
	getCam := call(guestAuth.AccessToken, http.MethodGet, "/Devices/CameraUploads?DeviceId=guest-dev", "", "")
	if getCam.Code != http.StatusOK || !strings.Contains(getCam.Body.String(), "vacation.png") {
		t.Fatalf("GET CameraUploads failed: %d %s", getCam.Code, getCam.Body.String())
	}
	// Admin querying same DeviceId under admin's own callerUID sees 0 uploads (strict per-user isolation!)
	getAdminCam := call(adminAuth.AccessToken, http.MethodGet, "/Devices/CameraUploads?DeviceId=guest-dev", "", "")
	if strings.Contains(getAdminCam.Body.String(), "vacation.png") {
		t.Fatalf("expected CameraUploads isolated by caller UserID, got %s", getAdminCam.Body.String())
	}

	// 8. [Task 6.5] Co-watching AdditionalUsers & real DB Likes/Dislikes persistence
	rtTicks := int64(1_000_000_000)
	movieRec, _ := env.libRepo.GetItemRecordByID(ctx, env.itemID)
	movieRec.RuntimeTicks = &rtTicks
	_ = env.libRepo.UpsertItem(ctx, movieRec)
	env.sessions.SetLibraryRepo(env.libRepo)

	if r := call(adminAuth.AccessToken, http.MethodPost, "/Sessions/"+admSess.Id+"/Users/"+env.user2.ID, "", ""); r.Code != http.StatusNoContent {
		t.Fatalf("add additional user failed: %d", r.Code)
	}
	_ = env.sessions.ReportPlaybackStart(ctx, env.admin, auth.ClientInfo{DeviceID: "adm-dev", Client: "Web"}, session.PlaybackStartRequest{ItemId: env.itemID})
	_ = env.sessions.ReportPlaybackStopped(ctx, env.admin, auth.ClientInfo{DeviceID: "adm-dev", Client: "Web"}, session.PlaybackStoppedRequest{
		ItemId:        env.itemID,
		PositionTicks: 950_000_000, // > 90% threshold -> marks played for host AND co-watcher!
	})
	coUD, err := env.userRepo.GetUserItemData(ctx, env.user2.ID, env.itemID)
	if err != nil || !coUD.Played {
		t.Fatalf("expected co-watcher marked Played after host finishes playback, got %+v err=%v", coUD, err)
	}

	// Real DB Likes/Dislikes
	if r := call(guestAuth.AccessToken, http.MethodPost, "/Users/"+env.user2.ID+"/Items/"+env.itemID+"/Rating?Likes=false", "", ""); r.Code != http.StatusOK {
		t.Fatalf("POST Rating Likes=false failed: %d", r.Code)
	}
	udDislike, err := env.userRepo.GetUserItemData(ctx, env.user2.ID, env.itemID)
	if err != nil || udDislike.Likes == nil || *udDislike.Likes != false {
		t.Fatalf("expected Likes=false persisted in user_item_data, got %+v err=%v", udDislike, err)
	}
	if r := call(guestAuth.AccessToken, http.MethodDelete, "/Users/"+env.user2.ID+"/Items/"+env.itemID+"/Rating", "", ""); r.Code != http.StatusOK {
		t.Fatalf("DELETE Rating failed: %d", r.Code)
	}
	udCleared, err := env.userRepo.GetUserItemData(ctx, env.user2.ID, env.itemID)
	if err != nil || udCleared.Likes != nil {
		t.Fatalf("expected Likes=nil in user_item_data after DELETE Rating, got %+v err=%v", udCleared, err)
	}

	// 9. [L-01 ~ L-03] Control-char username rejection, rename session UserName sync, hidden user avatar 404, delete user disk/hook cleanup
	if r := call(adminAuth.AccessToken, http.MethodPost, "/Users/"+env.user2.ID, `{"Name":"bad\u0007name"}`, ""); r.Code != http.StatusBadRequest {
		t.Fatalf("L-03: expected 400 for control char in username rename, got %d", r.Code)
	}
	guestSess := env.sessions.TouchSession(env.user2, auth.ClientInfo{DeviceID: "guest-dev", Client: "Web"})
	if r := call(adminAuth.AccessToken, http.MethodPost, "/Users/"+env.user2.ID, `{"Name":"renamed_guest"}`, ""); r.Code != http.StatusNoContent {
		t.Fatalf("rename user failed: %d", r.Code)
	}
	if updatedSess, ok := env.sessions.GetSession(guestSess.Id); !ok || updatedSess.UserName != "renamed_guest" {
		t.Fatalf("L-03: expected active session UserName updated to renamed_guest, got %+v", updatedSess)
	}

	// Upload avatar for user2, mark IsHidden=true -> anonymous GET returns 404 while authenticated GET returns 200
	pngB64 := base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\nhiddenavatar"))
	if r := call(adminAuth.AccessToken, http.MethodPost, "/Users/"+env.user2.ID+"/Images/Primary", pngB64, ""); r.Code != http.StatusNoContent {
		t.Fatalf("upload avatar failed: %d", r.Code)
	}
	hidPol := reducedPol
	hidPol.IsHidden = true
	hidBody, _ := json.Marshal(map[string]any{"Policy": hidPol})
	_ = call(adminAuth.AccessToken, http.MethodPost, "/Users/"+env.user2.ID, string(hidBody), "")

	// Build an anonymous-capable router (UserIDFromCtx returns "" when unauthenticated)
	if rAnon := call("", http.MethodGet, "/Users/"+env.user2.ID+"/Images/Primary", "", ""); rAnon.Code != http.StatusNotFound {
		t.Fatalf("L-03: expected 404 on anonymous GET avatar of IsHidden user, got %d", rAnon.Code)
	}
	if rAdminImg := call(adminAuth.AccessToken, http.MethodGet, "/Users/"+env.user2.ID+"/Images/Primary", "", ""); rAdminImg.Code != http.StatusOK {
		t.Fatalf("expected 200 on admin GET avatar of IsHidden user, got %d", rAdminImg.Code)
	}

	// Delete user2 -> removes metadata/users/<uid> & camera-uploads/<uid> and fires SetOnUserDeleted
	avatarDir := filepath.Dir(env.handler.userImagePath(env.user2.ID, "Primary", "0"))
	camUserDir := filepath.Join(env.handler.cameraUploadsRootDir(), sanitizeFilename(env.user2.ID))
	if rDel := call(adminAuth.AccessToken, http.MethodPost, "/Users/"+env.user2.ID+"/Delete", "", ""); rDel.Code != http.StatusNoContent {
		t.Fatalf("delete user2 failed: %d", rDel.Code)
	}
	if _, err := os.Stat(avatarDir); !os.IsNotExist(err) {
		t.Fatalf("L-01: expected metadata/users/<uid> directory removed on user delete, err=%v", err)
	}
	if _, err := os.Stat(camUserDir); !os.IsNotExist(err) {
		t.Fatalf("L-01: expected camera-uploads/<uid> directory removed on user delete, err=%v", err)
	}
	delMu.Lock()
	hookCalled := len(deletedHookUsers) == 1 && deletedHookUsers[0] == env.user2.ID
	delMu.Unlock()
	if !hookCalled {
		t.Fatalf("L-01: expected SetOnUserDeleted invoked for %s, got %v", env.user2.ID, deletedHookUsers)
	}
}
