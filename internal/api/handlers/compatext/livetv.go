package compatext

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
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

		// LiveRecordings (7 routes)
		r.Get("/LiveTv/LiveRecordings/{Id}/stream", h.handleLiveRecordingStream)
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

	// Admin LiveTv routes (16 routes)
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
		r.Get("/LiveTv/Tuners/Discover", h.handleDiscoverTuners)
		r.Get("/LiveTv/Tuners/Discvover", h.handleDiscoverTuners) // Emby legacy typo alias
	})
}

func (h *Handler) lookupChannel(ctx context.Context, id string) (liveTVChannel, bool) {
	h.mu.RLock()
	lt := h.liveTV
	h.mu.RUnlock()
	if lt != nil {
		if ch, ok := lt.GetChannelByID(id); ok && ch != nil {
			return liveTVChannel{
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
			}, true
		}
	}
	var st sharedLiveTVState
	if h.readConfigJSON(ctx, liveTVConfigKey, &st) {
		for _, ch := range st.Channels {
			if ch.Id == id {
				return ch, true
			}
		}
	}
	return liveTVChannel{}, false
}

func (h *Handler) lookupRecording(ctx context.Context, id string) (liveTVRecording, bool) {
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
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return strings.HasSuffix(strings.ToLower(strings.TrimSpace(raw)), ".m3u8")
	}
	return strings.HasSuffix(strings.ToLower(u.Path), ".m3u8")
}

func appendRawQuery(targetPath, rawQuery string) string {
	if rawQuery == "" {
		return targetPath
	}
	return targetPath + "?" + rawQuery
}

func (h *Handler) handleLiveStreamFile(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "Id")
	ch, ok := h.lookupChannel(r.Context(), id)
	if !ok || strings.TrimSpace(ch.StreamUrl) == "" {
		httpx.Error(w, http.StatusNotFound, "live stream channel not found")
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
		target := appendRawQuery("/LiveTv/LiveRecordings/"+url.PathEscape(rec.Id)+"/stream", r.URL.RawQuery)
		http.Redirect(w, r, target, http.StatusFound)
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
		_ = json.NewDecoder(r.Body).Decode(&body)
		disabled = body.Disabled
	}

	h.mu.Lock()
	h.ensureMapsLocked()
	h.state.ChannelDisabled[id] = disabled
	h.saveStateLocked(r.Context())
	h.mu.Unlock()

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
			h.writeConfigJSON(r.Context(), liveTVConfigKey, st)
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
			h.writeConfigJSON(r.Context(), liveTVConfigKey, st)
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
		h.writeConfigJSON(r.Context(), liveTVConfigKey, st)
	}
	for _, p := range removedPaths {
		if resolved, ok := h.resolveRecordingPath(p); ok {
			_ = os.Remove(resolved)
		}
	}
	httpx.NoContent(w)
}

func (h *Handler) handleDeleteTimerPost(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "Id")
	h.mu.RLock()
	lt := h.liveTV
	rec := h.dvrRec
	h.mu.RUnlock()
	if lt != nil {
		lt.DeleteTimer(r.Context(), id)
		if rec != nil {
			_ = rec.CancelRecording(id)
		}
		httpx.NoContent(w)
		return
	}
	var st sharedLiveTVState
	if h.readConfigJSON(r.Context(), liveTVConfigKey, &st) {
		st.Timers = filterRawByID(st.Timers, id)
		h.writeConfigJSON(r.Context(), liveTVConfigKey, st)
	}
	if rec != nil {
		_ = rec.CancelRecording(id)
	}
	httpx.NoContent(w)
}

func (h *Handler) handleDeleteSeriesTimerPost(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "Id")
	h.mu.RLock()
	lt := h.liveTV
	h.mu.RUnlock()
	if lt != nil {
		lt.DeleteSeriesTimer(r.Context(), id)
		httpx.NoContent(w)
		return
	}
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
	providerSeen := make(map[string]struct{})
	providerChannels := make([]NameIdPair, 0, len(st.Channels))
	for _, ch := range st.Channels {
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
	for _, prog := range st.Programs {
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
