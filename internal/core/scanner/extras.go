package scanner

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

var extraSubdirTypes = map[string]string{
	"trailers":          "Trailer",
	"extras":            "Clip",
	"featurettes":       "Featurette",
	"behind the scenes": "BehindTheScenes",
	"deleted scenes":    "DeletedScene",
	"interviews":        "Interview",
	"scenes":            "Scene",
	"shorts":            "Short",
	"theme-music":       "ThemeSong",
	"backdrops":         "ThemeVideo",
}

var videoExtraSuffixes = []struct {
	suffix    string
	extraType string
}{
	{"-behindthescenes", "BehindTheScenes"},
	{"-deletedscene", "DeletedScene"},
	{"-featurette", "Featurette"},
	{"-interview", "Interview"},
	{"-trailer", "Trailer"},
	{"-deleted", "DeletedScene"},
	{"-scene", "Scene"},
	{"-short", "Short"},
	{"-extra", "Clip"},
	{"-other", "Clip"},
	{"-theme", "ThemeVideo"},
}

// ClassifyExtraFile determines whether filePath (relative to parentDir) is a local extra
// (trailer, featurette, behind the scenes, deleted scene, interview, scene, short, clip,
// theme song, or theme video) according to Emby naming conventions.
func ClassifyExtraFile(parentDir, filePath string) (extraType string, ok bool) {
	isVideo := IsVideoFile(filePath) || IsStrmFile(filePath)
	isAudio := IsAudioFile(filePath)
	if !isVideo && !isAudio {
		return "", false
	}

	cleanFile := filepath.Clean(filePath)
	cleanParent := filepath.Clean(strings.TrimSpace(parentDir))

	// 1. Check Emby extra subdirectories relative to parentDir (or immediate parent dir if parentDir is empty).
	var dirSegments []string
	if cleanParent != "" && cleanParent != "." {
		if rel, err := filepath.Rel(cleanParent, cleanFile); err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			relDir := filepath.Dir(rel)
			if relDir != "." && relDir != "" {
				dirSegments = strings.Split(filepath.ToSlash(relDir), "/")
			}
		}
	} else {
		dirSegments = []string{filepath.Base(filepath.Dir(cleanFile))}
	}

	for _, seg := range dirSegments {
		if et, found := extraSubdirTypes[strings.ToLower(strings.TrimSpace(seg))]; found {
			if et == "ThemeSong" {
				if isAudio {
					return et, true
				}
				continue
			}
			if isVideo {
				return et, true
			}
		}
	}

	// 2. Check filename conventions (theme.mp3/.flac/.m4a or video suffixes like -trailer).
	baseNoExt := strings.TrimSuffix(filepath.Base(cleanFile), filepath.Ext(cleanFile))
	lowerBase := strings.ToLower(strings.TrimSpace(baseNoExt))

	if isAudio {
		if lowerBase == "theme" || strings.HasSuffix(lowerBase, "-theme") {
			return "ThemeSong", true
		}
		return "", false
	}

	for _, s := range videoExtraSuffixes {
		if strings.HasSuffix(lowerBase, s.suffix) {
			return s.extraType, true
		}
	}

	return "", false
}

func cleanExtraDisplayName(filePath string) string {
	base := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
	lower := strings.ToLower(base)
	for _, s := range videoExtraSuffixes {
		if strings.HasSuffix(lower, s.suffix) && len(base) > len(s.suffix) {
			trimmed := strings.TrimSpace(base[:len(base)-len(s.suffix)])
			if trimmed != "" {
				return base
			}
		}
	}
	return base
}

func isDedicatedItemDir(lib *sqlite.LibraryRecord, dir string) bool {
	cleanDir := filepath.Clean(dir)
	if lib == nil {
		return true
	}
	for _, root := range lib.Paths {
		if filepath.Clean(strings.TrimSpace(root)) == cleanDir {
			return false
		}
	}
	return true
}

func matchesSiblingExtraPrefix(filePath, mediaBaseName string) bool {
	baseNoExt := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))))
	if baseNoExt == "theme" {
		return true
	}
	if mediaBaseName == "" {
		return true
	}
	lowerMedia := strings.ToLower(strings.TrimSpace(mediaBaseName))
	if lowerMedia == "" {
		return true
	}
	return strings.HasPrefix(baseNoExt, lowerMedia+"-")
}

// scanAndIndexExtras discovers local trailers, special features, and theme media
// alongside or under parentDir and upserts them as playable child items of parent.
func (s *Scanner) scanAndIndexExtras(ctx context.Context, lib *sqlite.LibraryRecord, parent *sqlite.ItemRecord, parentDir, mediaBaseName string) {
	if s.repo == nil || lib == nil || parent == nil || strings.TrimSpace(parentDir) == "" {
		return
	}
	cleanParentDir := filepath.Clean(parentDir)
	entries, err := os.ReadDir(cleanParentDir)
	if err != nil {
		return
	}

	dedicatedDir := isDedicatedItemDir(lib, cleanParentDir)
	type discoveredExtra struct {
		path      string
		extraType string
	}
	var found []discoveredExtra

	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		fullPath := filepath.Join(cleanParentDir, name)
		if entry.IsDir() {
			if _, ok := extraSubdirTypes[strings.ToLower(strings.TrimSpace(name))]; !ok {
				continue
			}
			subEntries, subErr := os.ReadDir(fullPath)
			if subErr != nil {
				continue
			}
			for _, sub := range subEntries {
				if sub.IsDir() || strings.HasPrefix(sub.Name(), ".") {
					continue
				}
				subPath := filepath.Join(fullPath, sub.Name())
				if et, ok := ClassifyExtraFile(cleanParentDir, subPath); ok {
					found = append(found, discoveredExtra{path: subPath, extraType: et})
				}
			}
			continue
		}

		if et, ok := ClassifyExtraFile(cleanParentDir, fullPath); ok {
			if dedicatedDir || matchesSiblingExtraPrefix(fullPath, mediaBaseName) {
				found = append(found, discoveredExtra{path: fullPath, extraType: et})
			}
		}
	}

	for _, item := range found {
		if ctx.Err() != nil {
			return
		}
		s.upsertExtraItem(ctx, lib, parent, item.path, item.extraType)
	}
}

func (s *Scanner) upsertExtraItem(ctx context.Context, lib *sqlite.LibraryRecord, parent *sqlite.ItemRecord, extraPath, extraType string) {
	info, err := os.Stat(extraPath)
	if err != nil || info.IsDir() {
		return
	}
	size := info.Size()
	mtime := info.ModTime().Unix()

	existing, err := s.repo.FindItemByPath(ctx, lib.ID, extraPath)
	if err == nil && existing != nil {
		if existing.ParentID == parent.ID &&
			existing.Extra.ExtraType == extraType &&
			existing.FileSize != nil && *existing.FileSize == size &&
			existing.FileMtime != nil && *existing.FileMtime == mtime {
			return
		}
	}

	isAudio := IsAudioFile(extraPath)
	isStrm := IsStrmFile(extraPath)
	itemType := "Video"
	mediaType := "Video"
	if isAudio {
		itemType = "Audio"
		mediaType = "Audio"
	}
	container := strings.TrimPrefix(strings.ToLower(filepath.Ext(extraPath)), ".")
	if isStrm {
		container = "strm"
	}

	itemID := guid.New()
	if existing != nil {
		itemID = existing.ID
	}

	name := cleanExtraDisplayName(extraPath)
	rec := &sqlite.ItemRecord{
		ID:        itemID,
		LibraryID: lib.ID,
		ParentID:  parent.ID,
		Type:      itemType,
		MediaType: mediaType,
		IsFolder:  false,
		IsStrm:    isStrm,
		Name:      name,
		SortName:  strings.ToLower(name),
		Container: container,
		Path:      extraPath,
		FileSize:  &size,
		FileMtime: &mtime,
		Etag:      computeEtag(extraPath, size, mtime),
		Extra: sqlite.ItemExtra{
			ExtraType: extraType,
		},
	}

	if err := s.repo.UpsertItem(ctx, rec); err != nil {
		return
	}
	_ = s.repo.ReplaceItemAncestors(ctx, rec.ID, []sqlite.AncestorLink{
		{AncestorID: parent.ID, Depth: 1},
		{AncestorID: lib.ID, Depth: 2},
	})
	ms := s.buildMediaSource(rec, extraPath, size, mtime, isStrm, container, nil)
	_ = s.repo.UpsertMediaSourceAndStreams(ctx, rec.ID, ms)
}
