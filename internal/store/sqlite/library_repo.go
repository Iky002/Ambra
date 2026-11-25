package sqlite

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/pkg/guid"
)

const sqliteInChunkSize = 400

var ErrNotFound = errors.New("not found")

// LibraryRecord represents a row in the libraries table.
type LibraryRecord struct {
	ID             string
	Name           string
	CollectionType string
	Paths          []string
	OptionsJSON    string
	CreatedAt      int64
	UpdatedAt      int64
}

// ParseLibraryOptions parses a library's options_json into a map and applies
// defaults (SaveLocalMetadata defaults to true when omitted).
func ParseLibraryOptions(raw string) map[string]any {
	opts := make(map[string]any)
	raw = strings.TrimSpace(raw)
	if raw != "" && raw != "{}" {
		_ = json.Unmarshal([]byte(raw), &opts)
	}
	if _, ok := opts["SaveLocalMetadata"]; !ok {
		opts["SaveLocalMetadata"] = true
	}
	return opts
}

// SaveLocalMetadataEnabled reports whether scraped NFO and artwork should be
// saved alongside media files for this library (defaults to true).
func (l *LibraryRecord) SaveLocalMetadataEnabled() bool {
	if l == nil {
		return true
	}
	opts := ParseLibraryOptions(l.OptionsJSON)
	if v, ok := opts["SaveLocalMetadata"].(bool); ok {
		return v
	}
	return true
}

// ChapterRecord represents a video chapter persisted inside ItemExtra.
type ChapterRecord struct {
	StartPositionTicks int64  `json:"start_position_ticks"`
	Name               string `json:"name,omitempty"`
}

// ItemExtra is stored as JSON in items.extra_json.
type ItemExtra struct {
	Overview                string            `json:"overview,omitempty"`
	Taglines                []string          `json:"taglines,omitempty"`
	ProviderIDs             map[string]string `json:"provider_ids,omitempty"`
	SeriesName              string            `json:"series_name,omitempty"`
	SeasonName              string            `json:"season_name,omitempty"`
	Album                   string            `json:"album,omitempty"`
	AlbumID                 string            `json:"album_id,omitempty"`
	AlbumArtist             string            `json:"album_artist,omitempty"`
	Artists                 []string          `json:"artists,omitempty"`
	PrimaryImageAspectRatio *float64          `json:"primary_image_aspect_ratio,omitempty"`
	CollectionType          string            `json:"collection_type,omitempty"`
	ExtraType               string            `json:"extra_type,omitempty"`
	Chapters                []ChapterRecord   `json:"chapters,omitempty"`
	LockedFields            []string          `json:"LockedFields,omitempty"`
	LockData                bool              `json:"LockData,omitempty"`
}

// ItemRecord represents a row in the items table.
type ItemRecord struct {
	ID                string
	InternalID        *int64
	LibraryID         string
	ParentID          string
	SeriesID          string
	SeasonID          string
	Type              string
	MediaType         string
	IsFolder          bool
	IsStrm            bool
	Name              string
	OriginalTitle     string
	SortName          string
	IndexNumber       *int
	ParentIndexNumber *int
	ProductionYear    *int
	PremiereDate      *int64
	EndDate           *int64
	CommunityRating   *float64
	CriticRating      *float64
	OfficialRating    string
	RuntimeTicks      *int64
	Container         string
	Path              string
	FileSize          *int64
	FileMtime         *int64
	Etag              string
	Extra             ItemExtra
	DateCreated       int64
	DateModified      int64
}

// MediaSourceRecord represents a row in item_media_sources with its streams.
type MediaSourceRecord struct {
	ID                   string
	ItemID               string
	Name                 string
	Path                 string
	Protocol             string
	Container            string
	Size                 int64
	Bitrate              int
	RuntimeTicks         int64
	IsRemote             bool
	SupportsDirectPlay   bool
	SupportsDirectStream bool
	SupportsTranscoding  bool
	ExtraJSON            string
	Streams              []MediaStreamRecord
}

// MediaStreamRecord represents a row in item_media_streams.
type MediaStreamRecord struct {
	MediaSourceID string
	ItemID        string
	StreamIndex   int
	StreamType    string
	Codec         string
	Language      string
	SubVariant    string
	DisplayTitle  string
	Title         string
	Width         int
	Height        int
	VideoRange    string
	AudioLayout   string
	Channels      int
	SampleRate    int
	Bitrate       int
	IsDefault     bool
	IsForced      bool
	IsExternal    bool
	Path          string
}

// ImageRecord represents a row in item_images.
type ImageRecord struct {
	ItemID     string
	ImageType  string
	ImageIndex int
	Path       string
	Tag        string
	Width      int
	Height     int
	Blurhash   string
}

// PersonInput represents a person credit to associate with an item.
type PersonInput struct {
	Name      string
	Role      string
	Type      string
	SortOrder int
}

// AncestorLink represents an entry in item_ancestors.
type AncestorLink struct {
	AncestorID string
	Depth      int
}

// UserItemDataRecord represents a row in user_item_data.
type UserItemDataRecord struct {
	UserID                string
	ItemID                string
	Played                bool
	PlayCount             int
	PlaybackPositionTicks int64
	IsFavorite            bool
	LastPlayedDate        *int64
	AudioStreamIndex      *int
	SubtitleStreamIndex   *int
}

// ItemFilter holds query parameters for QueryItems.
type ItemFilter struct {
	ServerID               string
	UserID                 string
	ParentID               string
	LibraryID              string
	AllowedLibraryIDs      []string
	MaxParentalRating      *int
	BlockedTags            []string
	SeriesID               string
	SeasonID               string
	Recursive              bool
	IncludeItemTypes       []string
	ExcludeItemTypes       []string
	IDs                    []string
	ExcludeItemIDs         []string
	Genres                 []string
	GenreIDs               []string
	Studios                []string
	StudioIDs              []string
	Tags                   []string
	OfficialRatings        []string
	MinCommunityRating     *float64
	PersonIDs              []string
	Persons                []string
	Years                  []int
	IsHD                   *bool
	Is4K                   *bool
	HasSubtitles           *bool
	IsPlayed               *bool
	IsUnplayed             *bool
	IsFavorite              *bool
	IsResumable             *bool
	SearchTerm              string
	NameStartsWith          string
	NameStartsWithOrGreater string
	NameLessThan            string
	SortBy                  []string
	SortOrder              string
	StartIndex             int
	Limit                  int
	Fields                 []string
	EnableTotalRecordCount *bool
}

// SearchHintFilter holds parameters for SearchHints.
type SearchHintFilter struct {
	UserID            string
	ParentID          string
	AllowedLibraryIDs []string
	MaxParentalRating *int
	BlockedTags       []string
	SearchTerm        string
	IncludeItemTypes  []string
	Limit             int
}

// parentalRatingScores maps standard MPAA / TV / BBFC / FSK / HK / TW parental ratings
// to numeric scores matching /Localization/ParentalRatings.
var parentalRatingScores = map[string]int{
	"G":        1,
	"TV-Y":     1,
	"TV-G":     1,
	"U":        1,
	"0":        1,
	"0+":       1,
	"I":        1,
	"FSK-0":    1,
	"TV-Y7":    4,
	"TV-Y7-FV": 4,
	"6":        4,
	"6+":       4,
	"FSK-6":    4,
	"PG":       5,
	"TV-PG":    5,
	"IIA":      5,
	"PG-13":    7,
	"12":       7,
	"12A":      7,
	"12+":      7,
	"FSK-12":   7,
	"TV-14":    8,
	"15":       8,
	"15+":      8,
	"IIB":      8,
	"MA15+":    8,
	"R":        9,
	"TV-MA":    9,
	"16":       9,
	"16+":      9,
	"18":       9,
	"18+":      9,
	"18A":      9,
	"III":      9,
	"FSK-16":   9,
	"FSK-18":   9,
	"R18+":     9,
	"NC-17":    10,
	"X":        10,
	"XXX":      10,
}

// ParentalRatingScore maps a parental rating label (case-insensitive) to its numeric score.
func ParentalRatingScore(rating string) (int, bool) {
	key := strings.ToUpper(strings.TrimSpace(rating))
	if key == "" {
		return 0, false
	}
	score, ok := parentalRatingScores[key]
	return score, ok
}

// ExceedingParentalRatings returns a sorted slice of uppercase rating strings whose
// numeric score strictly exceeds maxScore.
func ExceedingParentalRatings(maxScore int) []string {
	var out []string
	for label, score := range parentalRatingScores {
		if score > maxScore {
			out = append(out, label)
		}
	}
	sort.Strings(out)
	return out
}

func normalizeBlockedTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(tags))
	out := make([]string, 0, len(tags))
	for _, raw := range tags {
		t := strings.ToLower(strings.TrimSpace(raw))
		if t == "" {
			continue
		}
		if _, dup := seen[t]; dup {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}

func appendParentalFilterSQL(alias string, maxRating *int, blockedTags []string) ([]string, []any) {
	var (
		clauses []string
		args    []any
	)
	if maxRating != nil {
		if exceeding := ExceedingParentalRatings(*maxRating); len(exceeding) > 0 {
			ph := make([]string, len(exceeding))
			for i, r := range exceeding {
				ph[i] = "?"
				args = append(args, r)
			}
			clauses = append(clauses, fmt.Sprintf(`COALESCE(upper(trim(%s.official_rating)), '') NOT IN (%s)`, alias, strings.Join(ph, ",")))
		}
	}
	if normTags := normalizeBlockedTags(blockedTags); len(normTags) > 0 {
		ph := make([]string, len(normTags))
		for i, t := range normTags {
			ph[i] = "?"
			args = append(args, t)
		}
		clauses = append(clauses, fmt.Sprintf(`NOT EXISTS (
			SELECT 1 FROM item_taxonomies it
			JOIN taxonomies t ON t.id = it.taxonomy_id
			WHERE it.item_id IN (%s.id, COALESCE(%s.series_id, ''))
			  AND lower(t.kind) = 'tag'
			  AND lower(t.name) IN (%s)
		)`, alias, alias, strings.Join(ph, ",")))
	}
	return clauses, args
}

// IsItemBlockedByParentalPolicy reports whether rec is blocked by maxRating or blockedTags.
func (r *LibraryRepo) IsItemBlockedByParentalPolicy(ctx context.Context, rec *ItemRecord, maxRating *int, blockedTags []string) (bool, error) {
	if rec == nil {
		return false, nil
	}
	if maxRating != nil {
		if score, ok := ParentalRatingScore(rec.OfficialRating); ok && score > *maxRating {
			return true, nil
		}
	}
	if normTags := normalizeBlockedTags(blockedTags); len(normTags) > 0 {
		ph := make([]string, len(normTags))
		args := make([]any, 0, len(normTags)+2)
		args = append(args, guid.Normalize(rec.ID), guid.Normalize(rec.SeriesID))
		for i, t := range normTags {
			ph[i] = "?"
			args = append(args, t)
		}
		var blocked int
		err := r.store.Reader.QueryRowContext(ctx, fmt.Sprintf(`
			SELECT EXISTS (
				SELECT 1 FROM item_taxonomies it
				JOIN taxonomies t ON t.id = it.taxonomy_id
				WHERE it.item_id IN (?, ?)
				  AND lower(t.kind) = 'tag'
				  AND lower(t.name) IN (%s)
			)`, strings.Join(ph, ",")), args...,
		).Scan(&blocked)
		if err != nil {
			return false, err
		}
		if blocked == 1 {
			return true, nil
		}
	}
	return false, nil
}

// ItemCounts holds aggregate item counts by type.
type ItemCounts struct {
	MovieCount      int `json:"MovieCount"`
	SeriesCount     int `json:"SeriesCount"`
	EpisodeCount    int `json:"EpisodeCount"`
	ArtistCount     int `json:"ArtistCount"`
	ProgramCount    int `json:"ProgramCount"`
	TrailerCount    int `json:"TrailerCount"`
	SongCount       int `json:"SongCount"`
	AlbumCount      int `json:"AlbumCount"`
	MusicVideoCount int `json:"MusicVideoCount"`
	BoxSetCount     int `json:"BoxSetCount"`
	BookCount       int `json:"BookCount"`
	ItemCount       int `json:"ItemCount"`
}

// LibraryRepo implements media library, item, search and user-data persistence.
type LibraryRepo struct {
	store *Store
}

func NewLibraryRepo(store *Store) *LibraryRepo {
	return &LibraryRepo{store: store}
}

// --- Library CRUD ---

func (r *LibraryRepo) CreateLibrary(ctx context.Context, name, collectionType string, paths []string) (*LibraryRecord, error) {
	if collectionType == "" {
		collectionType = "movies"
	}
	if paths == nil {
		paths = []string{}
	}
	now := time.Now().Unix()
	rec := &LibraryRecord{
		ID:             guid.New(),
		Name:           name,
		CollectionType: collectionType,
		Paths:          paths,
		OptionsJSON:    `{"SaveLocalMetadata":true}`,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	pathsRaw, err := json.Marshal(rec.Paths)
	if err != nil {
		return nil, err
	}
	_, err = r.store.Writer.ExecContext(ctx,
		`INSERT INTO libraries (id, name, collection_type, paths_json, options_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		rec.ID, rec.Name, rec.CollectionType, string(pathsRaw), rec.OptionsJSON, rec.CreatedAt, rec.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return rec, nil
}

func (r *LibraryRepo) ListLibraries(ctx context.Context) ([]LibraryRecord, error) {
	rows, err := r.store.Reader.QueryContext(ctx,
		`SELECT id, name, collection_type, paths_json, options_json, created_at, updated_at
		 FROM libraries ORDER BY created_at ASC, name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []LibraryRecord
	for rows.Next() {
		var rec LibraryRecord
		var pathsRaw string
		if err := rows.Scan(&rec.ID, &rec.Name, &rec.CollectionType, &pathsRaw, &rec.OptionsJSON, &rec.CreatedAt, &rec.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(pathsRaw), &rec.Paths)
		if rec.Paths == nil {
			rec.Paths = []string{}
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (r *LibraryRepo) GetLibrary(ctx context.Context, id string) (*LibraryRecord, error) {
	id = guid.Normalize(id)
	var rec LibraryRecord
	var pathsRaw string
	err := r.store.Reader.QueryRowContext(ctx,
		`SELECT id, name, collection_type, paths_json, options_json, created_at, updated_at
		 FROM libraries WHERE id = ?`, id,
	).Scan(&rec.ID, &rec.Name, &rec.CollectionType, &pathsRaw, &rec.OptionsJSON, &rec.CreatedAt, &rec.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(pathsRaw), &rec.Paths)
	if rec.Paths == nil {
		rec.Paths = []string{}
	}
	return &rec, nil
}

// UpdateLibraryOptions merges opts into the library's existing options_json and persists it.
func (r *LibraryRepo) UpdateLibraryOptions(ctx context.Context, id string, opts map[string]any) (*LibraryRecord, error) {
	lib, err := r.GetLibrary(ctx, id)
	if err != nil {
		return nil, err
	}
	merged := ParseLibraryOptions(lib.OptionsJSON)
	for k, v := range opts {
		merged[k] = v
	}
	if _, ok := merged["SaveLocalMetadata"]; !ok {
		merged["SaveLocalMetadata"] = true
	}
	raw, err := json.Marshal(merged)
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	if _, err := r.store.Writer.ExecContext(ctx,
		`UPDATE libraries SET options_json = ?, updated_at = ? WHERE id = ?`,
		string(raw), now, lib.ID,
	); err != nil {
		return nil, err
	}
	lib.OptionsJSON = string(raw)
	lib.UpdatedAt = now
	return lib, nil
}

func (r *LibraryRepo) DeleteLibrary(ctx context.Context, id string) error {
	id = guid.Normalize(id)
	tx, err := r.store.Writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM item_search_fts WHERE item_id IN (SELECT id FROM items WHERE library_id = ?)`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM item_images WHERE item_id IN (SELECT id FROM items WHERE library_id = ?)`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM item_ancestors WHERE ancestor_id = ? OR descendant_id IN (SELECT id FROM items WHERE library_id = ?)`, id, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM items WHERE library_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM libraries WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *LibraryRepo) DeleteLibraryByName(ctx context.Context, name string) error {
	var id string
	err := r.store.Reader.QueryRowContext(ctx, `SELECT id FROM libraries WHERE name = ? LIMIT 1`, name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return r.DeleteLibrary(ctx, id)
}

func (r *LibraryRepo) AddLibraryPath(ctx context.Context, id, path string) (*LibraryRecord, error) {
	lib, err := r.GetLibrary(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, p := range lib.Paths {
		if p == path {
			return lib, nil
		}
	}
	lib.Paths = append(lib.Paths, path)
	lib.UpdatedAt = time.Now().Unix()
	pathsRaw, err := json.Marshal(lib.Paths)
	if err != nil {
		return nil, err
	}
	_, err = r.store.Writer.ExecContext(ctx,
		`UPDATE libraries SET paths_json = ?, updated_at = ? WHERE id = ?`,
		string(pathsRaw), lib.UpdatedAt, lib.ID,
	)
	if err != nil {
		return nil, err
	}
	return lib, nil
}

// --- Item & Relation Writes ---

func (r *LibraryRepo) UpsertItem(ctx context.Context, item *ItemRecord) error {
	if item.ID == "" {
		item.ID = guid.New()
	} else {
		item.ID = guid.Normalize(item.ID)
	}
	now := time.Now().Unix()
	if item.DateCreated == 0 {
		item.DateCreated = now
	}
	if item.DateModified == 0 {
		item.DateModified = now
	}
	if item.SortName == "" {
		item.SortName = strings.ToLower(item.Name)
	}
	if item.Etag == "" {
		item.Etag = guid.New()[:16]
	}
	extraRaw, err := json.Marshal(item.Extra)
	if err != nil {
		return err
	}
	_, err = r.store.Writer.ExecContext(ctx, `
		INSERT INTO items (
			id, internal_id, library_id, parent_id, series_id, season_id,
			type, media_type, is_folder, is_strm,
			name, original_title, sort_name,
			index_number, parent_index_number, production_year,
			premiere_date, end_date, community_rating, critic_rating, official_rating,
			runtime_ticks, container, path, file_size, file_mtime,
			etag, extra_json, date_created, date_modified
		) VALUES (
			?, ?, ?, ?, ?, ?,
			?, ?, ?, ?,
			?, ?, ?,
			?, ?, ?,
			?, ?, ?, ?, ?,
			?, ?, ?, ?, ?,
			?, ?, ?, ?
		)
		ON CONFLICT(id) DO UPDATE SET
			library_id = excluded.library_id,
			parent_id = excluded.parent_id,
			series_id = excluded.series_id,
			season_id = excluded.season_id,
			type = excluded.type,
			media_type = excluded.media_type,
			is_folder = excluded.is_folder,
			is_strm = excluded.is_strm,
			name = excluded.name,
			original_title = excluded.original_title,
			sort_name = excluded.sort_name,
			index_number = excluded.index_number,
			parent_index_number = excluded.parent_index_number,
			production_year = excluded.production_year,
			premiere_date = excluded.premiere_date,
			end_date = excluded.end_date,
			community_rating = excluded.community_rating,
			critic_rating = excluded.critic_rating,
			official_rating = excluded.official_rating,
			runtime_ticks = excluded.runtime_ticks,
			container = excluded.container,
			path = excluded.path,
			file_size = excluded.file_size,
			file_mtime = excluded.file_mtime,
			etag = excluded.etag,
			extra_json = excluded.extra_json,
			date_modified = excluded.date_modified
	`,
		item.ID, item.InternalID, item.LibraryID, nullStr(item.ParentID), nullStr(item.SeriesID), nullStr(item.SeasonID),
		item.Type, nullStr(item.MediaType), boolToInt(item.IsFolder), boolToInt(item.IsStrm),
		item.Name, nullStr(item.OriginalTitle), item.SortName,
		item.IndexNumber, item.ParentIndexNumber, item.ProductionYear,
		item.PremiereDate, item.EndDate, item.CommunityRating, item.CriticRating, nullStr(item.OfficialRating),
		item.RuntimeTicks, nullStr(item.Container), nullStr(item.Path), item.FileSize, item.FileMtime,
		item.Etag, string(extraRaw), item.DateCreated, item.DateModified,
	)
	return err
}

// FindItemByPath looks up an existing item by library_id and path (for incremental scanning).
func (r *LibraryRepo) FindItemByPath(ctx context.Context, libraryID, path string) (*ItemRecord, error) {
	row := r.store.Reader.QueryRowContext(ctx,
		`SELECT id, library_id, parent_id, series_id, season_id, type, media_type, is_folder, is_strm,
		        name, original_title, sort_name, index_number, parent_index_number, production_year,
		        premiere_date, end_date, community_rating, critic_rating, official_rating,
		        runtime_ticks, container, path, file_size, file_mtime, etag, extra_json, date_created, date_modified
		 FROM items WHERE library_id = ? AND path = ? LIMIT 1`,
		guid.Normalize(libraryID), path,
	)
	rec, err := scanItemRecord(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return rec, err
}

func (r *LibraryRepo) ReplaceItemAncestors(ctx context.Context, itemID string, ancestors []AncestorLink) error {
	itemID = guid.Normalize(itemID)
	tx, err := r.store.Writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM item_ancestors WHERE descendant_id = ?`, itemID); err != nil {
		return err
	}
	for _, a := range ancestors {
		if a.AncestorID == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT OR REPLACE INTO item_ancestors (ancestor_id, descendant_id, depth) VALUES (?, ?, ?)`,
			guid.Normalize(a.AncestorID), itemID, a.Depth,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *LibraryRepo) ReplaceMediaSourcesAndStreams(ctx context.Context, itemID string, sources []MediaSourceRecord) error {
	itemID = guid.Normalize(itemID)
	tx, err := r.store.Writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM item_media_streams WHERE item_id = ?`, itemID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM item_media_sources WHERE item_id = ?`, itemID); err != nil {
		return err
	}
	for _, src := range sources {
		srcID := src.ID
		if srcID == "" {
			srcID = guid.New()
		}
		proto := src.Protocol
		if proto == "" {
			proto = "File"
		}
		extra := src.ExtraJSON
		if extra == "" {
			extra = "{}"
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO item_media_sources (
				id, item_id, name, path, protocol, container, size, bitrate, runtime_ticks,
				is_remote, supports_direct_play, supports_direct_stream, supports_transcoding, extra_json
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			srcID, itemID, src.Name, src.Path, proto, nullStr(src.Container), src.Size, src.Bitrate, src.RuntimeTicks,
			boolToInt(src.IsRemote), boolToInt(src.SupportsDirectPlay), boolToInt(src.SupportsDirectStream), boolToInt(src.SupportsTranscoding), extra,
		); err != nil {
			return err
		}
		for _, st := range src.Streams {
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
	}
	return tx.Commit()
}

func (r *LibraryRepo) ReplaceItemImages(ctx context.Context, itemID string, images []ImageRecord) error {
	itemID = guid.Normalize(itemID)
	tx, err := r.store.Writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM item_images WHERE item_id = ?`, itemID); err != nil {
		return err
	}
	for _, img := range images {
		tag := img.Tag
		if tag == "" {
			tag = guid.New()[:16]
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT OR REPLACE INTO item_images (item_id, image_type, image_index, path, tag, width, height, blurhash)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			itemID, img.ImageType, img.ImageIndex, img.Path, tag, img.Width, img.Height, nullStr(img.Blurhash),
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *LibraryRepo) UpsertImage(ctx context.Context, img *ImageRecord) error {
	if img == nil {
		return nil
	}
	itemID := guid.Normalize(img.ItemID)
	tag := img.Tag
	if tag == "" {
		tag = guid.New()[:16]
	}
	_, err := r.store.Writer.ExecContext(ctx, `
		INSERT OR REPLACE INTO item_images (item_id, image_type, image_index, path, tag, width, height, blurhash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		itemID, img.ImageType, img.ImageIndex, img.Path, tag, img.Width, img.Height, nullStr(img.Blurhash),
	)
	return err
}

func (r *LibraryRepo) ReplaceItemTaxonomies(ctx context.Context, itemID string, kind string, names []string) error {
	itemID = guid.Normalize(itemID)
	tx, err := r.store.Writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM item_taxonomies
		WHERE item_id = ? AND taxonomy_id IN (SELECT id FROM taxonomies WHERE kind = ?)`,
		itemID, kind,
	); err != nil {
		return err
	}

	seen := make(map[string]struct{}, len(names))
	for _, rawName := range names {
		name := strings.TrimSpace(rawName)
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}

		var taxID string
		err := tx.QueryRowContext(ctx, `SELECT id FROM taxonomies WHERE kind = ? AND name = ?`, kind, name).Scan(&taxID)
		if errors.Is(err, sql.ErrNoRows) {
			taxID = guid.New()
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO taxonomies (id, kind, name, sort_name) VALUES (?, ?, ?, ?)`,
				taxID, kind, name, strings.ToLower(name),
			); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO item_taxonomies (item_id, taxonomy_id) VALUES (?, ?)`,
			itemID, taxID,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *LibraryRepo) ReplaceItemPeople(ctx context.Context, itemID string, people []PersonInput) error {
	itemID = guid.Normalize(itemID)
	tx, err := r.store.Writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM item_people WHERE item_id = ?`, itemID); err != nil {
		return err
	}

	for i, p := range people {
		name := strings.TrimSpace(p.Name)
		if name == "" {
			continue
		}
		pType := p.Type
		if pType == "" {
			pType = "Actor"
		}
		var personID string
		err := tx.QueryRowContext(ctx, `SELECT id FROM people WHERE name = ?`, name).Scan(&personID)
		if errors.Is(err, sql.ErrNoRows) {
			personID = guid.New()
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO people (id, name, sort_name, extra_json) VALUES (?, ?, ?, '{}')`,
				personID, name, strings.ToLower(name),
			); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		sortOrder := p.SortOrder
		if sortOrder == 0 {
			sortOrder = i
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT OR REPLACE INTO item_people (item_id, person_id, role, person_type, sort_order) VALUES (?, ?, ?, ?, ?)`,
			itemID, personID, nullStr(p.Role), pType, sortOrder,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *LibraryRepo) UpsertSearchFTS(ctx context.Context, itemID, titleRaw, titleTokens, pinyinFull, pinyinInitials, peopleNames string) error {
	itemID = guid.Normalize(itemID)

	titleRaw = TradToSimp(titleRaw)
	peopleRaw := TradToSimp(peopleNames)
	if strings.TrimSpace(titleTokens) == "" && strings.TrimSpace(titleRaw) != "" {
		titleTokens = tokenizeForFTSSQLite(titleRaw)
	} else {
		titleTokens = TradToSimp(titleTokens)
	}
	if strings.TrimSpace(pinyinFull) == "" || strings.TrimSpace(pinyinInitials) == "" {
		autoFull, autoInitials := ToPinyin(strings.TrimSpace(titleRaw + " " + peopleRaw))
		if strings.TrimSpace(pinyinFull) == "" {
			pinyinFull = autoFull
		}
		if strings.TrimSpace(pinyinInitials) == "" {
			pinyinInitials = autoInitials
		}
	}
	if strings.TrimSpace(peopleRaw) != "" {
		peopleNames = tokenizeForFTSSQLite(peopleRaw)
	} else {
		peopleNames = ""
	}

	tx, err := r.store.Writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM item_search_fts WHERE item_id = ?`, itemID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO item_search_fts (item_id, title_raw, title_tokens, pinyin_full, pinyin_initials, people_names)
		VALUES (?, ?, ?, ?, ?, ?)`,
		itemID, titleRaw, titleTokens, pinyinFull, pinyinInitials, peopleNames,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// --- FTS5 Query Builder ---

// BuildFTSQuery converts a user search string into an FTS5 MATCH query supporting
// multi-term out-of-order Chinese/English fuzzy search with Traditional-to-Simplified
// normalization.
func BuildFTSQuery(term string) string {
	term = strings.TrimSpace(TradToSimp(term))
	if term == "" {
		return ""
	}
	// Split on whitespace and punctuation into logical tokens
	var rawTokens []string
	var cur strings.Builder
	for _, r := range term {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			if cur.Len() > 0 {
				rawTokens = append(rawTokens, cur.String())
				cur.Reset()
			}
			continue
		}
		cur.WriteRune(r)
	}
	if cur.Len() > 0 {
		rawTokens = append(rawTokens, cur.String())
	}

	var exprs []string
	seen := make(map[string]struct{})
	addTerm := func(t string) {
		t = strings.ReplaceAll(t, `"`, "")
		if t == "" {
			return
		}
		if _, ok := seen[t]; ok {
			return
		}
		seen[t] = struct{}{}
		exprs = append(exprs, fmt.Sprintf(`"%s"*`, t))
	}

	for _, tok := range rawTokens {
		// Separate contiguous CJK runs from ASCII/Latin runs
		runes := []rune(tok)
		i := 0
		for i < len(runes) {
			if isCJK(runes[i]) {
				j := i
				for j < len(runes) && isCJK(runes[j]) {
					j++
				}
				cjk := runes[i:j]
				if len(cjk) <= 2 {
					addTerm(string(cjk))
				} else {
					// Break into overlapping 2-grams so any length substring matches title_tokens bigrams
					for k := 0; k < len(cjk)-1; k++ {
						addTerm(string(cjk[k : k+2]))
					}
				}
				i = j
			} else {
				j := i
				for j < len(runes) && !isCJK(runes[j]) {
					j++
				}
				addTerm(strings.ToLower(string(runes[i:j])))
				i = j
			}
		}
	}
	if len(exprs) == 0 {
		return ""
	}
	return strings.Join(exprs, " AND ")
}

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r)
}

// --- Core Queries ---

func (r *LibraryRepo) QueryItems(ctx context.Context, f ItemFilter) ([]dto.BaseItemDto, int, error) {
	if f.AllowedLibraryIDs != nil && len(f.AllowedLibraryIDs) == 0 {
		return []dto.BaseItemDto{}, 0, nil
	}

	var (
		joins []string
		where []string
		args  []any
	)

	// Exclude local extras (trailers, special features, theme songs/videos) from normal library/poster-wall queries.
	where = append(where, `COALESCE(json_extract(i.extra_json, '$.extra_type'), '') = ''`)

	userID := guid.Normalize(f.UserID)
	if userID != "" {
		joins = append(joins, `LEFT JOIN user_item_data u ON u.item_id = i.id AND u.user_id = ?`)
		args = append(args, userID)
	}

	ftsActive := false
	if ftsExpr := BuildFTSQuery(f.SearchTerm); ftsExpr != "" {
		ftsActive = true
		joins = append(joins, `INNER JOIN item_search_fts fts ON fts.item_id = i.id`)
		where = append(where, `item_search_fts MATCH ?`)
		args = append(args, ftsExpr)
	}
	if prefix := strings.TrimSpace(f.NameStartsWith); prefix != "" {
		escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.ToLower(prefix))
		where = append(where, `lower(i.sort_name) LIKE ? ESCAPE '\'`)
		args = append(args, escaped+"%")
	}
	if gte := strings.TrimSpace(f.NameStartsWithOrGreater); gte != "" {
		where = append(where, `lower(i.sort_name) >= lower(?)`)
		args = append(args, gte)
	}
	if lt := strings.TrimSpace(f.NameLessThan); lt != "" {
		where = append(where, `lower(i.sort_name) < lower(?)`)
		args = append(args, lt)
	}

	if f.LibraryID != "" {
		where = append(where, `i.library_id = ?`)
		args = append(args, guid.Normalize(f.LibraryID))
	}
	if len(f.AllowedLibraryIDs) > 0 {
		placeholders := make([]string, len(f.AllowedLibraryIDs))
		for idx, lid := range f.AllowedLibraryIDs {
			placeholders[idx] = "?"
			args = append(args, guid.Normalize(lid))
		}
		where = append(where, fmt.Sprintf(`i.library_id IN (%s)`, strings.Join(placeholders, ",")))
	}
	if pClauses, pArgs := appendParentalFilterSQL("i", f.MaxParentalRating, f.BlockedTags); len(pClauses) > 0 {
		where = append(where, pClauses...)
		args = append(args, pArgs...)
	}
	if f.SeriesID != "" {
		where = append(where, `i.series_id = ?`)
		args = append(args, guid.Normalize(f.SeriesID))
	}
	if f.SeasonID != "" {
		where = append(where, `i.season_id = ?`)
		args = append(args, guid.Normalize(f.SeasonID))
	}
	if f.ParentID != "" {
		pid := guid.Normalize(f.ParentID)
		if f.Recursive {
			joins = append(joins, `INNER JOIN item_ancestors anc ON anc.descendant_id = i.id`)
			where = append(where, `anc.ancestor_id = ?`)
			args = append(args, pid)
		} else {
			where = append(where, `i.parent_id = ?`)
			args = append(args, pid)
		}
	}

	if len(f.IncludeItemTypes) > 0 {
		placeholders := make([]string, len(f.IncludeItemTypes))
		for idx, t := range f.IncludeItemTypes {
			placeholders[idx] = "?"
			args = append(args, t)
		}
		where = append(where, fmt.Sprintf(`i.type IN (%s)`, strings.Join(placeholders, ",")))
	}
	if len(f.ExcludeItemTypes) > 0 {
		placeholders := make([]string, len(f.ExcludeItemTypes))
		for idx, t := range f.ExcludeItemTypes {
			placeholders[idx] = "?"
			args = append(args, t)
		}
		where = append(where, fmt.Sprintf(`i.type NOT IN (%s)`, strings.Join(placeholders, ",")))
	}
	if len(f.IDs) > 0 {
		ph := make([]string, 0, len(f.IDs))
		for _, id := range f.IDs {
			if norm := guid.Normalize(id); norm != "" {
				ph = append(ph, "?")
				args = append(args, norm)
			}
		}
		if len(ph) > 0 {
			where = append(where, fmt.Sprintf(`i.id IN (%s)`, strings.Join(ph, ",")))
		}
	}
	if len(f.ExcludeItemIDs) > 0 {
		ph := make([]string, 0, len(f.ExcludeItemIDs))
		for _, id := range f.ExcludeItemIDs {
			if norm := guid.Normalize(id); norm != "" {
				ph = append(ph, "?")
				args = append(args, norm)
			}
		}
		if len(ph) > 0 {
			where = append(where, fmt.Sprintf(`i.id NOT IN (%s)`, strings.Join(ph, ",")))
		}
	}
	if len(f.Years) > 0 {
		placeholders := make([]string, len(f.Years))
		for idx, y := range f.Years {
			placeholders[idx] = "?"
			args = append(args, y)
		}
		where = append(where, fmt.Sprintf(`i.production_year IN (%s)`, strings.Join(placeholders, ",")))
	}
	if len(f.OfficialRatings) > 0 {
		ph := make([]string, 0, len(f.OfficialRatings))
		for _, or := range f.OfficialRatings {
			if trimmed := strings.ToUpper(strings.TrimSpace(or)); trimmed != "" {
				ph = append(ph, "?")
				args = append(args, trimmed)
			}
		}
		if len(ph) > 0 {
			where = append(where, fmt.Sprintf(`upper(trim(COALESCE(i.official_rating, ''))) IN (%s)`, strings.Join(ph, ",")))
		}
	}
	if f.MinCommunityRating != nil {
		where = append(where, `i.community_rating IS NOT NULL AND i.community_rating >= ?`)
		args = append(args, *f.MinCommunityRating)
	}

	if len(f.Genres) > 0 {
		ph := make([]string, len(f.Genres))
		for idx, g := range f.Genres {
			ph[idx] = "?"
			args = append(args, g)
		}
		where = append(where, fmt.Sprintf(`EXISTS (
			SELECT 1 FROM item_taxonomies it
			JOIN taxonomies tx ON tx.id = it.taxonomy_id
			WHERE it.item_id = i.id AND tx.kind = 'genre' AND tx.name IN (%s)
		)`, strings.Join(ph, ",")))
	}
	if len(f.GenreIDs) > 0 {
		ph := make([]string, 0, len(f.GenreIDs))
		for _, gid := range f.GenreIDs {
			if norm := guid.Normalize(gid); norm != "" {
				ph = append(ph, "?")
				args = append(args, norm)
			}
		}
		if len(ph) > 0 {
			where = append(where, fmt.Sprintf(`EXISTS (
				SELECT 1 FROM item_taxonomies it
				JOIN taxonomies tx ON tx.id = it.taxonomy_id
				WHERE it.item_id = i.id AND tx.kind = 'genre' AND tx.id IN (%s)
			)`, strings.Join(ph, ",")))
		}
	}
	if len(f.Studios) > 0 {
		ph := make([]string, len(f.Studios))
		for idx, s := range f.Studios {
			ph[idx] = "?"
			args = append(args, s)
		}
		where = append(where, fmt.Sprintf(`EXISTS (
			SELECT 1 FROM item_taxonomies it
			JOIN taxonomies tx ON tx.id = it.taxonomy_id
			WHERE it.item_id = i.id AND tx.kind = 'studio' AND tx.name IN (%s)
		)`, strings.Join(ph, ",")))
	}
	if len(f.StudioIDs) > 0 {
		ph := make([]string, 0, len(f.StudioIDs))
		for _, sid := range f.StudioIDs {
			if norm := guid.Normalize(sid); norm != "" {
				ph = append(ph, "?")
				args = append(args, norm)
			}
		}
		if len(ph) > 0 {
			where = append(where, fmt.Sprintf(`EXISTS (
				SELECT 1 FROM item_taxonomies it
				JOIN taxonomies tx ON tx.id = it.taxonomy_id
				WHERE it.item_id = i.id AND tx.kind = 'studio' AND tx.id IN (%s)
			)`, strings.Join(ph, ",")))
		}
	}
	if f.IsHD != nil {
		if *f.IsHD {
			where = append(where, `EXISTS (
				SELECT 1 FROM item_media_streams ms
				WHERE ms.item_id = i.id AND lower(ms.stream_type) = 'video'
				  AND (COALESCE(ms.height, 0) >= 720 OR COALESCE(ms.width, 0) >= 1280)
			)`)
		} else {
			where = append(where, `NOT EXISTS (
				SELECT 1 FROM item_media_streams ms
				WHERE ms.item_id = i.id AND lower(ms.stream_type) = 'video'
				  AND (COALESCE(ms.height, 0) >= 720 OR COALESCE(ms.width, 0) >= 1280)
			)`)
		}
	}
	if f.Is4K != nil {
		if *f.Is4K {
			where = append(where, `EXISTS (
				SELECT 1 FROM item_media_streams ms
				WHERE ms.item_id = i.id AND lower(ms.stream_type) = 'video'
				  AND (COALESCE(ms.width, 0) >= 3800 OR COALESCE(ms.height, 0) >= 2100)
			)`)
		} else {
			where = append(where, `NOT EXISTS (
				SELECT 1 FROM item_media_streams ms
				WHERE ms.item_id = i.id AND lower(ms.stream_type) = 'video'
				  AND (COALESCE(ms.width, 0) >= 3800 OR COALESCE(ms.height, 0) >= 2100)
			)`)
		}
	}
	if f.HasSubtitles != nil {
		if *f.HasSubtitles {
			where = append(where, `EXISTS (
				SELECT 1 FROM item_media_streams ms
				WHERE ms.item_id = i.id AND lower(ms.stream_type) = 'subtitle'
			)`)
		} else {
			where = append(where, `NOT EXISTS (
				SELECT 1 FROM item_media_streams ms
				WHERE ms.item_id = i.id AND lower(ms.stream_type) = 'subtitle'
			)`)
		}
	}
	if len(f.Tags) > 0 {
		ph := make([]string, len(f.Tags))
		for idx, t := range f.Tags {
			ph[idx] = "?"
			args = append(args, t)
		}
		where = append(where, fmt.Sprintf(`EXISTS (
			SELECT 1 FROM item_taxonomies it
			JOIN taxonomies tx ON tx.id = it.taxonomy_id
			WHERE it.item_id = i.id AND tx.kind = 'tag' AND tx.name IN (%s)
		)`, strings.Join(ph, ",")))
	}
	if len(f.PersonIDs) > 0 {
		ph := make([]string, len(f.PersonIDs))
		for idx, p := range f.PersonIDs {
			ph[idx] = "?"
			args = append(args, guid.Normalize(p))
		}
		where = append(where, fmt.Sprintf(`EXISTS (
			SELECT 1 FROM item_people ip WHERE ip.item_id = i.id AND ip.person_id IN (%s)
		)`, strings.Join(ph, ",")))
	}
	if len(f.Persons) > 0 {
		ph := make([]string, len(f.Persons))
		for idx, p := range f.Persons {
			ph[idx] = "?"
			args = append(args, p)
		}
		where = append(where, fmt.Sprintf(`EXISTS (
			SELECT 1 FROM item_people ip
			JOIN people p ON p.id = ip.person_id
			WHERE ip.item_id = i.id AND p.name IN (%s)
		)`, strings.Join(ph, ",")))
	}

	if userID != "" {
		if f.IsPlayed != nil && *f.IsPlayed {
			where = append(where, `COALESCE(u.played, 0) = 1`)
		}
		if f.IsUnplayed != nil && *f.IsUnplayed {
			where = append(where, `COALESCE(u.played, 0) = 0`)
		}
		if f.IsFavorite != nil && *f.IsFavorite {
			where = append(where, `COALESCE(u.is_favorite, 0) = 1`)
		}
		if f.IsResumable != nil && *f.IsResumable {
			where = append(where, `COALESCE(u.playback_position_ticks, 0) > 0 AND COALESCE(u.played, 0) = 0`)
		}
	}

	fromClause := `FROM items i`
	if len(joins) > 0 {
		fromClause += " " + strings.Join(joins, " ")
	}
	whereClause := ""
	if len(where) > 0 {
		whereClause = " WHERE " + strings.Join(where, " AND ")
	}

	total := 0
	countEnabled := f.EnableTotalRecordCount == nil || *f.EnableTotalRecordCount
	if countEnabled {
		countSQL := `SELECT COUNT(*) ` + fromClause + whereClause
		if err := r.store.Reader.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
			return nil, 0, fmt.Errorf("count items: %w", err)
		}
	}

	orderClause := buildOrderBy(f.SortBy, f.SortOrder, ftsActive, userID != "")

	selectCols := `
		i.id, i.library_id, i.parent_id, i.series_id, i.season_id,
		i.type, i.media_type, i.is_folder, i.is_strm,
		i.name, i.original_title, i.sort_name,
		i.index_number, i.parent_index_number, i.production_year,
		i.premiere_date, i.end_date, i.community_rating, i.critic_rating, i.official_rating,
		i.runtime_ticks, i.container, i.path, i.file_size, i.file_mtime,
		i.etag, i.extra_json, i.date_created, i.date_modified`
	if userID != "" {
		selectCols += `, COALESCE(u.played, 0), COALESCE(u.play_count, 0), COALESCE(u.playback_position_ticks, 0), COALESCE(u.is_favorite, 0), u.last_played_date`
	}

	querySQL := `SELECT ` + selectCols + ` ` + fromClause + whereClause + ` ` + orderClause
	queryArgs := append([]any(nil), args...)
	if f.Limit > 0 {
		querySQL += ` LIMIT ? OFFSET ?`
		queryArgs = append(queryArgs, f.Limit, max(0, f.StartIndex))
	} else if f.StartIndex > 0 {
		querySQL += ` LIMIT -1 OFFSET ?`
		queryArgs = append(queryArgs, f.StartIndex)
	}

	rows, err := r.store.Reader.QueryContext(ctx, querySQL, queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("query items: %w", err)
	}
	defer rows.Close()

	var recs []ItemRecord
	var userDataList []*dto.UserItemDataDto
	for rows.Next() {
		rec, ud, err := scanItemRow(rows, userID != "")
		if err != nil {
			return nil, 0, err
		}
		recs = append(recs, rec)
		userDataList = append(userDataList, ud)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	if !countEnabled {
		total = len(recs)
	}
	dtos, err := r.hydrateDTOs(ctx, recs, userDataList, f.ServerID, userID, f.Fields, false)
	if err != nil {
		return nil, 0, err
	}
	return dtos, total, nil
}

func (r *LibraryRepo) GetItemByID(ctx context.Context, serverID, userID, itemID string) (*dto.BaseItemDto, error) {
	itemID = guid.Normalize(itemID)
	userID = guid.Normalize(userID)

	var (
		querySQL string
		args     []any
	)
	if userID != "" {
		querySQL = `
			SELECT i.id, i.library_id, i.parent_id, i.series_id, i.season_id,
			       i.type, i.media_type, i.is_folder, i.is_strm,
			       i.name, i.original_title, i.sort_name,
			       i.index_number, i.parent_index_number, i.production_year,
			       i.premiere_date, i.end_date, i.community_rating, i.critic_rating, i.official_rating,
			       i.runtime_ticks, i.container, i.path, i.file_size, i.file_mtime,
			       i.etag, i.extra_json, i.date_created, i.date_modified,
			       COALESCE(u.played, 0), COALESCE(u.play_count, 0), COALESCE(u.playback_position_ticks, 0), COALESCE(u.is_favorite, 0), u.last_played_date
			FROM items i
			LEFT JOIN user_item_data u ON u.item_id = i.id AND u.user_id = ?
			WHERE i.id = ?`
		args = []any{userID, itemID}
	} else {
		querySQL = `
			SELECT i.id, i.library_id, i.parent_id, i.series_id, i.season_id,
			       i.type, i.media_type, i.is_folder, i.is_strm,
			       i.name, i.original_title, i.sort_name,
			       i.index_number, i.parent_index_number, i.production_year,
			       i.premiere_date, i.end_date, i.community_rating, i.critic_rating, i.official_rating,
			       i.runtime_ticks, i.container, i.path, i.file_size, i.file_mtime,
			       i.etag, i.extra_json, i.date_created, i.date_modified
			FROM items i
			WHERE i.id = ?`
		args = []any{itemID}
	}

	rows, err := r.store.Reader.QueryContext(ctx, querySQL, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, ErrNotFound
	}
	rec, ud, err := scanItemRow(rows, userID != "")
	if err != nil {
		return nil, err
	}
	dtos, err := r.hydrateDTOs(ctx, []ItemRecord{rec}, []*dto.UserItemDataDto{ud}, serverID, userID, nil, true)
	if err != nil {
		return nil, err
	}
	return &dtos[0], nil
}

// GetItemRecordByID returns the raw ItemRecord for internal services (playback/scanner).
func (r *LibraryRepo) GetItemRecordByID(ctx context.Context, itemID string) (*ItemRecord, error) {
	itemID = guid.Normalize(itemID)
	row := r.store.Reader.QueryRowContext(ctx,
		`SELECT id, library_id, parent_id, series_id, season_id, type, media_type, is_folder, is_strm,
		        name, original_title, sort_name, index_number, parent_index_number, production_year,
		        premiere_date, end_date, community_rating, critic_rating, official_rating,
		        runtime_ticks, container, path, file_size, file_mtime, etag, extra_json, date_created, date_modified
		 FROM items WHERE id = ?`, itemID)
	rec, err := scanItemRecord(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return rec, err
}

func (r *LibraryRepo) GetLatestItems(ctx context.Context, serverID, userID, parentID string, includeTypes []string, limit int, fields []string) ([]dto.BaseItemDto, error) {
	return r.GetLatestItemsWithPolicy(ctx, serverID, userID, parentID, includeTypes, limit, fields, nil, nil, nil)
}

func (r *LibraryRepo) GetLatestItemsWithAllowedLibraries(ctx context.Context, serverID, userID, parentID string, includeTypes []string, limit int, fields []string, allowedLibraryIDs []string) ([]dto.BaseItemDto, error) {
	return r.GetLatestItemsWithPolicy(ctx, serverID, userID, parentID, includeTypes, limit, fields, allowedLibraryIDs, nil, nil)
}

func (r *LibraryRepo) GetLatestItemsWithPolicy(ctx context.Context, serverID, userID, parentID string, includeTypes []string, limit int, fields []string, allowedLibraryIDs []string, maxParentalRating *int, blockedTags []string) ([]dto.BaseItemDto, error) {
	if limit <= 0 {
		limit = 16
	}
	if len(includeTypes) == 0 {
		includeTypes = []string{"Movie", "Series"}
	}
	disableCount := false
	items, _, err := r.QueryItems(ctx, ItemFilter{
		ServerID:               serverID,
		UserID:                 userID,
		ParentID:               parentID,
		AllowedLibraryIDs:      allowedLibraryIDs,
		MaxParentalRating:      maxParentalRating,
		BlockedTags:            blockedTags,
		Recursive:              parentID != "",
		IncludeItemTypes:       includeTypes,
		SortBy:                 []string{"DateCreated"},
		SortOrder:              "Descending",
		Limit:                  limit,
		Fields:                 fields,
		EnableTotalRecordCount: &disableCount,
	})
	return items, err
}

func (r *LibraryRepo) GetResumeItems(ctx context.Context, serverID, userID, parentID string, includeTypes []string, startIndex, limit int, fields []string) ([]dto.BaseItemDto, int, error) {
	return r.GetResumeItemsWithPolicy(ctx, serverID, userID, parentID, includeTypes, startIndex, limit, fields, nil, nil, nil)
}

func (r *LibraryRepo) GetResumeItemsWithAllowedLibraries(ctx context.Context, serverID, userID, parentID string, includeTypes []string, startIndex, limit int, fields []string, allowedLibraryIDs []string) ([]dto.BaseItemDto, int, error) {
	return r.GetResumeItemsWithPolicy(ctx, serverID, userID, parentID, includeTypes, startIndex, limit, fields, allowedLibraryIDs, nil, nil)
}

func (r *LibraryRepo) GetResumeItemsWithPolicy(ctx context.Context, serverID, userID, parentID string, includeTypes []string, startIndex, limit int, fields []string, allowedLibraryIDs []string, maxParentalRating *int, blockedTags []string) ([]dto.BaseItemDto, int, error) {
	if limit <= 0 {
		limit = 20
	}
	isResumable := true
	return r.QueryItems(ctx, ItemFilter{
		ServerID:          serverID,
		UserID:            userID,
		ParentID:          parentID,
		AllowedLibraryIDs: allowedLibraryIDs,
		MaxParentalRating: maxParentalRating,
		BlockedTags:       blockedTags,
		Recursive:         parentID != "",
		IncludeItemTypes:  includeTypes,
		IsResumable:       &isResumable,
		SortBy:            []string{"DatePlayed"},
		SortOrder:         "Descending",
		StartIndex:        startIndex,
		Limit:             limit,
		Fields:            fields,
	})
}

func (r *LibraryRepo) GetNextUpEpisodes(ctx context.Context, serverID, userID, seriesID string, limit int, fields []string) ([]dto.BaseItemDto, int, error) {
	return r.GetNextUpEpisodesWithPolicy(ctx, serverID, userID, seriesID, limit, fields, nil, nil, nil)
}

func (r *LibraryRepo) GetNextUpEpisodesWithAllowedLibraries(ctx context.Context, serverID, userID, seriesID string, limit int, fields []string, allowedLibraryIDs []string) ([]dto.BaseItemDto, int, error) {
	return r.GetNextUpEpisodesWithPolicy(ctx, serverID, userID, seriesID, limit, fields, allowedLibraryIDs, nil, nil)
}

func (r *LibraryRepo) GetNextUpEpisodesWithPolicy(ctx context.Context, serverID, userID, seriesID string, limit int, fields []string, allowedLibraryIDs []string, maxParentalRating *int, blockedTags []string) ([]dto.BaseItemDto, int, error) {
	if allowedLibraryIDs != nil && len(allowedLibraryIDs) == 0 {
		return []dto.BaseItemDto{}, 0, nil
	}
	if limit <= 0 {
		limit = 20
	}
	userID = guid.Normalize(userID)
	seriesID = guid.Normalize(seriesID)

	// Deduplicate per series BEFORE applying LIMIT, picking the earliest unplayed episode
	// per series and prioritizing series the user has already started watching.
	args := []any{userID, userID}
	var extraCond strings.Builder
	if seriesID != "" {
		extraCond.WriteString(" AND i.series_id = ?")
		args = append(args, seriesID)
	}
	if len(allowedLibraryIDs) > 0 {
		ph := make([]string, len(allowedLibraryIDs))
		for idx, lid := range allowedLibraryIDs {
			ph[idx] = "?"
			args = append(args, guid.Normalize(lid))
		}
		extraCond.WriteString(" AND i.library_id IN (" + strings.Join(ph, ",") + ")")
	}
	if pClauses, pArgs := appendParentalFilterSQL("i", maxParentalRating, blockedTags); len(pClauses) > 0 {
		for _, cl := range pClauses {
			extraCond.WriteString(" AND " + cl)
		}
		args = append(args, pArgs...)
	}
	args = append(args, limit)

	querySQL := `
		WITH series_activity AS (
			SELECT e.series_id,
			       MAX(COALESCE(ue.last_played_date, '')) AS last_played
			FROM items e
			INNER JOIN user_item_data ue ON ue.item_id = e.id AND ue.user_id = ?
			WHERE e.type = 'Episode'
			  AND COALESCE(e.series_id, '') != ''
			  AND (COALESCE(ue.played, 0) = 1 OR COALESCE(ue.playback_position_ticks, 0) > 0)
			GROUP BY e.series_id
		),
		ranked AS (
			SELECT i.id, i.library_id, i.parent_id, i.series_id, i.season_id,
			       i.type, i.media_type, i.is_folder, i.is_strm,
			       i.name, i.original_title, i.sort_name,
			       i.index_number, i.parent_index_number, i.production_year,
			       i.premiere_date, i.end_date, i.community_rating, i.critic_rating, i.official_rating,
			       i.runtime_ticks, i.container, i.path, i.file_size, i.file_mtime,
			       i.etag, i.extra_json, i.date_created, i.date_modified,
			       COALESCE(u.played, 0) AS u_played,
			       COALESCE(u.play_count, 0) AS u_play_count,
			       COALESCE(u.playback_position_ticks, 0) AS u_pos,
			       COALESCE(u.is_favorite, 0) AS u_fav,
			       u.last_played_date AS u_last_played,
			       CASE WHEN sa.series_id IS NOT NULL THEN 1 ELSE 0 END AS is_started,
			       COALESCE(sa.last_played, '') AS series_last_played,
			       ROW_NUMBER() OVER (
			           PARTITION BY CASE WHEN COALESCE(i.series_id, '') != '' THEN i.series_id ELSE i.id END
			           ORDER BY COALESCE(i.parent_index_number, 0) ASC, COALESCE(i.index_number, 0) ASC, i.id ASC
			       ) AS rn
			FROM items i
			LEFT JOIN user_item_data u ON u.item_id = i.id AND u.user_id = ?
			LEFT JOIN series_activity sa ON sa.series_id = i.series_id
			WHERE i.type = 'Episode' AND COALESCE(u.played, 0) = 0` + extraCond.String() + `
		)
		SELECT id, library_id, parent_id, series_id, season_id,
		       type, media_type, is_folder, is_strm,
		       name, original_title, sort_name,
		       index_number, parent_index_number, production_year,
		       premiere_date, end_date, community_rating, critic_rating, official_rating,
		       runtime_ticks, container, path, file_size, file_mtime,
		       etag, extra_json, date_created, date_modified,
		       u_played, u_play_count, u_pos, u_fav, u_last_played
		FROM ranked
		WHERE rn = 1
		ORDER BY is_started DESC, series_last_played DESC, date_created DESC, series_id ASC
		LIMIT ?`

	rows, err := r.store.Reader.QueryContext(ctx, querySQL, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var recs []ItemRecord
	var uds []*dto.UserItemDataDto
	for rows.Next() {
		rec, ud, err := scanItemRow(rows, true)
		if err != nil {
			return nil, 0, err
		}
		recs = append(recs, rec)
		uds = append(uds, ud)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	dtos, err := r.hydrateDTOs(ctx, recs, uds, serverID, userID, fields, false)
	if err != nil {
		return nil, 0, err
	}
	return dtos, len(dtos), nil
}

func (r *LibraryRepo) SearchHints(ctx context.Context, f SearchHintFilter) ([]dto.SearchHint, int, error) {
	if f.AllowedLibraryIDs != nil && len(f.AllowedLibraryIDs) == 0 {
		return []dto.SearchHint{}, 0, nil
	}
	ftsExpr := BuildFTSQuery(f.SearchTerm)
	if ftsExpr == "" {
		return []dto.SearchHint{}, 0, nil
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 24
	}

	var (
		joins []string
		where []string
		args  []any
	)

	// Exclude local extras (trailers, special features, theme songs/videos) from normal library/poster-wall queries.
	where = append(where, `COALESCE(json_extract(i.extra_json, '$.extra_type'), '') = ''`)
	joins = append(joins, `INNER JOIN item_search_fts fts ON fts.item_id = i.id`)
	where = append(where, `item_search_fts MATCH ?`)
	args = append(args, ftsExpr)

	if f.ParentID != "" {
		joins = append(joins, `INNER JOIN item_ancestors anc ON anc.descendant_id = i.id`)
		where = append(where, `anc.ancestor_id = ?`)
		args = append(args, guid.Normalize(f.ParentID))
	}
	if len(f.AllowedLibraryIDs) > 0 {
		ph := make([]string, len(f.AllowedLibraryIDs))
		for idx, lid := range f.AllowedLibraryIDs {
			ph[idx] = "?"
			args = append(args, guid.Normalize(lid))
		}
		where = append(where, fmt.Sprintf(`i.library_id IN (%s)`, strings.Join(ph, ",")))
	}
	if pClauses, pArgs := appendParentalFilterSQL("i", f.MaxParentalRating, f.BlockedTags); len(pClauses) > 0 {
		where = append(where, pClauses...)
		args = append(args, pArgs...)
	}
	if len(f.IncludeItemTypes) > 0 {
		ph := make([]string, len(f.IncludeItemTypes))
		for idx, t := range f.IncludeItemTypes {
			ph[idx] = "?"
			args = append(args, t)
		}
		where = append(where, fmt.Sprintf(`i.type IN (%s)`, strings.Join(ph, ",")))
	}

	querySQL := `
		SELECT i.id, i.name, i.production_year, i.type, i.media_type, i.runtime_ticks,
		       i.series_id, i.index_number, i.parent_index_number, i.extra_json
		FROM items i ` + strings.Join(joins, " ") + `
		WHERE ` + strings.Join(where, " AND ") + `
		ORDER BY bm25(item_search_fts, 0, 10.0, 5.0, 3.0, 2.0, 1.0) ASC
		LIMIT ?`
	args = append(args, limit)

	rows, err := r.store.Reader.QueryContext(ctx, querySQL, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("search hints: %w", err)
	}
	defer rows.Close()

	var hints []dto.SearchHint
	var ids []string
	for rows.Next() {
		var (
			h         dto.SearchHint
			mediaType sql.NullString
			seriesID  sql.NullString
			extraRaw  string
		)
		if err := rows.Scan(&h.Id, &h.Name, &h.ProductionYear, &h.Type, &mediaType, &h.RunTimeTicks,
			&seriesID, &h.IndexNumber, &h.ParentIndexNumber, &extraRaw); err != nil {
			return nil, 0, err
		}
		h.ItemId = h.Id
		h.MatchedTerm = f.SearchTerm
		h.MediaType = mediaType.String
		h.SeriesId = seriesID.String
		var extra ItemExtra
		_ = json.Unmarshal([]byte(extraRaw), &extra)
		h.Series = extra.SeriesName
		hints = append(hints, h)
		ids = append(ids, h.Id)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	// Hydrate Primary & Backdrop image tags
	if len(ids) > 0 {
		imgMap, err := r.loadImageTagsBatch(ctx, ids)
		if err == nil {
			for i := range hints {
				if m, ok := imgMap[hints[i].Id]; ok {
					hints[i].PrimaryImageTag = m.primary
					if len(m.backdrops) > 0 {
						hints[i].BackdropImageTag = m.backdrops[0]
						hints[i].BackdropImageItemId = hints[i].Id
					}
				}
			}
		}
	}
	if hints == nil {
		hints = []dto.SearchHint{}
	}
	return hints, len(hints), nil
}

// ListTaxonomies lists Genres, Studios, Persons, or Years as BaseItemDto.
func (r *LibraryRepo) ListTaxonomies(ctx context.Context, serverID, kind, parentID string, startIndex, limit int) ([]dto.BaseItemDto, int, error) {
	return r.ListTaxonomiesWithAllowedLibraries(ctx, serverID, kind, parentID, startIndex, limit, nil)
}

// ListTaxonomiesWithAllowedLibraries lists Genres, Studios, Persons, or Years scoped to allowedLibraryIDs.
// When allowedLibraryIDs is nil, all libraries are included; when non-nil and empty, an empty result is returned.
func (r *LibraryRepo) ListTaxonomiesWithAllowedLibraries(ctx context.Context, serverID, kind, parentID string, startIndex, limit int, allowedLibraryIDs []string) ([]dto.BaseItemDto, int, error) {
	return r.ListTaxonomiesWithPolicy(ctx, serverID, kind, parentID, startIndex, limit, allowedLibraryIDs, nil, nil)
}

// ListTaxonomiesWithPolicy lists Genres, Studios, Persons, or Years scoped to allowedLibraryIDs and parental policy.
func (r *LibraryRepo) ListTaxonomiesWithPolicy(ctx context.Context, serverID, kind, parentID string, startIndex, limit int, allowedLibraryIDs []string, maxParentalRating *int, blockedTags []string) ([]dto.BaseItemDto, int, error) {
	if limit <= 0 {
		limit = 200
	}
	if allowedLibraryIDs != nil && len(allowedLibraryIDs) == 0 {
		return []dto.BaseItemDto{}, 0, nil
	}
	parentID = guid.Normalize(parentID)
	hasParental := maxParentalRating != nil || len(normalizeBlockedTags(blockedTags)) > 0

	libInClause := func(col string) (string, []any) {
		if len(allowedLibraryIDs) == 0 {
			return "", nil
		}
		ph := make([]string, len(allowedLibraryIDs))
		vals := make([]any, len(allowedLibraryIDs))
		for i, id := range allowedLibraryIDs {
			ph[i] = "?"
			vals[i] = guid.Normalize(id)
		}
		return col + " IN (" + strings.Join(ph, ",") + ")", vals
	}

	switch strings.ToLower(kind) {
	case "year", "years":
		var (
			args       []any
			parentJoin string
			extraWhere strings.Builder
		)
		if parentID != "" {
			parentJoin = `INNER JOIN item_ancestors anc ON anc.descendant_id = i.id AND anc.ancestor_id = ?`
			args = append(args, parentID)
		}
		if clause, vals := libInClause("i.library_id"); clause != "" {
			extraWhere.WriteString(" AND " + clause)
			args = append(args, vals...)
		}
		if pClauses, pArgs := appendParentalFilterSQL("i", maxParentalRating, blockedTags); len(pClauses) > 0 {
			for _, cl := range pClauses {
				extraWhere.WriteString(" AND " + cl)
			}
			args = append(args, pArgs...)
		}
		rows, err := r.store.Reader.QueryContext(ctx,
			`SELECT DISTINCT i.production_year FROM items i `+parentJoin+`
			 WHERE i.production_year IS NOT NULL`+extraWhere.String()+` ORDER BY i.production_year DESC`, args...)
		if err != nil {
			return nil, 0, err
		}
		defer rows.Close()
		var all []dto.BaseItemDto
		for rows.Next() {
			var y int
			if err := rows.Scan(&y); err != nil {
				return nil, 0, err
			}
			yStr := fmt.Sprintf("%d", y)
			all = append(all, dto.BaseItemDto{
				Id:                yStr,
				Name:              yStr,
				ServerId:          serverID,
				Type:              "Year",
				ImageTags:         map[string]string{},
				BackdropImageTags: []string{},
			})
		}
		total := len(all)
		return paginateSlice(all, startIndex, limit), total, nil

	case "person", "persons":
		var (
			args  []any
			where string
		)
		if parentID != "" || len(allowedLibraryIDs) > 0 || hasParental {
			var joins, conds []string
			conds = append(conds, "ip.person_id = p.id")
			if parentID != "" {
				joins = append(joins, "JOIN item_ancestors anc ON anc.descendant_id = ip.item_id")
				conds = append(conds, "anc.ancestor_id = ?")
				args = append(args, parentID)
			}
			if len(allowedLibraryIDs) > 0 || hasParental {
				joins = append(joins, "JOIN items i ON i.id = ip.item_id")
				if clause, vals := libInClause("i.library_id"); clause != "" {
					conds = append(conds, clause)
					args = append(args, vals...)
				}
				if pClauses, pArgs := appendParentalFilterSQL("i", maxParentalRating, blockedTags); len(pClauses) > 0 {
					conds = append(conds, pClauses...)
					args = append(args, pArgs...)
				}
			}
			where = `WHERE EXISTS (SELECT 1 FROM item_people ip ` + strings.Join(joins, " ") + ` WHERE ` + strings.Join(conds, " AND ") + `)`
		}
		var total int
		if err := r.store.Reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM people p `+where, args...).Scan(&total); err != nil {
			return nil, 0, err
		}
		args = append(args, limit, max(0, startIndex))
		rows, err := r.store.Reader.QueryContext(ctx,
			`SELECT p.id, p.name, p.sort_name, COALESCE(img.tag, '')
			 FROM people p
			 LEFT JOIN item_images img ON img.item_id = p.id AND LOWER(img.image_type) = 'primary' AND img.image_index = 0
			 `+where+` ORDER BY p.sort_name ASC LIMIT ? OFFSET ?`, args...)
		if err != nil {
			return nil, 0, err
		}
		defer rows.Close()
		var out []dto.BaseItemDto
		for rows.Next() {
			var id, name, sortName, primaryTag string
			if err := rows.Scan(&id, &name, &sortName, &primaryTag); err != nil {
				return nil, 0, err
			}
			imageTags := map[string]string{}
			if primaryTag != "" {
				imageTags["Primary"] = primaryTag
			}
			out = append(out, dto.BaseItemDto{
				Id:                id,
				Name:              name,
				SortName:          sortName,
				ServerId:          serverID,
				Type:              "Person",
				ImageTags:         imageTags,
				BackdropImageTags: []string{},
			})
		}
		if out == nil {
			out = []dto.BaseItemDto{}
		}
		return out, total, rows.Err()

	default:
		// genre / studio / tag
		taxKind := strings.ToLower(strings.TrimSuffix(kind, "s"))
		dtoType := "Genre"
		if taxKind == "studio" {
			dtoType = "Studio"
		}
		var (
			args  = []any{taxKind}
			extra string
		)
		if parentID != "" || len(allowedLibraryIDs) > 0 || hasParental {
			var joins, conds []string
			conds = append(conds, "it.taxonomy_id = t.id")
			if parentID != "" {
				joins = append(joins, "JOIN item_ancestors anc ON anc.descendant_id = it.item_id")
				conds = append(conds, "anc.ancestor_id = ?")
				args = append(args, parentID)
			}
			if len(allowedLibraryIDs) > 0 || hasParental {
				joins = append(joins, "JOIN items i ON i.id = it.item_id")
				if clause, vals := libInClause("i.library_id"); clause != "" {
					conds = append(conds, clause)
					args = append(args, vals...)
				}
				if pClauses, pArgs := appendParentalFilterSQL("i", maxParentalRating, blockedTags); len(pClauses) > 0 {
					conds = append(conds, pClauses...)
					args = append(args, pArgs...)
				}
			}
			extra = ` AND EXISTS (SELECT 1 FROM item_taxonomies it ` + strings.Join(joins, " ") + ` WHERE ` + strings.Join(conds, " AND ") + `)`
		}
		var total int
		if err := r.store.Reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM taxonomies t WHERE t.kind = ?`+extra, args...).Scan(&total); err != nil {
			return nil, 0, err
		}
		args = append(args, limit, max(0, startIndex))
		rows, err := r.store.Reader.QueryContext(ctx,
			`SELECT t.id, t.name, t.sort_name FROM taxonomies t WHERE t.kind = ?`+extra+` ORDER BY t.sort_name ASC LIMIT ? OFFSET ?`, args...)
		if err != nil {
			return nil, 0, err
		}
		defer rows.Close()
		var out []dto.BaseItemDto
		for rows.Next() {
			var id, name, sortName string
			if err := rows.Scan(&id, &name, &sortName); err != nil {
				return nil, 0, err
			}
			out = append(out, dto.BaseItemDto{
				Id:                id,
				Name:              name,
				SortName:          sortName,
				ServerId:          serverID,
				Type:              dtoType,
				ImageTags:         map[string]string{},
				BackdropImageTags: []string{},
			})
		}
		if out == nil {
			out = []dto.BaseItemDto{}
		}
		return out, total, rows.Err()
	}
}

func (r *LibraryRepo) GetItemCounts(ctx context.Context) (*ItemCounts, error) {
	return r.GetItemCountsWithAllowedLibraries(ctx, nil)
}

// GetItemCountsWithAllowedLibraries counts items grouped by type, optionally restricted to allowedLibraryIDs.
func (r *LibraryRepo) GetItemCountsWithAllowedLibraries(ctx context.Context, allowedLibraryIDs []string) (*ItemCounts, error) {
	if allowedLibraryIDs != nil && len(allowedLibraryIDs) == 0 {
		return &ItemCounts{}, nil
	}
	query := `SELECT type, COUNT(*) FROM items WHERE COALESCE(json_extract(extra_json, '$.extra_type'), '') = ''`
	var args []any
	if len(allowedLibraryIDs) > 0 {
		ph := make([]string, len(allowedLibraryIDs))
		args = make([]any, len(allowedLibraryIDs))
		for i, id := range allowedLibraryIDs {
			ph[i] = "?"
			args[i] = guid.Normalize(id)
		}
		query += ` AND library_id IN (` + strings.Join(ph, ",") + `)`
	}
	query += ` GROUP BY type`

	rows, err := r.store.Reader.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	c := &ItemCounts{}
	for rows.Next() {
		var t string
		var cnt int
		if err := rows.Scan(&t, &cnt); err != nil {
			return nil, err
		}
		c.ItemCount += cnt
		switch t {
		case "Movie":
			c.MovieCount = cnt
		case "Series":
			c.SeriesCount = cnt
		case "Episode":
			c.EpisodeCount = cnt
		case "MusicArtist":
			c.ArtistCount = cnt
		case "Audio":
			c.SongCount = cnt
		case "MusicAlbum":
			c.AlbumCount = cnt
		case "MusicVideo":
			c.MusicVideoCount = cnt
		case "BoxSet":
			c.BoxSetCount = cnt
		case "Book":
			c.BookCount = cnt
		}
	}
	return c, rows.Err()
}

// --- User Item Data ---

func (r *LibraryRepo) GetUserItemData(ctx context.Context, userID, itemID string) (*dto.UserItemDataDto, error) {
	userID = guid.Normalize(userID)
	itemID = guid.Normalize(itemID)
	var (
		played, fav int
		playCount   int
		pos         int64
		lastPlayed  sql.NullInt64
	)
	err := r.store.Reader.QueryRowContext(ctx, `
		SELECT played, play_count, playback_position_ticks, is_favorite, last_played_date
		FROM user_item_data WHERE user_id = ? AND item_id = ?`,
		userID, itemID,
	).Scan(&played, &playCount, &pos, &fav, &lastPlayed)
	if errors.Is(err, sql.ErrNoRows) {
		return &dto.UserItemDataDto{Key: itemID}, nil
	}
	if err != nil {
		return nil, err
	}
	ud := &dto.UserItemDataDto{
		PlaybackPositionTicks: pos,
		PlayCount:             playCount,
		IsFavorite:            fav == 1,
		Played:                played == 1,
		Key:                   itemID,
	}
	if lastPlayed.Valid && lastPlayed.Int64 > 0 {
		t := time.Unix(lastPlayed.Int64, 0).UTC()
		ud.LastPlayedDate = &t
	}
	return ud, nil
}

func (r *LibraryRepo) collectCascadeItemIDs(ctx context.Context, tx *sql.Tx, itemID string) ([]string, error) {
	seen := map[string]struct{}{itemID: {}}
	ids := []string{itemID}
	queue := []string{itemID}

	for len(queue) > 0 {
		curID := queue[0]
		queue = queue[1:]

		var itemType string
		err := tx.QueryRowContext(ctx, `SELECT type FROM items WHERE id = ?`, curID).Scan(&itemType)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}

		var rows *sql.Rows
		switch strings.ToLower(itemType) {
		case "series":
			rows, err = tx.QueryContext(ctx,
				`SELECT id FROM items WHERE (series_id = ? OR parent_id = ?) AND COALESCE(json_extract(extra_json, '$.extra_type'), '') = ''`,
				curID, curID,
			)
		case "season":
			rows, err = tx.QueryContext(ctx,
				`SELECT id FROM items WHERE (season_id = ? OR parent_id = ?) AND COALESCE(json_extract(extra_json, '$.extra_type'), '') = ''`,
				curID, curID,
			)
		case "musicalbum":
			rows, err = tx.QueryContext(ctx,
				`SELECT id FROM items WHERE parent_id = ? AND COALESCE(json_extract(extra_json, '$.extra_type'), '') = ''`,
				curID,
			)
		case "boxset":
			rows, err = tx.QueryContext(ctx,
				`SELECT item_id FROM item_collections WHERE container_id = ?
				 UNION
				 SELECT id FROM items WHERE parent_id = ? AND COALESCE(json_extract(extra_json, '$.extra_type'), '') = ''`,
				curID, curID,
			)
		default:
			continue
		}
		if err != nil {
			return nil, err
		}

		var children []string
		for rows.Next() {
			var childID string
			if err := rows.Scan(&childID); err != nil {
				rows.Close()
				return nil, err
			}
			if childID != "" {
				children = append(children, childID)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()

		for _, childID := range children {
			if _, ok := seen[childID]; !ok {
				seen[childID] = struct{}{}
				ids = append(ids, childID)
				queue = append(queue, childID)
			}
		}
	}
	return ids, nil
}

func (r *LibraryRepo) MarkPlayed(ctx context.Context, userID, itemID string) (*dto.UserItemDataDto, error) {
	userID = guid.Normalize(userID)
	itemID = guid.Normalize(itemID)
	now := time.Now().Unix()

	tx, err := r.store.Writer.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	targetIDs, err := r.collectCascadeItemIDs(ctx, tx, itemID)
	if err != nil {
		return nil, err
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO user_item_data (user_id, item_id, played, play_count, playback_position_ticks, last_played_date)
		VALUES (?, ?, 1, 1, 0, ?)
		ON CONFLICT(user_id, item_id) DO UPDATE SET
			played = 1,
			play_count = CASE WHEN user_item_data.played = 1 THEN user_item_data.play_count ELSE user_item_data.play_count + 1 END,
			playback_position_ticks = 0,
			last_played_date = excluded.last_played_date`)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	for _, tid := range targetIDs {
		if _, err := stmt.ExecContext(ctx, userID, tid, now); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetUserItemData(ctx, userID, itemID)
}

func (r *LibraryRepo) MarkUnplayed(ctx context.Context, userID, itemID string) (*dto.UserItemDataDto, error) {
	userID = guid.Normalize(userID)
	itemID = guid.Normalize(itemID)

	tx, err := r.store.Writer.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	targetIDs, err := r.collectCascadeItemIDs(ctx, tx, itemID)
	if err != nil {
		return nil, err
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO user_item_data (user_id, item_id, played, play_count, playback_position_ticks)
		VALUES (?, ?, 0, 0, 0)
		ON CONFLICT(user_id, item_id) DO UPDATE SET
			played = 0,
			playback_position_ticks = 0`)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	for _, tid := range targetIDs {
		if _, err := stmt.ExecContext(ctx, userID, tid); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetUserItemData(ctx, userID, itemID)
}

func (r *LibraryRepo) MarkFavorite(ctx context.Context, userID, itemID string) (*dto.UserItemDataDto, error) {
	userID = guid.Normalize(userID)
	itemID = guid.Normalize(itemID)
	_, err := r.store.Writer.ExecContext(ctx, `
		INSERT INTO user_item_data (user_id, item_id, is_favorite)
		VALUES (?, ?, 1)
		ON CONFLICT(user_id, item_id) DO UPDATE SET is_favorite = 1`,
		userID, itemID,
	)
	if err != nil {
		return nil, err
	}
	return r.GetUserItemData(ctx, userID, itemID)
}

func (r *LibraryRepo) UnmarkFavorite(ctx context.Context, userID, itemID string) (*dto.UserItemDataDto, error) {
	userID = guid.Normalize(userID)
	itemID = guid.Normalize(itemID)
	_, err := r.store.Writer.ExecContext(ctx, `
		INSERT INTO user_item_data (user_id, item_id, is_favorite)
		VALUES (?, ?, 0)
		ON CONFLICT(user_id, item_id) DO UPDATE SET is_favorite = 0`,
		userID, itemID,
	)
	if err != nil {
		return nil, err
	}
	return r.GetUserItemData(ctx, userID, itemID)
}

// --- MediaSources & Images ---

func (r *LibraryRepo) GetMediaSourcesByItemID(ctx context.Context, itemID string) ([]MediaSourceRecord, error) {
	itemID = guid.Normalize(itemID)
	rows, err := r.store.Reader.QueryContext(ctx, `
		SELECT id, item_id, name, path, protocol, container, COALESCE(size,0), COALESCE(bitrate,0),
		       COALESCE(runtime_ticks,0), is_remote, supports_direct_play, supports_direct_stream,
		       supports_transcoding, extra_json
		FROM item_media_sources WHERE item_id = ?`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sources []MediaSourceRecord
	srcIndex := make(map[string]int)
	for rows.Next() {
		var (
			s          MediaSourceRecord
			container  sql.NullString
			isRem, dp, ds, tc int
		)
		if err := rows.Scan(&s.ID, &s.ItemID, &s.Name, &s.Path, &s.Protocol, &container,
			&s.Size, &s.Bitrate, &s.RuntimeTicks, &isRem, &dp, &ds, &tc, &s.ExtraJSON); err != nil {
			return nil, err
		}
		s.Container = container.String
		s.IsRemote = isRem == 1
		s.SupportsDirectPlay = dp == 1
		s.SupportsDirectStream = ds == 1
		s.SupportsTranscoding = tc == 1
		srcIndex[s.ID] = len(sources)
		sources = append(sources, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(sources) == 0 {
		return sources, nil
	}

	sRows, err := r.store.Reader.QueryContext(ctx, `
		SELECT media_source_id, item_id, stream_index, stream_type, codec, language, sub_variant,
		       display_title, title, COALESCE(width,0), COALESCE(height,0), video_range, audio_layout,
		       COALESCE(channels,0), COALESCE(sample_rate,0), COALESCE(bitrate,0),
		       is_default, is_forced, is_external, path
		FROM item_media_streams WHERE item_id = ? ORDER BY stream_index ASC`, itemID)
	if err != nil {
		return nil, err
	}
	defer sRows.Close()

	for sRows.Next() {
		var (
			st                                                                  MediaStreamRecord
			codec, lang, subVar, dispTitle, title, vRange, aLayout, path        sql.NullString
			isDef, isForced, isExt                                              int
		)
		if err := sRows.Scan(&st.MediaSourceID, &st.ItemID, &st.StreamIndex, &st.StreamType,
			&codec, &lang, &subVar, &dispTitle, &title, &st.Width, &st.Height, &vRange, &aLayout,
			&st.Channels, &st.SampleRate, &st.Bitrate, &isDef, &isForced, &isExt, &path); err != nil {
			return nil, err
		}
		st.Codec = codec.String
		st.Language = lang.String
		st.SubVariant = subVar.String
		st.DisplayTitle = dispTitle.String
		st.Title = title.String
		st.VideoRange = vRange.String
		st.AudioLayout = aLayout.String
		st.IsDefault = isDef == 1
		st.IsForced = isForced == 1
		st.IsExternal = isExt == 1
		st.Path = path.String

		if idx, ok := srcIndex[st.MediaSourceID]; ok {
			sources[idx].Streams = append(sources[idx].Streams, st)
		}
	}
	return sources, sRows.Err()
}

func (r *LibraryRepo) GetImageByItemAndType(ctx context.Context, itemID, imageType string, index int) (*ImageRecord, error) {
	itemID = guid.Normalize(itemID)
	var (
		rec      ImageRecord
		w, h     sql.NullInt64
		blurhash sql.NullString
	)
	err := r.store.Reader.QueryRowContext(ctx, `
		SELECT item_id, image_type, image_index, path, tag, width, height, blurhash
		FROM item_images
		WHERE item_id = ? AND lower(image_type) = lower(?) AND image_index = ?`,
		itemID, imageType, index,
	).Scan(&rec.ItemID, &rec.ImageType, &rec.ImageIndex, &rec.Path, &rec.Tag, &w, &h, &blurhash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rec.Width = int(w.Int64)
	rec.Height = int(h.Int64)
	rec.Blurhash = blurhash.String
	return &rec, nil
}

// --- Local Extras ---

// GetItemExtras returns child extra items (trailers, special features, theme songs/videos)
// belonging to parentID, optionally filtered by extraTypes (case-insensitive).
func (r *LibraryRepo) GetItemExtras(ctx context.Context, serverID, userID, parentID string, extraTypes []string) ([]dto.BaseItemDto, error) {
	parentID = guid.Normalize(parentID)
	userID = guid.Normalize(userID)
	if parentID == "" {
		return []dto.BaseItemDto{}, nil
	}

	var (
		joins string
		args  []any
	)
	selectCols := `
		i.id, i.library_id, i.parent_id, i.series_id, i.season_id,
		i.type, i.media_type, i.is_folder, i.is_strm,
		i.name, i.original_title, i.sort_name,
		i.index_number, i.parent_index_number, i.production_year,
		i.premiere_date, i.end_date, i.community_rating, i.critic_rating, i.official_rating,
		i.runtime_ticks, i.container, i.path, i.file_size, i.file_mtime,
		i.etag, i.extra_json, i.date_created, i.date_modified`
	if userID != "" {
		joins = `LEFT JOIN user_item_data u ON u.item_id = i.id AND u.user_id = ?`
		selectCols += `, COALESCE(u.played, 0), COALESCE(u.play_count, 0), COALESCE(u.playback_position_ticks, 0), COALESCE(u.is_favorite, 0), u.last_played_date`
		args = append(args, userID)
	}

	where := []string{
		`i.parent_id = ?`,
		`COALESCE(json_extract(i.extra_json, '$.extra_type'), '') != ''`,
	}
	args = append(args, parentID)

	if len(extraTypes) > 0 {
		ph := make([]string, len(extraTypes))
		for idx, et := range extraTypes {
			ph[idx] = "?"
			args = append(args, strings.ToLower(strings.TrimSpace(et)))
		}
		where = append(where, `LOWER(COALESCE(json_extract(i.extra_json, '$.extra_type'), '')) IN (`+strings.Join(ph, ",")+`)`)
	}

	querySQL := `SELECT ` + selectCols + ` FROM items i ` + joins + ` WHERE ` + strings.Join(where, " AND ") + ` ORDER BY i.sort_name ASC`
	rows, err := r.store.Reader.QueryContext(ctx, querySQL, args...)
	if err != nil {
		return nil, fmt.Errorf("get item extras: %w", err)
	}
	defer rows.Close()

	var recs []ItemRecord
	var uds []*dto.UserItemDataDto
	for rows.Next() {
		rec, ud, err := scanItemRow(rows, userID != "")
		if err != nil {
			return nil, err
		}
		recs = append(recs, rec)
		uds = append(uds, ud)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(recs) == 0 {
		return []dto.BaseItemDto{}, nil
	}
	return r.hydrateDTOs(ctx, recs, uds, serverID, userID, []string{"MediaSources", "MediaStreams", "Path"}, true)
}

// --- Hydration & Internal Helpers ---

func chunkStrings(items []string, size int) [][]string {
	if size <= 0 || len(items) == 0 {
		return nil
	}
	chunks := make([][]string, 0, (len(items)+size-1)/size)
	for start := 0; start < len(items); start += size {
		end := start + size
		if end > len(items) {
			end = len(items)
		}
		chunks = append(chunks, items[start:end])
	}
	return chunks
}

type imageTagsSummary struct {
	primary   string
	allTags   map[string]string
	backdrops []string
}

func (r *LibraryRepo) loadImageTagsBatch(ctx context.Context, itemIDs []string) (map[string]*imageTagsSummary, error) {
	out := make(map[string]*imageTagsSummary, len(itemIDs))
	if len(itemIDs) == 0 {
		return out, nil
	}
	for _, chunk := range chunkStrings(itemIDs, sqliteInChunkSize) {
		ph := make([]string, len(chunk))
		args := make([]any, len(chunk))
		for i, id := range chunk {
			ph[i] = "?"
			args[i] = id
		}
		rows, err := r.store.Reader.QueryContext(ctx,
			`SELECT item_id, image_type, image_index, tag FROM item_images WHERE item_id IN (`+strings.Join(ph, ",")+`) ORDER BY image_index ASC`,
			args...,
		)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var itemID, imgType, tag string
			var idx int
			if err := rows.Scan(&itemID, &imgType, &idx, &tag); err != nil {
				rows.Close()
				return nil, err
			}
			sum := out[itemID]
			if sum == nil {
				sum = &imageTagsSummary{allTags: make(map[string]string)}
				out[itemID] = sum
			}
			if strings.EqualFold(imgType, "Backdrop") {
				sum.backdrops = append(sum.backdrops, tag)
			} else {
				if idx == 0 {
					sum.allTags[imgType] = tag
				}
				if strings.EqualFold(imgType, "Primary") && idx == 0 {
					sum.primary = tag
				}
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

type extraCounts struct {
	trailers int
	specials int
}

func (r *LibraryRepo) loadExtrasCountsBatch(ctx context.Context, parentIDs []string) (map[string]*extraCounts, error) {
	out := make(map[string]*extraCounts)
	for _, chunk := range chunkStrings(parentIDs, sqliteInChunkSize) {
		ph := make([]string, len(chunk))
		args := make([]any, len(chunk))
		for i, id := range chunk {
			ph[i] = "?"
			args[i] = id
		}
		rows, err := r.store.Reader.QueryContext(ctx, `
			SELECT parent_id, LOWER(COALESCE(json_extract(extra_json, '$.extra_type'), '')), COUNT(*)
			FROM items
			WHERE parent_id IN (`+strings.Join(ph, ",")+`)
			  AND COALESCE(json_extract(extra_json, '$.extra_type'), '') != ''
			GROUP BY parent_id, LOWER(COALESCE(json_extract(extra_json, '$.extra_type'), ''))`,
			args...,
		)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var pid, et string
			var cnt int
			if err := rows.Scan(&pid, &et, &cnt); err != nil {
				rows.Close()
				return nil, err
			}
			ec := out[pid]
			if ec == nil {
				ec = &extraCounts{}
				out[pid] = ec
			}
			switch et {
			case "trailer":
				ec.trailers += cnt
			case "themesong", "themevideo":
				// Theme media are counted separately from trailers/special features.
			default:
				ec.specials += cnt
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

type containerPlayStats struct {
	childCount     int
	recursiveCount int
	unplayedCount  int
}

func (r *LibraryRepo) loadSeriesSeasonStatsBatch(ctx context.Context, userID string, seriesIDs, seasonIDs []string) (map[string]*containerPlayStats, error) {
	out := make(map[string]*containerPlayStats, len(seriesIDs)+len(seasonIDs))
	for _, chunk := range chunkStrings(seriesIDs, sqliteInChunkSize) {
		ph := make([]string, len(chunk))
		args := make([]any, len(chunk))
		for i, id := range chunk {
			ph[i] = "?"
			args[i] = id
			out[id] = &containerPlayStats{}
		}
		inClause := strings.Join(ph, ",")

		// Direct season count per series
		sRows, err := r.store.Reader.QueryContext(ctx,
			`SELECT series_id, COUNT(*) FROM items WHERE type = 'Season' AND series_id IN (`+inClause+`) GROUP BY series_id`,
			args...,
		)
		if err != nil {
			return nil, err
		}
		for sRows.Next() {
			var sid string
			var cnt int
			if err := sRows.Scan(&sid, &cnt); err != nil {
				sRows.Close()
				return nil, err
			}
			if st := out[sid]; st != nil {
				st.childCount = cnt
			}
		}
		if err := sRows.Err(); err != nil {
			sRows.Close()
			return nil, err
		}
		sRows.Close()

		// Episode total & unplayed count per series
		epArgs := make([]any, 0, len(chunk)+1)
		epArgs = append(epArgs, userID)
		epArgs = append(epArgs, args...)
		epRows, err := r.store.Reader.QueryContext(ctx,
			`SELECT i.series_id, COUNT(*), SUM(CASE WHEN COALESCE(u.played, 0) = 0 THEN 1 ELSE 0 END)
			 FROM items i
			 LEFT JOIN user_item_data u ON u.item_id = i.id AND u.user_id = ?
			 WHERE i.type = 'Episode' AND i.series_id IN (`+inClause+`)
			 GROUP BY i.series_id`,
			epArgs...,
		)
		if err != nil {
			return nil, err
		}
		for epRows.Next() {
			var sid string
			var total, unplayed int
			if err := epRows.Scan(&sid, &total, &unplayed); err != nil {
				epRows.Close()
				return nil, err
			}
			if st := out[sid]; st != nil {
				st.recursiveCount = total
				st.unplayedCount = unplayed
			}
		}
		if err := epRows.Err(); err != nil {
			epRows.Close()
			return nil, err
		}
		epRows.Close()
	}

	for _, chunk := range chunkStrings(seasonIDs, sqliteInChunkSize) {
		ph := make([]string, len(chunk))
		epArgs := make([]any, 0, len(chunk)+1)
		epArgs = append(epArgs, userID)
		for i, id := range chunk {
			ph[i] = "?"
			epArgs = append(epArgs, id)
			out[id] = &containerPlayStats{}
		}
		inClause := strings.Join(ph, ",")

		epRows, err := r.store.Reader.QueryContext(ctx,
			`SELECT i.season_id, COUNT(*), SUM(CASE WHEN COALESCE(u.played, 0) = 0 THEN 1 ELSE 0 END)
			 FROM items i
			 LEFT JOIN user_item_data u ON u.item_id = i.id AND u.user_id = ?
			 WHERE i.type = 'Episode' AND i.season_id IN (`+inClause+`)
			 GROUP BY i.season_id`,
			epArgs...,
		)
		if err != nil {
			return nil, err
		}
		for epRows.Next() {
			var sid string
			var total, unplayed int
			if err := epRows.Scan(&sid, &total, &unplayed); err != nil {
				epRows.Close()
				return nil, err
			}
			if st := out[sid]; st != nil {
				st.childCount = total
				st.recursiveCount = total
				st.unplayedCount = unplayed
			}
		}
		if err := epRows.Err(); err != nil {
			epRows.Close()
			return nil, err
		}
		epRows.Close()
	}

	return out, nil
}

func (r *LibraryRepo) hydrateDTOs(ctx context.Context, recs []ItemRecord, uds []*dto.UserItemDataDto, serverID, userID string, fields []string, fullDetail bool) ([]dto.BaseItemDto, error) {
	if len(recs) == 0 {
		return []dto.BaseItemDto{}, nil
	}
	fieldSet := make(map[string]bool, len(fields))
	for _, f := range fields {
		fieldSet[strings.ToLower(strings.TrimSpace(f))] = true
	}
	wantOverview := fullDetail || fieldSet["overview"]
	wantPath := fullDetail || fieldSet["path"]
	wantProviderIDs := fullDetail || fieldSet["providerids"]
	wantGenres := fullDetail || fieldSet["genres"]
	wantStudios := fullDetail || fieldSet["studios"]
	wantPeople := fullDetail || fieldSet["people"]
	wantMediaSources := fullDetail || fieldSet["mediasources"] || fieldSet["mediastreams"]

	ids := make([]string, len(recs))
	imgIDSet := make(map[string]struct{}, len(recs)*2)
	imgIDs := make([]string, 0, len(recs)*2)
	var seriesIDs, seasonIDs []string

	for i, rec := range recs {
		ids[i] = rec.ID
		if _, ok := imgIDSet[rec.ID]; !ok {
			imgIDSet[rec.ID] = struct{}{}
			imgIDs = append(imgIDs, rec.ID)
		}
		switch strings.ToLower(rec.Type) {
		case "series":
			seriesIDs = append(seriesIDs, rec.ID)
		case "season":
			seasonIDs = append(seasonIDs, rec.ID)
			parentRef := rec.SeriesID
			if parentRef == "" {
				parentRef = rec.ParentID
			}
			if parentRef != "" {
				if _, ok := imgIDSet[parentRef]; !ok {
					imgIDSet[parentRef] = struct{}{}
					imgIDs = append(imgIDs, parentRef)
				}
			}
		case "episode":
			if rec.SeriesID != "" {
				if _, ok := imgIDSet[rec.SeriesID]; !ok {
					imgIDSet[rec.SeriesID] = struct{}{}
					imgIDs = append(imgIDs, rec.SeriesID)
				}
			}
		}
	}

	imgMap, err := r.loadImageTagsBatch(ctx, imgIDs)
	if err != nil {
		return nil, err
	}

	genresMap, studiosMap, tagsMap, err := r.loadTaxonomiesBatch(ctx, ids)
	if err != nil {
		return nil, err
	}

	var statsMap map[string]*containerPlayStats
	if len(seriesIDs) > 0 || len(seasonIDs) > 0 {
		statsMap, err = r.loadSeriesSeasonStatsBatch(ctx, userID, seriesIDs, seasonIDs)
		if err != nil {
			return nil, err
		}
	}

	var peopleMap map[string][]dto.BaseItemPerson
	if wantPeople {
		peopleMap, err = r.loadPeopleBatch(ctx, ids)
		if err != nil {
			return nil, err
		}
	}

	var extrasMap map[string]*extraCounts
	if fullDetail {
		extrasMap, err = r.loadExtrasCountsBatch(ctx, ids)
		if err != nil {
			return nil, err
		}
	}

	dtos := make([]dto.BaseItemDto, len(recs))
	for i, rec := range recs {
		created := time.Unix(rec.DateCreated, 0).UTC()
		d := dto.BaseItemDto{
			Name:                    rec.Name,
			OriginalTitle:           rec.OriginalTitle,
			ServerId:                serverID,
			Id:                      rec.ID,
			Etag:                    rec.Etag,
			DateCreated:             &created,
			Container:               rec.Container,
			SortName:                rec.SortName,
			OfficialRating:          rec.OfficialRating,
			CommunityRating:         rec.CommunityRating,
			RunTimeTicks:            rec.RuntimeTicks,
			ProductionYear:          rec.ProductionYear,
			IndexNumber:             rec.IndexNumber,
			ParentIndexNumber:       rec.ParentIndexNumber,
			IsFolder:                rec.IsFolder,
			ParentId:                rec.ParentID,
			Type:                    rec.Type,
			MediaType:               rec.MediaType,
			ExtraType:               rec.Extra.ExtraType,
			CollectionType:          rec.Extra.CollectionType,
			SeriesId:                rec.SeriesID,
			SeriesName:              rec.Extra.SeriesName,
			SeasonId:                rec.SeasonID,
			SeasonName:              rec.Extra.SeasonName,
			Album:                   rec.Extra.Album,
			AlbumId:                 rec.Extra.AlbumID,
			AlbumArtist:             rec.Extra.AlbumArtist,
			Artists:                 rec.Extra.Artists,
			PrimaryImageAspectRatio: rec.Extra.PrimaryImageAspectRatio,
			UserData:                uds[i],
			ImageTags:               map[string]string{},
			BackdropImageTags:       []string{},
			LocationType:            "FileSystem",
		}
		if ec, ok := extrasMap[rec.ID]; ok && ec != nil {
			d.LocalTrailerCount = ec.trailers
			d.SpecialFeatureCount = ec.specials
		}
		if rec.PremiereDate != nil && *rec.PremiereDate > 0 {
			pt := time.Unix(*rec.PremiereDate, 0).UTC()
			d.PremiereDate = &pt
		}
		if wantOverview {
			d.Overview = rec.Extra.Overview
			d.Taglines = rec.Extra.Taglines
		}
		if wantPath {
			d.Path = rec.Path
		}
		if wantProviderIDs && len(rec.Extra.ProviderIDs) > 0 {
			d.ProviderIds = rec.Extra.ProviderIDs
		}
		if len(rec.Extra.Chapters) > 0 {
			chapters := make([]dto.ChapterInfo, len(rec.Extra.Chapters))
			tagSeed := rec.Etag
			if tagSeed == "" {
				tagSeed = rec.ID
			}
			for idx, ch := range rec.Extra.Chapters {
				sum := sha1.Sum([]byte(fmt.Sprintf("%s:%d", tagSeed, idx)))
				chapters[idx] = dto.ChapterInfo{
					StartPositionTicks: ch.StartPositionTicks,
					Name:               ch.Name,
					ImageTag:           hex.EncodeToString(sum[:])[:12],
				}
			}
			d.Chapters = chapters
		}
		if img, ok := imgMap[rec.ID]; ok {
			d.ImageTags = img.allTags
			if len(img.backdrops) > 0 {
				d.BackdropImageTags = img.backdrops
			}
		}
		if strings.EqualFold(rec.Type, "Season") || strings.EqualFold(rec.Type, "Episode") {
			parentRef := rec.SeriesID
			if parentRef == "" && strings.EqualFold(rec.Type, "Season") {
				parentRef = rec.ParentID
			}
			if parentRef != "" {
				if pImg, ok := imgMap[parentRef]; ok {
					if pImg.primary != "" {
						d.SeriesPrimaryImageTag = pImg.primary
					}
					if len(pImg.backdrops) > 0 {
						d.ParentBackdropItemId = parentRef
						d.ParentBackdropImageTags = pImg.backdrops
					}
				}
			}
		}
		if st, ok := statsMap[rec.ID]; ok && st != nil {
			cc := st.childCount
			rc := st.recursiveCount
			d.ChildCount = &cc
			d.RecursiveItemCount = &rc
			if userID != "" {
				if d.UserData == nil {
					d.UserData = &dto.UserItemDataDto{Key: rec.ID}
				}
				unplayed := st.unplayedCount
				d.UserData.UnplayedItemCount = &unplayed
				if st.recursiveCount > 0 {
					pct := float64(st.recursiveCount-st.unplayedCount) * 100.0 / float64(st.recursiveCount)
					d.UserData.PlayedPercentage = &pct
				}
			}
		}
		if ts := tagsMap[rec.ID]; len(ts) > 0 {
			d.Tags = ts
		}
		if wantGenres {
			if gs := genresMap[rec.ID]; len(gs) > 0 {
				d.GenreItems = gs
				d.Genres = make([]string, len(gs))
				for idx, g := range gs {
					d.Genres[idx] = g.Name
				}
			}
		}
		if wantStudios {
			if ss := studiosMap[rec.ID]; len(ss) > 0 {
				d.Studios = ss
			}
		}
		if wantPeople {
			if ps := peopleMap[rec.ID]; len(ps) > 0 {
				d.People = ps
			}
		}
		if wantMediaSources {
			srcs, err := r.GetMediaSourcesByItemID(ctx, rec.ID)
			if err == nil && len(srcs) > 0 {
				d.MediaSources = MediaSourceRecordsToDTO(srcs)
				if len(d.MediaSources) > 0 && len(d.MediaSources[0].MediaStreams) > 0 {
					d.MediaStreams = d.MediaSources[0].MediaStreams
				}
			}
		}
		dtos[i] = d
	}
	return dtos, nil
}

func (r *LibraryRepo) loadTaxonomiesBatch(ctx context.Context, itemIDs []string) (map[string][]dto.NameIdPair, map[string][]dto.NameIdPair, map[string][]string, error) {
	genres := make(map[string][]dto.NameIdPair)
	studios := make(map[string][]dto.NameIdPair)
	tags := make(map[string][]string)
	if len(itemIDs) == 0 {
		return genres, studios, tags, nil
	}
	for _, chunk := range chunkStrings(itemIDs, sqliteInChunkSize) {
		ph := make([]string, len(chunk))
		args := make([]any, len(chunk))
		for i, id := range chunk {
			ph[i] = "?"
			args[i] = id
		}
		rows, err := r.store.Reader.QueryContext(ctx, `
			SELECT it.item_id, t.kind, t.id, t.name
			FROM item_taxonomies it
			JOIN taxonomies t ON t.id = it.taxonomy_id
			WHERE it.item_id IN (`+strings.Join(ph, ",")+`)
			ORDER BY t.sort_name ASC`, args...)
		if err != nil {
			return nil, nil, nil, err
		}
		for rows.Next() {
			var itemID, kind, id, name string
			if err := rows.Scan(&itemID, &kind, &id, &name); err != nil {
				rows.Close()
				return nil, nil, nil, err
			}
			pair := dto.NameIdPair{Id: id, Name: name}
			switch strings.ToLower(kind) {
			case "genre":
				genres[itemID] = append(genres[itemID], pair)
			case "studio":
				studios[itemID] = append(studios[itemID], pair)
			case "tag":
				tags[itemID] = append(tags[itemID], name)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, nil, nil, err
		}
		rows.Close()
	}
	return genres, studios, tags, nil
}

// GetTaxonomiesByItemID returns persisted genre, studio, and tag names for itemID.
func (r *LibraryRepo) GetTaxonomiesByItemID(ctx context.Context, itemID string) (genres, studios, tags []string, err error) {
	itemID = guid.Normalize(itemID)
	rows, err := r.store.Reader.QueryContext(ctx, `
		SELECT t.kind, t.name
		FROM item_taxonomies it
		JOIN taxonomies t ON t.id = it.taxonomy_id
		WHERE it.item_id = ?
		ORDER BY t.sort_name ASC`, itemID)
	if err != nil {
		return nil, nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind, name string
		if err := rows.Scan(&kind, &name); err != nil {
			return nil, nil, nil, err
		}
		switch strings.ToLower(kind) {
		case "genre":
			genres = append(genres, name)
		case "studio":
			studios = append(studios, name)
		case "tag":
			tags = append(tags, name)
		}
	}
	return genres, studios, tags, rows.Err()
}

// GetPeopleByItemID returns all people associated with itemID (including PrimaryImageTag if present).
func (r *LibraryRepo) GetPeopleByItemID(ctx context.Context, itemID string) ([]dto.BaseItemPerson, error) {
	itemID = guid.Normalize(itemID)
	m, err := r.loadPeopleBatch(ctx, []string{itemID})
	if err != nil {
		return nil, err
	}
	return m[itemID], nil
}

func (r *LibraryRepo) loadPeopleBatch(ctx context.Context, itemIDs []string) (map[string][]dto.BaseItemPerson, error) {
	out := make(map[string][]dto.BaseItemPerson)
	if len(itemIDs) == 0 {
		return out, nil
	}
	for _, chunk := range chunkStrings(itemIDs, sqliteInChunkSize) {
		ph := make([]string, len(chunk))
		args := make([]any, len(chunk))
		for i, id := range chunk {
			ph[i] = "?"
			args[i] = id
		}
		rows, err := r.store.Reader.QueryContext(ctx, `
			SELECT ip.item_id, p.id, p.name, ip.role, ip.person_type, COALESCE(img.tag, '')
			FROM item_people ip
			JOIN people p ON p.id = ip.person_id
			LEFT JOIN item_images img ON img.item_id = p.id AND LOWER(img.image_type) = 'primary' AND img.image_index = 0
			WHERE ip.item_id IN (`+strings.Join(ph, ",")+`)
			ORDER BY ip.sort_order ASC`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var itemID, id, name, pType, primaryTag string
			var role sql.NullString
			if err := rows.Scan(&itemID, &id, &name, &role, &pType, &primaryTag); err != nil {
				rows.Close()
				return nil, err
			}
			out[itemID] = append(out[itemID], dto.BaseItemPerson{
				Id:              id,
				Name:            name,
				Role:            role.String,
				Type:            pType,
				PrimaryImageTag: primaryTag,
			})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

// MediaSourceRecordsToDTO converts MediaSourceRecord slices into Emby DTOs.
func MediaSourceRecordsToDTO(srcs []MediaSourceRecord) []dto.MediaSourceInfo {
	out := make([]dto.MediaSourceInfo, len(srcs))
	for i, s := range srcs {
		streams := make([]dto.MediaStream, len(s.Streams))
		for j, st := range s.Streams {
			streams[j] = dto.MediaStream{
				Codec:        st.Codec,
				Language:     st.Language,
				Title:        st.Title,
				DisplayTitle: st.DisplayTitle,
				BitRate:      st.Bitrate,
				Channels:     st.Channels,
				SampleRate:   st.SampleRate,
				Height:       st.Height,
				Width:        st.Width,
				VideoRange:   st.VideoRange,
				IsDefault:    st.IsDefault,
				IsForced:     st.IsForced,
				IsExternal:   st.IsExternal,
				Type:         st.StreamType,
				Index:        st.StreamIndex,
				Path:         st.Path,
				SubVariant:   st.SubVariant,
			}
		}
		out[i] = dto.MediaSourceInfo{
			Protocol:             s.Protocol,
			Id:                   s.ID,
			Path:                 s.Path,
			Type:                 "Default",
			Container:            s.Container,
			Size:                 s.Size,
			Name:                 s.Name,
			IsRemote:             s.IsRemote,
			RunTimeTicks:         s.RuntimeTicks,
			Bitrate:              s.Bitrate,
			SupportsTranscoding:  s.SupportsTranscoding,
			SupportsDirectStream: s.SupportsDirectStream,
			SupportsDirectPlay:   s.SupportsDirectPlay,
			MediaStreams:         streams,
		}
	}
	return out
}

func buildOrderBy(sortBy []string, sortOrder string, ftsActive, hasUser bool) string {
	dir := "ASC"
	if strings.EqualFold(sortOrder, "Descending") || strings.EqualFold(sortOrder, "DESC") {
		dir = "DESC"
	}
	if len(sortBy) == 0 {
		if ftsActive {
			return `ORDER BY bm25(item_search_fts, 0, 10.0, 5.0, 3.0, 2.0, 1.0) ASC`
		}
		return `ORDER BY i.sort_name ` + dir
	}
	var parts []string
	for _, s := range sortBy {
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "sortname", "name":
			parts = append(parts, "i.sort_name "+dir)
		case "datecreated":
			parts = append(parts, "i.date_created "+dir)
		case "premieredate", "productionyear":
			parts = append(parts, "COALESCE(i.premiere_date, 0) "+dir+", COALESCE(i.production_year, 0) "+dir)
		case "communityrating":
			parts = append(parts, "COALESCE(i.community_rating, 0) "+dir)
		case "runtime":
			parts = append(parts, "COALESCE(i.runtime_ticks, 0) "+dir)
		case "indexnumber":
			parts = append(parts, "COALESCE(i.parent_index_number, 0) "+dir+", COALESCE(i.index_number, 0) "+dir)
		case "dateplayed":
			if hasUser {
				parts = append(parts, "COALESCE(u.last_played_date, 0) "+dir)
			} else {
				parts = append(parts, "i.date_modified "+dir)
			}
		case "random":
			parts = append(parts, "RANDOM()")
		}
	}
	if len(parts) == 0 {
		return `ORDER BY i.sort_name ` + dir
	}
	return "ORDER BY " + strings.Join(parts, ", ")
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanItemRecord(row rowScanner) (*ItemRecord, error) {
	var (
		rec                                                          ItemRecord
		parentID, seriesID, seasonID, mediaType, origTitle, offRate  sql.NullString
		container, path                                              sql.NullString
		isFolder, isStrm                                             int
		extraRaw                                                     string
	)
	err := row.Scan(
		&rec.ID, &rec.LibraryID, &parentID, &seriesID, &seasonID,
		&rec.Type, &mediaType, &isFolder, &isStrm,
		&rec.Name, &origTitle, &rec.SortName,
		&rec.IndexNumber, &rec.ParentIndexNumber, &rec.ProductionYear,
		&rec.PremiereDate, &rec.EndDate, &rec.CommunityRating, &rec.CriticRating, &offRate,
		&rec.RuntimeTicks, &container, &path, &rec.FileSize, &rec.FileMtime,
		&rec.Etag, &extraRaw, &rec.DateCreated, &rec.DateModified,
	)
	if err != nil {
		return nil, err
	}
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
	_ = json.Unmarshal([]byte(extraRaw), &rec.Extra)
	return &rec, nil
}

func scanItemRow(rows *sql.Rows, hasUser bool) (ItemRecord, *dto.UserItemDataDto, error) {
	var (
		rec                                                         ItemRecord
		parentID, seriesID, seasonID, mediaType, origTitle, offRate sql.NullString
		container, path                                             sql.NullString
		isFolder, isStrm                                            int
		extraRaw                                                    string
	)
	if !hasUser {
		err := rows.Scan(
			&rec.ID, &rec.LibraryID, &parentID, &seriesID, &seasonID,
			&rec.Type, &mediaType, &isFolder, &isStrm,
			&rec.Name, &origTitle, &rec.SortName,
			&rec.IndexNumber, &rec.ParentIndexNumber, &rec.ProductionYear,
			&rec.PremiereDate, &rec.EndDate, &rec.CommunityRating, &rec.CriticRating, &offRate,
			&rec.RuntimeTicks, &container, &path, &rec.FileSize, &rec.FileMtime,
			&rec.Etag, &extraRaw, &rec.DateCreated, &rec.DateModified,
		)
		if err != nil {
			return rec, nil, err
		}
	} else {
		var (
			played, playCount, fav int
			pos                    int64
			lastPlayed             sql.NullInt64
		)
		err := rows.Scan(
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
			return rec, nil, err
		}
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
		_ = json.Unmarshal([]byte(extraRaw), &rec.Extra)
		return rec, ud, nil
	}

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
	_ = json.Unmarshal([]byte(extraRaw), &rec.Extra)
	return rec, nil, nil
}

func nullStr(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func paginateSlice[T any](items []T, start, limit int) []T {
	if start < 0 {
		start = 0
	}
	if start >= len(items) {
		return []T{}
	}
	end := len(items)
	if limit > 0 && start+limit < end {
		end = start + limit
	}
	return items[start:end]
}
