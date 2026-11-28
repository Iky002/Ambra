import { useDeferredValue, useEffect, useMemo, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  AlertTriangle,
  ArrowDownToLine,
  CalendarClock,
  Clock,
  Download,
  FileText,
  Loader2,
  Play,
  Plus,
  RefreshCw,
  Save,
  Search,
  Square,
  Terminal,
  Trash2,
} from 'lucide-react'
import { ApiError, api, embyFetch } from '../../api/client'
import type { QueryResult, ScheduledTaskInfo, ServerLogFile, TaskTriggerInfo } from '../../api/types'
import { QueryErrorState } from '../../components/QueryErrorState'
import { isDemoMode } from '../../demo/demoMode'
import { useI18n, type TranslationVars } from '../../i18n/I18nContext'
import { AdminModal, adminLabel, FeedbackBanner, formatBytes, formatDateTime } from './adminUtils'

interface SyncJobItem {
  Id: string
  Name?: string
  Status?: string
  Progress?: number
  TargetId?: string
  TargetName?: string
  Quality?: string
  Profile?: string
  Bitrate?: number
  UserId?: string
  ItemCount?: number
  DateCreated?: string
  DateLastModified?: string
}

const TICKS_PER_MINUTE = 60 * 10_000_000
const TICKS_PER_HOUR = 3600 * 10_000_000

const DAY_KEY_MAP: Record<string, string> = {
  Sunday: 'admin.tasks.daySun',
  Monday: 'admin.tasks.dayMon',
  Tuesday: 'admin.tasks.dayTue',
  Wednesday: 'admin.tasks.dayWed',
  Thursday: 'admin.tasks.dayThu',
  Friday: 'admin.tasks.dayFri',
  Saturday: 'admin.tasks.daySat',
}

const DAY_ORDER = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'] as const

function formatTicksToHHMM(ticks: number): string {
  const totalMins = Math.round(ticks / TICKS_PER_MINUTE)
  const hh = String(Math.floor(totalMins / 60) % 24).padStart(2, '0')
  const mm = String(totalMins % 60).padStart(2, '0')
  return `${hh}:${mm}`
}

function formatTriggerLabel(
  trigger: TaskTriggerInfo,
  t: (key: string, vars?: TranslationVars) => string,
): string {
  if (trigger.Type === 'StartupTrigger') {
    return t('admin.tasks.triggerStartup')
  }
  if (trigger.Type === 'DailyTrigger') {
    return t('admin.tasks.triggerDaily', {
      time: formatTicksToHHMM(trigger.TimeOfDayTicks ?? 0),
    })
  }
  if (trigger.Type === 'IntervalTrigger') {
    const hours = (trigger.IntervalTicks ?? 0) / TICKS_PER_HOUR
    if (hours >= 1 && Number.isInteger(hours)) {
      return t('admin.tasks.triggerIntervalHours', { hours })
    }
    const mins = Math.max(1, Math.round((trigger.IntervalTicks ?? 0) / TICKS_PER_MINUTE))
    return t('admin.tasks.triggerIntervalMins', { mins })
  }
  if (trigger.Type === 'WeeklyTrigger') {
    const dayKey = trigger.DayOfWeek ? DAY_KEY_MAP[trigger.DayOfWeek] : undefined
    const dayLabel = dayKey ? t(dayKey) : trigger.DayOfWeek || t('admin.tasks.daySun')
    const timeStr =
      typeof trigger.TimeOfDayTicks === 'number'
        ? ` ${formatTicksToHHMM(trigger.TimeOfDayTicks)}`
        : ''
    return t('admin.tasks.triggerWeekly', { day: dayLabel, time: timeStr })
  }
  return trigger.Type
}

export function TasksLogsTab() {
  const { t, locale } = useI18n()
  const queryClient = useQueryClient()
  const [feedback, setFeedback] = useState<{ type: 'success' | 'error'; message: string } | null>(
    null,
  )
  const [runningTaskId, setRunningTaskId] = useState<string | null>(null)
  const [stoppingTaskId, setStoppingTaskId] = useState<string | null>(null)
  const [selectedLog, setSelectedLog] = useState<string | null>(null)
  const [logFilter, setLogFilter] = useState('')
  const deferredLogFilter = useDeferredValue(logFilter)

  // Sync / 离线转码任务状态
  const [confirmCancelSyncJob, setConfirmCancelSyncJob] = useState<SyncJobItem | null>(null)
  const [cancelSyncJobModalError, setCancelSyncJobModalError] = useState<string | null>(null)
  const [deletingSyncJobId, setDeletingSyncJobId] = useState<string | null>(null)

  // 多触发规则列表管理状态
  const [editingTriggersTask, setEditingTriggersTask] = useState<ScheduledTaskInfo | null>(null)
  const [triggersModalError, setTriggersModalError] = useState<string | null>(null)
  const [draftTriggers, setDraftTriggers] = useState<TaskTriggerInfo[]>([])
  const [newTriggerType, setNewTriggerType] = useState<
    'DailyTrigger' | 'WeeklyTrigger' | 'IntervalTrigger' | 'StartupTrigger'
  >('DailyTrigger')
  const [newDailyTime, setNewDailyTime] = useState('03:00')
  const [newWeeklyDay, setNewWeeklyDay] = useState<string>('Sunday')
  const [newIntervalHours, setNewIntervalHours] = useState(6)
  const [savingTriggers, setSavingTriggers] = useState(false)
  const timersRef = useRef<Set<number>>(new Set())

  useEffect(() => {
    const timers = timersRef.current
    return () => {
      for (const timerId of timers) {
        window.clearTimeout(timerId)
      }
      timers.clear()
    }
  }, [])

  const {
    data: rawTasks,
    isLoading: tasksLoading,
    isFetching: tasksFetching,
    isError: tasksIsError,
    error: tasksError,
    refetch: refetchTasks,
  } = useQuery({
    queryKey: ['admin', 'scheduledTasks', locale],
    queryFn: () => api.getScheduledTasks(),
    refetchInterval: (query) =>
      Array.isArray(query.state.data) &&
      query.state.data.some((item) => item.State?.toLowerCase() === 'running')
        ? 2000
        : false,
  })

  const tasks: ScheduledTaskInfo[] = Array.isArray(rawTasks) ? rawTasks : []

  const {
    data: syncJobs = [],
    isLoading: syncJobsLoading,
    isFetching: syncJobsFetching,
    isError: syncJobsIsError,
    error: syncJobsError,
    refetch: refetchSyncJobs,
  } = useQuery({
    queryKey: ['admin', 'syncJobs'],
    queryFn: async (): Promise<SyncJobItem[]> => {
      try {
        const res = await embyFetch<QueryResult<SyncJobItem> | SyncJobItem[]>('/emby/Sync/Jobs')
        if (Array.isArray(res)) return res
        if (res && Array.isArray(res.Items)) return res.Items
        return []
      } catch (err) {
        if (isDemoMode() && err instanceof ApiError && err.status === 404) {
          return []
        }
        throw err
      }
    },
    retry: false,
    refetchInterval: (query) =>
      Array.isArray(query.state.data) &&
      query.state.data.some((job) => {
        const st = (job.Status || '').toLowerCase()
        return st === 'converting' || st === 'transferring' || st === 'queued'
      })
        ? 3000
        : false,
  })

  const {
    data: rawLogFiles,
    isLoading: logsLoading,
    isFetching: logsFetching,
    isError: logsIsError,
    error: logsError,
    refetch: refetchLogs,
  } = useQuery({
    queryKey: ['admin', 'serverLogs'],
    queryFn: () => api.getServerLogs(),
  })

  const logFiles: ServerLogFile[] = Array.isArray(rawLogFiles)
    ? rawLogFiles
    : rawLogFiles && Array.isArray((rawLogFiles as { Items?: unknown }).Items)
      ? (rawLogFiles as { Items: ServerLogFile[] }).Items
      : []

  const {
    data: logContent,
    isLoading: contentLoading,
    isFetching: contentFetching,
    error: contentError,
    refetch: refetchLogContent,
  } = useQuery({
    queryKey: ['admin', 'serverLogContent', selectedLog],
    queryFn: () => api.getServerLogContent(selectedLog!),
    enabled: Boolean(selectedLog),
  })

  const scheduleRefetchTasks = (delayMs: number) => {
    const timerId = window.setTimeout(() => {
      timersRef.current.delete(timerId)
      void refetchTasks()
    }, delayMs)
    timersRef.current.add(timerId)
  }

  const handleRunTask = async (taskId: string, taskName: string) => {
    setRunningTaskId(taskId)
    setFeedback(null)
    try {
      await api.runScheduledTask(taskId)
      setFeedback({
        type: 'success',
        message: t('admin.tasks.taskTriggeredSuccess', { name: taskName }),
      })
      scheduleRefetchTasks(600)
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('admin.tasks.taskRunFailed'),
      })
    } finally {
      setRunningTaskId(null)
    }
  }

  const handleStopTask = async (taskId: string, taskName: string) => {
    setStoppingTaskId(taskId)
    setFeedback(null)
    try {
      await api.stopScheduledTask(taskId)
      setFeedback({
        type: 'success',
        message: t('admin.tasks.taskStopSuccess', { name: taskName }),
      })
      scheduleRefetchTasks(400)
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('admin.tasks.taskStopFailed'),
      })
    } finally {
      setStoppingTaskId(null)
    }
  }

  const handleOpenTriggers = (task: ScheduledTaskInfo) => {
    setTriggersModalError(null)
    setEditingTriggersTask(task)
    // 深拷贝完整保留所有现有触发器（如 StartupTrigger、DailyTrigger、自定义触发器等），绝不截断为单条
    setDraftTriggers(
      Array.isArray(task.Triggers) ? task.Triggers.map((trigger) => ({ ...trigger })) : [],
    )
    setNewTriggerType('DailyTrigger')
    setNewDailyTime('03:00')
    setNewWeeklyDay('Sunday')
    setNewIntervalHours(6)
  }

  const handleAddTrigger = () => {
    let next: TaskTriggerInfo
    if (newTriggerType === 'DailyTrigger') {
      const [hhRaw, mmRaw] = newDailyTime.split(':')
      const hh = Math.min(23, Math.max(0, Number(hhRaw) || 0))
      const mm = Math.min(59, Math.max(0, Number(mmRaw) || 0))
      next = {
        Type: 'DailyTrigger',
        TimeOfDayTicks: (hh * 60 + mm) * TICKS_PER_MINUTE,
      }
    } else if (newTriggerType === 'WeeklyTrigger') {
      const [hhRaw, mmRaw] = newDailyTime.split(':')
      const hh = Math.min(23, Math.max(0, Number(hhRaw) || 0))
      const mm = Math.min(59, Math.max(0, Number(mmRaw) || 0))
      next = {
        Type: 'WeeklyTrigger',
        DayOfWeek: newWeeklyDay,
        TimeOfDayTicks: (hh * 60 + mm) * TICKS_PER_MINUTE,
      }
    } else if (newTriggerType === 'IntervalTrigger') {
      const hours = Math.max(1, Number(newIntervalHours) || 1)
      next = {
        Type: 'IntervalTrigger',
        IntervalTicks: hours * TICKS_PER_HOUR,
      }
    } else {
      next = { Type: 'StartupTrigger' }
    }
    setDraftTriggers((prev) => [...prev, next])
  }

  const rawLogText = useMemo(() => {
    return typeof logContent === 'string'
      ? logContent
      : logContent
        ? JSON.stringify(logContent, null, 2)
        : ''
  }, [logContent])

  const handleDownloadLog = () => {
    if (!selectedLog) return
    const blob = new Blob([rawLogText], { type: 'text/plain;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = selectedLog.toLowerCase().endsWith('.log') ? selectedLog : `${selectedLog}.log`
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    window.setTimeout(() => {
      URL.revokeObjectURL(url)
    }, 1000)
  }

  const handleSaveTriggers = async () => {
    if (!editingTriggersTask) return
    setSavingTriggers(true)
    setFeedback(null)
    setTriggersModalError(null)
    try {
      const triggersPayload = draftTriggers.map((item) => ({ ...item }))
      await api.updateTaskTriggers(editingTriggersTask.Id, triggersPayload)
      setFeedback({
        type: 'success',
        message: t('admin.tasks.triggersSavedSuccess', { name: editingTriggersTask.Name }),
      })
      setEditingTriggersTask(null)
      setTriggersModalError(null)
      void refetchTasks()
    } catch (err) {
      const msg = err instanceof Error ? err.message : t('admin.tasks.triggersSaveFailed')
      setTriggersModalError(msg)
      setFeedback({
        type: 'error',
        message: msg,
      })
    } finally {
      setSavingTriggers(false)
    }
  }

  const handleDeleteSyncJob = async (job: SyncJobItem) => {
    setDeletingSyncJobId(job.Id)
    setFeedback(null)
    setCancelSyncJobModalError(null)
    try {
      await embyFetch<void>(`/emby/Sync/Jobs/${encodeURIComponent(job.Id)}`, {
        method: 'DELETE',
      })
      setConfirmCancelSyncJob(null)
      setCancelSyncJobModalError(null)
      await queryClient.invalidateQueries({ queryKey: ['admin', 'syncJobs'] })
      setFeedback({
        type: 'success',
        message: adminLabel(
          t,
          locale,
          'admin.tasks.syncJobDeletedSuccess',
          '已取消并移除同步/离线转码任务「{{name}}」',
          'Cancelled and removed sync job "{{name}}"',
          { name: job.Name || job.Id },
        ),
      })
    } catch (err) {
      const msg =
        err instanceof Error
          ? err.message
          : adminLabel(
              t,
              locale,
              'admin.tasks.syncJobDeleteFailed',
              '取消同步任务失败',
              'Failed to cancel sync job',
            )
      setCancelSyncJobModalError(msg)
      setFeedback({
        type: 'error',
        message: msg,
      })
    } finally {
      setDeletingSyncJobId(null)
    }
  }

  const rawLogLines = useMemo(() => (rawLogText ? rawLogText.split('\n') : []), [rawLogText])

  const filteredLogText = useMemo(() => {
    const keyword = deferredLogFilter.trim().toLowerCase()
    if (!keyword) return rawLogText
    return rawLogLines.filter((line) => line.toLowerCase().includes(keyword)).join('\n')
  }, [rawLogText, rawLogLines, deferredLogFilter])

  return (
    <div className="space-y-8">
      <FeedbackBanner
        message={feedback?.message}
        type={feedback?.type}
        onClose={() => setFeedback(null)}
      />

      {/* 计划任务列表 */}
      <section className="space-y-4">
        <div className="flex items-center justify-between gap-3">
          <div>
            <h2 className="flex items-center gap-2 text-base font-semibold text-zinc-100">
              <CalendarClock className="h-4 w-4 text-amber-400" />
              <span>{t('admin.scheduledTasks')}</span>
            </h2>
            <p className="mt-1 text-xs text-zinc-400">{t('admin.tasks.tasksSubtitle')}</p>
          </div>
          <button
            type="button"
            onClick={() => void refetchTasks()}
            className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/[0.04] px-3 py-1.5 text-xs font-medium text-zinc-300 transition-colors hover:border-amber-400/40 hover:text-amber-300"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${tasksFetching ? 'animate-spin' : ''}`} />
            <span>{t('common.refresh')}</span>
          </button>
        </div>

        {tasksLoading ? (
          <div className="flex h-32 items-center justify-center rounded-2xl border border-white/8 bg-white/[0.02]">
            <Loader2 className="h-5 w-5 animate-spin text-amber-400" />
          </div>
        ) : tasksIsError ? (
          <QueryErrorState
            compact
            title={t('admin.tasks.tasksLoadError')}
            error={tasksError}
            onRetry={() => void refetchTasks()}
          />
        ) : tasks.length === 0 ? (
          <div className="rounded-2xl border border-white/8 bg-white/[0.02] py-10 text-center text-xs text-zinc-500">
            {t('admin.tasks.tasksEmpty')}
          </div>
        ) : (
          <div className="grid gap-3 md:grid-cols-2">
            {tasks.map((task) => {
              const isRunning = task.State?.toLowerCase() === 'running'
              const isTriggering = runningTaskId === task.Id
              return (
                <div
                  key={task.Id}
                  className="flex flex-col justify-between rounded-2xl border border-white/8 bg-white/[0.03] p-4"
                >
                  <div>
                    <div className="flex items-start justify-between gap-3">
                      <div className="min-w-0">
                        <div className="flex flex-wrap items-center gap-2">
                          <h3 className="truncate text-sm font-semibold text-zinc-100">
                            {task.Name}
                          </h3>
                          {task.Category && (
                            <span className="rounded bg-white/8 px-1.5 py-0.5 text-[10px] text-zinc-400">
                              {task.Category}
                            </span>
                          )}
                        </div>
                        {task.Description && (
                          <p className="mt-1.5 text-xs leading-relaxed text-zinc-400">
                            {task.Description}
                          </p>
                        )}
                        {(task.Triggers?.length ?? 0) > 0 &&
                          editingTriggersTask?.Id !== task.Id && (
                            <div className="mt-2.5 flex flex-wrap gap-1.5">
                              {task.Triggers!.map((tr, idx) => (
                                <span
                                  key={`${tr.Type}-${idx}`}
                                  className="inline-flex items-center gap-1 rounded-md bg-white/[0.05] px-2 py-0.5 text-[11px] text-zinc-300"
                                >
                                  <Clock className="h-3 w-3 text-amber-400/80" />
                                  <span>{formatTriggerLabel(tr, t)}</span>
                                </span>
                              ))}
                            </div>
                          )}
                      </div>

                      <span
                        className={`shrink-0 rounded-full px-2 py-0.5 text-[10px] font-semibold ${
                          isRunning
                            ? 'bg-amber-500/20 text-amber-300 ring-1 ring-amber-500/40'
                            : 'bg-zinc-800 text-zinc-300 ring-1 ring-white/10'
                        }`}
                      >
                        {task.State || 'Idle'}
                      </span>
                    </div>

                    {isRunning && typeof task.CurrentProgressPercentage === 'number' && (
                      <div className="mt-3">
                        <div className="h-1.5 w-full overflow-hidden rounded-full bg-white/10">
                          <div
                            className="h-full rounded-full bg-amber-500 transition-all"
                            style={{ width: `${Math.min(100, task.CurrentProgressPercentage)}%` }}
                          />
                        </div>
                      </div>
                    )}
                  </div>

                  <div className="mt-4 flex items-center justify-between border-t border-white/6 pt-3">
                    <span className="font-mono text-[11px] text-zinc-500">
                      {task.LastExecutionResult?.EndTimeUtc
                        ? t('admin.tasks.lastEnded', {
                            time: formatDateTime(task.LastExecutionResult.EndTimeUtc),
                          })
                        : `Key: ${task.Key || task.Id}`}
                    </span>

                    <div className="flex items-center gap-2">
                      <button
                        type="button"
                        onClick={() => handleOpenTriggers(task)}
                        className="inline-flex items-center gap-1 rounded-lg border border-white/10 bg-white/[0.04] px-2.5 py-1.5 text-xs font-medium text-zinc-300 transition-colors hover:border-amber-400/40 hover:text-amber-300"
                      >
                        <Clock className="h-3.5 w-3.5" />
                        <span>{t('admin.tasks.triggersBtn')}</span>
                      </button>
                      {isRunning && (
                        <button
                          type="button"
                          disabled={stoppingTaskId === task.Id}
                          onClick={() => void handleStopTask(task.Id, task.Name)}
                          className="inline-flex items-center gap-1.5 rounded-lg border border-red-500/30 bg-red-500/15 px-3 py-1.5 text-xs font-semibold text-red-300 transition-colors hover:bg-red-500/25 disabled:opacity-50"
                        >
                          {stoppingTaskId === task.Id ? (
                            <Loader2 className="h-3.5 w-3.5 animate-spin" />
                          ) : (
                            <Square className="h-3 w-3 fill-current" />
                          )}
                          <span>{t('admin.dashboard.stopBtn')}</span>
                        </button>
                      )}
                      <button
                        type="button"
                        disabled={isRunning || isTriggering}
                        onClick={() => void handleRunTask(task.Id, task.Name)}
                        className="inline-flex items-center gap-1.5 rounded-lg bg-amber-500/15 px-3 py-1.5 text-xs font-semibold text-amber-300 ring-1 ring-amber-500/30 transition-colors hover:bg-amber-500/25 disabled:opacity-50"
                      >
                        {isTriggering ? (
                          <Loader2 className="h-3.5 w-3.5 animate-spin" />
                        ) : (
                          <Play className="h-3.5 w-3.5 fill-current" />
                        )}
                        <span>{t('admin.tasks.runNowBtn')}</span>
                      </button>
                    </div>
                  </div>
                </div>
              )
            })}
          </div>
        )}
      </section>

      {/* Sync / 离线转码任务监控 */}
      <section data-testid="sync-jobs-section" className="space-y-4">
        <div className="flex items-center justify-between gap-3">
          <div>
            <h2 className="flex items-center gap-2 text-base font-semibold text-zinc-100">
              <ArrowDownToLine className="h-4 w-4 text-amber-400" />
              <span>
                {adminLabel(
                  t,
                  locale,
                  'admin.tasks.syncJobsTitle',
                  '同步与离线转码任务',
                  'Sync / Offline Transcode Jobs',
                )}
              </span>
              <span className="rounded-md bg-white/8 px-2 py-0.5 text-xs font-medium text-zinc-300">
                {syncJobs.length}
              </span>
            </h2>
            <p className="mt-1 text-xs text-zinc-400">
              {adminLabel(
                t,
                locale,
                'admin.tasks.syncJobsSubtitle',
                '监控客户端离线下载与后台转码同步队列，支持随时取消或清理异常任务。',
                'Monitor client offline download and background sync transcode jobs, and cancel or clean up stalled jobs.',
              )}
            </p>
          </div>
          <button
            type="button"
            data-testid="refresh-sync-jobs-btn"
            onClick={() => void refetchSyncJobs()}
            className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/[0.04] px-3 py-1.5 text-xs font-medium text-zinc-300 transition-colors hover:border-amber-400/40 hover:text-amber-300"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${syncJobsFetching ? 'animate-spin' : ''}`} />
            <span>
              {adminLabel(
                t,
                locale,
                'admin.tasks.refreshSyncJobsBtn',
                '刷新同步队列',
                'Refresh Sync Jobs',
              )}
            </span>
          </button>
        </div>

        {syncJobsLoading ? (
          <div className="flex h-24 items-center justify-center rounded-2xl border border-white/8 bg-white/[0.02]">
            <Loader2 className="h-5 w-5 animate-spin text-amber-400" />
          </div>
        ) : syncJobsIsError ? (
          <div
            data-testid="sync-jobs-error"
            className="flex flex-wrap items-center justify-between gap-3 rounded-2xl border border-red-500/25 bg-red-500/10 px-4 py-3 text-xs text-red-200"
          >
            <div className="flex items-center gap-2">
              <AlertTriangle className="h-4 w-4 shrink-0 text-red-400" />
              <span>
                {adminLabel(
                  t,
                  locale,
                  'admin.tasks.syncJobsLoadError',
                  '加载同步任务失败：{{error}}',
                  'Failed to load sync jobs: {{error}}',
                  {
                    error:
                      syncJobsError instanceof Error ? syncJobsError.message : 'Unknown error',
                  },
                )}
              </span>
            </div>
            <button
              type="button"
              data-testid="retry-sync-jobs-btn"
              onClick={() => void refetchSyncJobs()}
              className="rounded-lg border border-red-400/30 bg-red-500/15 px-2.5 py-1 text-xs font-medium text-red-200 hover:bg-red-500/25"
            >
              {adminLabel(
                t,
                locale,
                'admin.tasks.retrySyncJobsBtn',
                '重试同步队列',
                'Retry Sync Jobs',
              )}
            </button>
          </div>
        ) : syncJobs.length === 0 ? (
          <div
            data-testid="sync-jobs-empty"
            className="rounded-2xl border border-white/8 bg-white/[0.02] py-8 text-center text-xs text-zinc-500"
          >
            {adminLabel(
              t,
              locale,
              'admin.tasks.syncJobsEmpty',
              '当前暂无同步或离线转码任务',
              'No active or queued sync / offline transcode jobs',
            )}
          </div>
        ) : (
          <div className="divide-y divide-white/6 overflow-hidden rounded-2xl border border-white/8 bg-white/[0.02]">
            {syncJobs.map((job) => {
              const status = job.Status || 'Queued'
              const statusLower = status.toLowerCase()
              const isActive =
                statusLower === 'converting' ||
                statusLower === 'transferring' ||
                statusLower === 'queued' ||
                statusLower === 'readytotransfer'
              const isFailed = statusLower === 'failed' || statusLower === 'cancelled'
              const progressPct =
                typeof job.Progress === 'number'
                  ? Math.min(100, Math.max(0, Math.round(job.Progress)))
                  : null
              const isDeleting = deletingSyncJobId === job.Id

              return (
                <div
                  key={job.Id}
                  data-testid={`sync-job-row-${job.Id}`}
                  className="flex flex-col gap-3 px-4 py-3.5 sm:flex-row sm:items-center sm:justify-between"
                >
                  <div className="min-w-0 flex-1 space-y-1.5">
                    <div className="flex flex-wrap items-center gap-2">
                      <p className="truncate text-sm font-semibold text-zinc-100">
                        {job.Name || job.Id}
                      </p>
                      <span
                        className={`rounded-full px-2 py-0.5 text-[10px] font-semibold ring-1 ${
                          isFailed
                            ? 'bg-red-500/15 text-red-300 ring-red-500/30'
                            : isActive
                              ? 'bg-amber-500/20 text-amber-300 ring-amber-500/40'
                              : 'bg-emerald-500/15 text-emerald-300 ring-emerald-500/30'
                        }`}
                      >
                        {status}
                      </span>
                      {(job.Quality || job.Profile) && (
                        <span className="rounded bg-white/8 px-1.5 py-0.5 font-mono text-[10px] text-zinc-300">
                          {[job.Profile, job.Quality].filter(Boolean).join(' · ')}
                        </span>
                      )}
                    </div>

                    <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-zinc-400">
                      {(job.TargetName || job.TargetId) && (
                        <span>
                          {adminLabel(
                            t,
                            locale,
                            'admin.tasks.syncJobTarget',
                            '目标设备: {{target}}',
                            'Target: {{target}}',
                            { target: job.TargetName || job.TargetId || '-' },
                          )}
                        </span>
                      )}
                      {typeof job.ItemCount === 'number' && (
                        <span>
                          {adminLabel(
                            t,
                            locale,
                            'admin.tasks.syncJobItems',
                            '共 {{count}} 项',
                            '{{count}} item(s)',
                            { count: job.ItemCount },
                          )}
                        </span>
                      )}
                      {job.DateCreated && (
                        <span className="font-mono text-[11px] text-zinc-500">
                          {formatDateTime(job.DateCreated)}
                        </span>
                      )}
                    </div>

                    {progressPct !== null && (
                      <div className="flex items-center gap-2.5 pt-0.5">
                        <div className="h-1.5 max-w-xs flex-1 overflow-hidden rounded-full bg-white/10">
                          <div
                            className="h-full rounded-full bg-amber-500 transition-all"
                            style={{ width: `${progressPct}%` }}
                          />
                        </div>
                        <span className="font-mono text-[11px] text-zinc-300">{progressPct}%</span>
                      </div>
                    )}
                  </div>

                  <div className="flex shrink-0 items-center justify-end">
                    <button
                      type="button"
                      data-testid={`cancel-sync-job-${job.Id}`}
                      disabled={isDeleting}
                      onClick={() => {
                        setCancelSyncJobModalError(null)
                        setConfirmCancelSyncJob(job)
                      }}
                      className="inline-flex items-center gap-1 rounded-lg border border-red-500/25 bg-red-500/10 px-2.5 py-1.5 text-xs font-medium text-red-300 transition-colors hover:bg-red-500/20 disabled:opacity-50"
                    >
                      {isDeleting ? (
                        <Loader2 className="h-3.5 w-3.5 animate-spin" />
                      ) : (
                        <Trash2 className="h-3.5 w-3.5" />
                      )}
                      <span>
                        {adminLabel(
                          t,
                          locale,
                          'admin.tasks.cancelSyncJobBtn',
                          '取消 / 移除',
                          'Cancel / Remove',
                        )}
                      </span>
                    </button>
                  </div>
                </div>
              )
            })}
          </div>
        )}
      </section>

      {/* 服务器日志文件与查看器 */}
      <section className="space-y-4">
        <div className="flex items-center justify-between gap-3">
          <div>
            <h2 className="flex items-center gap-2 text-base font-semibold text-zinc-100">
              <Terminal className="h-4 w-4 text-amber-400" />
              <span>{t('admin.serverLogs')}</span>
            </h2>
            <p className="mt-1 text-xs text-zinc-400">{t('admin.tasks.logsSubtitle')}</p>
          </div>
          <button
            type="button"
            onClick={() => void refetchLogs()}
            className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/[0.04] px-3 py-1.5 text-xs font-medium text-zinc-300 transition-colors hover:border-amber-400/40 hover:text-amber-300"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${logsFetching ? 'animate-spin' : ''}`} />
            <span>{t('admin.plugins.refreshList')}</span>
          </button>
        </div>

        <div className="grid gap-4 lg:grid-cols-12">
          {/* 左侧文件列表 */}
          <div className="overflow-hidden rounded-2xl border border-white/8 bg-white/[0.02] lg:col-span-4">
            <div className="border-b border-white/8 px-4 py-2.5 text-xs font-semibold text-zinc-400">
              {t('admin.tasks.logFilesHeader', { count: logFiles.length })}
            </div>

            {logsLoading ? (
              <div className="flex h-36 items-center justify-center">
                <Loader2 className="h-5 w-5 animate-spin text-amber-400" />
              </div>
            ) : logsIsError ? (
              <div className="p-3">
                <QueryErrorState
                  compact
                  title={t('admin.tasks.logsLoadError')}
                  error={logsError}
                  onRetry={() => void refetchLogs()}
                />
              </div>
            ) : logFiles.length === 0 ? (
              <div className="py-12 text-center text-xs text-zinc-500">
                {t('admin.tasks.logsEmpty')}
              </div>
            ) : (
              <div className="max-h-[420px] divide-y divide-white/6 overflow-y-auto">
                {logFiles.map((file) => {
                  const active = selectedLog === file.Name
                  return (
                    <button
                      key={file.Name}
                      type="button"
                      onClick={() => setSelectedLog(file.Name)}
                      className={`flex w-full items-start gap-2.5 px-4 py-3 text-left transition-colors ${
                        active
                          ? 'bg-amber-500/15 text-amber-200'
                          : 'text-zinc-300 hover:bg-white/[0.04]'
                      }`}
                    >
                      <FileText
                        className={`mt-0.5 h-4 w-4 shrink-0 ${
                          active ? 'text-amber-400' : 'text-zinc-500'
                        }`}
                      />
                      <div className="min-w-0 flex-1">
                        <p className="truncate font-mono text-xs font-medium">{file.Name}</p>
                        <div className="mt-1 flex items-center justify-between text-[10px] text-zinc-500">
                          <span>{formatBytes(file.Size)}</span>
                          <span>{formatDateTime(file.DateModified || file.DateCreated)}</span>
                        </div>
                      </div>
                    </button>
                  )
                })}
              </div>
            )}
          </div>

          {/* 右侧日志内容查看器 */}
          <div className="flex flex-col overflow-hidden rounded-2xl border border-white/8 bg-[#0b0b11] lg:col-span-8">
            <div className="flex flex-wrap items-center justify-between gap-2 border-b border-white/8 bg-white/[0.02] px-4 py-2.5">
              <span className="truncate font-mono text-xs font-medium text-zinc-300">
                {selectedLog || t('admin.tasks.selectLogPrompt')}
              </span>

              {selectedLog && (
                <div className="flex items-center gap-2">
                  <div className="relative">
                    <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-zinc-500" />
                    <input
                      type="text"
                      value={logFilter}
                      onChange={(e) => setLogFilter(e.target.value)}
                      placeholder={t('admin.tasks.filterLogPlaceholder')}
                      className="w-40 rounded-lg border border-white/10 bg-black/40 py-1 pl-8 pr-2.5 text-xs text-zinc-200 placeholder-zinc-500 outline-none focus:border-amber-400/50 sm:w-52"
                    />
                  </div>
                  <button
                    type="button"
                    disabled={contentLoading || Boolean(contentError)}
                    onClick={handleDownloadLog}
                    className="inline-flex items-center gap-1 rounded-lg border border-white/10 bg-white/[0.04] px-2.5 py-1 text-xs font-medium text-zinc-200 transition-colors hover:border-amber-400/40 hover:text-amber-300 disabled:cursor-not-allowed disabled:opacity-40"
                    title={t('admin.tasks.downloadLogTitle')}
                  >
                    <Download className="h-3.5 w-3.5" />
                    <span>{t('admin.downloadLog')}</span>
                  </button>
                  <button
                    type="button"
                    onClick={() => void refetchLogContent()}
                    className="rounded-lg border border-white/10 bg-white/[0.04] p-1.5 text-zinc-400 hover:text-amber-300"
                    title={t('admin.tasks.reloadLogTitle')}
                  >
                    <RefreshCw className={`h-3.5 w-3.5 ${contentFetching ? 'animate-spin' : ''}`} />
                  </button>
                </div>
              )}
            </div>

            <div className="h-[420px] overflow-auto p-4">
              {!selectedLog ? (
                <div className="flex h-full items-center justify-center text-xs text-zinc-500">
                  {t('admin.tasks.clickLogPrompt')}
                </div>
              ) : contentLoading ? (
                <div className="flex h-full items-center justify-center">
                  <Loader2 className="h-5 w-5 animate-spin text-amber-400" />
                </div>
              ) : contentError ? (
                <div className="flex h-full items-center justify-center text-xs text-red-300">
                  {t('admin.tasks.readLogFailed', {
                    error: contentError instanceof Error ? contentError.message : 'Unknown error',
                  })}
                </div>
              ) : (
                <pre className="whitespace-pre-wrap break-all font-mono text-[11px] leading-relaxed text-zinc-300">
                  {filteredLogText || t('admin.tasks.emptyOrNoMatch')}
                </pre>
              )}
            </div>
          </div>
        </div>
      </section>

      {/* 多触发规则管理弹窗 */}
      <AdminModal
        open={Boolean(editingTriggersTask)}
        title={t('admin.tasks.triggersModalTitle', { name: editingTriggersTask?.Name || '' })}
        subtitle={t('admin.tasks.triggersModalSubtitle')}
        maxWidthClass="max-w-lg"
        onClose={() => {
          setEditingTriggersTask(null)
          setTriggersModalError(null)
        }}
      >
        <div className="space-y-5">
          <FeedbackBanner
            type="error"
            message={triggersModalError || undefined}
            onClose={() => setTriggersModalError(null)}
          />
          {/* 已有触发规则列表（保留 StartupTrigger 等所有既有规则并支持增删多条） */}
          <div className="space-y-2">
            <p className="text-xs font-medium text-zinc-400">
              {t('admin.tasks.currentRulesCount', { count: draftTriggers.length })}
            </p>
            {draftTriggers.length === 0 ? (
              <div className="rounded-xl border border-white/8 bg-white/[0.02] py-5 text-center text-xs text-zinc-500">
                {t('admin.tasks.noAutoTriggers')}
              </div>
            ) : (
              <div className="divide-y divide-white/6 overflow-hidden rounded-xl border border-white/8 bg-white/[0.02]">
                {draftTriggers.map((trigger, idx) => (
                  <div
                    key={`${trigger.Type}-${idx}`}
                    className="flex items-center justify-between gap-3 px-3.5 py-2.5 text-xs"
                  >
                    <div className="flex items-center gap-2 text-zinc-200">
                      <Clock className="h-3.5 w-3.5 text-amber-400" />
                      <span>{formatTriggerLabel(trigger, t)}</span>
                    </div>
                    <button
                      type="button"
                      aria-label={t('admin.tasks.deleteRuleAria', { index: idx + 1 })}
                      onClick={() =>
                        setDraftTriggers((prev) => prev.filter((_, i) => i !== idx))
                      }
                      className="rounded-lg p-1 text-zinc-400 transition-colors hover:bg-red-500/15 hover:text-red-300"
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </button>
                  </div>
                ))}
              </div>
            )}
          </div>

          {/* 新增触发规则 */}
          <div className="space-y-3 rounded-xl border border-white/8 bg-white/[0.02] p-3.5">
            <p className="text-xs font-medium text-zinc-300">{t('admin.tasks.addRuleTitle')}</p>
            <div className="grid gap-3 sm:grid-cols-12">
              <div className="sm:col-span-5">
                <select
                  aria-label={t('admin.tasks.triggerTypeAria')}
                  value={newTriggerType}
                  onChange={(e) =>
                    setNewTriggerType(
                      e.target.value as
                        | 'DailyTrigger'
                        | 'WeeklyTrigger'
                        | 'IntervalTrigger'
                        | 'StartupTrigger',
                    )
                  }
                  className="w-full rounded-lg border border-white/10 bg-[#181822] px-2.5 py-2 text-xs text-zinc-100 outline-none focus:border-amber-400/60"
                >
                  <option value="DailyTrigger">{t('admin.tasks.typeDaily')}</option>
                  <option value="WeeklyTrigger">{t('admin.tasks.typeWeekly')}</option>
                  <option value="IntervalTrigger">{t('admin.tasks.typeInterval')}</option>
                  <option value="StartupTrigger">{t('admin.tasks.typeStartup')}</option>
                </select>
              </div>

              <div className="sm:col-span-4">
                {newTriggerType === 'DailyTrigger' ? (
                  <input
                    aria-label={t('admin.tasks.dailyTimeAria')}
                    type="time"
                    value={newDailyTime}
                    onChange={(e) => setNewDailyTime(e.target.value)}
                    className="w-full rounded-lg border border-white/10 bg-zinc-900/90 px-2.5 py-2 text-xs text-zinc-100 outline-none focus:border-amber-400/60"
                  />
                ) : newTriggerType === 'WeeklyTrigger' ? (
                  <div className="flex flex-col gap-2">
                    <select
                      aria-label={t('admin.tasks.weeklyDayAria')}
                      value={newWeeklyDay}
                      onChange={(e) => setNewWeeklyDay(e.target.value)}
                      className="w-full rounded-lg border border-white/10 bg-[#181822] px-2.5 py-2 text-xs text-zinc-100 outline-none focus:border-amber-400/60"
                    >
                      {DAY_ORDER.map((dayVal) => (
                        <option key={dayVal} value={dayVal}>
                          {t(DAY_KEY_MAP[dayVal])}
                        </option>
                      ))}
                    </select>
                    <input
                      aria-label={t('admin.tasks.weeklyTimeAria')}
                      type="time"
                      value={newDailyTime}
                      onChange={(e) => setNewDailyTime(e.target.value)}
                      className="w-full rounded-lg border border-white/10 bg-zinc-900/90 px-2.5 py-2 text-xs text-zinc-100 outline-none focus:border-amber-400/60"
                    />
                  </div>
                ) : newTriggerType === 'IntervalTrigger' ? (
                  <div className="flex items-center gap-1.5">
                    <input
                      aria-label={t('admin.tasks.intervalHoursAria')}
                      type="number"
                      min={1}
                      max={168}
                      value={newIntervalHours}
                      onChange={(e) => setNewIntervalHours(Number(e.target.value))}
                      className="w-full rounded-lg border border-white/10 bg-zinc-900/90 px-2.5 py-2 text-xs text-zinc-100 outline-none focus:border-amber-400/60"
                    />
                    <span className="shrink-0 text-xs text-zinc-400">
                      {t('admin.tasks.hoursUnit')}
                    </span>
                  </div>
                ) : (
                  <div className="flex h-full items-center text-xs text-zinc-500">
                    {t('admin.tasks.startupAutoHint')}
                  </div>
                )}
              </div>

              <div className="sm:col-span-3">
                <button
                  type="button"
                  onClick={handleAddTrigger}
                  className="inline-flex w-full items-center justify-center gap-1 rounded-lg border border-amber-500/35 bg-amber-500/15 px-3 py-2 text-xs font-semibold text-amber-300 transition-colors hover:bg-amber-500/25"
                >
                  <Plus className="h-3.5 w-3.5" />
                  <span>{t('admin.tasks.addBtn')}</span>
                </button>
              </div>
            </div>
          </div>

          <div className="flex justify-end gap-2 pt-2">
            <button
              type="button"
              onClick={() => {
                setEditingTriggersTask(null)
                setTriggersModalError(null)
              }}
              className="rounded-xl border border-white/10 bg-white/[0.04] px-4 py-2 text-xs font-medium text-zinc-300 hover:bg-white/10"
            >
              {t('common.cancel')}
            </button>
            <button
              type="button"
              disabled={savingTriggers}
              onClick={() => void handleSaveTriggers()}
              className="inline-flex items-center gap-1.5 rounded-xl bg-amber-500 px-4 py-2 text-xs font-semibold text-zinc-950 transition-colors hover:bg-amber-400 disabled:opacity-60"
            >
              {savingTriggers ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <Save className="h-3.5 w-3.5" />
              )}
              <span>{t('admin.tasks.saveRulesBtn')}</span>
            </button>
          </div>
        </div>
      </AdminModal>

      {/* 取消/移除离线转码同步任务确认弹窗 */}
      <AdminModal
        open={Boolean(confirmCancelSyncJob)}
        title={adminLabel(
          t,
          locale,
          'admin.tasks.confirmCancelSyncJobTitle',
          '确认取消并移除同步任务',
          'Confirm Sync Job Removal',
        )}
        subtitle={confirmCancelSyncJob?.Name || confirmCancelSyncJob?.Id}
        maxWidthClass="max-w-md"
        onClose={() => {
          if (!deletingSyncJobId) {
            setConfirmCancelSyncJob(null)
            setCancelSyncJobModalError(null)
          }
        }}
      >
        <div className="space-y-4">
          <FeedbackBanner
            type="error"
            message={cancelSyncJobModalError || undefined}
            onClose={() => setCancelSyncJobModalError(null)}
          />
          <div
            data-testid="confirm-cancel-sync-job-warning"
            className="flex items-start gap-3 rounded-xl border border-red-500/30 bg-red-500/10 p-3.5 text-xs leading-relaxed text-red-200"
          >
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-red-400" />
            <p>
              {adminLabel(
                t,
                locale,
                'admin.tasks.confirmCancelSyncJobDesc',
                '确定要取消并删除同步/离线转码任务「{{name}}」吗？进行中的后台转码与已生成的临时缓存文件将被清理。',
                'Are you sure you want to cancel and remove sync job "{{name}}"? Any running offline transcode process and temporary files for this job will be cleaned up.',
                { name: confirmCancelSyncJob?.Name || confirmCancelSyncJob?.Id || '' },
              )}
            </p>
          </div>

          <div className="flex justify-end gap-2 pt-2">
            <button
              type="button"
              disabled={Boolean(deletingSyncJobId)}
              onClick={() => {
                setConfirmCancelSyncJob(null)
                setCancelSyncJobModalError(null)
              }}
              className="rounded-xl border border-white/10 bg-white/[0.04] px-4 py-2 text-xs font-medium text-zinc-300 hover:bg-white/10 disabled:opacity-50"
            >
              {t('common.cancel')}
            </button>
            <button
              type="button"
              data-testid="confirm-cancel-sync-job-btn"
              disabled={Boolean(deletingSyncJobId) || !confirmCancelSyncJob}
              onClick={() => {
                if (confirmCancelSyncJob) {
                  void handleDeleteSyncJob(confirmCancelSyncJob)
                }
              }}
              className="inline-flex items-center gap-1.5 rounded-xl bg-red-500 px-4 py-2 text-xs font-semibold text-white transition-colors hover:bg-red-400 disabled:opacity-60"
            >
              {deletingSyncJobId ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <Trash2 className="h-3.5 w-3.5" />
              )}
              <span>
                {adminLabel(
                  t,
                  locale,
                  'admin.tasks.confirmCancelSyncJobBtn',
                  '确认移除任务',
                  'Remove Sync Job',
                )}
              </span>
            </button>
          </div>
        </div>
      </AdminModal>
    </div>
  )
}
