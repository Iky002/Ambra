import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  ArrowDownWideNarrow,
  ArrowRight,
  ArrowUpNarrowWide,
  Film,
  Flame,
  LayoutGrid,
  Loader2,
  RefreshCw,
  Sparkles,
  Tv,
} from 'lucide-react'
import { api } from '../api/client'
import type { BaseItemDto, RankingChartStatus } from '../api/types'
import { MediaCard } from '../components/MediaCard'
import { QueryErrorState } from '../components/QueryErrorState'
import { useAuth } from '../context/AuthContext'
import { useI18n } from '../i18n/I18nContext'
import { FeedbackBanner, formatDateTime, formatRelativeTime } from './admin/adminUtils'

type TrendingViewMode = 'overview' | 'tmdb' | 'douban_movie' | 'douban_tv'
type TmdbSubTypeFilter = 'Movie,Series' | 'Movie' | 'Series'

const VIEW_TABS: Array<{ value: TrendingViewMode; label: string }> = [
  { value: 'overview', label: '全部榜单（分栏总览）' },
  { value: 'tmdb', label: 'TMDB 热门' },
  { value: 'douban_movie', label: '豆瓣热门电影' },
  { value: 'douban_tv', label: '豆瓣热门剧集' },
]

const TMDB_TYPE_PILLS: Array<{ value: TmdbSubTypeFilter; label: string }> = [
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

type AccentTheme = 'orange' | 'emerald' | 'sky'

const ACCENT_STYLES: Record<
  AccentTheme,
  {
    sectionBorder: string
    sectionBg: string
    iconWrap: string
    badge: string
    countText: string
    focusBtn: string
    spinner: string
  }
> = {
  orange: {
    sectionBorder: 'border-orange-500/25',
    sectionBg: 'bg-gradient-to-br from-orange-950/30 via-amber-950/15 to-zinc-950/90',
    iconWrap: 'bg-orange-500/15 text-orange-400 ring-1 ring-orange-400/30',
    badge: 'border-orange-400/35 bg-orange-500/15 text-orange-200',
    countText: 'text-amber-300',
    focusBtn: 'border-orange-400/30 bg-orange-500/10 text-orange-200 hover:bg-orange-500/20',
    spinner: 'text-amber-400',
  },
  emerald: {
    sectionBorder: 'border-emerald-500/25',
    sectionBg: 'bg-gradient-to-br from-emerald-950/30 via-teal-950/15 to-zinc-950/90',
    iconWrap: 'bg-emerald-500/15 text-emerald-400 ring-1 ring-emerald-400/30',
    badge: 'border-emerald-400/35 bg-emerald-500/15 text-emerald-200',
    countText: 'text-emerald-300',
    focusBtn: 'border-emerald-400/30 bg-emerald-500/10 text-emerald-200 hover:bg-emerald-500/20',
    spinner: 'text-emerald-400',
  },
  sky: {
    sectionBorder: 'border-sky-500/25',
    sectionBg: 'bg-gradient-to-br from-sky-950/30 via-cyan-950/15 to-zinc-950/90',
    iconWrap: 'bg-sky-500/15 text-sky-400 ring-1 ring-sky-400/30',
    badge: 'border-sky-400/35 bg-sky-500/15 text-sky-200',
    countText: 'text-sky-300',
    focusBtn: 'border-sky-400/30 bg-sky-500/10 text-sky-200 hover:bg-sky-500/20',
    spinner: 'text-sky-400',
  },
}

interface TrendingSectionProps {
  title: string
  subtitle: string
  accent: AccentTheme
  icon: React.ReactNode
  chart?: RankingChartStatus
  defaultSourceLabel: string
  items: BaseItemDto[]
  total: number
  isLoading: boolean
  isError?: boolean
  error?: unknown
  onRetry?: () => void
  isAdmin: boolean
  fallbackNotice?: boolean
  onViewAll?: () => void
  headerExtra?: React.ReactNode
}

function TrendingSection({
  title,
  subtitle,
  accent,
  icon,
  chart,
  defaultSourceLabel,
  items,
  total,
  isLoading,
  isError,
  error,
  onRetry,
  isAdmin,
  fallbackNotice,
  onViewAll,
  headerExtra,
}: TrendingSectionProps) {
  const { t } = useI18n()
  const styles = ACCENT_STYLES[accent]

  return (
    <section
      aria-label={title}
      className={`rounded-3xl border ${styles.sectionBorder} ${styles.sectionBg} p-5 shadow-xl shadow-black/25 sm:p-6`}
    >
      <div className="mb-5 flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="space-y-1.5">
          <div className="flex flex-wrap items-center gap-2.5">
            <span
              className={`inline-flex h-8 w-8 items-center justify-center rounded-xl ${styles.iconWrap}`}
            >
              {icon}
            </span>
            <h2 className="text-lg font-bold tracking-tight text-white sm:text-xl">{title}</h2>
            <span
              className={`inline-flex items-center rounded-full border px-2.5 py-0.5 text-[11px] font-semibold ${styles.badge}`}
            >
              {chart?.source || defaultSourceLabel}
            </span>
            {fallbackNotice && (
              <span className="rounded-full border border-white/10 bg-white/[0.06] px-2 py-0.5 text-[11px] text-zinc-300">
                兼容旧版 Trending 标签
              </span>
            )}
          </div>
          <p className="text-xs text-zinc-400">{subtitle}</p>
          <div className="flex flex-wrap items-center gap-2.5 pt-0.5 text-xs text-zinc-400">
            <span className="inline-flex items-center gap-1 rounded-lg bg-white/[0.06] px-2.5 py-1 text-zinc-200">
              {t('trending.inLibrary')} <strong className={styles.countText}>{total}</strong> 部
            </span>
            {chart && (
              <>
                <span className="rounded-lg bg-white/[0.04] px-2.5 py-1">
                  榜单缓存：<span className="text-zinc-200">{chart.count}</span> 条
                </span>
                <span
                  className="rounded-lg bg-white/[0.04] px-2.5 py-1"
                  title={formatDateTime(chart.updated_at)}
                >
                  最近更新：
                  <span className="text-zinc-200">{formatRelativeTime(chart.updated_at)}</span>
                </span>
              </>
            )}
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-2.5">
          {headerExtra}
          {onViewAll && (
            <button
              type="button"
              onClick={onViewAll}
              className={`inline-flex shrink-0 items-center gap-1.5 rounded-xl border px-3.5 py-2 text-xs font-semibold transition-colors ${styles.focusBtn}`}
            >
              <span>查看该栏全部</span>
              <ArrowRight className="h-3.5 w-3.5" />
            </button>
          )}
        </div>
      </div>

      {isLoading ? (
        <div className="flex h-44 items-center justify-center">
          <Loader2 className={`h-6 w-6 animate-spin ${styles.spinner}`} />
        </div>
      ) : isError ? (
        <QueryErrorState
          compact
          title={`无法加载${title}`}
          error={error}
          onRetry={onRetry}
        />
      ) : items.length === 0 ? (
        <div className="rounded-2xl border border-dashed border-white/12 bg-black/20 px-6 py-10 text-center">
          <p className="text-sm font-medium text-zinc-300">该榜单暂未匹配到库内作品</p>
          <p className="mx-auto mt-1.5 max-w-md text-xs text-zinc-400">
            {isAdmin
              ? '请点击页面右上角「立即同步热门榜」，系统将自动拉取最新榜单并为库内匹配的影片/剧集打标。'
              : '请联系管理员同步最新热门榜单缓存。'}
          </p>
        </div>
      ) : (
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7">
          {items.map((item) => (
            <MediaCard key={item.Id} item={item} />
          ))}
        </div>
      )}
    </section>
  )
}

export function TrendingPage() {
  const { auth, isAdmin } = useAuth()
  const { locale, t } = useI18n()
  const queryClient = useQueryClient()
  const userId = auth!.userId

  const [viewMode, setViewMode] = useState<TrendingViewMode>('overview')
  const [tmdbTypeFilter, setTmdbTypeFilter] = useState<TmdbSubTypeFilter>('Movie,Series')
  const [sortBy, setSortBy] = useState('CommunityRating')
  const [sortOrder, setSortOrder] = useState<'Ascending' | 'Descending'>('Descending')
  const [feedback, setFeedback] = useState<{ type: 'success' | 'error'; message: string } | null>(
    null,
  )

  const { data: rankings } = useQuery({
    queryKey: ['rankings'],
    queryFn: () => api.getRankingsSummary(),
  })

  const isOverview = viewMode === 'overview'
  const tmdbLimit = isOverview ? 24 : 60
  const doubanLimit = isOverview ? 24 : 60
  const activeTmdbTypes: TmdbSubTypeFilter = isOverview ? 'Movie,Series' : tmdbTypeFilter

  // 1. TMDB 热门
  const tmdbQuery = useQuery({
    queryKey: ['trendingItems', 'tmdb', userId, activeTmdbTypes, sortBy, sortOrder, tmdbLimit],
    queryFn: () =>
      api.getItems(userId, {
        tags: 'TMDB热门',
        includeItemTypes: activeTmdbTypes,
        recursive: true,
        sortBy,
        sortOrder,
        limit: tmdbLimit,
      }),
    enabled: isOverview || viewMode === 'tmdb',
    placeholderData: (prev) => prev,
  })

  // 当 TMDB热门 暂无数据时，优雅回退至旧版 Trending 标签（仅在非错误态时触发回退）
  const needTmdbFallback =
    (isOverview || viewMode === 'tmdb') &&
    !tmdbQuery.isLoading &&
    !tmdbQuery.isError &&
    (tmdbQuery.data?.Items?.length ?? 0) === 0

  const tmdbFallbackQuery = useQuery({
    queryKey: [
      'trendingItems',
      'tmdb-fallback',
      userId,
      activeTmdbTypes,
      sortBy,
      sortOrder,
      tmdbLimit,
    ],
    queryFn: () =>
      api.getItems(userId, {
        tags: 'Trending',
        includeItemTypes: activeTmdbTypes,
        recursive: true,
        sortBy,
        sortOrder,
        limit: tmdbLimit,
      }),
    enabled: needTmdbFallback,
    placeholderData: (prev) => prev,
  })

  // 2. 豆瓣热门电影
  const doubanMovieQuery = useQuery({
    queryKey: ['trendingItems', 'douban_movie', userId, sortBy, sortOrder, doubanLimit],
    queryFn: () =>
      api.getItems(userId, {
        tags: '豆瓣热门电影',
        includeItemTypes: 'Movie',
        recursive: true,
        sortBy,
        sortOrder,
        limit: doubanLimit,
      }),
    enabled: isOverview || viewMode === 'douban_movie',
    placeholderData: (prev) => prev,
  })

  // 3. 豆瓣热门剧集
  const doubanTvQuery = useQuery({
    queryKey: ['trendingItems', 'douban_tv', userId, sortBy, sortOrder, doubanLimit],
    queryFn: () =>
      api.getItems(userId, {
        tags: '豆瓣热门剧集',
        includeItemTypes: 'Series',
        recursive: true,
        sortBy,
        sortOrder,
        limit: doubanLimit,
      }),
    enabled: isOverview || viewMode === 'douban_tv',
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
      const parts: string[] = []
      if (res.tmdb_trending_count) parts.push(`TMDB 热门 ${res.tmdb_trending_count} 条`)
      if (res.douban_trending_movie_count)
        parts.push(`豆瓣热门电影 ${res.douban_trending_movie_count} 条`)
      if (res.douban_trending_tv_count) parts.push(`豆瓣热门剧集 ${res.douban_trending_tv_count} 条`)
      const summarySuffix = parts.length > 0 ? `（${parts.join(' / ')}）` : ''
      setFeedback({
        type: 'success',
        message: `热门榜同步完成${summarySuffix}：库内命中 ${res.matched_items} 部媒体，新增打标 ${res.tags_added} 次`,
      })
    },
    onError: (err) => {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '同步热门榜失败',
      })
    },
  })

  const usingTmdbFallback =
    needTmdbFallback && (tmdbFallbackQuery.data?.Items?.length ?? 0) > 0
  const tmdbItems = usingTmdbFallback
    ? tmdbFallbackQuery.data?.Items || []
    : tmdbQuery.data?.Items || []
  const tmdbTotal = usingTmdbFallback
    ? (tmdbFallbackQuery.data?.TotalRecordCount ?? tmdbItems.length)
    : (tmdbQuery.data?.TotalRecordCount ?? tmdbItems.length)
  const tmdbLoading = tmdbQuery.isLoading || (needTmdbFallback && tmdbFallbackQuery.isLoading)
  const tmdbIsError = tmdbQuery.isError || (needTmdbFallback && tmdbFallbackQuery.isError)
  const tmdbError = tmdbQuery.error || tmdbFallbackQuery.error
  const retryTmdb = () => {
    void tmdbQuery.refetch()
    if (needTmdbFallback) void tmdbFallbackQuery.refetch()
  }

  const doubanMovieItems = doubanMovieQuery.data?.Items || []
  const doubanMovieTotal = doubanMovieQuery.data?.TotalRecordCount ?? doubanMovieItems.length

  const doubanTvItems = doubanTvQuery.data?.Items || []
  const doubanTvTotal = doubanTvQuery.data?.TotalRecordCount ?? doubanTvItems.length

  const isAnyFetching =
    tmdbQuery.isFetching ||
    tmdbFallbackQuery.isFetching ||
    doubanMovieQuery.isFetching ||
    doubanTvQuery.isFetching

  const tmdbSubFilterControl = (
    <div className="inline-flex items-center gap-1 rounded-xl border border-white/10 bg-zinc-950/80 p-1">
      {TMDB_TYPE_PILLS.map((pill) => {
        const active = tmdbTypeFilter === pill.value
        return (
          <button
            key={pill.value}
            type="button"
            onClick={() => setTmdbTypeFilter(pill.value)}
            className={`rounded-lg px-3 py-1 text-xs font-medium transition-all ${
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
  )

  return (
    <div className="space-y-6 px-4 pt-6 pb-10 lg:px-8">
      {/* 顶部三榜融合渐变头部 */}
      <section className="relative overflow-hidden rounded-3xl border border-orange-500/25 bg-gradient-to-br from-orange-950/50 via-amber-950/30 to-zinc-950/95 p-6 shadow-2xl shadow-black/40 sm:p-8">
        <div className="pointer-events-none absolute -right-12 -top-12 h-56 w-56 rounded-full bg-gradient-to-br from-amber-500/25 to-orange-600/10 blur-3xl" />

        <div className="relative z-10 flex flex-col gap-5 md:flex-row md:items-center md:justify-between">
          <div className="space-y-2">
            <div className="inline-flex items-center gap-1.5 rounded-full border border-orange-400/40 bg-orange-500/15 px-3 py-1 text-xs font-semibold text-orange-200">
              <Flame className="h-3.5 w-3.5 text-orange-400" />
              <span>TMDB 全球热门 · 豆瓣热门电影 · 豆瓣热门剧集</span>
            </div>
            <h1 className="text-2xl font-bold tracking-tight text-white sm:text-3xl">
              {locale === 'zh-CN' ? '热门趋势' : t('trending.title')}
            </h1>
            <p className="max-w-2xl text-xs leading-relaxed text-zinc-300 sm:text-sm">
              {t('trending.subtitle')}
            </p>

            <div className="flex flex-wrap items-center gap-2.5 pt-1 text-xs text-zinc-400">
              <span className="inline-flex items-center gap-1.5 rounded-lg bg-white/[0.06] px-2.5 py-1 text-zinc-200">
                <Sparkles className="h-3.5 w-3.5 text-amber-400" />
                TMDB 热门 <strong className="text-amber-300">{tmdbTotal}</strong> 部
              </span>
              <span className="inline-flex items-center gap-1.5 rounded-lg bg-white/[0.06] px-2.5 py-1 text-zinc-200">
                <Film className="h-3.5 w-3.5 text-emerald-400" />
                豆瓣热门电影 <strong className="text-emerald-300">{doubanMovieTotal}</strong> 部
              </span>
              <span className="inline-flex items-center gap-1.5 rounded-lg bg-white/[0.06] px-2.5 py-1 text-zinc-200">
                <Tv className="h-3.5 w-3.5 text-sky-400" />
                豆瓣热门剧集 <strong className="text-sky-300">{doubanTvTotal}</strong> 部
              </span>
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
              <span>
                {refreshMutation.isPending
                  ? '正在同步热门榜...'
                  : locale === 'zh-CN'
                    ? '立即同步热门榜'
                    : t('trending.refreshNow')}
              </span>
            </button>
          )}
        </div>
      </section>

      <FeedbackBanner
        message={feedback?.message}
        type={feedback?.type}
        onClose={() => setFeedback(null)}
      />

      {/* 顶部视图切换与排序工具栏 */}
      <div className="flex flex-col gap-3 border-b border-white/8 pb-4 lg:flex-row lg:items-center lg:justify-between">
        <div className="inline-flex flex-wrap items-center gap-1.5 rounded-xl border border-white/10 bg-zinc-950/80 p-1">
          {VIEW_TABS.map((tab) => {
            const active = viewMode === tab.value
            const activeClass =
              tab.value === 'douban_movie'
                ? 'bg-emerald-500 text-zinc-950 shadow-sm'
                : tab.value === 'douban_tv'
                  ? 'bg-sky-500 text-zinc-950 shadow-sm'
                  : 'bg-amber-500 text-zinc-950 shadow-sm'
            return (
              <button
                key={tab.value}
                type="button"
                onClick={() => setViewMode(tab.value)}
                className={`inline-flex items-center gap-1.5 rounded-lg px-3.5 py-1.5 text-xs font-medium transition-all ${
                  active
                    ? activeClass
                    : 'text-zinc-400 hover:bg-white/[0.05] hover:text-zinc-100'
                }`}
              >
                {tab.value === 'overview' && <LayoutGrid className="h-3.5 w-3.5" />}
                {tab.value === 'tmdb' && <Flame className="h-3.5 w-3.5" />}
                {tab.value === 'douban_movie' && <Film className="h-3.5 w-3.5" />}
                {tab.value === 'douban_tv' && <Tv className="h-3.5 w-3.5" />}
                <span>{tab.label}</span>
              </button>
            )
          })}
        </div>

        <div className="flex flex-wrap items-center gap-2.5">
          {isAnyFetching && <span className="text-xs text-zinc-500">正在更新...</span>}
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

      {/* 三分栏总览 / 单栏聚焦视图 */}
      {isOverview ? (
        <div className="space-y-6">
          <TrendingSection
            title="TMDB 热门"
            subtitle="同步 TMDB 全球每周热门电影与剧集趋势，覆盖海内外热映与热播作品"
            accent="orange"
            icon={<Flame className="h-4 w-4" />}
            chart={rankings?.tmdb_trending}
            defaultSourceLabel="TMDB 全球热门榜"
            items={tmdbItems}
            total={tmdbTotal}
            isLoading={tmdbLoading}
            isError={tmdbIsError}
            error={tmdbError}
            onRetry={retryTmdb}
            isAdmin={isAdmin}
            fallbackNotice={usingTmdbFallback}
            onViewAll={() => setViewMode('tmdb')}
          />

          <TrendingSection
            title="豆瓣热门电影"
            subtitle="同步豆瓣实时热门电影榜单，聚焦中文影迷热议的新片与口碑佳作"
            accent="emerald"
            icon={<Film className="h-4 w-4" />}
            chart={rankings?.douban_trending_movie}
            defaultSourceLabel="豆瓣热门电影"
            items={doubanMovieItems}
            total={doubanMovieTotal}
            isLoading={doubanMovieQuery.isLoading}
            isError={doubanMovieQuery.isError}
            error={doubanMovieQuery.error}
            onRetry={() => void doubanMovieQuery.refetch()}
            isAdmin={isAdmin}
            onViewAll={() => setViewMode('douban_movie')}
          />

          <TrendingSection
            title="豆瓣热门剧集"
            subtitle="同步豆瓣实时热门电视剧与高分剧集，覆盖华语、日韩与欧美热播好剧"
            accent="sky"
            icon={<Tv className="h-4 w-4" />}
            chart={rankings?.douban_trending_tv}
            defaultSourceLabel="豆瓣热门剧集"
            items={doubanTvItems}
            total={doubanTvTotal}
            isLoading={doubanTvQuery.isLoading}
            isError={doubanTvQuery.isError}
            error={doubanTvQuery.error}
            onRetry={() => void doubanTvQuery.refetch()}
            isAdmin={isAdmin}
            onViewAll={() => setViewMode('douban_tv')}
          />
        </div>
      ) : viewMode === 'tmdb' ? (
        <TrendingSection
          title="TMDB 热门"
          subtitle="TMDB 全球热门电影与剧集完整命中列表（最多展示 60 部）"
          accent="orange"
          icon={<Flame className="h-4 w-4" />}
          chart={rankings?.tmdb_trending}
          defaultSourceLabel="TMDB 全球热门榜"
          items={tmdbItems}
          total={tmdbTotal}
          isLoading={tmdbLoading}
          isError={tmdbIsError}
          error={tmdbError}
          onRetry={retryTmdb}
          isAdmin={isAdmin}
          fallbackNotice={usingTmdbFallback}
          headerExtra={tmdbSubFilterControl}
        />
      ) : viewMode === 'douban_movie' ? (
        <TrendingSection
          title="豆瓣热门电影"
          subtitle="豆瓣热门电影完整命中列表（最多展示 60 部）"
          accent="emerald"
          icon={<Film className="h-4 w-4" />}
          chart={rankings?.douban_trending_movie}
          defaultSourceLabel="豆瓣热门电影"
          items={doubanMovieItems}
          total={doubanMovieTotal}
          isLoading={doubanMovieQuery.isLoading}
          isError={doubanMovieQuery.isError}
          error={doubanMovieQuery.error}
          onRetry={() => void doubanMovieQuery.refetch()}
          isAdmin={isAdmin}
        />
      ) : (
        <TrendingSection
          title="豆瓣热门剧集"
          subtitle="豆瓣热门剧集完整命中列表（最多展示 60 部）"
          accent="sky"
          icon={<Tv className="h-4 w-4" />}
          chart={rankings?.douban_trending_tv}
          defaultSourceLabel="豆瓣热门剧集"
          items={doubanTvItems}
          total={doubanTvTotal}
          isLoading={doubanTvQuery.isLoading}
          isError={doubanTvQuery.isError}
          error={doubanTvQuery.error}
          onRetry={() => void doubanTvQuery.refetch()}
          isAdmin={isAdmin}
        />
      )}
    </div>
  )
}
