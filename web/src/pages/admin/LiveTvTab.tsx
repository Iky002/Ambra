import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { CalendarDays, Loader2, Play, Plus, Radio, RefreshCw, Trash2, Tv } from 'lucide-react'
import { Link } from 'react-router-dom'
import { api } from '../../api/client'
import { FeedbackBanner } from './adminUtils'

export function LiveTvTab() {
  const queryClient = useQueryClient()
  const [feedback, setFeedback] = useState<{ type: 'success' | 'error'; message: string } | null>(
    null,
  )

  // M3U Tuner Form State
  const [tunerName, setTunerName] = useState('')
  const [tunerUrl, setTunerUrl] = useState('')
  const [addingTuner, setAddingTuner] = useState(false)
  const [deletingTunerId, setDeletingTunerId] = useState<string | null>(null)

  // XMLTV EPG Form State
  const [epgPath, setEpgPath] = useState('')
  const [addingEpg, setAddingEpg] = useState(false)
  const [deletingEpgId, setDeletingEpgId] = useState<string | null>(null)

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

  const channels = channelsData?.Items || []
  const totalChannels = channelsData?.TotalRecordCount ?? channels.length

  const handleRefreshAll = () => {
    void refetchTuners()
    void refetchEpg()
    void refetchChannels()
  }

  const handleAddTuner = async (e: React.FormEvent) => {
    e.preventDefault()
    const trimmedUrl = tunerUrl.trim()
    if (!trimmedUrl) {
      setFeedback({ type: 'error', message: '请输入 M3U 播放列表地址' })
      return
    }
    setAddingTuner(true)
    setFeedback(null)
    try {
      await api.addTunerHost({
        Type: 'm3u',
        FriendlyName: tunerName.trim() || 'M3U 直播源',
        Url: trimmedUrl,
      })
      setTunerName('')
      setTunerUrl('')
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['admin', 'liveTv', 'tunerHosts'] }),
        queryClient.invalidateQueries({ queryKey: ['admin', 'liveTv', 'channels'] }),
      ])
      setFeedback({ type: 'success', message: '已添加 M3U 直播源' })
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '添加 M3U 直播源失败',
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
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['admin', 'liveTv', 'tunerHosts'] }),
        queryClient.invalidateQueries({ queryKey: ['admin', 'liveTv', 'channels'] }),
      ])
      setFeedback({ type: 'success', message: `已删除直播源「${name || id}」` })
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '删除直播源失败',
      })
    } finally {
      setDeletingTunerId(null)
    }
  }

  const handleAddEpg = async (e: React.FormEvent) => {
    e.preventDefault()
    const trimmedPath = epgPath.trim()
    if (!trimmedPath) {
      setFeedback({ type: 'error', message: '请输入 XMLTV EPG 地址或本地路径' })
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
      await queryClient.invalidateQueries({ queryKey: ['admin', 'liveTv', 'listingProviders'] })
      setFeedback({ type: 'success', message: '已添加 XMLTV EPG 节目单来源' })
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '添加 XMLTV EPG 失败',
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
      await queryClient.invalidateQueries({ queryKey: ['admin', 'liveTv', 'listingProviders'] })
      setFeedback({ type: 'success', message: '已删除 XMLTV EPG 节目单来源' })
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '删除 XMLTV EPG 失败',
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
            <span>电视直播 (Live TV)</span>
          </h2>
          <p className="mt-1 text-xs text-zinc-400">
            管理 M3U 直播源调谐器、XMLTV 电子节目单（EPG）并预览已导入的电视频道
          </p>
        </div>

        <button
          type="button"
          onClick={handleRefreshAll}
          className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/[0.04] px-3 py-1.5 text-xs font-medium text-zinc-300 transition-colors hover:border-amber-400/40 hover:text-amber-300"
        >
          <RefreshCw
            className={`h-3.5 w-3.5 ${tunersFetching || channelsFetching ? 'animate-spin' : ''}`}
          />
          <span>刷新</span>
        </button>
      </div>

      <FeedbackBanner
        message={feedback?.message}
        type={feedback?.type}
        onClose={() => setFeedback(null)}
      />

      {/* 1. M3U 直播源 (Tuner Hosts) */}
      <section className="space-y-4">
        <div className="flex items-center justify-between">
          <h3 className="flex items-center gap-2 text-sm font-semibold text-zinc-200">
            <Tv className="h-4 w-4 text-amber-400" />
            <span>M3U 直播源 (Tuner Hosts)</span>
            <span className="rounded-md bg-white/8 px-2 py-0.5 text-xs font-medium text-zinc-300">
              {tunerHosts.length}
            </span>
          </h3>
        </div>

        <form
          onSubmit={handleAddTuner}
          className="grid gap-3 rounded-2xl border border-white/8 bg-white/[0.02] p-4 sm:grid-cols-12"
        >
          <div className="sm:col-span-4">
            <label htmlFor="livetv-tuner-name" className="mb-1 block text-xs text-zinc-400">
              源名称 (FriendlyName)
            </label>
            <input
              id="livetv-tuner-name"
              type="text"
              value={tunerName}
              onChange={(e) => setTunerName(e.target.value)}
              placeholder="例如：IPTV 高清源"
              className="w-full rounded-xl border border-white/10 bg-zinc-900/90 px-3 py-2 text-xs text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
            />
          </div>
          <div className="sm:col-span-6">
            <label htmlFor="livetv-tuner-url" className="mb-1 block text-xs text-zinc-400">
              M3U 地址 (Url) *
            </label>
            <input
              id="livetv-tuner-url"
              type="text"
              required
              value={tunerUrl}
              onChange={(e) => setTunerUrl(e.target.value)}
              placeholder="https://example.com/playlist.m3u 或 /data/iptv.m3u"
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
              <span>添加源</span>
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
            message={`加载 M3U 直播源失败：${tunersErr instanceof Error ? tunersErr.message : '请稍后重试'}`}
          />
        ) : tunerHosts.length === 0 ? (
          <div className="rounded-2xl border border-white/8 bg-white/[0.02] py-8 text-center text-xs text-zinc-500">
            暂未配置 M3U 直播源
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
                      {host.FriendlyName || 'M3U 调谐器'}
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
                    onClick={() => handleDeleteTuner(host.Id!, host.FriendlyName)}
                    className="inline-flex items-center gap-1 rounded-lg border border-red-500/25 bg-red-500/10 px-2.5 py-1.5 text-xs font-medium text-red-300 transition-colors hover:bg-red-500/20 disabled:opacity-50"
                  >
                    {deletingTunerId === host.Id ? (
                      <Loader2 className="h-3.5 w-3.5 animate-spin" />
                    ) : (
                      <Trash2 className="h-3.5 w-3.5" />
                    )}
                    <span>删除</span>
                  </button>
                )}
              </div>
            ))}
          </div>
        )}
      </section>

      {/* 2. XMLTV 节目单 (EPG Listing Providers) */}
      <section className="space-y-4">
        <div className="flex items-center justify-between">
          <h3 className="flex items-center gap-2 text-sm font-semibold text-zinc-200">
            <CalendarDays className="h-4 w-4 text-amber-400" />
            <span>XMLTV 节目单 (EPG Listing Providers)</span>
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
              XMLTV EPG 地址或路径 (Path) *
            </label>
            <input
              id="livetv-epg-path"
              type="text"
              required
              value={epgPath}
              onChange={(e) => setEpgPath(e.target.value)}
              placeholder="https://example.com/epg.xml 或 /data/epg.xml"
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
              <span>添加 EPG</span>
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
            message={`加载 XMLTV 节目单来源失败：${epgErr instanceof Error ? epgErr.message : '请稍后重试'}`}
          />
        ) : listingProviders.length === 0 ? (
          <div className="rounded-2xl border border-white/8 bg-white/[0.02] py-8 text-center text-xs text-zinc-500">
            暂未配置 XMLTV 节目单来源
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
                    <span>删除</span>
                  </button>
                )}
              </div>
            ))}
          </div>
        )}
      </section>

      {/* 3. 已导入频道预览 */}
      <section className="space-y-4">
        <div className="flex items-center justify-between">
          <h3 className="flex items-center gap-2 text-sm font-semibold text-zinc-200">
            <Radio className="h-4 w-4 text-amber-400" />
            <span>已导入频道预览</span>
            <span
              data-testid="livetv-channel-count"
              className="rounded-md bg-white/8 px-2 py-0.5 text-xs font-medium text-zinc-300"
            >
              共 {totalChannels} 个频道
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
            message={`加载电视频道列表失败：${channelsErr instanceof Error ? channelsErr.message : '请稍后重试'}`}
          />
        ) : channels.length === 0 ? (
          <div className="rounded-2xl border border-white/8 bg-white/[0.02] py-10 text-center text-xs text-zinc-500">
            暂无已导入频道，添加 M3U 直播源后将自动解析频道列表
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
                  <span>观看</span>
                </Link>
              </div>
            ))}
          </div>
        )}
      </section>
    </div>
  )
}
