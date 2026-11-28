package livetv

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/dvr"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestLiveTVHLSRewriterNewlinesWhitespaceAndFailClosed(t *testing.T) {
	h := New(nil, config.Default(t.TempDir()))
	defer h.Close()
	baseURL, _ := url.Parse("https://93.184.216.34/live/index.m3u8")

	raw := "#EXTM3U\r#EXT-X-KEY:METHOD=AES-128,URI = \"https://93.184.216.34/k.key\"\r\n#EXT-X-MAP:URI = \"unclosed\rseg0.ts\r"
	out := h.rewriteHLSPlaylistWithSessionAndUser(raw, baseURL, "ch-1", "Stream", "", false, 1000, 0, "user-123")
	if strings.Contains(out, "\r") {
		t.Fatalf("expected CR normalized out, got %q", out)
	}
	if !strings.Contains(out, "uid=user123") {
		t.Fatalf("expected uid bound in rewritten proxy URLs, got:\n%s", out)
	}
	if strings.Contains(out, "unclosed") {
		t.Fatalf("expected malformed URI= tag line to be dropped fail-closed, got:\n%s", out)
	}
	if strings.Count(out, "Stream?target=") != 2 {
		t.Fatalf("expected 2 rewritten URIs (key + seg0), got:\n%s", out)
	}
}

func TestLiveTVSignedSubTargetRevocationAndRecordingRBAC(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "livetv_hardening.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	userRepo := sqlite.NewUserRepo(store)
	libRepo := sqlite.NewLibraryRepo(store)

	viewerPol := sqlite.DefaultUserPolicy(false)
	viewerPol.EnableLiveTvAccess = true
	viewerPol.EnableMediaPlayback = true
	viewer := &sqlite.UserRecord{ID: guid.New(), Name: "viewer", Policy: viewerPol}
	if err := userRepo.CreateUser(ctx, viewer); err != nil {
		t.Fatal(err)
	}

	var upURL string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/live.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:4.0,\n" + upURL + "/seg0.ts\n"))
		default:
			w.Header().Set("Content-Type", "video/mp2t")
			_, _ = w.Write([]byte("SEG-OK"))
		}
	}))
	defer up.Close()
	upURL = up.URL

	cfg := config.Default(tmp)
	h := New(store, cfg)
	defer h.Close()
	h.SetUserRepo(userRepo)
	h.SetLibraryRepo(libRepo)
	h.SetStreamClient(up.Client())

	h.mu.Lock()
	h.state.TunerHosts = []TunerHostInfo{{Id: "th-1", Type: "m3u", AllowPrivateNetwork: true}}
	h.state.Channels = []ChannelInfo{{
		Id:          "ch-1",
		Name:        "News",
		Type:        "TvChannel",
		ChannelType: "TV",
		StreamUrl:   up.URL + "/live.m3u8",
		TunerHostId: "th-1",
	}}
	h.mu.Unlock()

	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{
		RequireAuth: func(next http.Handler) http.Handler { return next },
	})

	// Initial authenticated request mints uid-bound signed segment URL.
	reqInit := httptest.NewRequest(http.MethodGet, "/LiveTv/Channels/ch-1/Stream", nil)
	reqInit = reqInit.WithContext(middleware.WithAuthContext(reqInit.Context(), &middleware.AuthContext{User: viewer}))
	recInit := httptest.NewRecorder()
	mux.ServeHTTP(recInit, reqInit)
	if recInit.Code != http.StatusOK {
		t.Fatalf("initial HLS request failed: %d %s", recInit.Code, recInit.Body.String())
	}
	var segURL string
	for _, ln := range strings.Split(recInit.Body.String(), "\n") {
		if strings.HasPrefix(ln, "Stream?target=") {
			segURL = "/LiveTv/Channels/ch-1/" + ln
			break
		}
	}
	if segURL == "" || !strings.Contains(segURL, "&uid=") {
		t.Fatalf("expected uid-bound signed segment URL, got %q", segURL)
	}

	// Segment succeeds while user is enabled.
	recOK := httptest.NewRecorder()
	mux.ServeHTTP(recOK, httptest.NewRequest(http.MethodGet, segURL, nil))
	if recOK.Code != http.StatusOK {
		t.Fatalf("expected 200 on signed segment while user active, got %d", recOK.Code)
	}

	// Revoke LiveTV access -> signed segment must immediately return 403.
	viewer.Policy.EnableLiveTvAccess = false
	if err := userRepo.UpdateUserPolicy(ctx, viewer.ID, viewer.Policy); err != nil {
		t.Fatal(err)
	}
	recRevoked := httptest.NewRecorder()
	mux.ServeHTTP(recRevoked, httptest.NewRequest(http.MethodGet, segURL, nil))
	if recRevoked.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on signed segment after EnableLiveTvAccess revoked, got %d", recRevoked.Code)
	}

	// Recording folder RBAC: restrict viewer to another library -> recording hidden in list and 403 on direct get.
	viewer.Policy.EnableLiveTvAccess = true
	recDir := filepath.Join(tmp, "recordings")
	otherDir := filepath.Join(tmp, "other")
	_ = os.MkdirAll(recDir, 0o755)
	_ = os.MkdirAll(otherDir, 0o755)
	recLib, _ := libRepo.CreateLibrary(ctx, "Recordings", "livetv", []string{recDir})
	otherLib, _ := libRepo.CreateLibrary(ctx, "Other", "movies", []string{otherDir})
	viewer.Policy.EnableAllFolders = false
	viewer.Policy.EnabledFolders = []string{otherLib.ID}
	if err := userRepo.UpdateUserPolicy(ctx, viewer.ID, viewer.Policy); err != nil {
		t.Fatal(err)
	}

	recFile := filepath.Join(recDir, "show.mkv")
	_ = os.WriteFile(recFile, []byte("MKV"), 0o644)
	recInfo := h.AddRecording(ctx, RecordingInfo{Name: "Restricted Show", Path: recFile})
	_ = libRepo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:        recInfo.Id,
		LibraryID: recLib.ID,
		Type:      "Video",
		MediaType: "Video",
		Name:      recInfo.Name,
		Path:      recFile,
	})

	reqGet := httptest.NewRequest(http.MethodGet, "/LiveTv/Recordings/"+recInfo.Id, nil)
	reqGet = reqGet.WithContext(middleware.WithAuthContext(reqGet.Context(), &middleware.AuthContext{User: viewer}))
	wGet := httptest.NewRecorder()
	mux.ServeHTTP(wGet, reqGet)
	if wGet.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for restricted recording, got %d", wGet.Code)
	}

	// Deleted timer output is discarded in OnRecordingFinished.
	orphanOut := filepath.Join(recDir, "deleted_timer.mkv")
	_ = os.WriteFile(orphanOut, []byte("ORPHAN"), 0o644)
	dvrRec := dvr.NewRecorder(dvr.Options{DataDir: tmp})
	defer dvrRec.Close()
	h.SetRecorder(dvrRec)
	beforeCount := len(h.SnapshotRecordings())
	h.OnRecordingFinished(ctx, dvr.RecordingState{
		Job:        dvr.RecordingJob{TimerID: "deleted-timer-id", ProgramName: "Deleted"},
		OutputPath: orphanOut,
		Status:     dvr.StatusCompleted,
		StartedAt:  time.Now().UTC().Add(-time.Minute),
		EndedAt:    time.Now().UTC(),
	})
	if len(h.SnapshotRecordings()) != beforeCount {
		t.Fatal("expected recording for deleted timer to be discarded")
	}
	if _, err := os.Stat(orphanOut); !os.IsNotExist(err) {
		t.Fatalf("expected orphaned output file to be removed, stat err=%v", err)
	}
}

func TestProxyChannelStreamIgnoresUnsignedUIDParam(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-VERSION:3\n#EXTINF:4.0,\nseg0.ts\n"))
	}))
	defer upstream.Close()

	h := New(nil, config.Default(t.TempDir()))
	defer h.Close()
	h.SetHTTPClient(upstream.Client())
	h.streamClient = upstream.Client()
	h.customStreamClient = true

	viewerID := guid.Normalize("viewer-user-id-1111")
	adminID := guid.Normalize("admin-user-id-9999")
	chID := "ch-uid-test"
	h.mu.Lock()
	h.state.Channels = append(h.state.Channels, ChannelInfo{
		Id:        chID,
		Name:      "News",
		StreamUrl: upstream.URL + "/index.m3u8",
	})
	h.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/LiveTv/Channels/"+chID+"/Stream?uid="+adminID, nil)
	ac := &middleware.AuthContext{
		User: &sqlite.UserRecord{
			ID:     viewerID,
			Name:   "viewer",
			Policy: sqlite.DefaultUserPolicy(false),
		},
	}
	req = req.WithContext(middleware.WithAuthContext(req.Context(), ac))
	rec := httptest.NewRecorder()

	h.ProxyChannelStream(rec, req, chID, upstream.URL+"/index.m3u8")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "uid="+adminID) {
		t.Fatalf("unsigned ?uid= admin override leaked into rewritten playlist:\n%s", body)
	}
	if !strings.Contains(body, "uid="+viewerID) {
		t.Fatalf("expected rewritten playlist sub-targets bound to authenticated caller uid=%s, got:\n%s", viewerID, body)
	}
}

func TestRefreshAllGuidesConcurrentProviderMutationSkipsStaleSweep(t *testing.T) {
	fetchStarted := make(chan struct{})
	releaseFetch := make(chan struct{})
	now := time.Now().UTC()
	xmlStart := now.Add(-10 * time.Minute).Format("20060102150405 -0700")
	xmlStop := now.Add(50 * time.Minute).Format("20060102150405 -0700")

	epgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(fetchStarted)
		<-releaseFetch
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<tv>
  <channel id="epg-a"><display-name>Ch A</display-name></channel>
  <programme start="` + xmlStart + `" stop="` + xmlStop + `" channel="epg-a">
    <title>Show A</title>
  </programme>
</tv>`))
	}))
	defer epgSrv.Close()

	h := New(nil, config.Default(t.TempDir()))
	defer h.Close()
	h.SetHTTPClient(epgSrv.Client())

	h.mu.Lock()
	h.state.Channels = []ChannelInfo{
		{Id: "ch-a", Name: "Ch A", Number: "1"},
		{Id: "ch-b", Name: "Ch B", Number: "2"},
	}
	h.state.ListingProviders = []ListingProviderInfo{
		{Id: "lp-a", Type: "xmltv", Path: epgSrv.URL + "/a.xml", AllowPrivateNetwork: true, NetworkPolicySet: true},
	}
	h.state.Programs = []ProgramInfo{
		{
			Id:        "prog-b",
			ChannelId: "ch-b",
			Name:      "Show B From Concurrent Provider",
			StartDate: now.Add(-5 * time.Minute),
			EndDate:   now.Add(45 * time.Minute),
			Source:    "xmltv",
		},
	}
	h.mu.Unlock()

	done := make(chan error, 1)
	go func() {
		done <- h.RefreshAllGuides(context.Background())
	}()

	<-fetchStarted
	// Concurrently add a second XMLTV provider while RefreshAllGuides is fetching lp-a.
	h.mu.Lock()
	h.state.ListingProviders = append(h.state.ListingProviders, ListingProviderInfo{
		Id: "lp-b", Type: "xmltv", Path: epgSrv.URL + "/b.xml", AllowPrivateNetwork: true, NetworkPolicySet: true,
	})
	h.mu.Unlock()
	close(releaseFetch)

	if err := <-done; err != nil {
		t.Fatalf("RefreshAllGuides: %v", err)
	}

	h.mu.RLock()
	var foundB bool
	for _, p := range h.state.Programs {
		if p.Id == "prog-b" {
			foundB = true
			break
		}
	}
	h.mu.RUnlock()
	if !foundB {
		t.Fatal("expected prog-b to be preserved when listing providers mutated concurrently during RefreshAllGuides")
	}
}

func TestPruneOrphanedChannelMappingsUsesCallback(t *testing.T) {
	h := New(nil, config.Default(t.TempDir()))
	defer h.Close()

	h.mu.Lock()
	h.state.ListingProviders = []ListingProviderInfo{{Id: "lp-live", Type: "xmltv"}}
	h.state.Channels = []ChannelInfo{{Id: "ch-live", Name: "Live 1"}}
	h.mu.Unlock()

	var called bool
	var gotProviders, gotChannels map[string]struct{}
	h.SetOnPruneChannelMappings(func(_ context.Context, activeProviders, activeChannels map[string]struct{}) {
		called = true
		gotProviders = activeProviders
		gotChannels = activeChannels
	})

	h.pruneOrphanedChannelMappings(context.Background())
	if !called {
		t.Fatal("expected onPruneChannelMappings callback to be invoked")
	}
	if _, ok := gotProviders["lp-live"]; !ok {
		t.Fatalf("expected lp-live in activeProviders, got %+v", gotProviders)
	}
	if _, ok := gotChannels["ch-live"]; !ok {
		t.Fatalf("expected ch-live in activeChannels, got %+v", gotChannels)
	}
}

