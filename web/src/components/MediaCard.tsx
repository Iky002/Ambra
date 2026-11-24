import { Check, Film, Play, Star } from 'lucide-react'
import { Link, useNavigate } from 'react-router-dom'
import { getImageUrl } from '../api/client'
import type { BaseItemDto } from '../api/types'

interface MediaCardProps {
  item: BaseItemDto
  variant?: 'poster' | 'landscape'
  showProgress?: boolean
  className?: string
}

export function MediaCard({
  item,
  variant = 'poster',
  showProgress = false,
  className = '',
}: MediaCardProps) {
  const navigate = useNavigate()
  const isLandscape = variant === 'landscape'

  let imgUrl = ''
  if (isLandscape) {
    if (item.BackdropImageTags?.length) {
      imgUrl = getImageUrl(item.Id, 'Backdrop', { maxWidth: 640, tag: item.BackdropImageTags[0] })
    } else if (item.ImageTags?.Thumb) {
      imgUrl = getImageUrl(item.Id, 'Thumb', { maxWidth: 640, tag: item.ImageTags.Thumb })
    } else if (item.ImageTags?.Primary) {
      imgUrl = getImageUrl(item.Id, 'Primary', { maxWidth: 640, tag: item.ImageTags.Primary })
    } else if (item.SeriesId) {
      imgUrl = getImageUrl(item.SeriesId, 'Backdrop', { maxWidth: 640 })
    }
  } else {
    if (item.ImageTags?.Primary) {
      imgUrl = getImageUrl(item.Id, 'Primary', { maxWidth: 420, tag: item.ImageTags.Primary })
    } else if (item.SeriesId) {
      imgUrl = getImageUrl(item.SeriesId, 'Primary', { maxWidth: 420 })
    }
  }

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

  const canDirectPlay = item.Type === 'Movie' || item.Type === 'Episode' || item.MediaType === 'Video'

  return (
    <div className={`group relative flex flex-col ${className}`}>
      <Link
        to={`/item/${item.Id}`}
        className={`relative block overflow-hidden rounded-xl bg-zinc-900/90 ring-1 ring-white/8 transition-all duration-300 group-hover:ring-amber-400/50 group-hover:shadow-[0_12px_32px_rgba(0,0,0,0.65)] ${
          isLandscape ? 'aspect-video' : 'aspect-[2/3]'
        }`}
      >
        {imgUrl ? (
          <img
            src={imgUrl}
            alt={item.Name}
            loading="lazy"
            className="h-full w-full object-cover transition-transform duration-500 ease-out group-hover:scale-105"
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
              navigate(`/play/${item.Id}`)
            }}
            className="absolute left-1/2 top-1/2 flex h-12 w-12 -translate-x-1/2 -translate-y-1/2 scale-90 items-center justify-center rounded-full bg-amber-500 text-zinc-950 opacity-0 shadow-lg shadow-amber-500/30 transition-all duration-200 hover:bg-amber-400 group-hover:scale-100 group-hover:opacity-100"
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

        {/* 右上角已看/未看数量 */}
        {item.UserData?.Played ? (
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

        {/* 底部播放进度条 */}
        {(showProgress || progress > 0) && progress > 0 && (
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
          to={`/item/${item.Id}`}
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
