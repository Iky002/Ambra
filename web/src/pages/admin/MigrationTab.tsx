import { useEffect, useMemo, useRef, useState } from 'react'
import {
  AlertTriangle,
  ArrowLeft,
  ArrowRight,
  ArrowRightLeft,
  CheckCircle2,
  FolderOpen,
  History,
  KeyRound,
  Loader2,
  Play,
  RefreshCw,
  Server,
  ShieldCheck,
  Sparkles,
  StopCircle,
  Users,
} from 'lucide-react'
import { embyFetch } from '../../api/client'
import { useI18n } from '../../i18n/I18nContext'
import { FeedbackBanner, ToggleSwitch, formatDateTime } from './adminUtils'

interface SourceServerInfo {
  Id: string
  ServerName: string
  Version: string
  ProductName?: string
  OperatingSystem?: string
  LocalAddress?: string
  Status: string
}

interface SourceUser {
  Id: string
  Name: string
  HasPassword?: boolean
  LastLoginDate?: string
  LastActivityDate?: string
  Policy?: {
    IsAdministrator?: boolean
    IsDisabled?: boolean
  }
}

interface SourceVirtualFolder {
  Name: string
  ItemId?: string
  CollectionType?: string
  Locations?: string[]
}

interface SourceTestResult {
  server: SourceServerInfo
  users: SourceUser[]
  virtual_folders: SourceVirtualFolder[]
}

interface ConflictEntry {
  user_name?: string
  source_item_id?: string
  item_name?: string
  item_type?: string
  reason: string
}

interface MigrationReport {
  status: 'idle' | 'running' | 'completed' | 'cancelled' | 'failed'
  dry_run: boolean
  source_url?: string
  source_server_name?: string
  started_at?: string
  completed_at?: string
  total_users: number
  processed_users: number
  current_user?: string
  users_matched: number
  users_created: number
  items_scanned: number
  items_matched: number
  items_skipped: number
  played_migrated: number
  favorites_migrated: number
  resume_migrated: number
  conflicts: ConflictEntry[]
  errors: string[]
  resumed_from_checkpoint: boolean
  checkpoint_key?: string
}

type WizardStep = 1 | 2 | 3

export function MigrationTab() {
  const { t } = useI18n()
  const [step, setStep] = useState<WizardStep>(1)
  const [feedback, setFeedback] = useState<{ type: 'success' | 'error'; message: string } | null>(
    null,
  )

  // Step 1: 连接配置
  const [sourceURL, setSourceURL] = useState('')
  const [apiKey, setApiKey] = useState('')
  const [testing, setTesting] = useState(false)
  const [testResult, setTestResult] = useState<SourceTestResult | null>(null)

  // Step 2: 范围与 Dry-Run
  const [selectedUserIDs, setSelectedUserIDs] = useState<string[]>([])
  const [migrateUsers, setMigrateUsers] = useState(true)
  const [migratePlayed, setMigratePlayed] = useState(true)
  const [migrateFavorites, setMigrateFavorites] = useState(true)
  const [migrateResume, setMigrateResume] = useState(true)
  const [resetCheckpoint, setResetCheckpoint] = useState(false)
  const [dryRunning, setDryRunning] = useState(false)
  const [dryReport, setDryReport] = useState<MigrationReport | null>(null)

  // Step 3: 正式迁移与状态轮询
  const [starting, setStarting] = useState(false)
  const [cancelling, setCancelling] = useState(false)
  const [liveReport, setLiveReport] = useState<MigrationReport | null>(null)
  const pollSeqRef = useRef(0)

  const validateSourceUrl = (rawUrl: string): string | null => {
    const trimmed = rawUrl.trim()
    if (!trimmed) return t('admin.migration.errMissingCreds')
    try {
      const parsed = new URL(trimmed)
      if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') {
        return t('admin.migration.errProtocol')
      }
      return null
    } catch {
      return t('admin.migration.errInvalidUrl')
    }
  }

  // 初始拉取当前后端迁移状态（自动恢复运行中任务或预填中断断点）
  useEffect(() => {
    let mounted = true
    const reqSeq = ++pollSeqRef.current
    embyFetch<MigrationReport>('/api/admin/migration/status')
      .then((rep) => {
        if (!mounted || reqSeq !== pollSeqRef.current || !rep || rep.status === 'idle') return
        setLiveReport(rep)
        if (rep.source_url) {
          setSourceURL((prev) => (prev.trim() ? prev : rep.source_url || ''))
        }
        if (rep.status === 'running') {
          setStep(3)
        }
      })
      .catch(() => {
        // 忽略初始状态查询失败
      })
    return () => {
      mounted = false
    }
  }, [])

  // 运行中轮询进度
  useEffect(() => {
    if (!liveReport || liveReport.status !== 'running') return
    let cancelled = false
    const timer = window.setInterval(() => {
      const reqSeq = ++pollSeqRef.current
      embyFetch<MigrationReport>('/api/admin/migration/status')
        .then((rep) => {
          if (cancelled || reqSeq !== pollSeqRef.current) return
          setLiveReport(rep)
          if (rep.status === 'completed') {
            setFeedback({
              type: 'success',
              message: t('admin.migration.completedSuccess', {
                matched: rep.items_matched,
                played: rep.played_migrated,
                favorites: rep.favorites_migrated,
                resume: rep.resume_migrated,
              }),
            })
          } else if (rep.status === 'failed') {
            setFeedback({
              type: 'error',
              message: rep.errors?.[0] || t('admin.migration.failedDefault'),
            })
          }
        })
        .catch(() => {
          // 忽略单次轮询波动
        })
    }, 1200)
    return () => {
      cancelled = true
      window.clearInterval(timer)
    }
  }, [liveReport?.status, t])

  const toggleUser = (id: string) => {
    setSelectedUserIDs((prev) =>
      prev.includes(id) ? prev.filter((item) => item !== id) : [...prev, id],
    )
  }

  const toggleAllUsers = () => {
    if (!testResult) return
    if (selectedUserIDs.length === testResult.users.length) {
      setSelectedUserIDs([])
    } else {
      setSelectedUserIDs(testResult.users.map((u) => u.Id))
    }
  }

  const buildPayload = (dryRun: boolean) => ({
    source_url: sourceURL.trim(),
    api_key: apiKey.trim(),
    user_ids: selectedUserIDs,
    migrate_users: migrateUsers,
    migrate_played: migratePlayed,
    migrate_favorites: migrateFavorites,
    migrate_resume: migrateResume,
    dry_run: dryRun,
    reset_checkpoint: resetCheckpoint,
  })

  const handleTestConnection = async () => {
    setFeedback(null)
    if (!sourceURL.trim() || !apiKey.trim()) {
      setFeedback({ type: 'error', message: t('admin.migration.errMissingCreds') })
      return
    }
    const urlErr = validateSourceUrl(sourceURL)
    if (urlErr) {
      setFeedback({ type: 'error', message: urlErr })
      return
    }
    setTesting(true)
    try {
      const res = await embyFetch<SourceTestResult>('/api/admin/migration/test', {
        method: 'POST',
        body: JSON.stringify({ source_url: sourceURL.trim(), api_key: apiKey.trim() }),
      })
      setTestResult(res)
      setSelectedUserIDs(res.users.map((u) => u.Id))
      setDryReport(null)
      setFeedback({
        type: 'success',
        message: t('admin.migration.testSuccess', {
          name: res.server.ServerName,
          version: res.server.Version,
          users: res.users.length,
          libs: res.virtual_folders.length,
        }),
      })
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('admin.migration.testFailed'),
      })
    } finally {
      setTesting(false)
    }
  }

  const handleDryRun = async () => {
    setFeedback(null)
    const urlErr = validateSourceUrl(sourceURL)
    if (urlErr) {
      setFeedback({ type: 'error', message: urlErr })
      return
    }
    if (selectedUserIDs.length === 0) {
      setFeedback({ type: 'error', message: t('admin.migration.errNoUsers') })
      return
    }
    setDryRunning(true)
    try {
      const rep = await embyFetch<MigrationReport>('/api/admin/migration/dry-run', {
        method: 'POST',
        body: JSON.stringify(buildPayload(true)),
      })
      setDryReport(rep)
      setFeedback({
        type: 'success',
        message: t('admin.migration.dryRunSuccess', {
          scanned: rep.items_scanned,
          matched: rep.items_matched,
          conflicts: rep.conflicts.length,
        }),
      })
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('admin.migration.dryRunFailed'),
      })
    } finally {
      setDryRunning(false)
    }
  }

  const handleStartMigration = async () => {
    setFeedback(null)
    const urlErr = validateSourceUrl(sourceURL)
    if (urlErr) {
      setFeedback({ type: 'error', message: urlErr })
      return
    }
    if (!apiKey.trim()) {
      setFeedback({ type: 'error', message: t('admin.migration.errMissingCreds') })
      return
    }
    if (selectedUserIDs.length === 0) {
      setFeedback({ type: 'error', message: t('admin.migration.errNoUsers') })
      return
    }
    pollSeqRef.current += 1
    setStarting(true)
    try {
      const rep = await embyFetch<MigrationReport>('/api/admin/migration/start', {
        method: 'POST',
        body: JSON.stringify(buildPayload(false)),
      })
      setLiveReport(rep)
      setStep(3)
      setFeedback({
        type: 'success',
        message: t('admin.migration.startedSuccess'),
      })
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('admin.migration.startFailed'),
      })
    } finally {
      setStarting(false)
    }
  }

  const handleReturnToFillCredentials = () => {
    if (liveReport?.source_url) {
      setSourceURL((prev) => prev.trim() || liveReport.source_url || '')
    }
    setStep(1)
    setFeedback({
      type: 'success',
      message: t('admin.migration.backFillKeyHint'),
    })
  }

  const handleCancelMigration = async () => {
    pollSeqRef.current += 1
    setCancelling(true)
    try {
      const res = await embyFetch<{ cancelled: boolean; report: MigrationReport }>(
        '/api/admin/migration/cancel',
        { method: 'POST' },
      )
      pollSeqRef.current += 1
      setLiveReport(res.report)
      setFeedback({
        type: 'success',
        message: res.cancelled
          ? t('admin.migration.cancelledSuccess')
          : t('admin.migration.cancelNoRunning'),
      })
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('admin.migration.cancelFailed'),
      })
    } finally {
      setCancelling(false)
    }
  }

  const matchRate = useMemo(() => {
    const rep = step === 3 ? liveReport : dryReport
    if (!rep || rep.items_scanned === 0) return 0
    return Math.round((rep.items_matched / rep.items_scanned) * 100)
  }, [step, liveReport, dryReport])

  const progressPct = useMemo(() => {
    if (!liveReport) return 0
    if (liveReport.status === 'completed') return 100
    if (liveReport.total_users <= 0) return liveReport.status === 'running' ? 15 : 0
    return Math.min(99, Math.round((liveReport.processed_users / liveReport.total_users) * 100))
  }, [liveReport])

  const isCheckpointInterrupted =
    liveReport?.status === 'cancelled' || liveReport?.status === 'failed'
  const needsCredentialsToResume = !testResult || !apiKey.trim()

  return (
    <div className="space-y-6">
      {/* 顶部标题 */}
      <div>
        <h2 className="flex items-center gap-2 text-base font-semibold text-zinc-100">
          <ArrowRightLeft className="h-4 w-4 text-amber-400" />
          <span>{t('admin.migration.title')}</span>
        </h2>
        <p className="mt-0.5 text-xs text-zinc-400">{t('admin.migration.subtitle')}</p>
      </div>

      <FeedbackBanner
        message={feedback?.message}
        type={feedback?.type}
        onClose={() => setFeedback(null)}
      />

      {/* 3 步向导导航栏 */}
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        {[
          {
            num: 1 as WizardStep,
            title: t('admin.migration.step1Title'),
            desc: t('admin.migration.step1Desc'),
          },
          {
            num: 2 as WizardStep,
            title: t('admin.migration.step2Title'),
            desc: t('admin.migration.step2Desc'),
          },
          {
            num: 3 as WizardStep,
            title: t('admin.migration.step3Title'),
            desc: t('admin.migration.step3Desc'),
          },
        ].map((item) => {
          const active = step === item.num
          const done = step > item.num
          const clickable =
            item.num === 1 ||
            (item.num === 2 && Boolean(testResult)) ||
            (item.num === 3 && Boolean(liveReport || testResult))
          return (
            <button
              key={item.num}
              type="button"
              disabled={!clickable}
              onClick={() => clickable && setStep(item.num)}
              className={`flex items-center gap-3 rounded-xl border px-4 py-3 text-left transition-all ${
                active
                  ? 'border-amber-500/50 bg-amber-500/10 text-amber-200 shadow-sm shadow-amber-500/10'
                  : done
                    ? 'border-emerald-500/30 bg-emerald-500/5 text-zinc-200 hover:bg-emerald-500/10'
                    : 'border-white/8 bg-white/[0.02] text-zinc-400 hover:bg-white/[0.04] disabled:cursor-not-allowed disabled:opacity-45'
              }`}
            >
              <div
                className={`flex h-7 w-7 shrink-0 items-center justify-center rounded-full text-xs font-bold ${
                  active
                    ? 'bg-amber-500 text-zinc-950'
                    : done
                      ? 'bg-emerald-500/20 text-emerald-300'
                      : 'bg-white/8 text-zinc-400'
                }`}
              >
                {done ? <CheckCircle2 className="h-4 w-4" /> : item.num}
              </div>
              <div className="min-w-0">
                <p className="truncate text-sm font-medium">{item.title}</p>
                <p className="truncate text-xs opacity-70">{item.desc}</p>
              </div>
            </button>
          )
        })}
      </div>

      {/* Step 1: 连接旧 Emby 服务器 */}
      {step === 1 && (
        <section className="space-y-5 rounded-2xl border border-white/10 bg-white/[0.02] p-5">
          {isCheckpointInterrupted && liveReport && (
            <div
              data-testid="migration-checkpoint-banner"
              className="flex flex-col gap-3 rounded-xl border border-amber-500/35 bg-amber-500/10 p-4 sm:flex-row sm:items-center sm:justify-between"
            >
              <div className="space-y-1">
                <p className="flex items-center gap-1.5 text-xs font-semibold text-amber-200">
                  <History className="h-4 w-4 shrink-0 text-amber-400" />
                  <span>{t('admin.migration.checkpointBannerTitle')}</span>
                </p>
                <p className="text-xs leading-relaxed text-amber-100/85">
                  {t('admin.migration.checkpointBannerDesc', {
                    status:
                      liveReport.status === 'cancelled'
                        ? t('admin.migration.statusCancelled')
                        : t('admin.migration.statusFailed'),
                    url: liveReport.source_url || '-',
                    processed: liveReport.processed_users,
                    total: liveReport.total_users,
                    matched: liveReport.items_matched,
                  })}
                </p>
              </div>
              <button
                type="button"
                onClick={() => setStep(3)}
                className="inline-flex shrink-0 items-center gap-1.5 self-start rounded-lg border border-amber-400/40 bg-amber-500/15 px-3 py-1.5 text-xs font-medium text-amber-200 transition-colors hover:bg-amber-500/25 sm:self-center"
              >
                <span>{t('admin.migration.viewLastReport')}</span>
                <ArrowRight className="h-3.5 w-3.5" />
              </button>
            </div>
          )}

          <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
            <div>
              <label className="mb-1.5 flex items-center gap-1.5 text-xs font-medium text-zinc-300">
                <Server className="h-3.5 w-3.5 text-amber-400" />
                <span>{t('admin.migration.sourceUrlLabel')}</span>
              </label>
              <input
                type="url"
                value={sourceURL}
                onChange={(e) => setSourceURL(e.target.value)}
                placeholder={t('admin.migration.sourceUrlPlaceholder')}
                className="w-full rounded-xl border border-white/10 bg-[#0d0d14] px-3.5 py-2.5 text-sm text-zinc-100 placeholder-zinc-500 outline-none transition-colors focus:border-amber-500/60"
              />
            </div>

            <div>
              <label className="mb-1.5 flex items-center gap-1.5 text-xs font-medium text-zinc-300">
                <KeyRound className="h-3.5 w-3.5 text-amber-400" />
                <span>{t('admin.migration.apiKeyLabel')}</span>
              </label>
              <input
                type="password"
                value={apiKey}
                onChange={(e) => setApiKey(e.target.value)}
                placeholder={t('admin.migration.apiKeyPlaceholder')}
                className="w-full rounded-xl border border-white/10 bg-[#0d0d14] px-3.5 py-2.5 text-sm text-zinc-100 placeholder-zinc-500 outline-none transition-colors focus:border-amber-500/60"
              />
            </div>
          </div>

          <div className="flex flex-wrap items-center justify-between gap-3">
            <p className="text-xs text-zinc-500">{t('admin.migration.readOnlyHint')}</p>
            <button
              type="button"
              disabled={testing}
              onClick={() => void handleTestConnection()}
              className="inline-flex items-center gap-2 rounded-xl bg-amber-500 px-4 py-2 text-sm font-medium text-zinc-950 transition-colors hover:bg-amber-400 disabled:opacity-60"
            >
              {testing ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <RefreshCw className="h-4 w-4" />
              )}
              <span>{t('admin.migration.testBtn')}</span>
            </button>
          </div>

          {testResult && (
            <div className="space-y-4 border-t border-white/8 pt-4">
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
                <div className="rounded-xl border border-white/8 bg-[#0d0d14] p-3.5">
                  <p className="text-xs text-zinc-400">{t('admin.migration.sourceServer')}</p>
                  <p className="mt-1 truncate text-sm font-semibold text-zinc-100">
                    {testResult.server.ServerName}
                  </p>
                  <p className="mt-0.5 text-xs text-amber-400">Emby v{testResult.server.Version}</p>
                </div>
                <div className="rounded-xl border border-white/8 bg-[#0d0d14] p-3.5">
                  <p className="text-xs text-zinc-400">{t('admin.migration.discoveredUsers')}</p>
                  <p className="mt-1 text-xl font-bold text-zinc-100">{testResult.users.length}</p>
                  <p className="mt-0.5 text-xs text-zinc-500">
                    {t('admin.migration.discoveredUsersHint')}
                  </p>
                </div>
                <div className="rounded-xl border border-white/8 bg-[#0d0d14] p-3.5">
                  <p className="text-xs text-zinc-400">{t('admin.migration.sourceLibs')}</p>
                  <p className="mt-1 text-xl font-bold text-zinc-100">
                    {testResult.virtual_folders.length}
                  </p>
                  <p className="mt-0.5 truncate text-xs text-zinc-500">
                    {testResult.virtual_folders.map((f) => f.Name).join(', ') ||
                      t('admin.migration.noLibs')}
                  </p>
                </div>
              </div>

              {testResult.virtual_folders.length > 0 && (
                <div className="rounded-xl border border-white/8 bg-[#0d0d14] p-3.5">
                  <p className="mb-2 flex items-center gap-1.5 text-xs font-medium text-zinc-300">
                    <FolderOpen className="h-3.5 w-3.5 text-amber-400" />
                    <span>{t('admin.migration.sourceLibsOverview')}</span>
                  </p>
                  <div className="flex flex-wrap gap-2">
                    {testResult.virtual_folders.map((vf) => (
                      <span
                        key={vf.ItemId || vf.Name}
                        className="rounded-lg border border-white/8 bg-white/[0.03] px-2.5 py-1 text-xs text-zinc-300"
                      >
                        {vf.Name}
                        {vf.CollectionType ? ` (${vf.CollectionType})` : ''}
                      </span>
                    ))}
                  </div>
                </div>
              )}

              <div className="flex justify-end">
                <button
                  type="button"
                  onClick={() => setStep(2)}
                  className="inline-flex items-center gap-2 rounded-xl bg-amber-500 px-4 py-2 text-sm font-medium text-zinc-950 transition-colors hover:bg-amber-400"
                >
                  <span>{t('admin.migration.nextStepBtn')}</span>
                  <ArrowRight className="h-4 w-4" />
                </button>
              </div>
            </div>
          )}
        </section>
      )}

      {/* Step 2: 选择迁移范围与 Dry-Run 预览 */}
      {step === 2 && testResult && (
        <section className="space-y-5 rounded-2xl border border-white/10 bg-white/[0.02] p-5">
          <div className="space-y-3">
            <div className="flex items-center justify-between">
              <h3 className="flex items-center gap-2 text-sm font-semibold text-zinc-100">
                <Users className="h-4 w-4 text-amber-400" />
                <span>
                  {t('admin.migration.selectUsersTitle', {
                    selected: selectedUserIDs.length,
                    total: testResult.users.length,
                  })}
                </span>
              </h3>
              <button
                type="button"
                onClick={toggleAllUsers}
                className="text-xs font-medium text-amber-400 hover:text-amber-300"
              >
                {selectedUserIDs.length === testResult.users.length
                  ? t('admin.migration.deselectAll')
                  : t('admin.migration.selectAllUsers')}
              </button>
            </div>
            <div className="grid grid-cols-1 gap-2.5 sm:grid-cols-2 lg:grid-cols-3">
              {testResult.users.map((u) => {
                const checked = selectedUserIDs.includes(u.Id)
                return (
                  <label
                    key={u.Id}
                    className={`flex cursor-pointer items-center justify-between gap-3 rounded-xl border px-3.5 py-2.5 transition-colors ${
                      checked
                        ? 'border-amber-500/40 bg-amber-500/10'
                        : 'border-white/8 bg-[#0d0d14] hover:border-white/15'
                    }`}
                  >
                    <div className="flex min-w-0 items-center gap-2.5">
                      <input
                        type="checkbox"
                        checked={checked}
                        onChange={() => toggleUser(u.Id)}
                        className="h-4 w-4 rounded border-white/20 bg-zinc-900 accent-amber-500"
                      />
                      <div className="min-w-0">
                        <p className="truncate text-sm font-medium text-zinc-100">{u.Name}</p>
                        <p className="text-[11px] text-zinc-400">
                          {u.Policy?.IsAdministrator
                            ? t('admin.users.roleAdmin')
                            : t('admin.users.roleMember')}
                          {u.Policy?.IsDisabled ? ` · ${t('admin.users.disabledBadge')}` : ''}
                        </p>
                      </div>
                    </div>
                  </label>
                )
              })}
            </div>
          </div>

          {/* 迁移内容开关 */}
          <div className="space-y-3 border-t border-white/8 pt-4">
            <h3 className="flex items-center gap-2 text-sm font-semibold text-zinc-100">
              <ShieldCheck className="h-4 w-4 text-amber-400" />
              <span>{t('admin.migration.dataTypesTitle')}</span>
            </h3>
            <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
              <ToggleSwitch
                checked={migrateUsers}
                onChange={setMigrateUsers}
                label={t('admin.migration.typeUsersLabel')}
                description={t('admin.migration.typeUsersDesc')}
              />
              <ToggleSwitch
                checked={migratePlayed}
                onChange={setMigratePlayed}
                label={t('admin.migration.typePlayedLabel')}
                description={t('admin.migration.typePlayedDesc')}
              />
              <ToggleSwitch
                checked={migrateFavorites}
                onChange={setMigrateFavorites}
                label={t('admin.migration.typeFavsLabel')}
                description={t('admin.migration.typeFavsDesc')}
              />
              <ToggleSwitch
                checked={migrateResume}
                onChange={setMigrateResume}
                label={t('admin.migration.typeResumeLabel')}
                description={t('admin.migration.typeResumeDesc')}
              />
            </div>
            <ToggleSwitch
              checked={resetCheckpoint}
              onChange={setResetCheckpoint}
              label={t('admin.migration.resetCpLabel')}
              description={t('admin.migration.resetCpDesc')}
            />
          </div>

          {/* Dry-Run 模拟评估报告 */}
          {dryReport && (
            <div className="space-y-3 rounded-xl border border-amber-500/30 bg-amber-500/5 p-4">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <p className="text-sm font-semibold text-amber-200">
                  {t('admin.migration.dryRunResultTitle')}
                </p>
                <span className="rounded-full bg-amber-500/20 px-2.5 py-0.5 text-xs font-medium text-amber-300">
                  {t('admin.migration.matchRateBadge', { rate: matchRate })}
                </span>
              </div>
              <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
                <StatCard label={t('admin.migration.statScanned')} value={dryReport.items_scanned} />
                <StatCard
                  label={t('admin.migration.statMatched')}
                  value={dryReport.items_matched}
                  accent="emerald"
                />
                <StatCard label={t('admin.migration.statSkipped')} value={dryReport.items_skipped} />
                <StatCard
                  label={t('admin.migration.statConflicts')}
                  value={dryReport.conflicts.length}
                  accent={dryReport.conflicts.length > 0 ? 'amber' : 'default'}
                />
              </div>
              <div className="flex flex-wrap gap-4 text-xs text-zinc-300">
                <span>
                  {t('admin.migration.summaryUsersMatched', { count: dryReport.users_matched })}
                </span>
                <span>
                  {t('admin.migration.summaryUsersCreated', { count: dryReport.users_created })}
                </span>
                <span>
                  {t('admin.migration.summaryPlayed', { count: dryReport.played_migrated })}
                </span>
                <span>
                  {t('admin.migration.summaryFavs', { count: dryReport.favorites_migrated })}
                </span>
                <span>
                  {t('admin.migration.summaryResume', { count: dryReport.resume_migrated })}
                </span>
              </div>
              {dryReport.conflicts.length > 0 && (
                <div className="max-h-36 space-y-1.5 overflow-y-auto rounded-lg border border-white/8 bg-[#0d0d14] p-3 text-xs">
                  {dryReport.conflicts.slice(0, 10).map((c, idx) => (
                    <div key={idx} className="flex items-start gap-2 text-amber-300/90">
                      <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
                      <span>
                        [{c.user_name || t('admin.migration.systemTag')}] {c.item_name || c.source_item_id || ''}：{c.reason}
                      </span>
                    </div>
                  ))}
                </div>
              )}
            </div>
          )}

          {/* 底部操作 */}
          <div className="flex flex-wrap items-center justify-between gap-3 border-t border-white/8 pt-4">
            <button
              type="button"
              onClick={() => setStep(1)}
              className="inline-flex items-center gap-1.5 rounded-xl border border-white/10 px-3.5 py-2 text-sm text-zinc-300 transition-colors hover:bg-white/5"
            >
              <ArrowLeft className="h-4 w-4" />
              <span>{t('admin.migration.prevStepBtn')}</span>
            </button>
            <div className="flex flex-wrap items-center gap-2.5">
              <button
                type="button"
                disabled={dryRunning}
                onClick={() => void handleDryRun()}
                className="inline-flex items-center gap-2 rounded-xl border border-amber-500/40 bg-amber-500/10 px-4 py-2 text-sm font-medium text-amber-300 transition-colors hover:bg-amber-500/20 disabled:opacity-60"
              >
                {dryRunning ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  <Sparkles className="h-4 w-4" />
                )}
                <span>{t('admin.migration.dryRunBtn')}</span>
              </button>
              <button
                type="button"
                disabled={starting}
                onClick={() => void handleStartMigration()}
                className="inline-flex items-center gap-2 rounded-xl bg-amber-500 px-4 py-2 text-sm font-medium text-zinc-950 transition-colors hover:bg-amber-400 disabled:opacity-60"
              >
                {starting ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  <Play className="h-4 w-4" />
                )}
                <span>{t('admin.migration.startBtn')}</span>
              </button>
            </div>
          </div>
        </section>
      )}

      {/* Step 3: 执行迁移与结果报告 */}
      {step === 3 && (
        <section className="space-y-5 rounded-2xl border border-white/10 bg-white/[0.02] p-5">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <h3 className="text-sm font-semibold text-zinc-100">
                <span>{t('admin.migration.taskStatusLabel')}</span>
                <span className="ml-2 text-amber-400">
                  {liveReport?.status === 'running'
                    ? t('admin.migration.statusRunning')
                    : liveReport?.status === 'completed'
                      ? t('admin.migration.statusCompleted')
                      : liveReport?.status === 'cancelled'
                        ? t('admin.migration.statusCancelled')
                        : liveReport?.status === 'failed'
                          ? t('admin.migration.statusFailed')
                          : t('admin.migration.statusIdle')}
                </span>
              </h3>
              {liveReport?.started_at && (
                <p className="mt-0.5 text-xs text-zinc-400">
                  {t('admin.migration.startedAtLabel', {
                    time: formatDateTime(liveReport.started_at),
                  })}
                  {liveReport.completed_at
                    ? ` · ${t('admin.migration.completedAtLabel', {
                        time: formatDateTime(liveReport.completed_at),
                      })}`
                    : ''}
                  {liveReport.resumed_from_checkpoint
                    ? ` · ${t('admin.migration.resumedBadge')}`
                    : ''}
                </p>
              )}
            </div>

            <div className="flex items-center gap-2">
              {liveReport?.status === 'running' ? (
                <button
                  type="button"
                  disabled={cancelling}
                  onClick={() => void handleCancelMigration()}
                  className="inline-flex items-center gap-1.5 rounded-xl border border-red-500/40 bg-red-500/10 px-3.5 py-2 text-sm font-medium text-red-300 transition-colors hover:bg-red-500/20 disabled:opacity-60"
                >
                  {cancelling ? (
                    <Loader2 className="h-4 w-4 animate-spin" />
                  ) : (
                    <StopCircle className="h-4 w-4" />
                  )}
                  <span>{t('admin.migration.cancelBtn')}</span>
                </button>
              ) : needsCredentialsToResume ? (
                <button
                  type="button"
                  onClick={handleReturnToFillCredentials}
                  className="inline-flex items-center gap-1.5 rounded-xl bg-amber-500 px-3.5 py-2 text-sm font-medium text-zinc-950 transition-colors hover:bg-amber-400"
                >
                  <KeyRound className="h-4 w-4" />
                  <span>{t('admin.migration.backFillKeyResumeBtn')}</span>
                </button>
              ) : (
                <button
                  type="button"
                  disabled={starting}
                  onClick={() => void handleStartMigration()}
                  className="inline-flex items-center gap-1.5 rounded-xl bg-amber-500 px-3.5 py-2 text-sm font-medium text-zinc-950 transition-colors hover:bg-amber-400 disabled:opacity-60"
                >
                  <Play className="h-4 w-4" />
                  <span>
                    {liveReport?.status === 'cancelled' || liveReport?.status === 'failed'
                      ? t('admin.migration.resumeFromCpBtn')
                      : t('admin.migration.rerunBtn')}
                  </span>
                </button>
              )}
            </div>
          </div>

          {/* 进度条 */}
          <div className="space-y-2">
            <div className="flex items-center justify-between text-xs text-zinc-400">
              <span>
                {liveReport?.current_user
                  ? t('admin.migration.processingUser', { user: liveReport.current_user })
                  : t('admin.migration.userProgress', {
                      processed: liveReport?.processed_users ?? 0,
                      total: liveReport?.total_users ?? 0,
                    })}
              </span>
              <span className="font-mono text-amber-300">{progressPct}%</span>
            </div>
            <div className="h-2.5 w-full overflow-hidden rounded-full bg-white/8">
              <div
                className={`h-full rounded-full transition-all duration-300 ${
                  liveReport?.status === 'failed'
                    ? 'bg-red-500'
                    : liveReport?.status === 'completed'
                      ? 'bg-emerald-500'
                      : 'bg-amber-500'
                }`}
                style={{ width: `${progressPct}%` }}
              />
            </div>
          </div>

          {/* 统计指标 */}
          {liveReport && (
            <>
              <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
                <StatCard
                  label={t('admin.migration.statUsersPair')}
                  value={`${liveReport.users_matched} / ${liveReport.users_created}`}
                />
                <StatCard
                  label={t('admin.migration.statItemsPair')}
                  value={`${liveReport.items_scanned} / ${liveReport.items_matched}`}
                  accent="emerald"
                />
                <StatCard
                  label={t('admin.migration.statPlayedFavPair')}
                  value={`${liveReport.played_migrated} / ${liveReport.favorites_migrated}`}
                />
                <StatCard
                  label={t('admin.migration.statResumeSkipPair')}
                  value={`${liveReport.resume_migrated} / ${liveReport.items_skipped}`}
                  accent="amber"
                />
              </div>

              {liveReport.conflicts.length > 0 && (
                <div className="space-y-2 rounded-xl border border-amber-500/25 bg-amber-500/5 p-4">
                  <p className="text-xs font-semibold text-amber-300">
                    {t('admin.migration.conflictDetailsTitle', {
                      count: liveReport.conflicts.length,
                    })}
                  </p>
                  <div className="max-h-40 space-y-1.5 overflow-y-auto text-xs text-zinc-300">
                    {liveReport.conflicts.map((c, idx) => (
                      <div key={idx} className="flex items-start gap-2">
                        <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0 text-amber-400" />
                        <span>
                          [{c.user_name || t('admin.migration.systemTag')}] {c.item_name || c.source_item_id || ''}：{c.reason}
                        </span>
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {liveReport.errors.length > 0 && (
                <div className="space-y-1.5 rounded-xl border border-red-500/30 bg-red-500/10 p-4 text-xs text-red-300">
                  <p className="font-semibold">{t('admin.migration.errorTitle')}</p>
                  {liveReport.errors.map((e, idx) => (
                    <p key={idx}>{e}</p>
                  ))}
                </div>
              )}
            </>
          )}

          <div className="flex justify-between border-t border-white/8 pt-4">
            <button
              type="button"
              disabled={liveReport?.status === 'running'}
              onClick={() => setStep(testResult ? 2 : 1)}
              className="inline-flex items-center gap-1.5 rounded-xl border border-white/10 px-3.5 py-2 text-sm text-zinc-300 transition-colors hover:bg-white/5 disabled:opacity-50"
            >
              <ArrowLeft className="h-4 w-4" />
              <span>{t('admin.migration.backConfigBtn')}</span>
            </button>
          </div>
        </section>
      )}
    </div>
  )
}

function StatCard({
  label,
  value,
  accent = 'default',
}: {
  label: string
  value: string | number
  accent?: 'default' | 'emerald' | 'amber'
}) {
  const valueClass =
    accent === 'emerald'
      ? 'text-emerald-300'
      : accent === 'amber'
        ? 'text-amber-300'
        : 'text-zinc-100'
  return (
    <div className="rounded-xl border border-white/8 bg-[#0d0d14] p-3">
      <p className="text-[11px] text-zinc-400">{label}</p>
      <p className={`mt-1 text-lg font-bold ${valueClass}`}>{value}</p>
    </div>
  )
}
