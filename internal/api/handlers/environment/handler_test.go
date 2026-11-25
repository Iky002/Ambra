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

func TestEnvironmentHandler(t *testing.T) {
	tmp := t.TempDir()
	subDir := filepath.Join(tmp, "movies")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	filePath := filepath.Join(tmp, "readme.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	h := New(config.Default(tmp))
	h.RegisterRoutes(mux, AuthFuncs{})

	// 1. GET /Environment/Drives
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Environment/Drives", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("Drives status=%d body=%s", rec.Code, rec.Body.String())
	}
	var drives []FileSystemEntryInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &drives); err != nil || len(drives) == 0 {
		t.Fatalf("expected non-empty drives, got %+v err=%v", drives, err)
	}

	// 2. GET /Environment/DirectoryContents (dirs only)
	rec = httptest.NewRecorder()
	u := "/Environment/DirectoryContents?path=" + url.QueryEscape(tmp) + "&includeDirectories=true&includeFiles=false"
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, u, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("DirectoryContents status=%d body=%s", rec.Code, rec.Body.String())
	}
	var entries []FileSystemEntryInfo
	_ = json.Unmarshal(rec.Body.Bytes(), &entries)
	if len(entries) != 1 || entries[0].Name != "movies" || entries[0].Type != "Directory" {
		t.Fatalf("expected only directory 'movies', got %+v", entries)
	}

	// 3. GET /Environment/DirectoryContents (files + dirs)
	rec = httptest.NewRecorder()
	u = "/Environment/DirectoryContents?path=" + url.QueryEscape(tmp) + "&includeDirectories=true&includeFiles=true"
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, u, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("DirectoryContents both status=%d", rec.Code)
	}
	entries = nil
	_ = json.Unmarshal(rec.Body.Bytes(), &entries)
	if len(entries) != 2 || entries[0].Type != "Directory" || entries[1].Type != "File" {
		t.Fatalf("expected [Directory, File], got %+v", entries)
	}

	// 4. GET /Environment/ParentPath
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Environment/ParentPath?path="+url.QueryEscape(subDir), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("ParentPath status=%d", rec.Code)
	}
	var parent string
	_ = json.Unmarshal(rec.Body.Bytes(), &parent)
	if parent != tmp {
		t.Fatalf("expected parent %q, got %q", tmp, parent)
	}

	// 5. GET /Environment/DefaultDirectoryBrowser & NetworkShares
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Environment/DefaultDirectoryBrowser", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("DefaultDirectoryBrowser status=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Environment/NetworkShares", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("NetworkShares status=%d", rec.Code)
	}

	// 6. POST /Environment/ValidatePath (valid writable dir)
	body, _ := json.Marshal(ValidatePathRequest{Path: subDir, ValidateWritable: true})
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Environment/ValidatePath", bytes.NewReader(body)))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("ValidatePath valid status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 7. POST /Environment/ValidatePath (non-existent path)
	body, _ = json.Marshal(ValidatePathRequest{Path: filepath.Join(tmp, "does-not-exist")})
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Environment/ValidatePath", bytes.NewReader(body)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("ValidatePath nonexistent expected 404, got %d", rec.Code)
	}

	// 8. Relative paths must be rejected with 400 across all path endpoints
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Environment/DirectoryContents?path=relative/dir", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("DirectoryContents relative path expected 400, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Environment/ParentPath?path=relative/dir", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("ParentPath relative path expected 400, got %d", rec.Code)
	}
	body, _ = json.Marshal(ValidatePathRequest{Path: "relative/dir"})
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/Environment/ValidatePath", bytes.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("ValidatePath relative path expected 400, got %d", rec.Code)
	}
}
