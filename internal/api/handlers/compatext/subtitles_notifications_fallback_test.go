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

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestV0110_SubtitleHelpersAndNotificationOptionMerge(t *testing.T) {
	if got := subtitleContentTypeForExt(".vtt"); got != "text/vtt; charset=utf-8" {
		t.Fatalf("unexpected vtt content-type: %q", got)
	}
	if got := subtitleContentTypeForExt("srt"); got != "application/x-subrip; charset=utf-8" {
		t.Fatalf("unexpected srt content-type: %q", got)
	}
	if got := subtitleContentTypeForExt(".ass"); got != "text/x-ssa; charset=utf-8" {
		t.Fatalf("unexpected ass content-type: %q", got)
	}
	if got := subtitleContentTypeForExt(".bin"); got != "application/octet-stream" {
		t.Fatalf("unexpected default content-type: %q", got)
	}

	vttAlready := convertSRTBytesToWebVTT([]byte("\uFEFFWEBVTT\r\n\r\n00:00:01.000 --> 00:00:02.000\r\nHi\r\n"))
	if !strings.HasPrefix(string(vttAlready), "WEBVTT\n") {
		t.Fatalf("unexpected WEBVTT passthrough: %q", string(vttAlready))
	}

	if mergeNotificationOptions(map[string]string{"a": "1"}, nil) != nil {
		t.Fatal("expected nil when incoming options map is nil")
	}
	merged := mergeNotificationOptions(
		map[string]string{"api_key": "real-secret", "webhook_url": "https://old.hook", "room": "lobby"},
		map[string]string{"api_key": maskedOptionValue, "webhook_url": "https://new.hook", "pin": maskedOptionValue, "room": "ops"},
	)
	if merged["api_key"] != "real-secret" || merged["webhook_url"] != "https://new.hook" || merged["room"] != "ops" {
		t.Fatalf("unexpected merged notification options: %+v", merged)
	}
	if _, exists := merged["pin"]; exists {
		t.Fatalf("did not expect empty previous masked pin to be inserted: %+v", merged)
	}
}

func TestV0110_SharedSyncSubtitlesOfflineActionsNotificationsAndLiveTVFallback(t *testing.T) {
	tmp := t.TempDir()
	libDir := filepath.Join(tmp, "library")
	recDir := filepath.Join(tmp, "recordings")
	subCacheDir := filepath.Join(tmp, "cache", "subtitles")
	syncCacheDir := filepath.Join(tmp, "cache", "sync")
	for _, d := range []string{libDir, recDir, subCacheDir, syncCacheDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	store, err := sqlite.Open(filepath.Join(tmp, "ce_v0110.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	cfg := config.Default(tmp)

	// Fake FFmpeg that handles both:
	// 1. Embedded subtitle extraction (writes ASS/SRT/VTT text to last arg)
	// 2. Dynamic fMP4 HLS recording segments (writes init_gen_N.mp4 and seg_0.mp4)
	fakeFFmpeg := filepath.Join(tmp, "fake-ce-ffmpeg.sh")
	script := `#!/bin/sh
initname=""
segpat=""
prev=""
last=""
for arg in "$@"; do
  if [ "$prev" = "-hls_fmp4_init_filename" ]; then initname="$arg"; fi
  if [ "$prev" = "-hls_segment_filename" ]; then segpat="$arg"; fi
  prev="$arg"
  last="$arg"
done
if [ -n "$segpat" ]; then
  dir=$(dirname "$segpat")
  if [ -n "$initname" ]; then
    printf 'FMP4-INIT-BYTES' > "$dir/$initname"
  fi
  seg0=$(echo "$segpat" | sed "s/%d/0/")
  printf 'FMP4-SEG0-BYTES' > "$seg0"
  exit 0
fi
printf 'Dialogue: 0,0:00:01.00,0:00:02.00,Default,,0,0,0,,ExtractedSub' > "$last"
exit 0
`
	if err := os.WriteFile(fakeFFmpeg, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg.FFmpegPath = fakeFFmpeg

	tcMgr := transcode.NewManager(cfg, transcode.WithCapabilities(transcode.SoftwareOnlyCapabilities()))
	defer tcMgr.Close()

	userRepo := sqlite.NewUserRepo(store)
	libRepo := sqlite.NewLibraryRepo(store)
	authSvc := auth.NewService(userRepo, cfg)

	alice, _ := authSvc.CreateUser(ctx, "alice_ce110", "pass1234", false)
	bob, _ := authSvc.CreateUser(ctx, "bob_ce110", "pass1234", false)
	_, _ = authSvc.CreateUser(ctx, "admin_ce110", "pass1234", true)

	// Configure Alice with MaxParentalRating, BlockUnratedItems, AllowedTags, EnabledFolders, EnableLiveTvManagement
	libAllowed, _ := libRepo.CreateLibrary(ctx, "AllowedLib", "movies", []string{libDir})
	libDenied, _ := libRepo.CreateLibrary(ctx, "DeniedLib", "movies", []string{t.TempDir()})

	maxPG := 5
	alice.Policy.EnableAllFolders = false
	alice.Policy.EnabledFolders = []string{libAllowed.ID}
	alice.Policy.MaxParentalRating = &maxPG
	alice.Policy.BlockUnratedItems = []string{"Movie"}
	alice.Policy.AllowedTags = []string{"ceok"}
	alice.Policy.EnableContentDownloading = true
	alice.Policy.EnableLiveTvAccess = true
	alice.Policy.EnableLiveTvManagement = true
	_ = userRepo.UpdateUserPolicy(ctx, alice.ID, alice.Policy)

	aliceAuth, _ := authSvc.AuthenticateByName(ctx, "alice_ce110", "pass1234", "", auth.ClientInfo{DeviceID: "d-a110"})
	bobAuth, _ := authSvc.AuthenticateByName(ctx, "bob_ce110", "pass1234", "", auth.ClientInfo{DeviceID: "d-b110"})
	adminAuth, _ := authSvc.AuthenticateByName(ctx, "admin_ce110", "pass1234", "", auth.ClientInfo{DeviceID: "d-adm110"})

	// Create media files & subtitles (one external in subtitle cache dir, one embedded)
	moviePath := filepath.Join(libDir, "movie.mkv")
	_ = os.WriteFile(moviePath, []byte("MKV-CONTENT"), 0o644)
	cachedSubAss := filepath.Join(subCacheDir, "cached_en.ass")
	_ = os.WriteFile(cachedSubAss, []byte("[Script Info]\nTitle: Cached ASS\n"), 0o644)

	movieOK := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libAllowed.ID, ParentID: libAllowed.ID,
		Type: "Movie", MediaType: "Video", Name: "Movie OK", OfficialRating: "G", Path: moviePath,
	}
	movieUnrated := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libAllowed.ID, ParentID: libAllowed.ID,
		Type: "Movie", MediaType: "Video", Name: "Movie Unrated", Path: moviePath,
	}
	movieDeniedLib := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libDenied.ID, ParentID: libDenied.ID,
		Type: "Movie", MediaType: "Video", Name: "Movie Denied", OfficialRating: "G", Path: moviePath,
	}
	for _, it := range []*sqlite.ItemRecord{movieOK, movieUnrated, movieDeniedLib} {
		_ = libRepo.UpsertItem(ctx, it)
	}
	_ = libRepo.ReplaceItemTaxonomies(ctx, movieOK.ID, "tag", []string{"ceok"})
	_ = libRepo.ReplaceItemTaxonomies(ctx, movieUnrated.ID, "tag", []string{"ceok"})

	// Private vs Public Playlist for canAccessItemID check
	privPL, _ := libRepo.CreatePlaylist(ctx, "Bob Priv", "Video", bob.ID, nil)
	pubPL, _ := libRepo.CreatePlaylist(ctx, "Bob Pub", "Video", bob.ID, nil)
	_ = libRepo.ReplaceItemTaxonomies(ctx, pubPL.ID, "tag", []string{"ceok"})
	_, _ = store.Writer.ExecContext(ctx, `INSERT INTO server_config (key, value_json, updated_at) VALUES (?, '{"IsPublic":true}', 1)`, "item_visibility_"+guid.Normalize(pubPL.ID))

	// Attach external ASS subtitle in subtitle cache dir (stream 3) + embedded SRT subtitle (stream 4)
	_ = libRepo.ReplaceMediaSourcesAndStreams(ctx, movieOK.ID, []sqlite.MediaSourceRecord{
		{
			ID: movieOK.ID, ItemID: movieOK.ID, Name: "movie.mkv", Path: moviePath, Container: "mkv",
			Streams: []sqlite.MediaStreamRecord{
				{StreamIndex: 3, StreamType: "Subtitle", Codec: "ass", Language: "eng", IsExternal: true, Path: cachedSubAss},
				{StreamIndex: 4, StreamType: "Subtitle", Codec: "subrip", Language: "jpn", IsExternal: false},
			},
		},
	})

	// Build Handler WITHOUT SyncHandler or LiveTV handler first to cover all shared SQLite state paths!
	h := New(store, cfg)
	h.SetLibraryRepo(libRepo)
	h.SetUserRepo(userRepo)
	h.SetTranscodeManager(tcMgr)

	authMW := middleware.NewAuth(authSvc, nil)
	router := h.BuildRouter(AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
	})

	call := func(tok, method, target, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if tok != "" {
			req.Header.Set("X-Emby-Token", tok)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	// 1. canAccessItemID checks via OfflineActions (single object payload + array payload)
	// Blocked items (unrated, denied lib, private playlist) are silently skipped; movieOK & pubPL succeed!
	singleActionBody, _ := json.Marshal(map[string]any{
		"ItemId":        movieOK.ID,
		"Type":          "Played",
		"PositionTicks": 12000000,
		"IsFavorite":    true,
	})
	if rec := call(aliceAuth.AccessToken, http.MethodPost, "/Sync/OfflineActions", string(singleActionBody)); rec.Code != http.StatusNoContent {
		t.Fatalf("POST /Sync/OfflineActions single object failed: %d", rec.Code)
	}
	batchActionsBody, _ := json.Marshal([]map[string]any{
		{"ItemId": movieUnrated.ID, "Type": "Played"},
		{"ItemId": movieDeniedLib.ID, "Type": "Played"},
		{"ItemId": privPL.ID, "Type": "Favorite"},
		{"ItemId": pubPL.ID, "Type": "Favorite"},
		{"ItemId": movieOK.ID, "Played": false, "PlaybackPositionTicks": 55000000, "IsFavorite": false},
	})
	if rec := call(aliceAuth.AccessToken, http.MethodPost, "/Sync/OfflineActions", string(batchActionsBody)); rec.Code != http.StatusNoContent {
		t.Fatalf("POST /Sync/OfflineActions batch failed: %d", rec.Code)
	}

	// 2. Shared Sync SQLite state routes + AdditionalFiles (subtitle cache ASS + embedded SRT/ASS + fallback VTT/bin) + cache cleanup
	jobCacheDir := filepath.Join(syncCacheDir, "job-sh-1")
	_ = os.MkdirAll(jobCacheDir, 0o755)
	cachedOut1 := filepath.Join(jobCacheDir, "ji-sh-1.mp4")
	cachedOut2 := filepath.Join(jobCacheDir, "ji-sh-2.mp4")
	cachedOut3 := filepath.Join(jobCacheDir, "ji-sh-3.mp4")
	for _, p := range []string{cachedOut1, cachedOut2, cachedOut3} {
		_ = os.WriteFile(p, []byte("SYNC-CACHE"), 0o644)
	}

	_ = h.writeConfigJSON(ctx, syncConfigKey, sharedSyncState{
		Jobs: []sharedSyncJob{{Id: "job-sh-1", UserId: alice.ID, TargetId: "t-sh"}},
		JobItems: []sharedSyncJobItem{
			{Id: "ji-sh-1", JobId: "job-sh-1", ItemId: movieOK.ID, TargetId: "t-sh", Status: "ReadyToTransfer", OutputPath: cachedOut1},
			{Id: "ji-sh-2", JobId: "job-sh-1", ItemId: movieOK.ID, TargetId: "t-sh", Status: "ReadyToTransfer", OutputPath: cachedOut2},
			{Id: "ji-sh-3", JobId: "job-sh-1", ItemId: movieOK.ID, TargetId: "t-sh", Status: "ReadyToTransfer", OutputPath: cachedOut3},
		},
	})

	// HEAD /Sync/JobItems/{Id}/File
	if rec := call(aliceAuth.AccessToken, http.MethodHead, "/Sync/JobItems/ji-sh-1/File", ""); rec.Code != http.StatusOK {
		t.Fatalf("HEAD /Sync/JobItems/ji-sh-1/File status=%d", rec.Code)
	}
	if rec := call(aliceAuth.AccessToken, http.MethodHead, "/Sync/JobItems/nonexistent/File", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 HEAD unknown sync item, got %d", rec.Code)
	}

	// AdditionalFiles:
	// a) External ASS inside <DataDir>/cache/subtitles matched by language "eng" -> text/x-ssa!
	recAss := call(aliceAuth.AccessToken, http.MethodGet, "/Sync/JobItems/ji-sh-1/AdditionalFiles?Name=movie.eng.ass", "")
	if recAss.Code != http.StatusOK || !strings.Contains(recAss.Header().Get("Content-Type"), "text/x-ssa") || !strings.Contains(recAss.Body.String(), "Cached ASS") {
		t.Fatalf("expected cached ASS subtitle via isPathWithinSubtitleCache, got %d ct=%q body=%q", recAss.Code, recAss.Header().Get("Content-Type"), recAss.Body.String())
	}
	// b) Embedded stream 4 extracted as .srt via transcode.Extractor!
	recEmbSrt := call(aliceAuth.AccessToken, http.MethodGet, "/Sync/JobItems/ji-sh-1/AdditionalFiles?Name=sub.srt&StreamIndex=4", "")
	if recEmbSrt.Code != http.StatusOK || !strings.Contains(recEmbSrt.Header().Get("Content-Type"), "application/x-subrip") || !strings.Contains(recEmbSrt.Body.String(), "ExtractedSub") {
		t.Fatalf("expected extracted embedded SRT subtitle, got %d ct=%q body=%q", recEmbSrt.Code, recEmbSrt.Header().Get("Content-Type"), recEmbSrt.Body.String())
	}
	// c) Non-subtitle extension without StreamIndex -> empty octet-stream fallback
	recBin := call(aliceAuth.AccessToken, http.MethodGet, "/Sync/JobItems/ji-sh-1/AdditionalFiles?Name=metadata.xml", "")
	if recBin.Code != http.StatusOK || recBin.Header().Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("expected empty octet-stream fallback, got %d %q", recBin.Code, recBin.Header().Get("Content-Type"))
	}

	// Shared Sync Status, MarkForRemoval, UnmarkForRemoval, Delete Item, Cancel Items, Delete Job (cleaning up cache files & dir!)
	if rec := call(aliceAuth.AccessToken, http.MethodPost, "/Sync/"+movieOK.ID+"/Status", `{"TargetId":"t-sh"}`); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "ReadyToTransfer") {
		t.Fatalf("POST /Sync/{ItemId}/Status failed: %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(aliceAuth.AccessToken, http.MethodPost, "/Sync/JobItems/ji-sh-1/MarkForRemoval", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("MarkForRemoval failed: %d", rec.Code)
	}
	if rec := call(aliceAuth.AccessToken, http.MethodPost, "/Sync/JobItems/ji-sh-1/UnmarkForRemoval", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("UnmarkForRemoval failed: %d", rec.Code)
	}
	if rec := call(aliceAuth.AccessToken, http.MethodPost, "/Sync/JobItems/ji-sh-2/Delete", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("DeleteJobItemPost failed: %d", rec.Code)
	}
	if _, err := os.Stat(cachedOut2); !os.IsNotExist(err) {
		t.Fatal("expected cachedOut2 removed on DeleteJobItemPost")
	}
	if rec := call(aliceAuth.AccessToken, http.MethodPost, "/Sync/Items/Cancel", `{"TargetId":"t-sh","ItemIds":["`+movieOK.ID+`"]}`); rec.Code != http.StatusNoContent {
		t.Fatalf("CancelSyncItems failed: %d", rec.Code)
	}
	if rec := call(aliceAuth.AccessToken, http.MethodPost, "/Sync/Jobs/job-sh-1/Delete", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("DeleteSyncJobPost failed: %d", rec.Code)
	}
	if _, err := os.Stat(jobCacheDir); !os.IsNotExist(err) {
		t.Fatal("expected jobCacheDir removed on DeleteSyncJobPost")
	}

	// 3. Notifications: update preserving masked secrets + delete via JSON body Id + cross-user 403
	if rec := call(aliceAuth.AccessToken, http.MethodPost, "/Notifications/Services/Configured", `{
		"Id": "notif-1",
		"Name": "Webhook",
		"Enabled": true,
		"Options": {"webhook_url": "https://secret.hook/1", "channel": "alerts"}
	}`); rec.Code != http.StatusOK {
		t.Fatalf("create notif failed: %d", rec.Code)
	}
	if rec := call(aliceAuth.AccessToken, http.MethodPost, "/Notifications/Services/Configured", `{
		"Id": "notif-1",
		"Name": "Webhook Updated",
		"Enabled": true,
		"Options": {"webhook_url": "********", "channel": "general"}
	}`); rec.Code != http.StatusOK {
		t.Fatalf("update notif failed: %d", rec.Code)
	}
	if rec := call(bobAuth.AccessToken, http.MethodDelete, "/Notifications/Services/Configured", `{"Id":"notif-1"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when Bob deletes Alice's notification, got %d", rec.Code)
	}
	if rec := call(aliceAuth.AccessToken, http.MethodDelete, "/Notifications/Services/Configured", `{"Id":"notif-1"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 when Alice deletes own notification via body Id, got %d", rec.Code)
	}

	// 4. Shared LiveTV SQLite fallback routes + Dynamic Recording fMP4 HLS (HEAD + GET init.mp4 + 0.mp4 + .m3u8 recording)
	recMP4Src := filepath.Join(recDir, "match.ts")
	recM3U8Src := filepath.Join(recDir, "static.m3u8")
	_ = os.WriteFile(recMP4Src, []byte("TS-DATA"), 0o644)
	_ = os.WriteFile(recM3U8Src, []byte("#EXTM3U\n#EXT-X-ENDLIST\n"), 0o644)

	now := time.Now().UTC()
	_ = h.writeConfigJSON(ctx, liveTVConfigKey, sharedLiveTVState{
		Channels: []liveTVChannel{
			{Id: "ch-1", Name: "News HD", Number: "101", GroupTitle: "News", StreamUrl: "https://example.com/live.m3u8"},
			{Id: "ch-2", Name: "Sports TV", Number: "102", GroupTitle: "Sports", StreamUrl: "https://example.com/sports.ts"},
		},
		Programs: []liveTVProgram{
			{Id: "prog-1", Name: "Evening News", ChannelId: "epg-extra-1", ChannelName: "Extra EPG", StartDate: now, EndDate: now.Add(30 * time.Minute)},
		},
		Recordings: []liveTVRecording{
			{Id: "rec-fmp4", Name: "Match", Path: recMP4Src, StartDate: now, EndDate: now.Add(10 * time.Minute)},
			{Id: "rec-m3u8", Name: "Static HLS", Path: recM3U8Src},
		},
		Timers:           []json.RawMessage{json.RawMessage(`{"Id":"tmr-1"}`)},
		SeriesTimers:     []json.RawMessage{json.RawMessage(`{"Id":"stmr-1"}`)},
		TunerHosts:       []liveTVTunerHost{{Id: "th-1", Type: "m3u"}},
		ListingProviders: []liveTVListingProvider{{Id: "lp-1", Type: "xmltv"}},
	})

	// Channel Tag Prefixes, Mapping Options, Set Mapping (insert + update), Disabled, SortIndex, Deletions
	if rec := call(aliceAuth.AccessToken, http.MethodGet, "/LiveTv/ChannelTags/Prefixes", ""); rec.Code != http.StatusOK {
		t.Fatalf("ChannelTagPrefixes status=%d", rec.Code)
	}
	if rec := call(adminAuth.AccessToken, http.MethodGet, "/LiveTv/TunerHosts/Default/hdhomerun", ""); rec.Code != http.StatusOK {
		t.Fatalf("DefaultTunerHost status=%d", rec.Code)
	}
	if rec := call(adminAuth.AccessToken, http.MethodPost, "/LiveTv/ChannelMappings", `{"ProviderId":"lp-1","TunerChannelId":"ch-1","ProviderChannelId":"epg-1"}`); rec.Code != http.StatusOK {
		t.Fatalf("SetChannelMapping insert failed: %d", rec.Code)
	}
	if rec := call(adminAuth.AccessToken, http.MethodPost, "/LiveTv/ChannelMappings", `{"ProviderId":"lp-1","TunerChannelId":"ch-1","ProviderChannelId":"epg-2"}`); rec.Code != http.StatusOK {
		t.Fatalf("SetChannelMapping update failed: %d", rec.Code)
	}
	if rec := call(adminAuth.AccessToken, http.MethodGet, "/LiveTv/ChannelMappingOptions?ProviderId=lp-1", ""); rec.Code != http.StatusOK {
		t.Fatalf("ChannelMappingOptions failed: %d", rec.Code)
	}
	if rec := call(adminAuth.AccessToken, http.MethodPost, "/LiveTv/Manage/Channels/ch-1/Disabled", `{"Disabled":true}`); rec.Code != http.StatusNoContent {
		t.Fatalf("SetChannelDisabled body failed: %d", rec.Code)
	}
	if rec := call(adminAuth.AccessToken, http.MethodPost, "/LiveTv/Manage/Channels/ch-1/SortIndex", `{"NewIndex":5}`); rec.Code != http.StatusNoContent {
		t.Fatalf("SetChannelSortIndex body failed: %d", rec.Code)
	}

	// Recording dynamic fMP4 HLS: HEAD + GET master.m3u8, live.m3u8, init.mp4, 0.mp4, bad segment (400), and static .m3u8 recording
	if rec := call(aliceAuth.AccessToken, http.MethodHead, "/LiveTv/LiveRecordings/rec-fmp4/hls/master.m3u8?SegmentContainer=mp4", ""); rec.Code != http.StatusOK {
		t.Fatalf("HEAD master.m3u8 status=%d", rec.Code)
	}
	if rec := call(aliceAuth.AccessToken, http.MethodHead, "/LiveTv/LiveRecordings/rec-fmp4/hls/live.m3u8?SegmentContainer=mp4", ""); rec.Code != http.StatusOK {
		t.Fatalf("HEAD live.m3u8 status=%d", rec.Code)
	}
	if rec := call(aliceAuth.AccessToken, http.MethodHead, "/LiveTv/LiveRecordings/rec-fmp4/hls/init.mp4?SegmentContainer=mp4", ""); rec.Code != http.StatusOK {
		t.Fatalf("HEAD init.mp4 status=%d", rec.Code)
	}
	if rec := call(aliceAuth.AccessToken, http.MethodGet, "/LiveTv/LiveRecordings/rec-fmp4/hls/init.mp4?SegmentContainer=mp4", ""); rec.Code != http.StatusOK || rec.Body.String() != "FMP4-INIT-BYTES" {
		t.Fatalf("GET init.mp4 status=%d body=%q", rec.Code, rec.Body.String())
	}
	if rec := call(aliceAuth.AccessToken, http.MethodHead, "/LiveTv/LiveRecordings/rec-fmp4/hls/0.mp4?SegmentContainer=mp4", ""); rec.Code != http.StatusOK {
		t.Fatalf("HEAD 0.mp4 status=%d", rec.Code)
	}
	if rec := call(aliceAuth.AccessToken, http.MethodGet, "/LiveTv/LiveRecordings/rec-fmp4/hls/0.mp4?SegmentContainer=mp4", ""); rec.Code != http.StatusOK || rec.Body.String() != "FMP4-SEG0-BYTES" {
		t.Fatalf("GET 0.mp4 status=%d body=%q", rec.Code, rec.Body.String())
	}
	if rec := call(aliceAuth.AccessToken, http.MethodGet, "/LiveTv/LiveRecordings/rec-fmp4/hls/not-a-segment", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on invalid segment, got %d", rec.Code)
	}
	if rec := call(aliceAuth.AccessToken, http.MethodHead, "/LiveTv/LiveRecordings/rec-fmp4/hls/not-a-segment", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on HEAD invalid segment, got %d", rec.Code)
	}
	if rec := call(aliceAuth.AccessToken, http.MethodGet, "/LiveTv/LiveRecordings/rec-m3u8/hls/master.m3u8", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "#EXTM3U") {
		t.Fatalf("expected static m3u8 recording served directly, got %d %q", rec.Code, rec.Body.String())
	}

	// Delete Timer, SeriesTimer, Recording, TunerHost, ListingProvider via shared SQLite state
	if rec := call(aliceAuth.AccessToken, http.MethodPost, "/LiveTv/Timers/tmr-1/Delete", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("DeleteTimerPost failed: %d", rec.Code)
	}
	if rec := call(aliceAuth.AccessToken, http.MethodPost, "/LiveTv/SeriesTimers/stmr-1/Delete", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("DeleteSeriesTimerPost failed: %d", rec.Code)
	}
	if rec := call(adminAuth.AccessToken, http.MethodPost, "/LiveTv/TunerHosts/Delete", `{"Id":"th-1"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("DeleteTunerHostPost failed: %d", rec.Code)
	}
	if rec := call(adminAuth.AccessToken, http.MethodPost, "/LiveTv/ListingProviders/Delete", `{"Id":"lp-1"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("DeleteListingProviderPost failed: %d", rec.Code)
	}
	if rec := call(adminAuth.AccessToken, http.MethodPost, "/LiveTv/Recordings/rec-m3u8/Delete", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("DeleteRecordingPost failed: %d", rec.Code)
	}
}
