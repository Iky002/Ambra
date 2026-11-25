package compatext

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/handlers/livetv"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/dvr"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

type routeSpec struct {
	method         string
	path           string
	admin          bool
	expectNotFound bool
	body           string
}

// all59Routes lists all 59 missing Emby 4.10.2.0 routes across /LiveTv (42), /Sync (12), and /Notifications (5).
var all59Routes = []routeSpec{
	// /LiveTv - LiveStreamFiles (11)
	{http.MethodGet, "/LiveTv/LiveStreamFiles/nonexistent-item-id/stream.mp4", false, true, ""},
	{http.MethodGet, "/LiveTv/LiveStreamFiles/nonexistent-item-id/hls/master.m3u8", false, true, ""},
	{http.MethodHead, "/LiveTv/LiveStreamFiles/nonexistent-item-id/hls/master.m3u8", false, true, ""},
	{http.MethodGet, "/LiveTv/LiveStreamFiles/nonexistent-item-id/hls/master_local.m3u8", false, true, ""},
	{http.MethodHead, "/LiveTv/LiveStreamFiles/nonexistent-item-id/hls/master_local.m3u8", false, true, ""},
	{http.MethodGet, "/LiveTv/LiveStreamFiles/nonexistent-item-id/hls/live.m3u8", false, true, ""},
	{http.MethodHead, "/LiveTv/LiveStreamFiles/nonexistent-item-id/hls/live.m3u8", false, true, ""},
	{http.MethodGet, "/LiveTv/LiveStreamFiles/nonexistent-item-id/hls/live_local.m3u8", false, true, ""},
	{http.MethodHead, "/LiveTv/LiveStreamFiles/nonexistent-item-id/hls/live_local.m3u8", false, true, ""},
	{http.MethodGet, "/LiveTv/LiveStreamFiles/nonexistent-item-id/hls/0", false, true, ""},
	{http.MethodHead, "/LiveTv/LiveStreamFiles/nonexistent-item-id/hls/0", false, true, ""},

	// /LiveTv - LiveRecordings (7)
	{http.MethodGet, "/LiveTv/LiveRecordings/nonexistent-item-id/stream", false, true, ""},
	{http.MethodGet, "/LiveTv/LiveRecordings/nonexistent-item-id/hls/master.m3u8", false, true, ""},
	{http.MethodHead, "/LiveTv/LiveRecordings/nonexistent-item-id/hls/master.m3u8", false, true, ""},
	{http.MethodGet, "/LiveTv/LiveRecordings/nonexistent-item-id/hls/0", false, true, ""},
	{http.MethodHead, "/LiveTv/LiveRecordings/nonexistent-item-id/hls/0", false, true, ""},
	{http.MethodGet, "/LiveTv/LiveRecordings/nonexistent-item-id/hls/live.m3u8", false, true, ""},
	{http.MethodHead, "/LiveTv/LiveRecordings/nonexistent-item-id/hls/live.m3u8", false, true, ""},

	// /LiveTv - Service (24)
	{http.MethodGet, "/LiveTv/AvailableRecordingOptions", false, false, ""},
	{http.MethodGet, "/LiveTv/EPG", false, false, ""},
	{http.MethodGet, "/LiveTv/Manage/Channels", true, false, ""},
	{http.MethodPost, "/LiveTv/Manage/Channels/ch-1/Disabled", true, false, `{"Disabled":true}`},
	{http.MethodPost, "/LiveTv/Manage/Channels/ch-1/SortIndex", true, false, `{"NewIndex":2}`},
	{http.MethodGet, "/LiveTv/Folder", false, false, ""},
	{http.MethodPost, "/LiveTv/Tuners/tuner-1/Reset", true, false, `{}`},
	{http.MethodGet, "/LiveTv/ChannelTags", false, false, ""},
	{http.MethodGet, "/LiveTv/ChannelTags/Prefixes", false, false, ""},
	{http.MethodPost, "/LiveTv/Recordings/rec-1/Delete", true, false, `{}`},
	{http.MethodPost, "/LiveTv/Timers/timer-1/Delete", true, false, `{}`},
	{http.MethodPost, "/LiveTv/SeriesTimers/st-1/Delete", true, false, `{}`},
	{http.MethodGet, "/LiveTv/TunerHosts", true, false, ""},
	{http.MethodGet, "/LiveTv/TunerHosts/Default/m3u", true, false, ""},
	{http.MethodPost, "/LiveTv/TunerHosts/Delete", true, false, `{"Id":"th-1"}`},
	{http.MethodGet, "/LiveTv/ListingProviders/Default", false, false, ""},
	{http.MethodGet, "/LiveTv/ListingProviders/Available", true, false, ""},
	{http.MethodGet, "/LiveTv/ListingProviders", true, false, ""},
	{http.MethodPost, "/LiveTv/ListingProviders/Delete", true, false, `{"Id":"lp-1"}`},
	{http.MethodGet, "/LiveTv/ListingProviders/Lineups", true, false, ""},
	{http.MethodGet, "/LiveTv/ChannelMappingOptions", true, false, ""},
	{http.MethodPost, "/LiveTv/ChannelMappings", true, false, `{"ProviderId":"lp-1","TunerChannelId":"ch-1","ProviderChannelId":"epg-1"}`},
	{http.MethodGet, "/LiveTv/Tuners/Discvover", true, false, ""},
	{http.MethodGet, "/LiveTv/Tuners/Discover", true, false, ""},

	// /Sync (12)
	{http.MethodPost, "/Sync/Jobs/job-1/Delete", false, false, `{}`},
	{http.MethodPost, "/Sync/item-1/Status", false, false, `{}`},
	{http.MethodPost, "/Sync/JobItems/ji-1/MarkForRemoval", false, false, `{}`},
	{http.MethodPost, "/Sync/JobItems/ji-1/UnmarkForRemoval", false, false, `{}`},
	{http.MethodPost, "/Sync/JobItems/ji-1/Delete", false, false, `{}`},
	{http.MethodPost, "/Sync/Items/Cancel", false, false, `{}`},
	{http.MethodPost, "/Sync/target-1/Items/Delete", false, false, `{}`},
	{http.MethodDelete, "/Sync/target-1/Items", false, false, ""},
	{http.MethodHead, "/Sync/JobItems/ji-1/File", false, true, ""},
	{http.MethodGet, "/Sync/JobItems/ji-1/File", false, true, ""},
	{http.MethodGet, "/Sync/JobItems/ji-1/AdditionalFiles", false, true, ""},
	{http.MethodPost, "/Sync/OfflineActions", false, false, `{}`},

	// /Notifications (5)
	{http.MethodGet, "/Notifications/Services/Configured", false, false, ""},
	{http.MethodPost, "/Notifications/Services/Configured", false, false, `{"Name":"Webhook","Enabled":true}`},
	{http.MethodGet, "/Notifications/Services/Defaults", false, false, ""},
	{http.MethodPost, "/Notifications/Services/Test", false, false, `{}`},
	{http.MethodDelete, "/Notifications/Services/Configured?Id=nonexistent", false, false, ""},
}

func makeAuthFuncs() AuthFuncs {
	attachRole := func(r *http.Request, role string) *http.Request {
		isAdmin := role == "admin"
		ac := &middleware.AuthContext{
			User: &sqlite.UserRecord{
				ID:      guid.Normalize("user-ctx-1"),
				Name:    role,
				IsAdmin: isAdmin,
				Policy:  sqlite.DefaultUserPolicy(isAdmin),
			},
		}
		return r.WithContext(middleware.WithAuthContext(r.Context(), ac))
	}
	return AuthFuncs{
		RequireAuth: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				role := r.Header.Get("X-Test-Role")
				if role == "" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				next.ServeHTTP(w, attachRole(r, role))
			})
		},
		RequireAdmin: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				role := r.Header.Get("X-Test-Role")
				if role == "" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				if role != "admin" {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				next.ServeHTTP(w, attachRole(r, role))
			})
		},
		UserIDFromCtx: func(ctx context.Context) string {
			return "user-ctx-1"
		},
	}
}

func TestAll59RoutesAndAuth(t *testing.T) {
	if len(all59Routes) != 59 {
		t.Fatalf("expected 59 routes, got %d", len(all59Routes))
	}

	h := New(nil, &config.Config{ServerID: "srv-1", DataDir: t.TempDir()})
	router := h.BuildRouter(makeAuthFuncs())

	for _, rt := range all59Routes {
		// 1. Unauthenticated -> 401
		req := httptest.NewRequest(rt.method, rt.path, strings.NewReader(rt.body))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without auth: got %d, want 401", rt.method, rt.path, w.Code)
		}

		// 2. Non-admin user -> 403 for admin routes, 404 for nonexistent media routes, 2xx for other auth routes
		req = httptest.NewRequest(rt.method, rt.path, strings.NewReader(rt.body))
		req.Header.Set("X-Test-Role", "user")
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if rt.admin {
			if w.Code != http.StatusForbidden {
				t.Errorf("%s %s as user: got %d, want 403", rt.method, rt.path, w.Code)
			}
		} else if rt.expectNotFound {
			if w.Code != http.StatusNotFound {
				t.Errorf("%s %s as user: got %d, want 404 (body=%q)", rt.method, rt.path, w.Code, w.Body.String())
			}
		} else {
			if w.Code < 200 || w.Code >= 300 {
				t.Errorf("%s %s as user: got %d, want 2xx (body=%q)", rt.method, rt.path, w.Code, w.Body.String())
			}
		}

		// 3. Admin user -> 404 for nonexistent media routes, 2xx for all other routes
		req = httptest.NewRequest(rt.method, rt.path, strings.NewReader(rt.body))
		req.Header.Set("X-Test-Role", "admin")
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if rt.expectNotFound {
			if w.Code != http.StatusNotFound {
				t.Errorf("%s %s as admin: got %d, want 404 (body=%q)", rt.method, rt.path, w.Code, w.Body.String())
			}
		} else if w.Code < 200 || w.Code >= 300 {
			t.Errorf("%s %s as admin: got %d, want 2xx (body=%q)", rt.method, rt.path, w.Code, w.Body.String())
		}
		if rt.method == http.MethodHead && w.Body.Len() != 0 {
			t.Errorf("%s %s HEAD returned non-empty body (%d bytes)", rt.method, rt.path, w.Body.Len())
		}
	}
}

func TestNilStoreAndNilAuthFallback(t *testing.T) {
	h := New(nil, nil)
	if h.serverID() != "" {
		t.Fatalf("expected empty serverID with nil cfg, got %q", h.serverID())
	}
	router := h.BuildRouter(AuthFuncs{})

	req := httptest.NewRequest(http.MethodGet, "/LiveTv/Folder", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with passthrough auth, got %d", w.Code)
	}

	// Verify nonexistent streams return 404 (never fake null packets) and .vtt branch works
	for _, path := range []string{
		"/LiveTv/LiveStreamFiles/s1/stream.mp4",
		"/LiveTv/LiveStreamFiles/s1/stream.ts",
		"/LiveTv/LiveStreamFiles/s1/hls/master.m3u8",
		"/LiveTv/LiveStreamFiles/s1/hls/seg0.ts",
		"/LiveTv/LiveRecordings/r1/stream",
		"/LiveTv/LiveRecordings/r1/hls/live.m3u8",
		"/Sync/JobItems/ji1/File",
	} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: code=%d want 404", path, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Sync/JobItems/ji1/AdditionalFiles?Name=sub.vtt", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET AdditionalFiles nonexistent item: code=%d want 404", rec.Code)
	}
}

func TestSQLitePersistenceAndSharedState(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmpDir, "compatext.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	cfg := &config.Config{ServerID: "srv-test", DataDir: tmpDir}
	h := New(store, cfg)
	router := h.BuildRouter(AuthFuncs{
		UserIDFromCtx: func(ctx context.Context) string { return "u-1" },
	})

	// Create real temp media files for recording stream (inside recordingsDir) and sync file serving
	recordingsDir := filepath.Join(tmpDir, "recordings")
	if err := os.MkdirAll(recordingsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	recFile := filepath.Join(recordingsDir, "rec1.ts")
	if err := os.WriteFile(recFile, []byte("REC_STREAM_DATA"), 0o600); err != nil {
		t.Fatal(err)
	}
	syncFile := filepath.Join(tmpDir, "sync1.mp4")
	if err := os.WriteFile(syncFile, []byte("SYNC_FILE_DATA"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Seed shared livetv:state and sync:state in SQLite server_config
	ctx := context.Background()
	now := time.Now().UTC()
	h.writeConfigJSON(ctx, liveTVConfigKey, sharedLiveTVState{
		Channels: []liveTVChannel{
			{Id: "ch-1", Name: "News HD", Number: "1", GroupTitle: "News"},
			{Id: "ch-2", Name: "Sports HD", Number: "2", GroupTitle: "Sports"},
			{Id: "ch-3", Name: "NoGroup", Number: "3", GroupTitle: ""},
		},
		Programs: []liveTVProgram{
			{Id: "p-1", Name: "Evening News", ChannelId: "ch-1", StartDate: now, EndDate: now.Add(time.Hour)},
			{Id: "p-2", Name: "Football", ChannelId: "ch-2", StartDate: now, EndDate: now.Add(time.Hour)},
		},
		Recordings: []liveTVRecording{
			{Id: "rec-1", Name: "Rec 1", Path: recFile, Status: "Completed"},
			{Id: "rec-2", Name: "Rec 2", Status: "Completed"},
		},
		Timers:       []json.RawMessage{json.RawMessage(`{"Id":"t-1"}`), json.RawMessage(`{"Id":"t-2"}`)},
		SeriesTimers: []json.RawMessage{json.RawMessage(`{"Id":"st-1"}`), json.RawMessage(`{"Id":"st-2"}`)},
		TunerHosts: []liveTVTunerHost{
			{Id: "th-1", Type: "m3u", FriendlyName: "My M3U"},
			{Id: "th-2", Type: "hdhomerun", FriendlyName: "HDHR"},
		},
		ListingProviders: []liveTVListingProvider{
			{Id: "lp-1", Type: "xmltv", Path: "/epg.xml"},
			{Id: "lp-2", Type: "schedulesdirect"},
		},
	})
	h.writeConfigJSON(ctx, syncConfigKey, sharedSyncState{
		Jobs: []sharedSyncJob{
			{Id: "j-1", TargetId: "dev-1", Name: "Job 1", Status: "Queued", UserId: "u-1"},
			{Id: "j-2", TargetId: "dev-2", Name: "Job 2", Status: "Queued", UserId: "u-1"},
		},
		JobItems: []sharedSyncJobItem{
			{Id: "ji-1", JobId: "j-1", ItemId: "item-1", TargetId: "dev-1", Status: "Queued", OutputPath: syncFile},
			{Id: "ji-2", JobId: "j-2", ItemId: "item-2", TargetId: "dev-2", Status: "Queued"},
			{Id: "ji-3", JobId: "j-2", ItemId: "item-3", TargetId: "dev-2", Status: "Queued"},
		},
	})

	// 1. LiveRecording stream reads real file
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/LiveTv/LiveRecordings/rec-1/stream", nil))
	if w.Code != http.StatusOK || w.Body.String() != "REC_STREAM_DATA" {
		t.Fatalf("recording stream: code=%d body=%q", w.Code, w.Body.String())
	}

	// 2. EPG filter by ChannelIds + pagination
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/LiveTv/EPG?ChannelIds=ch-1&StartIndex=-1&Limit=1", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Evening News") || strings.Contains(w.Body.String(), "Football") {
		t.Fatalf("EPG filter failed: %d %s", w.Code, w.Body.String())
	}

	// 3. Channel management: disable ch-1 via query, sort index ch-2 via query & body
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/LiveTv/Manage/Channels/ch-1/Disabled?Disabled=true", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("disable channel: %d", w.Code)
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/LiveTv/Manage/Channels/ch-2/SortIndex?SortIndex=0", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("sortindex query: %d", w.Code)
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/LiveTv/Manage/Channels/ch-1/SortIndex", strings.NewReader(`{"SortIndex":5}`)))
	if w.Code != http.StatusNoContent {
		t.Fatalf("sortindex body: %d", w.Code)
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/LiveTv/Manage/Channels/ch-3/SortIndex?NewIndex=1", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("sortindex NewIndex: %d", w.Code)
	}

	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/LiveTv/Manage/Channels?StartIndex=0&Limit=10", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"Disabled":true`) {
		t.Fatalf("manage channels: %d %s", w.Code, w.Body.String())
	}

	// 4. ChannelTags & Prefixes
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/LiveTv/ChannelTags", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "News") || !strings.Contains(w.Body.String(), "Sports") {
		t.Fatalf("channel tags: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/LiveTv/ChannelTags/Prefixes", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"Name":"N"`) {
		t.Fatalf("channel tag prefixes: %d %s", w.Code, w.Body.String())
	}

	// 5. ChannelMappings create & update & read
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/LiveTv/ChannelMappings", strings.NewReader(`{"ProviderId":"lp-1","TunerChannelId":"ch-1","ProviderChannelId":"epg-1"}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("create mapping: %d", w.Code)
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/LiveTv/ChannelMappings", strings.NewReader(`{"ProviderId":"lp-1","TunerChannelId":"ch-1","ProviderChannelId":"epg-2"}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("update mapping: %d", w.Code)
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/LiveTv/ChannelMappingOptions?ProviderId=lp-1", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "epg-2") {
		t.Fatalf("mapping options: %d %s", w.Code, w.Body.String())
	}

	// 6. Delete Recording, Timer, SeriesTimer, TunerHost, ListingProvider (admin context for timer deletion)
	adminAC := &middleware.AuthContext{
		User: &sqlite.UserRecord{
			ID:      guid.Normalize("admin-1"),
			Name:    "admin",
			IsAdmin: true,
			Policy:  sqlite.DefaultUserPolicy(true),
		},
	}
	for _, p := range []string{
		"/LiveTv/Recordings/rec-2/Delete",
		"/LiveTv/Timers/t-1/Delete",
		"/LiveTv/SeriesTimers/st-1/Delete",
		"/LiveTv/TunerHosts/Delete?Id=th-2",
		"/LiveTv/ListingProviders/Delete?Id=lp-2",
	} {
		w = httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, p, nil)
		req = req.WithContext(middleware.WithAuthContext(req.Context(), adminAC))
		router.ServeHTTP(w, req)
		if w.Code != http.StatusNoContent {
			t.Fatalf("POST %s: %d", p, w.Code)
		}
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/LiveTv/TunerHosts", nil))
	if strings.Contains(w.Body.String(), "th-2") || !strings.Contains(w.Body.String(), "th-1") {
		t.Fatalf("tuner hosts after delete: %s", w.Body.String())
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/LiveTv/ListingProviders", nil))
	if strings.Contains(w.Body.String(), "lp-2") || !strings.Contains(w.Body.String(), "lp-1") {
		t.Fatalf("listing providers after delete: %s", w.Body.String())
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/LiveTv/Tuners/Discover", nil))
	if !strings.Contains(w.Body.String(), "th-1") {
		t.Fatalf("discover tuners: %s", w.Body.String())
	}

	// 7. Sync shared state operations
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/Sync/item-1/Status", strings.NewReader(`{"TargetId":"dev-1"}`)))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"SyncJobItemId":"ji-1"`) {
		t.Fatalf("sync status: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/Sync/JobItems/ji-1/File", nil))
	if w.Code != http.StatusOK || w.Body.String() != "SYNC_FILE_DATA" {
		t.Fatalf("sync job item file: %d %q", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/Sync/JobItems/ji-1/AdditionalFiles?Name=sub.vtt", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Type"), "text/vtt") {
		t.Fatalf("sync job item additional files: %d %q", w.Code, w.Header().Get("Content-Type"))
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/Sync/JobItems/ji-1/MarkForRemoval", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("mark removal: %d", w.Code)
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/Sync/JobItems/ji-1/UnmarkForRemoval", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("unmark removal: %d", w.Code)
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/Sync/Items/Cancel", strings.NewReader(`{"TargetId":"dev-2","ItemIds":["item-2"]}`)))
	if w.Code != http.StatusNoContent {
		t.Fatalf("cancel sync items: %d", w.Code)
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/Sync/JobItems/ji-3/Delete", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete sync job item: %d", w.Code)
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/Sync/Jobs/j-1/Delete", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete sync job: %d", w.Code)
	}

	// OfflineActions: array and single object
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/Sync/OfflineActions", strings.NewReader(`[{"ItemId":"item-1","Type":"Played"}]`)))
	if w.Code != http.StatusNoContent {
		t.Fatalf("offline actions array: %d", w.Code)
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/Sync/OfflineActions", strings.NewReader(`{"ItemId":"item-2","Type":"Favorite"}`)))
	if w.Code != http.StatusNoContent {
		t.Fatalf("offline actions single: %d", w.Code)
	}

	// 8. Notifications CRUD + invalid payload + filter + reload persistence
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/Notifications/Services/Configured", strings.NewReader(`{invalid`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on invalid json, got %d", w.Code)
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/Notifications/Services/Configured", strings.NewReader(`{"Enabled":true}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("create notification service: %d", w.Code)
	}
	var created NotificationServiceConfig
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.Id == "" || created.UserId != "u1" {
		t.Fatalf("unexpected created notification: %+v", created)
	}
	// Update same ID
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/Notifications/Services/Configured",
		strings.NewReader(`{"Id":"`+created.Id+`","Name":"Updated Webhook","Enabled":false}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("update notification service: %d", w.Code)
	}
	// Filter by another UserId as non-admin -> 403 Forbidden
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/Notifications/Services/Configured?UserId=other-user", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin querying other user, got %d %s", w.Code, w.Body.String())
	}

	// Reload handler from SQLite and verify persisted state survives restart
	h2 := New(store, cfg)
	router2 := h2.BuildRouter(AuthFuncs{})
	w = httptest.NewRecorder()
	router2.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/Notifications/Services/Configured?UserId=u-1", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Updated Webhook") {
		t.Fatalf("reloaded notification state missing: %d %s", w.Code, w.Body.String())
	}

	// Delete configured notification
	w = httptest.NewRecorder()
	router2.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/Notifications/Services/Configured?Id="+created.Id, nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete notification service: %d", w.Code)
	}
	w = httptest.NewRecorder()
	router2.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/Notifications/Services/Configured", nil))
	if strings.Contains(w.Body.String(), created.Id) {
		t.Fatalf("deleted notification still present: %s", w.Body.String())
	}
}

func TestLiveStreamFileProxyAndRecordingPathTraversal(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmpDir, "compatext-stream.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("PROXIED_COMPAT_LIVE_STREAM"))
	}))
	defer upstream.Close()

	cfg := &config.Config{ServerID: "srv-stream", DataDir: tmpDir}
	lt := livetv.New(store, cfg)
	ltMux := http.NewServeMux()
	lt.RegisterRoutes(ltMux, livetv.AuthFuncs{})

	// Import one channel into livetv
	body := `{"Type":"m3u","M3UContent":"#EXTM3U\n#EXTINF:-1,Compat Live\n` + upstream.URL + `/stream.ts\n"}`
	ltRec := httptest.NewRecorder()
	ltMux.ServeHTTP(ltRec, httptest.NewRequest(http.MethodPost, "/LiveTv/TunerHosts", strings.NewReader(body)))
	if ltRec.Code != http.StatusOK {
		t.Fatalf("seed livetv channel: %d %s", ltRec.Code, ltRec.Body.String())
	}
	chListRec := httptest.NewRecorder()
	ltMux.ServeHTTP(chListRec, httptest.NewRequest(http.MethodGet, "/LiveTv/Channels", nil))
	var chResp struct {
		Items []livetv.ChannelInfo `json:"Items"`
	}
	_ = json.Unmarshal(chListRec.Body.Bytes(), &chResp)
	if len(chResp.Items) != 1 {
		t.Fatalf("expected 1 channel, got %d", len(chResp.Items))
	}
	chID := chResp.Items[0].Id

	dvrRec := dvr.NewRecorder(dvr.Options{DataDir: tmpDir})
	defer func() { _ = dvrRec.Close() }()

	h := New(store, cfg)
	h.SetLiveTV(lt)
	h.SetDVR(dvrRec)
	router := h.BuildRouter(AuthFuncs{})

	// 1. LiveStreamFiles with known channel proxies real upstream stream
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/LiveTv/LiveStreamFiles/"+chID+"/stream.ts", nil))
	if w.Code != http.StatusOK || w.Body.String() != "PROXIED_COMPAT_LIVE_STREAM" {
		t.Fatalf("expected proxied live stream, got %d %q", w.Code, w.Body.String())
	}

	// 2. LiveStreamFiles with unknown ID returns 404 Not Found
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/LiveTv/LiveStreamFiles/unknown-id/stream.ts", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown live stream ID, got %d body=%q", w.Code, w.Body.String())
	}

	// 3. LiveRecordings path traversal & symlink escape blocked with 403
	if err := os.MkdirAll(dvrRec.RecordingsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	outsideFile := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("SECRET_DATA"), 0o600); err != nil {
		t.Fatal(err)
	}
	symlinkEscape := filepath.Join(dvrRec.RecordingsDir(), "escape.ts")
	if err := os.Symlink(outsideFile, symlinkEscape); err != nil {
		t.Fatal(err)
	}
	validFile := filepath.Join(dvrRec.RecordingsDir(), "ok.ts")
	if err := os.WriteFile(validFile, []byte("VALID_REC"), 0o600); err != nil {
		t.Fatal(err)
	}

	lt.AddRecording(context.Background(), livetv.RecordingInfo{Id: "rec-valid", Name: "Valid", Path: validFile})
	lt.AddRecording(context.Background(), livetv.RecordingInfo{Id: "rec-outside", Name: "Outside", Path: outsideFile})
	lt.AddRecording(context.Background(), livetv.RecordingInfo{Id: "rec-symlink", Name: "Symlink", Path: symlinkEscape})

	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/LiveTv/LiveRecordings/rec-valid/stream", nil))
	if w.Code != http.StatusOK || w.Body.String() != "VALID_REC" {
		t.Fatalf("valid recording: code=%d body=%q", w.Code, w.Body.String())
	}
	for _, id := range []string{"rec-outside", "rec-symlink"} {
		w = httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/LiveTv/LiveRecordings/"+id+"/stream", nil))
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for %s, got %d body=%q", id, w.Code, w.Body.String())
		}
	}
}

func TestCompatExtDeleteDelegationUnifiesLiveTVState(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmpDir, "compatext-unify.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	cfg := &config.Config{ServerID: "srv-unify", DataDir: tmpDir}
	dvrRec := dvr.NewRecorder(dvr.Options{DataDir: tmpDir})
	defer func() { _ = dvrRec.Close() }()

	lt := livetv.New(store, cfg)
	lt.SetRecorder(dvrRec)
	ltMux := http.NewServeMux()
	lt.RegisterRoutes(ltMux, livetv.AuthFuncs{})

	h := New(store, cfg)
	h.SetLiveTV(lt)
	h.SetDVR(dvrRec)
	router := h.BuildRouter(AuthFuncs{})

	// 1. Seed tuner host (creates channel + placeholder program), recording, timer, series timer, listing provider via livetv
	thRec := httptest.NewRecorder()
	ltMux.ServeHTTP(thRec, httptest.NewRequest(http.MethodPost, "/LiveTv/TunerHosts",
		strings.NewReader(`{"Id":"th-del","Type":"m3u","M3UContent":"#EXTM3U\n#EXTINF:-1,Chan 1\nhttp://tv/1.ts\n"}`)))
	if thRec.Code != http.StatusOK {
		t.Fatalf("seed tuner host: %d %s", thRec.Code, thRec.Body.String())
	}

	if err := os.MkdirAll(dvrRec.RecordingsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	recFile := filepath.Join(dvrRec.RecordingsDir(), "del.mkv")
	if err := os.WriteFile(recFile, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	lt.AddRecording(context.Background(), livetv.RecordingInfo{Id: "rec-del", Name: "Del Rec", Path: recFile})

	ltMux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/LiveTv/Timers",
		strings.NewReader(`{"Id":"tm-del","Name":"Del Timer","ChannelId":"ch-1"}`)))
	ltMux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/LiveTv/SeriesTimers",
		strings.NewReader(`{"Id":"st-del","Name":"Del Series"}`)))
	ltMux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/LiveTv/ListingProviders",
		strings.NewReader(`{"Id":"lp-del","Type":"xmltv"}`)))

	// 2. Delete all 5 via compatext POST .../Delete routes
	for _, tc := range []struct {
		path string
		body string
	}{
		{"/LiveTv/Recordings/rec-del/Delete", `{}`},
		{"/LiveTv/Timers/tm-del/Delete", `{}`},
		{"/LiveTv/SeriesTimers/st-del/Delete", `{}`},
		{"/LiveTv/TunerHosts/Delete", `{"Id":"th-del"}`},
		{"/LiveTv/ListingProviders/Delete", `{"Id":"lp-del"}`},
	} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body)))
		if w.Code != http.StatusNoContent {
			t.Fatalf("POST %s: got %d, want 204", tc.path, w.Code)
		}
	}

	// 3. Verify recording file removed and livetv in-memory state immediately reflects all deletions (including cascading channels & programs)
	if _, err := os.Stat(recFile); !os.IsNotExist(err) {
		t.Fatalf("expected recording file to be removed via delegated DeleteRecording, err=%v", err)
	}
	for _, checkPath := range []string{
		"/LiveTv/Recordings",
		"/LiveTv/Timers",
		"/LiveTv/SeriesTimers",
		"/LiveTv/Channels",
		"/LiveTv/Programs",
	} {
		w := httptest.NewRecorder()
		ltMux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, checkPath, nil))
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"TotalRecordCount":0`) {
			t.Fatalf("GET %s after compatext delete: expected TotalRecordCount 0, got %d %s", checkPath, w.Code, w.Body.String())
		}
	}

	// 4. Write a new timer in livetv and verify deleted tuner host & listing provider do NOT resurrect in DB
	ltMux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/LiveTv/Timers",
		strings.NewReader(`{"Id":"tm-new","Name":"Fresh Timer"}`)))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/LiveTv/TunerHosts", nil))
	if strings.Contains(w.Body.String(), "th-del") {
		t.Fatalf("deleted tuner host resurrected: %s", w.Body.String())
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/LiveTv/ListingProviders", nil))
	if strings.Contains(w.Body.String(), "lp-del") {
		t.Fatalf("deleted listing provider resurrected: %s", w.Body.String())
	}
}

func TestLiveStreamFileHLSRewriteAndSignedProxy(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmpDir, "compatext-hls.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/live/master.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:6.0,\nseg0.ts\n"))
		case "/live/seg0.ts":
			w.Header().Set("Content-Type", "video/mp2t")
			_, _ = w.Write([]byte("COMPAT_HLS_SEG0"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer upstream.Close()

	cfg := &config.Config{ServerID: "srv-hls", DataDir: tmpDir}
	lt := livetv.New(store, cfg)
	ltMux := http.NewServeMux()
	lt.RegisterRoutes(ltMux, livetv.AuthFuncs{})

	body := `{"Type":"m3u","M3UContent":"#EXTM3U\n#EXTINF:-1,Compat HLS\n` + upstream.URL + `/live/master.m3u8\n"}`
	ltRec := httptest.NewRecorder()
	ltMux.ServeHTTP(ltRec, httptest.NewRequest(http.MethodPost, "/LiveTv/TunerHosts", strings.NewReader(body)))
	if ltRec.Code != http.StatusOK {
		t.Fatalf("seed livetv channel: %d %s", ltRec.Code, ltRec.Body.String())
	}
	chListRec := httptest.NewRecorder()
	ltMux.ServeHTTP(chListRec, httptest.NewRequest(http.MethodGet, "/LiveTv/Channels", nil))
	var chResp struct {
		Items []livetv.ChannelInfo `json:"Items"`
	}
	_ = json.Unmarshal(chListRec.Body.Bytes(), &chResp)
	chID := chResp.Items[0].Id

	h := New(store, cfg)
	h.SetLiveTV(lt)
	router := h.BuildRouter(AuthFuncs{})

	// Request HLS playlist via /LiveTv/LiveStreamFiles/{Id}/stream.m3u8
	reqPath := "/LiveTv/LiveStreamFiles/" + chID + "/stream.m3u8?api_key=tok-compat"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, reqPath, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("LiveStreamFiles m3u8 status=%d body=%s", w.Code, w.Body.String())
	}
	playlist := w.Body.String()
	var rewrittenLine string
	for _, line := range strings.Split(playlist, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			rewrittenLine = line
			break
		}
	}
	if rewrittenLine == "" || !strings.Contains(rewrittenLine, "Stream?target=") || !strings.Contains(rewrittenLine, "&sig=") {
		t.Fatalf("expected signed relative rewritten URI in playlist, got:\n%s", playlist)
	}

	// Resolve relative URI against the original request URL and verify livetv serves the segment
	base, err := url.Parse(reqPath)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := url.Parse(rewrittenLine)
	if err != nil {
		t.Fatal(err)
	}
	resolved := base.ResolveReference(ref)
	segRec := httptest.NewRecorder()
	ltMux.ServeHTTP(segRec, httptest.NewRequest(http.MethodGet, resolved.RequestURI(), nil))
	if segRec.Code != http.StatusOK || segRec.Body.String() != "COMPAT_HLS_SEG0" {
		t.Fatalf("expected resolved signed segment %q to return COMPAT_HLS_SEG0, got %d %q", resolved.RequestURI(), segRec.Code, segRec.Body.String())
	}
}
