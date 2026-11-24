import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from '../../App'
import { saveAuth } from '../../api/client'
import { AuthProvider } from '../../context/AuthContext'
import { LoginPage } from '../LoginPage'

function jsonRes(data: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    headers: new Headers({ 'content-type': 'application/json' }),
    json: async () => data,
    text: async () => JSON.stringify(data),
  } as Response
}

describe('LoginPage - 公开用户列表与表单登录', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('拉取公开服务器信息与公开用户，点击用户并提交表单后完成登录跳转', async () => {
    const user = userEvent.setup()

    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/emby/System/Info/Public') {
        return jsonRes({
          ServerName: 'Ambra 家庭影院',
          ProductName: 'Ambra',
          Version: '1.2.0',
          Id: 'srv-home',
        })
      }
      if (url === '/emby/Users/Public') {
        return jsonRes([
          { Id: 'u-alice', Name: 'Alice', ServerId: 'srv-home' },
          { Id: 'u-bob', Name: 'Bob', ServerId: 'srv-home' },
        ])
      }
      if (url === '/emby/Users/AuthenticateByName') {
        return jsonRes({
          AccessToken: 'tok-alice',
          ServerId: 'srv-home',
          User: {
            Id: 'u-alice',
            Name: 'Alice',
            ServerId: 'srv-home',
            Policy: { IsAdministrator: true },
          },
        })
      }
      if (url === '/emby/Users/u-alice') {
        return jsonRes({
          Id: 'u-alice',
          Name: 'Alice',
          ServerId: 'srv-home',
          Policy: { IsAdministrator: true },
        })
      }
      return jsonRes({})
    })

    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })

    render(
      <QueryClientProvider client={qc}>
        <AuthProvider>
          <MemoryRouter initialEntries={['/login']}>
            <Routes>
              <Route path="/login" element={<LoginPage />} />
              <Route path="/" element={<div>登录成功首页</div>} />
            </Routes>
          </MemoryRouter>
        </AuthProvider>
      </QueryClientProvider>,
    )

    // 校验服务器信息与公开用户加载展示
    expect(await screen.findByText('Ambra 家庭影院')).toBeInTheDocument()
    expect(await screen.findByRole('button', { name: /Alice/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Bob/i })).toBeInTheDocument()

    // 点击公开用户快速填充用户名
    await user.click(screen.getByRole('button', { name: /Alice/i }))
    const usernameInput = screen.getByPlaceholderText('输入用户名') as HTMLInputElement
    expect(usernameInput.value).toBe('Alice')

    // 输入密码并提交登录表单
    await user.type(screen.getByPlaceholderText('留空表示无密码账号'), 'my-secret')
    await user.click(screen.getByRole('button', { name: /登录 Ambra/i }))

    // 验证提交认证请求并成功跳转首页
    expect(await screen.findByText('登录成功首页')).toBeInTheDocument()
    const authCall = fetchSpy.mock.calls.find(([u]) => u === '/emby/Users/AuthenticateByName')
    expect(authCall).toBeDefined()
    expect(JSON.parse(String(authCall?.[1]?.body))).toEqual({
      Username: 'Alice',
      Pw: 'my-secret',
    })
  })

  it('未输入用户名直接提交时展示表单校验错误', async () => {
    const user = userEvent.setup()
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(jsonRes([]))

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={qc}>
        <AuthProvider>
          <MemoryRouter initialEntries={['/login']}>
            <LoginPage />
          </MemoryRouter>
        </AuthProvider>
      </QueryClientProvider>,
    )

    await user.click(screen.getByRole('button', { name: /登录 Ambra/i }))
    expect(await screen.findByText('请输入用户名')).toBeInTheDocument()
  })
})

describe('App RBAC 路由守卫 - /admin 访问控制', () => {
  beforeEach(() => {
    localStorage.clear()
    window.location.hash = ''
  })

  afterEach(() => {
    vi.restoreAllMocks()
    window.location.hash = ''
  })

  it('普通用户访问 /admin 被 RequireAdmin 拦截并展示无权限提示', async () => {
    saveAuth({
      accessToken: 'tok-normal',
      userId: 'user-normal-1',
      serverId: 'srv-1',
      userName: 'NormalUser',
      isAdmin: false,
      deviceId: 'dev-1',
    })
    window.location.hash = '#/admin'

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/emby/Users/user-normal-1') {
        return jsonRes({
          Id: 'user-normal-1',
          Name: 'NormalUser',
          ServerId: 'srv-1',
          Policy: { IsAdministrator: false },
        })
      }
      if (url.includes('/Views')) {
        return jsonRes({ Items: [], TotalRecordCount: 0 })
      }
      return jsonRes({})
    })

    render(<App />)

    expect(await screen.findByText('无权访问管理控制台')).toBeInTheDocument()
    expect(screen.getByText(/IsAdministrator = false/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '返回首页' })).toBeInTheDocument()
    expect(screen.queryByText('管理控制台 · Admin Console')).not.toBeInTheDocument()
  })

  it('管理员用户访问 /admin 正常放行并渲染管理控制台与仪表盘', async () => {
    saveAuth({
      accessToken: 'tok-admin',
      userId: 'user-admin-99',
      serverId: 'srv-1',
      userName: 'SuperAdmin',
      isAdmin: true,
      deviceId: 'dev-admin',
    })
    window.location.hash = '#/admin'

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/emby/Users/user-admin-99') {
        return jsonRes({
          Id: 'user-admin-99',
          Name: 'SuperAdmin',
          ServerId: 'srv-1',
          Policy: { IsAdministrator: true },
        })
      }
      if (url.includes('/Views')) {
        return jsonRes({ Items: [], TotalRecordCount: 0 })
      }
      if (url === '/emby/System/Info') {
        return jsonRes({
          ServerName: 'Ambra Production',
          ProductName: 'Ambra',
          Version: '2.0.0',
          Id: 'srv-prod-01',
          OperatingSystem: 'Linux',
          SystemArchitecture: 'arm64',
          HttpServerPortNumber: 8096,
        })
      }
      if (url === '/emby/Sessions') {
        return jsonRes([])
      }
      if (url.startsWith('/emby/System/ActivityLog/Entries')) {
        return jsonRes({ Items: [], TotalRecordCount: 0 })
      }
      return jsonRes({})
    })

    render(<App />)

    await waitFor(() => {
      expect(screen.getByText('管理控制台 · Admin Console')).toBeInTheDocument()
    })
    expect(screen.getByText('概览与监控')).toBeInTheDocument()
    expect(screen.getByText('用户与权限')).toBeInTheDocument()
    expect(screen.getByText('媒体库管理')).toBeInTheDocument()
    expect(await screen.findByText('Ambra Production')).toBeInTheDocument()
    expect(screen.queryByText('无权访问管理控制台')).not.toBeInTheDocument()
  })
})
