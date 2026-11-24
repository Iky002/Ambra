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
