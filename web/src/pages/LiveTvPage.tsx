import { useEffect, useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import Hls from 'hls.js'
import {
  AlertCircle,
  CalendarClock,
  Loader2,
  Radio,
  Search,
  Settings,
  Tv,
} from 'lucide-react'
import { Link, useSearchParams } from 'react-router-dom'
import { api, getLiveTvChannelStreamUrl } from '../api/client'
import { QueryErrorState } from '../components/QueryErrorState'
import { useAuth } from '../context/AuthContext'
import { DEMO_VIDEO_DATA_URL } from '../demo/demoMedia'
import { isDemoMode } from '../demo/demoMode'

interface LiveTvProgramItem {
  Id: string
  ChannelId: string
  Name: string
  Overview?: string
  StartDate?: string
  EndDate?: string
}

function formatTimeRange(startIso?: string, endIso?: string): string {
  if (!startIso && !endIso) return ''
  const fmt = (iso?: string) => {
    if (!iso) return '--:--'
    const d = new Date(iso)
    if (Number.isNaN(d.getTime())) return '--:--'
    return d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false })
  }
  return `${fmt(startIso)} - ${fmt(endIso)}`
}

function getProgramProgress(startIso?: string, endIso?: string, nowMs = Date.now()): number {
  if (!startIso || !endIso) return 0
  const start = Date.parse(startIso)
  const end = Date.parse(endIso)
  if (Number.isNaN(start) || Number.isNaN(end) || end <= start) return 0
  return Math.min(100, Math.max(0, ((nowMs - start) / (end - start)) * 100))
}

export function LiveTvPage() {
  const { isAdmin } = useAuth()
  const [searchParams, setSearchParams] = useSearchParams()
  const [searchQuery, setSearchQuery] = useState('')
  const [playbackError, setPlaybackError] = useState<string | null>(null)
  const videoRef = useRef<HTMLVideoElement | null>(null)
  const demoActive = isDemoMode()

  const {
    data: channelsData,
    isLoading: channelsLoading,
    isError: channelsError,
    error: channelsErr,
    refetch: refetchChannels,
  } = useQuery({
    queryKey: ['liveTv', 'channels'],
    queryFn: () => api.getLiveTvChannels(200),
  })

  const channels = useMemo(() => channelsData?.Items || [], [channelsData])

  const { data: programsData } = useQuery({
    queryKey: ['liveTv', 'programs'],
    queryFn: () => api.getLiveTvPrograms(undefined, 200),
    enabled: channels.length > 0,
    staleTime: 60_000,
  })

  const programs = useMemo<LiveTvProgramItem[]>(() => programsData?.Items || [], [programsData])

  const requestedChannelId = searchParams.get('channel') || ''

  const activeChannel = useMemo(() => {
    if (channels.length === 0) return null
    if (requestedChannelId) {
      const matched = channels.find((ch) => ch.Id === requestedChannelId)
      if (matched) return matched
    }
    return channels[0]
  }, [channels, requestedChannelId])

  const handleSelectChannel = (channelId: string) => {
    setPlaybackError(null)
    const next = new URLSearchParams(searchParams)
    next.set('channel', channelId)
    setSearchParams(next, { replace: true })
  }

  // 根据频道 ID 分组当前与即将播出的 EPG 节目
  const programsByChannel = useMemo(() => {
    const nowMs = Date.now()
    const map = new Map<
      string,
      { current: LiveTvProgramItem | null; upcoming: LiveTvProgramItem[] }
    >()

    const grouped = new Map<string, LiveTvProgramItem[]>()
    for (const p of programs) {
      const list = grouped.get(p.ChannelId) || []
      list.push(p)
      grouped.set(p.ChannelId, list)
    }

    for (const [chId, list] of grouped.entries()) {
      const sorted = [...list].sort((a, b) => {
        const ta = a.StartDate ? Date.parse(a.StartDate) : 0
        const tb = b.StartDate ? Date.parse(b.StartDate) : 0
        return ta - tb
      })
      let current: LiveTvProgramItem | null = null
      const upcoming: LiveTvProgramItem[] = []
      for (const prog of sorted) {
        const start = prog.StartDate ? Date.parse(prog.StartDate) : Number.NaN
        const end = prog.EndDate ? Date.parse(prog.EndDate) : Number.NaN
        if (!Number.isNaN(start) && !Number.isNaN(end) && start <= nowMs && end > nowMs) {
          if (!current) current = prog
        } else if (!Number.isNaN(start) && start > nowMs) {
          upcoming.push(prog)
        }
      }
      if (!current && sorted.length > 0 && upcoming.length === 0) {
        current = sorted[0]
      }
      map.set(chId, { current, upcoming })
    }

    return map
  }, [programs])

  const activeEpg = activeChannel ? programsByChannel.get(activeChannel.Id) : undefined

  const filteredChannels = useMemo(() => {
    const q = searchQuery.trim().toLowerCase()
    if (!q) return channels
    return channels.filter(
      (ch) =>
        ch.Name.toLowerCase().includes(q) ||
        (ch.Number && ch.Number.toLowerCase().includes(q)),
    )
  }, [channels, searchQuery])

  // 挂载播放器流（Demo 模式直连离线演示视频；实机优先 hls.js，不支持或非 HLS 时回退原生 video）
  useEffect(() => {
    const video = videoRef.current
    if (!video || !activeChannel) return

    setPlaybackError(null)

    if (demoActive) {
      video.src = DEMO_VIDEO_DATA_URL
      video.load()
      return () => {
        video.pause()
      }
    }

    const streamUrl = getLiveTvChannelStreamUrl(activeChannel.Id)
    const hintUrl = (activeChannel.StreamUrl || streamUrl).toLowerCase()
    const looksLikeHls = hintUrl.includes('.m3u8') || hintUrl.includes('hls')

    let hls: Hls | null = null

    if (looksLikeHls && Hls.isSupported()) {
      hls = new Hls({
        enableWorker: true,
        lowLatencyMode: true,
      })
      hls.loadSource(streamUrl)
      hls.attachMedia(video)
      hls.on(Hls.Events.ERROR, (_event, data) => {
        if (data.fatal) {
          if (data.type === Hls.ErrorTypes.NETWORK_ERROR) {
            setPlaybackError('直播流网络连接失败，请检查直播源可用性或稍后重试')
          } else if (data.type === Hls.ErrorTypes.MEDIA_ERROR) {
            hls?.recoverMediaError()
          } else {
            setPlaybackError('直播流解码异常，尝试切换频道或检查流格式')
          }
        }
      })
    } else {
      video.src = streamUrl
      video.load()
    }

    return () => {
      if (hls) {
        hls.destroy()
      }
      video.pause()
      video.removeAttribute('src')
    }
  }, [activeChannel, demoActive])

  if (channelsLoading) {
    return (
      <div className="flex min-h-[50vh] items-center justify-center">
        <Loader2 className="h-7 w-7 animate-spin text-amber-400" />
      </div>
    )
  }

  if (channelsError) {
    return (
      <QueryErrorState
        title="无法加载电视频道"
        error={channelsErr}
        onRetry={() => void refetchChannels()}
        backTo="/"
      />
    )
  }

  if (channels.length === 0) {
    return (
      <div className="mx-auto max-w-2xl px-4 py-16 text-center lg:px-8">
        <div className="rounded-2xl border border-white/10 bg-white/[0.02] p-10">
          <div className="mx-auto mb-4 flex h-14 w-14 items-center justify-center rounded-2xl bg-amber-500/15 text-amber-400 ring-1 ring-amber-500/30">
            <Radio className="h-7 w-7" />
          </div>
          <h1 className="text-lg font-semibold text-white">暂无可观看的电视频道</h1>
          <p className="mt-2 text-xs leading-relaxed text-zinc-400">
            当前服务器尚未导入 M3U 直播源或频道列表为空。
          </p>
          {isAdmin && (
            <Link
              to="/admin/livetv"
              className="mt-6 inline-flex items-center gap-1.5 rounded-xl bg-amber-500 px-5 py-2.5 text-xs font-semibold text-zinc-950 transition-colors hover:bg-amber-400"
            >
              <Settings className="h-3.5 w-3.5" />
              <span>前往配置直播源</span>
            </Link>
          )}
        </div>
      </div>
    )
  }

  const currentProgram = activeEpg?.current || null
  const upcomingPrograms = activeEpg?.upcoming || []
  const currentProgress = currentProgram
    ? getProgramProgress(currentProgram.StartDate, currentProgram.EndDate)
    : 0

  return (
    <div className="space-y-6 px-4 pt-6 pb-16 lg:px-8">
      {/* 页面头部 */}
      <div className="flex flex-wrap items-center justify-between gap-4 border-b border-white/8 pb-4">
        <div>
          <h1 className="flex items-center gap-2.5 text-2xl font-bold tracking-tight text-white">
            <Radio className="h-6 w-6 text-amber-400" />
            <span>电视直播</span>
          </h1>
          <p className="mt-1 text-xs text-zinc-400">
            共 {channels.length} 个在线频道 · 支持 HLS 直播与实时 EPG 节目单
          </p>
        </div>

        {isAdmin && (
          <Link
            to="/admin/livetv"
            className="inline-flex items-center gap-1.5 rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2 text-xs font-medium text-zinc-300 transition-colors hover:border-amber-400/40 hover:text-amber-300"
          >
            <Settings className="h-3.5 w-3.5" />
            <span>管理直播源</span>
          </Link>
        )}
      </div>

      {/* 2 栏主体布局：左播放器 + EPG，右可搜索频道列表 */}
      <div className="grid gap-6 lg:grid-cols-12">
        {/* 左栏：播放器与节目单 */}
        <div className="space-y-4 lg:col-span-8">
          <div className="overflow-hidden rounded-2xl border border-white/10 bg-black shadow-2xl">
            <div className="relative aspect-video w-full bg-zinc-950">
              <video
                ref={videoRef}
                data-testid="livetv-video"
                controls
                playsInline
                autoPlay
                onError={() => {
                  if (!demoActive) {
                    setPlaybackError('直播流加载失败，请确认直播源地址有效且支持浏览器播放')
                  }
                }}
                className="h-full w-full bg-black object-contain"
              />

              {activeChannel && (
                <div className="pointer-events-none absolute top-3 left-3 flex items-center gap-2 rounded-full bg-black/65 px-3 py-1 text-xs font-medium text-white backdrop-blur-md">
                  <span className="h-2 w-2 animate-pulse rounded-full bg-red-500" />
                  <span>LIVE</span>
                  {activeChannel.Number && (
                    <span className="font-mono text-amber-300">#{activeChannel.Number}</span>
                  )}
                  <span className="truncate">{activeChannel.Name}</span>
                </div>
              )}
            </div>

            {playbackError && (
              <div
                role="alert"
                className="flex items-center gap-2 border-t border-red-500/25 bg-red-950/40 px-4 py-2.5 text-xs text-red-200"
              >
                <AlertCircle className="h-4 w-4 shrink-0 text-red-400" />
                <span>{playbackError}</span>
              </div>
            )}
          </div>

          {/* 当前频道信息与 EPG 节目单 */}
          {activeChannel && (
            <div
              data-testid="livetv-epg-panel"
              className="space-y-4 rounded-2xl border border-white/8 bg-white/[0.02] p-5"
            >
              <div className="flex flex-wrap items-center justify-between gap-2 border-b border-white/8 pb-3">
                <div className="flex items-center gap-3">
                  <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-amber-500/15 font-mono text-xs font-bold text-amber-300 ring-1 ring-amber-500/30">
                    {activeChannel.Number || 'TV'}
                  </div>
                  <div>
                    <h2 className="text-base font-semibold text-white">{activeChannel.Name}</h2>
                    <p className="text-[11px] text-zinc-400">
                      {activeChannel.ChannelType || 'TV'}
                      {demoActive ? ' · Demo 离线演示流' : ' · 实时直播流'}
                    </p>
                  </div>
                </div>
              </div>

              {currentProgram ? (
                <div className="space-y-2.5">
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <div className="flex items-center gap-2">
                      <span className="rounded-md bg-amber-500/20 px-2 py-0.5 text-[10px] font-semibold text-amber-300">
                        正在播出
                      </span>
                      <span className="text-sm font-semibold text-zinc-100">
                        {currentProgram.Name}
                      </span>
                    </div>
                    <span className="font-mono text-xs text-zinc-400">
                      {formatTimeRange(currentProgram.StartDate, currentProgram.EndDate)}
                    </span>
                  </div>
                  <div className="h-1.5 w-full overflow-hidden rounded-full bg-white/10">
                    <div
                      className="h-full rounded-full bg-amber-400 transition-all"
                      style={{ width: `${currentProgress}%` }}
                    />
                  </div>
                  {currentProgram.Overview && (
                    <p className="text-xs leading-relaxed text-zinc-400">
                      {currentProgram.Overview}
                    </p>
                  )}
                </div>
              ) : (
                <div className="flex items-center gap-2 text-xs text-zinc-500">
                  <CalendarClock className="h-4 w-4 text-zinc-600" />
                  <span>暂无当前频道的 EPG 节目单信息</span>
                </div>
              )}

              {upcomingPrograms.length > 0 && (
                <div className="space-y-2 border-t border-white/8 pt-3">
                  <h3 className="flex items-center gap-1.5 text-xs font-semibold text-zinc-300">
                    <CalendarClock className="h-3.5 w-3.5 text-amber-400" />
                    <span>即将播出</span>
                  </h3>
                  <div className="divide-y divide-white/6 rounded-xl border border-white/8 bg-black/20">
                    {upcomingPrograms.slice(0, 5).map((prog) => (
                      <div
                        key={prog.Id}
                        className="flex items-center justify-between gap-3 px-3.5 py-2.5 text-xs"
                      >
                        <div className="min-w-0">
                          <p className="truncate font-medium text-zinc-200">{prog.Name}</p>
                          {prog.Overview && (
                            <p className="mt-0.5 truncate text-[11px] text-zinc-500">
                              {prog.Overview}
                            </p>
                          )}
                        </div>
                        <span className="shrink-0 font-mono text-[11px] text-zinc-400">
                          {formatTimeRange(prog.StartDate, prog.EndDate)}
                        </span>
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </div>
          )}
        </div>

        {/* 右栏：可搜索频道列表 */}
        <aside className="flex flex-col rounded-2xl border border-white/8 bg-white/[0.02] p-4 lg:col-span-4">
          <div className="mb-3 flex items-center justify-between">
            <h2 className="flex items-center gap-2 text-sm font-semibold text-zinc-100">
              <Tv className="h-4 w-4 text-amber-400" />
              <span>频道列表</span>
            </h2>
            <span className="rounded-md bg-white/8 px-2 py-0.5 font-mono text-[11px] text-zinc-400">
              {filteredChannels.length}/{channels.length}
            </span>
          </div>

          <div className="relative mb-3">
            <Search className="pointer-events-none absolute top-1/2 left-3 h-3.5 w-3.5 -translate-y-1/2 text-zinc-500" />
            <input
              type="text"
              aria-label="搜索频道"
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder="搜索频道名称或频道号..."
              className="w-full rounded-xl border border-white/10 bg-zinc-900/90 py-2 pr-3 pl-8 text-xs text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
            />
          </div>

          <div
            data-testid="livetv-channel-list"
            className="max-h-[560px] flex-1 space-y-1.5 overflow-y-auto pr-1"
          >
            {filteredChannels.length === 0 ? (
              <div className="py-10 text-center text-xs text-zinc-500">
                没有匹配「{searchQuery}」的频道
              </div>
            ) : (
              filteredChannels.map((ch) => {
                const isSelected = activeChannel?.Id === ch.Id
                const chCurrent = programsByChannel.get(ch.Id)?.current
                return (
                  <button
                    key={ch.Id}
                    type="button"
                    aria-pressed={isSelected}
                    onClick={() => handleSelectChannel(ch.Id)}
                    className={`flex w-full items-center gap-3 rounded-xl border px-3 py-2.5 text-left transition-all ${
                      isSelected
                        ? 'border-amber-400/50 bg-amber-500/15 text-amber-200'
                        : 'border-white/6 bg-white/[0.02] text-zinc-200 hover:border-white/15 hover:bg-white/[0.05]'
                    }`}
                  >
                    <div
                      className={`flex h-9 w-9 shrink-0 items-center justify-center rounded-lg font-mono text-xs font-bold ${
                        isSelected
                          ? 'bg-amber-500 text-zinc-950'
                          : 'bg-white/8 text-amber-300'
                      }`}
                    >
                      {ch.Number || 'TV'}
                    </div>
                    <div className="min-w-0 flex-1">
                      <p className="truncate text-xs font-semibold">{ch.Name}</p>
                      <p className="mt-0.5 truncate text-[11px] text-zinc-400">
                        {chCurrent ? `正在播出：${chCurrent.Name}` : ch.ChannelType || 'TV'}
                      </p>
                    </div>
                  </button>
                )
              })
            )}
          </div>
        </aside>
      </div>
    </div>
  )
}
