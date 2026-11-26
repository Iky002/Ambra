package sync

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

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/core/budget"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func doSyncV109AuthReq(t *testing.T, mux *http.ServeMux, ac *middleware.AuthContext, method, target string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, target, &buf)
	req.Header.Set("Content-Type", "application/json")
	if ac != nil {
		req = req.WithContext(middleware.WithAuthContext(req.Context(), ac))
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestSyncV0109AuditM24M25L20(t *testing.T) {
	h, mux, st, _ := setupSyncTest(t)
	ctx := context.Background()
	libRepo := sqlite.NewLibraryRepo(st)
	userRepo := sqlite.NewUserRepo(st)
	h.SetLibraryRepo(libRepo)
	h.SetUserRepo(userRepo)

	libAllowed, err := libRepo.CreateLibrary(ctx, "Allowed", "tvshows", []string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	libSecret, err := libRepo.CreateLibrary(ctx, "Secret", "movies", []string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}

	user := &sqlite.UserRecord{
		Name:   "alice_v109",
		Policy: sqlite.DefaultUserPolicy(false),
		Config: sqlite.DefaultUserConfig(),
	}
	user.Policy.EnableAllFolders = false
	user.Policy.EnabledFolders = []string{libAllowed.ID}
	user.Policy.EnableContentDownloading = true
	if err := userRepo.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	userAC := &middleware.AuthContext{User: user}

	// Build Series -> Season -> Episodes (one normal, one .strm) + Playlist with an accessible movie & a secret movie
	seriesID := guid.New()
	seasonID := guid.New()
	ep1ID := guid.New()
	epStrmID := guid.New()
	secretMovieID := guid.New()

	for _, rec := range []*sqlite.ItemRecord{
		{ID: seriesID, LibraryID: libAllowed.ID, ParentID: libAllowed.ID, Type: "Series", Name: "Show A"},
		{ID: seasonID, LibraryID: libAllowed.ID, ParentID: seriesID, SeriesID: seriesID, Type: "Season", Name: "Season 1"},
		{ID: ep1ID, LibraryID: libAllowed.ID, ParentID: seasonID, SeriesID: seriesID, SeasonID: seasonID, Type: "Episode", MediaType: "Video", Name: "Ep 1", Path: filepath.Join(libAllowed.Paths[0], "ep1.mp4")},
		{ID: epStrmID, LibraryID: libAllowed.ID, ParentID: seasonID, SeriesID: seriesID, SeasonID: seasonID, Type: "Episode", MediaType: "Video", Name: "Ep Strm", Path: filepath.Join(libAllowed.Paths[0], "ep2.strm"), IsStrm: true},
		{ID: secretMovieID, LibraryID: libSecret.ID, ParentID: libSecret.ID, Type: "Movie", MediaType: "Video", Name: "Secret Movie", Path: filepath.Join(libSecret.Paths[0], "secret.mp4")},
	} {
		if err := libRepo.UpsertItem(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}

	pl, err := libRepo.CreatePlaylist(ctx, "My List", "Video", user.ID, []string{ep1ID, epStrmID, secretMovieID})
	if err != nil {
		t.Fatal(err)
	}

	// 1. L-20: Duplicate/unnormalized ItemIds deduplicated + Series & Playlist containers expanded excluding .strm and forbidden items
	dashedSeries := seriesID[:8] + "-" + seriesID[8:12] + "-" + seriesID[12:16] + "-" + seriesID[16:20] + "-" + seriesID[20:]
	recCreate := doSyncV109AuthReq(t, mux, userAC, http.MethodPost, "/Sync/Jobs", map[string]any{
		"Name":     "Container Expand",
		"TargetId": "dev-109",
		"ItemIds":  []string{seriesID, strings.ToUpper(dashedSeries), pl.ID},
	})
	if recCreate.Code != http.StatusOK {
		t.Fatalf("create job with containers failed: %d %s", recCreate.Code, recCreate.Body.String())
	}
	var created SyncJobCreationResult
	if err := json.Unmarshal(recCreate.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if len(created.Job.RequestedItemIds) != 2 {
		t.Fatalf("L-20: expected RequestedItemIds deduplicated to 2, got %+v", created.Job.RequestedItemIds)
	}
	if len(created.JobItems) != 1 || guid.Normalize(created.JobItems[0].ItemId) != guid.Normalize(ep1ID) {
		t.Fatalf("L-20: expected container expansion to produce only accessible non-strm ep1ID (%s), got %+v", ep1ID, created.JobItems)
	}

	// 2. M-24: POST /Sync/Data skips Removing/Cancelled/Failed items and normalizes GUIDs while preserving original localID format
	h.mu.Lock()
	h.state.JobItems = append(h.state.JobItems,
		SyncJobItem{Id: "ji-rem", JobId: created.Job.Id, ItemId: "aaaa1111222233334444555566667777", TargetId: "dev-109", Status: "Removing"},
		SyncJobItem{Id: "ji-can", JobId: created.Job.Id, ItemId: "bbbb1111222233334444555566667777", TargetId: "dev-109", Status: "Cancelled"},
		SyncJobItem{Id: "ji-fail", JobId: created.Job.Id, ItemId: "cccc1111222233334444555566667777", TargetId: "dev-109", Status: "Failed"},
	)
	h.mu.Unlock()

	dashedEp1 := strings.ToUpper(ep1ID[:8] + "-" + ep1ID[8:12] + "-" + ep1ID[12:16] + "-" + ep1ID[16:20] + "-" + ep1ID[20:])
	dashedRem := "AAAA1111-2222-3333-4444-555566667777"
	recData := doSyncV109AuthReq(t, mux, userAC, http.MethodPost, "/Sync/Data", map[string]any{
		"TargetId":     "dev-109",
		"LocalItemIds": []string{dashedEp1, dashedRem, "bbbb1111222233334444555566667777", "cccc1111222233334444555566667777"},
	})
	if recData.Code != http.StatusOK {
		t.Fatalf("POST /Sync/Data status=%d", recData.Code)
	}
	var dataResp struct {
		ItemIdsToRemove []string `json:"ItemIdsToRemove"`
	}
	_ = json.Unmarshal(recData.Body.Bytes(), &dataResp)
	if len(dataResp.ItemIdsToRemove) != 3 {
		t.Fatalf("M-24: expected 3 items to remove (Removing/Cancelled/Failed) and dashedEp1 kept, got %+v", dataResp.ItemIdsToRemove)
	}
	if dataResp.ItemIdsToRemove[0] != dashedRem {
		t.Fatalf("M-24: expected original localID formatting preserved (%q), got %q", dashedRem, dataResp.ItemIdsToRemove[0])
	}

	// 3. M-25: DeleteJobForCaller / DeleteJobItemForCaller / SetJobItemRemovalForCaller return error and rollback on DB failure
	_ = st.Close()
	if found, allowed, err := h.SetJobItemRemovalForCaller(ctx, created.JobItems[0].Id, true, user.ID, false); !found || !allowed || err == nil {
		t.Fatalf("M-25: expected error on SetJobItemRemovalForCaller after DB close, got found=%v allowed=%v err=%v", found, allowed, err)
	}
	if found, allowed, err := h.DeleteJobItemForCaller(ctx, created.JobItems[0].Id, user.ID, false); !found || !allowed || err == nil {
		t.Fatalf("M-25: expected error on DeleteJobItemForCaller after DB close, got found=%v allowed=%v err=%v", found, allowed, err)
	}
	if found, allowed, err := h.DeleteJobForCaller(ctx, created.Job.Id, user.ID, false); !found || !allowed || err == nil {
		t.Fatalf("M-25: expected error on DeleteJobForCaller after DB close, got found=%v allowed=%v err=%v", found, allowed, err)
	}
	h.mu.RLock()
	if len(h.state.Jobs) != 1 || len(h.state.JobItems) != 4 {
		h.mu.RUnlock()
		t.Fatalf("M-25: expected sync state rolled back after persistence errors, got jobs=%d items=%d", len(h.state.Jobs), len(h.state.JobItems))
	}
	h.mu.RUnlock()
}

func TestSyncBackgroundWorkerFiltersNewContentAndCacheCleanup(t *testing.T) {
	h, mux, st, cfg := setupSyncTest(t)
	ctx := context.Background()
	libRepo := sqlite.NewLibraryRepo(st)
	userRepo := sqlite.NewUserRepo(st)
	h.SetLibraryRepo(libRepo)
	h.SetUserRepo(userRepo)
	h.SetBudget(budget.New(2))

	bus := eventbus.New()
	h.SubscribeEvents(bus)

	user := &sqlite.UserRecord{
		Name:   "sync_worker_user",
		Policy: sqlite.DefaultUserPolicy(false),
		Config: sqlite.DefaultUserConfig(),
	}
	user.Policy.EnableContentDownloading = true
	if err := userRepo.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	userAC := &middleware.AuthContext{User: user}

	mediaDir := t.TempDir()
	lib, err := libRepo.CreateLibrary(ctx, "TV", "tvshows", []string{mediaDir})
	if err != nil {
		t.Fatal(err)
	}

	seriesID := guid.New()
	epWatchedID := guid.New()
	epUnwatched1ID := guid.New()
	epUnwatched2ID := guid.New()

	ep1Path := filepath.Join(mediaDir, "ep1.mkv")
	ep2Path := filepath.Join(mediaDir, "ep2.mkv")
	ep3Path := filepath.Join(mediaDir, "ep3.mkv")
	for _, p := range []string{ep1Path, ep2Path, ep3Path} {
		if err := os.WriteFile(p, []byte("SOURCE-VIDEO-DATA"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	for _, rec := range []*sqlite.ItemRecord{
		{ID: seriesID, LibraryID: lib.ID, ParentID: lib.ID, Type: "Series", Name: "SciFi Show"},
		{ID: epWatchedID, LibraryID: lib.ID, ParentID: seriesID, SeriesID: seriesID, Type: "Episode", MediaType: "Video", Name: "Ep 1 Watched", Path: ep1Path},
		{ID: epUnwatched1ID, LibraryID: lib.ID, ParentID: seriesID, SeriesID: seriesID, Type: "Episode", MediaType: "Video", Name: "Ep 2 Unwatched", Path: ep2Path},
		{ID: epUnwatched2ID, LibraryID: lib.ID, ParentID: seriesID, SeriesID: seriesID, Type: "Episode", MediaType: "Video", Name: "Ep 3 Unwatched", Path: ep3Path},
	} {
		if err := libRepo.UpsertItem(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := libRepo.MarkPlayed(ctx, user.ID, epWatchedID); err != nil {
		t.Fatal(err)
	}

	// Create a mock ffmpeg shell script that writes deterministic transcoded MP4 bytes to the last argument (output path).
	mockFFmpeg := filepath.Join(t.TempDir(), "mock-ffmpeg.sh")
	script := "#!/bin/sh\nfor last; do true; done\nprintf 'TRANSCODED-SYNC-MP4' > \"$last\"\n"
	if err := os.WriteFile(mockFFmpeg, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	h.SetFFmpegPath(mockFFmpeg)
	h.StartWorker(ctx)
	defer h.StopWorker()

	// 1. Create job with UnwatchedOnly=true, ItemLimit=1, SyncNewContent=true, Bitrate=2000000
	//    -> Watched ep1 is filtered out, only 1 unwatched item is taken due to ItemLimit=1, and initial status is Queued/Converting!
	recCreate := doSyncV109AuthReq(t, mux, userAC, http.MethodPost, "/Sync/Jobs", map[string]any{
		"Name":           "Transcode Unwatched",
		"TargetId":       "tablet-1",
		"ItemIds":        []string{seriesID},
		"Quality":        "medium",
		"Bitrate":        2_000_000,
		"UnwatchedOnly":  true,
		"ItemLimit":      1,
		"SyncNewContent": true,
	})
	if recCreate.Code != http.StatusOK {
		t.Fatalf("POST /Sync/Jobs status=%d body=%s", recCreate.Code, recCreate.Body.String())
	}
	var created SyncJobCreationResult
	if err := json.Unmarshal(recCreate.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if len(created.JobItems) != 1 {
		t.Fatalf("expected 1 job item after UnwatchedOnly + ItemLimit=1, got %d (%+v)", len(created.JobItems), created.JobItems)
	}
	if guid.Normalize(created.JobItems[0].ItemId) == guid.Normalize(epWatchedID) {
		t.Fatalf("UnwatchedOnly failed: watched episode %s was included", epWatchedID)
	}

	jobID := created.Job.Id
	firstJobItemID := created.JobItems[0].Id
	expectedCacheOut := filepath.Join(cfg.DataDir, "cache", "sync", jobID, firstJobItemID+".mp4")

	// Wait for background worker to finish transcoding -> ReadyToTransfer with OutputPath in cache/sync/
	deadline := time.Now().Add(3 * time.Second)
	var readyItem SyncJobItem
	for time.Now().Before(deadline) {
		item, found, _ := h.GetJobItemForCaller(firstJobItemID, user.ID, true)
		if found && item != nil && item.Status == "ReadyToTransfer" {
			readyItem = *item
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if readyItem.Status != "ReadyToTransfer" {
		t.Fatalf("expected background worker to transition item to ReadyToTransfer, got %+v", readyItem)
	}
	if readyItem.OutputPath != expectedCacheOut {
		t.Fatalf("expected OutputPath=%q, got %q", expectedCacheOut, readyItem.OutputPath)
	}
	gotBytes, err := os.ReadFile(expectedCacheOut)
	if err != nil || string(gotBytes) != "TRANSCODED-SYNC-MP4" {
		t.Fatalf("expected transcoded file bytes at %s, err=%v got=%q", expectedCacheOut, err, string(gotBytes))
	}

	// 2. POST /Sync/JobItems/{Id}/Transferred cleans up staged cache file while preserving original source media!
	recTransferred := doSyncV109AuthReq(t, mux, userAC, http.MethodPost, "/Sync/JobItems/"+firstJobItemID+"/Transferred", nil)
	if recTransferred.Code != http.StatusNoContent {
		t.Fatalf("POST Transferred status=%d", recTransferred.Code)
	}
	if _, err := os.Stat(expectedCacheOut); !os.IsNotExist(err) {
		t.Fatalf("expected staged sync cache file removed after Transferred, stat err=%v", err)
	}
	if _, err := os.Stat(ep2Path); err != nil {
		t.Fatalf("original library source file must NEVER be deleted on Transferred: %v", err)
	}

	// 3. Raise ItemLimit to 3 and add a brand-new Episode 4 -> SyncNewContent automatically appends new items and queues transcoding!
	recUpd := doSyncV109AuthReq(t, mux, userAC, http.MethodPost, "/Sync/Jobs/"+jobID, map[string]any{
		"Name":           "Transcode Unwatched",
		"Quality":        "medium",
		"Bitrate":        2_000_000,
		"UnwatchedOnly":  true,
		"ItemLimit":      3,
		"SyncNewContent": true,
	})
	if recUpd.Code != http.StatusOK && recUpd.Code != http.StatusNoContent {
		t.Fatalf("update sync job status=%d", recUpd.Code)
	}

	epNewID := guid.New()
	epNewPath := filepath.Join(mediaDir, "ep4_new.mkv")
	_ = os.WriteFile(epNewPath, []byte("NEW-EP4"), 0o644)
	_ = libRepo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID: epNewID, LibraryID: lib.ID, ParentID: seriesID, SeriesID: seriesID,
		Type: "Episode", MediaType: "Video", Name: "Ep 4 New", Path: epNewPath,
	})
	bus.Publish(ctx, eventbus.Event{
		Type:    eventbus.EventLibraryChanged,
		Payload: map[string]any{"ItemId": epNewID},
	})

	deadline = time.Now().Add(3 * time.Second)
	sawNewReady := false
	for time.Now().Before(deadline) {
		if stItem, ok := h.GetItemStatusForCaller(epNewID, "tablet-1", user.ID, false); ok && stItem != nil && stItem.Status == "ReadyToTransfer" {
			sawNewReady = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !sawNewReady {
		t.Fatalf("expected SyncNewContent to auto-append and transcode newly added episode %s", epNewID)
	}

	// 4. DELETE /Sync/Jobs/{Id} removes the entire <DataDir>/cache/sync/<jobId> directory!
	recDel := doSyncV109AuthReq(t, mux, userAC, http.MethodDelete, "/Sync/Jobs/"+jobID, nil)
	if recDel.Code != http.StatusNoContent {
		t.Fatalf("DELETE /Sync/Jobs status=%d", recDel.Code)
	}
	if _, err := os.Stat(filepath.Join(cfg.DataDir, "cache", "sync", jobID)); !os.IsNotExist(err) {
		t.Fatalf("expected job cache directory deleted on job removal, stat err=%v", err)
	}
}
