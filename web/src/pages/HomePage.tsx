import { useEffect, useRef, useState } from 'react'
import { useQueries, useQuery } from '@tanstack/react-query'
import {
  ChevronLeft,
  ChevronRight,
  Clapperboard,
  Film,
  Info,
  Music,
  Play,
  Star,
  Tv,
} from 'lucide-react'
import { Link, useNavigate } from 'react-router-dom'
import { api, getBackdropUrl, getFallbackImageUrl, getImageUrl } from '../api/client'
import type { BaseItemDto } from '../api/types'
import { MediaCard } from '../components/MediaCard'
import { useAuth } from '../context/AuthContext'
import { formatTicks } from '../player/subtitleUtils'

function HorizontalRow({
  title,
  moreLink,
  children,
}: {
  title: string
  moreLink?: string
  children: React.ReactNode
}) {
  const scrollRef = useRef<HTMLDivElement>(null)

  const scroll = (dir: 'left' | 'right') => {
    const el = scrollRef.current
    if (!el) return
    const delta = el.clientWidth * 0.78
    el.scrollBy({ left: dir === 'left' ? -delta : delta, behavior: 'smooth' })
  }

  return (
    <section className="space-y-4">
      <div className="flex items-center justify-between px-4 lg:px-8">
        <div className="flex items-center gap-3">
          <h2 className="text-lg font-semibold tracking-tight text-zinc-100">{title}</h2>
          {moreLink && (
            <Link
              to={moreLink}
              className="text-xs font-medium text-amber-400/90 transition-colors hover:text-amber-300"
            >
              查看全部 →
            </Link>
          )}
        </div>
        <div className="hidden items-center gap-1.5 sm:flex">
          <button
            type="button"
            onClick={() => scroll('left')}
            className="rounded-lg border border-white/10 bg-white/[0.04] p-1.5 text-zinc-300 transition-colors hover:bg-white/10"
          >
            <ChevronLeft className="h-4 w-4" />
          </button>
          <button
            type="button"
            onClick={() => scroll('right')}
            className="rounded-lg border border-white/10 bg-white/[0.04] p-1.5 text-zinc-300 transition-colors hover:bg-white/10"
          >
            <ChevronRight className="h-4 w-4" />
          </button>
        </div>
      </div>

      <div
        ref={scrollRef}
        className="no-scrollbar flex gap-4 overflow-x-auto px-4 pb-2 lg:px-8"
      >
        {children}
      </div>
    </section>
  )
}

export function HomePage() {
  const { auth } = useAuth()
  const navigate = useNavigate()
  const userId = auth!.userId
  const [heroIdx, setHeroIdx] = useState(0)

  const { data: viewsData } = useQuery({
    queryKey: ['userViews', userId],
    queryFn: () => api.getUserViews(userId),
  })

  const { data: resumeData } = useQuery({
    queryKey: ['resumeItems', userId],
    queryFn: () => api.getResumeItems(userId, 12),
  })

  const { data: heroData } = useQuery({
    queryKey: ['heroItems', userId],
    queryFn: () =>
      api.getItems(userId, {
        includeItemTypes: 'Movie,Series',
        sortBy: 'CommunityRating,DateCreated',
        sortOrder: 'Descending',
        limit: 6,
      }),
  })

  const views = viewsData?.Items || []
  const resumeItems = resumeData?.Items || []
  const heroItems = heroData?.Items || []

  const latestQueries = useQueries({
    queries: views.slice(0, 6).map((view) => ({
      queryKey: ['latestItems', userId, view.Id],
      queryFn: () => api.getLatestItems(userId, view.Id, 16),
      staleTime: 30_000,
    })),
  })

  // 自动轮播 Hero Banner
  useEffect(() => {
    if (heroItems.length <= 1) return
    const timer = window.setInterval(() => {
      setHeroIdx((prev) => (prev + 1) % heroItems.length)
    }, 8000)
    return () => window.clearInterval(timer)
  }, [heroItems.length])

  const activeHero: BaseItemDto | undefined = heroItems[heroIdx % Math.max(1, heroItems.length)]
  const heroBackdrop = activeHero ? getBackdropUrl(activeHero, 1920) : null
  const heroFallbackBackdrop = activeHero
    ? getFallbackImageUrl(activeHero.Id, 'Backdrop')
    : null
  const [heroImgFailed, setHeroImgFailed] = useState(false)

  useEffect(() => {
    setHeroImgFailed(false)
  }, [activeHero?.Id, heroBackdrop])

  return (
    <div className="space-y-10">
      {/* 全宽轮播 Hero Banner */}
      {activeHero ? (
        <section className="relative h-[54vh] min-h-[380px] max-h-[560px] w-full overflow-hidden">
          {heroBackdrop && !heroImgFailed ? (
            <img
              key={activeHero.Id}
              src={heroBackdrop}
              alt={activeHero.Name}
              onError={(e) => {
                const el = e.currentTarget
                if (
                  heroFallbackBackdrop &&
                  !el.dataset.fallbackApplied &&
                  el.src !== heroFallbackBackdrop
                ) {
                  el.dataset.fallbackApplied = '1'
                  el.src = heroFallbackBackdrop
                  return
                }
                setHeroImgFailed(true)
              }}
              className="h-full w-full object-cover object-center transition-opacity duration-700"
            />
          ) : (
            <div className="h-full w-full bg-gradient-to-br from-zinc-900 via-zinc-950 to-black" />
          )}

          {/* 电影感多层次暗色渐变遮罩 */}
          <div className="absolute inset-0 bg-gradient-to-r from-[#09090d] via-[#09090d]/70 to-transparent" />
          <div className="absolute inset-0 bg-gradient-to-t from-[#09090d] via-transparent to-black/30" />

          <div className="absolute inset-0 flex flex-col justify-end px-5 pb-10 lg:px-10 lg:pb-12">
            <div className="max-w-2xl space-y-4">
              <div className="flex flex-wrap items-center gap-2 text-xs font-medium">
                <span className="rounded-md bg-amber-500/20 px-2 py-0.5 text-amber-300 ring-1 ring-amber-400/30">
                  精选推荐
                </span>
                {activeHero.CommunityRating ? (
                  <span className="flex items-center gap-1 rounded-md bg-black/50 px-2 py-0.5 text-amber-400 backdrop-blur-md">
                    <Star className="h-3 w-3 fill-amber-400" />
                    {activeHero.CommunityRating.toFixed(1)}
                  </span>
                ) : null}
                {activeHero.ProductionYear && (
                  <span className="text-zinc-300">{activeHero.ProductionYear}</span>
                )}
                {activeHero.RunTimeTicks && (
                  <span className="text-zinc-400">{formatTicks(activeHero.RunTimeTicks)}</span>
                )}
                {activeHero.Genres?.slice(0, 3).map((g) => (
                  <span key={g} className="rounded-md bg-white/8 px-2 py-0.5 text-zinc-300">
                    {g}
                  </span>
                ))}
              </div>

              <h1 className="text-3xl font-bold tracking-tight text-white drop-shadow-md sm:text-4xl lg:text-5xl">
                {activeHero.Name}
              </h1>

              {activeHero.Overview && (
                <p className="line-clamp-3 text-sm leading-relaxed text-zinc-300/90 sm:text-base">
                  {activeHero.Overview}
                </p>
              )}

              <div className="flex flex-wrap items-center gap-3 pt-1">
                <button
                  type="button"
                  onClick={() =>
                    navigate(
                      activeHero.Type === 'Series'
                        ? `/item/${activeHero.Id}`
                        : `/play/${activeHero.Id}`,
                    )
                  }
                  className="flex items-center gap-2 rounded-xl bg-amber-500 px-5 py-2.5 text-sm font-semibold text-zinc-950 shadow-lg shadow-amber-500/30 transition-all hover:bg-amber-400"
                >
                  <Play className="h-4 w-4 fill-current" />
                  <span>{activeHero.Type === 'Series' ? '开始观看' : '立即播放'}</span>
                </button>
                <Link
                  to={`/item/${activeHero.Id}`}
                  className="flex items-center gap-2 rounded-xl border border-white/15 bg-white/10 px-5 py-2.5 text-sm font-medium text-white backdrop-blur-md transition-all hover:bg-white/15"
                >
                  <Info className="h-4 w-4" />
                  <span>查看详情</span>
                </Link>
              </div>
            </div>

            {/* 轮播指示器 */}
            {heroItems.length > 1 && (
              <div className="mt-6 flex items-center gap-2">
                {heroItems.map((it, idx) => (
                  <button
                    key={it.Id}
                    type="button"
                    aria-label={`切换到 ${it.Name}`}
                    onClick={() => setHeroIdx(idx)}
                    className={`h-1.5 rounded-full transition-all ${
                      idx === heroIdx
                        ? 'w-8 bg-amber-400'
                        : 'w-2 bg-white/30 hover:bg-white/60'
                    }`}
                  />
                ))}
              </div>
            )}
          </div>
        </section>
      ) : (
        <div className="px-4 pt-6 lg:px-8">
          <div className="rounded-2xl border border-white/8 bg-gradient-to-r from-amber-500/10 via-zinc-900/60 to-zinc-950 p-8">
            <h1 className="text-2xl font-bold text-white">欢迎回到 Ambra 影院</h1>
            <p className="mt-2 text-sm text-zinc-400">
              当前媒体库暂无影片，可在管理后台配置并扫描你的本地影视目录。
            </p>
          </div>
        </div>
      )}

      {/* 我的媒体 */}
      {views.length > 0 && (
        <section className="space-y-4 px-4 lg:px-8">
          <h2 className="text-lg font-semibold tracking-tight text-zinc-100">我的媒体</h2>
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 xl:grid-cols-5">
            {views.map((view) => {
              const ct = (view.CollectionType || '').toLowerCase()
              const Icon =
                ct === 'movies'
                  ? Film
                  : ct === 'tvshows'
                    ? Tv
                    : ct === 'music'
                      ? Music
                      : Clapperboard
              const thumb = view.ImageTags?.Primary
                ? getImageUrl(view.Id, 'Primary', { maxWidth: 480, tag: view.ImageTags.Primary })
                : null

              return (
                <Link
                  key={view.Id}
                  to={`/library/${view.Id}`}
                  className="group relative flex aspect-[16/9] items-end overflow-hidden rounded-2xl border border-white/10 bg-zinc-900/80 p-4 transition-all hover:border-amber-400/50 hover:shadow-xl hover:shadow-black/50"
                >
                  {thumb ? (
                    <img
                      src={thumb}
                      alt={view.Name}
                      className="absolute inset-0 h-full w-full object-cover opacity-55 transition-transform duration-500 group-hover:scale-105 group-hover:opacity-70"
                    />
                  ) : (
                    <div className="absolute inset-0 bg-gradient-to-br from-amber-500/15 via-zinc-900 to-zinc-950" />
                  )}
                  <div className="absolute inset-0 bg-gradient-to-t from-black/85 via-black/30 to-transparent" />
                  <div className="relative z-10 flex items-center gap-2.5">
                    <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-amber-500/20 text-amber-400 backdrop-blur-md">
                      <Icon className="h-4 w-4" />
                    </div>
                    <div>
                      <p className="text-sm font-semibold text-zinc-100 group-hover:text-amber-300">
                        {view.Name}
                      </p>
                    </div>
                  </div>
                </Link>
              )
            })}
          </div>
        </section>
      )}

      {/* 继续观看 */}
      {resumeItems.length > 0 && (
        <HorizontalRow title="继续观看">
          {resumeItems.map((item) => (
            <MediaCard
              key={item.Id}
              item={item}
              variant="landscape"
              showProgress
              className="w-64 shrink-0 sm:w-72"
            />
          ))}
        </HorizontalRow>
      )}

      {/* 各媒体库最新添加 */}
      {views.slice(0, 6).map((view, idx) => {
        const items = latestQueries[idx]?.data || []
        if (items.length === 0) return null
        return (
          <HorizontalRow
            key={view.Id}
            title={`最新添加 · ${view.Name}`}
            moreLink={`/library/${view.Id}`}
          >
            {items.map((item) => (
              <MediaCard
                key={item.Id}
                item={item}
                variant="poster"
                className="w-36 shrink-0 sm:w-40 md:w-44"
              />
            ))}
          </HorizontalRow>
        )
      })}
    </div>
  )
}
