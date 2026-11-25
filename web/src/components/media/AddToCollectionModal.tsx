import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { FolderPlus, Layers, ListVideo, Loader2, Plus } from 'lucide-react'
import { api } from '../../api/client'
import type { BaseItemDto } from '../../api/types'
import { useAuth } from '../../context/AuthContext'
import { AdminModal, FeedbackBanner } from '../../pages/admin/adminUtils'

type ContainerTab = 'BoxSet' | 'Playlist'

export interface AddToCollectionModalProps {
  open: boolean
  itemIds: string[]
  itemLabel: string
  mediaType?: 'Video' | 'Audio'
  onClose: () => void
  onSuccess?: (message: string) => void
}

export function AddToCollectionModal({
  open,
  itemIds,
  itemLabel,
  mediaType = 'Video',
  onClose,
  onSuccess,
}: AddToCollectionModalProps) {
  const { auth, isAdmin } = useAuth()
  const queryClient = useQueryClient()
  const userId = auth?.userId || ''

  const [activeTab, setActiveTab] = useState<ContainerTab>(() => (isAdmin ? 'BoxSet' : 'Playlist'))
  const [newName, setNewName] = useState('')
  const [error, setError] = useState<string | null>(null)

  const effectiveTab: ContainerTab = isAdmin ? activeTab : 'Playlist'

  useEffect(() => {
    if (!isAdmin && activeTab === 'BoxSet') {
      setActiveTab('Playlist')
    }
  }, [isAdmin, activeTab])

  useEffect(() => {
    if (!open) return
    setNewName('')
    setError(null)
  }, [open, effectiveTab])

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
    enabled: Boolean(open && userId && isAdmin),
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
    enabled: Boolean(open && userId),
  })

  const invalidateCollections = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ['collections'] }),
      queryClient.invalidateQueries({ queryKey: ['collectionItems'] }),
    ])
  }

  const addExistingMutation = useMutation({
    mutationFn: async (target: BaseItemDto) => {
      if (itemIds.length === 0) return target
      if (effectiveTab === 'BoxSet') {
        await api.addCollectionItems(target.Id, itemIds)
      } else {
        await api.addPlaylistItems(target.Id, itemIds, userId)
      }
      return target
    },
    onSuccess: async (target) => {
      await invalidateCollections()
      const kind = effectiveTab === 'BoxSet' ? '合集' : '播放列表'
      onSuccess?.(`已将 ${itemIds.length} 项加入${kind}「${target.Name}」`)
      onClose()
    },
    onError: (err) => {
      setError(err instanceof Error ? err.message : '加入失败，请稍后重试')
    },
  })

  const createMutation = useMutation({
    mutationFn: async (name: string) => {
      if (effectiveTab === 'BoxSet') {
        return api.createCollection(name, itemIds)
      }
      return api.createPlaylist(name, userId, itemIds, mediaType)
    },
    onSuccess: async (created) => {
      await invalidateCollections()
      const kind = effectiveTab === 'BoxSet' ? '合集' : '播放列表'
      onSuccess?.(`已新建${kind}「${created.Name}」并加入 ${itemIds.length} 项`)
      onClose()
    },
    onError: (err) => {
      setError(err instanceof Error ? err.message : '新建失败，请稍后重试')
    },
  })

  const list = effectiveTab === 'BoxSet' ? boxSetsData?.Items || [] : playlistsData?.Items || []
  const isLoading = effectiveTab === 'BoxSet' ? boxSetsLoading : playlistsLoading
  const isBusy = addExistingMutation.isPending || createMutation.isPending

  const handleCreateSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    const trimmed = newName.trim()
    if (!trimmed) {
      setError(effectiveTab === 'BoxSet' ? '请输入新合集名称' : '请输入新播放列表名称')
      return
    }
    if (itemIds.length === 0) {
      setError('当前未选择任何媒体项')
      return
    }
    setError(null)
    createMutation.mutate(trimmed)
  }

  return (
    <AdminModal
      open={open}
      title={isAdmin ? '加入合集 / 播放列表' : '加入播放列表'}
      subtitle={`目标媒体：${itemLabel}`}
      maxWidthClass="max-w-lg"
      onClose={onClose}
    >
      <div className="space-y-4">
        <FeedbackBanner message={error ?? undefined} type="error" onClose={() => setError(null)} />

        {/* 合集 / 播放列表切换（仅管理员可操作 BoxSet 合集） */}
        {isAdmin && (
          <div
            role="tablist"
            aria-label="目标容器类型"
            className="grid grid-cols-2 gap-1 rounded-xl border border-white/10 bg-zinc-950/80 p-1"
          >
            <button
              type="button"
              role="tab"
              aria-selected={effectiveTab === 'BoxSet'}
              onClick={() => setActiveTab('BoxSet')}
              className={`inline-flex items-center justify-center gap-1.5 rounded-lg px-3 py-2 text-xs font-medium transition-colors ${
                effectiveTab === 'BoxSet'
                  ? 'bg-amber-500 text-zinc-950 shadow-sm'
                  : 'text-zinc-400 hover:bg-white/[0.05] hover:text-zinc-100'
              }`}
            >
              <Layers className="h-3.5 w-3.5" />
              <span>合集 (BoxSet)</span>
            </button>
            <button
              type="button"
              role="tab"
              aria-selected={effectiveTab === 'Playlist'}
              onClick={() => setActiveTab('Playlist')}
              className={`inline-flex items-center justify-center gap-1.5 rounded-lg px-3 py-2 text-xs font-medium transition-colors ${
                effectiveTab === 'Playlist'
                  ? 'bg-amber-500 text-zinc-950 shadow-sm'
                  : 'text-zinc-400 hover:bg-white/[0.05] hover:text-zinc-100'
              }`}
            >
              <ListVideo className="h-3.5 w-3.5" />
              <span>播放列表 (Playlist)</span>
            </button>
          </div>
        )}

        {/* 选择现有容器 */}
        <div className="space-y-2">
          <p className="text-xs font-medium text-zinc-400">
            选择现有{effectiveTab === 'BoxSet' ? '合集' : '播放列表'}
          </p>
          <div className="max-h-56 space-y-1.5 overflow-y-auto pr-1">
            {isLoading ? (
              <div className="flex h-24 items-center justify-center">
                <Loader2 className="h-5 w-5 animate-spin text-amber-400" />
              </div>
            ) : list.length === 0 ? (
              <div className="rounded-xl border border-dashed border-white/10 bg-white/[0.02] py-6 text-center text-xs text-zinc-500">
                暂无现有{effectiveTab === 'BoxSet' ? '合集' : '播放列表'}，可在下方直接新建
              </div>
            ) : (
              list.map((target) => (
                <button
                  key={target.Id}
                  type="button"
                  disabled={isBusy || itemIds.length === 0}
                  onClick={() => {
                    setError(null)
                    addExistingMutation.mutate(target)
                  }}
                  className="flex w-full items-center justify-between gap-3 rounded-xl border border-white/8 bg-white/[0.03] px-3.5 py-2.5 text-left transition-colors hover:border-amber-400/50 hover:bg-amber-500/10 disabled:opacity-50"
                >
                  <div className="flex min-w-0 items-center gap-2.5">
                    {effectiveTab === 'BoxSet' ? (
                      <Layers className="h-4 w-4 shrink-0 text-amber-400" />
                    ) : (
                      <ListVideo className="h-4 w-4 shrink-0 text-amber-400" />
                    )}
                    <span className="truncate text-sm font-medium text-zinc-100">
                      {target.Name}
                    </span>
                  </div>
                  <span className="shrink-0 text-xs font-medium text-amber-300">
                    {addExistingMutation.isPending && addExistingMutation.variables?.Id === target.Id
                      ? '加入中...'
                      : '加入'}
                  </span>
                </button>
              ))
            )}
          </div>
        </div>

        {/* 新建并加入 */}
        <form onSubmit={handleCreateSubmit} className="space-y-2 border-t border-white/8 pt-4">
          <label htmlFor="new-collection-name" className="block text-xs font-medium text-zinc-400">
            或新建{effectiveTab === 'BoxSet' ? '合集' : '播放列表'}并加入
          </label>
          <div className="flex items-center gap-2">
            <input
              id="new-collection-name"
              type="text"
              value={newName}
              onChange={(e) => {
                setNewName(e.target.value)
                if (error) setError(null)
              }}
              placeholder={
                effectiveTab === 'BoxSet' ? '输入新合集名称...' : '输入新播放列表名称...'
              }
              className="flex-1 rounded-xl border border-white/10 bg-zinc-900/90 px-3.5 py-2 text-sm text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
            />
            <button
              type="submit"
              disabled={isBusy || itemIds.length === 0}
              className="inline-flex shrink-0 items-center gap-1.5 rounded-xl bg-amber-500 px-4 py-2 text-xs font-semibold text-zinc-950 transition-colors hover:bg-amber-400 disabled:opacity-60"
            >
              {createMutation.isPending ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <Plus className="h-3.5 w-3.5" />
              )}
              <span>新建并加入</span>
            </button>
          </div>
        </form>

        <div className="flex items-center justify-between border-t border-white/8 pt-3 text-xs text-zinc-500">
          <span className="inline-flex items-center gap-1.5">
            <FolderPlus className="h-3.5 w-3.5 text-amber-400/80" />
            <span>共 {itemIds.length} 个待添加条目</span>
          </span>
          <button
            type="button"
            onClick={onClose}
            className="rounded-lg border border-white/10 bg-white/[0.04] px-3 py-1.5 text-xs font-medium text-zinc-300 hover:bg-white/10"
          >
            关闭
          </button>
        </div>
      </div>
    </AdminModal>
  )
}
