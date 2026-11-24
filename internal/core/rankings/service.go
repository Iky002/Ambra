package rankings

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

type chartIndex struct {
	imdbIDs   map[string]struct{}
	doubanIDs map[string]struct{}
	tmdbIDs   map[string]struct{}
	titles    map[string][]int // normalized title -> slice of years (0 if unknown)
}

func buildChartIndex(entries []ChartEntry) *chartIndex {
	idx := &chartIndex{
		imdbIDs:   make(map[string]struct{}, len(entries)),
		doubanIDs: make(map[string]struct{}, len(entries)),
		tmdbIDs:   make(map[string]struct{}, len(entries)),
		titles:    make(map[string][]int, len(entries)*2),
	}
	for _, e := range entries {
		if id := normalizeIMDbID(e.IMDbID); id != "" {
			idx.imdbIDs[id] = struct{}{}
		}
		if id := strings.TrimSpace(e.DoubanID); id != "" {
			idx.doubanIDs[id] = struct{}{}
		}
		if id := strings.TrimSpace(e.TMDBID); id != "" {
			idx.tmdbIDs[id] = struct{}{}
		}
		if norm := NormalizeTitle(e.Title); norm != "" {
			idx.titles[norm] = append(idx.titles[norm], e.Year)
		}
		if norm := NormalizeTitle(e.OriginalTitle); norm != "" {
			idx.titles[norm] = append(idx.titles[norm], e.Year)
		}
	}
	return idx
}

func (idx *chartIndex) match(name, originalTitle string, year int, imdbID, doubanID, tmdbID string) bool {
	if idx == nil {
		return false
	}
	if imdbID != "" {
		if _, ok := idx.imdbIDs[imdbID]; ok {
			return true
		}
	}
	if doubanID != "" {
		if _, ok := idx.doubanIDs[doubanID]; ok {
			return true
		}
	}
	if tmdbID != "" {
		if _, ok := idx.tmdbIDs[tmdbID]; ok {
			return true
		}
	}
	if matchNormalizedTitle(idx.titles, NormalizeTitle(name), year) {
		return true
	}
	if matchNormalizedTitle(idx.titles, NormalizeTitle(originalTitle), year) {
		return true
	}
	return false
}

func matchNormalizedTitle(titles map[string][]int, norm string, year int) bool {
	if norm == "" {
		return false
	}
	years, ok := titles[norm]
	if !ok {
		return false
	}
	for _, ey := range years {
		if year == 0 || ey == 0 {
			return true
		}
		diff := year - ey
		if diff >= -1 && diff <= 1 {
			return true
		}
	}
	return false
}

// NormalizeTitle removes punctuation and whitespace and lowercases the title.
func NormalizeTitle(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Service manages ranking chart syncing, SQLite caching, and idempotent library auto-tagging.
type Service struct {
	store   *sqlite.Store
	bus     *eventbus.Bus
	fetcher *Fetcher
	opts    Options

	mu              sync.RWMutex
	douban          *ChartSnapshot
	doubanIdx       *chartIndex
	imdb            *ChartSnapshot
	imdbIdx         *chartIndex
	tmdbTrending    *ChartSnapshot
	tmdbTrendingIdx *chartIndex

	syncMu  sync.Mutex
	started atomic.Bool
}

// New creates a new rankings Service.
func New(store *sqlite.Store, bus *eventbus.Bus, opts Options) *Service {
	if opts.SyncInterval <= 0 {
		opts.SyncInterval = DefaultSyncInterval
	}
	if opts.InitialDelay <= 0 {
		opts.InitialDelay = DefaultInitialDelay
	}
	return &Service{
		store:   store,
		bus:     bus,
		fetcher: NewFetcher(opts),
		opts:    opts,
	}
}

// LoadCache loads persisted chart snapshots from SQLite server_config into memory.
func (s *Service) LoadCache(ctx context.Context) error {
	if s.store == nil {
		return nil
	}
	doubanSnap, err := s.loadSnapshot(ctx, ConfigKeyDoubanTop250)
	if err != nil {
		return err
	}
	imdbSnap, err := s.loadSnapshot(ctx, ConfigKeyIMDbTop250)
	if err != nil {
		return err
	}
	tmdbSnap, err := s.loadSnapshot(ctx, ConfigKeyTMDBTrending)
	if err != nil {
		return err
	}

	s.mu.Lock()
	if doubanSnap != nil {
		s.douban = doubanSnap
		s.doubanIdx = buildChartIndex(doubanSnap.Entries)
	}
	if imdbSnap != nil {
		s.imdb = imdbSnap
		s.imdbIdx = buildChartIndex(imdbSnap.Entries)
	}
	if tmdbSnap != nil {
		s.tmdbTrending = tmdbSnap
		s.tmdbTrendingIdx = buildChartIndex(tmdbSnap.Entries)
	}
	s.mu.Unlock()
	return nil
}

func (s *Service) loadSnapshot(ctx context.Context, key string) (*ChartSnapshot, error) {
	var raw string
	err := s.store.Reader.QueryRowContext(ctx, `SELECT value_json FROM server_config WHERE key = ?`, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", key, err)
	}
	var snap ChartSnapshot
	if err := json.Unmarshal([]byte(raw), &snap); err != nil {
		return nil, fmt.Errorf("unmarshal %s: %w", key, err)
	}
	snap.Count = len(snap.Entries)
	return &snap, nil
}

func (s *Service) saveSnapshot(ctx context.Context, key string, snap *ChartSnapshot) error {
	if s.store == nil || snap == nil {
		return nil
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	_, err = s.store.Writer.ExecContext(ctx, `
		INSERT INTO server_config (key, value_json, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			value_json = excluded.value_json,
			updated_at = excluded.updated_at`,
		key, string(raw), time.Now().Unix(),
	)
	return err
}

// Status returns the current cached Douban Top 250, IMDb Top 250, and TMDB Trending snapshots.
func (s *Service) Status(includeEntries bool) StatusResponse {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return StatusResponse{
		DoubanTop250: copySnapshot(ChartDoubanTop250, s.douban, includeEntries),
		IMDbTop250:   copySnapshot(ChartIMDbTop250, s.imdb, includeEntries),
		TMDBTrending: copySnapshot(ChartTMDBTrending, s.tmdbTrending, includeEntries),
	}
}

// TrendingEntries returns the cached TMDB Trending entries and metadata status.
func (s *Service) TrendingEntries(_ context.Context) ([]ChartEntry, ChartSnapshot) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	status := copySnapshot(ChartTMDBTrending, s.tmdbTrending, false)
	if s.tmdbTrending == nil || len(s.tmdbTrending.Entries) == 0 {
		return []ChartEntry{}, status
	}
	entries := append([]ChartEntry(nil), s.tmdbTrending.Entries...)
	return entries, status
}

func copySnapshot(chart string, snap *ChartSnapshot, includeEntries bool) ChartSnapshot {
	if snap == nil {
		return ChartSnapshot{Chart: chart}
	}
	out := ChartSnapshot{
		Chart:     snap.Chart,
		Source:    snap.Source,
		UpdatedAt: snap.UpdatedAt,
		Count:     len(snap.Entries),
	}
	if out.Chart == "" {
		out.Chart = chart
	}
	if includeEntries && len(snap.Entries) > 0 {
		out.Entries = append([]ChartEntry(nil), snap.Entries...)
	}
	return out
}

// MatchTagsForItem returns the ranking tags that match the given item metadata.
func (s *Service) MatchTagsForItem(name, originalTitle string, year int, providerIDs map[string]string) []string {
	imdbID, doubanID, tmdbID := extractProviderIDs(providerIDs)

	s.mu.RLock()
	dIdx := s.doubanIdx
	iIdx := s.imdbIdx
	tIdx := s.tmdbTrendingIdx
	s.mu.RUnlock()

	var tags []string
	if dIdx != nil && dIdx.match(name, originalTitle, year, imdbID, doubanID, tmdbID) {
		tags = append(tags, TagDoubanTop250)
	}
	if iIdx != nil && iIdx.match(name, originalTitle, year, imdbID, doubanID, tmdbID) {
		tags = append(tags, TagIMDbTop250)
	}
	if tIdx != nil && tIdx.match(name, originalTitle, year, imdbID, doubanID, tmdbID) {
		tags = append(tags, TagTMDBTrending)
	}
	return tags
}

func extractProviderIDs(providerIDs map[string]string) (imdbID, doubanID, tmdbID string) {
	for k, v := range providerIDs {
		val := strings.TrimSpace(v)
		if val == "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(k)) {
		case "imdb":
			imdbID = normalizeIMDbID(val)
		case "douban":
			doubanID = val
		case "tmdb":
			tmdbID = val
		}
	}
	return imdbID, doubanID, tmdbID
}

// ApplyTagsToLibrary scans all Movie and Series items in SQLite and non-destructively
// appends matching ranking tags while preserving all existing user tags.
func (s *Service) ApplyTagsToLibrary(ctx context.Context) (matchedItems int, tagsAdded int, err error) {
	if s.store == nil {
		return 0, 0, nil
	}

	type itemTagPlan struct {
		itemID string
		tags   []string
	}

	rows, err := s.store.Reader.QueryContext(ctx, `
		SELECT id, name, COALESCE(original_title, ''), COALESCE(production_year, 0), COALESCE(extra_json, '{}')
		FROM items
		WHERE type IN ('Movie', 'Series')`)
	if err != nil {
		return 0, 0, fmt.Errorf("query library items: %w", err)
	}

	var plans []itemTagPlan
	for rows.Next() {
		var id, name, originalTitle, extraJSON string
		var year int
		if err := rows.Scan(&id, &name, &originalTitle, &year, &extraJSON); err != nil {
			rows.Close()
			return 0, 0, fmt.Errorf("scan library item: %w", err)
		}
		var extra sqlite.ItemExtra
		if extraJSON != "" && extraJSON != "{}" {
			_ = json.Unmarshal([]byte(extraJSON), &extra)
		}
		tags := s.MatchTagsForItem(name, originalTitle, year, extra.ProviderIDs)
		if len(tags) > 0 {
			plans = append(plans, itemTagPlan{itemID: id, tags: tags})
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, 0, err
	}
	rows.Close()

	matchedItems = len(plans)
	if matchedItems == 0 {
		return 0, 0, nil
	}

	tx, err := s.store.Writer.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("begin tag tx: %w", err)
	}
	defer tx.Rollback()

	taxIDCache := make(map[string]string, 2)
	ensureTaxID := func(tagName string) (string, error) {
		if id, ok := taxIDCache[tagName]; ok {
			return id, nil
		}
		newID := guid.New()
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO taxonomies (id, kind, name, sort_name) VALUES (?, 'tag', ?, ?)`,
			newID, tagName, strings.ToLower(tagName),
		); err != nil {
			return "", err
		}
		var taxID string
		if err := tx.QueryRowContext(ctx,
			`SELECT id FROM taxonomies WHERE kind = 'tag' AND name = ?`, tagName,
		).Scan(&taxID); err != nil {
			return "", err
		}
		taxIDCache[tagName] = taxID
		return taxID, nil
	}

	for _, p := range plans {
		for _, tag := range p.tags {
			taxID, err := ensureTaxID(tag)
			if err != nil {
				return 0, 0, fmt.Errorf("ensure tag %q: %w", tag, err)
			}
			res, err := tx.ExecContext(ctx,
				`INSERT OR IGNORE INTO item_taxonomies (item_id, taxonomy_id) VALUES (?, ?)`,
				p.itemID, taxID,
			)
			if err != nil {
				return 0, 0, fmt.Errorf("link tag %q to item %s: %w", tag, p.itemID, err)
			}
			if n, _ := res.RowsAffected(); n > 0 {
				tagsAdded += int(n)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("commit tag tx: %w", err)
	}

	if tagsAdded > 0 && s.bus != nil {
		s.bus.Publish(ctx, eventbus.Event{
			Type: eventbus.EventLibraryChanged,
			Payload: map[string]any{
				"Source":       "rankings",
				"MatchedItems": matchedItems,
				"TagsAdded":    tagsAdded,
			},
		})
	}

	return matchedItems, tagsAdded, nil
}

// SyncAndApply fetches Douban Top 250, IMDb Top 250, and TMDB Trending sequentially
// (preserving existing caches on individual fetch failures), persists updated snapshots,
// and applies matching tags to the library.
func (s *Service) SyncAndApply(ctx context.Context) (SyncSummary, error) {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	summary := SyncSummary{SyncedAt: time.Now().UTC()}

	var doubanErr, imdbErr, tmdbErr error
	tmdbLiveSucceeded := false

	if snap, err := s.fetcher.FetchDoubanTop250(ctx); err != nil {
		doubanErr = err
		summary.DoubanError = err.Error()
	} else {
		if err := s.saveSnapshot(ctx, ConfigKeyDoubanTop250, snap); err != nil {
			return summary, fmt.Errorf("save douban cache: %w", err)
		}
		s.mu.Lock()
		s.douban = snap
		s.doubanIdx = buildChartIndex(snap.Entries)
		s.mu.Unlock()
	}

	if snap, err := s.fetcher.FetchIMDbTop250(ctx); err != nil {
		imdbErr = err
		summary.IMDbError = err.Error()
	} else {
		if err := s.saveSnapshot(ctx, ConfigKeyIMDbTop250, snap); err != nil {
			return summary, fmt.Errorf("save imdb cache: %w", err)
		}
		s.mu.Lock()
		s.imdb = snap
		s.imdbIdx = buildChartIndex(snap.Entries)
		s.mu.Unlock()
	}

	if snap, err := s.fetcher.FetchTMDBTrending(ctx); err != nil {
		tmdbErr = err
		summary.TMDBTrendingError = err.Error()
	} else {
		if err := s.saveSnapshot(ctx, ConfigKeyTMDBTrending, snap); err != nil {
			return summary, fmt.Errorf("save tmdb trending cache: %w", err)
		}
		if snap.Source != SourceTMDBTrendingBuiltin {
			tmdbLiveSucceeded = true
		}
		s.mu.Lock()
		s.tmdbTrending = snap
		s.tmdbTrendingIdx = buildChartIndex(snap.Entries)
		s.mu.Unlock()
	}

	s.mu.RLock()
	if s.douban != nil {
		summary.DoubanCount = len(s.douban.Entries)
	}
	if s.imdb != nil {
		summary.IMDbCount = len(s.imdb.Entries)
	}
	if s.tmdbTrending != nil {
		summary.TMDBTrendingCount = len(s.tmdbTrending.Entries)
	}
	s.mu.RUnlock()

	matched, added, applyErr := s.ApplyTagsToLibrary(ctx)
	if applyErr != nil {
		return summary, applyErr
	}
	summary.MatchedItems = matched
	summary.TagsAdded = added

	if doubanErr != nil && imdbErr != nil && !tmdbLiveSucceeded {
		if tmdbErr != nil {
			return summary, fmt.Errorf("all chart fetches failed: douban=%v; imdb=%v; tmdb_trending=%v", doubanErr, imdbErr, tmdbErr)
		}
		return summary, fmt.Errorf("all chart fetches failed: douban=%v; imdb=%v", doubanErr, imdbErr)
	}
	return summary, nil
}

// StartPeriodicSync starts a background goroutine that refreshes rankings on
// the given interval (default 24h) and triggers an initial delayed sync when
// either cache is missing or stale. It exits cleanly when ctx is canceled.
func (s *Service) StartPeriodicSync(ctx context.Context, interval time.Duration) {
	if !s.started.CompareAndSwap(false, true) {
		return
	}
	if interval <= 0 {
		interval = s.opts.SyncInterval
	}
	if interval <= 0 {
		interval = DefaultSyncInterval
	}
	initialDelay := s.opts.InitialDelay

	go func() {
		defer s.started.Store(false)
		_ = s.LoadCache(ctx)

		if s.needsInitialSync(interval) {
			timer := time.NewTimer(initialDelay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
				_, _ = s.SyncAndApply(ctx)
			}
		}

		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _ = s.SyncAndApply(ctx)
			}
		}
	}()
}

func (s *Service) needsInitialSync(interval time.Duration) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	if s.douban == nil || len(s.douban.Entries) == 0 || now.Sub(s.douban.UpdatedAt) >= interval {
		return true
	}
	if s.imdb == nil || len(s.imdb.Entries) == 0 || now.Sub(s.imdb.UpdatedAt) >= interval {
		return true
	}
	if s.tmdbTrending == nil || len(s.tmdbTrending.Entries) == 0 || now.Sub(s.tmdbTrending.UpdatedAt) >= interval {
		return true
	}
	return false
}
