package trakt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ambra-media/ambra/internal/core/session"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/pkg/netutil"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

const (
	PluginID                   = "ambra.builtin.trakt"
	defaultBaseURL             = "https://api.trakt.tv"
	defaultPriority            = 40
	defaultTimeout             = 10 * time.Second
	defaultPauseThrottleWindow = 60 * time.Second
	defaultCompleteDedupWindow = 2 * time.Minute
	scrobbleCompletionPct      = 80.0
	traktAPIVersion            = "2"
	maxErrBodyPreview          = 1024
	maxDiscardRespBytes        = 1 << 20
	traktWorkerPoolSize        = 8
	traktWorkerQueueSize       = 128
)

var _ plugins.Plugin = (*Plugin)(nil)

// Plugin implements a built-in Trakt.tv scrobble and watched-history sync notifier.
type Plugin struct {
	mu               sync.RWMutex
	enabled          bool
	clientID         string
	accessToken      string
	allowedUserID    string
	boundUserID      string
	scrobblePlayback bool
	syncWatched      bool
	priority         int
	baseURL          string
	client           *http.Client

	stateMu             sync.Mutex
	pausedAt            map[string]time.Time
	completedAt         map[string]time.Time
	pauseThrottleWindow time.Duration
	completeDedupWindow time.Duration
	nowFunc             func() time.Time
}

type configSnapshot struct {
	enabled          bool
	clientID         string
	accessToken      string
	allowedUserID    string
	scrobblePlayback bool
	syncWatched      bool
	priority         int
	baseURL          string
	client           *http.Client
}

// New creates a new Trakt plugin instance (disabled by default).
func New() *Plugin {
	return &Plugin{
		enabled:             false,
		scrobblePlayback:    true,
		syncWatched:         true,
		priority:            defaultPriority,
		baseURL:             defaultBaseURL,
		client:              netutil.NewSafeHTTPClient(defaultTimeout, false),
		pausedAt:            make(map[string]time.Time),
		completedAt:         make(map[string]time.Time),
		pauseThrottleWindow: defaultPauseThrottleWindow,
		completeDedupWindow: defaultCompleteDedupWindow,
		nowFunc:             time.Now,
	}
}

// SetHTTPClient overrides the HTTP client used for Trakt API calls (useful in tests).
func (p *Plugin) SetHTTPClient(c *http.Client) {
	if c == nil {
		return
	}
	p.mu.Lock()
	p.client = c
	p.mu.Unlock()
}

func (p *Plugin) snapshot() configSnapshot {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return configSnapshot{
		enabled:          p.enabled,
		clientID:         p.clientID,
		accessToken:      p.accessToken,
		allowedUserID:    p.allowedUserID,
		scrobblePlayback: p.scrobblePlayback,
		syncWatched:      p.syncWatched,
		priority:         p.priority,
		baseURL:          p.baseURL,
		client:           p.client,
	}
}

func (p *Plugin) Manifest() plugins.Manifest {
	snap := p.snapshot()
	return plugins.Manifest{
		ID:          PluginID,
		Name:        "Trakt Scrobble & Sync",
		Version:     "1.0.0",
		Description: "Scrobbles playback progress and syncs watched history to Trakt.tv (disabled by default)",
		Author:      "Ambra",
		BuiltIn:     true,
		Enabled:     snap.enabled,
		Priority:    snap.priority,
		Kinds:       []plugins.Kind{plugins.KindNotifier},
	}
}

// Configure updates plugin configuration in a thread-safe manner.
func (p *Plugin) Configure(cfg map[string]any) error {
	if cfg == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	if v, ok := firstBool(cfg, "enabled", "Enabled"); ok {
		p.enabled = v
	}
	if v, ok := firstString(cfg, "client_id", "ClientId", "trakt_client_id"); ok {
		p.clientID = strings.TrimSpace(v)
	}
	if v, ok := firstString(cfg, "access_token", "AccessToken", "trakt_access_token"); ok {
		p.accessToken = strings.TrimSpace(v)
	}
	if v, ok := firstString(cfg, "user_id", "UserId", "UserID", "allowed_user_id", "AllowedUserId"); ok {
		p.allowedUserID = strings.TrimSpace(v)
		p.boundUserID = ""
	}
	if v, ok := firstBool(cfg, "scrobble_playback", "ScrobblePlayback"); ok {
		p.scrobblePlayback = v
	}
	if v, ok := firstBool(cfg, "sync_watched", "SyncWatched"); ok {
		p.syncWatched = v
	}
	if v, ok := firstInt(cfg, "priority", "Priority"); ok {
		p.priority = v
	}
	if v, ok := firstString(cfg, "base_url", "BaseUrl"); ok {
		if trimmed := strings.TrimRight(strings.TrimSpace(v), "/"); trimmed != "" {
			u, err := url.Parse(trimmed)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return fmt.Errorf("trakt: invalid base_url %q: must be http(s) URL", v)
			}
			p.baseURL = trimmed
		}
	}
	return nil
}

// Trakt API v2 payload types.

type traktIDs struct {
	Tmdb int    `json:"tmdb,omitempty"`
	Imdb string `json:"imdb,omitempty"`
	Tvdb int    `json:"tvdb,omitempty"`
}

func (id traktIDs) isEmpty() bool {
	return id.Tmdb == 0 && id.Imdb == "" && id.Tvdb == 0
}

type traktMovie struct {
	Title string    `json:"title,omitempty"`
	Year  int       `json:"year,omitempty"`
	IDs   *traktIDs `json:"ids,omitempty"`
}

type traktShow struct {
	Title string    `json:"title,omitempty"`
	Year  int       `json:"year,omitempty"`
	IDs   *traktIDs `json:"ids,omitempty"`
}

type traktEpisode struct {
	Season int       `json:"season"`
	Number int       `json:"number"`
	IDs    *traktIDs `json:"ids,omitempty"`
}

type scrobblePayload struct {
	Movie    *traktMovie   `json:"movie,omitempty"`
	Show     *traktShow    `json:"show,omitempty"`
	Episode  *traktEpisode `json:"episode,omitempty"`
	Progress float64       `json:"progress"`
}

type historyEpisode struct {
	Number int `json:"number"`
}

type historySeason struct {
	Number   int              `json:"number"`
	Episodes []historyEpisode `json:"episodes"`
}

type historyShow struct {
	Title   string          `json:"title,omitempty"`
	Year    int             `json:"year,omitempty"`
	IDs     *traktIDs       `json:"ids,omitempty"`
	Seasons []historySeason `json:"seasons"`
}

type syncHistoryPayload struct {
	Movies []traktMovie  `json:"movies,omitempty"`
	Shows  []historyShow `json:"shows,omitempty"`
}

// ScrobbleStart sends a POST /scrobble/start request to Trakt.
func (p *Plugin) ScrobbleStart(ctx context.Context, item *sqlite.ItemRecord, series *sqlite.ItemRecord, progressPct float64) error {
	return p.scrobble(ctx, "/scrobble/start", item, series, progressPct)
}

// ScrobblePause sends a POST /scrobble/pause request to Trakt.
func (p *Plugin) ScrobblePause(ctx context.Context, item *sqlite.ItemRecord, series *sqlite.ItemRecord, progressPct float64) error {
	return p.scrobble(ctx, "/scrobble/pause", item, series, progressPct)
}

// ScrobbleStop sends a POST /scrobble/stop request to Trakt.
func (p *Plugin) ScrobbleStop(ctx context.Context, item *sqlite.ItemRecord, series *sqlite.ItemRecord, progressPct float64) error {
	return p.scrobble(ctx, "/scrobble/stop", item, series, progressPct)
}

func (p *Plugin) scrobble(ctx context.Context, endpoint string, item *sqlite.ItemRecord, series *sqlite.ItemRecord, progressPct float64) error {
	snap := p.snapshot()
	if !snap.enabled || !snap.scrobblePlayback || snap.clientID == "" || snap.accessToken == "" {
		return nil
	}
	payload := buildScrobblePayload(item, series, progressPct)
	if payload == nil {
		return nil
	}
	return postTrakt(ctx, snap, endpoint, payload)
}

// SyncWatched sends a POST /sync/history request to Trakt.
func (p *Plugin) SyncWatched(ctx context.Context, item *sqlite.ItemRecord, series *sqlite.ItemRecord) error {
	snap := p.snapshot()
	if !snap.enabled || !snap.syncWatched || snap.clientID == "" || snap.accessToken == "" {
		return nil
	}
	payload := buildSyncHistoryPayload(item, series)
	if payload == nil {
		return nil
	}
	return postTrakt(ctx, snap, "/sync/history", payload)
}

func buildScrobblePayload(item *sqlite.ItemRecord, series *sqlite.ItemRecord, progressPct float64) *scrobblePayload {
	if item == nil {
		return nil
	}
	pct := clampProgress(progressPct)
	switch strings.ToLower(strings.TrimSpace(item.Type)) {
	case "movie":
		m := buildTraktMovie(item)
		return &scrobblePayload{
			Movie:    &m,
			Progress: pct,
		}
	case "episode":
		show := buildTraktShow(item, series)
		ep := buildTraktEpisode(item)
		return &scrobblePayload{
			Show:     &show,
			Episode:  &ep,
			Progress: pct,
		}
	default:
		return nil
	}
}

func buildSyncHistoryPayload(item *sqlite.ItemRecord, series *sqlite.ItemRecord) *syncHistoryPayload {
	if item == nil {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(item.Type)) {
	case "movie":
		return &syncHistoryPayload{
			Movies: []traktMovie{buildTraktMovie(item)},
		}
	case "episode":
		show := buildTraktShow(item, series)
		seasonNum := 0
		if item.ParentIndexNumber != nil {
			seasonNum = *item.ParentIndexNumber
		}
		epNum := 0
		if item.IndexNumber != nil {
			epNum = *item.IndexNumber
		}
		return &syncHistoryPayload{
			Shows: []historyShow{
				{
					Title: show.Title,
					Year:  show.Year,
					IDs:   show.IDs,
					Seasons: []historySeason{
						{
							Number: seasonNum,
							Episodes: []historyEpisode{
								{Number: epNum},
							},
						},
					},
				},
			},
		}
	default:
		return nil
	}
}

func buildTraktMovie(item *sqlite.ItemRecord) traktMovie {
	m := traktMovie{
		Title: strings.TrimSpace(item.Name),
	}
	if item.ProductionYear != nil && *item.ProductionYear > 0 {
		m.Year = *item.ProductionYear
	}
	if ids := extractTraktIDs(item.Extra.ProviderIDs); !ids.isEmpty() {
		m.IDs = &ids
	}
	return m
}

func buildTraktShow(item *sqlite.ItemRecord, series *sqlite.ItemRecord) traktShow {
	s := traktShow{}
	if series != nil {
		s.Title = strings.TrimSpace(series.Name)
		if series.ProductionYear != nil && *series.ProductionYear > 0 {
			s.Year = *series.ProductionYear
		}
		if ids := extractTraktIDs(series.Extra.ProviderIDs); !ids.isEmpty() {
			s.IDs = &ids
		}
	} else if item != nil {
		s.Title = strings.TrimSpace(item.Extra.SeriesName)
	}
	return s
}

func buildTraktEpisode(item *sqlite.ItemRecord) traktEpisode {
	ep := traktEpisode{}
	if item.ParentIndexNumber != nil {
		ep.Season = *item.ParentIndexNumber
	}
	if item.IndexNumber != nil {
		ep.Number = *item.IndexNumber
	}
	if ids := extractTraktIDs(item.Extra.ProviderIDs); !ids.isEmpty() {
		ep.IDs = &ids
	}
	return ep
}

func extractTraktIDs(providerIDs map[string]string) traktIDs {
	var ids traktIDs
	for k, v := range providerIDs {
		val := strings.TrimSpace(v)
		if val == "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(k)) {
		case "tmdb":
			if n, err := strconv.Atoi(val); err == nil && n > 0 {
				ids.Tmdb = n
			}
		case "imdb":
			ids.Imdb = val
		case "tvdb":
			if n, err := strconv.Atoi(val); err == nil && n > 0 {
				ids.Tvdb = n
			}
		}
	}
	return ids
}

func clampProgress(p float64) float64 {
	if math.IsNaN(p) || p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return p
}

func postTrakt(ctx context.Context, snap configSnapshot, endpoint string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("trakt marshal %s: %w", endpoint, err)
	}
	url := snap.baseURL + endpoint
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("trakt request %s: %w", endpoint, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("trakt-api-version", traktAPIVersion)
	req.Header.Set("trakt-api-key", snap.clientID)
	req.Header.Set("Authorization", "Bearer "+snap.accessToken)

	resp, err := snap.client.Do(req)
	if err != nil {
		return fmt.Errorf("trakt post %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		preview, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrBodyPreview))
		return fmt.Errorf("trakt %s: status %d: %s", endpoint, resp.StatusCode, strings.TrimSpace(string(preview)))
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxDiscardRespBytes))
	return nil
}

func (p *Plugin) shouldReportForUser(userID string) bool {
	uid := strings.TrimSpace(userID)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.allowedUserID != "" {
		return uid != "" && strings.EqualFold(uid, p.allowedUserID)
	}
	if uid == "" {
		return true
	}
	if p.boundUserID == "" {
		p.boundUserID = uid
		return true
	}
	return strings.EqualFold(uid, p.boundUserID)
}

func userItemKey(userID, itemID string) string {
	return strings.ToLower(strings.TrimSpace(userID)) + ":" + strings.ToLower(strings.TrimSpace(itemID))
}

func (p *Plugin) now() time.Time {
	if p.nowFunc != nil {
		return p.nowFunc()
	}
	return time.Now()
}

func (p *Plugin) clearPaused(userID, itemID string) {
	p.stateMu.Lock()
	delete(p.pausedAt, userItemKey(userID, itemID))
	p.stateMu.Unlock()
}

// allowPauseScrobble returns true if a pause scrobble should be sent (throttling repeated pause heartbeats).
func (p *Plugin) allowPauseScrobble(userID, itemID string) bool {
	key := userItemKey(userID, itemID)
	now := p.now()
	win := p.pauseThrottleWindow
	if win <= 0 {
		win = defaultPauseThrottleWindow
	}
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	if p.pausedAt == nil {
		p.pausedAt = make(map[string]time.Time)
	}
	if last, ok := p.pausedAt[key]; ok && now.Sub(last) < win {
		return false
	}
	if len(p.pausedAt) > 1024 {
		for k, ts := range p.pausedAt {
			if now.Sub(ts) >= win {
				delete(p.pausedAt, k)
			}
		}
	}
	p.pausedAt[key] = now
	return true
}

func (p *Plugin) markScrobbleCompleted(userID, itemID string) {
	key := userItemKey(userID, itemID)
	now := p.now()
	win := p.completeDedupWindow
	if win <= 0 {
		win = defaultCompleteDedupWindow
	}
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	if p.completedAt == nil {
		p.completedAt = make(map[string]time.Time)
	}
	if len(p.completedAt) > 1024 {
		for k, ts := range p.completedAt {
			if now.Sub(ts) >= win {
				delete(p.completedAt, k)
			}
		}
	}
	p.completedAt[key] = now
}

func (p *Plugin) wasRecentlyScrobbleCompleted(userID, itemID string) bool {
	key := userItemKey(userID, itemID)
	now := p.now()
	win := p.completeDedupWindow
	if win <= 0 {
		win = defaultCompleteDedupWindow
	}
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	if ts, ok := p.completedAt[key]; ok {
		if now.Sub(ts) < win {
			return true
		}
		delete(p.completedAt, key)
	}
	return false
}

// SubscribeEvents registers EventBus handlers for playback scrobbling and watched-history sync.
// Calling the returned function stops the plugin from processing subsequent events.
func (p *Plugin) SubscribeEvents(bus *eventbus.Bus, repo *sqlite.LibraryRepo) func() {
	if bus == nil {
		return func() {}
	}
	var unsubscribed atomic.Bool
	stopCh := make(chan struct{})
	jobs := make(chan func(), traktWorkerQueueSize)
	var stopOnce sync.Once

	for i := 0; i < traktWorkerPoolSize; i++ {
		go func() {
			for {
				select {
				case <-stopCh:
					return
				case job := <-jobs:
					if job != nil {
						job()
					}
				}
			}
		}()
	}

	enqueue := func(fn func()) {
		if unsubscribed.Load() {
			return
		}
		select {
		case jobs <- fn:
		default:
			// Drop when queue is saturated rather than spawning unbounded goroutines.
		}
	}

	isReady := func() bool {
		if unsubscribed.Load() || repo == nil {
			return false
		}
		snap := p.snapshot()
		return snap.enabled && snap.clientID != "" && snap.accessToken != ""
	}

	bus.Subscribe(eventbus.EventPlaybackStart, func(_ context.Context, e eventbus.Event) {
		if !isReady() {
			return
		}
		ev, ok := parsePlaybackEvent(e.Payload)
		if !ok || ev.itemID == "" || !p.shouldReportForUser(ev.userID) {
			return
		}
		p.clearPaused(ev.userID, ev.itemID)
		enqueue(func() {
			ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
			defer cancel()
			item, series := lookupItemAndSeries(ctx, repo, ev.itemID)
			if item == nil {
				return
			}
			pct := computeProgress(ev.positionTicks, ev.runtimeTicks, item.RuntimeTicks)
			_ = p.ScrobbleStart(ctx, item, series, pct)
		})
	})

	bus.Subscribe(eventbus.EventPlaybackProgress, func(_ context.Context, e eventbus.Event) {
		if !isReady() {
			return
		}
		ev, ok := parsePlaybackEvent(e.Payload)
		if !ok || ev.itemID == "" || !p.shouldReportForUser(ev.userID) {
			return
		}
		if !ev.isPaused {
			p.clearPaused(ev.userID, ev.itemID)
			return
		}
		if !p.allowPauseScrobble(ev.userID, ev.itemID) {
			return
		}
		enqueue(func() {
			ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
			defer cancel()
			item, series := lookupItemAndSeries(ctx, repo, ev.itemID)
			if item == nil {
				return
			}
			pct := computeProgress(ev.positionTicks, ev.runtimeTicks, item.RuntimeTicks)
			_ = p.ScrobblePause(ctx, item, series, pct)
		})
	})

	bus.Subscribe(eventbus.EventPlaybackStop, func(_ context.Context, e eventbus.Event) {
		if !isReady() {
			return
		}
		ev, ok := parsePlaybackEvent(e.Payload)
		if !ok || ev.itemID == "" || !p.shouldReportForUser(ev.userID) {
			return
		}
		p.clearPaused(ev.userID, ev.itemID)
		if ev.played || computeProgress(ev.positionTicks, ev.runtimeTicks, nil) >= scrobbleCompletionPct {
			p.markScrobbleCompleted(ev.userID, ev.itemID)
		}
		enqueue(func() {
			ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
			defer cancel()
			item, series := lookupItemAndSeries(ctx, repo, ev.itemID)
			if item == nil {
				return
			}
			var pct float64
			if ev.played && ev.positionTicks == 0 {
				pct = 100
			} else {
				pct = computeProgress(ev.positionTicks, ev.runtimeTicks, item.RuntimeTicks)
			}
			if pct >= scrobbleCompletionPct {
				p.markScrobbleCompleted(ev.userID, ev.itemID)
			}
			_ = p.ScrobbleStop(ctx, item, series, pct)
		})
	})

	bus.Subscribe(eventbus.EventUserDataChanged, func(_ context.Context, e eventbus.Event) {
		if !isReady() {
			return
		}
		entries := parseUserDataChangedEvent(e.Payload)
		for _, entry := range entries {
			if !entry.played || entry.itemID == "" || !p.shouldReportForUser(entry.userID) {
				continue
			}
			if p.wasRecentlyScrobbleCompleted(entry.userID, entry.itemID) {
				continue
			}
			itemID := entry.itemID
			enqueue(func() {
				ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
				defer cancel()
				item, series := lookupItemAndSeries(ctx, repo, itemID)
				if item == nil {
					return
				}
				_ = p.SyncWatched(ctx, item, series)
			})
		}
	})

	return func() {
		unsubscribed.Store(true)
		stopOnce.Do(func() {
			close(stopCh)
		})
	}
}

func lookupItemAndSeries(ctx context.Context, repo *sqlite.LibraryRepo, itemID string) (*sqlite.ItemRecord, *sqlite.ItemRecord) {
	item, err := repo.GetItemRecordByID(ctx, itemID)
	if err != nil || item == nil {
		return nil, nil
	}
	var series *sqlite.ItemRecord
	if strings.EqualFold(item.Type, "Episode") && item.SeriesID != "" {
		if s, sErr := repo.GetItemRecordByID(ctx, item.SeriesID); sErr == nil && s != nil {
			series = s
		}
	}
	return item, series
}

func computeProgress(positionTicks, payloadRuntimeTicks int64, itemRuntimeTicks *int64) float64 {
	rt := payloadRuntimeTicks
	if rt <= 0 && itemRuntimeTicks != nil {
		rt = *itemRuntimeTicks
	}
	if rt <= 0 || positionTicks <= 0 {
		return 0
	}
	return clampProgress((float64(positionTicks) / float64(rt)) * 100)
}

type playbackEventData struct {
	userID        string
	itemID        string
	positionTicks int64
	runtimeTicks  int64
	isPaused      bool
	played        bool
}

func extractPlaybackStructFields(v session.PlaybackEventPayload) playbackEventData {
	return playbackEventData{
		userID:        strings.TrimSpace(v.UserId),
		itemID:        strings.TrimSpace(v.ItemId),
		positionTicks: v.PositionTicks,
		runtimeTicks:  v.RuntimeTicks,
		isPaused:      v.IsPaused,
		played:        v.Played,
	}
}

func parsePlaybackEvent(payload any) (playbackEventData, bool) {
	switch v := payload.(type) {
	case session.PlaybackEventPayload:
		return extractPlaybackStructFields(v), true
	case *session.PlaybackEventPayload:
		if v == nil {
			return playbackEventData{}, false
		}
		return extractPlaybackStructFields(*v), true
	case map[string]any:
		return parsePlaybackMap(v), true
	default:
		if payload == nil {
			return playbackEventData{}, false
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			return playbackEventData{}, false
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			return playbackEventData{}, false
		}
		return parsePlaybackMap(m), true
	}
}

func parsePlaybackMap(m map[string]any) playbackEventData {
	var out playbackEventData
	if s, ok := firstString(m, "user_id", "UserId", "UserID"); ok {
		out.userID = strings.TrimSpace(s)
	}
	if s, ok := firstString(m, "item_id", "ItemId", "ItemID"); ok {
		out.itemID = strings.TrimSpace(s)
	}
	if n, ok := firstInt64(m, "position_ticks", "PositionTicks"); ok {
		out.positionTicks = n
	}
	if n, ok := firstInt64(m, "runtime_ticks", "RuntimeTicks", "RunTimeTicks"); ok {
		out.runtimeTicks = n
	}
	if b, ok := firstBool(m, "is_paused", "IsPaused", "paused"); ok {
		out.isPaused = b
	}
	if b, ok := firstBool(m, "played", "Played"); ok {
		out.played = b
	}
	return out
}

type userDataEventEntry struct {
	userID string
	itemID string
	played bool
}

func parseUserDataChangedEvent(payload any) []userDataEventEntry {
	switch v := payload.(type) {
	case session.UserDataChangedPayload:
		uid := strings.TrimSpace(v.UserId)
		out := make([]userDataEventEntry, 0, len(v.UserDataList))
		for _, entry := range v.UserDataList {
			out = append(out, userDataEventEntry{
				userID: uid,
				itemID: strings.TrimSpace(entry.ItemId),
				played: entry.Played,
			})
		}
		return out
	case *session.UserDataChangedPayload:
		if v == nil {
			return nil
		}
		uid := strings.TrimSpace(v.UserId)
		out := make([]userDataEventEntry, 0, len(v.UserDataList))
		for _, entry := range v.UserDataList {
			out = append(out, userDataEventEntry{
				userID: uid,
				itemID: strings.TrimSpace(entry.ItemId),
				played: entry.Played,
			})
		}
		return out
	case map[string]any:
		return parseUserDataMap(v)
	default:
		if payload == nil {
			return nil
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil
		}
		return parseUserDataMap(m)
	}
}

func parseUserDataMap(m map[string]any) []userDataEventEntry {
	uid, _ := firstString(m, "user_id", "UserId", "UserID")
	uid = strings.TrimSpace(uid)
	if listRaw, ok := m["UserDataList"]; ok {
		return parseUserDataList(listRaw, uid)
	}
	if listRaw, ok := m["user_data_list"]; ok {
		return parseUserDataList(listRaw, uid)
	}
	itemID, _ := firstString(m, "item_id", "ItemId", "ItemID")
	played, _ := firstBool(m, "played", "Played")
	if strings.TrimSpace(itemID) == "" {
		return nil
	}
	return []userDataEventEntry{{userID: uid, itemID: strings.TrimSpace(itemID), played: played}}
}

func parseUserDataList(raw any, userID string) []userDataEventEntry {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]userDataEventEntry, 0, len(items))
	for _, it := range items {
		em, ok := it.(map[string]any)
		if !ok {
			continue
		}
		itemID, _ := firstString(em, "item_id", "ItemId", "ItemID")
		played, _ := firstBool(em, "played", "Played")
		if strings.TrimSpace(itemID) != "" {
			out = append(out, userDataEventEntry{userID: userID, itemID: strings.TrimSpace(itemID), played: played})
		}
	}
	return out
}

func firstString(m map[string]any, keys ...string) (string, bool) {
	for _, k := range keys {
		if v, ok := m[k].(string); ok {
			return v, true
		}
	}
	return "", false
}

func firstBool(m map[string]any, keys ...string) (bool, bool) {
	for _, k := range keys {
		if v, ok := m[k].(bool); ok {
			return v, true
		}
	}
	return false, false
}

func firstInt(m map[string]any, keys ...string) (int, bool) {
	for _, k := range keys {
		switch v := m[k].(type) {
		case int:
			return v, true
		case int64:
			return int(v), true
		case float64:
			return int(v), true
		}
	}
	return 0, false
}

func firstInt64(m map[string]any, keys ...string) (int64, bool) {
	for _, k := range keys {
		switch v := m[k].(type) {
		case int64:
			return v, true
		case int:
			return int64(v), true
		case float64:
			return int64(v), true
		}
	}
	return 0, false
}
