package playback

import (
	"path/filepath"
	"strings"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

// TrackSelectionInput contains all signals needed to choose default audio & subtitle tracks.
type TrackSelectionInput struct {
	Streams           []dto.MediaStream
	UserConfig        *dto.UserConfiguration
	UserData          *sqlite.UserItemDataRecord
	ServerDefaultLang string
}

// TrackSelectionResult holds the computed default audio and subtitle stream indices.
type TrackSelectionResult struct {
	AudioStreamIndex    *int
	SubtitleStreamIndex *int
}

// LocaleToTrackLang maps a BCP-47 / UI / server locale (e.g. "zh-CN", "zh-TW", "en-US")
// to a normalized track language code ("chs", "cht", "eng", "jpn", "kor", etc.).
func LocaleToTrackLang(locale string) string {
	l := strings.ToLower(strings.TrimSpace(locale))
	if l == "" {
		return ""
	}
	l = strings.ReplaceAll(l, "_", "-")
	switch {
	case l == "chs" || l == "zh-cn" || l == "zh-sg" || l == "zh-hans" || strings.HasPrefix(l, "zh-hans-"):
		return "chs"
	case l == "cht" || l == "zh-tw" || l == "zh-hk" || l == "zh-mo" || l == "zh-hant" || strings.HasPrefix(l, "zh-hant-"):
		return "cht"
	case l == "zh" || l == "chi" || l == "zho":
		return "chs"
	case l == "en" || l == "eng" || strings.HasPrefix(l, "en-"):
		return "eng"
	case l == "ja" || l == "jpn" || strings.HasPrefix(l, "ja-"):
		return "jpn"
	case l == "ko" || l == "kor" || strings.HasPrefix(l, "ko-"):
		return "kor"
	case l == "es" || l == "spa" || strings.HasPrefix(l, "es-"):
		return "spa"
	case l == "de" || l == "ger" || l == "deu" || strings.HasPrefix(l, "de-"):
		return "ger"
	case l == "fr" || l == "fre" || l == "fra" || strings.HasPrefix(l, "fr-"):
		return "fre"
	case l == "pt" || l == "por" || strings.HasPrefix(l, "pt-"):
		return "por"
	case l == "th" || l == "tha" || strings.HasPrefix(l, "th-"):
		return "tha"
	default:
		if idx := strings.IndexByte(l, '-'); idx > 0 {
			return l[:idx]
		}
		return l
	}
}

// ApplyTrackSelection computes default audio & subtitle indices for ms using unified rules
// and writes them onto ms.DefaultAudioStreamIndex and ms.DefaultSubtitleStreamIndex.
func ApplyTrackSelection(ms *dto.MediaSourceInfo, cfg *dto.UserConfiguration, ud *sqlite.UserItemDataRecord, serverDefaultLang string) {
	if ms == nil {
		return
	}
	res := SelectDefaultTracks(TrackSelectionInput{
		Streams:           ms.MediaStreams,
		UserConfig:        cfg,
		UserData:          ud,
		ServerDefaultLang: serverDefaultLang,
	})
	ms.DefaultAudioStreamIndex = res.AudioStreamIndex
	ms.DefaultSubtitleStreamIndex = res.SubtitleStreamIndex
}

// SelectDefaultTracks computes the preferred audio and subtitle stream indices.
//
// Audio priority:
//  1. Per-item memory (UserData.AudioStreamIndex when RememberAudioSelections is enabled)
//  2. UserConfig.AudioLanguagePreference ("auto" resolves ServerDefaultLang; "media"/"" skips language matching)
//  3. First IsDefault audio stream
//  4. First audio stream
//
// Subtitle priority:
//  1. Disabled globally (SubtitleMode == "None" or SubtitleLanguagePreference == "off") -> nil
//  2. Per-item memory (UserData.SubtitleStreamIndex when RememberSubtitleSelections is enabled; -1 = explicitly off -> nil)
//  3. SubtitleMode rules (OnlyForced / Smart / Always / Default)
//  4. SubtitleLanguagePreference ("" or "auto" falls back to ServerDefaultLang; "media" skips language matching)
//     Within same language, prefers: IsExternal > IsDefault > chs/bilingual (or cht/bilingual) > text format
//  5. First IsDefault subtitle stream fallback
func SelectDefaultTracks(in TrackSelectionInput) TrackSelectionResult {
	var audioStreams []dto.MediaStream
	var subStreams []dto.MediaStream
	for _, st := range in.Streams {
		switch {
		case strings.EqualFold(st.Type, "Audio"):
			audioStreams = append(audioStreams, st)
		case strings.EqualFold(st.Type, "Subtitle"):
			subStreams = append(subStreams, st)
		}
	}

	audioIdx, selectedAudio := selectAudioTrack(audioStreams, in.UserConfig, in.UserData, in.ServerDefaultLang)
	subIdx := selectSubtitleTrack(subStreams, selectedAudio, in.UserConfig, in.UserData, in.ServerDefaultLang)

	return TrackSelectionResult{
		AudioStreamIndex:    audioIdx,
		SubtitleStreamIndex: subIdx,
	}
}

func selectAudioTrack(
	streams []dto.MediaStream,
	cfg *dto.UserConfiguration,
	ud *sqlite.UserItemDataRecord,
	serverDefaultLang string,
) (*int, *dto.MediaStream) {
	if len(streams) == 0 {
		return nil, nil
	}

	// 1. Per-item memory
	rememberAudio := cfg == nil || cfg.RememberAudioSelections
	if rememberAudio && ud != nil && ud.AudioStreamIndex != nil {
		for i := range streams {
			if streams[i].Index == *ud.AudioStreamIndex {
				idx := streams[i].Index
				return &idx, &streams[i]
			}
		}
	}

	// 2. AudioLanguagePreference
	if cfg != nil {
		rawPref := strings.ToLower(strings.TrimSpace(cfg.AudioLanguagePreference))
		if rawPref != "" && rawPref != "media" {
			var targetLang string
			if rawPref == "auto" {
				targetLang = LocaleToTrackLang(serverDefaultLang)
			} else {
				targetLang = LocaleToTrackLang(rawPref)
			}
			if targetLang != "" {
				if picked := pickBestAudioByLang(streams, targetLang); picked != nil {
					idx := picked.Index
					return &idx, picked
				}
			}
		}
	}

	// 3. First IsDefault audio stream
	for i := range streams {
		if streams[i].IsDefault {
			idx := streams[i].Index
			return &idx, &streams[i]
		}
	}

	// 4. First audio stream fallback
	idx := streams[0].Index
	return &idx, &streams[0]
}

func selectSubtitleTrack(
	streams []dto.MediaStream,
	selectedAudio *dto.MediaStream,
	cfg *dto.UserConfiguration,
	ud *sqlite.UserItemDataRecord,
	serverDefaultLang string,
) *int {
	if len(streams) == 0 {
		return nil
	}

	mode := "default"
	rawPref := ""
	if cfg != nil {
		if m := strings.ToLower(strings.TrimSpace(cfg.SubtitleMode)); m != "" {
			mode = m
		}
		rawPref = strings.ToLower(strings.TrimSpace(cfg.SubtitleLanguagePreference))
	}

	// 1. Globally disabled
	if mode == "none" || rawPref == "off" {
		return nil
	}

	// 2. Per-item memory (-1 explicitly disables subtitles for this item)
	rememberSub := cfg == nil || cfg.RememberSubtitleSelections
	if rememberSub && ud != nil && ud.SubtitleStreamIndex != nil {
		if *ud.SubtitleStreamIndex == -1 {
			return nil
		}
		for i := range streams {
			if streams[i].Index == *ud.SubtitleStreamIndex {
				idx := streams[i].Index
				return &idx
			}
		}
	}

	// Resolve target language
	followMediaDefault := rawPref == "media"
	targetLang := ""
	if !followMediaDefault {
		if rawPref == "" || rawPref == "auto" {
			targetLang = LocaleToTrackLang(serverDefaultLang)
		} else {
			targetLang = LocaleToTrackLang(rawPref)
		}
	}

	// 3. OnlyForced mode
	if mode == "onlyforced" {
		return pickForcedSubtitle(streams, targetLang)
	}

	// 4. Smart mode: if selected audio already matches target language, only pick forced subtitles
	if mode == "smart" && targetLang != "" && selectedAudio != nil && matchesTrackLanguage(*selectedAudio, targetLang) {
		return pickForcedSubtitle(streams, targetLang)
	}

	// 5. Language preference matching (unless explicitly following media default)
	if !followMediaDefault && targetLang != "" {
		if picked := pickBestSubtitleByLang(streams, targetLang); picked != nil {
			idx := picked.Index
			return &idx
		}
	}

	// 6. IsDefault fallback
	for i := range streams {
		if streams[i].IsDefault {
			idx := streams[i].Index
			return &idx
		}
	}

	// 7. Always mode fallback to first subtitle stream
	if mode == "always" {
		idx := streams[0].Index
		return &idx
	}

	return nil
}

func pickForcedSubtitle(streams []dto.MediaStream, targetLang string) *int {
	var forced []dto.MediaStream
	for _, st := range streams {
		if st.IsForced {
			forced = append(forced, st)
		}
	}
	if len(forced) == 0 {
		return nil
	}
	if targetLang != "" {
		if picked := pickBestSubtitleByLang(forced, targetLang); picked != nil {
			idx := picked.Index
			return &idx
		}
	}
	idx := forced[0].Index
	return &idx
}

func pickBestAudioByLang(streams []dto.MediaStream, targetLang string) *dto.MediaStream {
	var best *dto.MediaStream
	bestScore := -1
	for i := range streams {
		st := &streams[i]
		if !matchesTrackLanguage(*st, targetLang) {
			continue
		}
		score := 10
		if targetLang == "chs" || targetLang == "cht" {
			v := detectSubChineseVariant(*st)
			if v == targetLang {
				score += 20
			} else if v != "" && v != targetLang && v != "bilingual" {
				// Avoid picking opposite Chinese variant unless nothing else matches
				score -= 5
			}
		}
		if st.IsDefault {
			score += 5
		}
		if score > bestScore {
			bestScore = score
			best = st
		}
	}
	return best
}

// pickBestSubtitleByLang ranks matching subtitle streams by:
// IsExternal > IsDefault > Chinese variant preference (exact / bilingual) > text format.
func pickBestSubtitleByLang(streams []dto.MediaStream, targetLang string) *dto.MediaStream {
	var best *dto.MediaStream
	bestScore := -1
	for i := range streams {
		st := &streams[i]
		if !matchesTrackLanguage(*st, targetLang) {
			continue
		}
		variant := detectSubChineseVariant(*st)
		// When user specifically wants chs, deprioritize pure cht; when cht, deprioritize pure chs.
		score := 0
		if st.IsExternal {
			score += 1000
		}
		if st.IsDefault {
			score += 400
		}
		switch targetLang {
		case "chs":
			switch variant {
			case "chs":
				score += 200
			case "bilingual":
				score += 180
			case "":
				score += 100
			case "cht":
				score += 10
			}
		case "cht":
			switch variant {
			case "cht":
				score += 200
			case "bilingual":
				score += 180
			case "":
				score += 100
			case "chs":
				score += 10
			}
		default:
			if variant == "chs" || variant == "bilingual" {
				score += 50
			}
		}
		if isTextSubtitleCodec(st.Codec) {
			score += 50
		}
		if score > bestScore {
			bestScore = score
			best = st
		}
	}
	return best
}

func isTextSubtitleCodec(codec string) bool {
	switch normalizeSubCodec(codec) {
	case "subrip", "vtt", "ass", "ssa", "webvtt", "mov_text", "text":
		return true
	default:
		return false
	}
}

// detectSubChineseVariant returns "chs", "cht", "bilingual", or "".
func detectSubChineseVariant(st dto.MediaStream) string {
	sv := strings.ToLower(strings.TrimSpace(st.SubVariant))
	switch sv {
	case "chs", "sc", "zh-cn", "zh-hans":
		return "chs"
	case "cht", "tc", "zh-tw", "zh-hk", "zh-hant":
		return "cht"
	case "bilingual":
		return "bilingual"
	}

	basePath := strings.TrimSpace(st.Path)
	if basePath != "" {
		basePath = filepath.Base(strings.ReplaceAll(basePath, "\\", "/"))
	}
	combined := strings.ToLower(strings.Join([]string{
		st.Language, st.DisplayLanguage, st.Title, st.DisplayTitle, basePath,
	}, " "))

	hasBilingual := strings.Contains(combined, "双语") ||
		strings.Contains(combined, "雙語") ||
		strings.Contains(combined, "简英") ||
		strings.Contains(combined, "簡英") ||
		strings.Contains(combined, "中英") ||
		strings.Contains(combined, "繁英") ||
		strings.Contains(combined, "简繁") ||
		strings.Contains(combined, "簡繁") ||
		strings.Contains(combined, "bilingual")

	allowShortTokens := !isExplicitNonChineseLang(st.Language) && !hasExplicitNonChineseSubtitleMarker(combined)
	shortTokenText := stripNonChineseRegionPairs(combined)

	hasCHS := strings.Contains(combined, "简体") ||
		strings.Contains(combined, "簡體") ||
		strings.Contains(combined, "简中") ||
		strings.Contains(combined, "zh-cn") ||
		strings.Contains(combined, "zh-hans") ||
		strings.Contains(combined, "zh-sg") ||
		containsWordToken(combined, "chs") ||
		containsWordToken(combined, "hans") ||
		(allowShortTokens && (containsWordToken(shortTokenText, "sc") || containsWordToken(shortTokenText, "gb")))

	hasCHT := strings.Contains(combined, "繁体") ||
		strings.Contains(combined, "繁體") ||
		strings.Contains(combined, "繁中") ||
		strings.Contains(combined, "正体") ||
		strings.Contains(combined, "正體") ||
		strings.Contains(combined, "zh-tw") ||
		strings.Contains(combined, "zh-hk") ||
		strings.Contains(combined, "zh-hant") ||
		containsWordToken(combined, "cht") ||
		containsWordToken(combined, "big5") ||
		containsWordToken(combined, "hant") ||
		(allowShortTokens && containsWordToken(shortTokenText, "tc"))

	if hasBilingual {
		return "bilingual"
	}
	if hasCHS && !hasCHT {
		return "chs"
	}
	if hasCHT && !hasCHS {
		return "cht"
	}
	if hasCHS && hasCHT {
		return "bilingual"
	}
	return ""
}

func isExplicitNonChineseLang(lang string) bool {
	l := strings.ToLower(strings.TrimSpace(lang))
	if l == "" || l == "und" || l == "mul" || l == "unknown" || l == "zxx" {
		return false
	}
	switch LocaleToTrackLang(l) {
	case "", "chs", "cht", "chi", "zho", "zh", "cmn", "yue", "und", "mul", "unknown":
		return false
	default:
		return true
	}
}

func hasExplicitNonChineseSubtitleMarker(combined string) bool {
	if strings.Contains(combined, "中文") || strings.Contains(combined, "chinese") ||
		strings.Contains(combined, "国语") || strings.Contains(combined, "國語") ||
		strings.Contains(combined, "粤语") || strings.Contains(combined, "粵語") {
		return false
	}
	for _, kw := range []string{"en-gb", "en_gb", "en.gb", "english", "japanese", "korean", "french", "german", "spanish"} {
		if strings.Contains(combined, kw) {
			return true
		}
	}
	return false
}

var nonChineseRegionReplacer = strings.NewReplacer(
	"en-gb", " ",
	"en_gb", " ",
	"en.gb", " ",
	"cy-gb", " ",
	"cy_gb", " ",
	"gd-gb", " ",
	"gd_gb", " ",
	"kw-gb", " ",
	"kw_gb", " ",
)

func stripNonChineseRegionPairs(s string) string {
	return nonChineseRegionReplacer.Replace(s)
}

func matchesTrackLanguage(st dto.MediaStream, targetLang string) bool {
	pref := strings.ToLower(strings.TrimSpace(targetLang))
	if pref == "" {
		return false
	}

	lang := strings.ToLower(strings.TrimSpace(st.Language))
	dispLang := strings.ToLower(strings.TrimSpace(st.DisplayLanguage))
	title := strings.ToLower(strings.TrimSpace(st.Title))
	dispTitle := strings.ToLower(strings.TrimSpace(st.DisplayTitle))
	combined := strings.Join([]string{lang, dispLang, title, dispTitle}, " ")

	switch pref {
	case "chs", "cht", "chi", "zho", "zh":
		if lang == "chi" || lang == "zho" || lang == "zh" || lang == "cmn" || lang == "yue" ||
			lang == "chs" || lang == "cht" || strings.HasPrefix(lang, "zh-") {
			return true
		}
		if detectSubChineseVariant(st) != "" {
			return true
		}
		for _, kw := range []string{"中文", "国语", "國語", "粤语", "粵語", "汉语", "漢語", "华语", "華語", "简体", "繁体", "繁體", "双语", "雙語", "chinese", "mandarin", "cantonese"} {
			if strings.Contains(combined, kw) {
				return true
			}
		}
		return containsWordToken(combined, "chi") || containsWordToken(combined, "zho") || containsWordToken(combined, "zh") || containsWordToken(combined, "cmn") || containsWordToken(combined, "yue")
	case "eng", "en":
		if lang == "eng" || lang == "en" || strings.HasPrefix(lang, "en-") {
			return true
		}
		return strings.Contains(combined, "english") || strings.Contains(combined, "英语") || strings.Contains(combined, "英語") || strings.Contains(combined, "英文") || containsWordToken(combined, "eng")
	case "jpn", "ja":
		if lang == "jpn" || lang == "ja" || strings.HasPrefix(lang, "ja-") {
			return true
		}
		return strings.Contains(combined, "japanese") || strings.Contains(combined, "日语") || strings.Contains(combined, "日語") || strings.Contains(combined, "日文") || strings.Contains(combined, "日本語") || containsWordToken(combined, "jpn")
	case "kor", "ko":
		if lang == "kor" || lang == "ko" || strings.HasPrefix(lang, "ko-") {
			return true
		}
		return strings.Contains(combined, "korean") || strings.Contains(combined, "韩语") || strings.Contains(combined, "韓語") || strings.Contains(combined, "韩文") || strings.Contains(combined, "韓文") || strings.Contains(combined, "한국어") || containsWordToken(combined, "kor")
	case "spa", "es":
		return lang == "spa" || lang == "es" || strings.HasPrefix(lang, "es-") || strings.Contains(combined, "spanish") || strings.Contains(combined, "español") || strings.Contains(combined, "西班牙")
	case "ger", "deu", "de":
		return lang == "ger" || lang == "deu" || lang == "de" || strings.HasPrefix(lang, "de-") || strings.Contains(combined, "german") || strings.Contains(combined, "deutsch") || strings.Contains(combined, "德语") || strings.Contains(combined, "德文")
	case "fre", "fra", "fr":
		return lang == "fre" || lang == "fra" || lang == "fr" || strings.HasPrefix(lang, "fr-") || strings.Contains(combined, "french") || strings.Contains(combined, "français") || strings.Contains(combined, "法语") || strings.Contains(combined, "法文")
	case "por", "pt":
		return lang == "por" || lang == "pt" || strings.HasPrefix(lang, "pt-") || strings.Contains(combined, "portuguese") || strings.Contains(combined, "português") || strings.Contains(combined, "葡萄牙")
	case "tha", "th":
		return lang == "tha" || lang == "th" || strings.HasPrefix(lang, "th-") || strings.Contains(combined, "thai") || strings.Contains(combined, "泰语") || strings.Contains(combined, "泰文")
	default:
		return lang == pref || strings.HasPrefix(lang, pref+"-") || containsWordToken(combined, pref)
	}
}

func containsWordToken(s, word string) bool {
	if s == "" || word == "" {
		return false
	}
	start := 0
	for {
		idx := strings.Index(s[start:], word)
		if idx < 0 {
			return false
		}
		abs := start + idx
		beforeOK := abs == 0 || !isASCIIAlphaNum(s[abs-1])
		afterPos := abs + len(word)
		afterOK := afterPos == len(s) || !isASCIIAlphaNum(s[afterPos])
		if beforeOK && afterOK {
			return true
		}
		start = abs + 1
	}
}

func isASCIIAlphaNum(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}
