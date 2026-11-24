import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  ArrowDownWideNarrow,
  ArrowUpNarrowWide,
  Flame,
  Loader2,
  RefreshCw,
  Sparkles,
} from 'lucide-react'
import { api } from '../api/client'
import { MediaCard } from '../components/MediaCard'
import { useAuth } from '../context/AuthContext'
import { FeedbackBanner, formatDateTime, formatRelativeTime } from './admin/adminUtils'

type TrendingTypeFilter = 'Movie,Series' | 'Movie' | 'Series'

const TYPE_PILLS: Array<{ value: TrendingTypeFilter; label: string }> = [
  { value: 'Movie,Series', label: '全部' },
  { value: 'Movie', label: '电影' },
  { value: 'Series', label: '剧集' },
]

const SORT_OPTIONS = [
  { value: 'CommunityRating', label: '评分最高' },
  { value: 'DateCreated', label: '添加时间' },
  { value: 'PremiereDate', label: '上映年份' },
  { value: 'SortName', label: '名称排序' },
]

export function TrendingPage() {
  const { auth, isAdmin } = useAuth()
  const queryClient = useQueryClient()
  const userId = auth!.userId

  const [filterType, setFilterType] = useState<TrendingTypeFilter>('Movie,Series')
  const [sortBy, setSortBy] = useState('CommunityRating')
  const [sortOrder, setSortOrder] = useState<'Ascending' | 'Descending'>('Descending')
  const [feedback, setFeedback] = useState<{ type: 'success' | 'error'; message: string } | null>(
    null,
  )

  const { data: rankings } = useQuery({
    queryKey: ['rankings'],
    queryFn: () => api.getRankingsSummary(),
  })

  const { data, isLoading, isFetching } = useQuery({
    queryKey: ['trendingItems', userId, filterType, sortBy, sortOrder],
    queryFn: () =>
      api.getItems(userId, {
        tags: 'Trending',
        includeItemTypes: filterType,
        recursive: true,
        sortBy,
        sortOrder,
        limit: 60,
      }),
    placeholderData: (prev) => prev,
  })

  const refreshMutation = useMutation({
    mutationFn: () => api.refreshRankings(),
    onSuccess: async (res) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['rankings'] }),
        queryClient.invalidateQueries({ queryKey: ['trendingItems'] }),
        queryClient.invalidateQueries({ queryKey: ['items'] }),
        queryClient.invalidateQueries({ queryKey: ['tags'] }),
      ])
      const trendCount = res.tmdb_trending_count ?? 0
      setFeedback({
        type: 'success',
        message: `热门榜同步完成${trendCount > 0 ? `（TMDB 热门 ${trendCount} 条）` : ''}：库内命中 ${res.matched_items} 部媒体，新增打标 ${res.tags_added} 次`,
      })
    },
    onError: (err) => {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '同步热门榜失败',
      })
    },
  })

  const items = data?.Items || []
  const total = data?.TotalRecordCount ?? items.length
  const trendingChart = rankings?.tmdb_trending

  return (
    <div className="space-y-6 px-4 pt-6 lg:px-8">
      {/* 琥珀火焰渐变头部 */}
      <section className="relative overflow-hidden rounded-3xl border border-orange-500/25 bg-gradient-to-br from-orange-950/50 via-amber-950/30 to-zinc-950/95 p-6 shadow-2xl shadow-black/40 sm:p-8">
        <div className="pointer-events-none absolute -right-12 -top-12 h-56 w-56 rounded-full bg-gradient-to-br from-amber-500/25 to-orange-600/10 blur-3xl" />

        <div className="relative z-10 flex flex-col gap-5 md:flex-row md:items-center md:justify-between">
          <div className="space-y-2">
            <div className="inline-flex items-center gap-1.5 rounded-full border border-orange-400/40 bg-orange-500/15 px-3 py-1 text-xs font-semibold text-orange-200">
              <Flame className="h-3.5 w-3.5 text-orange-400" />
              <span>TMDB 全球热门趋势</span>
            </div>
            <h1 className="text-2xl font-bold tracking-tight text-white sm:text-3xl">
              热门趋势
            </h1>
            <p className="max-w-2xl text-xs leading-relaxed text-zinc-300 sm:text-sm">
              自动匹配 TMDB 全球热门电影与剧集榜单，为你聚合媒体库中正在热议的高人气作品。
            </p>

            <div className="flex flex-wrap items-center gap-3 pt-1 text-xs text-zinc-400">
              <span className="inline-flex items-center gap-1.5 rounded-lg bg-white/[0.06] px-2.5 py-1 text-zinc-200">
                <Sparkles className="h-3.5 w-3.5 text-amber-400" />
                库内命中 <strong className="text-amber-300">{total}</strong> 部
              </span>
              {trendingChart && (
                <>
                  <span className="rounded-lg bg-white/[0.04] px-2.5 py-1">
                    榜单总数：<span className="text-zinc-200">{trendingChart.count}</span> 条
                  </span>
                  <span
                    className="rounded-lg bg-white/[0.04] px-2.5 py-1"
                    title={formatDateTime(trendingChart.updated_at)}
                  >
                    最近同步：
                    <span className="text-zinc-200">
                      {formatRelativeTime(trendingChart.updated_at)}
                    </span>
                  </span>
                </>
              )}
            </div>
          </div>

          {isAdmin && (
            <button
              type="button"
              disabled={refreshMutation.isPending}
              onClick={() => refreshMutation.mutate()}
              className="inline-flex shrink-0 items-center justify-center gap-2 rounded-xl bg-gradient-to-r from-amber-500 to-orange-500 px-5 py-2.5 text-sm font-semibold text-zinc-950 shadow-lg shadow-orange-500/25 transition-all hover:from-amber-400 hover:to-orange-400 disabled:opacity-60"
            >
              <RefreshCw
                className={`h-4 w-4 ${refreshMutation.isPending ? 'animate-spin' : ''}`}
              />
              <span>{refreshMutation.isPending ? '正在同步热门榜...' : '立即同步热门榜'}</span>
            </button>
          )}
        </div>
      </section>

      <FeedbackBanner
        message={feedback?.message}
        type={feedback?.type}
        onClose={() => setFeedback(null)}
      />

      {/* 筛选与排序工具栏 */}
      <div className="flex flex-col gap-3 border-b border-white/8 pb-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="inline-flex items-center gap-1.5 rounded-xl border border-white/10 bg-zinc-950/80 p-1">
          {TYPE_PILLS.map((pill) => {
            const active = filterType === pill.value
            return (
              <button
                key={pill.value}
                type="button"
                onClick={() => setFilterType(pill.value)}
                className={`rounded-lg px-3.5 py-1.5 text-xs font-medium transition-all ${
                  active
                    ? 'bg-amber-500 text-zinc-950 shadow-sm'
                    : 'text-zinc-400 hover:bg-white/[0.05] hover:text-zinc-100'
                }`}
              >
                {pill.label}
              </button>
            )
          })}
        </div>

        <div className="flex flex-wrap items-center gap-2.5">
          {isFetching && !isLoading && (
            <span className="text-xs text-zinc-500">正在更新...</span>
          )}
          <div className="flex items-center gap-1.5 rounded-xl border border-white/10 bg-white/[0.04] px-3 py-1.5 text-xs">
            <select
              value={sortBy}
              onChange={(e) => setSortBy(e.target.value)}
              aria-label="排序方式"
              className="bg-transparent text-zinc-200 outline-none"
            >
              {SORT_OPTIONS.map((opt) => (
                <option key={opt.value} value={opt.value} className="bg-zinc-900 text-zinc-100">
                  按{opt.label}
                </option>
              ))}
            </select>
          </div>

          <button
            type="button"
            onClick={() =>
              setSortOrder((prev) => (prev === 'Descending' ? 'Ascending' : 'Descending'))
            }
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

      {/* 内容网格 */}
      {isLoading ? (
        <div className="flex h-64 items-center justify-center">
          <Loader2 className="h-7 w-7 animate-spin text-amber-400" />
        </div>
      ) : items.length === 0 ? (
        <div className="rounded-2xl border border-dashed border-white/12 bg-white/[0.02] px-6 py-16 text-center">
          <div className="mx-auto mb-3 flex h-12 w-12 items-center justify-center rounded-2xl bg-orange-500/15 text-orange-400 ring-1 ring-orange-500/30">
            <Flame className="h-6 w-6" />
          </div>
          <h2 className="text-base font-semibold text-zinc-200">
            媒体库中暂未匹配到热门榜单作品
          </h2>
          <p className="mx-auto mt-2 max-w-md text-xs leading-relaxed text-zinc-400">
            {isAdmin
              ? '请点击右上角「立即同步热门榜」，系统将拉取 TMDB 热门趋势并为库内匹配的电影/剧集自动打上 Trending 标签。'
              : '请联系管理员在控制台或热门页面同步 TMDB 热门榜单缓存。'}
          </p>
        </div>
      ) : (
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7">
          {items.map((item) => (
            <MediaCard key={item.Id} item={item} />
          ))}
        </div>
      )}
    </div>
  )
}
