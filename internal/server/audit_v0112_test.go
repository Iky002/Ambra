package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

type testNotifierPlugin struct {
	id    string
	calls atomic.Int32
	err   error
}

func (p *testNotifierPlugin) Manifest() plugins.Manifest {
	return plugins.Manifest{
		ID:      p.id,
		Name:    "Test Notifier",
		Version: "1.0.0",
		Enabled: true,
		Kinds:   []plugins.Kind{plugins.KindNotifier},
	}
}

func (p *testNotifierPlugin) Configure(_ map[string]any) error { return nil }

func (p *testNotifierPlugin) Notify(_ context.Context, _ plugins.NotificationPayload) error {
	p.calls.Add(1)
	return p.err
}

func TestAuditV0112_DynamicDLNA_LiveTVWSFilter_NotifBridge_AndHooks(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	cfg, err := config.Load(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.EnableDLNA = false

	srv, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	defer func() { _ = srv.Close() }()

	if srv.SSDPBroadcaster() != nil {
		t.Fatal("expected SSDP broadcaster nil when EnableDLNA=false at boot")
	}

	apiKey, err := srv.Auth.CreateAPIKey(ctx, "wave2-admin")
	if err != nil {
		t.Fatal(err)
	}
	adminToken := apiKey.Token

	doJSON := func(token, method, path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		var rdr *bytes.Reader
		if body != nil {
			raw, mErr := json.Marshal(body)
			if mErr != nil {
				t.Fatal(mErr)
			}
			rdr = bytes.NewReader(raw)
		} else {
			rdr = bytes.NewReader(nil)
		}
		req := httptest.NewRequest(method, path, rdr)
		req.RemoteAddr = "127.0.0.1:43210"
		if token != "" {
			req.Header.Set("X-Emby-Token", token)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		srv.Handler.ServeHTTP(w, req)
		return w
	}

	// 1. M-11: Dynamic DLNA SSDP toggle + DLNADefaultUserID runtime update via POST /System/Configuration
	restrictedDLNAUser, err := srv.Auth.CreateUser(ctx, "dlna_no_play", "pw123456", false)
	if err != nil {
		t.Fatal(err)
	}
	userRepo := sqlite.NewUserRepo(srv.Store)
	noPlayPol := sqlite.DefaultUserPolicy(false)
	noPlayPol.EnableMediaPlayback = false
	if err := userRepo.UpdateUserPolicy(ctx, restrictedDLNAUser.ID, noPlayPol); err != nil {
		t.Fatal(err)
	}

	if w := doJSON(adminToken, http.MethodPost, "/System/Configuration", map[string]any{
		"EnableDLNA":        true,
		"DLNADefaultUserID": restrictedDLNAUser.ID,
	}); w.Code != http.StatusNoContent {
		t.Fatalf("enable DLNA via POST /System/Configuration: %d %s", w.Code, w.Body.String())
	}
	if srv.SSDPBroadcaster() == nil {
		t.Fatal("expected SSDP broadcaster dynamically started when EnableDLNA toggled true")
	}

	// Verify HasPendingRestart remained false (since OnDLNAConfigChanged handled it dynamically!)
	wInfo := doJSON(adminToken, http.MethodGet, "/System/Info", nil)
	if wInfo.Code != http.StatusOK {
		t.Fatalf("GET /System/Info: %d", wInfo.Code)
	}
	var sysInfo dto.SystemInfo
	_ = json.NewDecoder(wInfo.Body).Decode(&sysInfo)
	if sysInfo.HasPendingRestart {
		t.Fatal("expected HasPendingRestart=false when DLNA toggled dynamically")
	}

	// Toggle EnableDLNA back to false -> stops SSDP dynamically
	if w := doJSON(adminToken, http.MethodPost, "/System/Configuration", map[string]any{
		"EnableDLNA": false,
	}); w.Code != http.StatusNoContent {
		t.Fatalf("disable DLNA via POST /System/Configuration: %d", w.Code)
	}
	if srv.SSDPBroadcaster() != nil {
		t.Fatal("expected SSDP broadcaster stopped and cleared when EnableDLNA toggled false")
	}

	// 2. H-04: EnableAllFolders=true + EnableLiveTvAccess=false blocks livetv library in session checker & WS filter!
	libRepo := sqlite.NewLibraryRepo(srv.Store)
	liveLib, err := libRepo.CreateLibrary(ctx, "DVR Recordings", "livetv", []string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	dvrItem := &sqlite.ItemRecord{
		ID:        guid.New(),
		LibraryID: liveLib.ID,
		ParentID:  liveLib.ID,
		Type:      "Video",
		MediaType: "Video",
		Name:      "Recorded Match",
	}
	if err := libRepo.UpsertItem(ctx, dvrItem); err != nil {
		t.Fatal(err)
	}

	allFoldersNoTV, err := srv.Auth.CreateUser(ctx, "allfolders_notv", "pw123456", false)
	if err != nil {
		t.Fatal(err)
	}
	noTVPol := sqlite.DefaultUserPolicy(false)
	noTVPol.EnableAllFolders = true
	noTVPol.EnableLiveTvAccess = false
	if err := userRepo.UpdateUserPolicy(ctx, allFoldersNoTV.ID, noTVPol); err != nil {
		t.Fatal(err)
	}
	allFoldersNoTV, _ = userRepo.GetUserByID(ctx, allFoldersNoTV.ID)

	checker := buildSessionItemAccessChecker(libRepo)
	if checker(ctx, allFoldersNoTV, dvrItem.ID) {
		t.Fatal("H-04: expected buildSessionItemAccessChecker=false for livetv item when EnableLiveTvAccess=false even with EnableAllFolders=true")
	}
	wsFilter := buildWSLibraryChangeFilter(userRepo, libRepo)
	if _, ok := wsFilter(ctx, allFoldersNoTV.ID, map[string]any{"LibraryId": liveLib.ID, "ItemsAdded": 1}); ok {
		t.Fatal("H-04: expected WS LibraryChanged event for livetv library dropped when EnableLiveTvAccess=false")
	}

	// 3. 3.1-8: POST /Notifications/Services/Test dispatches to registry.Notifiers() & records notification
	np := &testNotifierPlugin{id: "ext.test.notifier"}
	srv.Registry.Register(np)
	userLogin, err := srv.Auth.AuthenticateByName(ctx, "allfolders_notv", "pw123456", "", auth.ClientInfo{DeviceID: "d-notif"})
	if err != nil {
		t.Fatal(err)
	}
	if w := doJSON(userLogin.AccessToken, http.MethodPost, "/Notifications/Services/Test", map[string]any{}); w.Code != http.StatusNoContent {
		t.Fatalf("POST /Notifications/Services/Test expected 204, got %d %s", w.Code, w.Body.String())
	}
	if np.calls.Load() != 1 {
		t.Fatalf("expected 1 notifier plugin call, got %d", np.calls.Load())
	}
	wSummary := doJSON(userLogin.AccessToken, http.MethodGet, "/Notifications/"+allFoldersNoTV.ID+"/Summary", nil)
	if wSummary.Code != http.StatusOK {
		t.Fatalf("GET /Notifications/{UserId}/Summary: %d", wSummary.Code)
	}
	var summary struct {
		UnreadCount int `json:"UnreadCount"`
	}
	_ = json.NewDecoder(wSummary.Body).Decode(&summary)
	if summary.UnreadCount < 1 {
		t.Fatalf("expected test notification recorded in user notifications, unread=%d", summary.UnreadCount)
	}

	np.err = errors.New("notifier downstream failure")
	if w := doJSON(userLogin.AccessToken, http.MethodPost, "/Notifications/Services/Test", map[string]any{}); w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 when notifier plugin fails, got %d", w.Code)
	}
	srv.Registry.Unregister(np.id)

	// 4. L-05: Dynamic TranscodingTempPath wired from transcodeMgr.CurrentTempDir into CleanTranscodeCache
	customTemp := t.TempDir()
	if w := doJSON(adminToken, http.MethodPost, "/Encoding/FfmpegOptions", map[string]any{
		"TranscodingTempPath": customTemp,
	}); w.Code != http.StatusNoContent {
		t.Fatalf("POST /Encoding/FfmpegOptions: %d %s", w.Code, w.Body.String())
	}
	staleSeg := filepath.Join(customTemp, "stale-seg.ts")
	if err := os.WriteFile(staleSeg, []byte("ts"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := srv.Scheduler.Run("CleanTranscodeCache"); err != nil {
		t.Fatal(err)
	}
	srv.Scheduler.Wait("CleanTranscodeCache")
	if _, err := os.Stat(staleSeg); !os.IsNotExist(err) {
		t.Fatalf("expected CleanTranscodeCache to clean dynamic TranscodingTempPath, stat err=%v", err)
	}
}
