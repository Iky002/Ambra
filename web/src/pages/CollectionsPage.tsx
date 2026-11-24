import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  ArrowDown,
  ArrowLeft,
  ArrowUp,
  Layers,
  Library,
  ListVideo,
  Loader2,
  Play,
  Plus,
  Trash2,
  X,
} from 'lucide-react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api } from '../api/client'
import type { BaseItemDto } from '../api/types'
import { MediaCard } from '../components/MediaCard'
import { QueryErrorState } from '../components/QueryErrorState'
import { useAudioPlayer } from '../context/AudioPlayerContext'
import { useAuth } from '../context/AuthContext'
import { AdminModal, FeedbackBanner } from './admin/adminUtils'

type CollectionTab = 'BoxSet' | 'Playlist'

export function CollectionsPage() {
  const { id: selectedId } = useParams<{ id: string }>()
  const { auth } = useAuth()
  const { playQueue } = useAudioPlayer()
  const userId = auth!.userId
  const navigate = useNavigate()
  const queryClient = useQueryClient()

  const [activeTab, setActiveTab] = useState<CollectionTab>('BoxSet')
  const [createOpen, setCreateOpen] = useState(false)
  const [createType, setCreateType] = useState<CollectionTab>('BoxSet')
  const [newName, setNewName] = useState('')
  const [deleteTarget, setDeleteTarget] = useState<BaseItemDto | null>(null)
  const [feedback, setFeedback] = useState<{ type: 'success' | 'error'; message: string } | null>(
    null,
  )

  const {
    data: boxSetsData,
    isLoading: boxSetsLoading,
    isError: boxSetsError,
    error: boxSetsErr,
    refetch: refetchBoxSets,
  } = useQuery({
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

  const {
    data: playlistsData,
    isLoading: playlistsLoading,
    isError: playlistsError,
    error: playlistsErr,
    refetch: refetchPlaylists,
  } = useQuery({
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

  // 详情视图：查询当前选中的合集或播放列表元数据及其成员条目
  const {
    data: selectedContainer,
    isLoading: containerLoading,
    isError: containerError,
    error: containerErr,
    refetch: refetchContainer,
  } = useQuery({
    queryKey: ['item', userId, selectedId],
    queryFn: () => api.getItem(userId, selectedId!),
    enabled: Boolean(selectedId),
  })

  const isSelectedPlaylist = selectedContainer?.Type === 'Playlist'

  const {
    data: memberData,
    isLoading: membersLoading,
    isError: membersError,
    error: membersErr,
    refetch: refetchMembers,
  } = useQuery({
    queryKey: ['collectionItems', selectedId, selectedContainer?.Type, userId],
    queryFn: () =>
      isSelectedPlaylist
        ? api.getPlaylistItems(selectedId!, userId)
        : api.getCollectionItems(selectedId!, userId),
    enabled: Boolean(selectedId && selectedContainer),
  })

  const createMutation = useMutation({
    mutationFn: async () => {
      const trimmed = newName.trim()
      if (!trimmed) throw new Error('请输入名称')
      if (createType === 'Playlist') {
        return api.createPlaylist(trimmed, userId)
      }
      return api.createCollection(trimmed)
    },
    onSuccess: async (created) => {
      const label = createType === 'Playlist' ? '播放列表' : '合集'
      setNewName('')
      setCreateOpen(false)
      setActiveTab(createType)
      await queryClient.invalidateQueries({ queryKey: ['collections', userId] })
      setFeedback({ type: 'success', message: `已创建${label}「${created.Name}」` })
    },
    onError: (err) => {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '创建失败',
      })
    },
  })

  const deleteContainerMutation = useMutation({
    mutationFn: async (target: BaseItemDto) => {
      if (target.Type === 'Playlist') {
        await api.deletePlaylist(target.Id)
      } else {
        await api.deleteCollection(target.Id)
      }
      return target
    },
    onSuccess: async (target) => {
      setDeleteTarget(null)
      await queryClient.invalidateQueries({ queryKey: ['collections', userId] })
      setFeedback({ type: 'success', message: `已删除「${target.Name}」` })
      if (selectedId === target.Id) {
        navigate('/collections', { replace: true })
      }
    },
    onError: (err) => {
      setDeleteTarget(null)
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '删除失败',
      })
    },
  })

  const removeMemberMutation = useMutation({
    mutationFn: async (itemId: string) => {
      if (!selectedId) return
      if (isSelectedPlaylist) {
        await api.removePlaylistItems(selectedId, [itemId])
      } else {
        await api.removeCollectionItems(selectedId, [itemId])
      }
    },
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['collectionItems', selectedId] }),
        queryClient.invalidateQueries({ queryKey: ['collections', userId] }),
      ])
      setFeedback({ type: 'success', message: '已将该作品移出列表' })
    },
    onError: (err) => {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '移出失败',
      })
    },
  })

  const moveMemberMutation = useMutation({
    mutationFn: async ({ itemId, newIndex }: { itemId: string; newIndex: number }) => {
      if (!selectedId || !isSelectedPlaylist) return
      await api.movePlaylistItem(selectedId, itemId, newIndex)
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['collectionItems', selectedId] })
    },
    onError: (err) => {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '调整排序失败',
      })
    },
  })

  const openCreateModal = (type: CollectionTab) => {
    setCreateType(type)
    setNewName('')
    setCreateOpen(true)
  }

  // ==================== 详情视图 (/collections/:id) ====================
  if (selectedId) {
    if (containerError) {
      return (
        <div className="px-4 pt-6 lg:px-8">
          <QueryErrorState
            title="无法加载合集或播放列表详情"
            error={containerErr}
            onRetry={() => void refetchContainer()}
            backTo="/collections"
            backLabel="返回合集与播放列表"
          />
        </div>
      )
    }

    const members = memberData?.Items || []
    return (
      <div className="space-y-6 px-4 pt-6 lg:px-8">
        <div className="flex flex-col gap-4 border-b border-white/8 pb-5 sm:flex-row sm:items-center sm:justify-between">
          <div className="space-y-2">
            <Link
              to="/collections"
              className="inline-flex items-center gap-1.5 text-xs font-medium text-amber-400 transition-colors hover:text-amber-300"
            >
              <ArrowLeft className="h-3.5 w-3.5" />
              <span>返回合集与播放列表</span>
            </Link>
            <div className="flex items-center gap-2.5">
              {isSelectedPlaylist ? (
                <ListVideo className="h-6 w-6 text-amber-400" />
              ) : (
                <Layers className="h-6 w-6 text-amber-400" />
              )}
              <h1 className="text-2xl font-bold tracking-tight text-white">
                {selectedContainer?.Name || '加载中...'}
              </h1>
              <span className="rounded-full bg-white/10 px-2.5 py-0.5 text-xs text-zinc-300">
                {isSelectedPlaylist ? '播放列表' : '合集'} · {members.length} 部
              </span>
            </div>
            {selectedContainer?.Overview && (
              <p className="max-w-3xl text-xs leading-relaxed text-zinc-400">
                {selectedContainer.Overview}
              </p>
            )}
          </div>

          {selectedContainer && (
            <div className="flex flex-wrap items-center gap-2 self-start">
              {isSelectedPlaylist && members.length > 0 && (
                <button
                  type="button"
                  onClick={() => {
                    const audioItems = members.filter((m) => m.Type === 'Audio')
                    if (
                      audioItems.length > 0 &&
                      (selectedContainer.MediaType === 'Audio' ||
                        audioItems.length === members.length ||
                        members[0]?.Type === 'Audio')
                    ) {
                      playQueue(audioItems, 0)
                      return
                    }
                    const firstVideo = members.find((m) => m.Type !== 'Audio') || members[0]
                    if (firstVideo) {
                      navigate(`/play/${encodeURIComponent(firstVideo.Id)}`)
                    }
                  }}
                  className="inline-flex items-center gap-1.5 rounded-xl bg-amber-500 px-3.5 py-2 text-xs font-semibold text-zinc-950 shadow-md shadow-amber-500/20 transition-colors hover:bg-amber-400"
                >
                  <Play className="h-3.5 w-3.5 fill-current" />
                  <span>播放全部</span>
                </button>
              )}
              <button
                type="button"
                onClick={() => setDeleteTarget(selectedContainer)}
                className="inline-flex items-center gap-1.5 rounded-xl border border-red-500/30 bg-red-500/10 px-3.5 py-2 text-xs font-medium text-red-300 transition-colors hover:bg-red-500/20"
              >
                <Trash2 className="h-3.5 w-3.5" />
                <span>删除{isSelectedPlaylist ? '播放列表' : '合集'}</span>
              </button>
            </div>
          )}
        </div>

        {feedback && (
          <FeedbackBanner
            type={feedback.type}
            message={feedback.message}
            onClose={() => setFeedback(null)}
          />
        )}

        {containerLoading || membersLoading ? (
          <div className="flex h-64 items-center justify-center">
            <Loader2 className="h-7 w-7 animate-spin text-amber-400" />
          </div>
        ) : membersError ? (
          <QueryErrorState
            compact
            title="无法加载列表成员"
            error={membersErr}
            onRetry={() => void refetchMembers()}
            backTo="/collections"
            backLabel="返回合集与播放列表"
          />
        ) : members.length === 0 ? (
          <div className="rounded-2xl border border-dashed border-white/12 bg-white/[0.02] px-6 py-20 text-center">
            <p className="text-sm font-medium text-zinc-300">当前列表还没有任何作品</p>
            <p className="mt-1.5 text-xs text-zinc-500">
              在任意影片或剧集详情页点击「加入合集 / 片单」即可快速添加。
            </p>
          </div>
        ) : (
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
            {members.map((member, idx) => (
              <div key={member.Id} className="group/member relative">
                <MediaCard item={member} />
                {isSelectedPlaylist && members.length > 1 && (
                  <div className="absolute left-2 top-2 z-20 flex items-center gap-1 opacity-0 transition-opacity group-hover/member:opacity-100 focus-within:opacity-100">
                    <button
                      type="button"
                      disabled={idx === 0 || moveMemberMutation.isPending}
                      onClick={(e) => {
                        e.preventDefault()
                        e.stopPropagation()
                        moveMemberMutation.mutate({ itemId: member.Id, newIndex: idx - 1 })
                      }}
                      title="上移"
                      aria-label={`上移 ${member.Name}`}
                      className="flex h-7 w-7 items-center justify-center rounded-full bg-black/75 text-zinc-200 ring-1 ring-white/20 backdrop-blur-md transition-all hover:bg-amber-500 hover:text-zinc-950 disabled:opacity-35"
                    >
                      <ArrowUp className="h-3.5 w-3.5" />
                    </button>
                    <button
                      type="button"
                      disabled={idx === members.length - 1 || moveMemberMutation.isPending}
                      onClick={(e) => {
                        e.preventDefault()
                        e.stopPropagation()
                        moveMemberMutation.mutate({ itemId: member.Id, newIndex: idx + 1 })
                      }}
                      title="下移"
                      aria-label={`下移 ${member.Name}`}
                      className="flex h-7 w-7 items-center justify-center rounded-full bg-black/75 text-zinc-200 ring-1 ring-white/20 backdrop-blur-md transition-all hover:bg-amber-500 hover:text-zinc-950 disabled:opacity-35"
                    >
                      <ArrowDown className="h-3.5 w-3.5" />
                    </button>
                  </div>
                )}
                <button
                  type="button"
                  disabled={removeMemberMutation.isPending}
                  onClick={(e) => {
                    e.preventDefault()
                    e.stopPropagation()
                    removeMemberMutation.mutate(member.Id)
                  }}
                  title="移出列表"
                  aria-label={`移出 ${member.Name}`}
                  className="absolute right-2 top-2 z-20 flex h-7 w-7 items-center justify-center rounded-full bg-black/75 text-zinc-200 opacity-0 ring-1 ring-white/20 backdrop-blur-md transition-all hover:bg-red-500 hover:text-white group-hover/member:opacity-100 focus:opacity-100"
                >
                  <X className="h-3.5 w-3.5" />
                </button>
              </div>
            ))}
          </div>
        )}

        <AdminModal
          open={Boolean(deleteTarget)}
          title={`确认删除${deleteTarget?.Type === 'Playlist' ? '播放列表' : '合集'}`}
          subtitle="仅移除合集分组，不会删除媒体库中的原始影片"
          maxWidthClass="max-w-md"
          onClose={() => setDeleteTarget(null)}
        >
          <div className="space-y-5">
            <p className="text-sm text-zinc-300">
              确定要删除「<strong className="text-white">{deleteTarget?.Name}</strong>」吗？
            </p>
            <div className="flex justify-end gap-2.5">
              <button
                type="button"
                onClick={() => setDeleteTarget(null)}
                className="rounded-xl border border-white/10 bg-white/[0.04] px-4 py-2 text-xs font-medium text-zinc-300 hover:bg-white/10"
              >
                取消
              </button>
              <button
                type="button"
                disabled={deleteContainerMutation.isPending}
                onClick={() => deleteTarget && deleteContainerMutation.mutate(deleteTarget)}
                className="rounded-xl bg-red-500 px-4 py-2 text-xs font-semibold text-white hover:bg-red-400 disabled:opacity-50"
              >
                确认删除
              </button>
            </div>
          </div>
        </AdminModal>
      </div>
    )
  }

  // ==================== 列表视图 (/collections) ====================
  const boxSets = boxSetsData?.Items || []
  const playlists = playlistsData?.Items || []
  const activeItems = activeTab === 'BoxSet' ? boxSets : playlists
  const isLoading = activeTab === 'BoxSet' ? boxSetsLoading : playlistsLoading
  const activeIsError = activeTab === 'BoxSet' ? boxSetsError : playlistsError
  const activeError = activeTab === 'BoxSet' ? boxSetsErr : playlistsErr
  const activeRefetch = activeTab === 'BoxSet' ? refetchBoxSets : refetchPlaylists

  return (
    <div className="space-y-6 px-4 pt-6 lg:px-8">
      {/* 头部标题与 Tab 切换 + 新建按钮 */}
      <div className="flex flex-col gap-4 border-b border-white/8 pb-5 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="flex items-center gap-2.5 text-2xl font-bold tracking-tight text-white">
            <Library className="h-6 w-6 text-amber-400" />
            <span>合集与播放列表</span>
          </h1>
          <p className="mt-1 text-xs text-zinc-400">
            浏览系列电影合集（BoxSet）与自定义播放列表，点击即可管理列表内的全部作品
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-2.5">
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
                  activeTab === 'Playlist'
                    ? 'bg-black/20 text-zinc-950'
                    : 'bg-white/10 text-zinc-300'
                }`}
              >
                {playlists.length}
              </span>
            </button>
          </div>

          <button
            type="button"
            onClick={() => openCreateModal(activeTab)}
            className="inline-flex items-center gap-1.5 rounded-xl bg-amber-500 px-3.5 py-2 text-xs font-semibold text-zinc-950 shadow-md shadow-amber-500/20 transition-colors hover:bg-amber-400"
          >
            <Plus className="h-4 w-4" />
            <span>{activeTab === 'BoxSet' ? '新建合集' : '新建播放列表'}</span>
          </button>
        </div>
      </div>

      {feedback && (
        <FeedbackBanner
          type={feedback.type}
          message={feedback.message}
          onClose={() => setFeedback(null)}
        />
      )}

      {/* 列表网格 */}
      {isLoading ? (
        <div className="flex h-64 items-center justify-center">
          <Loader2 className="h-7 w-7 animate-spin text-amber-400" />
        </div>
      ) : activeIsError ? (
        <QueryErrorState
          title={activeTab === 'BoxSet' ? '无法加载合集列表' : '无法加载播放列表'}
          error={activeError}
          onRetry={() => void activeRefetch()}
          backTo="/"
        />
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
              ? '点击右上角「新建合集」，将系列电影或主题影片归类管理。'
              : '点击右上角「新建播放列表」，创建您的专属片单。'}
          </p>
        </div>
      ) : (
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
          {activeItems.map((col) => (
            <div key={col.Id} className="group/col relative">
              <MediaCard item={col} linkTo={`/collections/${col.Id}`} />
              {typeof col.ChildCount === 'number' && col.ChildCount > 0 && (
                <div className="pointer-events-none absolute left-2 top-2 rounded-full bg-black/75 px-2 py-0.5 text-[10px] font-semibold text-amber-300 ring-1 ring-amber-400/40 backdrop-blur-md">
                  {col.ChildCount} 部
                </div>
              )}
              <button
                type="button"
                onClick={(e) => {
                  e.preventDefault()
                  e.stopPropagation()
                  setDeleteTarget(col)
                }}
                title={`删除 ${col.Name}`}
                aria-label={`删除 ${col.Name}`}
                className="absolute right-2 top-2 z-20 flex h-7 w-7 items-center justify-center rounded-full bg-black/75 text-zinc-300 opacity-0 ring-1 ring-white/20 backdrop-blur-md transition-all hover:bg-red-500 hover:text-white group-hover/col:opacity-100 focus:opacity-100"
              >
                <Trash2 className="h-3.5 w-3.5" />
              </button>
            </div>
          ))}
        </div>
      )}

      {/* 新建合集 / 播放列表弹窗 */}
      <AdminModal
        open={createOpen}
        title={createType === 'BoxSet' ? '新建合集' : '新建播放列表'}
        subtitle={
          createType === 'BoxSet'
            ? '创建一个新的系列合集（BoxSet）'
            : '创建一个新的自定义播放列表'
        }
        maxWidthClass="max-w-md"
        onClose={() => setCreateOpen(false)}
      >
        <form
          onSubmit={(e) => {
            e.preventDefault()
            createMutation.mutate()
          }}
          className="space-y-4"
        >
          <div className="flex gap-2">
            <button
              type="button"
              onClick={() => setCreateType('BoxSet')}
              className={`flex-1 rounded-xl border px-3 py-2 text-xs font-medium transition-colors ${
                createType === 'BoxSet'
                  ? 'border-amber-400/50 bg-amber-500/15 text-amber-300'
                  : 'border-white/10 bg-white/[0.03] text-zinc-400 hover:text-zinc-200'
              }`}
            >
              合集 (BoxSet)
            </button>
            <button
              type="button"
              onClick={() => setCreateType('Playlist')}
              className={`flex-1 rounded-xl border px-3 py-2 text-xs font-medium transition-colors ${
                createType === 'Playlist'
                  ? 'border-amber-400/50 bg-amber-500/15 text-amber-300'
                  : 'border-white/10 bg-white/[0.03] text-zinc-400 hover:text-zinc-200'
              }`}
            >
              播放列表 (Playlist)
            </button>
          </div>

          <label className="block space-y-1.5">
            <span className="text-xs font-medium text-zinc-300">名称</span>
            <input
              type="text"
              autoFocus
              required
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
              placeholder={createType === 'BoxSet' ? '例如：诺兰科幻三部曲' : '例如：周末必看片单'}
              className="w-full rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2.5 text-sm text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
            />
          </label>

          <div className="flex justify-end gap-2.5 pt-2">
            <button
              type="button"
              onClick={() => setCreateOpen(false)}
              className="rounded-xl border border-white/10 bg-white/[0.04] px-4 py-2 text-xs font-medium text-zinc-300 hover:bg-white/10"
            >
              取消
            </button>
            <button
              type="submit"
              disabled={createMutation.isPending || !newName.trim()}
              className="inline-flex items-center gap-1.5 rounded-xl bg-amber-500 px-4 py-2 text-xs font-semibold text-zinc-950 hover:bg-amber-400 disabled:opacity-50"
            >
              {createMutation.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              <span>创建</span>
            </button>
          </div>
        </form>
      </AdminModal>

      {/* 删除确认弹窗 */}
      <AdminModal
        open={Boolean(deleteTarget)}
        title={`确认删除${deleteTarget?.Type === 'Playlist' ? '播放列表' : '合集'}`}
        subtitle="仅移除合集分组，不会删除媒体库中的原始影片"
        maxWidthClass="max-w-md"
        onClose={() => setDeleteTarget(null)}
      >
        <div className="space-y-5">
          <p className="text-sm text-zinc-300">
            确定要删除「<strong className="text-white">{deleteTarget?.Name}</strong>」吗？
          </p>
          <div className="flex justify-end gap-2.5">
            <button
              type="button"
              onClick={() => setDeleteTarget(null)}
              className="rounded-xl border border-white/10 bg-white/[0.04] px-4 py-2 text-xs font-medium text-zinc-300 hover:bg-white/10"
            >
              取消
            </button>
            <button
              type="button"
              disabled={deleteContainerMutation.isPending}
              onClick={() => deleteTarget && deleteContainerMutation.mutate(deleteTarget)}
              className="rounded-xl bg-red-500 px-4 py-2 text-xs font-semibold text-white hover:bg-red-400 disabled:opacity-50"
            >
              确认删除
            </button>
          </div>
        </div>
      </AdminModal>
    </div>
  )
}
