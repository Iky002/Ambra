import { useEffect, useMemo, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  ArrowDownWideNarrow,
  ArrowUpNarrowWide,
  Building2,
  Calendar,
  Check,
  CheckSquare,
  ChevronDown,
  ChevronUp,
  Clock,
  Disc3,
  Eye,
  EyeOff,
  Film,
  Filter,
  FolderPlus,
  Heart,
  LayoutGrid,
  List,
  Loader2,
  Mic2,
  Music,
  RotateCcw,
  SlidersHorizontal,
  Sparkles,
  Star,
  Tag,
  Tags,
  X,
} from 'lucide-react'
import { Link, useParams, useSearchParams } from 'react-router-dom'
import { api, getFallbackImageUrl, getImageUrl } from '../api/client'
import type { BaseItemDto, NameIdPair } from '../api/types'
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
const VIRTUALIZE_GRID_THRESHOLD = 120
const ESTIMATED_GRID_ROW_HEIGHT = 320
const VIRTUAL_OVERSCAN_ROWS = 3
const VIEW_MODE_STORAGE_KEY = 'ambra_library_view_mode'
const ALPHABET_LETTERS = ['全部', '#', ...'ABCDEFGHIJKLMNOPQRSTUVWXYZ'.split('')] as const

function extractFilterString(entry: string | NameIdPair | number | null | undefined): string {
  if (entry === null || entry === undefined) return ''
  if (typeof entry === 'number') return Number.isFinite(entry) ? String(entry) : ''
  if (typeof entry === 'string') return entry.trim()
  if (typeof entry === 'object' && typeof entry.Name === 'string') return entry.Name.trim()
  return ''
}

function getResponsiveGridCols(viewportWidth: number): number {
  if (!Number.isFinite(viewportWidth) || viewportWidth <= 0) return 6
  if (viewportWidth < 640) return 2
  if (viewportWidth < 768) return 3
  if (viewportWidth < 1024) return 4
  if (viewportWidth < 1280) return 5
  if (viewportWidth < 1536) return 6
  return 7
}

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
  const [advancedOpen, setAdvancedOpen] = useState(false)
  const [resolutionFilter, setResolutionFilter] = useState<'' | '4K' | 'HD'>('')
  const [hdrOnly, setHdrOnly] = useState(false)
  const [videoCodecFilter, setVideoCodecFilter] = useState('')
  const [minRatingFilter, setMinRatingFilter] = useState('')
  const [likedOnly, setLikedOnly] = useState(false)
  const [pageSize, setPageSize] = useState<number>(PAGE_SIZE)
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
    setResolutionFilter('')
    setHdrOnly(false)
    setVideoCodecFilter('')
    setMinRatingFilter('')
    setLikedOnly(false)
    setSelectMode(false)
    setSelectedIds(new Set())
    setBatchFeedback(null)
  }, [id])

  // 切换任何筛选条件或音乐子标签时清空已选集合，避免隐藏项被误包含进批量操作
  useEffect(() => {
    setSelectedIds(new Set())
  }, [
    musicSubTab,
    statusFilter,
    genreFilter,
    yearFilter,
    tagFilter,
    studioFilter,
    letterFilter,
    resolutionFilter,
    hdrOnly,
    videoCodecFilter,
    minRatingFilter,
    likedOnly,
  ])

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

  const isMusicLibrary = (libraryInfo?.CollectionType || '').toLowerCase() === 'music'
  const includeItemTypes = isMusicLibrary
    ? musicSubTab
    : 'Movie,Series,MusicAlbum,Video'

  const { data: filters2Data } = useQuery({
    queryKey: ['itemFilters2', userId, id, includeItemTypes],
    queryFn: () =>
      api
        .getItemFilters2({ userId, parentId: id, includeItemTypes })
        .catch(() => ({ Genres: [], Tags: [], Years: [], Codecs: [] })),
    enabled: Boolean(id && (libraryInfo || libraryInfoFetched)),
    staleTime: 60_000,
  })

  const genreOptions = useMemo(() => {
    const merged = new Set<string>()
    for (const g of genresData?.Items || []) {
      const name = extractFilterString(g)
      if (name) merged.add(name)
    }
    for (const g of (filters2Data?.Genres || []) as Array<string | NameIdPair>) {
      const name = extractFilterString(g)
      if (name) merged.add(name)
    }
    const list = Array.from(merged)
    if (genreFilter && !list.includes(genreFilter)) {
      return [genreFilter, ...list]
    }
    return list
  }, [genresData, filters2Data?.Genres, genreFilter])

  const yearOptions = useMemo(() => {
    const merged = new Set<string>()
    for (const y of yearsData?.Items || []) {
      const name = extractFilterString(y)
      if (name) merged.add(name)
    }
    for (const y of filters2Data?.Years || []) {
      const name = extractFilterString(y)
      if (name) merged.add(name)
    }
    const list = Array.from(merged).sort((a, b) => Number(b) - Number(a))
    if (yearFilter && !list.includes(yearFilter)) {
      return [yearFilter, ...list]
    }
    return list
  }, [yearsData, filters2Data?.Years, yearFilter])

  const tagOptions = useMemo(() => {
    const merged = new Set<string>()
    for (const tItem of tagsData?.Items || []) {
      const name = extractFilterString(tItem)
      if (name) merged.add(name)
    }
    for (const tItem of (filters2Data?.Tags || []) as Array<string | NameIdPair>) {
      const name = extractFilterString(tItem)
      if (name) merged.add(name)
    }
    const list = Array.from(merged)
    if (tagFilter && !list.includes(tagFilter)) {
      return [tagFilter, ...list]
    }
    return list
  }, [tagsData, filters2Data?.Tags, tagFilter])

  const codecOptions = useMemo(() => {
    const defaults = ['hevc', 'h264', 'av1', 'vp9']
    const merged = new Set<string>(defaults)
    for (const c of filters2Data?.Codecs || []) {
      const normalized = extractFilterString(c).toLowerCase()
      if (normalized) merged.add(normalized)
    }
    if (videoCodecFilter && !merged.has(videoCodecFilter.toLowerCase())) {
      merged.add(videoCodecFilter.toLowerCase())
    }
    return Array.from(merged)
  }, [filters2Data?.Codecs, videoCodecFilter])

  const combinedFilters = useMemo(() => {
    const parts = [statusFilter, likedOnly ? 'IsLiked' : ''].filter(Boolean)
    return parts.length > 0 ? parts.join(',') : undefined
  }, [statusFilter, likedOnly])

  const parsedMinRating = useMemo(() => {
    if (!minRatingFilter) return undefined
    const n = Number(minRatingFilter)
    return Number.isFinite(n) && n > 0 ? n : undefined
  }, [minRatingFilter])

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
      resolutionFilter,
      hdrOnly,
      videoCodecFilter,
      minRatingFilter,
      likedOnly,
      pageSize,
      page,
    ],
    queryFn: () =>
      api.getItems(userId, {
        parentId: id,
        includeItemTypes,
        sortBy,
        sortOrder,
        filters: combinedFilters,
        genres: genreFilter || undefined,
        years: yearFilter || undefined,
        tags: tagFilter || undefined,
        studios: studioFilter || undefined,
        is4K: resolutionFilter === '4K' ? true : undefined,
        isHD: resolutionFilter === 'HD' ? true : undefined,
        isHDR: hdrOnly ? true : undefined,
        videoCodecs: videoCodecFilter || undefined,
        minCommunityRating: parsedMinRating,
        isLiked: likedOnly ? true : undefined,
        nameLessThan: letterFilter === '#' ? 'a' : undefined,
        nameStartsWith:
          letterFilter && letterFilter !== '#' ? letterFilter.toLowerCase() : undefined,
        startIndex: page * pageSize,
        limit: pageSize,
      }),
    enabled: Boolean(id && (libraryInfo || libraryInfoFetched)),
    placeholderData: (prev) => prev,
  })

  const items = data?.Items || []
  const total = data?.TotalRecordCount ?? items.length

  const totalPages = Math.max(1, Math.ceil(total / pageSize))
  const advancedActiveCount = useMemo(() => {
    let count = 0
    if (resolutionFilter) count += 1
    if (hdrOnly) count += 1
    if (videoCodecFilter) count += 1
    if (minRatingFilter) count += 1
    if (likedOnly) count += 1
    return count
  }, [resolutionFilter, hdrOnly, videoCodecFilter, minRatingFilter, likedOnly])
  const hasActiveFilters = Boolean(
    statusFilter ||
      letterFilter ||
      genreFilter ||
      yearFilter ||
      tagFilter ||
      studioFilter ||
      advancedActiveCount > 0,
  )

  const handleResetFilters = () => {
    setStatusFilter('')
    setLetterFilter('')
    setGenreFilter('')
    setYearFilter('')
    setTagFilter('')
    setStudioFilter('')
    setResolutionFilter('')
    setHdrOnly(false)
    setVideoCodecFilter('')
    setMinRatingFilter('')
    setLikedOnly(false)
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

          {/* 高级筛选折叠开关 */}
          <button
            type="button"
            data-testid="toggle-advanced-filters"
            aria-expanded={advancedOpen}
            onClick={() => setAdvancedOpen((prev) => !prev)}
            className={`flex items-center gap-1.5 rounded-xl border px-3 py-1.5 text-xs font-medium transition-colors ${
              advancedOpen || advancedActiveCount > 0
                ? 'border-amber-400/50 bg-amber-500/15 text-amber-300'
                : 'border-white/10 bg-white/[0.04] text-zinc-200 hover:bg-white/10'
            }`}
          >
            <SlidersHorizontal className="h-3.5 w-3.5 text-amber-400" />
            <span>高级筛选</span>
            {advancedActiveCount > 0 && (
              <span className="rounded-full bg-amber-500 px-1.5 py-0.2 text-[10px] font-bold text-zinc-950">
                {advancedActiveCount}
              </span>
            )}
            {advancedOpen ? (
              <ChevronUp className="h-3.5 w-3.5" />
            ) : (
              <ChevronDown className="h-3.5 w-3.5" />
            )}
          </button>

          {/* 每页容量切换（支持大画幅虚拟滚动浏览） */}
          <div className="flex items-center gap-1.5 rounded-xl border border-white/10 bg-white/[0.04] px-2.5 py-1.5 text-xs">
            <select
              aria-label="每页数量"
              value={pageSize}
              onChange={(e) => {
                setPageSize(Number(e.target.value) || PAGE_SIZE)
                setPage(0)
              }}
              className="bg-transparent text-zinc-200 outline-none"
            >
              <option value={36} className="bg-zinc-900 text-zinc-100">
                36 / 页
              </option>
              <option value={150} className="bg-zinc-900 text-zinc-100">
                150 / 页
              </option>
              <option value={300} className="bg-zinc-900 text-zinc-100">
                300 / 页
              </option>
            </select>
          </div>

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

      {/* 可折叠高级多维筛选栏（对接 /emby/Items/Filters2） */}
      {advancedOpen && (
        <div
          data-testid="advanced-filters-panel"
          className="grid grid-cols-1 gap-3 rounded-2xl border border-amber-500/20 bg-zinc-950/75 p-4 backdrop-blur-md sm:grid-cols-2 lg:grid-cols-6"
        >
          <label className="space-y-1 text-xs">
            <span className="block text-[11px] font-medium text-zinc-400">流派</span>
            <select
              aria-label="高级筛选流派"
              value={genreFilter}
              onChange={(e) => {
                setGenreFilter(e.target.value)
                updateUrlParam('genre', e.target.value)
              }}
              className="w-full rounded-xl border border-white/10 bg-zinc-900 px-3 py-2 text-xs text-zinc-100 outline-none focus:border-amber-400/50"
            >
              <option value="">全部流派</option>
              {genreOptions.map((g) => (
                <option key={g} value={g}>
                  {g}
                </option>
              ))}
            </select>
          </label>

          <label className="space-y-1 text-xs">
            <span className="block text-[11px] font-medium text-zinc-400">年份</span>
            <select
              aria-label="高级筛选年份"
              value={yearFilter}
              onChange={(e) => {
                setYearFilter(e.target.value)
                updateUrlParam('year', e.target.value)
              }}
              className="w-full rounded-xl border border-white/10 bg-zinc-900 px-3 py-2 text-xs text-zinc-100 outline-none focus:border-amber-400/50"
            >
              <option value="">全部年份</option>
              {yearOptions.map((y) => (
                <option key={y} value={y}>
                  {y}
                </option>
              ))}
            </select>
          </label>

          <label className="space-y-1 text-xs">
            <span className="block text-[11px] font-medium text-zinc-400">分辨率规格</span>
            <select
              aria-label="按分辨率筛选"
              value={resolutionFilter}
              onChange={(e) => {
                setResolutionFilter((e.target.value as '' | '4K' | 'HD') || '')
                setPage(0)
              }}
              className="w-full rounded-xl border border-white/10 bg-zinc-900 px-3 py-2 text-xs text-zinc-100 outline-none focus:border-amber-400/50"
            >
              <option value="">全部分辨率</option>
              <option value="4K">4K 超高清 (UHD)</option>
              <option value="HD">高清 (HD 1080p/720p)</option>
            </select>
          </label>

          <label className="space-y-1 text-xs">
            <span className="block text-[11px] font-medium text-zinc-400">视频编码</span>
            <select
              aria-label="按视频编码筛选"
              value={videoCodecFilter}
              onChange={(e) => {
                setVideoCodecFilter(e.target.value)
                setPage(0)
              }}
              className="w-full rounded-xl border border-white/10 bg-zinc-900 px-3 py-2 text-xs text-zinc-100 outline-none focus:border-amber-400/50"
            >
              <option value="">全部编码</option>
              {codecOptions.map((codec) => (
                <option key={codec} value={codec}>
                  {codec.toUpperCase()}
                </option>
              ))}
            </select>
          </label>

          <label className="space-y-1 text-xs">
            <span className="block text-[11px] font-medium text-zinc-400">最低评分</span>
            <select
              aria-label="按最低评分筛选"
              value={minRatingFilter}
              onChange={(e) => {
                setMinRatingFilter(e.target.value)
                setPage(0)
              }}
              className="w-full rounded-xl border border-white/10 bg-zinc-900 px-3 py-2 text-xs text-zinc-100 outline-none focus:border-amber-400/50"
            >
              <option value="">不限评分</option>
              <option value="9">≥ 9.0 分</option>
              <option value="8">≥ 8.0 分</option>
              <option value="7">≥ 7.0 分</option>
              <option value="6">≥ 6.0 分</option>
            </select>
          </label>

          <div className="flex items-end gap-2">
            <button
              type="button"
              data-testid="filter-hdr-toggle"
              aria-pressed={hdrOnly}
              onClick={() => {
                setHdrOnly((v) => !v)
                setPage(0)
              }}
              className={`flex flex-1 items-center justify-center gap-1.5 rounded-xl border px-3 py-2 text-xs font-semibold transition-colors ${
                hdrOnly
                  ? 'border-amber-400/60 bg-amber-500/20 text-amber-300'
                  : 'border-white/10 bg-zinc-900 text-zinc-300 hover:bg-white/8'
              }`}
            >
              <Sparkles className="h-3.5 w-3.5" />
              <span>HDR</span>
            </button>

            <button
              type="button"
              data-testid="filter-liked-toggle"
              aria-pressed={likedOnly}
              onClick={() => {
                setLikedOnly((v) => !v)
                setPage(0)
              }}
              className={`flex flex-1 items-center justify-center gap-1.5 rounded-xl border px-3 py-2 text-xs font-semibold transition-colors ${
                likedOnly
                  ? 'border-rose-400/60 bg-rose-500/20 text-rose-300'
                  : 'border-white/10 bg-zinc-900 text-zinc-300 hover:bg-white/8'
              }`}
            >
              <Heart className={`h-3.5 w-3.5 ${likedOnly ? 'fill-current' : ''}`} />
              <span>已赞</span>
            </button>
          </div>
        </div>
      )}

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
      ) : items.length > VIRTUALIZE_GRID_THRESHOLD ? (
        <VirtualizedLibraryGrid
          items={items}
          selectable={selectMode}
          selectedIds={selectedIds}
          onToggleSelect={toggleSelectItem}
        />
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

function VirtualizedLibraryGrid({
  items,
  selectable,
  selectedIds,
  onToggleSelect,
}: {
  items: BaseItemDto[]
  selectable: boolean
  selectedIds: Set<string>
  onToggleSelect: (item: BaseItemDto) => void
}) {
  const containerRef = useRef<HTMLDivElement>(null)
  const [cols, setCols] = useState(() =>
    typeof window !== 'undefined' ? getResponsiveGridCols(window.innerWidth) : 6,
  )
  const [scrollState, setScrollState] = useState({ offsetTop: 0, viewportHeight: 900 })

  useEffect(() => {
    const updateViewport = () => {
      if (typeof window === 'undefined') return
      const nextCols = getResponsiveGridCols(window.innerWidth)
      setCols(nextCols)
      const rectTop = containerRef.current?.getBoundingClientRect().top ?? 0
      const offsetTop = Math.max(0, -rectTop)
      const viewportHeight = window.innerHeight > 0 ? window.innerHeight : 900
      setScrollState((prev) =>
        prev.offsetTop === offsetTop && prev.viewportHeight === viewportHeight
          ? prev
          : { offsetTop, viewportHeight },
      )
    }

    updateViewport()
    window.addEventListener('scroll', updateViewport, { passive: true })
    window.addEventListener('resize', updateViewport, { passive: true })
    return () => {
      window.removeEventListener('scroll', updateViewport)
      window.removeEventListener('resize', updateViewport)
    }
  }, [items.length])

  const totalRows = Math.ceil(items.length / cols)
  const startRow = Math.max(
    0,
    Math.floor(scrollState.offsetTop / ESTIMATED_GRID_ROW_HEIGHT) - VIRTUAL_OVERSCAN_ROWS,
  )
  const visibleRowCount =
    Math.ceil(scrollState.viewportHeight / ESTIMATED_GRID_ROW_HEIGHT) + VIRTUAL_OVERSCAN_ROWS * 2
  const endRow = Math.min(totalRows, startRow + visibleRowCount)

  const startIdx = startRow * cols
  const endIdx = Math.min(items.length, endRow * cols)
  const visibleItems = items.slice(startIdx, endIdx)

  const topSpacerHeight = startRow * ESTIMATED_GRID_ROW_HEIGHT
  const bottomSpacerHeight = Math.max(0, (totalRows - endRow) * ESTIMATED_GRID_ROW_HEIGHT)

  return (
    <div ref={containerRef} data-testid="library-virtual-scroll">
      {topSpacerHeight > 0 && (
        <div
          data-testid="virtual-grid-top-spacer"
          style={{ height: `${topSpacerHeight}px` }}
          aria-hidden="true"
        />
      )}
      <div
        data-testid="library-grid-view"
        className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7"
      >
        {visibleItems.map((item) => (
          <MediaCard
            key={item.Id}
            item={item}
            selectable={selectable}
            selected={selectedIds.has(item.Id)}
            onToggleSelect={onToggleSelect}
          />
        ))}
      </div>
      {bottomSpacerHeight > 0 && (
        <div
          data-testid="virtual-grid-bottom-spacer"
          style={{ height: `${bottomSpacerHeight}px` }}
          aria-hidden="true"
        />
      )}
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
