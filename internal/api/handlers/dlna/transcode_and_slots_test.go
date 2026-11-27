package dlna

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestDLNAModeScopedTokensBoundedSemaphoreAndSharedRangeSlot(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "dlna.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	libDir := filepath.Join(tmp, "media")
	_ = os.MkdirAll(libDir, 0o755)
	vidPath := filepath.Join(libDir, "sample.avi")
	_ = os.WriteFile(vidPath, []byte("0123456789abcdefghijklmnopqrstuvwxyz"), 0o644)

	ctx := context.Background()
	libRepo := sqlite.NewLibraryRepo(store)
	lib, err := libRepo.CreateLibrary(ctx, "Movies", "movies", []string{libDir})
	if err != nil {
		t.Fatal(err)
	}
	item := &sqlite.ItemRecord{
		LibraryID: lib.ID,
		Type:      "Movie",
		MediaType: "Video",
		Name:      "Sample AVI",
		Path:      vidPath,
		Container: "avi",
	}
	if err := libRepo.UpsertItem(ctx, item); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default(tmp)
	cfg.EnableDLNA = true
	h := New(cfg, libRepo, store)
	mgr := transcode.NewManager(cfg, transcode.WithCapabilities(transcode.SoftwareOnlyCapabilities()))
	defer mgr.Close()
	h.SetTranscodeManager(mgr)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	exp := time.Now().UTC().Add(time.Hour).Unix()
	expStr := strconv.FormatInt(exp, 10)

	// 1. Direct token cannot be upgraded to transcode=1.
	directSig := h.signStreamToken(item.ID, exp)
	req := httptest.NewRequest(http.MethodGet, "/Dlna/srv-1/stream/"+item.ID+"?exp="+expStr+"&sig="+directSig+"&transcode=1", nil)
	req.RemoteAddr = "192.168.1.50:12345"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when direct token used with transcode=1, got %d", rec.Code)
	}

	// 2. Bounded transcodeSem enforces concurrency even when SimultaneousStreamLimit == 0.
	cfg.SimultaneousStreamLimit = 0
	for i := 0; i < cap(h.transcodeSem); i++ {
		h.transcodeSem <- struct{}{}
	}
	tcSig := h.signStreamTokenForMode(item.ID, exp, true)
	reqSat := httptest.NewRequest(http.MethodGet, "/Dlna/srv-1/stream/"+item.ID+"?exp="+expStr+"&sig="+tcSig+"&transcode=1", nil)
	reqSat.RemoteAddr = "192.168.1.50:12345"
	recSat := httptest.NewRecorder()
	mux.ServeHTTP(recSat, reqSat)
	if recSat.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 when transcodeSem saturated, got %d", recSat.Code)
	}
	for i := 0; i < cap(h.transcodeSem); i++ {
		<-h.transcodeSem
	}

	// 3. Overlapping direct Range requests from the same LAN IP share 1 SimultaneousStreamLimit slot.
	cfg.SimultaneousStreamLimit = 1
	r1 := httptest.NewRequest(http.MethodGet, "/Dlna/srv-1/stream/"+item.ID+"?exp="+expStr+"&sig="+directSig, nil)
	r1.RemoteAddr = "192.168.1.50:12345"
	rel1, ok1 := h.acquireDLNAStreamSlot(r1, item.ID, false)
	if !ok1 {
		t.Fatal("expected first direct slot acquisition to succeed")
	}
	defer rel1()

	r2 := httptest.NewRequest(http.MethodGet, "/Dlna/srv-1/stream/"+item.ID+"?exp="+expStr+"&sig="+directSig, nil)
	r2.RemoteAddr = "192.168.1.50:54321"
	rel2, ok2 := h.acquireDLNAStreamSlot(r2, item.ID, false)
	if !ok2 {
		t.Fatal("expected overlapping Range request from same LAN IP & item to share 1 slot")
	}
	rel2()

	rOther := httptest.NewRequest(http.MethodGet, "/Dlna/srv-1/stream/"+item.ID+"?exp="+expStr+"&sig="+directSig, nil)
	rOther.RemoteAddr = "192.168.1.99:54321"
	if rel3, ok3 := h.acquireDLNAStreamSlot(rOther, item.ID, false); ok3 {
		rel3()
		t.Fatal("expected different LAN IP to be blocked when SimultaneousStreamLimit=1 is occupied")
	}
}
