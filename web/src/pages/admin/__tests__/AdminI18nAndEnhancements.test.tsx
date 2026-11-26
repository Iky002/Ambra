import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import * as clientModule from '../../../api/client'
import { api, saveAuth } from '../../../api/client'
import { AuthProvider } from '../../../context/AuthContext'
import { setDemoModeOverride } from '../../../demo/demoMode'
import { I18nProvider } from '../../../i18n/I18nContext'
import type { LocaleCode } from '../../../i18n/types'
import { AdminPage } from '../../AdminPage'
import { DashboardTab } from '../DashboardTab'
import { DevicesKeysTab } from '../DevicesKeysTab'
import { MigrationTab } from '../MigrationTab'
import { PluginsTab } from '../PluginsTab'
import { SettingsTab } from '../SettingsTab'
import { TasksLogsTab } from '../TasksLogsTab'
import { UsersTab } from '../UsersTab'
import { WebhooksTab } from '../WebhooksTab'

function renderAdminUi(
  ui: React.ReactElement,
  options?: { locale?: LocaleCode; initialEntries?: string[] },
) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
    },
  })
  const locale = options?.locale ?? 'zh-CN'
  const entries = options?.initialEntries ?? ['/admin/settings']
  return {
    queryClient,
    ...render(
      <I18nProvider initialLocale={locale}>
        <AuthProvider>
          <QueryClientProvider client={queryClient}>
            <MemoryRouter initialEntries={entries}>{ui}</MemoryRouter>
          </QueryClientProvider>
        </AuthProvider>
      </I18nProvider>,
    ),
  }
}

describe('Admin Console Enhancements, Bug Fixes & 10-Locale i18n', () => {
  beforeEach(() => {
    localStorage.clear()
    setDemoModeOverride(false)
    saveAuth({
      accessToken: 'admin-token-123',
      userId: 'user-admin',
      serverId: 'srv-1',
      userName: 'admin',
      isAdmin: true,
      deviceId: 'dev-admin',
    })
  })

  afterEach(() => {
    vi.restoreAllMocks()
    setDemoModeOverride(null)
    localStorage.clear()
  })

  it('SettingsTab: 保护已脱敏 TMDBApiKey 不被覆盖、校验 PlayedThreshold 范围，并支持网络与转码卡片保存', async () => {
    vi.spyOn(api, 'getServerConfig').mockResolvedValue({
      ServerName: 'Ambra Home',
      TMDBApiKey: '********',
      HasTMDBApiKey: true,
      TMDBLanguage: 'zh-CN',
      EnableSTRM302: true,
      PlayedThreshold: 0.9,
      EnableRemoteAccess: true,
      EnableQuickConnect: true,
      EnableDLNA: false,
      DLNADefaultUserID: '',
      LocalNetworkSubnets: ['192.168.0.0/16'],
      TrustedProxies: ['127.0.0.1'],
      AllowedOrigins: [],
      SimultaneousStreamLimit: 4,
      RemoteClientBitrateLimit: 20000000,
    })
    vi.spyOn(api, 'getUsers').mockResolvedValue([
      { Id: 'u-guest', Name: 'guest', ServerId: 'srv-1', Policy: { IsAdministrator: false } },
    ])
    const updateConfigSpy = vi.spyOn(api, 'updateServerConfig').mockResolvedValue(undefined)
    const embyFetchSpy = vi
      .spyOn(clientModule, 'embyFetch')
      .mockImplementation(async (path: string, init?: RequestInit) => {
        if (path === '/emby/Encoding/FfmpegOptions' && (!init?.method || init.method === 'GET')) {
          return {
            HardwareAccelerationMode: 1,
            EnableHardwareEncoding: false,
            AllowHevcEncoding: false,
            H264Crf: 23,
            H264Preset: 'veryfast',
            EnableThrottling: true,
            ThrottleBufferSize: 120,
            TranscodingTempPath: '',
          }
        }
        if (
          path === '/emby/Encoding/FullToneMapOptions' &&
          (!init?.method || init.method === 'GET')
        ) {
          return {
            EnableToneMapping: false,
            ToneMappingAlgorithm: 'bt2390',
          }
        }
        return undefined
      })

    renderAdminUi(<SettingsTab />)

    const tmdbInput = (await screen.findByLabelText('TMDB API Key')) as HTMLInputElement
    expect(tmdbInput.value).toBe('')
    expect(tmdbInput.placeholder).toContain('已配置密钥')

    // 1. PlayedThreshold 越界拦截（40% < 50%）
    const thresholdNumberInput = screen.getByLabelText('已看进度百分比数值')
    fireEvent.change(thresholdNumberInput, { target: { value: '40' } })
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))
    expect(await screen.findByText('标记为已看进度阈值必须在 50% 到 99% 之间')).toBeInTheDocument()
    expect(updateConfigSpy).not.toHaveBeenCalled()

    // 2. 修正阈值并修改服务器名与网络参数，留空 TMDB 密钥保存 -> 发送 "********" 保持原密钥
    fireEvent.change(thresholdNumberInput, { target: { value: '92' } })
    fireEvent.change(screen.getByLabelText(/服务器名称/), { target: { value: 'Ambra 影院 Pro' } })
    fireEvent.click(screen.getByRole('switch', { name: /DLNA/ }))
    fireEvent.change(screen.getByLabelText('DLNA 默认权限账号'), { target: { value: 'u-guest' } })
    fireEvent.change(screen.getByLabelText('本地局域网子网 CIDR'), {
      target: { value: '192.168.0.0/16, 10.0.0.0/8' },
    })
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))

    await waitFor(() => {
      expect(updateConfigSpy).toHaveBeenCalledWith(
        expect.objectContaining({
          ServerName: 'Ambra 影院 Pro',
          TMDBApiKey: '********',
          PlayedThreshold: 0.92,
          EnableDLNA: true,
          DLNADefaultUserID: 'u-guest',
          LocalNetworkSubnets: ['192.168.0.0/16', '10.0.0.0/8'],
        }),
      )
    })

    // 3. 转码与硬件加速卡片保存
    const encodingCard = screen.getByTestId('settings-encoding-card')
    fireEvent.change(within(encodingCard).getByLabelText('硬件加速模式'), {
      target: { value: '5' },
    })
    fireEvent.click(within(encodingCard).getByRole('switch', { name: /HDR 色调映射/ }))
    fireEvent.change(within(encodingCard).getByLabelText('HDR 色调映射算法'), {
      target: { value: 'mobius' },
    })
    fireEvent.change(within(encodingCard).getByLabelText('自定义转码临时目录'), {
      target: { value: '/dev/shm/transcode' },
    })
    fireEvent.click(within(encodingCard).getByRole('button', { name: '保存转码设置' }))

    await waitFor(() => {
      expect(embyFetchSpy).toHaveBeenCalledWith(
        '/emby/Encoding/FfmpegOptions',
        expect.objectContaining({
          method: 'POST',
          body: expect.stringContaining('"/dev/shm/transcode"'),
        }),
      )
      expect(embyFetchSpy).toHaveBeenCalledWith(
        '/emby/Encoding/FullToneMapOptions',
        expect.objectContaining({
          method: 'POST',
          body: JSON.stringify({
            EnableToneMapping: true,
            ToneMappingAlgorithm: 'mobius',
          }),
        }),
      )
    })
  })

  it('UsersTab: 支持编辑 BlockUnratedItems 与 AllowedTags 并在卡片摘要展示', async () => {
    vi.spyOn(api, 'getUsers').mockResolvedValue([
      {
        Id: 'user-kid',
        Name: 'kids',
        ServerId: 'srv-1',
        Policy: {
          IsAdministrator: false,
          EnableAllFolders: true,
          EnabledFolders: [],
          BlockUnratedItems: ['Movie'],
          AllowedTags: ['动画'],
        },
      },
    ])
    vi.spyOn(api, 'getVirtualFolders').mockResolvedValue([])
    vi.spyOn(api, 'getParentalRatings').mockResolvedValue([{ Name: 'PG', Value: 5 }])
    const updatePolicySpy = vi.spyOn(api, 'updateUserPolicy').mockResolvedValue(undefined)

    renderAdminUi(<UsersTab />)

    expect(await screen.findByText('拦截未分级 1 类')).toBeInTheDocument()
    expect(screen.getByText('白名单 1 个标签')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: /权限策略/ }))

    const seriesCheckbox = await screen.findByLabelText('剧集 (Series)')
    fireEvent.click(seriesCheckbox)
    const allowedInput = screen.getByLabelText(/白名单允许标签/)
    fireEvent.change(allowedInput, { target: { value: '动画, 家庭, 科普' } })

    fireEvent.click(screen.getByRole('button', { name: '保存策略' }))

    await waitFor(() => {
      expect(updatePolicySpy).toHaveBeenCalledWith(
        'user-kid',
        expect.objectContaining({
          BlockUnratedItems: expect.arrayContaining(['Movie', 'Series']),
          AllowedTags: ['动画', '家庭', '科普'],
        }),
      )
    })
  })

  it('PluginsTab & DevicesKeysTab: 支持外部插件卸载与优先级编辑、API Key 一次性横幅与脱敏隐藏切换', async () => {
    vi.spyOn(api, 'getPlugins').mockResolvedValue([
      {
        Id: 'ambra.builtin.tmdb',
        Name: 'TMDB 刮削器',
        BuiltIn: true,
        Enabled: true,
        Priority: 10,
        Kinds: ['metadata'],
      },
      {
        Id: 'ext.subfinder',
        Name: 'SubFinder 外部字幕',
        BuiltIn: false,
        Enabled: true,
        Priority: 30,
        Kinds: ['subtitle'],
      },
    ])
    vi.spyOn(api, 'getPluginConfiguration').mockResolvedValue({ Enabled: true, Priority: 30 })
    const updatePluginSpy = vi.spyOn(api, 'updatePluginConfiguration').mockResolvedValue(undefined)
    const embyFetchSpy = vi.spyOn(clientModule, 'embyFetch').mockResolvedValue(undefined)

    const { unmount } = renderAdminUi(<PluginsTab />)

    // 仅外部插件展示卸载按钮
    const uninstallBtns = await screen.findAllByRole('button', { name: '卸载插件' })
    expect(uninstallBtns).toHaveLength(1)

    // 修改优先级保存
    const configBtns = screen.getAllByRole('button', { name: '编辑配置' })
    fireEvent.click(configBtns[1])
    const prioInput = await screen.findByLabelText(/执行优先级/)
    fireEvent.change(prioInput, { target: { value: '5' } })
    fireEvent.click(screen.getByRole('button', { name: '保存配置' }))

    await waitFor(() => {
      expect(updatePluginSpy).toHaveBeenCalledWith(
        'ext.subfinder',
        expect.objectContaining({ priority: 5, Priority: 5 }),
      )
    })

    // 卸载外部插件二次确认
    fireEvent.click(screen.getByRole('button', { name: '卸载插件' }))
    fireEvent.click(await screen.findByRole('button', { name: '确认卸载' }))
    await waitFor(() => {
      expect(embyFetchSpy).toHaveBeenCalledWith('/emby/Plugins/ext.subfinder', {
        method: 'DELETE',
      })
    })
    unmount()

    // DevicesKeysTab 验证
    vi.spyOn(api, 'getDevices').mockResolvedValue({ Items: [], TotalRecordCount: 0 })
    vi.spyOn(api, 'getApiKeys').mockResolvedValue({
      Items: [
        {
          Id: 'key-id-99',
          AppName: 'Sonarr',
          AccessToken: 'amb_••••••••••••9988',
          MaskedToken: 'amb_••••••••••••9988',
          DateCreated: '2026-05-01T00:00:00Z',
        },
      ],
      TotalRecordCount: 1,
    })
    vi.spyOn(api, 'createApiKey').mockResolvedValue({
      Id: 'key-id-100',
      AppName: 'Radarr',
      AccessToken: 'amb_live_full_secret_token_2026',
      DateCreated: '2026-05-02T00:00:00Z',
    })
    const deleteKeySpy = vi.spyOn(api, 'deleteApiKey').mockResolvedValue(undefined)

    renderAdminUi(<DevicesKeysTab />)
    expect(await screen.findByText('Sonarr')).toBeInTheDocument()
    // 已脱敏历史密钥不应渲染“显示完整密钥”按钮
    expect(screen.queryByTitle('显示完整密钥')).not.toBeInTheDocument()

    // 新建 API Key 展示一次性明文横幅
    fireEvent.change(screen.getByPlaceholderText(/Sonarr/), { target: { value: 'Radarr' } })
    fireEvent.click(screen.getByRole('button', { name: '创建 API Key' }))
    expect(await screen.findByTestId('created-api-key-banner')).toBeInTheDocument()
    expect(screen.getByTestId('created-api-key-value')).toHaveTextContent(
      'amb_live_full_secret_token_2026',
    )

    // 吊销优先传 item.Id
    fireEvent.click(screen.getAllByRole('button', { name: '吊销' })[0])
    fireEvent.click(await screen.findByRole('button', { name: '确认吊销' }))
    await waitFor(() => {
      expect(deleteKeySpy).toHaveBeenCalledWith('key-id-99')
    })
  })

  it('MigrationTab: 初始 cancelled 状态渲染断点摘要横幅，且 Step 3 无密钥时点击返回填写密钥不死锁', async () => {
    vi.spyOn(clientModule, 'embyFetch').mockImplementation(async (path: string) => {
      if (path === '/api/admin/migration/status') {
        return {
          status: 'cancelled',
          dry_run: false,
          source_url: 'http://192.168.50.10:8096',
          total_users: 4,
          processed_users: 2,
          users_matched: 1,
          users_created: 1,
          items_scanned: 280,
          items_matched: 265,
          items_skipped: 15,
          played_migrated: 180,
          favorites_migrated: 40,
          resume_migrated: 12,
          conflicts: [],
          errors: [],
          resumed_from_checkpoint: false,
        }
      }
      return undefined
    })

    renderAdminUi(<MigrationTab />)

    // Step 1 渲染历史断点横幅并预填 source_url
    const banner = await screen.findByTestId('migration-checkpoint-banner')
    expect(banner).toHaveTextContent('http://192.168.50.10:8096')
    const urlInput = screen.getByPlaceholderText(/192\.168\.1\.88/) as HTMLInputElement
    expect(urlInput.value).toBe('http://192.168.50.10:8096')

    // 进入 Step 3 查看中断报告，由于刷新后未填写 apiKey，按钮应可点击并引导跳回 Step 1
    fireEvent.click(within(banner).getByRole('button', { name: /查看中断报告/ }))
    const resumeBtn = await screen.findByRole('button', { name: '返回填写密钥从断点继续' })
    expect(resumeBtn).not.toBeDisabled()

    fireEvent.click(resumeBtn)
    expect(await screen.findByText(/已自动预填上次源服务器地址/)).toBeInTheDocument()
    expect(
      (screen.getByPlaceholderText(/192\.168\.1\.88/) as HTMLInputElement).value,
    ).toBe('http://192.168.50.10:8096')
  })

  it('DashboardTab & TasksLogsTab & WebhooksTab: 服务端日志过滤、多触发器保留增删与 HTTP 200 失败详情展示', async () => {
    vi.spyOn(api, 'getSystemInfo').mockResolvedValue({
      Id: 'srv-1',
      ServerName: 'Ambra',
      Version: '4.10.2.0',
    })
    vi.spyOn(api, 'getSessions').mockResolvedValue([])
    vi.spyOn(api, 'getScanProgress').mockResolvedValue({
      running: false,
      paused: false,
      phase: 'idle',
      total: 0,
      processed: 0,
      added: 0,
      updated: 0,
      skipped: 0,
      failed: 0,
      percentage: 0,
    })
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const urlStr = String(input)
      if (urlStr.includes('Severity=Error') && urlStr.includes('SearchTerm=NVENC')) {
        return new Response(
          JSON.stringify({
            Items: [
              {
                Id: 'log-err-1',
                Name: 'FFmpeg NVENC 初始化失败',
                Type: 'TranscodeError',
                Severity: 'Error',
                Date: new Date().toISOString(),
              },
            ],
            TotalRecordCount: 1,
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        )
      }
      return new Response(JSON.stringify({ Items: [], TotalRecordCount: 0 }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    })

    const { unmount: unmountDash } = renderAdminUi(<DashboardTab />)
    await screen.findByText('系统活动日志')

    fireEvent.click(screen.getByRole('button', { name: 'Error' }))
    fireEvent.change(screen.getByLabelText('搜索活动日志'), { target: { value: 'NVENC' } })

    await waitFor(() => {
      const calledUrls = fetchSpy.mock.calls.map((c) => String(c[0]))
      expect(calledUrls).toContain(
        '/emby/System/ActivityLog/Entries?StartIndex=0&Limit=20&Severity=Error&SearchTerm=NVENC',
      )
    })
    expect(await screen.findByText('FFmpeg NVENC 初始化失败')).toBeInTheDocument()
    unmountDash()

    // TasksLogsTab: 保留 StartupTrigger 并新增 DailyTrigger 一并提交
    vi.spyOn(api, 'getScheduledTasks').mockResolvedValue([
      {
        Id: 'task-1',
        Name: '扫描媒体库',
        State: 'Idle',
        Triggers: [{ Type: 'StartupTrigger' }],
      },
    ])
    vi.spyOn(api, 'getServerLogs').mockResolvedValue([])
    const updateTriggersSpy = vi.spyOn(api, 'updateTaskTriggers').mockResolvedValue(undefined)

    const { unmount: unmountTasks } = renderAdminUi(<TasksLogsTab />)
    fireEvent.click(await screen.findByRole('button', { name: '触发规则' }))
    expect(await screen.findByText('服务器启动时触发')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '添加' }))
    fireEvent.click(screen.getByRole('button', { name: '保存规则' }))

    await waitFor(() => {
      expect(updateTriggersSpy).toHaveBeenCalledWith('task-1', [
        { Type: 'StartupTrigger' },
        expect.objectContaining({ Type: 'DailyTrigger' }),
      ])
    })
    unmountTasks()

    // WebhooksTab: HTTP 200 且 Success === false 展示失败原因与 DurationMs
    vi.spyOn(api, 'getWebhooks').mockResolvedValue([
      {
        Id: 'wh-1',
        Name: 'Slack 告警',
        Url: 'https://hooks.slack.com/services/T00/B00/XX',
        Enabled: true,
        Events: ['*'],
      },
    ])
    vi.spyOn(api, 'testWebhook').mockResolvedValue({
      Success: false,
      StatusCode: 502,
      Error: 'Bad Gateway from upstream proxy',
      DurationMs: 186,
    })

    renderAdminUi(<WebhooksTab />)
    fireEvent.click(await screen.findByRole('button', { name: '发送测试' }))

    const resultBadge = await screen.findByTestId('webhook-test-result-wh-1')
    expect(resultBadge).toHaveTextContent('Bad Gateway from upstream proxy')
    expect(resultBadge).toHaveTextContent('HTTP 502')
    expect(resultBadge).toHaveTextContent('186 ms')
  })

  it('AdminPage: 切换 en-US 与 ja-JP 时 12 个导航项、副标题及面板内容完整国际化且无中文残留', async () => {
    vi.spyOn(api, 'getServerConfig').mockResolvedValue({
      ServerName: 'Ambra Global',
      TMDBApiKey: '********',
      HasTMDBApiKey: true,
      TMDBLanguage: 'en-US',
      EnableSTRM302: true,
      PlayedThreshold: 0.9,
    })
    vi.spyOn(api, 'getUsers').mockResolvedValue([])
    vi.spyOn(clientModule, 'embyFetch').mockResolvedValue(null)

    const { unmount } = renderAdminUi(
      <Routes>
        <Route path="/admin/*" element={<AdminPage />} />
      </Routes>,
      { locale: 'en-US', initialEntries: ['/admin/settings'] },
    )

    const nav = await screen.findByTestId('admin-tab-nav')
    expect(within(nav).getByText('Dashboard')).toBeInTheDocument()
    expect(within(nav).getByText('Metadata')).toBeInTheDocument()
    expect(within(nav).getByText('Health & Backup')).toBeInTheDocument()
    expect(within(nav).getByText('Diagnostics · Audit · Hot Backup')).toBeInTheDocument()
    expect(await screen.findByText('Network, Remote Access & Protocols')).toBeInTheDocument()
    expect(screen.getByText('Transcoding & Hardware Acceleration')).toBeInTheDocument()
    expect(/[\u4e00-\u9fff]/.test(nav.textContent || '')).toBe(false)
    unmount()

    renderAdminUi(
      <Routes>
        <Route path="/admin/*" element={<AdminPage />} />
      </Routes>,
      { locale: 'ja-JP', initialEntries: ['/admin/settings'] },
    )
    const jaNav = await screen.findByTestId('admin-tab-nav')
    expect(within(jaNav).getByText('メタデータ管理')).toBeInTheDocument()
    expect(within(jaNav).getByText('ヘルスとバックアップ')).toBeInTheDocument()
    expect(
      await screen.findByText('トランスコードとハードウェアアクセラレーション'),
    ).toBeInTheDocument()
  })
})
