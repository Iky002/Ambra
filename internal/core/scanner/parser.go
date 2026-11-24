package scanner

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
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

	reSeasonEpisode = regexp.MustCompile(`(?i)(?:^|[\s._\-\[\(])S(\d{1,2})[\s._\-]*E(\d{1,3})(?:[\s._\-\]\)]|$)`)
	reAltEpisode    = regexp.MustCompile(`(?i)(?:^|[\s._\-])(\d{1,2})x(\d{1,3})(?:[\s._\-]|$)`)
	reSeasonDir     = regexp.MustCompile(`(?i)^(?:season|s|第)\s*0*(\d{1,2})\s*(?:季)?$`)
	reYearParens    = regexp.MustCompile(`[\(\[（]((?:19|20)\d{2})[\)\]）]`)
	reYearBare      = regexp.MustCompile(`(?:^|[\s._\-])((?:19|20)\d{2})(?:$|[\s._\-])`)
	reQualityNoise  = regexp.MustCompile(`(?i)\b(2160p|1080p|720p|480p|4k|uhd|hdr|hdr10|dv|bluray|blu-ray|bdrip|web-dl|webdl|webrip|remux|x264|x265|h264|h265|hevc|avc|aac|dts|ac3|atmos|truehd|flac|10bit)\b.*$`)
)

// IsVideoFile reports whether the file extension is a supported video format.
func IsVideoFile(path string) bool {
	return videoExtensions[strings.ToLower(filepath.Ext(path))]
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

// ParsedMovie holds parsed movie title and year from a filename or folder name.
type ParsedMovie struct {
	Title string
	Year  int
}

// ParseMovieName extracts a clean title and optional production year from a file or directory name.
func ParseMovieName(filename string) ParsedMovie {
	base := strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))
	year := 0

	if m := reYearParens.FindStringSubmatchIndex(base); m != nil {
		y, _ := strconv.Atoi(base[m[2]:m[3]])
		year = y
		base = base[:m[0]]
	} else if m := reYearBare.FindStringSubmatchIndex(base); m != nil {
		y, _ := strconv.Atoi(base[m[2]:m[3]])
		year = y
		base = base[:m[0]]
	}

	base = reQualityNoise.ReplaceAllString(base, "")
	title := cleanTitle(base)
	return ParsedMovie{Title: title, Year: year}
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
