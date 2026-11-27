import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import Hls from 'hls.js'
import {
  Activity,
  AlertTriangle,
  ArrowLeft,
  Bookmark,
  Cpu,
  Gauge,
  HelpCircle,
  Layers,
  Loader2,
  Maximize,
  Maximize2,
  Minimize,
  Pause,
  PictureInPicture2,
  Play,
  RotateCcw,
  RotateCw,
  Search,
  Settings,
  SkipBack,
  SkipForward,
  SlidersHorizontal,
  Sparkles,
  Subtitles,
  Volume2,
  VolumeX,
  X,
} from 'lucide-react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import {
  api,
  buildEmbyAuthHeader,
  getBifUrl,
  getStoredAuth,
  getUniversalAudioTranscodeUrl,
  isSameOriginUrl,
  resolveApiUrl,
} from '../api/client'
import type { MediaSourceInfo, MediaStream } from '../api/types'
import { AmbraLogo } from '../components/AmbraLogo'
import { RemoteSubtitleModal } from '../components/media/RemoteSubtitleModal'
import { useAudioPlayer } from '../context/AudioPlayerContext'
import { useAuth } from '../context/AuthContext'
import { useWebSocket } from '../context/WebSocketContext'
import { loadDemoServer } from '../demo/demoLoader'
import { isDemoMode } from '../demo/demoMode'
import { useI18n } from '../i18n/I18nContext'
import { AssJassubRenderer } from '../player/assRenderer'
import { parseBif, type BifArchive } from '../player/bifParser'
import { PgsCanvasRenderer } from '../player/pgsRenderer'
import {
  loadPlaybackPrefs,
  pickPreferredAudioIndex,
  pickPreferredSubtitleIndex,
} from '../player/playbackPrefs'
import {
  DEFAULT_SUBTITLE_PREFS,
  formatSubtitleOffset,
  loadSubtitlePrefs,
  resetSubtitlePrefs,
  saveSubtitlePrefs,
  stepSubtitleOffset,
  SUBTITLE_PREFS_LIMITS,
  type SubtitlePrefs,
} from '../player/subtitlePrefs'
import {
  detectSubtitleEngine,
  formatSeconds,
  getAudioLabel,
  getSubtitleMeta,
  parseVttOrSrt,
  type TextSubtitleCue,
} from '../player/subtitleUtils'
import {
  canStreamMkvRemux,
  canStreamMkvVideoOnly,
  isMkvContainer,
  StreamMkvFallbackError,
  StreamMkvRemuxer,
} from '../player/streamMkvRemuxer'
import {
  canUseWasmRemux,
  DEFAULT_WASM_MAX_BYTES,
  formatBytes,
  getPreferWasmRemux,
  hasSharedArrayBufferSupport,
  needsClientRemux,
  setPreferWasmRemux,
  WasmRemuxer,
  WasmRemuxFallbackError,
  type WasmRemuxStage,
} from '../player/wasmRemuxer'

type ActivePanel =
  | 'none'
  | 'version'
  | 'subtitle'
  | 'subtitlePrefs'
  | 'audio'
  | 'quality'
  | 'chapters'
  | 'speed'
  | 'settings'

type VideoAspectMode = 'contain' | 'cover'

const PLAYER_UI_PREFS_KEY = 'ambra_player_ui_prefs'

interface PlayerUiPrefs {
  volume: number
  muted: boolean
  playbackRate: number
  aspectMode: VideoAspectMode
}

function loadPlayerUiPrefs(): PlayerUiPrefs {
  const defaults: PlayerUiPrefs = {
    volume: 1,
    muted: false,
    playbackRate: 1,
    aspectMode: 'contain',
  }
  try {
    const raw = localStorage.getItem(PLAYER_UI_PREFS_KEY)
    if (!raw) return defaults
    const parsed = JSON.parse(raw) as Partial<PlayerUiPrefs>
    const volume =
      typeof parsed.volume === 'number' && Number.isFinite(parsed.volume)
        ? Math.min(1, Math.max(0, parsed.volume))
        : defaults.volume
    const muted = typeof parsed.muted === 'boolean' ? parsed.muted : defaults.muted
    const playbackRate =
      typeof parsed.playbackRate === 'number' &&
      Number.isFinite(parsed.playbackRate) &&
      parsed.playbackRate >= 0.25 &&
      parsed.playbackRate <= 4
        ? parsed.playbackRate
        : defaults.playbackRate
    const aspectMode: VideoAspectMode = parsed.aspectMode === 'cover' ? 'cover' : 'contain'
    return { volume, muted, playbackRate, aspectMode }
  } catch {
    return defaults
  }
}

function savePlayerUiPrefs(prefs: PlayerUiPrefs): void {
  try {
    localStorage.setItem(PLAYER_UI_PREFS_KEY, JSON.stringify(prefs))
  } catch {
    // ignore storage quota errors
  }
}
type PlayMethodMode =
  | 'DirectPlay'
  | 'Proxy Direct'
  | 'Stream MKV (MSE)'
  | 'Client WASM Remux'
  | 'Server HLS'

type QualityPresetId = 'auto' | '1080p8' | '720p4' | '480p1.5'

interface QualityPreset {
  id: QualityPresetId
  label: string
  shortLabel: string
  labelKey?: string
  shortLabelKey?: string
  maxBitrate?: number
  maxWidth?: number
  maxHeight?: number
}

const QUALITY_PRESETS: QualityPreset[] = [
  {
    id: 'auto',
    label: 'Auto',
    shortLabel: 'Auto',
    labelKey: 'player.qualityAuto',
    shortLabelKey: 'playerUi.qualityAutoShort',
  },
  {
    id: '1080p8',
    label: '1080p · 8 Mbps',
    shortLabel: '1080p',
    maxBitrate: 8_000_000,
    maxWidth: 1920,
    maxHeight: 1080,
  },
  {
    id: '720p4',
    label: '720p · 4 Mbps',
    shortLabel: '720p',
    maxBitrate: 4_000_000,
    maxWidth: 1280,
    maxHeight: 720,
  },
  {
    id: '480p1.5',
    label: '480p · 1.5 Mbps',
    shortLabel: '480p',
    maxBitrate: 1_500_000,
    maxWidth: 854,
    maxHeight: 480,
  },
]

const KEYBOARD_SHORTCUTS: Array<{ keys: string; descKey: string }> = [
  { keys: 'Space', descKey: 'playerUi.scPlayPause' },
  { keys: '← / →', descKey: 'playerUi.scSeek10' },
  { keys: '↑ / ↓', descKey: 'playerUi.scVolume10' },
  { keys: 'M', descKey: 'playerUi.scMute' },
  { keys: 'F', descKey: 'playerUi.scFullscreen' },
  { keys: 'P', descKey: 'playerUi.scPip' },
  { keys: 'C', descKey: 'playerUi.scCycleSubtitle' },
  { keys: 'A', descKey: 'playerUi.scCycleAudio' },
  { keys: 'I', descKey: 'player.stats' },
  { keys: '?', descKey: 'playerUi.scToggleShortcuts' },
  { keys: 'Esc', descKey: 'playerUi.scEsc' },
]

function isStrmSource(source?: MediaSourceInfo): boolean {
  if (!source) return false
  if (source.IsRemote) return true
  if ((source.Container || '').trim().toLowerCase() === 'strm') return true
  if ((source.Protocol || '').trim().toLowerCase() === 'http') return true
  if ((source.Path || '').trim().toLowerCase().endsWith('.strm')) return true
  return false
}

function parseNonNegativeIntParam(raw: string | null): number | undefined {
  if (raw === null || raw.trim() === '') return undefined
  const n = Number(raw)
  return Number.isInteger(n) && n >= 0 ? n : undefined
}

const INTRO_CHAPTER_RE = /^(intro|opening|op|片头|片頭|オープニング)/i

interface WasmProgressState {
  stage: WasmRemuxStage
  percent: number
  detail: string
}

export function PlayerPage() {
  const { id } = useParams<{ id: string }>()
  const [searchParams] = useSearchParams()
  const navigate = useNavigate()
  const { auth } = useAuth()
  const { locale, t } = useI18n()
  const tRef = useRef(t)
  tRef.current = t
  const { subscribePlaystate } = useWebSocket()
  const audioPlayer = useAudioPlayer()
  const userId = auth!.userId

  // 进入视频播放页时自动暂停全局音乐播放器，避免音视频同时出声
  const audioPlayerRef = useRef(audioPlayer)
  audioPlayerRef.current = audioPlayer
  useEffect(() => {
    if (audioPlayerRef.current.isPlaying) {
      audioPlayerRef.current.pause()
    }
  }, [])

  const initialMediaSourceId = searchParams.get('mediaSourceId') || undefined
  const initialAudio = parseNonNegativeIntParam(searchParams.get('audio'))
  const playlistId = searchParams.get('playlistId') || undefined
  const rawInitialSubtitle = searchParams.get('subtitle')
  const initialSubtitle =
    rawInitialSubtitle && rawInitialSubtitle !== 'off'
      ? parseNonNegativeIntParam(rawInitialSubtitle)
      : undefined
  const rawStartTicks = searchParams.get('startTicks')
  const parsedStartTicks =
    rawStartTicks !== null && rawStartTicks.trim() !== '' ? Number(rawStartTicks) : null
  const explicitStartTicks =
    parsedStartTicks !== null && Number.isFinite(parsedStartTicks) && parsedStartTicks >= 0
      ? parsedStartTicks
      : null
  const initialStartTicks = explicitStartTicks ?? 0

  const containerRef = useRef<HTMLDivElement>(null)
  const videoRef = useRef<HTMLVideoElement>(null)
  const hybridAudioRef = useRef<HTMLAudioElement>(null)
  const hybridAudioBaseSecRef = useRef(0)
  const assCanvasRef = useRef<HTMLCanvasElement>(null)
  const pgsCanvasRef = useRef<HTMLCanvasElement>(null)

  const hlsRef = useRef<Hls | null>(null)
  const streamMkvRef = useRef<StreamMkvRemuxer | null>(null)
  const wasmRemuxerRef = useRef<WasmRemuxer | null>(null)
  const assRendererRef = useRef<AssJassubRenderer | null>(null)
  const pgsRendererRef = useRef<PgsCanvasRenderer | null>(null)
  const bifArchiveRef = useRef<BifArchive | null>(null)
  const hideOsdTimerRef = useRef<number | null>(null)
  const initialSeekDoneRef = useRef(false)
  const initializedAudioSourceIdRef = useRef<string | null>(null)
  const initializedSubSourceIdRef = useRef<string | null>(null)
  const subtitleExplicitlyOffRef = useRef<boolean>(rawInitialSubtitle === 'off')
  const stopReportedRef = useRef<boolean>(false)
  const lastVideoTimeSecRef = useRef(0)
  const lastSeekReportRef = useRef(0)
  const resumeSecRef = useRef<number | null>(null)
  const hlsRecoveryCountRef = useRef(0)
  const searchParamsRef = useRef(searchParams)
  searchParamsRef.current = searchParams
  const prevIdRef = useRef<string | undefined>(id)
  const renderedItemIdRef = useRef<string | undefined>(id)
  renderedItemIdRef.current = id

  const [bifArchive, setBifArchive] = useState<BifArchive | null>(null)
  const [hoverSeek, setHoverSeek] = useState<{ timeSec: number; pct: number } | null>(null)

  const [selectedSourceId, setSelectedSourceId] = useState<string | undefined>(initialMediaSourceId)
  const [querySourceId, setQuerySourceId] = useState<string | undefined>(initialMediaSourceId)
  const [audioIndex, setAudioIndex] = useState<number | undefined>(initialAudio)
  const [queryAudioIndex, setQueryAudioIndex] = useState<number | undefined>(initialAudio)
  const [subtitleIndex, setSubtitleIndex] = useState<number | undefined>(initialSubtitle)
  const [qualityPreset, setQualityPreset] = useState<QualityPresetId>('auto')
  const [episodeIdSynced, setEpisodeIdSynced] = useState<string | undefined>(id)
  const [retryNonce, setRetryNonce] = useState(0)

  const initialUiPrefs = useMemo(() => loadPlayerUiPrefs(), [])
  const [playing, setPlaying] = useState(false)
  const [buffering, setBuffering] = useState(true)
  const [currentTime, setCurrentTime] = useState(0)
  const [duration, setDuration] = useState(0)
  const [bufferedEnd, setBufferedEnd] = useState(0)
  const [volume, setVolume] = useState(() => initialUiPrefs.volume)
  const [muted, setMuted] = useState(() => initialUiPrefs.muted)
  const [playbackRate, setPlaybackRate] = useState(() => initialUiPrefs.playbackRate)
  const [aspectMode, setAspectMode] = useState<VideoAspectMode>(() => initialUiPrefs.aspectMode)
  const volumeRef = useRef(volume)
  volumeRef.current = volume
  const mutedRef = useRef(muted)
  mutedRef.current = muted
  const playbackRateRef = useRef(playbackRate)
  playbackRateRef.current = playbackRate

  useEffect(() => {
    savePlayerUiPrefs({ volume, muted, playbackRate, aspectMode })
  }, [volume, muted, playbackRate, aspectMode])

  useEffect(() => {
    const v = videoRef.current
    if (!v) return
    v.volume = initialUiPrefs.volume
    v.muted = initialUiPrefs.muted
    if (Number.isFinite(initialUiPrefs.playbackRate) && initialUiPrefs.playbackRate > 0) {
      v.playbackRate = initialUiPrefs.playbackRate
    }
  }, [initialUiPrefs])
  const [isFullscreen, setIsFullscreen] = useState(false)
  const [isPip, setIsPip] = useState(false)
  const [osdVisible, setOsdVisible] = useState(true)
  const [activePanel, setActivePanel] = useState<ActivePanel>('none')
  const [showStats, setShowStats] = useState(false)
  const [showShortcuts, setShowShortcuts] = useState(false)
  const [playerError, setPlayerError] = useState<string | null>(null)
  const [subtitleError, setSubtitleError] = useState<string | null>(null)
  const [playMethod, setPlayMethod] = useState<PlayMethodMode>('DirectPlay')
  const [nextUpCountdown, setNextUpCountdown] = useState<number | null>(null)

  const captureResumePosition = useCallback(() => {
    const v = videoRef.current
    if (v && Number.isFinite(v.currentTime) && v.currentTime > 0) {
      resumeSecRef.current = v.currentTime
    }
  }, [])

  // 客户端 WASM 解复用偏好与运行态
  const [preferWasmRemux, setPreferWasmRemuxState] = useState<boolean>(() => getPreferWasmRemux())
  const [useProxyDirect, setUseProxyDirect] = useState(false)
  const [forceServerHls, setForceServerHls] = useState(false)
  const [wasmProgress, setWasmProgress] = useState<WasmProgressState | null>(null)
  const [wasmFallbackNotice, setWasmFallbackNotice] = useState<string | null>(null)
  const wasmSupported = useMemo(() => canUseWasmRemux(), [])
  const sabSupported = useMemo(() => hasSharedArrayBufferSupport(), [])

  const togglePreferWasmRemux = useCallback(() => {
    captureResumePosition()
    setPreferWasmRemuxState((prev) => {
      const next = !prev
      setPreferWasmRemux(next)
      setUseProxyDirect(false)
      setForceServerHls(false)
      setWasmFallbackNotice(null)
      return next
    })
  }, [captureResumePosition])

  // 切换视频条目（上一集 / 下一集 / 自动连播）时彻底重置源、轨道、进度、错误与回退状态
  useEffect(() => {
    initialSeekDoneRef.current = false
    initializedAudioSourceIdRef.current = null
    initializedSubSourceIdRef.current = null
    stopReportedRef.current = false
    lastVideoTimeSecRef.current = 0
    resumeSecRef.current = null
    hlsRecoveryCountRef.current = 0
    setNextUpCountdown(null)
    setHoverSeek(null)

    if (prevIdRef.current !== id) {
      prevIdRef.current = id
      const sp = searchParamsRef.current
      const nextSourceId = sp.get('mediaSourceId') || undefined
      const nextAudio = parseNonNegativeIntParam(sp.get('audio'))
      const nextSubRaw = sp.get('subtitle')
      const nextSubtitle =
        nextSubRaw && nextSubRaw !== 'off' ? parseNonNegativeIntParam(nextSubRaw) : undefined
      subtitleExplicitlyOffRef.current = nextSubRaw === 'off'
      setSelectedSourceId(nextSourceId)
      setQuerySourceId(nextSourceId)
      setAudioIndex(nextAudio)
      setQueryAudioIndex(nextAudio)
      setSubtitleIndex(nextSubtitle)
      setQualityPreset('auto')
      setShowShortcuts(false)
      setPlayerError(null)
      setSubtitleError(null)
      setCurrentTime(0)
      setDuration(0)
      setBufferedEnd(0)
      setTextCues([])
      setUseProxyDirect(false)
      setForceServerHls(false)
      setWasmFallbackNotice(null)
      setBuffering(true)
      setEpisodeIdSynced(id)
    }
  }, [id])

  // WebVTT / SRT 文本字幕状态 & 字幕微调偏好（按 userId 作用域隔离）
  const [textCues, setTextCues] = useState<TextSubtitleCue[]>([])
  const [subtitlePrefs, setSubtitlePrefs] = useState<SubtitlePrefs>(() => loadSubtitlePrefs(userId))
  const subtitlePrefsRef = useRef<SubtitlePrefs>(subtitlePrefs)
  subtitlePrefsRef.current = subtitlePrefs
  const [loadedFontCount, setLoadedFontCount] = useState(0)
  const [remoteSubModalOpen, setRemoteSubModalOpen] = useState(false)

  useEffect(() => {
    setSubtitlePrefs(loadSubtitlePrefs(userId))
  }, [userId])

  const updateSubtitlePrefs = useCallback(
    (patch: Partial<SubtitlePrefs>) => {
      setSubtitlePrefs((prev) => saveSubtitlePrefs({ ...prev, ...patch }, userId))
    },
    [userId],
  )

  const handleResetSubtitlePrefs = useCallback(() => {
    setSubtitlePrefs(resetSubtitlePrefs(userId))
  }, [userId])

  const { data: item } = useQuery({
    queryKey: ['item', userId, id],
    queryFn: () => api.getItem(userId, id!),
    enabled: Boolean(id),
  })

  // 剧集分集列表：用于上一集 / 下一集快捷切换与播放完毕自动连播倒计时
  const { data: seasonEpisodesData } = useQuery({
    queryKey: ['episodes', item?.SeriesId, item?.SeasonId || '', userId],
    queryFn: () => api.getEpisodes(item!.SeriesId!, item!.SeasonId || '', userId),
    enabled: Boolean(item?.Type === 'Episode' && item.SeriesId),
    staleTime: 60_000,
  })

  const seasonEpisodes = seasonEpisodesData?.Items || []
  const currentEpisodeIdx = useMemo(
    () => seasonEpisodes.findIndex((ep) => ep.Id === id),
    [seasonEpisodes, id],
  )
  const prevEpisode = currentEpisodeIdx > 0 ? seasonEpisodes[currentEpisodeIdx - 1] : null
  const nextInSeasonEpisode =
    currentEpisodeIdx >= 0 && currentEpisodeIdx + 1 < seasonEpisodes.length
      ? seasonEpisodes[currentEpisodeIdx + 1]
      : null

  // 当前季最后一集时，通过 NextUp 查询跨季下一集
  const isLastInSeason =
    Boolean(item?.Type === 'Episode' && item.SeriesId) &&
    currentEpisodeIdx >= 0 &&
    currentEpisodeIdx === seasonEpisodes.length - 1

  const { data: crossSeasonNextUpData } = useQuery({
    queryKey: ['nextUp', userId, item?.SeriesId, id],
    queryFn: () => api.getNextUp(userId, { seriesId: item!.SeriesId!, limit: 2 }),
    enabled: isLastInSeason,
    staleTime: 60_000,
  })

  const crossSeasonNextEpisode = useMemo(() => {
    if (!isLastInSeason || !crossSeasonNextUpData?.Items?.length) return null
    return crossSeasonNextUpData.Items.find((ep) => ep.Id !== item?.Id) || null
  }, [isLastInSeason, crossSeasonNextUpData, item?.Id])

  const nextEpisode = nextInSeasonEpisode || crossSeasonNextEpisode

  // 播放列表队列：当携带 ?playlistId=... 时支持上一项 / 下一项与 8 秒自动连播
  const { data: playlistItemsData } = useQuery({
    queryKey: ['playlistItems', playlistId, userId],
    queryFn: () => api.getPlaylistItems(playlistId!, userId),
    enabled: Boolean(playlistId),
    staleTime: 60_000,
  })

  const playlistVideoItems = useMemo(
    () => (playlistItemsData?.Items || []).filter((it) => it.Type !== 'Audio'),
    [playlistItemsData],
  )
  const currentPlaylistIdx = useMemo(
    () => (playlistId ? playlistVideoItems.findIndex((it) => it.Id === id) : -1),
    [playlistId, playlistVideoItems, id],
  )
  const prevPlaylistItem =
    currentPlaylistIdx > 0 ? playlistVideoItems[currentPlaylistIdx - 1] : null
  const nextPlaylistItem =
    currentPlaylistIdx >= 0 && currentPlaylistIdx + 1 < playlistVideoItems.length
      ? playlistVideoItems[currentPlaylistIdx + 1]
      : null

  const prevQueueItem = playlistId ? prevPlaylistItem : prevEpisode
  const nextQueueItem = playlistId ? nextPlaylistItem : nextEpisode

  const buildQueuePlayUrl = useCallback(
    (targetId: string) =>
      playlistId
        ? `/play/${encodeURIComponent(targetId)}?playlistId=${encodeURIComponent(playlistId)}`
        : `/play/${encodeURIComponent(targetId)}`,
    [playlistId],
  )

  useEffect(() => {
    if (nextUpCountdown === null || !nextQueueItem) return
    if (nextUpCountdown <= 0) {
      setNextUpCountdown(null)
      navigate(buildQueuePlayUrl(nextQueueItem.Id), { replace: true })
      return
    }
    const timer = window.setTimeout(() => {
      setNextUpCountdown((c) => (c === null ? null : c - 1))
    }, 1000)
    return () => window.clearTimeout(timer)
  }, [nextUpCountdown, nextQueueItem, navigate, buildQueuePlayUrl])

  const {
    data: playbackInfo,
    isLoading: infoLoading,
    isPlaceholderData: infoIsPlaceholder,
    error: infoError,
    refetch: refetchPlaybackInfo,
  } = useQuery({
    queryKey: ['playbackInfo', id, userId, querySourceId, queryAudioIndex],
    queryFn: () =>
      api.getPlaybackInfo(id!, userId, {
        mediaSourceId: querySourceId,
        audioStreamIndex: queryAudioIndex,
        subtitleStreamIndex: subtitleIndex,
        startTimeTicks: initialStartTicks,
      }),
    enabled: Boolean(id),
    staleTime: 0,
    placeholderData: (prev, prevQuery) => (prevQuery?.queryKey[1] === id ? prev : undefined),
  })

  const activeSource: MediaSourceInfo | undefined = useMemo(() => {
    if (!playbackInfo?.MediaSources?.length) return undefined
    return (
      playbackInfo.MediaSources.find((s) => s.Id === selectedSourceId) ||
      playbackInfo.MediaSources[0]
    )
  }, [playbackInfo, selectedSourceId])

  const streams = useMemo(() => activeSource?.MediaStreams || [], [activeSource?.MediaStreams])
  const videoStream = useMemo(() => streams.find((s) => s.Type === 'Video'), [streams])
  const audioStreams = useMemo(() => streams.filter((s) => s.Type === 'Audio'), [streams])
  const subtitleStreams = useMemo(() => streams.filter((s) => s.Type === 'Subtitle'), [streams])

  // 同步计算生效音轨（避免首帧 audioIndex=undefined 与初始化回填触发两次流挂载）
  const effectiveAudioIndex = useMemo(() => {
    if (audioIndex !== undefined) return audioIndex
    if (!activeSource || episodeIdSynced !== id) return undefined
    const prefs = loadPlaybackPrefs(userId)
    return pickPreferredAudioIndex(
      audioStreams,
      prefs.preferredAudioLang,
      activeSource.DefaultAudioStreamIndex,
      locale,
    )
  }, [audioIndex, activeSource, episodeIdSynced, id, audioStreams, locale, userId])

  // 初始化默认音轨与字幕轨（优先用户偏好语言，其次服务端默认；每个媒体源仅初始化一次，避免用户关闭字幕后回弹）
  useEffect(() => {
    if (!activeSource || episodeIdSynced !== id) return
    if (selectedSourceId === undefined) setSelectedSourceId(activeSource.Id)
    const prefs = loadPlaybackPrefs(userId)
    let resolvedAudioIdx = audioIndex
    if (initializedAudioSourceIdRef.current !== activeSource.Id) {
      initializedAudioSourceIdRef.current = activeSource.Id
      if (audioIndex === undefined) {
        const pickedAudio = pickPreferredAudioIndex(
          audioStreams,
          prefs.preferredAudioLang,
          activeSource.DefaultAudioStreamIndex,
          locale,
        )
        resolvedAudioIdx = pickedAudio
        if (pickedAudio !== undefined) {
          setAudioIndex(pickedAudio)
        }
      }
    }
    if (initializedSubSourceIdRef.current !== activeSource.Id) {
      initializedSubSourceIdRef.current = activeSource.Id
      if (searchParamsRef.current.get('subtitle') === 'off') {
        subtitleExplicitlyOffRef.current = true
        setSubtitleIndex(undefined)
      } else if (subtitleIndex === undefined) {
        const resolvedAudioStream =
          audioStreams.find((a) => a.Index === resolvedAudioIdx) || audioStreams[0]
        const pickedSub = pickPreferredSubtitleIndex(
          subtitleStreams,
          prefs.preferredSubtitleLang,
          activeSource.DefaultSubtitleStreamIndex,
          locale,
          prefs.subtitleMode,
          resolvedAudioStream,
        )
        if (pickedSub !== undefined) {
          subtitleExplicitlyOffRef.current = false
          setSubtitleIndex(pickedSub)
        } else {
          subtitleExplicitlyOffRef.current =
            prefs.preferredSubtitleLang === 'off' ||
            prefs.subtitleMode === 'None' ||
            prefs.subtitleMode === 'Smart' ||
            prefs.subtitleMode === 'OnlyForced'
        }
      }
    }
  }, [
    activeSource,
    episodeIdSynced,
    id,
    selectedSourceId,
    audioIndex,
    subtitleIndex,
    audioStreams,
    subtitleStreams,
    locale,
    userId,
  ])

  // 计算生效的起播位置（显式传 startTicks >= 0 时直接使用；未传参时从 item.UserData.PlaybackPositionTicks 自动续播）
  const effectiveStartTicks = useMemo(() => {
    if (explicitStartTicks !== null) {
      return explicitStartTicks
    }
    const savedTicks = item?.UserData?.PlaybackPositionTicks ?? 0
    if (!Number.isFinite(savedTicks) || savedTicks <= 5 * 10_000_000) {
      return 0
    }
    if (item?.UserData?.Played) {
      return 0
    }
    const runtimeTicks = item?.RunTimeTicks ?? activeSource?.RunTimeTicks ?? 0
    if (runtimeTicks > 0 && savedTicks >= runtimeTicks * 0.95) {
      return 0
    }
    return savedTicks
  }, [
    explicitStartTicks,
    item?.UserData?.PlaybackPositionTicks,
    item?.UserData?.Played,
    item?.RunTimeTicks,
    activeSource?.RunTimeTicks,
  ])

  const effectiveStartTicksRef = useRef(effectiveStartTicks)
  effectiveStartTicksRef.current = effectiveStartTicks

  // 若元数据先于 item 到达，item 加载完成后补做一次初始续播定位
  useEffect(() => {
    if (initialSeekDoneRef.current || effectiveStartTicks <= 0) return
    const video = videoRef.current
    if (!video || video.readyState < 1) return
    const startSec = effectiveStartTicks / 10_000_000
    if (Number.isFinite(startSec) && startSec > 0) {
      video.currentTime = startSec
      initialSeekDoneRef.current = true
    }
  }, [effectiveStartTicks])

  const activeSubtitleStream: MediaStream | undefined = useMemo(
    () => subtitleStreams.find((s) => s.Index === subtitleIndex),
    [subtitleStreams, subtitleIndex],
  )

  const activeSubtitleMeta = useMemo(
    () => getSubtitleMeta(activeSubtitleStream),
    [activeSubtitleStream],
  )

  const activeAudioStream = useMemo(
    () => audioStreams.find((a) => a.Index === effectiveAudioIndex) || audioStreams[0],
    [audioStreams, effectiveAudioIndex],
  )

  const selectedAudioOrdinal = useMemo(() => {
    if (effectiveAudioIndex === undefined || audioStreams.length === 0) return undefined
    const idx = audioStreams.findIndex((a) => a.Index === effectiveAudioIndex)
    return idx >= 0 ? idx : undefined
  }, [audioStreams, effectiveAudioIndex])

  const selectedAudioOrdinalRef = useRef(selectedAudioOrdinal)
  selectedAudioOrdinalRef.current = selectedAudioOrdinal

  const browserSupportsHls = useMemo(() => {
    if (Hls.isSupported()) return true
    if (typeof document !== 'undefined') {
      const probe = document.createElement('video')
      if (
        typeof probe.canPlayType === 'function' &&
        probe.canPlayType('application/vnd.apple.mpegurl')
      ) {
        return true
      }
    }
    return false
  }, [])

  const defaultAudioStreamIndex =
    activeSource?.DefaultAudioStreamIndex ?? audioStreams[0]?.Index
  const isNonDefaultEmbeddedAudio =
    audioStreams.length > 1 &&
    effectiveAudioIndex !== undefined &&
    defaultAudioStreamIndex !== undefined &&
    effectiveAudioIndex !== defaultAudioStreamIndex &&
    !activeAudioStream?.IsExternal
  // 无论是 URL ?audio=N、用户语言偏好还是手动切换，只要选中非默认内封音轨即触发音轨选择/切流
  const needsTrackSwitchRemux = isNonDefaultEmbeddedAudio

  // 播放策略规划：
  // 1) 原生兼容容器 (mp4/m4v/webm/mov) 且音轨原生兼容 -> DirectPlay
  // 2) 非原生 MKV 且浏览器 MSE 支持对应音视频编码 -> Stream MKV (MSE)（Range 分块流式解复用，支持 GB 级大文件）
  // 3) 其他非原生容器 (ts/avi/flv 等) 或 AC3/EAC3/DTS 音轨：
  //    - 若开启客户端解复用 + 浏览器支持 WASM + 文件体积 <= 150MB -> Client WASM Remux
  //    - 否则 / 超时 / 超阈值 / 出错 -> 自动无感回退服务端 HLS (master.m3u8)
  // 4) 字幕始终走客户端 JASSUB / PGS Canvas / WebVTT，绝不因字幕触发转码
  const streamPlan = useMemo(() => {
    if (!activeSource || !id || infoIsPlaceholder) return null

    const staticParams = new URLSearchParams({
      Static: 'true',
      MediaSourceId: activeSource.Id,
    })
    if (effectiveAudioIndex !== undefined) {
      staticParams.set('AudioStreamIndex', String(effectiveAudioIndex))
    }
    const staticDirectUrl = resolveApiUrl(
      `/emby/Videos/${encodeURIComponent(id)}/stream?${staticParams.toString()}`,
    )

    const proxyParams = new URLSearchParams(staticParams)
    proxyParams.set('proxy', '1')
    const proxyDirectUrl = resolveApiUrl(
      `/emby/Videos/${encodeURIComponent(id)}/stream?${proxyParams.toString()}`,
    )

    const directUrl =
      activeSource.DirectStreamUrl && !activeSource.DirectStreamUrl.includes('.m3u8')
        ? resolveApiUrl(activeSource.DirectStreamUrl)
        : staticDirectUrl

    const isStrm = isStrmSource(activeSource)

    const activeQuality = QUALITY_PRESETS.find((q) => q.id === qualityPreset) || QUALITY_PRESETS[0]

    const hlsParams = new URLSearchParams({
      MediaSourceId: activeSource.Id,
    })
    if (playbackInfo?.PlaySessionId) hlsParams.set('PlaySessionId', playbackInfo.PlaySessionId)
    if (effectiveAudioIndex !== undefined) {
      hlsParams.set('AudioStreamIndex', String(effectiveAudioIndex))
    }
    if (qualityPreset !== 'auto') {
      if (activeQuality.maxBitrate) {
        hlsParams.set('MaxStreamingBitrate', String(activeQuality.maxBitrate))
      }
      if (activeQuality.maxWidth) hlsParams.set('MaxWidth', String(activeQuality.maxWidth))
      if (activeQuality.maxHeight) hlsParams.set('MaxHeight', String(activeQuality.maxHeight))
    }
    const fallbackMasterHlsUrl = resolveApiUrl(
      `/emby/Videos/${encodeURIComponent(id)}/master.m3u8?${hlsParams.toString()}`,
    )

    const hlsUrl =
      qualityPreset !== 'auto' || needsTrackSwitchRemux
        ? fallbackMasterHlsUrl
        : activeSource.TranscodingUrl
          ? resolveApiUrl(activeSource.TranscodingUrl)
          : activeSource.DirectStreamUrl?.includes('.m3u8')
            ? resolveApiUrl(activeSource.DirectStreamUrl)
            : fallbackMasterHlsUrl

    const looksLikeMkvStrm =
      isStrm &&
      /\.mkv(?:$|[?#\s])/i.test(
        `${activeSource.Path || ''} ${activeSource.DirectStreamUrl || ''} ${activeSource.Name || ''}`,
      )
    const effectiveContainer =
      isMkvContainer(activeSource.Container) || looksLikeMkvStrm
        ? 'mkv'
        : activeSource.Container

    const requiresRemux = needsClientRemux(effectiveContainer, activeAudioStream?.Codec)
    const sizeBytes = activeSource.Size
    const withinWasmThreshold = sizeBytes === undefined || sizeBytes <= DEFAULT_WASM_MAX_BYTES
    const mkvStreamEligible =
      isMkvContainer(effectiveContainer) &&
      canStreamMkvRemux(videoStream?.Codec, activeAudioStream?.Codec, effectiveContainer)
    const mkvVideoOnlyEligible =
      isMkvContainer(effectiveContainer) &&
      canStreamMkvVideoOnly(videoStream?.Codec, effectiveContainer) &&
      !mkvStreamEligible &&
      audioStreams.length > 0

    if (qualityPreset !== 'auto' || forceServerHls) {
      return {
        strategy: 'hls' as const,
        videoOnly: false,
        directUrl,
        proxyDirectUrl,
        hlsUrl,
        isStrm,
        requiresRemux,
        withinWasmThreshold,
        skipReason: null as string | null,
      }
    }

    // 0. STRM 直连失败后，仅在容器与音轨原生兼容时尝试服务端反代直出 (&proxy=1)；对 MKV 或不支持音轨优先走反代解复用
    if (useProxyDirect && isStrm && !requiresRemux && !needsTrackSwitchRemux) {
      return {
        strategy: 'proxy-direct' as const,
        videoOnly: false,
        directUrl,
        proxyDirectUrl,
        hlsUrl,
        isStrm,
        requiresRemux,
        withinWasmThreshold,
        skipReason: null as string | null,
      }
    }

    // 1. 原生兼容容器 + 原生兼容音轨（未选中非默认内封音轨，或当前环境不支持 HLS 时由 DirectPlay 配合 audioTracks 切轨）-> DirectPlay
    if (
      !requiresRemux &&
      (!needsTrackSwitchRemux || (!mkvStreamEligible && !browserSupportsHls)) &&
      activeSource.SupportsDirectPlay !== false
    ) {
      return {
        strategy: 'direct' as const,
        videoOnly: false,
        directUrl,
        proxyDirectUrl,
        hlsUrl,
        isStrm,
        requiresRemux,
        withinWasmThreshold,
        skipReason: null as string | null,
      }
    }

    // 2. 非原生 MKV 或选中非默认内封音轨的 MKV 且浏览器 MSE 支持该音视频编码 -> 优先流式 Range + MSE（STRM 自动经同源反代）
    if ((requiresRemux || needsTrackSwitchRemux) && preferWasmRemux && mkvStreamEligible) {
      return {
        strategy: 'stream-mkv' as const,
        videoOnly: false,
        directUrl,
        proxyDirectUrl,
        hlsUrl,
        isStrm,
        requiresRemux,
        withinWasmThreshold,
        skipReason: null as string | null,
      }
    }

    // 2.5 静态 DirectPlay 容器（如 MP4）选中非默认内封音轨（?audio=N / 偏好语言 / 手动切换）-> 切换服务端 HLS 精准选轨
    if (needsTrackSwitchRemux && !requiresRemux) {
      return {
        strategy: 'hls' as const,
        videoOnly: false,
        directUrl,
        proxyDirectUrl,
        hlsUrl,
        isStrm,
        requiresRemux,
        withinWasmThreshold,
        skipReason: tRef.current('playerUi.skipNonDefaultAudioHls'),
      }
    }

    // 3. 小体积非原生容器 / 需音频转码（<= 150MB）-> 尝试客户端 ffmpeg.wasm
    if (
      requiresRemux &&
      preferWasmRemux &&
      canUseWasmRemux({ enabled: preferWasmRemux }) &&
      withinWasmThreshold
    ) {
      return {
        strategy: 'wasm' as const,
        videoOnly: false,
        directUrl,
        proxyDirectUrl,
        hlsUrl,
        isStrm,
        requiresRemux,
        withinWasmThreshold,
        skipReason: null as string | null,
      }
    }

    // 3.5 大体积 (>150MB) 杜比 AC3/EAC3/DTS/TrueHD MKV 电影零视频重编码：MSE 仅解复用直通视频轨 + 隐藏同步 <audio> 播放服务端轻量 AAC 音轨流
    if ((requiresRemux || needsTrackSwitchRemux) && preferWasmRemux && mkvVideoOnlyEligible) {
      return {
        strategy: 'stream-mkv' as const,
        videoOnly: true,
        directUrl,
        proxyDirectUrl,
        hlsUrl,
        isStrm,
        requiresRemux,
        withinWasmThreshold,
        skipReason: null as string | null,
      }
    }

    // 4. 回退服务端 HLS master.m3u8，并记录回退原因供统计面板展示
    let skipReason: string | null = null
    if (requiresRemux) {
      if (!preferWasmRemux) {
        skipReason = tRef.current('playerUi.skipWasmToggleOff')
      } else if (!canUseWasmRemux()) {
        skipReason = tRef.current('playerUi.skipBrowserNoWasm')
      } else if (!withinWasmThreshold && sizeBytes !== undefined) {
        skipReason = tRef.current('playerUi.skipSizeExceedsWasm', {
          size: formatBytes(sizeBytes),
          max: formatBytes(DEFAULT_WASM_MAX_BYTES),
        })
      }
    }

    return {
      strategy: 'hls' as const,
      videoOnly: false,
      directUrl,
      proxyDirectUrl,
      hlsUrl,
      isStrm,
      requiresRemux,
      withinWasmThreshold,
      skipReason,
    }
  }, [
    activeSource,
    id,
    infoIsPlaceholder,
    effectiveAudioIndex,
    playbackInfo?.PlaySessionId,
    videoStream?.Codec,
    activeAudioStream?.Codec,
    audioStreams.length,
    preferWasmRemux,
    useProxyDirect,
    forceServerHls,
    qualityPreset,
    needsTrackSwitchRemux,
    browserSupportsHls,
  ])

  const isHybridVideoOnly = Boolean(
    streamPlan?.strategy === 'stream-mkv' && streamPlan.videoOnly && !forceServerHls,
  )

  // 挂载视频流 (DirectPlay / Stream MKV (MSE) / Client WASM Remux / Server HLS 自动降级)
  useEffect(() => {
    const video = videoRef.current
    const hybridAudio = hybridAudioRef.current
    if (!video || !streamPlan) return

    let cancelled = false
    let playbackStarted = false
    const abortController = new AbortController()

    setPlayerError(null)
    setBuffering(true)
    setWasmProgress(null)
    if (streamPlan.skipReason) {
      setWasmFallbackNotice(streamPlan.skipReason)
    } else if (!forceServerHls && !useProxyDirect) {
      setWasmFallbackNotice(null)
    }

    if (hlsRef.current) {
      hlsRef.current.destroy()
      hlsRef.current = null
    }
    if (streamMkvRef.current) {
      streamMkvRef.current.dispose()
      streamMkvRef.current = null
    }
    if (wasmRemuxerRef.current) {
      wasmRemuxerRef.current.dispose()
      wasmRemuxerRef.current = null
    }
    if (hybridAudio) {
      hybridAudio.pause()
      hybridAudio.removeAttribute('src')
      hybridAudio.load()
    }
    hybridAudioBaseSecRef.current = 0

    const startPlayback = () => {
      if (cancelled) return
      playbackStarted = true
      if (
        resumeSecRef.current !== null &&
        Number.isFinite(resumeSecRef.current) &&
        resumeSecRef.current > 0
      ) {
        video.currentTime = resumeSecRef.current
        resumeSecRef.current = null
        initialSeekDoneRef.current = true
      } else {
        const targetStartTicks = effectiveStartTicksRef.current
        if (!initialSeekDoneRef.current && targetStartTicks > 0) {
          const startSec = targetStartTicks / 10_000_000
          if (Number.isFinite(startSec) && startSec > 0) {
            video.currentTime = startSec
          }
          initialSeekDoneRef.current = true
        }
      }
      video.volume = volumeRef.current
      video.muted = mutedRef.current
      if (Number.isFinite(playbackRateRef.current) && playbackRateRef.current > 0) {
        video.playbackRate = playbackRateRef.current
      }
      if (hybridAudio && streamPlan.strategy === 'stream-mkv' && streamPlan.videoOnly) {
        hybridAudio.volume = volumeRef.current
        hybridAudio.muted = mutedRef.current
        if (Number.isFinite(playbackRateRef.current) && playbackRateRef.current > 0) {
          hybridAudio.playbackRate = playbackRateRef.current
        }
      }
      // 在支持原生 HTMLMediaElement.audioTracks 的浏览器（如 Safari）下，DirectPlay 同步切换对应音轨
      const nativeAudioTracks = (
        video as HTMLVideoElement & {
          audioTracks?: { length: number; [idx: number]: { enabled: boolean } }
        }
      ).audioTracks
      if (
        nativeAudioTracks &&
        nativeAudioTracks.length > 1 &&
        selectedAudioOrdinalRef.current !== undefined
      ) {
        const targetOrd = selectedAudioOrdinalRef.current
        for (let i = 0; i < nativeAudioTracks.length; i++) {
          nativeAudioTracks[i].enabled = i === targetOrd
        }
      }
      Promise.resolve(video.play()).catch(() => {
        // 浏览器自动播放策略阻止时保持暂停状态让用户点击
      })
      if (hybridAudio && streamPlan.strategy === 'stream-mkv' && streamPlan.videoOnly && hybridAudio.src) {
        Promise.resolve(hybridAudio.play()).catch(() => {
          // 随主视频 onPlay 事件再次触发播放
        })
      }
    }

    const buildAuthHeaders = (targetUrl: string): Record<string, string> => {
      if (!isSameOriginUrl(targetUrl)) {
        return {}
      }
      const storedAuth = getStoredAuth()
      return {
        Authorization: buildEmbyAuthHeader(storedAuth?.accessToken),
      }
    }

    const remuxSourceUrl = streamPlan.isStrm ? streamPlan.proxyDirectUrl : streamPlan.directUrl

    const mountServerHls = (url: string) => {
      if (cancelled) return
      setPlayMethod('Server HLS')
      setWasmProgress(null)
      setBuffering(true)
      hlsRecoveryCountRef.current = 0

      if (hlsRef.current) {
        hlsRef.current.destroy()
        hlsRef.current = null
      }

      if (Hls.isSupported()) {
        const hls = new Hls({
          enableWorker: true,
          lowLatencyMode: false,
          xhrSetup: (xhr, requestUrl) => {
            if (!isSameOriginUrl(requestUrl)) return
            const headers = buildAuthHeaders(requestUrl)
            Object.entries(headers).forEach(([k, v]) => {
              xhr.setRequestHeader(k, v)
            })
          },
        })
        hlsRef.current = hls
        hls.loadSource(url)
        hls.attachMedia(video)
        hls.on(Hls.Events.MANIFEST_PARSED, startPlayback)
        hls.on(Hls.Events.ERROR, (_evt, data) => {
          if (!data.fatal || cancelled) return
          if (hlsRecoveryCountRef.current < 2) {
            if (data.type === Hls.ErrorTypes.NETWORK_ERROR) {
              hlsRecoveryCountRef.current += 1
              hls.startLoad()
              return
            }
            if (data.type === Hls.ErrorTypes.MEDIA_ERROR) {
              hlsRecoveryCountRef.current += 1
              hls.recoverMediaError()
              return
            }
          }
          setBuffering(false)
          setPlayerError(tRef.current('playerUi.errHlsPlayback', { details: data.details }))
        })
      } else if (video.canPlayType('application/vnd.apple.mpegurl')) {
        video.src = url
        video.addEventListener('loadedmetadata', startPlayback, { once: true })
      } else {
        setPlayerError(tRef.current('playerUi.errBrowserNoHls'))
      }
    }

    const mountWasmRemux = () => {
      if (cancelled) return
      setPlayMethod('Client WASM Remux')
      setWasmProgress({
        stage: 'fetch',
        percent: 0,
        detail: tRef.current('playerUi.wasmPreparingDetail'),
      })

      const remuxer = new WasmRemuxer()
      wasmRemuxerRef.current = remuxer

      void remuxer
        .remuxToMP4(remuxSourceUrl, buildAuthHeaders(remuxSourceUrl), {
          maxBytes: DEFAULT_WASM_MAX_BYTES,
          knownSizeBytes: activeSource?.Size,
          container: activeSource?.Container,
          audioStreamIndex: selectedAudioOrdinal,
          signal: abortController.signal,
          onProgress: (stage, percent, detail) => {
            if (cancelled) return
            setWasmProgress({
              stage,
              percent,
              detail:
                detail ||
                (stage === 'fetch'
                  ? tRef.current('playerUi.wasmFetchingPercent', { percent })
                  : stage === 'init'
                    ? tRef.current('playerUi.wasmInitEngine')
                    : stage === 'remux'
                      ? tRef.current('playerUi.wasmRemuxingPercent', { percent })
                      : tRef.current('playerUi.wasmDone')),
            })
          },
        })
        .then((result) => {
          if (cancelled) {
            remuxer.revokeUrl(result.url)
            return
          }
          setWasmProgress(null)
          setPlayMethod('Client WASM Remux')
          video.src = result.url
          video.addEventListener('loadedmetadata', startPlayback, { once: true })
        })
        .catch((err: unknown) => {
          if (cancelled) return
          const reasonMsg =
            err instanceof WasmRemuxFallbackError
              ? err.message
              : err instanceof Error
                ? err.message
                : tRef.current('playerUi.errWasmRemuxFailed')
          // 清理 WASM 内存后无感回退服务端 HLS master.m3u8，并同步锁定 forceServerHls 防止重复重试 WASM
          remuxer.dispose()
          if (wasmRemuxerRef.current === remuxer) {
            wasmRemuxerRef.current = null
          }
          captureResumePosition()
          setWasmFallbackNotice(
            tRef.current('playerUi.fallbackToServerHlsReason', { reason: reasonMsg }),
          )
          setForceServerHls(true)
        })
    }

    const fallbackFromStreamMkv = (err: unknown, duringPlayback = false) => {
      if (cancelled) return
      captureResumePosition()
      if (streamMkvRef.current) {
        streamMkvRef.current.dispose()
        streamMkvRef.current = null
      }
      if (hybridAudio) {
        hybridAudio.pause()
        hybridAudio.removeAttribute('src')
        hybridAudio.load()
      }
      const reasonMsg =
        err instanceof StreamMkvFallbackError
          ? err.message
          : err instanceof Error
            ? err.message
            : typeof err === 'string'
              ? err
              : tRef.current('playerUi.errStreamMkvDemux')
      const isFetchOrRedirectFailure =
        err instanceof StreamMkvFallbackError && err.reason === 'fetch_failed'
      if (
        !duringPlayback &&
        !playbackStarted &&
        !isFetchOrRedirectFailure &&
        !streamPlan.videoOnly &&
        streamPlan.withinWasmThreshold &&
        canUseWasmRemux({ enabled: preferWasmRemux })
      ) {
        setWasmFallbackNotice(
          tRef.current('playerUi.fallbackToWasmRemuxReason', { reason: reasonMsg }),
        )
        mountWasmRemux()
      } else {
        setWasmFallbackNotice(
          tRef.current('playerUi.fallbackToServerHlsReason', { reason: reasonMsg }),
        )
        setForceServerHls(true)
      }
    }

    if (streamPlan.strategy === 'direct') {
      setPlayMethod('DirectPlay')
      video.src = streamPlan.directUrl
      video.addEventListener('loadedmetadata', startPlayback, { once: true })
    } else if (streamPlan.strategy === 'proxy-direct') {
      setPlayMethod('Proxy Direct')
      video.src = streamPlan.proxyDirectUrl
      video.addEventListener('loadedmetadata', startPlayback, { once: true })
    } else if (streamPlan.strategy === 'hls') {
      mountServerHls(streamPlan.hlsUrl)
    } else if (streamPlan.strategy === 'stream-mkv') {
      // 大体积 MKV 流式解复用路径（HTTP Range + 纯 TS EBML/fMP4 + MSE；STRM 自动走 proxyDirectUrl）
      setPlayMethod('Stream MKV (MSE)')
      const streamMkv = new StreamMkvRemuxer()
      streamMkvRef.current = streamMkv

      if (streamPlan.videoOnly && hybridAudio && id) {
        const initSec =
          resumeSecRef.current !== null &&
          Number.isFinite(resumeSecRef.current) &&
          resumeSecRef.current > 0
            ? resumeSecRef.current
            : !initialSeekDoneRef.current && effectiveStartTicksRef.current > 0
              ? effectiveStartTicksRef.current / 10_000_000
              : 0
        hybridAudioBaseSecRef.current = initSec
        hybridAudio.src = getUniversalAudioTranscodeUrl(id, {
          mediaSourceId: activeSource?.Id,
          audioStreamIndex: effectiveAudioIndex,
          startTimeTicks: Math.floor(initSec * 10_000_000),
          isStrm: streamPlan.isStrm,
          userId,
        })
        hybridAudio.load()
        const initRate = playbackRateRef.current
        if (Number.isFinite(initRate) && initRate > 0) {
          hybridAudio.defaultPlaybackRate = initRate
          hybridAudio.playbackRate = initRate
        }
      }

      void streamMkv
        .attach(video, remuxSourceUrl, buildAuthHeaders(remuxSourceUrl), {
          audioTrackIndex: selectedAudioOrdinal,
          videoOnly: streamPlan.videoOnly,
          signal: abortController.signal,
          onError: (err) => {
            fallbackFromStreamMkv(err, true)
          },
        })
        .then(() => {
          if (cancelled) return
          setPlayMethod('Stream MKV (MSE)')
          startPlayback()
        })
        .catch((err: unknown) => {
          if (cancelled) return
          fallbackFromStreamMkv(err, false)
        })
    } else {
      // Client WASM Remux 路径：失败/超阈值/超时时自动无感回退服务端 HLS
      mountWasmRemux()
    }

    return () => {
      cancelled = true
      abortController.abort()
      video.removeEventListener('loadedmetadata', startPlayback)
      if (hlsRef.current) {
        hlsRef.current.destroy()
        hlsRef.current = null
      }
      if (streamMkvRef.current) {
        streamMkvRef.current.dispose()
        streamMkvRef.current = null
      }
      if (wasmRemuxerRef.current) {
        wasmRemuxerRef.current.dispose()
        wasmRemuxerRef.current = null
      }
      if (hybridAudio) {
        hybridAudio.pause()
        hybridAudio.removeAttribute('src')
        hybridAudio.load()
      }
      video.pause()
      video.removeAttribute('src')
      video.load()
    }
  }, [
    streamPlan,
    activeSource?.Id,
    activeSource?.Size,
    activeSource?.Container,
    effectiveAudioIndex,
    id,
    userId,
    initialStartTicks,
    useProxyDirect,
    forceServerHls,
    preferWasmRemux,
    selectedAudioOrdinal,
    retryNonce,
  ])

  // 异步静默拉取 BIF 视频预览图归档（404 或不可用时静默降级，不影响播放；卸载或切片时释放全部 ObjectURL）
  useEffect(() => {
    if (!id || !activeSource?.Id) {
      if (bifArchiveRef.current) {
        bifArchiveRef.current.dispose()
        bifArchiveRef.current = null
      }
      setBifArchive(null)
      return
    }

    let cancelled = false
    const controller = new AbortController()

    const loadBif = async () => {
      try {
        let rawBuf: ArrayBuffer | Uint8Array | null = null
        if (isDemoMode()) {
          const demo = await loadDemoServer()
          const demoRes = await demo.demoFetch<ArrayBuffer | Uint8Array>(
            `/emby/Videos/${encodeURIComponent(id)}/index.bif?Width=320&MediaSourceId=${encodeURIComponent(activeSource.Id)}`,
            { signal: controller.signal },
          )
          if (demoRes instanceof ArrayBuffer || demoRes instanceof Uint8Array) {
            rawBuf = demoRes
          }
        } else {
          const bifUrl = getBifUrl(id, 320, activeSource.Id)
          if (!bifUrl) return
          const storedAuth = getStoredAuth()
          const headers: Record<string, string> = {}
          if (isSameOriginUrl(bifUrl)) {
            headers['Authorization'] = buildEmbyAuthHeader(storedAuth?.accessToken)
          }
          const res = await fetch(bifUrl, {
            method: 'GET',
            headers,
            signal: controller.signal,
          })
          if (!res.ok) return
          if (typeof res.arrayBuffer === 'function') {
            rawBuf = await res.arrayBuffer()
          }
        }
        if (cancelled || !rawBuf) return
        const parsed = parseBif(rawBuf)
        if (cancelled) {
          parsed.dispose()
          return
        }
        if (bifArchiveRef.current) {
          bifArchiveRef.current.dispose()
        }
        bifArchiveRef.current = parsed
        setBifArchive(parsed)
      } catch {
        // 404 / 取消 / 格式无效静默降级，不干扰正常播放
      }
    }

    void loadBif()

    return () => {
      cancelled = true
      controller.abort()
      if (bifArchiveRef.current) {
        bifArchiveRef.current.dispose()
        bifArchiveRef.current = null
      }
      setBifArchive(null)
    }
  }, [id, activeSource?.Id])

  // 构建外挂字幕直出 URL（仅同源附加 api_key，跨域字幕不泄露会话令牌）
  const resolveSubtitleUrl = useCallback(
    (stream: MediaStream): string => {
      if (stream.DeliveryUrl) {
        return isSameOriginUrl(stream.DeliveryUrl)
          ? resolveApiUrl(stream.DeliveryUrl)
          : stream.DeliveryUrl
      }
      const ext =
        detectSubtitleEngine(stream) === 'ass'
          ? 'ass'
          : detectSubtitleEngine(stream) === 'pgs'
            ? 'sup'
            : 'vtt'
      const msId = activeSource?.Id || id || ''
      return resolveApiUrl(
        `/emby/Videos/${encodeURIComponent(id || '')}/${encodeURIComponent(msId)}/Subtitles/${stream.Index}/Stream.${ext}`,
      )
    },
    [activeSource?.Id, id],
  )

  const activeSubtitleStreamRef = useRef<MediaStream | undefined>(activeSubtitleStream)
  activeSubtitleStreamRef.current = activeSubtitleStream
  const activeSubtitleKey = activeSubtitleStream
    ? `${id || ''}|${activeSource?.Id || ''}|${activeSubtitleStream.Index}|${activeSubtitleStream.DeliveryUrl || ''}|${activeSubtitleStream.Codec || ''}`
    : ''

  // 字幕渲染引擎切换与生命周期管理（JASSUB WASM / PGS Canvas / Native VTT）
  useEffect(() => {
    const currentSubStream = activeSubtitleStreamRef.current
    let cancelled = false
    const controller = new AbortController()
    setSubtitleError(null)
    setTextCues([])
    setLoadedFontCount(0)

    const cleanupEngines = async () => {
      if (assRendererRef.current) {
        await assRendererRef.current.destroy()
        assRendererRef.current = null
      }
      if (pgsRendererRef.current) {
        pgsRendererRef.current.destroy()
        pgsRendererRef.current = null
      }
      if (pgsCanvasRef.current) {
        const ctx = pgsCanvasRef.current.getContext('2d')
        ctx?.clearRect(0, 0, pgsCanvasRef.current.width, pgsCanvasRef.current.height)
      }
    }

    const setup = async () => {
      await cleanupEngines()
      if (cancelled || !currentSubStream) return

      const engine = detectSubtitleEngine(currentSubStream)
      const subUrl = resolveSubtitleUrl(currentSubStream)

      try {
        if (engine === 'ass') {
          if (!videoRef.current || !assCanvasRef.current) return
          const attachments =
            id && activeSource?.Id
              ? await api.getMediaAttachments(id, activeSource.Id)
              : []
          if (cancelled || !videoRef.current || !assCanvasRef.current) return
          const validFontAttachments = attachments.filter((att) => {
            const rawUrl = (att.Url || '').trim()
            if (!rawUrl) return false
            const cleanUrlPath = rawUrl.split(/[?#]/)[0]
            const lastSeg = cleanUrlPath.slice(cleanUrlPath.lastIndexOf('/') + 1)
            const urlHasExt = lastSeg.includes('.')
            const validExtRe = /\.(ttf|otf|ttc|woff2?)$/i
            const fileName = (att.FileName || '').trim()
            if (fileName) {
              if (!validExtRe.test(fileName)) return false
              if (urlHasExt && !validExtRe.test(lastSeg)) return false
              return true
            }
            return validExtRe.test(lastSeg)
          })
          const fontUrls = validFontAttachments.map((att) => {
            const rawPath =
              att.Url.startsWith('/emby/') ||
              /^https?:\/\//i.test(att.Url) ||
              att.Url.startsWith('//')
                ? att.Url
                : `/emby${att.Url.startsWith('/') ? '' : '/'}${att.Url}`
            return isSameOriginUrl(rawPath) ? resolveApiUrl(rawPath) : rawPath
          })
          setLoadedFontCount(fontUrls.length)

          const renderer = new AssJassubRenderer()
          assRendererRef.current = renderer
          await renderer.mount(videoRef.current, assCanvasRef.current, subUrl, {
            fonts: fontUrls,
            timeOffset: subtitlePrefsRef.current.offsetSec,
          })
        } else if (engine === 'pgs') {
          if (!pgsCanvasRef.current) return
          const renderer = new PgsCanvasRenderer(pgsCanvasRef.current)
          pgsRendererRef.current = renderer
          await renderer.loadFromUrl(subUrl)
          if (!cancelled && videoRef.current) {
            renderer.setTimeOffset(subtitlePrefsRef.current.offsetSec)
            renderer.renderAtTime(videoRef.current.currentTime)
          }
        } else {
          const res = await fetch(subUrl, { signal: controller.signal })
          if (!res.ok) {
            throw new Error(tRef.current('playerUi.errLoadTextSubtitleStatus', { status: res.status }))
          }
          const raw = await res.text()
          if (!cancelled) {
            setTextCues(parseVttOrSrt(raw))
          }
        }
      } catch (err) {
        if (!cancelled && !controller.signal.aborted) {
          setSubtitleError(
            err instanceof Error ? err.message : tRef.current('playerUi.errSubtitleLoadFailed'),
          )
        }
      }
    }

    setup()

    return () => {
      cancelled = true
      controller.abort()
      void cleanupEngines()
    }
  }, [activeSubtitleKey, resolveSubtitleUrl, id, activeSource?.Id])

  // 字幕时间轴偏移实时同步至 ASS (JASSUB) 与 PGS Canvas 引擎
  useEffect(() => {
    assRendererRef.current?.setTimeOffset(subtitlePrefs.offsetSec)
    pgsRendererRef.current?.setTimeOffset(subtitlePrefs.offsetSec)
  }, [subtitlePrefs.offsetSec])

  // PGS 实时时间同步渲染
  useEffect(() => {
    if (pgsRendererRef.current) {
      pgsRendererRef.current.renderAtTime(currentTime)
    }
  }, [currentTime])

  // 窗口变化时同步 JASSUB 画布尺寸
  useEffect(() => {
    const onResize = () => {
      assRendererRef.current?.resize()
    }
    window.addEventListener('resize', onResize)
    return () => window.removeEventListener('resize', onResize)
  }, [])

  // 将音轨 / 字幕 / 播放模式 / 会话元数据保存在 ref 中，避免切换轨道或回退 HLS 时重建 reportState 触发多余的 Stopped -> Playing 循环
  const playbackMetaRef = useRef({
    audioIndex: effectiveAudioIndex,
    subtitleIndex,
    playMethod,
    mediaSourceId: activeSource?.Id,
    playSessionId: playbackInfo?.PlaySessionId,
    effectiveStartTicks,
  })
  playbackMetaRef.current = {
    audioIndex: effectiveAudioIndex,
    subtitleIndex,
    playMethod,
    mediaSourceId: activeSource?.Id,
    playSessionId: playbackInfo?.PlaySessionId,
    effectiveStartTicks,
  }

  // 上报播放状态 (/emby/Sessions/Playing, Progress, Stopped)
  const reportState = useCallback(
    (
      type: 'start' | 'progress' | 'stop',
      eventName?: string,
      sessionOverride?: {
        itemId: string
        mediaSourceId?: string
        playSessionId?: string
        initialStartTicks: number
        audioIndex?: number
        subtitleIndex?: number
        subtitleExplicitlyOff?: boolean
      },
    ) => {
      const targetItemId = sessionOverride?.itemId ?? id
      const videoEl = videoRef.current
      if (!targetItemId || (!videoEl && type !== 'stop')) return
      if (videoEl && Number.isFinite(videoEl.currentTime) && videoEl.currentTime >= 0) {
        lastVideoTimeSecRef.current = videoEl.currentTime
      }
      const curSec = videoEl ? videoEl.currentTime : lastVideoTimeSecRef.current
      const {
        audioIndex: latestAudioIndex,
        subtitleIndex: latestSubtitleIndex,
        playMethod: latestPlayMethod,
        mediaSourceId: latestMediaSourceId,
        playSessionId: latestPlaySessionId,
        effectiveStartTicks: latestStartTicks,
      } = playbackMetaRef.current

      const resolvedMediaSourceId = sessionOverride
        ? sessionOverride.mediaSourceId
        : latestMediaSourceId
      const resolvedPlaySessionId = sessionOverride
        ? sessionOverride.playSessionId
        : latestPlaySessionId
      const resolvedStartTicks = sessionOverride
        ? sessionOverride.initialStartTicks
        : latestStartTicks
      const resolvedAudioIndex = sessionOverride ? sessionOverride.audioIndex : latestAudioIndex
      const resolvedSubIndex = sessionOverride
        ? sessionOverride.subtitleIndex !== undefined
          ? sessionOverride.subtitleIndex
          : sessionOverride.subtitleExplicitlyOff
            ? -1
            : undefined
        : latestSubtitleIndex !== undefined
          ? latestSubtitleIndex
          : subtitleExplicitlyOffRef.current
            ? -1
            : undefined

      let posTicks = Math.floor(curSec * 10_000_000)
      if (!initialSeekDoneRef.current && resolvedStartTicks > 0 && curSec < 1) {
        posTicks = resolvedStartTicks
      }
      const base = {
        ItemId: targetItemId,
        MediaSourceId: resolvedMediaSourceId,
        PlaySessionId: resolvedPlaySessionId,
        PositionTicks: posTicks,
      }
      if (type === 'stop') {
        if (stopReportedRef.current) return
        stopReportedRef.current = true
        void api.reportStopped({
          ...base,
          AudioStreamIndex: resolvedAudioIndex,
          SubtitleStreamIndex: resolvedSubIndex,
        })
        return
      }
      if (type === 'start') {
        stopReportedRef.current = false
      }
      const full = {
        ...base,
        IsPaused: videoEl ? videoEl.paused : true,
        IsMuted: videoEl ? videoEl.muted : mutedRef.current,
        VolumeLevel: Math.round((videoEl ? videoEl.volume : volumeRef.current) * 100),
        AudioStreamIndex: resolvedAudioIndex,
        SubtitleStreamIndex: resolvedSubIndex,
        PlayMethod:
          latestPlayMethod === 'Server HLS'
            ? 'Transcode'
            : latestPlayMethod === 'Proxy Direct'
              ? 'DirectStream'
              : 'DirectPlay',
      }
      if (type === 'start') {
        void api.reportPlaying(full)
      } else {
        void api.reportProgress({ ...full, EventName: eventName })
      }
    },
    [id],
  )

  const hasPlaybackInfo = Boolean(playbackInfo)

  useEffect(() => {
    if (!hasPlaybackInfo || !id) return
    const sessionSnapshot = {
      itemId: id,
      mediaSourceId: playbackMetaRef.current.mediaSourceId,
      playSessionId: playbackMetaRef.current.playSessionId,
      initialStartTicks: playbackMetaRef.current.effectiveStartTicks,
      audioIndex: playbackMetaRef.current.audioIndex,
      subtitleIndex: playbackMetaRef.current.subtitleIndex,
      subtitleExplicitlyOff: subtitleExplicitlyOffRef.current,
    }
    const syncSameEpisodeTracks = () => {
      if (renderedItemIdRef.current !== sessionSnapshot.itemId) return
      sessionSnapshot.audioIndex = playbackMetaRef.current.audioIndex
      sessionSnapshot.subtitleIndex = playbackMetaRef.current.subtitleIndex
      sessionSnapshot.subtitleExplicitlyOff = subtitleExplicitlyOffRef.current
      if (playbackMetaRef.current.mediaSourceId) {
        sessionSnapshot.mediaSourceId = playbackMetaRef.current.mediaSourceId
      }
      if (playbackMetaRef.current.playSessionId) {
        sessionSnapshot.playSessionId = playbackMetaRef.current.playSessionId
      }
    }

    reportState('start', undefined, sessionSnapshot)
    const interval = window.setInterval(() => {
      syncSameEpisodeTracks()
      reportState('progress', 'TimeUpdate', sessionSnapshot)
    }, 10_000)

    return () => {
      window.clearInterval(interval)
      syncSameEpisodeTracks()
      reportState('stop', undefined, sessionSnapshot)
    }
  }, [hasPlaybackInfo, id, reportState])

  // 页面关闭或刷新时通过 keepalive 上报最新播放进度，避免丢失续播点
  useEffect(() => {
    if (!hasPlaybackInfo || !id || isDemoMode()) return
    const sessionSnapshot = {
      itemId: id,
      mediaSourceId: playbackMetaRef.current.mediaSourceId,
      playSessionId: playbackMetaRef.current.playSessionId,
      initialStartTicks: playbackMetaRef.current.effectiveStartTicks,
      audioIndex: playbackMetaRef.current.audioIndex,
      subtitleIndex: playbackMetaRef.current.subtitleIndex,
      subtitleExplicitlyOff: subtitleExplicitlyOffRef.current,
    }

    let unloadReported = false
    const sendKeepaliveStop = () => {
      if (unloadReported || stopReportedRef.current) return
      const videoEl = videoRef.current
      if (!videoEl && lastVideoTimeSecRef.current <= 0 && sessionSnapshot.initialStartTicks <= 0) {
        return
      }
      unloadReported = true
      stopReportedRef.current = true
      const storedAuth = getStoredAuth()
      const token = storedAuth?.accessToken
      if (!token) return
      if (renderedItemIdRef.current === sessionSnapshot.itemId) {
        sessionSnapshot.audioIndex = playbackMetaRef.current.audioIndex
        sessionSnapshot.subtitleIndex = playbackMetaRef.current.subtitleIndex
        sessionSnapshot.subtitleExplicitlyOff = subtitleExplicitlyOffRef.current
        if (playbackMetaRef.current.mediaSourceId) {
          sessionSnapshot.mediaSourceId = playbackMetaRef.current.mediaSourceId
        }
        if (playbackMetaRef.current.playSessionId) {
          sessionSnapshot.playSessionId = playbackMetaRef.current.playSessionId
        }
      }
      const reportedSubIndex =
        sessionSnapshot.subtitleIndex !== undefined
          ? sessionSnapshot.subtitleIndex
          : sessionSnapshot.subtitleExplicitlyOff
            ? -1
            : undefined
      const curSec = videoEl ? videoEl.currentTime : lastVideoTimeSecRef.current
      let posTicks = Math.floor(curSec * 10_000_000)
      if (!initialSeekDoneRef.current && sessionSnapshot.initialStartTicks > 0 && curSec < 1) {
        posTicks = sessionSnapshot.initialStartTicks
      }
      const headers: Record<string, string> = {
        'Content-Type': 'application/json',
        Accept: 'application/json',
        Authorization: buildEmbyAuthHeader(token),
      }
      try {
        void fetch('/emby/Sessions/Playing/Stopped', {
          method: 'POST',
          headers,
          body: JSON.stringify({
            ItemId: sessionSnapshot.itemId,
            MediaSourceId: sessionSnapshot.mediaSourceId,
            PlaySessionId: sessionSnapshot.playSessionId,
            PositionTicks: posTicks,
            AudioStreamIndex: sessionSnapshot.audioIndex,
            SubtitleStreamIndex: reportedSubIndex,
          }),
          keepalive: true,
        })
      } catch {
        // 忽略页面卸载期间的网络异常
      }
    }

    window.addEventListener('pagehide', sendKeepaliveStop)
    window.addEventListener('beforeunload', sendKeepaliveStop)
    return () => {
      window.removeEventListener('pagehide', sendKeepaliveStop)
      window.removeEventListener('beforeunload', sendKeepaliveStop)
    }
  }, [hasPlaybackInfo, id])

  const reportSeekProgress = useCallback(() => {
    const now = Date.now()
    if (now - lastSeekReportRef.current < 100) return
    lastSeekReportRef.current = now
    reportState('progress', 'timeupdate')
  }, [reportState])

  // OSD 自动隐藏逻辑
  const pokeOsd = useCallback(() => {
    setOsdVisible(true)
    if (hideOsdTimerRef.current) window.clearTimeout(hideOsdTimerRef.current)
    hideOsdTimerRef.current = window.setTimeout(() => {
      if (videoRef.current && !videoRef.current.paused && activePanel === 'none') {
        setOsdVisible(false)
      }
    }, 3200)
  }, [activePanel])

  useEffect(() => {
    return () => {
      if (hideOsdTimerRef.current) {
        window.clearTimeout(hideOsdTimerRef.current)
        hideOsdTimerRef.current = null
      }
    }
  }, [])

  // 播放控制方法
  const togglePlay = useCallback(() => {
    const v = videoRef.current
    if (!v) return
    if (v.paused) {
      v.play().catch(() => undefined)
    } else {
      v.pause()
    }
    pokeOsd()
  }, [pokeOsd])

  const effectiveDurationSec = useMemo(() => {
    if (Number.isFinite(duration) && duration > 0) return duration
    const runtimeTicks = activeSource?.RunTimeTicks ?? item?.RunTimeTicks ?? 0
    return Number.isFinite(runtimeTicks) && runtimeTicks > 0 ? runtimeTicks / 10_000_000 : 0
  }, [duration, activeSource?.RunTimeTicks, item?.RunTimeTicks])

  const seekBy = useCallback(
    (delta: number) => {
      const v = videoRef.current
      if (!v) return
      const maxDuration =
        Number.isFinite(v.duration) && v.duration > 0 ? v.duration : effectiveDurationSec
      const baseTime = Number.isFinite(v.currentTime) ? v.currentTime : 0
      const target = baseTime + delta
      v.currentTime =
        maxDuration > 0 ? Math.max(0, Math.min(maxDuration, target)) : Math.max(0, target)
      setCurrentTime(v.currentTime)
      reportState('progress', 'timeupdate')
      pokeOsd()
    },
    [effectiveDurationSec, pokeOsd, reportState],
  )

  const toggleFullscreen = useCallback(() => {
    const el = containerRef.current
    if (!el) return
    if (!document.fullscreenElement) {
      el.requestFullscreen?.().catch(() => undefined)
    } else {
      document.exitFullscreen?.().catch(() => undefined)
    }
  }, [])

  const selectSubtitleTrack = useCallback(
    (nextIndex: number | undefined) => {
      subtitleExplicitlyOffRef.current = nextIndex === undefined
      setSubtitleIndex(nextIndex)
      playbackMetaRef.current.subtitleIndex = nextIndex
      reportState('progress', 'SubtitleTrackChange')
      pokeOsd()
    },
    [reportState, pokeOsd],
  )

  const cycleSubtitle = useCallback(() => {
    if (subtitleStreams.length === 0) return
    const indices = [undefined, ...subtitleStreams.map((s) => s.Index)]
    const curPos = indices.indexOf(subtitleIndex)
    const next = indices[(curPos + 1) % indices.length]
    selectSubtitleTrack(next)
  }, [subtitleStreams, subtitleIndex, selectSubtitleTrack])

  const handleExit = useCallback(() => {
    const historyIdx = (window.history.state as { idx?: number } | null)?.idx ?? 0
    if (historyIdx > 0) {
      navigate(-1)
    } else {
      navigate(item?.Id ? `/item/${item.Id}` : '/', { replace: true })
    }
  }, [navigate, item?.Id])

  const pipSupported = Boolean(typeof document !== 'undefined' && document.pictureInPictureEnabled)

  const togglePip = useCallback(() => {
    const v = videoRef.current
    if (!v || !pipSupported) return
    if (document.pictureInPictureElement) {
      document.exitPictureInPicture?.().catch(() => undefined)
    } else if (typeof v.requestPictureInPicture === 'function') {
      v.requestPictureInPicture().catch(() => undefined)
    }
    pokeOsd()
  }, [pipSupported, pokeOsd])

  // 订阅 WebSocket 远端播控指令 (Pause / Unpause / PlayPause / Stop / Seek)，校验 PlaySessionId
  useEffect(() => {
    return subscribePlaystate((cmd) => {
      if (
        cmd.PlaySessionId &&
        playbackMetaRef.current.playSessionId &&
        cmd.PlaySessionId !== playbackMetaRef.current.playSessionId
      ) {
        return
      }
      const v = videoRef.current
      switch (cmd.Command) {
        case 'Pause':
          v?.pause()
          pokeOsd()
          break
        case 'Unpause':
          v?.play().catch(() => undefined)
          pokeOsd()
          break
        case 'PlayPause':
          togglePlay()
          break
        case 'Stop':
          reportState('stop')
          handleExit()
          break
        case 'Seek': {
          const rawTicks = cmd.SeekPositionTicks
          const ticks = Number(rawTicks)
          if (
            rawTicks !== undefined &&
            rawTicks !== null &&
            rawTicks !== '' &&
            Number.isFinite(ticks) &&
            ticks >= 0
          ) {
            const targetSec = ticks / 10_000_000
            if (v) {
              v.currentTime = targetSec
              setCurrentTime(targetSec)
              reportState('progress', 'timeupdate')
            }
            pokeOsd()
          }
          break
        }
        default:
          break
      }
    })
  }, [subscribePlaystate, togglePlay, reportState, handleExit, pokeOsd])

  const handleSelectQuality = useCallback(
    (presetId: QualityPresetId) => {
      if (presetId !== qualityPreset) {
        captureResumePosition()
        setQualityPreset(presetId)
        if (presetId === 'auto') {
          setForceServerHls(false)
        }
      }
      setActivePanel('none')
      pokeOsd()
    },
    [qualityPreset, captureResumePosition, pokeOsd],
  )

  const chapters = item?.Chapters || []
  const activeChapterIdx = useMemo(() => {
    if (chapters.length === 0) return -1
    let idx = -1
    for (let i = 0; i < chapters.length; i++) {
      const startSec = chapters[i].StartPositionTicks / 10_000_000
      if (currentTime >= startSec - 0.5) {
        idx = i
      }
    }
    return idx
  }, [chapters, currentTime])

  const introSkipTarget = useMemo(() => {
    if (chapters.length === 0) return null
    for (let i = 0; i < chapters.length; i++) {
      const name = (chapters[i].Name || '').trim()
      if (!INTRO_CHAPTER_RE.test(name)) continue
      const startSec = chapters[i].StartPositionTicks / 10_000_000
      if (!Number.isFinite(startSec) || startSec < 0) continue
      const nextChapter = chapters[i + 1]
      const endSec = nextChapter
        ? nextChapter.StartPositionTicks / 10_000_000
        : effectiveDurationSec > startSec + 30
          ? Math.min(effectiveDurationSec, startSec + 90)
          : 0
      if (endSec > startSec && currentTime >= startSec && currentTime < endSec) {
        return {
          targetTicks: nextChapter ? nextChapter.StartPositionTicks : Math.floor(endSec * 10_000_000),
          label: name || t('playerUi.introDefaultLabel'),
        }
      }
    }
    return null
  }, [chapters, currentTime, effectiveDurationSec, t])

  const seekToChapter = useCallback(
    (startTicks: number) => {
      const v = videoRef.current
      const targetSec = Math.max(0, startTicks / 10_000_000)
      if (v && Number.isFinite(targetSec)) {
        v.currentTime = targetSec
        setCurrentTime(targetSec)
        reportState('progress', 'timeupdate')
      }
      setActivePanel('none')
      pokeOsd()
    },
    [pokeOsd, reportState],
  )

  const selectAudioTrack = useCallback(
    (nextIndex: number) => {
      if (nextIndex !== effectiveAudioIndex) {
        captureResumePosition()
        setUseProxyDirect(false)
        setForceServerHls(false)
        setWasmFallbackNotice(null)
        setAudioIndex(nextIndex)
        setQueryAudioIndex(nextIndex)
        playbackMetaRef.current.audioIndex = nextIndex
        reportState('progress', 'AudioTrackChange')
      }
      pokeOsd()
    },
    [effectiveAudioIndex, captureResumePosition, reportState, pokeOsd],
  )

  const cycleAudio = useCallback(() => {
    if (audioStreams.length <= 1) return
    const curPos = audioStreams.findIndex((a) => a.Index === effectiveAudioIndex)
    const next = audioStreams[(curPos + 1) % audioStreams.length]
    selectAudioTrack(next.Index)
  }, [audioStreams, effectiveAudioIndex, selectAudioTrack])

  const handleRetryPlayback = useCallback(() => {
    captureResumePosition()
    hlsRecoveryCountRef.current = 0
    setPlayerError(null)
    setBuffering(true)
    setRetryNonce((n) => n + 1)
    if (infoError) {
      void refetchPlaybackInfo()
    }
  }, [captureResumePosition, infoError, refetchPlaybackInfo])

  const toggleMute = useCallback(() => {
    const v = videoRef.current
    if (!v) return
    const nextMuted = !v.muted && !muted
    if (!nextMuted && v.volume === 0) {
      v.volume = 0.5
      if (hybridAudioRef.current) hybridAudioRef.current.volume = 0.5
      setVolume(0.5)
    }
    v.muted = nextMuted
    if (hybridAudioRef.current) hybridAudioRef.current.muted = nextMuted
    setMuted(nextMuted)
    pokeOsd()
  }, [muted, pokeOsd])

  useEffect(() => {
    const a = hybridAudioRef.current
    if (!a) return
    a.volume = volume
    a.muted = muted
    if (Number.isFinite(playbackRate) && playbackRate > 0) {
      a.playbackRate = playbackRate
    }
  }, [volume, muted, playbackRate])

  // 监听全屏与画中画状态变化
  useEffect(() => {
    const onFsChange = () => {
      setIsFullscreen(Boolean(document.fullscreenElement))
      assRendererRef.current?.resize()
    }
    document.addEventListener('fullscreenchange', onFsChange)
    return () => document.removeEventListener('fullscreenchange', onFsChange)
  }, [])

  useEffect(() => {
    const v = videoRef.current
    if (!v) return
    const onEnterPip = () => setIsPip(true)
    const onLeavePip = () => setIsPip(false)
    v.addEventListener('enterpictureinpicture', onEnterPip)
    v.addEventListener('leavepictureinpicture', onLeavePip)
    return () => {
      v.removeEventListener('enterpictureinpicture', onEnterPip)
      v.removeEventListener('leavepictureinpicture', onLeavePip)
    }
  }, [])

  // 全局快捷键：Space / 左右 / 上下 / M / F / P / C / A / I / ? / Esc
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.ctrlKey || e.metaKey || e.altKey) return
      if (
        e.target instanceof HTMLInputElement ||
        e.target instanceof HTMLTextAreaElement ||
        e.target instanceof HTMLSelectElement ||
        (e.target instanceof HTMLElement && e.target.isContentEditable)
      ) {
        return
      }
      if (remoteSubModalOpen) {
        return
      }
      if (showShortcuts) {
        if (e.key === 'Escape' || e.key === '?') {
          e.preventDefault()
          setShowShortcuts(false)
        }
        return
      }
      const key = e.key.toLowerCase()
      if (e.code === 'Space') {
        e.preventDefault()
        togglePlay()
      } else if (e.key === 'ArrowLeft') {
        e.preventDefault()
        seekBy(-10)
      } else if (e.key === 'ArrowRight') {
        e.preventDefault()
        seekBy(10)
      } else if (e.key === 'ArrowUp') {
        e.preventDefault()
        if (videoRef.current) {
          const next = Math.min(1, Number((videoRef.current.volume + 0.1).toFixed(2)))
          videoRef.current.volume = next
          videoRef.current.muted = false
          setVolume(next)
          setMuted(false)
        }
        pokeOsd()
      } else if (e.key === 'ArrowDown') {
        e.preventDefault()
        if (videoRef.current) {
          const next = Math.max(0, Number((videoRef.current.volume - 0.1).toFixed(2)))
          videoRef.current.volume = next
          videoRef.current.muted = next === 0
          setVolume(next)
          setMuted(next === 0)
        }
        pokeOsd()
      } else if (key === 'm') {
        e.preventDefault()
        toggleMute()
      } else if (key === 'f') {
        e.preventDefault()
        toggleFullscreen()
      } else if (key === 'p') {
        e.preventDefault()
        togglePip()
      } else if (key === 'c') {
        e.preventDefault()
        cycleSubtitle()
      } else if (key === 'a') {
        e.preventDefault()
        cycleAudio()
      } else if (key === 'i') {
        e.preventDefault()
        setShowStats((s) => !s)
      } else if (e.key === '?') {
        e.preventDefault()
        setShowShortcuts((s) => !s)
      } else if (e.key === 'Escape') {
        if (remoteSubModalOpen) {
        return
      }
      if (showShortcuts) {
          setShowShortcuts(false)
        } else if (activePanel !== 'none') {
          setActivePanel('none')
        } else if (!document.fullscreenElement) {
          handleExit()
        }
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [
    togglePlay,
    seekBy,
    toggleMute,
    toggleFullscreen,
    togglePip,
    cycleSubtitle,
    cycleAudio,
    remoteSubModalOpen,
    showShortcuts,
    activePanel,
    handleExit,
    pokeOsd,
  ])

  // 计算当前时间（叠加时间轴偏移）生效的 WebVTT/SRT 字幕文本
  const activeVttText = useMemo(() => {
    if (textCues.length === 0) return ''
    const effectiveTime = currentTime + subtitlePrefs.offsetSec
    return textCues
      .filter((c) => effectiveTime >= c.start && effectiveTime <= c.end)
      .map((c) => c.text)
      .join('\n')
  }, [textCues, currentTime, subtitlePrefs.offsetSec])

  const progressPct =
    effectiveDurationSec > 0 ? Math.min(100, (currentTime / effectiveDurationSec) * 100) : 0
  const bufferedPct =
    effectiveDurationSec > 0 ? Math.min(100, (bufferedEnd / effectiveDurationSec) * 100) : 0
  const hoverBifFrame = useMemo(() => {
    if (!hoverSeek || !bifArchive) return null
    return bifArchive.getFrameUrl(hoverSeek.timeSec)
  }, [hoverSeek, bifArchive])
  const hoverChapterTitle = useMemo(() => {
    if (!hoverSeek || chapters.length === 0) return null
    let matched: string | null = null
    for (let i = 0; i < chapters.length; i++) {
      const startSec = chapters[i].StartPositionTicks / 10_000_000
      if (hoverSeek.timeSec >= startSec) {
        matched = chapters[i].Name || t('itemDetailUi.chapterNumberTitle', { index: i + 1 })
      }
    }
    return matched
  }, [hoverSeek, chapters, t])

  return (
    <div
      ref={containerRef}
      onMouseMove={pokeOsd}
      onClick={() => {
        if (activePanel !== 'none') {
          setActivePanel('none')
        }
      }}
      className={`relative flex h-screen w-screen select-none items-center justify-center overflow-hidden bg-black ${
        osdVisible ? 'cursor-default' : 'cursor-none'
      }`}
    >
      {/* 主视频元素（不设 crossOrigin，避免 STRM 302 重定向到无 ACAO 响应头的网盘/CDN 时被浏览器 CORS 拦截） */}
      <video
        ref={videoRef}
        playsInline
        onClick={(e) => {
          e.stopPropagation()
          if (activePanel !== 'none') {
            setActivePanel('none')
          } else {
            togglePlay()
          }
        }}
        onDoubleClick={toggleFullscreen}
        onPlay={() => {
          stopReportedRef.current = false
          setPlaying(true)
          setBuffering(false)
          if (isHybridVideoOnly && hybridAudioRef.current?.src) {
            hybridAudioRef.current.play().catch(() => undefined)
          }
          reportState('progress', 'Unpause')
        }}
        onPause={() => {
          setPlaying(false)
          setOsdVisible(true)
          if (isHybridVideoOnly && hybridAudioRef.current) {
            hybridAudioRef.current.pause()
          }
          reportState('progress', 'Pause')
        }}
        onWaiting={() => {
          setBuffering(true)
          if (isHybridVideoOnly && hybridAudioRef.current) {
            hybridAudioRef.current.pause()
          }
        }}
        onPlaying={() => {
          setBuffering(false)
          if (isHybridVideoOnly && hybridAudioRef.current?.src && !videoRef.current?.paused) {
            hybridAudioRef.current.play().catch(() => undefined)
          }
        }}
        onSeeked={(e) => {
          const targetSec = Math.max(0, e.currentTarget.currentTime || 0)
          if (Number.isFinite(targetSec)) {
            lastVideoTimeSecRef.current = targetSec
          }
          if (!isHybridVideoOnly || !hybridAudioRef.current || !id) return
          const a = hybridAudioRef.current
          const localSec = targetSec - hybridAudioBaseSecRef.current
          let withinBuffered = false
          if (localSec >= 0 && a.buffered) {
            for (let i = 0; i < a.buffered.length; i++) {
              if (localSec >= a.buffered.start(i) && localSec <= a.buffered.end(i)) {
                withinBuffered = true
                break
              }
            }
          }
          if (withinBuffered) {
            a.currentTime = localSec
            const rate = playbackRateRef.current
            if (Number.isFinite(rate) && rate > 0) {
              a.playbackRate = rate
            }
            if (!e.currentTarget.paused) {
              a.play().catch(() => undefined)
            }
          } else {
            hybridAudioBaseSecRef.current = targetSec
            a.src = getUniversalAudioTranscodeUrl(id, {
              mediaSourceId: activeSource?.Id,
              audioStreamIndex: effectiveAudioIndex,
              startTimeTicks: Math.floor(targetSec * 10_000_000),
              isStrm: Boolean(streamPlan?.isStrm),
              userId,
            })
            a.load()
            const rate = playbackRateRef.current
            if (Number.isFinite(rate) && rate > 0) {
              a.defaultPlaybackRate = rate
              a.playbackRate = rate
            }
            if (!e.currentTarget.paused) {
              a.play().catch(() => undefined)
            }
          }
        }}
        onLoadedMetadata={(e) => {
          const d = e.currentTarget.duration
          setDuration(Number.isFinite(d) && d > 0 ? d : 0)
          setBuffering(false)
        }}
        onDurationChange={(e) => {
          const d = e.currentTarget.duration
          if (Number.isFinite(d) && d > 0) {
            setDuration(d)
          }
        }}
        onTimeUpdate={(e) => {
          const v = e.currentTarget
          if (Number.isFinite(v.currentTime) && v.currentTime >= 0) {
            lastVideoTimeSecRef.current = v.currentTime
          }
          setCurrentTime(v.currentTime)
          if (v.buffered.length > 0) {
            setBufferedEnd(v.buffered.end(v.buffered.length - 1))
          }
          if (
            isHybridVideoOnly &&
            !v.paused &&
            hybridAudioRef.current &&
            !hybridAudioRef.current.paused
          ) {
            const a = hybridAudioRef.current
            const expectedLocal = Math.max(0, v.currentTime - hybridAudioBaseSecRef.current)
            const drift = expectedLocal - a.currentTime
            if (Math.abs(drift) > 0.35 && Math.abs(drift) < 5) {
              for (let i = 0; i < a.buffered.length; i++) {
                if (expectedLocal >= a.buffered.start(i) && expectedLocal <= a.buffered.end(i)) {
                  a.currentTime = expectedLocal
                  break
                }
              }
            }
          }
        }}
        onEnded={() => {
          setPlaying(false)
          setOsdVisible(true)
          if (hybridAudioRef.current) {
            hybridAudioRef.current.pause()
          }
          reportState('stop')
          if (nextQueueItem && (playlistId || loadPlaybackPrefs(userId).autoPlayNextEpisode)) {
            setNextUpCountdown(8)
          }
        }}
        onError={() => {
          // 1) STRM 源直连失败（如 302 跨域/Referer 限制或浏览器拒绝外链）时，优先尝试服务端代理直出 (&proxy=1)
          if (
            playMethod === 'DirectPlay' &&
            !useProxyDirect &&
            !forceServerHls &&
            streamPlan?.isStrm &&
            streamPlan?.proxyDirectUrl
          ) {
            captureResumePosition()
            setWasmFallbackNotice(t('playerUi.fallbackStrmProxyNotice'))
            setUseProxyDirect(true)
            return
          }
          // 2) 若当前为 DirectPlay / Proxy Direct / Stream MKV (MSE) / Client WASM Remux 解码失败，自动无感回退服务端 HLS
          if (playMethod !== 'Server HLS' && !forceServerHls && streamPlan?.hlsUrl) {
            captureResumePosition()
            setWasmFallbackNotice(
              playMethod === 'Proxy Direct'
                ? t('playerUi.fallbackProxyDirectDecodeFailed')
                : playMethod === 'Stream MKV (MSE)'
                  ? t('playerUi.fallbackStreamMkvDecodeFailed')
                  : playMethod === 'Client WASM Remux'
                    ? t('playerUi.fallbackWasmDecodeFailed')
                    : t('playerUi.fallbackDirectDecodeFailed'),
            )
            setForceServerHls(true)
            return
          }
          setBuffering(false)
          setPlayerError(t('player.loadError'))
        }}
        className={`h-full w-full ${aspectMode === 'cover' ? 'object-cover' : 'object-contain'}`}
      />

      {/* 混合双流模式隐藏同步音频（大体积杜比 AC3/EAC3/DTS MKV 零视频重编码） */}
      <audio
        ref={hybridAudioRef}
        data-testid="hybrid-audio-element"
        className="hidden"
        onLoadedMetadata={(e) => {
          const rate = playbackRateRef.current
          if (Number.isFinite(rate) && rate > 0) {
            e.currentTarget.defaultPlaybackRate = rate
            e.currentTarget.playbackRate = rate
          }
        }}
        onError={() => {
          if (isHybridVideoOnly && !forceServerHls) {
            captureResumePosition()
            setWasmFallbackNotice(t('playerUi.fallbackHybridAudioFailed'))
            setForceServerHls(true)
          }
        }}
      />

      {/* JASSUB (libass WASM) ASS/SSA 特效字幕覆盖 Canvas */}
      <canvas
        ref={assCanvasRef}
        className={`pointer-events-none absolute inset-0 h-full w-full ${
          aspectMode === 'cover' ? 'object-cover' : 'object-contain'
        } ${activeSubtitleMeta.engine === 'ass' ? 'block' : 'hidden'}`}
      />

      {/* PGS 蓝光图形字幕覆盖 Canvas */}
      <canvas
        ref={pgsCanvasRef}
        className={`pointer-events-none absolute inset-0 h-full w-full ${
          aspectMode === 'cover' ? 'object-cover' : 'object-contain'
        } ${activeSubtitleMeta.engine === 'pgs' ? 'block' : 'hidden'}`}
      />

      {/* WebVTT / SRT 影院级文本字幕覆盖层 */}
      {activeSubtitleMeta.engine === 'vtt' && activeVttText && (
        <div
          className="pointer-events-none absolute inset-x-0 z-10 flex justify-center px-6"
          style={{ bottom: `${subtitlePrefs.bottomMarginPx}px` }}
        >
          <div
            className="cinema-subtitle-text max-w-4xl whitespace-pre-line rounded-xl px-4 py-1.5 text-center font-semibold leading-snug tracking-wide text-white"
            style={{
              fontSize: `${(1.5 * subtitlePrefs.fontScale).toFixed(3)}rem`,
              backgroundColor: `rgba(0, 0, 0, ${subtitlePrefs.bgOpacity})`,
            }}
          >
            {activeVttText}
          </div>
        </div>
      )}

      {/* 客户端 WASM 零重编码转封装品牌进度浮层 */}
      {wasmProgress && !playerError && !infoError && (
        <div
          onClick={(e) => e.stopPropagation()}
          className="absolute inset-0 z-15 flex items-center justify-center bg-black/55 px-4 backdrop-blur-sm"
        >
          <div className="w-full max-w-md rounded-2xl border border-amber-500/30 bg-zinc-950/90 p-6 shadow-2xl backdrop-blur-xl">
            <div className="flex items-center gap-3.5">
              <div className="relative flex h-12 w-12 shrink-0 items-center justify-center rounded-xl bg-amber-500/15 ring-1 ring-amber-400/30">
                <AmbraLogo size={30} className="animate-pulse" />
                <Cpu className="absolute -bottom-1 -right-1 h-4 w-4 rounded-full bg-zinc-950 p-0.5 text-amber-400" />
              </div>
              <div className="min-w-0 flex-1">
                <div className="flex items-center justify-between gap-2">
                  <span className="text-sm font-semibold text-white">
                    {wasmProgress.stage === 'fetch'
                      ? t('playerUi.wasmStageFetch')
                      : wasmProgress.stage === 'init'
                        ? t('playerUi.wasmStageInit')
                        : wasmProgress.stage === 'remux'
                          ? t('playerUi.wasmStageRemux')
                          : t('playerUi.wasmDone')}
                  </span>
                  <span className="font-mono text-xs font-semibold text-amber-300">
                    {wasmProgress.percent}%
                  </span>
                </div>
                <p className="mt-0.5 truncate text-xs text-zinc-400">{wasmProgress.detail}</p>
              </div>
            </div>

            <div className="mt-4 h-2 w-full overflow-hidden rounded-full bg-white/10">
              <div
                className="h-full rounded-full bg-gradient-to-r from-amber-500 via-orange-400 to-amber-300 transition-all duration-300"
                style={{ width: `${Math.max(4, wasmProgress.percent)}%` }}
              />
            </div>

            <div className="mt-3.5 flex items-center justify-between text-[11px] text-zinc-400">
              <span className="flex items-center gap-1.5">
                <span className="inline-block h-1.5 w-1.5 rounded-full bg-amber-400" />
                {t('playerUi.wasmPassthroughHint')}
              </span>
              <button
                type="button"
                onClick={() => {
                  captureResumePosition()
                  setWasmFallbackNotice(t('playerUi.switchedToServerHlsManual'))
                  setForceServerHls(true)
                }}
                className="rounded-lg border border-white/15 bg-white/5 px-2.5 py-1 text-[11px] font-medium text-zinc-200 transition-colors hover:bg-white/15"
              >
                {t('playerUi.switchToServerHlsBtn')}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* 缓冲或初始化加载指示 */}
      {(infoLoading || buffering) && !wasmProgress && !playerError && !infoError && (
        <div className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center">
          <div className="flex flex-col items-center gap-3 rounded-2xl border border-amber-500/20 bg-black/70 px-6 py-5 shadow-2xl backdrop-blur-md">
            <div className="relative flex items-center justify-center">
              <AmbraLogo size={44} className="animate-pulse" />
              <Loader2 className="absolute -bottom-1.5 -right-1.5 h-5 w-5 animate-spin rounded-full bg-zinc-950/90 p-0.5 text-amber-400" />
            </div>
            <span className="text-xs font-medium tracking-wide text-zinc-300">
              {infoLoading ? t('player.loadingStream') : t('player.buffering')}
            </span>
          </div>
        </div>
      )}

      {/* 错误提示浮层 */}
      {(playerError || infoError) && (
        <div className="absolute inset-0 z-20 flex items-center justify-center bg-black/80 px-4">
          <div className="max-w-md rounded-2xl border border-red-500/30 bg-zinc-950 p-6 text-center shadow-2xl">
            <AlertTriangle className="mx-auto mb-3 h-10 w-10 text-red-400" />
            <h2 className="text-base font-semibold text-white">{t('player.cannotPlayTitle')}</h2>
            <p className="mt-2 text-xs text-zinc-400">
              {playerError ||
                (infoError instanceof Error
                  ? infoError.message
                  : t('playerUi.errFetchPlaybackInfo'))}
            </p>
            <div className="mt-5 flex items-center justify-center gap-3">
              <button
                type="button"
                onClick={handleRetryPlayback}
                className="rounded-xl bg-amber-500 px-5 py-2 text-xs font-semibold text-zinc-950 hover:bg-amber-400"
              >
                {t('player.retryPlayback')}
              </button>
              <button
                type="button"
                onClick={handleExit}
                className="rounded-xl border border-white/15 bg-white/10 px-5 py-2 text-xs font-semibold text-zinc-100 hover:bg-white/20"
              >
                {t('player.goBack')}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* 跳过片头 (Intro Skip) 浮层按钮 */}
      {introSkipTarget && nextUpCountdown === null && !playerError && !infoError && (
        <button
          type="button"
          data-testid="skip-intro-button"
          onClick={(e) => {
            e.stopPropagation()
            seekToChapter(introSkipTarget.targetTicks)
          }}
          className="absolute bottom-24 right-6 z-30 inline-flex items-center gap-2 rounded-xl border border-amber-400/50 bg-zinc-950/90 px-4 py-2.5 text-xs font-semibold text-amber-300 shadow-2xl backdrop-blur-xl transition-all hover:bg-amber-500 hover:text-zinc-950"
        >
          <SkipForward className="h-4 w-4" />
          <span>{t('playerUi.skipIntroBtn')}</span>
        </button>
      )}

      {/* 自动连播下一集 / 下一项 8 秒倒计时浮层 */}
      {nextUpCountdown !== null && nextQueueItem && (
        <div
          onClick={(e) => e.stopPropagation()}
          className="absolute bottom-28 right-6 z-30 w-80 max-w-[calc(100vw-3rem)] rounded-2xl border border-amber-500/35 bg-zinc-950/92 p-4 shadow-2xl backdrop-blur-xl"
        >
          <p className="text-[11px] font-semibold uppercase tracking-wider text-amber-400">
            {playlistId ? t('playerUi.upNextPlaylistHeader') : t('playerUi.upNextEpisodeHeader')}
          </p>
          <p className="mt-1 truncate text-sm font-semibold text-white">
            {nextQueueItem.ParentIndexNumber != null && nextQueueItem.IndexNumber != null
              ? `S${String(nextQueueItem.ParentIndexNumber).padStart(2, '0')}E${String(nextQueueItem.IndexNumber).padStart(2, '0')} `
              : nextQueueItem.IndexNumber != null
                ? `${t('itemDetailUi.episodeNumberLabel', { index: nextQueueItem.IndexNumber })} `
                : ''}
            {nextQueueItem.Name}
          </p>
          <p className="mt-1 text-xs text-zinc-400">
            {t('playerUi.autoPlayInSeconds', { seconds: nextUpCountdown })}
          </p>
          <div className="mt-3 h-1 w-full overflow-hidden rounded-full bg-white/10">
            <div
              className="h-full bg-amber-400 transition-all duration-1000 ease-linear"
              style={{ width: `${Math.max(0, Math.min(100, ((8 - nextUpCountdown) / 8) * 100))}%` }}
            />
          </div>
          <div className="mt-3.5 flex items-center justify-end gap-2">
            <button
              type="button"
              onClick={() => setNextUpCountdown(null)}
              className="rounded-xl border border-white/15 bg-white/5 px-3 py-1.5 text-xs font-medium text-zinc-200 transition-colors hover:bg-white/15"
            >
              {t('common.cancel')}
            </button>
            <button
              type="button"
              onClick={() => {
                setNextUpCountdown(null)
                navigate(buildQueuePlayUrl(nextQueueItem.Id), { replace: true })
              }}
              className="inline-flex items-center gap-1 rounded-xl bg-amber-500 px-3.5 py-1.5 text-xs font-semibold text-zinc-950 transition-colors hover:bg-amber-400"
            >
              <Play className="h-3.5 w-3.5 fill-current" />
              <span>{t('player.playNextNow')}</span>
            </button>
          </div>
        </div>
      )}

      {/* 播放统计浮层 (Stats for Nerds, 快捷键 I) */}
      {showStats && (
        <div
          onClick={(e) => e.stopPropagation()}
          className="absolute left-5 top-20 z-30 w-88 rounded-2xl border border-white/15 bg-black/85 p-4 text-xs text-zinc-200 shadow-2xl backdrop-blur-xl"
        >
          <div className="mb-3 flex items-center justify-between border-b border-white/10 pb-2">
            <span className="flex items-center gap-1.5 font-semibold text-amber-400">
              <Activity className="h-3.5 w-3.5" />
              {t('player.stats')}
            </span>
            <button
              type="button"
              onClick={() => setShowStats(false)}
              className="text-zinc-400 hover:text-white"
            >
              <X className="h-3.5 w-3.5" />
            </button>
          </div>
          <dl className="space-y-2 font-mono text-[11px]">
            <div className="flex items-center justify-between gap-3">
              <dt className="text-zinc-400">{t('playerUi.statPlayMode')}</dt>
              <dd>
                <PlayMethodBadge method={playMethod} />
              </dd>
            </div>
            <StatRow
              label={t('playerUi.statClientDemux')}
              value={
                playMethod === 'Stream MKV (MSE)'
                  ? isHybridVideoOnly
                    ? t('playerUi.statDemuxHybridMse')
                    : t('playerUi.statDemuxRangeMse')
                  : playMethod === 'Client WASM Remux'
                    ? t('playerUi.statDemuxWasmActive')
                    : !wasmSupported
                      ? t('playerUi.statDemuxUnsupported')
                      : !preferWasmRemux
                        ? t('playerUi.statDemuxDisabled')
                        : wasmFallbackNotice
                          ? t('playerUi.statDemuxAutoDowngraded')
                          : t('playerUi.statDemuxStandby')
              }
              highlight={playMethod === 'Stream MKV (MSE)' || playMethod === 'Client WASM Remux'}
            />
            <StatRow
              label={t('detail.container')}
              value={(activeSource?.Container || '-').toUpperCase()}
            />
            {activeSource?.Size !== undefined && activeSource.Size > 0 && (
              <StatRow
                label={t('playerUi.statFileVolume')}
                value={formatBytes(activeSource.Size)}
              />
            )}
            <StatRow
              label={t('playerUi.statVideoCodec')}
              value={
                videoStream
                  ? `${(videoStream.Codec || '-').toUpperCase()} ${videoStream.Width || ''}x${videoStream.Height || ''}`
                  : '-'
              }
            />
            <StatRow
              label={t('playerUi.statAudioCodec')}
              value={
                activeAudioStream
                  ? `${(activeAudioStream.Codec || '-').toUpperCase()} ${activeAudioStream.Channels || ''}ch`
                  : '-'
              }
            />
            <StatRow
              label={t('playerUi.statSubtitleEngine')}
              value={
                activeSubtitleMeta.engine === 'none'
                  ? t('playerUi.statSubtitleNone')
                  : activeSubtitleMeta.engineLabel
              }
              highlight={activeSubtitleMeta.engine !== 'none'}
            />
            {activeSubtitleMeta.engine === 'ass' && loadedFontCount > 0 && (
              <StatRow
                label={t('playerUi.statEmbeddedFonts')}
                value={t('playerUi.statFontsMountedCount', { count: loadedFontCount })}
                highlight
              />
            )}
            {subtitlePrefs.offsetSec !== 0 && (
              <StatRow
                label={t('playerUi.statSubtitleOffset')}
                value={formatSubtitleOffset(subtitlePrefs.offsetSec)}
                highlight
              />
            )}
            <StatRow
              label={t('playerUi.statBufferProgress')}
              value={`${formatSeconds(bufferedEnd)} / ${formatSeconds(effectiveDurationSec)}`}
            />
            {wasmFallbackNotice && (
              <p className="rounded-lg border border-sky-400/20 bg-sky-500/10 px-2.5 py-1.5 font-sans text-[11px] leading-relaxed text-sky-200">
                {t('playerUi.statFallbackNoticePrefix', { notice: wasmFallbackNotice })}
              </p>
            )}
            {subtitleError && (
              <p className="pt-1 text-[11px] text-rose-400">
                {t('playerUi.statSubtitleErrorPrefix', { error: subtitleError })}
              </p>
            )}
          </dl>
        </div>
      )}

      {/* 快捷键说明浮层 (快捷键 ?) */}
      {showShortcuts && (
        <div
          onClick={() => setShowShortcuts(false)}
          className="absolute inset-0 z-30 flex items-center justify-center bg-black/60 px-4 backdrop-blur-sm"
        >
          <div
            onClick={(e) => e.stopPropagation()}
            className="w-full max-w-md rounded-2xl border border-white/15 bg-zinc-950/90 p-5 text-xs text-zinc-200 shadow-2xl backdrop-blur-xl"
          >
            <div className="mb-3 flex items-center justify-between border-b border-white/10 pb-2.5">
              <span className="flex items-center gap-1.5 text-sm font-semibold text-amber-400">
                <HelpCircle className="h-4 w-4" />
                {t('player.shortcuts')}
              </span>
              <button
                type="button"
                onClick={() => setShowShortcuts(false)}
                aria-label={t('playerUi.closeShortcutsAria')}
                className="rounded-lg p-1 text-zinc-400 hover:bg-white/10 hover:text-white"
              >
                <X className="h-4 w-4" />
              </button>
            </div>
            <dl className="grid grid-cols-1 gap-2 sm:grid-cols-2">
              {KEYBOARD_SHORTCUTS.map((sc) => (
                <div
                  key={sc.keys}
                  className="flex items-center justify-between gap-2 rounded-xl border border-white/8 bg-white/[0.03] px-3 py-2"
                >
                  <dt className="text-zinc-300">{t(sc.descKey)}</dt>
                  <dd className="shrink-0 rounded-md border border-white/15 bg-white/10 px-1.5 py-0.5 font-mono text-[11px] font-semibold text-amber-300">
                    {sc.keys}
                  </dd>
                </div>
              ))}
            </dl>
          </div>
        </div>
      )}

      {/* 顶部 OSD 导航栏 */}
      <div
        onClick={(e) => e.stopPropagation()}
        className={`absolute inset-x-0 top-0 z-20 flex items-center justify-between bg-gradient-to-b from-black/85 via-black/45 to-transparent px-5 py-4 transition-opacity duration-300 ${
          osdVisible ? 'opacity-100' : 'pointer-events-none opacity-0'
        }`}
      >
        <div className="flex min-w-0 items-center gap-3">
          <button
            type="button"
            onClick={handleExit}
            className="flex h-10 w-10 items-center justify-center rounded-xl bg-white/10 text-white backdrop-blur-md transition-colors hover:bg-white/20"
            title={t('common.back')}
          >
            <ArrowLeft className="h-5 w-5" />
          </button>
          <div className="min-w-0">
            <h1 className="truncate text-base font-semibold text-white">
              {item?.SeriesName ? `${item.SeriesName} · ` : ''}
              {item?.Name || t('playerUi.nowPlayingFallback')}
            </h1>
            <div className="mt-0.5 flex items-center gap-2 text-[11px] text-zinc-300">
              <PlayMethodBadge method={playMethod} />
              {activeSubtitleMeta.engine !== 'none' && (
                <span className="rounded bg-white/10 px-1.5 py-0.5 text-zinc-200">
                  {t('playerUi.topSubtitleBadge', { label: activeSubtitleMeta.engineLabel })}
                </span>
              )}
            </div>
          </div>
        </div>

        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => setShowShortcuts((s) => !s)}
            className={`flex items-center gap-1.5 rounded-xl px-2.5 py-2 text-xs font-medium backdrop-blur-md transition-colors ${
              showShortcuts
                ? 'bg-amber-500 text-zinc-950'
                : 'bg-white/10 text-zinc-200 hover:bg-white/20'
            }`}
            title={t('playerUi.shortcutsBtnTitle')}
            aria-label={t('player.shortcuts')}
          >
            <HelpCircle className="h-4 w-4" />
            <span className="hidden sm:inline">?</span>
          </button>

          <button
            type="button"
            onClick={() => setShowStats((s) => !s)}
            className={`flex items-center gap-1.5 rounded-xl px-3 py-2 text-xs font-medium backdrop-blur-md transition-colors ${
              showStats
                ? 'bg-amber-500 text-zinc-950'
                : 'bg-white/10 text-zinc-200 hover:bg-white/20'
            }`}
            title={t('playerUi.statsBtnTitle')}
          >
            <Activity className="h-4 w-4" />
            <span className="hidden sm:inline">{t('playerUi.statsBtnShort')}</span>
          </button>
        </div>
      </div>

      {/* 底部 OSD 播放控制栏 */}
      <div
        onClick={(e) => e.stopPropagation()}
        className={`absolute inset-x-0 bottom-0 z-20 bg-gradient-to-t from-black/90 via-black/60 to-transparent px-5 pb-5 pt-12 transition-opacity duration-300 ${
          osdVisible ? 'opacity-100' : 'pointer-events-none opacity-0'
        }`}
      >
        {/* 进度条 */}
        <div
          className="group/bar relative mb-3 flex items-center py-2"
          onMouseMove={(e) => {
            if (effectiveDurationSec <= 0) return
            const rect = e.currentTarget.getBoundingClientRect()
            if (rect.width <= 0) return
            const ratio = Math.min(1, Math.max(0, (e.clientX - rect.left) / rect.width))
            setHoverSeek({ timeSec: ratio * effectiveDurationSec, pct: ratio * 100 })
          }}
          onMouseLeave={() => setHoverSeek(null)}
        >
          {hoverSeek && effectiveDurationSec > 0 && (
            <div
              data-testid="bif-preview-tooltip"
              className="pointer-events-none absolute bottom-8 z-30 -translate-x-1/2 overflow-hidden rounded-xl border border-white/15 bg-zinc-950/95 p-1.5 text-center shadow-2xl backdrop-blur-md"
              style={{ left: `${Math.min(92, Math.max(8, hoverSeek.pct))}%` }}
            >
              {hoverBifFrame && (
                <img
                  data-testid="bif-preview-image"
                  src={hoverBifFrame.url}
                  alt={t('playerUi.bifPreviewAlt', { time: formatSeconds(hoverSeek.timeSec) })}
                  className="mb-1 block h-[90px] w-40 rounded-lg bg-black object-cover"
                />
              )}
              <div className="px-1 font-mono text-[11px] font-semibold text-amber-300">
                {formatSeconds(hoverSeek.timeSec)}
              </div>
              {hoverChapterTitle && (
                <div className="max-w-[160px] truncate px-1 text-[10px] text-zinc-300">
                  {hoverChapterTitle}
                </div>
              )}
            </div>
          )}
          <div className="relative h-1.5 w-full overflow-hidden rounded-full bg-white/20 transition-all group-hover/bar:h-2">
            <div
              className="absolute inset-y-0 left-0 bg-white/30"
              style={{ width: `${bufferedPct}%` }}
            />
            <div
              className="absolute inset-y-0 left-0 bg-gradient-to-r from-amber-500 to-orange-400"
              style={{ width: `${progressPct}%` }}
            />
            {chapters.length > 0 &&
              effectiveDurationSec > 0 &&
              chapters.map((ch, idx) => {
                const startSec = ch.StartPositionTicks / 10_000_000
                if (!Number.isFinite(startSec) || startSec < 0 || startSec > effectiveDurationSec)
                  return null
                const pct = (startSec / effectiveDurationSec) * 100
                const chName = ch.Name || t('itemDetailUi.chapterNumberTitle', { index: idx + 1 })
                return (
                  <span
                    key={`${ch.StartPositionTicks}-${idx}`}
                    data-testid="chapter-marker"
                    title={`${chName} (${formatSeconds(startSec)})`}
                    className="pointer-events-none absolute inset-y-0 z-10 w-0.5 bg-white/75"
                    style={{ left: `${pct}%` }}
                  />
                )
              })}
          </div>
          <input
            type="range"
            min={0}
            max={effectiveDurationSec || 100}
            step={0.5}
            value={currentTime}
            onChange={(e) => {
              const t = Number(e.target.value)
              if (videoRef.current) videoRef.current.currentTime = t
              setCurrentTime(t)
              if (effectiveDurationSec > 0) {
                setHoverSeek({
                  timeSec: t,
                  pct: Math.min(100, Math.max(0, (t / effectiveDurationSec) * 100)),
                })
              }
            }}
            onPointerUp={() => {
              setHoverSeek(null)
              reportSeekProgress()
            }}
            onMouseUp={reportSeekProgress}
            onTouchEnd={() => {
              setHoverSeek(null)
              reportSeekProgress()
            }}
            onKeyUp={reportSeekProgress}
            className="ambra-range absolute inset-0 h-full w-full opacity-0 group-hover/bar:opacity-100"
          />
        </div>

        {/* 按钮行 */}
        <div className="flex items-center justify-between gap-3">
          {/* 左侧控制 */}
          <div className="flex items-center gap-2 sm:gap-3">
            {prevQueueItem && (
              <button
                type="button"
                onClick={() => navigate(buildQueuePlayUrl(prevQueueItem.Id), { replace: true })}
                className="rounded-lg p-2 text-zinc-200 hover:bg-white/10"
                title={
                  playlistId
                    ? t('playerUi.prevPlaylistItemTitle', { name: prevQueueItem.Name })
                    : t('playerUi.prevEpisodeTitle', { name: prevQueueItem.Name })
                }
                aria-label={playlistId ? t('playerUi.prevItemAria') : t('playerUi.prevEpisodeAria')}
              >
                <SkipBack className="h-4 w-4" />
              </button>
            )}

            <button
              type="button"
              onClick={togglePlay}
              className="flex h-10 w-10 items-center justify-center rounded-xl bg-amber-500 text-zinc-950 transition-colors hover:bg-amber-400"
              title={t('playerUi.playPauseTitle')}
            >
              {playing ? (
                <Pause className="h-5 w-5 fill-current" />
              ) : (
                <Play className="ml-0.5 h-5 w-5 fill-current" />
              )}
            </button>

            {nextQueueItem && (
              <button
                type="button"
                onClick={() => navigate(buildQueuePlayUrl(nextQueueItem.Id), { replace: true })}
                className="rounded-lg p-2 text-zinc-200 hover:bg-white/10"
                title={
                  playlistId
                    ? t('playerUi.nextPlaylistItemTitle', { name: nextQueueItem.Name })
                    : t('playerUi.nextEpisodeTitle', { name: nextQueueItem.Name })
                }
                aria-label={playlistId ? t('playerUi.nextItemAria') : t('playerUi.nextEpisodeAria')}
              >
                <SkipForward className="h-4 w-4" />
              </button>
            )}

            <button
              type="button"
              onClick={() => seekBy(-10)}
              className="rounded-lg p-2 text-zinc-200 hover:bg-white/10"
              title={t('playerUi.rewind10Title')}
            >
              <RotateCcw className="h-4 w-4" />
            </button>
            <button
              type="button"
              onClick={() => seekBy(10)}
              className="rounded-lg p-2 text-zinc-200 hover:bg-white/10"
              title={t('playerUi.forward10Title')}
            >
              <RotateCw className="h-4 w-4" />
            </button>

            {/* 音量 */}
            <div className="hidden items-center gap-2 sm:flex">
              <button
                type="button"
                onClick={toggleMute}
                title={t('playerUi.muteToggleTitle')}
                aria-label={
                  muted || volume === 0 ? t('playerUi.unmuteAria') : t('playerUi.muteAria')
                }
                className="rounded-lg p-2 text-zinc-200 hover:bg-white/10"
              >
                {muted || volume === 0 ? (
                  <VolumeX className="h-4 w-4 text-zinc-400" />
                ) : (
                  <Volume2 className="h-4 w-4" />
                )}
              </button>
              <input
                type="range"
                min={0}
                max={1}
                step={0.05}
                value={muted ? 0 : volume}
                onChange={(e) => {
                  const val = Number(e.target.value)
                  if (videoRef.current) {
                    videoRef.current.volume = val
                    videoRef.current.muted = val === 0
                  }
                  if (hybridAudioRef.current) {
                    hybridAudioRef.current.volume = val
                    hybridAudioRef.current.muted = val === 0
                  }
                  setVolume(val)
                  setMuted(val === 0)
                }}
                className="ambra-range h-1 w-20 rounded-full bg-white/25"
              />
            </div>

            <span className="ml-1 font-mono text-xs text-zinc-300">
              {formatSeconds(currentTime)} <span className="text-zinc-500">/</span>{' '}
              {formatSeconds(effectiveDurationSec)}
            </span>
          </div>

          {/* 右侧控制：章节 / 画质 / 音轨 / 字幕 / 倍速 / 画中画 / 全屏 */}
          <div className="relative flex items-center gap-1.5 sm:gap-2">
            {/* 章节列表 */}
            {chapters.length > 0 && (
              <div className="relative">
                <button
                  type="button"
                  onClick={() => setActivePanel((p) => (p === 'chapters' ? 'none' : 'chapters'))}
                  className={`flex items-center gap-1.5 rounded-xl px-3 py-2 text-xs font-medium transition-colors ${
                    activePanel === 'chapters'
                      ? 'bg-amber-500 text-zinc-950'
                      : 'bg-white/10 text-zinc-200 hover:bg-white/20'
                  }`}
                  title={t('playerUi.chapterListTitle')}
                >
                  <Bookmark className="h-4 w-4" />
                  <span className="hidden md:inline">{t('player.chapters')}</span>
                </button>

                {activePanel === 'chapters' && (
                  <div className="absolute bottom-12 right-0 z-30 w-72 overflow-hidden rounded-2xl border border-white/15 bg-zinc-950/95 p-2 shadow-2xl backdrop-blur-xl">
                    <p className="px-2.5 py-1.5 text-[11px] font-semibold text-zinc-400">
                      {t('player.chapters')} ({chapters.length})
                    </p>
                    <div className="max-h-64 space-y-1 overflow-y-auto">
                      {chapters.map((ch, idx) => {
                        const startSec = ch.StartPositionTicks / 10_000_000
                        const isActive = idx === activeChapterIdx
                        return (
                          <button
                            key={`${ch.StartPositionTicks}-${idx}`}
                            type="button"
                            onClick={() => seekToChapter(ch.StartPositionTicks)}
                            className={`flex w-full items-center justify-between gap-2 rounded-xl px-2.5 py-2 text-left text-xs transition-colors ${
                              isActive
                                ? 'bg-amber-500/20 font-semibold text-amber-300'
                                : 'text-zinc-200 hover:bg-white/8'
                            }`}
                          >
                            <span className="truncate">
                              {ch.Name || t('itemDetailUi.chapterNumberTitle', { index: idx + 1 })}
                            </span>
                            <span className="shrink-0 font-mono text-[11px] text-zinc-400">
                              {formatSeconds(startSec)}
                            </span>
                          </button>
                        )
                      })}
                    </div>
                  </div>
                )}
              </div>
            )}

            {/* 多版本切换 */}
            {(playbackInfo?.MediaSources?.length ?? 0) > 1 && (
              <div className="relative">
                <button
                  type="button"
                  onClick={() => setActivePanel((p) => (p === 'version' ? 'none' : 'version'))}
                  className={`flex items-center gap-1.5 rounded-xl px-2.5 py-2 text-xs font-medium transition-colors ${
                    activePanel === 'version'
                      ? 'bg-amber-500 text-zinc-950'
                      : 'bg-white/10 text-zinc-200 hover:bg-white/20'
                  }`}
                  title={t('playerUi.switchVersionTitle')}
                  aria-label={t('playerUi.switchVersionTitle')}
                >
                  <Layers className="h-4 w-4" />
                  <span className="hidden md:inline">{t('player.version')}</span>
                </button>

                {activePanel === 'version' && (
                  <div className="absolute bottom-12 right-0 z-30 w-64 overflow-hidden rounded-2xl border border-white/15 bg-zinc-950/95 p-2 shadow-2xl backdrop-blur-xl">
                    <p className="px-2.5 py-1.5 text-[11px] font-semibold text-zinc-400">
                      {t('playerUi.selectVersionCount', {
                        count: playbackInfo!.MediaSources.length,
                      })}
                    </p>
                    <div className="max-h-60 space-y-1 overflow-y-auto">
                      {playbackInfo!.MediaSources.map((src) => (
                        <button
                          key={src.Id}
                          type="button"
                          onClick={() => {
                            if (src.Id !== activeSource?.Id) {
                              captureResumePosition()
                              const prefs = loadPlaybackPrefs(userId)
                              const nextStreams = src.MediaStreams || []
                              const nextAudioList = nextStreams.filter((s) => s.Type === 'Audio')
                              const nextAudio = pickPreferredAudioIndex(
                                nextAudioList,
                                prefs.preferredAudioLang,
                                src.DefaultAudioStreamIndex,
                                locale,
                              )
                              const nextAudioStream =
                                nextAudioList.find((a) => a.Index === nextAudio) || nextAudioList[0]
                              const nextSubList = nextStreams.filter((s) => s.Type === 'Subtitle')
                              const nextSub = pickPreferredSubtitleIndex(
                                nextSubList,
                                prefs.preferredSubtitleLang,
                                src.DefaultSubtitleStreamIndex,
                                locale,
                                prefs.subtitleMode,
                                nextAudioStream,
                              )
                              initializedAudioSourceIdRef.current = src.Id
                              initializedSubSourceIdRef.current = src.Id
                              subtitleExplicitlyOffRef.current =
                                nextSub === undefined &&
                                (prefs.preferredSubtitleLang === 'off' ||
                                  prefs.subtitleMode === 'None' ||
                                  prefs.subtitleMode === 'Smart' ||
                                  prefs.subtitleMode === 'OnlyForced')
                              setUseProxyDirect(false)
                              setForceServerHls(false)
                              setWasmFallbackNotice(null)
                              setSelectedSourceId(src.Id)
                              setQuerySourceId(src.Id)
                              setAudioIndex(nextAudio)
                              setQueryAudioIndex(
                                nextAudio !== undefined && nextAudio !== src.DefaultAudioStreamIndex
                                  ? nextAudio
                                  : undefined,
                              )
                              setSubtitleIndex(nextSub)
                            }
                            setActivePanel('none')
                            pokeOsd()
                          }}
                          className={`flex w-full items-center justify-between gap-2 rounded-xl px-2.5 py-2 text-left text-xs transition-colors ${
                            src.Id === activeSource?.Id
                              ? 'bg-amber-500/20 font-semibold text-amber-300'
                              : 'text-zinc-200 hover:bg-white/8'
                          }`}
                        >
                          <span className="truncate">{src.Name || src.Id}</span>
                          {src.Container && (
                            <span className="shrink-0 rounded bg-white/10 px-1.5 py-0.5 font-mono text-[10px] text-zinc-300">
                              {src.Container.toUpperCase()}
                            </span>
                          )}
                        </button>
                      ))}
                    </div>
                  </div>
                )}
              </div>
            )}

            {/* 画质 / 码率切换 */}
            <div className="relative">
              <button
                type="button"
                onClick={() => setActivePanel((p) => (p === 'quality' ? 'none' : 'quality'))}
                className={`flex items-center gap-1.5 rounded-xl px-2.5 py-2 text-xs font-medium transition-colors ${
                  activePanel === 'quality'
                    ? 'bg-amber-500 text-zinc-950'
                    : qualityPreset !== 'auto'
                      ? 'bg-amber-500/20 text-amber-300 ring-1 ring-amber-400/40'
                      : 'bg-white/10 text-zinc-200 hover:bg-white/20'
                }`}
                title={t('playerUi.switchQualityTitle')}
              >
                <Sparkles className="h-4 w-4" />
                <span>
                  {(() => {
                    const activePreset =
                      QUALITY_PRESETS.find((q) => q.id === qualityPreset) || QUALITY_PRESETS[0]
                    return activePreset.shortLabelKey
                      ? t(activePreset.shortLabelKey)
                      : activePreset.shortLabel
                  })()}
                </span>
              </button>

              {activePanel === 'quality' && (
                <div className="absolute bottom-12 right-0 z-30 w-52 overflow-hidden rounded-2xl border border-white/15 bg-zinc-950/95 p-2 shadow-2xl backdrop-blur-xl">
                  <p className="px-2.5 py-1.5 text-[11px] font-semibold text-zinc-400">
                    {t('playerUi.qualityBitrateLimitHeader')}
                  </p>
                  <div className="space-y-1">
                    {QUALITY_PRESETS.map((preset) => (
                      <button
                        key={preset.id}
                        type="button"
                        onClick={() => handleSelectQuality(preset.id)}
                        className={`flex w-full items-center justify-between rounded-xl px-2.5 py-2 text-left text-xs transition-colors ${
                          qualityPreset === preset.id
                            ? 'bg-amber-500/20 font-semibold text-amber-300'
                            : 'text-zinc-200 hover:bg-white/8'
                        }`}
                      >
                        <span>{preset.labelKey ? t(preset.labelKey) : preset.label}</span>
                      </button>
                    ))}
                  </div>
                </div>
              )}
            </div>

            {/* 音轨切换 */}
            {audioStreams.length > 0 && (
              <div className="relative">
                <button
                  type="button"
                  onClick={() => setActivePanel((p) => (p === 'audio' ? 'none' : 'audio'))}
                  className={`flex items-center gap-1.5 rounded-xl px-3 py-2 text-xs font-medium transition-colors ${
                    activePanel === 'audio'
                      ? 'bg-amber-500 text-zinc-950'
                      : 'bg-white/10 text-zinc-200 hover:bg-white/20'
                  }`}
                  title={t('playerUi.switchAudioTitle')}
                >
                  <Volume2 className="h-4 w-4" />
                  <span className="hidden md:inline">{t('player.audio')}</span>
                </button>

                {activePanel === 'audio' && (
                  <div className="absolute bottom-12 right-0 z-30 w-64 overflow-hidden rounded-2xl border border-white/15 bg-zinc-950/95 p-2 shadow-2xl backdrop-blur-xl">
                    <p className="px-2.5 py-1.5 text-[11px] font-semibold text-zinc-400">
                      {t('playerUi.selectAudioHeader')}
                    </p>
                    <div className="max-h-60 space-y-1 overflow-y-auto">
                      {audioStreams.map((a) => (
                        <button
                          key={a.Index}
                          type="button"
                          onClick={() => {
                            selectAudioTrack(a.Index)
                            setActivePanel('none')
                          }}
                          className={`flex w-full items-center justify-between rounded-xl px-2.5 py-2 text-left text-xs transition-colors ${
                            a.Index === effectiveAudioIndex
                              ? 'bg-amber-500/20 font-semibold text-amber-300'
                              : 'text-zinc-200 hover:bg-white/8'
                          }`}
                        >
                          <span className="truncate">{getAudioLabel(a)}</span>
                        </button>
                      ))}
                    </div>
                  </div>
                )}
              </div>
            )}

            {/* 字幕切换 */}
            <div className="relative">
              <button
                type="button"
                onClick={() => setActivePanel((p) => (p === 'subtitle' ? 'none' : 'subtitle'))}
                className={`flex items-center gap-1.5 rounded-xl px-3 py-2 text-xs font-medium transition-colors ${
                  activePanel === 'subtitle' || subtitleIndex !== undefined
                    ? 'bg-amber-500/20 text-amber-300 ring-1 ring-amber-400/40'
                    : 'bg-white/10 text-zinc-200 hover:bg-white/20'
                }`}
                title={t('playerUi.switchSubtitleTitle')}
              >
                <Subtitles className="h-4 w-4" />
                <span className="hidden md:inline">
                  {subtitleIndex !== undefined
                    ? activeSubtitleMeta.formatBadge || t('player.subtitles')
                    : t('player.subtitles')}
                </span>
              </button>

              {activePanel === 'subtitle' && (
                <div className="absolute bottom-12 right-0 z-30 w-80 overflow-hidden rounded-2xl border border-white/15 bg-zinc-950/95 p-2 shadow-2xl backdrop-blur-xl">
                  <p className="px-2.5 py-1.5 text-[11px] font-semibold text-zinc-400">
                    {t('playerUi.clientDirectSubtitlesHeader')}
                  </p>
                  <div className="max-h-72 space-y-1 overflow-y-auto">
                    <button
                      type="button"
                      onClick={() => {
                        selectSubtitleTrack(undefined)
                        setActivePanel('none')
                      }}
                      className={`flex w-full items-center justify-between rounded-xl px-2.5 py-2 text-left text-xs transition-colors ${
                        subtitleIndex === undefined
                          ? 'bg-amber-500/20 font-semibold text-amber-300'
                          : 'text-zinc-200 hover:bg-white/8'
                      }`}
                    >
                      <span>{t('player.subtitleOff')}</span>
                    </button>
                    {subtitleStreams.map((s) => {
                      const meta = getSubtitleMeta(s)
                      return (
                        <button
                          key={s.Index}
                          type="button"
                          onClick={() => {
                            selectSubtitleTrack(s.Index)
                            setActivePanel('none')
                          }}
                          className={`flex w-full items-center justify-between gap-2 rounded-xl px-2.5 py-2 text-left text-xs transition-colors ${
                            s.Index === subtitleIndex
                              ? 'bg-amber-500/20 font-semibold text-amber-300'
                              : 'text-zinc-200 hover:bg-white/8'
                          }`}
                        >
                          <span className="truncate">{meta.displayLabel}</span>
                          <span className="shrink-0 rounded bg-white/10 px-1.5 py-0.5 text-[10px] text-zinc-300">
                            {meta.engineLabel}
                          </span>
                        </button>
                      )
                    })}
                  </div>
                  <div className="mt-1.5 border-t border-white/10 pt-1.5">
                    <button
                      type="button"
                      data-testid="open-remote-subtitle-modal"
                      onClick={() => {
                        setActivePanel('none')
                        setRemoteSubModalOpen(true)
                      }}
                      className="flex w-full items-center justify-center gap-1.5 rounded-xl border border-amber-500/30 bg-amber-500/12 px-2.5 py-2 text-xs font-medium text-amber-300 transition-colors hover:bg-amber-500/20"
                    >
                      <Search className="h-3.5 w-3.5" />
                      <span>{t('itemDetailUi.searchSubtitlesOnlineBtn')}</span>
                    </button>
                  </div>
                </div>
              )}
            </div>

            {/* 字幕微调 */}
            <div className="relative">
              <button
                type="button"
                onClick={() =>
                  setActivePanel((p) => (p === 'subtitlePrefs' ? 'none' : 'subtitlePrefs'))
                }
                className={`flex items-center gap-1.5 rounded-xl px-3 py-2 text-xs font-medium transition-colors ${
                  activePanel === 'subtitlePrefs'
                    ? 'bg-amber-500 text-zinc-950'
                    : subtitlePrefs.offsetSec !== DEFAULT_SUBTITLE_PREFS.offsetSec
                      ? 'bg-amber-500/20 text-amber-300 ring-1 ring-amber-400/40'
                      : 'bg-white/10 text-zinc-200 hover:bg-white/20'
                }`}
                title={t('playerUi.subtitleTweaksTitle')}
              >
                <SlidersHorizontal className="h-4 w-4" />
                <span className="hidden lg:inline">{t('player.subtitleTweaks')}</span>
              </button>

              {activePanel === 'subtitlePrefs' && (
                <div className="absolute bottom-12 right-0 z-30 w-80 overflow-hidden rounded-2xl border border-white/15 bg-zinc-950/95 p-3.5 shadow-2xl backdrop-blur-xl">
                  <div className="mb-3 flex items-center justify-between">
                    <div>
                      <p className="text-xs font-semibold text-white">{t('player.subtitleTweaks')}</p>
                      <p className="text-[10px] text-zinc-400">
                        {t('playerUi.subtitleTweaksDesc')}
                      </p>
                    </div>
                    <button
                      type="button"
                      onClick={handleResetSubtitlePrefs}
                      className="rounded-lg border border-white/15 bg-white/5 px-2.5 py-1 text-[11px] font-medium text-zinc-300 transition-colors hover:bg-white/15 hover:text-white"
                    >
                      {t('playerUi.resetBtn')}
                    </button>
                  </div>

                  <div className="space-y-3.5">
                    {/* 时间轴偏移 */}
                    <div>
                      <div className="mb-1.5 flex items-center justify-between text-[11px]">
                        <span className="text-zinc-300">{t('playerUi.timelineOffsetLabel')}</span>
                        <span className="font-mono font-semibold text-amber-300">
                          {formatSubtitleOffset(subtitlePrefs.offsetSec)}
                        </span>
                      </div>
                      <div className="mb-2 grid grid-cols-3 gap-1.5">
                        <button
                          type="button"
                          onClick={() =>
                            updateSubtitlePrefs({
                              offsetSec: stepSubtitleOffset(subtitlePrefs.offsetSec, -0.5),
                            })
                          }
                          className="rounded-lg bg-white/10 py-1.5 font-mono text-xs font-medium text-zinc-100 transition-colors hover:bg-white/20"
                        >
                          -0.5s
                        </button>
                        <button
                          type="button"
                          onClick={() => updateSubtitlePrefs({ offsetSec: 0 })}
                          className={`rounded-lg py-1.5 font-mono text-xs font-medium transition-colors ${
                            subtitlePrefs.offsetSec === 0
                              ? 'bg-amber-500/20 text-amber-300 ring-1 ring-amber-400/40'
                              : 'bg-white/10 text-zinc-200 hover:bg-white/20'
                          }`}
                        >
                          0s
                        </button>
                        <button
                          type="button"
                          onClick={() =>
                            updateSubtitlePrefs({
                              offsetSec: stepSubtitleOffset(subtitlePrefs.offsetSec, 0.5),
                            })
                          }
                          className="rounded-lg bg-white/10 py-1.5 font-mono text-xs font-medium text-zinc-100 transition-colors hover:bg-white/20"
                        >
                          +0.5s
                        </button>
                      </div>
                      <input
                        type="range"
                        min={SUBTITLE_PREFS_LIMITS.offsetSec.min}
                        max={SUBTITLE_PREFS_LIMITS.offsetSec.max}
                        step={SUBTITLE_PREFS_LIMITS.offsetSec.step}
                        value={subtitlePrefs.offsetSec}
                        onChange={(e) =>
                          updateSubtitlePrefs({ offsetSec: Number(e.target.value) })
                        }
                        className="ambra-range h-1.5 w-full rounded-full bg-white/20"
                      />
                    </div>

                    {/* 字号缩放 */}
                    <div>
                      <div className="mb-1 flex items-center justify-between text-[11px]">
                        <span className="text-zinc-300">{t('playerUi.fontScaleLabel')}</span>
                        <span className="font-mono text-zinc-200">
                          {Math.round(subtitlePrefs.fontScale * 100)}%
                        </span>
                      </div>
                      <input
                        type="range"
                        min={SUBTITLE_PREFS_LIMITS.fontScale.min}
                        max={SUBTITLE_PREFS_LIMITS.fontScale.max}
                        step={SUBTITLE_PREFS_LIMITS.fontScale.step}
                        value={subtitlePrefs.fontScale}
                        onChange={(e) =>
                          updateSubtitlePrefs({ fontScale: Number(e.target.value) })
                        }
                        className="ambra-range h-1.5 w-full rounded-full bg-white/20"
                      />
                    </div>

                    {/* 底边距 */}
                    <div>
                      <div className="mb-1 flex items-center justify-between text-[11px]">
                        <span className="text-zinc-300">{t('playerUi.bottomMarginLabel')}</span>
                        <span className="font-mono text-zinc-200">
                          {subtitlePrefs.bottomMarginPx}px
                        </span>
                      </div>
                      <input
                        type="range"
                        min={SUBTITLE_PREFS_LIMITS.bottomMarginPx.min}
                        max={SUBTITLE_PREFS_LIMITS.bottomMarginPx.max}
                        step={SUBTITLE_PREFS_LIMITS.bottomMarginPx.step}
                        value={subtitlePrefs.bottomMarginPx}
                        onChange={(e) =>
                          updateSubtitlePrefs({ bottomMarginPx: Number(e.target.value) })
                        }
                        className="ambra-range h-1.5 w-full rounded-full bg-white/20"
                      />
                    </div>

                    {/* 背景透明度 */}
                    <div>
                      <div className="mb-1 flex items-center justify-between text-[11px]">
                        <span className="text-zinc-300">{t('playerUi.bgOpacityLabel')}</span>
                        <span className="font-mono text-zinc-200">
                          {Math.round(subtitlePrefs.bgOpacity * 100)}%
                        </span>
                      </div>
                      <input
                        type="range"
                        min={SUBTITLE_PREFS_LIMITS.bgOpacity.min}
                        max={SUBTITLE_PREFS_LIMITS.bgOpacity.max}
                        step={SUBTITLE_PREFS_LIMITS.bgOpacity.step}
                        value={subtitlePrefs.bgOpacity}
                        onChange={(e) =>
                          updateSubtitlePrefs({ bgOpacity: Number(e.target.value) })
                        }
                        className="ambra-range h-1.5 w-full rounded-full bg-white/20"
                      />
                    </div>
                  </div>
                </div>
              )}
            </div>

            {/* 倍速 */}
            <div className="relative">
              <button
                type="button"
                onClick={() => setActivePanel((p) => (p === 'speed' ? 'none' : 'speed'))}
                className="flex items-center gap-1 rounded-xl bg-white/10 px-2.5 py-2 text-xs font-medium text-zinc-200 hover:bg-white/20"
              >
                <Gauge className="h-4 w-4" />
                <span>{playbackRate}x</span>
              </button>

              {activePanel === 'speed' && (
                <div className="absolute bottom-12 right-0 z-30 w-28 overflow-hidden rounded-2xl border border-white/15 bg-zinc-950/95 p-1.5 shadow-2xl backdrop-blur-xl">
                  {[0.75, 1, 1.25, 1.5, 2].map((r) => (
                    <button
                      key={r}
                      type="button"
                      onClick={() => {
                        setPlaybackRate(r)
                        if (videoRef.current) videoRef.current.playbackRate = r
                        if (hybridAudioRef.current) hybridAudioRef.current.playbackRate = r
                        setActivePanel('none')
                      }}
                      className={`block w-full rounded-xl px-3 py-1.5 text-center text-xs ${
                        playbackRate === r
                          ? 'bg-amber-500/20 font-semibold text-amber-300'
                          : 'text-zinc-200 hover:bg-white/10'
                      }`}
                    >
                      {r}x
                    </button>
                  ))}
                </div>
              )}
            </div>

            {/* 播放器设置 */}
            <div className="relative">
              <button
                type="button"
                onClick={() => setActivePanel((p) => (p === 'settings' ? 'none' : 'settings'))}
                className={`rounded-xl p-2 transition-colors ${
                  activePanel === 'settings'
                    ? 'bg-amber-500 text-zinc-950'
                    : 'bg-white/10 text-zinc-200 hover:bg-white/20'
                }`}
                title={t('playerUi.playerSettingsTitle')}
              >
                <Settings className="h-4 w-4" />
              </button>

              {activePanel === 'settings' && (
                <div className="absolute bottom-12 right-0 z-30 w-80 overflow-hidden rounded-2xl border border-white/15 bg-zinc-950/95 p-3 shadow-2xl backdrop-blur-xl">
                  <p className="mb-2 text-[11px] font-semibold text-zinc-400">
                    {t('playerUi.engineSettingsHeader')}
                  </p>

                  <button
                    type="button"
                    role="switch"
                    aria-checked={preferWasmRemux}
                    onClick={togglePreferWasmRemux}
                    disabled={!wasmSupported}
                    className="flex w-full items-start justify-between gap-3 rounded-xl bg-white/5 p-2.5 text-left transition-colors hover:bg-white/10 disabled:cursor-not-allowed disabled:opacity-50"
                  >
                    <div className="min-w-0">
                      <div className="text-xs font-medium text-zinc-100">
                        {t('playerUi.preferWasmToggleTitle')}
                      </div>
                      <p className="mt-1 text-[11px] leading-relaxed text-zinc-400">
                        {wasmSupported
                          ? t('playerUi.preferWasmSupportedDesc', {
                              max: formatBytes(DEFAULT_WASM_MAX_BYTES),
                            })
                          : t('playerUi.preferWasmUnsupportedDesc')}
                      </p>
                    </div>
                    <span
                      className={`mt-0.5 inline-flex h-5 w-9 shrink-0 items-center rounded-full p-0.5 transition-colors ${
                        preferWasmRemux && wasmSupported ? 'bg-amber-500' : 'bg-zinc-700'
                      }`}
                    >
                      <span
                        className={`h-4 w-4 rounded-full bg-white shadow transition-transform ${
                          preferWasmRemux && wasmSupported ? 'translate-x-4' : 'translate-x-0'
                        }`}
                      />
                    </span>
                  </button>

                  <div className="mt-2.5 flex items-center justify-between rounded-xl border border-white/8 bg-black/40 px-2.5 py-2 text-[11px] text-zinc-400">
                    <span>{t('playerUi.runtimeEnvLabel')}</span>
                    <span className="font-mono text-zinc-300">
                      {!wasmSupported
                        ? t('playerUi.runtimeWasmUnavailable')
                        : sabSupported
                          ? t('playerUi.runtimeWasmReadyCoi')
                          : t('playerUi.runtimeSingleThreadLan')}
                    </span>
                  </div>
                </div>
              )}
            </div>

            {/* 画面填充比例切换 (适应 / 铺满) */}
            <button
              type="button"
              onClick={() => {
                setAspectMode((prev) => (prev === 'contain' ? 'cover' : 'contain'))
                pokeOsd()
              }}
              className={`flex items-center gap-1 rounded-xl px-2.5 py-2 text-xs font-medium transition-colors ${
                aspectMode === 'cover'
                  ? 'bg-amber-500/20 text-amber-300 ring-1 ring-amber-400/40'
                  : 'bg-white/10 text-zinc-200 hover:bg-white/20'
              }`}
              title={
                aspectMode === 'contain'
                  ? t('playerUi.aspectContainTitle')
                  : t('playerUi.aspectCoverTitle')
              }
              aria-label={t('playerUi.toggleAspectAria')}
              aria-pressed={aspectMode === 'cover'}
            >
              <Maximize2 className="h-4 w-4" />
              <span className="hidden lg:inline">
                {aspectMode === 'cover' ? t('playerUi.aspectFillShort') : t('playerUi.aspectFitShort')}
              </span>
            </button>

            {/* 画中画 (PiP) */}
            {pipSupported && (
              <button
                type="button"
                onClick={togglePip}
                className={`rounded-xl p-2 transition-colors ${
                  isPip
                    ? 'bg-amber-500 text-zinc-950'
                    : 'bg-white/10 text-zinc-200 hover:bg-white/20'
                }`}
                title={t('playerUi.pipTitle')}
                aria-label={t('playerUi.pipAria')}
              >
                <PictureInPicture2 className="h-4 w-4" />
              </button>
            )}

            {/* 全屏 */}
            <button
              type="button"
              onClick={toggleFullscreen}
              className="rounded-xl bg-white/10 p-2 text-zinc-200 hover:bg-white/20"
              title={t('playerUi.fullscreenTitle')}
            >
              {isFullscreen ? <Minimize className="h-4 w-4" /> : <Maximize className="h-4 w-4" />}
            </button>
          </div>
        </div>
      </div>

      <RemoteSubtitleModal
        open={remoteSubModalOpen}
        itemId={id || ''}
        itemName={item?.Name}
        onClose={() => setRemoteSubModalOpen(false)}
        onDownloaded={() => {
          void refetchPlaybackInfo()
        }}
      />
    </div>
  )
}

function PlayMethodBadge({ method }: { method: PlayMethodMode }) {
  const styleClass =
    method === 'DirectPlay'
      ? 'border-emerald-400/35 bg-emerald-500/20 text-emerald-300'
      : method === 'Proxy Direct'
        ? 'border-teal-400/35 bg-teal-500/20 text-teal-300'
        : method === 'Stream MKV (MSE)'
          ? 'border-violet-400/40 bg-violet-500/20 text-violet-200'
          : method === 'Client WASM Remux'
            ? 'border-amber-400/40 bg-amber-500/20 text-amber-300'
            : 'border-sky-400/35 bg-sky-500/20 text-sky-300'

  return (
    <span
      className={`inline-flex items-center gap-1 rounded border px-1.5 py-0.5 font-mono text-[11px] font-semibold ${styleClass}`}
    >
      {(method === 'Client WASM Remux' || method === 'Stream MKV (MSE)') && (
        <Cpu className="h-3 w-3" />
      )}
      {method}
    </span>
  )
}

function StatRow({
  label,
  value,
  highlight = false,
}: {
  label: string
  value: string
  highlight?: boolean
}) {
  return (
    <div className="flex items-center justify-between gap-3">
      <dt className="text-zinc-400">{label}</dt>
      <dd className={`truncate font-semibold ${highlight ? 'text-amber-300' : 'text-zinc-100'}`}>
        {value}
      </dd>
    </div>
  )
}
