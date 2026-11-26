import { useEffect, useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
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
import { api } from '../../api/client'
import type { ScheduledTaskInfo, ServerLogFile, TaskTriggerInfo } from '../../api/types'
import { QueryErrorState } from '../../components/QueryErrorState'
import { AdminModal, FeedbackBanner, formatBytes, formatDateTime } from './adminUtils'

const TICKS_PER_MINUTE = 60 * 10_000_000
const TICKS_PER_HOUR = 3600 * 10_000_000

const DAYS_OF_WEEK = [
  { value: 'Sunday', label: '周日 (Sunday)' },
  { value: 'Monday', label: '周一 (Monday)' },
  { value: 'Tuesday', label: '周二 (Tuesday)' },
  { value: 'Wednesday', label: '周三 (Wednesday)' },
  { value: 'Thursday', label: '周四 (Thursday)' },
  { value: 'Friday', label: '周五 (Friday)' },
  { value: 'Saturday', label: '周六 (Saturday)' },
] as const

function formatTicksToHHMM(ticks: number): string {
  const totalMins = Math.round(ticks / TICKS_PER_MINUTE)
  const hh = String(Math.floor(totalMins / 60) % 24).padStart(2, '0')
  const mm = String(totalMins % 60).padStart(2, '0')
  return `${hh}:${mm}`
}

function formatTriggerLabel(trigger: TaskTriggerInfo): string {
  if (trigger.Type === 'StartupTrigger') {
    return '服务器启动时触发'
  }
  if (trigger.Type === 'DailyTrigger') {
    return `每日 ${formatTicksToHHMM(trigger.TimeOfDayTicks ?? 0)} 执行`
  }
  if (trigger.Type === 'IntervalTrigger') {
    const hours = (trigger.IntervalTicks ?? 0) / TICKS_PER_HOUR
    if (hours >= 1 && Number.isInteger(hours)) {
      return `每隔 ${hours} 小时执行`
    }
    const mins = Math.max(1, Math.round((trigger.IntervalTicks ?? 0) / TICKS_PER_MINUTE))
    return `每隔 ${mins} 分钟执行`
  }
  if (trigger.Type === 'WeeklyTrigger') {
    const dayLabel =
      DAYS_OF_WEEK.find((d) => d.value === trigger.DayOfWeek)?.label ||
      trigger.DayOfWeek ||
      'Sunday'
    const timeStr =
      typeof trigger.TimeOfDayTicks === 'number'
        ? ` ${formatTicksToHHMM(trigger.TimeOfDayTicks)}`
        : ''
    return `每周 ${dayLabel}${timeStr} 执行`
  }
  return trigger.Type
}

export function TasksLogsTab() {
  const [feedback, setFeedback] = useState<{ type: 'success' | 'error'; message: string } | null>(
    null,
  )
  const [runningTaskId, setRunningTaskId] = useState<string | null>(null)
  const [stoppingTaskId, setStoppingTaskId] = useState<string | null>(null)
  const [selectedLog, setSelectedLog] = useState<string | null>(null)
  const [logFilter, setLogFilter] = useState('')

  // 触发规则弹窗状态
  const [editingTriggersTask, setEditingTriggersTask] = useState<ScheduledTaskInfo | null>(null)
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
    queryKey: ['admin', 'scheduledTasks'],
    queryFn: () => api.getScheduledTasks(),
    refetchInterval: (query) =>
      Array.isArray(query.state.data) &&
      query.state.data.some((t) => t.State?.toLowerCase() === 'running')
        ? 2000
        : false,
  })

  const tasks: ScheduledTaskInfo[] = Array.isArray(rawTasks) ? rawTasks : []

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
      setFeedback({ type: 'success', message: `已触发计划任务「${taskName}」` })
      scheduleRefetchTasks(600)
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '运行计划任务失败',
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
      setFeedback({ type: 'success', message: `已请求停止计划任务「${taskName}」` })
      scheduleRefetchTasks(400)
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '停止计划任务失败',
      })
    } finally {
      setStoppingTaskId(null)
    }
  }

  const handleOpenTriggers = (task: ScheduledTaskInfo) => {
    setEditingTriggersTask(task)
    setDraftTriggers(Array.isArray(task.Triggers) ? task.Triggers.map((t) => ({ ...t })) : [])
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
    try {
      await api.updateTaskTriggers(editingTriggersTask.Id, draftTriggers)
      setFeedback({
        type: 'success',
        message: `已更新计划任务「${editingTriggersTask.Name}」的触发规则`,
      })
      setEditingTriggersTask(null)
      void refetchTasks()
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '保存触发规则失败',
      })
    } finally {
      setSavingTriggers(false)
    }
  }

  const filteredLogText = useMemo(() => {
    const raw =
      typeof logContent === 'string'
        ? logContent
        : logContent
          ? JSON.stringify(logContent, null, 2)
          : ''
    if (!logFilter.trim()) return raw
    const keyword = logFilter.trim().toLowerCase()
    return raw
      .split('\n')
      .filter((line) => line.toLowerCase().includes(keyword))
      .join('\n')
  }, [logContent, logFilter])

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
              <span>计划任务</span>
            </h2>
            <p className="mt-1 text-xs text-zinc-400">
              查看系统维护与媒体索引任务状态，支持一键手动触发运行
            </p>
          </div>
          <button
            type="button"
            onClick={() => void refetchTasks()}
            className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/[0.04] px-3 py-1.5 text-xs font-medium text-zinc-300 transition-colors hover:border-amber-400/40 hover:text-amber-300"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${tasksFetching ? 'animate-spin' : ''}`} />
            <span>刷新</span>
          </button>
        </div>

        {tasksLoading ? (
          <div className="flex h-32 items-center justify-center rounded-2xl border border-white/8 bg-white/[0.02]">
            <Loader2 className="h-5 w-5 animate-spin text-amber-400" />
          </div>
        ) : tasksIsError ? (
          <QueryErrorState
            compact
            title="无法加载计划任务"
            error={tasksError}
            onRetry={() => void refetchTasks()}
          />
        ) : tasks.length === 0 ? (
          <div className="rounded-2xl border border-white/8 bg-white/[0.02] py-10 text-center text-xs text-zinc-500">
            暂无计划任务
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
                        ? `上次结束: ${formatDateTime(task.LastExecutionResult.EndTimeUtc)}`
                        : `Key: ${task.Key || task.Id}`}
                    </span>

                    <div className="flex items-center gap-2">
                      <button
                        type="button"
                        onClick={() => handleOpenTriggers(task)}
                        className="inline-flex items-center gap-1 rounded-lg border border-white/10 bg-white/[0.04] px-2.5 py-1.5 text-xs font-medium text-zinc-300 transition-colors hover:border-amber-400/40 hover:text-amber-300"
                      >
                        <Clock className="h-3.5 w-3.5" />
                        <span>触发规则</span>
                      </button>
                      {isRunning && (
                        <button
                          type="button"
                          disabled={stoppingTaskId === task.Id}
                          onClick={() => handleStopTask(task.Id, task.Name)}
                          className="inline-flex items-center gap-1.5 rounded-lg border border-red-500/30 bg-red-500/15 px-3 py-1.5 text-xs font-semibold text-red-300 transition-colors hover:bg-red-500/25 disabled:opacity-50"
                        >
                          {stoppingTaskId === task.Id ? (
                            <Loader2 className="h-3.5 w-3.5 animate-spin" />
                          ) : (
                            <Square className="h-3 w-3 fill-current" />
                          )}
                          <span>停止</span>
                        </button>
                      )}
                      <button
                        type="button"
                        disabled={isRunning || isTriggering}
                        onClick={() => handleRunTask(task.Id, task.Name)}
                        className="inline-flex items-center gap-1.5 rounded-lg bg-amber-500/15 px-3 py-1.5 text-xs font-semibold text-amber-300 ring-1 ring-amber-500/30 transition-colors hover:bg-amber-500/25 disabled:opacity-50"
                      >
                        {isTriggering ? (
                          <Loader2 className="h-3.5 w-3.5 animate-spin" />
                        ) : (
                          <Play className="h-3.5 w-3.5 fill-current" />
                        )}
                        <span>立即运行</span>
                      </button>
                    </div>
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
              <span>服务器日志</span>
            </h2>
            <p className="mt-1 text-xs text-zinc-400">
              浏览 Ambra 服务端日志文件并实时在线查看输出内容
            </p>
          </div>
          <button
            type="button"
            onClick={() => void refetchLogs()}
            className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/[0.04] px-3 py-1.5 text-xs font-medium text-zinc-300 transition-colors hover:border-amber-400/40 hover:text-amber-300"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${logsFetching ? 'animate-spin' : ''}`} />
            <span>刷新列表</span>
          </button>
        </div>

        <div className="grid gap-4 lg:grid-cols-12">
          {/* 左侧文件列表 */}
          <div className="overflow-hidden rounded-2xl border border-white/8 bg-white/[0.02] lg:col-span-4">
            <div className="border-b border-white/8 px-4 py-2.5 text-xs font-semibold text-zinc-400">
              日志文件 ({logFiles.length})
            </div>

            {logsLoading ? (
              <div className="flex h-36 items-center justify-center">
                <Loader2 className="h-5 w-5 animate-spin text-amber-400" />
              </div>
            ) : logsIsError ? (
              <div className="p-3">
                <QueryErrorState
                  compact
                  title="无法加载服务器日志列表"
                  error={logsError}
                  onRetry={() => void refetchLogs()}
                />
              </div>
            ) : logFiles.length === 0 ? (
              <div className="py-12 text-center text-xs text-zinc-500">暂无服务器日志文件</div>
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
                {selectedLog || '请在左侧选择日志文件查看'}
              </span>

              {selectedLog && (
                <div className="flex items-center gap-2">
                  <div className="relative">
                    <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-zinc-500" />
                    <input
                      type="text"
                      value={logFilter}
                      onChange={(e) => setLogFilter(e.target.value)}
                      placeholder="过滤日志关键词..."
                      className="w-40 rounded-lg border border-white/10 bg-black/40 py-1 pl-8 pr-2.5 text-xs text-zinc-200 placeholder-zinc-500 outline-none focus:border-amber-400/50 sm:w-52"
                    />
                  </div>
                  <button
                    type="button"
                    disabled={contentLoading || Boolean(contentError)}
                    onClick={handleDownloadLog}
                    className="inline-flex items-center gap-1 rounded-lg border border-white/10 bg-white/[0.04] px-2.5 py-1 text-xs font-medium text-zinc-200 transition-colors hover:border-amber-400/40 hover:text-amber-300 disabled:cursor-not-allowed disabled:opacity-40"
                    title="下载当前日志文件"
                  >
                    <Download className="h-3.5 w-3.5" />
                    <span>下载日志</span>
                  </button>
                  <button
                    type="button"
                    onClick={() => void refetchLogContent()}
                    className="rounded-lg border border-white/10 bg-white/[0.04] p-1.5 text-zinc-400 hover:text-amber-300"
                    title="重新加载日志"
                  >
                    <RefreshCw className={`h-3.5 w-3.5 ${contentFetching ? 'animate-spin' : ''}`} />
                  </button>
                </div>
              )}
            </div>

            <div className="h-[420px] overflow-auto p-4">
              {!selectedLog ? (
                <div className="flex h-full items-center justify-center text-xs text-zinc-500">
                  点击左侧日志文件名加载内容
                </div>
              ) : contentLoading ? (
                <div className="flex h-full items-center justify-center">
                  <Loader2 className="h-5 w-5 animate-spin text-amber-400" />
                </div>
              ) : contentError ? (
                <div className="flex h-full items-center justify-center text-xs text-red-300">
                  读取日志失败：
                  {contentError instanceof Error ? contentError.message : '未知错误'}
                </div>
              ) : (
                <pre className="whitespace-pre-wrap break-all font-mono text-[11px] leading-relaxed text-zinc-300">
                  {filteredLogText || '(日志内容为空或无匹配行)'}
                </pre>
              )}
            </div>
          </div>
        </div>
      </section>

      {/* 触发规则管理弹窗 */}
      <AdminModal
        open={Boolean(editingTriggersTask)}
        title={`触发规则 · ${editingTriggersTask?.Name || ''}`}
        subtitle="配置计划任务的定时、间隔或启动触发规则"
        maxWidthClass="max-w-lg"
        onClose={() => setEditingTriggersTask(null)}
      >
        <div className="space-y-5">
          {/* 已有触发规则列表 */}
          <div className="space-y-2">
            <p className="text-xs font-medium text-zinc-400">
              当前规则 ({draftTriggers.length})
            </p>
            {draftTriggers.length === 0 ? (
              <div className="rounded-xl border border-white/8 bg-white/[0.02] py-5 text-center text-xs text-zinc-500">
                暂无自动触发规则（仅手动运行）
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
                      <span>{formatTriggerLabel(trigger)}</span>
                    </div>
                    <button
                      type="button"
                      aria-label={`删除规则 ${idx + 1}`}
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
            <p className="text-xs font-medium text-zinc-300">添加触发规则</p>
            <div className="grid gap-3 sm:grid-cols-12">
              <div className="sm:col-span-5">
                <select
                  aria-label="触发类型"
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
                  <option value="DailyTrigger">每日定时 (Daily)</option>
                  <option value="WeeklyTrigger">每周定时 (Weekly)</option>
                  <option value="IntervalTrigger">固定间隔 (Interval)</option>
                  <option value="StartupTrigger">启动时触发 (Startup)</option>
                </select>
              </div>

              <div className="sm:col-span-4">
                {newTriggerType === 'DailyTrigger' ? (
                  <input
                    aria-label="每日执行时间"
                    type="time"
                    value={newDailyTime}
                    onChange={(e) => setNewDailyTime(e.target.value)}
                    className="w-full rounded-lg border border-white/10 bg-zinc-900/90 px-2.5 py-2 text-xs text-zinc-100 outline-none focus:border-amber-400/60"
                  />
                ) : newTriggerType === 'WeeklyTrigger' ? (
                  <div className="flex flex-col gap-2">
                    <select
                      aria-label="每周执行星期"
                      value={newWeeklyDay}
                      onChange={(e) => setNewWeeklyDay(e.target.value)}
                      className="w-full rounded-lg border border-white/10 bg-[#181822] px-2.5 py-2 text-xs text-zinc-100 outline-none focus:border-amber-400/60"
                    >
                      {DAYS_OF_WEEK.map((d) => (
                        <option key={d.value} value={d.value}>
                          {d.label}
                        </option>
                      ))}
                    </select>
                    <input
                      aria-label="每周执行时间"
                      type="time"
                      value={newDailyTime}
                      onChange={(e) => setNewDailyTime(e.target.value)}
                      className="w-full rounded-lg border border-white/10 bg-zinc-900/90 px-2.5 py-2 text-xs text-zinc-100 outline-none focus:border-amber-400/60"
                    />
                  </div>
                ) : newTriggerType === 'IntervalTrigger' ? (
                  <div className="flex items-center gap-1.5">
                    <input
                      aria-label="间隔小时数"
                      type="number"
                      min={1}
                      max={168}
                      value={newIntervalHours}
                      onChange={(e) => setNewIntervalHours(Number(e.target.value))}
                      className="w-full rounded-lg border border-white/10 bg-zinc-900/90 px-2.5 py-2 text-xs text-zinc-100 outline-none focus:border-amber-400/60"
                    />
                    <span className="shrink-0 text-xs text-zinc-400">小时</span>
                  </div>
                ) : (
                  <div className="flex h-full items-center text-xs text-zinc-500">
                    服务启动自动运行
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
                  <span>添加</span>
                </button>
              </div>
            </div>
          </div>

          {/* 底部保存/取消 */}
          <div className="flex justify-end gap-2 pt-2">
            <button
              type="button"
              onClick={() => setEditingTriggersTask(null)}
              className="rounded-xl border border-white/10 bg-white/[0.04] px-4 py-2 text-xs font-medium text-zinc-300 hover:bg-white/10"
            >
              取消
            </button>
            <button
              type="button"
              disabled={savingTriggers}
              onClick={handleSaveTriggers}
              className="inline-flex items-center gap-1.5 rounded-xl bg-amber-500 px-4 py-2 text-xs font-semibold text-zinc-950 transition-colors hover:bg-amber-400 disabled:opacity-60"
            >
              {savingTriggers ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <Save className="h-3.5 w-3.5" />
              )}
              <span>保存规则</span>
            </button>
          </div>
        </div>
      </AdminModal>
    </div>
  )
}
