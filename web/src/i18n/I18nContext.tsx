import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import {
  LOCALE_STORAGE_KEY,
  SUPPORTED_LOCALES,
  type LocaleCode,
  type LocaleMeta,
  type TranslationDict,
} from './types'
import zhCN from './locales/zh-CN.json'
import zhTW from './locales/zh-TW.json'
import enUS from './locales/en-US.json'
import jaJP from './locales/ja-JP.json'
import koKR from './locales/ko-KR.json'
import esES from './locales/es-ES.json'
import deDE from './locales/de-DE.json'
import frFR from './locales/fr-FR.json'
import ptBR from './locales/pt-BR.json'
import thTH from './locales/th-TH.json'

const DICTS: Record<LocaleCode, TranslationDict> = {
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

const LOCALE_META_MAP: Record<LocaleCode, LocaleMeta> = SUPPORTED_LOCALES.reduce(
  (acc, item) => {
    acc[item.code] = item
    return acc
  },
  {} as Record<LocaleCode, LocaleMeta>,
)

export function normalizeLocale(input?: string | null): LocaleCode | null {
  if (!input) return null
  const normalized = input.trim().toLowerCase().replace(/_/g, '-')
  if (!normalized) return null

  if (
    normalized === 'zh-tw' ||
    normalized === 'zh-hk' ||
    normalized === 'zh-mo' ||
    normalized === 'zh-hant' ||
    normalized.startsWith('zh-hant-') ||
    normalized.startsWith('zh-tw-') ||
    normalized.startsWith('zh-hk-') ||
    normalized.startsWith('zh-mo-')
  ) {
    return 'zh-TW'
  }
  if (normalized === 'zh' || normalized.startsWith('zh-')) return 'zh-CN'
  if (normalized === 'en' || normalized.startsWith('en-')) return 'en-US'
  if (normalized === 'ja' || normalized.startsWith('ja-')) return 'ja-JP'
  if (normalized === 'ko' || normalized.startsWith('ko-')) return 'ko-KR'
  if (normalized === 'es' || normalized.startsWith('es-')) return 'es-ES'
  if (normalized === 'de' || normalized.startsWith('de-')) return 'de-DE'
  if (normalized === 'fr' || normalized.startsWith('fr-')) return 'fr-FR'
  if (normalized === 'pt' || normalized.startsWith('pt-')) return 'pt-BR'
  if (normalized === 'th' || normalized.startsWith('th-')) return 'th-TH'

  return null
}

function readStoredLocale(): LocaleCode | null {
  try {
    if (typeof localStorage === 'undefined') return null
    return normalizeLocale(localStorage.getItem(LOCALE_STORAGE_KEY))
  } catch {
    return null
  }
}

function isTestEnvironment(): boolean {
  const mode = (import.meta as ImportMeta & { env?: { MODE?: string } }).env?.MODE
  if (mode === 'test') return true
  if (typeof navigator !== 'undefined' && /jsdom/i.test(navigator.userAgent)) {
    return true
  }
  return false
}

export function detectInitialLocale(): LocaleCode {
  const stored = readStoredLocale()
  if (stored) return stored

  if (isTestEnvironment()) {
    return 'zh-CN'
  }

  if (typeof navigator !== 'undefined') {
    const candidates: string[] = []
    if (Array.isArray(navigator.languages)) {
      candidates.push(...navigator.languages)
    }
    if (navigator.language) {
      candidates.push(navigator.language)
    }

    if (candidates.length > 0) {
      for (const candidate of candidates) {
        const matched = normalizeLocale(candidate)
        if (matched) return matched
      }
      return 'en-US'
    }
  }

  return 'zh-CN'
}

let activeLocale: LocaleCode | null = null

export function getActiveLocale(): LocaleCode {
  return activeLocale ?? detectInitialLocale()
}

export type TranslationVars = Record<string, string | number>

export function translateForLocale(
  locale: LocaleCode,
  key: string,
  vars?: TranslationVars,
): string {
  const template = DICTS[locale]?.[key] ?? DICTS['en-US']?.[key] ?? DICTS['zh-CN']?.[key] ?? key
  if (!vars) return template
  return template.replace(/\{\{\s*(\w+)\s*\}\}/g, (match, name: string) => {
    const value = vars[name]
    return value === undefined || value === null ? match : String(value)
  })
}

export function formatDateTimeForLocale(
  locale: LocaleCode,
  value?: string | number | Date | null,
  options?: Intl.DateTimeFormatOptions,
): string {
  if (value === null || value === undefined || value === '') return ''
  const date = value instanceof Date ? value : new Date(value)
  if (Number.isNaN(date.getTime())) return String(value)
  const dateLocale = LOCALE_META_MAP[locale]?.dateLocale ?? locale
  try {
    return new Intl.DateTimeFormat(dateLocale, options).format(date)
  } catch {
    return date.toLocaleString()
  }
}

export interface I18nContextValue {
  locale: LocaleCode
  localeMeta: LocaleMeta
  setLocale: (next: LocaleCode) => void
  t: (key: string, vars?: TranslationVars) => string
  formatDateTime: (
    value?: string | number | Date | null,
    options?: Intl.DateTimeFormatOptions,
  ) => string
}

const FALLBACK_CONTEXT_CACHE: Partial<Record<LocaleCode, I18nContextValue>> = {}

const setFallbackLocale = (next: LocaleCode) => {
  activeLocale = next
  try {
    if (typeof localStorage !== 'undefined') {
      localStorage.setItem(LOCALE_STORAGE_KEY, next)
    }
  } catch {
    // Ignore storage write failures in restricted environments
  }
  if (typeof document !== 'undefined') {
    document.documentElement.lang = next
  }
}

function getStableFallbackContextValue(): I18nContextValue {
  const locale = getActiveLocale()
  const cached = FALLBACK_CONTEXT_CACHE[locale]
  if (cached) return cached

  const created: I18nContextValue = {
    locale,
    localeMeta: LOCALE_META_MAP[locale] ?? SUPPORTED_LOCALES[0],
    setLocale: setFallbackLocale,
    t: (key: string, vars?: TranslationVars) => translateForLocale(locale, key, vars),
    formatDateTime: (
      value?: string | number | Date | null,
      options?: Intl.DateTimeFormatOptions,
    ) => formatDateTimeForLocale(locale, value, options),
  }
  FALLBACK_CONTEXT_CACHE[locale] = created
  return created
}

const I18nContext = createContext<I18nContextValue | null>(null)

export interface I18nProviderProps {
  initialLocale?: LocaleCode
  children: ReactNode
}

export function I18nProvider({ initialLocale, children }: I18nProviderProps) {
  const [locale, setLocaleState] = useState<LocaleCode>(() => {
    const resolved = initialLocale ?? detectInitialLocale()
    activeLocale = resolved
    return resolved
  })

  useEffect(() => {
    activeLocale = locale
    if (typeof document !== 'undefined') {
      document.documentElement.lang = locale
    }
  }, [locale])

  const setLocale = useCallback((next: LocaleCode) => {
    activeLocale = next
    setLocaleState(next)
    try {
      if (typeof localStorage !== 'undefined') {
        localStorage.setItem(LOCALE_STORAGE_KEY, next)
      }
    } catch {
      // Ignore storage write failures in restricted environments
    }
    if (typeof document !== 'undefined') {
      document.documentElement.lang = next
    }
  }, [])

  const t = useCallback(
    (key: string, vars?: TranslationVars) => translateForLocale(locale, key, vars),
    [locale],
  )

  const formatDateTime = useCallback(
    (value?: string | number | Date | null, options?: Intl.DateTimeFormatOptions) =>
      formatDateTimeForLocale(locale, value, options),
    [locale],
  )

  const value = useMemo<I18nContextValue>(
    () => ({
      locale,
      localeMeta: LOCALE_META_MAP[locale] ?? SUPPORTED_LOCALES[0],
      setLocale,
      t,
      formatDateTime,
    }),
    [locale, setLocale, t, formatDateTime],
  )

  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>
}

export function useI18n(): I18nContextValue {
  const ctx = useContext(I18nContext)
  if (ctx) return ctx
  return getStableFallbackContextValue()
}
