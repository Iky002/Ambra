package environment

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
)

func TestEnvironmentHandlerExtraBranches(t *testing.T) {
	tmp := t.TempDir()
	dirA := filepath.Join(tmp, "b_dir")
	dirB := filepath.Join(tmp, "a_dir")
	_ = os.MkdirAll(dirA, 0o755)
	_ = os.MkdirAll(dirB, 0o755)
	fileA := filepath.Join(tmp, "b.txt")
	fileB := filepath.Join(tmp, "a.txt")
	_ = os.WriteFile(fileA, []byte("b"), 0o644)
	_ = os.WriteFile(fileB, []byte("a"), 0o644)
	// Symlink to dirA
	_ = os.Symlink(dirA, filepath.Join(tmp, "link_dir"))

	mux := http.NewServeMux()
	h := New(config.Default(tmp))
	h.RegisterRoutes(mux, AuthFuncs{})

	// DirectoryContents: missing path -> 400, nonexistent -> 404, file -> 400
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Environment/DirectoryContents", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing path, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Environment/DirectoryContents?path="+url.QueryEscape(filepath.Join(tmp, "nope")), nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing dir, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Environment/DirectoryContents?path="+url.QueryEscape(fileA), nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for file path, got %d", rec.Code)
	}

	// Default (neither includeFiles nor includeDirectories specified) -> both included + sorted + symlink dir detected
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Environment/DirectoryContents?path="+url.QueryEscape(tmp), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("DirectoryContents default status=%d", rec.Code)
	}
	var entries []FileSystemEntryInfo
	_ = json.Unmarshal(rec.Body.Bytes(), &entries)
	if len(entries) != 5 {
		t.Fatalf("expected 5 entries (3 dirs + 2 files), got %+v", entries)
	}

	// Only files (includeFiles=true, includeDirectories=false) -> skips directories
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Environment/DirectoryContents?path="+url.QueryEscape(tmp)+"&includeFiles=true&includeDirectories=false", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("DirectoryContents files-only status=%d", rec.Code)
	}
	entries = nil
	_ = json.Unmarshal(rec.Body.Bytes(), &entries)
	if len(entries) != 2 || entries[0].Name != "a.txt" {
		t.Fatalf("expected 2 sorted files, got %+v", entries)
	}

	// ParentPath: empty path -> "", root "/" -> ""
	for _, p := range []string{"", "/"} {
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Environment/ParentPath?path="+url.QueryEscape(p), nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("ParentPath(%q) status=%d", p, rec.Code)
		}
		var out string
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		if out != "" {
			t.Fatalf("expected empty parent for %q, got %q", p, out)
		}
	}

	// ValidatePath error cases
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Environment/ValidatePath", bytes.NewReader([]byte(`bad-json`))))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad JSON, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Environment/ValidatePath", bytes.NewReader([]byte(`{"Path":""}`))))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty path, got %d", rec.Code)
	}

	trueVal, falseVal := true, false
	// IsFile=true but dir -> 400
	b1, _ := json.Marshal(ValidatePathRequest{Path: dirA, IsFile: &trueVal})
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Environment/ValidatePath", bytes.NewReader(b1)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when IsFile=true for dir, got %d", rec.Code)
	}

	// IsFile=false but file -> 400
	b2, _ := json.Marshal(ValidatePathRequest{Path: fileA, IsFile: &falseVal})
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Environment/ValidatePath", bytes.NewReader(b2)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when IsFile=false for file, got %d", rec.Code)
	}

	// ValidateWritable=true on a file -> checks parent dir -> 204
	b3, _ := json.Marshal(ValidatePathRequest{Path: fileA, IsFile: &trueVal, ValidateWritable: true})
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Environment/ValidatePath", bytes.NewReader(b3)))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for writable file parent, got %d", rec.Code)
	}

	// Read-only / unreadable dir tests (skip if running as root)
	if os.Geteuid() != 0 {
		roDir := filepath.Join(tmp, "ro_dir")
		_ = os.MkdirAll(roDir, 0o555)
		t.Cleanup(func() { _ = os.Chmod(roDir, 0o755) })
		b4, _ := json.Marshal(ValidatePathRequest{Path: roDir, ValidateWritable: true})
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Environment/ValidatePath", bytes.NewReader(b4)))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for read-only dir, got %d", rec.Code)
		}

		noReadDir := filepath.Join(tmp, "noread_dir")
		_ = os.MkdirAll(noReadDir, 0o000)
		t.Cleanup(func() { _ = os.Chmod(noReadDir, 0o755) })
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Environment/DirectoryContents?path="+url.QueryEscape(noReadDir), nil))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for unreadable dir, got %d", rec.Code)
		}
	}
}
