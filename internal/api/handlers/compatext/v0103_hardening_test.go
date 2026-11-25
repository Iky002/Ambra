package compatext

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestCompatExtV0103Hardening(t *testing.T) {
	tmp := t.TempDir()
	cfg, err := config.Load(filepath.Join(tmp, "data"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	userRepo := sqlite.NewUserRepo(store)
	libRepo := sqlite.NewLibraryRepo(store)
	authSvc := auth.NewService(userRepo, cfg)

	admin, _ := authSvc.CreateUser(ctx, "admin", "admin123", true)
	noTV, _ := authSvc.CreateUser(ctx, "notv", "notv123", false)
	restricted, _ := authSvc.CreateUser(ctx, "restricted", "rest123", false)

	// noTV: EnableLiveTvAccess = false, EnableContentDownloading = false
	pNoTV := noTV.Policy
	pNoTV.EnableLiveTvAccess = false
	pNoTV.EnableContentDownloading = false
	_ = userRepo.UpdateUserPolicy(ctx, noTV.ID, pNoTV)
	authSvc.InvalidateUserCache(noTV.ID)

	// restricted: EnableLiveTvAccess = true, EnableContentDownloading = true, restricted to libAllowed
	libAllowed, _ := libRepo.CreateLibrary(ctx, "Allowed", "movies", []string{filepath.Join(tmp, "allowed")})
	libSecret, _ := libRepo.CreateLibrary(ctx, "Secret", "movies", []string{filepath.Join(tmp, "secret")})

	pRest := restricted.Policy
	pRest.EnableLiveTvAccess = true
	pRest.EnableContentDownloading = true
	pRest.EnableAllFolders = false
	pRest.EnabledFolders = []string{libAllowed.ID}
	_ = userRepo.UpdateUserPolicy(ctx, restricted.ID, pRest)
	authSvc.InvalidateUserCache(restricted.ID)

	mp4Path := filepath.Join(tmp, "safe.mp4")
	strmPath := filepath.Join(tmp, "cloud.strm")
	_ = os.WriteFile(mp4Path, []byte("MP4BYTES"), 0o600)
	_ = os.WriteFile(strmPath, []byte("https://93.184.216.34/a.mkv"), 0o600)

	safeItem := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libAllowed.ID, ParentID: libAllowed.ID, Type: "Movie", MediaType: "Video", Name: "Safe", Path: mp4Path}
	secretItem := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libSecret.ID, ParentID: libSecret.ID, Type: "Movie", MediaType: "Video", Name: "Secret", Path: mp4Path}
	strmItem := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libAllowed.ID, ParentID: libAllowed.ID, Type: "Movie", MediaType: "Video", Name: "Cloud", Path: strmPath, IsStrm: true}
	_ = libRepo.UpsertItem(ctx, safeItem)
	_ = libRepo.UpsertItem(ctx, secretItem)
	_ = libRepo.UpsertItem(ctx, strmItem)

	h := New(store, cfg)
	h.SetUserRepo(userRepo)
	h.SetLibraryRepo(libRepo)

	// Seed LiveTV channels (one enabled, one disabled) and sync job items
	h.writeConfigJSON(ctx, liveTVConfigKey, sharedLiveTVState{
		Channels: []liveTVChannel{
			{Id: "ch-on", Name: "Active", StreamUrl: "http://example.com/live.m3u8", Disabled: false},
			{Id: "ch-off", Name: "Disabled", StreamUrl: "http://example.com/off.m3u8", Disabled: true},
		},
	})
	h.writeConfigJSON(ctx, syncConfigKey, sharedSyncState{
		Jobs: []sharedSyncJob{
			{Id: "job-notv", UserId: noTV.ID, TargetId: "dev1"},
			{Id: "job-rest", UserId: restricted.ID, TargetId: "dev2"},
		},
		JobItems: []sharedSyncJobItem{
			{Id: "ji-notv", JobId: "job-notv", ItemId: safeItem.ID, OutputPath: mp4Path},
			{Id: "ji-secret", JobId: "job-rest", ItemId: secretItem.ID, OutputPath: mp4Path},
			{Id: "ji-strm", JobId: "job-rest", ItemId: strmItem.ID, OutputPath: strmPath},
			{Id: "ji-ok", JobId: "job-rest", ItemId: safeItem.ID, OutputPath: mp4Path},
		},
	})

	authMW := middleware.NewAuth(authSvc, nil)
	router := h.BuildRouter(AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
		UserIDFromCtx: func(ctx context.Context) string {
			if ac, ok := middleware.AuthFromContext(ctx); ok && ac != nil {
				return ac.UserID()
			}
			return ""
		},
	})

	adminAuth, _ := authSvc.AuthenticateByName(ctx, "admin", "admin123", "", auth.ClientInfo{DeviceID: "d0"})
	noTVAuth, _ := authSvc.AuthenticateByName(ctx, "notv", "notv123", "", auth.ClientInfo{DeviceID: "d1"})
	restAuth, _ := authSvc.AuthenticateByName(ctx, "restricted", "rest123", "", auth.ClientInfo{DeviceID: "d2"})

	call := func(tok, method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("X-Emby-Token", tok)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	// 1. User without EnableLiveTvAccess is blocked on all non-admin LiveTV routes
	for _, ep := range []string{
		"/LiveTv/EPG",
		"/LiveTv/Folder",
		"/LiveTv/ChannelTags",
		"/LiveTv/LiveStreamFiles/ch-on/stream.ts",
		"/LiveTv/LiveStreamFiles/ch-on/hls/master.m3u8",
		"/LiveTv/AvailableRecordingOptions",
	} {
		if rec := call(noTVAuth.AccessToken, http.MethodGet, ep, ""); rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for noTV user on %s, got %d", ep, rec.Code)
		}
	}

	// 2. Disabled channel is rejected on both LiveStreamFile and LiveStreamHLS
	if rec := call(restAuth.AccessToken, http.MethodGet, "/LiveTv/LiveStreamFiles/ch-off/stream.ts", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for disabled channel stream, got %d", rec.Code)
	}
	if rec := call(restAuth.AccessToken, http.MethodGet, "/LiveTv/LiveStreamFiles/ch-off/hls/master.m3u8", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for disabled channel HLS, got %d", rec.Code)
	}

	// 3. AvailableRecordingOptions hides physical paths for non-admin, shows for admin
	rec := call(restAuth.AccessToken, http.MethodGet, "/LiveTv/AvailableRecordingOptions", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("AvailableRecordingOptions (non-admin) got %d", rec.Code)
	}
	var opts map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &opts)
	if opts["DefaultRecordingPath"] != "" || opts["MovieRecordingPath"] != "" {
		t.Fatalf("expected empty recording paths for non-admin, got %+v", opts)
	}
	rec = call(adminAuth.AccessToken, http.MethodGet, "/LiveTv/AvailableRecordingOptions", "")
	_ = json.Unmarshal(rec.Body.Bytes(), &opts)
	if opts["DefaultRecordingPath"] == "" {
		t.Fatalf("expected admin to see DefaultRecordingPath (admin=%s), got %+v", admin.ID, opts)
	}

	// 4. SyncJobItemFile enforces EnableContentDownloading, blocks .strm, and enforces item RBAC
	if rec := call(noTVAuth.AccessToken, http.MethodGet, "/Sync/JobItems/ji-notv/File", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when EnableContentDownloading=false, got %d", rec.Code)
	}
	if rec := call(restAuth.AccessToken, http.MethodGet, "/Sync/JobItems/ji-secret/File", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for sync file in forbidden library, got %d", rec.Code)
	}
	if rec := call(restAuth.AccessToken, http.MethodGet, "/Sync/JobItems/ji-strm/File", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for .strm sync file download, got %d", rec.Code)
	}
	if rec := call(restAuth.AccessToken, http.MethodGet, "/Sync/JobItems/ji-ok/File", ""); rec.Code != http.StatusOK || rec.Body.String() != "MP4BYTES" {
		t.Fatalf("expected 200 MP4BYTES for allowed sync file, got %d %q", rec.Code, rec.Body.String())
	}

	// 5. Sync/OfflineActions ignores unauthorized ItemId and applies authorized ItemId
	actionsBody := `[{"ItemId":"` + secretItem.ID + `","Played":true},{"ItemId":"` + safeItem.ID + `","Played":true}]`
	if rec := call(restAuth.AccessToken, http.MethodPost, "/Sync/OfflineActions", actionsBody); rec.Code != http.StatusNoContent {
		t.Fatalf("OfflineActions got %d", rec.Code)
	}
	if secDTO, err := libRepo.GetItemByID(ctx, cfg.ServerID, restricted.ID, secretItem.ID); err == nil && secDTO != nil && secDTO.UserData != nil && secDTO.UserData.Played {
		t.Fatal("security regression: OfflineActions mutated Played on forbidden secretItem")
	}
	if safeDTO, err := libRepo.GetItemByID(ctx, cfg.ServerID, restricted.ID, safeItem.ID); err != nil || safeDTO == nil || safeDTO.UserData == nil || !safeDTO.UserData.Played {
		t.Fatalf("expected OfflineActions to mark safeItem Played, got %+v err=%v", safeDTO, err)
	}
}
