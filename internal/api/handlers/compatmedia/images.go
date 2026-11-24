package compatmedia

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/core/imaging"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

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
			httpx.Error(w, http.StatusNotFound, "image not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, err.Error())
	}
}

func (h *Handler) handleUploadItemImage(w http.ResponseWriter, r *http.Request) {
	itemID := guid.Normalize(chi.URLParam(r, "Id"))
	imageType := chi.URLParam(r, "Type")
	if imageType == "" {
		httpx.Error(w, http.StatusBadRequest, "image type is required")
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
	filePath := filepath.Join(baseDir, strings.ToLower(imageType)+"_"+strconv.Itoa(idx)+ext)
	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
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
	imageType := chi.URLParam(r, "Type")
	idx, _ := strconv.Atoi(chi.URLParam(r, "Index"))
	var body map[string]any
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	_ = h.setConfigJSON(r.Context(), "image_url_"+itemID+"_"+strings.ToLower(imageType)+"_"+strconv.Itoa(idx), body)
	httpx.NoContent(w)
}

func (h *Handler) handleRemoteImages(w http.ResponseWriter, _ *http.Request) {
	providers := h.listImageProviderNames()
	httpx.JSON(w, http.StatusOK, map[string]any{
		"Images":           []any{},
		"TotalRecordCount": 0,
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

func (h *Handler) handleDownloadRemoteImage(w http.ResponseWriter, _ *http.Request) {
	httpx.NoContent(w)
}

func (h *Handler) handleRemoteImageProxy(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	if q.Get("imageUrl") == "" {
		httpx.Error(w, http.StatusBadRequest, "imageUrl is required")
		return
	}
	httpx.Error(w, http.StatusNotFound, "remote image not cached")
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
