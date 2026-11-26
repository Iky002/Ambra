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
	imdbIDs                       map[string]struct{}
	doubanIDs                     map[string]struct{}
	tmdbIDsAny                    map[string]struct{}
	tmdbIDsMovie                  map[string]struct{}
	tmdbIDsTV                     map[string]struct{}
	titlesAny                     map[string][]int // normalized title -> slice of years (0 if unknown)
	titlesMovie                   map[string][]int
	titlesTV                      map[string][]int
	requireRecentYearWhenDateless bool
}

func normalizeChartMediaType(mt string) string {
	switch strings.ToLower(strings.TrimSpace(mt)) {
	case "movie", "film":
		return "movie"
	case "tv", "series", "show":
		return "tv"
	default:
		return ""
	}
}

func buildChartIndex(entries []ChartEntry) *chartIndex {
	idx := &chartIndex{
		imdbIDs:      make(map[string]struct{}, len(entries)),
		doubanIDs:    make(map[string]struct{}, len(entries)),
		tmdbIDsAny:   make(map[string]struct{}),
		tmdbIDsMovie: make(map[string]struct{}),
		tmdbIDsTV:    make(map[string]struct{}),
		titlesAny:    make(map[string][]int),
		titlesMovie:  make(map[string][]int),
		titlesTV:     make(map[string][]int),
	}
	for _, e := range entries {
		if id := normalizeIMDbID(e.IMDbID); id != "" {
			idx.imdbIDs[id] = struct{}{}
		}
		if id := strings.TrimSpace(e.DoubanID); id != "" {
			idx.doubanIDs[id] = struct{}{}
		}
		ns := normalizeChartMediaType(e.MediaType)
		if id := strings.TrimSpace(e.TMDBID); id != "" {
			switch ns {
			case "movie":
				idx.tmdbIDsMovie[id] = struct{}{}
			case "tv":
				idx.tmdbIDsTV[id] = struct{}{}
			default:
				idx.tmdbIDsAny[id] = struct{}{}
			}
		}
		targetTitles := idx.titlesAny
		switch ns {
		case "movie":
			targetTitles = idx.titlesMovie
		case "tv":
			targetTitles = idx.titlesTV
		}
		if norm := NormalizeTitle(e.Title); norm != "" {
			targetTitles[norm] = append(targetTitles[norm], e.Year)
		}
		if norm := NormalizeTitle(e.OriginalTitle); norm != "" {
			targetTitles[norm] = append(targetTitles[norm], e.Year)
		}
	}
	return idx
}

func buildTrendingChartIndex(entries []ChartEntry) *chartIndex {
	idx := buildChartIndex(entries)
	idx.requireRecentYearWhenDateless = true
	return idx
}

func (idx *chartIndex) match(mediaNS, name, originalTitle string, year int, imdbID, doubanID, tmdbID string) bool {
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
		if idx.matchTMDB(mediaNS, tmdbID) {
			return true
		}
	}
	minYearWhenDateless := 0
	if idx.requireRecentYearWhenDateless {
		minYearWhenDateless = time.Now().Year() - 1
	}
	if idx.matchTitleWithMinYear(mediaNS, NormalizeTitle(name), year, minYearWhenDateless) {
		return true
	}
	if idx.matchTitleWithMinYear(mediaNS, NormalizeTitle(originalTitle), year, minYearWhenDateless) {
		return true
	}
	return false
}

func (idx *chartIndex) matchTMDB(mediaNS, tmdbID string) bool {
	if _, ok := idx.tmdbIDsAny[tmdbID]; ok {
		return true
	}
	switch mediaNS {
	case "movie":
		_, ok := idx.tmdbIDsMovie[tmdbID]
		return ok
	case "tv":
		_, ok := idx.tmdbIDsTV[tmdbID]
		return ok
	default:
		if _, ok := idx.tmdbIDsMovie[tmdbID]; ok {
			return true
		}
		_, ok := idx.tmdbIDsTV[tmdbID]
		return ok
	}
}

func (idx *chartIndex) matchTitle(mediaNS, norm string, year int) bool {
	return idx.matchTitleWithMinYear(mediaNS, norm, year, 0)
}

func (idx *chartIndex) matchTitleWithMinYear(mediaNS, norm string, year, minYearWhenDateless int) bool {
	if norm == "" {
		return false
	}
	if matchNormalizedTitleWithMinYear(idx.titlesAny, norm, year, minYearWhenDateless) {
		return true
	}
	switch mediaNS {
	case "movie":
		return matchNormalizedTitleWithMinYear(idx.titlesMovie, norm, year, minYearWhenDateless)
	case "tv":
		return matchNormalizedTitleWithMinYear(idx.titlesTV, norm, year, minYearWhenDateless)
	default:
		return matchNormalizedTitleWithMinYear(idx.titlesMovie, norm, year, minYearWhenDateless) ||
			matchNormalizedTitleWithMinYear(idx.titlesTV, norm, year, minYearWhenDateless)
	}
}

func matchNormalizedTitle(titles map[string][]int, norm string, year int) bool {
	return matchNormalizedTitleWithMinYear(titles, norm, year, 0)
}

func matchNormalizedTitleWithMinYear(titles map[string][]int, norm string, year, minYearWhenDateless int) bool {
	if norm == "" {
		return false
	}
	years, ok := titles[norm]
	if !ok {
		return false
	}
	for _, ey := range years {
		if ey == 0 {
			if minYearWhenDateless > 0 && year < minYearWhenDateless {
				continue
			}
			return true
		}
		if year == 0 {
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

	mu                     sync.RWMutex
	douban                 *ChartSnapshot
	doubanIdx              *chartIndex
	imdb                   *ChartSnapshot
	imdbIdx                *chartIndex
	tmdbTrending           *ChartSnapshot
	tmdbTrendingIdx        *chartIndex
	doubanTrendingMovie    *ChartSnapshot
	doubanTrendingMovieIdx *chartIndex
	doubanTrendingTV       *ChartSnapshot
	doubanTrendingTVIdx    *chartIndex

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
	doubanMovieSnap, err := s.loadSnapshot(ctx, ConfigKeyDoubanTrendingMovie)
	if err != nil {
		return err
	}
	doubanTVSnap, err := s.loadSnapshot(ctx, ConfigKeyDoubanTrendingTV)
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
	if doubanMovieSnap != nil {
		s.doubanTrendingMovie = doubanMovieSnap
		s.doubanTrendingMovieIdx = buildTrendingChartIndex(doubanMovieSnap.Entries)
	}
	if doubanTVSnap != nil {
		s.doubanTrendingTV = doubanTVSnap
		s.doubanTrendingTVIdx = buildTrendingChartIndex(doubanTVSnap.Entries)
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

// Status returns the current cached ranking chart snapshots.
func (s *Service) Status(includeEntries bool) StatusResponse {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return StatusResponse{
		DoubanTop250:        copySnapshot(ChartDoubanTop250, s.douban, includeEntries),
		IMDbTop250:          copySnapshot(ChartIMDbTop250, s.imdb, includeEntries),
		TMDBTrending:        copySnapshot(ChartTMDBTrending, s.tmdbTrending, includeEntries),
		DoubanTrendingMovie: copySnapshot(ChartDoubanTrendingMovie, s.doubanTrendingMovie, includeEntries),
		DoubanTrendingTV:    copySnapshot(ChartDoubanTrendingTV, s.doubanTrendingTV, includeEntries),
	}
}

// Trending returns the full TrendingResponse containing both legacy TMDB fields
// and grouped snapshots for TMDB, Douban Trending Movies, and Douban Trending TV.
func (s *Service) Trending(_ context.Context) TrendingResponse {
	s.mu.RLock()
	defer s.mu.RUnlock()

	status := copySnapshot(ChartTMDBTrending, s.tmdbTrending, false)
	entries := []ChartEntry{}
	if s.tmdbTrending != nil && len(s.tmdbTrending.Entries) > 0 {
		entries = append([]ChartEntry(nil), s.tmdbTrending.Entries...)
	}
	return TrendingResponse{
		Status:      status,
		Entries:     entries,
		TMDB:        copySnapshot(ChartTMDBTrending, s.tmdbTrending, true),
		DoubanMovie: copySnapshot(ChartDoubanTrendingMovie, s.doubanTrendingMovie, true),
		DoubanTV:    copySnapshot(ChartDoubanTrendingTV, s.doubanTrendingTV, true),
	}
}

// TrendingEntries returns the cached TMDB Trending entries and metadata status.
func (s *Service) TrendingEntries(ctx context.Context) ([]ChartEntry, ChartSnapshot) {
	resp := s.Trending(ctx)
	return resp.Entries, resp.Status
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

// MatchTagsForItem returns the ranking tags that match the given item metadata
// without restricting by media type.
func (s *Service) MatchTagsForItem(name, originalTitle string, year int, providerIDs map[string]string) []string {
	return s.MatchTagsForTypedItem("", name, originalTitle, year, providerIDs)
}

// MatchTagsForTypedItem returns the ranking tags that match the given item metadata,
// applying media-type filtering for Douban Trending Movies (Movie) and TV (Series).
func (s *Service) MatchTagsForTypedItem(itemType, name, originalTitle string, year int, providerIDs map[string]string) []string {
	imdbID, doubanID, tmdbID := extractProviderIDs(providerIDs)

	itemTypeNorm := strings.ToLower(strings.TrimSpace(itemType))
	mediaNS := normalizeChartMediaType(itemTypeNorm)
	allowMovie := itemTypeNorm == "" || mediaNS == "movie"
	allowTV := itemTypeNorm == "" || mediaNS == "tv"

	s.mu.RLock()
	dIdx := s.doubanIdx
	iIdx := s.imdbIdx
	tIdx := s.tmdbTrendingIdx
	dmIdx := s.doubanTrendingMovieIdx
	dtIdx := s.doubanTrendingTVIdx
	s.mu.RUnlock()

	var tags []string
	hasTrending := false
	addTrending := func() {
		if !hasTrending {
			hasTrending = true
			tags = append(tags, TagTMDBTrending)
		}
	}

	if allowMovie && dIdx != nil && dIdx.match(mediaNS, name, originalTitle, year, imdbID, doubanID, tmdbID) {
		tags = append(tags, TagDoubanTop250)
	}
	if allowMovie && iIdx != nil && iIdx.match(mediaNS, name, originalTitle, year, imdbID, doubanID, tmdbID) {
		tags = append(tags, TagIMDbTop250)
	}
	if tIdx != nil && tIdx.match(mediaNS, name, originalTitle, year, imdbID, doubanID, tmdbID) {
		addTrending()
		tags = append(tags, TagTMDBTrendingSpecific)
	}
	if allowMovie && dmIdx != nil && dmIdx.match(mediaNS, name, originalTitle, year, imdbID, doubanID, tmdbID) {
		addTrending()
		tags = append(tags, TagDoubanTrendingMovie)
	}
	if allowTV && dtIdx != nil && dtIdx.match(mediaNS, name, originalTitle, year, imdbID, doubanID, tmdbID) {
		addTrending()
		tags = append(tags, TagDoubanTrendingTV)
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

	s.mu.RLock()
	hasTMDBChart := s.tmdbTrending != nil && len(s.tmdbTrending.Entries) > 0
	hasDoubanMovieChart := s.doubanTrendingMovie != nil && len(s.doubanTrendingMovie.Entries) > 0
	hasDoubanTVChart := s.doubanTrendingTV != nil && len(s.doubanTrendingTV.Entries) > 0
	s.mu.RUnlock()

	rows, err := s.store.Reader.QueryContext(ctx, `
		SELECT id, type, name, COALESCE(original_title, ''), COALESCE(production_year, 0), COALESCE(extra_json, '{}')
		FROM items
		WHERE type IN ('Movie', 'Series')`)
	if err != nil {
		return 0, 0, fmt.Errorf("query library items: %w", err)
	}

	var plans []itemTagPlan
	lockedItemIDs := make(map[string]struct{})
	activeByDynamicTag := map[string]map[string]struct{}{
		TagTMDBTrending:         {},
		TagTMDBTrendingSpecific: {},
		TagDoubanTrendingMovie:  {},
		TagDoubanTrendingTV:     {},
	}
	isItemTagsLocked := func(extra *sqlite.ItemExtra) bool {
		if extra == nil {
			return false
		}
		if extra.LockData {
			return true
		}
		for _, f := range extra.LockedFields {
			if strings.EqualFold(strings.TrimSpace(f), "Tags") {
				return true
			}
		}
		return false
	}
	for rows.Next() {
		var id, itemType, name, originalTitle, extraJSON string
		var year int
		if err := rows.Scan(&id, &itemType, &name, &originalTitle, &year, &extraJSON); err != nil {
			rows.Close()
			return 0, 0, fmt.Errorf("scan library item: %w", err)
		}
		var extra sqlite.ItemExtra
		if extraJSON != "" && extraJSON != "{}" {
			_ = json.Unmarshal([]byte(extraJSON), &extra)
		}
		if isItemTagsLocked(&extra) {
			lockedItemIDs[id] = struct{}{}
			continue
		}
		tags := s.MatchTagsForTypedItem(itemType, name, originalTitle, year, extra.ProviderIDs)
		if len(tags) > 0 {
			plans = append(plans, itemTagPlan{itemID: id, tags: tags})
			for _, t := range tags {
				if set, isDyn := activeByDynamicTag[t]; isDyn {
					set[id] = struct{}{}
				}
			}
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, 0, err
	}
	rows.Close()

	matchedItems = len(plans)
	hasAnyDynamicChart := hasTMDBChart || hasDoubanMovieChart || hasDoubanTVChart
	if matchedItems == 0 && !hasAnyDynamicChart {
		return 0, 0, nil
	}

	tx, err := s.store.Writer.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("begin tag tx: %w", err)
	}
	defer tx.Rollback()

	taxIDCache := make(map[string]string, 6)
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

	// Prune stale dynamic trending tags from items that have dropped off the active charts.
	pruneStaleTag := func(tagName string) (int, error) {
		var taxID string
		err := tx.QueryRowContext(ctx, `SELECT id FROM taxonomies WHERE kind = 'tag' AND name = ?`, tagName).Scan(&taxID)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		if err != nil {
			return 0, err
		}
		curRows, err := tx.QueryContext(ctx, `SELECT item_id FROM item_taxonomies WHERE taxonomy_id = ?`, taxID)
		if err != nil {
			return 0, err
		}
		var toRemove []string
		activeSet := activeByDynamicTag[tagName]
		for curRows.Next() {
			var itemID string
			if err := curRows.Scan(&itemID); err != nil {
				curRows.Close()
				return 0, err
			}
			if _, locked := lockedItemIDs[itemID]; locked {
				continue
			}
			if _, stillOnChart := activeSet[itemID]; !stillOnChart {
				toRemove = append(toRemove, itemID)
			}
		}
		if err := curRows.Err(); err != nil {
			curRows.Close()
			return 0, err
		}
		curRows.Close()

		removed := 0
		for _, itemID := range toRemove {
			res, err := tx.ExecContext(ctx, `DELETE FROM item_taxonomies WHERE item_id = ? AND taxonomy_id = ?`, itemID, taxID)
			if err != nil {
				return removed, err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				removed += int(n)
			}
		}
		return removed, nil
	}

	tagsRemoved := 0
	if hasTMDBChart {
		if n, err := pruneStaleTag(TagTMDBTrendingSpecific); err != nil {
			return 0, 0, fmt.Errorf("prune stale %q: %w", TagTMDBTrendingSpecific, err)
		} else {
			tagsRemoved += n
		}
	}
	if hasDoubanMovieChart {
		if n, err := pruneStaleTag(TagDoubanTrendingMovie); err != nil {
			return 0, 0, fmt.Errorf("prune stale %q: %w", TagDoubanTrendingMovie, err)
		} else {
			tagsRemoved += n
		}
	}
	if hasDoubanTVChart {
		if n, err := pruneStaleTag(TagDoubanTrendingTV); err != nil {
			return 0, 0, fmt.Errorf("prune stale %q: %w", TagDoubanTrendingTV, err)
		} else {
			tagsRemoved += n
		}
	}
	if hasAnyDynamicChart {
		if n, err := pruneStaleTag(TagTMDBTrending); err != nil {
			return 0, 0, fmt.Errorf("prune stale %q: %w", TagTMDBTrending, err)
		} else {
			tagsRemoved += n
		}
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

	if (tagsAdded > 0 || tagsRemoved > 0) && s.bus != nil {
		s.bus.Publish(ctx, eventbus.Event{
			Type: eventbus.EventLibraryChanged,
			Payload: map[string]any{
				"Source":       "rankings",
				"MatchedItems": matchedItems,
				"TagsAdded":    tagsAdded,
				"TagsRemoved":  tagsRemoved,
			},
		})
	}

	return matchedItems, tagsAdded, nil
}

// SyncAndApply fetches Douban Top 250, IMDb Top 250, TMDB Trending, Douban Trending Movies,
// and Douban Trending TV sequentially (preserving existing caches on individual fetch failures),
// persists updated snapshots, and applies matching tags to the library.
func (s *Service) SyncAndApply(ctx context.Context) (SyncSummary, error) {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	summary := SyncSummary{SyncedAt: time.Now().UTC()}

	var doubanErr, imdbErr, tmdbErr, doubanMovieErr, doubanTVErr error
	imdbLiveSucceeded := false
	tmdbLiveSucceeded := false
	doubanMovieLiveSucceeded := false
	doubanTVLiveSucceeded := false

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
		if snap.Source != SourceIMDbTop250Builtin {
			imdbLiveSucceeded = true
		}
		s.mu.RLock()
		hasLiveIMDb := s.imdb != nil && len(s.imdb.Entries) > 0 && s.imdb.Source != SourceIMDbTop250Builtin
		s.mu.RUnlock()
		if imdbLiveSucceeded || !hasLiveIMDb {
			if err := s.saveSnapshot(ctx, ConfigKeyIMDbTop250, snap); err != nil {
				return summary, fmt.Errorf("save imdb cache: %w", err)
			}
			s.mu.Lock()
			s.imdb = snap
			s.imdbIdx = buildChartIndex(snap.Entries)
			s.mu.Unlock()
		}
	}

	if snap, err := s.fetcher.FetchTMDBTrending(ctx); err != nil {
		tmdbErr = err
		summary.TMDBTrendingError = err.Error()
	} else {
		if snap.Source != SourceTMDBTrendingBuiltin {
			tmdbLiveSucceeded = true
		}
		s.mu.RLock()
		hasLiveTMDB := s.tmdbTrending != nil && len(s.tmdbTrending.Entries) > 0 && s.tmdbTrending.Source != SourceTMDBTrendingBuiltin
		s.mu.RUnlock()
		if tmdbLiveSucceeded || !hasLiveTMDB {
			if err := s.saveSnapshot(ctx, ConfigKeyTMDBTrending, snap); err != nil {
				return summary, fmt.Errorf("save tmdb trending cache: %w", err)
			}
			s.mu.Lock()
			s.tmdbTrending = snap
			s.tmdbTrendingIdx = buildChartIndex(snap.Entries)
			s.mu.Unlock()
		}
	}

	if snap, err := s.fetcher.FetchDoubanTrendingMovie(ctx); err != nil {
		doubanMovieErr = err
		summary.DoubanTrendingMovieError = err.Error()
	} else {
		if snap.Source != SourceDoubanTrendingMovieBuiltin {
			doubanMovieLiveSucceeded = true
		}
		s.mu.RLock()
		hasLiveDoubanMovie := s.doubanTrendingMovie != nil && len(s.doubanTrendingMovie.Entries) > 0 && s.doubanTrendingMovie.Source != SourceDoubanTrendingMovieBuiltin
		s.mu.RUnlock()
		if doubanMovieLiveSucceeded || !hasLiveDoubanMovie {
			if err := s.saveSnapshot(ctx, ConfigKeyDoubanTrendingMovie, snap); err != nil {
				return summary, fmt.Errorf("save douban trending movie cache: %w", err)
			}
			s.mu.Lock()
			s.doubanTrendingMovie = snap
			s.doubanTrendingMovieIdx = buildTrendingChartIndex(snap.Entries)
			s.mu.Unlock()
		}
	}

	if snap, err := s.fetcher.FetchDoubanTrendingTV(ctx); err != nil {
		doubanTVErr = err
		summary.DoubanTrendingTVError = err.Error()
	} else {
		if snap.Source != SourceDoubanTrendingTVBuiltin {
			doubanTVLiveSucceeded = true
		}
		s.mu.RLock()
		hasLiveDoubanTV := s.doubanTrendingTV != nil && len(s.doubanTrendingTV.Entries) > 0 && s.doubanTrendingTV.Source != SourceDoubanTrendingTVBuiltin
		s.mu.RUnlock()
		if doubanTVLiveSucceeded || !hasLiveDoubanTV {
			if err := s.saveSnapshot(ctx, ConfigKeyDoubanTrendingTV, snap); err != nil {
				return summary, fmt.Errorf("save douban trending tv cache: %w", err)
			}
			s.mu.Lock()
			s.doubanTrendingTV = snap
			s.doubanTrendingTVIdx = buildTrendingChartIndex(snap.Entries)
			s.mu.Unlock()
		}
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
	if s.doubanTrendingMovie != nil {
		summary.DoubanTrendingMovieCount = len(s.doubanTrendingMovie.Entries)
	}
	if s.doubanTrendingTV != nil {
		summary.DoubanTrendingTVCount = len(s.doubanTrendingTV.Entries)
	}
	s.mu.RUnlock()

	matched, added, applyErr := s.ApplyTagsToLibrary(ctx)
	if applyErr != nil {
		return summary, applyErr
	}
	summary.MatchedItems = matched
	summary.TagsAdded = added

	if doubanErr != nil && !imdbLiveSucceeded && !tmdbLiveSucceeded && !doubanMovieLiveSucceeded && !doubanTVLiveSucceeded {
		parts := []string{
			fmt.Sprintf("douban=%v", doubanErr),
		}
		if imdbErr != nil {
			parts = append(parts, fmt.Sprintf("imdb=%v", imdbErr))
		}
		if tmdbErr != nil {
			parts = append(parts, fmt.Sprintf("tmdb_trending=%v", tmdbErr))
		}
		if doubanMovieErr != nil {
			parts = append(parts, fmt.Sprintf("douban_trending_movie=%v", doubanMovieErr))
		}
		if doubanTVErr != nil {
			parts = append(parts, fmt.Sprintf("douban_trending_tv=%v", doubanTVErr))
		}
		return summary, fmt.Errorf("all chart fetches failed: %s", strings.Join(parts, "; "))
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
	if s.doubanTrendingMovie == nil || len(s.doubanTrendingMovie.Entries) == 0 || now.Sub(s.doubanTrendingMovie.UpdatedAt) >= interval {
		return true
	}
	if s.doubanTrendingTV == nil || len(s.doubanTrendingTV.Entries) == 0 || now.Sub(s.doubanTrendingTV.UpdatedAt) >= interval {
		return true
	}
	return false
}
