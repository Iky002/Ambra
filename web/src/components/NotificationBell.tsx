import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  AlertCircle,
  AlertTriangle,
  Bell,
  CheckCheck,
  Info,
  Loader2,
  Megaphone,
  Send,
  X,
} from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import { api } from '../api/client'
import type { NotificationItem } from '../api/types'
import { useAuth } from '../context/AuthContext'
import { useI18n } from '../i18n/I18nContext'
import { formatRelativeTime } from '../pages/admin/adminUtils'

type NotificationLevel = 'Normal' | 'Warning' | 'Error'

function resolveSafeInternalUrl(raw?: string): string | null {
  if (!raw) return null
  const trimmed = raw.trim()
  if (!trimmed) return null
  if (trimmed.startsWith('#/')) {
    const hashPath = trimmed.slice(1)
    return hashPath.startsWith('/') && !hashPath.startsWith('//') ? hashPath : null
  }
  if (trimmed.startsWith('/') && !trimmed.startsWith('//')) {
    return trimmed
  }
  return null
}

function getLevelPresentation(level: string) {
  switch (level) {
    case 'Error':
      return {
        icon: AlertCircle,
        iconClass: 'bg-red-500/15 text-red-400 ring-1 ring-red-500/30',
        label: '严重告警',
      }
    case 'Warning':
      return {
        icon: AlertTriangle,
        iconClass: 'bg-amber-500/15 text-amber-400 ring-1 ring-amber-500/30',
        label: '警告提醒',
      }
    default:
      return {
        icon: Info,
        iconClass: 'bg-sky-500/15 text-sky-400 ring-1 ring-sky-500/30',
        label: '系统通知',
      }
  }
}

export function NotificationBell() {
  const { auth, isAdmin } = useAuth()
  const { locale, t } = useI18n()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const containerRef = useRef<HTMLDivElement | null>(null)

  const [open, setOpen] = useState(false)
  const [composeOpen, setComposeOpen] = useState(false)
  const [announceName, setAnnounceName] = useState('')
  const [announceDesc, setAnnounceDesc] = useState('')
  const [announceLevel, setAnnounceLevel] = useState<NotificationLevel>('Normal')
  const [composeError, setComposeError] = useState<string | null>(null)

  const userId = auth?.userId || ''

  const { data: summary } = useQuery({
    queryKey: ['notifications', 'summary', userId],
    queryFn: () => api.getNotificationsSummary(userId),
    enabled: Boolean(userId),
    refetchInterval: 45_000,
    staleTime: 20_000,
  })

  const {
    data: notificationsData,
    isLoading: listLoading,
    isError: listError,
    refetch: refetchList,
  } = useQuery({
    queryKey: ['notifications', 'list', userId],
    queryFn: () => api.getNotifications(userId, 0, 20),
    enabled: open && Boolean(userId),
  })

  const invalidateNotifications = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ['notifications', 'summary', userId] }),
      queryClient.invalidateQueries({ queryKey: ['notifications', 'list', userId] }),
    ])
  }

  const markReadMutation = useMutation({
    mutationFn: (ids?: string[]) => api.markNotificationsRead(userId, ids),
    onSuccess: async () => {
      await invalidateNotifications()
    },
  })

  const sendAdminMutation = useMutation({
    mutationFn: (payload: { Name: string; Description?: string; Level: NotificationLevel }) =>
      api.sendAdminNotification(payload),
    onSuccess: async () => {
      setAnnounceName('')
      setAnnounceDesc('')
      setAnnounceLevel('Normal')
      setComposeError(null)
      setComposeOpen(false)
      await invalidateNotifications()
    },
    onError: (err) => {
      setComposeError(err instanceof Error ? err.message : '发送公告失败，请稍后重试')
    },
  })

  useEffect(() => {
    if (!open) return
    const handlePointerDown = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', handlePointerDown)
    window.addEventListener('keydown', handleKeyDown)
    return () => {
      document.removeEventListener('mousedown', handlePointerDown)
      window.removeEventListener('keydown', handleKeyDown)
    }
  }, [open])

  if (!userId) return null

  const unreadCount = summary?.UnreadCount ?? 0
  const notifications = notificationsData?.Notifications ?? []
  const hasUnread = unreadCount > 0 || notifications.some((n) => !n.IsRead)

  const handleItemClick = (item: NotificationItem) => {
    if (!item.IsRead && !markReadMutation.isPending) {
      markReadMutation.mutate([item.Id])
    }
    const targetPath = resolveSafeInternalUrl(item.Url)
    if (targetPath) {
      setOpen(false)
      navigate(targetPath)
    }
  }

  const handleSendAnnouncement = (e: React.FormEvent) => {
    e.preventDefault()
    const trimmedName = announceName.trim()
    if (!trimmedName) {
      setComposeError('请输入公告标题')
      return
    }
    setComposeError(null)
    sendAdminMutation.mutate({
      Name: trimmedName,
      Description: announceDesc.trim() || undefined,
      Level: announceLevel,
    })
  }

  return (
    <div ref={containerRef} className="relative">
      <button
        type="button"
        data-testid="notification-bell-button"
        aria-label={t('notifications.title')}
        aria-expanded={open}
        aria-haspopup="dialog"
        onClick={() => setOpen((prev) => !prev)}
        className={`relative flex h-9 w-9 items-center justify-center rounded-xl border transition-colors ${
          open
            ? 'border-amber-400/50 bg-amber-500/15 text-amber-300'
            : 'border-white/10 bg-white/[0.04] text-zinc-300 hover:border-amber-400/40 hover:bg-white/[0.08] hover:text-zinc-100'
        }`}
      >
        <Bell className="h-4 w-4" />
        {unreadCount > 0 && (
          <span
            data-testid="notification-unread-badge"
            className="absolute -top-1 -right-1 flex min-w-[18px] items-center justify-center rounded-full bg-amber-500 px-1 py-0.5 text-[10px] leading-none font-bold text-zinc-950 shadow-sm ring-2 ring-[#09090d]"
          >
            {unreadCount > 99 ? '99+' : unreadCount}
          </span>
        )}
      </button>

      {open && (
        <div
          role="dialog"
          aria-label={t('notifications.title')}
          data-testid="notification-dropdown"
          className="absolute right-0 z-50 mt-2 w-80 overflow-hidden rounded-2xl border border-white/12 bg-[#111118]/98 shadow-2xl shadow-black/60 backdrop-blur-xl sm:w-96"
        >
          {/* 头部 */}
          <div className="flex items-center justify-between gap-2 border-b border-white/8 px-4 py-3">
            <div className="flex items-center gap-2">
              <Bell className="h-4 w-4 text-amber-400" />
              <h2 className="text-sm font-semibold text-zinc-100">{t('notifications.title')}</h2>
              {unreadCount > 0 && (
                <span className="rounded-full bg-amber-500/15 px-2 py-0.5 text-[10px] font-semibold text-amber-300">
                  {unreadCount} 未读
                </span>
              )}
            </div>

            <div className="flex items-center gap-1.5">
              {isAdmin && (
                <button
                  type="button"
                  onClick={() => {
                    setComposeError(null)
                    setComposeOpen((prev) => !prev)
                  }}
                  className={`inline-flex items-center gap-1 rounded-lg px-2 py-1 text-[11px] font-medium transition-colors ${
                    composeOpen
                      ? 'bg-amber-500/20 text-amber-300'
                      : 'text-zinc-400 hover:bg-white/8 hover:text-zinc-100'
                  }`}
                >
                  <Megaphone className="h-3.5 w-3.5" />
                  <span>发送全员公告</span>
                </button>
              )}

              {hasUnread && (
                <button
                  type="button"
                  disabled={markReadMutation.isPending}
                  onClick={() => markReadMutation.mutate(undefined)}
                  className="inline-flex items-center gap-1 rounded-lg px-2 py-1 text-[11px] font-medium text-amber-300 transition-colors hover:bg-amber-500/15 disabled:opacity-50"
                >
                  <CheckCheck className="h-3.5 w-3.5" />
                  <span>{locale === 'zh-CN' ? '全部标为已读' : t('notifications.markAllRead')}</span>
                </button>
              )}
            </div>
          </div>

          {/* 管理员内联发送全员公告表单 */}
          {isAdmin && composeOpen && (
            <form
              data-testid="admin-notification-form"
              onSubmit={handleSendAnnouncement}
              className="space-y-2.5 border-b border-white/8 bg-white/[0.02] p-3.5"
            >
              <div className="flex items-center justify-between">
                <span className="text-xs font-semibold text-amber-300">发送全员公告</span>
                <button
                  type="button"
                  aria-label="关闭公告表单"
                  onClick={() => {
                    setComposeOpen(false)
                    setComposeError(null)
                  }}
                  className="rounded p-0.5 text-zinc-400 hover:bg-white/8 hover:text-zinc-200"
                >
                  <X className="h-3.5 w-3.5" />
                </button>
              </div>

              {composeError && (
                <div
                  role="alert"
                  className="rounded-lg border border-red-500/30 bg-red-500/10 px-2.5 py-1.5 text-[11px] text-red-300"
                >
                  {composeError}
                </div>
              )}

              <input
                type="text"
                aria-label="公告标题"
                value={announceName}
                onChange={(e) => setAnnounceName(e.target.value)}
                placeholder="输入公告标题 *"
                className="w-full rounded-lg border border-white/10 bg-zinc-900/90 px-3 py-1.5 text-xs text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
              />

              <textarea
                rows={2}
                aria-label="公告内容"
                value={announceDesc}
                onChange={(e) => setAnnounceDesc(e.target.value)}
                placeholder="输入公告详细内容（可选）"
                className="w-full resize-none rounded-lg border border-white/10 bg-zinc-900/90 px-3 py-1.5 text-xs text-zinc-100 placeholder-zinc-500 outline-none focus:border-amber-400/60"
              />

              <div className="flex items-center justify-between gap-2">
                <select
                  aria-label="通知级别"
                  value={announceLevel}
                  onChange={(e) => setAnnounceLevel(e.target.value as NotificationLevel)}
                  className="rounded-lg border border-white/10 bg-[#14141c] px-2.5 py-1.5 text-xs text-zinc-200 outline-none focus:border-amber-400/60"
                >
                  <option value="Normal">普通 (Normal)</option>
                  <option value="Warning">警告 (Warning)</option>
                  <option value="Error">严重 (Error)</option>
                </select>

                <button
                  type="submit"
                  disabled={sendAdminMutation.isPending}
                  className="inline-flex items-center gap-1.5 rounded-lg bg-amber-500 px-3 py-1.5 text-xs font-semibold text-zinc-950 transition-colors hover:bg-amber-400 disabled:opacity-50"
                >
                  {sendAdminMutation.isPending ? (
                    <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  ) : (
                    <Send className="h-3.5 w-3.5" />
                  )}
                  <span>发送公告</span>
                </button>
              </div>
            </form>
          )}

          {/* 通知列表 */}
          <div className="max-h-96 overflow-y-auto divide-y divide-white/6">
            {listLoading ? (
              <div className="flex h-32 items-center justify-center">
                <Loader2 className="h-5 w-5 animate-spin text-amber-400" />
              </div>
            ) : listError ? (
              <div className="space-y-2 px-4 py-8 text-center text-xs text-zinc-400">
                <p>加载通知列表失败</p>
                <button
                  type="button"
                  onClick={() => void refetchList()}
                  className="rounded-lg border border-white/10 bg-white/[0.04] px-3 py-1 text-zinc-200 hover:border-amber-400/40 hover:text-amber-300"
                >
                  {t('common.retry')}
                </button>
              </div>
            ) : notifications.length === 0 ? (
              <div className="px-4 py-10 text-center text-xs text-zinc-500">
                {locale === 'zh-CN' ? '暂无新通知' : t('notifications.empty')}
              </div>
            ) : (
              notifications.map((item) => {
                const { icon: LevelIcon, iconClass } = getLevelPresentation(item.Level)
                return (
                  <button
                    key={item.Id}
                    type="button"
                    data-testid={`notification-item-${item.Id}`}
                    onClick={() => handleItemClick(item)}
                    className={`flex w-full items-start gap-3 px-4 py-3 text-left transition-colors ${
                      item.IsRead
                        ? 'bg-transparent opacity-75 hover:bg-white/[0.03]'
                        : 'bg-white/[0.03] hover:bg-white/[0.06]'
                    }`}
                  >
                    <div
                      className={`mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-xl ${iconClass}`}
                    >
                      <LevelIcon className="h-4 w-4" />
                    </div>
                    <div className="min-w-0 flex-1 space-y-0.5">
                      <div className="flex items-start justify-between gap-2">
                        <p className="text-xs font-semibold text-zinc-100">{item.Name}</p>
                        {!item.IsRead && (
                          <span
                            data-testid={`notification-unread-dot-${item.Id}`}
                            className="mt-1 h-2 w-2 shrink-0 rounded-full bg-amber-400"
                            aria-label="未读"
                          />
                        )}
                      </div>
                      {item.Description && (
                        <p className="line-clamp-2 text-[11px] leading-relaxed text-zinc-400">
                          {item.Description}
                        </p>
                      )}
                      <p className="text-[10px] text-zinc-500">
                        {formatRelativeTime(item.Date)}
                      </p>
                    </div>
                  </button>
                )
              })
            )}
          </div>
        </div>
      )}
    </div>
  )
}
