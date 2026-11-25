import { useEffect, useState } from 'react'
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
  Music,
  Plus,
  Server,
  ShieldCheck,
  Sparkles,
  Tv,
  User,
} from 'lucide-react'
import { Navigate, useNavigate } from 'react-router-dom'
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

const STEPS: { id: WizardStep; title: string; subtitle: string; icon: typeof Server }[] = [
  { id: 1, title: '服务器设置', subtitle: '名称与语言', icon: Server },
  { id: 2, title: '管理员账号', subtitle: '创建超级管理员', icon: ShieldCheck },
  { id: 3, title: '添加媒体库', subtitle: '挂载影视目录', icon: FolderPlus },
  { id: 4, title: '刮削与偏好', subtitle: 'TMDB 与榜单', icon: Sparkles },
]

const LANGUAGE_OPTIONS = [
  { value: 'zh-CN', label: '简体中文 (zh-CN)' },
  { value: 'zh-TW', label: '繁體中文 (zh-TW)' },
  { value: 'en-US', label: 'English (en-US)' },
  { value: 'ja-JP', label: '日本語 (ja-JP)' },
]

const COUNTRY_OPTIONS = [
  { value: 'CN', label: '中国大陆 (CN)' },
  { value: 'HK', label: '中国香港 (HK)' },
  { value: 'TW', label: '中国台湾 (TW)' },
  { value: 'US', label: '美国 (US)' },
  { value: 'JP', label: '日本 (JP)' },
]

const COLLECTION_TYPES = [
  { value: 'movies', label: '电影', icon: Film },
  { value: 'tvshows', label: '剧集', icon: Tv },
  { value: 'music', label: '音乐', icon: Music },
  { value: 'mixed', label: '混合内容', icon: Folder },
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
  const { auth, applyAuthResult } = useAuth()
  const { locale, t } = useI18n()
  const navigate = useNavigate()
  const queryClient = useQueryClient()

  const [step, setStep] = useState<WizardStep>(1)
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)

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
    enabled: browserOpen && browsePath === '',
  })

  const {
    data: dirEntries = [],
    isLoading: dirLoading,
    error: dirError,
  } = useQuery({
    queryKey: ['setup', 'env', 'dir', browsePath],
    queryFn: () => api.getDirectoryContents(browsePath, true, false),
    enabled: browserOpen && browsePath !== '',
  })

  if (setupStatus?.completed === true) {
    return <Navigate to={auth ? '/' : '/login'} replace />
  }

  const handleStep1Next = (e: React.FormEvent) => {
    e.preventDefault()
    if (!serverName.trim()) {
      setError('请输入服务器名称')
      return
    }
    setError('')
    if (!tmdbLangCustomized) {
      setTmdbLanguage(language)
    }
    setStep(2)
  }

  const handleStep2Submit = async (e: React.FormEvent) => {
    e.preventDefault()
    const trimmedUser = username.trim()
    if (!trimmedUser) {
      setError('请输入管理员用户名')
      return
    }
    if (!password) {
      setError('请输入管理员密码')
      return
    }
    if (password.length < 4) {
      setError('管理员密码长度至少为 4 位')
      return
    }
    if (password !== confirmPassword) {
      setError('两次输入的密码不一致，请重新确认')
      return
    }

    setError('')
    setSubmitting(true)
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
      setError(err instanceof Error ? err.message : '创建管理员账号失败，请重试')
    } finally {
      setSubmitting(false)
    }
  }

  const handleAddLibrary = async (e: React.FormEvent) => {
    e.preventDefault()
    const trimmedName = libName.trim()
    const trimmedPath = libPath.trim()
    if (!trimmedName) {
      setError('请输入媒体库名称')
      return
    }
    if (!trimmedPath) {
      setError('请输入或浏览选择一个服务器目录路径')
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
      setLibraryFeedback(`已添加媒体库「${trimmedName}」`)
    } catch (err) {
      setError(err instanceof Error ? err.message : '添加媒体库失败，请检查路径后重试')
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
    setError('')
    setSubmitting(true)
    try {
      await api.completeSetup({
        server_name: serverName.trim() || 'Ambra',
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
      setError(err instanceof Error ? err.message : '完成初始化失败，请重试')
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
          <p className="mt-1.5 text-xs text-zinc-400">
            {locale === 'zh-CN'
              ? '首次运行初始化向导 · 只需 4 步完成你的专属私人影院部署'
              : t('setup.subtitle')}
          </p>
        </div>

        {/* 4 步水平进度条 */}
        <div className="mb-6 grid grid-cols-4 gap-2 sm:gap-3" aria-label="初始化步骤">
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
                  {s.title}
                </span>
                <span className="mt-0.5 hidden text-[10px] text-zinc-500 sm:block">
                  {s.subtitle}
                </span>
              </div>
            )
          })}
        </div>

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
                <h2 className="text-lg font-semibold text-zinc-100">第 1 步 · 服务器基础设置</h2>
                <p className="mt-1 text-xs text-zinc-400">
                  为你的影院服务器命名，并选择默认的元数据刮削语言与地区
                </p>
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
                      placeholder="例如：Ambra 家庭影院"
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
                      {locale === 'zh-CN' ? '首选刮削语言' : t('setup.tmdbLanguage')}
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
                      国家 / 地区
                    </label>
                    <select
                      id="setup-country"
                      value={country}
                      onChange={(e) => setCountry(e.target.value)}
                      className="w-full rounded-xl border border-white/10 bg-[#14141d] px-3.5 py-2.5 text-sm text-zinc-100 outline-none transition-colors focus:border-amber-400/60"
                    >
                      {COUNTRY_OPTIONS.map((opt) => (
                        <option key={opt.value} value={opt.value}>
                          {opt.label}
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
                  <span>下一步：创建管理员</span>
                  <ArrowRight className="h-4 w-4" />
                </button>
              </div>
            </form>
          )}

          {/* ==================== Step 2: 管理员账号 ==================== */}
          {step === 2 && (
            <form onSubmit={handleStep2Submit} className="space-y-5">
              <div>
                <h2 className="text-lg font-semibold text-zinc-100">
                  {locale === 'zh-CN' ? '第 2 步 · 创建管理员账号' : t('setup.createAdmin')}
                </h2>
                <p className="mt-1 text-xs text-zinc-400">
                  此账号拥有服务器完整管理权限，创建完成后将自动为您登录
                </p>
              </div>

              <div className="space-y-4">
                <div>
                  <label
                    htmlFor="setup-admin-username"
                    className="mb-1.5 block text-xs font-medium text-zinc-400"
                  >
                    管理员用户名
                  </label>
                  <div className="relative">
                    <User className="pointer-events-none absolute left-3.5 top-1/2 h-4 w-4 -translate-y-1/2 text-zinc-500" />
                    <input
                      id="setup-admin-username"
                      type="text"
                      autoComplete="username"
                      value={username}
                      onChange={(e) => setUsername(e.target.value)}
                      placeholder="输入管理员用户名"
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
                      管理员密码（至少 4 位）
                    </label>
                    <div className="relative">
                      <Lock className="pointer-events-none absolute left-3.5 top-1/2 h-4 w-4 -translate-y-1/2 text-zinc-500" />
                      <input
                        id="setup-admin-password"
                        type="password"
                        autoComplete="new-password"
                        value={password}
                        onChange={(e) => setPassword(e.target.value)}
                        placeholder="设置登录密码"
                        className="w-full rounded-xl border border-white/10 bg-white/[0.04] py-2.5 pl-10 pr-4 text-sm text-zinc-100 placeholder-zinc-500 outline-none transition-colors focus:border-amber-400/60 focus:bg-white/[0.06]"
                      />
                    </div>
                  </div>

                  <div>
                    <label
                      htmlFor="setup-admin-confirm"
                      className="mb-1.5 block text-xs font-medium text-zinc-400"
                    >
                      确认密码
                    </label>
                    <div className="relative">
                      <Lock className="pointer-events-none absolute left-3.5 top-1/2 h-4 w-4 -translate-y-1/2 text-zinc-500" />
                      <input
                        id="setup-admin-confirm"
                        type="password"
                        autoComplete="new-password"
                        value={confirmPassword}
                        onChange={(e) => setConfirmPassword(e.target.value)}
                        placeholder="再次输入密码"
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
                  <span>上一步</span>
                </button>

                <button
                  type="submit"
                  disabled={submitting}
                  className="inline-flex items-center gap-2 rounded-xl bg-gradient-to-r from-amber-500 to-orange-500 px-6 py-2.5 text-sm font-semibold text-zinc-950 shadow-lg shadow-amber-500/25 transition-all hover:from-amber-400 hover:to-orange-400 disabled:opacity-60"
                >
                  {submitting ? (
                    <>
                      <Loader2 className="h-4 w-4 animate-spin" />
                      <span>正在创建管理员...</span>
                    </>
                  ) : (
                    <>
                      <span>创建并继续</span>
                      <ArrowRight className="h-4 w-4" />
                    </>
                  )}
                </button>
              </div>
            </form>
          )}

          {/* ==================== Step 3: 添加媒体库 ==================== */}
          {step === 3 && (
            <div className="space-y-5">
              <div>
                <h2 className="text-lg font-semibold text-zinc-100">第 3 步 · 添加媒体库</h2>
                <p className="mt-1 text-xs text-zinc-400">
                  挂载你的电影、剧集或音乐目录（支持连续添加多个，也可暂时跳过稍后在管理控制台添加）
                </p>
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
                    已添加的媒体库（{addedLibraries.length}）
                  </p>
                  <div className="space-y-2">
                    {addedLibraries.map((lib) => {
                      const typeInfo = COLLECTION_TYPES.find((t) => t.value === lib.collectionType)
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
                                {typeInfo?.label || lib.collectionType}
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
                      媒体库名称
                    </label>
                    <input
                      id="setup-lib-name"
                      type="text"
                      value={libName}
                      onChange={(e) => setLibName(e.target.value)}
                      placeholder="例如：4K 电影"
                      className="w-full rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2 text-sm text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
                    />
                  </div>

                  <div>
                    <label
                      htmlFor="setup-lib-type"
                      className="mb-1.5 block text-xs font-medium text-zinc-400"
                    >
                      内容类型
                    </label>
                    <select
                      id="setup-lib-type"
                      value={libType}
                      onChange={(e) => setLibType(e.target.value)}
                      className="w-full rounded-xl border border-white/10 bg-[#14141d] px-3.5 py-2 text-sm text-zinc-100 outline-none focus:border-amber-400/60"
                    >
                      {COLLECTION_TYPES.map((t) => (
                        <option key={t.value} value={t.value}>
                          {t.label}
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
                    服务器目录路径
                  </label>
                  <div className="flex gap-2">
                    <input
                      id="setup-lib-path"
                      type="text"
                      value={libPath}
                      onChange={(e) => setLibPath(e.target.value)}
                      placeholder="输入路径如 /media/movies，或点击右侧浏览"
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
                      <span>浏览目录</span>
                    </button>
                  </div>
                </div>

                <ToggleSwitch
                  checked={saveLocalMetadata}
                  onChange={setSaveLocalMetadata}
                  label="保存元数据与图片到媒体文件夹"
                  description="刮削完成后自动将 .nfo 与 poster.jpg / fanart.jpg 保存到媒体同目录（兼容 Emby / Kodi）"
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
                    <span>添加此媒体库</span>
                  </button>
                </div>
              </form>

              <div className="flex items-center justify-between pt-2">
                <span className="text-xs text-zinc-500">
                  {addedLibraries.length === 0
                    ? '也可以稍后在管理控制台添加媒体库'
                    : `已就绪 ${addedLibraries.length} 个媒体库`}
                </span>
                <button
                  type="button"
                  onClick={handleStep3Next}
                  className="inline-flex items-center gap-2 rounded-xl bg-gradient-to-r from-amber-500 to-orange-500 px-6 py-2.5 text-sm font-semibold text-zinc-950 shadow-lg shadow-amber-500/25 transition-all hover:from-amber-400 hover:to-orange-400"
                >
                  <span>{addedLibraries.length > 0 ? '下一步：刮削设置' : '暂时跳过'}</span>
                  <ArrowRight className="h-4 w-4" />
                </button>
              </div>
            </div>
          )}

          {/* ==================== Step 4: 刮削与偏好设置 ==================== */}
          {step === 4 && (
            <form onSubmit={handleCompleteSetup} className="space-y-5">
              <div>
                <h2 className="text-lg font-semibold text-zinc-100">第 4 步 · 刮削与偏好设置</h2>
                <p className="mt-1 text-xs text-zinc-400">
                  配置 TMDB 元数据刮削与影院偏好，完成后即可进入 Ambra 首页
                </p>
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
                      placeholder="输入 TMDB v3 API Key 或 Read Access Token"
                      className="w-full rounded-xl border border-white/10 bg-white/[0.04] py-2.5 pl-10 pr-4 font-mono text-xs text-zinc-100 placeholder-zinc-500 outline-none transition-colors focus:border-amber-400/60 focus:bg-white/[0.06]"
                    />
                  </div>
                  <p className="mt-1.5 text-[11px] leading-relaxed text-zinc-500">
                    填写后可自动刮削中文元数据与高清海报，留空也可稍后在管理控制台配置
                  </p>
                </div>

                <div>
                  <label
                    htmlFor="setup-tmdb-language"
                    className="mb-1.5 block text-xs font-medium text-zinc-400"
                  >
                    刮削语言确认
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
                    label="启用 STRM 302 直链重定向"
                    description="播放 .strm 云盘/网盘文件时直接 302 重定向到源站，节省服务器带宽"
                  />

                  <ToggleSwitch
                    checked={triggerScan}
                    onChange={setTriggerScan}
                    label="完成后立即扫描媒体库"
                    description="自动索引刚添加的媒体目录并拉取影片海报与演职员信息"
                  />

                  <ToggleSwitch
                    checked={syncRankings}
                    onChange={setSyncRankings}
                    label="完成后同步豆瓣 / IMDb / TMDB 热门榜单"
                    description="自动在后台同步高分经典与近期热门榜单，丰富首页影单推荐"
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
                  <span>上一步</span>
                </button>

                <button
                  type="submit"
                  disabled={submitting}
                  className="inline-flex items-center gap-2 rounded-xl bg-gradient-to-r from-amber-500 to-orange-500 px-6 py-2.5 text-sm font-semibold text-zinc-950 shadow-lg shadow-amber-500/25 transition-all hover:from-amber-400 hover:to-orange-400 disabled:opacity-60"
                >
                  {submitting ? (
                    <>
                      <Loader2 className="h-4 w-4 animate-spin" />
                      <span>正在完成初始化...</span>
                    </>
                  ) : (
                    <>
                      <Sparkles className="h-4 w-4" />
                      <span>
                        {locale === 'zh-CN' ? '完成初始化并进入 Ambra' : t('setup.complete')}
                      </span>
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
        title="浏览服务器目录"
        subtitle="点击文件夹进入下一级，选择目标目录作为媒体库路径"
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
                <span>上级</span>
              </button>
              <button
                type="button"
                onClick={() => setBrowsePath('')}
                className="rounded-lg px-2 py-1 text-[11px] text-zinc-400 hover:bg-white/8 hover:text-zinc-200"
              >
                磁盘根列表
              </button>
              <span className="truncate font-mono text-xs text-amber-300">
                {browsePath || '根目录 / 驱动器'}
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
                <span>选择当前目录</span>
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
                读取服务器目录失败：
                {browserError instanceof Error ? browserError.message : '接口暂不可用'}
                <p className="mt-1 text-zinc-500">您仍可关闭弹窗并手动输入路径</p>
              </div>
            ) : folderEntries.length === 0 ? (
              <div className="py-10 text-center text-xs text-zinc-500">当前目录下没有子文件夹</div>
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
                      选中
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
