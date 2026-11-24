package logs

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/config"
)

func TestLogsHandlerAndTraversalProtection(t *testing.T) {
	tmp := t.TempDir()
	logDir := filepath.Join(tmp, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	h := New(config.Default(tmp))
	h.RegisterRoutes(mux, AuthFuncs{})

	// 1. Empty log dir falls back to runtime log
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/System/Logs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /System/Logs status=%d", rec.Code)
	}
	var list []LogFileDto
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 1 || list[0].Name != defaultRuntimeLogName {
		t.Fatalf("expected runtime log fallback, got %+v", list)
	}

	// 2. Write a real log file and test pagination by lines
	content := "line-1\nline-2\nline-3\nline-4\nline-5\n"
	if err := os.WriteFile(filepath.Join(logDir, "server.log"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "secret.txt"), []byte("top-secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/System/Logs/Query", nil))
	var qr dto.QueryResult[LogFileDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &qr)
	if qr.TotalRecordCount != 1 || qr.Items[0].Name != "server.log" {
		t.Fatalf("expected server.log, got %+v", qr)
	}

	// Read full log
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/System/Logs/server.log", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "line-3") {
		t.Fatalf("unexpected log content: %d %q", rec.Code, rec.Body.String())
	}

	// Read paginated lines (StartIndex=1, Limit=2 -> line-2, line-3)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/System/Logs/server.log/Lines?StartIndex=1&Limit=2", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET Lines status=%d", rec.Code)
	}
	var linesRes dto.QueryResult[string]
	_ = json.Unmarshal(rec.Body.Bytes(), &linesRes)
	if linesRes.TotalRecordCount != 5 || len(linesRes.Items) != 2 || linesRes.Items[0] != "line-2" || linesRes.Items[1] != "line-3" {
		t.Fatalf("unexpected lines page: %+v", linesRes)
	}

	// 3. Path traversal protection
	for _, bad := range []string{"..%2Fsecret.txt", "..\\secret.txt", ".."} {
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/System/Logs/"+bad, nil))
		if rec.Code == http.StatusOK {
			t.Fatalf("expected traversal attempt %q to be rejected, got 200: %s", bad, rec.Body.String())
		}
	}
}
