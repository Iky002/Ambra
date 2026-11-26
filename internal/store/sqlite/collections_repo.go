package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
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

func (r *LibraryRepo) upsertContainerAncestors(ctx context.Context, rec *ItemRecord) error {
	if rec == nil || rec.ID == "" {
		return nil
	}
	seen := make(map[string]struct{}, 4)
	ancestors := make([]AncestorLink, 0, 4)
	depth := 0
	addAnc := func(rawID string) {
		aid := guid.Normalize(rawID)
		if aid == "" {
			return
		}
		if _, dup := seen[aid]; dup {
			return
		}
		seen[aid] = struct{}{}
		ancestors = append(ancestors, AncestorLink{AncestorID: aid, Depth: depth})
		depth++
	}
	addAnc(rec.ID)
	if rec.ParentID != "" {
		addAnc(rec.ParentID)
		rows, err := r.store.Reader.QueryContext(ctx,
			`SELECT ancestor_id FROM item_ancestors WHERE descendant_id = ? ORDER BY depth ASC`,
			guid.Normalize(rec.ParentID),
		)
		if err == nil {
			for rows.Next() {
				var aid string
				if rows.Scan(&aid) == nil {
					addAnc(aid)
				}
			}
			rows.Close()
		}
	}
	addAnc(rec.LibraryID)
	return r.ReplaceItemAncestors(ctx, rec.ID, ancestors)
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
	if err := r.upsertContainerAncestors(ctx, rec); err != nil {
		return nil, err
	}
	_ = r.UpsertSearchFTS(ctx, rec.ID, rec.Name, "", "", "", "")
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
	if err := r.upsertContainerAncestors(ctx, rec); err != nil {
		return nil, err
	}
	_ = r.UpsertSearchFTS(ctx, rec.ID, rec.Name, "", "", "", "")
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

	if _, err := deleteItemIDsTx(ctx, tx, []string{itemID}); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	r.InvalidateCountCache()
	return nil
}

// DeleteItemsByPathPrefix removes all items in libraryID whose path equals dirPath or lies under dirPath,
// along with their descendants and associated index/image/ancestor records, inside a single write transaction.
func (r *LibraryRepo) DeleteItemsByPathPrefix(ctx context.Context, libraryID, dirPath string) (int64, error) {
	libID := guid.Normalize(libraryID)
	trimmedDir := strings.TrimSpace(dirPath)
	if libID == "" || trimmedDir == "" {
		return 0, nil
	}

	cleanDir := filepath.Clean(trimmedDir)
	slashDir := filepath.ToSlash(cleanDir)
	osPrefix := cleanDir
	if !strings.HasSuffix(osPrefix, string(filepath.Separator)) {
		osPrefix += string(filepath.Separator)
	}
	slashPrefix := slashDir
	if !strings.HasSuffix(slashPrefix, "/") {
		slashPrefix += "/"
	}

	tx, err := r.store.Writer.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
		SELECT id FROM items
		WHERE library_id = ?
		  AND (path = ? OR path = ? OR path LIKE ? ESCAPE '\' OR path LIKE ? ESCAPE '\')`,
		libID, cleanDir, slashDir,
		escapeLikePattern(osPrefix)+"%",
		escapeLikePattern(slashPrefix)+"%",
	)
	if err != nil {
		return 0, err
	}
	var seedIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		seedIDs = append(seedIDs, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	if len(seedIDs) == 0 {
		return 0, nil
	}

	deleted, err := deleteItemIDsTx(ctx, tx, seedIDs)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	if deleted > 0 {
		r.InvalidateCountCache()
	}
	return deleted, nil
}

func deleteItemIDsTx(ctx context.Context, tx *sql.Tx, seedIDs []string) (int64, error) {
	seen := make(map[string]struct{}, len(seedIDs))
	ids := make([]string, 0, len(seedIDs))
	for _, raw := range seedIDs {
		id := guid.Normalize(raw)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return 0, nil
	}

	// Collect all descendant IDs via parent_id/series_id/season_id and item_ancestors.
	for i := 0; i < len(ids); i++ {
		cur := ids[i]
		rows, err := tx.QueryContext(ctx,
			`SELECT id FROM items WHERE parent_id = ? OR series_id = ? OR season_id = ?
			 UNION
			 SELECT descendant_id FROM item_ancestors WHERE ancestor_id = ?`,
			cur, cur, cur, cur,
		)
		if err != nil {
			return 0, err
		}
		for rows.Next() {
			var childID string
			if err := rows.Scan(&childID); err != nil {
				rows.Close()
				return 0, err
			}
			if _, ok := seen[childID]; !ok {
				seen[childID] = struct{}{}
				ids = append(ids, childID)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return 0, err
		}
		rows.Close()
	}

	// Chunk deletes to stay well below SQLite's bind parameter limit.
	const chunkSize = 400
	var deleted int64
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
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM item_search_fts WHERE item_id IN (`+placeholders+`)`, args...); err != nil {
			return 0, err
		}
		doubleArgs := make([]any, 0, len(batch)*2)
		doubleArgs = append(doubleArgs, args...)
		doubleArgs = append(doubleArgs, args...)
		if _, err := tx.ExecContext(ctx, `DELETE FROM item_ancestors WHERE ancestor_id IN (`+placeholders+`) OR descendant_id IN (`+placeholders+`)`, doubleArgs...); err != nil {
			return 0, err
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM items WHERE id IN (`+placeholders+`)`, args...)
		if err != nil {
			return 0, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			deleted += n
		}
	}
	return deleted, nil
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
// (promoting a remaining source's path, container, file_size, is_strm, etag, and runtime_ticks
// to items in a single transaction if needed).
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

	tx, err := r.store.Writer.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	txActive := true
	defer func() {
		if txActive {
			_ = tx.Rollback()
		}
	}()

	type srcRow struct {
		path         string
		container    string
		size         int64
		runtimeTicks int64
		isRemote     bool
	}
	sRows, err := tx.QueryContext(ctx, `
		SELECT path, COALESCE(container, ''), COALESCE(size, 0), COALESCE(runtime_ticks, 0), is_remote
		FROM item_media_sources
		WHERE item_id = ?
		ORDER BY rowid ASC`, item.ID)
	if err != nil {
		return false, err
	}
	var sources []srcRow
	for sRows.Next() {
		var sr srcRow
		var isRem int
		if err := sRows.Scan(&sr.path, &sr.container, &sr.size, &sr.runtimeTicks, &isRem); err != nil {
			sRows.Close()
			return false, err
		}
		sr.isRemote = isRem == 1
		sources = append(sources, sr)
	}
	if err := sRows.Err(); err != nil {
		sRows.Close()
		return false, err
	}
	sRows.Close()

	if len(sources) > 1 {
		var remaining *srcRow
		matched := false
		for i := range sources {
			if sources[i].path == path {
				matched = true
				continue
			}
			if remaining == nil {
				remaining = &sources[i]
			}
		}
		if matched && remaining != nil {
			if _, err := tx.ExecContext(ctx, `DELETE FROM item_media_sources WHERE item_id = ? AND path = ?`, item.ID, path); err != nil {
				return false, err
			}
			now := time.Now().Unix()
			newEtag := guid.New()[:16]
			if item.Path == path {
				isStrm := remaining.isRemote || strings.EqualFold(remaining.container, "strm")
				if _, err := tx.ExecContext(ctx, `
					UPDATE items
					SET path = ?,
					    container = ?,
					    file_size = ?,
					    is_strm = ?,
					    etag = ?,
					    runtime_ticks = CASE WHEN ? > 0 THEN ? ELSE runtime_ticks END,
					    date_modified = ?
					WHERE id = ?`,
					remaining.path, nullStr(remaining.container), remaining.size,
					boolToInt(isStrm), newEtag, remaining.runtimeTicks, remaining.runtimeTicks,
					now, item.ID,
				); err != nil {
					return false, err
				}
			} else {
				if _, err := tx.ExecContext(ctx,
					`UPDATE items SET etag = ?, date_modified = ? WHERE id = ?`,
					newEtag, now, item.ID,
				); err != nil {
					return false, err
				}
			}
			if err := tx.Commit(); err != nil {
				return false, err
			}
			txActive = false
			return true, nil
		}
	}

	// Release single-writer transaction before calling DeleteItem.
	_ = tx.Rollback()
	txActive = false

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
		if err := rows.Err(); err != nil {
			rows.Close()
			return 0, err
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
	if err := tx.Commit(); err != nil {
		return err
	}
	r.InvalidateCountCache()
	return nil
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
	return r.GetContainerItemsWithPolicy(ctx, serverID, userID, containerID, startIndex, limit, fields, nil, nil, nil)
}

// GetContainerItemsWithPolicy returns ordered items belonging to a BoxSet or Playlist
// while enforcing library RBAC (allowedLibraryIDs) and parental controls (maxParentalRating, blockedTags).
func (r *LibraryRepo) GetContainerItemsWithPolicy(
	ctx context.Context,
	serverID, userID, containerID string,
	startIndex, limit int,
	fields []string,
	allowedLibraryIDs []string,
	maxParentalRating *int,
	blockedTags []string,
) ([]PlaylistItemDto, int, error) {
	containerID = guid.Normalize(containerID)
	var exists int
	if err := r.store.Reader.QueryRowContext(ctx, `SELECT 1 FROM items WHERE id = ?`, containerID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, 0, ErrNotFound
		}
		return nil, 0, err
	}
	if allowedLibraryIDs != nil && len(allowedLibraryIDs) == 0 {
		return []PlaylistItemDto{}, 0, nil
	}

	var (
		policyConds []string
		policyArgs  []any
	)
	if len(allowedLibraryIDs) > 0 {
		ph := make([]string, len(allowedLibraryIDs))
		for i, lid := range allowedLibraryIDs {
			ph[i] = "?"
			policyArgs = append(policyArgs, guid.Normalize(lid))
		}
		policyConds = append(policyConds, fmt.Sprintf(`i.library_id IN (%s)`, strings.Join(ph, ",")))
	}
	if pClauses, pArgs := appendParentalFilterSQL("i", maxParentalRating, blockedTags); len(pClauses) > 0 {
		policyConds = append(policyConds, pClauses...)
		policyArgs = append(policyArgs, pArgs...)
	}
	policySQL := ""
	if len(policyConds) > 0 {
		policySQL = " AND " + strings.Join(policyConds, " AND ")
	}

	countArgs := make([]any, 0, 1+len(policyArgs))
	countArgs = append(countArgs, containerID)
	countArgs = append(countArgs, policyArgs...)
	var total int
	if err := r.store.Reader.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM item_collections ic JOIN items i ON i.id = ic.item_id WHERE ic.container_id = ?`+policySQL,
		countArgs...,
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
			WHERE ic.container_id = ?` + policySQL + `
			ORDER BY ic.position ASC`
		args = append(args, uid, containerID)
		args = append(args, policyArgs...)
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
			WHERE ic.container_id = ?` + policySQL + `
			ORDER BY ic.position ASC`
		args = append(args, containerID)
		args = append(args, policyArgs...)
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
	return r.GetSimilarItemsWithPolicy(ctx, serverID, userID, seedID, limit, fields, nil, nil, nil)
}

func (r *LibraryRepo) isSeedAccessibleUnderPolicy(
	ctx context.Context,
	seedRec *ItemRecord,
	allowedLibraryIDs []string,
	maxParentalRating *int,
	blockedTags []string,
) (bool, error) {
	if seedRec == nil {
		return false, nil
	}
	if len(allowedLibraryIDs) > 0 && seedRec.LibraryID != "" &&
		seedRec.LibraryID != "collections" && seedRec.LibraryID != "playlists" {
		normLib := guid.Normalize(seedRec.LibraryID)
		found := false
		for _, lid := range allowedLibraryIDs {
			if guid.Normalize(lid) == normLib {
				found = true
				break
			}
		}
		if !found {
			return false, nil
		}
	}
	blocked, err := r.IsItemBlockedByParentalPolicy(ctx, seedRec, maxParentalRating, blockedTags)
	if err != nil {
		return false, err
	}
	return !blocked, nil
}

// GetSimilarItemsWithPolicy finds similar items while enforcing library RBAC and parental policy.
func (r *LibraryRepo) GetSimilarItemsWithPolicy(
	ctx context.Context,
	serverID, userID, seedID string,
	limit int,
	fields []string,
	allowedLibraryIDs []string,
	maxParentalRating *int,
	blockedTags []string,
) ([]dto.BaseItemDto, int, error) {
	if allowedLibraryIDs != nil && len(allowedLibraryIDs) == 0 {
		return []dto.BaseItemDto{}, 0, nil
	}
	seedID = guid.Normalize(seedID)
	if limit <= 0 {
		limit = 12
	}
	seedRec, err := r.GetItemRecordByID(ctx, seedID)
	if errors.Is(err, ErrNotFound) {
		return []dto.BaseItemDto{}, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	accessible, err := r.isSeedAccessibleUnderPolicy(ctx, seedRec, allowedLibraryIDs, maxParentalRating, blockedTags)
	if err != nil {
		return nil, 0, err
	}
	if !accessible {
		return []dto.BaseItemDto{}, 0, nil
	}
	seedType := seedRec.Type

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
		if err := gRows.Scan(&g); err != nil {
			gRows.Close()
			return nil, 0, err
		}
		genres = append(genres, g)
	}
	if err := gRows.Err(); err != nil {
		gRows.Close()
		return nil, 0, err
	}
	gRows.Close()

	var result []dto.BaseItemDto
	seen := map[string]bool{seedID: true}

	if len(genres) > 0 {
		items, _, err := r.QueryItems(ctx, ItemFilter{
			ServerID:          serverID,
			UserID:            userID,
			AllowedLibraryIDs: allowedLibraryIDs,
			MaxParentalRating: maxParentalRating,
			BlockedTags:       blockedTags,
			IncludeItemTypes:  []string{seedType},
			Genres:            genres,
			SortBy:            []string{"CommunityRating", "DateCreated"},
			SortOrder:         "Descending",
			Limit:             limit + 1,
			Fields:            fields,
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
			ServerID:          serverID,
			UserID:            userID,
			AllowedLibraryIDs: allowedLibraryIDs,
			MaxParentalRating: maxParentalRating,
			BlockedTags:       blockedTags,
			IncludeItemTypes:  []string{seedType},
			SortBy:            []string{"DateCreated"},
			SortOrder:         "Descending",
			Limit:             limit + len(seen),
			Fields:            fields,
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
	return r.GetInstantMixWithPolicy(ctx, serverID, userID, seedID, limit, fields, nil, nil, nil)
}

// GetInstantMixWithPolicy generates an instant mix while enforcing library RBAC and parental policy.
func (r *LibraryRepo) GetInstantMixWithPolicy(
	ctx context.Context,
	serverID, userID, seedID string,
	limit int,
	fields []string,
	allowedLibraryIDs []string,
	maxParentalRating *int,
	blockedTags []string,
) ([]dto.BaseItemDto, int, error) {
	if allowedLibraryIDs != nil && len(allowedLibraryIDs) == 0 {
		return []dto.BaseItemDto{}, 0, nil
	}
	seedID = guid.Normalize(seedID)
	if limit <= 0 {
		limit = 50
	}

	seedRec, err := r.GetItemRecordByID(ctx, seedID)
	if errors.Is(err, ErrNotFound) {
		return []dto.BaseItemDto{}, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	accessible, err := r.isSeedAccessibleUnderPolicy(ctx, seedRec, allowedLibraryIDs, maxParentalRating, blockedTags)
	if err != nil {
		return nil, 0, err
	}
	if !accessible {
		return []dto.BaseItemDto{}, 0, nil
	}
	seedType := seedRec.Type
	seedMediaType := seedRec.MediaType

	// Determine target playable item type
	targetTypes := []string{"Audio"}
	if strings.EqualFold(seedMediaType, "Video") || strings.EqualFold(seedType, "Movie") || strings.EqualFold(seedType, "Episode") {
		targetTypes = []string{"Movie", "Episode"}
	} else if strings.EqualFold(seedType, "Playlist") && strings.EqualFold(seedMediaType, "Video") {
		targetTypes = []string{"Movie", "Episode"}
	}

	// Collect seed genres (directly or from accessible playlist/album children)
	var (
		genreConds []string
		genreArgs  = []any{seedID, seedID, seedID}
	)
	if len(allowedLibraryIDs) > 0 {
		ph := make([]string, len(allowedLibraryIDs))
		for i, lid := range allowedLibraryIDs {
			ph[i] = "?"
			genreArgs = append(genreArgs, guid.Normalize(lid))
		}
		genreConds = append(genreConds, fmt.Sprintf(`(i.library_id IN (%s) OR i.library_id IN ('', 'collections', 'playlists'))`, strings.Join(ph, ",")))
	}
	if pClauses, pArgs := appendParentalFilterSQL("i", maxParentalRating, blockedTags); len(pClauses) > 0 {
		genreConds = append(genreConds, pClauses...)
		genreArgs = append(genreArgs, pArgs...)
	}
	genreScopeSQL := ""
	if len(genreConds) > 0 {
		genreScopeSQL = " AND " + strings.Join(genreConds, " AND ")
	}

	var genres []string
	gRows, err := r.store.Reader.QueryContext(ctx, `
		SELECT DISTINCT t.name
		FROM item_taxonomies it
		JOIN taxonomies t ON t.id = it.taxonomy_id
		JOIN items i ON i.id = it.item_id
		WHERE t.kind = 'genre'
		  AND (it.item_id = ?
		       OR it.item_id IN (SELECT item_id FROM item_collections WHERE container_id = ?)
		       OR it.item_id IN (SELECT id FROM items WHERE parent_id = ?))`+genreScopeSQL,
		genreArgs...,
	)
	if err != nil {
		return nil, 0, err
	}
	for gRows.Next() {
		var g string
		if err := gRows.Scan(&g); err != nil {
			gRows.Close()
			return nil, 0, err
		}
		genres = append(genres, g)
	}
	if err := gRows.Err(); err != nil {
		gRows.Close()
		return nil, 0, err
	}
	gRows.Close()

	seen := map[string]bool{}
	var mix []dto.BaseItemDto

	if len(genres) > 0 {
		items, _, err := r.QueryItems(ctx, ItemFilter{
			ServerID:          serverID,
			UserID:            userID,
			AllowedLibraryIDs: allowedLibraryIDs,
			MaxParentalRating: maxParentalRating,
			BlockedTags:       blockedTags,
			IncludeItemTypes:  targetTypes,
			Genres:            genres,
			SortBy:            []string{"Random"},
			Limit:             limit,
			Fields:            fields,
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
			ServerID:          serverID,
			UserID:            userID,
			AllowedLibraryIDs: allowedLibraryIDs,
			MaxParentalRating: maxParentalRating,
			BlockedTags:       blockedTags,
			IncludeItemTypes:  targetTypes,
			SortBy:            []string{"Random"},
			Limit:             limit,
			Fields:            fields,
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

// GetMovieRecommendations returns grouped movie recommendations by user history, top genres, and recent additions.
func (r *LibraryRepo) GetMovieRecommendations(ctx context.Context, serverID, userID string, categoryLimit, itemLimit int, fields []string) ([]RecommendationCategory, error) {
	return r.GetMovieRecommendationsWithPolicy(ctx, serverID, userID, categoryLimit, itemLimit, fields, nil, nil, nil)
}

// GetMovieRecommendationsWithPolicy returns personalized movie recommendations when userID has played/favorited movies,
// falling back to top library genres and recent additions, while enforcing library RBAC and parental policy.
func (r *LibraryRepo) GetMovieRecommendationsWithPolicy(
	ctx context.Context,
	serverID, userID string,
	categoryLimit, itemLimit int,
	fields []string,
	allowedLibraryIDs []string,
	maxParentalRating *int,
	blockedTags []string,
) ([]RecommendationCategory, error) {
	if allowedLibraryIDs != nil && len(allowedLibraryIDs) == 0 {
		return []RecommendationCategory{}, nil
	}
	if categoryLimit <= 0 {
		categoryLimit = 5
	}
	if itemLimit <= 0 {
		itemLimit = 8
	}

	buildPolicyScope := func(alias string) (string, []any) {
		var conds []string
		var a []any
		if len(allowedLibraryIDs) > 0 {
			ph := make([]string, len(allowedLibraryIDs))
			for i, lid := range allowedLibraryIDs {
				ph[i] = "?"
				a = append(a, guid.Normalize(lid))
			}
			conds = append(conds, fmt.Sprintf(`%s.library_id IN (%s)`, alias, strings.Join(ph, ",")))
		}
		if pClauses, pArgs := appendParentalFilterSQL(alias, maxParentalRating, blockedTags); len(pClauses) > 0 {
			conds = append(conds, pClauses...)
			a = append(a, pArgs...)
		}
		if len(conds) == 0 {
			return "", nil
		}
		return " AND " + strings.Join(conds, " AND "), a
	}

	out := make([]RecommendationCategory, 0, categoryLimit)
	coveredGenres := make(map[string]bool)

	// 1. Personalized recommendations based on the user's recently played / favorited movies.
	uid := guid.Normalize(userID)
	if uid != "" {
		scopeSQL, scopeArgs := buildPolicyScope("i")
		seedArgs := make([]any, 0, len(scopeArgs)+2)
		seedArgs = append(seedArgs, uid)
		seedArgs = append(seedArgs, scopeArgs...)
		seedArgs = append(seedArgs, categoryLimit*2)

		seedRows, err := r.store.Reader.QueryContext(ctx, `
			SELECT i.id, i.name, COALESCE(u.is_favorite, 0)
			FROM user_item_data u
			JOIN items i ON i.id = u.item_id
			WHERE u.user_id = ?
			  AND i.type = 'Movie'
			  AND i.extra_type = ''
			  AND (COALESCE(u.played, 0) = 1 OR COALESCE(u.is_favorite, 0) = 1)`+scopeSQL+`
			ORDER BY COALESCE(u.is_favorite, 0) DESC, COALESCE(u.last_played_date, 0) DESC, i.date_modified DESC
			LIMIT ?`, seedArgs...)
		if err != nil {
			return nil, err
		}
		type seedMovie struct {
			id    string
			name  string
			isFav bool
		}
		var seeds []seedMovie
		for seedRows.Next() {
			var sm seedMovie
			var favInt int
			if err := seedRows.Scan(&sm.id, &sm.name, &favInt); err != nil {
				seedRows.Close()
				return nil, err
			}
			sm.isFav = favInt == 1
			seeds = append(seeds, sm)
		}
		if err := seedRows.Err(); err != nil {
			seedRows.Close()
			return nil, err
		}
		seedRows.Close()

		for _, seed := range seeds {
			if len(out) >= categoryLimit {
				break
			}
			gRows, err := r.store.Reader.QueryContext(ctx, `
				SELECT t.name FROM item_taxonomies it
				JOIN taxonomies t ON t.id = it.taxonomy_id
				WHERE it.item_id = ? AND t.kind = 'genre'
				ORDER BY t.sort_name ASC`, seed.id)
			if err != nil {
				return nil, err
			}
			var seedGenres []string
			for gRows.Next() {
				var g string
				if err := gRows.Scan(&g); err != nil {
					gRows.Close()
					return nil, err
				}
				if g != "" {
					seedGenres = append(seedGenres, g)
				}
			}
			if err := gRows.Err(); err != nil {
				gRows.Close()
				return nil, err
			}
			gRows.Close()
			if len(seedGenres) == 0 {
				continue
			}

			items, _, err := r.QueryItems(ctx, ItemFilter{
				ServerID:          serverID,
				UserID:            userID,
				AllowedLibraryIDs: allowedLibraryIDs,
				MaxParentalRating: maxParentalRating,
				BlockedTags:       blockedTags,
				IncludeItemTypes:  []string{"Movie"},
				ExcludeItemIDs:    []string{seed.id},
				Genres:            seedGenres,
				SortBy:            []string{"CommunityRating", "DateCreated"},
				SortOrder:         "Descending",
				Limit:             itemLimit,
				Fields:            fields,
			})
			if err != nil {
				return nil, err
			}
			if len(items) == 0 {
				continue
			}
			for _, g := range seedGenres {
				coveredGenres[strings.ToLower(g)] = true
			}
			recType := "SimilarToRecentlyPlayed"
			if seed.isFav {
				recType = "SimilarToLikedItem"
			}
			out = append(out, RecommendationCategory{
				Items:              items,
				RecommendationType: recType,
				BaselineItemName:   seed.name,
				CategoryId:         seed.id,
			})
		}
	}

	// 2. Fall back to top library genres for any remaining category slots.
	if len(out) < categoryLimit {
		scopeSQL, scopeArgs := buildPolicyScope("i")
		genreArgs := append(append([]any(nil), scopeArgs...), categoryLimit+len(coveredGenres))
		rows, err := r.store.Reader.QueryContext(ctx, `
			SELECT t.id, t.name, COUNT(*) AS cnt
			FROM item_taxonomies it
			JOIN taxonomies t ON t.id = it.taxonomy_id
			JOIN items i ON i.id = it.item_id
			WHERE t.kind = 'genre'
			  AND i.type = 'Movie'
			  AND i.extra_type = ''`+scopeSQL+`
			GROUP BY t.id, t.name
			ORDER BY cnt DESC, t.sort_name ASC
			LIMIT ?`, genreArgs...)
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
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()

		for _, g := range genres {
			if len(out) >= categoryLimit {
				break
			}
			if coveredGenres[strings.ToLower(g.name)] {
				continue
			}
			items, _, err := r.QueryItems(ctx, ItemFilter{
				ServerID:          serverID,
				UserID:            userID,
				AllowedLibraryIDs: allowedLibraryIDs,
				MaxParentalRating: maxParentalRating,
				BlockedTags:       blockedTags,
				IncludeItemTypes:  []string{"Movie"},
				Genres:            []string{g.name},
				SortBy:            []string{"CommunityRating", "DateCreated"},
				SortOrder:         "Descending",
				Limit:             itemLimit,
				Fields:            fields,
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
	}

	if len(out) == 0 {
		items, _, err := r.QueryItems(ctx, ItemFilter{
			ServerID:          serverID,
			UserID:            userID,
			AllowedLibraryIDs: allowedLibraryIDs,
			MaxParentalRating: maxParentalRating,
			BlockedTags:       blockedTags,
			IncludeItemTypes:  []string{"Movie"},
			SortBy:            []string{"DateCreated"},
			SortOrder:         "Descending",
			Limit:             itemLimit,
			Fields:            fields,
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

// QueryFiltersFilter scopes GET /Items/Filters and GET /Items/Filters2.
type QueryFiltersFilter struct {
	ParentID          string
	IncludeItemTypes  []string
	AllowedLibraryIDs []string
	MaxParentalRating *int
	BlockedTags       []string
}

// QueryFiltersResult holds distinct filter dimensions for /Items/Filters and /Items/Filters2.
type QueryFiltersResult struct {
	Genres          []dto.NameIdPair
	Studios         []dto.NameIdPair
	Tags            []string
	OfficialRatings []string
	Years           []int
}

// GetQueryFilters returns distinct Genres, Studios, Tags, OfficialRatings, and Years scoped to f.
func (r *LibraryRepo) GetQueryFilters(ctx context.Context, f QueryFiltersFilter) (*QueryFiltersResult, error) {
	res := &QueryFiltersResult{
		Genres:          []dto.NameIdPair{},
		Studios:         []dto.NameIdPair{},
		Tags:            []string{},
		OfficialRatings: []string{},
		Years:           []int{},
	}
	if f.AllowedLibraryIDs != nil && len(f.AllowedLibraryIDs) == 0 {
		return res, nil
	}

	parentID := guid.Normalize(f.ParentID)
	var (
		conds = []string{`i.extra_type = ''`}
		args  []any
	)
	if parentID != "" {
		conds = append(conds, `(i.library_id = ? OR i.parent_id = ? OR EXISTS (SELECT 1 FROM item_ancestors ia WHERE ia.ancestor_id = ? AND ia.descendant_id = i.id AND ia.depth > 0))`)
		args = append(args, parentID, parentID, parentID)
	}
	if len(f.IncludeItemTypes) > 0 {
		ph := make([]string, len(f.IncludeItemTypes))
		for idx, t := range f.IncludeItemTypes {
			ph[idx] = "?"
			args = append(args, t)
		}
		conds = append(conds, fmt.Sprintf(`i.type IN (%s)`, strings.Join(ph, ",")))
	}
	if len(f.AllowedLibraryIDs) > 0 {
		ph := make([]string, len(f.AllowedLibraryIDs))
		for idx, lid := range f.AllowedLibraryIDs {
			ph[idx] = "?"
			args = append(args, guid.Normalize(lid))
		}
		conds = append(conds, fmt.Sprintf(`i.library_id IN (%s)`, strings.Join(ph, ",")))
	}
	if pClauses, pArgs := appendParentalFilterSQL("i", f.MaxParentalRating, f.BlockedTags); len(pClauses) > 0 {
		conds = append(conds, pClauses...)
		args = append(args, pArgs...)
	}
	scope := strings.Join(conds, " AND ")

	// 1. Genres
	gRows, err := r.store.Reader.QueryContext(ctx, `
		SELECT DISTINCT t.id, t.name
		FROM taxonomies t
		JOIN item_taxonomies it ON it.taxonomy_id = t.id
		JOIN items i ON i.id = it.item_id
		WHERE t.kind = 'genre' AND `+scope+`
		ORDER BY t.sort_name ASC`, args...)
	if err != nil {
		return nil, err
	}
	for gRows.Next() {
		var p dto.NameIdPair
		if err := gRows.Scan(&p.Id, &p.Name); err != nil {
			gRows.Close()
			return nil, err
		}
		res.Genres = append(res.Genres, p)
	}
	if err := gRows.Err(); err != nil {
		gRows.Close()
		return nil, err
	}
	gRows.Close()

	// 2. Studios
	sRows, err := r.store.Reader.QueryContext(ctx, `
		SELECT DISTINCT t.id, t.name
		FROM taxonomies t
		JOIN item_taxonomies it ON it.taxonomy_id = t.id
		JOIN items i ON i.id = it.item_id
		WHERE t.kind = 'studio' AND `+scope+`
		ORDER BY t.sort_name ASC`, args...)
	if err != nil {
		return nil, err
	}
	for sRows.Next() {
		var p dto.NameIdPair
		if err := sRows.Scan(&p.Id, &p.Name); err != nil {
			sRows.Close()
			return nil, err
		}
		res.Studios = append(res.Studios, p)
	}
	if err := sRows.Err(); err != nil {
		sRows.Close()
		return nil, err
	}
	sRows.Close()

	// 3. Tags
	tRows, err := r.store.Reader.QueryContext(ctx, `
		SELECT DISTINCT t.name
		FROM taxonomies t
		JOIN item_taxonomies it ON it.taxonomy_id = t.id
		JOIN items i ON i.id = it.item_id
		WHERE t.kind = 'tag' AND `+scope+`
		ORDER BY t.sort_name ASC`, args...)
	if err != nil {
		return nil, err
	}
	for tRows.Next() {
		var name string
		if err := tRows.Scan(&name); err != nil {
			tRows.Close()
			return nil, err
		}
		res.Tags = append(res.Tags, name)
	}
	if err := tRows.Err(); err != nil {
		tRows.Close()
		return nil, err
	}
	tRows.Close()

	// 4. OfficialRatings
	rRows, err := r.store.Reader.QueryContext(ctx, `
		SELECT DISTINCT i.official_rating
		FROM items i
		WHERE i.official_rating IS NOT NULL AND i.official_rating != '' AND `+scope+`
		ORDER BY lower(i.official_rating) ASC`, args...)
	if err != nil {
		return nil, err
	}
	for rRows.Next() {
		var rating string
		if err := rRows.Scan(&rating); err != nil {
			rRows.Close()
			return nil, err
		}
		res.OfficialRatings = append(res.OfficialRatings, rating)
	}
	if err := rRows.Err(); err != nil {
		rRows.Close()
		return nil, err
	}
	rRows.Close()

	// 5. Years
	yRows, err := r.store.Reader.QueryContext(ctx, `
		SELECT DISTINCT i.production_year
		FROM items i
		WHERE i.production_year IS NOT NULL AND `+scope+`
		ORDER BY i.production_year DESC`, args...)
	if err != nil {
		return nil, err
	}
	for yRows.Next() {
		var y int
		if err := yRows.Scan(&y); err != nil {
			yRows.Close()
			return nil, err
		}
		res.Years = append(res.Years, y)
	}
	if err := yRows.Err(); err != nil {
		yRows.Close()
		return nil, err
	}
	yRows.Close()

	return res, nil
}

// ListFilterValues dynamically aggregates distinct filter values (genres, tags, codecs, containers, etc.)
// from SQLite tables and returns them as Emby-compatible BaseItemDto records.
func (r *LibraryRepo) ListFilterValues(
	ctx context.Context,
	serverID, dimension, parentID string,
	includeItemTypes []string,
	startIndex, limit int,
) ([]dto.BaseItemDto, int, error) {
	return r.ListFilterValuesWithPolicy(ctx, serverID, dimension, parentID, includeItemTypes, startIndex, limit, nil, nil, nil)
}

// ListFilterValuesWithPolicy dynamically aggregates distinct filter values while enforcing library RBAC and parental policy.
func (r *LibraryRepo) ListFilterValuesWithPolicy(
	ctx context.Context,
	serverID, dimension, parentID string,
	includeItemTypes []string,
	startIndex, limit int,
	allowedLibraryIDs []string,
	maxParentalRating *int,
	blockedTags []string,
) ([]dto.BaseItemDto, int, error) {
	if allowedLibraryIDs != nil && len(allowedLibraryIDs) == 0 {
		return []dto.BaseItemDto{}, 0, nil
	}
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
		conds := []string{fmt.Sprintf(`%s.extra_type = ''`, alias)}
		var a []any
		if parentID != "" {
			conds = append(conds, fmt.Sprintf(`(%s.library_id = ? OR %s.parent_id = ? OR EXISTS (SELECT 1 FROM item_ancestors ia WHERE ia.ancestor_id = ? AND ia.descendant_id = %s.id AND ia.depth > 0))`, alias, alias, alias))
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
		if len(allowedLibraryIDs) > 0 {
			ph := make([]string, len(allowedLibraryIDs))
			for i, lid := range allowedLibraryIDs {
				ph[i] = "?"
				a = append(a, guid.Normalize(lid))
			}
			conds = append(conds, fmt.Sprintf(`%s.library_id IN (%s)`, alias, strings.Join(ph, ",")))
		}
		if pClauses, pArgs := appendParentalFilterSQL(alias, maxParentalRating, blockedTags); len(pClauses) > 0 {
			conds = append(conds, pClauses...)
			a = append(a, pArgs...)
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
