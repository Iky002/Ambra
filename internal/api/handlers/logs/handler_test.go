package logs

import (
	"bytes"
	"encoding/json"
	"log/slog"
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

func TestMultiSlogHandlerBridgeAndFilePersistence(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Default(tmp)
	h := New(cfg)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	fixedDay := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	dailyLogPath := DailyLogFilePath(tmp, fixedDay)
	if !strings.HasSuffix(dailyLogPath, filepath.Join("logs", "ambra-2026-11-24.log")) {
		t.Fatalf("unexpected daily log path: %s", dailyLogPath)
	}
	if DailyLogFilePath("", fixedDay) != "" {
		t.Fatalf("expected empty daily log path for empty dataDir")
	}

	// Use a fixed non-daily log filename here because slog.Logger stamps records with
	// time.Now(), which would rotate a daily-named file away from fixedDay on any other
	// UTC date (daily rotation itself is tested deterministically in slog_rotation_test.go).
	const testLogName = "ambra-bridge.log"
	logFilePath := filepath.Join(tmp, "logs", testLogName)

	var stdoutBuf bytes.Buffer
	bridge := NewMultiSlogHandler(&stdoutBuf, nil, logFilePath).WithLevel(slog.LevelDebug)
	bridge.SetLogsHandler(h)
	defer func() { _ = bridge.Close() }()

	logger := slog.New(bridge).With("component", "transcode").WithGroup("req")
	logger.Info("server started", "addr", ":8096", "note", "hello world")
	logger.Warn("fallback engaged", "encoder", "libx264")
	logger.Debug("trace detail", "id", 42)

	// 1. Verify stdout output formatting
	outStr := stdoutBuf.String()
	if !strings.Contains(outStr, "[INFO] server started") ||
		!strings.Contains(outStr, "req.component=transcode") ||
		!strings.Contains(outStr, `req.note="hello world"`) ||
		!strings.Contains(outStr, "[WARN] fallback engaged req.component=transcode req.encoder=libx264") ||
		!strings.Contains(outStr, "[DEBUG] trace detail") {
		t.Fatalf("unexpected formatted stdout log:\n%s", outStr)
	}

	// 2. Verify GET /System/Logs lists ambra-bridge.log and GET /System/Logs/{Name} serves it
	recList := httptest.NewRecorder()
	mux.ServeHTTP(recList, httptest.NewRequest(http.MethodGet, "/System/Logs", nil))
	var list []LogFileDto
	_ = json.Unmarshal(recList.Body.Bytes(), &list)
	if len(list) != 1 || list[0].Name != testLogName {
		t.Fatalf("expected %s in /System/Logs, got %+v", testLogName, list)
	}

	recFile := httptest.NewRecorder()
	mux.ServeHTTP(recFile, httptest.NewRequest(http.MethodGet, "/System/Logs/"+testLogName, nil))
	if recFile.Code != http.StatusOK || !strings.Contains(recFile.Body.String(), "server started") || !strings.Contains(recFile.Body.String(), "fallback engaged") {
		t.Fatalf("expected persisted log content, got code=%d body=%q", recFile.Code, recFile.Body.String())
	}

	// 3. Verify in-memory runtime buffer also received the bridged lines
	h.mu.RLock()
	joinedMem := strings.Join(h.runtimeLines, "\n")
	h.mu.RUnlock()
	if !strings.Contains(joinedMem, "server started") || !strings.Contains(joinedMem, "fallback engaged") {
		t.Fatalf("expected in-memory runtimeLines to receive bridged logs, got:\n%s", joinedMem)
	}

	// 4. Close is idempotent
	if err := bridge.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}
	if err := bridge.Close(); err != nil {
		t.Fatalf("second Close failed: %v", err)
	}
}
