package streaming

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/core/imaging"
	"github.com/ambra-media/ambra/internal/core/playback"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

// AuthFuncs decouples route registration from the concrete middleware package.
type AuthFuncs struct {
	RequireAuth   func(http.Handler) http.Handler
	RequireAdmin  func(http.Handler) http.Handler
	UserIDFromCtx func(context.Context) string
}

type Handler struct {
	repo    *sqlite.LibraryRepo
	engine  *playback.Engine
	imaging *imaging.Service
	auth    AuthFuncs
}

func New(repo *sqlite.LibraryRepo, engine *playback.Engine, img *imaging.Service) *Handler {
	return &Handler{
		repo:    repo,
		engine:  engine,
		imaging: img,
	}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth AuthFuncs) {
	h.auth = auth
	wrapAuth := auth.RequireAuth
	if wrapAuth == nil {
		wrapAuth = passthrough
	}

	// PlaybackInfo
	mux.Handle("GET /Items/{Id}/PlaybackInfo", wrapAuth(http.HandlerFunc(h.handlePlaybackInfo)))
	mux.Handle("POST /Items/{Id}/PlaybackInfo", wrapAuth(http.HandlerFunc(h.handlePlaybackInfo)))

	// Video Direct Stream / Original (Go 1.22+ GET pattern automatically matches GET and HEAD)
	mux.Handle("GET /Videos/{Id}/{File}", wrapAuth(http.HandlerFunc(h.handleVideoStream)))

	// Subtitles (handles /Videos/{Id}/{MediaSourceId}/Subtitles/{Index}/Stream.{Format})
	mux.Handle("GET /Videos/{Id}/{MediaSourceId}/Subtitles/{Index}/{StreamFile}", wrapAuth(http.HandlerFunc(h.handleSubtitle)))

	// Images (publicly cacheable by Emby clients with tag, no mandatory auth required)
	mux.HandleFunc("GET /Items/{Id}/Images/{Type}", h.handleImage)
	mux.HandleFunc("HEAD /Items/{Id}/Images/{Type}", h.handleImage)
	mux.HandleFunc("GET /Items/{Id}/Images/{Type}/{Index}", h.handleImage)
	mux.HandleFunc("HEAD /Items/{Id}/Images/{Type}/{Index}", h.handleImage)

	// Download
	mux.Handle("GET /Items/{Id}/Download", wrapAuth(http.HandlerFunc(h.handleDownload)))
}

func (h *Handler) handlePlaybackInfo(w http.ResponseWriter, r *http.Request) {
	itemID := r.PathValue("Id")
	q := httpx.ParseQuery(r)

	var req playback.PlaybackInfoRequest
	if r.Body != nil && r.Method == http.MethodPost {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	if req.UserID == "" {
		req.UserID = q.Get("UserId")
		if req.UserID == "" && h.auth.UserIDFromCtx != nil {
			req.UserID = h.auth.UserIDFromCtx(r.Context())
		}
	}
	if req.MediaSourceID == "" {
		req.MediaSourceID = q.Get("MediaSourceId")
	}
	if req.MaxStreamingBitrate == 0 {
		req.MaxStreamingBitrate = q.Int("MaxStreamingBitrate", 0)
	}
	if req.StartTimeTicks == 0 {
		req.StartTimeTicks = q.Int64("StartTimeTicks", 0)
	}
	if req.AudioStreamIndex == nil && q.Has("AudioStreamIndex") {
		v := q.Int("AudioStreamIndex", 0)
		req.AudioStreamIndex = &v
	}
	if req.SubtitleStreamIndex == nil && q.Has("SubtitleStreamIndex") {
		v := q.Int("SubtitleStreamIndex", 0)
		req.SubtitleStreamIndex = &v
	}

	resp, _, err := h.engine.Decide(r.Context(), itemID, req)
	if errors.Is(err, sqlite.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "item not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

func (h *Handler) handleVideoStream(w http.ResponseWriter, r *http.Request) {
	fileSeg := strings.ToLower(r.PathValue("File"))
	if fileSeg != "stream" && !strings.HasPrefix(fileSeg, "stream.") && !strings.HasPrefix(fileSeg, "original.") {
		httpx.Error(w, http.StatusNotFound, "unsupported stream endpoint")
		return
	}
	h.serveMediaFile(w, r, false)
}

func (h *Handler) handleDownload(w http.ResponseWriter, r *http.Request) {
	h.serveMediaFile(w, r, true)
}

func (h *Handler) serveMediaFile(w http.ResponseWriter, r *http.Request, asAttachment bool) {
	itemID := guid.Normalize(r.PathValue("Id"))
	q := httpx.ParseQuery(r)
	mediaSourceID := guid.Normalize(q.Get("MediaSourceId"))

	mediaPath, err := h.resolveMediaPath(r.Context(), itemID, mediaSourceID)
	if errors.Is(err, sqlite.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "media source not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Check STRM 302 resolution first
	if strings.EqualFold(filepath.Ext(mediaPath), ".strm") {
		resolved, err := h.engine.ResolveStream(r.Context(), mediaPath)
		if err != nil {
			httpx.Error(w, http.StatusBadGateway, "failed to resolve strm: "+err.Error())
			return
		}
		if resolved != nil && resolved.Is302 && resolved.URL != "" {
			w.Header().Set("Cache-Control", "no-store")
			http.Redirect(w, r, resolved.URL, http.StatusFound)
			return
		}
		if resolved != nil && resolved.URL != "" {
			mediaPath = resolved.URL
		}
	}

	f, err := os.Open(mediaPath)
	if err != nil {
		if os.IsNotExist(err) {
			httpx.Error(w, http.StatusNotFound, "media file missing on disk")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	if ct := videoContentType(mediaPath); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	if asAttachment {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filepath.Base(mediaPath)))
	}
	// http.ServeContent handles HTTP Range, If-Range, Accept-Ranges, HEAD, and 206 Partial Content natively.
	http.ServeContent(w, r, stat.Name(), stat.ModTime(), f)
}

func (h *Handler) resolveMediaPath(ctx context.Context, itemID, mediaSourceID string) (string, error) {
	sources, err := h.repo.GetMediaSourcesByItemID(ctx, itemID)
	if err == nil && len(sources) > 0 {
		if mediaSourceID != "" {
			for _, s := range sources {
				if guid.Normalize(s.ID) == mediaSourceID {
					return s.Path, nil
				}
			}
		}
		return sources[0].Path, nil
	}
	item, err := h.repo.GetItemRecordByID(ctx, itemID)
	if err != nil {
		return "", err
	}
	if item.Path == "" {
		return "", sqlite.ErrNotFound
	}
	return item.Path, nil
}

func (h *Handler) handleSubtitle(w http.ResponseWriter, r *http.Request) {
	itemID := guid.Normalize(r.PathValue("Id"))
	msID := guid.Normalize(r.PathValue("MediaSourceId"))
	streamIdx, err := strconv.Atoi(r.PathValue("Index"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid subtitle index")
		return
	}

	streamFile := r.PathValue("StreamFile")
	targetFormat := ""
	if dot := strings.LastIndexByte(streamFile, '.'); dot != -1 {
		targetFormat = strings.ToLower(streamFile[dot+1:])
	}

	sources, err := h.repo.GetMediaSourcesByItemID(r.Context(), itemID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	var subPath string
	for _, s := range sources {
		if msID != "" && guid.Normalize(s.ID) != msID {
			continue
		}
		for _, st := range s.Streams {
			if st.StreamType == "Subtitle" && st.StreamIndex == streamIdx {
				subPath = st.Path
				break
			}
		}
	}
	if subPath == "" {
		httpx.Error(w, http.StatusNotFound, "subtitle stream not found")
		return
	}

	q := httpx.ParseQuery(r)
	startTicks := q.Int64("StartPositionTicks", 0)
	ct, data, err := playback.ServeSubtitle(subPath, targetFormat, startTicks)
	if err != nil {
		if os.IsNotExist(err) {
			httpx.Error(w, http.StatusNotFound, "subtitle file missing")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (h *Handler) handleImage(w http.ResponseWriter, r *http.Request) {
	itemID := r.PathValue("Id")
	imgType := r.PathValue("Type")
	idx := 0
	if idxStr := r.PathValue("Index"); idxStr != "" {
		if n, err := strconv.Atoi(idxStr); err == nil {
			idx = n
		}
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
	err := h.imaging.ServeHTTPWithOptions(w, r, itemID, imgType, idx, opts)
	if errors.Is(err, sqlite.ErrNotFound) || os.IsNotExist(err) {
		httpx.Error(w, http.StatusNotFound, "image not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
}

func videoContentType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp4", ".m4v":
		return "video/mp4"
	case ".mkv":
		return "video/x-matroska"
	case ".webm":
		return "video/webm"
	case ".mov":
		return "video/quicktime"
	case ".avi":
		return "video/x-msvideo"
	case ".ts", ".m2ts":
		return "video/mp2t"
	default:
		return "application/octet-stream"
	}
}

func passthrough(next http.Handler) http.Handler { return next }
