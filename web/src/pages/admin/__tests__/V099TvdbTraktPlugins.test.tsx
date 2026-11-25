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

  it('打开 Trakt 配置弹窗并保存 enabled、client_id、access_token、scrobble_playback、sync_watched，同时剥离 extraJson 中的 Trakt 字段', async () => {
    vi.spyOn(api, 'getPlugins').mockResolvedValue(MOCK_PLUGINS)
    vi.spyOn(api, 'getPluginConfiguration').mockResolvedValue({
      enabled: false,
      priority: 40,
      client_id: 'old-client-id',
      ClientId: 'old-client-id-pascal',
      access_token: 'old-secret-token',
      AccessToken: 'old-secret-token-pascal',
      scrobble_playback: true,
      ScrobblePlayback: true,
      sync_watched: true,
      SyncWatched: true,
      trakt_extra: 'keep-me',
    })
    const updateSpy = vi.spyOn(api, 'updatePluginConfiguration').mockResolvedValue(undefined)

    renderWithQueryClient(<PluginsTab />)

    const traktHeading = await screen.findByText('Trakt Scrobble & Sync')
    const traktCard = traktHeading.closest('div.rounded-2xl') as HTMLElement
    fireEvent.click(within(traktCard).getByRole('button', { name: '编辑配置' }))

    expect(
      await screen.findByText('默认关闭；启用后自动将播放进度与已看状态同步至 Trakt.tv'),
    ).toBeInTheDocument()

    // 验证 Access Token 为密码输入框
    const accessTokenInput = screen.getByLabelText('Access Token') as HTMLInputElement
    expect(accessTokenInput.type).toBe('password')

    // 验证 extraJson 已剥离 Trakt 专属字段，但保留 trakt_extra
    const textareaEl = document.querySelector('textarea') as HTMLTextAreaElement
    expect(textareaEl.value).toContain('"trakt_extra": "keep-me"')
    expect(textareaEl.value).not.toContain('client_id')
    expect(textareaEl.value).not.toContain('ClientId')
    expect(textareaEl.value).not.toContain('access_token')
    expect(textareaEl.value).not.toContain('AccessToken')
    expect(textareaEl.value).not.toContain('scrobble_playback')
    expect(textareaEl.value).not.toContain('ScrobblePlayback')
    expect(textareaEl.value).not.toContain('sync_watched')
    expect(textareaEl.value).not.toContain('SyncWatched')

    // 验证两个同步开关默认开启
    const scrobbleSwitch = screen.getByRole('switch', { name: /实时同步播放进度 \(Scrobble\)/ })
    const watchedSwitch = screen.getByRole('switch', { name: /同步已观看历史 \(Watched Sync\)/ })
    expect(scrobbleSwitch).toHaveAttribute('aria-checked', 'true')
    expect(watchedSwitch).toHaveAttribute('aria-checked', 'true')

    // 启用插件、修改字段并关闭 sync_watched
    fireEvent.click(screen.getByRole('switch', { name: /启用插件/ }))
    fireEvent.change(screen.getByLabelText('Client ID'), {
      target: { value: 'trakt-client-abc' },
    })
    fireEvent.change(accessTokenInput, {
      target: { value: 'trakt-token-xyz' },
    })
    fireEvent.click(watchedSwitch)

    fireEvent.click(screen.getByRole('button', { name: '保存配置' }))

    await waitFor(() => {
      expect(updateSpy).toHaveBeenCalledWith(
        'ambra.builtin.trakt',
        expect.objectContaining({
          enabled: true,
          Enabled: true,
          priority: 40,
          client_id: 'trakt-client-abc',
          access_token: 'trakt-token-xyz',
          scrobble_playback: true,
          sync_watched: false,
          trakt_extra: 'keep-me',
        }),
      )
    })
  })
})
