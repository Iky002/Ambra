import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api, getStoredAuth, saveAuth } from '../../api/client'
import { AuthProvider } from '../../context/AuthContext'
import { setDemoModeOverride } from '../../demo/demoMode'
import { resetDemoStore } from '../../demo/mockServer'
import { LoginPage } from '../LoginPage'
import { ProfilePage } from '../ProfilePage'

function jsonRes(data: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    headers: new Headers({ 'content-type': 'application/json' }),
    json: async () => data,
    text: async () => JSON.stringify(data),
  } as Response
}

function renderWithProviders(ui: React.ReactNode, initialEntries = ['/login']) {
  const qc = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
  return render(
    <QueryClientProvider client={qc}>
      <AuthProvider>
        <MemoryRouter initialEntries={initialEntries}>{ui}</MemoryRouter>
      </AuthProvider>
    </QueryClientProvider>,
  )
}

describe('Stream 4 (v0.9.3): QuickConnect Login, Profile Device Authorization & Demo Mocks', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    setDemoModeOverride(null)
    resetDemoStore()
  })

  afterEach(() => {
    vi.useRealTimers()
    setDemoModeOverride(null)
    vi.restoreAllMocks()
    localStorage.clear()
    sessionStorage.clear()
  })

  it('LoginPage 显示 v0.9.3 回退版本，启用 Quick Connect 时展示按钮并完成轮询登录跳转首页', async () => {
    let connectPollCount = 0
    let authWithQcCalled = false

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/api/setup/status') {
        return jsonRes({ completed: true, server_name: 'Ambra', default_language: 'zh-CN', has_users: true, has_tmdb_key: true })
      }
      if (url === '/emby/System/Info/Public') {
        return jsonRes({ Id: 'srv-1', ServerName: 'Ambra Server', ProductName: 'Ambra' })
      }
      if (url === '/emby/Users/Public') {
        return jsonRes([])
      }
      if (url === '/emby/QuickConnect/Enabled') {
        return jsonRes(true)
      }
      if (url === '/emby/QuickConnect/Initiate') {
        return jsonRes({
          Authenticated: false,
          Secret: 'sec-test-001',
          Code: '654321',
          DateAdded: new Date().toISOString(),
        })
      }
      if (url.startsWith('/emby/QuickConnect/Connect?secret=sec-test-001')) {
        connectPollCount += 1
        return jsonRes({
          Authenticated: connectPollCount >= 2,
          Secret: 'sec-test-001',
          Code: '654321',
        })
      }
      if (url === '/emby/Users/AuthenticateWithQuickConnect') {
        authWithQcCalled = true
        return jsonRes({
          User: {
            Id: 'u-admin',
            Name: 'Admin',
            ServerId: 'srv-1',
            Policy: { IsAdministrator: true },
          },
          AccessToken: 'tok-qc-success',
          ServerId: 'srv-1',
        })
      }
      if (url === '/emby/Users/u-admin') {
        return jsonRes({
          Id: 'u-admin',
          Name: 'Admin',
          ServerId: 'srv-1',
          Policy: { IsAdministrator: true },
        })
      }
      return jsonRes({})
    })

    renderWithProviders(
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route path="/" element={<div data-testid="home-page">Ambra Home</div>} />
      </Routes>,
    )

    // 验证回退版本号更新为 v0.12.1，且默认隐藏公开用户列表时不渲染快捷选号区
    await waitFor(() => {
      expect(screen.getByText(/v0\.12\.1/)).toBeInTheDocument()
    })
    expect(screen.queryByText('选择账号快速登录')).not.toBeInTheDocument()

    // 显示 Quick Connect 按钮
    const qcButton = await screen.findByRole('button', { name: /Quick Connect 授权码登录/ })
    expect(qcButton).toBeInTheDocument()

    // 在打开弹窗前启用 fake timers，确保 setInterval 注册在 fake timer 上
    vi.useFakeTimers()

    // 点击打开弹窗并获取 6 位授权码
    await act(async () => {
      fireEvent.click(qcButton)
      await Promise.resolve()
      await Promise.resolve()
    })

    const codeEl = screen.getByTestId('quick-connect-code')
    expect(codeEl).toHaveTextContent('654321')
    expect(codeEl.className).toContain('tracking-[0.35em]')
    expect(
      screen.getByText('在已登录的 Ambra 网页端「个人中心 → Quick Connect」输入此授权码'),
    ).toBeInTheDocument()

    // 第 1 次轮询：Authenticated = false
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2000)
    })
    expect(connectPollCount).toBe(1)
    expect(authWithQcCalled).toBe(false)

    // 第 2 次轮询：Authenticated = true -> 触发 AuthenticateWithQuickConnect 并导航到 /
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2000)
    })

    vi.useRealTimers()

    await waitFor(() => {
      expect(authWithQcCalled).toBe(true)
      expect(screen.getByTestId('home-page')).toBeInTheDocument()
    })

    const stored = getStoredAuth()
    expect(stored?.accessToken).toBe('tok-qc-success')
    expect(stored?.userId).toBe('u-admin')
  })

  it('LoginPage 关闭 Quick Connect 弹窗时清理轮询定时器，未启用时不渲染入口按钮', async () => {
    let connectPollCount = 0
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/api/setup/status') {
        return jsonRes({ completed: true, server_name: 'Ambra', default_language: 'zh-CN', has_users: true, has_tmdb_key: true })
      }
      if (url === '/emby/System/Info/Public') {
        return jsonRes({ Id: 'srv-1', ServerName: 'Ambra Server', ProductName: 'Ambra' })
      }
      if (url === '/emby/Users/Public') {
        return jsonRes([])
      }
      if (url === '/emby/QuickConnect/Enabled') {
        return jsonRes(true)
      }
      if (url === '/emby/QuickConnect/Initiate') {
        return jsonRes({ Authenticated: false, Secret: 'sec-close-test', Code: '888999' })
      }
      if (url.startsWith('/emby/QuickConnect/Connect')) {
        connectPollCount += 1
        return jsonRes({ Authenticated: false, Secret: 'sec-close-test', Code: '888999' })
      }
      return jsonRes({})
    })

    renderWithProviders(<LoginPage />)

    const qcButton = await screen.findByRole('button', { name: /Quick Connect 授权码登录/ })
    vi.useFakeTimers()

    await act(async () => {
      fireEvent.click(qcButton)
      await Promise.resolve()
      await Promise.resolve()
    })
    expect(screen.getByTestId('quick-connect-code')).toHaveTextContent('888999')

    fireEvent.click(screen.getByRole('button', { name: '关闭 Quick Connect 弹窗' }))

    await act(async () => {
      await vi.advanceTimersByTimeAsync(6000)
    })
    expect(connectPollCount).toBe(0)
  })

  it('LoginPage 在 QuickConnect/Enabled 返回 false 或失败且非 Demo 模式时静默隐藏按钮', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/api/setup/status') {
        return jsonRes({ completed: true, server_name: 'Ambra', default_language: 'zh-CN', has_users: true, has_tmdb_key: true })
      }
      if (url === '/emby/System/Info/Public') {
        return jsonRes({ Id: 'srv-1', ServerName: 'Ambra Server', ProductName: 'Ambra' })
      }
      if (url === '/emby/Users/Public') {
        return jsonRes([])
      }
      if (url === '/emby/QuickConnect/Enabled') {
        return jsonRes({ error: 'Not Found' }, 404)
      }
      return jsonRes({})
    })

    renderWithProviders(<LoginPage />)

    await waitFor(() => {
      expect(screen.getByText('Ambra Server')).toBeInTheDocument()
    })
    expect(screen.queryByRole('button', { name: /Quick Connect 授权码登录/ })).not.toBeInTheDocument()
  })

  it('ProfilePage Quick Connect 授权新设备卡片支持输入授权码、成功提示与错误处理', async () => {
    const user = userEvent.setup()
    saveAuth({
      accessToken: 'tok-profile',
      userId: 'u-1',
      serverId: 'srv-1',
      userName: 'Admin',
      isAdmin: true,
      deviceId: 'dev-1',
    })

    const authorizedCodes: string[] = []

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/emby/Users/u-1') {
        return jsonRes({ Id: 'u-1', Name: 'Admin', Policy: { IsAdministrator: true } })
      }
      if (url.startsWith('/emby/QuickConnect/Authorize')) {
        const parsed = new URL(url, 'http://localhost')
        const code = parsed.searchParams.get('code') || ''
        if (code === '123456') {
          authorizedCodes.push(code)
          return jsonRes(true)
        }
        return jsonRes({ error: '授权码不存在或已过期' }, 404)
      }
      return jsonRes({})
    })

    renderWithProviders(<ProfilePage />, ['/profile'])

    expect(await screen.findByText('Quick Connect 授权新设备')).toBeInTheDocument()

    const codeInput = screen.getByPlaceholderText('例如：123456')
    const submitBtn = screen.getByRole('button', { name: '确认授权' })

    // 空输入校验
    await user.click(submitBtn)
    expect(await screen.findByText('请输入 6 位 Quick Connect 授权码')).toBeInTheDocument()

    // 错误授权码提示
    await user.type(codeInput, '000000')
    await user.click(submitBtn)
    expect(await screen.findByText(/授权码不存在或已过期/)).toBeInTheDocument()

    // 正确授权码提示成功并清空输入
    await user.clear(codeInput)
    await user.type(codeInput, ' 123456 ')
    await user.click(submitBtn)
    expect(await screen.findByText('已成功授权该设备登录')).toBeInTheDocument()
    expect(authorizedCodes).toEqual(['123456'])
    expect(codeInput).toHaveValue('')
  })

  it('Demo Mock 完整支持 Notifications 与 QuickConnect 全链路闭环', async () => {
    setDemoModeOverride(true)
    const authRes = await api.authenticateByName('admin', '')
    saveAuth({
      accessToken: authRes.AccessToken,
      userId: authRes.User.Id,
      serverId: authRes.ServerId,
      userName: authRes.User.Name,
      isAdmin: true,
      deviceId: 'dev-demo-test',
    })
    const uid = authRes.User.Id

    // 1. Notifications 列表与未读汇总
    const initialNotifs = await api.getNotifications(uid)
    expect(initialNotifs.TotalRecordCount).toBeGreaterThanOrEqual(3)
    const summaryBefore = await api.getNotificationsSummary(uid)
    expect(summaryBefore.UnreadCount).toBe(2)
    expect(summaryBefore.MaxUnreadNotificationLevel).toBe('Normal')

    // 2. 管理员广播 Warning 级别通知
    const created = await api.sendAdminNotification({
      Name: '维护公告',
      Description: '今晚 02:00 进行索引优化',
      Level: 'Warning',
    })
    expect(created.Name).toBe('维护公告')
    expect(created.IsRead).toBe(false)

    const summaryAfterBroadcast = await api.getNotificationsSummary(uid)
    expect(summaryAfterBroadcast.UnreadCount).toBe(3)
    expect(summaryAfterBroadcast.MaxUnreadNotificationLevel).toBe('Warning')

    // 3. 标记单条已读 & 全部已读
    await api.markNotificationsRead(uid, [created.Id])
    const summaryAfterSingleRead = await api.getNotificationsSummary(uid)
    expect(summaryAfterSingleRead.UnreadCount).toBe(2)
    expect(summaryAfterSingleRead.MaxUnreadNotificationLevel).toBe('Normal')

    await api.markNotificationsRead(uid)
    const summaryAfterAllRead = await api.getNotificationsSummary(uid)
    expect(summaryAfterAllRead.UnreadCount).toBe(0)

    // 4. QuickConnect 全流程
    expect(await api.getQuickConnectEnabled()).toBe(true)

    const initiated = await api.quickConnectInitiate()
    expect(initiated.Code).toMatch(/^\d{6}$/)
    expect(initiated.Secret).toBeTruthy()
    expect(initiated.Authenticated).toBe(false)

    // 未授权前 Connect 返回 Authenticated = false，尝试登录应 401
    const pendingState = await api.quickConnectConnect(initiated.Secret)
    expect(pendingState.Authenticated).toBe(false)
    await expect(api.authenticateWithQuickConnect(initiated.Secret)).rejects.toMatchObject({
      status: 401,
    })

    // Authorize 批准后 Connect 返回 Authenticated = true
    expect(await api.quickConnectAuthorize(initiated.Code)).toBe(true)
    const approvedState = await api.quickConnectConnect(initiated.Secret)
    expect(approvedState.Authenticated).toBe(true)

    // 演示码 '123456' 也可直接通过授权
    expect(await api.quickConnectAuthorize('123456')).toBe(true)

    // AuthenticateWithQuickConnect 返回管理员会话
    const qcAuth = await api.authenticateWithQuickConnect(initiated.Secret)
    expect(qcAuth.AccessToken).toBeTruthy()
    expect(qcAuth.User.Policy?.IsAdministrator).toBe(true)
  })
})
