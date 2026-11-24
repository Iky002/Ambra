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
	Plot          string     `xml:"plot,omitempty"`
	Rating        *float64   `xml:"rating,omitempty"`
	MPAA          string     `xml:"mpaa,omitempty"`
	TMDBID        string     `xml:"tmdbid,omitempty"`
	IMDBID        string     `xml:"imdbid,omitempty"`
	Genres        []string   `xml:"genre,omitempty"`
	Tags          []string   `xml:"tag,omitempty"`
	Actors        []nfoActor `xml:"actor,omitempty"`
}

// SetLockedFields stores the list of protected metadata fields in items.extra_json
// under the "LockedFields" key so future rescans do not overwrite them.
func (s *Service) SetLockedFields(ctx context.Context, itemID string, fields []string) (*LockedFieldsResult, error) {
	itemID = guid.Normalize(itemID)
	if itemID == "" {
		return nil, fmt.Errorf("%w: item id is required", ErrInvalidArgument)
	}

	cleaned := normalizeLockedFields(fields)

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
	extra["LockedFields"] = cleaned

	encoded, err := json.Marshal(extra)
	if err != nil {
		return nil, err
	}

	now := time.Now().Unix()
	if _, err := s.store.Writer.ExecContext(ctx,
		`UPDATE items SET extra_json = ?, date_modified = ? WHERE id = ?`,
		string(encoded), now, itemID,
	); err != nil {
		return nil, err
	}

	return &LockedFieldsResult{
		ItemID:       itemID,
		LockedFields: cleaned,
	}, nil
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
		itemType       string
		isFolderInt    int
		name           string
		originalTitle  sql.NullString
		productionYear sql.NullInt64
		communityRate  sql.NullFloat64
		officialRating sql.NullString
		itemPath       sql.NullString
		extraRaw       string
	)
	err := s.store.Reader.QueryRowContext(ctx, `
		SELECT type, is_folder, name, original_title, production_year,
		       community_rating, official_rating, path, extra_json
		FROM items WHERE id = ?`, itemID,
	).Scan(
		&itemType, &isFolderInt, &name, &originalTitle, &productionYear,
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

	rootTag, nfoPath := resolveNFOTarget(itemType, isFolderInt != 0, pathStr)
	parentDir := filepath.Dir(nfoPath)
	if info, statErr := os.Stat(parentDir); statErr != nil || !info.IsDir() {
		return nil, fmt.Errorf("nfo target directory unavailable: %s", parentDir)
	}

	extra := decodeExtraMap(extraRaw)
	plot, _ := extra["overview"].(string)
	providerIDs := extractProviderIDs(extra)

	genres, tags, err := s.loadItemTaxonomies(ctx, itemID)
	if err != nil {
		return nil, err
	}
	actors, err := s.loadItemActors(ctx, itemID)
	if err != nil {
		return nil, err
	}

	doc := nfoDocument{
		XMLName:       xml.Name{Local: rootTag},
		Title:         name,
		OriginalTitle: strings.TrimSpace(originalTitle.String),
		Plot:          strings.TrimSpace(plot),
		MPAA:          strings.TrimSpace(officialRating.String),
		TMDBID:        lookupProviderID(providerIDs, "Tmdb"),
		IMDBID:        lookupProviderID(providerIDs, "Imdb"),
		Genres:        genres,
		Tags:          tags,
		Actors:        actors,
	}
	if productionYear.Valid {
		y := int(productionYear.Int64)
		doc.Year = &y
	}
	if communityRate.Valid {
		r := communityRate.Float64
		doc.Rating = &r
	}

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

func resolveNFOTarget(itemType string, isFolder bool, itemPath string) (rootTag string, nfoPath string) {
	switch strings.ToLower(strings.TrimSpace(itemType)) {
	case "series":
		rootTag = "tvshow"
		if isFolder || isExistingDir(itemPath) {
			return rootTag, filepath.Join(itemPath, "tvshow.nfo")
		}
		return rootTag, filepath.Join(filepath.Dir(itemPath), "tvshow.nfo")
	case "episode":
		rootTag = "episodedetails"
		return rootTag, replaceExtWithNFO(itemPath)
	default:
		rootTag = "movie"
		if isFolder || isExistingDir(itemPath) {
			return rootTag, filepath.Join(itemPath, "movie.nfo")
		}
		return rootTag, replaceExtWithNFO(itemPath)
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

// atomicWriteFile writes data to <target>.tmp, calls Sync(), closes it, and renames to target.
func atomicWriteFile(targetPath string, data []byte) error {
	tmpPath := targetPath + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("create nfo temp file: %w", err)
	}
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
	if err := os.Rename(tmpPath, targetPath); err != nil {
		return fmt.Errorf("rename nfo temp file: %w", err)
	}
	cleanup = false
	return nil
}

func (s *Service) loadItemTaxonomies(ctx context.Context, itemID string) (genres []string, tags []string, err error) {
	rows, err := s.store.Reader.QueryContext(ctx, `
		SELECT t.kind, t.name
		FROM item_taxonomies it
		JOIN taxonomies t ON t.id = it.taxonomy_id
		WHERE it.item_id = ?
		ORDER BY t.sort_name ASC`, itemID,
	)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var kind, name string
		if err := rows.Scan(&kind, &name); err != nil {
			return nil, nil, err
		}
		switch strings.ToLower(strings.TrimSpace(kind)) {
		case "genre":
			genres = append(genres, name)
		case "tag":
			tags = append(tags, name)
		}
	}
	return genres, tags, rows.Err()
}

func (s *Service) loadItemActors(ctx context.Context, itemID string) ([]nfoActor, error) {
	rows, err := s.store.Reader.QueryContext(ctx, `
		SELECT p.name, ip.role, ip.sort_order
		FROM item_people ip
		JOIN people p ON p.id = ip.person_id
		WHERE ip.item_id = ? AND lower(ip.person_type) = 'actor'
		ORDER BY ip.sort_order ASC, p.sort_name ASC`, itemID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var actors []nfoActor
	for rows.Next() {
		var (
			name  string
			role  sql.NullString
			order int
		)
		if err := rows.Scan(&name, &role, &order); err != nil {
			return nil, err
		}
		actors = append(actors, nfoActor{
			Name:  name,
			Role:  strings.TrimSpace(role.String),
			Order: order,
		})
	}
	return actors, rows.Err()
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
