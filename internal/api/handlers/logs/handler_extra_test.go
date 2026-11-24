package logs

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/config"
)

func TestLogsHandlerExtraBranches(t *testing.T) {
	// 1. Nil cfg -> logDir() == "", resolveSafeLogPath returns ErrNotExist, listLogFiles returns runtime log
	hNil := New(nil)
	if hNil.logDir() != "" {
		t.Fatalf("expected empty logDir for nil cfg")
	}
	if _, err := hNil.resolveSafeLogPath("a.log"); !os.IsNotExist(err) {
		t.Fatalf("expected ErrNotExist for nil cfg, got %v", err)
	}
	// Append >2000 lines to exercise ring truncation
	for i := 0; i < 2005; i++ {
		hNil.AppendRuntimeLog(fmt.Sprintf("line-%d", i))
	}
	if len(hNil.runtimeLines) != 2000 {
		t.Fatalf("expected runtimeLines capped at 2000, got %d", len(hNil.runtimeLines))
	}

	// 2. Empty logs dir -> fallback to in-memory ambra-runtime.log
	tmp := t.TempDir()
	h := New(config.Default(tmp))
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/System/Logs", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), defaultRuntimeLogName) {
		t.Fatalf("expected fallback runtime log in list, got %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/System/Logs/"+defaultRuntimeLogName, nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Ambra Media Server") {
		t.Fatalf("expected runtime log content, got %d %s", rec.Code, rec.Body.String())
	}

	// 3. Real log files (including empty file, subdir to skip, same mtime tie-break)
	logDir := filepath.Join(tmp, "logs")
	_ = os.MkdirAll(filepath.Join(logDir, "subdir"), 0o755)
	f1 := filepath.Join(logDir, "a.log")
	f2 := filepath.Join(logDir, "b.log")
	fEmpty := filepath.Join(logDir, "empty.log")
	_ = os.WriteFile(f1, []byte("l1\nl2\nl3\n"), 0o644)
	_ = os.WriteFile(f2, []byte("b1\n"), 0o644)
	_ = os.WriteFile(fEmpty, []byte(""), 0o644)
	sameTime := time.Now().UTC().Truncate(time.Second)
	_ = os.Chtimes(f1, sameTime, sameTime)
	_ = os.Chtimes(f2, sameTime, sameTime)

	// Query logs: StartIndex < 0, StartIndex >= total, Limit truncation
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/System/Logs/Query?StartIndex=-1&Limit=1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("QueryLogs status=%d", rec.Code)
	}
	var qRes dto.QueryResult[LogFileDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &qRes)
	if len(qRes.Items) != 1 || qRes.TotalRecordCount != 3 {
		t.Fatalf("unexpected QueryLogs result: %+v", qRes)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/System/Logs/Query?StartIndex=99", nil))
	qRes = dto.QueryResult[LogFileDto]{}
	_ = json.Unmarshal(rec.Body.Bytes(), &qRes)
	if len(qRes.Items) != 0 {
		t.Fatalf("expected empty page when StartIndex >= total")
	}

	// Empty log file
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/System/Logs/empty.log", nil))
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Fatalf("expected empty 200 for empty.log, got %d %q", rec.Code, rec.Body.String())
	}

	// Not found (404) & path traversal (400) for both GetLog and GetLogLines
	for _, path := range []string{
		"/System/Logs/missing.log",
		"/System/Logs/missing.log/Lines",
	} {
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s expected 404, got %d", path, rec.Code)
		}
	}
	for _, bad := range []string{"..evil", "a\\b"} {
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/System/Logs/"+bad, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("GET /System/Logs/%s expected 400, got %d", bad, rec.Code)
		}
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/System/Logs/"+bad+"/Lines", nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("GET /System/Logs/%s/Lines expected 400, got %d", bad, rec.Code)
		}
	}

	// GetLogLines pagination edge cases (StartIndex < 0, StartIndex >= total, Limit)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/System/Logs/a.log/Lines?StartIndex=-2&Limit=2", nil))
	var lRes dto.QueryResult[string]
	_ = json.Unmarshal(rec.Body.Bytes(), &lRes)
	if len(lRes.Items) != 2 || lRes.TotalRecordCount != 3 {
		t.Fatalf("unexpected GetLogLines result: %+v", lRes)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/System/Logs/a.log/Lines?StartIndex=100", nil))
	lRes = dto.QueryResult[string]{}
	_ = json.Unmarshal(rec.Body.Bytes(), &lRes)
	if len(lRes.Items) != 0 {
		t.Fatalf("expected empty lines when StartIndex >= total")
	}
}
