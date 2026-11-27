package sync

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestAuditWave1_H04_H05_M11_SyncPolicyPurgeTimeoutAndClamps(t *testing.T) {
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "data")
	store, err := sqlite.Open(filepath.Join(tmp, "sync_wave1.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	libRepo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(dataDir)

	hangFFmpeg := filepath.Join(tmp, "hang-ffmpeg.sh")
	hangScript := "#!/bin/sh\nexec sleep 10\n"
	if err := os.WriteFile(hangFFmpeg, []byte(hangScript), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg.FFmpegPath = hangFFmpeg

	moviesLib, _ := libRepo.CreateLibrary(ctx, "Movies", "movies", []string{tmp})
	dvrLib, _ := libRepo.CreateLibrary(ctx, "Recordings", "livetv", []string{tmp})

	mediaFile := filepath.Join(tmp, "vid.mkv")
	_ = os.WriteFile(mediaFile, []byte("MKV-CONTENT"), 0o644)

	safeMovie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: moviesLib.ID, ParentID: moviesLib.ID,
		Type: "Movie", MediaType: "Video", Name: "Safe Movie", OfficialRating: "G", Path: mediaFile,
	}
	dvrItem := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: dvrLib.ID, ParentID: dvrLib.ID,
		Type: "Video", MediaType: "Video", Name: "DVR Rec", OfficialRating: "G", Path: mediaFile,
	}
	unratedSeries := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: moviesLib.ID, ParentID: moviesLib.ID,
		Type: "Series", Name: "Unrated Show", OfficialRating: "", IsFolder: true,
	}
	unratedEp := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: moviesLib.ID, ParentID: unratedSeries.ID, SeriesID: unratedSeries.ID,
		Type: "Episode", MediaType: "Video", Name: "Ep 1", OfficialRating: "", Path: mediaFile,
	}
	for _, it := range []*sqlite.ItemRecord{safeMovie, dvrItem, unratedSeries, unratedEp} {
		if err := libRepo.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}

	alicePol := sqlite.DefaultUserPolicy(false)
	alicePol.EnableAllFolders = true
	alicePol.EnableLiveTvAccess = false
	alicePol.BlockUnratedItems = []string{"Series"}
	alicePol.EnableContentDownloading = true
	alice := &sqlite.UserRecord{ID: guid.New(), Name: "alice", Policy: alicePol, Config: sqlite.DefaultUserConfig()}

	bobPol := sqlite.DefaultUserPolicy(false)
	bobPol.EnableContentDownloading = true
	bob := &sqlite.UserRecord{ID: guid.New(), Name: "bob", Policy: bobPol, Config: sqlite.DefaultUserConfig()}

	for _, u := range []*sqlite.UserRecord{alice, bob} {
		_ = userRepo.CreateUser(ctx, u)
	}

	h := New(store, cfg)
	h.SetLibraryRepo(libRepo)
	h.SetUserRepo(userRepo)
	h.SetTranscodeTimeout(80 * time.Millisecond)
	h.StartWorker(ctx)
	defer h.StopWorker()

	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	doJSON := func(u *sqlite.UserRecord, method, target string, payload any) *httptest.ResponseRecorder {
		var body []byte
		if payload != nil {
			body, _ = json.Marshal(payload)
		}
		req := httptest.NewRequest(method, target, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(middleware.WithAuthContext(req.Context(), &middleware.AuthContext{User: u}))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// 1. H-04 & H-05: DVR livetv item and Series-unrated Episode are blocked when creating sync job!
	if code := doJSON(alice, http.MethodPost, "/Sync/Jobs", map[string]any{
		"TargetId": "dev-a", "ItemIds": []string{dvrItem.ID},
	}).Code; code != http.StatusForbidden {
		t.Fatalf("H-04: expected 403 creating sync job for livetv item when EnableLiveTvAccess=false, got %d", code)
	}
	if code := doJSON(alice, http.MethodPost, "/Sync/Jobs", map[string]any{
		"TargetId": "dev-a", "ItemIds": []string{unratedEp.ID},
	}).Code; code != http.StatusForbidden {
		t.Fatalf("H-05: expected 403 creating sync job for Episode of unrated Series when BlockUnratedItems=[Series], got %d", code)
	}

	// 2. M-11: Bitrate clamp (<= 200 Mbps) + transcode timeout marks item Failed instead of hanging forever!
	createAlice := doJSON(alice, http.MethodPost, "/Sync/Jobs", map[string]any{
		"TargetId": "dev-a",
		"Quality":  "custom",
		"Bitrate":  999_999_999,
		"ItemIds":  []string{safeMovie.ID},
	})
	if createAlice.Code != http.StatusOK {
		t.Fatalf("expected 200 creating Alice sync job, got %d: %s", createAlice.Code, createAlice.Body.String())
	}
	var aliceCreated SyncJobCreationResult
	_ = json.Unmarshal(createAlice.Body.Bytes(), &aliceCreated)
	if aliceCreated.Job.Bitrate != 200_000_000 {
		t.Fatalf("M-11: expected clamped Bitrate 200000000, got %d", aliceCreated.Job.Bitrate)
	}

	// Wait for worker to time out the hanging FFmpeg transcode and mark item Failed.
	deadline := time.Now().Add(4 * time.Second)
	markedFailed := false
	for time.Now().Before(deadline) {
		if len(aliceCreated.JobItems) > 0 {
			ji, found, _ := h.GetJobItemForCaller(aliceCreated.JobItems[0].Id, guid.Normalize(alice.ID), true)
			if found && ji.Status == "Failed" {
				markedFailed = true
				break
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !markedFailed {
		t.Fatal("M-11: expected timed-out sync transcode job item to transition to Failed")
	}

	// 3. M-11: handleUpdateJob enforces EnableContentDownloading!
	alicePolNoDL := alice.Policy
	alicePolNoDL.EnableContentDownloading = false
	_ = userRepo.UpdateUserPolicy(ctx, alice.ID, alicePolNoDL)
	aliceUpdated, _ := userRepo.GetUserByID(ctx, alice.ID)
	if code := doJSON(aliceUpdated, http.MethodPost, "/Sync/Jobs/"+aliceCreated.Job.Id, map[string]any{
		"Name":    "Updated",
		"Bitrate": 500_000_000,
	}).Code; code != http.StatusForbidden {
		t.Fatalf("M-11: expected 403 updating sync job when EnableContentDownloading=false, got %d", code)
	}

	// 4. M-11: PurgeUser removes all of Alice's jobs/items/cache while keeping Bob's job intact!
	createBob := doJSON(bob, http.MethodPost, "/Sync/Jobs", map[string]any{
		"TargetId": "dev-b",
		"Quality":  "original",
		"ItemIds":  []string{safeMovie.ID},
	})
	if createBob.Code != http.StatusOK {
		t.Fatalf("expected 200 creating Bob sync job, got %d", createBob.Code)
	}
	var bobCreated SyncJobCreationResult
	_ = json.Unmarshal(createBob.Body.Bytes(), &bobCreated)

	aliceCacheDir := filepath.Join(dataDir, "cache", "sync", aliceCreated.Job.Id)
	_ = os.MkdirAll(aliceCacheDir, 0o755)
	aliceCacheFile := filepath.Join(aliceCacheDir, "out.mp4")
	_ = os.WriteFile(aliceCacheFile, []byte("CACHED"), 0o644)

	h.PurgeUser(ctx, alice.ID)

	if code := doJSON(aliceUpdated, http.MethodGet, "/Sync/Jobs/"+aliceCreated.Job.Id, nil).Code; code != http.StatusNotFound {
		t.Fatalf("M-11: expected Alice's job to be purged (404), got %d", code)
	}
	if _, statErr := os.Stat(aliceCacheFile); !os.IsNotExist(statErr) {
		t.Fatalf("M-11: expected Alice's sync cache directory to be deleted, statErr=%v", statErr)
	}
	if code := doJSON(bob, http.MethodGet, "/Sync/Jobs/"+bobCreated.Job.Id, nil).Code; code != http.StatusOK {
		t.Fatalf("M-11: expected Bob's job to remain intact (200), got %d", code)
	}
}
