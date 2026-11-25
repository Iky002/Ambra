package migration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	coremigration "github.com/ambra-media/ambra/internal/core/migration"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func setupHandlerTest(t *testing.T, auth AuthFuncs) (*http.ServeMux, *httptest.Server, *coremigration.Migrator, chan struct{}) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "handler.db")
	st, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	userRepo := sqlite.NewUserRepo(st)
	libRepo := sqlite.NewLibraryRepo(st)
	ctx := context.Background()
	lib, _ := libRepo.CreateLibrary(ctx, "Movies", "movies", []string{"/movies"})
	_ = libRepo.UpsertItem(ctx, &sqlite.ItemRecord{
		LibraryID: lib.ID,
		Type:      "Movie",
		Name:      "The Matrix",
		Extra:     sqlite.ItemExtra{ProviderIDs: map[string]string{"Tmdb": "603"}},
	})

	blockCh := make(chan struct{})
	sourceSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Emby-Token") != "valid-key" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("unauthorized"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/System/Info":
			_ = json.NewEncoder(w).Encode(coremigration.SourceServerInfo{
				ID:         "legacy-srv",
				ServerName: "Old Emby",
				Version:    "4.8.10",
			})
		case "/Users":
			_ = json.NewEncoder(w).Encode([]coremigration.SourceUser{
				{ID: "u1", Name: "Neo", Policy: sqlite.DefaultUserPolicy(false), Configuration: sqlite.DefaultUserConfig()},
			})
		case "/Library/VirtualFolders":
			_ = json.NewEncoder(w).Encode([]coremigration.SourceVirtualFolder{
				{Name: "Movies", ItemID: "f1", CollectionType: "movies", Locations: []string{"/movies"}},
			})
		case "/Users/u1/Items":
			select {
			case <-blockCh:
			default:
			}
			_ = json.NewEncoder(w).Encode(coremigration.SourceItemsPage{
				TotalRecordCount: 1,
				Items: []coremigration.SourceItem{
					{
						ID:          "m1",
						Name:        "The Matrix",
						Type:        "Movie",
						ProviderIDs: map[string]string{"Tmdb": "603"},
						UserData:    &coremigration.SourceUserData{Played: true, IsFavorite: true},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(sourceSrv.Close)

	migrator := coremigration.NewMigrator(st, userRepo, coremigration.WithClientFactory(func(baseURL, apiKey string) (*coremigration.SourceClient, error) {
		return coremigration.NewSourceClient(baseURL, apiKey, coremigration.WithHTTPClient(sourceSrv.Client()), coremigration.WithBaseBackoff(time.Millisecond))
	}))

	mux := http.NewServeMux()
	h := New(migrator)
	h.RegisterRoutes(mux, auth)
	return mux, sourceSrv, migrator, blockCh
}

func doJSONReq(t *testing.T, mux *http.ServeMux, method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if s, ok := body.(string); ok {
			buf.WriteString(s)
		} else if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestMigrationHandlerAuthEnforcement(t *testing.T) {
	auth := AuthFuncs{
		RequireAuth: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Test-User") == "" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				next.ServeHTTP(w, r)
			})
		},
		RequireAdmin: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Test-Role") != "admin" {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				next.ServeHTTP(w, r)
			})
		},
	}

	mux, _, _, _ := setupHandlerTest(t, auth)

	// Unauthenticated -> 401
	rec := doJSONReq(t, mux, http.MethodGet, "/api/admin/migration/status", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}

	// Non-admin -> 403
	rec = doJSONReq(t, mux, http.MethodGet, "/api/admin/migration/status", nil, map[string]string{
		"X-Test-User": "bob",
		"X-Test-Role": "user",
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}

	// Admin -> 200
	rec = doJSONReq(t, mux, http.MethodGet, "/api/admin/migration/status", nil, map[string]string{
		"X-Test-User": "admin",
		"X-Test-Role": "admin",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestMigrationHandlerEndpointsFlow(t *testing.T) {
	mux, sourceSrv, _, _ := setupHandlerTest(t, AuthFuncs{})

	// 1. Validation errors (bad JSON & missing fields)
	for _, path := range []string{"/api/admin/migration/test", "/api/admin/migration/dry-run", "/api/admin/migration/start"} {
		rec := doJSONReq(t, mux, http.MethodPost, path, "{bad-json", nil)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s bad json: expected 400, got %d", path, rec.Code)
		}
		rec = doJSONReq(t, mux, http.MethodPost, path, map[string]string{"source_url": ""}, nil)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s missing fields: expected 400, got %d", path, rec.Code)
		}
	}

	// Invalid URL format -> 400
	rec := doJSONReq(t, mux, http.MethodPost, "/api/admin/migration/test", map[string]string{
		"source_url": "ftp://bad",
		"api_key":    "valid-key",
	}, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid URL, got %d", rec.Code)
	}

	// Wrong API key -> 502
	rec = doJSONReq(t, mux, http.MethodPost, "/api/admin/migration/test", map[string]string{
		"source_url": sourceSrv.URL,
		"api_key":    "wrong-key",
	}, nil)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 for wrong source key, got %d", rec.Code)
	}

	// 2. POST /api/admin/migration/test -> 200
	rec = doJSONReq(t, mux, http.MethodPost, "/api/admin/migration/test", map[string]string{
		"source_url": sourceSrv.URL,
		"api_key":    "valid-key",
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("test endpoint expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var testRes coremigration.SourceTestResult
	if err := json.NewDecoder(rec.Body).Decode(&testRes); err != nil {
		t.Fatalf("decode test response: %v", err)
	}
	if testRes.Server.ServerName != "Old Emby" || len(testRes.Users) != 1 || len(testRes.VirtualFolders) != 1 {
		t.Fatalf("unexpected testRes: %+v", testRes)
	}

	// 3. POST /api/admin/migration/dry-run -> 200
	rec = doJSONReq(t, mux, http.MethodPost, "/api/admin/migration/dry-run", coremigration.MigrationRequest{
		SourceURL:        sourceSrv.URL,
		APIKey:           "valid-key",
		MigrateUsers:     true,
		MigratePlayed:    true,
		MigrateFavorites: true,
		MigrateResume:    true,
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("dry-run expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var dryRep coremigration.MigrationReport
	if err := json.NewDecoder(rec.Body).Decode(&dryRep); err != nil {
		t.Fatalf("decode dry-run: %v", err)
	}
	if !dryRep.DryRun || dryRep.ItemsMatched != 1 || dryRep.UsersCreated != 1 {
		t.Fatalf("unexpected dryRep: %+v", dryRep)
	}

	// 4. POST /api/admin/migration/start?sync=true -> 200
	rec = doJSONReq(t, mux, http.MethodPost, "/api/admin/migration/start?sync=true", coremigration.MigrationRequest{
		SourceURL:        sourceSrv.URL,
		APIKey:           "valid-key",
		MigrateUsers:     true,
		MigratePlayed:    true,
		MigrateFavorites: true,
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("sync start expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var syncRep coremigration.MigrationReport
	_ = json.NewDecoder(rec.Body).Decode(&syncRep)
	if syncRep.Status != coremigration.StatusCompleted || syncRep.PlayedMigrated != 1 {
		t.Fatalf("unexpected syncRep: %+v", syncRep)
	}

	// 5. POST /api/admin/migration/start (async) -> 202
	rec = doJSONReq(t, mux, http.MethodPost, "/api/admin/migration/start", coremigration.MigrationRequest{
		SourceURL:     sourceSrv.URL,
		APIKey:        "valid-key",
		MigrateUsers:  true,
		MigratePlayed: true,
	}, nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("async start expected 202, got %d: %s", rec.Code, rec.Body.String())
	}

	// Wait for completion via GET /api/admin/migration/status
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		sRec := doJSONReq(t, mux, http.MethodGet, "/api/admin/migration/status", nil, nil)
		if sRec.Code != http.StatusOK {
			t.Fatalf("status expected 200, got %d", sRec.Code)
		}
		var stRep coremigration.MigrationReport
		_ = json.NewDecoder(sRec.Body).Decode(&stRep)
		if stRep.Status == coremigration.StatusCompleted {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}

	// 6. POST /api/admin/migration/cancel when idle -> 200 with cancelled=false
	rec = doJSONReq(t, mux, http.MethodPost, "/api/admin/migration/cancel", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel expected 200, got %d", rec.Code)
	}
	var cancelResp struct {
		Cancelled bool                          `json:"cancelled"`
		Report    coremigration.MigrationReport `json:"report"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&cancelResp)
	if cancelResp.Cancelled {
		t.Fatalf("expected cancelled=false when idle")
	}
}

func TestMigrationHandlerConflictAndErrorMapping(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "conflict.db")
	st, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	gate := make(chan struct{})
	entered := make(chan struct{}, 1)
	slowSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/System/Info":
			_ = json.NewEncoder(w).Encode(coremigration.SourceServerInfo{ID: "s1", ServerName: "Slow"})
		case "/Users":
			select {
			case entered <- struct{}{}:
			default:
			}
			<-gate
			_ = json.NewEncoder(w).Encode([]coremigration.SourceUser{})
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	t.Cleanup(slowSrv.Close)

	migrator := coremigration.NewMigrator(st, sqlite.NewUserRepo(st))
	mux := http.NewServeMux()
	New(migrator).RegisterRoutes(mux, AuthFuncs{})

	// Start async job that blocks on /Users
	rec := doJSONReq(t, mux, http.MethodPost, "/api/admin/migration/start", coremigration.MigrationRequest{
		SourceURL: slowSrv.URL,
		APIKey:    "k",
	}, nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rec.Code)
	}
	<-entered

	// Second start while running -> 409 Conflict
	rec2 := doJSONReq(t, mux, http.MethodPost, "/api/admin/migration/start", coremigration.MigrationRequest{
		SourceURL: slowSrv.URL,
		APIKey:    "k",
	}, nil)
	if rec2.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict, got %d", rec2.Code)
	}

	// Dry-run while running -> 409 Conflict
	rec3 := doJSONReq(t, mux, http.MethodPost, "/api/admin/migration/dry-run", coremigration.MigrationRequest{
		SourceURL: slowSrv.URL,
		APIKey:    "k",
	}, nil)
	if rec3.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict on dry-run, got %d", rec3.Code)
	}

	// Cancel running job -> 200 with cancelled=true
	cRec := doJSONReq(t, mux, http.MethodPost, "/api/admin/migration/cancel", nil, nil)
	if cRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on cancel, got %d", cRec.Code)
	}
	var cResp map[string]any
	_ = json.NewDecoder(cRec.Body).Decode(&cResp)
	if cResp["cancelled"] != true {
		t.Fatalf("expected cancelled=true, got %+v", cResp)
	}
	close(gate)

	// Wait until finished
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if migrator.Status().Status != coremigration.StatusRunning {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Non-401 APIError (500 from /Library/VirtualFolders) -> 502
	errRec := doJSONReq(t, mux, http.MethodPost, "/api/admin/migration/test", map[string]string{
		"source_url": slowSrv.URL,
		"api_key":    "k",
	}, nil)
	if errRec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 for upstream 500, got %d", errRec.Code)
	}

	// Strip URL userinfo & truncate long errors
	if got := stripURLUserinfo("https://admin:supersecret@emby.example.com:8096/emby"); got != "https://emby.example.com:8096/emby" {
		t.Fatalf("expected userinfo stripped, got %q", got)
	}
	longErr := make([]byte, 1024)
	for i := range longErr {
		longErr[i] = 'A'
	}
	trunc := truncateErrorMsg(string(longErr), maxUpstreamErrorBytes)
	if len(trunc) > maxUpstreamErrorBytes {
		t.Fatalf("expected truncated error <= %d bytes, got %d", maxUpstreamErrorBytes, len(trunc))
	}
}
