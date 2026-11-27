import { useMemo, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  AlertTriangle,
  CalendarDays,
  ChevronDown,
  ChevronRight,
  Link2,
  Loader2,
  Play,
  Plus,
  Radar,
  Radio,
  RefreshCw,
  Save,
  Trash2,
  Tv,
} from 'lucide-react'
import { Link } from 'react-router-dom'
import { ApiError, api, embyFetch } from '../../api/client'
import type { LiveTvTunerHostInfo, NameIdPair } from '../../api/types'
import { isDemoMode } from '../../demo/demoMode'
import { useI18n } from '../../i18n/I18nContext'
import { AdminModal, adminLabel, FeedbackBanner } from './adminUtils'

interface DiscoveredTunerHost {
  Id?: string
  Url: string
  Type?: string
  DeviceId?: string
  FriendlyName?: string
  TunerCount?: number
  ImportFavoritesOnly?: boolean
}

interface ChannelMappingOptionsRaw {
  TunerChannels?: Array<{
    Id: string
    Name: string
    Number?: string
    ProviderChannelId?: string
  }>
  ProviderChannels?: NameIdPair[]
  Mappings?: Array<{
    Name?: string
    Value?: string
    TunerChannelId?: string
    ProviderChannelId?: string
  }>
  ProviderName?: string
}

export function LiveTvTab() {
  const { t, locale } = useI18n()
  const queryClient = useQueryClient()
  const [feedback, setFeedback] = useState<{ type: 'success' | 'error'; message: string } | null>(
    null,
  )

  const [tunerType, setTunerType] = useState<'m3u' | 'hdhomerun'>('m3u')
  const [tunerName, setTunerName] = useState('')
  const [tunerUrl, setTunerUrl] = useState('')
  const [addingTuner, setAddingTuner] = useState(false)
  const [deletingTunerId, setDeletingTunerId] = useState<string | null>(null)
  const [confirmDeleteTuner, setConfirmDeleteTuner] = useState<LiveTvTunerHostInfo | null>(null)

  const [discoveringTuners, setDiscoveringTuners] = useState(false)
  const [discoveredTuners, setDiscoveredTuners] = useState<DiscoveredTunerHost[] | null>(null)

  const [epgPath, setEpgPath] = useState('')
  const [addingEpg, setAddingEpg] = useState(false)
  const [deletingEpgId, setDeletingEpgId] = useState<string | null>(null)

  const [mappingExpanded, setMappingExpanded] = useState(false)
  const [selectedProviderId, setSelectedProviderId] = useState('')
  const [draftMappings, setDraftMappings] = useState<Record<string, string>>({})
  const [savingMappingChannelId, setSavingMappingChannelId] = useState<string | null>(null)

  const {
    data: tunerHosts = [],
    isLoading: tunersLoading,
    isFetching: tunersFetching,
    isError: tunersError,
    error: tunersErr,
    refetch: refetchTuners,
  } = useQuery({
    queryKey: ['admin', 'liveTv', 'tunerHosts'],
    queryFn: () => api.getTunerHosts(),
  })

  const {
    data: listingProviders = [],
    isLoading: epgLoading,
    isError: epgError,
    error: epgErr,
    refetch: refetchEpg,
  } = useQuery({
    queryKey: ['admin', 'liveTv', 'listingProviders'],
    queryFn: () => api.getListingProviders(),
  })

  const {
    data: channelsData,
    isLoading: channelsLoading,
    isFetching: channelsFetching,
    isError: channelsError,
    error: channelsErr,
    refetch: refetchChannels,
  } = useQuery({
    queryKey: ['admin', 'liveTv', 'channels'],
    queryFn: () => api.getLiveTvChannels(100),
  })

  const channels = useMemo(() => channelsData?.Items ?? [], [channelsData])
  const totalChannels = channelsData?.TotalRecordCount ?? channels.length

  const activeProviderId = useMemo(() => {
    if (selectedProviderId && listingProviders.some((p) => p.Id === selectedProviderId)) {
      return selectedProviderId
    }
    return listingProviders[0]?.Id || ''
  }, [selectedProviderId, listingProviders])

  const {
    data: mappingOptions,
    isLoading: mappingLoading,
    isFetching: mappingFetching,
    isError: mappingError,
    error: mappingErr,
    refetch: refetchMappingOptions,
  } = useQuery({
    queryKey: ['admin', 'liveTv', 'channelMappingOptions', activeProviderId],
    enabled: mappingExpanded,
    queryFn: async (): Promise<ChannelMappingOptionsRaw> => {
      const qs = activeProviderId ? `?ProviderId=${encodeURIComponent(activeProviderId)}` : ''
      try {
        const res = await embyFetch<ChannelMappingOptionsRaw>(
          `/emby/LiveTv/ChannelMappingOptions${qs}`,
        )
        return res ?? {}
      } catch (err) {
        if (isDemoMode() && err instanceof ApiError && err.status === 404) {
          const fallbackMappings = await embyFetch<
            Array<{ TunerChannelId?: string; ProviderChannelId?: string }>
          >('/emby/LiveTv/ChannelMappings').catch(() => [])
          return {
            TunerChannels: channels.map((ch) => ({
              Id: ch.Id,
              Name: ch.Name,
              Number: ch.Number,
            })),
            ProviderChannels: channels.map((ch) => ({
              Id: ch.Id,
              Name: ch.Name,
            })),
            Mappings: Array.isArray(fallbackMappings) ? fallbackMappings : [],
          }
        }
        throw err
      }
    },
  })

  const serverMappingsByTunerId = useMemo(() => {
    const map: Record<string, string> = {}
    for (const tc of mappingOptions?.TunerChannels || []) {
      if (tc.Id && tc.ProviderChannelId) {
        map[tc.Id] = tc.ProviderChannelId
      }
    }
    for (const m of mappingOptions?.Mappings || []) {
      const tunerId = m.TunerChannelId || m.Name || ''
      const provId = m.ProviderChannelId ?? m.Value ?? ''
      if (tunerId) {
        map[tunerId] = provId
      }
    }
    return map
  }, [mappingOptions])

  const mergedMappingChannels = useMemo(() => {
    const seen = new Set<string>()
    const list: Array<{ Id: string; Name: string; Number?: string }> = []
    for (const tc of mappingOptions?.TunerChannels || []) {
      if (!tc.Id || seen.has(tc.Id)) continue
      seen.add(tc.Id)
      list.push({ Id: tc.Id, Name: tc.Name || tc.Id, Number: tc.Number })
    }
    for (const ch of channels) {
      if (!ch.Id || seen.has(ch.Id)) continue
      seen.add(ch.Id)
      list.push({ Id: ch.Id, Name: ch.Name || ch.Id, Number: ch.Number })
    }
    return list
  }, [mappingOptions, channels])

  const providerChannels = useMemo(
    () => mappingOptions?.ProviderChannels ?? [],
    [mappingOptions],
  )

  const handleRefreshAll = () => {
    void refetchTuners()
    void refetchEpg()
    void refetchChannels()
    if (mappingExpanded) {
      void refetchMappingOptions()
    }
  }

  const handleDiscoverTuners = async () => {
    setDiscoveringTuners(true)
    setFeedback(null)
    try {
      const found = await embyFetch<DiscoveredTunerHost[]>('/emby/LiveTv/Tuners/Discover')
      const list = Array.isArray(found) ? found : []
      setDiscoveredTuners(list)
      if (list.length === 0) {
        setFeedback({
          type: 'success',
          message: adminLabel(
            t,
            locale,
            'admin.livetv.discoverEmpty',
            '局域网扫描完成，暂未发现 HDHomeRun 调谐器设备',
            'LAN scan finished: no HDHomeRun tuners discovered',
          ),
        })
      }
    } catch (err) {
      if (isDemoMode() && err instanceof ApiError && err.status === 404) {
        setDiscoveredTuners([])
        return
      }
      setFeedback({
        type: 'error',
        message:
          err instanceof Error
            ? err.message
            : adminLabel(
                t,
                locale,
                'admin.livetv.discoverFailed',
                '扫描局域网调谐器失败',
                'Failed to discover LAN tuners',
              ),
      })
    } finally {
      setDiscoveringTuners(false)
    }
  }

  const handleUseDiscoveredTuner = (device: DiscoveredTunerHost) => {
    const nextType = device.Type?.toLowerCase() === 'm3u' ? 'm3u' : 'hdhomerun'
    setTunerType(nextType)
    setTunerName(
      device.FriendlyName ||
        (device.DeviceId ? `HDHomeRun (${device.DeviceId})` : 'HDHomeRun'),
    )
    setTunerUrl(device.Url || '')
  }

  const handleAddTuner = async (e: React.FormEvent) => {
    e.preventDefault()
    const trimmedUrl = tunerUrl.trim()
    if (!trimmedUrl) {
      setFeedback({ type: 'error', message: t('admin.livetv.errTunerUrlRequired') })
      return
    }
    setAddingTuner(true)
    setFeedback(null)
    try {
      const defaultName =
        tunerType === 'hdhomerun' ? 'HDHomeRun' : t('admin.livetv.defaultTunerName')
      await api.addTunerHost({
        Type: tunerType,
        FriendlyName: tunerName.trim() || defaultName,
        Url: trimmedUrl,
      })
      setTunerName('')
      setTunerUrl('')
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['admin', 'liveTv', 'tunerHosts'] }),
        queryClient.invalidateQueries({ queryKey: ['admin', 'liveTv', 'channels'] }),
        queryClient.invalidateQueries({ queryKey: ['admin', 'liveTv', 'channelMappingOptions'] }),
        queryClient.invalidateQueries({ queryKey: ['liveTvInfo'] }),
        queryClient.invalidateQueries({ queryKey: ['liveTv'] }),
      ])
      setFeedback({ type: 'success', message: t('admin.livetv.tunerAddedSuccess') })
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('admin.livetv.tunerAddFailed'),
      })
    } finally {
      setAddingTuner(false)
    }
  }

  const handleDeleteTuner = async (id: string, name?: string) => {
    setDeletingTunerId(id)
    setFeedback(null)
    try {
      await api.deleteTunerHost(id)
      setConfirmDeleteTuner(null)
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['admin', 'liveTv', 'tunerHosts'] }),
        queryClient.invalidateQueries({ queryKey: ['admin', 'liveTv', 'channels'] }),
        queryClient.invalidateQueries({ queryKey: ['admin', 'liveTv', 'channelMappingOptions'] }),
        queryClient.invalidateQueries({ queryKey: ['liveTvInfo'] }),
        queryClient.invalidateQueries({ queryKey: ['liveTv'] }),
      ])
      setFeedback({
        type: 'success',
        message: t('admin.livetv.tunerDeletedSuccess', { name: name || id }),
      })
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('admin.livetv.tunerDeleteFailed'),
      })
    } finally {
      setDeletingTunerId(null)
    }
  }

  const handleSaveChannelMapping = async (tunerChannelId: string) => {
    const providerChannelId = (
      draftMappings[tunerChannelId] ??
      serverMappingsByTunerId[tunerChannelId] ??
      ''
    ).trim()
    setSavingMappingChannelId(tunerChannelId)
    setFeedback(null)
    try {
      await embyFetch<{ TunerChannelId?: string; ProviderChannelId?: string }>(
        '/emby/LiveTv/ChannelMappings',
        {
          method: 'POST',
          body: JSON.stringify({
            ProviderId: activeProviderId,
            TunerChannelId: tunerChannelId,
            ProviderChannelId: providerChannelId,
          }),
        },
      )
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['admin', 'liveTv', 'channelMappingOptions'] }),
        queryClient.invalidateQueries({ queryKey: ['admin', 'liveTv', 'channels'] }),
        queryClient.invalidateQueries({ queryKey: ['liveTv'] }),
      ])
      setFeedback({
        type: 'success',
        message: adminLabel(
          t,
          locale,
          'admin.livetv.mappingSavedSuccess',
          '频道 EPG 映射已保存',
          'Channel EPG mapping saved',
        ),
      })
    } catch (err) {
      setFeedback({
        type: 'error',
        message:
          err instanceof Error
            ? err.message
            : adminLabel(
                t,
                locale,
                'admin.livetv.mappingSaveFailed',
                '保存频道 EPG 映射失败',
                'Failed to save channel EPG mapping',
              ),
      })
    } finally {
      setSavingMappingChannelId(null)
    }
  }

  const handleAddEpg = async (e: React.FormEvent) => {
    e.preventDefault()
    const trimmedPath = epgPath.trim()
    if (!trimmedPath) {
      setFeedback({ type: 'error', message: t('admin.livetv.errEpgPathRequired') })
      return
    }
    setAddingEpg(true)
    setFeedback(null)
    try {
      await api.addListingProvider({
        Type: 'xmltv',
        Path: trimmedPath,
      })
      setEpgPath('')
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['admin', 'liveTv', 'listingProviders'] }),
        queryClient.invalidateQueries({ queryKey: ['admin', 'liveTv', 'channelMappingOptions'] }),
      ])
      setFeedback({ type: 'success', message: t('admin.livetv.epgAddedSuccess') })
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('admin.livetv.epgAddFailed'),
      })
    } finally {
      setAddingEpg(false)
    }
  }

  const handleDeleteEpg = async (id: string) => {
    setDeletingEpgId(id)
    setFeedback(null)
    try {
      await api.deleteListingProvider(id)
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['admin', 'liveTv', 'listingProviders'] }),
        queryClient.invalidateQueries({ queryKey: ['admin', 'liveTv', 'channelMappingOptions'] }),
      ])
      setFeedback({ type: 'success', message: t('admin.livetv.epgDeletedSuccess') })
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('admin.livetv.epgDeleteFailed'),
      })
    } finally {
      setDeletingEpgId(null)
    }
  }

  return (
    <div className="space-y-8">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <h2 className="flex items-center gap-2 text-base font-semibold text-zinc-100">
            <Radio className="h-4 w-4 text-amber-400" />
            <span>{t('admin.livetv.title')}</span>
          </h2>
          <p className="mt-1 text-xs text-zinc-400">{t('admin.livetv.subtitle')}</p>
        </div>

        <button
          type="button"
          onClick={handleRefreshAll}
          className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/[0.04] px-3 py-1.5 text-xs font-medium text-zinc-300 transition-colors hover:border-amber-400/40 hover:text-amber-300"
        >
          <RefreshCw
            className={`h-3.5 w-3.5 ${tunersFetching || channelsFetching ? 'animate-spin' : ''}`}
          />
          <span>{t('common.refresh')}</span>
        </button>
      </div>

      <FeedbackBanner
        message={feedback?.message}
        type={feedback?.type}
        onClose={() => setFeedback(null)}
      />

      {/* 1. M3U / HDHomeRun Tuner Hosts */}
      <section className="space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h3 className="flex items-center gap-2 text-sm font-semibold text-zinc-200">
            <Tv className="h-4 w-4 text-amber-400" />
            <span>{t('admin.livetv.tunerSectionTitle')}</span>
            <span className="rounded-md bg-white/8 px-2 py-0.5 text-xs font-medium text-zinc-300">
              {tunerHosts.length}
            </span>
          </h3>

          <button
            type="button"
            data-testid="discover-lan-tuners-btn"
            disabled={discoveringTuners}
            onClick={() => void handleDiscoverTuners()}
            className="inline-flex items-center gap-1.5 rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-1.5 text-xs font-medium text-amber-300 transition-colors hover:bg-amber-500/20 disabled:opacity-60"
          >
            {discoveringTuners ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <Radar className="h-3.5 w-3.5" />
            )}
            <span>
              {adminLabel(
                t,
                locale,
                'admin.livetv.discoverTunersBtn',
                '扫描局域网调谐器',
                'Discover LAN Tuners',
              )}
            </span>
          </button>
        </div>

        {discoveredTuners && discoveredTuners.length > 0 && (
          <div
            data-testid="discovered-tuners-panel"
            className="space-y-2 rounded-2xl border border-amber-500/25 bg-amber-500/[0.05] p-3.5"
          >
            <p className="text-xs font-medium text-amber-200">
              {adminLabel(
                t,
                locale,
                'admin.livetv.discoveredTunersTitle',
                '已发现 {{count}} 台局域网调谐器设备',
                'Discovered {{count}} LAN tuner device(s)',
                { count: discoveredTuners.length },
              )}
            </p>
            <div className="divide-y divide-white/8 overflow-hidden rounded-xl border border-white/10 bg-zinc-950/60">
              {discoveredTuners.map((dev, idx) => (
                <div
                  key={dev.DeviceId || dev.Url || idx}
                  className="flex flex-wrap items-center justify-between gap-3 px-3.5 py-2.5"
                >
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="text-xs font-semibold text-zinc-100">
                        {dev.FriendlyName || 'HDHomeRun'}
                      </span>
                      <span className="rounded bg-amber-500/20 px-1.5 py-0.5 text-[10px] font-medium uppercase text-amber-300">
                        {dev.Type || 'hdhomerun'}
                      </span>
                      {typeof dev.TunerCount === 'number' && dev.TunerCount > 0 && (
                        <span className="text-[11px] text-zinc-400">
                          {adminLabel(
                            t,
                            locale,
                            'admin.livetv.tunerCountBadge',
                            '{{count}} 路调谐器',
                            '{{count}} tuners',
                            { count: dev.TunerCount },
                          )}
                        </span>
                      )}
                    </div>
                    <p className="mt-0.5 truncate font-mono text-[11px] text-zinc-400">
                      {dev.Url}
                      {dev.DeviceId ? ` · ID: ${dev.DeviceId}` : ''}
                    </p>
                  </div>
                  <button
                    type="button"
                    data-testid={`use-discovered-tuner-${idx}`}
                    onClick={() => handleUseDiscoveredTuner(dev)}
                    className="inline-flex items-center gap-1 rounded-lg border border-amber-500/35 bg-amber-500/15 px-2.5 py-1 text-xs font-semibold text-amber-300 transition-colors hover:bg-amber-500/25"
                  >
                    <span>
                      {adminLabel(
                        t,
                        locale,
                        'admin.livetv.useDiscoveredTunerBtn',
                        '填入配置',
                        'Use Tuner',
                      )}
                    </span>
                  </button>
                </div>
              ))}
            </div>
          </div>
        )}

        <form
          onSubmit={handleAddTuner}
          className="grid gap-3 rounded-2xl border border-white/8 bg-white/[0.02] p-4 sm:grid-cols-12"
        >
          <div className="sm:col-span-3">
            <label htmlFor="livetv-tuner-type" className="mb-1 block text-xs text-zinc-400">
              {adminLabel(
                t,
                locale,
                'admin.livetv.tunerTypeLabel',
                '调谐器类型',
                'Tuner Type',
              )}
            </label>
            <select
              id="livetv-tuner-type"
              value={tunerType}
              onChange={(e) => setTunerType(e.target.value as 'm3u' | 'hdhomerun')}
              className="w-full rounded-xl border border-white/10 bg-[#181822] px-3 py-2 text-xs text-zinc-100 outline-none focus:border-amber-400/60"
            >
              <option value="m3u">
                {adminLabel(
                  t,
                  locale,
                  'admin.livetv.tunerTypeM3u',
                  'M3U 播放列表 (m3u)',
                  'M3U Playlist (m3u)',
                )}
              </option>
              <option value="hdhomerun">
                {adminLabel(
                  t,
                  locale,
                  'admin.livetv.tunerTypeHdhomerun',
                  'HDHomeRun 设备 (hdhomerun)',
                  'HDHomeRun Device (hdhomerun)',
                )}
              </option>
            </select>
          </div>
          <div className="sm:col-span-3">
            <label htmlFor="livetv-tuner-name" className="mb-1 block text-xs text-zinc-400">
              {t('admin.livetv.tunerNameLabel')}
            </label>
            <input
              id="livetv-tuner-name"
              type="text"
              value={tunerName}
              onChange={(e) => setTunerName(e.target.value)}
              placeholder={
                tunerType === 'hdhomerun'
                  ? 'HDHomeRun Flex 4K'
                  : t('admin.livetv.tunerNamePlaceholder')
              }
              className="w-full rounded-xl border border-white/10 bg-zinc-900/90 px-3 py-2 text-xs text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
            />
          </div>
          <div className="sm:col-span-4">
            <label htmlFor="livetv-tuner-url" className="mb-1 block text-xs text-zinc-400">
              {tunerType === 'hdhomerun'
                ? adminLabel(
                    t,
                    locale,
                    'admin.livetv.hdhrUrlLabel',
                    'HDHomeRun 设备地址 (Url) *',
                    'HDHomeRun Device URL *',
                  )
                : t('admin.livetv.tunerUrlLabel')}
            </label>
            <input
              id="livetv-tuner-url"
              type="text"
              required
              value={tunerUrl}
              onChange={(e) => setTunerUrl(e.target.value)}
              placeholder={
                tunerType === 'hdhomerun'
                  ? 'http://192.168.1.50'
                  : t('admin.livetv.tunerUrlPlaceholder')
              }
              className="w-full rounded-xl border border-white/10 bg-zinc-900/90 px-3 py-2 font-mono text-xs text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
            />
          </div>
          <div className="flex items-end sm:col-span-2">
            <button
              type="submit"
              disabled={addingTuner}
              className="inline-flex w-full items-center justify-center gap-1.5 rounded-xl bg-amber-500 px-3 py-2 text-xs font-semibold text-zinc-950 transition-colors hover:bg-amber-400 disabled:opacity-60"
            >
              {addingTuner ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <Plus className="h-3.5 w-3.5" />
              )}
              <span>{t('admin.livetv.addTunerBtn')}</span>
            </button>
          </div>
        </form>

        {tunersLoading ? (
          <div className="flex h-24 items-center justify-center rounded-2xl border border-white/8 bg-white/[0.02]">
            <Loader2 className="h-5 w-5 animate-spin text-amber-400" />
          </div>
        ) : tunersError ? (
          <FeedbackBanner
            type="error"
            message={t('admin.livetv.tunersLoadError', {
              error: tunersErr instanceof Error ? tunersErr.message : t('admin.livetv.retryLater'),
            })}
          />
        ) : tunerHosts.length === 0 ? (
          <div className="rounded-2xl border border-white/8 bg-white/[0.02] py-8 text-center text-xs text-zinc-500">
            {t('admin.livetv.tunersEmpty')}
          </div>
        ) : (
          <div className="divide-y divide-white/6 overflow-hidden rounded-2xl border border-white/8 bg-white/[0.02]">
            {tunerHosts.map((host) => (
              <div
                key={host.Id || host.Url}
                className="flex flex-wrap items-center justify-between gap-3 px-4 py-3"
              >
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <p className="truncate text-sm font-semibold text-zinc-100">
                      {host.FriendlyName || t('admin.livetv.defaultTunerName')}
                    </p>
                    <span className="rounded bg-amber-500/15 px-1.5 py-0.5 text-[10px] font-medium uppercase text-amber-300">
                      {host.Type || 'm3u'}
                    </span>
                  </div>
                  <p className="mt-0.5 truncate font-mono text-xs text-zinc-400" title={host.Url}>
                    {host.Url}
                  </p>
                </div>
                {host.Id && (
                  <button
                    type="button"
                    disabled={deletingTunerId === host.Id}
                    onClick={() => setConfirmDeleteTuner(host)}
                    className="inline-flex items-center gap-1 rounded-lg border border-red-500/25 bg-red-500/10 px-2.5 py-1.5 text-xs font-medium text-red-300 transition-colors hover:bg-red-500/20 disabled:opacity-50"
                  >
                    {deletingTunerId === host.Id ? (
                      <Loader2 className="h-3.5 w-3.5 animate-spin" />
                    ) : (
                      <Trash2 className="h-3.5 w-3.5" />
                    )}
                    <span>{t('common.delete')}</span>
                  </button>
                )}
              </div>
            ))}
          </div>
        )}
      </section>

      {/* 2. XMLTV EPG Listing Providers */}
      <section className="space-y-4">
        <div className="flex items-center justify-between">
          <h3 className="flex items-center gap-2 text-sm font-semibold text-zinc-200">
            <CalendarDays className="h-4 w-4 text-amber-400" />
            <span>{t('admin.livetv.epgSectionTitle')}</span>
            <span className="rounded-md bg-white/8 px-2 py-0.5 text-xs font-medium text-zinc-300">
              {listingProviders.length}
            </span>
          </h3>
        </div>

        <form
          onSubmit={handleAddEpg}
          className="grid gap-3 rounded-2xl border border-white/8 bg-white/[0.02] p-4 sm:grid-cols-12"
        >
          <div className="sm:col-span-10">
            <label htmlFor="livetv-epg-path" className="mb-1 block text-xs text-zinc-400">
              {t('admin.livetv.epgPathLabel')}
            </label>
            <input
              id="livetv-epg-path"
              type="text"
              required
              value={epgPath}
              onChange={(e) => setEpgPath(e.target.value)}
              placeholder={t('admin.livetv.epgPathPlaceholder')}
              className="w-full rounded-xl border border-white/10 bg-zinc-900/90 px-3 py-2 font-mono text-xs text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
            />
          </div>
          <div className="flex items-end sm:col-span-2">
            <button
              type="submit"
              disabled={addingEpg}
              className="inline-flex w-full items-center justify-center gap-1.5 rounded-xl bg-amber-500 px-3 py-2 text-xs font-semibold text-zinc-950 transition-colors hover:bg-amber-400 disabled:opacity-60"
            >
              {addingEpg ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <Plus className="h-3.5 w-3.5" />
              )}
              <span>{t('admin.livetv.addEpgBtn')}</span>
            </button>
          </div>
        </form>

        {epgLoading ? (
          <div className="flex h-24 items-center justify-center rounded-2xl border border-white/8 bg-white/[0.02]">
            <Loader2 className="h-5 w-5 animate-spin text-amber-400" />
          </div>
        ) : epgError ? (
          <FeedbackBanner
            type="error"
            message={t('admin.livetv.epgLoadError', {
              error: epgErr instanceof Error ? epgErr.message : t('admin.livetv.retryLater'),
            })}
          />
        ) : listingProviders.length === 0 ? (
          <div className="rounded-2xl border border-white/8 bg-white/[0.02] py-8 text-center text-xs text-zinc-500">
            {t('admin.livetv.epgEmpty')}
          </div>
        ) : (
          <div className="divide-y divide-white/6 overflow-hidden rounded-2xl border border-white/8 bg-white/[0.02]">
            {listingProviders.map((provider) => (
              <div
                key={provider.Id || provider.Path}
                className="flex flex-wrap items-center justify-between gap-3 px-4 py-3"
              >
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="rounded bg-sky-500/15 px-1.5 py-0.5 text-[10px] font-medium uppercase text-sky-300">
                      {provider.Type || 'xmltv'}
                    </span>
                    <p
                      className="truncate font-mono text-xs font-medium text-zinc-200"
                      title={provider.Path}
                    >
                      {provider.Path}
                    </p>
                  </div>
                </div>
                {provider.Id && (
                  <button
                    type="button"
                    disabled={deletingEpgId === provider.Id}
                    onClick={() => handleDeleteEpg(provider.Id!)}
                    className="inline-flex items-center gap-1 rounded-lg border border-red-500/25 bg-red-500/10 px-2.5 py-1.5 text-xs font-medium text-red-300 transition-colors hover:bg-red-500/20 disabled:opacity-50"
                  >
                    {deletingEpgId === provider.Id ? (
                      <Loader2 className="h-3.5 w-3.5 animate-spin" />
                    ) : (
                      <Trash2 className="h-3.5 w-3.5" />
                    )}
                    <span>{t('common.delete')}</span>
                  </button>
                )}
              </div>
            ))}
          </div>
        )}
      </section>

      {/* 3. Imported Channels Preview */}
      <section className="space-y-4">
        <div className="flex items-center justify-between">
          <h3 className="flex items-center gap-2 text-sm font-semibold text-zinc-200">
            <Radio className="h-4 w-4 text-amber-400" />
            <span>{t('admin.livetv.channelsSectionTitle')}</span>
            <span
              data-testid="livetv-channel-count"
              className="rounded-md bg-white/8 px-2 py-0.5 text-xs font-medium text-zinc-300"
            >
              {t('admin.livetv.channelsCountBadge', { count: totalChannels })}
            </span>
          </h3>
        </div>

        {channelsLoading ? (
          <div className="flex h-28 items-center justify-center rounded-2xl border border-white/8 bg-white/[0.02]">
            <Loader2 className="h-5 w-5 animate-spin text-amber-400" />
          </div>
        ) : channelsError ? (
          <FeedbackBanner
            type="error"
            message={t('admin.livetv.channelsLoadError', {
              error:
                channelsErr instanceof Error ? channelsErr.message : t('admin.livetv.retryLater'),
            })}
          />
        ) : channels.length === 0 ? (
          <div className="rounded-2xl border border-white/8 bg-white/[0.02] py-10 text-center text-xs text-zinc-500">
            {t('admin.livetv.channelsEmpty')}
          </div>
        ) : (
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {channels.map((ch) => (
              <div
                key={ch.Id}
                className="flex items-center gap-3 rounded-xl border border-white/8 bg-white/[0.03] px-3.5 py-2.5"
              >
                <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-amber-500/15 font-mono text-xs font-bold text-amber-300">
                  {ch.Number || 'TV'}
                </div>
                <div className="min-w-0 flex-1">
                  <p className="truncate text-xs font-semibold text-zinc-100">{ch.Name}</p>
                  <p className="mt-0.5 truncate font-mono text-[10px] text-zinc-500">
                    {ch.ChannelType || 'TV'}
                    {ch.StreamUrl ? ` · ${ch.StreamUrl}` : ''}
                  </p>
                </div>
                <Link
                  to={`/livetv?channel=${encodeURIComponent(ch.Id)}`}
                  className="inline-flex shrink-0 items-center gap-1 rounded-lg border border-amber-500/35 bg-amber-500/10 px-2.5 py-1 text-[11px] font-medium text-amber-300 transition-colors hover:bg-amber-500/20"
                >
                  <Play className="h-3 w-3" />
                  <span>{t('admin.livetv.watchBtn')}</span>
                </Link>
              </div>
            ))}
          </div>
        )}
      </section>

      {/* 4. Collapsible Channel & EPG Mapping */}
      <section className="overflow-hidden rounded-2xl border border-white/8 bg-white/[0.02]">
        <button
          type="button"
          data-testid="toggle-channel-mapping-btn"
          aria-expanded={mappingExpanded}
          onClick={() => setMappingExpanded((prev) => !prev)}
          className="flex w-full items-center justify-between gap-3 px-4 py-3.5 text-left transition-colors hover:bg-white/[0.03]"
        >
          <div className="flex items-center gap-2">
            <Link2 className="h-4 w-4 text-amber-400" />
            <span className="text-sm font-semibold text-zinc-200">
              {adminLabel(
                t,
                locale,
                'admin.livetv.mappingSectionTitle',
                '频道与 EPG 节目单映射',
                'Channel & EPG Mapping',
              )}
            </span>
            <span className="rounded-md bg-white/8 px-2 py-0.5 text-xs font-medium text-zinc-300">
              {totalChannels}
            </span>
          </div>
          <div className="flex items-center gap-1.5 text-xs text-zinc-400">
            <span>
              {mappingExpanded
                ? adminLabel(t, locale, 'admin.livetv.collapseMapping', '收起', 'Collapse')
                : adminLabel(t, locale, 'admin.livetv.expandMapping', '展开配置', 'Expand')}
            </span>
            {mappingExpanded ? (
              <ChevronDown className="h-4 w-4 text-amber-400" />
            ) : (
              <ChevronRight className="h-4 w-4" />
            )}
          </div>
        </button>

        {mappingExpanded && (
          <div
            data-testid="channel-mapping-section"
            className="space-y-4 border-t border-white/8 p-4"
          >
            <div className="flex flex-wrap items-center justify-between gap-3">
              <p className="text-xs text-zinc-400">
                {adminLabel(
                  t,
                  locale,
                  'admin.livetv.mappingSectionDesc',
                  '将调谐器导入的直播频道绑定到对应的 XMLTV EPG 频道 ID，修正节目指南错位或缺失。',
                  'Bind imported tuner channels to XMLTV EPG channel IDs to fix missing or mismatched program guides.',
                )}
              </p>

              <div className="flex flex-wrap items-center gap-2">
                {listingProviders.length > 0 && (
                  <div className="flex items-center gap-2">
                    <label htmlFor="livetv-mapping-provider" className="text-xs text-zinc-400">
                      {adminLabel(
                        t,
                        locale,
                        'admin.livetv.mappingProviderLabel',
                        'EPG 数据源',
                        'EPG Provider',
                      )}
                    </label>
                    <select
                      id="livetv-mapping-provider"
                      value={activeProviderId}
                      onChange={(e) => {
                        setSelectedProviderId(e.target.value)
                        setDraftMappings({})
                      }}
                      className="rounded-lg border border-white/10 bg-[#181822] px-2.5 py-1.5 text-xs text-zinc-100 outline-none focus:border-amber-400/60"
                    >
                      {listingProviders.map((prov, idx) => (
                        <option key={prov.Id || idx} value={prov.Id || ''}>
                          {prov.Path || prov.Id || `EPG #${idx + 1}`}
                        </option>
                      ))}
                    </select>
                  </div>
                )}

                <button
                  type="button"
                  data-testid="refresh-channel-mappings-btn"
                  onClick={() => void refetchMappingOptions()}
                  className="inline-flex items-center gap-1 rounded-lg border border-white/10 bg-white/[0.04] px-2.5 py-1.5 text-xs font-medium text-zinc-300 transition-colors hover:border-amber-400/40 hover:text-amber-300"
                >
                  <RefreshCw className={`h-3.5 w-3.5 ${mappingFetching ? 'animate-spin' : ''}`} />
                  <span>
                    {adminLabel(
                      t,
                      locale,
                      'admin.livetv.refreshMappingsBtn',
                      '刷新映射选项',
                      'Refresh Mappings',
                    )}
                  </span>
                </button>
              </div>
            </div>

            {mappingLoading ? (
              <div className="flex h-24 items-center justify-center rounded-xl border border-white/8 bg-white/[0.02]">
                <Loader2 className="h-5 w-5 animate-spin text-amber-400" />
              </div>
            ) : mappingError ? (
              <FeedbackBanner
                type="error"
                message={
                  mappingErr instanceof Error
                    ? mappingErr.message
                    : adminLabel(
                        t,
                        locale,
                        'admin.livetv.mappingLoadError',
                        '无法加载频道映射选项',
                        'Failed to load channel mapping options',
                      )
                }
              />
            ) : mergedMappingChannels.length === 0 ? (
              <div className="rounded-xl border border-white/8 bg-white/[0.02] py-8 text-center text-xs text-zinc-500">
                {t('admin.livetv.channelsEmpty')}
              </div>
            ) : (
              <div className="divide-y divide-white/6 overflow-hidden rounded-xl border border-white/8 bg-zinc-950/40">
                {mergedMappingChannels.map((ch) => {
                  const mappedValue = serverMappingsByTunerId[ch.Id] ?? ''
                  const draftValue = draftMappings[ch.Id] ?? mappedValue
                  const hasCustomValue =
                    Boolean(draftValue) && !providerChannels.some((pc) => pc.Id === draftValue)
                  const isSaving = savingMappingChannelId === ch.Id

                  return (
                    <div
                      key={ch.Id}
                      data-testid={`channel-mapping-row-${ch.Id}`}
                      className="grid items-center gap-3 px-4 py-3 sm:grid-cols-12"
                    >
                      <div className="min-w-0 sm:col-span-4">
                        <div className="flex items-center gap-2">
                          <span className="rounded bg-amber-500/15 px-1.5 py-0.5 font-mono text-[11px] font-semibold text-amber-300">
                            {ch.Number || 'TV'}
                          </span>
                          <p className="truncate text-xs font-semibold text-zinc-100">{ch.Name}</p>
                        </div>
                        <p className="mt-0.5 truncate font-mono text-[10px] text-zinc-500">
                          {adminLabel(
                            t,
                            locale,
                            'admin.livetv.currentMappedEpg',
                            '当前映射: {{epg}}',
                            'Mapped: {{epg}}',
                            {
                              epg:
                                mappedValue ||
                                adminLabel(
                                  t,
                                  locale,
                                  'admin.livetv.unmappedLabel',
                                  '自动匹配',
                                  'Auto',
                                ),
                            },
                          )}
                        </p>
                      </div>

                      <div className="sm:col-span-3">
                        <select
                          aria-label={adminLabel(
                            t,
                            locale,
                            'admin.livetv.selectEpgChannelAria',
                            '选择 {{name}} 的 EPG 频道',
                            'Select EPG channel for {{name}}',
                            { name: ch.Name },
                          )}
                          data-testid={`channel-mapping-select-${ch.Id}`}
                          value={draftValue}
                          onChange={(e) =>
                            setDraftMappings((prev) => ({
                              ...prev,
                              [ch.Id]: e.target.value,
                            }))
                          }
                          className="w-full rounded-lg border border-white/10 bg-[#181822] px-2.5 py-1.5 text-xs text-zinc-100 outline-none focus:border-amber-400/60"
                        >
                          <option value="">
                            {adminLabel(
                              t,
                              locale,
                              'admin.livetv.epgAutoOption',
                              '-- 自动匹配 / 未指定 --',
                              '-- Auto / Unassigned --',
                            )}
                          </option>
                          {providerChannels.map((pc) => (
                            <option key={pc.Id} value={pc.Id}>
                              {pc.Name && pc.Name !== pc.Id ? `${pc.Name} (${pc.Id})` : pc.Id}
                            </option>
                          ))}
                          {hasCustomValue && <option value={draftValue}>{draftValue}</option>}
                        </select>
                      </div>

                      <div className="sm:col-span-3">
                        <input
                          type="text"
                          data-testid={`channel-mapping-input-${ch.Id}`}
                          value={draftValue}
                          onChange={(e) =>
                            setDraftMappings((prev) => ({
                              ...prev,
                              [ch.Id]: e.target.value,
                            }))
                          }
                          placeholder={adminLabel(
                            t,
                            locale,
                            'admin.livetv.customEpgIdPlaceholder',
                            '自定义 XMLTV 频道 ID',
                            'Custom XMLTV channel ID',
                          )}
                          className="w-full rounded-lg border border-white/10 bg-zinc-900/90 px-2.5 py-1.5 font-mono text-xs text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
                        />
                      </div>

                      <div className="flex justify-end sm:col-span-2">
                        <button
                          type="button"
                          data-testid={`save-channel-mapping-${ch.Id}`}
                          disabled={isSaving}
                          onClick={() => void handleSaveChannelMapping(ch.Id)}
                          className="inline-flex w-full items-center justify-center gap-1 rounded-lg border border-amber-500/35 bg-amber-500/15 px-2.5 py-1.5 text-xs font-semibold text-amber-300 transition-colors hover:bg-amber-500/25 disabled:opacity-50"
                        >
                          {isSaving ? (
                            <Loader2 className="h-3.5 w-3.5 animate-spin" />
                          ) : (
                            <Save className="h-3.5 w-3.5" />
                          )}
                          <span>
                            {adminLabel(
                              t,
                              locale,
                              'admin.livetv.saveMappingBtn',
                              '保存映射',
                              'Save Mapping',
                            )}
                          </span>
                        </button>
                      </div>
                    </div>
                  )
                })}
              </div>
            )}
          </div>
        )}
      </section>

      {/* M-15: Tuner Host Delete Confirmation Modal */}
      <AdminModal
        open={Boolean(confirmDeleteTuner)}
        title={adminLabel(
          t,
          locale,
          'admin.livetv.confirmDeleteTunerTitle',
          '确认删除调谐器源',
          'Confirm Tuner Host Deletion',
        )}
        subtitle={
          confirmDeleteTuner
            ? `${confirmDeleteTuner.FriendlyName || t('admin.livetv.defaultTunerName')} · ${confirmDeleteTuner.Url || ''}`
            : undefined
        }
        maxWidthClass="max-w-md"
        onClose={() => {
          if (!deletingTunerId) {
            setConfirmDeleteTuner(null)
          }
        }}
      >
        <div className="space-y-4">
          <div
            data-testid="confirm-delete-tuner-warning"
            className="flex items-start gap-3 rounded-xl border border-red-500/30 bg-red-500/10 p-3.5 text-xs leading-relaxed text-red-200"
          >
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-red-400" />
            <div className="space-y-1">
              <p className="font-semibold text-red-100">
                {adminLabel(
                  t,
                  locale,
                  'admin.livetv.confirmDeleteTunerWarningTitle',
                  '删除调谐器将移除关联频道并中断活跃录制',
                  'Deleting this tuner will remove its channels and interrupt active recordings',
                )}
              </p>
              <p>
                {adminLabel(
                  t,
                  locale,
                  'admin.livetv.confirmDeleteTunerWarningDesc',
                  '确定要移除调谐器「{{name}}」吗？该调谐器导入的所有直播频道将被移除，正在进行的直播流与关联 DVR 录制计划也将被中断。',
                  'Are you sure you want to remove tuner "{{name}}"? All imported live channels from this tuner will be removed, and any active streams or scheduled DVR recordings using it will be interrupted.',
                  {
                    name:
                      confirmDeleteTuner?.FriendlyName ||
                      confirmDeleteTuner?.Id ||
                      t('admin.livetv.defaultTunerName'),
                  },
                )}
              </p>
            </div>
          </div>

          <div className="flex justify-end gap-2 pt-2">
            <button
              type="button"
              disabled={Boolean(deletingTunerId)}
              onClick={() => setConfirmDeleteTuner(null)}
              className="rounded-xl border border-white/10 bg-white/[0.04] px-4 py-2 text-xs font-medium text-zinc-300 hover:bg-white/10 disabled:opacity-50"
            >
              {t('common.cancel')}
            </button>
            <button
              type="button"
              data-testid="confirm-delete-tuner-btn"
              disabled={Boolean(deletingTunerId) || !confirmDeleteTuner?.Id}
              onClick={() => {
                if (confirmDeleteTuner?.Id) {
                  void handleDeleteTuner(confirmDeleteTuner.Id, confirmDeleteTuner.FriendlyName)
                }
              }}
              className="inline-flex items-center gap-1.5 rounded-xl bg-red-500 px-4 py-2 text-xs font-semibold text-white transition-colors hover:bg-red-400 disabled:opacity-60"
            >
              {deletingTunerId ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <Trash2 className="h-3.5 w-3.5" />
              )}
              <span>
                {adminLabel(
                  t,
                  locale,
                  'admin.livetv.confirmDeleteTunerBtn',
                  '确认删除调谐器',
                  'Delete Tuner',
                )}
              </span>
            </button>
          </div>
        </div>
      </AdminModal>
    </div>
  )
}
