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
import { useI18n } from '../i18n/I18nContext'
import { AdminModal, FeedbackBanner } from './admin/adminUtils'

type CollectionTab = 'BoxSet' | 'Playlist'
const COLLECTIONS_PAGE_SIZE = 48

export function CollectionsPage() {
  const { id: selectedId } = useParams<{ id: string }>()
  const { auth, isAdmin } = useAuth()
  const { t } = useI18n()
  const { playQueue } = useAudioPlayer()
  const userId = auth!.userId
  const navigate = useNavigate()
  const queryClient = useQueryClient()

  const [activeTab, setActiveTab] = useState<CollectionTab>('BoxSet')
  const [boxSetLimit, setBoxSetLimit] = useState(COLLECTIONS_PAGE_SIZE)
  const [playlistLimit, setPlaylistLimit] = useState(COLLECTIONS_PAGE_SIZE)
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
    isFetching: boxSetsFetching,
    isError: boxSetsError,
    error: boxSetsErr,
    refetch: refetchBoxSets,
  } = useQuery({
    queryKey: ['collections', userId, 'BoxSet', boxSetLimit],
    queryFn: () =>
      api.getItems(userId, {
        includeItemTypes: 'BoxSet',
        recursive: true,
        sortBy: 'SortName',
        sortOrder: 'Ascending',
        startIndex: 0,
        limit: boxSetLimit,
      }),
    placeholderData: (prev) => prev,
  })

  const {
    data: playlistsData,
    isLoading: playlistsLoading,
    isFetching: playlistsFetching,
    isError: playlistsError,
    error: playlistsErr,
    refetch: refetchPlaylists,
  } = useQuery({
    queryKey: ['collections', userId, 'Playlist', playlistLimit],
    queryFn: () =>
      api.getItems(userId, {
        includeItemTypes: 'Playlist',
        recursive: true,
        sortBy: 'SortName',
        sortOrder: 'Ascending',
        startIndex: 0,
        limit: playlistLimit,
      }),
    placeholderData: (prev) => prev,
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
      if (!trimmed) throw new Error(t('collectionsUi.errNameRequired'))
      if (createType === 'Playlist') {
        return api.createPlaylist(trimmed, userId)
      }
      return api.createCollection(trimmed)
    },
    onSuccess: async (created) => {
      setNewName('')
      setCreateOpen(false)
      setActiveTab(createType)
      await queryClient.invalidateQueries({ queryKey: ['collections', userId] })
      setFeedback({
        type: 'success',
        message:
          createType === 'Playlist'
            ? t('collectionsUi.createdPlaylistSuccess', { name: created.Name })
            : t('collectionsUi.createdCollectionSuccess', { name: created.Name }),
      })
    },
    onError: (err) => {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('collectionsUi.createFailed'),
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
      setFeedback({
        type: 'success',
        message: t('collectionsUi.deletedSuccess', { name: target.Name }),
      })
      if (selectedId === target.Id) {
        navigate('/collections', { replace: true })
      }
    },
    onError: (err) => {
      setDeleteTarget(null)
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('collectionsUi.deleteFailed'),
      })
    },
  })

  const removeMemberMutation = useMutation({
    mutationFn: async (member: BaseItemDto) => {
      if (!selectedId) return
      if (isSelectedPlaylist) {
        if (member.PlaylistItemId) {
          await api.removePlaylistItems(selectedId, [member.PlaylistItemId], {
            isPlaylistItemId: true,
          })
        } else {
          await api.removePlaylistItems(selectedId, [member.Id], {
            isPlaylistItemId: false,
          })
        }
      } else {
        await api.removeCollectionItems(selectedId, [member.Id])
      }
    },
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['collectionItems', selectedId] }),
        queryClient.invalidateQueries({ queryKey: ['collections', userId] }),
      ])
      setFeedback({ type: 'success', message: t('collectionsUi.removedMemberSuccess') })
    },
    onError: (err) => {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('collectionsUi.removeMemberFailed'),
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
        message: err instanceof Error ? err.message : t('collectionsUi.reorderFailed'),
      })
    },
  })

  const openCreateModal = (type: CollectionTab) => {
    const allowedType: CollectionTab = !isAdmin && type === 'BoxSet' ? 'Playlist' : type
    setCreateType(allowedType)
    setNewName('')
    setCreateOpen(true)
  }

  // ==================== 详情视图 (/collections/:id) ====================
  if (selectedId) {
    if (containerError) {
      return (
        <div className="px-4 pt-6 lg:px-8">
          <QueryErrorState
            title={t('collectionsUi.loadDetailError')}
            error={containerErr}
            onRetry={() => void refetchContainer()}
            backTo="/collections"
            backLabel={t('collectionsUi.backToCollections')}
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
              <span>{t('collectionsUi.backToCollections')}</span>
            </Link>
            <div className="flex items-center gap-2.5">
              {isSelectedPlaylist ? (
                <ListVideo className="h-6 w-6 text-amber-400" />
              ) : (
                <Layers className="h-6 w-6 text-amber-400" />
              )}
              <h1 className="text-2xl font-bold tracking-tight text-white">
                {selectedContainer?.Name || t('common.loading')}
              </h1>
              <span className="rounded-full bg-white/10 px-2.5 py-0.5 text-xs text-zinc-300">
                {isSelectedPlaylist ? t('collections.playlists') : t('collections.boxSets')} ·{' '}
                {t('collectionsUi.itemsCountBadge', {
                  count: memberData?.TotalRecordCount ?? members.length,
                })}
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
                      navigate(
                        `/play/${encodeURIComponent(firstVideo.Id)}?playlistId=${encodeURIComponent(selectedContainer.Id)}`,
                      )
                    }
                  }}
                  className="inline-flex items-center gap-1.5 rounded-xl bg-amber-500 px-3.5 py-2 text-xs font-semibold text-zinc-950 shadow-md shadow-amber-500/20 transition-colors hover:bg-amber-400"
                >
                  <Play className="h-3.5 w-3.5 fill-current" />
                  <span>{t('collections.playAll')}</span>
                </button>
              )}
              {(isSelectedPlaylist || isAdmin) && (
                <button
                  type="button"
                  onClick={() => setDeleteTarget(selectedContainer)}
                  className="inline-flex items-center gap-1.5 rounded-xl border border-red-500/30 bg-red-500/10 px-3.5 py-2 text-xs font-medium text-red-300 transition-colors hover:bg-red-500/20"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                  <span>
                    {isSelectedPlaylist
                      ? t('collectionsUi.deletePlaylistBtn')
                      : t('collectionsUi.deleteCollectionBtn')}
                  </span>
                </button>
              )}
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
            title={t('collectionsUi.loadMembersError')}
            error={membersErr}
            onRetry={() => void refetchMembers()}
            backTo="/collections"
            backLabel={t('collectionsUi.backToCollections')}
          />
        ) : members.length === 0 ? (
          <div className="rounded-2xl border border-dashed border-white/12 bg-white/[0.02] px-6 py-20 text-center">
            <p className="text-sm font-medium text-zinc-300">
              {t('collectionsUi.emptyMembersTitle')}
            </p>
            <p className="mt-1.5 text-xs text-zinc-500">{t('collectionsUi.emptyMembersDesc')}</p>
          </div>
        ) : (
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
            {members.map((member, idx) => {
              const memberEntryId =
                isSelectedPlaylist && member.PlaylistItemId ? member.PlaylistItemId : member.Id
              const memberKey =
                isSelectedPlaylist && member.PlaylistItemId
                  ? member.PlaylistItemId
                  : `${member.Id}-${idx}`
              return (
                <div key={memberKey} className="group/member relative">
                  <MediaCard item={member} />
                  {isSelectedPlaylist && members.length > 1 && (
                    <div className="absolute left-2 top-2 z-20 flex items-center gap-1 opacity-100 transition-opacity [@media(hover:hover)]:opacity-0 [@media(hover:hover)]:group-hover/member:opacity-100 [@media(hover:hover)]:focus-within:opacity-100">
                      <button
                        type="button"
                        disabled={idx === 0 || moveMemberMutation.isPending}
                        onClick={(e) => {
                          e.preventDefault()
                          e.stopPropagation()
                          moveMemberMutation.mutate({ itemId: memberEntryId, newIndex: idx - 1 })
                        }}
                        title={t('collectionsUi.moveUp')}
                        aria-label={t('collectionsUi.moveUpItemAria', { name: member.Name })}
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
                          moveMemberMutation.mutate({ itemId: memberEntryId, newIndex: idx + 1 })
                        }}
                        title={t('collectionsUi.moveDown')}
                        aria-label={t('collectionsUi.moveDownItemAria', { name: member.Name })}
                        className="flex h-7 w-7 items-center justify-center rounded-full bg-black/75 text-zinc-200 ring-1 ring-white/20 backdrop-blur-md transition-all hover:bg-amber-500 hover:text-zinc-950 disabled:opacity-35"
                      >
                        <ArrowDown className="h-3.5 w-3.5" />
                      </button>
                    </div>
                  )}
                  {(isSelectedPlaylist || isAdmin) && (
                    <button
                      type="button"
                      disabled={removeMemberMutation.isPending}
                      onClick={(e) => {
                        e.preventDefault()
                        e.stopPropagation()
                        removeMemberMutation.mutate(member)
                      }}
                      title={t('collectionsUi.removeFromList')}
                      aria-label={t('collectionsUi.removeItemAria', { name: member.Name })}
                      className="absolute right-2 top-2 z-20 flex h-7 w-7 items-center justify-center rounded-full bg-black/75 text-zinc-200 opacity-100 ring-1 ring-white/20 backdrop-blur-md transition-all hover:bg-red-500 hover:text-white [@media(hover:hover)]:opacity-0 [@media(hover:hover)]:group-hover/member:opacity-100 [@media(hover:hover)]:focus:opacity-100"
                    >
                      <X className="h-3.5 w-3.5" />
                    </button>
                  )}
                </div>
              )
            })}
          </div>
        )}

        <AdminModal
          open={Boolean(deleteTarget)}
          title={
            deleteTarget?.Type === 'Playlist'
              ? t('collectionsUi.confirmDeletePlaylistTitle')
              : t('collectionsUi.confirmDeleteCollectionTitle')
          }
          subtitle={t('collectionsUi.confirmDeleteSubtitle')}
          maxWidthClass="max-w-md"
          onClose={() => setDeleteTarget(null)}
        >
          <div className="space-y-5">
            <p className="text-sm font-medium text-zinc-200">
              {t('collectionsUi.confirmDeletePrompt', { name: deleteTarget?.Name || '' })}
            </p>
            <div className="flex justify-end gap-2.5">
              <button
                type="button"
                onClick={() => setDeleteTarget(null)}
                className="rounded-xl border border-white/10 bg-white/[0.04] px-4 py-2 text-xs font-medium text-zinc-300 hover:bg-white/10"
              >
                {t('common.cancel')}
              </button>
              <button
                type="button"
                disabled={deleteContainerMutation.isPending}
                onClick={() => deleteTarget && deleteContainerMutation.mutate(deleteTarget)}
                className="rounded-xl bg-red-500 px-4 py-2 text-xs font-semibold text-white hover:bg-red-400 disabled:opacity-50"
              >
                {t('collectionsUi.confirmDeleteBtn')}
              </button>
            </div>
          </div>
        </AdminModal>
      </div>
    )
  }

  // ==================== 列表视图 (/collections) ====================
  const boxSets = boxSetsData?.Items || []
  const boxSetsTotal = boxSetsData?.TotalRecordCount ?? boxSets.length
  const playlists = playlistsData?.Items || []
  const playlistsTotal = playlistsData?.TotalRecordCount ?? playlists.length
  const activeItems = activeTab === 'BoxSet' ? boxSets : playlists
  const activeTotal = activeTab === 'BoxSet' ? boxSetsTotal : playlistsTotal
  const isLoading = activeTab === 'BoxSet' ? boxSetsLoading : playlistsLoading
  const isFetching = activeTab === 'BoxSet' ? boxSetsFetching : playlistsFetching
  const activeIsError = activeTab === 'BoxSet' ? boxSetsError : playlistsError
  const activeError = activeTab === 'BoxSet' ? boxSetsErr : playlistsErr
  const activeRefetch = activeTab === 'BoxSet' ? refetchBoxSets : refetchPlaylists
  const hasMoreActive = activeItems.length < activeTotal

  return (
    <div className="space-y-6 px-4 pt-6 lg:px-8">
      {/* 头部标题与 Tab 切换 + 新建按钮 */}
      <div className="flex flex-col gap-4 border-b border-white/8 pb-5 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="flex items-center gap-2.5 text-2xl font-bold tracking-tight text-white">
            <Library className="h-6 w-6 text-amber-400" />
            <span>{t('collections.title')}</span>
          </h1>
          <p className="mt-1 text-xs text-zinc-400">{t('collectionsUi.pageSubtitle')}</p>
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
              <span>{t('collections.boxSets')} (BoxSet)</span>
              <span
                className={`rounded-full px-1.5 py-0.5 text-[10px] font-semibold ${
                  activeTab === 'BoxSet' ? 'bg-black/20 text-zinc-950' : 'bg-white/10 text-zinc-300'
                }`}
              >
                {boxSetsTotal}
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
              <span>{t('collections.playlists')}</span>
              <span
                className={`rounded-full px-1.5 py-0.5 text-[10px] font-semibold ${
                  activeTab === 'Playlist'
                    ? 'bg-black/20 text-zinc-950'
                    : 'bg-white/10 text-zinc-300'
                }`}
              >
                {playlistsTotal}
              </span>
            </button>
          </div>

          {(activeTab === 'Playlist' || isAdmin) && (
            <button
              type="button"
              onClick={() => openCreateModal(activeTab)}
              className="inline-flex items-center gap-1.5 rounded-xl bg-amber-500 px-3.5 py-2 text-xs font-semibold text-zinc-950 shadow-md shadow-amber-500/20 transition-colors hover:bg-amber-400"
            >
              <Plus className="h-4 w-4" />
              <span>
                {activeTab === 'BoxSet'
                  ? t('collections.createCollection')
                  : t('collections.createPlaylist')}
              </span>
            </button>
          )}
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
          title={
            activeTab === 'BoxSet'
              ? t('collectionsUi.loadCollectionsError')
              : t('collectionsUi.loadPlaylistsError')
          }
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
            {activeTab === 'BoxSet'
              ? t('collectionsUi.emptyCollectionsTitle')
              : t('collectionsUi.emptyPlaylistsTitle')}
          </h2>
          <p className="mx-auto mt-2 max-w-md text-xs leading-relaxed text-zinc-400">
            {activeTab === 'BoxSet'
              ? isAdmin
                ? t('collectionsUi.emptyCollectionsAdminDesc')
                : t('collectionsUi.emptyCollectionsMemberDesc')
              : t('collectionsUi.emptyPlaylistsDesc')}
          </p>
        </div>
      ) : (
        <div className="space-y-6">
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
            {activeItems.map((col) => (
              <div key={col.Id} className="group/col relative">
                <MediaCard item={col} linkTo={`/collections/${col.Id}`} />
                {typeof col.ChildCount === 'number' && col.ChildCount > 0 && (
                  <div className="pointer-events-none absolute left-2 top-2 rounded-full bg-black/75 px-2 py-0.5 text-[10px] font-semibold text-amber-300 ring-1 ring-amber-400/40 backdrop-blur-md">
                    {t('collectionsUi.itemsCountBadge', { count: col.ChildCount })}
                  </div>
                )}
                {(col.Type === 'Playlist' || isAdmin) && (
                  <button
                    type="button"
                    onClick={(e) => {
                      e.preventDefault()
                      e.stopPropagation()
                      setDeleteTarget(col)
                    }}
                    title={t('collectionsUi.deleteItemAria', { name: col.Name })}
                    aria-label={t('collectionsUi.deleteItemAria', { name: col.Name })}
                    className="absolute right-2 top-2 z-20 flex h-7 w-7 items-center justify-center rounded-full bg-black/75 text-zinc-300 opacity-100 ring-1 ring-white/20 backdrop-blur-md transition-all hover:bg-red-500 hover:text-white [@media(hover:hover)]:opacity-0 [@media(hover:hover)]:group-hover/col:opacity-100 [@media(hover:hover)]:focus:opacity-100"
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </button>
                )}
              </div>
            ))}
          </div>
          {hasMoreActive && (
            <div className="flex justify-center pt-2">
              <button
                type="button"
                disabled={isFetching}
                onClick={() => {
                  if (activeTab === 'BoxSet') {
                    setBoxSetLimit((prev) => prev + COLLECTIONS_PAGE_SIZE)
                  } else {
                    setPlaylistLimit((prev) => prev + COLLECTIONS_PAGE_SIZE)
                  }
                }}
                className="inline-flex items-center gap-2 rounded-xl border border-white/10 bg-white/[0.05] px-5 py-2.5 text-xs font-medium text-zinc-200 transition-colors hover:border-amber-400/40 hover:bg-white/10 disabled:opacity-50"
              >
                {isFetching && <Loader2 className="h-3.5 w-3.5 animate-spin text-amber-400" />}
                <span>
                  {t('collectionsUi.loadMoreCount', {
                    shown: activeItems.length,
                    total: activeTotal,
                  })}
                </span>
              </button>
            </div>
          )}
        </div>
      )}

      {/* 新建合集 / 播放列表弹窗 */}
      <AdminModal
        open={createOpen}
        title={
          createType === 'BoxSet'
            ? t('collections.createCollection')
            : t('collections.createPlaylist')
        }
        subtitle={
          createType === 'BoxSet'
            ? t('collectionsUi.createCollectionModalSub')
            : t('collectionsUi.createPlaylistModalSub')
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
          {isAdmin && (
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
                {t('collectionsUi.typeBoxSetBtn')}
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
                {t('collectionsUi.typePlaylistBtn')}
              </button>
            </div>
          )}

          <label className="block space-y-1.5">
            <span className="text-xs font-medium text-zinc-300">{t('collectionsUi.nameLabel')}</span>
            <input
              type="text"
              autoFocus
              required
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
              placeholder={
                createType === 'BoxSet'
                  ? t('collectionsUi.collectionNamePlaceholder')
                  : t('collectionsUi.playlistNamePlaceholder')
              }
              className="w-full rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2.5 text-sm text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
            />
          </label>

          <div className="flex justify-end gap-2.5 pt-2">
            <button
              type="button"
              onClick={() => setCreateOpen(false)}
              className="rounded-xl border border-white/10 bg-white/[0.04] px-4 py-2 text-xs font-medium text-zinc-300 hover:bg-white/10"
            >
              {t('common.cancel')}
            </button>
            <button
              type="submit"
              disabled={createMutation.isPending || !newName.trim()}
              className="inline-flex items-center gap-1.5 rounded-xl bg-amber-500 px-4 py-2 text-xs font-semibold text-zinc-950 hover:bg-amber-400 disabled:opacity-50"
            >
              {createMutation.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              <span>{t('collectionsUi.createSubmitBtn')}</span>
            </button>
          </div>
        </form>
      </AdminModal>

      {/* 删除确认弹窗 */}
      <AdminModal
        open={Boolean(deleteTarget)}
        title={
          deleteTarget?.Type === 'Playlist'
            ? t('collectionsUi.confirmDeletePlaylistTitle')
            : t('collectionsUi.confirmDeleteCollectionTitle')
        }
        subtitle={t('collectionsUi.confirmDeleteSubtitle')}
        maxWidthClass="max-w-md"
        onClose={() => setDeleteTarget(null)}
      >
        <div className="space-y-5">
          <p className="text-sm font-medium text-zinc-200">
            {t('collectionsUi.confirmDeletePrompt', { name: deleteTarget?.Name || '' })}
          </p>
          <div className="flex justify-end gap-2.5">
            <button
              type="button"
              onClick={() => setDeleteTarget(null)}
              className="rounded-xl border border-white/10 bg-white/[0.04] px-4 py-2 text-xs font-medium text-zinc-300 hover:bg-white/10"
            >
              {t('common.cancel')}
            </button>
            <button
              type="button"
              disabled={deleteContainerMutation.isPending}
              onClick={() => deleteTarget && deleteContainerMutation.mutate(deleteTarget)}
              className="rounded-xl bg-red-500 px-4 py-2 text-xs font-semibold text-white hover:bg-red-400 disabled:opacity-50"
            >
              {t('collectionsUi.confirmDeleteBtn')}
            </button>
          </div>
        </div>
      </AdminModal>
    </div>
  )
}
