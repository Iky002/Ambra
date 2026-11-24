package hls

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestHLSRBACAndLiveStreamOwnership(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "hls_rbac.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)
	mgr := transcode.NewManager(cfg, transcode.WithCapabilities(transcode.SoftwareOnlyCapabilities()))
	defer mgr.Close()

	libAllowed, _ := repo.CreateLibrary(ctx, "Allowed", "movies", []string{filepath.Join(tmp, "a")})
	libForbidden, _ := repo.CreateLibrary(ctx, "Forbidden", "movies", []string{filepath.Join(tmp, "b")})

	allowedItem := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libAllowed.ID, ParentID: libAllowed.ID,
		Type: "Movie", MediaType: "Video", Name: "AllowedMovie", Path: filepath.Join(tmp, "a.mkv"),
	}
	forbiddenItem := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libForbidden.ID, ParentID: libForbidden.ID,
		Type: "Movie", MediaType: "Video", Name: "SecretMovie", Path: filepath.Join(tmp, "b.mkv"),
	}
	_ = repo.UpsertItem(ctx, allowedItem)
	_ = repo.UpsertItem(ctx, forbiddenItem)
	_ = repo.UpsertMediaSourceAndStreams(ctx, forbiddenItem.ID, sqlite.MediaSourceRecord{
		ID: forbiddenItem.ID, ItemID: forbiddenItem.ID, Path: forbiddenItem.Path, Container: "mkv",
		Streams: []sqlite.MediaStreamRecord{
			{StreamIndex: 0, StreamType: "Video", Codec: "h264", Width: 1920, Height: 1080},
			{StreamIndex: 2, StreamType: "Subtitle", Codec: "srt", Language: "eng"},
		},
	})

	polA := sqlite.DefaultUserPolicy(false)
	polA.EnableAllFolders = false
	polA.EnabledFolders = []string{libAllowed.ID}
	userA := &sqlite.UserRecord{ID: guid.New(), Name: "userA", Policy: polA, Config: sqlite.DefaultUserConfig()}
	_ = userRepo.CreateUser(ctx, userA)

	userB := &sqlite.UserRecord{ID: guid.New(), Name: "userB", Policy: polA, Config: sqlite.DefaultUserConfig()}
	_ = userRepo.CreateUser(ctx, userB)

	admin := &sqlite.UserRecord{ID: guid.New(), Name: "admin", IsAdmin: true, Policy: sqlite.DefaultUserPolicy(true), Config: sqlite.DefaultUserConfig()}
	_ = userRepo.CreateUser(ctx, admin)

	h := New(repo, mgr, cfg)
	h.SetUserRepo(userRepo)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	doReq := func(user *sqlite.UserRecord, method, target string, body []byte) *httptest.ResponseRecorder {
		var r *http.Request
		if body != nil {
			r = httptest.NewRequest(method, target, bytes.NewReader(body))
		} else {
			r = httptest.NewRequest(method, target, nil)
		}
		if user != nil {
			r = r.WithContext(middleware.WithAuthContext(r.Context(), &middleware.AuthContext{User: user}))
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, r)
		return rec
	}

	// Forbidden library item must return 403 on all HLS & LiveStream endpoints.
	for _, path := range []string{
		"/Videos/" + forbiddenItem.ID + "/master.m3u8",
		"/Videos/" + forbiddenItem.ID + "/main.m3u8",
		"/Videos/" + forbiddenItem.ID + "/live.m3u8",
		"/Videos/" + forbiddenItem.ID + "/hls1/main/0.ts",
		"/Videos/" + forbiddenItem.ID + "/" + forbiddenItem.ID + "/Subtitles/2/subtitles.m3u8",
	} {
		rec := doReq(userA, http.MethodGet, path, nil)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("GET %s as restricted user: got %d, want 403", path, rec.Code)
		}
	}

	openForbiddenBody, _ := json.Marshal(map[string]any{"ItemId": forbiddenItem.ID})
	if rec := doReq(userA, http.MethodPost, "/LiveStreams/Open", openForbiddenBody); rec.Code != http.StatusForbidden {
		t.Fatalf("POST /LiveStreams/Open forbidden item: got %d, want 403", rec.Code)
	}

	// Allowed library item succeeds and registers session with UserID.
	rec := doReq(userA, http.MethodGet, "/Videos/"+allowedItem.ID+"/master.m3u8?DeviceId=devA&PlaySessionId=psA", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET master.m3u8 allowed item: got %d, want 200", rec.Code)
	}

	// userB cannot kill userA's active encoding.
	_ = doReq(userB, http.MethodDelete, "/Videos/ActiveEncodings?DeviceId=devA&PlaySessionId=psA", nil)
	if len(mgr.ActiveSessions()) != 1 {
		t.Fatalf("userB should not be able to kill userA's session")
	}
	// userA can kill own active encoding.
	_ = doReq(userA, http.MethodDelete, "/Videos/ActiveEncodings?DeviceId=devA&PlaySessionId=psA", nil)
	if len(mgr.ActiveSessions()) != 0 {
		t.Fatalf("userA should be able to kill own session")
	}

	// LiveStream ownership checks.
	openAllowedBody, _ := json.Marshal(map[string]any{"ItemId": allowedItem.ID, "PlaySessionId": "ls-ps-1"})
	openRec := doReq(userA, http.MethodPost, "/LiveStreams/Open", openAllowedBody)
	if openRec.Code != http.StatusOK {
		t.Fatalf("POST /LiveStreams/Open allowed: got %d", openRec.Code)
	}
	var openResp struct {
		LiveStreamId string `json:"LiveStreamId"`
	}
	_ = json.Unmarshal(openRec.Body.Bytes(), &openResp)
	if openResp.LiveStreamId == "" {
		t.Fatalf("expected non-empty LiveStreamId")
	}

	// userB gets 403 on MediaInfo and Close.
	if rec := doReq(userB, http.MethodPost, "/LiveStreams/MediaInfo?LiveStreamId="+openResp.LiveStreamId, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("userB MediaInfo: got %d, want 403", rec.Code)
	}
	if rec := doReq(userB, http.MethodPost, "/LiveStreams/Close?LiveStreamId="+openResp.LiveStreamId, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("userB Close: got %d, want 403", rec.Code)
	}
	// Owner gets 200 on MediaInfo.
	if rec := doReq(userA, http.MethodPost, "/LiveStreams/MediaInfo?LiveStreamId="+openResp.LiveStreamId, nil); rec.Code != http.StatusOK {
		t.Fatalf("owner MediaInfo: got %d, want 200", rec.Code)
	}
	// Admin can close userA's live stream.
	if rec := doReq(admin, http.MethodPost, "/LiveStreams/Close?LiveStreamId="+openResp.LiveStreamId, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("admin Close: got %d, want 204", rec.Code)
	}
}

func TestBuildSessionParamsSubtitleBurnIn(t *testing.T) {
	h := New(nil, nil, nil)
	item := &sqlite.ItemRecord{ID: "item1", Path: "/media/movie.mkv"}
	src := &sqlite.MediaSourceRecord{
		ID:   "ms1",
		Path: "/media/movie.mkv",
		Streams: []sqlite.MediaStreamRecord{
			{StreamIndex: 3, StreamType: "Subtitle", Codec: "ass", IsExternal: true, Path: "/media/movie.zh.ass"},
			{StreamIndex: 4, StreamType: "Subtitle", Codec: "pgssub", IsExternal: false},
		},
	}

	r := httptest.NewRequest(http.MethodGet, "/Videos/item1/master.m3u8?SubtitleStreamIndex=3&SubtitleMethod=Encode", nil)
	r = r.WithContext(middleware.WithAuthContext(r.Context(), &middleware.AuthContext{
		User: &sqlite.UserRecord{ID: "user-123"},
	}))
	p := h.buildSessionParams(r, item, src, httpx.ParseQuery(r))
	if p.UserID != "user123" {
		t.Fatalf("UserID = %q, want user123", p.UserID)
	}
	if p.SubtitleStreamIndex == nil || *p.SubtitleStreamIndex != 3 {
		t.Fatalf("SubtitleStreamIndex = %v, want 3", p.SubtitleStreamIndex)
	}
	if p.SubtitleMethod != "Encode" || p.SubtitleCodec != "ass" || p.SubtitlePath != "/media/movie.zh.ass" {
		t.Fatalf("unexpected subtitle params: %+v", p)
	}

	r2 := httptest.NewRequest(http.MethodGet, "/Videos/item1/master.m3u8?SubtitleStreamIndex=4&SubtitleMethod=Encode", nil)
	p2 := h.buildSessionParams(r2, item, src, httpx.ParseQuery(r2))
	if p2.SubtitleCodec != "pgssub" || p2.SubtitlePath != "" || p2.SubtitleRelativeIndex != 0 {
		t.Fatalf("unexpected embedded subtitle params: %+v", p2)
	}
}

func TestBuildSessionParamsSubtitleRelativeIndexAndBitrate(t *testing.T) {
	h := New(nil, nil, nil)
	item := &sqlite.ItemRecord{ID: "item1", Path: "/media/movie.mkv"}
	src := &sqlite.MediaSourceRecord{
		ID:   "ms1",
		Path: "/media/movie.mkv",
		Streams: []sqlite.MediaStreamRecord{
			{StreamIndex: 0, StreamType: "Video", Codec: "h264"},
			{StreamIndex: 1, StreamType: "Audio", Codec: "aac"},
			{StreamIndex: 2, StreamType: "Audio", Codec: "ac3"},
			{StreamIndex: 3, StreamType: "Subtitle", Codec: "srt", IsExternal: true, Path: "/media/movie.en.srt"},
			{StreamIndex: 4, StreamType: "Subtitle", Codec: "srt", IsExternal: false},
			{StreamIndex: 5, StreamType: "Subtitle", Codec: "ass", IsExternal: false},
		},
	}

	// Embedded subtitle at global index 4 is 0-th embedded subtitle (external index 3 is skipped).
	r0 := httptest.NewRequest(http.MethodGet, "/Videos/item1/master.m3u8?SubtitleStreamIndex=4&SubtitleMethod=Encode&MaxStreamingBitrate=2000000", nil)
	p0 := h.buildSessionParams(r0, item, src, httpx.ParseQuery(r0))
	if p0.SubtitleRelativeIndex != 0 {
		t.Fatalf("SubtitleRelativeIndex for stream 4 = %d, want 0", p0.SubtitleRelativeIndex)
	}
	if p0.VideoBitrate != 2_000_000-192_000 {
		t.Fatalf("VideoBitrate = %d, want %d", p0.VideoBitrate, 2_000_000-192_000)
	}

	// Embedded subtitle at global index 5 is 1-st embedded subtitle.
	r1 := httptest.NewRequest(http.MethodGet, "/Videos/item1/master.m3u8?SubtitleStreamIndex=5&SubtitleMethod=Encode&MaxStreamingBitrate=300000", nil)
	p1 := h.buildSessionParams(r1, item, src, httpx.ParseQuery(r1))
	if p1.SubtitleRelativeIndex != 1 {
		t.Fatalf("SubtitleRelativeIndex for stream 5 = %d, want 1", p1.SubtitleRelativeIndex)
	}
	if p1.VideoBitrate != 256_000 {
		t.Fatalf("VideoBitrate floor = %d, want 256000", p1.VideoBitrate)
	}
}

func TestReadFirstLineURLBlocksMetadataAndLinkLocal(t *testing.T) {
	tmp := t.TempDir()
	blocked := []string{
		"http://169.254.169.254/latest/meta-data/",
		"http://169.254.0.1/secret",
		"http://[fe80::1]/stream.mkv",
		"http://[::ffff:169.254.169.254]/latest/meta-data/",
		"http://metadata.google.internal/computeMetadata/v1/",
		"http://METADATA.GOOGLE.INTERNAL./computeMetadata/v1/",
	}
	for _, u := range blocked {
		p := filepath.Join(tmp, "blocked.strm")
		if err := os.WriteFile(p, []byte(u+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := readFirstLineURL(p); got != "" {
			t.Fatalf("readFirstLineURL(%q) = %q, want empty string", u, got)
		}
	}
}
