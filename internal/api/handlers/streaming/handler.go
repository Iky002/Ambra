package streaming

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/core/imaging"
	"github.com/ambra-media/ambra/internal/core/playback"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/pkg/netutil"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

const (
	defaultBrowserUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	maxImageDim      = 4096
)

func clampImageDim(v int) int {
	if v < 0 {
		return 0
	}
	if v > maxImageDim {
		return maxImageDim
	}
	return v
}

// AuthFuncs decouples route registration from the concrete middleware package.
type AuthFuncs struct {
	RequireAuth   func(http.Handler) http.Handler
	RequireAdmin  func(http.Handler) http.Handler
	UserIDFromCtx func(context.Context) string
}

type Handler struct {
	repo       *sqlite.LibraryRepo
	userRepo   *sqlite.UserRepo
	engine     *playback.Engine
	imaging    *imaging.Service
	extractor  *transcode.Extractor
	httpClient *http.Client
	auth       AuthFuncs
}

func New(repo *sqlite.LibraryRepo, engine *playback.Engine, img *imaging.Service) *Handler {
	return &Handler{
		repo:       repo,
		engine:     engine,
		imaging:    img,
		httpClient: netutil.NewSafeHTTPClient(0, true),
	}
}

// SetProxyClient overrides the HTTP client used for remote STRM reverse-proxying (primarily for testing).
func (h *Handler) SetProxyClient(client *http.Client) {
	if client != nil {
		h.httpClient = client
	}
}

// SetExtractor configures the embedded subtitle extractor used when a requested subtitle stream is not external.
func (h *Handler) SetExtractor(ext *transcode.Extractor) {
	h.extractor = ext
}

// SetUserRepo attaches a UserRepo for per-user library access control on playback/streaming/download/subtitles.
func (h *Handler) SetUserRepo(userRepo *sqlite.UserRepo) {
	h.userRepo = userRepo
}

// CanAccessLibrary returns true if userID is permitted to access libraryID.
func (h *Handler) CanAccessLibrary(ctx context.Context, userID, libraryID string) bool {
	if h.userRepo == nil || libraryID == "" {
		return true
	}
	if strings.TrimSpace(userID) == "" {
		return false
	}
	u, err := h.userRepo.GetUserByID(ctx, guid.Normalize(userID))
	if err != nil || u == nil {
		return false
	}
	if u.IsAdmin || u.Policy.IsAdministrator || u.Policy.EnableAllFolders {
		return true
	}
	normLib := guid.Normalize(libraryID)
	for _, fid := range u.Policy.EnabledFolders {
		if guid.Normalize(fid) == normLib {
			return true
		}
	}
	return false
}

func (h *Handler) callerIdentity(r *http.Request) (userID string, isAdmin bool) {
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && (ac.User != nil || ac.IsAPIKey) {
		return guid.Normalize(ac.UserID()), ac.IsAdmin()
	}
	if h.auth.UserIDFromCtx != nil {
		uid := guid.Normalize(h.auth.UserIDFromCtx(r.Context()))
		if uid != "" {
			if h.userRepo != nil {
				if u, err := h.userRepo.GetUserByID(r.Context(), uid); err == nil && u != nil {
					return uid, u.IsAdmin || u.Policy.IsAdministrator
				}
			}
			return uid, false
		}
	}
	return "", false
}

func (h *Handler) isAuthenticated(r *http.Request) bool {
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && (ac.User != nil || ac.IsAPIKey) {
		return true
	}
	if h.auth.UserIDFromCtx != nil && strings.TrimSpace(h.auth.UserIDFromCtx(r.Context())) != "" {
		return true
	}
	return false
}

func (h *Handler) canAccessUserData(r *http.Request, targetUserID string) bool {
	normTarget := guid.Normalize(targetUserID)
	if normTarget == "" {
		return true
	}
	callerID, isAdmin := h.callerIdentity(r)
	if isAdmin || callerID == "" {
		return true
	}
	return callerID == normTarget
}

func (h *Handler) canDownloadContent(r *http.Request) bool {
	if h.userRepo == nil {
		return true
	}
	callerID, isAdmin := h.callerIdentity(r)
	if isAdmin {
		return true
	}
	if callerID == "" {
		return false
	}
	u, err := h.userRepo.GetUserByID(r.Context(), callerID)
	if err != nil || u == nil {
		return false
	}
	if u.IsAdmin || u.Policy.IsAdministrator {
		return true
	}
	return u.Policy.EnableContentDownloading
}

func (h *Handler) canAccessItem(r *http.Request, itemID string) bool {
	if h.userRepo == nil || h.repo == nil {
		return true
	}
	callerID, isAdmin := h.callerIdentity(r)
	if isAdmin {
		return true
	}
	if callerID == "" {
		return false
	}
	rec, err := h.repo.GetItemRecordByID(r.Context(), itemID)
	if errors.Is(err, sqlite.ErrNotFound) {
		return true
	}
	if err != nil || rec == nil {
		return false
	}
	if rec.LibraryID != "" && !h.CanAccessLibrary(r.Context(), callerID, rec.LibraryID) {
		return false
	}
	u, err := h.userRepo.GetUserByID(r.Context(), callerID)
	if err != nil || u == nil {
		return false
	}
	if u.IsAdmin || u.Policy.IsAdministrator {
		return true
	}
	blocked, err := h.repo.IsItemBlockedByParentalPolicy(r.Context(), rec, u.Policy.MaxParentalRating, u.Policy.BlockedTags)
	if err != nil {
		return false
	}
	return !blocked
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
	if !h.canAccessItem(r, itemID) {
		httpx.Error(w, http.StatusForbidden, "access denied")
		return
	}
	q := httpx.ParseQuery(r)

	var req playback.PlaybackInfoRequest
	if r.Body != nil && r.Method == http.MethodPost {
		_ = httpx.DecodeJSONLimit(r, &req, 0)
	}
	if req.UserID == "" {
		req.UserID = q.Get("UserId")
		if req.UserID == "" && h.auth.UserIDFromCtx != nil {
			req.UserID = h.auth.UserIDFromCtx(r.Context())
		}
	}
	if req.UserID != "" && !h.canAccessUserData(r, req.UserID) {
		httpx.Error(w, http.StatusForbidden, "access denied")
		return
	}
	if req.MediaSourceID == "" {
		req.MediaSourceID = q.Get("MediaSourceId")
	}
	if req.MaxStreamingBitrate == 0 {
		req.MaxStreamingBitrate = q.Int("MaxStreamingBitrate", 0)
	}
	if req.MaxWidth == 0 {
		req.MaxWidth = q.Int("MaxWidth", 0)
	}
	if req.MaxHeight == 0 {
		req.MaxHeight = q.Int("MaxHeight", 0)
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
	if req.EnableDirectPlay == nil {
		req.EnableDirectPlay = q.Bool("EnableDirectPlay")
	}
	if req.EnableDirectStream == nil {
		req.EnableDirectStream = q.Bool("EnableDirectStream")
	}
	if req.EnableTranscoding == nil {
		req.EnableTranscoding = q.Bool("EnableTranscoding")
	}

	resp, _, err := h.engine.Decide(r.Context(), itemID, req)
	if errors.Is(err, sqlite.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "item not found")
		return
	}
	if err != nil {
		slog.Error("failed to determine playback info", "item_id", itemID, "error", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to determine playback info")
		return
	}
	if _, isAdmin := h.callerIdentity(r); h.isAuthenticated(r) && !isAdmin {
		dto.SanitizePlaybackInfo(resp)
	}
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && strings.TrimSpace(ac.Token) != "" {
		appendAPIKeyToPlaybackInfo(resp, ac.Token)
	}
	httpx.JSON(w, http.StatusOK, resp)
}

func appendAPIKeyToPlaybackInfo(resp *dto.PlaybackInfoResponse, token string) {
	if resp == nil || strings.TrimSpace(token) == "" {
		return
	}
	for i := range resp.MediaSources {
		ms := &resp.MediaSources[i]
		ms.DirectStreamUrl = appendAPIKeyQuery(ms.DirectStreamUrl, token)
		ms.TranscodingUrl = appendAPIKeyQuery(ms.TranscodingUrl, token)
		for j := range ms.MediaStreams {
			ms.MediaStreams[j].DeliveryUrl = appendAPIKeyQuery(ms.MediaStreams[j].DeliveryUrl, token)
		}
	}
}

func appendAPIKeyQuery(rawURL, token string) string {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" || !strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "//") {
		return rawURL
	}
	parsed, err := url.Parse(trimmed)
	if err == nil {
		for k := range parsed.Query() {
			if strings.EqualFold(k, "api_key") {
				return trimmed
			}
		}
	}
	sep := "?"
	if strings.Contains(trimmed, "?") {
		sep = "&"
	}
	return trimmed + sep + "api_key=" + url.QueryEscape(token)
}

func (h *Handler) handleVideoStream(w http.ResponseWriter, r *http.Request) {
	fileSeg := strings.ToLower(r.PathValue("File"))
	if fileSeg == "index.bif" {
		h.handleVideoBIF(w, r)
		return
	}
	if fileSeg != "stream" && !strings.HasPrefix(fileSeg, "stream.") && !strings.HasPrefix(fileSeg, "original.") {
		httpx.Error(w, http.StatusNotFound, "unsupported stream endpoint")
		return
	}
	h.serveMediaFile(w, r, false)
}

// handleVideoBIF serves pre-generated or sidecar Roku/Emby BIF preview archives from cache only.
// It intentionally never invokes FFmpeg on demand; BIF files are generated exclusively by the
// scheduled GenerateBifThumbnails task (or placed alongside media as sidecar .bif files).
func (h *Handler) handleVideoBIF(w http.ResponseWriter, r *http.Request) {
	itemID := guid.Normalize(r.PathValue("Id"))
	if !h.canAccessItem(r, itemID) {
		httpx.Error(w, http.StatusForbidden, "access denied")
		return
	}
	if h.extractor == nil {
		httpx.Error(w, http.StatusNotFound, "bif not found")
		return
	}
	q := httpx.ParseQuery(r)
	mediaSourceID := guid.Normalize(q.Get("MediaSourceId"))
	width := transcode.ParseBIFWidthParam(q.Get("Width"))
	mediaPath, _ := h.resolveMediaPath(r.Context(), itemID, mediaSourceID)
	bifPath := h.extractor.FindExistingBIF(mediaPath, itemID, width)
	if bifPath == "" {
		httpx.Error(w, http.StatusNotFound, "bif not found")
		return
	}
	f, err := os.Open(bifPath)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			logAndWritePermissionDenied(w, bifPath, err)
			return
		}
		httpx.Error(w, http.StatusNotFound, "bif not found")
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || stat.IsDir() {
		httpx.Error(w, http.StatusNotFound, "bif not found")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeContent(w, r, "index.bif", stat.ModTime(), f)
}

func logAndWritePermissionDenied(w http.ResponseWriter, path string, err error) {
	slog.Error("permission denied reading media file",
		"path", path,
		"uid", os.Geteuid(),
		"gid", os.Getegid(),
		"error", err,
	)
	httpx.Error(w, http.StatusForbidden, "permission denied reading media file")
}

func (h *Handler) handleDownload(w http.ResponseWriter, r *http.Request) {
	if !h.canDownloadContent(r) {
		httpx.Error(w, http.StatusForbidden, "content downloading is disabled")
		return
	}
	h.serveMediaFile(w, r, true)
}

func (h *Handler) serveMediaFile(w http.ResponseWriter, r *http.Request, asAttachment bool) {
	itemID := guid.Normalize(r.PathValue("Id"))
	if !h.canAccessItem(r, itemID) {
		httpx.Error(w, http.StatusForbidden, "access denied")
		return
	}
	q := httpx.ParseQuery(r)
	mediaSourceID := guid.Normalize(q.Get("MediaSourceId"))

	mediaPath, err := h.resolveMediaPath(r.Context(), itemID, mediaSourceID)
	if errors.Is(err, sqlite.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "media source not found")
		return
	}
	if err != nil {
		slog.Error("failed to resolve media path", "item_id", itemID, "error", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to resolve media path")
		return
	}

	// Check STRM 302 resolution first
	forceProxy := false
	if b := q.Bool("proxy"); b != nil && *b {
		forceProxy = true
	}
	if strings.EqualFold(filepath.Ext(mediaPath), ".strm") {
		if h.engine == nil {
			httpx.Error(w, http.StatusBadGateway, "strm resolver unavailable")
			return
		}
		resolved, err := h.engine.ResolveStream(r.Context(), mediaPath)
		if err != nil {
			slog.Error("failed to resolve strm", "path", mediaPath, "error", err)
			httpx.Error(w, http.StatusBadGateway, "failed to resolve strm stream")
			return
		}
		if resolved == nil || !isHTTPURL(resolved.URL) {
			httpx.Error(w, http.StatusBadGateway, "invalid strm target url")
			return
		}
		targetURL := strings.TrimSpace(resolved.URL)
		if err := netutil.ValidateRemoteURL(targetURL, true); err != nil {
			slog.Error("blocked upstream stream url", "url", targetURL, "error", err)
			httpx.Error(w, http.StatusBadGateway, "blocked upstream stream url")
			return
		}
		if resolved.Is302 && !forceProxy {
			w.Header().Set("Cache-Control", "no-store")
			http.Redirect(w, r, targetURL, http.StatusFound)
			return
		}
		mediaPath = targetURL
	}

	// Reverse-proxy remote HTTP(S) streams (e.g. non-302 STRM or explicit proxy=1/true)
	if isHTTPURL(mediaPath) {
		h.proxyRemoteMedia(w, r, mediaPath, asAttachment)
		return
	}

	f, err := os.Open(mediaPath)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			logAndWritePermissionDenied(w, mediaPath, err)
			return
		}
		if os.IsNotExist(err) {
			httpx.Error(w, http.StatusNotFound, "media file missing on disk")
			return
		}
		slog.Error("failed to open media file", "path", mediaPath, "error", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to open media file")
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		slog.Error("failed to stat media file", "path", mediaPath, "error", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to read media file")
		return
	}
	if stat.IsDir() {
		httpx.Error(w, http.StatusNotFound, "media file missing on disk")
		return
	}

	if ct := videoContentType(mediaPath); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	if asAttachment {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, sanitizeContentDispositionFilename(filepath.Base(mediaPath))))
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
	if !h.canAccessItem(r, itemID) {
		httpx.Error(w, http.StatusForbidden, "access denied")
		return
	}
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
		slog.Error("failed to load media sources", "item_id", itemID, "error", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to load media sources")
		return
	}
	var (
		foundStream sqlite.MediaStreamRecord
		found       bool
		sourceID    string
		sourcePath  string
	)
	for _, s := range sources {
		if msID != "" && guid.Normalize(s.ID) != msID {
			continue
		}
		for _, st := range s.Streams {
			if strings.EqualFold(st.StreamType, "Subtitle") && st.StreamIndex == streamIdx {
				foundStream = st
				found = true
				sourceID = s.ID
				sourcePath = s.Path
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		httpx.Error(w, http.StatusNotFound, "subtitle stream not found")
		return
	}

	subPath := foundStream.Path
	if !foundStream.IsExternal && subPath == "" {
		if h.extractor == nil {
			httpx.Error(w, http.StatusNotFound, "subtitle stream not found")
			return
		}
		mediaPath := sourcePath
		if mediaPath == "" {
			mediaPath, _ = h.resolveMediaPath(r.Context(), itemID, sourceID)
		}
		if mediaPath == "" {
			httpx.Error(w, http.StatusNotFound, "media file not found for subtitle extraction")
			return
		}
		mediaPath, err = h.resolveExtractionMediaPath(r.Context(), mediaPath)
		if err != nil {
			slog.Error("failed to resolve strm for subtitle extraction", "item_id", itemID, "error", err)
			httpx.Error(w, http.StatusBadGateway, "failed to resolve strm stream")
			return
		}
		extractExt := targetFormat
		if extractExt == "" {
			extractExt = foundStream.Codec
		}
		extractedPath, err := h.extractor.ExtractSubtitle(r.Context(), mediaPath, itemID, sourceID, foundStream.StreamIndex, extractExt)
		if err != nil {
			if errors.Is(err, os.ErrPermission) {
				logAndWritePermissionDenied(w, mediaPath, err)
				return
			}
			slog.Error("failed to extract subtitle", "item_id", itemID, "path", mediaPath, "error", err)
			httpx.Error(w, http.StatusInternalServerError, "failed to extract subtitle")
			return
		}
		subPath = extractedPath
	}
	if subPath == "" {
		httpx.Error(w, http.StatusNotFound, "subtitle stream not found")
		return
	}

	q := httpx.ParseQuery(r)
	startTicks := q.Int64("StartPositionTicks", 0)
	ct, data, err := playback.ServeSubtitle(subPath, targetFormat, startTicks)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			logAndWritePermissionDenied(w, subPath, err)
			return
		}
		if os.IsNotExist(err) {
			httpx.Error(w, http.StatusNotFound, "subtitle file missing")
			return
		}
		slog.Error("failed to serve subtitle", "path", subPath, "error", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to serve subtitle")
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "private, max-age=3600")
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
		MaxWidth:   clampImageDim(q.Int("maxWidth", 0)),
		MaxHeight:  clampImageDim(q.Int("maxHeight", 0)),
		Width:      clampImageDim(q.Int("width", 0)),
		Height:     clampImageDim(q.Int("height", 0)),
		FillWidth:  clampImageDim(q.Int("fillWidth", 0)),
		FillHeight: clampImageDim(q.Int("fillHeight", 0)),
		Quality:    q.Int("quality", 0),
		Format:     q.Get("format"),
	}
	err := h.imaging.ServeHTTPWithOptions(w, r, itemID, imgType, idx, opts)
	if errors.Is(err, sqlite.ErrNotFound) || os.IsNotExist(err) {
		// Chapter thumbnails are extracted exclusively by the scheduled ExtractChapterImages task
		// into the dedicated cache/chapters directory; never invoke FFmpeg during page visits.
		if !strings.EqualFold(imgType, "Chapter") && h.tryServeLibraryCover(w, r, itemID, opts) {
			return
		}
		httpx.Error(w, http.StatusNotFound, "image not found")
		return
	}
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			imgPath := itemID
			if h.repo != nil {
				if rec, rErr := h.repo.GetImageByItemAndType(r.Context(), itemID, imgType, idx); rErr == nil && rec != nil && rec.Path != "" {
					imgPath = rec.Path
				}
			}
			logAndWritePermissionDenied(w, imgPath, err)
			return
		}
		slog.Error("failed to serve image", "item_id", itemID, "image_type", imgType, "error", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to serve image")
		return
	}
}

func (h *Handler) tryServeLibraryCover(w http.ResponseWriter, r *http.Request, libraryID string, opts imaging.Options) bool {
	lib, err := h.repo.GetLibrary(r.Context(), libraryID)
	if err != nil || lib == nil {
		return false
	}
	children, _, err := h.repo.QueryItems(r.Context(), sqlite.ItemFilter{
		LibraryID:        lib.ID,
		IncludeItemTypes: []string{"Movie", "Series", "MusicAlbum", "Video"},
		Recursive:        true,
		Limit:            12,
	})
	if err != nil || len(children) == 0 {
		return false
	}
	for _, it := range children {
		if len(it.BackdropImageTags) > 0 {
			if err := h.imaging.ServeHTTPWithOptions(w, r, it.Id, "Backdrop", 0, opts); err == nil {
				return true
			}
		}
	}
	for _, it := range children {
		if it.ImageTags["Primary"] != "" {
			if err := h.imaging.ServeHTTPWithOptions(w, r, it.Id, "Primary", 0, opts); err == nil {
				return true
			}
		}
	}
	return false
}

func (h *Handler) proxyRemoteMedia(w http.ResponseWriter, r *http.Request, targetURL string, asAttachment bool) {
	if err := netutil.ValidateRemoteURL(targetURL, true); err != nil {
		slog.Error("blocked upstream stream url", "url", targetURL, "error", err)
		httpx.Error(w, http.StatusBadGateway, "blocked upstream stream url")
		return
	}
	upReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, nil)
	if err != nil {
		slog.Error("invalid upstream stream url", "url", targetURL, "error", err)
		httpx.Error(w, http.StatusBadGateway, "invalid upstream stream url")
		return
	}
	if rng := r.Header.Get("Range"); rng != "" {
		upReq.Header.Set("Range", rng)
	}
	if ifRange := r.Header.Get("If-Range"); ifRange != "" {
		upReq.Header.Set("If-Range", ifRange)
	}
	ua := r.Header.Get("User-Agent")
	if ua == "" {
		ua = defaultBrowserUA
	}
	upReq.Header.Set("User-Agent", ua)

	resp, err := h.httpClient.Do(upReq)
	if err != nil {
		slog.Error("upstream stream request failed", "url", targetURL, "error", err)
		httpx.Error(w, http.StatusBadGateway, "upstream stream error")
		return
	}
	defer resp.Body.Close()

	for _, hdr := range []string{"Content-Length", "Content-Range", "Accept-Ranges", "ETag", "Last-Modified"} {
		if val := resp.Header.Get(hdr); val != "" {
			w.Header().Set(hdr, val)
		}
	}
	fallbackCT := "application/octet-stream"
	if u, parseErr := url.Parse(targetURL); parseErr == nil {
		fallbackCT = videoContentType(u.Path)
	}
	w.Header().Set("Content-Type", sanitizeProxyMediaContentType(resp.Header.Get("Content-Type"), fallbackCT))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox")
	if asAttachment {
		filename := "media"
		if u, parseErr := url.Parse(targetURL); parseErr == nil {
			if base := filepath.Base(u.Path); base != "" && base != "." && base != "/" {
				filename = base
			}
		}
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, sanitizeContentDispositionFilename(filename)))
	}

	w.WriteHeader(resp.StatusCode)
	if r.Method != http.MethodHead {
		_, _ = io.Copy(w, resp.Body)
	}
}

func sanitizeProxyMediaContentType(rawCT, fallbackCT string) string {
	trimmed := strings.TrimSpace(rawCT)
	if trimmed == "" {
		if fallbackCT != "" {
			return fallbackCT
		}
		return "application/octet-stream"
	}
	mediaType, _, err := mime.ParseMediaType(trimmed)
	if err != nil {
		return "application/octet-stream"
	}
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	if isAllowedProxyMediaMIME(mediaType) {
		return mediaType
	}
	return "application/octet-stream"
}

func isAllowedProxyMediaMIME(mediaType string) bool {
	if strings.HasPrefix(mediaType, "video/") || strings.HasPrefix(mediaType, "audio/") {
		return true
	}
	switch mediaType {
	case "application/octet-stream",
		"application/vnd.apple.mpegurl",
		"application/x-mpegurl",
		"application/mp4",
		"application/mp2t",
		"text/vtt":
		return true
	default:
		return false
	}
}

func sanitizeContentDispositionFilename(name string) string {
	name = strings.ReplaceAll(name, "\r", "")
	name = strings.ReplaceAll(name, "\n", "")
	name = strings.ReplaceAll(name, "\\", "\\\\")
	name = strings.ReplaceAll(name, `"`, `\"`)
	if name == "" {
		return "media"
	}
	return name
}

func (h *Handler) resolveExtractionMediaPath(ctx context.Context, mediaPath string) (string, error) {
	if !strings.EqualFold(filepath.Ext(mediaPath), ".strm") {
		return mediaPath, nil
	}
	if h.engine == nil {
		return "", errors.New("strm resolver unavailable")
	}
	resolved, err := h.engine.ResolveStream(ctx, mediaPath)
	if err != nil {
		return "", err
	}
	if resolved == nil || !isHTTPURL(resolved.URL) {
		return "", errors.New("invalid strm target url")
	}
	targetURL := strings.TrimSpace(resolved.URL)
	if err := netutil.ValidateRemoteURL(targetURL, true); err != nil {
		return "", err
	}
	return targetURL, nil
}

func isHTTPURL(s string) bool {
	lower := strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
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
