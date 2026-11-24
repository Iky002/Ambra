import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Activity,
  AlertTriangle,
  Archive,
  Check,
  CheckCircle2,
  Cpu,
  Database,
  FileWarning,
  HardDrive,
  ImageOff,
  Loader2,
  Lock,
  RefreshCw,
  RotateCcw,
  SearchCheck,
  ShieldAlert,
  Sparkles,
  Trash2,
  XCircle,
} from 'lucide-react'
import { api, embyFetch } from '../../api/client'
import type { BackupFileInfo, BackupInfoResponse } from '../../api/types'
import { AdminModal, FeedbackBanner, formatBytes, formatDateTime } from './adminUtils'

interface MemoryHealth {
  AllocBytes: number
  TotalAllocBytes: number
  SysBytes: number
  HeapInuseBytes: number
  NumGC: number
}

interface DatabaseHealth {
  DbPath: string
  DbBytes: number
  WalBytes: number
  ShmBytes: number
  TotalBytes: number
}

interface CacheHealth {
  ImagesBytes: number
  SubtitlesBytes: number
  TranscodeBytes: number
  TotalBytes: number
}

interface DiskHealth {
  Path: string
  TotalBytes: number
  FreeBytes: number
  AvailableBytes: number
  UsedPercent: number
}

interface LibraryStatsHealth {
  TotalItems: number
  MovieCount: number
  SeriesCount: number
  EpisodeCount: number
  UserCount: number
  PendingAudits: number
}

interface SchedulerHealth {
  TotalTasks: number
  ActiveTasks: number
  BudgetInUse: number
  BudgetCapacity: number
  BudgetUsagePct: number
}

interface SystemHealthReport {
  CollectedAt: number
  Status: 'healthy' | 'warning' | 'critical' | string
  GoVersion: string
  NumCpu: number
  NumGoroutine: number
  Memory: MemoryHealth
  Database: DatabaseHealth
  Cache: CacheHealth
  Disk: DiskHealth
  Library: LibraryStatsHealth
  Scheduler: SchedulerHealth
}

interface AuditIssueItem {
  Id: string
  Name: string
  Type: string
  LibraryId: string
  Path: string
  Year?: number
  IsStrm: boolean
}

interface LibraryAuditReport {
  ScannedAt: number
  TotalChecked: number
  MissingPosters: AuditIssueItem[]
  MissingTmdb: AuditIssueItem[]
  OrphanItems: AuditIssueItem[]
}

interface OrphanCleanupResult {
  Requested: number
  Deleted: number
  DeletedIds: string[]
}

interface ScrapeCandidate {
  ProviderId: string
  ProviderName?: string
  Title: string
  OriginalTitle?: string
  Year?: number
  Overview?: string
  Rating?: number
  PosterUrl?: string
  Confidence: number
  Genres?: string[]
  Tags?: string[]
}

interface ScrapeAuditItem {
  Id: string
  ItemId: string
  ItemName: string
  ItemType: string
  Path: string
  Confidence: number
  Candidates: ScrapeCandidate[]
  Status: 'pending' | 'approved' | 'dismissed' | string
  CreatedAt: number
  UpdatedAt: number
}

interface ScrapeReviewListResponse {
  Items: ScrapeAuditItem[]
  TotalRecordCount: number
}

type AuditSubTab = 'missingPosters' | 'missingTmdb' | 'orphans'
type ReviewStatusFilter = 'pending' | 'approved' | 'dismissed' | 'all'

const DEFAULT_LOCK_FIELDS = ['Name', 'Overview', 'ProductionYear', 'ProviderIds']

function formatUnixTime(sec?: number): string {
  if (!sec) return '-'
  return formatDateTime(new Date(sec * 1000).toISOString())
}

function getStatusBadge(status?: string) {
  switch (status) {
    case 'critical':
      return {
        label: '磁盘告急',
        cls: 'bg-red-500/20 text-red-300 ring-1 ring-red-500/40',
      }
    case 'warning':
      return {
        label: '需要注意',
        cls: 'bg-amber-500/20 text-amber-300 ring-1 ring-amber-500/40',
      }
    default:
      return {
        label: '运行健康',
        cls: 'bg-emerald-500/20 text-emerald-300 ring-1 ring-emerald-500/40',
      }
  }
}

export function HealthAndAuditTab() {
  const [feedback, setFeedback] = useState<{ type: 'success' | 'error'; message: string } | null>(
    null,
  )

  // 1. System health state
  const [health, setHealth] = useState<SystemHealthReport | null>(null)
  const [healthLoading, setHealthLoading] = useState(true)
  const [healthRefreshing, setHealthRefreshing] = useState(false)

  // 2. Library audit state
  const [auditReport, setAuditReport] = useState<LibraryAuditReport | null>(null)
  const [auditRunning, setAuditRunning] = useState(false)
  const [auditSubTab, setAuditSubTab] = useState<AuditSubTab>('missingPosters')
  const [selectedOrphans, setSelectedOrphans] = useState<Set<string>>(new Set())
  const [cleaningOrphans, setCleaningOrphans] = useState(false)

  // 3. Scrape review queue state
  const [reviewStatus, setReviewStatus] = useState<ReviewStatusFilter>('pending')
  const [reviews, setReviews] = useState<ScrapeAuditItem[]>([])
  const [reviewsLoading, setReviewsLoading] = useState(true)
  const [lockFields, setLockFields] = useState<string[]>(DEFAULT_LOCK_FIELDS)
  const [actingReviewId, setActingReviewId] = useState<string | null>(null)

  // 4. Database backup & restore state
  const [backupInfo, setBackupInfo] = useState<BackupInfoResponse | null>(null)
  const [backupLoading, setBackupLoading] = useState(true)
  const [creatingBackup, setCreatingBackup] = useState(false)
  const [restoreTarget, setRestoreTarget] = useState<BackupFileInfo | null>(null)
  const [restoringBackup, setRestoringBackup] = useState(false)

  const fetchBackupInfo = useCallback(async () => {
    setBackupLoading(true)
    try {
      const data = await api.getBackupInfo()
      setBackupInfo(data)
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '获取数据库备份列表失败',
      })
    } finally {
      setBackupLoading(false)
    }
  }, [])

  const fetchHealth = useCallback(async (isManual = false) => {
    if (isManual) {
      setHealthRefreshing(true)
    } else {
      setHealthLoading(true)
    }
    try {
      const data = await embyFetch<SystemHealthReport>('/api/admin/health')
      setHealth(data)
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '获取系统健康诊断失败',
      })
    } finally {
      setHealthLoading(false)
      setHealthRefreshing(false)
    }
  }, [])

  const fetchReviews = useCallback(async (status: ReviewStatusFilter) => {
    setReviewsLoading(true)
    try {
      const res = await embyFetch<ScrapeReviewListResponse>(
        `/api/admin/governance/review?status=${encodeURIComponent(status)}`,
      )
      setReviews(res.Items || [])
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '加载低置信度审校队列失败',
      })
    } finally {
      setReviewsLoading(false)
    }
  }, [])

  useEffect(() => {
    void fetchHealth(false)
    void fetchBackupInfo()
  }, [fetchHealth, fetchBackupInfo])

  useEffect(() => {
    void fetchReviews(reviewStatus)
  }, [fetchReviews, reviewStatus])

  const handleCreateBackup = async () => {
    setCreatingBackup(true)
    setFeedback(null)
    try {
      const created = await api.createBackup()
      setFeedback({
        type: 'success',
        message: `已创建数据库备份「${created.Name}」（${formatBytes(created.Size)}）`,
      })
      await fetchBackupInfo()
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '创建数据库备份失败',
      })
    } finally {
      setCreatingBackup(false)
    }
  }

  const handleConfirmRestore = async () => {
    if (!restoreTarget) return
    setRestoringBackup(true)
    setFeedback(null)
    try {
      const res = await api.restoreBackup(restoreTarget.Name)
      setRestoreTarget(null)
      setFeedback({
        type: 'success',
        message: `已暂存恢复文件「${res.BackupName}」至 ${res.StagedPath || 'ambra.db.restore'}，将在下次重启服务器后生效`,
      })
      await fetchBackupInfo()
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '恢复数据库备份失败',
      })
    } finally {
      setRestoringBackup(false)
    }
  }

  const handleRunAudit = async () => {
    setAuditRunning(true)
    setFeedback(null)
    try {
      const report = await embyFetch<LibraryAuditReport>('/api/admin/governance/audit')
      setAuditReport(report)
      setSelectedOrphans(new Set())
      setFeedback({
        type: 'success',
        message: `体检完成：共检查 ${report.TotalChecked} 个条目，缺海报 ${report.MissingPosters.length}，缺 TMDB ${report.MissingTmdb.length}，孤立记录 ${report.OrphanItems.length}`,
      })
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '执行媒体库体检失败',
      })
    } finally {
      setAuditRunning(false)
    }
  }

  const toggleOrphanSelect = (id: string) => {
    setSelectedOrphans((prev) => {
      const next = new Set(prev)
      if (next.has(id)) {
        next.delete(id)
      } else {
        next.add(id)
      }
      return next
    })
  }

  const allOrphansSelected = useMemo(() => {
    const list = auditReport?.OrphanItems || []
    return list.length > 0 && list.every((item) => selectedOrphans.has(item.Id))
  }, [auditReport, selectedOrphans])

  const handleToggleSelectAllOrphans = () => {
    const list = auditReport?.OrphanItems || []
    if (allOrphansSelected) {
      setSelectedOrphans(new Set())
    } else {
      setSelectedOrphans(new Set(list.map((i) => i.Id)))
    }
  }

  const handleCleanupOrphans = async () => {
    const ids = Array.from(selectedOrphans)
    if (ids.length === 0) return
    setCleaningOrphans(true)
    setFeedback(null)
    try {
      const res = await embyFetch<OrphanCleanupResult>('/api/admin/governance/orphans/cleanup', {
        method: 'POST',
        body: JSON.stringify({ ItemIds: ids }),
      })
      setFeedback({
        type: 'success',
        message: `已清理 ${res.Deleted} 条磁盘孤立记录`,
      })
      setSelectedOrphans(new Set())
      const refreshed = await embyFetch<LibraryAuditReport>('/api/admin/governance/audit')
      setAuditReport(refreshed)
      void fetchHealth(true)
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '清理孤立记录失败',
      })
    } finally {
      setCleaningOrphans(false)
    }
  }

  const toggleLockField = (field: string) => {
    setLockFields((prev) =>
      prev.includes(field) ? prev.filter((f) => f !== field) : [...prev, field],
    )
  }

  const handleApproveReview = async (audit: ScrapeAuditItem, candidate: ScrapeCandidate) => {
    setActingReviewId(audit.Id)
    setFeedback(null)
    try {
      await embyFetch<ScrapeAuditItem>(
        `/api/admin/governance/review/${encodeURIComponent(audit.Id)}/approve`,
        {
          method: 'POST',
          body: JSON.stringify({
            Candidate: candidate,
            LockFields: lockFields,
          }),
        },
      )
      setFeedback({
        type: 'success',
        message: `已将「${candidate.Title}」应用至「${audit.ItemName}」并锁定 ${lockFields.length} 个字段`,
      })
      await fetchReviews(reviewStatus)
      void fetchHealth(true)
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '应用候选元数据失败',
      })
    } finally {
      setActingReviewId(null)
    }
  }

  const handleDismissReview = async (audit: ScrapeAuditItem) => {
    setActingReviewId(audit.Id)
    setFeedback(null)
    try {
      await embyFetch<ScrapeAuditItem>(
        `/api/admin/governance/review/${encodeURIComponent(audit.Id)}/dismiss`,
        { method: 'POST' },
      )
      setFeedback({
        type: 'success',
        message: `已忽略审校项「${audit.ItemName}」`,
      })
      await fetchReviews(reviewStatus)
      void fetchHealth(true)
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '忽略审校项失败',
      })
    } finally {
      setActingReviewId(null)
    }
  }

  const activeAuditItems = useMemo<AuditIssueItem[]>(() => {
    if (!auditReport) return []
    switch (auditSubTab) {
      case 'missingPosters':
        return auditReport.MissingPosters || []
      case 'missingTmdb':
        return auditReport.MissingTmdb || []
      case 'orphans':
        return auditReport.OrphanItems || []
    }
  }, [auditReport, auditSubTab])

  const statusBadge = getStatusBadge(health?.Status)
  const diskPct = Math.min(100, Math.max(0, health?.Disk.UsedPercent ?? 0))
  const budgetPct = Math.min(100, Math.max(0, health?.Scheduler.BudgetUsagePct ?? 0))

  return (
    <div className="space-y-8">
      <FeedbackBanner
        message={feedback?.message}
        type={feedback?.type}
        onClose={() => setFeedback(null)}
      />

      {/* 区块 1：系统健康诊断卡片 */}
      <section className="space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h2 className="flex items-center gap-2 text-base font-semibold text-zinc-100">
              <Activity className="h-4 w-4 text-amber-400" />
              <span>系统健康诊断</span>
              {health && (
                <span className={`rounded-full px-2 py-0.5 text-[10px] font-semibold ${statusBadge.cls}`}>
                  {statusBadge.label}
                </span>
              )}
            </h2>
            <p className="mt-1 text-xs text-zinc-400">
              实时监控 Go 运行时内存与协程、SQLite 数据库及 WAL 体积、缓存目录占用与数据盘剩余空间
            </p>
          </div>
          <div className="flex items-center gap-2">
            {health && (
              <span className="hidden font-mono text-[11px] text-zinc-500 sm:inline">
                {health.GoVersion} · {health.NumCpu} vCPU · 更新于 {formatUnixTime(health.CollectedAt)}
              </span>
            )}
            <button
              type="button"
              onClick={() => void fetchHealth(true)}
              disabled={healthRefreshing}
              className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/[0.04] px-3 py-1.5 text-xs font-medium text-zinc-300 transition-colors hover:border-amber-400/40 hover:text-amber-300 disabled:opacity-50"
            >
              <RefreshCw className={`h-3.5 w-3.5 ${healthRefreshing ? 'animate-spin' : ''}`} />
              <span>刷新指标</span>
            </button>
          </div>
        </div>

        {healthLoading ? (
          <div className="flex h-36 items-center justify-center rounded-2xl border border-white/8 bg-white/[0.02]">
            <Loader2 className="h-5 w-5 animate-spin text-amber-400" />
          </div>
        ) : !health ? (
          <div className="rounded-2xl border border-white/8 bg-white/[0.02] py-10 text-center text-xs text-zinc-500">
            暂未获取到健康诊断数据
          </div>
        ) : (
          <div className="space-y-4">
            <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
              {/* 内存与 Goroutine */}
              <div className="rounded-2xl border border-white/8 bg-white/[0.03] p-4">
                <div className="flex items-center justify-between text-xs text-zinc-400">
                  <span className="flex items-center gap-1.5 font-medium">
                    <Cpu className="h-3.5 w-3.5 text-amber-400" />
                    运行时内存 / 协程
                  </span>
                  <span className="font-mono text-[11px] text-zinc-500">
                    GC #{health.Memory.NumGC}
                  </span>
                </div>
                <div className="mt-2.5 flex items-baseline justify-between">
                  <p className="text-xl font-semibold text-zinc-100">
                    {formatBytes(health.Memory.AllocBytes)}
                  </p>
                  <span className="rounded-md bg-white/6 px-2 py-0.5 font-mono text-[11px] text-amber-300">
                    {health.NumGoroutine} goroutines
                  </span>
                </div>
                <p className="mt-2 text-[11px] text-zinc-500">
                  HeapInuse {formatBytes(health.Memory.HeapInuseBytes)} · Sys{' '}
                  {formatBytes(health.Memory.SysBytes)}
                </p>
              </div>

              {/* SQLite DB + WAL */}
              <div className="rounded-2xl border border-white/8 bg-white/[0.03] p-4">
                <div className="flex items-center justify-between text-xs text-zinc-400">
                  <span className="flex items-center gap-1.5 font-medium">
                    <Database className="h-3.5 w-3.5 text-amber-400" />
                    SQLite 数据库体积
                  </span>
                  <span className="font-mono text-[11px] text-zinc-500">WAL Mode</span>
                </div>
                <p className="mt-2.5 text-xl font-semibold text-zinc-100">
                  {formatBytes(health.Database.TotalBytes)}
                </p>
                <p className="mt-2 text-[11px] text-zinc-500">
                  DB {formatBytes(health.Database.DbBytes)} · WAL{' '}
                  {formatBytes(health.Database.WalBytes)} · SHM{' '}
                  {formatBytes(health.Database.ShmBytes)}
                </p>
              </div>

              {/* 缓存目录占用 */}
              <div className="rounded-2xl border border-white/8 bg-white/[0.03] p-4">
                <div className="flex items-center justify-between text-xs text-zinc-400">
                  <span className="flex items-center gap-1.5 font-medium">
                    <HardDrive className="h-3.5 w-3.5 text-amber-400" />
                    缓存目录占用
                  </span>
                  <span className="font-mono text-[11px] text-zinc-500">3 项缓存</span>
                </div>
                <p className="mt-2.5 text-xl font-semibold text-zinc-100">
                  {formatBytes(health.Cache.TotalBytes)}
                </p>
                <p className="mt-2 text-[11px] text-zinc-500">
                  图片 {formatBytes(health.Cache.ImagesBytes)} · 字幕{' '}
                  {formatBytes(health.Cache.SubtitlesBytes)} · 转码{' '}
                  {formatBytes(health.Cache.TranscodeBytes)}
                </p>
              </div>

              {/* 媒体条目 & 任务预算 */}
              <div className="rounded-2xl border border-white/8 bg-white/[0.03] p-4">
                <div className="flex items-center justify-between text-xs text-zinc-400">
                  <span className="flex items-center gap-1.5 font-medium">
                    <Sparkles className="h-3.5 w-3.5 text-amber-400" />
                    媒体规模 / 后台预算
                  </span>
                  <span className="font-mono text-[11px] text-zinc-500">
                    {health.Scheduler.ActiveTasks}/{health.Scheduler.TotalTasks} 任务活跃
                  </span>
                </div>
                <div className="mt-2.5 flex items-baseline justify-between">
                  <p className="text-xl font-semibold text-zinc-100">
                    {health.Library.TotalItems.toLocaleString()} 条目
                  </p>
                  <span className="font-mono text-[11px] text-zinc-400">
                    {health.Library.UserCount} 用户
                  </span>
                </div>
                <div className="mt-2 flex items-center gap-2">
                  <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-white/10">
                    <div
                      className="h-full rounded-full bg-amber-400 transition-all"
                      style={{ width: `${budgetPct}%` }}
                    />
                  </div>
                  <span className="font-mono text-[10px] text-zinc-400">
                    预算 {health.Scheduler.BudgetInUse}/{health.Scheduler.BudgetCapacity} (
                    {budgetPct.toFixed(0)}%)
                  </span>
                </div>
              </div>
            </div>

            {/* 数据盘剩余空间进度条 */}
            <div className="rounded-2xl border border-white/8 bg-white/[0.03] p-4">
              <div className="flex flex-wrap items-center justify-between gap-2 text-xs">
                <div className="flex items-center gap-2 text-zinc-200">
                  <HardDrive className="h-4 w-4 text-amber-400" />
                  <span className="font-medium">数据盘空间</span>
                  <span className="font-mono text-[11px] text-zinc-500">
                    {health.Disk.Path || '-'}
                  </span>
                </div>
                <div className="font-mono text-xs text-zinc-300">
                  可用 {formatBytes(health.Disk.AvailableBytes)} / 总计{' '}
                  {formatBytes(health.Disk.TotalBytes)} ({diskPct.toFixed(1)}% 已用)
                </div>
              </div>
              <div className="mt-2.5 h-2.5 w-full overflow-hidden rounded-full bg-white/10">
                <div
                  className={`h-full rounded-full transition-all ${
                    diskPct >= 95
                      ? 'bg-red-500'
                      : diskPct >= 85
                        ? 'bg-amber-500'
                        : 'bg-emerald-500'
                  }`}
                  style={{ width: `${diskPct}%` }}
                />
              </div>
            </div>
          </div>
        )}
      </section>

      {/* 区块 2：媒体库体检中心 */}
      <section className="space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h2 className="flex items-center gap-2 text-base font-semibold text-zinc-100">
              <SearchCheck className="h-4 w-4 text-amber-400" />
              <span>媒体库体检中心</span>
            </h2>
            <p className="mt-1 text-xs text-zinc-400">
              一键扫描缺失海报、未关联 TMDB 编号及磁盘物理路径已失效的孤立媒体记录
            </p>
          </div>
          <div className="flex items-center gap-2">
            {auditReport && (
              <span className="font-mono text-[11px] text-zinc-500">
                上次体检：{formatUnixTime(auditReport.ScannedAt)}
              </span>
            )}
            <button
              type="button"
              onClick={() => void handleRunAudit()}
              disabled={auditRunning}
              className="inline-flex items-center gap-1.5 rounded-lg bg-amber-500 px-3.5 py-1.5 text-xs font-semibold text-zinc-950 transition-colors hover:bg-amber-400 disabled:opacity-50"
            >
              {auditRunning ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <SearchCheck className="h-3.5 w-3.5" />
              )}
              <span>{auditRunning ? '体检扫描中...' : '一键全面体检'}</span>
            </button>
          </div>
        </div>

        <div className="overflow-hidden rounded-2xl border border-white/8 bg-white/[0.02]">
          {/* 分 Tab 导航 */}
          <div className="flex flex-wrap items-center justify-between gap-2 border-b border-white/8 px-4 py-3">
            <div className="flex flex-wrap items-center gap-2">
              <button
                type="button"
                onClick={() => setAuditSubTab('missingPosters')}
                className={`inline-flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium transition-colors ${
                  auditSubTab === 'missingPosters'
                    ? 'bg-amber-500/20 text-amber-300 ring-1 ring-amber-500/40'
                    : 'bg-white/[0.03] text-zinc-400 hover:text-zinc-200'
                }`}
              >
                <ImageOff className="h-3.5 w-3.5" />
                <span>缺失海报</span>
                <span className="rounded bg-black/30 px-1.5 py-0.5 font-mono text-[10px]">
                  {auditReport?.MissingPosters.length ?? 0}
                </span>
              </button>

              <button
                type="button"
                onClick={() => setAuditSubTab('missingTmdb')}
                className={`inline-flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium transition-colors ${
                  auditSubTab === 'missingTmdb'
                    ? 'bg-amber-500/20 text-amber-300 ring-1 ring-amber-500/40'
                    : 'bg-white/[0.03] text-zinc-400 hover:text-zinc-200'
                }`}
              >
                <FileWarning className="h-3.5 w-3.5" />
                <span>未刮削 TMDB</span>
                <span className="rounded bg-black/30 px-1.5 py-0.5 font-mono text-[10px]">
                  {auditReport?.MissingTmdb.length ?? 0}
                </span>
              </button>

              <button
                type="button"
                onClick={() => setAuditSubTab('orphans')}
                className={`inline-flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium transition-colors ${
                  auditSubTab === 'orphans'
                    ? 'bg-red-500/20 text-red-300 ring-1 ring-red-500/40'
                    : 'bg-white/[0.03] text-zinc-400 hover:text-zinc-200'
                }`}
              >
                <AlertTriangle className="h-3.5 w-3.5" />
                <span>磁盘孤立记录</span>
                <span className="rounded bg-black/30 px-1.5 py-0.5 font-mono text-[10px]">
                  {auditReport?.OrphanItems.length ?? 0}
                </span>
              </button>
            </div>

            {auditSubTab === 'orphans' && (auditReport?.OrphanItems.length ?? 0) > 0 && (
              <div className="flex items-center gap-2">
                <button
                  type="button"
                  onClick={handleToggleSelectAllOrphans}
                  className="rounded-lg border border-white/10 bg-white/[0.04] px-2.5 py-1 text-xs text-zinc-300 hover:text-zinc-100"
                >
                  {allOrphansSelected ? '取消全选' : '全选孤立项'}
                </button>
                <button
                  type="button"
                  disabled={selectedOrphans.size === 0 || cleaningOrphans}
                  onClick={() => void handleCleanupOrphans()}
                  className="inline-flex items-center gap-1.5 rounded-lg bg-red-500/20 px-3 py-1 text-xs font-semibold text-red-300 ring-1 ring-red-500/40 transition-colors hover:bg-red-500/30 disabled:opacity-40"
                >
                  {cleaningOrphans ? (
                    <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  ) : (
                    <Trash2 className="h-3.5 w-3.5" />
                  )}
                  <span>一键清理选中 ({selectedOrphans.size})</span>
                </button>
              </div>
            )}
          </div>

          {!auditReport ? (
            <div className="py-12 text-center text-xs text-zinc-500">
              点击右上角「一键全面体检」开始扫描媒体库异常项
            </div>
          ) : activeAuditItems.length === 0 ? (
            <div className="flex flex-col items-center justify-center gap-2 py-12 text-xs text-emerald-400">
              <CheckCircle2 className="h-5 w-5" />
              <span>该维度未发现任何异常条目，媒体库状态良好</span>
            </div>
          ) : (
            <div className="max-h-[340px] divide-y divide-white/6 overflow-y-auto">
              {activeAuditItems.map((item) => {
                const isOrphanTab = auditSubTab === 'orphans'
                const checked = selectedOrphans.has(item.Id)
                return (
                  <div
                    key={item.Id}
                    className="flex items-center justify-between gap-3 px-4 py-3 text-xs hover:bg-white/[0.03]"
                  >
                    <div className="flex min-w-0 items-center gap-3">
                      {isOrphanTab && (
                        <input
                          type="checkbox"
                          checked={checked}
                          onChange={() => toggleOrphanSelect(item.Id)}
                          className="h-4 w-4 rounded border-white/20 bg-black/40 accent-amber-500"
                        />
                      )}
                      <div className="min-w-0">
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="truncate font-medium text-zinc-100">{item.Name}</span>
                          <span className="rounded bg-white/8 px-1.5 py-0.5 text-[10px] text-zinc-400">
                            {item.Type}
                          </span>
                          {item.Year && (
                            <span className="font-mono text-[10px] text-zinc-500">{item.Year}</span>
                          )}
                        </div>
                        <p className="mt-0.5 truncate font-mono text-[11px] text-zinc-500">
                          {item.Path || '(无本地路径)'}
                        </p>
                      </div>
                    </div>
                    <span className="shrink-0 font-mono text-[10px] text-zinc-600">
                      {item.Id.slice(0, 8)}
                    </span>
                  </div>
                )
              })}
            </div>
          )}
        </div>
      </section>

      {/* 区块 3：低置信度刮削审校队列 */}
      <section className="space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h2 className="flex items-center gap-2 text-base font-semibold text-zinc-100">
              <ShieldAlert className="h-4 w-4 text-amber-400" />
              <span>低置信度刮削审校队列</span>
            </h2>
            <p className="mt-1 text-xs text-zinc-400">
              人工核对模糊匹配的刮削结果，一键应用正确候选元数据并锁定字段防止重扫覆盖
            </p>
          </div>

          <div className="flex flex-wrap items-center gap-2">
            <select
              value={reviewStatus}
              onChange={(e) => setReviewStatus(e.target.value as ReviewStatusFilter)}
              className="rounded-lg border border-white/10 bg-[#12121a] px-2.5 py-1.5 text-xs text-zinc-200 outline-none focus:border-amber-400/50"
            >
              <option value="pending">待审校</option>
              <option value="approved">已通过</option>
              <option value="dismissed">已忽略</option>
              <option value="all">全部状态</option>
            </select>
            <button
              type="button"
              onClick={() => void fetchReviews(reviewStatus)}
              className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/[0.04] px-3 py-1.5 text-xs font-medium text-zinc-300 transition-colors hover:border-amber-400/40 hover:text-amber-300"
            >
              <RefreshCw className={`h-3.5 w-3.5 ${reviewsLoading ? 'animate-spin' : ''}`} />
              <span>刷新</span>
            </button>
          </div>
        </div>

        {/* 应用时默认锁定字段选择 */}
        <div className="flex flex-wrap items-center gap-2 rounded-xl border border-white/8 bg-white/[0.02] px-4 py-2.5 text-xs">
          <span className="flex items-center gap-1.5 text-zinc-400">
            <Lock className="h-3.5 w-3.5 text-amber-400" />
            <span>应用候选时自动锁定字段：</span>
          </span>
          {DEFAULT_LOCK_FIELDS.map((field) => {
            const active = lockFields.includes(field)
            return (
              <button
                key={field}
                type="button"
                onClick={() => toggleLockField(field)}
                className={`rounded-md px-2 py-0.5 font-mono text-[11px] transition-colors ${
                  active
                    ? 'bg-amber-500/20 text-amber-300 ring-1 ring-amber-500/40'
                    : 'bg-white/5 text-zinc-500 hover:text-zinc-300'
                }`}
              >
                {field}
              </button>
            )
          })}
        </div>

        {reviewsLoading ? (
          <div className="flex h-32 items-center justify-center rounded-2xl border border-white/8 bg-white/[0.02]">
            <Loader2 className="h-5 w-5 animate-spin text-amber-400" />
          </div>
        ) : reviews.length === 0 ? (
          <div className="rounded-2xl border border-white/8 bg-white/[0.02] py-12 text-center text-xs text-zinc-500">
            当前筛选条件下暂无刮削审校条目
          </div>
        ) : (
          <div className="space-y-3">
            {reviews.map((audit) => {
              const isBusy = actingReviewId === audit.Id
              const isPending = audit.Status === 'pending'
              return (
                <div
                  key={audit.Id}
                  className="rounded-2xl border border-white/8 bg-white/[0.03] p-4"
                >
                  <div className="flex flex-wrap items-start justify-between gap-3">
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-2">
                        <h3 className="text-sm font-semibold text-zinc-100">{audit.ItemName}</h3>
                        <span className="rounded bg-white/8 px-1.5 py-0.5 text-[10px] text-zinc-400">
                          {audit.ItemType}
                        </span>
                        <span className="rounded-full bg-amber-500/15 px-2 py-0.5 font-mono text-[10px] text-amber-300">
                          置信度 {(audit.Confidence * 100).toFixed(0)}%
                        </span>
                        <span
                          className={`rounded-full px-2 py-0.5 text-[10px] font-medium ${
                            audit.Status === 'approved'
                              ? 'bg-emerald-500/15 text-emerald-300'
                              : audit.Status === 'dismissed'
                                ? 'bg-zinc-700/60 text-zinc-400'
                                : 'bg-amber-500/15 text-amber-300'
                          }`}
                        >
                          {audit.Status}
                        </span>
                      </div>
                      <p className="mt-1 truncate font-mono text-[11px] text-zinc-500">
                        {audit.Path || `ItemId: ${audit.ItemId}`}
                      </p>
                    </div>

                    {isPending && (
                      <button
                        type="button"
                        disabled={isBusy}
                        onClick={() => void handleDismissReview(audit)}
                        className="inline-flex items-center gap-1 rounded-lg border border-white/10 bg-white/[0.04] px-2.5 py-1 text-xs text-zinc-400 transition-colors hover:border-red-400/40 hover:text-red-300 disabled:opacity-50"
                      >
                        <XCircle className="h-3.5 w-3.5" />
                        <span>忽略</span>
                      </button>
                    )}
                  </div>

                  {/* 候选对比列表 */}
                  {audit.Candidates.length === 0 ? (
                    <p className="mt-3 text-xs text-zinc-500">暂无自动候选匹配项</p>
                  ) : (
                    <div className="mt-3 grid gap-2.5 md:grid-cols-2">
                      {audit.Candidates.map((cand, idx) => (
                        <div
                          key={`${audit.Id}-${cand.ProviderId}-${idx}`}
                          className="flex flex-col justify-between rounded-xl border border-white/8 bg-black/25 p-3"
                        >
                          <div>
                            <div className="flex items-start justify-between gap-2">
                              <div className="min-w-0">
                                <p className="truncate text-xs font-semibold text-zinc-100">
                                  {cand.Title}
                                  {cand.Year ? ` (${cand.Year})` : ''}
                                </p>
                                {cand.OriginalTitle && (
                                  <p className="truncate text-[11px] text-zinc-400">
                                    {cand.OriginalTitle}
                                  </p>
                                )}
                              </div>
                              <span className="shrink-0 font-mono text-[10px] text-amber-300">
                                {(cand.Confidence * 100).toFixed(0)}%
                              </span>
                            </div>
                            {cand.Overview && (
                              <p className="mt-1.5 line-clamp-2 text-[11px] leading-relaxed text-zinc-400">
                                {cand.Overview}
                              </p>
                            )}
                          </div>

                          <div className="mt-3 flex items-center justify-between border-t border-white/6 pt-2">
                            <span className="font-mono text-[10px] text-zinc-500">
                              {cand.ProviderName || 'Tmdb'}: {cand.ProviderId || '-'}
                              {typeof cand.Rating === 'number' ? ` · ★ ${cand.Rating.toFixed(1)}` : ''}
                            </span>
                            {isPending && (
                              <button
                                type="button"
                                disabled={isBusy}
                                onClick={() => void handleApproveReview(audit, cand)}
                                className="inline-flex items-center gap-1 rounded-lg bg-emerald-500/20 px-2.5 py-1 text-[11px] font-semibold text-emerald-300 ring-1 ring-emerald-500/35 transition-colors hover:bg-emerald-500/30 disabled:opacity-50"
                              >
                                {isBusy ? (
                                  <Loader2 className="h-3 w-3 animate-spin" />
                                ) : (
                                  <Check className="h-3 w-3" />
                                )}
                                <span>应用并锁定</span>
                              </button>
                            )}
                          </div>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              )
            })}
          </div>
        )}
      </section>
      {/* 区块 4：数据库备份与恢复 */}
      <section className="space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h2 className="flex items-center gap-2 text-base font-semibold text-zinc-100">
              <Archive className="h-4 w-4 text-amber-400" />
              <span>数据库备份与恢复</span>
            </h2>
            <p className="mt-1 text-xs text-zinc-400">
              在线创建 SQLite 数据库热备份快照，或从历史备份文件暂存恢复（重启后生效）
            </p>
          </div>
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={() => void fetchBackupInfo()}
              disabled={backupLoading}
              className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/[0.04] px-3 py-1.5 text-xs font-medium text-zinc-300 transition-colors hover:border-amber-400/40 hover:text-amber-300 disabled:opacity-50"
            >
              <RefreshCw className={`h-3.5 w-3.5 ${backupLoading ? 'animate-spin' : ''}`} />
              <span>刷新</span>
            </button>
            <button
              type="button"
              onClick={() => void handleCreateBackup()}
              disabled={creatingBackup}
              className="inline-flex items-center gap-1.5 rounded-lg bg-amber-500 px-3.5 py-1.5 text-xs font-semibold text-zinc-950 transition-colors hover:bg-amber-400 disabled:opacity-50"
            >
              {creatingBackup ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <Database className="h-3.5 w-3.5" />
              )}
              <span>{creatingBackup ? '创建备份中...' : '立即创建备份'}</span>
            </button>
          </div>
        </div>

        <div className="overflow-hidden rounded-2xl border border-white/8 bg-white/[0.02]">
          <div className="flex flex-wrap items-center justify-between gap-2 border-b border-white/8 px-4 py-3 text-xs">
            <div className="flex items-center gap-2 text-zinc-400">
              <HardDrive className="h-3.5 w-3.5 text-amber-400" />
              <span>备份目录：</span>
              <span className="font-mono text-zinc-200">
                {backupInfo?.BackupDirectory || '-'}
              </span>
            </div>
            <span className="font-mono text-[11px] text-zinc-500">
              共 {backupInfo?.Backups?.length ?? 0} 个备份文件
            </span>
          </div>

          {backupLoading ? (
            <div className="flex h-28 items-center justify-center">
              <Loader2 className="h-5 w-5 animate-spin text-amber-400" />
            </div>
          ) : !backupInfo?.Backups || backupInfo.Backups.length === 0 ? (
            <div className="py-10 text-center text-xs text-zinc-500">
              暂无数据库备份文件，点击右上角「立即创建备份」生成首个快照
            </div>
          ) : (
            <div className="divide-y divide-white/6">
              {backupInfo.Backups.map((backup) => (
                <div
                  key={backup.Name}
                  className="flex flex-wrap items-center justify-between gap-3 px-4 py-3 text-xs hover:bg-white/[0.03]"
                >
                  <div className="min-w-0 flex-1">
                    <p className="truncate font-mono text-sm font-medium text-zinc-100">
                      {backup.Name}
                    </p>
                    <p className="mt-0.5 text-[11px] text-zinc-400">
                      大小 {formatBytes(backup.Size)} · 创建于 {formatDateTime(backup.DateCreated)}
                    </p>
                  </div>
                  <button
                    type="button"
                    onClick={() => setRestoreTarget(backup)}
                    className="inline-flex shrink-0 items-center gap-1.5 rounded-lg border border-amber-500/35 bg-amber-500/10 px-3 py-1.5 text-xs font-medium text-amber-300 transition-colors hover:bg-amber-500/20"
                  >
                    <RotateCcw className="h-3.5 w-3.5" />
                    <span>恢复此备份</span>
                  </button>
                </div>
              ))}
            </div>
          )}
        </div>
      </section>

      <AdminModal
        open={Boolean(restoreTarget)}
        title="确认恢复数据库备份"
        subtitle={restoreTarget ? `目标快照：${restoreTarget.Name}` : undefined}
        maxWidthClass="max-w-md"
        onClose={() => {
          if (!restoringBackup) setRestoreTarget(null)
        }}
      >
        <div className="space-y-4">
          <div className="flex items-start gap-3 rounded-xl border border-amber-500/30 bg-amber-500/10 p-3.5 text-xs leading-relaxed text-amber-200">
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-amber-400" />
            <div>
              <p className="font-semibold text-amber-100">即将暂存数据库恢复文件</p>
              <p className="mt-1 text-amber-200/85">
                此操作会将所选备份暂存为 <code className="font-mono text-amber-100">ambra.db.restore</code>
                ，并在<strong>下次重启服务器</strong>时自动覆盖当前数据库。请确认已了解风险后再继续。
              </p>
            </div>
          </div>

          {restoreTarget && (
            <div className="rounded-xl border border-white/8 bg-white/[0.03] px-3.5 py-2.5 text-xs text-zinc-400">
              <p className="font-mono text-zinc-200">{restoreTarget.Name}</p>
              <p className="mt-1">
                大小：{formatBytes(restoreTarget.Size)} · 创建时间：
                {formatDateTime(restoreTarget.DateCreated)}
              </p>
            </div>
          )}

          <div className="flex justify-end gap-2 pt-2">
            <button
              type="button"
              disabled={restoringBackup}
              onClick={() => setRestoreTarget(null)}
              className="rounded-xl border border-white/10 bg-white/[0.04] px-4 py-2 text-xs font-medium text-zinc-300 hover:bg-white/10 disabled:opacity-50"
            >
              取消
            </button>
            <button
              type="button"
              disabled={restoringBackup}
              onClick={() => void handleConfirmRestore()}
              className="inline-flex items-center gap-1.5 rounded-xl bg-amber-500 px-4 py-2 text-xs font-semibold text-zinc-950 transition-colors hover:bg-amber-400 disabled:opacity-60"
            >
              {restoringBackup ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <RotateCcw className="h-3.5 w-3.5" />
              )}
              <span>确认恢复</span>
            </button>
          </div>
        </div>
      </AdminModal>
    </div>
  )
}
