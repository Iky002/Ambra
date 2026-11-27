package governance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	coregov "github.com/ambra-media/ambra/internal/core/governance"
	"github.com/ambra-media/ambra/internal/pkg/guid"
)

func TestGovernanceV0107AuditReviewPagination(t *testing.T) {
	mux, svc, _, _ := setupHandlerTest(t, false)
	ctx := context.Background()

	for _, name := range []string{"Item A", "Item B", "Item C"} {
		_, err := svc.EnqueueAudit(ctx, coregov.EnqueueAuditInput{
			ItemID:     guid.New(),
			ItemName:   name,
			ItemType:   "Movie",
			Confidence: 0.5,
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	// Page 1: StartIndex=1&Limit=1 -> returns 1 item, TotalRecordCount=3
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/admin/governance/review?StartIndex=1&Limit=1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("review page status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items            []map[string]any `json:"Items"`
		TotalRecordCount int              `json:"TotalRecordCount"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.TotalRecordCount != 3 || len(resp.Items) != 1 {
		t.Fatalf("expected TotalRecordCount=3 len=1, got %+v", resp)
	}

	// Beyond end: StartIndex=10&Limit=5 -> returns 0 items, TotalRecordCount=3
	recEmpty := httptest.NewRecorder()
	mux.ServeHTTP(recEmpty, httptest.NewRequest(http.MethodGet, "/api/admin/governance/review?StartIndex=10&Limit=5", nil))
	if recEmpty.Code != http.StatusOK {
		t.Fatalf("review out-of-range status=%d", recEmpty.Code)
	}
	resp.Items = nil
	_ = json.Unmarshal(recEmpty.Body.Bytes(), &resp)
	if resp.TotalRecordCount != 3 || len(resp.Items) != 0 {
		t.Fatalf("expected TotalRecordCount=3 len=0, got %+v", resp)
	}

	// Negative StartIndex clamped to 0
	recNeg := httptest.NewRecorder()
	mux.ServeHTTP(recNeg, httptest.NewRequest(http.MethodGet, "/api/admin/governance/review?StartIndex=-2&Limit=2", nil))
	if recNeg.Code != http.StatusOK {
		t.Fatalf("review negative StartIndex status=%d", recNeg.Code)
	}
	resp.Items = nil
	_ = json.Unmarshal(recNeg.Body.Bytes(), &resp)
	if resp.TotalRecordCount != 3 || len(resp.Items) != 2 {
		t.Fatalf("expected TotalRecordCount=3 len=2, got %+v", resp)
	}
}
