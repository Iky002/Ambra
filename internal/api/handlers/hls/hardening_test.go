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
	"time"

	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestHLSStreamResolverAndLiveStreamOpenNotFound(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "hls_hardening.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	cfg := config.Default(tmp)
	mgr := transcode.NewManager(cfg, transcode.WithCapabilities(&transcode.HWCapabilities{}))
	defer mgr.Close()

	strmPath := filepath.Join(tmp, "movie.strm")
	_ = os.WriteFile(strmPath, []byte("https://93.184.216.34/raw.mkv\n"), 0o644)

	item := &sqlite.ItemRecord{ID: guid.New(), LibraryID: "lib1", Type: "Movie", Name: "STRM Movie", Path: strmPath}
	_ = repo.UpsertItem(ctx, item)

	h := New(repo, mgr, cfg)

	// 1. Without SetStreamResolver, falls back to readFirstLineURL
	req := httptest.NewRequest(http.MethodGet, "/Videos/"+item.ID+"/master.m3u8", nil)
	params := h.buildSessionParams(req, item, nil, httpx.ParseQuery(req))
	if params.InputPath != "https://93.184.216.34/raw.mkv" {
		t.Fatalf("expected fallback URL, got %q", params.InputPath)
	}

	// 2. With SetStreamResolver, uses the custom resolver
	called := false
	h.SetStreamResolver(func(_ context.Context, rawPath string) (string, error) {
		called = true
		if rawPath != strmPath {
			t.Errorf("unexpected rawPath %q", rawPath)
		}
		return "https://93.184.216.34/final.mkv", nil
	})
	params = h.buildSessionParams(req, item, nil, httpx.ParseQuery(req))
	if !called || params.InputPath != "https://93.184.216.34/final.mkv" {
		t.Fatalf("expected custom resolver URL, called=%v got=%q", called, params.InputPath)
	}

	// 2b. With SetStreamResolver returning unsafe URL (metadata IP or file://), InputPath must be cleared
	for _, bad := range []string{"http://169.254.169.254/latest/meta-data/", "file:///etc/passwd", "/etc/passwd"} {
		h.SetStreamResolver(func(_ context.Context, _ string) (string, error) {
			return bad, nil
		})
		pBad := h.buildSessionParams(req, item, nil, httpx.ParseQuery(req))
		if pBad.InputPath != "" {
			t.Fatalf("expected unsafe resolved strm %q to clear InputPath, got %q", bad, pBad.InputPath)
		}
	}

	// 3. handleLiveStreamOpen returns 404 when ItemId is specified but not found in repo
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	missingID := guid.New()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/LiveStreams/Open", bytes.NewReader([]byte(`{"ItemId":"`+missingID+`"}`))))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for nonexistent ItemId in LiveStreams/Open, got %d body=%s", rec.Code, rec.Body.String())
	}

	// Empty ItemId still succeeds with synthetic LiveStream
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/LiveStreams/Open", bytes.NewReader([]byte(`{}`))))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for empty ItemId in LiveStreams/Open, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestLiveStreamsCapAndTTLEviction(t *testing.T) {
	h := New(nil, nil, config.Default(t.TempDir()))
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	// 1. Seed an expired entry (>4 hours old)
	expiredID := "expired-live-stream"
	h.liveMu.Lock()
	h.liveStreams[expiredID] = &liveStreamEntry{
		LiveStreamID: expiredID,
		CreatedAt:    time.Now().UTC().Add(-5 * time.Hour),
	}
	h.liveMu.Unlock()

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/LiveStreams/Open", bytes.NewReader([]byte(`{}`))))
	if rec.Code != http.StatusOK {
		t.Fatalf("LiveStreams/Open got %d", rec.Code)
	}
	h.liveMu.RLock()
	_, stillExists := h.liveStreams[expiredID]
	h.liveMu.RUnlock()
	if stillExists {
		t.Fatalf("expected expired live stream entry to be evicted")
	}

	// 2. Open maxLiveStreams + 10 entries; map must stay capped at maxLiveStreams
	for i := 0; i < maxLiveStreams+10; i++ {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/LiveStreams/Open", bytes.NewReader([]byte(`{}`))))
		if rec.Code != http.StatusOK {
			t.Fatalf("LiveStreams/Open #%d got %d", i, rec.Code)
		}
	}
	h.liveMu.RLock()
	count := len(h.liveStreams)
	h.liveMu.RUnlock()
	if count != maxLiveStreams {
		t.Fatalf("expected liveStreams capped at %d, got %d", maxLiveStreams, count)
	}
}

func TestHLSPlaySessionIsolationParamClampsAndAPIKeyLiveStream(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "hls_sec2.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	cfg := config.Default(tmp)
	mgr := transcode.NewManager(cfg, transcode.WithCapabilities(transcode.SoftwareOnlyCapabilities()))
	defer mgr.Close()

	h := New(sqlite.NewLibraryRepo(store), mgr, cfg)
	userRepo := sqlite.NewUserRepo(store)
	h.SetUserRepo(userRepo)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	// 1. Parameter clamping and AudioCodec whitelist
	item := &sqlite.ItemRecord{ID: "item-clamp", Path: filepath.Join(tmp, "demo.mkv")}
	r := httptest.NewRequest(http.MethodGet, "/Videos/item-clamp/master.m3u8?PlaySessionId=ps1&DeviceId=spoofed&AudioCodec=libevil&VideoBitrate=999999999&AudioBitrate=9999999&TranscodingMaxAudioChannels=32&SegmentLength=1", nil)
	r = r.WithContext(middleware.WithAuthContext(r.Context(), &middleware.AuthContext{
		User:   &sqlite.UserRecord{ID: "user-a"},
		Client: auth.ClientInfo{DeviceID: "token-bound-dev"},
	}))
	p := h.buildSessionParams(r, item, nil, httpx.ParseQuery(r))
	if p.DeviceID != "token-bound-dev" {
		t.Fatalf("expected token-bound DeviceID, got %q", p.DeviceID)
	}
	if p.PlaySessionID != "usera:ps1" {
		t.Fatalf("expected user-scoped PlaySessionID, got %q", p.PlaySessionID)
	}
	if p.AudioCodec != "aac" || p.VideoBitrate != maxHLSVideoBitrate || p.AudioBitrate != maxHLSAudioBitrate || p.AudioChannels != maxHLSAudioChannels || p.SegmentDuration != minHLSSegmentLength {
		t.Fatalf("unexpected clamped params: %+v", p)
	}
	rMaxSeg := httptest.NewRequest(http.MethodGet, "/Videos/item-clamp/master.m3u8?SegmentLength=99&AudioCodec=opus", nil)
	p2 := h.buildSessionParams(rMaxSeg, item, nil, httpx.ParseQuery(rMaxSeg))
	if p2.SegmentDuration != maxHLSSegmentLength || p2.AudioCodec != "opus" {
		t.Fatalf("expected SegmentLength clamped to %d and AudioCodec=opus, got %+v", maxHLSSegmentLength, p2)
	}

	// 2. Non-admin cannot inspect or close API-Key-opened LiveStream (UserID == "")
	apiKeyReq := httptest.NewRequest(http.MethodPost, "/LiveStreams/Open", bytes.NewReader([]byte(`{}`)))
	apiKeyReq = apiKeyReq.WithContext(middleware.WithAuthContext(apiKeyReq.Context(), &middleware.AuthContext{IsAPIKey: true}))
	recOpen := httptest.NewRecorder()
	mux.ServeHTTP(recOpen, apiKeyReq)
	if recOpen.Code != http.StatusOK {
		t.Fatalf("API key LiveStreams/Open failed: %d", recOpen.Code)
	}
	var openResp struct {
		LiveStreamId string `json:"LiveStreamId"`
	}
	_ = json.Unmarshal(recOpen.Body.Bytes(), &openResp)

	userAC := &middleware.AuthContext{User: &sqlite.UserRecord{ID: "normal-user"}}
	for _, ep := range []string{"/LiveStreams/MediaInfo", "/LiveStreams/Close"} {
		req := httptest.NewRequest(http.MethodPost, ep+"?LiveStreamId="+openResp.LiveStreamId, nil)
		req = req.WithContext(middleware.WithAuthContext(req.Context(), userAC))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for non-admin on API key %s, got %d", ep, rec.Code)
		}
	}

	// 3. Fail-closed canAccessItem when unauthenticated and userRepo is set
	if h.canAccessItem(httptest.NewRequest(http.MethodGet, "/Videos/x/master.m3u8", nil), item) {
		t.Fatalf("expected canAccessItem fail-closed when unauthenticated")
	}
}
