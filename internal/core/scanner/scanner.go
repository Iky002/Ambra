package scanner

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/eventbus"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

// ScanStats summarizes the result of a library scan.
type ScanStats struct {
	LibraryID    string
	ItemsAdded   int
	ItemsUpdated int
	ItemsSkipped int
	Errors       int
}

// Scanner performs incremental media library scanning, metadata merging, and FTS5 indexing.
type Scanner struct {
	repo     *sqlite.LibraryRepo
	registry *plugins.Registry
	bus      *eventbus.Bus
	cfg      *config.Config
	prober   *Prober

	mu       sync.Mutex
	scanning map[string]bool
}

func New(repo *sqlite.LibraryRepo, registry *plugins.Registry, bus *eventbus.Bus, cfg *config.Config) *Scanner {
	ffprobePath := "ffprobe"
	if cfg != nil && cfg.FFprobePath != "" {
		ffprobePath = cfg.FFprobePath
	}
	return &Scanner{
		repo:     repo,
		registry: registry,
		bus:      bus,
		cfg:      cfg,
		prober:   NewProber(ffprobePath),
		scanning: make(map[string]bool),
	}
}

// SetProber replaces the media prober (pass nil or NewDisabledProber() to disable probing).
func (s *Scanner) SetProber(p *Prober) {
	s.prober = p
}

// ScanAll scans all configured libraries sequentially.
func (s *Scanner) ScanAll(ctx context.Context) ([]ScanStats, error) {
	libs, err := s.repo.ListLibraries(ctx)
	if err != nil {
		return nil, err
	}
	var all []ScanStats
	for _, lib := range libs {
		st, err := s.ScanLibrary(ctx, lib.ID)
		if err != nil {
			return all, err
		}
		all = append(all, st)
	}
	return all, nil
}

// ScanLibraryAsync starts an asynchronous scan for a single library (or all if libraryID == "").
func (s *Scanner) ScanLibraryAsync(libraryID string) {
	go func() {
		ctx := context.Background()
		if libraryID == "" {
			_, _ = s.ScanAll(ctx)
			return
		}
		_, _ = s.ScanLibrary(ctx, libraryID)
	}()
}

// WaitIdle blocks until no library scan is in progress or ctx is done.
func (s *Scanner) WaitIdle(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		s.mu.Lock()
		busy := len(s.scanning) > 0
		s.mu.Unlock()
		if !busy {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// ScanLibrary incrementally scans all root paths of a library.
func (s *Scanner) ScanLibrary(ctx context.Context, libraryID string) (ScanStats, error) {
	libraryID = guid.Normalize(libraryID)
	stats := ScanStats{LibraryID: libraryID}

	s.mu.Lock()
	if s.scanning[libraryID] {
		s.mu.Unlock()
		return stats, nil
	}
	s.scanning[libraryID] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.scanning, libraryID)
		s.mu.Unlock()
	}()

	lib, err := s.repo.GetLibrary(ctx, libraryID)
	if err != nil {
		return stats, err
	}

	forceTV := strings.EqualFold(lib.CollectionType, "tvshows")
	seriesCache := make(map[string]*sqlite.ItemRecord)
	seasonCache := make(map[string]*sqlite.ItemRecord)

	for _, root := range lib.Paths {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				stats.Errors++
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			if d.IsDir() {
				if strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasPrefix(d.Name(), ".") {
				return nil
			}
			if !IsVideoFile(path) && !IsStrmFile(path) {
				return nil
			}

			epInfo := ParseEpisodePath(root, path, forceTV)
			var err error
			var changed bool
			if epInfo.IsEpisode {
				changed, err = s.indexEpisode(ctx, lib, epInfo, path, seriesCache, seasonCache)
			} else {
				changed, err = s.indexMovie(ctx, lib, path)
			}
			if err != nil {
				stats.Errors++
				return nil
			}
			if changed {
				stats.ItemsAdded++
			} else {
				stats.ItemsSkipped++
			}
			return nil
		})
	}

	if s.bus != nil {
		s.bus.Publish(ctx, eventbus.Event{
			Type: eventbus.EventLibraryChanged,
			Payload: map[string]any{
				"LibraryId":    lib.ID,
				"ItemsAdded":   stats.ItemsAdded,
				"ItemsUpdated": stats.ItemsUpdated,
			},
		})
	}
	return stats, nil
}

func (s *Scanner) indexMovie(ctx context.Context, lib *sqlite.LibraryRecord, mediaPath string) (bool, error) {
	info, err := os.Stat(mediaPath)
	if err != nil {
		return false, err
	}
	mtime := info.ModTime().Unix()
	size := info.Size()

	isStrm := IsStrmFile(mediaPath)
	container := strings.TrimPrefix(strings.ToLower(filepath.Ext(mediaPath)), ".")
	if isStrm {
		container = "strm"
	}

	existing, err := s.repo.FindItemByMediaSourcePath(ctx, lib.ID, mediaPath)
	if err == nil && existing != nil {
		if s.isMediaSourceUnchanged(ctx, existing, mediaPath, size, mtime) {
			return false, nil
		}
		if existing.Path != mediaPath {
			probeRes := s.probeMedia(ctx, mediaPath, isStrm)
			ms := s.buildMediaSource(existing, mediaPath, size, mtime, isStrm, container, probeRes)
			if err := s.repo.UpsertMediaSourceAndStreams(ctx, existing.ID, ms); err != nil {
				return false, err
			}
			return true, nil
		}
	}

	parsed := ParseMovieName(mediaPath)
	title := parsed.Title
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(mediaPath), filepath.Ext(mediaPath))
	}

	meta := s.fetchMergedMetadata(ctx, plugins.MediaQuery{
		ItemType: "Movie",
		Title:    title,
		Year:     parsed.Year,
		Path:     mediaPath,
		Language: s.language(),
	})

	// Auto-merge into existing movie if this is a new file matching an existing item by TMDB/IMDB ID or same-dir title.
	if existing == nil {
		var mergeTarget *sqlite.ItemRecord
		if meta != nil && len(meta.ProviderIDs) > 0 {
			if tmdbID := getProviderID(meta.ProviderIDs, "Tmdb"); tmdbID != "" {
				mergeTarget, _ = s.repo.FindItemByProviderID(ctx, lib.ID, "Movie", "Tmdb", tmdbID)
			}
			if mergeTarget == nil {
				if imdbID := getProviderID(meta.ProviderIDs, "Imdb"); imdbID != "" {
					mergeTarget, _ = s.repo.FindItemByProviderID(ctx, lib.ID, "Movie", "Imdb", imdbID)
				}
			}
		}
		if mergeTarget == nil {
			mergeTarget, _ = s.repo.FindMovieInDirByBaseTitle(ctx, lib.ID, filepath.Dir(mediaPath), title)
		}
		if mergeTarget != nil {
			probeRes := s.probeMedia(ctx, mediaPath, isStrm)
			ms := s.buildMediaSource(mergeTarget, mediaPath, size, mtime, isStrm, container, probeRes)
			if err := s.repo.UpsertMediaSourceAndStreams(ctx, mergeTarget.ID, ms); err != nil {
				return false, err
			}
			return true, nil
		}
	}

	itemID := guid.New()
	if existing != nil {
		itemID = existing.ID
	}

	probeRes := s.probeMedia(ctx, mediaPath, isStrm)

	rec := &sqlite.ItemRecord{
		ID:        itemID,
		LibraryID: lib.ID,
		ParentID:  lib.ID,
		Type:      "Movie",
		MediaType: "Video",
		IsFolder:  false,
		IsStrm:    isStrm,
		Name:      title,
		SortName:  strings.ToLower(title),
		Container: container,
		Path:      mediaPath,
		FileSize:  &size,
		FileMtime: &mtime,
		Etag:      computeEtag(mediaPath, size, mtime),
	}
	if probeRes != nil && probeRes.RuntimeTicks > 0 {
		rt := probeRes.RuntimeTicks
		rec.RuntimeTicks = &rt
	}
	if parsed.Year > 0 {
		y := parsed.Year
		rec.ProductionYear = &y
	}
	applyMetadataToItem(rec, meta)

	if err := s.repo.UpsertItem(ctx, rec); err != nil {
		return false, err
	}
	if err := s.repo.ReplaceItemAncestors(ctx, rec.ID, []sqlite.AncestorLink{
		{AncestorID: lib.ID, Depth: 1},
	}); err != nil {
		return false, err
	}

	// MediaSource + probed embedded streams + external subtitles (preserving any merged alternate versions)
	ms := s.buildMediaSource(rec, mediaPath, size, mtime, isStrm, container, probeRes)
	if err := s.repo.UpsertMediaSourceAndStreams(ctx, rec.ID, ms); err != nil {
		return false, err
	}

	// Local images
	imgs := discoverLocalImages(rec.ID, mediaPath, filepath.Dir(mediaPath), false)
	if err := s.repo.ReplaceItemImages(ctx, rec.ID, imgs); err != nil {
		return false, err
	}

	// Taxonomies, People & FTS5
	s.persistMetaRelationsAndFTS(ctx, rec, meta)
	return true, nil
}

func (s *Scanner) indexEpisode(
	ctx context.Context,
	lib *sqlite.LibraryRecord,
	epInfo ParsedEpisode,
	mediaPath string,
	seriesCache map[string]*sqlite.ItemRecord,
	seasonCache map[string]*sqlite.ItemRecord,
) (bool, error) {
	info, err := os.Stat(mediaPath)
	if err != nil {
		return false, err
	}
	mtime := info.ModTime().Unix()
	size := info.Size()

	isStrm := IsStrmFile(mediaPath)
	container := strings.TrimPrefix(strings.ToLower(filepath.Ext(mediaPath)), ".")
	if isStrm {
		container = "strm"
	}

	existing, err := s.repo.FindItemByMediaSourcePath(ctx, lib.ID, mediaPath)
	if err == nil && existing != nil {
		if s.isMediaSourceUnchanged(ctx, existing, mediaPath, size, mtime) {
			return false, nil
		}
		if existing.Path != mediaPath {
			probeRes := s.probeMedia(ctx, mediaPath, isStrm)
			ms := s.buildMediaSource(existing, mediaPath, size, mtime, isStrm, container, probeRes)
			if err := s.repo.UpsertMediaSourceAndStreams(ctx, existing.ID, ms); err != nil {
				return false, err
			}
			return true, nil
		}
	}

	// 1. Ensure Series
	seriesRec, err := s.ensureSeries(ctx, lib, epInfo, seriesCache)
	if err != nil {
		return false, err
	}

	// 2. Ensure Season
	seasonRec, err := s.ensureSeason(ctx, lib, seriesRec, epInfo, seasonCache)
	if err != nil {
		return false, err
	}

	// Auto-merge into existing episode if same Series + SeasonNumber + EpisodeNumber already exists.
	if existing == nil {
		if mergeTarget, err := s.repo.FindEpisodeByNumber(ctx, seriesRec.ID, epInfo.SeasonNumber, epInfo.EpisodeNumber); err == nil && mergeTarget != nil {
			probeRes := s.probeMedia(ctx, mediaPath, isStrm)
			ms := s.buildMediaSource(mergeTarget, mediaPath, size, mtime, isStrm, container, probeRes)
			if err := s.repo.UpsertMediaSourceAndStreams(ctx, mergeTarget.ID, ms); err != nil {
				return false, err
			}
			return true, nil
		}
	}

	// 3. Upsert Episode
	meta := s.fetchMergedMetadata(ctx, plugins.MediaQuery{
		ItemType:      "Episode",
		Title:         epInfo.EpisodeTitle,
		SeasonNumber:  epInfo.SeasonNumber,
		EpisodeNumber: epInfo.EpisodeNumber,
		Path:          mediaPath,
		ProviderIDs:   seriesRec.Extra.ProviderIDs,
		Language:      s.language(),
	})

	itemID := guid.New()
	if existing != nil {
		itemID = existing.ID
	}

	probeRes := s.probeMedia(ctx, mediaPath, isStrm)

	epNum := epInfo.EpisodeNumber
	seasonNum := epInfo.SeasonNumber
	name := epInfo.EpisodeTitle
	if name == "" {
		name = fmt.Sprintf("S%02dE%02d", seasonNum, epNum)
	}

	rec := &sqlite.ItemRecord{
		ID:                itemID,
		LibraryID:         lib.ID,
		ParentID:          seasonRec.ID,
		SeriesID:          seriesRec.ID,
		SeasonID:          seasonRec.ID,
		Type:              "Episode",
		MediaType:         "Video",
		IsFolder:          false,
		IsStrm:            isStrm,
		Name:              name,
		SortName:          fmt.Sprintf("%03d-%04d-%s", seasonNum, epNum, strings.ToLower(name)),
		IndexNumber:       &epNum,
		ParentIndexNumber: &seasonNum,
		Container:         container,
		Path:              mediaPath,
		FileSize:          &size,
		FileMtime:         &mtime,
		Etag:              computeEtag(mediaPath, size, mtime),
		Extra: sqlite.ItemExtra{
			SeriesName: seriesRec.Name,
			SeasonName: seasonRec.Name,
		},
	}
	if probeRes != nil && probeRes.RuntimeTicks > 0 {
		rt := probeRes.RuntimeTicks
		rec.RuntimeTicks = &rt
	}
	applyMetadataToItem(rec, meta)

	if err := s.repo.UpsertItem(ctx, rec); err != nil {
		return false, err
	}
	if err := s.repo.ReplaceItemAncestors(ctx, rec.ID, []sqlite.AncestorLink{
		{AncestorID: seasonRec.ID, Depth: 1},
		{AncestorID: seriesRec.ID, Depth: 2},
		{AncestorID: lib.ID, Depth: 3},
	}); err != nil {
		return false, err
	}

	ms := s.buildMediaSource(rec, mediaPath, size, mtime, isStrm, container, probeRes)
	if err := s.repo.UpsertMediaSourceAndStreams(ctx, rec.ID, ms); err != nil {
		return false, err
	}

	imgs := discoverLocalImages(rec.ID, mediaPath, filepath.Dir(mediaPath), true)
	if len(imgs) > 0 {
		_ = s.repo.ReplaceItemImages(ctx, rec.ID, imgs)
	}

	s.persistMetaRelationsAndFTS(ctx, rec, meta)
	return true, nil
}

func (s *Scanner) ensureSeries(ctx context.Context, lib *sqlite.LibraryRecord, epInfo ParsedEpisode, cache map[string]*sqlite.ItemRecord) (*sqlite.ItemRecord, error) {
	if rec, ok := cache[epInfo.SeriesDir]; ok {
		return rec, nil
	}
	existing, err := s.repo.FindItemByPath(ctx, lib.ID, epInfo.SeriesDir)
	if err == nil && existing != nil {
		cache[epInfo.SeriesDir] = existing
		return existing, nil
	}
	if err != nil && !errors.Is(err, sqlite.ErrNotFound) {
		return nil, err
	}

	meta := s.fetchMergedMetadata(ctx, plugins.MediaQuery{
		ItemType: "Series",
		Title:    epInfo.SeriesName,
		Year:     epInfo.SeriesYear,
		Path:     epInfo.SeriesDir,
		Language: s.language(),
	})

	// Cross-directory auto-merge for Series sharing the same TMDB/IMDB ID.
	if meta != nil && len(meta.ProviderIDs) > 0 {
		var matched *sqlite.ItemRecord
		if tmdbID := getProviderID(meta.ProviderIDs, "Tmdb"); tmdbID != "" {
			matched, _ = s.repo.FindItemByProviderID(ctx, lib.ID, "Series", "Tmdb", tmdbID)
		}
		if matched == nil {
			if imdbID := getProviderID(meta.ProviderIDs, "Imdb"); imdbID != "" {
				matched, _ = s.repo.FindItemByProviderID(ctx, lib.ID, "Series", "Imdb", imdbID)
			}
		}
		if matched != nil {
			cache[epInfo.SeriesDir] = matched
			return matched, nil
		}
	}

	rec := &sqlite.ItemRecord{
		ID:        guid.New(),
		LibraryID: lib.ID,
		ParentID:  lib.ID,
		Type:      "Series",
		IsFolder:  true,
		Name:      epInfo.SeriesName,
		SortName:  strings.ToLower(epInfo.SeriesName),
		Path:      epInfo.SeriesDir,
		Etag:      computeEtag(epInfo.SeriesDir, 0, time.Now().Unix()),
	}
	if epInfo.SeriesYear > 0 {
		y := epInfo.SeriesYear
		rec.ProductionYear = &y
	}
	applyMetadataToItem(rec, meta)

	if err := s.repo.UpsertItem(ctx, rec); err != nil {
		return nil, err
	}
	if err := s.repo.ReplaceItemAncestors(ctx, rec.ID, []sqlite.AncestorLink{
		{AncestorID: lib.ID, Depth: 1},
	}); err != nil {
		return nil, err
	}

	imgs := discoverLocalImages(rec.ID, "", epInfo.SeriesDir, false)
	if len(imgs) > 0 {
		_ = s.repo.ReplaceItemImages(ctx, rec.ID, imgs)
	}
	s.persistMetaRelationsAndFTS(ctx, rec, meta)

	cache[epInfo.SeriesDir] = rec
	return rec, nil
}

func (s *Scanner) ensureSeason(ctx context.Context, lib *sqlite.LibraryRecord, series *sqlite.ItemRecord, epInfo ParsedEpisode, cache map[string]*sqlite.ItemRecord) (*sqlite.ItemRecord, error) {
	key := fmt.Sprintf("%s#%d", series.ID, epInfo.SeasonNumber)
	if rec, ok := cache[key]; ok {
		return rec, nil
	}
	if existingByNum, err := s.repo.FindSeasonByNumber(ctx, series.ID, epInfo.SeasonNumber); err == nil && existingByNum != nil {
		cache[key] = existingByNum
		return existingByNum, nil
	}
	seasonPath := epInfo.SeasonDir
	if seasonPath == "" {
		seasonPath = fmt.Sprintf("%s#Season%02d", series.Path, epInfo.SeasonNumber)
	}

	existing, err := s.repo.FindItemByPath(ctx, lib.ID, seasonPath)
	if err == nil && existing != nil {
		cache[key] = existing
		return existing, nil
	}
	if err != nil && !errors.Is(err, sqlite.ErrNotFound) {
		return nil, err
	}

	sn := epInfo.SeasonNumber
	name := fmt.Sprintf("Season %d", sn)
	rec := &sqlite.ItemRecord{
		ID:          guid.New(),
		LibraryID:   lib.ID,
		ParentID:    series.ID,
		SeriesID:    series.ID,
		Type:        "Season",
		IsFolder:    true,
		Name:        name,
		SortName:    fmt.Sprintf("%04d", sn),
		IndexNumber: &sn,
		Path:        seasonPath,
		Etag:        computeEtag(seasonPath, 0, time.Now().Unix()),
		Extra: sqlite.ItemExtra{
			SeriesName: series.Name,
		},
	}
	if err := s.repo.UpsertItem(ctx, rec); err != nil {
		return nil, err
	}
	if err := s.repo.ReplaceItemAncestors(ctx, rec.ID, []sqlite.AncestorLink{
		{AncestorID: series.ID, Depth: 1},
		{AncestorID: lib.ID, Depth: 2},
	}); err != nil {
		return nil, err
	}
	if epInfo.SeasonDir != "" {
		imgs := discoverLocalImages(rec.ID, "", epInfo.SeasonDir, false)
		if len(imgs) > 0 {
			_ = s.repo.ReplaceItemImages(ctx, rec.ID, imgs)
		}
	}
	cache[key] = rec
	return rec, nil
}

func (s *Scanner) probeMedia(ctx context.Context, mediaPath string, isStrm bool) *ProbeResult {
	// Never probe .strm files during scan to avoid triggering cloud-drive rate limits or bans.
	if isStrm || s.prober == nil {
		return nil
	}
	res, err := s.prober.Probe(ctx, mediaPath)
	if err != nil || res == nil {
		return nil
	}
	return res
}

func (s *Scanner) buildMediaSource(item *sqlite.ItemRecord, mediaPath string, size, mtime int64, isStrm bool, container string, probeRes *ProbeResult) sqlite.MediaSourceRecord {
	msID := guid.New()
	proto := "File"
	if isStrm {
		proto = "Http"
	}

	var streams []sqlite.MediaStreamRecord
	var bitrate int
	var runtimeTicks int64

	if probeRes != nil && len(probeRes.Streams) > 0 {
		runtimeTicks = probeRes.RuntimeTicks
		bitrate = probeRes.Bitrate
		if size == 0 && probeRes.Size > 0 {
			size = probeRes.Size
		}
		if container == "" && probeRes.Container != "" {
			container = probeRes.Container
		}
		streams = make([]sqlite.MediaStreamRecord, 0, len(probeRes.Streams))
		for i, ps := range probeRes.Streams {
			streams = append(streams, sqlite.MediaStreamRecord{
				MediaSourceID: msID,
				ItemID:        item.ID,
				StreamIndex:   i,
				StreamType:    ps.StreamType,
				Codec:         ps.Codec,
				Language:      ps.Language,
				SubVariant:    ps.SubVariant,
				DisplayTitle:  ps.DisplayTitle,
				Title:         ps.Title,
				Width:         ps.Width,
				Height:        ps.Height,
				VideoRange:    ps.VideoRange,
				AudioLayout:   ps.AudioLayout,
				Channels:      ps.Channels,
				SampleRate:    ps.SampleRate,
				Bitrate:       ps.Bitrate,
				IsDefault:     ps.IsDefault,
				IsForced:      ps.IsForced,
				IsExternal:    false,
			})
		}
	} else {
		// Default synthetic Video & Audio streams (when ffprobe is not executed or gracefully degraded)
		videoCodec := "h264"
		if container == "webm" {
			videoCodec = "vp9"
		}
		audioCodec := "aac"
		if container == "mkv" {
			audioCodec = "ac3"
		}

		streams = []sqlite.MediaStreamRecord{
			{
				MediaSourceID: msID,
				ItemID:        item.ID,
				StreamIndex:   0,
				StreamType:    "Video",
				Codec:         videoCodec,
				DisplayTitle:  strings.ToUpper(videoCodec),
				Width:         1920,
				Height:        1080,
				VideoRange:    "SDR",
				IsDefault:     true,
			},
			{
				MediaSourceID: msID,
				ItemID:        item.ID,
				StreamIndex:   1,
				StreamType:    "Audio",
				Codec:         audioCodec,
				Language:      "und",
				DisplayTitle:  strings.ToUpper(audioCodec) + " Stereo",
				Channels:      2,
				SampleRate:    48000,
				IsDefault:     true,
			},
		}
	}

	// Scan companion external subtitles in the same directory; stream_index continues after embedded streams.
	extSubs := discoverExternalSubtitles(mediaPath, msID, item.ID, len(streams))
	streams = append(streams, extSubs...)

	extraJSON := fmt.Sprintf(`{"file_mtime":%d}`, mtime)

	return sqlite.MediaSourceRecord{
		ID:                   msID,
		ItemID:               item.ID,
		Name:                 buildVersionDisplayName(mediaPath, container, isStrm, probeRes),
		Path:                 mediaPath,
		Protocol:             proto,
		Container:            container,
		Size:                 size,
		Bitrate:              bitrate,
		RuntimeTicks:         runtimeTicks,
		IsRemote:             isStrm,
		SupportsDirectPlay:   true,
		SupportsDirectStream: true,
		SupportsTranscoding:  true,
		ExtraJSON:            extraJSON,
		Streams:              streams,
	}
}

func (s *Scanner) isMediaSourceUnchanged(ctx context.Context, existing *sqlite.ItemRecord, mediaPath string, size, mtime int64) bool {
	if existing.Path == mediaPath {
		return existing.FileMtime != nil && *existing.FileMtime == mtime && existing.FileSize != nil && *existing.FileSize == size
	}
	sources, err := s.repo.GetMediaSourcesByItemID(ctx, existing.ID)
	if err != nil {
		return false
	}
	for _, src := range sources {
		if src.Path != mediaPath {
			continue
		}
		if src.Size != size {
			return false
		}
		var meta struct {
			FileMtime int64 `json:"file_mtime"`
		}
		if src.ExtraJSON != "" && json.Unmarshal([]byte(src.ExtraJSON), &meta) == nil && meta.FileMtime > 0 {
			return meta.FileMtime == mtime
		}
		return true
	}
	return false
}

func getProviderID(ids map[string]string, key string) string {
	if len(ids) == 0 {
		return ""
	}
	if v := strings.TrimSpace(ids[key]); v != "" {
		return v
	}
	for k, v := range ids {
		if strings.EqualFold(k, key) {
			if tv := strings.TrimSpace(v); tv != "" {
				return tv
			}
		}
	}
	return ""
}

func buildVersionDisplayName(mediaPath, container string, isStrm bool, probeRes *ProbeResult) string {
	stem := strings.TrimSuffix(filepath.Base(mediaPath), filepath.Ext(mediaPath))
	lower := strings.ToLower(stem)

	var resLabel, rangeLabel string
	if probeRes != nil {
		for _, st := range probeRes.Streams {
			if !strings.EqualFold(st.StreamType, "Video") {
				continue
			}
			switch {
			case st.Height >= 2100:
				resLabel = "2160p"
			case st.Height >= 1000:
				resLabel = "1080p"
			case st.Height >= 700:
				resLabel = "720p"
			case st.Height >= 480:
				resLabel = "480p"
			}
			vr := strings.ToUpper(strings.TrimSpace(st.VideoRange))
			switch {
			case strings.Contains(vr, "DOVI") || strings.Contains(vr, "DOLBY"):
				rangeLabel = "DolbyVision"
			case vr != "" && vr != "SDR":
				rangeLabel = vr
			}
			break
		}
	}

	if resLabel == "" {
		switch {
		case containsToken(lower, "2160p") || containsToken(lower, "4k") || containsToken(lower, "uhd"):
			resLabel = "2160p"
		case containsToken(lower, "1080p"):
			resLabel = "1080p"
		case containsToken(lower, "720p"):
			resLabel = "720p"
		case containsToken(lower, "480p"):
			resLabel = "480p"
		}
	}

	if rangeLabel == "" {
		switch {
		case containsToken(lower, "dv") || containsToken(lower, "dovi") || strings.Contains(lower, "dolbyvision") || strings.Contains(lower, "dolby.vision"):
			rangeLabel = "DolbyVision"
		case strings.Contains(lower, "hdr10+"):
			rangeLabel = "HDR10+"
		case containsToken(lower, "hdr10"):
			rangeLabel = "HDR10"
		case containsToken(lower, "hdr"):
			rangeLabel = "HDR"
		}
	}

	formatLabel := strings.ToUpper(container)
	if isStrm {
		formatLabel = "STRM"
	}

	var specParts []string
	if resLabel != "" {
		specParts = append(specParts, resLabel)
	}
	if rangeLabel != "" {
		specParts = append(specParts, rangeLabel)
	}

	if len(specParts) > 0 {
		label := strings.Join(specParts, " ")
		if formatLabel != "" {
			return label + " · " + formatLabel
		}
		return label
	}

	// Fallback to trailing " - <tag>" suffix (Emby/Jellyfin multi-version convention)
	if idx := strings.LastIndex(stem, " - "); idx >= 0 {
		tag := strings.TrimSpace(stem[idx+3:])
		if tag != "" {
			if formatLabel != "" {
				return tag + " · " + formatLabel
			}
			return tag
		}
	}

	if formatLabel != "" && stem != "" {
		return stem + " · " + formatLabel
	}
	return stem
}

func discoverExternalSubtitles(mediaPath, mediaSourceID, itemID string, startIndex int) []sqlite.MediaStreamRecord {
	dir := filepath.Dir(mediaPath)
	mediaStem := strings.ToLower(strings.TrimSuffix(filepath.Base(mediaPath), filepath.Ext(mediaPath)))

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []sqlite.MediaStreamRecord
	idx := startIndex
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		codec, ok := SubtitleCodecForExt(name)
		if !ok {
			continue
		}
		lowerName := strings.ToLower(name)
		if !strings.HasPrefix(lowerName, mediaStem) {
			continue
		}
		cls := ClassifySubtitle(name, codec)
		out = append(out, sqlite.MediaStreamRecord{
			MediaSourceID: mediaSourceID,
			ItemID:        itemID,
			StreamIndex:   idx,
			StreamType:    "Subtitle",
			Codec:         cls.Codec,
			Language:      cls.Language,
			SubVariant:    cls.SubVariant,
			DisplayTitle:  cls.DisplayTitle,
			Title:         strings.TrimSuffix(name, filepath.Ext(name)),
			IsDefault:     cls.IsDefault,
			IsForced:      cls.IsForced,
			IsExternal:    true,
			Path:          filepath.Join(dir, name),
		})
		idx++
	}
	return out
}

func discoverLocalImages(itemID, mediaPath, dir string, episodeOnly bool) []sqlite.ImageRecord {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	mediaStem := ""
	if mediaPath != "" {
		mediaStem = strings.ToLower(strings.TrimSuffix(filepath.Base(mediaPath), filepath.Ext(mediaPath)))
	}

	var out []sqlite.ImageRecord
	seenType := make(map[string]int)

	addImg := func(imgType, fullPath string) {
		info, err := os.Stat(fullPath)
		if err != nil {
			return
		}
		idx := seenType[imgType]
		// Only allow multiple indices for Backdrop
		if imgType != "Backdrop" && idx > 0 {
			return
		}
		seenType[imgType] = idx + 1
		out = append(out, sqlite.ImageRecord{
			ItemID:     itemID,
			ImageType:  imgType,
			ImageIndex: idx,
			Path:       fullPath,
			Tag:        computeEtag(fullPath, info.Size(), info.ModTime().Unix()),
		})
	}

	for _, entry := range entries {
		if entry.IsDir() || !IsImageFile(entry.Name()) {
			continue
		}
		fullPath := filepath.Join(dir, entry.Name())
		stem := strings.ToLower(strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())))

		if episodeOnly {
			if mediaStem != "" && (stem == mediaStem || stem == mediaStem+"-thumb" || stem == mediaStem+"-poster") {
				addImg("Primary", fullPath)
			}
			continue
		}

		switch {
		case stem == "poster" || stem == "folder" || stem == "cover" || (mediaStem != "" && (stem == mediaStem+"-poster" || stem == mediaStem)):
			addImg("Primary", fullPath)
		case stem == "backdrop" || stem == "fanart" || strings.HasPrefix(stem, "backdrop") || strings.HasPrefix(stem, "fanart") || (mediaStem != "" && (stem == mediaStem+"-fanart" || stem == mediaStem+"-backdrop")):
			addImg("Backdrop", fullPath)
		case stem == "logo" || stem == "clearlogo" || (mediaStem != "" && stem == mediaStem+"-logo"):
			addImg("Logo", fullPath)
		case stem == "thumb" || stem == "landscape" || (mediaStem != "" && stem == mediaStem+"-thumb"):
			addImg("Thumb", fullPath)
		}
	}
	return out
}

func (s *Scanner) fetchMergedMetadata(ctx context.Context, q plugins.MediaQuery) *plugins.MetadataResult {
	if s.registry == nil {
		return nil
	}
	providers := s.registry.MetadataProviders()
	if len(providers) == 0 {
		return nil
	}
	var merged *plugins.MetadataResult
	for _, p := range providers {
		res, err := p.FetchMetadata(ctx, q)
		if err != nil || res == nil {
			continue
		}
		if merged == nil {
			cp := *res
			if cp.ProviderIDs == nil {
				cp.ProviderIDs = make(map[string]string)
			}
			merged = &cp
			// Propagate discovered ProviderIDs to lower-priority providers (e.g. NFO tmdbid -> TMDB)
			if len(merged.ProviderIDs) > 0 {
				if q.ProviderIDs == nil {
					q.ProviderIDs = make(map[string]string)
				}
				for k, v := range merged.ProviderIDs {
					q.ProviderIDs[k] = v
				}
			}
			continue
		}
		mergeMetadata(merged, res)
	}
	return merged
}

func mergeMetadata(dst, src *plugins.MetadataResult) {
	if dst.Name == "" {
		dst.Name = src.Name
	}
	if dst.OriginalTitle == "" {
		dst.OriginalTitle = src.OriginalTitle
	}
	if dst.Overview == "" {
		dst.Overview = src.Overview
	}
	if dst.Tagline == "" {
		dst.Tagline = src.Tagline
	}
	if dst.Year == 0 {
		dst.Year = src.Year
	}
	if dst.PremiereDate == "" {
		dst.PremiereDate = src.PremiereDate
	}
	if dst.CommunityRating == 0 {
		dst.CommunityRating = src.CommunityRating
	}
	if dst.OfficialRating == "" {
		dst.OfficialRating = src.OfficialRating
	}
	if len(dst.Genres) == 0 {
		dst.Genres = src.Genres
	}
	if len(dst.Studios) == 0 {
		dst.Studios = src.Studios
	}
	if len(dst.Tags) == 0 {
		dst.Tags = src.Tags
	}
	if len(dst.People) == 0 {
		dst.People = src.People
	}
	for k, v := range src.ProviderIDs {
		if _, exists := dst.ProviderIDs[k]; !exists {
			dst.ProviderIDs[k] = v
		}
	}
	if dst.PosterURL == "" {
		dst.PosterURL = src.PosterURL
	}
	if dst.BackdropURL == "" {
		dst.BackdropURL = src.BackdropURL
	}
	if dst.LogoURL == "" {
		dst.LogoURL = src.LogoURL
	}
}

func applyMetadataToItem(rec *sqlite.ItemRecord, meta *plugins.MetadataResult) {
	if meta == nil {
		return
	}
	if meta.Name != "" {
		rec.Name = meta.Name
		rec.SortName = strings.ToLower(meta.Name)
	}
	if meta.OriginalTitle != "" {
		rec.OriginalTitle = meta.OriginalTitle
	}
	if meta.Overview != "" {
		rec.Extra.Overview = meta.Overview
	}
	if meta.Tagline != "" {
		rec.Extra.Taglines = []string{meta.Tagline}
	}
	if meta.Year > 0 {
		y := meta.Year
		rec.ProductionYear = &y
	}
	if meta.PremiereDate != "" {
		if t, err := time.Parse("2006-01-02", meta.PremiereDate); err == nil {
			unix := t.Unix()
			rec.PremiereDate = &unix
		}
	}
	if meta.CommunityRating > 0 {
		r := meta.CommunityRating
		rec.CommunityRating = &r
	}
	if meta.OfficialRating != "" {
		rec.OfficialRating = meta.OfficialRating
	}
	if len(meta.ProviderIDs) > 0 {
		rec.Extra.ProviderIDs = meta.ProviderIDs
	}
}

func (s *Scanner) persistMetaRelationsAndFTS(ctx context.Context, rec *sqlite.ItemRecord, meta *plugins.MetadataResult) {
	var peopleNames []string
	if meta != nil {
		if len(meta.Genres) > 0 {
			_ = s.repo.ReplaceItemTaxonomies(ctx, rec.ID, "genre", meta.Genres)
		}
		if len(meta.Studios) > 0 {
			_ = s.repo.ReplaceItemTaxonomies(ctx, rec.ID, "studio", meta.Studios)
		}
		if len(meta.Tags) > 0 {
			_ = s.repo.ReplaceItemTaxonomies(ctx, rec.ID, "tag", meta.Tags)
		}
		if len(meta.People) > 0 {
			pins := make([]sqlite.PersonInput, len(meta.People))
			for i, p := range meta.People {
				pins[i] = sqlite.PersonInput{
					Name:      p.Name,
					Role:      p.Role,
					Type:      p.Type,
					SortOrder: i,
				}
				peopleNames = append(peopleNames, p.Name)
			}
			_ = s.repo.ReplaceItemPeople(ctx, rec.ID, pins)
		}
	}

	titleRaw := strings.TrimSpace(rec.Name + " " + rec.OriginalTitle)
	titleTokens := TokenizeForFTS(rec.Name, rec.OriginalTitle, rec.Extra.SeriesName)
	peopleTokens := TokenizeForFTS(peopleNames...)
	_ = s.repo.UpsertSearchFTS(ctx, rec.ID, titleRaw, titleTokens, "", "", peopleTokens)
}

func (s *Scanner) language() string {
	if s.cfg != nil && s.cfg.TMDBLanguage != "" {
		return s.cfg.TMDBLanguage
	}
	return "zh-CN"
}

func computeEtag(path string, size, mtime int64) string {
	h := sha1.Sum([]byte(fmt.Sprintf("%s|%d|%d", path, size, mtime)))
	return hex.EncodeToString(h[:8])
}
