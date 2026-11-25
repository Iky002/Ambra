package hls

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/config"
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
	_ = os.WriteFile(strmPath, []byte("https://fallback.example.com/raw.mkv\n"), 0o644)

	item := &sqlite.ItemRecord{ID: guid.New(), LibraryID: "lib1", Type: "Movie", Name: "STRM Movie", Path: strmPath}
	_ = repo.UpsertItem(ctx, item)

	h := New(repo, mgr, cfg)

	// 1. Without SetStreamResolver, falls back to readFirstLineURL
	req := httptest.NewRequest(http.MethodGet, "/Videos/"+item.ID+"/master.m3u8", nil)
	params := h.buildSessionParams(req, item, nil, httpx.ParseQuery(req))
	if params.InputPath != "https://fallback.example.com/raw.mkv" {
		t.Fatalf("expected fallback URL, got %q", params.InputPath)
	}

	// 2. With SetStreamResolver, uses the custom resolver
	called := false
	h.SetStreamResolver(func(_ context.Context, rawPath string) (string, error) {
		called = true
		if rawPath != strmPath {
			t.Errorf("unexpected rawPath %q", rawPath)
		}
		return "https://resolved-302.example.com/final.mkv", nil
	})
	params = h.buildSessionParams(req, item, nil, httpx.ParseQuery(req))
	if !called || params.InputPath != "https://resolved-302.example.com/final.mkv" {
		t.Fatalf("expected custom resolver URL, called=%v got=%q", called, params.InputPath)
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
