package sync

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func setupSyncTest(t *testing.T) (*Handler, *http.ServeMux, *sqlite.Store, *config.Config) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default(dir)
	st, err := sqlite.Open(cfg.DBPath())
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	h := New(st, cfg)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{
		UserIDFromCtx: func(ctx context.Context) string { return "user-1" },
	})
	return h, mux, st, cfg
}

func doReq(t *testing.T, mux *http.ServeMux, method, target string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		switch v := body.(type) {
		case string:
			buf.WriteString(v)
		default:
			_ = json.NewEncoder(&buf).Encode(v)
		}
	}
	req := httptest.NewRequest(method, target, &buf)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestSyncFullLifecycle(t *testing.T) {
	_, mux, st, cfg := setupSyncTest(t)

	// Targets & Options
	if r := doReq(t, mux, http.MethodGet, "/Sync/Targets", nil); r.Code != http.StatusOK {
		t.Fatalf("GET /Sync/Targets status=%d", r.Code)
	}
	if r := doReq(t, mux, http.MethodGet, "/Sync/Options", nil); r.Code != http.StatusOK {
		t.Fatalf("GET /Sync/Options status=%d", r.Code)
	}

	// Create job bad payload
	if r := doReq(t, mux, http.MethodPost, "/Sync/Jobs", "{bad"); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", r.Code)
	}

	// Create job with empty items (Queued status)
	res := doReq(t, mux, http.MethodPost, "/Sync/Jobs", map[string]any{})
	if res.Code != http.StatusOK {
		t.Fatalf("POST /Sync/Jobs empty status=%d", res.Code)
	}

	// Create job with 2 items
	res = doReq(t, mux, http.MethodPost, "/Sync/Jobs", map[string]any{
		"TargetId": "ipad-pro",
		"Name":     "Offline Movies",
		"ItemIds":  []string{"item-a", "item-b"},
		"Quality":  "high",
	})
	if res.Code != http.StatusOK {
		t.Fatalf("POST /Sync/Jobs status=%d body=%s", res.Code, res.Body.String())
	}
	var created SyncJobCreationResult
	_ = json.Unmarshal(res.Body.Bytes(), &created)
	if created.Job.Id == "" || len(created.JobItems) != 2 {
		t.Fatalf("unexpected created job: %+v", created)
	}

	// List Jobs with filters
	res = doReq(t, mux, http.MethodGet, "/Sync/Jobs?TargetId=ipad-pro&UserId=user-1&StartIndex=0&Limit=10", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("GET /Sync/Jobs status=%d", res.Code)
	}
	var jobsRes dto.QueryResult[SyncJob]
	_ = json.Unmarshal(res.Body.Bytes(), &jobsRes)
	if jobsRes.TotalRecordCount != 1 {
		t.Fatalf("expected 1 filtered job, got %d", jobsRes.TotalRecordCount)
	}

	// Get job & 404
	if r := doReq(t, mux, http.MethodGet, "/Sync/Jobs/"+created.Job.Id, nil); r.Code != http.StatusOK {
		t.Fatalf("GET /Sync/Jobs/{Id} status=%d", r.Code)
	}
	if r := doReq(t, mux, http.MethodGet, "/Sync/Jobs/missing", nil); r.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", r.Code)
	}

	// Update job
	if r := doReq(t, mux, http.MethodPost, "/Sync/Jobs/"+created.Job.Id, "{bad"); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", r.Code)
	}
	bitrate := 4000000
	unwatched := true
	syncNew := true
	limit := 10
	res = doReq(t, mux, http.MethodPost, "/Sync/Jobs/"+created.Job.Id, map[string]any{
		"Name":           "Offline Movies 2",
		"Quality":        "medium",
		"Bitrate":        bitrate,
		"UnwatchedOnly":  unwatched,
		"SyncNewContent": syncNew,
		"ItemLimit":      limit,
		"Status":         "ReadyToTransfer",
	})
	if res.Code != http.StatusOK {
		t.Fatalf("POST /Sync/Jobs/{Id} status=%d", res.Code)
	}
	if r := doReq(t, mux, http.MethodPost, "/Sync/Jobs/missing", map[string]any{"Name": "x"}); r.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", r.Code)
	}

	// List JobItems
	res = doReq(t, mux, http.MethodGet, "/Sync/JobItems?JobId="+created.Job.Id+"&TargetId=ipad-pro&Statuses=ReadyToTransfer", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("GET /Sync/JobItems status=%d", res.Code)
	}
	var itemsRes dto.QueryResult[SyncJobItem]
	_ = json.Unmarshal(res.Body.Bytes(), &itemsRes)
	if itemsRes.TotalRecordCount != 2 {
		t.Fatalf("expected 2 job items, got %d", itemsRes.TotalRecordCount)
	}

	// Ready items
	res = doReq(t, mux, http.MethodGet, "/Sync/Items/Ready?TargetId=ipad-pro", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("GET /Sync/Items/Ready status=%d", res.Code)
	}
	var ready []SyncedItem
	_ = json.Unmarshal(res.Body.Bytes(), &ready)
	if len(ready) != 2 {
		t.Fatalf("expected 2 ready items, got %d", len(ready))
	}

	// Mark item 1 transferred, then re-enable, then transfer both
	ji1 := created.JobItems[0].Id
	ji2 := created.JobItems[1].Id
	if r := doReq(t, mux, http.MethodPost, "/Sync/JobItems/"+ji1+"/Transferred", nil); r.Code != http.StatusNoContent {
		t.Fatalf("POST Transferred status=%d", r.Code)
	}
	if r := doReq(t, mux, http.MethodPost, "/Sync/JobItems/missing/Transferred", nil); r.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", r.Code)
	}
	if r := doReq(t, mux, http.MethodPost, "/Sync/JobItems/"+ji1+"/Enable", nil); r.Code != http.StatusNoContent {
		t.Fatalf("POST Enable status=%d", r.Code)
	}
	if r := doReq(t, mux, http.MethodPost, "/Sync/JobItems/missing/Enable", nil); r.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", r.Code)
	}
	if r := doReq(t, mux, http.MethodPost, "/Sync/JobItems/"+ji1+"/Transferred", nil); r.Code != http.StatusNoContent {
		t.Fatalf("POST Transferred ji1 status=%d", r.Code)
	}
	if r := doReq(t, mux, http.MethodPost, "/Sync/JobItems/"+ji2+"/Transferred", nil); r.Code != http.StatusNoContent {
		t.Fatalf("POST Transferred ji2 status=%d", r.Code)
	}

	// Verify job completed
	res = doReq(t, mux, http.MethodGet, "/Sync/Jobs/"+created.Job.Id, nil)
	var doneJob SyncJob
	_ = json.Unmarshal(res.Body.Bytes(), &doneJob)
	if doneJob.Status != "Completed" || doneJob.Progress != 100 {
		t.Fatalf("expected Completed/100, got %+v", doneJob)
	}

	// Sync Data reconciliation
	if r := doReq(t, mux, http.MethodPost, "/Sync/Data", "{bad"); r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", r.Code)
	}
	res = doReq(t, mux, http.MethodPost, "/Sync/Data", map[string]any{
		"TargetId":     "ipad-pro",
		"LocalItemIds": []string{"item-a", "stale-item"},
	})
	if res.Code != http.StatusOK {
		t.Fatalf("POST /Sync/Data status=%d", res.Code)
	}
	var dataRes struct {
		ItemIdsToRemove []string `json:"ItemIdsToRemove"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &dataRes)
	if len(dataRes.ItemIdsToRemove) != 1 || dataRes.ItemIdsToRemove[0] != "stale-item" {
		t.Fatalf("unexpected ItemIdsToRemove: %+v", dataRes.ItemIdsToRemove)
	}

	// Delete single JobItem & 404
	if r := doReq(t, mux, http.MethodDelete, "/Sync/JobItems/"+ji1, nil); r.Code != http.StatusNoContent {
		t.Fatalf("DELETE /Sync/JobItems/{Id} status=%d", r.Code)
	}
	if r := doReq(t, mux, http.MethodDelete, "/Sync/JobItems/"+ji1, nil); r.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", r.Code)
	}
	// Delete second JobItem -> triggers total == 0 branch in recomputeJobLocked
	if r := doReq(t, mux, http.MethodDelete, "/Sync/JobItems/"+ji2, nil); r.Code != http.StatusNoContent {
		t.Fatalf("DELETE ji2 status=%d", r.Code)
	}

	// Reload from SQLite
	reloaded := New(st, cfg)
	if len(reloaded.state.Jobs) != 2 {
		t.Fatalf("expected 2 persisted jobs, got %d", len(reloaded.state.Jobs))
	}

	// Delete Job & 404
	if r := doReq(t, mux, http.MethodDelete, "/Sync/Jobs/"+created.Job.Id, nil); r.Code != http.StatusNoContent {
		t.Fatalf("DELETE /Sync/Jobs/{Id} status=%d", r.Code)
	}
	if r := doReq(t, mux, http.MethodDelete, "/Sync/Jobs/"+created.Job.Id, nil); r.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", r.Code)
	}

	// Nil store/cfg & paginate edge cases
	nilH := New(nil, nil)
	if nilH.serverID() != "" {
		t.Fatalf("expected empty serverID")
	}
	p, tot := paginate([]int{1, 2, 3}, -1, 2)
	if tot != 3 || len(p) != 2 {
		t.Fatalf("unexpected paginate: %v %d", p, tot)
	}
	p, tot = paginate([]int{1, 2, 3}, 10, 2)
	if tot != 3 || len(p) != 0 {
		t.Fatalf("unexpected paginate beyond end: %v %d", p, tot)
	}
}
