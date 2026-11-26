import { useEffect, useMemo, useRef, useState } from 'react'
import {
  AlertTriangle,
  ArrowLeft,
  ArrowRight,
  ArrowRightLeft,
  CheckCircle2,
  FolderOpen,
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
import { isDemoMode } from '../../demo/demoMode'
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

const DEMO_TEST_RESULT: SourceTestResult = {
  server: {
    Id: 'demo-legacy-emby-01',
    ServerName: '旧 Emby NAS 影院 (Demo)',
    Version: '4.8.10.0',
    ProductName: 'Emby Server',
    OperatingSystem: 'Linux',
    Status: 'ok',
  },
  users: [
    {
      Id: 'demo-u-1',
      Name: 'Admin',
      Policy: { IsAdministrator: true, IsDisabled: false },
      LastActivityDate: new Date(Date.now() - 3600_000).toISOString(),
    },
    {
      Id: 'demo-u-2',
      Name: '家庭观影',
      Policy: { IsAdministrator: false, IsDisabled: false },
      LastActivityDate: new Date(Date.now() - 86400_000).toISOString(),
    },
    {
      Id: 'demo-u-3',
      Name: '访客账号',
      Policy: { IsAdministrator: false, IsDisabled: false },
      LastActivityDate: new Date(Date.now() - 7 * 86400_000).toISOString(),
    },
  ],
  virtual_folders: [
    { Name: '4K 电影', CollectionType: 'movies', Locations: ['/volume1/media/movies'] },
    { Name: '精选剧集', CollectionType: 'tvshows', Locations: ['/volume1/media/tv'] },
    { Name: '动画番剧', CollectionType: 'tvshows', Locations: ['/volume1/media/anime'] },
  ],
}

export function MigrationTab() {
  const demo = isDemoMode()
  const [step, setStep] = useState<WizardStep>(1)
  const [feedback, setFeedback] = useState<{ type: 'success' | 'error'; message: string } | null>(
    null,
  )

  // Step 1: 连接配置
  const [sourceURL, setSourceURL] = useState(demo ? 'http://192.168.1.88:8096' : '')
  const [apiKey, setApiKey] = useState(demo ? 'demo-emby-api-key-9f2a4c' : '')
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
  const demoTimerRef = useRef<number | null>(null)
  const pollSeqRef = useRef(0)

  const validateSourceUrl = (rawUrl: string): string | null => {
    const trimmed = rawUrl.trim()
    if (!trimmed) return '请先填写旧 Emby 服务器地址和 API Key'
    try {
      const parsed = new URL(trimmed)
      if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') {
        return '旧 Emby 服务器地址必须使用 http:// 或 https:// 协议'
      }
      return null
    } catch {
      return '旧 Emby 服务器地址格式无效，请填写完整的 http:// 或 https:// 地址'
    }
  }

  // 初始拉取当前后端迁移状态（如果有正在进行或最近完成的任务）
  useEffect(() => {
    if (demo) return
    let mounted = true
    const reqSeq = ++pollSeqRef.current
    embyFetch<MigrationReport>('/api/admin/migration/status')
      .then((rep) => {
        if (!mounted || reqSeq !== pollSeqRef.current || !rep || rep.status === 'idle') return
        setLiveReport(rep)
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
  }, [demo])

  // 运行中轮询进度
  useEffect(() => {
    if (demo || !liveReport || liveReport.status !== 'running') return
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
              message: `迁移完成：共匹配 ${rep.items_matched} 条记录（已看 ${rep.played_migrated} / 收藏 ${rep.favorites_migrated} / 断点 ${rep.resume_migrated}）`,
            })
          } else if (rep.status === 'failed') {
            setFeedback({
              type: 'error',
              message: rep.errors?.[0] || '迁移执行失败，请检查源服务器状态与日志',
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
  }, [demo, liveReport?.status])

  useEffect(() => {
    return () => {
      if (demoTimerRef.current) {
        window.clearInterval(demoTimerRef.current)
      }
    }
  }, [])

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
      setFeedback({ type: 'error', message: '请先填写旧 Emby 服务器地址和 API Key' })
      return
    }
    const urlErr = validateSourceUrl(sourceURL)
    if (urlErr) {
      setFeedback({ type: 'error', message: urlErr })
      return
    }
    setTesting(true)
    try {
      let res: SourceTestResult
      if (demo) {
        await new Promise((r) => setTimeout(r, 450))
        res = DEMO_TEST_RESULT
      } else {
        res = await embyFetch<SourceTestResult>('/api/admin/migration/test', {
          method: 'POST',
          body: JSON.stringify({ source_url: sourceURL.trim(), api_key: apiKey.trim() }),
        })
      }
      setTestResult(res)
      setSelectedUserIDs(res.users.map((u) => u.Id))
      setDryReport(null)
      setFeedback({
        type: 'success',
        message: `连接成功：${res.server.ServerName}（v${res.server.Version}），发现 ${res.users.length} 个用户与 ${res.virtual_folders.length} 个媒体库`,
      })
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '无法连接到旧 Emby 服务器',
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
      setFeedback({ type: 'error', message: '请至少勾选一个需要迁移的用户' })
      return
    }
    setDryRunning(true)
    try {
      let rep: MigrationReport
      if (demo) {
        await new Promise((r) => setTimeout(r, 600))
        const userCount = selectedUserIDs.length
        rep = {
          status: 'completed',
          dry_run: true,
          source_url: sourceURL.trim(),
          source_server_name: DEMO_TEST_RESULT.server.ServerName,
          started_at: new Date().toISOString(),
          completed_at: new Date().toISOString(),
          total_users: userCount,
          processed_users: userCount,
          users_matched: 1,
          users_created: migrateUsers ? Math.max(0, userCount - 1) : 0,
          items_scanned: userCount * 142,
          items_matched: userCount * 134,
          items_skipped: userCount * 8,
          played_migrated: migratePlayed ? userCount * 96 : 0,
          favorites_migrated: migrateFavorites ? userCount * 24 : 0,
          resume_migrated: migrateResume ? userCount * 14 : 0,
          conflicts: [
            {
              user_name: 'Admin',
              item_name: '沙丘 (2021)',
              item_type: 'Movie',
              reason: '演示冲突：本地存在多版本重复条目，已安全跳过',
            },
          ],
          errors: [],
          resumed_from_checkpoint: false,
        }
      } else {
        rep = await embyFetch<MigrationReport>('/api/admin/migration/dry-run', {
          method: 'POST',
          body: JSON.stringify(buildPayload(true)),
        })
      }
      setDryReport(rep)
      setFeedback({
        type: 'success',
        message: `Dry-Run 模拟评估完成：扫描 ${rep.items_scanned} 条，可精确匹配 ${rep.items_matched} 条，冲突 ${rep.conflicts.length} 条`,
      })
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : 'Dry-Run 模拟评估失败',
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
    if (selectedUserIDs.length === 0) {
      setFeedback({ type: 'error', message: '请至少勾选一个需要迁移的用户' })
      return
    }
    pollSeqRef.current += 1
    setStarting(true)
    setStep(3)
    try {
      if (demo) {
        const totalUsers = selectedUserIDs.length
        const startedAt = new Date().toISOString()
        let tick = 0
        setLiveReport({
          status: 'running',
          dry_run: false,
          source_url: sourceURL.trim(),
          source_server_name: DEMO_TEST_RESULT.server.ServerName,
          started_at: startedAt,
          total_users: totalUsers,
          processed_users: 0,
          current_user: DEMO_TEST_RESULT.users[0]?.Name || 'Admin',
          users_matched: 0,
          users_created: 0,
          items_scanned: 0,
          items_matched: 0,
          items_skipped: 0,
          played_migrated: 0,
          favorites_migrated: 0,
          resume_migrated: 0,
          conflicts: [],
          errors: [],
          resumed_from_checkpoint: false,
        })
        if (demoTimerRef.current) window.clearInterval(demoTimerRef.current)
        demoTimerRef.current = window.setInterval(() => {
          tick += 1
          const done = tick >= totalUsers
          const processed = Math.min(tick, totalUsers)
          setLiveReport({
            status: done ? 'completed' : 'running',
            dry_run: false,
            source_url: sourceURL.trim(),
            source_server_name: DEMO_TEST_RESULT.server.ServerName,
            started_at: startedAt,
            completed_at: done ? new Date().toISOString() : undefined,
            total_users: totalUsers,
            processed_users: processed,
            current_user: done ? '' : DEMO_TEST_RESULT.users[processed]?.Name || '',
            users_matched: 1,
            users_created: migrateUsers ? Math.max(0, processed - 1) : 0,
            items_scanned: processed * 142,
            items_matched: processed * 134,
            items_skipped: processed * 8,
            played_migrated: migratePlayed ? processed * 96 : 0,
            favorites_migrated: migrateFavorites ? processed * 24 : 0,
            resume_migrated: migrateResume ? processed * 14 : 0,
            conflicts: done
              ? [
                  {
                    user_name: 'Admin',
                    item_name: '沙丘 (2021)',
                    item_type: 'Movie',
                    reason: '演示冲突：本地存在多版本重复条目，已安全跳过',
                  },
                ]
              : [],
            errors: [],
            resumed_from_checkpoint: false,
          })
          if (done && demoTimerRef.current) {
            window.clearInterval(demoTimerRef.current)
            demoTimerRef.current = null
            setFeedback({
              type: 'success',
              message: '演示迁移已完成！所有用户观影记录、收藏与播放断点已同步到 Ambra',
            })
          }
        }, 700)
      } else {
        const rep = await embyFetch<MigrationReport>('/api/admin/migration/start', {
          method: 'POST',
          body: JSON.stringify(buildPayload(false)),
        })
        setLiveReport(rep)
        setFeedback({
          type: 'success',
          message: '迁移任务已启动，正在后台批量写入并自动保存断点...',
        })
      }
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '启动迁移失败',
      })
    } finally {
      setStarting(false)
    }
  }

  const handleCancelMigration = async () => {
    pollSeqRef.current += 1
    setCancelling(true)
    try {
      if (demo) {
        if (demoTimerRef.current) {
          window.clearInterval(demoTimerRef.current)
          demoTimerRef.current = null
        }
        setLiveReport((prev) =>
          prev
            ? { ...prev, status: 'cancelled', completed_at: new Date().toISOString() }
            : prev,
        )
        setFeedback({ type: 'success', message: '已取消演示迁移任务，当前断点已保留' })
      } else {
        const res = await embyFetch<{ cancelled: boolean; report: MigrationReport }>(
          '/api/admin/migration/cancel',
          { method: 'POST' },
        )
        pollSeqRef.current += 1
        setLiveReport(res.report)
        setFeedback({
          type: 'success',
          message: res.cancelled
            ? '已发送取消信号，已完成批次的断点已持久化，可随时断点续传'
            : '当前没有正在运行的迁移任务',
        })
      }
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '取消迁移失败',
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

  return (
    <div className="space-y-6">
      {/* 顶部标题 */}
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 className="flex items-center gap-2 text-base font-semibold text-zinc-100">
            <ArrowRightLeft className="h-4 w-4 text-amber-400" />
            从旧 Emby 一键迁移
          </h2>
          <p className="mt-0.5 text-xs text-zinc-400">
            通过旧 Emby API 安全迁移用户账号与权限、已看状态、收藏列表及播放断点，支持 Dry-Run 预览与断点续传
          </p>
        </div>
        {demo && (
          <span className="inline-flex items-center gap-1.5 self-start rounded-full border border-amber-500/30 bg-amber-500/10 px-3 py-1 text-xs font-medium text-amber-300">
            <Sparkles className="h-3.5 w-3.5" />
            Demo 模拟模式
          </span>
        )}
      </div>

      <FeedbackBanner
        message={feedback?.message}
        type={feedback?.type}
        onClose={() => setFeedback(null)}
      />

      {/* 3 步向导导航栏 */}
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        {[
          { num: 1 as WizardStep, title: '1. 连接旧 Emby 服务器', desc: '验证地址与 API Key' },
          { num: 2 as WizardStep, title: '2. 迁移范围与 Dry-Run', desc: '勾选用户与模拟评估' },
          { num: 3 as WizardStep, title: '3. 执行迁移与报告', desc: '实时进度与结果汇总' },
        ].map((item) => {
          const active = step === item.num
          const done = step > item.num
          const clickable = item.num === 1 || (item.num === 2 && Boolean(testResult)) || (item.num === 3 && Boolean(liveReport || testResult))
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
          <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
            <div>
              <label className="mb-1.5 flex items-center gap-1.5 text-xs font-medium text-zinc-300">
                <Server className="h-3.5 w-3.5 text-amber-400" />
                旧 Emby 服务器地址
              </label>
              <input
                type="url"
                value={sourceURL}
                onChange={(e) => setSourceURL(e.target.value)}
                placeholder="例如 http://192.168.1.88:8096"
                className="w-full rounded-xl border border-white/10 bg-[#0d0d14] px-3.5 py-2.5 text-sm text-zinc-100 placeholder-zinc-500 outline-none transition-colors focus:border-amber-500/60"
              />
            </div>

            <div>
              <label className="mb-1.5 flex items-center gap-1.5 text-xs font-medium text-zinc-300">
                <KeyRound className="h-3.5 w-3.5 text-amber-400" />
                旧 Emby 管理员 API Key
              </label>
              <input
                type="password"
                value={apiKey}
                onChange={(e) => setApiKey(e.target.value)}
                placeholder="在旧 Emby 控制台 → API 密钥中生成"
                className="w-full rounded-xl border border-white/10 bg-[#0d0d14] px-3.5 py-2.5 text-sm text-zinc-100 placeholder-zinc-500 outline-none transition-colors focus:border-amber-500/60"
              />
            </div>
          </div>

          <div className="flex flex-wrap items-center justify-between gap-3">
            <p className="text-xs text-zinc-500">
              迁移过程仅读取旧服务器数据，不会修改或删除旧 Emby 上的任何文件与记录。
            </p>
            <button
              type="button"
              disabled={testing}
              onClick={handleTestConnection}
              className="inline-flex items-center gap-2 rounded-xl bg-amber-500 px-4 py-2 text-sm font-medium text-zinc-950 transition-colors hover:bg-amber-400 disabled:opacity-60"
            >
              {testing ? <Loader2 className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}
              测试连接
            </button>
          </div>

          {testResult && (
            <div className="space-y-4 border-t border-white/8 pt-4">
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
                <div className="rounded-xl border border-white/8 bg-[#0d0d14] p-3.5">
                  <p className="text-xs text-zinc-400">源服务器</p>
                  <p className="mt-1 truncate text-sm font-semibold text-zinc-100">
                    {testResult.server.ServerName}
                  </p>
                  <p className="mt-0.5 text-xs text-amber-400">Emby v{testResult.server.Version}</p>
                </div>
                <div className="rounded-xl border border-white/8 bg-[#0d0d14] p-3.5">
                  <p className="text-xs text-zinc-400">发现用户数</p>
                  <p className="mt-1 text-xl font-bold text-zinc-100">{testResult.users.length}</p>
                  <p className="mt-0.5 text-xs text-zinc-500">支持自动匹配或新建本地账号</p>
                </div>
                <div className="rounded-xl border border-white/8 bg-[#0d0d14] p-3.5">
                  <p className="text-xs text-zinc-400">源媒体库数</p>
                  <p className="mt-1 text-xl font-bold text-zinc-100">
                    {testResult.virtual_folders.length}
                  </p>
                  <p className="mt-0.5 truncate text-xs text-zinc-500">
                    {testResult.virtual_folders.map((f) => f.Name).join('、') || '暂无媒体库'}
                  </p>
                </div>
              </div>

              {testResult.virtual_folders.length > 0 && (
                <div className="rounded-xl border border-white/8 bg-[#0d0d14] p-3.5">
                  <p className="mb-2 flex items-center gap-1.5 text-xs font-medium text-zinc-300">
                    <FolderOpen className="h-3.5 w-3.5 text-amber-400" />
                    源媒体库概览
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
                  下一步：选择迁移范围
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
          {/* 用户选择 */}
          <div className="space-y-3">
            <div className="flex items-center justify-between">
              <h3 className="flex items-center gap-2 text-sm font-semibold text-zinc-100">
                <Users className="h-4 w-4 text-amber-400" />
                选择要迁移的用户 ({selectedUserIDs.length}/{testResult.users.length})
              </h3>
              <button
                type="button"
                onClick={toggleAllUsers}
                className="text-xs font-medium text-amber-400 hover:text-amber-300"
              >
                {selectedUserIDs.length === testResult.users.length ? '取消全选' : '全选所有用户'}
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
                          {u.Policy?.IsAdministrator ? '管理员' : '普通用户'}
                          {u.Policy?.IsDisabled ? ' · 已禁用' : ''}
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
              选择迁移数据类型
            </h3>
            <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
              <ToggleSwitch
                checked={migrateUsers}
                onChange={setMigrateUsers}
                label="用户账号与权限"
                description="自动按用户名匹配本地账号，不存在时自动创建并同步 Policy / 配置"
              />
              <ToggleSwitch
                checked={migratePlayed}
                onChange={setMigratePlayed}
                label="已看记录"
                description="迁移影片与剧集的已看状态、观看次数及最近播放时间"
              />
              <ToggleSwitch
                checked={migrateFavorites}
                onChange={setMigrateFavorites}
                label="收藏列表"
                description="保留用户在旧 Emby 中标记的收藏影片、剧集与单集"
              />
              <ToggleSwitch
                checked={migrateResume}
                onChange={setMigrateResume}
                label="播放断点"
                description="迁移未看完媒体的播放进度（Ticks），无缝衔接继续观看"
              />
            </div>
            <ToggleSwitch
              checked={resetCheckpoint}
              onChange={setResetCheckpoint}
              label="忽略历史断点，从头全量重跑"
              description="默认启用断点续传；勾选此项将清除历史断点并从头幂等重跑"
            />
          </div>

          {/* Dry-Run 模拟评估报告 */}
          {dryReport && (
            <div className="space-y-3 rounded-xl border border-amber-500/30 bg-amber-500/5 p-4">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <p className="text-sm font-semibold text-amber-200">Dry-Run 模拟评估结果</p>
                <span className="rounded-full bg-amber-500/20 px-2.5 py-0.5 text-xs font-medium text-amber-300">
                  匹配率 {matchRate}%
                </span>
              </div>
              <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
                <StatCard label="扫描条目" value={dryReport.items_scanned} />
                <StatCard label="精确匹配" value={dryReport.items_matched} accent="emerald" />
                <StatCard label="跳过 / 无数据" value={dryReport.items_skipped} />
                <StatCard
                  label="潜在冲突"
                  value={dryReport.conflicts.length}
                  accent={dryReport.conflicts.length > 0 ? 'amber' : 'default'}
                />
              </div>
              <div className="flex flex-wrap gap-4 text-xs text-zinc-300">
                <span>匹配本地用户：{dryReport.users_matched}</span>
                <span>预计新建用户：{dryReport.users_created}</span>
                <span>预计同步已看：{dryReport.played_migrated}</span>
                <span>预计同步收藏：{dryReport.favorites_migrated}</span>
                <span>预计同步断点：{dryReport.resume_migrated}</span>
              </div>
              {dryReport.conflicts.length > 0 && (
                <div className="max-h-36 space-y-1.5 overflow-y-auto rounded-lg border border-white/8 bg-[#0d0d14] p-3 text-xs">
                  {dryReport.conflicts.slice(0, 10).map((c, idx) => (
                    <div key={idx} className="flex items-start gap-2 text-amber-300/90">
                      <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
                      <span>
                        [{c.user_name || '系统'}] {c.item_name || c.source_item_id || ''}：{c.reason}
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
              上一步
            </button>
            <div className="flex flex-wrap items-center gap-2.5">
              <button
                type="button"
                disabled={dryRunning}
                onClick={handleDryRun}
                className="inline-flex items-center gap-2 rounded-xl border border-amber-500/40 bg-amber-500/10 px-4 py-2 text-sm font-medium text-amber-300 transition-colors hover:bg-amber-500/20 disabled:opacity-60"
              >
                {dryRunning ? <Loader2 className="h-4 w-4 animate-spin" /> : <Sparkles className="h-4 w-4" />}
                Dry-Run 模拟评估
              </button>
              <button
                type="button"
                disabled={starting}
                onClick={handleStartMigration}
                className="inline-flex items-center gap-2 rounded-xl bg-amber-500 px-4 py-2 text-sm font-medium text-zinc-950 transition-colors hover:bg-amber-400 disabled:opacity-60"
              >
                {starting ? <Loader2 className="h-4 w-4 animate-spin" /> : <Play className="h-4 w-4" />}
                开始正式迁移
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
                迁移任务状态：
                <span className="ml-2 text-amber-400">
                  {liveReport?.status === 'running'
                    ? '迁移进行中...'
                    : liveReport?.status === 'completed'
                      ? '迁移已完成'
                      : liveReport?.status === 'cancelled'
                        ? '已取消（已保留断点）'
                        : liveReport?.status === 'failed'
                          ? '迁移失败'
                          : '等待启动'}
                </span>
              </h3>
              {liveReport?.started_at && (
                <p className="mt-0.5 text-xs text-zinc-400">
                  开始时间：{formatDateTime(liveReport.started_at)}
                  {liveReport.completed_at ? ` · 结束时间：${formatDateTime(liveReport.completed_at)}` : ''}
                  {liveReport.resumed_from_checkpoint ? ' · 已从历史断点续传' : ''}
                </p>
              )}
            </div>

            <div className="flex items-center gap-2">
              {liveReport?.status === 'running' ? (
                <button
                  type="button"
                  disabled={cancelling}
                  onClick={handleCancelMigration}
                  className="inline-flex items-center gap-1.5 rounded-xl border border-red-500/40 bg-red-500/10 px-3.5 py-2 text-sm font-medium text-red-300 transition-colors hover:bg-red-500/20 disabled:opacity-60"
                >
                  {cancelling ? <Loader2 className="h-4 w-4 animate-spin" /> : <StopCircle className="h-4 w-4" />}
                  取消迁移
                </button>
              ) : (
                <button
                  type="button"
                  disabled={starting || !testResult}
                  onClick={handleStartMigration}
                  className="inline-flex items-center gap-1.5 rounded-xl bg-amber-500 px-3.5 py-2 text-sm font-medium text-zinc-950 transition-colors hover:bg-amber-400 disabled:opacity-60"
                >
                  <Play className="h-4 w-4" />
                  {liveReport?.status === 'cancelled' ? '从断点继续迁移' : '重新执行迁移'}
                </button>
              )}
            </div>
          </div>

          {/* 进度条 */}
          <div className="space-y-2">
            <div className="flex items-center justify-between text-xs text-zinc-400">
              <span>
                {liveReport?.current_user
                  ? `正在处理用户：${liveReport.current_user}`
                  : `用户进度：${liveReport?.processed_users ?? 0} / ${liveReport?.total_users ?? 0}`}
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
                <StatCard label="匹配 / 新建用户" value={`${liveReport.users_matched} / ${liveReport.users_created}`} />
                <StatCard label="扫描 / 匹配条目" value={`${liveReport.items_scanned} / ${liveReport.items_matched}`} accent="emerald" />
                <StatCard label="已看 / 收藏" value={`${liveReport.played_migrated} / ${liveReport.favorites_migrated}`} />
                <StatCard label="播放断点 / 跳过" value={`${liveReport.resume_migrated} / ${liveReport.items_skipped}`} accent="amber" />
              </div>

              {liveReport.conflicts.length > 0 && (
                <div className="space-y-2 rounded-xl border border-amber-500/25 bg-amber-500/5 p-4">
                  <p className="text-xs font-semibold text-amber-300">
                    冲突明细（共 {liveReport.conflicts.length} 条）
                  </p>
                  <div className="max-h-40 space-y-1.5 overflow-y-auto text-xs text-zinc-300">
                    {liveReport.conflicts.map((c, idx) => (
                      <div key={idx} className="flex items-start gap-2">
                        <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0 text-amber-400" />
                        <span>
                          [{c.user_name || '系统'}] {c.item_name || c.source_item_id || ''}：{c.reason}
                        </span>
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {liveReport.errors.length > 0 && (
                <div className="space-y-1.5 rounded-xl border border-red-500/30 bg-red-500/10 p-4 text-xs text-red-300">
                  <p className="font-semibold">错误信息</p>
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
              返回配置
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
