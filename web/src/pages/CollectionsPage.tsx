import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Layers, Library, ListVideo, Loader2 } from 'lucide-react'
import { api } from '../api/client'
import { MediaCard } from '../components/MediaCard'
import { useAuth } from '../context/AuthContext'

type CollectionTab = 'BoxSet' | 'Playlist'

export function CollectionsPage() {
  const { auth } = useAuth()
  const userId = auth!.userId
  const [activeTab, setActiveTab] = useState<CollectionTab>('BoxSet')

  const { data: boxSetsData, isLoading: boxSetsLoading } = useQuery({
    queryKey: ['collections', userId, 'BoxSet'],
    queryFn: () =>
      api.getItems(userId, {
        includeItemTypes: 'BoxSet',
        recursive: true,
        sortBy: 'SortName',
        sortOrder: 'Ascending',
        limit: 100,
      }),
  })

  const { data: playlistsData, isLoading: playlistsLoading } = useQuery({
    queryKey: ['collections', userId, 'Playlist'],
    queryFn: () =>
      api.getItems(userId, {
        includeItemTypes: 'Playlist',
        recursive: true,
        sortBy: 'SortName',
        sortOrder: 'Ascending',
        limit: 100,
      }),
  })

  const boxSets = boxSetsData?.Items || []
  const playlists = playlistsData?.Items || []
  const activeItems = activeTab === 'BoxSet' ? boxSets : playlists
  const isLoading = activeTab === 'BoxSet' ? boxSetsLoading : playlistsLoading

  return (
    <div className="space-y-6 px-4 pt-6 lg:px-8">
      {/* 头部标题与 Tab 切换 */}
      <div className="flex flex-col gap-4 border-b border-white/8 pb-5 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="flex items-center gap-2.5 text-2xl font-bold tracking-tight text-white">
            <Library className="h-6 w-6 text-amber-400" />
            <span>合集与播放列表</span>
          </h1>
          <p className="mt-1 text-xs text-zinc-400">
            浏览系列电影合集（BoxSet）与自定义播放列表，点击即可查看合集内全部作品
          </p>
        </div>

        <div className="inline-flex items-center gap-1 rounded-xl border border-white/10 bg-zinc-950/80 p-1">
          <button
            type="button"
            onClick={() => setActiveTab('BoxSet')}
            className={`inline-flex items-center gap-1.5 rounded-lg px-3.5 py-2 text-xs font-medium transition-colors ${
              activeTab === 'BoxSet'
                ? 'bg-amber-500 text-zinc-950 shadow-sm'
                : 'text-zinc-400 hover:bg-white/[0.05] hover:text-zinc-100'
            }`}
          >
            <Layers className="h-3.5 w-3.5" />
            <span>合集 (BoxSet)</span>
            <span
              className={`rounded-full px-1.5 py-0.5 text-[10px] font-semibold ${
                activeTab === 'BoxSet' ? 'bg-black/20 text-zinc-950' : 'bg-white/10 text-zinc-300'
              }`}
            >
              {boxSets.length}
            </span>
          </button>

          <button
            type="button"
            onClick={() => setActiveTab('Playlist')}
            className={`inline-flex items-center gap-1.5 rounded-lg px-3.5 py-2 text-xs font-medium transition-colors ${
              activeTab === 'Playlist'
                ? 'bg-amber-500 text-zinc-950 shadow-sm'
                : 'text-zinc-400 hover:bg-white/[0.05] hover:text-zinc-100'
            }`}
          >
            <ListVideo className="h-3.5 w-3.5" />
            <span>播放列表</span>
            <span
              className={`rounded-full px-1.5 py-0.5 text-[10px] font-semibold ${
                activeTab === 'Playlist' ? 'bg-black/20 text-zinc-950' : 'bg-white/10 text-zinc-300'
              }`}
            >
              {playlists.length}
            </span>
          </button>
        </div>
      </div>

      {/* 列表网格 */}
      {isLoading ? (
        <div className="flex h-64 items-center justify-center">
          <Loader2 className="h-7 w-7 animate-spin text-amber-400" />
        </div>
      ) : activeItems.length === 0 ? (
        <div className="rounded-2xl border border-dashed border-white/12 bg-white/[0.02] px-6 py-20 text-center">
          <div className="mx-auto mb-3 flex h-12 w-12 items-center justify-center rounded-2xl bg-amber-500/15 text-amber-400 ring-1 ring-amber-500/30">
            {activeTab === 'BoxSet' ? (
              <Layers className="h-6 w-6" />
            ) : (
              <ListVideo className="h-6 w-6" />
            )}
          </div>
          <h2 className="text-base font-semibold text-zinc-200">
            {activeTab === 'BoxSet' ? '暂无媒体合集' : '暂无播放列表'}
          </h2>
          <p className="mx-auto mt-2 max-w-md text-xs leading-relaxed text-zinc-400">
            {activeTab === 'BoxSet'
              ? '当媒体库中存在系列合集（BoxSet）时，它们会自动聚合展示在这里。'
              : '创建或导入自定义播放列表后，即可在此集中管理与连续播放。'}
          </p>
        </div>
      ) : (
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
          {activeItems.map((col) => (
            <div key={col.Id} className="relative">
              <MediaCard item={col} linkTo={`/library/${col.Id}`} />
              {typeof col.ChildCount === 'number' && col.ChildCount > 0 && (
                <div className="pointer-events-none absolute right-2 top-2 rounded-full bg-black/75 px-2 py-0.5 text-[10px] font-semibold text-amber-300 ring-1 ring-amber-400/40 backdrop-blur-md">
                  {col.ChildCount} 部
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
