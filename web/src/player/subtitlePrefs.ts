export const SUBTITLE_PREFS_STORAGE_KEY = 'ambra_subtitle_prefs'

export interface SubtitlePrefs {
  /** 字幕时间轴偏移（秒），范围 -10 ~ +10，步进 0.5 */
  offsetSec: number
  /** 文本字幕字号缩放倍率，范围 0.75 ~ 1.6 */
  fontScale: number
  /** 文本字幕底边距（像素），范围 16 ~ 140 */
  bottomMarginPx: number
  /** 文本字幕背景不透明度，范围 0 ~ 0.95 */
  bgOpacity: number
}

export const SUBTITLE_PREFS_LIMITS = {
  offsetSec: { min: -10, max: 10, step: 0.5 },
  fontScale: { min: 0.75, max: 1.6, step: 0.05 },
  bottomMarginPx: { min: 16, max: 140, step: 4 },
  bgOpacity: { min: 0, max: 0.95, step: 0.05 },
} as const

export const DEFAULT_SUBTITLE_PREFS: SubtitlePrefs = {
  offsetSec: 0,
  fontScale: 1.0,
  bottomMarginPx: 56,
  bgOpacity: 0.65,
}

function clampNumber(
  value: unknown,
  min: number,
  max: number,
  fallback: number,
  decimals = 2,
): number {
  const num = typeof value === 'number' ? value : Number(value)
  if (!Number.isFinite(num)) return fallback
  const clamped = Math.min(max, Math.max(min, num))
  const factor = 10 ** decimals
  const rounded = Math.round(clamped * factor) / factor
  return Object.is(rounded, -0) ? 0 : rounded
}

export function clampSubtitlePrefs(partial?: Partial<SubtitlePrefs> | null): SubtitlePrefs {
  const src = partial ?? {}
  return {
    offsetSec: clampNumber(
      src.offsetSec,
      SUBTITLE_PREFS_LIMITS.offsetSec.min,
      SUBTITLE_PREFS_LIMITS.offsetSec.max,
      DEFAULT_SUBTITLE_PREFS.offsetSec,
      1,
    ),
    fontScale: clampNumber(
      src.fontScale,
      SUBTITLE_PREFS_LIMITS.fontScale.min,
      SUBTITLE_PREFS_LIMITS.fontScale.max,
      DEFAULT_SUBTITLE_PREFS.fontScale,
      2,
    ),
    bottomMarginPx: clampNumber(
      src.bottomMarginPx,
      SUBTITLE_PREFS_LIMITS.bottomMarginPx.min,
      SUBTITLE_PREFS_LIMITS.bottomMarginPx.max,
      DEFAULT_SUBTITLE_PREFS.bottomMarginPx,
      0,
    ),
    bgOpacity: clampNumber(
      src.bgOpacity,
      SUBTITLE_PREFS_LIMITS.bgOpacity.min,
      SUBTITLE_PREFS_LIMITS.bgOpacity.max,
      DEFAULT_SUBTITLE_PREFS.bgOpacity,
      2,
    ),
  }
}

export function loadSubtitlePrefs(): SubtitlePrefs {
  try {
    if (typeof localStorage === 'undefined') return { ...DEFAULT_SUBTITLE_PREFS }
    const raw = localStorage.getItem(SUBTITLE_PREFS_STORAGE_KEY)
    if (!raw) return { ...DEFAULT_SUBTITLE_PREFS }
    const parsed = JSON.parse(raw) as Partial<SubtitlePrefs>
    return clampSubtitlePrefs(parsed)
  } catch {
    return { ...DEFAULT_SUBTITLE_PREFS }
  }
}

export function saveSubtitlePrefs(prefs: Partial<SubtitlePrefs>): SubtitlePrefs {
  const normalized = clampSubtitlePrefs(prefs)
  try {
    if (typeof localStorage !== 'undefined') {
      localStorage.setItem(SUBTITLE_PREFS_STORAGE_KEY, JSON.stringify(normalized))
    }
  } catch {
    // ignore storage quota / privacy mode errors
  }
  return normalized
}

export function resetSubtitlePrefs(): SubtitlePrefs {
  return saveSubtitlePrefs(DEFAULT_SUBTITLE_PREFS)
}

export function stepSubtitleOffset(currentSec: number, deltaSec: number): number {
  const base = Number.isFinite(currentSec) ? currentSec : 0
  const delta = Number.isFinite(deltaSec) ? deltaSec : 0
  return clampNumber(
    base + delta,
    SUBTITLE_PREFS_LIMITS.offsetSec.min,
    SUBTITLE_PREFS_LIMITS.offsetSec.max,
    DEFAULT_SUBTITLE_PREFS.offsetSec,
    1,
  )
}

export function formatSubtitleOffset(offsetSec: number): string {
  const normalized = clampNumber(
    offsetSec,
    SUBTITLE_PREFS_LIMITS.offsetSec.min,
    SUBTITLE_PREFS_LIMITS.offsetSec.max,
    0,
    1,
  )
  if (normalized > 0) return `+${normalized.toFixed(1)}s`
  return `${normalized.toFixed(1)}s`
}
