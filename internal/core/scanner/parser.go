package scanner

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

var (
	videoExtensions = map[string]bool{
		".mkv":  true,
		".mp4":  true,
		".avi":  true,
		".ts":   true,
		".m2ts": true,
		".mov":  true,
		".webm": true,
	}

	audioExtensions = map[string]bool{
		".mp3":  true,
		".flac": true,
		".m4a":  true,
		".aac":  true,
		".ogg":  true,
		".opus": true,
		".wav":  true,
		".wma":  true,
		".alac": true,
		".ape":  true,
		".dsf":  true,
	}

	subtitleExtensions = map[string]string{
		".srt": "subrip",
		".ass": "ass",
		".ssa": "ssa",
		".vtt": "vtt",
		".sup": "pgssub",
	}

	imageExtensions = map[string]bool{
		".jpg":  true,
		".jpeg": true,
		".png":  true,
		".webp": true,
		".gif":  true,
	}

	reSeasonEpisode   = regexp.MustCompile(`(?i)(?:^|[\s._\-\[\(])S(\d{1,2})[\s._\-]*E(\d{1,3})(?:[\s._\-\]\)]|$)`)
	reAltEpisode      = regexp.MustCompile(`(?i)(?:^|[\s._\-])(\d{1,2})x(\d{1,3})(?:[\s._\-]|$)`)
	reSeasonDir       = regexp.MustCompile(`(?i)^(?:season|s|第)\s*0*(\d{1,2})\s*(?:季)?$`)
	reYearParens      = regexp.MustCompile(`[\(\[（【]((?:19|20)\d{2})[\)\]）】]`)
	reYearDigits      = regexp.MustCompile(`(?:19|20)\d{2}`)
	reProviderTag     = regexp.MustCompile(`(?i)[\{\[\(（【]\s*(tmdb|imdb|tvdb)(?:id)?\s*[-:=]\s*(tt\d+|\d+)\s*[\}\]）)】]`)
	reLeadingBracket  = regexp.MustCompile(`^\s*(?:[\[\(（【][^\]\)）】]{1,40}[\]\)）】]\s*)+`)
	reQualityNoise    = regexp.MustCompile(`(?i)(?:^|[\s._\-\[\(（【])(2160p|1080p|1080i|720p|576p|480p|4k|uhd|hdr10\+|hdr10plus|hdr10|hdr|hlg|dovi|dv|dolby[\s._]?vision|bluray|blu-ray|bdrip|bdremux|web-dl|webdl|webrip|hdtv|dvdrip|remux|x264|x265|h\.?264|h\.?265|hevc|avc|av1|aac|dts-hd|dtshd|dts|ac3|eac3|ddp|dd5\.?1|atmos|truehd|flac|lpcm|10bit|8bit|60fps|120fps|hq|imax|proper|repack|extended|unrated|directors?[\s._]?cut|theatrical)(?:$|[\s._\-\]\)）】]).*$`)
	reChineseEdition  = regexp.MustCompile(`(?:^|[\s._\-\[\(（【])(国粤双语|国语|粤语|中英双字|双语字幕|简繁字幕|特效字幕|中字|未删减版?|导演剪辑版?|加长版|高码版|收藏版|臻彩|帧享|杜比视界|杜比全景声).*$`)
	reAudioDiscDir    = regexp.MustCompile(`(?i)^(?:disc|cd|disk)\s*0*(\d{1,2})$`)
	reAudioDiscTrack  = regexp.MustCompile(`^(\d{1,2})[-.](\d{1,3})(?:\s*[-._]\s*|\s+)(.*)$`)
	reAudioTrack      = regexp.MustCompile(`^(\d{1,3})(?:\s*[-._]\s*|\s+)(.*)$`)
	reAlbumYearLead   = regexp.MustCompile(`^(?:[\(\[（]((?:19|20)\d{2})[\)\]）]|((?:19|20)\d{2}))\s*[-._]?\s*(.+)$`)

	genericMovieDirs = map[string]bool{
		"movie": true, "movies": true, "film": true, "films": true,
		"video": true, "videos": true, "media": true, "strm": true,
		"4k": true, "uhd": true, "2160p": true, "1080p": true, "720p": true,
		"bluray": true, "remux": true, "downloads": true, "download": true,
		"电影": true, "影视": true, "影片": true, "高清电影": true, "4k电影": true,
	}
)

// IsVideoFile reports whether the file extension is a supported video format.
func IsVideoFile(path string) bool {
	return videoExtensions[strings.ToLower(filepath.Ext(path))]
}

// IsAudioFile reports whether the file extension is a supported audio format.
func IsAudioFile(path string) bool {
	return audioExtensions[strings.ToLower(filepath.Ext(path))]
}

// IsStrmFile reports whether the file is a .strm file.
func IsStrmFile(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".strm")
}

// SubtitleCodecForExt returns the canonical codec name for a subtitle file extension.
func SubtitleCodecForExt(path string) (string, bool) {
	c, ok := subtitleExtensions[strings.ToLower(filepath.Ext(path))]
	return c, ok
}

// IsImageFile reports whether the file extension is a supported image format.
func IsImageFile(path string) bool {
	return imageExtensions[strings.ToLower(filepath.Ext(path))]
}

// ParsedMovie holds parsed movie title, optional alternate/original title, year,
// and any explicit provider IDs extracted from a filename or folder name.
type ParsedMovie struct {
	Title         string
	OriginalTitle string
	Year          int
	ProviderIDs   map[string]string
}

func maxValidReleaseYear() int {
	return time.Now().Year() + 2
}

func isValidReleaseYear(y int) bool {
	return y >= 1900 && y <= maxValidReleaseYear()
}

func isYearBoundaryRune(r rune) bool {
	if r == 0 {
		return true
	}
	return unicode.IsSpace(r) || r == '.' || r == '_' || r == '-' ||
		r == '(' || r == ')' || r == '[' || r == ']' ||
		r == '（' || r == '）' || r == '【' || r == '】' || r == '/'
}

func extractProviderIDsFromName(s string) (string, map[string]string) {
	matches := reProviderTag.FindAllStringSubmatch(s, -1)
	if len(matches) == 0 {
		return s, nil
	}
	ids := make(map[string]string, len(matches))
	for _, m := range matches {
		if len(m) >= 3 {
			ids[strings.ToLower(m[1])] = strings.TrimSpace(m[2])
		}
	}
	cleaned := reProviderTag.ReplaceAllString(s, " ")
	return cleaned, ids
}

func splitBilingualTitle(title string) (string, string) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", ""
	}
	// Check if title starts with CJK characters followed by an English/Latin title
	// e.g. "肖申克的救赎 The Shawshank Redemption" or "流浪地球2 The Wandering Earth II"
	fields := strings.Fields(title)
	if len(fields) < 2 {
		return title, ""
	}
	splitIdx := -1
	seenCJK := false
	for i, f := range fields {
		if containsCJK(f) {
			seenCJK = true
			continue
		}
		if seenCJK && containsASCIIAlpha(f) {
			splitIdx = i
			break
		}
	}
	if splitIdx > 0 {
		// Verify remainder has no CJK (pure English/Latin alternate title)
		rest := strings.Join(fields[splitIdx:], " ")
		if !containsCJK(rest) && len(rest) >= 2 {
			return strings.TrimSpace(strings.Join(fields[:splitIdx], " ")), strings.TrimSpace(rest)
		}
	}
	return title, ""
}

func containsCJK(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r) {
			return true
		}
	}
	return false
}

func containsASCIIAlpha(s string) bool {
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return true
		}
	}
	return false
}

func stripMediaExt(filename string) string {
	base := filepath.Base(filename)
	ext := strings.ToLower(filepath.Ext(base))
	if ext != "" && (videoExtensions[ext] || audioExtensions[ext] || ext == ".strm" || ext == ".nfo" || ext == ".iso") {
		return strings.TrimSuffix(base, filepath.Ext(base))
	}
	return base
}

// ParseMovieName extracts a clean title, optional alternate title, production year,
// and inline provider IDs from a file or directory name using MoviePilot-inspired rules:
//   - Prefers the LAST valid release year (1900..currentYear+2) so titles containing
//     numbers/years (e.g. "The.Legend.of.1900.1998", "Blade.Runner.2049.2017",
//     "2001.A.Space.Odyssey.1968", "1917.2019") are never truncated prematurely.
//   - Supports full-width CJK brackets （） 【】 and inline {tmdb-12345} / [imdb=tt...] tags.
//   - Strips release-group prefixes, technical codecs/resolutions, and Chinese edition tags.
func ParseMovieName(filename string) ParsedMovie {
	base := stripMediaExt(filename)
	base, providerIDs := extractProviderIDsFromName(base)

	// Strip leading release-group brackets only when meaningful title text follows.
	if stripped := strings.TrimSpace(reLeadingBracket.ReplaceAllString(base, "")); stripped != "" {
		base = stripped
	}

	year := 0

	// 1. Try parenthesized years first: (), [], （）, 【】 — pick the LAST valid year.
	if allParens := reYearParens.FindAllStringSubmatchIndex(base, -1); len(allParens) > 0 {
		for i := len(allParens) - 1; i >= 0; i-- {
			m := allParens[i]
			y, _ := strconv.Atoi(base[m[2]:m[3]])
			if !isValidReleaseYear(y) {
				continue
			}
			before := strings.TrimSpace(base[:m[0]])
			after := strings.TrimSpace(base[m[1]:])
			if cleanTitle(before) != "" {
				year = y
				base = before
				break
			}
			if cleanTitle(after) != "" {
				year = y
				base = after
				break
			}
		}
	}

	// 2. Strip quality/codec/Chinese edition noise BEFORE bare-year fallback only after
	//    recording candidate bare years so a release year right before "1080p" is kept.
	if year == 0 {
		// Scan all 4-digit year tokens with explicit boundary checks so consecutive
		// years like "1900.1998" or "2049.2017" are BOTH discovered.
		allDigits := reYearDigits.FindAllStringIndex(base, -1)
		for i := len(allDigits) - 1; i >= 0; i-- {
			start, end := allDigits[i][0], allDigits[i][1]
			var prevRune, nextRune rune
			if start > 0 {
				r := []rune(base[:start])
				prevRune = r[len(r)-1]
			}
			if end < len(base) {
				r := []rune(base[end:])
				nextRune = r[0]
			}
			if !isYearBoundaryRune(prevRune) || !isYearBoundaryRune(nextRune) {
				continue
			}
			y, _ := strconv.Atoi(base[start:end])
			if !isValidReleaseYear(y) {
				continue
			}
			prefix := strings.TrimSpace(base[:start])
			if cleanTitle(reQualityNoise.ReplaceAllString(prefix, "")) == "" {
				// Leading year is part of the title itself (e.g. "2001.A.Space.Odyssey", "1917").
				continue
			}
			year = y
			base = prefix
			break
		}
	}

	base = reQualityNoise.ReplaceAllString(base, "")
	base = reChineseEdition.ReplaceAllString(base, "")
	title := cleanTitle(base)
	primary, alt := splitBilingualTitle(title)
	return ParsedMovie{
		Title:         primary,
		OriginalTitle: alt,
		Year:          year,
		ProviderIDs:   providerIDs,
	}
}

// ParseMoviePath parses both the media filename and its immediate parent directory
// (when the file lives inside a dedicated movie folder under rootDir), cross-enriching
// title, original/alternate title, year, and provider IDs like MoviePilot.
func ParseMoviePath(rootDir, mediaPath string) ParsedMovie {
	fileParsed := ParseMovieName(mediaPath)

	dir := filepath.Clean(filepath.Dir(mediaPath))
	cleanRoot := filepath.Clean(strings.TrimSpace(rootDir))
	if dir == "" || dir == "." || dir == string(filepath.Separator) || (cleanRoot != "" && cleanRoot != "." && dir == cleanRoot) {
		return fileParsed
	}

	dirBase := strings.TrimSpace(filepath.Base(dir))
	if dirBase == "" || genericMovieDirs[strings.ToLower(dirBase)] {
		return fileParsed
	}

	dirParsed := ParseMovieName(dirBase)
	if dirParsed.Title == "" && dirParsed.Year == 0 && len(dirParsed.ProviderIDs) == 0 {
		return fileParsed
	}

	out := fileParsed
	if len(dirParsed.ProviderIDs) > 0 {
		if out.ProviderIDs == nil {
			out.ProviderIDs = make(map[string]string, len(dirParsed.ProviderIDs))
		}
		for k, v := range dirParsed.ProviderIDs {
			if _, exists := out.ProviderIDs[k]; !exists {
				out.ProviderIDs[k] = v
			}
		}
	}

	// Prefer explicit folder year when filename year is absent, or when folder year
	// is parenthesized and filename year looks suspiciously older than folder year.
	if out.Year == 0 && dirParsed.Year > 0 {
		out.Year = dirParsed.Year
	}

	// Cross-enrich Chinese folder title + English filename title (e.g.
	// "海上钢琴师 （1998）/The.Legend.of.1900.1998.1080p.strm" or
	// "银翼杀手2049 (2017)/Blade.Runner.2049.2017.2160p.strm"):
	// keep the clean CJK title as Title and the English filename title as OriginalTitle
	// so both display and TMDB fallback search work seamlessly.
	if dirParsed.Title != "" {
		if out.Title == "" {
			out.Title = dirParsed.Title
		} else if containsCJK(dirParsed.Title) && !containsCJK(out.Title) {
			if out.OriginalTitle == "" {
				out.OriginalTitle = out.Title
			}
			out.Title = dirParsed.Title
		} else if !strings.EqualFold(dirParsed.Title, out.Title) && out.OriginalTitle == "" {
			out.OriginalTitle = dirParsed.Title
		}
	}
	if out.OriginalTitle == "" && dirParsed.OriginalTitle != "" && !strings.EqualFold(dirParsed.OriginalTitle, out.Title) {
		out.OriginalTitle = dirParsed.OriginalTitle
	}
	if strings.EqualFold(out.OriginalTitle, out.Title) {
		out.OriginalTitle = ""
	}
	return out
}

// ParsedEpisode holds parsed episode metadata from a file path.
type ParsedEpisode struct {
	IsEpisode     bool
	SeriesName    string
	SeriesYear    int
	SeasonNumber  int
	EpisodeNumber int
	EpisodeTitle  string
	SeriesDir     string
	SeasonDir     string
}

// ParseEpisodePath detects whether mediaPath belongs to a TV series (e.g. Series/Season 01/S01E01...).
func ParseEpisodePath(rootDir, mediaPath string, forceTV bool) ParsedEpisode {
	base := strings.TrimSuffix(filepath.Base(mediaPath), filepath.Ext(mediaPath))
	var seasonNum, epNum int
	var matched, isSpecialsDir bool
	var epTitle string

	if m := reSeasonEpisode.FindStringSubmatchIndex(base); m != nil {
		seasonNum, _ = strconv.Atoi(base[m[2]:m[3]])
		epNum, _ = strconv.Atoi(base[m[4]:m[5]])
		matched = true
		rest := strings.TrimSpace(base[m[1]:])
		rest = reQualityNoise.ReplaceAllString(rest, "")
		epTitle = cleanTitle(rest)
	} else if m := reAltEpisode.FindStringSubmatchIndex(base); m != nil {
		seasonNum, _ = strconv.Atoi(base[m[2]:m[3]])
		epNum, _ = strconv.Atoi(base[m[4]:m[5]])
		matched = true
		rest := strings.TrimSpace(base[m[1]:])
		rest = reQualityNoise.ReplaceAllString(rest, "")
		epTitle = cleanTitle(rest)
	}

	if !matched && !forceTV {
		return ParsedEpisode{IsEpisode: false}
	}

	fileDir := filepath.Dir(mediaPath)
	seasonDir := ""
	seriesDir := fileDir

	// Check if immediate parent directory is a Season directory (e.g., "Season 01", "S01", "第1季", "Specials")
	parentBase := filepath.Base(fileDir)
	if m := reSeasonDir.FindStringSubmatch(parentBase); m != nil {
		dirNum, _ := strconv.Atoi(m[1])
		if !matched || seasonNum == 0 {
			seasonNum = dirNum
		}
		if dirNum == 0 {
			isSpecialsDir = true
		}
		seasonDir = fileDir
		seriesDir = filepath.Dir(fileDir)
		matched = true
	} else if strings.EqualFold(strings.TrimSpace(parentBase), "Specials") {
		if !matched || seasonNum == 0 {
			seasonNum = 0
		}
		isSpecialsDir = true
		seasonDir = fileDir
		seriesDir = filepath.Dir(fileDir)
		matched = true
	}

	if !matched {
		return ParsedEpisode{IsEpisode: false}
	}

	// Ensure seriesDir is inside rootDir (fallback to fileDir if equal to rootDir)
	cleanRoot := filepath.Clean(rootDir)
	if filepath.Clean(seriesDir) == cleanRoot {
		seriesDir = fileDir
	}

	parsedSeries := ParseMovieName(filepath.Base(seriesDir))
	seriesName := parsedSeries.Title
	if seriesName == "" {
		seriesName = filepath.Base(seriesDir)
	}
	if seasonNum == 0 && !isSpecialsDir {
		seasonNum = 1
	}
	if epTitle == "" && epNum > 0 {
		epTitle = "Episode " + strconv.Itoa(epNum)
	}

	return ParsedEpisode{
		IsEpisode:     true,
		SeriesName:    seriesName,
		SeriesYear:    parsedSeries.Year,
		SeasonNumber:  seasonNum,
		EpisodeNumber: epNum,
		EpisodeTitle:  epTitle,
		SeriesDir:     seriesDir,
		SeasonDir:     seasonDir,
	}
}

func cleanTitle(s string) string {
	s = strings.ReplaceAll(s, ".", " ")
	s = strings.ReplaceAll(s, "_", " ")
	s = strings.Trim(s, " -_[]()（）")
	fields := strings.Fields(s)
	return strings.Join(fields, " ")
}

// ParsedAudio holds parsed artist, album, disc/track numbers, title, year, and directory paths for an audio file.
type ParsedAudio struct {
	Artist      string
	Album       string
	TrackNumber int
	DiscNumber  int
	Title       string
	Year        int
	ArtistDir   string
	AlbumDir    string
}

// ParseAudioPath extracts audio metadata from a media path relative to a library root.
// Supports:
//   - root/Artist/Album (2023)/01 - Track Title.flac
//   - root/Artist/Album/Disc 2/01 - Track.mp3
//   - root/Artist/Album/1-02 Track.mp3
//   - root/Album/03. Song.m4a
//   - root/01 Song.mp3
func ParseAudioPath(root, mediaPath string) ParsedAudio {
	cleanRoot := filepath.Clean(root)
	cleanMedia := filepath.Clean(mediaPath)

	stem := strings.TrimSpace(strings.TrimSuffix(filepath.Base(cleanMedia), filepath.Ext(cleanMedia)))
	discNum := 0
	trackNum := 0
	title := stem

	if m := reAudioDiscTrack.FindStringSubmatch(stem); m != nil {
		discNum, _ = strconv.Atoi(m[1])
		trackNum, _ = strconv.Atoi(m[2])
		title = cleanAudioTitle(m[3])
	} else if m := reAudioTrack.FindStringSubmatch(stem); m != nil {
		trackNum, _ = strconv.Atoi(m[1])
		title = cleanAudioTitle(m[2])
	} else {
		title = cleanAudioTitle(stem)
	}
	if title == "" {
		title = stem
	}

	var dirs []string
	if rel, err := filepath.Rel(cleanRoot, cleanMedia); err == nil && rel != "" && !strings.HasPrefix(rel, "..") {
		relDir := filepath.Dir(rel)
		if relDir != "." && relDir != "" {
			for _, seg := range strings.Split(filepath.ToSlash(relDir), "/") {
				if seg != "" && seg != "." {
					dirs = append(dirs, seg)
				}
			}
		}
	}

	// Optional Disc/CD subdirectory (e.g. "Disc 1", "CD2")
	if len(dirs) > 0 {
		if m := reAudioDiscDir.FindStringSubmatch(strings.TrimSpace(dirs[len(dirs)-1])); m != nil {
			if discNum == 0 {
				discNum, _ = strconv.Atoi(m[1])
			}
			dirs = dirs[:len(dirs)-1]
		}
	}

	var artist, album, artistDir, albumDir string
	var year int

	switch {
	case len(dirs) >= 2:
		artist = strings.TrimSpace(dirs[len(dirs)-2])
		album, year = parseAlbumDirName(dirs[len(dirs)-1])
		artistDir = filepath.Join(append([]string{cleanRoot}, dirs[:len(dirs)-1]...)...)
		albumDir = filepath.Join(append([]string{cleanRoot}, dirs...)...)
	case len(dirs) == 1:
		album, year = parseAlbumDirName(dirs[0])
		albumDir = filepath.Join(cleanRoot, dirs[0])
	}

	if discNum == 0 {
		discNum = 1
	}

	return ParsedAudio{
		Artist:      artist,
		Album:       album,
		TrackNumber: trackNum,
		DiscNumber:  discNum,
		Title:       title,
		Year:        year,
		ArtistDir:   artistDir,
		AlbumDir:    albumDir,
	}
}

func parseAlbumDirName(dirName string) (string, int) {
	s := strings.TrimSpace(dirName)
	year := 0

	if m := reYearParens.FindStringSubmatchIndex(s); m != nil {
		y, _ := strconv.Atoi(s[m[2]:m[3]])
		year = y
		before := strings.TrimSpace(s[:m[0]])
		after := strings.TrimSpace(s[m[1]:])
		if before != "" {
			s = before
		} else {
			s = after
		}
	} else if m := reAlbumYearLead.FindStringSubmatch(s); m != nil {
		ys := m[1]
		if ys == "" {
			ys = m[2]
		}
		year, _ = strconv.Atoi(ys)
		s = strings.TrimSpace(m[3])
	}

	s = strings.Trim(s, " -_[]()（）")
	if s == "" {
		s = strings.TrimSpace(dirName)
	}
	return s, year
}

func cleanAudioTitle(s string) string {
	s = strings.ReplaceAll(s, "_", " ")
	s = strings.Trim(s, " -_")
	return strings.Join(strings.Fields(s), " ")
}

// SubtitleClassification holds normalized language, Chinese variant, and display title.
type SubtitleClassification struct {
	Language     string // ISO 639-2: chi / eng / jpn / kor / und
	SubVariant   string // chs / cht / bilingual / ""
	DisplayTitle string // e.g. "简体中文 (ASS)", "繁体中文 (PGS)", "简繁双语 (SRT)"
	Codec        string // subrip / ass / ssa / vtt / pgssub
	IsDefault    bool
	IsForced     bool
}

// ClassifySubtitle analyzes a subtitle filename (or embedded track title+language) to accurately
// distinguish Simplified Chinese (chs), Traditional Chinese (cht), and Bilingual tracks.
func ClassifySubtitle(filename, codec string) SubtitleClassification {
	base := filepath.Base(filename)
	lower := strings.ToLower(base)
	codecLower := strings.ToLower(codec)
	if codecLower == "" {
		if c, ok := SubtitleCodecForExt(base); ok {
			codecLower = c
		}
	}

	formatLabel := subtitleFormatLabel(codecLower)

	isDefault := containsToken(lower, "default")
	isForced := containsToken(lower, "forced")

	hasCHS := containsChineseMarker(lower, []string{"chs", "sc", "gb", "hans", "zh-cn", "zh_cn", "zh-hans", "zh_hans", "简体", "简中", "简日", "简英", "简"})
	hasCHT := containsChineseMarker(lower, []string{"cht", "tc", "big5", "hant", "zh-tw", "zh_tw", "zh-hk", "zh_hk", "zh-hant", "zh_hant", "繁体", "繁體", "繁中", "繁日", "繁英", "繁"})
	hasBilingual := strings.Contains(lower, "双语") || strings.Contains(lower, "雙語") || strings.Contains(lower, "bilingual") ||
		(hasCHS && hasCHT) ||
		strings.Contains(lower, "chs&eng") || strings.Contains(lower, "cht&eng") ||
		strings.Contains(lower, "chs_eng") || strings.Contains(lower, "cht_eng") ||
		strings.Contains(lower, "scjp") || strings.Contains(lower, "tcjp")

	res := SubtitleClassification{
		Codec:     codecLower,
		IsDefault: isDefault,
		IsForced:  isForced,
	}

	switch {
	case hasCHS && hasCHT:
		res.Language = "chi"
		res.SubVariant = "bilingual"
		res.DisplayTitle = "简繁双语 (" + formatLabel + ")"
	case hasBilingual && hasCHS:
		res.Language = "chi"
		res.SubVariant = "chs"
		res.DisplayTitle = "简体双语 (" + formatLabel + ")"
	case hasBilingual && hasCHT:
		res.Language = "chi"
		res.SubVariant = "cht"
		res.DisplayTitle = "繁体双语 (" + formatLabel + ")"
	case hasBilingual:
		res.Language = "chi"
		res.SubVariant = "bilingual"
		res.DisplayTitle = "双语中文 (" + formatLabel + ")"
	case hasCHS:
		res.Language = "chi"
		res.SubVariant = "chs"
		res.DisplayTitle = "简体中文 (" + formatLabel + ")"
	case hasCHT:
		res.Language = "chi"
		res.SubVariant = "cht"
		res.DisplayTitle = "繁体中文 (" + formatLabel + ")"
	case containsToken(lower, "chi") || containsToken(lower, "zho") || containsToken(lower, "zh") || strings.Contains(lower, "中文"):
		res.Language = "chi"
		res.DisplayTitle = "中文 (" + formatLabel + ")"
	case containsToken(lower, "eng") || containsToken(lower, "en") || strings.Contains(lower, "english"):
		res.Language = "eng"
		res.DisplayTitle = "English (" + formatLabel + ")"
	case containsToken(lower, "jpn") || containsToken(lower, "ja") || containsToken(lower, "jp") || strings.Contains(lower, "日文") || strings.Contains(lower, "日语"):
		res.Language = "jpn"
		res.DisplayTitle = "日本語 (" + formatLabel + ")"
	case containsToken(lower, "kor") || containsToken(lower, "ko") || strings.Contains(lower, "韩文") || strings.Contains(lower, "韩语"):
		res.Language = "kor"
		res.DisplayTitle = "한국어 (" + formatLabel + ")"
	default:
		res.Language = "und"
		res.DisplayTitle = "Subtitle (" + formatLabel + ")"
	}
	return res
}

func subtitleFormatLabel(codec string) string {
	switch strings.ToLower(codec) {
	case "ass":
		return "ASS"
	case "ssa":
		return "SSA"
	case "pgssub", "sup", "hdmv_pgs_subtitle":
		return "PGS"
	case "vtt", "webvtt":
		return "VTT"
	case "subrip", "srt":
		return "SRT"
	default:
		if codec == "" {
			return "SUB"
		}
		return strings.ToUpper(codec)
	}
}

func containsChineseMarker(lower string, markers []string) bool {
	for _, m := range markers {
		// Multi-byte Chinese characters can be matched anywhere
		if hasNonASCII(m) {
			if strings.Contains(lower, m) {
				return true
			}
			continue
		}
		if containsToken(lower, m) {
			return true
		}
	}
	return false
}

func hasNonASCII(s string) bool {
	for _, r := range s {
		if r > 127 {
			return true
		}
	}
	return false
}

// containsToken checks if tok appears as a delimited segment in s (split by ., _, -, space, [, ], (, ), &).
func containsToken(s, tok string) bool {
	if s == tok {
		return true
	}
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '.' || r == '_' || r == '-' || r == ' ' || r == '[' || r == ']' || r == '(' || r == ')' || r == '&'
	})
	for _, p := range parts {
		if p == tok {
			return true
		}
	}
	// Also match hyphenated locales like zh-cn / zh-hans directly
	if strings.Contains(tok, "-") || strings.Contains(tok, "_") {
		return strings.Contains(s, tok)
	}
	return false
}

// TokenizeForFTS builds Chinese unigram/bigram + word tokens for SQLite FTS5 indexing
// without any C/external tokenizer dependency.
func TokenizeForFTS(texts ...string) string {
	var tokens []string
	seen := make(map[string]struct{})
	add := func(t string) {
		t = strings.TrimSpace(strings.ToLower(t))
		if t == "" {
			return
		}
		if _, ok := seen[t]; ok {
			return
		}
		seen[t] = struct{}{}
		tokens = append(tokens, t)
	}

	for _, text := range texts {
		if text == "" {
			continue
		}
		runes := []rune(text)
		i := 0
		for i < len(runes) {
			r := runes[i]
			if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
				i++
				continue
			}
			if unicode.Is(unicode.Han, r) {
				j := i
				for j < len(runes) && unicode.Is(unicode.Han, runes[j]) {
					j++
				}
				cjk := runes[i:j]
				add(string(cjk))
				for k := 0; k < len(cjk); k++ {
					add(string(cjk[k]))
					if k+1 < len(cjk) {
						add(string(cjk[k : k+2]))
					}
				}
				i = j
			} else {
				j := i
				for j < len(runes) && !unicode.IsSpace(runes[j]) && !unicode.IsPunct(runes[j]) && !unicode.IsSymbol(runes[j]) && !unicode.Is(unicode.Han, runes[j]) {
					j++
				}
				add(string(runes[i:j]))
				i = j
			}
		}
	}
	return strings.Join(tokens, " ")
}
