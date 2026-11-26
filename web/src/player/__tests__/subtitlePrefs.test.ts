import { beforeEach, describe, expect, it } from 'vitest'
import {
  clampSubtitlePrefs,
  DEFAULT_SUBTITLE_PREFS,
  formatSubtitleOffset,
  loadSubtitlePrefs,
  resetSubtitlePrefs,
  saveSubtitlePrefs,
  stepSubtitleOffset,
  SUBTITLE_PREFS_STORAGE_KEY,
} from '../subtitlePrefs'

describe('subtitlePrefs', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('returns default preferences when localStorage is empty or invalid', () => {
    expect(loadSubtitlePrefs()).toEqual(DEFAULT_SUBTITLE_PREFS)

    localStorage.setItem(SUBTITLE_PREFS_STORAGE_KEY, '{bad-json')
    expect(loadSubtitlePrefs()).toEqual(DEFAULT_SUBTITLE_PREFS)
  })

  it('clamps out-of-range and non-finite values to valid bounds', () => {
    expect(
      clampSubtitlePrefs({
        offsetSec: 25,
        fontScale: 0.1,
        bottomMarginPx: 999,
        bgOpacity: -0.5,
      }),
    ).toEqual({
      offsetSec: 10,
      fontScale: 0.75,
      bottomMarginPx: 140,
      bgOpacity: 0,
    })

    expect(
      clampSubtitlePrefs({
        offsetSec: -20,
        fontScale: 3.2,
        bottomMarginPx: 2,
        bgOpacity: 1.5,
      }),
    ).toEqual({
      offsetSec: -10,
      fontScale: 1.6,
      bottomMarginPx: 16,
      bgOpacity: 0.95,
    })

    expect(
      clampSubtitlePrefs({
        offsetSec: Number.NaN,
        fontScale: Number.POSITIVE_INFINITY,
        bottomMarginPx: undefined,
        bgOpacity: Number.NaN,
      }),
    ).toEqual(DEFAULT_SUBTITLE_PREFS)
  })

  it('persists and reloads preferences under ambra_subtitle_prefs', () => {
    const saved = saveSubtitlePrefs({
      offsetSec: -1.5,
      fontScale: 1.25,
      bottomMarginPx: 88,
      bgOpacity: 0.8,
    })
    expect(saved).toEqual({
      offsetSec: -1.5,
      fontScale: 1.25,
      bottomMarginPx: 88,
      bgOpacity: 0.8,
    })

    const raw = localStorage.getItem(SUBTITLE_PREFS_STORAGE_KEY)
    expect(raw).toBeTruthy()
    expect(loadSubtitlePrefs()).toEqual(saved)

    const reset = resetSubtitlePrefs()
    expect(reset).toEqual(DEFAULT_SUBTITLE_PREFS)
    expect(loadSubtitlePrefs()).toEqual(DEFAULT_SUBTITLE_PREFS)
  })

  it('steps and formats subtitle time offset accurately', () => {
    expect(stepSubtitleOffset(0, 0.5)).toBe(0.5)
    expect(stepSubtitleOffset(0.5, -0.5)).toBe(0)
    expect(stepSubtitleOffset(9.8, 0.5)).toBe(10)
    expect(stepSubtitleOffset(-9.8, -0.5)).toBe(-10)

    expect(formatSubtitleOffset(0)).toBe('0.0s')
    expect(formatSubtitleOffset(1.5)).toBe('+1.5s')
    expect(formatSubtitleOffset(-2)).toBe('-2.0s')
  })

  it('[M-02] scopes subtitle preferences by userId so switching accounts does not leak offsets', () => {
    saveSubtitlePrefs({ ...DEFAULT_SUBTITLE_PREFS, offsetSec: 2.5, fontScale: 1.3 }, 'user-1')
    expect(loadSubtitlePrefs('user-1').offsetSec).toBe(2.5)
    expect(loadSubtitlePrefs('user-2')).toEqual(DEFAULT_SUBTITLE_PREFS)

    resetSubtitlePrefs('user-1')
    expect(loadSubtitlePrefs('user-1')).toEqual(DEFAULT_SUBTITLE_PREFS)
  })
})
