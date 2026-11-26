package imaging

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestAuditV0109_L25_StartupCacheSizeAsyncPruneAndPascalCaseTag(t *testing.T) {
	tmp := t.TempDir()
	cacheDir := filepath.Join(tmp, "cache")
	if err := os.MkdirAll(filepath.Join(cacheDir, "aa", "bb"), 0o700); err != nil {
		t.Fatal(err)
	}
	// Pre-populate disk cache before Service initialization.
	existing := make([]byte, 4096)
	if err := os.WriteFile(filepath.Join(cacheDir, "aa", "bb", "aabbccdd.jpg"), existing, 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := sqlite.Open(filepath.Join(tmp, "img.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	repo := sqlite.NewLibraryRepo(store)

	svc := New(repo).WithCacheDir(cacheDir).WithMaxCacheBytes(2048)

	// Wait briefly for async startup scan to count pre-existing cache bytes.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if svc.approxCacheBytes.Load() >= 4096 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := svc.approxCacheBytes.Load(); got < 4096 {
		t.Fatalf("expected startup async scan to observe >=4096 bytes, got %d", got)
	}

	// PascalCase Tag query param must produce immutable Cache-Control just like lowercase tag.
	imgPath := filepath.Join(tmp, "poster.png")
	if err := os.WriteFile(imgPath, []byte("\x89PNG\r\n\x1a\nfake-png-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertImage(context.Background(), &sqlite.ImageRecord{
		ItemID:     "item-tag",
		ImageType:  "Primary",
		ImageIndex: 0,
		Path:       imgPath,
		Tag:        "tag123",
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/Items/item-tag/Images/Primary?Tag=tag123", nil)
	rec := httptest.NewRecorder()
	if err := svc.ServeHTTP(rec, req, "item-tag", "Primary", 0); err != nil {
		t.Fatalf("ServeHTTP: %v", err)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Fatalf("expected immutable Cache-Control for PascalCase Tag, got %q", cc)
	}
}
