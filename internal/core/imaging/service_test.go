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

func TestImageServiceETagAnd304(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "img.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := sqlite.NewLibraryRepo(store)
	ctx := context.Background()

	itemID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:        itemID,
		LibraryID: "lib1",
		Type:      "Movie",
		Name:      "Poster Test",
	})

	imgPath := filepath.Join(tmp, "poster.png")
	pngData := []byte("\x89PNG\r\n\x1a\nfake-png-bytes")
	_ = os.WriteFile(imgPath, pngData, 0o644)

	_ = repo.ReplaceItemImages(ctx, itemID, []sqlite.ImageRecord{{
		ItemID:     itemID,
		ImageType:  "Primary",
		ImageIndex: 0,
		Path:       imgPath,
		Tag:        "abc123etag",
	}})

	svc := New(repo)

	// First request -> 200 OK + ETag
	req := httptest.NewRequest(http.MethodGet, "/Items/"+itemID+"/Images/Primary?tag=abc123etag", nil)
	rec := httptest.NewRecorder()
	if err := svc.ServeHTTP(rec, req, itemID, "Primary", 0); err != nil {
		t.Fatalf("ServeHTTP failed: %v", err)
	}
	if rec.Code != http.StatusOK || rec.Header().Get("ETag") != `"abc123etag"` {
		t.Fatalf("expected 200 with ETag, got code=%d etag=%s", rec.Code, rec.Header().Get("ETag"))
	}
	if rec.Body.String() != string(pngData) {
		t.Fatalf("unexpected image body")
	}

	// Second request with If-None-Match -> 304 Not Modified
	req2 := httptest.NewRequest(http.MethodGet, "/Items/"+itemID+"/Images/Primary", nil)
	req2.Header.Set("If-None-Match", `"abc123etag"`)
	rec2 := httptest.NewRecorder()
	if err := svc.ServeHTTP(rec2, req2, itemID, "Primary", 0); err != nil {
		t.Fatalf("ServeHTTP 304 failed: %v", err)
	}
	if rec2.Code != http.StatusNotModified {
		t.Fatalf("expected 304 Not Modified, got %d", rec2.Code)
	}
}
