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
