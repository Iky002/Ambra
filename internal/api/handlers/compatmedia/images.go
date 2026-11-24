package compatmedia

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/core/imaging"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins/tmdb"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

const (
	maxRemoteImageDownloadBytes = 15 * 1024 * 1024
	remoteImageUserAgent        = "Ambra-Media-Server/1.0"
)

type remoteImageFetcher interface {
	FetchImages(ctx context.Context, itemType, tmdbID string) ([]tmdb.RemoteImageCandidate, error)
}

type imageInfoDTO struct {
	ImageType  string `json:"ImageType"`
	ImageIndex *int   `json:"ImageIndex,omitempty"`
	Path       string `json:"Path,omitempty"`
	Filename   string `json:"Filename,omitempty"`
	Height     int    `json:"Height,omitempty"`
	Width      int    `json:"Width,omitempty"`
	Size       int64  `json:"Size,omitempty"`
	ImageTag   string `json:"ImageTag,omitempty"`
}

func (h *Handler) handleListItemImages(w http.ResponseWriter, r *http.Request) {
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	out := make([]imageInfoDTO, 0)
	if h.store != nil {
		rows, err := h.store.Reader.QueryContext(r.Context(),
			`SELECT image_type, image_index, path, tag, COALESCE(width, 0), COALESCE(height, 0)
			 FROM item_images WHERE item_id = ? ORDER BY image_type ASC, image_index ASC`, itemID)
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer rows.Close()
		for rows.Next() {
			var imgType, imgPath, tag string
			var idx, width, height int
			if err := rows.Scan(&imgType, &idx, &imgPath, &tag, &width, &height); err != nil {
				continue
			}
			var size int64
			if st, err := os.Stat(imgPath); err == nil {
				size = st.Size()
			}
			idxCopy := idx
			out = append(out, imageInfoDTO{
				ImageType:  imgType,
				ImageIndex: &idxCopy,
				Path:       imgPath,
				Filename:   filepath.Base(imgPath),
				Width:      width,
				Height:     height,
				Size:       size,
				ImageTag:   tag,
			})
		}
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) handleServeItemImageFull(w http.ResponseWriter, r *http.Request) {
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	imageType := chi.URLParam(r, "Type")
	idx, _ := strconv.Atoi(chi.URLParam(r, "Index"))
	maxW, _ := strconv.Atoi(chi.URLParam(r, "MaxWidth"))
	maxH, _ := strconv.Atoi(chi.URLParam(r, "MaxHeight"))
	format := chi.URLParam(r, "Format")

	opts := imaging.Options{
		MaxWidth:  maxW,
		MaxHeight: maxH,
		Format:    format,
	}
	if h.imaging == nil {
		httpx.Error(w, http.StatusNotFound, "image service unavailable")
		return
	}
	if err := h.imaging.ServeHTTPWithOptions(w, r, itemID, imageType, idx, opts); err != nil {
		if errors.Is(err, sqlite.ErrNotFound) || os.IsNotExist(err) {
			if strings.EqualFold(imageType, "Chapter") && h.tryExtractAndServeChapterImage(w, r, itemID, idx, opts) {
				return
			}
			httpx.Error(w, http.StatusNotFound, "image not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, err.Error())
	}
}

func (h *Handler) tryExtractAndServeChapterImage(w http.ResponseWriter, r *http.Request, itemID string, idx int, opts imaging.Options) bool {
	if h.extractor == nil || h.repo == nil || idx < 0 {
		return false
	}
	normID := guid.Normalize(itemID)
	rec, err := h.repo.GetItemRecordByID(r.Context(), normID)
	if err != nil || rec == nil || idx >= len(rec.Extra.Chapters) {
		return false
	}
	mediaPath, err := h.resolveItemMediaPath(r.Context(), normID, rec)
	if err != nil || strings.TrimSpace(mediaPath) == "" {
		return false
	}
	startTicks := rec.Extra.Chapters[idx].StartPositionTicks
	thumbPath, err := h.extractor.ExtractChapterThumbnail(r.Context(), mediaPath, normID, idx, startTicks)
	if err != nil || thumbPath == "" {
		return false
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("chapter:%s:%d:%d", normID, idx, startTicks)))
	tag := hex.EncodeToString(sum[:8])
	_ = h.repo.UpsertImage(r.Context(), &sqlite.ImageRecord{
		ItemID:     normID,
		ImageType:  "Chapter",
		ImageIndex: idx,
		Path:       thumbPath,
		Tag:        tag,
	})
	return h.imaging.ServeHTTPWithOptions(w, r, normID, "Chapter", idx, opts) == nil
}

func (h *Handler) resolveItemMediaPath(ctx context.Context, itemID string, rec *sqlite.ItemRecord) (string, error) {
	if sources, err := h.repo.GetMediaSourcesByItemID(ctx, itemID); err == nil && len(sources) > 0 && strings.TrimSpace(sources[0].Path) != "" {
		return sources[0].Path, nil
	}
	if rec != nil && strings.TrimSpace(rec.Path) != "" {
		return rec.Path, nil
	}
	return "", sqlite.ErrNotFound
}

func (h *Handler) handleUploadItemImage(w http.ResponseWriter, r *http.Request) {
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	imageType, ok := normalizeAllowedImageType(chi.URLParam(r, "Type"))
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "unsupported image type")
		return
	}
	idx := 0
	if rawIdx := chi.URLParam(r, "Index"); rawIdx != "" {
		idx, _ = strconv.Atoi(rawIdx)
	}
	if _, err := h.repo.GetItemRecordByID(r.Context(), itemID); errors.Is(err, sqlite.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "item not found")
		return
	}

	rawBody, err := io.ReadAll(io.LimitReader(r.Body, 20<<20))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "failed to read image body")
		return
	}
	data := decodeImageBody(rawBody, r.Header.Get("Content-Type"))
	if len(data) == 0 {
		httpx.NoContent(w)
		return
	}

	ext := imageExtForData(data, r.Header.Get("Content-Type"))
	var filePath string
	if h.scanner != nil && idx == 0 {
		if localPath, saved, localErr := h.scanner.SaveItemLocalImage(r.Context(), itemID, imageType, ext, data); localErr == nil && saved && localPath != "" {
			filePath = localPath
		}
	}
	if filePath == "" {
		baseDir := os.TempDir()
		if h.cfg != nil && h.cfg.DataDir != "" {
			baseDir = filepath.Join(h.cfg.DataDir, "metadata", "images", itemID)
		} else {
			baseDir = filepath.Join(baseDir, "ambra-images", itemID)
		}
		if err := os.MkdirAll(baseDir, 0o755); err != nil {
			httpx.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		var err error
		filePath, err = safeImageFilePath(baseDir, imageType, idx, ext)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "invalid image path")
			return
		}
		if err := os.WriteFile(filePath, data, 0o644); err != nil {
			httpx.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	width, height := 0, 0
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
		width, height = cfg.Width, cfg.Height
	}
	tag := guid.New()[:16]
	if h.store != nil {
		_, err = h.store.Writer.ExecContext(r.Context(), `
			INSERT INTO item_images (item_id, image_type, image_index, path, tag, width, height)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(item_id, image_type, image_index) DO UPDATE SET
				path = excluded.path, tag = excluded.tag, width = excluded.width, height = excluded.height`,
			itemID, imageType, idx, filePath, tag, width, height,
		)
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	httpx.NoContent(w)
}

func decodeImageBody(raw []byte, contentType string) []byte {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil
	}
	// If it already looks like binary image magic bytes, keep as-is.
	if bytes.HasPrefix(trimmed, []byte{0x89, 'P', 'N', 'G'}) ||
		bytes.HasPrefix(trimmed, []byte{0xFF, 0xD8, 0xFF}) ||
		bytes.HasPrefix(trimmed, []byte("GIF8")) ||
		bytes.HasPrefix(trimmed, []byte("RIFF")) {
		return trimmed
	}
	// Emby clients often POST base64-encoded image bytes with Content-Type: image/* or text/plain.
	if dec, err := base64.StdEncoding.DecodeString(string(trimmed)); err == nil && len(dec) > 0 {
		return dec
	}
	if strings.Contains(strings.ToLower(contentType), "image/") {
		return raw
	}
	return trimmed
}

func imageExtForData(data []byte, contentType string) string {
	ct := strings.ToLower(contentType)
	if len(data) > 0 {
		detected := http.DetectContentType(data)
		if strings.HasPrefix(detected, "image/") {
			ct = detected
		}
	}
	switch {
	case strings.Contains(ct, "png"):
		return ".png"
	case strings.Contains(ct, "gif"):
		return ".gif"
	case strings.Contains(ct, "webp"):
		return ".webp"
	default:
		return ".jpg"
	}
}

func (h *Handler) handleDeleteItemImage(w http.ResponseWriter, r *http.Request) {
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	imageType := chi.URLParam(r, "Type")
	idx := 0
	if rawIdx := chi.URLParam(r, "Index"); rawIdx != "" {
		idx, _ = strconv.Atoi(rawIdx)
	}
	if h.store != nil {
		var imgPath string
		_ = h.store.Reader.QueryRowContext(r.Context(),
			`SELECT path FROM item_images WHERE item_id = ? AND LOWER(image_type) = LOWER(?) AND image_index = ?`,
			itemID, imageType, idx,
		).Scan(&imgPath)
		_, _ = h.store.Writer.ExecContext(r.Context(),
			`DELETE FROM item_images WHERE item_id = ? AND LOWER(image_type) = LOWER(?) AND image_index = ?`,
			itemID, imageType, idx,
		)
		if imgPath != "" && h.cfg != nil && h.cfg.DataDir != "" &&
			strings.HasPrefix(imgPath, filepath.Join(h.cfg.DataDir, "metadata", "images")) {
			_ = os.Remove(imgPath)
		}
	}
	httpx.NoContent(w)
}

func (h *Handler) handleUpdateImageIndex(w http.ResponseWriter, r *http.Request) {
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	imageType := chi.URLParam(r, "Type")
	oldIdx, _ := strconv.Atoi(chi.URLParam(r, "Index"))
	q := httpx.ParseQuery(r)
	newIdx := q.Int("newIndex", oldIdx)

	if h.store != nil && oldIdx != newIdx {
		_, _ = h.store.Writer.ExecContext(r.Context(),
			`UPDATE item_images SET image_index = ? WHERE item_id = ? AND LOWER(image_type) = LOWER(?) AND image_index = ?`,
			newIdx, itemID, imageType, oldIdx,
		)
	}
	httpx.NoContent(w)
}

func (h *Handler) handleSetImageURL(w http.ResponseWriter, r *http.Request) {
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	rawType := strings.TrimSpace(chi.URLParam(r, "Type"))
	if rawType == "" {
		rawType = "Primary"
	}
	imageType, ok := normalizeAllowedImageType(rawType)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "unsupported image type")
		return
	}
	idx := 0
	if rawIdx := chi.URLParam(r, "Index"); rawIdx != "" {
		idx, _ = strconv.Atoi(rawIdx)
	}

	q := httpx.ParseQuery(r)
	rawURL := nonEmpty(q.Get("Url"), q.Get("ImageUrl"))
	if r.Body != nil {
		var body struct {
			Url      string `json:"Url"`
			ImageUrl string `json:"ImageUrl"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
			rawURL = nonEmpty(rawURL, body.Url, body.ImageUrl)
		}
	}
	if err := h.validateRemoteImageURL(rawURL); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := h.repo.GetItemRecordByID(r.Context(), itemID); errors.Is(err, sqlite.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "item not found")
		return
	} else if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	if err := h.downloadAndSaveItemImage(r.Context(), itemID, imageType, idx, rawURL); err != nil {
		httpx.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) handleRemoteImages(w http.ResponseWriter, r *http.Request) {
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	q := httpx.ParseQuery(r)
	filterType := strings.TrimSpace(q.Get("Type"))
	startIndex := q.Int("StartIndex", 0)
	limit := q.Int("Limit", 0)

	providers := h.listImageProviderNames()
	images := make([]tmdb.RemoteImageCandidate, 0)

	rec, err := h.repo.GetItemRecordByID(r.Context(), itemID)
	if errors.Is(err, sqlite.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "item not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	tmdbID := lookupProviderID(rec.Extra.ProviderIDs, "tmdb")
	hasTMDBProvider := false
	if h.registry != nil && tmdbID != "" {
		for _, p := range h.registry.MetadataProviders() {
			fetcher, ok := p.(remoteImageFetcher)
			if !ok {
				continue
			}
			hasTMDBProvider = true
			cands, fErr := fetcher.FetchImages(r.Context(), rec.Type, tmdbID)
			if fErr != nil {
				continue
			}
			for _, c := range cands {
				if filterType != "" && !strings.EqualFold(c.Type, filterType) {
					continue
				}
				images = append(images, c)
			}
		}
	}
	if hasTMDBProvider {
		providers = ensureProviderName(providers, "TheMovieDb")
	}

	total := len(images)
	if startIndex > 0 {
		if startIndex >= len(images) {
			images = []tmdb.RemoteImageCandidate{}
		} else {
			images = images[startIndex:]
		}
	}
	if limit > 0 && len(images) > limit {
		images = images[:limit]
	}

	httpx.JSON(w, http.StatusOK, map[string]any{
		"Images":           images,
		"TotalRecordCount": total,
		"Providers":        providers,
	})
}

func (h *Handler) handleRemoteImageProviders(w http.ResponseWriter, _ *http.Request) {
	names := h.listImageProviderNames()
	out := make([]map[string]any, 0, len(names))
	for _, n := range names {
		out = append(out, map[string]any{
			"Name":            n,
			"SupportedImages": []string{"Primary", "Backdrop", "Logo", "Thumb"},
		})
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) listImageProviderNames() []string {
	names := []string{}
	if h.registry != nil {
		for _, m := range h.registry.List() {
			if !m.Enabled {
				continue
			}
			for _, k := range m.Kinds {
				if k == "image" || k == "metadata" {
					names = append(names, m.Name)
					break
				}
			}
		}
	}
	return names
}

func (h *Handler) handleDownloadRemoteImage(w http.ResponseWriter, r *http.Request) {
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	q := httpx.ParseQuery(r)
	rawType := strings.TrimSpace(q.Get("Type"))
	imageURL := nonEmpty(q.Get("ImageUrl"), q.Get("Url"))

	if r.Body != nil {
		var body struct {
			Type     string `json:"Type"`
			ImageUrl string `json:"ImageUrl"`
			Url      string `json:"Url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
			rawType = nonEmpty(rawType, body.Type)
			imageURL = nonEmpty(imageURL, body.ImageUrl, body.Url)
		}
	}
	if rawType == "" {
		rawType = "Primary"
	}
	imageType, ok := normalizeAllowedImageType(rawType)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "unsupported image type")
		return
	}
	if err := h.validateRemoteImageURL(imageURL); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := h.repo.GetItemRecordByID(r.Context(), itemID); errors.Is(err, sqlite.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "item not found")
		return
	} else if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	if err := h.downloadAndSaveItemImage(r.Context(), itemID, imageType, 0, imageURL); err != nil {
		httpx.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) handleRemoteImageProxy(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	rawURL := strings.TrimSpace(q.Get("imageUrl"))
	if err := h.validateRemoteImageURL(rawURL); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	parsed, _ := url.Parse(rawURL)

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, rawURL, nil)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid imageUrl")
		return
	}
	req.Header.Set("User-Agent", remoteImageUserAgent)

	resp, err := h.getHTTPClient().Do(req)
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, "failed to fetch remote image")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		httpx.Error(w, http.StatusNotFound, "remote image not found")
		return
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxRemoteImageDownloadBytes+1))
	if err != nil || len(data) == 0 || len(data) > maxRemoteImageDownloadBytes {
		httpx.Error(w, http.StatusBadGateway, "invalid remote image payload")
		return
	}

	ct := normalizeContentType(resp.Header.Get("Content-Type"))
	detected := normalizeContentType(http.DetectContentType(data))
	isTMDBHost := parsed != nil && (strings.EqualFold(parsed.Hostname(), "image.tmdb.org") || strings.HasSuffix(strings.ToLower(parsed.Hostname()), ".tmdb.org"))
	if !isTMDBHost && !strings.HasPrefix(ct, "image/") && !strings.HasPrefix(detected, "image/") {
		httpx.Error(w, http.StatusBadRequest, "remote resource is not an image")
		return
	}
	if !strings.HasPrefix(ct, "image/") {
		if strings.HasPrefix(detected, "image/") {
			ct = detected
		} else {
			ct = "image/jpeg"
		}
	}

	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (h *Handler) downloadAndSaveItemImage(ctx context.Context, itemID, imageType string, idx int, rawURL string) error {
	safeType, ok := normalizeAllowedImageType(imageType)
	if !ok {
		return fmt.Errorf("unsupported image type %q", imageType)
	}
	if err := h.validateRemoteImageURL(rawURL); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", remoteImageUserAgent)

	resp, err := h.getHTTPClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("remote image returned status %d", resp.StatusCode)
	}

	ct := normalizeContentType(resp.Header.Get("Content-Type"))
	if strings.HasPrefix(ct, "text/") || strings.Contains(ct, "json") || strings.Contains(ct, "xml") {
		return fmt.Errorf("remote image has non-image content-type %q", ct)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxRemoteImageDownloadBytes+1))
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return errors.New("remote image is empty")
	}
	if len(data) > maxRemoteImageDownloadBytes {
		return errors.New("remote image exceeds 15MB limit")
	}

	ext := imageExtForData(data, ct)
	var filePath string
	if h.scanner != nil && idx == 0 {
		if localPath, saved, localErr := h.scanner.SaveItemLocalImage(ctx, itemID, safeType, ext, data); localErr == nil && saved && localPath != "" {
			filePath = localPath
		}
	}
	if filePath == "" {
		baseDir := filepath.Join(os.TempDir(), "ambra-images", itemID)
		if h.cfg != nil && strings.TrimSpace(h.cfg.DataDir) != "" {
			baseDir = filepath.Join(h.cfg.DataDir, "metadata", "images", itemID)
		}
		if err := os.MkdirAll(baseDir, 0o755); err != nil {
			return err
		}

		filePath, err = safeImageFilePath(baseDir, safeType, idx, ext)
		if err != nil {
			return err
		}
		tmpPath := filePath + ".tmp"
		if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
			return err
		}
		if err := os.Rename(tmpPath, filePath); err != nil {
			_ = os.Remove(tmpPath)
			return err
		}
	}

	width, height := 0, 0
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
		width, height = cfg.Width, cfg.Height
	}
	sum := sha256.Sum256(data)
	tag := hex.EncodeToString(sum[:8])

	return h.repo.UpsertImage(ctx, &sqlite.ImageRecord{
		ItemID:     itemID,
		ImageType:  safeType,
		ImageIndex: idx,
		Path:       filePath,
		Tag:        tag,
		Width:      width,
		Height:     height,
	})
}

func normalizeAllowedImageType(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "primary":
		return "Primary", true
	case "backdrop":
		return "Backdrop", true
	case "logo":
		return "Logo", true
	case "thumb":
		return "Thumb", true
	case "banner":
		return "Banner", true
	case "art":
		return "Art", true
	case "disc":
		return "Disc", true
	default:
		return "", false
	}
}

func safeImageFilePath(baseDir, imageType string, idx int, ext string) (string, error) {
	filePath := filepath.Join(baseDir, strings.ToLower(imageType)+"_"+strconv.Itoa(idx)+ext)
	rel, err := filepath.Rel(baseDir, filePath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", errors.New("image path escapes base directory")
	}
	return filePath, nil
}

func isBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() ||
		ip.IsMulticast()
}

func validatePublicHTTPURL(raw string, allowLocal bool) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return errors.New("valid http/https image url is required")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return errors.New("valid http/https image url is required")
	}
	if allowLocal {
		return nil
	}
	host := strings.TrimSpace(u.Hostname())
	if host == "" {
		return errors.New("valid http/https image url is required")
	}
	lowerHost := strings.ToLower(host)
	if lowerHost == "localhost" || strings.HasSuffix(lowerHost, ".localhost") {
		return errors.New("remote image url targets a blocked local host")
	}
	if ip := net.ParseIP(host); ip != nil && isBlockedIP(ip) {
		return errors.New("remote image url targets a blocked private or local IP")
	}
	return nil
}

func (h *Handler) validateRemoteImageURL(raw string) error {
	return validatePublicHTTPURL(raw, h.httpClient != nil)
}

func newSafeRemoteHTTPClient() *http.Client {
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			for _, ipAddr := range ips {
				if isBlockedIP(ipAddr.IP) {
					return nil, fmt.Errorf("blocked remote IP %s", ipAddr.IP.String())
				}
			}
			if len(ips) == 0 {
				return nil, errors.New("no resolved IP addresses")
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
		},
	}
	return &http.Client{
		Timeout:   15 * time.Second,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			return validatePublicHTTPURL(req.URL.String(), false)
		},
	}
}

func normalizeContentType(ct string) string {
	ct = strings.ToLower(strings.TrimSpace(ct))
	if mt, _, err := mime.ParseMediaType(ct); err == nil {
		return mt
	}
	return ct
}

func lookupProviderID(ids map[string]string, key string) string {
	if len(ids) == 0 {
		return ""
	}
	if v := strings.TrimSpace(ids[key]); v != "" {
		return v
	}
	for k, v := range ids {
		if strings.EqualFold(k, key) {
			if tv := strings.TrimSpace(v); tv != "" {
				return tv
			}
		}
	}
	return ""
}

func ensureProviderName(names []string, target string) []string {
	for _, n := range names {
		if strings.EqualFold(n, target) {
			return names
		}
	}
	return append(names, target)
}

// handleEntityImage serves GET/HEAD /{Artists|Genres|MusicGenres|GameGenres|Persons|Studios}/{Name}/Images/{Type}[/{Index}].
func (h *Handler) handleEntityImage(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "Name")
		imageType := chi.URLParam(r, "Type")
		idx := 0
		if rawIdx := chi.URLParam(r, "Index"); rawIdx != "" {
			idx, _ = strconv.Atoi(rawIdx)
		}

		entityID := h.resolveEntityID(r.Context(), kind, name)
		if entityID == "" || h.imaging == nil {
			httpx.Error(w, http.StatusNotFound, "image not found")
			return
		}
		q := httpx.ParseQuery(r)
		opts := imaging.Options{
			MaxWidth:   q.Int("maxWidth", 0),
			MaxHeight:  q.Int("maxHeight", 0),
			Width:      q.Int("width", 0),
			Height:     q.Int("height", 0),
			FillWidth:  q.Int("fillWidth", 0),
			FillHeight: q.Int("fillHeight", 0),
			Quality:    q.Int("quality", 0),
			Format:     q.Get("format"),
		}
		if err := h.imaging.ServeHTTPWithOptions(w, r, entityID, imageType, idx, opts); err != nil {
			if errors.Is(err, sqlite.ErrNotFound) || os.IsNotExist(err) {
				httpx.Error(w, http.StatusNotFound, "image not found")
				return
			}
			httpx.Error(w, http.StatusInternalServerError, err.Error())
		}
	}
}

func (h *Handler) resolveEntityID(ctx context.Context, kind, name string) string {
	if h.store == nil || name == "" {
		return ""
	}
	var id string
	switch kind {
	case "person":
		if err := h.store.Reader.QueryRowContext(ctx, `SELECT id FROM people WHERE LOWER(name) = LOWER(?) LIMIT 1`, name).Scan(&id); err == nil {
			return id
		}
	case "artist":
		if err := h.store.Reader.QueryRowContext(ctx, `SELECT id FROM items WHERE LOWER(type) IN ('musicartist','artist') AND LOWER(name) = LOWER(?) LIMIT 1`, name).Scan(&id); err == nil {
			return id
		}
	}
	err := h.store.Reader.QueryRowContext(ctx, `SELECT id FROM taxonomies WHERE LOWER(kind) = LOWER(?) AND LOWER(name) = LOWER(?) LIMIT 1`, kind, name).Scan(&id)
	if err == nil {
		return id
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ""
	}
	return ""
}
