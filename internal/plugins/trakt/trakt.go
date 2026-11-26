package trakt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
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
	defaultUserDataSettleDelay = 50 * time.Millisecond
	defaultMinWriteInterval    = 1 * time.Second
	scrobbleCompletionPct      = 80.0
	traktAPIVersion            = "2"
	maxErrBodyPreview          = 1024
	maxDiscardRespBytes        = 1 << 20
	traktWorkerPoolSize        = 8
	traktWorkerQueueSize       = 512
)

var (
	_ plugins.Plugin                = (*Plugin)(nil)
	_ plugins.DefaultConfigExporter = (*Plugin)(nil)
)

type stopBarrier struct {
	ch   chan struct{}
	once sync.Once
}

func (b *stopBarrier) close() {
	if b == nil {
		return
	}
	b.once.Do(func() {
		close(b.ch)
	})
}

// Plugin implements a built-in Trakt.tv scrobble and watched-history sync notifier.
type Plugin struct {
	mu          sync.RWMutex
	enabled     bool
	userConfigs UserConfigStore
	priority    int
	baseURL     string
	client      *http.Client

	stateMu             sync.Mutex
	pausedAt            map[string]time.Time
	completedAt         map[string]time.Time
	stopInFlight        map[string]*stopBarrier
	pauseThrottleWindow time.Duration
	completeDedupWindow time.Duration
	nowFunc             func() time.Time

	writeRateMu      sync.Mutex
	lastWriteAt      time.Time
	minWriteInterval time.Duration
}

type configSnapshot struct {
	enabled          bool
	clientID         string
	accessToken      string
	scrobblePlayback bool
	syncWatched      bool
	priority         int
	baseURL          string
	client           *http.Client
}

type UserConfigStore interface {
	GetUserTraktConfig(context.Context, string) (*sqlite.TraktConfig, error)
}

func (p *Plugin) SetUserConfigStore(store UserConfigStore) {
	p.mu.Lock()
	p.userConfigs = store
	p.mu.Unlock()
}

// SetMinWriteInterval configures the minimum interval between outgoing Trakt write requests (~1 req/s by default).
func (p *Plugin) SetMinWriteInterval(d time.Duration) {
	p.writeRateMu.Lock()
	if d < 0 {
		d = 0
	}
	p.minWriteInterval = d
	p.writeRateMu.Unlock()
}

// New creates a new Trakt plugin instance (disabled by default).
func New() *Plugin {
	return &Plugin{
		enabled:             false,
		priority:            defaultPriority,
		baseURL:             defaultBaseURL,
		client:              netutil.NewSafeHTTPClient(defaultTimeout, false),
		pausedAt:            make(map[string]time.Time),
		completedAt:         make(map[string]time.Time),
		stopInFlight:        make(map[string]*stopBarrier),
		pauseThrottleWindow: defaultPauseThrottleWindow,
		completeDedupWindow: defaultCompleteDedupWindow,
		nowFunc:             time.Now,
		minWriteInterval:    defaultMinWriteInterval,
	}
}

// SetBaseURL overrides the Trakt API base URL (intended for tests).
func (p *Plugin) SetBaseURL(raw string) {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		return
	}
	p.mu.Lock()
	p.baseURL = trimmed
	p.mu.Unlock()
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
		enabled:  p.enabled,
		priority: p.priority,
		baseURL:  p.baseURL,
		client:   p.client,
	}
}

func (p *Plugin) snapshotForUser(ctx context.Context, userID string) (configSnapshot, error) {
	snap := p.snapshot()
	p.mu.RLock()
	store := p.userConfigs
	p.mu.RUnlock()
	if !snap.enabled || store == nil || strings.TrimSpace(userID) == "" {
		return snap, nil
	}
	cfg, err := store.GetUserTraktConfig(ctx, userID)
	if err != nil || cfg == nil {
		return snap, err
	}
	snap.clientID, snap.accessToken = cfg.ClientID, cfg.AccessToken
	snap.scrobblePlayback, snap.syncWatched = cfg.ScrobblePlayback, cfg.SyncWatched
	return snap, nil
}

func (p *Plugin) Manifest() plugins.Manifest {
	snap := p.snapshot()
	return plugins.Manifest{
		ID:          PluginID,
		Name:        "Trakt Scrobble & Sync",
		Version:     "1.0.0",
		Description: "Allows each user to bind their own Trakt.tv account and sync playback and watched history (disabled by default)",
		Author:      "Ambra",
		BuiltIn:     true,
		Enabled:     snap.enabled,
		Priority:    snap.priority,
		Kinds:       []plugins.Kind{plugins.KindNotifier},
	}
}

func (p *Plugin) DefaultConfig() map[string]any {
	snap := p.snapshot()
	return map[string]any{
		"enabled":  snap.enabled,
		"priority": snap.priority,
	}
}

// Configure updates plugin configuration in a thread-safe manner.
// Runtime modification of base_url is intentionally disallowed so user OAuth
// tokens can never be redirected to an arbitrary host.
func (p *Plugin) Configure(cfg map[string]any) error {
	if cfg == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	if v, ok := firstBool(cfg, "enabled", "Enabled"); ok {
		p.enabled = v
	}
	if v, ok := firstInt(cfg, "priority", "Priority"); ok {
		p.priority = v
	}
	return nil
}

// Only availability and priority are server-wide. Legacy global account settings
// are discarded, never copied to an arbitrary user's account.
func (p *Plugin) FilterServerConfig(cfg map[string]any) map[string]any {
	out := make(map[string]any)
	if v, ok := firstBool(cfg, "enabled", "Enabled"); ok {
		out["enabled"] = v
	}
	if v, ok := firstInt(cfg, "priority", "Priority"); ok {
		out["priority"] = v
	}
	return out
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

type itemWithSeries struct {
	item   *sqlite.ItemRecord
	series *sqlite.ItemRecord
}

func (p *Plugin) throttleWrite(ctx context.Context, stopCh <-chan struct{}) error {
	p.writeRateMu.Lock()
	interval := p.minWriteInterval
	if interval <= 0 {
		p.writeRateMu.Unlock()
		return nil
	}
	now := time.Now()
	target := now
	if !p.lastWriteAt.IsZero() {
		nextAllowed := p.lastWriteAt.Add(interval)
		if nextAllowed.After(now) {
			target = nextAllowed
		}
	}
	wait := target.Sub(now)
	p.lastWriteAt = target
	p.writeRateMu.Unlock()

	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	if stopCh != nil {
		select {
		case <-timer.C:
			return nil
		case <-stopCh:
			return context.Canceled
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ScrobbleStart sends a POST /scrobble/start request to Trakt.
func (p *Plugin) ScrobbleStart(ctx context.Context, userID string, item *sqlite.ItemRecord, series *sqlite.ItemRecord, progressPct float64) error {
	return p.scrobble(ctx, userID, "/scrobble/start", item, series, progressPct)
}

// ScrobblePause sends a POST /scrobble/pause request to Trakt.
func (p *Plugin) ScrobblePause(ctx context.Context, userID string, item *sqlite.ItemRecord, series *sqlite.ItemRecord, progressPct float64) error {
	return p.scrobble(ctx, userID, "/scrobble/pause", item, series, progressPct)
}

// ScrobbleStop sends a POST /scrobble/stop request to Trakt.
func (p *Plugin) ScrobbleStop(ctx context.Context, userID string, item *sqlite.ItemRecord, series *sqlite.ItemRecord, progressPct float64) error {
	return p.scrobble(ctx, userID, "/scrobble/stop", item, series, progressPct)
}

func (p *Plugin) scrobble(ctx context.Context, userID, endpoint string, item *sqlite.ItemRecord, series *sqlite.ItemRecord, progressPct float64) error {
	snap, err := p.snapshotForUser(ctx, userID)
	if err != nil {
		return err
	}
	return p.scrobbleWithSnapshot(ctx, nil, snap, endpoint, item, series, progressPct)
}

func (p *Plugin) scrobbleWithSnapshot(ctx context.Context, stopCh <-chan struct{}, snap configSnapshot, endpoint string, item *sqlite.ItemRecord, series *sqlite.ItemRecord, progressPct float64) error {
	if !snap.enabled || !snap.scrobblePlayback || snap.clientID == "" || snap.accessToken == "" {
		return nil
	}
	payload := buildScrobblePayload(item, series, progressPct)
	if payload == nil {
		return nil
	}
	if err := p.throttleWrite(ctx, stopCh); err != nil {
		return err
	}
	return postTrakt(ctx, snap, endpoint, payload)
}

// SyncWatched sends a POST /sync/history request to Trakt.
func (p *Plugin) SyncWatched(ctx context.Context, userID string, item *sqlite.ItemRecord, series *sqlite.ItemRecord) error {
	snap, err := p.snapshotForUser(ctx, userID)
	if err != nil {
		return err
	}
	return p.syncWatchedWithSnapshot(ctx, nil, snap, item, series)
}

func (p *Plugin) syncWatchedWithSnapshot(ctx context.Context, stopCh <-chan struct{}, snap configSnapshot, item *sqlite.ItemRecord, series *sqlite.ItemRecord) error {
	if !snap.enabled || !snap.syncWatched || snap.clientID == "" || snap.accessToken == "" {
		return nil
	}
	payload := buildSyncHistoryPayload(item, series)
	if payload == nil {
		return nil
	}
	if err := p.throttleWrite(ctx, stopCh); err != nil {
		return err
	}
	return postTrakt(ctx, snap, "/sync/history", payload)
}

func (p *Plugin) syncWatchedBatchWithSnapshot(ctx context.Context, stopCh <-chan struct{}, snap configSnapshot, batch []itemWithSeries) error {
	if !snap.enabled || !snap.syncWatched || snap.clientID == "" || snap.accessToken == "" {
		return nil
	}
	payload := buildBatchSyncHistoryPayload(batch)
	if payload == nil {
		return nil
	}
	if err := p.throttleWrite(ctx, stopCh); err != nil {
		return err
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
	return buildBatchSyncHistoryPayload([]itemWithSeries{{item: item, series: series}})
}

func showGroupingKey(item *sqlite.ItemRecord, series *sqlite.ItemRecord, show traktShow) string {
	if series != nil && series.ID != "" {
		return "id:" + series.ID
	}
	if item != nil && item.SeriesID != "" {
		return "id:" + item.SeriesID
	}
	if show.IDs != nil {
		if show.IDs.Tmdb > 0 {
			return fmt.Sprintf("tmdb:%d", show.IDs.Tmdb)
		}
		if show.IDs.Tvdb > 0 {
			return fmt.Sprintf("tvdb:%d", show.IDs.Tvdb)
		}
		if show.IDs.Imdb != "" {
			return "imdb:" + strings.ToLower(show.IDs.Imdb)
		}
	}
	return fmt.Sprintf("title:%s:%d", strings.ToLower(show.Title), show.Year)
}

func buildBatchSyncHistoryPayload(batch []itemWithSeries) *syncHistoryPayload {
	if len(batch) == 0 {
		return nil
	}
	var movies []traktMovie
	type seasonBuilder struct {
		number  int
		epSeen  map[int]struct{}
		epList  []historyEpisode
	}
	type showBuilder struct {
		show        traktShow
		seasonIndex map[int]int
		seasons     []*seasonBuilder
	}
	var showList []*showBuilder
	showIdxByKey := make(map[string]int)

	for _, pair := range batch {
		item := pair.item
		if item == nil {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(item.Type)) {
		case "movie":
			movies = append(movies, buildTraktMovie(item))
		case "episode":
			show := buildTraktShow(item, pair.series)
			key := showGroupingKey(item, pair.series, show)
			sIdx, exists := showIdxByKey[key]
			if !exists {
				sIdx = len(showList)
				showIdxByKey[key] = sIdx
				showList = append(showList, &showBuilder{
					show:        show,
					seasonIndex: make(map[int]int),
				})
			}
			sb := showList[sIdx]
			seasonNum := 0
			if item.ParentIndexNumber != nil {
				seasonNum = *item.ParentIndexNumber
			}
			epNum := 0
			if item.IndexNumber != nil {
				epNum = *item.IndexNumber
			}
			seaIdx, sExists := sb.seasonIndex[seasonNum]
			if !sExists {
				seaIdx = len(sb.seasons)
				sb.seasonIndex[seasonNum] = seaIdx
				sb.seasons = append(sb.seasons, &seasonBuilder{
					number: seasonNum,
					epSeen: make(map[int]struct{}),
				})
			}
			seab := sb.seasons[seaIdx]
			if _, dup := seab.epSeen[epNum]; !dup {
				seab.epSeen[epNum] = struct{}{}
				seab.epList = append(seab.epList, historyEpisode{Number: epNum})
			}
		}
	}

	var shows []historyShow
	for _, sb := range showList {
		seasons := make([]historySeason, 0, len(sb.seasons))
		for _, seab := range sb.seasons {
			seasons = append(seasons, historySeason{
				Number:   seab.number,
				Episodes: seab.epList,
			})
		}
		shows = append(shows, historyShow{
			Title:   sb.show.Title,
			Year:    sb.show.Year,
			IDs:     sb.show.IDs,
			Seasons: seasons,
		})
	}

	if len(movies) == 0 && len(shows) == 0 {
		return nil
	}
	return &syncHistoryPayload{
		Movies: movies,
		Shows:  shows,
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
	snap, err := p.snapshotForUser(context.Background(), userID)
	return err == nil && snap.enabled && snap.clientID != "" && snap.accessToken != ""
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

func (p *Plugin) beginInFlightStop(userID, itemID string) func() {
	key := userItemKey(userID, itemID)
	b := &stopBarrier{ch: make(chan struct{})}
	p.stateMu.Lock()
	if p.stopInFlight == nil {
		p.stopInFlight = make(map[string]*stopBarrier)
	}
	prev := p.stopInFlight[key]
	p.stopInFlight[key] = b
	p.stateMu.Unlock()
	if prev != nil {
		prev.close()
	}
	return func() {
		p.stateMu.Lock()
		if p.stopInFlight[key] == b {
			delete(p.stopInFlight, key)
		}
		p.stateMu.Unlock()
		b.close()
	}
}

func (p *Plugin) closeAllInFlightStops() {
	p.stateMu.Lock()
	barriers := make([]*stopBarrier, 0, len(p.stopInFlight))
	for k, b := range p.stopInFlight {
		barriers = append(barriers, b)
		delete(p.stopInFlight, k)
	}
	p.stateMu.Unlock()
	for _, b := range barriers {
		b.close()
	}
}

func (p *Plugin) waitInFlightStop(ctx context.Context, userID, itemID string) {
	key := userItemKey(userID, itemID)
	p.stateMu.Lock()
	b := p.stopInFlight[key]
	p.stateMu.Unlock()
	if b == nil {
		return
	}
	select {
	case <-b.ch:
	case <-ctx.Done():
	}
}

type traktTask struct {
	userID  string
	action  string
	itemIDs []string
	run     func()
}

// SubscribeEvents registers EventBus handlers for playback scrobbling and watched-history sync.
// Calling the returned function stops the plugin from processing subsequent events.
func (p *Plugin) SubscribeEvents(bus *eventbus.Bus, repo *sqlite.LibraryRepo) func() {
	if bus == nil {
		return func() {}
	}
	var unsubscribed atomic.Bool
	stopCh := make(chan struct{})
	jobs := make(chan traktTask, traktWorkerQueueSize)
	var stopOnce sync.Once

	for i := 0; i < traktWorkerPoolSize; i++ {
		go func() {
			for {
				select {
				case <-stopCh:
					return
				case task := <-jobs:
					if task.run != nil {
						task.run()
					}
				}
			}
		}()
	}

	enqueueTask := func(t traktTask) bool {
		if unsubscribed.Load() {
			return false
		}
		select {
		case jobs <- t:
			return true
		default:
			// Drop when queue is saturated rather than spawning unbounded goroutines.
			return false
		}
	}

	isReady := func() bool {
		if unsubscribed.Load() || repo == nil {
			return false
		}
		snap := p.snapshot()
		return snap.enabled
	}

	bus.Subscribe(eventbus.EventPlaybackStart, func(_ context.Context, e eventbus.Event) {
		if !isReady() {
			return
		}
		ev, ok := parsePlaybackEvent(e.Payload)
		if !ok || ev.itemID == "" || ev.userID == "" {
			return
		}
		enqueueTask(traktTask{
			userID:  ev.userID,
			action:  "scrobble_start",
			itemIDs: []string{ev.itemID},
			run: func() {
				ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout+5*time.Second)
				defer cancel()
				snap, err := p.snapshotForUser(ctx, ev.userID)
				if err != nil || !snap.enabled || !snap.scrobblePlayback || snap.clientID == "" || snap.accessToken == "" {
					return
				}
				p.clearPaused(ev.userID, ev.itemID)
				item, series := lookupItemAndSeries(ctx, repo, ev.itemID)
				if item == nil {
					return
				}
				pct := computeProgress(ev.positionTicks, ev.runtimeTicks, item.RuntimeTicks)
				_ = p.scrobbleWithSnapshot(ctx, stopCh, snap, "/scrobble/start", item, series, pct)
			},
		})
	})

	bus.Subscribe(eventbus.EventPlaybackProgress, func(_ context.Context, e eventbus.Event) {
		if !isReady() {
			return
		}
		ev, ok := parsePlaybackEvent(e.Payload)
		if !ok || ev.itemID == "" || ev.userID == "" {
			return
		}
		if !ev.isPaused {
			p.clearPaused(ev.userID, ev.itemID)
			return
		}
		enqueueTask(traktTask{
			userID:  ev.userID,
			action:  "scrobble_pause",
			itemIDs: []string{ev.itemID},
			run: func() {
				ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout+5*time.Second)
				defer cancel()
				snap, err := p.snapshotForUser(ctx, ev.userID)
				if err != nil || !snap.enabled || !snap.scrobblePlayback || snap.clientID == "" || snap.accessToken == "" {
					return
				}
				if !p.allowPauseScrobble(ev.userID, ev.itemID) {
					return
				}
				item, series := lookupItemAndSeries(ctx, repo, ev.itemID)
				if item == nil {
					return
				}
				pct := computeProgress(ev.positionTicks, ev.runtimeTicks, item.RuntimeTicks)
				_ = p.scrobbleWithSnapshot(ctx, stopCh, snap, "/scrobble/pause", item, series, pct)
			},
		})
	})

	bus.Subscribe(eventbus.EventPlaybackStop, func(_ context.Context, e eventbus.Event) {
		if !isReady() {
			return
		}
		ev, ok := parsePlaybackEvent(e.Payload)
		if !ok || ev.itemID == "" || ev.userID == "" {
			return
		}
		finishStop := p.beginInFlightStop(ev.userID, ev.itemID)
		if !enqueueTask(traktTask{
			userID:  ev.userID,
			action:  "scrobble_stop",
			itemIDs: []string{ev.itemID},
			run: func() {
				defer finishStop()
				ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout+5*time.Second)
				defer cancel()
				snap, err := p.snapshotForUser(ctx, ev.userID)
				if err != nil || !snap.enabled || !snap.scrobblePlayback || snap.clientID == "" || snap.accessToken == "" {
					return
				}
				p.clearPaused(ev.userID, ev.itemID)
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
				if err := p.scrobbleWithSnapshot(ctx, stopCh, snap, "/scrobble/stop", item, series, pct); err == nil {
					if ev.played || pct >= scrobbleCompletionPct {
						p.markScrobbleCompleted(ev.userID, ev.itemID)
					}
				}
			},
		}) {
			finishStop()
		}
	})

	bus.Subscribe(eventbus.EventUserDataChanged, func(_ context.Context, e eventbus.Event) {
		if !isReady() {
			return
		}
		entries := parseUserDataChangedEvent(e.Payload)
		if len(entries) == 0 {
			return
		}

		// Aggregate bulk watched entries by (UserID, Action) into a single batch traktTask
		// so marking an entire season/series (e.g. 200+ episodes) watched sends ONE batched
		// /sync/history request rather than overflowing the worker queue or triggering 429s.
		type batchKey struct {
			userID string
			action string
		}
		type batchGroup struct {
			userID  string
			action  string
			seen    map[string]struct{}
			itemIDs []string
		}
		var groups []*batchGroup
		groupIndex := make(map[batchKey]int)

		for _, entry := range entries {
			if !entry.played || entry.itemID == "" || entry.userID == "" {
				continue
			}
			if p.wasRecentlyScrobbleCompleted(entry.userID, entry.itemID) {
				continue
			}
			bk := batchKey{userID: entry.userID, action: "sync_watched"}
			idx, exists := groupIndex[bk]
			if !exists {
				idx = len(groups)
				groupIndex[bk] = idx
				groups = append(groups, &batchGroup{
					userID: entry.userID,
					action: "sync_watched",
					seen:   make(map[string]struct{}),
				})
			}
			g := groups[idx]
			if _, dup := g.seen[entry.itemID]; !dup {
				g.seen[entry.itemID] = struct{}{}
				g.itemIDs = append(g.itemIDs, entry.itemID)
			}
		}

		for _, g := range groups {
			userID := g.userID
			action := g.action
			itemIDs := g.itemIDs
			enqueueTask(traktTask{
				userID:  userID,
				action:  action,
				itemIDs: itemIDs,
				run: func() {
					ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout+15*time.Second)
					defer cancel()
					// Briefly settle so a concurrent EventPlaybackStop handler can register its
					// in-flight stop marker or completion timestamp before we check dedup.
					timer := time.NewTimer(defaultUserDataSettleDelay)
					select {
					case <-timer.C:
					case <-stopCh:
						timer.Stop()
						return
					case <-ctx.Done():
						timer.Stop()
						return
					}

					var activeIDs []string
					for _, id := range itemIDs {
						p.waitInFlightStop(ctx, userID, id)
						if unsubscribed.Load() {
							return
						}
						if !p.wasRecentlyScrobbleCompleted(userID, id) {
							activeIDs = append(activeIDs, id)
						}
					}
					if len(activeIDs) == 0 {
						return
					}

					snap, err := p.snapshotForUser(ctx, userID)
					if err != nil || !snap.enabled || !snap.syncWatched || snap.clientID == "" || snap.accessToken == "" {
						return
					}

					seriesCache := make(map[string]*sqlite.ItemRecord)
					pairs := make([]itemWithSeries, 0, len(activeIDs))
					for _, id := range activeIDs {
						item, err := repo.GetItemRecordByID(ctx, id)
						if err != nil || item == nil {
							continue
						}
						var series *sqlite.ItemRecord
						if strings.EqualFold(item.Type, "Episode") && item.SeriesID != "" {
							if cached, ok := seriesCache[item.SeriesID]; ok {
								series = cached
							} else if s, sErr := repo.GetItemRecordByID(ctx, item.SeriesID); sErr == nil && s != nil {
								series = s
								seriesCache[item.SeriesID] = s
							}
						}
						pairs = append(pairs, itemWithSeries{item: item, series: series})
					}
					if len(pairs) == 0 {
						return
					}
					_ = p.syncWatchedBatchWithSnapshot(ctx, stopCh, snap, pairs)
				},
			})
		}
	})

	return func() {
		unsubscribed.Store(true)
		stopOnce.Do(func() {
			close(stopCh)
		})
		p.closeAllInFlightStops()
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
		euid := userID
		if u, hasU := firstString(em, "user_id", "UserId", "UserID"); hasU && strings.TrimSpace(u) != "" {
			euid = strings.TrimSpace(u)
		}
		itemID, _ := firstString(em, "item_id", "ItemId", "ItemID")
		played, _ := firstBool(em, "played", "Played")
		if strings.TrimSpace(itemID) != "" {
			out = append(out, userDataEventEntry{userID: euid, itemID: strings.TrimSpace(itemID), played: played})
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
