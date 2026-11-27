package compatcore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/api/ws"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

type spyWSHubV0110 struct {
	mu                 sync.Mutex
	sent               []ws.Message
	broadcasts         []ws.Message
	disconnectedUsers  []string
	disconnectedDevs   []string
	disconnectedTokens []string
}

func (s *spyWSHubV0110) SendToUserDevice(_, _ string, msg ws.Message) {
	s.mu.Lock()
	s.sent = append(s.sent, msg)
	s.mu.Unlock()
}

func (s *spyWSHubV0110) Broadcast(msg ws.Message) {
	s.mu.Lock()
	s.broadcasts = append(s.broadcasts, msg)
	s.mu.Unlock()
}

func (s *spyWSHubV0110) DisconnectUser(uid string) {
	s.mu.Lock()
	s.disconnectedUsers = append(s.disconnectedUsers, uid)
	s.mu.Unlock()
}

func (s *spyWSHubV0110) DisconnectDevice(devID string) {
	s.mu.Lock()
	s.disconnectedDevs = append(s.disconnectedDevs, devID)
	s.mu.Unlock()
}

func (s *spyWSHubV0110) DisconnectUserDevice(uid, devID string) {
	s.mu.Lock()
	s.disconnectedDevs = append(s.disconnectedDevs, uid+":"+devID)
	s.mu.Unlock()
}

func (s *spyWSHubV0110) DisconnectToken(tok string) {
	s.mu.Lock()
	s.disconnectedTokens = append(s.disconnectedTokens, tok)
	s.mu.Unlock()
}

func doAuthCtxReqV0110(t *testing.T, router http.Handler, ac *middleware.AuthContext, method, target string, body []byte, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, target, bytes.NewReader(body))
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.RemoteAddr = "127.0.0.1:12345"
	if ac != nil {
		req = req.WithContext(middleware.WithAuthContext(req.Context(), ac))
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestCameraUploadsDeduplicationCleanupAndValidationV0110(t *testing.T) {
	// Unit-test segment & filename sanitizers across edge cases.
	for _, badSeg := range []string{"", ".", "..", "a/b", `a\b`, "bad\x00seg", "...", strings.Repeat("a", 20)} {
		if _, ok := sanitizeCameraSegment(badSeg, 10); ok {
			t.Fatalf("expected sanitizeCameraSegment(%q) to fail", badSeg)
		}
	}
	if got, ok := sanitizeCameraSegment("My Vacation!@#2026", 64); !ok || !strings.HasPrefix(got, "My Vacation___2026") {
		t.Fatalf("unexpected sanitizeCameraSegment result: %q, %v", got, ok)
	}

	for _, badFile := range []string{"", "../img.jpg", "sub/img.jpg", "bad\x07.jpg", "script.exe", "...jpg"} {
		if _, _, err := validateCameraUploadFilename(badFile); err == nil {
			t.Fatalf("expected validateCameraUploadFilename(%q) to fail", badFile)
		}
	}
	if safe, ext, err := validateCameraUploadFilename("  IMG 001?.JPG "); err != nil || safe != "IMG 001_.jpg" || ext != ".jpg" {
		t.Fatalf("unexpected validateCameraUploadFilename: safe=%q ext=%q err=%v", safe, ext, err)
	}

	env := newTestEnv(t)
	ctx := context.Background()
	authSvc := auth.NewService(env.userRepo, env.cfg)
	env.handler.SetAuthService(authSvc)
	wsSpy := &spyWSHubV0110{}
	env.handler.SetWSHub(wsSpy)

	// Register devices for admin and user2.
	if err := env.userRepo.CreateAccessToken(ctx, &sqlite.AccessTokenRecord{
		Token: "tok-admin-cam", UserID: env.admin.ID, DeviceID: "dev-admin", DeviceName: "AdminPhone", AppName: "Emby", AppVersion: "1.0",
	}); err != nil {
		t.Fatal(err)
	}
	if err := env.userRepo.CreateAccessToken(ctx, &sqlite.AccessTokenRecord{
		Token: "tok-user2-cam", UserID: env.user2.ID, DeviceID: "dev-user2", DeviceName: "GuestPhone", AppName: "Emby", AppVersion: "1.0",
	}); err != nil {
		t.Fatal(err)
	}

	user2AC := &middleware.AuthContext{
		User:   env.user2,
		Token:  "tok-user2-cam",
		Client: auth.ClientInfo{DeviceID: "dev-user2"},
	}
	adminAC := &middleware.AuthContext{
		User:   env.admin,
		Token:  "tok-admin-cam",
		Client: auth.ClientInfo{DeviceID: "dev-admin"},
	}

	// Validation & RBAC failures on CameraUploads.
	if r := doAuthCtxReqV0110(t, env.router, user2AC, http.MethodGet, "/Devices/CameraUploads", nil, ""); r.Code != http.StatusBadRequest {
		t.Fatalf("missing DeviceId GET expected 400, got %d", r.Code)
	}
	if r := doAuthCtxReqV0110(t, env.router, user2AC, http.MethodGet, "/Devices/CameraUploads?DeviceId=dev-admin", nil, ""); r.Code != http.StatusForbidden {
		t.Fatalf("cross-user DeviceId GET expected 403, got %d", r.Code)
	}
	if r := doAuthCtxReqV0110(t, env.router, user2AC, http.MethodPost, "/Devices/CameraUploads", []byte("img"), "image/jpeg"); r.Code != http.StatusBadRequest {
		t.Fatalf("missing DeviceId POST expected 400, got %d", r.Code)
	}
	if r := doAuthCtxReqV0110(t, env.router, user2AC, http.MethodPost, "/Devices/CameraUploads?DeviceId=dev-admin&Name=a.jpg", []byte("img"), "image/jpeg"); r.Code != http.StatusForbidden {
		t.Fatalf("cross-user DeviceId POST expected 403, got %d", r.Code)
	}
	if r := doAuthCtxReqV0110(t, env.router, user2AC, http.MethodPost, "/Devices/CameraUploads?DeviceId=dev-user2&Name=bad.exe", []byte("img"), ""); r.Code != http.StatusBadRequest {
		t.Fatalf("bad ext POST expected 400, got %d", r.Code)
	}
	if r := doAuthCtxReqV0110(t, env.router, user2AC, http.MethodPost, "/Devices/CameraUploads?DeviceId=dev-user2&Name=a.jpg&Album=..", []byte("img"), ""); r.Code != http.StatusBadRequest {
		t.Fatalf("bad Album POST expected 400, got %d", r.Code)
	}
	if r := doAuthCtxReqV0110(t, env.router, user2AC, http.MethodPost, "/Devices/CameraUploads?DeviceId=dev-user2&Name=a.jpg&Id=bad%00id", []byte("img"), ""); r.Code != http.StatusBadRequest {
		t.Fatalf("control char Id POST expected 400, got %d", r.Code)
	}
	if r := doAuthCtxReqV0110(t, env.router, user2AC, http.MethodPost, "/Devices/CameraUploads?DeviceId=dev-user2&Name=a.jpg", []byte{}, "image/jpeg"); r.Code != http.StatusBadRequest {
		t.Fatalf("empty upload body expected 400, got %d", r.Code)
	}
	bigReq := httptest.NewRequest(http.MethodPost, "/Devices/CameraUploads?DeviceId=dev-user2&Name=a.jpg", strings.NewReader("x"))
	bigReq.ContentLength = maxCameraUploadBytes + 1
	bigReq = bigReq.WithContext(middleware.WithAuthContext(bigReq.Context(), user2AC))
	bigRec := httptest.NewRecorder()
	env.router.ServeHTTP(bigRec, bigReq)
	if bigRec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized ContentLength expected 413, got %d", bigRec.Code)
	}

	// Stream upload + deduplication by FileID + MIME auto-detection when Content-Type is application/json.
	payload1 := []byte("first-camera-photo-data")
	payload2 := []byte("updated-camera-photo-data-longer")
	if r := doAuthCtxReqV0110(t, env.router, user2AC, http.MethodPost, "/Devices/CameraUploads?DeviceId=dev-user2&Name=IMG_100.jpg&Album=Summer&Id=photo-100", payload1, "application/json"); r.Code != http.StatusNoContent {
		t.Fatalf("upload 1 failed: %d %s", r.Code, r.Body.String())
	}
	if r := doAuthCtxReqV0110(t, env.router, user2AC, http.MethodPost, "/Devices/CameraUploads?DeviceId=dev-user2&Name=IMG_100.jpg&Album=Summer&Id=photo-100", payload2, ""); r.Code != http.StatusNoContent {
		t.Fatalf("upload 2 (dedupe upsert) failed: %d %s", r.Code, r.Body.String())
	}

	listRes := doAuthCtxReqV0110(t, env.router, user2AC, http.MethodGet, "/Devices/CameraUploads?DeviceId=dev-user2", nil, "")
	if listRes.Code != http.StatusOK {
		t.Fatalf("GET CameraUploads failed: %d %s", listRes.Code, listRes.Body.String())
	}
	var listBody struct {
		DeviceId      string           `json:"DeviceId"`
		FilesUploaded []map[string]any `json:"FilesUploaded"`
	}
	if err := json.Unmarshal(listRes.Body.Bytes(), &listBody); err != nil {
		t.Fatal(err)
	}
	if len(listBody.FilesUploaded) != 1 || listBody.FilesUploaded[0]["Id"] != "photo-100" || int64(listBody.FilesUploaded[0]["Size"].(float64)) != int64(len(payload2)) {
		t.Fatalf("expected 1 deduplicated camera upload with updated size, got %+v", listBody)
	}

	// Non-admin postDevicesDelete direct handler call (disconnects own device), then admin POST /Devices/Delete cleans device artifacts!
	selfDelReq := httptest.NewRequest(http.MethodPost, "/Devices/Delete?Id=dev-user2", nil)
	selfDelReq = selfDelReq.WithContext(middleware.WithAuthContext(selfDelReq.Context(), user2AC))
	selfDelRec := httptest.NewRecorder()
	env.handler.postDevicesDelete(selfDelRec, selfDelReq)
	if selfDelRec.Code != http.StatusNoContent {
		t.Fatalf("non-admin own postDevicesDelete expected 204, got %d", selfDelRec.Code)
	}

	// Admin deletes device -> purges camera_uploads table rows for dev-user2.
	if r := doAuthCtxReqV0110(t, env.router, adminAC, http.MethodPost, "/Devices/Delete", []byte(`{"Id":"dev-user2"}`), "application/json"); r.Code != http.StatusNoContent {
		t.Fatalf("admin POST /Devices/Delete expected 204, got %d", r.Code)
	}
	afterDelRes := doAuthCtxReqV0110(t, env.router, adminAC, http.MethodGet, "/Devices/CameraUploads?DeviceId=dev-user2", nil, "")
	_ = json.Unmarshal(afterDelRes.Body.Bytes(), &listBody)
	if len(listBody.FilesUploaded) != 0 {
		t.Fatalf("expected camera uploads purged after device delete, got %+v", listBody.FilesUploaded)
	}

	// Standalone in-memory KV fallback (when userRepo == nil and cfg == nil).
	memUID := guid.New()
	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Join(os.TempDir(), "ambra-camera-uploads", sanitizeFilename(memUID)))
	})
	memH := New(nil, nil, nil, nil, nil, nil)
	memRouter := memH.BuildRouter(AuthFuncs{
		UserIDFromCtx: func(context.Context) string { return memUID },
	})
	memAC := &middleware.AuthContext{
		User:   &sqlite.UserRecord{ID: memUID, Name: "mem", IsAdmin: true, Policy: sqlite.DefaultUserPolicy(true)},
		Client: auth.ClientInfo{DeviceID: "mem-dev"},
	}
	if r := doAuthCtxReqV0110(t, memRouter, memAC, http.MethodPost, "/Devices/CameraUploads?DeviceId=mem-dev&Name=clip.dng", []byte("raw-dng"), ""); r.Code != http.StatusNoContent {
		t.Fatalf("mem KV camera upload POST failed: %d %s", r.Code, r.Body.String())
	}
	if r := doAuthCtxReqV0110(t, memRouter, memAC, http.MethodGet, "/Devices/CameraUploads?DeviceId=mem-dev", nil, ""); r.Code != http.StatusOK || !strings.Contains(r.Body.String(), "clip.dng") {
		t.Fatalf("mem KV camera upload GET failed: %d %s", r.Code, r.Body.String())
	}
}

func TestForgotPasswordPinExpiryRateLimitsAndPruningV0110(t *testing.T) {
	env := newTestEnv(t)
	authSvc := auth.NewService(env.userRepo, env.cfg)
	env.handler.SetAuthService(authSvc)

	// Pre-create 12 passwordreset-*.txt files to exercise prunePasswordResetFiles keeping 10.
	for i := 0; i < 12; i++ {
		f := filepath.Join(env.cfg.DataDir, fmt.Sprintf("passwordreset-old%02d.txt", i))
		_ = os.WriteFile(f, []byte("old"), 0o600)
	}
	prunePasswordResetFiles("", 10)
	prunePasswordResetFiles(env.cfg.DataDir, 10)

	// Non-LAN public IP requests must be rejected with 403 on both ForgotPassword and ForgotPassword/Pin.
	for _, ep := range []string{"/Users/ForgotPassword", "/Users/ForgotPassword/Pin"} {
		pubReq := httptest.NewRequest(http.MethodPost, ep, strings.NewReader(`{}`))
		pubReq.RemoteAddr = "198.51.100.42:55555"
		pubRec := httptest.NewRecorder()
		env.router.ServeHTTP(pubRec, pubReq)
		if pubRec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for public IP on %s, got %d", ep, pubRec.Code)
		}
	}

	// Pre-fill forgotPins to max capacity (256) + 1 expired entry to test expiry cleanup and oldest eviction.
	staleFile := filepath.Join(env.cfg.DataDir, "passwordreset-expired.txt")
	_ = os.WriteFile(staleFile, []byte("expired"), 0o600)
	evictFile := filepath.Join(env.cfg.DataDir, "passwordreset-evict.txt")
	_ = os.WriteFile(evictFile, []byte("evict"), 0o600)

	now := time.Now().UTC()
	env.handler.mu.Lock()
	env.handler.forgotPins["000000"] = forgotPinEntry{
		UserID: "expired-u", Pin: "000000", PinFile: staleFile, ExpiresAt: now.Add(-time.Minute),
	}
	for i := 1; i <= forgotPinsMaxEntries; i++ {
		k := fmt.Sprintf("%06d", i)
		pf := ""
		exp := now.Add(20 * time.Minute)
		if i == 1 {
			pf = evictFile
			exp = now.Add(1 * time.Minute) // oldest non-expired entry
		}
		env.handler.forgotPins[k] = forgotPinEntry{
			UserID: fmt.Sprintf("u-%d", i), Pin: k, PinFile: pf, ExpiresAt: exp,
		}
	}
	env.handler.mu.Unlock()

	// Trigger ForgotPassword for user2 on LAN IP 192.168.1.50.
	fpReq := httptest.NewRequest(http.MethodPost, "/Users/ForgotPassword", strings.NewReader(`{"EnteredUsername":"`+env.user2.Name+`"}`))
	fpReq.Header.Set("Content-Type", "application/json")
	fpReq.RemoteAddr = "192.168.1.50:12345"
	fpRec := httptest.NewRecorder()
	env.router.ServeHTTP(fpRec, fpReq)
	if fpRec.Code != http.StatusOK {
		t.Fatalf("ForgotPassword failed: %d %s", fpRec.Code, fpRec.Body.String())
	}
	if _, err := os.Stat(staleFile); !os.IsNotExist(err) {
		t.Fatal("expected expired PIN file to be removed")
	}
	if _, err := os.Stat(evictFile); !os.IsNotExist(err) {
		t.Fatal("expected oldest PIN file to be evicted when forgotPinsMaxEntries reached")
	}

	// Extract generated PIN for user2 and redeem it via /Users/ForgotPassword/Pin.
	var user2Pin string
	env.handler.mu.RLock()
	for k, e := range env.handler.forgotPins {
		if e.UserID == env.user2.ID {
			user2Pin = k
			break
		}
	}
	// Also inject an expired PIN entry right before redemption to cover expired pruning inside forgotPasswordPin.
	env.handler.mu.RUnlock()
	env.handler.mu.Lock()
	env.handler.forgotPins["999999"] = forgotPinEntry{
		UserID: env.admin.ID, Pin: "999999", ExpiresAt: time.Now().UTC().Add(-time.Second),
	}
	env.handler.mu.Unlock()

	if user2Pin == "" {
		t.Fatal("expected PIN entry generated for user2")
	}

	// Expired PIN "999999" must not succeed.
	expPinReq := httptest.NewRequest(http.MethodPost, "/Users/ForgotPassword/Pin", strings.NewReader(`{"Pin":"999999"}`))
	expPinReq.RemoteAddr = "192.168.1.51:12345"
	expPinRec := httptest.NewRecorder()
	env.router.ServeHTTP(expPinRec, expPinReq)
	if expPinRec.Code != http.StatusOK || !strings.Contains(expPinRec.Body.String(), `"Success":false`) {
		t.Fatalf("expired PIN expected Success=false, got %d %s", expPinRec.Code, expPinRec.Body.String())
	}

	// Valid PIN resets password and returns Success=true.
	okPinReq := httptest.NewRequest(http.MethodPost, "/Users/ForgotPassword/Pin", strings.NewReader(`{"Pin":"`+user2Pin+`"}`))
	okPinReq.RemoteAddr = "192.168.1.51:12345"
	okPinRec := httptest.NewRecorder()
	env.router.ServeHTTP(okPinRec, okPinReq)
	if okPinRec.Code != http.StatusOK || !strings.Contains(okPinRec.Body.String(), `"Success":true`) {
		t.Fatalf("valid PIN expected Success=true, got %d %s", okPinRec.Code, okPinRec.Body.String())
	}

	// Rate limit exhaustion on ForgotPassword (5 per IP) and ForgotPassword/Pin (5 per IP).
	for i := 0; i < forgotPasswordRateLimit; i++ {
		rq := httptest.NewRequest(http.MethodPost, "/Users/ForgotPassword", strings.NewReader(`{"EnteredUsername":"nobody"}`))
		rq.RemoteAddr = "192.168.1.99:12345"
		rc := httptest.NewRecorder()
		env.router.ServeHTTP(rc, rq)
		if rc.Code != http.StatusOK {
			t.Fatalf("ForgotPassword attempt %d got %d", i+1, rc.Code)
		}
	}
	limReq := httptest.NewRequest(http.MethodPost, "/Users/ForgotPassword", strings.NewReader(`{"EnteredUsername":"nobody"}`))
	limReq.RemoteAddr = "192.168.1.99:12345"
	limRec := httptest.NewRecorder()
	env.router.ServeHTTP(limRec, limReq)
	if limRec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on ForgotPassword rate limit, got %d", limRec.Code)
	}

	for i := 0; i < forgotPinMaxAttempts; i++ {
		rq := httptest.NewRequest(http.MethodPost, "/Users/ForgotPassword/Pin", strings.NewReader(`{"Pin":"111111"}`))
		rq.RemoteAddr = "192.168.1.98:12345"
		rc := httptest.NewRecorder()
		env.router.ServeHTTP(rc, rq)
		if rc.Code != http.StatusOK {
			t.Fatalf("ForgotPassword/Pin attempt %d got %d", i+1, rc.Code)
		}
	}
	limPinReq := httptest.NewRequest(http.MethodPost, "/Users/ForgotPassword/Pin", strings.NewReader(`{"Pin":"111111"}`))
	limPinReq.RemoteAddr = "192.168.1.98:12345"
	limPinRec := httptest.NewRecorder()
	env.router.ServeHTTP(limPinRec, limPinReq)
	if limPinRec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on ForgotPassword/Pin rate limit, got %d", limPinRec.Code)
	}
}

func TestUserPolicyRatingsDelegationAndSessionsV0110(t *testing.T) {
	// Unit-test policy helper functions directly for all branches.
	if hasRemovedFolder(nil, []string{"f1"}) {
		t.Fatal("expected false when oldFolders is empty")
	}
	if !hasRemovedFolder([]string{"f1", "f2"}, []string{"f1"}) {
		t.Fatal("expected true when f2 was removed")
	}
	if hasRemovedFolder([]string{"f1", ""}, []string{"f1"}) {
		t.Fatal("expected false when no folder was removed")
	}
	basePol := sqlite.DefaultUserPolicy(false)
	noRemote := basePol
	noRemote.EnableRemoteAccess = false
	if !policyRequiresWSDisconnect(basePol, false, noRemote) {
		t.Fatal("expected disconnect when EnableRemoteAccess disabled")
	}
	noLiveTV := basePol
	noLiveTV.EnableLiveTvAccess = false
	if !policyRequiresWSDisconnect(basePol, false, noLiveTV) {
		t.Fatal("expected disconnect when EnableLiveTvAccess disabled")
	}
	restrictedFolders := basePol
	restrictedFolders.EnableAllFolders = false
	restrictedFolders.EnabledFolders = []string{"lib-a", "lib-b"}
	if !policyRequiresWSDisconnect(basePol, false, restrictedFolders) {
		t.Fatal("expected disconnect when EnableAllFolders switched off")
	}
	fewerFolders := restrictedFolders
	fewerFolders.EnabledFolders = []string{"lib-a"}
	if !policyRequiresWSDisconnect(restrictedFolders, false, fewerFolders) {
		t.Fatal("expected disconnect when a folder is removed from EnabledFolders")
	}

	env := newTestEnv(t)
	ctx := context.Background()
	authSvc := auth.NewService(env.userRepo, env.cfg)
	env.handler.SetAuthService(authSvc)
	wsSpy := &spyWSHubV0110{}
	env.handler.SetWSHub(wsSpy)

	adminAC := &middleware.AuthContext{User: env.admin, Token: "adm-tok", Client: auth.ClientInfo{DeviceID: "d-adm"}}
	user2AC := &middleware.AuthContext{User: env.user2, Token: "u2-tok", Client: auth.ClientInfo{DeviceID: "d-u2"}}

	// Delegated Restore & Party handlers (SetRestoreHandler & SetPartyHandlers).
	var restoreCalled, leaveCalled, msgCalled bool
	env.handler.SetRestoreHandler(func(w http.ResponseWriter, r *http.Request) {
		restoreCalled = true
		w.WriteHeader(http.StatusAccepted)
	})
	env.handler.SetPartyHandlers(
		func(w http.ResponseWriter, r *http.Request) {
			leaveCalled = true
			w.WriteHeader(http.StatusNoContent)
		},
		func(w http.ResponseWriter, r *http.Request) {
			msgCalled = true
			w.WriteHeader(http.StatusNoContent)
		},
	)
	if r := doAuthCtxReqV0110(t, env.router, adminAC, http.MethodPost, "/BackupRestore/RestoreData", []byte(`{"Name":"b.zip"}`), "application/json"); r.Code != http.StatusAccepted || !restoreCalled {
		t.Fatalf("delegated RestoreData failed: code=%d called=%v", r.Code, restoreCalled)
	}
	if r := doAuthCtxReqV0110(t, env.router, user2AC, http.MethodPost, "/Parties/Leave", nil, ""); r.Code != http.StatusNoContent || !leaveCalled {
		t.Fatalf("delegated Parties/Leave failed: code=%d called=%v", r.Code, leaveCalled)
	}
	if r := doAuthCtxReqV0110(t, env.router, user2AC, http.MethodPost, "/Parties/Messages", []byte(`{"Text":"hi"}`), "application/json"); r.Code != http.StatusNoContent || !msgCalled {
		t.Fatalf("delegated Parties/Messages failed: code=%d called=%v", r.Code, msgCalled)
	}

	// Clear delegated restore handler to cover standalone fallback with non-empty backup Name -> 404.
	env.handler.SetRestoreHandler(nil)
	if r := doAuthCtxReqV0110(t, env.router, adminAC, http.MethodPost, "/BackupRestore/RestoreData", []byte(`{"Path":"/tmp/missing.zip"}`), "application/json"); r.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when restoreHandler nil and Path provided, got %d", r.Code)
	}

	// User rating (Likes=true, Likes=false, missing item 404, DELETE rating, missing item DELETE 404).
	if r := doAuthCtxReqV0110(t, env.router, user2AC, http.MethodPost, "/Users/"+env.user2.ID+"/Items/"+env.itemID+"/Rating?Likes=true", nil, ""); r.Code != http.StatusOK || !strings.Contains(r.Body.String(), `"Likes":true`) {
		t.Fatalf("POST Rating Likes=true failed: %d %s", r.Code, r.Body.String())
	}
	if r := doAuthCtxReqV0110(t, env.router, user2AC, http.MethodPost, "/Users/"+env.user2.ID+"/Items/"+env.itemID+"/Rating?Likes=false", nil, ""); r.Code != http.StatusOK || !strings.Contains(r.Body.String(), `"Likes":false`) {
		t.Fatalf("POST Rating Likes=false failed: %d %s", r.Code, r.Body.String())
	}
	if r := doAuthCtxReqV0110(t, env.router, adminAC, http.MethodPost, "/Users/"+env.admin.ID+"/Items/"+guid.New()+"/Rating?Likes=true", nil, ""); r.Code != http.StatusNotFound {
		t.Fatalf("POST Rating missing item expected 404, got %d", r.Code)
	}
	if r := doAuthCtxReqV0110(t, env.router, user2AC, http.MethodDelete, "/Users/"+env.user2.ID+"/Items/"+env.itemID+"/Rating", nil, ""); r.Code != http.StatusOK {
		t.Fatalf("DELETE Rating failed: %d %s", r.Code, r.Body.String())
	}
	if r := doAuthCtxReqV0110(t, env.router, adminAC, http.MethodDelete, "/Users/"+env.admin.ID+"/Items/"+guid.New()+"/Rating", nil, ""); r.Code != http.StatusNotFound {
		t.Fatalf("DELETE Rating missing item expected 404, got %d", r.Code)
	}

	// Rating fallback when userRepo == nil (covers userItemDataWithLikes).
	memUID := guid.New()
	memH := New(nil, nil, nil, nil, nil, nil)
	memRouter := memH.BuildRouter(AuthFuncs{UserIDFromCtx: func(context.Context) string { return memUID }})
	if r := doReq(t, memRouter, http.MethodPost, "/Users/"+memUID+"/Items/"+env.itemID+"/Rating?Likes=true", ""); r.Code != http.StatusOK || !strings.Contains(r.Body.String(), `"Likes":true`) {
		t.Fatalf("mem KV POST Rating failed: %d %s", r.Code, r.Body.String())
	}
	if r := doReq(t, memRouter, http.MethodDelete, "/Users/"+memUID+"/Items/"+env.itemID+"/Rating", ""); r.Code != http.StatusOK {
		t.Fatalf("mem KV DELETE Rating failed: %d %s", r.Code, r.Body.String())
	}

	// updateUser: non-admin cannot change Policy or rename; admin duplicate rename -> 400; admin policy update triggers WS disconnect.
	polBody, _ := json.Marshal(map[string]any{"Policy": noRemote})
	if r := doAuthCtxReqV0110(t, env.router, user2AC, http.MethodPost, "/Users/"+env.user2.ID, polBody, "application/json"); r.Code != http.StatusForbidden {
		t.Fatalf("non-admin Policy update expected 403, got %d", r.Code)
	}
	if r := doAuthCtxReqV0110(t, env.router, user2AC, http.MethodPost, "/Users/"+env.user2.ID, []byte(`{"Name":"renamed"}`), "application/json"); r.Code != http.StatusForbidden {
		t.Fatalf("non-admin rename expected 403, got %d", r.Code)
	}
	if r := doAuthCtxReqV0110(t, env.router, adminAC, http.MethodPost, "/Users/"+env.user2.ID, []byte(`{"Name":"`+env.admin.Name+`"}`), "application/json"); r.Code != http.StatusBadRequest {
		t.Fatalf("duplicate rename expected 400, got %d", r.Code)
	}
	if r := doAuthCtxReqV0110(t, env.router, adminAC, http.MethodPost, "/Users/"+env.user2.ID, polBody, "application/json"); r.Code != http.StatusNoContent {
		t.Fatalf("admin Policy update expected 204, got %d %s", r.Code, r.Body.String())
	}

	// updateUserConfigurationPartial: invalid type -> 400, valid merge -> 204.
	if r := doAuthCtxReqV0110(t, env.router, user2AC, http.MethodPost, "/Users/"+env.user2.ID+"/Configuration/Partial", []byte(`{"PlayDefaultAudioTrack":"not-a-bool"}`), "application/json"); r.Code != http.StatusBadRequest {
		t.Fatalf("invalid partial config type expected 400, got %d", r.Code)
	}
	if r := doAuthCtxReqV0110(t, env.router, user2AC, http.MethodPost, "/Users/"+env.user2.ID+"/Configuration/Partial", []byte(`{"SubtitleMode":"Always"}`), "application/json"); r.Code != http.StatusNoContent {
		t.Fatalf("valid partial config expected 204, got %d", r.Code)
	}

	// copyUserData with UserPolicy option: copying non-admin policy onto sole admin -> 400 ErrLastAdmin; copying config+policy onto user2 -> 204.
	if r := doAuthCtxReqV0110(t, env.router, adminAC, http.MethodPost, "/Users/"+env.user2.ID+"/CopyData", []byte(`{"ToUserIds":["`+env.admin.ID+`"],"CopyOptions":["UserPolicy"]}`), "application/json"); r.Code != http.StatusBadRequest {
		t.Fatalf("copying non-admin policy onto last admin expected 400, got %d", r.Code)
	}

	// Connect Link + Exchange + Delete Connect Link (using admin so both AuthContext and UserIDFromCtx resolve to admin.ID).
	if r := doAuthCtxReqV0110(t, env.router, adminAC, http.MethodPost, "/Users/"+env.admin.ID+"/Connect/Link", []byte(`{"ConnectUsername":"emby_admin"}`), "application/json"); r.Code != http.StatusOK {
		t.Fatalf("Connect/Link failed: %d", r.Code)
	}
	if r := doAuthCtxReqV0110(t, env.router, adminAC, http.MethodGet, "/Connect/Exchange?ConnectUserId=emby_admin", nil, ""); r.Code != http.StatusOK {
		t.Fatalf("Connect/Exchange failed: %d %s", r.Code, r.Body.String())
	}
	if r := doAuthCtxReqV0110(t, env.router, adminAC, http.MethodDelete, "/Users/"+env.admin.ID+"/Connect/Link", nil, ""); r.Code != http.StatusNoContent {
		t.Fatalf("DELETE Connect/Link failed: %d", r.Code)
	}

	// QueryUsers filters/sorting, UserPrefixes (including non-letter '#'), TrackSelections, HomeSections, UserSettings, TypedSettings.
	numUser := &sqlite.UserRecord{Name: "99Bot", Policy: sqlite.DefaultUserPolicy(false)}
	_ = env.userRepo.CreateUser(ctx, numUser)
	for _, qURL := range []string{
		"/Users/Query?NameStartsWith=a&SortBy=LastActivityDate&SortOrder=Descending&StartIndex=-1&Limit=1",
		"/Users/Query?NameStartsWithOrGreater=b&SortBy=LastLoginDate&SortOrder=Ascending",
		"/Users/Query?SearchTerm=bot&IsHidden=false&IsDisabled=false&IsAdministrator=false&SortBy=IsAdministrator,Name",
		"/Users/Prefix",
	} {
		if r := doAuthCtxReqV0110(t, env.router, adminAC, http.MethodGet, qURL, nil, ""); r.Code != http.StatusOK {
			t.Fatalf("GET %s failed: %d %s", qURL, r.Code, r.Body.String())
		}
	}
	for _, tt := range []string{"audio", "subtitle", "all"} {
		if r := doAuthCtxReqV0110(t, env.router, adminAC, http.MethodDelete, "/Users/"+env.admin.ID+"/TrackSelections/"+tt, nil, ""); r.Code != http.StatusNoContent {
			t.Fatalf("DELETE TrackSelections/%s failed: %d", tt, r.Code)
		}
	}
	if r := doAuthCtxReqV0110(t, env.router, adminAC, http.MethodGet, "/Users/"+env.admin.ID+"/HomeSections", nil, ""); r.Code != http.StatusOK {
		t.Fatalf("GET HomeSections failed: %d", r.Code)
	}
	if r := doAuthCtxReqV0110(t, env.router, adminAC, http.MethodPost, "/Users/"+env.admin.ID+"/HomeSections", []byte(`{"Id":"sec-custom","Name":"Custom","SectionType":"Custom"}`), "application/json"); r.Code != http.StatusOK {
		t.Fatalf("POST HomeSections failed: %d", r.Code)
	}
	if r := doAuthCtxReqV0110(t, env.router, adminAC, http.MethodPost, "/Users/"+env.admin.ID+"/HomeSections/Move?Id=sec-custom&NewIndex=0", nil, ""); r.Code != http.StatusNoContent {
		t.Fatalf("Move HomeSections failed: %d", r.Code)
	}
	if r := doAuthCtxReqV0110(t, env.router, adminAC, http.MethodPost, "/Users/"+env.admin.ID+"/HomeSections/Delete?Ids=sec-custom", nil, ""); r.Code != http.StatusNoContent {
		t.Fatalf("POST HomeSections/Delete failed: %d", r.Code)
	}
	if r := doAuthCtxReqV0110(t, env.router, adminAC, http.MethodPost, "/UserSettings/"+env.admin.ID, []byte(`{"Theme":"dark"}`), "application/json"); r.Code != http.StatusNoContent {
		t.Fatalf("POST UserSettings failed: %d", r.Code)
	}
	if r := doAuthCtxReqV0110(t, env.router, adminAC, http.MethodPost, "/UserSettings/"+env.admin.ID+"/Partial", []byte(`{"Accent":"blue"}`), "application/json"); r.Code != http.StatusNoContent {
		t.Fatalf("POST UserSettings/Partial failed: %d", r.Code)
	}
	if r := doAuthCtxReqV0110(t, env.router, adminAC, http.MethodGet, "/UserSettings/"+env.admin.ID, nil, ""); r.Code != http.StatusOK {
		t.Fatalf("GET UserSettings failed: %d", r.Code)
	}
	if r := doAuthCtxReqV0110(t, env.router, adminAC, http.MethodPost, "/Users/"+env.admin.ID+"/TypedSettings/playback", []byte(`{"Speed":1.25}`), "application/json"); r.Code != http.StatusNoContent {
		t.Fatalf("POST TypedSettings failed: %d", r.Code)
	}
	if r := doAuthCtxReqV0110(t, env.router, adminAC, http.MethodGet, "/Users/"+env.admin.ID+"/TypedSettings/playback", nil, ""); r.Code != http.StatusOK {
		t.Fatalf("GET TypedSettings failed: %d", r.Code)
	}

	// API Key delete via KeyID triggers wsHub.DisconnectToken; System Restart/Shutdown trigger Broadcast + callbacks.
	apiKey := &sqlite.APIKeyRecord{KeyID: "kid-1", Token: "raw-api-tok-1", AppName: "TestScript"}
	if err := env.userRepo.CreateAPIKey(ctx, apiKey); err != nil {
		t.Fatal(err)
	}
	if r := doAuthCtxReqV0110(t, env.router, adminAC, http.MethodPost, "/Auth/Keys/kid-1/Delete", nil, ""); r.Code != http.StatusNoContent {
		t.Fatalf("deleteAuthKey by KeyID failed: %d", r.Code)
	}
	rstCh := make(chan struct{}, 1)
	shutCh := make(chan struct{}, 1)
	env.handler.SetLifecycleHooks(func() { rstCh <- struct{}{} }, func() { shutCh <- struct{}{} })
	if r := doAuthCtxReqV0110(t, env.router, adminAC, http.MethodPost, "/System/Restart", nil, ""); r.Code != http.StatusNoContent {
		t.Fatalf("POST /System/Restart failed: %d", r.Code)
	}
	if r := doAuthCtxReqV0110(t, env.router, adminAC, http.MethodPost, "/System/Shutdown", nil, ""); r.Code != http.StatusNoContent {
		t.Fatalf("POST /System/Shutdown failed: %d", r.Code)
	}
	<-rstCh
	<-shutCh

	// Session AddUser (valid, missing user 404, disabled user 403) + remote control WS pushes & negative error branches.
	sess := env.sessions.TouchSession(env.admin, auth.ClientInfo{DeviceID: "d-sess", Device: "TV", Client: "AmbraTV", Version: "1.0"})
	if r := doAuthCtxReqV0110(t, env.router, adminAC, http.MethodPost, "/Sessions/"+sess.Id+"/Users/"+env.user2.ID, nil, ""); r.Code != http.StatusNoContent {
		t.Fatalf("postSessionAddUser valid failed: %d %s", r.Code, r.Body.String())
	}
	if r := doAuthCtxReqV0110(t, env.router, adminAC, http.MethodPost, "/Sessions/"+sess.Id+"/Users/"+guid.New(), nil, ""); r.Code != http.StatusNotFound {
		t.Fatalf("postSessionAddUser missing user expected 404, got %d", r.Code)
	}
	disPol := env.user2.Policy
	disPol.IsDisabled = true
	_ = env.userRepo.UpdateUserPolicy(ctx, env.user2.ID, disPol)
	if r := doAuthCtxReqV0110(t, env.router, adminAC, http.MethodPost, "/Sessions/"+sess.Id+"/Users/"+env.user2.ID, nil, ""); r.Code != http.StatusForbidden {
		t.Fatalf("postSessionAddUser disabled user expected 403, got %d", r.Code)
	}
	if r := doAuthCtxReqV0110(t, env.router, adminAC, http.MethodPost, "/Sessions/"+sess.Id+"/Playing?ItemIds="+env.itemID+"&PlayCommand=PlayNow", nil, ""); r.Code != http.StatusNoContent {
		t.Fatalf("postSessionPlaying failed: %d", r.Code)
	}
	if r := doAuthCtxReqV0110(t, env.router, adminAC, http.MethodPost, "/Sessions/"+sess.Id+"/Playing/Seek?SeekPositionTicks=50000", nil, ""); r.Code != http.StatusNoContent {
		t.Fatalf("postSessionPlayingCommand failed: %d", r.Code)
	}
	if r := doAuthCtxReqV0110(t, env.router, adminAC, http.MethodPost, "/Sessions/"+sess.Id+"/System/GoHome", nil, ""); r.Code != http.StatusNoContent {
		t.Fatalf("postSessionSystemCommand failed: %d", r.Code)
	}
	if r := doAuthCtxReqV0110(t, env.router, adminAC, http.MethodPost, "/Sessions/"+sess.Id+"/Command", []byte(`{"Name":"GoHome"}`), "application/json"); r.Code != http.StatusNoContent {
		t.Fatalf("postSessionCommand failed: %d", r.Code)
	}
	if r := doAuthCtxReqV0110(t, env.router, adminAC, http.MethodPost, "/Sessions/"+sess.Id+"/Message?Header=Hi&Text=Hello", nil, ""); r.Code != http.StatusNoContent {
		t.Fatalf("postSessionMessage failed: %d", r.Code)
	}
	for _, ep := range []string{"Playing", "Playing/Pause", "Command", "Message"} {
		if r := doAuthCtxReqV0110(t, env.router, adminAC, http.MethodPost, "/Sessions/missing-sess/"+ep, nil, ""); r.Code != http.StatusNotFound {
			t.Fatalf("missing session %s expected 404, got %d", ep, r.Code)
		}
		if r := doAuthCtxReqV0110(t, env.router, adminAC, http.MethodPost, "/Sessions/"+sess.Id+"/"+ep, []byte(`{bad-json`), "application/json"); r.Code != http.StatusBadRequest {
			t.Fatalf("malformed JSON %s expected 400, got %d", ep, r.Code)
		}
	}
}
