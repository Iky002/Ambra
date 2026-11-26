package hls

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestHLSV0107AuditStrmBadGatewayAndPermissionDenied(t *testing.T) {
	tmp := t.TempDir()
	st, err := sqlite.Open(filepath.Join(tmp, "hls_v0107.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(st)
	cfg := config.Default(tmp)
	mgr := transcode.NewManager(cfg, transcode.WithCapabilities(transcode.SoftwareOnlyCapabilities()))
	defer mgr.Close()

	badStrmPath := filepath.Join(tmp, "blocked.strm")
	_ = os.WriteFile(badStrmPath, []byte("http://169.254.169.254/latest/meta-data/\n"), 0o644)

	item := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: "lib1", Type: "Movie", MediaType: "Video",
		Name: "Blocked STRM", Path: badStrmPath, IsStrm: true,
	}
	_ = repo.UpsertItem(ctx, item)

	h := New(repo, mgr, cfg)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	// 1. [M-14] Blocked SSRF .strm causes master.m3u8, main.m3u8, segment, and init.mp4 to return 502 Bad Gateway
	for _, ep := range []string{
		"/Videos/" + item.ID + "/master.m3u8",
		"/Videos/" + item.ID + "/main.m3u8",
		"/Videos/" + item.ID + "/hls1/main/0.ts",
		"/Videos/" + item.ID + "/init.mp4",
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, ep, nil))
		if rec.Code != http.StatusBadGateway {
			t.Fatalf("GET %s expected 502 for blocked .strm, got %d body=%q", ep, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "169.254.169.254") || strings.Contains(rec.Body.String(), tmp) {
			t.Fatalf("GET %s leaked internal URL/path: %q", ep, rec.Body.String())
		}
	}

	// Resolver error -> 502 Bad Gateway
	h.SetStreamResolver(func(_ context.Context, _ string) (string, error) {
		return "", errors.New("upstream auth secret token failure")
	})
	for _, ep := range []string{
		"/Videos/" + item.ID + "/master.m3u8",
		"/Videos/" + item.ID + "/main.m3u8",
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, ep, nil))
		if rec.Code != http.StatusBadGateway {
			t.Fatalf("GET %s expected 502 on resolver error, got %d", ep, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "secret token") {
			t.Fatalf("GET %s leaked raw resolver error: %q", ep, rec.Body.String())
		}
	}

	// 2. [Task 1 / M-7] serveHLSFile permission denied, missing file, directory
	missingRec := httptest.NewRecorder()
	serveHLSFile(missingRec, httptest.NewRequest(http.MethodGet, "/seg.ts", nil), filepath.Join(tmp, "nope.ts"))
	if missingRec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing segment, got %d", missingRec.Code)
	}
	dirRec := httptest.NewRecorder()
	serveHLSFile(dirRec, httptest.NewRequest(http.MethodGet, "/seg.ts", nil), tmp)
	if dirRec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for directory segment, got %d", dirRec.Code)
	}

	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		var logBuf bytes.Buffer
		oldLogger := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, nil)))
		t.Cleanup(func() { slog.SetDefault(oldLogger) })

		lockedSeg := filepath.Join(tmp, "locked.ts")
		_ = os.WriteFile(lockedSeg, []byte("TS"), 0o644)
		_ = os.Chmod(lockedSeg, 0o000)
		t.Cleanup(func() { _ = os.Chmod(lockedSeg, 0o644) })

		recPerm := httptest.NewRecorder()
		serveHLSFile(recPerm, httptest.NewRequest(http.MethodGet, "/seg.ts", nil), lockedSeg)
		if recPerm.Code != http.StatusForbidden {
			t.Fatalf("expected 403 on unreadable segment, got %d body=%q", recPerm.Code, recPerm.Body.String())
		}
		if !strings.Contains(recPerm.Body.String(), "permission denied reading media file") || strings.Contains(recPerm.Body.String(), tmp) {
			t.Fatalf("unexpected permission response: %q", recPerm.Body.String())
		}
		logOut := logBuf.String()
		if !strings.Contains(logOut, "uid=") || !strings.Contains(logOut, "gid=") || !strings.Contains(logOut, lockedSeg) {
			t.Fatalf("expected uid/gid/path in slog output, got %q", logOut)
		}
	}

	// 3. Invalid segment id -> sanitized 400
	recBadSeg := httptest.NewRecorder()
	mux.ServeHTTP(recBadSeg, httptest.NewRequest(http.MethodGet, "/Videos/"+item.ID+"/hls1/main/notanumber.ts", nil))
	if recBadSeg.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid segment id, got %d", recBadSeg.Code)
	}
}
