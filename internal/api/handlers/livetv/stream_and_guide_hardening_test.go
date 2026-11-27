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
