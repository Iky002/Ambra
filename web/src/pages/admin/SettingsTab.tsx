import { useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Eye, EyeOff, Loader2, RefreshCw, Save, SlidersHorizontal } from 'lucide-react'
import { api } from '../../api/client'
import { FeedbackBanner, ToggleSwitch } from './adminUtils'

const TMDB_LANGUAGE_OPTIONS = [
  { value: 'zh-CN', label: '简体中文 (zh-CN)' },
  { value: 'zh-TW', label: '繁体中文 (zh-TW)' },
  { value: 'en-US', label: 'English (en-US)' },
  { value: 'ja-JP', label: '日本語 (ja-JP)' },
] as const

export function SettingsTab() {
  const queryClient = useQueryClient()
  const [feedback, setFeedback] = useState<{ type: 'success' | 'error'; message: string } | null>(
    null,
  )

  const [serverName, setServerName] = useState('')
  const [tmdbApiKey, setTmdbApiKey] = useState('')
  const [showApiKey, setShowApiKey] = useState(false)
  const [tmdbLanguage, setTmdbLanguage] = useState('zh-CN')
  const [enableStrm302, setEnableStrm302] = useState(true)
  const [playedThresholdPct, setPlayedThresholdPct] = useState(90)
  const [saving, setSaving] = useState(false)

  const {
    data: config,
    isLoading,
    isFetching,
    refetch,
  } = useQuery({
    queryKey: ['admin', 'serverConfig'],
    queryFn: () => api.getServerConfig(),
  })

  useEffect(() => {
    if (!config) return
    setServerName(config.ServerName || '')
    setTmdbApiKey(config.TMDBApiKey || '')
    setTmdbLanguage(config.TMDBLanguage || 'zh-CN')
    setEnableStrm302(config.EnableSTRM302 ?? true)
    const rawPct =
      typeof config.PlayedThreshold === 'number' && config.PlayedThreshold > 0
        ? Math.round(config.PlayedThreshold * 100)
        : 90
    setPlayedThresholdPct(Math.min(98, Math.max(50, rawPct)))
  }, [config])

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault()
    const trimmedName = serverName.trim()
    if (!trimmedName) {
      setFeedback({ type: 'error', message: '服务器名称不能为空' })
      return
    }

    setSaving(true)
    setFeedback(null)
    try {
      await api.updateServerConfig({
        ServerName: trimmedName,
        TMDBApiKey: tmdbApiKey.trim(),
        TMDBLanguage: tmdbLanguage,
        EnableSTRM302: enableStrm302,
        PlayedThreshold: Number((playedThresholdPct / 100).toFixed(2)),
      })
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['admin', 'serverConfig'] }),
        queryClient.invalidateQueries({ queryKey: ['publicSystemInfo'] }),
        queryClient.invalidateQueries({ queryKey: ['systemInfo'] }),
      ])
      setFeedback({ type: 'success', message: '已保存服务器常规设置' })
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '保存服务器设置失败',
      })
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <h2 className="flex items-center gap-2 text-base font-semibold text-zinc-100">
            <SlidersHorizontal className="h-4 w-4 text-amber-400" />
            <span>常规设置</span>
          </h2>
          <p className="mt-1 text-xs text-zinc-400">
            配置服务器名称、TMDB 刮削参数、STRM 302 直链重定向以及已看状态进度阈值
          </p>
        </div>

        <button
          type="button"
          onClick={() => void refetch()}
          className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/[0.04] px-3 py-1.5 text-xs font-medium text-zinc-300 transition-colors hover:border-amber-400/40 hover:text-amber-300"
        >
          <RefreshCw className={`h-3.5 w-3.5 ${isFetching ? 'animate-spin' : ''}`} />
          <span>刷新配置</span>
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
      ) : (
        <form onSubmit={handleSave} className="space-y-5">
          <div className="rounded-2xl border border-white/8 bg-white/[0.02] p-5 space-y-5">
            {/* 服务器名称 */}
            <div>
              <label
                htmlFor="settings-server-name"
                className="mb-1.5 block text-xs font-medium text-zinc-300"
              >
                服务器名称 *
              </label>
              <input
                id="settings-server-name"
                type="text"
                required
                value={serverName}
                onChange={(e) => setServerName(e.target.value)}
                placeholder="例如：Ambra 家庭影院"
                className="w-full rounded-xl border border-white/10 bg-zinc-900/90 px-3.5 py-2.5 text-sm text-zinc-100 placeholder-zinc-500 outline-none transition-colors focus:border-amber-400/60"
              />
            </div>

            {/* TMDB API Key + 首选刮削语言 */}
            <div className="grid gap-4 sm:grid-cols-2">
              <div>
                <label
                  htmlFor="settings-tmdb-key"
                  className="mb-1.5 block text-xs font-medium text-zinc-300"
                >
                  TMDB API Key
                </label>
                <div className="relative">
                  <input
                    id="settings-tmdb-key"
                    type={showApiKey ? 'text' : 'password'}
                    value={tmdbApiKey}
                    onChange={(e) => setTmdbApiKey(e.target.value)}
                    placeholder="输入 TMDB v3 API Key"
                    className="w-full rounded-xl border border-white/10 bg-zinc-900/90 py-2.5 pl-3.5 pr-10 font-mono text-sm text-zinc-100 placeholder-zinc-500 outline-none transition-colors focus:border-amber-400/60"
                  />
                  <button
                    type="button"
                    onClick={() => setShowApiKey((v) => !v)}
                    aria-label={showApiKey ? '隐藏 API Key' : '显示 API Key'}
                    className="absolute right-2.5 top-1/2 -translate-y-1/2 rounded-lg p-1 text-zinc-400 hover:bg-white/8 hover:text-zinc-200"
                  >
                    {showApiKey ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                  </button>
                </div>
              </div>

              <div>
                <label
                  htmlFor="settings-tmdb-lang"
                  className="mb-1.5 block text-xs font-medium text-zinc-300"
                >
                  首选刮削语言
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

            {/* 启用 STRM 302 直链重定向 */}
            <ToggleSwitch
              checked={enableStrm302}
              onChange={setEnableStrm302}
              label="启用 STRM 302 直链重定向"
              description="播放 .strm 网盘/远程直链文件时，通过 HTTP 302 直接重定向至源地址播放，避免服务器中转带宽消耗"
            />

            {/* 标记为已看进度阈值 */}
            <div className="rounded-xl border border-white/8 bg-white/[0.02] px-4 py-3.5">
              <div className="flex items-center justify-between gap-3">
                <div>
                  <label
                    htmlFor="settings-played-threshold"
                    className="text-sm font-medium text-zinc-100"
                  >
                    标记为已看进度阈值
                  </label>
                  <p className="mt-0.5 text-xs text-zinc-400">
                    影片或剧集播放进度超过此比例后自动标记为「已看」
                  </p>
                </div>
                <span
                  data-testid="played-threshold-value"
                  className="shrink-0 rounded-lg border border-amber-500/30 bg-amber-500/15 px-2.5 py-1 font-mono text-xs font-semibold text-amber-300"
                >
                  {playedThresholdPct}%
                </span>
              </div>
              <div className="mt-3 flex items-center gap-3">
                <span className="font-mono text-[11px] text-zinc-500">50%</span>
                <input
                  id="settings-played-threshold"
                  type="range"
                  min={50}
                  max={98}
                  step={1}
                  value={playedThresholdPct}
                  onChange={(e) => setPlayedThresholdPct(Number(e.target.value))}
                  className="h-1.5 flex-1 cursor-pointer appearance-none rounded-lg bg-white/15 accent-amber-500"
                />
                <span className="font-mono text-[11px] text-zinc-500">98%</span>
              </div>
            </div>
          </div>

          <div className="flex justify-end">
            <button
              type="submit"
              disabled={saving}
              className="inline-flex items-center gap-2 rounded-xl bg-amber-500 px-5 py-2.5 text-sm font-semibold text-zinc-950 shadow-lg shadow-amber-500/20 transition-all hover:bg-amber-400 disabled:opacity-60"
            >
              {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
              <span>{saving ? '保存中...' : '保存设置'}</span>
            </button>
          </div>
        </form>
      )}
    </div>
  )
}
