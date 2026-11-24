package hls_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/core/transcode"
)

func TestLegacyHLSSegmentRoute(t *testing.T) {
	mux, mgr, itemID, msID := setupHLSTest(t)

	// Pre-register a session and seed seg_0.ts on disk so handleSegment serves it immediately without running ffmpeg.
	snap, err := mgr.RegisterSession(transcode.SessionParams{
		PlaySessionID:    "legacy-ps-1",
		ItemID:           itemID,
		MediaSourceID:    msID,
		VideoCodec:       "h264",
		AudioCodec:       "aac",
		SegmentContainer: "ts",
	})
	if err != nil {
		t.Fatalf("RegisterSession: %v", err)
	}
	segPath := filepath.Join(snap.WorkDir, "seg_0.ts")
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
