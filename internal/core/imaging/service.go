package imaging

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"golang.org/x/sync/singleflight"

	"github.com/ambra-media/ambra/internal/store/sqlite"
)

// ImagePayload represents a loaded image ready for HTTP delivery.
type ImagePayload struct {
	Data        []byte
	ContentType string
	ETag        string
}

// Service serves item artwork with dynamic resizing, disk caching, singleflight deduplication, and strong ETag caching.
type Service struct {
	repo         *sqlite.LibraryRepo
	mu           sync.RWMutex
	cacheDir     string
	sf           singleflight.Group
	processCount atomic.Int64
	tmpSeq       atomic.Uint64
}

func New(repo *sqlite.LibraryRepo) *Service {
	return &Service{
		repo:     repo,
		cacheDir: filepath.Join(os.TempDir(), "ambra-image-cache"),
	}
}

// WithCacheDir configures the disk cache directory and returns the Service for chaining.
func (s *Service) WithCacheDir(dir string) *Service {
	s.SetCacheDir(dir)
	return s
}

// SetCacheDir updates the disk cache directory. An empty path falls back to os.TempDir().
func (s *Service) SetCacheDir(dir string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(dir) == "" {
		s.cacheDir = filepath.Join(os.TempDir(), "ambra-image-cache")
		return
	}
	s.cacheDir = dir
}

// CacheDir returns the active disk cache directory.
func (s *Service) CacheDir() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cacheDir
}

// GetImage loads the original image for an item (delegating to GetImageWithOptions with zero options).
func (s *Service) GetImage(ctx context.Context, itemID, imageType string, index int) (*ImagePayload, error) {
	return s.GetImageWithOptions(ctx, itemID, imageType, index, Options{})
}

// Load is a backwards-compatible alias for GetImage.
func (s *Service) Load(ctx context.Context, itemID, imageType string, index int) (*ImagePayload, error) {
	return s.GetImage(ctx, itemID, imageType, index)
}

// GetImageWithOptions resolves, resizes/caches on disk as needed, and returns the image payload.
func (s *Service) GetImageWithOptions(ctx context.Context, itemID, imageType string, index int, opts Options) (*ImagePayload, error) {
	rec, err := s.repo.GetImageByItemAndType(ctx, itemID, imageType, index)
	if err != nil {
		return nil, err
	}
	return s.getImageFromRecord(rec, opts)
}

func (s *Service) getImageFromRecord(rec *sqlite.ImageRecord, opts Options) (*ImagePayload, error) {
	opts = opts.Normalized()
	if opts.IsZero() {
		key := fmt.Sprintf("orig:%s:%s:%d:%s", rec.ItemID, strings.ToLower(rec.ImageType), rec.ImageIndex, rec.Tag)
		v, err, _ := s.sf.Do(key, func() (any, error) {
			data, err := os.ReadFile(rec.Path)
			if err != nil {
				return nil, err
			}
			ct := contentTypeForImage(rec.Path, data)
			return &ImagePayload{
				Data:        data,
				ContentType: ct,
				ETag:        fmt.Sprintf(`"%s"`, rec.Tag),
			}, nil
		})
		if err != nil {
			return nil, err
		}
		return v.(*ImagePayload), nil
	}

	cacheKey := computeCacheKey(rec.ItemID, rec.ImageType, rec.ImageIndex, rec.Tag, opts)
	etag := buildETag(rec.Tag, cacheKey)

	// Fast path: disk cache hit without acquiring singleflight.
	if payload, ok := s.readDiskCache(cacheKey, opts, etag); ok {
		return payload, nil
	}

	v, err, _ := s.sf.Do(cacheKey, func() (any, error) {
		// Double-check disk cache inside singleflight in case another caller just finished.
		if payload, ok := s.readDiskCache(cacheKey, opts, etag); ok {
			return payload, nil
		}

		rawData, err := os.ReadFile(rec.Path)
		if err != nil {
			return nil, err
		}
		origCT := contentTypeForImage(rec.Path, rawData)

		s.processCount.Add(1)
		outData, outCT, transformed, procErr := ProcessImage(rawData, origCT, opts)
		if procErr != nil {
			// Graceful degradation for corrupted or undecodable images: serve original bytes.
			return &ImagePayload{
				Data:        rawData,
				ContentType: origCT,
				ETag:        etag,
			}, nil
		}

		if transformed {
			_ = s.writeDiskCache(cacheKey, outCT, outData)
		}

		return &ImagePayload{
			Data:        outData,
			ContentType: outCT,
			ETag:        etag,
		}, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*ImagePayload), nil
}

// ServeHTTP writes an original image response with strong ETag and 304 Not Modified support.
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request, itemID, imageType string, index int) error {
	return s.ServeHTTPWithOptions(w, r, itemID, imageType, index, Options{})
}

// ServeHTTPWithOptions writes a resized or original image response with strong ETag and 304 support.
func (s *Service) ServeHTTPWithOptions(w http.ResponseWriter, r *http.Request, itemID, imageType string, index int, opts Options) error {
	rec, err := s.repo.GetImageByItemAndType(r.Context(), itemID, imageType, index)
	if err != nil {
		return err
	}

	opts = opts.Normalized()
	etag := fmt.Sprintf(`"%s"`, rec.Tag)
	rawTag := rec.Tag
	if !opts.IsZero() {
		cacheKey := computeCacheKey(rec.ItemID, rec.ImageType, rec.ImageIndex, rec.Tag, opts)
		etag = buildETag(rec.Tag, cacheKey)
		rawTag = strings.Trim(etag, `"`)
	}

	if inm := strings.TrimSpace(r.Header.Get("If-None-Match")); inm != "" && (inm == etag || inm == rawTag) {
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusNotModified)
		return nil
	}

	payload, err := s.getImageFromRecord(rec, opts)
	if err != nil {
		return err
	}

	w.Header().Set("Content-Type", payload.ContentType)
	w.Header().Set("ETag", payload.ETag)
	if r.URL.Query().Get("tag") != "" {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=86400")
	}
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(payload.Data)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(payload.Data)
	}
	return nil
}

func computeCacheKey(itemID, imageType string, index int, tag string, opts Options) string {
	raw := fmt.Sprintf("%s|%s|%d|%s|%s",
		itemID,
		strings.ToLower(imageType),
		index,
		tag,
		opts.CacheSignature(),
	)
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func buildETag(tag, cacheKey string) string {
	return fmt.Sprintf(`"%s-%s"`, tag, cacheKey[:16])
}

func (s *Service) cachePath(cacheKey, ext string) string {
	dir := s.CacheDir()
	return filepath.Join(dir, cacheKey[:2], cacheKey+"."+ext)
}

func (s *Service) readDiskCache(cacheKey string, opts Options, etag string) (*ImagePayload, bool) {
	candidates := [2]struct {
		ext string
		ct  string
	}{
		{"jpg", "image/jpeg"},
		{"png", "image/png"},
	}
	if opts.Format == "png" {
		candidates[0], candidates[1] = candidates[1], candidates[0]
	}

	for _, c := range candidates {
		data, err := os.ReadFile(s.cachePath(cacheKey, c.ext))
		if err == nil && len(data) > 0 {
			return &ImagePayload{
				Data:        data,
				ContentType: c.ct,
				ETag:        etag,
			}, true
		}
	}
	return nil, false
}

func (s *Service) writeDiskCache(cacheKey, contentType string, data []byte) error {
	ext := "jpg"
	if contentType == "image/png" {
		ext = "png"
	}
	targetPath := s.cachePath(cacheKey, ext)
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	seq := s.tmpSeq.Add(1)
	tmpPath := fmt.Sprintf("%s.%d.%d.tmp", targetPath, os.Getpid(), seq)
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func contentTypeForImage(path string, data []byte) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	default:
		return http.DetectContentType(data)
	}
}
