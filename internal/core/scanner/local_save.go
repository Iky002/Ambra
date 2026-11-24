package scanner

import (
	"context"
	"encoding/xml"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

type localNFOUniqueID struct {
	Type    string `xml:"type,attr"`
	Default bool   `xml:"default,attr,omitempty"`
	Value   string `xml:",chardata"`
}

type localNFOActor struct {
	Name  string `xml:"name"`
	Role  string `xml:"role,omitempty"`
	Order int    `xml:"order"`
}

type localNFODocument struct {
	XMLName       xml.Name           `xml:""`
	Title         string             `xml:"title"`
	OriginalTitle string             `xml:"originaltitle,omitempty"`
	Plot          string             `xml:"plot,omitempty"`
	Tagline       string             `xml:"tagline,omitempty"`
	Year          *int               `xml:"year,omitempty"`
	Premiered     string             `xml:"premiered,omitempty"`
	Aired         string             `xml:"aired,omitempty"`
	Rating        *float64           `xml:"rating,omitempty"`
	MPAA          string             `xml:"mpaa,omitempty"`
	SeasonNumber  *int               `xml:"seasonnumber,omitempty"`
	Season        *int               `xml:"season,omitempty"`
	Episode       *int               `xml:"episode,omitempty"`
	TMDBID        string             `xml:"tmdbid,omitempty"`
	IMDBID        string             `xml:"imdbid,omitempty"`
	TVDBID        string             `xml:"tvdbid,omitempty"`
	UniqueIDs     []localNFOUniqueID `xml:"uniqueid,omitempty"`
	Genres        []string           `xml:"genre,omitempty"`
	Studios       []string           `xml:"studio,omitempty"`
	Tags          []string           `xml:"tag,omitempty"`
	Directors     []string           `xml:"director,omitempty"`
	Writers       []string           `xml:"writer,omitempty"`
	Actors        []localNFOActor    `xml:"actor,omitempty"`
}

// shouldSaveLocalMetadata checks the item's library SaveLocalMetadata option (defaults to true).
func (s *Scanner) shouldSaveLocalMetadata(ctx context.Context, libraryID string) (*sqlite.LibraryRecord, bool) {
	if s.repo == nil || strings.TrimSpace(libraryID) == "" {
		return nil, false
	}
	lib, err := s.repo.GetLibrary(ctx, libraryID)
	if err != nil || lib == nil {
		return nil, false
	}
	return lib, lib.SaveLocalMetadataEnabled()
}

// resolveLocalImagePath determines the Emby/Kodi-compatible local artwork path
// alongside the media item. Returns ("", false) when the item has no writable
// media directory or the image type has no local convention.
func (s *Scanner) resolveLocalImagePath(ctx context.Context, lib *sqlite.LibraryRecord, rec *sqlite.ItemRecord, imgType, ext string) (string, bool) {
	if rec == nil || strings.TrimSpace(rec.Path) == "" {
		return "", false
	}
	itemPath := filepath.Clean(strings.TrimSpace(rec.Path))
	itemType := strings.ToLower(strings.TrimSpace(rec.Type))

	var dir string
	var filename string

	switch itemType {
	case "series":
		dir = itemPath
		if !isExistingDirPath(dir) {
			dir = filepath.Dir(itemPath)
		}
		filename = standardArtworkFilename(imgType, ext)

	case "season":
		seasonDir := itemPath
		if !isExistingDirPath(seasonDir) {
			seasonDir = filepath.Dir(itemPath)
		}
		seriesDir := s.resolveSeriesDir(ctx, rec)
		seasonNum := 1
		if rec.IndexNumber != nil {
			seasonNum = *rec.IndexNumber
		}
		if seriesDir != "" && filepath.Clean(seasonDir) == filepath.Clean(seriesDir) {
			// Flat series directory without dedicated Season subfolder:
			// use season{NN}-poster.jpg / season-specials-poster.jpg so we never overwrite Series poster.jpg.
			dir = seriesDir
			filename = flatSeasonArtworkFilename(seasonNum, imgType, ext)
		} else {
			dir = seasonDir
			filename = standardArtworkFilename(imgType, ext)
		}

	case "episode":
		dir = filepath.Dir(itemPath)
		stem := strings.TrimSuffix(filepath.Base(itemPath), filepath.Ext(itemPath))
		if stem == "" {
			return "", false
		}
		switch imgType {
		case "Primary":
			filename = stem + "-thumb" + ext
		case "Backdrop":
			filename = stem + "-fanart" + ext
		default:
			return "", false
		}

	case "movie":
		if rec.IsFolder || isExistingDirPath(itemPath) {
			dir = itemPath
			filename = standardArtworkFilename(imgType, ext)
		} else {
			dir = filepath.Dir(itemPath)
			stem := strings.TrimSuffix(filepath.Base(itemPath), filepath.Ext(itemPath))
			if isSharedMovieRootDir(dir, lib) {
				// Multiple movies placed directly inside the library root:
				// prefix with media stem to avoid clobbering sibling movies.
				filename = prefixedArtworkFilename(stem, imgType, ext)
			} else {
				filename = standardArtworkFilename(imgType, ext)
			}
		}

	default:
		return "", false
	}

	if dir == "" || filename == "" || !isExistingDirPath(dir) {
		return "", false
	}
	return filepath.Join(dir, filename), true
}

func standardArtworkFilename(imgType, ext string) string {
	switch imgType {
	case "Primary":
		return "poster" + ext
	case "Backdrop":
		return "fanart" + ext
	case "Logo":
		return "clearlogo" + ext
	default:
		return ""
	}
}

func prefixedArtworkFilename(stem, imgType, ext string) string {
	if stem == "" {
		return standardArtworkFilename(imgType, ext)
	}
	switch imgType {
	case "Primary":
		return stem + "-poster" + ext
	case "Backdrop":
		return stem + "-fanart" + ext
	case "Logo":
		return stem + "-logo" + ext
	default:
		return ""
	}
}

func flatSeasonArtworkFilename(seasonNum int, imgType, ext string) string {
	prefix := fmt.Sprintf("season%02d", seasonNum)
	if seasonNum == 0 {
		prefix = "season-specials"
	}
	switch imgType {
	case "Primary":
		return prefix + "-poster" + ext
	case "Backdrop":
		return prefix + "-fanart" + ext
	default:
		return ""
	}
}

// isSharedMovieRootDir returns true when dir is one of the library's root paths
// AND contains more than one video/strm file, meaning movies are stored flat
// rather than in individual per-movie folders.
func isSharedMovieRootDir(dir string, lib *sqlite.LibraryRecord) bool {
	if lib == nil {
		return false
	}
	cleanDir := filepath.Clean(dir)
	isRoot := false
	for _, p := range lib.Paths {
		if filepath.Clean(strings.TrimSpace(p)) == cleanDir {
			isRoot = true
			break
		}
	}
	if !isRoot {
		return false
	}
	entries, err := os.ReadDir(cleanDir)
	if err != nil {
		return false
	}
	videoCount := 0
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if IsVideoFile(e.Name()) || IsStrmFile(e.Name()) {
			videoCount++
			if videoCount > 1 {
				return true
			}
		}
	}
	return false
}

func (s *Scanner) resolveSeriesDir(ctx context.Context, rec *sqlite.ItemRecord) string {
	if rec == nil || s.repo == nil {
		return ""
	}
	parentID := rec.SeriesID
	if parentID == "" {
		parentID = rec.ParentID
	}
	if parentID == "" {
		return ""
	}
	parent, err := s.repo.GetItemRecordByID(ctx, parentID)
	if err != nil || parent == nil || strings.TrimSpace(parent.Path) == "" {
		return ""
	}
	if isExistingDirPath(parent.Path) {
		return filepath.Clean(parent.Path)
	}
	return filepath.Clean(filepath.Dir(parent.Path))
}

func isExistingDirPath(p string) bool {
	if strings.TrimSpace(p) == "" {
		return false
	}
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// saveLocalNFOIfEnabled writes a Kodi/Emby-compatible .nfo file alongside the
// media item when the item's library has SaveLocalMetadata enabled.
// When forceReplace is false, existing non-empty .nfo files are preserved.
// Locked items (LockData=true) are never overwritten.
func (s *Scanner) saveLocalNFOIfEnabled(ctx context.Context, rec *sqlite.ItemRecord, meta *plugins.MetadataResult, forceReplace bool) {
	if rec == nil || rec.Extra.LockData {
		return
	}
	_, enabled := s.shouldSaveLocalMetadata(ctx, rec.LibraryID)
	if !enabled {
		return
	}

	rootTag, nfoPath, ok := s.resolveLocalNFOTarget(ctx, rec)
	if !ok {
		return
	}

	if !forceReplace {
		if info, err := os.Stat(nfoPath); err == nil && !info.IsDir() && info.Size() > 0 {
			return
		}
	}

	doc := buildLocalNFODocument(rootTag, rec, meta)
	body, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		slog.Warn("marshal local nfo failed", "item_id", rec.ID, "error", err)
		return
	}
	payload := append([]byte(xml.Header), body...)
	payload = append(payload, '\n')

	if err := writeFileAtomic(nfoPath, payload); err != nil {
		slog.Warn("write local nfo failed", "item_id", rec.ID, "path", nfoPath, "error", err)
	}
}

func (s *Scanner) resolveLocalNFOTarget(ctx context.Context, rec *sqlite.ItemRecord) (rootTag string, nfoPath string, ok bool) {
	itemPath := strings.TrimSpace(rec.Path)
	if itemPath == "" {
		return "", "", false
	}
	itemPath = filepath.Clean(itemPath)
	itemType := strings.ToLower(strings.TrimSpace(rec.Type))

	switch itemType {
	case "series":
		dir := itemPath
		if !isExistingDirPath(dir) {
			dir = filepath.Dir(itemPath)
		}
		if !isExistingDirPath(dir) {
			return "", "", false
		}
		return "tvshow", filepath.Join(dir, "tvshow.nfo"), true

	case "season":
		seasonDir := itemPath
		if !isExistingDirPath(seasonDir) {
			return "", "", false
		}
		seriesDir := s.resolveSeriesDir(ctx, rec)
		if seriesDir != "" && filepath.Clean(seasonDir) == filepath.Clean(seriesDir) {
			// Flat series directory without a dedicated Season folder: skip season.nfo
			// so multiple seasons never overwrite one another.
			return "", "", false
		}
		return "season", filepath.Join(seasonDir, "season.nfo"), true

	case "episode":
		dir := filepath.Dir(itemPath)
		if !isExistingDirPath(dir) {
			return "", "", false
		}
		return "episodedetails", replacePathExtWithNFO(itemPath), true

	case "movie":
		if rec.IsFolder || isExistingDirPath(itemPath) {
			if !isExistingDirPath(itemPath) {
				return "", "", false
			}
			return "movie", filepath.Join(itemPath, "movie.nfo"), true
		}
		dir := filepath.Dir(itemPath)
		if !isExistingDirPath(dir) {
			return "", "", false
		}
		return "movie", replacePathExtWithNFO(itemPath), true

	default:
		return "", "", false
	}
}

func replacePathExtWithNFO(p string) string {
	ext := filepath.Ext(p)
	if ext == "" {
		return p + ".nfo"
	}
	return strings.TrimSuffix(p, ext) + ".nfo"
}

func buildLocalNFODocument(rootTag string, rec *sqlite.ItemRecord, meta *plugins.MetadataResult) localNFODocument {
	doc := localNFODocument{
		XMLName:       xml.Name{Local: rootTag},
		Title:         strings.TrimSpace(rec.Name),
		OriginalTitle: strings.TrimSpace(rec.OriginalTitle),
		Plot:          strings.TrimSpace(rec.Extra.Overview),
		MPAA:          strings.TrimSpace(rec.OfficialRating),
	}
	if len(rec.Extra.Taglines) > 0 {
		doc.Tagline = strings.TrimSpace(rec.Extra.Taglines[0])
	}
	if rec.ProductionYear != nil && *rec.ProductionYear > 0 {
		y := *rec.ProductionYear
		doc.Year = &y
	}
	if rec.PremiereDate != nil && *rec.PremiereDate > 0 {
		dateStr := time.Unix(*rec.PremiereDate, 0).UTC().Format("2006-01-02")
		if rootTag == "episodedetails" {
			doc.Aired = dateStr
		} else {
			doc.Premiered = dateStr
		}
	}
	if rec.CommunityRating != nil && *rec.CommunityRating > 0 {
		r := *rec.CommunityRating
		doc.Rating = &r
	}

	switch rootTag {
	case "season":
		if rec.IndexNumber != nil {
			n := *rec.IndexNumber
			doc.SeasonNumber = &n
		}
	case "episodedetails":
		if rec.ParentIndexNumber != nil {
			sn := *rec.ParentIndexNumber
			doc.Season = &sn
		}
		if rec.IndexNumber != nil {
			en := *rec.IndexNumber
			doc.Episode = &en
		}
	}

	tmdbID := getProviderID(rec.Extra.ProviderIDs, "tmdb")
	imdbID := getProviderID(rec.Extra.ProviderIDs, "imdb")
	tvdbID := getProviderID(rec.Extra.ProviderIDs, "tvdb")
	doc.TMDBID = tmdbID
	doc.IMDBID = imdbID
	doc.TVDBID = tvdbID
	if tmdbID != "" {
		doc.UniqueIDs = append(doc.UniqueIDs, localNFOUniqueID{Type: "tmdb", Default: true, Value: tmdbID})
	}
	if imdbID != "" {
		doc.UniqueIDs = append(doc.UniqueIDs, localNFOUniqueID{Type: "imdb", Default: tmdbID == "", Value: imdbID})
	}
	if tvdbID != "" {
		doc.UniqueIDs = append(doc.UniqueIDs, localNFOUniqueID{Type: "tvdb", Value: tvdbID})
	}

	if meta != nil {
		doc.Genres = dedupeNonEmptyStrings(meta.Genres)
		doc.Studios = dedupeNonEmptyStrings(meta.Studios)
		doc.Tags = dedupeNonEmptyStrings(meta.Tags)

		actorOrder := 0
		for _, p := range meta.People {
			name := strings.TrimSpace(p.Name)
			if name == "" {
				continue
			}
			switch strings.ToLower(strings.TrimSpace(p.Type)) {
			case "director":
				doc.Directors = append(doc.Directors, name)
			case "writer":
				doc.Writers = append(doc.Writers, name)
			default:
				doc.Actors = append(doc.Actors, localNFOActor{
					Name:  name,
					Role:  strings.TrimSpace(p.Role),
					Order: actorOrder,
				})
				actorOrder++
			}
		}
		doc.Directors = dedupeNonEmptyStrings(doc.Directors)
		doc.Writers = dedupeNonEmptyStrings(doc.Writers)
	}

	return doc
}

func dedupeNonEmptyStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		trimmed := strings.TrimSpace(s)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}
