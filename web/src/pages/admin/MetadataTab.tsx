import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  AlertTriangle,
  Award,
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
} from 'lucide-react'
import { Link } from 'react-router-dom'
import { api, getFallbackImageUrl, getImageUrl } from '../../api/client'
import type { BaseItemDto } from '../../api/types'
import { IdentifyModal } from '../../components/media/IdentifyModal'
import { ImageManagerModal } from '../../components/media/ImageManagerModal'
import { MetadataEditorModal } from '../../components/media/MetadataEditorModal'
import { useAuth } from '../../context/AuthContext'
import { AdminModal, FeedbackBanner, formatDateTime, formatRelativeTime } from './adminUtils'

type TypeFilter = 'ALL' | 'Movie' | 'Series'

export function MetadataTab() {
  const { auth } = useAuth()
  const queryClient = useQueryClient()
  const userId = auth!.userId

  const [searchTerm, setSearchTerm] = useState('')
  const [typeFilter, setTypeFilter] = useState<TypeFilter>('ALL')
  const [tagFilter, setTagFilter] = useState('')
  const [feedback, setFeedback] = useState<{ type: 'success' | 'error'; message: string } | null>(
    null,
  )

  const [editingItem, setEditingItem] = useState<BaseItemDto | null>(null)
  const [imageItem, setImageItem] = useState<BaseItemDto | null>(null)
  const [identifyItem, setIdentifyItem] = useState<BaseItemDto | null>(null)
  const [deletingItem, setDeletingItem] = useState<BaseItemDto | null>(null)
  const [refreshingId, setRefreshingId] = useState<string | null>(null)

  const { data: rankings, isLoading: rankingsLoading } = useQuery({
    queryKey: ['rankings'],
    queryFn: () => api.getRankings(),
  })

  const { data: tagsData } = useQuery({
    queryKey: ['tags', userId],
    queryFn: () => api.getTags({ userId }),
  })

  const { data: itemsData, isLoading: itemsLoading } = useQuery({
    queryKey: ['admin-metadata-items', userId, typeFilter, tagFilter, searchTerm.trim()],
    queryFn: () =>
      api.getItems(userId, {
        includeItemTypes: typeFilter === 'ALL' ? 'Movie,Series' : typeFilter,
        tags: tagFilter || undefined,
        searchTerm: searchTerm.trim() || undefined,
        sortBy: 'DateCreated',
        sortOrder: 'Descending',
        limit: 200,
      }),
  })

  const items = itemsData?.Items || []

  const availableTags = useMemo(() => {
    const set = new Set<string>(['Trending', '豆瓣Top250', 'IMDb Top250'])
    for (const t of tagsData?.Items || []) {
      if (t.Name) set.add(t.Name)
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
      const trendPart =
        res.tmdb_trending_count !== undefined ? ` / TMDB Trending (${res.tmdb_trending_count} 条)` : ''
      setFeedback({
        type: 'success',
        message: `榜单同步完成：豆瓣 Top250 (${res.douban_count} 条) / IMDb Top250 (${res.imdb_count} 条)${trendPart}，命中 ${res.matched_items} 部媒体，新增打标 ${res.tags_added} 次`,
      })
    },
    onError: (err) => {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '同步榜单失败',
      })
    },
  })

  const handleRefreshItem = async (item: BaseItemDto) => {
    setRefreshingId(item.Id)
    try {
      await api.refreshItemMetadata(item.Id, false)
      await queryClient.invalidateQueries({ queryKey: ['admin-metadata-items'] })
      setFeedback({ type: 'success', message: `已刷新「${item.Name}」的元数据` })
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '刷新元数据失败',
      })
    } finally {
      setRefreshingId(null)
    }
  }

  const deleteMutation = useMutation({
    mutationFn: (itemId: string) => api.deleteItem(itemId),
    onSuccess: async () => {
      const removedName = deletingItem?.Name || '媒体'
      setDeletingItem(null)
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['admin-metadata-items'] }),
        queryClient.invalidateQueries({ queryKey: ['items'] }),
        queryClient.invalidateQueries({ queryKey: ['latestItems'] }),
      ])
      setFeedback({ type: 'success', message: `已删除「${removedName}」` })
    },
    onError: (err) => {
      setDeletingItem(null)
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '删除媒体失败',
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

      {/* 顶部区域：豆瓣 Top 250 / IMDb Top 250 榜单缓存卡片 */}
      <section className="space-y-4">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h2 className="flex items-center gap-2 text-base font-semibold text-zinc-100">
              <Award className="h-4 w-4 text-amber-400" />
              豆瓣 Top 250 / IMDb Top 250 榜单缓存
            </h2>
            <p className="mt-0.5 text-xs text-zinc-400">
              自动同步权威影史榜单并为库内匹配影片打上「豆瓣Top250」「IMDb Top250」标签
            </p>
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
              {refreshRankingsMutation.isPending ? '正在同步并打标...' : '立即同步并自动打标'}
            </span>
          </button>
        </div>

        <div className="grid gap-4 md:grid-cols-3">
          {/* 豆瓣 Top 250 卡片 */}
          <div className="rounded-2xl border border-emerald-500/25 bg-gradient-to-br from-emerald-950/35 via-zinc-950/80 to-zinc-950/90 p-5">
            <div className="flex items-start justify-between gap-3">
              <div>
                <span className="inline-flex items-center rounded-md border border-emerald-400/40 bg-emerald-500/15 px-2.5 py-0.5 text-[11px] font-semibold text-emerald-200">
                  豆瓣 Top 250
                </span>
                <p className="mt-2 text-2xl font-bold text-white">
                  {rankingsLoading ? '...' : rankings?.douban_top250.count ?? 0}
                  <span className="ml-1.5 text-xs font-normal text-zinc-400">部缓存条目</span>
                </p>
              </div>
              <Sparkles className="h-5 w-5 text-emerald-400/80" />
            </div>
            <div className="mt-3 space-y-1 text-xs text-zinc-400">
              <p className="truncate">
                来源：<span className="text-zinc-300">{rankings?.douban_top250.source || '-'}</span>
              </p>
              <p>
                更新时间：
                <span
                  className="text-zinc-300"
                  title={formatDateTime(rankings?.douban_top250.updated_at)}
                >
                  {formatRelativeTime(rankings?.douban_top250.updated_at)}
                </span>
              </p>
            </div>
          </div>

          {/* IMDb Top 250 卡片 */}
          <div className="rounded-2xl border border-amber-500/25 bg-gradient-to-br from-amber-950/35 via-zinc-950/80 to-zinc-950/90 p-5">
            <div className="flex items-start justify-between gap-3">
              <div>
                <span className="inline-flex items-center rounded-md border border-amber-400/40 bg-amber-500/15 px-2.5 py-0.5 text-[11px] font-semibold text-amber-200">
                  IMDb Top 250
                </span>
                <p className="mt-2 text-2xl font-bold text-white">
                  {rankingsLoading ? '...' : rankings?.imdb_top250.count ?? 0}
                  <span className="ml-1.5 text-xs font-normal text-zinc-400">部缓存条目</span>
                </p>
              </div>
              <Sparkles className="h-5 w-5 text-amber-400/80" />
            </div>
            <div className="mt-3 space-y-1 text-xs text-zinc-400">
              <p className="truncate">
                来源：<span className="text-zinc-300">{rankings?.imdb_top250.source || '-'}</span>
              </p>
              <p>
                更新时间：
                <span
                  className="text-zinc-300"
                  title={formatDateTime(rankings?.imdb_top250.updated_at)}
                >
                  {formatRelativeTime(rankings?.imdb_top250.updated_at)}
                </span>
              </p>
            </div>
          </div>

          {/* TMDB Trending 热门榜卡片 */}
          <div className="rounded-2xl border border-orange-500/25 bg-gradient-to-br from-orange-950/35 via-zinc-950/80 to-zinc-950/90 p-5">
            <div className="flex items-start justify-between gap-3">
              <div>
                <span className="inline-flex items-center gap-1 rounded-md border border-orange-400/45 bg-orange-500/15 px-2.5 py-0.5 text-[11px] font-semibold text-orange-200">
                  <Flame className="h-3 w-3 text-orange-400" />
                  <span>TMDB Trending</span>
                </span>
                <p className="mt-2 text-2xl font-bold text-white">
                  {rankingsLoading ? '...' : rankings?.tmdb_trending?.count ?? 0}
                  <span className="ml-1.5 text-xs font-normal text-zinc-400">部缓存条目</span>
                </p>
              </div>
              <Flame className="h-5 w-5 text-orange-400/80" />
            </div>
            <div className="mt-3 space-y-1 text-xs text-zinc-400">
              <p className="truncate">
                来源：
                <span className="text-zinc-300">{rankings?.tmdb_trending?.source || 'TMDB 热门榜'}</span>
              </p>
              <p>
                更新时间：
                <span
                  className="text-zinc-300"
                  title={formatDateTime(rankings?.tmdb_trending?.updated_at)}
                >
                  {formatRelativeTime(rankings?.tmdb_trending?.updated_at)}
                </span>
              </p>
            </div>
          </div>
        </div>
      </section>

      {/* 下方区域：全库媒体元数据管理器 */}
      <section className="space-y-4 border-t border-white/8 pt-6">
        <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
          <div>
            <h2 className="text-base font-semibold text-zinc-100">全库媒体元数据管理器</h2>
            <p className="mt-0.5 text-xs text-zinc-400">
              共 {itemsData?.TotalRecordCount ?? items.length} 部影片与剧集，支持快速编辑、换图、识别与刷新
            </p>
          </div>

          {/* 搜索与筛选 */}
          <div className="flex flex-wrap items-center gap-2.5">
            <div className="relative flex-1 sm:w-56 sm:flex-none">
              <Search className="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-zinc-500" />
              <input
                type="text"
                value={searchTerm}
                onChange={(e) => setSearchTerm(e.target.value)}
                placeholder="搜索片名 / 原名 / 演员..."
                className="w-full rounded-xl border border-white/10 bg-zinc-900/90 py-2 pl-8 pr-3 text-xs text-zinc-100 outline-none focus:border-amber-400/60"
              />
            </div>

            <div className="inline-flex rounded-xl border border-white/10 bg-zinc-950 p-1">
              {(
                [
                  { value: 'ALL', label: '全部' },
                  { value: 'Movie', label: '电影' },
                  { value: 'Series', label: '剧集' },
                ] as const
              ).map((opt) => (
                <button
                  key={opt.value}
                  type="button"
                  onClick={() => setTypeFilter(opt.value)}
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
              onChange={(e) => setTagFilter(e.target.value)}
              aria-label="按标签筛选"
              className="rounded-xl border border-white/10 bg-zinc-900 px-3 py-2 text-xs text-zinc-200 outline-none focus:border-amber-400/60"
            >
              <option value="">全部标签</option>
              {availableTags.map((t) => (
                <option key={t} value={t}>
                  {t}
                </option>
              ))}
            </select>
          </div>
        </div>

        {/* 媒体列表 */}
        {itemsLoading ? (
          <div className="flex h-44 items-center justify-center">
            <Loader2 className="h-6 w-6 animate-spin text-amber-400" />
          </div>
        ) : items.length === 0 ? (
          <div className="rounded-2xl border border-dashed border-white/10 bg-white/[0.02] py-12 text-center text-sm text-zinc-400">
            没有符合当前筛选条件的媒体条目
          </div>
        ) : (
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
                          {item.Type === 'Series' ? '剧集' : '电影'}
                        </span>
                        {versionCount > 1 && (
                          <span className="inline-flex items-center gap-1 rounded bg-amber-500/15 px-1.5 py-0.5 text-[10px] font-medium text-amber-300">
                            <Layers className="h-2.5 w-2.5" />
                            {versionCount} 版本
                          </span>
                        )}
                      </div>

                      <div className="flex flex-wrap items-center gap-1.5">
                        {item.OriginalTitle && item.OriginalTitle !== item.Name && (
                          <span className="truncate text-[11px] text-zinc-500">
                            {item.OriginalTitle}
                          </span>
                        )}
                        {item.Tags?.some((t) => t.toLowerCase() === 'trending') && (
                          <span className="inline-flex items-center gap-0.5 rounded border border-orange-400/45 bg-orange-500/15 px-1.5 py-0.5 text-[10px] font-semibold text-orange-200">
                            <Flame className="h-2.5 w-2.5 text-orange-400" />
                            <span>热门</span>
                          </span>
                        )}
                        {item.Tags?.includes('豆瓣Top250') && (
                          <span className="rounded border border-emerald-400/40 bg-emerald-500/15 px-1.5 py-0.5 text-[10px] font-semibold text-emerald-200">
                            豆瓣 Top250
                          </span>
                        )}
                        {item.Tags?.includes('IMDb Top250') && (
                          <span className="rounded border border-amber-400/40 bg-amber-500/15 px-1.5 py-0.5 text-[10px] font-semibold text-amber-200">
                            IMDb Top250
                          </span>
                        )}
                      </div>
                    </div>
                  </div>

                  {/* 行内快捷操作 */}
                  <div className="flex flex-wrap items-center gap-1.5 lg:shrink-0">
                    <button
                      type="button"
                      onClick={() => setEditingItem(item)}
                      className="inline-flex items-center gap-1 rounded-lg border border-white/10 bg-white/[0.04] px-2.5 py-1.5 text-xs font-medium text-zinc-200 transition-colors hover:border-amber-400/40 hover:bg-white/10"
                    >
                      <Pencil className="h-3 w-3" />
                      <span>编辑元数据</span>
                    </button>
                    <button
                      type="button"
                      onClick={() => setImageItem(item)}
                      className="inline-flex items-center gap-1 rounded-lg border border-white/10 bg-white/[0.04] px-2.5 py-1.5 text-xs font-medium text-zinc-200 transition-colors hover:border-amber-400/40 hover:bg-white/10"
                    >
                      <ImageIcon className="h-3 w-3" />
                      <span>图片/海报</span>
                    </button>
                    <button
                      type="button"
                      onClick={() => setIdentifyItem(item)}
                      className="inline-flex items-center gap-1 rounded-lg border border-white/10 bg-white/[0.04] px-2.5 py-1.5 text-xs font-medium text-zinc-200 transition-colors hover:border-amber-400/40 hover:bg-white/10"
                    >
                      <ScanSearch className="h-3 w-3" />
                      <span>识别</span>
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
                      <span>刷新</span>
                    </button>
                    <button
                      type="button"
                      onClick={() => setDeletingItem(item)}
                      className="inline-flex items-center gap-1 rounded-lg border border-red-500/25 bg-red-500/10 px-2.5 py-1.5 text-xs font-medium text-red-300 transition-colors hover:bg-red-500/20"
                    >
                      <Trash2 className="h-3 w-3" />
                      <span>删除</span>
                    </button>
                  </div>
                </div>
              )
            })}
          </div>
        )}
      </section>

      {/* 弹窗组 */}
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
        title="确认删除媒体"
        subtitle="此操作不可撤销"
        maxWidthClass="max-w-md"
        onClose={() => setDeletingItem(null)}
      >
        <div className="space-y-5">
          <div className="flex items-start gap-3 rounded-xl border border-red-500/25 bg-red-500/10 p-4 text-sm text-red-200">
            <AlertTriangle className="mt-0.5 h-5 w-5 shrink-0 text-red-400" />
            <div>
              <p className="font-semibold">确定要删除「{deletingItem?.Name}」吗？</p>
              <p className="mt-1 text-xs leading-relaxed text-red-200/80">
                将从媒体库索引中移除该条目的元数据、图片与关联记录。
              </p>
            </div>
          </div>
          <div className="flex items-center justify-end gap-3">
            <button
              type="button"
              onClick={() => setDeletingItem(null)}
              className="rounded-xl border border-white/10 bg-white/[0.04] px-4 py-2 text-sm font-medium text-zinc-300 hover:bg-white/10"
            >
              取消
            </button>
            <button
              type="button"
              disabled={deleteMutation.isPending}
              onClick={() => deletingItem && deleteMutation.mutate(deletingItem.Id)}
              className="inline-flex items-center gap-2 rounded-xl bg-red-600 px-5 py-2 text-sm font-semibold text-white transition-colors hover:bg-red-500 disabled:opacity-60"
            >
              {deleteMutation.isPending && <Loader2 className="h-4 w-4 animate-spin" />}
              <span>确认删除</span>
            </button>
          </div>
        </div>
      </AdminModal>
    </div>
  )
}
