import { useEffect, useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  AlertCircle,
  FileText,
  ListMusic,
  Loader2,
  Music,
  Pause,
  Play,
  Repeat,
  Repeat1,
  RotateCw,
  Shuffle,
  SkipBack,
  SkipForward,
  Trash2,
  Volume2,
  VolumeX,
  X,
} from 'lucide-react'
import { Link } from 'react-router-dom'
import { api, getFallbackImageUrl, getImageUrl } from '../api/client'
import type { AudioLyricsResponse, BaseItemDto } from '../api/types'
import { useAudioPlayer } from '../context/AudioPlayerContext'

export interface ParsedLyricLine {
  timeSec: number | null
  text: string
}

export function formatAudioTime(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) return '00:00'
  const total = Math.floor(seconds)
  const mins = Math.floor(total / 60)
  const secs = total % 60
  return `${String(mins).padStart(2, '0')}:${String(secs).padStart(2, '0')}`
}

export function parseLrcText(raw: string): ParsedLyricLine[] {
  const leadingMetaLines: ParsedLyricLine[] = []
  const timedAndTrailingLines: Array<{
    line: ParsedLyricLine
    order: number
    anchorTime: number | null
  }> = []
  const timeTagRegex = /\[(\d{1,3}):(\d{2})(?:[.:](\d{1,3}))?\]/g
  const offsetRegex = /^\[offset:\s*([+-]?\d+)\s*\]$/i
  let offsetMs = 0
  let seenTimedLine = false
  let lastTimedSec: number | null = null
  let orderCounter = 0

  for (const rawLine of raw.split(/\r?\n/)) {
    const trimmed = rawLine.trim()
    if (!trimmed) continue

    const offsetMatch = trimmed.match(offsetRegex)
    if (offsetMatch) {
      const parsed = Number(offsetMatch[1])
      if (Number.isFinite(parsed)) {
        offsetMs = parsed
      }
      continue
    }

    const matches = Array.from(trimmed.matchAll(timeTagRegex))
    const text = trimmed.replace(timeTagRegex, '').trim()

    if (matches.length > 0) {
      seenTimedLine = true
      for (const m of matches) {
        const min = Number(m[1])
        const sec = Number(m[2])
        const msRaw = m[3] || '0'
        const frac = Number(msRaw) / Math.pow(10, msRaw.length)
        // LRC 标准：[offset:+ms] 使歌词提前显示（时间戳减去 offsetMs/1000）
        const adjustedSec = Math.max(0, min * 60 + sec + frac - offsetMs / 1000)
        lastTimedSec = adjustedSec
        timedAndTrailingLines.push({
          line: { timeSec: adjustedSec, text },
          order: orderCounter++,
          anchorTime: adjustedSec,
        })
      }
    } else if (!/^\[[a-zA-Z]+:.*\]$/.test(trimmed)) {
      if (!seenTimedLine) {
        leadingMetaLines.push({ timeSec: null, text: trimmed })
      } else {
        timedAndTrailingLines.push({
          line: { timeSec: null, text: trimmed },
          order: orderCounter++,
          anchorTime: lastTimedSec,
        })
      }
    }
  }

  timedAndTrailingLines.sort((a, b) => {
    if (a.anchorTime == null && b.anchorTime == null) return a.order - b.order
    if (a.anchorTime == null) return 1
    if (b.anchorTime == null) return -1
    if (a.anchorTime !== b.anchorTime) return a.anchorTime - b.anchorTime
    return a.order - b.order
  })

  return [...leadingMetaLines, ...timedAndTrailingLines.map((item) => item.line)]
}

export function normalizeLyricsResponse(
  data: AudioLyricsResponse | string | undefined,
): ParsedLyricLine[] {
  if (!data) return []
  if (typeof data === 'string') {
    return parseLrcText(data)
  }
  if (data.RawLrc) {
    return parseLrcText(data.RawLrc)
  }
  if (Array.isArray(data.Lyrics) && data.Lyrics.length > 0) {
    return data.Lyrics.filter(
      (line) => line != null && (Boolean(line.Text?.trim()) || line.Start != null),
    ).map((line) => ({
      // Emby/Jellyfin 的 Start 字段始终为 100ns ticks（10_000_000 = 1 秒）
      timeSec:
        typeof line.Start === 'number' && Number.isFinite(line.Start)
          ? line.Start / 10_000_000
          : null,
      text: (line.Text || '').trim(),
    }))
  }
  return []
}

function getTrackArtwork(track: BaseItemDto): { primary: string; fallback: string | null } {
  if (track.ImageTags?.Primary) {
    return {
      primary: getImageUrl(track.Id, 'Primary', { maxWidth: 160, tag: track.ImageTags.Primary }),
      fallback: getFallbackImageUrl(track.Id, 'Primary'),
    }
  }
  if (track.AlbumId) {
    return {
      primary: getImageUrl(track.AlbumId, 'Primary', {
        maxWidth: 160,
        tag: track.AlbumPrimaryImageTag,
      }),
      fallback: getFallbackImageUrl(track.AlbumId, 'Primary'),
    }
  }
  return { primary: '', fallback: null }
}

function getTrackArtist(track: BaseItemDto): string {
  if (track.Artists && track.Artists.length > 0) {
    return track.Artists.join(' / ')
  }
  return track.AlbumArtist || '未知艺术家'
}

export function AudioPlayerBar() {
  const {
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
    togglePlay,
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
  } = useAudioPlayer()

  const [queueOpen, setQueueOpen] = useState(false)
  const [lyricsOpen, setLyricsOpen] = useState(false)
  const [coverFailed, setCoverFailed] = useState(false)
  const activeLyricRef = useRef<HTMLButtonElement | null>(null)

  useEffect(() => {
    setCoverFailed(false)
  }, [currentTrack?.Id])

  const { data: lyricsData, isLoading: lyricsLoading } = useQuery({
    queryKey: ['audioLyrics', currentTrack?.Id],
    queryFn: () => api.getAudioLyrics(currentTrack!.Id),
    enabled: Boolean(currentTrack?.Id),
    staleTime: 300_000,
    retry: false,
  })

  const parsedLyrics = useMemo(() => normalizeLyricsResponse(lyricsData), [lyricsData])

  const activeLyricTimeSec = useMemo(() => {
    if (parsedLyrics.length === 0) return null
    let matchedTime: number | null = null
    for (let i = 0; i < parsedLyrics.length; i++) {
      const t = parsedLyrics[i].timeSec
      if (t != null && t <= currentTime + 0.15) {
        matchedTime = t
      }
    }
    return matchedTime
  }, [parsedLyrics, currentTime])

  const activeLyricIndex = useMemo(() => {
    if (activeLyricTimeSec == null) return -1
    return parsedLyrics.findIndex(
      (line) => line.timeSec != null && Math.abs(line.timeSec - activeLyricTimeSec) < 0.005,
    )
  }, [parsedLyrics, activeLyricTimeSec])

  // 当前歌词行变化时平滑滚动至居中
  useEffect(() => {
    if (!lyricsOpen || activeLyricIndex < 0) return
    const el = activeLyricRef.current
    if (el && typeof el.scrollIntoView === 'function') {
      el.scrollIntoView({ behavior: 'smooth', block: 'center' })
    }
  }, [activeLyricIndex, lyricsOpen])

  if (!currentTrack) {
    return null
  }

  const art = getTrackArtwork(currentTrack)
  const artistName = getTrackArtist(currentTrack)
  const effectiveDuration =
    duration > 0 ? duration : currentTrack.RunTimeTicks ? currentTrack.RunTimeTicks / 10_000_000 : 0
  const repeatLabel =
    repeatMode === 'one'
      ? '单曲循环'
      : repeatMode === 'all'
        ? '列表循环'
        : '关闭循环'

  return (
    <div
      data-testid="audio-player-bar"
      className="fixed inset-x-0 bottom-0 z-40 border-t border-white/10 bg-[#0d0d14]/95 shadow-[0_-8px_32px_rgba(0,0,0,0.65)] backdrop-blur-2xl"
    >
      {/* LRC 歌词同步滚动抽屉 */}
      {lyricsOpen && (
        <div
          data-testid="lyrics-drawer"
          className="border-b border-white/10 bg-[#0b0b12]/98 px-4 py-4 shadow-2xl backdrop-blur-2xl lg:px-8"
        >
          <div className="mx-auto max-w-3xl">
            <div className="mb-3 flex items-center justify-between border-b border-white/8 pb-2.5">
              <div className="min-w-0">
                <h3 className="truncate text-sm font-semibold text-zinc-100">
                  同步歌词 · {currentTrack.Name}
                </h3>
                <p className="truncate text-xs text-zinc-400">
                  {artistName}
                  {currentTrack.Album ? ` · ${currentTrack.Album}` : ''}
                </p>
              </div>
              <button
                type="button"
                onClick={() => setLyricsOpen(false)}
                aria-label="收起歌词面板"
                className="rounded-lg p-1.5 text-zinc-400 transition-colors hover:bg-white/10 hover:text-zinc-100"
              >
                <X className="h-4 w-4" />
              </button>
            </div>

            <div className="max-h-56 space-y-1.5 overflow-y-auto px-2 py-2 text-center">
              {lyricsLoading ? (
                <div className="flex items-center justify-center gap-2 py-8 text-xs text-zinc-400">
                  <Loader2 className="h-4 w-4 animate-spin text-amber-400" />
                  <span>正在加载同步歌词...</span>
                </div>
              ) : parsedLyrics.length === 0 ? (
                <p className="py-8 text-xs text-zinc-500">暂无同步歌词</p>
              ) : (
                parsedLyrics.map((line, idx) => {
                  const isActive =
                    activeLyricTimeSec != null &&
                    line.timeSec != null &&
                    Math.abs(line.timeSec - activeLyricTimeSec) < 0.005
                  const isScrollAnchor = idx === activeLyricIndex
                  return (
                    <button
                      key={`${idx}-${line.timeSec ?? 'plain'}`}
                      ref={isScrollAnchor ? activeLyricRef : null}
                      type="button"
                      aria-current={isActive ? 'true' : undefined}
                      disabled={line.timeSec == null}
                      onClick={() => {
                        if (line.timeSec != null) {
                          seek(line.timeSec)
                        }
                      }}
                      className={`block w-full rounded-xl px-3 py-1.5 text-center transition-all ${
                        isActive
                          ? 'bg-amber-500/15 text-base font-semibold text-amber-300'
                          : 'text-sm text-zinc-400 hover:bg-white/[0.04] hover:text-zinc-200'
                      } ${line.timeSec != null ? 'cursor-pointer' : 'cursor-default'}`}
                    >
                      {line.text || '···'}
                    </button>
                  )
                })
              )}
            </div>
          </div>
        </div>
      )}

      {/* 播放队列弹层 */}
      {queueOpen && (
        <div
          data-testid="queue-popover"
          className="fixed bottom-24 right-4 z-50 w-80 max-w-[calc(100vw-2rem)] overflow-hidden rounded-2xl border border-white/12 bg-[#11111a]/98 shadow-2xl backdrop-blur-2xl sm:w-96"
        >
          <div className="flex items-center justify-between border-b border-white/8 px-4 py-3">
            <div className="flex items-center gap-2">
              <ListMusic className="h-4 w-4 text-amber-400" />
              <span className="text-sm font-semibold text-zinc-100">
                播放队列 ({queue.length})
              </span>
            </div>
            <button
              type="button"
              onClick={() => setQueueOpen(false)}
              aria-label="关闭播放队列"
              className="rounded-lg p-1 text-zinc-400 hover:bg-white/10 hover:text-zinc-100"
            >
              <X className="h-4 w-4" />
            </button>
          </div>
          <div className="max-h-72 divide-y divide-white/5 overflow-y-auto p-1.5">
            {queue.map((track, idx) => {
              const isCurrent = idx === currentIndex
              return (
                <div
                  key={`${track.Id}-${idx}`}
                  className={`group flex items-center gap-2.5 rounded-xl px-2.5 py-2 transition-colors ${
                    isCurrent
                      ? 'bg-amber-500/15 text-amber-300'
                      : 'text-zinc-300 hover:bg-white/[0.05]'
                  }`}
                >
                  <button
                    type="button"
                    onClick={() => selectTrackAt(idx)}
                    className="flex min-w-0 flex-1 items-center gap-2.5 text-left"
                  >
                    <span className="w-5 shrink-0 text-center text-xs text-zinc-500">
                      {isCurrent ? '▶' : idx + 1}
                    </span>
                    <div className="min-w-0 flex-1">
                      <p className="truncate text-xs font-medium">{track.Name}</p>
                      <p className="truncate text-[11px] text-zinc-500">
                        {getTrackArtist(track)}
                      </p>
                    </div>
                  </button>
                  {track.RunTimeTicks ? (
                    <span className="shrink-0 text-[11px] text-zinc-500">
                      {formatAudioTime(track.RunTimeTicks / 10_000_000)}
                    </span>
                  ) : null}
                  <button
                    type="button"
                    onClick={() => removeFromQueue(idx)}
                    aria-label={`从队列移除 ${track.Name}`}
                    className="rounded-md p-1 text-zinc-500 opacity-70 transition-opacity hover:bg-red-500/15 hover:text-red-400 sm:opacity-0 sm:group-hover:opacity-100"
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </button>
                </div>
              )
            })}
          </div>
        </div>
      )}

      {/* 音频播放错误提示条 */}
      {playbackError && (
        <div
          data-testid="audio-playback-error"
          role="alert"
          className="flex items-center justify-between gap-3 border-b border-red-500/25 bg-red-500/12 px-4 py-1.5 text-xs text-red-200 lg:px-6"
        >
          <div className="flex min-w-0 items-center gap-2">
            <AlertCircle className="h-3.5 w-3.5 shrink-0 text-red-400" />
            <span className="truncate">{playbackError}</span>
          </div>
          <div className="flex shrink-0 items-center gap-1.5">
            <button
              type="button"
              onClick={retry}
              className="inline-flex items-center gap-1 rounded-md bg-red-500/20 px-2 py-0.5 text-[11px] font-medium text-red-100 transition-colors hover:bg-red-500/35"
            >
              <RotateCw className="h-3 w-3" />
              <span>重试</span>
            </button>
            <button
              type="button"
              onClick={clearError}
              aria-label="关闭错误提示"
              className="rounded-md p-0.5 text-red-300 transition-colors hover:bg-red-500/20 hover:text-red-100"
            >
              <X className="h-3.5 w-3.5" />
            </button>
          </div>
        </div>
      )}

      {/* 底部主控制栏 */}
      <div className="mx-auto flex max-w-screen-2xl flex-wrap items-center justify-between gap-3 px-4 py-2.5 lg:flex-nowrap lg:px-6">
        {/* 左侧：封面与曲目信息 */}
        <div className="flex min-w-0 items-center gap-3 lg:w-72">
          <div className="flex h-11 w-11 shrink-0 items-center justify-center overflow-hidden rounded-xl bg-zinc-900 ring-1 ring-white/10">
            {art.primary && !coverFailed ? (
              <img
                src={art.primary}
                alt={currentTrack.Name}
                onError={(e) => {
                  const el = e.currentTarget
                  if (art.fallback && !el.dataset.fallbackApplied && el.src !== art.fallback) {
                    el.dataset.fallbackApplied = '1'
                    el.src = art.fallback
                    return
                  }
                  setCoverFailed(true)
                }}
                className="h-full w-full object-cover"
              />
            ) : (
              <Music className="h-5 w-5 text-amber-400/80" />
            )}
          </div>
          <div className="min-w-0">
            <Link
              to={`/item/${currentTrack.AlbumId || currentTrack.Id}`}
              className="block truncate text-sm font-semibold text-zinc-100 transition-colors hover:text-amber-300"
              title={currentTrack.Name}
            >
              {currentTrack.Name}
            </Link>
            <p className="truncate text-xs text-zinc-400">
              {artistName}
              {currentTrack.Album ? ` · ${currentTrack.Album}` : ''}
            </p>
          </div>
        </div>

        {/* 中央：播控按钮与可拖动进度条 */}
        <div className="flex w-full flex-col items-center gap-1.5 lg:max-w-xl lg:flex-1">
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={toggleShuffle}
              aria-label="随机播放"
              title={shuffle ? '已开启随机播放' : '开启随机播放'}
              className={`rounded-lg p-1.5 transition-colors ${
                shuffle
                  ? 'bg-amber-500/15 text-amber-400'
                  : 'text-zinc-400 hover:bg-white/8 hover:text-zinc-100'
              }`}
            >
              <Shuffle className="h-3.5 w-3.5" />
            </button>
            <button
              type="button"
              onClick={prev}
              aria-label="上一曲"
              title="上一曲"
              className="rounded-lg p-1.5 text-zinc-300 transition-colors hover:bg-white/8 hover:text-white"
            >
              <SkipBack className="h-4 w-4" />
            </button>
            <button
              type="button"
              onClick={togglePlay}
              aria-label={isPlaying ? '暂停' : '播放'}
              title={isPlaying ? '暂停' : '播放'}
              className="flex h-9 w-9 items-center justify-center rounded-full bg-amber-500 text-zinc-950 shadow-md shadow-amber-500/30 transition-transform hover:bg-amber-400 active:scale-95"
            >
              {isPlaying ? (
                <Pause className="h-4 w-4 fill-current" />
              ) : (
                <Play className="ml-0.5 h-4 w-4 fill-current" />
              )}
            </button>
            <button
              type="button"
              onClick={next}
              aria-label="下一曲"
              title="下一曲"
              className="rounded-lg p-1.5 text-zinc-300 transition-colors hover:bg-white/8 hover:text-white"
            >
              <SkipForward className="h-4 w-4" />
            </button>
            <button
              type="button"
              onClick={cycleRepeatMode}
              aria-label={`循环模式：${repeatLabel}`}
              title={repeatLabel}
              className={`rounded-lg p-1.5 transition-colors ${
                repeatMode !== 'off'
                  ? 'bg-amber-500/15 text-amber-400'
                  : 'text-zinc-400 hover:bg-white/8 hover:text-zinc-100'
              }`}
            >
              {repeatMode === 'one' ? (
                <Repeat1 className="h-3.5 w-3.5" />
              ) : (
                <Repeat className="h-3.5 w-3.5" />
              )}
            </button>
          </div>

          <div className="flex w-full items-center gap-2.5 text-[11px] tabular-nums text-zinc-400">
            <span className="w-10 text-right">{formatAudioTime(currentTime)}</span>
            <input
              type="range"
              aria-label="播放进度"
              min={0}
              max={effectiveDuration > 0 ? effectiveDuration : 100}
              step={0.5}
              value={Math.min(currentTime, effectiveDuration > 0 ? effectiveDuration : 100)}
              onChange={(e) => seek(Number(e.target.value))}
              className="h-1 flex-1 cursor-pointer accent-amber-500"
            />
            <span className="w-10">{formatAudioTime(effectiveDuration)}</span>
          </div>
        </div>

        {/* 右侧：歌词、播放队列、音量与关闭 */}
        <div className="flex items-center gap-1.5 lg:w-72 lg:justify-end">
          <button
            type="button"
            onClick={() => {
              setLyricsOpen((v) => {
                const nextOpen = !v
                if (nextOpen) setQueueOpen(false)
                return nextOpen
              })
            }}
            aria-label="同步歌词"
            title="同步歌词"
            className={`inline-flex items-center gap-1 rounded-lg px-2.5 py-1.5 text-xs font-medium transition-colors ${
              lyricsOpen
                ? 'bg-amber-500/20 text-amber-300 ring-1 ring-amber-400/40'
                : 'text-zinc-300 hover:bg-white/8 hover:text-white'
            }`}
          >
            <FileText className="h-3.5 w-3.5" />
            <span className="hidden sm:inline">歌词</span>
          </button>

          <button
            type="button"
            onClick={() => {
              setQueueOpen((v) => {
                const nextOpen = !v
                if (nextOpen) setLyricsOpen(false)
                return nextOpen
              })
            }}
            aria-label="播放队列"
            title="播放队列"
            className={`inline-flex items-center gap-1 rounded-lg px-2.5 py-1.5 text-xs font-medium transition-colors ${
              queueOpen
                ? 'bg-amber-500/20 text-amber-300 ring-1 ring-amber-400/40'
                : 'text-zinc-300 hover:bg-white/8 hover:text-white'
            }`}
          >
            <ListMusic className="h-3.5 w-3.5" />
            <span className="hidden sm:inline">{queue.length}</span>
          </button>

          <div className="hidden items-center gap-1.5 sm:flex">
            <button
              type="button"
              onClick={toggleMute}
              aria-label={muted || volume === 0 ? '取消静音' : '静音'}
              title={muted || volume === 0 ? '取消静音' : '静音'}
              className="rounded-lg p-1.5 text-zinc-400 transition-colors hover:bg-white/8 hover:text-zinc-100"
            >
              {muted || volume === 0 ? (
                <VolumeX className="h-4 w-4" />
              ) : (
                <Volume2 className="h-4 w-4" />
              )}
            </button>
            <input
              type="range"
              aria-label="音量"
              min={0}
              max={1}
              step={0.05}
              value={muted ? 0 : volume}
              onChange={(e) => setVolume(Number(e.target.value))}
              className="h-1 w-20 cursor-pointer accent-amber-500"
            />
          </div>

          <button
            type="button"
            onClick={close}
            aria-label="关闭音乐播放器"
            title="关闭播放器"
            className="rounded-lg p-1.5 text-zinc-400 transition-colors hover:bg-red-500/15 hover:text-red-400"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
      </div>
    </div>
  )
}
