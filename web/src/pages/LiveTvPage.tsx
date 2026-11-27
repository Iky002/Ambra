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
  Link2,
  Loader2,
  Play,
  Plus,
  Radio,
  RefreshCw,
  Repeat,
  Save,
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
  getLiveTvRecordingHlsUrl,
  getLiveTvRecordingStreamUrl,
  getStoredAuth,
  isSameOriginUrl,
} from '../api/client'
import type { LiveTvRecordingInfo, LiveTvSeriesTimerInfo } from '../api/types'
import { QueryErrorState } from '../components/QueryErrorState'
import { useAudioPlayer } from '../context/AudioPlayerContext'
import { useAuth } from '../context/AuthContext'
import { getCachedDemoServer, loadDemoServer } from '../demo/demoLoader'
import { isDemoMode } from '../demo/demoMode'
import { getActiveLocale, translateForLocale, useI18n } from '../i18n/I18nContext'

type LiveTvSubTab = 'channels' | 'timers' | 'seriesTimers' | 'recordings' | 'mappings'

function attachEmbyHlsAuth(xhr: XMLHttpRequest, requestUrl: string): void {
  if (!isSameOriginUrl(requestUrl)) return
  const storedAuth = getStoredAuth()
  xhr.setRequestHeader('Authorization', buildEmbyAuthHeader(storedAuth?.accessToken))
}

interface LiveTvProgramItem {
  Id: string
  ChannelId: string
  Name: string
  Overview?: string
  StartDate?: string
  EndDate?: string
}

const MAX_HLS_MEDIA_RECOVERIES = 10
const MAX_HLS_NETWORK_RECOVERIES = 10
const HLS_RECOVERY_RESET_MS = 15_000
const UNRECOVERABLE_CODEC_DETAILS = new Set([
  'bufferAddCodecError',
  'manifestIncompatibleCodecsError',
])
const MEDIA_ERR_SRC_NOT_SUPPORTED = 4

function formatHlsError(msg: string, details?: string): string {
  const trimmed = details?.trim()
  return trimmed ? `${msg} (${trimmed})` : msg
}

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
  if (!startIso && !endIso) return translateForLocale(getActiveLocale(), 'liveTvUi.unknownTime')
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
  const { pause: pauseGlobalAudio } = useAudioPlayer()
  const pauseGlobalAudioRef = useRef(pauseGlobalAudio)
  pauseGlobalAudioRef.current = pauseGlobalAudio
  const canManageLiveTv = isAdmin || Boolean(user?.Policy?.EnableLiveTvManagement)
  const { t } = useI18n()
  const tRef = useRef(t)
  tRef.current = t
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
  const [schedulingSeriesProgramId, setSchedulingSeriesProgramId] = useState<string | null>(null)
  const [recordNewOnlyPref, setRecordNewOnlyPref] = useState(true)
  const [newSeriesName, setNewSeriesName] = useState('')
  const [newSeriesChannelId, setNewSeriesChannelId] = useState('')
  const [newSeriesRecordNewOnly, setNewSeriesRecordNewOnly] = useState(true)
  const [selectedMappingProviderId, setSelectedMappingProviderId] = useState('')
  const [draftMappings, setDraftMappings] = useState<Record<string, string>>({})
  const [savingMappingChannelId, setSavingMappingChannelId] = useState<string | null>(null)
  const [playingRecording, setPlayingRecording] = useState<LiveTvRecordingInfo | null>(null)
  const [recordingFallbackDirect, setRecordingFallbackDirect] = useState(false)
  const [recordingPlaybackError, setRecordingPlaybackError] = useState<string | null>(null)
  const videoRef = useRef<HTMLVideoElement | null>(null)
  const recordingVideoRef = useRef<HTMLVideoElement | null>(null)
  const hlsRef = useRef<Hls | null>(null)
  const recordingHlsRef = useRef<Hls | null>(null)
  const mediaRecoveryCountRef = useRef(0)
  const networkRecoveryCountRef = useRef(0)
  const volumePrefsRef = useRef<{ volume: number; muted: boolean } | null>(null)
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
  const channelIdsForEpg = useMemo(
    () => channels.map((ch) => ch.Id).filter((id): id is string => Boolean(id)),
    [channels],
  )

  const { data: programsData } = useQuery({
    queryKey: ['liveTv', 'programs', channelIdsForEpg.length],
    queryFn: () => {
      const baseNow = Date.now()
      const minEndDate = new Date(baseNow - 5 * 60_000).toISOString()
      const maxStartDate = new Date(baseNow + 24 * 3_600_000).toISOString()
      const limit = Math.max(2000, channelIdsForEpg.length * 16)
      return api.getLiveTvPrograms(
        channelIdsForEpg.length > 0 ? channelIdsForEpg : undefined,
        limit,
        { minEndDate, maxStartDate },
      )
    },
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
    () => timers.map((tm) => tm.ProgramId).filter((id): id is string => Boolean(id)),
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

  const {
    data: seriesTimersData,
    isLoading: seriesTimersLoading,
    isError: seriesTimersError,
    error: seriesTimersErr,
    refetch: refetchSeriesTimers,
  } = useQuery({
    queryKey: ['liveTv', 'seriesTimers'],
    queryFn: () => api.getSeriesTimers(),
    enabled: activeSubTab === 'seriesTimers' || (canManageLiveTv && channels.length > 0),
  })

  const seriesTimers = useMemo(() => seriesTimersData?.Items || [], [seriesTimersData])

  const scheduledSeriesNames = useMemo(
    () =>
      new Set(
        seriesTimers
          .map((st) => (st.Name || '').trim().toLowerCase())
          .filter((name) => name.length > 0),
      ),
    [seriesTimers],
  )

  const { data: listingProvidersData } = useQuery({
    queryKey: ['liveTv', 'listingProviders'],
    queryFn: () => api.getListingProviders(),
    enabled: activeSubTab === 'mappings' && canManageLiveTv,
  })

  const listingProviders = useMemo(() => listingProvidersData || [], [listingProvidersData])

  useEffect(() => {
    if (
      activeSubTab === 'mappings' &&
      !selectedMappingProviderId &&
      listingProviders.length > 0 &&
      listingProviders[0].Id
    ) {
      setSelectedMappingProviderId(listingProviders[0].Id)
    }
  }, [activeSubTab, selectedMappingProviderId, listingProviders])

  const {
    data: channelMappingsData,
    isLoading: mappingsLoading,
    isError: mappingsError,
    error: mappingsErr,
    refetch: refetchMappings,
  } = useQuery({
    queryKey: ['liveTv', 'channelMappings', selectedMappingProviderId],
    queryFn: () => api.getChannelMappings(selectedMappingProviderId),
    enabled: activeSubTab === 'mappings' && canManageLiveTv,
  })

  const createSeriesTimerMutation = useMutation({
    mutationFn: (payload: LiveTvSeriesTimerInfo) => api.createSeriesTimer(payload),
    onMutate: (payload) => {
      if (payload.ProgramId) {
        setSchedulingSeriesProgramId(payload.ProgramId)
      }
      setDvrFeedback(null)
    },
    onSuccess: async (_created, payload) => {
      setNewSeriesName('')
      setDvrFeedback({
        type: 'success',
        message: payload.RecordNewOnly
          ? t('liveTvUi.seriesTimerCreatedNewOnly', { name: payload.Name || '' })
          : t('liveTvUi.seriesTimerCreated', { name: payload.Name || '' }),
      })
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['liveTv', 'seriesTimers'] }),
        queryClient.invalidateQueries({ queryKey: ['liveTv', 'timers'] }),
      ])
    },
    onError: (err) => {
      setDvrFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('liveTvUi.seriesTimerCreateFailed'),
      })
    },
    onSettled: () => {
      setSchedulingSeriesProgramId(null)
    },
  })

  const deleteSeriesTimerMutation = useMutation({
    mutationFn: (seriesTimerId: string) => api.deleteSeriesTimer(seriesTimerId),
    onSuccess: async () => {
      setDvrFeedback({ type: 'success', message: t('liveTvUi.seriesTimerCancelled') })
      await queryClient.invalidateQueries({ queryKey: ['liveTv', 'seriesTimers'] })
    },
    onError: (err) => {
      setDvrFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('liveTvUi.seriesTimerCancelFailed'),
      })
    },
  })

  const setChannelMappingMutation = useMutation({
    mutationFn: (payload: {
      providerId: string
      tunerChannelId: string
      providerChannelId: string
    }) =>
      api.setChannelMapping({
        ProviderId: payload.providerId || undefined,
        TunerChannelId: payload.tunerChannelId,
        ProviderChannelId: payload.providerChannelId,
      }),
    onMutate: (payload) => {
      setSavingMappingChannelId(payload.tunerChannelId)
      setDvrFeedback(null)
    },
    onSuccess: async (_res, payload) => {
      setDvrFeedback({
        type: 'success',
        message: payload.providerChannelId
          ? t('liveTvUi.epgMappingSaved')
          : t('liveTvUi.epgMappingCleared'),
      })
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['liveTv', 'channelMappings'] }),
        queryClient.invalidateQueries({ queryKey: ['liveTv', 'programs'] }),
      ])
      setDraftMappings((prev) => {
        const next = { ...prev }
        delete next[payload.tunerChannelId]
        return next
      })
    },
    onError: (err) => {
      setDvrFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('liveTvUi.epgMappingSaveFailed'),
      })
    },
    onSettled: () => {
      setSavingMappingChannelId(null)
    },
  })

  const createTimerMutation = useMutation({
    mutationFn: ({
      program: prog,
      channelId,
      channelName,
    }: {
      program: LiveTvProgramItem
      channelId: string
      channelName?: string
    }) => {
      const startIso = prog.StartDate || new Date(nowMs).toISOString()
      const endIso = prog.EndDate || new Date(Date.parse(startIso) + 3_600_000).toISOString()
      return api.createLiveTvTimer({
        ChannelId: channelId,
        ChannelName: channelName,
        ProgramId: prog.Id,
        Name: prog.Name,
        Overview: prog.Overview,
        StartDate: startIso,
        EndDate: endIso,
      })
    },
    onMutate: ({ program: prog }) => {
      setSchedulingProgramId(prog.Id)
      setDvrFeedback(null)
    },
    onSuccess: async (_created, { program: prog }) => {
      setDvrFeedback({
        type: 'success',
        message: t('liveTvUi.timerScheduledSuccess', { name: prog.Name }),
      })
      await queryClient.invalidateQueries({ queryKey: ['liveTv', 'timers'] })
    },
    onError: (err) => {
      setDvrFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('liveTvUi.timerScheduleFailed'),
      })
    },
    onSettled: () => {
      setSchedulingProgramId(null)
    },
  })

  const deleteTimerMutation = useMutation({
    mutationFn: (timerId: string) => api.deleteLiveTvTimer(timerId),
    onSuccess: async () => {
      setDvrFeedback({ type: 'success', message: t('liveTvUi.timerCancelledSuccess') })
      await queryClient.invalidateQueries({ queryKey: ['liveTv', 'timers'] })
    },
    onError: (err) => {
      setDvrFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('liveTvUi.timerCancelFailed'),
      })
    },
  })

  const deleteRecordingMutation = useMutation({
    mutationFn: (recordingId: string) => api.deleteLiveTvRecording(recordingId),
    onSuccess: async (_data, recordingId) => {
      setPlayingRecording((prev) => (prev?.Id === recordingId ? null : prev))
      setDvrFeedback({ type: 'success', message: t('liveTvUi.recordingDeletedSuccess') })
      await queryClient.invalidateQueries({ queryKey: ['liveTv', 'recordings'] })
    },
    onError: (err) => {
      setDvrFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('liveTvUi.recordingDeleteFailed'),
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

  const activeChannelId = activeChannel?.Id ?? ''
  const activeStreamUrl = activeChannelId ? getLiveTvChannelStreamUrl(activeChannelId) : ''
  const activeLooksLikeHls = useMemo(() => {
    if (!activeChannel) return false
    if (activeChannel.IsHls === true) return true
    if ((activeChannel.StreamFormat || '').trim().toLowerCase() === 'hls') return true
    if (activeChannel.IsHls === false) return false
    const hintUrl = (activeChannel.StreamUrl || activeStreamUrl).toLowerCase()
    return hintUrl.includes('.m3u8') || hintUrl.includes('hls')
  }, [activeChannel, activeStreamUrl])

  useEffect(() => {
    setForceHls(false)
    mediaRecoveryCountRef.current = 0
    networkRecoveryCountRef.current = 0
  }, [activeChannelId])

  const handleSelectChannel = (channelId: string) => {
    pauseGlobalAudio()
    setPlaybackError(null)
    setForceHls(false)
    mediaRecoveryCountRef.current = 0
    networkRecoveryCountRef.current = 0
    const next = new URLSearchParams(searchParams)
    next.set('channel', channelId)
    setSearchParams(next, { replace: true })
  }

  const handleRetryStream = () => {
    pauseGlobalAudio()
    setPlaybackError(null)
    mediaRecoveryCountRef.current = 0
    networkRecoveryCountRef.current = 0
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
    if (!video || !activeChannelId) return

    if (volumePrefsRef.current) {
      video.volume = volumePrefsRef.current.volume
      video.muted = volumePrefsRef.current.muted
    }

    pauseGlobalAudioRef.current()
    setPlaybackError(null)
    mediaRecoveryCountRef.current = 0
    networkRecoveryCountRef.current = 0

    if (demoActive) {
      const cachedUrl = getCachedDemoServer()?.getDemoVideoUrl()
      if (cachedUrl) {
        video.src = cachedUrl
        video.load()
        return () => {
          video.pause()
        }
      }
      let cancelled = false
      void loadDemoServer().then((srv) => {
        if (cancelled) return
        video.src = srv.getDemoVideoUrl()
        video.load()
      })
      return () => {
        cancelled = true
        video.pause()
      }
    }

    const streamUrl = activeStreamUrl
    const useHls = (activeLooksLikeHls || forceHls) && Hls.isSupported()

    let cancelled = false
    let hls: Hls | null = null
    let recoveryTimer: number | null = null
    let stableResetTimer: number | null = null
    let lastFatalErrorAt = 0

    const clearRecoveryTimer = () => {
      if (recoveryTimer !== null) {
        window.clearTimeout(recoveryTimer)
        recoveryTimer = null
      }
    }

    const clearStableResetTimer = () => {
      if (stableResetTimer !== null) {
        window.clearTimeout(stableResetTimer)
        stableResetTimer = null
      }
    }

    const destroyHls = () => {
      clearRecoveryTimer()
      clearStableResetTimer()
      if (hls) {
        hls.destroy()
        if (hlsRef.current === hls) {
          hlsRef.current = null
        }
        hls = null
      }
    }

    const handleStablePlaybackSignal = () => {
      if (cancelled || !hls || hlsRef.current !== hls) return
      if (mediaRecoveryCountRef.current === 0 && networkRecoveryCountRef.current === 0) return

      const elapsed = lastFatalErrorAt > 0 ? Date.now() - lastFatalErrorAt : 0
      if (lastFatalErrorAt > 0 && elapsed >= HLS_RECOVERY_RESET_MS) {
        mediaRecoveryCountRef.current = 0
        networkRecoveryCountRef.current = 0
        lastFatalErrorAt = 0
        clearStableResetTimer()
        return
      }

      if (stableResetTimer === null) {
        const remaining =
          lastFatalErrorAt > 0
            ? Math.max(0, HLS_RECOVERY_RESET_MS - elapsed)
            : HLS_RECOVERY_RESET_MS
        stableResetTimer = window.setTimeout(() => {
          stableResetTimer = null
          if (cancelled || !hls || hlsRef.current !== hls) return
          mediaRecoveryCountRef.current = 0
          networkRecoveryCountRef.current = 0
          lastFatalErrorAt = 0
        }, remaining)
      }
    }

    const onPlaying = () => {
      handleStablePlaybackSignal()
    }

    if (useHls) {
      hls = new Hls({
        enableWorker: true,
        lowLatencyMode: false,
        liveSyncDurationCount: 3,
        liveMaxLatencyDurationCount: 10,
        maxBufferLength: 30,
        backBufferLength: 30,
        manifestLoadingMaxRetry: 4,
        levelLoadingMaxRetry: 4,
        fragLoadingMaxRetry: 6,
        xhrSetup: attachEmbyHlsAuth,
      })
      hlsRef.current = hls
      video.addEventListener('playing', onPlaying)
      hls.loadSource(streamUrl)
      hls.attachMedia(video)
      hls.on(Hls.Events.FRAG_BUFFERED, () => {
        if (cancelled || hlsRef.current !== hls) return
        handleStablePlaybackSignal()
      })
      hls.on(Hls.Events.ERROR, (_event, data) => {
        if (cancelled || hlsRef.current !== hls) return
        if (!data.fatal) return

        if (data.details && UNRECOVERABLE_CODEC_DETAILS.has(data.details)) {
          destroyHls()
          setPlaybackError(
            formatHlsError(tRef.current('liveTvUi.errHlsUnsupportedCodec'), data.details),
          )
          return
        }

        if (data.type === Hls.ErrorTypes.MEDIA_ERROR) {
          lastFatalErrorAt = Date.now()
          clearStableResetTimer()
          if (mediaRecoveryCountRef.current < MAX_HLS_MEDIA_RECOVERIES) {
            mediaRecoveryCountRef.current += 1
            const attempt = mediaRecoveryCountRef.current
            const delay = Math.min(500 * attempt, 3000)
            clearRecoveryTimer()
            recoveryTimer = window.setTimeout(() => {
              recoveryTimer = null
              if (cancelled || !hls || hlsRef.current !== hls) return
              if (attempt % 2 === 0) {
                hls.swapAudioCodec()
              }
              hls.recoverMediaError()
            }, delay)
          } else {
            destroyHls()
            setPlaybackError(
              formatHlsError(tRef.current('liveTvUi.errHlsMediaDecode'), data.details),
            )
          }
        } else if (data.type === Hls.ErrorTypes.NETWORK_ERROR) {
          lastFatalErrorAt = Date.now()
          clearStableResetTimer()
          if (networkRecoveryCountRef.current < MAX_HLS_NETWORK_RECOVERIES) {
            networkRecoveryCountRef.current += 1
            const attempt = networkRecoveryCountRef.current
            const delay = Math.min(500 * attempt, 3000)
            clearRecoveryTimer()
            recoveryTimer = window.setTimeout(() => {
              recoveryTimer = null
              if (cancelled || !hls || hlsRef.current !== hls) return
              hls.startLoad()
            }, delay)
          } else {
            destroyHls()
            setPlaybackError(
              formatHlsError(tRef.current('liveTvUi.errHlsNetwork'), data.details),
            )
          }
        } else {
          destroyHls()
          setPlaybackError(formatHlsError(tRef.current('liveTvUi.errHlsFatal'), data.details))
        }
      })
    } else {
      video.src = streamUrl
      video.load()
    }

    return () => {
      cancelled = true
      clearRecoveryTimer()
      clearStableResetTimer()
      video.removeEventListener('playing', onPlaying)
      destroyHls()
      video.pause()
      video.removeAttribute('src')
      video.load()
    }
  }, [
    activeChannelId,
    activeLooksLikeHls,
    activeStreamUrl,
    activeSubTab,
    demoActive,
    forceHls,
    streamRetryTick,
  ])

  const shouldUseRecordingHls = Boolean(
    activeSubTab === 'recordings' &&
      playingRecording &&
      !demoActive &&
      !recordingFallbackDirect &&
      Hls.isSupported(),
  )

  // DVR 录像在线回看优先接入 HLS 流 (/emby/LiveTv/LiveRecordings/{id}/hls/master.m3u8)，以支持 .ts/.mkv 录像浏览器直接播放；失败时自动降级直出流
  useEffect(() => {
    const video = recordingVideoRef.current
    if (!shouldUseRecordingHls || !video || !playingRecording) return

    let hls: Hls | null = new Hls({
      enableWorker: true,
      lowLatencyMode: false,
      xhrSetup: attachEmbyHlsAuth,
    })
    recordingHlsRef.current = hls
    const hlsUrl = getLiveTvRecordingHlsUrl(playingRecording.Id)
    hls.loadSource(hlsUrl)
    hls.attachMedia(video)
    hls.on(Hls.Events.ERROR, (_evt, data) => {
      if (!data.fatal) return
      if (hls) {
        hls.destroy()
        if (recordingHlsRef.current === hls) {
          recordingHlsRef.current = null
        }
        hls = null
      }
      setRecordingFallbackDirect(true)
    })

    return () => {
      if (hls) {
        hls.destroy()
        if (recordingHlsRef.current === hls) {
          recordingHlsRef.current = null
        }
        hls = null
      }
    }
  }, [shouldUseRecordingHls, playingRecording])

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
        title={t('liveTvUi.loadChannelsError')}
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
            {t('liveTvUi.headerSubtitle', { count: channels.length })}
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-3">
          {/* 子标签切换：直播频道 / 预约计划 / 系列追录 / 已录节目 / 频道映射 */}
          <div
            role="tablist"
            aria-label={t('liveTvUi.subTabsAria')}
            className="inline-flex flex-wrap items-center gap-1 rounded-xl border border-white/10 bg-zinc-950/80 p-1"
          >
            {(
              [
                {
                  key: 'channels' as LiveTvSubTab,
                  label: t('liveTvUi.tabChannels'),
                  icon: Tv,
                },
                {
                  key: 'timers' as LiveTvSubTab,
                  label: t('liveTvUi.tabTimers'),
                  icon: CalendarClock,
                },
                {
                  key: 'seriesTimers' as LiveTvSubTab,
                  label: t('liveTvUi.tabSeriesTimers'),
                  icon: Repeat,
                },
                {
                  key: 'recordings' as LiveTvSubTab,
                  label: t('liveTvUi.tabRecordings'),
                  icon: Film,
                },
                ...(canManageLiveTv
                  ? [
                      {
                        key: 'mappings' as LiveTvSubTab,
                        label: t('liveTvUi.tabMappings'),
                        icon: Link2,
                      },
                    ]
                  : []),
              ]
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
              <span>{t('liveTvUi.manageSourcesBtn')}</span>
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
            {t('common.close')}
          </button>
        </div>
      )}

      {activeSubTab === 'channels' && channels.length === 0 && (
        <div className="mx-auto max-w-2xl py-10 text-center">
          <div className="rounded-2xl border border-white/10 bg-white/[0.02] p-10">
            <div className="mx-auto mb-4 flex h-14 w-14 items-center justify-center rounded-2xl bg-amber-500/15 text-amber-400 ring-1 ring-amber-500/30">
              <Radio className="h-7 w-7" />
            </div>
            <h2 className="text-lg font-semibold text-white">{t('liveTvUi.noChannelsTitle')}</h2>
            <p className="mt-2 text-xs leading-relaxed text-zinc-400">
              {t('liveTvUi.noChannelsDesc')}
            </p>
            {isAdmin && (
              <Link
                to="/admin/livetv"
                className="mt-6 inline-flex items-center gap-1.5 rounded-xl bg-amber-500 px-5 py-2.5 text-xs font-semibold text-zinc-950 transition-colors hover:bg-amber-400"
              >
                <Settings className="h-3.5 w-3.5" />
                <span>{t('liveTvUi.goConfigureSourcesBtn')}</span>
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
                  key={`${activeChannelId || 'none'}-${forceHls ? 'hls' : 'auto'}-${streamRetryTick}`}
                  ref={videoRef}
                  data-testid="livetv-video"
                  controls
                  playsInline
                  autoPlay
                  onVolumeChange={(e) => {
                    volumePrefsRef.current = {
                      volume: e.currentTarget.volume,
                      muted: e.currentTarget.muted,
                    }
                  }}
                  onError={(e) => {
                    if (demoActive) return
                    if (hlsRef.current) return
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
                    setPlaybackError(t('liveTvUi.errStreamLoad'))
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
                    <span>{t('common.retry')}</span>
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
                        {activeChannel.ChannelType || 'TV'} ·{' '}
                        {demoActive
                          ? t('liveTvUi.demoOfflineStream')
                          : t('liveTvUi.realtimeLiveStream')}
                      </p>
                    </div>
                  </div>
                  {canManageLiveTv && (
                    <label className="inline-flex cursor-pointer items-center gap-1.5 rounded-lg border border-white/10 bg-white/[0.03] px-2.5 py-1 text-[11px] text-zinc-300">
                      <input
                        type="checkbox"
                        checked={recordNewOnlyPref}
                        onChange={(e) => setRecordNewOnlyPref(e.target.checked)}
                        className="h-3.5 w-3.5 accent-amber-500"
                      />
                      <span>{t('liveTvUi.recordNewOnlyPref')}</span>
                    </label>
                  )}
                </div>

                {currentProgram ? (
                  <div className="space-y-2.5">
                    <div className="flex flex-wrap items-center justify-between gap-2">
                      <div className="flex items-center gap-2">
                        <span className="rounded-md bg-amber-500/20 px-2 py-0.5 text-[10px] font-semibold text-amber-300">
                          {t('liveTvUi.nowPlayingBadge')}
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
                          <div className="flex flex-wrap items-center gap-1.5">
                            <button
                              type="button"
                              disabled={
                                schedulingProgramId === currentProgram.Id ||
                                scheduledProgramIds.includes(currentProgram.Id)
                              }
                              onClick={() =>
                                createTimerMutation.mutate({
                                  program: currentProgram,
                                  channelId: activeChannel.Id,
                                  channelName: activeChannel.Name,
                                })
                              }
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
                                  ? t('liveTvUi.scheduledBadge')
                                  : t('liveTvUi.scheduleRecordBtn')}
                              </span>
                            </button>

                            <button
                              type="button"
                              disabled={
                                schedulingSeriesProgramId === currentProgram.Id ||
                                scheduledSeriesNames.has(currentProgram.Name.trim().toLowerCase())
                              }
                              onClick={() =>
                                createSeriesTimerMutation.mutate({
                                  Name: currentProgram.Name,
                                  ChannelId: activeChannel.Id,
                                  ProgramId: currentProgram.Id,
                                  Overview: currentProgram.Overview,
                                  RecordAnyTime: true,
                                  RecordAnyChannel: false,
                                  RecordNewOnly: recordNewOnlyPref,
                                })
                              }
                              className={`inline-flex items-center gap-1 rounded-lg border px-2.5 py-1 text-[11px] font-medium transition-colors disabled:opacity-60 ${
                                scheduledSeriesNames.has(currentProgram.Name.trim().toLowerCase())
                                  ? 'border-emerald-500/35 bg-emerald-500/15 text-emerald-300'
                                  : 'border-sky-500/35 bg-sky-500/10 text-sky-300 hover:bg-sky-500/20'
                              }`}
                            >
                              {schedulingSeriesProgramId === currentProgram.Id ? (
                                <Loader2 className="h-3 w-3 animate-spin" />
                              ) : (
                                <Repeat className="h-3 w-3" />
                              )}
                              <span>
                                {scheduledSeriesNames.has(currentProgram.Name.trim().toLowerCase())
                                  ? t('liveTvUi.seriesScheduledBadge')
                                  : t('liveTvUi.tabSeriesTimers')}
                              </span>
                            </button>
                          </div>
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
                    <span>{t('liveTvUi.noEpgForChannel')}</span>
                  </div>
                )}

                {upcomingPrograms.length > 0 && (
                  <div className="space-y-2 border-t border-white/8 pt-3">
                    <h3 className="flex items-center gap-1.5 text-xs font-semibold text-zinc-300">
                      <CalendarClock className="h-3.5 w-3.5 text-amber-400" />
                      <span>{t('liveTvUi.upNextTitle')}</span>
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
                                <div className="flex items-center gap-1.5">
                                  <button
                                    type="button"
                                    disabled={isScheduling || isScheduled}
                                    onClick={() =>
                                      createTimerMutation.mutate({
                                        program: prog,
                                        channelId: activeChannel.Id,
                                        channelName: activeChannel.Name,
                                      })
                                    }
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
                                    <span>
                                      {isScheduled
                                        ? t('liveTvUi.scheduledBadge')
                                        : t('liveTvUi.scheduleRecordBtn')}
                                    </span>
                                  </button>

                                  <button
                                    type="button"
                                    disabled={
                                      schedulingSeriesProgramId === prog.Id ||
                                      scheduledSeriesNames.has(prog.Name.trim().toLowerCase())
                                    }
                                    onClick={() =>
                                      createSeriesTimerMutation.mutate({
                                        Name: prog.Name,
                                        ChannelId: activeChannel.Id,
                                        ProgramId: prog.Id,
                                        Overview: prog.Overview,
                                        RecordAnyTime: true,
                                        RecordAnyChannel: false,
                                        RecordNewOnly: recordNewOnlyPref,
                                      })
                                    }
                                    className={`inline-flex items-center gap-1 rounded-lg border px-2 py-1 text-[11px] font-medium transition-colors disabled:opacity-60 ${
                                      scheduledSeriesNames.has(prog.Name.trim().toLowerCase())
                                        ? 'border-emerald-500/35 bg-emerald-500/15 text-emerald-300'
                                        : 'border-sky-500/35 bg-sky-500/10 text-sky-300 hover:bg-sky-500/20'
                                    }`}
                                  >
                                    {schedulingSeriesProgramId === prog.Id ? (
                                      <Loader2 className="h-3 w-3 animate-spin" />
                                    ) : (
                                      <Repeat className="h-3 w-3" />
                                    )}
                                    <span>
                                      {scheduledSeriesNames.has(prog.Name.trim().toLowerCase())
                                        ? t('liveTvUi.seriesScheduledBadge')
                                        : t('liveTvUi.followRecordShortBtn')}
                                    </span>
                                  </button>
                                </div>
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
                aria-label={t('liveTvUi.searchChannelsAria')}
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                placeholder={t('liveTvUi.searchChannelsPlaceholder')}
                className="w-full rounded-xl border border-white/10 bg-zinc-900/90 py-2 pr-3 pl-8 text-xs text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
              />
            </div>

            <div
              data-testid="livetv-channel-list"
              className="max-h-[560px] flex-1 space-y-1.5 overflow-y-auto pr-1"
            >
              {filteredChannels.length === 0 ? (
                <div className="py-10 text-center text-xs text-zinc-500">
                  {t('liveTvUi.noMatchingChannels', { query: searchQuery })}
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
                          {chCurrent
                            ? t('liveTvUi.channelNowPlaying', { name: chCurrent.Name })
                            : ch.ChannelType || 'TV'}
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
              <span>{t('liveTvUi.dvrTimersTitle')}</span>
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
              <span>{t('common.refresh')}</span>
            </button>
          </div>

          {timersLoading ? (
            <div className="flex h-36 items-center justify-center">
              <Loader2 className="h-6 w-6 animate-spin text-amber-400" />
            </div>
          ) : timersError ? (
            <QueryErrorState
              title={t('liveTvUi.loadTimersError')}
              error={timersErr}
              onRetry={() => void refetchTimers()}
            />
          ) : timers.length === 0 ? (
            <div className="py-12 text-center text-xs text-zinc-500">
              {t('liveTvUi.emptyTimersHint')}
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
                      <span>{t('liveTvUi.cancelTimerBtn')}</span>
                    </button>
                  )}
                </div>
              ))}
            </div>
          )}
        </section>
      )}

      {activeSubTab === 'seriesTimers' && (
        <section
          data-testid="livetv-series-timers-panel"
          className="space-y-4 rounded-2xl border border-white/8 bg-white/[0.02] p-5"
        >
          <div className="flex flex-wrap items-center justify-between gap-3 border-b border-white/8 pb-3">
            <h2 className="flex items-center gap-2 text-sm font-semibold text-zinc-100">
              <Repeat className="h-4 w-4 text-amber-400" />
              <span>{t('liveTvUi.seriesTimersTitle')}</span>
              <span className="rounded-md bg-white/8 px-2 py-0.5 font-mono text-[11px] text-zinc-400">
                {seriesTimers.length}
              </span>
            </h2>
            <button
              type="button"
              onClick={() => void refetchSeriesTimers()}
              className="inline-flex items-center gap-1 rounded-lg border border-white/10 bg-white/[0.04] px-2.5 py-1 text-xs text-zinc-300 hover:border-amber-400/40 hover:text-amber-300"
            >
              <RefreshCw className="h-3 w-3" />
              <span>{t('common.refresh')}</span>
            </button>
          </div>

          {canManageLiveTv && (
            <form
              onSubmit={(e) => {
                e.preventDefault()
                const trimmed = newSeriesName.trim()
                if (!trimmed) return
                createSeriesTimerMutation.mutate({
                  Name: trimmed,
                  ChannelId: newSeriesChannelId || undefined,
                  RecordAnyTime: true,
                  RecordAnyChannel: !newSeriesChannelId,
                  RecordNewOnly: newSeriesRecordNewOnly,
                })
              }}
              className="flex flex-wrap items-end gap-3 rounded-xl border border-white/8 bg-black/25 p-4"
            >
              <label className="min-w-[200px] flex-1 space-y-1 text-xs">
                <span className="block text-[11px] text-zinc-400">
                  {t('liveTvUi.seriesMatchNameLabel')}
                </span>
                <input
                  type="text"
                  aria-label={t('liveTvUi.seriesMatchNameAria')}
                  value={newSeriesName}
                  onChange={(e) => setNewSeriesName(e.target.value)}
                  placeholder={t('liveTvUi.seriesMatchNamePlaceholder')}
                  className="w-full rounded-xl border border-white/10 bg-zinc-900 px-3 py-2 text-xs text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/50"
                />
              </label>

              <label className="min-w-[160px] space-y-1 text-xs">
                <span className="block text-[11px] text-zinc-400">
                  {t('liveTvUi.limitChannelLabel')}
                </span>
                <select
                  aria-label={t('liveTvUi.limitChannelAria')}
                  value={newSeriesChannelId}
                  onChange={(e) => setNewSeriesChannelId(e.target.value)}
                  className="w-full rounded-xl border border-white/10 bg-zinc-900 px-3 py-2 text-xs text-zinc-100 outline-none focus:border-amber-400/50"
                >
                  <option value="">{t('liveTvUi.anyChannel')}</option>
                  {channels.map((ch) => (
                    <option key={ch.Id} value={ch.Id}>
                      {ch.Name}
                    </option>
                  ))}
                </select>
              </label>

              <label className="inline-flex cursor-pointer items-center gap-2 rounded-xl border border-white/10 bg-zinc-900 px-3 py-2 text-xs text-zinc-200">
                <input
                  type="checkbox"
                  aria-label={t('liveTvUi.recordNewOnlyAria')}
                  checked={newSeriesRecordNewOnly}
                  onChange={(e) => setNewSeriesRecordNewOnly(e.target.checked)}
                  className="h-3.5 w-3.5 accent-amber-500"
                />
                <span>{t('liveTvUi.recordNewOnlyCheckbox')}</span>
              </label>

              <button
                type="submit"
                disabled={!newSeriesName.trim() || createSeriesTimerMutation.isPending}
                className="inline-flex items-center gap-1.5 rounded-xl bg-amber-500 px-4 py-2 text-xs font-semibold text-zinc-950 transition-colors hover:bg-amber-400 disabled:opacity-50"
              >
                {createSeriesTimerMutation.isPending ? (
                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                ) : (
                  <Plus className="h-3.5 w-3.5" />
                )}
                <span>{t('liveTvUi.addSeriesTimerBtn')}</span>
              </button>
            </form>
          )}

          {seriesTimersLoading ? (
            <div className="flex h-36 items-center justify-center">
              <Loader2 className="h-6 w-6 animate-spin text-amber-400" />
            </div>
          ) : seriesTimersError ? (
            <QueryErrorState
              title={t('liveTvUi.loadSeriesTimersError')}
              error={seriesTimersErr}
              onRetry={() => void refetchSeriesTimers()}
            />
          ) : seriesTimers.length === 0 ? (
            <div className="py-12 text-center text-xs text-zinc-500">
              {t('liveTvUi.emptySeriesTimersHint')}
            </div>
          ) : (
            <div className="divide-y divide-white/6 rounded-xl border border-white/8 bg-black/20">
              {seriesTimers.map((st, idx) => {
                const channelLabel = st.ChannelId
                  ? channels.find((c) => c.Id === st.ChannelId)?.Name || st.ChannelId
                  : t('liveTvUi.anyChannel')
                return (
                  <div
                    key={st.Id || `${st.Name}-${idx}`}
                    className="flex flex-wrap items-center justify-between gap-3 px-4 py-3 text-xs"
                  >
                    <div className="min-w-0 flex-1 space-y-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="font-semibold text-zinc-100">{st.Name}</span>
                        <span className="rounded bg-amber-500/15 px-1.5 py-0.5 text-[10px] font-medium text-amber-300">
                          {channelLabel}
                        </span>
                        <span
                          className={`rounded px-1.5 py-0.5 text-[10px] font-medium ${
                            st.RecordNewOnly
                              ? 'bg-emerald-500/15 text-emerald-300'
                              : 'bg-white/10 text-zinc-300'
                          }`}
                        >
                          {st.RecordNewOnly
                            ? t('liveTvUi.newEpisodesOnlyBadge')
                            : t('liveTvUi.includeRerunsBadge')}
                        </span>
                      </div>
                      {st.Overview && (
                        <p className="truncate text-[11px] text-zinc-500">{st.Overview}</p>
                      )}
                    </div>
                    {canManageLiveTv && st.Id && (
                      <button
                        type="button"
                        disabled={
                          deleteSeriesTimerMutation.isPending &&
                          deleteSeriesTimerMutation.variables === st.Id
                        }
                        onClick={() => deleteSeriesTimerMutation.mutate(st.Id!)}
                        className="inline-flex shrink-0 items-center gap-1 rounded-lg border border-red-500/30 bg-red-500/10 px-3 py-1.5 text-xs font-medium text-red-300 transition-colors hover:bg-red-500/20 disabled:opacity-50"
                      >
                        {deleteSeriesTimerMutation.isPending &&
                        deleteSeriesTimerMutation.variables === st.Id ? (
                          <Loader2 className="h-3.5 w-3.5 animate-spin" />
                        ) : (
                          <Trash2 className="h-3.5 w-3.5" />
                        )}
                        <span>{t('liveTvUi.cancelSeriesTimerBtn')}</span>
                      </button>
                    )}
                  </div>
                )
              })}
            </div>
          )}
        </section>
      )}

      {activeSubTab === 'mappings' && canManageLiveTv && (
        <section
          data-testid="livetv-mappings-panel"
          className="space-y-4 rounded-2xl border border-white/8 bg-white/[0.02] p-5"
        >
          <div className="flex flex-wrap items-center justify-between gap-3 border-b border-white/8 pb-3">
            <div className="flex items-center gap-2">
              <Link2 className="h-4 w-4 text-amber-400" />
              <h2 className="text-sm font-semibold text-zinc-100">
                {t('liveTvUi.epgMappingTitle')}
              </h2>
            </div>

            <div className="flex flex-wrap items-center gap-2">
              {listingProviders.length > 0 && (
                <select
                  aria-label={t('liveTvUi.selectListingProviderAria')}
                  value={selectedMappingProviderId}
                  onChange={(e) => setSelectedMappingProviderId(e.target.value)}
                  className="rounded-xl border border-white/10 bg-zinc-900 px-3 py-1.5 text-xs text-zinc-100 outline-none focus:border-amber-400/50"
                >
                  {listingProviders.map((prov, idx) => (
                    <option key={prov.Id || idx} value={prov.Id || ''}>
                      {prov.Type || 'XMLTV'} · {prov.Path}
                    </option>
                  ))}
                </select>
              )}
              <button
                type="button"
                onClick={() => void refetchMappings()}
                className="inline-flex items-center gap-1 rounded-lg border border-white/10 bg-white/[0.04] px-2.5 py-1 text-xs text-zinc-300 hover:border-amber-400/40 hover:text-amber-300"
              >
                <RefreshCw className="h-3 w-3" />
                <span>{t('common.refresh')}</span>
              </button>
            </div>
          </div>

          {mappingsLoading ? (
            <div className="flex h-36 items-center justify-center">
              <Loader2 className="h-6 w-6 animate-spin text-amber-400" />
            </div>
          ) : mappingsError ? (
            <QueryErrorState
              title={t('liveTvUi.loadMappingsError')}
              error={mappingsErr}
              onRetry={() => void refetchMappings()}
            />
          ) : (
            (() => {
              const tunerList =
                channelMappingsData?.TunerChannels && channelMappingsData.TunerChannels.length > 0
                  ? channelMappingsData.TunerChannels
                  : channels.map((c) => ({ Id: c.Id, Name: c.Name, Number: c.Number }))
              const providerChannelOptions = channelMappingsData?.ProviderChannels || []
              const existingMap = new Map(
                (channelMappingsData?.Mappings || []).map((m) => [
                  m.TunerChannelId,
                  m.ProviderChannelId,
                ]),
              )

              if (tunerList.length === 0) {
                return (
                  <div className="py-12 text-center text-xs text-zinc-500">
                    {t('liveTvUi.emptyTunerChannelsForMapping')}
                  </div>
                )
              }

              return (
                <div className="divide-y divide-white/6 rounded-xl border border-white/8 bg-black/20">
                  {tunerList.map((tunerCh) => {
                    const initialMapped =
                      existingMap.get(tunerCh.Id) ??
                      ('ProviderChannelId' in tunerCh && tunerCh.ProviderChannelId
                        ? tunerCh.ProviderChannelId
                        : '')
                    const hasDraft = Object.prototype.hasOwnProperty.call(draftMappings, tunerCh.Id)
                    const currentMapped = hasDraft ? draftMappings[tunerCh.Id] : initialMapped
                    const isMappingDirty = currentMapped !== initialMapped
                    const isSaving = savingMappingChannelId === tunerCh.Id
                    return (
                      <div
                        key={tunerCh.Id}
                        className="flex flex-wrap items-center justify-between gap-3 px-4 py-3 text-xs"
                      >
                        <div className="min-w-[180px] flex-1">
                          <span className="font-semibold text-zinc-100">{tunerCh.Name}</span>
                          {tunerCh.Number && (
                            <span className="ml-2 font-mono text-[11px] text-amber-300">
                              #{tunerCh.Number}
                            </span>
                          )}
                        </div>

                        <div className="flex flex-wrap items-center gap-2">
                          {providerChannelOptions.length > 0 ? (
                            <select
                              aria-label={t('liveTvUi.channelEpgMappingAria', {
                                name: tunerCh.Name,
                              })}
                              value={currentMapped}
                              onChange={(e) =>
                                setDraftMappings((prev) => ({
                                  ...prev,
                                  [tunerCh.Id]: e.target.value,
                                }))
                              }
                              className="min-w-[200px] rounded-xl border border-white/10 bg-zinc-900 px-3 py-1.5 text-xs text-zinc-100 outline-none focus:border-amber-400/50"
                            >
                              <option value="">{t('liveTvUi.unassignedEpgOption')}</option>
                              {providerChannelOptions.map((opt) => (
                                <option key={opt.Id} value={opt.Id}>
                                  {opt.Name} ({opt.Id})
                                </option>
                              ))}
                            </select>
                          ) : (
                            <input
                              type="text"
                              aria-label={t('liveTvUi.channelEpgMappingAria', {
                                name: tunerCh.Name,
                              })}
                              value={currentMapped}
                              onChange={(e) =>
                                setDraftMappings((prev) => ({
                                  ...prev,
                                  [tunerCh.Id]: e.target.value,
                                }))
                              }
                              placeholder={t('liveTvUi.xmltvChannelIdPlaceholder')}
                              className="min-w-[200px] rounded-xl border border-white/10 bg-zinc-900 px-3 py-1.5 text-xs text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/50"
                            />
                          )}

                          <button
                            type="button"
                            disabled={!isMappingDirty || isSaving}
                            onClick={() =>
                              setChannelMappingMutation.mutate({
                                providerId: selectedMappingProviderId,
                                tunerChannelId: tunerCh.Id,
                                providerChannelId: currentMapped,
                              })
                            }
                            className="inline-flex items-center gap-1 rounded-xl bg-amber-500 px-3 py-1.5 text-xs font-semibold text-zinc-950 transition-colors hover:bg-amber-400 disabled:opacity-50"
                          >
                            {isSaving ? (
                              <Loader2 className="h-3.5 w-3.5 animate-spin" />
                            ) : (
                              <Save className="h-3.5 w-3.5" />
                            )}
                            <span>{t('liveTvUi.saveMappingBtn')}</span>
                          </button>
                        </div>
                      </div>
                    )
                  })}
                </div>
              )
            })()
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
                  aria-label={t('liveTvUi.closeRecordingPlayerAria')}
                  onClick={() => {
                    setPlayingRecording(null)
                    setRecordingPlaybackError(null)
                  }}
                  className="inline-flex shrink-0 items-center gap-1 rounded-lg border border-white/10 bg-white/[0.04] px-2.5 py-1 text-[11px] font-medium text-zinc-300 transition-colors hover:bg-white/10 hover:text-white"
                >
                  <X className="h-3.5 w-3.5" />
                  <span>{t('common.close')}</span>
                </button>
              </div>

              <div className="aspect-video w-full bg-zinc-950">
                <video
                  ref={recordingVideoRef}
                  key={`${playingRecording.Id}-${recordingFallbackDirect ? 'direct' : 'hls'}`}
                  data-testid="dvr-recording-video"
                  controls
                  playsInline
                  autoPlay
                  src={
                    demoActive
                      ? getCachedDemoServer()?.getDemoVideoUrl() || ''
                      : shouldUseRecordingHls
                        ? undefined
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
                      setRecordingPlaybackError(t('liveTvUi.errRecordingUnsupportedFormat'))
                    } else {
                      setRecordingPlaybackError(t('liveTvUi.errRecordingLoadFailed'))
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
                    <span>{t('liveTvUi.downloadRecordingBtn')}</span>
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
              title={t('liveTvUi.loadRecordingsError')}
              error={recordingsErr}
              onRetry={() => void refetchRecordings()}
            />
          ) : recordings.length === 0 ? (
            <div className="py-12 text-center text-xs text-zinc-500">
              {t('liveTvUi.emptyRecordings')}
            </div>
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
                          pauseGlobalAudio()
                          setRecordingPlaybackError(null)
                          setRecordingFallbackDirect(false)
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
                        <span>{t('liveTvUi.deleteRecordingBtn')}</span>
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
