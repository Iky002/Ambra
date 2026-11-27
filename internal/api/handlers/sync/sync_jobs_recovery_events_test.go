package sync

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

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestV0110_SyncWorkerPureHelpersAndEdgeBranches(t *testing.T) {
	// 1. needsSyncTranscode all quality tiers & numeric/custom fallbacks
	cases := []struct {
		quality string
		bitrate int
		wantTC  bool
		wantBR  int
	}{
		{"", 0, false, 0},
		{"original", 0, false, 0},
		{"original", 2_500_000, true, 2_500_000},
		{"high", 0, true, 8_000_000},
		{"medium", 0, true, 4_000_000},
		{"low", 0, true, 1_500_000},
		{"3200000", 0, true, 3_200_000},
		{"custom-profile", 0, true, 4_000_000},
	}
	for _, tc := range cases {
		gotTC, gotBR := needsSyncTranscode(tc.quality, tc.bitrate)
		if gotTC != tc.wantTC || gotBR != tc.wantBR {
			t.Fatalf("needsSyncTranscode(%q, %d) = (%v, %d), want (%v, %d)",
				tc.quality, tc.bitrate, gotTC, gotBR, tc.wantTC, tc.wantBR)
		}
	}

	// 2. buildSyncFFmpegArgs for Audio (normal & >320k cap) + HTTP Video + low bitrate floor
	audioArgs := strings.Join(buildSyncFFmpegArgs("/media/song.flac", "/tmp/out.mp4", "Audio", 500_000), " ")
	if !strings.Contains(audioArgs, "-vn -c:a aac -b:a 192000") || !strings.Contains(audioArgs, "-protocol_whitelist file,crypto,data") {
		t.Fatalf("unexpected audio sync args: %s", audioArgs)
	}
	httpVideoArgs := strings.Join(buildSyncFFmpegArgs("http://127.0.0.1:9999/stream.mp4", "/tmp/v.mp4", "Video", 200_000), " ")
	if !strings.Contains(httpVideoArgs, "-protocol_whitelist http,https,tcp,tls,crypto") || !strings.Contains(httpVideoArgs, "-b:v 250000") {
		t.Fatalf("unexpected http video sync args: %s", httpVideoArgs)
	}

	// 3. extractSyncEventItemIDs payload variants
	if got := extractSyncEventItemIDs("  item-single  "); len(got) != 1 || got[0] != "item-single" {
		t.Fatalf("unexpected string payload ids: %v", got)
	}
	if got := extractSyncEventItemIDs([]string{"a", "b"}); len(got) != 2 {
		t.Fatalf("unexpected []string payload ids: %v", got)
	}
	mapIDs := extractSyncEventItemIDs(map[string]any{
		"item_id":      "id1",
		"Id":           "id2",
		"AddedItemIds": []string{"id3"},
		"ItemIds":      []any{"id4", 123},
	})
	if len(mapIDs) != 4 {
		t.Fatalf("unexpected map payload ids: %v", mapIDs)
	}
	if extractSyncEventItemIDs(42) != nil {
		t.Fatal("expected nil for unsupported payload type")
	}

	// 4. Handler with nil cfg / inactive worker safety checks
	hNil := New(nil, nil)
	if hNil.syncCacheDir() != "" {
		t.Fatal("expected empty syncCacheDir when cfg is nil")
	}
	if _, ok := hNil.resolveSafeSyncCacheTarget("/tmp/anything"); ok {
		t.Fatal("expected false resolveSafeSyncCacheTarget when cfg is nil")
	}
	hNil.cleanupSyncJobCacheDir("..")
	hNil.cleanupSyncJobCacheDir("bad/slash")
	hNil.StopWorker() // no-op when not running

	cancelled := false
	hNil.registerActiveCancel("ji-inactive", func() { cancelled = true })
	if !cancelled {
		t.Fatal("expected registerActiveCancel to invoke cancel immediately when worker is not running")
	}
}

func TestV0110_SyncWorkerRecoveryFailuresStrmResolverAndCancellation(t *testing.T) {
	h, mux, st, cfg := setupSyncTest(t)
	ctx := context.Background()
	libRepo := sqlite.NewLibraryRepo(st)
	userRepo := sqlite.NewUserRepo(st)
	h.SetLibraryRepo(libRepo)
	h.SetUserRepo(userRepo)

	mediaDir := t.TempDir()
	srcOK := filepath.Join(mediaDir, "ok.mkv")
	srcFail := filepath.Join(mediaDir, "fail.mkv")
	srcEmptyOut := filepath.Join(mediaDir, "emptyout.mkv")
	srcSlow := filepath.Join(mediaDir, "slow.mkv")
	srcStrm := filepath.Join(mediaDir, "remote.strm")
	for _, p := range []string{srcOK, srcFail, srcEmptyOut, srcSlow, srcStrm} {
		_ = os.WriteFile(p, []byte("MEDIA"), 0o644)
	}

	// Upstream HTTP server for resolved .strm source
	var gotUpstreamAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUpstreamAuth = r.Header.Get("X-Test-Auth")
		w.Header().Set("Content-Type", "video/mp4")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("UPSTREAM-VIDEO-STREAM"))
	}))
	defer upstream.Close()

	h.SetStreamResolverWithHeaders(func(_ context.Context, rawPath string) (*plugins.StreamResolveResult, error) {
		if rawPath == srcStrm {
			return &plugins.StreamResolveResult{
				URL:     upstream.URL + "/video.mp4",
				Headers: map[string]string{"X-Test-Auth": "token-strm-123"},
			}, nil
		}
		return nil, nil
	})

	// Fake FFmpeg handling fail / no-output / slow / HTTP-fetch / normal modes based on input path or job item output name
	fakeFFmpeg := filepath.Join(t.TempDir(), "fake-sync-ffmpeg.sh")
	ffScript := `#!/bin/sh
input=""
prev=""
last=""
for arg in "$@"; do
  if [ "$prev" = "-i" ]; then input="$arg"; fi
  prev="$arg"
  last="$arg"
done
case "$input" in
  *fail.mkv*)
    echo "simulated ffmpeg encode error" >&2
    exit 1
    ;;
  *emptyout.mkv*)
    exit 0
    ;;
  *slow.mkv*)
    exec sleep 10
    ;;
  http://*)
    curl -sf "$input" >/dev/null 2>&1 || true
    printf 'HTTP_PROXY_TRANSCODED' > "$last"
    exit 0
    ;;
  *)
    printf 'LOCAL_TRANSCODED' > "$last"
    exit 0
    ;;
esac
`
	if err := os.WriteFile(fakeFFmpeg, []byte(ffScript), 0o755); err != nil {
		t.Fatal(err)
	}
	h.SetFFmpegPath(fakeFFmpeg)

	// Seed library item for SourcePath="" lookup in nextQueuedWork
	lib, _ := libRepo.CreateLibrary(ctx, "Movies", "movies", []string{mediaDir})
	dbMovie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "DB Lookup Movie", Path: srcOK,
	}
	_ = libRepo.UpsertItem(ctx, dbMovie)

	// Pre-populate state BEFORE StartWorker:
	// - ji-interrupted: "Converting" -> StartWorker must recover to "Queued" and process to "ReadyToTransfer"!
	// - ji-virtual: SourcePath="" & no ItemId -> virtual item transitions directly to "ReadyToTransfer"!
	// - ji-fail: input fail.mkv -> transitions to "Failed" via markSyncItemFailed!
	// - ji-noout: input emptyout.mkv -> transitions to "Failed" (produced no output file)!
	// - ji-strm: input remote.strm -> resolved to upstream HTTP URL via LoopbackStreamProxy -> "ReadyToTransfer"!
	h.mu.Lock()
	h.state.Jobs = []SyncJob{
		{Id: "job-rec", Name: "Recovery Job", Quality: "medium", Status: "Converting", UserId: "user-1", TargetId: "dev1"},
		{Id: "job-fail", Name: "Fail Job", Quality: "low", Status: "Queued", UserId: "user-1", TargetId: "dev1"},
		{Id: "job-cancelled", Name: "Cancelled Job", Quality: "high", Status: "Cancelled", UserId: "user-1", TargetId: "dev1"},
	}
	h.state.JobItems = []SyncJobItem{
		{Id: "ji-interrupted", JobId: "job-rec", ItemId: dbMovie.ID, SourcePath: "", Status: "Converting", Progress: 45, TargetId: "dev1"},
		{Id: "ji-virtual", JobId: "job-rec", ItemId: "", SourcePath: "", Status: "Queued", TargetId: "dev1"},
		{Id: "ji-strm", JobId: "job-rec", ItemId: "i-strm", SourcePath: srcStrm, MediaType: "Video", Status: "Queued", TargetId: "dev1"},
		{Id: "ji-fail", JobId: "job-fail", ItemId: "i-fail", SourcePath: srcFail, MediaType: "Video", Status: "Queued", TargetId: "dev1"},
		{Id: "ji-noout", JobId: "job-fail", ItemId: "i-noout", SourcePath: srcEmptyOut, MediaType: "Audio", Status: "Queued", TargetId: "dev1"},
		{Id: "ji-skip-cancel", JobId: "job-cancelled", ItemId: "i-c", SourcePath: srcOK, Status: "Queued", TargetId: "dev1"},
	}
	h.mu.Unlock()

	h.StartWorker(nil)
	h.StartWorker(ctx) // duplicate call is no-op

	// Wait until all active items settle
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		it1, _, _ := h.GetJobItemForCaller("ji-interrupted", "user-1", true)
		it2, _, _ := h.GetJobItemForCaller("ji-virtual", "user-1", true)
		it3, _, _ := h.GetJobItemForCaller("ji-strm", "user-1", true)
		it4, _, _ := h.GetJobItemForCaller("ji-fail", "user-1", true)
		it5, _, _ := h.GetJobItemForCaller("ji-noout", "user-1", true)
		if it1 != nil && it1.Status == "ReadyToTransfer" &&
			it2 != nil && it2.Status == "ReadyToTransfer" &&
			it3 != nil && it3.Status == "ReadyToTransfer" &&
			it4 != nil && it4.Status == "Failed" &&
			it5 != nil && it5.Status == "Failed" {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}

	it1, _, _ := h.GetJobItemForCaller("ji-interrupted", "user-1", true)
	it3, _, _ := h.GetJobItemForCaller("ji-strm", "user-1", true)
	it4, _, _ := h.GetJobItemForCaller("ji-fail", "user-1", true)
	if it1.Status != "ReadyToTransfer" || it3.Status != "ReadyToTransfer" || it4.Status != "Failed" {
		t.Fatalf("unexpected settled statuses: it1=%+v it3=%+v it4=%+v", it1, it3, it4)
	}
	if gotUpstreamAuth != "" && gotUpstreamAuth != "token-strm-123" {
		t.Fatalf("unexpected upstream auth header: %q", gotUpstreamAuth)
	}

	// Test smooth fallback when FFmpeg becomes unavailable at runtime
	h.SetFFmpegPath(filepath.Join(t.TempDir(), "missing-ffmpeg"))
	cfg.FFmpegPath = filepath.Join(t.TempDir(), "missing-ffmpeg-2")
	h.mu.Lock()
	h.state.JobItems = append(h.state.JobItems, SyncJobItem{
		Id: "ji-noff", JobId: "job-rec", ItemId: "i-noff", SourcePath: srcOK, Status: "Queued", TargetId: "dev1",
	})
	h.mu.Unlock()
	h.wakeWorker()

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		it, _, _ := h.GetJobItemForCaller("ji-noff", "user-1", true)
		if it != nil && it.Status == "ReadyToTransfer" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if it, _, _ := h.GetJobItemForCaller("ji-noff", "user-1", true); it == nil || it.Status != "ReadyToTransfer" || it.OutputPath != srcOK {
		t.Fatalf("expected runtime missing-ffmpeg fallback to ReadyToTransfer with srcPath, got %+v", it)
	}

	// Restore fakeFFmpeg, enqueue a slow item, wait until Converting, then cancel via StopWorker!
	h.SetFFmpegPath(fakeFFmpeg)
	h.mu.Lock()
	h.state.JobItems = append(h.state.JobItems, SyncJobItem{
		Id: "ji-slow", JobId: "job-rec", ItemId: "i-slow", SourcePath: srcSlow, Status: "Queued", TargetId: "dev1",
	})
	h.mu.Unlock()
	h.wakeWorker()

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		it, _, _ := h.GetJobItemForCaller("ji-slow", "user-1", true)
		if it != nil && it.Status == "Converting" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	stopStart := time.Now()
	h.StopWorker()
	if time.Since(stopStart) > 3*time.Second {
		t.Fatalf("StopWorker did not cancel in-flight slow ffmpeg promptly: %v", time.Since(stopStart))
	}
	_ = mux
}

func TestV0110_SyncRBACContainersTargetsAndLibraryEvents(t *testing.T) {
	h, mux, st, _ := setupSyncTest(t)
	ctx := context.Background()
	libRepo := sqlite.NewLibraryRepo(st)
	userRepo := sqlite.NewUserRepo(st)
	h.SetLibraryRepo(libRepo)
	h.SetUserRepo(userRepo)

	mediaDir := t.TempDir()
	libAllowed, _ := libRepo.CreateLibrary(ctx, "AllowedMovies", "movies", []string{mediaDir})
	libDenied, _ := libRepo.CreateLibrary(ctx, "DeniedMovies", "movies", []string{t.TempDir()})

	// Create restricted user with MaxParentalRating, BlockUnratedItems, AllowedTags, EnabledFolders
	maxPG := 5
	pol := sqlite.DefaultUserPolicy(false)
	pol.EnableContentDownloading = true
	pol.EnableAllFolders = false
	pol.EnabledFolders = []string{libAllowed.ID}
	pol.MaxParentalRating = &maxPG
	pol.BlockUnratedItems = []string{"Movie"}
	pol.AllowedTags = []string{"syncok"}

	alice := &sqlite.UserRecord{
		ID:     guid.New(),
		Name:   "alice_sync_v110",
		Policy: pol,
		Config: sqlite.DefaultUserConfig(),
	}
	bob := &sqlite.UserRecord{
		ID:     guid.New(),
		Name:   "bob_sync_v110",
		Policy: sqlite.DefaultUserPolicy(false),
		Config: sqlite.DefaultUserConfig(),
	}
	_ = userRepo.CreateUser(ctx, alice)
	_ = userRepo.CreateUser(ctx, bob)

	// Register devices for alice and bob via access tokens to exercise resolveTargets
	_ = userRepo.CreateAccessToken(ctx, &sqlite.AccessTokenRecord{
		Token: "tok-alice-v110", UserID: alice.ID, DeviceID: "dev-alice-1", DeviceName: "", AppName: "Ambra iOS", AppVersion: "1.0",
	})
	_ = userRepo.CreateAccessToken(ctx, &sqlite.AccessTokenRecord{
		Token: "tok-bob-v110", UserID: bob.ID, DeviceID: "dev-bob-1", DeviceName: "Bob TV", AppName: "Ambra TV", AppVersion: "1.0",
	})

	f1 := filepath.Join(mediaDir, "m1.mp4")
	f2 := filepath.Join(mediaDir, "m2.mp4")
	_ = os.WriteFile(f1, []byte("M1"), 0o644)
	_ = os.WriteFile(f2, []byte("M2"), 0o644)

	folderRec := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libAllowed.ID, ParentID: libAllowed.ID,
		Type: "Folder", IsFolder: true, Name: "SubFolder", OfficialRating: "G",
	}
	movieAllowed := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libAllowed.ID, ParentID: folderRec.ID,
		Type: "Movie", MediaType: "Video", Name: "Movie Allowed", OfficialRating: "G", Path: f1,
	}
	movieUnrated := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libAllowed.ID, ParentID: folderRec.ID,
		Type: "Movie", MediaType: "Video", Name: "Movie Unrated", Path: f2,
	}
	movieWrongTag := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libAllowed.ID, ParentID: folderRec.ID,
		Type: "Movie", MediaType: "Video", Name: "Movie WrongTag", OfficialRating: "G", Path: f2,
	}
	for _, r := range []*sqlite.ItemRecord{folderRec, movieAllowed, movieUnrated, movieWrongTag} {
		_ = libRepo.UpsertItem(ctx, r)
	}
	_ = libRepo.ReplaceItemAncestors(ctx, folderRec.ID, []sqlite.AncestorLink{{AncestorID: libAllowed.ID, Depth: 1}})
	for _, child := range []*sqlite.ItemRecord{movieAllowed, movieUnrated, movieWrongTag} {
		_ = libRepo.ReplaceItemAncestors(ctx, child.ID, []sqlite.AncestorLink{
			{AncestorID: folderRec.ID, Depth: 1},
			{AncestorID: libAllowed.ID, Depth: 2},
		})
	}
	_ = libRepo.ReplaceItemTaxonomies(ctx, folderRec.ID, "tag", []string{"SyncOK"})
	_ = libRepo.ReplaceItemTaxonomies(ctx, movieAllowed.ID, "tag", []string{"SyncOK"})
	_ = libRepo.ReplaceItemTaxonomies(ctx, movieUnrated.ID, "tag", []string{"SyncOK"})
	_ = libRepo.ReplaceItemTaxonomies(ctx, movieWrongTag.ID, "tag", []string{"OtherTag"})

	// Public vs Private Playlist owned by Bob
	bobPrivPL, _ := libRepo.CreatePlaylist(ctx, "Bob Private", "Video", bob.ID, []string{movieAllowed.ID})
	bobPubPL, _ := libRepo.CreatePlaylist(ctx, "Bob Public", "Video", bob.ID, []string{movieAllowed.ID})
	_ = libRepo.ReplaceItemTaxonomies(ctx, bobPubPL.ID, "tag", []string{"SyncOK"})
	_, _ = st.Writer.ExecContext(ctx, `INSERT INTO server_config (key, value_json, updated_at) VALUES (?, '{"IsPublic":true}', 1)`, "item_visibility_"+guid.Normalize(bobPubPL.ID))

	aliceAC := &middleware.AuthContext{User: alice}
	bobAC := &middleware.AuthContext{User: bob}

	// 1. Targets resolves Alice's device (falling back to AppName when Name=="") and excludes Bob's device
	recTargets := doSyncV109AuthReq(t, mux, aliceAC, http.MethodGet, "/Sync/Targets", nil)
	if recTargets.Code != http.StatusOK {
		t.Fatalf("GET /Sync/Targets status=%d", recTargets.Code)
	}
	var targets []SyncTarget
	_ = json.Unmarshal(recTargets.Body.Bytes(), &targets)
	if len(targets) != 1 || targets[0].Id != "dev-alice-1" || targets[0].Name != "Ambra iOS" {
		t.Fatalf("unexpected targets for alice: %+v", targets)
	}

	// 2. CreateJob RBAC rejections: unrated item (403), wrong tag item (403), private playlist (403), denied library ParentId (403)
	for _, badBody := range []map[string]any{
		{"ItemIds": []string{movieUnrated.ID}},
		{"ItemIds": []string{movieWrongTag.ID}},
		{"ItemIds": []string{bobPrivPL.ID}},
		{"ParentId": libDenied.ID},
	} {
		if r := doSyncV109AuthReq(t, mux, aliceAC, http.MethodPost, "/Sync/Jobs", badBody); r.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for %+v, got %d (%s)", badBody, r.Code, r.Body.String())
		}
	}

	// 3. CreateJob with public playlist & ParentId=libAllowed.ID + SyncNewContent=true
	recPub := doSyncV109AuthReq(t, mux, aliceAC, http.MethodPost, "/Sync/Jobs", map[string]any{
		"Name":           "Lib Sync Job",
		"TargetId":       "dev-alice-1",
		"ParentId":       libAllowed.ID,
		"SyncNewContent": true,
	})
	if recPub.Code != http.StatusOK {
		t.Fatalf("POST /Sync/Jobs library ParentId failed: %d %s", recPub.Code, recPub.Body.String())
	}
	var libJob SyncJobCreationResult
	_ = json.Unmarshal(recPub.Body.Bytes(), &libJob)
	if len(libJob.JobItems) != 1 || guid.Normalize(libJob.JobItems[0].ItemId) != guid.Normalize(movieAllowed.ID) {
		t.Fatalf("expected only movieAllowed expanded from library, got %+v", libJob.JobItems)
	}

	// 4. CreateJob with Folder container & UnwatchedOnly=true when movieAllowed is already watched -> Completed (100%)
	_, _ = libRepo.MarkPlayed(ctx, alice.ID, movieAllowed.ID)
	recDone := doSyncV109AuthReq(t, mux, aliceAC, http.MethodPost, "/Sync/Jobs", map[string]any{
		"Name":          "Watched Folder",
		"ParentId":      folderRec.ID,
		"UnwatchedOnly": true,
	})
	if recDone.Code != http.StatusOK {
		t.Fatalf("POST /Sync/Jobs watched folder failed: %d %s", recDone.Code, recDone.Body.String())
	}
	var doneJob SyncJobCreationResult
	_ = json.Unmarshal(recDone.Body.Bytes(), &doneJob)
	if doneJob.Job.Status != "Completed" || doneJob.Job.Progress != 100 {
		t.Fatalf("expected Completed 100%% when all matched items are already watched, got %+v", doneJob.Job)
	}

	// 5. ListJobItems with JobId / Statuses / cross-user 403 + ReadyItems + EnableJobItem
	if r := doSyncV109AuthReq(t, mux, bobAC, http.MethodGet, "/Sync/JobItems?JobId="+libJob.Job.Id, nil); r.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when Bob lists Alice's JobId items, got %d", r.Code)
	}
	recList := doSyncV109AuthReq(t, mux, aliceAC, http.MethodGet, "/Sync/JobItems?JobId="+libJob.Job.Id+"&TargetId=dev-alice-1&Statuses=ReadyToTransfer", nil)
	var jiRes dto.QueryResult[SyncJobItem]
	_ = json.Unmarshal(recList.Body.Bytes(), &jiRes)
	if jiRes.TotalRecordCount != 1 {
		t.Fatalf("expected 1 job item in list, got %+v", jiRes)
	}
	if r := doSyncV109AuthReq(t, mux, aliceAC, http.MethodGet, "/Sync/Items/Ready?TargetId=dev-alice-1", nil); r.Code != http.StatusOK {
		t.Fatalf("GET /Sync/Items/Ready failed: %d", r.Code)
	}
	if r := doSyncV109AuthReq(t, mux, aliceAC, http.MethodPost, "/Sync/JobItems/"+libJob.JobItems[0].Id+"/Enable", nil); r.Code != http.StatusNoContent {
		t.Fatalf("POST Enable failed: %d", r.Code)
	}

	// 6. OnLibraryItemAdded for library-scoped SyncNewContent job + explicit hint job (ParentId=="" && RequestedItemIds empty)
	recHintJob := doSyncV109AuthReq(t, mux, aliceAC, http.MethodPost, "/Sync/Jobs", map[string]any{
		"Name":           "Explicit Hint Sync",
		"TargetId":       "dev-alice-1",
		"SyncNewContent": true,
	})
	var hintJob SyncJobCreationResult
	_ = json.Unmarshal(recHintJob.Body.Bytes(), &hintJob)

	newMovie := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libAllowed.ID, ParentID: libAllowed.ID,
		Type: "Movie", MediaType: "Video", Name: "Brand New Movie", OfficialRating: "PG", Path: f1,
	}
	_ = libRepo.UpsertItem(ctx, newMovie)
	_ = libRepo.ReplaceItemTaxonomies(ctx, newMovie.ID, "tag", []string{"SyncOK"})

	h.OnLibraryItemAdded(ctx, newMovie.ID)
	if _, ok := h.GetItemStatusForCaller(newMovie.ID, "dev-alice-1", alice.ID, false); !ok {
		t.Fatal("expected OnLibraryItemAdded to append newMovie to SyncNewContent jobs")
	}

	// 7. resolveCallerUserForSync fallback when userRepo is nil
	h.SetUserRepo(nil)
	if r := doSyncV109AuthReq(t, mux, aliceAC, http.MethodGet, "/Sync/Items/Ready", nil); r.Code != http.StatusOK {
		t.Fatalf("expected ReadyItems 200 with ac.User fallback, got %d", r.Code)
	}
	disabledUser := *alice
	disabledUser.IsDisabled = true
	if r := doSyncV109AuthReq(t, mux, &middleware.AuthContext{User: &disabledUser}, http.MethodGet, "/Sync/Items/Ready", nil); r.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for disabled user in ReadyItems fallback, got %d", r.Code)
	}
}
