package governance

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/budget"
	coregov "github.com/ambra-media/ambra/internal/core/governance"
	"github.com/ambra-media/ambra/internal/core/scheduler"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func setupHandlerTest(t *testing.T, withAdminGate bool) (*http.ServeMux, *coregov.Service, *sqlite.LibraryRepo, string) {
	t.Helper()
	dataDir := t.TempDir()
	cfg, err := config.Load(dataDir)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	store, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	svc, err := coregov.NewService(coregov.Options{
		Store:      store,
		Config:     cfg,
		Scheduler:  scheduler.New(nil),
		BudgetPool: budget.New(2),
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	mux := http.NewServeMux()
	h := New(svc)
	var auth AuthFuncs
	if withAdminGate {
		auth = AuthFuncs{
			RequireAuth: func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Header.Get("X-Test-Role") == "" {
						w.WriteHeader(http.StatusUnauthorized)
						return
					}
					next.ServeHTTP(w, r)
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
					next.ServeHTTP(w, r)
				})
			},
		}
	}
	h.RegisterRoutes(mux, auth)
	return mux, svc, sqlite.NewLibraryRepo(store), dataDir
}

func doReq(mux *http.ServeMux, method, path, role string, body any) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		switch v := body.(type) {
		case string:
			buf.WriteString(v)
		default:
			_ = json.NewEncoder(&buf).Encode(v)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	if role != "" {
		req.Header.Set("X-Test-Role", role)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestGovernanceAdminAuthEnforcement(t *testing.T) {
	mux, _, _, _ := setupHandlerTest(t, true)

	endpoints := []struct {
		method string
		path   string
	}{
		{"GET", "/api/admin/health"},
		{"GET", "/api/admin/governance/audit"},
		{"POST", "/api/admin/governance/orphans/cleanup"},
		{"GET", "/api/admin/governance/review"},
		{"POST", "/api/admin/governance/review/abc/approve"},
		{"POST", "/api/admin/governance/review/abc/dismiss"},
		{"POST", "/api/admin/governance/items/abc/lock"},
		{"POST", "/api/admin/governance/items/abc/nfo"},
	}

	for _, ep := range endpoints {
		// Unauthenticated -> 401
		rec := doReq(mux, ep.method, ep.path, "", nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s without auth: want 401, got %d", ep.method, ep.path, rec.Code)
		}
		// Non-admin user -> 403
		rec = doReq(mux, ep.method, ep.path, "user", nil)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s %s as user: want 403, got %d", ep.method, ep.path, rec.Code)
		}
	}
}

func TestGovernanceEndpointsFullFlow(t *testing.T) {
	mux, svc, repo, _ := setupHandlerTest(t, true)
	ctx := context.Background()

	mediaDir := t.TempDir()
	lib, _ := repo.CreateLibrary(ctx, "Movies", "movies", []string{mediaDir})

	moviePath := filepath.Join(mediaDir, "Matrix.mkv")
	_ = os.WriteFile(moviePath, []byte("vid"), 0o644)
	movie := &sqlite.ItemRecord{
		LibraryID: lib.ID,
		Type:      "Movie",
		Name:      "Matrix Raw",
		Path:      moviePath,
	}
	orphan := &sqlite.ItemRecord{
		LibraryID: lib.ID,
		Type:      "Movie",
		Name:      "Ghost Film",
		Path:      filepath.Join(mediaDir, "missing.mkv"),
	}
	_ = repo.UpsertItem(ctx, movie)
	_ = repo.UpsertItem(ctx, orphan)

	audit1, _ := svc.EnqueueAudit(ctx, coregov.EnqueueAuditInput{
		ItemID:     movie.ID,
		ItemName:   movie.Name,
		ItemType:   "Movie",
		Path:       movie.Path,
		Confidence: 0.55,
		Candidates: []coregov.ScrapeCandidate{{ProviderID: "603", Title: "黑客帝国", Confidence: 0.85}},
	})
	audit2, _ := svc.EnqueueAudit(ctx, coregov.EnqueueAuditInput{
		ItemID:     orphan.ID,
		ItemName:   orphan.Name,
		ItemType:   "Movie",
		Path:       orphan.Path,
		Confidence: 0.40,
	})

	// 1. GET /api/admin/health
	rec := doReq(mux, "GET", "/api/admin/health", "admin", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("health: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var health coregov.SystemHealthReport
	if err := json.Unmarshal(rec.Body.Bytes(), &health); err != nil {
		t.Fatalf("decode health: %v", err)
	}
	if health.Library.TotalItems != 2 || health.Library.PendingAudits != 2 {
		t.Fatalf("unexpected health library stats: %+v", health.Library)
	}

	// 2. GET /api/admin/governance/audit
	rec = doReq(mux, "GET", "/api/admin/governance/audit", "admin", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("audit: want 200, got %d", rec.Code)
	}
	var auditReport coregov.LibraryAuditReport
	if err := json.Unmarshal(rec.Body.Bytes(), &auditReport); err != nil {
		t.Fatalf("decode audit: %v", err)
	}
	if len(auditReport.OrphanItems) != 1 || auditReport.OrphanItems[0].ID != orphan.ID {
		t.Fatalf("expected 1 orphan, got %+v", auditReport.OrphanItems)
	}

	// 3. GET /api/admin/governance/review
	rec = doReq(mux, "GET", "/api/admin/governance/review", "admin", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list reviews: want 200, got %d", rec.Code)
	}
	var listResp struct {
		Items            []coregov.ScrapeAuditItem `json:"Items"`
		TotalRecordCount int                       `json:"TotalRecordCount"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listResp); err != nil || listResp.TotalRecordCount != 2 {
		t.Fatalf("unexpected reviews list: %+v err=%v", listResp, err)
	}

	// 4. POST /api/admin/governance/review/{Id}/approve
	rec = doReq(mux, "POST", "/api/admin/governance/review/"+audit1.ID+"/approve", "admin", map[string]any{
		"Candidate": map[string]any{
			"ProviderId": "603",
			"Title":      "黑客帝国",
			"Year":       1999,
		},
		"LockFields": []string{"Name", "ProductionYear"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("approve review: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	// 5. POST /api/admin/governance/review/{Id}/dismiss
	rec = doReq(mux, "POST", "/api/admin/governance/review/"+audit2.ID+"/dismiss", "admin", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("dismiss review: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	// 6. POST /api/admin/governance/items/{Id}/lock
	rec = doReq(mux, "POST", "/api/admin/governance/items/"+movie.ID+"/lock", "admin", map[string]any{
		"LockedFields": []string{"Name", "Overview"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("lock fields: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	// Also test snake_case "fields" fallback
	rec = doReq(mux, "POST", "/api/admin/governance/items/"+movie.ID+"/lock", "admin", map[string]any{
		"fields": []string{"Name"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("lock fields fallback: want 200, got %d", rec.Code)
	}

	// 7. POST /api/admin/governance/items/{Id}/nfo
	rec = doReq(mux, "POST", "/api/admin/governance/items/"+movie.ID+"/nfo", "admin", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("write nfo: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var nfoRes coregov.NFOWriteResult
	if err := json.Unmarshal(rec.Body.Bytes(), &nfoRes); err != nil || nfoRes.RootTag != "movie" {
		t.Fatalf("unexpected nfo result: %+v err=%v", nfoRes, err)
	}

	// 8. POST /api/admin/governance/orphans/cleanup
	rec = doReq(mux, "POST", "/api/admin/governance/orphans/cleanup", "admin", map[string]any{
		"ItemIds": []string{orphan.ID},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("cleanup orphans: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var cleanRes coregov.OrphanCleanupResult
	if err := json.Unmarshal(rec.Body.Bytes(), &cleanRes); err != nil || cleanRes.Deleted != 1 {
		t.Fatalf("unexpected cleanup result: %+v err=%v", cleanRes, err)
	}
}

func TestGovernanceErrorBranchesAndPassthrough(t *testing.T) {
	mux, _, _, _ := setupHandlerTest(t, false)

	// Invalid JSON payloads -> 400
	if rec := doReq(mux, "POST", "/api/admin/governance/orphans/cleanup", "", "{bad"); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad cleanup json: want 400, got %d", rec.Code)
	}
	if rec := doReq(mux, "POST", "/api/admin/governance/orphans/cleanup", "", map[string]any{"ItemIds": []string{}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty cleanup ids: want 400, got %d", rec.Code)
	}
	if rec := doReq(mux, "POST", "/api/admin/governance/orphans/cleanup", "", map[string]any{"item_ids": []string{"nonexistent"}}); rec.Code != http.StatusOK {
		t.Fatalf("snake_case item_ids: want 200, got %d", rec.Code)
	}
	if rec := doReq(mux, "POST", "/api/admin/governance/review/nope/approve", "", "{bad"); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad approve json: want 400, got %d", rec.Code)
	}
	if rec := doReq(mux, "POST", "/api/admin/governance/review/nope/approve", "", map[string]any{}); rec.Code != http.StatusNotFound {
		t.Fatalf("missing approve id: want 404, got %d", rec.Code)
	}
	if rec := doReq(mux, "POST", "/api/admin/governance/review/nope/dismiss", "", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing dismiss id: want 404, got %d", rec.Code)
	}
	if rec := doReq(mux, "POST", "/api/admin/governance/items/nope/lock", "", "{bad"); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad lock json: want 400, got %d", rec.Code)
	}
	if rec := doReq(mux, "POST", "/api/admin/governance/items/nope/lock", "", map[string]any{"LockedFields": []string{"Name"}}); rec.Code != http.StatusNotFound {
		t.Fatalf("missing lock id: want 404, got %d", rec.Code)
	}
	if rec := doReq(mux, "POST", "/api/admin/governance/items/nope/nfo", "", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing nfo id: want 404, got %d", rec.Code)
	}
}

func TestGovernanceHandlerBadRequestAndInternalErrors(t *testing.T) {
	dataDir := t.TempDir()
	cfg, err := config.Load(dataDir)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	store, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	svc, err := coregov.NewService(coregov.Options{Store: store, Config: cfg})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	repo := sqlite.NewLibraryRepo(store)
	ctx := context.Background()
	lib, _ := repo.CreateLibrary(ctx, "Movies", "movies", []string{dataDir})
	noPathItem := &sqlite.ItemRecord{LibraryID: lib.ID, Type: "Movie", Name: "NoPath"}
	_ = repo.UpsertItem(ctx, noPathItem)

	mux := http.NewServeMux()
	New(svc).RegisterRoutes(mux, AuthFuncs{})

	// Empty path -> ErrItemPathEmpty -> 400 Bad Request
	if rec := doReq(mux, "POST", "/api/admin/governance/items/"+noPathItem.ID+"/nfo", "", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty path nfo: want 400, got %d", rec.Code)
	}

	// Close store to exercise 500 Internal Server Error paths across handlers
	_ = store.Close()

	for _, tc := range []struct {
		method string
		path   string
		body   any
	}{
		{"GET", "/api/admin/health", nil},
		{"GET", "/api/admin/governance/audit", nil},
		{"GET", "/api/admin/governance/review?status=all", nil},
		{"POST", "/api/admin/governance/orphans/cleanup", map[string]any{"ItemIds": []string{"x"}}},
		{"POST", "/api/admin/governance/items/x/nfo", nil},
	} {
		if rec := doReq(mux, tc.method, tc.path, "", tc.body); rec.Code != http.StatusInternalServerError {
			t.Fatalf("%s %s on closed store: want 500, got %d", tc.method, tc.path, rec.Code)
		}
	}
}
