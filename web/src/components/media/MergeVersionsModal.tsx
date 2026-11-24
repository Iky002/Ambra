import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Film, GitMerge, Layers, Loader2, Search } from 'lucide-react'
import { api, getFallbackImageUrl, getImageUrl } from '../../api/client'
import type { BaseItemDto } from '../../api/types'
import { useAuth } from '../../context/AuthContext'
import { AdminModal, FeedbackBanner } from '../../pages/admin/adminUtils'

export interface MergeVersionsModalProps {
  open: boolean
  item: BaseItemDto | null
  onClose: () => void
  onMerged?: (message: string) => void
}

export function MergeVersionsModal({
  open,
  item,
  onClose,
  onMerged,
}: MergeVersionsModalProps) {
  const { auth } = useAuth()
  const queryClient = useQueryClient()
  const userId = auth?.userId || ''

  const [searchTerm, setSearchTerm] = useState('')
  const [selectedIds, setSelectedIds] = useState<string[]>([])
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!open || !item) return
    setSearchTerm('')
    setSelectedIds([])
    setError(null)
  }, [open, item])

  const { data: candidatesData, isLoading, isFetching } = useQuery({
    queryKey: ['mergeCandidates', userId, item?.Id, item?.Type, searchTerm.trim()],
    queryFn: () =>
      api.getItems(userId, {
        searchTerm: searchTerm.trim() || undefined,
        includeItemTypes: item?.Type || 'Movie',
        sortBy: 'SortName',
        sortOrder: 'Ascending',
        limit: 20,
      }),
    enabled: Boolean(open && item && userId),
    placeholderData: (prev) => prev,
  })

  const candidates = (candidatesData?.Items || []).filter((c) => c.Id !== item?.Id)

  const toggleCandidate = (candidateId: string) => {
    setSelectedIds((prev) =>
      prev.includes(candidateId) ? prev.filter((id) => id !== candidateId) : [...prev, candidateId],
    )
  }

  const mergeMutation = useMutation({
    mutationFn: async () => {
      if (!item || selectedIds.length === 0) return
      await api.mergeItemVersions([item.Id, ...selectedIds])
    },
    onSuccess: async () => {
      if (!item) return
      const count = selectedIds.length + 1
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['item'] }),
        queryClient.invalidateQueries({ queryKey: ['libraryItems'] }),
        queryClient.invalidateQueries({ queryKey: ['latestItems'] }),
        queryClient.invalidateQueries({ queryKey: ['items'] }),
        queryClient.invalidateQueries({ queryKey: ['admin-metadata-items'] }),
      ])
      onMerged?.(`已将 ${count} 个版本合并到「${item.Name}」`)
      onClose()
    },
    onError: (err) => {
      setError(err instanceof Error ? err.message : '合并多版本失败')
    },
  })

  return (
    <AdminModal
      open={open}
      title="合并为多版本"
      subtitle={
        item
          ? `选择要合并到「${item.Name}」的其他同类型媒体条目（合并后保留当前条目的主元数据）`
          : undefined
      }
      maxWidthClass="max-w-2xl"
      onClose={onClose}
    >
      <div className="space-y-4">
        <FeedbackBanner message={error ?? undefined} type="error" onClose={() => setError(null)} />

        {/* 搜索框 */}
        <div className="relative">
          <Search className="pointer-events-none absolute left-3.5 top-1/2 h-4 w-4 -translate-y-1/2 text-zinc-500" />
          <input
            type="text"
            value={searchTerm}
            onChange={(e) => setSearchTerm(e.target.value)}
            placeholder="搜索片名、原名或年份，查找要合并的其他版本..."
            className="w-full rounded-xl border border-white/10 bg-zinc-900 py-2.5 pl-10 pr-4 text-sm text-zinc-100 outline-none focus:border-amber-400/60"
          />
          {isFetching && !isLoading && (
            <Loader2 className="absolute right-3.5 top-1/2 h-4 w-4 -translate-y-1/2 animate-spin text-amber-400" />
          )}
        </div>

        {/* 候选列表 */}
        <div className="max-h-80 space-y-2 overflow-y-auto pr-1">
          {isLoading ? (
            <div className="flex h-36 items-center justify-center">
              <Loader2 className="h-6 w-6 animate-spin text-amber-400" />
            </div>
          ) : candidates.length === 0 ? (
            <div className="rounded-xl border border-dashed border-white/10 bg-white/[0.02] py-10 text-center text-xs text-zinc-400">
              未找到可合并的候选条目，请尝试更换搜索关键词
            </div>
          ) : (
            candidates.map((cand) => {
              const checked = selectedIds.includes(cand.Id)
              const poster = cand.ImageTags?.Primary
                ? getImageUrl(cand.Id, 'Primary', { maxWidth: 120, tag: cand.ImageTags.Primary })
                : null
              const fallback = getFallbackImageUrl(cand.Id, 'Primary')
              const sourceCount = cand.MediaSources?.length || 0

              return (
                <button
                  key={cand.Id}
                  type="button"
                  onClick={() => toggleCandidate(cand.Id)}
                  aria-pressed={checked}
                  className={`flex w-full items-center gap-3.5 rounded-xl border p-2.5 text-left transition-all ${
                    checked
                      ? 'border-amber-400/70 bg-amber-500/10 ring-1 ring-amber-400/30'
                      : 'border-white/8 bg-white/[0.025] hover:border-white/20 hover:bg-white/[0.05]'
                  }`}
                >
                  <div
                    className={`flex h-5 w-5 shrink-0 items-center justify-center rounded-md border transition-colors ${
                      checked
                        ? 'border-amber-400 bg-amber-400 text-zinc-950'
                        : 'border-white/25 bg-zinc-900 text-transparent'
                    }`}
                  >
                    <Check className="h-3.5 w-3.5 stroke-[3]" />
                  </div>

                  <div className="h-14 w-10 shrink-0 overflow-hidden rounded-lg bg-zinc-900 ring-1 ring-white/10">
                    {poster ? (
                      <img
                        src={poster}
                        alt={cand.Name}
                        loading="lazy"
                        onError={(e) => {
                          if (fallback && e.currentTarget.src !== fallback) {
                            e.currentTarget.src = fallback
                          }
                        }}
                        className="h-full w-full object-cover"
                      />
                    ) : (
                      <div className="flex h-full w-full items-center justify-center text-zinc-600">
                        <Film className="h-4 w-4" />
                      </div>
                    )}
                  </div>

                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <p className="truncate text-sm font-medium text-zinc-100">{cand.Name}</p>
                      {cand.ProductionYear && (
                        <span className="text-xs text-zinc-500">{cand.ProductionYear}</span>
                      )}
                      {sourceCount > 1 && (
                        <span className="inline-flex items-center gap-1 rounded bg-amber-500/15 px-1.5 py-0.5 text-[10px] font-medium text-amber-300">
                          <Layers className="h-2.5 w-2.5" />
                          {sourceCount} 版本
                        </span>
                      )}
                    </div>
                    {cand.OriginalTitle && cand.OriginalTitle !== cand.Name && (
                      <p className="mt-0.5 truncate text-xs text-zinc-500">{cand.OriginalTitle}</p>
                    )}
                    {cand.MediaSources?.[0]?.Name && (
                      <p className="mt-0.5 truncate text-[11px] text-zinc-400">
                        {cand.MediaSources[0].Name}
                      </p>
                    )}
                  </div>
                </button>
              )
            })
          )}
        </div>

        {/* 底部操作栏 */}
        <div className="flex flex-wrap items-center justify-between gap-3 border-t border-white/8 pt-4">
          <p className="text-xs text-zinc-400">
            {selectedIds.length > 0 ? (
              <>
                已选择 <span className="font-semibold text-amber-300">{selectedIds.length}</span>{' '}
                个候选条目，合并后共 {selectedIds.length + 1} 个条目归并
              </>
            ) : (
              '请勾选至少 1 个候选条目进行合并'
            )}
          </p>
          <div className="flex items-center gap-2.5">
            <button
              type="button"
              onClick={onClose}
              className="rounded-xl border border-white/10 bg-white/[0.04] px-4 py-2 text-sm font-medium text-zinc-300 transition-colors hover:bg-white/10"
            >
              取消
            </button>
            <button
              type="button"
              disabled={selectedIds.length === 0 || mergeMutation.isPending}
              onClick={() => mergeMutation.mutate()}
              className="inline-flex items-center gap-2 rounded-xl bg-amber-500 px-5 py-2 text-sm font-semibold text-zinc-950 shadow-lg shadow-amber-500/20 transition-colors hover:bg-amber-400 disabled:opacity-50"
            >
              {mergeMutation.isPending ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <GitMerge className="h-4 w-4" />
              )}
              <span>确认合并</span>
            </button>
          </div>
        </div>
      </div>
    </AdminModal>
  )
}
