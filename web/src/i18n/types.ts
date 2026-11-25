export type LocaleCode =
  | 'zh-CN'
  | 'zh-TW'
  | 'en-US'
  | 'ja-JP'
  | 'ko-KR'
  | 'es-ES'
  | 'de-DE'
  | 'fr-FR'
  | 'pt-BR'
  | 'th-TH'

export interface LocaleMeta {
  code: LocaleCode
  nativeName: string
  englishName: string
  shortLabel: string
  dateLocale: string
}

export const SUPPORTED_LOCALES: LocaleMeta[] = [
  { code: 'zh-CN', nativeName: '简体中文', englishName: 'Chinese (Simplified)', shortLabel: '简中', dateLocale: 'zh-CN' },
  { code: 'zh-TW', nativeName: '繁體中文', englishName: 'Chinese (Traditional)', shortLabel: '繁中', dateLocale: 'zh-TW' },
  { code: 'en-US', nativeName: 'English', englishName: 'English', shortLabel: 'EN', dateLocale: 'en-US' },
  { code: 'ja-JP', nativeName: '日本語', englishName: 'Japanese', shortLabel: '日本語', dateLocale: 'ja-JP' },
  { code: 'ko-KR', nativeName: '한국어', englishName: 'Korean', shortLabel: '한국어', dateLocale: 'ko-KR' },
  { code: 'es-ES', nativeName: 'Español', englishName: 'Spanish', shortLabel: 'ES', dateLocale: 'es-ES' },
  { code: 'de-DE', nativeName: 'Deutsch', englishName: 'German', shortLabel: 'DE', dateLocale: 'de-DE' },
  { code: 'fr-FR', nativeName: 'Français', englishName: 'French', shortLabel: 'FR', dateLocale: 'fr-FR' },
  { code: 'pt-BR', nativeName: 'Português', englishName: 'Portuguese', shortLabel: 'PT', dateLocale: 'pt-BR' },
  { code: 'th-TH', nativeName: 'ไทย', englishName: 'Thai', shortLabel: 'ไทย', dateLocale: 'th-TH' },
]

export const LOCALE_STORAGE_KEY = 'ambra_locale_v1'

export type TranslationDict = Record<string, string>
