import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  Activity,
  ChevronLeft,
  ChevronRight,
  Cpu,
  HardDrive,
  Loader2,
  MessageSquare,
  MonitorPlay,
  Pause,
  Play,
  Radio,
  RefreshCw,
  Search,
  Send,
  Server,
  ShieldCheck,
  Square,
  Terminal,
} from 'lucide-react'
import { api } from '../../api/client'
import type { SessionInfo } from '../../api/types'
import { QueryErrorState } from '../../components/QueryErrorState'
import { ScanProgressBar } from '../../components/ScanProgressBar'
import { useI18n } from '../../i18n/I18nContext'
import {
  AdminModal,
  FeedbackBanner,
  formatDateTime,
  formatRelativeTime,
  formatTicks,
} from './adminUtils'

const ACTIVITY_PAGE_SIZE = 20
type SeverityFilter = 'All' | 'Info' | 'Warn' | 'Error'

const SEVERITY_OPTIONS: Array<{ value: SeverityFilter; label: string }> = [
  { value: 'All', label: '全部' },
  { value: 'Info', label: 'Info' },
  { value: 'Warn', label: 'Warn' },
  { value: 'Error', label: 'Error' },
]

function matchesSeverity(severity: string | undefined, filter: SeverityFilter): boolean {
  if (filter === 'All') return true
  const s = (severity || 'Info').toLowerCase()
  if (filter === 'Error') return s === 'error' || s === 'fatal' || s === 'critical'
  if (filter === 'Warn') return s === 'warn' || s === 'warning'
  return s !== 'error' && s !== 'fatal' && s !== 'critical' && s !== 'warn' && s !== 'warning'
}

export function DashboardTab() {
  const { t } = useI18n()
  const [autoRefresh, setAutoRefresh] = useState(true)
  const [feedback, setFeedback] = useState<{ type: 'success' | 'error'; message: string } | null>(
    null,
  )
  const [busySessionId, setBusySessionId] = useState<string | null>(null)
  const [messageTarget, setMessageTarget] = useState<SessionInfo | null>(null)
  const [msgHeader, setMsgHeader] = useState('系统通知')
  const [msgText, setMsgText] = useState('')
  const [msgError, setMsgError] = useState<string | null>(null)
  const [sendingMsg, setSendingMsg] = useState(false)

  // 系统活动日志分页与筛选
  const [logPage, setLogPage] = useState(0)
  const [severityFilter, setSeverityFilter] = useState<SeverityFilter>('All')
  const [logSearch, setLogSearch] = useState('')

  const {
    data: sysInfo,
    isLoading: sysLoading,
    isError: sysIsError,
    error: sysError,
    refetch: refetchSysInfo,
  } = useQuery({
    queryKey: ['admin', 'systemInfo'],
    queryFn: () => api.getSystemInfo(),
  })

  const {
    data: sessions = [],
    isLoading: sessionsLoading,
    isFetching: sessionsFetching,
    isError: sessionsIsError,
    error: sessionsError,
    refetch: refetchSessions,
  } = useQuery({
    queryKey: ['admin', 'sessions'],
    queryFn: () => api.getSessions(),
    refetchInterval: autoRefresh ? 5000 : false,
  })

  const {
    data: activityData,
    isLoading: logsLoading,
    isFetching: logsFetching,
    isError: logsIsError,
    error: logsError,
    refetch: refetchLogs,
  } = useQuery({
    queryKey: ['admin', 'activityLogs', logPage],
    queryFn: () => api.getActivityLogs(logPage * ACTIVITY_PAGE_SIZE, ACTIVITY_PAGE_SIZE),
    refetchInterval: autoRefresh ? 15000 : false,
  })

  const activityItems = activityData?.Items || []
  const totalActivityCount = activityData?.TotalRecordCount ?? activityItems.length
  const totalActivityPages = Math.max(1, Math.ceil(totalActivityCount / ACTIVITY_PAGE_SIZE))

  const filteredActivityItems = useMemo(() => {
    const keyword = logSearch.trim().toLowerCase()
    return activityItems.filter((entry) => {
      if (!matchesSeverity(entry.Severity, severityFilter)) {
        return false
      }
      if (!keyword) return true
      const haystack = [
        entry.Name,
        entry.ShortOverview,
        entry.Overview,
        entry.Type,
        entry.Severity,
      ]
        .filter(Boolean)
        .join(' ')
        .toLowerCase()
      return haystack.includes(keyword)
    })
  }, [activityItems, severityFilter, logSearch])

  const playingCount = sessions.filter((s) => Boolean(s.NowPlayingItem)).length

  const handleRemoteCommand = async (session: SessionInfo, command: 'PlayPause' | 'Stop') => {
    setBusySessionId(session.Id)
    setFeedback(null)
    const label = session.DeviceName || session.UserName || session.Id
    try {
      await api.sendSessionPlaystateCommand(session.Id, command)
      setFeedback({
        type: 'success',
        message: `已向「${label}」发送${command === 'Stop' ? '停止播放' : '暂停/继续'}指令`,
      })
      void refetchSessions()
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '发送远程控制指令失败',
      })
    } finally {
      setBusySessionId(null)
    }
  }

  const handleOpenMessage = (session: SessionInfo) => {
    setMessageTarget(session)
    setMsgHeader('系统通知')
    setMsgText('')
    setMsgError(null)
  }

  const handleSendMessage = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!messageTarget) return
    const trimmedText = msgText.trim()
    if (!trimmedText) {
      setMsgError('消息内容不能为空')
      return
    }
    setSendingMsg(true)
    setMsgError(null)
    setFeedback(null)
    const label = messageTarget.DeviceName || messageTarget.UserName || messageTarget.Id
    try {
      await api.sendSessionMessage(messageTarget.Id, {
        Header: msgHeader.trim() || '系统通知',
        Text: trimmedText,
      })
      setFeedback({ type: 'success', message: `已向「${label}」发送弹窗消息` })
      setMessageTarget(null)
      setMsgText('')
      setMsgError(null)
    } catch (err) {
      setMsgError(err instanceof Error ? err.message : '发送消息失败')
    } finally {
      setSendingMsg(false)
    }
  }

  return (
    <div className="space-y-8">
      <FeedbackBanner
        message={feedback?.message}
        type={feedback?.type}
        onClose={() => setFeedback(null)}
      />

      {/* 实时媒体库扫描与刮削进度（仅扫描中或刚完成时展示） */}
      <ScanProgressBar variant="card" />

      {/* 服务器状态卡片 */}
      <section className="space-y-4">
        <div className="flex items-center justify-between gap-3">
          <div className="flex items-center gap-2 text-sm font-semibold text-zinc-200">
            <Server className="h-4 w-4 text-amber-400" />
            <span>{t('admin.dashboardOverview')}</span>
          </div>
          <span className="inline-flex items-center gap-1.5 rounded-full border border-emerald-500/30 bg-emerald-500/10 px-2.5 py-1 text-xs font-medium text-emerald-300">
            <span className="h-2 w-2 animate-pulse rounded-full bg-emerald-400" />
            {sysInfo?.IsShuttingDown ? '正在关闭' : '运行正常'}
          </span>
        </div>

        {sysLoading ? (
          <div className="flex h-32 items-center justify-center rounded-2xl border border-white/8 bg-white/[0.02]">
            <Loader2 className="h-6 w-6 animate-spin text-amber-400" />
          </div>
        ) : sysIsError ? (
          <QueryErrorState
            compact
            title="无法加载服务器状态"
            error={sysError}
            onRetry={() => void refetchSysInfo()}
          />
        ) : (
          <>
            <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
              <StatusTile
                label="服务器名称"
                value={sysInfo?.ServerName || 'Ambra'}
                sub={`产品: ${sysInfo?.ProductName || 'Ambra'}`}
              />
              <StatusTile
                label="当前版本"
                value={sysInfo?.AmbraVersion || (sysInfo?.Version ? `v${sysInfo.Version}` : 'v0.10.4')}
                sub={`Emby ${sysInfo?.Version || '4.10.2.0'} 兼容 · 端口 ${sysInfo?.HttpServerPortNumber || 8096}`}
              />
              <StatusTile
                label="Server ID"
                value={sysInfo?.Id || '-'}
                mono
                sub={sysInfo?.LocalAddress || '本地实例'}
              />
              <StatusTile
                label="系统与架构"
                value={
                  [
                    sysInfo?.OperatingSystemDisplayName || sysInfo?.OperatingSystem,
                    sysInfo?.SystemArchitecture,
                  ]
                    .filter(Boolean)
                    .join(' · ') || '-'
                }
                sub={`活动会话: ${sessions.length}（播放中 ${playingCount}）`}
              />
            </div>

            {sysInfo && (
              <div className="grid gap-3 rounded-2xl border border-white/8 bg-white/[0.02] p-4 text-xs text-zinc-400 md:grid-cols-3">
                <div className="flex items-center gap-2 truncate">
                  <HardDrive className="h-3.5 w-3.5 shrink-0 text-amber-400/80" />
                  <span className="text-zinc-500">数据目录:</span>
                  <span className="truncate font-mono text-zinc-300" title={sysInfo.ProgramDataPath}>
                    {sysInfo.ProgramDataPath || '-'}
                  </span>
                </div>
                <div className="flex items-center gap-2 truncate">
                  <Cpu className="h-3.5 w-3.5 shrink-0 text-amber-400/80" />
                  <span className="text-zinc-500">转码缓存:</span>
                  <span
                    className="truncate font-mono text-zinc-300"
                    title={sysInfo.TranscodingTempPath}
                  >
                    {sysInfo.TranscodingTempPath || '-'}
                  </span>
                </div>
                <div className="flex items-center gap-2 truncate">
                  <Terminal className="h-3.5 w-3.5 shrink-0 text-amber-400/80" />
                  <span className="text-zinc-500">日志目录:</span>
                  <span className="truncate font-mono text-zinc-300" title={sysInfo.LogPath}>
                    {sysInfo.LogPath || '-'}
                  </span>
                </div>
              </div>
            )}
          </>
        )}
      </section>

      {/* 实时活动会话 */}
      <section className="space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-2 text-sm font-semibold text-zinc-200">
            <Radio className="h-4 w-4 text-amber-400" />
            <span>实时活动会话</span>
            <span className="rounded-md bg-white/8 px-2 py-0.5 text-xs font-medium text-zinc-300">
              {sessions.length}
            </span>
          </div>

          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={() => setAutoRefresh((v) => !v)}
              className={`rounded-lg border px-2.5 py-1.5 text-xs font-medium transition-colors ${
                autoRefresh
                  ? 'border-amber-500/35 bg-amber-500/12 text-amber-300'
                  : 'border-white/10 bg-white/[0.03] text-zinc-400 hover:text-zinc-200'
              }`}
            >
              {autoRefresh ? '自动刷新: 5s' : '自动刷新已暂停'}
            </button>
            <button
              type="button"
              onClick={() => void refetchSessions()}
              className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/[0.04] px-2.5 py-1.5 text-xs font-medium text-zinc-300 transition-colors hover:border-amber-400/40 hover:text-amber-300"
            >
              <RefreshCw className={`h-3.5 w-3.5 ${sessionsFetching ? 'animate-spin' : ''}`} />
              <span>刷新</span>
            </button>
          </div>
        </div>

        {sessionsLoading ? (
          <div className="flex h-28 items-center justify-center rounded-2xl border border-white/8 bg-white/[0.02]">
            <Loader2 className="h-5 w-5 animate-spin text-amber-400" />
          </div>
        ) : sessionsIsError ? (
          <QueryErrorState
            compact
            title="无法加载实时活动会话"
            error={sessionsError}
            onRetry={() => void refetchSessions()}
          />
        ) : sessions.length === 0 ? (
          <div className="rounded-2xl border border-white/8 bg-white/[0.02] py-10 text-center text-xs text-zinc-500">
            当前暂无在线活动会话
          </div>
        ) : (
          <div className="grid gap-4 lg:grid-cols-2">
            {sessions.map((session) => (
              <SessionCard
                key={session.Id || session.DeviceId}
                session={session}
                busy={busySessionId === session.Id}
                onRemoteCommand={handleRemoteCommand}
                onOpenMessage={handleOpenMessage}
              />
            ))}
          </div>
        )}
      </section>

      {/* 系统活动日志 */}
      <section className="space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-2 text-sm font-semibold text-zinc-200">
            <Activity className="h-4 w-4 text-amber-400" />
            <span>系统活动日志</span>
            <span className="rounded-md bg-white/8 px-2 py-0.5 text-xs font-medium text-zinc-300">
              {totalActivityCount}
            </span>
          </div>

          <div className="flex flex-wrap items-center gap-2">
            <div className="inline-flex rounded-lg border border-white/10 bg-white/[0.03] p-0.5">
              {SEVERITY_OPTIONS.map((opt) => {
                const active = severityFilter === opt.value
                return (
                  <button
                    key={opt.value}
                    type="button"
                    aria-pressed={active}
                    onClick={() => setSeverityFilter(opt.value)}
                    className={`rounded-md px-2.5 py-1 text-xs font-medium transition-colors ${
                      active
                        ? 'bg-amber-500/20 text-amber-300'
                        : 'text-zinc-400 hover:text-zinc-200'
                    }`}
                  >
                    {opt.label}
                  </button>
                )
              })}
            </div>

            <div className="relative">
              <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-zinc-500" />
              <input
                type="text"
                aria-label="搜索活动日志"
                value={logSearch}
                onChange={(e) => setLogSearch(e.target.value)}
                placeholder="搜索日志关键词..."
                className="w-40 rounded-lg border border-white/10 bg-white/[0.04] py-1.5 pl-8 pr-2.5 text-xs text-zinc-200 placeholder-zinc-500 outline-none focus:border-amber-400/50 sm:w-48"
              />
            </div>

            <button
              type="button"
              onClick={() => void refetchLogs()}
              className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/[0.04] px-2.5 py-1.5 text-xs font-medium text-zinc-300 transition-colors hover:border-amber-400/40 hover:text-amber-300"
            >
              <RefreshCw className={`h-3.5 w-3.5 ${logsFetching ? 'animate-spin' : ''}`} />
              <span>刷新</span>
            </button>
          </div>
        </div>

        {logsLoading ? (
          <div className="flex h-32 items-center justify-center rounded-2xl border border-white/8 bg-white/[0.02]">
            <Loader2 className="h-5 w-5 animate-spin text-amber-400" />
          </div>
        ) : logsIsError ? (
          <QueryErrorState
            compact
            title="无法加载系统活动日志"
            error={logsError}
            onRetry={() => void refetchLogs()}
          />
        ) : activityItems.length === 0 ? (
          <div className="rounded-2xl border border-white/8 bg-white/[0.02] py-10 text-center text-xs text-zinc-500">
            暂无活动日志记录
          </div>
        ) : filteredActivityItems.length === 0 ? (
          <div className="rounded-2xl border border-white/8 bg-white/[0.02] py-10 text-center text-xs text-zinc-500">
            当前筛选条件下无匹配的活动日志
          </div>
        ) : (
          <div className="overflow-hidden rounded-2xl border border-white/8 bg-white/[0.02]">
            <div className="divide-y divide-white/6">
              {filteredActivityItems.map((entry) => (
                <div
                  key={entry.Id}
                  className="flex flex-col gap-1.5 px-4 py-3 text-xs sm:flex-row sm:items-center sm:justify-between"
                >
                  <div className="flex min-w-0 items-start gap-2.5">
                    <SeverityBadge severity={entry.Severity} />
                    <div className="min-w-0">
                      <p className="font-medium text-zinc-200">{entry.Name}</p>
                      {(entry.ShortOverview || entry.Overview) && (
                        <p className="mt-0.5 truncate text-zinc-400">
                          {entry.ShortOverview || entry.Overview}
                        </p>
                      )}
                    </div>
                  </div>
                  <div className="flex shrink-0 items-center gap-2 text-[11px] text-zinc-500">
                    <span className="rounded bg-white/5 px-1.5 py-0.5 font-mono text-zinc-400">
                      {entry.Type}
                    </span>
                    <span title={formatDateTime(entry.Date)}>{formatRelativeTime(entry.Date)}</span>
                  </div>
                </div>
              ))}
            </div>

            <div className="flex flex-wrap items-center justify-between gap-2 border-t border-white/8 bg-white/[0.02] px-4 py-2.5 text-xs text-zinc-400">
              <span>
                共 {totalActivityCount} 条记录 · 第 {logPage + 1} / {totalActivityPages} 页
              </span>
              <div className="flex items-center gap-2">
                <button
                  type="button"
                  disabled={logPage <= 0}
                  onClick={() => setLogPage((p) => Math.max(0, p - 1))}
                  className="inline-flex items-center gap-1 rounded-lg border border-white/10 bg-white/[0.04] px-2.5 py-1 text-xs font-medium text-zinc-200 transition-colors hover:border-amber-400/40 hover:text-amber-300 disabled:cursor-not-allowed disabled:opacity-40"
                >
                  <ChevronLeft className="h-3.5 w-3.5" />
                  <span>上一页</span>
                </button>
                <button
                  type="button"
                  disabled={(logPage + 1) * ACTIVITY_PAGE_SIZE >= totalActivityCount}
                  onClick={() => setLogPage((p) => p + 1)}
                  className="inline-flex items-center gap-1 rounded-lg border border-white/10 bg-white/[0.04] px-2.5 py-1 text-xs font-medium text-zinc-200 transition-colors hover:border-amber-400/40 hover:text-amber-300 disabled:cursor-not-allowed disabled:opacity-40"
                >
                  <span>下一页</span>
                  <ChevronRight className="h-3.5 w-3.5" />
                </button>
              </div>
            </div>
          </div>
        )}
      </section>

      <AdminModal
        open={Boolean(messageTarget)}
        title={`向会话发送消息`}
        subtitle={
          messageTarget
            ? `${messageTarget.UserName || '未知用户'} · ${messageTarget.DeviceName || messageTarget.DeviceId || messageTarget.Id}`
            : undefined
        }
        maxWidthClass="max-w-md"
        onClose={() => {
          setMessageTarget(null)
          setMsgError(null)
        }}
      >
        <form onSubmit={handleSendMessage} noValidate className="space-y-4">
          {msgError && (
            <FeedbackBanner
              message={msgError}
              type="error"
              onClose={() => setMsgError(null)}
            />
          )}
          <div>
            <label htmlFor="session-msg-header" className="mb-1.5 block text-xs text-zinc-400">
              消息标题 (Header)
            </label>
            <input
              id="session-msg-header"
              type="text"
              value={msgHeader}
              onChange={(e) => setMsgHeader(e.target.value)}
              placeholder="系统通知"
              className="w-full rounded-xl border border-white/10 bg-zinc-900/90 px-3.5 py-2 text-sm text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
            />
          </div>
          <div>
            <label htmlFor="session-msg-text" className="mb-1.5 block text-xs text-zinc-400">
              消息正文 (Text) *
            </label>
            <textarea
              id="session-msg-text"
              rows={3}
              value={msgText}
              onChange={(e) => {
                setMsgText(e.target.value)
                if (msgError) setMsgError(null)
              }}
              placeholder="输入要推送到客户端屏幕的提示内容..."
              className="w-full rounded-xl border border-white/10 bg-zinc-900/90 px-3.5 py-2 text-sm text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
            />
          </div>
          <div className="flex justify-end gap-2 pt-2">
            <button
              type="button"
              onClick={() => {
                setMessageTarget(null)
                setMsgError(null)
              }}
              className="rounded-xl border border-white/10 bg-white/[0.04] px-4 py-2 text-xs font-medium text-zinc-300 hover:bg-white/10"
            >
              取消
            </button>
            <button
              type="submit"
              disabled={sendingMsg}
              className="inline-flex items-center gap-1.5 rounded-xl bg-amber-500 px-4 py-2 text-xs font-semibold text-zinc-950 transition-colors hover:bg-amber-400 disabled:opacity-60"
            >
              {sendingMsg ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <Send className="h-3.5 w-3.5" />
              )}
              <span>发送消息</span>
            </button>
          </div>
        </form>
      </AdminModal>
    </div>
  )
}

function StatusTile({
  label,
  value,
  sub,
  mono,
}: {
  label: string
  value: string
  sub?: string
  mono?: boolean
}) {
  return (
    <div className="rounded-2xl border border-white/8 bg-white/[0.03] p-4">
      <p className="text-xs text-zinc-500">{label}</p>
      <p
        className={`mt-1.5 truncate text-base font-semibold text-zinc-100 ${mono ? 'font-mono text-sm' : ''}`}
        title={value}
      >
        {value}
      </p>
      {sub && (
        <p className="mt-1 truncate text-[11px] text-zinc-400" title={sub}>
          {sub}
        </p>
      )}
    </div>
  )
}

function SessionCard({
  session,
  busy = false,
  onRemoteCommand,
  onOpenMessage,
}: {
  session: SessionInfo
  busy?: boolean
  onRemoteCommand: (session: SessionInfo, command: 'PlayPause' | 'Stop') => void
  onOpenMessage: (session: SessionInfo) => void
}) {
  const item = session.NowPlayingItem
  const posTicks = session.PlayState?.PositionTicks ?? 0
  const runTicks = item?.RunTimeTicks ?? 0
  const pct = runTicks > 0 ? Math.min(100, Math.max(0, (posTicks / runTicks) * 100)) : 0
  const isPaused = Boolean(session.PlayState?.IsPaused)

  return (
    <div className="rounded-2xl border border-white/8 bg-white/[0.03] p-4 transition-colors hover:border-white/15">
      <div className="flex items-start justify-between gap-3">
        <div className="flex min-w-0 items-center gap-3">
          <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-amber-500/15 text-amber-400 ring-1 ring-amber-500/25">
            <MonitorPlay className="h-5 w-5" />
          </div>
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <p className="truncate text-sm font-semibold text-zinc-100">
                {session.UserName || '未知用户'}
              </p>
              <span className="rounded bg-white/8 px-1.5 py-0.5 text-[10px] font-medium text-zinc-300">
                {session.Client || 'Client'} {session.ApplicationVersion || ''}
              </span>
            </div>
            <p className="mt-0.5 truncate text-xs text-zinc-400">
              {session.DeviceName || session.DeviceId || '未知设备'}
            </p>
          </div>
        </div>
        <span className="shrink-0 text-[11px] text-zinc-500">
          {formatRelativeTime(session.LastActivityDate)}
        </span>
      </div>

      {item ? (
        <div className="mt-4 rounded-xl border border-amber-500/20 bg-amber-500/[0.06] p-3">
          <div className="flex items-center justify-between gap-2">
            <div className="flex min-w-0 items-center gap-2">
              {isPaused ? (
                <Pause className="h-3.5 w-3.5 shrink-0 text-amber-300" />
              ) : (
                <Play className="h-3.5 w-3.5 shrink-0 fill-amber-400 text-amber-400" />
              )}
              <p className="truncate text-xs font-semibold text-zinc-100">
                {item.SeriesName ? `${item.SeriesName} · ` : ''}
                {item.Name}
              </p>
            </div>
            {session.PlayState?.PlayMethod && (
              <span className="shrink-0 rounded bg-amber-500/20 px-1.5 py-0.5 text-[10px] font-medium text-amber-300">
                {session.PlayState.PlayMethod}
              </span>
            )}
          </div>

          <div className="mt-2.5 h-1.5 w-full overflow-hidden rounded-full bg-white/10">
            <div
              className="h-full rounded-full bg-gradient-to-r from-amber-500 to-orange-400 transition-all duration-500"
              style={{ width: `${pct}%` }}
            />
          </div>

          <div className="mt-1.5 flex items-center justify-between text-[11px] font-mono text-zinc-400">
            <span>{formatTicks(posTicks)}</span>
            <span>{runTicks > 0 ? `${Math.round(pct)}% · ${formatTicks(runTicks)}` : '直播 / 未知时长'}</span>
          </div>
        </div>
      ) : (
        <div className="mt-3 flex items-center gap-1.5 text-xs text-zinc-500">
          <ShieldCheck className="h-3.5 w-3.5 text-zinc-500" />
          <span>当前空闲在线</span>
        </div>
      )}

      {/* 远程控制操作栏 */}
      <div className="mt-3 flex flex-wrap items-center gap-2 border-t border-white/6 pt-3">
        <button
          type="button"
          disabled={busy || !item}
          onClick={() => onRemoteCommand(session, 'PlayPause')}
          className="inline-flex items-center gap-1 rounded-lg border border-white/10 bg-white/[0.04] px-2.5 py-1 text-[11px] font-medium text-zinc-200 transition-colors hover:border-amber-400/40 hover:text-amber-300 disabled:cursor-not-allowed disabled:opacity-40"
        >
          {isPaused ? <Play className="h-3 w-3" /> : <Pause className="h-3 w-3" />}
          <span>暂停/继续</span>
        </button>
        <button
          type="button"
          disabled={busy || !item}
          onClick={() => onRemoteCommand(session, 'Stop')}
          className="inline-flex items-center gap-1 rounded-lg border border-red-500/25 bg-red-500/10 px-2.5 py-1 text-[11px] font-medium text-red-300 transition-colors hover:bg-red-500/20 disabled:cursor-not-allowed disabled:opacity-40"
        >
          <Square className="h-3 w-3 fill-current" />
          <span>停止</span>
        </button>
        <button
          type="button"
          disabled={busy}
          onClick={() => onOpenMessage(session)}
          className="inline-flex items-center gap-1 rounded-lg border border-white/10 bg-white/[0.04] px-2.5 py-1 text-[11px] font-medium text-zinc-200 transition-colors hover:border-amber-400/40 hover:text-amber-300 disabled:opacity-40"
        >
          <MessageSquare className="h-3 w-3" />
          <span>发消息</span>
        </button>
      </div>
    </div>
  )
}

function SeverityBadge({ severity }: { severity?: string }) {
  const s = (severity || 'Info').toLowerCase()
  const cls =
    s === 'error' || s === 'fatal'
      ? 'bg-red-500/15 text-red-300 ring-red-500/30'
      : s === 'warn' || s === 'warning'
        ? 'bg-amber-500/15 text-amber-300 ring-amber-500/30'
        : 'bg-sky-500/15 text-sky-300 ring-sky-500/30'
  return (
    <span
      className={`mt-0.5 inline-flex shrink-0 rounded px-1.5 py-0.5 text-[10px] font-semibold uppercase ring-1 ${cls}`}
    >
      {severity || 'Info'}
    </span>
  )
}
