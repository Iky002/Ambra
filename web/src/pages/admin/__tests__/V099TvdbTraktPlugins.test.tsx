import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api, saveAuth } from '../../../api/client'
import type { PluginInfo } from '../../../api/types'
import { setDemoModeOverride } from '../../../demo/demoMode'
import { resetDemoStore } from '../../../demo/mockServer'
import { PluginsTab } from '../PluginsTab'

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

const MOCK_PLUGINS: PluginInfo[] = [
  {
    Id: 'ambra.builtin.tvdb',
    Name: 'TheTVDB Scraper',
    Version: '1.0.0',
    Description: '默认关闭；启用并填写 TheTVDB v4 API Key 后，将按优先级作为剧集/电影刮削补充源。',
    Author: 'Ambra Team',
    BuiltIn: true,
    Enabled: false,
    Priority: 30,
    Kinds: ['metadata', 'image'],
    Status: 'Disabled',
  },
  {
    Id: 'ambra.builtin.trakt',
    Name: 'Trakt Scrobble & Sync',
    Version: '1.0.0',
    Description: '默认关闭；启用后自动将播放进度与已看状态同步至 Trakt.tv。',
    Author: 'Ambra Team',
    BuiltIn: true,
    Enabled: false,
    Priority: 40,
    Kinds: ['notifier'],
    Status: 'Disabled',
  },
]

describe('v0.9.9 PluginsTab TVDB & Trakt Configuration', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    resetDemoStore()
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
    sessionStorage.clear()
  })

  it('默认渲染 TVDB 与 Trakt 插件为已停用状态，且 Demo 模式包含对应内置插件', async () => {
    setDemoModeOverride(true)
    saveAuth({
      accessToken: 'demo-admin-token',
      userId: 'user-admin',
      serverId: 'srv-demo',
      userName: 'admin',
      isAdmin: true,
      deviceId: 'dev-demo',
    })
    const demoPlugins = await api.getPlugins()
    const tvdb = demoPlugins.find((p) => p.Id === 'ambra.builtin.tvdb')
    const trakt = demoPlugins.find((p) => p.Id === 'ambra.builtin.trakt')

    expect(tvdb).toMatchObject({
      Id: 'ambra.builtin.tvdb',
      Name: 'TheTVDB Scraper',
      Enabled: false,
      Priority: 30,
      BuiltIn: true,
      Kinds: ['metadata', 'image'],
    })
    expect(trakt).toMatchObject({
      Id: 'ambra.builtin.trakt',
      Name: 'Trakt Scrobble & Sync',
      Enabled: false,
      Priority: 40,
      BuiltIn: true,
      Kinds: ['notifier'],
    })

    renderWithQueryClient(<PluginsTab />)

    const tvdbHeading = await screen.findByText('TheTVDB Scraper')
    const tvdbCard = tvdbHeading.closest('div.rounded-2xl') as HTMLElement
    expect(within(tvdbCard).getByText('已停用')).toBeInTheDocument()
    expect(within(tvdbCard).getByText('ambra.builtin.tvdb')).toBeInTheDocument()

    const traktHeading = screen.getByText('Trakt Scrobble & Sync')
    const traktCard = traktHeading.closest('div.rounded-2xl') as HTMLElement
    expect(within(traktCard).getByText('已停用')).toBeInTheDocument()
    expect(within(traktCard).getByText('ambra.builtin.trakt')).toBeInTheDocument()
  })

  it('打开 TVDB 配置弹窗并保存 enabled、api_key、pin、language，同时剥离 extraJson 中的 pin 字段', async () => {
    vi.spyOn(api, 'getPlugins').mockResolvedValue(MOCK_PLUGINS)
    vi.spyOn(api, 'getPluginConfiguration').mockResolvedValue({
      enabled: false,
      priority: 30,
      api_key: 'old-tvdb-key',
      pin: 'old-pin',
      Pin: 'old-pin-pascal',
      tvdb_pin: 'old-tvdb-pin',
      language: 'en-US',
      custom_flag: true,
    })
    const updateSpy = vi.spyOn(api, 'updatePluginConfiguration').mockResolvedValue(undefined)

    renderWithQueryClient(<PluginsTab />)

    const tvdbHeading = await screen.findByText('TheTVDB Scraper')
    const tvdbCard = tvdbHeading.closest('div.rounded-2xl') as HTMLElement
    fireEvent.click(within(tvdbCard).getByRole('button', { name: '编辑配置' }))

    expect(
      await screen.findByText(
        '默认关闭；启用并填写 TheTVDB v4 API Key 后，将按优先级作为剧集/电影刮削补充源',
      ),
    ).toBeInTheDocument()

    // 验证 extraJson 已剥离 pin / Pin / tvdb_pin，但保留 custom_flag
    const extraTextarea = screen.getByRole('textbox', { name: '' }) || document.querySelector('textarea')
    expect(extraTextarea).toBeTruthy()
    const textareaEl = document.querySelector('textarea') as HTMLTextAreaElement
    expect(textareaEl.value).toContain('"custom_flag": true')
    expect(textareaEl.value).not.toContain('pin')
    expect(textareaEl.value).not.toContain('Pin')

    // 验证语言下拉包含 10 种语言
    const langSelect = screen.getByLabelText('刮削语言') as HTMLSelectElement
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

    // 启用插件并填写 TVDB 配置
    const enableSwitch = screen.getByRole('switch', { name: /启用插件/ })
    expect(enableSwitch).toHaveAttribute('aria-checked', 'false')
    fireEvent.click(enableSwitch)

    fireEvent.change(screen.getByLabelText('API Key'), {
      target: { value: 'tvdb-v4-live-key' },
    })
    fireEvent.change(screen.getByLabelText('Subscriber PIN（可选）'), {
      target: { value: 'SUB-PIN-999' },
    })
    fireEvent.change(langSelect, {
      target: { value: 'ja-JP' },
    })

    fireEvent.click(screen.getByRole('button', { name: '保存配置' }))

    await waitFor(() => {
      expect(updateSpy).toHaveBeenCalledWith(
        'ambra.builtin.tvdb',
        expect.objectContaining({
          enabled: true,
          Enabled: true,
          priority: 30,
          api_key: 'tvdb-v4-live-key',
          pin: 'SUB-PIN-999',
          language: 'ja-JP',
          custom_flag: true,
        }),
      )
    })
  })

  it('Trakt 管理员配置只保存可用性，不再包含账号凭据或个人同步偏好', async () => {
    vi.spyOn(api, 'getPlugins').mockResolvedValue(MOCK_PLUGINS)
    vi.spyOn(api, 'getPluginConfiguration').mockResolvedValue({
      enabled: false, priority: 40, client_id: 'legacy-client',
      access_token: 'legacy-secret', sync_watched: true,
    })
    const updateSpy = vi.spyOn(api, 'updatePluginConfiguration').mockResolvedValue(undefined)
    renderWithQueryClient(<PluginsTab />)
    const heading = await screen.findByText('Trakt Scrobble & Sync')
    fireEvent.click(within(heading.closest('div.rounded-2xl') as HTMLElement)
      .getByRole('button', { name: '编辑配置' }))
    expect(await screen.findByText(/管理员控制是否向用户开放 Trakt/)).toBeInTheDocument()
    expect(screen.queryByLabelText('Client ID')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Access Token')).not.toBeInTheDocument()
    expect(document.querySelector('textarea')).toBeNull()
    fireEvent.click(screen.getByRole('switch', { name: /启用插件/ }))
    fireEvent.click(screen.getByRole('button', { name: '保存配置' }))
    await waitFor(() => expect(updateSpy).toHaveBeenCalledWith('ambra.builtin.trakt', {
      enabled: true, Enabled: true, priority: 40, Priority: 40,
    }))
  })
})
