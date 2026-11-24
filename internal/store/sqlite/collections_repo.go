package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/pkg/guid"
)

// PlaylistItemDto embeds dto.BaseItemDto and attaches the playlist entry identifier.
type PlaylistItemDto struct {
	dto.BaseItemDto
	PlaylistItemId string `json:"PlaylistItemId,omitempty"`
}

// RecommendationCategory mirrors Emby's RecommendationDto for /Movies/Recommendations.
type RecommendationCategory struct {
	Items              []dto.BaseItemDto `json:"Items"`
	RecommendationType string            `json:"RecommendationType"`
	BaselineItemName   string            `json:"BaselineItemName,omitempty"`
	CategoryId         string            `json:"CategoryId"`
}

// CreateCollection creates a new BoxSet item and optionally adds initial child items.
func (r *LibraryRepo) CreateCollection(ctx context.Context, name, parentID string, itemIDs []string) (*ItemRecord, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("collection name is required")
	}
	now := time.Now().Unix()
	rec := &ItemRecord{
		ID:           guid.New(),
		LibraryID:    "collections",
		ParentID:     guid.Normalize(parentID),
		Type:         "BoxSet",
		IsFolder:     true,
		Name:         name,
		SortName:     strings.ToLower(name),
		Etag:         guid.New()[:16],
		DateCreated:  now,
		DateModified: now,
	}
	if err := r.UpsertItem(ctx, rec); err != nil {
		return nil, err
	}
	_ = r.UpsertSearchFTS(ctx, rec.ID, rec.Name, rec.Name, "", "", "")
	if len(itemIDs) > 0 {
		if _, err := r.AddItemsToContainer(ctx, rec.ID, itemIDs, true); err != nil {
			return nil, err
		}
	}
	return rec, nil
}

// CreatePlaylist creates a new Playlist item and optionally populates it with initial items.
func (r *LibraryRepo) CreatePlaylist(ctx context.Context, name, mediaType, userID string, itemIDs []string) (*ItemRecord, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("playlist name is required")
	}
	if mediaType == "" && len(itemIDs) > 0 {
		var mt sql.NullString
		_ = r.store.Reader.QueryRowContext(ctx, `SELECT media_type FROM items WHERE id = ?`, guid.Normalize(itemIDs[0])).Scan(&mt)
		if mt.Valid && mt.String != "" {
			mediaType = mt.String
		}
	}
	if mediaType == "" {
		mediaType = "Audio"
	}
	now := time.Now().Unix()
	rec := &ItemRecord{
		ID:           guid.New(),
		LibraryID:    "playlists",
		ParentID:     guid.Normalize(userID),
		Type:         "Playlist",
		MediaType:    mediaType,
		IsFolder:     true,
		Name:         name,
		SortName:     strings.ToLower(name),
		Etag:         guid.New()[:16],
		DateCreated:  now,
		DateModified: now,
	}
	if err := r.UpsertItem(ctx, rec); err != nil {
		return nil, err
	}
	_ = r.UpsertSearchFTS(ctx, rec.ID, rec.Name, rec.Name, "", "", "")
	if len(itemIDs) > 0 {
		if _, err := r.AddItemsToContainer(ctx, rec.ID, itemIDs, false); err != nil {
			return nil, err
		}
	}
	return rec, nil
}

// DeleteItem deletes an item and all of its descendants (e.g., Series -> Seasons -> Episodes),
// cleaning up item_images, item_search_fts, item_ancestors, and items inside a transaction.
func (r *LibraryRepo) DeleteItem(ctx context.Context, itemID string) error {
	itemID = guid.Normalize(itemID)
	tx, err := r.store.Writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM items WHERE id = ?`, itemID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}

	// Collect itemID and all descendant IDs via parent_id/series_id/season_id and item_ancestors.
	ids := []string{itemID}
	seen := map[string]struct{}{itemID: {}}
	for i := 0; i < len(ids); i++ {
		cur := ids[i]
		rows, err := tx.QueryContext(ctx,
			`SELECT id FROM items WHERE parent_id = ? OR series_id = ? OR season_id = ?
			 UNION
			 SELECT descendant_id FROM item_ancestors WHERE ancestor_id = ?`,
			cur, cur, cur, cur,
		)
		if err != nil {
			return err
		}
		for rows.Next() {
			var childID string
			if err := rows.Scan(&childID); err != nil {
				rows.Close()
				return err
			}
			if _, ok := seen[childID]; !ok {
				seen[childID] = struct{}{}
				ids = append(ids, childID)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
	}

	// Chunk deletes to stay well below SQLite's bind parameter limit.
	const chunkSize = 400
	for start := 0; start < len(ids); start += chunkSize {
		end := start + chunkSize
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[start:end]
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		args := make([]any, len(batch))
		for i, id := range batch {
			args[i] = id
		}

		if _, err := tx.ExecContext(ctx, `DELETE FROM item_images WHERE item_id IN (`+placeholders+`)`, args...); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM item_search_fts WHERE item_id IN (`+placeholders+`)`, args...); err != nil {
			return err
		}
		doubleArgs := make([]any, 0, len(batch)*2)
		doubleArgs = append(doubleArgs, args...)
		doubleArgs = append(doubleArgs, args...)
		if _, err := tx.ExecContext(ctx, `DELETE FROM item_ancestors WHERE ancestor_id IN (`+placeholders+`) OR descendant_id IN (`+placeholders+`)`, doubleArgs...); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM items WHERE id IN (`+placeholders+`)`, args...); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// ListLibraryItemPaths returns a map of media file path -> itemID for all Movie, Episode, and Audio
// items (and their attached media sources) belonging to libraryID.
func (r *LibraryRepo) ListLibraryItemPaths(ctx context.Context, libraryID string) (map[string]string, error) {
	libID := guid.Normalize(libraryID)
	out := make(map[string]string)

	rows, err := r.store.Reader.QueryContext(ctx,
		`SELECT id, path FROM items
		 WHERE library_id = ? AND path != '' AND type IN ('Movie', 'Episode', 'Audio')`,
		libID,
	)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, p string
		if err := rows.Scan(&id, &p); err != nil {
			rows.Close()
			return nil, err
		}
		out[p] = id
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	msRows, err := r.store.Reader.QueryContext(ctx,
		`SELECT ms.item_id, ms.path
		 FROM item_media_sources ms
		 JOIN items i ON i.id = ms.item_id
		 WHERE i.library_id = ? AND ms.path != '' AND i.type IN ('Movie', 'Episode', 'Audio')`,
		libID,
	)
	if err != nil {
		return nil, err
	}
	defer msRows.Close()
	for msRows.Next() {
		var id, p string
		if err := msRows.Scan(&id, &p); err != nil {
			return nil, err
		}
		out[p] = id
	}
	if err := msRows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteItemByPath removes a stale media file from libraryID.
// If the item has multiple media sources, only the matching media source is removed
// (promoting a remaining source's path to items.path if needed).
// Otherwise, the entire item (and its images/FTS/descendants) is deleted.
// Returns true if a media source or item was removed.
func (r *LibraryRepo) DeleteItemByPath(ctx context.Context, libraryID, path string) (bool, error) {
	item, err := r.FindItemByMediaSourcePath(ctx, libraryID, path)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return false, nil
		}
		return false, err
	}

	sources, err := r.GetMediaSourcesByItemID(ctx, item.ID)
	if err != nil {
		return false, err
	}

	if len(sources) > 1 {
		var remaining *MediaSourceRecord
		matched := false
		for i := range sources {
			if sources[i].Path == path {
				matched = true
				continue
			}
			if remaining == nil {
				remaining = &sources[i]
			}
		}
		if matched && remaining != nil {
			tx, err := r.store.Writer.BeginTx(ctx, nil)
			if err != nil {
				return false, err
			}
			defer tx.Rollback()

			if _, err := tx.ExecContext(ctx, `DELETE FROM item_media_sources WHERE item_id = ? AND path = ?`, item.ID, path); err != nil {
				return false, err
			}
			if item.Path == path {
				if _, err := tx.ExecContext(ctx,
					`UPDATE items SET path = ?, container = ?, file_size = ?, date_modified = ? WHERE id = ?`,
					remaining.Path, remaining.Container, remaining.Size, time.Now().Unix(), item.ID,
				); err != nil {
					return false, err
				}
			}
			if err := tx.Commit(); err != nil {
				return false, err
			}
			return true, nil
		}
	}

	if err := r.DeleteItem(ctx, item.ID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// AddItemsToContainer appends itemIDs to a collection or playlist.
// When deduplicate is true, items already present in the container are skipped.
func (r *LibraryRepo) AddItemsToContainer(ctx context.Context, containerID string, itemIDs []string, deduplicate bool) (int, error) {
	containerID = guid.Normalize(containerID)
	var exists int
	if err := r.store.Reader.QueryRowContext(ctx, `SELECT 1 FROM items WHERE id = ?`, containerID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, err
	}

	tx, err := r.store.Writer.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var maxPos int
	_ = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(position), -1) FROM item_collections WHERE container_id = ?`, containerID).Scan(&maxPos)

	existing := make(map[string]bool)
	if deduplicate {
		rows, err := tx.QueryContext(ctx, `SELECT item_id FROM item_collections WHERE container_id = ?`, containerID)
		if err != nil {
			return 0, err
		}
		for rows.Next() {
			var iid string
			if err := rows.Scan(&iid); err != nil {
				rows.Close()
				return 0, err
			}
			existing[iid] = true
		}
		rows.Close()
	}

	added := 0
	for _, rawID := range itemIDs {
		iid := guid.Normalize(rawID)
		if iid == "" || iid == containerID {
			continue
		}
		if deduplicate && existing[iid] {
			continue
		}
		var itemExists int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM items WHERE id = ?`, iid).Scan(&itemExists); err != nil {
			continue
		}
		maxPos++
		entryID := guid.New()
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO item_collections (container_id, item_id, position, entry_id) VALUES (?, ?, ?, ?)`,
			containerID, iid, maxPos, entryID,
		); err != nil {
			return 0, err
		}
		existing[iid] = true
		added++
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return added, nil
}

// RemoveItemsFromContainer removes entries matching either entry_id or item_id and compacts positions.
func (r *LibraryRepo) RemoveItemsFromContainer(ctx context.Context, containerID string, ids []string) (int, error) {
	containerID = guid.Normalize(containerID)
	var exists int
	if err := r.store.Reader.QueryRowContext(ctx, `SELECT 1 FROM items WHERE id = ?`, containerID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}

	tx, err := r.store.Writer.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	removed := 0
	for _, rawID := range ids {
		norm := guid.Normalize(rawID)
		if norm == "" {
			continue
		}
		res, err := tx.ExecContext(ctx,
			`DELETE FROM item_collections WHERE container_id = ? AND (entry_id = ? OR item_id = ?)`,
			containerID, norm, norm,
		)
		if err != nil {
			return 0, err
		}
		n, _ := res.RowsAffected()
		removed += int(n)
	}

	if err := compactContainerPositionsTx(ctx, tx, containerID); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return removed, nil
}

// MoveContainerItem moves an entry (matched by entry_id or item_id) to newIndex within a playlist/collection.
func (r *LibraryRepo) MoveContainerItem(ctx context.Context, containerID, itemOrEntryID string, newIndex int) error {
	containerID = guid.Normalize(containerID)
	target := guid.Normalize(itemOrEntryID)

	tx, err := r.store.Writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	type entryRow struct {
		entryID string
		itemID  string
	}
	rows, err := tx.QueryContext(ctx,
		`SELECT entry_id, item_id FROM item_collections WHERE container_id = ? ORDER BY position ASC`,
		containerID,
	)
	if err != nil {
		return err
	}
	var entries []entryRow
	for rows.Next() {
		var er entryRow
		if err := rows.Scan(&er.entryID, &er.itemID); err != nil {
			rows.Close()
			return err
		}
		entries = append(entries, er)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(entries) == 0 {
		return ErrNotFound
	}

	oldIdx := -1
	for i, e := range entries {
		if e.entryID == target {
			oldIdx = i
			break
		}
	}
	if oldIdx == -1 {
		for i, e := range entries {
			if e.itemID == target {
				oldIdx = i
				break
			}
		}
	}
	if oldIdx == -1 {
		return ErrNotFound
	}

	if newIndex < 0 {
		newIndex = 0
	}
	if newIndex >= len(entries) {
		newIndex = len(entries) - 1
	}
	if oldIdx == newIndex {
		return tx.Commit()
	}

	moved := entries[oldIdx]
	entries = append(entries[:oldIdx], entries[oldIdx+1:]...)
	reordered := make([]entryRow, 0, len(entries)+1)
	reordered = append(reordered, entries[:newIndex]...)
	reordered = append(reordered, moved)
	reordered = append(reordered, entries[newIndex:]...)

	for pos, e := range reordered {
		if _, err := tx.ExecContext(ctx,
			`UPDATE item_collections SET position = ? WHERE container_id = ? AND entry_id = ?`,
			pos, containerID, e.entryID,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func compactContainerPositionsTx(ctx context.Context, tx *sql.Tx, containerID string) error {
	rows, err := tx.QueryContext(ctx,
		`SELECT entry_id FROM item_collections WHERE container_id = ? ORDER BY position ASC`,
		containerID,
	)
	if err != nil {
		return err
	}
	var entryIDs []string
	for rows.Next() {
		var eid string
		if err := rows.Scan(&eid); err != nil {
			rows.Close()
			return err
		}
		entryIDs = append(entryIDs, eid)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for pos, eid := range entryIDs {
		if _, err := tx.ExecContext(ctx,
			`UPDATE item_collections SET position = ? WHERE container_id = ? AND entry_id = ?`,
			pos, containerID, eid,
		); err != nil {
			return err
		}
	}
	return nil
}

// GetContainerItems returns ordered items belonging to a BoxSet or Playlist.
func (r *LibraryRepo) GetContainerItems(ctx context.Context, serverID, userID, containerID string, startIndex, limit int, fields []string) ([]PlaylistItemDto, int, error) {
	containerID = guid.Normalize(containerID)
	var exists int
	if err := r.store.Reader.QueryRowContext(ctx, `SELECT 1 FROM items WHERE id = ?`, containerID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, 0, ErrNotFound
		}
		return nil, 0, err
	}

	var total int
	if err := r.store.Reader.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM item_collections ic JOIN items i ON i.id = ic.item_id WHERE ic.container_id = ?`,
		containerID,
	).Scan(&total); err != nil {
		return nil, 0, err
	}

	uid := guid.Normalize(userID)
	hasUser := uid != ""
	var (
		query string
		args  []any
	)
	if hasUser {
		query = `
			SELECT ic.entry_id,
			       i.id, i.library_id, i.parent_id, i.series_id, i.season_id,
			       i.type, i.media_type, i.is_folder, i.is_strm,
			       i.name, i.original_title, i.sort_name,
			       i.index_number, i.parent_index_number, i.production_year,
			       i.premiere_date, i.end_date, i.community_rating, i.critic_rating, i.official_rating,
			       i.runtime_ticks, i.container, i.path, i.file_size, i.file_mtime,
			       i.etag, i.extra_json, i.date_created, i.date_modified,
			       COALESCE(u.played, 0), COALESCE(u.play_count, 0), COALESCE(u.playback_position_ticks, 0),
			       COALESCE(u.is_favorite, 0), u.last_played_date
			FROM item_collections ic
			JOIN items i ON i.id = ic.item_id
			LEFT JOIN user_item_data u ON u.item_id = i.id AND u.user_id = ?
			WHERE ic.container_id = ?
			ORDER BY ic.position ASC`
		args = append(args, uid, containerID)
	} else {
		query = `
			SELECT ic.entry_id,
			       i.id, i.library_id, i.parent_id, i.series_id, i.season_id,
			       i.type, i.media_type, i.is_folder, i.is_strm,
			       i.name, i.original_title, i.sort_name,
			       i.index_number, i.parent_index_number, i.production_year,
			       i.premiere_date, i.end_date, i.community_rating, i.critic_rating, i.official_rating,
			       i.runtime_ticks, i.container, i.path, i.file_size, i.file_mtime,
			       i.etag, i.extra_json, i.date_created, i.date_modified
			FROM item_collections ic
			JOIN items i ON i.id = ic.item_id
			WHERE ic.container_id = ?
			ORDER BY ic.position ASC`
		args = append(args, containerID)
	}

	if limit > 0 {
		query += ` LIMIT ? OFFSET ?`
		args = append(args, limit, max(0, startIndex))
	} else if startIndex > 0 {
		query += ` LIMIT -1 OFFSET ?`
		args = append(args, startIndex)
	}

	rows, err := r.store.Reader.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var (
		entryIDs []string
		recs     []ItemRecord
		uds      []*dto.UserItemDataDto
	)
	for rows.Next() {
		entryID, rec, ud, err := scanContainerItemRow(rows, hasUser)
		if err != nil {
			return nil, 0, err
		}
		entryIDs = append(entryIDs, entryID)
		recs = append(recs, rec)
		uds = append(uds, ud)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	baseDTOs, err := r.hydrateDTOs(ctx, recs, uds, serverID, userID, fields, false)
	if err != nil {
		return nil, 0, err
	}
	out := make([]PlaylistItemDto, len(baseDTOs))
	for i, d := range baseDTOs {
		out[i] = PlaylistItemDto{
			BaseItemDto:    d,
			PlaylistItemId: entryIDs[i],
		}
	}
	return out, total, nil
}

func scanContainerItemRow(rows *sql.Rows, hasUser bool) (string, ItemRecord, *dto.UserItemDataDto, error) {
	var (
		entryID                                                     string
		rec                                                         ItemRecord
		parentID, seriesID, seasonID, mediaType, origTitle, offRate sql.NullString
		container, path                                             sql.NullString
		isFolder, isStrm                                            int
		extraRaw                                                    string
	)
	if !hasUser {
		err := rows.Scan(
			&entryID,
			&rec.ID, &rec.LibraryID, &parentID, &seriesID, &seasonID,
			&rec.Type, &mediaType, &isFolder, &isStrm,
			&rec.Name, &origTitle, &rec.SortName,
			&rec.IndexNumber, &rec.ParentIndexNumber, &rec.ProductionYear,
			&rec.PremiereDate, &rec.EndDate, &rec.CommunityRating, &rec.CriticRating, &offRate,
			&rec.RuntimeTicks, &container, &path, &rec.FileSize, &rec.FileMtime,
			&rec.Etag, &extraRaw, &rec.DateCreated, &rec.DateModified,
		)
		if err != nil {
			return "", rec, nil, err
		}
		populateScannedItem(&rec, parentID, seriesID, seasonID, mediaType, origTitle, offRate, container, path, isFolder, isStrm, extraRaw)
		return entryID, rec, nil, nil
	}

	var (
		played, playCount, fav int
		pos                    int64
		lastPlayed             sql.NullInt64
	)
	err := rows.Scan(
		&entryID,
		&rec.ID, &rec.LibraryID, &parentID, &seriesID, &seasonID,
		&rec.Type, &mediaType, &isFolder, &isStrm,
		&rec.Name, &origTitle, &rec.SortName,
		&rec.IndexNumber, &rec.ParentIndexNumber, &rec.ProductionYear,
		&rec.PremiereDate, &rec.EndDate, &rec.CommunityRating, &rec.CriticRating, &offRate,
		&rec.RuntimeTicks, &container, &path, &rec.FileSize, &rec.FileMtime,
		&rec.Etag, &extraRaw, &rec.DateCreated, &rec.DateModified,
		&played, &playCount, &pos, &fav, &lastPlayed,
	)
	if err != nil {
		return "", rec, nil, err
	}
	populateScannedItem(&rec, parentID, seriesID, seasonID, mediaType, origTitle, offRate, container, path, isFolder, isStrm, extraRaw)
	ud := &dto.UserItemDataDto{
		PlaybackPositionTicks: pos,
		PlayCount:             playCount,
		IsFavorite:            fav == 1,
		Played:                played == 1,
		Key:                   rec.ID,
	}
	if lastPlayed.Valid && lastPlayed.Int64 > 0 {
		t := time.Unix(lastPlayed.Int64, 0).UTC()
		ud.LastPlayedDate = &t
	}
	return entryID, rec, ud, nil
}

func populateScannedItem(rec *ItemRecord, parentID, seriesID, seasonID, mediaType, origTitle, offRate, container, path sql.NullString, isFolder, isStrm int, extraRaw string) {
	rec.ParentID = parentID.String
	rec.SeriesID = seriesID.String
	rec.SeasonID = seasonID.String
	rec.MediaType = mediaType.String
	rec.OriginalTitle = origTitle.String
	rec.OfficialRating = offRate.String
	rec.Container = container.String
	rec.Path = path.String
	rec.IsFolder = isFolder == 1
	rec.IsStrm = isStrm == 1
	_ = rec.Extra.unmarshal(extraRaw)
}

func (e *ItemExtra) unmarshal(raw string) error {
	if raw == "" || raw == "{}" {
		return nil
	}
	return json.Unmarshal([]byte(raw), e)
}

// GetSimilarItems finds items of the same type sharing genres with seedID, falling back to same-type items.
func (r *LibraryRepo) GetSimilarItems(ctx context.Context, serverID, userID, seedID string, limit int, fields []string) ([]dto.BaseItemDto, int, error) {
	seedID = guid.Normalize(seedID)
	if limit <= 0 {
		limit = 12
	}
	var seedType string
	err := r.store.Reader.QueryRowContext(ctx, `SELECT type FROM items WHERE id = ?`, seedID).Scan(&seedType)
	if errors.Is(err, sql.ErrNoRows) {
		return []dto.BaseItemDto{}, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}

	// Find genres of seed item
	gRows, err := r.store.Reader.QueryContext(ctx, `
		SELECT t.name FROM item_taxonomies it
		JOIN taxonomies t ON t.id = it.taxonomy_id
		WHERE it.item_id = ? AND t.kind = 'genre'`, seedID)
	if err != nil {
		return nil, 0, err
	}
	var genres []string
	for gRows.Next() {
		var g string
		if err := gRows.Scan(&g); err == nil {
			genres = append(genres, g)
		}
	}
	gRows.Close()

	var result []dto.BaseItemDto
	seen := map[string]bool{seedID: true}

	if len(genres) > 0 {
		items, _, err := r.QueryItems(ctx, ItemFilter{
			ServerID:         serverID,
			UserID:           userID,
			IncludeItemTypes: []string{seedType},
			Genres:           genres,
			SortBy:           []string{"CommunityRating", "DateCreated"},
			SortOrder:        "Descending",
			Limit:            limit + 1,
			Fields:           fields,
		})
		if err != nil {
			return nil, 0, err
		}
		for _, it := range items {
			if seen[it.Id] {
				continue
			}
			seen[it.Id] = true
			result = append(result, it)
			if len(result) >= limit {
				break
			}
		}
	}

	if len(result) < limit {
		items, _, err := r.QueryItems(ctx, ItemFilter{
			ServerID:         serverID,
			UserID:           userID,
			IncludeItemTypes: []string{seedType},
			SortBy:           []string{"DateCreated"},
			SortOrder:        "Descending",
			Limit:            limit + len(seen),
			Fields:           fields,
		})
		if err != nil {
			return nil, 0, err
		}
		for _, it := range items {
			if seen[it.Id] {
				continue
			}
			seen[it.Id] = true
			result = append(result, it)
			if len(result) >= limit {
				break
			}
		}
	}
	if result == nil {
		result = []dto.BaseItemDto{}
	}
	return result, len(result), nil
}

// GetInstantMix generates an instant mix list seeded from an item, album, song, or playlist.
func (r *LibraryRepo) GetInstantMix(ctx context.Context, serverID, userID, seedID string, limit int, fields []string) ([]dto.BaseItemDto, int, error) {
	seedID = guid.Normalize(seedID)
	if limit <= 0 {
		limit = 50
	}

	var seedType, seedMediaType string
	err := r.store.Reader.QueryRowContext(ctx,
		`SELECT type, COALESCE(media_type, '') FROM items WHERE id = ?`, seedID,
	).Scan(&seedType, &seedMediaType)
	if errors.Is(err, sql.ErrNoRows) {
		return []dto.BaseItemDto{}, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}

	// Determine target playable item type
	targetTypes := []string{"Audio"}
	if strings.EqualFold(seedMediaType, "Video") || strings.EqualFold(seedType, "Movie") || strings.EqualFold(seedType, "Episode") {
		targetTypes = []string{"Movie", "Episode"}
	} else if strings.EqualFold(seedType, "Playlist") && strings.EqualFold(seedMediaType, "Video") {
		targetTypes = []string{"Movie", "Episode"}
	}

	// Collect seed genres (directly or from playlist/album children)
	var genres []string
	gRows, err := r.store.Reader.QueryContext(ctx, `
		SELECT DISTINCT t.name
		FROM item_taxonomies it
		JOIN taxonomies t ON t.id = it.taxonomy_id
		WHERE t.kind = 'genre'
		  AND (it.item_id = ?
		       OR it.item_id IN (SELECT item_id FROM item_collections WHERE container_id = ?)
		       OR it.item_id IN (SELECT id FROM items WHERE parent_id = ?))`,
		seedID, seedID, seedID,
	)
	if err == nil {
		for gRows.Next() {
			var g string
			if err := gRows.Scan(&g); err == nil {
				genres = append(genres, g)
			}
		}
		gRows.Close()
	}

	seen := map[string]bool{}
	var mix []dto.BaseItemDto

	if len(genres) > 0 {
		items, _, err := r.QueryItems(ctx, ItemFilter{
			ServerID:         serverID,
			UserID:           userID,
			IncludeItemTypes: targetTypes,
			Genres:           genres,
			SortBy:           []string{"Random"},
			Limit:            limit,
			Fields:           fields,
		})
		if err != nil {
			return nil, 0, err
		}
		for _, it := range items {
			if !seen[it.Id] {
				seen[it.Id] = true
				mix = append(mix, it)
			}
		}
	}

	if len(mix) < limit {
		items, _, err := r.QueryItems(ctx, ItemFilter{
			ServerID:         serverID,
			UserID:           userID,
			IncludeItemTypes: targetTypes,
			SortBy:           []string{"Random"},
			Limit:            limit,
			Fields:           fields,
		})
		if err != nil {
			return nil, 0, err
		}
		for _, it := range items {
			if seen[it.Id] {
				continue
			}
			seen[it.Id] = true
			mix = append(mix, it)
			if len(mix) >= limit {
				break
			}
		}
	}
	if mix == nil {
		mix = []dto.BaseItemDto{}
	}
	return mix, len(mix), nil
}

// GetMovieRecommendations returns grouped movie recommendations by top genres and recent additions.
func (r *LibraryRepo) GetMovieRecommendations(ctx context.Context, serverID, userID string, categoryLimit, itemLimit int, fields []string) ([]RecommendationCategory, error) {
	if categoryLimit <= 0 {
		categoryLimit = 5
	}
	if itemLimit <= 0 {
		itemLimit = 8
	}

	// Pick top genres that have movies
	rows, err := r.store.Reader.QueryContext(ctx, `
		SELECT t.id, t.name, COUNT(*) AS cnt
		FROM item_taxonomies it
		JOIN taxonomies t ON t.id = it.taxonomy_id
		JOIN items i ON i.id = it.item_id
		WHERE t.kind = 'genre' AND i.type = 'Movie'
		GROUP BY t.id, t.name
		ORDER BY cnt DESC, t.sort_name ASC
		LIMIT ?`, categoryLimit)
	if err != nil {
		return nil, err
	}
	type genreRow struct {
		id   string
		name string
	}
	var genres []genreRow
	for rows.Next() {
		var g genreRow
		var cnt int
		if err := rows.Scan(&g.id, &g.name, &cnt); err != nil {
			rows.Close()
			return nil, err
		}
		genres = append(genres, g)
	}
	rows.Close()

	out := make([]RecommendationCategory, 0, len(genres)+1)
	for _, g := range genres {
		items, _, err := r.QueryItems(ctx, ItemFilter{
			ServerID:         serverID,
			UserID:           userID,
			IncludeItemTypes: []string{"Movie"},
			Genres:           []string{g.name},
			SortBy:           []string{"CommunityRating", "DateCreated"},
			SortOrder:        "Descending",
			Limit:            itemLimit,
			Fields:           fields,
		})
		if err != nil {
			return nil, err
		}
		if len(items) > 0 {
			out = append(out, RecommendationCategory{
				Items:              items,
				RecommendationType: "SimilarToRecentlyPlayed",
				BaselineItemName:   g.name,
				CategoryId:         g.id,
			})
		}
	}

	if len(out) == 0 {
		items, _, err := r.QueryItems(ctx, ItemFilter{
			ServerID:         serverID,
			UserID:           userID,
			IncludeItemTypes: []string{"Movie"},
			SortBy:           []string{"DateCreated"},
			SortOrder:        "Descending",
			Limit:            itemLimit,
			Fields:           fields,
		})
		if err != nil {
			return nil, err
		}
		if len(items) > 0 {
			out = append(out, RecommendationCategory{
				Items:              items,
				RecommendationType: "SimilarToRecentlyPlayed",
				BaselineItemName:   "Recently Added",
				CategoryId:         "recent-movies",
			})
		}
	}
	return out, nil
}

// ListFilterValues dynamically aggregates distinct filter values (genres, tags, codecs, containers, etc.)
// from SQLite tables and returns them as Emby-compatible BaseItemDto records.
func (r *LibraryRepo) ListFilterValues(
	ctx context.Context,
	serverID, dimension, parentID string,
	includeItemTypes []string,
	startIndex, limit int,
) ([]dto.BaseItemDto, int, error) {
	if limit <= 0 {
		limit = 200
	}
	parentID = guid.Normalize(parentID)

	var (
		query   string
		args    []any
		dtoType = dimension
	)

	itemScopeClause := func(alias string) (string, []any) {
		var conds []string
		var a []any
		if parentID != "" {
			conds = append(conds, fmt.Sprintf(`(%s.library_id = ? OR %s.parent_id = ? OR EXISTS (SELECT 1 FROM item_ancestors ia WHERE ia.ancestor_id = ? AND ia.descendant_id = %s.id))`, alias, alias, alias))
			a = append(a, parentID, parentID, parentID)
		}
		if len(includeItemTypes) > 0 {
			ph := make([]string, len(includeItemTypes))
			for i, t := range includeItemTypes {
				ph[i] = "?"
				a = append(a, t)
			}
			conds = append(conds, fmt.Sprintf(`%s.type IN (%s)`, alias, strings.Join(ph, ",")))
		}
		if len(conds) == 0 {
			return "", nil
		}
		return " AND " + strings.Join(conds, " AND "), a
	}

	switch dimension {
	case "MusicGenre":
		if len(includeItemTypes) == 0 {
			includeItemTypes = []string{"Audio", "MusicAlbum", "MusicArtist", "MusicVideo"}
		}
		scope, sArgs := itemScopeClause("i")
		query = `
			SELECT DISTINCT t.id, t.name
			FROM taxonomies t
			JOIN item_taxonomies it ON it.taxonomy_id = t.id
			JOIN items i ON i.id = it.item_id
			WHERE t.kind IN ('genre', 'musicgenre')` + scope + `
			ORDER BY t.sort_name ASC`
		args = sArgs

	case "GameGenre":
		if len(includeItemTypes) == 0 {
			includeItemTypes = []string{"Game", "GameSystem"}
		}
		scope, sArgs := itemScopeClause("i")
		query = `
			SELECT DISTINCT t.id, t.name
			FROM taxonomies t
			JOIN item_taxonomies it ON it.taxonomy_id = t.id
			JOIN items i ON i.id = it.item_id
			WHERE t.kind IN ('genre', 'gamegenre')` + scope + `
			ORDER BY t.sort_name ASC`
		args = sArgs

	case "Tag":
		scope, sArgs := itemScopeClause("i")
		if scope == "" {
			query = `SELECT id, name FROM taxonomies WHERE kind = 'tag' ORDER BY sort_name ASC`
		} else {
			query = `
				SELECT DISTINCT t.id, t.name
				FROM taxonomies t
				JOIN item_taxonomies it ON it.taxonomy_id = t.id
				JOIN items i ON i.id = it.item_id
				WHERE t.kind = 'tag'` + scope + `
				ORDER BY t.sort_name ASC`
			args = sArgs
		}

	case "OfficialRating":
		scope, sArgs := itemScopeClause("i")
		query = `
			SELECT DISTINCT i.official_rating, i.official_rating
			FROM items i
			WHERE i.official_rating IS NOT NULL AND i.official_rating != ''` + scope + `
			ORDER BY lower(i.official_rating) ASC`
		args = sArgs

	case "ItemType":
		scope, sArgs := itemScopeClause("i")
		query = `
			SELECT DISTINCT i.type, i.type
			FROM items i
			WHERE i.type IS NOT NULL AND i.type != ''` + scope + `
			ORDER BY lower(i.type) ASC`
		args = sArgs

	case "Container":
		scope, sArgs := itemScopeClause("i")
		query = `
			SELECT DISTINCT lower(i.container), lower(i.container)
			FROM items i
			WHERE i.container IS NOT NULL AND i.container != ''` + scope + `
			ORDER BY lower(i.container) ASC`
		args = sArgs

	case "AudioCodec":
		scope, sArgs := itemScopeClause("i")
		query = `
			SELECT DISTINCT lower(s.codec), lower(s.codec)
			FROM item_media_streams s
			JOIN items i ON i.id = s.item_id
			WHERE lower(s.stream_type) = 'audio' AND s.codec IS NOT NULL AND s.codec != ''` + scope + `
			ORDER BY lower(s.codec) ASC`
		args = sArgs

	case "AudioLayout":
		scope, sArgs := itemScopeClause("i")
		query = `
			SELECT DISTINCT s.audio_layout, s.audio_layout
			FROM item_media_streams s
			JOIN items i ON i.id = s.item_id
			WHERE lower(s.stream_type) = 'audio' AND s.audio_layout IS NOT NULL AND s.audio_layout != ''` + scope + `
			ORDER BY lower(s.audio_layout) ASC`
		args = sArgs

	case "VideoCodec":
		scope, sArgs := itemScopeClause("i")
		query = `
			SELECT DISTINCT lower(s.codec), lower(s.codec)
			FROM item_media_streams s
			JOIN items i ON i.id = s.item_id
			WHERE lower(s.stream_type) = 'video' AND s.codec IS NOT NULL AND s.codec != ''` + scope + `
			ORDER BY lower(s.codec) ASC`
		args = sArgs

	case "SubtitleCodec":
		scope, sArgs := itemScopeClause("i")
		query = `
			SELECT DISTINCT lower(s.codec), lower(s.codec)
			FROM item_media_streams s
			JOIN items i ON i.id = s.item_id
			WHERE lower(s.stream_type) = 'subtitle' AND s.codec IS NOT NULL AND s.codec != ''` + scope + `
			ORDER BY lower(s.codec) ASC`
		args = sArgs

	case "StreamLanguage":
		scope, sArgs := itemScopeClause("i")
		query = `
			SELECT DISTINCT lower(s.language), lower(s.language)
			FROM item_media_streams s
			JOIN items i ON i.id = s.item_id
			WHERE s.language IS NOT NULL AND s.language != ''` + scope + `
			ORDER BY lower(s.language) ASC`
		args = sArgs

	case "ExtendedVideoType":
		scope, sArgs := itemScopeClause("i")
		query = `
			SELECT DISTINCT s.video_range, s.video_range
			FROM item_media_streams s
			JOIN items i ON i.id = s.item_id
			WHERE lower(s.stream_type) = 'video' AND s.video_range IS NOT NULL AND s.video_range != ''` + scope + `
			ORDER BY lower(s.video_range) ASC`
		args = sArgs

	default:
		return []dto.BaseItemDto{}, 0, nil
	}

	rows, err := r.store.Reader.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var all []dto.BaseItemDto
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, 0, err
		}
		all = append(all, dto.BaseItemDto{
			Id:                id,
			Name:              name,
			ServerId:          serverID,
			Type:              dtoType,
			ImageTags:         map[string]string{},
			BackdropImageTags: []string{},
		})
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	total := len(all)
	return paginateSlice(all, startIndex, limit), total, nil
}
