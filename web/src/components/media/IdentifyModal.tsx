import { useEffect, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { CheckCircle2, Film, Loader2, Search, Sparkles } from 'lucide-react'
import { api } from '../../api/client'
import type { BaseItemDto, RemoteSearchItemType, RemoteSearchResult } from '../../api/types'
import { useI18n } from '../../i18n/I18nContext'
import { AdminModal, FeedbackBanner } from '../../pages/admin/adminUtils'

const SUPPORTED_REMOTE_SEARCH_TYPES = new Set<RemoteSearchItemType>([
  'Movie',
  'Series',
  'BoxSet',
  'MusicAlbum',
  'MusicArtist',
  'MusicVideo',
  'Person',
  'Book',
  'Game',
  'Trailer',
])

export function resolveRemoteSearchItemType(rawType?: string): RemoteSearchItemType {
  if (rawType && SUPPORTED_REMOTE_SEARCH_TYPES.has(rawType as RemoteSearchItemType)) {
    return rawType as RemoteSearchItemType
  }
  return 'Movie'
}

export interface IdentifyModalProps {
  open: boolean
  item: BaseItemDto | null
  onClose: () => void
  onApplied?: (message: string) => void
}

export function IdentifyModal({
  open,
  item,
  onClose,
  onApplied,
}: IdentifyModalProps) {
  const { t } = useI18n()
  const queryClient = useQueryClient()

  const [name, setName] = useState('')
  const [year, setYear] = useState('')
  const [tmdbId, setTmdbId] = useState('')
  const [imdbId, setImdbId] = useState('')
  const [replaceAllImages, setReplaceAllImages] = useState(true)
  const [results, setResults] = useState<RemoteSearchResult[]>([])
  const [searched, setSearched] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!open || !item) return
    setName(item.Name || '')
    setYear(item.ProductionYear != null ? String(item.ProductionYear) : '')
    setTmdbId(item.ProviderIds?.Tmdb || '')
    setImdbId(item.ProviderIds?.Imdb || '')
    setReplaceAllImages(true)
    setResults([])
    setSearched(false)
    setError(null)
  }, [open, item])

  const searchMutation = useMutation({
    mutationFn: async () => {
      if (!item) return []
      const itemType = resolveRemoteSearchItemType(item.Type)
      const providerIds: Record<string, string> = {}
      if (tmdbId.trim()) providerIds.Tmdb = tmdbId.trim()
      if (imdbId.trim()) providerIds.Imdb = imdbId.trim()
      const trimmedYear = year.trim()
      const parsedYear = trimmedYear ? Number(trimmedYear) : undefined
      const validYear =
        parsedYear !== undefined && Number.isInteger(parsedYear) && parsedYear > 1800
          ? parsedYear
          : undefined
      return api.remoteSearch(itemType, {
        Name: name.trim() || undefined,
        Year: validYear,
        ProviderIds: Object.keys(providerIds).length > 0 ? providerIds : undefined,
      })
    },
    onSuccess: (data) => {
      setResults(data || [])
      setSearched(true)
      setError(null)
    },
    onError: (err) => {
      setError(err instanceof Error ? err.message : t('modal.identify.searchError'))
    },
  })

  const applyMutation = useMutation({
    mutationFn: async (candidate: RemoteSearchResult) => {
      if (!item) return
      await api.applyRemoteSearch(item.Id, candidate, replaceAllImages)
      return candidate
    },
    onSuccess: async (candidate) => {
      if (!item) return
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['item'] }),
        queryClient.invalidateQueries({ queryKey: ['item', item.Id] }),
        queryClient.invalidateQueries({ queryKey: ['item-images', item.Id] }),
        queryClient.invalidateQueries({ queryKey: ['libraryItems'] }),
        queryClient.invalidateQueries({ queryKey: ['latestItems'] }),
        queryClient.invalidateQueries({ queryKey: ['admin-metadata-items'] }),
      ])
      onApplied?.(t('modal.identify.applySuccess', { name: candidate?.Name || item.Name }))
      onClose()
    },
    onError: (err) => {
      setError(err instanceof Error ? err.message : t('modal.identify.applyError'))
    },
  })

  const handleSearch = (e: React.FormEvent) => {
    e.preventDefault()
    if (!name.trim() && !tmdbId.trim() && !imdbId.trim()) {
      setError(t('modal.identify.missingCriteria'))
      return
    }
    const trimmedYear = year.trim()
    if (trimmedYear) {
      const parsed = Number(trimmedYear)
      if (!Number.isInteger(parsed) || parsed <= 1800) {
        setError(t('modal.identify.invalidYear'))
        return
      }
    }
    setError(null)
    searchMutation.mutate()
  }

  return (
    <AdminModal
      open={open}
      title={t('modal.identify.title')}
      subtitle={item ? t('modal.identify.subtitle', { name: item.Name }) : undefined}
      maxWidthClass="max-w-3xl"
      onClose={onClose}
    >
      <div className="space-y-5">
        <FeedbackBanner message={error ?? undefined} type="error" onClose={() => setError(null)} />

        <form onSubmit={handleSearch} className="space-y-4">
          <div className="grid gap-3 sm:grid-cols-4">
            <label className="block space-y-1 sm:col-span-2">
              <span className="text-xs font-medium text-zinc-300">
                {t('modal.identify.fieldName')}
              </span>
              <input
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder={t('modal.identify.fieldNamePlaceholder')}
                className="w-full rounded-xl border border-white/10 bg-zinc-900 px-3.5 py-2 text-sm text-zinc-100 outline-none focus:border-amber-400/60"
              />
            </label>
            <label className="block space-y-1">
              <span className="text-xs font-medium text-zinc-300">
                {t('modal.identify.fieldYear')}
              </span>
              <input
                type="number"
                value={year}
                onChange={(e) => setYear(e.target.value)}
                placeholder={t('modal.identify.fieldYearPlaceholder')}
                className="w-full rounded-xl border border-white/10 bg-zinc-900 px-3.5 py-2 text-sm text-zinc-100 outline-none focus:border-amber-400/60"
              />
            </label>
            <label className="block space-y-1">
              <span className="text-xs font-medium text-zinc-300">
                {t('modal.identify.fieldTmdbId')}
              </span>
              <input
                type="text"
                value={tmdbId}
                onChange={(e) => setTmdbId(e.target.value)}
                placeholder={t('modal.identify.fieldTmdbIdPlaceholder')}
                className="w-full rounded-xl border border-white/10 bg-zinc-900 px-3.5 py-2 text-sm text-zinc-100 outline-none focus:border-amber-400/60"
              />
            </label>
          </div>

          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="flex flex-wrap items-center gap-4">
              <label className="flex items-center gap-2 text-xs text-zinc-300">
                <span>{t('modal.identify.fieldImdbIdLabel')}</span>
                <input
                  type="text"
                  value={imdbId}
                  onChange={(e) => setImdbId(e.target.value)}
                  placeholder="tt1375666"
                  className="w-32 rounded-lg border border-white/10 bg-zinc-900 px-2.5 py-1.5 text-xs text-zinc-100 outline-none focus:border-amber-400/60"
                />
              </label>

              <label className="flex cursor-pointer items-center gap-2 text-xs text-zinc-300">
                <input
                  type="checkbox"
                  checked={replaceAllImages}
                  onChange={(e) => setReplaceAllImages(e.target.checked)}
                  className="h-4 w-4 rounded border-white/20 bg-zinc-900 accent-amber-500"
                />
                <span>{t('modal.identify.replaceImages')}</span>
              </label>
            </div>

            <button
              type="submit"
              disabled={searchMutation.isPending}
              className="inline-flex items-center gap-2 rounded-xl bg-amber-500 px-5 py-2 text-sm font-semibold text-zinc-950 shadow-lg shadow-amber-500/20 transition-colors hover:bg-amber-400 disabled:opacity-60"
            >
              {searchMutation.isPending ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <Search className="h-4 w-4" />
              )}
              <span>{t('modal.identify.searchBtn')}</span>
            </button>
          </div>
        </form>

        {/* 候选结果列表 */}
        {searched && (
          <div className="space-y-3 border-t border-white/8 pt-4">
            <p className="text-xs font-medium text-zinc-400">
              {t('modal.identify.resultsCount', { count: results.length })}
            </p>

            {results.length === 0 ? (
              <div className="rounded-2xl border border-dashed border-white/12 bg-white/[0.02] py-10 text-center text-sm text-zinc-400">
                {t('modal.identify.emptyResults')}
              </div>
            ) : (
              <div className="space-y-3">
                {results.map((cand, idx) => (
                  <div
                    key={`${cand.Name}-${cand.ProductionYear ?? idx}-${idx}`}
                    className="flex flex-col gap-4 rounded-2xl border border-white/10 bg-white/[0.025] p-3.5 transition-colors hover:border-amber-400/40 sm:flex-row"
                  >
                    <div className="h-32 w-22 shrink-0 overflow-hidden rounded-xl bg-zinc-900">
                      {cand.ImageUrl ? (
                        <img
                          src={cand.ImageUrl}
                          alt={cand.Name}
                          className="h-full w-full object-cover"
                        />
                      ) : (
                        <div className="flex h-full w-full items-center justify-center text-zinc-600">
                          <Film className="h-6 w-6" />
                        </div>
                      )}
                    </div>

                    <div className="min-w-0 flex-1 space-y-1.5">
                      <div className="flex flex-wrap items-center gap-2">
                        <h4 className="text-sm font-semibold text-zinc-100">{cand.Name}</h4>
                        {cand.ProductionYear && (
                          <span className="rounded-md bg-white/8 px-2 py-0.5 text-[11px] text-zinc-300">
                            {cand.ProductionYear}
                          </span>
                        )}
                        {cand.SearchProviderName && (
                          <span className="inline-flex items-center gap-1 rounded-md bg-amber-500/15 px-2 py-0.5 text-[11px] font-medium text-amber-300">
                            <Sparkles className="h-3 w-3" />
                            {cand.SearchProviderName}
                          </span>
                        )}
                      </div>

                      {cand.Overview && (
                        <p className="line-clamp-2 text-xs leading-relaxed text-zinc-400">
                          {cand.Overview}
                        </p>
                      )}

                      {cand.ProviderIds && Object.keys(cand.ProviderIds).length > 0 && (
                        <div className="flex flex-wrap gap-1.5 pt-0.5">
                          {Object.entries(cand.ProviderIds).map(([k, v]) => (
                            <span
                              key={k}
                              className="rounded border border-white/10 bg-zinc-900 px-2 py-0.5 font-mono text-[10px] text-zinc-400"
                            >
                              {k}: {v}
                            </span>
                          ))}
                        </div>
                      )}
                    </div>

                    <div className="flex shrink-0 items-center sm:self-center">
                      <button
                        type="button"
                        disabled={applyMutation.isPending}
                        onClick={() => applyMutation.mutate(cand)}
                        className="inline-flex items-center gap-1.5 rounded-xl bg-amber-500 px-4 py-2 text-xs font-semibold text-zinc-950 transition-colors hover:bg-amber-400 disabled:opacity-60"
                      >
                        {applyMutation.isPending ? (
                          <Loader2 className="h-3.5 w-3.5 animate-spin" />
                        ) : (
                          <CheckCircle2 className="h-3.5 w-3.5" />
                        )}
                        <span>{t('modal.identify.applyBtn')}</span>
                      </button>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        )}
      </div>
    </AdminModal>
  )
}
