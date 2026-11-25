package sync

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func doSyncAuthReq(t *testing.T, mux *http.ServeMux, ac *middleware.AuthContext, method, target string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, target, &buf)
	req.Header.Set("Content-Type", "application/json")
	if ac != nil {
		req = req.WithContext(middleware.WithAuthContext(req.Context(), ac))
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func makeSyncAuth(id, name string, isAdmin bool) *middleware.AuthContext {
	return &middleware.AuthContext{
		User: &sqlite.UserRecord{
			ID:      guid.Normalize(id),
			Name:    name,
			IsAdmin: isAdmin,
			Policy:  sqlite.DefaultUserPolicy(isAdmin),
		},
	}
}

func TestSyncRBACOwnershipAndAdminAccess(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default(dir)
	st, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	h := New(st, cfg)
	mux := http.NewServeMux()
	authMW := middleware.NewAuth(nil, nil)
	h.RegisterRoutes(mux, AuthFuncs{
		RequireAuth:  authMW.RequireAuth,
		RequireAdmin: authMW.RequireAdmin,
	})

	alice := makeSyncAuth("alice-id", "alice", false)
	bob := makeSyncAuth("bob-id", "bob", false)
	admin := makeSyncAuth("admin-id", "admin", true)

	// Alice creates a job (even if trying to spoof bob's UserId, must be bound to Alice).
	rec := doSyncAuthReq(t, mux, alice, http.MethodPost, "/Sync/Jobs", map[string]any{
		"Name":     "Alice Offline",
		"TargetId": "alice-ipad",
		"UserId":   bob.UserID(),
		"ItemIds":  []string{"item-1"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /Sync/Jobs status=%d", rec.Code)
	}
	var aliceJob SyncJobCreationResult
	_ = json.Unmarshal(rec.Body.Bytes(), &aliceJob)
	if aliceJob.Job.UserId != alice.UserID() {
		t.Fatalf("expected job owner %q, got %q", alice.UserID(), aliceJob.Job.UserId)
	}

	// Bob creates his own job.
	rec = doSyncAuthReq(t, mux, bob, http.MethodPost, "/Sync/Jobs", map[string]any{
		"Name":     "Bob Offline",
		"TargetId": "bob-phone",
		"ItemIds":  []string{"item-2"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /Sync/Jobs bob status=%d", rec.Code)
	}

	// Non-admin GET /Sync/Jobs only sees their own jobs even if passing another UserId query param.
	rec = doSyncAuthReq(t, mux, bob, http.MethodGet, "/Sync/Jobs?UserId="+alice.UserID(), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /Sync/Jobs bob status=%d", rec.Code)
	}
	var bobJobs dto.QueryResult[SyncJob]
	_ = json.Unmarshal(rec.Body.Bytes(), &bobJobs)
	if bobJobs.TotalRecordCount != 1 || bobJobs.Items[0].UserId != bob.UserID() {
		t.Fatalf("expected only Bob's job, got %+v", bobJobs)
	}

	// Admin GET /Sync/Jobs sees all jobs.
	rec = doSyncAuthReq(t, mux, admin, http.MethodGet, "/Sync/Jobs", nil)
	var adminJobs dto.QueryResult[SyncJob]
	_ = json.Unmarshal(rec.Body.Bytes(), &adminJobs)
	if adminJobs.TotalRecordCount != 2 {
		t.Fatalf("expected admin to see 2 jobs, got %d", adminJobs.TotalRecordCount)
	}

	// Bob gets 403 on all of Alice's Job & JobItem endpoints.
	jiID := aliceJob.JobItems[0].Id
	forbiddenChecks := []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/Sync/Jobs/" + aliceJob.Job.Id, nil},
		{http.MethodPost, "/Sync/Jobs/" + aliceJob.Job.Id, map[string]any{"Name": "Hacked"}},
		{http.MethodDelete, "/Sync/Jobs/" + aliceJob.Job.Id, nil},
		{http.MethodGet, "/Sync/JobItems?JobId=" + aliceJob.Job.Id, nil},
		{http.MethodPost, "/Sync/JobItems/" + jiID + "/Transferred", nil},
		{http.MethodPost, "/Sync/JobItems/" + jiID + "/Enable", nil},
		{http.MethodDelete, "/Sync/JobItems/" + jiID, nil},
	}
	for _, tc := range forbiddenChecks {
		r := doSyncAuthReq(t, mux, bob, tc.method, tc.path, tc.body)
		if r.Code != http.StatusForbidden {
			t.Fatalf("%s %s by Bob: expected 403, got %d", tc.method, tc.path, r.Code)
		}
	}

	// Admin can access and update Alice's job & items.
	if r := doSyncAuthReq(t, mux, admin, http.MethodGet, "/Sync/Jobs/"+aliceJob.Job.Id, nil); r.Code != http.StatusOK {
		t.Fatalf("admin GET Alice job: expected 200, got %d", r.Code)
	}
	if r := doSyncAuthReq(t, mux, admin, http.MethodPost, "/Sync/JobItems/"+jiID+"/Transferred", nil); r.Code != http.StatusNoContent {
		t.Fatalf("admin Transferred Alice item: expected 204, got %d", r.Code)
	}
}

func TestSyncLibraryHydrationAndRealTargets(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default(dir)
	st, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	ctx := context.Background()
	libRepo := sqlite.NewLibraryRepo(st)
	userRepo := sqlite.NewUserRepo(st)

	// Create a library, item, and media source.
	lib, err := libRepo.CreateLibrary(ctx, "Movies", "movies", []string{dir})
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}
	itemID := guid.New()
	if err := libRepo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:        itemID,
		LibraryID: lib.ID,
		ParentID:  lib.ID,
		Type:      "Movie",
		MediaType: "Video",
		Name:      "Inception",
		Path:      "/media/movies/Inception.mkv",
	}); err != nil {
		t.Fatalf("UpsertItem: %v", err)
	}
	if err := libRepo.ReplaceMediaSourcesAndStreams(ctx, itemID, []sqlite.MediaSourceRecord{
		{
			ID:        "ms-inception-1",
			ItemID:    itemID,
			Name:      "Inception.mkv",
			Path:      "/media/movies/Inception.mkv",
			Container: "mkv",
		},
	}); err != nil {
		t.Fatalf("ReplaceMediaSourcesAndStreams: %v", err)
	}

	// Create a user & device token so ListDevices returns a real device.
	u := &sqlite.UserRecord{
		Name:   "tester",
		Policy: sqlite.DefaultUserPolicy(false),
		Config: sqlite.DefaultUserConfig(),
	}
	if err := userRepo.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	h := New(st, cfg)
	h.SetLibraryRepo(libRepo)
	h.SetUserRepo(userRepo)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	// Before any device tokens exist -> falls back to local-device.
	rec := doReq(t, mux, http.MethodGet, "/Sync/Targets", nil)
	var targets []SyncTarget
	_ = json.Unmarshal(rec.Body.Bytes(), &targets)
	if len(targets) != 1 || targets[0].Id != "local-device" {
		t.Fatalf("expected fallback target, got %+v", targets)
	}

	// Register a real device token -> GET /Sync/Targets returns real device.
	if err := userRepo.CreateAccessToken(ctx, &sqlite.AccessTokenRecord{
		Token:      "tok-1",
		UserID:     u.ID,
		DeviceID:   "dev-iphone-15",
		DeviceName: "iPhone 15 Pro",
		AppName:    "Infuse",
		AppVersion: "8.0",
	}); err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}

	rec = doReq(t, mux, http.MethodGet, "/Sync/Targets", nil)
	targets = nil
	_ = json.Unmarshal(rec.Body.Bytes(), &targets)
	if len(targets) != 1 || targets[0].Id != "dev-iphone-15" || targets[0].Name != "iPhone 15 Pro" {
		t.Fatalf("expected real device target, got %+v", targets)
	}

	// POST /Sync/Jobs populates real ItemName, OutputPath, MediaSourceId.
	rec = doReq(t, mux, http.MethodPost, "/Sync/Jobs", map[string]any{
		"TargetId": "dev-iphone-15",
		"Name":     "Flight Movies",
		"ItemIds":  []string{itemID},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /Sync/Jobs status=%d", rec.Code)
	}
	var created SyncJobCreationResult
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if len(created.JobItems) != 1 {
		t.Fatalf("expected 1 job item, got %d", len(created.JobItems))
	}
	ji := created.JobItems[0]
	if ji.ItemName != "Inception" || ji.OutputPath != "/media/movies/Inception.mkv" || ji.MediaSource != "ms-inception-1" {
		t.Fatalf("expected real hydrated item metadata, got %+v", ji)
	}

	// GET /Sync/Items/Ready includes real Item DTO and OriginalFileName.
	rec = doReq(t, mux, http.MethodGet, "/Sync/Items/Ready?TargetId=dev-iphone-15", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /Sync/Items/Ready status=%d", rec.Code)
	}
	var ready []SyncedItem
	_ = json.Unmarshal(rec.Body.Bytes(), &ready)
	if len(ready) != 1 || ready[0].Item == nil || ready[0].Item.Name != "Inception" || ready[0].OriginalFile != "Inception.mkv" {
		t.Fatalf("expected hydrated ready item DTO, got %+v", ready)
	}
}
