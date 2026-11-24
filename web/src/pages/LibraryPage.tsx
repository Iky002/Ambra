import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ArrowDownWideNarrow, ArrowUpNarrowWide, Filter, Loader2 } from 'lucide-react'
import { useParams } from 'react-router-dom'
import { api } from '../api/client'
import { MediaCard } from '../components/MediaCard'
import { useAuth } from '../context/AuthContext'

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
  const { auth } = useAuth()
  const userId = auth!.userId

  const [sortBy, setSortBy] = useState('DateCreated')
  const [sortOrder, setSortOrder] = useState<'Ascending' | 'Descending'>('Descending')
  const [statusFilter, setStatusFilter] = useState('')
  const [page, setPage] = useState(0)

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

  const items = data?.Items || []
  const total = data?.TotalRecordCount || 0
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))

  return (
    <div className="space-y-6 px-4 pt-6 lg:px-8">
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
          {items.map((item) => (
            <MediaCard key={item.Id} item={item} />
          ))}
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
    </div>
  )
}
