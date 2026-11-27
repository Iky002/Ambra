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
	"net"
	"net/http"
	"net/netip"
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
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/pkg/netutil"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

const (
	stateConfigKey              = "livetv:state"
	maxM3UPlaylistBytes         = 20 << 20
	maxTimerDuration            = 24 * time.Hour
	maxPaddingSeconds           = 3600
	proxyTargetTTL              = 10 * time.Minute
	maxLiveStreamSessionSeconds = int64(24 * 60 * 60)
	maxTimers                   = 1000
	maxSeriesTimers             = 200
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
	Id            string    `json:"Id"`
	Name          string    `json:"Name"`
	EpisodeTitle  string    `json:"EpisodeTitle,omitempty"`
	EpisodeNumber string    `json:"EpisodeNumber,omitempty"`
	ServerId      string    `json:"ServerId,omitempty"`
	Type          string    `json:"Type"`
	ChannelId     string    `json:"ChannelId"`
	ChannelName   string    `json:"ChannelName,omitempty"`
	SeriesId      string    `json:"SeriesId,omitempty"`
	Overview      string    `json:"Overview,omitempty"`
	StartDate     time.Time `json:"StartDate"`
	EndDate       time.Time `json:"EndDate"`
	IsMovie       bool      `json:"IsMovie,omitempty"`
	IsSeries      bool      `json:"IsSeries,omitempty"`
	IsLive        bool      `json:"IsLive,omitempty"`
	IsRepeat      bool      `json:"IsRepeat,omitempty"`
	Source        string    `json:"Source,omitempty"`
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

type ListingProviderInfo struct {
	Id                  string `json:"Id"`
	Type                string `json:"Type"`
	Path                string `json:"Path,omitempty"`
	Username            string `json:"Username,omitempty"`
	Listings            string `json:"ListingsId,omitempty"`
	Country             string `json:"Country,omitempty"`
	AllowPrivateNetwork bool   `json:"AllowPrivateNetwork,omitempty"`
	NetworkPolicySet    bool   `json:"NetworkPolicySet,omitempty"`
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

const defaultLiveStreamLeaseTTL = 45 * time.Second

type liveStreamLease struct {
	release func()
	timer   *time.Timer
}

type Handler struct {
	store              *sqlite.Store
	cfg                *config.Config
	userRepo           *sqlite.UserRepo
	libRepo            *sqlite.LibraryRepo
	httpClient         *http.Client
	initHTTPClient     *http.Client
	publicHTTPClient   *http.Client
	streamClient       *http.Client
	publicStreamClient *http.Client
	customHTTPClient   bool
	customStreamClient bool
	proxySecret        []byte
	nowFn              func() time.Time
	recorder           *dvr.Recorder
	transcodeMgr       *transcode.Manager
	streamHub          *LiveStreamHub
	auth               AuthFuncs

	leaseMu      sync.Mutex
	streamLeases map[string]*liveStreamLease
	leaseTTL     time.Duration
	closed       bool

	mu    sync.RWMutex
	state liveTVState
}

func New(store *sqlite.Store, cfg *config.Config) *Handler {
	secret := make([]byte, 32)
	_, _ = rand.Read(secret)
	lanHTTP := netutil.NewSafeHTTPClientNoLoopback(15*time.Second, true)
	h := &Handler{
		store:              store,
		cfg:                cfg,
		httpClient:         lanHTTP,
		initHTTPClient:     lanHTTP,
		publicHTTPClient:   netutil.NewSafeHTTPClientNoLoopback(15*time.Second, false),
		streamClient:       netutil.NewSafeHTTPClientNoLoopback(0, true),
		publicStreamClient: netutil.NewSafeHTTPClientNoLoopback(0, false),
		proxySecret:        secret,
		streamHub:          newLiveStreamHub(),
		streamLeases:       make(map[string]*liveStreamLease),
		leaseTTL:           defaultLiveStreamLeaseTTL,
	}
	h.loadState(context.Background())
	return h
}

func (h *Handler) hasCustomHTTPClient() bool {
	if h == nil {
		return false
	}
	return h.customHTTPClient || (h.httpClient != nil && h.initHTTPClient != nil && h.httpClient != h.initHTTPClient)
}

func (h *Handler) nowUTC() time.Time {
	if h != nil && h.nowFn != nil {
		return h.nowFn().UTC()
	}
	return time.Now().UTC()
}

// SetLibraryRepo attaches a LibraryRepo so completed DVR recordings are automatically registered as playable media items.
func (h *Handler) SetLibraryRepo(r *sqlite.LibraryRepo) {
	h.mu.Lock()
	h.libRepo = r
	h.mu.Unlock()
}

func (h *Handler) resolveLibraryRepo() *sqlite.LibraryRepo {
	h.mu.RLock()
	r := h.libRepo
	st := h.store
	h.mu.RUnlock()
	if r != nil {
		return r
	}
	if st != nil {
		return sqlite.NewLibraryRepo(st)
	}
	return nil
}

// SetStreamIdleTimeout configures the upstream read idle timeout on the shared LiveStreamHub (default 30s).
func (h *Handler) SetStreamIdleTimeout(d time.Duration) {
	if h.streamHub != nil {
		h.streamHub.setIdleTimeout(d)
	}
}

// SetStreamGracePeriod configures the post-disconnect upstream teardown delay on the shared LiveStreamHub.
func (h *Handler) SetStreamGracePeriod(d time.Duration) {
	if h.streamHub != nil {
		h.streamHub.setGracePeriod(d)
	}
}

// Close releases any shared upstream live stream connections and active HLS stream leases managed by this Handler.
func (h *Handler) Close() {
	if h == nil {
		return
	}
	h.leaseMu.Lock()
	h.closed = true
	releases := make([]func(), 0, len(h.streamLeases))
	for k, lease := range h.streamLeases {
		if lease.timer != nil {
			lease.timer.Stop()
		}
		if lease.release != nil {
			releases = append(releases, lease.release)
		}
		delete(h.streamLeases, k)
	}
	h.leaseMu.Unlock()
	for _, rel := range releases {
		rel()
	}
	if h.streamHub != nil {
		h.streamHub.Close()
	}
}

func (h *Handler) releaseStreamLeases() {
	if h == nil {
		return
	}
	h.leaseMu.Lock()
	releases := make([]func(), 0, len(h.streamLeases))
	for k, lease := range h.streamLeases {
		if lease.timer != nil {
			lease.timer.Stop()
		}
		if lease.release != nil {
			releases = append(releases, lease.release)
		}
		delete(h.streamLeases, k)
	}
	h.leaseMu.Unlock()
	for _, rel := range releases {
		rel()
	}
}

func (h *Handler) expireStreamLease(key string, target *liveStreamLease) {
	h.leaseMu.Lock()
	cur, ok := h.streamLeases[key]
	if !ok || cur != target {
		h.leaseMu.Unlock()
		return
	}
	delete(h.streamLeases, key)
	rel := cur.release
	h.leaseMu.Unlock()
	if rel != nil {
		rel()
	}
}

func (h *Handler) touchOrAcquireLiveStreamLease(r *http.Request, channelID string, sessionIAT int64, uid string) bool {
	if h == nil || h.cfg == nil {
		return true
	}
	mgr := h.getTranscodeManager()
	if mgr == nil {
		return true
	}
	limit := h.cfg.Snapshot().SimultaneousStreamLimit
	if limit <= 0 {
		return true
	}

	var leaseKey string
	if normUID := guid.Normalize(uid); normUID != "" {
		leaseKey = "livetv-lease:u:" + normUID + ":" + strings.TrimSpace(channelID)
	} else if r != nil {
		leaseKey = "livetv-lease:ip:" + netutil.ClientIP(r) + ":" + strings.TrimSpace(channelID)
	} else if sessionIAT > 0 {
		leaseKey = "livetv-lease:iat:" + strconv.FormatInt(sessionIAT, 10) + ":" + strings.TrimSpace(channelID)
	} else {
		leaseKey = "livetv-lease:ch:" + strings.TrimSpace(channelID)
	}

	h.leaseMu.Lock()
	if h.closed {
		h.leaseMu.Unlock()
		return false
	}
	ttl := h.leaseTTL
	if ttl <= 0 {
		ttl = defaultLiveStreamLeaseTTL
	}
	if lease, ok := h.streamLeases[leaseKey]; ok {
		if lease.timer != nil {
			lease.timer.Reset(ttl)
		}
		h.leaseMu.Unlock()
		return true
	}
	h.leaseMu.Unlock()

	release, allowed := mgr.TryAcquireStreamSlot(limit, leaseKey)
	if !allowed {
		return false
	}

	h.leaseMu.Lock()
	if h.closed {
		h.leaseMu.Unlock()
		release()
		return false
	}
	if h.streamLeases == nil {
		h.streamLeases = make(map[string]*liveStreamLease)
	}
	if existing, ok := h.streamLeases[leaseKey]; ok {
		if existing.timer != nil {
			existing.timer.Reset(ttl)
		}
		h.leaseMu.Unlock()
		release()
		return true
	}
	lease := &liveStreamLease{release: release}
	lease.timer = time.AfterFunc(ttl, func() {
		h.expireStreamLease(leaseKey, lease)
	})
	h.streamLeases[leaseKey] = lease
	h.leaseMu.Unlock()
	return true
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

// SetTranscodeManager attaches an optional transcode Manager for global SimultaneousStreamLimit enforcement.
func (h *Handler) SetTranscodeManager(mgr *transcode.Manager) {
	h.mu.Lock()
	h.transcodeMgr = mgr
	h.mu.Unlock()
}

func (h *Handler) getTranscodeManager() *transcode.Manager {
	h.mu.RLock()
	mgr := h.transcodeMgr
	h.mu.RUnlock()
	return mgr
}

func (h *Handler) tryAcquireLiveStreamSlot(r *http.Request, channelID string) (func(), bool) {
	noop := func() {}
	if h == nil || h.cfg == nil {
		return noop, true
	}
	mgr := h.getTranscodeManager()
	if mgr == nil {
		return noop, true
	}
	limit := h.cfg.Snapshot().SimultaneousStreamLimit
	if limit <= 0 {
		return noop, true
	}
	slotKey := ""
	if r != nil && r.URL != nil {
		if ps := strings.TrimSpace(r.URL.Query().Get("PlaySessionId")); ps != "" {
			callerID, _, _ := h.callerAuthInfo(r)
			slotKey = "livetv:" + callerID + ":" + strings.TrimSpace(channelID) + ":" + ps
		}
	}
	return mgr.TryAcquireStreamSlot(limit, slotKey)
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

// SnapshotChannels returns a copy of all channels in memory.
func (h *Handler) SnapshotChannels() []ChannelInfo {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]ChannelInfo, len(h.state.Channels))
	copy(out, h.state.Channels)
	return out
}

// SnapshotPrograms returns a copy of all EPG programs in memory.
func (h *Handler) SnapshotPrograms() []ProgramInfo {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]ProgramInfo, len(h.state.Programs))
	copy(out, h.state.Programs)
	return out
}

// SnapshotRecordings returns a copy of all recordings in memory.
func (h *Handler) SnapshotRecordings() []RecordingInfo {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]RecordingInfo, len(h.state.Recordings))
	copy(out, h.state.Recordings)
	return out
}

// GetRecordingByID returns a copy of the recording with the given ID if it exists.
func (h *Handler) GetRecordingByID(id string) (*RecordingInfo, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for i := range h.state.Recordings {
		if h.state.Recordings[i].Id == id {
			rec := h.state.Recordings[i]
			return &rec, true
		}
	}
	return nil, false
}

// SnapshotTunerHosts returns a copy of all tuner hosts in memory.
func (h *Handler) SnapshotTunerHosts() []TunerHostInfo {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]TunerHostInfo, len(h.state.TunerHosts))
	copy(out, h.state.TunerHosts)
	return out
}

// SnapshotListingProviders returns a copy of all listing providers in memory.
func (h *Handler) SnapshotListingProviders() []ListingProviderInfo {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]ListingProviderInfo, len(h.state.ListingProviders))
	copy(out, h.state.ListingProviders)
	return out
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
		timerID         string
		channelName     string
		programName     string
		streamURL       string
		duration        time.Duration
		ch              ChannelInfo
		allowPrivateLAN bool
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
			ch:          ch,
		})
	}
	h.mu.RUnlock()

	if len(candidates) == 0 {
		return nil, nil
	}

	valid := make(map[string]candidate, len(candidates))
	for _, c := range candidates {
		allowPrivateLAN := h.channelAllowsPrivateLANForChannel(&c.ch)
		if err := netutil.ValidateRemoteURLNoLoopback(c.streamURL, allowPrivateLAN); err != nil {
			continue
		}
		c.allowPrivateLAN = allowPrivateLAN
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
		ua := ""
		if c.ch.TunerHostId != "" {
			for _, th := range h.state.TunerHosts {
				if th.Id == c.ch.TunerHostId {
					ua = strings.TrimSpace(th.UserAgent)
					break
				}
			}
		}
		jobs = append(jobs, dvr.RecordingJob{
			TimerID:        c.timerID,
			ChannelName:    c.channelName,
			ProgramName:    c.programName,
			StreamURL:      c.streamURL,
			UserAgent:      ua,
			Duration:       c.duration,
			DenyPrivateLAN: !c.allowPrivateLAN,
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
		h.publicHTTPClient = client
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
// appends a RecordingInfo entry and persists Live TV state. If the timer was deleted while
// recording, completed output is discarded rather than registered.
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
	var (
		changed      bool
		timerFound   bool
		channelID    string
		channelName  = state.Job.ChannelName
		programID    string
		recName      = state.Job.ProgramName
		newRecording *RecordingInfo
		recorder     = h.recorder
	)

	for i := range h.state.Timers {
		if h.state.Timers[i].Id == state.Job.TimerID {
			timerFound = true
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

	// If the timer was deleted while the recording was in flight, discard completed output.
	if !timerFound && strings.TrimSpace(state.Job.TimerID) != "" {
		h.mu.Unlock()
		if state.Status == dvr.StatusCompleted && strings.TrimSpace(state.OutputPath) != "" && recorder != nil {
			if resolvedFile, ok := resolvePathWithinDir(recorder.RecordingsDir(), state.OutputPath); ok {
				_ = os.Remove(resolvedFile)
			}
		}
		return
	}

	if state.Status == dvr.StatusCompleted {
		if recName == "" {
			if channelName != "" {
				recName = channelName
			} else {
				recName = "Recording"
			}
		}
		recEntry := RecordingInfo{
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
		}
		h.state.Recordings = append(h.state.Recordings, recEntry)
		newRecording = &recEntry
		changed = true
	}

	if changed {
		h.saveStateLocked(ctx)
	}
	h.mu.Unlock()

	if newRecording != nil && strings.TrimSpace(newRecording.Path) != "" {
		h.registerCompletedRecordingMedia(ctx, *newRecording)
	}
}

func (h *Handler) registerCompletedRecordingMedia(ctx context.Context, rec RecordingInfo) {
	if ctx == nil {
		ctx = context.Background()
	}
	libRepo := h.resolveLibraryRepo()
	if libRepo == nil {
		return
	}
	outPath := strings.TrimSpace(rec.Path)
	fi, err := os.Stat(outPath)
	if err != nil || fi.IsDir() {
		return
	}

	recDir := filepath.Dir(outPath)
	if h.recorder != nil && strings.TrimSpace(h.recorder.RecordingsDir()) != "" {
		recDir = h.recorder.RecordingsDir()
	}

	var libID string
	if libs, err := libRepo.ListLibraries(ctx); err == nil {
		for _, lib := range libs {
			if strings.EqualFold(lib.CollectionType, "livetv") || strings.EqualFold(lib.Name, "Recordings") {
				libID = lib.ID
				break
			}
		}
	}
	if libID == "" {
		if created, err := libRepo.CreateLibrary(ctx, "Recordings", "livetv", []string{recDir}); err == nil && created != nil {
			libID = created.ID
		}
	}
	if libID == "" {
		return
	}

	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(outPath), "."))
	if ext == "" {
		ext = "ts"
	}
	var rtPtr *int64
	if !rec.StartDate.IsZero() && rec.EndDate.After(rec.StartDate) {
		ticks := int64(rec.EndDate.Sub(rec.StartDate).Seconds() * 10_000_000)
		if ticks > 0 {
			rtPtr = &ticks
		}
	}
	var premPtr *int64
	if !rec.StartDate.IsZero() {
		ts := rec.StartDate.UTC().Unix()
		premPtr = &ts
	}

	itemRec := &sqlite.ItemRecord{
		ID:           rec.Id,
		LibraryID:    libID,
		ParentID:     libID,
		Type:         "Video",
		MediaType:    "Video",
		Name:         rec.Name,
		SortName:     strings.ToLower(rec.Name),
		Path:         outPath,
		Container:    ext,
		RuntimeTicks: rtPtr,
		PremiereDate: premPtr,
	}
	if err := libRepo.UpsertItem(ctx, itemRec); err != nil {
		return
	}
	_ = libRepo.ReplaceMediaSourcesAndStreams(ctx, rec.Id, []sqlite.MediaSourceRecord{
		{
			ID:        rec.Id,
			ItemID:    rec.Id,
			Name:      filepath.Base(outPath),
			Path:      outPath,
			Protocol:  "File",
			Container: ext,
			Size:      fi.Size(),
		},
	})
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
	mux.Handle("GET /LiveTv/Channels/{Id}/Stream", h.requireLiveTvStreamAccess(wrapAuth)(http.HandlerFunc(h.handleStreamChannel)))
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
	handleAdmin("POST /LiveTv/TunerHosts/{Id}/Refresh", h.handleRefreshTunerHost)
	handleAdmin("DELETE /LiveTv/TunerHosts", h.handleDeleteTunerHost)
	handleAuth("GET /LiveTv/TunerHosts/Types", h.handleTunerHostTypes)
	handleAdmin("POST /LiveTv/ListingProviders", h.handleSaveListingProvider)
	handleAdmin("DELETE /LiveTv/ListingProviders", h.handleDeleteListingProvider)
	handleLiveTvAccess("GET /LiveTv/GuideInfo", h.handleGuideInfo)
}

func (h *Handler) isSignedStreamUserAllowed(ctx context.Context, rawUID string) bool {
	uid := guid.Normalize(rawUID)
	if uid == "" {
		return true
	}
	ur := h.getUserRepo()
	if ur == nil {
		return true
	}
	u, err := ur.GetUserByID(ctx, uid)
	if err != nil || u == nil || u.IsDisabled || u.Policy.IsDisabled {
		return false
	}
	if u.IsAdmin || u.Policy.IsAdministrator {
		return true
	}
	return u.Policy.EnableLiveTvAccess && u.Policy.EnableMediaPlayback
}

func (h *Handler) validateSignedStreamTarget(r *http.Request) (valid bool, revoked bool) {
	if r == nil {
		return false, false
	}
	id := r.PathValue("Id")
	if id == "" {
		return false, false
	}
	q := r.URL.Query()
	target := strings.TrimSpace(q.Get("target"))
	sig := strings.TrimSpace(q.Get("sig"))
	expStr := strings.TrimSpace(q.Get("exp"))
	iatStr := strings.TrimSpace(q.Get("iat"))
	lanParam := strings.TrimSpace(q.Get("lan"))
	uidParam := strings.TrimSpace(q.Get("uid"))
	if target == "" || sig == "" || expStr == "" || (lanParam != "0" && lanParam != "1") {
		return false, false
	}
	reqAllowLAN := lanParam == "1"
	if iatStr != "" {
		if _, _, ok := h.verifyProxyTargetWithIATAndUser(id, target, reqAllowLAN, iatStr, expStr, uidParam, sig); !ok {
			return false, false
		}
	} else if !h.verifyProxyTargetWithUser(id, target, reqAllowLAN, expStr, uidParam, sig) {
		return false, false
	}
	ch, ok := h.GetChannelByID(id)
	if !ok || ch.Disabled {
		return false, false
	}
	if reqAllowLAN && !h.channelAllowsPrivateLANForChannel(ch) {
		return false, false
	}
	if uidParam != "" && !h.isSignedStreamUserAllowed(r.Context(), uidParam) {
		return false, true
	}
	return true, false
}

func (h *Handler) hasValidSignedStreamTarget(r *http.Request) bool {
	valid, _ := h.validateSignedStreamTarget(r)
	return valid
}

func (h *Handler) canCallerPlayMedia(r *http.Request) bool {
	if r == nil {
		return true
	}
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && (ac.User != nil || ac.IsAPIKey) {
		if ac.IsAPIKey {
			return true
		}
		if ac.User == nil || ac.User.IsDisabled {
			return false
		}
		if ur := h.getUserRepo(); ur != nil {
			uid := guid.Normalize(ac.User.ID)
			if uid == "" {
				return false
			}
			u, err := ur.GetUserByID(r.Context(), uid)
			if err != nil || u == nil || u.IsDisabled {
				return false
			}
			return u.IsAdmin || u.Policy.IsAdministrator || u.Policy.EnableMediaPlayback
		}
		return ac.IsAdmin() || ac.User.Policy.EnableMediaPlayback
	}
	if h.auth.UserIDFromCtx != nil {
		if ur := h.getUserRepo(); ur != nil {
			if uid := guid.Normalize(h.auth.UserIDFromCtx(r.Context())); uid != "" {
				u, err := ur.GetUserByID(r.Context(), uid)
				if err != nil || u == nil || u.IsDisabled {
					return false
				}
				return u.IsAdmin || u.Policy.IsAdministrator || u.Policy.EnableMediaPlayback
			}
		}
	}
	return true
}

func (h *Handler) requireLiveTvStreamAccess(wrapAuth func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	authGuarded := h.requireLiveTvAccess(wrapAuth)
	return func(next http.Handler) http.Handler {
		guarded := authGuarded(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !h.canCallerPlayMedia(r) {
				httpx.Error(w, http.StatusForbidden, "Media playback is disabled for this user")
				return
			}
			next.ServeHTTP(w, r)
		}))
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			valid, revoked := h.validateSignedStreamTarget(r)
			if revoked {
				httpx.Error(w, http.StatusForbidden, "Live TV stream access revoked for this user")
				return
			}
			if valid {
				next.ServeHTTP(w, r)
				return
			}
			guarded.ServeHTTP(w, r)
		})
	}
}

func (h *Handler) requireLiveTvAccess(wrapAuth func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return wrapAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && (ac.User != nil || ac.IsAPIKey) {
				if !ac.IsAPIKey {
					if ac.User == nil || ac.User.IsDisabled {
						httpx.Error(w, http.StatusForbidden, "Live TV access is disabled for this user")
						return
					}
					if ur := h.getUserRepo(); ur != nil {
						uid := guid.Normalize(ac.User.ID)
						if uid == "" {
							httpx.Error(w, http.StatusForbidden, "Live TV access is disabled for this user")
							return
						}
						u, err := ur.GetUserByID(r.Context(), uid)
						if err != nil || u == nil || u.IsDisabled {
							httpx.Error(w, http.StatusForbidden, "Live TV access is disabled for this user")
							return
						}
						if !u.IsAdmin && !u.Policy.IsAdministrator && !u.Policy.EnableLiveTvAccess {
							httpx.Error(w, http.StatusForbidden, "Live TV access is disabled for this user")
							return
						}
					} else if !ac.IsAdmin() && !ac.User.Policy.EnableLiveTvAccess {
						httpx.Error(w, http.StatusForbidden, "Live TV access is disabled for this user")
						return
					}
				}
			} else if h.auth.UserIDFromCtx != nil {
				if ur := h.getUserRepo(); ur != nil {
					if uid := guid.Normalize(h.auth.UserIDFromCtx(r.Context())); uid != "" {
						u, err := ur.GetUserByID(r.Context(), uid)
						if err != nil || u == nil || u.IsDisabled {
							httpx.Error(w, http.StatusForbidden, "Live TV access is disabled for this user")
							return
						}
						if !u.IsAdmin && !u.Policy.IsAdministrator && !u.Policy.EnableLiveTvAccess {
							httpx.Error(w, http.StatusForbidden, "Live TV access is disabled for this user")
							return
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

var (
	hlsHasURIAttributeRe = regexp.MustCompile(`(?i)\bURI\s*=`)
	hlsURIAttributeRe    = regexp.MustCompile(`(?i)\bURI\s*=\s*(?:"([^"]*)"|([^",\s]+))`)
)

func (h *Handler) signProxyTarget(channelID, target string, allowPrivateLAN bool, exp int64) string {
	return h.signProxyTargetWithUser(channelID, target, allowPrivateLAN, exp, "")
}

func (h *Handler) signProxyTargetWithUser(channelID, target string, allowPrivateLAN bool, exp int64, uid string) string {
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
	if normUID := guid.Normalize(uid); normUID != "" {
		mac.Write([]byte{0})
		mac.Write([]byte("uid=" + normUID))
	}
	return hex.EncodeToString(mac.Sum(nil))
}

func (h *Handler) verifyProxyTarget(channelID, target string, allowPrivateLAN bool, expStr, sig string) bool {
	return h.verifyProxyTargetWithUser(channelID, target, allowPrivateLAN, expStr, "", sig)
}

func (h *Handler) verifyProxyTargetWithUser(channelID, target string, allowPrivateLAN bool, expStr, uid, sig string) bool {
	if len(h.proxySecret) == 0 {
		return false
	}
	exp, err := strconv.ParseInt(strings.TrimSpace(expStr), 10, 64)
	if err != nil || exp <= h.nowUTC().Unix() {
		return false
	}
	got, err := hex.DecodeString(strings.TrimSpace(sig))
	if err != nil || len(got) != sha256.Size {
		return false
	}
	expectedHex := h.signProxyTargetWithUser(channelID, target, allowPrivateLAN, exp, uid)
	expected, err := hex.DecodeString(expectedHex)
	if err != nil {
		return false
	}
	return hmac.Equal(got, expected)
}

func (h *Handler) signProxyTargetWithIAT(channelID, target string, allowPrivateLAN bool, iat, exp int64) string {
	return h.signProxyTargetWithIATAndUser(channelID, target, allowPrivateLAN, iat, exp, "")
}

func (h *Handler) signProxyTargetWithIATAndUser(channelID, target string, allowPrivateLAN bool, iat, exp int64, uid string) string {
	lanFlag := "0"
	if allowPrivateLAN {
		lanFlag = "1"
	}
	mac := hmac.New(sha256.New, h.proxySecret)
	mac.Write([]byte(channelID))
	mac.Write([]byte{0})
	mac.Write([]byte(lanFlag))
	mac.Write([]byte{0})
	mac.Write([]byte(strconv.FormatInt(iat, 10)))
	mac.Write([]byte{0})
	mac.Write([]byte(strconv.FormatInt(exp, 10)))
	mac.Write([]byte{0})
	mac.Write([]byte(target))
	if normUID := guid.Normalize(uid); normUID != "" {
		mac.Write([]byte{0})
		mac.Write([]byte("uid=" + normUID))
	}
	return hex.EncodeToString(mac.Sum(nil))
}

func (h *Handler) verifyProxyTargetWithIAT(channelID, target string, allowPrivateLAN bool, iatStr, expStr, sig string) (int64, int64, bool) {
	return h.verifyProxyTargetWithIATAndUser(channelID, target, allowPrivateLAN, iatStr, expStr, "", sig)
}

func (h *Handler) verifyProxyTargetWithIATAndUser(channelID, target string, allowPrivateLAN bool, iatStr, expStr, uid, sig string) (int64, int64, bool) {
	if len(h.proxySecret) == 0 {
		return 0, 0, false
	}
	nowUnix := h.nowUTC().Unix()
	iat, err := strconv.ParseInt(strings.TrimSpace(iatStr), 10, 64)
	if err != nil || iat <= 0 || iat > nowUnix+60 || nowUnix-iat > maxLiveStreamSessionSeconds {
		return 0, 0, false
	}
	exp, err := strconv.ParseInt(strings.TrimSpace(expStr), 10, 64)
	if err != nil || exp <= nowUnix || exp > iat+maxLiveStreamSessionSeconds {
		return 0, 0, false
	}
	got, err := hex.DecodeString(strings.TrimSpace(sig))
	if err != nil || len(got) != sha256.Size {
		return 0, 0, false
	}
	expectedHex := h.signProxyTargetWithIATAndUser(channelID, target, allowPrivateLAN, iat, exp, uid)
	expected, err := hex.DecodeString(expectedHex)
	if err != nil || !hmac.Equal(got, expected) {
		return 0, 0, false
	}
	return iat, exp, true
}

func (h *Handler) validateStreamURL(rawURL string, allowPrivateLAN bool) error {
	if h.customStreamClient {
		return netutil.ValidateRemoteURL(rawURL, allowPrivateLAN)
	}
	return netutil.ValidateRemoteURLNoLoopback(rawURL, allowPrivateLAN)
}

func isPrivateLANAddr(addr netip.Addr, allowLoopback bool) bool {
	if !addr.IsValid() {
		return false
	}
	addr = addr.WithZone("").Unmap()
	if addr.IsUnspecified() || (addr.Is4() && addr.As4()[0] == 0) ||
		addr.IsLinkLocalUnicast() ||
		addr.IsMulticast() || addr.IsLinkLocalMulticast() || addr.IsInterfaceLocalMulticast() {
		return false
	}
	if addr.IsLoopback() {
		return allowLoopback
	}
	return addr.IsPrivate()
}

func isStrictPrivateLANURL(rawURL string, allowLoopback bool) bool {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return false
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return false
	}
	host := strings.TrimSpace(u.Hostname())
	if host == "" {
		return false
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return isPrivateLANAddr(addr, allowLoopback)
	}
	lowerHost := strings.ToLower(strings.TrimSuffix(host, "."))
	if !allowLoopback && (lowerHost == "localhost" || strings.HasSuffix(lowerHost, ".localhost")) {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ipAddrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(ipAddrs) == 0 {
		return false
	}
	for _, ia := range ipAddrs {
		addr, ok := netip.AddrFromSlice(ia.IP)
		if !ok || !isPrivateLANAddr(addr, allowLoopback) {
			return false
		}
	}
	return true
}

func (h *Handler) channelAllowsPrivateLAN(channelURL string) bool {
	if !isStrictPrivateLANURL(channelURL, h.customStreamClient) {
		return false
	}
	return h.validateStreamURL(channelURL, true) == nil
}

func (h *Handler) tunerAllowsPrivateNetwork(tunerHostID string) (allow bool, found bool) {
	if strings.TrimSpace(tunerHostID) == "" {
		return false, false
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, th := range h.state.TunerHosts {
		if th.Id == tunerHostID {
			return th.AllowPrivateNetwork, true
		}
	}
	return false, false
}

func (h *Handler) channelAllowsPrivateLANForChannel(ch *ChannelInfo) bool {
	if ch == nil {
		return false
	}
	if ch.TunerHostId != "" {
		if allow, found := h.tunerAllowsPrivateNetwork(ch.TunerHostId); found && !allow {
			return false
		}
	}
	return h.channelAllowsPrivateLAN(ch.StreamUrl)
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
	channelAllowLAN := h.channelAllowsPrivateLANForChannel(ch)
	streamURL := ch.StreamUrl
	allowPrivateLAN := channelAllowLAN
	var sessionIAT int64
	var maxExp int64
	var streamUID string
	if target := strings.TrimSpace(r.URL.Query().Get("target")); target != "" {
		q := r.URL.Query()
		sig := strings.TrimSpace(q.Get("sig"))
		expStr := strings.TrimSpace(q.Get("exp"))
		iatStr := strings.TrimSpace(q.Get("iat"))
		lanParam := strings.TrimSpace(q.Get("lan"))
		uidParam := strings.TrimSpace(q.Get("uid"))
		if lanParam != "0" && lanParam != "1" {
			httpx.Error(w, http.StatusForbidden, "invalid or missing target scope")
			return
		}
		reqAllowLAN := lanParam == "1"
		if reqAllowLAN && !channelAllowLAN {
			httpx.Error(w, http.StatusForbidden, "target scope exceeds channel network scope")
			return
		}
		if iatStr != "" {
			iat, _, valid := h.verifyProxyTargetWithIATAndUser(id, target, reqAllowLAN, iatStr, expStr, uidParam, sig)
			if !valid {
				httpx.Error(w, http.StatusForbidden, "invalid, expired, or missing target signature")
				return
			}
			sessionIAT = iat
		} else {
			if !h.verifyProxyTargetWithUser(id, target, reqAllowLAN, expStr, uidParam, sig) {
				httpx.Error(w, http.StatusForbidden, "invalid, expired, or missing target signature")
				return
			}
			if parsedExp, err := strconv.ParseInt(expStr, 10, 64); err == nil && parsedExp > 0 {
				maxExp = parsedExp
			}
		}
		if uidParam != "" && !h.isSignedStreamUserAllowed(r.Context(), uidParam) {
			httpx.Error(w, http.StatusForbidden, "Live TV stream access revoked for this user")
			return
		}
		if err := h.validateStreamURL(target, reqAllowLAN); err != nil {
			httpx.Error(w, http.StatusBadRequest, fmt.Sprintf("unsafe target stream url: %v", err))
			return
		}
		streamURL = target
		allowPrivateLAN = reqAllowLAN
		streamUID = guid.Normalize(uidParam)
	} else {
		sessionIAT = h.nowUTC().Unix()
		if callerID, _, hasAuth := h.callerAuthInfo(r); hasAuth && callerID != "" {
			streamUID = guid.Normalize(callerID)
		}
	}
	h.proxyChannelStreamWithSessionAndUser(w, r, id, streamURL, allowPrivateLAN, sessionIAT, maxExp, streamUID)
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
	if !h.canCallerPlayMedia(r) {
		httpx.Error(w, http.StatusForbidden, "Media playback is disabled for this user")
		return
	}
	h.proxyChannelStreamWithPolicy(w, r, "", streamURL, h.channelAllowsPrivateLAN(streamURL))
}

// ProxyChannelStream validates streamURL and proxies the upstream live stream for channelID,
// rewriting HLS playlist URIs with signed relative proxy URLs when channelID is non-empty.
func (h *Handler) ProxyChannelStream(w http.ResponseWriter, r *http.Request, channelID, streamURL string) {
	if !h.canCallerPlayMedia(r) {
		httpx.Error(w, http.StatusForbidden, "Media playback is disabled for this user")
		return
	}
	allowLAN := h.channelAllowsPrivateLAN(streamURL)
	if channelID != "" {
		if ch, ok := h.GetChannelByID(channelID); ok && strings.TrimSpace(ch.StreamUrl) != "" {
			allowLAN = h.channelAllowsPrivateLANForChannel(ch)
		}
	}
	h.proxyChannelStreamWithSession(w, r, channelID, streamURL, allowLAN, h.nowUTC().Unix(), 0)
}

func (h *Handler) proxyChannelStream(w http.ResponseWriter, r *http.Request, channelID, streamURL string) {
	h.ProxyChannelStream(w, r, channelID, streamURL)
}

func (h *Handler) channelUserAgent(channelID string) string {
	if strings.TrimSpace(channelID) == "" {
		return ""
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	var tunerID string
	for _, ch := range h.state.Channels {
		if ch.Id == channelID {
			tunerID = ch.TunerHostId
			break
		}
	}
	if tunerID == "" {
		return ""
	}
	for _, th := range h.state.TunerHosts {
		if th.Id == tunerID {
			ua := strings.TrimSpace(th.UserAgent)
			ua = strings.NewReplacer("\r", "", "\n", "").Replace(ua)
			return ua
		}
	}
	return ""
}

func (h *Handler) proxyChannelStreamWithPolicy(w http.ResponseWriter, r *http.Request, channelID, streamURL string, allowPrivateLAN bool) {
	h.proxyChannelStreamWithOptions(w, r, channelID, streamURL, allowPrivateLAN, 0)
}

func (h *Handler) acquireStreamHTTPClient(allowPrivateLAN bool) (*http.Client, func()) {
	noop := func() {}
	if allowPrivateLAN {
		if h.streamClient != nil {
			return h.streamClient, noop
		}
		c := netutil.NewSafeHTTPClientNoLoopback(0, true)
		return c, c.CloseIdleConnections
	}
	if h.publicStreamClient != nil {
		return h.publicStreamClient, noop
	}
	if h.customStreamClient && h.streamClient != nil {
		return h.streamClient, noop
	}
	c := netutil.NewSafeHTTPClientNoLoopback(0, false)
	return c, c.CloseIdleConnections
}

func (h *Handler) selectStreamHTTPClient(allowPrivateLAN bool) *http.Client {
	c, _ := h.acquireStreamHTTPClient(allowPrivateLAN)
	return c
}

func (h *Handler) proxyChannelStreamWithOptions(w http.ResponseWriter, r *http.Request, channelID, streamURL string, allowPrivateLAN bool, maxExp int64) {
	var sessionIAT int64
	if maxExp == 0 && (r == nil || strings.TrimSpace(r.URL.Query().Get("target")) == "") {
		sessionIAT = h.nowUTC().Unix()
	}
	h.proxyChannelStreamWithSession(w, r, channelID, streamURL, allowPrivateLAN, sessionIAT, maxExp)
}

func (h *Handler) proxyChannelStreamWithSession(w http.ResponseWriter, r *http.Request, channelID, streamURL string, allowPrivateLAN bool, sessionIAT, maxExp int64) {
	var streamUID string
	if r != nil {
		if qUID := strings.TrimSpace(r.URL.Query().Get("uid")); qUID != "" {
			streamUID = guid.Normalize(qUID)
		} else if callerID, _, hasAuth := h.callerAuthInfo(r); hasAuth && callerID != "" {
			streamUID = guid.Normalize(callerID)
		}
	}
	h.proxyChannelStreamWithSessionAndUser(w, r, channelID, streamURL, allowPrivateLAN, sessionIAT, maxExp, streamUID)
}

func (h *Handler) proxyChannelStreamWithSessionAndUser(w http.ResponseWriter, r *http.Request, channelID, streamURL string, allowPrivateLAN bool, sessionIAT, maxExp int64, streamUID string) {
	trimmed := strings.TrimSpace(streamURL)
	if err := h.validateStreamURL(trimmed, allowPrivateLAN); err != nil {
		httpx.Error(w, http.StatusBadRequest, fmt.Sprintf("unsafe channel stream url: %v", err))
		return
	}
	if _, err := http.NewRequestWithContext(r.Context(), http.MethodGet, trimmed, nil); err != nil {
		httpx.Error(w, http.StatusBadRequest, fmt.Sprintf("invalid channel stream url: %v", err))
		return
	}
	if r != nil && r.Context().Err() != nil {
		return
	}

	isSignedSubTarget := r != nil && strings.TrimSpace(r.URL.Query().Get("target")) != ""
	isHLSStream := isSignedSubTarget || isHLSStreamURLForHub(trimmed)
	if r == nil || r.Method != http.MethodHead {
		if isHLSStream {
			if !h.touchOrAcquireLiveStreamLease(r, channelID, sessionIAT, streamUID) {
				httpx.Error(w, http.StatusTooManyRequests, "simultaneous stream limit reached")
				return
			}
		} else {
			releaseSlot, allowed := h.tryAcquireLiveStreamSlot(r, channelID)
			if !allowed {
				httpx.Error(w, http.StatusTooManyRequests, "simultaneous stream limit reached")
				return
			}
			defer releaseSlot()
		}
	}

	ua := h.channelUserAgent(channelID)
	client, closeClient := h.acquireStreamHTTPClient(allowPrivateLAN)
	defer closeClient()

	// Multiplex continuous named channel streams across concurrent viewers via LiveStreamHub.
	if h.streamHub != nil && strings.TrimSpace(channelID) != "" && !isSignedSubTarget && !isHLSStreamURLForHub(trimmed) {
		h.serveMultiplexedStream(w, r, channelID, trimmed, ua, allowPrivateLAN, sessionIAT, maxExp, streamUID, client)
		return
	}

	idleTimeout := defaultStreamIdleTimeout
	if h.streamHub != nil {
		idleTimeout, _ = h.streamHub.getTimings()
	}
	upCtx, cancelUp := context.WithCancel(r.Context())
	defer cancelUp()
	watchdog := newIdleWatchdog(idleTimeout, cancelUp)
	defer watchdog.Stop()

	req, err := http.NewRequestWithContext(upCtx, http.MethodGet, trimmed, nil)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, fmt.Sprintf("invalid channel stream url: %v", err))
		return
	}
	if ua != "" {
		req.Header.Set("User-Agent", ua)
	}

	resp, err := client.Do(req)
	if err != nil {
		if r.Context().Err() != nil && !watchdog.TimedOut() {
			return
		}
		httpx.Error(w, http.StatusBadGateway, fmt.Sprintf("upstream stream error: %v", err))
		return
	}
	defer resp.Body.Close()
	watchdog.Touch()

	if resp.StatusCode >= 400 {
		httpx.Error(w, http.StatusBadGateway, fmt.Sprintf("upstream stream returned status %d", resp.StatusCode))
		return
	}

	baseURL := req.URL
	if resp.Request != nil && resp.Request.URL != nil {
		baseURL = resp.Request.URL
	}

	ct := resp.Header.Get("Content-Type")
	bodyReader := &watchdogReader{r: resp.Body, wd: watchdog}
	if channelID != "" && isHLSPlaylistResponse(ct, baseURL) {
		raw, err := io.ReadAll(io.LimitReader(bodyReader, maxHLSPlaylistBytes+1))
		if err != nil {
			if r.Context().Err() != nil && !watchdog.TimedOut() {
				return
			}
			httpx.Error(w, http.StatusBadGateway, fmt.Sprintf("upstream playlist read error: %v", err))
			return
		}
		if len(raw) > maxHLSPlaylistBytes {
			httpx.Error(w, http.StatusBadGateway, "upstream HLS playlist exceeds 2 MiB limit")
			return
		}

		proxyBase := computeHLSProxyBase(r, channelID)
		rewritten := h.rewriteHLSPlaylistWithSessionAndUser(string(raw), baseURL, channelID, proxyBase, "", allowPrivateLAN, sessionIAT, maxExp, streamUID)
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
	_, _ = io.Copy(dst, bodyReader)
}

func isHLSStreamURLForHub(rawURL string) bool {
	isHLS, _ := detectStreamFormat(rawURL)
	return isHLS
}

func (h *Handler) serveMultiplexedStream(
	w http.ResponseWriter,
	r *http.Request,
	channelID, streamURL, userAgent string,
	allowPrivateLAN bool,
	sessionIAT, maxExp int64,
	streamUID string,
	client *http.Client,
) {
	key := streamKeyForChannel(channelID, streamURL, allowPrivateLAN)
	b, sub, err := h.streamHub.acquireOrCreate(key, streamURL, userAgent, client)
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, fmt.Sprintf("live stream hub error: %v", err))
		return
	}
	defer b.removeSubscriber(sub)

	select {
	case <-r.Context().Done():
		return
	case <-b.readyCh:
	}

	if b.initErr != nil {
		if r.Context().Err() != nil {
			return
		}
		httpx.Error(w, http.StatusBadGateway, fmt.Sprintf("upstream stream error: %v", b.initErr))
		return
	}

	if b.isHLS {
		if len(b.hlsRaw) > maxHLSPlaylistBytes {
			httpx.Error(w, http.StatusBadGateway, "upstream HLS playlist exceeds 2 MiB limit")
			return
		}
		if channelID != "" {
			proxyBase := computeHLSProxyBase(r, channelID)
			rewritten := h.rewriteHLSPlaylistWithSessionAndUser(string(b.hlsRaw), b.baseURL, channelID, proxyBase, "", allowPrivateLAN, sessionIAT, maxExp, streamUID)
			safeCT := sanitizeLiveTVProxyContentType(b.contentType, "application/vnd.apple.mpegurl")
			w.Header().Set("Content-Type", safeCT)
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Content-Security-Policy", "sandbox")
			w.Header().Set("Cache-Control", "no-cache, no-store")
			w.Header().Set("Content-Length", strconv.Itoa(len(rewritten)))
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, rewritten)
			return
		}
		safeCT := sanitizeLiveTVProxyContentType(b.contentType, "application/vnd.apple.mpegurl")
		w.Header().Set("Content-Type", safeCT)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "sandbox")
		w.Header().Set("Cache-Control", "no-cache, no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(b.hlsRaw)
		return
	}

	safeCT := sanitizeLiveTVProxyContentType(b.contentType, "video/mp2t")
	w.Header().Set("Content-Type", safeCT)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.WriteHeader(http.StatusOK)

	var dst io.Writer = w
	if f, ok := w.(http.Flusher); ok {
		dst = flushWriter{w: w, f: f}
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case chunk := <-sub.ch:
			if len(chunk) > 0 {
				if _, wErr := dst.Write(chunk); wErr != nil {
					return
				}
			}
		case <-sub.done:
			// Drain any buffered chunks written right before upstream closed.
			for {
				select {
				case chunk := <-sub.ch:
					if len(chunk) > 0 {
						if _, wErr := dst.Write(chunk); wErr != nil {
							return
						}
					}
				default:
					return
				}
			}
		}
	}
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

func computeHLSProxyBase(r *http.Request, channelID string) string {
	if r == nil || r.URL == nil {
		return "Stream"
	}
	lowerPath := strings.ToLower(r.URL.Path)
	const marker = "/livestreamfiles/"
	idx := strings.Index(lowerPath, marker)
	if idx < 0 {
		return "Stream"
	}
	rest := strings.TrimPrefix(lowerPath[idx+len(marker):], "/")
	ups := strings.Count(rest, "/") + 1
	return strings.Repeat("../", ups) + "Channels/" + url.PathEscape(channelID) + "/Stream"
}

func (h *Handler) rewriteHLSPlaylistWithPolicy(playlist string, baseURL *url.URL, channelID, proxyBase, token string, allowPrivateLAN bool) string {
	return h.rewriteHLSPlaylistWithPolicyAndExp(playlist, baseURL, channelID, proxyBase, token, allowPrivateLAN, 0)
}

func (h *Handler) rewriteHLSPlaylistWithPolicyAndExp(playlist string, baseURL *url.URL, channelID, proxyBase, token string, allowPrivateLAN bool, maxExp int64) string {
	return h.rewriteHLSPlaylistWithSession(playlist, baseURL, channelID, proxyBase, token, allowPrivateLAN, 0, maxExp)
}

func (h *Handler) rewriteHLSPlaylistWithSession(playlist string, baseURL *url.URL, channelID, proxyBase, token string, allowPrivateLAN bool, sessionIAT, maxExp int64) string {
	return h.rewriteHLSPlaylistWithSessionAndUser(playlist, baseURL, channelID, proxyBase, token, allowPrivateLAN, sessionIAT, maxExp, "")
}

func (h *Handler) rewriteHLSPlaylistWithSessionAndUser(playlist string, baseURL *url.URL, channelID, proxyBase, token string, allowPrivateLAN bool, sessionIAT, maxExp int64, uid string) string {
	if proxyBase == "" {
		proxyBase = "Stream"
	}
	nowUnix := h.nowUTC().Unix()
	exp := nowUnix + int64(proxyTargetTTL.Seconds())
	if sessionIAT > 0 {
		ceiling := sessionIAT + maxLiveStreamSessionSeconds
		if exp > ceiling {
			exp = ceiling
		}
	}
	if maxExp > 0 && exp > maxExp {
		exp = maxExp
	}
	lanFlag := "0"
	if allowPrivateLAN {
		lanFlag = "1"
	}
	normUID := guid.Normalize(uid)
	rewriteURI := func(rawURI string) (string, bool) {
		trimmed := strings.TrimSpace(rawURI)
		if trimmed == "" {
			return "", false
		}
		if baseURL == nil {
			return rawURI, true
		}
		ref, err := url.Parse(trimmed)
		if err != nil {
			return "", false
		}
		resolved := baseURL.ResolveReference(ref)
		scheme := strings.ToLower(resolved.Scheme)
		if scheme != "http" && scheme != "https" {
			return "", false
		}
		resolvedStr := resolved.String()
		if err := h.validateStreamURL(resolvedStr, allowPrivateLAN); err != nil {
			return "", false
		}
		var proxied string
		if sessionIAT > 0 {
			sig := h.signProxyTargetWithIATAndUser(channelID, resolvedStr, allowPrivateLAN, sessionIAT, exp, normUID)
			proxied = proxyBase + "?target=" + url.QueryEscape(resolvedStr) + "&exp=" + strconv.FormatInt(exp, 10) + "&iat=" + strconv.FormatInt(sessionIAT, 10) + "&lan=" + lanFlag
			if normUID != "" {
				proxied += "&uid=" + url.QueryEscape(normUID)
			}
			proxied += "&sig=" + sig
		} else {
			sig := h.signProxyTargetWithUser(channelID, resolvedStr, allowPrivateLAN, exp, normUID)
			proxied = proxyBase + "?target=" + url.QueryEscape(resolvedStr) + "&exp=" + strconv.FormatInt(exp, 10) + "&lan=" + lanFlag
			if normUID != "" {
				proxied += "&uid=" + url.QueryEscape(normUID)
			}
			proxied += "&sig=" + sig
		}
		if token != "" {
			proxied += "&api_key=" + url.QueryEscape(token)
		}
		return proxied, true
	}

	normalized := strings.ReplaceAll(strings.ReplaceAll(playlist, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(normalized, "\n")
	out := make([]string, 0, len(lines))
	expectedProxyPrefix := proxyBase + "?target="
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			out = append(out, line)
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			if hlsHasURIAttributeRe.MatchString(line) {
				invalidURI := false
				replaced := hlsURIAttributeRe.ReplaceAllStringFunc(line, func(match string) string {
					if invalidURI {
						return match
					}
					sub := hlsURIAttributeRe.FindStringSubmatch(match)
					if len(sub) < 3 {
						invalidURI = true
						return match
					}
					rawURI := sub[1]
					if !strings.Contains(match, `"`) {
						rawURI = sub[2]
					}
					if strings.TrimSpace(rawURI) == "" {
						invalidURI = true
						return match
					}
					rewritten, ok := rewriteURI(rawURI)
					if !ok {
						invalidURI = true
						return match
					}
					return `URI="` + rewritten + `"`
				})
				if !invalidURI && baseURL != nil {
					// Fail closed if any URI= attribute remained unrewritten or points outside proxyBase.
					allAttrs := hlsHasURIAttributeRe.FindAllStringIndex(replaced, -1)
					matchedAttrs := hlsURIAttributeRe.FindAllStringSubmatch(replaced, -1)
					if len(allAttrs) != len(matchedAttrs) {
						invalidURI = true
					} else {
						for _, sub := range matchedAttrs {
							val := sub[1]
							if val == "" {
								val = sub[2]
							}
							if !strings.HasPrefix(val, expectedProxyPrefix) {
								invalidURI = true
								break
							}
						}
					}
				}
				if invalidURI {
					continue
				}
				out = append(out, replaced)
				continue
			}
			out = append(out, line)
			continue
		}
		rewritten, ok := rewriteURI(trimmed)
		if !ok {
			if len(out) > 0 && strings.HasPrefix(strings.TrimSpace(out[len(out)-1]), "#EXTINF:") {
				out = out[:len(out)-1]
			}
			continue
		}
		out = append(out, rewritten)
	}
	return strings.Join(out, "\n")
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

func (h *Handler) callerUserPolicy(r *http.Request) (dto.UserPolicy, bool, bool) {
	if r == nil {
		return dto.UserPolicy{}, false, false
	}
	callerID, isAdmin, hasAuth := h.callerAuthInfo(r)
	if !hasAuth {
		return dto.UserPolicy{}, false, false
	}
	if isAdmin {
		return dto.UserPolicy{IsAdministrator: true}, true, true
	}
	if ur := h.getUserRepo(); ur != nil && callerID != "" {
		if u, err := ur.GetUserByID(r.Context(), guid.Normalize(callerID)); err == nil && u != nil {
			return u.Policy, u.IsAdmin || u.Policy.IsAdministrator, true
		}
	}
	if ac, ok := middleware.AuthFromContext(r.Context()); ok && ac != nil && ac.User != nil {
		return ac.User.Policy, ac.IsAdmin(), true
	}
	return dto.UserPolicy{}, false, true
}

func (h *Handler) canCallerAccessRecording(ctx context.Context, libRepo *sqlite.LibraryRepo, pol dto.UserPolicy, isAdmin, hasAuth bool, recordingID string) bool {
	if !hasAuth || isAdmin || libRepo == nil {
		return true
	}
	item, err := libRepo.GetItemRecordByID(ctx, recordingID)
	if errors.Is(err, sqlite.ErrNotFound) || item == nil {
		return true
	}
	if err != nil {
		return false
	}
	if item.LibraryID != "" && !libRepo.CanUserAccessLibrary(ctx, pol, item.LibraryID) {
		return false
	}
	blocked, err := libRepo.IsItemBlockedByPolicy(ctx, item, pol)
	return err == nil && !blocked
}

func (h *Handler) handleListRecordings(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	chID := q.Get("ChannelId")
	status := strings.ToLower(q.Get("Status"))
	pol, isAdmin, hasAuth := h.callerUserPolicy(r)
	hidePath := hasAuth && !isAdmin
	libRepo := h.resolveLibraryRepo()

	h.mu.RLock()
	candidates := make([]RecordingInfo, 0, len(h.state.Recordings))
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
		candidates = append(candidates, rec)
	}
	h.mu.RUnlock()

	filtered := make([]RecordingInfo, 0, len(candidates))
	for _, rec := range candidates {
		if !h.canCallerAccessRecording(r.Context(), libRepo, pol, isAdmin, hasAuth, rec.Id) {
			continue
		}
		filtered = append(filtered, rec)
	}

	page, total := paginate(filtered, q.Int("StartIndex", 0), q.Int("Limit", 0))
	httpx.JSON(w, http.StatusOK, dto.QueryResult[RecordingInfo]{
		Items:            page,
		TotalRecordCount: total,
	})
}

func (h *Handler) handleGetRecording(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("Id")
	pol, isAdmin, hasAuth := h.callerUserPolicy(r)
	hidePath := hasAuth && !isAdmin
	h.mu.RLock()
	var found *RecordingInfo
	for _, rec := range h.state.Recordings {
		if rec.Id == id {
			copyRec := rec
			if hidePath {
				copyRec.Path = ""
			}
			found = &copyRec
			break
		}
	}
	h.mu.RUnlock()
	if found == nil {
		httpx.Error(w, http.StatusNotFound, "recording not found")
		return
	}
	if !h.canCallerAccessRecording(r.Context(), h.resolveLibraryRepo(), pol, isAdmin, hasAuth, found.Id) {
		httpx.Error(w, http.StatusForbidden, "access to this recording is denied")
		return
	}
	httpx.JSON(w, http.StatusOK, *found)
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
	if libRepo := h.resolveLibraryRepo(); libRepo != nil {
		_ = libRepo.DeleteItem(ctx, id)
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

func (h *Handler) channelExistsLocked(channelID string) bool {
	for _, ch := range h.state.Channels {
		if ch.Id == channelID {
			return true
		}
	}
	return false
}

func (h *Handler) handleCreateTimer(w http.ResponseWriter, r *http.Request) {
	var t TimerInfo
	if err := httpx.DecodeJSONLimit(r, &t, 0); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid timer payload")
		return
	}
	t.Id = strings.TrimSpace(t.Id)
	if t.Id == "" {
		t.Id = guid.New()
	}
	t.ChannelId = strings.TrimSpace(t.ChannelId)
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
	if len(h.state.Timers) >= maxTimers {
		h.mu.Unlock()
		httpx.Error(w, http.StatusTooManyRequests, "maximum number of timers reached")
		return
	}
	for _, existing := range h.state.Timers {
		if existing.Id == t.Id {
			h.mu.Unlock()
			httpx.Error(w, http.StatusConflict, "timer id already exists")
			return
		}
	}
	if t.ChannelId == "" || !h.channelExistsLocked(t.ChannelId) {
		h.mu.Unlock()
		httpx.Error(w, http.StatusBadRequest, "valid existing ChannelId is required")
		return
	}
	h.state.Timers = append(h.state.Timers, t)
	err := h.saveStateLocked(r.Context())
	if err != nil {
		h.state.Timers = h.state.Timers[:len(h.state.Timers)-1]
	}
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
			upd.ChannelId = strings.TrimSpace(upd.ChannelId)
			if upd.ChannelId == "" {
				upd.ChannelId = existing.ChannelId
			} else if !h.channelExistsLocked(upd.ChannelId) {
				h.mu.Unlock()
				httpx.Error(w, http.StatusBadRequest, "valid existing ChannelId is required")
				return
			}
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
			if err != nil {
				h.state.Timers[i] = existing
			}
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
	st.Id = strings.TrimSpace(st.Id)
	if st.Id == "" {
		st.Id = guid.New()
	}
	st.ChannelId = strings.TrimSpace(st.ChannelId)
	st.ServerId = h.serverID()
	if st.Days == nil {
		st.Days = []string{}
	}

	h.mu.Lock()
	if len(h.state.SeriesTimers) >= maxSeriesTimers {
		h.mu.Unlock()
		httpx.Error(w, http.StatusTooManyRequests, "maximum number of series timers reached")
		return
	}
	for _, existing := range h.state.SeriesTimers {
		if existing.Id == st.Id {
			h.mu.Unlock()
			httpx.Error(w, http.StatusConflict, "series timer id already exists")
			return
		}
	}
	if !st.RecordAnyChannel && st.ChannelId != "" && !h.channelExistsLocked(st.ChannelId) {
		h.mu.Unlock()
		httpx.Error(w, http.StatusBadRequest, "valid existing ChannelId is required")
		return
	}
	prevSeriesLen := len(h.state.SeriesTimers)
	prevTimersLen := len(h.state.Timers)
	h.state.SeriesTimers = append(h.state.SeriesTimers, st)
	h.scheduleTimersForSeriesLocked(st)
	err := h.saveStateLocked(r.Context())
	if err != nil {
		h.state.SeriesTimers = h.state.SeriesTimers[:prevSeriesLen]
		h.state.Timers = h.state.Timers[:prevTimersLen]
	}
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
		if len(h.state.Timers) >= maxTimers {
			break
		}
		if disabledChannels[p.ChannelId] {
			continue
		}
		if p.StartDate.IsZero() || p.EndDate.IsZero() || !p.EndDate.After(p.StartDate) || p.EndDate.Sub(p.StartDate) > maxTimerDuration {
			continue
		}
		if !p.EndDate.After(now) {
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
		if st.RecordNewOnly && h.shouldSkipSeriesProgramAsRepeatLocked(st, p) {
			continue
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

func programEpisodeIdentity(name, epNum, epTitle, overview string) string {
	normName := strings.ToLower(strings.TrimSpace(name))
	if normName == "" {
		return ""
	}
	if en := strings.ToLower(strings.TrimSpace(epNum)); en != "" {
		return normName + "\x00epnum:" + en
	}
	if et := strings.ToLower(strings.TrimSpace(epTitle)); et != "" {
		return normName + "\x00eptitle:" + et
	}
	if ov := strings.ToLower(strings.TrimSpace(overview)); ov != "" {
		return normName + "\x00ov:" + ov
	}
	return ""
}

func (h *Handler) shouldSkipSeriesProgramAsRepeatLocked(st SeriesTimerInfo, p ProgramInfo) bool {
	if p.IsRepeat {
		return true
	}
	progByID := make(map[string]ProgramInfo, len(h.state.Programs))
	for _, ep := range h.state.Programs {
		if ep.Id != "" {
			progByID[ep.Id] = ep
		}
	}
	candIdentity := programEpisodeIdentity(p.Name, p.EpisodeNumber, p.EpisodeTitle, p.Overview)

	for _, rec := range h.state.Recordings {
		if !strings.EqualFold(rec.Status, "Completed") && rec.Status != "" {
			continue
		}
		if p.Id != "" && rec.ProgramId == p.Id {
			return true
		}
		if candIdentity != "" {
			if rp, ok := progByID[rec.ProgramId]; ok {
				if programEpisodeIdentity(rp.Name, rp.EpisodeNumber, rp.EpisodeTitle, rp.Overview) == candIdentity {
					return true
				}
			}
		} else if strings.EqualFold(strings.TrimSpace(rec.Name), strings.TrimSpace(p.Name)) {
			// When the EPG provides neither episode numbers/titles nor distinct overview,
			// treat an already-completed recording of the exact same title as already recorded.
			return true
		}
	}

	for _, tm := range h.state.Timers {
		if strings.EqualFold(tm.Status, "Cancelled") || strings.EqualFold(tm.Status, "Failed") {
			continue
		}
		if p.Id != "" && tm.ProgramId == p.Id {
			return true
		}
		if candIdentity != "" {
			var tmIdentity string
			if tp, ok := progByID[tm.ProgramId]; ok {
				tmIdentity = programEpisodeIdentity(tp.Name, tp.EpisodeNumber, tp.EpisodeTitle, tp.Overview)
			} else {
				tmIdentity = programEpisodeIdentity(tm.Name, "", "", tm.Overview)
			}
			if tmIdentity != "" && tmIdentity == candIdentity {
				return true
			}
		} else if tm.SeriesTimerId == st.Id && strings.EqualFold(strings.TrimSpace(tm.Name), strings.TrimSpace(p.Name)) {
			return true
		}
	}
	return false
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
			upd.ChannelId = strings.TrimSpace(upd.ChannelId)
			if !upd.RecordAnyChannel && upd.ChannelId != "" && !h.channelExistsLocked(upd.ChannelId) {
				h.mu.Unlock()
				httpx.Error(w, http.StatusBadRequest, "valid existing ChannelId is required")
				return
			}
			if upd.Days == nil {
				upd.Days = []string{}
			}
			prevTimers := append([]TimerInfo(nil), h.state.Timers...)
			h.state.SeriesTimers[i] = upd
			// Remove existing pending ("New") timers belonging to this series timer before rescheduling.
			keptTimers := make([]TimerInfo, 0, len(h.state.Timers))
			for _, t := range h.state.Timers {
				if t.SeriesTimerId == id && (t.Status == "" || t.Status == "New") {
					continue
				}
				keptTimers = append(keptTimers, t)
			}
			h.state.Timers = keptTimers
			h.scheduleTimersForSeriesLocked(upd)
			err := h.saveStateLocked(r.Context())
			if err != nil {
				h.state.SeriesTimers[i] = existing
				h.state.Timers = prevTimers
			}
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

// DeleteSeriesTimer removes a series timer by ID, cascade-deletes its pending single timers,
// cancels any in-progress recordings belonging to it, and persists state.
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
	recorder := h.recorder
	var cancelIDs []string
	h.state.SeriesTimers = append(h.state.SeriesTimers[:idx], h.state.SeriesTimers[idx+1:]...)
	keptTimers := make([]TimerInfo, 0, len(h.state.Timers))
	for _, t := range h.state.Timers {
		if t.SeriesTimerId == id {
			if t.Status == "" || t.Status == "New" {
				continue
			}
			if strings.EqualFold(t.Status, "InProgress") {
				cancelIDs = append(cancelIDs, t.Id)
				continue
			}
		}
		keptTimers = append(keptTimers, t)
	}
	h.state.Timers = keptTimers
	h.saveStateLocked(ctx)
	h.mu.Unlock()

	if recorder != nil {
		for _, tid := range cancelIDs {
			_ = recorder.CancelRecording(tid)
		}
	}
	return true
}

func (h *Handler) handleDeleteSeriesTimer(w http.ResponseWriter, r *http.Request) {
	if !h.DeleteSeriesTimer(r.Context(), r.PathValue("Id")) {
		httpx.Error(w, http.StatusNotFound, "series timer not found")
		return
	}
	httpx.NoContent(w)
}

func deterministicChannelID(tunerID string, m M3UChannel, seq int) string {
	identity := strings.ToLower(strings.TrimSpace(m.TvgID))
	if identity == "" {
		identity = strings.ToLower(strings.TrimSpace(m.Name)) + "\x00" + strings.TrimSpace(m.URL)
	}
	if seq > 0 {
		identity += "\x00#" + strconv.Itoa(seq)
	}
	sum := sha256.Sum256([]byte("ambra:livetv:ch:" + tunerID + "\x00" + identity))
	return hex.EncodeToString(sum[:16])
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

	rawURL := strings.TrimSpace(host.Url)
	rawM3U := strings.TrimSpace(host.M3UContent)
	if rawURL != "" {
		if u, err := url.Parse(rawURL); err == nil && (strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https")) {
			if isStrictPrivateLANURL(rawURL, h.hasCustomHTTPClient()) {
				host.AllowPrivateNetwork = true
			} else {
				// Never automatically widen a public M3U source to RFC1918 private LAN proxying.
				host.AllowPrivateNetwork = false
			}
		} else {
			// Local filesystem M3U file managed by server admin.
			host.AllowPrivateNetwork = true
		}
	} else if rawM3U != "" && h.customStreamClient {
		host.AllowPrivateNetwork = true
	}

	var parsed []M3UChannel
	if strings.EqualFold(host.Type, "m3u") {
		var err error
		if rawM3U != "" {
			parsed, err = ParseM3UString(host.M3UContent)
		} else if rawURL != "" {
			parsed, err = h.loadM3UFromSourceWithPolicy(r.Context(), host.Url, host.UserAgent, host.AllowPrivateNetwork)
		}
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, fmt.Sprintf("failed to load m3u playlist: %v", err))
			return
		}
	} else if strings.EqualFold(host.Type, "hdhomerun") {
		if rawURL == "" {
			httpx.Error(w, http.StatusBadRequest, "hdhomerun tuner url is required")
			return
		}
		hdChannels, discovered, err := h.loadHDHomeRunFromSourceWithPolicy(r.Context(), rawURL, host.UserAgent, host.ImportFavoritesOnly, host.AllowPrivateNetwork)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, fmt.Sprintf("failed to load hdhomerun lineup: %v", err))
			return
		}
		parsed = hdChannels
		if discovered != nil {
			if host.FriendlyName == "" && discovered.FriendlyName != "" {
				host.FriendlyName = discovered.FriendlyName
			}
			if host.DeviceId == "" && discovered.DeviceID != "" {
				host.DeviceId = discovered.DeviceID
			}
			if host.TunerCount <= 0 && discovered.TunerCount > 0 {
				host.TunerCount = discovered.TunerCount
			}
		}
	}
	// Do not persist raw inline M3U playlist content after parsing.
	host.M3UContent = ""

	h.mu.Lock()
	snapshot := cloneLiveTVState(h.state)
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

	// Replace channels for this tuner host using deterministic channel IDs to preserve Timer/EPG/mapping links.
	// For M3U and HDHomeRun hosts with an explicit Url or M3UContent, also clear stale channels/programs when parsed is empty.
	if len(parsed) > 0 || ((strings.EqualFold(host.Type, "m3u") || strings.EqualFold(host.Type, "hdhomerun")) && (rawURL != "" || rawM3U != "")) {
		h.mergeTunerHostChannelsLocked(host.Id, parsed)
	}
	err := h.saveStateLocked(r.Context())
	if err != nil {
		h.state = snapshot
	}
	h.mu.Unlock()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to persist tuner host")
		return
	}

	httpx.JSON(w, http.StatusOK, host)
}

var (
	errTunerHostNoURL            = errors.New("tuner host has no configured url to refresh")
	errTunerHostUnsupportedType  = errors.New("unsupported tuner host type for refresh")
	errTunerHostEmptyRefresh     = errors.New("tuner host refresh returned 0 channels; keeping existing channels")
	errTunerHostConcurrentUpdate = errors.New("tuner host configuration changed during refresh")
	errTunerHostPersist          = errors.New("failed to persist refreshed tuner host")
)

// mergeTunerHostChannelsLocked replaces channels belonging to hostID with parsed channels
// using deterministic channel IDs while preserving Disabled, SortIndex, existing Programs,
// EPG mappings, and DVR timers. For channels without a tvg-id whose stream URL changed,
// it reuses the previous channel ID (preserving Programs, ChannelMappings, Timers, and SeriesTimers)
// and inherits Disabled and SortIndex when there is a unique previous and parsed channel
// with the same trimmed lowercase Name under the same tuner host.
func (h *Handler) mergeTunerHostChannelsLocked(hostID string, parsed []M3UChannel) {
	prevByID := make(map[string]ChannelInfo)
	prevByName := make(map[string]ChannelInfo)
	prevByNameCount := make(map[string]int)
	keptChannels := make([]ChannelInfo, 0, len(h.state.Channels))
	removedChannelIDs := make(map[string]struct{})
	for _, ch := range h.state.Channels {
		if ch.TunerHostId == hostID {
			prevByID[ch.Id] = ch
			if nameKey := strings.ToLower(strings.TrimSpace(ch.Name)); nameKey != "" {
				prevByNameCount[nameKey]++
				prevByName[nameKey] = ch
			}
			removedChannelIDs[ch.Id] = struct{}{}
			continue
		}
		keptChannels = append(keptChannels, ch)
	}

	parsedNameCount := make(map[string]int, len(parsed))
	claimedPrevIDs := make(map[string]bool, len(parsed))
	preScanSeq := make(map[string]int, len(parsed))
	for _, m := range parsed {
		if nameKey := strings.ToLower(strings.TrimSpace(m.Name)); nameKey != "" {
			parsedNameCount[nameKey]++
		}
		bID := deterministicChannelID(hostID, m, 0)
		s := preScanSeq[bID]
		preScanSeq[bID] = s + 1
		cID := bID
		if s > 0 {
			cID = deterministicChannelID(hostID, m, s)
		}
		if _, ok := prevByID[cID]; ok {
			claimedPrevIDs[cID] = true
		}
	}

	now := time.Now().UTC().Truncate(time.Hour)
	usedIDs := make(map[string]int, len(parsed))
	newChannelIDs := make(map[string]struct{}, len(parsed))
	newChannels := make([]ChannelInfo, 0, len(parsed))
	for _, m := range parsed {
		baseID := deterministicChannelID(hostID, m, 0)
		seq := usedIDs[baseID]
		usedIDs[baseID] = seq + 1
		chID := baseID
		if seq > 0 {
			chID = deterministicChannelID(hostID, m, seq)
		}
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
			TunerHostId: hostID,
		}
		if prev, ok := prevByID[chID]; ok {
			ch.Disabled = prev.Disabled
			ch.SortIndex = prev.SortIndex
		} else if strings.TrimSpace(m.TvgID) == "" {
			if nameKey := strings.ToLower(strings.TrimSpace(m.Name)); nameKey != "" && prevByNameCount[nameKey] == 1 && parsedNameCount[nameKey] == 1 {
				prev := prevByName[nameKey]
				if _, taken := newChannelIDs[prev.Id]; !taken && !claimedPrevIDs[prev.Id] && usedIDs[prev.Id] == 0 {
					chID = prev.Id
					ch.Id = prev.Id
					usedIDs[prev.Id] = 1
					claimedPrevIDs[prev.Id] = true
				}
				ch.Disabled = prev.Disabled
				ch.SortIndex = prev.SortIndex
			}
		}
		newChannelIDs[chID] = struct{}{}
		delete(removedChannelIDs, chID)
		newChannels = append(newChannels, ch)
	}

	existingProgramsByCh := make(map[string]bool)
	keptPrograms := make([]ProgramInfo, 0, len(h.state.Programs))
	for _, p := range h.state.Programs {
		if _, drop := removedChannelIDs[p.ChannelId]; drop {
			continue
		}
		keptPrograms = append(keptPrograms, p)
		if _, ok := newChannelIDs[p.ChannelId]; ok {
			existingProgramsByCh[p.ChannelId] = true
		}
	}

	for _, ch := range newChannels {
		keptChannels = append(keptChannels, ch)
		if !existingProgramsByCh[ch.Id] {
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
	}
	h.state.Channels = keptChannels
	h.state.Programs = keptPrograms
}

// RefreshTunerHost reloads channels from the configured URL/file of a single M3U or HDHomeRun
// tuner host and losslessly merges them into LiveTV state. Network and file I/O are performed
// outside h.mu, and an empty upstream response preserves existing channels intact.
func (h *Handler) RefreshTunerHost(ctx context.Context, id string) (TunerHostInfo, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return TunerHostInfo{}, fmt.Errorf("tuner host id is empty: %w", os.ErrNotExist)
	}

	h.mu.RLock()
	var host TunerHostInfo
	found := false
	for _, existing := range h.state.TunerHosts {
		if existing.Id == id {
			host = existing
			found = true
			break
		}
	}
	h.mu.RUnlock()
	if !found {
		return TunerHostInfo{}, fmt.Errorf("tuner host %q not found: %w", id, os.ErrNotExist)
	}

	rawURL := strings.TrimSpace(host.Url)
	if rawURL == "" {
		return TunerHostInfo{}, errTunerHostNoURL
	}

	var (
		parsed     []M3UChannel
		discovered *hdHomeRunDiscoverPayload
		err        error
	)
	if strings.EqualFold(host.Type, "m3u") {
		parsed, err = h.loadM3UFromSourceWithPolicy(ctx, rawURL, host.UserAgent, host.AllowPrivateNetwork)
		if err != nil {
			return TunerHostInfo{}, fmt.Errorf("failed to load m3u playlist: %w", err)
		}
	} else if strings.EqualFold(host.Type, "hdhomerun") {
		parsed, discovered, err = h.loadHDHomeRunFromSourceWithPolicy(ctx, rawURL, host.UserAgent, host.ImportFavoritesOnly, host.AllowPrivateNetwork)
		if err != nil {
			return TunerHostInfo{}, fmt.Errorf("failed to load hdhomerun lineup: %w", err)
		}
	} else {
		return TunerHostInfo{}, fmt.Errorf("%w: %s", errTunerHostUnsupportedType, host.Type)
	}

	if len(parsed) == 0 {
		return TunerHostInfo{}, errTunerHostEmptyRefresh
	}

	h.mu.Lock()
	idx := -1
	for i := range h.state.TunerHosts {
		if h.state.TunerHosts[i].Id == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		h.mu.Unlock()
		return TunerHostInfo{}, fmt.Errorf("tuner host %q not found: %w", id, os.ErrNotExist)
	}
	cur := h.state.TunerHosts[idx]
	if strings.TrimSpace(cur.Url) != strings.TrimSpace(host.Url) ||
		!strings.EqualFold(cur.Type, host.Type) ||
		cur.ImportFavoritesOnly != host.ImportFavoritesOnly {
		h.mu.Unlock()
		return TunerHostInfo{}, errTunerHostConcurrentUpdate
	}

	snapshot := cloneLiveTVState(h.state)
	updated := cur
	updated.ImportedChannelCount = len(parsed)
	if discovered != nil {
		if updated.FriendlyName == "" && discovered.FriendlyName != "" {
			updated.FriendlyName = discovered.FriendlyName
		}
		if updated.DeviceId == "" && discovered.DeviceID != "" {
			updated.DeviceId = discovered.DeviceID
		}
		if updated.TunerCount <= 0 && discovered.TunerCount > 0 {
			updated.TunerCount = discovered.TunerCount
		}
	}
	h.state.TunerHosts[idx] = updated
	h.mergeTunerHostChannelsLocked(updated.Id, parsed)

	saveErr := h.saveStateLocked(ctx)
	if saveErr != nil {
		h.state = snapshot
	}
	h.mu.Unlock()
	if saveErr != nil {
		return TunerHostInfo{}, fmt.Errorf("%w: %v", errTunerHostPersist, saveErr)
	}
	return updated, nil
}

// RefreshAllTunerHosts refreshes all configured M3U and HDHomeRun tuner hosts that have a
// non-empty Url, skipping manual inline M3U hosts cleanly. If a host fails to refresh, its
// existing channels are preserved and the remaining hosts continue to be refreshed.
func (h *Handler) RefreshAllTunerHosts(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	hosts := h.SnapshotTunerHosts()
	var errs []error
	for _, host := range hosts {
		if strings.TrimSpace(host.Url) == "" {
			continue
		}
		if !strings.EqualFold(host.Type, "m3u") && !strings.EqualFold(host.Type, "hdhomerun") {
			continue
		}
		if _, err := h.RefreshTunerHost(ctx, host.Id); err != nil {
			errs = append(errs, fmt.Errorf("tuner host %s: %w", host.Id, err))
		}
	}
	return errors.Join(errs...)
}

func (h *Handler) handleRefreshTunerHost(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("Id"))
	updated, err := h.RefreshTunerHost(r.Context(), id)
	if err != nil {
		switch {
		case errors.Is(err, os.ErrNotExist):
			httpx.Error(w, http.StatusNotFound, "tuner host not found")
		case errors.Is(err, errTunerHostNoURL), errors.Is(err, errTunerHostUnsupportedType):
			httpx.Error(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, errTunerHostConcurrentUpdate):
			httpx.Error(w, http.StatusConflict, err.Error())
		case errors.Is(err, errTunerHostPersist):
			httpx.Error(w, http.StatusInternalServerError, "failed to persist tuner host")
		default:
			httpx.Error(w, http.StatusBadGateway, err.Error())
		}
		return
	}
	httpx.JSON(w, http.StatusOK, updated)
}

func (h *Handler) loadM3UFromSource(ctx context.Context, src, userAgent string) ([]M3UChannel, error) {
	return h.loadM3UFromSourceWithPolicy(ctx, src, userAgent, isStrictPrivateLANURL(src, h.hasCustomHTTPClient()))
}

func (h *Handler) loadM3UFromSourceWithPolicy(ctx context.Context, src, userAgent string, allowPrivateLAN bool) ([]M3UChannel, error) {
	trimmed := strings.TrimSpace(src)
	if trimmed == "" {
		return nil, errors.New("m3u source is empty")
	}

	if u, err := url.Parse(trimmed); err == nil && u.Scheme != "" {
		scheme := strings.ToLower(u.Scheme)
		if scheme == "http" || scheme == "https" {
			if err := h.validateMetadataFetchURL(trimmed, allowPrivateLAN); err != nil {
				return nil, fmt.Errorf("blocked m3u url: %w", err)
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, trimmed, nil)
			if err != nil {
				return nil, err
			}
			if userAgent != "" {
				req.Header.Set("User-Agent", userAgent)
			}
			client, closeClient := h.acquireMetadataHTTPClient(allowPrivateLAN)
			defer closeClient()
			resp, err := client.Do(req)
			if err != nil {
				return nil, err
			}
			defer resp.Body.Close()
			if resp.StatusCode >= 400 {
				return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
			}
			if resp.Request != nil && resp.Request.URL != nil {
				if err := h.validateMetadataFetchURL(resp.Request.URL.String(), allowPrivateLAN); err != nil {
					return nil, fmt.Errorf("blocked redirected m3u url: %w", err)
				}
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

// DeleteTunerHost removes a tuner host by ID along with its channels, programs, and pending/in-progress timers,
// cancels active DVR recordings on those channels, and persists state.
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
	snapshot := cloneLiveTVState(h.state)
	recorder := h.recorder
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

	var cancelTimerIDs []string
	keptTimers := make([]TimerInfo, 0, len(h.state.Timers))
	for _, t := range h.state.Timers {
		if _, drop := removedChannelIDs[t.ChannelId]; drop {
			if t.Status == "" || t.Status == "New" {
				continue
			}
			if strings.EqualFold(t.Status, "InProgress") {
				cancelTimerIDs = append(cancelTimerIDs, t.Id)
				continue
			}
		}
		keptTimers = append(keptTimers, t)
	}
	h.state.Timers = keptTimers

	if err := h.saveStateLocked(ctx); err != nil {
		h.state = snapshot
		h.mu.Unlock()
		return false
	}
	h.mu.Unlock()

	if recorder != nil {
		for _, tid := range cancelTimerIDs {
			_ = recorder.CancelRecording(tid)
		}
	}
	h.pruneOrphanedChannelMappings(ctx)
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

	rawPath := strings.TrimSpace(lp.Path)
	if rawPath != "" {
		if u, err := url.Parse(rawPath); err == nil && (strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https")) {
			if isStrictPrivateLANURL(rawPath, h.hasCustomHTTPClient()) {
				lp.AllowPrivateNetwork = true
			} else {
				lp.AllowPrivateNetwork = false
			}
		} else {
			lp.AllowPrivateNetwork = true
		}
		lp.NetworkPolicySet = true
	}

	var guide *XMLTVGuide
	if strings.EqualFold(lp.Type, "xmltv") && rawPath != "" {
		parsed, err := h.loadXMLTVFromSourceWithPolicy(r.Context(), lp.Path, lp.AllowPrivateNetwork)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, fmt.Sprintf("failed to load xmltv guide: %v", err))
			return
		}
		guide = parsed
	}

	h.mu.Lock()
	snapshot := cloneLiveTVState(h.state)
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
		mappings := h.loadManualChannelMappingsForProvider(r.Context(), lp.Id)
		h.applyXMLTVGuideWithMappingsLocked(guide, mappings)
		for _, st := range h.state.SeriesTimers {
			h.scheduleTimersForSeriesLocked(st)
		}
	}
	err := h.saveStateLocked(r.Context())
	if err != nil {
		h.state = snapshot
	}
	h.mu.Unlock()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to persist listing provider")
		return
	}
	httpx.JSON(w, http.StatusOK, lp)
}

func (h *Handler) loadXMLTVFromSource(ctx context.Context, src string) (*XMLTVGuide, error) {
	return h.loadXMLTVFromSourceWithPolicy(ctx, src, isStrictPrivateLANURL(src, h.hasCustomHTTPClient()))
}

func (h *Handler) loadXMLTVFromSourceWithPolicy(ctx context.Context, src string, allowPrivateLAN bool) (*XMLTVGuide, error) {
	trimmed := strings.TrimSpace(src)
	if trimmed == "" {
		return nil, errors.New("xmltv source is empty")
	}

	if u, err := url.Parse(trimmed); err == nil && u.Scheme != "" {
		scheme := strings.ToLower(u.Scheme)
		if scheme == "http" || scheme == "https" {
			if err := h.validateMetadataFetchURL(trimmed, allowPrivateLAN); err != nil {
				return nil, fmt.Errorf("blocked xmltv url: %w", err)
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, trimmed, nil)
			if err != nil {
				return nil, err
			}
			client, closeClient := h.acquireMetadataHTTPClient(allowPrivateLAN)
			defer closeClient()
			resp, err := client.Do(req)
			if err != nil {
				return nil, err
			}
			defer resp.Body.Close()
			if resp.StatusCode >= 400 {
				return nil, fmt.Errorf("unexpected xmltv status %d", resp.StatusCode)
			}
			if resp.Request != nil && resp.Request.URL != nil {
				if err := h.validateMetadataFetchURL(resp.Request.URL.String(), allowPrivateLAN); err != nil {
					return nil, fmt.Errorf("blocked redirected xmltv url: %w", err)
				}
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

type hdHomeRunDiscoverPayload struct {
	FriendlyName string `json:"FriendlyName"`
	ModelNumber  string `json:"ModelNumber"`
	DeviceID     string `json:"DeviceID"`
	TunerCount   int    `json:"TunerCount"`
	BaseURL      string `json:"BaseURL"`
	LineupURL    string `json:"LineupURL"`
}

type hdHomeRunLineupEntry struct {
	GuideNumber string `json:"GuideNumber"`
	GuideName   string `json:"GuideName"`
	URL         string `json:"URL"`
	Favorite    int    `json:"Favorite"`
	HD          int    `json:"HD"`
}

func normalizeHDHomeRunBaseURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("hdhomerun url is empty")
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "http://" + trimmed
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return "", err
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("unsupported hdhomerun url scheme %q", u.Scheme)
	}
	if strings.TrimSpace(u.Hostname()) == "" {
		return "", errors.New("hdhomerun host is empty")
	}
	u.RawQuery = ""
	u.Fragment = ""
	u.Path = strings.TrimRight(u.Path, "/")
	u.Path = strings.TrimSuffix(u.Path, "/discover.json")
	u.Path = strings.TrimSuffix(u.Path, "/lineup.json")
	return strings.TrimRight(u.String(), "/"), nil
}

func sameHTTPOrigin(a, b *url.URL) bool {
	if a == nil || b == nil {
		return false
	}
	schemeA := strings.ToLower(strings.TrimSpace(a.Scheme))
	schemeB := strings.ToLower(strings.TrimSpace(b.Scheme))
	if schemeA == "" || schemeA != schemeB {
		return false
	}
	hostA := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(a.Hostname()), "."))
	hostB := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(b.Hostname()), "."))
	if hostA == "" || hostA != hostB {
		return false
	}
	portOf := func(u *url.URL, scheme string) string {
		if p := u.Port(); p != "" {
			return p
		}
		if scheme == "https" {
			return "443"
		}
		return "80"
	}
	return portOf(a, schemeA) == portOf(b, schemeB)
}

func (h *Handler) validateMetadataFetchURL(rawURL string, allowPrivateLAN bool) error {
	if !h.hasCustomHTTPClient() {
		return netutil.ValidateRemoteURLNoLoopback(rawURL, allowPrivateLAN)
	}
	if allowPrivateLAN {
		return netutil.ValidateRemoteURL(rawURL, true)
	}
	if u, err := url.Parse(strings.TrimSpace(rawURL)); err == nil {
		if addr, aErr := netip.ParseAddr(u.Hostname()); aErr == nil && addr.Unmap().IsLoopback() {
			return nil
		}
	}
	return netutil.ValidateRemoteURL(rawURL, false)
}

func (h *Handler) acquireMetadataHTTPClient(allowPrivateLAN bool) (*http.Client, func()) {
	noop := func() {}
	if allowPrivateLAN {
		if h.httpClient != nil {
			return h.httpClient, noop
		}
		c := netutil.NewSafeHTTPClientNoLoopback(15*time.Second, true)
		return c, c.CloseIdleConnections
	}
	if h.hasCustomHTTPClient() && h.httpClient != nil {
		return h.httpClient, noop
	}
	if h.publicHTTPClient != nil {
		return h.publicHTTPClient, noop
	}
	c := netutil.NewSafeHTTPClientNoLoopback(15*time.Second, false)
	return c, c.CloseIdleConnections
}

func (h *Handler) selectMetadataHTTPClient(allowPrivateLAN bool) *http.Client {
	c, _ := h.acquireMetadataHTTPClient(allowPrivateLAN)
	return c
}

func (h *Handler) fetchHDHomeRunJSON(ctx context.Context, targetURL, userAgent string, dst any) error {
	return h.fetchHDHomeRunJSONWithPolicy(ctx, targetURL, userAgent, isStrictPrivateLANURL(targetURL, h.hasCustomHTTPClient()), dst)
}

func (h *Handler) fetchHDHomeRunJSONWithPolicy(ctx context.Context, targetURL, userAgent string, allowPrivateLAN bool, dst any) error {
	if !h.hasCustomHTTPClient() {
		if err := netutil.ValidateRemoteURLNoLoopback(targetURL, allowPrivateLAN); err != nil {
			return fmt.Errorf("blocked hdhomerun url: %w", err)
		}
	} else if !allowPrivateLAN {
		if err := netutil.ValidateRemoteURL(targetURL, false); err != nil {
			return fmt.Errorf("blocked hdhomerun url: %w", err)
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return err
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	client, closeClient := h.acquireMetadataHTTPClient(allowPrivateLAN)
	defer closeClient()
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL := resp.Request.URL.String()
		if !h.hasCustomHTTPClient() {
			if err := netutil.ValidateRemoteURLNoLoopback(finalURL, allowPrivateLAN); err != nil {
				return fmt.Errorf("blocked redirected hdhomerun url: %w", err)
			}
		} else if !allowPrivateLAN {
			if err := netutil.ValidateRemoteURL(finalURL, false); err != nil {
				return fmt.Errorf("blocked redirected hdhomerun url: %w", err)
			}
		}
	}
	return json.NewDecoder(io.LimitReader(resp.Body, maxM3UPlaylistBytes)).Decode(dst)
}

func (h *Handler) loadHDHomeRunFromSource(ctx context.Context, rawURL, userAgent string, favoritesOnly bool) ([]M3UChannel, *hdHomeRunDiscoverPayload, error) {
	baseURL, err := normalizeHDHomeRunBaseURL(rawURL)
	if err != nil {
		return nil, nil, err
	}
	return h.loadHDHomeRunFromSourceWithPolicy(ctx, rawURL, userAgent, favoritesOnly, isStrictPrivateLANURL(baseURL, h.hasCustomHTTPClient()))
}

func (h *Handler) loadHDHomeRunFromSourceWithPolicy(ctx context.Context, rawURL, userAgent string, favoritesOnly, allowPrivateLAN bool) ([]M3UChannel, *hdHomeRunDiscoverPayload, error) {
	baseURL, err := normalizeHDHomeRunBaseURL(rawURL)
	if err != nil {
		return nil, nil, err
	}
	parsedBase, err := url.Parse(baseURL)
	if err != nil {
		return nil, nil, err
	}

	var disc hdHomeRunDiscoverPayload
	var discPtr *hdHomeRunDiscoverPayload
	if dErr := h.fetchHDHomeRunJSONWithPolicy(ctx, baseURL+"/discover.json", userAgent, allowPrivateLAN, &disc); dErr == nil {
		discPtr = &disc
	}

	lineupURL := baseURL + "/lineup.json"
	if discPtr != nil && strings.TrimSpace(discPtr.LineupURL) != "" {
		rawLU := strings.TrimSpace(discPtr.LineupURL)
		parsedLU, lErr := url.Parse(rawLU)
		if lErr != nil {
			return nil, discPtr, fmt.Errorf("invalid hdhomerun LineupURL: %w", lErr)
		}
		resolvedLU := parsedBase.ResolveReference(parsedLU)
		if !strings.EqualFold(resolvedLU.Scheme, "http") && !strings.EqualFold(resolvedLU.Scheme, "https") {
			return nil, discPtr, fmt.Errorf("unsupported hdhomerun LineupURL scheme %q", resolvedLU.Scheme)
		}
		if !sameHTTPOrigin(parsedBase, resolvedLU) {
			return nil, discPtr, fmt.Errorf("blocked cross-origin hdhomerun LineupURL %q", rawLU)
		}
		lineupURL = resolvedLU.String()
	}

	var lineup []hdHomeRunLineupEntry
	if err := h.fetchHDHomeRunJSONWithPolicy(ctx, lineupURL, userAgent, allowPrivateLAN, &lineup); err != nil {
		return nil, discPtr, err
	}

	channels := make([]M3UChannel, 0, min(len(lineup), maxM3UChannels))
	for i, item := range lineup {
		if len(channels) >= maxM3UChannels {
			break
		}
		if favoritesOnly && item.Favorite != 1 {
			continue
		}
		streamURL := strings.TrimSpace(item.URL)
		if streamURL == "" {
			continue
		}
		num := strings.TrimSpace(item.GuideNumber)
		if num == "" {
			num = strconv.Itoa(i + 1)
		}
		name := strings.TrimSpace(item.GuideName)
		if name == "" {
			name = "Channel " + num
		}
		group := ""
		if item.HD == 1 {
			group = "HD"
		}
		channels = append(channels, M3UChannel{
			TvgID:      num,
			TvgName:    name,
			GroupTitle: group,
			Number:     num,
			Name:       name,
			URL:        streamURL,
		})
	}
	return channels, discPtr, nil
}

// DiscoverTuners returns configured tuner hosts enriched with live HDHomeRun discovery metadata,
// and optionally probes probeURL (e.g. "http://192.168.1.50") for an unconfigured HDHomeRun device on the LAN.
func (h *Handler) DiscoverTuners(ctx context.Context, probeURL string) []TunerHostInfo {
	if ctx == nil {
		ctx = context.Background()
	}
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	existing := h.SnapshotTunerHosts()
	seenURLs := make(map[string]bool, len(existing))
	seenDevices := make(map[string]bool, len(existing))

	for i := range existing {
		normBase, err := normalizeHDHomeRunBaseURL(existing[i].Url)
		if err == nil {
			seenURLs[strings.ToLower(normBase)] = true
		}
		if existing[i].DeviceId != "" {
			seenDevices[strings.ToLower(existing[i].DeviceId)] = true
		}
		if strings.EqualFold(existing[i].Type, "hdhomerun") && err == nil {
			var disc hdHomeRunDiscoverPayload
			if dErr := h.fetchHDHomeRunJSONWithPolicy(probeCtx, normBase+"/discover.json", existing[i].UserAgent, existing[i].AllowPrivateNetwork, &disc); dErr == nil {
				if existing[i].FriendlyName == "" && disc.FriendlyName != "" {
					existing[i].FriendlyName = disc.FriendlyName
				}
				if existing[i].DeviceId == "" && disc.DeviceID != "" {
					existing[i].DeviceId = disc.DeviceID
					seenDevices[strings.ToLower(disc.DeviceID)] = true
				}
				if existing[i].TunerCount <= 0 && disc.TunerCount > 0 {
					existing[i].TunerCount = disc.TunerCount
				}
			}
		}
	}

	if strings.TrimSpace(probeURL) != "" {
		if normBase, err := normalizeHDHomeRunBaseURL(probeURL); err == nil && !seenURLs[strings.ToLower(normBase)] {
			allowLAN := isStrictPrivateLANURL(normBase, h.hasCustomHTTPClient())
			var disc hdHomeRunDiscoverPayload
			if dErr := h.fetchHDHomeRunJSONWithPolicy(probeCtx, normBase+"/discover.json", "", allowLAN, &disc); dErr == nil {
				if disc.DeviceID == "" || !seenDevices[strings.ToLower(disc.DeviceID)] {
					friendly := strings.TrimSpace(disc.FriendlyName)
					if friendly == "" {
						friendly = "HDHomeRun"
					}
					existing = append(existing, TunerHostInfo{
						Url:                 normBase,
						Type:                "hdhomerun",
						DeviceId:            disc.DeviceID,
						FriendlyName:        friendly,
						TunerCount:          disc.TunerCount,
						AllowHWTranscoding:  true,
						AllowPrivateNetwork: allowLAN,
					})
				}
			}
		}
	}
	if existing == nil {
		return []TunerHostInfo{}
	}
	return existing
}

func (h *Handler) loadManualChannelMappingsByProvider(ctx context.Context, activeProviderIDs map[string]struct{}) map[string]map[string][]string {
	if h.store == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var raw string
	err := h.store.Reader.QueryRowContext(ctx,
		`SELECT value_json FROM server_config WHERE key = ?`, "compatext:state",
	).Scan(&raw)
	if err != nil || strings.TrimSpace(raw) == "" {
		return nil
	}
	var parsed struct {
		ChannelMappings map[string][]struct {
			Name  string `json:"Name"`  // TunerChannelId
			Value string `json:"Value"` // ProviderChannelId
		} `json:"channel_mappings"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil || len(parsed.ChannelMappings) == 0 {
		return nil
	}
	out := make(map[string]map[string][]string)
	for provID, list := range parsed.ChannelMappings {
		if activeProviderIDs != nil {
			if _, ok := activeProviderIDs[provID]; !ok {
				continue
			}
		}
		byProviderCh := make(map[string][]string)
		for _, m := range list {
			tunerChID := strings.TrimSpace(m.Name)
			provChID := strings.ToLower(strings.TrimSpace(m.Value))
			if tunerChID == "" || provChID == "" {
				continue
			}
			dup := false
			for _, existing := range byProviderCh[provChID] {
				if strings.EqualFold(existing, tunerChID) {
					dup = true
					break
				}
			}
			if !dup {
				byProviderCh[provChID] = append(byProviderCh[provChID], tunerChID)
			}
		}
		if len(byProviderCh) > 0 {
			out[provID] = byProviderCh
		}
	}
	return out
}

func (h *Handler) loadManualChannelMappingsForProvider(ctx context.Context, providerID string) map[string][]string {
	if strings.TrimSpace(providerID) == "" {
		return nil
	}
	byProv := h.loadManualChannelMappingsByProvider(ctx, map[string]struct{}{providerID: {}})
	return byProv[providerID]
}

func (h *Handler) loadManualChannelMappingsLocked(ctx context.Context) map[string][]string {
	activeIDs := make(map[string]struct{}, len(h.state.ListingProviders))
	for _, lp := range h.state.ListingProviders {
		activeIDs[lp.Id] = struct{}{}
	}
	byProv := h.loadManualChannelMappingsByProvider(ctx, activeIDs)
	if len(byProv) == 0 {
		return nil
	}
	merged := make(map[string][]string)
	for _, m := range byProv {
		for provChID, tunerIDs := range m {
			for _, tid := range tunerIDs {
				dup := false
				for _, existing := range merged[provChID] {
					if strings.EqualFold(existing, tid) {
						dup = true
						break
					}
				}
				if !dup {
					merged[provChID] = append(merged[provChID], tid)
				}
			}
		}
	}
	return merged
}

func (h *Handler) loadManualChannelMappings(ctx context.Context) map[string][]string {
	providers := h.SnapshotListingProviders()
	activeIDs := make(map[string]struct{}, len(providers))
	for _, lp := range providers {
		activeIDs[lp.Id] = struct{}{}
	}
	byProv := h.loadManualChannelMappingsByProvider(ctx, activeIDs)
	if len(byProv) == 0 {
		return nil
	}
	merged := make(map[string][]string)
	for _, m := range byProv {
		for provChID, tunerIDs := range m {
			for _, tid := range tunerIDs {
				dup := false
				for _, existing := range merged[provChID] {
					if strings.EqualFold(existing, tid) {
						dup = true
						break
					}
				}
				if !dup {
					merged[provChID] = append(merged[provChID], tid)
				}
			}
		}
	}
	return merged
}

func (h *Handler) pruneOrphanedChannelMappings(ctx context.Context) {
	if h.store == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	h.mu.RLock()
	activeProviders := make(map[string]struct{}, len(h.state.ListingProviders))
	for _, lp := range h.state.ListingProviders {
		activeProviders[lp.Id] = struct{}{}
	}
	activeChannels := make(map[string]struct{}, len(h.state.Channels))
	for _, ch := range h.state.Channels {
		activeChannels[ch.Id] = struct{}{}
	}
	h.mu.RUnlock()

	var raw string
	err := h.store.Reader.QueryRowContext(ctx,
		`SELECT value_json FROM server_config WHERE key = ?`, "compatext:state",
	).Scan(&raw)
	if err != nil || strings.TrimSpace(raw) == "" {
		return
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(raw), &root); err != nil || root == nil {
		return
	}
	cmRaw, ok := root["channel_mappings"]
	if !ok || cmRaw == nil {
		return
	}
	cmBytes, err := json.Marshal(cmRaw)
	if err != nil {
		return
	}
	type mappingEntry struct {
		Name  string `json:"Name"`
		Value string `json:"Value"`
	}
	var cm map[string][]mappingEntry
	if err := json.Unmarshal(cmBytes, &cm); err != nil || len(cm) == 0 {
		return
	}

	modified := false
	cleaned := make(map[string][]mappingEntry, len(cm))
	for provID, list := range cm {
		if _, exists := activeProviders[provID]; !exists {
			modified = true
			continue
		}
		kept := make([]mappingEntry, 0, len(list))
		for _, m := range list {
			tunerID := strings.TrimSpace(m.Name)
			provCh := strings.TrimSpace(m.Value)
			if tunerID == "" || provCh == "" {
				modified = true
				continue
			}
			if _, exists := activeChannels[tunerID]; !exists {
				modified = true
				continue
			}
			kept = append(kept, m)
		}
		if len(kept) == 0 {
			if len(list) > 0 {
				modified = true
			}
			continue
		}
		cleaned[provID] = kept
	}
	if !modified {
		return
	}
	root["channel_mappings"] = cleaned
	updated, err := json.Marshal(root)
	if err != nil {
		return
	}
	_, _ = h.store.Writer.ExecContext(ctx,
		`INSERT INTO server_config (key, value_json, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value_json = excluded.value_json, updated_at = excluded.updated_at`,
		"compatext:state", string(updated), time.Now().UTC().Unix(),
	)
}

// RefreshAllGuides reloads all configured XMLTV listing providers, applies per-provider manual
// ChannelMappings first, clears stale XMLTV programs for channels no longer matched,
// prunes historical programs that ended more than 24 hours ago, reschedules SeriesTimers, and persists state.
func (h *Handler) RefreshAllGuides(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	type fetchedGuide struct {
		providerID   string
		providerType string
		rawPath      string
		guide        *XMLTVGuide
	}
	providers := h.SnapshotListingProviders()
	var guides []fetchedGuide
	var firstErr error
	for _, lp := range providers {
		rawPath := strings.TrimSpace(lp.Path)
		if !strings.EqualFold(lp.Type, "xmltv") || rawPath == "" {
			continue
		}
		allowPrivateLAN := lp.AllowPrivateNetwork
		if !lp.NetworkPolicySet {
			allowPrivateLAN = allowPrivateLAN || isStrictPrivateLANURL(rawPath, h.hasCustomHTTPClient())
		}
		g, err := h.loadXMLTVFromSourceWithPolicy(ctx, rawPath, allowPrivateLAN)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if g != nil && len(g.Programmes) > 0 {
			guides = append(guides, fetchedGuide{
				providerID:   lp.Id,
				providerType: lp.Type,
				rawPath:      rawPath,
				guide:        g,
			})
		}
	}
	cutoff := time.Now().UTC().Add(-24 * time.Hour)

	h.mu.Lock()
	snapshot := cloneLiveTVState(h.state)
	changed := false

	activeProviders := make(map[string]ListingProviderInfo, len(h.state.ListingProviders))
	activeProviderIDs := make(map[string]struct{}, len(h.state.ListingProviders))
	for _, lp := range h.state.ListingProviders {
		activeProviders[lp.Id] = lp
		activeProviderIDs[lp.Id] = struct{}{}
	}
	mappingsByProvider := h.loadManualChannelMappingsByProvider(ctx, activeProviderIDs)

	allMatchedChannelIDs := make(map[string]struct{})
	for _, fg := range guides {
		curLP, exists := activeProviders[fg.providerID]
		if !exists || !strings.EqualFold(curLP.Type, fg.providerType) || strings.TrimSpace(curLP.Path) != fg.rawPath {
			continue
		}
		matched := h.applyXMLTVGuideWithMappingsLocked(fg.guide, mappingsByProvider[fg.providerID])
		for chID := range matched {
			allMatchedChannelIDs[chID] = struct{}{}
		}
		if len(matched) > 0 {
			changed = true
		}
	}

	// Clear stale XMLTV-sourced programs for channels no longer matched by any active listing provider
	// (e.g. after unmapping a channel or removing a provider), only when all fetches succeeded.
	if firstErr == nil && len(h.state.Programs) > 0 {
		kept := make([]ProgramInfo, 0, len(h.state.Programs))
		for _, p := range h.state.Programs {
			if p.Source == "xmltv" {
				if _, ok := allMatchedChannelIDs[p.ChannelId]; !ok {
					changed = true
					continue
				}
			}
			kept = append(kept, p)
		}
		h.state.Programs = kept
	}

	// Prune historical EPG programs expired for more than 24 hours.
	if len(h.state.Programs) > 0 {
		kept := make([]ProgramInfo, 0, len(h.state.Programs))
		for _, p := range h.state.Programs {
			if !p.EndDate.IsZero() && p.EndDate.Before(cutoff) {
				changed = true
				continue
			}
			kept = append(kept, p)
		}
		h.state.Programs = kept
	}

	if changed && len(h.state.SeriesTimers) > 0 {
		for _, st := range h.state.SeriesTimers {
			h.scheduleTimersForSeriesLocked(st)
		}
	}

	var saveErr error
	if changed {
		saveErr = h.saveStateLocked(ctx)
		if saveErr != nil {
			h.state = snapshot
		}
	}
	h.mu.Unlock()

	if saveErr != nil {
		return saveErr
	}
	return firstErr
}

func (h *Handler) applyXMLTVGuideLocked(guide *XMLTVGuide) {
	h.applyXMLTVGuideWithMappingsLocked(guide, h.loadManualChannelMappingsLocked(context.Background()))
}

func (h *Handler) applyXMLTVGuideWithMappingsLocked(guide *XMLTVGuide, manualByProviderCh map[string][]string) map[string]struct{} {
	if guide == nil || len(guide.Programmes) == 0 || len(h.state.Channels) == 0 {
		return nil
	}

	byTvgID := make(map[string]ChannelInfo, len(h.state.Channels))
	byID := make(map[string]ChannelInfo, len(h.state.Channels))
	byName := make(map[string]ChannelInfo, len(h.state.Channels))
	mappedTunerSet := make(map[string]bool)
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
	for _, tunerIDs := range manualByProviderCh {
		for _, tid := range tunerIDs {
			mappedTunerSet[strings.ToLower(strings.TrimSpace(tid))] = true
		}
	}

	xmlDisplayNames := make(map[string][]string, len(guide.Channels))
	for _, xc := range guide.Channels {
		if k := strings.ToLower(strings.TrimSpace(xc.ID)); k != "" {
			xmlDisplayNames[k] = xc.DisplayNames
		}
	}

	matchChannels := func(xmlChID string) []ChannelInfo {
		key := strings.ToLower(strings.TrimSpace(xmlChID))
		if key == "" {
			return nil
		}
		// Priority 1: explicit manual ChannelMappings (ProviderChannelId -> []TunerChannelId)
		if tunerIDs, ok := manualByProviderCh[key]; ok && len(tunerIDs) > 0 {
			var matched []ChannelInfo
			for _, tid := range tunerIDs {
				if ch, exists := byID[strings.ToLower(strings.TrimSpace(tid))]; exists {
					matched = append(matched, ch)
				}
			}
			if len(matched) > 0 {
				return matched
			}
		}
		// Priority 2: automatic TvgId / Id / Name / XMLTV display-name matching
		// (skip channels that the admin explicitly pinned to a different ProviderChannelId)
		if ch, ok := byTvgID[key]; ok && !mappedTunerSet[strings.ToLower(ch.Id)] {
			return []ChannelInfo{ch}
		}
		if ch, ok := byID[key]; ok && !mappedTunerSet[strings.ToLower(ch.Id)] {
			return []ChannelInfo{ch}
		}
		if ch, ok := byName[key]; ok && !mappedTunerSet[strings.ToLower(ch.Id)] {
			return []ChannelInfo{ch}
		}
		for _, dn := range xmlDisplayNames[key] {
			dnKey := strings.ToLower(strings.TrimSpace(dn))
			if dnKey == "" {
				continue
			}
			if ch, ok := byName[dnKey]; ok && !mappedTunerSet[strings.ToLower(ch.Id)] {
				return []ChannelInfo{ch}
			}
			if ch, ok := byTvgID[dnKey]; ok && !mappedTunerSet[strings.ToLower(ch.Id)] {
				return []ChannelInfo{ch}
			}
		}
		return nil
	}

	matchedChannelIDs := make(map[string]struct{})
	newPrograms := make([]ProgramInfo, 0, len(guide.Programmes))
	serverID := h.serverID()

	for _, prog := range guide.Programmes {
		chs := matchChannels(prog.Channel)
		if len(chs) == 0 {
			continue
		}
		isMovie, isSeries := classifyXMLTVCategories(prog.Categories)
		if prog.EpisodeNum != "" || prog.SubTitle != "" {
			isSeries = true
		}
		for _, ch := range chs {
			matchedChannelIDs[ch.Id] = struct{}{}
			newPrograms = append(newPrograms, ProgramInfo{
				Id:            guid.New(),
				Name:          prog.Title,
				EpisodeTitle:  prog.SubTitle,
				EpisodeNumber: prog.EpisodeNum,
				ServerId:      serverID,
				Type:          "Program",
				ChannelId:     ch.Id,
				ChannelName:   ch.Name,
				Overview:      prog.Desc,
				StartDate:     prog.Start,
				EndDate:       prog.Stop,
				IsMovie:       isMovie,
				IsSeries:      isSeries,
				IsRepeat:      prog.PreviouslyShown,
				Source:        "xmltv",
			})
		}
	}

	if len(matchedChannelIDs) == 0 {
		return nil
	}

	kept := make([]ProgramInfo, 0, len(h.state.Programs)+len(newPrograms))
	for _, p := range h.state.Programs {
		if _, replace := matchedChannelIDs[p.ChannelId]; !replace {
			kept = append(kept, p)
		}
	}
	kept = append(kept, newPrograms...)
	h.state.Programs = kept
	return matchedChannelIDs
}

// DeleteListingProvider removes a listing provider by ID, prunes orphaned channel mappings, and persists state.
func (h *Handler) DeleteListingProvider(ctx context.Context, id string) bool {
	h.mu.Lock()
	for i, lp := range h.state.ListingProviders {
		if lp.Id == id {
			h.state.ListingProviders = append(h.state.ListingProviders[:i], h.state.ListingProviders[i+1:]...)
			h.saveStateLocked(ctx)
			h.mu.Unlock()
			h.pruneOrphanedChannelMappings(ctx)
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

func cloneLiveTVState(st liveTVState) liveTVState {
	out := liveTVState{
		Channels:         append([]ChannelInfo(nil), st.Channels...),
		Programs:         append([]ProgramInfo(nil), st.Programs...),
		Recordings:       append([]RecordingInfo(nil), st.Recordings...),
		Timers:           append([]TimerInfo(nil), st.Timers...),
		SeriesTimers:     make([]SeriesTimerInfo, len(st.SeriesTimers)),
		TunerHosts:       append([]TunerHostInfo(nil), st.TunerHosts...),
		ListingProviders: append([]ListingProviderInfo(nil), st.ListingProviders...),
	}
	copy(out.SeriesTimers, st.SeriesTimers)
	for i := range out.SeriesTimers {
		if st.SeriesTimers[i].Days != nil {
			out.SeriesTimers[i].Days = append([]string(nil), st.SeriesTimers[i].Days...)
		}
	}
	return out
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
