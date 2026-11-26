import React, { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react'
import { QueryClientContext } from '@tanstack/react-query'
import { api, clearAuth, getDeviceId, getStoredAuth, saveAuth } from '../api/client'
import type { AuthSession, AuthenticationResult, UserDto } from '../api/types'
import { isDemoMode } from '../demo/demoMode'
import { syncPlaybackPrefsFromUserConfig } from '../player/playbackPrefs'

const DEMO_LOGGED_OUT_KEY = 'ambra_demo_logged_out'

interface AuthContextValue {
  auth: AuthSession | null
  user: UserDto | null
  isAdmin: boolean
  userLoading: boolean
  userError?: string | null
  login: (username: string, password: string) => Promise<void>
  applyAuthResult: (res: AuthenticationResult) => void
  logout: () => void
  refreshUser: () => Promise<void>
  switchDemoRole: (role: 'admin' | 'guest') => Promise<void>
}

const AuthContext = createContext<AuthContextValue | null>(null)

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const queryClient = useContext(QueryClientContext)
  const [auth, setAuth] = useState<AuthSession | null>(() => getStoredAuth())
  const [user, setUser] = useState<UserDto | null>(null)
  const [userLoading, setUserLoading] = useState<boolean>(() => Boolean(getStoredAuth()))
  const [userError, setUserError] = useState<string | null>(null)
  const skipNextRefreshUserIdRef = useRef<string | null>(null)
  const refreshSeqRef = useRef(0)

  const logout = useCallback(() => {
    refreshSeqRef.current += 1
    skipNextRefreshUserIdRef.current = null
    if (isDemoMode()) {
      try {
        sessionStorage.setItem(DEMO_LOGGED_OUT_KEY, '1')
      } catch {
        // 忽略存储异常
      }
    } else if (getStoredAuth()?.accessToken) {
      // 通知后端注销当前会话（fire-and-forget，忽略网络异常）
      void api.logout().catch(() => undefined)
    }
    clearAuth()
    queryClient?.clear()
    setAuth(null)
    setUser(null)
    setUserError(null)
    setUserLoading(false)
  }, [queryClient])

  const refreshUser = useCallback(async () => {
    const seq = ++refreshSeqRef.current
    const current = getStoredAuth()
    if (!current?.userId) {
      setUser(null)
      setUserError(null)
      setUserLoading(false)
      return
    }
    setUserLoading(true)
    try {
      const freshUser = await api.getUser(current.userId)
      if (seq !== refreshSeqRef.current || getStoredAuth()?.accessToken !== current.accessToken) {
        return
      }
      setUser(freshUser)
      setUserError(null)
      syncPlaybackPrefsFromUserConfig(freshUser.Configuration)
      const freshIsAdmin = Boolean(freshUser.Policy?.IsAdministrator)
      if (current.isAdmin !== freshIsAdmin || current.userName !== freshUser.Name) {
        const updated: AuthSession = {
          ...current,
          userName: freshUser.Name,
          isAdmin: freshIsAdmin,
        }
        saveAuth(updated)
        setAuth(updated)
      }
    } catch (err) {
      if (seq !== refreshSeqRef.current || getStoredAuth()?.accessToken !== current.accessToken) {
        return
      }
      // 401 失效时 embyFetch 会触发 ambra:unauthorized 事件清空 auth/user；
      // 非 401 网络/500 错误保留既有已验证 user 与重试能力，仅记录错误状态
      setUserError(err instanceof Error ? err.message : '用户信息加载失败')
    } finally {
      if (seq === refreshSeqRef.current) {
        setUserLoading(false)
      }
    }
  }, [])

  const applyAuthResult = useCallback(
    (res: AuthenticationResult) => {
      refreshSeqRef.current += 1
      const isAdminFlag = Boolean(res.User.Policy?.IsAdministrator)
      const session: AuthSession = {
        accessToken: res.AccessToken,
        userId: res.User.Id,
        serverId: res.ServerId || res.User.ServerId || '',
        userName: res.User.Name,
        isAdmin: isAdminFlag,
        deviceId: getDeviceId(),
      }
      if (isDemoMode()) {
        try {
          sessionStorage.removeItem(DEMO_LOGGED_OUT_KEY)
        } catch {
          // 忽略存储异常
        }
      }
      // 登录或切换账号时清空 React Query 内存缓存，防止跨账号数据残留
      queryClient?.clear()
      skipNextRefreshUserIdRef.current = res.User.Id
      saveAuth(session)
      setAuth(session)
      setUser(res.User)
      setUserError(null)
      syncPlaybackPrefsFromUserConfig(res.User.Configuration)
      setUserLoading(false)
    },
    [queryClient],
  )

  const login = useCallback(
    async (username: string, password: string) => {
      const res = await api.authenticateByName(username, password)
      applyAuthResult(res)
    },
    [applyAuthResult],
  )

  const switchDemoRole = useCallback(
    async (role: 'admin' | 'guest') => {
      if (!isDemoMode()) return
      await login(role, '')
    },
    [login],
  )

  // Demo 模式下首次访问（未手动退出过）自动以演示管理员 admin 登录，开箱即享完整影院体验
  useEffect(() => {
    if (!isDemoMode() || auth?.userId) return
    let manuallyLoggedOut = false
    try {
      manuallyLoggedOut = sessionStorage.getItem(DEMO_LOGGED_OUT_KEY) === '1'
    } catch {
      manuallyLoggedOut = false
    }
    if (!manuallyLoggedOut) {
      void login('admin', '')
    }
  }, [auth?.userId, login])

  useEffect(() => {
    if (auth?.userId) {
      if (skipNextRefreshUserIdRef.current === auth.userId) {
        skipNextRefreshUserIdRef.current = null
        return
      }
      void refreshUser()
    } else {
      skipNextRefreshUserIdRef.current = null
      setUser(null)
      setUserLoading(false)
    }
  }, [auth?.userId, refreshUser])

  useEffect(() => {
    const onUnauthorized = () => {
      refreshSeqRef.current += 1
      skipNextRefreshUserIdRef.current = null
      queryClient?.clear()
      setAuth(null)
      setUser(null)
      setUserError(null)
      setUserLoading(false)
    }
    window.addEventListener('ambra:unauthorized', onUnauthorized)
    return () => window.removeEventListener('ambra:unauthorized', onUnauthorized)
  }, [queryClient])

  // 严格以服务端验证或登录响应返回的 User.Policy.IsAdministrator 为准，不信任本地缓存的 auth.isAdmin
  const isAdmin = Boolean(user?.Policy?.IsAdministrator)

  return (
    <AuthContext.Provider
      value={{
        auth,
        user,
        isAdmin,
        userLoading,
        userError,
        login,
        applyAuthResult,
        logout,
        refreshUser,
        switchDemoRole,
      }}
    >
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth 必须在 AuthProvider 内部使用')
  return ctx
}
