import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import Hls from 'hls.js'
import {
  Activity,
  AlertTriangle,
  ArrowLeft,
  Gauge,
  Loader2,
  Maximize,
  Minimize,
  Pause,
  Play,
  RotateCcw,
  RotateCw,
  Subtitles,
  Volume2,
  VolumeX,
  X,
} from 'lucide-react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { api, resolveApiUrl } from '../api/client'
import type { MediaSourceInfo, MediaStream } from '../api/types'
import { AmbraLogo } from '../components/AmbraLogo'
import { useAuth } from '../context/AuthContext'
import { AssJassubRenderer } from '../player/assRenderer'
import { PgsCanvasRenderer } from '../player/pgsRenderer'
import {
  detectSubtitleEngine,
  formatSeconds,
  getAudioLabel,
  getSubtitleMeta,
  parseVttOrSrt,
  type TextSubtitleCue,
} from '../player/subtitleUtils'

type ActivePanel = 'none' | 'subtitle' | 'audio' | 'speed'

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
  const assRendererRef = useRef<AssJassubRenderer | null>(null)
  const pgsRendererRef = useRef<PgsCanvasRenderer | null>(null)
  const hideOsdTimerRef = useRef<number | null>(null)
  const initialSeekDoneRef = useRef(false)

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
  const [playMethod, setPlayMethod] = useState<'DirectPlay' | 'DirectStream' | 'HLS Transcode'>(
    'DirectPlay',
  )

  // WebVTT / SRT 文本字幕状态
  const [textCues, setTextCues] = useState<TextSubtitleCue[]>([])

  const { data: item } = useQuery({
    queryKey: ['item', userId, id],
    queryFn: () => api.getItem(userId, id!),
    enabled: Boolean(id),
  })

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

  // 初始化默认音轨与字幕轨
  useEffect(() => {
    if (!activeSource) return
    if (selectedSourceId === undefined) setSelectedSourceId(activeSource.Id)
    if (audioIndex === undefined && activeSource.DefaultAudioStreamIndex !== undefined) {
      setAudioIndex(activeSource.DefaultAudioStreamIndex)
    }
    if (subtitleIndex === undefined && activeSource.DefaultSubtitleStreamIndex !== undefined) {
      setSubtitleIndex(activeSource.DefaultSubtitleStreamIndex)
    }
  }, [activeSource, selectedSourceId, audioIndex, subtitleIndex])

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

  // 计算视频流 URL（优先 DirectPlay，否则 HLS）
  const streamDescriptor = useMemo(() => {
    if (!activeSource || !id) return null

    if (activeSource.SupportsDirectPlay !== false && activeSource.DirectStreamUrl) {
      return {
        url: resolveApiUrl(activeSource.DirectStreamUrl),
        isHls: activeSource.DirectStreamUrl.includes('.m3u8'),
        method: 'DirectPlay' as const,
      }
    }

    if (activeSource.TranscodingUrl) {
      return {
        url: resolveApiUrl(activeSource.TranscodingUrl),
        isHls: activeSource.TranscodingUrl.includes('.m3u8'),
        method: 'HLS Transcode' as const,
      }
    }

    // 标准 Emby DirectPlay 静态直连 URL 回退
    const params = new URLSearchParams({
      Static: 'true',
      MediaSourceId: activeSource.Id,
    })
    if (audioIndex !== undefined) params.set('AudioStreamIndex', String(audioIndex))
    return {
      url: resolveApiUrl(`/emby/Videos/${encodeURIComponent(id)}/stream?${params.toString()}`),
      isHls: false,
      method: 'DirectPlay' as const,
    }
  }, [activeSource, id, audioIndex])

  // 挂载视频流 (Native DirectPlay / HLS.js)
  useEffect(() => {
    const video = videoRef.current
    if (!video || !streamDescriptor) return

    setPlayerError(null)
    setBuffering(true)
    setPlayMethod(streamDescriptor.method)

    if (hlsRef.current) {
      hlsRef.current.destroy()
      hlsRef.current = null
    }

    const startPlayback = () => {
      if (!initialSeekDoneRef.current && initialStartTicks > 0) {
        const startSec = initialStartTicks / 10_000_000
        if (Number.isFinite(startSec) && startSec > 0) {
          video.currentTime = startSec
        }
        initialSeekDoneRef.current = true
      }
      video.play().catch(() => {
        // 浏览器自动播放策略阻止时保持暂停状态让用户点击
      })
    }

    if (streamDescriptor.isHls) {
      if (Hls.isSupported()) {
        const hls = new Hls({
          enableWorker: true,
          lowLatencyMode: false,
        })
        hlsRef.current = hls
        hls.loadSource(streamDescriptor.url)
        hls.attachMedia(video)
        hls.on(Hls.Events.MANIFEST_PARSED, startPlayback)
        hls.on(Hls.Events.ERROR, (_evt, data) => {
          if (data.fatal) {
            setPlayerError(`HLS 播放错误: ${data.details}`)
          }
        })
      } else if (video.canPlayType('application/vnd.apple.mpegurl')) {
        video.src = streamDescriptor.url
        video.addEventListener('loadedmetadata', startPlayback, { once: true })
      } else {
        setPlayerError('当前浏览器不支持 HLS 流播放')
      }
    } else {
      video.src = streamDescriptor.url
      video.addEventListener('loadedmetadata', startPlayback, { once: true })
    }

    return () => {
      if (hlsRef.current) {
        hlsRef.current.destroy()
        hlsRef.current = null
      }
    }
  }, [streamDescriptor, initialStartTicks])

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
        `/emby/Videos/${encodeURIComponent(id || '')}/${encodeURIComponent(msId)}/Subtitles/${stream.Index}/0/Stream.${ext}`,
      )
    },
    [activeSource?.Id, id],
  )

  // 字幕渲染引擎切换与生命周期管理（JASSUB WASM / PGS Canvas / Native VTT）
  useEffect(() => {
    let cancelled = false
    setSubtitleError(null)
    setTextCues([])

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
          const renderer = new AssJassubRenderer()
          assRendererRef.current = renderer
          await renderer.mount(videoRef.current, assCanvasRef.current, subUrl)
        } else if (engine === 'pgs') {
          if (!pgsCanvasRef.current) return
          const renderer = new PgsCanvasRenderer(pgsCanvasRef.current)
          pgsRendererRef.current = renderer
          await renderer.loadFromUrl(subUrl)
          if (!cancelled && videoRef.current) {
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
  }, [activeSubtitleStream, resolveSubtitleUrl])

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

  // 上报播放状态 (/emby/Sessions/Playing, Progress, Stopped)
  const reportState = useCallback(
    (type: 'start' | 'progress' | 'stop', eventName?: string) => {
      if (!id || !videoRef.current) return
      const posTicks = Math.floor(videoRef.current.currentTime * 10_000_000)
      const base = {
        ItemId: id,
        MediaSourceId: activeSource?.Id,
        PlaySessionId: playbackInfo?.PlaySessionId,
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
        AudioStreamIndex: audioIndex,
        SubtitleStreamIndex: subtitleIndex,
        PlayMethod: playMethod === 'HLS Transcode' ? 'Transcode' : 'DirectPlay',
      }
      if (type === 'start') {
        void api.reportPlaying(full)
      } else {
        void api.reportProgress({ ...full, EventName: eventName })
      }
    },
    [id, activeSource?.Id, playbackInfo?.PlaySessionId, audioIndex, subtitleIndex, playMethod],
  )

  useEffect(() => {
    if (!playbackInfo) return
    reportState('start')
    const interval = window.setInterval(() => {
      reportState('progress', 'TimeUpdate')
    }, 10_000)

    return () => {
      window.clearInterval(interval)
      reportState('stop')
    }
  }, [playbackInfo, reportState])

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
      pokeOsd()
    },
    [pokeOsd],
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

  // 监听全屏状态变化
  useEffect(() => {
    const onFsChange = () => {
      setIsFullscreen(Boolean(document.fullscreenElement))
      assRendererRef.current?.resize()
    }
    document.addEventListener('fullscreenchange', onFsChange)
    return () => document.removeEventListener('fullscreenchange', onFsChange)
  }, [])

  // 全局快捷键：Space / 左右 / 上下 / F / C / A / I / Esc
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
          const next = Math.min(1, videoRef.current.volume + 0.1)
          videoRef.current.volume = next
          setVolume(next)
          setMuted(false)
        }
        pokeOsd()
      } else if (e.key === 'ArrowDown') {
        e.preventDefault()
        if (videoRef.current) {
          const next = Math.max(0, videoRef.current.volume - 0.1)
          videoRef.current.volume = next
          setVolume(next)
        }
        pokeOsd()
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
  }, [togglePlay, seekBy, toggleFullscreen, cycleSubtitle, cycleAudio, activePanel, navigate, pokeOsd])

  // 计算当前时间生效的 WebVTT/SRT 字幕文本
  const activeVttText = useMemo(() => {
    if (textCues.length === 0) return ''
    return textCues
      .filter((c) => currentTime >= c.start && currentTime <= c.end)
      .map((c) => c.text)
      .join('\n')
  }, [textCues, currentTime])

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
        onError={() => {
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
        <div className="pointer-events-none absolute inset-x-0 bottom-[11%] z-10 flex justify-center px-6">
          <div className="cinema-subtitle-text max-w-4xl whitespace-pre-line text-center text-xl font-semibold leading-snug tracking-wide text-white sm:text-2xl md:text-3xl">
            {activeVttText}
          </div>
        </div>
      )}

      {/* 缓冲或初始化加载指示 */}
      {(infoLoading || buffering) && !playerError && !infoError && (
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

      {/* 播放统计浮层 (Stats for Nerds) */}
      {showStats && (
        <div
          onClick={(e) => e.stopPropagation()}
          className="absolute left-5 top-20 z-30 w-80 rounded-2xl border border-white/15 bg-black/80 p-4 text-xs text-zinc-200 shadow-2xl backdrop-blur-xl"
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
          <dl className="space-y-1.5 font-mono text-[11px]">
            <StatRow label="播放模式" value={playMethod} highlight />
            <StatRow label="容器格式" value={(activeSource?.Container || '-').toUpperCase()} />
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
            <StatRow
              label="缓冲进度"
              value={`${formatSeconds(bufferedEnd)} / ${formatSeconds(duration)}`}
            />
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
              <span className="rounded bg-amber-500/25 px-1.5 py-0.5 font-medium text-amber-300">
                {playMethod}
              </span>
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
            className="ambra-range absolute inset-0 h-full w-full opacity-0 group-hover/bar:opacity-100"
          />
        </div>

        {/* 按钮行 */}
        <div className="flex items-center justify-between gap-3">
          {/* 左侧控制 */}
          <div className="flex items-center gap-2 sm:gap-3">
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
                onClick={() => {
                  if (!videoRef.current) return
                  const next = !muted
                  videoRef.current.muted = next
                  setMuted(next)
                }}
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
