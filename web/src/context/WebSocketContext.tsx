import React, { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react'
import { Bell, X } from 'lucide-react'
import { getDeviceId } from '../api/client'
import { isDemoMode } from '../demo/demoMode'
import { useAuth } from './AuthContext'

export interface PlaystateCommand {
  Command: 'Pause' | 'Unpause' | 'PlayPause' | 'Stop' | 'Seek' | string
  SeekPositionTicks?: number
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

export function WebSocketProvider({ children }: { children: React.ReactNode }) {
  const { auth } = useAuth()
  const [connected, setConnected] = useState(false)
  const [toasts, setToasts] = useState<ToastNotification[]>([])

  const playstateListenersRef = useRef<Set<(cmd: PlaystateCommand) => void>>(new Set())
  const toastTimersRef = useRef<Map<string, number>>(new Map())

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
    }
  }, [])

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
      }
    },
    [pushToast],
  )

  const subscribePlaystate = useCallback((cb: (cmd: PlaystateCommand) => void) => {
    playstateListenersRef.current.add(cb)
    return () => {
      playstateListenersRef.current.delete(cb)
    }
  }, [])

  // Demo 模式及本地调试事件桥：支持通过 window.dispatchEvent(new CustomEvent('ambra:ws-mock', { detail })) 模拟消息
  useEffect(() => {
    const onMockEvent = (evt: Event) => {
      const customEvt = evt as CustomEvent<unknown>
      handleIncomingPayload(customEvt.detail)
    }
    window.addEventListener('ambra:ws-mock', onMockEvent)
    return () => window.removeEventListener('ambra:ws-mock', onMockEvent)
  }, [handleIncomingPayload])

  // 连接 /embywebsocket（含指数退避重连 + 30s KeepAlive 心跳）
  useEffect(() => {
    const token = auth?.accessToken
    if (!token || isDemoMode() || typeof WebSocket === 'undefined') {
      setConnected(false)
      return
    }

    let disposed = false
    let ws: WebSocket | null = null
    let reconnectTimer: number | null = null
    let keepAliveTimer: number | null = null
    let reconnectDelay = INITIAL_RECONNECT_DELAY_MS

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

    const connect = () => {
      if (disposed) return
      clearKeepAlive()

      const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
      const deviceId = auth.deviceId || getDeviceId()
      const wsUrl = `${protocol}//${window.location.host}/embywebsocket?api_key=${encodeURIComponent(token)}&deviceId=${encodeURIComponent(deviceId)}`

      try {
        ws = new WebSocket(wsUrl)
      } catch {
        setConnected(false)
        reconnectTimer = window.setTimeout(() => {
          reconnectDelay = Math.min(MAX_RECONNECT_DELAY_MS, reconnectDelay * 2)
          connect()
        }, reconnectDelay)
        return
      }

      ws.onopen = () => {
        if (disposed) {
          ws?.close()
          return
        }
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
        clearKeepAlive()
        setConnected(false)
        if (disposed) return
        const delay = reconnectDelay
        reconnectDelay = Math.min(MAX_RECONNECT_DELAY_MS, reconnectDelay * 2)
        clearReconnect()
        reconnectTimer = window.setTimeout(connect, delay)
      }
    }

    connect()

    return () => {
      disposed = true
      clearReconnect()
      clearKeepAlive()
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
  }, [auth?.accessToken, auth?.deviceId, handleIncomingPayload])

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
