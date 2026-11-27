import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  AlertTriangle,
  Award,
  ChevronLeft,
  ChevronRight,
  ChevronsLeft,
  ChevronsRight,
  FileCode2,
  Film,
  Flame,
  Image as ImageIcon,
  Layers,
  Loader2,
  Pencil,
  RefreshCw,
  ScanSearch,
  Search,
  Sparkles,
  Trash2,
  Tv,
} from 'lucide-react'
import { Link } from 'react-router-dom'
import { api, embyFetch, getFallbackImageUrl, getImageUrl } from '../../api/client'
import type { BaseItemDto } from '../../api/types'
import { IdentifyModal } from '../../components/media/IdentifyModal'
import { ImageManagerModal } from '../../components/media/ImageManagerModal'
import { MetadataEditorModal } from '../../components/media/MetadataEditorModal'
import { QueryErrorState } from '../../components/QueryErrorState'
import { useAuth } from '../../context/AuthContext'
import { useI18n } from '../../i18n/I18nContext'
import {
  AdminModal,
  adminLabel,
  FeedbackBanner,
  formatDateTime,
  formatRelativeTime,
} from './adminUtils'

type TypeFilter = 'ALL' | 'Movie' | 'Series'

const PAGE_SIZE_OPTIONS = [20, 50, 100] as const

// Server-side tag keys persisted on items during chart auto-tagging
const TAG_TRENDING = 'Trending'
const TAG_TMDB_HOT = 'TMDB\u70ed\u95e8'
const TAG_DOUBAN_HOT_MOVIE = '\u8c46\u74e3\u70ed\u95e8\u7535\u5f71'
const TAG_DOUBAN_HOT_TV = '\u8c46\u74e3\u70ed\u95e8\u5267\u96c6'
const TAG_DOUBAN_TOP250 = '\u8c46\u74e3Top250'
const TAG_IMDB_TOP250 = 'IMDb Top250'

function buildPageNumbers(current: number, totalPages: number): Array<number | 'ellipsis'> {
  if (totalPages <= 7) {
    return Array.from({ length: totalPages }, (_, i) => i)
  }
  const pages = new Set<number>([0, totalPages - 1, current, current - 1, current + 1])
  const sorted = Array.from(pages)
    .filter((p) => p >= 0 && p < totalPages)
    .sort((a, b) => a - b)

  const out: Array<number | 'ellipsis'> = []
  for (let i = 0; i < sorted.length; i++) {
    if (i > 0 && sorted[i] - sorted[i - 1] > 1) {
      out.push('ellipsis')
    }
    out.push(sorted[i])
  }
  return out
}

export function MetadataTab() {
  const { t, locale } = useI18n()
  const { auth } = useAuth()
  const queryClient = useQueryClient()
  const userId = auth!.userId

  const [searchTerm, setSearchTerm] = useState('')
  const [typeFilter, setTypeFilter] = useState<TypeFilter>('ALL')
  const [tagFilter, setTagFilter] = useState('')
  const [page, setPage] = useState(0)
  const [pageSize, setPageSize] = useState<number>(20)
  const [feedback, setFeedback] = useState<{ type: 'success' | 'error'; message: string } | null>(
    null,
  )

  const [editingItem, setEditingItem] = useState<BaseItemDto | null>(null)
  const [imageItem, setImageItem] = useState<BaseItemDto | null>(null)
  const [identifyItem, setIdentifyItem] = useState<BaseItemDto | null>(null)
  const [deletingItem, setDeletingItem] = useState<BaseItemDto | null>(null)
  const [refreshingId, setRefreshingId] = useState<string | null>(null)
  const [writingNfoId, setWritingNfoId] = useState<string | null>(null)

  const formatTagDisplay = (tag: string): string => {
    switch (tag) {
      case TAG_TMDB_HOT:
        return t('admin.metadata.tmdbTrending')
      case TAG_TRENDING:
        return t('nav.trending')
      case TAG_DOUBAN_HOT_MOVIE:
        return t('admin.metadata.doubanHotMovie')
      case TAG_DOUBAN_HOT_TV:
        return t('admin.metadata.doubanHotTv')
      case TAG_DOUBAN_TOP250:
        return t('admin.metadata.doubanTop250')
      case TAG_IMDB_TOP250:
        return t('admin.metadata.imdbTop250')
      default:
        return tag
    }
  }

  const { data: rankings, isLoading: rankingsLoading } = useQuery({
    queryKey: ['rankings'],
    queryFn: () => api.getRankings(),
  })

  const { data: tagsData } = useQuery({
    queryKey: ['tags', userId],
    queryFn: () => api.getTags({ userId }),
  })

  const {
    data: itemsData,
    isLoading: itemsLoading,
    isFetching: itemsFetching,
    isError: itemsIsError,
    error: itemsError,
    refetch: refetchItems,
  } = useQuery({
    queryKey: [
      'admin-metadata-items',
      userId,
      typeFilter,
      tagFilter,
      searchTerm.trim(),
      page,
      pageSize,
    ],
    queryFn: () =>
      api.getItems(userId, {
        includeItemTypes: typeFilter === 'ALL' ? 'Movie,Series' : typeFilter,
        tags: tagFilter || undefined,
        searchTerm: searchTerm.trim() || undefined,
        sortBy: 'DateCreated',
        sortOrder: 'Descending',
        startIndex: page * pageSize,
        limit: pageSize,
      }),
    placeholderData: (prev) => prev,
  })

  const items = itemsData?.Items || []
  const totalCount = itemsData?.TotalRecordCount ?? items.length
  const totalPages = Math.max(1, Math.ceil(totalCount / pageSize))
  const currentPage = Math.min(page, totalPages - 1)

  useEffect(() => {
    if (itemsData && page >= totalPages) {
      setPage(Math.max(0, totalPages - 1))
    }
  }, [itemsData, page, totalPages])

  const rangeStart = totalCount === 0 ? 0 : currentPage * pageSize + 1
  const rangeEnd = totalCount === 0 ? 0 : Math.min(totalCount, currentPage * pageSize + items.length)
  const pageButtons = useMemo(
    () => buildPageNumbers(currentPage, totalPages),
    [currentPage, totalPages],
  )

  const availableTags = useMemo(() => {
    const set = new Set<string>([
      TAG_TRENDING,
      TAG_TMDB_HOT,
      TAG_DOUBAN_HOT_MOVIE,
      TAG_DOUBAN_HOT_TV,
      TAG_DOUBAN_TOP250,
      TAG_IMDB_TOP250,
    ])
    for (const tagEntry of tagsData?.Items || []) {
      if (tagEntry.Name) set.add(tagEntry.Name)
    }
    return Array.from(set)
  }, [tagsData])

  const refreshRankingsMutation = useMutation({
    mutationFn: () => api.refreshRankings(),
    onSuccess: async (res) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['rankings'] }),
        queryClient.invalidateQueries({ queryKey: ['trendingItems'] }),
        queryClient.invalidateQueries({ queryKey: ['admin-metadata-items'] }),
        queryClient.invalidateQueries({ queryKey: ['items'] }),
        queryClient.invalidateQueries({ queryKey: ['tags'] }),
      ])
      const parts = [
        `${t('admin.metadata.doubanTop250')} (${t('admin.metadata.itemsCountOpt', { size: res.douban_count })})`,
        `${t('admin.metadata.imdbTop250')} (${t('admin.metadata.itemsCountOpt', { size: res.imdb_count })})`,
      ]
      if (res.tmdb_trending_count !== undefined) {
        parts.push(
          `${t('admin.metadata.tmdbTrending')} (${t('admin.metadata.itemsCountOpt', { size: res.tmdb_trending_count })})`,
        )
      }
      if (res.douban_trending_movie_count !== undefined) {
        parts.push(
          `${t('admin.metadata.doubanHotMovie')} (${t('admin.metadata.itemsCountOpt', { size: res.douban_trending_movie_count })})`,
        )
      }
      if (res.douban_trending_tv_count !== undefined) {
        parts.push(
          `${t('admin.metadata.doubanHotTv')} (${t('admin.metadata.itemsCountOpt', { size: res.douban_trending_tv_count })})`,
        )
      }
      const partialErrors: string[] = []
      if (res.douban_error)
        partialErrors.push(`${t('admin.metadata.doubanTop250')}: ${res.douban_error}`)
      if (res.imdb_error)
        partialErrors.push(`${t('admin.metadata.imdbTop250')}: ${res.imdb_error}`)
      if (res.tmdb_trending_error)
        partialErrors.push(`${t('admin.metadata.tmdbTrending')}: ${res.tmdb_trending_error}`)
      if (res.douban_trending_movie_error)
        partialErrors.push(
          `${t('admin.metadata.doubanHotMovie')}: ${res.douban_trending_movie_error}`,
        )
      if (res.douban_trending_tv_error)
        partialErrors.push(`${t('admin.metadata.doubanHotTv')}: ${res.douban_trending_tv_error}`)
      if (partialErrors.length > 0) {
        setFeedback({
          type: 'error',
          message: t('admin.metadata.syncPartialError', {
            errors: partialErrors.join('; '),
            parts: parts.join(' / '),
            matched: res.matched_items,
          }),
        })
      } else {
        setFeedback({
          type: 'success',
          message: t('admin.metadata.syncSuccess', {
            parts: parts.join(' / '),
            matched: res.matched_items,
            tags: res.tags_added,
          }),
        })
      }
    },
    onError: (err) => {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('admin.metadata.syncFailed'),
      })
    },
  })

  const handleRefreshItem = async (item: BaseItemDto) => {
    setRefreshingId(item.Id)
    try {
      await api.refreshItemMetadata(item.Id, false)
      await queryClient.invalidateQueries({ queryKey: ['admin-metadata-items'] })
      setFeedback({
        type: 'success',
        message: t('admin.metadata.refreshedSuccess', { name: item.Name }),
      })
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('admin.metadata.refreshFailed'),
      })
    } finally {
      setRefreshingId(null)
    }
  }

  const handleWriteItemNfo = async (item: BaseItemDto) => {
    setWritingNfoId(item.Id)
    try {
      const res = await api.writeItemNfo(item.Id)
      const nfoPath = res?.NfoPath || res?.path
      setFeedback({
        type: 'success',
        message: nfoPath
          ? t('admin.metadata.nfoWrittenPath', { name: item.Name, path: nfoPath })
          : t('admin.metadata.nfoWrittenSuccess', { name: item.Name }),
      })
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('admin.metadata.nfoWriteFailed'),
      })
    } finally {
      setWritingNfoId(null)
    }
  }

  const {
    data: deleteInfo,
    isLoading: deleteInfoLoading,
    isError: deleteInfoError,
  } = useQuery({
    queryKey: ['admin-item-delete-info', deletingItem?.Id],
    queryFn: () =>
      embyFetch<{ Paths?: string[] }>(
        `/emby/Items/${encodeURIComponent(deletingItem!.Id)}/DeleteInfo`,
      ),
    enabled: Boolean(deletingItem?.Id),
  })

  const deleteInfoPaths = useMemo(() => {
    if (!Array.isArray(deleteInfo?.Paths)) return []
    return deleteInfo.Paths.map((p) => (typeof p === 'string' ? p.trim() : '')).filter(Boolean)
  }, [deleteInfo])

  const deleteMutation = useMutation({
    mutationFn: (itemId: string) => api.deleteItem(itemId),
    onSuccess: async () => {
      const removedName = deletingItem?.Name || '-'
      setDeletingItem(null)
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['admin-metadata-items'] }),
        queryClient.invalidateQueries({ queryKey: ['items'] }),
        queryClient.invalidateQueries({ queryKey: ['latestItems'] }),
      ])
      setFeedback({
        type: 'success',
        message: t('admin.metadata.deletedSuccess', { name: removedName }),
      })
    },
    onError: (err) => {
      setDeletingItem(null)
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('admin.metadata.deleteFailed'),
      })
    },
  })

  return (
    <div className="space-y-6">
      <FeedbackBanner
        message={feedback?.message}
        type={feedback?.type}
        onClose={() => setFeedback(null)}
      />

      {/* Top 250 & Trending Cache Cards */}
      <section className="space-y-4">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h2 className="flex items-center gap-2 text-base font-semibold text-zinc-100">
              <Award className="h-4 w-4 text-amber-400" />
              {t('admin.metadata.rankingsTitle')}
            </h2>
            <p className="mt-0.5 text-xs text-zinc-400">{t('admin.metadata.rankingsSubtitle')}</p>
          </div>
          <button
            type="button"
            disabled={refreshRankingsMutation.isPending}
            onClick={() => refreshRankingsMutation.mutate()}
            className="inline-flex items-center justify-center gap-2 rounded-xl bg-amber-500 px-4 py-2 text-xs font-semibold text-zinc-950 shadow-lg shadow-amber-500/20 transition-all hover:bg-amber-400 disabled:opacity-60"
          >
            <RefreshCw
              className={`h-3.5 w-3.5 ${refreshRankingsMutation.isPending ? 'animate-spin' : ''}`}
            />
            <span>
              {refreshRankingsMutation.isPending
                ? t('admin.metadata.syncingBtn')
                : t('admin.metadata.syncAndTagBtn')}
            </span>
          </button>
        </div>

        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-5">
          <div className="rounded-2xl border border-emerald-500/25 bg-gradient-to-br from-emerald-950/35 via-zinc-950/80 to-zinc-950/90 p-5">
            <div className="flex items-start justify-between gap-3">
              <div>
                <span className="inline-flex items-center rounded-md border border-emerald-400/40 bg-emerald-500/15 px-2.5 py-0.5 text-[11px] font-semibold text-emerald-200">
                  {t('admin.metadata.doubanTop250')}
                </span>
                <p className="mt-2 text-2xl font-bold text-white">
                  {rankingsLoading ? '...' : rankings?.douban_top250.count ?? 0}
                  <span className="ml-1.5 text-xs font-normal text-zinc-400">
                    {t('admin.metadata.cachedItemsUnit')}
                  </span>
                </p>
              </div>
              <Sparkles className="h-5 w-5 text-emerald-400/80" />
            </div>
            <div className="mt-3 space-y-1 text-xs text-zinc-400">
              <p className="truncate">
                {t('admin.metadata.sourceLabel')}
                <span className="text-zinc-300">{rankings?.douban_top250.source || '-'}</span>
              </p>
              <p>
                {t('admin.metadata.updatedAtLabel')}
                <span
                  className="text-zinc-300"
                  title={formatDateTime(rankings?.douban_top250.updated_at)}
                >
                  {formatRelativeTime(rankings?.douban_top250.updated_at)}
                </span>
              </p>
            </div>
          </div>

          <div className="rounded-2xl border border-amber-500/25 bg-gradient-to-br from-amber-950/35 via-zinc-950/80 to-zinc-950/90 p-5">
            <div className="flex items-start justify-between gap-3">
              <div>
                <span className="inline-flex items-center rounded-md border border-amber-400/40 bg-amber-500/15 px-2.5 py-0.5 text-[11px] font-semibold text-amber-200">
                  {t('admin.metadata.imdbTop250')}
                </span>
                <p className="mt-2 text-2xl font-bold text-white">
                  {rankingsLoading ? '...' : rankings?.imdb_top250.count ?? 0}
                  <span className="ml-1.5 text-xs font-normal text-zinc-400">
                    {t('admin.metadata.cachedItemsUnit')}
                  </span>
                </p>
              </div>
              <Sparkles className="h-5 w-5 text-amber-400/80" />
            </div>
            <div className="mt-3 space-y-1 text-xs text-zinc-400">
              <p className="truncate">
                {t('admin.metadata.sourceLabel')}
                <span className="text-zinc-300">{rankings?.imdb_top250.source || '-'}</span>
              </p>
              <p>
                {t('admin.metadata.updatedAtLabel')}
                <span
                  className="text-zinc-300"
                  title={formatDateTime(rankings?.imdb_top250.updated_at)}
                >
                  {formatRelativeTime(rankings?.imdb_top250.updated_at)}
                </span>
              </p>
            </div>
          </div>

          <div className="rounded-2xl border border-orange-500/25 bg-gradient-to-br from-orange-950/35 via-zinc-950/80 to-zinc-950/90 p-5">
            <div className="flex items-start justify-between gap-3">
              <div>
                <span className="inline-flex items-center gap-1 rounded-md border border-orange-400/45 bg-orange-500/15 px-2.5 py-0.5 text-[11px] font-semibold text-orange-200">
                  <Flame className="h-3 w-3 text-orange-400" />
                  <span>{t('admin.metadata.tmdbTrending')}</span>
                </span>
                <p className="mt-2 text-2xl font-bold text-white">
                  {rankingsLoading ? '...' : rankings?.tmdb_trending?.count ?? 0}
                  <span className="ml-1.5 text-xs font-normal text-zinc-400">
                    {t('admin.metadata.cachedItemsUnit')}
                  </span>
                </p>
              </div>
              <Flame className="h-5 w-5 text-orange-400/80" />
            </div>
            <div className="mt-3 space-y-1 text-xs text-zinc-400">
              <p className="truncate">
                {t('admin.metadata.sourceLabel')}
                <span className="text-zinc-300">
                  {rankings?.tmdb_trending?.source || t('admin.metadata.tmdbTrending')}
                </span>
              </p>
              <p>
                {t('admin.metadata.updatedAtLabel')}
                <span
                  className="text-zinc-300"
                  title={formatDateTime(rankings?.tmdb_trending?.updated_at)}
                >
                  {formatRelativeTime(rankings?.tmdb_trending?.updated_at)}
                </span>
              </p>
            </div>
          </div>

          <div className="rounded-2xl border border-teal-500/25 bg-gradient-to-br from-teal-950/35 via-zinc-950/80 to-zinc-950/90 p-5">
            <div className="flex items-start justify-between gap-3">
              <div>
                <span className="inline-flex items-center gap-1 rounded-md border border-teal-400/45 bg-teal-500/15 px-2.5 py-0.5 text-[11px] font-semibold text-teal-200">
                  <Film className="h-3 w-3 text-teal-400" />
                  <span>{t('admin.metadata.doubanHotMovie')}</span>
                </span>
                <p className="mt-2 text-2xl font-bold text-white">
                  {rankingsLoading ? '...' : rankings?.douban_trending_movie?.count ?? 0}
                  <span className="ml-1.5 text-xs font-normal text-zinc-400">
                    {t('admin.metadata.cachedItemsUnit')}
                  </span>
                </p>
              </div>
              <Film className="h-5 w-5 text-teal-400/80" />
            </div>
            <div className="mt-3 space-y-1 text-xs text-zinc-400">
              <p className="truncate">
                {t('admin.metadata.sourceLabel')}
                <span className="text-zinc-300">
                  {rankings?.douban_trending_movie?.source || t('admin.metadata.doubanHotMovie')}
                </span>
              </p>
              <p>
                {t('admin.metadata.updatedAtLabel')}
                <span
                  className="text-zinc-300"
                  title={formatDateTime(rankings?.douban_trending_movie?.updated_at)}
                >
                  {formatRelativeTime(rankings?.douban_trending_movie?.updated_at)}
                </span>
              </p>
            </div>
          </div>

          <div className="rounded-2xl border border-sky-500/25 bg-gradient-to-br from-sky-950/35 via-zinc-950/80 to-zinc-950/90 p-5">
            <div className="flex items-start justify-between gap-3">
              <div>
                <span className="inline-flex items-center gap-1 rounded-md border border-sky-400/45 bg-sky-500/15 px-2.5 py-0.5 text-[11px] font-semibold text-sky-200">
                  <Tv className="h-3 w-3 text-sky-400" />
                  <span>{t('admin.metadata.doubanHotTv')}</span>
                </span>
                <p className="mt-2 text-2xl font-bold text-white">
                  {rankingsLoading ? '...' : rankings?.douban_trending_tv?.count ?? 0}
                  <span className="ml-1.5 text-xs font-normal text-zinc-400">
                    {t('admin.metadata.cachedItemsUnit')}
                  </span>
                </p>
              </div>
              <Tv className="h-5 w-5 text-sky-400/80" />
            </div>
            <div className="mt-3 space-y-1 text-xs text-zinc-400">
              <p className="truncate">
                {t('admin.metadata.sourceLabel')}
                <span className="text-zinc-300">
                  {rankings?.douban_trending_tv?.source || t('admin.metadata.doubanHotTv')}
                </span>
              </p>
              <p>
                {t('admin.metadata.updatedAtLabel')}
                <span
                  className="text-zinc-300"
                  title={formatDateTime(rankings?.douban_trending_tv?.updated_at)}
                >
                  {formatRelativeTime(rankings?.douban_trending_tv?.updated_at)}
                </span>
              </p>
            </div>
          </div>
        </div>
      </section>

      {/* Library Metadata Manager */}
      <section className="space-y-4 border-t border-white/8 pt-6">
        <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
          <div>
            <h2 className="text-base font-semibold text-zinc-100">
              {t('admin.metadata.managerTitle')}
            </h2>
            <p className="mt-0.5 text-xs text-zinc-400">
              {t('admin.metadata.managerCountSub', { count: totalCount })}
              {itemsFetching && !itemsLoading ? ` · ${t('common.loading')}` : ''}
            </p>
          </div>

          <div className="flex flex-wrap items-center gap-2.5">
            <div className="relative flex-1 sm:w-56 sm:flex-none">
              <Search className="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-zinc-500" />
              <input
                type="text"
                value={searchTerm}
                onChange={(e) => {
                  setSearchTerm(e.target.value)
                  setPage(0)
                }}
                placeholder={t('admin.metadata.searchPlaceholder')}
                className="w-full rounded-xl border border-white/10 bg-zinc-900/90 py-2 pl-8 pr-3 text-xs text-zinc-100 outline-none focus:border-amber-400/60"
              />
            </div>

            <div className="inline-flex rounded-xl border border-white/10 bg-zinc-950 p-1">
              {(
                [
                  { value: 'ALL', label: t('common.all') },
                  { value: 'Movie', label: t('admin.metadata.typeMovie') },
                  { value: 'Series', label: t('admin.metadata.typeSeries') },
                ] as const
              ).map((opt) => (
                <button
                  key={opt.value}
                  type="button"
                  onClick={() => {
                    setTypeFilter(opt.value)
                    setPage(0)
                  }}
                  className={`rounded-lg px-2.5 py-1 text-xs font-medium transition-colors ${
                    typeFilter === opt.value
                      ? 'bg-amber-500 text-zinc-950'
                      : 'text-zinc-400 hover:text-zinc-100'
                  }`}
                >
                  {opt.label}
                </button>
              ))}
            </div>

            <select
              value={tagFilter}
              onChange={(e) => {
                setTagFilter(e.target.value)
                setPage(0)
              }}
              aria-label={t('admin.metadata.filterByTagAria')}
              className="rounded-xl border border-white/10 bg-zinc-900 px-3 py-2 text-xs text-zinc-200 outline-none focus:border-amber-400/60"
            >
              <option value="">{t('admin.metadata.allTags')}</option>
              {availableTags.map((tag) => (
                <option key={tag} value={tag}>
                  {formatTagDisplay(tag)}
                </option>
              ))}
            </select>
          </div>
        </div>

        {itemsLoading ? (
          <div className="flex h-44 items-center justify-center">
            <Loader2 className="h-6 w-6 animate-spin text-amber-400" />
          </div>
        ) : itemsIsError ? (
          <QueryErrorState
            compact
            title={t('admin.metadata.loadError')}
            error={itemsError}
            onRetry={() => void refetchItems()}
          />
        ) : items.length === 0 ? (
          <div className="rounded-2xl border border-dashed border-white/10 bg-white/[0.02] py-12 text-center text-sm text-zinc-400">
            {t('admin.metadata.emptyItems')}
          </div>
        ) : (
          <div className="space-y-4">
            <div className="divide-y divide-white/6 overflow-hidden rounded-2xl border border-white/8 bg-zinc-950/60">
              {items.map((item) => {
                const poster = item.ImageTags?.Primary
                  ? getImageUrl(item.Id, 'Primary', { maxWidth: 120, tag: item.ImageTags.Primary })
                  : null
                const fallback = getFallbackImageUrl(item.Id, 'Primary')
                const versionCount = item.MediaSources?.length || 0

                return (
                  <div
                    key={item.Id}
                    className="flex flex-col gap-3 px-4 py-3 transition-colors hover:bg-white/[0.03] lg:flex-row lg:items-center lg:justify-between"
                  >
                    <div className="flex min-w-0 items-center gap-3.5">
                      <Link
                        to={`/item/${item.Id}`}
                        className="h-16 w-11 shrink-0 overflow-hidden rounded-lg bg-zinc-900 ring-1 ring-white/10"
                      >
                        {poster ? (
                          <img
                            src={poster}
                            alt={item.Name}
                            loading="lazy"
                            onError={(e) => {
                              if (fallback && e.currentTarget.src !== fallback) {
                                e.currentTarget.src = fallback
                              }
                            }}
                            className="h-full w-full object-cover"
                          />
                        ) : (
                          <div className="flex h-full w-full items-center justify-center text-zinc-600">
                            <Film className="h-4 w-4" />
                          </div>
                        )}
                      </Link>

                      <div className="min-w-0 space-y-1">
                        <div className="flex flex-wrap items-center gap-2">
                          <Link
                            to={`/item/${item.Id}`}
                            className="truncate text-sm font-semibold text-zinc-100 hover:text-amber-300"
                          >
                            {item.Name}
                          </Link>
                          {item.ProductionYear && (
                            <span className="text-xs text-zinc-500">{item.ProductionYear}</span>
                          )}
                          <span className="rounded bg-white/8 px-1.5 py-0.5 text-[10px] font-medium text-zinc-400">
                            {item.Type === 'Series'
                              ? t('admin.metadata.typeSeries')
                              : t('admin.metadata.typeMovie')}
                          </span>
                          {versionCount > 1 && (
                            <span className="inline-flex items-center gap-1 rounded bg-amber-500/15 px-1.5 py-0.5 text-[10px] font-medium text-amber-300">
                              <Layers className="h-2.5 w-2.5" />
                              {t('admin.metadata.versionsCount', { count: versionCount })}
                            </span>
                          )}
                        </div>

                        <div className="flex flex-wrap items-center gap-1.5">
                          {item.OriginalTitle && item.OriginalTitle !== item.Name && (
                            <span className="truncate text-[11px] text-zinc-500">
                              {item.OriginalTitle}
                            </span>
                          )}
                          {item.Tags?.includes(TAG_TMDB_HOT) && (
                            <span className="inline-flex items-center gap-0.5 rounded border border-orange-400/45 bg-orange-500/15 px-1.5 py-0.5 text-[10px] font-semibold text-orange-200">
                              <Flame className="h-2.5 w-2.5 text-orange-400" />
                              <span>{t('admin.metadata.tmdbTrending')}</span>
                            </span>
                          )}
                          {!item.Tags?.includes(TAG_TMDB_HOT) &&
                            item.Tags?.some((tag) => tag.toLowerCase() === 'trending') && (
                              <span className="inline-flex items-center gap-0.5 rounded border border-orange-400/45 bg-orange-500/15 px-1.5 py-0.5 text-[10px] font-semibold text-orange-200">
                                <Flame className="h-2.5 w-2.5 text-orange-400" />
                                <span>{t('nav.trending')}</span>
                              </span>
                            )}
                          {item.Tags?.includes(TAG_DOUBAN_HOT_MOVIE) && (
                            <span className="rounded border border-teal-400/40 bg-teal-500/15 px-1.5 py-0.5 text-[10px] font-semibold text-teal-200">
                              {t('admin.metadata.doubanHotMovie')}
                            </span>
                          )}
                          {item.Tags?.includes(TAG_DOUBAN_HOT_TV) && (
                            <span className="rounded border border-sky-400/40 bg-sky-500/15 px-1.5 py-0.5 text-[10px] font-semibold text-sky-200">
                              {t('admin.metadata.doubanHotTv')}
                            </span>
                          )}
                          {item.Tags?.includes(TAG_DOUBAN_TOP250) && (
                            <span className="rounded border border-emerald-400/40 bg-emerald-500/15 px-1.5 py-0.5 text-[10px] font-semibold text-emerald-200">
                              {t('admin.metadata.doubanTop250')}
                            </span>
                          )}
                          {item.Tags?.includes(TAG_IMDB_TOP250) && (
                            <span className="rounded border border-amber-400/40 bg-amber-500/15 px-1.5 py-0.5 text-[10px] font-semibold text-amber-200">
                              {t('admin.metadata.imdbTop250')}
                            </span>
                          )}
                        </div>
                      </div>
                    </div>

                    <div className="flex flex-wrap items-center gap-1.5 lg:shrink-0">
                      <button
                        type="button"
                        onClick={() => setEditingItem(item)}
                        className="inline-flex items-center gap-1 rounded-lg border border-white/10 bg-white/[0.04] px-2.5 py-1.5 text-xs font-medium text-zinc-200 transition-colors hover:border-amber-400/40 hover:bg-white/10"
                      >
                        <Pencil className="h-3 w-3" />
                        <span>{t('detail.editMetadata')}</span>
                      </button>
                      <button
                        type="button"
                        onClick={() => setImageItem(item)}
                        className="inline-flex items-center gap-1 rounded-lg border border-white/10 bg-white/[0.04] px-2.5 py-1.5 text-xs font-medium text-zinc-200 transition-colors hover:border-amber-400/40 hover:bg-white/10"
                      >
                        <ImageIcon className="h-3 w-3" />
                        <span>{t('admin.metadata.imagesPosterBtn')}</span>
                      </button>
                      <button
                        type="button"
                        onClick={() => setIdentifyItem(item)}
                        className="inline-flex items-center gap-1 rounded-lg border border-white/10 bg-white/[0.04] px-2.5 py-1.5 text-xs font-medium text-zinc-200 transition-colors hover:border-amber-400/40 hover:bg-white/10"
                      >
                        <ScanSearch className="h-3 w-3" />
                        <span>{t('detail.identify')}</span>
                      </button>
                      <button
                        type="button"
                        disabled={refreshingId === item.Id}
                        onClick={() => void handleRefreshItem(item)}
                        className="inline-flex items-center gap-1 rounded-lg border border-white/10 bg-white/[0.04] px-2.5 py-1.5 text-xs font-medium text-zinc-200 transition-colors hover:border-amber-400/40 hover:bg-white/10 disabled:opacity-50"
                      >
                        <RefreshCw
                          className={`h-3 w-3 ${refreshingId === item.Id ? 'animate-spin' : ''}`}
                        />
                        <span>{t('common.refresh')}</span>
                      </button>
                      <button
                        type="button"
                        disabled={writingNfoId === item.Id}
                        onClick={() => void handleWriteItemNfo(item)}
                        title={t('admin.metadata.writeNfoTitle')}
                        className="inline-flex items-center gap-1 rounded-lg border border-emerald-500/25 bg-emerald-500/10 px-2.5 py-1.5 text-xs font-medium text-emerald-300 transition-colors hover:bg-emerald-500/20 disabled:opacity-50"
                      >
                        {writingNfoId === item.Id ? (
                          <Loader2 className="h-3 w-3 animate-spin" />
                        ) : (
                          <FileCode2 className="h-3 w-3" />
                        )}
                        <span>{t('admin.metadata.writeNfoBtn')}</span>
                      </button>
                      <button
                        type="button"
                        onClick={() => setDeletingItem(item)}
                        className="inline-flex items-center gap-1 rounded-lg border border-red-500/25 bg-red-500/10 px-2.5 py-1.5 text-xs font-medium text-red-300 transition-colors hover:bg-red-500/20"
                      >
                        <Trash2 className="h-3 w-3" />
                        <span>{t('common.delete')}</span>
                      </button>
                    </div>
                  </div>
                )
              })}
            </div>

            {/* Pagination Bar */}
            <div className="flex flex-col gap-3 rounded-2xl border border-white/8 bg-zinc-950/60 px-4 py-3 text-xs text-zinc-400 sm:flex-row sm:items-center sm:justify-between">
              <div className="flex flex-wrap items-center gap-3">
                <span>
                  {(() => {
                    const rawSummary = t('admin.metadata.pageSummary', {
                      total: totalCount,
                      start: '__RANGE_START__',
                      end: '__RANGE_END__',
                      page: currentPage + 1,
                      totalPages,
                    })
                    const segments = rawSummary.split(/__RANGE_START__.?__RANGE_END__/)
                    if (segments.length === 2) {
                      return (
                        <>
                          {segments[0]}
                          <strong className="text-zinc-100">
                            {rangeStart}–{rangeEnd}
                          </strong>
                          {segments[1]}
                        </>
                      )
                    }
                    return t('admin.metadata.pageSummary', {
                      total: totalCount,
                      start: rangeStart,
                      end: rangeEnd,
                      page: currentPage + 1,
                      totalPages,
                    })
                  })()}
                </span>
                <label className="inline-flex items-center gap-1.5">
                  <span>{t('admin.metadata.perPageLabel')}</span>
                  <select
                    value={pageSize}
                    onChange={(e) => {
                      setPageSize(Number(e.target.value))
                      setPage(0)
                    }}
                    aria-label={t('admin.metadata.perPageAria')}
                    className="rounded-lg border border-white/10 bg-zinc-900 px-2 py-1 text-xs text-zinc-200 outline-none focus:border-amber-400/60"
                  >
                    {PAGE_SIZE_OPTIONS.map((size) => (
                      <option key={size} value={size}>
                        {t('admin.metadata.itemsCountOpt', { size })}
                      </option>
                    ))}
                  </select>
                </label>
              </div>

              <div className="flex flex-wrap items-center gap-1.5">
                <button
                  type="button"
                  disabled={currentPage <= 0}
                  onClick={() => setPage(0)}
                  aria-label={t('admin.metadata.firstPage')}
                  className="inline-flex items-center gap-1 rounded-lg border border-white/10 bg-white/[0.04] px-2.5 py-1.5 text-xs font-medium text-zinc-200 transition-colors hover:bg-white/10 disabled:cursor-not-allowed disabled:opacity-40"
                >
                  <ChevronsLeft className="h-3.5 w-3.5" />
                  <span>{t('admin.metadata.firstPage')}</span>
                </button>
                <button
                  type="button"
                  disabled={currentPage <= 0}
                  onClick={() => setPage((p) => Math.max(0, p - 1))}
                  aria-label={t('common.prev')}
                  className="inline-flex items-center gap-1 rounded-lg border border-white/10 bg-white/[0.04] px-2.5 py-1.5 text-xs font-medium text-zinc-200 transition-colors hover:bg-white/10 disabled:cursor-not-allowed disabled:opacity-40"
                >
                  <ChevronLeft className="h-3.5 w-3.5" />
                  <span>{t('common.prev')}</span>
                </button>

                {pageButtons.map((entry, idx) =>
                  entry === 'ellipsis' ? (
                    <span key={`ellipsis-${idx}`} className="px-1.5 text-zinc-500">
                      ...
                    </span>
                  ) : (
                    <button
                      key={entry}
                      type="button"
                      onClick={() => setPage(entry)}
                      aria-label={t('admin.metadata.pageNumAria', { page: entry + 1 })}
                      aria-current={entry === currentPage ? 'page' : undefined}
                      className={`min-w-[2rem] rounded-lg px-2.5 py-1.5 text-xs font-semibold transition-colors ${
                        entry === currentPage
                          ? 'bg-amber-500 text-zinc-950'
                          : 'border border-white/10 bg-white/[0.04] text-zinc-300 hover:bg-white/10'
                      }`}
                    >
                      {entry + 1}
                    </button>
                  ),
                )}

                <button
                  type="button"
                  disabled={currentPage >= totalPages - 1}
                  onClick={() => setPage((p) => Math.min(totalPages - 1, p + 1))}
                  aria-label={t('common.next')}
                  className="inline-flex items-center gap-1 rounded-lg border border-white/10 bg-white/[0.04] px-2.5 py-1.5 text-xs font-medium text-zinc-200 transition-colors hover:bg-white/10 disabled:cursor-not-allowed disabled:opacity-40"
                >
                  <span>{t('common.next')}</span>
                  <ChevronRight className="h-3.5 w-3.5" />
                </button>
                <button
                  type="button"
                  disabled={currentPage >= totalPages - 1}
                  onClick={() => setPage(totalPages - 1)}
                  aria-label={t('admin.metadata.lastPage')}
                  className="inline-flex items-center gap-1 rounded-lg border border-white/10 bg-white/[0.04] px-2.5 py-1.5 text-xs font-medium text-zinc-200 transition-colors hover:bg-white/10 disabled:cursor-not-allowed disabled:opacity-40"
                >
                  <span>{t('admin.metadata.lastPage')}</span>
                  <ChevronsRight className="h-3.5 w-3.5" />
                </button>
              </div>
            </div>
          </div>
        )}
      </section>

      <MetadataEditorModal
        open={Boolean(editingItem)}
        item={editingItem}
        onClose={() => setEditingItem(null)}
        onSaved={(msg) => setFeedback({ type: 'success', message: msg })}
      />
      <ImageManagerModal
        open={Boolean(imageItem)}
        item={imageItem}
        onClose={() => setImageItem(null)}
        onUpdated={(msg) => setFeedback({ type: 'success', message: msg })}
      />
      <IdentifyModal
        open={Boolean(identifyItem)}
        item={identifyItem}
        onClose={() => setIdentifyItem(null)}
        onApplied={(msg) => setFeedback({ type: 'success', message: msg })}
      />
      <AdminModal
        open={Boolean(deletingItem)}
        title={t('admin.metadata.deleteModalTitle')}
        subtitle={t('admin.metadata.deleteModalSubtitle')}
        maxWidthClass="max-w-lg"
        onClose={() => {
          if (!deleteMutation.isPending) setDeletingItem(null)
        }}
      >
        <div className="space-y-4">
          <div
            role="alert"
            data-testid="metadata-physical-delete-warning"
            className="flex items-start gap-3 rounded-xl border border-red-500/40 bg-red-500/15 p-4 text-sm text-red-100"
          >
            <AlertTriangle className="mt-0.5 h-5 w-5 shrink-0 text-red-400" />
            <div className="min-w-0 space-y-1.5">
              <div className="flex flex-wrap items-center gap-2">
                <p className="font-semibold text-red-100">
                  {t('admin.metadata.deleteConfirmTitle', { name: deletingItem?.Name || '' })}
                </p>
                <span className="rounded bg-red-600/35 px-1.5 py-0.5 text-[10px] font-bold uppercase tracking-wide text-red-200 ring-1 ring-red-400/40">
                  {adminLabel(
                    t,
                    locale,
                    'admin.metadata.deletePhysicalBadge',
                    '永久磁盘删除 · 不可撤销',
                    'Permanent Disk Deletion · Cannot Be Undone',
                  )}
                </span>
              </div>
              <p className="text-xs leading-relaxed text-red-100/90">
                {adminLabel(
                  t,
                  locale,
                  'admin.metadata.deletePhysicalWarnDesc',
                  '高危操作：确认后将从磁盘永久物理删除原始媒体文件、关联的外挂字幕 / NFO / 海报剧照以及数据库索引记录（若删除剧集 Series 或分季 Season，将递归删除磁盘上所有分集视频文件），此操作不可撤销！',
                  'DANGER: Confirming will permanently delete the original media file(s) from disk, sidecar subtitles/NFO/artwork, and database records (for a Series or Season, ALL episode video files on disk will be recursively deleted). This action CANNOT be undone!',
                )}
              </p>
              {(deletingItem?.Type === 'Series' || deletingItem?.Type === 'Season') && (
                <p className="rounded-lg border border-red-400/30 bg-red-950/50 px-2.5 py-1.5 text-[11px] font-medium text-red-200">
                  {adminLabel(
                    t,
                    locale,
                    'admin.metadata.deleteSeriesRecursiveWarn',
                    '当前所选为整部剧集或分季，确认后其下属全部剧集视频文件均会从磁盘永久删除！',
                    'Selected item is a Series or Season: all child episode video files on disk will be recursively deleted!',
                  )}
                </p>
              )}
            </div>
          </div>

          <div
            data-testid="delete-info-paths"
            className="rounded-xl border border-white/10 bg-black/40 p-3.5 text-xs"
          >
            {deleteInfoLoading ? (
              <div className="flex items-center gap-2 py-2 text-zinc-400">
                <Loader2 className="h-4 w-4 animate-spin text-amber-400" />
                <span>
                  {adminLabel(
                    t,
                    locale,
                    'admin.metadata.deleteInfoLoading',
                    '正在读取即将从磁盘删除的文件路径列表...',
                    'Loading physical disk paths to be deleted...',
                  )}
                </span>
              </div>
            ) : deleteInfoPaths.length > 0 ? (
              <div className="space-y-2">
                <p className="font-semibold text-red-300">
                  {adminLabel(
                    t,
                    locale,
                    'admin.metadata.deleteInfoPathsCount',
                    '即将从磁盘永久删除以下文件 / 目录（共 {{count}} 项）：',
                    'Physical files / directories to be permanently deleted ({{count}}):',
                    { count: deleteInfoPaths.length },
                  )}
                </p>
                <ul className="max-h-44 space-y-1 overflow-y-auto rounded-lg border border-red-500/20 bg-zinc-950/80 p-2.5 font-mono text-[11px] text-zinc-200">
                  {deleteInfoPaths.map((p, idx) => (
                    <li key={`${p}-${idx}`} className="break-all">
                      {p}
                    </li>
                  ))}
                </ul>
              </div>
            ) : deleteInfoError ? (
              <div className="space-y-1 text-amber-200/90">
                <p>
                  {adminLabel(
                    t,
                    locale,
                    'admin.metadata.deleteInfoErrorFallback',
                    '无法加载待删除磁盘路径预览；确认删除仍会清理服务端关联的原始媒体文件与数据库记录。',
                    'Unable to preview affected disk paths; confirming will still delete associated media files on disk and database records.',
                  )}
                </p>
                {deletingItem?.Path && (
                  <p className="break-all font-mono text-[11px] text-zinc-400">
                    {deletingItem.Path}
                  </p>
                )}
              </div>
            ) : (
              <div className="space-y-1 text-zinc-400">
                <p>
                  {adminLabel(
                    t,
                    locale,
                    'admin.metadata.deleteInfoEmptyFallback',
                    '服务端未返回可删除的媒体库内物理文件路径（若存在关联文件与元数据记录将一并清理）。',
                    'No physical disk paths reported for this item (associated files within library roots and database records will be removed).',
                  )}
                </p>
                {deletingItem?.Path && (
                  <p className="break-all font-mono text-[11px] text-zinc-300">
                    {deletingItem.Path}
                  </p>
                )}
              </div>
            )}
          </div>

          <div className="flex items-center justify-end gap-3">
            <button
              type="button"
              disabled={deleteMutation.isPending}
              onClick={() => setDeletingItem(null)}
              className="rounded-xl border border-white/10 bg-white/[0.04] px-4 py-2 text-sm font-medium text-zinc-300 hover:bg-white/10 disabled:opacity-50"
            >
              {t('common.cancel')}
            </button>
            <button
              type="button"
              disabled={deleteMutation.isPending}
              onClick={() => deletingItem && deleteMutation.mutate(deletingItem.Id)}
              className="inline-flex items-center gap-2 rounded-xl bg-red-600 px-5 py-2 text-sm font-semibold text-white transition-colors hover:bg-red-500 disabled:opacity-60"
            >
              {deleteMutation.isPending && <Loader2 className="h-4 w-4 animate-spin" />}
              <span>{t('admin.users.confirmDelete')}</span>
            </button>
          </div>
        </div>
      </AdminModal>
    </div>
  )
}
