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

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestHLSV0104SecurityFMP4InitHDRQuotaAndScopedStop(t *testing.T) {
	tmp := t.TempDir()
	st, err := sqlite.Open(filepath.Join(tmp, "hls_v0104.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(st)
	userRepo := sqlite.NewUserRepo(st)
	cfg := config.Default(tmp)
	mgr := transcode.NewManager(cfg, transcode.WithCapabilities(transcode.SoftwareOnlyCapabilities()))
	defer mgr.Close()

	lib, _ := repo.CreateLibrary(ctx, "Movies", "movies", []string{tmp})
	mkvPath := filepath.Join(tmp, "hdr_movie.mkv")
	_ = os.WriteFile(mkvPath, []byte("fake-mkv"), 0o644)

	hdrItem := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, ParentID: lib.ID,
		Type: "Movie", MediaType: "Video", Name: "HDR Movie", Path: mkvPath,
	}
	_ = repo.UpsertItem(ctx, hdrItem)
	_ = repo.ReplaceMediaSourcesAndStreams(ctx, hdrItem.ID, []sqlite.MediaSourceRecord{{
		ID:        "ms-hdr-1",
		ItemID:    hdrItem.ID,
		Name:      "hdr_movie.mkv",
		Path:      mkvPath,
		Container: "mkv",
		Streams: []sqlite.MediaStreamRecord{
			{StreamIndex: 0, StreamType: "Video", Codec: "hevc", Width: 3840, Height: 2160, VideoRange: "HDR10"},
			{StreamIndex: 1, StreamType: "Audio", Codec: "aac", Channels: 2},
			{StreamIndex: 2, StreamType: "Subtitle", Codec: "srt", IsExternal: true, Path: filepath.Join(tmp, "sub.srt")},
		},
	}})

	userA := &sqlite.UserRecord{ID: guid.New(), Name: "alice", Policy: sqlite.DefaultUserPolicy(false), Config: sqlite.DefaultUserConfig()}
	userB := &sqlite.UserRecord{ID: guid.New(), Name: "bob", Policy: sqlite.DefaultUserPolicy(false), Config: sqlite.DefaultUserConfig()}
	admin := &sqlite.UserRecord{ID: guid.New(), Name: "admin", IsAdmin: true, Policy: sqlite.DefaultUserPolicy(true), Config: sqlite.DefaultUserConfig()}
	for _, u := range []*sqlite.UserRecord{userA, userB, admin} {
		_ = userRepo.CreateUser(ctx, u)
	}

	h := New(repo, mgr, cfg)
	h.SetUserRepo(userRepo)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	doAuth := func(ac *middleware.AuthContext, method, target string, body []byte) *httptest.ResponseRecorder {
		var r *http.Request
		if body != nil {
			r = httptest.NewRequest(method, target, bytes.NewReader(body))
		} else {
			r = httptest.NewRequest(method, target, nil)
		}
		if ac != nil {
			r = r.WithContext(middleware.WithAuthContext(r.Context(), ac))
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, r)
		return rec
	}

	aliceAC := &middleware.AuthContext{User: userA}
	bobAC := &middleware.AuthContext{User: userB}
	adminAC := &middleware.AuthContext{User: admin}
	apiKeyAC := &middleware.AuthContext{IsAPIKey: true}

	// 1. Non-.strm HTTP src.Path & SubtitlePath SSRF validation
	for _, badURL := range []string{"http://169.254.169.254/latest/meta-data/", "http://[fe80::1]/video.mp4"} {
		req := httptest.NewRequest(http.MethodGet, "/Videos/x/main.m3u8?SubtitleStreamIndex=2&SubtitleMethod=Encode", nil)
		p := h.buildSessionParams(req, &sqlite.ItemRecord{ID: "x", Path: badURL}, &sqlite.MediaSourceRecord{
			ID: "ms", Path: badURL,
			Streams: []sqlite.MediaStreamRecord{
				{StreamIndex: 2, StreamType: "Subtitle", Codec: "srt", IsExternal: true, Path: badURL},
			},
		}, httpx.ParseQuery(req))
		if p.InputPath != "" || p.SubtitlePath != "" {
			t.Fatalf("expected unsafe HTTP non-.strm InputPath/SubtitlePath cleared for %q, got in=%q sub=%q", badURL, p.InputPath, p.SubtitlePath)
		}
	}
	reqSafe := httptest.NewRequest(http.MethodGet, "/Videos/x/main.m3u8", nil)
	pSafe := h.buildSessionParams(reqSafe, &sqlite.ItemRecord{ID: "x", Path: "https://93.184.216.34/video.mp4"}, nil, httpx.ParseQuery(reqSafe))
	if pSafe.InputPath != "https://93.184.216.34/video.mp4" {
		t.Fatalf("expected safe public HTTP src.Path preserved, got %q", pSafe.InputPath)
	}

	// 2. IsHDR detection across HDR/HLG/DOVI/PQ/BT2020 vs SDR
	for vr, want := range map[string]bool{
		"HDR10": true, "HDR10+": true, "HLG": true, "DOVI": true,
		"DolbyVision": true, "PQ": true, "BT2020": true, "SDR": false, "": false,
	} {
		if got := isHDRVideoRange(vr); got != want {
			t.Fatalf("isHDRVideoRange(%q) = %v, want %v", vr, got, want)
		}
	}
	reqHDR := httptest.NewRequest(http.MethodGet, "/Videos/"+hdrItem.ID+"/main.m3u8", nil)
	_, srcHDR, _ := h.resolveItemAndSource(ctx, hdrItem.ID, "ms-hdr-1")
	pHDR := h.buildSessionParams(reqHDR, hdrItem, srcHDR, httpx.ParseQuery(reqHDR))
	if !pHDR.IsHDR {
		t.Fatalf("expected IsHDR=true for HDR10 video stream")
	}

	// 3. master.m3u8 does NOT allocate transcode sessions; main.m3u8 does
	recMaster := doAuth(aliceAC, http.MethodGet, "/Videos/"+hdrItem.ID+"/master.m3u8?PlaySessionId=ps-hdr&DeviceId=dev-a", nil)
	if recMaster.Code != http.StatusOK {
		t.Fatalf("master.m3u8 status=%d", recMaster.Code)
	}
	if len(mgr.ActiveSessions()) != 0 {
		t.Fatalf("master.m3u8 must not register transcode session, got %d", len(mgr.ActiveSessions()))
	}

	// 4. fMP4 init.mp4 routes (/hls1/{PlaylistId}/init.mp4, /hls/{PlaylistId}/init.mp4, /init.mp4)
	recMain := doAuth(aliceAC, http.MethodGet, "/Videos/"+hdrItem.ID+"/main.m3u8?PlaySessionId=ps-init&DeviceId=dev-a&SegmentContainer=m4s", nil)
	if recMain.Code != http.StatusOK {
		t.Fatalf("main.m3u8 fmp4 status=%d", recMain.Code)
	}
	sessions := mgr.ActiveSessions()
	if len(sessions) != 1 {
		t.Fatalf("expected 1 registered session, got %d", len(sessions))
	}
	if err := os.WriteFile(filepath.Join(sessions[0].WorkDir, "init.mp4"), []byte("FTYP_MOOV_INIT"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, initPath := range []string{
		"/Videos/" + hdrItem.ID + "/hls1/main/init.mp4?PlaySessionId=ps-init&DeviceId=dev-a&SegmentContainer=m4s",
		"/Videos/" + hdrItem.ID + "/hls/main/init.mp4?PlaySessionId=ps-init&DeviceId=dev-a&SegmentContainer=m4s",
		"/Videos/" + hdrItem.ID + "/init.mp4?PlaySessionId=ps-init&DeviceId=dev-a&SegmentContainer=m4s",
	} {
		recInit := doAuth(aliceAC, http.MethodGet, initPath, nil)
		if recInit.Code != http.StatusOK || recInit.Header().Get("Content-Type") != "video/mp4" || recInit.Body.String() != "FTYP_MOOV_INIT" {
			t.Fatalf("GET %s failed: code=%d ct=%q body=%q", initPath, recInit.Code, recInit.Header().Get("Content-Type"), recInit.Body.String())
		}
	}
	// Missing item on init.mp4 -> 404
	if r := doAuth(aliceAC, http.MethodGet, "/Videos/"+guid.New()+"/init.mp4", nil); r.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown item init.mp4, got %d", r.Code)
	}

	// 5. StopSessionsScoped: non-admin cannot stop API-Key session or another user's session; admin can stop by raw PlaySessionId!
	_ = doAuth(apiKeyAC, http.MethodGet, "/Videos/"+hdrItem.ID+"/main.m3u8?PlaySessionId=api-ps&DeviceId=api-dev", nil)
	if len(mgr.ActiveSessions()) != 2 {
		t.Fatalf("expected 2 sessions (alice + apikey), got %d", len(mgr.ActiveSessions()))
	}
	// Bob cannot stop API key session or Alice's session
	_ = doAuth(bobAC, http.MethodDelete, "/Videos/ActiveEncodings?PlaySessionId=api-ps&DeviceId=api-dev", nil)
	_ = doAuth(bobAC, http.MethodDelete, "/Videos/ActiveEncodings?PlaySessionId=ps-init&DeviceId=dev-a", nil)
	if len(mgr.ActiveSessions()) != 2 {
		t.Fatalf("non-admin must not stop API key or other user's sessions, remaining=%d", len(mgr.ActiveSessions()))
	}
	// Admin stops Alice's session by raw PlaySessionId=ps-init!
	_ = doAuth(adminAC, http.MethodDelete, "/Videos/ActiveEncodings?PlaySessionId=ps-init", nil)
	if len(mgr.ActiveSessions()) != 1 {
		t.Fatalf("expected admin to stop user session by raw PlaySessionId, remaining=%d", len(mgr.ActiveSessions()))
	}
	// Admin stops API key session
	_ = doAuth(adminAC, http.MethodDelete, "/Videos/ActiveEncodings?PlaySessionId=api-ps", nil)
	if len(mgr.ActiveSessions()) != 0 {
		t.Fatalf("expected 0 remaining sessions, got %d", len(mgr.ActiveSessions()))
	}

	// 6. LiveStreams/Open per-user quota (16) and non-admin path sanitization
	openBody, _ := json.Marshal(map[string]any{"ItemId": hdrItem.ID, "MediaSourceId": "ms-hdr-1"})
	var firstLiveID string
	for i := 0; i < maxLiveStreamsPerUser; i++ {
		rec := doAuth(aliceAC, http.MethodPost, "/LiveStreams/Open", openBody)
		if rec.Code != http.StatusOK {
			t.Fatalf("alice LiveStreams/Open #%d failed: %d %s", i+1, rec.Code, rec.Body.String())
		}
		if i == 0 {
			var resp struct {
				LiveStreamId string              `json:"LiveStreamId"`
				MediaSource  dto.MediaSourceInfo `json:"MediaSource"`
			}
			_ = json.Unmarshal(rec.Body.Bytes(), &resp)
			firstLiveID = resp.LiveStreamId
			if resp.MediaSource.Path != "" {
				t.Fatalf("expected non-admin LiveStreams/Open to strip MediaSource.Path, got %q", resp.MediaSource.Path)
			}
			for _, strm := range resp.MediaSource.MediaStreams {
				if strm.Path != "" {
					t.Fatalf("expected non-admin LiveStreams/Open to strip MediaStream.Path, got %q", strm.Path)
				}
			}
		}
	}
	// 17th for alice -> 429
	if rec := doAuth(aliceAC, http.MethodPost, "/LiveStreams/Open", openBody); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on 17th LiveStreams/Open for alice, got %d", rec.Code)
	}
	// MediaInfo for non-admin also strips Path
	recMI := doAuth(aliceAC, http.MethodPost, "/LiveStreams/MediaInfo?LiveStreamId="+firstLiveID, nil)
	var miResp liveStreamOpenResponse
	_ = json.Unmarshal(recMI.Body.Bytes(), &miResp)
	if miResp.MediaSource.Path != "" {
		t.Fatalf("expected non-admin LiveStreams/MediaInfo to strip Path, got %q", miResp.MediaSource.Path)
	}
	// Admin LiveStreams/Open retains Path and is not blocked by Alice's quota
	recAdminOpen := doAuth(adminAC, http.MethodPost, "/LiveStreams/Open", openBody)
	if recAdminOpen.Code != http.StatusOK {
		t.Fatalf("admin LiveStreams/Open failed: %d", recAdminOpen.Code)
	}
	var adminResp struct {
		MediaSource dto.MediaSourceInfo `json:"MediaSource"`
	}
	_ = json.Unmarshal(recAdminOpen.Body.Bytes(), &adminResp)
	if adminResp.MediaSource.Path != mkvPath {
		t.Fatalf("expected admin LiveStreams/Open to retain Path, got %q", adminResp.MediaSource.Path)
	}
}
