import { beforeEach, describe, expect, it } from 'vitest'
import type { MediaStream } from '../../api/types'
import {
  DEFAULT_PLAYBACK_PREFS,
  loadPlaybackPrefs,
  localeToTrackLang,
  pickPreferredAudioIndex,
  pickPreferredSubtitleIndex,
  playbackPrefsToUserConfig,
  savePlaybackPrefs,
  syncPlaybackPrefsFromUserConfig,
} from '../playbackPrefs'

describe('playbackPrefs', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('uses media default for audio and auto UI language for subtitles by default', () => {
    expect(DEFAULT_PLAYBACK_PREFS.preferredAudioLang).toBe('media')
    expect(DEFAULT_PLAYBACK_PREFS.preferredSubtitleLang).toBe('auto')
    expect(DEFAULT_PLAYBACK_PREFS.subtitleMode).toBe('Default')
    expect(loadPlaybackPrefs()).toEqual(DEFAULT_PLAYBACK_PREFS)
  })

  it('maps UI locales to canonical track language codes', () => {
    expect(localeToTrackLang('zh-CN')).toBe('chs')
    expect(localeToTrackLang('zh-TW')).toBe('cht')
    expect(localeToTrackLang('en-US')).toBe('eng')
    expect(localeToTrackLang('ja-JP')).toBe('jpn')
    expect(localeToTrackLang('ko-KR')).toBe('kor')
    expect(localeToTrackLang('de-DE')).toBe('ger')
  })

  it('selects IsDefault audio/subtitle in media mode even when serverDefault is undefined', () => {
    const audioStreams: MediaStream[] = [
      { Type: 'Audio', Index: 1, Language: 'eng', Codec: 'aac', IsDefault: false },
      { Type: 'Audio', Index: 2, Language: 'jpn', Codec: 'aac', IsDefault: true },
    ]
    const subStreams: MediaStream[] = [
      { Type: 'Subtitle', Index: 3, Language: 'eng', Codec: 'srt', IsDefault: false },
      { Type: 'Subtitle', Index: 4, Language: 'chi', Codec: 'ass', IsDefault: true },
    ]

    expect(pickPreferredAudioIndex(audioStreams, 'media', undefined, 'zh-CN')).toBe(2)
    expect(pickPreferredSubtitleIndex(subStreams, 'media', undefined, 'zh-CN')).toBe(4)

    // Falls back to first audio stream when none is IsDefault
    const noDefaultAudio: MediaStream[] = [
      { Type: 'Audio', Index: 5, Language: 'eng', Codec: 'aac', IsDefault: false },
      { Type: 'Audio', Index: 6, Language: 'chi', Codec: 'aac', IsDefault: false },
    ]
    expect(pickPreferredAudioIndex(noDefaultAudio, 'media', undefined, 'zh-CN')).toBe(5)
  })

  it('follows UI locale in auto mode and ranks external > IsDefault > chs/bilingual > text', () => {
    const subs: MediaStream[] = [
      { Type: 'Subtitle', Index: 2, Language: 'eng', Codec: 'srt', IsDefault: true },
      { Type: 'Subtitle', Index: 3, Language: 'chi', Codec: 'pgssub', Title: '简体中文', SubVariant: 'chs' },
      { Type: 'Subtitle', Index: 4, Language: 'chi', Codec: 'ass', Title: '简英双语', SubVariant: 'bilingual' },
      { Type: 'Subtitle', Index: 5, Language: 'chi', Codec: 'srt', Title: '简体中文', SubVariant: 'chs', IsExternal: true },
      { Type: 'Subtitle', Index: 6, Language: 'chi', Codec: 'srt', Title: '繁体中文', SubVariant: 'cht' },
    ]

    expect(pickPreferredSubtitleIndex(subs, 'auto', undefined, 'zh-CN')).toBe(5)
    // Without external, bilingual/chs text beats bitmap pgssub
    const embeddedOnly = subs.filter((s) => !s.IsExternal)
    expect(pickPreferredSubtitleIndex(embeddedOnly, 'auto', undefined, 'zh-CN')).toBe(4)
    expect(pickPreferredSubtitleIndex(embeddedOnly, 'auto', undefined, 'zh-TW')).toBe(6)
    expect(pickPreferredSubtitleIndex(embeddedOnly, 'auto', undefined, 'en-US')).toBe(2)
    expect(pickPreferredSubtitleIndex(subs, 'off', 2, 'zh-CN')).toBeUndefined()
    expect(pickPreferredSubtitleIndex(subs, 'auto', 2, 'zh-CN', 'None')).toBeUndefined()
  })

  it('supports OnlyForced and Always subtitle modes', () => {
    const subs: MediaStream[] = [
      { Type: 'Subtitle', Index: 3, Language: 'chi', Codec: 'srt', IsForced: false },
      { Type: 'Subtitle', Index: 4, Language: 'chi', Codec: 'srt', IsForced: true },
    ]
    expect(pickPreferredSubtitleIndex(subs, 'auto', undefined, 'zh-CN', 'OnlyForced')).toBe(4)

    const noForced: MediaStream[] = [
      { Type: 'Subtitle', Index: 3, Language: 'spa', Codec: 'srt', IsForced: false },
    ]
    expect(pickPreferredSubtitleIndex(noForced, 'auto', undefined, 'zh-CN', 'OnlyForced')).toBeUndefined()
    expect(pickPreferredSubtitleIndex(noForced, 'auto', undefined, 'zh-CN', 'Always')).toBe(3)
  })

  it('syncs cleanly between UserConfiguration and local PlaybackPrefs', () => {
    const synced = syncPlaybackPrefsFromUserConfig({
      AudioLanguagePreference: 'jpn',
      SubtitleLanguagePreference: 'cht',
      SubtitleMode: 'Smart',
      EnableNextEpisodeAutoPlay: false,
    })
    expect(synced).toEqual({
      autoPlayNextEpisode: false,
      preferredAudioLang: 'jpn',
      preferredSubtitleLang: 'cht',
      subtitleMode: 'Smart',
    })
    expect(loadPlaybackPrefs()).toEqual(synced)

    const updated = savePlaybackPrefs({ preferredAudioLang: 'media', preferredSubtitleLang: 'auto' })
    const cfg = playbackPrefsToUserConfig(updated, { RememberSubtitleSelections: true })
    expect(cfg.AudioLanguagePreference).toBe('')
    expect(cfg.PlayDefaultAudioTrack).toBe(true)
    expect(cfg.SubtitleLanguagePreference).toBe('auto')
    expect(cfg.SubtitleMode).toBe('Smart')
    expect(cfg.RememberSubtitleSelections).toBe(true)
  })

  it('H-20: PlayDefaultAudioTrack in playbackPrefsToUserConfig overrides existing config value', () => {
    const nonMedia = savePlaybackPrefs({ preferredAudioLang: 'jpn' })
    const cfg1 = playbackPrefsToUserConfig(nonMedia, { PlayDefaultAudioTrack: true })
    expect(cfg1.PlayDefaultAudioTrack).toBe(false)
    expect(cfg1.AudioLanguagePreference).toBe('jpn')

    const media = savePlaybackPrefs({ preferredAudioLang: 'media' })
    const cfg2 = playbackPrefsToUserConfig(media, { PlayDefaultAudioTrack: false })
    expect(cfg2.PlayDefaultAudioTrack).toBe(true)
    expect(cfg2.AudioLanguagePreference).toBe('')
  })

  it('H-21: supports Smart subtitle mode based on selectedAudioStream language', () => {
    const subsWithForced: MediaStream[] = [
      { Type: 'Subtitle', Index: 3, Language: 'chi', Codec: 'srt', Title: '简体中文', SubVariant: 'chs', IsForced: false },
      { Type: 'Subtitle', Index: 4, Language: 'chi', Codec: 'srt', Title: '简体强制字幕', SubVariant: 'chs', IsForced: true },
      { Type: 'Subtitle', Index: 5, Language: 'eng', Codec: 'srt', Title: 'English', IsForced: false },
    ]
    const subsWithoutForced = subsWithForced.filter((s) => !s.IsForced)

    const zhAudio: MediaStream = { Type: 'Audio', Index: 1, Language: 'chi', Codec: 'aac' }
    const jpnAudio: MediaStream = { Type: 'Audio', Index: 2, Language: 'jpn', Codec: 'aac' }

    // 音轨与用户母语/偏好语言一致：若有强制字幕则仅选强制字幕，否则不开启字幕
    expect(pickPreferredSubtitleIndex(subsWithForced, 'auto', undefined, 'zh-CN', 'Smart', zhAudio)).toBe(4)
    expect(pickPreferredSubtitleIndex(subsWithoutForced, 'auto', undefined, 'zh-CN', 'Smart', zhAudio)).toBeUndefined()

    // 音轨为外语：按偏好语言选中对应字幕
    expect(pickPreferredSubtitleIndex(subsWithoutForced, 'auto', undefined, 'zh-CN', 'Smart', jpnAudio)).toBe(3)
    expect(pickPreferredSubtitleIndex(subsWithForced, 'eng', undefined, 'zh-CN', 'Smart', jpnAudio)).toBe(5)
  })
})
