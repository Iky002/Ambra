import fs from 'node:fs'
import path from 'node:path'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api, embyFetch, getBifUrl, getStoredAuth, saveAuth } from '../../api/client'
import type { BaseItemDto, UserDto } from '../../api/types'
import { MediaCard } from '../../components/MediaCard'
import { NotificationBell } from '../../components/NotificationBell'
import { SearchModal } from '../../components/SearchModal'
import {
  clearScrollPositions,
  getScrollPositionsCount,
  ScrollToTop,
} from '../../components/ScrollToTop'
import { IdentifyModal } from '../../components/media/IdentifyModal'
import { AudioPlayerProvider } from '../../context/AudioPlayerContext'
import { AuthProvider, useAuth } from '../../context/AuthContext'
import { setDemoModeOverride } from '../../demo/demoMode'
import { resetDemoStore } from '../../demo/mockServer'
import { buildBifBuffer, parseBif } from '../../player/bifParser'
import { CollectionsPage } from '../CollectionsPage'
import { FavoritesPage } from '../FavoritesPage'
import { ItemDetailPage } from '../ItemDetailPage'
import { LibraryPage } from '../LibraryPage'
import { PersonDetailPage } from '../PersonDetailPage'
import { PlayerPage } from '../PlayerPage'
import { SearchPage } from '../SearchPage'
import { SetupWizardPage } from '../SetupWizardPage'
import { TrendingPage } from '../TrendingPage'
import { DashboardTab } from '../admin/DashboardTab'
import { DevicesKeysTab } from '../admin/DevicesKeysTab'
import { HealthAndAuditTab } from '../admin/HealthAndAuditTab'
import { MetadataTab } from '../admin/MetadataTab'
import { WebhooksTab } from '../admin/WebhooksTab'
import { AdminModal } from '../admin/adminUtils'

function LocationProbe() {
  const loc = useLocation()
  return <div data-testid="loc-probe">{`${loc.pathname}${loc.search}`}</div>
}

function renderWithProviders(
  ui: React.ReactNode,
  initialEntries: string[] = ['/'],
  queryClient?: QueryClient,
) {
  const qc =
    queryClient ??
    new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
  return {
    qc,
    ...render(
      <QueryClientProvider client={qc}>
        <AuthProvider>
          <AudioPlayerProvider>
            <MemoryRouter initialEntries={initialEntries}>{ui}</MemoryRouter>
          </AudioPlayerProvider>
        </AuthProvider>
      </QueryClientProvider>,
    ),
  }
}

const ADMIN_USER: UserDto = {
  Id: 'u-admin',
  Name: 'admin',
  ServerId: 'srv-1',
  Policy: { IsAdministrator: true },
}

const GUEST_USER: UserDto = {
  Id: 'u-guest',
  Name: 'guest',
  ServerId: 'srv-1',
  Policy: { IsAdministrator: false },
}

describe('v0.10.7 Audit 5 Web 全量修复回归测试', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    clearScrollPositions()
    resetDemoStore()
    setDemoModeOverride(false)
    vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => undefined)
    vi.spyOn(HTMLMediaElement.prototype, 'load').mockImplementation(() => undefined)
    vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined)
  })

  afterEach(() => {
    setDemoModeOverride(null)
    localStorage.clear()
    sessionStorage.clear()
    clearScrollPositions()
    vi.restoreAllMocks()
  })

  it('H-15: vercel.json 与 public/_headers 包含完整安全响应头 (CSP/HSTS/Permissions-Policy)', () => {
    const webRoot = path.resolve(__dirname, '../../..')
    const vercelRaw = fs.readFileSync(path.join(webRoot, 'vercel.json'), 'utf8')
    const vercel = JSON.parse(vercelRaw) as {
      headers?: Array<{ source: string; headers: Array<{ key: string; value: string }> }>
    }
    expect(Array.isArray(vercel.headers)).toBe(true)
    const rootRule = vercel.headers?.find((r) => r.source === '/(.*)')
    const headerKeys = (rootRule?.headers || []).map((h) => h.key)
    expect(headerKeys).toEqual(
      expect.arrayContaining([
        'X-Frame-Options',
        'X-Content-Type-Options',
        'Referrer-Policy',
        'Strict-Transport-Security',
        'Permissions-Policy',
        'Content-Security-Policy',
      ]),
    )
    const csp = rootRule?.headers.find((h) => h.key === 'Content-Security-Policy')?.value || ''
    expect(csp).toContain('wasm-unsafe-eval')
    expect(csp).toContain('blob:')

    const cfHeaders = fs.readFileSync(path.join(webRoot, 'public/_headers'), 'utf8')
    expect(cfHeaders).toContain('Content-Security-Policy:')
    expect(cfHeaders).toContain('Strict-Transport-Security:')
    expect(cfHeaders).toContain('Permissions-Policy:')
  })

  it('H-19: 详情页「从头播放」携带 startTicks=0，PlayerPage 显式收到 startTicks=0 不回落到续播进度', async () => {
    saveAuth({
      accessToken: 'tok-1',
      userId: ADMIN_USER.Id,
      serverId: 'srv-1',
      userName: 'admin',
      isAdmin: true,
      deviceId: 'dev-1',
    })
    vi.spyOn(api, 'getUser').mockResolvedValue(ADMIN_USER)
    const movie: BaseItemDto = {
      Id: 'movie-resume',
      Name: '星际穿越',
      Type: 'Movie',
      RunTimeTicks: 7200 * 10_000_000,
      UserData: {
        PlaybackPositionTicks: 1800 * 10_000_000,
        PlayCount: 1,
        IsFavorite: false,
        Played: false,
      },
      MediaSources: [
        {
          Id: 'ms-1',
          Name: '4K HDR',
          Container: 'mp4',
          DirectStreamUrl: '/emby/Videos/movie-resume/stream.mp4',
          SupportsDirectPlay: true,
          MediaStreams: [
            { Type: 'Video', Index: 0, Codec: 'h264', Width: 1920, Height: 1080 },
            { Type: 'Audio', Index: 1, Codec: 'aac', Language: 'chi', IsDefault: true },
          ],
        },
      ],
    }
    vi.spyOn(api, 'getItem').mockResolvedValue(movie)
    vi.spyOn(api, 'getSimilarItems').mockResolvedValue({ Items: [], TotalRecordCount: 0 })
    vi.spyOn(api, 'getLocalTrailers').mockResolvedValue([])
    vi.spyOn(api, 'getSpecialFeatures').mockResolvedValue([])
    vi.spyOn(api, 'getThemeSongs').mockResolvedValue({ Items: [], TotalRecordCount: 0 })

    renderWithProviders(
      <Routes>
        <Route path="/item/:id" element={<ItemDetailPage />} />
        <Route path="/play/:id" element={<LocationProbe />} />
      </Routes>,
      ['/item/movie-resume'],
    )

    const restartBtn = await screen.findByRole('button', { name: /从头播放/ })
    fireEvent.click(restartBtn)

    const loc = await screen.findByTestId('loc-probe')
    expect(loc.textContent).toContain('/play/movie-resume')
    expect(loc.textContent).toContain('startTicks=0')
  })

  it('H-22 & M-4 & M-6 & M-7: PlayerPage 加载 BIF 悬停预览、过滤非法 ?audio=、去重 Stopped 上报、卸载取消字幕与释放 BIF', async () => {
    saveAuth({
      accessToken: 'tok-1',
      userId: ADMIN_USER.Id,
      serverId: 'srv-1',
      userName: 'admin',
      isAdmin: true,
      deviceId: 'dev-1',
    })
    vi.spyOn(api, 'getUser').mockResolvedValue(ADMIN_USER)
    const movie: BaseItemDto = {
      Id: 'movie-bif',
      Name: 'BIF 测试影片',
      Type: 'Movie',
      RunTimeTicks: 100 * 10_000_000,
      UserData: {
        PlaybackPositionTicks: 50 * 10_000_000,
        PlayCount: 0,
        IsFavorite: false,
        Played: false,
      },
      MediaSources: [
        {
          Id: 'ms-bif',
          Name: '1080P MP4',
          Container: 'mp4',
          DirectStreamUrl: '/emby/Videos/movie-bif/stream.mp4',
          SupportsDirectPlay: true,
          MediaStreams: [
            { Type: 'Video', Index: 0, Codec: 'h264', Width: 1920, Height: 1080 },
            { Type: 'Audio', Index: 1, Codec: 'aac', Language: 'chi', IsDefault: true },
            { Type: 'Audio', Index: 2, Codec: 'aac', Language: 'eng', IsDefault: false },
            {
              Type: 'Subtitle',
              Index: 3,
              Codec: 'vtt',
              Language: 'chi',
              IsDefault: true,
              IsExternal: true,
              DeliveryUrl: '/emby/Videos/movie-bif/ms-bif/Subtitles/3/Stream.vtt',
            },
          ],
        },
      ],
    }

    vi.spyOn(api, 'getItem').mockResolvedValue(movie)
    vi.spyOn(api, 'getPlaybackInfo').mockResolvedValue({
      MediaSources: movie.MediaSources!,
      PlaySessionId: 'ps-bif-1',
    })
    vi.spyOn(api, 'reportPlaying').mockResolvedValue(undefined)
    vi.spyOn(api, 'reportProgress').mockResolvedValue(undefined)
    const stoppedSpy = vi.spyOn(api, 'reportStopped').mockResolvedValue(undefined)

    const bifBytes = buildBifBuffer(
      [
        { timestampIndex: 0, data: new Uint8Array([0xff, 0xd8, 0x01, 0xff, 0xd9]) },
        { timestampIndex: 10, data: new Uint8Array([0xff, 0xd8, 0x02, 0xff, 0xd9]) },
      ],
      1000,
    )
    const bifArrayBuf = bifBytes.buffer.slice(bifBytes.byteOffset, bifBytes.byteOffset + bifBytes.byteLength)

    let subtitleAbortSignal: AbortSignal | undefined
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url
      if (url.includes('/index.bif')) {
        return new Response(bifArrayBuf, {
          status: 200,
          headers: { 'Content-Type': 'application/octet-stream' },
        })
      }
      if (url.includes('Stream.vtt')) {
        subtitleAbortSignal = init?.signal ?? undefined
        return new Response('WEBVTT\n\n00:00.000 --> 00:05.000\n你好\n', { status: 200 })
      }
      return new Response('', { status: 404 })
    })

    const createObjSpy = vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:bif-preview-0')
    const revokeObjSpy = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})

    // 显式 startTicks=0 且传入非法 ?audio=-5
    const { container, unmount } = renderWithProviders(
      <Routes>
        <Route path="/play/:id" element={<PlayerPage />} />
      </Routes>,
      ['/play/movie-bif?startTicks=0&audio=-5'],
    )

    // 等待视频元素渲染并检查未因非法 ?audio=-5 报错
    const videoEl = await waitFor(() => {
      const v = container.querySelector('video')
      expect(v).not.toBeNull()
      return v as HTMLVideoElement
    })
    Object.defineProperty(videoEl, 'duration', { configurable: true, value: 100 })
    fireEvent.loadedMetadata(videoEl)
    // H-19: startTicks=0 不回落到 50s 的 PlaybackPositionTicks
    expect(videoEl.currentTime).toBe(0)

    // 悬停进度条展示 BIF 缩略图
    const slider = container.querySelector('input[type="range"]') as HTMLElement | null
    const progressBar = slider?.parentElement ?? null
    expect(progressBar).not.toBeNull()
    vi.spyOn(progressBar!, 'getBoundingClientRect').mockReturnValue({
      left: 0,
      top: 0,
      width: 200,
      height: 10,
      right: 200,
      bottom: 10,
      x: 0,
      y: 0,
      toJSON: () => ({}),
    })
    await waitFor(() => {
      fireEvent.mouseMove(progressBar!, { clientX: 40 })
      expect(screen.getByTestId('bif-preview-tooltip')).toBeInTheDocument()
      expect(screen.getByTestId('bif-preview-image')).toHaveAttribute('src', 'blob:bif-preview-0')
    })
    expect(createObjSpy).toHaveBeenCalled()

    // M-7: 字幕请求携带 AbortSignal
    await waitFor(() => {
      expect(subtitleAbortSignal).toBeDefined()
    })

    // M-6: 连续触发 ended + unmount，只上报一次 Stopped
    fireEvent.ended(videoEl)
    unmount()
    expect(subtitleAbortSignal?.aborted).toBe(true)
    expect(revokeObjSpy).toHaveBeenCalledWith('blob:bif-preview-0')
    expect(stoppedSpy).toHaveBeenCalledTimes(1)
  })

  it('H-23: 非管理员在 /setup 点击「切换管理员登录」先清空登录态再跳转 /login?redirect=/setup&switch=1', async () => {
    saveAuth({
      accessToken: 'tok-guest',
      userId: GUEST_USER.Id,
      serverId: 'srv-1',
      userName: 'guest',
      isAdmin: false,
      deviceId: 'dev-1',
    })
    vi.spyOn(api, 'getUser').mockResolvedValue(GUEST_USER)
    vi.spyOn(api, 'logout').mockResolvedValue(undefined)
    vi.spyOn(api, 'getSetupStatus').mockResolvedValue({
      completed: false,
      has_admin: true,
      server_name: 'Ambra',
    })

    renderWithProviders(
      <Routes>
        <Route path="/setup" element={<SetupWizardPage />} />
        <Route path="/login" element={<LocationProbe />} />
      </Routes>,
      ['/setup'],
    )

    const switchBtn = await screen.findByRole('button', { name: /切换管理员登录/ })
    fireEvent.click(switchBtn)

    const loc = await screen.findByTestId('loc-probe')
    expect(loc.textContent).toBe('/login?redirect=/setup&switch=1')
    expect(getStoredAuth()).toBeNull()
  })

  it('M-1: embyFetch 默认附带 Accept: application/json 且不覆盖调用方自定义 Accept', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockImplementation(
      async () =>
        new Response(JSON.stringify({ ok: true }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
    )

    await embyFetch('/emby/System/Info/Public', {}, false)
    const firstHeaders = fetchSpy.mock.calls[0]?.[1]?.headers as Headers
    expect(firstHeaders.get('Accept')).toBe('application/json')

    await embyFetch(
      '/emby/System/Info/Public',
      { headers: { Accept: 'text/plain' } },
      false,
    )
    const secondHeaders = fetchSpy.mock.calls[1]?.[1]?.headers as Headers
    expect(secondHeaders.get('Accept')).toBe('text/plain')
  })

  it('M-8: PersonDetailPage 对包含原始 % 字符的 id 不抛出 URIError', async () => {
    saveAuth({
      accessToken: 'tok-1',
      userId: ADMIN_USER.Id,
      serverId: 'srv-1',
      userName: 'admin',
      isAdmin: true,
      deviceId: 'dev-1',
    })
    vi.spyOn(api, 'getUser').mockResolvedValue(ADMIN_USER)
    vi.spyOn(api, 'getItem').mockResolvedValue({
      Id: 'person%2',
      Name: '100% 演员',
      Type: 'Person',
    })
    vi.spyOn(api, 'getItems').mockResolvedValue({ Items: [], TotalRecordCount: 0 })

    renderWithProviders(
      <Routes>
        <Route path="/person/:id" element={<PersonDetailPage />} />
      </Routes>,
      ['/person/person%252'],
    )

    expect(await screen.findByText('100% 演员')).toBeInTheDocument()
  })

  it('M-9: SearchPage 输入多关键字尾随空格时不被 URL 同步吞掉', async () => {
    saveAuth({
      accessToken: 'tok-1',
      userId: ADMIN_USER.Id,
      serverId: 'srv-1',
      userName: 'admin',
      isAdmin: true,
      deviceId: 'dev-1',
    })
    vi.spyOn(api, 'getUser').mockResolvedValue(ADMIN_USER)
    vi.spyOn(api, 'getItems').mockResolvedValue({ Items: [], TotalRecordCount: 0 })
    vi.spyOn(api, 'searchHints').mockResolvedValue({ SearchHints: [], TotalRecordCount: 0 })

    const { container } = renderWithProviders(
      <Routes>
        <Route path="/search" element={<SearchPage />} />
      </Routes>,
      ['/search?q=%E6%B5%81%E6%B5%AA'],
    )

    const input = await waitFor(() => {
      const el = container.querySelector('input') as HTMLInputElement | null
      expect(el).not.toBeNull()
      return el as HTMLInputElement
    })
    expect(input.value).toBe('流浪')
    fireEvent.change(input, { target: { value: '流浪 ' } })
    expect(input.value).toBe('流浪 ')
  })

  it('M-12: MetadataTab 展示错误态且兼容 NfoPath；TrendingPage 展示部分榜单 *_error 告警', async () => {
    saveAuth({
      accessToken: 'tok-1',
      userId: ADMIN_USER.Id,
      serverId: 'srv-1',
      userName: 'admin',
      isAdmin: true,
      deviceId: 'dev-1',
    })
    vi.spyOn(api, 'getUser').mockResolvedValue(ADMIN_USER)
    vi.spyOn(api, 'getRankings').mockResolvedValue({
      douban_top250: { chart: 'douban_top250', source: 'douban', count: 250 },
      imdb_top250: { chart: 'imdb_top250', source: 'imdb', count: 250 },
    })
    vi.spyOn(api, 'getItems').mockRejectedValue(new Error('媒体查询超时'))

    const metaView = renderWithProviders(<MetadataTab />)
    expect(await metaView.findByText('无法加载媒体元数据列表')).toBeInTheDocument()
    metaView.unmount()

    vi.spyOn(api, 'getItems').mockResolvedValue({ Items: [], TotalRecordCount: 0 })
    vi.spyOn(api, 'refreshRankings').mockResolvedValue({
      douban_count: 250,
      imdb_count: 0,
      imdb_error: 'IMDb 网络超时',
      matched_items: 5,
      tags_added: 2,
    })

    renderWithProviders(<TrendingPage />)
    const refreshBtn = await screen.findByRole('button', { name: /立即同步热门榜/ })
    fireEvent.click(refreshBtn)
    expect(await screen.findByText(/IMDb 网络超时/)).toBeInTheDocument()
  })

  it('M-13: DashboardTab 活动日志筛选为空页时仍保留分页控件可翻页', async () => {
    vi.spyOn(api, 'getSystemInfo').mockResolvedValue({
      Id: 'srv-1',
      ServerName: 'Ambra',
      Version: '0.10.6',
    })
    vi.spyOn(api, 'getSessions').mockResolvedValue([])
    vi.spyOn(api, 'getActivityLogs').mockResolvedValue({
      Items: [
        {
          Id: 1,
          Name: '普通启动日志',
          Type: 'SystemStarted',
          Date: new Date().toISOString(),
          Severity: 'Info',
        },
      ],
      TotalRecordCount: 40,
    })

    renderWithProviders(<DashboardTab />)
    await screen.findByText('普通启动日志')

    // 输入无匹配关键词，使当前页筛选结果为空
    fireEvent.change(screen.getByLabelText('搜索活动日志'), {
      target: { value: '完全不存在的日志关键词' },
    })
    expect(await screen.findByText('当前页筛选条件下无匹配的活动日志')).toBeInTheDocument()
    // 分页按钮仍然可见且可点下一页
    expect(screen.getByRole('button', { name: '下一页' })).toBeInTheDocument()
  })

  it('M-14: DevicesKeysTab 在非 HTTPS 无 navigator.clipboard 环境下降级到 execCommand("copy")', async () => {
    vi.spyOn(api, 'getDevices').mockResolvedValue({ Items: [], TotalRecordCount: 0 })
    vi.spyOn(api, 'getApiKeys').mockResolvedValue({
      Items: [
        {
          AccessToken: 'ambra-secret-key-lan-01',
          AppName: 'Infuse',
          DateCreated: new Date().toISOString(),
          IsActive: true,
        },
      ],
      TotalRecordCount: 1,
    })

    const origClipboard = navigator.clipboard
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: undefined })
    const execSpy = vi.fn().mockReturnValue(true)
    ;(document as Document & { execCommand: (cmd: string) => boolean }).execCommand = execSpy

    try {
      renderWithProviders(<DevicesKeysTab />)
      const copyBtn = await screen.findByRole('button', { name: '复制' })
      fireEvent.click(copyBtn)
      await waitFor(() => {
        expect(execSpy).toHaveBeenCalledWith('copy')
      })
    } finally {
      Object.defineProperty(navigator, 'clipboard', { configurable: true, value: origClipboard })
    }
  })

  it('M-15: WebhooksTab 编辑时支持显式勾选清除已有 HMAC 密钥，HealthAndAuditTab 切换审校状态丢弃过期响应', async () => {
    setDemoModeOverride(true)
    saveAuth({
      accessToken: 'tok-demo',
      userId: 'user-demo-admin',
      serverId: 'ambra-serverless-demo-01',
      userName: 'admin',
      isAdmin: true,
      deviceId: 'dev-1',
    })

    const saveSpy = vi.spyOn(api, 'saveWebhook')
    renderWithProviders(<WebhooksTab />)

    const editBtn = await screen.findByRole('button', { name: '编辑' })
    fireEvent.click(editBtn)

    const clearCheckbox = await screen.findByLabelText('清除已有 HMAC 签名密钥')
    fireEvent.click(clearCheckbox)
    fireEvent.click(screen.getByRole('button', { name: '保存修改' }))

    await waitFor(() => {
      expect(saveSpy).toHaveBeenCalledWith(
        expect.objectContaining({
          Id: 'wh-demo-01',
          Secret: '',
          ClearSecret: true,
        }),
      )
    })

    // Demo mock 已清除 HasSecret
    const webhooksAfter = await api.getWebhooks()
    expect(webhooksAfter[0].HasSecret).toBe(false)

    // HealthAndAuditTab 竞态防护：快速从 pending 切到 approved，慢速 pending 响应不应覆盖 approved 列表
    setDemoModeOverride(false)
    saveAuth({
      accessToken: 'tok-admin',
      userId: ADMIN_USER.Id,
      serverId: 'srv-1',
      userName: 'admin',
      isAdmin: true,
      deviceId: 'dev-1',
    })
    vi.spyOn(api, 'getUser').mockResolvedValue(ADMIN_USER)
    let resolvePending!: (val: Response) => void
    vi.spyOn(api, 'getBackupInfo').mockResolvedValue({
      BackupDirectory: '/data/backups',
      Backups: [],
    })
    vi.spyOn(globalThis, 'fetch').mockImplementation((input) => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url
      if (url.includes('/api/admin/health')) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              CollectedAt: 1,
              Status: 'healthy',
              GoVersion: 'go1.24',
              NumCpu: 4,
              NumGoroutine: 10,
              Memory: { AllocBytes: 1, TotalAllocBytes: 1, SysBytes: 1, HeapInuseBytes: 1, NumGC: 1 },
              Database: { DbPath: '/db', DbBytes: 1, WalBytes: 0, ShmBytes: 0, TotalBytes: 1 },
              Cache: { ImagesBytes: 1, SubtitlesBytes: 1, TranscodeBytes: 0, TotalBytes: 2 },
              Disk: { Path: '/', TotalBytes: 100, FreeBytes: 50, AvailableBytes: 50, UsedPercent: 50 },
              Library: { TotalItems: 1, MovieCount: 1, SeriesCount: 0, EpisodeCount: 0, UserCount: 1, PendingAudits: 1 },
              Scheduler: { TotalTasks: 1, ActiveTasks: 0, BudgetInUse: 0, BudgetCapacity: 4, BudgetUsagePct: 0 },
            }),
            { status: 200, headers: { 'Content-Type': 'application/json' } },
          ),
        )
      }
      if (url.includes('/api/admin/governance/review?status=pending')) {
        return new Promise<Response>((resolve) => {
          resolvePending = resolve
        })
      }
      if (url.includes('/api/admin/governance/review?status=approved')) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              Items: [
                {
                  Id: 'rev-approved',
                  ItemId: 'm-1',
                  ItemName: '已通过条目',
                  ItemType: 'Movie',
                  Path: '/a.mkv',
                  Confidence: 0.9,
                  Candidates: [],
                  Status: 'approved',
                  CreatedAt: 1,
                  UpdatedAt: 2,
                },
              ],
              TotalRecordCount: 1,
            }),
            { status: 200, headers: { 'Content-Type': 'application/json' } },
          ),
        )
      }
      return Promise.resolve(new Response('{}', { status: 200, headers: { 'Content-Type': 'application/json' } }))
    })

    const { container: healthContainer } = renderWithProviders(<HealthAndAuditTab />)
    const statusSelect = await waitFor(() => {
      const sel = healthContainer.querySelector('select') as HTMLSelectElement | null
      expect(sel).not.toBeNull()
      return sel as HTMLSelectElement
    })
    fireEvent.change(statusSelect, { target: { value: 'approved' } })
    expect(await screen.findByText('已通过条目')).toBeInTheDocument()

    // 迟到的 pending 响应不得覆盖当前的 approved 列表
    await act(async () => {
      resolvePending(
        new Response(
          JSON.stringify({
            Items: [
              {
                Id: 'rev-stale',
                ItemId: 'm-2',
                ItemName: '过期滞后条目',
                ItemType: 'Movie',
                Path: '/b.mkv',
                Confidence: 0.5,
                Candidates: [],
                Status: 'pending',
                CreatedAt: 1,
                UpdatedAt: 2,
              },
            ],
            TotalRecordCount: 1,
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        ),
      )
    })
    expect(screen.getByText('已通过条目')).toBeInTheDocument()
    expect(screen.queryByText('过期滞后条目')).toBeNull()
  })

  it('M-16: AuthContext 登录后不立即多发冗余 /Users/{id}，且未通过服务端验证前 isAdmin 默认为 false', async () => {
    // 1. 仅篡改 localStorage isAdmin:true 且 getUser 未完成时，isAdmin 应为 false
    saveAuth({
      accessToken: 'tok-spoof',
      userId: 'u-spoof',
      serverId: 'srv-1',
      userName: 'spoof',
      isAdmin: true,
      deviceId: 'dev-1',
    })
    const getUserSpy = vi.spyOn(api, 'getUser').mockReturnValue(new Promise(() => {}))

    function AuthProbe() {
      const { isAdmin, login } = useAuth()
      return (
        <div>
          <span data-testid="is-admin">{String(isAdmin)}</span>
          <button type="button" onClick={() => void login('admin', 'pw')}>
            do-login
          </button>
        </div>
      )
    }

    const { unmount } = renderWithProviders(<AuthProbe />)
    expect(screen.getByTestId('is-admin').textContent).toBe('false')
    unmount()

    // 2. 通过 login() 登录后，直接使用认证响应中的 User，不再立即多发一次 getUser
    localStorage.clear()
    getUserSpy.mockClear()
    vi.spyOn(api, 'authenticateByName').mockResolvedValue({
      User: ADMIN_USER,
      AccessToken: 'tok-fresh',
      ServerId: 'srv-1',
    })
    renderWithProviders(<AuthProbe />)
    fireEvent.click(screen.getByRole('button', { name: 'do-login' }))
    await waitFor(() => {
      expect(screen.getByTestId('is-admin').textContent).toBe('true')
    })
    expect(getUserSpy).not.toHaveBeenCalled()
  })

  it('M-17: FavoritesPage/CollectionsPage 展示 TotalRecordCount 与加载更多；LibraryPage 透传 Studios', async () => {
    saveAuth({
      accessToken: 'tok-1',
      userId: ADMIN_USER.Id,
      serverId: 'srv-1',
      userName: 'admin',
      isAdmin: true,
      deviceId: 'dev-1',
    })
    vi.spyOn(api, 'getUser').mockResolvedValue(ADMIN_USER)
    const getItemsSpy = vi.spyOn(api, 'getItems').mockResolvedValue({
      Items: [{ Id: 'm-1', Name: '电影一', Type: 'Movie' } as BaseItemDto],
      TotalRecordCount: 80,
    })

    const favView = renderWithProviders(<FavoritesPage />)
    expect(await favView.findByRole('button', { name: /加载更多（已显示 1 \/ 80）/ })).toBeInTheDocument()
    favView.unmount()

    const colView = renderWithProviders(<CollectionsPage />)
    expect(await colView.findByRole('button', { name: /加载更多（已显示 1 \/ 80）/ })).toBeInTheDocument()
    colView.unmount()

    getItemsSpy.mockClear()
    vi.spyOn(api, 'getItem').mockResolvedValue({
      Id: 'lib-1',
      Name: '电影',
      Type: 'CollectionFolder',
      CollectionType: 'movies',
    })
    vi.spyOn(api, 'getGenres').mockResolvedValue({ Items: [], TotalRecordCount: 0 })
    vi.spyOn(api, 'getYears').mockResolvedValue({ Items: [], TotalRecordCount: 0 })
    vi.spyOn(api, 'getTags').mockResolvedValue({ Items: [], TotalRecordCount: 0 })

    renderWithProviders(
      <Routes>
        <Route path="/library/:id" element={<LibraryPage />} />
      </Routes>,
      ['/library/lib-1?studio=A24'],
    )

    await waitFor(() => {
      expect(getItemsSpy).toHaveBeenCalledWith(
        ADMIN_USER.Id,
        expect.objectContaining({ studios: 'A24' }),
      )
    })
  })

  it('M-19: Demo Mock 同步 v0.11.2 版本号、默认隐藏公开用户列表、ChapterCachePath/BifCachePath、264MB 缓存、/Attachments 与 /index.bif', async () => {
    setDemoModeOverride(true)
    // 默认 EnableLoginUserList=false，匿名获取公开用户列表应返回空数组 []
    const pubBefore = await api.getPublicUsers()
    expect(pubBefore).toEqual([])

    const authRes = await api.authenticateByName('admin', '')
    saveAuth({
      accessToken: authRes.AccessToken,
      userId: authRes.User.Id,
      serverId: authRes.ServerId,
      userName: authRes.User.Name,
      isAdmin: true,
      deviceId: 'dev-demo',
    })

    // 管理员开启 EnableLoginUserList=true 后，公开用户列表返回脱敏的非隐藏用户
    await api.updateServerConfig({ EnableLoginUserList: true })
    const pubAfter = await api.getPublicUsers()
    expect(pubAfter.length).toBeGreaterThan(0)
    expect(pubAfter.some((u) => u.Name === 'admin')).toBe(true)
    expect(pubAfter[0].Policy?.IsAdministrator).toBeUndefined()
    await api.updateServerConfig({ EnableLoginUserList: false })

    const sysInfo = await api.getSystemInfo()
    expect(sysInfo.Version).toBe('0.11.2-demo')
    expect(sysInfo.ChapterCachePath).toBe('/var/lib/ambra/cache/chapters')
    expect(sysInfo.BifCachePath).toBe('/var/lib/ambra/cache/bif')

    const storage = await embyFetch<{ CacheBytes: number }>('/api/admin/governance/storage')
    expect(storage.CacheBytes).toBe(264 * 1024 * 1024)

    const attachments = await api.getMediaAttachments('item-wandering-earth-2', 'item-wandering-earth-2-src-4k')
    expect(attachments).toEqual([])

    const bifUrl = getBifUrl('item-wandering-earth-2', 320)
    expect(bifUrl).toContain('/emby/Videos/item-wandering-earth-2/index.bif')
    const bifRaw = await embyFetch<ArrayBuffer>('/emby/Videos/item-wandering-earth-2/index.bif?Width=320')
    const parsedBif = parseBif(bifRaw)
    expect(parsedBif.frameCount).toBe(5)

    const thumbSet = await api.getThumbnailSet('item-wandering-earth-2', 320)
    expect(thumbSet.Thumbnails.length).toBe(5)

    // 确认不再有假 pgssub 指向 VTT
    const pb = await api.getPlaybackInfo('item-wandering-earth-2', authRes.User.Id)
    const subStreams = (pb.MediaSources[0].MediaStreams || []).filter((s) => s.Type === 'Subtitle')
    expect(subStreams.some((s) => s.Codec === 'pgssub' && s.DeliveryUrl?.startsWith('data:text/vtt'))).toBe(false)
    expect(subStreams.some((s) => s.SubVariant === 'cht' && s.Codec === 'vtt')).toBe(true)
  })

  it('Low 项修复：MediaCard 无嵌套按钮、AdminModal/SearchModal 无障碍与 Escape、NotificationBell 安全跳转、ScrollToTop LRU 上限、IdentifyModal 年份校验', async () => {
    // 1. MediaCard: <Link> 内不嵌套 <button>
    const { container: cardContainer, unmount: unmountCard } = renderWithProviders(
      <MediaCard
        item={{ Id: 'm-card', Name: '卡片测试', Type: 'Movie', ProductionYear: 2024 }}
        onQuickAction={() => {}}
      />,
    )
    expect(cardContainer.querySelector('a button')).toBeNull()
    unmountCard()

    // 2. AdminModal dialog ARIA
    const { unmount: unmountModal } = render(
      <AdminModal open title="测试弹窗" onClose={() => {}}>
        <div>body</div>
      </AdminModal>,
    )
    const dlg = screen.getByRole('dialog')
    expect(dlg).toHaveAttribute('aria-modal', 'true')
    expect(screen.getByRole('button', { name: '关闭弹窗' })).toBeInTheDocument()
    unmountModal()

    // 3. SearchModal dialog ARIA + window Escape
    const onCloseSearch = vi.fn()
    const { unmount: unmountSearch } = renderWithProviders(
      <SearchModal open onClose={onCloseSearch} />,
    )
    expect(screen.getByRole('dialog')).toHaveAttribute('aria-modal', 'true')
    fireEvent.keyDown(window, { key: 'Escape' })
    expect(onCloseSearch).toHaveBeenCalledTimes(1)
    unmountSearch()

    // 4. NotificationBell 安全站内跳转（阻止外链 //evil.com）
    saveAuth({
      accessToken: 'tok-1',
      userId: ADMIN_USER.Id,
      serverId: 'srv-1',
      userName: 'admin',
      isAdmin: true,
      deviceId: 'dev-1',
    })
    vi.spyOn(api, 'getUser').mockResolvedValue(ADMIN_USER)
    vi.spyOn(api, 'getNotificationsSummary').mockResolvedValue({ UnreadCount: 1 })
    vi.spyOn(api, 'getNotifications').mockResolvedValue({
      Notifications: [
        {
          Id: 'n-safe',
          Date: new Date().toISOString(),
          IsRead: false,
          Name: '打开管理后台',
          Url: '/admin',
          Level: 'Normal',
        },
        {
          Id: 'n-evil',
          Date: new Date().toISOString(),
          IsRead: true,
          Name: '恶意跳转',
          Url: '//evil.example/phish',
          Level: 'Warning',
        },
      ],
      TotalRecordCount: 2,
    })
    vi.spyOn(api, 'markNotificationsRead').mockResolvedValue(undefined)

    const { unmount: unmountBell } = renderWithProviders(
      <Routes>
        <Route
          path="*"
          element={
            <>
              <NotificationBell />
              <LocationProbe />
            </>
          }
        />
      </Routes>,
      ['/'],
    )
    fireEvent.click(await screen.findByTestId('notification-bell-button'))
    const dropdown = await screen.findByTestId('notification-dropdown')
    fireEvent.click(await within(dropdown).findByTestId('notification-item-n-evil'))
    expect(screen.getByTestId('loc-probe').textContent).toBe('/')
    fireEvent.click(await within(dropdown).findByTestId('notification-item-n-safe'))
    await waitFor(() => {
      expect(screen.getByTestId('loc-probe').textContent).toBe('/admin')
    })
    unmountBell()

    // 5. ScrollToTop LRU 容量限制 <= 100
    vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
    for (let i = 0; i < 120; i++) {
      const { unmount } = render(
        <MemoryRouter initialEntries={[`/page-${i}`]}>
          <ScrollToTop />
        </MemoryRouter>,
      )
      unmount()
    }
    expect(getScrollPositionsCount()).toBeLessThanOrEqual(100)

    // 6. IdentifyModal 年份校验 > 1800
    const remoteSearchSpy = vi.spyOn(api, 'remoteSearch').mockResolvedValue([])
    renderWithProviders(
      <IdentifyModal
        open
        item={{ Id: 'm-id', Name: '测试影片', Type: 'Movie' }}
        onClose={() => {}}
      />,
    )
    const yearInput = screen.getByPlaceholderText('例如：2010')
    fireEvent.change(yearInput, { target: { value: '1750' } })
    fireEvent.click(screen.getByRole('button', { name: /搜索候选匹配/ }))
    expect(await screen.findByText(/请输入有效的发行年份/)).toBeInTheDocument()
    expect(remoteSearchSpy).not.toHaveBeenCalled()
  })
})
