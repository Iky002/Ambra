package imaging

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sync/singleflight"

	"github.com/ambra-media/ambra/internal/store/sqlite"
)

// ImagePayload represents a loaded image ready for HTTP delivery.
type ImagePayload struct {
	Data        []byte
	ContentType string
	ETag        string
}

// Service serves item artwork with singleflight deduplication and strong ETag caching.
type Service struct {
	repo *sqlite.LibraryRepo
	sf   singleflight.Group
}

func New(repo *sqlite.LibraryRepo) *Service {
	return &Service{repo: repo}
}

// Load resolves and reads the image bytes for an item, coalescing concurrent reads via singleflight.
func (s *Service) Load(ctx context.Context, itemID, imageType string, index int) (*ImagePayload, error) {
	rec, err := s.repo.GetImageByItemAndType(ctx, itemID, imageType, index)
	if err != nil {
		return nil, err
	}

	key := fmt.Sprintf("%s:%s:%d:%s", rec.ItemID, strings.ToLower(rec.ImageType), rec.ImageIndex, rec.Tag)
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

// ServeHTTP writes an image response with strong ETag and 304 Not Modified support.
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request, itemID, imageType string, index int) error {
	rec, err := s.repo.GetImageByItemAndType(r.Context(), itemID, imageType, index)
	if err != nil {
		return err
	}
	etag := fmt.Sprintf(`"%s"`, rec.Tag)
	if inm := strings.TrimSpace(r.Header.Get("If-None-Match")); inm != "" && (inm == etag || inm == rec.Tag) {
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusNotModified)
		return nil
	}

	payload, err := s.Load(r.Context(), itemID, imageType, index)
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
