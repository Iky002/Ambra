import { useEffect, useMemo, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  ArrowDownWideNarrow,
  ArrowUpNarrowWide,
  Building2,
  Calendar,
  Check,
  CheckSquare,
  Clock,
  Disc3,
  Eye,
  EyeOff,
  Film,
  Filter,
  FolderPlus,
  LayoutGrid,
  List,
  Loader2,
  Mic2,
  Music,
  RotateCcw,
  Star,
  Tag,
  Tags,
  X,
} from 'lucide-react'
import { Link, useParams, useSearchParams } from 'react-router-dom'
import { api, getFallbackImageUrl, getImageUrl } from '../api/client'
import type { BaseItemDto } from '../api/types'
import { MediaCard } from '../components/MediaCard'
import { AddToCollectionModal } from '../components/media/AddToCollectionModal'
import { QueryErrorState } from '../components/QueryErrorState'
import { useAudioPlayer } from '../context/AudioPlayerContext'
import { useAuth } from '../context/AuthContext'
import { useI18n } from '../i18n/I18nContext'
import { formatTicks } from '../player/subtitleUtils'
import { FeedbackBanner } from './admin/adminUtils'

const SORT_OPTIONS = [
  { value: 'DateCreated', zhLabel: '按添加时间', i18nKey: 'library.sortDateAdded' },
  { value: 'SortName', zhLabel: '按名称排序', i18nKey: 'library.sortName' },
  { value: 'CommunityRating', zhLabel: '按评分最高', i18nKey: 'library.sortRating' },
  { value: 'PremiereDate', zhLabel: '按上映年份', i18nKey: 'library.sortReleaseDate' },
  { value: 'PlayCount', zhLabel: '按播放次数', i18nKey: 'library.sortPlayCount' },
]

const FILTER_OPTIONS = [
  { value: '', zhLabel: '全部状态', i18nKey: 'library.filterAll' },
  { value: 'IsUnplayed', zhLabel: '仅看未播', i18nKey: 'library.filterUnplayed' },
  { value: 'IsPlayed', zhLabel: '仅看已播', i18nKey: 'library.filterPlayed' },
  { value: 'IsFavorite', zhLabel: '仅看收藏', i18nKey: 'library.filterFavorite' },
]

type MusicSubTab = 'MusicAlbum' | 'MusicArtist' | 'Audio'

const MUSIC_SUB_TABS: Array<{ id: MusicSubTab; i18nKey: string; icon: typeof Disc3 }> = [
  { id: 'MusicAlbum', i18nKey: 'library.albums', icon: Disc3 },
  { id: 'MusicArtist', i18nKey: 'library.artists', icon: Mic2 },
  { id: 'Audio', i18nKey: 'library.tracks', icon: Music },
]

const PAGE_SIZE = 36
const VIEW_MODE_STORAGE_KEY = 'ambra_library_view_mode'
const ALPHABET_LETTERS = ['全部', '#', ...'ABCDEFGHIJKLMNOPQRSTUVWXYZ'.split('')] as const

type LibraryViewMode = 'grid' | 'list'

function readStoredViewMode(): LibraryViewMode {
  try {
    const saved = localStorage.getItem(VIEW_MODE_STORAGE_KEY)
    return saved === 'list' ? 'list' : 'grid'
  } catch {
    return 'grid'
  }
}

export function LibraryPage() {
  const { id } = useParams<{ id: string }>()
  const [searchParams, setSearchParams] = useSearchParams()
  const { auth } = useAuth()
  const { locale, t } = useI18n()
  const { currentTrack } = useAudioPlayer()
  const queryClient = useQueryClient()
  const userId = auth!.userId

  const [sortBy, setSortBy] = useState('DateCreated')
  const [sortOrder, setSortOrder] = useState<'Ascending' | 'Descending'>('Descending')
  const [statusFilter, setStatusFilter] = useState('')
  const [letterFilter, setLetterFilter] = useState(() => searchParams.get('letter') || '')
  const [viewMode, setViewMode] = useState<LibraryViewMode>(() => readStoredViewMode())
  const [genreFilter, setGenreFilter] = useState(() => searchParams.get('genre') || '')
  const [yearFilter, setYearFilter] = useState(() => searchParams.get('year') || '')
  const [tagFilter, setTagFilter] = useState(() => searchParams.get('tag') || '')
  const [studioFilter, setStudioFilter] = useState(() => searchParams.get('studio') || '')
  const [musicSubTab, setMusicSubTab] = useState<MusicSubTab>('MusicAlbum')
  const [page, setPage] = useState(0)

  // 多选批量操作状态
  const [selectMode, setSelectMode] = useState(false)
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set())
  const [batchBusy, setBatchBusy] = useState(false)
  const [batchFeedback, setBatchFeedback] = useState<{
    type: 'success' | 'error'
    message: string
  } | null>(null)
  const [collectionModalOpen, setCollectionModalOpen] = useState(false)

  const handleSetViewMode = (mode: LibraryViewMode) => {
    setViewMode(mode)
    try {
      localStorage.setItem(VIEW_MODE_STORAGE_KEY, mode)
    } catch {
      // ignore storage errors
    }
  }

  // 响应 URL query 参数变化（例如从详情页点击流派/年份/标签/制片厂跳转或字母筛选）
  useEffect(() => {
    setGenreFilter(searchParams.get('genre') || '')
    setYearFilter(searchParams.get('year') || '')
    setTagFilter(searchParams.get('tag') || '')
    setStudioFilter(searchParams.get('studio') || '')
    setLetterFilter(searchParams.get('letter') || '')
    setPage(0)
  }, [id, searchParams])

  useEffect(() => {
    setMusicSubTab('MusicAlbum')
    setStatusFilter('')
    setSelectMode(false)
    setSelectedIds(new Set())
    setBatchFeedback(null)
  }, [id])

  // 切换任何筛选条件或音乐子标签时清空已选集合，避免隐藏项被误包含进批量操作
  useEffect(() => {
    setSelectedIds(new Set())
  }, [musicSubTab, statusFilter, genreFilter, yearFilter, tagFilter, studioFilter, letterFilter])

  const updateUrlParam = (key: 'genre' | 'year' | 'tag' | 'studio' | 'letter', value: string) => {
    const next = new URLSearchParams(searchParams)
    if (value) {
      next.set(key, value)
    } else {
      next.delete(key)
    }
    setSearchParams(next, { replace: true })
    setPage(0)
  }

  const { data: libraryInfo, isFetched: libraryInfoFetched } = useQuery({
    queryKey: ['item', userId, id],
    queryFn: () => api.getItem(userId, id!),
    enabled: Boolean(id),
  })

  const { data: genresData } = useQuery({
    queryKey: ['genres', userId, id],
    queryFn: () => api.getGenres({ userId, parentId: id }),
    enabled: Boolean(id),
    staleTime: 60_000,
  })

  const { data: yearsData } = useQuery({
    queryKey: ['years', userId, id],
    queryFn: () => api.getYears({ userId, parentId: id }),
    enabled: Boolean(id),
    staleTime: 60_000,
  })

  const { data: tagsData } = useQuery({
    queryKey: ['tags', userId, id],
    queryFn: () => api.getTags({ userId, parentId: id }),
    enabled: Boolean(id),
    staleTime: 60_000,
  })

  const genreOptions = useMemo(() => {
    const list = (genresData?.Items || []).map((g) => g.Name).filter(Boolean)
    if (genreFilter && !list.includes(genreFilter)) {
      return [genreFilter, ...list]
    }
    return list
  }, [genresData, genreFilter])

  const yearOptions = useMemo(() => {
    const list = (yearsData?.Items || []).map((y) => y.Name).filter(Boolean)
    if (yearFilter && !list.includes(yearFilter)) {
      return [yearFilter, ...list]
    }
    return list
  }, [yearsData, yearFilter])

  const tagOptions = useMemo(() => {
    const list = (tagsData?.Items || []).map((t) => t.Name).filter(Boolean)
    if (tagFilter && !list.includes(tagFilter)) {
      return [tagFilter, ...list]
    }
    return list
  }, [tagsData, tagFilter])

  const isMusicLibrary = (libraryInfo?.CollectionType || '').toLowerCase() === 'music'
  const includeItemTypes = isMusicLibrary
    ? musicSubTab
    : 'Movie,Series,MusicAlbum,Video'

  const { data, isLoading, isFetching, isError, error, refetch } = useQuery({
    queryKey: [
      'libraryItems',
      userId,
      id,
      includeItemTypes,
      sortBy,
      sortOrder,
      statusFilter,
      letterFilter,
      genreFilter,
      yearFilter,
      tagFilter,
      studioFilter,
      page,
    ],
    queryFn: () =>
      api.getItems(userId, {
        parentId: id,
        includeItemTypes,
        sortBy,
        sortOrder,
        filters: statusFilter || undefined,
        genres: genreFilter || undefined,
        years: yearFilter || undefined,
        tags: tagFilter || undefined,
        studios: studioFilter || undefined,
        nameLessThan: letterFilter === '#' ? 'a' : undefined,
        nameStartsWith:
          letterFilter && letterFilter !== '#' ? letterFilter.toLowerCase() : undefined,
        startIndex: page * PAGE_SIZE,
        limit: PAGE_SIZE,
      }),
    enabled: Boolean(id && (libraryInfo || libraryInfoFetched)),
    placeholderData: (prev) => prev,
  })

  const { items, total } = useMemo(() => {
    const rawItems = data?.Items || []
    if (!studioFilter) {
      return { items: rawItems, total: data?.TotalRecordCount ?? rawItems.length }
    }
    const targetStudio = studioFilter.trim().toLowerCase()
    const hasStudioMismatch = rawItems.some(
      (item) =>
        item.Studios &&
        item.Studios.length > 0 &&
        !item.Studios.some((s) => s.Name.trim().toLowerCase() === targetStudio),
    )
    if (!hasStudioMismatch) {
      return { items: rawItems, total: data?.TotalRecordCount ?? rawItems.length }
    }
    const matched = rawItems.filter((item) =>
      item.Studios?.some((s) => s.Name.trim().toLowerCase() === targetStudio),
    )
    return {
      items: matched,
      total: matched.length,
    }
  }, [data, studioFilter])

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const hasActiveFilters = Boolean(
    statusFilter || letterFilter || genreFilter || yearFilter || tagFilter || studioFilter,
  )

  const handleResetFilters = () => {
    setStatusFilter('')
    setLetterFilter('')
    setGenreFilter('')
    setYearFilter('')
    setTagFilter('')
    setStudioFilter('')
    setSearchParams({}, { replace: true })
    setPage(0)
  }

  const handleToggleSelectMode = () => {
    setSelectMode((prev) => {
      if (prev) {
        setSelectedIds(new Set())
      }
      return !prev
    })
  }

  const toggleSelectItem = (item: BaseItemDto) => {
    setSelectedIds((prev) => {
      const next = new Set(prev)
      if (next.has(item.Id)) {
        next.delete(item.Id)
      } else {
        next.add(item.Id)
      }
      return next
    })
  }

  const handleSelectAllPage = () => {
    setSelectedIds(new Set(items.map((i) => i.Id)))
  }

  const handleClearSelection = () => {
    setSelectedIds(new Set())
  }

  const handleBatchPlayed = async (markAsPlayed: boolean) => {
    const ids = Array.from(selectedIds)
    if (ids.length === 0) return
    setBatchBusy(true)
    setBatchFeedback(null)
    try {
      const results = await Promise.allSettled(
        ids.map((itemId) => api.togglePlayed(userId, itemId, !markAsPlayed)),
      )
      const succeeded = results.filter((r) => r.status === 'fulfilled').length
      const failed = results.length - succeeded

      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['libraryItems'] }),
        queryClient.invalidateQueries({ queryKey: ['resumeItems'] }),
        queryClient.invalidateQueries({ queryKey: ['nextUp'] }),
        queryClient.invalidateQueries({ queryKey: ['collections'] }),
      ])

      const actionLabel = markAsPlayed ? '已看' : '未看'
      if (failed === 0) {
        setBatchFeedback({
          type: 'success',
          message: `已将 ${succeeded} 项标记为${actionLabel}`,
        })
        setSelectedIds(new Set())
      } else {
        setBatchFeedback({
          type: 'error',
          message: `批量标记${actionLabel}完成：成功 ${succeeded} 项，失败 ${failed} 项`,
        })
      }
    } finally {
      setBatchBusy(false)
    }
  }

  return (
    <div className="space-y-6 px-4 pt-6 pb-24 lg:px-8">
      <FeedbackBanner
        message={batchFeedback?.message}
        type={batchFeedback?.type}
        onClose={() => setBatchFeedback(null)}
      />

      {/* 头部标题与多维筛选栏 */}
      <div className="flex flex-col gap-4 border-b border-white/8 pb-5 xl:flex-row xl:items-center xl:justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-white">
            {libraryInfo?.Name || t('nav.libraries')}
          </h1>
          <p className="mt-1 text-xs text-zinc-400">
            共 {total} 部媒体{isFetching && !isLoading ? ' · 正在更新...' : ''}
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          {/* 流派筛选 */}
          <div className="flex items-center gap-1.5 rounded-xl border border-white/10 bg-white/[0.04] px-3 py-1.5 text-xs">
            <Tags className="h-3.5 w-3.5 text-amber-400" />
            <select
              aria-label="按流派筛选"
              value={genreFilter}
              onChange={(e) => {
                setGenreFilter(e.target.value)
                updateUrlParam('genre', e.target.value)
              }}
              className="max-w-[120px] bg-transparent text-zinc-200 outline-none"
            >
              <option value="" className="bg-zinc-900 text-zinc-100">
                {locale === 'zh-CN' ? '全部流派' : `${t('common.all')} · ${t('library.filterGenre')}`}
              </option>
              {genreOptions.map((g) => (
                <option key={g} value={g} className="bg-zinc-900 text-zinc-100">
                  {g}
                </option>
              ))}
            </select>
          </div>

          {/* 年份筛选 */}
          <div className="flex items-center gap-1.5 rounded-xl border border-white/10 bg-white/[0.04] px-3 py-1.5 text-xs">
            <Calendar className="h-3.5 w-3.5 text-amber-400" />
            <select
              aria-label="按年份筛选"
              value={yearFilter}
              onChange={(e) => {
                setYearFilter(e.target.value)
                updateUrlParam('year', e.target.value)
              }}
              className="max-w-[100px] bg-transparent text-zinc-200 outline-none"
            >
              <option value="" className="bg-zinc-900 text-zinc-100">
                {locale === 'zh-CN' ? '全部年份' : `${t('common.all')} · ${t('library.filterYear')}`}
              </option>
              {yearOptions.map((y) => (
                <option key={y} value={y} className="bg-zinc-900 text-zinc-100">
                  {y}
                </option>
              ))}
            </select>
          </div>

          {/* 标签筛选 */}
          <div className="flex items-center gap-1.5 rounded-xl border border-white/10 bg-white/[0.04] px-3 py-1.5 text-xs">
            <Tag className="h-3.5 w-3.5 text-amber-400" />
            <select
              aria-label="按标签筛选"
              value={tagFilter}
              onChange={(e) => {
                setTagFilter(e.target.value)
                updateUrlParam('tag', e.target.value)
              }}
              className="max-w-[130px] bg-transparent text-zinc-200 outline-none"
            >
              <option value="" className="bg-zinc-900 text-zinc-100">
                {locale === 'zh-CN' ? '全部标签' : `${t('common.all')} · ${t('library.filterTag')}`}
              </option>
              {tagOptions.map((t) => (
                <option key={t} value={t} className="bg-zinc-900 text-zinc-100">
                  {t}
                </option>
              ))}
            </select>
          </div>

          {/* 状态过滤 */}
          <div className="flex items-center gap-1.5 rounded-xl border border-white/10 bg-white/[0.04] px-3 py-1.5 text-xs">
            <Filter className="h-3.5 w-3.5 text-amber-400" />
            <select
              aria-label="按观看状态筛选"
              value={statusFilter}
              onChange={(e) => {
                setStatusFilter(e.target.value)
                setPage(0)
              }}
              className="bg-transparent text-zinc-200 outline-none"
            >
              {FILTER_OPTIONS.map((opt) => (
                <option key={opt.value} value={opt.value} className="bg-zinc-900 text-zinc-100">
                  {locale === 'zh-CN' ? opt.zhLabel : t(opt.i18nKey)}
                </option>
              ))}
            </select>
          </div>

          {/* 排序字段 */}
          <div className="flex items-center gap-1.5 rounded-xl border border-white/10 bg-white/[0.04] px-3 py-1.5 text-xs">
            <select
              aria-label="排序方式"
              value={sortBy}
              onChange={(e) => {
                setSortBy(e.target.value)
                setPage(0)
              }}
              className="bg-transparent text-zinc-200 outline-none"
            >
              {SORT_OPTIONS.map((opt) => (
                <option key={opt.value} value={opt.value} className="bg-zinc-900 text-zinc-100">
                  {locale === 'zh-CN' ? opt.zhLabel : t(opt.i18nKey)}
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

          {/* 网格/列表视图切换 */}
          <div
            role="group"
            aria-label="视图切换"
            className="inline-flex items-center rounded-xl border border-white/10 bg-white/[0.04] p-0.5"
          >
            <button
              type="button"
              aria-label="网格视图"
              aria-pressed={viewMode === 'grid'}
              title="网格视图"
              onClick={() => handleSetViewMode('grid')}
              className={`rounded-lg p-1.5 transition-colors ${
                viewMode === 'grid'
                  ? 'bg-amber-500 text-zinc-950 shadow-sm'
                  : 'text-zinc-400 hover:text-zinc-100'
              }`}
            >
              <LayoutGrid className="h-3.5 w-3.5" />
            </button>
            <button
              type="button"
              aria-label="列表视图"
              aria-pressed={viewMode === 'list'}
              title="列表视图"
              onClick={() => handleSetViewMode('list')}
              className={`rounded-lg p-1.5 transition-colors ${
                viewMode === 'list'
                  ? 'bg-amber-500 text-zinc-950 shadow-sm'
                  : 'text-zinc-400 hover:text-zinc-100'
              }`}
            >
              <List className="h-3.5 w-3.5" />
            </button>
          </div>

          {/* 多选批量操作开关 */}
          <button
            type="button"
            aria-pressed={selectMode}
            onClick={handleToggleSelectMode}
            className={`flex items-center gap-1.5 rounded-xl border px-3 py-1.5 text-xs font-medium transition-colors ${
              selectMode
                ? 'border-amber-400/70 bg-amber-500 text-zinc-950 shadow-sm'
                : 'border-white/10 bg-white/[0.04] text-zinc-200 hover:bg-white/10'
            }`}
          >
            <CheckSquare className="h-3.5 w-3.5" />
            <span>{locale === 'zh-CN' ? '多选' : t('library.multiSelect')}</span>
          </button>

          {studioFilter && (
            <span className="inline-flex items-center gap-1.5 rounded-xl border border-amber-400/35 bg-amber-500/15 px-3 py-1.5 text-xs font-medium text-amber-200">
              <Building2 className="h-3.5 w-3.5 text-amber-400" />
              <span>
                {t('library.filterStudio')}：{studioFilter}
              </span>
              <button
                type="button"
                aria-label="清除制片厂筛选"
                onClick={() => {
                  setStudioFilter('')
                  updateUrlParam('studio', '')
                }}
                className="ml-0.5 rounded-full p-0.5 text-amber-300 transition-colors hover:bg-amber-400/20 hover:text-white"
              >
                <X className="h-3 w-3" />
              </button>
            </span>
          )}

          {hasActiveFilters && (
            <button
              type="button"
              onClick={handleResetFilters}
              className="flex items-center gap-1 rounded-xl border border-amber-500/30 bg-amber-500/10 px-2.5 py-1.5 text-xs font-medium text-amber-300 transition-colors hover:bg-amber-500/20"
            >
              <RotateCcw className="h-3 w-3" />
              <span>{locale === 'zh-CN' ? '重置' : t('library.clearFilters')}</span>
            </button>
          )}
        </div>
      </div>

      {/* 字母快速索引条 */}
      <div
        role="navigation"
        aria-label="字母索引"
        className="no-scrollbar flex items-center gap-1 overflow-x-auto pb-1"
      >
        {ALPHABET_LETTERS.map((letter) => {
          const value = letter === '全部' ? '' : letter
          const active = letterFilter === value
          return (
            <button
              key={letter}
              type="button"
              aria-pressed={active}
              onClick={() => {
                setLetterFilter(value)
                updateUrlParam('letter', value)
              }}
              className={`shrink-0 rounded-lg px-2 py-1 text-xs font-medium transition-colors ${
                active
                  ? 'bg-amber-500 text-zinc-950 shadow-sm'
                  : 'border border-white/8 bg-white/[0.03] text-zinc-400 hover:border-white/15 hover:text-zinc-100'
              }`}
            >
              {letter === '全部' ? t('library.allItems') : letter}
            </button>
          )
        })}
      </div>

      {/* 音乐库子标签：专辑 / 艺术家 / 曲目 */}
      {isMusicLibrary && (
        <div
          role="tablist"
          aria-label="音乐库分类"
          className="inline-flex items-center gap-1 rounded-xl border border-white/10 bg-zinc-950/80 p-1"
        >
          {MUSIC_SUB_TABS.map((tab) => {
            const Icon = tab.icon
            const active = musicSubTab === tab.id
            return (
              <button
                key={tab.id}
                type="button"
                role="tab"
                aria-selected={active}
                onClick={() => {
                  setMusicSubTab(tab.id)
                  setPage(0)
                }}
                className={`inline-flex items-center gap-1.5 rounded-lg px-3.5 py-2 text-xs font-medium transition-colors ${
                  active
                    ? 'bg-amber-500 text-zinc-950 shadow-sm'
                    : 'text-zinc-400 hover:bg-white/[0.05] hover:text-zinc-100'
                }`}
              >
                <Icon className="h-3.5 w-3.5" />
                <span>{t(tab.i18nKey)}</span>
              </button>
            )
          })}
        </div>
      )}

      {/* 海报墙网格 */}
      {isLoading ? (
        <div className="flex h-64 items-center justify-center">
          <Loader2 className="h-7 w-7 animate-spin text-amber-400" />
        </div>
      ) : isError ? (
        <QueryErrorState
          title="无法加载媒体库内容"
          error={error}
          onRetry={() => void refetch()}
          backTo="/"
        />
      ) : items.length === 0 ? (
        <div className="rounded-2xl border border-white/8 bg-white/[0.02] py-20 text-center text-sm text-zinc-500">
          {locale === 'zh-CN' ? '当前筛选条件下没有找到媒体项目' : t('library.emptyResult')}
        </div>
      ) : viewMode === 'list' ? (
        <div
          data-testid="library-list-view"
          className="divide-y divide-white/6 overflow-hidden rounded-2xl border border-white/8 bg-white/[0.02]"
        >
          {items.map((item) => (
            <LibraryListRow
              key={item.Id}
              item={item}
              selectable={selectMode}
              selected={selectedIds.has(item.Id)}
              onToggleSelect={toggleSelectItem}
            />
          ))}
        </div>
      ) : (
        <div
          data-testid="library-grid-view"
          className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7"
        >
          {items.map((item) => (
            <MediaCard
              key={item.Id}
              item={item}
              selectable={selectMode}
              selected={selectedIds.has(item.Id)}
              onToggleSelect={toggleSelectItem}
            />
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
            {t('common.prev')}
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
            {t('common.next')}
          </button>
        </div>
      )}

      {/* 悬浮底部多选批量操作栏 */}
      {selectMode && (
        <div
          role="region"
          aria-label="批量操作栏"
          data-testid="batch-action-bar"
          className={`fixed left-1/2 z-40 flex max-w-[calc(100vw-2rem)] -translate-x-1/2 flex-wrap items-center gap-2 rounded-2xl border border-amber-400/35 bg-zinc-950/92 px-4 py-2.5 text-xs text-zinc-100 shadow-[0_12px_40px_rgba(0,0,0,0.75)] backdrop-blur-xl transition-all ${
            currentTrack ? 'bottom-40 lg:bottom-24' : 'bottom-6'
          }`}
        >
          <span className="font-semibold text-amber-300">
            {t('library.selectedCount', { count: selectedIds.size })}
          </span>
          <span className="h-4 w-px bg-white/15" />

          <button
            type="button"
            onClick={handleSelectAllPage}
            disabled={items.length === 0 || batchBusy}
            className="rounded-lg border border-white/10 bg-white/[0.05] px-2.5 py-1.5 font-medium text-zinc-200 transition-colors hover:bg-white/10 disabled:opacity-40"
          >
            全选本页
          </button>
          <button
            type="button"
            onClick={handleClearSelection}
            disabled={selectedIds.size === 0 || batchBusy}
            className="rounded-lg border border-white/10 bg-white/[0.05] px-2.5 py-1.5 font-medium text-zinc-200 transition-colors hover:bg-white/10 disabled:opacity-40"
          >
            {locale === 'zh-CN' ? '清空' : t('common.clear')}
          </button>

          <span className="h-4 w-px bg-white/15" />

          <button
            type="button"
            onClick={() => void handleBatchPlayed(true)}
            disabled={selectedIds.size === 0 || batchBusy}
            className="inline-flex items-center gap-1 rounded-lg border border-emerald-500/35 bg-emerald-500/15 px-2.5 py-1.5 font-medium text-emerald-300 transition-colors hover:bg-emerald-500/25 disabled:opacity-40"
          >
            {batchBusy ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <Eye className="h-3.5 w-3.5" />
            )}
            <span>{t('home.markPlayed')}</span>
          </button>
          <button
            type="button"
            onClick={() => void handleBatchPlayed(false)}
            disabled={selectedIds.size === 0 || batchBusy}
            className="inline-flex items-center gap-1 rounded-lg border border-white/10 bg-white/[0.05] px-2.5 py-1.5 font-medium text-zinc-200 transition-colors hover:bg-white/10 disabled:opacity-40"
          >
            <EyeOff className="h-3.5 w-3.5" />
            <span>{t('home.markUnplayed')}</span>
          </button>
          <button
            type="button"
            onClick={() => setCollectionModalOpen(true)}
            disabled={selectedIds.size === 0 || batchBusy}
            className="inline-flex items-center gap-1 rounded-lg bg-amber-500 px-3 py-1.5 font-semibold text-zinc-950 transition-colors hover:bg-amber-400 disabled:opacity-40"
          >
            <FolderPlus className="h-3.5 w-3.5" />
            <span>{locale === 'zh-CN' ? '加入合集/片单' : t('detail.addToCollection')}</span>
          </button>

          <button
            type="button"
            aria-label={t('library.exitMultiSelect')}
            title={t('library.exitMultiSelect')}
            onClick={handleToggleSelectMode}
            className="ml-1 rounded-lg p-1.5 text-zinc-400 transition-colors hover:bg-white/10 hover:text-zinc-100"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
      )}

      <AddToCollectionModal
        open={collectionModalOpen}
        itemIds={Array.from(selectedIds)}
        itemLabel={`已选的 ${selectedIds.size} 个媒体项`}
        mediaType={isMusicLibrary ? 'Audio' : 'Video'}
        onClose={() => setCollectionModalOpen(false)}
        onSuccess={(msg) => {
          setBatchFeedback({ type: 'success', message: msg })
          setSelectedIds(new Set())
        }}
      />
    </div>
  )
}

function LibraryListRow({
  item,
  selectable = false,
  selected = false,
  onToggleSelect,
}: {
  item: BaseItemDto
  selectable?: boolean
  selected?: boolean
  onToggleSelect?: (item: BaseItemDto) => void
}) {
  const isMusicItem =
    item.Type === 'Audio' || item.Type === 'MusicAlbum' || item.Type === 'MusicArtist'
  const imgUrl = item.ImageTags?.Primary
    ? getImageUrl(item.Id, 'Primary', { maxWidth: 160, tag: item.ImageTags.Primary })
    : item.SeriesId
      ? getImageUrl(item.SeriesId, 'Primary', { maxWidth: 160 })
      : ''
  const fallbackUrl = item.ImageTags?.Primary
    ? getFallbackImageUrl(item.Id, 'Primary')
    : item.SeriesId
      ? getFallbackImageUrl(item.SeriesId, 'Primary')
      : null
  const [imgFailed, setImgFailed] = useState(false)

  useEffect(() => {
    setImgFailed(false)
  }, [imgUrl])

  const handleClick = (e: React.MouseEvent) => {
    if (selectable) {
      e.preventDefault()
      onToggleSelect?.(item)
    }
  }

  return (
    <Link
      to={`/item/${item.Id}`}
      onClick={handleClick}
      aria-pressed={selectable ? selected : undefined}
      className={`group flex items-center gap-4 px-4 py-3 transition-colors ${
        selected ? 'bg-amber-500/10' : 'hover:bg-white/[0.04]'
      }`}
    >
      {selectable && (
        <div
          aria-label={selected ? '已选中' : '未选中'}
          className={`flex h-5 w-5 shrink-0 items-center justify-center rounded-md border transition-colors ${
            selected
              ? 'border-amber-400 bg-amber-400 text-zinc-950'
              : 'border-white/30 bg-zinc-900 text-transparent'
          }`}
        >
          <Check className="h-3.5 w-3.5 stroke-[3]" />
        </div>
      )}
      <div className="h-14 w-10 shrink-0 overflow-hidden rounded-lg border border-white/10 bg-zinc-900">
        {imgUrl && !imgFailed ? (
          <img
            src={imgUrl}
            alt={item.Name}
            loading="lazy"
            onError={(e) => {
              const el = e.currentTarget
              if (fallbackUrl && !el.dataset.fallbackApplied && el.src !== fallbackUrl) {
                el.dataset.fallbackApplied = '1'
                el.src = fallbackUrl
                return
              }
              setImgFailed(true)
            }}
            className="h-full w-full object-cover"
          />
        ) : (
          <div className="flex h-full w-full items-center justify-center text-zinc-600">
            {isMusicItem ? <Music className="h-4 w-4" /> : <Film className="h-4 w-4" />}
          </div>
        )}
      </div>

      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <p className="truncate text-sm font-semibold text-zinc-100 transition-colors group-hover:text-amber-400">
            {item.Name}
          </p>
          {item.UserData?.Played && (
            <span
              title="已观看"
              className="inline-flex shrink-0 items-center gap-0.5 rounded-full bg-emerald-500/20 px-1.5 py-0.5 text-[10px] font-semibold text-emerald-300 ring-1 ring-emerald-500/35"
            >
              <Check className="h-2.5 w-2.5 stroke-[3]" />
              <span>已看</span>
            </span>
          )}
        </div>
        <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-zinc-400">
          {item.ProductionYear && <span>{item.ProductionYear}</span>}
          {item.CommunityRating ? (
            <span className="inline-flex items-center gap-1 text-amber-400">
              <Star className="h-3 w-3 fill-amber-400" />
              <span>{item.CommunityRating.toFixed(1)}</span>
            </span>
          ) : null}
          {item.RunTimeTicks ? (
            <span className="inline-flex items-center gap-1 text-zinc-400">
              <Clock className="h-3 w-3" />
              <span>{formatTicks(item.RunTimeTicks)}</span>
            </span>
          ) : null}
          {item.Genres && item.Genres.length > 0 && (
            <div className="flex flex-wrap items-center gap-1">
              {item.Genres.slice(0, 3).map((g) => (
                <span
                  key={g}
                  className="rounded bg-white/[0.06] px-1.5 py-0.5 text-[10px] text-zinc-300"
                >
                  {g}
                </span>
              ))}
            </div>
          )}
        </div>
      </div>
    </Link>
  )
}
