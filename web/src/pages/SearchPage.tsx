import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Loader2, Search } from 'lucide-react'
import { useSearchParams } from 'react-router-dom'
import { api } from '../api/client'
import type { BaseItemDto } from '../api/types'
import { MediaCard } from '../components/MediaCard'
import { QueryErrorState } from '../components/QueryErrorState'
import { useAuth } from '../context/AuthContext'
import { useI18n } from '../i18n/I18nContext'

type SearchTypeFilter = 'all' | 'Movie' | 'Series' | 'Music' | 'Person' | 'BoxSet'

const TYPE_PILLS: Array<{
  id: SearchTypeFilter
  label: string
  labelKey?: string
  includeItemTypes: string
}> = [
  {
    id: 'all',
    label: '全部',
    labelKey: 'common.all',
    includeItemTypes: 'Movie,Series,Episode,BoxSet,Person,MusicArtist,MusicAlbum,Audio,Playlist',
  },
  { id: 'Movie', label: '电影', includeItemTypes: 'Movie' },
  { id: 'Series', label: '剧集', includeItemTypes: 'Series,Episode' },
  { id: 'Music', label: '音乐', includeItemTypes: 'MusicAlbum,MusicArtist,Audio' },
  { id: 'Person', label: '演职员', labelKey: 'detail.castAndCrew', includeItemTypes: 'Person' },
  { id: 'BoxSet', label: '合集', labelKey: 'collections.boxSets', includeItemTypes: 'BoxSet' },
]

function resolveSearchItemLink(item: BaseItemDto): string | undefined {
  if (item.Type === 'Person') {
    return `/person/${encodeURIComponent(item.Id)}?name=${encodeURIComponent(item.Name)}`
  }
  if (item.Type === 'BoxSet' || item.Type === 'Playlist') {
    return `/collections/${encodeURIComponent(item.Id)}`
  }
  return undefined
}

export function SearchPage() {
  const { auth } = useAuth()
  const { locale, t } = useI18n()
  const [searchParams, setSearchParams] = useSearchParams()
  const urlQuery = searchParams.get('q') || ''
  const [input, setInput] = useState(urlQuery)
  const [debounced, setDebounced] = useState(urlQuery.trim())
  const [typeFilter, setTypeFilter] = useState<SearchTypeFilter>('all')

  // 同步外部 URL ?q= 变化（例如在 /search 页通过 ⌘K 触发新搜索）
  useEffect(() => {
    setInput(urlQuery)
    setDebounced(urlQuery.trim())
  }, [urlQuery])

  useEffect(() => {
    const t = window.setTimeout(() => {
      const trimmed = input.trim()
      setDebounced(trimmed)
      if (trimmed !== urlQuery) {
        if (trimmed) {
          setSearchParams({ q: trimmed }, { replace: true })
        } else {
          setSearchParams({}, { replace: true })
        }
      }
    }, 250)
    return () => window.clearTimeout(t)
  }, [input, urlQuery, setSearchParams])

  const activePill = TYPE_PILLS.find((p) => p.id === typeFilter) || TYPE_PILLS[0]

  const { data, isLoading, isError, error, refetch } = useQuery({
    queryKey: ['searchItems', auth?.userId, debounced, activePill.includeItemTypes],
    queryFn: () =>
      api.getItems(auth!.userId, {
        searchTerm: debounced,
        includeItemTypes: activePill.includeItemTypes,
        limit: 48,
      }),
    enabled: debounced.length > 0,
  })

  const items = data?.Items || []

  return (
    <div className="space-y-6 px-4 pt-6 lg:px-8">
      <div className="max-w-2xl space-y-4">
        <h1 className="text-2xl font-bold text-white">{t('search.title')}</h1>
        <div className="relative">
          <Search className="pointer-events-none absolute left-4 top-1/2 h-5 w-5 -translate-y-1/2 text-amber-400" />
          <input
            type="text"
            autoFocus
            value={input}
            onChange={(e) => setInput(e.target.value)}
            placeholder={
              locale === 'zh-CN'
                ? '输入片名、原名、演职员、合集或乱序关键词...'
                : t('search.placeholder')
            }
            className="w-full rounded-2xl border border-white/12 bg-white/[0.05] py-3.5 pl-12 pr-4 text-base text-zinc-100 placeholder-zinc-500 outline-none transition-all focus:border-amber-400/60 focus:bg-white/[0.08]"
          />
        </div>

        {/* 类型筛选胶囊 */}
        <div className="flex flex-wrap items-center gap-2">
          {TYPE_PILLS.map((pill) => {
            const active = pill.id === typeFilter
            return (
              <button
                key={pill.id}
                type="button"
                onClick={() => setTypeFilter(pill.id)}
                className={`rounded-full px-3.5 py-1.5 text-xs font-medium transition-colors ${
                  active
                    ? 'bg-amber-500 text-zinc-950 shadow-sm'
                    : 'border border-white/10 bg-white/[0.04] text-zinc-300 hover:bg-white/10 hover:text-white'
                }`}
              >
                {pill.labelKey ? t(pill.labelKey) : pill.label}
              </button>
            )
          })}
        </div>
      </div>

      {!debounced ? (
        <div className="rounded-2xl border border-white/6 bg-white/[0.02] py-16 text-center text-sm text-zinc-500">
          支持中英文及多关键词乱序搜索，随时按 <kbd className="rounded bg-white/10 px-1.5 py-0.5 text-zinc-300">⌘K</kbd> 快速唤起全局搜索
        </div>
      ) : isLoading ? (
        <div className="flex h-48 items-center justify-center">
          <Loader2 className="h-7 w-7 animate-spin text-amber-400" />
        </div>
      ) : isError ? (
        <QueryErrorState
          compact
          title="搜索请求失败"
          error={error}
          onRetry={() => void refetch()}
        />
      ) : items.length === 0 ? (
        <div className="rounded-2xl border border-white/6 bg-white/[0.02] py-16 text-center text-sm text-zinc-500">
          {t('search.noResults')}
        </div>
      ) : (
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
          {items.map((item) => (
            <MediaCard key={item.Id} item={item} linkTo={resolveSearchItemLink(item)} />
          ))}
        </div>
      )}
    </div>
  )
}
