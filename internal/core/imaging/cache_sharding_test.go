package imaging

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func setupShardingTestService(t *testing.T) (*Service, *sqlite.LibraryRepo, string, string) {
	t.Helper()
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "shard.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	repo := sqlite.NewLibraryRepo(store)
	cacheDir := filepath.Join(tmp, "cache", "images")
	svc := New(repo).WithCacheDir(cacheDir)
	return svc, repo, tmp, cacheDir
}

func TestDiskCacheTwoLevelShardingWriteAndHit(t *testing.T) {
	svc, repo, tmp, cacheDir := setupShardingTestService(t)
	ctx := context.Background()

	itemID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:        itemID,
		LibraryID: "lib1",
		Type:      "Movie",
		Name:      "Sharded Poster",
	})

	imgPath := filepath.Join(tmp, "poster.jpg")
	origBytes := encodeJPEGBytes(t, makeTestImage(320, 180), 90)
	if err := os.WriteFile(imgPath, origBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	_ = repo.ReplaceItemImages(ctx, itemID, []sqlite.ImageRecord{{
		ItemID:     itemID,
		ImageType:  "Primary",
		ImageIndex: 0,
		Path:       imgPath,
		Tag:        "shardtag1",
	}})

	// 1. Write resized JPEG to 2-level sharded path
	jpgOpts := Options{MaxWidth: 160, Quality: 80}
	jpgPayload1, err := svc.GetImageWithOptions(ctx, itemID, "Primary", 0, jpgOpts)
	if err != nil {
		t.Fatalf("GetImageWithOptions jpeg failed: %v", err)
	}
	jpgKey := computeCacheKey(itemID, "Primary", 0, "shardtag1", jpgOpts.Normalized())
	jpgSharded := filepath.Join(cacheDir, jpgKey[:2], jpgKey[2:4], jpgKey+".jpg")
	if _, err := os.Stat(jpgSharded); err != nil {
		t.Fatalf("expected 2-level sharded jpeg at %s: %v", jpgSharded, err)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, jpgKey+".jpg")); !os.IsNotExist(err) {
		t.Fatalf("expected no flat cache file for jpeg")
	}
	if _, err := os.Stat(filepath.Join(cacheDir, jpgKey[:2], jpgKey+".jpg")); !os.IsNotExist(err) {
		t.Fatalf("expected no 1-level cache file for jpeg")
	}

	// 2. Write resized PNG to 2-level sharded path
	pngOpts := Options{FillWidth: 80, FillHeight: 80, Format: "png"}
	pngPayload1, err := svc.GetImageWithOptions(ctx, itemID, "Primary", 0, pngOpts)
	if err != nil {
		t.Fatalf("GetImageWithOptions png failed: %v", err)
	}
	pngKey := computeCacheKey(itemID, "Primary", 0, "shardtag1", pngOpts.Normalized())
	pngSharded := filepath.Join(cacheDir, pngKey[:2], pngKey[2:4], pngKey+".png")
	if _, err := os.Stat(pngSharded); err != nil {
		t.Fatalf("expected 2-level sharded png at %s: %v", pngSharded, err)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, pngKey+".png")); !os.IsNotExist(err) {
		t.Fatalf("expected no flat cache file for png")
	}

	// 3. Remove source image and verify subsequent requests hit 2-level sharded cache cleanly
	if err := os.Remove(imgPath); err != nil {
		t.Fatal(err)
	}
	countBefore := svc.processCount.Load()

	jpgPayload2, err := svc.GetImageWithOptions(ctx, itemID, "Primary", 0, jpgOpts)
	if err != nil {
		t.Fatalf("sharded jpeg cache hit failed: %v", err)
	}
	if !bytes.Equal(jpgPayload1.Data, jpgPayload2.Data) || jpgPayload2.ContentType != "image/jpeg" {
		t.Fatalf("sharded jpeg cache hit mismatch")
	}

	pngPayload2, err := svc.GetImageWithOptions(ctx, itemID, "Primary", 0, pngOpts)
	if err != nil {
		t.Fatalf("sharded png cache hit failed: %v", err)
	}
	if !bytes.Equal(pngPayload1.Data, pngPayload2.Data) || pngPayload2.ContentType != "image/png" {
		t.Fatalf("sharded png cache hit mismatch")
	}

	if svc.processCount.Load() != countBefore {
		t.Fatalf("expected zero reprocessing on sharded cache hits, got %d -> %d", countBefore, svc.processCount.Load())
	}
}

func TestDiskCacheLegacyFlatAndOneLevelFallback(t *testing.T) {
	svc, repo, tmp, cacheDir := setupShardingTestService(t)
	ctx := context.Background()

	itemID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:        itemID,
		LibraryID: "lib1",
		Type:      "Movie",
		Name:      "Legacy Cached Movie",
	})
	// Source file intentionally does not exist so any cache miss would error out.
	_ = repo.ReplaceItemImages(ctx, itemID, []sqlite.ImageRecord{{
		ItemID:     itemID,
		ImageType:  "Primary",
		ImageIndex: 0,
		Path:       filepath.Join(tmp, "removed-source.jpg"),
		Tag:        "legacytag",
	}})

	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// 1. Legacy flat JPEG file: <cacheDir>/<key>.jpg
	flatJpgOpts := Options{MaxWidth: 120, Quality: 80}
	flatJpgKey := computeCacheKey(itemID, "Primary", 0, "legacytag", flatJpgOpts.Normalized())
	flatJpgData := []byte("legacy-flat-jpeg-bytes")
	if err := os.WriteFile(filepath.Join(cacheDir, flatJpgKey+".jpg"), flatJpgData, 0o644); err != nil {
		t.Fatal(err)
	}

	gotFlatJpg, err := svc.GetImageWithOptions(ctx, itemID, "Primary", 0, flatJpgOpts)
	if err != nil {
		t.Fatalf("expected legacy flat jpeg cache hit, got err: %v", err)
	}
	if !bytes.Equal(gotFlatJpg.Data, flatJpgData) || gotFlatJpg.ContentType != "image/jpeg" {
		t.Fatalf("unexpected legacy flat jpeg payload: ct=%s data=%q", gotFlatJpg.ContentType, gotFlatJpg.Data)
	}

	// 2. Legacy flat PNG file: <cacheDir>/<key>.png
	flatPngOpts := Options{MaxWidth: 120, Format: "png"}
	flatPngKey := computeCacheKey(itemID, "Primary", 0, "legacytag", flatPngOpts.Normalized())
	flatPngData := []byte("legacy-flat-png-bytes")
	if err := os.WriteFile(filepath.Join(cacheDir, flatPngKey+".png"), flatPngData, 0o644); err != nil {
		t.Fatal(err)
	}

	gotFlatPng, err := svc.GetImageWithOptions(ctx, itemID, "Primary", 0, flatPngOpts)
	if err != nil {
		t.Fatalf("expected legacy flat png cache hit, got err: %v", err)
	}
	if !bytes.Equal(gotFlatPng.Data, flatPngData) || gotFlatPng.ContentType != "image/png" {
		t.Fatalf("unexpected legacy flat png payload: ct=%s data=%q", gotFlatPng.ContentType, gotFlatPng.Data)
	}

	// 3. Legacy 1-level sharded file: <cacheDir>/<key[:2]>/<key>.jpg
	oneLvlOpts := Options{MaxWidth: 240, Quality: 70}
	oneLvlKey := computeCacheKey(itemID, "Primary", 0, "legacytag", oneLvlOpts.Normalized())
	oneLvlPath := filepath.Join(cacheDir, oneLvlKey[:2], oneLvlKey+".jpg")
	if err := os.MkdirAll(filepath.Dir(oneLvlPath), 0o755); err != nil {
		t.Fatal(err)
	}
	oneLvlData := []byte("legacy-one-level-jpeg-bytes")
	if err := os.WriteFile(oneLvlPath, oneLvlData, 0o644); err != nil {
		t.Fatal(err)
	}

	gotOneLvl, err := svc.GetImageWithOptions(ctx, itemID, "Primary", 0, oneLvlOpts)
	if err != nil {
		t.Fatalf("expected legacy 1-level cache hit, got err: %v", err)
	}
	if !bytes.Equal(gotOneLvl.Data, oneLvlData) || gotOneLvl.ContentType != "image/jpeg" {
		t.Fatalf("unexpected legacy 1-level payload: ct=%s data=%q", gotOneLvl.ContentType, gotOneLvl.Data)
	}

	if svc.processCount.Load() != 0 {
		t.Fatalf("expected zero image processing for legacy cache hits, got %d", svc.processCount.Load())
	}
}

func TestDiskCacheShardedTakesPrecedenceOverLegacy(t *testing.T) {
	svc, repo, tmp, cacheDir := setupShardingTestService(t)
	ctx := context.Background()

	itemID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:        itemID,
		LibraryID: "lib1",
		Type:      "Movie",
		Name:      "Precedence Movie",
	})
	_ = repo.ReplaceItemImages(ctx, itemID, []sqlite.ImageRecord{{
		ItemID:     itemID,
		ImageType:  "Primary",
		ImageIndex: 0,
		Path:       filepath.Join(tmp, "missing.jpg"),
		Tag:        "prectag",
	}})

	opts := Options{MaxWidth: 150}
	key := computeCacheKey(itemID, "Primary", 0, "prectag", opts.Normalized())

	// Populate both legacy flat and new 2-level sharded files with distinct contents.
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, key+".jpg"), []byte("old-flat-data"), 0o644); err != nil {
		t.Fatal(err)
	}
	shardedPath := filepath.Join(cacheDir, key[:2], key[2:4], key+".jpg")
	if err := os.MkdirAll(filepath.Dir(shardedPath), 0o755); err != nil {
		t.Fatal(err)
	}
	shardedData := []byte("new-2level-sharded-data")
	if err := os.WriteFile(shardedPath, shardedData, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := svc.GetImageWithOptions(ctx, itemID, "Primary", 0, opts)
	if err != nil {
		t.Fatalf("GetImageWithOptions failed: %v", err)
	}
	if !bytes.Equal(got.Data, shardedData) {
		t.Fatalf("expected 2-level sharded file to take precedence over legacy flat, got %q", got.Data)
	}
}

func TestCachePathShortKeyFallback(t *testing.T) {
	svc := New(nil).WithCacheDir("/tmp/ambra-test-cache")
	if got := svc.cachePath("abc", "jpg"); got != filepath.Join("/tmp/ambra-test-cache", "abc.jpg") {
		t.Fatalf("unexpected short cachePath: %s", got)
	}
	if got := svc.cachePath("abcd1234", "jpg"); got != filepath.Join("/tmp/ambra-test-cache", "ab", "cd", "abcd1234.jpg") {
		t.Fatalf("unexpected 2-level cachePath: %s", got)
	}
	legacyShort := svc.legacyCachePaths("a", "jpg")
	if len(legacyShort) != 1 || legacyShort[0] != filepath.Join("/tmp/ambra-test-cache", "a.jpg") {
		t.Fatalf("unexpected short legacyCachePaths: %v", legacyShort)
	}
}
