import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Heart, Loader2 } from 'lucide-react'
import { api } from '../api/client'
import { MediaCard } from '../components/MediaCard'
import { QueryErrorState } from '../components/QueryErrorState'
import { useAuth } from '../context/AuthContext'
import { useI18n } from '../i18n/I18nContext'

const FAVORITES_PAGE_SIZE = 48

export function FavoritesPage() {
  const { auth } = useAuth()
  const { t } = useI18n()
  const userId = auth!.userId
  const [limit, setLimit] = useState(FAVORITES_PAGE_SIZE)

  const { data, isLoading, isFetching, isError, error, refetch } = useQuery({
    queryKey: ['favorites', userId, limit],
    queryFn: () =>
      api.getItems(userId, {
        filters: 'IsFavorite',
        includeItemTypes: 'Movie,Series,Episode,MusicAlbum,MusicArtist,Audio',
        sortBy: 'SortName',
        sortOrder: 'Ascending',
        startIndex: 0,
        limit,
      }),
    placeholderData: (prev) => prev,
  })

  const items = data?.Items || []
  const total = data?.TotalRecordCount ?? items.length
  const hasMore = items.length < total

  return (
    <div className="space-y-6 px-4 pt-6 lg:px-8">
      <div className="flex items-center gap-3 border-b border-white/8 pb-5">
        <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-rose-500/15 text-rose-400">
          <Heart className="h-5 w-5 fill-current" />
        </div>
        <div>
          <h1 className="text-2xl font-bold text-white">{t('favorites.title')}</h1>
          <p className="text-xs text-zinc-400">共收藏 {total} 部影片与剧集</p>
        </div>
      </div>

      {isLoading ? (
        <div className="flex h-64 items-center justify-center">
          <Loader2 className="h-7 w-7 animate-spin text-amber-400" />
        </div>
      ) : isError ? (
        <QueryErrorState
          title="无法加载收藏列表"
          error={error}
          onRetry={() => void refetch()}
          backTo="/"
        />
      ) : items.length === 0 ? (
        <div className="rounded-2xl border border-white/8 bg-white/[0.02] py-20 text-center">
          <p className="text-sm font-medium text-zinc-300">{t('favorites.emptyTitle')}</p>
          <p className="mt-1.5 text-xs text-zinc-500">{t('favorites.emptyDesc')}</p>
        </div>
      ) : (
        <div className="space-y-6">
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
            {items.map((item) => (
              <MediaCard key={item.Id} item={item} />
            ))}
          </div>
          {hasMore && (
            <div className="flex justify-center pt-2">
              <button
                type="button"
                disabled={isFetching}
                onClick={() => setLimit((prev) => prev + FAVORITES_PAGE_SIZE)}
                className="inline-flex items-center gap-2 rounded-xl border border-white/10 bg-white/[0.05] px-5 py-2.5 text-xs font-medium text-zinc-200 transition-colors hover:border-amber-400/40 hover:bg-white/10 disabled:opacity-50"
              >
                {isFetching && <Loader2 className="h-3.5 w-3.5 animate-spin text-amber-400" />}
                <span>
                  加载更多（已显示 {items.length} / {total}）
                </span>
              </button>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
