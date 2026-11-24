import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2, Puzzle, RefreshCw, SlidersHorizontal } from 'lucide-react'
import { api } from '../../api/client'
import type { PluginInfo } from '../../api/types'
import { AdminModal, FeedbackBanner, ToggleSwitch } from './adminUtils'

const KIND_LABELS: Record<string, string> = {
  metadata: '元数据刮削',
  image: '图像刮削',
  subtitle: '字幕扩展',
  stream: '流解析 / 302',
  notifier: '消息通知',
}

const TMDB_LANGUAGES = [
  { value: 'zh-CN', label: '简体中文 (zh-CN)' },
  { value: 'zh-TW', label: '繁体中文 (zh-TW)' },
  { value: 'en-US', label: 'English (en-US)' },
  { value: 'ja-JP', label: '日本語 (ja-JP)' },
  { value: 'ko-KR', label: '한국어 (ko-KR)' },
]

export function PluginsTab() {
  const queryClient = useQueryClient()
  const [feedback, setFeedback] = useState<{ type: 'success' | 'error'; message: string } | null>(
    null,
  )

  const [editingPlugin, setEditingPlugin] = useState<PluginInfo | null>(null)
  const [loadingConfig, setLoadingConfig] = useState(false)
  const [savingConfig, setSavingConfig] = useState(false)

  // 通用与扩展配置字段
  const [enabled, setEnabled] = useState(true)
  const [priority, setPriority] = useState(10)
  const [tmdbApiKey, setTmdbApiKey] = useState('')
  const [tmdbLanguage, setTmdbLanguage] = useState('zh-CN')
  const [enable302, setEnable302] = useState(true)
  const [extraJson, setExtraJson] = useState('{}')

  const {
    data: plugins = [],
    isLoading,
    isFetching,
    refetch,
  } = useQuery({
    queryKey: ['admin', 'plugins'],
    queryFn: () => api.getPlugins(),
  })

  const isTmdbPlugin = (p: PluginInfo | null) =>
    Boolean(p && `${p.Id} ${p.Name}`.toLowerCase().includes('tmdb'))

  const isStrmPlugin = (p: PluginInfo | null) =>
    Boolean(p && `${p.Id} ${p.Name}`.toLowerCase().includes('strm'))

  const openConfigModal = async (plugin: PluginInfo) => {
    setEditingPlugin(plugin)
    setEnabled(plugin.Enabled !== false)
    setPriority(plugin.Priority ?? 10)
    setTmdbApiKey('')
    setTmdbLanguage('zh-CN')
    setEnable302(true)
    setExtraJson('{}')
    setLoadingConfig(true)

    try {
      const cfg = await api.getPluginConfiguration(plugin.Id)
      if (cfg && typeof cfg === 'object') {
        if (typeof cfg.Enabled === 'boolean') setEnabled(cfg.Enabled)
        else if (typeof cfg.enabled === 'boolean') setEnabled(cfg.enabled)

        if (typeof cfg.Priority === 'number') setPriority(cfg.Priority)
        else if (typeof cfg.priority === 'number') setPriority(cfg.priority)

        const apiKeyVal =
          (cfg.ApiKey as string) ||
          (cfg.api_key as string) ||
          (cfg.TMDBApiKey as string) ||
          (cfg.tmdb_api_key as string) ||
          ''
        if (apiKeyVal) setTmdbApiKey(apiKeyVal)

        const langVal =
          (cfg.Language as string) ||
          (cfg.language as string) ||
          (cfg.TMDBLanguage as string) ||
          ''
        if (langVal) setTmdbLanguage(langVal)

        if (typeof cfg.Enable302 === 'boolean') setEnable302(cfg.Enable302)
        else if (typeof cfg.enable_302 === 'boolean') setEnable302(cfg.enable_302)

        const rest: Record<string, unknown> = { ...cfg }
        delete rest.Enabled
        delete rest.enabled
        delete rest.Priority
        delete rest.priority
        delete rest.ApiKey
        delete rest.api_key
        delete rest.TMDBApiKey
        delete rest.tmdb_api_key
        delete rest.Language
        delete rest.language
        delete rest.TMDBLanguage
        delete rest.Enable302
        delete rest.enable_302
        setExtraJson(Object.keys(rest).length > 0 ? JSON.stringify(rest, null, 2) : '{}')
      }
    } catch {
      // 如果服务端暂未返回已有配置，保留 Manifest 默认值
    } finally {
      setLoadingConfig(false)
    }
  }

  const handleSaveConfig = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!editingPlugin) return

    let parsedExtra: Record<string, unknown> = {}
    if (extraJson.trim()) {
      try {
        const parsed = JSON.parse(extraJson)
        if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
          parsedExtra = parsed as Record<string, unknown>
        } else {
          setFeedback({ type: 'error', message: '高级 JSON 配置必须是对象格式 {}' })
          return
        }
      } catch {
        setFeedback({ type: 'error', message: '高级 JSON 配置语法错误，请检查格式' })
        return
      }
    }

    const payload: Record<string, unknown> = {
      ...parsedExtra,
      Enabled: enabled,
      Priority: Number(priority) || 0,
    }

    if (isTmdbPlugin(editingPlugin)) {
      payload.ApiKey = tmdbApiKey.trim()
      payload.api_key = tmdbApiKey.trim()
      payload.Language = tmdbLanguage
      payload.language = tmdbLanguage
    }

    if (isStrmPlugin(editingPlugin)) {
      payload.Enable302 = enable302
      payload.enable_302 = enable302
    }

    setSavingConfig(true)
    setFeedback(null)
    try {
      await api.updatePluginConfiguration(editingPlugin.Id, payload)
      await queryClient.invalidateQueries({ queryKey: ['admin', 'plugins'] })
      setFeedback({ type: 'success', message: `已保存插件「${editingPlugin.Name}」配置` })
      setEditingPlugin(null)
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '保存插件配置失败',
      })
    } finally {
      setSavingConfig(false)
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <h2 className="flex items-center gap-2 text-base font-semibold text-zinc-100">
            <Puzzle className="h-4 w-4 text-amber-400" />
            <span>插件与刮削器管理</span>
          </h2>
          <p className="mt-1 text-xs text-zinc-400">
            管理内置及外部扩展插件的启用状态、执行优先级与刮削参数（如 TMDB API Key）
          </p>
        </div>

        <button
          type="button"
          onClick={() => void refetch()}
          className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/[0.04] px-3 py-1.5 text-xs font-medium text-zinc-300 transition-colors hover:border-amber-400/40 hover:text-amber-300"
        >
          <RefreshCw className={`h-3.5 w-3.5 ${isFetching ? 'animate-spin' : ''}`} />
          <span>刷新列表</span>
        </button>
      </div>

      <FeedbackBanner
        message={feedback?.message}
        type={feedback?.type}
        onClose={() => setFeedback(null)}
      />

      {isLoading ? (
        <div className="flex h-40 items-center justify-center rounded-2xl border border-white/8 bg-white/[0.02]">
          <Loader2 className="h-6 w-6 animate-spin text-amber-400" />
        </div>
      ) : plugins.length === 0 ? (
        <div className="rounded-2xl border border-white/8 bg-white/[0.02] py-12 text-center text-xs text-zinc-500">
          暂无已注册插件
        </div>
      ) : (
        <div className="grid gap-4 md:grid-cols-2">
          {plugins.map((plugin) => {
            const isEnabled = plugin.Enabled !== false
            return (
              <div
                key={plugin.Id}
                className="flex flex-col justify-between rounded-2xl border border-white/8 bg-white/[0.03] p-4 transition-colors hover:border-white/15"
              >
                <div>
                  <div className="flex items-start justify-between gap-2">
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-1.5">
                        <h3 className="truncate text-sm font-semibold text-zinc-100">
                          {plugin.Name}
                        </h3>
                        {plugin.Version && (
                          <span className="rounded bg-white/8 px-1.5 py-0.5 font-mono text-[10px] text-zinc-300">
                            v{plugin.Version}
                          </span>
                        )}
                        <span
                          className={`rounded px-1.5 py-0.5 text-[10px] font-semibold ${
                            plugin.BuiltIn
                              ? 'bg-sky-500/15 text-sky-300 ring-1 ring-sky-500/30'
                              : 'bg-purple-500/15 text-purple-300 ring-1 ring-purple-500/30'
                          }`}
                        >
                          {plugin.BuiltIn ? '内置' : '外部'}
                        </span>
                      </div>
                      <p className="mt-0.5 font-mono text-[11px] text-zinc-500">{plugin.Id}</p>
                    </div>

                    <span
                      className={`shrink-0 rounded-full px-2 py-0.5 text-[10px] font-semibold ${
                        isEnabled
                          ? 'bg-emerald-500/15 text-emerald-300 ring-1 ring-emerald-500/30'
                          : 'bg-zinc-800 text-zinc-400 ring-1 ring-white/10'
                      }`}
                    >
                      {isEnabled ? '已启用' : '已停用'}
                    </span>
                  </div>

                  {plugin.Description && (
                    <p className="mt-2.5 text-xs leading-relaxed text-zinc-400">
                      {plugin.Description}
                    </p>
                  )}

                  <div className="mt-3 flex flex-wrap items-center gap-1.5">
                    {(plugin.Kinds || []).map((k) => (
                      <span
                        key={k}
                        className="rounded-md bg-amber-500/12 px-2 py-0.5 text-[11px] font-medium text-amber-300"
                      >
                        {KIND_LABELS[k.toLowerCase()] || k}
                      </span>
                    ))}
                    <span className="rounded-md bg-white/[0.05] px-2 py-0.5 text-[11px] text-zinc-400">
                      优先级: {plugin.Priority ?? 0}
                    </span>
                  </div>
                </div>

                <div className="mt-4 flex items-center justify-between border-t border-white/6 pt-3">
                  <span className="text-[11px] text-zinc-500">
                    {plugin.Author ? `作者: ${plugin.Author}` : 'Ambra Plugin'}
                  </span>
                  <button
                    type="button"
                    onClick={() => void openConfigModal(plugin)}
                    className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/[0.05] px-3 py-1.5 text-xs font-medium text-zinc-200 transition-colors hover:border-amber-400/40 hover:text-amber-300"
                  >
                    <SlidersHorizontal className="h-3.5 w-3.5" />
                    <span>编辑配置</span>
                  </button>
                </div>
              </div>
            )
          })}
        </div>
      )}

      {/* 插件配置弹窗 */}
      <AdminModal
        open={Boolean(editingPlugin)}
        title={`配置插件 · ${editingPlugin?.Name || ''}`}
        subtitle={`ID: ${editingPlugin?.Id || ''}`}
        maxWidthClass="max-w-lg"
        onClose={() => setEditingPlugin(null)}
      >
        {loadingConfig ? (
          <div className="flex h-36 items-center justify-center">
            <Loader2 className="h-6 w-6 animate-spin text-amber-400" />
          </div>
        ) : (
          <form onSubmit={handleSaveConfig} className="space-y-4">
            <ToggleSwitch
              checked={enabled}
              onChange={setEnabled}
              label="启用插件"
              description="关闭后扫描与播放流程将跳过此插件"
            />

            <div>
              <label className="mb-1.5 block text-xs font-medium text-zinc-400">
                执行优先级（数字越小越优先）
              </label>
              <input
                type="number"
                value={priority}
                onChange={(e) => setPriority(Number(e.target.value))}
                className="w-full rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2 text-sm text-zinc-100 outline-none focus:border-amber-400/60"
              />
            </div>

            {isTmdbPlugin(editingPlugin) && (
              <div className="space-y-3 rounded-xl border border-amber-500/20 bg-amber-500/[0.04] p-4">
                <p className="text-xs font-semibold text-amber-300">TMDB 刮削器参数</p>
                <div>
                  <label className="mb-1.5 block text-xs font-medium text-zinc-400">
                    TMDB API Key
                  </label>
                  <input
                    type="text"
                    value={tmdbApiKey}
                    onChange={(e) => setTmdbApiKey(e.target.value)}
                    placeholder="输入 TMDB v3 API Key"
                    className="w-full rounded-xl border border-white/10 bg-black/40 px-3.5 py-2 font-mono text-xs text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
                  />
                </div>
                <div>
                  <label className="mb-1.5 block text-xs font-medium text-zinc-400">
                    首选元数据语言
                  </label>
                  <select
                    value={tmdbLanguage}
                    onChange={(e) => setTmdbLanguage(e.target.value)}
                    className="w-full rounded-xl border border-white/10 bg-[#181822] px-3.5 py-2 text-sm text-zinc-100 outline-none focus:border-amber-400/60"
                  >
                    {TMDB_LANGUAGES.map((lang) => (
                      <option key={lang.value} value={lang.value}>
                        {lang.label}
                      </option>
                    ))}
                  </select>
                </div>
              </div>
            )}

            {isStrmPlugin(editingPlugin) && (
              <ToggleSwitch
                checked={enable302}
                onChange={setEnable302}
                label="启用 STRM 302 直链重定向"
                description="播放 STRM 媒体时直接返回 302 跳转至云盘直链，节省服务端流量"
              />
            )}

            <div>
              <label className="mb-1.5 block text-xs font-medium text-zinc-400">
                扩展配置 (JSON)
              </label>
              <textarea
                rows={4}
                value={extraJson}
                onChange={(e) => setExtraJson(e.target.value)}
                spellCheck={false}
                className="w-full rounded-xl border border-white/10 bg-black/40 p-3 font-mono text-xs text-zinc-200 outline-none focus:border-amber-400/60"
              />
            </div>

            <div className="flex justify-end gap-2 pt-2">
              <button
                type="button"
                onClick={() => setEditingPlugin(null)}
                className="rounded-xl border border-white/10 px-4 py-2 text-xs font-medium text-zinc-300 hover:bg-white/5"
              >
                取消
              </button>
              <button
                type="submit"
                disabled={savingConfig}
                className="inline-flex items-center gap-2 rounded-xl bg-amber-500 px-4 py-2 text-xs font-semibold text-zinc-950 hover:bg-amber-400 disabled:opacity-60"
              >
                {savingConfig && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
                <span>保存配置</span>
              </button>
            </div>
          </form>
        )}
      </AdminModal>
    </div>
  )
}
