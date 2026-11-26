import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link2, Loader2, Unlink } from 'lucide-react'
import { api } from '../api/client'
import type { UserTraktStatus } from '../api/types'
import { useI18n } from '../i18n/I18nContext'

export function TraktSettings({ userId }: { userId: string }) {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const queryKey = ['user', userId, 'trakt']
  const { data, isLoading, isError, refetch } = useQuery({
    queryKey, queryFn: () => api.getUserTrakt(), enabled: Boolean(userId), retry: false,
  })
  const [clientId, setClientId] = useState('')
  const [token, setToken] = useState('')
  const [scrobble, setScrobble] = useState(true)
  const [watched, setWatched] = useState(true)
  const [feedback, setFeedback] = useState('')
  useEffect(() => {
    setClientId(data?.ClientId || '')
    setToken('')
    setScrobble(data?.ScrobblePlayback ?? true)
    setWatched(data?.SyncWatched ?? true)
  }, [data, userId])

  const applyStatus = (status: UserTraktStatus) => {
    setToken('')
    queryClient.setQueryData(queryKey, status)
  }
  const save = useMutation({
    mutationFn: () => api.updateUserTrakt({
      ClientId: clientId.trim(),
      ...(token.trim() ? { AccessToken: token.trim() } : {}),
      ScrobblePlayback: scrobble, SyncWatched: watched,
    }),
    onSuccess: (status) => {
      applyStatus(status)
      setFeedback(t('trakt.saved'))
    },
  })
  const disconnect = useMutation({
    mutationFn: () => api.disconnectUserTrakt(),
    onSuccess: () => {
      applyStatus({
        Enabled: data?.Enabled ?? false, Configured: false, ClientId: '',
        AccessTokenConfigured: false, ScrobblePlayback: true, SyncWatched: true,
      })
      setFeedback(t('trakt.disconnected'))
    },
  })
  const pending = save.isPending || disconnect.isPending
  const error = save.error || disconnect.error
  const canSave = Boolean(data?.Enabled) && !pending && Boolean(clientId.trim()) &&
    (Boolean(token.trim()) || Boolean(data?.AccessTokenConfigured && clientId.trim() === data.ClientId))
  return (
    <section className="rounded-2xl border border-white/10 bg-zinc-950/70 p-6 backdrop-blur-md">
      <div className="mb-3 flex items-center gap-2">
        <Link2 className="h-4 w-4 text-rose-400" />
        <h2 className="text-base font-semibold text-white">{t('trakt.title')}</h2>
        {data?.Configured && <span className="text-xs text-emerald-400">{t('trakt.configured')}</span>}
      </div>
      <p className="mb-4 text-xs leading-relaxed text-zinc-400">{t('trakt.description')}</p>
      {isLoading ? <Loader2 className="h-5 w-5 animate-spin text-zinc-400" /> :
        isError ? (
          <button type="button" onClick={() => refetch()} className="text-sm text-amber-400">
            {t('trakt.retry')}
          </button>
        ) : (
          <form className="space-y-4" onSubmit={(e) => {
            e.preventDefault()
            if (canSave) { setFeedback(''); save.reset(); disconnect.reset(); save.mutate() }
          }}>
            {!data?.Enabled && <p className="text-xs text-amber-300">{t('trakt.disabled')}</p>}
            <fieldset disabled={!data?.Enabled || pending} className="space-y-3 disabled:opacity-50">
              <label className="block space-y-1.5 text-xs text-zinc-300">
                <span>Client ID</span>
                <input value={clientId} onChange={(e) => setClientId(e.target.value)}
                  autoComplete="off" maxLength={256}
                  className="w-full rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2.5 font-mono text-sm text-zinc-100 outline-none focus:border-rose-400/60" />
              </label>
              <label className="block space-y-1.5 text-xs text-zinc-300">
                <span>Access Token</span>
                <input type="password" autoComplete="new-password" value={token}
                  maxLength={4096} onChange={(e) => setToken(e.target.value)}
                  placeholder={data?.AccessTokenConfigured ? t('trakt.keepToken') : t('trakt.tokenPlaceholder')}
                  className="w-full rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2.5 font-mono text-sm text-zinc-100 outline-none focus:border-rose-400/60" />
              </label>
              <p className="text-xs leading-relaxed text-zinc-500">{t('trakt.credentialsHelp')}</p>
              <label className="flex items-center gap-2 text-sm text-zinc-300">
                <input type="checkbox" checked={scrobble} onChange={(e) => setScrobble(e.target.checked)} />
                {t('trakt.scrobble')}
              </label>
              <label className="flex items-center gap-2 text-sm text-zinc-300">
                <input type="checkbox" checked={watched} onChange={(e) => setWatched(e.target.checked)} />
                {t('trakt.watched')}
              </label>
            </fieldset>
            <div className="flex flex-wrap gap-3">
              <button type="submit" disabled={!canSave}
                className="inline-flex items-center gap-2 rounded-xl bg-rose-500 px-4 py-2 text-sm font-semibold text-white hover:bg-rose-400 disabled:opacity-50">
                {save.isPending && <Loader2 className="h-4 w-4 animate-spin" />}
                {t('trakt.save')}
              </button>
              {data?.Configured && (
                <button type="button" disabled={pending}
                  onClick={() => { setFeedback(''); save.reset(); disconnect.reset(); disconnect.mutate() }}
                  className="inline-flex items-center gap-2 rounded-xl border border-white/10 px-4 py-2 text-sm text-zinc-300 hover:bg-white/5 disabled:opacity-50">
                  <Unlink className="h-4 w-4" />{t('trakt.disconnect')}
                </button>
              )}
            </div>
            {feedback && <p role="status" className="text-xs text-emerald-300">{feedback}</p>}
            {error && <p role="alert" className="text-xs text-red-300">{error.message}</p>}
          </form>
        )}
    </section>
  )
}
