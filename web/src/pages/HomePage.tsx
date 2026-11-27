import { useEffect, useRef, useState } from 'react'
import { useQueries, useQuery, useQueryClient } from '@tanstack/react-query'
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
import { QueryErrorState } from '../components/QueryErrorState'
import { useAuth } from '../context/AuthContext'
import { useI18n } from '../i18n/I18nContext'
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
  const { t } = useI18n()
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
              {t('home.viewAll')} →
            </Link>
          )}
        </div>
        <div className="hidden items-center gap-1.5 sm:flex">
          <button
            type="button"
            onClick={() => scroll('left')}
            aria-label={t('home.scrollLeft', { title })}
            className="rounded-lg border border-white/10 bg-white/[0.04] p-1.5 text-zinc-300 transition-colors hover:bg-white/10"
          >
            <ChevronLeft className="h-4 w-4" />
          </button>
          <button
            type="button"
            onClick={() => scroll('right')}
            aria-label={t('home.scrollRight', { title })}
            className="rounded-lg border border-white/10 bg-white/[0.04] p-1.5 text-zinc-300 transition-colors hover:bg-white/10"
          >
            <ChevronRight className="h-4 w-4" />
          </button>
        </div>
      </div>

      <div className="px-4 lg:px-8">
        <div
          ref={scrollRef}
          className="no-scrollbar -mx-1 flex gap-4 overflow-x-auto px-1 pb-4 pt-2"
        >
          {children}
        </div>
      </div>
    </section>
  )
}

export function HomePage() {
  const { auth } = useAuth()
  const { locale, t } = useI18n()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const userId = auth!.userId
  const [heroIdx, setHeroIdx] = useState(0)

  const invalidateHomeProgress = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ['resumeItems'] }),
      queryClient.invalidateQueries({ queryKey: ['nextUp'] }),
      queryClient.invalidateQueries({ queryKey: ['latestItems'] }),
    ])
  }

  const handleMarkPlayed = async (item: BaseItemDto) => {
    try {
      await api.togglePlayed(userId, item.Id, false)
      await invalidateHomeProgress()
    } catch {
      // ignore error in quick action
    }
  }

  const handleHideFromResume = async (item: BaseItemDto) => {
    try {
      await api.hideFromResume(userId, item.Id, true)
      await invalidateHomeProgress()
    } catch {
      // ignore error in quick action
    }
  }

  const {
    data: viewsData,
    isError: viewsError,
    error: viewsErr,
    refetch: refetchViews,
  } = useQuery({
    queryKey: ['userViews', userId],
    queryFn: () => api.getUserViews(userId),
  })

  const {
    data: resumeData,
    refetch: refetchResume,
  } = useQuery({
    queryKey: ['resumeItems', userId],
    queryFn: () => api.getResumeItems(userId, 12),
  })

  const {
    data: nextUpData,
    refetch: refetchNextUp,
  } = useQuery({
    queryKey: ['nextUp', userId],
    queryFn: () => api.getNextUp(userId, { limit: 12 }),
  })

  const {
    data: heroData,
    isError: heroError,
    error: heroErr,
    refetch: refetchHero,
  } = useQuery({
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
  const nextUpItems = nextUpData?.Items || []
  const heroItems = heroData?.Items || []
  const hasPrimaryError = (viewsError || heroError) && views.length === 0 && heroItems.length === 0
  const primaryError = viewsErr || heroErr

  const handleRetryHome = () => {
    void refetchViews()
    void refetchHero()
    void refetchResume()
    void refetchNextUp()
  }

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

  if (hasPrimaryError) {
    return (
      <QueryErrorState
        title={t('home.loadErrorTitle')}
        error={primaryError}
        onRetry={handleRetryHome}
      />
    )
  }

  return (
    <div className="space-y-10">
      {(viewsError || heroError) && (
        <div className="px-4 pt-6 lg:px-8">
          <QueryErrorState
            compact
            title={t('home.partialLoadErrorTitle')}
            error={primaryError}
            onRetry={handleRetryHome}
          />
        </div>
      )}
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
              className="animate-hero-fade h-full w-full object-cover object-center"
            />
          ) : (
            <div className="h-full w-full bg-gradient-to-br from-zinc-900 via-zinc-950 to-black" />
          )}

          {/* 电影感多层次暗色渐变遮罩 */}
          <div className="absolute inset-0 bg-gradient-to-r from-[#09090d] via-[#09090d]/70 to-transparent" />
          <div className="absolute inset-0 bg-gradient-to-t from-[#09090d] via-transparent to-black/30" />

          <div className="absolute inset-0 flex flex-col justify-end px-5 pb-10 lg:px-10 lg:pb-12">
            <div key={activeHero.Id} className="animate-page-enter max-w-2xl space-y-4">
              <div className="flex flex-wrap items-center gap-2 text-xs font-medium">
                <span className="rounded-md bg-amber-500/20 px-2 py-0.5 text-amber-300 ring-1 ring-amber-400/30">
                  {t('home.featuredBadge')}
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
                  className="flex items-center gap-2 rounded-xl bg-amber-500 px-5 py-2.5 text-sm font-semibold text-zinc-950 shadow-lg shadow-amber-500/30 transition-all duration-200 hover:-translate-y-0.5 hover:bg-amber-400 active:scale-[0.97]"
                >
                  <Play className="h-4 w-4 fill-current" />
                  <span>
                    {activeHero.Type === 'Series'
                      ? locale === 'zh-CN'
                        ? '开始观看'
                        : t('home.playNow')
                      : t('home.playNow')}
                  </span>
                </button>
                <Link
                  to={`/item/${activeHero.Id}`}
                  className="flex items-center gap-2 rounded-xl border border-white/15 bg-white/10 px-5 py-2.5 text-sm font-medium text-white backdrop-blur-md transition-all duration-200 hover:-translate-y-0.5 hover:bg-white/15 active:scale-[0.97]"
                >
                  <Info className="h-4 w-4" />
                  <span>{t('home.details')}</span>
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
                    aria-label={t('home.switchToSlide', { name: it.Name })}
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
            <h1 className="text-2xl font-bold text-white">
              {locale === 'zh-CN' ? '欢迎回到 Ambra 影院' : t('home.emptyTitle')}
            </h1>
            <p className="mt-2 text-sm text-zinc-400">
              {locale === 'zh-CN'
                ? '当前媒体库暂无影片，可在管理后台配置并扫描你的本地影视目录。'
                : t('home.emptyDesc')}
            </p>
          </div>
        </div>
      )}

      {/* 我的媒体 */}
      {views.length > 0 && (
        <section className="space-y-4 px-4 lg:px-8">
          <h2 className="text-lg font-semibold tracking-tight text-zinc-100">{t('home.myMedia')}</h2>
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
                  className="group relative flex aspect-[16/9] items-end overflow-hidden rounded-2xl border border-white/10 bg-zinc-900/80 p-4 transition-all duration-300 ease-out hover:-translate-y-1 hover:border-amber-400/65 hover:shadow-[0_14px_34px_rgba(0,0,0,0.55),0_0_24px_rgba(245,158,11,0.16)]"
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
                    <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-amber-500/20 text-amber-400 backdrop-blur-md transition-transform duration-300 group-hover:scale-110 group-hover:bg-amber-500/30">
                      <Icon className="h-4 w-4" />
                    </div>
                    <div>
                      <p className="text-sm font-semibold text-zinc-100 transition-colors group-hover:text-amber-300">
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
        <HorizontalRow title={t('home.continueWatching')}>
          {resumeItems.map((item) => (
            <MediaCard
              key={item.Id}
              item={item}
              variant="landscape"
              showProgress
              onMarkPlayed={handleMarkPlayed}
              onHideFromResume={handleHideFromResume}
              className="w-64 shrink-0 sm:w-72"
            />
          ))}
        </HorizontalRow>
      )}

      {/* 接下来播放 (Next Up) */}
      {nextUpItems.length > 0 && (
        <HorizontalRow title={t('home.nextUp')}>
          {nextUpItems.map((item) => (
            <MediaCard
              key={item.Id}
              item={item}
              variant="landscape"
              showProgress
              onMarkPlayed={handleMarkPlayed}
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
            title={`${t('home.latestAdded')} · ${view.Name}`}
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
