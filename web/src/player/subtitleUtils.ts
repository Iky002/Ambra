import type { MediaStream } from '../api/types'

export type SubtitleEngineKind = 'ass' | 'pgs' | 'vtt' | 'none'

export interface SubtitleMeta {
  engine: SubtitleEngineKind
  engineLabel: string
  formatBadge: string
  variantBadge?: string
  displayLabel: string
}

export function detectSubtitleEngine(stream?: MediaStream | null): SubtitleEngineKind {
  if (!stream) return 'none'
  const codec = (stream.Codec || '').toLowerCase()
  const path = (stream.Path || stream.DeliveryUrl || '').toLowerCase()

  if (codec === 'ass' || codec === 'ssa' || path.endsWith('.ass') || path.endsWith('.ssa')) {
    return 'ass'
  }
  if (
    codec === 'pgssub' ||
    codec === 'pgs' ||
    codec === 'hdmv_pgs_subtitle' ||
    codec === 'sup' ||
    path.endsWith('.sup') ||
    path.endsWith('.pgs')
  ) {
    return 'pgs'
  }
  return 'vtt'
}

export function detectChineseVariant(stream: MediaStream): string | undefined {
  const subVar = (stream.SubVariant || '').toLowerCase()
  if (subVar === 'bilingual') return '双语'
  if (subVar === 'chs' || subVar === 'sc' || subVar === 'zh-cn') return '简体'
  if (subVar === 'cht' || subVar === 'tc' || subVar === 'zh-tw' || subVar === 'zh-hk') return '繁体'

  const combined = `${stream.Title || ''} ${stream.DisplayTitle || ''} ${stream.DisplayLanguage || ''} ${stream.Language || ''} ${stream.Path || ''}`.toLowerCase()

  const isBilingual =
    /双语|雙語|中英|简英|繁英|中字.*英|bilingual|chs&eng|cht&eng|sc&jp|tc&jp|简日|繁日/.test(
      combined,
    )
  if (isBilingual) return '双语'

  const isChs =
    /简体|简中|chs|gb|zh-cn|zh-hans|simplified|(\.sc\.)|(\bsc\b)/.test(combined)
  const isCht =
    /繁体|繁體|繁中|cht|big5|zh-tw|zh-hk|zh-hant|traditional|(\.tc\.)|(\btc\b)/.test(combined)

  if (isChs && isCht) return '简繁'
  if (isChs) return '简体'
  if (isCht) return '繁体'
  return undefined
}

export function getSubtitleMeta(stream?: MediaStream | null): SubtitleMeta {
  if (!stream) {
    return {
      engine: 'none',
      engineLabel: '关闭',
      formatBadge: '',
      displayLabel: '关闭字幕',
    }
  }

  const engine = detectSubtitleEngine(stream)
  const codec = (stream.Codec || '').toUpperCase()
  const variantBadge = detectChineseVariant(stream)

  let formatBadge = codec || 'SUB'
  let engineLabel = 'Native VTT'
  if (engine === 'ass') {
    formatBadge = codec === 'SSA' ? 'SSA' : 'ASS'
    engineLabel = 'JASSUB WASM'
  } else if (engine === 'pgs') {
    formatBadge = 'PGS'
    engineLabel = 'PGS Canvas'
  } else {
    formatBadge = codec === 'SUBRIP' || codec === 'SRT' ? 'SRT' : 'VTT'
    engineLabel = 'Native VTT'
  }

  const baseName =
    stream.DisplayTitle ||
    stream.Title ||
    stream.DisplayLanguage ||
    stream.Language ||
    `字幕轨 #${stream.Index}`

  const tags: string[] = []
  if (variantBadge && !baseName.includes(variantBadge)) {
    tags.push(variantBadge)
  }
  if (!baseName.toUpperCase().includes(formatBadge)) {
    tags.push(formatBadge)
  }
  if (stream.IsDefault) {
    tags.push('默认')
  }

  const displayLabel = tags.length > 0 ? `${baseName} [${tags.join(' · ')}]` : baseName

  return {
    engine,
    engineLabel,
    formatBadge,
    variantBadge,
    displayLabel,
  }
}

export function getAudioLabel(stream: MediaStream): string {
  const base =
    stream.DisplayTitle ||
    stream.Title ||
    stream.DisplayLanguage ||
    stream.Language ||
    `音轨 #${stream.Index}`
  const parts: string[] = []
  if (stream.Codec && !base.toUpperCase().includes(stream.Codec.toUpperCase())) {
    parts.push(stream.Codec.toUpperCase())
  }
  if (stream.Channels) {
    const chLabel =
      stream.Channels === 8
        ? '7.1'
        : stream.Channels === 6
          ? '5.1'
          : stream.Channels === 2
            ? '2.0'
            : `${stream.Channels}ch`
    if (!base.includes(chLabel)) parts.push(chLabel)
  }
  return parts.length > 0 ? `${base} (${parts.join(' ')})` : base
}

/**
 * 将 SRT 文本转换为标准 WebVTT，并解析为时间轴 Cue 数组以便自定义高质量渲染。
 */
export interface TextSubtitleCue {
  start: number
  end: number
  text: string
}

function parseTimestamp(ts: string): number {
  const clean = ts.trim().replace(',', '.')
  const parts = clean.split(':').map(Number)
  if (parts.length === 3) {
    return parts[0] * 3600 + parts[1] * 60 + parts[2]
  }
  if (parts.length === 2) {
    return parts[0] * 60 + parts[1]
  }
  return 0
}

export function parseVttOrSrt(raw: string): TextSubtitleCue[] {
  const normalized = raw.replace(/\r\n/g, '\n').replace(/\r/g, '\n').trim()
  const blocks = normalized.split(/\n{2,}/)
  const cues: TextSubtitleCue[] = []

  for (const block of blocks) {
    const lines = block
      .split('\n')
      .map((l) => l.trim())
      .filter(Boolean)
    if (lines.length === 0) continue
    if (lines[0].startsWith('WEBVTT') || lines[0].startsWith('NOTE') || lines[0].startsWith('STYLE')) {
      continue
    }

    let timeLineIdx = lines.findIndex((l) => l.includes('-->'))
    if (timeLineIdx === -1) continue

    const timeLine = lines[timeLineIdx]
    const [startRaw, endRawWithSettings] = timeLine.split('-->')
    if (!startRaw || !endRawWithSettings) continue

    const endRaw = endRawWithSettings.trim().split(/\s+/)[0]
    const start = parseTimestamp(startRaw)
    const end = parseTimestamp(endRaw)
    const text = lines
      .slice(timeLineIdx + 1)
      .join('\n')
      .replace(/<[^>]+>/g, '') // 去除基础 HTML/VTT 标签以防 XSS
      .trim()

    if (text && end > start) {
      cues.push({ start, end, text })
    }
  }

  return cues.sort((a, b) => a.start - b.start)
}

export function formatTicks(ticks?: number | null): string {
  if (!ticks || ticks <= 0) return ''
  const totalMinutes = Math.round(ticks / 10_000_000 / 60)
  const hours = Math.floor(totalMinutes / 60)
  const minutes = totalMinutes % 60
  if (hours > 0) return `${hours}h ${minutes}m`
  return `${minutes}m`
}

export function formatSeconds(sec: number): string {
  if (!Number.isFinite(sec) || sec < 0) return '00:00'
  const total = Math.floor(sec)
  const h = Math.floor(total / 3600)
  const m = Math.floor((total % 3600) / 60)
  const s = total % 60
  const pad = (n: number) => String(n).padStart(2, '0')
  return h > 0 ? `${h}:${pad(m)}:${pad(s)}` : `${pad(m)}:${pad(s)}`
}
