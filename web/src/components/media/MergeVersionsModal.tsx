import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Film, GitMerge, Layers, Loader2, Search } from 'lucide-react'
import { api, getFallbackImageUrl, getImageUrl } from '../../api/client'
import type { BaseItemDto } from '../../api/types'
import { useAuth } from '../../context/AuthContext'
import { useI18n } from '../../i18n/I18nContext'
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
  const { t } = useI18n()
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

  const candidates = (candidatesData?.Items || [])
    .filter((c) => c.Id !== item?.Id)
    .sort((a, b) => {
      if (item?.Type === 'Episode') {
        const aSameSeries = Boolean(item.SeriesId && a.SeriesId === item.SeriesId)
        const bSameSeries = Boolean(item.SeriesId && b.SeriesId === item.SeriesId)
        if (aSameSeries !== bSameSeries) return aSameSeries ? -1 : 1
        const aSameEp =
          a.ParentIndexNumber === item.ParentIndexNumber && a.IndexNumber === item.IndexNumber
        const bSameEp =
          b.ParentIndexNumber === item.ParentIndexNumber && b.IndexNumber === item.IndexNumber
        if (aSameEp !== bSameEp) return aSameEp ? -1 : 1
      }
      return 0
    })

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
        queryClient.invalidateQueries({ queryKey: ['episodes'] }),
        queryClient.invalidateQueries({ queryKey: ['seasons'] }),
        queryClient.invalidateQueries({ queryKey: ['libraryItems'] }),
        queryClient.invalidateQueries({ queryKey: ['latestItems'] }),
        queryClient.invalidateQueries({ queryKey: ['items'] }),
        queryClient.invalidateQueries({ queryKey: ['admin-metadata-items'] }),
      ])
      onMerged?.(t('modal.merge.mergedSuccess', { count, name: item.Name }))
      onClose()
    },
    onError: (err) => {
      setError(err instanceof Error ? err.message : t('modal.merge.mergeError'))
    },
  })

  return (
    <AdminModal
      open={open}
      title={t('modal.merge.title')}
      subtitle={item ? t('modal.merge.subtitle', { name: item.Name }) : undefined}
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
            placeholder={t('modal.merge.searchPlaceholder')}
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
              {t('modal.merge.emptyCandidates')}
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
                  disabled={mergeMutation.isPending}
                  onClick={() => toggleCandidate(cand.Id)}
                  aria-pressed={checked}
                  className={`flex w-full items-center gap-3.5 rounded-xl border p-2.5 text-left transition-all disabled:cursor-not-allowed disabled:opacity-60 ${
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
                      {cand.Type === 'Episode' &&
                        (cand.ParentIndexNumber != null || cand.IndexNumber != null) && (
                          <span className="rounded bg-white/10 px-1.5 py-0.5 font-mono text-[10px] text-amber-300">
                            {cand.ParentIndexNumber != null
                              ? `S${String(cand.ParentIndexNumber).padStart(2, '0')}`
                              : ''}
                            {cand.IndexNumber != null
                              ? `E${String(cand.IndexNumber).padStart(2, '0')}`
                              : ''}
                          </span>
                        )}
                      <p className="truncate text-sm font-medium text-zinc-100">{cand.Name}</p>
                      {cand.ProductionYear && (
                        <span className="text-xs text-zinc-500">{cand.ProductionYear}</span>
                      )}
                      {sourceCount > 1 && (
                        <span className="inline-flex items-center gap-1 rounded bg-amber-500/15 px-1.5 py-0.5 text-[10px] font-medium text-amber-300">
                          <Layers className="h-2.5 w-2.5" />
                          {t('modal.merge.versionCount', { count: sourceCount })}
                        </span>
                      )}
                    </div>
                    {cand.SeriesName && (
                      <p className="mt-0.5 truncate text-xs text-amber-400/80">{cand.SeriesName}</p>
                    )}
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
            {selectedIds.length > 0
              ? t('modal.merge.selectedSummary', {
                  selected: selectedIds.length,
                  total: selectedIds.length + 1,
                })
              : t('modal.merge.selectPrompt')}
          </p>
          <div className="flex items-center gap-2.5">
            <button
              type="button"
              onClick={onClose}
              className="rounded-xl border border-white/10 bg-white/[0.04] px-4 py-2 text-sm font-medium text-zinc-300 transition-colors hover:bg-white/10"
            >
              {t('modal.merge.cancel')}
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
              <span>{t('modal.merge.confirmBtn')}</span>
            </button>
          </div>
        </div>
      </div>
    </AdminModal>
  )
}
