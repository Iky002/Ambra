import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  CalendarClock,
  FileText,
  Loader2,
  Play,
  RefreshCw,
  Search,
  Terminal,
} from 'lucide-react'
import { api } from '../../api/client'
import type { ScheduledTaskInfo, ServerLogFile } from '../../api/types'
import { FeedbackBanner, formatBytes, formatDateTime } from './adminUtils'

export function TasksLogsTab() {
  const [feedback, setFeedback] = useState<{ type: 'success' | 'error'; message: string } | null>(
    null,
  )
  const [runningTaskId, setRunningTaskId] = useState<string | null>(null)
  const [selectedLog, setSelectedLog] = useState<string | null>(null)
  const [logFilter, setLogFilter] = useState('')

  const {
    data: rawTasks,
    isLoading: tasksLoading,
    isFetching: tasksFetching,
    refetch: refetchTasks,
  } = useQuery({
    queryKey: ['admin', 'scheduledTasks'],
    queryFn: () => api.getScheduledTasks(),
  })

  const tasks: ScheduledTaskInfo[] = Array.isArray(rawTasks) ? rawTasks : []

  const {
    data: rawLogFiles,
    isLoading: logsLoading,
    isFetching: logsFetching,
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

  const handleRunTask = async (taskId: string, taskName: string) => {
    setRunningTaskId(taskId)
    setFeedback(null)
    try {
      await api.runScheduledTask(taskId)
      setFeedback({ type: 'success', message: `已触发计划任务「${taskName}」` })
      window.setTimeout(() => {
        void refetchTasks()
      }, 600)
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '运行计划任务失败',
      })
    } finally {
      setRunningTaskId(null)
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
    </div>
  )
}
