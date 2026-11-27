package logs

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/config"
)

func TestLogsCovV0112_SanitizeLogLineAllBranches(t *testing.T) {
	in := strings.Join([]string{
		`api_key=sec123 Token="tok456" "Password":"pw789" Bearer jwt.tok.1 user@example.org`,
		`loopback6=[::1]:8096 pub6=[2001:4860:4860::8888]:443 bad6=[:::invalid]:80`,
		`rawLoop6=::1 rawPub6=2001:db8::1234`,
		`loop4=127.0.0.1:8096 ver=` + config.ServerVersion + ` bad4=999.999.999.999 pub4=8.8.8.8:53`,
	}, " | ")

	got := SanitizeLogLine(in)
	for _, mustContain := range []string{
		"api_key=[REDACTED]",
		`Token="[REDACTED]"`,
		`"Password":"[REDACTED]"`,
		"Bearer [REDACTED]",
		"[REDACTED_EMAIL]",
		"[::1]:8096",
		"[:::invalid]:80",
		"rawLoop6=::1",
		"loop4=127.0.0.1:8096",
		"ver=" + config.ServerVersion,
		"bad4=999.999.999.999",
		"[REDACTED_IP]",
	} {
		if !strings.Contains(got, mustContain) {
			t.Fatalf("expected sanitized line to contain %q, got:\n%s", mustContain, got)
		}
	}
	for _, mustRedact := range []string{"sec123", "tok456", "pw789", "jwt.tok.1", "user@example.org", "2001:4860:4860::8888", "2001:db8::1234", "8.8.8.8"} {
		if strings.Contains(got, mustRedact) {
			t.Fatalf("expected %q to be redacted, got:\n%s", mustRedact, got)
		}
	}
}

func TestLogsCovV0112_SlogBridgeRetentionGroupsAndPruning(t *testing.T) {
	if p := DailyLogFilePath("   ", time.Time{}); p != "" {
		t.Fatalf("expected empty path for blank dataDir, got %q", p)
	}
	dir := t.TempDir()
	autoPath := DailyLogFilePath(dir, time.Time{})
	if !strings.Contains(autoPath, "ambra-") || !strings.HasSuffix(autoPath, ".log") {
		t.Fatalf("unexpected DailyLogFilePath with zero time: %q", autoPath)
	}

	// Seed logs dir with old daily log, fresh daily log, non-daily file, invalid date file, subdir, and symlink.
	logsDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(filepath.Join(logsDir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldFile := filepath.Join(logsDir, "ambra-2020-01-01.log")
	freshFile := filepath.Join(logsDir, "ambra-2026-11-25.log")
	otherFile := filepath.Join(logsDir, "other.txt")
	badDateFile := filepath.Join(logsDir, "ambra-2026-99-99.log")
	for _, f := range []string{oldFile, freshFile, otherFile, badDateFile} {
		if err := os.WriteFile(f, []byte("log\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.Symlink(otherFile, filepath.Join(logsDir, "ambra-2019-01-01.log"))

	var stdout bytes.Buffer
	lh := New(nil)
	startLogPath := filepath.Join(logsDir, "ambra-2026-11-26.log")
	mh := NewMultiSlogHandler(&stdout, nil, startLogPath).WithRetentionDays(5).WithLevel(slog.LevelDebug)
	mh.SetLogsHandler(lh)
	defer mh.Close()

	// WithGroup("") returns receiver; WithGroup("api") + WithAttrs + nested Group + empty Group + zero Attr
	if mh.WithGroup("") != mh {
		t.Fatal("WithGroup(\"\") should return same handler")
	}
	grouped := mh.WithGroup("api").WithAttrs([]slog.Attr{slog.String("ver", "1")})
	logger := slog.New(grouped)

	rec := slog.NewRecord(time.Date(2026, 11, 27, 1, 2, 3, 0, time.UTC), slog.LevelInfo, "hello\nworld", 0)
	rec.AddAttrs(
		slog.Attr{},
		slog.Group("empty_grp"),
		slog.Group("sub", slog.Int("code", 200)),
	)
	if err := grouped.Handle(context.Background(), rec); err != nil {
		t.Fatalf("grouped.Handle failed: %v", err)
	}
	logger.Debug("zero-timecheck")

	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Fatalf("expected old daily log to be pruned, stat err=%v", err)
	}
	if _, err := os.Stat(freshFile); err != nil {
		t.Fatalf("expected fresh daily log to be retained, stat err=%v", err)
	}
	if !strings.Contains(stdout.String(), "api.ver=1") || !strings.Contains(stdout.String(), "api.sub.code=200") {
		t.Fatalf("expected formatted group attrs in stdout, got:\n%s", stdout.String())
	}
}

func TestLogsCovV0112_HandlerRuntimeAndErrorBranches(t *testing.T) {
	// 1. Nil config handler serves runtime log with pagination & sanitize
	hNil := New(nil)
	for i := 0; i < 2005; i++ {
		hNil.AppendRuntimeLog("line from 8.8.8.8 api_key=abc")
	}

	muxNil := http.NewServeMux()
	hNil.RegisterRoutes(muxNil, AuthFuncs{})

	// Runtime log via /System/Logs/Log?name=ambra-runtime.log&Sanitize=true
	r1 := httptest.NewRequest(http.MethodGet, "/System/Logs/Log?name=ambra-runtime.log&Sanitize=true", nil)
	w1 := httptest.NewRecorder()
	muxNil.ServeHTTP(w1, r1)
	if w1.Code != http.StatusOK || !strings.Contains(w1.Body.String(), "[REDACTED_IP]") {
		t.Fatalf("expected 200 sanitized runtime log, got code=%d", w1.Code)
	}

	// Missing ?name= -> 400
	rMissing := httptest.NewRequest(http.MethodGet, "/System/Logs/Log", nil)
	wMissing := httptest.NewRecorder()
	muxNil.ServeHTTP(wMissing, rMissing)
	if wMissing.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing name, got %d", wMissing.Code)
	}

	// Runtime log lines pagination: StartIndex < 0, Limit=2, Sanitize=true, and StartIndex >= total
	page, total, err := hNil.readLogLinesPage(defaultRuntimeLogName, -5, 2, true)
	if err != nil || total != 2000 || len(page) != 2 || !strings.Contains(page[0], "[REDACTED]") {
		t.Fatalf("unexpected runtime page: len=%d total=%d err=%v", len(page), total, err)
	}
	emptyPage, _, err := hNil.readLogLinesPage(defaultRuntimeLogName, 5000, 10, false)
	if err != nil || len(emptyPage) != 0 {
		t.Fatalf("expected empty page for start>=total, got len=%d err=%v", len(emptyPage), err)
	}

	// Query logs pagination edges
	for _, u := range []string{"/System/Logs/Query?StartIndex=-2&Limit=1", "/System/Logs/Query?StartIndex=99&Limit=1"} {
		req := httptest.NewRequest(http.MethodGet, u, nil)
		rec := httptest.NewRecorder()
		muxNil.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s failed with %d", u, rec.Code)
		}
	}

	// 2. Real DataDir logs: bounded huge line, symlink/dir forbidden (403), invalid name (400), missing (404), generic 500
	dataDir := t.TempDir()
	logsDir := filepath.Join(dataDir, "logs")
	if err := os.MkdirAll(filepath.Join(logsDir, "nested-dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	hugeLine := strings.Repeat("A", maxLogLineBytes+4096) + " 8.8.4.4"
	bigLogPath := filepath.Join(logsDir, "big.log")
	if err := os.WriteFile(bigLogPath, []byte(hugeLine), 0o600); err != nil { // no trailing newline tests EOF gotData branch
		t.Fatal(err)
	}

	cfg := config.Default(dataDir)
	h := New(cfg)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	// Sanitized GET /System/Logs/big.log?Sanitize=true & /Lines
	for _, u := range []string{
		"/System/Logs/big.log?Sanitize=true",
		"/System/Logs/big.log/Lines?StartIndex=0&Limit=5000&Sanitize=true",
	} {
		req := httptest.NewRequest(http.MethodGet, u, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s expected 200, got %d", u, rec.Code)
		}
	}

	// Directory inside logs -> 403 Forbidden
	reqDir := httptest.NewRequest(http.MethodGet, "/System/Logs/nested-dir", nil)
	recDir := httptest.NewRecorder()
	mux.ServeHTTP(recDir, reqDir)
	if recDir.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for directory log name, got %d", recDir.Code)
	}

	// Invalid name ".." -> 400 Bad Request
	wInvalid := httptest.NewRecorder()
	h.serveLogByName(wInvalid, "bad..name", false)
	if wInvalid.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid log name, got %d", wInvalid.Code)
	}

	// Missing file -> 404 Not Found
	wNotFound := httptest.NewRecorder()
	h.serveLogByName(wNotFound, "missing.log", false)
	if wNotFound.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing log, got %d", wNotFound.Code)
	}

	// writeLogError default 500 branch
	w500 := httptest.NewRecorder()
	writeLogError(w500, errors.New("unexpected io failure"))
	if w500.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for generic error, got %d", w500.Code)
	}
}
