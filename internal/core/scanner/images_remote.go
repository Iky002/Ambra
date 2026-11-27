package scanner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ambra-media/ambra/internal/pkg/netutil"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

const (
	maxRemoteImageBytes = 15 * 1024 * 1024
	remoteImageUA       = "Ambra-Media-Server/1.0"
)

// SetHTTPClient sets the HTTP client used to download remote artwork (useful for testing).
func (s *Scanner) SetHTTPClient(c *http.Client) {
	s.mu.Lock()
	s.httpClient = c
	s.mu.Unlock()
}

func (s *Scanner) getHTTPClient() *http.Client {
	s.mu.Lock()
	c := s.httpClient
	if c == nil {
		c = netutil.NewSafeHTTPClient(15*time.Second, false)
		s.httpClient = c
	}
	s.mu.Unlock()
	return c
}

func (s *Scanner) imageStorageDir() string {
	if snap := s.cfg.Snapshot(); strings.TrimSpace(snap.DataDir) != "" {
		return filepath.Join(snap.DataDir, "metadata", "images")
	}
	return filepath.Join(os.TempDir(), "ambra-metadata-images")
}

func (s *Scanner) isMissingPrimaryImage(ctx context.Context, itemID string) bool {
	existingImg, err := s.repo.GetImageByItemAndType(ctx, itemID, "Primary", 0)
	if err != nil || existingImg == nil || existingImg.Path == "" {
		return true
	}
	info, statErr := os.Stat(existingImg.Path)
	return statErr != nil || info.Size() == 0
}

func (s *Scanner) downloadAndSaveRemoteImages(ctx context.Context, rec *sqlite.ItemRecord, meta *plugins.MetadataResult) bool {
	return s.downloadAndSaveRemoteImagesForce(ctx, rec, meta, false)
}

func (s *Scanner) downloadAndSaveRemoteImagesForce(ctx context.Context, rec *sqlite.ItemRecord, meta *plugins.MetadataResult, forceReplace bool) bool {
	if rec == nil || meta == nil {
		return false
	}

	lib, saveLocal := s.shouldSaveLocalMetadata(ctx, rec.LibraryID)

	artworks := []struct {
		imgType string
		rawURL  string
	}{
		{imgType: "Primary", rawURL: meta.PosterURL},
		{imgType: "Backdrop", rawURL: meta.BackdropURL},
		{imgType: "Logo", rawURL: meta.LogoURL},
	}

	var savedAny bool
	for _, art := range artworks {
		if s.downloadAndSaveItemRemoteImage(ctx, lib, rec, saveLocal, art.imgType, art.rawURL, forceReplace) {
			savedAny = true
		}
	}

	return savedAny
}

func (s *Scanner) downloadAndSaveItemRemoteImage(
	ctx context.Context,
	lib *sqlite.LibraryRecord,
	rec *sqlite.ItemRecord,
	saveLocal bool,
	imgType, rawURL string,
	forceReplace bool,
) bool {
	rawURL = strings.TrimSpace(rawURL)
	if rec == nil || rec.ID == "" || !isHTTPURL(rawURL) {
		return false
	}

	if !forceReplace {
		if existing, err := s.repo.GetImageByItemAndType(ctx, rec.ID, imgType, 0); err == nil && existing != nil && existing.Path != "" {
			if info, statErr := os.Stat(existing.Path); statErr == nil && info.Size() > 0 {
				return false
			}
		}
	}

	data, contentType, ok := fetchRemoteImage(ctx, s.getHTTPClient(), rawURL)
	if !ok {
		return false
	}

	ext := detectImageExtension(contentType, rawURL)
	destPath := ""
	if saveLocal {
		if localPath, canLocal := s.resolveLocalImagePath(ctx, lib, rec, imgType, ext); canLocal {
			if err := writeFileAtomic(localPath, data); err == nil {
				destPath = localPath
			}
		}
	}

	if destPath == "" {
		destDir := filepath.Join(s.imageStorageDir(), rec.ID)
		if err := os.MkdirAll(destDir, 0o755); err != nil {
			return false
		}
		destPath = filepath.Join(destDir, strings.ToLower(imgType)+ext)
		if err := writeFileAtomic(destPath, data); err != nil {
			return false
		}
	}

	sum := sha256.Sum256(data)
	tag := hex.EncodeToString(sum[:8])
	if err := s.repo.UpsertImage(ctx, &sqlite.ImageRecord{
		ItemID:     rec.ID,
		ImageType:  imgType,
		ImageIndex: 0,
		Path:       destPath,
		Tag:        tag,
	}); err == nil {
		return true
	}
	return false
}

func (s *Scanner) downloadAndSaveRemoteImage(ctx context.Context, targetID, imgType, rawURL string) bool {
	return s.downloadAndSaveRemoteImageForce(ctx, targetID, imgType, rawURL, false)
}

func (s *Scanner) downloadAndSaveRemoteImageForce(ctx context.Context, targetID, imgType, rawURL string, forceReplace bool) bool {
	rawURL = strings.TrimSpace(rawURL)
	if targetID == "" || !isHTTPURL(rawURL) {
		return false
	}

	if !forceReplace {
		if existing, err := s.repo.GetImageByItemAndType(ctx, targetID, imgType, 0); err == nil && existing != nil && existing.Path != "" {
			if info, statErr := os.Stat(existing.Path); statErr == nil && info.Size() > 0 {
				return false
			}
		}
	}

	data, contentType, ok := fetchRemoteImage(ctx, s.getHTTPClient(), rawURL)
	if !ok {
		return false
	}

	ext := detectImageExtension(contentType, rawURL)
	destDir := filepath.Join(s.imageStorageDir(), targetID)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return false
	}

	destPath := filepath.Join(destDir, strings.ToLower(imgType)+ext)
	if err := writeFileAtomic(destPath, data); err != nil {
		return false
	}

	sum := sha256.Sum256(data)
	tag := hex.EncodeToString(sum[:8])
	if err := s.repo.UpsertImage(ctx, &sqlite.ImageRecord{
		ItemID:     targetID,
		ImageType:  imgType,
		ImageIndex: 0,
		Path:       destPath,
		Tag:        tag,
	}); err == nil {
		return true
	}
	return false
}

func isHTTPURL(raw string) bool {
	lower := strings.ToLower(raw)
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

func fetchRemoteImage(ctx context.Context, client *http.Client, rawURL string) ([]byte, string, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", false
	}
	req.Header.Set("User-Agent", remoteImageUA)

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", false
	}

	ct := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	if mt, _, err := mime.ParseMediaType(ct); err == nil {
		ct = mt
	}
	if strings.HasPrefix(ct, "text/") || strings.Contains(ct, "json") || strings.Contains(ct, "xml") {
		return nil, "", false
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxRemoteImageBytes+1))
	if err != nil || len(data) == 0 || len(data) > maxRemoteImageBytes {
		return nil, "", false
	}
	return data, ct, true
}

func detectImageExtension(contentType, rawURL string) string {
	switch strings.ToLower(strings.TrimSpace(contentType)) {
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/jpeg", "image/jpg":
		return ".jpg"
	}

	if u, err := url.Parse(rawURL); err == nil {
		switch strings.ToLower(filepath.Ext(u.Path)) {
		case ".png":
			return ".png"
		case ".webp":
			return ".webp"
		case ".jpg", ".jpeg":
			return ".jpg"
		}
	}
	return ".jpg"
}

func writeFileAtomic(destPath string, data []byte) error {
	if fi, err := os.Lstat(destPath); err == nil {
		if fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to overwrite symlink or directory target: %s", destPath)
		}
	}
	f, err := os.CreateTemp(filepath.Dir(destPath), ".ambra-atomic-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := f.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	_ = os.Chmod(tmpPath, 0o644)

	if fi, err := os.Lstat(destPath); err == nil {
		if fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to overwrite symlink or directory target: %s", destPath)
		}
	}
	if err := os.Rename(tmpPath, destPath); err != nil {
		return err
	}
	cleanup = false
	return nil
}
