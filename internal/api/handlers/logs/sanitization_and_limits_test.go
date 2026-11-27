package logs

import (
	"bytes"
	"context"
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

func TestLogsV0104ANSIEscapeAndSymlinkSkipInList(t *testing.T) {
	tmp := t.TempDir()
	logDir := filepath.Join(tmp, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// 1. ANSI ESC (\x1b) escaped in slog message, attribute key/value, AppendRuntimeLog, and readLogLinesPage
	var buf bytes.Buffer
	h := New(config.Default(tmp))
	bridge := NewMultiSlogHandler(&buf, h, "")
	logger := slog.New(bridge)
	logger.Info("alert \x1b[31mRED\x1b[0m", "k\x1b[1m", "\x1b[32mgreen\x1b[0m")

	out := buf.String()
	if strings.Contains(out, "\x1b") {
		t.Fatalf("expected raw ANSI ESC byte escaped in slog output, got %q", out)
	}
	if !strings.Contains(out, `\x1b[31mRED\x1b[0m`) {
		t.Fatalf("expected escaped ANSI sequence in message, got %q", out)
	}

	h.AppendRuntimeLog("direct \x1b[2Jclear\r\nfake")
	h.mu.RLock()
	lastLine := h.runtimeLines[len(h.runtimeLines)-1]
	h.mu.RUnlock()
	if strings.ContainsAny(lastLine, "\x1b\r\n") {
		t.Fatalf("AppendRuntimeLog did not escape control chars: %q", lastLine)
	}

	// File containing raw ESC byte is escaped when read via /Lines
	realLog := filepath.Join(logDir, "app.log")
	_ = os.WriteFile(realLog, []byte("ok line\nansi \x1b[33mwarn\x1b[0m\n"), 0o600)

	// 2. listLogFiles MUST skip symlinks
	secretFile := filepath.Join(tmp, "secret.txt")
	_ = os.WriteFile(secretFile, []byte("top-secret"), 0o600)
	_ = os.Symlink(secretFile, filepath.Join(logDir, "symlink.log"))

	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	recList := httptest.NewRecorder()
	mux.ServeHTTP(recList, httptest.NewRequest(http.MethodGet, "/System/Logs", nil))
	if recList.Code != http.StatusOK {
		t.Fatalf("GET /System/Logs status=%d", recList.Code)
	}
	var listed []LogFileDto
	_ = json.Unmarshal(recList.Body.Bytes(), &listed)
	if len(listed) != 1 || listed[0].Name != "app.log" {
		t.Fatalf("expected symlink.log skipped in listLogFiles, got %+v", listed)
	}

	recLines := httptest.NewRecorder()
	mux.ServeHTTP(recLines, httptest.NewRequest(http.MethodGet, "/System/Logs/app.log/Lines", nil))
	var linesRes dto.QueryResult[string]
	_ = json.Unmarshal(recLines.Body.Bytes(), &linesRes)
	if len(linesRes.Items) != 2 || strings.Contains(linesRes.Items[1], "\x1b") {
		t.Fatalf("expected ESC escaped in /Lines response, got %+v", linesRes.Items)
	}
}

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

func TestLogsV0109AuditL14LineCapNoSniffControlCharsAndRetention(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Default(tmp)
	h := New(cfg)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	logDir := filepath.Join(tmp, "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		t.Fatal(err)
	}

	// 1. L-14: Single line length bounded to maxLogLineBytes (64 KiB) without OOM
	hugeLine := strings.Repeat("A", maxLogLineBytes+20000)
	logFile := filepath.Join(logDir, "huge.log")
	if err := os.WriteFile(logFile, []byte(hugeLine+"\nsecond-line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	recLines := httptest.NewRecorder()
	mux.ServeHTTP(recLines, httptest.NewRequest(http.MethodGet, "/System/Logs/huge.log/Lines?StartIndex=0&Limit=10", nil))
	if recLines.Code != http.StatusOK {
		t.Fatalf("GET Lines failed: %d %s", recLines.Code, recLines.Body.String())
	}
	var qr dto.QueryResult[string]
	if err := json.Unmarshal(recLines.Body.Bytes(), &qr); err != nil {
		t.Fatal(err)
	}
	if qr.TotalRecordCount != 2 || len(qr.Items) != 2 {
		t.Fatalf("L-14: expected 2 lines counted, got %+v", qr)
	}
	if len(qr.Items[0]) > maxLogLineBytes {
		t.Fatalf("L-14: expected first line bounded to %d bytes, got %d", maxLogLineBytes, len(qr.Items[0]))
	}
	if qr.Items[1] != "second-line" {
		t.Fatalf("L-14: expected second line intact, got %q", qr.Items[1])
	}

	// 2. L-14: serveLogByName sets X-Content-Type-Options: nosniff (both file and runtime log)
	recFile := httptest.NewRecorder()
	mux.ServeHTTP(recFile, httptest.NewRequest(http.MethodGet, "/System/Logs/huge.log", nil))
	if recFile.Code != http.StatusOK || recFile.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("L-14: expected nosniff on file log, status=%d hdr=%q", recFile.Code, recFile.Header().Get("X-Content-Type-Options"))
	}
	recRT := httptest.NewRecorder()
	mux.ServeHTTP(recRT, httptest.NewRequest(http.MethodGet, "/System/Logs/"+defaultRuntimeLogName, nil))
	if recRT.Code != http.StatusOK || recRT.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("L-14: expected nosniff on runtime log, status=%d hdr=%q", recRT.Code, recRT.Header().Get("X-Content-Type-Options"))
	}

	// 3. L-14: escapeLogControlChars escapes \x9b and \x00
	escaped := escapeLogControlChars("hello\x9b31m\x00world")
	if strings.ContainsAny(escaped, "\x9b\x00") || !strings.Contains(escaped, `\x9b`) || !strings.Contains(escaped, `\x00`) {
		t.Fatalf("L-14: expected \\x9b and \\x00 escaped, got %q", escaped)
	}

	// 4. L-14: Daily log rotation prunes logs older than retention window (default 30 days)
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	oldDate := now.AddDate(0, 0, -40).Format("2006-01-02")
	recentDate := now.AddDate(0, 0, -5).Format("2006-01-02")
	oldPath := filepath.Join(logDir, "ambra-"+oldDate+".log")
	recentPath := filepath.Join(logDir, "ambra-"+recentDate+".log")
	_ = os.WriteFile(oldPath, []byte("old\n"), 0o600)
	_ = os.WriteFile(recentPath, []byte("recent\n"), 0o600)

	todayPath := DailyLogFilePath(tmp, now)
	mh := NewMultiSlogHandler(nil, nil, todayPath)
	defer func() { _ = mh.Close() }()
	rec := slog.NewRecord(now, slog.LevelInfo, "retention check", 0)
	if err := mh.Handle(context.Background(), rec); err != nil {
		t.Fatalf("Handle failed: %v", err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("L-14: expected old daily log %s pruned, stat err=%v", oldPath, err)
	}
	if _, err := os.Stat(recentPath); err != nil {
		t.Fatalf("L-14: expected recent daily log %s preserved, stat err=%v", recentPath, err)
	}
}
