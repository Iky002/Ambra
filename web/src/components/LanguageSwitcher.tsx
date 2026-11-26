import { Globe } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { api, getStoredAuth } from '../api/client'
import { useOptionalAuth } from '../context/AuthContext'
import { useI18n } from '../i18n/I18nContext'
import { SUPPORTED_LOCALES, type LocaleCode } from '../i18n/types'
import { loadPlaybackPrefs, playbackPrefsToUserConfig } from '../player/playbackPrefs'

interface LanguageSwitcherProps {
  compact?: boolean
  align?: 'left' | 'right'
}

export function LanguageSwitcher({ compact = false, align = 'right' }: LanguageSwitcherProps) {
  const { locale, localeMeta, setLocale, t } = useI18n()
  const authCtx = useOptionalAuth()
  const [open, setOpen] = useState(false)
  const containerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const onPointerDown = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', onPointerDown)
    window.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('mousedown', onPointerDown)
      window.removeEventListener('keydown', onKeyDown)
    }
  }, [open])

  const handleSelect = (code: LocaleCode) => {
    setLocale(code)
    setOpen(false)

    const activeSession = authCtx?.auth ?? getStoredAuth()
    if (activeSession?.userId && activeSession.accessToken) {
      const targetUserId = activeSession.userId
      const existingCfg = authCtx?.user?.Configuration
      const syncCulture = async () => {
        const baseCfg =
          existingCfg ??
          (await api
            .getUser(targetUserId)
            .then((u) => u.Configuration)
            .catch(() => undefined))
        const mergedCfg = {
          ...playbackPrefsToUserConfig(loadPlaybackPrefs(targetUserId), baseCfg),
          UICulture: code,
        }
        await api.updateUserConfiguration(targetUserId, mergedCfg)
        await authCtx?.refreshUser()
      }
      void syncCulture().catch(() => undefined)
    }
  }

  return (
    <div ref={containerRef} className="relative">
      <button
        type="button"
        onClick={() => setOpen((prev) => !prev)}
        title={t('common.language')}
        aria-label={t('common.language')}
        aria-expanded={open}
        aria-haspopup="listbox"
        className="inline-flex items-center gap-1.5 rounded-xl border border-white/10 bg-white/[0.04] px-2.5 py-1.5 text-xs font-medium text-zinc-300 transition-colors hover:border-amber-400/40 hover:bg-white/[0.08] hover:text-zinc-100"
      >
        <Globe className="h-3.5 w-3.5 text-amber-400/85" />
        <span>{compact ? localeMeta.shortLabel : localeMeta.nativeName}</span>
      </button>

      {open && (
        <div
          role="listbox"
          aria-label={t('common.language')}
          className={`absolute top-full z-50 mt-2 w-48 overflow-hidden rounded-2xl border border-white/12 bg-[#111118]/98 py-1.5 shadow-2xl shadow-black/60 backdrop-blur-xl ${
            align === 'left' ? 'left-0' : 'right-0'
          }`}
        >
          {SUPPORTED_LOCALES.map((item) => {
            const active = item.code === locale
            return (
              <button
                key={item.code}
                type="button"
                role="option"
                aria-selected={active}
                onClick={() => handleSelect(item.code)}
                className={`flex w-full items-center justify-between px-3.5 py-2 text-left text-xs transition-colors ${
                  active
                    ? 'bg-amber-500/15 font-semibold text-amber-300'
                    : 'text-zinc-300 hover:bg-white/[0.06] hover:text-zinc-100'
                }`}
              >
                <span>{item.nativeName}</span>
                <span className="text-[10px] text-zinc-500">{item.code}</span>
              </button>
            )
          })}
        </div>
      )}
    </div>
  )
}
