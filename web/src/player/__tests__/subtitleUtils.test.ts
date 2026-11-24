import { describe, expect, it } from 'vitest'
import type { MediaStream } from '../../api/types'
import {
  detectChineseVariant,
  detectSubtitleEngine,
  formatSeconds,
  formatTicks,
  getAudioLabel,
  getSubtitleMeta,
  parseVttOrSrt,
} from '../subtitleUtils'

describe('subtitleUtils - 字幕引擎分发判断', () => {
  it('空流返回 none', () => {
    expect(detectSubtitleEngine(null)).toBe('none')
    expect(detectSubtitleEngine(undefined)).toBe('none')
  })

  it('准确识别 ASS / SSA 字幕（基于 Codec 或 Path/DeliveryUrl）', () => {
    expect(detectSubtitleEngine({ Type: 'Subtitle', Index: 2, Codec: 'ass' })).toBe('ass')
    expect(detectSubtitleEngine({ Type: 'Subtitle', Index: 3, Codec: 'SSA' })).toBe('ass')
    expect(
      detectSubtitleEngine({ Type: 'Subtitle', Index: 4, Codec: 'unknown', Path: '/media/movie.chs.ass' }),
    ).toBe('ass')
    expect(
      detectSubtitleEngine({
        Type: 'Subtitle',
        Index: 5,
        DeliveryUrl: '/emby/Videos/1/Subtitles/5/Stream.ssa',
      }),
    ).toBe('ass')
  })

  it('准确识别 PGS Blu-ray 图形字幕', () => {
    expect(detectSubtitleEngine({ Type: 'Subtitle', Index: 2, Codec: 'pgssub' })).toBe('pgs')
    expect(detectSubtitleEngine({ Type: 'Subtitle', Index: 3, Codec: 'PGS' })).toBe('pgs')
    expect(detectSubtitleEngine({ Type: 'Subtitle', Index: 4, Codec: 'hdmv_pgs_subtitle' })).toBe('pgs')
    expect(detectSubtitleEngine({ Type: 'Subtitle', Index: 5, Codec: 'sup' })).toBe('pgs')
    expect(
      detectSubtitleEngine({ Type: 'Subtitle', Index: 6, Path: '/media/bluray.sup' }),
    ).toBe('pgs')
    expect(
      detectSubtitleEngine({ Type: 'Subtitle', Index: 7, DeliveryUrl: '/sub/track.pgs' }),
    ).toBe('pgs')
  })

  it('文本字幕 SRT / SubRip / VTT 统一分发到 vtt 引擎', () => {
    expect(detectSubtitleEngine({ Type: 'Subtitle', Index: 2, Codec: 'srt' })).toBe('vtt')
    expect(detectSubtitleEngine({ Type: 'Subtitle', Index: 3, Codec: 'subrip' })).toBe('vtt')
    expect(detectSubtitleEngine({ Type: 'Subtitle', Index: 4, Codec: 'vtt' })).toBe('vtt')
    expect(detectSubtitleEngine({ Type: 'Subtitle', Index: 5, Codec: 'webvtt' })).toBe('vtt')
  })
})

describe('subtitleUtils - 简繁/双语中文字幕变体识别', () => {
  it('优先使用 Ambra 后端扩展字段 SubVariant', () => {
    expect(detectChineseVariant({ Type: 'Subtitle', Index: 1, SubVariant: 'bilingual' })).toBe('双语')
    expect(detectChineseVariant({ Type: 'Subtitle', Index: 1, SubVariant: 'chs' })).toBe('简体')
    expect(detectChineseVariant({ Type: 'Subtitle', Index: 1, SubVariant: 'zh-cn' })).toBe('简体')
    expect(detectChineseVariant({ Type: 'Subtitle', Index: 1, SubVariant: 'cht' })).toBe('繁体')
    expect(detectChineseVariant({ Type: 'Subtitle', Index: 1, SubVariant: 'zh-tw' })).toBe('繁体')
    expect(detectChineseVariant({ Type: 'Subtitle', Index: 1, SubVariant: 'zh-hk' })).toBe('繁体')
  })

  it('通过标题、语言与文件路径识别双语/简体/繁体/简繁', () => {
    expect(
      detectChineseVariant({ Type: 'Subtitle', Index: 1, Title: '简英双语特效', Codec: 'ass' }),
    ).toBe('双语')
    expect(
      detectChineseVariant({ Type: 'Subtitle', Index: 1, Path: '/movies/demo.chs&eng.ass' }),
    ).toBe('双语')
    expect(
      detectChineseVariant({ Type: 'Subtitle', Index: 1, Path: '/anime/ep01.sc&jp.ass' }),
    ).toBe('双语')
    expect(
      detectChineseVariant({ Type: 'Subtitle', Index: 1, DisplayTitle: 'Chinese Simplified (CHS)' }),
    ).toBe('简体')
    expect(
      detectChineseVariant({ Type: 'Subtitle', Index: 1, Path: '/movies/demo.sc.srt' }),
    ).toBe('简体')
    expect(
      detectChineseVariant({ Type: 'Subtitle', Index: 1, DisplayTitle: '繁體中文 (Big5)' }),
    ).toBe('繁体')
    expect(
      detectChineseVariant({ Type: 'Subtitle', Index: 1, Path: '/movies/demo.tc.ass' }),
    ).toBe('繁体')
    expect(
      detectChineseVariant({ Type: 'Subtitle', Index: 1, Title: '简体/繁体合集' }),
    ).toBe('简繁')
    expect(
      detectChineseVariant({ Type: 'Subtitle', Index: 1, Title: 'English SDH', Language: 'eng' }),
    ).toBeUndefined()
  })
})

describe('subtitleUtils - 字幕与音轨元信息格式化', () => {
  it('正确生成关闭字幕与各引擎字幕的元数据标签', () => {
    expect(getSubtitleMeta(null)).toEqual({
      engine: 'none',
      engineLabel: '关闭',
      formatBadge: '',
      displayLabel: '关闭字幕',
    })

    const assStream: MediaStream = {
      Type: 'Subtitle',
      Index: 2,
      Codec: 'ass',
      DisplayTitle: '中文字幕',
      SubVariant: 'bilingual',
      IsDefault: true,
    }
    const assMeta = getSubtitleMeta(assStream)
    expect(assMeta.engine).toBe('ass')
    expect(assMeta.engineLabel).toBe('JASSUB WASM')
    expect(assMeta.formatBadge).toBe('ASS')
    expect(assMeta.variantBadge).toBe('双语')
    expect(assMeta.displayLabel).toBe('中文字幕 [双语 · ASS · 默认]')

    const pgsStream: MediaStream = {
      Type: 'Subtitle',
      Index: 3,
      Codec: 'pgssub',
      Title: '繁体中文 PGS',
      SubVariant: 'cht',
    }
    const pgsMeta = getSubtitleMeta(pgsStream)
    expect(pgsMeta.engine).toBe('pgs')
    expect(pgsMeta.engineLabel).toBe('PGS Canvas')
    expect(pgsMeta.formatBadge).toBe('PGS')
    // 标题中已包含“繁体”和“PGS”，不应重复追加
    expect(pgsMeta.displayLabel).toBe('繁体中文 PGS')

    const srtMeta = getSubtitleMeta({
      Type: 'Subtitle',
      Index: 4,
      Codec: 'subrip',
      Language: 'chi',
    })
    expect(srtMeta.engine).toBe('vtt')
    expect(srtMeta.formatBadge).toBe('SRT')
    expect(srtMeta.displayLabel).toBe('chi [SRT]')
  })

  it('正确格式化音轨编码与声道数', () => {
    expect(
      getAudioLabel({ Type: 'Audio', Index: 1, DisplayTitle: '国语', Codec: 'aac', Channels: 6 }),
    ).toBe('国语 (AAC 5.1)')
    expect(
      getAudioLabel({ Type: 'Audio', Index: 1, Title: 'English TrueHD 7.1', Codec: 'truehd', Channels: 8 }),
    ).toBe('English TrueHD 7.1')
    expect(getAudioLabel({ Type: 'Audio', Index: 2, Codec: 'opus', Channels: 2 })).toBe(
      '音轨 #2 (OPUS 2.0)',
    )
  })
})

describe('subtitleUtils - SRT 与 WebVTT 解析与样式清理', () => {
  it('解析标准 SRT 时间轴（含逗号毫秒、多行文本和 HTML 标签清理）', () => {
    const srt = `1
00:00:01,500 --> 00:00:04,250
<i>第一行对白</i>
<b>第二行 <script>alert(1)</script>对白</b>

2
00:01:05,000 --> 00:01:08,500
单行字幕
`
    const cues = parseVttOrSrt(srt)
    expect(cues).toHaveLength(2)
    expect(cues[0]).toEqual({
      start: 1.5,
      end: 4.25,
      text: '第一行对白\n第二行 alert(1)对白',
    })
    expect(cues[1]).toEqual({
      start: 65,
      end: 68.5,
      text: '单行字幕',
    })
  })

  it('解析 WebVTT（跳过头部/注释/样式、支持 MM:SS.mmm 与 cue settings、自动排序并过滤无效项）', () => {
    const vtt = `WEBVTT

NOTE 这是注释，应被忽略

STYLE
::cue { color: yellow; }

cue-late
01:10.500 --> 01:15.000 align:center line:85%
<c.amber>后出现的句子</c>

cue-early
00:02.000 --> 00:05.500
先出现的句子

cue-invalid
00:10.000 --> 00:09.000
结束时间早于开始时间应被过滤
`
    const cues = parseVttOrSrt(vtt)
    expect(cues).toHaveLength(2)
    expect(cues[0]).toEqual({ start: 2, end: 5.5, text: '先出现的句子' })
    expect(cues[1]).toEqual({ start: 70.5, end: 75, text: '后出现的句子' })
  })

  it('正确格式化 Ticks 与秒数', () => {
    expect(formatTicks(0)).toBe('')
    expect(formatTicks(45 * 60 * 10_000_000)).toBe('45m')
    expect(formatTicks(125 * 60 * 10_000_000)).toBe('2h 5m')

    expect(formatSeconds(-1)).toBe('00:00')
    expect(formatSeconds(65)).toBe('01:05')
    expect(formatSeconds(3661)).toBe('1:01:01')
  })
})
