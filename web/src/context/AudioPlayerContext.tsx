import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import { api, getAudioStreamUrl, getStoredAuth } from '../api/client'
import type { BaseItemDto } from '../api/types'
import { useI18n } from '../i18n/I18nContext'
import { useAuth } from './AuthContext'
import { useWebSocket } from './WebSocketContext'

export type RepeatMode = 'off' | 'all' | 'one'

export interface AudioPlayerContextValue {
  queue: BaseItemDto[]
  currentIndex: number
  currentTrack: BaseItemDto | null
  isPlaying: boolean
  currentTime: number
  duration: number
  volume: number
  muted: boolean
  repeatMode: RepeatMode
  shuffle: boolean
  playbackError: string | null
  playTrack: (track: BaseItemDto, queue?: BaseItemDto[]) => void
  playQueue: (tracks: BaseItemDto[], startIndex?: number) => void
  togglePlay: () => void
  pause: () => void
  next: () => void
  prev: () => void
  seek: (sec: number) => void
  setVolume: (vol: number) => void
  toggleMute: () => void
  cycleRepeatMode: () => void
  toggleShuffle: () => void
  selectTrackAt: (index: number) => void
  removeFromQueue: (index: number) => void
  retry: () => void
  clearError: () => void
  close: () => void
}

const NOOP = () => undefined

const DEFAULT_AUDIO_CONTEXT: AudioPlayerContextValue = {
  queue: [],
  currentIndex: -1,
  currentTrack: null,
  isPlaying: false,
  currentTime: 0,
  duration: 0,
  volume: 0.85,
  muted: false,
  repeatMode: 'off',
  shuffle: false,
  playbackError: null,
  playTrack: NOOP,
  playQueue: NOOP,
  togglePlay: NOOP,
  pause: NOOP,
  next: NOOP,
  prev: NOOP,
  seek: NOOP,
  setVolume: NOOP,
  toggleMute: NOOP,
  cycleRepeatMode: NOOP,
  toggleShuffle: NOOP,
  selectTrackAt: NOOP,
  removeFromQueue: NOOP,
  retry: NOOP,
  clearError: NOOP,
  close: NOOP,
}

const AudioPlayerContext = createContext<AudioPlayerContextValue>(DEFAULT_AUDIO_CONTEXT)

function isAbortError(err: unknown): boolean {
  if (!err || typeof err !== 'object') return false
  const name = 'name' in err ? String((err as { name?: unknown }).name) : ''
  return name === 'AbortError'
}

export function AudioPlayerProvider({ children }: { children: ReactNode }) {
  const { auth } = useAuth()
  const { subscribePlaystate } = useWebSocket()
  const { t } = useI18n()
  const tRef = useRef(t)
  tRef.current = t
  const authUserId = auth?.userId ?? null
  const audioRef = useRef<HTMLAudioElement | null>(null)
  const playSeqRef = useRef<number>(0)
  const activeSessionRef = useRef<{
    itemId: string
    playSessionId: string
    lastPositionTicks: number
  } | null>(null)
  const [queue, setQueue] = useState<BaseItemDto[]>([])
  const [currentIndex, setCurrentIndex] = useState<number>(-1)
  const [isPlaying, setIsPlaying] = useState<boolean>(false)
  const [currentTime, setCurrentTime] = useState<number>(0)
  const [duration, setDuration] = useState<number>(0)
  const [volume, setVolumeState] = useState<number>(0.85)
  const [muted, setMuted] = useState<boolean>(false)
  const [repeatMode, setRepeatMode] = useState<RepeatMode>('off')
  const [shuffle, setShuffle] = useState<boolean>(false)
  const [playbackError, setPlaybackError] = useState<string | null>(null)

  const clearError = useCallback(() => {
    setPlaybackError(null)
  }, [])

  const triggerAudioPlay = useCallback((audio: HTMLAudioElement) => {
    const seq = ++playSeqRef.current
    const playPromise = audio.play()
    if (playPromise && typeof playPromise.then === 'function') {
      playPromise
        .then(() => {
          if (seq !== playSeqRef.current) return
          setIsPlaying(true)
          setPlaybackError(null)
        })
        .catch((err: unknown) => {
          if (seq !== playSeqRef.current || isAbortError(err)) return
          setIsPlaying(false)
          const msg =
            err instanceof Error && err.message
              ? tRef.current('audio.playbackFailed', { message: err.message })
              : tRef.current('audio.playbackBlockedOrFailed')
          setPlaybackError(msg)
        })
    } else {
      setIsPlaying(true)
    }
  }, [])

  // 用 ref 保存最新状态供原生 <audio> ended 事件与会话上报读取
  const stateRef = useRef({
    queue,
    currentIndex,
    repeatMode,
    shuffle,
    volume,
    muted,
  })
  stateRef.current = { queue, currentIndex, repeatMode, shuffle, volume, muted }

  const reportActiveStopped = useCallback(() => {
    const active = activeSessionRef.current
    if (!active) return
    activeSessionRef.current = null
    if (!getStoredAuth()?.accessToken) return
    const audio = audioRef.current
    const posTicks =
      audio && Number.isFinite(audio.currentTime) && audio.currentTime > 0
        ? Math.floor(audio.currentTime * 10_000_000)
        : active.lastPositionTicks
    void api.reportPlaybackStopped({
      ItemId: active.itemId,
      MediaSourceId: active.itemId,
      PlaySessionId: active.playSessionId,
      PositionTicks: posTicks,
    })
  }, [])

  const reportActiveStart = useCallback((trackId: string) => {
    const playSessionId = `audio-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`
    activeSessionRef.current = {
      itemId: trackId,
      playSessionId,
      lastPositionTicks: 0,
    }
    if (!getStoredAuth()?.accessToken) return
    void api.reportPlaybackStart({
      ItemId: trackId,
      MediaSourceId: trackId,
      PlaySessionId: playSessionId,
      PositionTicks: 0,
      IsPaused: false,
      IsMuted: stateRef.current.muted,
      VolumeLevel: Math.round(stateRef.current.volume * 100),
      PlayMethod: 'DirectStream',
    })
  }, [])

  const reportActiveProgress = useCallback((eventName?: string, forcedPaused?: boolean) => {
    const active = activeSessionRef.current
    if (!active || !getStoredAuth()?.accessToken) return
    const audio = audioRef.current
    const curSec = audio && Number.isFinite(audio.currentTime) ? audio.currentTime : 0
    const posTicks = Math.max(0, Math.floor(curSec * 10_000_000))
    active.lastPositionTicks = posTicks
    void api.reportPlaybackProgress({
      ItemId: active.itemId,
      MediaSourceId: active.itemId,
      PlaySessionId: active.playSessionId,
      PositionTicks: posTicks,
      IsPaused: forcedPaused ?? Boolean(audio?.paused),
      IsMuted: audio ? audio.muted : stateRef.current.muted,
      VolumeLevel: Math.round((audio ? audio.volume : stateRef.current.volume) * 100),
      PlayMethod: 'DirectStream',
      EventName: eventName,
    })
  }, [])

  const restartSameTrack = useCallback(
    (trackId: string) => {
      const audio = audioRef.current
      if (!audio) return
      reportActiveStopped()
      audio.currentTime = 0
      setCurrentTime(0)
      setPlaybackError(null)
      reportActiveStart(trackId)
      triggerAudioPlay(audio)
    },
    [reportActiveStart, reportActiveStopped, triggerAudioPlay],
  )

  const currentTrack = currentIndex >= 0 && currentIndex < queue.length ? queue[currentIndex] : null

  const currentTrackId = currentTrack?.Id ?? null
  const currentTrackFallbackDuration = currentTrack?.RunTimeTicks
    ? currentTrack.RunTimeTicks / 10_000_000
    : 0
  const currentTrackFallbackDurationRef = useRef(currentTrackFallbackDuration)
  currentTrackFallbackDurationRef.current = currentTrackFallbackDuration

  // 切换曲目时上报上一曲停止，加载新音频流并上报开始播放
  useEffect(() => {
    const audio = audioRef.current
    if (!audio) return

    if (!currentTrackId) {
      reportActiveStopped()
      playSeqRef.current++
      audio.pause()
      audio.removeAttribute('src')
      audio.load()
      setIsPlaying(false)
      setCurrentTime(0)
      setDuration(0)
      setPlaybackError(null)
      return
    }

    if (activeSessionRef.current && activeSessionRef.current.itemId !== currentTrackId) {
      reportActiveStopped()
    }

    const streamUrl = getAudioStreamUrl(currentTrackId)
    audio.src = streamUrl
    audio.currentTime = 0
    setCurrentTime(0)
    setDuration(currentTrackFallbackDurationRef.current)
    setPlaybackError(null)

    if (!activeSessionRef.current || activeSessionRef.current.itemId !== currentTrackId) {
      reportActiveStart(currentTrackId)
    }

    triggerAudioPlay(audio)
  }, [currentTrackId, reportActiveStart, reportActiveStopped, triggerAudioPlay])

  // 播放期间每 10 秒上报一次播放进度，卸载时上报停止
  useEffect(() => {
    if (!currentTrackId || !isPlaying) return
    const timer = window.setInterval(() => {
      reportActiveProgress('TimeUpdate', false)
    }, 10_000)
    return () => window.clearInterval(timer)
  }, [currentTrackId, isPlaying, reportActiveProgress])

  useEffect(() => {
    return () => {
      reportActiveStopped()
    }
  }, [reportActiveStopped])

  // 同步音量与静音状态
  useEffect(() => {
    const audio = audioRef.current
    if (!audio) return
    audio.volume = Math.max(0, Math.min(1, volume))
    audio.muted = muted
  }, [volume, muted])

  const switchToTrackIndex = useCallback(
    (targetIdx: number) => {
      const { queue: q, currentIndex: prevIdx } = stateRef.current
      if (targetIdx < 0 || targetIdx >= q.length) return
      const prevTrack = prevIdx >= 0 && prevIdx < q.length ? q[prevIdx] : null
      const nextTrack = q[targetIdx]
      setCurrentIndex(targetIdx)
      if (prevTrack && nextTrack && prevTrack.Id === nextTrack.Id) {
        restartSameTrack(nextTrack.Id)
      }
    },
    [restartSameTrack],
  )

  const next = useCallback(() => {
    const { queue: q, currentIndex: idx, repeatMode: rep, shuffle: shuf } = stateRef.current
    if (q.length === 0) return

    if (shuf && q.length > 1) {
      let randIdx = idx
      for (let i = 0; i < 6 && randIdx === idx; i++) {
        randIdx = Math.floor(Math.random() * q.length)
      }
      switchToTrackIndex(randIdx)
      return
    }

    if (idx + 1 < q.length) {
      switchToTrackIndex(idx + 1)
    } else if (rep === 'all') {
      switchToTrackIndex(0)
    } else {
      // 播放完最后一首且未开启循环：上报停止并暂停播放
      reportActiveStopped()
      const audio = audioRef.current
      if (audio) {
        audio.pause()
      }
      setIsPlaying(false)
    }
  }, [reportActiveStopped, switchToTrackIndex])

  const prev = useCallback(() => {
    const { queue: q, currentIndex: idx, repeatMode: rep } = stateRef.current
    if (q.length === 0) return
    const audio = audioRef.current
    if (audio && audio.currentTime > 3) {
      audio.currentTime = 0
      setCurrentTime(0)
      reportActiveProgress('TimeUpdate')
      return
    }
    if (idx > 0) {
      switchToTrackIndex(idx - 1)
    } else if (rep === 'all') {
      switchToTrackIndex(q.length - 1)
    } else if (audio) {
      audio.currentTime = 0
      setCurrentTime(0)
      reportActiveProgress('TimeUpdate')
    }
  }, [reportActiveProgress, switchToTrackIndex])

  const playQueue = useCallback(
    (tracks: BaseItemDto[], startIndex = 0) => {
      if (!tracks || tracks.length === 0) return
      const clamped = Math.max(0, Math.min(tracks.length - 1, startIndex))
      const target = tracks[clamped]
      const prevTrack =
        stateRef.current.currentIndex >= 0
          ? stateRef.current.queue[stateRef.current.currentIndex]
          : null

      setQueue(tracks)
      setCurrentIndex(clamped)

      // 如果点击的是当前已在播放的同一首曲目，确保从头或恢复播放
      if (prevTrack && target && prevTrack.Id === target.Id) {
        restartSameTrack(target.Id)
      }
    },
    [restartSameTrack],
  )

  const playTrack = useCallback(
    (track: BaseItemDto, nextQueue?: BaseItemDto[]) => {
      if (nextQueue && nextQueue.length > 0) {
        const idx = nextQueue.findIndex((item) => item.Id === track.Id)
        playQueue(nextQueue, idx >= 0 ? idx : 0)
        return
      }
      playQueue([track], 0)
    },
    [playQueue],
  )

  const pause = useCallback(() => {
    playSeqRef.current++
    const audio = audioRef.current
    if (audio) {
      audio.pause()
    }
    setIsPlaying(false)
    reportActiveProgress('Pause', true)
  }, [reportActiveProgress])

  const retry = useCallback(() => {
    const audio = audioRef.current
    const { queue: q, currentIndex: idx } = stateRef.current
    const track = idx >= 0 && idx < q.length ? q[idx] : null
    if (!audio || !track) return

    setPlaybackError(null)
    audio.src = getAudioStreamUrl(track.Id)
    audio.load()
    triggerAudioPlay(audio)
  }, [triggerAudioPlay])

  const togglePlay = useCallback(() => {
    const audio = audioRef.current
    if (!audio || stateRef.current.currentIndex < 0) return
    if (isPlaying) {
      playSeqRef.current++
      audio.pause()
      setIsPlaying(false)
      reportActiveProgress('Pause', true)
    } else {
      const noSourceState =
        typeof HTMLMediaElement !== 'undefined' ? HTMLMediaElement.NETWORK_NO_SOURCE : 3
      if (audio.error || audio.networkState === noSourceState) {
        retry()
        return
      }
      setPlaybackError(null)
      if (!activeSessionRef.current) {
        const cur = stateRef.current.queue[stateRef.current.currentIndex]
        if (cur) reportActiveStart(cur.Id)
      } else {
        reportActiveProgress('Unpause', false)
      }
      triggerAudioPlay(audio)
    }
  }, [isPlaying, reportActiveProgress, reportActiveStart, retry, triggerAudioPlay])

  const seek = useCallback(
    (sec: number) => {
      const audio = audioRef.current
      const safeSec = Math.max(0, sec)
      if (audio) {
        try {
          audio.currentTime = safeSec
        } catch {
          // 忽略未加载完媒体时的 seek 异常
        }
      }
      setCurrentTime(safeSec)
      if (activeSessionRef.current) {
        activeSessionRef.current.lastPositionTicks = Math.floor(safeSec * 10_000_000)
        reportActiveProgress('TimeUpdate')
      }
    },
    [reportActiveProgress],
  )

  const setVolume = useCallback((vol: number) => {
    const clamped = Math.max(0, Math.min(1, vol))
    setVolumeState(clamped)
    if (clamped > 0) {
      setMuted(false)
    }
  }, [])

  const toggleMute = useCallback(() => {
    setMuted((m) => !m)
  }, [])

  const cycleRepeatMode = useCallback(() => {
    setRepeatMode((prevMode) => {
      if (prevMode === 'off') return 'all'
      if (prevMode === 'all') return 'one'
      return 'off'
    })
  }, [])

  const toggleShuffle = useCallback(() => {
    setShuffle((s) => !s)
  }, [])

  const selectTrackAt = useCallback(
    (index: number) => {
      switchToTrackIndex(index)
    },
    [switchToTrackIndex],
  )

  const removeFromQueue = useCallback(
    (index: number) => {
      const { queue: prevQueue, currentIndex: prevIdx } = stateRef.current
      if (index < 0 || index >= prevQueue.length) return
      const prevTrack = prevIdx >= 0 && prevIdx < prevQueue.length ? prevQueue[prevIdx] : null
      const nextQueue = prevQueue.filter((_, i) => i !== index)
      let nextIdx: number
      if (nextQueue.length === 0) {
        nextIdx = -1
      } else if (index < prevIdx) {
        nextIdx = prevIdx - 1
      } else if (index === prevIdx) {
        nextIdx = Math.min(prevIdx, nextQueue.length - 1)
      } else {
        nextIdx = prevIdx
      }
      setQueue(nextQueue)
      setCurrentIndex(nextIdx)
      if (index === prevIdx && nextIdx >= 0) {
        const nextTrack = nextQueue[nextIdx]
        if (prevTrack && nextTrack && prevTrack.Id === nextTrack.Id) {
          restartSameTrack(nextTrack.Id)
        }
      }
    },
    [restartSameTrack],
  )

  const close = useCallback(() => {
    reportActiveStopped()
    playSeqRef.current++
    const audio = audioRef.current
    if (audio) {
      audio.pause()
      audio.removeAttribute('src')
      audio.load()
    }
    setQueue([])
    setCurrentIndex(-1)
    setIsPlaying(false)
    setCurrentTime(0)
    setDuration(0)
    setPlaybackError(null)
  }, [reportActiveStopped])

  // 用户退出登录或切换账号时停止播放并清空队列
  const prevUserIdRef = useRef<string | null>(authUserId)
  useEffect(() => {
    const prevUserId = prevUserIdRef.current
    prevUserIdRef.current = authUserId
    if (prevUserId !== authUserId && (!authUserId || prevUserId !== null)) {
      close()
    }
  }, [authUserId, close])

  const handleEnded = useCallback(() => {
    const { repeatMode: rep, queue: q, currentIndex: idx } = stateRef.current
    const curTrack = idx >= 0 && idx < q.length ? q[idx] : null
    if (rep === 'one' && curTrack) {
      restartSameTrack(curTrack.Id)
      return
    }
    next()
  }, [next, restartSameTrack])

  // 订阅 WebSocket 远端控播指令 (Pause / Unpause / PlayPause / Stop / NextTrack / PreviousTrack / Seek)
  useEffect(() => {
    return subscribePlaystate((cmd) => {
      if (cmd.PartyId) return
      const { currentIndex: idx, queue: q } = stateRef.current
      if (idx < 0 || idx >= q.length) return
      const curTrack = q[idx]
      if (cmd.ItemId && cmd.ItemId !== curTrack?.Id) return
      const activeSessionId = activeSessionRef.current?.playSessionId
      if (cmd.PlaySessionId && cmd.PlaySessionId !== activeSessionId) {
        return
      }
      const onVideoPlayerRoute =
        typeof window !== 'undefined' && window.location?.pathname?.startsWith('/play/')
      if (onVideoPlayerRoute && (!cmd.PlaySessionId || cmd.PlaySessionId !== activeSessionId)) {
        return
      }
      switch (cmd.Command) {
        case 'Pause':
          pause()
          break
        case 'Unpause': {
          const audio = audioRef.current
          if (audio) {
            setPlaybackError(null)
            if (!activeSessionRef.current && curTrack) {
              reportActiveStart(curTrack.Id)
            }
            reportActiveProgress('Unpause', false)
            triggerAudioPlay(audio)
          }
          break
        }
        case 'PlayPause':
          togglePlay()
          break
        case 'Stop':
          close()
          break
        case 'NextTrack':
          next()
          break
        case 'PreviousTrack':
          prev()
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
            seek(ticks / 10_000_000)
          }
          break
        }
        default:
          break
      }
    })
  }, [
    subscribePlaystate,
    pause,
    reportActiveStart,
    reportActiveProgress,
    triggerAudioPlay,
    togglePlay,
    close,
    next,
    prev,
    seek,
  ])

  const value = useMemo<AudioPlayerContextValue>(
    () => ({
      queue,
      currentIndex,
      currentTrack,
      isPlaying,
      currentTime,
      duration,
      volume,
      muted,
      repeatMode,
      shuffle,
      playbackError,
      playTrack,
      playQueue,
      togglePlay,
      pause,
      next,
      prev,
      seek,
      setVolume,
      toggleMute,
      cycleRepeatMode,
      toggleShuffle,
      selectTrackAt,
      removeFromQueue,
      retry,
      clearError,
      close,
    }),
    [
      queue,
      currentIndex,
      currentTrack,
      isPlaying,
      currentTime,
      duration,
      volume,
      muted,
      repeatMode,
      shuffle,
      playbackError,
      playTrack,
      playQueue,
      togglePlay,
      pause,
      next,
      prev,
      seek,
      setVolume,
      toggleMute,
      cycleRepeatMode,
      toggleShuffle,
      selectTrackAt,
      removeFromQueue,
      retry,
      clearError,
      close,
    ],
  )

  return (
    <AudioPlayerContext.Provider value={value}>
      {children}
      <audio
        ref={audioRef}
        preload="metadata"
        className="hidden"
        onTimeUpdate={(e) => {
          const cur = e.currentTarget.currentTime || 0
          setCurrentTime(cur)
          if (activeSessionRef.current && Number.isFinite(cur) && cur >= 0) {
            activeSessionRef.current.lastPositionTicks = Math.floor(cur * 10_000_000)
          }
        }}
        onLoadedMetadata={(e) => {
          const d = e.currentTarget.duration
          if (Number.isFinite(d) && d > 0) {
            setDuration(d)
          }
        }}
        onDurationChange={(e) => {
          const d = e.currentTarget.duration
          if (Number.isFinite(d) && d > 0) {
            setDuration(d)
          }
        }}
        onPlay={() => {
          setIsPlaying(true)
          setPlaybackError(null)
        }}
        onPause={() => setIsPlaying(false)}
        onError={() => {
          setIsPlaying(false)
          setPlaybackError(tRef.current('audio.streamLoadFailed'))
        }}
        onEnded={handleEnded}
      />
    </AudioPlayerContext.Provider>
  )
}

export function useAudioPlayer(): AudioPlayerContextValue {
  return useContext(AudioPlayerContext)
}
