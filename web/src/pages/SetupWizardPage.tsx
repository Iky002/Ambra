import { useEffect, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  AlertCircle,
  ArrowLeft,
  ArrowRight,
  ArrowUp,
  Check,
  CheckCircle2,
  ChevronRight,
  Film,
  Folder,
  FolderPlus,
  Globe,
  HardDrive,
  KeyRound,
  Loader2,
  Lock,
  LogIn,
  Music,
  Plus,
  Server,
  ShieldCheck,
  Sparkles,
  Tv,
  User,
} from 'lucide-react'
import { Link, Navigate, useNavigate } from 'react-router-dom'
import { api } from '../api/client'
import { AmbraLogo } from '../components/AmbraLogo'
import { LanguageSwitcher } from '../components/LanguageSwitcher'
import { useAuth } from '../context/AuthContext'
import { useI18n } from '../i18n/I18nContext'
import { AdminModal, ToggleSwitch } from './admin/adminUtils'

type WizardStep = 1 | 2 | 3 | 4

interface AddedLibraryPreview {
  id: string
  name: string
  collectionType: string
  paths: string[]
}

const STEPS: { id: WizardStep; titleKey: string; subtitleKey: string; icon: typeof Server }[] = [
  { id: 1, titleKey: 'setupUi.step1NavTitle', subtitleKey: 'setupUi.step1NavSub', icon: Server },
  { id: 2, titleKey: 'setupUi.step2NavTitle', subtitleKey: 'setupUi.step2NavSub', icon: ShieldCheck },
  { id: 3, titleKey: 'setupUi.step3NavTitle', subtitleKey: 'setupUi.step3NavSub', icon: FolderPlus },
  { id: 4, titleKey: 'setupUi.step4NavTitle', subtitleKey: 'setupUi.step4NavSub', icon: Sparkles },
]

const LANGUAGE_OPTIONS = [
  { value: 'zh-CN', label: '简体中文 (zh-CN)' },
  { value: 'zh-TW', label: '繁體中文 (zh-TW)' },
  { value: 'en-US', label: 'English (en-US)' },
  { value: 'ja-JP', label: '日本語 (ja-JP)' },
  { value: 'ko-KR', label: '한국어 (ko-KR)' },
  { value: 'es-ES', label: 'Español (es-ES)' },
  { value: 'de-DE', label: 'Deutsch (de-DE)' },
  { value: 'fr-FR', label: 'Français (fr-FR)' },
  { value: 'pt-BR', label: 'Português (pt-BR)' },
  { value: 'th-TH', label: 'ไทย (th-TH)' },
]

const COUNTRY_OPTIONS = [
  { value: 'CN', labelKey: 'setupUi.countryCN' },
  { value: 'HK', labelKey: 'setupUi.countryHK' },
  { value: 'TW', labelKey: 'setupUi.countryTW' },
  { value: 'US', labelKey: 'setupUi.countryUS' },
  { value: 'JP', labelKey: 'setupUi.countryJP' },
  { value: 'KR', labelKey: 'setupUi.countryKR' },
  { value: 'ES', labelKey: 'setupUi.countryES' },
  { value: 'DE', labelKey: 'setupUi.countryDE' },
  { value: 'FR', labelKey: 'setupUi.countryFR' },
  { value: 'BR', labelKey: 'setupUi.countryBR' },
  { value: 'TH', labelKey: 'setupUi.countryTH' },
]

const COLLECTION_TYPES = [
  { value: 'movies', labelKey: 'setupUi.colTypeMovies', icon: Film },
  { value: 'tvshows', labelKey: 'setupUi.colTypeTvShows', icon: Tv },
  { value: 'music', labelKey: 'setupUi.colTypeMusic', icon: Music },
  { value: 'mixed', labelKey: 'setupUi.colTypeMixed', icon: Folder },
]

function getParentPath(path: string): string {
  const normalized = path.replace(/\\/g, '/').replace(/\/+$/, '')
  if (!normalized || normalized === '') return ''
  if (/^[A-Za-z]:$/.test(normalized)) return ''
  const idx = normalized.lastIndexOf('/')
  if (idx < 0) return ''
  if (idx === 0) return '/'
  const parent = normalized.slice(0, idx)
  if (/^[A-Za-z]:$/.test(parent)) return `${parent}/`
  return parent
}

export function SetupWizardPage() {
  const { auth, isAdmin, userLoading, applyAuthResult, logout } = useAuth()
  const { t } = useI18n()
  const navigate = useNavigate()
  const queryClient = useQueryClient()

  const handleSwitchAdminLogin = () => {
    logout()
    navigate('/login?redirect=/setup&switch=1', { replace: true })
  }

  const [step, setStep] = useState<WizardStep>(1)
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const skippedSetupAdminRef = useRef(false)

  // Step 1: 服务器设置
  const [serverName, setServerName] = useState('Ambra')
  const [serverNameTouched, setServerNameTouched] = useState(false)
  const [language, setLanguage] = useState('zh-CN')
  const [languageTouched, setLanguageTouched] = useState(false)
  const [country, setCountry] = useState('CN')

  // Step 2: 管理员账号
  const [username, setUsername] = useState('admin')
  const [password, setPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')

  // Step 3: 添加媒体库
  const [addedLibraries, setAddedLibraries] = useState<AddedLibraryPreview[]>([])
  const [libName, setLibName] = useState('')
  const [libType, setLibType] = useState('movies')
  const [libPath, setLibPath] = useState('')
  const [saveLocalMetadata, setSaveLocalMetadata] = useState(true)
  const [addingLibrary, setAddingLibrary] = useState(false)
  const [libraryFeedback, setLibraryFeedback] = useState('')

  // 目录浏览器弹窗
  const [browserOpen, setBrowserOpen] = useState(false)
  const [browsePath, setBrowsePath] = useState('')

  // Step 4: 刮削与偏好设置
  const [tmdbApiKey, setTmdbApiKey] = useState('')
  const [tmdbLanguage, setTmdbLanguage] = useState('zh-CN')
  const [tmdbLangCustomized, setTmdbLangCustomized] = useState(false)
  const [enableStrm302, setEnableStrm302] = useState(true)
  const [triggerScan, setTriggerScan] = useState(false)
  const [syncRankings, setSyncRankings] = useState(true)

  const { data: setupStatus } = useQuery({
    queryKey: ['setupStatus'],
    queryFn: () => api.getSetupStatus(),
    retry: false,
  })

  useEffect(() => {
    if (!setupStatus) return
    if (!serverNameTouched && setupStatus.server_name) {
      setServerName(setupStatus.server_name)
    }
    if (
      !languageTouched &&
      setupStatus.default_language &&
      LANGUAGE_OPTIONS.some((opt) => opt.value === setupStatus.default_language)
    ) {
      setLanguage(setupStatus.default_language)
      if (!tmdbLangCustomized) {
        setTmdbLanguage(setupStatus.default_language)
      }
    }
  }, [setupStatus, serverNameTouched, languageTouched, tmdbLangCustomized])

  const {
    data: drives = [],
    isLoading: drivesLoading,
    error: drivesError,
  } = useQuery({
    queryKey: ['setup', 'env', 'drives'],
    queryFn: () => api.getDrives(),
    enabled: isAdmin && browserOpen && browsePath === '',
  })

  const {
    data: dirEntries = [],
    isLoading: dirLoading,
    error: dirError,
  } = useQuery({
    queryKey: ['setup', 'env', 'dir', browsePath],
    queryFn: () => api.getDirectoryContents(browsePath, true, false),
    enabled: isAdmin && browserOpen && browsePath !== '',
  })

  const nonAdminBlocked = Boolean(auth && !userLoading && !isAdmin)
  const adminRequiredForStep =
    step >= 3 || (step === 2 && setupStatus?.has_users === true)
  const blockedByRole = adminRequiredForStep && (!isAdmin && (!userLoading || !auth))

  if (setupStatus?.completed === true) {
    return <Navigate to={auth ? '/' : '/login'} replace />
  }

  const handleStep1Next = (e: React.FormEvent) => {
    e.preventDefault()
    if (!serverName.trim()) {
      setError(t('setupUi.errServerNameRequired'))
      return
    }
    setError('')
    if (!tmdbLangCustomized) {
      setTmdbLanguage(language)
    }
    setStep(2)
  }

  const handleStep2SkipWithExistingAdmin = async () => {
    if (!isAdmin) return
    setError('')
    setSubmitting(true)
    skippedSetupAdminRef.current = true
    try {
      await api.updateServerConfig({
        ServerName: serverName.trim() || 'Ambra',
        TMDBLanguage: language,
        PreferredMetadataLanguage: language,
        MetadataCountryCode: country,
      })
      setStep(3)
    } catch (err) {
      setError(err instanceof Error ? err.message : t('setupUi.errSaveLangCountryFailed'))
    } finally {
      setSubmitting(false)
    }
  }

  const handleStep2Submit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (setupStatus?.has_users === true && !isAdmin) {
      setError(t('setupUi.errUsersExistLoginAdmin'))
      return
    }
    const trimmedUser = username.trim()
    if (!trimmedUser) {
      setError(t('setupUi.errAdminUsernameRequired'))
      return
    }
    if (!password) {
      setError(t('setupUi.errAdminPasswordRequired'))
      return
    }
    if (password.length < 6) {
      setError(t('setupUi.errAdminPasswordTooShort'))
      return
    }
    if (password !== confirmPassword) {
      setError(t('setupUi.errPasswordMismatch'))
      return
    }

    setError('')
    setSubmitting(true)
    skippedSetupAdminRef.current = false
    try {
      const res = await api.setupAdmin({
        server_name: serverName.trim(),
        language,
        country,
        username: trimmedUser,
        password,
      })
      applyAuthResult(res)
      setStep(3)
    } catch (err) {
      setError(err instanceof Error ? err.message : t('setupUi.errCreateAdminFailed'))
    } finally {
      setSubmitting(false)
    }
  }

  const handleAddLibrary = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!isAdmin) {
      setError(t('setupUi.errAdminOnlyLibrary'))
      return
    }
    const trimmedName = libName.trim()
    const trimmedPath = libPath.trim()
    if (!trimmedName) {
      setError(t('setupUi.errLibNameRequired'))
      return
    }
    if (!trimmedPath) {
      setError(t('setupUi.errLibPathRequired'))
      return
    }

    setError('')
    setLibraryFeedback('')
    setAddingLibrary(true)
    try {
      const created = await api.createVirtualFolder({
        Name: trimmedName,
        CollectionType: libType,
        Paths: [trimmedPath],
        refreshLibrary: false,
        saveLocalMetadata,
      })
      setAddedLibraries((prev) => [
        ...prev,
        {
          id: created?.ItemId || `lib-${Date.now()}`,
          name: trimmedName,
          collectionType: libType,
          paths: [trimmedPath],
        },
      ])
      setTriggerScan(true)
      setLibName('')
      setLibPath('')
      setLibraryFeedback(t('setupUi.libAddedFeedback', { name: trimmedName }))
    } catch (err) {
      setError(err instanceof Error ? err.message : t('setupUi.errAddLibFailed'))
    } finally {
      setAddingLibrary(false)
    }
  }

  const handleStep3Next = () => {
    setError('')
    setLibraryFeedback('')
    setTriggerScan(addedLibraries.length > 0)
    setStep(4)
  }

  const handleCompleteSetup = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!isAdmin) {
      setError(t('setupUi.errAdminOnlyComplete'))
      return
    }
    setError('')
    setSubmitting(true)
    try {
      if (skippedSetupAdminRef.current) {
        await api.updateServerConfig({
          ServerName: serverName.trim() || 'Ambra',
          TMDBLanguage: tmdbLanguage || language,
          PreferredMetadataLanguage: language,
          MetadataCountryCode: country,
        })
      }
      await api.completeSetup({
        server_name: serverName.trim() || 'Ambra',
        ...(skippedSetupAdminRef.current ? { language, country } : {}),
        tmdb_api_key: tmdbApiKey.trim() || undefined,
        tmdb_language: tmdbLanguage,
        enable_strm_302: enableStrm302,
        trigger_scan: triggerScan,
        sync_rankings: syncRankings,
      })
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['setupStatus'] }),
        queryClient.invalidateQueries({ queryKey: ['publicSystemInfo'] }),
        queryClient.invalidateQueries({ queryKey: ['userViews'] }),
      ])
      navigate('/', { replace: true })
    } catch (err) {
      setError(err instanceof Error ? err.message : t('setupUi.errCompleteFailed'))
    } finally {
      setSubmitting(false)
    }
  }

  const folderEntries = browsePath === '' ? drives : dirEntries
  const browserLoading = browsePath === '' ? drivesLoading : dirLoading
  const browserError = browsePath === '' ? drivesError : dirError

  return (
    <div className="relative flex min-h-screen items-center justify-center overflow-hidden bg-[#07070b] px-4 py-10">
      {/* 右上角语言切换 */}
      <div className="absolute right-4 top-4 z-20 sm:right-6 sm:top-6">
        <LanguageSwitcher />
      </div>

      {/* 影院氛围背景光晕 */}
      <div className="pointer-events-none absolute -left-32 -top-32 h-96 w-96 rounded-full bg-amber-500/15 blur-[130px]" />
      <div className="pointer-events-none absolute -bottom-32 -right-32 h-96 w-96 rounded-full bg-orange-600/15 blur-[130px]" />

      <div className="relative z-10 w-full max-w-2xl">
        {/* 顶部品牌区 */}
        <div className="mb-7 text-center">
          <div className="mb-3.5 flex justify-center">
            <AmbraLogo size={54} />
          </div>
          <h1 className="text-2xl font-bold tracking-tight text-zinc-50">{t('setup.title')}</h1>
          <p className="mt-1.5 text-xs text-zinc-400">{t('setupUi.wizardBannerSubtitle')}</p>
        </div>

        {/* 4 步水平进度条 */}
        <div className="mb-6 grid grid-cols-4 gap-2 sm:gap-3" aria-label={t('setupUi.stepsAria')}>
          {STEPS.map((s) => {
            const Icon = s.icon
            const isDone = step > s.id
            const isCurrent = step === s.id
            return (
              <div
                key={s.id}
                className={`flex flex-col items-center rounded-2xl border px-2 py-3 text-center transition-all ${
                  isCurrent
                    ? 'border-amber-400/50 bg-amber-500/12 shadow-lg shadow-amber-500/10'
                    : isDone
                      ? 'border-emerald-500/30 bg-emerald-500/[0.07]'
                      : 'border-white/8 bg-white/[0.02]'
                }`}
              >
                <div
                  className={`mb-1.5 flex h-8 w-8 items-center justify-center rounded-full text-xs font-semibold transition-colors ${
                    isCurrent
                      ? 'bg-gradient-to-br from-amber-400 to-orange-500 text-zinc-950'
                      : isDone
                        ? 'bg-emerald-500/20 text-emerald-300 ring-1 ring-emerald-400/40'
                        : 'bg-white/[0.06] text-zinc-500'
                  }`}
                >
                  {isDone ? <Check className="h-4 w-4" /> : <Icon className="h-4 w-4" />}
                </div>
                <span
                  className={`text-xs font-semibold ${
                    isCurrent ? 'text-amber-200' : isDone ? 'text-emerald-200' : 'text-zinc-400'
                  }`}
                >
                  {t(s.titleKey)}
                </span>
                <span className="mt-0.5 hidden text-[10px] text-zinc-500 sm:block">
                  {t(s.subtitleKey)}
                </span>
              </div>
            )
          })}
        </div>

        {/* 非管理员已登录时提示无权继续初始化管理步骤 */}
        {nonAdminBlocked && (
          <div
            role="alert"
            className="mb-5 flex flex-wrap items-center justify-between gap-3 rounded-2xl border border-red-500/35 bg-red-500/10 px-4 py-3 text-xs text-red-200"
          >
            <div className="min-w-0 flex-1">
              <p className="font-semibold text-red-100">{t('setupUi.nonAdminAlertTitle')}</p>
              <p className="mt-0.5 text-[11px] text-red-200/80">{t('setupUi.nonAdminAlertDesc')}</p>
            </div>
            <button
              type="button"
              onClick={handleSwitchAdminLogin}
              className="inline-flex shrink-0 items-center gap-1.5 rounded-xl bg-amber-500 px-3.5 py-2 text-xs font-semibold text-zinc-950 transition-colors hover:bg-amber-400"
            >
              <LogIn className="h-3.5 w-3.5" />
              <span>{t('setupUi.switchAdminLoginBtn')}</span>
            </button>
          </div>
        )}

        {/* 已有管理员账号但未登录时，提供登录入口防止向导死锁 */}
        {setupStatus?.has_users === true && !auth && (
          <div className="mb-5 flex flex-wrap items-center justify-between gap-3 rounded-2xl border border-amber-500/30 bg-amber-500/10 px-4 py-3 text-xs text-amber-200">
            <div className="min-w-0 flex-1">
              <p className="font-semibold text-amber-100">{t('setupUi.existingAdminAlertTitle')}</p>
              <p className="mt-0.5 text-[11px] text-amber-200/80">
                {t('setupUi.existingAdminAlertDesc')}
              </p>
            </div>
            <Link
              to="/login?redirect=/setup"
              className="inline-flex shrink-0 items-center gap-1.5 rounded-xl bg-amber-500 px-3.5 py-2 text-xs font-semibold text-zinc-950 transition-colors hover:bg-amber-400"
            >
              <LogIn className="h-3.5 w-3.5" />
              <span>{t('setupUi.loginExistingAdminBtn')}</span>
            </Link>
          </div>
        )}

        {/* 主卡片容器 */}
        <div className="rounded-2xl border border-white/10 bg-zinc-950/85 p-6 shadow-2xl backdrop-blur-xl sm:p-8">
          {error && (
            <div
              role="alert"
              className="mb-5 flex items-start gap-2 rounded-xl border border-red-500/30 bg-red-500/10 px-3.5 py-2.5 text-xs text-red-300"
            >
              <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
              <span>{error}</span>
            </div>
          )}

          {/* ==================== Step 1: 服务器设置 ==================== */}
          {step === 1 && (
            <form onSubmit={handleStep1Next} className="space-y-5">
              <div>
                <h2 className="text-lg font-semibold text-zinc-100">{t('setupUi.step1Heading')}</h2>
                <p className="mt-1 text-xs text-zinc-400">{t('setupUi.step1Desc')}</p>
              </div>

              <div className="space-y-4">
                <div>
                  <label
                    htmlFor="setup-server-name"
                    className="mb-1.5 block text-xs font-medium text-zinc-400"
                  >
                    {t('setup.serverName')}
                  </label>
                  <div className="relative">
                    <Server className="pointer-events-none absolute left-3.5 top-1/2 h-4 w-4 -translate-y-1/2 text-zinc-500" />
                    <input
                      id="setup-server-name"
                      type="text"
                      value={serverName}
                      onChange={(e) => {
                        setServerName(e.target.value)
                        setServerNameTouched(true)
                      }}
                      placeholder={t('setupUi.serverNamePlaceholder')}
                      className="w-full rounded-xl border border-white/10 bg-white/[0.04] py-2.5 pl-10 pr-4 text-sm text-zinc-100 placeholder-zinc-500 outline-none transition-colors focus:border-amber-400/60 focus:bg-white/[0.06]"
                    />
                  </div>
                </div>

                <div className="grid gap-4 sm:grid-cols-2">
                  <div>
                    <label
                      htmlFor="setup-language"
                      className="mb-1.5 block text-xs font-medium text-zinc-400"
                    >
                      {t('setupUi.preferredScrapeLang')}
                    </label>
                    <div className="relative">
                      <Globe className="pointer-events-none absolute left-3.5 top-1/2 h-4 w-4 -translate-y-1/2 text-zinc-500" />
                      <select
                        id="setup-language"
                        value={language}
                        onChange={(e) => {
                          setLanguage(e.target.value)
                          setLanguageTouched(true)
                        }}
                        className="w-full rounded-xl border border-white/10 bg-[#14141d] py-2.5 pl-10 pr-4 text-sm text-zinc-100 outline-none transition-colors focus:border-amber-400/60"
                      >
                        {LANGUAGE_OPTIONS.map((opt) => (
                          <option key={opt.value} value={opt.value}>
                            {opt.label}
                          </option>
                        ))}
                      </select>
                    </div>
                  </div>

                  <div>
                    <label
                      htmlFor="setup-country"
                      className="mb-1.5 block text-xs font-medium text-zinc-400"
                    >
                      {t('setupUi.countryRegionLabel')}
                    </label>
                    <select
                      id="setup-country"
                      value={country}
                      onChange={(e) => setCountry(e.target.value)}
                      className="w-full rounded-xl border border-white/10 bg-[#14141d] px-3.5 py-2.5 text-sm text-zinc-100 outline-none transition-colors focus:border-amber-400/60"
                    >
                      {COUNTRY_OPTIONS.map((opt) => (
                        <option key={opt.value} value={opt.value}>
                          {t(opt.labelKey)}
                        </option>
                      ))}
                    </select>
                  </div>
                </div>
              </div>

              <div className="flex justify-end pt-2">
                <button
                  type="submit"
                  className="inline-flex items-center gap-2 rounded-xl bg-gradient-to-r from-amber-500 to-orange-500 px-6 py-2.5 text-sm font-semibold text-zinc-950 shadow-lg shadow-amber-500/25 transition-all hover:from-amber-400 hover:to-orange-400"
                >
                  <span>{t('setupUi.nextCreateAdminBtn')}</span>
                  <ArrowRight className="h-4 w-4" />
                </button>
              </div>
            </form>
          )}

          {/* ==================== Step 2: 管理员账号 ==================== */}
          {step === 2 && (
            <form onSubmit={handleStep2Submit} className="space-y-5">
              <div>
                <h2 className="text-lg font-semibold text-zinc-100">{t('setupUi.step2Heading')}</h2>
                <p className="mt-1 text-xs text-zinc-400">{t('setupUi.step2Desc')}</p>
              </div>

              <div className="space-y-4">
                <div>
                  <label
                    htmlFor="setup-admin-username"
                    className="mb-1.5 block text-xs font-medium text-zinc-400"
                  >
                    {t('setupUi.adminUsernameLabel')}
                  </label>
                  <div className="relative">
                    <User className="pointer-events-none absolute left-3.5 top-1/2 h-4 w-4 -translate-y-1/2 text-zinc-500" />
                    <input
                      id="setup-admin-username"
                      type="text"
                      autoComplete="username"
                      value={username}
                      onChange={(e) => setUsername(e.target.value)}
                      placeholder={t('setupUi.adminUsernamePlaceholder')}
                      className="w-full rounded-xl border border-white/10 bg-white/[0.04] py-2.5 pl-10 pr-4 text-sm text-zinc-100 placeholder-zinc-500 outline-none transition-colors focus:border-amber-400/60 focus:bg-white/[0.06]"
                    />
                  </div>
                </div>

                <div className="grid gap-4 sm:grid-cols-2">
                  <div>
                    <label
                      htmlFor="setup-admin-password"
                      className="mb-1.5 block text-xs font-medium text-zinc-400"
                    >
                      {t('setupUi.adminPasswordLabel')}
                    </label>
                    <div className="relative">
                      <Lock className="pointer-events-none absolute left-3.5 top-1/2 h-4 w-4 -translate-y-1/2 text-zinc-500" />
                      <input
                        id="setup-admin-password"
                        type="password"
                        autoComplete="new-password"
                        value={password}
                        onChange={(e) => setPassword(e.target.value)}
                        placeholder={t('setupUi.adminPasswordPlaceholder')}
                        className="w-full rounded-xl border border-white/10 bg-white/[0.04] py-2.5 pl-10 pr-4 text-sm text-zinc-100 placeholder-zinc-500 outline-none transition-colors focus:border-amber-400/60 focus:bg-white/[0.06]"
                      />
                    </div>
                  </div>

                  <div>
                    <label
                      htmlFor="setup-admin-confirm"
                      className="mb-1.5 block text-xs font-medium text-zinc-400"
                    >
                      {t('setupUi.confirmPasswordLabel')}
                    </label>
                    <div className="relative">
                      <Lock className="pointer-events-none absolute left-3.5 top-1/2 h-4 w-4 -translate-y-1/2 text-zinc-500" />
                      <input
                        id="setup-admin-confirm"
                        type="password"
                        autoComplete="new-password"
                        value={confirmPassword}
                        onChange={(e) => setConfirmPassword(e.target.value)}
                        placeholder={t('setupUi.confirmPasswordPlaceholder')}
                        className="w-full rounded-xl border border-white/10 bg-white/[0.04] py-2.5 pl-10 pr-4 text-sm text-zinc-100 placeholder-zinc-500 outline-none transition-colors focus:border-amber-400/60 focus:bg-white/[0.06]"
                      />
                    </div>
                  </div>
                </div>
              </div>

              <div className="flex items-center justify-between pt-2">
                <button
                  type="button"
                  disabled={submitting}
                  onClick={() => {
                    setError('')
                    setStep(1)
                  }}
                  className="inline-flex items-center gap-1.5 rounded-xl border border-white/10 bg-white/[0.04] px-4 py-2.5 text-xs font-medium text-zinc-300 transition-colors hover:bg-white/[0.08] disabled:opacity-50"
                >
                  <ArrowLeft className="h-4 w-4" />
                  <span>{t('setupUi.prevStepBtn')}</span>
                </button>

                <div className="flex flex-wrap items-center gap-2">
                  {setupStatus?.has_users === true && !auth && (
                    <Link
                      to="/login?redirect=/setup"
                      className="inline-flex items-center gap-1.5 rounded-xl border border-amber-400/40 bg-amber-500/15 px-4 py-2.5 text-xs font-semibold text-amber-200 transition-colors hover:bg-amber-500/25"
                    >
                      <LogIn className="h-3.5 w-3.5" />
                      <span>{t('setupUi.loginExistingAdminBtn')}</span>
                    </Link>
                  )}
                  {setupStatus?.has_users === true && auth && isAdmin && (
                    <button
                      type="button"
                      disabled={submitting}
                      onClick={() => void handleStep2SkipWithExistingAdmin()}
                      className="inline-flex items-center gap-1.5 rounded-xl border border-emerald-400/40 bg-emerald-500/15 px-4 py-2.5 text-xs font-semibold text-emerald-200 transition-colors hover:bg-emerald-500/25 disabled:opacity-50"
                    >
                      <span>{t('setupUi.loggedInSkipBtn')}</span>
                      <ArrowRight className="h-3.5 w-3.5" />
                    </button>
                  )}
                  <button
                    type="submit"
                    disabled={submitting || (setupStatus?.has_users === true && !isAdmin)}
                    className="inline-flex items-center gap-2 rounded-xl bg-gradient-to-r from-amber-500 to-orange-500 px-6 py-2.5 text-sm font-semibold text-zinc-950 shadow-lg shadow-amber-500/25 transition-all hover:from-amber-400 hover:to-orange-400 disabled:opacity-60"
                  >
                    {submitting ? (
                      <>
                        <Loader2 className="h-4 w-4 animate-spin" />
                        <span>{t('setupUi.creatingAdmin')}</span>
                      </>
                    ) : (
                      <>
                        <span>{t('setupUi.createAndContinueBtn')}</span>
                        <ArrowRight className="h-4 w-4" />
                      </>
                    )}
                  </button>
                </div>
              </div>
            </form>
          )}

          {/* ==================== 非管理员访问 Step 3 / Step 4 权限拦截 ==================== */}
          {step >= 3 && blockedByRole && (
            <div className="space-y-4 py-6 text-center">
              <div className="mx-auto flex h-12 w-12 items-center justify-center rounded-2xl bg-red-500/15 text-red-400 ring-1 ring-red-500/30">
                <ShieldCheck className="h-6 w-6" />
              </div>
              <h2 className="text-base font-semibold text-zinc-100">
                {t('setupUi.adminRequiredTitle')}
              </h2>
              <p className="mx-auto max-w-md text-xs leading-relaxed text-zinc-400">
                {t('setupUi.adminRequiredDesc')}
              </p>
              <div className="flex items-center justify-center gap-3 pt-2">
                <button
                  type="button"
                  onClick={handleSwitchAdminLogin}
                  className="inline-flex items-center gap-1.5 rounded-xl bg-amber-500 px-5 py-2.5 text-xs font-semibold text-zinc-950 hover:bg-amber-400"
                >
                  <LogIn className="h-3.5 w-3.5" />
                  <span>{t('setupUi.switchAdminLoginBtn')}</span>
                </button>
                <Link
                  to="/"
                  className="inline-flex items-center gap-1.5 rounded-xl border border-white/10 bg-white/[0.04] px-4 py-2.5 text-xs font-medium text-zinc-300 hover:bg-white/[0.08]"
                >
                  <span>{t('auth.backHome')}</span>
                </Link>
              </div>
            </div>
          )}

          {/* ==================== Step 3: 添加媒体库 ==================== */}
          {step === 3 && !blockedByRole && (
            <div className="space-y-5">
              <div>
                <h2 className="text-lg font-semibold text-zinc-100">{t('setupUi.step3Heading')}</h2>
                <p className="mt-1 text-xs text-zinc-400">{t('setupUi.step3Desc')}</p>
              </div>

              {libraryFeedback && (
                <div className="flex items-center gap-2 rounded-xl border border-emerald-500/30 bg-emerald-500/10 px-3.5 py-2.5 text-xs text-emerald-300">
                  <CheckCircle2 className="h-4 w-4 shrink-0" />
                  <span>{libraryFeedback}</span>
                </div>
              )}

              {/* 已添加媒体库预览列表 */}
              {addedLibraries.length > 0 && (
                <div className="space-y-2">
                  <p className="text-xs font-medium text-zinc-400">
                    {t('setupUi.addedLibsHeader', { count: addedLibraries.length })}
                  </p>
                  <div className="space-y-2">
                    {addedLibraries.map((lib) => {
                      const typeInfo = COLLECTION_TYPES.find(
                        (ct) => ct.value === lib.collectionType,
                      )
                      return (
                        <div
                          key={lib.id}
                          className="flex items-center justify-between gap-3 rounded-xl border border-emerald-500/25 bg-emerald-500/[0.06] px-3.5 py-2.5"
                        >
                          <div className="min-w-0">
                            <div className="flex items-center gap-2">
                              <span className="truncate text-sm font-semibold text-zinc-100">
                                {lib.name}
                              </span>
                              <span className="rounded bg-amber-500/15 px-1.5 py-0.5 text-[10px] font-medium text-amber-300">
                                {typeInfo ? t(typeInfo.labelKey) : lib.collectionType}
                              </span>
                            </div>
                            <p className="mt-0.5 truncate font-mono text-xs text-zinc-400">
                              {lib.paths.join(', ')}
                            </p>
                          </div>
                          <CheckCircle2 className="h-4 w-4 shrink-0 text-emerald-400" />
                        </div>
                      )
                    })}
                  </div>
                </div>
              )}

              {/* 添加媒体库表单 */}
              <form
                onSubmit={handleAddLibrary}
                className="space-y-4 rounded-xl border border-white/8 bg-white/[0.02] p-4"
              >
                <div className="grid gap-4 sm:grid-cols-2">
                  <div>
                    <label
                      htmlFor="setup-lib-name"
                      className="mb-1.5 block text-xs font-medium text-zinc-400"
                    >
                      {t('setupUi.libNameLabel')}
                    </label>
                    <input
                      id="setup-lib-name"
                      type="text"
                      value={libName}
                      onChange={(e) => setLibName(e.target.value)}
                      placeholder={t('setupUi.libNamePlaceholder')}
                      className="w-full rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2 text-sm text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
                    />
                  </div>

                  <div>
                    <label
                      htmlFor="setup-lib-type"
                      className="mb-1.5 block text-xs font-medium text-zinc-400"
                    >
                      {t('setupUi.libTypeLabel')}
                    </label>
                    <select
                      id="setup-lib-type"
                      value={libType}
                      onChange={(e) => setLibType(e.target.value)}
                      className="w-full rounded-xl border border-white/10 bg-[#14141d] px-3.5 py-2 text-sm text-zinc-100 outline-none focus:border-amber-400/60"
                    >
                      {COLLECTION_TYPES.map((ct) => (
                        <option key={ct.value} value={ct.value}>
                          {t(ct.labelKey)}
                        </option>
                      ))}
                    </select>
                  </div>
                </div>

                <div>
                  <label
                    htmlFor="setup-lib-path"
                    className="mb-1.5 block text-xs font-medium text-zinc-400"
                  >
                    {t('setupUi.serverDirPathLabel')}
                  </label>
                  <div className="flex gap-2">
                    <input
                      id="setup-lib-path"
                      type="text"
                      value={libPath}
                      onChange={(e) => setLibPath(e.target.value)}
                      placeholder={t('setupUi.serverDirPathPlaceholder')}
                      className="flex-1 rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2 font-mono text-xs text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
                    />
                    <button
                      type="button"
                      onClick={() => {
                        setBrowsePath('')
                        setBrowserOpen(true)
                      }}
                      className="inline-flex shrink-0 items-center gap-1.5 rounded-xl border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-xs font-medium text-amber-300 transition-colors hover:bg-amber-500/20"
                    >
                      <HardDrive className="h-3.5 w-3.5" />
                      <span>{t('setupUi.browseDirBtn')}</span>
                    </button>
                  </div>
                </div>

                <ToggleSwitch
                  checked={saveLocalMetadata}
                  onChange={setSaveLocalMetadata}
                  label={t('setupUi.saveLocalMetaLabel')}
                  description={t('setupUi.saveLocalMetaDesc')}
                />

                <div className="flex justify-end">
                  <button
                    type="submit"
                    disabled={addingLibrary}
                    className="inline-flex items-center gap-1.5 rounded-xl border border-amber-400/40 bg-amber-500/15 px-4 py-2 text-xs font-semibold text-amber-200 transition-colors hover:bg-amber-500/25 disabled:opacity-50"
                  >
                    {addingLibrary ? (
                      <Loader2 className="h-3.5 w-3.5 animate-spin" />
                    ) : (
                      <Plus className="h-3.5 w-3.5" />
                    )}
                    <span>{t('setupUi.addThisLibBtn')}</span>
                  </button>
                </div>
              </form>

              <div className="flex items-center justify-between pt-2">
                <span className="text-xs text-zinc-500">
                  {addedLibraries.length === 0
                    ? t('setupUi.canAddLaterHint')
                    : t('setupUi.readyLibsCount', { count: addedLibraries.length })}
                </span>
                <button
                  type="button"
                  onClick={handleStep3Next}
                  className="inline-flex items-center gap-2 rounded-xl bg-gradient-to-r from-amber-500 to-orange-500 px-6 py-2.5 text-sm font-semibold text-zinc-950 shadow-lg shadow-amber-500/25 transition-all hover:from-amber-400 hover:to-orange-400"
                >
                  <span>
                    {addedLibraries.length > 0
                      ? t('setupUi.nextScraperSettingsBtn')
                      : t('setupUi.skipForNowBtn')}
                  </span>
                  <ArrowRight className="h-4 w-4" />
                </button>
              </div>
            </div>
          )}

          {/* ==================== Step 4: 刮削与偏好设置 ==================== */}
          {step === 4 && !blockedByRole && (
            <form onSubmit={handleCompleteSetup} className="space-y-5">
              <div>
                <h2 className="text-lg font-semibold text-zinc-100">{t('setupUi.step4Heading')}</h2>
                <p className="mt-1 text-xs text-zinc-400">{t('setupUi.step4Desc')}</p>
              </div>

              <div className="space-y-4">
                <div>
                  <label
                    htmlFor="setup-tmdb-key"
                    className="mb-1.5 block text-xs font-medium text-zinc-400"
                  >
                    {t('setup.tmdbApiKey')}
                  </label>
                  <div className="relative">
                    <KeyRound className="pointer-events-none absolute left-3.5 top-1/2 h-4 w-4 -translate-y-1/2 text-zinc-500" />
                    <input
                      id="setup-tmdb-key"
                      type="text"
                      value={tmdbApiKey}
                      onChange={(e) => setTmdbApiKey(e.target.value)}
                      placeholder={t('setupUi.tmdbKeyPlaceholder')}
                      className="w-full rounded-xl border border-white/10 bg-white/[0.04] py-2.5 pl-10 pr-4 font-mono text-xs text-zinc-100 placeholder-zinc-500 outline-none transition-colors focus:border-amber-400/60 focus:bg-white/[0.06]"
                    />
                  </div>
                  <p className="mt-1.5 text-[11px] leading-relaxed text-zinc-500">
                    {t('setupUi.tmdbKeyHint')}
                  </p>
                </div>

                <div>
                  <label
                    htmlFor="setup-tmdb-language"
                    className="mb-1.5 block text-xs font-medium text-zinc-400"
                  >
                    {t('setupUi.confirmScraperLangLabel')}
                  </label>
                  <select
                    id="setup-tmdb-language"
                    value={tmdbLanguage}
                    onChange={(e) => {
                      setTmdbLanguage(e.target.value)
                      setTmdbLangCustomized(true)
                    }}
                    className="w-full rounded-xl border border-white/10 bg-[#14141d] px-3.5 py-2.5 text-sm text-zinc-100 outline-none transition-colors focus:border-amber-400/60"
                  >
                    {LANGUAGE_OPTIONS.map((opt) => (
                      <option key={opt.value} value={opt.value}>
                        {opt.label}
                      </option>
                    ))}
                  </select>
                </div>

                <div className="space-y-2.5 pt-1">
                  <ToggleSwitch
                    checked={enableStrm302}
                    onChange={setEnableStrm302}
                    label={t('setupUi.strm302Label')}
                    description={t('setupUi.strm302Desc')}
                  />

                  <ToggleSwitch
                    checked={triggerScan}
                    onChange={setTriggerScan}
                    label={t('setupUi.scanOnCompleteLabel')}
                    description={t('setupUi.scanOnCompleteDesc')}
                  />

                  <ToggleSwitch
                    checked={syncRankings}
                    onChange={setSyncRankings}
                    label={t('setupUi.syncRankingsLabel')}
                    description={t('setupUi.syncRankingsDesc')}
                  />
                </div>
              </div>

              <div className="flex items-center justify-between pt-2">
                <button
                  type="button"
                  disabled={submitting}
                  onClick={() => {
                    setError('')
                    setStep(3)
                  }}
                  className="inline-flex items-center gap-1.5 rounded-xl border border-white/10 bg-white/[0.04] px-4 py-2.5 text-xs font-medium text-zinc-300 transition-colors hover:bg-white/[0.08] disabled:opacity-50"
                >
                  <ArrowLeft className="h-4 w-4" />
                  <span>{t('setupUi.prevStepBtn')}</span>
                </button>

                <button
                  type="submit"
                  disabled={submitting}
                  className="inline-flex items-center gap-2 rounded-xl bg-gradient-to-r from-amber-500 to-orange-500 px-6 py-2.5 text-sm font-semibold text-zinc-950 shadow-lg shadow-amber-500/25 transition-all hover:from-amber-400 hover:to-orange-400 disabled:opacity-60"
                >
                  {submitting ? (
                    <>
                      <Loader2 className="h-4 w-4 animate-spin" />
                      <span>{t('setupUi.completingSetup')}</span>
                    </>
                  ) : (
                    <>
                      <Sparkles className="h-4 w-4" />
                      <span>{t('setupUi.completeAndEnterBtn')}</span>
                    </>
                  )}
                </button>
              </div>
            </form>
          )}
        </div>
      </div>

      {/* 服务器目录浏览弹窗 */}
      <AdminModal
        open={browserOpen}
        title={t('setupUi.browserModalTitle')}
        subtitle={t('setupUi.browserModalSubtitle')}
        maxWidthClass="max-w-xl"
        onClose={() => setBrowserOpen(false)}
      >
        <div className="space-y-3">
          <div className="flex items-center justify-between gap-2 rounded-xl border border-white/10 bg-white/[0.03] px-3 py-2">
            <div className="flex min-w-0 items-center gap-2">
              <button
                type="button"
                disabled={!browsePath}
                onClick={() => setBrowsePath(getParentPath(browsePath))}
                className="inline-flex items-center gap-1 rounded-lg border border-white/10 bg-white/[0.05] px-2 py-1 text-[11px] font-medium text-zinc-200 hover:bg-white/10 disabled:opacity-40"
              >
                <ArrowUp className="h-3 w-3" />
                <span>{t('setupUi.browserUpLevel')}</span>
              </button>
              <button
                type="button"
                onClick={() => setBrowsePath('')}
                className="rounded-lg px-2 py-1 text-[11px] text-zinc-400 hover:bg-white/8 hover:text-zinc-200"
              >
                {t('setupUi.browserDiskRoots')}
              </button>
              <span className="truncate font-mono text-xs text-amber-300">
                {browsePath || t('setupUi.browserRootDrives')}
              </span>
            </div>

            {browsePath && (
              <button
                type="button"
                onClick={() => {
                  setLibPath(browsePath)
                  setBrowserOpen(false)
                }}
                className="inline-flex shrink-0 items-center gap-1 rounded-lg bg-amber-500/20 px-2.5 py-1 text-[11px] font-semibold text-amber-300 hover:bg-amber-500/30"
              >
                <Check className="h-3 w-3" />
                <span>{t('setupUi.browserSelectCurrent')}</span>
              </button>
            )}
          </div>

          <div className="max-h-72 overflow-y-auto rounded-xl border border-white/10 bg-black/40 p-2">
            {browserLoading ? (
              <div className="flex h-28 items-center justify-center">
                <Loader2 className="h-5 w-5 animate-spin text-amber-400" />
              </div>
            ) : browserError ? (
              <div className="px-3 py-8 text-center text-xs text-red-300">
                {t('setupUi.browserReadError', {
                  error:
                    browserError instanceof Error
                      ? browserError.message
                      : t('setupUi.browserApiUnavailable'),
                })}
                <p className="mt-1 text-zinc-500">{t('setupUi.browserManualInputHint')}</p>
              </div>
            ) : folderEntries.length === 0 ? (
              <div className="py-10 text-center text-xs text-zinc-500">
                {t('setupUi.browserNoSubfolders')}
              </div>
            ) : (
              <div className="space-y-1">
                {folderEntries.map((entry) => (
                  <div
                    key={entry.Path}
                    className="flex items-center justify-between gap-2 rounded-lg px-2.5 py-1.5 text-xs hover:bg-white/[0.06]"
                  >
                    <button
                      type="button"
                      onClick={() => setBrowsePath(entry.Path)}
                      className="flex min-w-0 flex-1 items-center gap-2 text-left text-zinc-200"
                    >
                      {browsePath === '' ? (
                        <HardDrive className="h-3.5 w-3.5 shrink-0 text-amber-400" />
                      ) : (
                        <Folder className="h-3.5 w-3.5 shrink-0 text-amber-400/90" />
                      )}
                      <span className="truncate">{entry.Name || entry.Path}</span>
                      <ChevronRight className="h-3 w-3 shrink-0 text-zinc-600" />
                    </button>
                    <button
                      type="button"
                      onClick={() => {
                        setLibPath(entry.Path)
                        setBrowserOpen(false)
                      }}
                      className="shrink-0 rounded-md bg-white/8 px-2.5 py-1 text-[11px] font-medium text-zinc-300 transition-colors hover:bg-amber-500/25 hover:text-amber-200"
                    >
                      {t('setupUi.browserSelectBtn')}
                    </button>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      </AdminModal>
    </div>
  )
}
