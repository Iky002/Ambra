import { useEffect, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { CheckCircle2, FolderSync, Loader2, Pause, Play, Sparkles, XCircle } from 'lucide-react'
import { api } from '../api/client'
import type { LibraryScanProgress, ScanProgressSnapshot } from '../api/types'

export interface ScanProgressBarProps {
  variant?: 'banner' | 'card'
  enabled?: boolean
  className?: string
}

function getPhaseLabel(phase?: string): string {
  switch (phase) {
    case 'discovering':
      return '文件发现'
    case 'enriching':
      return '元数据刮削与海报下载'
    case 'completed':
      return '扫描完成'
    default:
      return '媒体库扫描'
  }
}

function clampPercentage(percentage?: number, processed = 0, total = 0): number {
  const raw =
    typeof percentage === 'number' && !Number.isNaN(percentage) && percentage > 0
      ? percentage
      : total > 0
        ? (processed / total) * 100
        : 0
  return Math.min(100, Math.max(0, Math.round(raw)))
}

export function ScanProgressBar({
  variant = 'banner',
  enabled = true,
  className = '',
}: ScanProgressBarProps) {
  const queryClient = useQueryClient()
  const wasRunningRef = useRef(false)
  const lastActiveRef = useRef<ScanProgressSnapshot | null>(null)
  const [completedSnapshot, setCompletedSnapshot] = useState<ScanProgressSnapshot | null>(null)
  const [localPaused, setLocalPaused] = useState(false)
  const [controlBusy, setControlBusy] = useState<'pause' | 'cancel' | null>(null)

  const { data: progress } = useQuery({
    queryKey: ['scanProgress'],
    queryFn: () => api.getScanProgress(),
    enabled,
    retry: false,
    refetchInterval: (query) => (query.state.data?.running ? 1000 : 8000),
  })

  useEffect(() => {
    if (!progress) return

    if (progress.running) {
      wasRunningRef.current = true
      lastActiveRef.current = progress
      setCompletedSnapshot(null)
      if (typeof progress.paused === 'boolean') {
        setLocalPaused(progress.paused)
      }
      return
    }

    if (wasRunningRef.current && !progress.running) {
      wasRunningRef.current = false
      setLocalPaused(false)
      const prev = lastActiveRef.current
      setCompletedSnapshot(
        prev
          ? {
              ...prev,
              running: false,
              phase: 'completed',
              processed: prev.total > 0 ? prev.total : prev.processed,
              percentage: 100,
            }
          : {
              ...progress,
              running: false,
              phase: 'completed',
              percentage: 100,
            },
      )

      void Promise.all([
        queryClient.invalidateQueries({ queryKey: ['libraryItems'] }),
        queryClient.invalidateQueries({ queryKey: ['latestItems'] }),
        queryClient.invalidateQueries({ queryKey: ['admin-metadata-items'] }),
        queryClient.invalidateQueries({ queryKey: ['trendingItems'] }),
      ])

      const timer = window.setTimeout(() => {
        setCompletedSnapshot(null)
      }, 4000)
      return () => window.clearTimeout(timer)
    }
  }, [progress, queryClient])

  const active = progress?.running ? progress : completedSnapshot
  if (!active) return null

  const isCompleted = !active.running
  const isPaused = typeof active.paused === 'boolean' ? active.paused : localPaused
  const pct = isCompleted ? 100 : clampPercentage(active.percentage, active.processed, active.total)
  const phaseLabel = getPhaseLabel(active.phase)
  const libraries: LibraryScanProgress[] = active.libraries || []
  const countText = active.total > 0 ? `${active.processed} / ${active.total}` : `${active.processed}`

  const handleTogglePause = async () => {
    setControlBusy('pause')
    try {
      if (isPaused) {
        await api.resumeScan()
        setLocalPaused(false)
      } else {
        await api.pauseScan()
        setLocalPaused(true)
      }
      await queryClient.invalidateQueries({ queryKey: ['scanProgress'] })
    } catch {
      // 忽略控制失败，等待下次轮询同步真实状态
    } finally {
      setControlBusy(null)
    }
  }

  const handleCancelScan = async () => {
    setControlBusy('cancel')
    try {
      await api.cancelScan()
      setLocalPaused(false)
      await queryClient.invalidateQueries({ queryKey: ['scanProgress'] })
    } catch {
      // 忽略取消失败，等待下次轮询同步真实状态
    } finally {
      setControlBusy(null)
    }
  }

  if (variant === 'banner') {
    return (
      <div
        role="status"
        aria-live="polite"
        data-testid="scan-progress-banner"
        className={`border-b border-amber-500/20 bg-gradient-to-r from-amber-500/12 via-[#12121a] to-amber-500/10 px-4 py-2 text-xs text-zinc-100 lg:px-8 ${className}`}
      >
        <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1">
          <div className="flex min-w-0 flex-1 items-center gap-2">
            {isCompleted ? (
              <CheckCircle2 className="h-3.5 w-3.5 shrink-0 text-emerald-400" />
            ) : (
              <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin text-amber-400" />
            )}

            <span
              className={`shrink-0 rounded-md px-1.5 py-0.5 text-[11px] font-semibold ${
                isCompleted
                  ? 'bg-emerald-500/20 text-emerald-300'
                  : 'bg-amber-500/20 text-amber-300'
              }`}
            >
              {phaseLabel}
            </span>

            {active.library_name && (
              <span className="hidden shrink-0 text-zinc-400 sm:inline">
                [{active.library_name}]
              </span>
            )}

            <span className="truncate text-zinc-200">
              {isCompleted
                ? '媒体库扫描与元数据刮削已完成，已刷新最新片单与海报'
                : active.current_item
                  ? `当前：${active.current_item}`
                  : active.current_action || '正在扫描媒体库...'}
            </span>
          </div>

          <div className="flex shrink-0 items-center gap-2.5 font-mono text-[11px]">
            <span className="text-zinc-300">{countText}</span>
            <span
              className={`font-semibold ${isCompleted ? 'text-emerald-300' : 'text-amber-300'}`}
            >
              {pct}%
            </span>
          </div>
        </div>

        <div className="mt-1.5 h-1 w-full overflow-hidden rounded-full bg-white/10">
          <div
            className={`h-full rounded-full transition-all duration-300 ${
              isCompleted
                ? 'bg-emerald-400'
                : 'bg-gradient-to-r from-amber-500 via-amber-400 to-orange-400'
            }`}
            style={{ width: `${pct}%` }}
          />
        </div>
      </div>
    )
  }

  return (
    <div
      role="status"
      aria-live="polite"
      data-testid="scan-progress-card"
      className={`rounded-2xl border p-4 shadow-lg ${
        isCompleted
          ? 'border-emerald-500/30 bg-emerald-500/[0.06] shadow-emerald-500/5'
          : 'border-amber-500/25 bg-gradient-to-br from-amber-500/[0.08] via-white/[0.03] to-transparent shadow-amber-500/5'
      } ${className}`}
    >
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex min-w-0 items-center gap-2.5">
          <div
            className={`flex h-9 w-9 shrink-0 items-center justify-center rounded-xl ring-1 ${
              isCompleted
                ? 'bg-emerald-500/15 text-emerald-400 ring-emerald-500/30'
                : 'bg-amber-500/15 text-amber-400 ring-amber-500/30'
            }`}
          >
            {isCompleted ? (
              <CheckCircle2 className="h-4.5 w-4.5" />
            ) : (
              <Loader2 className="h-4.5 w-4.5 animate-spin" />
            )}
          </div>
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <h3 className="text-sm font-semibold text-zinc-100">
                {isCompleted ? '媒体库扫描与刮削已完成' : '媒体库扫描与刮削进行中'}
              </h3>
              <span
                className={`rounded-md px-2 py-0.5 text-[11px] font-medium ${
                  isCompleted
                    ? 'bg-emerald-500/20 text-emerald-300'
                    : 'bg-amber-500/20 text-amber-300'
                }`}
              >
                {phaseLabel}
              </span>
              {active.library_name && (
                <span className="inline-flex items-center gap-1 rounded-md bg-white/8 px-2 py-0.5 text-[11px] text-zinc-300">
                  <FolderSync className="h-3 w-3 text-amber-400" />
                  <span>{active.library_name}</span>
                </span>
              )}
            </div>
            <p className="mt-1 truncate text-xs text-zinc-300">
              {isCompleted ? (
                '已完成全部条目的索引与元数据刮削，页面数据已自动更新'
              ) : (
                <>
                  {active.current_action ? (
                    <span className="text-zinc-400">{active.current_action} · </span>
                  ) : null}
                  <span>
                    当前：{active.current_item || '正在准备扫描队列...'}
                  </span>
                </>
              )}
            </p>
          </div>
        </div>

        <div className="flex shrink-0 flex-wrap items-center gap-2">
          {!isCompleted && (
            <div className="flex items-center gap-1.5">
              <button
                type="button"
                disabled={controlBusy !== null}
                onClick={() => void handleTogglePause()}
                className="inline-flex items-center gap-1 rounded-lg border border-amber-500/30 bg-amber-500/15 px-2.5 py-1 text-xs font-medium text-amber-200 transition-colors hover:bg-amber-500/25 disabled:opacity-50"
              >
                {controlBusy === 'pause' ? (
                  <Loader2 className="h-3 w-3 animate-spin" />
                ) : isPaused ? (
                  <Play className="h-3 w-3 fill-current" />
                ) : (
                  <Pause className="h-3 w-3" />
                )}
                <span>{isPaused ? '恢复' : '暂停'}</span>
              </button>
              <button
                type="button"
                disabled={controlBusy !== null}
                onClick={() => void handleCancelScan()}
                className="inline-flex items-center gap-1 rounded-lg border border-red-500/30 bg-red-500/15 px-2.5 py-1 text-xs font-medium text-red-200 transition-colors hover:bg-red-500/25 disabled:opacity-50"
              >
                {controlBusy === 'cancel' ? (
                  <Loader2 className="h-3 w-3 animate-spin" />
                ) : (
                  <XCircle className="h-3 w-3" />
                )}
                <span>取消扫描</span>
              </button>
            </div>
          )}
          <div className="flex shrink-0 items-baseline gap-2 font-mono">
            <span className="text-xs text-zinc-400">{countText}</span>
            <span
              className={`text-lg font-bold ${isCompleted ? 'text-emerald-300' : 'text-amber-300'}`}
            >
              {pct}%
            </span>
          </div>
        </div>
      </div>

      <div className="mt-3.5 h-2 w-full overflow-hidden rounded-full bg-white/10">
        <div
          className={`h-full rounded-full transition-all duration-300 ${
            isCompleted
              ? 'bg-emerald-400'
              : 'bg-gradient-to-r from-amber-500 via-amber-400 to-orange-400'
          }`}
          style={{ width: `${pct}%` }}
        />
      </div>

      <div className="mt-3 flex flex-wrap items-center gap-2 text-[11px]">
        <span className="inline-flex items-center gap-1 rounded-lg border border-emerald-500/25 bg-emerald-500/10 px-2 py-0.5 font-medium text-emerald-300">
          新增 {active.added}
        </span>
        <span className="inline-flex items-center gap-1 rounded-lg border border-sky-500/25 bg-sky-500/10 px-2 py-0.5 font-medium text-sky-300">
          更新 {active.updated}
        </span>
        <span className="inline-flex items-center gap-1 rounded-lg border border-white/10 bg-white/[0.04] px-2 py-0.5 font-medium text-zinc-300">
          跳过 {active.skipped}
        </span>
        <span
          className={`inline-flex items-center gap-1 rounded-lg border px-2 py-0.5 font-medium ${
            active.failed > 0
              ? 'border-red-500/30 bg-red-500/15 text-red-300'
              : 'border-white/10 bg-white/[0.04] text-zinc-400'
          }`}
        >
          失败 {active.failed}
        </span>
      </div>

      {libraries.length > 1 && (
        <div className="mt-3.5 space-y-2 border-t border-white/8 pt-3">
          <div className="flex items-center gap-1.5 text-[11px] font-medium text-zinc-400">
            <Sparkles className="h-3 w-3 text-amber-400" />
            <span>各媒体库实时进度 ({libraries.length})</span>
          </div>
          <div className="grid gap-2 sm:grid-cols-2">
            {libraries.map((lib) => {
              const libPct = clampPercentage(lib.percentage, lib.processed, lib.total)
              return (
                <div
                  key={lib.library_id || lib.library_name}
                  className="rounded-xl border border-white/8 bg-black/25 px-3 py-2 text-xs"
                >
                  <div className="flex items-center justify-between gap-2">
                    <span className="truncate font-medium text-zinc-200">{lib.library_name}</span>
                    <span className="shrink-0 font-mono text-[11px] text-amber-300">
                      {lib.processed}/{lib.total} · {libPct}%
                    </span>
                  </div>
                  {lib.current_item && (
                    <p className="mt-0.5 truncate text-[11px] text-zinc-400">
                      {getPhaseLabel(lib.phase)} · {lib.current_item}
                    </p>
                  )}
                  <div className="mt-1.5 h-1 w-full overflow-hidden rounded-full bg-white/10">
                    <div
                      className="h-full rounded-full bg-amber-400 transition-all duration-300"
                      style={{ width: `${libPct}%` }}
                    />
                  </div>
                </div>
              )
            })}
          </div>
        </div>
      )}
    </div>
  )
}
