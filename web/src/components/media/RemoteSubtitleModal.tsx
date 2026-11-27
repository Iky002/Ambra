import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { CheckCircle2, Download, Globe, Loader2, Search, Star, Subtitles, X } from 'lucide-react'
import { api } from '../../api/client'
import type { RemoteSubtitleInfo } from '../../api/types'
import { useI18n } from '../../i18n/I18nContext'

interface RemoteSubtitleModalProps {
  open: boolean
  itemId: string
  itemName?: string
  initialLanguage?: string
  onClose: () => void
  onDownloaded?: (message: string) => void
}

const SUBTITLE_LANGUAGES: Array<{ code: string; labelKey: string }> = [
  { code: 'chi', labelKey: 'modal.subtitles.langChi' },
  { code: 'zho', labelKey: 'modal.subtitles.langZho' },
  { code: 'eng', labelKey: 'modal.subtitles.langEng' },
  { code: 'jpn', labelKey: 'modal.subtitles.langJpn' },
  { code: 'kor', labelKey: 'modal.subtitles.langKor' },
  { code: 'fre', labelKey: 'modal.subtitles.langFre' },
  { code: 'ger', labelKey: 'modal.subtitles.langGer' },
  { code: 'spa', labelKey: 'modal.subtitles.langSpa' },
]

export function RemoteSubtitleModal({
  open,
  itemId,
  itemName,
  initialLanguage = 'chi',
  onClose,
  onDownloaded,
}: RemoteSubtitleModalProps) {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const [language, setLanguage] = useState(initialLanguage)
  const [searchLanguage, setSearchLanguage] = useState(initialLanguage)
  const [downloadingId, setDownloadingId] = useState<string | null>(null)
  const [downloadedIds, setDownloadedIds] = useState<Set<string>>(new Set())
  const [feedback, setFeedback] = useState<{ type: 'success' | 'error'; text: string } | null>(
    null,
  )

  useEffect(() => {
    setDownloadedIds(new Set())
    setLanguage(initialLanguage)
    setSearchLanguage(initialLanguage)
    setDownloadingId(null)
    setFeedback(null)
  }, [itemId, initialLanguage])

  useEffect(() => {
    if (open) {
      setFeedback(null)
      setDownloadingId(null)
    }
  }, [open])

  const {
    data: results = [],
    isLoading,
    isFetching,
    isError,
    error,
    refetch,
  } = useQuery({
    queryKey: ['remoteSubtitles', itemId, searchLanguage],
    queryFn: () => api.searchRemoteSubtitles(itemId, searchLanguage),
    enabled: Boolean(open && itemId && searchLanguage),
    staleTime: 30_000,
  })

  const downloadMutation = useMutation({
    mutationFn: (sub: RemoteSubtitleInfo) => api.downloadRemoteSubtitle(itemId, sub.Id),
    onMutate: (sub) => {
      setDownloadingId(sub.Id)
      setFeedback(null)
    },
    onSuccess: async (_data, sub) => {
      setDownloadedIds((prev) => {
        const next = new Set(prev)
        next.add(sub.Id)
        return next
      })
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['item'] }),
        queryClient.invalidateQueries({ queryKey: ['playbackInfo'] }),
      ])
      const msg = t('modal.subtitles.downloadSuccess', { name: sub.Name })
      setFeedback({ type: 'success', text: msg })
      onDownloaded?.(msg)
    },
    onError: (err) => {
      setFeedback({
        type: 'error',
        text: err instanceof Error ? err.message : t('modal.subtitles.downloadError'),
      })
    },
    onSettled: () => {
      setDownloadingId(null)
    },
  })

  if (!open) return null

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label={t('modal.subtitles.title')}
      data-testid="remote-subtitle-modal"
      onClick={(e) => {
        e.stopPropagation()
        onClose()
      }}
      onKeyDown={(e) => {
        e.stopPropagation()
        if (e.key === 'Escape') {
          onClose()
        }
      }}
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/75 p-4 backdrop-blur-sm"
    >
      <div
        onClick={(e) => e.stopPropagation()}
        className="flex max-h-[85vh] w-full max-w-2xl flex-col overflow-hidden rounded-2xl border border-white/15 bg-zinc-950 text-zinc-100 shadow-2xl"
      >
        <div className="flex items-center justify-between gap-3 border-b border-white/10 px-5 py-4">
          <div className="min-w-0">
            <h2 className="flex items-center gap-2 text-base font-semibold text-white">
              <Subtitles className="h-4 w-4 text-amber-400" />
              <span>{t('modal.subtitles.title')}</span>
            </h2>
            {itemName && (
              <p className="mt-0.5 truncate text-xs text-zinc-400">
                {t('modal.subtitles.subtitle', { name: itemName })}
              </p>
            )}
          </div>
          <button
            type="button"
            aria-label={t('modal.subtitles.closeAria')}
            onClick={onClose}
            className="rounded-lg p-1.5 text-zinc-400 transition-colors hover:bg-white/10 hover:text-white"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="space-y-4 overflow-y-auto p-5">
          <form
            onSubmit={(e) => {
              e.preventDefault()
              if (searchLanguage === language) {
                void refetch()
              } else {
                setSearchLanguage(language)
              }
            }}
            className="flex flex-wrap items-end gap-3"
          >
            <label className="min-w-[180px] flex-1 space-y-1.5">
              <span className="flex items-center gap-1.5 text-xs font-medium text-zinc-300">
                <Globe className="h-3.5 w-3.5 text-amber-400" />
                <span>{t('modal.subtitles.targetLanguage')}</span>
              </span>
              <select
                aria-label={t('modal.subtitles.selectLanguageAria')}
                value={language}
                onChange={(e) => setLanguage(e.target.value)}
                className="w-full rounded-xl border border-white/10 bg-zinc-900 px-3 py-2 text-xs text-zinc-100 outline-none focus:border-amber-400/50"
              >
                {SUBTITLE_LANGUAGES.map((lang) => (
                  <option key={lang.code} value={lang.code}>
                    {t(lang.labelKey)}
                  </option>
                ))}
              </select>
            </label>

            <button
              type="submit"
              disabled={isFetching}
              className="inline-flex items-center gap-1.5 rounded-xl bg-amber-500 px-4 py-2 text-xs font-semibold text-zinc-950 transition-colors hover:bg-amber-400 disabled:opacity-50"
            >
              {isFetching ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <Search className="h-3.5 w-3.5" />
              )}
              <span>{t('modal.subtitles.searchBtn')}</span>
            </button>
          </form>

          {feedback && (
            <div
              role="status"
              className={`rounded-xl border px-3.5 py-2.5 text-xs ${
                feedback.type === 'success'
                  ? 'border-emerald-500/35 bg-emerald-500/12 text-emerald-200'
                  : 'border-red-500/35 bg-red-500/12 text-red-200'
              }`}
            >
              {feedback.text}
            </div>
          )}

          {isLoading ? (
            <div className="flex h-40 items-center justify-center">
              <Loader2 className="h-6 w-6 animate-spin text-amber-400" />
            </div>
          ) : isError ? (
            <div className="rounded-xl border border-red-500/25 bg-red-500/10 p-4 text-xs text-red-200">
              {error instanceof Error ? error.message : t('modal.subtitles.searchError')}
            </div>
          ) : results.length === 0 ? (
            <div className="rounded-xl border border-dashed border-white/10 py-10 text-center text-xs text-zinc-500">
              {t('modal.subtitles.emptyResults')}
            </div>
          ) : (
            <div className="divide-y divide-white/8 overflow-hidden rounded-xl border border-white/10 bg-white/[0.02]">
              {results.map((sub) => {
                const isDownloading = downloadingId === sub.Id
                const isDownloaded = downloadedIds.has(sub.Id)
                return (
                  <div
                    key={sub.Id}
                    className="flex flex-wrap items-center justify-between gap-3 px-4 py-3 text-xs hover:bg-white/[0.03]"
                  >
                    <div className="min-w-0 flex-1 space-y-1">
                      <div className="flex flex-wrap items-center gap-1.5">
                        <span className="truncate font-semibold text-zinc-100">{sub.Name}</span>
                        {sub.Format && (
                          <span className="rounded bg-amber-500/15 px-1.5 py-0.5 font-mono text-[10px] font-semibold text-amber-300 uppercase">
                            {sub.Format}
                          </span>
                        )}
                        {sub.ProviderName && (
                          <span className="rounded bg-white/10 px-1.5 py-0.5 text-[10px] text-zinc-300">
                            {sub.ProviderName}
                          </span>
                        )}
                        {sub.IsHashMatch && (
                          <span className="rounded bg-emerald-500/15 px-1.5 py-0.5 text-[10px] font-medium text-emerald-300">
                            {t('modal.subtitles.hashMatch')}
                          </span>
                        )}
                      </div>
                      <div className="flex flex-wrap items-center gap-3 text-[11px] text-zinc-400">
                        {(sub.ThreeLetterISOLanguageName || sub.Language) && (
                          <span>
                            {t('modal.subtitles.metaLanguage', {
                              lang: sub.ThreeLetterISOLanguageName || sub.Language || '',
                            })}
                          </span>
                        )}
                        {typeof sub.CommunityRating === 'number' && sub.CommunityRating > 0 && (
                          <span className="inline-flex items-center gap-0.5 text-amber-300">
                            <Star className="h-3 w-3 fill-amber-400 text-amber-400" />
                            {sub.CommunityRating.toFixed(1)}
                          </span>
                        )}
                        {typeof sub.DownloadCount === 'number' && (
                          <span>
                            {t('modal.subtitles.metaDownloads', { count: sub.DownloadCount })}
                          </span>
                        )}
                        {sub.Comment && <span className="truncate text-zinc-500">{sub.Comment}</span>}
                      </div>
                    </div>

                    <button
                      type="button"
                      disabled={isDownloading}
                      onClick={() => downloadMutation.mutate(sub)}
                      className={`inline-flex shrink-0 items-center gap-1.5 rounded-xl px-3 py-1.5 text-xs font-semibold transition-colors disabled:opacity-50 ${
                        isDownloaded
                          ? 'border border-emerald-500/40 bg-emerald-500/15 text-emerald-300'
                          : 'bg-amber-500 text-zinc-950 hover:bg-amber-400'
                      }`}
                    >
                      {isDownloading ? (
                        <Loader2 className="h-3.5 w-3.5 animate-spin" />
                      ) : isDownloaded ? (
                        <CheckCircle2 className="h-3.5 w-3.5" />
                      ) : (
                        <Download className="h-3.5 w-3.5" />
                      )}
                      <span>
                        {isDownloaded
                          ? t('modal.subtitles.downloadedBtn')
                          : t('modal.subtitles.downloadBtn')}
                      </span>
                    </button>
                  </div>
                )
              })}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
