import { useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2, Puzzle, RefreshCw, RotateCcw, SlidersHorizontal, Trash2 } from 'lucide-react'
import { api } from '../../api/client'
import type { PluginInfo } from '../../api/types'
import { QueryErrorState } from '../../components/QueryErrorState'
import { useI18n } from '../../i18n/I18nContext'
import { AdminModal, FeedbackBanner, ToggleSwitch } from './adminUtils'

export const MASKED_PLUGIN_SECRET = '********'

function resolveSecretForSubmit(input: string, configured: boolean, cleared: boolean): string {
  const trimmed = input.trim()
  if (trimmed !== '') return trimmed
  if (cleared) return ''
  if (configured) return MASKED_PLUGIN_SECRET
  return ''
}

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
  { value: 'es-ES', label: 'Español (es-ES)' },
  { value: 'de-DE', label: 'Deutsch (de-DE)' },
  { value: 'fr-FR', label: 'Français (fr-FR)' },
  { value: 'pt-BR', label: 'Português (pt-BR)' },
  { value: 'th-TH', label: 'ไทย (th-TH)' },
]

const TVDB_LANGUAGES = [
  { value: 'zh-CN', label: '简体中文 (zh-CN)' },
  { value: 'zh-TW', label: '繁体中文 (zh-TW)' },
  { value: 'en-US', label: 'English (en-US)' },
  { value: 'ja-JP', label: '日本語 (ja-JP)' },
  { value: 'ko-KR', label: '한국어 (ko-KR)' },
  { value: 'es-ES', label: 'Español (es-ES)' },
  { value: 'de-DE', label: 'Deutsch (de-DE)' },
  { value: 'fr-FR', label: 'Français (fr-FR)' },
  { value: 'pt-BR', label: 'Português (pt-BR)' },
  { value: 'th-TH', label: 'ไทย (th-TH)' },
]

export function PluginsTab() {
  const queryClient = useQueryClient()
  const { t } = useI18n()
  const [feedback, setFeedback] = useState<{ type: 'success' | 'error'; message: string } | null>(
    null,
  )

  const [editingPlugin, setEditingPlugin] = useState<PluginInfo | null>(null)
  const [loadingConfig, setLoadingConfig] = useState(false)
  const [savingConfig, setSavingConfig] = useState(false)
  const [modalError, setModalError] = useState<string | null>(null)
  const loadSeqRef = useRef(0)

  // 通用与扩展配置字段
  const [enabled, setEnabled] = useState(true)
  const [priority, setPriority] = useState(10)
  const [tmdbApiKey, setTmdbApiKey] = useState('')
  const [tmdbApiKeyConfigured, setTmdbApiKeyConfigured] = useState(false)
  const [tmdbApiKeyCleared, setTmdbApiKeyCleared] = useState(false)
  const [tmdbLanguage, setTmdbLanguage] = useState('zh-CN')
  const [tvdbApiKey, setTvdbApiKey] = useState('')
  const [tvdbApiKeyConfigured, setTvdbApiKeyConfigured] = useState(false)
  const [tvdbApiKeyCleared, setTvdbApiKeyCleared] = useState(false)
  const [tvdbPin, setTvdbPin] = useState('')
  const [tvdbPinConfigured, setTvdbPinConfigured] = useState(false)
  const [tvdbPinCleared, setTvdbPinCleared] = useState(false)
  const [tvdbLanguage, setTvdbLanguage] = useState('zh-CN')
  const [enable302, setEnable302] = useState(true)
  const [extraJson, setExtraJson] = useState('{}')

  const {
    data: plugins = [],
    isLoading,
    isFetching,
    isError,
    error,
    refetch,
  } = useQuery({
    queryKey: ['admin', 'plugins'],
    queryFn: () => api.getPlugins(),
  })

  const isTmdbPlugin = (p: PluginInfo | null) =>
    Boolean(p && `${p.Id} ${p.Name}`.toLowerCase().includes('tmdb'))

  const isTvdbPlugin = (p: PluginInfo | null) =>
    Boolean(p && `${p.Id} ${p.Name}`.toLowerCase().includes('tvdb'))

  const isTraktPlugin = (p: PluginInfo | null) =>
    Boolean(p && `${p.Id} ${p.Name}`.toLowerCase().includes('trakt'))

  const isStrmPlugin = (p: PluginInfo | null) =>
    Boolean(p && `${p.Id} ${p.Name}`.toLowerCase().includes('strm'))

  const closeConfigModal = () => {
    loadSeqRef.current += 1
    setEditingPlugin(null)
    setLoadingConfig(false)
    setModalError(null)
  }

  const openConfigModal = async (plugin: PluginInfo) => {
    const seq = ++loadSeqRef.current
    setEditingPlugin(plugin)
    setModalError(null)
    setEnabled(plugin.Enabled !== false)
    setPriority(plugin.Priority ?? 10)
    setTmdbApiKey('')
    setTmdbApiKeyConfigured(false)
    setTmdbApiKeyCleared(false)
    setTmdbLanguage('zh-CN')
    setTvdbApiKey('')
    setTvdbApiKeyConfigured(false)
    setTvdbApiKeyCleared(false)
    setTvdbPin('')
    setTvdbPinConfigured(false)
    setTvdbPinCleared(false)
    setTvdbLanguage('zh-CN')
    setEnable302(true)
    setExtraJson('{}')
    setLoadingConfig(true)

    try {
      const cfg = await api.getPluginConfiguration(plugin.Id)
      if (seq !== loadSeqRef.current) return
      if (cfg && typeof cfg === 'object') {
        if (typeof cfg.enabled === 'boolean') setEnabled(cfg.enabled)
        else if (typeof cfg.Enabled === 'boolean') setEnabled(cfg.Enabled)

        if (typeof cfg.priority === 'number') setPriority(cfg.priority)
        else if (typeof cfg.Priority === 'number') setPriority(cfg.Priority)

        const rest: Record<string, unknown> = { ...cfg }
        delete rest.Enabled
        delete rest.enabled
        delete rest.Priority
        delete rest.priority

        if (isTmdbPlugin(plugin)) {
          const apiKeyVal =
            (cfg.api_key as string) ||
            (cfg.ApiKey as string) ||
            (cfg.tmdb_api_key as string) ||
            (cfg.TMDBApiKey as string) ||
            ''
          if (apiKeyVal === MASKED_PLUGIN_SECRET) {
            setTmdbApiKey('')
            setTmdbApiKeyConfigured(true)
          } else if (apiKeyVal) {
            setTmdbApiKey(apiKeyVal)
          }

          const langVal =
            (cfg.language as string) ||
            (cfg.Language as string) ||
            (cfg.TMDBLanguage as string) ||
            ''
          if (langVal) setTmdbLanguage(langVal)

          delete rest.ApiKey
          delete rest.api_key
          delete rest.TMDBApiKey
          delete rest.tmdb_api_key
          delete rest.Language
          delete rest.language
          delete rest.TMDBLanguage
        }

        if (isTvdbPlugin(plugin)) {
          const tvdbKeyVal =
            (cfg.api_key as string) ||
            (cfg.ApiKey as string) ||
            (cfg.tvdb_api_key as string) ||
            (cfg.TVDBApiKey as string) ||
            ''
          if (tvdbKeyVal === MASKED_PLUGIN_SECRET) {
            setTvdbApiKey('')
            setTvdbApiKeyConfigured(true)
          } else if (tvdbKeyVal) {
            setTvdbApiKey(tvdbKeyVal)
          }

          const tvdbPinVal =
            (cfg.pin as string) ||
            (cfg.Pin as string) ||
            (cfg.tvdb_pin as string) ||
            (cfg.TVDBPin as string) ||
            ''
          if (tvdbPinVal === MASKED_PLUGIN_SECRET) {
            setTvdbPin('')
            setTvdbPinConfigured(true)
          } else if (tvdbPinVal) {
            setTvdbPin(tvdbPinVal)
          }

          const tvdbLangVal =
            (cfg.language as string) ||
            (cfg.Language as string) ||
            (cfg.tvdb_language as string) ||
            (cfg.TVDBLanguage as string) ||
            ''
          if (tvdbLangVal) setTvdbLanguage(tvdbLangVal)

          delete rest.ApiKey
          delete rest.api_key
          delete rest.Language
          delete rest.language
          delete rest.pin
          delete rest.Pin
          delete rest.tvdb_pin
          delete rest.TVDBPin
          delete rest.tvdb_api_key
          delete rest.TVDBApiKey
          delete rest.tvdb_language
          delete rest.TVDBLanguage
        }

        if (isStrmPlugin(plugin)) {
          if (typeof cfg.enable_302 === 'boolean') setEnable302(cfg.enable_302)
          else if (typeof cfg.Enable302 === 'boolean') setEnable302(cfg.Enable302)

          delete rest.Enable302
          delete rest.enable_302
        }

        if (isTraktPlugin(plugin)) {
          delete rest.client_id
          delete rest.ClientId
          delete rest.ClientID
          delete rest.access_token
          delete rest.AccessToken
          delete rest.scrobble_playback
          delete rest.ScrobblePlayback
          delete rest.sync_watched
          delete rest.SyncWatched
        }
        setExtraJson(Object.keys(rest).length > 0 ? JSON.stringify(rest, null, 2) : '{}')
      }
    } catch {
      if (seq !== loadSeqRef.current) return
      // 如果服务端暂未返回已有配置，保留 Manifest 默认值
    } finally {
      if (seq === loadSeqRef.current) {
        setLoadingConfig(false)
      }
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
          setModalError('高级 JSON 配置必须是对象格式 {}')
          return
        }
      } catch {
        setModalError('高级 JSON 配置语法错误，请检查格式')
        return
      }
    }

    const payload: Record<string, unknown> = {
      ...parsedExtra,
      enabled,
      Enabled: enabled,
      priority: Number(priority) || 0,
      Priority: Number(priority) || 0,
    }

    if (isTmdbPlugin(editingPlugin)) {
      const nextTmdbKey = resolveSecretForSubmit(
        tmdbApiKey,
        tmdbApiKeyConfigured,
        tmdbApiKeyCleared,
      )
      payload.ApiKey = nextTmdbKey
      payload.api_key = nextTmdbKey
      payload.Language = tmdbLanguage
      payload.language = tmdbLanguage
    }

    if (isTvdbPlugin(editingPlugin)) {
      const nextTvdbKey = resolveSecretForSubmit(
        tvdbApiKey,
        tvdbApiKeyConfigured,
        tvdbApiKeyCleared,
      )
      const nextTvdbPin = resolveSecretForSubmit(tvdbPin, tvdbPinConfigured, tvdbPinCleared)
      payload.ApiKey = nextTvdbKey
      payload.api_key = nextTvdbKey
      payload.Pin = nextTvdbPin
      payload.pin = nextTvdbPin
      payload.Language = tvdbLanguage
      payload.language = tvdbLanguage
    }

    if (isTraktPlugin(editingPlugin)) {
      for (const key of Object.keys(payload)) {
        if (!['Enabled', 'enabled', 'Priority', 'priority'].includes(key)) delete payload[key]
      }
    }

    if (isStrmPlugin(editingPlugin)) {
      payload.Enable302 = enable302
      payload.enable_302 = enable302
    }

    setSavingConfig(true)
    setModalError(null)
    setFeedback(null)
    try {
      await api.updatePluginConfiguration(editingPlugin.Id, payload)
      await queryClient.invalidateQueries({ queryKey: ['admin', 'plugins'] })
      setFeedback({ type: 'success', message: `已保存插件「${editingPlugin.Name}」配置` })
      closeConfigModal()
    } catch (err) {
      setModalError(err instanceof Error ? err.message : '保存插件配置失败')
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
            <span>{t('admin.pluginsTitle')}</span>
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
      ) : isError ? (
        <QueryErrorState
          compact
          title="无法加载插件列表"
          error={error}
          onRetry={() => void refetch()}
        />
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
        onClose={closeConfigModal}
      >
        {loadingConfig ? (
          <div className="flex h-36 items-center justify-center">
            <Loader2 className="h-6 w-6 animate-spin text-amber-400" />
          </div>
        ) : (
          <form onSubmit={handleSaveConfig} className="space-y-4">
            <FeedbackBanner
              message={modalError ?? undefined}
              type="error"
              onClose={() => setModalError(null)}
            />
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
                  <div className="mb-1.5 flex items-center justify-between gap-2">
                    <label htmlFor="tmdb-api-key" className="block text-xs font-medium text-zinc-400">
                      TMDB API Key
                    </label>
                    {tmdbApiKeyConfigured && !tmdbApiKeyCleared && !tmdbApiKey.trim() && (
                      <button
                        type="button"
                        onClick={() => {
                          setTmdbApiKey('')
                          setTmdbApiKeyCleared(true)
                        }}
                        className="inline-flex items-center gap-1 rounded-md border border-red-500/30 bg-red-500/10 px-2 py-0.5 text-[11px] font-medium text-red-300 transition-colors hover:bg-red-500/20"
                      >
                        <Trash2 className="h-3 w-3" />
                        <span>清除密钥</span>
                      </button>
                    )}
                    {tmdbApiKeyCleared && (
                      <button
                        type="button"
                        onClick={() => setTmdbApiKeyCleared(false)}
                        className="inline-flex items-center gap-1 rounded-md border border-amber-500/30 bg-amber-500/10 px-2 py-0.5 text-[11px] font-medium text-amber-300 transition-colors hover:bg-amber-500/20"
                      >
                        <RotateCcw className="h-3 w-3" />
                        <span>撤销清除</span>
                      </button>
                    )}
                  </div>
                  <input
                    id="tmdb-api-key"
                    type="password"
                    autoComplete="off"
                    value={tmdbApiKey}
                    onChange={(e) => {
                      setTmdbApiKey(e.target.value)
                      if (e.target.value.trim()) setTmdbApiKeyCleared(false)
                    }}
                    placeholder={
                      tmdbApiKeyCleared
                        ? '已标记清除自定义密钥，保存后将恢复使用内置默认密钥'
                        : tmdbApiKeyConfigured
                          ? '已配置密钥，留空保留原值'
                          : '留空将使用内置默认密钥，或输入自定义 TMDB v3 API Key'
                    }
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

            {isTvdbPlugin(editingPlugin) && (
              <div className="space-y-3 rounded-xl border border-sky-500/20 bg-sky-500/[0.04] p-4">
                <p className="text-xs font-semibold text-sky-300">TheTVDB 刮削器参数</p>
                <p className="text-xs leading-relaxed text-zinc-400">
                  默认关闭；启用并填写 TheTVDB v4 API Key 后，将按优先级作为剧集/电影刮削补充源
                </p>
                <div>
                  <div className="mb-1.5 flex items-center justify-between gap-2">
                    <label
                      htmlFor="tvdb-api-key"
                      className="block text-xs font-medium text-zinc-400"
                    >
                      API Key
                    </label>
                    {tvdbApiKeyConfigured && !tvdbApiKeyCleared && !tvdbApiKey.trim() && (
                      <button
                        type="button"
                        onClick={() => {
                          setTvdbApiKey('')
                          setTvdbApiKeyCleared(true)
                        }}
                        className="inline-flex items-center gap-1 rounded-md border border-red-500/30 bg-red-500/10 px-2 py-0.5 text-[11px] font-medium text-red-300 transition-colors hover:bg-red-500/20"
                      >
                        <Trash2 className="h-3 w-3" />
                        <span>清除密钥</span>
                      </button>
                    )}
                    {tvdbApiKeyCleared && (
                      <button
                        type="button"
                        onClick={() => setTvdbApiKeyCleared(false)}
                        className="inline-flex items-center gap-1 rounded-md border border-amber-500/30 bg-amber-500/10 px-2 py-0.5 text-[11px] font-medium text-amber-300 transition-colors hover:bg-amber-500/20"
                      >
                        <RotateCcw className="h-3 w-3" />
                        <span>撤销清除</span>
                      </button>
                    )}
                  </div>
                  <input
                    id="tvdb-api-key"
                    type="password"
                    autoComplete="off"
                    value={tvdbApiKey}
                    onChange={(e) => {
                      setTvdbApiKey(e.target.value)
                      if (e.target.value.trim()) setTvdbApiKeyCleared(false)
                    }}
                    placeholder={
                      tvdbApiKeyCleared
                        ? '已标记清除密钥，保存后将清空原值'
                        : tvdbApiKeyConfigured
                          ? '已配置密钥，留空保留原值'
                          : '输入 TheTVDB v4 API Key'
                    }
                    className="w-full rounded-xl border border-white/10 bg-black/40 px-3.5 py-2 font-mono text-xs text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
                  />
                </div>
                <div>
                  <div className="mb-1.5 flex items-center justify-between gap-2">
                    <label
                      htmlFor="tvdb-pin"
                      className="block text-xs font-medium text-zinc-400"
                    >
                      Subscriber PIN（可选）
                    </label>
                    {tvdbPinConfigured && !tvdbPinCleared && !tvdbPin.trim() && (
                      <button
                        type="button"
                        onClick={() => {
                          setTvdbPin('')
                          setTvdbPinCleared(true)
                        }}
                        className="inline-flex items-center gap-1 rounded-md border border-red-500/30 bg-red-500/10 px-2 py-0.5 text-[11px] font-medium text-red-300 transition-colors hover:bg-red-500/20"
                      >
                        <Trash2 className="h-3 w-3" />
                        <span>清除密钥</span>
                      </button>
                    )}
                    {tvdbPinCleared && (
                      <button
                        type="button"
                        onClick={() => setTvdbPinCleared(false)}
                        className="inline-flex items-center gap-1 rounded-md border border-amber-500/30 bg-amber-500/10 px-2 py-0.5 text-[11px] font-medium text-amber-300 transition-colors hover:bg-amber-500/20"
                      >
                        <RotateCcw className="h-3 w-3" />
                        <span>撤销清除</span>
                      </button>
                    )}
                  </div>
                  <input
                    id="tvdb-pin"
                    type="text"
                    value={tvdbPin}
                    onChange={(e) => {
                      setTvdbPin(e.target.value)
                      if (e.target.value.trim()) setTvdbPinCleared(false)
                    }}
                    placeholder={
                      tvdbPinCleared
                        ? '已标记清除密钥，保存后将清空原值'
                        : tvdbPinConfigured
                          ? '已配置密钥，留空保留原值'
                          : '若使用个人订阅 PIN 可在此填写'
                    }
                    className="w-full rounded-xl border border-white/10 bg-black/40 px-3.5 py-2 font-mono text-xs text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
                  />
                </div>
                <div>
                  <label
                    htmlFor="tvdb-language"
                    className="mb-1.5 block text-xs font-medium text-zinc-400"
                  >
                    刮削语言
                  </label>
                  <select
                    id="tvdb-language"
                    value={tvdbLanguage}
                    onChange={(e) => setTvdbLanguage(e.target.value)}
                    className="w-full rounded-xl border border-white/10 bg-[#181822] px-3.5 py-2 text-sm text-zinc-100 outline-none focus:border-amber-400/60"
                  >
                    {TVDB_LANGUAGES.map((lang) => (
                      <option key={lang.value} value={lang.value}>
                        {lang.label}
                      </option>
                    ))}
                  </select>
                </div>
              </div>
            )}

            {isTraktPlugin(editingPlugin) && (
              <div className="rounded-xl border border-rose-500/20 bg-rose-500/[0.04] p-4 text-xs leading-relaxed text-zinc-400">
                {t('trakt.adminDescription')}
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

            {!isTraktPlugin(editingPlugin) && (
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
            )}

            <div className="flex justify-end gap-2 pt-2">
              <button
                type="button"
                onClick={closeConfigModal}
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
