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
	"unicode"

	"github.com/ambra-media/ambra/internal/pkg/guid"
)

const itemSelectCols = `
	i.id, i.library_id, i.parent_id, i.series_id, i.season_id,
	i.type, i.media_type, i.is_folder, i.is_strm,
	i.name, i.original_title, i.sort_name,
	i.index_number, i.parent_index_number, i.production_year,
	i.premiere_date, i.end_date, i.community_rating, i.critic_rating, i.official_rating,
	i.runtime_ticks, i.container, i.path, i.file_size, i.file_mtime,
	i.etag, i.extra_json, i.date_created, i.date_modified`

// FindItemByMediaSourcePath looks up an existing item in a library by either its primary path
// (items.path) or any attached media source path (item_media_sources.path).
func (r *LibraryRepo) FindItemByMediaSourcePath(ctx context.Context, libraryID, path string) (*ItemRecord, error) {
	if path == "" {
		return nil, ErrNotFound
	}
	libID := guid.Normalize(libraryID)
	row := r.store.Reader.QueryRowContext(ctx,
		`SELECT `+itemSelectCols+`
		 FROM items i
		 WHERE i.library_id = ?
		   AND (i.path = ? OR EXISTS (SELECT 1 FROM item_media_sources ms WHERE ms.item_id = i.id AND ms.path = ?))
		 LIMIT 1`,
		libID, path, path,
	)
	rec, err := scanItemRecord(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return rec, err
}

// FindItemByProviderID finds the oldest existing item (e.g. Movie or Series) within the same library
// matching the given external provider key/value (case-insensitive key matching for Tmdb/tmdb, Imdb/imdb, etc.).
func (r *LibraryRepo) FindItemByProviderID(ctx context.Context, libraryID, itemType, providerKey, providerVal string) (*ItemRecord, error) {
	items, err := r.FindItemsByProviderID(ctx, libraryID, itemType, providerKey, providerVal)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, ErrNotFound
	}
	return items[0], nil
}

// FindItemsByProviderID returns all items within the same library matching the given external provider
// key/value ordered by date_created ASC, id ASC (oldest/primary first).
func (r *LibraryRepo) FindItemsByProviderID(ctx context.Context, libraryID, itemType, providerKey, providerVal string) ([]*ItemRecord, error) {
	providerKey = strings.TrimSpace(providerKey)
	providerVal = strings.TrimSpace(providerVal)
	if providerKey == "" || providerVal == "" {
		return nil, nil
	}
	libID := guid.Normalize(libraryID)

	// Build unique case variants of the provider key so both "tmdb"/"Tmdb"/"TMDB" and
	// both JSON structures ("$.provider_ids.<k>" and "$.ProviderIds.<k>") match via json_extract.
	variants := providerKeyVariants(providerKey)
	var conds []string
	args := []any{libID}
	if itemType != "" {
		args = append(args, itemType)
	}
	for _, v := range variants {
		quoted := escapeJSONPathKey(v)
		for _, prefix := range []string{"$.provider_ids.", "$.ProviderIds."} {
			conds = append(conds, `LOWER(CAST(json_extract(i.extra_json, ?) AS TEXT)) = LOWER(?)`)
			args = append(args, prefix+quoted, providerVal)
		}
	}

	typeClause := ""
	if itemType != "" {
		typeClause = ` AND LOWER(i.type) = LOWER(?)`
	}

	query := `SELECT ` + itemSelectCols + `
		 FROM items i
		 WHERE i.library_id = ?` + typeClause + `
		   AND (` + strings.Join(conds, " OR ") + `)
		 ORDER BY i.date_created ASC, i.id ASC`

	rows, err := r.store.Reader.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*ItemRecord
	for rows.Next() {
		rec, err := scanItemRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func escapeJSONPathKey(key string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(key)
	return `"` + escaped + `"`
}

func escapeLikePattern(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func providerKeyVariants(key string) []string {
	if key == "" {
		return nil
	}
	lower := strings.ToLower(key)
	upper := strings.ToUpper(key)
	runes := []rune(lower)
	title := string(unicode.ToUpper(runes[0])) + string(runes[1:])
	seen := make(map[string]struct{}, 4)
	out := make([]string, 0, 4)
	for _, v := range []string{key, lower, title, upper} {
		if _, ok := seen[v]; !ok {
			seen[v] = struct{}{}
			out = append(out, v)
		}
	}
	return out
}

// FindSeasonByNumber finds an existing Season for a Series by season number.
func (r *LibraryRepo) FindSeasonByNumber(ctx context.Context, seriesID string, seasonNum int) (*ItemRecord, error) {
	seriesID = guid.Normalize(seriesID)
	if seriesID == "" {
		return nil, ErrNotFound
	}
	row := r.store.Reader.QueryRowContext(ctx,
		`SELECT `+itemSelectCols+`
		 FROM items i
		 WHERE i.series_id = ? AND i.type = 'Season' AND i.index_number = ?
		 ORDER BY i.date_created ASC, i.id ASC
		 LIMIT 1`,
		seriesID, seasonNum,
	)
	rec, err := scanItemRecord(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return rec, err
}

// FindEpisodeByNumber finds an existing Episode for a Series by season and episode numbers.
func (r *LibraryRepo) FindEpisodeByNumber(ctx context.Context, seriesID string, seasonNum, episodeNum int) (*ItemRecord, error) {
	seriesID = guid.Normalize(seriesID)
	if seriesID == "" {
		return nil, ErrNotFound
	}
	row := r.store.Reader.QueryRowContext(ctx,
		`SELECT `+itemSelectCols+`
		 FROM items i
		 WHERE i.series_id = ? AND i.type = 'Episode' AND i.parent_index_number = ? AND i.index_number = ?
		 ORDER BY i.date_created ASC, i.id ASC
		 LIMIT 1`,
		seriesID, seasonNum, episodeNum,
	)
	rec, err := scanItemRecord(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return rec, err
}

// FindMovieInDirByBaseTitle finds an existing Movie in the exact same directory whose normalized
// title matches normalizedTitle, used for same-directory multi-version merging when TMDB/IMDB IDs are absent.
func (r *LibraryRepo) FindMovieInDirByBaseTitle(ctx context.Context, libraryID, dir, normalizedTitle string) (*ItemRecord, error) {
	normalizedTitle = strings.TrimSpace(strings.ToLower(normalizedTitle))
	if dir == "" || normalizedTitle == "" {
		return nil, ErrNotFound
	}
	cleanDir := filepath.Clean(dir)
	prefix := escapeLikePattern(cleanDir+string(filepath.Separator)) + "%"

	rows, err := r.store.Reader.QueryContext(ctx,
		`SELECT `+itemSelectCols+`
		 FROM items i
		 WHERE i.library_id = ? AND i.type = 'Movie' AND i.path LIKE ? ESCAPE '\'
		 ORDER BY i.date_created ASC, i.id ASC`,
		guid.Normalize(libraryID), prefix,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		rec, err := scanItemRecord(rows)
		if err != nil {
			return nil, err
		}
		if filepath.Clean(filepath.Dir(rec.Path)) != cleanDir {
			continue
		}
		if strings.ToLower(strings.TrimSpace(rec.Name)) == normalizedTitle ||
			strings.ToLower(strings.TrimSpace(rec.SortName)) == normalizedTitle ||
			(rec.OriginalTitle != "" && strings.ToLower(strings.TrimSpace(rec.OriginalTitle)) == normalizedTitle) {
			return rec, nil
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return nil, ErrNotFound
}

// UpsertMediaSourceAndStreams idempotently inserts or updates a single MediaSource and its
// streams keyed by (item_id, path), preserving all other MediaSources belonging to the item.
func (r *LibraryRepo) UpsertMediaSourceAndStreams(ctx context.Context, itemID string, ms MediaSourceRecord) error {
	itemID = guid.Normalize(itemID)
	tx, err := r.store.Writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	proto := ms.Protocol
	if proto == "" {
		proto = "File"
	}
	extra := ms.ExtraJSON
	if extra == "" {
		extra = "{}"
	}

	var srcID string
	err = tx.QueryRowContext(ctx,
		`SELECT id FROM item_media_sources WHERE item_id = ? AND path = ? LIMIT 1`,
		itemID, ms.Path,
	).Scan(&srcID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	if srcID != "" {
		if _, err := tx.ExecContext(ctx, `DELETE FROM item_media_streams WHERE media_source_id = ?`, srcID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE item_media_sources
			SET name = ?, protocol = ?, container = ?, size = ?, bitrate = ?, runtime_ticks = ?,
			    is_remote = ?, supports_direct_play = ?, supports_direct_stream = ?, supports_transcoding = ?, extra_json = ?
			WHERE id = ?`,
			ms.Name, proto, nullStr(ms.Container), ms.Size, ms.Bitrate, ms.RuntimeTicks,
			boolToInt(ms.IsRemote), boolToInt(ms.SupportsDirectPlay), boolToInt(ms.SupportsDirectStream), boolToInt(ms.SupportsTranscoding), extra,
			srcID,
		); err != nil {
			return err
		}
	} else {
		srcID = ms.ID
		if srcID == "" {
			srcID = guid.New()
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO item_media_sources (
				id, item_id, name, path, protocol, container, size, bitrate, runtime_ticks,
				is_remote, supports_direct_play, supports_direct_stream, supports_transcoding, extra_json
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			srcID, itemID, ms.Name, ms.Path, proto, nullStr(ms.Container), ms.Size, ms.Bitrate, ms.RuntimeTicks,
			boolToInt(ms.IsRemote), boolToInt(ms.SupportsDirectPlay), boolToInt(ms.SupportsDirectStream), boolToInt(ms.SupportsTranscoding), extra,
		); err != nil {
			return err
		}
	}

	for _, st := range ms.Streams {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO item_media_streams (
				media_source_id, item_id, stream_index, stream_type, codec, language, sub_variant,
				display_title, title, width, height, video_range, audio_layout, channels,
				sample_rate, bitrate, is_default, is_forced, is_external, path
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			srcID, itemID, st.StreamIndex, st.StreamType, nullStr(st.Codec), nullStr(st.Language), nullStr(st.SubVariant),
			nullStr(st.DisplayTitle), nullStr(st.Title), st.Width, st.Height, nullStr(st.VideoRange), nullStr(st.AudioLayout), st.Channels,
			st.SampleRate, st.Bitrate, boolToInt(st.IsDefault), boolToInt(st.IsForced), boolToInt(st.IsExternal), nullStr(st.Path),
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// MergeItemVersions merges multiple items into itemIDs[0] as the primary item.
// Media sources and streams from secondary items are reassigned to primaryID (deduplicated by path),
// and the redundant secondary items (along with their FTS and ancestor rows) are removed.
func (r *LibraryRepo) MergeItemVersions(ctx context.Context, itemIDs []string) (string, int, error) {
	seen := make(map[string]struct{}, len(itemIDs))
	cleanIDs := make([]string, 0, len(itemIDs))
	for _, raw := range itemIDs {
		id := guid.Normalize(raw)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		cleanIDs = append(cleanIDs, id)
	}
	if len(cleanIDs) < 2 {
		return "", 0, fmt.Errorf("at least 2 distinct item IDs are required to merge")
	}

	primaryID := cleanIDs[0]

	tx, err := r.store.Writer.BeginTx(ctx, nil)
	if err != nil {
		return "", 0, err
	}
	defer tx.Rollback()

	var (
		primaryLibraryID string
		primaryType      string
		primaryIsFolder  int
	)
	if err := tx.QueryRowContext(ctx, `SELECT library_id, type, is_folder FROM items WHERE id = ?`, primaryID).Scan(&primaryLibraryID, &primaryType, &primaryIsFolder); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", 0, ErrNotFound
		}
		return "", 0, err
	}
	if primaryIsFolder != 0 {
		return "", 0, fmt.Errorf("cannot merge folder item %s", primaryID)
	}

	existingPaths := make(map[string]bool)
	pRows, err := tx.QueryContext(ctx, `SELECT path FROM item_media_sources WHERE item_id = ?`, primaryID)
	if err != nil {
		return "", 0, err
	}
	for pRows.Next() {
		var p string
		if err := pRows.Scan(&p); err != nil {
			pRows.Close()
			return "", 0, err
		}
		existingPaths[p] = true
	}
	if err := pRows.Err(); err != nil {
		pRows.Close()
		return "", 0, err
	}
	pRows.Close()

	mergedCount := 0
	for _, otherID := range cleanIDs[1:] {
		var (
			otherLibraryID string
			otherType      string
			otherIsFolder  int
		)
		if err := tx.QueryRowContext(ctx, `SELECT library_id, type, is_folder FROM items WHERE id = ?`, otherID).Scan(&otherLibraryID, &otherType, &otherIsFolder); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return "", 0, err
		}
		if otherIsFolder != 0 {
			return "", 0, fmt.Errorf("cannot merge folder item %s", otherID)
		}
		if guid.Normalize(otherLibraryID) != guid.Normalize(primaryLibraryID) {
			return "", 0, fmt.Errorf("cannot merge items across different libraries: %s vs %s", primaryLibraryID, otherLibraryID)
		}
		if !strings.EqualFold(otherType, primaryType) {
			return "", 0, fmt.Errorf("cannot merge items of different types: %s vs %s", primaryType, otherType)
		}

		type srcRow struct {
			id   string
			path string
		}
		var otherSources []srcRow
		sRows, err := tx.QueryContext(ctx, `SELECT id, path FROM item_media_sources WHERE item_id = ? ORDER BY rowid ASC`, otherID)
		if err != nil {
			return "", 0, err
		}
		for sRows.Next() {
			var sr srcRow
			if err := sRows.Scan(&sr.id, &sr.path); err != nil {
				sRows.Close()
				return "", 0, err
			}
			otherSources = append(otherSources, sr)
		}
		if err := sRows.Err(); err != nil {
			sRows.Close()
			return "", 0, err
		}
		sRows.Close()

		for _, sr := range otherSources {
			if existingPaths[sr.path] {
				if _, err := tx.ExecContext(ctx, `DELETE FROM item_media_streams WHERE media_source_id = ?`, sr.id); err != nil {
					return "", 0, err
				}
				if _, err := tx.ExecContext(ctx, `DELETE FROM item_media_sources WHERE id = ?`, sr.id); err != nil {
					return "", 0, err
				}
				continue
			}
			existingPaths[sr.path] = true
			if _, err := tx.ExecContext(ctx, `UPDATE item_media_sources SET item_id = ? WHERE id = ?`, primaryID, sr.id); err != nil {
				return "", 0, err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE item_media_streams SET item_id = ? WHERE media_source_id = ?`, primaryID, sr.id); err != nil {
				return "", 0, err
			}
		}

		// Preserve secondary item's user_item_data by merging into primaryID before cascade delete.
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO user_item_data (
				user_id, item_id, played, play_count, playback_position_ticks,
				is_favorite, last_played_date, audio_stream_index, subtitle_stream_index
			)
			SELECT user_id, ?, played, play_count, playback_position_ticks,
			       is_favorite, last_played_date, audio_stream_index, subtitle_stream_index
			FROM user_item_data
			WHERE item_id = ?
			ON CONFLICT(user_id, item_id) DO UPDATE SET
				played = MAX(user_item_data.played, excluded.played),
				play_count = MAX(user_item_data.play_count, excluded.play_count),
				playback_position_ticks = CASE
					WHEN MAX(user_item_data.played, excluded.played) = 1 THEN 0
					WHEN COALESCE(excluded.last_played_date, 0) > COALESCE(user_item_data.last_played_date, 0) THEN excluded.playback_position_ticks
					ELSE MAX(user_item_data.playback_position_ticks, excluded.playback_position_ticks)
				END,
				is_favorite = MAX(user_item_data.is_favorite, excluded.is_favorite),
				last_played_date = CASE
					WHEN user_item_data.last_played_date IS NULL THEN excluded.last_played_date
					WHEN excluded.last_played_date IS NULL THEN user_item_data.last_played_date
					ELSE MAX(user_item_data.last_played_date, excluded.last_played_date)
				END,
				audio_stream_index = COALESCE(user_item_data.audio_stream_index, excluded.audio_stream_index),
				subtitle_stream_index = COALESCE(user_item_data.subtitle_stream_index, excluded.subtitle_stream_index)`,
			primaryID, otherID,
		); err != nil {
			return "", 0, err
		}

		// Preserve collection/playlist memberships that do not already include primaryID.
		if _, err := tx.ExecContext(ctx, `
			UPDATE item_collections
			SET item_id = ?
			WHERE item_id = ?
			  AND NOT EXISTS (
				  SELECT 1 FROM item_collections ic2
				  WHERE ic2.container_id = item_collections.container_id
				    AND ic2.item_id = ?
			  )`,
			primaryID, otherID, primaryID,
		); err != nil {
			return "", 0, err
		}

		if _, err := tx.ExecContext(ctx, `DELETE FROM item_search_fts WHERE item_id = ?`, otherID); err != nil {
			return "", 0, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM item_images WHERE item_id = ?`, otherID); err != nil {
			return "", 0, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM item_ancestors WHERE ancestor_id = ? OR descendant_id = ?`, otherID, otherID); err != nil {
			return "", 0, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM items WHERE id = ?`, otherID); err != nil {
			return "", 0, err
		}
		mergedCount++
	}

	if _, err := tx.ExecContext(ctx, `UPDATE items SET date_modified = ?, etag = ? WHERE id = ?`,
		time.Now().Unix(), guid.New()[:16], primaryID); err != nil {
		return "", 0, err
	}

	if err := tx.Commit(); err != nil {
		return "", 0, err
	}
	r.InvalidateCountCache()
	return primaryID, mergedCount, nil
}

// SplitItemVersions keeps the first MediaSource on itemID and splits every additional
// MediaSource into its own independent ItemRecord (copying base metadata, ancestors, and FTS,
// and migrating the corresponding MediaSource and MediaStreams) inside a single Writer transaction.
func (r *LibraryRepo) SplitItemVersions(ctx context.Context, itemID string) ([]string, error) {
	itemID = guid.Normalize(itemID)

	tx, err := r.store.Writer.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	primary, err := scanItemRecord(tx.QueryRowContext(ctx,
		`SELECT `+itemSelectCols+` FROM items i WHERE i.id = ? LIMIT 1`, itemID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	var sources []MediaSourceRecord
	msRows, err := tx.QueryContext(ctx, `
		SELECT id, item_id, name, path, protocol, COALESCE(container,''), COALESCE(size,0), COALESCE(bitrate,0),
		       COALESCE(runtime_ticks,0), is_remote, supports_direct_play, supports_direct_stream, supports_transcoding, extra_json
		FROM item_media_sources WHERE item_id = ? ORDER BY rowid ASC`, itemID)
	if err != nil {
		return nil, err
	}
	for msRows.Next() {
		var ms MediaSourceRecord
		var isRem, dp, ds, tc int
		if err := msRows.Scan(&ms.ID, &ms.ItemID, &ms.Name, &ms.Path, &ms.Protocol, &ms.Container, &ms.Size, &ms.Bitrate,
			&ms.RuntimeTicks, &isRem, &dp, &ds, &tc, &ms.ExtraJSON); err != nil {
			msRows.Close()
			return nil, err
		}
		ms.IsRemote = isRem != 0
		ms.SupportsDirectPlay = dp != 0
		ms.SupportsDirectStream = ds != 0
		ms.SupportsTranscoding = tc != 0
		sources = append(sources, ms)
	}
	if err := msRows.Err(); err != nil {
		msRows.Close()
		return nil, err
	}
	msRows.Close()

	if len(sources) <= 1 {
		return []string{}, nil
	}

	// Load existing ancestors to replicate on split items.
	var ancestors []AncestorLink
	aRows, err := tx.QueryContext(ctx,
		`SELECT ancestor_id, depth FROM item_ancestors WHERE descendant_id = ? ORDER BY depth ASC`, itemID)
	if err != nil {
		return nil, err
	}
	for aRows.Next() {
		var a AncestorLink
		if err := aRows.Scan(&a.AncestorID, &a.Depth); err != nil {
			aRows.Close()
			return nil, err
		}
		ancestors = append(ancestors, a)
	}
	if err := aRows.Err(); err != nil {
		aRows.Close()
		return nil, err
	}
	aRows.Close()

	// Load existing FTS fields if present.
	var ftsTitleRaw, ftsTitleTokens, ftsPinyinFull, ftsPinyinInit, ftsPeople string
	hasFTS := tx.QueryRowContext(ctx,
		`SELECT title_raw, title_tokens, pinyin_full, pinyin_initials, people_names FROM item_search_fts WHERE item_id = ? LIMIT 1`,
		itemID,
	).Scan(&ftsTitleRaw, &ftsTitleTokens, &ftsPinyinFull, &ftsPinyinInit, &ftsPeople) == nil

	extraRaw, err := json.Marshal(primary.Extra)
	if err != nil {
		return nil, err
	}

	now := time.Now().Unix()
	createdIDs := make([]string, 0, len(sources)-1)

	for _, src := range sources[1:] {
		newID := guid.New()
		isStrm := src.IsRemote || strings.EqualFold(src.Container, "strm")
		rt := primary.RuntimeTicks
		if src.RuntimeTicks > 0 {
			v := src.RuntimeTicks
			rt = &v
		}
		var fileSize *int64
		if src.Size > 0 {
			sz := src.Size
			fileSize = &sz
		}
		container := src.Container
		if container == "" {
			container = primary.Container
		}
		sortKey, extraType, parentalRatingValue := computeItemDerivedFields(primary)
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO items (
				id, internal_id, library_id, parent_id, series_id, season_id,
				type, media_type, is_folder, is_strm,
				name, original_title, sort_name,
				index_number, parent_index_number, production_year,
				premiere_date, end_date, community_rating, critic_rating, official_rating,
				runtime_ticks, container, path, file_size, file_mtime,
				etag, extra_json, date_created, date_modified,
				sort_key, extra_type, parental_rating_value
			) VALUES (
				?, NULL, ?, ?, ?, ?,
				?, ?, 0, ?,
				?, ?, ?,
				?, ?, ?,
				?, ?, ?, ?, ?,
				?, ?, ?, ?, NULL,
				?, ?, ?, ?,
				?, ?, ?
			)`,
			newID, primary.LibraryID, nullStr(primary.ParentID), nullStr(primary.SeriesID), nullStr(primary.SeasonID),
			primary.Type, nullStr(primary.MediaType), boolToInt(isStrm),
			primary.Name, nullStr(primary.OriginalTitle), primary.SortName,
			primary.IndexNumber, primary.ParentIndexNumber, primary.ProductionYear,
			primary.PremiereDate, primary.EndDate, primary.CommunityRating, primary.CriticRating, nullStr(primary.OfficialRating),
			rt, nullStr(container), nullStr(src.Path), fileSize,
			guid.New()[:16], string(extraRaw), now, now,
			sortKey, extraType, parentalRatingValue,
		); err != nil {
			return nil, err
		}

		for _, a := range ancestors {
			if _, err := tx.ExecContext(ctx,
				`INSERT OR REPLACE INTO item_ancestors (ancestor_id, descendant_id, depth) VALUES (?, ?, ?)`,
				a.AncestorID, newID, a.Depth,
			); err != nil {
				return nil, err
			}
		}

		if hasFTS {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO item_search_fts (item_id, title_raw, title_tokens, pinyin_full, pinyin_initials, people_names)
				VALUES (?, ?, ?, ?, ?, ?)`,
				newID, ftsTitleRaw, ftsTitleTokens, ftsPinyinFull, ftsPinyinInit, ftsPeople,
			); err != nil {
				return nil, err
			}
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO item_images (item_id, image_type, image_index, path, tag, width, height, blurhash)
			SELECT ?, image_type, image_index, path, tag, width, height, blurhash
			FROM item_images WHERE item_id = ?`,
			newID, itemID,
		); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO item_taxonomies (item_id, taxonomy_id)
			SELECT ?, taxonomy_id FROM item_taxonomies WHERE item_id = ?`,
			newID, itemID,
		); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO item_people (item_id, person_id, role, person_type, sort_order)
			SELECT ?, person_id, role, person_type, sort_order FROM item_people WHERE item_id = ?`,
			newID, itemID,
		); err != nil {
			return nil, err
		}

		if _, err := tx.ExecContext(ctx, `UPDATE item_media_sources SET item_id = ? WHERE id = ?`, newID, src.ID); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE item_media_streams SET item_id = ? WHERE media_source_id = ?`, newID, src.ID); err != nil {
			return nil, err
		}

		createdIDs = append(createdIDs, newID)
	}

	firstSrc := sources[0]
	firstIsStrm := firstSrc.IsRemote || strings.EqualFold(firstSrc.Container, "strm")
	firstContainer := firstSrc.Container
	if firstContainer == "" {
		firstContainer = primary.Container
	}
	firstRT := primary.RuntimeTicks
	if firstSrc.RuntimeTicks > 0 {
		v := firstSrc.RuntimeTicks
		firstRT = &v
	}
	firstSize := primary.FileSize
	if firstSrc.Size > 0 {
		sz := firstSrc.Size
		firstSize = &sz
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE items
		SET path = ?, container = ?, file_size = ?, is_strm = ?, runtime_ticks = ?, date_modified = ?, etag = ?
		WHERE id = ?`,
		nullStr(firstSrc.Path), nullStr(firstContainer), firstSize, boolToInt(firstIsStrm), firstRT,
		now, guid.New()[:16], itemID,
	); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	r.InvalidateCountCache()
	return createdIDs, nil
}
