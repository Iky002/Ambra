import type { MediaStream, UserConfiguration } from '../api/types'
import { detectChineseVariant } from './subtitleUtils'

const PLAYBACK_PREFS_KEY = 'ambra_web_playback_prefs_v1'

export type SubtitleModeOption = 'Default' | 'Smart' | 'OnlyForced' | 'Always' | 'None'

export interface PlaybackPrefs {
  autoPlayNextEpisode: boolean
  preferredAudioLang: string
  preferredSubtitleLang: string
  subtitleMode: SubtitleModeOption
}

export const DEFAULT_PLAYBACK_PREFS: PlaybackPrefs = {
  autoPlayNextEpisode: true,
  preferredAudioLang: 'media',
  preferredSubtitleLang: 'auto',
  subtitleMode: 'Default',
}

function normalizeAudioPref(v?: string): string {
  const s = (v || '').trim().toLowerCase()
  if (!s) return 'media'
  return s
}

function normalizeSubtitlePref(v?: string): string {
  const s = (v || '').trim().toLowerCase()
  if (!s) return 'auto'
  return s
}

function normalizeSubtitleMode(v?: string): SubtitleModeOption {
  const s = (v || '').trim().toLowerCase()
  switch (s) {
    case 'smart':
      return 'Smart'
    case 'onlyforced':
      return 'OnlyForced'
    case 'always':
      return 'Always'
    case 'none':
      return 'None'
    default:
      return 'Default'
  }
}

export function localeToTrackLang(locale?: string): string {
  const l = (locale || '').trim().toLowerCase().replace(/_/g, '-')
  if (!l) return ''
  if (l === 'chs' || l === 'zh-cn' || l === 'zh-sg' || l === 'zh-hans' || l.startsWith('zh-hans-')) {
    return 'chs'
  }
  if (
    l === 'cht' ||
    l === 'zh-tw' ||
    l === 'zh-hk' ||
    l === 'zh-mo' ||
    l === 'zh-hant' ||
    l.startsWith('zh-hant-')
  ) {
    return 'cht'
  }
  if (l === 'zh' || l === 'chi' || l === 'zho') return 'chs'
  if (l === 'en' || l === 'eng' || l.startsWith('en-')) return 'eng'
  if (l === 'ja' || l === 'jpn' || l.startsWith('ja-')) return 'jpn'
  if (l === 'ko' || l === 'kor' || l.startsWith('ko-')) return 'kor'
  if (l === 'es' || l === 'spa' || l.startsWith('es-')) return 'spa'
  if (l === 'de' || l === 'ger' || l === 'deu' || l.startsWith('de-')) return 'ger'
  if (l === 'fr' || l === 'fre' || l === 'fra' || l.startsWith('fr-')) return 'fre'
  if (l === 'pt' || l === 'por' || l.startsWith('pt-')) return 'por'
  if (l === 'th' || l === 'tha' || l.startsWith('th-')) return 'tha'
  const dash = l.indexOf('-')
  return dash > 0 ? l.slice(0, dash) : l
}

export function loadPlaybackPrefs(): PlaybackPrefs {
  try {
    const raw = localStorage.getItem(PLAYBACK_PREFS_KEY)
    if (!raw) return { ...DEFAULT_PLAYBACK_PREFS }
    const parsed = JSON.parse(raw) as Partial<PlaybackPrefs>
    return {
      autoPlayNextEpisode:
        typeof parsed.autoPlayNextEpisode === 'boolean'
          ? parsed.autoPlayNextEpisode
          : DEFAULT_PLAYBACK_PREFS.autoPlayNextEpisode,
      preferredAudioLang:
        typeof parsed.preferredAudioLang === 'string'
          ? normalizeAudioPref(parsed.preferredAudioLang)
          : DEFAULT_PLAYBACK_PREFS.preferredAudioLang,
      preferredSubtitleLang:
        typeof parsed.preferredSubtitleLang === 'string'
          ? normalizeSubtitlePref(parsed.preferredSubtitleLang)
          : DEFAULT_PLAYBACK_PREFS.preferredSubtitleLang,
      subtitleMode:
        typeof parsed.subtitleMode === 'string'
          ? normalizeSubtitleMode(parsed.subtitleMode)
          : DEFAULT_PLAYBACK_PREFS.subtitleMode,
    }
  } catch {
    return { ...DEFAULT_PLAYBACK_PREFS }
  }
}

export function savePlaybackPrefs(patch: Partial<PlaybackPrefs>): PlaybackPrefs {
  const current = loadPlaybackPrefs()
  const next: PlaybackPrefs = {
    autoPlayNextEpisode:
      typeof patch.autoPlayNextEpisode === 'boolean'
        ? patch.autoPlayNextEpisode
        : current.autoPlayNextEpisode,
    preferredAudioLang:
      typeof patch.preferredAudioLang === 'string'
        ? normalizeAudioPref(patch.preferredAudioLang)
        : current.preferredAudioLang,
    preferredSubtitleLang:
      typeof patch.preferredSubtitleLang === 'string'
        ? normalizeSubtitlePref(patch.preferredSubtitleLang)
        : current.preferredSubtitleLang,
    subtitleMode:
      typeof patch.subtitleMode === 'string'
        ? normalizeSubtitleMode(patch.subtitleMode)
        : current.subtitleMode,
  }
  try {
    localStorage.setItem(PLAYBACK_PREFS_KEY, JSON.stringify(next))
  } catch {
    // 忽略存储异常
  }
  return next
}

export function syncPlaybackPrefsFromUserConfig(cfg?: UserConfiguration | null): PlaybackPrefs {
  if (!cfg) return loadPlaybackPrefs()
  const patch: Partial<PlaybackPrefs> = {}
  if (typeof cfg.EnableNextEpisodeAutoPlay === 'boolean') {
    patch.autoPlayNextEpisode = cfg.EnableNextEpisodeAutoPlay
  }
  if (cfg.AudioLanguagePreference !== undefined) {
    patch.preferredAudioLang = normalizeAudioPref(cfg.AudioLanguagePreference)
  }
  if (cfg.SubtitleLanguagePreference !== undefined) {
    patch.preferredSubtitleLang = normalizeSubtitlePref(cfg.SubtitleLanguagePreference)
  }
  if (cfg.SubtitleMode !== undefined) {
    patch.subtitleMode = normalizeSubtitleMode(cfg.SubtitleMode)
  }
  return savePlaybackPrefs(patch)
}

export function playbackPrefsToUserConfig(
  prefs: PlaybackPrefs,
  existing?: UserConfiguration | null,
): UserConfiguration {
  return {
    PlayDefaultAudioTrack: prefs.preferredAudioLang === 'media',
    DisplayMissingEpisodes: existing?.DisplayMissingEpisodes ?? false,
    EnableLocalPassword: existing?.EnableLocalPassword ?? true,
    OrderedViews: existing?.OrderedViews ?? [],
    LatestItemsExcludes: existing?.LatestItemsExcludes ?? [],
    MyMediaExcludes: existing?.MyMediaExcludes ?? [],
    HidePlayedInLatest: existing?.HidePlayedInLatest ?? false,
    RememberAudioSelections: existing?.RememberAudioSelections ?? true,
    RememberSubtitleSelections: existing?.RememberSubtitleSelections ?? true,
    ...(existing || {}),
    AudioLanguagePreference: prefs.preferredAudioLang === 'media' ? '' : prefs.preferredAudioLang,
    SubtitleLanguagePreference: prefs.preferredSubtitleLang,
    SubtitleMode: prefs.subtitleMode,
    EnableNextEpisodeAutoPlay: prefs.autoPlayNextEpisode,
  }
}

function matchesLanguage(stream: MediaStream, langCode: string): boolean {
  const pref = langCode.trim().toLowerCase()
  if (!pref) return false

  const lang = (stream.Language || '').trim().toLowerCase()
  const dispLang = (stream.DisplayLanguage || '').trim().toLowerCase()
  const title = (stream.Title || '').trim().toLowerCase()
  const dispTitle = (stream.DisplayTitle || '').trim().toLowerCase()
  const combined = `${lang} ${dispLang} ${title} ${dispTitle}`

  if (pref === 'chi' || pref === 'zho' || pref === 'zh' || pref === 'chs' || pref === 'cht') {
    if (
      lang === 'chi' ||
      lang === 'zho' ||
      lang === 'zh' ||
      lang === 'cmn' ||
      lang === 'yue' ||
      lang === 'chs' ||
      lang === 'cht' ||
      lang.startsWith('zh-')
    ) {
      return true
    }
    if (detectChineseVariant(stream)) return true
    return /\b(chi|zho|zh|cmn|yue|chinese|mandarin|cantonese)\b|中文|国语|國語|粤语|粵語|汉语|漢語|华语|華語|简体|繁体|繁體|双语|雙語/.test(
      combined,
    )
  }

  if (pref === 'eng' || pref === 'en') {
    if (lang === 'eng' || lang === 'en' || lang.startsWith('en-')) return true
    return /\b(eng|english)\b|英语|英語|英文/.test(combined)
  }

  if (pref === 'jpn' || pref === 'ja') {
    if (lang === 'jpn' || lang === 'ja' || lang.startsWith('ja-')) return true
    return /\b(jpn|japanese)\b|日语|日語|日文|日本語/.test(combined)
  }

  if (pref === 'kor' || pref === 'ko') {
    if (lang === 'kor' || lang === 'ko' || lang.startsWith('ko-')) return true
    return /\b(kor|korean)\b|韩语|韓語|韩文|韓文|한국어/.test(combined)
  }

  if (pref === 'spa' || pref === 'es') {
    if (lang === 'spa' || lang === 'es' || lang.startsWith('es-')) return true
    return /\b(spa|spanish|español)\b|西班牙/.test(combined)
  }

  if (pref === 'ger' || pref === 'deu' || pref === 'de') {
    if (lang === 'ger' || lang === 'deu' || lang === 'de' || lang.startsWith('de-')) return true
    return /\b(ger|deu|german|deutsch)\b|德语|德文/.test(combined)
  }

  if (pref === 'fre' || pref === 'fra' || pref === 'fr') {
    if (lang === 'fre' || lang === 'fra' || lang === 'fr' || lang.startsWith('fr-')) return true
    return /\b(fre|fra|french|français)\b|法语|法文/.test(combined)
  }

  if (pref === 'por' || pref === 'pt') {
    if (lang === 'por' || lang === 'pt' || lang.startsWith('pt-')) return true
    return /\b(por|portuguese|português)\b|葡萄牙/.test(combined)
  }

  if (pref === 'tha' || pref === 'th') {
    if (lang === 'tha' || lang === 'th' || lang.startsWith('th-')) return true
    return /\b(tha|thai)\b|泰语|泰文/.test(combined)
  }

  return (
    lang === pref ||
    lang.startsWith(`${pref}-`) ||
    dispLang.includes(pref) ||
    title.includes(pref) ||
    dispTitle.includes(pref)
  )
}

function isTextSubtitle(stream: MediaStream): boolean {
  const codec = (stream.Codec || '').trim().toLowerCase()
  return (
    codec === 'srt' ||
    codec === 'subrip' ||
    codec === 'vtt' ||
    codec === 'webvtt' ||
    codec === 'ass' ||
    codec === 'ssa' ||
    codec === 'mov_text' ||
    codec === 'text'
  )
}

function scoreSubtitleForLang(stream: MediaStream, targetLang: string): number {
  let score = 0
  if (stream.IsExternal) score += 1000
  if (stream.IsDefault) score += 400
  const variant = detectChineseVariant(stream)
  if (targetLang === 'chs') {
    if (variant === '简体') score += 200
    else if (variant === '双语' || variant === '简繁') score += 180
    else if (!variant) score += 100
    else if (variant === '繁体') score += 10
  } else if (targetLang === 'cht') {
    if (variant === '繁体') score += 200
    else if (variant === '双语' || variant === '简繁') score += 180
    else if (!variant) score += 100
    else if (variant === '简体') score += 10
  } else if (variant === '简体' || variant === '双语' || variant === '简繁') {
    score += 50
  }
  if (isTextSubtitle(stream)) score += 50
  return score
}

function pickBestSubtitleForLang(
  streams: MediaStream[],
  targetLang: string,
): MediaStream | undefined {
  let best: MediaStream | undefined
  let bestScore = -1
  for (const s of streams) {
    if (!matchesLanguage(s, targetLang)) continue
    const score = scoreSubtitleForLang(s, targetLang)
    if (score > bestScore) {
      bestScore = score
      best = s
    }
  }
  return best
}

export function pickPreferredAudioIndex(
  streams: MediaStream[],
  preferredLang: string,
  serverDefault?: number,
  uiLocale?: string,
): number | undefined {
  if (streams.length === 0) return serverDefault

  const mediaDefault =
    serverDefault ?? streams.find((s) => s.IsDefault)?.Index ?? streams[0]?.Index

  const rawPref = preferredLang.trim().toLowerCase()
  if (!rawPref || rawPref === 'media') return mediaDefault

  const pref = rawPref === 'auto' ? localeToTrackLang(uiLocale) : rawPref
  if (!pref) return mediaDefault

  if (pref === 'chs') {
    const exact = streams.find((s) => detectChineseVariant(s) === '简体')
    if (exact) return exact.Index
    const fallbackZh = streams.find(
      (s) => matchesLanguage(s, 'chi') && detectChineseVariant(s) !== '繁体',
    )
    if (fallbackZh) return fallbackZh.Index
    return mediaDefault
  }

  if (pref === 'cht') {
    const exact = streams.find((s) => detectChineseVariant(s) === '繁体')
    if (exact) return exact.Index
    const fallbackZh = streams.find(
      (s) => matchesLanguage(s, 'chi') && detectChineseVariant(s) !== '简体',
    )
    if (fallbackZh) return fallbackZh.Index
    return mediaDefault
  }

  const matching = streams.filter((s) => matchesLanguage(s, pref))
  if (matching.length > 0) {
    const defMatch = matching.find((s) => s.IsDefault)
    return (defMatch ?? matching[0]).Index
  }
  return mediaDefault
}

export function pickPreferredSubtitleIndex(
  streams: MediaStream[],
  preferredLang: string,
  serverDefault?: number,
  uiLocale?: string,
  subtitleMode?: string,
): number | undefined {
  const mode = (subtitleMode || 'Default').trim().toLowerCase()
  if (mode === 'none') return undefined

  const rawPref = preferredLang.trim().toLowerCase()
  if (rawPref === 'off') return undefined
  if (streams.length === 0) return serverDefault

  const mediaDefault = serverDefault ?? streams.find((s) => s.IsDefault)?.Index

  if (mode === 'onlyforced') {
    const forced = streams.filter((s) => s.IsForced)
    if (forced.length === 0) return undefined
    const targetLang =
      rawPref === 'media'
        ? ''
        : !rawPref || rawPref === 'auto'
          ? localeToTrackLang(uiLocale)
          : localeToTrackLang(rawPref)
    if (targetLang) {
      const matchedForced = pickBestSubtitleForLang(forced, targetLang)
      if (matchedForced) return matchedForced.Index
    }
    return forced[0].Index
  }

  if (rawPref === 'media') {
    if (mediaDefault !== undefined) return mediaDefault
    return mode === 'always' ? streams[0]?.Index : undefined
  }

  const pref = !rawPref || rawPref === 'auto' ? localeToTrackLang(uiLocale) : localeToTrackLang(rawPref)
  if (!pref) {
    if (mediaDefault !== undefined) return mediaDefault
    return mode === 'always' ? streams[0]?.Index : undefined
  }

  const best = pickBestSubtitleForLang(streams, pref)
  if (best) return best.Index

  if (mediaDefault !== undefined) return mediaDefault
  return mode === 'always' ? streams[0]?.Index : undefined
}
