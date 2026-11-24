package hls

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

// AuthFuncs decouples route registration from the concrete middleware package.
type AuthFuncs struct {
	RequireAuth   func(http.Handler) http.Handler
	RequireAdmin  func(http.Handler) http.Handler
	UserIDFromCtx func(context.Context) string
}

type liveStreamEntry struct {
	LiveStreamID  string
	PlaySessionID string
	ItemID        string
	MediaSource   dto.MediaSourceInfo
}

// Handler serves dynamic HLS playlists/segments, encoding capabilities/defaults, and LiveStreams APIs.
type Handler struct {
	repo *sqlite.LibraryRepo
	mgr  *transcode.Manager
	cfg  *config.Config
	auth AuthFuncs

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
	mux.Handle("GET /Videos/{Id}/hls1/{PlaylistId}/{Segment}", wrapAuth(http.HandlerFunc(h.handleSegment)))

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
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	params := h.buildSessionParams(item, src, q)
	if h.mgr != nil {
		_, _ = h.mgr.RegisterSession(params)
	}

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
		Query:            preserveQuery(r.URL.Query()),
	})

	w.Header().Set("Content-Type", "application/x-mpegURL; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(m3u8))
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
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	params := h.buildSessionParams(item, src, q)
	if h.mgr != nil {
		_, _ = h.mgr.RegisterSession(params)
	}

	m3u8 := transcode.BuildMediaPlaylist(transcode.PlaylistParams{
		ItemID:           item.ID,
		PlaylistID:       "main",
		DurationTicks:    resolveRuntimeTicks(item, src),
		SegmentDuration:  params.SegmentDuration,
		SegmentContainer: params.SegmentContainer,
		Query:            preserveQuery(r.URL.Query()),
	})

	w.Header().Set("Content-Type", "application/x-mpegURL; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(m3u8))
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
		httpx.Error(w, http.StatusBadRequest, err.Error())
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
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	params := h.buildSessionParams(item, src, q)
	params.SegmentContainer = ext

	segPath, err := h.mgr.GetSegment(r.Context(), params, segIdx)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	switch ext {
	case "mp4", "m4s":
		w.Header().Set("Content-Type", "video/mp4")
	default:
		w.Header().Set("Content-Type", "video/mp2t")
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeFile(w, r, segPath)
}

func (h *Handler) handleDeleteActiveEncodings(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	deviceID := q.Get("DeviceId")
	playSessionID := q.Get("PlaySessionId")
	if h.mgr != nil {
		h.mgr.StopByDevice(deviceID, playSessionID)
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
	q := httpx.ParseQuery(r)
	var req liveStreamOpenRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
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

	if req.ItemId != "" && h.repo != nil {
		item, src, err := h.resolveItemAndSource(r.Context(), guid.Normalize(req.ItemId), req.MediaSourceId)
		if err == nil && src != nil {
			msDTO = sqlite.MediaSourceRecordsToDTO([]sqlite.MediaSourceRecord{*src})[0]
			msDTO.Type = "Default"
			if msDTO.Id == "" {
				msDTO.Id = item.ID
			}
		} else if err == nil && item != nil {
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

	h.liveMu.Lock()
	h.liveStreams[liveID] = &liveStreamEntry{
		LiveStreamID:  liveID,
		PlaySessionID: req.PlaySessionId,
		ItemID:        guid.Normalize(req.ItemId),
		MediaSource:   msDTO,
	}
	h.liveMu.Unlock()

	httpx.JSON(w, http.StatusOK, map[string]any{
		"LiveStreamId": liveID,
		"MediaSource":  msDTO,
	})
}

type liveStreamCloseRequest struct {
	LiveStreamId string `json:"LiveStreamId"`
}

func (h *Handler) handleLiveStreamClose(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	liveID := q.Get("LiveStreamId")
	if liveID == "" && r.Body != nil {
		var req liveStreamCloseRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		liveID = req.LiveStreamId
	}

	var playSessionID string
	if liveID != "" {
		h.liveMu.Lock()
		if entry, ok := h.liveStreams[liveID]; ok {
			playSessionID = entry.PlaySessionID
			delete(h.liveStreams, liveID)
		}
		h.liveMu.Unlock()
	}
	if h.mgr != nil && playSessionID != "" {
		h.mgr.StopByDevice("", playSessionID)
	}
	httpx.NoContent(w)
}

func (h *Handler) handleLiveStreamMediaInfo(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	liveID := q.Get("LiveStreamId")
	if liveID == "" && r.Body != nil {
		var req liveStreamCloseRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		liveID = req.LiveStreamId
	}

	h.liveMu.RLock()
	entry, ok := h.liveStreams[liveID]
	h.liveMu.RUnlock()
	if !ok {
		httpx.Error(w, http.StatusNotFound, "live stream not found")
		return
	}
	httpx.JSON(w, http.StatusOK, liveStreamOpenResponse{MediaSource: entry.MediaSource})
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

func (h *Handler) buildSessionParams(item *sqlite.ItemRecord, src *sqlite.MediaSourceRecord, q httpx.Query) transcode.SessionParams {
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
	// Resolve .strm file to its target URL for FFmpeg input
	if strings.EqualFold(filepath.Ext(inputPath), ".strm") {
		if u := readFirstLineURL(inputPath); u != "" {
			inputPath = u
		}
	}

	segDur := q.Int("SegmentLength", transcode.DefaultSegmentDuration)
	if segDur <= 0 {
		segDur = transcode.DefaultSegmentDuration
	}

	videoBitrate := q.Int("VideoBitrate", 0)
	if videoBitrate <= 0 {
		videoBitrate = q.Int("MaxStreamingBitrate", 0)
	}
	audioBitrate := q.Int("AudioBitrate", 192_000)

	var audioIdx *int
	if q.Has("AudioStreamIndex") {
		v := q.Int("AudioStreamIndex", -1)
		if v >= 0 {
			audioIdx = &v
		}
	}

	runtimeTicks := resolveRuntimeTicks(item, src)
	return transcode.SessionParams{
		PlaySessionID:    q.Get("PlaySessionId"),
		DeviceID:         q.Get("DeviceId"),
		ItemID:           item.ID,
		MediaSourceID:    msID,
		InputPath:        inputPath,
		VideoCodec:       q.Get("VideoCodec"),
		AudioCodec:       q.Get("AudioCodec"),
		VideoBitrate:     videoBitrate,
		AudioBitrate:     audioBitrate,
		AudioChannels:    q.Int("TranscodingMaxAudioChannels", 2),
		MaxWidth:         q.Int("MaxWidth", 0),
		MaxHeight:        q.Int("MaxHeight", 0),
		AudioStreamIndex: audioIdx,
		SegmentDuration:  segDur,
		SegmentContainer: q.Get("SegmentContainer"),
		DurationSec:      float64(runtimeTicks) / float64(transcode.TicksPerSecond),
	}
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

func readFirstLineURL(strmPath string) string {
	f, err := os.Open(strmPath)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			return line
		}
	}
	return ""
}

func passthrough(next http.Handler) http.Handler { return next }
