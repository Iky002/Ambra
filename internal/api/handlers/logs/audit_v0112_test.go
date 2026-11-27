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

func TestM06_MultiSlogHandlerClosePreventsFileReopen(t *testing.T) {
	dir := t.TempDir()
	day1 := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 11, 27, 1, 0, 0, 0, time.UTC)
	logPath := DailyLogFilePath(dir, day1)

	var stdout bytes.Buffer
	lh := New(config.Default(dir))
	sh := NewMultiSlogHandler(&stdout, lh, logPath)

	if err := sh.Handle(context.Background(), slog.NewRecord(day1, slog.LevelInfo, "before close", 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("expected log file created before Close, got %v", err)
	}
	if err := sh.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if err := os.Remove(logPath); err != nil {
		t.Fatalf("failed to remove closed log file: %v", err)
	}

	// Subsequent Handle calls (even on a new day that would trigger rotation) must NOT reopen or recreate disk log files!
	if err := sh.Handle(context.Background(), slog.NewRecord(day2, slog.LevelInfo, "after close", 0)); err != nil {
		t.Fatalf("Handle after Close returned error: %v", err)
	}
	nextDayPath := DailyLogFilePath(dir, day2)
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("expected original log file NOT recreated after Close, stat err=%v", err)
	}
	if _, err := os.Stat(nextDayPath); !os.IsNotExist(err) {
		t.Fatalf("expected rotated log file NOT created after Close, stat err=%v", err)
	}
}

func Test31_10_LogsSanitizeQueryParamRedactsSecretsEmailsAndIPs(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		t.Fatal(err)
	}

	rawLines := strings.Join([]string{
		`2026-11-26T12:00:00Z [INFO] server version=` + config.ServerVersion + ` local=127.0.0.1 loopback=::1`,
		`2026-11-26T12:00:01Z [WARN] request url=https://media.example.com/Videos/1/stream?api_key=sec_tok_999&X-Emby-Token=emby_tok_888 client_ip=203.0.113.45`,
		`2026-11-26T12:00:02Z [INFO] auth header MediaBrowser Token="hdr_tok_777" Bearer jwt.secret.token user=alice.user@example.org ipv6=2001:db8:85a3::8a2e:370:7334`,
		`2026-11-26T12:00:03Z [DEBUG] payload={"AccessToken":"json_tok_666","Pw":"mySecretPass"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(logDir, "ambra-2026-11-26.log"), []byte(rawLines), 0o600); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	h := New(config.Default(dir))
	h.RegisterRoutes(mux, AuthFuncs{})

	// 1. Raw download with ?Sanitize=true
	recRaw := httptest.NewRecorder()
	mux.ServeHTTP(recRaw, httptest.NewRequest(http.MethodGet, "/System/Logs/ambra-2026-11-26.log?Sanitize=true", nil))
	if recRaw.Code != http.StatusOK {
		t.Fatalf("GET /System/Logs/{Name}?Sanitize=true status=%d", recRaw.Code)
	}
	sanitized := recRaw.Body.String()
	for _, secret := range []string{
		"sec_tok_999", "emby_tok_888", "hdr_tok_777", "jwt.secret.token",
		"alice.user@example.org", "203.0.113.45", "2001:db8:85a3::8a2e:370:7334",
		"json_tok_666", "mySecretPass",
	} {
		if strings.Contains(sanitized, secret) {
			t.Fatalf("Sanitize=true leaked %q in output:\n%s", secret, sanitized)
		}
	}
	// Loopback and ServerVersion must remain intact!
	for _, preserved := range []string{config.ServerVersion, "127.0.0.1", "::1"} {
		if !strings.Contains(sanitized, preserved) {
			t.Fatalf("Sanitize=true unexpectedly stripped %q:\n%s", preserved, sanitized)
		}
	}

	// 2. Paginated /Lines with ?Sanitize=true
	recLines := httptest.NewRecorder()
	mux.ServeHTTP(recLines, httptest.NewRequest(http.MethodGet, "/System/Logs/ambra-2026-11-26.log/Lines?Sanitize=true", nil))
	if recLines.Code != http.StatusOK {
		t.Fatalf("GET /System/Logs/{Name}/Lines?Sanitize=true status=%d", recLines.Code)
	}
	var qr dto.QueryResult[string]
	if err := json.Unmarshal(recLines.Body.Bytes(), &qr); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(qr.Items, "\n")
	if strings.Contains(joined, "sec_tok_999") || strings.Contains(joined, "203.0.113.45") {
		t.Fatalf("Lines?Sanitize=true leaked sensitive data: %s", joined)
	}
}
