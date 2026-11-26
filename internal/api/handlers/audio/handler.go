package audio

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/pkg/netutil"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

const (
	ticksPerSecond               = 10_000_000
	maxAudioBitrate              = 1_536_000
	maxAudioChannels             = 8
	maxAudioSampleRate           = 192_000
	maxConcurrentAudioTranscodes = 8
	defaultAudioBrowserUA        = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)

// AuthFuncs decouples route registration from the concrete middleware package.
type AuthFuncs struct {
	RequireAuth   func(http.Handler) http.Handler
	RequireAdmin  func(http.Handler) http.Handler
	UserIDFromCtx func(context.Context) string
}

// LyricMetadata holds parsed LRC header tags.
type LyricMetadata struct {
	Artist   string `json:"Artist,omitempty"`
	Album    string `json:"Album,omitempty"`
	Title    string `json:"Title,omitempty"`
	Author   string `json:"Author,omitempty"`
	IsSynced bool   `json:"IsSynced"`
}

// LyricLine represents a single timestamped lyric line in Emby/Jellyfin LyricDto format.
type LyricLine struct {
	Text  string `json:"Text"`
	Start *int64 `json:"Start,omitempty"` // Emby ticks (10,000,000 = 1s)
}

// LyricDto is the response payload for GET /Audio/{Id}/Lyrics.
type LyricDto struct {
	Metadata LyricMetadata `json:"Metadata"`
	Lyrics   []LyricLine   `json:"Lyrics"`
}

// Handler serves audio direct streams, universal audio streaming/transcoding, dynamic audio HLS, and lyrics.
type Handler struct {
	repo                      *sqlite.LibraryRepo
	userRepo                  *sqlite.UserRepo
	cfg                       *config.Config
	transcodeMgr              *transcode.Manager
	auth                      AuthFuncs
	streamResolver            func(ctx context.Context, rawPath string) (string, error)
	streamResolverFull        func(ctx context.Context, rawPath string) (string, bool, error)
	streamResolverWithHeaders func(ctx context.Context, rawPath string) (*plugins.StreamResolveResult, error)
	httpClient                *http.Client
	transcodeSem              chan struct{}
	activePipeStreams         atomic.Int64
}

// New constructs an Audio API handler.
func New(repo *sqlite.LibraryRepo, cfg *config.Config) *Handler {
	return &Handler{
		repo:         repo,
		cfg:          cfg,
		httpClient:   netutil.NewSafeHTTPClient(0, true),
		transcodeSem: make(chan struct{}, maxConcurrentAudioTranscodes),
	}
}

// SetUserRepo attaches a UserRepo for per-user library access control on audio streams and lyrics.
func (h *Handler) SetUserRepo(userRepo *sqlite.UserRepo) {
	h.userRepo = userRepo
}

// SetTranscodeManager attaches the shared transcode Manager for global stream limit coordination and dynamic audio HLS.
func (h *Handler) SetTranscodeManager(mgr *transcode.Manager) {
	h.transcodeMgr = mgr
}

// SetStreamResolver attaches a resolver (e.g. playback.Engine.ResolveStream) for .strm audio files.
func (h *Handler) SetStreamResolver(fn func(ctx context.Context, rawPath string) (string, error)) {
	h.streamResolver = fn
}

// SetStreamResolverFull attaches a resolver returning (targetURL, is302, err) for .strm audio files.
func (h *Handler) SetStreamResolverFull(fn func(ctx context.Context, rawPath string) (string, bool, error)) {
	h.streamResolverFull = fn
}

// SetStreamResolverWithHeaders attaches a full resolver returning URL, custom HTTP headers, and Is302 flag.
func (h *Handler) SetStreamResolverWithHeaders(fn func(ctx context.Context, rawPath string) (*plugins.StreamResolveResult, error)) {
	h.streamResolverWithHeaders = fn
}

// SetProxyClient overrides the HTTP client used for remote STRM audio reverse-proxying (primarily for testing).
func (h *Handler) SetProxyClient(client *http.Client) {
	if client != nil {
		h.httpClient = client
	}
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

func (h *Handler) callerPolicy(r *http.Request) (*dto.UserPolicy, string, bool) {
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && (ac.User != nil || ac.IsAPIKey) {
		if ac.IsAdmin() {
			return nil, guid.Normalize(ac.UserID()), true
		}
		uid := guid.Normalize(ac.UserID())
		if h.userRepo != nil && uid != "" {
			if u, err := h.userRepo.GetUserByID(r.Context(), uid); err == nil && u != nil {
				if u.IsAdmin || u.Policy.IsAdministrator {
					return nil, uid, true
				}
				pol := u.Policy
				return &pol, uid, false
			}
		}
		if ac.User != nil {
			pol := ac.User.Policy
			return &pol, uid, false
		}
	}
	if h.auth.UserIDFromCtx != nil {
		uid := guid.Normalize(h.auth.UserIDFromCtx(r.Context()))
		if uid != "" && h.userRepo != nil {
			if u, err := h.userRepo.GetUserByID(r.Context(), uid); err == nil && u != nil {
				if u.IsAdmin || u.Policy.IsAdministrator {
					return nil, uid, true
				}
				pol := u.Policy
				return &pol, uid, false
			}
		}
		return nil, uid, false
	}
	return nil, "", false
}

func matchesBlockUnratedAudio(itemType, mediaType string, blockUnrated []string) bool {
	for _, b := range blockUnrated {
		t := strings.TrimSpace(b)
		if t == "" {
			continue
		}
		if t == "*" || strings.EqualFold(t, "All") || strings.EqualFold(t, itemType) || (mediaType != "" && strings.EqualFold(t, mediaType)) {
			return true
		}
		if strings.EqualFold(t, "Music") && (strings.EqualFold(itemType, "Audio") || strings.EqualFold(itemType, "MusicAlbum") || strings.EqualFold(itemType, "MusicArtist") || strings.EqualFold(mediaType, "Audio")) {
			return true
		}
	}
	return false
}

func (h *Handler) isBlockedByExtendedAudioPolicy(ctx context.Context, rec *sqlite.ItemRecord, pol dto.UserPolicy) bool {
	if rec == nil || h.repo == nil {
		return false
	}
	if len(pol.BlockUnratedItems) > 0 && matchesBlockUnratedAudio(rec.Type, rec.MediaType, pol.BlockUnratedItems) {
		if strings.TrimSpace(rec.OfficialRating) == "" {
			return true
		}
	}
	if len(pol.AllowedTags) > 0 {
		allowSet := make(map[string]bool, len(pol.AllowedTags))
		for _, t := range pol.AllowedTags {
			if s := strings.ToLower(strings.TrimSpace(t)); s != "" {
				allowSet[s] = true
			}
		}
		if len(allowSet) > 0 {
			matched := false
			_, _, tags, err := h.repo.GetTaxonomiesByItemID(ctx, rec.ID)
			if err == nil {
				for _, tg := range tags {
					if allowSet[strings.ToLower(strings.TrimSpace(tg))] {
						matched = true
						break
					}
				}
			}
			if !matched {
				return true
			}
		}
	}
	return false
}

func isLocalOrSubnetAudioRequest(r *http.Request, customSubnets []string) bool {
	if netutil.IsLocalOrLANRequest(r) {
		return true
	}
	if len(customSubnets) == 0 {
		return false
	}
	addr, err := netip.ParseAddr(netutil.ClientIP(r))
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	for _, raw := range customSubnets {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		if pfx, err := netip.ParsePrefix(s); err == nil && pfx.Contains(addr) {
			return true
		}
		if single, err := netip.ParseAddr(s); err == nil && single.Unmap() == addr {
			return true
		}
	}
	return false
}

func (h *Handler) canAccessItem(r *http.Request, itemID string) bool {
	if h.repo == nil {
		return true
	}
	if h.userRepo == nil {
		if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && ac.User != nil && !ac.IsAdmin() {
			rec, err := h.repo.GetItemRecordByID(r.Context(), itemID)
			if errors.Is(err, sqlite.ErrNotFound) {
				return true
			}
			if err != nil || rec == nil {
				return false
			}
			pol := ac.User.Policy
			if rec.LibraryID != "" && !pol.EnableAllFolders {
				allowed := false
				normLib := guid.Normalize(rec.LibraryID)
				for _, fid := range pol.EnabledFolders {
					if guid.Normalize(fid) == normLib {
						allowed = true
						break
					}
				}
				if !allowed {
					return false
				}
			}
			blocked, err := h.repo.IsItemBlockedByParentalPolicy(r.Context(), rec, pol.MaxParentalRating, pol.BlockedTags)
			if err != nil || blocked {
				return false
			}
			return !h.isBlockedByExtendedAudioPolicy(r.Context(), rec, pol)
		}
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
	if err != nil || blocked {
		return false
	}
	return !h.isBlockedByExtendedAudioPolicy(r.Context(), rec, u.Policy)
}

// RegisterRoutes mounts all /Audio/* endpoints on mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth AuthFuncs) {
	h.auth = auth
	wrapAuth := auth.RequireAuth
	if wrapAuth == nil {
		wrapAuth = passthrough
	}

	// Note: In Go 1.22+ ServeMux, "GET ..." automatically matches both GET and HEAD.
	mux.Handle("GET /Audio/{Id}/Lyrics", wrapAuth(http.HandlerFunc(h.handleLyrics)))
	mux.Handle("GET /Audio/{Id}/hls1/{PlaylistId}/{Segment}", wrapAuth(http.HandlerFunc(h.handleAudioHLSSegment)))
	mux.Handle("GET /Audio/{Id}/hls/{PlaylistId}/{Segment}", wrapAuth(http.HandlerFunc(h.handleAudioHLSSegment)))
	mux.Handle("GET /Audio/{Id}/{File}", wrapAuth(http.HandlerFunc(h.handleAudioFile)))
}

func (h *Handler) handleAudioFile(w http.ResponseWriter, r *http.Request) {
	fileSeg := strings.ToLower(r.PathValue("File"))
	switch {
	case fileSeg == "lyrics":
		h.handleLyrics(w, r)
	case fileSeg == "master.m3u8" || fileSeg == "main.m3u8" || fileSeg == "live.m3u8":
		h.handleAudioHLSPlaylist(w, r, fileSeg)
	case fileSeg == "stream" || strings.HasPrefix(fileSeg, "stream."):
		h.handleDirectStream(w, r)
	case fileSeg == "universal" || strings.HasPrefix(fileSeg, "universal."):
		ext := ""
		if dot := strings.LastIndexByte(fileSeg, '.'); dot != -1 {
			ext = fileSeg[dot+1:]
		}
		h.handleUniversal(w, r, ext)
	default:
		httpx.Error(w, http.StatusNotFound, "unsupported audio endpoint")
	}
}

func (h *Handler) handleDirectStream(w http.ResponseWriter, r *http.Request) {
	itemID := guid.Normalize(r.PathValue("Id"))
	if !h.canAccessItem(r, itemID) {
		httpx.Error(w, http.StatusForbidden, "access denied")
		return
	}
	q := httpx.ParseQuery(r)
	mediaPath, _, _, err := h.resolveAudioPath(r.Context(), itemID, q.Get("MediaSourceId"))
	if errors.Is(err, sqlite.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "audio item not found")
		return
	}
	if err != nil {
		slog.Error("failed to resolve audio path", "item_id", itemID, "error", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to resolve audio path")
		return
	}
	forceProxy := false
	if b := q.Bool("proxy"); b != nil && *b {
		forceProxy = true
	}
	var resolvedHeaders map[string]string
	if strings.EqualFold(filepath.Ext(mediaPath), ".strm") {
		resolved, headers, is302, err := h.resolveStrmAudioWithHeaders(r.Context(), mediaPath)
		if err != nil {
			slog.Error("failed to resolve strm audio", "path", mediaPath, "error", err)
			httpx.Error(w, http.StatusBadGateway, "failed to resolve strm stream")
			return
		}
		resolvedHeaders = headers
		if is302 && !forceProxy && len(resolvedHeaders) == 0 {
			w.Header().Set("Cache-Control", "no-store")
			http.Redirect(w, r, resolved, http.StatusFound)
			return
		}
		mediaPath = resolved
	}
	if isHTTPURL(mediaPath) {
		h.proxyRemoteAudioWithHeaders(w, r, mediaPath, resolvedHeaders)
		return
	}
	serveAudioFile(w, r, mediaPath)
}

func (h *Handler) handleUniversal(w http.ResponseWriter, r *http.Request, urlExt string) {
	itemID := guid.Normalize(r.PathValue("Id"))
	if !h.canAccessItem(r, itemID) {
		httpx.Error(w, http.StatusForbidden, "access denied")
		return
	}
	q := httpx.ParseQuery(r)
	mediaPath, srcBitrate, srcContainer, err := h.resolveAudioPath(r.Context(), itemID, q.Get("MediaSourceId"))
	if errors.Is(err, sqlite.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "audio item not found")
		return
	}
	if err != nil {
		slog.Error("failed to resolve audio path", "item_id", itemID, "error", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to resolve audio path")
		return
	}
	forceProxy := false
	if b := q.Bool("proxy"); b != nil && *b {
		forceProxy = true
	}
	is302 := false
	var resolvedHeaders map[string]string
	if strings.EqualFold(filepath.Ext(mediaPath), ".strm") {
		resolved, headers, redirect302, err := h.resolveStrmAudioWithHeaders(r.Context(), mediaPath)
		if err != nil {
			slog.Error("failed to resolve strm audio", "path", mediaPath, "error", err)
			httpx.Error(w, http.StatusBadGateway, "failed to resolve strm stream")
			return
		}
		mediaPath = resolved
		resolvedHeaders = headers
		is302 = redirect302
	}

	isRemote := isHTTPURL(mediaPath)
	srcExt := strings.ToLower(strings.TrimPrefix(filepath.Ext(mediaPath), "."))
	if isRemote {
		srcExt = ""
		if u, parseErr := url.Parse(mediaPath); parseErr == nil {
			srcExt = strings.ToLower(strings.TrimPrefix(filepath.Ext(u.Path), "."))
		}
	}
	if srcExt == "" && srcContainer != "" {
		srcExt = srcContainer
	}
	allowedContainers := q.List("Container")
	maxBitrate := q.Int("MaxStreamingBitrate", 0)
	if maxBitrate <= 0 {
		maxBitrate = q.Int("AudioBitrate", 0)
	}
	if h.cfg != nil {
		snap := h.cfg.Snapshot()
		if snap.RemoteClientBitrateLimit > 0 && !isLocalOrSubnetAudioRequest(r, snap.LocalNetworkSubnets) {
			if maxBitrate <= 0 || maxBitrate > snap.RemoteClientBitrateLimit {
				maxBitrate = snap.RemoteClientBitrateLimit
			}
		}
	}
	startTimeTicks := q.Int64("StartTimeTicks", 0)

	// Direct play if no seek offset is requested, source container is supported by client, and within bitrate limit.
	// For extensionless remote STRM 302 signed URLs without seek, custom headers, or bitrate limits, prefer 302 redirect over FFmpeg transcode.
	bitrateOK := maxBitrate <= 0 || srcBitrate <= 0 || srcBitrate <= maxBitrate
	directContainerOK := isContainerAllowed(srcExt, allowedContainers) && (urlExt == "" || urlExt == srcExt)
	extensionless302OK := isRemote && is302 && !forceProxy && len(resolvedHeaders) == 0 && srcExt == ""
	if startTimeTicks <= 0 && bitrateOK && (directContainerOK || extensionless302OK) {
		if isRemote {
			if is302 && !forceProxy && len(resolvedHeaders) == 0 {
				w.Header().Set("Cache-Control", "no-store")
				http.Redirect(w, r, mediaPath, http.StatusFound)
				return
			}
			h.proxyRemoteAudioWithHeaders(w, r, mediaPath, resolvedHeaders)
			return
		}
		serveAudioFile(w, r, mediaPath)
		return
	}

	// [H-05] Enforce per-user audio transcoding permission before launching FFmpeg.
	if pol, _, isAdmin := h.callerPolicy(r); !isAdmin && pol != nil && !pol.EnableAudioPlaybackTranscoding {
		httpx.Error(w, http.StatusForbidden, "audio playback transcoding is disabled for this user")
		return
	}

	// Otherwise stream-transcode via FFmpeg to requested transcoding container/codec
	targetContainer := strings.ToLower(strings.TrimPrefix(q.Get("TranscodingContainer"), "."))
	if targetContainer == "" {
		targetContainer = urlExt
	}
	if targetContainer == "" {
		targetContainer = "mp3"
	}
	targetBitrate := q.Int("AudioBitrate", 192_000)
	if maxBitrate > 0 && maxBitrate < targetBitrate {
		targetBitrate = maxBitrate
	}
	if targetBitrate <= 0 {
		targetBitrate = 192_000
	}
	if targetBitrate > maxAudioBitrate {
		targetBitrate = maxAudioBitrate
	}

	audioChannels := q.Int("AudioChannels", 0)
	if audioChannels <= 0 {
		audioChannels = q.Int("MaxAudioChannels", 0)
	}
	if audioChannels > maxAudioChannels {
		audioChannels = maxAudioChannels
	}
	audioSampleRate := q.Int("AudioSampleRate", 0)
	if audioSampleRate > maxAudioSampleRate {
		audioSampleRate = maxAudioSampleRate
	}

	h.streamTranscodeAudioWithHeaders(w, r, mediaPath, resolvedHeaders, targetContainer, q.Get("AudioCodec"), targetBitrate, startTimeTicks, audioChannels, audioSampleRate)
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

func (h *Handler) streamTranscodeAudio(w http.ResponseWriter, r *http.Request, mediaPath, targetContainer, targetCodec string, bitrate int, startTimeTicks int64, audioChannels, audioSampleRate int) {
	h.streamTranscodeAudioWithHeaders(w, r, mediaPath, nil, targetContainer, targetCodec, bitrate, startTimeTicks, audioChannels, audioSampleRate)
}

func (h *Handler) streamTranscodeAudioWithHeaders(w http.ResponseWriter, r *http.Request, mediaPath string, inputHeaders map[string]string, targetContainer, targetCodec string, bitrate int, startTimeTicks int64, audioChannels, audioSampleRate int) {
	if pol, _, isAdmin := h.callerPolicy(r); !isAdmin && pol != nil && !pol.EnableAudioPlaybackTranscoding {
		httpx.Error(w, http.StatusForbidden, "audio playback transcoding is disabled for this user")
		return
	}

	if isHTTPURL(mediaPath) {
		if err := netutil.ValidateRemoteURL(mediaPath, true); err != nil {
			slog.Error("blocked upstream audio url", "url", mediaPath, "error", err)
			httpx.Error(w, http.StatusBadGateway, "blocked upstream audio url")
			return
		}
	} else if _, err := os.Stat(mediaPath); err != nil {
		if errors.Is(err, os.ErrPermission) {
			logAndWritePermissionDenied(w, mediaPath, err)
			return
		}
		if os.IsNotExist(err) {
			httpx.Error(w, http.StatusNotFound, "audio file missing on disk")
			return
		}
		slog.Error("failed to stat audio file", "path", mediaPath, "error", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to access audio file")
		return
	}

	ct, muxFmt, encoder := resolveAudioTranscodeTarget(targetContainer, targetCodec)
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}

	// [H-05] Enforce global SimultaneousStreamLimit across HLS and pipe transcodes.
	if h.cfg != nil {
		if limit := h.cfg.Snapshot().SimultaneousStreamLimit; limit > 0 {
			if h.transcodeMgr != nil {
				releaseSlot, allowed := h.transcodeMgr.TryAcquireStreamSlot(limit, "")
				if !allowed {
					httpx.Error(w, http.StatusTooManyRequests, "simultaneous stream limit reached")
					return
				}
				defer releaseSlot()
			} else {
				cur := h.activePipeStreams.Add(1)
				defer h.activePipeStreams.Add(-1)
				if int(cur) > limit {
					httpx.Error(w, http.StatusTooManyRequests, "simultaneous stream limit reached")
					return
				}
			}
		}
	}

	if h.transcodeSem != nil {
		select {
		case h.transcodeSem <- struct{}{}:
			defer func() { <-h.transcodeSem }()
		default:
			httpx.Error(w, http.StatusServiceUnavailable, "audio transcoding concurrency limit reached")
			return
		}
	}

	ffmpegInput := mediaPath
	if isHTTPURL(mediaPath) {
		proxy, proxyErr := transcode.StartLoopbackStreamProxy(transcode.LoopbackProxyOptions{
			TargetURL:       mediaPath,
			Headers:         inputHeaders,
			AllowPrivateLAN: true,
		})
		if proxyErr != nil {
			slog.Error("failed to start loopback proxy for audio transcoding", "url", mediaPath, "error", proxyErr)
			httpx.Error(w, http.StatusBadGateway, "blocked upstream audio url")
			return
		}
		defer proxy.Close()
		ffmpegInput = proxy.URL()
	}

	ffmpegPath := "ffmpeg"
	if h.cfg != nil {
		if p := h.cfg.Snapshot().FFmpegPath; p != "" {
			ffmpegPath = p
		}
	}

	args := netutil.FilterFFmpegArgs(ffmpegPath, buildAudioFFmpegArgs(ffmpegInput, muxFmt, encoder, bitrate, startTimeTicks, audioChannels, audioSampleRate))
	cmd := exec.CommandContext(r.Context(), ffmpegPath, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		slog.Error("failed to open ffmpeg stdout pipe", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to start audio transcoding")
		return
	}
	if err := cmd.Start(); err != nil {
		slog.Error("failed to start ffmpeg for audio transcoding", "ffmpeg", ffmpegPath, "error", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to start audio transcoding")
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, stdout)
	_ = cmd.Wait()
}

func buildAudioFFmpegArgs(mediaPath, muxFmt, encoder string, bitrate int, startTimeTicks int64, audioChannels, audioSampleRate int) []string {
	if bitrate > maxAudioBitrate {
		bitrate = maxAudioBitrate
	}
	if audioChannels > maxAudioChannels {
		audioChannels = maxAudioChannels
	}
	if audioSampleRate > maxAudioSampleRate {
		audioSampleRate = maxAudioSampleRate
	}
	args := []string{"-hide_banner", "-loglevel", "error"}
	if isHTTPURL(mediaPath) {
		args = append(args,
			"-protocol_whitelist", "http,https,tcp,tls,crypto",
			"-max_redirects", "0",
			"-rw_timeout", "15000000",
		)
	} else {
		args = append(args, "-protocol_whitelist", "file,crypto,data")
	}
	if startTimeTicks > 0 {
		secs := float64(startTimeTicks) / float64(ticksPerSecond)
		args = append(args, "-ss", strconv.FormatFloat(secs, 'f', 3, 64))
	}
	args = append(args,
		"-i", mediaPath,
		"-vn",
		"-c:a", encoder,
		"-b:a", strconv.Itoa(bitrate),
	)
	if audioChannels > 0 {
		args = append(args, "-ac", strconv.Itoa(audioChannels))
	}
	if audioSampleRate > 0 {
		args = append(args, "-ar", strconv.Itoa(audioSampleRate))
	}
	if muxFmt == "mp4" {
		args = append(args, "-movflags", "frag_keyframe+empty_moov")
	}
	args = append(args, "-f", muxFmt, "pipe:1")
	return args
}

func (h *Handler) handleLyrics(w http.ResponseWriter, r *http.Request) {
	itemID := guid.Normalize(r.PathValue("Id"))
	if !h.canAccessItem(r, itemID) {
		httpx.Error(w, http.StatusForbidden, "access denied")
		return
	}
	q := httpx.ParseQuery(r)
	mediaPath, _, _, err := h.resolveAudioPath(r.Context(), itemID, q.Get("MediaSourceId"))
	if errors.Is(err, sqlite.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "audio item not found")
		return
	}
	if err != nil {
		slog.Error("failed to resolve audio path for lyrics", "item_id", itemID, "error", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to resolve audio path")
		return
	}

	lrcPath := findLRCFile(mediaPath)
	if lrcPath != "" {
		fi, stErr := os.Stat(lrcPath)
		if stErr != nil {
			if errors.Is(stErr, os.ErrPermission) {
				logAndWritePermissionDenied(w, lrcPath, stErr)
				return
			}
			httpx.Error(w, http.StatusNotFound, "lyrics not found")
			return
		}
		if fi.Size() > maxExternalLRCBytes {
			httpx.Error(w, http.StatusRequestEntityTooLarge, "external lrc file exceeds 2 MiB limit")
			return
		}
		f, openErr := os.Open(lrcPath)
		if openErr != nil {
			if errors.Is(openErr, os.ErrPermission) {
				logAndWritePermissionDenied(w, lrcPath, openErr)
				return
			}
			if os.IsNotExist(openErr) {
				httpx.Error(w, http.StatusNotFound, "lyrics not found")
				return
			}
			slog.Error("failed to read lyrics file", "path", lrcPath, "error", openErr)
			httpx.Error(w, http.StatusInternalServerError, "failed to read lyrics file")
			return
		}
		raw, readErr := io.ReadAll(io.LimitReader(f, maxExternalLRCBytes+1))
		_ = f.Close()
		if readErr != nil {
			slog.Error("failed to read lyrics file", "path", lrcPath, "error", readErr)
			httpx.Error(w, http.StatusInternalServerError, "failed to read lyrics file")
			return
		}
		if len(raw) > maxExternalLRCBytes {
			httpx.Error(w, http.StatusRequestEntityTooLarge, "external lrc file exceeds 2 MiB limit")
			return
		}
		lyricDTO := ParseLRC(string(raw))
		httpx.JSON(w, http.StatusOK, lyricDTO)
		return
	}

	// Fallback: pure-Go embedded lyrics extraction from local FLAC / MP3 / M4A header tags.
	if !isHTTPURL(mediaPath) && !strings.EqualFold(filepath.Ext(mediaPath), ".strm") {
		if embedded, embErr := ExtractEmbeddedLyrics(mediaPath); embErr == nil && strings.TrimSpace(embedded) != "" {
			lyricDTO := ParseLRC(embedded)
			httpx.JSON(w, http.StatusOK, lyricDTO)
			return
		}
	}

	httpx.Error(w, http.StatusNotFound, "lyrics not found")
}

func (h *Handler) buildAudioHLSSessionParams(r *http.Request, itemID string) (transcode.SessionParams, *sqlite.ItemRecord, error) {
	q := httpx.ParseQuery(r)
	mediaSourceID := guid.Normalize(q.Get("MediaSourceId"))
	mediaPath, _, _, err := h.resolveAudioPath(r.Context(), itemID, mediaSourceID)
	if err != nil {
		return transcode.SessionParams{}, nil, err
	}
	var headers map[string]string
	if strings.EqualFold(filepath.Ext(mediaPath), ".strm") {
		resolved, hdrs, _, rErr := h.resolveStrmAudioWithHeaders(r.Context(), mediaPath)
		if rErr != nil {
			return transcode.SessionParams{}, nil, rErr
		}
		mediaPath = resolved
		headers = hdrs
	} else if !isHTTPURL(mediaPath) {
		if fi, stErr := os.Stat(mediaPath); stErr != nil || fi.IsDir() {
			return transcode.SessionParams{}, nil, sqlite.ErrNotFound
		}
	}
	rec, _ := h.repo.GetItemRecordByID(r.Context(), itemID)
	_, callerID, isAdmin := h.callerPolicy(r)
	rawPS := strings.TrimSpace(q.Get("PlaySessionId"))
	if rawPS == "" {
		rawPS = strings.TrimSpace(r.PathValue("PlaylistId"))
	}
	if rawPS != "" && !isAdmin && callerID != "" && !strings.HasPrefix(rawPS, callerID+":") {
		rawPS = callerID + ":" + rawPS
	}
	segExt := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(q.Get("SegmentContainer")), "."))
	if segExt != "mp4" && segExt != "m4s" {
		segExt = "ts"
	}
	bitrate := q.Int("AudioBitrate", 192_000)
	if h.cfg != nil {
		snap := h.cfg.Snapshot()
		if snap.RemoteClientBitrateLimit > 0 && !isLocalOrSubnetAudioRequest(r, snap.LocalNetworkSubnets) && bitrate > snap.RemoteClientBitrateLimit {
			bitrate = snap.RemoteClientBitrateLimit
		}
	}
	var durSec float64
	if rec != nil && rec.RuntimeTicks != nil && *rec.RuntimeTicks > 0 {
		durSec = float64(*rec.RuntimeTicks) / float64(ticksPerSecond)
	}
	if durSec <= 0 {
		durSec = 240
	}
	return transcode.SessionParams{
		PlaySessionID:    rawPS,
		DeviceID:         strings.TrimSpace(q.Get("DeviceId")),
		UserID:           callerID,
		ItemID:           itemID,
		MediaSourceID:    firstNonEmptyAudio(mediaSourceID, itemID),
		InputPath:        mediaPath,
		InputHeaders:     headers,
		VideoCodec:       "none",
		AudioCodec:       firstNonEmptyAudio(q.Get("AudioCodec"), "aac"),
		AudioBitrate:     bitrate,
		AudioChannels:    q.Int("AudioChannels", 2),
		SegmentDuration:  q.Int("SegmentLength", transcode.DefaultSegmentDuration),
		SegmentContainer: segExt,
		DurationSec:      durSec,
		AudioOnly:        true,
	}, rec, nil
}

func firstNonEmptyAudio(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

func (h *Handler) handleAudioHLSPlaylist(w http.ResponseWriter, r *http.Request, playlistFile string) {
	itemID := guid.Normalize(r.PathValue("Id"))
	if !h.canAccessItem(r, itemID) {
		httpx.Error(w, http.StatusForbidden, "access denied")
		return
	}
	if pol, _, isAdmin := h.callerPolicy(r); !isAdmin && pol != nil && !pol.EnableAudioPlaybackTranscoding {
		httpx.Error(w, http.StatusForbidden, "audio playback transcoding is disabled for this user")
		return
	}
	params, _, err := h.buildAudioHLSSessionParams(r, itemID)
	if errors.Is(err, sqlite.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "audio item not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, "failed to prepare audio hls stream")
		return
	}
	if h.cfg != nil && h.transcodeMgr != nil {
		if limit := h.cfg.Snapshot().SimultaneousStreamLimit; limit > 0 {
			if _, allowed := h.transcodeMgr.TryAcquireStreamSlot(limit, transcode.SessionKey(params)); !allowed {
				httpx.Error(w, http.StatusTooManyRequests, "simultaneous stream limit reached")
				return
			}
		}
		_, _ = h.transcodeMgr.RegisterSession(params)
	}

	rawQuery := r.URL.RawQuery
	var body string
	if playlistFile == "master.m3u8" {
		target := "main.m3u8"
		if rawQuery != "" {
			target += "?" + rawQuery
		}
		bw := params.AudioBitrate
		if bw <= 0 {
			bw = 192_000
		}
		body = fmt.Sprintf("#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-STREAM-INF:BANDWIDTH=%d,CODECS=\"mp4a.40.2\"\n%s\n", bw, target)
	} else {
		var qVals url.Values
		if rawQuery != "" {
			qVals, _ = url.ParseQuery(rawQuery)
		}
		body = transcode.BuildMediaPlaylist(transcode.PlaylistParams{
			ItemID:           itemID,
			PlaylistID:       "main",
			DurationSec:      params.DurationSec,
			SegmentDuration:  params.SegmentDuration,
			SegmentContainer: params.SegmentContainer,
			Query:            qVals,
		})
	}
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write([]byte(body))
	}
}

func (h *Handler) handleAudioHLSSegment(w http.ResponseWriter, r *http.Request) {
	itemID := guid.Normalize(r.PathValue("Id"))
	if !h.canAccessItem(r, itemID) {
		httpx.Error(w, http.StatusForbidden, "access denied")
		return
	}
	if pol, _, isAdmin := h.callerPolicy(r); !isAdmin && pol != nil && !pol.EnableAudioPlaybackTranscoding {
		httpx.Error(w, http.StatusForbidden, "audio playback transcoding is disabled for this user")
		return
	}
	if h.transcodeMgr == nil {
		httpx.Error(w, http.StatusNotFound, "audio hls segment not available")
		return
	}
	segRaw := strings.TrimSpace(r.PathValue("Segment"))
	isInit := strings.EqualFold(segRaw, "init.mp4")
	segIdx := 0
	segExt := "ts"
	if isInit {
		segExt = "m4s"
	} else {
		dot := strings.LastIndexByte(segRaw, '.')
		if dot <= 0 {
			httpx.Error(w, http.StatusBadRequest, "invalid audio hls segment")
			return
		}
		base := strings.TrimPrefix(strings.TrimPrefix(segRaw[:dot], "seg_"), "segment")
		parsed, convErr := strconv.Atoi(base)
		if convErr != nil || parsed < 0 {
			httpx.Error(w, http.StatusBadRequest, "invalid audio hls segment index")
			return
		}
		segIdx = parsed
		segExt = strings.ToLower(segRaw[dot+1:])
		if segExt != "mp4" && segExt != "m4s" {
			segExt = "ts"
		}
	}

	params, _, err := h.buildAudioHLSSessionParams(r, itemID)
	if errors.Is(err, sqlite.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "audio item not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, "failed to resolve audio source")
		return
	}
	params.SegmentContainer = segExt
	if h.cfg != nil {
		if limit := h.cfg.Snapshot().SimultaneousStreamLimit; limit > 0 {
			if _, allowed := h.transcodeMgr.TryAcquireStreamSlot(limit, transcode.SessionKey(params)); !allowed {
				httpx.Error(w, http.StatusTooManyRequests, "simultaneous stream limit reached")
				return
			}
		}
	}

	var segPath string
	if isInit {
		segPath, err = h.transcodeMgr.GetInitSegment(r.Context(), params)
	} else {
		segPath, err = h.transcodeMgr.GetSegment(r.Context(), params, segIdx)
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to transcode audio segment")
		return
	}
	f, err := os.Open(segPath)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "audio segment missing")
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		httpx.Error(w, http.StatusNotFound, "audio segment missing")
		return
	}
	ct := "audio/mp2t"
	if isInit || segExt == "mp4" || segExt == "m4s" {
		ct = "audio/mp4"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeContent(w, r, st.Name(), st.ModTime(), f)
}

func (h *Handler) resolveAudioPath(ctx context.Context, itemID, mediaSourceID string) (string, int, string, error) {
	if h.repo == nil {
		return "", 0, "", sqlite.ErrNotFound
	}
	sources, err := h.repo.GetMediaSourcesByItemID(ctx, itemID)
	if err == nil && len(sources) > 0 {
		selected := sources[0]
		normMS := guid.Normalize(mediaSourceID)
		if normMS != "" {
			for _, s := range sources {
				if guid.Normalize(s.ID) == normMS {
					selected = s
					break
				}
			}
		}
		container := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(selected.Container), "."))
		if container == "" {
			if item, iErr := h.repo.GetItemRecordByID(ctx, itemID); iErr == nil && item != nil {
				container = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(item.Container), "."))
			}
		}
		return selected.Path, selected.Bitrate, container, nil
	}
	item, err := h.repo.GetItemRecordByID(ctx, itemID)
	if err != nil {
		return "", 0, "", err
	}
	if item.Path == "" {
		return "", 0, "", sqlite.ErrNotFound
	}
	container := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(item.Container), "."))
	return item.Path, 0, container, nil
}

func serveAudioFile(w http.ResponseWriter, r *http.Request, mediaPath string) {
	f, err := os.Open(mediaPath)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			logAndWritePermissionDenied(w, mediaPath, err)
			return
		}
		if os.IsNotExist(err) {
			httpx.Error(w, http.StatusNotFound, "audio file missing on disk")
			return
		}
		slog.Error("failed to open audio file", "path", mediaPath, "error", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to open audio file")
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		slog.Error("failed to stat audio file", "path", mediaPath, "error", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to read audio file")
		return
	}
	if stat.IsDir() {
		httpx.Error(w, http.StatusNotFound, "audio file missing on disk")
		return
	}
	if ct := audioContentType(mediaPath); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	http.ServeContent(w, r, stat.Name(), stat.ModTime(), f)
}

func isContainerAllowed(srcExt string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	srcExt = strings.ToLower(strings.TrimPrefix(srcExt, "."))
	for _, a := range allowed {
		a = strings.ToLower(strings.TrimSpace(a))
		if a == "*" || a == srcExt {
			return true
		}
	}
	return false
}

func resolveAudioTranscodeTarget(container, codec string) (contentType, muxFmt, encoder string) {
	switch strings.ToLower(strings.TrimSpace(container)) {
	case "aac", "adts":
		return "audio/aac", "adts", "aac"
	case "m4a", "mp4":
		return "audio/mp4", "mp4", "aac"
	case "ogg", "opus":
		if strings.EqualFold(codec, "vorbis") {
			return "audio/ogg", "ogg", "libvorbis"
		}
		return "audio/ogg", "ogg", "libopus"
	case "flac":
		return "audio/flac", "flac", "flac"
	case "wav":
		return "audio/wav", "wav", "pcm_s16le"
	default:
		if strings.EqualFold(codec, "aac") {
			return "audio/aac", "adts", "aac"
		}
		return "audio/mpeg", "mp3", "libmp3lame"
	}
}

func audioContentType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3":
		return "audio/mpeg"
	case ".flac":
		return "audio/flac"
	case ".m4a", ".mp4", ".alac":
		return "audio/mp4"
	case ".aac":
		return "audio/aac"
	case ".ogg", ".oga":
		return "audio/ogg"
	case ".opus":
		return "audio/opus"
	case ".wav":
		return "audio/wav"
	case ".wma":
		return "audio/x-ms-wma"
	default:
		return "application/octet-stream"
	}
}

func findLRCFile(audioPath string) string {
	base := strings.TrimSuffix(audioPath, filepath.Ext(audioPath))
	for _, ext := range []string{".lrc", ".LRC"} {
		candidate := base + ext
		if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
			return candidate
		}
	}
	return ""
}

var (
	lrcTimeRe = regexp.MustCompile(`\[(\d{1,3}):(\d{2})(?:[.:](\d{1,3}))?\]`)
	lrcTagRe  = regexp.MustCompile(`^\[([a-zA-Z]+):(.*)\]$`)
)

// ParseLRC parses standard LRC lyrics text into an Emby-compatible LyricDto.
// Supports multiple timestamps per line (e.g. [00:12.00][01:20.50]Chorus), metadata tags,
// and [offset:ms] adjustment.
func ParseLRC(content string) *LyricDto {
	content = strings.TrimPrefix(content, "\ufeff")
	out := &LyricDto{
		Lyrics: []LyricLine{},
	}

	var offsetMs int64
	type rawEntry struct {
		ticks int64
		text  string
		order int
	}
	var synced []rawEntry
	var unsynced []LyricLine
	order := 0

	for _, rawLine := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}

		matches := lrcTimeRe.FindAllStringSubmatchIndex(line, -1)
		if len(matches) > 0 {
			text := strings.TrimSpace(lrcTimeRe.ReplaceAllString(line, ""))
			for _, m := range matches {
				sub := lrcTimeRe.FindStringSubmatch(line[m[0]:m[1]])
				if len(sub) < 3 {
					continue
				}
				mins, _ := strconv.ParseInt(sub[1], 10, 64)
				secs, _ := strconv.ParseInt(sub[2], 10, 64)
				var ms int64
				if len(sub) >= 4 && sub[3] != "" {
					frac := sub[3]
					switch len(frac) {
					case 1:
						v, _ := strconv.ParseInt(frac, 10, 64)
						ms = v * 100
					case 2:
						v, _ := strconv.ParseInt(frac, 10, 64)
						ms = v * 10
					default:
						v, _ := strconv.ParseInt(frac[:3], 10, 64)
						ms = v
					}
				}
				totalMs := mins*60_000 + secs*1000 + ms
				synced = append(synced, rawEntry{
					ticks: totalMs * (ticksPerSecond / 1000),
					text:  text,
					order: order,
				})
				order++
			}
			continue
		}

		// Metadata tag like [ar:Artist] or [offset:+500]
		if tag := lrcTagRe.FindStringSubmatch(line); len(tag) == 3 {
			key := strings.ToLower(strings.TrimSpace(tag[1]))
			val := strings.TrimSpace(tag[2])
			switch key {
			case "ar":
				out.Metadata.Artist = val
			case "ti":
				out.Metadata.Title = val
			case "al":
				out.Metadata.Album = val
			case "by", "au":
				out.Metadata.Author = val
			case "offset":
				if v, err := strconv.ParseInt(val, 10, 64); err == nil {
					offsetMs = v
				}
			}
			continue
		}

		unsynced = append(unsynced, LyricLine{Text: line})
	}

	if len(synced) > 0 {
		out.Metadata.IsSynced = true
		offsetTicks := offsetMs * (ticksPerSecond / 1000)
		for i := range synced {
			synced[i].ticks += offsetTicks
			if synced[i].ticks < 0 {
				synced[i].ticks = 0
			}
		}
		sort.SliceStable(synced, func(i, j int) bool {
			if synced[i].ticks == synced[j].ticks {
				return synced[i].order < synced[j].order
			}
			return synced[i].ticks < synced[j].ticks
		})
		for _, e := range synced {
			t := e.ticks
			out.Lyrics = append(out.Lyrics, LyricLine{
				Text:  e.text,
				Start: &t,
			})
		}
	} else if len(unsynced) > 0 {
		out.Lyrics = unsynced
	}

	return out
}

func (h *Handler) resolveStrmAudio(ctx context.Context, strmPath string) (string, error) {
	u, _, err := h.resolveStrmAudioFull(ctx, strmPath)
	return u, err
}

func (h *Handler) resolveStrmAudioFull(ctx context.Context, strmPath string) (string, bool, error) {
	u, _, is302, err := h.resolveStrmAudioWithHeaders(ctx, strmPath)
	return u, is302, err
}

func (h *Handler) resolveStrmAudioWithHeaders(ctx context.Context, strmPath string) (string, map[string]string, bool, error) {
	var (
		resolved string
		headers  map[string]string
		is302    bool
	)
	if h.streamResolverWithHeaders != nil {
		res, err := h.streamResolverWithHeaders(ctx, strmPath)
		if err != nil {
			return "", nil, false, err
		}
		if res != nil {
			resolved = strings.TrimSpace(res.URL)
			headers = res.Headers
			is302 = res.Is302
		}
	} else if h.streamResolverFull != nil {
		u, redirect302, err := h.streamResolverFull(ctx, strmPath)
		if err != nil {
			return "", nil, false, err
		}
		resolved = strings.TrimSpace(u)
		is302 = redirect302
	} else if h.streamResolver != nil {
		u, err := h.streamResolver(ctx, strmPath)
		if err != nil {
			return "", nil, false, err
		}
		resolved = strings.TrimSpace(u)
	} else {
		resolved = readFirstLineAudioURL(strmPath)
		if h.cfg != nil && h.cfg.Snapshot().EnableSTRM302 {
			is302 = true
		}
	}
	if resolved == "" {
		return "", nil, false, errors.New("empty or invalid strm file")
	}
	if !isHTTPURL(resolved) {
		return "", nil, false, errors.New("strm target must be an http/https url")
	}
	if err := netutil.ValidateRemoteURL(resolved, true); err != nil {
		return "", nil, false, err
	}
	return resolved, headers, is302, nil
}

func readFirstLineAudioURL(strmPath string) string {
	f, err := os.Open(strmPath)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimPrefix(sc.Text(), "\uFEFF")
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			candidate := strings.ReplaceAll(line, " ", "%20")
			if isHTTPURL(candidate) {
				return candidate
			}
			return ""
		}
	}
	return ""
}

func (h *Handler) proxyRemoteAudio(w http.ResponseWriter, r *http.Request, targetURL string) {
	h.proxyRemoteAudioWithHeaders(w, r, targetURL, nil)
}

func (h *Handler) proxyRemoteAudioWithHeaders(w http.ResponseWriter, r *http.Request, targetURL string, customHeaders map[string]string) {
	if err := netutil.ValidateRemoteURL(targetURL, true); err != nil {
		slog.Error("blocked upstream audio url", "url", targetURL, "error", err)
		httpx.Error(w, http.StatusBadGateway, "blocked upstream audio url")
		return
	}
	upReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, nil)
	if err != nil {
		slog.Error("invalid upstream audio url", "url", targetURL, "error", err)
		httpx.Error(w, http.StatusBadGateway, "invalid upstream audio url")
		return
	}
	for k, v := range customHeaders {
		key := strings.TrimSpace(k)
		val := strings.TrimSpace(v)
		if key == "" || val == "" || strings.ContainsAny(key, "\r\n\x00") || strings.ContainsAny(val, "\r\n\x00") {
			continue
		}
		switch strings.ToLower(key) {
		case "user-agent", "referer", "cookie", "authorization", "origin":
			upReq.Header.Set(key, val)
		}
	}
	if rng := r.Header.Get("Range"); rng != "" {
		upReq.Header.Set("Range", rng)
	}
	if ifRange := r.Header.Get("If-Range"); ifRange != "" {
		upReq.Header.Set("If-Range", ifRange)
	}
	if upReq.Header.Get("User-Agent") == "" {
		ua := r.Header.Get("User-Agent")
		if ua == "" {
			ua = defaultAudioBrowserUA
		}
		upReq.Header.Set("User-Agent", ua)
	}

	client := h.httpClient
	if client == nil {
		client = netutil.NewSafeHTTPClient(0, true)
	}
	resp, err := client.Do(upReq)
	if err != nil {
		slog.Error("upstream audio request failed", "url", targetURL, "error", err)
		httpx.Error(w, http.StatusBadGateway, "upstream audio error")
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
		fallbackCT = audioContentType(u.Path)
	}
	w.Header().Set("Content-Type", sanitizeProxyAudioContentType(resp.Header.Get("Content-Type"), fallbackCT))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox")
	w.WriteHeader(resp.StatusCode)
	if r.Method != http.MethodHead {
		_, _ = io.Copy(w, resp.Body)
	}
}

func sanitizeProxyAudioContentType(rawCT, fallbackCT string) string {
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
	if strings.HasPrefix(mediaType, "audio/") || strings.HasPrefix(mediaType, "video/") {
		return mediaType
	}
	switch mediaType {
	case "application/octet-stream",
		"application/vnd.apple.mpegurl",
		"application/x-mpegurl",
		"application/mp4",
		"application/ogg",
		"text/vtt":
		return mediaType
	default:
		return "application/octet-stream"
	}
}

func isHTTPURL(s string) bool {
	lower := strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

func passthrough(next http.Handler) http.Handler { return next }
