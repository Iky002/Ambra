package environment

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
)

func TestEnvironmentV0107AuditFileWritableAndDirCap(t *testing.T) {
	dir := t.TempDir()
	h := New(config.Default(dir))
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	// 1. ValidateWritable on regular file uses O_WRONLY|O_APPEND without creating temp files in parent dir
	writableFile := filepath.Join(dir, "writable.txt")
	_ = os.WriteFile(writableFile, []byte("abc"), 0o644)
	isFile := true
	body, _ := json.Marshal(ValidatePathRequest{Path: writableFile, ValidateWritable: true, IsFile: &isFile})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Environment/ValidatePath", bytes.NewReader(body)))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for writable regular file, got %d (%s)", rec.Code, rec.Body.String())
	}

	if os.Geteuid() != 0 {
		roFile := filepath.Join(dir, "readonly.txt")
		_ = os.WriteFile(roFile, []byte("abc"), 0o444)
		bodyRO, _ := json.Marshal(ValidatePathRequest{Path: roFile, ValidateWritable: true, IsFile: &isFile})
		recRO := httptest.NewRecorder()
		mux.ServeHTTP(recRO, httptest.NewRequest(http.MethodPost, "/Environment/ValidatePath", bytes.NewReader(bodyRO)))
		if recRO.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for read-only regular file, got %d", recRO.Code)
		}
	}

	// 2. handleDirectoryContents caps returned entries at maxDirectoryEntries (2000)
	bigDir := filepath.Join(dir, "many")
	_ = os.MkdirAll(bigDir, 0o755)
	for i := 0; i < maxDirectoryEntries+25; i++ {
		f, err := os.Create(filepath.Join(bigDir, fmt.Sprintf("f-%04d.txt", i)))
		if err == nil {
			_ = f.Close()
		}
	}
	recDir := httptest.NewRecorder()
	mux.ServeHTTP(recDir, httptest.NewRequest(http.MethodGet, "/Environment/DirectoryContents?path="+bigDir, nil))
	if recDir.Code != http.StatusOK {
		t.Fatalf("DirectoryContents status=%d", recDir.Code)
	}
	var entries []FileSystemEntryInfo
	if err := json.Unmarshal(recDir.Body.Bytes(), &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != maxDirectoryEntries {
		t.Fatalf("expected entries capped at %d, got %d", maxDirectoryEntries, len(entries))
	}
}

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
