import { useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  ArrowDownWideNarrow,
  ArrowUpNarrowWide,
  Calendar,
  Disc3,
  Filter,
  Loader2,
  Mic2,
  Music,
  RotateCcw,
  Tag,
  Tags,
} from 'lucide-react'
import { useParams, useSearchParams } from 'react-router-dom'
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

type MusicSubTab = 'MusicAlbum' | 'MusicArtist' | 'Audio'

const MUSIC_SUB_TABS: Array<{ id: MusicSubTab; label: string; icon: typeof Disc3 }> = [
  { id: 'MusicAlbum', label: '专辑', icon: Disc3 },
  { id: 'MusicArtist', label: '艺术家', icon: Mic2 },
  { id: 'Audio', label: '曲目', icon: Music },
]

const PAGE_SIZE = 36

export function LibraryPage() {
  const { id } = useParams<{ id: string }>()
  const [searchParams, setSearchParams] = useSearchParams()
  const { auth } = useAuth()
  const userId = auth!.userId

  const [sortBy, setSortBy] = useState('DateCreated')
  const [sortOrder, setSortOrder] = useState<'Ascending' | 'Descending'>('Descending')
  const [statusFilter, setStatusFilter] = useState('')
  const [genreFilter, setGenreFilter] = useState(() => searchParams.get('genre') || '')
  const [yearFilter, setYearFilter] = useState(() => searchParams.get('year') || '')
  const [tagFilter, setTagFilter] = useState(() => searchParams.get('tag') || '')
  const [musicSubTab, setMusicSubTab] = useState<MusicSubTab>('MusicAlbum')
  const [page, setPage] = useState(0)

  // 响应 URL query 参数变化（例如从详情页点击流派/年份/标签跳转）
  useEffect(() => {
    setGenreFilter(searchParams.get('genre') || '')
    setYearFilter(searchParams.get('year') || '')
    setTagFilter(searchParams.get('tag') || '')
    setPage(0)
  }, [id, searchParams])

  useEffect(() => {
    setMusicSubTab('MusicAlbum')
  }, [id])

  const updateUrlParam = (key: 'genre' | 'year' | 'tag', value: string) => {
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

  const { data, isLoading, isFetching } = useQuery({
    queryKey: [
      'libraryItems',
      userId,
      id,
      includeItemTypes,
      sortBy,
      sortOrder,
      statusFilter,
      genreFilter,
      yearFilter,
      tagFilter,
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
        startIndex: page * PAGE_SIZE,
        limit: PAGE_SIZE,
      }),
    enabled: Boolean(id && (libraryInfo || libraryInfoFetched)),
    placeholderData: (prev) => prev,
  })

  const items = data?.Items || []
  const total = data?.TotalRecordCount || 0
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const hasActiveFilters = Boolean(statusFilter || genreFilter || yearFilter || tagFilter)

  const handleResetFilters = () => {
    setStatusFilter('')
    setGenreFilter('')
    setYearFilter('')
    setTagFilter('')
    setSearchParams({}, { replace: true })
    setPage(0)
  }

  return (
    <div className="space-y-6 px-4 pt-6 lg:px-8">
      {/* 头部标题与多维筛选栏 */}
      <div className="flex flex-col gap-4 border-b border-white/8 pb-5 xl:flex-row xl:items-center xl:justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-white">
            {libraryInfo?.Name || '媒体库'}
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
                全部流派
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
                全部年份
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
                全部标签
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
                  {opt.label}
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

          {hasActiveFilters && (
            <button
              type="button"
              onClick={handleResetFilters}
              className="flex items-center gap-1 rounded-xl border border-amber-500/30 bg-amber-500/10 px-2.5 py-1.5 text-xs font-medium text-amber-300 transition-colors hover:bg-amber-500/20"
            >
              <RotateCcw className="h-3 w-3" />
              <span>重置</span>
            </button>
          )}
        </div>
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
                <span>{tab.label}</span>
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
