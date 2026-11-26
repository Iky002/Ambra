package scanner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func isExtraFieldLocked(extra *sqlite.ItemExtra, field string) bool {
	if extra == nil {
		return false
	}
	if extra.LockData {
		return true
	}
	for _, f := range extra.LockedFields {
		if strings.EqualFold(strings.TrimSpace(f), field) {
			return true
		}
	}
	return false
}

func (s *Scanner) isCustomMetadataImagePath(imgPath string) bool {
	clean := filepath.Clean(strings.TrimSpace(imgPath))
	if clean == "" {
		return false
	}
	info, err := os.Stat(clean)
	if err != nil || info.IsDir() || info.Size() == 0 {
		return false
	}
	metaDir := filepath.Clean(s.imageStorageDir())
	if clean == metaDir || strings.HasPrefix(clean, metaDir+string(filepath.Separator)) {
		return true
	}
	slashed := filepath.ToSlash(clean)
	return strings.Contains(slashed, "/metadata/images/") || strings.Contains(slashed, "/metadata/custom/")
}

func (s *Scanner) upsertLocalImagesWithOption(ctx context.Context, itemID string, imgs []sqlite.ImageRecord, forceOverwriteCustom bool) (bool, error) {
	if len(imgs) == 0 || s.repo == nil {
		return false, nil
	}
	if existingRec, err := s.repo.GetItemRecordByID(ctx, itemID); err == nil && existingRec != nil && existingRec.Extra.LockData {
		return false, nil
	}

	changed := false
	for i := range imgs {
		img := imgs[i]
		img.ItemID = itemID
		if !forceOverwriteCustom {
			if curImg, err := s.repo.GetImageByItemAndType(ctx, itemID, img.ImageType, img.ImageIndex); err == nil && curImg != nil && curImg.Path != "" {
				if s.isCustomMetadataImagePath(curImg.Path) {
					continue
				}
				if filepath.Clean(curImg.Path) == filepath.Clean(img.Path) && curImg.Tag == img.Tag {
					continue
				}
			}
		}
		if err := s.repo.UpsertImage(ctx, &img); err != nil {
			return changed, err
		}
		changed = true
	}
	return changed, nil
}

// --- Per-Library Metadata Scraper Options ---

func normalizeFetcherAlias(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	switch s {
	case "themoviedb", "tmdb", "the movie db", "ambra.builtin.tmdb":
		return "tmdb"
	case "thetvdb", "tvdb", "the tvdb", "thetvdb scraper", "ambra.builtin.tvdb":
		return "tvdb"
	case "nfo", "local nfo", "kodi nfo", "ambra.builtin.nfo":
		return "nfo"
	default:
		return s
	}
}

func extractFetcherListFromAny(raw any) (enabled []string, disabled []string, order []string, present bool) {
	if raw == nil {
		return nil, nil, nil, false
	}
	switch v := rodSlice(raw).(type) {
	case []string:
		out := make([]string, 0, len(v))
		for _, s := range v {
			if t := strings.TrimSpace(s); t != "" {
				out = append(out, t)
			}
		}
		return out, nil, nil, true
	case []any:
		out := make([]string, 0, len(v))
		var dis []string
		var ord []string
		hasObj := false
		for _, elem := range v {
			switch item := elem.(type) {
			case string:
				if t := strings.TrimSpace(item); t != "" {
					out = append(out, t)
				}
			case map[string]any:
				hasObj = true
				name, _ := item["Name"].(string)
				if name == "" {
					name, _ = item["Id"].(string)
				}
				name = strings.TrimSpace(name)
				if name == "" {
					continue
				}
				ord = append(ord, name)
				isEnabled := true
				if b, ok := item["Enabled"].(bool); ok {
					isEnabled = b
				}
				if isEnabled {
					out = append(out, name)
				} else {
					dis = append(dis, name)
				}
			}
		}
		if hasObj {
			return out, dis, ord, true
		}
		return out, nil, nil, true
	}
	return nil, nil, nil, false
}

func rodSlice(v any) any { return v }

func extractStringSliceAny(raw any) []string {
	switch v := raw.(type) {
	case []string:
		out := make([]string, 0, len(v))
		for _, s := range v {
			if t := strings.TrimSpace(s); t != "" {
				out = append(out, t)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(v))
		for _, e := range v {
			if s, ok := e.(string); ok {
				if t := strings.TrimSpace(s); t != "" {
					out = append(out, t)
				}
			}
		}
		return out
	}
	return nil
}

func findTypeOptionMap(opts map[string]any, itemType string) map[string]any {
	rawList, ok := opts["TypeOptions"].([]any)
	if !ok || len(rawList) == 0 {
		return nil
	}
	matchType := func(target string) map[string]any {
		for _, entry := range rawList {
			m, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			t, _ := m["Type"].(string)
			if strings.EqualFold(strings.TrimSpace(t), target) {
				return m
			}
		}
		return nil
	}
	if m := matchType(itemType); m != nil {
		return m
	}
	if strings.EqualFold(itemType, "Season") || strings.EqualFold(itemType, "Episode") {
		return matchType("Series")
	}
	return nil
}

func (s *Scanner) metadataProvidersForLibrary(lib *sqlite.LibraryRecord, itemType string) []plugins.MetadataProvider {
	if s.registry == nil {
		return nil
	}
	if lib == nil || strings.TrimSpace(lib.OptionsJSON) == "" {
		return s.registry.MetadataProviders()
	}

	opts := sqlite.ParseLibraryOptions(lib.OptionsJSON)
	var (
		explicitEnabled []string
		hasExplicit     bool
		disabledList    []string
		orderList       []string
	)

	// Top-level options
	if en, dis, ord, ok := extractFetcherListFromAny(opts["MetadataFetchers"]); ok {
		explicitEnabled = en
		hasExplicit = true
		disabledList = append(disabledList, dis...)
		if len(ord) > 0 {
			orderList = ord
		}
	}
	disabledList = append(disabledList, extractStringSliceAny(opts["DisabledMetadataFetchers"])...)
	disabledList = append(disabledList, extractStringSliceAny(opts["DisabledLocalMetadataReaders"])...)
	if topOrder := extractStringSliceAny(opts["MetadataFetcherOrder"]); len(topOrder) > 0 {
		orderList = topOrder
	}

	// Per-ItemType TypeOptions override when present
	if typeMap := findTypeOptionMap(opts, itemType); typeMap != nil {
		if en, dis, ord, ok := extractFetcherListFromAny(typeMap["MetadataFetchers"]); ok {
			explicitEnabled = en
			hasExplicit = true
			disabledList = append(disabledList, dis...)
			if len(ord) > 0 {
				orderList = ord
			}
		}
		disabledList = append(disabledList, extractStringSliceAny(typeMap["DisabledMetadataFetchers"])...)
		if typeOrder := extractStringSliceAny(typeMap["MetadataFetcherOrder"]); len(typeOrder) > 0 {
			orderList = typeOrder
		}
	}

	if !hasExplicit && len(disabledList) == 0 && len(orderList) == 0 {
		return s.registry.MetadataProviders()
	}

	disabledAliases := make(map[string]bool, len(disabledList))
	for _, d := range disabledList {
		alias := normalizeFetcherAlias(d)
		if alias != "" {
			disabledAliases[alias] = true
		}
		disabledAliases[strings.ToLower(strings.TrimSpace(d))] = true
	}

	var filterEnabled []string
	if hasExplicit {
		filterEnabled = make([]string, 0, len(explicitEnabled)+1)
		hasNFO := false
		for _, e := range explicitEnabled {
			alias := normalizeFetcherAlias(e)
			if disabledAliases[alias] || disabledAliases[strings.ToLower(strings.TrimSpace(e))] {
				continue
			}
			if alias == "nfo" {
				hasNFO = true
			}
			filterEnabled = append(filterEnabled, e)
		}
		// Keep local NFO reader active unless explicitly disabled.
		if !hasNFO && !disabledAliases["nfo"] && !disabledAliases["ambra.builtin.nfo"] {
			filterEnabled = append(filterEnabled, "ambra.builtin.nfo")
		}
	} else if len(disabledAliases) > 0 {
		all := s.registry.MetadataProviders()
		filterEnabled = make([]string, 0, len(all))
		for _, p := range all {
			m := p.Manifest()
			idAlias := normalizeFetcherAlias(m.ID)
			nameAlias := normalizeFetcherAlias(m.Name)
			if disabledAliases[idAlias] || disabledAliases[nameAlias] ||
				disabledAliases[strings.ToLower(m.ID)] || disabledAliases[strings.ToLower(m.Name)] {
				continue
			}
			filterEnabled = append(filterEnabled, m.ID)
		}
	}

	return s.registry.MetadataProvidersWithFilter(filterEnabled, orderList)
}

func (s *Scanner) hasLibraryOnlineMetadataProviders(lib *sqlite.LibraryRecord, itemType string) bool {
	for _, p := range s.metadataProvidersForLibrary(lib, itemType) {
		if !isLocalMetadataProvider(p) {
			return true
		}
	}
	return false
}

func (s *Scanner) fetchLibraryMetadataFiltered(ctx context.Context, lib *sqlite.LibraryRecord, q plugins.MediaQuery, localOnly bool) *plugins.MetadataResult {
	providers := s.metadataProvidersForLibrary(lib, q.ItemType)
	if len(providers) == 0 {
		return nil
	}
	var merged *plugins.MetadataResult
	for _, p := range providers {
		if localOnly != isLocalMetadataProvider(p) {
			continue
		}
		res, err := p.FetchMetadata(ctx, q)
		if err != nil || res == nil {
			continue
		}
		if merged == nil {
			cp := *res
			if cp.ProviderIDs == nil {
				cp.ProviderIDs = make(map[string]string)
			}
			merged = &cp
			if len(merged.ProviderIDs) > 0 {
				if q.ProviderIDs == nil {
					q.ProviderIDs = make(map[string]string)
				}
				for k, v := range merged.ProviderIDs {
					q.ProviderIDs[k] = v
				}
			}
			continue
		}
		mergeMetadata(merged, res)
	}
	return merged
}

// --- Companion External Subtitles, Local Artwork & NFO Incremental Rescan ---

func sameSubtitleStreams(a, b []sqlite.MediaStreamRecord) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Path != b[i].Path ||
			a[i].Codec != b[i].Codec ||
			a[i].Language != b[i].Language ||
			a[i].SubVariant != b[i].SubVariant ||
			a[i].IsForced != b[i].IsForced ||
			a[i].IsDefault != b[i].IsDefault ||
			a[i].StreamIndex != b[i].StreamIndex {
			return false
		}
	}
	return true
}

func (s *Scanner) rescanItemExternalSubtitles(ctx context.Context, lib *sqlite.LibraryRecord, rec *sqlite.ItemRecord) (bool, error) {
	if s.repo == nil || rec == nil {
		return false, nil
	}
	if strings.EqualFold(rec.MediaType, "Audio") || strings.EqualFold(rec.Type, "Audio") || rec.IsFolder {
		return false, nil
	}
	sources, err := s.repo.GetMediaSourcesByItemID(ctx, rec.ID)
	if err != nil || len(sources) == 0 {
		return false, err
	}

	policy := s.currentSymlinkPolicy()
	changed := false
	for _, src := range sources {
		if strings.TrimSpace(src.Path) == "" {
			continue
		}
		var libRoot string
		if lib != nil {
			libRoot = findContainingRoot(lib.Paths, src.Path)
		}
		if libRoot == "" {
			libRoot = filepath.Dir(src.Path)
		}

		var embedded []sqlite.MediaStreamRecord
		var oldExternal []sqlite.MediaStreamRecord
		nextIdx := 0
		for _, st := range src.Streams {
			if st.IsExternal {
				oldExternal = append(oldExternal, st)
				continue
			}
			embedded = append(embedded, st)
			if st.StreamIndex >= nextIdx {
				nextIdx = st.StreamIndex + 1
			}
		}

		newExternal := discoverExternalSubtitlesWithPolicy(libRoot, src.Path, src.ID, rec.ID, nextIdx, policy)
		if sameSubtitleStreams(oldExternal, newExternal) {
			continue
		}

		src.Streams = append(embedded, newExternal...)
		if err := s.repo.UpsertMediaSourceAndStreams(ctx, rec.ID, src); err != nil {
			return changed, err
		}
		changed = true
	}

	if changed {
		rec.DateModified = time.Now().Unix()
		rec.Etag = guid.New()[:16]
		_ = s.repo.UpsertItem(ctx, rec)
	}
	return changed, nil
}

func (s *Scanner) rescanItemLocalImages(ctx context.Context, lib *sqlite.LibraryRecord, rec *sqlite.ItemRecord, forceOverwriteCustom bool) (bool, error) {
	if s.repo == nil || rec == nil || rec.Extra.LockData || strings.TrimSpace(rec.Path) == "" {
		return false, nil
	}
	policy := s.currentSymlinkPolicy()
	itemPath := filepath.Clean(strings.TrimSpace(rec.Path))
	var libRoot string
	if lib != nil {
		libRoot = findContainingRoot(lib.Paths, itemPath)
	}

	var imgs []sqlite.ImageRecord
	switch strings.ToLower(strings.TrimSpace(rec.Type)) {
	case "movie":
		dir := filepath.Dir(itemPath)
		if rec.IsFolder || isExistingDirPath(itemPath) {
			dir = itemPath
		}
		if libRoot == "" {
			libRoot = dir
		}
		imgs = discoverLocalImagesWithPolicy(libRoot, rec.ID, itemPath, dir, false, policy)

	case "series":
		dir := itemPath
		if !isExistingDirPath(dir) {
			dir = filepath.Dir(itemPath)
		}
		if libRoot == "" {
			libRoot = dir
		}
		imgs = discoverLocalImagesWithPolicy(libRoot, rec.ID, "", dir, false, policy)

	case "season":
		seasonDir := itemPath
		if idx := strings.IndexByte(seasonDir, '#'); idx > 0 {
			seasonDir = seasonDir[:idx]
		}
		seriesDir := s.resolveSeriesDir(ctx, rec)
		if libRoot == "" {
			libRoot = seriesDir
		}
		seasonNum := 1
		if rec.IndexNumber != nil {
			seasonNum = *rec.IndexNumber
		}
		imgs = discoverSeasonImagesWithPolicy(libRoot, rec.ID, seasonNum, seasonDir, seriesDir, policy)

	case "episode":
		dir := filepath.Dir(itemPath)
		if libRoot == "" {
			libRoot = dir
		}
		imgs = discoverLocalImagesWithPolicy(libRoot, rec.ID, itemPath, dir, true, policy)

	case "musicartist":
		if isExistingDirPath(itemPath) {
			if libRoot == "" {
				libRoot = itemPath
			}
			imgs = discoverMusicDirImagesWithPolicy(libRoot, rec.ID, itemPath, []string{"artist", "folder", "poster", "cover"}, policy)
		}

	case "musicalbum":
		if isExistingDirPath(itemPath) {
			if libRoot == "" {
				libRoot = itemPath
			}
			imgs = discoverMusicDirImagesWithPolicy(libRoot, rec.ID, itemPath, []string{"cover", "folder", "front", "album", "poster"}, policy)
		}
	}

	if len(imgs) == 0 {
		return false, nil
	}
	changed, err := s.upsertLocalImagesWithOption(ctx, rec.ID, imgs, forceOverwriteCustom)
	if changed {
		rec.DateModified = time.Now().Unix()
		rec.Etag = guid.New()[:16]
		_ = s.repo.UpsertItem(ctx, rec)
	}
	return changed, err
}

func (s *Scanner) findItemsInOrMatchingDir(ctx context.Context, libraryID, dir string) []*sqlite.ItemRecord {
	cleanDir := filepath.Clean(strings.TrimSpace(dir))
	if s.repo == nil || cleanDir == "" {
		return nil
	}
	seen := make(map[string]struct{})
	var out []*sqlite.ItemRecord
	add := func(rec *sqlite.ItemRecord) {
		if rec == nil || rec.ID == "" {
			return
		}
		if _, ok := seen[rec.ID]; ok {
			return
		}
		seen[rec.ID] = struct{}{}
		out = append(out, rec)
	}

	if rec, err := s.repo.FindItemByPath(ctx, libraryID, cleanDir); err == nil && rec != nil {
		add(rec)
		// If cleanDir is a Series folder, also include its Seasons so season{NN}-poster.* or season.nfo updates apply.
		if strings.EqualFold(rec.Type, "Series") {
			if seasons, _, sErr := s.repo.QueryItems(ctx, sqlite.ItemFilter{
				LibraryID:        libraryID,
				SeriesID:         rec.ID,
				IncludeItemTypes: []string{"Season"},
			}); sErr == nil {
				for _, sd := range seasons {
					if sRec, gErr := s.repo.GetItemRecordByID(ctx, sd.Id); gErr == nil {
						add(sRec)
					}
				}
			}
		}
	}

	if itemPaths, err := s.repo.ListLibraryItemPaths(ctx, libraryID); err == nil {
		for p, itemID := range itemPaths {
			if filepath.Clean(filepath.Dir(p)) != cleanDir {
				continue
			}
			if rec, gErr := s.repo.GetItemRecordByID(ctx, itemID); gErr == nil && rec != nil {
				add(rec)
				if rec.SeasonID != "" {
					if seaRec, sErr := s.repo.GetItemRecordByID(ctx, rec.SeasonID); sErr == nil {
						add(seaRec)
					}
				}
				if rec.SeriesID != "" {
					if serRec, sErr := s.repo.GetItemRecordByID(ctx, rec.SeriesID); sErr == nil {
						add(serRec)
					}
				}
				if rec.ParentID != "" && rec.ParentID != libraryID {
					if parRec, pErr := s.repo.GetItemRecordByID(ctx, rec.ParentID); pErr == nil {
						add(parRec)
					}
				}
			}
		}
	}
	return out
}

func isCompanionSidecarFile(path string) (isSub, isImg, isNFO bool) {
	if _, ok := SubtitleCodecForExt(path); ok {
		return true, false, false
	}
	if IsImageFile(path) {
		return false, true, false
	}
	if strings.EqualFold(filepath.Ext(path), ".nfo") {
		return false, false, true
	}
	return false, false, false
}

func (s *Scanner) handleCompanionFileChange(ctx context.Context, lib *sqlite.LibraryRecord, cleanTarget string, deleted bool) (bool, error) {
	isSub, isImg, isNFO := isCompanionSidecarFile(cleanTarget)
	if !isSub && !isImg && !isNFO {
		return false, nil
	}
	dir := filepath.Dir(cleanTarget)
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return false, nil
	}

	candidates := s.findItemsInOrMatchingDir(ctx, lib.ID, dir)
	if len(candidates) == 0 {
		return false, nil
	}

	updatedAny := false
	if isSub {
		for _, rec := range candidates {
			if strings.EqualFold(rec.Type, "Movie") || strings.EqualFold(rec.Type, "Episode") {
				if changed, err := s.rescanItemExternalSubtitles(ctx, lib, rec); err == nil && changed {
					updatedAny = true
				}
			}
		}
		return updatedAny, nil
	}

	if deleted {
		return false, nil
	}

	if isImg {
		for _, rec := range candidates {
			if changed, err := s.rescanItemLocalImages(ctx, lib, rec, false); err == nil && changed {
				updatedAny = true
			}
		}
		return updatedAny, nil
	}

	if isNFO {
		baseLower := strings.ToLower(filepath.Base(cleanTarget))
		stemLower := strings.TrimSuffix(baseLower, filepath.Ext(baseLower))
		movieCountInDir := 0
		for _, rec := range candidates {
			if strings.EqualFold(rec.Type, "Movie") {
				movieCountInDir++
			}
		}

		for _, rec := range candidates {
			if rec.Extra.LockData {
				continue
			}
			itemTypeLower := strings.ToLower(rec.Type)
			matched := false
			switch baseLower {
			case "tvshow.nfo":
				matched = itemTypeLower == "series" && filepath.Clean(rec.Path) == filepath.Clean(dir)
			case "season.nfo":
				matched = itemTypeLower == "season" && strings.HasPrefix(filepath.Clean(rec.Path), filepath.Clean(dir))
			case "artist.nfo":
				matched = itemTypeLower == "musicartist" && filepath.Clean(rec.Path) == filepath.Clean(dir)
			case "album.nfo":
				matched = itemTypeLower == "musicalbum" && filepath.Clean(rec.Path) == filepath.Clean(dir)
			case "movie.nfo":
				matched = itemTypeLower == "movie" && (movieCountInDir == 1 || filepath.Clean(rec.Path) == filepath.Clean(dir))
			default:
				if itemTypeLower == "movie" || itemTypeLower == "episode" {
					recStem := strings.ToLower(strings.TrimSuffix(filepath.Base(rec.Path), filepath.Ext(rec.Path)))
					if recStem == stemLower {
						matched = true
					} else if srcs, sErr := s.repo.GetMediaSourcesByItemID(ctx, rec.ID); sErr == nil {
						for _, src := range srcs {
							sStem := strings.ToLower(strings.TrimSuffix(filepath.Base(src.Path), filepath.Ext(src.Path)))
							if sStem == stemLower {
								matched = true
								break
							}
						}
					}
				}
			}
			if !matched {
				continue
			}

			q := plugins.MediaQuery{
				ItemType:      rec.Type,
				Title:         rec.Name,
				OriginalTitle: rec.OriginalTitle,
				Path:          rec.Path,
				ProviderIDs:   cloneStringMap(rec.Extra.ProviderIDs),
				Language:      s.language(),
			}
			if rec.IndexNumber != nil {
				if itemTypeLower == "season" {
					q.SeasonNumber = *rec.IndexNumber
				} else if itemTypeLower == "episode" {
					q.EpisodeNumber = *rec.IndexNumber
				}
			}
			if itemTypeLower == "episode" && rec.ParentIndexNumber != nil {
				q.SeasonNumber = *rec.ParentIndexNumber
			}

			nfoMeta := s.fetchLibraryMetadataFiltered(ctx, lib, q, true)
			if nfoMeta == nil {
				continue
			}
			applyRefreshMetadataToItem(rec, nfoMeta, true)
			rec.DateModified = time.Now().Unix()
			rec.Etag = guid.New()[:16]
			if err := s.repo.UpsertItem(ctx, rec); err == nil {
				relMeta := filterLockedRelations(rec, nfoMeta)
				s.persistMetaRelationsAndFTS(ctx, rec, relMeta)
				if itemTypeLower == "movie" && nfoMeta.Collection != nil {
					s.ensureAutoBoxSet(ctx, rec, nfoMeta.Collection)
				}
				updatedAny = true
			}
		}
	}

	return updatedAny, nil
}
