package environment

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
)

func TestEnvironmentV0109AuditL13RejectNonRegularWritable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("FIFO not applicable on windows")
	}
	dir := t.TempDir()
	fifoPath := filepath.Join(dir, "pipe.fifo")
	if err := syscall.Mkfifo(fifoPath, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}

	h := New(config.Default(dir))
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	body, _ := json.Marshal(ValidatePathRequest{
		Path:             fifoPath,
		ValidateWritable: true,
	})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Environment/ValidatePath", bytes.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("L-13: expected 400 rejecting FIFO named pipe without blocking, got %d (%s)", rec.Code, rec.Body.String())
	}
}
