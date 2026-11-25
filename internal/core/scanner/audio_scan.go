package scanner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func (s *Scanner) indexAudio(
	ctx context.Context,
	lib *sqlite.LibraryRecord,
	root, mediaPath string,
	artistCache, albumCache map[string]*sqlite.ItemRecord,
) (bool, *enrichTask, error) {
	info, err := os.Stat(mediaPath)
	if err != nil {
		return false, nil, err
	}
	mtime := info.ModTime().Unix()
	size := info.Size()

	container := strings.TrimPrefix(strings.ToLower(filepath.Ext(mediaPath)), ".")
	needProbe := s.getProber().IsEnabled()

	existing, err := s.repo.FindItemByMediaSourcePath(ctx, lib.ID, mediaPath)
	if err == nil && existing != nil {
		if s.isMediaSourceUnchanged(ctx, existing, mediaPath, size, mtime) {
			return false, nil, nil
		}
	}

	parsed := ParseAudioPath(root, mediaPath)

	var artistRec *sqlite.ItemRecord
	if parsed.Artist != "" {
		artistRec, err = s.ensureMusicArtist(ctx, lib, parsed, artistCache)
		if err != nil {
			return false, nil, err
		}
	}

	var albumRec *sqlite.ItemRecord
	if parsed.Album != "" {
		albumRec, err = s.ensureMusicAlbum(ctx, lib, artistRec, parsed, albumCache)
		if err != nil {
			return false, nil, err
		}
	}

	itemID := guid.New()
	if existing != nil {
		itemID = existing.ID
	}

	parentID := lib.ID
	var ancestors []sqlite.AncestorLink
	switch {
	case albumRec != nil && artistRec != nil:
		parentID = albumRec.ID
		ancestors = []sqlite.AncestorLink{
			{AncestorID: albumRec.ID, Depth: 1},
			{AncestorID: artistRec.ID, Depth: 2},
			{AncestorID: lib.ID, Depth: 3},
		}
	case albumRec != nil:
		parentID = albumRec.ID
		ancestors = []sqlite.AncestorLink{
			{AncestorID: albumRec.ID, Depth: 1},
			{AncestorID: lib.ID, Depth: 2},
		}
	case artistRec != nil:
		parentID = artistRec.ID
		ancestors = []sqlite.AncestorLink{
			{AncestorID: artistRec.ID, Depth: 1},
			{AncestorID: lib.ID, Depth: 2},
		}
	default:
		ancestors = []sqlite.AncestorLink{
			{AncestorID: lib.ID, Depth: 1},
		}
	}

	title := parsed.Title
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(mediaPath), filepath.Ext(mediaPath))
	}

	sortName := strings.ToLower(title)
	if parsed.TrackNumber > 0 {
		sortName = fmt.Sprintf("%02d-%04d-%s", parsed.DiscNumber, parsed.TrackNumber, strings.ToLower(title))
	}

	rec := &sqlite.ItemRecord{
		ID:        itemID,
		LibraryID: lib.ID,
		ParentID:  parentID,
		Type:      "Audio",
		MediaType: "Audio",
		IsFolder:  false,
		Name:      title,
		SortName:  sortName,
		Container: container,
		Path:      mediaPath,
		FileSize:  &size,
		FileMtime: &mtime,
		Etag:      computeEtag(mediaPath, size, mtime),
		Extra: sqlite.ItemExtra{
			Album:       parsed.Album,
			AlbumArtist: parsed.Artist,
		},
	}
	if albumRec != nil {
		rec.Extra.AlbumID = albumRec.ID
	}
	if parsed.Artist != "" {
		rec.Extra.Artists = []string{parsed.Artist}
	}
	if parsed.TrackNumber > 0 {
		tn := parsed.TrackNumber
		rec.IndexNumber = &tn
	}
	if parsed.DiscNumber > 0 {
		dn := parsed.DiscNumber
		rec.ParentIndexNumber = &dn
	}
	if parsed.Year > 0 {
		y := parsed.Year
		rec.ProductionYear = &y
	}

	if err := s.repo.UpsertItem(ctx, rec); err != nil {
		return false, nil, err
	}
	if err := s.repo.ReplaceItemAncestors(ctx, rec.ID, ancestors); err != nil {
		return false, nil, err
	}

	ms := s.buildMediaSource(rec, mediaPath, size, mtime, false, container, nil)
	if err := s.repo.UpsertMediaSourceAndStreams(ctx, rec.ID, ms); err != nil {
		return false, nil, err
	}

	titleTokens := TokenizeForFTS(rec.Name, parsed.Album, parsed.Artist)
	_ = s.repo.UpsertSearchFTS(ctx, rec.ID, rec.Name, titleTokens, "", "", parsed.Artist)

	var task *enrichTask
	if needProbe {
		task = &enrichTask{
			ItemID:      rec.ID,
			LibraryID:   lib.ID,
			ItemType:    "Audio",
			MediaPath:   mediaPath,
			Size:        size,
			Mtime:       mtime,
			Container:   container,
			NeedProbe:   true,
			DisplayName: rec.Name,
		}
	}
	return true, task, nil
}

func (s *Scanner) ensureMusicArtist(
	ctx context.Context,
	lib *sqlite.LibraryRecord,
	parsed ParsedAudio,
	cache map[string]*sqlite.ItemRecord,
) (*sqlite.ItemRecord, error) {
	artistPath := parsed.ArtistDir
	if artistPath == "" {
		artistPath = fmt.Sprintf("artist://%s/%s", lib.ID, strings.ToLower(parsed.Artist))
	}
	if rec, ok := cache[artistPath]; ok {
		return rec, nil
	}

	existing, err := s.repo.FindItemByPath(ctx, lib.ID, artistPath)
	if err == nil && existing != nil {
		cache[artistPath] = existing
		if parsed.ArtistDir != "" && s.isMissingPrimaryImage(ctx, existing.ID) {
			if imgs := discoverMusicDirImages(existing.ID, parsed.ArtistDir, []string{"artist", "folder", "poster", "cover"}); len(imgs) > 0 {
				_ = s.repo.ReplaceItemImages(ctx, existing.ID, imgs)
			}
		}
		return existing, nil
	}
	if err != nil && !errors.Is(err, sqlite.ErrNotFound) {
		return nil, err
	}

	rec := &sqlite.ItemRecord{
		ID:        guid.New(),
		LibraryID: lib.ID,
		ParentID:  lib.ID,
		Type:      "MusicArtist",
		IsFolder:  true,
		Name:      parsed.Artist,
		SortName:  strings.ToLower(parsed.Artist),
		Path:      artistPath,
		Etag:      computeEtag(artistPath, 0, time.Now().Unix()),
	}
	if err := s.repo.UpsertItem(ctx, rec); err != nil {
		return nil, err
	}
	if err := s.repo.ReplaceItemAncestors(ctx, rec.ID, []sqlite.AncestorLink{
		{AncestorID: lib.ID, Depth: 1},
	}); err != nil {
		return nil, err
	}
	if parsed.ArtistDir != "" {
		if imgs := discoverMusicDirImages(rec.ID, parsed.ArtistDir, []string{"artist", "folder", "poster", "cover"}); len(imgs) > 0 {
			_ = s.repo.ReplaceItemImages(ctx, rec.ID, imgs)
		}
	}
	_ = s.repo.UpsertSearchFTS(ctx, rec.ID, rec.Name, TokenizeForFTS(rec.Name), "", "", "")

	cache[artistPath] = rec
	return rec, nil
}

func (s *Scanner) ensureMusicAlbum(
	ctx context.Context,
	lib *sqlite.LibraryRecord,
	artistRec *sqlite.ItemRecord,
	parsed ParsedAudio,
	cache map[string]*sqlite.ItemRecord,
) (*sqlite.ItemRecord, error) {
	albumPath := parsed.AlbumDir
	if albumPath == "" {
		albumPath = fmt.Sprintf("album://%s/%s", lib.ID, strings.ToLower(parsed.Album))
	}
	if rec, ok := cache[albumPath]; ok {
		return rec, nil
	}

	existing, err := s.repo.FindItemByPath(ctx, lib.ID, albumPath)
	if err == nil && existing != nil {
		cache[albumPath] = existing
		if parsed.AlbumDir != "" && s.isMissingPrimaryImage(ctx, existing.ID) {
			if imgs := discoverMusicDirImages(existing.ID, parsed.AlbumDir, []string{"cover", "folder", "front", "album", "poster"}); len(imgs) > 0 {
				_ = s.repo.ReplaceItemImages(ctx, existing.ID, imgs)
			}
		}
		return existing, nil
	}
	if err != nil && !errors.Is(err, sqlite.ErrNotFound) {
		return nil, err
	}

	parentID := lib.ID
	ancestors := []sqlite.AncestorLink{
		{AncestorID: lib.ID, Depth: 1},
	}
	if artistRec != nil {
		parentID = artistRec.ID
		ancestors = []sqlite.AncestorLink{
			{AncestorID: artistRec.ID, Depth: 1},
			{AncestorID: lib.ID, Depth: 2},
		}
	}

	aspect := 1.0
	rec := &sqlite.ItemRecord{
		ID:        guid.New(),
		LibraryID: lib.ID,
		ParentID:  parentID,
		Type:      "MusicAlbum",
		IsFolder:  true,
		Name:      parsed.Album,
		SortName:  strings.ToLower(parsed.Album),
		Path:      albumPath,
		Etag:      computeEtag(albumPath, 0, time.Now().Unix()),
		Extra: sqlite.ItemExtra{
			AlbumArtist:             parsed.Artist,
			PrimaryImageAspectRatio: &aspect,
		},
	}
	if parsed.Artist != "" {
		rec.Extra.Artists = []string{parsed.Artist}
	}
	if parsed.Year > 0 {
		y := parsed.Year
		rec.ProductionYear = &y
	}

	if err := s.repo.UpsertItem(ctx, rec); err != nil {
		return nil, err
	}
	if err := s.repo.ReplaceItemAncestors(ctx, rec.ID, ancestors); err != nil {
		return nil, err
	}
	if parsed.AlbumDir != "" {
		if imgs := discoverMusicDirImages(rec.ID, parsed.AlbumDir, []string{"cover", "folder", "front", "album", "poster"}); len(imgs) > 0 {
			_ = s.repo.ReplaceItemImages(ctx, rec.ID, imgs)
		}
	}
	_ = s.repo.UpsertSearchFTS(ctx, rec.ID, rec.Name, TokenizeForFTS(rec.Name, parsed.Artist), "", "", parsed.Artist)

	cache[albumPath] = rec
	return rec, nil
}

func discoverMusicDirImages(itemID, dir string, primaryStems []string) []sqlite.ImageRecord {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	byStem := make(map[string]string)
	var backdropPaths []string
	for _, entry := range entries {
		if entry.IsDir() || !IsImageFile(entry.Name()) {
			continue
		}
		name := entry.Name()
		stem := strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
		fullPath := filepath.Join(dir, name)
		if _, exists := byStem[stem]; !exists {
			byStem[stem] = fullPath
		}
		if stem == "backdrop" || stem == "fanart" || strings.HasPrefix(stem, "backdrop") || strings.HasPrefix(stem, "fanart") {
			backdropPaths = append(backdropPaths, fullPath)
		}
	}

	var out []sqlite.ImageRecord
	for _, stem := range primaryStems {
		if fullPath, ok := byStem[stem]; ok {
			if info, err := os.Stat(fullPath); err == nil {
				out = append(out, sqlite.ImageRecord{
					ItemID:     itemID,
					ImageType:  "Primary",
					ImageIndex: 0,
					Path:       fullPath,
					Tag:        computeEtag(fullPath, info.Size(), info.ModTime().Unix()),
				})
				break
			}
		}
	}

	for idx, fullPath := range backdropPaths {
		if info, err := os.Stat(fullPath); err == nil {
			out = append(out, sqlite.ImageRecord{
				ItemID:     itemID,
				ImageType:  "Backdrop",
				ImageIndex: idx,
				Path:       fullPath,
				Tag:        computeEtag(fullPath, info.Size(), info.ModTime().Unix()),
			})
		}
	}
	return out
}

func defaultAudioCodecForContainer(container string) string {
	switch strings.ToLower(strings.TrimSpace(container)) {
	case "mp3":
		return "mp3"
	case "flac":
		return "flac"
	case "m4a", "aac":
		return "aac"
	case "ogg":
		return "vorbis"
	case "opus":
		return "opus"
	case "wav":
		return "pcm_s16le"
	case "wma":
		return "wmav2"
	case "alac":
		return "alac"
	case "ape":
		return "ape"
	case "dsf":
		return "dsd_lsbf"
	default:
		if container != "" {
			return strings.ToLower(container)
		}
		return "aac"
	}
}

// applyAudioProbeTags enriches an Audio ItemRecord's Name, Album, Artists, AlbumArtist,
// ProductionYear, and track/disc numbers from embedded ID3/Vorbis/FLAC format tags.
func applyAudioProbeTags(rec *sqlite.ItemRecord, tags map[string]string) bool {
	if rec == nil || len(tags) == 0 || rec.Extra.LockData {
		return false
	}
	changed := false

	if title := tagValue(tags, "title"); title != "" && !isFieldLocked(rec, "Name") {
		if rec.Name != title {
			rec.Name = title
			if rec.IndexNumber != nil && *rec.IndexNumber > 0 {
				dn := 0
				if rec.ParentIndexNumber != nil {
					dn = *rec.ParentIndexNumber
				}
				rec.SortName = fmt.Sprintf("%02d-%04d-%s", dn, *rec.IndexNumber, strings.ToLower(title))
			} else {
				rec.SortName = strings.ToLower(title)
			}
			changed = true
		}
	}

	if album := tagValue(tags, "album"); album != "" {
		if rec.Extra.Album != album {
			rec.Extra.Album = album
			changed = true
		}
	}

	if artistRaw := tagValue(tags, "artist"); artistRaw != "" {
		var artists []string
		for _, part := range strings.FieldsFunc(artistRaw, func(r rune) bool { return r == ';' || r == '/' }) {
			if s := strings.TrimSpace(part); s != "" {
				artists = append(artists, s)
			}
		}
		if len(artists) > 0 {
			rec.Extra.Artists = artists
			if rec.Extra.AlbumArtist == "" {
				rec.Extra.AlbumArtist = artists[0]
			}
			changed = true
		}
	}

	if albumArtist := tagValue(tags, "album_artist"); albumArtist != "" {
		if rec.Extra.AlbumArtist != albumArtist {
			rec.Extra.AlbumArtist = albumArtist
			changed = true
		}
	} else if albumArtist := tagValue(tags, "albumartist"); albumArtist != "" {
		if rec.Extra.AlbumArtist != albumArtist {
			rec.Extra.AlbumArtist = albumArtist
			changed = true
		}
	}

	if !isFieldLocked(rec, "ProductionYear") {
		rawDate := tagValue(tags, "date")
		if rawDate == "" {
			rawDate = tagValue(tags, "year")
		}
		if len(rawDate) >= 4 {
			if yr, err := strconv.Atoi(rawDate[:4]); err == nil && yr >= 1900 && yr <= 2100 {
				if rec.ProductionYear == nil || *rec.ProductionYear != yr {
					rec.ProductionYear = &yr
					changed = true
				}
			}
		}
	}

	if rec.IndexNumber == nil {
		if rawTrack := tagValue(tags, "track"); rawTrack != "" {
			numPart := strings.SplitN(rawTrack, "/", 2)[0]
			if tn, err := strconv.Atoi(strings.TrimSpace(numPart)); err == nil && tn > 0 {
				rec.IndexNumber = &tn
				changed = true
			}
		}
	}

	if rec.ParentIndexNumber == nil {
		if rawDisc := tagValue(tags, "disc"); rawDisc != "" {
			numPart := strings.SplitN(rawDisc, "/", 2)[0]
			if dn, err := strconv.Atoi(strings.TrimSpace(numPart)); err == nil && dn > 0 {
				rec.ParentIndexNumber = &dn
				changed = true
			}
		}
	}

	return changed
}
