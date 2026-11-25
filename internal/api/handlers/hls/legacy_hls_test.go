package hls_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyHLSSegmentRoute(t *testing.T) {
	mux, mgr, itemID, msID := setupHLSTest(t)

	// Register the session via master.m3u8 so SessionParams match exact handler normalization, then seed seg_0.ts.
	masterRec := httptest.NewRecorder()
	mux.ServeHTTP(masterRec, httptest.NewRequest(http.MethodGet, "/Videos/"+itemID+"/master.m3u8?PlaySessionId=legacy-ps-1&MediaSourceId="+msID, nil))
	if masterRec.Code != http.StatusOK {
		t.Fatalf("master.m3u8 failed: %d", masterRec.Code)
	}
	sessions := mgr.ActiveSessions()
	if len(sessions) != 1 {
		t.Fatalf("expected 1 registered session, got %d", len(sessions))
	}
	segPath := filepath.Join(sessions[0].WorkDir, "seg_0.ts")
	if err := os.WriteFile(segPath, []byte("LEGACY_TS_PAYLOAD"), 0o644); err != nil {
		t.Fatalf("write seg_0.ts: %v", err)
	}

	// Request legacy /Videos/{Id}/hls/{PlaylistId}/{Segment} (without the "1" in hls1).
	req := httptest.NewRequest(http.MethodGet, "/Videos/"+itemID+"/hls/main/0.ts?PlaySessionId=legacy-ps-1&MediaSourceId="+msID, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on legacy /hls/ route, got %d body=%q", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "LEGACY_TS_PAYLOAD" {
		t.Fatalf("unexpected segment body: %q", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "video/mp2t" {
		t.Fatalf("expected Content-Type video/mp2t, got %q", ct)
	}
}
