import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Check,
  Copy,
  Eye,
  EyeOff,
  Key,
  Loader2,
  LogOut,
  Plus,
  RefreshCw,
  Smartphone,
  Trash2,
} from 'lucide-react'
import { api } from '../../api/client'
import { QueryErrorState } from '../../components/QueryErrorState'
import { AdminModal, FeedbackBanner, formatDateTime, formatRelativeTime } from './adminUtils'

export function DevicesKeysTab() {
  const queryClient = useQueryClient()
  const [feedback, setFeedback] = useState<{ type: 'success' | 'error'; message: string } | null>(
    null,
  )

  const [revokingDeviceId, setRevokingDeviceId] = useState<string | null>(null)
  const [confirmDevice, setConfirmDevice] = useState<{ id: string; name: string } | null>(null)
  const [appName, setAppName] = useState('')
  const [creatingKey, setCreatingKey] = useState(false)
  const [revokingKey, setRevokingKey] = useState<string | null>(null)
  const [confirmApiKey, setConfirmApiKey] = useState<{ token: string; appName: string } | null>(null)
  const [copiedKey, setCopiedKey] = useState<string | null>(null)
  const [revealedKeys, setRevealedKeys] = useState<Record<string, boolean>>({})

  const {
    data: devicesData,
    isLoading: devicesLoading,
    isFetching: devicesFetching,
    isError: devicesIsError,
    error: devicesError,
    refetch: refetchDevices,
  } = useQuery({
    queryKey: ['admin', 'devices'],
    queryFn: () => api.getDevices(),
  })

  const {
    data: keysData,
    isLoading: keysLoading,
    isFetching: keysFetching,
    isError: keysIsError,
    error: keysError,
    refetch: refetchKeys,
  } = useQuery({
    queryKey: ['admin', 'apiKeys'],
    queryFn: () => api.getApiKeys(),
  })

  const devices = devicesData?.Items || []
  const apiKeys = keysData?.Items || []

  const handleDeleteDevice = async (deviceId: string, deviceName: string) => {
    setRevokingDeviceId(deviceId)
    setFeedback(null)
    try {
      await api.deleteDevice(deviceId)
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['admin', 'devices'] }),
        queryClient.invalidateQueries({ queryKey: ['admin', 'sessions'] }),
      ])
      setConfirmDevice(null)
      setFeedback({ type: 'success', message: `已注销并踢出设备「${deviceName || deviceId}」` })
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '注销设备失败',
      })
    } finally {
      setRevokingDeviceId(null)
    }
  }

  const handleCreateApiKey = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!appName.trim()) {
      setFeedback({ type: 'error', message: '请输入应用名称' })
      return
    }
    setCreatingKey(true)
    setFeedback(null)
    try {
      const created = await api.createApiKey(appName.trim())
      await queryClient.invalidateQueries({ queryKey: ['admin', 'apiKeys'] })
      setAppName('')
      if (created?.AccessToken) {
        setRevealedKeys((prev) => ({ ...prev, [created.AccessToken]: true }))
      }
      setFeedback({ type: 'success', message: `已为「${appName.trim()}」创建新的 API Key` })
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '创建 API Key 失败',
      })
    } finally {
      setCreatingKey(false)
    }
  }

  const handleDeleteApiKey = async (key: string, name: string) => {
    setRevokingKey(key)
    setFeedback(null)
    try {
      await api.deleteApiKey(key)
      await queryClient.invalidateQueries({ queryKey: ['admin', 'apiKeys'] })
      setConfirmApiKey(null)
      setFeedback({ type: 'success', message: `已吊销「${name || 'API Key'}」` })
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '吊销 API Key 失败',
      })
    } finally {
      setRevokingKey(null)
    }
  }

  const handleCopy = async (token: string) => {
    try {
      await navigator.clipboard.writeText(token)
      setCopiedKey(token)
      window.setTimeout(() => {
        setCopiedKey((prev) => (prev === token ? null : prev))
      }, 2000)
    } catch {
      setFeedback({ type: 'error', message: '复制失败，请手动选中复制' })
    }
  }

  return (
    <div className="space-y-8">
      <FeedbackBanner
        message={feedback?.message}
        type={feedback?.type}
        onClose={() => setFeedback(null)}
      />

      {/* 在线设备列表 */}
      <section className="space-y-4">
        <div className="flex items-center justify-between gap-3">
          <div>
            <h2 className="flex items-center gap-2 text-base font-semibold text-zinc-100">
              <Smartphone className="h-4 w-4 text-amber-400" />
              <span>已登录设备</span>
              <span className="rounded-md bg-white/8 px-2 py-0.5 text-xs font-medium text-zinc-300">
                {devices.length}
              </span>
            </h2>
            <p className="mt-1 text-xs text-zinc-400">
              查看所有接入客户端设备，支持一键强制注销并吊销设备令牌
            </p>
          </div>

          <button
            type="button"
            onClick={() => void refetchDevices()}
            className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/[0.04] px-3 py-1.5 text-xs font-medium text-zinc-300 transition-colors hover:border-amber-400/40 hover:text-amber-300"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${devicesFetching ? 'animate-spin' : ''}`} />
            <span>刷新</span>
          </button>
        </div>

        {devicesLoading ? (
          <div className="flex h-32 items-center justify-center rounded-2xl border border-white/8 bg-white/[0.02]">
            <Loader2 className="h-5 w-5 animate-spin text-amber-400" />
          </div>
        ) : devicesIsError ? (
          <QueryErrorState
            compact
            title="无法加载已登录设备"
            error={devicesError}
            onRetry={() => void refetchDevices()}
          />
        ) : devices.length === 0 ? (
          <div className="rounded-2xl border border-white/8 bg-white/[0.02] py-10 text-center text-xs text-zinc-500">
            暂无设备记录
          </div>
        ) : (
          <div className="grid gap-3 md:grid-cols-2">
            {devices.map((dev) => (
              <div
                key={dev.Id}
                className="flex items-start justify-between gap-3 rounded-2xl border border-white/8 bg-white/[0.03] p-4 transition-colors hover:border-white/15"
              >
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <p className="truncate text-sm font-semibold text-zinc-100">
                      {dev.Name || '未命名设备'}
                    </p>
                    <span className="rounded bg-white/8 px-1.5 py-0.5 text-[10px] font-medium text-zinc-300">
                      {dev.AppName || 'Unknown'} {dev.AppVersion || ''}
                    </span>
                  </div>
                  <p className="mt-1 text-xs text-zinc-400">
                    用户: <span className="text-zinc-200">{dev.LastUserName || '-'}</span>
                  </p>
                  <p className="mt-0.5 truncate font-mono text-[11px] text-zinc-500" title={dev.Id}>
                    ID: {dev.Id}
                  </p>
                  <p
                    className="mt-1 text-[11px] text-zinc-500"
                    title={formatDateTime(dev.DateLastActivity)}
                  >
                    最后活跃: {formatRelativeTime(dev.DateLastActivity)}
                  </p>
                </div>

                <button
                  type="button"
                  disabled={revokingDeviceId === dev.Id}
                  onClick={() => setConfirmDevice({ id: dev.Id, name: dev.Name || dev.Id })}
                  className="inline-flex shrink-0 items-center gap-1.5 rounded-lg border border-red-500/25 bg-red-500/10 px-3 py-1.5 text-xs font-medium text-red-300 transition-colors hover:bg-red-500/20 disabled:opacity-50"
                >
                  {revokingDeviceId === dev.Id ? (
                    <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  ) : (
                    <LogOut className="h-3.5 w-3.5" />
                  )}
                  <span>踢出</span>
                </button>
              </div>
            ))}
          </div>
        )}
      </section>

      {/* API Key 管理 */}
      <section className="space-y-4">
        <div className="flex items-center justify-between gap-3">
          <div>
            <h2 className="flex items-center gap-2 text-base font-semibold text-zinc-100">
              <Key className="h-4 w-4 text-amber-400" />
              <span>API Keys</span>
              <span className="rounded-md bg-white/8 px-2 py-0.5 text-xs font-medium text-zinc-300">
                {apiKeys.length}
              </span>
            </h2>
            <p className="mt-1 text-xs text-zinc-400">
              为第三方自动化工具（如 Sonarr / Radarr / 刮削脚本）签发与吊销访问密钥
            </p>
          </div>

          <button
            type="button"
            onClick={() => void refetchKeys()}
            className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/[0.04] px-3 py-1.5 text-xs font-medium text-zinc-300 transition-colors hover:border-amber-400/40 hover:text-amber-300"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${keysFetching ? 'animate-spin' : ''}`} />
            <span>刷新</span>
          </button>
        </div>

        {/* 创建 API Key 表单 */}
        <form
          onSubmit={handleCreateApiKey}
          className="flex flex-col gap-2 rounded-2xl border border-white/8 bg-white/[0.02] p-4 sm:flex-row"
        >
          <input
            type="text"
            value={appName}
            onChange={(e) => setAppName(e.target.value)}
            placeholder="输入应用名称，例如: Sonarr、MoviePilot、CLI 脚本..."
            className="flex-1 rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2 text-sm text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
          />
          <button
            type="submit"
            disabled={creatingKey}
            className="inline-flex items-center justify-center gap-1.5 rounded-xl bg-amber-500 px-4 py-2 text-sm font-semibold text-zinc-950 transition-colors hover:bg-amber-400 disabled:opacity-60"
          >
            {creatingKey ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : (
              <Plus className="h-4 w-4" />
            )}
            <span>创建 API Key</span>
          </button>
        </form>

        {keysLoading ? (
          <div className="flex h-28 items-center justify-center rounded-2xl border border-white/8 bg-white/[0.02]">
            <Loader2 className="h-5 w-5 animate-spin text-amber-400" />
          </div>
        ) : keysIsError ? (
          <QueryErrorState
            compact
            title="无法加载 API Keys"
            error={keysError}
            onRetry={() => void refetchKeys()}
          />
        ) : apiKeys.length === 0 ? (
          <div className="rounded-2xl border border-white/8 bg-white/[0.02] py-10 text-center text-xs text-zinc-500">
            尚未创建任何 API Key
          </div>
        ) : (
          <div className="overflow-hidden rounded-2xl border border-white/8 bg-white/[0.02]">
            <div className="divide-y divide-white/6">
              {apiKeys.map((item) => {
                const revealed = Boolean(revealedKeys[item.AccessToken])
                const masked =
                  item.AccessToken.length > 10
                    ? `${item.AccessToken.slice(0, 6)}${'•'.repeat(16)}${item.AccessToken.slice(-4)}`
                    : item.AccessToken

                return (
                  <div
                    key={item.AccessToken}
                    className="flex flex-col gap-3 px-4 py-3.5 sm:flex-row sm:items-center sm:justify-between"
                  >
                    <div className="min-w-0">
                      <div className="flex items-center gap-2">
                        <p className="text-sm font-semibold text-zinc-100">
                          {item.AppName || '未命名应用'}
                        </p>
                        <span className="rounded bg-emerald-500/15 px-1.5 py-0.5 text-[10px] font-medium text-emerald-300">
                          生效中
                        </span>
                      </div>
                      <div className="mt-1 flex items-center gap-2">
                        <code className="truncate rounded bg-black/40 px-2 py-0.5 font-mono text-xs text-amber-200/90">
                          {revealed ? item.AccessToken : masked}
                        </code>
                        <button
                          type="button"
                          onClick={() =>
                            setRevealedKeys((prev) => ({
                              ...prev,
                              [item.AccessToken]: !prev[item.AccessToken],
                            }))
                          }
                          title={revealed ? '隐藏密钥' : '显示完整密钥'}
                          className="rounded p-1 text-zinc-400 hover:bg-white/8 hover:text-zinc-100"
                        >
                          {revealed ? (
                            <EyeOff className="h-3.5 w-3.5" />
                          ) : (
                            <Eye className="h-3.5 w-3.5" />
                          )}
                        </button>
                      </div>
                      <p className="mt-1 text-[11px] text-zinc-500">
                        创建时间: {formatDateTime(item.DateCreated)}
                      </p>
                    </div>

                    <div className="flex shrink-0 items-center gap-2">
                      <button
                        type="button"
                        onClick={() => void handleCopy(item.AccessToken)}
                        className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/[0.05] px-3 py-1.5 text-xs font-medium text-zinc-200 transition-colors hover:border-amber-400/40 hover:text-amber-300"
                      >
                        {copiedKey === item.AccessToken ? (
                          <>
                            <Check className="h-3.5 w-3.5 text-emerald-400" />
                            <span className="text-emerald-300">已复制</span>
                          </>
                        ) : (
                          <>
                            <Copy className="h-3.5 w-3.5" />
                            <span>复制</span>
                          </>
                        )}
                      </button>

                      <button
                        type="button"
                        disabled={revokingKey === item.AccessToken}
                        onClick={() =>
                          setConfirmApiKey({
                            token: item.AccessToken,
                            appName: item.AppName || 'API Key',
                          })
                        }
                        className="inline-flex items-center gap-1.5 rounded-lg border border-red-500/20 bg-red-500/10 px-3 py-1.5 text-xs font-medium text-red-300 transition-colors hover:bg-red-500/20 disabled:opacity-50"
                      >
                        {revokingKey === item.AccessToken ? (
                          <Loader2 className="h-3.5 w-3.5 animate-spin" />
                        ) : (
                          <Trash2 className="h-3.5 w-3.5" />
                        )}
                        <span>吊销</span>
                      </button>
                    </div>
                  </div>
                )
              })}
            </div>
          </div>
        )}
      </section>

      {/* 踢出设备确认弹窗 */}
      <AdminModal
        open={Boolean(confirmDevice)}
        title="确认踢出设备"
        subtitle="该设备将被强制注销并吊销访问令牌，客户端需要重新登录"
        maxWidthClass="max-w-md"
        onClose={() => setConfirmDevice(null)}
      >
        <p className="text-sm text-zinc-300">
          确定要强制注销并踢出设备{' '}
          <span className="font-semibold text-white">{confirmDevice?.name}</span> 吗？
        </p>
        <div className="mt-6 flex justify-end gap-2">
          <button
            type="button"
            onClick={() => setConfirmDevice(null)}
            className="rounded-xl border border-white/10 px-4 py-2 text-xs font-medium text-zinc-300 hover:bg-white/5"
          >
            取消
          </button>
          <button
            type="button"
            disabled={!confirmDevice || revokingDeviceId === confirmDevice.id}
            onClick={() => {
              if (confirmDevice) {
                void handleDeleteDevice(confirmDevice.id, confirmDevice.name)
              }
            }}
            className="inline-flex items-center gap-2 rounded-xl bg-red-500 px-4 py-2 text-xs font-semibold text-white hover:bg-red-400 disabled:opacity-60"
          >
            {confirmDevice && revokingDeviceId === confirmDevice.id && (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            )}
            <span>确认踢出</span>
          </button>
        </div>
      </AdminModal>

      {/* 吊销 API Key 确认弹窗 */}
      <AdminModal
        open={Boolean(confirmApiKey)}
        title="确认吊销 API Key"
        subtitle="此操作不可恢复，使用该密钥的第三方应用将立即失去访问权限"
        maxWidthClass="max-w-md"
        onClose={() => setConfirmApiKey(null)}
      >
        <p className="text-sm text-zinc-300">
          确定要永久吊销应用{' '}
          <span className="font-semibold text-white">{confirmApiKey?.appName}</span> 的 API Key 吗？
        </p>
        <div className="mt-6 flex justify-end gap-2">
          <button
            type="button"
            onClick={() => setConfirmApiKey(null)}
            className="rounded-xl border border-white/10 px-4 py-2 text-xs font-medium text-zinc-300 hover:bg-white/5"
          >
            取消
          </button>
          <button
            type="button"
            disabled={!confirmApiKey || revokingKey === confirmApiKey.token}
            onClick={() => {
              if (confirmApiKey) {
                void handleDeleteApiKey(confirmApiKey.token, confirmApiKey.appName)
              }
            }}
            className="inline-flex items-center gap-2 rounded-xl bg-red-500 px-4 py-2 text-xs font-semibold text-white hover:bg-red-400 disabled:opacity-60"
          >
            {confirmApiKey && revokingKey === confirmApiKey.token && (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            )}
            <span>确认吊销</span>
          </button>
        </div>
      </AdminModal>
    </div>
  )
}
