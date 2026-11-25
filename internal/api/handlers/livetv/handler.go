package livetv

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/dvr"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/pkg/netutil"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

const (
	stateConfigKey      = "livetv:state"
	maxM3UPlaylistBytes = 20 << 20
	maxTimerDuration    = 24 * time.Hour
	maxPaddingSeconds   = 3600
	proxyTargetTTL      = 6 * time.Hour
)

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
	Id           string `json:"Id"`
	Name         string `json:"Name"`
	ServerId     string `json:"ServerId,omitempty"`
	Type         string `json:"Type"`
	ChannelType  string `json:"ChannelType"`
	Number       string `json:"Number,omitempty"`
	TvgId        string `json:"TvgId,omitempty"`
	LogoUrl      string `json:"LogoUrl,omitempty"`
	GroupTitle   string `json:"GroupTitle,omitempty"`
	StreamUrl    string `json:"StreamUrl,omitempty"`
	IsHls        bool   `json:"IsHls"`
	StreamFormat string `json:"StreamFormat,omitempty"`
	TunerHostId  string `json:"TunerHostId,omitempty"`
	Disabled     bool   `json:"Disabled,omitempty"`
	SortIndex    int    `json:"SortIndex,omitempty"`
}

type ProgramInfo struct {
	Id          string    `json:"Id"`
	Name        string    `json:"Name"`
	ServerId    string    `json:"ServerId,omitempty"`
	Type        string    `json:"Type"`
	ChannelId   string    `json:"ChannelId"`
	ChannelName string    `json:"ChannelName,omitempty"`
	SeriesId    string    `json:"SeriesId,omitempty"`
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
	ProgramId             string   `json:"ProgramId,omitempty"`
	SeriesId              string   `json:"SeriesId,omitempty"`
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
	store              *sqlite.Store
	cfg                *config.Config
	userRepo           *sqlite.UserRepo
	httpClient         *http.Client
	streamClient       *http.Client
	publicStreamClient *http.Client
	customHTTPClient   bool
	customStreamClient bool
	proxySecret        []byte
	recorder           *dvr.Recorder
	auth               AuthFuncs

	mu    sync.RWMutex
	state liveTVState
}

func New(store *sqlite.Store, cfg *config.Config) *Handler {
	secret := make([]byte, 32)
	_, _ = rand.Read(secret)
	h := &Handler{
		store:              store,
		cfg:                cfg,
		httpClient:         netutil.NewSafeHTTPClientNoLoopback(15*time.Second, true),
		streamClient:       netutil.NewSafeHTTPClientNoLoopback(0, true),
		publicStreamClient: netutil.NewSafeHTTPClientNoLoopback(0, false),
		proxySecret:        secret,
	}
	h.loadState(context.Background())
	return h
}

// SetUserRepo attaches a UserRepo for Live TV user access listing.
func (h *Handler) SetUserRepo(r *sqlite.UserRepo) {
	h.mu.Lock()
	h.userRepo = r
	h.mu.Unlock()
}

func (h *Handler) getUserRepo() *sqlite.UserRepo {
	h.mu.RLock()
	r := h.userRepo
	h.mu.RUnlock()
	return r
}

// SetRecorder attaches a DVR recorder for timer cancellation and recording cleanup.
func (h *Handler) SetRecorder(r *dvr.Recorder) {
	h.mu.Lock()
	h.recorder = r
	h.mu.Unlock()
}

// GetChannelByID returns a copy of the channel with the given ID if it exists.
func (h *Handler) GetChannelByID(id string) (*ChannelInfo, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for i := range h.state.Channels {
		if h.state.Channels[i].Id == id {
			ch := h.state.Channels[i]
			return &ch, true
		}
	}
	return nil, false
}

// SetChannelDisabled updates a channel's disabled state in memory and persists state to SQLite.
func (h *Handler) SetChannelDisabled(ctx context.Context, channelID string, disabled bool) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := range h.state.Channels {
		if h.state.Channels[i].Id == channelID {
			h.state.Channels[i].Disabled = disabled
			return h.saveStateLocked(ctx)
		}
	}
	return os.ErrNotExist
}

// SetChannelSortIndex updates a channel's sort index in memory and persists state to SQLite.
func (h *Handler) SetChannelSortIndex(ctx context.Context, channelID string, sortIndex int) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := range h.state.Channels {
		if h.state.Channels[i].Id == channelID {
			h.state.Channels[i].SortIndex = sortIndex
			return h.saveStateLocked(ctx)
		}
	}
	return os.ErrNotExist
}

// DeleteTimerByID removes a timer by ID, persists state, and cancels any active DVR recording.
func (h *Handler) DeleteTimerByID(ctx context.Context, id string) bool {
	return h.DeleteTimer(ctx, id)
}

// DeleteSeriesTimerByID removes a series timer by ID (cascading pending single timers) and persists state.
func (h *Handler) DeleteSeriesTimerByID(ctx context.Context, id string) bool {
	return h.DeleteSeriesTimer(ctx, id)
}

// HasLiveTvManagement reports whether the request caller has admin or Live TV management privileges.
func (h *Handler) HasLiveTvManagement(ctx context.Context, r *http.Request) bool {
	if r != nil && ctx == nil {
		ctx = r.Context()
	}
	var ac *middleware.AuthContext
	if r != nil {
		if a, ok := middleware.AuthFromContext(r.Context()); ok && a != nil && (a.User != nil || a.IsAPIKey) {
			ac = a
		}
	}
	if ac == nil && ctx != nil {
		if a, ok := middleware.AuthFromContext(ctx); ok && a != nil && (a.User != nil || a.IsAPIKey) {
			ac = a
		}
	}
	ur := h.getUserRepo()
	if ac != nil {
		if ac.IsAdmin() {
			return true
		}
		if ac.User == nil {
			return false
		}
		if ur != nil && ac.User.ID != "" {
			u, err := ur.GetUserByID(ctx, guid.Normalize(ac.User.ID))
			if err != nil || u == nil || u.IsDisabled {
				return false
			}
			return u.IsAdmin || u.Policy.IsAdministrator || u.Policy.EnableLiveTvManagement
		}
		return ac.User.Policy.EnableLiveTvManagement
	}
	if ctx != nil && h.auth.UserIDFromCtx != nil {
		uid := guid.Normalize(h.auth.UserIDFromCtx(ctx))
		if uid == "" {
			return false
		}
		if ur == nil {
			return false
		}
		u, err := ur.GetUserByID(ctx, uid)
		if err != nil || u == nil || u.IsDisabled {
			return false
		}
		return u.IsAdmin || u.Policy.IsAdministrator || u.Policy.EnableLiveTvManagement
	}
	return false
}

// DueRecordingJobs scans pending ("New") timers whose padded window includes now,
// validates their channel stream URLs, transitions them to "InProgress", persists state,
// and returns the resulting DVR recording jobs.
func (h *Handler) DueRecordingJobs(ctx context.Context) ([]dvr.RecordingJob, error) {
	now := time.Now().UTC()

	type candidate struct {
		timerID     string
		channelName string
		programName string
		streamURL   string
		duration    time.Duration
	}

	h.mu.RLock()
	chByID := make(map[string]ChannelInfo, len(h.state.Channels))
	for _, ch := range h.state.Channels {
		chByID[ch.Id] = ch
	}
	var candidates []candidate
	for _, t := range h.state.Timers {
		if t.Status != "New" {
			continue
		}
		windowStart := t.StartDate.Add(-time.Duration(t.PrePaddingSeconds) * time.Second)
		windowEnd := t.EndDate.Add(time.Duration(t.PostPaddingSeconds) * time.Second)
		if now.Before(windowStart) || !now.Before(windowEnd) {
			continue
		}
		ch, ok := chByID[t.ChannelId]
		if !ok || ch.Disabled {
			continue
		}
		streamURL := strings.TrimSpace(ch.StreamUrl)
		if streamURL == "" {
			continue
		}
		chName := t.ChannelName
		if chName == "" {
			chName = ch.Name
		}
		candidates = append(candidates, candidate{
			timerID:     t.Id,
			channelName: chName,
			programName: t.Name,
			streamURL:   streamURL,
			duration:    windowEnd.Sub(now),
		})
	}
	h.mu.RUnlock()

	if len(candidates) == 0 {
		return nil, nil
	}

	valid := make(map[string]candidate, len(candidates))
	for _, c := range candidates {
		if err := netutil.ValidateRemoteURLNoLoopback(c.streamURL, true); err != nil {
			continue
		}
		valid[c.timerID] = c
	}
	if len(valid) == 0 {
		return nil, nil
	}

	h.mu.Lock()
	jobs := make([]dvr.RecordingJob, 0, len(valid))
	changed := false
	for i := range h.state.Timers {
		t := &h.state.Timers[i]
		if t.Status != "New" {
			continue
		}
		c, ok := valid[t.Id]
		if !ok {
			continue
		}
		t.Status = "InProgress"
		changed = true
		jobs = append(jobs, dvr.RecordingJob{
			TimerID:     c.timerID,
			ChannelName: c.channelName,
			ProgramName: c.programName,
			StreamURL:   c.streamURL,
			Duration:    c.duration,
		})
	}
	if changed {
		h.saveStateLocked(ctx)
	}
	h.mu.Unlock()

	return jobs, nil
}

// SetHTTPClient overrides the HTTP client used for remote M3U playlist fetching (primarily for testing).
func (h *Handler) SetHTTPClient(client *http.Client) {
	if client != nil {
		h.httpClient = client
		h.customHTTPClient = true
	}
}

// SetStreamClient overrides the HTTP clients used for channel stream proxying (primarily for testing).
func (h *Handler) SetStreamClient(client *http.Client) {
	if client != nil {
		h.streamClient = client
		h.publicStreamClient = client
		h.customStreamClient = true
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

// OnRecordingFinished updates the matching timer status and, when the recording completed,
// appends a RecordingInfo entry and persists Live TV state.
func (h *Handler) OnRecordingFinished(ctx context.Context, state dvr.RecordingState) {
	timerStatus := string(state.Status)
	switch state.Status {
	case dvr.StatusCompleted:
		timerStatus = "Completed"
	case dvr.StatusFailed:
		timerStatus = "Failed"
	case dvr.StatusCancelled:
		timerStatus = "Cancelled"
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	var (
		changed     bool
		channelID   string
		channelName = state.Job.ChannelName
		programID   string
		recName     = state.Job.ProgramName
	)

	for i := range h.state.Timers {
		if h.state.Timers[i].Id == state.Job.TimerID {
			h.state.Timers[i].Status = timerStatus
			channelID = h.state.Timers[i].ChannelId
			if channelName == "" {
				channelName = h.state.Timers[i].ChannelName
			}
			programID = h.state.Timers[i].ProgramId
			if recName == "" {
				recName = h.state.Timers[i].Name
			}
			changed = true
			break
		}
	}

	if state.Status == dvr.StatusCompleted {
		if recName == "" {
			if channelName != "" {
				recName = channelName
			} else {
				recName = "Recording"
			}
		}
		h.state.Recordings = append(h.state.Recordings, RecordingInfo{
			Id:          guid.New(),
			Name:        recName,
			ServerId:    h.serverID(),
			Type:        "Recording",
			ChannelId:   channelID,
			ChannelName: channelName,
			ProgramId:   programID,
			Status:      "Completed",
			StartDate:   state.StartedAt,
			EndDate:     state.EndedAt,
			Path:        state.OutputPath,
		})
		changed = true
	}

	if changed {
		h.saveStateLocked(ctx)
	}
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
	handleLiveTvAccess := func(pattern string, fn http.HandlerFunc) {
		mux.Handle(pattern, h.requireLiveTvAccess(wrapAuth)(fn))
	}
	handleLiveTvManage := func(pattern string, fn http.HandlerFunc) {
		mux.Handle(pattern, h.requireLiveTvManagement(wrapAuth)(fn))
	}

	// Info, Channels, Programs
	handleAuth("GET /LiveTv/Info", h.handleInfo)
	handleLiveTvAccess("GET /LiveTv/Channels", h.handleListChannels)
	handleLiveTvAccess("GET /LiveTv/Channels/{Id}", h.handleGetChannel)
	handleLiveTvAccess("GET /LiveTv/Channels/{Id}/Stream", h.handleStreamChannel)
	handleLiveTvAccess("GET /LiveTv/Programs", h.handleListPrograms)
	handleLiveTvAccess("POST /LiveTv/Programs", h.handleQueryProgramsPost)
	handleLiveTvAccess("GET /LiveTv/Programs/Recommended", h.handleRecommendedPrograms)
	handleLiveTvAccess("GET /LiveTv/Programs/{Id}", h.handleGetProgram)

	// Recordings
	handleLiveTvAccess("GET /LiveTv/Recordings", h.handleListRecordings)
	handleLiveTvAccess("GET /LiveTv/Recordings/Folders", h.handleRecordingFolders)
	handleLiveTvAccess("GET /LiveTv/Recordings/{Id}", h.handleGetRecording)
	handleAdmin("DELETE /LiveTv/Recordings/{Id}", h.handleDeleteRecording)

	// Timers
	handleLiveTvAccess("GET /LiveTv/Timers", h.handleListTimers)
	handleLiveTvManage("POST /LiveTv/Timers", h.handleCreateTimer)
	handleLiveTvAccess("GET /LiveTv/Timers/Defaults", h.handleTimerDefaults)
	handleLiveTvAccess("GET /LiveTv/Timers/{Id}", h.handleGetTimer)
	handleLiveTvManage("POST /LiveTv/Timers/{Id}", h.handleUpdateTimer)
	handleLiveTvManage("DELETE /LiveTv/Timers/{Id}", h.handleDeleteTimer)

	// SeriesTimers
	handleLiveTvAccess("GET /LiveTv/SeriesTimers", h.handleListSeriesTimers)
	handleLiveTvManage("POST /LiveTv/SeriesTimers", h.handleCreateSeriesTimer)
	handleLiveTvAccess("GET /LiveTv/SeriesTimers/{Id}", h.handleGetSeriesTimer)
	handleLiveTvManage("POST /LiveTv/SeriesTimers/{Id}", h.handleUpdateSeriesTimer)
	handleLiveTvManage("DELETE /LiveTv/SeriesTimers/{Id}", h.handleDeleteSeriesTimer)

	// Tuners, ListingProviders & GuideInfo
	handleAdmin("POST /LiveTv/TunerHosts", h.handleSaveTunerHost)
	handleAdmin("DELETE /LiveTv/TunerHosts", h.handleDeleteTunerHost)
	handleAuth("GET /LiveTv/TunerHosts/Types", h.handleTunerHostTypes)
	handleAdmin("POST /LiveTv/ListingProviders", h.handleSaveListingProvider)
	handleAdmin("DELETE /LiveTv/ListingProviders", h.handleDeleteListingProvider)
	handleLiveTvAccess("GET /LiveTv/GuideInfo", h.handleGuideInfo)
}

func (h *Handler) requireLiveTvAccess(wrapAuth func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return wrapAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && (ac.User != nil || ac.IsAPIKey) {
				if !ac.IsAdmin() {
					if ac.User == nil || !ac.User.Policy.EnableLiveTvAccess {
						httpx.Error(w, http.StatusForbidden, "Live TV access is disabled for this user")
						return
					}
					if ur := h.getUserRepo(); ur != nil && ac.User.ID != "" {
						if u, err := ur.GetUserByID(r.Context(), ac.User.ID); err == nil && u != nil {
							if !u.IsAdmin && !u.Policy.IsAdministrator && !u.Policy.EnableLiveTvAccess {
								httpx.Error(w, http.StatusForbidden, "Live TV access is disabled for this user")
								return
							}
						}
					}
				}
			} else if h.auth.UserIDFromCtx != nil {
				if ur := h.getUserRepo(); ur != nil {
					if uid := guid.Normalize(h.auth.UserIDFromCtx(r.Context())); uid != "" {
						if u, err := ur.GetUserByID(r.Context(), uid); err == nil && u != nil {
							if !u.IsAdmin && !u.Policy.IsAdministrator && !u.Policy.EnableLiveTvAccess {
								httpx.Error(w, http.StatusForbidden, "Live TV access is disabled for this user")
								return
							}
						}
					}
				}
			}
			next.ServeHTTP(w, r)
		}))
	}
}

func (h *Handler) requireLiveTvManagement(wrapAuth func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return wrapAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if h.auth.RequireAuth == nil && h.auth.UserIDFromCtx == nil && h.getUserRepo() == nil {
				if ac, ok := middleware.AuthFromContext(r.Context()); !ok || ac == nil || (ac.User == nil && !ac.IsAPIKey) {
					next.ServeHTTP(w, r)
					return
				}
			}
			if !h.HasLiveTvManagement(r.Context(), r) {
				httpx.Error(w, http.StatusForbidden, "Live TV management privileges required")
				return
			}
			next.ServeHTTP(w, r)
		}))
	}
}

func (h *Handler) callerAuthInfo(r *http.Request) (userID string, isAdmin bool, hasAuth bool) {
	if r == nil {
		return "", false, false
	}
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && (ac.User != nil || ac.IsAPIKey) {
		return guid.Normalize(ac.UserID()), ac.IsAdmin(), true
	}
	if h.auth.UserIDFromCtx != nil {
		if uid := guid.Normalize(h.auth.UserIDFromCtx(r.Context())); uid != "" {
			if ur := h.getUserRepo(); ur != nil {
				if u, err := ur.GetUserByID(r.Context(), uid); err == nil && u != nil {
					return uid, u.IsAdmin || u.Policy.IsAdministrator, true
				}
			}
			return uid, false, true
		}
	}
	return "", false, false
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

	callerID, isAdmin, hasAuth := h.callerAuthInfo(r)
	enabledUsers := make([]string, 0)
	if userRepo := h.getUserRepo(); userRepo != nil {
		if hasAuth && !isAdmin {
			if callerID != "" {
				if u, err := userRepo.GetUserByID(r.Context(), callerID); err == nil && u != nil && !u.IsDisabled && (u.IsAdmin || u.Policy.IsAdministrator || u.Policy.EnableLiveTvAccess) {
					enabledUsers = append(enabledUsers, u.ID)
				}
			}
		} else if users, err := userRepo.ListUsers(r.Context()); err == nil {
			for _, u := range users {
				if u != nil && !u.IsDisabled && (u.IsAdmin || u.Policy.IsAdministrator || u.Policy.EnableLiveTvAccess) {
					enabledUsers = append(enabledUsers, u.ID)
				}
			}
		}
	}

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
		EnabledUsers: enabledUsers,
	})
}

func detectStreamFormat(streamURL string) (isHLS bool, format string) {
	trimmed := strings.TrimSpace(streamURL)
	if trimmed == "" {
		return false, "unknown"
	}
	lowerRaw := strings.ToLower(trimmed)
	lowerPath := lowerRaw
	if u, err := url.Parse(trimmed); err == nil && u.Path != "" {
		lowerPath = strings.ToLower(u.Path)
	}
	if strings.HasSuffix(lowerPath, ".m3u8") ||
		strings.HasSuffix(lowerPath, ".m3u") ||
		strings.Contains(lowerPath, ".m3u8") ||
		strings.Contains(lowerRaw, "format=m3u8") ||
		strings.Contains(lowerRaw, "type=m3u8") {
		return true, "hls"
	}
	if strings.HasSuffix(lowerPath, ".ts") ||
		strings.HasSuffix(lowerPath, ".m2ts") ||
		strings.HasSuffix(lowerPath, ".mpeg") ||
		strings.HasSuffix(lowerPath, ".mpg") ||
		strings.Contains(lowerRaw, "format=ts") ||
		strings.Contains(lowerRaw, "output=ts") ||
		strings.Contains(lowerRaw, "type=mpegts") {
		return false, "mpegts"
	}
	return false, "unknown"
}

func (h *Handler) handleListChannels(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	search := strings.ToLower(strings.TrimSpace(q.Get("SearchTerm")))
	group := strings.ToLower(strings.TrimSpace(q.Get("GroupTitle")))
	chType := strings.ToLower(strings.TrimSpace(q.Get("Type")))

	_, isAdmin, hasAuth := h.callerAuthInfo(r)
	hideStreamURL := hasAuth && !isAdmin

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
		ch.IsHls, ch.StreamFormat = detectStreamFormat(ch.StreamUrl)
		if hideStreamURL {
			ch.StreamUrl = ""
		}
		filtered = append(filtered, ch)
	}
	h.mu.RUnlock()

	sort.SliceStable(filtered, func(i, j int) bool {
		return filtered[i].SortIndex < filtered[j].SortIndex
	})

	page, total := paginate(filtered, q.Int("StartIndex", 0), q.Int("Limit", 0))
	httpx.JSON(w, http.StatusOK, dto.QueryResult[ChannelInfo]{
		Items:            page,
		TotalRecordCount: total,
	})
}

func (h *Handler) handleGetChannel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("Id")
	ch, ok := h.GetChannelByID(id)
	if !ok {
		httpx.Error(w, http.StatusNotFound, "channel not found")
		return
	}
	ch.IsHls, ch.StreamFormat = detectStreamFormat(ch.StreamUrl)
	if _, isAdmin, hasAuth := h.callerAuthInfo(r); hasAuth && !isAdmin {
		ch.StreamUrl = ""
	}
	httpx.JSON(w, http.StatusOK, ch)
}

const maxHLSPlaylistBytes = 2 << 20 // 2 MiB

var hlsURIAttributeRe = regexp.MustCompile(`URI="([^"]+)"`)

func (h *Handler) signProxyTarget(channelID, target string, allowPrivateLAN bool, exp int64) string {
	lanFlag := "0"
	if allowPrivateLAN {
		lanFlag = "1"
	}
	mac := hmac.New(sha256.New, h.proxySecret)
	mac.Write([]byte(channelID))
	mac.Write([]byte{0})
	mac.Write([]byte(lanFlag))
	mac.Write([]byte{0})
	mac.Write([]byte(strconv.FormatInt(exp, 10)))
	mac.Write([]byte{0})
	mac.Write([]byte(target))
	return hex.EncodeToString(mac.Sum(nil))
}

func (h *Handler) verifyProxyTarget(channelID, target string, allowPrivateLAN bool, expStr, sig string) bool {
	if len(h.proxySecret) == 0 {
		return false
	}
	exp, err := strconv.ParseInt(strings.TrimSpace(expStr), 10, 64)
	if err != nil || exp <= time.Now().UTC().Unix() {
		return false
	}
	got, err := hex.DecodeString(strings.TrimSpace(sig))
	if err != nil || len(got) != sha256.Size {
		return false
	}
	lanFlag := "0"
	if allowPrivateLAN {
		lanFlag = "1"
	}
	mac := hmac.New(sha256.New, h.proxySecret)
	mac.Write([]byte(channelID))
	mac.Write([]byte{0})
	mac.Write([]byte(lanFlag))
	mac.Write([]byte{0})
	mac.Write([]byte(strconv.FormatInt(exp, 10)))
	mac.Write([]byte{0})
	mac.Write([]byte(target))
	return hmac.Equal(got, mac.Sum(nil))
}

func (h *Handler) validateStreamURL(rawURL string, allowPrivateLAN bool) error {
	if h.customStreamClient {
		return netutil.ValidateRemoteURL(rawURL, allowPrivateLAN)
	}
	return netutil.ValidateRemoteURLNoLoopback(rawURL, allowPrivateLAN)
}

func (h *Handler) channelAllowsPrivateLAN(channelURL string) bool {
	trimmed := strings.TrimSpace(channelURL)
	if trimmed == "" {
		return false
	}
	// Public channels (valid under allowPrivateLAN=false) MUST NOT access RFC1918 private LAN.
	if netutil.ValidateRemoteURL(trimmed, false) == nil {
		return false
	}
	// Private LAN channels are allowed LAN access, never loopback in production.
	return h.validateStreamURL(trimmed, true) == nil
}

func (h *Handler) handleStreamChannel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("Id")
	ch, ok := h.GetChannelByID(id)
	if !ok {
		httpx.Error(w, http.StatusNotFound, "channel not found")
		return
	}
	if ch.Disabled {
		httpx.Error(w, http.StatusForbidden, "channel is disabled")
		return
	}
	channelAllowLAN := h.channelAllowsPrivateLAN(ch.StreamUrl)
	streamURL := ch.StreamUrl
	allowPrivateLAN := channelAllowLAN
	if target := strings.TrimSpace(r.URL.Query().Get("target")); target != "" {
		q := r.URL.Query()
		sig := strings.TrimSpace(q.Get("sig"))
		expStr := strings.TrimSpace(q.Get("exp"))
		lanParam := strings.TrimSpace(q.Get("lan"))
		if lanParam != "0" && lanParam != "1" {
			httpx.Error(w, http.StatusForbidden, "invalid or missing target scope")
			return
		}
		reqAllowLAN := lanParam == "1"
		if reqAllowLAN && !channelAllowLAN {
			httpx.Error(w, http.StatusForbidden, "target scope exceeds channel network scope")
			return
		}
		if !h.verifyProxyTarget(id, target, reqAllowLAN, expStr, sig) {
			httpx.Error(w, http.StatusForbidden, "invalid, expired, or missing target signature")
			return
		}
		if err := h.validateStreamURL(target, reqAllowLAN); err != nil {
			httpx.Error(w, http.StatusBadRequest, fmt.Sprintf("unsafe target stream url: %v", err))
			return
		}
		streamURL = target
		allowPrivateLAN = reqAllowLAN
	}
	h.proxyChannelStreamWithPolicy(w, r, id, streamURL, allowPrivateLAN)
}

type flushWriter struct {
	w http.ResponseWriter
	f http.Flusher
}

func (fw flushWriter) Write(p []byte) (int, error) {
	n, err := fw.w.Write(p)
	if fw.f != nil && n > 0 {
		fw.f.Flush()
	}
	return n, err
}

// ProxyStreamURL validates streamURL (allowing private LAN IPTV tuners while blocking
// loopback/metadata/link-local/non-HTTP URLs) and proxies the upstream live stream bound to r.Context().
func (h *Handler) ProxyStreamURL(w http.ResponseWriter, r *http.Request, streamURL string) {
	h.proxyChannelStreamWithPolicy(w, r, "", streamURL, h.channelAllowsPrivateLAN(streamURL))
}

// ProxyChannelStream validates streamURL and proxies the upstream live stream for channelID,
// rewriting HLS playlist URIs with signed relative proxy URLs when channelID is non-empty.
func (h *Handler) ProxyChannelStream(w http.ResponseWriter, r *http.Request, channelID, streamURL string) {
	allowLAN := h.channelAllowsPrivateLAN(streamURL)
	if channelID != "" {
		if ch, ok := h.GetChannelByID(channelID); ok && strings.TrimSpace(ch.StreamUrl) != "" {
			allowLAN = h.channelAllowsPrivateLAN(ch.StreamUrl)
		}
	}
	h.proxyChannelStreamWithPolicy(w, r, channelID, streamURL, allowLAN)
}

func (h *Handler) proxyChannelStream(w http.ResponseWriter, r *http.Request, channelID, streamURL string) {
	h.ProxyChannelStream(w, r, channelID, streamURL)
}

func (h *Handler) proxyChannelStreamWithPolicy(w http.ResponseWriter, r *http.Request, channelID, streamURL string, allowPrivateLAN bool) {
	trimmed := strings.TrimSpace(streamURL)
	if err := h.validateStreamURL(trimmed, allowPrivateLAN); err != nil {
		httpx.Error(w, http.StatusBadRequest, fmt.Sprintf("unsafe channel stream url: %v", err))
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, trimmed, nil)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, fmt.Sprintf("invalid channel stream url: %v", err))
		return
	}

	var client *http.Client
	if allowPrivateLAN {
		client = h.streamClient
		if client == nil {
			client = netutil.NewSafeHTTPClientNoLoopback(0, true)
		}
	} else {
		client = h.publicStreamClient
		if client == nil {
			if h.customStreamClient && h.streamClient != nil {
				client = h.streamClient
			} else {
				client = netutil.NewSafeHTTPClientNoLoopback(0, false)
			}
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		httpx.Error(w, http.StatusBadGateway, fmt.Sprintf("upstream stream error: %v", err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		httpx.Error(w, http.StatusBadGateway, fmt.Sprintf("upstream stream returned status %d", resp.StatusCode))
		return
	}

	baseURL := req.URL
	if resp.Request != nil && resp.Request.URL != nil {
		baseURL = resp.Request.URL
	}

	ct := resp.Header.Get("Content-Type")
	if channelID != "" && isHLSPlaylistResponse(ct, baseURL) {
		raw, err := io.ReadAll(io.LimitReader(resp.Body, maxHLSPlaylistBytes+1))
		if err != nil {
			if r.Context().Err() != nil {
				return
			}
			httpx.Error(w, http.StatusBadGateway, fmt.Sprintf("upstream playlist read error: %v", err))
			return
		}
		if len(raw) > maxHLSPlaylistBytes {
			httpx.Error(w, http.StatusBadGateway, "upstream HLS playlist exceeds 2 MiB limit")
			return
		}

		token := extractRequestToken(r)
		proxyBase := "Stream"
		if r != nil && strings.Contains(strings.ToLower(r.URL.Path), "/livestreamfiles/") {
			proxyBase = "../../Channels/" + url.PathEscape(channelID) + "/Stream"
		}
		rewritten := h.rewriteHLSPlaylistWithPolicy(string(raw), baseURL, channelID, proxyBase, token, allowPrivateLAN)
		safeCT := sanitizeLiveTVProxyContentType(ct, "application/vnd.apple.mpegurl")
		w.Header().Set("Content-Type", safeCT)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "sandbox")
		w.Header().Set("Cache-Control", "no-cache, no-store")
		w.Header().Set("Content-Length", strconv.Itoa(len(rewritten)))
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, rewritten)
		return
	}

	safeCT := sanitizeLiveTVProxyContentType(ct, "video/mp2t")
	w.Header().Set("Content-Type", safeCT)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.WriteHeader(http.StatusOK)

	var dst io.Writer = w
	if f, ok := w.(http.Flusher); ok {
		dst = flushWriter{w: w, f: f}
	}
	_, _ = io.Copy(dst, resp.Body)
}

func sanitizeLiveTVProxyContentType(rawCT, fallbackCT string) string {
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
	if strings.HasPrefix(mediaType, "video/") || strings.HasPrefix(mediaType, "audio/") {
		return mediaType
	}
	switch mediaType {
	case "application/octet-stream",
		"application/vnd.apple.mpegurl",
		"application/x-mpegurl",
		"application/mp4",
		"application/mp2t",
		"text/vtt":
		return mediaType
	default:
		return "application/octet-stream"
	}
}

func isHLSPlaylistResponse(contentType string, u *url.URL) bool {
	ctLower := strings.ToLower(contentType)
	if strings.Contains(ctLower, "mpegurl") || strings.Contains(ctLower, "x-mpegurl") {
		return true
	}
	if u != nil && strings.HasSuffix(strings.ToLower(u.Path), ".m3u8") {
		return true
	}
	return false
}

func (h *Handler) rewriteHLSPlaylist(playlist string, baseURL *url.URL, channelID, proxyBase, token string) string {
	allowPrivateLAN := false
	if baseURL != nil {
		allowPrivateLAN = h.channelAllowsPrivateLAN(baseURL.String())
	}
	return h.rewriteHLSPlaylistWithPolicy(playlist, baseURL, channelID, proxyBase, token, allowPrivateLAN)
}

func (h *Handler) rewriteHLSPlaylistWithPolicy(playlist string, baseURL *url.URL, channelID, proxyBase, token string, allowPrivateLAN bool) string {
	if proxyBase == "" {
		proxyBase = "Stream"
	}
	exp := time.Now().UTC().Add(proxyTargetTTL).Unix()
	lanFlag := "0"
	if allowPrivateLAN {
		lanFlag = "1"
	}
	rewriteURI := func(rawURI string) string {
		trimmed := strings.TrimSpace(rawURI)
		if trimmed == "" || baseURL == nil {
			return rawURI
		}
		ref, err := url.Parse(trimmed)
		if err != nil {
			return rawURI
		}
		resolved := baseURL.ResolveReference(ref)
		scheme := strings.ToLower(resolved.Scheme)
		if scheme != "http" && scheme != "https" {
			return rawURI
		}
		resolvedStr := resolved.String()
		if err := h.validateStreamURL(resolvedStr, allowPrivateLAN); err != nil {
			return rawURI
		}
		sig := h.signProxyTarget(channelID, resolvedStr, allowPrivateLAN, exp)
		proxied := proxyBase + "?target=" + url.QueryEscape(resolvedStr) + "&exp=" + strconv.FormatInt(exp, 10) + "&lan=" + lanFlag + "&sig=" + sig
		if token != "" {
			proxied += "&api_key=" + url.QueryEscape(token)
		}
		return proxied
	}

	lines := strings.Split(playlist, "\n")
	for i, line := range lines {
		cr := ""
		if strings.HasSuffix(line, "\r") {
			cr = "\r"
			line = strings.TrimSuffix(line, "\r")
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			if strings.Contains(line, `URI="`) {
				lines[i] = hlsURIAttributeRe.ReplaceAllStringFunc(line, func(match string) string {
					sub := hlsURIAttributeRe.FindStringSubmatch(match)
					if len(sub) < 2 {
						return match
					}
					return `URI="` + rewriteURI(sub[1]) + `"`
				}) + cr
			}
			continue
		}
		lines[i] = rewriteURI(trimmed) + cr
	}
	return strings.Join(lines, "\n")
}

func extractRequestToken(r *http.Request) string {
	if r == nil {
		return ""
	}
	if v := strings.TrimSpace(httpx.ParseQuery(r).Get("api_key")); v != "" {
		return v
	}
	if v := strings.TrimSpace(r.Header.Get("X-Emby-Token")); v != "" {
		return v
	}
	if v := strings.TrimSpace(r.Header.Get("X-MediaBrowser-Token")); v != "" {
		return v
	}
	authHeader := strings.TrimSpace(r.Header.Get("X-Emby-Authorization"))
	if authHeader == "" {
		authHeader = strings.TrimSpace(r.Header.Get("Authorization"))
	}
	if authHeader == "" {
		return ""
	}
	return parseTokenFromAuthHeader(authHeader)
}

func parseTokenFromAuthHeader(header string) string {
	s := strings.TrimSpace(header)
	if idx := strings.IndexAny(s, " \t"); idx > 0 {
		scheme := strings.ToLower(s[:idx])
		rest := strings.TrimSpace(s[idx+1:])
		if scheme == "bearer" {
			return rest
		}
		if scheme == "mediabrowser" || scheme == "emby" {
			s = rest
		}
	}
	for len(s) > 0 {
		s = strings.TrimLeft(s, " ,\t")
		if len(s) == 0 {
			break
		}
		eq := strings.IndexByte(s, '=')
		if eq < 0 {
			break
		}
		key := strings.ToLower(strings.TrimSpace(s[:eq]))
		s = strings.TrimLeft(s[eq+1:], " \t")
		var val string
		if len(s) > 0 && s[0] == '"' {
			s = s[1:]
			if end := strings.IndexByte(s, '"'); end >= 0 {
				val = s[:end]
				s = s[end+1:]
			} else {
				val = s
				s = ""
			}
		} else if comma := strings.IndexByte(s, ','); comma >= 0 {
			val = strings.TrimSpace(s[:comma])
			s = s[comma+1:]
		} else {
			val = strings.TrimSpace(s)
			s = ""
		}
		if key == "token" && val != "" {
			return val
		}
	}
	return ""
}

type programQuery struct {
	ChannelIds   []string
	SearchTerm   string
	IsMovie      *bool
	IsSeries     *bool
	MinStartDate *time.Time
	MaxStartDate *time.Time
	MinEndDate   *time.Time
	MaxEndDate   *time.Time
	HasAired     *bool
	IsAiring     *bool
	SortBy       []string
	SortOrder    string
	StartIndex   int
	Limit        int
}

type flexStringList []string

func (f *flexStringList) UnmarshalJSON(data []byte) error {
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		var out []string
		for _, item := range arr {
			for _, p := range strings.Split(item, ",") {
				if s := strings.TrimSpace(p); s != "" {
					out = append(out, s)
				}
			}
		}
		*f = out
		return nil
	}
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		var out []string
		for _, p := range strings.Split(single, ",") {
			if s := strings.TrimSpace(p); s != "" {
				out = append(out, s)
			}
		}
		*f = out
		return nil
	}
	return nil
}

func parseQueryTime(raw string) *time.Time {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			u := t.UTC()
			return &u
		}
	}
	return nil
}

func (h *Handler) handleListPrograms(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	h.writeFilteredPrograms(w, programQuery{
		ChannelIds:   q.List("ChannelIds"),
		SearchTerm:   q.Get("SearchTerm"),
		IsMovie:      q.Bool("IsMovie"),
		IsSeries:     q.Bool("IsSeries"),
		MinStartDate: parseQueryTime(q.Get("MinStartDate")),
		MaxStartDate: parseQueryTime(q.Get("MaxStartDate")),
		MinEndDate:   parseQueryTime(q.Get("MinEndDate")),
		MaxEndDate:   parseQueryTime(q.Get("MaxEndDate")),
		HasAired:     q.Bool("HasAired"),
		IsAiring:     q.Bool("IsAiring"),
		SortBy:       q.List("SortBy"),
		SortOrder:    q.Get("SortOrder"),
		StartIndex:   q.Int("StartIndex", 0),
		Limit:        q.Int("Limit", 0),
	})
}

func (h *Handler) handleQueryProgramsPost(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ChannelIds   []string       `json:"ChannelIds"`
		SearchTerm   string         `json:"SearchTerm"`
		IsMovie      *bool          `json:"IsMovie"`
		IsSeries     *bool          `json:"IsSeries"`
		MinStartDate string         `json:"MinStartDate"`
		MaxStartDate string         `json:"MaxStartDate"`
		MinEndDate   string         `json:"MinEndDate"`
		MaxEndDate   string         `json:"MaxEndDate"`
		HasAired     *bool          `json:"HasAired"`
		IsAiring     *bool          `json:"IsAiring"`
		SortBy       flexStringList `json:"SortBy"`
		SortOrder    string         `json:"SortOrder"`
		StartIndex   int            `json:"StartIndex"`
		Limit        int            `json:"Limit"`
	}
	if err := httpx.DecodeJSONLimit(r, &body, 0); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid program query payload")
		return
	}
	h.writeFilteredPrograms(w, programQuery{
		ChannelIds:   body.ChannelIds,
		SearchTerm:   body.SearchTerm,
		IsMovie:      body.IsMovie,
		IsSeries:     body.IsSeries,
		MinStartDate: parseQueryTime(body.MinStartDate),
		MaxStartDate: parseQueryTime(body.MaxStartDate),
		MinEndDate:   parseQueryTime(body.MinEndDate),
		MaxEndDate:   parseQueryTime(body.MaxEndDate),
		HasAired:     body.HasAired,
		IsAiring:     body.IsAiring,
		SortBy:       []string(body.SortBy),
		SortOrder:    body.SortOrder,
		StartIndex:   body.StartIndex,
		Limit:        body.Limit,
	})
}

func (h *Handler) writeFilteredPrograms(w http.ResponseWriter, pq programQuery) {
	chSet := make(map[string]struct{}, len(pq.ChannelIds))
	for _, cid := range pq.ChannelIds {
		chSet[cid] = struct{}{}
	}
	search := strings.ToLower(strings.TrimSpace(pq.SearchTerm))
	now := time.Now().UTC()

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
		if pq.IsMovie != nil && p.IsMovie != *pq.IsMovie {
			continue
		}
		if pq.IsSeries != nil && p.IsSeries != *pq.IsSeries {
			continue
		}
		if pq.MinStartDate != nil && p.StartDate.Before(*pq.MinStartDate) {
			continue
		}
		if pq.MaxStartDate != nil && p.StartDate.After(*pq.MaxStartDate) {
			continue
		}
		if pq.MinEndDate != nil && p.EndDate.Before(*pq.MinEndDate) {
			continue
		}
		if pq.MaxEndDate != nil && p.EndDate.After(*pq.MaxEndDate) {
			continue
		}
		if pq.HasAired != nil {
			aired := !p.EndDate.IsZero() && !p.EndDate.After(now)
			if aired != *pq.HasAired {
				continue
			}
		}
		if pq.IsAiring != nil {
			airing := !p.StartDate.IsZero() && !p.EndDate.IsZero() && !p.StartDate.After(now) && p.EndDate.After(now)
			if airing != *pq.IsAiring {
				continue
			}
		}
		filtered = append(filtered, p)
	}
	h.mu.RUnlock()

	if len(pq.SortBy) > 0 {
		desc := strings.EqualFold(strings.TrimSpace(pq.SortOrder), "descending") ||
			strings.EqualFold(strings.TrimSpace(pq.SortOrder), "desc")
		sort.SliceStable(filtered, func(i, j int) bool {
			a, b := filtered[i], filtered[j]
			for _, key := range pq.SortBy {
				var cmp int
				switch strings.ToLower(strings.TrimSpace(key)) {
				case "startdate":
					cmp = compareTimes(a.StartDate, b.StartDate)
				case "enddate":
					cmp = compareTimes(a.EndDate, b.EndDate)
				case "name", "sortname":
					cmp = strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
				case "channelname":
					cmp = strings.Compare(strings.ToLower(a.ChannelName), strings.ToLower(b.ChannelName))
				}
				if cmp != 0 {
					if desc {
						return cmp > 0
					}
					return cmp < 0
				}
			}
			return false
		})
	}

	page, total := paginate(filtered, pq.StartIndex, pq.Limit)
	httpx.JSON(w, http.StatusOK, dto.QueryResult[ProgramInfo]{
		Items:            page,
		TotalRecordCount: total,
	})
}

func compareTimes(a, b time.Time) int {
	if a.Before(b) {
		return -1
	}
	if a.After(b) {
		return 1
	}
	return 0
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
	_, isAdmin, hasAuth := h.callerAuthInfo(r)
	hidePath := hasAuth && !isAdmin

	h.mu.RLock()
	filtered := make([]RecordingInfo, 0, len(h.state.Recordings))
	for _, rec := range h.state.Recordings {
		if chID != "" && rec.ChannelId != chID {
			continue
		}
		if status != "" && strings.ToLower(rec.Status) != status {
			continue
		}
		if hidePath {
			rec.Path = ""
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
	_, isAdmin, hasAuth := h.callerAuthInfo(r)
	hidePath := hasAuth && !isAdmin
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, rec := range h.state.Recordings {
		if rec.Id == id {
			if hidePath {
				rec.Path = ""
			}
			httpx.JSON(w, http.StatusOK, rec)
			return
		}
	}
	httpx.Error(w, http.StatusNotFound, "recording not found")
}

// DeleteRecording removes a recording by ID, persists state, and deletes its file if inside RecordingsDir.
func (h *Handler) DeleteRecording(ctx context.Context, id string) bool {
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
		return false
	}
	recPath := strings.TrimSpace(h.state.Recordings[idx].Path)
	recorder := h.recorder
	h.state.Recordings = append(h.state.Recordings[:idx], h.state.Recordings[idx+1:]...)
	h.saveStateLocked(ctx)
	h.mu.Unlock()

	if recorder != nil && recPath != "" {
		if resolvedFile, ok := resolvePathWithinDir(recorder.RecordingsDir(), recPath); ok {
			if err := os.Remove(resolvedFile); err != nil && !errors.Is(err, os.ErrNotExist) {
				// Best-effort file cleanup; recording record has already been removed.
				_ = err
			}
		}
	}
	return true
}

func (h *Handler) handleDeleteRecording(w http.ResponseWriter, r *http.Request) {
	if !h.DeleteRecording(r.Context(), r.PathValue("Id")) {
		httpx.Error(w, http.StatusNotFound, "recording not found")
		return
	}
	httpx.NoContent(w)
}

func resolvePathWithinDir(dir, target string) (string, bool) {
	dir = strings.TrimSpace(dir)
	target = strings.TrimSpace(target)
	if dir == "" || target == "" {
		return "", false
	}
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", false
	}
	resolvedDir = filepath.Clean(resolvedDir)

	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", false
	}
	resolvedTarget = filepath.Clean(resolvedTarget)

	if resolvedTarget == resolvedDir || !isPathWithinRoot(resolvedDir, resolvedTarget) {
		return "", false
	}
	return resolvedTarget, true
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
	if _, isAdmin, hasAuth := h.callerAuthInfo(r); hasAuth && !isAdmin {
		dto.SanitizeItemPathsSlice(folders)
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

func validateTimerBounds(start, end time.Time, prePad, postPad int) error {
	if prePad < 0 || prePad > maxPaddingSeconds {
		return fmt.Errorf("PrePaddingSeconds must be between 0 and %d", maxPaddingSeconds)
	}
	if postPad < 0 || postPad > maxPaddingSeconds {
		return fmt.Errorf("PostPaddingSeconds must be between 0 and %d", maxPaddingSeconds)
	}
	if !end.After(start) {
		return errors.New("EndDate must be after StartDate")
	}
	if end.Sub(start) > maxTimerDuration {
		return errors.New("recording duration cannot exceed 24 hours")
	}
	return nil
}

func (h *Handler) handleCreateTimer(w http.ResponseWriter, r *http.Request) {
	var t TimerInfo
	if err := httpx.DecodeJSONLimit(r, &t, 0); err != nil {
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
	if err := validateTimerBounds(t.StartDate, t.EndDate, t.PrePaddingSeconds, t.PostPaddingSeconds); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	h.mu.Lock()
	h.state.Timers = append(h.state.Timers, t)
	err := h.saveStateLocked(r.Context())
	h.mu.Unlock()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to persist timer")
		return
	}
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
	if err := httpx.DecodeJSONLimit(r, &upd, 0); err != nil {
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
			if upd.StartDate.IsZero() {
				upd.StartDate = existing.StartDate
			}
			if upd.EndDate.IsZero() {
				upd.EndDate = existing.EndDate
			}
			if err := validateTimerBounds(upd.StartDate, upd.EndDate, upd.PrePaddingSeconds, upd.PostPaddingSeconds); err != nil {
				h.mu.Unlock()
				httpx.Error(w, http.StatusBadRequest, err.Error())
				return
			}
			h.state.Timers[i] = upd
			err := h.saveStateLocked(r.Context())
			h.mu.Unlock()
			if err != nil {
				httpx.Error(w, http.StatusInternalServerError, "failed to persist timer")
				return
			}
			httpx.JSON(w, http.StatusOK, upd)
			return
		}
	}
	h.mu.Unlock()
	httpx.Error(w, http.StatusNotFound, "timer not found")
}

// DeleteTimer removes a timer by ID, persists state, and cancels any active DVR recording.
func (h *Handler) DeleteTimer(ctx context.Context, id string) bool {
	h.mu.Lock()
	for i, t := range h.state.Timers {
		if t.Id == id {
			recorder := h.recorder
			h.state.Timers = append(h.state.Timers[:i], h.state.Timers[i+1:]...)
			h.saveStateLocked(ctx)
			h.mu.Unlock()
			if recorder != nil {
				_ = recorder.CancelRecording(id)
			}
			return true
		}
	}
	h.mu.Unlock()
	return false
}

func (h *Handler) handleDeleteTimer(w http.ResponseWriter, r *http.Request) {
	if !h.DeleteTimer(r.Context(), r.PathValue("Id")) {
		httpx.Error(w, http.StatusNotFound, "timer not found")
		return
	}
	httpx.NoContent(w)
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
	if err := httpx.DecodeJSONLimit(r, &st, 0); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid series timer payload")
		return
	}
	if st.PrePaddingSeconds < 0 || st.PrePaddingSeconds > maxPaddingSeconds || st.PostPaddingSeconds < 0 || st.PostPaddingSeconds > maxPaddingSeconds {
		httpx.Error(w, http.StatusBadRequest, fmt.Sprintf("padding seconds must be between 0 and %d", maxPaddingSeconds))
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
	h.scheduleTimersForSeriesLocked(st)
	err := h.saveStateLocked(r.Context())
	h.mu.Unlock()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to persist series timer")
		return
	}
	httpx.JSON(w, http.StatusOK, st)
}

func (h *Handler) scheduleTimersForSeriesLocked(st SeriesTimerInfo) {
	seriesID := strings.TrimSpace(st.SeriesId)
	seriesName := strings.TrimSpace(st.Name)
	if seriesID == "" && seriesName == "" {
		return
	}
	daySet := make(map[string]struct{}, len(st.Days))
	for _, d := range st.Days {
		if k := strings.ToLower(strings.TrimSpace(d)); k != "" {
			daySet[k] = struct{}{}
		}
	}

	now := time.Now().UTC()
	serverID := h.serverID()
	disabledChannels := make(map[string]bool)
	for _, ch := range h.state.Channels {
		if ch.Disabled {
			disabledChannels[ch.Id] = true
		}
	}

	for _, p := range h.state.Programs {
		if disabledChannels[p.ChannelId] {
			continue
		}
		if !p.EndDate.IsZero() && !p.EndDate.After(now) {
			continue
		}
		if !st.RecordAnyChannel && st.ChannelId != "" && p.ChannelId != st.ChannelId {
			continue
		}
		matched := false
		if seriesID != "" && p.SeriesId != "" && strings.EqualFold(strings.TrimSpace(p.SeriesId), seriesID) {
			matched = true
		} else if seriesName != "" && strings.EqualFold(strings.TrimSpace(p.Name), seriesName) {
			matched = true
		}
		if !matched {
			continue
		}
		if len(daySet) > 0 && !p.StartDate.IsZero() {
			if _, ok := daySet[strings.ToLower(p.StartDate.UTC().Weekday().String())]; !ok {
				continue
			}
		}

		// Avoid creating duplicate timers for the same program or time slot.
		duplicate := false
		for _, existing := range h.state.Timers {
			if p.Id != "" && existing.ProgramId == p.Id {
				duplicate = true
				break
			}
			if existing.SeriesTimerId == st.Id &&
				existing.ChannelId == p.ChannelId &&
				existing.StartDate.Equal(p.StartDate) &&
				strings.EqualFold(existing.Name, p.Name) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}

		h.state.Timers = append(h.state.Timers, TimerInfo{
			Id:                    guid.New(),
			ServerId:              serverID,
			ChannelId:             p.ChannelId,
			ChannelName:           p.ChannelName,
			ProgramId:             p.Id,
			SeriesTimerId:         st.Id,
			Name:                  p.Name,
			Overview:              p.Overview,
			StartDate:             p.StartDate,
			EndDate:               p.EndDate,
			Status:                "New",
			PrePaddingSeconds:     st.PrePaddingSeconds,
			PostPaddingSeconds:    st.PostPaddingSeconds,
			IsPrePaddingRequired:  st.IsPrePaddingRequired,
			IsPostPaddingRequired: st.IsPostPaddingRequired,
		})
	}
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
	if err := httpx.DecodeJSONLimit(r, &upd, 0); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid series timer payload")
		return
	}
	if upd.PrePaddingSeconds < 0 || upd.PrePaddingSeconds > maxPaddingSeconds || upd.PostPaddingSeconds < 0 || upd.PostPaddingSeconds > maxPaddingSeconds {
		httpx.Error(w, http.StatusBadRequest, fmt.Sprintf("padding seconds must be between 0 and %d", maxPaddingSeconds))
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
			h.scheduleTimersForSeriesLocked(upd)
			err := h.saveStateLocked(r.Context())
			h.mu.Unlock()
			if err != nil {
				httpx.Error(w, http.StatusInternalServerError, "failed to persist series timer")
				return
			}
			httpx.JSON(w, http.StatusOK, upd)
			return
		}
	}
	h.mu.Unlock()
	httpx.Error(w, http.StatusNotFound, "series timer not found")
}

// DeleteSeriesTimer removes a series timer by ID, cascade-deletes its pending single timers, and persists state.
func (h *Handler) DeleteSeriesTimer(ctx context.Context, id string) bool {
	h.mu.Lock()
	idx := -1
	for i, st := range h.state.SeriesTimers {
		if st.Id == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		h.mu.Unlock()
		return false
	}
	h.state.SeriesTimers = append(h.state.SeriesTimers[:idx], h.state.SeriesTimers[idx+1:]...)
	keptTimers := make([]TimerInfo, 0, len(h.state.Timers))
	for _, t := range h.state.Timers {
		if t.SeriesTimerId == id && (t.Status == "" || t.Status == "New") {
			continue
		}
		keptTimers = append(keptTimers, t)
	}
	h.state.Timers = keptTimers
	h.saveStateLocked(ctx)
	h.mu.Unlock()
	return true
}

func (h *Handler) handleDeleteSeriesTimer(w http.ResponseWriter, r *http.Request) {
	if !h.DeleteSeriesTimer(r.Context(), r.PathValue("Id")) {
		httpx.Error(w, http.StatusNotFound, "series timer not found")
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) handleSaveTunerHost(w http.ResponseWriter, r *http.Request) {
	var host TunerHostInfo
	if err := httpx.DecodeJSONLimit(r, &host, maxM3UPlaylistBytes); err != nil {
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
	// Do not persist raw inline M3U playlist content after parsing.
	host.M3UContent = ""

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
	err := h.saveStateLocked(r.Context())
	h.mu.Unlock()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to persist tuner host")
		return
	}

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
				if err := netutil.ValidateRemoteURLNoLoopback(trimmed, true); err != nil {
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
			return ParseM3U(io.LimitReader(resp.Body, maxM3UPlaylistBytes))
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
	return ParseM3U(io.LimitReader(f, maxM3UPlaylistBytes))
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

// DeleteTunerHost removes a tuner host by ID along with its channels and programs, and persists state.
func (h *Handler) DeleteTunerHost(ctx context.Context, id string) bool {
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
		return false
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
	h.saveStateLocked(ctx)
	h.mu.Unlock()
	return true
}

func (h *Handler) handleDeleteTunerHost(w http.ResponseWriter, r *http.Request) {
	id := httpx.ParseQuery(r).Get("id")
	if id == "" {
		httpx.Error(w, http.StatusBadRequest, "id query parameter is required")
		return
	}
	if !h.DeleteTunerHost(r.Context(), id) {
		httpx.Error(w, http.StatusNotFound, "tuner host not found")
		return
	}
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
	if err := httpx.DecodeJSONLimit(r, &lp, 0); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid listing provider payload")
		return
	}
	if lp.Id == "" {
		lp.Id = guid.New()
	}
	if lp.Type == "" {
		lp.Type = "xmltv"
	}

	var guide *XMLTVGuide
	if strings.EqualFold(lp.Type, "xmltv") && strings.TrimSpace(lp.Path) != "" {
		parsed, err := h.loadXMLTVFromSource(r.Context(), lp.Path)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, fmt.Sprintf("failed to load xmltv guide: %v", err))
			return
		}
		guide = parsed
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
	if guide != nil && len(guide.Programmes) > 0 {
		h.applyXMLTVGuideLocked(guide)
		for _, st := range h.state.SeriesTimers {
			h.scheduleTimersForSeriesLocked(st)
		}
	}
	err := h.saveStateLocked(r.Context())
	h.mu.Unlock()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to persist listing provider")
		return
	}
	httpx.JSON(w, http.StatusOK, lp)
}

func (h *Handler) loadXMLTVFromSource(ctx context.Context, src string) (*XMLTVGuide, error) {
	trimmed := strings.TrimSpace(src)
	if trimmed == "" {
		return nil, errors.New("xmltv source is empty")
	}

	if u, err := url.Parse(trimmed); err == nil && u.Scheme != "" {
		scheme := strings.ToLower(u.Scheme)
		if scheme == "http" || scheme == "https" {
			if !h.customHTTPClient {
				if err := netutil.ValidateRemoteURLNoLoopback(trimmed, true); err != nil {
					return nil, fmt.Errorf("blocked xmltv url: %w", err)
				}
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, trimmed, nil)
			if err != nil {
				return nil, err
			}
			resp, err := h.httpClient.Do(req)
			if err != nil {
				return nil, err
			}
			defer resp.Body.Close()
			if resp.StatusCode >= 400 {
				return nil, fmt.Errorf("unexpected xmltv status %d", resp.StatusCode)
			}
			return ParseXMLTV(io.LimitReader(resp.Body, 50<<20))
		}
		if len(scheme) > 1 {
			return nil, fmt.Errorf("unsupported xmltv url scheme %q", u.Scheme)
		}
	}

	resolvedPath, err := h.validateLocalXMLTVPath(ctx, trimmed)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(resolvedPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseXMLTV(io.LimitReader(f, maxXMLTVDecompressedBytes+1))
}

func (h *Handler) validateLocalXMLTVPath(ctx context.Context, rawPath string) (string, error) {
	if !hasXMLTVExt(rawPath) {
		return "", fmt.Errorf("local xmltv file must have .xml or .xml.gz extension")
	}
	resolvedPath, err := filepath.EvalSymlinks(rawPath)
	if err != nil {
		return "", err
	}
	resolvedPath = filepath.Clean(resolvedPath)
	if !hasXMLTVExt(resolvedPath) {
		return "", fmt.Errorf("resolved local xmltv target must have .xml or .xml.gz extension")
	}
	allowedRoots := h.allowedLocalM3URoots(ctx)
	for _, root := range allowedRoots {
		if isPathWithinRoot(root, resolvedPath) {
			return resolvedPath, nil
		}
	}
	return "", fmt.Errorf("local xmltv file path is outside allowed directories")
}

func (h *Handler) applyXMLTVGuideLocked(guide *XMLTVGuide) {
	if guide == nil || len(guide.Programmes) == 0 || len(h.state.Channels) == 0 {
		return
	}

	byTvgID := make(map[string]ChannelInfo, len(h.state.Channels))
	byID := make(map[string]ChannelInfo, len(h.state.Channels))
	byName := make(map[string]ChannelInfo, len(h.state.Channels))
	for _, ch := range h.state.Channels {
		if k := strings.ToLower(strings.TrimSpace(ch.TvgId)); k != "" {
			byTvgID[k] = ch
		}
		if k := strings.ToLower(strings.TrimSpace(ch.Id)); k != "" {
			byID[k] = ch
		}
		if k := strings.ToLower(strings.TrimSpace(ch.Name)); k != "" {
			byName[k] = ch
		}
	}

	xmlDisplayNames := make(map[string][]string, len(guide.Channels))
	for _, xc := range guide.Channels {
		if k := strings.ToLower(strings.TrimSpace(xc.ID)); k != "" {
			xmlDisplayNames[k] = xc.DisplayNames
		}
	}

	matchChannel := func(xmlChID string) (ChannelInfo, bool) {
		key := strings.ToLower(strings.TrimSpace(xmlChID))
		if key == "" {
			return ChannelInfo{}, false
		}
		if ch, ok := byTvgID[key]; ok {
			return ch, true
		}
		if ch, ok := byID[key]; ok {
			return ch, true
		}
		if ch, ok := byName[key]; ok {
			return ch, true
		}
		for _, dn := range xmlDisplayNames[key] {
			dnKey := strings.ToLower(strings.TrimSpace(dn))
			if dnKey == "" {
				continue
			}
			if ch, ok := byName[dnKey]; ok {
				return ch, true
			}
			if ch, ok := byTvgID[dnKey]; ok {
				return ch, true
			}
		}
		return ChannelInfo{}, false
	}

	matchedChannelIDs := make(map[string]struct{})
	newPrograms := make([]ProgramInfo, 0, len(guide.Programmes))
	serverID := h.serverID()

	for _, prog := range guide.Programmes {
		ch, ok := matchChannel(prog.Channel)
		if !ok {
			continue
		}
		matchedChannelIDs[ch.Id] = struct{}{}
		isMovie, isSeries := classifyXMLTVCategories(prog.Categories)
		newPrograms = append(newPrograms, ProgramInfo{
			Id:          guid.New(),
			Name:        prog.Title,
			ServerId:    serverID,
			Type:        "Program",
			ChannelId:   ch.Id,
			ChannelName: ch.Name,
			Overview:    prog.Desc,
			StartDate:   prog.Start,
			EndDate:     prog.Stop,
			IsMovie:     isMovie,
			IsSeries:    isSeries,
		})
	}

	if len(matchedChannelIDs) == 0 {
		return
	}

	kept := make([]ProgramInfo, 0, len(h.state.Programs)+len(newPrograms))
	for _, p := range h.state.Programs {
		if _, replace := matchedChannelIDs[p.ChannelId]; !replace {
			kept = append(kept, p)
		}
	}
	kept = append(kept, newPrograms...)
	h.state.Programs = kept
}

// DeleteListingProvider removes a listing provider by ID and persists state.
func (h *Handler) DeleteListingProvider(ctx context.Context, id string) bool {
	h.mu.Lock()
	for i, lp := range h.state.ListingProviders {
		if lp.Id == id {
			h.state.ListingProviders = append(h.state.ListingProviders[:i], h.state.ListingProviders[i+1:]...)
			h.saveStateLocked(ctx)
			h.mu.Unlock()
			return true
		}
	}
	h.mu.Unlock()
	return false
}

func (h *Handler) handleDeleteListingProvider(w http.ResponseWriter, r *http.Request) {
	id := httpx.ParseQuery(r).Get("id")
	if id == "" {
		httpx.Error(w, http.StatusBadRequest, "id query parameter is required")
		return
	}
	if !h.DeleteListingProvider(r.Context(), id) {
		httpx.Error(w, http.StatusNotFound, "listing provider not found")
		return
	}
	httpx.NoContent(w)
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

func (h *Handler) saveStateLocked(ctx context.Context) error {
	if h.store == nil {
		return nil
	}
	raw, err := json.Marshal(h.state)
	if err != nil {
		slog.Error("livetv: marshal state failed", "err", err)
		return err
	}
	_, err = h.store.Writer.ExecContext(ctx, `
		INSERT INTO server_config (key, value_json, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			value_json = excluded.value_json,
			updated_at = excluded.updated_at`,
		stateConfigKey, string(raw), time.Now().UTC().Unix(),
	)
	if err != nil {
		slog.Error("livetv: save state failed", "err", err)
	}
	return err
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
