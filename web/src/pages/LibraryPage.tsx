import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  ArrowDownWideNarrow,
  ArrowUpNarrowWide,
  Filter,
  GitMerge,
  Layers,
  Loader2,
  X,
} from 'lucide-react'
import { useParams } from 'react-router-dom'
import { api } from '../api/client'
import type { BaseItemDto } from '../api/types'
import { MediaCard } from '../components/MediaCard'
import { useAuth } from '../context/AuthContext'
import { FeedbackBanner } from './admin/adminUtils'

const SORT_OPTIONS = [
  { value: 'DateCreated', label: '添加时间' },
  { value: 'SortName', label: '名称排序' },
  { value: 'CommunityRating', label: '评分最高' },
  { value: 'PremiereDate', label: '上映年份' },
  { value: 'PlayCount', label: '播放次数' },
]

const FILTER_OPTIONS = [
  { value: '', label: '全部状态' },
  { value: 'IsUnplayed', label: '仅看未播' },
  { value: 'IsPlayed', label: '仅看已播' },
  { value: 'IsFavorite', label: '仅看收藏' },
]

const PAGE_SIZE = 36

export function LibraryPage() {
  const { id } = useParams<{ id: string }>()
  const { auth, isAdmin } = useAuth()
  const queryClient = useQueryClient()
  const userId = auth!.userId

  const [sortBy, setSortBy] = useState('DateCreated')
  const [sortOrder, setSortOrder] = useState<'Ascending' | 'Descending'>('Descending')
  const [statusFilter, setStatusFilter] = useState('')
  const [page, setPage] = useState(0)

  // 管理员手动多版本合并模式
  const [mergeMode, setMergeMode] = useState(false)
  const [selectedIds, setSelectedIds] = useState<string[]>([])
  const [banner, setBanner] = useState<{ type: 'success' | 'error'; message: string } | null>(null)

  useEffect(() => {
    setMergeMode(false)
    setSelectedIds([])
    setPage(0)
  }, [id])

  useEffect(() => {
    if (!banner) return
    const timer = window.setTimeout(() => setBanner(null), 4500)
    return () => window.clearTimeout(timer)
  }, [banner])

  const { data: libraryInfo } = useQuery({
    queryKey: ['item', userId, id],
    queryFn: () => api.getItem(userId, id!),
    enabled: Boolean(id),
  })

  const { data, isLoading, isFetching } = useQuery({
    queryKey: ['libraryItems', userId, id, sortBy, sortOrder, statusFilter, page],
    queryFn: () =>
      api.getItems(userId, {
        parentId: id,
        includeItemTypes: 'Movie,Series,MusicAlbum,Video',
        sortBy,
        sortOrder,
        filters: statusFilter || undefined,
        startIndex: page * PAGE_SIZE,
        limit: PAGE_SIZE,
      }),
    enabled: Boolean(id),
    placeholderData: (prev) => prev,
  })

  const mergeMutation = useMutation({
    mutationFn: (ids: string[]) => api.mergeItemVersions(ids),
    onSuccess: async (_data, ids) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['libraryItems'] }),
        queryClient.invalidateQueries({ queryKey: ['latestItems'] }),
        queryClient.invalidateQueries({ queryKey: ['item'] }),
        queryClient.invalidateQueries({ queryKey: ['items'] }),
      ])
      setMergeMode(false)
      setSelectedIds([])
      setBanner({ type: 'success', message: `已成功将 ${ids.length} 项媒体合并为多版本` })
    },
    onError: (err) => {
      setBanner({
        type: 'error',
        message: err instanceof Error ? err.message : '合并多版本失败',
      })
    },
  })

  const handleToggleSelect = (item: BaseItemDto) => {
    setSelectedIds((prev) =>
      prev.includes(item.Id) ? prev.filter((x) => x !== item.Id) : [...prev, item.Id],
    )
  }

  const items = data?.Items || []
  const total = data?.TotalRecordCount || 0
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))

  return (
    <div className={`space-y-6 px-4 pt-6 lg:px-8 ${mergeMode ? 'pb-24' : ''}`}>
      {/* 头部标题与筛选栏 */}
      <div className="flex flex-col gap-4 border-b border-white/8 pb-5 md:flex-row md:items-center md:justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-white">
            {libraryInfo?.Name || '媒体库'}
          </h1>
          <p className="mt-1 text-xs text-zinc-400">
            共 {total} 部媒体{isFetching && !isLoading ? ' · 正在更新...' : ''}
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-2.5">
          {/* 管理员合并多版本切换 */}
          {isAdmin && (
            <button
              type="button"
              onClick={() => {
                setMergeMode((prev) => {
                  const next = !prev
                  if (!next) setSelectedIds([])
                  return next
                })
              }}
              className={`inline-flex items-center gap-1.5 rounded-xl border px-3 py-1.5 text-xs font-medium transition-all ${
                mergeMode
                  ? 'border-amber-400/70 bg-amber-500/20 text-amber-300 ring-1 ring-amber-400/35'
                  : 'border-white/10 bg-white/[0.04] text-zinc-200 hover:border-amber-400/40 hover:bg-white/10 hover:text-amber-200'
              }`}
            >
              <Layers className="h-3.5 w-3.5 text-amber-400" />
              <span>{mergeMode ? '退出合并模式' : '合并多版本'}</span>
            </button>
          )}

          {/* 状态过滤 */}
          <div className="flex items-center gap-1.5 rounded-xl border border-white/10 bg-white/[0.04] px-3 py-1.5 text-xs">
            <Filter className="h-3.5 w-3.5 text-amber-400" />
            <select
              value={statusFilter}
              onChange={(e) => {
                setStatusFilter(e.target.value)
                setPage(0)
              }}
              className="bg-transparent text-zinc-200 outline-none"
            >
              {FILTER_OPTIONS.map((opt) => (
                <option key={opt.value} value={opt.value} className="bg-zinc-900 text-zinc-100">
                  {opt.label}
                </option>
              ))}
            </select>
          </div>

          {/* 排序字段 */}
          <div className="flex items-center gap-1.5 rounded-xl border border-white/10 bg-white/[0.04] px-3 py-1.5 text-xs">
            <select
              value={sortBy}
              onChange={(e) => {
                setSortBy(e.target.value)
                setPage(0)
              }}
              className="bg-transparent text-zinc-200 outline-none"
            >
              {SORT_OPTIONS.map((opt) => (
                <option key={opt.value} value={opt.value} className="bg-zinc-900 text-zinc-100">
                  按{opt.label}
                </option>
              ))}
            </select>
          </div>

          {/* 升序/降序切换 */}
          <button
            type="button"
            onClick={() => {
              setSortOrder((prev) => (prev === 'Descending' ? 'Ascending' : 'Descending'))
              setPage(0)
            }}
            className="flex items-center gap-1.5 rounded-xl border border-white/10 bg-white/[0.04] px-3 py-1.5 text-xs text-zinc-200 transition-colors hover:bg-white/10"
            title="切换排序方向"
          >
            {sortOrder === 'Descending' ? (
              <>
                <ArrowDownWideNarrow className="h-3.5 w-3.5 text-amber-400" />
                <span>降序</span>
              </>
            ) : (
              <>
                <ArrowUpNarrowWide className="h-3.5 w-3.5 text-amber-400" />
                <span>升序</span>
              </>
            )}
          </button>
        </div>
      </div>

      {/* 合并操作反馈提示 */}
      <FeedbackBanner
        message={banner?.message}
        type={banner?.type}
        onClose={() => setBanner(null)}
      />

      {/* 多选合并提示条 */}
      {mergeMode && (
        <div className="flex items-center justify-between gap-3 rounded-xl border border-amber-500/30 bg-amber-500/10 px-4 py-2.5 text-xs text-amber-200">
          <span>
            多版本合并模式已开启：请点击选择至少 2 部电影或视频条目，首选项将作为合并后的主版本保留元数据。
          </span>
        </div>
      )}

      {/* 海报墙网格 */}
      {isLoading ? (
        <div className="flex h-64 items-center justify-center">
          <Loader2 className="h-7 w-7 animate-spin text-amber-400" />
        </div>
      ) : items.length === 0 ? (
        <div className="rounded-2xl border border-white/8 bg-white/[0.02] py-20 text-center text-sm text-zinc-500">
          当前筛选条件下没有找到媒体项目
        </div>
      ) : (
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7">
          {items.map((item) => {
            const isMergeable = item.Type === 'Movie' || item.Type === 'Video'
            const canSelect = mergeMode && isMergeable
            return (
              <MediaCard
                key={item.Id}
                item={item}
                selectable={canSelect}
                selected={canSelect && selectedIds.includes(item.Id)}
                onToggleSelect={handleToggleSelect}
                className={mergeMode && !isMergeable ? 'pointer-events-none opacity-45' : ''}
              />
            )
          })}
        </div>
      )}

      {/* 分页控件 */}
      {totalPages > 1 && (
        <div className="flex items-center justify-center gap-3 pt-4">
          <button
            type="button"
            disabled={page === 0}
            onClick={() => {
              setPage((p) => Math.max(0, p - 1))
              window.scrollTo({ top: 0, behavior: 'smooth' })
            }}
            className="rounded-xl border border-white/10 bg-white/[0.04] px-4 py-2 text-xs font-medium text-zinc-200 transition-colors hover:bg-white/10 disabled:opacity-40"
          >
            上一页
          </button>
          <span className="text-xs text-zinc-400">
            第 {page + 1} / {totalPages} 页
          </span>
          <button
            type="button"
            disabled={page + 1 >= totalPages}
            onClick={() => {
              setPage((p) => Math.min(totalPages - 1, p + 1))
              window.scrollTo({ top: 0, behavior: 'smooth' })
            }}
            className="rounded-xl border border-white/10 bg-white/[0.04] px-4 py-2 text-xs font-medium text-zinc-200 transition-colors hover:bg-white/10 disabled:opacity-40"
          >
            下一页
          </button>
        </div>
      )}

      {/* 底部悬浮多版本合并操作条 */}
      {mergeMode && (
        <div className="fixed inset-x-0 bottom-5 z-40 flex justify-center px-4">
          <div className="flex flex-wrap items-center gap-3 rounded-2xl border border-amber-500/35 bg-[#12121a]/95 px-5 py-3 shadow-2xl shadow-black/75 backdrop-blur-xl">
            <span className="text-sm font-medium text-zinc-200">
              已选 <strong className="text-amber-400">{selectedIds.length}</strong> 项
            </span>
            <div className="h-4 w-px bg-white/15" />
            <button
              type="button"
              disabled={selectedIds.length === 0 || mergeMutation.isPending}
              onClick={() => setSelectedIds([])}
              className="rounded-xl border border-white/10 bg-white/[0.04] px-3 py-1.5 text-xs font-medium text-zinc-300 transition-colors hover:bg-white/10 disabled:opacity-40"
            >
              清空
            </button>
            <button
              type="button"
              disabled={mergeMutation.isPending}
              onClick={() => {
                setMergeMode(false)
                setSelectedIds([])
              }}
              className="inline-flex items-center gap-1 rounded-xl border border-white/10 bg-white/[0.04] px-3 py-1.5 text-xs font-medium text-zinc-300 transition-colors hover:bg-white/10 disabled:opacity-40"
            >
              <X className="h-3.5 w-3.5" />
              <span>退出</span>
            </button>
            <button
              type="button"
              disabled={selectedIds.length < 2 || mergeMutation.isPending}
              onClick={() => mergeMutation.mutate(selectedIds)}
              className="inline-flex items-center gap-1.5 rounded-xl bg-amber-500 px-4 py-1.5 text-xs font-semibold text-zinc-950 shadow-lg shadow-amber-500/25 transition-colors hover:bg-amber-400 disabled:opacity-45"
            >
              {mergeMutation.isPending ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <GitMerge className="h-3.5 w-3.5" />
              )}
              <span>合并为多版本</span>
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
