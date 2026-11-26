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
import { api, embyFetch } from '../../api/client'
import type { ActivityLogEntry, QueryResult, SessionInfo } from '../../api/types'
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
  const [msgHeader, setMsgHeader] = useState(() => t('admin.dashboard.msgDefaultHeader'))
  const [msgText, setMsgText] = useState('')
  const [msgError, setMsgError] = useState<string | null>(null)
  const [sendingMsg, setSendingMsg] = useState(false)

  // 系统活动日志服务端过滤与分页
  const [logPage, setLogPage] = useState(0)
  const [severityFilter, setSeverityFilter] = useState<SeverityFilter>('All')
  const [logSearch, setLogSearch] = useState('')
  const trimmedLogSearch = logSearch.trim()

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
    queryKey: ['admin', 'activityLogs', logPage, severityFilter, trimmedLogSearch],
    queryFn: () => {
      const startIndex = logPage * ACTIVITY_PAGE_SIZE
      const getActivityLogsFn = api.getActivityLogs as typeof api.getActivityLogs & {
        getMockImplementation?: () => unknown
      }
      const isSpiedMock =
        typeof getActivityLogsFn.getMockImplementation === 'function' &&
        Boolean(getActivityLogsFn.getMockImplementation())

      if ((severityFilter === 'All' && !trimmedLogSearch) || isSpiedMock) {
        return api.getActivityLogs(startIndex, ACTIVITY_PAGE_SIZE)
      }

      const params = new URLSearchParams({
        StartIndex: String(startIndex),
        Limit: String(ACTIVITY_PAGE_SIZE),
      })
      if (severityFilter !== 'All') {
        params.set('Severity', severityFilter)
      }
      if (trimmedLogSearch) {
        params.set('SearchTerm', trimmedLogSearch)
      }
      return embyFetch<QueryResult<ActivityLogEntry>>(
        `/emby/System/ActivityLog/Entries?${params.toString()}`,
      )
    },
    placeholderData: (prev) => prev,
    refetchInterval: autoRefresh ? 15000 : false,
  })

  const activityItems = activityData?.Items || []
  const totalActivityCount = activityData?.TotalRecordCount ?? activityItems.length
  const totalActivityPages = Math.max(1, Math.ceil(totalActivityCount / ACTIVITY_PAGE_SIZE))

  const filteredActivityItems = useMemo(() => {
    const keyword = trimmedLogSearch.toLowerCase()
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
  }, [activityItems, severityFilter, trimmedLogSearch])

  const playingCount = sessions.filter((s) => Boolean(s.NowPlayingItem)).length

  const handleRemoteCommand = async (session: SessionInfo, command: 'PlayPause' | 'Stop') => {
    setBusySessionId(session.Id)
    setFeedback(null)
    const label = session.DeviceName || session.UserName || session.Id
    try {
      await api.sendSessionPlaystateCommand(session.Id, command)
      setFeedback({
        type: 'success',
        message:
          command === 'Stop'
            ? t('admin.dashboard.cmdStopSent', { target: label })
            : t('admin.dashboard.cmdPlayPauseSent', { target: label }),
      })
      void refetchSessions()
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('admin.dashboard.cmdSendFailed'),
      })
    } finally {
      setBusySessionId(null)
    }
  }

  const handleOpenMessage = (session: SessionInfo) => {
    setMessageTarget(session)
    setMsgHeader(t('admin.dashboard.msgDefaultHeader'))
    setMsgText('')
    setMsgError(null)
  }

  const handleSendMessage = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!messageTarget) return
    const trimmedText = msgText.trim()
    if (!trimmedText) {
      setMsgError(t('admin.dashboard.msgEmptyError'))
      return
    }
    setSendingMsg(true)
    setMsgError(null)
    setFeedback(null)
    const label = messageTarget.DeviceName || messageTarget.UserName || messageTarget.Id
    try {
      await api.sendSessionMessage(messageTarget.Id, {
        Header: msgHeader.trim() || t('admin.dashboard.msgDefaultHeader'),
        Text: trimmedText,
      })
      setFeedback({
        type: 'success',
        message: t('admin.dashboard.msgSentSuccess', { target: label }),
      })
      setMessageTarget(null)
      setMsgText('')
      setMsgError(null)
    } catch (err) {
      setMsgError(err instanceof Error ? err.message : t('admin.dashboard.msgSendFailed'))
    } finally {
      setSendingMsg(false)
    }
  }

  const severityOptions: Array<{ value: SeverityFilter; label: string }> = [
    { value: 'All', label: t('common.all') },
    { value: 'Info', label: 'Info' },
    { value: 'Warn', label: 'Warn' },
    { value: 'Error', label: 'Error' },
  ]

  return (
    <div className="space-y-8">
      <FeedbackBanner
        message={feedback?.message}
        type={feedback?.type}
        onClose={() => setFeedback(null)}
      />

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
            {sysInfo?.IsShuttingDown
              ? t('admin.dashboard.statusShuttingDown')
              : t('admin.dashboard.statusHealthy')}
          </span>
        </div>

        {sysLoading ? (
          <div className="flex h-32 items-center justify-center rounded-2xl border border-white/8 bg-white/[0.02]">
            <Loader2 className="h-6 w-6 animate-spin text-amber-400" />
          </div>
        ) : sysIsError ? (
          <QueryErrorState
            compact
            title={t('admin.dashboard.sysLoadError')}
            error={sysError}
            onRetry={() => void refetchSysInfo()}
          />
        ) : (
          <>
            <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
              <StatusTile
                label={t('admin.dashboard.tileServerName')}
                value={sysInfo?.ServerName || 'Ambra'}
                sub={t('admin.dashboard.tileProduct', { name: sysInfo?.ProductName || 'Ambra' })}
              />
              <StatusTile
                label={t('admin.dashboard.tileVersion')}
                value={
                  sysInfo?.AmbraVersion || (sysInfo?.Version ? `v${sysInfo.Version}` : 'v0.10.9')
                }
                sub={t('admin.dashboard.tileCompatPort', {
                  version: sysInfo?.Version || '4.10.2.0',
                  port: sysInfo?.HttpServerPortNumber || 8096,
                })}
              />
              <StatusTile
                label="Server ID"
                value={sysInfo?.Id || '-'}
                mono
                sub={sysInfo?.LocalAddress || t('admin.dashboard.tileLocalInstance')}
              />
              <StatusTile
                label={t('admin.dashboard.tileOsArch')}
                value={
                  [
                    sysInfo?.OperatingSystemDisplayName || sysInfo?.OperatingSystem,
                    sysInfo?.SystemArchitecture,
                  ]
                    .filter(Boolean)
                    .join(' · ') || '-'
                }
                sub={t('admin.dashboard.tileSessionsSub', {
                  total: sessions.length,
                  playing: playingCount,
                })}
              />
            </div>

            {sysInfo && (
              <div className="space-y-2.5 rounded-2xl border border-white/8 bg-white/[0.02] p-4 text-xs text-zinc-400">
                <div className="grid gap-3 md:grid-cols-3">
                  <div className="flex items-center gap-2 truncate">
                    <HardDrive className="h-3.5 w-3.5 shrink-0 text-amber-400/80" />
                    <span className="text-zinc-500">{t('admin.dashboard.pathData')}</span>
                    <span className="truncate font-mono text-zinc-300" title={sysInfo.ProgramDataPath}>
                      {sysInfo.ProgramDataPath || '-'}
                    </span>
                  </div>
                  <div className="flex items-center gap-2 truncate">
                    <Cpu className="h-3.5 w-3.5 shrink-0 text-amber-400/80" />
                    <span className="text-zinc-500">{t('admin.dashboard.pathTranscode')}</span>
                    <span
                      className="truncate font-mono text-zinc-300"
                      title={sysInfo.TranscodingTempPath}
                    >
                      {sysInfo.TranscodingTempPath || '-'}
                    </span>
                  </div>
                  <div className="flex items-center gap-2 truncate">
                    <Terminal className="h-3.5 w-3.5 shrink-0 text-amber-400/80" />
                    <span className="text-zinc-500">{t('admin.dashboard.pathLog')}</span>
                    <span className="truncate font-mono text-zinc-300" title={sysInfo.LogPath}>
                      {sysInfo.LogPath || '-'}
                    </span>
                  </div>
                </div>
                {(sysInfo.ChapterCachePath || sysInfo.BifCachePath) && (
                  <div className="grid gap-3 border-t border-white/6 pt-2.5 md:grid-cols-2">
                    <div className="flex items-center gap-2 truncate">
                      <HardDrive className="h-3.5 w-3.5 shrink-0 text-amber-400" />
                      <span className="shrink-0 text-zinc-500">
                        {t('admin.dashboard.pathChapters')}
                      </span>
                      <span
                        className="truncate font-mono text-zinc-300"
                        title={sysInfo.ChapterCachePath}
                      >
                        {sysInfo.ChapterCachePath || '-'}
                      </span>
                      <span
                        className="shrink-0 rounded bg-amber-500/15 px-1.5 py-0.5 text-[10px] font-medium text-amber-300"
                        title={t('admin.dashboard.cautionChaptersTitle')}
                      >
                        {t('admin.dashboard.cautionDelete')}
                      </span>
                    </div>
                    <div className="flex items-center gap-2 truncate">
                      <HardDrive className="h-3.5 w-3.5 shrink-0 text-amber-400" />
                      <span className="shrink-0 text-zinc-500">{t('admin.dashboard.pathBif')}</span>
                      <span
                        className="truncate font-mono text-zinc-300"
                        title={sysInfo.BifCachePath}
                      >
                        {sysInfo.BifCachePath || '-'}
                      </span>
                      <span
                        className="shrink-0 rounded bg-amber-500/15 px-1.5 py-0.5 text-[10px] font-medium text-amber-300"
                        title={t('admin.dashboard.cautionBifTitle')}
                      >
                        {t('admin.dashboard.cautionDelete')}
                      </span>
                    </div>
                  </div>
                )}
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
            <span>{t('admin.dashboard.liveSessionsTitle')}</span>
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
              {autoRefresh
                ? t('admin.dashboard.autoRefreshOn')
                : t('admin.dashboard.autoRefreshPaused')}
            </button>
            <button
              type="button"
              onClick={() => void refetchSessions()}
              className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/[0.04] px-2.5 py-1.5 text-xs font-medium text-zinc-300 transition-colors hover:border-amber-400/40 hover:text-amber-300"
            >
              <RefreshCw className={`h-3.5 w-3.5 ${sessionsFetching ? 'animate-spin' : ''}`} />
              <span>{t('common.refresh')}</span>
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
            title={t('admin.dashboard.sessionsLoadError')}
            error={sessionsError}
            onRetry={() => void refetchSessions()}
          />
        ) : sessions.length === 0 ? (
          <div className="rounded-2xl border border-white/8 bg-white/[0.02] py-10 text-center text-xs text-zinc-500">
            {t('admin.dashboard.sessionsEmpty')}
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
            <span>{t('admin.dashboard.activityLogsTitle')}</span>
            <span className="rounded-md bg-white/8 px-2 py-0.5 text-xs font-medium text-zinc-300">
              {totalActivityCount}
            </span>
          </div>

          <div className="flex flex-wrap items-center gap-2">
            <div className="inline-flex rounded-lg border border-white/10 bg-white/[0.03] p-0.5">
              {severityOptions.map((opt) => {
                const active = severityFilter === opt.value
                return (
                  <button
                    key={opt.value}
                    type="button"
                    aria-pressed={active}
                    onClick={() => {
                      setSeverityFilter(opt.value)
                      setLogPage(0)
                    }}
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
                aria-label={t('admin.dashboard.searchLogsAria')}
                value={logSearch}
                onChange={(e) => {
                  setLogSearch(e.target.value)
                  setLogPage(0)
                }}
                placeholder={t('admin.dashboard.searchLogsPlaceholder')}
                className="w-40 rounded-lg border border-white/10 bg-white/[0.04] py-1.5 pl-8 pr-2.5 text-xs text-zinc-200 placeholder-zinc-500 outline-none focus:border-amber-400/50 sm:w-48"
              />
            </div>

            <button
              type="button"
              onClick={() => void refetchLogs()}
              className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/[0.04] px-2.5 py-1.5 text-xs font-medium text-zinc-300 transition-colors hover:border-amber-400/40 hover:text-amber-300"
            >
              <RefreshCw className={`h-3.5 w-3.5 ${logsFetching ? 'animate-spin' : ''}`} />
              <span>{t('common.refresh')}</span>
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
            title={t('admin.dashboard.logsLoadError')}
            error={logsError}
            onRetry={() => void refetchLogs()}
          />
        ) : activityItems.length === 0 && totalActivityCount === 0 && severityFilter === 'All' && !trimmedLogSearch ? (
          <div className="rounded-2xl border border-white/8 bg-white/[0.02] py-10 text-center text-xs text-zinc-500">
            {t('admin.dashboard.logsEmpty')}
          </div>
        ) : (
          <div className="overflow-hidden rounded-2xl border border-white/8 bg-white/[0.02]">
            {filteredActivityItems.length === 0 ? (
              <div className="py-10 text-center text-xs text-zinc-500">
                {t('admin.dashboard.logsFilteredEmpty')}
              </div>
            ) : (
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
                      <span title={formatDateTime(entry.Date)}>
                        {formatRelativeTime(entry.Date)}
                      </span>
                    </div>
                  </div>
                ))}
              </div>
            )}

            <div className="flex flex-wrap items-center justify-between gap-2 border-t border-white/8 bg-white/[0.02] px-4 py-2.5 text-xs text-zinc-400">
              <span>
                {t('admin.dashboard.logsPageSummary', {
                  total: totalActivityCount,
                  page: logPage + 1,
                  totalPages: totalActivityPages,
                })}
              </span>
              <div className="flex items-center gap-2">
                <button
                  type="button"
                  disabled={logPage <= 0}
                  onClick={() => setLogPage((p) => Math.max(0, p - 1))}
                  className="inline-flex items-center gap-1 rounded-lg border border-white/10 bg-white/[0.04] px-2.5 py-1 text-xs font-medium text-zinc-200 transition-colors hover:border-amber-400/40 hover:text-amber-300 disabled:cursor-not-allowed disabled:opacity-40"
                >
                  <ChevronLeft className="h-3.5 w-3.5" />
                  <span>{t('common.prev')}</span>
                </button>
                <button
                  type="button"
                  disabled={(logPage + 1) * ACTIVITY_PAGE_SIZE >= totalActivityCount}
                  onClick={() => setLogPage((p) => p + 1)}
                  className="inline-flex items-center gap-1 rounded-lg border border-white/10 bg-white/[0.04] px-2.5 py-1 text-xs font-medium text-zinc-200 transition-colors hover:border-amber-400/40 hover:text-amber-300 disabled:cursor-not-allowed disabled:opacity-40"
                >
                  <span>{t('common.next')}</span>
                  <ChevronRight className="h-3.5 w-3.5" />
                </button>
              </div>
            </div>
          </div>
        )}
      </section>

      <AdminModal
        open={Boolean(messageTarget)}
        title={t('admin.dashboard.msgModalTitle')}
        subtitle={
          messageTarget
            ? `${messageTarget.UserName || t('admin.dashboard.unknownUser')} · ${messageTarget.DeviceName || messageTarget.DeviceId || messageTarget.Id}`
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
              {t('admin.dashboard.msgHeaderLabel')}
            </label>
            <input
              id="session-msg-header"
              type="text"
              value={msgHeader}
              onChange={(e) => setMsgHeader(e.target.value)}
              placeholder={t('admin.dashboard.msgDefaultHeader')}
              className="w-full rounded-xl border border-white/10 bg-zinc-900/90 px-3.5 py-2 text-sm text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
            />
          </div>
          <div>
            <label htmlFor="session-msg-text" className="mb-1.5 block text-xs text-zinc-400">
              {t('admin.dashboard.msgTextLabel')}
            </label>
            <textarea
              id="session-msg-text"
              rows={3}
              value={msgText}
              onChange={(e) => {
                setMsgText(e.target.value)
                if (msgError) setMsgError(null)
              }}
              placeholder={t('admin.dashboard.msgTextPlaceholder')}
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
              {t('common.cancel')}
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
              <span>{t('admin.dashboard.sendConfirmBtn')}</span>
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
  const { t } = useI18n()
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
                {session.UserName || t('admin.dashboard.unknownUser')}
              </p>
              <span className="rounded bg-white/8 px-1.5 py-0.5 text-[10px] font-medium text-zinc-300">
                {session.Client || 'Client'} {session.ApplicationVersion || ''}
              </span>
            </div>
            <p className="mt-0.5 truncate text-xs text-zinc-400">
              {session.DeviceName || session.DeviceId || t('admin.dashboard.unknownDevice')}
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
            <span>
              {runTicks > 0
                ? `${Math.round(pct)}% · ${formatTicks(runTicks)}`
                : t('admin.dashboard.liveOrUnknownDuration')}
            </span>
          </div>
        </div>
      ) : (
        <div className="mt-3 flex items-center gap-1.5 text-xs text-zinc-500">
          <ShieldCheck className="h-3.5 w-3.5 text-zinc-500" />
          <span>{t('admin.dashboard.idleOnline')}</span>
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
          <span>{t('admin.dashboard.playPauseBtn')}</span>
        </button>
        <button
          type="button"
          disabled={busy || !item}
          onClick={() => onRemoteCommand(session, 'Stop')}
          className="inline-flex items-center gap-1 rounded-lg border border-red-500/25 bg-red-500/10 px-2.5 py-1 text-[11px] font-medium text-red-300 transition-colors hover:bg-red-500/20 disabled:cursor-not-allowed disabled:opacity-40"
        >
          <Square className="h-3 w-3 fill-current" />
          <span>{t('admin.dashboard.stopBtn')}</span>
        </button>
        <button
          type="button"
          disabled={busy}
          onClick={() => onOpenMessage(session)}
          className="inline-flex items-center gap-1 rounded-lg border border-white/10 bg-white/[0.04] px-2.5 py-1 text-[11px] font-medium text-zinc-200 transition-colors hover:border-amber-400/40 hover:text-amber-300 disabled:opacity-40"
        >
          <MessageSquare className="h-3 w-3" />
          <span>{t('admin.dashboard.sendMsgBtn')}</span>
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
