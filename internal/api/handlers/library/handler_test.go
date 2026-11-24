package library

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
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/scanner"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/plugins/nfo"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestLibraryHandlerRoutes(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "lib_api.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	reg := plugins.NewRegistry()
	reg.Register(nfo.New())
	cfg := config.Default(tmp)
	sc := scanner.New(repo, reg, eventbus.New(), cfg)

	mux := http.NewServeMux()
	h := New(repo, sc, cfg)
	h.RegisterRoutes(mux, AuthFuncs{})

	// Create virtual folder via POST /Library/VirtualFolders
	mediaDir := filepath.Join(tmp, "media")
	_ = os.MkdirAll(mediaDir, 0o755)
	_ = os.WriteFile(filepath.Join(mediaDir, "Interstellar (2014).mkv"), []byte("video"), 0o644)
	_ = os.WriteFile(filepath.Join(mediaDir, "Interstellar (2014).nfo"), []byte(`<movie><title>星际穿越</title><year>2014</year><genre>科幻</genre></movie>`), 0o644)

	body, _ := json.Marshal(map[string]any{
		"Name":           "电影库",
		"CollectionType": "movies",
		"Paths":          []string{mediaDir},
	})
	req := httptest.NewRequest(http.MethodPost, "/Library/VirtualFolders", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /Library/VirtualFolders failed: %d %s", rec.Code, rec.Body.String())
	}

	// Scan synchronously so items are ready
	if _, err := sc.ScanAll(context.Background()); err != nil {
		t.Fatal(err)
	}

	// GET /Users/u1/Views
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Users/u1/Views", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /Users/u1/Views failed: %d", rec.Code)
	}
	var views dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &views)
	if views.TotalRecordCount != 1 || views.Items[0].Name != "电影库" {
		t.Fatalf("unexpected views: %+v", views)
	}

	// GET /Search/Hints?SearchTerm=穿越+星际
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Search/Hints?SearchTerm=%E7%A9%BF%E8%B6%8A+%E6%98%9F%E9%99%85", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /Search/Hints failed: %d", rec.Code)
	}
	var hints dto.SearchHintResult
	_ = json.Unmarshal(rec.Body.Bytes(), &hints)
	if hints.TotalRecordCount != 1 || hints.SearchHints[0].Name != "星际穿越" {
		t.Fatalf("unexpected search hints: %+v", hints)
	}

	// GET /Items/Counts
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Items/Counts", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /Items/Counts failed: %d", rec.Code)
	}
	var counts sqlite.ItemCounts
	_ = json.Unmarshal(rec.Body.Bytes(), &counts)
	if counts.MovieCount != 1 {
		t.Fatalf("expected MovieCount=1, got %+v", counts)
	}

	// GET /api/admin/scanner/progress
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/admin/scanner/progress", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/admin/scanner/progress failed: %d %s", rec.Code, rec.Body.String())
	}
	var prog scanner.ScanProgressSnapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &prog); err != nil {
		t.Fatalf("unmarshal progress: %v", err)
	}
	if prog.Running || prog.Phase != "idle" {
		t.Fatalf("expected idle progress snapshot, got %+v", prog)
	}

	// Verify RequireAdmin protection and nil scanner fallback
	adminMux := http.NewServeMux()
	hNilScanner := New(repo, nil, cfg)
	hNilScanner.RegisterRoutes(adminMux, AuthFuncs{
		RequireAdmin: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Admin") != "1" {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				next.ServeHTTP(w, r)
			})
		},
	})
	rec = httptest.NewRecorder()
	adminMux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/admin/scanner/progress", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin, got %d", rec.Code)
	}
	reqAdmin := httptest.NewRequest(http.MethodGet, "/api/admin/scanner/progress", nil)
	reqAdmin.Header.Set("X-Admin", "1")
	rec = httptest.NewRecorder()
	adminMux.ServeHTTP(rec, reqAdmin)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for admin with nil scanner, got %d", rec.Code)
	}
}
