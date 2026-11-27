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
  ShieldAlert,
  Smartphone,
  Trash2,
  X,
} from 'lucide-react'
import { api } from '../../api/client'
import type { ApiKeyInfo } from '../../api/types'
import { QueryErrorState } from '../../components/QueryErrorState'
import { useI18n } from '../../i18n/I18nContext'
import { AdminModal, FeedbackBanner, formatDateTime, formatRelativeTime } from './adminUtils'

function isAlreadyMaskedToken(item: ApiKeyInfo): boolean {
  const token = item.AccessToken || ''
  if (!token) return true
  if (token.includes('•') || token.includes('*')) return true
  if (item.MaskedToken && item.MaskedToken === item.AccessToken) return true
  return false
}

export function DevicesKeysTab() {
  const queryClient = useQueryClient()
  const { t } = useI18n()
  const [feedback, setFeedback] = useState<{ type: 'success' | 'error'; message: string } | null>(
    null,
  )

  const [revokingDeviceId, setRevokingDeviceId] = useState<string | null>(null)
  const [confirmDevice, setConfirmDevice] = useState<{ id: string; name: string } | null>(null)
  const [appName, setAppName] = useState('')
  const [creatingKey, setCreatingKey] = useState(false)
  const [newlyCreatedKey, setNewlyCreatedKey] = useState<{
    id?: string
    appName: string
    token: string
  } | null>(null)
  const [revokingKey, setRevokingKey] = useState<string | null>(null)
  const [confirmApiKey, setConfirmApiKey] = useState<{
    revokeId: string
    appName: string
    accessToken?: string
  } | null>(null)
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
      setFeedback({
        type: 'success',
        message: t('admin.devices.kickedSuccess', { name: deviceName || deviceId }),
      })
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('admin.devices.kickFailed'),
      })
    } finally {
      setRevokingDeviceId(null)
    }
  }

  const handleCreateApiKey = async (e: React.FormEvent) => {
    e.preventDefault()
    const trimmedApp = appName.trim()
    if (!trimmedApp) {
      setFeedback({ type: 'error', message: t('admin.devices.appNameRequired') })
      return
    }
    setCreatingKey(true)
    setFeedback(null)
    try {
      const created = await api.createApiKey(trimmedApp)
      await queryClient.invalidateQueries({ queryKey: ['admin', 'apiKeys'] })
      setAppName('')
      if (created?.AccessToken) {
        setNewlyCreatedKey({
          id: created.Id,
          appName: created.AppName || trimmedApp,
          token: created.AccessToken,
        })
        const rowKey = created.Id || created.AccessToken
        setRevealedKeys((prev) => ({ ...prev, [rowKey]: true }))
      }
      setFeedback({
        type: 'success',
        message: t('admin.devices.createdSuccess', { name: trimmedApp }),
      })
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('admin.devices.createFailed'),
      })
    } finally {
      setCreatingKey(false)
    }
  }

  const handleDeleteApiKey = async (revokeId: string, name: string, accessToken?: string) => {
    setRevokingKey(revokeId)
    setFeedback(null)
    try {
      await api.deleteApiKey(revokeId)
      setNewlyCreatedKey((prev) => {
        if (!prev) return null
        const matchedById = Boolean(prev.id && prev.id === revokeId)
        const matchedByToken =
          prev.token === revokeId || Boolean(accessToken && prev.token === accessToken)
        const matchedByApp = !prev.id && Boolean(name && prev.appName === name)
        if (matchedById || matchedByToken || matchedByApp) {
          return null
        }
        return prev
      })
      setRevealedKeys((prev) => {
        if (!(revokeId in prev)) return prev
        const next = { ...prev }
        delete next[revokeId]
        return next
      })
      setCopiedKey((prev) =>
        prev && (prev === revokeId || (accessToken && prev === accessToken)) ? null : prev,
      )
      await queryClient.invalidateQueries({ queryKey: ['admin', 'apiKeys'] })
      setConfirmApiKey(null)
      setFeedback({
        type: 'success',
        message: t('admin.devices.revokedSuccess', { name: name || 'API Key' }),
      })
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('admin.devices.revokeFailed'),
      })
    } finally {
      setRevokingKey(null)
    }
  }

  const handleCopy = async (token: string) => {
    let copied = false
    try {
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(token)
        copied = true
      }
    } catch {
      copied = false
    }
    if (!copied) {
      try {
        const ta = document.createElement('textarea')
        ta.value = token
        ta.setAttribute('readonly', '')
        ta.style.position = 'fixed'
        ta.style.opacity = '0'
        document.body.appendChild(ta)
        ta.select()
        copied = document.execCommand('copy')
        document.body.removeChild(ta)
      } catch {
        copied = false
      }
    }
    if (copied) {
      setCopiedKey(token)
      window.setTimeout(() => {
        setCopiedKey((prev) => (prev === token ? null : prev))
      }, 2000)
    } else {
      setFeedback({ type: 'error', message: t('admin.devices.copyFailed') })
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
              <span>{t('admin.devices.onlineDevices')}</span>
              <span className="rounded-md bg-white/8 px-2 py-0.5 text-xs font-medium text-zinc-300">
                {devices.length}
              </span>
            </h2>
            <p className="mt-1 text-xs text-zinc-400">{t('admin.devices.devicesDesc')}</p>
          </div>

          <button
            type="button"
            onClick={() => void refetchDevices()}
            className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/[0.04] px-3 py-1.5 text-xs font-medium text-zinc-300 transition-colors hover:border-amber-400/40 hover:text-amber-300"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${devicesFetching ? 'animate-spin' : ''}`} />
            <span>{t('common.refresh')}</span>
          </button>
        </div>

        {devicesLoading ? (
          <div className="flex h-32 items-center justify-center rounded-2xl border border-white/8 bg-white/[0.02]">
            <Loader2 className="h-5 w-5 animate-spin text-amber-400" />
          </div>
        ) : devicesIsError ? (
          <QueryErrorState
            compact
            title={t('admin.devices.devicesLoadError')}
            error={devicesError}
            onRetry={() => void refetchDevices()}
          />
        ) : devices.length === 0 ? (
          <div className="rounded-2xl border border-white/8 bg-white/[0.02] py-10 text-center text-xs text-zinc-500">
            {t('admin.devices.devicesEmpty')}
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
                      {dev.Name || t('admin.devices.unnamedDevice')}
                    </p>
                    <span className="rounded bg-white/8 px-1.5 py-0.5 text-[10px] font-medium text-zinc-300">
                      {dev.AppName || 'Unknown'} {dev.AppVersion || ''}
                    </span>
                  </div>
                  <p className="mt-1 text-xs text-zinc-400">
                    {t('admin.devices.userLabel')}{' '}
                    <span className="text-zinc-200">{dev.LastUserName || '-'}</span>
                  </p>
                  <p className="mt-0.5 truncate font-mono text-[11px] text-zinc-500" title={dev.Id}>
                    ID: {dev.Id}
                  </p>
                  <p
                    className="mt-1 text-[11px] text-zinc-500"
                    title={formatDateTime(dev.DateLastActivity)}
                  >
                    {t('admin.users.lastActive', {
                      time: formatRelativeTime(dev.DateLastActivity),
                    })}
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
                  <span>{t('admin.devices.kickBtn')}</span>
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
              <span>{t('admin.devices.apiKeysTitle')}</span>
              <span className="rounded-md bg-white/8 px-2 py-0.5 text-xs font-medium text-zinc-300">
                {apiKeys.length}
              </span>
            </h2>
            <p className="mt-1 text-xs text-zinc-400">{t('admin.devices.apiKeysDesc')}</p>
          </div>

          <button
            type="button"
            onClick={() => void refetchKeys()}
            className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/[0.04] px-3 py-1.5 text-xs font-medium text-zinc-300 transition-colors hover:border-amber-400/40 hover:text-amber-300"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${keysFetching ? 'animate-spin' : ''}`} />
            <span>{t('common.refresh')}</span>
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
            placeholder={t('admin.devices.appNamePlaceholder')}
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
            <span>{t('admin.devices.createKeyBtn')}</span>
          </button>
        </form>

        {/* 新建成功后一次性完整明文密钥醒目展示横幅 */}
        {newlyCreatedKey && (
          <div
            data-testid="created-api-key-banner"
            className="rounded-2xl border border-amber-500/40 bg-amber-500/10 p-4"
          >
            <div className="flex items-start justify-between gap-3">
              <div className="min-w-0 flex-1">
                <p className="flex items-center gap-1.5 text-xs font-semibold text-amber-200">
                  <ShieldAlert className="h-4 w-4 shrink-0 text-amber-400" />
                  <span>{t('admin.devices.createdBannerTitle')}</span>
                </p>
                <div className="mt-2.5 flex flex-wrap items-center gap-2">
                  <code
                    data-testid="created-api-key-value"
                    className="select-all break-all rounded-lg border border-amber-400/30 bg-black/50 px-3 py-1.5 font-mono text-xs font-semibold text-amber-200"
                  >
                    {newlyCreatedKey.token}
                  </code>
                  <button
                    type="button"
                    onClick={() => void handleCopy(newlyCreatedKey.token)}
                    className="inline-flex items-center gap-1.5 rounded-lg bg-amber-500 px-3 py-1.5 text-xs font-semibold text-zinc-950 transition-colors hover:bg-amber-400"
                  >
                    {copiedKey === newlyCreatedKey.token ? (
                      <>
                        <Check className="h-3.5 w-3.5" />
                        <span>{t('common.copied')}</span>
                      </>
                    ) : (
                      <>
                        <Copy className="h-3.5 w-3.5" />
                        <span>{t('common.copy')}</span>
                      </>
                    )}
                  </button>
                </div>
              </div>
              <button
                type="button"
                aria-label={t('common.close')}
                onClick={() => setNewlyCreatedKey(null)}
                className="rounded-lg p-1 text-amber-300/70 hover:bg-amber-500/20 hover:text-amber-100"
              >
                <X className="h-4 w-4" />
              </button>
            </div>
          </div>
        )}

        {keysLoading ? (
          <div className="flex h-28 items-center justify-center rounded-2xl border border-white/8 bg-white/[0.02]">
            <Loader2 className="h-5 w-5 animate-spin text-amber-400" />
          </div>
        ) : keysIsError ? (
          <QueryErrorState
            compact
            title={t('admin.devices.keysLoadError')}
            error={keysError}
            onRetry={() => void refetchKeys()}
          />
        ) : apiKeys.length === 0 ? (
          <div className="rounded-2xl border border-white/8 bg-white/[0.02] py-10 text-center text-xs text-zinc-500">
            {t('admin.devices.keysEmpty')}
          </div>
        ) : (
          <div className="overflow-hidden rounded-2xl border border-white/8 bg-white/[0.02]">
            <div className="divide-y divide-white/6">
              {apiKeys.map((item) => {
                const revokeId = item.Id || item.AccessToken
                const serverMasked = isAlreadyMaskedToken(item)
                const revealed = Boolean(revealedKeys[revokeId])
                const clientMasked =
                  item.MaskedToken ||
                  (item.AccessToken.length > 10
                    ? `${item.AccessToken.slice(0, 6)}${'•'.repeat(16)}${item.AccessToken.slice(-4)}`
                    : item.AccessToken)
                const displayToken = serverMasked
                  ? item.MaskedToken || item.AccessToken
                  : revealed
                    ? item.AccessToken
                    : clientMasked

                return (
                  <div
                    key={revokeId}
                    className="flex flex-col gap-3 px-4 py-3.5 sm:flex-row sm:items-center sm:justify-between"
                  >
                    <div className="min-w-0">
                      <div className="flex items-center gap-2">
                        <p className="text-sm font-semibold text-zinc-100">
                          {item.AppName || t('admin.devices.unnamedApp')}
                        </p>
                        <span className="rounded bg-emerald-500/15 px-1.5 py-0.5 text-[10px] font-medium text-emerald-300">
                          {t('admin.devices.statusActive')}
                        </span>
                      </div>
                      <div className="mt-1 flex items-center gap-2">
                        <code className="truncate rounded bg-black/40 px-2 py-0.5 font-mono text-xs text-amber-200/90">
                          {displayToken}
                        </code>
                        {!serverMasked && (
                          <button
                            type="button"
                            onClick={() =>
                              setRevealedKeys((prev) => ({
                                ...prev,
                                [revokeId]: !prev[revokeId],
                              }))
                            }
                            title={
                              revealed
                                ? t('admin.devices.hideSecret')
                                : t('admin.devices.showSecret')
                            }
                            className="rounded p-1 text-zinc-400 hover:bg-white/8 hover:text-zinc-100"
                          >
                            {revealed ? (
                              <EyeOff className="h-3.5 w-3.5" />
                            ) : (
                              <Eye className="h-3.5 w-3.5" />
                            )}
                          </button>
                        )}
                      </div>
                      <p className="mt-1 text-[11px] text-zinc-500">
                        {t('admin.devices.createdAt', { time: formatDateTime(item.DateCreated) })}
                      </p>
                    </div>

                    <div className="flex shrink-0 items-center gap-2">
                      {!serverMasked && (
                        <button
                          type="button"
                          onClick={() => void handleCopy(item.AccessToken)}
                          className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/[0.05] px-3 py-1.5 text-xs font-medium text-zinc-200 transition-colors hover:border-amber-400/40 hover:text-amber-300"
                        >
                          {copiedKey === item.AccessToken ? (
                            <>
                              <Check className="h-3.5 w-3.5 text-emerald-400" />
                              <span className="text-emerald-300">{t('common.copied')}</span>
                            </>
                          ) : (
                            <>
                              <Copy className="h-3.5 w-3.5" />
                              <span>{t('common.copy')}</span>
                            </>
                          )}
                        </button>
                      )}

                      <button
                        type="button"
                        disabled={revokingKey === revokeId}
                        onClick={() =>
                          setConfirmApiKey({
                            revokeId,
                            appName: item.AppName || 'API Key',
                            accessToken: item.AccessToken,
                          })
                        }
                        className="inline-flex items-center gap-1.5 rounded-lg border border-red-500/20 bg-red-500/10 px-3 py-1.5 text-xs font-medium text-red-300 transition-colors hover:bg-red-500/20 disabled:opacity-50"
                      >
                        {revokingKey === revokeId ? (
                          <Loader2 className="h-3.5 w-3.5 animate-spin" />
                        ) : (
                          <Trash2 className="h-3.5 w-3.5" />
                        )}
                        <span>{t('admin.devices.revokeBtn')}</span>
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
        title={t('admin.devices.kickModalTitle')}
        subtitle={t('admin.devices.kickModalSubtitle')}
        maxWidthClass="max-w-md"
        onClose={() => setConfirmDevice(null)}
      >
        <p className="text-sm text-zinc-300">
          {t('admin.devices.kickConfirmPrompt', { name: confirmDevice?.name || '' })}
        </p>
        <div className="mt-6 flex justify-end gap-2">
          <button
            type="button"
            onClick={() => setConfirmDevice(null)}
            className="rounded-xl border border-white/10 px-4 py-2 text-xs font-medium text-zinc-300 hover:bg-white/5"
          >
            {t('common.cancel')}
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
            <span>{t('admin.devices.confirmKick')}</span>
          </button>
        </div>
      </AdminModal>

      {/* 吊销 API Key 确认弹窗 */}
      <AdminModal
        open={Boolean(confirmApiKey)}
        title={t('admin.devices.revokeModalTitle')}
        subtitle={t('admin.devices.revokeModalSubtitle')}
        maxWidthClass="max-w-md"
        onClose={() => setConfirmApiKey(null)}
      >
        <p className="text-sm text-zinc-300">
          {t('admin.devices.revokeConfirmPrompt', { name: confirmApiKey?.appName || '' })}
        </p>
        <div className="mt-6 flex justify-end gap-2">
          <button
            type="button"
            onClick={() => setConfirmApiKey(null)}
            className="rounded-xl border border-white/10 px-4 py-2 text-xs font-medium text-zinc-300 hover:bg-white/5"
          >
            {t('common.cancel')}
          </button>
          <button
            type="button"
            disabled={!confirmApiKey || revokingKey === confirmApiKey.revokeId}
            onClick={() => {
              if (confirmApiKey) {
                void handleDeleteApiKey(
                  confirmApiKey.revokeId,
                  confirmApiKey.appName,
                  confirmApiKey.accessToken,
                )
              }
            }}
            className="inline-flex items-center gap-2 rounded-xl bg-red-500 px-4 py-2 text-xs font-semibold text-white hover:bg-red-400 disabled:opacity-60"
          >
            {confirmApiKey && revokingKey === confirmApiKey.revokeId && (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            )}
            <span>{t('admin.devices.confirmRevoke')}</span>
          </button>
        </div>
      </AdminModal>
    </div>
  )
}
