import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
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
import { useI18n } from '../../i18n/I18nContext'
import { AdminModal, adminLabel, FeedbackBanner, formatBytes, formatDateTime } from './adminUtils'

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
  ChaptersBytes?: number
  BifBytes?: number
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

function getStatusBadge(status: string | undefined, t: (key: string) => string) {
  switch (status) {
    case 'critical':
      return {
        label: t('admin.health.badgeCritical'),
        cls: 'bg-red-500/20 text-red-300 ring-1 ring-red-500/40',
      }
    case 'warning':
      return {
        label: t('admin.health.badgeWarning'),
        cls: 'bg-amber-500/20 text-amber-300 ring-1 ring-amber-500/40',
      }
    default:
      return {
        label: t('admin.health.badgeHealthy'),
        cls: 'bg-emerald-500/20 text-emerald-300 ring-1 ring-emerald-500/40',
      }
  }
}

export function HealthAndAuditTab() {
  const { t, locale } = useI18n()
  const [feedback, setFeedback] = useState<{ type: 'success' | 'error'; message: string } | null>(
    null,
  )

  const [health, setHealth] = useState<SystemHealthReport | null>(null)
  const [healthLoading, setHealthLoading] = useState(true)
  const [healthRefreshing, setHealthRefreshing] = useState(false)

  const [auditReport, setAuditReport] = useState<LibraryAuditReport | null>(null)
  const [auditRunning, setAuditRunning] = useState(false)
  const [auditSubTab, setAuditSubTab] = useState<AuditSubTab>('missingPosters')
  const [selectedOrphans, setSelectedOrphans] = useState<Set<string>>(new Set())
  const [cleaningOrphans, setCleaningOrphans] = useState(false)
  const [confirmOrphanCleanupOpen, setConfirmOrphanCleanupOpen] = useState(false)

  const [reviewStatus, setReviewStatus] = useState<ReviewStatusFilter>('pending')
  const [reviews, setReviews] = useState<ScrapeAuditItem[]>([])
  const [reviewsLoading, setReviewsLoading] = useState(true)
  const [lockFields, setLockFields] = useState<string[]>(DEFAULT_LOCK_FIELDS)
  const [actingReviewId, setActingReviewId] = useState<string | null>(null)
  const reviewReqSeqRef = useRef(0)
  const reviewAbortRef = useRef<AbortController | null>(null)

  const [backupInfo, setBackupInfo] = useState<BackupInfoResponse | null>(null)
  const [backupLoading, setBackupLoading] = useState(true)
  const [creatingBackup, setCreatingBackup] = useState(false)
  const [restoreTarget, setRestoreTarget] = useState<BackupFileInfo | null>(null)
  const [restoringBackup, setRestoringBackup] = useState(false)
  const [backupFeedback, setBackupFeedback] = useState<{
    type: 'success' | 'error'
    message: string
  } | null>(null)

  const fetchBackupInfo = useCallback(async () => {
    setBackupLoading(true)
    try {
      const data = await api.getBackupInfo()
      setBackupInfo(data)
    } catch (err) {
      setBackupFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('admin.health.backupListFailed'),
      })
    } finally {
      setBackupLoading(false)
    }
  }, [t])

  const fetchHealth = useCallback(
    async (isManual = false, silentError = false) => {
      if (isManual) {
        setHealthRefreshing(true)
      } else {
        setHealthLoading(true)
      }
      try {
        const data = await embyFetch<SystemHealthReport>('/api/admin/health')
        setHealth(data)
      } catch (err) {
        if (!silentError) {
          setFeedback({
            type: 'error',
            message: err instanceof Error ? err.message : t('admin.health.healthFetchFailed'),
          })
        }
      } finally {
        setHealthLoading(false)
        setHealthRefreshing(false)
      }
    },
    [t],
  )

  const fetchReviews = useCallback(
    async (status: ReviewStatusFilter) => {
      reviewAbortRef.current?.abort()
      const controller = new AbortController()
      reviewAbortRef.current = controller
      const seq = ++reviewReqSeqRef.current
      setReviewsLoading(true)
      try {
        const res = await embyFetch<ScrapeReviewListResponse>(
          `/api/admin/governance/review?status=${encodeURIComponent(status)}`,
          { signal: controller.signal },
        )
        if (seq !== reviewReqSeqRef.current || controller.signal.aborted) return
        setReviews(res.Items || [])
      } catch (err) {
        if (seq !== reviewReqSeqRef.current || controller.signal.aborted) return
        setFeedback({
          type: 'error',
          message: err instanceof Error ? err.message : t('admin.health.reviewsFetchFailed'),
        })
      } finally {
        if (seq === reviewReqSeqRef.current && !controller.signal.aborted) {
          setReviewsLoading(false)
        }
      }
    },
    [t],
  )

  useEffect(() => {
    void fetchHealth(false)
    void fetchBackupInfo()
  }, [fetchHealth, fetchBackupInfo])

  useEffect(() => {
    void fetchReviews(reviewStatus)
    return () => {
      reviewAbortRef.current?.abort()
    }
  }, [fetchReviews, reviewStatus])

  const handleCreateBackup = async () => {
    setCreatingBackup(true)
    setBackupFeedback(null)
    try {
      const created = await api.createBackup()
      setBackupFeedback({
        type: 'success',
        message: t('admin.health.backupCreatedSuccess', {
          name: created.Name,
          size: formatBytes(created.Size),
        }),
      })
      await fetchBackupInfo()
    } catch (err) {
      setBackupFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('admin.health.backupCreateFailed'),
      })
    } finally {
      setCreatingBackup(false)
    }
  }

  const handleConfirmRestore = async () => {
    if (!restoreTarget) return
    setRestoringBackup(true)
    setBackupFeedback(null)
    try {
      const res = await api.restoreBackup(restoreTarget.Name)
      setRestoreTarget(null)
      setBackupFeedback({
        type: 'success',
        message: t('admin.health.backupStagedSuccess', {
          name: res.BackupName,
          path: res.StagedPath || 'ambra.db.restore',
        }),
      })
      await fetchBackupInfo()
    } catch (err) {
      setBackupFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('admin.health.backupRestoreFailed'),
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
        message: t('admin.health.auditDoneSuccess', {
          checked: report.TotalChecked,
          posters: report.MissingPosters.length,
          tmdb: report.MissingTmdb.length,
          orphans: report.OrphanItems.length,
        }),
      })
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('admin.health.auditFailed'),
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
      setConfirmOrphanCleanupOpen(false)
      setFeedback({
        type: 'success',
        message: t('admin.health.orphansCleanedSuccess', { count: res.Deleted }),
      })
      setSelectedOrphans(new Set())
      try {
        const refreshed = await embyFetch<LibraryAuditReport>('/api/admin/governance/audit')
        setAuditReport(refreshed)
      } catch {
        /* keep cleanup success banner */
      }
      void fetchHealth(true, true)
    } catch (err) {
      setConfirmOrphanCleanupOpen(false)
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('admin.health.orphansCleanFailed'),
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
        message: t('admin.health.reviewApprovedSuccess', {
          title: candidate.Title,
          item: audit.ItemName,
          count: lockFields.length,
        }),
      })
      await fetchReviews(reviewStatus)
      void fetchHealth(true)
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('admin.health.reviewApproveFailed'),
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
        message: t('admin.health.reviewDismissedSuccess', { item: audit.ItemName }),
      })
      await fetchReviews(reviewStatus)
      void fetchHealth(true)
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('admin.health.reviewDismissFailed'),
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

  const statusBadge = getStatusBadge(health?.Status, t)
  const diskPct = Math.min(100, Math.max(0, health?.Disk.UsedPercent ?? 0))
  const budgetPct = Math.min(100, Math.max(0, health?.Scheduler.BudgetUsagePct ?? 0))

  return (
    <div className="space-y-8">
      <FeedbackBanner
        message={feedback?.message}
        type={feedback?.type}
        onClose={() => setFeedback(null)}
      />

      {/* Section 1: System Health Diagnostics */}
      <section className="space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h2 className="flex items-center gap-2 text-base font-semibold text-zinc-100">
              <Activity className="h-4 w-4 text-amber-400" />
              <span>{t('admin.health.sysHealthTitle')}</span>
              {health && (
                <span
                  className={`rounded-full px-2 py-0.5 text-[10px] font-semibold ${statusBadge.cls}`}
                >
                  {statusBadge.label}
                </span>
              )}
            </h2>
            <p className="mt-1 text-xs text-zinc-400">{t('admin.health.sysHealthDesc')}</p>
          </div>
          <div className="flex items-center gap-2">
            {health && (
              <span className="hidden font-mono text-[11px] text-zinc-500 sm:inline">
                {health.GoVersion} · {health.NumCpu} vCPU ·{' '}
                {t('admin.health.updatedAtPrefix', { time: formatUnixTime(health.CollectedAt) })}
              </span>
            )}
            <button
              type="button"
              onClick={() => void fetchHealth(true)}
              disabled={healthRefreshing}
              className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/[0.04] px-3 py-1.5 text-xs font-medium text-zinc-300 transition-colors hover:border-amber-400/40 hover:text-amber-300 disabled:opacity-50"
            >
              <RefreshCw className={`h-3.5 w-3.5 ${healthRefreshing ? 'animate-spin' : ''}`} />
              <span>{t('admin.health.refreshMetricsBtn')}</span>
            </button>
          </div>
        </div>

        {healthLoading ? (
          <div className="flex h-36 items-center justify-center rounded-2xl border border-white/8 bg-white/[0.02]">
            <Loader2 className="h-5 w-5 animate-spin text-amber-400" />
          </div>
        ) : !health ? (
          <div className="rounded-2xl border border-white/8 bg-white/[0.02] py-10 text-center text-xs text-zinc-500">
            {t('admin.health.noHealthData')}
          </div>
        ) : (
          <div className="space-y-4">
            <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
              <div className="rounded-2xl border border-white/8 bg-white/[0.03] p-4">
                <div className="flex items-center justify-between text-xs text-zinc-400">
                  <span className="flex items-center gap-1.5 font-medium">
                    <Cpu className="h-3.5 w-3.5 text-amber-400" />
                    {t('admin.health.memGoroutineTitle')}
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

              <div className="rounded-2xl border border-white/8 bg-white/[0.03] p-4">
                <div className="flex items-center justify-between text-xs text-zinc-400">
                  <span className="flex items-center gap-1.5 font-medium">
                    <Database className="h-3.5 w-3.5 text-amber-400" />
                    {t('admin.health.sqliteVolumeTitle')}
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

              <div className="rounded-2xl border border-white/8 bg-white/[0.03] p-4">
                <div className="flex items-center justify-between text-xs text-zinc-400">
                  <span className="flex items-center gap-1.5 font-medium">
                    <HardDrive className="h-3.5 w-3.5 text-amber-400" />
                    {t('admin.health.cacheDirTitle')}
                  </span>
                  <span className="font-mono text-[11px] text-zinc-500">
                    {t('admin.health.cache5Items')}
                  </span>
                </div>
                <p className="mt-2.5 text-xl font-semibold text-zinc-100">
                  {formatBytes(health.Cache.TotalBytes)}
                </p>
                <p className="mt-2 text-[11px] text-zinc-500">
                  {t('admin.health.cacheBreakdown', {
                    images: formatBytes(health.Cache.ImagesBytes),
                    chapters: formatBytes(health.Cache.ChaptersBytes ?? 0),
                    bif: formatBytes(health.Cache.BifBytes ?? 0),
                    subs: formatBytes(health.Cache.SubtitlesBytes),
                    transcode: formatBytes(health.Cache.TranscodeBytes),
                  })}
                </p>
                <p className="mt-1.5 text-[10px] leading-relaxed text-amber-400/80">
                  {t('admin.health.cacheCautionNote')}
                </p>
              </div>

              <div className="rounded-2xl border border-white/8 bg-white/[0.03] p-4">
                <div className="flex items-center justify-between text-xs text-zinc-400">
                  <span className="flex items-center gap-1.5 font-medium">
                    <Sparkles className="h-3.5 w-3.5 text-amber-400" />
                    {t('admin.health.scaleBudgetTitle')}
                  </span>
                  <span className="font-mono text-[11px] text-zinc-500">
                    {t('admin.health.tasksActiveCount', {
                      active: health.Scheduler.ActiveTasks,
                      total: health.Scheduler.TotalTasks,
                    })}
                  </span>
                </div>
                <div className="mt-2.5 flex items-baseline justify-between">
                  <p className="text-xl font-semibold text-zinc-100">
                    {t('admin.health.itemsCount', {
                      count: health.Library.TotalItems.toLocaleString(),
                    })}
                  </p>
                  <span className="font-mono text-[11px] text-zinc-400">
                    {t('admin.health.usersCount', { count: health.Library.UserCount })}
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
                    {t('admin.health.budgetUsage', {
                      inUse: health.Scheduler.BudgetInUse,
                      capacity: health.Scheduler.BudgetCapacity,
                      pct: budgetPct.toFixed(0),
                    })}
                  </span>
                </div>
              </div>
            </div>

            <div className="rounded-2xl border border-white/8 bg-white/[0.03] p-4">
              <div className="flex flex-wrap items-center justify-between gap-2 text-xs">
                <div className="flex items-center gap-2 text-zinc-200">
                  <HardDrive className="h-4 w-4 text-amber-400" />
                  <span className="font-medium">{t('admin.health.diskSpaceTitle')}</span>
                  <span className="font-mono text-[11px] text-zinc-500">
                    {health.Disk.Path || '-'}
                  </span>
                </div>
                <div className="font-mono text-xs text-zinc-300">
                  {t('admin.health.diskUsageSummary', {
                    avail: formatBytes(health.Disk.AvailableBytes),
                    total: formatBytes(health.Disk.TotalBytes),
                    pct: diskPct.toFixed(1),
                  })}
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

      {/* Section 2: Library Integrity Audit Center */}
      <section className="space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h2 className="flex items-center gap-2 text-base font-semibold text-zinc-100">
              <SearchCheck className="h-4 w-4 text-amber-400" />
              <span>{t('admin.health.auditCenterTitle')}</span>
            </h2>
            <p className="mt-1 text-xs text-zinc-400">{t('admin.health.auditCenterDesc')}</p>
          </div>
          <div className="flex items-center gap-2">
            {auditReport && (
              <span className="font-mono text-[11px] text-zinc-500">
                {t('admin.health.lastAuditPrefix', { time: formatUnixTime(auditReport.ScannedAt) })}
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
              <span>
                {auditRunning ? t('admin.health.auditingBtn') : t('admin.health.runAuditBtn')}
              </span>
            </button>
          </div>
        </div>

        <div className="overflow-hidden rounded-2xl border border-white/8 bg-white/[0.02]">
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
                <span>{t('admin.health.missingPostersTab')}</span>
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
                <span>{t('admin.health.missingTmdbTab')}</span>
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
                <span>{t('admin.health.orphanItemsTab')}</span>
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
                  {allOrphansSelected
                    ? t('admin.health.deselectAllOrphans')
                    : t('admin.health.selectAllOrphans')}
                </button>
                <button
                  type="button"
                  disabled={selectedOrphans.size === 0 || cleaningOrphans}
                  onClick={() => setConfirmOrphanCleanupOpen(true)}
                  className="inline-flex items-center gap-1.5 rounded-lg bg-red-500/20 px-3 py-1 text-xs font-semibold text-red-300 ring-1 ring-red-500/40 transition-colors hover:bg-red-500/30 disabled:opacity-40"
                >
                  {cleaningOrphans ? (
                    <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  ) : (
                    <Trash2 className="h-3.5 w-3.5" />
                  )}
                  <span>
                    {t('admin.health.cleanupSelectedBtn', { count: selectedOrphans.size })}
                  </span>
                </button>
              </div>
            )}
          </div>

          {!auditReport ? (
            <div className="py-12 text-center text-xs text-zinc-500">
              {t('admin.health.auditIdlePrompt')}
            </div>
          ) : activeAuditItems.length === 0 ? (
            <div className="flex flex-col items-center justify-center gap-2 py-12 text-xs text-emerald-400">
              <CheckCircle2 className="h-5 w-5" />
              <span>{t('admin.health.auditCleanState')}</span>
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
                          {item.Path || t('admin.health.noLocalPath')}
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

      {/* Section 3: Low-Confidence Scrape Review Queue */}
      <section className="space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h2 className="flex items-center gap-2 text-base font-semibold text-zinc-100">
              <ShieldAlert className="h-4 w-4 text-amber-400" />
              <span>{t('admin.health.reviewQueueTitle')}</span>
            </h2>
            <p className="mt-1 text-xs text-zinc-400">{t('admin.health.reviewQueueDesc')}</p>
          </div>

          <div className="flex flex-wrap items-center gap-2">
            <select
              value={reviewStatus}
              onChange={(e) => setReviewStatus(e.target.value as ReviewStatusFilter)}
              className="rounded-lg border border-white/10 bg-[#12121a] px-2.5 py-1.5 text-xs text-zinc-200 outline-none focus:border-amber-400/50"
            >
              <option value="pending">{t('admin.health.statusPending')}</option>
              <option value="approved">{t('admin.health.statusApproved')}</option>
              <option value="dismissed">{t('admin.health.statusDismissed')}</option>
              <option value="all">{t('admin.health.statusAll')}</option>
            </select>
            <button
              type="button"
              onClick={() => void fetchReviews(reviewStatus)}
              className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/[0.04] px-3 py-1.5 text-xs font-medium text-zinc-300 transition-colors hover:border-amber-400/40 hover:text-amber-300"
            >
              <RefreshCw className={`h-3.5 w-3.5 ${reviewsLoading ? 'animate-spin' : ''}`} />
              <span>{t('common.refresh')}</span>
            </button>
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-2 rounded-xl border border-white/8 bg-white/[0.02] px-4 py-2.5 text-xs">
          <span className="flex items-center gap-1.5 text-zinc-400">
            <Lock className="h-3.5 w-3.5 text-amber-400" />
            <span>{t('admin.health.autoLockFieldsLabel')}</span>
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
            {t('admin.health.reviewsEmpty')}
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
                          {t('admin.health.confidenceBadge', {
                            pct: (audit.Confidence * 100).toFixed(0),
                          })}
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
                        <span>{t('admin.health.ignoreBtn')}</span>
                      </button>
                    )}
                  </div>

                  {audit.Candidates.length === 0 ? (
                    <p className="mt-3 text-xs text-zinc-500">{t('admin.health.noCandidates')}</p>
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
                              {typeof cand.Rating === 'number'
                                ? ` · ★ ${cand.Rating.toFixed(1)}`
                                : ''}
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
                                <span>{t('admin.health.applyAndLockBtn')}</span>
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

      {/* Section 4: Database Backup & Restore */}
      <section data-testid="backup-restore-section" className="space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h2 className="flex items-center gap-2 text-base font-semibold text-zinc-100">
              <Archive className="h-4 w-4 text-amber-400" />
              <span>{t('admin.health.backupSectionTitle')}</span>
            </h2>
            <p className="mt-1 text-xs text-zinc-400">{t('admin.health.backupSectionDesc')}</p>
          </div>
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={() => void fetchBackupInfo()}
              disabled={backupLoading}
              className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/[0.04] px-3 py-1.5 text-xs font-medium text-zinc-300 transition-colors hover:border-amber-400/40 hover:text-amber-300 disabled:opacity-50"
            >
              <RefreshCw className={`h-3.5 w-3.5 ${backupLoading ? 'animate-spin' : ''}`} />
              <span>{t('common.refresh')}</span>
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
              <span>
                {creatingBackup
                  ? t('admin.health.creatingBackupBtn')
                  : t('admin.health.createBackupBtn')}
              </span>
            </button>
          </div>
        </div>

        <div
          data-testid="backup-restore-card"
          className="overflow-hidden rounded-2xl border border-white/8 bg-white/[0.02]"
        >
          {backupFeedback && (
            <div className="border-b border-white/8 px-4 py-3">
              <FeedbackBanner
                message={backupFeedback.message}
                type={backupFeedback.type}
                onClose={() => setBackupFeedback(null)}
              />
            </div>
          )}
          <div className="flex flex-wrap items-center justify-between gap-2 border-b border-white/8 px-4 py-3 text-xs">
            <div className="flex items-center gap-2 text-zinc-400">
              <HardDrive className="h-3.5 w-3.5 text-amber-400" />
              <span>{t('admin.health.backupDirLabel')}</span>
              <span className="font-mono text-zinc-200">{backupInfo?.BackupDirectory || '-'}</span>
            </div>
            <span className="font-mono text-[11px] text-zinc-500">
              {t('admin.health.backupCountLabel', { count: backupInfo?.Backups?.length ?? 0 })}
            </span>
          </div>

          {backupLoading ? (
            <div className="flex h-28 items-center justify-center">
              <Loader2 className="h-5 w-5 animate-spin text-amber-400" />
            </div>
          ) : !backupInfo?.Backups || backupInfo.Backups.length === 0 ? (
            <div className="py-10 text-center text-xs text-zinc-500">
              {t('admin.health.backupsEmpty')}
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
                      {t('admin.health.backupSizeCreated', {
                        size: formatBytes(backup.Size),
                        time: formatDateTime(backup.DateCreated),
                      })}
                    </p>
                  </div>
                  <button
                    type="button"
                    onClick={() => setRestoreTarget(backup)}
                    className="inline-flex shrink-0 items-center gap-1.5 rounded-lg border border-amber-500/35 bg-amber-500/10 px-3 py-1.5 text-xs font-medium text-amber-300 transition-colors hover:bg-amber-500/20"
                  >
                    <RotateCcw className="h-3.5 w-3.5" />
                    <span>{t('admin.health.restoreThisBtn')}</span>
                  </button>
                </div>
              ))}
            </div>
          )}
        </div>
      </section>

      <AdminModal
        open={Boolean(restoreTarget)}
        title={t('admin.health.restoreModalTitle')}
        subtitle={
          restoreTarget
            ? t('admin.health.restoreModalSubtitle', { name: restoreTarget.Name })
            : undefined
        }
        maxWidthClass="max-w-md"
        onClose={() => {
          if (!restoringBackup) setRestoreTarget(null)
        }}
      >
        <div className="space-y-4">
          <div className="flex items-start gap-3 rounded-xl border border-amber-500/30 bg-amber-500/10 p-3.5 text-xs leading-relaxed text-amber-200">
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-amber-400" />
            <div>
              <p className="font-semibold text-amber-100">{t('admin.health.restoreWarnTitle')}</p>
              <p className="mt-1 text-amber-200/85">{t('admin.health.restoreWarnDesc')}</p>
            </div>
          </div>

          {restoreTarget && (
            <div className="rounded-xl border border-white/8 bg-white/[0.03] px-3.5 py-2.5 text-xs text-zinc-400">
              <p className="font-mono text-zinc-200">{restoreTarget.Name}</p>
              <p className="mt-1">
                {t('admin.health.backupSizeCreated', {
                  size: formatBytes(restoreTarget.Size),
                  time: formatDateTime(restoreTarget.DateCreated),
                })}
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
              {t('common.cancel')}
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
              <span>{t('admin.health.confirmRestoreBtn')}</span>
            </button>
          </div>
        </div>
      </AdminModal>

      <AdminModal
        open={confirmOrphanCleanupOpen}
        title={adminLabel(
          t,
          locale,
          'admin.health.orphanCleanupModalTitle',
          '确认清理磁盘孤立记录',
          'Confirm Orphaned Records Cleanup',
        )}
        subtitle={adminLabel(
          t,
          locale,
          'admin.health.orphanCleanupModalSubtitle',
          '已选中 {{count}} 条孤立媒体记录',
          '{{count}} orphaned media record(s) selected',
          { count: selectedOrphans.size },
        )}
        maxWidthClass="max-w-md"
        onClose={() => {
          if (!cleaningOrphans) setConfirmOrphanCleanupOpen(false)
        }}
      >
        <div className="space-y-4">
          <div
            role="alert"
            data-testid="orphan-cleanup-nas-warning"
            className="flex items-start gap-3 rounded-xl border border-red-500/35 bg-red-500/12 p-3.5 text-xs leading-relaxed text-red-100"
          >
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-red-400" />
            <div className="space-y-1.5">
              <p className="font-semibold text-red-100">
                {adminLabel(
                  t,
                  locale,
                  'admin.health.orphanCleanupNasWarnTitle',
                  '请先确认 NAS / 网络存储挂载已正常在线！',
                  'Verify NAS / Network Drives Are Mounted & Online First!',
                )}
              </p>
              <p className="text-red-200/90">
                {adminLabel(
                  t,
                  locale,
                  'admin.health.orphanCleanupNasWarnDesc',
                  '若 SMB / NFS / 云盘挂载点因掉线或未挂载导致路径暂时不可访问，正常影片也会被判定为孤立项。继续执行将从数据库中永久移除这 {{count}} 条媒体记录及其关联观看数据！',
                  'If an SMB / NFS / cloud mount is temporarily offline or unmounted, valid media files may appear missing. Proceeding will permanently remove {{count}} media record(s) and associated watch metadata from the database!',
                  { count: selectedOrphans.size },
                )}
              </p>
            </div>
          </div>

          <div className="flex justify-end gap-2 pt-2">
            <button
              type="button"
              disabled={cleaningOrphans}
              onClick={() => setConfirmOrphanCleanupOpen(false)}
              className="rounded-xl border border-white/10 bg-white/[0.04] px-4 py-2 text-xs font-medium text-zinc-300 hover:bg-white/10 disabled:opacity-50"
            >
              {t('common.cancel')}
            </button>
            <button
              type="button"
              disabled={cleaningOrphans || selectedOrphans.size === 0}
              onClick={() => void handleCleanupOrphans()}
              className="inline-flex items-center gap-1.5 rounded-xl bg-red-600 px-4 py-2 text-xs font-semibold text-white transition-colors hover:bg-red-500 disabled:opacity-60"
            >
              {cleaningOrphans ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <Trash2 className="h-3.5 w-3.5" />
              )}
              <span>
                {adminLabel(
                  t,
                  locale,
                  'admin.health.confirmCleanupOrphansBtn',
                  '确认清理 ({{count}})',
                  'Confirm Cleanup ({{count}})',
                  { count: selectedOrphans.size },
                )}
              </span>
            </button>
          </div>
        </div>
      </AdminModal>
    </div>
  )
}
