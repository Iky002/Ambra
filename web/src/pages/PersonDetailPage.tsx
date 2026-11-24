import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ArrowLeft, Film, Loader2, Tv, User } from 'lucide-react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { api, getFallbackImageUrl, getImageUrl } from '../api/client'
import { MediaCard } from '../components/MediaCard'
import { useAuth } from '../context/AuthContext'

type MediaFilter = 'ALL' | 'Movie' | 'Series'

export function PersonDetailPage() {
  const { id } = useParams<{ id: string }>()
  const [searchParams] = useSearchParams()
  const navigate = useNavigate()
  const { auth } = useAuth()
  const userId = auth!.userId

  const decodedId = id ? decodeURIComponent(id) : ''
  const nameFromQuery = searchParams.get('name') || ''
  const [mediaFilter, setMediaFilter] = useState<MediaFilter>('ALL')
  const [avatarFailed, setAvatarFailed] = useState(false)

  const { data: personItem, isLoading: personLoading } = useQuery({
    queryKey: ['personItem', userId, decodedId],
    queryFn: async () => {
      try {
        return await api.getItem(userId, decodedId)
      } catch {
        return null
      }
    },
    enabled: Boolean(decodedId),
    retry: false,
  })

  const displayName = personItem?.Name || nameFromQuery || decodedId

  const { data: worksData, isLoading: worksLoading } = useQuery({
    queryKey: ['personWorks', userId, decodedId, displayName],
    queryFn: async () => {
      const idLooksLikeName = Boolean(nameFromQuery && decodedId === nameFromQuery)
      if (!idLooksLikeName) {
        const byId = await api.getItems(userId, {
          personIds: decodedId,
          includeItemTypes: 'Movie,Series',
          recursive: true,
          sortBy: 'PremiereDate',
          sortOrder: 'Descending',
          limit: 100,
        })
        if (byId.Items.length > 0 || !displayName) {
          return byId
        }
      }
      if (displayName) {
        return api.getItems(userId, {
          person: displayName,
          includeItemTypes: 'Movie,Series',
          recursive: true,
          sortBy: 'PremiereDate',
          sortOrder: 'Descending',
          limit: 100,
        })
      }
      return { Items: [], TotalRecordCount: 0 }
    },
    enabled: Boolean(decodedId),
  })

  const allWorks = worksData?.Items || []

  const filteredWorks = useMemo(() => {
    if (mediaFilter === 'ALL') return allWorks
    return allWorks.filter((w) => w.Type === mediaFilter)
  }, [allWorks, mediaFilter])

  const movieCount = allWorks.filter((w) => w.Type === 'Movie').length
  const seriesCount = allWorks.filter((w) => w.Type === 'Series').length

  // 从参演作品的 People 数组中提取角色/身份信息作为补充
  const rolesSummary = useMemo(() => {
    const types = new Set<string>()
    for (const work of allWorks) {
      for (const p of work.People || []) {
        if (p.Id === decodedId || p.Name === displayName) {
          if (p.Type) types.add(p.Type === 'Director' ? '导演' : p.Type === 'Actor' ? '演员' : p.Type)
        }
      }
    }
    return Array.from(types)
  }, [allWorks, decodedId, displayName])

  const avatarUrl =
    !avatarFailed && decodedId
      ? getImageUrl(decodedId, 'Primary', {
          maxWidth: 320,
          tag: personItem?.ImageTags?.Primary,
        })
      : null
  const avatarFallback = decodedId ? getFallbackImageUrl(decodedId, 'Primary') : null

  const initials = (displayName || '?').slice(0, 1).toUpperCase()

  return (
    <div className="mx-auto max-w-7xl space-y-8 px-4 pt-6 lg:px-8">
      {/* 返回按钮 */}
      <div>
        <button
          type="button"
          onClick={() => navigate(-1)}
          className="inline-flex items-center gap-1.5 rounded-xl border border-white/10 bg-white/[0.04] px-3 py-1.5 text-xs font-medium text-zinc-300 transition-colors hover:border-amber-400/40 hover:bg-white/10 hover:text-zinc-100"
        >
          <ArrowLeft className="h-3.5 w-3.5" />
          <span>返回上一页</span>
        </button>
      </div>

      {/* 演职员资料头部 */}
      <section className="relative overflow-hidden rounded-3xl border border-white/10 bg-gradient-to-br from-zinc-900/90 via-[#0e0e16] to-zinc-950 p-6 sm:p-8">
        <div className="pointer-events-none absolute -right-16 -top-16 h-60 w-60 rounded-full bg-amber-500/10 blur-3xl" />

        <div className="relative z-10 flex flex-col items-center gap-6 sm:flex-row sm:items-start">
          {/* 头像 */}
          <div className="flex h-32 w-32 shrink-0 items-center justify-center overflow-hidden rounded-full border-2 border-amber-400/35 bg-zinc-900 shadow-2xl shadow-black/60 sm:h-40 sm:w-40">
            {avatarUrl ? (
              <img
                src={avatarUrl}
                alt={displayName}
                onError={(e) => {
                  const el = e.currentTarget
                  if (avatarFallback && !el.dataset.fallbackApplied && el.src !== avatarFallback) {
                    el.dataset.fallbackApplied = '1'
                    el.src = avatarFallback
                    return
                  }
                  setAvatarFailed(true)
                }}
                className="h-full w-full object-cover object-top"
              />
            ) : (
              <div className="flex h-full w-full flex-col items-center justify-center bg-gradient-to-br from-amber-500/20 to-zinc-900 text-amber-300">
                {displayName ? (
                  <span className="text-4xl font-bold">{initials}</span>
                ) : (
                  <User className="h-12 w-12 opacity-60" />
                )}
              </div>
            )}
          </div>

          {/* 姓名与简介 */}
          <div className="min-w-0 flex-1 space-y-3 text-center sm:text-left">
            <div className="flex flex-wrap items-center justify-center gap-2 sm:justify-start">
              <h1 className="text-2xl font-bold tracking-tight text-white sm:text-3xl">
                {displayName}
              </h1>
              {rolesSummary.length > 0 ? (
                rolesSummary.map((r) => (
                  <span
                    key={r}
                    className="rounded-full border border-amber-400/35 bg-amber-500/15 px-2.5 py-0.5 text-xs font-medium text-amber-300"
                  >
                    {r}
                  </span>
                ))
              ) : (
                <span className="rounded-full border border-white/12 bg-white/[0.05] px-2.5 py-0.5 text-xs font-medium text-zinc-300">
                  演职员
                </span>
              )}
            </div>

            <div className="flex flex-wrap items-center justify-center gap-3 text-xs text-zinc-400 sm:justify-start">
              <span className="inline-flex items-center gap-1.5">
                <Film className="h-3.5 w-3.5 text-amber-400" />
                电影 {movieCount} 部
              </span>
              <span className="inline-flex items-center gap-1.5">
                <Tv className="h-3.5 w-3.5 text-amber-400" />
                剧集 {seriesCount} 部
              </span>
            </div>

            {personLoading ? (
              <p className="text-xs text-zinc-500">正在加载演职员资料...</p>
            ) : personItem?.Overview ? (
              <p className="max-w-3xl text-sm leading-relaxed text-zinc-300">
                {personItem.Overview}
              </p>
            ) : (
              <p className="max-w-2xl text-xs leading-relaxed text-zinc-500">
                下方展示当前媒体库中与「{displayName}」相关的全部电影与剧集作品。
              </p>
            )}
          </div>
        </div>
      </section>

      {/* 参演作品筛选与网格 */}
      <section className="space-y-5">
        <div className="flex flex-col gap-3 border-b border-white/8 pb-4 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h2 className="text-lg font-semibold text-zinc-100">库内相关作品</h2>
            <p className="mt-0.5 text-xs text-zinc-400">共 {filteredWorks.length} 部媒体</p>
          </div>

          <div className="inline-flex items-center gap-1 rounded-xl border border-white/10 bg-zinc-950/80 p-1">
            {(
              [
                { value: 'ALL', label: `全部 (${allWorks.length})` },
                { value: 'Movie', label: `电影 (${movieCount})` },
                { value: 'Series', label: `剧集 (${seriesCount})` },
              ] as const
            ).map((opt) => (
              <button
                key={opt.value}
                type="button"
                onClick={() => setMediaFilter(opt.value)}
                className={`rounded-lg px-3 py-1.5 text-xs font-medium transition-colors ${
                  mediaFilter === opt.value
                    ? 'bg-amber-500 text-zinc-950'
                    : 'text-zinc-400 hover:bg-white/[0.05] hover:text-zinc-100'
                }`}
              >
                {opt.label}
              </button>
            ))}
          </div>
        </div>

        {worksLoading ? (
          <div className="flex h-52 items-center justify-center">
            <Loader2 className="h-7 w-7 animate-spin text-amber-400" />
          </div>
        ) : filteredWorks.length === 0 ? (
          <div className="rounded-2xl border border-dashed border-white/10 bg-white/[0.02] py-16 text-center text-sm text-zinc-500">
            媒体库中暂未找到该分类下与「{displayName}」相关的作品
          </div>
        ) : (
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
            {filteredWorks.map((work) => (
              <MediaCard key={work.Id} item={work} />
            ))}
          </div>
        )}
      </section>
    </div>
  )
}
