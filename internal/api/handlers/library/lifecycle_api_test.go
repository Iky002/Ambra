package library

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/scanner"
	"github.com/ambra-media/ambra/internal/core/watcher"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestScannerControlEndpointsAndWatcherHooks(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "lifecycle_api.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	cfg := config.Default(tmp)
	sc := scanner.New(repo, nil, nil, cfg)
	sc.SetProber(nil)

	var mu sync.Mutex
	events := make(map[string]int)
	eventCh := make(chan string, 8)
	w, err := watcher.New(func(_ context.Context, libraryID string, _ []string) {
		mu.Lock()
		events[libraryID]++
		mu.Unlock()
		select {
		case eventCh <- libraryID:
		default:
		}
	}, watcher.WithDebounce(20*time.Millisecond))
	if err != nil {
		t.Fatalf("watcher.New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := w.Start(ctx); err != nil {
		t.Fatalf("watcher.Start: %v", err)
	}
	defer w.Close()

	mux := http.NewServeMux()
	h := New(repo, sc, cfg)
	h.SetWatcher(w)
	h.RegisterRoutes(mux, AuthFuncs{})

	// 1. Create virtual folder -> watcher starts watching its path.
	mediaDir1 := filepath.Join(tmp, "lib1")
	mediaDir2 := filepath.Join(tmp, "lib2")
	_ = os.MkdirAll(mediaDir1, 0o755)
	_ = os.MkdirAll(mediaDir2, 0o755)

	body1, _ := json.Marshal(map[string]any{
		"Name":           "LibOne",
		"CollectionType": "movies",
		"Paths":          []string{mediaDir1},
	})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Library/VirtualFolders", bytes.NewReader(body1)))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /Library/VirtualFolders status=%d body=%s", rec.Code, rec.Body.String())
	}
	var vf1 virtualFolderDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &vf1)

	// Writing a file in mediaDir1 should trigger watcher callback for vf1.ItemId.
	_ = os.WriteFile(filepath.Join(mediaDir1, "test.mkv"), []byte("x"), 0o644)
	select {
	case gotLib := <-eventCh:
		if gotLib != vf1.ItemId {
			t.Fatalf("expected watcher event for %s, got %s", vf1.ItemId, gotLib)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for watcher event after POST /Library/VirtualFolders")
	}

	// Create second library and delete by name -> watcher unwatch path covered.
	body2, _ := json.Marshal(map[string]any{
		"Name":           "LibTwo",
		"CollectionType": "movies",
		"Paths":          []string{mediaDir2},
	})
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Library/VirtualFolders", bytes.NewReader(body2)))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /Library/VirtualFolders(2) status=%d", rec.Code)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/Library/VirtualFolders?id="+vf1.ItemId, nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE by id status=%d", rec.Code)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/Library/VirtualFolders?name=LibTwo", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE by name status=%d", rec.Code)
	}

	// 2. Scanner pause / resume / cancel endpoints.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/admin/scanner/pause", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/admin/scanner/pause status=%d", rec.Code)
	}
	var snap scanner.ScanProgressSnapshot
	_ = json.Unmarshal(rec.Body.Bytes(), &snap)
	if !snap.Paused || !sc.IsPaused() {
		t.Fatalf("expected paused=true after /pause, got snap=%+v isPaused=%v", snap, sc.IsPaused())
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/admin/scanner/resume", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/admin/scanner/resume status=%d", rec.Code)
	}
	snap = scanner.ScanProgressSnapshot{}
	_ = json.Unmarshal(rec.Body.Bytes(), &snap)
	if snap.Paused || sc.IsPaused() {
		t.Fatalf("expected paused=false after /resume, got snap=%+v isPaused=%v", snap, sc.IsPaused())
	}

	sc.Pause()
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/admin/scanner/cancel", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/admin/scanner/cancel status=%d", rec.Code)
	}
	snap = scanner.ScanProgressSnapshot{}
	_ = json.Unmarshal(rec.Body.Bytes(), &snap)
	if snap.Paused || sc.IsPaused() {
		t.Fatalf("expected paused=false after /cancel, got snap=%+v isPaused=%v", snap, sc.IsPaused())
	}

	// 3. Nil scanner safety on control endpoints.
	nilMux := http.NewServeMux()
	New(repo, nil, cfg).RegisterRoutes(nilMux, AuthFuncs{})
	for _, ep := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/admin/scanner/progress"},
		{http.MethodPost, "/api/admin/scanner/pause"},
		{http.MethodPost, "/api/admin/scanner/resume"},
		{http.MethodPost, "/api/admin/scanner/cancel"},
	} {
		rec = httptest.NewRecorder()
		nilMux.ServeHTTP(rec, httptest.NewRequest(ep.method, ep.path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("nil scanner %s %s status=%d", ep.method, ep.path, rec.Code)
		}
	}
}
