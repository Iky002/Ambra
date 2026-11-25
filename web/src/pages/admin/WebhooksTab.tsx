import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
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
import { AdminModal, FeedbackBanner, ToggleSwitch } from './adminUtils'

export const WEBHOOK_EVENT_OPTIONS = [
  { id: '*', label: '全部事件 (*)', desc: '接收所有系统与播放事件推送' },
  { id: 'LibraryChanged', label: '媒体库变更 (LibraryChanged)', desc: '新媒体入库或元数据更新时触发' },
  { id: 'PlaybackStart', label: '开始播放 (PlaybackStart)', desc: '用户开始播放音视频时触发' },
  { id: 'PlaybackStop', label: '停止播放 (PlaybackStop)', desc: '用户停止播放或播完时触发' },
  { id: 'UserDataChanged', label: '用户数据变更 (UserDataChanged)', desc: '收藏、已看状态或进度变更时触发' },
  { id: 'TaskProgress', label: '计划任务进度 (TaskProgress)', desc: '后台计划任务执行状态变更时触发' },
  { id: 'SessionStart', label: '会话开始 (SessionStart)', desc: '新客户端登录并建立会话时触发' },
  { id: 'SessionEnd', label: '会话结束 (SessionEnd)', desc: '客户端注销或会话断开时触发' },
] as const

const EVENT_LABEL_MAP: Record<string, string> = {
  '*': '全部事件 (*)',
  LibraryChanged: 'LibraryChanged',
  PlaybackStart: 'PlaybackStart',
  PlaybackStop: 'PlaybackStop',
  UserDataChanged: 'UserDataChanged',
  TaskProgress: 'TaskProgress',
  SessionStart: 'SessionStart',
  SessionEnd: 'SessionEnd',
}

export function WebhooksTab() {
  const queryClient = useQueryClient()

  const [feedback, setFeedback] = useState<{ type: 'success' | 'error'; message: string } | null>(
    null,
  )

  // 新增 / 编辑弹窗状态
  const [modalOpen, setModalOpen] = useState(false)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [name, setName] = useState('')
  const [url, setUrl] = useState('')
  const [secret, setSecret] = useState('')
  const [enabled, setEnabled] = useState(true)
  const [events, setEvents] = useState<string[]>(['*'])
  const [saving, setSaving] = useState(false)

  // 发送测试状态与每条 Webhook 的最近测试结果
  const [testingId, setTestingId] = useState<string | null>(null)
  const [testResults, setTestResults] = useState<Record<string, WebhookTestResult>>({})

  // 删除确认弹窗状态
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
    setName('')
    setUrl('')
    setSecret('')
    setEnabled(true)
    setEvents(['*'])
    setModalOpen(true)
  }

  const openEditModal = (wh: WebhookInfo) => {
    setEditingId(wh.Id)
    setName(wh.Name)
    setUrl(wh.Url)
    setSecret(wh.Secret || '')
    setEnabled(wh.Enabled)
    setEvents(Array.isArray(wh.Events) && wh.Events.length > 0 ? [...wh.Events] : ['*'])
    setModalOpen(true)
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
      setFeedback({ type: 'error', message: '请输入 Webhook 名称' })
      return
    }
    if (!trimmedUrl) {
      setFeedback({ type: 'error', message: '请输入目标推送 URL' })
      return
    }
    if (events.length === 0) {
      setFeedback({ type: 'error', message: '请至少选择一个订阅事件' })
      return
    }

    setSaving(true)
    setFeedback(null)
    try {
      await api.saveWebhook({
        ...(editingId ? { Id: editingId } : {}),
        Name: trimmedName,
        Url: trimmedUrl,
        Secret: secret.trim() || undefined,
        Enabled: enabled,
        Events: events,
      })
      await invalidateWebhooks()
      setModalOpen(false)
      setFeedback({
        type: 'success',
        message: editingId
          ? `已更新 Webhook「${trimmedName}」`
          : `已创建 Webhook「${trimmedName}」`,
      })
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '保存 Webhook 失败',
      })
    } finally {
      setSaving(false)
    }
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
        const statusPart = res.StatusCode !== undefined ? `HTTP ${res.StatusCode}` : '成功'
        const durationPart = res.DurationMs !== undefined ? ` · ${res.DurationMs} ms` : ''
        setFeedback({
          type: 'success',
          message: `Webhook「${wh.Name}」测试推送成功（${statusPart}${durationPart}）`,
        })
      } else {
        const statusPart = res.StatusCode !== undefined ? `HTTP ${res.StatusCode}` : ''
        const durationPart = res.DurationMs !== undefined ? ` · ${res.DurationMs} ms` : ''
        setFeedback({
          type: 'error',
          message: `Webhook「${wh.Name}」测试推送失败：${res.Error || statusPart || '目标端点返回异常'}${durationPart}`,
        })
      }
    } catch (err) {
      const msg = err instanceof Error ? err.message : '发送测试请求失败'
      setTestResults((prev) => ({
        ...prev,
        [wh.Id]: { Success: false, Error: msg },
      }))
      setFeedback({ type: 'error', message: `Webhook「${wh.Name}」测试失败：${msg}` })
    } finally {
      setTestingId(null)
    }
  }

  const handleDelete = async () => {
    if (!deleteTarget) return
    setDeleting(true)
    setFeedback(null)
    try {
      await api.deleteWebhook(deleteTarget.Id)
      await invalidateWebhooks()
      setTestResults((prev) => {
        const next = { ...prev }
        delete next[deleteTarget.Id]
        return next
      })
      setFeedback({ type: 'success', message: `已删除 Webhook「${deleteTarget.Name}」` })
      setDeleteTarget(null)
    } catch (err) {
      setFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '删除 Webhook 失败',
      })
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
            <span>Webhook 事件通知</span>
          </h2>
          <p className="mt-1 text-xs text-zinc-400">
            将媒体库入库、播放启停与系统任务事件通过 HTTP POST 实时推送至 Discord、Slack 或自建服务
          </p>
        </div>

        <button
          type="button"
          onClick={openCreateModal}
          className="inline-flex items-center gap-2 rounded-xl bg-amber-500 px-4 py-2 text-sm font-semibold text-zinc-950 shadow-lg shadow-amber-500/20 transition-colors hover:bg-amber-400"
        >
          <Plus className="h-4 w-4" />
          <span>新增 Webhook</span>
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
          title="无法加载 Webhook 列表"
          error={error}
          onRetry={() => refetch()}
        />
      ) : webhooks.length === 0 ? (
        <div className="flex flex-col items-center justify-center rounded-2xl border border-dashed border-white/12 bg-white/[0.02] px-6 py-12 text-center">
          <div className="mb-3 flex h-12 w-12 items-center justify-center rounded-2xl bg-amber-500/10 text-amber-400 ring-1 ring-amber-500/20">
            <Webhook className="h-6 w-6" />
          </div>
          <p className="text-sm font-semibold text-zinc-100">尚未配置任何 Webhook 通知</p>
          <p className="mt-1 max-w-md text-xs text-zinc-400">
            添加 Webhook 端点后，Ambra 可在媒体库更新或用户播放时自动发送带可选 HMAC-SHA256 签名的 JSON 回调。
          </p>
          <button
            type="button"
            onClick={openCreateModal}
            className="mt-4 inline-flex items-center gap-1.5 rounded-xl bg-amber-500 px-4 py-2 text-xs font-semibold text-zinc-950 transition-colors hover:bg-amber-400"
          >
            <Plus className="h-3.5 w-3.5" />
            <span>添加第一个 Webhook</span>
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
                          已启用
                        </span>
                      ) : (
                        <span className="inline-flex items-center gap-1 rounded-md bg-zinc-800 px-2 py-0.5 text-[11px] font-medium text-zinc-400 ring-1 ring-white/10">
                          已停用
                        </span>
                      )}
                      {wh.Secret && (
                        <span className="inline-flex items-center gap-1 rounded-md bg-amber-500/10 px-2 py-0.5 text-[11px] font-medium text-amber-300 ring-1 ring-amber-500/20">
                          <Lock className="h-3 w-3" />
                          HMAC 签名
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
                          {EVENT_LABEL_MAP[ev] || ev}
                        </span>
                      ))}
                    </div>

                    {lastTest && (
                      <div
                        data-testid={`webhook-test-result-${wh.Id}`}
                        className={`inline-flex items-center gap-1.5 rounded-lg border px-2.5 py-1 text-xs ${
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
                        <span>
                          {lastTest.Success ? '测试成功' : `测试失败${lastTest.Error ? `: ${lastTest.Error}` : ''}`}
                          {lastTest.StatusCode !== undefined ? ` · HTTP ${lastTest.StatusCode}` : ''}
                          {lastTest.DurationMs !== undefined ? ` · ${lastTest.DurationMs} ms` : ''}
                        </span>
                      </div>
                    )}
                  </div>

                  <div className="flex shrink-0 flex-wrap items-center gap-2">
                    <button
                      type="button"
                      disabled={isTesting}
                      onClick={() => handleTest(wh)}
                      className="inline-flex items-center gap-1.5 rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-1.5 text-xs font-medium text-amber-300 transition-colors hover:bg-amber-500/20 disabled:opacity-60"
                    >
                      {isTesting ? (
                        <Loader2 className="h-3.5 w-3.5 animate-spin" />
                      ) : (
                        <Send className="h-3.5 w-3.5" />
                      )}
                      <span>发送测试</span>
                    </button>

                    <button
                      type="button"
                      onClick={() => openEditModal(wh)}
                      className="inline-flex items-center gap-1.5 rounded-lg border border-white/10 bg-white/[0.05] px-3 py-1.5 text-xs font-medium text-zinc-200 transition-colors hover:border-amber-400/40 hover:text-amber-300"
                    >
                      <Edit3 className="h-3.5 w-3.5" />
                      <span>编辑</span>
                    </button>

                    <button
                      type="button"
                      onClick={() => setDeleteTarget(wh)}
                      className="inline-flex items-center gap-1.5 rounded-lg border border-red-500/20 bg-red-500/10 px-3 py-1.5 text-xs font-medium text-red-300 transition-colors hover:bg-red-500/20"
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                      <span>删除</span>
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
        title={editingId ? '编辑 Webhook' : '新增 Webhook'}
        subtitle="配置目标推送地址、可选签名密钥以及需要监听的事件类型"
        maxWidthClass="max-w-xl"
        onClose={() => setModalOpen(false)}
      >
        <form onSubmit={handleSave} className="space-y-4">
          <div>
            <label
              htmlFor="webhook-name-input"
              className="mb-1.5 block text-xs font-medium text-zinc-400"
            >
              名称 *
            </label>
            <input
              id="webhook-name-input"
              type="text"
              required
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="例如: Discord 入库通知"
              className="w-full rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2.5 text-sm text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
            />
          </div>

          <div>
            <label
              htmlFor="webhook-url-input"
              className="mb-1.5 block text-xs font-medium text-zinc-400"
            >
              推送 URL *
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
              HMAC 密钥（可选）
            </label>
            <input
              id="webhook-secret-input"
              type="password"
              value={secret}
              onChange={(e) => setSecret(e.target.value)}
              placeholder="留空则不签名；填写后将附带 X-Ambra-Signature 请求头"
              className="w-full rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2.5 text-sm text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
            />
          </div>

          <ToggleSwitch
            checked={enabled}
            onChange={setEnabled}
            label="启用此 Webhook"
            description="关闭后将暂停向该端点发送事件推送"
          />

          <div className="rounded-xl border border-white/8 bg-white/[0.02] p-4">
            <p className="mb-2.5 text-xs font-semibold text-zinc-200">订阅事件 *</p>
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
                      <p className="truncate font-medium">{ev.label}</p>
                      <p className="mt-0.5 text-[11px] text-zinc-400">{ev.desc}</p>
                    </div>
                  </label>
                )
              })}
            </div>
          </div>

          <div className="flex justify-end gap-2 pt-2">
            <button
              type="button"
              onClick={() => setModalOpen(false)}
              className="rounded-xl border border-white/10 px-4 py-2 text-xs font-medium text-zinc-300 hover:bg-white/5"
            >
              取消
            </button>
            <button
              type="submit"
              disabled={saving}
              className="inline-flex items-center gap-2 rounded-xl bg-amber-500 px-4 py-2 text-xs font-semibold text-zinc-950 hover:bg-amber-400 disabled:opacity-60"
            >
              {saving && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              <span>{editingId ? '保存修改' : '创建 Webhook'}</span>
            </button>
          </div>
        </form>
      </AdminModal>

      {/* 删除确认弹窗 */}
      <AdminModal
        open={Boolean(deleteTarget)}
        title="确认删除 Webhook"
        subtitle="删除后将不再向此端点发送任何事件通知"
        maxWidthClass="max-w-md"
        onClose={() => setDeleteTarget(null)}
      >
        <p className="text-sm text-zinc-300">
          确定要删除 Webhook <span className="font-semibold text-white">{deleteTarget?.Name}</span>{' '}
          吗？
        </p>
        <div className="mt-6 flex justify-end gap-2">
          <button
            type="button"
            onClick={() => setDeleteTarget(null)}
            className="rounded-xl border border-white/10 px-4 py-2 text-xs font-medium text-zinc-300 hover:bg-white/5"
          >
            取消
          </button>
          <button
            type="button"
            disabled={deleting}
            onClick={handleDelete}
            className="inline-flex items-center gap-2 rounded-xl bg-red-500 px-4 py-2 text-xs font-semibold text-white hover:bg-red-400 disabled:opacity-60"
          >
            {deleting && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
            <span>确认删除</span>
          </button>
        </div>
      </AdminModal>
    </div>
  )
}
