import { useEffect, useRef, useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import {
  AlertCircle,
  Camera,
  CheckCircle2,
  Globe,
  KeyRound,
  Loader2,
  LogOut,
  QrCode,
  ShieldCheck,
  Sliders,
  Trash2,
  User as UserIcon,
} from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import { api, ApiError, getUserImageUrl } from '../api/client'
import { useAuth } from '../context/AuthContext'
import { useI18n } from '../i18n/I18nContext'
import { SUPPORTED_LOCALES } from '../i18n/types'
import {
  loadPlaybackPrefs,
  playbackPrefsToUserConfig,
  savePlaybackPrefs,
  syncPlaybackPrefsFromUserConfig,
  type PlaybackPrefs,
  type SubtitleModeOption,
} from '../player/playbackPrefs'
import { FeedbackBanner } from './admin/adminUtils'
import { TraktSettings } from '../components/TraktSettings'

const ALLOWED_AVATAR_MIME_TYPES = new Set(['image/jpeg', 'image/png', 'image/webp', 'image/gif'])
const MAX_AVATAR_UPLOAD_BYTES = 5 * 1024 * 1024 // 5 MB

const AUDIO_LANG_OPTIONS = [
  { value: 'media', labelKey: 'profile.langMediaDefault' },
  { value: 'auto', labelKey: 'profile.langFollowUi' },
  { value: 'chi', labelKey: 'profile.langChinese' },
  { value: 'eng', labelKey: 'profile.langEnglish' },
  { value: 'jpn', labelKey: 'profile.langJapanese' },
  { value: 'kor', labelKey: 'profile.langKorean' },
  { value: 'spa', labelKey: 'profile.langSpanish' },
  { value: 'ger', labelKey: 'profile.langGerman' },
  { value: 'fre', labelKey: 'profile.langFrench' },
  { value: 'por', labelKey: 'profile.langPortuguese' },
  { value: 'tha', labelKey: 'profile.langThai' },
]

const SUBTITLE_LANG_OPTIONS = [
  { value: 'auto', labelKey: 'profile.langFollowUiDefault' },
  { value: 'media', labelKey: 'profile.langMediaDefault' },
  { value: 'chs', labelKey: 'profile.langChineseSimplified' },
  { value: 'cht', labelKey: 'profile.langChineseTraditional' },
  { value: 'chi', labelKey: 'profile.langChineseAny' },
  { value: 'eng', labelKey: 'profile.langEnglish' },
  { value: 'jpn', labelKey: 'profile.langJapanese' },
  { value: 'kor', labelKey: 'profile.langKorean' },
  { value: 'spa', labelKey: 'profile.langSpanish' },
  { value: 'ger', labelKey: 'profile.langGerman' },
  { value: 'fre', labelKey: 'profile.langFrench' },
  { value: 'por', labelKey: 'profile.langPortuguese' },
  { value: 'tha', labelKey: 'profile.langThai' },
  { value: 'off', labelKey: 'profile.subOff' },
]

const SUBTITLE_MODE_OPTIONS: { value: SubtitleModeOption; labelKey: string }[] = [
  { value: 'Default', labelKey: 'profile.subModeDefault' },
  { value: 'Smart', labelKey: 'profile.subModeSmart' },
  { value: 'OnlyForced', labelKey: 'profile.subModeOnlyForced' },
  { value: 'Always', labelKey: 'profile.subModeAlways' },
  { value: 'None', labelKey: 'profile.subModeNone' },
]

export function ProfilePage() {
  const { auth, user, isAdmin, refreshUser, logout } = useAuth()
  const { locale, setLocale, t, formatDateTime } = useI18n()
  const navigate = useNavigate()
  const userId = auth?.userId || ''
  const fileInputRef = useRef<HTMLInputElement>(null)
  const prefsSeqRef = useRef(0)
  const prefsInFlightRef = useRef(0)
  const prefsQueueRef = useRef<{ nextPrefs: PlaybackPrefs; seq: number } | null>(null)
  const prefsDrainingRef = useRef(false)
  const userConfigRef = useRef(user?.Configuration)
  userConfigRef.current = user?.Configuration

  const [avatarError, setAvatarError] = useState(false)
  const [feedback, setFeedback] = useState<{ type: 'success' | 'error'; message: string } | null>(
    null,
  )

  // 修改密码表单
  const [currentPw, setCurrentPw] = useState('')
  const [newPw, setNewPw] = useState('')
  const [confirmPw, setConfirmPw] = useState('')

  // Quick Connect 授权新设备
  const [qcCode, setQcCode] = useState('')
  const [qcFeedback, setQcFeedback] = useState<{
    type: 'success' | 'error'
    message: string
  } | null>(null)

  // 播放偏好（优先与账号 UserConfiguration 同步，localStorage 作本地缓存）
  const [prefs, setPrefs] = useState<PlaybackPrefs>(() => loadPlaybackPrefs(userId))

  useEffect(() => {
    setPrefs(loadPlaybackPrefs(userId))
  }, [userId])

  useEffect(() => {
    if (user?.Configuration && prefsInFlightRef.current === 0) {
      setPrefs(syncPlaybackPrefsFromUserConfig(user.Configuration, userId))
    }
  }, [user?.Configuration, userId])

  useEffect(() => {
    setAvatarError(false)
  }, [user?.PrimaryImageTag])

  useEffect(() => {
    if (!feedback) return
    const t = window.setTimeout(() => setFeedback(null), 4500)
    return () => window.clearTimeout(t)
  }, [feedback])

  const uploadAvatarMutation = useMutation({
    mutationFn: async (file: File) => {
      const dataUrl = await new Promise<string>((resolve, reject) => {
        const reader = new FileReader()
        reader.onload = () => resolve(String(reader.result || ''))
        reader.onerror = () => reject(new Error(t('profile.errReadImage')))
        reader.readAsDataURL(file)
      })
      await api.uploadUserImage(userId, dataUrl, file.type || 'image/jpeg')
    },
    onSuccess: async () => {
      await refreshUser()
      setAvatarError(false)
      setFeedback({ type: 'success', message: t('profile.avatarUpdated') })
    },
    onError: (err) => {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('profile.avatarUploadFailed'),
      })
    },
  })

  const handleAvatarFileSelected = (file?: File | null) => {
    if (!file) return
    if (!ALLOWED_AVATAR_MIME_TYPES.has((file.type || '').toLowerCase())) {
      setFeedback({ type: 'error', message: t('profile.errAvatarFormat') })
      return
    }
    if (file.size > MAX_AVATAR_UPLOAD_BYTES) {
      setFeedback({ type: 'error', message: t('profile.errAvatarSize') })
      return
    }
    uploadAvatarMutation.mutate(file)
  }

  const deleteAvatarMutation = useMutation({
    mutationFn: () => api.deleteUserImage(userId),
    onSuccess: async () => {
      await refreshUser()
      setAvatarError(false)
      setFeedback({ type: 'success', message: t('profile.avatarRemoved') })
    },
    onError: (err) => {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('profile.avatarRemoveFailed'),
      })
    },
  })

  const passwordMutation = useMutation({
    mutationFn: () =>
      api.updateUserPassword(userId, {
        CurrentPw: currentPw,
        NewPw: newPw,
      }),
    onSuccess: () => {
      setCurrentPw('')
      setNewPw('')
      setConfirmPw('')
      setFeedback({ type: 'success', message: t('profile.passwordChangedSuccess') })
    },
    onError: (err) => {
      if (err instanceof ApiError && err.status === 401) {
        setFeedback({ type: 'error', message: t('profile.sessionExpired') })
        navigate('/login', { replace: true })
        return
      }
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('profile.passwordChangeFailed'),
      })
    },
  })

  const handlePasswordSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (!newPw.trim()) {
      setFeedback({ type: 'error', message: t('profile.errNewPasswordRequired') })
      return
    }
    if (newPw !== confirmPw) {
      setFeedback({ type: 'error', message: t('profile.errPasswordMismatch') })
      return
    }
    passwordMutation.mutate()
  }

  const quickConnectMutation = useMutation({
    mutationFn: (code: string) => api.quickConnectAuthorize(code),
    onSuccess: (ok) => {
      if (ok === false) {
        setQcFeedback({ type: 'error', message: t('profile.qcInvalidOrExpired') })
        return
      }
      setQcCode('')
      setQcFeedback({ type: 'success', message: t('profile.qcAuthorizedSuccess') })
    },
    onError: (err) => {
      setQcFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('profile.qcAuthorizeFailed'),
      })
    },
  })

  const handleQuickConnectSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    const trimmed = qcCode.trim()
    if (!trimmed) {
      setQcFeedback({ type: 'error', message: t('profile.errQcCodeRequired') })
      return
    }
    setQcFeedback(null)
    quickConnectMutation.mutate(trimmed)
  }

  const drainPrefsQueue = async () => {
    if (prefsDrainingRef.current) return
    prefsDrainingRef.current = true
    try {
      while (prefsQueueRef.current) {
        const currentTask = prefsQueueRef.current
        prefsQueueRef.current = null
        prefsInFlightRef.current += 1
        try {
          if (userId) {
            const nextCfg = playbackPrefsToUserConfig(currentTask.nextPrefs, userConfigRef.current)
            await api.updateUserConfiguration(userId, nextCfg)
            userConfigRef.current = nextCfg
          }
          if (!prefsQueueRef.current && currentTask.seq === prefsSeqRef.current) {
            await refreshUser()
            if (!prefsQueueRef.current && currentTask.seq === prefsSeqRef.current) {
              setFeedback({ type: 'success', message: t('profile.prefsSavedSuccess') })
            }
          }
        } catch (err) {
          if (!prefsQueueRef.current && currentTask.seq === prefsSeqRef.current) {
            setFeedback({
              type: 'error',
              message:
                err instanceof Error
                  ? t('profile.prefsSavedLocalSyncFailedMsg', { message: err.message })
                  : t('profile.prefsSavedLocalSyncFailed'),
            })
          }
        } finally {
          prefsInFlightRef.current = Math.max(0, prefsInFlightRef.current - 1)
        }
      }
    } finally {
      prefsDrainingRef.current = false
    }
  }

  const handlePrefsChange = (patch: Partial<PlaybackPrefs>) => {
    const seq = ++prefsSeqRef.current
    const updated = savePlaybackPrefs(patch, userId)
    setPrefs(updated)
    prefsQueueRef.current = { nextPrefs: updated, seq }
    void drainPrefsQueue()
  }

  const handleSelectLocale = (code: typeof locale) => {
    setLocale(code)
    if (!userId) return
    const nextCfg = {
      ...playbackPrefsToUserConfig(prefs, userConfigRef.current),
      UICulture: code,
    }
    userConfigRef.current = nextCfg
    void api
      .updateUserConfiguration(userId, nextCfg)
      .then(() => refreshUser())
      .catch(() => undefined)
  }

  if (!auth) return null

  const hasAvatar = Boolean(user?.PrimaryImageTag) && !avatarError
  const avatarUrl = hasAvatar ? getUserImageUrl(userId, user?.PrimaryImageTag) : null
  const displayName = user?.Name || auth?.userName || t('profile.defaultUserName')

  return (
    <div className="mx-auto max-w-4xl space-y-6 px-4 pt-6 lg:px-8">
      <div className="border-b border-white/8 pb-5">
        <h1 className="flex items-center gap-2.5 text-2xl font-bold tracking-tight text-white">
          <UserIcon className="h-6 w-6 text-amber-400" />
          <span>{t('profile.title')}</span>
        </h1>
        <p className="mt-1 text-xs text-zinc-400">{t('profile.subtitle')}</p>
      </div>

      {feedback && (
        <FeedbackBanner
          type={feedback.type}
          message={feedback.message}
          onClose={() => setFeedback(null)}
        />
      )}

      {/* 账号与头像 */}
      <section className="rounded-2xl border border-white/10 bg-zinc-950/70 p-6 backdrop-blur-md">
        <div className="flex flex-col gap-5 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex items-center gap-4">
            <div className="relative flex h-20 w-20 shrink-0 items-center justify-center overflow-hidden rounded-full bg-gradient-to-br from-amber-500/30 to-orange-500/20 text-2xl font-bold text-amber-300 ring-2 ring-amber-400/35">
              {avatarUrl ? (
                <img
                  src={avatarUrl}
                  alt={displayName}
                  onError={() => setAvatarError(true)}
                  className="h-full w-full object-cover"
                />
              ) : (
                <span>{displayName.slice(0, 1).toUpperCase()}</span>
              )}
            </div>
            <div>
              <div className="flex items-center gap-2">
                <h2 className="text-lg font-semibold text-white">{displayName}</h2>
                <span
                  className={`inline-flex items-center gap-1 rounded-full px-2.5 py-0.5 text-[11px] font-medium ${
                    isAdmin
                      ? 'bg-amber-500/20 text-amber-300 ring-1 ring-amber-400/35'
                      : 'bg-white/10 text-zinc-300'
                  }`}
                >
                  {isAdmin && <ShieldCheck className="h-3 w-3" />}
                  {isAdmin ? t('nav.roleAdmin') : t('nav.roleMember')}
                </span>
              </div>
              <p className="mt-1 text-xs text-zinc-400">
                {t('profile.userIdLabel')}
                <span className="font-mono text-zinc-300">{userId}</span>
              </p>
              {user?.LastLoginDate && (
                <p className="mt-0.5 text-[11px] text-zinc-500">
                  {t('profile.lastLoginLabel', { time: formatDateTime(user.LastLoginDate) })}
                </p>
              )}
            </div>
          </div>

          <div className="flex flex-wrap items-center gap-2.5">
            <input
              ref={fileInputRef}
              type="file"
              accept="image/jpeg,image/png,image/webp,image/gif"
              aria-label={t('profile.selectAvatarAria')}
              className="hidden"
              onChange={(e) => {
                handleAvatarFileSelected(e.target.files?.[0])
                e.target.value = ''
              }}
            />
            <button
              type="button"
              disabled={uploadAvatarMutation.isPending}
              onClick={() => fileInputRef.current?.click()}
              className="inline-flex items-center gap-1.5 rounded-xl bg-amber-500 px-4 py-2 text-xs font-semibold text-zinc-950 transition-colors hover:bg-amber-400 disabled:opacity-50"
            >
              {uploadAvatarMutation.isPending ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <Camera className="h-3.5 w-3.5" />
              )}
              <span>{t('profile.uploadAvatar')}</span>
            </button>
            {user?.PrimaryImageTag && (
              <button
                type="button"
                disabled={deleteAvatarMutation.isPending}
                onClick={() => deleteAvatarMutation.mutate()}
                className="inline-flex items-center gap-1.5 rounded-xl border border-red-500/30 bg-red-500/10 px-3.5 py-2 text-xs font-medium text-red-300 transition-colors hover:bg-red-500/20 disabled:opacity-50"
              >
                <Trash2 className="h-3.5 w-3.5" />
                <span>{t('profile.removeAvatar')}</span>
              </button>
            )}
            <button
              type="button"
              onClick={() => {
                logout()
                navigate('/login')
              }}
              className="inline-flex items-center gap-1.5 rounded-xl border border-white/15 bg-white/[0.04] px-3.5 py-2 text-xs font-medium text-zinc-200 transition-colors hover:border-red-500/40 hover:bg-red-500/15 hover:text-red-300"
            >
              <LogOut className="h-3.5 w-3.5" />
              <span>{t('nav.logout')}</span>
            </button>
          </div>
        </div>
      </section>

      {/* 界面语言设置 */}
      <section className="rounded-2xl border border-white/10 bg-zinc-950/70 p-6 backdrop-blur-md">
        <div className="mb-1.5 flex items-center gap-2">
          <Globe className="h-4 w-4 text-amber-400" />
          <h2 className="text-base font-semibold text-white">{t('profile.uiLanguage')}</h2>
        </div>
        <p className="mb-4 text-xs text-zinc-400">{t('profile.uiLanguageDesc')}</p>
        <div className="flex flex-wrap gap-2" role="radiogroup" aria-label={t('profile.uiLanguage')}>
          {SUPPORTED_LOCALES.map((item) => {
            const active = item.code === locale
            return (
              <button
                key={item.code}
                type="button"
                role="radio"
                aria-checked={active}
                onClick={() => handleSelectLocale(item.code)}
                className={`inline-flex items-center gap-2 rounded-xl border px-3.5 py-2 text-xs font-medium transition-colors ${
                  active
                    ? 'border-amber-400/60 bg-amber-500/20 text-amber-200 ring-1 ring-amber-400/30'
                    : 'border-white/10 bg-white/[0.03] text-zinc-300 hover:border-white/20 hover:bg-white/[0.07] hover:text-zinc-100'
                }`}
              >
                <span>{item.nativeName}</span>
                <span className="text-[10px] text-zinc-500">{item.code}</span>
              </button>
            )
          })}
        </div>
      </section>

      <div className="grid gap-6 md:grid-cols-2">
        {/* 修改密码 */}
        <section className="rounded-2xl border border-white/10 bg-zinc-950/70 p-6 backdrop-blur-md">
          <div className="mb-4 flex items-center gap-2">
            <KeyRound className="h-4 w-4 text-amber-400" />
            <h2 className="text-base font-semibold text-white">{t('profile.changePassword')}</h2>
          </div>

          <form onSubmit={handlePasswordSubmit} className="space-y-4">
            <label className="block space-y-1.5">
              <span className="text-xs font-medium text-zinc-300">{t('profile.currentPassword')}</span>
              <input
                type="password"
                value={currentPw}
                onChange={(e) => setCurrentPw(e.target.value)}
                placeholder={t('profile.currentPasswordPlaceholder')}
                autoComplete="current-password"
                className="w-full rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2.5 text-sm text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
              />
            </label>

            <label className="block space-y-1.5">
              <span className="text-xs font-medium text-zinc-300">{t('profile.newPassword')}</span>
              <input
                type="password"
                value={newPw}
                onChange={(e) => setNewPw(e.target.value)}
                placeholder={t('profile.newPasswordPlaceholder')}
                autoComplete="new-password"
                className="w-full rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2.5 text-sm text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
              />
            </label>

            <label className="block space-y-1.5">
              <span className="text-xs font-medium text-zinc-300">{t('profile.confirmNewPassword')}</span>
              <input
                type="password"
                value={confirmPw}
                onChange={(e) => setConfirmPw(e.target.value)}
                placeholder={t('profile.confirmNewPasswordPlaceholder')}
                autoComplete="new-password"
                className="w-full rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2.5 text-sm text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
              />
            </label>

            <button
              type="submit"
              disabled={passwordMutation.isPending}
              className="inline-flex w-full items-center justify-center gap-2 rounded-xl bg-amber-500 px-4 py-2.5 text-sm font-semibold text-zinc-950 transition-colors hover:bg-amber-400 disabled:opacity-50"
            >
              {passwordMutation.isPending ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <CheckCircle2 className="h-4 w-4" />
              )}
              <span>{t('profile.saveNewPassword')}</span>
            </button>
          </form>
        </section>

        {/* 本地播放偏好 */}
        <section className="rounded-2xl border border-white/10 bg-zinc-950/70 p-6 backdrop-blur-md">
          <div className="mb-4 flex items-center gap-2">
            <Sliders className="h-4 w-4 text-amber-400" />
            <h2 className="text-base font-semibold text-white">{t('profile.playbackPrefs')}</h2>
          </div>

          <div className="space-y-5">
            <div className="flex items-start justify-between gap-4 rounded-xl border border-white/8 bg-white/[0.03] p-3.5">
              <div>
                <p className="text-sm font-medium text-zinc-100">{t('profile.autoPlayNextTitle')}</p>
                <p className="mt-0.5 text-xs text-zinc-400">{t('profile.autoPlayNextDesc')}</p>
              </div>
              <button
                type="button"
                role="switch"
                aria-label={t('profile.autoPlayNextTitle')}
                aria-checked={prefs.autoPlayNextEpisode}
                onClick={() =>
                  handlePrefsChange({ autoPlayNextEpisode: !prefs.autoPlayNextEpisode })
                }
                className={`mt-0.5 inline-flex h-6 w-11 shrink-0 items-center rounded-full p-0.5 transition-colors ${
                  prefs.autoPlayNextEpisode ? 'bg-amber-500' : 'bg-zinc-700'
                }`}
              >
                <span
                  className={`h-5 w-5 rounded-full bg-white shadow transition-transform ${
                    prefs.autoPlayNextEpisode ? 'translate-x-5' : 'translate-x-0'
                  }`}
                />
              </button>
            </div>

            <label className="block space-y-1.5">
              <span className="text-xs font-medium text-zinc-300">{t('profile.preferredAudioLang')}</span>
              <select
                value={prefs.preferredAudioLang}
                onChange={(e) => handlePrefsChange({ preferredAudioLang: e.target.value })}
                className="w-full rounded-xl border border-white/10 bg-zinc-900 px-3.5 py-2.5 text-sm text-zinc-100 outline-none focus:border-amber-400/60"
              >
                {AUDIO_LANG_OPTIONS.map((opt) => (
                  <option key={opt.value} value={opt.value}>
                    {t(opt.labelKey)}
                  </option>
                ))}
              </select>
            </label>

            <label className="block space-y-1.5">
              <span className="text-xs font-medium text-zinc-300">{t('profile.preferredSubtitleLang')}</span>
              <select
                value={prefs.preferredSubtitleLang}
                onChange={(e) => handlePrefsChange({ preferredSubtitleLang: e.target.value })}
                className="w-full rounded-xl border border-white/10 bg-zinc-900 px-3.5 py-2.5 text-sm text-zinc-100 outline-none focus:border-amber-400/60"
              >
                {SUBTITLE_LANG_OPTIONS.map((opt) => (
                  <option key={opt.value} value={opt.value}>
                    {t(opt.labelKey)}
                  </option>
                ))}
              </select>
            </label>

            <label className="block space-y-1.5">
              <span className="text-xs font-medium text-zinc-300">{t('profile.subtitleDisplayMode')}</span>
              <select
                value={prefs.subtitleMode}
                onChange={(e) =>
                  handlePrefsChange({ subtitleMode: e.target.value as SubtitleModeOption })
                }
                className="w-full rounded-xl border border-white/10 bg-zinc-900 px-3.5 py-2.5 text-sm text-zinc-100 outline-none focus:border-amber-400/60"
              >
                {SUBTITLE_MODE_OPTIONS.map((opt) => (
                  <option key={opt.value} value={opt.value}>
                    {t(opt.labelKey)}
                  </option>
                ))}
              </select>
            </label>
          </div>
        </section>
      </div>

      {/* Quick Connect 授权新设备 */}
      <TraktSettings key={userId} userId={userId} />

      <section className="rounded-2xl border border-white/10 bg-zinc-950/70 p-6 backdrop-blur-md">
        <div className="mb-4 flex items-center gap-2">
          <QrCode className="h-4 w-4 text-amber-400" />
          <h2 className="text-base font-semibold text-white">
            {locale === 'zh-CN' ? 'Quick Connect 授权新设备' : t('profile.quickConnect')}
          </h2>
        </div>
        <p className="mb-4 text-xs text-zinc-400">{t('profile.qcDesc')}</p>

        <form onSubmit={handleQuickConnectSubmit} className="space-y-3.5">
          <div className="flex flex-col gap-3 sm:flex-row sm:items-end">
            <label className="block flex-1 space-y-1.5">
              <span className="text-xs font-medium text-zinc-300">{t('profile.qcCodeLabel')}</span>
              <input
                type="text"
                inputMode="numeric"
                maxLength={12}
                value={qcCode}
                onChange={(e) => {
                  setQcCode(e.target.value)
                  if (qcFeedback) setQcFeedback(null)
                }}
                placeholder={t('profile.qcCodePlaceholder')}
                className="w-full rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2.5 font-mono text-sm tracking-[0.25em] text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
              />
            </label>

            <button
              type="submit"
              disabled={quickConnectMutation.isPending}
              className="inline-flex items-center justify-center gap-2 rounded-xl bg-amber-500 px-5 py-2.5 text-sm font-semibold text-zinc-950 transition-colors hover:bg-amber-400 disabled:opacity-50 sm:shrink-0"
            >
              {quickConnectMutation.isPending ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <CheckCircle2 className="h-4 w-4" />
              )}
              <span>{t('profile.qcConfirmBtn')}</span>
            </button>
          </div>

          {qcFeedback && (
            <div
              role="status"
              className={`flex items-start gap-2 rounded-xl border px-3.5 py-2.5 text-xs ${
                qcFeedback.type === 'success'
                  ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-300'
                  : 'border-red-500/30 bg-red-500/10 text-red-300'
              }`}
            >
              {qcFeedback.type === 'success' ? (
                <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0" />
              ) : (
                <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
              )}
              <span>{qcFeedback.message}</span>
            </div>
          )}
        </form>
      </section>
    </div>
  )
}
