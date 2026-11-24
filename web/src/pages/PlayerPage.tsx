import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import Hls from 'hls.js'
import {
  Activity,
  AlertTriangle,
  ArrowLeft,
  Cpu,
  Gauge,
  Loader2,
  Maximize,
  Minimize,
  Pause,
  Play,
  RotateCcw,
  RotateCw,
  Settings,
  SkipBack,
  SkipForward,
  SlidersHorizontal,
  Subtitles,
  Volume2,
  VolumeX,
  X,
} from 'lucide-react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { api, buildEmbyAuthHeader, getStoredAuth, resolveApiUrl } from '../api/client'
import type { MediaSourceInfo, MediaStream } from '../api/types'
import { AmbraLogo } from '../components/AmbraLogo'
import { useAuth } from '../context/AuthContext'
import { isDemoMode } from '../demo/demoMode'
import { AssJassubRenderer } from '../player/assRenderer'
import { PgsCanvasRenderer } from '../player/pgsRenderer'
import { loadPlaybackPrefs } from '../player/playbackPrefs'
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

type ActivePanel = 'none' | 'subtitle' | 'subtitlePrefs' | 'audio' | 'speed' | 'settings'
type PlayMethodMode =
  | 'DirectPlay'
  | 'Proxy Direct'
  | 'Stream MKV (MSE)'
  | 'Client WASM Remux'
  | 'Server HLS'

function isStrmSource(source?: MediaSourceInfo): boolean {
  if (!source) return false
  if (source.IsRemote) return true
  if ((source.Container || '').trim().toLowerCase() === 'strm') return true
  if ((source.Protocol || '').trim().toLowerCase() === 'http') return true
  if ((source.Path || '').trim().toLowerCase().endsWith('.strm')) return true
  return false
}

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
  const userId = auth!.userId

  const initialMediaSourceId = searchParams.get('mediaSourceId') || undefined
  const initialAudio = searchParams.get('audio') ? Number(searchParams.get('audio')) : undefined
  const initialSubtitle = searchParams.get('subtitle')
    ? Number(searchParams.get('subtitle'))
    : undefined
  const initialStartTicks = searchParams.get('startTicks')
    ? Number(searchParams.get('startTicks'))
    : 0

  const containerRef = useRef<HTMLDivElement>(null)
  const videoRef = useRef<HTMLVideoElement>(null)
  const assCanvasRef = useRef<HTMLCanvasElement>(null)
  const pgsCanvasRef = useRef<HTMLCanvasElement>(null)

  const hlsRef = useRef<Hls | null>(null)
  const streamMkvRef = useRef<StreamMkvRemuxer | null>(null)
  const wasmRemuxerRef = useRef<WasmRemuxer | null>(null)
  const assRendererRef = useRef<AssJassubRenderer | null>(null)
  const pgsRendererRef = useRef<PgsCanvasRenderer | null>(null)
  const hideOsdTimerRef = useRef<number | null>(null)
  const initialSeekDoneRef = useRef(false)
  const initializedSubSourceIdRef = useRef<string | null>(null)
  const lastSeekReportRef = useRef(0)

  const [selectedSourceId, setSelectedSourceId] = useState<string | undefined>(initialMediaSourceId)
  const [audioIndex, setAudioIndex] = useState<number | undefined>(initialAudio)
  const [subtitleIndex, setSubtitleIndex] = useState<number | undefined>(initialSubtitle)

  const [playing, setPlaying] = useState(false)
  const [buffering, setBuffering] = useState(true)
  const [currentTime, setCurrentTime] = useState(0)
  const [duration, setDuration] = useState(0)
  const [bufferedEnd, setBufferedEnd] = useState(0)
  const [volume, setVolume] = useState(1)
  const [muted, setMuted] = useState(false)
  const [playbackRate, setPlaybackRate] = useState(1)
  const [isFullscreen, setIsFullscreen] = useState(false)
  const [osdVisible, setOsdVisible] = useState(true)
  const [activePanel, setActivePanel] = useState<ActivePanel>('none')
  const [showStats, setShowStats] = useState(false)
  const [playerError, setPlayerError] = useState<string | null>(null)
  const [subtitleError, setSubtitleError] = useState<string | null>(null)
  const [playMethod, setPlayMethod] = useState<PlayMethodMode>('DirectPlay')
  const [nextUpCountdown, setNextUpCountdown] = useState<number | null>(null)

  // 客户端 WASM 解复用偏好与运行态
  const [preferWasmRemux, setPreferWasmRemuxState] = useState<boolean>(() => getPreferWasmRemux())
  const [useProxyDirect, setUseProxyDirect] = useState(false)
  const [forceServerHls, setForceServerHls] = useState(false)
  const [wasmProgress, setWasmProgress] = useState<WasmProgressState | null>(null)
  const [wasmFallbackNotice, setWasmFallbackNotice] = useState<string | null>(null)
  const wasmSupported = useMemo(() => canUseWasmRemux(), [])
  const sabSupported = useMemo(() => hasSharedArrayBufferSupport(), [])

  const togglePreferWasmRemux = useCallback(() => {
    setPreferWasmRemuxState((prev) => {
      const next = !prev
      setPreferWasmRemux(next)
      setUseProxyDirect(false)
      setForceServerHls(false)
      setWasmFallbackNotice(null)
      return next
    })
  }, [])

  // 切换视频条目时重置初始化起播定位与默认字幕初始化标记
  useEffect(() => {
    initialSeekDoneRef.current = false
    initializedSubSourceIdRef.current = null
    setNextUpCountdown(null)
  }, [id])

  // 切换视频或音轨时重置强制回退状态
  useEffect(() => {
    setUseProxyDirect(false)
    setForceServerHls(false)
    setWasmFallbackNotice(null)
  }, [id, selectedSourceId, audioIndex])

  // WebVTT / SRT 文本字幕状态 & 字幕微调偏好
  const [textCues, setTextCues] = useState<TextSubtitleCue[]>([])
  const [subtitlePrefs, setSubtitlePrefs] = useState<SubtitlePrefs>(() => loadSubtitlePrefs())
  const subtitlePrefsRef = useRef<SubtitlePrefs>(subtitlePrefs)
  subtitlePrefsRef.current = subtitlePrefs
  const [loadedFontCount, setLoadedFontCount] = useState(0)

  const updateSubtitlePrefs = useCallback((patch: Partial<SubtitlePrefs>) => {
    setSubtitlePrefs((prev) => saveSubtitlePrefs({ ...prev, ...patch }))
  }, [])

  const handleResetSubtitlePrefs = useCallback(() => {
    setSubtitlePrefs(resetSubtitlePrefs())
  }, [])

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
  const nextEpisode =
    currentEpisodeIdx >= 0 && currentEpisodeIdx + 1 < seasonEpisodes.length
      ? seasonEpisodes[currentEpisodeIdx + 1]
      : null

  useEffect(() => {
    if (nextUpCountdown === null || !nextEpisode) return
    if (nextUpCountdown <= 0) {
      setNextUpCountdown(null)
      navigate(`/play/${nextEpisode.Id}`, { replace: true })
      return
    }
    const timer = window.setTimeout(() => {
      setNextUpCountdown((c) => (c === null ? null : c - 1))
    }, 1000)
    return () => window.clearTimeout(timer)
  }, [nextUpCountdown, nextEpisode, navigate])

  const {
    data: playbackInfo,
    isLoading: infoLoading,
    error: infoError,
  } = useQuery({
    queryKey: ['playbackInfo', id, userId, selectedSourceId, audioIndex],
    queryFn: () =>
      api.getPlaybackInfo(id!, userId, {
        mediaSourceId: selectedSourceId,
        audioStreamIndex: audioIndex,
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

  const streams = activeSource?.MediaStreams || []
  const videoStream = streams.find((s) => s.Type === 'Video')
  const audioStreams = streams.filter((s) => s.Type === 'Audio')
  const subtitleStreams = streams.filter((s) => s.Type === 'Subtitle')

  // 初始化默认音轨与字幕轨（每个媒体源仅初始化一次默认字幕，避免用户关闭字幕后回弹）
  useEffect(() => {
    if (!activeSource) return
    if (selectedSourceId === undefined) setSelectedSourceId(activeSource.Id)
    if (audioIndex === undefined && activeSource.DefaultAudioStreamIndex !== undefined) {
      setAudioIndex(activeSource.DefaultAudioStreamIndex)
    }
    if (initializedSubSourceIdRef.current !== activeSource.Id) {
      initializedSubSourceIdRef.current = activeSource.Id
      if (subtitleIndex === undefined && activeSource.DefaultSubtitleStreamIndex !== undefined) {
        setSubtitleIndex(activeSource.DefaultSubtitleStreamIndex)
      }
    }
  }, [activeSource, selectedSourceId, audioIndex, subtitleIndex])

  // 计算生效的起播位置（优先 URL startTicks，否则从 item.UserData.PlaybackPositionTicks 自动续播）
  const effectiveStartTicks = useMemo(() => {
    if (Number.isFinite(initialStartTicks) && initialStartTicks > 0) {
      return initialStartTicks
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
    initialStartTicks,
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
    () => audioStreams.find((a) => a.Index === audioIndex) || audioStreams[0],
    [audioStreams, audioIndex],
  )

  // 播放策略规划：
  // 1) 原生兼容容器 (mp4/m4v/webm/mov) 且音轨原生兼容 -> DirectPlay
  // 2) 非原生 MKV 且浏览器 MSE 支持对应音视频编码 -> Stream MKV (MSE)（Range 分块流式解复用，支持 GB 级大文件）
  // 3) 其他非原生容器 (ts/avi/flv 等) 或 AC3/EAC3/DTS 音轨：
  //    - 若开启客户端解复用 + 浏览器支持 WASM + 文件体积 <= 150MB -> Client WASM Remux
  //    - 否则 / 超时 / 超阈值 / 出错 -> 自动无感回退服务端 HLS (master.m3u8)
  // 4) 字幕始终走客户端 JASSUB / PGS Canvas / WebVTT，绝不因字幕触发转码
  const streamPlan = useMemo(() => {
    if (!activeSource || !id) return null

    const staticParams = new URLSearchParams({
      Static: 'true',
      MediaSourceId: activeSource.Id,
    })
    if (audioIndex !== undefined) staticParams.set('AudioStreamIndex', String(audioIndex))
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

    const hlsParams = new URLSearchParams({
      MediaSourceId: activeSource.Id,
    })
    if (playbackInfo?.PlaySessionId) hlsParams.set('PlaySessionId', playbackInfo.PlaySessionId)
    if (audioIndex !== undefined) hlsParams.set('AudioStreamIndex', String(audioIndex))
    const fallbackMasterHlsUrl = resolveApiUrl(
      `/emby/Videos/${encodeURIComponent(id)}/master.m3u8?${hlsParams.toString()}`,
    )

    const hlsUrl = activeSource.TranscodingUrl
      ? resolveApiUrl(activeSource.TranscodingUrl)
      : activeSource.DirectStreamUrl?.includes('.m3u8')
        ? resolveApiUrl(activeSource.DirectStreamUrl)
        : fallbackMasterHlsUrl

    const requiresRemux = needsClientRemux(activeSource.Container, activeAudioStream?.Codec)
    const sizeBytes = activeSource.Size
    const withinWasmThreshold = sizeBytes === undefined || sizeBytes <= DEFAULT_WASM_MAX_BYTES
    const mkvStreamEligible =
      isMkvContainer(activeSource.Container) &&
      canStreamMkvRemux(videoStream?.Codec, activeAudioStream?.Codec, activeSource.Container)

    if (forceServerHls) {
      return {
        strategy: 'hls' as const,
        directUrl,
        proxyDirectUrl,
        hlsUrl,
        isStrm,
        requiresRemux,
        withinWasmThreshold,
        skipReason: null as string | null,
      }
    }

    // 0. STRM 直连失败后，先尝试服务端反代直出 (&proxy=1，支持 Range)
    if (useProxyDirect && isStrm) {
      return {
        strategy: 'proxy-direct' as const,
        directUrl,
        proxyDirectUrl,
        hlsUrl,
        isStrm,
        requiresRemux,
        withinWasmThreshold,
        skipReason: null as string | null,
      }
    }

    // 1. 原生兼容容器 + 原生兼容音轨 -> DirectPlay
    if (!requiresRemux && activeSource.SupportsDirectPlay !== false) {
      return {
        strategy: 'direct' as const,
        directUrl,
        proxyDirectUrl,
        hlsUrl,
        isStrm,
        requiresRemux,
        withinWasmThreshold,
        skipReason: null as string | null,
      }
    }

    // 2. 非原生 MKV 且浏览器 MSE 支持该音视频编码 -> 优先流式 Range + MSE（无体积上限，支持 GB 级 MKV）
    if (requiresRemux && preferWasmRemux && mkvStreamEligible) {
      return {
        strategy: 'stream-mkv' as const,
        directUrl,
        proxyDirectUrl,
        hlsUrl,
        isStrm,
        requiresRemux,
        withinWasmThreshold,
        skipReason: null as string | null,
      }
    }

    // 3. 其他非原生容器 / 需音频转码（AC3/DTS 等）-> 尝试客户端 ffmpeg.wasm（<= 150MB）
    if (
      requiresRemux &&
      preferWasmRemux &&
      canUseWasmRemux({ enabled: preferWasmRemux }) &&
      withinWasmThreshold
    ) {
      return {
        strategy: 'wasm' as const,
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
        skipReason = '已关闭客户端解复用开关'
      } else if (!canUseWasmRemux()) {
        skipReason = '当前浏览器不支持客户端流式解复用 / WebAssembly'
      } else if (!withinWasmThreshold && sizeBytes !== undefined) {
        skipReason = `文件体积 ${formatBytes(sizeBytes)} 超过 WASM 阈值 (${formatBytes(DEFAULT_WASM_MAX_BYTES)}) 且音视频编码不支持 MSE 直通`
      }
    }

    return {
      strategy: 'hls' as const,
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
    audioIndex,
    playbackInfo?.PlaySessionId,
    videoStream?.Codec,
    activeAudioStream?.Codec,
    preferWasmRemux,
    useProxyDirect,
    forceServerHls,
  ])

  // 挂载视频流 (DirectPlay / Stream MKV (MSE) / Client WASM Remux / Server HLS 自动降级)
  useEffect(() => {
    const video = videoRef.current
    if (!video || !streamPlan) return

    let cancelled = false
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

    const startPlayback = () => {
      if (cancelled) return
      const targetStartTicks = effectiveStartTicksRef.current
      if (!initialSeekDoneRef.current && targetStartTicks > 0) {
        const startSec = targetStartTicks / 10_000_000
        if (Number.isFinite(startSec) && startSec > 0) {
          video.currentTime = startSec
        }
        initialSeekDoneRef.current = true
      }
      video.play().catch(() => {
        // 浏览器自动播放策略阻止时保持暂停状态让用户点击
      })
    }

    const buildAuthHeaders = (): Record<string, string> => {
      const storedAuth = getStoredAuth()
      const headers: Record<string, string> = {
        'X-Emby-Authorization': buildEmbyAuthHeader(storedAuth?.accessToken),
      }
      if (storedAuth?.accessToken) {
        headers['X-Emby-Token'] = storedAuth.accessToken
      }
      return headers
    }

    const mountServerHls = (url: string) => {
      if (cancelled) return
      setPlayMethod('Server HLS')
      setWasmProgress(null)
      setBuffering(true)

      if (hlsRef.current) {
        hlsRef.current.destroy()
        hlsRef.current = null
      }

      if (Hls.isSupported()) {
        const hls = new Hls({
          enableWorker: true,
          lowLatencyMode: false,
        })
        hlsRef.current = hls
        hls.loadSource(url)
        hls.attachMedia(video)
        hls.on(Hls.Events.MANIFEST_PARSED, startPlayback)
        hls.on(Hls.Events.ERROR, (_evt, data) => {
          if (data.fatal && !cancelled) {
            setPlayerError(`HLS 播放错误: ${data.details}`)
          }
        })
      } else if (video.canPlayType('application/vnd.apple.mpegurl')) {
        video.src = url
        video.addEventListener('loadedmetadata', startPlayback, { once: true })
      } else {
        setPlayerError('当前浏览器不支持 HLS 流播放')
      }
    }

    const mountWasmRemux = () => {
      if (cancelled) return
      setPlayMethod('Client WASM Remux')
      setWasmProgress({
        stage: 'fetch',
        percent: 0,
        detail: '正在准备客户端 WASM 零重编码转封装...',
      })

      const remuxer = new WasmRemuxer()
      wasmRemuxerRef.current = remuxer

      void remuxer
        .remuxToMP4(streamPlan.directUrl, buildAuthHeaders(), {
          maxBytes: DEFAULT_WASM_MAX_BYTES,
          knownSizeBytes: activeSource?.Size,
          container: activeSource?.Container,
          signal: abortController.signal,
          onProgress: (stage, percent, detail) => {
            if (cancelled) return
            setWasmProgress({
              stage,
              percent,
              detail:
                detail ||
                (stage === 'fetch'
                  ? `正在拉取媒体流 (${percent}%)...`
                  : stage === 'init'
                    ? '正在初始化 FFmpeg WASM 引擎...'
                    : stage === 'remux'
                      ? `视频零重编码转封装中 (${percent}%)...`
                      : '转封装完成'),
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
                : '客户端 WASM 转封装失败'
          // 清理 WASM 内存后无感回退服务端 HLS master.m3u8
          remuxer.dispose()
          if (wasmRemuxerRef.current === remuxer) {
            wasmRemuxerRef.current = null
          }
          setWasmFallbackNotice(`${reasonMsg}，已自动切换服务端 HLS`)
          mountServerHls(streamPlan.hlsUrl)
        })
    }

    const fallbackFromStreamMkv = (reasonMsg: string) => {
      if (cancelled) return
      if (streamMkvRef.current) {
        streamMkvRef.current.dispose()
        streamMkvRef.current = null
      }
      if (streamPlan.withinWasmThreshold && canUseWasmRemux({ enabled: preferWasmRemux })) {
        setWasmFallbackNotice(`${reasonMsg}，已自动回退客户端 WASM 转封装`)
        mountWasmRemux()
      } else {
        setWasmFallbackNotice(`${reasonMsg}，已自动切换服务端 HLS`)
        mountServerHls(streamPlan.hlsUrl)
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
      // 大体积 MKV 流式解复用路径（HTTP Range + 纯 TS EBML/fMP4 + MSE）
      setPlayMethod('Stream MKV (MSE)')
      const streamMkv = new StreamMkvRemuxer()
      streamMkvRef.current = streamMkv

      void streamMkv
        .attach(video, streamPlan.directUrl, buildAuthHeaders(), {
          signal: abortController.signal,
          onError: (err) => {
            fallbackFromStreamMkv(err.message || '流式 MKV 解复用异常')
          },
        })
        .then(() => {
          if (cancelled) return
          setPlayMethod('Stream MKV (MSE)')
          startPlayback()
        })
        .catch((err: unknown) => {
          if (cancelled) return
          const reasonMsg =
            err instanceof StreamMkvFallbackError
              ? err.message
              : err instanceof Error
                ? err.message
                : '流式 MKV 解复用初始化失败'
          fallbackFromStreamMkv(reasonMsg)
        })
    } else {
      // Client WASM Remux 路径：失败/超阈值/超时时自动无感回退服务端 HLS
      mountWasmRemux()
    }

    return () => {
      cancelled = true
      abortController.abort()
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
    }
  }, [
    streamPlan,
    activeSource?.Size,
    activeSource?.Container,
    initialStartTicks,
    useProxyDirect,
    forceServerHls,
    preferWasmRemux,
  ])

  // 构建外挂字幕直出 URL
  const resolveSubtitleUrl = useCallback(
    (stream: MediaStream): string => {
      if (stream.DeliveryUrl) {
        return resolveApiUrl(stream.DeliveryUrl)
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

  // 字幕渲染引擎切换与生命周期管理（JASSUB WASM / PGS Canvas / Native VTT）
  useEffect(() => {
    let cancelled = false
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
      if (cancelled || !activeSubtitleStream) return

      const engine = detectSubtitleEngine(activeSubtitleStream)
      const subUrl = resolveSubtitleUrl(activeSubtitleStream)

      try {
        if (engine === 'ass') {
          if (!videoRef.current || !assCanvasRef.current) return
          const attachments =
            id && activeSource?.Id
              ? await api.getMediaAttachments(id, activeSource.Id)
              : []
          if (cancelled || !videoRef.current || !assCanvasRef.current) return
          const fontUrls = attachments.map((att) => {
            const rawPath =
              att.Url.startsWith('/emby/') || /^https?:\/\//i.test(att.Url)
                ? att.Url
                : `/emby${att.Url.startsWith('/') ? '' : '/'}${att.Url}`
            return resolveApiUrl(rawPath)
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
          const res = await fetch(subUrl)
          if (!res.ok) throw new Error(`加载文本字幕失败 (${res.status})`)
          const raw = await res.text()
          if (!cancelled) {
            setTextCues(parseVttOrSrt(raw))
          }
        }
      } catch (err) {
        if (!cancelled) {
          setSubtitleError(err instanceof Error ? err.message : '字幕加载失败')
        }
      }
    }

    setup()

    return () => {
      cancelled = true
      void cleanupEngines()
    }
  }, [activeSubtitleStream, resolveSubtitleUrl, id, activeSource?.Id])

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
    audioIndex,
    subtitleIndex,
    playMethod,
    mediaSourceId: activeSource?.Id,
    playSessionId: playbackInfo?.PlaySessionId,
    effectiveStartTicks,
  })
  playbackMetaRef.current = {
    audioIndex,
    subtitleIndex,
    playMethod,
    mediaSourceId: activeSource?.Id,
    playSessionId: playbackInfo?.PlaySessionId,
    effectiveStartTicks,
  }

  // 上报播放状态 (/emby/Sessions/Playing, Progress, Stopped)
  const reportState = useCallback(
    (type: 'start' | 'progress' | 'stop', eventName?: string) => {
      if (!id || !videoRef.current) return
      const {
        audioIndex: latestAudioIndex,
        subtitleIndex: latestSubtitleIndex,
        playMethod: latestPlayMethod,
        mediaSourceId: latestMediaSourceId,
        playSessionId: latestPlaySessionId,
        effectiveStartTicks: latestStartTicks,
      } = playbackMetaRef.current
      let posTicks = Math.floor(videoRef.current.currentTime * 10_000_000)
      if (
        type === 'stop' &&
        !initialSeekDoneRef.current &&
        latestStartTicks > 0 &&
        videoRef.current.currentTime < 1
      ) {
        posTicks = latestStartTicks
      }
      const base = {
        ItemId: id,
        MediaSourceId: latestMediaSourceId,
        PlaySessionId: latestPlaySessionId,
        PositionTicks: posTicks,
      }
      if (type === 'stop') {
        void api.reportStopped(base)
        return
      }
      const full = {
        ...base,
        IsPaused: videoRef.current.paused,
        IsMuted: videoRef.current.muted,
        VolumeLevel: Math.round(videoRef.current.volume * 100),
        AudioStreamIndex: latestAudioIndex,
        SubtitleStreamIndex: latestSubtitleIndex,
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
    if (!hasPlaybackInfo) return
    reportState('start')
    const interval = window.setInterval(() => {
      reportState('progress', 'TimeUpdate')
    }, 10_000)

    return () => {
      window.clearInterval(interval)
      reportState('stop')
    }
  }, [hasPlaybackInfo, reportState])

  // 页面关闭或刷新时通过 keepalive 上报最新播放进度，避免丢失续播点
  useEffect(() => {
    if (!hasPlaybackInfo || !id || isDemoMode()) return

    let unloadReported = false
    const sendKeepaliveStop = () => {
      if (unloadReported || !videoRef.current) return
      unloadReported = true
      const storedAuth = getStoredAuth()
      const token = storedAuth?.accessToken
      if (!token) return
      const {
        mediaSourceId: latestMediaSourceId,
        playSessionId: latestPlaySessionId,
        effectiveStartTicks: latestStartTicks,
      } = playbackMetaRef.current
      let posTicks = Math.floor(videoRef.current.currentTime * 10_000_000)
      if (!initialSeekDoneRef.current && latestStartTicks > 0 && videoRef.current.currentTime < 1) {
        posTicks = latestStartTicks
      }
      const headers: Record<string, string> = {
        'Content-Type': 'application/json',
        Accept: 'application/json',
        'X-Emby-Authorization': buildEmbyAuthHeader(token),
        'X-Emby-Token': token,
      }
      try {
        void fetch('/emby/Sessions/Playing/Stopped', {
          method: 'POST',
          headers,
          body: JSON.stringify({
            ItemId: id,
            MediaSourceId: latestMediaSourceId,
            PlaySessionId: latestPlaySessionId,
            PositionTicks: posTicks,
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

  const seekBy = useCallback(
    (delta: number) => {
      const v = videoRef.current
      if (!v) return
      v.currentTime = Math.max(0, Math.min(v.duration || 0, v.currentTime + delta))
      setCurrentTime(v.currentTime)
      reportState('progress', 'timeupdate')
      pokeOsd()
    },
    [pokeOsd, reportState],
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

  const cycleSubtitle = useCallback(() => {
    if (subtitleStreams.length === 0) return
    const indices = [undefined, ...subtitleStreams.map((s) => s.Index)]
    const curPos = indices.indexOf(subtitleIndex)
    const next = indices[(curPos + 1) % indices.length]
    setSubtitleIndex(next)
    pokeOsd()
  }, [subtitleStreams, subtitleIndex, pokeOsd])

  const cycleAudio = useCallback(() => {
    if (audioStreams.length <= 1) return
    const curPos = audioStreams.findIndex((a) => a.Index === audioIndex)
    const next = audioStreams[(curPos + 1) % audioStreams.length]
    setAudioIndex(next.Index)
    pokeOsd()
  }, [audioStreams, audioIndex, pokeOsd])

  const toggleMute = useCallback(() => {
    const v = videoRef.current
    if (!v) return
    const nextMuted = !v.muted && !muted
    if (!nextMuted && v.volume === 0) {
      v.volume = 0.5
      setVolume(0.5)
    }
    v.muted = nextMuted
    setMuted(nextMuted)
    pokeOsd()
  }, [muted, pokeOsd])

  // 监听全屏状态变化
  useEffect(() => {
    const onFsChange = () => {
      setIsFullscreen(Boolean(document.fullscreenElement))
      assRendererRef.current?.resize()
    }
    document.addEventListener('fullscreenchange', onFsChange)
    return () => document.removeEventListener('fullscreenchange', onFsChange)
  }, [])

  // 全局快捷键：Space / 左右 / 上下 / M / F / C / A / I / Esc
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement) return
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
      } else if (key === 'c') {
        e.preventDefault()
        cycleSubtitle()
      } else if (key === 'a') {
        e.preventDefault()
        cycleAudio()
      } else if (key === 'i') {
        e.preventDefault()
        setShowStats((s) => !s)
      } else if (e.key === 'Escape') {
        if (activePanel !== 'none') {
          setActivePanel('none')
        } else if (!document.fullscreenElement) {
          navigate(-1)
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
    cycleSubtitle,
    cycleAudio,
    activePanel,
    navigate,
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

  const progressPct = duration > 0 ? Math.min(100, (currentTime / duration) * 100) : 0
  const bufferedPct = duration > 0 ? Math.min(100, (bufferedEnd / duration) * 100) : 0

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
      {/* 主视频元素 */}
      <video
        ref={videoRef}
        playsInline
        crossOrigin="anonymous"
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
          setPlaying(true)
          setBuffering(false)
          reportState('progress', 'Unpause')
        }}
        onPause={() => {
          setPlaying(false)
          setOsdVisible(true)
          reportState('progress', 'Pause')
        }}
        onWaiting={() => setBuffering(true)}
        onPlaying={() => setBuffering(false)}
        onLoadedMetadata={(e) => {
          setDuration(e.currentTarget.duration || 0)
          setBuffering(false)
        }}
        onTimeUpdate={(e) => {
          const v = e.currentTarget
          setCurrentTime(v.currentTime)
          if (v.buffered.length > 0) {
            setBufferedEnd(v.buffered.end(v.buffered.length - 1))
          }
        }}
        onEnded={() => {
          setPlaying(false)
          setOsdVisible(true)
          reportState('stop')
          if (nextEpisode && loadPlaybackPrefs().autoPlayNextEpisode) {
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
            setWasmFallbackNotice('STRM 直连失败，正在切换服务端代理直出 (&proxy=1)...')
            setUseProxyDirect(true)
            return
          }
          // 2) 若当前为 DirectPlay / Proxy Direct / Stream MKV (MSE) / Client WASM Remux 解码失败，自动无感回退服务端 HLS
          if (playMethod !== 'Server HLS' && !forceServerHls && streamPlan?.hlsUrl) {
            setWasmFallbackNotice(
              playMethod === 'Proxy Direct'
                ? '服务端代理直出解码失败，已自动回退服务端 HLS'
                : playMethod === 'Stream MKV (MSE)'
                  ? '流式 MKV (MSE) 解码异常，已自动回退服务端 HLS'
                  : playMethod === 'Client WASM Remux'
                    ? '客户端转封装流解码异常，已自动回退服务端 HLS'
                    : '浏览器原生直连解码失败，已自动回退服务端 HLS',
            )
            setForceServerHls(true)
            return
          }
          setBuffering(false)
          setPlayerError('视频流解码或加载失败，请检查媒体格式或网络连接')
        }}
        className="h-full w-full object-contain"
      />

      {/* JASSUB (libass WASM) ASS/SSA 特效字幕覆盖 Canvas */}
      <canvas
        ref={assCanvasRef}
        className={`pointer-events-none absolute inset-0 h-full w-full object-contain ${
          activeSubtitleMeta.engine === 'ass' ? 'block' : 'hidden'
        }`}
      />

      {/* PGS 蓝光图形字幕覆盖 Canvas */}
      <canvas
        ref={pgsCanvasRef}
        className={`pointer-events-none absolute inset-0 h-full w-full object-contain ${
          activeSubtitleMeta.engine === 'pgs' ? 'block' : 'hidden'
        }`}
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
                      ? '拉取媒体流'
                      : wasmProgress.stage === 'init'
                        ? '启动 WASM 解复用引擎'
                        : wasmProgress.stage === 'remux'
                          ? '客户端零重编码转封装'
                          : '转封装完成'}
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
                视频直通 (-c:v copy) · 减少服务端负载
              </span>
              <button
                type="button"
                onClick={() => {
                  setWasmFallbackNotice('已手动切换至服务端 HLS')
                  setForceServerHls(true)
                }}
                className="rounded-lg border border-white/15 bg-white/5 px-2.5 py-1 text-[11px] font-medium text-zinc-200 transition-colors hover:bg-white/15"
              >
                改用服务端 HLS
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
              {infoLoading ? '正在加载媒体流...' : '正在缓冲...'}
            </span>
          </div>
        </div>
      )}

      {/* 错误提示浮层 */}
      {(playerError || infoError) && (
        <div className="absolute inset-0 z-20 flex items-center justify-center bg-black/80 px-4">
          <div className="max-w-md rounded-2xl border border-red-500/30 bg-zinc-950 p-6 text-center shadow-2xl">
            <AlertTriangle className="mx-auto mb-3 h-10 w-10 text-red-400" />
            <h2 className="text-base font-semibold text-white">无法播放该媒体</h2>
            <p className="mt-2 text-xs text-zinc-400">
              {playerError || (infoError instanceof Error ? infoError.message : '获取播放信息失败')}
            </p>
            <button
              type="button"
              onClick={() => navigate(-1)}
              className="mt-5 rounded-xl bg-amber-500 px-5 py-2 text-xs font-semibold text-zinc-950 hover:bg-amber-400"
            >
              返回上一页
            </button>
          </div>
        </div>
      )}

      {/* 自动连播下一集 8 秒倒计时浮层 */}
      {nextUpCountdown !== null && nextEpisode && (
        <div
          onClick={(e) => e.stopPropagation()}
          className="absolute bottom-28 right-6 z-30 w-80 max-w-[calc(100vw-3rem)] rounded-2xl border border-amber-500/35 bg-zinc-950/92 p-4 shadow-2xl backdrop-blur-xl"
        >
          <p className="text-[11px] font-semibold uppercase tracking-wider text-amber-400">
            即将播放下一集
          </p>
          <p className="mt-1 truncate text-sm font-semibold text-white">
            {nextEpisode.ParentIndexNumber != null && nextEpisode.IndexNumber != null
              ? `S${String(nextEpisode.ParentIndexNumber).padStart(2, '0')}E${String(nextEpisode.IndexNumber).padStart(2, '0')} `
              : nextEpisode.IndexNumber != null
                ? `第 ${nextEpisode.IndexNumber} 集 `
                : ''}
            {nextEpisode.Name}
          </p>
          <p className="mt-1 text-xs text-zinc-400">{nextUpCountdown} 秒后自动播放</p>
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
              取消
            </button>
            <button
              type="button"
              onClick={() => {
                setNextUpCountdown(null)
                navigate(`/play/${nextEpisode.Id}`, { replace: true })
              }}
              className="inline-flex items-center gap-1 rounded-xl bg-amber-500 px-3.5 py-1.5 text-xs font-semibold text-zinc-950 transition-colors hover:bg-amber-400"
            >
              <Play className="h-3.5 w-3.5 fill-current" />
              <span>立即播放</span>
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
              播放统计信息
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
              <dt className="text-zinc-400">播放模式</dt>
              <dd>
                <PlayMethodBadge method={playMethod} />
              </dd>
            </div>
            <StatRow
              label="客户端解复用"
              value={
                playMethod === 'Stream MKV (MSE)'
                  ? '流式 Range + MSE（支持 GB 级 MKV）'
                  : playMethod === 'Client WASM Remux'
                    ? 'WASM 已启用（单线程兼容）'
                    : !wasmSupported
                      ? '浏览器不支持'
                      : !preferWasmRemux
                        ? '已关闭'
                        : wasmFallbackNotice
                          ? '已自动降级'
                          : '待命（当前直连）'
              }
              highlight={playMethod === 'Stream MKV (MSE)' || playMethod === 'Client WASM Remux'}
            />
            <StatRow label="容器格式" value={(activeSource?.Container || '-').toUpperCase()} />
            {activeSource?.Size !== undefined && activeSource.Size > 0 && (
              <StatRow label="文件体积" value={formatBytes(activeSource.Size)} />
            )}
            <StatRow
              label="视频编码"
              value={
                videoStream
                  ? `${(videoStream.Codec || '-').toUpperCase()} ${videoStream.Width || ''}x${videoStream.Height || ''}`
                  : '-'
              }
            />
            <StatRow
              label="音轨编码"
              value={
                activeAudioStream
                  ? `${(activeAudioStream.Codec || '-').toUpperCase()} ${activeAudioStream.Channels || ''}ch`
                  : '-'
              }
            />
            <StatRow
              label="字幕引擎"
              value={activeSubtitleMeta.engineLabel}
              highlight={activeSubtitleMeta.engine !== 'none'}
            />
            {activeSubtitleMeta.engine === 'ass' && loadedFontCount > 0 && (
              <StatRow label="内封字体" value={`已挂载 ${loadedFontCount} 个`} highlight />
            )}
            {subtitlePrefs.offsetSec !== 0 && (
              <StatRow label="字幕偏移" value={formatSubtitleOffset(subtitlePrefs.offsetSec)} highlight />
            )}
            <StatRow
              label="缓冲进度"
              value={`${formatSeconds(bufferedEnd)} / ${formatSeconds(duration)}`}
            />
            {wasmFallbackNotice && (
              <p className="rounded-lg border border-sky-400/20 bg-sky-500/10 px-2.5 py-1.5 font-sans text-[11px] leading-relaxed text-sky-200">
                降级说明：{wasmFallbackNotice}
              </p>
            )}
            {subtitleError && (
              <p className="pt-1 text-[11px] text-rose-400">字幕异常: {subtitleError}</p>
            )}
          </dl>
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
            onClick={() => navigate(-1)}
            className="flex h-10 w-10 items-center justify-center rounded-xl bg-white/10 text-white backdrop-blur-md transition-colors hover:bg-white/20"
            title="返回"
          >
            <ArrowLeft className="h-5 w-5" />
          </button>
          <div className="min-w-0">
            <h1 className="truncate text-base font-semibold text-white">
              {item?.SeriesName ? `${item.SeriesName} · ` : ''}
              {item?.Name || '正在播放'}
            </h1>
            <div className="mt-0.5 flex items-center gap-2 text-[11px] text-zinc-300">
              <PlayMethodBadge method={playMethod} />
              {activeSubtitleMeta.engine !== 'none' && (
                <span className="rounded bg-white/10 px-1.5 py-0.5 text-zinc-200">
                  字幕: {activeSubtitleMeta.engineLabel}
                </span>
              )}
            </div>
          </div>
        </div>

        <button
          type="button"
          onClick={() => setShowStats((s) => !s)}
          className={`flex items-center gap-1.5 rounded-xl px-3 py-2 text-xs font-medium backdrop-blur-md transition-colors ${
            showStats
              ? 'bg-amber-500 text-zinc-950'
              : 'bg-white/10 text-zinc-200 hover:bg-white/20'
          }`}
          title="播放统计 (快捷键 I)"
        >
          <Activity className="h-4 w-4" />
          <span className="hidden sm:inline">统计</span>
        </button>
      </div>

      {/* 底部 OSD 播放控制栏 */}
      <div
        onClick={(e) => e.stopPropagation()}
        className={`absolute inset-x-0 bottom-0 z-20 bg-gradient-to-t from-black/90 via-black/60 to-transparent px-5 pb-5 pt-12 transition-opacity duration-300 ${
          osdVisible ? 'opacity-100' : 'pointer-events-none opacity-0'
        }`}
      >
        {/* 进度条 */}
        <div className="group/bar relative mb-3 flex items-center py-2">
          <div className="relative h-1.5 w-full overflow-hidden rounded-full bg-white/20 transition-all group-hover/bar:h-2">
            <div
              className="absolute inset-y-0 left-0 bg-white/30"
              style={{ width: `${bufferedPct}%` }}
            />
            <div
              className="absolute inset-y-0 left-0 bg-gradient-to-r from-amber-500 to-orange-400"
              style={{ width: `${progressPct}%` }}
            />
          </div>
          <input
            type="range"
            min={0}
            max={duration || 100}
            step={0.5}
            value={currentTime}
            onChange={(e) => {
              const t = Number(e.target.value)
              if (videoRef.current) videoRef.current.currentTime = t
              setCurrentTime(t)
            }}
            onPointerUp={reportSeekProgress}
            onMouseUp={reportSeekProgress}
            onTouchEnd={reportSeekProgress}
            onKeyUp={reportSeekProgress}
            className="ambra-range absolute inset-0 h-full w-full opacity-0 group-hover/bar:opacity-100"
          />
        </div>

        {/* 按钮行 */}
        <div className="flex items-center justify-between gap-3">
          {/* 左侧控制 */}
          <div className="flex items-center gap-2 sm:gap-3">
            {prevEpisode && (
              <button
                type="button"
                onClick={() => navigate(`/play/${prevEpisode.Id}`, { replace: true })}
                className="rounded-lg p-2 text-zinc-200 hover:bg-white/10"
                title={`上一集：${prevEpisode.Name}`}
                aria-label="上一集"
              >
                <SkipBack className="h-4 w-4" />
              </button>
            )}

            <button
              type="button"
              onClick={togglePlay}
              className="flex h-10 w-10 items-center justify-center rounded-xl bg-amber-500 text-zinc-950 transition-colors hover:bg-amber-400"
              title="播放/暂停 (Space)"
            >
              {playing ? (
                <Pause className="h-5 w-5 fill-current" />
              ) : (
                <Play className="ml-0.5 h-5 w-5 fill-current" />
              )}
            </button>

            {nextEpisode && (
              <button
                type="button"
                onClick={() => navigate(`/play/${nextEpisode.Id}`, { replace: true })}
                className="rounded-lg p-2 text-zinc-200 hover:bg-white/10"
                title={`下一集：${nextEpisode.Name}`}
                aria-label="下一集"
              >
                <SkipForward className="h-4 w-4" />
              </button>
            )}

            <button
              type="button"
              onClick={() => seekBy(-10)}
              className="rounded-lg p-2 text-zinc-200 hover:bg-white/10"
              title="快退 10 秒 (←)"
            >
              <RotateCcw className="h-4 w-4" />
            </button>
            <button
              type="button"
              onClick={() => seekBy(10)}
              className="rounded-lg p-2 text-zinc-200 hover:bg-white/10"
              title="快进 10 秒 (→)"
            >
              <RotateCw className="h-4 w-4" />
            </button>

            {/* 音量 */}
            <div className="hidden items-center gap-2 sm:flex">
              <button
                type="button"
                onClick={toggleMute}
                title="静音/取消静音 (M)"
                aria-label={muted || volume === 0 ? '取消静音' : '静音'}
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
                  setVolume(val)
                  setMuted(val === 0)
                }}
                className="ambra-range h-1 w-20 rounded-full bg-white/25"
              />
            </div>

            <span className="ml-1 font-mono text-xs text-zinc-300">
              {formatSeconds(currentTime)} <span className="text-zinc-500">/</span>{' '}
              {formatSeconds(duration)}
            </span>
          </div>

          {/* 右侧控制：音轨 / 字幕 / 倍速 / 全屏 */}
          <div className="relative flex items-center gap-1.5 sm:gap-2">
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
                  title="切换音轨 (A)"
                >
                  <Volume2 className="h-4 w-4" />
                  <span className="hidden md:inline">音轨</span>
                </button>

                {activePanel === 'audio' && (
                  <div className="absolute bottom-12 right-0 z-30 w-64 overflow-hidden rounded-2xl border border-white/15 bg-zinc-950/95 p-2 shadow-2xl backdrop-blur-xl">
                    <p className="px-2.5 py-1.5 text-[11px] font-semibold text-zinc-400">
                      选择音轨
                    </p>
                    <div className="max-h-60 space-y-1 overflow-y-auto">
                      {audioStreams.map((a) => (
                        <button
                          key={a.Index}
                          type="button"
                          onClick={() => {
                            setAudioIndex(a.Index)
                            setActivePanel('none')
                          }}
                          className={`flex w-full items-center justify-between rounded-xl px-2.5 py-2 text-left text-xs transition-colors ${
                            a.Index === audioIndex
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
                title="切换字幕 (C)"
              >
                <Subtitles className="h-4 w-4" />
                <span className="hidden md:inline">
                  {subtitleIndex !== undefined ? activeSubtitleMeta.formatBadge || '字幕' : '字幕'}
                </span>
              </button>

              {activePanel === 'subtitle' && (
                <div className="absolute bottom-12 right-0 z-30 w-80 overflow-hidden rounded-2xl border border-white/15 bg-zinc-950/95 p-2 shadow-2xl backdrop-blur-xl">
                  <p className="px-2.5 py-1.5 text-[11px] font-semibold text-zinc-400">
                    客户端直出字幕（零转码）
                  </p>
                  <div className="max-h-72 space-y-1 overflow-y-auto">
                    <button
                      type="button"
                      onClick={() => {
                        setSubtitleIndex(undefined)
                        setActivePanel('none')
                      }}
                      className={`flex w-full items-center justify-between rounded-xl px-2.5 py-2 text-left text-xs transition-colors ${
                        subtitleIndex === undefined
                          ? 'bg-amber-500/20 font-semibold text-amber-300'
                          : 'text-zinc-200 hover:bg-white/8'
                      }`}
                    >
                      <span>关闭字幕</span>
                    </button>
                    {subtitleStreams.map((s) => {
                      const meta = getSubtitleMeta(s)
                      return (
                        <button
                          key={s.Index}
                          type="button"
                          onClick={() => {
                            setSubtitleIndex(s.Index)
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
                title="字幕微调（时间偏移 / 字号 / 边距 / 背景）"
              >
                <SlidersHorizontal className="h-4 w-4" />
                <span className="hidden lg:inline">字幕微调</span>
              </button>

              {activePanel === 'subtitlePrefs' && (
                <div className="absolute bottom-12 right-0 z-30 w-80 overflow-hidden rounded-2xl border border-white/15 bg-zinc-950/95 p-3.5 shadow-2xl backdrop-blur-xl">
                  <div className="mb-3 flex items-center justify-between">
                    <div>
                      <p className="text-xs font-semibold text-white">字幕微调</p>
                      <p className="text-[10px] text-zinc-400">
                        偏移同步作用于 ASS / PGS / VTT / SRT
                      </p>
                    </div>
                    <button
                      type="button"
                      onClick={handleResetSubtitlePrefs}
                      className="rounded-lg border border-white/15 bg-white/5 px-2.5 py-1 text-[11px] font-medium text-zinc-300 transition-colors hover:bg-white/15 hover:text-white"
                    >
                      重置
                    </button>
                  </div>

                  <div className="space-y-3.5">
                    {/* 时间轴偏移 */}
                    <div>
                      <div className="mb-1.5 flex items-center justify-between text-[11px]">
                        <span className="text-zinc-300">时间轴偏移</span>
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
                        <span className="text-zinc-300">字号缩放</span>
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
                        <span className="text-zinc-300">底边距</span>
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
                        <span className="text-zinc-300">背景透明度</span>
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
                title="播放器设置"
              >
                <Settings className="h-4 w-4" />
              </button>

              {activePanel === 'settings' && (
                <div className="absolute bottom-12 right-0 z-30 w-80 overflow-hidden rounded-2xl border border-white/15 bg-zinc-950/95 p-3 shadow-2xl backdrop-blur-xl">
                  <p className="mb-2 text-[11px] font-semibold text-zinc-400">播放内核设置</p>

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
                        优先客户端 WASM 解复用（减少服务端转码）
                      </div>
                      <p className="mt-1 text-[11px] leading-relaxed text-zinc-400">
                        {wasmSupported
                          ? `对 MKV / TS / AC3 等格式优先在浏览器零重编码转封装，超出 ${formatBytes(DEFAULT_WASM_MAX_BYTES)} 或异常时自动回退服务端 HLS。`
                          : '当前浏览器不支持 WebAssembly 或 Web Worker。'}
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
                    <span>运行环境</span>
                    <span className="font-mono text-zinc-300">
                      {!wasmSupported
                        ? 'WASM 不可用'
                        : sabSupported
                          ? 'WASM 就绪 (COI)'
                          : '单线程兼容 (HTTP 局域网)'}
                    </span>
                  </div>
                </div>
              )}
            </div>

            {/* 全屏 */}
            <button
              type="button"
              onClick={toggleFullscreen}
              className="rounded-xl bg-white/10 p-2 text-zinc-200 hover:bg-white/20"
              title="全屏 (F)"
            >
              {isFullscreen ? <Minimize className="h-4 w-4" /> : <Maximize className="h-4 w-4" />}
            </button>
          </div>
        </div>
      </div>
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
