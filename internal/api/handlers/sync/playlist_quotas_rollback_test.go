package sync

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
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestSyncV0107AuditPlaylistQuotasValidationAndRollback(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default(dir)
	st, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	userRepo := sqlite.NewUserRepo(st)
	libRepo := sqlite.NewLibraryRepo(st)

	lib, _ := libRepo.CreateLibrary(ctx, "Movies", "movies", []string{dir})
	movie := &sqlite.ItemRecord{
		ID:        guid.New(),
		LibraryID: lib.ID,
		ParentID:  lib.ID,
		Type:      "Movie",
		MediaType: "Video",
		Name:      "Allowed Movie",
		Path:      filepath.Join(dir, "movie.mp4"),
	}
	_ = libRepo.UpsertItem(ctx, movie)

	alicePol := sqlite.DefaultUserPolicy(false)
	alicePol.EnableAllFolders = false
	alicePol.EnabledFolders = []string{lib.ID}
	alicePol.EnableContentDownloading = true
	alice := &sqlite.UserRecord{Name: "alice_v107", Policy: alicePol, Config: sqlite.DefaultUserConfig()}
	_ = userRepo.CreateUser(ctx, alice)

	bobPol := sqlite.DefaultUserPolicy(false)
	bobPol.EnableAllFolders = true
	bobPol.EnableContentDownloading = true
	bob := &sqlite.UserRecord{Name: "bob_v107", Policy: bobPol, Config: sqlite.DefaultUserConfig()}
	_ = userRepo.CreateUser(ctx, bob)

	// Bob's private playlist and public playlist; Alice's own playlist (library_id = "playlists", NOT in alice's EnabledFolders)
	bobPrivPl, _ := libRepo.CreatePlaylist(ctx, "Bob Private", "Video", bob.ID, []string{movie.ID})
	bobPubPl, _ := libRepo.CreatePlaylist(ctx, "Bob Public", "Video", bob.ID, []string{movie.ID})
	aliceOwnPl, _ := libRepo.CreatePlaylist(ctx, "Alice Own", "Video", alice.ID, []string{movie.ID})
	_, _ = st.Writer.ExecContext(ctx,
		`INSERT INTO server_config (key, value_json, updated_at) VALUES (?, ?, ?)`,
		"item_visibility_"+guid.Normalize(bobPubPl.ID), `{"IsPublic":true}`, time.Now().UTC().Unix(),
	)

	h := New(st, cfg)
	h.SetLibraryRepo(libRepo)
	h.SetUserRepo(userRepo)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	doAs := func(u *sqlite.UserRecord, method, path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		var rdr *bytes.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rdr = bytes.NewReader(b)
		} else {
			rdr = bytes.NewReader(nil)
		}
		req := httptest.NewRequest(method, path, rdr)
		if u != nil {
			req = req.WithContext(middleware.WithAuthContext(req.Context(), &middleware.AuthContext{User: u}))
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// 1. H-11: Alice denied Bob's private playlist (403)
	if r := doAs(alice, http.MethodPost, "/Sync/Jobs", map[string]any{"ItemIds": []string{bobPrivPl.ID}}); r.Code != http.StatusForbidden {
		t.Fatalf("expected 403 syncing another user's private playlist, got %d (%s)", r.Code, r.Body.String())
	}
	// Alice allowed Bob's public playlist (200)
	if r := doAs(alice, http.MethodPost, "/Sync/Jobs", map[string]any{"ItemIds": []string{bobPubPl.ID}}); r.Code != http.StatusOK {
		t.Fatalf("expected 200 syncing public playlist, got %d (%s)", r.Code, r.Body.String())
	}
	// Alice allowed her own playlist even with EnableAllFolders=false (library_id="playlists")
	rOwn := doAs(alice, http.MethodPost, "/Sync/Jobs", map[string]any{"ItemIds": []string{aliceOwnPl.ID}})
	if rOwn.Code != http.StatusOK {
		t.Fatalf("expected 200 syncing own playlist, got %d (%s)", rOwn.Code, rOwn.Body.String())
	}
	var ownJobRes SyncJobCreationResult
	_ = json.Unmarshal(rOwn.Body.Bytes(), &ownJobRes)
	if len(h.JobItemIDsForJob(ownJobRes.Job.Id)) != 1 {
		t.Fatalf("expected JobItemIDsForJob to return 1 item")
	}

	// 2. H-14: Field length limits & negative Bitrate/ItemLimit rejected (400)
	if r := doAs(alice, http.MethodPost, "/Sync/Jobs", map[string]any{"Name": strings.Repeat("a", maxSyncJobNameLen+1)}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for overlong Name, got %d", r.Code)
	}
	if r := doAs(alice, http.MethodPost, "/Sync/Jobs", map[string]any{"Bitrate": -10}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for negative Bitrate, got %d", r.Code)
	}
	if r := doAs(alice, http.MethodPost, "/Sync/Jobs", map[string]any{"ItemLimit": -1}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for negative ItemLimit, got %d", r.Code)
	}

	// 3. M-17: handleUpdateJob status allowlist, lower bounds, and length limits
	if r := doAs(alice, http.MethodPost, "/Sync/Jobs/"+ownJobRes.Job.Id, map[string]any{"Status": "HackedStatus"}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid Status, got %d", r.Code)
	}
	if r := doAs(alice, http.MethodPost, "/Sync/Jobs/"+ownJobRes.Job.Id, map[string]any{"Bitrate": -5}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for negative Bitrate update, got %d", r.Code)
	}
	if r := doAs(alice, http.MethodPost, "/Sync/Jobs/"+ownJobRes.Job.Id, map[string]any{"ItemLimit": -2}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for negative ItemLimit update, got %d", r.Code)
	}
	if r := doAs(alice, http.MethodPost, "/Sync/Jobs/"+ownJobRes.Job.Id, map[string]any{"Name": strings.Repeat("n", maxSyncJobNameLen+1)}); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for overlong Name update, got %d", r.Code)
	}
	if r := doAs(alice, http.MethodPost, "/Sync/Jobs/"+ownJobRes.Job.Id, map[string]any{"Status": "Queued", "Bitrate": 4000000}); r.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid job update, got %d (%s)", r.Code, r.Body.String())
	}

	// 4. M-17: handleReadyItems re-checks EnableContentDownloading dynamically
	alicePol.EnableContentDownloading = false
	_ = userRepo.UpdateUserPolicy(ctx, alice.ID, alicePol)
	if r := doAs(alice, http.MethodGet, "/Sync/Items/Ready", nil); r.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on ReadyItems after EnableContentDownloading revoked, got %d", r.Code)
	}
	alicePol.EnableContentDownloading = true
	_ = userRepo.UpdateUserPolicy(ctx, alice.ID, alicePol)

	// 5. H-14: Per-user job limit (100 jobs)
	h.mu.Lock()
	for i := len(h.state.Jobs); i < maxSyncJobsPerUser; i++ {
		h.state.Jobs = append(h.state.Jobs, SyncJob{Id: fmt.Sprintf("fill-%d", i), UserId: guid.Normalize(alice.ID)})
	}
	h.mu.Unlock()
	if r := doAs(alice, http.MethodPost, "/Sync/Jobs", map[string]any{"Name": "101st"}); r.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on 101st sync job for alice, got %d", r.Code)
	}
	// Reset to just ownJobRes for persistence rollback tests
	h.mu.Lock()
	h.state.Jobs = []SyncJob{ownJobRes.Job}
	h.state.JobItems = append([]SyncJobItem(nil), ownJobRes.JobItems...)
	h.mu.Unlock()

	// 6. M-17: saveStateLocked failures roll back in-memory state and return 500
	adminUser := &sqlite.UserRecord{ID: guid.New(), Name: "admin_v107", IsAdmin: true, Policy: sqlite.DefaultUserPolicy(true)}
	_ = st.Close()
	jiID := ownJobRes.JobItems[0].Id

	if r := doAs(adminUser, http.MethodPost, "/Sync/Jobs", map[string]any{"Name": "ShouldFail"}); r.Code != http.StatusInternalServerError {
		t.Fatalf("create job with closed DB expected 500, got %d", r.Code)
	}
	if r := doAs(adminUser, http.MethodPost, "/Sync/Jobs/"+ownJobRes.Job.Id, map[string]any{"Name": "ShouldFailUpd"}); r.Code != http.StatusInternalServerError {
		t.Fatalf("update job with closed DB expected 500, got %d", r.Code)
	}
	if r := doAs(adminUser, http.MethodPost, "/Sync/JobItems/"+jiID+"/Transferred", nil); r.Code != http.StatusInternalServerError {
		t.Fatalf("mark transferred with closed DB expected 500, got %d", r.Code)
	}
	if r := doAs(adminUser, http.MethodPost, "/Sync/JobItems/"+jiID+"/Enable", nil); r.Code != http.StatusInternalServerError {
		t.Fatalf("enable job item with closed DB expected 500, got %d", r.Code)
	}
	if r := doAs(adminUser, http.MethodDelete, "/Sync/JobItems/"+jiID, nil); r.Code != http.StatusInternalServerError {
		t.Fatalf("delete job item with closed DB expected 500, got %d", r.Code)
	}
	if r := doAs(adminUser, http.MethodDelete, "/Sync/Jobs/"+ownJobRes.Job.Id, nil); r.Code != http.StatusInternalServerError {
		t.Fatalf("delete job with closed DB expected 500, got %d", r.Code)
	}
	// Exported caller methods also roll back on persistence failure
	_, _, _ = h.SetJobItemRemovalForCaller(ctx, jiID, true, alice.ID, true)
	_, _, _ = h.DeleteJobItemForCaller(ctx, jiID, alice.ID, true)
	_, _, _ = h.DeleteJobForCaller(ctx, ownJobRes.Job.Id, alice.ID, true)
	if err := h.CancelItemsForCaller(ctx, ownJobRes.Job.TargetId, []string{ownJobRes.JobItems[0].ItemId}, alice.ID, true); err == nil {
		t.Fatal("expected CancelItemsForCaller to return error when DB closed")
	}

	h.mu.RLock()
	if len(h.state.Jobs) != 1 || len(h.state.JobItems) != 1 || h.state.JobItems[0].Status != "ReadyToTransfer" {
		h.mu.RUnlock()
		t.Fatalf("expected in-memory state rolled back after DB failures, got jobs=%+v items=%+v", h.state.Jobs, h.state.JobItems)
	}
	h.mu.RUnlock()
}

func TestEnableJobItemRBACStateTransitionsAndLibraryItemQuotas(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default(dir)
	st, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	userRepo := sqlite.NewUserRepo(st)
	libRepo := sqlite.NewLibraryRepo(st)

	srcFile := filepath.Join(dir, "source.mkv")
	if err := os.WriteFile(srcFile, []byte("VIDEO-DATA"), 0o644); err != nil {
		t.Fatal(err)
	}
	fakeFFmpeg := filepath.Join(dir, "fake-ffmpeg.sh")
	ffScript := "#!/bin/sh\nlast=\"\"\nfor a in \"$@\"; do last=\"$a\"; done\nprintf 'TRANSCODED' > \"$last\"\n"
	if err := os.WriteFile(fakeFFmpeg, []byte(ffScript), 0o755); err != nil {
		t.Fatal(err)
	}

	libAllowed, _ := libRepo.CreateLibrary(ctx, "Allowed", "movies", []string{dir})
	libSecret, _ := libRepo.CreateLibrary(ctx, "Secret", "movies", []string{dir})
	movieOK := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libAllowed.ID, ParentID: libAllowed.ID,
		Type: "Movie", MediaType: "Video", Name: "Movie OK", Path: srcFile, OfficialRating: "G",
	}
	movieExtra := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libAllowed.ID, ParentID: libAllowed.ID,
		Type: "Movie", MediaType: "Video", Name: "Movie Extra", Path: srcFile, OfficialRating: "G",
	}
	for _, it := range []*sqlite.ItemRecord{movieOK, movieExtra} {
		_ = libRepo.UpsertItem(ctx, it)
	}

	pol := sqlite.DefaultUserPolicy(false)
	pol.EnableAllFolders = false
	pol.EnabledFolders = []string{libAllowed.ID}
	pol.EnableContentDownloading = true
	alice := &sqlite.UserRecord{ID: guid.New(), Name: "alice_enable", Policy: pol, Config: sqlite.DefaultUserConfig()}
	_ = userRepo.CreateUser(ctx, alice)

	h := New(st, cfg)
	h.SetLibraryRepo(libRepo)
	h.SetUserRepo(userRepo)
	h.SetFFmpegPath(fakeFFmpeg)
	h.StartWorker(ctx)
	defer h.StopWorker()

	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	doAs := func(u *sqlite.UserRecord, method, path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		var rdr *bytes.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rdr = bytes.NewReader(b)
		} else {
			rdr = bytes.NewReader(nil)
		}
		req := httptest.NewRequest(method, path, rdr)
		if u != nil {
			req = req.WithContext(middleware.WithAuthContext(req.Context(), &middleware.AuthContext{User: u}))
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// Create a transcoding job for Alice and seed item states to test RBAC + state transitions.
	h.mu.Lock()
	jobID := guid.New()
	h.state.Jobs = append(h.state.Jobs, SyncJob{
		Id:             jobID,
		UserId:         guid.Normalize(alice.ID),
		TargetId:       "dev-alice",
		Name:           "Transcode Sync",
		Quality:        "medium",
		Status:         "Failed",
		SyncNewContent: true,
		ParentId:       libAllowed.ID,
	})
	convertingID := guid.New()
	failedNeedTCID := guid.New()
	cachedFile := filepath.Join(dir, "existing-out.mp4")
	_ = os.WriteFile(cachedFile, []byte("CACHED-MP4"), 0o644)
	disabledHasFileID := guid.New()
	h.state.JobItems = append(h.state.JobItems,
		SyncJobItem{
			Id: convertingID, JobId: jobID, ItemId: movieOK.ID, TargetId: "dev-alice",
			Status: "Converting", Progress: 67.5, SourcePath: srcFile,
		},
		SyncJobItem{
			Id: failedNeedTCID, JobId: jobID, ItemId: movieOK.ID, TargetId: "dev-alice",
			Status: "Failed", Progress: 12, SourcePath: srcFile, OutputPath: "",
		},
		SyncJobItem{
			Id: disabledHasFileID, JobId: jobID, ItemId: movieOK.ID, TargetId: "dev-alice",
			Status: "Disabled", Progress: 0, SourcePath: srcFile, OutputPath: cachedFile,
		},
	)
	h.mu.Unlock()

	// 1. Revoking EnableContentDownloading -> 403 on POST /Sync/JobItems/{Id}/Enable
	noDL := pol
	noDL.EnableContentDownloading = false
	_ = userRepo.UpdateUserPolicy(ctx, alice.ID, noDL)
	if r := doAs(alice, http.MethodPost, "/Sync/JobItems/"+failedNeedTCID+"/Enable", nil); r.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when EnableContentDownloading=false, got %d", r.Code)
	}

	// 2. Disabling user -> 403 on POST /Sync/JobItems/{Id}/Enable
	disPol := pol
	disPol.IsDisabled = true
	_ = userRepo.UpdateUserPolicy(ctx, alice.ID, disPol)
	if r := doAs(alice, http.MethodPost, "/Sync/JobItems/"+failedNeedTCID+"/Enable", nil); r.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when caller user is disabled, got %d", r.Code)
	}

	// 3. Revoking library access to item -> 403 on POST /Sync/JobItems/{Id}/Enable
	secretOnlyPol := pol
	secretOnlyPol.EnabledFolders = []string{libSecret.ID}
	_ = userRepo.UpdateUserPolicy(ctx, alice.ID, secretOnlyPol)
	if r := doAs(alice, http.MethodPost, "/Sync/JobItems/"+failedNeedTCID+"/Enable", nil); r.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when caller lost library access to item, got %d", r.Code)
	}

	// Restore Alice's valid policy.
	_ = userRepo.UpdateUserPolicy(ctx, alice.ID, pol)

	// 4. Enabling an active "Converting" item MUST preserve Status="Converting" and Progress=67.5!
	if r := doAs(alice, http.MethodPost, "/Sync/JobItems/"+convertingID+"/Enable", nil); r.Code != http.StatusNoContent {
		t.Fatalf("enable converting item got %d", r.Code)
	}
	if it, _, _ := h.GetJobItemForCaller(convertingID, alice.ID, true); it == nil || it.Status != "Converting" || it.Progress != 67.5 {
		t.Fatalf("expected Converting/67.5 preserved, got %+v", it)
	}

	// 5. Enabling a "Disabled" item whose OutputPath already exists on disk transitions directly to "ReadyToTransfer".
	if r := doAs(alice, http.MethodPost, "/Sync/JobItems/"+disabledHasFileID+"/Enable", nil); r.Code != http.StatusNoContent {
		t.Fatalf("enable cached item got %d", r.Code)
	}
	if it, _, _ := h.GetJobItemForCaller(disabledHasFileID, alice.ID, true); it == nil || it.Status != "ReadyToTransfer" {
		t.Fatalf("expected ReadyToTransfer when OutputPath exists on disk, got %+v", it)
	}

	// 6. Enabling a "Failed" item with OutputPath=="" on a transcode job queues and wakes worker -> settles at ReadyToTransfer!
	if r := doAs(alice, http.MethodPost, "/Sync/JobItems/"+failedNeedTCID+"/Enable", nil); r.Code != http.StatusNoContent {
		t.Fatalf("enable failed item got %d", r.Code)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		it, _, _ := h.GetJobItemForCaller(failedNeedTCID, alice.ID, true)
		if it != nil && it.Status == "ReadyToTransfer" && it.OutputPath != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if it, _, _ := h.GetJobItemForCaller(failedNeedTCID, alice.ID, true); it == nil || it.Status != "ReadyToTransfer" || it.OutputPath == "" {
		t.Fatalf("expected re-enabled Failed item to be Queued and transcoded to ReadyToTransfer, got %+v", it)
	}

	// 7. OnLibraryItemAdded enforces per-user SyncJobItem quota (maxSyncJobItemsPerUser) and per-job cap (maxSyncJobItemIDs).
	fillerJobID := guid.New()
	h.mu.Lock()
	h.state.Jobs = append(h.state.Jobs, SyncJob{
		Id:     fillerJobID,
		UserId: guid.Normalize(alice.ID),
		Name:   "Quota Filler",
		Status: "ReadyToTransfer",
	})
	for len(h.state.JobItems) < maxSyncJobItemsPerUser {
		h.state.JobItems = append(h.state.JobItems, SyncJobItem{
			Id:     fmt.Sprintf("fill-ji-%d", len(h.state.JobItems)),
			JobId:  fillerJobID,
			ItemId: fmt.Sprintf("fill-item-%d", len(h.state.JobItems)),
			Status: "Synced",
		})
	}
	beforeCount := len(h.state.JobItems)
	h.mu.Unlock()

	h.OnLibraryItemAdded(ctx, movieExtra.ID)

	h.mu.RLock()
	afterCount := len(h.state.JobItems)
	h.mu.RUnlock()
	if afterCount != beforeCount {
		t.Fatalf("expected OnLibraryItemAdded to enforce maxSyncJobItemsPerUser (%d), got %d -> %d",
			maxSyncJobItemsPerUser, beforeCount, afterCount)
	}
}
