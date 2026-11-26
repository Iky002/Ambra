package imaging

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/ambra-media/ambra/internal/store/sqlite"
)

const (
	maxImageFileSize     = 50 << 20  // 50 MiB
	defaultMaxCacheBytes = 512 << 20 // 512 MiB
)

func readLimitedImageFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if fi, err := f.Stat(); err == nil && fi.Size() > maxImageFileSize {
		return nil, fmt.Errorf("image file %q exceeds max size of %d bytes", filepath.Base(path), maxImageFileSize)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxImageFileSize+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxImageFileSize {
		return nil, fmt.Errorf("image file %q exceeds max size of %d bytes", filepath.Base(path), maxImageFileSize)
	}
	return data, nil
}

// ImagePayload represents a loaded image ready for HTTP delivery.
type ImagePayload struct {
	Data        []byte
	ContentType string
	ETag        string
}

// Service serves item artwork with dynamic resizing, disk caching, singleflight deduplication, and strong ETag caching.
type Service struct {
	repo             *sqlite.LibraryRepo
	mu               sync.RWMutex
	cacheDir         string
	maxCacheBytes    int64
	sem              chan struct{}
	pruneMu          sync.Mutex
	approxCacheBytes atomic.Int64
	sf               singleflight.Group
	processCount     atomic.Int64
	tmpSeq           atomic.Uint64
}

func New(repo *sqlite.LibraryRepo) *Service {
	workers := runtime.NumCPU()
	if workers < 4 {
		workers = 4
	}
	return &Service{
		repo:          repo,
		cacheDir:      filepath.Join(os.TempDir(), "ambra-image-cache"),
		maxCacheBytes: defaultMaxCacheBytes,
		sem:           make(chan struct{}, workers),
	}
}

// WithMaxCacheBytes sets the maximum disk cache size in bytes before LRU/oldest-file pruning triggers.
func (s *Service) WithMaxCacheBytes(maxBytes int64) *Service {
	s.mu.Lock()
	if maxBytes > 0 {
		s.maxCacheBytes = maxBytes
	}
	s.mu.Unlock()
	return s
}

// WithMaxConcurrency overrides the maximum concurrent decode/resize worker semaphore size.
func (s *Service) WithMaxConcurrency(n int) *Service {
	if n > 0 {
		s.mu.Lock()
		s.sem = make(chan struct{}, n)
		s.mu.Unlock()
	}
	return s
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
	return s.getImageFromRecord(ctx, rec, opts)
}

func (s *Service) acquireDecodeSlot(ctx context.Context) (func(), error) {
	s.mu.RLock()
	sem := s.sem
	s.mu.RUnlock()
	if sem == nil {
		return func() {}, nil
	}
	if ctx == nil {
		sem <- struct{}{}
		return func() { <-sem }, nil
	}
	select {
	case sem <- struct{}{}:
		return func() { <-sem }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *Service) getImageFromRecord(ctx context.Context, rec *sqlite.ImageRecord, opts Options) (*ImagePayload, error) {
	opts = opts.Normalized()
	if opts.IsZero() {
		key := fmt.Sprintf("orig:%s:%s:%d:%s", rec.ItemID, strings.ToLower(rec.ImageType), rec.ImageIndex, rec.Tag)
		v, err, _ := s.sf.Do(key, func() (any, error) {
			data, err := readLimitedImageFile(rec.Path)
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

		release, err := s.acquireDecodeSlot(context.WithoutCancel(ctx))
		if err != nil {
			return nil, err
		}
		defer release()

		rawData, err := readLimitedImageFile(rec.Path)
		if err != nil {
			return nil, err
		}
		origCT := contentTypeForImage(rec.Path, rawData)

		// Pre-check image dimensions before full decode to guard against pixel bombs.
		if cfg, _, cfgErr := image.DecodeConfig(bytes.NewReader(rawData)); cfgErr == nil {
			if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > maxSourcePixels {
				return nil, fmt.Errorf("source image dimensions %dx%d exceed safety limit", cfg.Width, cfg.Height)
			}
		}

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
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusNotModified)
		return nil
	}

	payload, err := s.getImageFromRecord(r.Context(), rec, opts)
	if err != nil {
		return err
	}

	w.Header().Set("X-Content-Type-Options", "nosniff")
	if !isSafeRasterContentType(payload.ContentType) {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	}
	w.Header().Set("Content-Type", payload.ContentType)
	w.Header().Set("ETag", payload.ETag)
	if tag := r.URL.Query().Get("tag"); tag != "" && tag == rec.Tag {
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
	filename := cacheKey + "." + ext
	if len(cacheKey) < 4 {
		return filepath.Join(dir, filename)
	}
	return filepath.Join(dir, cacheKey[:2], cacheKey[2:4], filename)
}

func (s *Service) legacyCachePaths(cacheKey, ext string) []string {
	dir := s.CacheDir()
	filename := cacheKey + "." + ext
	if len(cacheKey) < 2 {
		return []string{filepath.Join(dir, filename)}
	}
	return []string{
		filepath.Join(dir, cacheKey[:2], filename), // legacy 1-level sharded path
		filepath.Join(dir, filename),               // legacy flat path
	}
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

	// Check the 2-level sharded cache path first.
	for _, c := range candidates {
		if data, err := readLimitedImageFile(s.cachePath(cacheKey, c.ext)); err == nil && len(data) > 0 {
			return &ImagePayload{
				Data:        data,
				ContentType: c.ct,
				ETag:        etag,
			}, true
		}
	}

	// Seamless backward compatibility: fall back to legacy 1-level sharded and flat paths.
	for _, c := range candidates {
		for _, legacyPath := range s.legacyCachePaths(cacheKey, c.ext) {
			if data, err := readLimitedImageFile(legacyPath); err == nil && len(data) > 0 {
				return &ImagePayload{
					Data:        data,
					ContentType: c.ct,
					ETag:        etag,
				}, true
			}
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
	if err := os.MkdirAll(s.CacheDir(), 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
		return err
	}
	seq := s.tmpSeq.Add(1)
	tmpPath := fmt.Sprintf("%s.%d.%d.tmp", targetPath, os.Getpid(), seq)
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	s.mu.RLock()
	limit := s.maxCacheBytes
	s.mu.RUnlock()
	if limit > 0 && s.approxCacheBytes.Add(int64(len(data))) > limit {
		s.PruneCache()
	}
	return nil
}

// PruneCache enforces maxCacheBytes by removing the oldest cached image files until usage is within limit.
func (s *Service) PruneCache() {
	s.mu.RLock()
	dir := s.cacheDir
	limit := s.maxCacheBytes
	s.mu.RUnlock()
	if dir == "" || limit <= 0 {
		return
	}

	s.pruneMu.Lock()
	defer s.pruneMu.Unlock()

	type cacheFile struct {
		path    string
		size    int64
		modTime time.Time
	}
	var files []cacheFile
	var total int64
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.HasSuffix(d.Name(), ".tmp") {
			return nil
		}
		if info, statErr := d.Info(); statErr == nil {
			sz := info.Size()
			total += sz
			files = append(files, cacheFile{
				path:    p,
				size:    sz,
				modTime: info.ModTime(),
			})
		}
		return nil
	})
	if total <= limit {
		s.approxCacheBytes.Store(total)
		return
	}

	sort.Slice(files, func(i, j int) bool {
		if !files[i].modTime.Equal(files[j].modTime) {
			return files[i].modTime.Before(files[j].modTime)
		}
		return files[i].path < files[j].path
	})

	target := limit * 80 / 100
	for _, f := range files {
		if total <= target {
			break
		}
		if err := os.Remove(f.path); err == nil {
			total -= f.size
		}
	}
	if total < 0 {
		total = 0
	}
	s.approxCacheBytes.Store(total)
}

func isSafeRasterContentType(ct string) bool {
	base := strings.ToLower(strings.TrimSpace(strings.SplitN(ct, ";", 2)[0]))
	switch base {
	case "image/jpeg", "image/png", "image/webp", "image/gif", "image/bmp", "image/avif":
		return true
	default:
		return false
	}
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
	case ".bmp":
		return "image/bmp"
	case ".avif":
		return "image/avif"
	default:
		detected := http.DetectContentType(data)
		if isSafeRasterContentType(detected) {
			return detected
		}
		return "application/octet-stream"
	}
}
