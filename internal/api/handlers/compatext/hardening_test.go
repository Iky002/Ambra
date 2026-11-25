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
	"time"

	"github.com/ambra-media/ambra/internal/api/handlers/livetv"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/dvr"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestLiveTVHardeningHLSChannelMappingAndPersistence(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmpDir, "livetv_hardening.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/live/master.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:6.0,\nseg0.ts\n"))
		case "/live/raw.ts":
			w.Header().Set("Content-Type", "video/mp2t")
			_, _ = w.Write([]byte("RAW_TS_STREAM"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer upstream.Close()

	cfg := &config.Config{ServerID: "srv-hard", DataDir: tmpDir}
	dvrRec := dvr.NewRecorder(dvr.Options{DataDir: tmpDir})
	defer func() { _ = dvrRec.Close() }()

	lt := livetv.New(store, cfg)
	lt.SetRecorder(dvrRec)
	ltMux := http.NewServeMux()
	lt.RegisterRoutes(ltMux, livetv.AuthFuncs{})

	// Seed two channels: one HLS (.m3u8) and one non-HLS (.ts)
	m3u := "#EXTM3U\n" +
		"#EXTINF:-1 tvg-id=\"epg-hls\",HLS Channel\n" + upstream.URL + "/live/master.m3u8\n" +
		"#EXTINF:-1 tvg-id=\"epg-ts\",TS Channel\n" + upstream.URL + "/live/raw.ts\n"
	thBody, _ := json.Marshal(map[string]any{"Type": "m3u", "M3UContent": m3u})
	ltRec := httptest.NewRecorder()
	ltMux.ServeHTTP(ltRec, httptest.NewRequest(http.MethodPost, "/LiveTv/TunerHosts", strings.NewReader(string(thBody))))
	if ltRec.Code != http.StatusOK {
		t.Fatalf("seed tuner host: %d %s", ltRec.Code, ltRec.Body.String())
	}

	chListRec := httptest.NewRecorder()
	ltMux.ServeHTTP(chListRec, httptest.NewRequest(http.MethodGet, "/LiveTv/Channels", nil))
	var chResp struct {
		Items []livetv.ChannelInfo `json:"Items"`
	}
	_ = json.Unmarshal(chListRec.Body.Bytes(), &chResp)
	if len(chResp.Items) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(chResp.Items))
	}
	var hlsChID, tsChID string
	for _, ch := range chResp.Items {
		if ch.Name == "HLS Channel" {
			hlsChID = ch.Id
		} else if ch.Name == "TS Channel" {
			tsChID = ch.Id
		}
	}

	// Seed recordings (one .ts, one .m3u8)
	if err := os.MkdirAll(dvrRec.RecordingsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	recTS := filepath.Join(dvrRec.RecordingsDir(), "show.ts")
	recM3U8 := filepath.Join(dvrRec.RecordingsDir(), "show.m3u8")
	_ = os.WriteFile(recTS, []byte("RECORDED_TS"), 0o600)
	_ = os.WriteFile(recM3U8, []byte("#EXTM3U\n#EXTINF:5.0,\npart0.ts\n"), 0o600)
	lt.AddRecording(context.Background(), livetv.RecordingInfo{Id: "rec-ts", Name: "Show TS", Path: recTS})
	lt.AddRecording(context.Background(), livetv.RecordingInfo{Id: "rec-m3u8", Name: "Show HLS", Path: recM3U8})

	h := New(store, cfg)
	h.SetLiveTV(lt)
	h.SetDVR(dvrRec)
	router := h.BuildRouter(AuthFuncs{})

	// 1. LiveStreamFiles HLS for .m3u8 channel proxies real upstream HLS playlist (GET) and returns empty body (HEAD)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/LiveTv/LiveStreamFiles/"+hlsChID+"/hls/master.m3u8", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "#EXTM3U") || !strings.Contains(w.Body.String(), "sig=") {
		t.Fatalf("expected proxied HLS playlist, got %d %q", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodHead, "/LiveTv/LiveStreamFiles/"+hlsChID+"/hls/master.m3u8", nil))
	if w.Code != http.StatusOK || w.Body.Len() != 0 {
		t.Fatalf("expected 200 empty body on HEAD HLS playlist, got %d len=%d", w.Code, w.Body.Len())
	}

	// 2. LiveStreamFiles HLS for non-HLS (.ts) channel redirects to /LiveTv/Channels/{Id}/Stream
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/LiveTv/LiveStreamFiles/"+tsChID+"/hls/live.m3u8?api_key=abc", nil))
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/LiveTv/Channels/"+tsChID+"/Stream?api_key=abc" {
		t.Fatalf("expected 302 redirect for non-HLS channel, got %d loc=%q", w.Code, w.Header().Get("Location"))
	}

	// 3. LiveRecordings HLS: .m3u8 served directly, .ts redirected to /LiveTv/LiveRecordings/{Id}/stream, unknown -> 404
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/LiveTv/LiveRecordings/rec-m3u8/hls/master.m3u8", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "part0.ts") {
		t.Fatalf("expected served recording m3u8, got %d %q", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/LiveTv/LiveRecordings/rec-ts/hls/live.m3u8", nil))
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/LiveTv/LiveRecordings/rec-ts/stream" {
		t.Fatalf("expected 302 redirect for non-HLS recording, got %d loc=%q", w.Code, w.Header().Get("Location"))
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/LiveTv/LiveRecordings/does-not-exist/stream", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown recording stream, got %d", w.Code)
	}

	// 4. ChannelMappingOptions populates ProviderChannels from loaded channels/EPG
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/LiveTv/ChannelMappingOptions?ProviderId=lp-1", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ChannelMappingOptions status=%d", w.Code)
	}
	var mapResp struct {
		ProviderChannels []NameIdPair `json:"ProviderChannels"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &mapResp); err != nil {
		t.Fatal(err)
	}
	if len(mapResp.ProviderChannels) < 2 {
		t.Fatalf("expected ProviderChannels populated from channels/EPG, got %+v", mapResp.ProviderChannels)
	}

	// 5. Disabled & SortIndex persisted onto shared livetv:state channels
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/LiveTv/Manage/Channels/"+hlsChID+"/Disabled", strings.NewReader(`{"Disabled":true}`)))
	if w.Code != http.StatusNoContent {
		t.Fatalf("set disabled status=%d", w.Code)
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/LiveTv/Manage/Channels/"+hlsChID+"/SortIndex", strings.NewReader(`{"NewIndex":42}`)))
	if w.Code != http.StatusNoContent {
		t.Fatalf("set sort index status=%d", w.Code)
	}
	var persisted sharedLiveTVState
	if !h.readConfigJSON(context.Background(), liveTVConfigKey, &persisted) {
		t.Fatal("failed to read persisted livetv:state")
	}
	foundCh := false
	for _, ch := range persisted.Channels {
		if ch.Id == hlsChID {
			foundCh = true
			if !ch.Disabled || ch.SortIndex != 42 {
				t.Fatalf("expected Disabled=true SortIndex=42 persisted on livetv:state channel, got %+v", ch)
			}
		}
	}
	if !foundCh {
		t.Fatalf("channel %s not found in persisted livetv:state", hlsChID)
	}
}

func TestSyncAndNotificationsHardeningRBACAndOfflineActions(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmpDir, "sync_notif_hardening.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	libRepo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)

	userA := &sqlite.UserRecord{ID: guid.New(), Name: "alice", Policy: sqlite.DefaultUserPolicy(false), Config: sqlite.DefaultUserConfig()}
	userB := &sqlite.UserRecord{ID: guid.New(), Name: "bob", Policy: sqlite.DefaultUserPolicy(false), Config: sqlite.DefaultUserConfig()}
	adminU := &sqlite.UserRecord{ID: guid.New(), Name: "admin", IsAdmin: true, Policy: sqlite.DefaultUserPolicy(true), Config: sqlite.DefaultUserConfig()}
	_ = userRepo.CreateUser(ctx, userA)
	_ = userRepo.CreateUser(ctx, userB)
	_ = userRepo.CreateUser(ctx, adminU)

	lib, _ := libRepo.CreateLibrary(ctx, "Movies", "movies", []string{tmpDir})
	item1 := &sqlite.ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", MediaType: "Video", Name: "Movie 1"}
	item2 := &sqlite.ItemRecord{ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID, Type: "Movie", MediaType: "Video", Name: "Movie 2"}
	_ = libRepo.UpsertItem(ctx, item1)
	_ = libRepo.UpsertItem(ctx, item2)

	h := New(store, &config.Config{ServerID: "srv-rb", DataDir: tmpDir})
	h.SetLibraryRepo(libRepo)
	h.SetUserRepo(userRepo)

	currentCaller := userA.ID
	router := h.BuildRouter(AuthFuncs{
		UserIDFromCtx: func(context.Context) string { return currentCaller },
	})

	// 1. Notifications RBAC: Alice creates her service (omitting UserId -> defaults to Alice)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/Notifications/Services/Configured", strings.NewReader(`{"Name":"Alice Hook","Enabled":true}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("alice create notif: %d %s", w.Code, w.Body.String())
	}
	var aliceSvc NotificationServiceConfig
	_ = json.Unmarshal(w.Body.Bytes(), &aliceSvc)
	if aliceSvc.UserId != userA.ID {
		t.Fatalf("expected UserId=%s, got %s", userA.ID, aliceSvc.UserId)
	}

	// Alice cannot create a notification for Bob -> 403
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/Notifications/Services/Configured", strings.NewReader(`{"Name":"Spoof","UserId":"`+userB.ID+`"}`)))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when alice creates notif for bob, got %d", w.Code)
	}

	// Switch to Bob: Bob's GET without UserId sees empty list (not Alice's); GET with Alice's UserId -> 403
	currentCaller = userB.ID
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/Notifications/Services/Configured", nil))
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), aliceSvc.Id) {
		t.Fatalf("bob should not see alice's notif: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/Notifications/Services/Configured?UserId="+userA.ID, nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when bob queries alice's notif, got %d", w.Code)
	}
	// Bob cannot overwrite or delete Alice's service -> 403
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/Notifications/Services/Configured", strings.NewReader(`{"Id":"`+aliceSvc.Id+`","Name":"Hijack"}`)))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when bob updates alice's notif, got %d", w.Code)
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/Notifications/Services/Configured?Id="+aliceSvc.Id, nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when bob deletes alice's notif, got %d", w.Code)
	}

	// Admin can view and delete Alice's service
	currentCaller = adminU.ID
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/Notifications/Services/Configured?UserId="+userA.ID, nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), aliceSvc.Id) {
		t.Fatalf("admin should see alice's notif: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/Notifications/Services/Configured?Id="+aliceSvc.Id, nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("admin delete alice's notif: %d", w.Code)
	}

	// 2. Sync/JobItems/{Id}/File RBAC + missing file -> 404
	validSyncFile := filepath.Join(tmpDir, "alice_sync.mp4")
	_ = os.WriteFile(validSyncFile, []byte("SYNC_BYTES"), 0o600)
	h.writeConfigJSON(ctx, syncConfigKey, sharedSyncState{
		Jobs: []sharedSyncJob{
			{Id: "job-a", TargetId: "dev-a", Name: "Alice Job", UserId: userA.ID, DateCreated: time.Now()},
		},
		JobItems: []sharedSyncJobItem{
			{Id: "ji-ok", JobId: "job-a", ItemId: item1.ID, TargetId: "dev-a", Status: "Synced", OutputPath: validSyncFile},
			{Id: "ji-missing", JobId: "job-a", ItemId: item2.ID, TargetId: "dev-a", Status: "Synced", OutputPath: filepath.Join(tmpDir, "missing.mp4")},
			{Id: "ji-empty", JobId: "job-a", ItemId: item2.ID, TargetId: "dev-a", Status: "Queued", OutputPath: ""},
		},
	})

	// Bob denied on Alice's sync file (GET and HEAD)
	currentCaller = userB.ID
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		w = httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(method, "/Sync/JobItems/ji-ok/File", nil))
		if w.Code != http.StatusForbidden {
			t.Fatalf("bob %s ji-ok: got %d, want 403", method, w.Code)
		}
	}

	// Alice allowed on ji-ok, gets 404 on ji-missing and ji-empty
	currentCaller = userA.ID
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/Sync/JobItems/ji-ok/File", nil))
	if w.Code != http.StatusOK || w.Body.String() != "SYNC_BYTES" {
		t.Fatalf("alice GET ji-ok: %d %q", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodHead, "/Sync/JobItems/ji-ok/File", nil))
	if w.Code != http.StatusOK || w.Body.Len() != 0 {
		t.Fatalf("alice HEAD ji-ok: %d len=%d", w.Code, w.Body.Len())
	}
	for _, id := range []string{"ji-missing", "ji-empty", "ji-unknown"} {
		w = httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/Sync/JobItems/"+id+"/File", nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("alice GET %s: got %d, want 404", id, w.Code)
		}
	}

	// Admin allowed on Alice's sync file
	currentCaller = adminU.ID
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/Sync/JobItems/ji-ok/File", nil))
	if w.Code != http.StatusOK || w.Body.String() != "SYNC_BYTES" {
		t.Fatalf("admin GET ji-ok: %d %q", w.Code, w.Body.String())
	}

	// 3. POST /Sync/OfflineActions applies Played, PlaybackPositionTicks, IsFavorite to libRepo
	currentCaller = userA.ID
	payload := `[
		{"ItemId":"` + item1.ID + `","Played":true,"IsFavorite":true},
		{"ItemId":"` + item2.ID + `","PlaybackPositionTicks":123450000,"IsFavorite":true}
	]`
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/Sync/OfflineActions", strings.NewReader(payload)))
	if w.Code != http.StatusNoContent {
		t.Fatalf("offline actions status=%d", w.Code)
	}

	ud1, err := libRepo.GetUserItemData(ctx, userA.ID, item1.ID)
	if err != nil || !ud1.Played || !ud1.IsFavorite {
		t.Fatalf("expected item1 Played=true IsFavorite=true, got %+v err=%v", ud1, err)
	}
	ud2, err := libRepo.GetUserItemData(ctx, userA.ID, item2.ID)
	if err != nil || ud2.PlaybackPositionTicks != 123450000 || !ud2.IsFavorite {
		t.Fatalf("expected item2 PlaybackPositionTicks=123450000 IsFavorite=true, got %+v err=%v", ud2, err)
	}
}
