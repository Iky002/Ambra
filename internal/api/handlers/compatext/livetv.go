package compatext

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/httpx"
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

// minimal 188-byte MPEG-TS null packet (sync 0x47, PID 0x1FFF).
var tsNullPacket = func() []byte {
	b := make([]byte, 188)
	b[0] = 0x47
	b[1] = 0x1F
	b[2] = 0xFF
	b[3] = 0x10
	return b
}()

func (h *Handler) registerLiveTVRoutes(r chi.Router, wrapAuth, wrapAdmin func(http.Handler) http.Handler) {
	// Authenticated LiveTv routes
	r.Group(func(r chi.Router) {
		r.Use(wrapAuth)

		// LiveStreamFiles (11 routes)
		r.Get("/LiveTv/LiveStreamFiles/{Id}/stream.{Container}", h.handleLiveStreamFile)
		r.Get("/LiveTv/LiveStreamFiles/{Id}/hls/master.m3u8", h.handleHLSMaster(false))
		r.Head("/LiveTv/LiveStreamFiles/{Id}/hls/master.m3u8", h.handleHLSMaster(true))
		r.Get("/LiveTv/LiveStreamFiles/{Id}/hls/master_local.m3u8", h.handleHLSMaster(false))
		r.Head("/LiveTv/LiveStreamFiles/{Id}/hls/master_local.m3u8", h.handleHLSMaster(true))
		r.Get("/LiveTv/LiveStreamFiles/{Id}/hls/live.m3u8", h.handleHLSLive(false))
		r.Head("/LiveTv/LiveStreamFiles/{Id}/hls/live.m3u8", h.handleHLSLive(true))
		r.Get("/LiveTv/LiveStreamFiles/{Id}/hls/live_local.m3u8", h.handleHLSLive(false))
		r.Head("/LiveTv/LiveStreamFiles/{Id}/hls/live_local.m3u8", h.handleHLSLive(true))
		r.Get("/LiveTv/LiveStreamFiles/{Id}/hls/{Segment}", h.handleHLSSegment(false))
		r.Head("/LiveTv/LiveStreamFiles/{Id}/hls/{Segment}", h.handleHLSSegment(true))

		// LiveRecordings (7 routes)
		r.Get("/LiveTv/LiveRecordings/{Id}/stream", h.handleLiveRecordingStream)
		r.Get("/LiveTv/LiveRecordings/{Id}/hls/master.m3u8", h.handleHLSMaster(false))
		r.Head("/LiveTv/LiveRecordings/{Id}/hls/master.m3u8", h.handleHLSMaster(true))
		r.Get("/LiveTv/LiveRecordings/{Id}/hls/live.m3u8", h.handleHLSLive(false))
		r.Head("/LiveTv/LiveRecordings/{Id}/hls/live.m3u8", h.handleHLSLive(true))
		r.Get("/LiveTv/LiveRecordings/{Id}/hls/{Segment}", h.handleHLSSegment(false))
		r.Head("/LiveTv/LiveRecordings/{Id}/hls/{Segment}", h.handleHLSSegment(true))

		// LiveTv general authenticated endpoints (10 routes)
		r.Get("/LiveTv/AvailableRecordingOptions", h.handleAvailableRecordingOptions)
		r.Get("/LiveTv/EPG", h.handleEPG)
		r.Get("/LiveTv/Folder", h.handleLiveTVFolder)
		r.Get("/LiveTv/ChannelTags", h.handleChannelTags)
		r.Get("/LiveTv/ChannelTags/Prefixes", h.handleChannelTagPrefixes)
		r.Post("/LiveTv/Timers/{Id}/Delete", h.handleDeleteTimerPost)
		r.Post("/LiveTv/SeriesTimers/{Id}/Delete", h.handleDeleteSeriesTimerPost)
		r.Get("/LiveTv/ListingProviders/Default", h.handleListingProviderDefault)
		r.Get("/LiveTv/Tuners/Discover", h.handleDiscoverTuners)
		r.Get("/LiveTv/Tuners/Discvover", h.handleDiscoverTuners) // Emby legacy typo alias
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
		r.Get("/LiveTv/ChannelMappingOptions", h.handleChannelMappingOptions)
		r.Post("/LiveTv/ChannelMappings", h.handleSetChannelMapping)
	})
}

func (h *Handler) handleLiveStreamFile(w http.ResponseWriter, r *http.Request) {
	container := strings.ToLower(chi.URLParam(r, "Container"))
	w.Header().Set("Content-Type", containerContentType(container))
	w.Header().Set("Content-Length", strconv.Itoa(len(tsNullPacket)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(tsNullPacket)
}

func (h *Handler) handleLiveRecordingStream(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "Id")
	var st sharedLiveTVState
	if h.readConfigJSON(r.Context(), liveTVConfigKey, &st) {
		for _, rec := range st.Recordings {
			if rec.Id == id && rec.Path != "" {
				http.ServeFile(w, r, rec.Path)
				return
			}
		}
	}
	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Content-Length", strconv.Itoa(len(tsNullPacket)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(tsNullPacket)
}

func (h *Handler) handleHLSMaster(headOnly bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		playlist := "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-STREAM-INF:BANDWIDTH=2000000,RESOLUTION=1280x720\nlive.m3u8\n"
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Content-Length", strconv.Itoa(len(playlist)))
		w.WriteHeader(http.StatusOK)
		if !headOnly {
			_, _ = w.Write([]byte(playlist))
		}
	}
}

func (h *Handler) handleHLSLive(headOnly bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		playlist := "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:6\n#EXT-X-MEDIA-SEQUENCE:0\n#EXTINF:6.000,\n0.ts\n"
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Content-Length", strconv.Itoa(len(playlist)))
		w.WriteHeader(http.StatusOK)
		if !headOnly {
			_, _ = w.Write([]byte(playlist))
		}
	}
}

func (h *Handler) handleHLSSegment(headOnly bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		seg := strings.ToLower(chi.URLParam(r, "Segment"))
		ct := "video/mp2t"
		if strings.HasSuffix(seg, ".m4s") || strings.HasSuffix(seg, ".mp4") {
			ct = "video/iso.segment"
		}
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Content-Length", strconv.Itoa(len(tsNullPacket)))
		w.WriteHeader(http.StatusOK)
		if !headOnly {
			_, _ = w.Write(tsNullPacket)
		}
	}
}

func containerContentType(container string) string {
	switch container {
	case "mp4", "m4v":
		return "video/mp4"
	case "ts", "m2ts":
		return "video/mp2t"
	case "mkv":
		return "video/x-matroska"
	case "webm":
		return "video/webm"
	case "m3u8":
		return "application/vnd.apple.mpegurl"
	default:
		return "application/octet-stream"
	}
}

func (h *Handler) handleAvailableRecordingOptions(w http.ResponseWriter, r *http.Request) {
	baseDir := ""
	if h.cfg != nil && h.cfg.DataDir != "" {
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

	var st sharedLiveTVState
	_ = h.readConfigJSON(r.Context(), liveTVConfigKey, &st)

	items := make([]liveTVProgram, 0, len(st.Programs))
	for _, p := range st.Programs {
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
	var st sharedLiveTVState
	_ = h.readConfigJSON(r.Context(), liveTVConfigKey, &st)

	h.mu.RLock()
	out := make([]ChannelManagementInfo, 0, len(st.Channels))
	for i, ch := range st.Channels {
		sortIdx := i
		if v, ok := h.state.ChannelSortIndex[ch.Id]; ok {
			sortIdx = v
		}
		out = append(out, ChannelManagementInfo{
			Id:        ch.Id,
			Name:      ch.Name,
			Number:    ch.Number,
			Disabled:  h.state.ChannelDisabled[ch.Id],
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
		_ = json.NewDecoder(r.Body).Decode(&body)
		disabled = body.Disabled
	}

	h.mu.Lock()
	h.ensureMapsLocked()
	h.state.ChannelDisabled[id] = disabled
	h.saveStateLocked(r.Context())
	h.mu.Unlock()
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
			if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
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
	h.state.ChannelSortIndex[id] = newIdx
	h.saveStateLocked(r.Context())
	h.mu.Unlock()
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
	h.state.TunerResetAt[id] = time.Now().UTC().Unix()
	h.saveStateLocked(r.Context())
	h.mu.Unlock()
	httpx.NoContent(w)
}

func (h *Handler) collectChannelTags(r *http.Request) []string {
	var st sharedLiveTVState
	_ = h.readConfigJSON(r.Context(), liveTVConfigKey, &st)
	seen := make(map[string]struct{})
	var tags []string
	for _, ch := range st.Channels {
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
	var st sharedLiveTVState
	if h.readConfigJSON(r.Context(), liveTVConfigKey, &st) {
		filtered := make([]liveTVRecording, 0, len(st.Recordings))
		for _, rec := range st.Recordings {
			if rec.Id != id {
				filtered = append(filtered, rec)
			}
		}
		st.Recordings = filtered
		h.writeConfigJSON(r.Context(), liveTVConfigKey, st)
	}
	httpx.NoContent(w)
}

func (h *Handler) handleDeleteTimerPost(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "Id")
	var st sharedLiveTVState
	if h.readConfigJSON(r.Context(), liveTVConfigKey, &st) {
		st.Timers = filterRawByID(st.Timers, id)
		h.writeConfigJSON(r.Context(), liveTVConfigKey, st)
	}
	httpx.NoContent(w)
}

func (h *Handler) handleDeleteSeriesTimerPost(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "Id")
	var st sharedLiveTVState
	if h.readConfigJSON(r.Context(), liveTVConfigKey, &st) {
		st.SeriesTimers = filterRawByID(st.SeriesTimers, id)
		h.writeConfigJSON(r.Context(), liveTVConfigKey, st)
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
	var st sharedLiveTVState
	_ = h.readConfigJSON(r.Context(), liveTVConfigKey, &st)
	if st.TunerHosts == nil {
		st.TunerHosts = []liveTVTunerHost{}
	}
	httpx.JSON(w, http.StatusOK, st.TunerHosts)
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
	var st sharedLiveTVState
	if id != "" && h.readConfigJSON(r.Context(), liveTVConfigKey, &st) {
		filtered := make([]liveTVTunerHost, 0, len(st.TunerHosts))
		for _, t := range st.TunerHosts {
			if t.Id != id {
				filtered = append(filtered, t)
			}
		}
		st.TunerHosts = filtered
		h.writeConfigJSON(r.Context(), liveTVConfigKey, st)
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
	var st sharedLiveTVState
	_ = h.readConfigJSON(r.Context(), liveTVConfigKey, &st)
	if st.ListingProviders == nil {
		st.ListingProviders = []liveTVListingProvider{}
	}
	httpx.JSON(w, http.StatusOK, st.ListingProviders)
}

func (h *Handler) handleDeleteListingProviderPost(w http.ResponseWriter, r *http.Request) {
	id := extractIDParam(r)
	var st sharedLiveTVState
	if id != "" && h.readConfigJSON(r.Context(), liveTVConfigKey, &st) {
		filtered := make([]liveTVListingProvider, 0, len(st.ListingProviders))
		for _, lp := range st.ListingProviders {
			if lp.Id != id {
				filtered = append(filtered, lp)
			}
		}
		st.ListingProviders = filtered
		h.writeConfigJSON(r.Context(), liveTVConfigKey, st)
	}
	httpx.NoContent(w)
}

func (h *Handler) handleListingProviderLineups(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, []NameIdPair{})
}

func (h *Handler) handleChannelMappingOptions(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	providerID := q.Get("ProviderId")

	var st sharedLiveTVState
	_ = h.readConfigJSON(r.Context(), liveTVConfigKey, &st)

	tunerChannels := make([]map[string]any, 0, len(st.Channels))
	for _, ch := range st.Channels {
		tunerChannels = append(tunerChannels, map[string]any{
			"Id":     ch.Id,
			"Name":   ch.Name,
			"Number": ch.Number,
		})
	}

	h.mu.RLock()
	mappings := append([]ChannelMapping(nil), h.state.ChannelMappings[providerID]...)
	h.mu.RUnlock()
	if mappings == nil {
		mappings = []ChannelMapping{}
	}

	httpx.JSON(w, http.StatusOK, map[string]any{
		"TunerChannels":    tunerChannels,
		"ProviderChannels": []NameIdPair{},
		"Mappings":         mappings,
		"ProviderName":     providerID,
	})
}

func (h *Handler) handleSetChannelMapping(w http.ResponseWriter, r *http.Request) {
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
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
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
	list := h.state.ChannelMappings[req.ProviderId]
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
	h.saveStateLocked(r.Context())
	h.mu.Unlock()

	httpx.JSON(w, http.StatusOK, map[string]any{
		"TunerChannelId":    req.TunerChannelId,
		"ProviderChannelId": req.ProviderChannelId,
	})
}

func (h *Handler) handleDiscoverTuners(w http.ResponseWriter, r *http.Request) {
	var st sharedLiveTVState
	_ = h.readConfigJSON(r.Context(), liveTVConfigKey, &st)
	if st.TunerHosts == nil {
		st.TunerHosts = []liveTVTunerHost{}
	}
	httpx.JSON(w, http.StatusOK, st.TunerHosts)
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
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil && body.Id != "" {
			return body.Id
		}
	}
	return ""
}
