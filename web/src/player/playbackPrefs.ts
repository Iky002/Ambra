import type { MediaStream } from '../api/types'
import { detectChineseVariant } from './subtitleUtils'

const PLAYBACK_PREFS_KEY = 'ambra_web_playback_prefs_v1'

export interface PlaybackPrefs {
  autoPlayNextEpisode: boolean
  preferredAudioLang: string
  preferredSubtitleLang: string
}

export const DEFAULT_PLAYBACK_PREFS: PlaybackPrefs = {
  autoPlayNextEpisode: true,
  preferredAudioLang: '',
  preferredSubtitleLang: '',
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
          ? parsed.preferredAudioLang
          : DEFAULT_PLAYBACK_PREFS.preferredAudioLang,
      preferredSubtitleLang:
        typeof parsed.preferredSubtitleLang === 'string'
          ? parsed.preferredSubtitleLang
          : DEFAULT_PLAYBACK_PREFS.preferredSubtitleLang,
    }
  } catch {
    return { ...DEFAULT_PLAYBACK_PREFS }
  }
}

export function savePlaybackPrefs(patch: Partial<PlaybackPrefs>): PlaybackPrefs {
  const next: PlaybackPrefs = {
    ...loadPlaybackPrefs(),
    ...patch,
  }
  try {
    localStorage.setItem(PLAYBACK_PREFS_KEY, JSON.stringify(next))
  } catch {
    // 忽略存储异常
  }
  return next
}

function matchesLanguage(stream: MediaStream, langCode: string): boolean {
  const pref = langCode.trim().toLowerCase()
  if (!pref) return false

  const lang = (stream.Language || '').trim().toLowerCase()
  const dispLang = (stream.DisplayLanguage || '').trim().toLowerCase()
  const title = (stream.Title || '').trim().toLowerCase()
  const dispTitle = (stream.DisplayTitle || '').trim().toLowerCase()
  const combined = `${lang} ${dispLang} ${title} ${dispTitle}`

  if (pref === 'chi' || pref === 'zho' || pref === 'zh') {
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
    return /\b(chi|zho|zh|cmn|yue)\b|中文|国语|國語|粤语|粵語|汉语|漢語|华语|華語|简体|繁体|繁體|双语|雙語/.test(
      combined,
    )
  }

  if (pref === 'eng' || pref === 'en') {
    if (lang === 'eng' || lang === 'en' || lang.startsWith('en-')) return true
    return /\b(eng|english)\b|英语|英語|英文/.test(combined)
  }

  if (pref === 'jpn' || pref === 'ja') {
    if (lang === 'jpn' || lang === 'ja' || lang.startsWith('ja-')) return true
    return /\b(jpn|japanese)\b|日语|日語|日文/.test(combined)
  }

  if (pref === 'kor' || pref === 'ko') {
    if (lang === 'kor' || lang === 'ko' || lang.startsWith('ko-')) return true
    return /\b(kor|korean)\b|韩语|韓語|韩文|韓文/.test(combined)
  }

  return (
    lang === pref ||
    lang.startsWith(`${pref}-`) ||
    dispLang.includes(pref) ||
    title.includes(pref) ||
    dispTitle.includes(pref)
  )
}

export function pickPreferredAudioIndex(
  streams: MediaStream[],
  preferredLang: string,
  serverDefault?: number,
): number | undefined {
  const pref = preferredLang.trim().toLowerCase()
  if (!pref || streams.length === 0) return serverDefault

  if (pref === 'chs') {
    const exact = streams.find((s) => detectChineseVariant(s) === '简体')
    if (exact) return exact.Index
    const fallbackZh = streams.find(
      (s) => matchesLanguage(s, 'chi') && detectChineseVariant(s) !== '繁体',
    )
    if (fallbackZh) return fallbackZh.Index
    return serverDefault
  }

  if (pref === 'cht') {
    const exact = streams.find((s) => detectChineseVariant(s) === '繁体')
    if (exact) return exact.Index
    const fallbackZh = streams.find(
      (s) => matchesLanguage(s, 'chi') && detectChineseVariant(s) !== '简体',
    )
    if (fallbackZh) return fallbackZh.Index
    return serverDefault
  }

  const matched = streams.find((s) => matchesLanguage(s, pref))
  return matched ? matched.Index : serverDefault
}

export function pickPreferredSubtitleIndex(
  streams: MediaStream[],
  preferredLang: string,
  serverDefault?: number,
): number | undefined {
  const pref = preferredLang.trim().toLowerCase()
  if (pref === 'off') return undefined
  if (!pref || streams.length === 0) return serverDefault

  if (pref === 'chs') {
    const exact = streams.find((s) => detectChineseVariant(s) === '简体')
    if (exact) return exact.Index
    const bilingual = streams.find((s) => {
      const v = detectChineseVariant(s)
      return v === '双语' || v === '简繁'
    })
    if (bilingual) return bilingual.Index
    const genericZh = streams.find(
      (s) => matchesLanguage(s, 'chi') && detectChineseVariant(s) !== '繁体',
    )
    if (genericZh) return genericZh.Index
    return serverDefault
  }

  if (pref === 'cht') {
    const exact = streams.find((s) => detectChineseVariant(s) === '繁体')
    if (exact) return exact.Index
    const bilingual = streams.find((s) => {
      const v = detectChineseVariant(s)
      return v === '双语' || v === '简繁'
    })
    if (bilingual) return bilingual.Index
    const genericZh = streams.find(
      (s) => matchesLanguage(s, 'chi') && detectChineseVariant(s) !== '简体',
    )
    if (genericZh) return genericZh.Index
    return serverDefault
  }

  const matched = streams.find((s) => matchesLanguage(s, pref))
  return matched ? matched.Index : serverDefault
}
