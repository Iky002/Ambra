package compatext

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	syncapi "github.com/ambra-media/ambra/internal/api/handlers/sync"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

type ceCtxKey string

const testCEUIDKey ceCtxKey = "ce_uid"

func TestCompatExtCoverageBoostV0103(t *testing.T) {
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
	alice, _ := authSvc.CreateUser(ctx, "alice", "alice123", false)
	bob, _ := authSvc.CreateUser(ctx, "bob", "bob123", false)

	allowedDir := filepath.Join(tmp, "allowed")
	secretDir := filepath.Join(tmp, "secret")
	_ = os.MkdirAll(allowedDir, 0o755)
	_ = os.MkdirAll(secretDir, 0o755)
	libAllowed, _ := libRepo.CreateLibrary(ctx, "Allowed", "movies", []string{allowedDir})
	libSecret, _ := libRepo.CreateLibrary(ctx, "Secret", "movies", []string{secretDir})

	pAlice := alice.Policy
	pAlice.EnableLiveTvAccess = true
	pAlice.EnableLiveTvManagement = true
	pAlice.EnableContentDownloading = true
	pAlice.EnableAllFolders = false
	pAlice.EnabledFolders = []string{libAllowed.ID}
	maxPG := 5
	pAlice.MaxParentalRating = &maxPG
	_ = userRepo.UpdateUserPolicy(ctx, alice.ID, pAlice)
	authSvc.InvalidateUserCache(alice.ID)

	pBob := bob.Policy
	pBob.EnableLiveTvAccess = false
	pBob.EnableContentDownloading = false
	_ = userRepo.UpdateUserPolicy(ctx, bob.ID, pBob)
	authSvc.InvalidateUserCache(bob.ID)

	mp4Path := filepath.Join(allowedDir, "movie.mp4")
	strmPath := filepath.Join(allowedDir, "cloud.strm")
	_ = os.WriteFile(mp4Path, []byte("MP4DATA"), 0o600)
	_ = os.WriteFile(strmPath, []byte("https://93.184.216.34/a.mkv"), 0o600)

	safeItem := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libAllowed.ID, ParentID: libAllowed.ID, Type: "Movie", MediaType: "Video", Name: "Safe", Path: mp4Path, OfficialRating: "G"}
	rItem := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libAllowed.ID, ParentID: libAllowed.ID, Type: "Movie", MediaType: "Video", Name: "Mature", Path: mp4Path, OfficialRating: "R"}
	secretItem := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libSecret.ID, ParentID: libSecret.ID, Type: "Movie", MediaType: "Video", Name: "Secret", Path: mp4Path, OfficialRating: "G"}
	strmItem := &sqlite.ItemRecord{ID: guid.New(), LibraryID: libAllowed.ID, ParentID: libAllowed.ID, Type: "Movie", MediaType: "Video", Name: "Strm", Path: strmPath, IsStrm: true}
	for _, it := range []*sqlite.ItemRecord{safeItem, rItem, secretItem, strmItem} {
		_ = libRepo.UpsertItem(ctx, it)
	}

	recDir := filepath.Join(cfg.DataDir, "recordings")
	_ = os.MkdirAll(recDir, 0o755)
	recSubDir := filepath.Join(recDir, "subdir")
	_ = os.MkdirAll(recSubDir, 0o755)
	outsideFile := filepath.Join(tmp, "outside.ts")
	_ = os.WriteFile(outsideFile, []byte("OUTSIDE"), 0o600)

	h := New(store, cfg)
	h.SetUserRepo(userRepo)
	h.SetLibraryRepo(libRepo)

	h.writeConfigJSON(ctx, liveTVConfigKey, sharedLiveTVState{
		Channels: []liveTVChannel{
			{Id: "ch1", Name: "Ch1", StreamUrl: "http://example.com/live.ts", Disabled: false},
			{Id: "ch2", Name: "Ch2", StreamUrl: "http://example.com/live.m3u8", Disabled: true},
		},
		Recordings: []liveTVRecording{
			{Id: "rec-out", Path: outsideFile},
			{Id: "rec-dir", Path: recSubDir},
		},
	})
	h.writeConfigJSON(ctx, syncConfigKey, sharedSyncState{
		Jobs: []sharedSyncJob{
			{Id: "job-alice", UserId: alice.ID, TargetId: "t-alice"},
			{Id: "job-bob", UserId: bob.ID, TargetId: "t-bob"},
		},
		JobItems: []sharedSyncJobItem{
			{Id: "ji-alice-ok", JobId: "job-alice", TargetId: "t-alice", ItemId: safeItem.ID, OutputPath: mp4Path},
			{Id: "ji-alice-secret", JobId: "job-alice", TargetId: "t-alice", ItemId: secretItem.ID, OutputPath: mp4Path},
			{Id: "ji-alice-strm-ext", JobId: "job-alice", TargetId: "t-alice", ItemId: safeItem.ID, OutputPath: strmPath},
			{Id: "ji-alice-strm-rec", JobId: "job-alice", TargetId: "t-alice", ItemId: strmItem.ID, OutputPath: mp4Path},
			{Id: "ji-alice-empty", JobId: "job-alice", TargetId: "t-alice", ItemId: safeItem.ID, OutputPath: ""},
			{Id: "ji-alice-missing", JobId: "job-alice", TargetId: "t-alice", ItemId: safeItem.ID, OutputPath: filepath.Join(tmp, "does-not-exist.mp4")},
			{Id: "ji-bob-item", JobId: "job-bob", TargetId: "t-bob", ItemId: safeItem.ID, OutputPath: mp4Path},
		},
	})

	authMW := middleware.NewAuth(authSvc, nil)
	router := h.BuildRouter(AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
		UserIDFromCtx: func(c context.Context) string {
			if ac, ok := middleware.AuthFromContext(c); ok && ac != nil {
				return ac.UserID()
			}
			if v, ok := c.Value(testCEUIDKey).(string); ok {
				return v
			}
			return ""
		},
	})

	adminAuth, _ := authSvc.AuthenticateByName(ctx, "admin", "admin123", "", auth.ClientInfo{DeviceID: "d0"})
	aliceAuth, _ := authSvc.AuthenticateByName(ctx, "alice", "alice123", "", auth.ClientInfo{DeviceID: "d1"})
	bobAuth, _ := authSvc.AuthenticateByName(ctx, "bob", "bob123", "", auth.ClientInfo{DeviceID: "d2"})

	call := func(tok, method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if tok != "" {
			req.Header.Set("X-Emby-Token", tok)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	// 1. handler.go RBAC & state helpers
	t.Run("handler_helpers_branches", func(t *testing.T) {
		reqBase := httptest.NewRequest(http.MethodGet, "/", nil)
		reqAdminUID := reqBase.WithContext(context.WithValue(ctx, testCEUIDKey, admin.ID))
		reqAliceUID := reqBase.WithContext(context.WithValue(ctx, testCEUIDKey, alice.ID))
		reqBobUID := reqBase.WithContext(context.WithValue(ctx, testCEUIDKey, bob.ID))
		reqUnknownUID := reqBase.WithContext(context.WithValue(ctx, testCEUIDKey, guid.New()))

		// isNonAdminCaller via UserIDFromCtx
		if h.isNonAdminCaller(reqAdminUID) {
			t.Fatal("admin via UserIDFromCtx should not be non-admin")
		}
		if !h.isNonAdminCaller(reqAliceUID) {
			t.Fatal("alice via UserIDFromCtx should be non-admin")
		}
		if !h.isNonAdminCaller(reqUnknownUID) {
			t.Fatal("unknown UID should be treated as non-admin")
		}
		if h.isNonAdminCaller(reqBase) {
			t.Fatal("no auth should return false")
		}

		// hasLiveTvAccess via UserIDFromCtx
		if !h.hasLiveTvAccess(reqAliceUID) {
			t.Fatal("alice has LiveTV access")
		}
		if h.hasLiveTvAccess(reqBobUID) {
			t.Fatal("bob lacks LiveTV access")
		}
		if h.hasLiveTvAccess(reqUnknownUID) {
			t.Fatal("unknown UID lacks LiveTV access")
		}

		// hasLiveTvManagement via UserIDFromCtx
		if !h.hasLiveTvManagement(reqAliceUID) {
			t.Fatal("alice has LiveTV management")
		}
		if h.hasLiveTvManagement(reqBobUID) {
			t.Fatal("bob lacks LiveTV management")
		}
		if h.hasLiveTvManagement(reqUnknownUID) {
			t.Fatal("unknown UID lacks LiveTV management")
		}

		// canDownloadContent edge cases: admin in DB via non-admin AuthContext + unknown UID
		nonAdminACForAdmin := &middleware.AuthContext{User: &sqlite.UserRecord{ID: admin.ID}}
		if !h.canDownloadContent(reqBase.WithContext(middleware.WithAuthContext(ctx, nonAdminACForAdmin))) {
			t.Fatal("admin in DB should be allowed to download")
		}
		if h.canDownloadContent(reqUnknownUID) {
			t.Fatal("unknown UID should not be allowed to download")
		}

		// canAccessItemID edge cases
		if !h.canAccessItemID(reqAliceUID, "") {
			t.Fatal("empty itemID should be allowed")
		}
		if !h.canAccessItemID(reqAliceUID, guid.New()) {
			t.Fatal("missing item should return true")
		}
		if !h.canAccessItemID(reqBase.WithContext(middleware.WithAuthContext(ctx, nonAdminACForAdmin)), secretItem.ID) {
			t.Fatal("admin in DB should access secret item")
		}
		if h.canAccessItemID(reqAliceUID, rItem.ID) {
			t.Fatal("R-rated item should be blocked by alice's parental rating")
		}
		if h.canAccessItemID(reqUnknownUID, safeItem.ID) {
			t.Fatal("unknown UID with nil policy should return false")
		}
		// Fallback to AuthContext.User.Policy when userRepo == nil
		hNoUserRepo := New(store, cfg)
		hNoUserRepo.SetLibraryRepo(libRepo)
		aliceFresh, _ := userRepo.GetUserByID(ctx, alice.ID)
		reqAliceAC := reqBase.WithContext(middleware.WithAuthContext(ctx, &middleware.AuthContext{User: aliceFresh}))
		if hNoUserRepo.canAccessItemID(reqAliceAC, secretItem.ID) {
			t.Fatal("expected secret item blocked via AuthContext.User.Policy fallback")
		}

		// ensureMapsLocked on zero state + writeConfigJSON nil store & unmarshalable value
		hZero := &Handler{}
		hZero.ensureMapsLocked()
		if hZero.state.ChannelDisabled == nil || hZero.state.ChannelSortIndex == nil || hZero.state.ChannelMappings == nil {
			t.Fatal("expected maps to be initialized")
		}
		hZero.writeConfigJSON(ctx, "k", map[string]any{"a": 1}) // nil store early return
		h.writeConfigJSON(ctx, "k", make(chan int))             // marshal error early return

		// requireLiveTvAccess HEAD request -> 403
		if rec := call(bobAuth.AccessToken, http.MethodHead, "/LiveTv/LiveStreamFiles/ch1/hls/master.m3u8", ""); rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for HEAD LiveTV without access, got %d", rec.Code)
		}
	})

	// 2. livetv.go branches: LiveRecordingHLS (outside dir & dir target, GET/HEAD), LiveRecordingStream dir target, LiveStreamFile redirect, LiveStreamHLS HEAD disabled, lookupChannel overrides, resolveRecordingPath
	t.Run("livetv_streaming_and_recording_edge_cases", func(t *testing.T) {
		// Recording outside recordings dir -> 403 on GET & HEAD
		if rec := call(aliceAuth.AccessToken, http.MethodGet, "/LiveTv/LiveRecordings/rec-out/hls/master.m3u8", ""); rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 GET outside recording HLS, got %d", rec.Code)
		}
		if rec := call(aliceAuth.AccessToken, http.MethodHead, "/LiveTv/LiveRecordings/rec-out/hls/master.m3u8", ""); rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 HEAD outside recording HLS, got %d", rec.Code)
		}
		// Recording pointing to a directory -> 404 on GET & HEAD (HLS) and GET (stream)
		if rec := call(aliceAuth.AccessToken, http.MethodGet, "/LiveTv/LiveRecordings/rec-dir/hls/master.m3u8", ""); rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 GET dir recording HLS, got %d", rec.Code)
		}
		if rec := call(aliceAuth.AccessToken, http.MethodHead, "/LiveTv/LiveRecordings/rec-dir/hls/master.m3u8", ""); rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 HEAD dir recording HLS, got %d", rec.Code)
		}
		if rec := call(aliceAuth.AccessToken, http.MethodGet, "/LiveTv/LiveRecordings/rec-dir/stream", ""); rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 GET dir recording stream, got %d", rec.Code)
		}

		// LiveStreamFile without liveTV proxy -> 302 redirect
		if rec := call(aliceAuth.AccessToken, http.MethodGet, "/LiveTv/LiveStreamFiles/ch1/stream.ts", ""); rec.Code != http.StatusFound {
			t.Fatalf("expected 302 redirect for LiveStreamFile, got %d", rec.Code)
		}
		// LiveStreamHLS HEAD on disabled channel -> 403
		if rec := call(aliceAuth.AccessToken, http.MethodHead, "/LiveTv/LiveStreamFiles/ch2/hls/master.m3u8", ""); rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 HEAD on disabled channel HLS, got %d", rec.Code)
		}

		// lookupChannel with disabled & sortIndex overrides
		h.mu.Lock()
		h.state.ChannelDisabled["ch1"] = true
		h.state.ChannelSortIndex["ch1"] = 42
		h.mu.Unlock()
		ch, ok := h.lookupChannel(ctx, "ch1")
		if !ok || !ch.Disabled || ch.SortIndex != 42 {
			t.Fatalf("expected overrides applied in lookupChannel, got %+v ok=%v", ch, ok)
		}

		// resolveRecordingPath edge cases
		if _, ok := h.resolveRecordingPath(""); ok {
			t.Fatal("empty target should fail")
		}
		if _, ok := h.resolveRecordingPath(filepath.Join(recDir, "nonexistent.ts")); ok {
			t.Fatal("nonexistent target should fail")
		}
		hBadDir := New(store, &config.Config{DataDir: filepath.Join(tmp, "no-such-dir")})
		if _, ok := hBadDir.resolveRecordingPath(outsideFile); ok {
			t.Fatal("nonexistent baseDir should fail")
		}
	})

	// 3. sync_notif.go: HEAD/GET branches on SyncJobItemFile, AdditionalFiles, CancelSyncItems, MarkJobItemRemoval, DeleteSyncJobItemPost, OfflineActions
	t.Run("sync_job_item_file_and_mutations", func(t *testing.T) {
		// HEAD when EnableContentDownloading=false -> 403
		if rec := call(bobAuth.AccessToken, http.MethodHead, "/Sync/JobItems/ji-bob-item/File", ""); rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 HEAD without download permission, got %d", rec.Code)
		}
		// HEAD missing job item -> 404
		if rec := call(aliceAuth.AccessToken, http.MethodHead, "/Sync/JobItems/no-such-item/File", ""); rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 HEAD missing job item, got %d", rec.Code)
		}
		// HEAD forbidden item (secret library) -> 403
		if rec := call(aliceAuth.AccessToken, http.MethodHead, "/Sync/JobItems/ji-alice-secret/File", ""); rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 HEAD forbidden library item, got %d", rec.Code)
		}
		// HEAD .strm OutputPath -> 403
		if rec := call(aliceAuth.AccessToken, http.MethodHead, "/Sync/JobItems/ji-alice-strm-ext/File", ""); rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 HEAD .strm OutputPath, got %d", rec.Code)
		}
		// GET & HEAD when OutputPath is .mp4 but DB record is IsStrm=true -> 403
		if rec := call(aliceAuth.AccessToken, http.MethodGet, "/Sync/JobItems/ji-alice-strm-rec/File", ""); rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 GET IsStrm DB record, got %d", rec.Code)
		}
		if rec := call(aliceAuth.AccessToken, http.MethodHead, "/Sync/JobItems/ji-alice-strm-rec/File", ""); rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 HEAD IsStrm DB record, got %d", rec.Code)
		}
		// HEAD empty OutputPath & missing file -> 404
		if rec := call(aliceAuth.AccessToken, http.MethodHead, "/Sync/JobItems/ji-alice-empty/File", ""); rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 HEAD empty outPath, got %d", rec.Code)
		}
		if rec := call(aliceAuth.AccessToken, http.MethodHead, "/Sync/JobItems/ji-alice-missing/File", ""); rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 HEAD missing file, got %d", rec.Code)
		}

		// AdditionalFiles: missing item (404), another user's item (403), non-.vtt (200 octet-stream)
		if rec := call(aliceAuth.AccessToken, http.MethodGet, "/Sync/JobItems/no-such/AdditionalFiles?Name=a.bin", ""); rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 AdditionalFiles missing, got %d", rec.Code)
		}
		if rec := call(aliceAuth.AccessToken, http.MethodGet, "/Sync/JobItems/ji-bob-item/AdditionalFiles?Name=a.bin", ""); rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 AdditionalFiles cross-user, got %d", rec.Code)
		}
		if rec := call(aliceAuth.AccessToken, http.MethodGet, "/Sync/JobItems/ji-alice-ok/AdditionalFiles?Name=meta.bin", ""); rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/octet-stream" {
			t.Fatalf("expected 200 octet-stream AdditionalFiles, got %d ct=%s", rec.Code, rec.Header().Get("Content-Type"))
		}

		// Cross-user mutations denied (403): MarkJobItemRemoval, DeleteSyncJobItemPost, CancelSyncItems
		if rec := call(aliceAuth.AccessToken, http.MethodPost, "/Sync/JobItems/ji-bob-item/MarkForRemoval", ""); rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 MarkForRemoval cross-user, got %d", rec.Code)
		}
		if rec := call(aliceAuth.AccessToken, http.MethodPost, "/Sync/JobItems/ji-bob-item/Delete", ""); rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 DeleteSyncJobItemPost cross-user, got %d", rec.Code)
		}
		if rec := call(aliceAuth.AccessToken, http.MethodPost, "/Sync/t-bob/Items/Delete?ItemIds="+safeItem.ID, ""); rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 CancelSyncItems cross-user, got %d", rec.Code)
		}
	})

	// 4. SyncJobItemFile & CancelSyncItems WITH live syncHandler attached
	t.Run("sync_with_live_sync_handler", func(t *testing.T) {
		sh := syncapi.New(store, cfg)
		h.SetSyncHandler(sh)
		defer h.SetSyncHandler(nil)

		// Missing item via syncHandler (GET & HEAD -> 404)
		if rec := call(aliceAuth.AccessToken, http.MethodGet, "/Sync/JobItems/missing-sh/File", ""); rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 GET missing via syncHandler, got %d", rec.Code)
		}
		if rec := call(aliceAuth.AccessToken, http.MethodHead, "/Sync/JobItems/missing-sh/File", ""); rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 HEAD missing via syncHandler, got %d", rec.Code)
		}
		// Unauthorized cross-user via syncHandler (GET & HEAD -> 403)
		if rec := call(aliceAuth.AccessToken, http.MethodGet, "/Sync/JobItems/ji-bob-item/File", ""); rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 GET cross-user via syncHandler, got %d", rec.Code)
		}
		if rec := call(aliceAuth.AccessToken, http.MethodHead, "/Sync/JobItems/ji-bob-item/File", ""); rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 HEAD cross-user via syncHandler, got %d", rec.Code)
		}
		// Authorized GET via syncHandler -> 200
		if rec := call(aliceAuth.AccessToken, http.MethodGet, "/Sync/JobItems/ji-alice-ok/File", ""); rec.Code != http.StatusOK {
			t.Fatalf("expected 200 GET own via syncHandler, got %d", rec.Code)
		}
		// CancelSyncItems via syncHandler (own -> 204)
		if rec := call(aliceAuth.AccessToken, http.MethodPost, "/Sync/t-alice/Items/Delete?ItemIds="+safeItem.ID, ""); rec.Code != http.StatusNoContent {
			t.Fatalf("expected 204 CancelSyncItems via syncHandler, got %d", rec.Code)
		}
	})

	// 5. OfflineActions: all-denied early return + Played/Unplayed/Favorite/Unfavorite/Type variants + admin acting for UserId + nil userRepo fallback
	t.Run("offline_actions_all_branches", func(t *testing.T) {
		// All items unauthorized -> early 204
		if rec := call(aliceAuth.AccessToken, http.MethodPost, "/Sync/OfflineActions", `[{"ItemId":"`+secretItem.ID+`","Played":true}]`); rec.Code != http.StatusNoContent {
			t.Fatalf("expected 204 all-denied offline actions, got %d", rec.Code)
		}

		// Temporarily nil userRepo to exercise sqlite.NewUserRepo(h.store) fallback
		h.SetUserRepo(nil)
		defer h.SetUserRepo(userRepo)

		payload := `[
			{"ItemId":"","Type":"Played"},
			{"ItemId":"` + safeItem.ID + `","UserId":"` + alice.ID + `","Played":false,"IsFavorite":true,"PositionTicks":500000},
			{"ItemId":"` + safeItem.ID + `","UserId":"` + alice.ID + `","Type":"Played","IsFavorite":false},
			{"ItemId":"` + safeItem.ID + `","UserId":"` + alice.ID + `","Type":"Unplayed"},
			{"ItemId":"` + safeItem.ID + `","UserId":"` + alice.ID + `","Type":"Favorite"},
			{"ItemId":"` + safeItem.ID + `","UserId":"` + alice.ID + `","Type":"Unfavorite"}
		]`
		if rec := call(adminAuth.AccessToken, http.MethodPost, "/Sync/OfflineActions", payload); rec.Code != http.StatusNoContent {
			t.Fatalf("expected 204 offline actions batch, got %d", rec.Code)
		}
		_ = admin
	})
}
