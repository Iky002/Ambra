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
import { getAudioStreamUrl } from '../api/client'
import type { BaseItemDto } from '../api/types'
import { useAuth } from './AuthContext'

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
  clearError: NOOP,
  close: NOOP,
}

const AudioPlayerContext = createContext<AudioPlayerContextValue>(DEFAULT_AUDIO_CONTEXT)

export function AudioPlayerProvider({ children }: { children: ReactNode }) {
  const { auth } = useAuth()
  const authUserId = auth?.userId ?? null
  const audioRef = useRef<HTMLAudioElement | null>(null)
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

  // 用 ref 保存最新状态供原生 <audio> ended 事件读取
  const stateRef = useRef({
    queue,
    currentIndex,
    repeatMode,
    shuffle,
  })
  stateRef.current = { queue, currentIndex, repeatMode, shuffle }

  const currentTrack = currentIndex >= 0 && currentIndex < queue.length ? queue[currentIndex] : null

  const currentTrackId = currentTrack?.Id ?? null
  const currentTrackFallbackDuration = currentTrack?.RunTimeTicks
    ? currentTrack.RunTimeTicks / 10_000_000
    : 0

  // 切换曲目时加载音频流并自动播放
  useEffect(() => {
    const audio = audioRef.current
    if (!audio) return

    if (!currentTrackId) {
      audio.pause()
      audio.removeAttribute('src')
      audio.load()
      setIsPlaying(false)
      setCurrentTime(0)
      setDuration(0)
      setPlaybackError(null)
      return
    }

    const streamUrl = getAudioStreamUrl(currentTrackId)
    audio.src = streamUrl
    audio.currentTime = 0
    setCurrentTime(0)
    setDuration(currentTrackFallbackDuration)
    setPlaybackError(null)

    const playPromise = audio.play()
    if (playPromise && typeof playPromise.then === 'function') {
      playPromise
        .then(() => {
          setIsPlaying(true)
          setPlaybackError(null)
        })
        .catch((err: unknown) => {
          setIsPlaying(false)
          const msg =
            err instanceof Error && err.message
              ? `音频播放失败：${err.message}`
              : '音频播放被拦截或加载失败，请点击重试'
          setPlaybackError(msg)
        })
    } else {
      setIsPlaying(true)
    }
  }, [currentTrackId, currentTrackFallbackDuration])

  // 同步音量与静音状态
  useEffect(() => {
    const audio = audioRef.current
    if (!audio) return
    audio.volume = Math.max(0, Math.min(1, volume))
    audio.muted = muted
  }, [volume, muted])

  const next = useCallback(() => {
    const { queue: q, currentIndex: idx, repeatMode: rep, shuffle: shuf } = stateRef.current
    if (q.length === 0) return

    if (shuf && q.length > 1) {
      let randIdx = idx
      for (let i = 0; i < 6 && randIdx === idx; i++) {
        randIdx = Math.floor(Math.random() * q.length)
      }
      setCurrentIndex(randIdx)
      return
    }

    if (idx + 1 < q.length) {
      setCurrentIndex(idx + 1)
    } else if (rep === 'all') {
      setCurrentIndex(0)
    } else {
      // 播放完最后一首且未开启循环：停止播放
      const audio = audioRef.current
      if (audio) {
        audio.pause()
      }
      setIsPlaying(false)
    }
  }, [])

  const prev = useCallback(() => {
    const { queue: q, currentIndex: idx, repeatMode: rep } = stateRef.current
    if (q.length === 0) return
    const audio = audioRef.current
    if (audio && audio.currentTime > 3) {
      audio.currentTime = 0
      setCurrentTime(0)
      return
    }
    if (idx > 0) {
      setCurrentIndex(idx - 1)
    } else if (rep === 'all') {
      setCurrentIndex(q.length - 1)
    } else if (audio) {
      audio.currentTime = 0
      setCurrentTime(0)
    }
  }, [])

  const playQueue = useCallback((tracks: BaseItemDto[], startIndex = 0) => {
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
      const audio = audioRef.current
      if (audio) {
        audio.currentTime = 0
        setCurrentTime(0)
        setPlaybackError(null)
        const p = audio.play()
        if (p && typeof p.then === 'function') {
          p.then(() => {
            setIsPlaying(true)
            setPlaybackError(null)
          }).catch((err: unknown) => {
            setIsPlaying(false)
            setPlaybackError(
              err instanceof Error && err.message
                ? `音频播放失败：${err.message}`
                : '音频播放被拦截或加载失败，请点击重试',
            )
          })
        } else {
          setIsPlaying(true)
        }
      }
    }
  }, [])

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
    const audio = audioRef.current
    if (audio) {
      audio.pause()
    }
    setIsPlaying(false)
  }, [])

  const togglePlay = useCallback(() => {
    const audio = audioRef.current
    if (!audio || stateRef.current.currentIndex < 0) return
    if (isPlaying) {
      audio.pause()
      setIsPlaying(false)
    } else {
      setPlaybackError(null)
      const p = audio.play()
      if (p && typeof p.then === 'function') {
        p.then(() => {
          setIsPlaying(true)
          setPlaybackError(null)
        }).catch((err: unknown) => {
          setIsPlaying(false)
          setPlaybackError(
            err instanceof Error && err.message
              ? `音频播放失败：${err.message}`
              : '音频播放被拦截或加载失败，请点击重试',
          )
        })
      } else {
        setIsPlaying(true)
      }
    }
  }, [isPlaying])

  const seek = useCallback((sec: number) => {
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
  }, [])

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

  const selectTrackAt = useCallback((index: number) => {
    const q = stateRef.current.queue
    if (index < 0 || index >= q.length) return
    setCurrentIndex(index)
  }, [])

  const removeFromQueue = useCallback((index: number) => {
    setQueue((prevQueue) => {
      if (index < 0 || index >= prevQueue.length) return prevQueue
      const nextQueue = prevQueue.filter((_, i) => i !== index)
      setCurrentIndex((prevIdx) => {
        if (nextQueue.length === 0) return -1
        if (index < prevIdx) return prevIdx - 1
        if (index === prevIdx) return Math.min(prevIdx, nextQueue.length - 1)
        return prevIdx
      })
      return nextQueue
    })
  }, [])

  const close = useCallback(() => {
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
  }, [])

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
    const { repeatMode: rep } = stateRef.current
    const audio = audioRef.current
    if (rep === 'one' && audio) {
      audio.currentTime = 0
      setCurrentTime(0)
      setPlaybackError(null)
      const p = audio.play()
      if (p && typeof p.then === 'function') {
        p.then(() => {
          setIsPlaying(true)
          setPlaybackError(null)
        }).catch((err: unknown) => {
          setIsPlaying(false)
          setPlaybackError(
            err instanceof Error && err.message
              ? `音频播放失败：${err.message}`
              : '音频播放被拦截或加载失败，请点击重试',
          )
        })
      } else {
        setIsPlaying(true)
      }
      return
    }
    next()
  }, [next])

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
          setCurrentTime(e.currentTarget.currentTime || 0)
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
          setPlaybackError('音频流加载失败，请检查网络连接或点击重试')
        }}
        onEnded={handleEnded}
      />
    </AudioPlayerContext.Provider>
  )
}

export function useAudioPlayer(): AudioPlayerContextValue {
  return useContext(AudioPlayerContext)
}
