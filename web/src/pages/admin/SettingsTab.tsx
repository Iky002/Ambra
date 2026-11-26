import { useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Cpu,
  Eye,
  EyeOff,
  Globe,
  Loader2,
  RefreshCw,
  RotateCcw,
  Save,
  SlidersHorizontal,
  Trash2,
} from 'lucide-react'
import { api, embyFetch } from '../../api/client'
import type { EncodingFfmpegOptions, ToneMapOptions } from '../../api/types'
import { QueryErrorState } from '../../components/QueryErrorState'
import { useI18n } from '../../i18n/I18nContext'
import { FeedbackBanner, ToggleSwitch } from './adminUtils'

const MASKED_SECRET = '********'

const TMDB_LANGUAGE_OPTIONS = [
  { value: 'zh-CN', label: '简体中文 (zh-CN)' },
  { value: 'zh-TW', label: '繁體中文 (zh-TW)' },
  { value: 'en-US', label: 'English (en-US)' },
  { value: 'ja-JP', label: '日本語 (ja-JP)' },
  { value: 'ko-KR', label: '한국어 (ko-KR)' },
  { value: 'es-ES', label: 'Español (es-ES)' },
  { value: 'de-DE', label: 'Deutsch (de-DE)' },
  { value: 'fr-FR', label: 'Français (fr-FR)' },
  { value: 'pt-BR', label: 'Português (pt-BR)' },
  { value: 'th-TH', label: 'ไทย (th-TH)' },
] as const

const H264_PRESETS = [
  'ultrafast',
  'superfast',
  'veryfast',
  'faster',
  'fast',
  'medium',
  'slow',
  'slower',
  'veryslow',
] as const

const TONE_MAP_ALGORITHMS = [
  { value: 'hable', label: 'Hable (Filmic)' },
  { value: 'mobius', label: 'Mobius' },
  { value: 'bt2390', label: 'ITU-R BT.2390' },
] as const

function parseListInput(raw: string): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  for (const item of raw.split(/[\n,，]+/)) {
    const cleaned = item.trim()
    if (cleaned && !seen.has(cleaned)) {
      seen.add(cleaned)
      out.push(cleaned)
    }
  }
  return out
}

export function SettingsTab() {
  const queryClient = useQueryClient()
  const { t } = useI18n()
  const [feedback, setFeedback] = useState<{ type: 'success' | 'error'; message: string } | null>(
    null,
  )

  // 常规配置状态
  const [serverName, setServerName] = useState('')
  const [tmdbApiKey, setTmdbApiKey] = useState('')
  const [tmdbKeyConfigured, setTmdbKeyConfigured] = useState(false)
  const [tmdbKeyCleared, setTmdbKeyCleared] = useState(false)
  const [showApiKey, setShowApiKey] = useState(false)
  const [tmdbLanguage, setTmdbLanguage] = useState('zh-CN')
  const [enableStrm302, setEnableStrm302] = useState(true)
  const [playedThresholdPct, setPlayedThresholdPct] = useState(90)

  // 网络、远程访问与协议配置状态
  const [enableRemoteAccess, setEnableRemoteAccess] = useState(true)
  const [enableQuickConnect, setEnableQuickConnect] = useState(true)
  const [enableDlna, setEnableDlna] = useState(false)
  const [dlnaDefaultUserId, setDlnaDefaultUserId] = useState('')
  const [localSubnetsText, setLocalSubnetsText] = useState('')
  const [trustedProxiesText, setTrustedProxiesText] = useState('')
  const [allowedOriginsText, setAllowedOriginsText] = useState('')
  const [streamLimit, setStreamLimit] = useState(0)
  const [remoteBitrateLimit, setRemoteBitrateLimit] = useState(0)

  const [saving, setSaving] = useState(false)

  // 转码与硬件加速配置状态
  const [hwAccelMode, setHwAccelMode] = useState(0)
  const [enableHwEncoding, setEnableHwEncoding] = useState(false)
  const [allowHevcEncoding, setAllowHevcEncoding] = useState(false)
  const [h264Crf, setH264Crf] = useState(23)
  const [h264Preset, setH264Preset] = useState('veryfast')
  const [enableToneMapping, setEnableToneMapping] = useState(false)
  const [toneMappingAlgorithm, setToneMappingAlgorithm] = useState('bt2390')
  const [enableThrottling, setEnableThrottling] = useState(true)
  const [throttleBufferSize, setThrottleBufferSize] = useState(120)
  const [transcodingTempPath, setTranscodingTempPath] = useState('')
  const [savingEncoding, setSavingEncoding] = useState(false)

  const {
    data: config,
    isLoading,
    isFetching,
    isError,
    error,
    refetch,
  } = useQuery({
    queryKey: ['admin', 'serverConfig'],
    queryFn: () => api.getServerConfig(),
  })

  const { data: users = [] } = useQuery({
    queryKey: ['admin', 'users', 'dlnaOptions'],
    queryFn: () => api.getUsers().catch(() => []),
    retry: false,
  })

  const { data: ffmpegOptions, refetch: refetchFfmpeg } = useQuery({
    queryKey: ['admin', 'encodingFfmpegOptions'],
    queryFn: () =>
      embyFetch<EncodingFfmpegOptions>('/emby/Encoding/FfmpegOptions').catch(() => null),
    retry: false,
  })

  const { data: toneMapOptions, refetch: refetchToneMap } = useQuery({
    queryKey: ['admin', 'encodingToneMapOptions'],
    queryFn: () =>
      embyFetch<ToneMapOptions>('/emby/Encoding/FullToneMapOptions').catch(() => null),
    retry: false,
  })

  useEffect(() => {
    if (!config) return
    setServerName(config.ServerName || '')

    const rawKey = config.TMDBApiKey || ''
    if (rawKey === MASKED_SECRET || Boolean(config.HasTMDBApiKey && !rawKey)) {
      setTmdbApiKey('')
      setTmdbKeyConfigured(true)
      setTmdbKeyCleared(false)
    } else if (rawKey === '' && !config.HasTMDBApiKey) {
      setTmdbApiKey('')
      setTmdbKeyConfigured(false)
      setTmdbKeyCleared(false)
    } else {
      setTmdbApiKey(rawKey)
      setTmdbKeyConfigured(Boolean(config.HasTMDBApiKey))
      setTmdbKeyCleared(false)
    }

    setTmdbLanguage(config.TMDBLanguage || 'zh-CN')
    setEnableStrm302(config.EnableSTRM302 ?? true)
    const rawPct =
      typeof config.PlayedThreshold === 'number' && config.PlayedThreshold > 0
        ? Math.round(config.PlayedThreshold * 100)
        : 90
    setPlayedThresholdPct(Math.min(99, Math.max(50, rawPct)))

    setEnableRemoteAccess(config.EnableRemoteAccess ?? true)
    setEnableQuickConnect(config.EnableQuickConnect ?? true)
    setEnableDlna(config.EnableDLNA ?? false)
    setDlnaDefaultUserId(config.DLNADefaultUserID || '')
    setLocalSubnetsText(Array.isArray(config.LocalNetworkSubnets) ? config.LocalNetworkSubnets.join(', ') : '')
    setTrustedProxiesText(Array.isArray(config.TrustedProxies) ? config.TrustedProxies.join(', ') : '')
    setAllowedOriginsText(Array.isArray(config.AllowedOrigins) ? config.AllowedOrigins.join(', ') : '')
    setStreamLimit(
      typeof config.SimultaneousStreamLimit === 'number' && config.SimultaneousStreamLimit >= 0
        ? config.SimultaneousStreamLimit
        : 0,
    )
    setRemoteBitrateLimit(
      typeof config.RemoteClientBitrateLimit === 'number' && config.RemoteClientBitrateLimit >= 0
        ? config.RemoteClientBitrateLimit
        : 0,
    )
  }, [config])

  useEffect(() => {
    if (!ffmpegOptions) return
    if (typeof ffmpegOptions.HardwareAccelerationMode === 'number') {
      setHwAccelMode(ffmpegOptions.HardwareAccelerationMode)
    }
    if (typeof ffmpegOptions.EnableHardwareEncoding === 'boolean') {
      setEnableHwEncoding(ffmpegOptions.EnableHardwareEncoding)
    }
    if (typeof ffmpegOptions.AllowHevcEncoding === 'boolean') {
      setAllowHevcEncoding(ffmpegOptions.AllowHevcEncoding)
    }
    if (typeof ffmpegOptions.H264Crf === 'number') {
      setH264Crf(ffmpegOptions.H264Crf)
    }
    if (typeof ffmpegOptions.H264Preset === 'string' && ffmpegOptions.H264Preset) {
      setH264Preset(ffmpegOptions.H264Preset)
    }
    if (typeof ffmpegOptions.EnableThrottling === 'boolean') {
      setEnableThrottling(ffmpegOptions.EnableThrottling)
    }
    if (typeof ffmpegOptions.ThrottleBufferSize === 'number') {
      setThrottleBufferSize(ffmpegOptions.ThrottleBufferSize)
    }
    if (typeof ffmpegOptions.TranscodingTempPath === 'string') {
      setTranscodingTempPath(ffmpegOptions.TranscodingTempPath)
    }
  }, [ffmpegOptions])

  useEffect(() => {
    if (!toneMapOptions) return
    if (typeof toneMapOptions.EnableToneMapping === 'boolean') {
      setEnableToneMapping(toneMapOptions.EnableToneMapping)
    }
    if (typeof toneMapOptions.ToneMappingAlgorithm === 'string' && toneMapOptions.ToneMappingAlgorithm) {
      setToneMappingAlgorithm(toneMapOptions.ToneMappingAlgorithm)
    }
  }, [toneMapOptions])

  const handleRefreshAll = () => {
    void refetch()
    void refetchFfmpeg()
    void refetchToneMap()
  }

  const resolveNextTmdbKey = (): string => {
    const trimmed = tmdbApiKey.trim()
    if (trimmed !== '') return trimmed
    if (tmdbKeyCleared) return ''
    if (tmdbKeyConfigured) return MASKED_SECRET
    return ''
  }

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault()
    const trimmedName = serverName.trim()
    if (!trimmedName) {
      setFeedback({ type: 'error', message: t('admin.settings.serverNameRequired') })
      return
    }
    if (
      !Number.isFinite(playedThresholdPct) ||
      playedThresholdPct < 50 ||
      playedThresholdPct > 99
    ) {
      setFeedback({ type: 'error', message: t('admin.settings.playedThresholdRangeError') })
      return
    }

    setSaving(true)
    setFeedback(null)
    try {
      await api.updateServerConfig({
        ServerName: trimmedName,
        TMDBApiKey: resolveNextTmdbKey(),
        TMDBLanguage: tmdbLanguage,
        EnableSTRM302: enableStrm302,
        PlayedThreshold: Number((playedThresholdPct / 100).toFixed(2)),
        EnableRemoteAccess: enableRemoteAccess,
        EnableQuickConnect: enableQuickConnect,
        EnableDLNA: enableDlna,
        DLNADefaultUserID: dlnaDefaultUserId,
        LocalNetworkSubnets: parseListInput(localSubnetsText),
        TrustedProxies: parseListInput(trustedProxiesText),
        AllowedOrigins: parseListInput(allowedOriginsText),
        SimultaneousStreamLimit: Math.max(0, Math.floor(Number(streamLimit) || 0)),
        RemoteClientBitrateLimit: Math.max(0, Math.floor(Number(remoteBitrateLimit) || 0)),
      })
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['admin', 'serverConfig'] }),
        queryClient.invalidateQueries({ queryKey: ['admin', 'plugins'] }),
        queryClient.invalidateQueries({ queryKey: ['publicSystemInfo'] }),
        queryClient.invalidateQueries({ queryKey: ['admin', 'systemInfo'] }),
      ])
      setFeedback({ type: 'success', message: t('admin.settings.savedSuccess') })
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('admin.settings.saveError'),
      })
    } finally {
      setSaving(false)
    }
  }

  const handleSaveEncoding = async (e: React.FormEvent) => {
    e.preventDefault()
    setSavingEncoding(true)
    setFeedback(null)
    try {
      const ffmpegPayload: EncodingFfmpegOptions = {
        HardwareAccelerationMode: Number(hwAccelMode) || 0,
        EnableHardwareEncoding: enableHwEncoding,
        AllowHevcEncoding: allowHevcEncoding,
        H264Crf: Math.min(51, Math.max(0, Math.round(Number(h264Crf) || 23))),
        H264Preset: h264Preset,
        EnableThrottling: enableThrottling,
        ThrottleBufferSize: Math.max(10, Math.round(Number(throttleBufferSize) || 120)),
        TranscodingTempPath: transcodingTempPath.trim(),
      }
      const toneMapPayload: ToneMapOptions = {
        EnableToneMapping: enableToneMapping,
        ToneMappingAlgorithm: toneMappingAlgorithm,
      }
      await Promise.all([
        embyFetch<void>('/emby/Encoding/FfmpegOptions', {
          method: 'POST',
          body: JSON.stringify(ffmpegPayload),
        }),
        embyFetch<void>('/emby/Encoding/FullToneMapOptions', {
          method: 'POST',
          body: JSON.stringify(toneMapPayload),
        }),
      ])
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['admin', 'encodingFfmpegOptions'] }),
        queryClient.invalidateQueries({ queryKey: ['admin', 'encodingToneMapOptions'] }),
        queryClient.invalidateQueries({ queryKey: ['admin', 'systemInfo'] }),
      ])
      setFeedback({ type: 'success', message: t('admin.settings.encodingSavedSuccess') })
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('admin.settings.encodingSaveError'),
      })
    } finally {
      setSavingEncoding(false)
    }
  }

  const hwModeOptions = [
    { value: 0, label: t('admin.settings.hwModeNone') },
    { value: 1, label: t('admin.settings.hwModeAuto') },
    { value: 2, label: t('admin.settings.hwModeQsv') },
    { value: 3, label: t('admin.settings.hwModeNvenc') },
    { value: 4, label: t('admin.settings.hwModeVaapi') },
    { value: 5, label: t('admin.settings.hwModeVideotoolbox') },
    { value: 6, label: t('admin.settings.hwModeAmf') },
  ]

  return (
    <div className="space-y-8">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <h2 className="flex items-center gap-2 text-base font-semibold text-zinc-100">
            <SlidersHorizontal className="h-4 w-4 text-amber-400" />
            <span>{t('admin.settings.generalTitle')}</span>
          </h2>
          <p className="mt-1 text-xs text-zinc-400">{t('admin.settings.generalDesc')}</p>
        </div>

        <button
          type="button"
          onClick={handleRefreshAll}
          className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/[0.04] px-3 py-1.5 text-xs font-medium text-zinc-300 transition-colors hover:border-amber-400/40 hover:text-amber-300"
        >
          <RefreshCw className={`h-3.5 w-3.5 ${isFetching ? 'animate-spin' : ''}`} />
          <span>{t('admin.settings.refreshConfig')}</span>
        </button>
      </div>

      <FeedbackBanner
        message={feedback?.message}
        type={feedback?.type}
        onClose={() => setFeedback(null)}
      />

      {isLoading ? (
        <div className="flex h-48 items-center justify-center rounded-2xl border border-white/8 bg-white/[0.02]">
          <Loader2 className="h-6 w-6 animate-spin text-amber-400" />
        </div>
      ) : isError ? (
        <QueryErrorState
          compact
          title={t('admin.settings.loadError')}
          error={error}
          onRetry={handleRefreshAll}
        />
      ) : (
        <>
          <form onSubmit={handleSave} noValidate className="space-y-6">
            {/* 卡片 1：常规与刮削设置 */}
            <div className="space-y-5 rounded-2xl border border-white/8 bg-white/[0.02] p-5">
              <div>
                <label
                  htmlFor="settings-server-name"
                  className="mb-1.5 block text-xs font-medium text-zinc-300"
                >
                  {t('admin.settings.serverNameLabel')}
                </label>
                <input
                  id="settings-server-name"
                  type="text"
                  required
                  value={serverName}
                  onChange={(e) => setServerName(e.target.value)}
                  placeholder={t('admin.settings.serverNamePlaceholder')}
                  className="w-full rounded-xl border border-white/10 bg-zinc-900/90 px-3.5 py-2.5 text-sm text-zinc-100 placeholder-zinc-500 outline-none transition-colors focus:border-amber-400/60"
                />
              </div>

              <div className="grid gap-4 sm:grid-cols-2">
                <div>
                  <div className="mb-1.5 flex items-center justify-between gap-2">
                    <label
                      htmlFor="settings-tmdb-key"
                      className="block text-xs font-medium text-zinc-300"
                    >
                      {t('admin.settings.tmdbKeyLabel')}
                    </label>
                    {tmdbKeyConfigured && !tmdbKeyCleared && !tmdbApiKey.trim() && (
                      <button
                        type="button"
                        onClick={() => {
                          setTmdbApiKey('')
                          setTmdbKeyCleared(true)
                        }}
                        className="inline-flex items-center gap-1 rounded-md border border-red-500/30 bg-red-500/10 px-2 py-0.5 text-[11px] font-medium text-red-300 transition-colors hover:bg-red-500/20"
                      >
                        <Trash2 className="h-3 w-3" />
                        <span>{t('admin.plugins.clearSecret')}</span>
                      </button>
                    )}
                    {tmdbKeyCleared && (
                      <button
                        type="button"
                        onClick={() => setTmdbKeyCleared(false)}
                        className="inline-flex items-center gap-1 rounded-md border border-amber-500/30 bg-amber-500/10 px-2 py-0.5 text-[11px] font-medium text-amber-300 transition-colors hover:bg-amber-500/20"
                      >
                        <RotateCcw className="h-3 w-3" />
                        <span>{t('admin.plugins.undoClearSecret')}</span>
                      </button>
                    )}
                  </div>
                  <div className="relative">
                    <input
                      id="settings-tmdb-key"
                      type={showApiKey ? 'text' : 'password'}
                      autoComplete="off"
                      value={tmdbApiKey}
                      onChange={(e) => {
                        setTmdbApiKey(e.target.value)
                        if (e.target.value.trim()) setTmdbKeyCleared(false)
                      }}
                      placeholder={
                        tmdbKeyCleared
                          ? t('admin.settings.tmdbKeyClearedPlaceholder')
                          : tmdbKeyConfigured
                            ? t('admin.settings.tmdbKeyConfiguredPlaceholder')
                            : t('admin.settings.tmdbKeyEmptyPlaceholder')
                      }
                      className="w-full rounded-xl border border-white/10 bg-zinc-900/90 py-2.5 pl-3.5 pr-10 font-mono text-sm text-zinc-100 placeholder-zinc-500 outline-none transition-colors focus:border-amber-400/60"
                    />
                    <button
                      type="button"
                      onClick={() => setShowApiKey((v) => !v)}
                      aria-label={
                        showApiKey
                          ? t('admin.settings.hideApiKey')
                          : t('admin.settings.showApiKey')
                      }
                      className="absolute right-2.5 top-1/2 -translate-y-1/2 rounded-lg p-1 text-zinc-400 hover:bg-white/8 hover:text-zinc-200"
                    >
                      {showApiKey ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                    </button>
                  </div>
                  <p className="mt-1.5 text-[11px] text-zinc-500">
                    {t('admin.settings.tmdbKeyHint')}
                  </p>
                </div>

                <div>
                  <label
                    htmlFor="settings-tmdb-lang"
                    className="mb-1.5 block text-xs font-medium text-zinc-300"
                  >
                    {t('admin.settings.tmdbLangLabel')}
                  </label>
                  <select
                    id="settings-tmdb-lang"
                    value={tmdbLanguage}
                    onChange={(e) => setTmdbLanguage(e.target.value)}
                    className="w-full rounded-xl border border-white/10 bg-[#181822] px-3.5 py-2.5 text-sm text-zinc-100 outline-none transition-colors focus:border-amber-400/60"
                  >
                    {TMDB_LANGUAGE_OPTIONS.map((opt) => (
                      <option key={opt.value} value={opt.value}>
                        {opt.label}
                      </option>
                    ))}
                  </select>
                </div>
              </div>

              <ToggleSwitch
                checked={enableStrm302}
                onChange={setEnableStrm302}
                label={t('admin.settings.strm302Label')}
                description={t('admin.settings.strm302Desc')}
              />

              <div className="rounded-xl border border-white/8 bg-white/[0.02] px-4 py-3.5">
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <div>
                    <label
                      htmlFor="settings-played-threshold"
                      className="text-sm font-medium text-zinc-100"
                    >
                      {t('admin.settings.playedThresholdLabel')}
                    </label>
                    <p className="mt-0.5 text-xs text-zinc-400">
                      {t('admin.settings.playedThresholdDesc')}
                    </p>
                  </div>
                  <div className="flex items-center gap-2">
                    <input
                      type="number"
                      aria-label={t('admin.settings.playedThresholdInputAria')}
                      value={Number.isNaN(playedThresholdPct) ? '' : playedThresholdPct}
                      onChange={(e) => setPlayedThresholdPct(Number(e.target.value))}
                      className="w-16 rounded-lg border border-white/10 bg-zinc-900/90 px-2 py-1 text-center font-mono text-xs text-zinc-100 outline-none focus:border-amber-400/60"
                    />
                    <span
                      data-testid="played-threshold-value"
                      className="shrink-0 rounded-lg border border-amber-500/30 bg-amber-500/15 px-2.5 py-1 font-mono text-xs font-semibold text-amber-300"
                    >
                      {playedThresholdPct}%
                    </span>
                  </div>
                </div>
                <div className="mt-3 flex items-center gap-3">
                  <span className="font-mono text-[11px] text-zinc-500">50%</span>
                  <input
                    id="settings-played-threshold"
                    type="range"
                    min={50}
                    max={99}
                    step={1}
                    value={Math.min(99, Math.max(50, Number(playedThresholdPct) || 90))}
                    onChange={(e) => setPlayedThresholdPct(Number(e.target.value))}
                    className="h-1.5 flex-1 cursor-pointer appearance-none rounded-lg bg-white/15 accent-amber-500"
                  />
                  <span className="font-mono text-[11px] text-zinc-500">99%</span>
                </div>
              </div>
            </div>

            {/* 卡片 2：网络、远程访问与协议 */}
            <div
              data-testid="settings-network-card"
              className="space-y-4 rounded-2xl border border-white/8 bg-white/[0.02] p-5"
            >
              <div>
                <h3 className="flex items-center gap-2 text-sm font-semibold text-zinc-100">
                  <Globe className="h-4 w-4 text-amber-400" />
                  <span>{t('admin.settings.networkTitle')}</span>
                </h3>
                <p className="mt-0.5 text-xs text-zinc-400">{t('admin.settings.networkDesc')}</p>
              </div>

              <div className="grid gap-3 md:grid-cols-2">
                <ToggleSwitch
                  checked={enableRemoteAccess}
                  onChange={setEnableRemoteAccess}
                  label={t('admin.settings.remoteAccessLabel')}
                  description={t('admin.settings.remoteAccessDesc')}
                />
                <ToggleSwitch
                  checked={enableQuickConnect}
                  onChange={setEnableQuickConnect}
                  label={t('admin.settings.quickConnectLabel')}
                  description={t('admin.settings.quickConnectDesc')}
                />
              </div>

              <div className="grid gap-4 md:grid-cols-2">
                <ToggleSwitch
                  checked={enableDlna}
                  onChange={setEnableDlna}
                  label={t('admin.settings.dlnaLabel')}
                  description={t('admin.settings.dlnaDesc')}
                />
                <div>
                  <label
                    htmlFor="settings-dlna-user"
                    className="mb-1.5 block text-xs font-medium text-zinc-300"
                  >
                    {t('admin.settings.dlnaDefaultUserLabel')}
                  </label>
                  <select
                    id="settings-dlna-user"
                    disabled={!enableDlna}
                    value={dlnaDefaultUserId}
                    onChange={(e) => setDlnaDefaultUserId(e.target.value)}
                    className="w-full rounded-xl border border-white/10 bg-[#181822] px-3.5 py-2.5 text-sm text-zinc-100 outline-none transition-colors focus:border-amber-400/60 disabled:cursor-not-allowed disabled:opacity-50"
                  >
                    <option value="">{t('admin.settings.dlnaLeastPrivilege')}</option>
                    {users.map((u) => (
                      <option key={u.Id} value={u.Id}>
                        {u.Name}
                      </option>
                    ))}
                  </select>
                </div>
              </div>

              <div className="grid gap-4 md:grid-cols-3">
                <div>
                  <label
                    htmlFor="settings-local-subnets"
                    className="mb-1.5 block text-xs font-medium text-zinc-300"
                  >
                    {t('admin.settings.localSubnetsLabel')}
                  </label>
                  <input
                    id="settings-local-subnets"
                    type="text"
                    value={localSubnetsText}
                    onChange={(e) => setLocalSubnetsText(e.target.value)}
                    placeholder={t('admin.settings.localSubnetsPlaceholder')}
                    className="w-full rounded-xl border border-white/10 bg-zinc-900/90 px-3.5 py-2 text-xs font-mono text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
                  />
                </div>

                <div>
                  <label
                    htmlFor="settings-trusted-proxies"
                    className="mb-1.5 block text-xs font-medium text-zinc-300"
                  >
                    {t('admin.settings.trustedProxiesLabel')}
                  </label>
                  <input
                    id="settings-trusted-proxies"
                    type="text"
                    value={trustedProxiesText}
                    onChange={(e) => setTrustedProxiesText(e.target.value)}
                    placeholder={t('admin.settings.trustedProxiesPlaceholder')}
                    className="w-full rounded-xl border border-white/10 bg-zinc-900/90 px-3.5 py-2 text-xs font-mono text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
                  />
                </div>

                <div>
                  <label
                    htmlFor="settings-allowed-origins"
                    className="mb-1.5 block text-xs font-medium text-zinc-300"
                  >
                    {t('admin.settings.allowedOriginsLabel')}
                  </label>
                  <input
                    id="settings-allowed-origins"
                    type="text"
                    value={allowedOriginsText}
                    onChange={(e) => setAllowedOriginsText(e.target.value)}
                    placeholder={t('admin.settings.allowedOriginsPlaceholder')}
                    className="w-full rounded-xl border border-white/10 bg-zinc-900/90 px-3.5 py-2 text-xs font-mono text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
                  />
                </div>
              </div>

              <div className="grid gap-4 sm:grid-cols-2">
                <div>
                  <label
                    htmlFor="settings-stream-limit"
                    className="mb-1.5 block text-xs font-medium text-zinc-300"
                  >
                    {t('admin.settings.streamLimitLabel')}
                  </label>
                  <input
                    id="settings-stream-limit"
                    type="number"
                    min={0}
                    value={streamLimit}
                    onChange={(e) => setStreamLimit(Number(e.target.value))}
                    className="w-full rounded-xl border border-white/10 bg-zinc-900/90 px-3.5 py-2 text-sm text-zinc-100 outline-none focus:border-amber-400/60"
                  />
                  <p className="mt-1 text-[11px] text-zinc-500">
                    {t('admin.settings.streamLimitHint')}
                  </p>
                </div>

                <div>
                  <label
                    htmlFor="settings-remote-bitrate"
                    className="mb-1.5 block text-xs font-medium text-zinc-300"
                  >
                    {t('admin.settings.remoteBitrateLabel')}
                  </label>
                  <input
                    id="settings-remote-bitrate"
                    type="number"
                    min={0}
                    step={1000000}
                    value={remoteBitrateLimit}
                    onChange={(e) => setRemoteBitrateLimit(Number(e.target.value))}
                    className="w-full rounded-xl border border-white/10 bg-zinc-900/90 px-3.5 py-2 font-mono text-sm text-zinc-100 outline-none focus:border-amber-400/60"
                  />
                  <p className="mt-1 text-[11px] text-zinc-500">
                    {t('admin.settings.remoteBitrateHint')}
                  </p>
                </div>
              </div>
            </div>

            <div className="flex justify-end">
              <button
                type="submit"
                disabled={saving}
                className="inline-flex items-center gap-2 rounded-xl bg-amber-500 px-5 py-2.5 text-sm font-semibold text-zinc-950 shadow-lg shadow-amber-500/20 transition-all hover:bg-amber-400 disabled:opacity-60"
              >
                {saving ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  <Save className="h-4 w-4" />
                )}
                <span>{saving ? t('common.saving') : t('admin.settings.saveBtn')}</span>
              </button>
            </div>
          </form>

          {/* 卡片 3：转码与硬件加速（独立保存表单） */}
          <form
            onSubmit={handleSaveEncoding}
            data-testid="settings-encoding-card"
            className="space-y-5 rounded-2xl border border-white/8 bg-white/[0.02] p-5"
          >
            <div>
              <h3 className="flex items-center gap-2 text-sm font-semibold text-zinc-100">
                <Cpu className="h-4 w-4 text-amber-400" />
                <span>{t('admin.settings.encodingTitle')}</span>
              </h3>
              <p className="mt-0.5 text-xs text-zinc-400">{t('admin.settings.encodingDesc')}</p>
            </div>

            <div className="grid gap-4 sm:grid-cols-3">
              <div>
                <label
                  htmlFor="encoding-hw-mode"
                  className="mb-1.5 block text-xs font-medium text-zinc-300"
                >
                  {t('admin.settings.hwAccelLabel')}
                </label>
                <select
                  id="encoding-hw-mode"
                  value={hwAccelMode}
                  onChange={(e) => setHwAccelMode(Number(e.target.value))}
                  className="w-full rounded-xl border border-white/10 bg-[#181822] px-3.5 py-2.5 text-sm text-zinc-100 outline-none focus:border-amber-400/60"
                >
                  {hwModeOptions.map((opt) => (
                    <option key={opt.value} value={opt.value}>
                      {opt.label}
                    </option>
                  ))}
                </select>
              </div>

              <div>
                <label
                  htmlFor="encoding-h264-crf"
                  className="mb-1.5 block text-xs font-medium text-zinc-300"
                >
                  {t('admin.settings.h264CrfLabel')}
                </label>
                <input
                  id="encoding-h264-crf"
                  type="number"
                  min={0}
                  max={51}
                  value={h264Crf}
                  onChange={(e) => setH264Crf(Number(e.target.value))}
                  className="w-full rounded-xl border border-white/10 bg-zinc-900/90 px-3.5 py-2.5 font-mono text-sm text-zinc-100 outline-none focus:border-amber-400/60"
                />
              </div>

              <div>
                <label
                  htmlFor="encoding-h264-preset"
                  className="mb-1.5 block text-xs font-medium text-zinc-300"
                >
                  {t('admin.settings.h264PresetLabel')}
                </label>
                <select
                  id="encoding-h264-preset"
                  value={h264Preset}
                  onChange={(e) => setH264Preset(e.target.value)}
                  className="w-full rounded-xl border border-white/10 bg-[#181822] px-3.5 py-2.5 text-sm text-zinc-100 outline-none focus:border-amber-400/60"
                >
                  {H264_PRESETS.map((preset) => (
                    <option key={preset} value={preset}>
                      {preset}
                    </option>
                  ))}
                </select>
              </div>
            </div>

            <div className="grid gap-3 md:grid-cols-2">
              <ToggleSwitch
                checked={enableHwEncoding}
                onChange={setEnableHwEncoding}
                label={t('admin.settings.hwEncodingLabel')}
                description={t('admin.settings.hwEncodingDesc')}
              />
              <ToggleSwitch
                checked={allowHevcEncoding}
                onChange={setAllowHevcEncoding}
                label={t('admin.settings.allowHevcLabel')}
                description={t('admin.settings.allowHevcDesc')}
              />
            </div>

            <div className="grid gap-4 md:grid-cols-2">
              <ToggleSwitch
                checked={enableToneMapping}
                onChange={setEnableToneMapping}
                label={t('admin.settings.toneMappingLabel')}
                description={t('admin.settings.toneMappingDesc')}
              />
              <div>
                <label
                  htmlFor="encoding-tonemap-algo"
                  className="mb-1.5 block text-xs font-medium text-zinc-300"
                >
                  {t('admin.settings.toneMappingAlgoLabel')}
                </label>
                <select
                  id="encoding-tonemap-algo"
                  disabled={!enableToneMapping}
                  value={toneMappingAlgorithm}
                  onChange={(e) => setToneMappingAlgorithm(e.target.value)}
                  className="w-full rounded-xl border border-white/10 bg-[#181822] px-3.5 py-2.5 text-sm text-zinc-100 outline-none focus:border-amber-400/60 disabled:cursor-not-allowed disabled:opacity-50"
                >
                  {TONE_MAP_ALGORITHMS.map((algo) => (
                    <option key={algo.value} value={algo.value}>
                      {algo.label}
                    </option>
                  ))}
                </select>
              </div>
            </div>

            <div className="grid gap-4 md:grid-cols-2">
              <ToggleSwitch
                checked={enableThrottling}
                onChange={setEnableThrottling}
                label={t('admin.settings.throttlingLabel')}
                description={t('admin.settings.throttlingDesc')}
              />
              <div>
                <label
                  htmlFor="encoding-throttle-buffer"
                  className="mb-1.5 block text-xs font-medium text-zinc-300"
                >
                  {t('admin.settings.throttleBufferLabel')}
                </label>
                <input
                  id="encoding-throttle-buffer"
                  type="number"
                  min={10}
                  max={1800}
                  disabled={!enableThrottling}
                  value={throttleBufferSize}
                  onChange={(e) => setThrottleBufferSize(Number(e.target.value))}
                  className="w-full rounded-xl border border-white/10 bg-zinc-900/90 px-3.5 py-2.5 font-mono text-sm text-zinc-100 outline-none focus:border-amber-400/60 disabled:cursor-not-allowed disabled:opacity-50"
                />
              </div>
            </div>

            <div>
              <label
                htmlFor="encoding-temp-path"
                className="mb-1.5 block text-xs font-medium text-zinc-300"
              >
                {t('admin.settings.transcodeTempPathLabel')}
              </label>
              <input
                id="encoding-temp-path"
                type="text"
                value={transcodingTempPath}
                onChange={(e) => setTranscodingTempPath(e.target.value)}
                placeholder={t('admin.settings.transcodeTempPathPlaceholder')}
                className="w-full rounded-xl border border-white/10 bg-zinc-900/90 px-3.5 py-2.5 font-mono text-xs text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
              />
            </div>

            <div className="flex justify-end pt-1">
              <button
                type="submit"
                disabled={savingEncoding}
                className="inline-flex items-center gap-2 rounded-xl border border-amber-500/40 bg-amber-500/15 px-5 py-2.5 text-sm font-semibold text-amber-300 transition-all hover:bg-amber-500/25 disabled:opacity-60"
              >
                {savingEncoding ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  <Save className="h-4 w-4" />
                )}
                <span>
                  {savingEncoding ? t('common.saving') : t('admin.settings.saveEncodingBtn')}
                </span>
              </button>
            </div>
          </form>
        </>
      )}
    </div>
  )
}
