import React, { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { Bell, X } from 'lucide-react'
import { api, ApiError, clearAuth, getDeviceId } from '../api/client'
import { isDemoMode } from '../demo/demoMode'
import { useAuth } from './AuthContext'

export interface PlaystateCommand {
  Command: 'Pause' | 'Unpause' | 'PlayPause' | 'Stop' | 'Seek' | string
  SeekPositionTicks?: number | string
  ControllingUserId?: string
}

export interface DisplayMessagePayload {
  Header?: string
  Text?: string
  TimeoutMs?: number
}

interface ToastNotification {
  id: string
  header: string
  text: string
}

interface WebSocketEnvelope {
  MessageType?: string
  Data?: unknown
}

export interface WebSocketContextValue {
  connected: boolean
  subscribePlaystate: (cb: (cmd: PlaystateCommand) => void) => () => void
}

const defaultWebSocketContext: WebSocketContextValue = {
  connected: false,
  subscribePlaystate: () => () => undefined,
}

const WebSocketContext = createContext<WebSocketContextValue>(defaultWebSocketContext)

const INITIAL_RECONNECT_DELAY_MS = 1_000
const MAX_RECONNECT_DELAY_MS = 30_000
const KEEPALIVE_INTERVAL_MS = 30_000
const DEFAULT_TOAST_TIMEOUT_MS = 5_000
const MAX_VISIBLE_TOASTS = 5
const INVALIDATE_DEBOUNCE_MS = 350
const AUTH_PROBE_FAILURE_THRESHOLD = 3
const MAX_CONSECUTIVE_HANDSHAKE_FAILURES = 5

const USER_DATA_CHANGED_KEYS = [
  'resumeItems',
  'nextUp',
  'favorites',
  'episodes',
  'seasons',
  'libraryItems',
  'item',
] as const

const LIBRARY_CHANGED_KEYS = [
  'latestItems',
  'libraryItems',
  'userViews',
  'collections',
] as const

export function WebSocketProvider({ children }: { children: React.ReactNode }) {
  const { auth } = useAuth()
  const queryClient = useQueryClient()
  const [connected, setConnected] = useState(false)
  const [toasts, setToasts] = useState<ToastNotification[]>([])

  const playstateListenersRef = useRef<Set<(cmd: PlaystateCommand) => void>>(new Set())
  const toastTimersRef = useRef<Map<string, number>>(new Map())
  const pendingInvalidationsRef = useRef<Set<string>>(new Set())
  const invalidateTimerRef = useRef<number | null>(null)

  const clearPendingInvalidations = useCallback(() => {
    if (invalidateTimerRef.current !== null) {
      window.clearTimeout(invalidateTimerRef.current)
      invalidateTimerRef.current = null
    }
    pendingInvalidationsRef.current.clear()
  }, [])

  const scheduleInvalidations = useCallback(
    (keys: readonly string[]) => {
      keys.forEach((key) => pendingInvalidationsRef.current.add(key))
      if (invalidateTimerRef.current !== null) {
        window.clearTimeout(invalidateTimerRef.current)
      }
      invalidateTimerRef.current = window.setTimeout(() => {
        invalidateTimerRef.current = null
        const keysToFlush = Array.from(pendingInvalidationsRef.current)
        pendingInvalidationsRef.current.clear()
        keysToFlush.forEach((key) => {
          void queryClient.invalidateQueries({ queryKey: [key] })
        })
      }, INVALIDATE_DEBOUNCE_MS)
    },
    [queryClient],
  )

  const dismissToast = useCallback((id: string) => {
    const timer = toastTimersRef.current.get(id)
    if (timer !== undefined) {
      window.clearTimeout(timer)
      toastTimersRef.current.delete(id)
    }
    setToasts((prev) => prev.filter((t) => t.id !== id))
  }, [])

  const pushToast = useCallback(
    (payload: DisplayMessagePayload) => {
      const header = (payload.Header || '').trim()
      const text = (payload.Text || '').trim()
      if (!header && !text) return

      const rawTimeout = Number(payload.TimeoutMs)
      const timeoutMs =
        Number.isFinite(rawTimeout) && rawTimeout > 0 ? rawTimeout : DEFAULT_TOAST_TIMEOUT_MS
      const id = `ws-toast-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`

      setToasts((prev) => {
        const next = [...prev, { id, header: header || '系统消息', text }]
        if (next.length > MAX_VISIBLE_TOASTS) {
          const removed = next.shift()
          if (removed) {
            const oldTimer = toastTimersRef.current.get(removed.id)
            if (oldTimer !== undefined) {
              window.clearTimeout(oldTimer)
              toastTimersRef.current.delete(removed.id)
            }
          }
        }
        return next
      })

      const timer = window.setTimeout(() => {
        dismissToast(id)
      }, timeoutMs)
      toastTimersRef.current.set(id, timer)
    },
    [dismissToast],
  )

  useEffect(() => {
    const timers = toastTimersRef.current
    return () => {
      timers.forEach((timer) => window.clearTimeout(timer))
      timers.clear()
      clearPendingInvalidations()
    }
  }, [clearPendingInvalidations])

  const handleIncomingPayload = useCallback(
    (raw: unknown) => {
      let envelope: WebSocketEnvelope | null = null
      if (typeof raw === 'string') {
        try {
          envelope = JSON.parse(raw) as WebSocketEnvelope
        } catch {
          return
        }
      } else if (raw && typeof raw === 'object') {
        envelope = raw as WebSocketEnvelope
      }
      if (!envelope?.MessageType) return

      if (envelope.MessageType === 'GeneralCommand') {
        const data = envelope.Data as
          | { Name?: string; Arguments?: Record<string, unknown> }
          | undefined
        if (data?.Name === 'DisplayMessage') {
          const args = data.Arguments || {}
          pushToast({
            Header: typeof args.Header === 'string' ? args.Header : undefined,
            Text: typeof args.Text === 'string' ? args.Text : undefined,
            TimeoutMs:
              typeof args.TimeoutMs === 'number'
                ? args.TimeoutMs
                : typeof args.TimeoutMs === 'string'
                  ? Number(args.TimeoutMs)
                  : undefined,
          })
        }
      } else if (envelope.MessageType === 'Playstate') {
        const data = envelope.Data as PlaystateCommand | undefined
        if (data?.Command) {
          playstateListenersRef.current.forEach((cb) => {
            try {
              cb(data)
            } catch {
              // 忽略单个监听器异常，避免影响其他订阅者
            }
          })
        }
      } else if (envelope.MessageType === 'UserDataChanged') {
        scheduleInvalidations(USER_DATA_CHANGED_KEYS)
      } else if (envelope.MessageType === 'LibraryChanged') {
        scheduleInvalidations(LIBRARY_CHANGED_KEYS)
      }
    },
    [pushToast, scheduleInvalidations],
  )

  const subscribePlaystate = useCallback((cb: (cmd: PlaystateCommand) => void) => {
    playstateListenersRef.current.add(cb)
    return () => {
      playstateListenersRef.current.delete(cb)
    }
  }, [])

  const demoActive = isDemoMode()

  // 仅在 Demo 模式下注册 ambra:ws-mock 事件监听，避免生产环境暴露调试事件注入通道
  useEffect(() => {
    if (!demoActive) return
    const onMockEvent = (evt: Event) => {
      const customEvt = evt as CustomEvent<unknown>
      handleIncomingPayload(customEvt.detail)
    }
    window.addEventListener('ambra:ws-mock', onMockEvent)
    return () => window.removeEventListener('ambra:ws-mock', onMockEvent)
  }, [demoActive, handleIncomingPayload])

  // 连接 /embywebsocket（含指数退避重连 + 30s KeepAlive 心跳 + 连续握手失败鉴权探测）
  useEffect(() => {
    const token = auth?.accessToken
    const userId = auth?.userId
    if (!token || demoActive || typeof WebSocket === 'undefined') {
      setConnected(false)
      return
    }

    let disposed = false
    let ws: WebSocket | null = null
    let reconnectTimer: number | null = null
    let keepAliveTimer: number | null = null
    let reconnectDelay = INITIAL_RECONNECT_DELAY_MS
    let consecutiveHandshakeFailures = 0
    let probingAuth = false

    const clearKeepAlive = () => {
      if (keepAliveTimer !== null) {
        window.clearInterval(keepAliveTimer)
        keepAliveTimer = null
      }
    }

    const clearReconnect = () => {
      if (reconnectTimer !== null) {
        window.clearTimeout(reconnectTimer)
        reconnectTimer = null
      }
    }

    const scheduleNextReconnect = () => {
      if (disposed) return
      if (consecutiveHandshakeFailures >= MAX_CONSECUTIVE_HANDSHAKE_FAILURES) {
        return
      }
      const delay = reconnectDelay
      reconnectDelay = Math.min(MAX_RECONNECT_DELAY_MS, reconnectDelay * 2)
      clearReconnect()
      reconnectTimer = window.setTimeout(connect, delay)
    }

    const handleDisconnect = (openedDuringAttempt: boolean) => {
      clearKeepAlive()
      setConnected(false)
      if (disposed) return

      if (!openedDuringAttempt) {
        consecutiveHandshakeFailures += 1
        if (consecutiveHandshakeFailures >= AUTH_PROBE_FAILURE_THRESHOLD && !probingAuth) {
          probingAuth = true
          const probePromise = userId ? api.getUser(userId) : api.getSystemInfo()
          void probePromise
            .then(() => {
              probingAuth = false
              scheduleNextReconnect()
            })
            .catch((err: unknown) => {
              probingAuth = false
              if (disposed) return
              if (err instanceof ApiError && err.status === 401) {
                disposed = true
                clearReconnect()
                clearKeepAlive()
                clearAuth()
                window.dispatchEvent(new CustomEvent('ambra:unauthorized'))
                return
              }
              scheduleNextReconnect()
            })
          return
        }
      }

      scheduleNextReconnect()
    }

    const connect = () => {
      if (disposed) return
      clearKeepAlive()

      const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
      const deviceId = auth.deviceId || getDeviceId()
      const wsUrl = `${protocol}//${window.location.host}/embywebsocket?api_key=${encodeURIComponent(token)}&deviceId=${encodeURIComponent(deviceId)}`

      let openedDuringAttempt = false

      try {
        ws = new WebSocket(wsUrl)
      } catch {
        handleDisconnect(false)
        return
      }

      ws.onopen = () => {
        if (disposed) {
          ws?.close()
          return
        }
        openedDuringAttempt = true
        consecutiveHandshakeFailures = 0
        setConnected(true)
        reconnectDelay = INITIAL_RECONNECT_DELAY_MS
        clearKeepAlive()
        keepAliveTimer = window.setInterval(() => {
          if (ws && ws.readyState === WebSocket.OPEN) {
            try {
              ws.send(JSON.stringify({ MessageType: 'KeepAlive' }))
            } catch {
              // 忽略发送失败，等待 onclose 触发重连
            }
          }
        }, KEEPALIVE_INTERVAL_MS)
      }

      ws.onmessage = (event: MessageEvent) => {
        if (disposed) return
        handleIncomingPayload(event.data)
      }

      ws.onerror = () => {
        if (disposed) return
        setConnected(false)
      }

      ws.onclose = () => {
        handleDisconnect(openedDuringAttempt)
      }
    }

    connect()

    return () => {
      disposed = true
      clearReconnect()
      clearKeepAlive()
      clearPendingInvalidations()
      if (ws) {
        ws.onopen = null
        ws.onmessage = null
        ws.onerror = null
        ws.onclose = null
        try {
          ws.close()
        } catch {
          // 忽略关闭异常
        }
        ws = null
      }
      setConnected(false)
    }
  }, [auth?.accessToken, auth?.userId, auth?.deviceId, demoActive, handleIncomingPayload, clearPendingInvalidations])

  const value = useMemo<WebSocketContextValue>(
    () => ({
      connected,
      subscribePlaystate,
    }),
    [connected, subscribePlaystate],
  )

  return (
    <WebSocketContext.Provider value={value}>
      {children}

      {/* 全局实时消息 Toast 浮层（z-50，覆盖 Layout 与全屏 PlayerPage） */}
      {toasts.length > 0 && (
        <div
          aria-live="polite"
          className="pointer-events-none fixed right-4 top-4 z-50 flex w-80 max-w-[calc(100vw-2rem)] flex-col gap-2.5"
        >
          {toasts.map((toast) => (
            <div
              key={toast.id}
              role="status"
              className="pointer-events-auto flex items-start gap-3 rounded-2xl border border-amber-500/30 bg-zinc-950/88 p-3.5 text-zinc-100 shadow-2xl shadow-black/60 backdrop-blur-xl"
            >
              <div className="mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-xl bg-amber-500/15 text-amber-400 ring-1 ring-amber-400/30">
                <Bell className="h-4 w-4" />
              </div>
              <div className="min-w-0 flex-1">
                <p className="truncate text-xs font-semibold text-amber-300">{toast.header}</p>
                {toast.text && (
                  <p className="mt-0.5 break-words text-xs leading-relaxed text-zinc-200">
                    {toast.text}
                  </p>
                )}
              </div>
              <button
                type="button"
                aria-label="关闭通知"
                onClick={() => dismissToast(toast.id)}
                className="shrink-0 rounded-lg p-1 text-zinc-400 transition-colors hover:bg-white/10 hover:text-white"
              >
                <X className="h-3.5 w-3.5" />
              </button>
            </div>
          ))}
        </div>
      )}
    </WebSocketContext.Provider>
  )
}

export function useWebSocket(): WebSocketContextValue {
  return useContext(WebSocketContext)
}
