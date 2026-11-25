import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it } from 'vitest'
import { LanguageSwitcher } from '../../components/LanguageSwitcher'
import {
  I18nProvider,
  detectInitialLocale,
  getActiveLocale,
  normalizeLocale,
  useI18n,
} from '../I18nContext'
import { LOCALE_STORAGE_KEY, SUPPORTED_LOCALES, type LocaleCode } from '../types'
import zhCN from '../locales/zh-CN.json'
import zhTW from '../locales/zh-TW.json'
import enUS from '../locales/en-US.json'
import jaJP from '../locales/ja-JP.json'
import koKR from '../locales/ko-KR.json'
import esES from '../locales/es-ES.json'
import deDE from '../locales/de-DE.json'
import frFR from '../locales/fr-FR.json'
import ptBR from '../locales/pt-BR.json'
import thTH from '../locales/th-TH.json'

const ALL_DICTS: Record<LocaleCode, Record<string, string>> = {
  'zh-CN': zhCN,
  'zh-TW': zhTW,
  'en-US': enUS,
  'ja-JP': jaJP,
  'ko-KR': koKR,
  'es-ES': esES,
  'de-DE': deDE,
  'fr-FR': frFR,
  'pt-BR': ptBR,
  'th-TH': thTH,
}

function extractPlaceholders(value: string): string[] {
  const matches = value.match(/\{\{\s*\w+\s*\}\}/g) ?? []
  return matches.map((m) => m.replace(/\s+/g, '')).sort()
}

function ProbeComponent() {
  const { locale, localeMeta, t, formatDateTime } = useI18n()
  return (
    <div>
      <span data-testid="locale-code">{locale}</span>
      <span data-testid="locale-native">{localeMeta.nativeName}</span>
      <span data-testid="nav-home">{t('nav.home')}</span>
      <span data-testid="selected-count">{t('library.selectedCount', { count: 7 })}</span>
      <span data-testid="next-up">{t('player.nextUpIn', { seconds: 15 })}</span>
      <span data-testid="missing-key">{t('non.existent.key')}</span>
      <span data-testid="formatted-date">
        {formatDateTime('2026-01-15T10:30:00Z', { year: 'numeric', timeZone: 'UTC' })}
      </span>
      <LanguageSwitcher />
    </div>
  )
}

describe('i18n locale dictionaries', () => {
  const baseKeys = Object.keys(zhCN).sort()

  it('covers all 10 supported locales', () => {
    expect(SUPPORTED_LOCALES).toHaveLength(10)
    expect(Object.keys(ALL_DICTS).sort()).toEqual(SUPPORTED_LOCALES.map((l) => l.code).sort())
  })

  it.each(SUPPORTED_LOCALES.map((l) => l.code))(
    'has complete key parity, non-empty values, and matching placeholders for %s',
    (code) => {
      const dict = ALL_DICTS[code]
      const keys = Object.keys(dict).sort()
      expect(keys).toEqual(baseKeys)

      for (const key of baseKeys) {
        const value = dict[key]
        expect(typeof value).toBe('string')
        expect(value.trim().length, `${code}:${key} should be non-empty`).toBeGreaterThan(0)
        expect(
          extractPlaceholders(value),
          `${code}:${key} placeholders should match zh-CN`,
        ).toEqual(extractPlaceholders(zhCN[key as keyof typeof zhCN]))
      }
    },
  )
})

describe('normalizeLocale', () => {
  it('maps all 10 base languages and regional variants', () => {
    expect(normalizeLocale('zh-CN')).toBe('zh-CN')
    expect(normalizeLocale('zh')).toBe('zh-CN')
    expect(normalizeLocale('zh-SG')).toBe('zh-CN')
    expect(normalizeLocale('zh-Hans-CN')).toBe('zh-CN')

    expect(normalizeLocale('zh-TW')).toBe('zh-TW')
    expect(normalizeLocale('zh-HK')).toBe('zh-TW')
    expect(normalizeLocale('zh-MO')).toBe('zh-TW')
    expect(normalizeLocale('zh-Hant')).toBe('zh-TW')
    expect(normalizeLocale('zh_Hant_TW')).toBe('zh-TW')

    expect(normalizeLocale('en')).toBe('en-US')
    expect(normalizeLocale('en-GB')).toBe('en-US')
    expect(normalizeLocale('en-AU')).toBe('en-US')

    expect(normalizeLocale('ja')).toBe('ja-JP')
    expect(normalizeLocale('ja-JP')).toBe('ja-JP')

    expect(normalizeLocale('ko')).toBe('ko-KR')
    expect(normalizeLocale('ko-KR')).toBe('ko-KR')

    expect(normalizeLocale('es')).toBe('es-ES')
    expect(normalizeLocale('es-MX')).toBe('es-ES')
    expect(normalizeLocale('es-419')).toBe('es-ES')

    expect(normalizeLocale('de')).toBe('de-DE')
    expect(normalizeLocale('de-AT')).toBe('de-DE')

    expect(normalizeLocale('fr')).toBe('fr-FR')
    expect(normalizeLocale('fr-CA')).toBe('fr-FR')

    expect(normalizeLocale('pt')).toBe('pt-BR')
    expect(normalizeLocale('pt-PT')).toBe('pt-BR')
    expect(normalizeLocale('pt-BR')).toBe('pt-BR')

    expect(normalizeLocale('th')).toBe('th-TH')
    expect(normalizeLocale('th-TH')).toBe('th-TH')
  })

  it('returns null for empty or unsupported inputs', () => {
    expect(normalizeLocale(undefined)).toBeNull()
    expect(normalizeLocale(null)).toBeNull()
    expect(normalizeLocale('')).toBeNull()
    expect(normalizeLocale('   ')).toBeNull()
    expect(normalizeLocale('ru-RU')).toBeNull()
    expect(normalizeLocale('ar-SA')).toBeNull()
  })
})

describe('I18nProvider & LanguageSwitcher', () => {
  beforeEach(() => {
    localStorage.clear()
    document.documentElement.lang = ''
  })

  it('defaults to zh-CN under Vitest when localStorage is empty', () => {
    expect(detectInitialLocale()).toBe('zh-CN')
  })

  it('gracefully works without I18nProvider wrapper', () => {
    render(<ProbeComponent />)
    expect(screen.getByTestId('nav-home').textContent).toBe(zhCN['nav.home'])
    expect(screen.getByTestId('selected-count').textContent).toBe('已选 7 项')
    expect(screen.getByTestId('missing-key').textContent).toBe('non.existent.key')
  })

  it('switches across locales via LanguageSwitcher, persists to localStorage, and updates document.documentElement.lang', () => {
    render(
      <I18nProvider initialLocale="zh-CN">
        <ProbeComponent />
      </I18nProvider>,
    )

    expect(screen.getByTestId('locale-code').textContent).toBe('zh-CN')
    expect(screen.getByTestId('nav-home').textContent).toBe('首页')
    expect(document.documentElement.lang).toBe('zh-CN')

    const transitions: Array<{
      code: LocaleCode
      nativeName: string
      expectedHome: string
      expectedCount: string
      expectedNextUp: string
    }> = [
      {
        code: 'zh-TW',
        nativeName: '繁體中文',
        expectedHome: zhTW['nav.home'],
        expectedCount: '已選 7 項',
        expectedNextUp: '15 秒後自動播放下一項',
      },
      {
        code: 'en-US',
        nativeName: 'English',
        expectedHome: enUS['nav.home'],
        expectedCount: '7 selected',
        expectedNextUp: 'Playing next in 15s',
      },
      {
        code: 'ja-JP',
        nativeName: '日本語',
        expectedHome: jaJP['nav.home'],
        expectedCount: '7 件選択中',
        expectedNextUp: '15 秒後に次を自動再生',
      },
      {
        code: 'ko-KR',
        nativeName: '한국어',
        expectedHome: koKR['nav.home'],
        expectedCount: '7개 선택됨',
        expectedNextUp: '15초 후 다음 항목 자동 재생',
      },
      {
        code: 'th-TH',
        nativeName: 'ไทย',
        expectedHome: thTH['nav.home'],
        expectedCount: 'เลือกแล้ว 7 รายการ',
        expectedNextUp: 'เล่นรายการถัดไปใน 15 วินาที',
      },
    ]

    for (const step of transitions) {
      const trigger = screen.getByRole('button', { name: ALL_DICTS[getActiveLocale()]['common.language'] })
      fireEvent.click(trigger)
      const option = screen.getByRole('option', { name: new RegExp(step.code) })
      fireEvent.click(option)

      expect(screen.getByTestId('locale-code').textContent).toBe(step.code)
      expect(screen.getByTestId('locale-native').textContent).toBe(step.nativeName)
      expect(screen.getByTestId('nav-home').textContent).toBe(step.expectedHome)
      expect(screen.getByTestId('selected-count').textContent).toBe(step.expectedCount)
      expect(screen.getByTestId('next-up').textContent).toBe(step.expectedNextUp)
      expect(localStorage.getItem(LOCALE_STORAGE_KEY)).toBe(step.code)
      expect(document.documentElement.lang).toBe(step.code)
      expect(getActiveLocale()).toBe(step.code)
    }

    expect(screen.getByTestId('formatted-date').textContent).toContain('2569')
  })
})
