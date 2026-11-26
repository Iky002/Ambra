package scanner

import (
	"context"
	"strings"
	"time"

	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

// RefreshItem refreshes metadata, relations, FTS index, and remote artwork for a single item.
// When replaceMetadata is true, non-locked fields are overwritten even if already populated.
// When replaceImages is true, existing cached remote images are re-downloaded and replaced.
func (s *Scanner) RefreshItem(ctx context.Context, itemID string, replaceMetadata, replaceImages bool) error {
	rec, err := s.repo.GetItemRecordByID(ctx, itemID)
	if err != nil {
		return err
	}
	if rec.Extra.LockData {
		return nil
	}

	q := plugins.MediaQuery{
		ItemType:      rec.Type,
		Title:         rec.Name,
		OriginalTitle: rec.OriginalTitle,
		Path:          rec.Path,
		ProviderIDs:   cloneStringMap(rec.Extra.ProviderIDs),
		Language:      s.language(),
	}
	if rec.ProductionYear != nil {
		q.Year = *rec.ProductionYear
	}

	itemTypeLower := strings.ToLower(rec.Type)
	if itemTypeLower == "movie" && rec.Path != "" && getProviderID(q.ProviderIDs, "tmdb") == "" {
		var rootDir string
		if lib, libErr := s.repo.GetLibrary(ctx, rec.LibraryID); libErr == nil && lib != nil {
			rootDir = findContainingRoot(lib.Paths, rec.Path)
		}
		reparsed := ParseMoviePath(rootDir, rec.Path)
		if reparsed.Title != "" {
			q.Title = reparsed.Title
		}
		if reparsed.OriginalTitle != "" {
			q.OriginalTitle = reparsed.OriginalTitle
		}
		if reparsed.Year > 0 {
			q.Year = reparsed.Year
		}
		for k, v := range reparsed.ProviderIDs {
			if q.ProviderIDs == nil {
				q.ProviderIDs = make(map[string]string)
			}
			if _, exists := q.ProviderIDs[k]; !exists {
				q.ProviderIDs[k] = v
			}
		}
	}
	if itemTypeLower == "season" {
		if rec.IndexNumber != nil {
			q.SeasonNumber = *rec.IndexNumber
		}
	} else if itemTypeLower == "episode" {
		if rec.IndexNumber != nil {
			q.EpisodeNumber = *rec.IndexNumber
		}
		if rec.ParentIndexNumber != nil {
			q.SeasonNumber = *rec.ParentIndexNumber
		}
	}

	if itemTypeLower == "season" || itemTypeLower == "episode" {
		parentSeriesID := rec.SeriesID
		if parentSeriesID == "" && itemTypeLower == "season" {
			parentSeriesID = rec.ParentID
		}
		if parentSeriesID != "" {
			s.applyParentSeriesQueryContext(ctx, parentSeriesID, &q)
		}
	}

	meta := s.fetchMetadataFiltered(ctx, q, false)
	if meta == nil {
		return nil
	}

	applyRefreshMetadataToItem(rec, meta, replaceMetadata)
	rec.DateModified = time.Now().Unix()
	rec.Etag = guid.New()[:16]

	if err := s.repo.UpsertItem(ctx, rec); err != nil {
		return err
	}

	relMeta := filterLockedRelations(rec, meta)
	s.persistMetaRelationsAndFTS(ctx, rec, relMeta)
	s.downloadAndSaveRemoteImagesForce(ctx, rec, meta, replaceImages)
	s.saveLocalNFOIfEnabled(ctx, rec, relMeta, replaceMetadata)
	if strings.EqualFold(rec.Type, "Movie") && meta.Collection != nil {
		s.ensureAutoBoxSet(ctx, rec, meta.Collection)
	}

	return nil
}

func applyRefreshMetadataToItem(rec *sqlite.ItemRecord, meta *plugins.MetadataResult, replaceMetadata bool) {
	if rec == nil || meta == nil {
		return
	}
	if !replaceMetadata {
		applyOnlineMetadataToItem(rec, meta, false)
		return
	}

	if meta.Name != "" && !isFieldLocked(rec, "Name") {
		rec.Name = meta.Name
		rec.SortName = strings.ToLower(meta.Name)
	}
	if meta.OriginalTitle != "" && !isFieldLocked(rec, "OriginalTitle") {
		rec.OriginalTitle = meta.OriginalTitle
	}
	if meta.Overview != "" && !isFieldLocked(rec, "Overview") {
		rec.Extra.Overview = meta.Overview
	}
	if meta.Tagline != "" && !isFieldLocked(rec, "Taglines") {
		rec.Extra.Taglines = []string{meta.Tagline}
	}
	if meta.Year > 0 && !isFieldLocked(rec, "ProductionYear") {
		y := meta.Year
		rec.ProductionYear = &y
	}
	if meta.PremiereDate != "" && !isFieldLocked(rec, "PremiereDate") {
		if t, err := time.Parse("2006-01-02", meta.PremiereDate); err == nil {
			unix := t.Unix()
			rec.PremiereDate = &unix
		}
	}
	if meta.CommunityRating > 0 && !isFieldLocked(rec, "CommunityRating") {
		r := meta.CommunityRating
		rec.CommunityRating = &r
	}
	if meta.OfficialRating != "" && !isFieldLocked(rec, "OfficialRating") {
		rec.OfficialRating = meta.OfficialRating
	}
	if len(meta.ProviderIDs) > 0 && !rec.Extra.LockData {
		if rec.Extra.ProviderIDs == nil {
			rec.Extra.ProviderIDs = make(map[string]string, len(meta.ProviderIDs))
		}
		for k, v := range meta.ProviderIDs {
			val := strings.TrimSpace(v)
			if val == "" {
				continue
			}
			deleteProviderIDCaseInsensitive(rec.Extra.ProviderIDs, k)
			rec.Extra.ProviderIDs[k] = val
		}
	}
	if meta.LockData {
		rec.Extra.LockData = true
	}
}

// applyParentSeriesQueryContext populates a Season or Episode MediaQuery with the parent
// Series's TVDB/TMDB provider IDs (replacing any child-level IDs) and canonical titles.
func (s *Scanner) applyParentSeriesQueryContext(ctx context.Context, parentSeriesID string, q *plugins.MediaQuery) {
	if s.repo == nil || strings.TrimSpace(parentSeriesID) == "" || q == nil {
		return
	}
	parent, pErr := s.repo.GetItemRecordByID(ctx, parentSeriesID)
	if pErr != nil || parent == nil {
		return
	}
	if q.ProviderIDs == nil {
		q.ProviderIDs = make(map[string]string, len(parent.Extra.ProviderIDs)+2)
	}
	// Season/Episode metadata lookups on TMDB and TVDB require the parent Series provider ID,
	// never the individual season/episode's own TVDB ID (which causes 404 on /series/<id>/...).
	deleteProviderIDCaseInsensitive(q.ProviderIDs, "tvdb")
	if tvdbID := getProviderID(parent.Extra.ProviderIDs, "tvdb"); tvdbID != "" {
		q.ProviderIDs["Tvdb"] = tvdbID
	}
	if tmdbID := getProviderID(parent.Extra.ProviderIDs, "tmdb"); tmdbID != "" {
		deleteProviderIDCaseInsensitive(q.ProviderIDs, "tmdb")
		q.ProviderIDs["tmdb"] = tmdbID
	}
	for k, v := range parent.Extra.ProviderIDs {
		if strings.EqualFold(k, "tvdb") || strings.EqualFold(k, "tmdb") {
			continue
		}
		if getProviderID(q.ProviderIDs, k) == "" && strings.TrimSpace(v) != "" {
			q.ProviderIDs[k] = strings.TrimSpace(v)
		}
	}
	if strings.TrimSpace(parent.Name) != "" {
		q.Title = parent.Name
	}
	if strings.TrimSpace(parent.OriginalTitle) != "" {
		q.OriginalTitle = parent.OriginalTitle
	}
}

func deleteProviderIDCaseInsensitive(ids map[string]string, key string) {
	for k := range ids {
		if strings.EqualFold(k, key) {
			delete(ids, k)
		}
	}
}

func filterLockedRelations(rec *sqlite.ItemRecord, meta *plugins.MetadataResult) *plugins.MetadataResult {
	if rec == nil || meta == nil {
		return meta
	}
	cp := *meta
	if isFieldLocked(rec, "Genres") {
		cp.Genres = nil
	}
	if isFieldLocked(rec, "Studios") {
		cp.Studios = nil
	}
	if isFieldLocked(rec, "Tags") {
		cp.Tags = nil
	}
	if isFieldLocked(rec, "Cast") || isFieldLocked(rec, "People") {
		cp.People = nil
	}
	return &cp
}
