package scanner

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/budget"
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
	ItemsDeleted int
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

	mu             sync.Mutex
	budget         *budget.Pool
	httpClient     *http.Client
	symlinkPolicy  SymlinkPolicy
	onScanComplete func(ctx context.Context, libraryID string, stats ScanStats)
	scanning       map[string]bool
	cancels        map[string]context.CancelFunc
	progress       map[string]*LibraryScanProgress

	pauseMu  sync.Mutex
	paused   bool
	resumeCh chan struct{}

	mergeMu sync.Mutex
}

func New(repo *sqlite.LibraryRepo, registry *plugins.Registry, bus *eventbus.Bus, cfg *config.Config) *Scanner {
	ffprobePath := "ffprobe"
	if cfg != nil && cfg.FFprobePath != "" {
		ffprobePath = cfg.FFprobePath
	}
	return &Scanner{
		repo:          repo,
		registry:      registry,
		bus:           bus,
		cfg:           cfg,
		prober:        NewProber(ffprobePath),
		symlinkPolicy: SymlinkFollowDirect,
		scanning:      make(map[string]bool),
		cancels:       make(map[string]context.CancelFunc),
		progress:      make(map[string]*LibraryScanProgress),
	}
}

// SetProber replaces the media prober (pass nil or NewDisabledProber() to disable probing).
func (s *Scanner) SetProber(p *Prober) {
	s.mu.Lock()
	s.prober = p
	s.mu.Unlock()
}

func (s *Scanner) getProber() *Prober {
	s.mu.Lock()
	p := s.prober
	s.mu.Unlock()
	return p
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

// ScanLibrary incrementally scans all root paths of a library using the two-phase
// pipeline (Phase A discovery followed by budget-constrained concurrent Phase B enrichment).
func (s *Scanner) ScanLibrary(ctx context.Context, libraryID string) (ScanStats, error) {
	return s.scanLibrary(ctx, libraryID, false)
}

func (s *Scanner) indexMovie(ctx context.Context, lib *sqlite.LibraryRecord, mediaPath string, batch *phaseABatch) (bool, bool, *enrichTask, error) {
	info, err := os.Stat(mediaPath)
	if err != nil {
		return false, false, nil, err
	}
	mtime := info.ModTime().Unix()
	size := info.Size()

	isStrm := IsStrmFile(mediaPath)
	container := strings.TrimPrefix(strings.ToLower(filepath.Ext(mediaPath)), ".")
	if isStrm {
		container = "strm"
	}

	prober := s.getProber()
	needProbe := !isStrm && prober.IsEnabled()
	needOnline := s.hasOnlineMetadataProviders()

	existing, err := s.repo.FindItemByMediaSourcePath(ctx, lib.ID, mediaPath)
	if err == nil && existing != nil {
		if s.isMediaSourceUnchanged(ctx, existing, mediaPath, size, mtime) {
			s.scanAndIndexExtras(ctx, lib, existing, filepath.Dir(mediaPath), strings.TrimSuffix(filepath.Base(mediaPath), filepath.Ext(mediaPath)))
			if needOnline && s.isMissingPrimaryImage(ctx, existing.ID) {
				var year int
				if existing.ProductionYear != nil {
					year = *existing.ProductionYear
				}
				queryTitle := existing.Name
				queryOrig := existing.OriginalTitle
				queryIDs := cloneStringMap(existing.Extra.ProviderIDs)
				// If previously unscraped (no TMDB ID) and unlocked, re-parse from path
				// so improved filename/folder recognition heals previously mis-parsed titles.
				if !existing.Extra.LockData && getProviderID(queryIDs, "tmdb") == "" {
					reparsed := ParseMoviePath(findContainingRoot(lib.Paths, mediaPath), mediaPath)
					if reparsed.Title != "" {
						queryTitle = reparsed.Title
					}
					if reparsed.OriginalTitle != "" {
						queryOrig = reparsed.OriginalTitle
					}
					if reparsed.Year > 0 {
						year = reparsed.Year
					}
					for k, v := range reparsed.ProviderIDs {
						if queryIDs == nil {
							queryIDs = make(map[string]string)
						}
						if _, exists := queryIDs[k]; !exists {
							queryIDs[k] = v
						}
					}
				}
				backfillQuery := plugins.MediaQuery{
					ItemType:      "Movie",
					Title:         queryTitle,
					OriginalTitle: queryOrig,
					Year:          year,
					Path:          mediaPath,
					ProviderIDs:   queryIDs,
					Language:      s.language(),
				}
				return false, false, &enrichTask{
					ItemID:         existing.ID,
					LibraryID:      lib.ID,
					ItemType:       "Movie",
					MediaPath:      mediaPath,
					Size:           size,
					Mtime:          mtime,
					IsStrm:         isStrm,
					Container:      container,
					Query:          backfillQuery,
					NeedProbe:      false,
					NeedOnlineMeta: true,
					HasLocalTitle:  false,
					DisplayName:    queryTitle,
				}, nil
			}
			return false, false, nil, nil
		}
		if existing.Path != mediaPath {
			ms := s.buildMediaSource(existing, mediaPath, size, mtime, isStrm, container, nil)
			if err := s.repo.UpsertMediaSourceAndStreams(ctx, existing.ID, ms); err != nil {
				return false, false, nil, err
			}
			var task *enrichTask
			if needProbe {
				task = &enrichTask{
					ItemID:           existing.ID,
					LibraryID:        lib.ID,
					ItemType:         "Movie",
					MediaPath:        mediaPath,
					Size:             size,
					Mtime:            mtime,
					IsStrm:           isStrm,
					Container:        container,
					NeedProbe:        true,
					IsAltMediaSource: true,
				}
			}
			return true, false, task, nil
		}
	}

	parsed := ParseMoviePath(findContainingRoot(lib.Paths, mediaPath), mediaPath)
	title := parsed.Title
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(mediaPath), filepath.Ext(mediaPath))
	}

	query := plugins.MediaQuery{
		ItemType:      "Movie",
		Title:         title,
		OriginalTitle: parsed.OriginalTitle,
		Year:          parsed.Year,
		Path:          mediaPath,
		ProviderIDs:   cloneStringMap(parsed.ProviderIDs),
		Language:      s.language(),
	}
	meta := s.fetchMetadataFiltered(ctx, query, true)

	// Auto-merge into existing movie if this is a new file matching an existing item by TMDB/IMDB ID or same-dir title.
	if existing == nil {
		if batch != nil {
			var checkProviderIDs map[string]string
			if meta != nil && len(meta.ProviderIDs) > 0 {
				checkProviderIDs = meta.ProviderIDs
			} else if len(parsed.ProviderIDs) > 0 {
				checkProviderIDs = parsed.ProviderIDs
			}
			if flushErr := batch.flushIfMovieCollides(ctx, filepath.Dir(mediaPath), title, checkProviderIDs); flushErr != nil && ctx.Err() != nil {
				return false, false, nil, flushErr
			}
		}
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
			ms := s.buildMediaSource(mergeTarget, mediaPath, size, mtime, isStrm, container, nil)
			if err := s.repo.UpsertMediaSourceAndStreams(ctx, mergeTarget.ID, ms); err != nil {
				return false, false, nil, err
			}
			var task *enrichTask
			if needProbe {
				task = &enrichTask{
					ItemID:           mergeTarget.ID,
					LibraryID:        lib.ID,
					ItemType:         "Movie",
					MediaPath:        mediaPath,
					Size:             size,
					Mtime:            mtime,
					IsStrm:           isStrm,
					Container:        container,
					NeedProbe:        true,
					IsAltMediaSource: true,
				}
			}
			return true, false, task, nil
		}
	}

	itemID := guid.New()
	if existing != nil {
		itemID = existing.ID
	}

	rec := &sqlite.ItemRecord{
		ID:            itemID,
		LibraryID:     lib.ID,
		ParentID:      lib.ID,
		Type:          "Movie",
		MediaType:     "Video",
		IsFolder:      false,
		IsStrm:        isStrm,
		Name:          title,
		OriginalTitle: parsed.OriginalTitle,
		SortName:      strings.ToLower(title),
		Container:     container,
		Path:          mediaPath,
		FileSize:      &size,
		FileMtime:     &mtime,
		Etag:          computeEtag(mediaPath, size, mtime),
	}
	if len(parsed.ProviderIDs) > 0 {
		rec.Extra.ProviderIDs = cloneStringMap(parsed.ProviderIDs)
	}
	if parsed.Year > 0 {
		y := parsed.Year
		rec.ProductionYear = &y
	}
	applyMetadataToItem(rec, meta)

	hasLocalTitle := meta != nil && strings.TrimSpace(meta.Name) != ""
	var task *enrichTask
	if needProbe || needOnline {
		if len(rec.Extra.ProviderIDs) > 0 {
			query.ProviderIDs = rec.Extra.ProviderIDs
		}
		task = &enrichTask{
			ItemID:         rec.ID,
			LibraryID:      lib.ID,
			ItemType:       "Movie",
			MediaPath:      mediaPath,
			Size:           size,
			Mtime:          mtime,
			IsStrm:         isStrm,
			Container:      container,
			Query:          query,
			NeedProbe:      needProbe,
			NeedOnlineMeta: needOnline,
			HasLocalTitle:  hasLocalTitle,
			DisplayName:    rec.Name,
		}
	}

	ms := s.buildMediaSource(rec, mediaPath, size, mtime, isStrm, container, nil)
	imgs := discoverLocalImages(rec.ID, mediaPath, filepath.Dir(mediaPath), false)
	mediaDir := filepath.Dir(mediaPath)
	mediaBaseName := strings.TrimSuffix(filepath.Base(mediaPath), filepath.Ext(mediaPath))

	if batch != nil {
		titleRaw, titleTokens, peopleTokens := buildFTSFields(rec, extractMetaPeopleNames(meta))
		var pendingTasks []enrichTask
		if task != nil {
			pendingTasks = []enrichTask{*task}
		}
		pending := pendingPhaseAItem{
			input: sqlite.BatchItemInput{
				Item:           rec,
				AncestorIDs:    []string{lib.ID},
				FTSTitleRaw:    titleRaw,
				FTSTitleTokens: titleTokens,
				FTSPinyinFull:  "",
				FTSPinyinInit:  "",
				FTSPeopleNames: peopleTokens,
			},
			postHook: func(hookCtx context.Context) error {
				if err := s.repo.UpsertMediaSourceAndStreams(hookCtx, rec.ID, ms); err != nil {
					return err
				}
				if err := s.repo.ReplaceItemImages(hookCtx, rec.ID, imgs); err != nil {
					return err
				}
				if meta != nil {
					s.downloadAndSaveRemoteImages(hookCtx, rec, meta)
				}
				s.persistMetaRelations(hookCtx, rec, meta)
				if meta != nil && meta.Collection != nil {
					s.ensureAutoBoxSet(hookCtx, rec, meta.Collection)
				}
				s.scanAndIndexExtras(hookCtx, lib, rec, mediaDir, mediaBaseName)
				return nil
			},
			displayFileName: filepath.Base(mediaPath),
			tasks:           pendingTasks,
		}
		flushErr := batch.addMovie(ctx, pending, mediaDir, title)
		return true, true, nil, flushErr
	}

	if err := s.repo.UpsertItem(ctx, rec); err != nil {
		return false, false, nil, err
	}
	if err := s.repo.ReplaceItemAncestors(ctx, rec.ID, []sqlite.AncestorLink{
		{AncestorID: lib.ID, Depth: 1},
	}); err != nil {
		return false, false, nil, err
	}

	// MediaSource + external subtitles (ffprobe runs in Phase B)
	if err := s.repo.UpsertMediaSourceAndStreams(ctx, rec.ID, ms); err != nil {
		return false, false, nil, err
	}

	// Local images
	if err := s.repo.ReplaceItemImages(ctx, rec.ID, imgs); err != nil {
		return false, false, nil, err
	}
	if meta != nil {
		s.downloadAndSaveRemoteImages(ctx, rec, meta)
	}

	// Taxonomies, People & FTS5
	s.persistMetaRelationsAndFTS(ctx, rec, meta)
	if meta != nil && meta.Collection != nil {
		s.ensureAutoBoxSet(ctx, rec, meta.Collection)
	}

	// Local Trailers, Special Features & Theme Songs
	s.scanAndIndexExtras(ctx, lib, rec, mediaDir, mediaBaseName)

	return true, false, task, nil
}

func (s *Scanner) indexEpisode(
	ctx context.Context,
	lib *sqlite.LibraryRecord,
	epInfo ParsedEpisode,
	mediaPath string,
	seriesCache map[string]*sqlite.ItemRecord,
	seasonCache map[string]*sqlite.ItemRecord,
	seenSeriesEnrich map[string]struct{},
	seenSeriesExtras map[string]struct{},
	batch *phaseABatch,
) (bool, bool, []enrichTask, error) {
	info, err := os.Stat(mediaPath)
	if err != nil {
		return false, false, nil, err
	}
	mtime := info.ModTime().Unix()
	size := info.Size()

	isStrm := IsStrmFile(mediaPath)
	container := strings.TrimPrefix(strings.ToLower(filepath.Ext(mediaPath)), ".")
	if isStrm {
		container = "strm"
	}

	prober := s.getProber()
	needProbe := !isStrm && prober.IsEnabled()
	needOnline := s.hasOnlineMetadataProviders()

	existing, err := s.repo.FindItemByMediaSourcePath(ctx, lib.ID, mediaPath)
	if err == nil && existing != nil {
		if s.isMediaSourceUnchanged(ctx, existing, mediaPath, size, mtime) {
			if existing.SeriesID != "" {
				if _, checked := seenSeriesExtras[existing.SeriesID]; !checked {
					seenSeriesExtras[existing.SeriesID] = struct{}{}
					if seriesRec, sErr := s.repo.GetItemRecordByID(ctx, existing.SeriesID); sErr == nil && seriesRec != nil {
						seriesDir := strings.TrimSpace(seriesRec.Path)
						if seriesDir == "" {
							seriesDir = epInfo.SeriesDir
						}
						if seriesDir != "" {
							s.scanAndIndexExtras(ctx, lib, seriesRec, seriesDir, "")
						}
					}
				}
			}
			if needOnline {
				var backfillTasks []enrichTask
				if existing.SeriesID != "" {
					if _, dup := seenSeriesEnrich[existing.SeriesID]; !dup && s.isMissingPrimaryImage(ctx, existing.SeriesID) {
						if seriesRec, sErr := s.repo.GetItemRecordByID(ctx, existing.SeriesID); sErr == nil && seriesRec != nil {
							seenSeriesEnrich[existing.SeriesID] = struct{}{}
							var sYear int
							if seriesRec.ProductionYear != nil {
								sYear = *seriesRec.ProductionYear
							}
							backfillTasks = append(backfillTasks, enrichTask{
								ItemID:    seriesRec.ID,
								LibraryID: lib.ID,
								ItemType:  "Series",
								MediaPath: seriesRec.Path,
								Query: plugins.MediaQuery{
									ItemType:      "Series",
									Title:         seriesRec.Name,
									OriginalTitle: seriesRec.OriginalTitle,
									Year:          sYear,
									Path:          seriesRec.Path,
									ProviderIDs:   cloneStringMap(seriesRec.Extra.ProviderIDs),
									Language:      s.language(),
								},
								NeedOnlineMeta: true,
								HasLocalTitle:  false,
								DisplayName:    seriesRec.Name,
							})
						}
					}
				}
				if existing.SeasonID != "" {
					if _, dup := seenSeriesEnrich[existing.SeasonID]; !dup && s.isMissingPrimaryImage(ctx, existing.SeasonID) {
						if seasonRec, sErr := s.repo.GetItemRecordByID(ctx, existing.SeasonID); sErr == nil && seasonRec != nil {
							seenSeriesEnrich[existing.SeasonID] = struct{}{}
							var sNum int
							if seasonRec.IndexNumber != nil {
								sNum = *seasonRec.IndexNumber
							} else if existing.ParentIndexNumber != nil {
								sNum = *existing.ParentIndexNumber
							}
							backfillTasks = append(backfillTasks, enrichTask{
								ItemID:         seasonRec.ID,
								LibraryID:      lib.ID,
								ItemType:       "Season",
								ParentSeriesID: existing.SeriesID,
								MediaPath:      seasonRec.Path,
								Query: plugins.MediaQuery{
									ItemType:     "Season",
									Title:        seasonRec.Name,
									SeasonNumber: sNum,
									Path:         seasonRec.Path,
									Language:     s.language(),
								},
								NeedOnlineMeta: true,
								HasLocalTitle:  false,
								DisplayName:    seasonRec.Name,
							})
						}
					}
				}
				if s.isMissingPrimaryImage(ctx, existing.ID) {
					var epNum, seasonNum int
					if existing.IndexNumber != nil {
						epNum = *existing.IndexNumber
					}
					if existing.ParentIndexNumber != nil {
						seasonNum = *existing.ParentIndexNumber
					}
					backfillTasks = append(backfillTasks, enrichTask{
						ItemID:         existing.ID,
						LibraryID:      lib.ID,
						ItemType:       "Episode",
						ParentSeriesID: existing.SeriesID,
						MediaPath:      mediaPath,
						Size:           size,
						Mtime:          mtime,
						IsStrm:         isStrm,
						Container:      container,
						Query: plugins.MediaQuery{
							ItemType:      "Episode",
							Title:         existing.Name,
							SeasonNumber:  seasonNum,
							EpisodeNumber: epNum,
							Path:          mediaPath,
							ProviderIDs:   cloneStringMap(existing.Extra.ProviderIDs),
							Language:      s.language(),
						},
						NeedProbe:      false,
						NeedOnlineMeta: true,
						HasLocalTitle:  false,
						DisplayName:    existing.Name,
					})
				}
				if len(backfillTasks) > 0 {
					return false, false, backfillTasks, nil
				}
			}
			return false, false, nil, nil
		}
		if existing.Path != mediaPath {
			ms := s.buildMediaSource(existing, mediaPath, size, mtime, isStrm, container, nil)
			if err := s.repo.UpsertMediaSourceAndStreams(ctx, existing.ID, ms); err != nil {
				return false, false, nil, err
			}
			var tasks []enrichTask
			if needProbe {
				tasks = append(tasks, enrichTask{
					ItemID:           existing.ID,
					LibraryID:        lib.ID,
					ItemType:         "Episode",
					MediaPath:        mediaPath,
					Size:             size,
					Mtime:            mtime,
					IsStrm:           isStrm,
					Container:        container,
					NeedProbe:        true,
					IsAltMediaSource: true,
				})
			}
			return true, false, tasks, nil
		}
	}

	var tasks []enrichTask

	// 1. Ensure Series
	seriesRec, seriesTask, err := s.ensureSeries(ctx, lib, epInfo, seriesCache, seenSeriesEnrich)
	if err != nil {
		return false, false, nil, err
	}
	if seriesRec != nil {
		seenSeriesExtras[seriesRec.ID] = struct{}{}
	}
	if seriesTask != nil {
		tasks = append(tasks, *seriesTask)
	}

	// 2. Ensure Season
	seasonRec, seasonTask, err := s.ensureSeason(ctx, lib, seriesRec, epInfo, seasonCache, seenSeriesEnrich)
	if err != nil {
		return false, false, nil, err
	}
	if seasonTask != nil {
		tasks = append(tasks, *seasonTask)
	}

	// Auto-merge into existing episode if same Series + SeasonNumber + EpisodeNumber already exists.
	if existing == nil {
		if batch != nil {
			if flushErr := batch.flushIfEpisodeCollides(ctx, seriesRec.ID, epInfo.SeasonNumber, epInfo.EpisodeNumber); flushErr != nil && ctx.Err() != nil {
				return false, false, nil, flushErr
			}
		}
		if mergeTarget, err := s.repo.FindEpisodeByNumber(ctx, seriesRec.ID, epInfo.SeasonNumber, epInfo.EpisodeNumber); err == nil && mergeTarget != nil {
			ms := s.buildMediaSource(mergeTarget, mediaPath, size, mtime, isStrm, container, nil)
			if err := s.repo.UpsertMediaSourceAndStreams(ctx, mergeTarget.ID, ms); err != nil {
				return false, false, nil, err
			}
			if needProbe {
				tasks = append(tasks, enrichTask{
					ItemID:           mergeTarget.ID,
					LibraryID:        lib.ID,
					ItemType:         "Episode",
					MediaPath:        mediaPath,
					Size:             size,
					Mtime:            mtime,
					IsStrm:           isStrm,
					Container:        container,
					NeedProbe:        true,
					IsAltMediaSource: true,
				})
			}
			return true, false, tasks, nil
		}
	}

	// 3. Upsert Episode
	query := plugins.MediaQuery{
		ItemType:      "Episode",
		Title:         epInfo.EpisodeTitle,
		SeasonNumber:  epInfo.SeasonNumber,
		EpisodeNumber: epInfo.EpisodeNumber,
		Path:          mediaPath,
		ProviderIDs:   cloneStringMap(seriesRec.Extra.ProviderIDs),
		Language:      s.language(),
	}
	meta := s.fetchMetadataFiltered(ctx, query, true)

	itemID := guid.New()
	if existing != nil {
		itemID = existing.ID
	}

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
	applyMetadataToItem(rec, meta)

	hasLocalTitle := meta != nil && strings.TrimSpace(meta.Name) != ""
	if needProbe || needOnline {
		tasks = append(tasks, enrichTask{
			ItemID:         rec.ID,
			LibraryID:      lib.ID,
			ItemType:       "Episode",
			ParentSeriesID: seriesRec.ID,
			MediaPath:      mediaPath,
			Size:           size,
			Mtime:          mtime,
			IsStrm:         isStrm,
			Container:      container,
			Query:          query,
			NeedProbe:      needProbe,
			NeedOnlineMeta: needOnline,
			HasLocalTitle:  hasLocalTitle,
			DisplayName:    rec.Name,
		})
	}

	ms := s.buildMediaSource(rec, mediaPath, size, mtime, isStrm, container, nil)
	imgs := discoverLocalImages(rec.ID, mediaPath, filepath.Dir(mediaPath), true)

	if batch != nil {
		titleRaw, titleTokens, peopleTokens := buildFTSFields(rec, extractMetaPeopleNames(meta))
		pending := pendingPhaseAItem{
			input: sqlite.BatchItemInput{
				Item:           rec,
				AncestorIDs:    []string{seasonRec.ID, seriesRec.ID, lib.ID},
				FTSTitleRaw:    titleRaw,
				FTSTitleTokens: titleTokens,
				FTSPinyinFull:  "",
				FTSPinyinInit:  "",
				FTSPeopleNames: peopleTokens, // empty for Episode (cast/crew indexed at Series level)
			},
			postHook: func(hookCtx context.Context) error {
				if err := s.repo.UpsertMediaSourceAndStreams(hookCtx, rec.ID, ms); err != nil {
					return err
				}
				if len(imgs) > 0 {
					_ = s.repo.ReplaceItemImages(hookCtx, rec.ID, imgs)
				}
				s.persistMetaRelations(hookCtx, rec, meta)
				return nil
			},
			displayFileName: filepath.Base(mediaPath),
			tasks:           tasks,
		}
		flushErr := batch.addEpisode(ctx, pending, seriesRec.ID, seasonNum, epNum)
		return true, true, nil, flushErr
	}

	if err := s.repo.UpsertItem(ctx, rec); err != nil {
		return false, false, nil, err
	}
	if err := s.repo.ReplaceItemAncestors(ctx, rec.ID, []sqlite.AncestorLink{
		{AncestorID: seasonRec.ID, Depth: 1},
		{AncestorID: seriesRec.ID, Depth: 2},
		{AncestorID: lib.ID, Depth: 3},
	}); err != nil {
		return false, false, nil, err
	}

	if err := s.repo.UpsertMediaSourceAndStreams(ctx, rec.ID, ms); err != nil {
		return false, false, nil, err
	}

	if len(imgs) > 0 {
		_ = s.repo.ReplaceItemImages(ctx, rec.ID, imgs)
	}

	s.persistMetaRelationsAndFTS(ctx, rec, meta)

	return true, false, tasks, nil
}

func (s *Scanner) ensureSeries(
	ctx context.Context,
	lib *sqlite.LibraryRecord,
	epInfo ParsedEpisode,
	cache map[string]*sqlite.ItemRecord,
	seenEnrich map[string]struct{},
) (*sqlite.ItemRecord, *enrichTask, error) {
	if rec, ok := cache[epInfo.SeriesDir]; ok {
		return rec, nil, nil
	}
	existing, err := s.repo.FindItemByPath(ctx, lib.ID, epInfo.SeriesDir)
	if err == nil && existing != nil {
		cache[epInfo.SeriesDir] = existing
		s.scanAndIndexExtras(ctx, lib, existing, epInfo.SeriesDir, "")
		var backfillTask *enrichTask
		if s.hasOnlineMetadataProviders() && s.isMissingPrimaryImage(ctx, existing.ID) {
			if _, dup := seenEnrich[existing.ID]; !dup {
				seenEnrich[existing.ID] = struct{}{}
				var year int
				if existing.ProductionYear != nil {
					year = *existing.ProductionYear
				}
				queryOrig := existing.OriginalTitle
				if queryOrig == "" {
					queryOrig = epInfo.SeriesOriginalTitle
				}
				queryIDs := cloneStringMap(existing.Extra.ProviderIDs)
				for k, v := range epInfo.SeriesProviderIDs {
					if queryIDs == nil {
						queryIDs = make(map[string]string, len(epInfo.SeriesProviderIDs))
					}
					if _, ok := queryIDs[k]; !ok {
						queryIDs[k] = v
					}
				}
				backfillTask = &enrichTask{
					ItemID:    existing.ID,
					LibraryID: lib.ID,
					ItemType:  "Series",
					MediaPath: epInfo.SeriesDir,
					Query: plugins.MediaQuery{
						ItemType:      "Series",
						Title:         existing.Name,
						OriginalTitle: queryOrig,
						Year:          year,
						Path:          epInfo.SeriesDir,
						ProviderIDs:   queryIDs,
						Language:      s.language(),
					},
					NeedOnlineMeta: true,
					HasLocalTitle:  false,
					DisplayName:    existing.Name,
				}
			}
		}
		return existing, backfillTask, nil
	}
	if err != nil && !errors.Is(err, sqlite.ErrNotFound) {
		return nil, nil, err
	}

	query := plugins.MediaQuery{
		ItemType:      "Series",
		Title:         epInfo.SeriesName,
		OriginalTitle: epInfo.SeriesOriginalTitle,
		Year:          epInfo.SeriesYear,
		Path:          epInfo.SeriesDir,
		ProviderIDs:   cloneStringMap(epInfo.SeriesProviderIDs),
		Language:      s.language(),
	}
	meta := s.fetchMetadataFiltered(ctx, query, true)

	// Cross-directory auto-merge for Series sharing the same TMDB/IMDB ID.
	mergeProviderIDs := cloneStringMap(epInfo.SeriesProviderIDs)
	if meta != nil && len(meta.ProviderIDs) > 0 {
		if mergeProviderIDs == nil {
			mergeProviderIDs = make(map[string]string, len(meta.ProviderIDs))
		}
		for k, v := range meta.ProviderIDs {
			mergeProviderIDs[k] = v
		}
	}
	if len(mergeProviderIDs) > 0 {
		var matched *sqlite.ItemRecord
		if tmdbID := getProviderID(mergeProviderIDs, "Tmdb"); tmdbID != "" {
			matched, _ = s.repo.FindItemByProviderID(ctx, lib.ID, "Series", "Tmdb", tmdbID)
		}
		if matched == nil {
			if imdbID := getProviderID(mergeProviderIDs, "Imdb"); imdbID != "" {
				matched, _ = s.repo.FindItemByProviderID(ctx, lib.ID, "Series", "Imdb", imdbID)
			}
		}
		if matched != nil {
			cache[epInfo.SeriesDir] = matched
			return matched, nil, nil
		}
	}

	rec := &sqlite.ItemRecord{
		ID:            guid.New(),
		LibraryID:     lib.ID,
		ParentID:      lib.ID,
		Type:          "Series",
		IsFolder:      true,
		Name:          epInfo.SeriesName,
		OriginalTitle: epInfo.SeriesOriginalTitle,
		SortName:      strings.ToLower(epInfo.SeriesName),
		Path:          epInfo.SeriesDir,
		Etag:          computeEtag(epInfo.SeriesDir, 0, time.Now().Unix()),
	}
	if len(epInfo.SeriesProviderIDs) > 0 {
		rec.Extra.ProviderIDs = cloneStringMap(epInfo.SeriesProviderIDs)
	}
	if epInfo.SeriesYear > 0 {
		y := epInfo.SeriesYear
		rec.ProductionYear = &y
	}
	applyMetadataToItem(rec, meta)

	if err := s.repo.UpsertItem(ctx, rec); err != nil {
		return nil, nil, err
	}
	if err := s.repo.ReplaceItemAncestors(ctx, rec.ID, []sqlite.AncestorLink{
		{AncestorID: lib.ID, Depth: 1},
	}); err != nil {
		return nil, nil, err
	}

	imgs := discoverLocalImages(rec.ID, "", epInfo.SeriesDir, false)
	if len(imgs) > 0 {
		_ = s.repo.ReplaceItemImages(ctx, rec.ID, imgs)
	}
	if meta != nil {
		s.downloadAndSaveRemoteImages(ctx, rec, meta)
	}
	s.persistMetaRelationsAndFTS(ctx, rec, meta)
	s.scanAndIndexExtras(ctx, lib, rec, epInfo.SeriesDir, "")

	cache[epInfo.SeriesDir] = rec

	hasLocalTitle := meta != nil && strings.TrimSpace(meta.Name) != ""

	var task *enrichTask
	if s.hasOnlineMetadataProviders() {
		if _, dup := seenEnrich[rec.ID]; !dup {
			seenEnrich[rec.ID] = struct{}{}
			if len(rec.Extra.ProviderIDs) > 0 {
				query.ProviderIDs = rec.Extra.ProviderIDs
			}
			task = &enrichTask{
				ItemID:         rec.ID,
				LibraryID:      lib.ID,
				ItemType:       "Series",
				MediaPath:      epInfo.SeriesDir,
				Query:          query,
				NeedOnlineMeta: true,
				HasLocalTitle:  hasLocalTitle,
				DisplayName:    rec.Name,
			}
		}
	}
	return rec, task, nil
}

func (s *Scanner) ensureSeason(
	ctx context.Context,
	lib *sqlite.LibraryRecord,
	series *sqlite.ItemRecord,
	epInfo ParsedEpisode,
	cache map[string]*sqlite.ItemRecord,
	seenEnrich map[string]struct{},
) (*sqlite.ItemRecord, *enrichTask, error) {
	key := fmt.Sprintf("%s#%d", series.ID, epInfo.SeasonNumber)
	if rec, ok := cache[key]; ok {
		return rec, nil, nil
	}
	seriesDir := epInfo.SeriesDir
	if seriesDir == "" {
		seriesDir = series.Path
	}
	if existingByNum, err := s.repo.FindSeasonByNumber(ctx, series.ID, epInfo.SeasonNumber); err == nil && existingByNum != nil {
		cache[key] = existingByNum
		if s.isMissingPrimaryImage(ctx, existingByNum.ID) {
			if imgs := discoverSeasonImages(existingByNum.ID, epInfo.SeasonNumber, epInfo.SeasonDir, seriesDir); len(imgs) > 0 {
				_ = s.repo.ReplaceItemImages(ctx, existingByNum.ID, imgs)
			}
		}
		var task *enrichTask
		if s.hasOnlineMetadataProviders() && s.isMissingPrimaryImage(ctx, existingByNum.ID) {
			task = s.maybeBuildSeasonEnrichTask(lib.ID, series, existingByNum, epInfo.SeasonNumber, seenEnrich)
		}
		return existingByNum, task, nil
	}
	seasonPath := epInfo.SeasonDir
	if seasonPath == "" {
		seasonPath = fmt.Sprintf("%s#Season%02d", series.Path, epInfo.SeasonNumber)
	}

	existing, err := s.repo.FindItemByPath(ctx, lib.ID, seasonPath)
	if err == nil && existing != nil {
		cache[key] = existing
		if s.isMissingPrimaryImage(ctx, existing.ID) {
			if imgs := discoverSeasonImages(existing.ID, epInfo.SeasonNumber, epInfo.SeasonDir, seriesDir); len(imgs) > 0 {
				_ = s.repo.ReplaceItemImages(ctx, existing.ID, imgs)
			}
		}
		var task *enrichTask
		if s.hasOnlineMetadataProviders() && s.isMissingPrimaryImage(ctx, existing.ID) {
			task = s.maybeBuildSeasonEnrichTask(lib.ID, series, existing, epInfo.SeasonNumber, seenEnrich)
		}
		return existing, task, nil
	}
	if err != nil && !errors.Is(err, sqlite.ErrNotFound) {
		return nil, nil, err
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
		return nil, nil, err
	}
	if err := s.repo.ReplaceItemAncestors(ctx, rec.ID, []sqlite.AncestorLink{
		{AncestorID: series.ID, Depth: 1},
		{AncestorID: lib.ID, Depth: 2},
	}); err != nil {
		return nil, nil, err
	}
	imgs := discoverSeasonImages(rec.ID, epInfo.SeasonNumber, epInfo.SeasonDir, seriesDir)
	if len(imgs) > 0 {
		_ = s.repo.ReplaceItemImages(ctx, rec.ID, imgs)
	}
	cache[key] = rec

	var task *enrichTask
	if s.hasOnlineMetadataProviders() {
		task = s.maybeBuildSeasonEnrichTask(lib.ID, series, rec, epInfo.SeasonNumber, seenEnrich)
	}
	return rec, task, nil
}

func (s *Scanner) maybeBuildSeasonEnrichTask(
	libraryID string,
	series, season *sqlite.ItemRecord,
	seasonNum int,
	seenEnrich map[string]struct{},
) *enrichTask {
	if _, dup := seenEnrich[season.ID]; dup {
		return nil
	}
	seenEnrich[season.ID] = struct{}{}
	return &enrichTask{
		ItemID:         season.ID,
		LibraryID:      libraryID,
		ItemType:       "Season",
		ParentSeriesID: series.ID,
		MediaPath:      season.Path,
		Query: plugins.MediaQuery{
			ItemType:     "Season",
			Title:        season.Name,
			SeasonNumber: seasonNum,
			Path:         season.Path,
			ProviderIDs:  cloneStringMap(series.Extra.ProviderIDs),
			Language:     s.language(),
		},
		NeedOnlineMeta: true,
		HasLocalTitle:  false,
		DisplayName:    season.Name,
	}
}

func (s *Scanner) probeMedia(ctx context.Context, mediaPath string, isStrm bool) *ProbeResult {
	// Never probe .strm files during scan to avoid triggering cloud-drive rate limits or bans.
	prober := s.getProber()
	if isStrm || !prober.IsEnabled() {
		return nil
	}
	res, err := prober.Probe(ctx, mediaPath)
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
		for _, ps := range probeRes.Streams {
			streams = append(streams, sqlite.MediaStreamRecord{
				MediaSourceID: msID,
				ItemID:        item.ID,
				StreamIndex:   ps.Index,
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
	} else if strings.EqualFold(item.MediaType, "Audio") || strings.EqualFold(item.Type, "Audio") {
		audioCodec := defaultAudioCodecForContainer(container)
		streams = []sqlite.MediaStreamRecord{
			{
				MediaSourceID: msID,
				ItemID:        item.ID,
				StreamIndex:   0,
				StreamType:    "Audio",
				Codec:         audioCodec,
				Language:      "und",
				DisplayTitle:  strings.ToUpper(audioCodec) + " Stereo",
				Channels:      2,
				SampleRate:    44100,
				IsDefault:     true,
			},
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

	// Scan companion external subtitles in the same directory for video items; stream_index continues after max embedded stream index.
	if !strings.EqualFold(item.MediaType, "Audio") && !strings.EqualFold(item.Type, "Audio") {
		nextIdx := 0
		for _, st := range streams {
			if st.StreamIndex >= nextIdx {
				nextIdx = st.StreamIndex + 1
			}
		}
		extSubs := discoverExternalSubtitles(mediaPath, msID, item.ID, nextIdx)
		streams = append(streams, extSubs...)
	}

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
		suffix := strings.TrimPrefix(lowerName, mediaStem)
		if !isValidSubtitleSuffixBoundary(suffix) {
			continue
		}
		cls := ClassifySubtitle(suffix, codec)
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

func isValidSubtitleSuffixBoundary(suffix string) bool {
	if len(suffix) == 0 {
		return false
	}
	switch suffix[0] {
	case '.', '_', '-':
		return true
	case ' ':
		// Reject sequel-like suffixes such as " 2.srt" where the token before extension is purely numeric.
		rest := strings.TrimSpace(strings.TrimSuffix(suffix, filepath.Ext(suffix)))
		if rest == "" {
			return true
		}
		allDigits := true
		for _, r := range rest {
			if r < '0' || r > '9' {
				allDigits = false
				break
			}
		}
		return !allDigits
	default:
		return false
	}
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

// discoverSeasonImages scans both the season subfolder (if distinct from seriesDir)
// and the parent Series directory for Kodi/Emby season artwork naming conventions:
// season{NN}-poster.*, season-specials-poster.*, season{NN}-fanart.*, etc.
func discoverSeasonImages(itemID string, seasonNum int, seasonDir, seriesDir string) []sqlite.ImageRecord {
	var out []sqlite.ImageRecord
	seenType := make(map[string]int)

	if seasonDir != "" && filepath.Clean(seasonDir) != filepath.Clean(seriesDir) {
		for _, img := range discoverLocalImages(itemID, "", seasonDir, false) {
			out = append(out, img)
			if img.ImageIndex+1 > seenType[img.ImageType] {
				seenType[img.ImageType] = img.ImageIndex + 1
			}
		}
	}

	if seriesDir == "" {
		return out
	}
	entries, err := os.ReadDir(seriesDir)
	if err != nil {
		return out
	}

	addImg := func(imgType, fullPath string) {
		info, err := os.Stat(fullPath)
		if err != nil {
			return
		}
		idx := seenType[imgType]
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

	var prefixes []string
	if seasonNum == 0 {
		prefixes = []string{"season-specials", "season00", "season0"}
	} else {
		padded := fmt.Sprintf("season%02d", seasonNum)
		unpadded := fmt.Sprintf("season%d", seasonNum)
		prefixes = []string{padded}
		if unpadded != padded {
			prefixes = append(prefixes, unpadded)
		}
	}

	for _, entry := range entries {
		if entry.IsDir() || !IsImageFile(entry.Name()) {
			continue
		}
		stem := strings.ToLower(strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())))
		fullPath := filepath.Join(seriesDir, entry.Name())
		for _, pfx := range prefixes {
			switch stem {
			case pfx + "-poster":
				addImg("Primary", fullPath)
			case pfx + "-fanart", pfx + "-backdrop":
				addImg("Backdrop", fullPath)
			case pfx + "-landscape", pfx + "-thumb":
				addImg("Thumb", fullPath)
			case pfx + "-banner":
				addImg("Banner", fullPath)
			}
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
	if dst.Collection == nil && src.Collection != nil {
		cp := *src.Collection
		dst.Collection = &cp
	}
	if src.LockData {
		dst.LockData = true
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
	if meta.LockData {
		rec.Extra.LockData = true
	}
}

func mergeUniqueStrings(existing, incoming []string) []string {
	seen := make(map[string]struct{}, len(existing)+len(incoming))
	out := make([]string, 0, len(existing)+len(incoming))
	for _, list := range [][]string{existing, incoming} {
		for _, raw := range list {
			trimmed := strings.TrimSpace(raw)
			if trimmed == "" {
				continue
			}
			key := strings.ToLower(trimmed)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, trimmed)
		}
	}
	return out
}

func (s *Scanner) persistMetaRelations(ctx context.Context, rec *sqlite.ItemRecord, meta *plugins.MetadataResult) []string {
	var peopleNames []string
	if meta != nil {
		if len(meta.Genres) > 0 {
			_ = s.repo.ReplaceItemTaxonomies(ctx, rec.ID, "genre", meta.Genres)
		}
		if len(meta.Studios) > 0 {
			_ = s.repo.ReplaceItemTaxonomies(ctx, rec.ID, "studio", meta.Studios)
		}
		if len(meta.Tags) > 0 {
			// Merge scraped tags with existing tags so ranking tags (e.g. Douban/IMDb Top 250)
			// and user-defined custom tags are preserved instead of overwritten.
			var existingTags []string
			if _, _, curTags, err := s.repo.GetTaxonomiesByItemID(ctx, rec.ID); err == nil {
				existingTags = curTags
			}
			mergedTags := mergeUniqueStrings(existingTags, meta.Tags)
			_ = s.repo.ReplaceItemTaxonomies(ctx, rec.ID, "tag", mergedTags)
		}
		if len(meta.People) > 0 {
			pins := make([]sqlite.PersonInput, len(meta.People))
			hasAnyAvatar := false
			for i, p := range meta.People {
				pins[i] = sqlite.PersonInput{
					Name:      p.Name,
					Role:      p.Role,
					Type:      p.Type,
					SortOrder: i,
				}
				peopleNames = append(peopleNames, p.Name)
				if strings.TrimSpace(p.ImageURL) != "" {
					hasAnyAvatar = true
				}
			}
			if err := s.repo.ReplaceItemPeople(ctx, rec.ID, pins); err == nil && hasAnyAvatar {
				s.downloadPersonAvatars(ctx, rec.ID, meta.People)
			}
		}
	}
	return peopleNames
}

func extractMetaPeopleNames(meta *plugins.MetadataResult) []string {
	if meta == nil || len(meta.People) == 0 {
		return nil
	}
	names := make([]string, 0, len(meta.People))
	for _, p := range meta.People {
		names = append(names, p.Name)
	}
	return names
}

func buildFTSFields(rec *sqlite.ItemRecord, peopleNames []string) (titleRaw, titleTokens, peopleTokens string) {
	titleRaw = strings.TrimSpace(rec.Name + " " + rec.OriginalTitle)
	titleTokens = TokenizeForFTS(rec.Name, rec.OriginalTitle, rec.Extra.SeriesName)
	// Episodes omit peopleNames in item_search_fts because cast/crew are already indexed at the
	// Series level, eliminating massive FTS5 bloat across hundreds of thousands of episodes.
	if !strings.EqualFold(rec.Type, "Episode") && len(peopleNames) > 0 {
		peopleTokens = TokenizeForFTS(peopleNames...)
	}
	return titleRaw, titleTokens, peopleTokens
}

func (s *Scanner) persistMetaRelationsAndFTS(ctx context.Context, rec *sqlite.ItemRecord, meta *plugins.MetadataResult) {
	peopleNames := s.persistMetaRelations(ctx, rec, meta)
	titleRaw, titleTokens, peopleTokens := buildFTSFields(rec, peopleNames)
	_ = s.repo.UpsertSearchFTS(ctx, rec.ID, titleRaw, titleTokens, "", "", peopleTokens)
}

const maxPersonAvatarsPerItem = 12

func (s *Scanner) downloadPersonAvatars(ctx context.Context, itemID string, credits []plugins.PersonCredit) {
	persisted, err := s.repo.GetPeopleByItemID(ctx, itemID)
	if err != nil || len(persisted) == 0 {
		return
	}
	nameToID := make(map[string]string, len(persisted))
	for _, p := range persisted {
		key := strings.ToLower(strings.TrimSpace(p.Name))
		if key != "" && p.Id != "" {
			nameToID[key] = p.Id
		}
	}
	seenPerson := make(map[string]struct{}, maxPersonAvatarsPerItem)
	processed := 0
	for _, c := range credits {
		if processed >= maxPersonAvatarsPerItem {
			break
		}
		imgURL := strings.TrimSpace(c.ImageURL)
		if imgURL == "" {
			continue
		}
		personID := nameToID[strings.ToLower(strings.TrimSpace(c.Name))]
		if personID == "" {
			continue
		}
		if _, dup := seenPerson[personID]; dup {
			continue
		}
		seenPerson[personID] = struct{}{}
		processed++
		s.downloadAndSaveRemoteImage(ctx, personID, "Primary", imgURL)
	}
}

func cloneStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
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
