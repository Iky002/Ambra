package livetv

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/pkg/netutil"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

const stateConfigKey = "livetv:state"

// AuthFuncs decouples route registration from the concrete middleware package.
type AuthFuncs struct {
	RequireAuth   func(http.Handler) http.Handler
	RequireAdmin  func(http.Handler) http.Handler
	UserIDFromCtx func(context.Context) string
}

type LiveTvServiceInfo struct {
	Name    string   `json:"Name"`
	Status  string   `json:"Status"`
	Version string   `json:"Version"`
	Tuners  []string `json:"Tuners"`
}

type LiveTvInfo struct {
	Services     []LiveTvServiceInfo `json:"Services"`
	IsEnabled    bool                `json:"IsEnabled"`
	EnabledUsers []string            `json:"EnabledUsers"`
}

type ChannelInfo struct {
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

type ProgramInfo struct {
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

type RecordingInfo struct {
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

type TimerInfo struct {
	Id                    string    `json:"Id"`
	ServerId              string    `json:"ServerId,omitempty"`
	ChannelId             string    `json:"ChannelId"`
	ChannelName           string    `json:"ChannelName,omitempty"`
	ProgramId             string    `json:"ProgramId,omitempty"`
	SeriesTimerId         string    `json:"SeriesTimerId,omitempty"`
	Name                  string    `json:"Name"`
	Overview              string    `json:"Overview,omitempty"`
	StartDate             time.Time `json:"StartDate"`
	EndDate               time.Time `json:"EndDate"`
	Status                string    `json:"Status"`
	PrePaddingSeconds     int       `json:"PrePaddingSeconds"`
	PostPaddingSeconds    int       `json:"PostPaddingSeconds"`
	IsPrePaddingRequired  bool      `json:"IsPrePaddingRequired"`
	IsPostPaddingRequired bool      `json:"IsPostPaddingRequired"`
}

type SeriesTimerInfo struct {
	Id                    string   `json:"Id"`
	ServerId              string   `json:"ServerId,omitempty"`
	ChannelId             string   `json:"ChannelId,omitempty"`
	Name                  string   `json:"Name"`
	Overview              string   `json:"Overview,omitempty"`
	RecordAnyTime         bool     `json:"RecordAnyTime"`
	RecordAnyChannel      bool     `json:"RecordAnyChannel"`
	RecordNewOnly         bool     `json:"RecordNewOnly"`
	Days                  []string `json:"Days"`
	PrePaddingSeconds     int      `json:"PrePaddingSeconds"`
	PostPaddingSeconds    int      `json:"PostPaddingSeconds"`
	IsPrePaddingRequired  bool     `json:"IsPrePaddingRequired"`
	IsPostPaddingRequired bool     `json:"IsPostPaddingRequired"`
}

type TunerHostInfo struct {
	Id                      string `json:"Id"`
	Url                     string `json:"Url,omitempty"`
	Type                    string `json:"Type"`
	DeviceId                string `json:"DeviceId,omitempty"`
	FriendlyName            string `json:"FriendlyName,omitempty"`
	ImportFavoritesOnly     bool   `json:"ImportFavoritesOnly,omitempty"`
	AllowHWTranscoding      bool   `json:"AllowHWTranscoding,omitempty"`
	TunerCount              int    `json:"TunerCount,omitempty"`
	UserAgent               string `json:"UserAgent,omitempty"`
	M3UContent              string `json:"M3UContent,omitempty"`
	ImportedChannelCount    int    `json:"ImportedChannelCount,omitempty"`
}

type ListingProviderInfo struct {
	Id       string `json:"Id"`
	Type     string `json:"Type"`
	Path     string `json:"Path,omitempty"`
	Username string `json:"Username,omitempty"`
	Listings string `json:"ListingsId,omitempty"`
	Country  string `json:"Country,omitempty"`
}

type GuideInfo struct {
	StartDate time.Time `json:"StartDate"`
	EndDate   time.Time `json:"EndDate"`
}

type NameIdPair struct {
	Name string `json:"Name"`
	Id   string `json:"Id"`
}

type liveTVState struct {
	Channels         []ChannelInfo         `json:"channels"`
	Programs         []ProgramInfo         `json:"programs"`
	Recordings       []RecordingInfo       `json:"recordings"`
	Timers           []TimerInfo           `json:"timers"`
	SeriesTimers     []SeriesTimerInfo     `json:"series_timers"`
	TunerHosts       []TunerHostInfo       `json:"tuner_hosts"`
	ListingProviders []ListingProviderInfo `json:"listing_providers"`
}

type Handler struct {
	store            *sqlite.Store
	cfg              *config.Config
	httpClient       *http.Client
	customHTTPClient bool
	auth             AuthFuncs

	mu    sync.RWMutex
	state liveTVState
}

func New(store *sqlite.Store, cfg *config.Config) *Handler {
	h := &Handler{
		store:      store,
		cfg:        cfg,
		httpClient: netutil.NewSafeHTTPClient(15*time.Second, false),
	}
	h.loadState(context.Background())
	return h
}

// SetHTTPClient overrides the HTTP client used for remote M3U playlist fetching (primarily for testing).
func (h *Handler) SetHTTPClient(client *http.Client) {
	if client != nil {
		h.httpClient = client
		h.customHTTPClient = true
	}
}

// AddRecording inserts a recording record (useful for tests and internal DVR integration).
func (h *Handler) AddRecording(ctx context.Context, rec RecordingInfo) RecordingInfo {
	h.mu.Lock()
	if rec.Id == "" {
		rec.Id = guid.New()
	}
	if rec.Type == "" {
		rec.Type = "Recording"
	}
	if rec.ServerId == "" {
		rec.ServerId = h.serverID()
	}
	if rec.Status == "" {
		rec.Status = "Completed"
	}
	h.state.Recordings = append(h.state.Recordings, rec)
	h.saveStateLocked(ctx)
	h.mu.Unlock()
	return rec
}

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

	handleAuth := func(pattern string, fn http.HandlerFunc) {
		mux.Handle(pattern, wrapAuth(fn))
	}
	handleAdmin := func(pattern string, fn http.HandlerFunc) {
		mux.Handle(pattern, wrapAdmin(fn))
	}

	// Info, Channels, Programs
	handleAuth("GET /LiveTv/Info", h.handleInfo)
	handleAuth("GET /LiveTv/Channels", h.handleListChannels)
	handleAuth("GET /LiveTv/Channels/{Id}", h.handleGetChannel)
	handleAuth("GET /LiveTv/Programs", h.handleListPrograms)
	handleAuth("POST /LiveTv/Programs", h.handleQueryProgramsPost)
	handleAuth("GET /LiveTv/Programs/Recommended", h.handleRecommendedPrograms)
	handleAuth("GET /LiveTv/Programs/{Id}", h.handleGetProgram)

	// Recordings
	handleAuth("GET /LiveTv/Recordings", h.handleListRecordings)
	handleAuth("GET /LiveTv/Recordings/Folders", h.handleRecordingFolders)
	handleAuth("GET /LiveTv/Recordings/{Id}", h.handleGetRecording)
	handleAdmin("DELETE /LiveTv/Recordings/{Id}", h.handleDeleteRecording)

	// Timers
	handleAuth("GET /LiveTv/Timers", h.handleListTimers)
	handleAuth("POST /LiveTv/Timers", h.handleCreateTimer)
	handleAuth("GET /LiveTv/Timers/Defaults", h.handleTimerDefaults)
	handleAuth("GET /LiveTv/Timers/{Id}", h.handleGetTimer)
	handleAuth("POST /LiveTv/Timers/{Id}", h.handleUpdateTimer)
	handleAuth("DELETE /LiveTv/Timers/{Id}", h.handleDeleteTimer)

	// SeriesTimers
	handleAuth("GET /LiveTv/SeriesTimers", h.handleListSeriesTimers)
	handleAuth("POST /LiveTv/SeriesTimers", h.handleCreateSeriesTimer)
	handleAuth("GET /LiveTv/SeriesTimers/{Id}", h.handleGetSeriesTimer)
	handleAuth("POST /LiveTv/SeriesTimers/{Id}", h.handleUpdateSeriesTimer)
	handleAuth("DELETE /LiveTv/SeriesTimers/{Id}", h.handleDeleteSeriesTimer)

	// Tuners, ListingProviders & GuideInfo
	handleAdmin("POST /LiveTv/TunerHosts", h.handleSaveTunerHost)
	handleAdmin("DELETE /LiveTv/TunerHosts", h.handleDeleteTunerHost)
	handleAuth("GET /LiveTv/TunerHosts/Types", h.handleTunerHostTypes)
	handleAdmin("POST /LiveTv/ListingProviders", h.handleSaveListingProvider)
	handleAdmin("DELETE /LiveTv/ListingProviders", h.handleDeleteListingProvider)
	handleAuth("GET /LiveTv/GuideInfo", h.handleGuideInfo)
}

func (h *Handler) serverID() string {
	if h.cfg != nil {
		return h.cfg.ServerID
	}
	return ""
}

func (h *Handler) handleInfo(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	tuners := make([]string, 0, len(h.state.TunerHosts))
	for _, t := range h.state.TunerHosts {
		label := t.FriendlyName
		if label == "" {
			label = t.Type
		}
		tuners = append(tuners, label)
	}
	enabled := len(h.state.TunerHosts) > 0 || len(h.state.Channels) > 0
	h.mu.RUnlock()

	httpx.JSON(w, http.StatusOK, LiveTvInfo{
		Services: []LiveTvServiceInfo{
			{
				Name:    "Ambra Live TV",
				Status:  "Ok",
				Version: config.ServerVersion,
				Tuners:  tuners,
			},
		},
		IsEnabled:    enabled,
		EnabledUsers: []string{},
	})
}

func (h *Handler) handleListChannels(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	search := strings.ToLower(strings.TrimSpace(q.Get("SearchTerm")))
	group := strings.ToLower(strings.TrimSpace(q.Get("GroupTitle")))
	chType := strings.ToLower(strings.TrimSpace(q.Get("Type")))

	h.mu.RLock()
	filtered := make([]ChannelInfo, 0, len(h.state.Channels))
	for _, ch := range h.state.Channels {
		if search != "" && !strings.Contains(strings.ToLower(ch.Name), search) && !strings.Contains(strings.ToLower(ch.Number), search) {
			continue
		}
		if group != "" && strings.ToLower(ch.GroupTitle) != group {
			continue
		}
		if chType != "" && strings.ToLower(ch.ChannelType) != chType {
			continue
		}
		filtered = append(filtered, ch)
	}
	h.mu.RUnlock()

	page, total := paginate(filtered, q.Int("StartIndex", 0), q.Int("Limit", 0))
	httpx.JSON(w, http.StatusOK, dto.QueryResult[ChannelInfo]{
		Items:            page,
		TotalRecordCount: total,
	})
}

func (h *Handler) handleGetChannel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("Id")
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, ch := range h.state.Channels {
		if ch.Id == id {
			httpx.JSON(w, http.StatusOK, ch)
			return
		}
	}
	httpx.Error(w, http.StatusNotFound, "channel not found")
}

func (h *Handler) handleListPrograms(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	h.writeFilteredPrograms(w, q.List("ChannelIds"), q.Get("SearchTerm"), q.Bool("IsMovie"), q.Bool("IsSeries"), q.Int("StartIndex", 0), q.Int("Limit", 0))
}

func (h *Handler) handleQueryProgramsPost(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ChannelIds []string `json:"ChannelIds"`
		SearchTerm string   `json:"SearchTerm"`
		IsMovie    *bool    `json:"IsMovie"`
		IsSeries   *bool    `json:"IsSeries"`
		StartIndex int      `json:"StartIndex"`
		Limit      int      `json:"Limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid program query payload")
		return
	}
	h.writeFilteredPrograms(w, body.ChannelIds, body.SearchTerm, body.IsMovie, body.IsSeries, body.StartIndex, body.Limit)
}

func (h *Handler) writeFilteredPrograms(w http.ResponseWriter, channelIDs []string, searchTerm string, isMovie, isSeries *bool, startIndex, limit int) {
	chSet := make(map[string]struct{}, len(channelIDs))
	for _, cid := range channelIDs {
		chSet[cid] = struct{}{}
	}
	search := strings.ToLower(strings.TrimSpace(searchTerm))

	h.mu.RLock()
	filtered := make([]ProgramInfo, 0, len(h.state.Programs))
	for _, p := range h.state.Programs {
		if len(chSet) > 0 {
			if _, ok := chSet[p.ChannelId]; !ok {
				continue
			}
		}
		if search != "" && !strings.Contains(strings.ToLower(p.Name), search) {
			continue
		}
		if isMovie != nil && p.IsMovie != *isMovie {
			continue
		}
		if isSeries != nil && p.IsSeries != *isSeries {
			continue
		}
		filtered = append(filtered, p)
	}
	h.mu.RUnlock()

	page, total := paginate(filtered, startIndex, limit)
	httpx.JSON(w, http.StatusOK, dto.QueryResult[ProgramInfo]{
		Items:            page,
		TotalRecordCount: total,
	})
}

func (h *Handler) handleRecommendedPrograms(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	now := time.Now().UTC()

	h.mu.RLock()
	rec := make([]ProgramInfo, 0, len(h.state.Programs))
	for _, p := range h.state.Programs {
		if p.EndDate.IsZero() || p.EndDate.After(now) {
			rec = append(rec, p)
		}
	}
	h.mu.RUnlock()

	page, total := paginate(rec, q.Int("StartIndex", 0), q.Int("Limit", 24))
	httpx.JSON(w, http.StatusOK, dto.QueryResult[ProgramInfo]{
		Items:            page,
		TotalRecordCount: total,
	})
}

func (h *Handler) handleGetProgram(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("Id")
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, p := range h.state.Programs {
		if p.Id == id {
			httpx.JSON(w, http.StatusOK, p)
			return
		}
	}
	httpx.Error(w, http.StatusNotFound, "program not found")
}

func (h *Handler) handleListRecordings(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	chID := q.Get("ChannelId")
	status := strings.ToLower(q.Get("Status"))

	h.mu.RLock()
	filtered := make([]RecordingInfo, 0, len(h.state.Recordings))
	for _, rec := range h.state.Recordings {
		if chID != "" && rec.ChannelId != chID {
			continue
		}
		if status != "" && strings.ToLower(rec.Status) != status {
			continue
		}
		filtered = append(filtered, rec)
	}
	h.mu.RUnlock()

	page, total := paginate(filtered, q.Int("StartIndex", 0), q.Int("Limit", 0))
	httpx.JSON(w, http.StatusOK, dto.QueryResult[RecordingInfo]{
		Items:            page,
		TotalRecordCount: total,
	})
}

func (h *Handler) handleGetRecording(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("Id")
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, rec := range h.state.Recordings {
		if rec.Id == id {
			httpx.JSON(w, http.StatusOK, rec)
			return
		}
	}
	httpx.Error(w, http.StatusNotFound, "recording not found")
}

func (h *Handler) handleDeleteRecording(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("Id")
	h.mu.Lock()
	idx := -1
	for i, rec := range h.state.Recordings {
		if rec.Id == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		h.mu.Unlock()
		httpx.Error(w, http.StatusNotFound, "recording not found")
		return
	}
	h.state.Recordings = append(h.state.Recordings[:idx], h.state.Recordings[idx+1:]...)
	h.saveStateLocked(r.Context())
	h.mu.Unlock()
	httpx.NoContent(w)
}

func (h *Handler) handleRecordingFolders(w http.ResponseWriter, r *http.Request) {
	folders := []dto.BaseItemDto{
		{
			Id:                "livetv-recordings",
			Name:              "Recordings",
			ServerId:          h.serverID(),
			Type:              "Folder",
			CollectionType:    "livetv",
			IsFolder:          true,
			ImageTags:         map[string]string{},
			BackdropImageTags: []string{},
		},
	}
	httpx.JSON(w, http.StatusOK, dto.QueryResult[dto.BaseItemDto]{
		Items:            folders,
		TotalRecordCount: len(folders),
	})
}

func (h *Handler) handleListTimers(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	chID := q.Get("ChannelId")
	stID := q.Get("SeriesTimerId")

	h.mu.RLock()
	filtered := make([]TimerInfo, 0, len(h.state.Timers))
	for _, t := range h.state.Timers {
		if chID != "" && t.ChannelId != chID {
			continue
		}
		if stID != "" && t.SeriesTimerId != stID {
			continue
		}
		filtered = append(filtered, t)
	}
	h.mu.RUnlock()

	httpx.JSON(w, http.StatusOK, dto.QueryResult[TimerInfo]{
		Items:            filtered,
		TotalRecordCount: len(filtered),
	})
}

func (h *Handler) handleCreateTimer(w http.ResponseWriter, r *http.Request) {
	var t TimerInfo
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid timer payload")
		return
	}
	if t.Id == "" {
		t.Id = guid.New()
	}
	t.ServerId = h.serverID()
	if t.Status == "" {
		t.Status = "New"
	}
	if t.StartDate.IsZero() {
		t.StartDate = time.Now().UTC()
	}
	if t.EndDate.IsZero() {
		t.EndDate = t.StartDate.Add(time.Hour)
	}

	h.mu.Lock()
	h.state.Timers = append(h.state.Timers, t)
	h.saveStateLocked(r.Context())
	h.mu.Unlock()
	httpx.JSON(w, http.StatusOK, t)
}

func (h *Handler) handleGetTimer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("Id")
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, t := range h.state.Timers {
		if t.Id == id {
			httpx.JSON(w, http.StatusOK, t)
			return
		}
	}
	httpx.Error(w, http.StatusNotFound, "timer not found")
}

func (h *Handler) handleUpdateTimer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("Id")
	var upd TimerInfo
	if err := json.NewDecoder(r.Body).Decode(&upd); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid timer payload")
		return
	}
	h.mu.Lock()
	for i, existing := range h.state.Timers {
		if existing.Id == id {
			upd.Id = id
			upd.ServerId = h.serverID()
			if upd.Status == "" {
				upd.Status = existing.Status
			}
			h.state.Timers[i] = upd
			h.saveStateLocked(r.Context())
			h.mu.Unlock()
			httpx.JSON(w, http.StatusOK, upd)
			return
		}
	}
	h.mu.Unlock()
	httpx.Error(w, http.StatusNotFound, "timer not found")
}

func (h *Handler) handleDeleteTimer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("Id")
	h.mu.Lock()
	for i, t := range h.state.Timers {
		if t.Id == id {
			h.state.Timers = append(h.state.Timers[:i], h.state.Timers[i+1:]...)
			h.saveStateLocked(r.Context())
			h.mu.Unlock()
			httpx.NoContent(w)
			return
		}
	}
	h.mu.Unlock()
	httpx.Error(w, http.StatusNotFound, "timer not found")
}

func (h *Handler) handleTimerDefaults(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	programID := q.Get("programId")
	now := time.Now().UTC()
	def := TimerInfo{
		ServerId:           h.serverID(),
		Status:             "New",
		StartDate:          now,
		EndDate:            now.Add(time.Hour),
		PrePaddingSeconds:  120,
		PostPaddingSeconds: 300,
	}
	if programID != "" {
		h.mu.RLock()
		for _, p := range h.state.Programs {
			if p.Id == programID {
				def.ProgramId = p.Id
				def.ChannelId = p.ChannelId
				def.ChannelName = p.ChannelName
				def.Name = p.Name
				def.Overview = p.Overview
				def.StartDate = p.StartDate
				def.EndDate = p.EndDate
				break
			}
		}
		h.mu.RUnlock()
	}
	httpx.JSON(w, http.StatusOK, def)
}

func (h *Handler) handleListSeriesTimers(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	items := append([]SeriesTimerInfo(nil), h.state.SeriesTimers...)
	h.mu.RUnlock()
	if items == nil {
		items = []SeriesTimerInfo{}
	}
	httpx.JSON(w, http.StatusOK, dto.QueryResult[SeriesTimerInfo]{
		Items:            items,
		TotalRecordCount: len(items),
	})
}

func (h *Handler) handleCreateSeriesTimer(w http.ResponseWriter, r *http.Request) {
	var st SeriesTimerInfo
	if err := json.NewDecoder(r.Body).Decode(&st); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid series timer payload")
		return
	}
	if st.Id == "" {
		st.Id = guid.New()
	}
	st.ServerId = h.serverID()
	if st.Days == nil {
		st.Days = []string{}
	}

	h.mu.Lock()
	h.state.SeriesTimers = append(h.state.SeriesTimers, st)
	h.saveStateLocked(r.Context())
	h.mu.Unlock()
	httpx.JSON(w, http.StatusOK, st)
}

func (h *Handler) handleGetSeriesTimer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("Id")
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, st := range h.state.SeriesTimers {
		if st.Id == id {
			httpx.JSON(w, http.StatusOK, st)
			return
		}
	}
	httpx.Error(w, http.StatusNotFound, "series timer not found")
}

func (h *Handler) handleUpdateSeriesTimer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("Id")
	var upd SeriesTimerInfo
	if err := json.NewDecoder(r.Body).Decode(&upd); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid series timer payload")
		return
	}
	h.mu.Lock()
	for i, existing := range h.state.SeriesTimers {
		if existing.Id == id {
			upd.Id = id
			upd.ServerId = h.serverID()
			if upd.Days == nil {
				upd.Days = []string{}
			}
			h.state.SeriesTimers[i] = upd
			h.saveStateLocked(r.Context())
			h.mu.Unlock()
			httpx.JSON(w, http.StatusOK, upd)
			return
		}
	}
	h.mu.Unlock()
	httpx.Error(w, http.StatusNotFound, "series timer not found")
}

func (h *Handler) handleDeleteSeriesTimer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("Id")
	h.mu.Lock()
	for i, st := range h.state.SeriesTimers {
		if st.Id == id {
			h.state.SeriesTimers = append(h.state.SeriesTimers[:i], h.state.SeriesTimers[i+1:]...)
			h.saveStateLocked(r.Context())
			h.mu.Unlock()
			httpx.NoContent(w)
			return
		}
	}
	h.mu.Unlock()
	httpx.Error(w, http.StatusNotFound, "series timer not found")
}

func (h *Handler) handleSaveTunerHost(w http.ResponseWriter, r *http.Request) {
	var host TunerHostInfo
	if err := json.NewDecoder(r.Body).Decode(&host); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid tuner host payload")
		return
	}
	if host.Type == "" {
		host.Type = "m3u"
	}
	if host.Id == "" {
		host.Id = guid.New()
	}

	var parsed []M3UChannel
	if strings.EqualFold(host.Type, "m3u") {
		var err error
		if strings.TrimSpace(host.M3UContent) != "" {
			parsed, err = ParseM3UString(host.M3UContent)
		} else if strings.TrimSpace(host.Url) != "" {
			parsed, err = h.loadM3UFromSource(r.Context(), host.Url, host.UserAgent)
		}
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, fmt.Sprintf("failed to load m3u playlist: %v", err))
			return
		}
	}

	h.mu.Lock()
	// Upsert tuner host.
	replaced := false
	for i, existing := range h.state.TunerHosts {
		if existing.Id == host.Id {
			host.ImportedChannelCount = len(parsed)
			h.state.TunerHosts[i] = host
			replaced = true
			break
		}
	}
	if !replaced {
		host.ImportedChannelCount = len(parsed)
		h.state.TunerHosts = append(h.state.TunerHosts, host)
	}

	// Replace channels and placeholder programs for this tuner host if M3U channels were parsed.
	if len(parsed) > 0 {
		keptChannels := make([]ChannelInfo, 0, len(h.state.Channels))
		removedChannelIDs := make(map[string]struct{})
		for _, ch := range h.state.Channels {
			if ch.TunerHostId == host.Id {
				removedChannelIDs[ch.Id] = struct{}{}
				continue
			}
			keptChannels = append(keptChannels, ch)
		}
		keptPrograms := make([]ProgramInfo, 0, len(h.state.Programs))
		for _, p := range h.state.Programs {
			if _, drop := removedChannelIDs[p.ChannelId]; !drop {
				keptPrograms = append(keptPrograms, p)
			}
		}

		now := time.Now().UTC().Truncate(time.Hour)
		for _, m := range parsed {
			chID := guid.New()
			ch := ChannelInfo{
				Id:          chID,
				Name:        m.Name,
				ServerId:    h.serverID(),
				Type:        "TvChannel",
				ChannelType: "TV",
				Number:      m.Number,
				TvgId:       m.TvgID,
				LogoUrl:     m.TvgLogo,
				GroupTitle:  m.GroupTitle,
				StreamUrl:   m.URL,
				TunerHostId: host.Id,
			}
			keptChannels = append(keptChannels, ch)
			keptPrograms = append(keptPrograms, ProgramInfo{
				Id:          guid.New(),
				Name:        ch.Name + " Live",
				ServerId:    h.serverID(),
				Type:        "Program",
				ChannelId:   ch.Id,
				ChannelName: ch.Name,
				StartDate:   now,
				EndDate:     now.Add(24 * time.Hour),
				IsLive:      true,
			})
		}
		h.state.Channels = keptChannels
		h.state.Programs = keptPrograms
	}
	h.saveStateLocked(r.Context())
	h.mu.Unlock()

	httpx.JSON(w, http.StatusOK, host)
}

func (h *Handler) loadM3UFromSource(ctx context.Context, src, userAgent string) ([]M3UChannel, error) {
	trimmed := strings.TrimSpace(src)
	if trimmed == "" {
		return nil, errors.New("m3u source is empty")
	}

	if u, err := url.Parse(trimmed); err == nil && u.Scheme != "" {
		scheme := strings.ToLower(u.Scheme)
		if scheme == "http" || scheme == "https" {
			if !h.customHTTPClient {
				if err := netutil.ValidateRemoteURL(trimmed, false); err != nil {
					return nil, fmt.Errorf("blocked m3u url: %w", err)
				}
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, trimmed, nil)
			if err != nil {
				return nil, err
			}
			if userAgent != "" {
				req.Header.Set("User-Agent", userAgent)
			}
			resp, err := h.httpClient.Do(req)
			if err != nil {
				return nil, err
			}
			defer resp.Body.Close()
			if resp.StatusCode >= 400 {
				return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
			}
			return ParseM3U(resp.Body)
		}
		// Reject non-HTTP URL schemes (e.g. file://, ftp://, gopher://), while allowing
		// single-letter Windows drive prefixes like C:\playlist.m3u.
		if len(scheme) > 1 {
			return nil, fmt.Errorf("unsupported m3u url scheme %q", u.Scheme)
		}
	}

	resolvedPath, err := h.validateLocalM3UPath(ctx, trimmed)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(resolvedPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseM3U(f)
}

func (h *Handler) validateLocalM3UPath(ctx context.Context, rawPath string) (string, error) {
	ext := strings.ToLower(filepath.Ext(rawPath))
	if ext != ".m3u" && ext != ".m3u8" {
		return "", fmt.Errorf("local m3u file must have .m3u or .m3u8 extension")
	}

	resolvedPath, err := filepath.EvalSymlinks(rawPath)
	if err != nil {
		return "", err
	}
	resolvedPath = filepath.Clean(resolvedPath)

	// Re-check extension after symlink resolution to prevent symlink-to-/etc/passwd tricks.
	resolvedExt := strings.ToLower(filepath.Ext(resolvedPath))
	if resolvedExt != ".m3u" && resolvedExt != ".m3u8" {
		return "", fmt.Errorf("resolved local m3u target must have .m3u or .m3u8 extension")
	}

	allowedRoots := h.allowedLocalM3URoots(ctx)
	for _, root := range allowedRoots {
		if isPathWithinRoot(root, resolvedPath) {
			return resolvedPath, nil
		}
	}
	return "", fmt.Errorf("local m3u file path is outside allowed directories")
}

func (h *Handler) allowedLocalM3URoots(ctx context.Context) []string {
	var roots []string
	addRoot := func(dir string) {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			return
		}
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			roots = append(roots, filepath.Clean(resolved))
		} else {
			roots = append(roots, filepath.Clean(dir))
		}
	}

	if h.cfg != nil && h.cfg.DataDir != "" {
		addRoot(h.cfg.DataDir)
	}
	if h.store != nil {
		libRepo := sqlite.NewLibraryRepo(h.store)
		if libs, err := libRepo.ListLibraries(ctx); err == nil {
			for _, lib := range libs {
				for _, p := range lib.Paths {
					addRoot(p)
				}
			}
		}
	}
	return roots
}

func isPathWithinRoot(root, target string) bool {
	if root == "" || target == "" {
		return false
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return false
	}
	return true
}

func (h *Handler) handleDeleteTunerHost(w http.ResponseWriter, r *http.Request) {
	id := httpx.ParseQuery(r).Get("id")
	if id == "" {
		httpx.Error(w, http.StatusBadRequest, "id query parameter is required")
		return
	}
	h.mu.Lock()
	found := false
	keptHosts := make([]TunerHostInfo, 0, len(h.state.TunerHosts))
	for _, t := range h.state.TunerHosts {
		if t.Id == id {
			found = true
			continue
		}
		keptHosts = append(keptHosts, t)
	}
	if !found {
		h.mu.Unlock()
		httpx.Error(w, http.StatusNotFound, "tuner host not found")
		return
	}
	h.state.TunerHosts = keptHosts

	removedChannelIDs := make(map[string]struct{})
	keptChannels := make([]ChannelInfo, 0, len(h.state.Channels))
	for _, ch := range h.state.Channels {
		if ch.TunerHostId == id {
			removedChannelIDs[ch.Id] = struct{}{}
			continue
		}
		keptChannels = append(keptChannels, ch)
	}
	h.state.Channels = keptChannels

	keptPrograms := make([]ProgramInfo, 0, len(h.state.Programs))
	for _, p := range h.state.Programs {
		if _, drop := removedChannelIDs[p.ChannelId]; !drop {
			keptPrograms = append(keptPrograms, p)
		}
	}
	h.state.Programs = keptPrograms
	h.saveStateLocked(r.Context())
	h.mu.Unlock()
	httpx.NoContent(w)
}

func (h *Handler) handleTunerHostTypes(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, []NameIdPair{
		{Name: "M3U Tuner", Id: "m3u"},
		{Name: "HDHomeRun", Id: "hdhomerun"},
	})
}

func (h *Handler) handleSaveListingProvider(w http.ResponseWriter, r *http.Request) {
	var lp ListingProviderInfo
	if err := json.NewDecoder(r.Body).Decode(&lp); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid listing provider payload")
		return
	}
	if lp.Id == "" {
		lp.Id = guid.New()
	}
	if lp.Type == "" {
		lp.Type = "xmltv"
	}

	h.mu.Lock()
	replaced := false
	for i, existing := range h.state.ListingProviders {
		if existing.Id == lp.Id {
			h.state.ListingProviders[i] = lp
			replaced = true
			break
		}
	}
	if !replaced {
		h.state.ListingProviders = append(h.state.ListingProviders, lp)
	}
	h.saveStateLocked(r.Context())
	h.mu.Unlock()
	httpx.JSON(w, http.StatusOK, lp)
}

func (h *Handler) handleDeleteListingProvider(w http.ResponseWriter, r *http.Request) {
	id := httpx.ParseQuery(r).Get("id")
	if id == "" {
		httpx.Error(w, http.StatusBadRequest, "id query parameter is required")
		return
	}
	h.mu.Lock()
	for i, lp := range h.state.ListingProviders {
		if lp.Id == id {
			h.state.ListingProviders = append(h.state.ListingProviders[:i], h.state.ListingProviders[i+1:]...)
			h.saveStateLocked(r.Context())
			h.mu.Unlock()
			httpx.NoContent(w)
			return
		}
	}
	h.mu.Unlock()
	httpx.Error(w, http.StatusNotFound, "listing provider not found")
}

func (h *Handler) handleGuideInfo(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC().Truncate(time.Hour)
	start := now
	end := now.Add(7 * 24 * time.Hour)

	h.mu.RLock()
	for _, p := range h.state.Programs {
		if !p.StartDate.IsZero() && p.StartDate.Before(start) {
			start = p.StartDate
		}
		if !p.EndDate.IsZero() && p.EndDate.After(end) {
			end = p.EndDate
		}
	}
	h.mu.RUnlock()

	httpx.JSON(w, http.StatusOK, GuideInfo{
		StartDate: start,
		EndDate:   end,
	})
}

func (h *Handler) loadState(ctx context.Context) {
	if h.store == nil {
		return
	}
	var raw string
	err := h.store.Reader.QueryRowContext(ctx,
		`SELECT value_json FROM server_config WHERE key = ?`, stateConfigKey,
	).Scan(&raw)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return
		}
		return
	}
	var st liveTVState
	if err := json.Unmarshal([]byte(raw), &st); err == nil {
		h.mu.Lock()
		h.state = st
		h.mu.Unlock()
	}
}

func (h *Handler) saveStateLocked(ctx context.Context) {
	if h.store == nil {
		return
	}
	raw, err := json.Marshal(h.state)
	if err != nil {
		return
	}
	_, _ = h.store.Writer.ExecContext(ctx, `
		INSERT INTO server_config (key, value_json, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			value_json = excluded.value_json,
			updated_at = excluded.updated_at`,
		stateConfigKey, string(raw), time.Now().UTC().Unix(),
	)
}

func paginate[T any](items []T, startIndex, limit int) ([]T, int) {
	total := len(items)
	if startIndex < 0 {
		startIndex = 0
	}
	if startIndex >= total {
		return []T{}, total
	}
	end := total
	if limit > 0 && startIndex+limit < end {
		end = startIndex + limit
	}
	out := make([]T, end-startIndex)
	copy(out, items[startIndex:end])
	return out, total
}

func passthrough(next http.Handler) http.Handler { return next }
