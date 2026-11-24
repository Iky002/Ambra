import React, { createContext, useCallback, useContext, useEffect, useState } from 'react'
import { api, clearAuth, getDeviceId, getStoredAuth, saveAuth } from '../api/client'
import type { AuthSession, AuthenticationResult, UserDto } from '../api/types'
import { isDemoMode } from '../demo/demoMode'

const DEMO_LOGGED_OUT_KEY = 'ambra_demo_logged_out'

interface AuthContextValue {
  auth: AuthSession | null
  user: UserDto | null
  isAdmin: boolean
  userLoading: boolean
  login: (username: string, password: string) => Promise<void>
  applyAuthResult: (res: AuthenticationResult) => void
  logout: () => void
  refreshUser: () => Promise<void>
  switchDemoRole: (role: 'admin' | 'guest') => Promise<void>
}

const AuthContext = createContext<AuthContextValue | null>(null)

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [auth, setAuth] = useState<AuthSession | null>(() => getStoredAuth())
  const [user, setUser] = useState<UserDto | null>(null)
  const [userLoading, setUserLoading] = useState<boolean>(() => Boolean(getStoredAuth()))

  const logout = useCallback(() => {
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
    setAuth(null)
    setUser(null)
    setUserLoading(false)
  }, [])

  const refreshUser = useCallback(async () => {
    const current = getStoredAuth()
    if (!current?.userId) {
      setUser(null)
      setUserLoading(false)
      return
    }
    setUserLoading(true)
    try {
      const freshUser = await api.getUser(current.userId)
      setUser(freshUser)
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
    } catch {
      // 如果因 401 失效，embyFetch 会触发 ambra:unauthorized 事件清空 auth
    } finally {
      setUserLoading(false)
    }
  }, [])

  const applyAuthResult = useCallback((res: AuthenticationResult) => {
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
    saveAuth(session)
    setAuth(session)
    setUser(res.User)
    setUserLoading(false)
  }, [])

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
      void refreshUser()
    } else {
      setUser(null)
      setUserLoading(false)
    }
  }, [auth?.userId, refreshUser])

  useEffect(() => {
    const onUnauthorized = () => {
      setAuth(null)
      setUser(null)
      setUserLoading(false)
    }
    window.addEventListener('ambra:unauthorized', onUnauthorized)
    return () => window.removeEventListener('ambra:unauthorized', onUnauthorized)
  }, [])

  // 严格以当前用户 Policy.IsAdministrator 为准；用户资料尚未加载完时回退至登录会话中的 isAdmin
  const isAdmin = user ? Boolean(user.Policy?.IsAdministrator) : Boolean(auth?.isAdmin)

  return (
    <AuthContext.Provider
      value={{
        auth,
        user,
        isAdmin,
        userLoading,
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
