package hls

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/pkg/netutil"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

// AuthFuncs decouples route registration from the concrete middleware package.
type AuthFuncs struct {
	RequireAuth   func(http.Handler) http.Handler
	RequireAdmin  func(http.Handler) http.Handler
	UserIDFromCtx func(context.Context) string
}

const (
	maxLiveStreams        = 256
	maxLiveStreamsPerUser = 16
	liveStreamTTL         = 4 * time.Hour
	maxHLSVideoBitrate    = 120_000_000
	maxHLSAudioBitrate    = 1_536_000
	maxHLSAudioChannels   = 8
	minHLSSegmentLength   = 2
	maxHLSSegmentLength   = 15
)

type liveStreamEntry struct {
	LiveStreamID  string
	PlaySessionID string
	UserID        string
	ItemID        string
	MediaSource   dto.MediaSourceInfo
	CreatedAt     time.Time
}

// Handler serves dynamic HLS playlists/segments, encoding capabilities/defaults, and LiveStreams APIs.
type Handler struct {
	repo                      *sqlite.LibraryRepo
	userRepo                  *sqlite.UserRepo
	mgr                       *transcode.Manager
	cfg                       *config.Config
	auth                      AuthFuncs
	streamResolver            func(ctx context.Context, rawPath string) (string, error)
	streamResolverWithHeaders func(ctx context.Context, rawPath string) (string, map[string]string, error)

	liveMu      sync.RWMutex
	liveStreams map[string]*liveStreamEntry
}

// New constructs an HLS & Encoding API handler.
func New(repo *sqlite.LibraryRepo, mgr *transcode.Manager, cfg *config.Config) *Handler {
	return &Handler{
		repo:        repo,
		mgr:         mgr,
		cfg:         cfg,
		liveStreams: make(map[string]*liveStreamEntry),
	}
}

// SetUserRepo attaches a UserRepo for per-user library access control and ownership checks.
func (h *Handler) SetUserRepo(r *sqlite.UserRepo) {
	h.userRepo = r
}

// SetStreamResolver attaches a resolver (e.g. playback.Engine.ResolveStream) for .strm files.
func (h *Handler) SetStreamResolver(fn func(ctx context.Context, rawPath string) (string, error)) {
	h.streamResolver = fn
}

// SetStreamResolverWithHeaders attaches a resolver returning (targetURL, headers, err) for .strm files.
func (h *Handler) SetStreamResolverWithHeaders(fn func(ctx context.Context, rawPath string) (string, map[string]string, error)) {
	h.streamResolverWithHeaders = fn
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

func (h *Handler) isPlaylistPublic(ctx context.Context, playlistID string) bool {
	normID := guid.Normalize(playlistID)
	if normID == "" || h.userRepo == nil {
		return false
	}
	raw, err := h.userRepo.GetServerConfig(ctx, "item_visibility_"+normID)
	if err != nil || raw == "" {
		return false
	}
	var vis struct {
		IsPublic bool `json:"IsPublic"`
	}
	if err := json.Unmarshal([]byte(raw), &vis); err != nil {
		return false
	}
	return vis.IsPublic
}

func (h *Handler) canViewPlaylist(r *http.Request, playlistID, parentID string) bool {
	uid, isAdmin := h.callerIdentity(r)
	if isAdmin {
		return true
	}
	ownerID := guid.Normalize(parentID)
	if ownerID == "" || h.isPlaylistPublic(r.Context(), playlistID) {
		return true
	}
	return uid != "" && uid == ownerID
}

func (h *Handler) canAccessItem(r *http.Request, item *sqlite.ItemRecord) bool {
	if item == nil {
		return true
	}
	uid, isAdmin := h.callerIdentity(r)
	if isAdmin {
		return true
	}
	if h.userRepo == nil {
		if strings.EqualFold(item.Type, "Playlist") && uid != "" {
			if !h.canViewPlaylist(r, item.ID, item.ParentID) {
				return false
			}
		}
		if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && ac.User != nil && !ac.IsAdmin() && h.repo != nil {
			pol := ac.User.Policy
			if !strings.EqualFold(item.Type, "Playlist") && item.LibraryID != "" && !h.repo.CanUserAccessLibrary(r.Context(), pol, item.LibraryID) {
				return false
			}
			blocked, err := h.repo.IsItemBlockedByPolicy(r.Context(), item, pol)
			return err == nil && !blocked
		}
		return true
	}
	if uid == "" {
		return false
	}
	u, err := h.userRepo.GetUserByID(r.Context(), uid)
	if err != nil || u == nil {
		return false
	}
	if u.IsAdmin || u.Policy.IsAdministrator {
		return true
	}
	if strings.EqualFold(item.Type, "Playlist") {
		if !h.canViewPlaylist(r, item.ID, item.ParentID) {
			return false
		}
	} else if item.LibraryID != "" {
		if h.repo != nil {
			if !h.repo.CanUserAccessLibrary(r.Context(), u.Policy, item.LibraryID) {
				return false
			}
		} else if !u.Policy.EnableAllFolders {
			normLib := guid.Normalize(item.LibraryID)
			allowed := false
			for _, fid := range u.Policy.EnabledFolders {
				if guid.Normalize(fid) == normLib {
					allowed = true
					break
				}
			}
			if !allowed {
				return false
			}
		}
	}
	if h.repo != nil {
		blocked, err := h.repo.IsItemBlockedByPolicy(r.Context(), item, u.Policy)
		if err != nil || blocked {
			return false
		}
	}
	return true
}

func matchesUnratedType(itemType, mediaType string, blocked []string) bool {
	if strings.EqualFold(itemType, "CollectionFolder") || strings.EqualFold(itemType, "Folder") || strings.EqualFold(itemType, "UserView") {
		return false
	}
	for _, b := range blocked {
		t := strings.TrimSpace(b)
		if t == "" {
			continue
		}
		if t == "*" || strings.EqualFold(t, "All") || strings.EqualFold(t, itemType) || (mediaType != "" && strings.EqualFold(t, mediaType)) {
			return true
		}
		if strings.EqualFold(t, "Series") && (strings.EqualFold(itemType, "Season") || strings.EqualFold(itemType, "Episode")) {
			return true
		}
		if strings.EqualFold(t, "Music") && (strings.EqualFold(itemType, "Audio") || strings.EqualFold(itemType, "MusicAlbum") || strings.EqualFold(mediaType, "Audio")) {
			return true
		}
	}
	return false
}

func (h *Handler) callerPolicy(r *http.Request) (*dto.UserPolicy, bool) {
	uid, isAdmin := h.callerIdentity(r)
	if isAdmin {
		return nil, true
	}
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && ac.IsAPIKey {
		return nil, true
	}
	if h.userRepo != nil && uid != "" {
		if u, err := h.userRepo.GetUserByID(r.Context(), uid); err == nil && u != nil {
			if u.IsAdmin || u.Policy.IsAdministrator {
				return nil, true
			}
			pol := u.Policy
			return &pol, false
		}
	}
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && ac.User != nil {
		if ac.IsAdmin() {
			return nil, true
		}
		pol := ac.User.Policy
		return &pol, false
	}
	return nil, false
}

func (h *Handler) canPlayMedia(r *http.Request) bool {
	pol, isAdmin := h.callerPolicy(r)
	if isAdmin || pol == nil {
		return true
	}
	return pol.EnableMediaPlayback
}

func (h *Handler) authorizeAndAcquireHLS(w http.ResponseWriter, r *http.Request, item *sqlite.ItemRecord, params transcode.SessionParams) (func(), bool) {
	noop := func() {}
	if pol, isAdmin := h.callerPolicy(r); !isAdmin && pol != nil {
		if !pol.EnableMediaPlayback {
			httpx.Error(w, http.StatusForbidden, "media playback is disabled for this user")
			return nil, false
		}
		isAudio := item != nil && (strings.EqualFold(item.Type, "Audio") || strings.EqualFold(item.MediaType, "Audio"))
		if isAudio {
			if !pol.EnableAudioPlaybackTranscoding {
				httpx.Error(w, http.StatusForbidden, "audio playback transcoding is disabled for this user")
				return nil, false
			}
		} else {
			isRemuxOnly := strings.EqualFold(strings.TrimSpace(params.VideoCodec), "copy") &&
				!strings.EqualFold(strings.TrimSpace(params.SubtitleMethod), "Encode")
			if isRemuxOnly {
				if !pol.EnablePlaybackRemuxing && !pol.EnableVideoPlaybackTranscoding {
					httpx.Error(w, http.StatusForbidden, "playback remuxing and video transcoding are disabled for this user")
					return nil, false
				}
			} else if !pol.EnableVideoPlaybackTranscoding {
				httpx.Error(w, http.StatusForbidden, "video playback transcoding is disabled for this user")
				return nil, false
			}
		}
	}
	if r.Method != http.MethodHead && h.cfg != nil && h.mgr != nil {
		if limit := h.cfg.Snapshot().SimultaneousStreamLimit; limit > 0 {
			release, ok := h.mgr.TryAcquireStreamSlot(limit, transcode.SessionKey(params))
			if !ok {
				httpx.Error(w, http.StatusTooManyRequests, "simultaneous stream limit reached")
				return nil, false
			}
			return release, true
		}
	}
	return noop, true
}

func isRemoteClientRequest(r *http.Request, cfg *config.Config) bool {
	if netutil.IsLocalOrLANRequest(r) {
		return false
	}
	if cfg != nil {
		subnets := cfg.Snapshot().LocalNetworkSubnets
		if len(subnets) > 0 {
			if ip, err := netip.ParseAddr(netutil.ClientIP(r)); err == nil {
				ip = ip.Unmap()
				for _, raw := range subnets {
					s := strings.TrimSpace(raw)
					if pfx, pErr := netip.ParsePrefix(s); pErr == nil && pfx.Contains(ip) {
						return false
					}
					if a, aErr := netip.ParseAddr(s); aErr == nil && a.Unmap() == ip {
						return false
					}
				}
			}
		}
	}
	return true
}

func (h *Handler) resolveDeviceID(r *http.Request, q httpx.Query) string {
	queryDev := strings.TrimSpace(q.Get("DeviceId"))
	_, isAdmin := h.callerIdentity(r)
	if isAdmin && queryDev != "" {
		return queryDev
	}
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil {
		if boundDev := strings.TrimSpace(ac.Client.DeviceID); boundDev != "" {
			return boundDev
		}
	}
	return queryDev
}

func scopePlaySessionID(playSessionID, callerUserID string, isAdmin bool) string {
	playSessionID = strings.TrimSpace(playSessionID)
	if playSessionID == "" || isAdmin || callerUserID == "" {
		return playSessionID
	}
	prefix := callerUserID + ":"
	if strings.HasPrefix(playSessionID, prefix) {
		return playSessionID
	}
	return prefix + playSessionID
}

// RegisterRoutes mounts all HLS, ActiveEncodings, Encoding, and LiveStreams endpoints on mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth AuthFuncs) {
	h.auth = auth
	wrapAuth := auth.RequireAuth
	if wrapAuth == nil {
		wrapAuth = passthrough
	}
	wrapAdmin := auth.RequireAdmin
	if wrapAdmin == nil {
		wrapAdmin = wrapAuth
	}

	// Dynamic HLS Playlists & Segments (Go 1.22+ "GET" matches both GET and HEAD)
	mux.Handle("GET /Videos/{Id}/master.m3u8", wrapAuth(http.HandlerFunc(h.handleMasterPlaylist)))
	mux.Handle("GET /Videos/{Id}/main.m3u8", wrapAuth(http.HandlerFunc(h.handleMediaPlaylist)))
	mux.Handle("GET /Videos/{Id}/live.m3u8", wrapAuth(http.HandlerFunc(h.handleMediaPlaylist)))
	mux.Handle("GET /Videos/{Id}/hls1/{PlaylistId}/init.mp4", wrapAuth(http.HandlerFunc(h.handleInitSegment)))
	mux.Handle("GET /Videos/{Id}/hls/{PlaylistId}/init.mp4", wrapAuth(http.HandlerFunc(h.handleInitSegment)))
	mux.Handle("GET /Videos/{Id}/init.mp4", wrapAuth(http.HandlerFunc(h.handleInitSegment)))
	mux.Handle("GET /Videos/{Id}/hls1/{PlaylistId}/{Segment}", wrapAuth(http.HandlerFunc(h.handleSegment)))
	mux.Handle("GET /Videos/{Id}/hls/{PlaylistId}/{Segment}", wrapAuth(http.HandlerFunc(h.handleSegment)))
	mux.Handle("GET /Videos/{Id}/{MediaSourceId}/Subtitles/{Index}/subtitles.m3u8", wrapAuth(http.HandlerFunc(h.handleSubtitlePlaylist)))

	// Active Encodings Management
	mux.Handle("DELETE /Videos/ActiveEncodings", wrapAuth(http.HandlerFunc(h.handleDeleteActiveEncodings)))

	// Encoding Configuration & Hardware Codec Info
	mux.Handle("GET /Encoding/CodecInformation/Video", wrapAdmin(http.HandlerFunc(h.handleCodecInfoVideo)))
	mux.Handle("GET /Encoding/CodecConfiguration/Defaults", wrapAdmin(http.HandlerFunc(h.handleCodecConfigDefaults)))
	mux.Handle("POST /Encoding/FullSubtitleDeliveryEncode", wrapAuth(http.HandlerFunc(h.handleFullSubtitleDeliveryEncode)))

	// LiveStreams
	mux.Handle("POST /LiveStreams/Open", wrapAuth(http.HandlerFunc(h.handleLiveStreamOpen)))
	mux.Handle("POST /LiveStreams/Close", wrapAuth(http.HandlerFunc(h.handleLiveStreamClose)))
	mux.Handle("POST /LiveStreams/MediaInfo", wrapAuth(http.HandlerFunc(h.handleLiveStreamMediaInfo)))
}

func (h *Handler) handleMasterPlaylist(w http.ResponseWriter, r *http.Request) {
	itemID := guid.Normalize(r.PathValue("Id"))
	q := httpx.ParseQuery(r)

	item, src, err := h.resolveItemAndSource(r.Context(), itemID, q.Get("MediaSourceId"))
	if errors.Is(err, sqlite.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "media item not found")
		return
	}
	if err != nil {
		slog.Error("failed to resolve media item for master playlist", "item_id", itemID, "error", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to resolve media item")
		return
	}
	if !h.canAccessItem(r, item) {
		httpx.Error(w, http.StatusForbidden, "access denied")
		return
	}

	params, strmErr := h.buildSessionParamsChecked(r, item, src, q)
	if strmErr != nil {
		slog.Error("failed to resolve hls stream source", "item_id", item.ID, "error", strmErr)
		httpx.Error(w, http.StatusBadGateway, "failed to resolve stream url")
		return
	}
	releaseSlot, ok := h.authorizeAndAcquireHLS(w, r, item, params)
	if !ok {
		return
	}
	defer releaseSlot()

	width, height := extractVideoDimensions(src)
	if params.MaxWidth > 0 && (width == 0 || params.MaxWidth < width) {
		width = params.MaxWidth
	}
	if params.MaxHeight > 0 && (height == 0 || params.MaxHeight < height) {
		height = params.MaxHeight
	}
	bw := params.VideoBitrate + params.AudioBitrate
	if bw <= 0 {
		bw = 4_000_000
	}

	token := resolveRequestToken(r, q)
	m3u8 := transcode.BuildMasterPlaylist(transcode.PlaylistParams{
		ItemID:           item.ID,
		DurationTicks:    resolveRuntimeTicks(item, src),
		SegmentDuration:  params.SegmentDuration,
		SegmentContainer: params.SegmentContainer,
		Bandwidth:        bw,
		Width:            width,
		Height:           height,
		VideoCodec:       params.VideoCodec,
		AudioCodec:       params.AudioCodec,
		Query:            preserveQueryWithToken(r.URL.Query(), token),
		Subtitles:        extractTextSubtitles(item, src, token),
	})

	w.Header().Set("Content-Type", "application/x-mpegURL; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write([]byte(m3u8))
	}
}

func (h *Handler) handleMediaPlaylist(w http.ResponseWriter, r *http.Request) {
	itemID := guid.Normalize(r.PathValue("Id"))
	q := httpx.ParseQuery(r)

	item, src, err := h.resolveItemAndSource(r.Context(), itemID, q.Get("MediaSourceId"))
	if errors.Is(err, sqlite.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "media item not found")
		return
	}
	if err != nil {
		slog.Error("failed to resolve media item for master playlist", "item_id", itemID, "error", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to resolve media item")
		return
	}
	if !h.canAccessItem(r, item) {
		httpx.Error(w, http.StatusForbidden, "access denied")
		return
	}

	params, strmErr := h.buildSessionParamsChecked(r, item, src, q)
	if strmErr != nil {
		slog.Error("failed to resolve hls stream source", "item_id", item.ID, "error", strmErr)
		httpx.Error(w, http.StatusBadGateway, "failed to resolve stream url")
		return
	}
	releaseSlot, ok := h.authorizeAndAcquireHLS(w, r, item, params)
	if !ok {
		return
	}
	defer releaseSlot()
	if r.Method != http.MethodHead && h.mgr != nil {
		_, _ = h.mgr.RegisterSession(params)
	}

	token := resolveRequestToken(r, q)
	m3u8 := transcode.BuildMediaPlaylist(transcode.PlaylistParams{
		ItemID:           item.ID,
		PlaylistID:       "main",
		DurationTicks:    resolveRuntimeTicks(item, src),
		SegmentDuration:  params.SegmentDuration,
		SegmentContainer: params.SegmentContainer,
		Query:            preserveQueryWithToken(r.URL.Query(), token),
	})

	w.Header().Set("Content-Type", "application/x-mpegURL; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write([]byte(m3u8))
	}
}

func (h *Handler) handleSubtitlePlaylist(w http.ResponseWriter, r *http.Request) {
	if !h.canPlayMedia(r) {
		httpx.Error(w, http.StatusForbidden, "media playback is disabled for this user")
		return
	}
	itemID := guid.Normalize(r.PathValue("Id"))
	msID := guid.Normalize(r.PathValue("MediaSourceId"))
	idx, err := strconv.Atoi(r.PathValue("Index"))
	if err != nil || idx < 0 {
		httpx.Error(w, http.StatusBadRequest, "invalid subtitle index")
		return
	}

	item, src, err := h.resolveItemAndSource(r.Context(), itemID, msID)
	if errors.Is(err, sqlite.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "media item not found")
		return
	}
	if err != nil {
		slog.Error("failed to resolve media item for subtitle playlist", "item_id", itemID, "error", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to resolve media item")
		return
	}
	if !h.canAccessItem(r, item) {
		httpx.Error(w, http.StatusForbidden, "access denied")
		return
	}

	if !hasTextSubtitleStream(src, idx) {
		httpx.Error(w, http.StatusNotFound, "subtitle stream not found")
		return
	}

	vttURI := "Stream.vtt"
	q := httpx.ParseQuery(r)
	addMap := true
	if b := q.Bool("addVttTimeMap"); b != nil {
		addMap = *b
	}
	token := resolveRequestToken(r, q)
	switch {
	case token != "" && addMap:
		vttURI += "?api_key=" + url.QueryEscape(token) + "&addVttTimeMap=true"
	case token != "":
		vttURI += "?api_key=" + url.QueryEscape(token)
	case addMap:
		vttURI += "?addVttTimeMap=true"
	}

	durSec := float64(resolveRuntimeTicks(item, src)) / float64(transcode.TicksPerSecond)
	m3u8 := transcode.BuildSubtitlePlaylist(durSec, vttURI)

	w.Header().Set("Content-Type", "application/x-mpegURL; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write([]byte(m3u8))
	}
}

func hasTextSubtitleStream(src *sqlite.MediaSourceRecord, idx int) bool {
	if src == nil {
		return false
	}
	for _, st := range src.Streams {
		if st.StreamIndex == idx && strings.EqualFold(st.StreamType, "Subtitle") && isTextSubtitleCodec(st.Codec) {
			return true
		}
	}
	return false
}

func extractTextSubtitles(item *sqlite.ItemRecord, src *sqlite.MediaSourceRecord, apiKey string) []transcode.SubtitleTrackInfo {
	if item == nil || src == nil || len(src.Streams) == 0 {
		return nil
	}
	msID := src.ID
	if msID == "" {
		msID = item.ID
	}
	var out []transcode.SubtitleTrackInfo
	for _, st := range src.Streams {
		if !strings.EqualFold(st.StreamType, "Subtitle") || !isTextSubtitleCodec(st.Codec) {
			continue
		}
		uri := fmt.Sprintf("%s/Subtitles/%d/subtitles.m3u8", msID, st.StreamIndex)
		if apiKey != "" {
			uri += "?api_key=" + url.QueryEscape(apiKey)
		}
		title := strings.TrimSpace(st.DisplayTitle)
		if title == "" {
			title = strings.TrimSpace(st.Title)
		}
		if title == "" {
			title = strings.TrimSpace(st.Language)
		}
		out = append(out, transcode.SubtitleTrackInfo{
			Index:     st.StreamIndex,
			Language:  st.Language,
			Title:     title,
			IsDefault: st.IsDefault,
			IsForced:  st.IsForced,
			URI:       uri,
		})
	}
	return out
}

func isTextSubtitleCodec(codec string) bool {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "vtt", "webvtt", "srt", "subrip", "ass", "ssa", "mov_text", "tx3g":
		return true
	default:
		return false
	}
}

func (h *Handler) handleSegment(w http.ResponseWriter, r *http.Request) {
	if h.mgr == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "transcode manager unavailable")
		return
	}
	itemID := guid.Normalize(r.PathValue("Id"))
	segRaw := r.PathValue("Segment")
	segIdx, ext, err := transcode.ParseSegmentID(segRaw)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid segment id")
		return
	}
	switch ext {
	case "ts", "mp4", "m4s":
	default:
		httpx.Error(w, http.StatusBadRequest, "unsupported segment container: "+ext)
		return
	}

	q := httpx.ParseQuery(r)
	item, src, err := h.resolveItemAndSource(r.Context(), itemID, q.Get("MediaSourceId"))
	if errors.Is(err, sqlite.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "media item not found")
		return
	}
	if err != nil {
		slog.Error("failed to resolve media item for master playlist", "item_id", itemID, "error", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to resolve media item")
		return
	}
	if !h.canAccessItem(r, item) {
		httpx.Error(w, http.StatusForbidden, "access denied")
		return
	}

	params, strmErr := h.buildSessionParamsChecked(r, item, src, q)
	if strmErr != nil {
		slog.Error("failed to resolve hls stream source", "item_id", item.ID, "error", strmErr)
		httpx.Error(w, http.StatusBadGateway, "failed to resolve stream url")
		return
	}
	releaseSlot, ok := h.authorizeAndAcquireHLS(w, r, item, params)
	if !ok {
		return
	}
	defer releaseSlot()
	if r.Method == http.MethodHead {
		switch ext {
		case "mp4", "m4s":
			w.Header().Set("Content-Type", "video/mp4")
		default:
			w.Header().Set("Content-Type", "video/mp2t")
		}
		w.Header().Set("Cache-Control", "private, max-age=3600")
		w.WriteHeader(http.StatusOK)
		return
	}
	params.SegmentContainer = ext

	segPath, err := h.mgr.GetSegment(r.Context(), params, segIdx)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		if errors.Is(err, os.ErrPermission) {
			logAndWritePermissionDenied(w, params.InputPath, err)
			return
		}
		slog.Error("failed to generate hls segment", "item_id", itemID, "segment", segIdx, "error", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to generate hls segment")
		return
	}

	switch ext {
	case "mp4", "m4s":
		w.Header().Set("Content-Type", "video/mp4")
	default:
		w.Header().Set("Content-Type", "video/mp2t")
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	serveHLSFile(w, r, segPath)
}

func (h *Handler) handleInitSegment(w http.ResponseWriter, r *http.Request) {
	if h.mgr == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "transcode manager unavailable")
		return
	}
	itemID := guid.Normalize(r.PathValue("Id"))
	q := httpx.ParseQuery(r)
	item, src, err := h.resolveItemAndSource(r.Context(), itemID, q.Get("MediaSourceId"))
	if errors.Is(err, sqlite.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "media item not found")
		return
	}
	if err != nil {
		slog.Error("failed to resolve media item for init segment", "item_id", itemID, "error", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to resolve media item")
		return
	}
	if !h.canAccessItem(r, item) {
		httpx.Error(w, http.StatusForbidden, "access denied")
		return
	}

	params, strmErr := h.buildSessionParamsChecked(r, item, src, q)
	if strmErr != nil {
		slog.Error("failed to resolve hls stream source", "item_id", item.ID, "error", strmErr)
		httpx.Error(w, http.StatusBadGateway, "failed to resolve stream url")
		return
	}
	releaseSlot, ok := h.authorizeAndAcquireHLS(w, r, item, params)
	if !ok {
		return
	}
	defer releaseSlot()
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Cache-Control", "private, max-age=3600")
		w.WriteHeader(http.StatusOK)
		return
	}
	if sc := strings.ToLower(strings.TrimSpace(params.SegmentContainer)); sc != "mp4" && sc != "m4s" {
		params.SegmentContainer = "m4s"
	}

	initPath, err := h.mgr.GetInitSegment(r.Context(), params)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		if errors.Is(err, os.ErrPermission) {
			logAndWritePermissionDenied(w, params.InputPath, err)
			return
		}
		slog.Error("failed to generate hls init segment", "item_id", itemID, "error", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to generate hls init segment")
		return
	}

	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	serveHLSFile(w, r, initPath)
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

func serveHLSFile(w http.ResponseWriter, r *http.Request, filePath string) {
	f, err := os.Open(filePath)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			logAndWritePermissionDenied(w, filePath, err)
			return
		}
		if os.IsNotExist(err) {
			httpx.Error(w, http.StatusNotFound, "hls segment not found")
			return
		}
		slog.Error("failed to open hls segment", "path", filePath, "error", err)
		httpx.Error(w, http.StatusInternalServerError, "failed to read hls segment")
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || stat.IsDir() {
		httpx.Error(w, http.StatusNotFound, "hls segment not found")
		return
	}
	http.ServeContent(w, r, stat.Name(), stat.ModTime(), f)
}

func (h *Handler) handleDeleteActiveEncodings(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	callerUserID, isAdmin := h.callerIdentity(r)
	rawPlaySessionID := strings.TrimSpace(q.Get("PlaySessionId"))
	rawDeviceID := strings.TrimSpace(q.Get("DeviceId"))
	deviceID := ""
	if rawPlaySessionID == "" || rawDeviceID != "" {
		deviceID = h.resolveDeviceID(r, q)
	}
	playSessionID := scopePlaySessionID(rawPlaySessionID, callerUserID, isAdmin)
	if h.mgr != nil {
		h.mgr.StopSessionsScoped(callerUserID, playSessionID, deviceID, isAdmin)
		if isAdmin && rawPlaySessionID != "" {
			suffix := ":" + rawPlaySessionID
			for _, snap := range h.mgr.ActiveSessions() {
				if strings.HasSuffix(snap.PlaySessionID, suffix) {
					h.mgr.StopSessionsScoped(callerUserID, snap.PlaySessionID, "", true)
				}
			}
		}
	}
	httpx.NoContent(w)
}

func (h *Handler) handleCodecInfoVideo(w http.ResponseWriter, r *http.Request) {
	var codecs []transcode.CodecInfo
	if h.mgr != nil {
		caps := h.mgr.Capabilities(r.Context())
		if caps != nil {
			codecs = caps.Codecs
		}
	}
	if codecs == nil {
		codecs = []transcode.CodecInfo{}
	}
	httpx.JSON(w, http.StatusOK, codecs)
}

// CodecConfigurationDefault represents an Emby /Encoding/CodecConfiguration/Defaults entry.
type CodecConfigurationDefault struct {
	CodecId            string `json:"CodecId"`
	IsEnabled          bool   `json:"IsEnabled"`
	Priority           int    `json:"Priority"`
	VideoMediaType     string `json:"VideoMediaType"`
	HardwareAccelType  string `json:"HardwareAccelType,omitempty"`
	ToneMappingMode    string `json:"ToneMappingMode,omitempty"`
	AllowHardwareAccel bool   `json:"AllowHardwareAccel"`
}

func (h *Handler) handleCodecConfigDefaults(w http.ResponseWriter, r *http.Request) {
	var defaults []CodecConfigurationDefault
	if h.mgr != nil {
		caps := h.mgr.Capabilities(r.Context())
		if caps != nil {
			for i, c := range caps.Codecs {
				defaults = append(defaults, CodecConfigurationDefault{
					CodecId:            c.ShortName,
					IsEnabled:          c.IsEnabledByDefault,
					Priority:           i + 1,
					VideoMediaType:     c.VideoMediaType,
					HardwareAccelType:  c.SecondaryFramework,
					ToneMappingMode:    "hable",
					AllowHardwareAccel: c.IsHardwareCodec,
				})
			}
		}
	}
	if defaults == nil {
		defaults = []CodecConfigurationDefault{}
	}
	httpx.JSON(w, http.StatusOK, defaults)
}

func (h *Handler) handleFullSubtitleDeliveryEncode(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]any{
		"Status":  "Accepted",
		"Message": "Subtitle delivery encoding preference acknowledged",
	})
}

type liveStreamOpenRequest struct {
	OpenToken           string `json:"OpenToken"`
	UserId              string `json:"UserId"`
	PlaySessionId       string `json:"PlaySessionId"`
	MaxStreamingBitrate int    `json:"MaxStreamingBitrate"`
	StartTimeTicks      int64  `json:"StartTimeTicks"`
	AudioStreamIndex    *int   `json:"AudioStreamIndex"`
	SubtitleStreamIndex *int   `json:"SubtitleStreamIndex"`
	ItemId              string `json:"ItemId"`
	MediaSourceId       string `json:"MediaSourceId"`
	DirectPlayProtocols []string `json:"DirectPlayProtocols"`
}

type liveStreamOpenResponse struct {
	MediaSource dto.MediaSourceInfo `json:"MediaSource"`
}

func (h *Handler) handleLiveStreamOpen(w http.ResponseWriter, r *http.Request) {
	if !h.canPlayMedia(r) {
		httpx.Error(w, http.StatusForbidden, "media playback is disabled for this user")
		return
	}
	q := httpx.ParseQuery(r)
	var req liveStreamOpenRequest
	if r.Body != nil {
		_ = httpx.DecodeJSONLimit(r, &req, 0)
	}
	if req.ItemId == "" {
		req.ItemId = q.Get("ItemId")
	}
	if req.MediaSourceId == "" {
		req.MediaSourceId = q.Get("MediaSourceId")
	}
	if req.PlaySessionId == "" {
		req.PlaySessionId = q.Get("PlaySessionId")
	}
	if req.PlaySessionId == "" {
		req.PlaySessionId = guid.New()
	}

	liveID := guid.New()
	var msDTO dto.MediaSourceInfo
	callerUserID, isAdmin := h.callerIdentity(r)
	scopedPlaySessionID := scopePlaySessionID(req.PlaySessionId, callerUserID, isAdmin)

	if req.ItemId != "" && h.repo != nil {
		item, src, err := h.resolveItemAndSource(r.Context(), guid.Normalize(req.ItemId), req.MediaSourceId)
		if errors.Is(err, sqlite.ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "media item not found")
			return
		}
		if err != nil {
			slog.Error("failed to resolve live stream item", "item_id", req.ItemId, "error", err)
			httpx.Error(w, http.StatusInternalServerError, "failed to resolve media item")
			return
		}
		if item != nil && !h.canAccessItem(r, item) {
			httpx.Error(w, http.StatusForbidden, "access denied")
			return
		}
		if src != nil {
			msDTO = sqlite.MediaSourceRecordsToDTO([]sqlite.MediaSourceRecord{*src})[0]
			msDTO.Type = "Default"
			if msDTO.Id == "" {
				msDTO.Id = item.ID
			}
		} else if item != nil {
			msDTO = dto.MediaSourceInfo{
				Id:                   item.ID,
				Name:                 item.Name,
				Path:                 item.Path,
				Protocol:             "File",
				Type:                 "Default",
				Container:            item.Container,
				SupportsDirectPlay:   true,
				SupportsDirectStream: true,
				SupportsTranscoding:  true,
				MediaStreams:         []dto.MediaStream{},
			}
		}
	}

	if msDTO.Id == "" {
		msDTO = dto.MediaSourceInfo{
			Id:                   liveID,
			Name:                 "LiveStream",
			Protocol:             "Http",
			Type:                 "Default",
			IsInfiniteStream:     true,
			RequiresOpening:      true,
			RequiresClosing:      true,
			SupportsDirectPlay:   true,
			SupportsDirectStream: true,
			SupportsTranscoding:  true,
			MediaStreams:         []dto.MediaStream{},
		}
	}

	now := time.Now().UTC()
	h.liveMu.Lock()
	h.pruneLiveStreamsLocked(now)
	if !isAdmin && callerUserID != "" {
		userCount := 0
		for _, existing := range h.liveStreams {
			if existing != nil && existing.UserID == callerUserID {
				userCount++
			}
		}
		if userCount >= maxLiveStreamsPerUser {
			h.liveMu.Unlock()
			httpx.Error(w, http.StatusTooManyRequests, "maximum number of active live streams per user reached")
			return
		}
	}
	h.liveStreams[liveID] = &liveStreamEntry{
		LiveStreamID:  liveID,
		PlaySessionID: scopedPlaySessionID,
		UserID:        callerUserID,
		ItemID:        guid.Normalize(req.ItemId),
		MediaSource:   msDTO,
		CreatedAt:     now,
	}
	h.liveMu.Unlock()

	respMS := h.sanitizeMediaSourceForCaller(r, msDTO)
	httpx.JSON(w, http.StatusOK, map[string]any{
		"LiveStreamId": liveID,
		"MediaSource":  respMS,
	})
}

func (h *Handler) sanitizeMediaSourceForCaller(r *http.Request, ms dto.MediaSourceInfo) dto.MediaSourceInfo {
	uid, isAdmin := h.callerIdentity(r)
	hasAuth := uid != "" || isAdmin
	if !hasAuth {
		if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && (ac.User != nil || ac.IsAPIKey) {
			hasAuth = true
		}
	}
	if !hasAuth || isAdmin {
		return ms
	}
	if len(ms.MediaStreams) > 0 {
		ms.MediaStreams = append([]dto.MediaStream(nil), ms.MediaStreams...)
	}
	sources := []dto.MediaSourceInfo{ms}
	dto.SanitizeMediaSources(sources)
	return sources[0]
}

func (h *Handler) pruneLiveStreamsLocked(now time.Time) {
	cutoff := now.Add(-liveStreamTTL)
	for id, entry := range h.liveStreams {
		if entry == nil || entry.CreatedAt.Before(cutoff) {
			delete(h.liveStreams, id)
		}
	}
	for len(h.liveStreams) >= maxLiveStreams {
		var oldestID string
		var oldestTime time.Time
		for id, entry := range h.liveStreams {
			if oldestID == "" || entry.CreatedAt.Before(oldestTime) {
				oldestID = id
				oldestTime = entry.CreatedAt
			}
		}
		if oldestID == "" {
			break
		}
		delete(h.liveStreams, oldestID)
	}
}

type liveStreamCloseRequest struct {
	LiveStreamId string `json:"LiveStreamId"`
}

func (h *Handler) handleLiveStreamClose(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	liveID := q.Get("LiveStreamId")
	if liveID == "" && r.Body != nil {
		var req liveStreamCloseRequest
		_ = httpx.DecodeJSONLimit(r, &req, 0)
		liveID = req.LiveStreamId
	}

	callerUserID, isAdmin := h.callerIdentity(r)
	var playSessionID string
	if liveID != "" {
		h.liveMu.Lock()
		if entry, ok := h.liveStreams[liveID]; ok {
			if !isAdmin && (h.userRepo != nil || callerUserID != "" || entry.UserID != "") && (entry.UserID == "" || entry.UserID != callerUserID) {
				h.liveMu.Unlock()
				httpx.Error(w, http.StatusForbidden, "access denied")
				return
			}
			playSessionID = entry.PlaySessionID
			delete(h.liveStreams, liveID)
		}
		h.liveMu.Unlock()
	}
	if h.mgr != nil && playSessionID != "" {
		h.mgr.StopSessionsScoped(callerUserID, playSessionID, "", isAdmin)
	}
	httpx.NoContent(w)
}

func (h *Handler) handleLiveStreamMediaInfo(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	liveID := q.Get("LiveStreamId")
	if liveID == "" && r.Body != nil {
		var req liveStreamCloseRequest
		_ = httpx.DecodeJSONLimit(r, &req, 0)
		liveID = req.LiveStreamId
	}

	h.liveMu.RLock()
	entry, ok := h.liveStreams[liveID]
	h.liveMu.RUnlock()
	if !ok {
		httpx.Error(w, http.StatusNotFound, "live stream not found")
		return
	}
	callerUserID, isAdmin := h.callerIdentity(r)
	if !isAdmin && (h.userRepo != nil || callerUserID != "" || entry.UserID != "") && (entry.UserID == "" || entry.UserID != callerUserID) {
		httpx.Error(w, http.StatusForbidden, "access denied")
		return
	}
	httpx.JSON(w, http.StatusOK, liveStreamOpenResponse{MediaSource: h.sanitizeMediaSourceForCaller(r, entry.MediaSource)})
}

func (h *Handler) resolveItemAndSource(ctx context.Context, itemID, mediaSourceID string) (*sqlite.ItemRecord, *sqlite.MediaSourceRecord, error) {
	if h.repo == nil {
		return nil, nil, sqlite.ErrNotFound
	}
	item, err := h.repo.GetItemRecordByID(ctx, itemID)
	if err != nil {
		return nil, nil, err
	}
	sources, err := h.repo.GetMediaSourcesByItemID(ctx, itemID)
	if err != nil {
		return item, nil, err
	}
	if len(sources) > 0 {
		normMS := guid.Normalize(mediaSourceID)
		if normMS != "" {
			for i := range sources {
				if guid.Normalize(sources[i].ID) == normMS {
					return item, &sources[i], nil
				}
			}
		}
		return item, &sources[0], nil
	}
	return item, nil, nil
}

func (h *Handler) buildSessionParams(r *http.Request, item *sqlite.ItemRecord, src *sqlite.MediaSourceRecord, q httpx.Query) transcode.SessionParams {
	params, _ := h.buildSessionParamsChecked(r, item, src, q)
	return params
}

func (h *Handler) buildSessionParamsChecked(r *http.Request, item *sqlite.ItemRecord, src *sqlite.MediaSourceRecord, q httpx.Query) (transcode.SessionParams, error) {
	inputPath := item.Path
	msID := item.ID
	if src != nil {
		if src.Path != "" {
			inputPath = src.Path
		}
		if src.ID != "" {
			msID = src.ID
		}
	}
	var strmErr error
	var inputHeaders map[string]string
	// Resolve .strm file to its target URL for FFmpeg input
	if strings.EqualFold(filepath.Ext(inputPath), ".strm") {
		var resolved string
		if h.streamResolverWithHeaders != nil {
			u, hdrs, err := h.streamResolverWithHeaders(r.Context(), inputPath)
			if err != nil {
				strmErr = err
			} else {
				resolved = strings.TrimSpace(u)
				inputHeaders = hdrs
			}
		} else if h.streamResolver != nil {
			u, err := h.streamResolver(r.Context(), inputPath)
			if err != nil {
				strmErr = err
			} else {
				resolved = strings.TrimSpace(u)
			}
		} else {
			resolved = readFirstLineURL(inputPath)
		}
		if strmErr == nil && resolved != "" && isSafeStrmURL(resolved) {
			inputPath = resolved
		} else {
			inputPath = ""
			if strmErr == nil {
				strmErr = errors.New("invalid or blocked strm target url")
			}
		}
	} else if isHTTPURL(inputPath) && !isSafeStrmURL(inputPath) {
		inputPath = ""
		strmErr = errors.New("blocked upstream stream url")
	}

	segDur := q.Int("SegmentLength", transcode.DefaultSegmentDuration)
	if segDur <= 0 {
		segDur = transcode.DefaultSegmentDuration
	}
	if segDur < minHLSSegmentLength {
		segDur = minHLSSegmentLength
	}
	if segDur > maxHLSSegmentLength {
		segDur = maxHLSSegmentLength
	}

	audioBitrate := q.Int("AudioBitrate", 192_000)
	if audioBitrate <= 0 {
		audioBitrate = 192_000
	}
	if audioBitrate > maxHLSAudioBitrate {
		audioBitrate = maxHLSAudioBitrate
	}
	maxStreamingBitrate := q.Int("MaxStreamingBitrate", 0)
	if h.cfg != nil {
		if remoteLimit := h.cfg.Snapshot().RemoteClientBitrateLimit; remoteLimit > 0 && isRemoteClientRequest(r, h.cfg) {
			if maxStreamingBitrate <= 0 || maxStreamingBitrate > remoteLimit {
				maxStreamingBitrate = remoteLimit
			}
		}
	}
	videoBitrate := q.Int("VideoBitrate", 0)
	if videoBitrate <= 0 && maxStreamingBitrate > 0 {
		videoBitrate = maxStreamingBitrate - audioBitrate
		if videoBitrate < 256_000 {
			videoBitrate = 256_000
		}
	} else if maxStreamingBitrate > 0 && videoBitrate+audioBitrate > maxStreamingBitrate {
		capped := maxStreamingBitrate - audioBitrate
		if capped < 256_000 {
			capped = 256_000
		}
		if capped < videoBitrate {
			videoBitrate = capped
		}
	}
	if videoBitrate > maxHLSVideoBitrate {
		videoBitrate = maxHLSVideoBitrate
	}

	audioCodec := sanitizeAudioCodec(q.Get("AudioCodec"))
	audioChannels := q.Int("TranscodingMaxAudioChannels", 2)
	if audioChannels <= 0 {
		audioChannels = 2
	}
	if audioChannels > maxHLSAudioChannels {
		audioChannels = maxHLSAudioChannels
	}

	var audioIdx *int
	if q.Has("AudioStreamIndex") {
		v := q.Int("AudioStreamIndex", -1)
		if v >= 0 {
			audioIdx = &v
		}
	}

	var subIdx *int
	if q.Has("SubtitleStreamIndex") {
		v := q.Int("SubtitleStreamIndex", -1)
		if v >= 0 {
			subIdx = &v
		}
	}
	subMethod := strings.TrimSpace(q.Get("SubtitleMethod"))
	var subCodec, subPath string
	subRelIdx := 0
	if subIdx != nil && src != nil {
		embeddedSubIdx := 0
		for _, st := range src.Streams {
			if !strings.EqualFold(st.StreamType, "Subtitle") {
				continue
			}
			if st.StreamIndex == *subIdx {
				subCodec = st.Codec
				if st.IsExternal && strings.TrimSpace(st.Path) != "" {
					subPath = st.Path
				} else {
					subRelIdx = embeddedSubIdx
				}
				break
			}
			if !st.IsExternal {
				embeddedSubIdx++
			}
		}
	}

	if isHTTPURL(subPath) && !isSafeStrmURL(subPath) {
		subPath = ""
	}

	callerUserID, isAdmin := h.callerIdentity(r)
	deviceID := h.resolveDeviceID(r, q)
	playSessionID := scopePlaySessionID(q.Get("PlaySessionId"), callerUserID, isAdmin)
	if !isAdmin && callerUserID != "" && playSessionID == "" {
		if deviceID != "" {
			playSessionID = callerUserID + ":" + deviceID + ":" + item.ID
		} else {
			playSessionID = callerUserID + ":" + item.ID + ":" + msID
		}
	}
	runtimeTicks := resolveRuntimeTicks(item, src)
	return transcode.SessionParams{
		PlaySessionID:         playSessionID,
		DeviceID:              deviceID,
		UserID:                callerUserID,
		ItemID:                item.ID,
		MediaSourceID:         msID,
		InputPath:             inputPath,
		VideoCodec:            q.Get("VideoCodec"),
		AudioCodec:            audioCodec,
		VideoBitrate:          videoBitrate,
		AudioBitrate:          audioBitrate,
		AudioChannels:         audioChannels,
		MaxWidth:              q.Int("MaxWidth", 0),
		MaxHeight:             q.Int("MaxHeight", 0),
		StartTimeTicks:        q.Int64("StartTimeTicks", 0),
		AudioStreamIndex:      audioIdx,
		SubtitleStreamIndex:   subIdx,
		SubtitleRelativeIndex: subRelIdx,
		SubtitleMethod:        subMethod,
		SubtitleCodec:         subCodec,
		SubtitlePath:          subPath,
		IsHDR:                 extractVideoIsHDR(src),
		SegmentDuration:       segDur,
		SegmentContainer:      q.Get("SegmentContainer"),
		DurationSec:           float64(runtimeTicks) / float64(transcode.TicksPerSecond),
		InputHeaders:          inputHeaders,
	}, strmErr
}

func isHTTPURL(s string) bool {
	lower := strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

func isHDRVideoRange(videoRange string) bool {
	vr := strings.ToUpper(strings.TrimSpace(videoRange))
	if vr == "" || vr == "SDR" {
		return false
	}
	for _, kw := range []string{"HDR", "HLG", "DOVI", "DOLBY", "PQ", "BT2020", "SMPTEST2084", "ARIB"} {
		if strings.Contains(vr, kw) {
			return true
		}
	}
	return false
}

func extractVideoIsHDR(src *sqlite.MediaSourceRecord) bool {
	if src == nil {
		return false
	}
	for _, st := range src.Streams {
		if strings.EqualFold(st.StreamType, "Video") && isHDRVideoRange(st.VideoRange) {
			return true
		}
	}
	return false
}

func resolveRuntimeTicks(item *sqlite.ItemRecord, src *sqlite.MediaSourceRecord) int64 {
	if src != nil && src.RuntimeTicks > 0 {
		return src.RuntimeTicks
	}
	if item != nil && item.RuntimeTicks != nil && *item.RuntimeTicks > 0 {
		return *item.RuntimeTicks
	}
	return 0
}

func extractVideoDimensions(src *sqlite.MediaSourceRecord) (int, int) {
	if src == nil {
		return 0, 0
	}
	for _, st := range src.Streams {
		if st.StreamType == "Video" && st.Width > 0 && st.Height > 0 {
			return st.Width, st.Height
		}
	}
	return 0, 0
}

func preserveQuery(raw url.Values) url.Values {
	out := make(url.Values, len(raw))
	for k, v := range raw {
		out[k] = append([]string(nil), v...)
	}
	return out
}

func preserveQueryWithToken(raw url.Values, token string) url.Values {
	out := preserveQuery(raw)
	if token == "" {
		return out
	}
	for k, v := range out {
		if strings.EqualFold(k, "api_key") && len(v) > 0 && strings.TrimSpace(v[0]) != "" {
			return out
		}
	}
	out.Set("api_key", token)
	return out
}

func resolveRequestToken(r *http.Request, q httpx.Query) string {
	if tok := strings.TrimSpace(q.Get("api_key")); tok != "" {
		return tok
	}
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil {
		if tok := strings.TrimSpace(ac.Token); tok != "" {
			return tok
		}
	}
	if tok := strings.TrimSpace(middleware.ExtractClientInfo(r).Token); tok != "" {
		return tok
	}
	return ""
}

func readFirstLineURL(strmPath string) string {
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
			u, err := url.Parse(candidate)
			if err != nil || u.Host == "" {
				return ""
			}
			scheme := strings.ToLower(u.Scheme)
			if scheme != "http" && scheme != "https" {
				return ""
			}
			if isBlockedStrmHost(u.Hostname()) {
				return ""
			}
			return candidate
		}
	}
	return ""
}

func sanitizeAudioCodec(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return ""
	case "copy", "aac", "mp3", "opus", "ac3", "eac3", "flac":
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return "aac"
	}
}

func isSafeStrmURL(rawURL string) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return false
	}
	if isBlockedStrmHost(u.Hostname()) {
		return false
	}
	return netutil.ValidateRemoteURL(rawURL, true) == nil
}

func isBlockedStrmHost(host string) bool {
	h := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if h == "" {
		return true
	}
	if h == "metadata.google.internal" || h == "metadata" || h == "instance-data" {
		return true
	}
	if addr, err := netip.ParseAddr(h); err == nil {
		addr = addr.Unmap()
		if addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() {
			return true
		}
	}
	return false
}

func itemHasAnyAllowedTag(ctx context.Context, repo *sqlite.LibraryRepo, rec *sqlite.ItemRecord, allowedTags []string) bool {
	allowSet := make(map[string]bool, len(allowedTags))
	for _, t := range allowedTags {
		if s := strings.ToLower(strings.TrimSpace(t)); s != "" {
			allowSet[s] = true
		}
	}
	if len(allowSet) == 0 {
		return true
	}
	if repo == nil || rec == nil {
		return false
	}
	checkDTO := func(id string) bool {
		if strings.TrimSpace(id) == "" {
			return false
		}
		it, err := repo.GetItemByID(ctx, "", "", id)
		if err != nil || it == nil {
			return false
		}
		for _, tg := range it.Tags {
			if allowSet[strings.ToLower(strings.TrimSpace(tg))] {
				return true
			}
		}
		return false
	}
	if checkDTO(rec.ID) {
		return true
	}
	if rec.SeriesID != "" && checkDTO(rec.SeriesID) {
		return true
	}
	return false
}

func passthrough(next http.Handler) http.Handler { return next }
