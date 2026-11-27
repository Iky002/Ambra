import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import * as clientModule from '../../../api/client'
import { api, saveAuth } from '../../../api/client'
import { AuthProvider } from '../../../context/AuthContext'
import { setDemoModeOverride } from '../../../demo/demoMode'
import { I18nProvider } from '../../../i18n/I18nContext'
import type { LocaleCode } from '../../../i18n/types'
import { DashboardTab } from '../DashboardTab'
import { DevicesKeysTab } from '../DevicesKeysTab'
import { HealthAndAuditTab } from '../HealthAndAuditTab'
import { LibrariesTab } from '../LibrariesTab'
import { LiveTvTab } from '../LiveTvTab'
import { MetadataTab } from '../MetadataTab'
import { MigrationTab } from '../MigrationTab'
import { PluginsTab } from '../PluginsTab'
import { SettingsTab } from '../SettingsTab'
import { TasksLogsTab } from '../TasksLogsTab'
import { UsersTab } from '../UsersTab'
import { WebhooksTab } from '../WebhooksTab'

function renderAdmin(ui: React.ReactElement, locale: LocaleCode = 'zh-CN') {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
    },
  })
  return {
    queryClient,
    ...render(
      <I18nProvider initialLocale={locale}>
        <AuthProvider>
          <QueryClientProvider client={queryClient}>
            <MemoryRouter>{ui}</MemoryRouter>
          </QueryClientProvider>
        </AuthProvider>
      </I18nProvider>,
    ),
  }
}

describe('Admin Console Wave 1 Audit Remediation & Feature Completeness', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    setDemoModeOverride(false)
    saveAuth({
      accessToken: 'admin-wave1-token',
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
    sessionStorage.clear()
  })

  it('C-01 (MetadataTab): 删除确认弹窗拉取 /DeleteInfo 并展示物理磁盘永久删除警告与递归路径列表', async () => {
    vi.spyOn(api, 'getVirtualFolders').mockResolvedValue([])
    vi.spyOn(api, 'getItems').mockResolvedValue({
      Items: [
        {
          Id: 'series-dune',
          Name: '沙丘：预言',
          Type: 'Series',
          Path: '/mnt/nas/tv/Dune Prophecy',
        },
      ],
      TotalRecordCount: 1,
    })
    const deleteItemSpy = vi.spyOn(api, 'deleteItem').mockResolvedValue(undefined)
    const embyFetchSpy = vi
      .spyOn(clientModule, 'embyFetch')
      .mockImplementation(async (path: string) => {
        if (path === '/emby/Items/series-dune/DeleteInfo') {
          return {
            Paths: [
              '/mnt/nas/tv/Dune Prophecy/Season 01/S01E01.mkv',
              '/mnt/nas/tv/Dune Prophecy/Season 01/S01E01.zh.srt',
              '/mnt/nas/tv/Dune Prophecy/Season 01/S01E02.mkv',
            ],
          }
        }
        return undefined
      })

    renderAdmin(<MetadataTab />)

    expect(await screen.findByText('沙丘：预言')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '删除' }))

    expect(deleteItemSpy).not.toHaveBeenCalled()

    const dangerAlert = await screen.findByTestId('metadata-physical-delete-warning')
    expect(dangerAlert).toHaveTextContent('永久磁盘删除')
    expect(dangerAlert).toHaveTextContent('原始媒体文件')
    expect(dangerAlert).toHaveTextContent('递归删除')

    const pathsBox = await screen.findByTestId('delete-info-paths')
    await waitFor(() => {
      expect(embyFetchSpy).toHaveBeenCalledWith('/emby/Items/series-dune/DeleteInfo')
      expect(pathsBox).toHaveTextContent('共 3 项')
      expect(pathsBox).toHaveTextContent('/mnt/nas/tv/Dune Prophecy/Season 01/S01E01.mkv')
      expect(pathsBox).toHaveTextContent('/mnt/nas/tv/Dune Prophecy/Season 01/S01E02.mkv')
    })
  })

  it('H-13 & M-15 (PluginsTab): 配置加载失败时展示错误横幅+重试并禁用保存，且 #tvdb-pin 为 password 输入框', async () => {
    vi.spyOn(api, 'getPlugins').mockResolvedValue([
      {
        Id: 'ambra.builtin.tvdb',
        Name: 'TheTVDB Scraper',
        BuiltIn: true,
        Enabled: true,
        Priority: 20,
        Kinds: ['metadata'],
      },
    ])
    let shouldFail = true
    const getConfigSpy = vi
      .spyOn(api, 'getPluginConfiguration')
      .mockImplementation(async () => {
        if (shouldFail) {
          throw new Error('HTTP 502 Bad Gateway')
        }
        return {
          enabled: true,
          priority: 20,
          api_key: '********',
          has_api_key: true,
          pin: '',
          language: 'zh-CN',
        }
      })
    const updateSpy = vi.spyOn(api, 'updatePluginConfiguration').mockResolvedValue(undefined)

    renderAdmin(<PluginsTab />)

    fireEvent.click(await screen.findByRole('button', { name: '编辑配置' }))

    const errAlert = await screen.findByTestId('plugin-config-load-error')
    expect(errAlert).toHaveTextContent('HTTP 502 Bad Gateway')

    const saveBtn = screen.getByRole('button', { name: '保存配置' })
    expect(saveBtn).toBeDisabled()
    fireEvent.click(saveBtn)
    expect(updateSpy).not.toHaveBeenCalled()

    // 点击重试恢复成功，保存按钮重新可用，且 #tvdb-pin 为 password + new-password
    shouldFail = false
    fireEvent.click(within(errAlert).getByRole('button', { name: '重试' }))

    await waitFor(() => {
      expect(getConfigSpy).toHaveBeenCalledTimes(2)
      expect(screen.queryByTestId('plugin-config-load-error')).not.toBeInTheDocument()
      expect(screen.getByRole('button', { name: '保存配置' })).not.toBeDisabled()
    })

    const pinInput = document.getElementById('tvdb-pin') as HTMLInputElement
    expect(pinInput).toBeTruthy()
    expect(pinInput.type).toBe('password')
    expect(pinInput.autocomplete).toBe('new-password')
  })

  it('H-13 (SettingsTab): 转码配置读取失败时禁用保存转码设置；重试成功后合并保留高级字段提交', async () => {
    vi.spyOn(api, 'getServerConfig').mockResolvedValue({
      ServerName: 'Ambra',
      TMDBLanguage: 'zh-CN',
      EnableSTRM302: true,
      PlayedThreshold: 0.9,
    })
    vi.spyOn(api, 'getUsers').mockResolvedValue([])

    let failFfmpeg = true
    const embyFetchSpy = vi
      .spyOn(clientModule, 'embyFetch')
      .mockImplementation(async (path: string, init?: RequestInit) => {
        if (path === '/emby/Encoding/FfmpegOptions' && (!init?.method || init.method === 'GET')) {
          if (failFfmpeg) {
            throw new Error('FFmpeg 配置接口 500')
          }
          return {
            HardwareAccelerationMode: 3,
            EnableHardwareEncoding: true,
            AllowHevcEncoding: true,
            H264Crf: 21,
            H264Preset: 'fast',
            EnableThrottling: true,
            ThrottleBufferSize: 120,
            ThrottleHysteresis: 15,
            EnableSubtitleExtraction: true,
            TranscodingTempPath: '/var/cache/ambra',
          }
        }
        if (
          path === '/emby/Encoding/FullToneMapOptions' &&
          (!init?.method || init.method === 'GET')
        ) {
          return {
            EnableToneMapping: true,
            ToneMappingAlgorithm: 'hable',
            ToneMappingPeak: 100,
            ToneMappingMode: 'auto',
          }
        }
        return undefined
      })

    renderAdmin(<SettingsTab />)

    const encodingCard = await screen.findByTestId('settings-encoding-card')
    const errBanner = await within(encodingCard).findByTestId('encoding-load-error')
    expect(errBanner).toHaveTextContent('FFmpeg 配置接口 500')

    const saveEncodingBtn = within(encodingCard).getByRole('button', { name: '保存转码设置' })
    expect(saveEncodingBtn).toBeDisabled()

    // 重试成功后高级字段在保存时完整保留
    failFfmpeg = false
    fireEvent.click(within(errBanner).getByRole('button', { name: '重试' }))

    await waitFor(() => {
      expect(within(encodingCard).queryByTestId('encoding-load-error')).not.toBeInTheDocument()
      expect(saveEncodingBtn).not.toBeDisabled()
    })

    const crfInput = document.getElementById('encoding-h264-crf') as HTMLInputElement
    fireEvent.change(crfInput, { target: { value: '19' } })
    fireEvent.click(saveEncodingBtn)

    await waitFor(() => {
      const postFfmpegCall = embyFetchSpy.mock.calls.find(
        (c) => c[0] === '/emby/Encoding/FfmpegOptions' && c[1]?.method === 'POST',
      )
      expect(postFfmpegCall).toBeTruthy()
      const ffmpegBody = JSON.parse(String(postFfmpegCall![1]!.body))
      expect(ffmpegBody).toMatchObject({
        HardwareAccelerationMode: 3,
        H264Crf: 19,
        ThrottleHysteresis: 15,
        EnableSubtitleExtraction: true,
      })

      const postToneMapCall = embyFetchSpy.mock.calls.find(
        (c) => c[0] === '/emby/Encoding/FullToneMapOptions' && c[1]?.method === 'POST',
      )
      expect(postToneMapCall).toBeTruthy()
      const toneBody = JSON.parse(String(postToneMapCall![1]!.body))
      expect(toneBody).toMatchObject({
        EnableToneMapping: true,
        ToneMappingAlgorithm: 'hable',
        ToneMappingPeak: 100,
        ToneMappingMode: 'auto',
      })
    })
  })

  it('M-15 (HealthAndAuditTab): 一键清理孤立项前弹出 NAS 在线核验确认弹窗', async () => {
    vi.spyOn(api, 'getBackupInfo').mockResolvedValue({
      Backups: [],
      BackupDir: '/backups',
      MaxBackups: 5,
    })
    const embyFetchSpy = vi
      .spyOn(clientModule, 'embyFetch')
      .mockImplementation(async (path: string) => {
        if (path === '/api/admin/health') {
          return {
            CollectedAt: Date.now(),
            Status: 'healthy',
            GoVersion: 'go1.24',
            NumCpu: 8,
            NumGoroutine: 24,
            Memory: {
              AllocBytes: 64 * 1024 * 1024,
              TotalAllocBytes: 128 * 1024 * 1024,
              SysBytes: 256 * 1024 * 1024,
              HeapInuseBytes: 64 * 1024 * 1024,
              NumGC: 12,
            },
            Database: {
              DbPath: '/data/ambra.db',
              DbBytes: 10 * 1024 * 1024,
              WalBytes: 1024 * 1024,
              ShmBytes: 32768,
              TotalBytes: 11 * 1024 * 1024,
            },
            Cache: {
              ImagesBytes: 50 * 1024 * 1024,
              SubtitlesBytes: 2 * 1024 * 1024,
              TranscodeBytes: 0,
              TotalBytes: 52 * 1024 * 1024,
            },
            Disk: {
              Path: '/data',
              TotalBytes: 100 * 1024 * 1024 * 1024,
              FreeBytes: 60 * 1024 * 1024 * 1024,
              AvailableBytes: 60 * 1024 * 1024 * 1024,
              UsedPercent: 40,
            },
            Library: {
              TotalItems: 120,
              MovieCount: 80,
              SeriesCount: 10,
              EpisodeCount: 30,
              UserCount: 2,
              PendingAudits: 0,
            },
            Scheduler: {
              TotalTasks: 5,
              ActiveTasks: 0,
              BudgetInUse: 0,
              BudgetCapacity: 4,
              BudgetUsagePct: 0,
            },
          }
        }
        if (path.startsWith('/api/admin/scrape-reviews')) {
          return { Items: [], TotalRecordCount: 0 }
        }
        if (path === '/api/admin/governance/audit') {
          return {
            ScannedAt: Date.now(),
            TotalChecked: 120,
            MissingPosters: [],
            MissingTmdb: [],
            OrphanItems: [
              {
                Id: 'orphan-1',
                Name: '星际穿越',
                Type: 'Movie',
                LibraryId: 'lib-1',
                Path: '/mnt/nas/movies/Interstellar.mkv',
                IsStrm: false,
              },
              {
                Id: 'orphan-2',
                Name: '盗梦空间',
                Type: 'Movie',
                LibraryId: 'lib-1',
                Path: '/mnt/nas/movies/Inception.mkv',
                IsStrm: false,
              },
            ],
          }
        }
        if (path === '/api/admin/governance/orphans/cleanup') {
          return { Deleted: 2 }
        }
        return undefined
      })

    renderAdmin(<HealthAndAuditTab />)

    fireEvent.click(await screen.findByRole('button', { name: /一键全面体检/ }))
    const orphansTab = await screen.findByRole('button', { name: /磁盘孤立记录/ })
    fireEvent.click(orphansTab)

    fireEvent.click(await screen.findByRole('button', { name: /全选/ }))

    const cleanupBtn = await screen.findByRole('button', { name: /一键清理选中 \(2\)/ })
    fireEvent.click(cleanupBtn)

    // 未点确认前绝不发起 /api/admin/governance/orphans/cleanup
    expect(
      embyFetchSpy.mock.calls.some((c) => c[0] === '/api/admin/governance/orphans/cleanup'),
    ).toBe(false)

    const nasWarn = await screen.findByTestId('orphan-cleanup-nas-warning')
    expect(nasWarn).toHaveTextContent('NAS')
    expect(nasWarn).toHaveTextContent('2')

    fireEvent.click(screen.getByRole('button', { name: '确认清理 (2)' }))
    await waitFor(() => {
      expect(embyFetchSpy).toHaveBeenCalledWith(
        '/api/admin/governance/orphans/cleanup',
        expect.objectContaining({
          method: 'POST',
          body: JSON.stringify({ ItemIds: ['orphan-1', 'orphan-2'] }),
        }),
      )
    })
  })

  it('M-15 (WebhooksTab & MigrationTab): Webhook 修改 URL 域名/路径时告警密钥将自动清除；Migration 启动失败留在第 2 步', async () => {
    vi.spyOn(api, 'getWebhooks').mockResolvedValue([
      {
        Id: 'wh-sec',
        Name: 'Discord 通知',
        Url: 'https://discord.com/api/webhooks/111/aaa?wait=true',
        HasSecret: true,
        Enabled: true,
        Events: ['*'],
      },
    ])

    const { unmount } = renderAdmin(<WebhooksTab />)

    fireEvent.click(await screen.findByRole('button', { name: '编辑' }))
    const urlInput = screen.getByLabelText(/推送 URL/) as HTMLInputElement
    expect(screen.queryByTestId('webhook-secret-url-change-warning')).not.toBeInTheDocument()

    // 仅修改 query 参数不触发清空告警
    fireEvent.change(urlInput, {
      target: { value: 'https://discord.com/api/webhooks/111/aaa?wait=false' },
    })
    expect(screen.queryByTestId('webhook-secret-url-change-warning')).not.toBeInTheDocument()

    // 修改 pathname 或域名且未输入新密钥时触发高危清空提示
    fireEvent.change(urlInput, {
      target: { value: 'https://hooks.example.org/new-endpoint' },
    })
    expect(screen.getByTestId('webhook-secret-url-change-warning')).toHaveTextContent(
      '原有 HMAC 签名密钥将在保存时自动清除',
    )
    unmount()

    // MigrationTab: /api/admin/migration/start 失败时停留在第 2 步，成功后才进入第 3 步
    let startShouldFail = true
    vi.spyOn(clientModule, 'embyFetch').mockImplementation(async (path: string) => {
      if (path === '/api/admin/migration/status') {
        return { status: 'idle', conflicts: [], errors: [] }
      }
      if (path === '/api/admin/migration/test') {
        return {
          server: {
            Id: 'src-srv-1',
            ServerName: 'Legacy Jellyfin',
            Version: '10.9.0',
            Status: 'ok',
          },
          users: [{ Id: 'u-1', Name: 'alice' }],
          virtual_folders: [{ Name: 'Movies', CollectionType: 'movies', Locations: [] }],
        }
      }
      if (path === '/api/admin/migration/start') {
        if (startShouldFail) {
          throw new Error('源服务器连接中断 (ECONNREFUSED)')
        }
        return {
          status: 'running',
          dry_run: false,
          total_users: 1,
          processed_users: 0,
          users_matched: 0,
          users_created: 0,
          items_scanned: 0,
          items_matched: 0,
          items_skipped: 0,
          played_migrated: 0,
          favorites_migrated: 0,
          resume_migrated: 0,
          conflicts: [],
          errors: [],
        }
      }
      return undefined
    })

    renderAdmin(<MigrationTab />)

    fireEvent.change(screen.getByPlaceholderText(/192\.168\.1\.88/), {
      target: { value: 'http://192.168.1.99:8096' },
    })
    const pwdInput = document.querySelector('input[type="password"]') as HTMLInputElement
    fireEvent.change(pwdInput, { target: { value: 'jf-secret-key' } })
    fireEvent.click(screen.getByRole('button', { name: /测试连接/ }))

    fireEvent.click(await screen.findByRole('button', { name: /下一步/ }))

    // 第 2 步点击开始迁移但后端报错 -> 保持在第 2 步并显示错误
    const startBtn = await screen.findByRole('button', { name: /开始正式迁移/ })
    fireEvent.click(startBtn)

    expect(await screen.findByText(/源服务器连接中断/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /开始正式迁移/ })).toBeInTheDocument()

    // 修复后再次点击 -> 成功跳入第 3 步
    startShouldFail = false
    fireEvent.click(screen.getByRole('button', { name: /开始正式迁移/ }))
    expect(await screen.findByText(/迁移任务已启动/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /开始正式迁移/ })).not.toBeInTheDocument()
  })

  it('L-11 (DevicesKeysTab): 吊销刚创建的 API Key 时立即清除顶部一次性明文密钥横幅', async () => {
    vi.spyOn(api, 'getDevices').mockResolvedValue({ Items: [], TotalRecordCount: 0 })
    const keysList = [
      {
        Id: 'key-new-1',
        AppName: 'Prowlarr',
        AccessToken: 'amb_••••••••••••7777',
        MaskedToken: 'amb_••••••••••••7777',
        DateCreated: '2026-11-26T00:00:00Z',
      },
    ]
    vi.spyOn(api, 'getApiKeys').mockImplementation(async () => ({
      Items: keysList,
      TotalRecordCount: keysList.length,
    }))
    vi.spyOn(api, 'createApiKey').mockResolvedValue({
      Id: 'key-new-1',
      AppName: 'Prowlarr',
      AccessToken: 'amb_plaintext_once_7777',
      DateCreated: '2026-11-26T00:00:00Z',
    })
    vi.spyOn(api, 'deleteApiKey').mockResolvedValue(undefined)

    renderAdmin(<DevicesKeysTab />)

    fireEvent.change(await screen.findByPlaceholderText(/Sonarr/), {
      target: { value: 'Prowlarr' },
    })
    fireEvent.click(screen.getByRole('button', { name: '创建 API Key' }))

    expect(await screen.findByTestId('created-api-key-banner')).toBeInTheDocument()
    expect(screen.getByTestId('created-api-key-value')).toHaveTextContent(
      'amb_plaintext_once_7777',
    )

    fireEvent.click(screen.getByRole('button', { name: '吊销' }))
    fireEvent.click(await screen.findByRole('button', { name: '确认吊销' }))

    await waitFor(() => {
      expect(screen.queryByTestId('created-api-key-banner')).not.toBeInTheDocument()
    })
  })

  it('3.2-1 (UsersTab): 用户权限策略弹窗支持切换 IsHidden / EnableRemoteAccess / 媒体播放与转码拆封开关', async () => {
    vi.spyOn(api, 'getUsers').mockResolvedValue([
      {
        Id: 'user-bob',
        Name: 'bob',
        ServerId: 'srv-1',
        Policy: {
          IsAdministrator: false,
          IsHidden: false,
          EnableRemoteAccess: true,
          EnableAllFolders: true,
          EnabledFolders: [],
          EnableMediaPlayback: true,
          EnableVideoPlaybackTranscoding: true,
          EnableAudioPlaybackTranscoding: true,
          EnablePlaybackRemuxing: true,
        },
      },
    ])
    vi.spyOn(api, 'getVirtualFolders').mockResolvedValue([])
    vi.spyOn(api, 'getParentalRatings').mockResolvedValue([])
    const updatePolicySpy = vi.spyOn(api, 'updateUserPolicy').mockResolvedValue(undefined)

    renderAdmin(<UsersTab />)

    fireEvent.click(await screen.findByRole('button', { name: /权限策略/ }))

    fireEvent.click(await screen.findByRole('switch', { name: /IsHidden/ }))
    fireEvent.click(screen.getByRole('switch', { name: /EnableRemoteAccess/ }))
    fireEvent.click(screen.getByRole('switch', { name: /EnableMediaPlayback/ }))
    fireEvent.click(screen.getByRole('switch', { name: /EnableAudioPlaybackTranscoding/ }))
    fireEvent.click(screen.getByRole('switch', { name: /EnablePlaybackRemuxing/ }))

    fireEvent.click(screen.getByRole('button', { name: '保存策略' }))

    await waitFor(() => {
      expect(updatePolicySpy).toHaveBeenCalledWith(
        'user-bob',
        expect.objectContaining({
          IsHidden: true,
          EnableRemoteAccess: false,
          EnableMediaPlayback: false,
          EnableAudioPlaybackTranscoding: false,
          EnablePlaybackRemuxing: false,
        }),
      )
    })
  })

  it('M-15 & 3.2-2 (LibrariesTab): 支持媒体库刮削器启停与优先级排序保存，且移除挂载目录前需二次确认', async () => {
    vi.spyOn(api, 'getVirtualFolders').mockResolvedValue([
      {
        ItemId: 'lib-tv',
        Name: '欧美剧集',
        CollectionType: 'tvshows',
        Locations: ['/mnt/nas/tv1', '/mnt/nas/tv2'],
        LibraryOptions: {
          SaveLocalMetadata: true,
          MetadataSavers: ['Nfo'],
          MetadataFetchers: ['Nfo', 'TheMovieDb', 'TheTVDB'],
          DisabledMetadataFetchers: [],
          MetadataFetcherOrder: ['Nfo', 'TheMovieDb', 'TheTVDB'],
        },
      },
    ])
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
    const updateOptionsSpy = vi
      .spyOn(api, 'updateVirtualFolderOptions')
      .mockResolvedValue(undefined)
    const removePathSpy = vi.spyOn(api, 'removeVirtualFolderPath').mockResolvedValue(undefined)

    renderAdmin(<LibrariesTab />)

    fireEvent.click(await screen.findByRole('button', { name: '编辑' }))

    const scrapersBox = await screen.findByTestId('edit-library-scrapers')
    // 将 TheTVDB 上移一位，并取消勾选 本地 NFO 读取器
    fireEvent.click(within(scrapersBox).getByRole('button', { name: /上移 TheTVDB/ }))
    const nfoRow = within(scrapersBox).getByTestId('scraper-row-nfo')
    fireEvent.click(within(nfoRow).getByRole('checkbox'))

    fireEvent.click(within(scrapersBox).getByRole('button', { name: '保存刮削器配置' }))

    await waitFor(() => {
      expect(updateOptionsSpy).toHaveBeenCalledWith(
        'lib-tv',
        expect.objectContaining({
          MetadataFetchers: ['TheTVDB', 'TheMovieDb'],
          DisabledMetadataFetchers: ['Nfo'],
          DisabledLocalMetadataReaders: ['Nfo'],
          MetadataFetcherOrder: ['Nfo', 'TheTVDB', 'TheMovieDb'],
        }),
      )
    })

    // M-15: 点击移除挂载目录需先弹出确认框
    fireEvent.click(screen.getByRole('button', { name: '移除路径 /mnt/nas/tv2' }))
    expect(removePathSpy).not.toHaveBeenCalled()
    expect(await screen.findByTestId('confirm-remove-library-path')).toHaveTextContent(
      '/mnt/nas/tv2',
    )

    fireEvent.click(screen.getByRole('button', { name: '确认移除' }))
    await waitFor(() => {
      expect(removePathSpy).toHaveBeenCalledWith('欧美剧集', '/mnt/nas/tv2', 'lib-tv')
    })
  })

  it('M-15 & 3.2-3 (LiveTvTab): 支持局域网 HDHomeRun 发现一键填入、调谐器删除确认弹窗及频道 EPG 映射保存', async () => {
    vi.spyOn(api, 'getTunerHosts').mockResolvedValue([
      {
        Id: 'tuner-1',
        Type: 'm3u',
        FriendlyName: '客厅 IPTV',
        Url: 'http://192.168.1.10/iptv.m3u',
      },
    ])
    vi.spyOn(api, 'getListingProviders').mockResolvedValue([
      { Id: 'epg-1', Type: 'xmltv', Path: 'http://192.168.1.10/epg.xml' },
    ])
    vi.spyOn(api, 'getLiveTvChannels').mockResolvedValue({
      Items: [{ Id: 'ch-cctv1', Name: 'CCTV-1 综合', Number: '1' }],
      TotalRecordCount: 1,
    })
    const addTunerSpy = vi.spyOn(api, 'addTunerHost').mockResolvedValue({
      Id: 'tuner-hdhr',
      Type: 'hdhomerun',
      FriendlyName: 'HDHomeRun CONNECT',
      Url: 'http://192.168.1.55',
    })
    const deleteTunerSpy = vi.spyOn(api, 'deleteTunerHost').mockResolvedValue(undefined)

    const embyFetchSpy = vi
      .spyOn(clientModule, 'embyFetch')
      .mockImplementation(async (path: string, init?: RequestInit) => {
        if (path === '/emby/LiveTv/Tuners/Discover') {
          return [
            {
              DeviceId: '1080ABCD',
              FriendlyName: 'HDHomeRun CONNECT',
              Type: 'hdhomerun',
              Url: 'http://192.168.1.55',
              TunerCount: 4,
            },
          ]
        }
        if (path.startsWith('/emby/LiveTv/ChannelMappingOptions')) {
          return {
            TunerChannels: [{ Id: 'ch-cctv1', Name: 'CCTV-1 综合', Number: '1' }],
            ProviderChannels: [
              { Id: 'cctv1.cn', Name: 'CCTV1 综合高清' },
              { Id: 'cctv5.cn', Name: 'CCTV5 体育' },
            ],
            Mappings: [],
          }
        }
        if (path === '/emby/LiveTv/ChannelMappings' && init?.method === 'POST') {
          return { TunerChannelId: 'ch-cctv1', ProviderChannelId: 'cctv1.cn' }
        }
        return undefined
      })

    renderAdmin(<LiveTvTab />)

    // 1. 扫描局域网调谐器并一键填入添加 HDHomeRun
    fireEvent.click(await screen.findByTestId('discover-lan-tuners-btn'))
    expect(await screen.findByTestId('discovered-tuners-panel')).toHaveTextContent(
      'HDHomeRun CONNECT',
    )
    fireEvent.click(screen.getByTestId('use-discovered-tuner-0'))

    expect((document.getElementById('livetv-tuner-type') as HTMLSelectElement).value).toBe(
      'hdhomerun',
    )
    expect((document.getElementById('livetv-tuner-url') as HTMLInputElement).value).toBe(
      'http://192.168.1.55',
    )

    fireEvent.click(screen.getByRole('button', { name: '添加源' }))
    await waitFor(() => {
      expect(addTunerSpy).toHaveBeenCalledWith({
        Type: 'hdhomerun',
        FriendlyName: 'HDHomeRun CONNECT',
        Url: 'http://192.168.1.55',
      })
    })

    // 2. 删除调谐器需经过确认弹窗
    const tunerHeading = await screen.findByText('客厅 IPTV')
    const tunerRow = tunerHeading.closest('div.flex.flex-wrap') as HTMLElement
    fireEvent.click(within(tunerRow).getByRole('button', { name: '删除' }))
    expect(deleteTunerSpy).not.toHaveBeenCalled()
    expect(await screen.findByTestId('confirm-delete-tuner-warning')).toHaveTextContent(
      '客厅 IPTV',
    )

    fireEvent.click(screen.getByTestId('confirm-delete-tuner-btn'))
    await waitFor(() => {
      expect(deleteTunerSpy).toHaveBeenCalledWith('tuner-1')
    })

    // 3. 展开频道与 EPG 映射面板并保存映射
    fireEvent.click(screen.getByTestId('toggle-channel-mapping-btn'))
    const selectEl = (await screen.findByTestId(
      'channel-mapping-select-ch-cctv1',
    )) as HTMLSelectElement
    fireEvent.change(selectEl, { target: { value: 'cctv1.cn' } })
    fireEvent.click(screen.getByTestId('save-channel-mapping-ch-cctv1'))

    await waitFor(() => {
      expect(embyFetchSpy).toHaveBeenCalledWith(
        '/emby/LiveTv/ChannelMappings',
        expect.objectContaining({
          method: 'POST',
          body: JSON.stringify({
            ProviderId: 'epg-1',
            TunerChannelId: 'ch-cctv1',
            ProviderChannelId: 'cctv1.cn',
          }),
        }),
      )
    })
  })

  it('L-11 & 3.2-4 (DashboardTab & TasksLogsTab): 服务器重启/关机二次确认、Sync 离线任务取消与日志延迟过滤', async () => {
    vi.spyOn(api, 'getSystemInfo').mockResolvedValue({
      Id: 'srv-1',
      ServerName: 'Ambra Master',
      Version: '4.10.2.0',
    })
    vi.spyOn(api, 'getSessions').mockResolvedValue([])
    vi.spyOn(api, 'getActivityLogs').mockResolvedValue({ Items: [], TotalRecordCount: 0 })
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

    const embyFetchSpy = vi
      .spyOn(clientModule, 'embyFetch')
      .mockImplementation(async (path: string, init?: RequestInit) => {
        if (path === '/emby/Sync/Jobs' && (!init?.method || init.method === 'GET')) {
          return {
            Items: [
              {
                Id: 'sync-job-42',
                Name: '奥本海默 (离线 1080p)',
                Status: 'Converting',
                Progress: 64.4,
                TargetName: 'iPad Pro',
                Profile: 'mobile',
                Quality: '1080p',
                ItemCount: 1,
              },
            ],
            TotalRecordCount: 1,
          }
        }
        return undefined
      })

    const { unmount } = renderAdmin(<DashboardTab />)

    // 重启确认弹窗
    fireEvent.click(await screen.findByTestId('server-restart-btn'))
    expect(
      embyFetchSpy.mock.calls.some((c) => c[0] === '/emby/System/Restart'),
    ).toBe(false)
    expect(await screen.findByTestId('server-power-confirm-warning')).toHaveTextContent(
      '重启期间将中断',
    )
    fireEvent.click(screen.getByTestId('confirm-server-power-btn'))
    await waitFor(() => {
      expect(embyFetchSpy).toHaveBeenCalledWith('/emby/System/Restart', { method: 'POST' })
    })

    // 关机确认弹窗
    fireEvent.click(screen.getByTestId('server-shutdown-btn'))
    expect(
      embyFetchSpy.mock.calls.some((c) => c[0] === '/emby/System/Shutdown'),
    ).toBe(false)
    expect(await screen.findByTestId('server-power-confirm-warning')).toHaveTextContent(
      '关闭后无法通过网页端远程唤醒',
    )
    fireEvent.click(screen.getByTestId('confirm-server-power-btn'))
    await waitFor(() => {
      expect(embyFetchSpy).toHaveBeenCalledWith('/emby/System/Shutdown', { method: 'POST' })
    })
    unmount()

    // TasksLogsTab: Sync Jobs 列表展示、取消确认弹窗与 useDeferredValue 日志过滤
    vi.spyOn(api, 'getScheduledTasks').mockResolvedValue([])
    vi.spyOn(api, 'getServerLogs').mockResolvedValue([
      { Name: 'ambra.log', Size: 2048, DateModified: '2026-11-26T00:00:00Z' },
    ])
    vi.spyOn(api, 'getServerLogContent').mockResolvedValue(
      [
        '2026-11-26 10:00:01 INFO boot ok',
        '2026-11-26 10:01:02 ERROR vaapi device busy',
        '2026-11-26 10:02:03 INFO scanner idle',
      ].join('\n'),
    )

    renderAdmin(<TasksLogsTab />)

    const syncRow = await screen.findByTestId('sync-job-row-sync-job-42')
    expect(syncRow).toHaveTextContent('奥本海默 (离线 1080p)')
    expect(syncRow).toHaveTextContent('64%')
    expect(syncRow).toHaveTextContent('iPad Pro')

    fireEvent.click(screen.getByTestId('cancel-sync-job-sync-job-42'))
    expect(
      embyFetchSpy.mock.calls.some(
        (c) => c[0] === '/emby/Sync/Jobs/sync-job-42' && c[1]?.method === 'DELETE',
      ),
    ).toBe(false)
    expect(await screen.findByTestId('confirm-cancel-sync-job-warning')).toHaveTextContent(
      '奥本海默',
    )
    fireEvent.click(screen.getByTestId('confirm-cancel-sync-job-btn'))
    await waitFor(() => {
      expect(embyFetchSpy).toHaveBeenCalledWith('/emby/Sync/Jobs/sync-job-42', {
        method: 'DELETE',
      })
    })

    // L-11: 日志过滤
    fireEvent.click(screen.getByRole('button', { name: /ambra\.log/ }))
    expect(await screen.findByText(/vaapi device busy/)).toBeInTheDocument()
    fireEvent.change(screen.getByPlaceholderText(/过滤日志关键词/), {
      target: { value: 'vaapi' },
    })
    await waitFor(() => {
      expect(screen.getByText(/vaapi device busy/)).toBeInTheDocument()
      expect(screen.queryByText(/boot ok/)).not.toBeInTheDocument()
    })
  })

  it('SettingsTab 保存 H264Crf = 0 时保持 0 而非回退为 23', async () => {
    vi.spyOn(api, 'getServerConfig').mockResolvedValue({
      ServerName: 'Ambra Lab',
      EnableLoginUserList: true,
    })
    vi.spyOn(api, 'getUsers').mockResolvedValue([])
    const embyFetchSpy = vi
      .spyOn(clientModule, 'embyFetch')
      .mockImplementation(async (path: string, init?: RequestInit) => {
        if (path === '/emby/Encoding/FfmpegOptions' && (!init?.method || init.method === 'GET')) {
          return {
            HardwareAccelerationMode: 0,
            EnableHardwareEncoding: false,
            AllowHevcEncoding: false,
            H264Crf: 23,
            H264Preset: 'veryfast',
            EnableThrottling: true,
            ThrottleBufferSize: 120,
            TranscodingTempPath: '/tmp/ambra',
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

    renderAdmin(<SettingsTab />)

    const crfInput = (await screen.findByLabelText(/H\.264 CRF|CRF/)) as HTMLInputElement
    await waitFor(() => {
      expect(crfInput.value).toBe('23')
    })

    fireEvent.change(crfInput, { target: { value: '0' } })
    expect(crfInput.value).toBe('0')

    fireEvent.submit(crfInput.closest('form')!)

    await waitFor(() => {
      const postCall = embyFetchSpy.mock.calls.find(
        (c) => c[0] === '/emby/Encoding/FfmpegOptions' && c[1]?.method === 'POST',
      )
      expect(postCall).toBeDefined()
      const payload = JSON.parse(String(postCall?.[1]?.body || '{}'))
      expect(payload.H264Crf).toBe(0)
    })
  })

  it('LiveTvTab 保存频道映射后清除已保存草稿，且点击刷新映射选项清空全部未保存草稿', async () => {
    vi.spyOn(api, 'getTunerHosts').mockResolvedValue([])
    vi.spyOn(api, 'getListingProviders').mockResolvedValue([
      { Id: 'epg-1', Type: 'xmltv', Path: '/epg.xml' },
    ])
    vi.spyOn(api, 'getLiveTvChannels').mockResolvedValue({
      Items: [
        { Id: 'ch-1', Name: 'CCTV-1', Number: '1' },
        { Id: 'ch-2', Name: 'CCTV-6', Number: '6' },
      ],
      TotalRecordCount: 2,
    })

    let serverMappedCh1 = 'xmltv-1'
    const serverMappedCh2 = 'xmltv-6'
    vi.spyOn(clientModule, 'embyFetch').mockImplementation(
      async (path: string, init?: RequestInit) => {
        if (path.startsWith('/emby/LiveTv/ChannelMappingOptions')) {
          return {
            TunerChannels: [
              { Id: 'ch-1', Name: 'CCTV-1', Number: '1', ProviderChannelId: serverMappedCh1 },
              { Id: 'ch-2', Name: 'CCTV-6', Number: '6', ProviderChannelId: serverMappedCh2 },
            ],
            ProviderChannels: [
              { Id: 'xmltv-1', Name: 'CCTV-1 (XMLTV)' },
              { Id: 'xmltv-1-alt', Name: 'CCTV-1 HD (XMLTV)' },
              { Id: 'xmltv-6', Name: 'CCTV-6 (XMLTV)' },
              { Id: 'xmltv-6-alt', Name: 'CCTV-6 HD (XMLTV)' },
            ],
            Mappings: [
              { TunerChannelId: 'ch-1', ProviderChannelId: serverMappedCh1 },
              { TunerChannelId: 'ch-2', ProviderChannelId: serverMappedCh2 },
            ],
          }
        }
        if (path === '/emby/LiveTv/ChannelMappings' && init?.method === 'POST') {
          const body = JSON.parse(String(init.body || '{}'))
          if (body.TunerChannelId === 'ch-1') {
            serverMappedCh1 = body.ProviderChannelId
          }
          return body
        }
        return undefined
      },
    )

    renderAdmin(<LiveTvTab />)

    fireEvent.click(screen.getByTestId('toggle-channel-mapping-btn'))
    const selectCh1 = (await screen.findByTestId('channel-mapping-select-ch-1')) as HTMLSelectElement
    const selectCh2 = (await screen.findByTestId('channel-mapping-select-ch-2')) as HTMLSelectElement
    expect(selectCh1.value).toBe('xmltv-1')
    expect(selectCh2.value).toBe('xmltv-6')

    // 修改 ch-1 并保存，然后模拟服务端返回新值，验证 draftMappings[ch-1] 已被清除不会遮蔽服务端值
    fireEvent.change(selectCh1, { target: { value: 'xmltv-1-alt' } })
    fireEvent.click(screen.getByTestId('save-channel-mapping-ch-1'))
    await waitFor(() => {
      expect(selectCh1.value).toBe('xmltv-1-alt')
    })

    // 修改 ch-2 但不保存，点击"刷新映射选项"后应重置为服务端映射值
    fireEvent.change(selectCh2, { target: { value: 'xmltv-6-alt' } })
    expect(selectCh2.value).toBe('xmltv-6-alt')

    fireEvent.click(screen.getByTestId('refresh-channel-mappings-btn'))
    await waitFor(() => {
      expect(selectCh2.value).toBe('xmltv-6')
    })
  })
})
