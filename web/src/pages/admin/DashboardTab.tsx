import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  Activity,
  Cpu,
  HardDrive,
  Loader2,
  MonitorPlay,
  Pause,
  Play,
  Radio,
  RefreshCw,
  Server,
  ShieldCheck,
  Terminal,
} from 'lucide-react'
import { api } from '../../api/client'
import type { SessionInfo } from '../../api/types'
import { formatDateTime, formatRelativeTime, formatTicks } from './adminUtils'

export function DashboardTab() {
  const [autoRefresh, setAutoRefresh] = useState(true)

  const { data: sysInfo, isLoading: sysLoading } = useQuery({
    queryKey: ['admin', 'systemInfo'],
    queryFn: () => api.getSystemInfo(),
  })

  const {
    data: sessions = [],
    isLoading: sessionsLoading,
    isFetching: sessionsFetching,
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
    refetch: refetchLogs,
  } = useQuery({
    queryKey: ['admin', 'activityLogs'],
    queryFn: () => api.getActivityLogs(0, 30),
    refetchInterval: autoRefresh ? 15000 : false,
  })

  const activityItems = activityData?.Items || []
  const playingCount = sessions.filter((s) => Boolean(s.NowPlayingItem)).length

  return (
    <div className="space-y-8">
      {/* 服务器状态卡片 */}
      <section className="space-y-4">
        <div className="flex items-center justify-between gap-3">
          <div className="flex items-center gap-2 text-sm font-semibold text-zinc-200">
            <Server className="h-4 w-4 text-amber-400" />
            <span>服务器状态概览</span>
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
                value={`v${sysInfo?.Version || '0.1.0'}`}
                sub={`HTTP 端口: ${sysInfo?.HttpServerPortNumber || 8096}`}
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
        ) : sessions.length === 0 ? (
          <div className="rounded-2xl border border-white/8 bg-white/[0.02] py-10 text-center text-xs text-zinc-500">
            当前暂无在线活动会话
          </div>
        ) : (
          <div className="grid gap-4 lg:grid-cols-2">
            {sessions.map((session) => (
              <SessionCard key={session.Id || session.DeviceId} session={session} />
            ))}
          </div>
        )}
      </section>

      {/* 系统活动日志 */}
      <section className="space-y-4">
        <div className="flex items-center justify-between gap-3">
          <div className="flex items-center gap-2 text-sm font-semibold text-zinc-200">
            <Activity className="h-4 w-4 text-amber-400" />
            <span>系统活动日志</span>
            <span className="rounded-md bg-white/8 px-2 py-0.5 text-xs font-medium text-zinc-300">
              {activityData?.TotalRecordCount ?? activityItems.length}
            </span>
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

        {logsLoading ? (
          <div className="flex h-32 items-center justify-center rounded-2xl border border-white/8 bg-white/[0.02]">
            <Loader2 className="h-5 w-5 animate-spin text-amber-400" />
          </div>
        ) : activityItems.length === 0 ? (
          <div className="rounded-2xl border border-white/8 bg-white/[0.02] py-10 text-center text-xs text-zinc-500">
            暂无活动日志记录
          </div>
        ) : (
          <div className="overflow-hidden rounded-2xl border border-white/8 bg-white/[0.02]">
            <div className="divide-y divide-white/6">
              {activityItems.map((entry) => (
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
          </div>
        )}
      </section>
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

function SessionCard({ session }: { session: SessionInfo }) {
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
