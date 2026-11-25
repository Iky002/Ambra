import { useEffect, useMemo, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import Hls from 'hls.js'
import {
  AlertCircle,
  CalendarClock,
  CheckCircle2,
  CircleDot,
  Download,
  Film,
  Loader2,
  Play,
  Radio,
  RefreshCw,
  Search,
  Settings,
  Trash2,
  Tv,
  X,
} from 'lucide-react'
import { Link, useSearchParams } from 'react-router-dom'
import {
  api,
  buildEmbyAuthHeader,
  getLiveTvChannelStreamUrl,
  getLiveTvRecordingStreamUrl,
  getStoredAuth,
  isSameOriginUrl,
} from '../api/client'
import type { LiveTvRecordingInfo } from '../api/types'
import { QueryErrorState } from '../components/QueryErrorState'
import { useAuth } from '../context/AuthContext'
import { DEMO_VIDEO_DATA_URL } from '../demo/demoMedia'
import { isDemoMode } from '../demo/demoMode'
import { getActiveLocale, useI18n } from '../i18n/I18nContext'

type LiveTvSubTab = 'channels' | 'timers' | 'recordings'

interface LiveTvProgramItem {
  Id: string
  ChannelId: string
  Name: string
  Overview?: string
  StartDate?: string
  EndDate?: string
}

const MAX_HLS_MEDIA_RECOVERIES = 2
const MEDIA_ERR_SRC_NOT_SUPPORTED = 4

function formatTimeRange(startIso?: string, endIso?: string): string {
  if (!startIso && !endIso) return ''
  const fmt = (iso?: string) => {
    if (!iso) return '--:--'
    const d = new Date(iso)
    if (Number.isNaN(d.getTime())) return '--:--'
    return d.toLocaleTimeString(getActiveLocale(), { hour: '2-digit', minute: '2-digit', hour12: false })
  }
  return `${fmt(startIso)} - ${fmt(endIso)}`
}

function formatDateTimeRange(startIso?: string, endIso?: string): string {
  if (!startIso && !endIso) return '未知时间'
  const fmt = (iso?: string) => {
    if (!iso) return '--'
    const d = new Date(iso)
    if (Number.isNaN(d.getTime())) return iso
    return d.toLocaleString(getActiveLocale(), {
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      hour12: false,
    })
  }
  return `${fmt(startIso)} ~ ${fmt(endIso)}`
}

function getProgramProgress(startIso?: string, endIso?: string, nowMs = Date.now()): number {
  if (!startIso || !endIso) return 0
  const start = Date.parse(startIso)
  const end = Date.parse(endIso)
  if (Number.isNaN(start) || Number.isNaN(end) || end <= start) return 0
  return Math.min(100, Math.max(0, ((nowMs - start) / (end - start)) * 100))
}

export function LiveTvPage() {
  const { isAdmin, user } = useAuth()
  const canManageLiveTv = isAdmin || Boolean(user?.Policy?.EnableLiveTvManagement)
  const { locale, t } = useI18n()
  const queryClient = useQueryClient()
  const [searchParams, setSearchParams] = useSearchParams()
  const [activeSubTab, setActiveSubTab] = useState<LiveTvSubTab>('channels')
  const [searchQuery, setSearchQuery] = useState('')
  const [playbackError, setPlaybackError] = useState<string | null>(null)
  const [nowMs, setNowMs] = useState(() => Date.now())
  const [streamRetryTick, setStreamRetryTick] = useState(0)
  const [forceHls, setForceHls] = useState(false)
  const [dvrFeedback, setDvrFeedback] = useState<{
    type: 'success' | 'error'
    message: string
  } | null>(null)
  const [schedulingProgramId, setSchedulingProgramId] = useState<string | null>(null)
  const [playingRecording, setPlayingRecording] = useState<LiveTvRecordingInfo | null>(null)
  const [recordingPlaybackError, setRecordingPlaybackError] = useState<string | null>(null)
  const videoRef = useRef<HTMLVideoElement | null>(null)
  const mediaRecoveryCountRef = useRef(0)
  const demoActive = isDemoMode()

  // 每 30 秒刷新当前时间戳，驱动 EPG「正在播出 / 即将播出」与进度条实时更新
  useEffect(() => {
    const timerId = window.setInterval(() => {
      setNowMs(Date.now())
    }, 30_000)
    return () => window.clearInterval(timerId)
  }, [])

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

  const {
    data: timersData,
    isLoading: timersLoading,
    isError: timersError,
    error: timersErr,
    refetch: refetchTimers,
  } = useQuery({
    queryKey: ['liveTv', 'timers'],
    queryFn: () => api.getLiveTvTimers(),
    enabled: activeSubTab === 'timers' || channels.length > 0,
  })

  const timers = useMemo(() => timersData?.Items || [], [timersData])

  const scheduledProgramIds = useMemo(
    () => timers.map((t) => t.ProgramId).filter((id): id is string => Boolean(id)),
    [timers],
  )

  const {
    data: recordingsData,
    isLoading: recordingsLoading,
    isError: recordingsError,
    error: recordingsErr,
    refetch: refetchRecordings,
  } = useQuery({
    queryKey: ['liveTv', 'recordings'],
    queryFn: () => api.getLiveTvRecordings(),
    enabled: activeSubTab === 'recordings',
  })

  const recordings = useMemo(() => recordingsData?.Items || [], [recordingsData])

  const createTimerMutation = useMutation({
    mutationFn: (prog: LiveTvProgramItem) => {
      const startIso = prog.StartDate || new Date(nowMs).toISOString()
      const endIso = prog.EndDate || new Date(Date.parse(startIso) + 3_600_000).toISOString()
      return api.createLiveTvTimer({
        ChannelId: activeChannel!.Id,
        ChannelName: activeChannel!.Name,
        ProgramId: prog.Id,
        Name: prog.Name,
        Overview: prog.Overview,
        StartDate: startIso,
        EndDate: endIso,
      })
    },
    onMutate: (prog) => {
      setSchedulingProgramId(prog.Id)
      setDvrFeedback(null)
    },
    onSuccess: async (_created, prog) => {
      setDvrFeedback({
        type: 'success',
        message: `已预约录制「${prog.Name}」`,
      })
      await queryClient.invalidateQueries({ queryKey: ['liveTv', 'timers'] })
    },
    onError: (err) => {
      setDvrFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '预约录制失败，请稍后重试',
      })
    },
    onSettled: () => {
      setSchedulingProgramId(null)
    },
  })

  const deleteTimerMutation = useMutation({
    mutationFn: (timerId: string) => api.deleteLiveTvTimer(timerId),
    onSuccess: async () => {
      setDvrFeedback({ type: 'success', message: '已取消录制预约' })
      await queryClient.invalidateQueries({ queryKey: ['liveTv', 'timers'] })
    },
    onError: (err) => {
      setDvrFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '取消预约失败，请稍后重试',
      })
    },
  })

  const deleteRecordingMutation = useMutation({
    mutationFn: (recordingId: string) => api.deleteLiveTvRecording(recordingId),
    onSuccess: async (_data, recordingId) => {
      setPlayingRecording((prev) => (prev?.Id === recordingId ? null : prev))
      setDvrFeedback({ type: 'success', message: '已删除录制文件' })
      await queryClient.invalidateQueries({ queryKey: ['liveTv', 'recordings'] })
    },
    onError: (err) => {
      setDvrFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '删除录制失败，请稍后重试',
      })
    },
  })

  const requestedChannelId = searchParams.get('channel') || ''

  const activeChannel = useMemo(() => {
    if (channels.length === 0) return null
    if (requestedChannelId) {
      const matched = channels.find((ch) => ch.Id === requestedChannelId)
      if (matched) return matched
    }
    return channels[0]
  }, [channels, requestedChannelId])

  const activeStreamUrl = activeChannel ? getLiveTvChannelStreamUrl(activeChannel.Id) : ''
  const activeLooksLikeHls = useMemo(() => {
    if (!activeChannel) return false
    const hintUrl = (activeChannel.StreamUrl || activeStreamUrl).toLowerCase()
    return hintUrl.includes('.m3u8') || hintUrl.includes('hls')
  }, [activeChannel, activeStreamUrl])

  useEffect(() => {
    setForceHls(false)
    mediaRecoveryCountRef.current = 0
  }, [activeChannel?.Id])

  const handleSelectChannel = (channelId: string) => {
    setPlaybackError(null)
    setForceHls(false)
    mediaRecoveryCountRef.current = 0
    const next = new URLSearchParams(searchParams)
    next.set('channel', channelId)
    setSearchParams(next, { replace: true })
  }

  const handleRetryStream = () => {
    setPlaybackError(null)
    mediaRecoveryCountRef.current = 0
    setStreamRetryTick((tick) => tick + 1)
  }

  // 根据频道 ID 分组当前与即将播出的 EPG 节目
  const programsByChannel = useMemo(() => {
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
  }, [programs, nowMs])

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

  // 挂载播放器流（Demo 模式直连离线演示视频；实机优先 hls.js，不支持或非 HLS 时回退原生 video，并在 MEDIA_ERR_SRC_NOT_SUPPORTED 时自动回退 hls.js）
  useEffect(() => {
    if (activeSubTab !== 'channels') return
    const video = videoRef.current
    if (!video || !activeChannel) return

    setPlaybackError(null)
    mediaRecoveryCountRef.current = 0

    if (demoActive) {
      video.src = DEMO_VIDEO_DATA_URL
      video.load()
      return () => {
        video.pause()
      }
    }

    const streamUrl = activeStreamUrl
    const useHls = (activeLooksLikeHls || forceHls) && Hls.isSupported()

    let hls: Hls | null = null

    if (useHls) {
      hls = new Hls({
        enableWorker: true,
        lowLatencyMode: true,
        xhrSetup: (xhr, requestUrl) => {
          if (!isSameOriginUrl(requestUrl)) return
          const storedAuth = getStoredAuth()
          xhr.setRequestHeader(
            'X-Emby-Authorization',
            buildEmbyAuthHeader(storedAuth?.accessToken),
          )
          if (storedAuth?.accessToken) {
            xhr.setRequestHeader('X-Emby-Token', storedAuth.accessToken)
          }
        },
      })
      hls.loadSource(streamUrl)
      hls.attachMedia(video)
      hls.on(Hls.Events.ERROR, (_event, data) => {
        if (data.fatal) {
          if (data.type === Hls.ErrorTypes.NETWORK_ERROR) {
            setPlaybackError('直播流网络连接失败，请检查直播源可用性或稍后重试')
          } else if (data.type === Hls.ErrorTypes.MEDIA_ERROR) {
            if (mediaRecoveryCountRef.current < MAX_HLS_MEDIA_RECOVERIES) {
              mediaRecoveryCountRef.current += 1
              hls?.recoverMediaError()
            } else {
              hls?.destroy()
              setPlaybackError('直播流媒体解码多次失败，请点击重试或切换频道')
            }
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
  }, [activeChannel, activeLooksLikeHls, activeStreamUrl, activeSubTab, demoActive, forceHls, streamRetryTick])

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

  const currentProgram = activeEpg?.current || null
  const upcomingPrograms = activeEpg?.upcoming || []
  const currentProgress = currentProgram
    ? getProgramProgress(currentProgram.StartDate, currentProgram.EndDate, nowMs)
    : 0

  return (
    <div className="space-y-6 px-4 pt-6 pb-16 lg:px-8">
      {/* 页面头部 */}
      <div className="flex flex-wrap items-center justify-between gap-4 border-b border-white/8 pb-4">
        <div>
          <h1 className="flex items-center gap-2.5 text-2xl font-bold tracking-tight text-white">
            <Radio className="h-6 w-6 text-amber-400" />
            <span>{t('livetv.title')}</span>
          </h1>
          <p className="mt-1 text-xs text-zinc-400">
            共 {channels.length} 个在线频道 · 支持 HLS 直播、实时 EPG 节目单与 DVR 预约录制
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-3">
          {/* 子标签切换：直播频道 / 预约计划 / 已录节目 */}
          <div
            role="tablist"
            aria-label="电视直播功能标签"
            className="inline-flex items-center gap-1 rounded-xl border border-white/10 bg-zinc-950/80 p-1"
          >
            {(
              [
                {
                  key: 'channels',
                  label: locale === 'zh-CN' ? '直播频道' : t('livetv.channels'),
                  icon: Tv,
                },
                {
                  key: 'timers',
                  label: locale === 'zh-CN' ? '预约计划' : t('livetv.timers'),
                  icon: CalendarClock,
                },
                {
                  key: 'recordings',
                  label: locale === 'zh-CN' ? '已录节目' : t('livetv.recordings'),
                  icon: Film,
                },
              ] as const
            ).map(({ key, label, icon: Icon }) => {
              const selected = activeSubTab === key
              return (
                <button
                  key={key}
                  type="button"
                  role="tab"
                  aria-selected={selected}
                  onClick={() => {
                    setDvrFeedback(null)
                    setActiveSubTab(key)
                  }}
                  className={`inline-flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium transition-colors ${
                    selected
                      ? 'bg-amber-500 text-zinc-950 shadow-sm'
                      : 'text-zinc-400 hover:bg-white/[0.06] hover:text-zinc-100'
                  }`}
                >
                  <Icon className="h-3.5 w-3.5" />
                  <span>{label}</span>
                </button>
              )
            })}
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
      </div>

      {dvrFeedback && (
        <div
          role="status"
          className={`flex items-center justify-between gap-3 rounded-xl border px-4 py-2.5 text-xs ${
            dvrFeedback.type === 'success'
              ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-200'
              : 'border-red-500/30 bg-red-500/10 text-red-200'
          }`}
        >
          <div className="flex items-center gap-2">
            {dvrFeedback.type === 'success' ? (
              <CheckCircle2 className="h-4 w-4 shrink-0 text-emerald-400" />
            ) : (
              <AlertCircle className="h-4 w-4 shrink-0 text-red-400" />
            )}
            <span>{dvrFeedback.message}</span>
          </div>
          <button
            type="button"
            onClick={() => setDvrFeedback(null)}
            className="text-[11px] opacity-75 hover:opacity-100"
          >
            关闭
          </button>
        </div>
      )}

      {activeSubTab === 'channels' && channels.length === 0 && (
        <div className="mx-auto max-w-2xl py-10 text-center">
          <div className="rounded-2xl border border-white/10 bg-white/[0.02] p-10">
            <div className="mx-auto mb-4 flex h-14 w-14 items-center justify-center rounded-2xl bg-amber-500/15 text-amber-400 ring-1 ring-amber-500/30">
              <Radio className="h-7 w-7" />
            </div>
            <h2 className="text-lg font-semibold text-white">暂无可观看的电视频道</h2>
            <p className="mt-2 text-xs leading-relaxed text-zinc-400">
              当前服务器尚未导入 M3U 直播源或频道列表为空。您仍可切换到「预约计划」或「已录节目」查看历史记录。
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
      )}

      {activeSubTab === 'channels' && channels.length > 0 && (
        /* 2 栏主体布局：左播放器 + EPG，右可搜索频道列表 */
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
                  onError={(e) => {
                    if (demoActive) return
                    const errCode = e.currentTarget.error?.code
                    const srcNotSupported =
                      typeof MediaError !== 'undefined'
                        ? MediaError.MEDIA_ERR_SRC_NOT_SUPPORTED
                        : MEDIA_ERR_SRC_NOT_SUPPORTED
                    if (
                      errCode === srcNotSupported &&
                      !activeLooksLikeHls &&
                      !forceHls &&
                      Hls.isSupported()
                    ) {
                      setForceHls(true)
                      return
                    }
                    setPlaybackError('直播流加载失败，请确认直播源地址有效且支持浏览器播放')
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
                  className="flex items-center justify-between gap-3 border-t border-red-500/25 bg-red-950/40 px-4 py-2.5 text-xs text-red-200"
                >
                  <div className="flex items-center gap-2">
                    <AlertCircle className="h-4 w-4 shrink-0 text-red-400" />
                    <span>{playbackError}</span>
                  </div>
                  <button
                    type="button"
                    onClick={handleRetryStream}
                    className="inline-flex shrink-0 items-center gap-1 rounded-lg border border-red-400/35 bg-red-500/15 px-2.5 py-1 text-[11px] font-medium text-red-100 transition-colors hover:bg-red-500/25"
                  >
                    <RefreshCw className="h-3 w-3" />
                    <span>重试</span>
                  </button>
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
                      <div className="flex items-center gap-2.5">
                        <span className="font-mono text-xs text-zinc-400">
                          {formatTimeRange(currentProgram.StartDate, currentProgram.EndDate)}
                        </span>
                        {canManageLiveTv && (
                          <button
                            type="button"
                            disabled={
                              schedulingProgramId === currentProgram.Id ||
                              scheduledProgramIds.includes(currentProgram.Id)
                            }
                            onClick={() => createTimerMutation.mutate(currentProgram)}
                            className={`inline-flex items-center gap-1 rounded-lg border px-2.5 py-1 text-[11px] font-medium transition-colors disabled:opacity-60 ${
                              scheduledProgramIds.includes(currentProgram.Id)
                                ? 'border-emerald-500/35 bg-emerald-500/15 text-emerald-300'
                                : 'border-amber-500/35 bg-amber-500/10 text-amber-300 hover:bg-amber-500/20'
                            }`}
                          >
                            {schedulingProgramId === currentProgram.Id ? (
                              <Loader2 className="h-3 w-3 animate-spin" />
                            ) : (
                              <CircleDot className="h-3 w-3" />
                            )}
                            <span>
                              {scheduledProgramIds.includes(currentProgram.Id)
                                ? '已预约'
                                : '预约录制'}
                            </span>
                          </button>
                        )}
                      </div>
                    </div>
                    <div className="h-1.5 w-full overflow-hidden rounded-full bg-white/10">
                      <div
                        data-testid="epg-current-progress"
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
                      {upcomingPrograms.slice(0, 5).map((prog) => {
                        const isScheduled = scheduledProgramIds.includes(prog.Id)
                        const isScheduling = schedulingProgramId === prog.Id
                        return (
                          <div
                            key={prog.Id}
                            className="flex items-center justify-between gap-3 px-3.5 py-2.5 text-xs"
                          >
                            <div className="min-w-0 flex-1">
                              <p className="truncate font-medium text-zinc-200">{prog.Name}</p>
                              {prog.Overview && (
                                <p className="mt-0.5 truncate text-[11px] text-zinc-500">
                                  {prog.Overview}
                                </p>
                              )}
                            </div>
                            <div className="flex shrink-0 items-center gap-2.5">
                              <span className="font-mono text-[11px] text-zinc-400">
                                {formatTimeRange(prog.StartDate, prog.EndDate)}
                              </span>
                              {canManageLiveTv && (
                                <button
                                  type="button"
                                  disabled={isScheduling || isScheduled}
                                  onClick={() => createTimerMutation.mutate(prog)}
                                  className={`inline-flex items-center gap-1 rounded-lg border px-2.5 py-1 text-[11px] font-medium transition-colors disabled:opacity-60 ${
                                    isScheduled
                                      ? 'border-emerald-500/35 bg-emerald-500/15 text-emerald-300'
                                      : 'border-amber-500/35 bg-amber-500/10 text-amber-300 hover:bg-amber-500/20'
                                  }`}
                                >
                                  {isScheduling ? (
                                    <Loader2 className="h-3 w-3 animate-spin" />
                                  ) : (
                                    <CircleDot className="h-3 w-3" />
                                  )}
                                  <span>{isScheduled ? '已预约' : '预约录制'}</span>
                                </button>
                              )}
                            </div>
                          </div>
                        )
                      })}
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
                <span>{t('livetv.channels')}</span>
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
      )}

      {activeSubTab === 'timers' && (
        <section
          data-testid="livetv-timers-panel"
          className="space-y-4 rounded-2xl border border-white/8 bg-white/[0.02] p-5"
        >
          <div className="flex items-center justify-between gap-3 border-b border-white/8 pb-3">
            <h2 className="flex items-center gap-2 text-sm font-semibold text-zinc-100">
              <CalendarClock className="h-4 w-4 text-amber-400" />
              <span>DVR 预约计划</span>
              <span className="rounded-md bg-white/8 px-2 py-0.5 font-mono text-[11px] text-zinc-400">
                {timers.length}
              </span>
            </h2>
            <button
              type="button"
              onClick={() => void refetchTimers()}
              className="inline-flex items-center gap-1 rounded-lg border border-white/10 bg-white/[0.04] px-2.5 py-1 text-xs text-zinc-300 hover:border-amber-400/40 hover:text-amber-300"
            >
              <RefreshCw className="h-3 w-3" />
              <span>刷新</span>
            </button>
          </div>

          {timersLoading ? (
            <div className="flex h-36 items-center justify-center">
              <Loader2 className="h-6 w-6 animate-spin text-amber-400" />
            </div>
          ) : timersError ? (
            <QueryErrorState
              title="无法加载预约计划"
              error={timersErr}
              onRetry={() => void refetchTimers()}
            />
          ) : timers.length === 0 ? (
            <div className="py-12 text-center text-xs text-zinc-500">
              暂无录制预约，可在「直播频道」的 EPG 节目单中点击「预约录制」创建
            </div>
          ) : (
            <div className="divide-y divide-white/6 rounded-xl border border-white/8 bg-black/20">
              {timers.map((timer) => (
                <div
                  key={timer.Id}
                  className="flex flex-wrap items-center justify-between gap-3 px-4 py-3 text-xs"
                >
                  <div className="min-w-0 flex-1 space-y-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="font-semibold text-zinc-100">{timer.Name}</span>
                      {timer.ChannelName && (
                        <span className="rounded bg-amber-500/15 px-1.5 py-0.5 text-[10px] font-medium text-amber-300">
                          {timer.ChannelName}
                        </span>
                      )}
                      {timer.Status && (
                        <span className="rounded bg-white/10 px-1.5 py-0.5 text-[10px] text-zinc-300">
                          {timer.Status}
                        </span>
                      )}
                    </div>
                    <p className="font-mono text-[11px] text-zinc-400">
                      {formatDateTimeRange(timer.StartDate, timer.EndDate)}
                    </p>
                    {timer.Overview && (
                      <p className="truncate text-[11px] text-zinc-500">{timer.Overview}</p>
                    )}
                  </div>
                  {canManageLiveTv && (
                    <button
                      type="button"
                      disabled={
                        deleteTimerMutation.isPending && deleteTimerMutation.variables === timer.Id
                      }
                      onClick={() => deleteTimerMutation.mutate(timer.Id)}
                      className="inline-flex shrink-0 items-center gap-1 rounded-lg border border-red-500/30 bg-red-500/10 px-3 py-1.5 text-xs font-medium text-red-300 transition-colors hover:bg-red-500/20 disabled:opacity-50"
                    >
                      {deleteTimerMutation.isPending && deleteTimerMutation.variables === timer.Id ? (
                        <Loader2 className="h-3.5 w-3.5 animate-spin" />
                      ) : (
                        <Trash2 className="h-3.5 w-3.5" />
                      )}
                      <span>取消预约</span>
                    </button>
                  )}
                </div>
              ))}
            </div>
          )}
        </section>
      )}

      {activeSubTab === 'recordings' && (
        <section
          data-testid="livetv-recordings-panel"
          className="space-y-4 rounded-2xl border border-white/8 bg-white/[0.02] p-5"
        >
          <div className="flex items-center justify-between gap-3 border-b border-white/8 pb-3">
            <h2 className="flex items-center gap-2 text-sm font-semibold text-zinc-100">
              <Film className="h-4 w-4 text-amber-400" />
              <span>{t('livetv.recordings')}</span>
              <span className="rounded-md bg-white/8 px-2 py-0.5 font-mono text-[11px] text-zinc-400">
                {recordings.length}
              </span>
            </h2>
            <button
              type="button"
              onClick={() => void refetchRecordings()}
              className="inline-flex items-center gap-1 rounded-lg border border-white/10 bg-white/[0.04] px-2.5 py-1 text-xs text-zinc-300 hover:border-amber-400/40 hover:text-amber-300"
            >
              <RefreshCw className="h-3 w-3" />
              <span>{t('common.refresh')}</span>
            </button>
          </div>

          {playingRecording && (
            <div
              data-testid="dvr-recording-player"
              className="overflow-hidden rounded-2xl border border-white/10 bg-black shadow-2xl"
            >
              <div className="flex items-center justify-between gap-3 border-b border-white/8 bg-zinc-950/90 px-4 py-2.5">
                <div className="flex min-w-0 items-center gap-2">
                  <Film className="h-4 w-4 shrink-0 text-amber-400" />
                  <span className="truncate text-xs font-semibold text-zinc-100">
                    {playingRecording.Name}
                  </span>
                  {playingRecording.ChannelName && (
                    <span className="shrink-0 rounded bg-amber-500/15 px-1.5 py-0.5 text-[10px] font-medium text-amber-300">
                      {playingRecording.ChannelName}
                    </span>
                  )}
                </div>
                <button
                  type="button"
                  aria-label="关闭录像播放器"
                  onClick={() => {
                    setPlayingRecording(null)
                    setRecordingPlaybackError(null)
                  }}
                  className="inline-flex shrink-0 items-center gap-1 rounded-lg border border-white/10 bg-white/[0.04] px-2.5 py-1 text-[11px] font-medium text-zinc-300 transition-colors hover:bg-white/10 hover:text-white"
                >
                  <X className="h-3.5 w-3.5" />
                  <span>关闭</span>
                </button>
              </div>

              <div className="aspect-video w-full bg-zinc-950">
                <video
                  key={playingRecording.Id}
                  data-testid="dvr-recording-video"
                  controls
                  playsInline
                  autoPlay
                  src={
                    demoActive
                      ? DEMO_VIDEO_DATA_URL
                      : getLiveTvRecordingStreamUrl(playingRecording.Id)
                  }
                  onError={(e) => {
                    if (demoActive) return
                    const errCode = e.currentTarget.error?.code
                    const srcNotSupported =
                      typeof MediaError !== 'undefined'
                        ? MediaError.MEDIA_ERR_SRC_NOT_SUPPORTED
                        : MEDIA_ERR_SRC_NOT_SUPPORTED
                    if (errCode === srcNotSupported) {
                      setRecordingPlaybackError(
                        '当前浏览器不支持直接播放此录制格式（如 MKV/TS），请点击右侧按钮直接下载观看',
                      )
                    } else {
                      setRecordingPlaybackError('录制视频加载失败，可尝试直接下载录制文件')
                    }
                  }}
                  className="h-full w-full bg-black object-contain"
                />
              </div>

              {recordingPlaybackError && (
                <div
                  role="alert"
                  className="flex flex-wrap items-center justify-between gap-3 border-t border-amber-500/25 bg-amber-950/40 px-4 py-2.5 text-xs text-amber-200"
                >
                  <div className="flex items-center gap-2">
                    <AlertCircle className="h-4 w-4 shrink-0 text-amber-400" />
                    <span>{recordingPlaybackError}</span>
                  </div>
                  <a
                    href={getLiveTvRecordingStreamUrl(playingRecording.Id)}
                    download
                    className="inline-flex shrink-0 items-center gap-1.5 rounded-lg border border-amber-400/40 bg-amber-500/20 px-3 py-1 text-[11px] font-semibold text-amber-100 transition-colors hover:bg-amber-500/30"
                  >
                    <Download className="h-3.5 w-3.5" />
                    <span>直接下载录制文件</span>
                  </a>
                </div>
              )}
            </div>
          )}

          {recordingsLoading ? (
            <div className="flex h-36 items-center justify-center">
              <Loader2 className="h-6 w-6 animate-spin text-amber-400" />
            </div>
          ) : recordingsError ? (
            <QueryErrorState
              title="无法加载已录节目"
              error={recordingsErr}
              onRetry={() => void refetchRecordings()}
            />
          ) : recordings.length === 0 ? (
            <div className="py-12 text-center text-xs text-zinc-500">暂无已录制的电视节目</div>
          ) : (
            <div className="divide-y divide-white/6 rounded-xl border border-white/8 bg-black/20">
              {recordings.map((rec) => (
                <div
                  key={rec.Id}
                  className="flex flex-wrap items-center justify-between gap-3 px-4 py-3 text-xs"
                >
                  <div className="min-w-0 flex-1 space-y-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="font-semibold text-zinc-100">{rec.Name}</span>
                      {rec.ChannelName && (
                        <span className="rounded bg-amber-500/15 px-1.5 py-0.5 text-[10px] font-medium text-amber-300">
                          {rec.ChannelName}
                        </span>
                      )}
                      {rec.Status && (
                        <span className="rounded bg-white/10 px-1.5 py-0.5 text-[10px] text-zinc-300">
                          {rec.Status}
                        </span>
                      )}
                    </div>
                    <p className="font-mono text-[11px] text-zinc-400">
                      {formatDateTimeRange(rec.StartDate, rec.EndDate)}
                    </p>
                    {rec.Overview && (
                      <p className="truncate text-[11px] text-zinc-500">{rec.Overview}</p>
                    )}
                    {isAdmin && rec.Path && (
                      <p className="truncate font-mono text-[10px] text-zinc-500" title={rec.Path}>
                        {rec.Path}
                      </p>
                    )}
                  </div>
                  <div className="flex shrink-0 items-center gap-2">
                    {rec.Status !== 'InProgress' && (
                      <button
                        type="button"
                        onClick={() => {
                          setRecordingPlaybackError(null)
                          setPlayingRecording(rec)
                        }}
                        className="inline-flex shrink-0 items-center gap-1 rounded-lg border border-amber-500/35 bg-amber-500/15 px-3 py-1.5 text-xs font-medium text-amber-300 transition-colors hover:bg-amber-500/25"
                      >
                        <Play className="h-3.5 w-3.5" />
                        <span>{t('common.play')}</span>
                      </button>
                    )}
                    {isAdmin && (
                      <button
                        type="button"
                        disabled={
                          deleteRecordingMutation.isPending &&
                          deleteRecordingMutation.variables === rec.Id
                        }
                        onClick={() => deleteRecordingMutation.mutate(rec.Id)}
                        className="inline-flex shrink-0 items-center gap-1 rounded-lg border border-red-500/30 bg-red-500/10 px-3 py-1.5 text-xs font-medium text-red-300 transition-colors hover:bg-red-500/20 disabled:opacity-50"
                      >
                        {deleteRecordingMutation.isPending &&
                        deleteRecordingMutation.variables === rec.Id ? (
                          <Loader2 className="h-3.5 w-3.5 animate-spin" />
                        ) : (
                          <Trash2 className="h-3.5 w-3.5" />
                        )}
                        <span>删除录制</span>
                      </button>
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}
        </section>
      )}
    </div>
  )
}
