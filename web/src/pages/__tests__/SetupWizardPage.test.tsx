import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { getStoredAuth, saveAuth } from '../../api/client'
import { AuthProvider } from '../../context/AuthContext'
import { LoginPage } from '../LoginPage'
import { SetupWizardPage } from '../SetupWizardPage'

function jsonRes(data: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    headers: new Headers({ 'content-type': 'application/json' }),
    json: async () => data,
    text: async () => JSON.stringify(data),
  } as Response
}

function renderWithRouter(initialPath: string) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={qc}>
      <AuthProvider>
        <MemoryRouter initialEntries={[initialPath]}>
          <Routes>
            <Route path="/setup" element={<SetupWizardPage />} />
            <Route path="/login" element={<LoginPage />} />
            <Route path="/" element={<div>Ambra 影院首页</div>} />
          </Routes>
        </MemoryRouter>
      </AuthProvider>
    </QueryClientProvider>,
  )
}

describe('SetupWizardPage - 首次运行 4 步初始化向导', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('完整走通 4 步初始化流程：服务器设置 -> 管理员校验与创建 -> 浏览目录并添加媒体库 -> 刮削配置并跳转首页', async () => {
    const user = userEvent.setup()
    let setupCompleted = false

    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      const method = (init?.method || 'GET').toUpperCase()

      if (url === '/api/setup/status' && method === 'GET') {
        return jsonRes({
          completed: setupCompleted,
          server_name: 'Ambra',
          default_language: 'zh-CN',
          has_users: false,
          has_tmdb_key: false,
        })
      }

      if (url === '/api/setup/admin' && method === 'POST') {
        return jsonRes({
          AccessToken: 'tok-setup-admin-01',
          ServerId: 'srv-new-01',
          User: {
            Id: 'u-admin-01',
            Name: 'cinema_admin',
            ServerId: 'srv-new-01',
            Policy: { IsAdministrator: true },
          },
        })
      }

      if (url === '/emby/Users/u-admin-01' && method === 'GET') {
        return jsonRes({
          Id: 'u-admin-01',
          Name: 'cinema_admin',
          ServerId: 'srv-new-01',
          Policy: { IsAdministrator: true },
        })
      }

      if (url === '/emby/Environment/Drives' && method === 'GET') {
        return jsonRes([{ Name: '/media', Path: '/media', Type: 'Directory' }])
      }

      if (url.startsWith('/emby/Environment/DirectoryContents') && method === 'GET') {
        return jsonRes([
          { Name: 'movies', Path: '/media/movies', Type: 'Directory' },
          { Name: 'tvshows', Path: '/media/tvshows', Type: 'Directory' },
        ])
      }

      if (url.startsWith('/emby/Library/VirtualFolders') && method === 'POST') {
        return jsonRes({
          ItemId: 'lib-movies-01',
          Name: '4K 电影',
          CollectionType: 'movies',
          Locations: ['/media/movies'],
        })
      }

      if (url === '/api/setup/complete' && method === 'POST') {
        setupCompleted = true
        return jsonRes({ completed: true })
      }

      return jsonRes({})
    })

    renderWithRouter('/setup')

    // ==================== Step 1 ====================
    expect(await screen.findByText('第 1 步 · 服务器基础设置')).toBeInTheDocument()
    const serverNameInput = screen.getByLabelText('服务器名称') as HTMLInputElement
    await user.clear(serverNameInput)
    await user.type(serverNameInput, '我的私人影院')
    await user.selectOptions(screen.getByLabelText('首选刮削语言'), 'zh-TW')
    await user.selectOptions(screen.getByLabelText('国家 / 地区'), 'TW')
    await user.click(screen.getByRole('button', { name: /下一步：创建管理员/i }))

    // ==================== Step 2 ====================
    expect(await screen.findByText('第 2 步 · 创建管理员账号')).toBeInTheDocument()
    const usernameInput = screen.getByLabelText('管理员用户名')
    const passwordInput = screen.getByLabelText(/管理员密码/)
    const confirmInput = screen.getByLabelText('确认密码')

    await user.clear(usernameInput)
    await user.type(usernameInput, 'cinema_admin')

    // 密码长度不足 6 位校验
    await user.type(passwordInput, '12345')
    await user.type(confirmInput, '12345')
    await user.click(screen.getByRole('button', { name: /创建并继续/i }))
    expect(await screen.findByText('管理员密码长度至少为 6 位')).toBeInTheDocument()

    // 两次密码不一致校验
    await user.clear(passwordInput)
    await user.clear(confirmInput)
    await user.type(passwordInput, 'secret123')
    await user.type(confirmInput, 'different123')
    await user.click(screen.getByRole('button', { name: /创建并继续/i }))
    expect(await screen.findByText('两次输入的密码不一致，请重新确认')).toBeInTheDocument()

    // 正确提交创建管理员
    await user.clear(confirmInput)
    await user.type(confirmInput, 'secret123')
    await user.click(screen.getByRole('button', { name: /创建并继续/i }))

    // ==================== Step 3 ====================
    expect(await screen.findByText('第 3 步 · 添加媒体库')).toBeInTheDocument()

    // 校验 setupAdmin 请求与会话写入
    const setupAdminCall = fetchSpy.mock.calls.find(([u]) => u === '/api/setup/admin')
    expect(setupAdminCall).toBeDefined()
    expect(JSON.parse(String(setupAdminCall?.[1]?.body))).toEqual({
      server_name: '我的私人影院',
      language: 'zh-TW',
      country: 'TW',
      username: 'cinema_admin',
      password: 'secret123',
    })
    expect(getStoredAuth()?.accessToken).toBe('tok-setup-admin-01')
    expect(getStoredAuth()?.isAdmin).toBe(true)

    // 使用目录浏览器选择 /media/movies
    await user.type(screen.getByLabelText('媒体库名称'), '4K 电影')
    await user.click(screen.getByRole('button', { name: /浏览目录/i }))
    expect(await screen.findByText('/media')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /\/media/i }))
    expect(await screen.findByText('movies')).toBeInTheDocument()
    const selectButtons = screen.getAllByRole('button', { name: '选中' })
    await user.click(selectButtons[0])

    expect((screen.getByLabelText('服务器目录路径') as HTMLInputElement).value).toBe('/media/movies')

    // 验证 Step 3 本地元数据保存开关默认开启并可透传至 createVirtualFolder
    expect(screen.getByText('保存元数据与图片到媒体文件夹')).toBeInTheDocument()

    // 添加媒体库
    await user.click(screen.getByRole('button', { name: /添加此媒体库/i }))
    expect(await screen.findByText('已添加媒体库「4K 电影」')).toBeInTheDocument()
    expect(screen.getByText('已添加的媒体库（1）')).toBeInTheDocument()

    const createLibCall = fetchSpy.mock.calls.find(
      ([u, init]) =>
        String(u).startsWith('/emby/Library/VirtualFolders') &&
        (init?.method || 'GET').toUpperCase() === 'POST',
    )
    expect(createLibCall).toBeDefined()
    expect(JSON.parse(String(createLibCall?.[1]?.body)).LibraryOptions.SaveLocalMetadata).toBe(true)

    // 进入第 4 步
    await user.click(screen.getByRole('button', { name: /下一步：刮削设置/i }))

    // ==================== Step 4 ====================
    expect(await screen.findByText('第 4 步 · 刮削与偏好设置')).toBeInTheDocument()
    expect((screen.getByLabelText('刮削语言确认') as HTMLSelectElement).value).toBe('zh-TW')

    await user.type(screen.getByLabelText(/TMDB API Key/), 'tmdb-key-demo-888')
    await user.click(screen.getByRole('button', { name: /完成初始化并进入 Ambra/i }))

    // 校验 completeSetup 请求并跳转首页
    expect(await screen.findByText('Ambra 影院首页')).toBeInTheDocument()
    const completeCall = fetchSpy.mock.calls.find(([u]) => u === '/api/setup/complete')
    expect(completeCall).toBeDefined()
    expect(JSON.parse(String(completeCall?.[1]?.body))).toEqual({
      server_name: '我的私人影院',
      tmdb_api_key: 'tmdb-key-demo-888',
      tmdb_language: 'zh-TW',
      enable_strm_302: true,
      trigger_scan: true,
      sync_rankings: true,
    })
  })

  it('当 setupStatus.completed === false 时，访问 LoginPage 自动重定向至 /setup', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/api/setup/status') {
        return jsonRes({
          completed: false,
          server_name: 'Ambra',
          default_language: 'zh-CN',
          has_users: false,
          has_tmdb_key: false,
        })
      }
      return jsonRes([])
    })

    renderWithRouter('/login')

    expect(await screen.findByText('第 1 步 · 服务器基础设置')).toBeInTheDocument()
  })

  it('当 setupStatus.completed === true 且已登录时，访问 /setup 自动重定向至首页', async () => {
    saveAuth({
      accessToken: 'tok-existing',
      userId: 'u-1',
      serverId: 'srv-1',
      userName: 'admin',
      isAdmin: true,
      deviceId: 'dev-1',
    })

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/api/setup/status') {
        return jsonRes({
          completed: true,
          server_name: 'Ambra',
          default_language: 'zh-CN',
          has_users: true,
          has_tmdb_key: true,
        })
      }
      if (url === '/emby/Users/u-1') {
        return jsonRes({
          Id: 'u-1',
          Name: 'admin',
          ServerId: 'srv-1',
          Policy: { IsAdministrator: true },
        })
      }
      return jsonRes({})
    })

    renderWithRouter('/setup')

    await waitFor(() => {
      expect(screen.getByText('Ambra 影院首页')).toBeInTheDocument()
    })
  })

  it('[3.2-7] 提供全部 10 种刮削语言选项，且已有管理员点击「已登录，直接下一步」跳过 Step 2 时持久化语言与国家设置', async () => {
    const user = userEvent.setup()
    saveAuth({
      accessToken: 'tok-existing-admin',
      userId: 'u-admin-99',
      serverId: 'srv-1',
      userName: 'admin',
      isAdmin: true,
      deviceId: 'dev-1',
    })

    let setupCompleted = false
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      const method = (init?.method || 'GET').toUpperCase()
      if (url === '/api/setup/status' && method === 'GET') {
        return jsonRes({
          completed: setupCompleted,
          server_name: 'Ambra',
          default_language: 'zh-CN',
          has_users: true,
          has_tmdb_key: false,
        })
      }
      if (url === '/emby/Users/u-admin-99' && method === 'GET') {
        return jsonRes({
          Id: 'u-admin-99',
          Name: 'admin',
          ServerId: 'srv-1',
          Policy: { IsAdministrator: true },
        })
      }
      if (url === '/emby/System/Configuration' && method === 'POST') {
        return jsonRes({})
      }
      if (url === '/api/setup/complete' && method === 'POST') {
        setupCompleted = true
        return jsonRes({ completed: true })
      }
      return jsonRes([])
    })

    renderWithRouter('/setup')
    expect(await screen.findByText('第 1 步 · 服务器基础设置')).toBeInTheDocument()

    const langSelect = screen.getByLabelText('首选刮削语言') as HTMLSelectElement
    const optionValues = Array.from(langSelect.options).map((o) => o.value)
    expect(optionValues).toEqual([
      'zh-CN',
      'zh-TW',
      'en-US',
      'ja-JP',
      'ko-KR',
      'es-ES',
      'de-DE',
      'fr-FR',
      'pt-BR',
      'th-TH',
    ])

    await user.selectOptions(langSelect, 'ko-KR')
    await user.selectOptions(screen.getByLabelText('国家 / 地区'), 'KR')
    await user.click(screen.getByRole('button', { name: /下一步：创建管理员/i }))

    expect(await screen.findByText('第 2 步 · 创建管理员账号')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /已登录，直接下一步/i }))

    expect(await screen.findByText('第 3 步 · 添加媒体库')).toBeInTheDocument()
    const cfgCalls = fetchSpy.mock.calls.filter(
      ([u, init]) =>
        String(u) === '/emby/System/Configuration' &&
        (init?.method || 'GET').toUpperCase() === 'POST',
    )
    expect(cfgCalls.length).toBeGreaterThanOrEqual(1)
    expect(JSON.parse(String(cfgCalls[0]?.[1]?.body))).toMatchObject({
      PreferredMetadataLanguage: 'ko-KR',
      MetadataCountryCode: 'KR',
      TMDBLanguage: 'ko-KR',
    })

    await user.click(screen.getByRole('button', { name: /暂时跳过|下一步：刮削设置/i }))
    await user.click(screen.getByRole('button', { name: /完成初始化并进入 Ambra/i }))

    expect(await screen.findByText('Ambra 影院首页')).toBeInTheDocument()
    const completeCall = fetchSpy.mock.calls.find(([u]) => u === '/api/setup/complete')
    expect(JSON.parse(String(completeCall?.[1]?.body))).toMatchObject({
      language: 'ko-KR',
      country: 'KR',
      tmdb_language: 'ko-KR',
    })
  })
})
