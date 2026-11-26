package compatext

import (
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

	"github.com/ambra-media/ambra/internal/api/handlers/livetv"
	syncapi "github.com/ambra-media/ambra/internal/api/handlers/sync"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestCompatExtV0107AuditSecurityAndSnapshotReuse(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "ce_v0107.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	cfg := config.Default(tmp)
	userRepo := sqlite.NewUserRepo(store)
	libRepo := sqlite.NewLibraryRepo(store)
	authSvc := auth.NewService(userRepo, cfg)

	alice, _ := authSvc.CreateUser(ctx, "alice_107", "pass1234", false)
	_, _ = authSvc.CreateUser(ctx, "admin_107", "pass1234", true)
	aliceAuth, _ := authSvc.AuthenticateByName(ctx, "alice_107", "pass1234", "", auth.ClientInfo{DeviceID: "d-a"})
	adminAuth, _ := authSvc.AuthenticateByName(ctx, "admin_107", "pass1234", "", auth.ClientInfo{DeviceID: "d-adm"})

	lib, _ := libRepo.CreateLibrary(ctx, "Movies", "movies", []string{tmp})
	mediaFile := filepath.Join(tmp, "clip.mp4")
	_ = os.WriteFile(mediaFile, []byte("VIDEO"), 0o644)
	item := &sqlite.ItemRecord{
		ID:        guid.New(),
		LibraryID: lib.ID,
		ParentID:  lib.ID,
		Type:      "Movie",
		MediaType: "Video",
		Name:      "Clip",
		Path:      mediaFile,
	}
	_ = libRepo.UpsertItem(ctx, item)

	lt := livetv.New(store, cfg)
	ltMux := http.NewServeMux()
	lt.RegisterRoutes(ltMux, livetv.AuthFuncs{})
	sh := syncapi.New(store, cfg)
	sh.SetLibraryRepo(libRepo)
	sh.SetUserRepo(userRepo)

	h := New(store, cfg)
	h.SetLiveTV(lt)
	h.SetSyncHandler(sh)
	h.SetLibraryRepo(libRepo)
	h.SetUserRepo(userRepo)

	authMW := middleware.NewAuth(authSvc, nil)
	router := h.BuildRouter(AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
	})

	call := func(tok, method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if tok != "" {
			req.Header.Set("X-Emby-Token", tok)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	// 1. M-6: HEAD /LiveTv/LiveRecordings/{Id}/stream works
	recDir := filepath.Join(tmp, "recordings")
	_ = os.MkdirAll(recDir, 0o755)
	recFile := filepath.Join(recDir, "show.ts")
	_ = os.WriteFile(recFile, []byte("TSDATA"), 0o644)
	addedRec := lt.AddRecording(ctx, livetv.RecordingInfo{
		Name:      "Live Show",
		ChannelId: "ch-1",
		Status:    "Completed",
		StartDate: time.Now().UTC(),
		EndDate:   time.Now().UTC().Add(time.Hour),
		Path:      recFile,
	})
	if rec := call(aliceAuth.AccessToken, http.MethodHead, "/LiveTv/LiveRecordings/"+addedRec.Id+"/stream", ""); rec.Code != http.StatusOK {
		t.Fatalf("HEAD /LiveTv/LiveRecordings/{Id}/stream expected 200, got %d", rec.Code)
	}

	// 2. M-16: compatext LiveTV endpoints reuse h.liveTV snapshots
	m3u := "#EXTM3U\n#EXTINF:-1 tvg-id=\"c1\" group-title=\"News\",News One\nhttp://93.184.216.34/c1.m3u8\n"
	tunerBody, _ := json.Marshal(map[string]any{"Type": "m3u", "FriendlyName": "LiveTuner", "M3UContent": m3u})
	ltRec := httptest.NewRecorder()
	ltMux.ServeHTTP(ltRec, httptest.NewRequest(http.MethodPost, "/LiveTv/TunerHosts", strings.NewReader(string(tunerBody))))
	if ltRec.Code != http.StatusOK {
		t.Fatalf("seed tuner: %d", ltRec.Code)
	}
	for _, ep := range []string{
		"/LiveTv/EPG",
		"/LiveTv/Manage/Channels",
		"/LiveTv/ChannelTags",
		"/LiveTv/TunerHosts",
		"/LiveTv/ListingProviders",
		"/LiveTv/ChannelMappingOptions?ProviderId=p1",
		"/LiveTv/Tuners/Discover",
	} {
		if rec := call(adminAuth.AccessToken, http.MethodGet, ep, ""); rec.Code != http.StatusOK {
			t.Fatalf("GET %s expected 200, got %d", ep, rec.Code)
		}
	}

	// 3. M-8: SyncJobItemFile fail-closed 404 when library item deleted; AdditionalFiles checks canDownloadContent & canAccessItemID
	shMux := http.NewServeMux()
	sh.RegisterRoutes(shMux, syncapi.AuthFuncs{})
	jobReq := httptest.NewRequest(http.MethodPost, "/Sync/Jobs", strings.NewReader(fmt.Sprintf(`{"ItemIds":[%q]}`, item.ID)))
	jobReq = jobReq.WithContext(middleware.WithAuthContext(ctx, &middleware.AuthContext{User: alice}))
	jobRec := httptest.NewRecorder()
	shMux.ServeHTTP(jobRec, jobReq)
	if jobRec.Code != http.StatusOK {
		t.Fatalf("create sync job: %d %s", jobRec.Code, jobRec.Body.String())
	}
	var created syncapi.SyncJobCreationResult
	_ = json.Unmarshal(jobRec.Body.Bytes(), &created)
	jiID := created.JobItems[0].Id

	// MarkForRemoval succeeds on real item, then deleting job item cleans RemovedJobItems
	if rec := call(aliceAuth.AccessToken, http.MethodPost, "/Sync/JobItems/"+jiID+"/MarkForRemoval", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("MarkForRemoval expected 204, got %d", rec.Code)
	}
	h.mu.RLock()
	marked := h.state.RemovedJobItems[jiID]
	h.mu.RUnlock()
	if !marked {
		t.Fatal("expected real job item to be recorded in RemovedJobItems")
	}

	// AdditionalFiles denied when EnableContentDownloading=false
	alice.Policy.EnableContentDownloading = false
	_ = userRepo.UpdateUserPolicy(ctx, alice.ID, alice.Policy)
	if rec := call(aliceAuth.AccessToken, http.MethodGet, "/Sync/JobItems/"+jiID+"/AdditionalFiles?Name=sub.vtt", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("AdditionalFiles expected 403 when downloading disabled, got %d", rec.Code)
	}
	alice.Policy.EnableContentDownloading = true
	_ = userRepo.UpdateUserPolicy(ctx, alice.ID, alice.Policy)

	// Delete item from library -> SyncJobItemFile (GET & HEAD) and AdditionalFiles MUST return 404 (fail-closed)
	_ = libRepo.DeleteItem(ctx, item.ID)
	if rec := call(aliceAuth.AccessToken, http.MethodGet, "/Sync/JobItems/"+jiID+"/File", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("GET File for deleted library item expected 404, got %d", rec.Code)
	}
	if rec := call(aliceAuth.AccessToken, http.MethodHead, "/Sync/JobItems/"+jiID+"/File", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("HEAD File for deleted library item expected 404, got %d", rec.Code)
	}
	if rec := call(aliceAuth.AccessToken, http.MethodGet, "/Sync/JobItems/"+jiID+"/AdditionalFiles?Name=sub.vtt", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("AdditionalFiles for deleted library item expected 404, got %d", rec.Code)
	}

	// Deleting sync job cleans up RemovedJobItems
	if rec := call(aliceAuth.AccessToken, http.MethodPost, "/Sync/Jobs/"+created.Job.Id+"/Delete", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("Delete job expected 204, got %d", rec.Code)
	}
	h.mu.RLock()
	stillMarked := h.state.RemovedJobItems[jiID]
	h.mu.RUnlock()
	if stillMarked {
		t.Fatal("expected RemovedJobItems entry cleaned when parent job deleted")
	}

	// 4. H-14: Per-user NotificationServices quota (50)
	for i := 0; i < maxNotificationServicesPerUser; i++ {
		rec := call(aliceAuth.AccessToken, http.MethodPost, "/Notifications/Services/Configured", fmt.Sprintf(`{"Id":"u-svc-%d","Name":"Svc %d"}`, i, i))
		if rec.Code != http.StatusOK {
			t.Fatalf("create notification service #%d expected 200, got %d (%s)", i, rec.Code, rec.Body.String())
		}
	}
	if rec := call(aliceAuth.AccessToken, http.MethodPost, "/Notifications/Services/Configured", `{"Id":"u-svc-overflow","Name":"Overflow"}`); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on 51st notification service for alice, got %d", rec.Code)
	}
}
