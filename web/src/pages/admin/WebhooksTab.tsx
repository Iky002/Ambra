import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  AlertTriangle,
  CheckCircle2,
  Edit3,
  Loader2,
  Lock,
  Plus,
  Send,
  Trash2,
  Webhook,
  XCircle,
} from 'lucide-react'
import { api } from '../../api/client'
import type { WebhookInfo, WebhookTestResult } from '../../api/types'
import { QueryErrorState } from '../../components/QueryErrorState'
import { useI18n } from '../../i18n/I18nContext'
import { AdminModal, adminLabel, FeedbackBanner, ToggleSwitch } from './adminUtils'

function isSameWebhookOriginAndPath(oldRaw: string, newRaw: string): boolean {
  const mOld = oldRaw.trim().match(/^([a-zA-Z][a-zA-Z\d+\-.]*):\/\/([^/?#]+)([^?#]*)/)
  const mNew = newRaw.trim().match(/^([a-zA-Z][a-zA-Z\d+\-.]*):\/\/([^/?#]+)([^?#]*)/)
  if (!mOld || !mNew) return false
  if (mOld[1].toLowerCase() !== mNew[1].toLowerCase()) return false
  if (mOld[2].toLowerCase() !== mNew[2].toLowerCase()) return false
  return mOld[3] === mNew[3]
}

export const WEBHOOK_EVENT_OPTIONS = [
  { id: '*', labelKey: 'admin.webhooks.eventAllLabel', descKey: 'admin.webhooks.eventAllDesc' },
  {
    id: 'LibraryChanged',
    labelKey: 'admin.webhooks.eventLibLabel',
    descKey: 'admin.webhooks.eventLibDesc',
  },
  {
    id: 'UserDataChanged',
    labelKey: 'admin.webhooks.eventUserDataLabel',
    descKey: 'admin.webhooks.eventUserDataDesc',
  },
  {
    id: 'PlaybackStart',
    labelKey: 'admin.webhooks.eventPlayStartLabel',
    descKey: 'admin.webhooks.eventPlayStartDesc',
  },
  {
    id: 'PlaybackProgress',
    labelKey: 'admin.webhooks.eventPlayProgLabel',
    descKey: 'admin.webhooks.eventPlayProgDesc',
  },
  {
    id: 'PlaybackStop',
    labelKey: 'admin.webhooks.eventPlayStopLabel',
    descKey: 'admin.webhooks.eventPlayStopDesc',
  },
  {
    id: 'SessionUpdated',
    labelKey: 'admin.webhooks.eventSessionLabel',
    descKey: 'admin.webhooks.eventSessionDesc',
  },
  {
    id: 'TaskProgress',
    labelKey: 'admin.webhooks.eventTaskLabel',
    descKey: 'admin.webhooks.eventTaskDesc',
  },
] as const

export function WebhooksTab() {
  const queryClient = useQueryClient()
  const { t, locale } = useI18n()

  const [feedback, setFeedback] = useState<{ type: 'success' | 'error'; message: string } | null>(
    null,
  )

  const [modalOpen, setModalOpen] = useState(false)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [editingOriginalUrl, setEditingOriginalUrl] = useState('')
  const [editingHasSecret, setEditingHasSecret] = useState(false)
  const [name, setName] = useState('')
  const [url, setUrl] = useState('')
  const [secret, setSecret] = useState('')
  const [clearSecret, setClearSecret] = useState(false)
  const [enabled, setEnabled] = useState(true)
  const [events, setEvents] = useState<string[]>(['*'])
  const [saving, setSaving] = useState(false)
  const [modalError, setModalError] = useState<string | null>(null)

  const [testingId, setTestingId] = useState<string | null>(null)
  const [testResults, setTestResults] = useState<Record<string, WebhookTestResult>>({})

  const [deleteTarget, setDeleteTarget] = useState<WebhookInfo | null>(null)
  const [deleting, setDeleting] = useState(false)

  const {
    data: webhooks = [],
    isLoading,
    isError,
    error,
    refetch,
  } = useQuery({
    queryKey: ['admin', 'webhooks'],
    queryFn: () => api.getWebhooks(),
  })

  const invalidateWebhooks = async () => {
    await queryClient.invalidateQueries({ queryKey: ['admin', 'webhooks'] })
  }

  const openCreateModal = () => {
    setEditingId(null)
    setEditingOriginalUrl('')
    setEditingHasSecret(false)
    setName('')
    setUrl('')
    setSecret('')
    setClearSecret(false)
    setEnabled(true)
    setEvents(['*'])
    setModalError(null)
    setModalOpen(true)
  }

  const openEditModal = (wh: WebhookInfo) => {
    setEditingId(wh.Id)
    setEditingOriginalUrl(wh.Url)
    setEditingHasSecret(Boolean(wh.HasSecret || wh.Secret))
    setName(wh.Name)
    setUrl(wh.Url)
    setSecret('')
    setClearSecret(false)
    setEnabled(wh.Enabled)
    setEvents(Array.isArray(wh.Events) && wh.Events.length > 0 ? [...wh.Events] : ['*'])
    setModalError(null)
    setModalOpen(true)
  }

  const closeEditModal = () => {
    setModalOpen(false)
    setModalError(null)
  }

  const openDeleteModal = (wh: WebhookInfo) => {
    setModalError(null)
    setDeleteTarget(wh)
  }

  const closeDeleteModal = () => {
    setDeleteTarget(null)
    setModalError(null)
  }

  const toggleEvent = (eventId: string) => {
    setEvents((prev) => {
      if (eventId === '*') {
        return prev.includes('*') ? [] : ['*']
      }
      const withoutWildcard = prev.filter((ev) => ev !== '*')
      if (withoutWildcard.includes(eventId)) {
        return withoutWildcard.filter((ev) => ev !== eventId)
      }
      return [...withoutWildcard, eventId]
    })
  }

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault()
    const trimmedName = name.trim()
    const trimmedUrl = url.trim()
    if (!trimmedName) {
      setModalError(t('admin.webhooks.errNameRequired'))
      return
    }
    if (!trimmedUrl) {
      setModalError(t('admin.webhooks.errUrlRequired'))
      return
    }
    if (events.length === 0) {
      setModalError(t('admin.webhooks.errEventsRequired'))
      return
    }

    setSaving(true)
    setModalError(null)
    setFeedback(null)
    try {
      await api.saveWebhook({
        ...(editingId ? { Id: editingId } : {}),
        Name: trimmedName,
        Url: trimmedUrl,
        ...(clearSecret
          ? { Secret: '', ClearSecret: true }
          : secret.trim()
            ? { Secret: secret.trim() }
            : {}),
        Enabled: enabled,
        Events: events,
      })
      await invalidateWebhooks()
      setModalOpen(false)
      setModalError(null)
      setFeedback({
        type: 'success',
        message: editingId
          ? t('admin.webhooks.updatedSuccess', { name: trimmedName })
          : t('admin.webhooks.createdSuccess', { name: trimmedName }),
      })
    } catch (err) {
      setModalError(err instanceof Error ? err.message : t('admin.webhooks.saveFailed'))
    } finally {
      setSaving(false)
    }
  }

  const formatFailureDetail = (res: WebhookTestResult): string => {
    const parts: string[] = []
    const reason =
      res.Error?.trim() ||
      (res.StatusCode !== undefined && res.StatusCode > 0
        ? `HTTP ${res.StatusCode}`
        : t('admin.webhooks.unknownEndpointError'))
    parts.push(reason)
    if (
      res.StatusCode !== undefined &&
      res.StatusCode > 0 &&
      !reason.includes(`HTTP ${res.StatusCode}`)
    ) {
      parts.push(`HTTP ${res.StatusCode}`)
    }
    if (res.DurationMs !== undefined) {
      parts.push(`${res.DurationMs} ms`)
    }
    return parts.join(' · ')
  }

  const handleTest = async (wh: WebhookInfo) => {
    setTestingId(wh.Id)
    setFeedback(null)
    try {
      const res = await api.testWebhook({
        Id: wh.Id,
        Url: wh.Url,
        Secret: wh.Secret,
        Name: wh.Name,
      })
      setTestResults((prev) => ({ ...prev, [wh.Id]: res }))
      if (res.Success) {
        const statusPart =
          res.StatusCode !== undefined ? `HTTP ${res.StatusCode}` : t('admin.webhooks.testSuccessBadge')
        const durationPart = res.DurationMs !== undefined ? ` · ${res.DurationMs} ms` : ''
        setFeedback({
          type: 'success',
          message: t('admin.webhooks.testPushSuccess', {
            name: wh.Name,
            detail: `${statusPart}${durationPart}`,
          }),
        })
      } else {
        setFeedback({
          type: 'error',
          message: t('admin.webhooks.testPushFailed', {
            name: wh.Name,
            detail: formatFailureDetail(res),
          }),
        })
      }
    } catch (err) {
      const msg = err instanceof Error ? err.message : t('admin.webhooks.unknownEndpointError')
      setTestResults((prev) => ({
        ...prev,
        [wh.Id]: { Success: false, Error: msg },
      }))
      setFeedback({
        type: 'error',
        message: t('admin.webhooks.testPushFailed', { name: wh.Name, detail: msg }),
      })
    } finally {
      setTestingId(null)
    }
  }

  const handleDelete = async () => {
    if (!deleteTarget) return
    setDeleting(true)
    setModalError(null)
    setFeedback(null)
    try {
      await api.deleteWebhook(deleteTarget.Id)
      await invalidateWebhooks()
      setTestResults((prev) => {
        const next = { ...prev }
        delete next[deleteTarget.Id]
        return next
      })
      setFeedback({
        type: 'success',
        message: t('admin.webhooks.deletedSuccess', { name: deleteTarget.Name }),
      })
      setDeleteTarget(null)
      setModalError(null)
    } catch (err) {
      setModalError(err instanceof Error ? err.message : t('admin.webhooks.deleteFailed'))
    } finally {
      setDeleting(false)
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <h2 className="flex items-center gap-2 text-base font-semibold text-zinc-100">
            <Webhook className="h-4 w-4 text-amber-400" />
            <span>{t('admin.webhooksTitle')}</span>
          </h2>
          <p className="mt-1 text-xs text-zinc-400">{t('admin.webhooks.subtitle')}</p>
        </div>

        <button
          type="button"
          onClick={openCreateModal}
          className="inline-flex items-center gap-2 rounded-xl bg-amber-500 px-4 py-2 text-sm font-semibold text-zinc-950 shadow-lg shadow-amber-500/20 transition-colors hover:bg-amber-400"
        >
          <Plus className="h-4 w-4" />
          <span>{t('admin.createWebhook')}</span>
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
          title={t('admin.webhooks.loadError')}
          error={error}
          onRetry={() => void refetch()}
        />
      ) : webhooks.length === 0 ? (
        <div className="flex flex-col items-center justify-center rounded-2xl border border-dashed border-white/12 bg-white/[0.02] px-6 py-12 text-center">
          <div className="mb-3 flex h-12 w-12 items-center justify-center rounded-2xl bg-amber-500/10 text-amber-400 ring-1 ring-amber-500/20">
            <Webhook className="h-6 w-6" />
          </div>
          <p className="text-sm font-semibold text-zinc-100">{t('admin.webhooks.emptyTitle')}</p>
          <p className="mt-1 max-w-md text-xs text-zinc-400">{t('admin.webhooks.emptyDesc')}</p>
          <button
            type="button"
            onClick={openCreateModal}
            className="mt-4 inline-flex items-center gap-1.5 rounded-xl bg-amber-500 px-4 py-2 text-xs font-semibold text-zinc-950 transition-colors hover:bg-amber-400"
          >
            <Plus className="h-3.5 w-3.5" />
            <span>{t('admin.webhooks.addFirstBtn')}</span>
          </button>
        </div>
      ) : (
        <div className="space-y-3">
          {webhooks.map((wh) => {
            const isTesting = testingId === wh.Id
            const lastTest = testResults[wh.Id]
            return (
              <div
                key={wh.Id}
                data-testid={`webhook-card-${wh.Id}`}
                className="rounded-2xl border border-white/8 bg-white/[0.03] p-4 transition-colors hover:border-white/15"
              >
                <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                  <div className="min-w-0 flex-1 space-y-2">
                    <div className="flex flex-wrap items-center gap-2">
                      <h3 className="truncate text-sm font-semibold text-zinc-100">{wh.Name}</h3>
                      {wh.Enabled ? (
                        <span className="inline-flex items-center gap-1 rounded-md bg-emerald-500/15 px-2 py-0.5 text-[11px] font-medium text-emerald-300 ring-1 ring-emerald-500/30">
                          {t('admin.plugins.statusEnabled')}
                        </span>
                      ) : (
                        <span className="inline-flex items-center gap-1 rounded-md bg-zinc-800 px-2 py-0.5 text-[11px] font-medium text-zinc-400 ring-1 ring-white/10">
                          {t('admin.plugins.statusDisabled')}
                        </span>
                      )}
                      {(wh.HasSecret || Boolean(wh.Secret)) && (
                        <span className="inline-flex items-center gap-1 rounded-md bg-amber-500/10 px-2 py-0.5 text-[11px] font-medium text-amber-300 ring-1 ring-amber-500/20">
                          <Lock className="h-3 w-3" />
                          <span>{t('admin.webhooks.hmacBadge')}</span>
                        </span>
                      )}
                    </div>

                    <p className="truncate font-mono text-xs text-zinc-400" title={wh.Url}>
                      {wh.Url}
                    </p>

                    <div className="flex flex-wrap gap-1.5 pt-0.5">
                      {(wh.Events || []).map((ev) => (
                        <span
                          key={ev}
                          className="rounded-md border border-white/8 bg-white/[0.04] px-2 py-0.5 text-[11px] text-zinc-300"
                        >
                          {ev === '*' ? t('admin.webhooks.eventAllLabel') : ev}
                        </span>
                      ))}
                    </div>

                    {lastTest && (
                      <div
                        data-testid={`webhook-test-result-${wh.Id}`}
                        className={`inline-flex flex-wrap items-center gap-1.5 rounded-lg border px-2.5 py-1 text-xs ${
                          lastTest.Success
                            ? 'border-emerald-500/25 bg-emerald-500/10 text-emerald-300'
                            : 'border-red-500/25 bg-red-500/10 text-red-300'
                        }`}
                      >
                        {lastTest.Success ? (
                          <CheckCircle2 className="h-3.5 w-3.5 shrink-0" />
                        ) : (
                          <XCircle className="h-3.5 w-3.5 shrink-0" />
                        )}
                        {lastTest.Success ? (
                          <span>
                            {t('admin.webhooks.testSuccessBadge')}
                            {lastTest.StatusCode !== undefined
                              ? ` · HTTP ${lastTest.StatusCode}`
                              : ''}
                            {lastTest.DurationMs !== undefined
                              ? ` · ${lastTest.DurationMs} ms`
                              : ''}
                          </span>
                        ) : (
                          <span>
                            {t('admin.webhooks.testFailedReason', {
                              reason: formatFailureDetail(lastTest),
                            })}
                          </span>
                        )}
                      </div>
                    )}
                  </div>

                  <div className="flex shrink-0 flex-wrap items-center gap-2">
                    <button
                      type="button"
                      disabled={isTesting}
                      onClick={() => void handleTest(wh)}
                      className="inline-flex items-center gap-1.5 rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-1.5 text-xs font-medium text-amber-300 transition-colors hover:bg-amber-500/20 disabled:opacity-60"
                    >
                      {isTesting ? (
                        <Loader2 className="h-3.5 w-3.5 animate-spin" />
                      ) : (
                        <Send className="h-3.5 w-3.5" />
                      )}
                      <span>{t('admin.webhooks.sendTestBtn')}</span>
                    </button>

                    <button
                      type="button"
                      onClick={() => openEditModal(wh)}
                      className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/[0.05] px-3 py-1.5 text-xs font-medium text-zinc-200 transition-colors hover:border-amber-400/40 hover:text-amber-300"
                    >
                      <Edit3 className="h-3.5 w-3.5" />
                      <span>{t('common.edit')}</span>
                    </button>

                    <button
                      type="button"
                      onClick={() => openDeleteModal(wh)}
                      className="inline-flex items-center gap-1.5 rounded-lg border border-red-500/20 bg-red-500/10 px-3 py-1.5 text-xs font-medium text-red-300 transition-colors hover:bg-red-500/20"
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                      <span>{t('common.delete')}</span>
                    </button>
                  </div>
                </div>
              </div>
            )
          })}
        </div>
      )}

      {/* 新增 / 编辑 Webhook 弹窗 */}
      <AdminModal
        open={modalOpen}
        title={editingId ? t('admin.webhooks.editModalTitle') : t('admin.createWebhook')}
        subtitle={t('admin.webhooks.modalSubtitle')}
        maxWidthClass="max-w-xl"
        onClose={closeEditModal}
      >
        <form onSubmit={handleSave} className="space-y-4">
          {modalError && (
            <div role="alert">
              <FeedbackBanner
                message={modalError}
                type="error"
                onClose={() => setModalError(null)}
              />
            </div>
          )}
          <div>
            <label
              htmlFor="webhook-name-input"
              className="mb-1.5 block text-xs font-medium text-zinc-400"
            >
              {t('admin.webhooks.nameLabel')}
            </label>
            <input
              id="webhook-name-input"
              type="text"
              required
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t('admin.webhooks.namePlaceholder')}
              className="w-full rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2.5 text-sm text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
            />
          </div>

          <div>
            <label
              htmlFor="webhook-url-input"
              className="mb-1.5 block text-xs font-medium text-zinc-400"
            >
              {t('admin.webhooks.urlLabel')}
            </label>
            <input
              id="webhook-url-input"
              type="url"
              required
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              placeholder="https://example.com/webhook"
              className="w-full rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2.5 font-mono text-sm text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
            />
          </div>

          <div>
            <label
              htmlFor="webhook-secret-input"
              className="mb-1.5 block text-xs font-medium text-zinc-400"
            >
              {t('admin.webhooks.secretLabel')}
            </label>
            {(() => {
              const urlOriginOrPathChanged =
                Boolean(editingId) &&
                Boolean(editingOriginalUrl) &&
                !isSameWebhookOriginAndPath(editingOriginalUrl, url)
              const secretWillClearOnUrlChange =
                Boolean(editingId) &&
                editingHasSecret &&
                !clearSecret &&
                urlOriginOrPathChanged &&
                !secret.trim()

              return (
                <>
                  <input
                    id="webhook-secret-input"
                    type="password"
                    value={secret}
                    disabled={clearSecret}
                    onChange={(e) => setSecret(e.target.value)}
                    placeholder={
                      editingId && editingHasSecret
                        ? clearSecret
                          ? t('admin.webhooks.secretClearedPlaceholder')
                          : urlOriginOrPathChanged
                            ? adminLabel(
                                t,
                                locale,
                                'admin.webhooks.secretUrlChangedPlaceholder',
                                '目标 URL 域名或路径已变更，留空将自动清空原密钥',
                                'Target URL origin/path changed; leaving blank clears existing secret',
                              )
                            : t('admin.webhooks.secretConfiguredPlaceholder')
                        : t('admin.webhooks.secretEmptyPlaceholder')
                    }
                    className="w-full rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2.5 text-sm text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60 disabled:cursor-not-allowed disabled:opacity-50"
                  />
                  {editingId && editingHasSecret && (
                    <div className="mt-2 space-y-1.5">
                      <label
                        htmlFor="webhook-clear-secret"
                        className="inline-flex cursor-pointer items-center gap-2 text-xs text-zinc-300"
                      >
                        <input
                          id="webhook-clear-secret"
                          type="checkbox"
                          checked={clearSecret}
                          onChange={(e) => {
                            const next = e.target.checked
                            setClearSecret(next)
                            if (next) setSecret('')
                          }}
                          className="h-3.5 w-3.5 rounded accent-amber-500"
                        />
                        <span>{t('admin.webhooks.clearSecretLabel')}</span>
                      </label>
                      {clearSecret ? (
                        <p className="text-[11px] text-amber-300/80">
                          {t('admin.webhooks.clearSecretActiveHint')}
                        </p>
                      ) : secretWillClearOnUrlChange ? (
                        <div
                          role="status"
                          data-testid="webhook-secret-url-change-warning"
                          className="flex items-start gap-2 rounded-lg border border-red-500/35 bg-red-500/12 px-3 py-2 text-[11px] leading-relaxed text-red-200"
                        >
                          <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0 text-red-400" />
                          <span>
                            {adminLabel(
                              t,
                              locale,
                              'admin.webhooks.secretUrlChangeWarnHint',
                              '安全提示：修改目标推送 URL 的域名（Origin）或路径（Path）后，若不填写新的签名密钥，原有 HMAC 签名密钥将在保存时自动清除（防止旧密钥泄露给新端点）。',
                              'Security Warning: Changing the target URL origin or path clears the existing signing secret unless a new secret is entered.',
                            )}
                          </span>
                        </div>
                      ) : (
                        <p className="text-[11px] text-amber-300/80">
                          {t('admin.webhooks.clearSecretKeepHint')}
                        </p>
                      )}
                    </div>
                  )}
                </>
              )
            })()}
          </div>

          <ToggleSwitch
            checked={enabled}
            onChange={setEnabled}
            label={t('admin.webhooks.enableWebhookLabel')}
            description={t('admin.webhooks.enableWebhookDesc')}
          />

          <div className="rounded-xl border border-white/8 bg-white/[0.02] p-4">
            <p className="mb-2.5 text-xs font-semibold text-zinc-200">
              {t('admin.webhooks.eventsTitle')}
            </p>
            <div className="grid gap-2 sm:grid-cols-2">
              {WEBHOOK_EVENT_OPTIONS.map((ev) => {
                const checked = events.includes(ev.id)
                return (
                  <label
                    key={ev.id}
                    className={`flex cursor-pointer items-start gap-2.5 rounded-lg border px-3 py-2 text-xs transition-colors ${
                      checked
                        ? 'border-amber-400/50 bg-amber-500/15 text-amber-100'
                        : 'border-white/8 bg-white/[0.02] text-zinc-300 hover:bg-white/[0.05]'
                    }`}
                  >
                    <input
                      type="checkbox"
                      checked={checked}
                      onChange={() => toggleEvent(ev.id)}
                      className="mt-0.5 h-4 w-4 rounded accent-amber-500"
                    />
                    <div className="min-w-0">
                      <p className="truncate font-medium">{t(ev.labelKey)}</p>
                      <p className="mt-0.5 text-[11px] text-zinc-400">{t(ev.descKey)}</p>
                    </div>
                  </label>
                )
              })}
            </div>
          </div>

          <div className="flex justify-end gap-2 pt-2">
            <button
              type="button"
              onClick={closeEditModal}
              className="rounded-xl border border-white/10 px-4 py-2 text-xs font-medium text-zinc-300 hover:bg-white/5"
            >
              {t('common.cancel')}
            </button>
            <button
              type="submit"
              disabled={saving}
              className="inline-flex items-center gap-2 rounded-xl bg-amber-500 px-4 py-2 text-xs font-semibold text-zinc-950 hover:bg-amber-400 disabled:opacity-60"
            >
              {saving && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              <span>
                {editingId
                  ? t('admin.webhooks.saveChangesBtn')
                  : t('admin.webhooks.createConfirmBtn')}
              </span>
            </button>
          </div>
        </form>
      </AdminModal>

      {/* 删除确认弹窗 */}
      <AdminModal
        open={Boolean(deleteTarget)}
        title={t('admin.webhooks.deleteModalTitle')}
        subtitle={t('admin.webhooks.deleteModalSubtitle')}
        maxWidthClass="max-w-md"
        onClose={closeDeleteModal}
      >
        {modalError && (
          <div role="alert" className="mb-4">
            <FeedbackBanner
              message={modalError}
              type="error"
              onClose={() => setModalError(null)}
            />
          </div>
        )}
        <p className="text-sm text-zinc-300">
          {t('admin.webhooks.deleteConfirmPrompt', { name: deleteTarget?.Name || '' })}
        </p>
        <div className="mt-6 flex justify-end gap-2">
          <button
            type="button"
            onClick={closeDeleteModal}
            className="rounded-xl border border-white/10 px-4 py-2 text-xs font-medium text-zinc-300 hover:bg-white/5"
          >
            {t('common.cancel')}
          </button>
          <button
            type="button"
            disabled={deleting}
            onClick={() => void handleDelete()}
            className="inline-flex items-center gap-2 rounded-xl bg-red-500 px-4 py-2 text-xs font-semibold text-white hover:bg-red-400 disabled:opacity-60"
          >
            {deleting && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
            <span>{t('admin.users.confirmDelete')}</span>
          </button>
        </div>
      </AdminModal>
    </div>
  )
}
