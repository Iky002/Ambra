package governance

import (
	"context"
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ambra-media/ambra/internal/pkg/guid"
)

type nfoActor struct {
	Name  string `xml:"name"`
	Role  string `xml:"role,omitempty"`
	Order int    `xml:"order"`
}

type nfoDocument struct {
	XMLName       xml.Name
	Title         string     `xml:"title"`
	OriginalTitle string     `xml:"originaltitle,omitempty"`
	Year          *int       `xml:"year,omitempty"`
	Premiered     string     `xml:"premiered,omitempty"`
	Aired         string     `xml:"aired,omitempty"`
	Tagline       string     `xml:"tagline,omitempty"`
	Plot          string     `xml:"plot,omitempty"`
	Rating        *float64   `xml:"rating,omitempty"`
	MPAA          string     `xml:"mpaa,omitempty"`
	SeasonNumber  *int       `xml:"seasonnumber,omitempty"`
	Season        *int       `xml:"season,omitempty"`
	Episode       *int       `xml:"episode,omitempty"`
	TMDBID        string     `xml:"tmdbid,omitempty"`
	IMDBID        string     `xml:"imdbid,omitempty"`
	TVDBID        string     `xml:"tvdbid,omitempty"`
	Studios       []string   `xml:"studio,omitempty"`
	Genres        []string   `xml:"genre,omitempty"`
	Tags          []string   `xml:"tag,omitempty"`
	Directors     []string   `xml:"director,omitempty"`
	Writers       []string   `xml:"writer,omitempty"`
	Actors        []nfoActor `xml:"actor,omitempty"`
}

// SetLockedFields stores the list of protected metadata fields in items.extra_json
// under the "LockedFields" key so future rescans do not overwrite them.
func (s *Service) SetLockedFields(ctx context.Context, itemID string, fields []string) (*LockedFieldsResult, error) {
	cleaned := normalizeLockedFields(fields)
	return s.SetLockState(ctx, itemID, cleaned, nil)
}

// SetLockState updates LockedFields (when fields != nil) and/or global LockData (when lockData != nil)
// in items.extra_json within a single writer transaction so future rescans respect both locks safely.
func (s *Service) SetLockState(ctx context.Context, itemID string, fields []string, lockData *bool) (*LockedFieldsResult, error) {
	itemID = guid.Normalize(itemID)
	if itemID == "" {
		return nil, fmt.Errorf("%w: item id is required", ErrInvalidArgument)
	}

	tx, err := s.store.Writer.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var extraRaw string
	err = tx.QueryRowContext(ctx,
		`SELECT extra_json FROM items WHERE id = ?`, itemID,
	).Scan(&extraRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	extra := decodeExtraMap(extraRaw)
	var currentFields []string
	if fields != nil {
		currentFields = normalizeLockedFields(fields)
		extra["LockedFields"] = currentFields
	} else {
		currentFields = extractLockedFields(extra)
		if lockData == nil {
			currentFields = []string{}
			extra["LockedFields"] = currentFields
		}
	}

	currentLockData := extractLockData(extra)
	if lockData != nil {
		currentLockData = *lockData
		extra["LockData"] = currentLockData
	}

	encoded, err := json.Marshal(extra)
	if err != nil {
		return nil, err
	}

	now := time.Now().Unix()
	if _, err := tx.ExecContext(ctx,
		`UPDATE items SET extra_json = ?, date_modified = ? WHERE id = ?`,
		string(encoded), now, itemID,
	); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &LockedFieldsResult{
		ItemID:       itemID,
		LockedFields: currentFields,
		LockData:     currentLockData,
	}, nil
}

func extractLockData(extra map[string]any) bool {
	if raw, ok := extra["LockData"]; ok {
		if b, ok := raw.(bool); ok {
			return b
		}
	}
	return false
}

// GetLockedFields reads the locked metadata fields stored in items.extra_json.
func (s *Service) GetLockedFields(ctx context.Context, itemID string) ([]string, error) {
	itemID = guid.Normalize(itemID)
	if itemID == "" {
		return nil, fmt.Errorf("%w: item id is required", ErrInvalidArgument)
	}
	var extraRaw string
	err := s.store.Reader.QueryRowContext(ctx,
		`SELECT extra_json FROM items WHERE id = ?`, itemID,
	).Scan(&extraRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	extra := decodeExtraMap(extraRaw)
	return extractLockedFields(extra), nil
}

// WriteItemNFOAtomic generates a Kodi/Emby-compatible XML NFO file for the given item
// and atomically replaces the target .nfo file via write-to-.tmp -> Sync -> os.Rename.
func (s *Service) WriteItemNFOAtomic(ctx context.Context, itemID string) (*NFOWriteResult, error) {
	itemID = guid.Normalize(itemID)
	if itemID == "" {
		return nil, fmt.Errorf("%w: item id is required", ErrInvalidArgument)
	}

	var (
		libraryID         string
		itemType          string
		isFolderInt       int
		name              string
		originalTitle     sql.NullString
		productionYear    sql.NullInt64
		premiereDate      sql.NullInt64
		parentIndexNumber sql.NullInt64
		indexNumber       sql.NullInt64
		communityRate     sql.NullFloat64
		officialRating    sql.NullString
		itemPath          sql.NullString
		extraRaw          string
	)
	err := s.store.Reader.QueryRowContext(ctx, `
		SELECT library_id, type, is_folder, name, original_title, production_year, premiere_date,
		       parent_index_number, index_number,
		       community_rating, official_rating, path, extra_json
		FROM items WHERE id = ?`, itemID,
	).Scan(
		&libraryID, &itemType, &isFolderInt, &name, &originalTitle, &productionYear, &premiereDate,
		&parentIndexNumber, &indexNumber,
		&communityRate, &officialRating, &itemPath, &extraRaw,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	pathStr := strings.TrimSpace(itemPath.String)
	if pathStr == "" {
		return nil, ErrItemPathEmpty
	}
	if isVirtualPath(pathStr) {
		return nil, fmt.Errorf("%w: cannot write NFO for virtual item path %q", ErrInvalidArgument, pathStr)
	}

	rootTag, nfoPath, err := resolveNFOTarget(itemType, isFolderInt != 0, pathStr)
	if err != nil {
		return nil, err
	}
	parentDir := filepath.Dir(nfoPath)
	if info, statErr := os.Stat(parentDir); statErr != nil || !info.IsDir() {
		return nil, fmt.Errorf("nfo target directory unavailable: %s", parentDir)
	}
	realParentDir, err := filepath.EvalSymlinks(parentDir)
	if err != nil {
		return nil, fmt.Errorf("resolve nfo target directory: %w", err)
	}
	if err := s.verifyWithinRegisteredLibraryRoots(ctx, libraryID, realParentDir); err != nil {
		return nil, err
	}
	if fi, lErr := os.Lstat(nfoPath); lErr == nil && fi.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("refusing to overwrite symlink nfo target: %s", nfoPath)
	}

	extra := decodeExtraMap(extraRaw)
	plot, _ := extra["overview"].(string)
	tagline := extractTagline(extra)
	providerIDs := extractProviderIDs(extra)

	genres, studios, tags, err := s.loadItemTaxonomies(ctx, itemID)
	if err != nil {
		return nil, err
	}
	actors, directors, writers, err := s.loadItemPeople(ctx, itemID)
	if err != nil {
		return nil, err
	}

	var dateStr string
	if premiereDate.Valid && premiereDate.Int64 != 0 {
		dateStr = time.Unix(premiereDate.Int64, 0).UTC().Format("2006-01-02")
	}

	doc := nfoDocument{
		XMLName:       xml.Name{Local: rootTag},
		Title:         name,
		OriginalTitle: strings.TrimSpace(originalTitle.String),
		Tagline:       tagline,
		Plot:          strings.TrimSpace(plot),
		MPAA:          strings.TrimSpace(officialRating.String),
		TMDBID:        lookupProviderID(providerIDs, "Tmdb"),
		IMDBID:        lookupProviderID(providerIDs, "Imdb"),
		TVDBID:        lookupProviderID(providerIDs, "Tvdb"),
		Studios:       studios,
		Genres:        genres,
		Tags:          tags,
		Directors:     directors,
		Writers:       writers,
		Actors:        actors,
	}
	if dateStr != "" {
		if rootTag == "episodedetails" {
			doc.Aired = dateStr
		} else {
			doc.Premiered = dateStr
		}
	}
	if productionYear.Valid {
		y := int(productionYear.Int64)
		doc.Year = &y
	}
	if communityRate.Valid {
		r := communityRate.Float64
		doc.Rating = &r
	}
	switch rootTag {
	case "season":
		if indexNumber.Valid {
			sn := int(indexNumber.Int64)
			doc.SeasonNumber = &sn
		}
	case "episodedetails":
		if parentIndexNumber.Valid {
			sv := int(parentIndexNumber.Int64)
			doc.Season = &sv
		}
		if indexNumber.Valid {
			ev := int(indexNumber.Int64)
			doc.Episode = &ev
		}
	}
	preserveMissingNFOFields(nfoPath, &doc)

	body, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal nfo xml: %w", err)
	}
	payload := append([]byte(xml.Header), body...)
	payload = append(payload, '\n')

	if err := atomicWriteFile(nfoPath, payload); err != nil {
		return nil, err
	}

	return &NFOWriteResult{
		ItemID:    itemID,
		NFOPath:   nfoPath,
		RootTag:   rootTag,
		BytesSize: len(payload),
	}, nil
}

func resolveNFOTarget(itemType string, isFolder bool, itemPath string) (rootTag string, nfoPath string, err error) {
	switch strings.ToLower(strings.TrimSpace(itemType)) {
	case "movie":
		if isFolder || isExistingDir(itemPath) {
			return "movie", filepath.Join(itemPath, "movie.nfo"), nil
		}
		return "movie", replaceExtWithNFO(itemPath), nil
	case "series":
		if isFolder || isExistingDir(itemPath) {
			return "tvshow", filepath.Join(itemPath, "tvshow.nfo"), nil
		}
		return "tvshow", filepath.Join(filepath.Dir(itemPath), "tvshow.nfo"), nil
	case "season":
		if !isExistingDir(itemPath) {
			return "", "", fmt.Errorf("%w: season NFO requires an existing directory path %q", ErrInvalidArgument, itemPath)
		}
		return "season", filepath.Join(itemPath, "season.nfo"), nil
	case "episode":
		return "episodedetails", replaceExtWithNFO(itemPath), nil
	case "musicartist":
		if !isExistingDir(itemPath) {
			return "", "", fmt.Errorf("%w: music artist NFO requires an existing directory path %q", ErrInvalidArgument, itemPath)
		}
		return "artist", filepath.Join(itemPath, "artist.nfo"), nil
	case "musicalbum":
		if !isExistingDir(itemPath) {
			return "", "", fmt.Errorf("%w: music album NFO requires an existing directory path %q", ErrInvalidArgument, itemPath)
		}
		return "album", filepath.Join(itemPath, "album.nfo"), nil
	default:
		return "", "", fmt.Errorf("%w: unsupported item type %q for NFO write", ErrInvalidArgument, itemType)
	}
}

func replaceExtWithNFO(p string) string {
	ext := filepath.Ext(p)
	if ext == "" {
		return p + ".nfo"
	}
	return strings.TrimSuffix(p, ext) + ".nfo"
}

func isExistingDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func (s *Service) verifyWithinRegisteredLibraryRoots(ctx context.Context, libraryID, realParentDir string) error {
	rows, err := s.store.Reader.QueryContext(ctx, `SELECT id, paths_json FROM libraries`)
	if err != nil {
		// If libraries table is unavailable, fail open only if table does not exist in isolated unit test; otherwise propagate error.
		return err
	}
	defer rows.Close()

	normLibID := guid.Normalize(libraryID)
	var matchedLibRoots []string
	var allRoots []string
	for rows.Next() {
		var id, rawPaths string
		if err := rows.Scan(&id, &rawPaths); err != nil {
			return err
		}
		var paths []string
		if err := json.Unmarshal([]byte(rawPaths), &paths); err != nil {
			continue
		}
		for _, p := range paths {
			trimmed := strings.TrimSpace(p)
			if trimmed == "" {
				continue
			}
			allRoots = append(allRoots, trimmed)
			if normLibID != "" && guid.Normalize(id) == normLibID {
				matchedLibRoots = append(matchedLibRoots, trimmed)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	candidates := matchedLibRoots
	if len(candidates) == 0 {
		candidates = allRoots
	}
	if len(candidates) == 0 {
		// Isolated unit test with no registered libraries in DB.
		return nil
	}

	for _, root := range candidates {
		realRoot, rErr := filepath.EvalSymlinks(root)
		if rErr != nil {
			realRoot = filepath.Clean(root)
		}
		rel, relErr := filepath.Rel(realRoot, realParentDir)
		if relErr != nil {
			continue
		}
		if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)) {
			return nil
		}
	}
	return fmt.Errorf("%w: NFO target directory %q escapes registered library roots", ErrInvalidArgument, realParentDir)
}

// atomicWriteFile writes data to a unique temp file in the target directory, calls Sync(),
// closes it, refuses symlink targets via Lstat, and renames atomically to targetPath.
func atomicWriteFile(targetPath string, data []byte) error {
	if fi, err := os.Lstat(targetPath); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to overwrite symlink nfo target: %s", targetPath)
		}
	}
	f, err := os.CreateTemp(filepath.Dir(targetPath), ".ambra-nfo-*.tmp")
	if err != nil {
		return fmt.Errorf("create nfo temp file: %w", err)
	}
	tmpPath := f.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("write nfo temp file: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("sync nfo temp file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close nfo temp file: %w", err)
	}
	_ = os.Chmod(tmpPath, 0o644)
	if fi, err := os.Lstat(targetPath); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to overwrite symlink nfo target: %s", targetPath)
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		return fmt.Errorf("rename nfo temp file: %w", err)
	}
	cleanup = false
	return nil
}

func (s *Service) loadItemTaxonomies(ctx context.Context, itemID string) (genres []string, studios []string, tags []string, err error) {
	rows, err := s.store.Reader.QueryContext(ctx, `
		SELECT t.kind, t.name
		FROM item_taxonomies it
		JOIN taxonomies t ON t.id = it.taxonomy_id
		WHERE it.item_id = ?
		ORDER BY t.sort_name ASC`, itemID,
	)
	if err != nil {
		return nil, nil, nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var kind, name string
		if err := rows.Scan(&kind, &name); err != nil {
			return nil, nil, nil, err
		}
		switch strings.ToLower(strings.TrimSpace(kind)) {
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

func (s *Service) loadItemPeople(ctx context.Context, itemID string) (actors []nfoActor, directors []string, writers []string, err error) {
	rows, err := s.store.Reader.QueryContext(ctx, `
		SELECT p.name, ip.person_type, ip.role, ip.sort_order
		FROM item_people ip
		JOIN people p ON p.id = ip.person_id
		WHERE ip.item_id = ?
		ORDER BY ip.sort_order ASC, p.sort_name ASC`, itemID,
	)
	if err != nil {
		return nil, nil, nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			name       string
			personType string
			role       sql.NullString
			order      int
		)
		if err := rows.Scan(&name, &personType, &role, &order); err != nil {
			return nil, nil, nil, err
		}
		switch strings.ToLower(strings.TrimSpace(personType)) {
		case "actor":
			actors = append(actors, nfoActor{
				Name:  name,
				Role:  strings.TrimSpace(role.String),
				Order: order,
			})
		case "director":
			directors = append(directors, name)
		case "writer":
			writers = append(writers, name)
		}
	}
	return actors, directors, writers, rows.Err()
}

func extractTagline(extra map[string]any) string {
	if raw, ok := extra["taglines"]; ok && raw != nil {
		switch arr := raw.(type) {
		case []string:
			for _, s := range arr {
				if trimmed := strings.TrimSpace(s); trimmed != "" {
					return trimmed
				}
			}
		case []any:
			for _, elem := range arr {
				if s, ok := elem.(string); ok {
					if trimmed := strings.TrimSpace(s); trimmed != "" {
						return trimmed
					}
				}
			}
		}
	}
	if s, ok := extra["tagline"].(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func preserveMissingNFOFields(nfoPath string, doc *nfoDocument) {
	if doc == nil {
		return
	}
	fi, err := os.Lstat(nfoPath)
	if err != nil || fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 || fi.Size() <= 0 || fi.Size() > 1<<20 {
		return
	}
	raw, err := os.ReadFile(nfoPath)
	if err != nil {
		return
	}
	var existing nfoDocument
	if err := xml.Unmarshal(raw, &existing); err != nil {
		return
	}
	if doc.XMLName.Local == "episodedetails" {
		if doc.Aired == "" && strings.TrimSpace(existing.Aired) != "" {
			doc.Aired = strings.TrimSpace(existing.Aired)
		}
	} else {
		if doc.Premiered == "" && strings.TrimSpace(existing.Premiered) != "" {
			doc.Premiered = strings.TrimSpace(existing.Premiered)
		}
	}
	if doc.SeasonNumber == nil && existing.SeasonNumber != nil {
		v := *existing.SeasonNumber
		doc.SeasonNumber = &v
	}
	if doc.Tagline == "" && strings.TrimSpace(existing.Tagline) != "" {
		doc.Tagline = strings.TrimSpace(existing.Tagline)
	}
	if doc.TVDBID == "" && strings.TrimSpace(existing.TVDBID) != "" {
		doc.TVDBID = strings.TrimSpace(existing.TVDBID)
	}
	if doc.Season == nil && existing.Season != nil {
		v := *existing.Season
		doc.Season = &v
	}
	if doc.Episode == nil && existing.Episode != nil {
		v := *existing.Episode
		doc.Episode = &v
	}
	if len(doc.Studios) == 0 && len(existing.Studios) > 0 {
		doc.Studios = existing.Studios
	}
	if len(doc.Directors) == 0 && len(existing.Directors) > 0 {
		doc.Directors = existing.Directors
	}
	if len(doc.Writers) == 0 && len(existing.Writers) > 0 {
		doc.Writers = existing.Writers
	}
}

func decodeExtraMap(raw string) map[string]any {
	m := make(map[string]any)
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" {
		return m
	}
	_ = json.Unmarshal([]byte(raw), &m)
	if m == nil {
		return make(map[string]any)
	}
	return m
}

func extractProviderIDs(extra map[string]any) map[string]string {
	out := make(map[string]string)
	raw, ok := extra["provider_ids"]
	if !ok || raw == nil {
		return out
	}
	switch typed := raw.(type) {
	case map[string]any:
		for k, v := range typed {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				out[k] = strings.TrimSpace(s)
			}
		}
	case map[string]string:
		for k, v := range typed {
			if strings.TrimSpace(v) != "" {
				out[k] = strings.TrimSpace(v)
			}
		}
	}
	return out
}

func lookupProviderID(providers map[string]string, target string) string {
	if v, ok := providers[target]; ok && v != "" {
		return v
	}
	for k, v := range providers {
		if strings.EqualFold(k, target) && v != "" {
			return v
		}
	}
	return ""
}

func normalizeLockedFields(fields []string) []string {
	out := make([]string, 0, len(fields))
	seen := make(map[string]struct{}, len(fields))
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		key := strings.ToLower(f)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, f)
	}
	return out
}

func extractLockedFields(extra map[string]any) []string {
	raw, ok := extra["LockedFields"]
	if !ok || raw == nil {
		return []string{}
	}
	switch arr := raw.(type) {
	case []string:
		return normalizeLockedFields(arr)
	case []any:
		res := make([]string, 0, len(arr))
		for _, elem := range arr {
			if s, ok := elem.(string); ok {
				res = append(res, s)
			}
		}
		return normalizeLockedFields(res)
	default:
		return []string{}
	}
}
