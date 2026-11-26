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

func TestLogsV0107AuditLongLineOver4MiB(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	_ = os.MkdirAll(logDir, 0o755)

	// Write a log file with a >4 MiB single line plus a trailing line without newline
	hugeLine := strings.Repeat("X", 4*1024*1024+1024)
	content := "first-line\r\n" + hugeLine + "\nlast-line"
	if err := os.WriteFile(filepath.Join(logDir, "huge.log"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	h := New(config.Default(dir))
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/System/Logs/huge.log/Lines?StartIndex=0&Limit=10", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 reading >4MiB log line, got %d (%s)", rec.Code, rec.Body.String())
	}
	var res dto.QueryResult[string]
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.TotalRecordCount != 3 || len(res.Items) != 3 {
		t.Fatalf("expected 3 lines, got total=%d len=%d", res.TotalRecordCount, len(res.Items))
	}
	if res.Items[0] != "first-line" || len(res.Items[1]) != maxLogLineBytes || res.Items[2] != "last-line" {
		t.Fatalf("unexpected parsed lines: first=%q len1=%d last=%q", res.Items[0], len(res.Items[1]), res.Items[2])
	}
}
