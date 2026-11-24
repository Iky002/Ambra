import { useEffect, useState } from 'react'
import { Check, Film, Flame, Play, Star } from 'lucide-react'
import { Link, useNavigate } from 'react-router-dom'
import { getFallbackImageUrl, getImageUrl } from '../api/client'
import type { BaseItemDto } from '../api/types'

interface MediaCardProps {
  item: BaseItemDto
  variant?: 'poster' | 'landscape'
  showProgress?: boolean
  className?: string
  selectable?: boolean
  selected?: boolean
  onToggleSelect?: (item: BaseItemDto) => void
  linkTo?: string
}

export function MediaCard({
  item,
  variant = 'poster',
  showProgress = false,
  className = '',
  selectable = false,
  selected = false,
  onToggleSelect,
  linkTo,
}: MediaCardProps) {
  const navigate = useNavigate()
  const isLandscape = variant === 'landscape'
  const targetUrl = linkTo ?? `/item/${item.Id}`

  let imgUrl = ''
  let fallbackUrl: string | null = null
  if (isLandscape) {
    if (item.BackdropImageTags?.length) {
      imgUrl = getImageUrl(item.Id, 'Backdrop', { maxWidth: 640, tag: item.BackdropImageTags[0] })
      fallbackUrl = getFallbackImageUrl(item.Id, 'Backdrop')
    } else if (item.ImageTags?.Thumb) {
      imgUrl = getImageUrl(item.Id, 'Thumb', { maxWidth: 640, tag: item.ImageTags.Thumb })
      fallbackUrl = getFallbackImageUrl(item.Id, 'Thumb')
    } else if (item.ImageTags?.Primary) {
      imgUrl = getImageUrl(item.Id, 'Primary', { maxWidth: 640, tag: item.ImageTags.Primary })
      fallbackUrl = getFallbackImageUrl(item.Id, 'Primary')
    } else if (item.SeriesId) {
      imgUrl = getImageUrl(item.SeriesId, 'Backdrop', { maxWidth: 640 })
      fallbackUrl = getFallbackImageUrl(item.SeriesId, 'Backdrop')
    }
  } else {
    if (item.ImageTags?.Primary) {
      imgUrl = getImageUrl(item.Id, 'Primary', { maxWidth: 420, tag: item.ImageTags.Primary })
      fallbackUrl = getFallbackImageUrl(item.Id, 'Primary')
    } else if (item.SeriesId) {
      imgUrl = getImageUrl(item.SeriesId, 'Primary', { maxWidth: 420 })
      fallbackUrl = getFallbackImageUrl(item.SeriesId, 'Primary')
    }
  }

  const [imgFailed, setImgFailed] = useState(false)
  useEffect(() => {
    setImgFailed(false)
  }, [imgUrl])

  const progress =
    item.UserData?.PlayedPercentage ??
    (item.UserData?.PlaybackPositionTicks && item.RunTimeTicks
      ? Math.min(100, (item.UserData.PlaybackPositionTicks / item.RunTimeTicks) * 100)
      : 0)

  const subtitle =
    item.Type === 'Episode' && item.SeriesName
      ? `${item.SeriesName}${item.ParentIndexNumber != null ? ` · S${item.ParentIndexNumber}` : ''}${item.IndexNumber != null ? `E${item.IndexNumber}` : ''}`
      : item.ProductionYear
        ? String(item.ProductionYear)
        : item.Type === 'Series'
          ? '剧集'
          : ''

  const canDirectPlay =
    !selectable && (item.Type === 'Movie' || item.Type === 'Episode' || item.MediaType === 'Video')
  const directPlayUrl =
    showProgress && item.UserData?.PlaybackPositionTicks && item.UserData.PlaybackPositionTicks > 0
      ? `/play/${item.Id}?startTicks=${item.UserData.PlaybackPositionTicks}`
      : `/play/${item.Id}`
  const hasDoubanTop250 = Boolean(item.Tags?.some((t) => t === '豆瓣Top250' || t === '豆瓣 Top250'))
  const hasImdbTop250 = Boolean(item.Tags?.some((t) => t === 'IMDb Top250' || t === 'IMDb Top 250'))
  const hasTrending = Boolean(item.Tags?.some((t) => t.toLowerCase() === 'trending'))
  const hasProgressBar = (showProgress || progress > 0) && progress > 0

  const handleCardClick = (e: React.MouseEvent) => {
    if (selectable) {
      e.preventDefault()
      onToggleSelect?.(item)
    }
  }

  return (
    <div className={`group relative flex flex-col ${className}`}>
      <Link
        to={targetUrl}
        onClick={handleCardClick}
        aria-pressed={selectable ? selected : undefined}
        className={`relative block overflow-hidden rounded-xl border bg-zinc-900/90 transition-all duration-300 ease-out ${
          selected
            ? 'border-amber-400 ring-2 ring-amber-400/90 shadow-[0_0_24px_rgba(245,158,11,0.32)]'
            : 'border-white/10 group-hover:-translate-y-1 group-hover:border-amber-400/75 group-hover:shadow-[0_14px_34px_rgba(0,0,0,0.65),0_0_20px_rgba(245,158,11,0.16)]'
        } ${isLandscape ? 'aspect-video' : 'aspect-[2/3]'}`}
      >
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
            className="h-full w-full object-cover transition-transform duration-500 ease-out group-hover:scale-[1.06]"
          />
        ) : (
          <div className="flex h-full w-full flex-col items-center justify-center bg-gradient-to-br from-zinc-800 to-zinc-950 text-zinc-600">
            <Film className="h-10 w-10 opacity-40" />
          </div>
        )}

        {/* 暗色悬停遮罩与快速播放按钮 */}
        <div className="absolute inset-0 bg-gradient-to-t from-black/85 via-black/15 to-transparent opacity-0 transition-opacity duration-300 group-hover:opacity-100" />

        {canDirectPlay && (
          <button
            type="button"
            title="立即播放"
            onClick={(e) => {
              e.preventDefault()
              e.stopPropagation()
              navigate(directPlayUrl)
            }}
            className="absolute left-1/2 top-1/2 flex h-12 w-12 -translate-x-1/2 -translate-y-1/2 scale-75 items-center justify-center rounded-full bg-amber-500 text-zinc-950 opacity-0 shadow-lg shadow-amber-500/35 transition-all duration-300 ease-out hover:bg-amber-400 group-hover:scale-100 group-hover:opacity-100 active:scale-95"
          >
            <Play className="ml-0.5 h-5 w-5 fill-current" />
          </button>
        )}

        {/* 左上角评分 */}
        {item.CommunityRating ? (
          <div className="absolute left-2 top-2 flex items-center gap-1 rounded-md bg-black/70 px-1.5 py-0.5 text-[11px] font-semibold text-amber-400 backdrop-blur-md">
            <Star className="h-3 w-3 fill-amber-400" />
            <span>{item.CommunityRating.toFixed(1)}</span>
          </div>
        ) : null}

        {/* 右上角选择复选框或已看/未看数量 */}
        {selectable ? (
          <div
            aria-label={selected ? '已选中' : '未选中'}
            className={`absolute right-2 top-2 flex h-7 w-7 items-center justify-center rounded-full border-2 shadow-md transition-all ${
              selected
                ? 'border-amber-300 bg-amber-400 text-zinc-950 scale-105'
                : 'border-white/70 bg-black/55 text-transparent backdrop-blur-sm'
            }`}
          >
            <Check className="h-4 w-4 stroke-[3]" />
          </div>
        ) : item.UserData?.Played ? (
          <div
            title="已观看"
            className="absolute right-2 top-2 flex h-6 w-6 items-center justify-center rounded-full bg-emerald-500/90 text-zinc-950 shadow-md"
          >
            <Check className="h-3.5 w-3.5 stroke-[3]" />
          </div>
        ) : item.UserData?.UnplayedItemCount && item.UserData.UnplayedItemCount > 0 ? (
          <div className="absolute right-2 top-2 rounded-full bg-amber-500/90 px-2 py-0.5 text-[11px] font-bold text-zinc-950 shadow-md">
            {item.UserData.UnplayedItemCount}
          </div>
        ) : null}

        {/* 左下角榜单与热门徽章（热门 / 豆瓣 Top250 / IMDb Top250） */}
        {(hasTrending || hasDoubanTop250 || hasImdbTop250) && (
          <div
            className={`pointer-events-none absolute left-2 right-2 flex flex-wrap items-center gap-1 ${
              hasProgressBar ? 'bottom-3' : 'bottom-2'
            }`}
          >
            {hasTrending && (
              <span className="inline-flex items-center gap-0.5 rounded-md border border-orange-400/55 bg-gradient-to-r from-orange-950/90 via-amber-900/85 to-orange-950/90 px-1.5 py-0.5 text-[10px] font-semibold tracking-wide text-orange-200 shadow-[0_2px_8px_rgba(0,0,0,0.45)] backdrop-blur-md">
                <Flame className="h-2.5 w-2.5 text-orange-400" />
                <span>热门</span>
              </span>
            )}
            {hasDoubanTop250 && (
              <span className="inline-flex items-center rounded-md border border-emerald-400/50 bg-gradient-to-r from-emerald-950/90 via-emerald-900/85 to-teal-950/90 px-1.5 py-0.5 text-[10px] font-semibold tracking-wide text-emerald-200 shadow-[0_2px_8px_rgba(0,0,0,0.45)] backdrop-blur-md">
                豆瓣 Top250
              </span>
            )}
            {hasImdbTop250 && (
              <span className="inline-flex items-center rounded-md border border-amber-400/55 bg-gradient-to-r from-amber-950/90 via-yellow-900/80 to-amber-950/90 px-1.5 py-0.5 text-[10px] font-semibold tracking-wide text-amber-200 shadow-[0_2px_8px_rgba(0,0,0,0.45)] backdrop-blur-md">
                IMDb Top250
              </span>
            )}
          </div>
        )}

        {/* 底部播放进度条 */}
        {hasProgressBar && (
          <div className="absolute inset-x-0 bottom-0 h-1.5 bg-black/60">
            <div
              className="h-full bg-gradient-to-r from-amber-500 to-orange-400 transition-all"
              style={{ width: `${Math.max(3, Math.min(100, progress))}%` }}
            />
          </div>
        )}
      </Link>

      <div className="mt-2.5 px-0.5">
        <Link
          to={targetUrl}
          onClick={handleCardClick}
          className="block truncate text-sm font-medium text-zinc-100 transition-colors group-hover:text-amber-400"
          title={item.Name}
        >
          {item.Name}
        </Link>
        {subtitle && <p className="mt-0.5 truncate text-xs text-zinc-400">{subtitle}</p>}
      </div>
    </div>
  )
}
