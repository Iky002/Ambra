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

	if (itemTypeLower == "season" || itemTypeLower == "episode") && getProviderID(q.ProviderIDs, "tmdb") == "" {
		parentSeriesID := rec.SeriesID
		if parentSeriesID == "" && itemTypeLower == "season" {
			parentSeriesID = rec.ParentID
		}
		if parentSeriesID != "" {
			if parent, pErr := s.repo.GetItemRecordByID(ctx, parentSeriesID); pErr == nil && parent != nil && len(parent.Extra.ProviderIDs) > 0 {
				if q.ProviderIDs == nil {
					q.ProviderIDs = make(map[string]string, len(parent.Extra.ProviderIDs)+1)
				}
				for k, v := range parent.Extra.ProviderIDs {
					if _, exists := q.ProviderIDs[k]; !exists {
						q.ProviderIDs[k] = v
					}
				}
				if tmdbID := getProviderID(parent.Extra.ProviderIDs, "tmdb"); tmdbID != "" {
					q.ProviderIDs["tmdb"] = tmdbID
				}
			}
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
			rec.Extra.ProviderIDs[k] = v
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
