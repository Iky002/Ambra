package imaging

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestImageServiceExtraBranches(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "img2.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	ctx := context.Background()
	svc := New(repo)

	// Missing item/image -> error
	if _, err := svc.Load(ctx, "nonexistent", "Primary", 0); err == nil {
		t.Fatalf("expected error for missing image")
	}
	reqMiss := httptest.NewRequest(http.MethodGet, "/Items/nonexistent/Images/Primary", nil)
	if err := svc.ServeHTTP(httptest.NewRecorder(), reqMiss, "nonexistent", "Primary", 0); err == nil {
		t.Fatalf("expected ServeHTTP error for missing image")
	}

	// Item with image pointing to missing file on disk
	missingItemID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:        missingItemID,
		LibraryID: "lib1",
		Type:      "Movie",
		Name:      "Missing File",
	})
	_ = repo.ReplaceItemImages(ctx, missingItemID, []sqlite.ImageRecord{{
		ItemID:     missingItemID,
		ImageType:  "Primary",
		ImageIndex: 0,
		Path:       filepath.Join(tmp, "does_not_exist.jpg"),
		Tag:        "tagmissing",
	}})
	if _, err := svc.Load(ctx, missingItemID, "Primary", 0); err == nil {
		t.Fatalf("expected Load error when file missing on disk")
	}
	if err := svc.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil), missingItemID, "Primary", 0); err == nil {
		t.Fatalf("expected ServeHTTP error when file missing on disk")
	}

	// Valid item: no ?tag= (Cache-Control 86400), HEAD request, unquoted If-None-Match
	itemID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:        itemID,
		LibraryID: "lib1",
		Type:      "Movie",
		Name:      "JPEG Test",
	})
	jpgPath := filepath.Join(tmp, "poster.jpg")
	_ = os.WriteFile(jpgPath, []byte{0xff, 0xd8, 0xff, 0xe0}, 0o644)
	_ = repo.ReplaceItemImages(ctx, itemID, []sqlite.ImageRecord{{
		ItemID:     itemID,
		ImageType:  "Primary",
		ImageIndex: 0,
		Path:       jpgPath,
		Tag:        "jpgtag",
	}})

	reqHead := httptest.NewRequest(http.MethodHead, "/Items/"+itemID+"/Images/Primary", nil)
	recHead := httptest.NewRecorder()
	if err := svc.ServeHTTP(recHead, reqHead, itemID, "Primary", 0); err != nil {
		t.Fatalf("ServeHTTP HEAD failed: %v", err)
	}
	if recHead.Code != http.StatusOK || recHead.Body.Len() != 0 {
		t.Fatalf("expected 200 with empty body for HEAD, got code=%d len=%d", recHead.Code, recHead.Body.Len())
	}
	if recHead.Header().Get("Cache-Control") != "public, max-age=86400" {
		t.Fatalf("unexpected Cache-Control: %s", recHead.Header().Get("Cache-Control"))
	}

	// Unquoted If-None-Match
	req304 := httptest.NewRequest(http.MethodGet, "/Items/"+itemID+"/Images/Primary", nil)
	req304.Header.Set("If-None-Match", "jpgtag")
	rec304 := httptest.NewRecorder()
	if err := svc.ServeHTTP(rec304, req304, itemID, "Primary", 0); err != nil {
		t.Fatalf("ServeHTTP 304 failed: %v", err)
	}
	if rec304.Code != http.StatusNotModified {
		t.Fatalf("expected 304, got %d", rec304.Code)
	}
}

func TestContentTypeForImage(t *testing.T) {
	cases := []struct {
		path string
		data []byte
		want string
	}{
		{"a.jpg", nil, "image/jpeg"},
		{"a.JPEG", nil, "image/jpeg"},
		{"a.png", nil, "image/png"},
		{"a.webp", nil, "image/webp"},
		{"a.gif", nil, "image/gif"},
		{"a.bin", []byte("hello plain text"), "text/plain; charset=utf-8"},
	}
	for _, tc := range cases {
		if got := contentTypeForImage(tc.path, tc.data); got != tc.want {
			t.Errorf("contentTypeForImage(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}
