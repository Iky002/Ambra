import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api, saveAuth } from '../../../api/client'
import type { ScanProgressSnapshot } from '../../../api/types'
import { ScanProgressBar } from '../../../components/ScanProgressBar'
import { setDemoModeOverride } from '../../../demo/demoMode'
import { PluginsTab } from '../PluginsTab'
import { SettingsTab } from '../SettingsTab'

function renderWithQueryClient(ui: React.ReactElement) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
    },
  })
  return {
    queryClient,
    ...render(<QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>),
  }
}

describe('Audit Batch 2C — Admin Settings, Plugins, Image URL & Scan Controls', () => {
  beforeEach(() => {
    localStorage.clear()
    setDemoModeOverride(false)
    saveAuth({
      accessToken: 'test-admin-token',
      userId: 'user-admin',
      serverId: 'srv-1',
      userName: 'admin',
      isAdmin: true,
      deviceId: 'dev-test',
    })
  })

  afterEach(() => {
    vi.restoreAllMocks()
    setDemoModeOverride(null)
    localStorage.clear()
  })

  it('SettingsTab 加载服务器配置并在保存时提交更新并失效 systemInfo 缓存', async () => {
    vi.spyOn(api, 'getServerConfig').mockResolvedValue({
      ServerName: 'Ambra NAS',
      TMDBApiKey: 'old-key',
      TMDBLanguage: 'zh-CN',
      EnableSTRM302: true,
      PlayedThreshold: 0.9,
    })
    const updateSpy = vi.spyOn(api, 'updateServerConfig').mockResolvedValue(undefined)

    const { queryClient } = renderWithQueryClient(<SettingsTab />)
    const invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries')

    const nameInput = (await screen.findByLabelText(/服务器名称/)) as HTMLInputElement
    expect(nameInput.value).toBe('Ambra NAS')
    expect(screen.getByTestId('played-threshold-value')).toHaveTextContent('90%')

    fireEvent.change(nameInput, { target: { value: 'Ambra 客厅影院' } })
    fireEvent.change(screen.getByLabelText('TMDB API Key'), {
      target: { value: 'new-tmdb-key' },
    })
    fireEvent.change(screen.getByLabelText('首选刮削语言'), {
      target: { value: 'ja-JP' },
    })
    fireEvent.change(screen.getByLabelText('标记为已看进度阈值'), {
      target: { value: '85' },
    })
    fireEvent.click(screen.getByRole('switch', { name: /STRM 302/ }))

    fireEvent.click(screen.getByRole('button', { name: /保存设置/ }))

    await waitFor(() => {
      expect(updateSpy).toHaveBeenCalledWith(
        expect.objectContaining({
          ServerName: 'Ambra 客厅影院',
          TMDBApiKey: 'new-tmdb-key',
          TMDBLanguage: 'ja-JP',
          EnableSTRM302: false,
          PlayedThreshold: 0.85,
        }),
      )
    })

    expect(await screen.findByText('已保存服务器常规设置')).toBeInTheDocument()
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['publicSystemInfo'] })
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['admin', 'systemInfo'] })
  })

  it('PluginsTab 优先读取小写实时字段 enabled/priority 并在保存时同步写入大小写字段', async () => {
    vi.spyOn(api, 'getPlugins').mockResolvedValue([
      {
        Id: 'tmdb-meta',
        Name: 'TMDB 刮削器',
        Enabled: true,
        Priority: 10,
        Kinds: ['metadata'],
      },
    ])
    // 模拟后端同时返回旧 PascalCase 和实时小写字段，应以小写为准
    vi.spyOn(api, 'getPluginConfiguration').mockResolvedValue({
      Enabled: true,
      enabled: false,
      Priority: 10,
      priority: 25,
      api_key: 'live-key',
      language: 'zh-TW',
    })
    const updatePluginSpy = vi.spyOn(api, 'updatePluginConfiguration').mockResolvedValue(undefined)

    renderWithQueryClient(<PluginsTab />)

    const configBtn = await screen.findByRole('button', { name: /配置/ })
    fireEvent.click(configBtn)

    await waitFor(() => {
      const toggle = screen.getByRole('switch')
      expect(toggle).toHaveAttribute('aria-checked', 'false')
    })

    // 切换启用为 true 并保存
    fireEvent.click(screen.getByRole('switch'))
    fireEvent.click(screen.getByRole('button', { name: /保存配置/ }))

    await waitFor(() => {
      expect(updatePluginSpy).toHaveBeenCalledWith(
        'tmdb-meta',
        expect.objectContaining({
          enabled: true,
          Enabled: true,
          priority: 25,
          Priority: 25,
        }),
      )
    })
  })

  it('api.setItemImageByUrl 使用 /emby/Items/{id}/Images/{type}/0/Url 路径', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true,
      status: 204,
      headers: new Headers(),
      text: async () => '',
    } as Response)

    await api.setItemImageByUrl('item-42', 'Primary', 'https://image.tmdb.org/t/p/w500/poster.jpg')

    expect(fetchSpy).toHaveBeenCalledTimes(1)
    const [url, init] = fetchSpy.mock.calls[0]
    expect(url).toBe('/emby/Items/item-42/Images/Primary/0/Url')
    expect(init?.method).toBe('POST')
    expect(init?.body).toBe(JSON.stringify({ Url: 'https://image.tmdb.org/t/p/w500/poster.jpg' }))
  })

  it('ScanProgressBar (card) 在扫描运行时展示暂停与取消扫描按钮并调用对应接口', async () => {
    const runningSnapshot: ScanProgressSnapshot = {
      running: true,
      paused: false,
      phase: 'enriching',
      library_name: '4K 电影',
      total: 20,
      processed: 8,
      added: 3,
      updated: 5,
      skipped: 0,
      failed: 0,
      current_item: '沙丘 2',
      current_action: '刮削元数据',
      percentage: 40,
      updated_at: new Date().toISOString(),
    }
    vi.spyOn(api, 'getScanProgress').mockResolvedValue(runningSnapshot)
    const pauseSpy = vi.spyOn(api, 'pauseScan').mockResolvedValue(undefined)
    const cancelSpy = vi.spyOn(api, 'cancelScan').mockResolvedValue(undefined)

    renderWithQueryClient(<ScanProgressBar variant="card" />)

    const pauseBtn = await screen.findByRole('button', { name: /暂停/ })
    const cancelBtn = screen.getByRole('button', { name: /取消扫描/ })
    expect(pauseBtn).toBeInTheDocument()
    expect(cancelBtn).toBeInTheDocument()

    fireEvent.click(pauseBtn)
    await waitFor(() => {
      expect(pauseSpy).toHaveBeenCalledTimes(1)
    })

    fireEvent.click(cancelBtn)
    await waitFor(() => {
      expect(cancelSpy).toHaveBeenCalledTimes(1)
    })
  })
})
