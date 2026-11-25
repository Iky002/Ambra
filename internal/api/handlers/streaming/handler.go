package streaming

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/core/imaging"
	"github.com/ambra-media/ambra/internal/core/playback"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/pkg/netutil"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

const defaultBrowserUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

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
	if h.userRepo == nil || userID == "" || libraryID == "" {
		return true
	}
	u, err := h.userRepo.GetUserByID(ctx, guid.Normalize(userID))
	if err != nil || u == nil {
		return true
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

func (h *Handler) canAccessItem(r *http.Request, itemID string) bool {
	if h.userRepo == nil || h.repo == nil {
		return true
	}
	var callerID string
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && (ac.User != nil || ac.IsAPIKey) {
		if ac.IsAdmin() {
			return true
		}
		callerID = guid.Normalize(ac.UserID())
	} else if h.auth.UserIDFromCtx != nil {
		callerID = guid.Normalize(h.auth.UserIDFromCtx(r.Context()))
	}
	if callerID == "" {
		return true
	}
	rec, err := h.repo.GetItemRecordByID(r.Context(), itemID)
	if err != nil || rec == nil {
		return true
	}
	if rec.LibraryID != "" && !h.CanAccessLibrary(r.Context(), callerID, rec.LibraryID) {
		return false
	}
	u, err := h.userRepo.GetUserByID(r.Context(), callerID)
	if err != nil || u == nil || u.IsAdmin || u.Policy.IsAdministrator {
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
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Check STRM 302 resolution first
	forceProxy := false
	if b := q.Bool("proxy"); b != nil && *b {
		forceProxy = true
	}
	if strings.EqualFold(filepath.Ext(mediaPath), ".strm") {
		resolved, err := h.engine.ResolveStream(r.Context(), mediaPath)
		if err != nil {
			httpx.Error(w, http.StatusBadGateway, "failed to resolve strm: "+err.Error())
			return
		}
		if resolved != nil && resolved.URL != "" {
			if resolved.Is302 && !forceProxy {
				w.Header().Set("Cache-Control", "no-store")
				http.Redirect(w, r, resolved.URL, http.StatusFound)
				return
			}
			mediaPath = resolved.URL
		}
	}

	// Reverse-proxy remote HTTP(S) streams (e.g. non-302 STRM or explicit proxy=1/true)
	if isHTTPURL(mediaPath) {
		h.proxyRemoteMedia(w, r, mediaPath, asAttachment)
		return
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
		httpx.Error(w, http.StatusInternalServerError, err.Error())
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
		extractExt := targetFormat
		if extractExt == "" {
			extractExt = foundStream.Codec
		}
		extractedPath, err := h.extractor.ExtractSubtitle(r.Context(), mediaPath, itemID, sourceID, foundStream.StreamIndex, extractExt)
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, err.Error())
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
		if strings.EqualFold(imgType, "Chapter") && h.tryExtractAndServeChapterImage(w, r, itemID, idx, opts) {
			return
		}
		if h.tryServeLibraryCover(w, r, itemID, opts) {
			return
		}
		httpx.Error(w, http.StatusNotFound, "image not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
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
	mediaPath, err := h.resolveMediaPath(r.Context(), normID, "")
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
		httpx.Error(w, http.StatusBadGateway, "blocked upstream stream url: "+err.Error())
		return
	}
	upReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, nil)
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, "invalid upstream stream url: "+err.Error())
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
		httpx.Error(w, http.StatusBadGateway, "upstream stream error: "+err.Error())
		return
	}
	defer resp.Body.Close()

	for _, hdr := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "ETag", "Last-Modified"} {
		if val := resp.Header.Get(hdr); val != "" {
			w.Header().Set(hdr, val)
		}
	}
	if w.Header().Get("Content-Type") == "" {
		if u, parseErr := url.Parse(targetURL); parseErr == nil {
			w.Header().Set("Content-Type", videoContentType(u.Path))
		}
	}
	if asAttachment {
		filename := "media"
		if u, parseErr := url.Parse(targetURL); parseErr == nil {
			if base := filepath.Base(u.Path); base != "" && base != "." && base != "/" {
				filename = base
			}
		}
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	}

	w.WriteHeader(resp.StatusCode)
	if r.Method != http.MethodHead {
		_, _ = io.Copy(w, resp.Body)
	}
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
