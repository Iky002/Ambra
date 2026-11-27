package compatext

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/netutil"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

// Mirror structures for reading/updating shared livetv:state stored in server_config.
type liveTVChannel struct {
	Id          string `json:"Id"`
	Name        string `json:"Name"`
	ServerId    string `json:"ServerId,omitempty"`
	Type        string `json:"Type"`
	ChannelType string `json:"ChannelType"`
	Number      string `json:"Number,omitempty"`
	TvgId       string `json:"TvgId,omitempty"`
	LogoUrl     string `json:"LogoUrl,omitempty"`
	GroupTitle  string `json:"GroupTitle,omitempty"`
	StreamUrl   string `json:"StreamUrl,omitempty"`
	TunerHostId string `json:"TunerHostId,omitempty"`
	Disabled    bool   `json:"Disabled,omitempty"`
	SortIndex   int    `json:"SortIndex,omitempty"`
}

type liveTVProgram struct {
	Id          string    `json:"Id"`
	Name        string    `json:"Name"`
	ServerId    string    `json:"ServerId,omitempty"`
	Type        string    `json:"Type"`
	ChannelId   string    `json:"ChannelId"`
	ChannelName string    `json:"ChannelName,omitempty"`
	Overview    string    `json:"Overview,omitempty"`
	StartDate   time.Time `json:"StartDate"`
	EndDate     time.Time `json:"EndDate"`
	IsMovie     bool      `json:"IsMovie,omitempty"`
	IsSeries    bool      `json:"IsSeries,omitempty"`
	IsLive      bool      `json:"IsLive,omitempty"`
}

type liveTVRecording struct {
	Id          string    `json:"Id"`
	Name        string    `json:"Name"`
	ServerId    string    `json:"ServerId,omitempty"`
	Type        string    `json:"Type"`
	ChannelId   string    `json:"ChannelId,omitempty"`
	ChannelName string    `json:"ChannelName,omitempty"`
	ProgramId   string    `json:"ProgramId,omitempty"`
	Status      string    `json:"Status"`
	StartDate   time.Time `json:"StartDate"`
	EndDate     time.Time `json:"EndDate"`
	Path        string    `json:"Path,omitempty"`
}

type liveTVTimer struct {
	Id string `json:"Id"`
}

type liveTVTunerHost struct {
	Id                   string `json:"Id"`
	Url                  string `json:"Url,omitempty"`
	Type                 string `json:"Type"`
	DeviceId             string `json:"DeviceId,omitempty"`
	FriendlyName         string `json:"FriendlyName,omitempty"`
	ImportFavoritesOnly  bool   `json:"ImportFavoritesOnly,omitempty"`
	AllowHWTranscoding   bool   `json:"AllowHWTranscoding,omitempty"`
	AllowPrivateNetwork  bool   `json:"AllowPrivateNetwork,omitempty"`
	TunerCount           int    `json:"TunerCount,omitempty"`
	UserAgent            string `json:"UserAgent,omitempty"`
	M3UContent           string `json:"M3UContent,omitempty"`
	ImportedChannelCount int    `json:"ImportedChannelCount,omitempty"`
}

type liveTVListingProvider struct {
	Id       string `json:"Id"`
	Type     string `json:"Type"`
	Path     string `json:"Path,omitempty"`
	Username string `json:"Username,omitempty"`
	Listings string `json:"ListingsId,omitempty"`
	Country  string `json:"Country,omitempty"`
}

type sharedLiveTVState struct {
	Channels         []liveTVChannel         `json:"channels"`
	Programs         []liveTVProgram         `json:"programs"`
	Recordings       []liveTVRecording       `json:"recordings"`
	Timers           []json.RawMessage       `json:"timers"`
	SeriesTimers     []json.RawMessage       `json:"series_timers"`
	TunerHosts       []liveTVTunerHost       `json:"tuner_hosts"`
	ListingProviders []liveTVListingProvider `json:"listing_providers"`
}

// ChannelManagementInfo is returned by GET /LiveTv/Manage/Channels.
type ChannelManagementInfo struct {
	Id        string `json:"Id"`
	Name      string `json:"Name"`
	Number    string `json:"Number,omitempty"`
	Disabled  bool   `json:"Disabled"`
	SortIndex int    `json:"SortIndex"`
}

// NameIdPair is a standard Emby name/id pair.
type NameIdPair struct {
	Name string `json:"Name"`
	Id   string `json:"Id"`
}

// NameValuePair is a standard Emby name/value pair.
type NameValuePair struct {
	Name  string `json:"Name"`
	Value string `json:"Value"`
}

func (h *Handler) registerLiveTVRoutes(r chi.Router, wrapAuth, wrapAdmin func(http.Handler) http.Handler) {
	// Authenticated LiveTv routes
	r.Group(func(r chi.Router) {
		r.Use(wrapAuth)
		r.Use(h.requireLiveTvAccess)

		// LiveStreamFiles (11 routes)
		r.Get("/LiveTv/LiveStreamFiles/{Id}/stream.{Container}", h.handleLiveStreamFile)
		r.Get("/LiveTv/LiveStreamFiles/{Id}/hls/master.m3u8", h.handleLiveStreamHLS(false))
		r.Head("/LiveTv/LiveStreamFiles/{Id}/hls/master.m3u8", h.handleLiveStreamHLS(true))
		r.Get("/LiveTv/LiveStreamFiles/{Id}/hls/master_local.m3u8", h.handleLiveStreamHLS(false))
		r.Head("/LiveTv/LiveStreamFiles/{Id}/hls/master_local.m3u8", h.handleLiveStreamHLS(true))
		r.Get("/LiveTv/LiveStreamFiles/{Id}/hls/live.m3u8", h.handleLiveStreamHLS(false))
		r.Head("/LiveTv/LiveStreamFiles/{Id}/hls/live.m3u8", h.handleLiveStreamHLS(true))
		r.Get("/LiveTv/LiveStreamFiles/{Id}/hls/live_local.m3u8", h.handleLiveStreamHLS(false))
		r.Head("/LiveTv/LiveStreamFiles/{Id}/hls/live_local.m3u8", h.handleLiveStreamHLS(true))
		r.Get("/LiveTv/LiveStreamFiles/{Id}/hls/{Segment}", h.handleLiveStreamHLS(false))
		r.Head("/LiveTv/LiveStreamFiles/{Id}/hls/{Segment}", h.handleLiveStreamHLS(true))

		// LiveRecordings (8 routes)
		r.Get("/LiveTv/LiveRecordings/{Id}/stream", h.handleLiveRecordingStream)
		r.Head("/LiveTv/LiveRecordings/{Id}/stream", h.handleLiveRecordingStream)
		r.Get("/LiveTv/LiveRecordings/{Id}/hls/master.m3u8", h.handleLiveRecordingHLS(false))
		r.Head("/LiveTv/LiveRecordings/{Id}/hls/master.m3u8", h.handleLiveRecordingHLS(true))
		r.Get("/LiveTv/LiveRecordings/{Id}/hls/live.m3u8", h.handleLiveRecordingHLS(false))
		r.Head("/LiveTv/LiveRecordings/{Id}/hls/live.m3u8", h.handleLiveRecordingHLS(true))
		r.Get("/LiveTv/LiveRecordings/{Id}/hls/{Segment}", h.handleLiveRecordingHLS(false))
		r.Head("/LiveTv/LiveRecordings/{Id}/hls/{Segment}", h.handleLiveRecordingHLS(true))

		// LiveTv general authenticated endpoints (8 routes)
		r.Get("/LiveTv/AvailableRecordingOptions", h.handleAvailableRecordingOptions)
		r.Get("/LiveTv/EPG", h.handleEPG)
		r.Get("/LiveTv/Folder", h.handleLiveTVFolder)
		r.Get("/LiveTv/ChannelTags", h.handleChannelTags)
		r.Get("/LiveTv/ChannelTags/Prefixes", h.handleChannelTagPrefixes)
		r.Post("/LiveTv/Timers/{Id}/Delete", h.handleDeleteTimerPost)
		r.Post("/LiveTv/SeriesTimers/{Id}/Delete", h.handleDeleteSeriesTimerPost)
		r.Get("/LiveTv/ListingProviders/Default", h.handleListingProviderDefault)
	})

	// LiveTv management endpoints accessible to admins and users with EnableLiveTvManagement
	r.Group(func(r chi.Router) {
		r.Use(wrapAuth)
		r.Get("/LiveTv/ChannelMappingOptions", h.handleChannelMappingOptions)
		r.Post("/LiveTv/ChannelMappings", h.handleSetChannelMapping)
	})

	// Admin LiveTv routes (14 routes)
	r.Group(func(r chi.Router) {
		r.Use(wrapAdmin)

		r.Get("/LiveTv/Manage/Channels", h.handleManageChannels)
		r.Post("/LiveTv/Manage/Channels/{Id}/Disabled", h.handleSetChannelDisabled)
		r.Post("/LiveTv/Manage/Channels/{Id}/SortIndex", h.handleSetChannelSortIndex)
		r.Post("/LiveTv/Tuners/{Id}/Reset", h.handleResetTuner)
		r.Post("/LiveTv/Recordings/{Id}/Delete", h.handleDeleteRecordingPost)
		r.Get("/LiveTv/TunerHosts", h.handleGetTunerHosts)
		r.Get("/LiveTv/TunerHosts/Default/{Type}", h.handleDefaultTunerHost)
		r.Post("/LiveTv/TunerHosts/Delete", h.handleDeleteTunerHostPost)
		r.Get("/LiveTv/ListingProviders/Available", h.handleAvailableListingProviders)
		r.Get("/LiveTv/ListingProviders", h.handleGetListingProviders)
		r.Post("/LiveTv/ListingProviders/Delete", h.handleDeleteListingProviderPost)
		r.Get("/LiveTv/ListingProviders/Lineups", h.handleListingProviderLineups)
		r.Get("/LiveTv/Tuners/Discover", h.handleDiscoverTuners)
		r.Get("/LiveTv/Tuners/Discvover", h.handleDiscoverTuners) // Emby legacy typo alias
	})
}

func (h *Handler) lookupChannel(ctx context.Context, id string) (liveTVChannel, bool) {
	h.mu.RLock()
	lt := h.liveTV
	disabledOverride, hasDisabledOverride := h.state.ChannelDisabled[id]
	sortOverride, hasSortOverride := h.state.ChannelSortIndex[id]
	h.mu.RUnlock()
	if lt != nil {
		if ch, ok := lt.GetChannelByID(id); ok && ch != nil {
			res := liveTVChannel{
				Id:          ch.Id,
				Name:        ch.Name,
				ServerId:    ch.ServerId,
				Type:        ch.Type,
				ChannelType: ch.ChannelType,
				Number:      ch.Number,
				TvgId:       ch.TvgId,
				LogoUrl:     ch.LogoUrl,
				GroupTitle:  ch.GroupTitle,
				StreamUrl:   ch.StreamUrl,
				TunerHostId: ch.TunerHostId,
				Disabled:    ch.Disabled,
				SortIndex:   ch.SortIndex,
			}
			if hasDisabledOverride {
				res.Disabled = disabledOverride
			}
			if hasSortOverride {
				res.SortIndex = sortOverride
			}
			return res, true
		}
	}
	var st sharedLiveTVState
	if h.readConfigJSON(ctx, liveTVConfigKey, &st) {
		for _, ch := range st.Channels {
			if ch.Id == id {
				if hasDisabledOverride {
					ch.Disabled = disabledOverride
				}
				if hasSortOverride {
					ch.SortIndex = sortOverride
				}
				return ch, true
			}
		}
	}
	return liveTVChannel{}, false
}

func (h *Handler) snapshotLiveTVChannels(ctx context.Context) []liveTVChannel {
	h.mu.RLock()
	lt := h.liveTV
	h.mu.RUnlock()
	if lt != nil {
		src := lt.SnapshotChannels()
		out := make([]liveTVChannel, len(src))
		for i, ch := range src {
			out[i] = liveTVChannel{
				Id:          ch.Id,
				Name:        ch.Name,
				ServerId:    ch.ServerId,
				Type:        ch.Type,
				ChannelType: ch.ChannelType,
				Number:      ch.Number,
				TvgId:       ch.TvgId,
				LogoUrl:     ch.LogoUrl,
				GroupTitle:  ch.GroupTitle,
				StreamUrl:   ch.StreamUrl,
				TunerHostId: ch.TunerHostId,
				Disabled:    ch.Disabled,
				SortIndex:   ch.SortIndex,
			}
		}
		return out
	}
	var st sharedLiveTVState
	_ = h.readConfigJSON(ctx, liveTVConfigKey, &st)
	return st.Channels
}

func (h *Handler) snapshotLiveTVPrograms(ctx context.Context) []liveTVProgram {
	h.mu.RLock()
	lt := h.liveTV
	h.mu.RUnlock()
	if lt != nil {
		src := lt.SnapshotPrograms()
		out := make([]liveTVProgram, len(src))
		for i, p := range src {
			out[i] = liveTVProgram{
				Id:          p.Id,
				Name:        p.Name,
				ServerId:    p.ServerId,
				Type:        p.Type,
				ChannelId:   p.ChannelId,
				ChannelName: p.ChannelName,
				Overview:    p.Overview,
				StartDate:   p.StartDate,
				EndDate:     p.EndDate,
				IsMovie:     p.IsMovie,
				IsSeries:    p.IsSeries,
				IsLive:      p.IsLive,
			}
		}
		return out
	}
	var st sharedLiveTVState
	_ = h.readConfigJSON(ctx, liveTVConfigKey, &st)
	return st.Programs
}

func (h *Handler) snapshotLiveTVTunerHosts(ctx context.Context) []liveTVTunerHost {
	h.mu.RLock()
	lt := h.liveTV
	h.mu.RUnlock()
	if lt != nil {
		src := lt.SnapshotTunerHosts()
		out := make([]liveTVTunerHost, len(src))
		for i, t := range src {
			out[i] = liveTVTunerHost{
				Id:                   t.Id,
				Url:                  t.Url,
				Type:                 t.Type,
				DeviceId:             t.DeviceId,
				FriendlyName:         t.FriendlyName,
				ImportFavoritesOnly:  t.ImportFavoritesOnly,
				AllowHWTranscoding:   t.AllowHWTranscoding,
				AllowPrivateNetwork:  t.AllowPrivateNetwork,
				TunerCount:           t.TunerCount,
				UserAgent:            t.UserAgent,
				M3UContent:           t.M3UContent,
				ImportedChannelCount: t.ImportedChannelCount,
			}
		}
		return out
	}
	var st sharedLiveTVState
	_ = h.readConfigJSON(ctx, liveTVConfigKey, &st)
	if st.TunerHosts == nil {
		return []liveTVTunerHost{}
	}
	return st.TunerHosts
}

func (h *Handler) snapshotLiveTVListingProviders(ctx context.Context) []liveTVListingProvider {
	h.mu.RLock()
	lt := h.liveTV
	h.mu.RUnlock()
	if lt != nil {
		src := lt.SnapshotListingProviders()
		out := make([]liveTVListingProvider, len(src))
		for i, lp := range src {
			out[i] = liveTVListingProvider{
				Id:       lp.Id,
				Type:     lp.Type,
				Path:     lp.Path,
				Username: lp.Username,
				Listings: lp.Listings,
				Country:  lp.Country,
			}
		}
		return out
	}
	var st sharedLiveTVState
	_ = h.readConfigJSON(ctx, liveTVConfigKey, &st)
	if st.ListingProviders == nil {
		return []liveTVListingProvider{}
	}
	return st.ListingProviders
}

func (h *Handler) lookupRecording(ctx context.Context, id string) (liveTVRecording, bool) {
	h.mu.RLock()
	lt := h.liveTV
	h.mu.RUnlock()
	if lt != nil {
		if rec, ok := lt.GetRecordingByID(id); ok && rec != nil {
			return liveTVRecording{
				Id:          rec.Id,
				Name:        rec.Name,
				ServerId:    rec.ServerId,
				Type:        rec.Type,
				ChannelId:   rec.ChannelId,
				ChannelName: rec.ChannelName,
				ProgramId:   rec.ProgramId,
				Status:      rec.Status,
				StartDate:   rec.StartDate,
				EndDate:     rec.EndDate,
				Path:        rec.Path,
			}, true
		}
	}
	var st sharedLiveTVState
	if h.readConfigJSON(ctx, liveTVConfigKey, &st) {
		for _, rec := range st.Recordings {
			if rec.Id == id {
				return rec, true
			}
		}
	}
	return liveTVRecording{}, false
}

func isHLSStreamURL(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false
	}
	lowerRaw := strings.ToLower(trimmed)
	lowerPath := lowerRaw
	if u, err := url.Parse(trimmed); err == nil && u.Path != "" {
		lowerPath = strings.ToLower(u.Path)
	}
	return strings.HasSuffix(lowerPath, ".m3u8") ||
		strings.HasSuffix(lowerPath, ".m3u") ||
		strings.Contains(lowerPath, ".m3u8") ||
		strings.Contains(lowerRaw, "format=m3u8") ||
		strings.Contains(lowerRaw, "type=m3u8")
}

func appendRawQuery(targetPath, rawQuery string) string {
	if rawQuery == "" {
		return targetPath
	}
	return targetPath + "?" + rawQuery
}

func sanitizeLiveRecVideoCodec(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "h264", "avc":
		return "h264"
	case "hevc", "h265":
		return "hevc"
	case "av1":
		return "av1"
	case "vp9":
		return "vp9"
	case "copy":
		return "copy"
	default:
		return "h264"
	}
}

func sanitizeLiveRecAudioCodec(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "aac":
		return "aac"
	case "mp3", "opus", "flac", "ac3", "eac3", "vorbis", "copy":
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return "aac"
	}
}

func isRemoteLiveTVClientRequest(r *http.Request, cfg *config.Config) bool {
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

func writeCompatExtForbidden(w http.ResponseWriter, r *http.Request, headOnly bool, msg string) {
	if headOnly || r.Method == http.MethodHead {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	httpx.Error(w, http.StatusForbidden, msg)
}

func (h *Handler) handleLiveStreamFile(w http.ResponseWriter, r *http.Request) {
	if !h.canPlayMedia(r) {
		writeCompatExtForbidden(w, r, false, "Media playback is disabled for this user")
		return
	}
	id := chi.URLParam(r, "Id")
	ch, ok := h.lookupChannel(r.Context(), id)
	if !ok || strings.TrimSpace(ch.StreamUrl) == "" {
		httpx.Error(w, http.StatusNotFound, "live stream channel not found")
		return
	}
	if ch.Disabled {
		httpx.Error(w, http.StatusForbidden, "live stream channel is disabled")
		return
	}
	h.mu.RLock()
	lt := h.liveTV
	h.mu.RUnlock()
	if lt != nil {
		lt.ProxyChannelStream(w, r, ch.Id, ch.StreamUrl)
		return
	}
	target := appendRawQuery("/LiveTv/Channels/"+url.PathEscape(ch.Id)+"/Stream", r.URL.RawQuery)
	http.Redirect(w, r, target, http.StatusFound)
}

func (h *Handler) handleLiveRecordingStream(w http.ResponseWriter, r *http.Request) {
	if !h.canPlayMedia(r) {
		writeCompatExtForbidden(w, r, false, "Media playback is disabled for this user")
		return
	}
	id := chi.URLParam(r, "Id")
	rec, ok := h.lookupRecording(r.Context(), id)
	if !ok || strings.TrimSpace(rec.Path) == "" {
		httpx.Error(w, http.StatusNotFound, "live recording not found")
		return
	}
	resolved, ok := h.resolveRecordingPath(rec.Path)
	if !ok {
		httpx.Error(w, http.StatusForbidden, "recording path is outside recordings directory")
		return
	}
	fi, err := os.Stat(resolved)
	if err != nil || fi.IsDir() {
		httpx.Error(w, http.StatusNotFound, "recording file not found")
		return
	}
	h.mu.RLock()
	tcMgr := h.transcodeMgr
	h.mu.RUnlock()
	if r.Method != http.MethodHead && h.cfg != nil && tcMgr != nil {
		limit := h.cfg.Snapshot().SimultaneousStreamLimit
		if limit <= 0 {
			limit = h.cfg.SimultaneousStreamLimit
		}
		if limit > 0 {
			releaseSlot, allowed := tcMgr.TryAcquireStreamSlot(limit, "")
			if !allowed {
				httpx.Error(w, http.StatusTooManyRequests, "simultaneous stream limit reached")
				return
			}
			defer releaseSlot()
		}
	}
	http.ServeFile(w, r, resolved)
}

func (h *Handler) recordingsDir() string {
	h.mu.RLock()
	rec := h.dvrRec
	h.mu.RUnlock()
	if rec != nil && strings.TrimSpace(rec.RecordingsDir()) != "" {
		return rec.RecordingsDir()
	}
	if h.cfg != nil && strings.TrimSpace(h.cfg.DataDir) != "" {
		return filepath.Join(h.cfg.DataDir, "recordings")
	}
	return ""
}

func (h *Handler) resolveRecordingPath(target string) (string, bool) {
	baseDir := strings.TrimSpace(h.recordingsDir())
	target = strings.TrimSpace(target)
	if baseDir == "" || target == "" {
		return "", false
	}
	resolvedDir, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return "", false
	}
	resolvedDir = filepath.Clean(resolvedDir)

	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", false
	}
	resolvedTarget = filepath.Clean(resolvedTarget)

	rel, err := filepath.Rel(resolvedDir, resolvedTarget)
	if err != nil {
		return "", false
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	return resolvedTarget, true
}

func (h *Handler) handleLiveStreamHLS(headOnly bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.canPlayMedia(r) {
			writeCompatExtForbidden(w, r, headOnly, "Media playback is disabled for this user")
			return
		}
		id := chi.URLParam(r, "Id")
		ch, ok := h.lookupChannel(r.Context(), id)
		if !ok || strings.TrimSpace(ch.StreamUrl) == "" {
			if headOnly {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusNotFound)
				return
			}
			httpx.Error(w, http.StatusNotFound, "live stream channel not found")
			return
		}
		if ch.Disabled {
			if headOnly {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			httpx.Error(w, http.StatusForbidden, "live stream channel is disabled")
			return
		}

		isPlaylist := chi.URLParam(r, "Segment") == ""
		if isPlaylist && isHLSStreamURL(ch.StreamUrl) {
			if headOnly {
				w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
				w.WriteHeader(http.StatusOK)
				return
			}
			h.mu.RLock()
			lt := h.liveTV
			h.mu.RUnlock()
			if lt != nil {
				lt.ProxyChannelStream(w, r, ch.Id, ch.StreamUrl)
				return
			}
		}

		target := appendRawQuery("/LiveTv/Channels/"+url.PathEscape(ch.Id)+"/Stream", r.URL.RawQuery)
		http.Redirect(w, r, target, http.StatusFound)
	}
}

func (h *Handler) handleLiveRecordingHLS(headOnly bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.canPlayMedia(r) {
			writeCompatExtForbidden(w, r, headOnly, "Media playback is disabled for this user")
			return
		}
		id := chi.URLParam(r, "Id")
		rec, ok := h.lookupRecording(r.Context(), id)
		if !ok || strings.TrimSpace(rec.Path) == "" {
			if headOnly {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusNotFound)
				return
			}
			httpx.Error(w, http.StatusNotFound, "live recording not found")
			return
		}
		resolved, ok := h.resolveRecordingPath(rec.Path)
		if !ok {
			if headOnly {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusForbidden)
				return
			}
			httpx.Error(w, http.StatusForbidden, "recording path is outside recordings directory")
			return
		}
		fi, err := os.Stat(resolved)
		if err != nil || fi.IsDir() {
			if headOnly {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusNotFound)
				return
			}
			httpx.Error(w, http.StatusNotFound, "recording file not found")
			return
		}
		if strings.EqualFold(filepath.Ext(resolved), ".m3u8") {
			http.ServeFile(w, r, resolved)
			return
		}

		h.mu.RLock()
		tcMgr := h.transcodeMgr
		libRepo := h.libRepo
		h.mu.RUnlock()

		if tcMgr != nil {
			h.serveLiveRecordingDynamicHLS(w, r, tcMgr, libRepo, rec, resolved, headOnly)
			return
		}

		target := appendRawQuery("/LiveTv/LiveRecordings/"+url.PathEscape(rec.Id)+"/stream", r.URL.RawQuery)
		http.Redirect(w, r, target, http.StatusFound)
	}
}

func (h *Handler) serveLiveRecordingDynamicHLS(
	w http.ResponseWriter,
	r *http.Request,
	tcMgr *transcode.Manager,
	libRepo *sqlite.LibraryRepo,
	rec liveTVRecording,
	resolvedPath string,
	headOnly bool,
) {
	isHead := headOnly || r.Method == http.MethodHead
	q := r.URL.Query()
	segContainer := strings.ToLower(strings.TrimSpace(q.Get("SegmentContainer")))
	if segContainer != "mp4" && segContainer != "m4s" {
		segContainer = "ts"
	}
	vCodec := sanitizeLiveRecVideoCodec(q.Get("VideoCodec"))
	aCodec := sanitizeLiveRecAudioCodec(q.Get("AudioCodec"))

	pol, isAdminCaller, hasAuth := h.callerPolicy(r)
	if hasAuth && !isAdminCaller {
		if !pol.EnableMediaPlayback {
			writeCompatExtForbidden(w, r, isHead, "Media playback is disabled for this user")
			return
		}
		if vCodec == "copy" {
			if !pol.EnablePlaybackRemuxing && !pol.EnableVideoPlaybackTranscoding {
				writeCompatExtForbidden(w, r, isHead, "Playback remuxing is disabled for this user")
				return
			}
		} else if !pol.EnableVideoPlaybackTranscoding {
			writeCompatExtForbidden(w, r, isHead, "Video playback transcoding is disabled for this user")
			return
		}
	}

	videoBR, _ := strconv.Atoi(strings.TrimSpace(q.Get("VideoBitrate")))
	if videoBR <= 0 {
		videoBR = 4_000_000
	}
	videoBR = min(max(64_000, videoBR), transcode.MaxVideoBitrate)

	audioBR, _ := strconv.Atoi(strings.TrimSpace(q.Get("AudioBitrate")))
	if audioBR <= 0 {
		audioBR = 192_000
	}
	audioBR = min(max(32_000, audioBR), transcode.MaxAudioBitrate)

	maxW, _ := strconv.Atoi(strings.TrimSpace(q.Get("MaxWidth")))
	maxH, _ := strconv.Atoi(strings.TrimSpace(q.Get("MaxHeight")))
	maxW = min(max(0, maxW), transcode.MaxVideoDimension)
	maxH = min(max(0, maxH), transcode.MaxVideoDimension)

	segDur, _ := strconv.Atoi(strings.TrimSpace(q.Get("SegmentLength")))
	if segDur <= 0 {
		segDur, _ = strconv.Atoi(strings.TrimSpace(q.Get("SegmentDuration")))
	}
	if segDur <= 0 {
		segDur = transcode.DefaultSegmentDuration
	}
	segDur = min(max(2, segDur), 15)

	if h.cfg != nil {
		remoteLimit := h.cfg.Snapshot().RemoteClientBitrateLimit
		if remoteLimit <= 0 {
			remoteLimit = h.cfg.RemoteClientBitrateLimit
		}
		if remoteLimit > 0 && isRemoteLiveTVClientRequest(r, h.cfg) {
			limit := min(remoteLimit, transcode.MaxVideoBitrate)
			if audioBR >= limit {
				audioBR = max(32_000, min(limit/4, 192_000))
				if audioBR > limit {
					audioBR = limit
				}
			}
			maxAllowedV := max(64_000, limit-audioBR)
			if maxAllowedV > limit {
				maxAllowedV = limit
			}
			if videoBR > maxAllowedV {
				videoBR = maxAllowedV
			}
		}
	}

	durSec := 0.0
	if libRepo != nil {
		if itemRec, err := libRepo.GetItemRecordByID(r.Context(), rec.Id); err == nil && itemRec != nil && itemRec.RuntimeTicks != nil && *itemRec.RuntimeTicks > 0 {
			durSec = float64(*itemRec.RuntimeTicks) / float64(transcode.TicksPerSecond)
		}
	}
	if durSec <= 0 && !rec.StartDate.IsZero() && rec.EndDate.After(rec.StartDate) {
		durSec = rec.EndDate.Sub(rec.StartDate).Seconds()
	}
	if durSec <= 0 {
		durSec = 60
	}

	callerID, _ := h.callerIdentity(r)
	playSessionID := strings.TrimSpace(q.Get("PlaySessionId"))
	if playSessionID == "" {
		playSessionID = "liverec-" + rec.Id
	}
	sessParams := transcode.SessionParams{
		PlaySessionID:    playSessionID,
		DeviceID:         strings.TrimSpace(q.Get("DeviceId")),
		UserID:           callerID,
		ItemID:           rec.Id,
		MediaSourceID:    rec.Id,
		InputPath:        resolvedPath,
		VideoCodec:       vCodec,
		AudioCodec:       aCodec,
		VideoBitrate:     videoBR,
		AudioBitrate:     audioBR,
		MaxWidth:         maxW,
		MaxHeight:        maxH,
		SegmentDuration:  segDur,
		SegmentContainer: segContainer,
		DurationSec:      durSec,
	}

	preservedQuery := make(url.Values, len(q)+1)
	hasAPIKey := false
	for k, vals := range q {
		preservedQuery[k] = append([]string(nil), vals...)
		if strings.EqualFold(k, "api_key") && len(vals) > 0 && strings.TrimSpace(vals[0]) != "" {
			hasAPIKey = true
		}
	}
	if !hasAPIKey {
		if tok := resolveLiveRecordingRequestToken(r, q); tok != "" {
			preservedQuery.Set("api_key", tok)
		}
	}
	plParams := transcode.PlaylistParams{
		ItemID:           rec.Id,
		PlaylistID:       "main",
		DurationSec:      durSec,
		SegmentDuration:  segDur,
		SegmentContainer: segContainer,
		Bandwidth:        videoBR + audioBR,
		Width:            maxW,
		Height:           maxH,
		VideoCodec:       vCodec,
		AudioCodec:       aCodec,
		Query:            preservedQuery,
	}

	segParam := strings.TrimSpace(chi.URLParam(r, "Segment"))
	lowerPath := strings.ToLower(r.URL.Path)

	if segParam == "" && strings.HasSuffix(lowerPath, "/master.m3u8") {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-cache, no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if isHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		master := transcode.BuildMasterPlaylist(plParams)
		master = strings.Replace(master, "\nmain.m3u8", "\nlive.m3u8", 1)
		w.Header().Set("Content-Length", strconv.Itoa(len(master)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(master))
		return
	}

	if (segParam == "" && strings.HasSuffix(lowerPath, "/live.m3u8")) || strings.EqualFold(segParam, "main.m3u8") || strings.EqualFold(segParam, "live.m3u8") {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-cache, no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if isHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		if h.cfg != nil {
			limit := h.cfg.Snapshot().SimultaneousStreamLimit
			if limit <= 0 {
				limit = h.cfg.SimultaneousStreamLimit
			}
			if limit > 0 {
				releaseSlot, allowed := tcMgr.TryAcquireStreamSlot(limit, transcode.SessionKey(sessParams))
				if !allowed {
					httpx.Error(w, http.StatusTooManyRequests, "simultaneous stream limit reached")
					return
				}
				defer releaseSlot()
			}
		}
		_, _ = tcMgr.RegisterSession(sessParams)
		mediaPL := transcode.BuildMediaPlaylist(plParams)
		mediaPL = strings.ReplaceAll(mediaPL, "hls1/main/", "")
		w.Header().Set("Content-Length", strconv.Itoa(len(mediaPL)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(mediaPL))
		return
	}

	if strings.EqualFold(segParam, "init.mp4") {
		if isHead {
			w.Header().Set("Content-Type", "video/mp4")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.WriteHeader(http.StatusOK)
			return
		}
		if h.cfg != nil {
			limit := h.cfg.Snapshot().SimultaneousStreamLimit
			if limit <= 0 {
				limit = h.cfg.SimultaneousStreamLimit
			}
			if limit > 0 {
				releaseSlot, allowed := tcMgr.TryAcquireStreamSlot(limit, transcode.SessionKey(sessParams))
				if !allowed {
					httpx.Error(w, http.StatusTooManyRequests, "simultaneous stream limit reached")
					return
				}
				defer releaseSlot()
			}
		}
		initPath, err := tcMgr.GetInitSegment(r.Context(), sessParams)
		if err != nil {
			slog.Error("recording hls init segment failed", "recording_id", rec.Id, "error", err)
			httpx.Error(w, http.StatusBadGateway, "recording hls init segment unavailable")
			return
		}
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeFile(w, r, initPath)
		return
	}

	segIdx, segExt, parseErr := transcode.ParseSegmentID(segParam)
	if parseErr != nil {
		if isHead {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		httpx.Error(w, http.StatusBadRequest, "invalid hls segment identifier")
		return
	}
	sessParams.SegmentContainer = segExt
	segCT := "video/mp2t"
	if segExt == "mp4" || segExt == "m4s" {
		segCT = "video/mp4"
	}
	if isHead {
		w.Header().Set("Content-Type", segCT)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)
		return
	}
	if h.cfg != nil {
		limit := h.cfg.Snapshot().SimultaneousStreamLimit
		if limit <= 0 {
			limit = h.cfg.SimultaneousStreamLimit
		}
		if limit > 0 {
			releaseSlot, allowed := tcMgr.TryAcquireStreamSlot(limit, transcode.SessionKey(sessParams))
			if !allowed {
				httpx.Error(w, http.StatusTooManyRequests, "simultaneous stream limit reached")
				return
			}
			defer releaseSlot()
		}
	}
	segPath, err := tcMgr.GetSegment(r.Context(), sessParams, segIdx)
	if err != nil {
		slog.Error("recording hls segment failed", "recording_id", rec.Id, "segment", segIdx, "error", err)
		httpx.Error(w, http.StatusBadGateway, "recording hls segment unavailable")
		return
	}
	w.Header().Set("Content-Type", segCT)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, segPath)
}

func (h *Handler) handleAvailableRecordingOptions(w http.ResponseWriter, r *http.Request) {
	baseDir := ""
	if !h.isNonAdminCaller(r) && h.cfg != nil && h.cfg.DataDir != "" {
		baseDir = filepath.Join(h.cfg.DataDir, "recordings")
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"DefaultRecordingPath":      baseDir,
		"MovieRecordingPath":        baseDir,
		"SeriesRecordingPath":       baseDir,
		"PrePaddingSeconds":         60,
		"PostPaddingSeconds":        120,
		"EnableRecordingSubfolders": true,
		"RecordingPostProcessor":    "",
	})
}

func (h *Handler) handleEPG(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	channelFilter := make(map[string]struct{})
	for _, cid := range q.List("ChannelIds") {
		channelFilter[cid] = struct{}{}
	}

	programs := h.snapshotLiveTVPrograms(r.Context())

	items := make([]liveTVProgram, 0, len(programs))
	for _, p := range programs {
		if len(channelFilter) > 0 {
			if _, ok := channelFilter[p.ChannelId]; !ok {
				continue
			}
		}
		items = append(items, p)
	}
	page, total := paginate(items, q.Int("StartIndex", 0), q.Int("Limit", 0))
	httpx.JSON(w, http.StatusOK, dto.QueryResult[liveTVProgram]{
		Items:            page,
		TotalRecordCount: total,
	})
}

func (h *Handler) handleManageChannels(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	channels := h.snapshotLiveTVChannels(r.Context())

	h.mu.RLock()
	out := make([]ChannelManagementInfo, 0, len(channels))
	for i, ch := range channels {
		sortIdx := i
		if ch.SortIndex != 0 {
			sortIdx = ch.SortIndex
		}
		if v, ok := h.state.ChannelSortIndex[ch.Id]; ok {
			sortIdx = v
		}
		disabled := ch.Disabled
		if v, ok := h.state.ChannelDisabled[ch.Id]; ok {
			disabled = v
		}
		out = append(out, ChannelManagementInfo{
			Id:        ch.Id,
			Name:      ch.Name,
			Number:    ch.Number,
			Disabled:  disabled,
			SortIndex: sortIdx,
		})
	}
	h.mu.RUnlock()

	sort.SliceStable(out, func(i, j int) bool {
		return out[i].SortIndex < out[j].SortIndex
	})

	page, total := paginate(out, q.Int("StartIndex", 0), q.Int("Limit", 0))
	httpx.JSON(w, http.StatusOK, dto.QueryResult[ChannelManagementInfo]{
		Items:            page,
		TotalRecordCount: total,
	})
}

func (h *Handler) handleSetChannelDisabled(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "Id")
	q := httpx.ParseQuery(r)
	disabled := false
	if b := q.Bool("Disabled"); b != nil {
		disabled = *b
	} else if r.Body != nil {
		var body struct {
			Disabled bool `json:"Disabled"`
		}
		_ = httpx.DecodeJSONLimit(r, &body, 0)
		disabled = body.Disabled
	}

	h.mu.Lock()
	h.ensureMapsLocked()
	snapshot := cloneCompatExtState(h.state)
	h.state.ChannelDisabled[id] = disabled
	err := h.saveStateLocked(r.Context())
	if err != nil {
		h.state = snapshot
	}
	lt := h.liveTV
	h.mu.Unlock()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to persist channel state")
		return
	}

	if lt != nil {
		_ = lt.SetChannelDisabled(r.Context(), id, disabled)
		httpx.NoContent(w)
		return
	}

	var st sharedLiveTVState
	if h.readConfigJSON(r.Context(), liveTVConfigKey, &st) {
		changed := false
		for i := range st.Channels {
			if st.Channels[i].Id == id {
				st.Channels[i].Disabled = disabled
				changed = true
				break
			}
		}
		if changed {
			if err := h.writeConfigJSON(r.Context(), liveTVConfigKey, st); err != nil {
				httpx.Error(w, http.StatusInternalServerError, "failed to persist livetv state")
				return
			}
		}
	}
	httpx.NoContent(w)
}

func (h *Handler) handleSetChannelSortIndex(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "Id")
	q := httpx.ParseQuery(r)
	newIdx := 0
	switch {
	case q.Has("NewIndex"):
		newIdx = q.Int("NewIndex", 0)
	case q.Has("SortIndex"):
		newIdx = q.Int("SortIndex", 0)
	default:
		if r.Body != nil {
			var body struct {
				NewIndex  *int `json:"NewIndex"`
				SortIndex *int `json:"SortIndex"`
			}
			if err := httpx.DecodeJSONLimit(r, &body, 0); err == nil {
				if body.NewIndex != nil {
					newIdx = *body.NewIndex
				} else if body.SortIndex != nil {
					newIdx = *body.SortIndex
				}
			}
		}
	}

	h.mu.Lock()
	h.ensureMapsLocked()
	snapshot := cloneCompatExtState(h.state)
	h.state.ChannelSortIndex[id] = newIdx
	err := h.saveStateLocked(r.Context())
	if err != nil {
		h.state = snapshot
	}
	lt := h.liveTV
	h.mu.Unlock()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to persist channel sort index")
		return
	}

	if lt != nil {
		_ = lt.SetChannelSortIndex(r.Context(), id, newIdx)
		httpx.NoContent(w)
		return
	}

	var st sharedLiveTVState
	if h.readConfigJSON(r.Context(), liveTVConfigKey, &st) {
		changed := false
		for i := range st.Channels {
			if st.Channels[i].Id == id {
				st.Channels[i].SortIndex = newIdx
				changed = true
				break
			}
		}
		if changed {
			if err := h.writeConfigJSON(r.Context(), liveTVConfigKey, st); err != nil {
				httpx.Error(w, http.StatusInternalServerError, "failed to persist livetv state")
				return
			}
		}
	}
	httpx.NoContent(w)
}

func (h *Handler) handleLiveTVFolder(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]any{
		"Name":           "Live TV",
		"ServerId":       h.serverID(),
		"Id":             "livetv-root",
		"Type":           "Folder",
		"CollectionType": "livetv",
		"IsFolder":       true,
	})
}

func (h *Handler) handleResetTuner(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "Id")
	h.mu.Lock()
	h.ensureMapsLocked()
	snapshot := cloneCompatExtState(h.state)
	h.state.TunerResetAt[id] = time.Now().UTC().Unix()
	err := h.saveStateLocked(r.Context())
	if err != nil {
		h.state = snapshot
	}
	h.mu.Unlock()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to persist tuner reset")
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) collectChannelTags(r *http.Request) []string {
	channels := h.snapshotLiveTVChannels(r.Context())
	seen := make(map[string]struct{})
	var tags []string
	for _, ch := range channels {
		tag := strings.TrimSpace(ch.GroupTitle)
		if tag == "" {
			continue
		}
		key := strings.ToLower(tag)
		if _, ok := seen[key]; !ok {
			seen[key] = struct{}{}
			tags = append(tags, tag)
		}
	}
	sort.Strings(tags)
	return tags
}

func (h *Handler) handleChannelTags(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	tags := h.collectChannelTags(r)
	items := make([]NameIdPair, 0, len(tags))
	for _, t := range tags {
		items = append(items, NameIdPair{Name: t, Id: strings.ToLower(t)})
	}
	page, total := paginate(items, q.Int("StartIndex", 0), q.Int("Limit", 0))
	httpx.JSON(w, http.StatusOK, dto.QueryResult[NameIdPair]{
		Items:            page,
		TotalRecordCount: total,
	})
}

func (h *Handler) handleChannelTagPrefixes(w http.ResponseWriter, r *http.Request) {
	tags := h.collectChannelTags(r)
	seen := make(map[string]struct{})
	prefixes := make([]NameValuePair, 0)
	for _, t := range tags {
		runes := []rune(t)
		if len(runes) == 0 {
			continue
		}
		p := string(unicode.ToUpper(runes[0]))
		if _, ok := seen[p]; !ok {
			seen[p] = struct{}{}
			prefixes = append(prefixes, NameValuePair{Name: p, Value: p})
		}
	}
	httpx.JSON(w, http.StatusOK, prefixes)
}

func (h *Handler) handleDeleteRecordingPost(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "Id")
	h.mu.RLock()
	lt := h.liveTV
	h.mu.RUnlock()
	if lt != nil {
		lt.DeleteRecording(r.Context(), id)
		httpx.NoContent(w)
		return
	}
	var removedPaths []string
	var st sharedLiveTVState
	if h.readConfigJSON(r.Context(), liveTVConfigKey, &st) {
		filtered := make([]liveTVRecording, 0, len(st.Recordings))
		for _, rec := range st.Recordings {
			if rec.Id != id {
				filtered = append(filtered, rec)
			} else if strings.TrimSpace(rec.Path) != "" {
				removedPaths = append(removedPaths, rec.Path)
			}
		}
		st.Recordings = filtered
		if err := h.writeConfigJSON(r.Context(), liveTVConfigKey, st); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "failed to persist recording deletion")
			return
		}
	}
	for _, p := range removedPaths {
		if resolved, ok := h.resolveRecordingPath(p); ok {
			_ = os.Remove(resolved)
		}
	}
	httpx.NoContent(w)
}

func (h *Handler) handleDeleteTimerPost(w http.ResponseWriter, r *http.Request) {
	if !h.hasLiveTvManagement(r) {
		httpx.Error(w, http.StatusForbidden, "Live TV management privileges required")
		return
	}
	id := chi.URLParam(r, "Id")
	h.mu.RLock()
	lt := h.liveTV
	rec := h.dvrRec
	h.mu.RUnlock()
	if lt != nil {
		lt.DeleteTimerByID(r.Context(), id)
		if rec != nil {
			_ = rec.CancelRecording(id)
		}
		httpx.NoContent(w)
		return
	}
	var st sharedLiveTVState
	if h.readConfigJSON(r.Context(), liveTVConfigKey, &st) {
		st.Timers = filterRawByID(st.Timers, id)
		if err := h.writeConfigJSON(r.Context(), liveTVConfigKey, st); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "failed to persist timer deletion")
			return
		}
	}
	if rec != nil {
		_ = rec.CancelRecording(id)
	}
	httpx.NoContent(w)
}

func (h *Handler) handleDeleteSeriesTimerPost(w http.ResponseWriter, r *http.Request) {
	if !h.hasLiveTvManagement(r) {
		httpx.Error(w, http.StatusForbidden, "Live TV management privileges required")
		return
	}
	id := chi.URLParam(r, "Id")
	h.mu.RLock()
	lt := h.liveTV
	h.mu.RUnlock()
	if lt != nil {
		lt.DeleteSeriesTimerByID(r.Context(), id)
		httpx.NoContent(w)
		return
	}
	var st sharedLiveTVState
	if h.readConfigJSON(r.Context(), liveTVConfigKey, &st) {
		st.SeriesTimers = filterRawByID(st.SeriesTimers, id)
		if err := h.writeConfigJSON(r.Context(), liveTVConfigKey, st); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "failed to persist series timer deletion")
			return
		}
	}
	httpx.NoContent(w)
}

func filterRawByID(items []json.RawMessage, removeID string) []json.RawMessage {
	out := make([]json.RawMessage, 0, len(items))
	for _, raw := range items {
		var meta liveTVTimer
		if err := json.Unmarshal(raw, &meta); err == nil && meta.Id == removeID {
			continue
		}
		out = append(out, raw)
	}
	return out
}

func (h *Handler) handleGetTunerHosts(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, h.snapshotLiveTVTunerHosts(r.Context()))
}

func (h *Handler) handleDefaultTunerHost(w http.ResponseWriter, r *http.Request) {
	tunerType := strings.TrimSpace(chi.URLParam(r, "Type"))
	if tunerType == "" {
		tunerType = "m3u"
	}
	httpx.JSON(w, http.StatusOK, liveTVTunerHost{
		Type:               tunerType,
		FriendlyName:       fmt.Sprintf("Default %s Tuner", strings.ToUpper(tunerType)),
		AllowHWTranscoding: true,
		TunerCount:         0,
	})
}

func (h *Handler) handleDeleteTunerHostPost(w http.ResponseWriter, r *http.Request) {
	id := extractIDParam(r)
	h.mu.RLock()
	lt := h.liveTV
	h.mu.RUnlock()
	if lt != nil {
		if id != "" {
			lt.DeleteTunerHost(r.Context(), id)
		}
		httpx.NoContent(w)
		return
	}
	var st sharedLiveTVState
	if id != "" && h.readConfigJSON(r.Context(), liveTVConfigKey, &st) {
		filtered := make([]liveTVTunerHost, 0, len(st.TunerHosts))
		for _, t := range st.TunerHosts {
			if t.Id != id {
				filtered = append(filtered, t)
			}
		}
		st.TunerHosts = filtered
		if err := h.writeConfigJSON(r.Context(), liveTVConfigKey, st); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "failed to persist tuner host deletion")
			return
		}
	}
	httpx.NoContent(w)
}

func (h *Handler) handleListingProviderDefault(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, liveTVListingProvider{
		Type: "xmltv",
	})
}

func (h *Handler) handleAvailableListingProviders(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, []NameIdPair{
		{Name: "XMLTV", Id: "xmltv"},
		{Name: "Schedules Direct", Id: "schedulesdirect"},
		{Name: "Emby Guide Data", Id: "emby"},
	})
}

func (h *Handler) handleGetListingProviders(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, h.snapshotLiveTVListingProviders(r.Context()))
}

func (h *Handler) handleDeleteListingProviderPost(w http.ResponseWriter, r *http.Request) {
	id := extractIDParam(r)
	h.mu.RLock()
	lt := h.liveTV
	h.mu.RUnlock()
	if lt != nil {
		if id != "" {
			lt.DeleteListingProvider(r.Context(), id)
		}
		httpx.NoContent(w)
		return
	}
	var st sharedLiveTVState
	if id != "" && h.readConfigJSON(r.Context(), liveTVConfigKey, &st) {
		filtered := make([]liveTVListingProvider, 0, len(st.ListingProviders))
		for _, lp := range st.ListingProviders {
			if lp.Id != id {
				filtered = append(filtered, lp)
			}
		}
		st.ListingProviders = filtered
		if err := h.writeConfigJSON(r.Context(), liveTVConfigKey, st); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "failed to persist listing provider deletion")
			return
		}
	}
	httpx.NoContent(w)
}

func (h *Handler) handleListingProviderLineups(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, []NameIdPair{})
}

func (h *Handler) handleChannelMappingOptions(w http.ResponseWriter, r *http.Request) {
	if !h.hasLiveTvManagement(r) {
		httpx.Error(w, http.StatusForbidden, "Live TV management privileges required")
		return
	}
	q := httpx.ParseQuery(r)
	providerID := q.Get("ProviderId")

	channels := h.snapshotLiveTVChannels(r.Context())
	programs := h.snapshotLiveTVPrograms(r.Context())

	tunerChannels := make([]map[string]any, 0, len(channels))
	providerSeen := make(map[string]struct{})
	providerChannels := make([]NameIdPair, 0, len(channels))
	for _, ch := range channels {
		tunerChannels = append(tunerChannels, map[string]any{
			"Id":     ch.Id,
			"Name":   ch.Name,
			"Number": ch.Number,
		})
		epgID := strings.TrimSpace(ch.TvgId)
		if epgID == "" {
			epgID = strings.TrimSpace(ch.Id)
		}
		if epgID != "" {
			key := strings.ToLower(epgID)
			if _, ok := providerSeen[key]; !ok {
				providerSeen[key] = struct{}{}
				name := strings.TrimSpace(ch.Name)
				if name == "" {
					name = epgID
				}
				providerChannels = append(providerChannels, NameIdPair{
					Name: name,
					Id:   epgID,
				})
			}
		}
	}
	for _, prog := range programs {
		epgID := strings.TrimSpace(prog.ChannelId)
		if epgID == "" {
			continue
		}
		key := strings.ToLower(epgID)
		if _, ok := providerSeen[key]; !ok {
			providerSeen[key] = struct{}{}
			name := strings.TrimSpace(prog.ChannelName)
			if name == "" {
				name = epgID
			}
			providerChannels = append(providerChannels, NameIdPair{
				Name: name,
				Id:   epgID,
			})
		}
	}
	sort.SliceStable(providerChannels, func(i, j int) bool {
		if providerChannels[i].Name == providerChannels[j].Name {
			return providerChannels[i].Id < providerChannels[j].Id
		}
		return providerChannels[i].Name < providerChannels[j].Name
	})

	h.mu.RLock()
	mappings := append([]ChannelMapping(nil), h.state.ChannelMappings[providerID]...)
	h.mu.RUnlock()
	if mappings == nil {
		mappings = []ChannelMapping{}
	}

	httpx.JSON(w, http.StatusOK, map[string]any{
		"TunerChannels":    tunerChannels,
		"ProviderChannels": providerChannels,
		"Mappings":         mappings,
		"ProviderName":     providerID,
	})
}

func (h *Handler) handleSetChannelMapping(w http.ResponseWriter, r *http.Request) {
	if !h.hasLiveTvManagement(r) {
		httpx.Error(w, http.StatusForbidden, "Live TV management privileges required")
		return
	}
	q := httpx.ParseQuery(r)
	req := struct {
		ProviderId        string `json:"ProviderId"`
		TunerChannelId    string `json:"TunerChannelId"`
		ProviderChannelId string `json:"ProviderChannelId"`
	}{
		ProviderId:        q.Get("ProviderId"),
		TunerChannelId:    q.Get("TunerChannelId"),
		ProviderChannelId: q.Get("ProviderChannelId"),
	}
	if r.Body != nil {
		var body struct {
			ProviderId        string `json:"ProviderId"`
			TunerChannelId    string `json:"TunerChannelId"`
			ProviderChannelId string `json:"ProviderChannelId"`
		}
		if err := httpx.DecodeJSONLimit(r, &body, 0); err == nil {
			if body.ProviderId != "" {
				req.ProviderId = body.ProviderId
			}
			if body.TunerChannelId != "" {
				req.TunerChannelId = body.TunerChannelId
			}
			if body.ProviderChannelId != "" {
				req.ProviderChannelId = body.ProviderChannelId
			}
		}
	}

	mapping := ChannelMapping{
		Name:  req.TunerChannelId,
		Value: req.ProviderChannelId,
	}

	h.mu.Lock()
	h.ensureMapsLocked()
	snapshot := cloneCompatExtState(h.state)
	list := append([]ChannelMapping(nil), h.state.ChannelMappings[req.ProviderId]...)
	updated := false
	for i := range list {
		if list[i].Name == mapping.Name {
			list[i].Value = mapping.Value
			updated = true
			break
		}
	}
	if !updated && mapping.Name != "" {
		list = append(list, mapping)
	}
	h.state.ChannelMappings[req.ProviderId] = list
	err := h.saveStateLocked(r.Context())
	if err != nil {
		h.state = snapshot
	}
	lt := h.liveTV
	h.mu.Unlock()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to persist channel mapping")
		return
	}
	if lt != nil {
		_ = lt.RefreshAllGuides(r.Context())
	}

	httpx.JSON(w, http.StatusOK, map[string]any{
		"TunerChannelId":    req.TunerChannelId,
		"ProviderChannelId": req.ProviderChannelId,
	})
}

func (h *Handler) handleDiscoverTuners(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	probeURL := strings.TrimSpace(q.Get("Url"))
	if probeURL == "" {
		probeURL = strings.TrimSpace(q.Get("url"))
	}
	if probeURL == "" {
		probeURL = strings.TrimSpace(q.Get("Address"))
	}
	h.mu.RLock()
	lt := h.liveTV
	h.mu.RUnlock()
	if lt != nil {
		discovered := lt.DiscoverTuners(r.Context(), probeURL)
		out := make([]liveTVTunerHost, len(discovered))
		for i, t := range discovered {
			out[i] = liveTVTunerHost{
				Id:                   t.Id,
				Url:                  t.Url,
				Type:                 t.Type,
				DeviceId:             t.DeviceId,
				FriendlyName:         t.FriendlyName,
				ImportFavoritesOnly:  t.ImportFavoritesOnly,
				AllowHWTranscoding:   t.AllowHWTranscoding,
				AllowPrivateNetwork:  t.AllowPrivateNetwork,
				TunerCount:           t.TunerCount,
				UserAgent:            t.UserAgent,
				ImportedChannelCount: t.ImportedChannelCount,
			}
		}
		httpx.JSON(w, http.StatusOK, out)
		return
	}
	httpx.JSON(w, http.StatusOK, h.snapshotLiveTVTunerHosts(r.Context()))
}

func extractIDParam(r *http.Request) string {
	q := httpx.ParseQuery(r)
	if id := q.Get("Id"); id != "" {
		return id
	}
	if r.Body != nil {
		var body struct {
			Id string `json:"Id"`
		}
		if err := httpx.DecodeJSONLimit(r, &body, 0); err == nil && body.Id != "" {
			return body.Id
		}
	}
	return ""
}

func resolveLiveRecordingRequestToken(r *http.Request, q url.Values) string {
	for k, vals := range q {
		if (strings.EqualFold(k, "api_key") || strings.EqualFold(k, "X-Emby-Token")) && len(vals) > 0 {
			if tok := strings.TrimSpace(vals[0]); tok != "" {
				return tok
			}
		}
	}
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && strings.TrimSpace(ac.Token) != "" {
		return strings.TrimSpace(ac.Token)
	}
	for _, hdr := range []string{"X-Emby-Token", "X-MediaBrowser-Token"} {
		if tok := strings.TrimSpace(r.Header.Get(hdr)); tok != "" {
			return tok
		}
	}
	for _, hdr := range []string{"Authorization", "X-Emby-Authorization"} {
		raw := strings.TrimSpace(r.Header.Get(hdr))
		if raw == "" {
			continue
		}
		if strings.HasPrefix(strings.ToLower(raw), "bearer ") {
			if tok := strings.TrimSpace(raw[7:]); tok != "" {
				return tok
			}
		}
		lower := strings.ToLower(raw)
		if idx := strings.Index(lower, `token="`); idx >= 0 {
			rest := raw[idx+len(`token="`):]
			if end := strings.IndexByte(rest, '"'); end > 0 {
				return strings.TrimSpace(rest[:end])
			}
		}
	}
	return ""
}
