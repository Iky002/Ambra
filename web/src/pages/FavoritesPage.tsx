import { useQuery } from '@tanstack/react-query'
import { Heart, Loader2 } from 'lucide-react'
import { api } from '../api/client'
import { MediaCard } from '../components/MediaCard'
import { QueryErrorState } from '../components/QueryErrorState'
import { useAuth } from '../context/AuthContext'

export function FavoritesPage() {
  const { auth } = useAuth()
  const userId = auth!.userId

  const { data, isLoading, isError, error, refetch } = useQuery({
    queryKey: ['favorites', userId],
    queryFn: () =>
      api.getItems(userId, {
        filters: 'IsFavorite',
        includeItemTypes: 'Movie,Series,Episode,MusicAlbum,MusicArtist,Audio',
        sortBy: 'SortName',
        sortOrder: 'Ascending',
        limit: 100,
      }),
  })

  const items = data?.Items || []

  return (
    <div className="space-y-6 px-4 pt-6 lg:px-8">
      <div className="flex items-center gap-3 border-b border-white/8 pb-5">
        <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-rose-500/15 text-rose-400">
          <Heart className="h-5 w-5 fill-current" />
        </div>
        <div>
          <h1 className="text-2xl font-bold text-white">我的收藏</h1>
          <p className="text-xs text-zinc-400">共收藏 {items.length} 部影片与剧集</p>
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
        <div className="rounded-2xl border border-white/8 bg-white/[0.02] py-20 text-center text-sm text-zinc-500">
          还没有收藏任何影片，在详情页点击爱心即可加入收藏
        </div>
      ) : (
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
          {items.map((item) => (
            <MediaCard key={item.Id} item={item} />
          ))}
        </div>
      )}
    </div>
  )
}
