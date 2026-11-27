import fs from 'node:fs'
import path from 'node:path'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import Hls from 'hls.js'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api, getStoredAuth, saveAuth } from '../../api/client'
import type { BaseItemDto, PluginInfo, UserDto } from '../../api/types'
import { Layout } from '../../components/Layout'
import { MediaCard } from '../../components/MediaCard'
import { MergeVersionsModal } from '../../components/media/MergeVersionsModal'
import { MetadataEditorModal } from '../../components/media/MetadataEditorModal'
import { AudioPlayerProvider } from '../../context/AudioPlayerContext'
import { AuthProvider, useAuth } from '../../context/AuthContext'
import { setDemoModeOverride } from '../../demo/demoMode'
import { resetDemoStore } from '../../demo/mockServer'
import { I18nProvider } from '../../i18n/I18nContext'
import { buildBifBuffer } from '../../player/bifParser'
import { PgsCanvasRenderer } from '../../player/pgsRenderer'
import {
  savePlaybackPrefs,
  syncPlaybackPrefsFromUserConfig,
} from '../../player/playbackPrefs'
import { StreamMkvRemuxer } from '../../player/streamMkvRemuxer'
import { HomePage } from '../HomePage'
import { ItemDetailPage } from '../ItemDetailPage'
import { LibraryPage } from '../LibraryPage'
import { LiveTvPage } from '../LiveTvPage'
import { PlayerPage } from '../PlayerPage'
import { ProfilePage } from '../ProfilePage'
import { LibrariesTab } from '../admin/LibrariesTab'
import { MetadataTab } from '../admin/MetadataTab'
import { MigrationTab } from '../admin/MigrationTab'
import { PluginsTab } from '../admin/PluginsTab'
import { UsersTab } from '../admin/UsersTab'

function createQc() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
}

function renderWithProviders(
  ui: React.ReactNode,
  initialEntries: string[] = ['/'],
  queryClient?: QueryClient,
) {
  const qc = queryClient ?? createQc()
  return {
    qc,
    ...render(
      <I18nProvider initialLocale="zh-CN">
        <QueryClientProvider client={qc}>
          <AuthProvider>
            <AudioPlayerProvider>
              <MemoryRouter initialEntries={initialEntries}>{ui}</MemoryRouter>
            </AudioPlayerProvider>
          </AuthProvider>
        </QueryClientProvider>
      </I18nProvider>,
    ),
  }
}

const ADMIN_USER: UserDto = {
  Id: 'u-admin',
  Name: 'admin',
  ServerId: 'srv-1',
  Policy: { IsAdministrator: true, EnableContentDeletion: true },
}

const GUEST_USER: UserDto = {
  Id: 'u-guest',
  Name: 'guest',
  ServerId: 'srv-1',
  Policy: { IsAdministrator: false },
}

describe('v0.10.9 Stream 5 全量修复回归测试', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
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
    vi.restoreAllMocks()
  })

  it('H-17: AuthContext 并发切号/登出丢弃过期 getUser 响应，且非 401 网络错误不清空已验证 user', async () => {
    saveAuth({
      accessToken: 'tok-user-a',
      userId: 'u-a',
      serverId: 'srv-1',
      userName: 'UserA',
      isAdmin: true,
      deviceId: 'dev-1',
    })

    let resolveStaleUserA!: (u: UserDto) => void
    const getUserSpy = vi.spyOn(api, 'getUser').mockImplementationOnce(
      () =>
        new Promise<UserDto>((resolve) => {
          resolveStaleUserA = resolve
        }),
    )

    function AuthProbe() {
      const { auth, user, isAdmin, userError, applyAuthResult, refreshUser, logout } = useAuth()
      return (
        <div>
          <span data-testid="auth-token">{auth?.accessToken ?? 'none'}</span>
          <span data-testid="user-name">{user?.Name ?? 'null'}</span>
          <span data-testid="is-admin">{String(isAdmin)}</span>
          <span data-testid="user-error">{userError ?? ''}</span>
          <button
            type="button"
            onClick={() =>
              applyAuthResult({
                AccessToken: 'tok-user-b',
                ServerId: 'srv-1',
                User: GUEST_USER,
              })
            }
          >
            switch-to-guest
          </button>
          <button type="button" onClick={() => void refreshUser()}>
            do-refresh
          </button>
          <button type="button" onClick={logout}>
            do-logout
          </button>
        </div>
      )
    }

    renderWithProviders(<AuthProbe />)

    // 1) 在途 getUser(u-a) 期间切号为 guest
    fireEvent.click(screen.getByRole('button', { name: 'switch-to-guest' }))
    expect(screen.getByTestId('user-name').textContent).toBe('guest')
    expect(screen.getByTestId('is-admin').textContent).toBe('false')
    expect(screen.getByTestId('auth-token').textContent).toBe('tok-user-b')

    // 迟到的 UserA (admin) 响应到达 -> 必须丢弃，不得串号或提权或回滚 Token
    await act(async () => {
      resolveStaleUserA({
        Id: 'u-a',
        Name: 'UserA-Admin',
        ServerId: 'srv-1',
        Policy: { IsAdministrator: true },
      })
      await Promise.resolve()
    })
    expect(screen.getByTestId('user-name').textContent).toBe('guest')
    expect(screen.getByTestId('is-admin').textContent).toBe('false')
    expect(getStoredAuth()?.accessToken).toBe('tok-user-b')

    // 2) 非 401 网络/500 错误：保留已验证的 guest user 并记录 userError
    getUserSpy.mockRejectedValueOnce(new Error('500 Internal Server Error'))
    fireEvent.click(screen.getByRole('button', { name: 'do-refresh' }))
    await waitFor(() => {
      expect(screen.getByTestId('user-error').textContent).toContain('500 Internal Server Error')
    })
    expect(screen.getByTestId('user-name').textContent).toBe('guest')
    expect(getStoredAuth()?.accessToken).toBe('tok-user-b')

    // 3) 随后重试 refreshUser 成功 -> 清除 userError
    getUserSpy.mockResolvedValueOnce({ ...GUEST_USER, Name: 'guest-updated' })
    fireEvent.click(screen.getByRole('button', { name: 'do-refresh' }))
    await waitFor(() => {
      expect(screen.getByTestId('user-name').textContent).toBe('guest-updated')
      expect(screen.getByTestId('user-error').textContent).toBe('')
    })

    // 4) 登出竞态：登出前发起的 refreshUser 迟到返回也不得复活会话
    let resolveBeforeLogout!: (u: UserDto) => void
    getUserSpy.mockImplementationOnce(
      () =>
        new Promise<UserDto>((resolve) => {
          resolveBeforeLogout = resolve
        }),
    )
    vi.spyOn(api, 'logout').mockResolvedValue(undefined)
    fireEvent.click(screen.getByRole('button', { name: 'do-refresh' }))
    fireEvent.click(screen.getByRole('button', { name: 'do-logout' }))
    await act(async () => {
      resolveBeforeLogout(ADMIN_USER)
      await Promise.resolve()
    })
    expect(screen.getByTestId('user-name').textContent).toBe('null')
    expect(getStoredAuth()).toBeNull()
  })

  it('H-18 & H-19 & M-45: CSP media-src 包含 data:，vite.config.ts 代理 /api 且支持 VITE_BASE_PATH 兼容 GitHub Pages 二级子路径与 Vercel 根路径', async () => {
    const webRoot = path.resolve(__dirname, '../../..')
    const repoRoot = path.resolve(webRoot, '..')
    const vercel = JSON.parse(fs.readFileSync(path.join(webRoot, 'vercel.json'), 'utf8')) as {
      buildCommand?: string
      headers: Array<{ source: string; headers: Array<{ key: string; value: string }> }>
    }
    expect(vercel.buildCommand).toBe('VITE_BASE_PATH=/ npm run build:demo')
    const vercelCsp =
      vercel.headers
        .find((r) => r.source === '/(.*)')
        ?.headers.find((h) => h.key === 'Content-Security-Policy')?.value || ''
    expect(vercelCsp).toContain("media-src 'self' data: blob: https: http:;")

    const cfHeaders = fs.readFileSync(path.join(webRoot, 'public/_headers'), 'utf8')
    expect(cfHeaders).toContain("media-src 'self' data: blob: https: http:;")

    const viteConfigSrc = fs.readFileSync(path.join(webRoot, 'vite.config.ts'), 'utf8')
    expect(viteConfigSrc).toContain("'/api'")
    expect(viteConfigSrc).toContain("target: 'http://localhost:8096'")
    expect(viteConfigSrc).toContain('VITE_BASE_PATH')
    expect(viteConfigSrc).toContain("'./'")
    expect(viteConfigSrc).not.toContain("isDemoBuild ? '/' : './'")

    const pagesWorkflow = fs.readFileSync(
      path.join(repoRoot, '.github/workflows/pages.yml'),
      'utf8',
    )
    expect(pagesWorkflow).toContain('VITE_BASE_PATH: /${{ github.event.repository.name }}/')

    const distHtmlPath = path.join(webRoot, 'dist/index.html')
    if (fs.existsSync(distHtmlPath)) {
      const distHtml = fs.readFileSync(distHtmlPath, 'utf8')
      expect(distHtml).toContain('src="./assets/')
    }
  })

  it('H-20 & M-47: PlayerPage 首播非默认偏好音轨零二次重建、切换音轨保留 BIF、上报 effectiveAudioIndex、NaN/Infinity duration 快进不归零', async () => {
    saveAuth({
      accessToken: 'tok-p',
      userId: ADMIN_USER.Id,
      serverId: 'srv-1',
      userName: 'admin',
      isAdmin: true,
      deviceId: 'dev-p',
    })
    savePlaybackPrefs({
      preferredAudioLang: 'jpn',
      preferredSubtitleLang: 'off',
    })
    vi.spyOn(api, 'getUser').mockResolvedValue(ADMIN_USER)
    vi.spyOn(api, 'getItem').mockResolvedValue({
      Id: 'movie-h20',
      Name: '双音轨 BIF 测试片',
      Type: 'Movie',
      RunTimeTicks: 3600 * 10_000_000,
    })

    const getPlaybackInfoSpy = vi.spyOn(api, 'getPlaybackInfo').mockResolvedValue({
      PlaySessionId: 'ps-h20',
      MediaSources: [
        {
          Id: 'ms-h20',
          Container: 'mp4',
          RunTimeTicks: 3600 * 10_000_000,
          SupportsDirectPlay: true,
          DefaultAudioStreamIndex: 1,
          MediaStreams: [
            { Index: 0, Type: 'Video', Codec: 'h264', Width: 1920, Height: 1080 },
            { Index: 1, Type: 'Audio', Codec: 'aac', Language: 'eng', DisplayTitle: 'English' },
            { Index: 2, Type: 'Audio', Codec: 'aac', Language: 'jpn', DisplayTitle: '日本語' },
          ],
        },
      ],
    })
    const reportPlayingSpy = vi.spyOn(api, 'reportPlaying').mockResolvedValue(undefined)
    vi.spyOn(api, 'reportProgress').mockResolvedValue(undefined)
    vi.spyOn(api, 'reportStopped').mockResolvedValue(undefined)

    const bifBytes = buildBifBuffer(
      [{ timestampIndex: 0, data: new Uint8Array([0xff, 0xd8, 0x01, 0xff, 0xd9]) }],
      1000,
    )
    const bifArrayBuf = bifBytes.buffer.slice(
      bifBytes.byteOffset,
      bifBytes.byteOffset + bifBytes.byteLength,
    )
    let bifFetchCount = 0
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url
      if (url.includes('/index.bif')) {
        bifFetchCount += 1
        return new Response(bifArrayBuf, { status: 200 })
      }
      return new Response('', { status: 404 })
    })
    vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:bif-h20')
    const revokeSpy = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})

    const { container } = renderWithProviders(
      <Routes>
        <Route path="/play/:id" element={<PlayerPage />} />
      </Routes>,
      ['/play/movie-h20'],
    )

    const video = container.querySelector('video') as HTMLVideoElement
    await waitFor(() => {
      expect(video.src).toContain('AudioStreamIndex=2')
      expect(bifFetchCount).toBe(1)
    })

    // H-20: 首播偏好命中非默认音轨，不触发第二次 PlaybackInfo
    expect(getPlaybackInfoSpy).toHaveBeenCalledTimes(1)

    // M-47: 首帧 reportPlaying 使用 effectiveAudioIndex (2)
    expect(reportPlayingSpy).toHaveBeenCalledWith(
      expect.objectContaining({ AudioStreamIndex: 2 }),
    )

    // M-47: video.duration 为 NaN 时快进 10 秒应基于 effectiveDurationSec (3600s)，而非归零
    Object.defineProperty(video, 'duration', { configurable: true, value: Number.NaN })
    video.currentTime = 45
    fireEvent.click(screen.getByTitle('快进 10 秒 (→)'))
    expect(video.currentTime).toBe(55)

    // Infinity duration 同样不归零
    Object.defineProperty(video, 'duration', { configurable: true, value: Number.POSITIVE_INFINITY })
    fireEvent.click(screen.getByTitle('快进 10 秒 (→)'))
    expect(video.currentTime).toBe(65)

    // H-20: 手动切换音轨触发 PlaybackInfo 重新请求时，不重新拉取 index.bif 也不释放已有 BIF
    revokeSpy.mockClear()
    fireEvent.click(screen.getByTitle('切换音轨 (A)'))
    fireEvent.click(await screen.findByRole('button', { name: /English/ }))
    await waitFor(() => {
      expect(video.src).toContain('AudioStreamIndex=1')
    })
    expect(bifFetchCount).toBe(1)
    expect(revokeSpy).not.toHaveBeenCalled()
  })

  it('H-21 & M-42 & L-33: PluginsTab loadSeqRef 竞态防护、社区插件保留字段、10 语言下拉、弹窗内联报错；UsersTab 弹窗内联报错', async () => {
    const plugins: PluginInfo[] = [
      {
        Id: 'ambra.builtin.tmdb',
        Name: 'TMDB Scraper',
        BuiltIn: true,
        Enabled: true,
        Priority: 10,
        Kinds: ['metadata'],
      },
      {
        Id: 'community.custom.notifier',
        Name: 'Community Webhook Bot',
        BuiltIn: false,
        Enabled: true,
        Priority: 50,
        Kinds: ['notifier'],
      },
    ]
    vi.spyOn(api, 'getPlugins').mockResolvedValue(plugins)

    let resolveTmdb!: (cfg: Record<string, unknown>) => void
    vi.spyOn(api, 'getPluginConfiguration').mockImplementation((id) => {
      if (id === 'ambra.builtin.tmdb') {
        return new Promise((resolve) => {
          resolveTmdb = resolve
        })
      }
      return Promise.resolve({
        enabled: true,
        priority: 50,
        api_key: 'comm-secret',
        language: 'es-ES',
        enable_302: false,
      })
    })

    const { unmount } = renderWithProviders(<PluginsTab />)
    await screen.findByText('TMDB Scraper')

    // 先点 TMDB（慢响应），紧接着关闭并点社区插件（快响应）
    const editButtons = screen.getAllByRole('button', { name: '编辑配置' })
    fireEvent.click(editButtons[0])
    fireEvent.click(screen.getByRole('button', { name: '关闭弹窗' }))
    fireEvent.click(editButtons[1])

    const textarea = await waitFor(() => {
      const el = document.querySelector('textarea') as HTMLTextAreaElement | null
      expect(el).not.toBeNull()
      expect(el!.value).toContain('"api_key": "comm-secret"')
      return el!
    })
    // H-21: 社区插件保留 api_key / language / enable_302
    expect(textarea.value).toContain('"language": "es-ES"')
    expect(textarea.value).toContain('"enable_302": false')

    // H-21: 迟到的 TMDB 响应到达 -> 被 loadSeqRef 丢弃，不污染社区插件表单
    await act(async () => {
      resolveTmdb({ enabled: false, priority: 99, api_key: '********', language: 'ja-JP' })
      await Promise.resolve()
    })
    expect((document.querySelector('textarea') as HTMLTextAreaElement).value).toContain(
      '"api_key": "comm-secret"',
    )

    // M-42: JSON 语法错误在弹窗表单内可见
    fireEvent.change(textarea, { target: { value: '{bad json' } })
    fireEvent.click(screen.getByRole('button', { name: '保存配置' }))
    const form = screen.getByRole('button', { name: '保存配置' }).closest('form')!
    expect(await within(form).findByText('高级 JSON 配置语法错误，请检查格式')).toBeInTheDocument()

    // 关闭弹窗再打开 TMDB 验证 M-42 清空错误 & L-33 10 种语言 & 保留中文脱敏占位符
    fireEvent.click(screen.getByRole('button', { name: '取消' }))
    vi.spyOn(api, 'getPluginConfiguration').mockResolvedValueOnce({
      enabled: true,
      priority: 10,
      api_key: '********',
      language: 'zh-CN',
    })
    fireEvent.click(screen.getAllByRole('button', { name: '编辑配置' })[0])
    const tmdbInput = (await screen.findByLabelText('TMDB API Key')) as HTMLInputElement
    expect(tmdbInput.placeholder).toContain('已配置密钥，留空保留原值')
    expect(screen.queryByText('高级 JSON 配置语法错误，请检查格式')).toBeNull()

    const langSelect = document.querySelector('select') as HTMLSelectElement
    expect(Array.from(langSelect.options).map((o) => o.value)).toEqual([
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
    unmount()

    // M-42: UsersTab 弹窗内显示错误
    saveAuth({
      accessToken: 'tok-admin',
      userId: ADMIN_USER.Id,
      serverId: 'srv-1',
      userName: 'admin',
      isAdmin: true,
      deviceId: 'dev-1',
    })
    vi.spyOn(api, 'getUser').mockResolvedValue(ADMIN_USER)
    vi.spyOn(api, 'getUsers').mockResolvedValue([ADMIN_USER])
    vi.spyOn(api, 'getVirtualFolders').mockResolvedValue([])
    vi.spyOn(api, 'getParentalRatings').mockResolvedValue([])
    vi.spyOn(api, 'createUser').mockRejectedValue(new Error('用户名已被占用'))

    renderWithProviders(<UsersTab />)
    fireEvent.click(await screen.findByRole('button', { name: /新建用户/ }))
    const nameInput = await screen.findByPlaceholderText('例如: alice')
    fireEvent.change(nameInput, { target: { value: 'admin' } })
    fireEvent.click(screen.getByRole('button', { name: '确认创建' }))
    const createForm = screen.getByRole('button', { name: '确认创建' }).closest('form')!
    expect(await within(createForm).findByText('用户名已被占用')).toBeInTheDocument()
  })

  it('M-41 & M-51: StreamMkvRemuxer 旧分块不覆写 Seek 偏移；PgsCanvasRenderer 取消/并发后丢弃过期解析', async () => {
    // M-41: 验证 startPumpLoop 在 fetchRange 返回后立即检查 generation，不覆写 currentByteOffset
    const remuxer = new StreamMkvRemuxer()
    const internals = remuxer as unknown as {
      pumpGeneration: number
      currentByteOffset: number
      totalBytes: number
      chunkSizeBytes: number
      maxBufferAheadSec: number
      disposed: boolean
      fetchRange: (start: number, end: number, signal?: AbortSignal) => Promise<{
        data: Uint8Array
        totalBytes: number
      }>
      startPumpLoop: (generation: number) => Promise<void>
    }
    internals.pumpGeneration = 1
    internals.currentByteOffset = 100
    internals.totalBytes = 10000
    internals.chunkSizeBytes = 500
    internals.maxBufferAheadSec = 30

    let resolveChunk!: (val: { data: Uint8Array; totalBytes: number }) => void
    internals.fetchRange = vi.fn(
      () =>
        new Promise((resolve) => {
          resolveChunk = resolve
        }),
    )

    const loopPromise = internals.startPumpLoop(1)
    await Promise.resolve()

    // 模拟 Seek 发生：generation 递增且 currentByteOffset 跳转到新位置 5000
    internals.pumpGeneration = 2
    internals.currentByteOffset = 5000

    // 旧的 fetchRange 返回（未抛 AbortError，恰在微任务窗口 resolve）
    resolveChunk({ data: new Uint8Array(500), totalBytes: 10000 })
    await loopPromise

    // currentByteOffset 必须保持 5000，不被旧分块覆写为 100 + 500 = 600
    expect(internals.currentByteOffset).toBe(5000)

    // M-51: PgsCanvasRenderer.loadFromUrl 并发取消后丢弃过期 arrayBuffer
    const canvas = document.createElement('canvas')
    const clearRectSpy = vi.fn()
    vi.spyOn(canvas, 'getContext').mockReturnValue({
      clearRect: clearRectSpy,
      drawImage: vi.fn(),
    } as unknown as CanvasRenderingContext2D)

    const renderer = new PgsCanvasRenderer(canvas)
    let resolveSlowBuf!: (buf: ArrayBuffer) => void
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce({
      ok: true,
      status: 200,
      arrayBuffer: () =>
        new Promise<ArrayBuffer>((resolve) => {
          resolveSlowBuf = resolve
        }),
    } as unknown as Response)

    const loadPromise = renderer.loadFromUrl('/sub-slow.sup')
    await Promise.resolve()
    renderer.cancel()
    clearRectSpy.mockClear()

    resolveSlowBuf(new ArrayBuffer(16))
    const count = await loadPromise
    expect(count).toBe(0)
    expect(clearRectSpy).not.toHaveBeenCalled()
  })

  it('M-43 & L-36: MetadataEditorModal 保存锁定不重置未保存编辑；MergeVersionsModal 合并期间禁用候选按钮', async () => {
    const itemV1: BaseItemDto = {
      Id: 'm-edit-1',
      Name: '原始片名',
      Type: 'Movie',
      LockedFields: [],
      LockData: false,
    }
    vi.spyOn(api, 'lockItemFields').mockResolvedValue(undefined)

    const qc = createQc()
    const { rerender } = render(
      <QueryClientProvider client={qc}>
        <MetadataEditorModal open item={itemV1} onClose={() => undefined} />
      </QueryClientProvider>,
    )

    const titleInput = screen.getByPlaceholderText('例如：肖申克的救赎') as HTMLInputElement
    expect(titleInput.value).toBe('原始片名')
    fireEvent.change(titleInput, { target: { value: '用户未保存的新片名' } })

    fireEvent.click(screen.getByRole('button', { name: '保存锁定配置' }))
    await screen.findByText(/已保存字段锁定/)

    // 模拟 invalidateQueries 后父组件传入同一 Id 的新 item 引用
    rerender(
      <QueryClientProvider client={qc}>
        <MetadataEditorModal
          open
          item={{ ...itemV1, LockedFields: ['Name'] }}
          onClose={() => undefined}
        />
      </QueryClientProvider>,
    )
    expect(
      (screen.getByPlaceholderText('例如：肖申克的救赎') as HTMLInputElement).value,
    ).toBe('用户未保存的新片名')

    // L-36: MergeVersionsModal 在 mergeMutation.isPending 期间禁用候选按钮
    saveAuth({
      accessToken: 'tok-m',
      userId: ADMIN_USER.Id,
      serverId: 'srv-1',
      userName: 'admin',
      isAdmin: true,
      deviceId: 'dev-1',
    })
    vi.spyOn(api, 'getUser').mockResolvedValue(ADMIN_USER)
    vi.spyOn(api, 'getItems').mockResolvedValue({
      Items: [{ Id: 'm-cand-2', Name: '候选版本 4K', Type: 'Movie' }],
      TotalRecordCount: 1,
    })
    vi.spyOn(api, 'mergeItemVersions').mockReturnValue(new Promise(() => {}))

    renderWithProviders(<MergeVersionsModal open item={itemV1} onClose={() => undefined} />)
    const candBtn = await screen.findByRole('button', { name: /候选版本 4K/ })
    fireEvent.click(candBtn)
    fireEvent.click(screen.getByRole('button', { name: '确认合并' }))

    await waitFor(() => {
      expect(candBtn).toBeDisabled()
    })
  })

  it('M-44 & M-46 & M-48 & M-49 & M-50: 删除按类型跳转、迁移 URL 协议校验、播放偏好还原与串行、分页回退与工作室后端计数', async () => {
    // M-48: playbackPrefs.syncPlaybackPrefsFromUserConfig PlayDefaultAudioTrack===true 还原 media
    const synced = syncPlaybackPrefsFromUserConfig({
      PlayDefaultAudioTrack: true,
      AudioLanguagePreference: 'eng',
      SubtitleLanguagePreference: 'spa',
      SubtitleMode: 'Default',
    })
    expect(synced.preferredAudioLang).toBe('media')
    expect(synced.preferredSubtitleLang).toBe('spa')

    // M-44: Episode 删除跳转到 /item/{SeriesId}
    saveAuth({
      accessToken: 'tok-del',
      userId: ADMIN_USER.Id,
      serverId: 'srv-1',
      userName: 'admin',
      isAdmin: true,
      deviceId: 'dev-1',
    })
    vi.spyOn(api, 'getUser').mockResolvedValue(ADMIN_USER)
    vi.spyOn(api, 'getItem').mockResolvedValue({
      Id: 'ep-9',
      Name: '第 9 集',
      Type: 'Episode',
      SeriesId: 'series-88',
      ParentId: 'season-1',
    })
    vi.spyOn(api, 'getSimilarItems').mockResolvedValue({ Items: [], TotalRecordCount: 0 })
    vi.spyOn(api, 'deleteItem').mockResolvedValue(undefined)

    const detailView = renderWithProviders(
      <Routes>
        <Route path="/item/:id" element={<ItemDetailPage />} />
      </Routes>,
      ['/item/ep-9'],
    )
    fireEvent.click(await screen.findByRole('button', { name: /删除/ }))
    fireEvent.click(await screen.findByRole('button', { name: '确认删除' }))
    await waitFor(() => {
      expect(api.getItem).toHaveBeenCalledWith(ADMIN_USER.Id, 'series-88')
    })
    detailView.unmount()

    // M-46: MigrationTab 协议校验拦截 javascript: URL
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ status: 'idle' }), {
        status: 200,
        headers: { 'content-type': 'application/json' },
      }),
    )
    const migView = renderWithProviders(<MigrationTab />)
    fireEvent.change(screen.getByPlaceholderText('例如 http://192.168.1.88:8096'), {
      target: { value: 'javascript:alert(1)' },
    })
    fireEvent.change(screen.getByPlaceholderText('在旧 Emby 控制台 → API 密钥中生成'), {
      target: { value: 'key-123' },
    })
    fireEvent.click(screen.getByRole('button', { name: '测试连接' }))
    expect(await screen.findByText(/必须使用 http:\/\/ 或 https:\/\/ 协议/)).toBeInTheDocument()
    migView.unmount()

    // M-48: ProfilePage 包含 11 种语言选项且串行提交
    let resolveFirstSave!: () => void
    const updateCfgSpy = vi
      .spyOn(api, 'updateUserConfiguration')
      .mockImplementationOnce(
        () =>
          new Promise<void>((resolve) => {
            resolveFirstSave = resolve
          }),
      )
      .mockResolvedValueOnce(undefined)
    const profileView = renderWithProviders(<ProfilePage />, ['/profile'])
    const audioSelect = (await screen.findByLabelText('首选音轨语言')) as HTMLSelectElement
    const audioOptValues = Array.from(audioSelect.options).map((o) => o.value)
    expect(audioOptValues).toEqual(
      expect.arrayContaining(['spa', 'ger', 'fre', 'por', 'tha']),
    )
    expect(audioOptValues).toHaveLength(11)

    fireEvent.change(audioSelect, { target: { value: 'spa' } })
    fireEvent.change(audioSelect, { target: { value: 'ger' } })
    fireEvent.change(audioSelect, { target: { value: 'tha' } })
    // 串行队列：第一个未完成时不会并发发送第 2、3 次
    expect(updateCfgSpy).toHaveBeenCalledTimes(1)
    await act(async () => {
      resolveFirstSave()
      await Promise.resolve()
    })
    await waitFor(() => {
      expect(updateCfgSpy).toHaveBeenCalledTimes(2)
      expect(updateCfgSpy).toHaveBeenLastCalledWith(
        ADMIN_USER.Id,
        expect.objectContaining({ AudioLanguagePreference: 'tha' }),
      )
    })
    profileView.unmount()

    // M-49: MetadataTab 缩减总页数时自动回退 page 到合法范围
    vi.spyOn(api, 'getRankings').mockResolvedValue({
      douban_top250: { chart: 'douban_top250', source: 'douban', count: 0 },
      imdb_top250: { chart: 'imdb_top250', source: 'imdb', count: 0 },
    })
    vi.spyOn(api, 'getTags').mockResolvedValue({ Items: [], TotalRecordCount: 0 })
    const metaGetItemsSpy = vi
      .spyOn(api, 'getItems')
      .mockResolvedValueOnce({
        Items: [{ Id: 'm-1', Name: '第一页电影', Type: 'Movie' }],
        TotalRecordCount: 25,
      })
      .mockResolvedValueOnce({
        Items: [],
        TotalRecordCount: 5,
      })
      .mockResolvedValue({
        Items: [{ Id: 'm-1', Name: '第一页电影', Type: 'Movie' }],
        TotalRecordCount: 5,
      })
    const metaView = renderWithProviders(<MetadataTab />)
    await screen.findByText('第一页电影')
    fireEvent.click(screen.getByRole('button', { name: '下一页' }))
    await waitFor(() => {
      expect(metaGetItemsSpy).toHaveBeenLastCalledWith(
        ADMIN_USER.Id,
        expect.objectContaining({ startIndex: 0 }),
      )
    })
    metaView.unmount()

    // M-50: LibraryPage 信任后端 studio 过滤与 TotalRecordCount
    vi.spyOn(api, 'getItem').mockResolvedValue({
      Id: 'lib-m',
      Name: '电影',
      Type: 'CollectionFolder',
      CollectionType: 'movies',
    })
    vi.spyOn(api, 'getGenres').mockResolvedValue({ Items: [], TotalRecordCount: 0 })
    vi.spyOn(api, 'getYears').mockResolvedValue({ Items: [], TotalRecordCount: 0 })
    vi.spyOn(api, 'getItems').mockResolvedValue({
      Items: [{ Id: 'm-a24', Name: '瞬息全宇宙', Type: 'Movie', Studios: [{ Id: 's1', Name: 'Other' }] }],
      TotalRecordCount: 42,
    })
    const libView = renderWithProviders(
      <Routes>
        <Route path="/library/:id" element={<LibraryPage />} />
      </Routes>,
      ['/library/lib-m?studio=A24'],
    )
    expect(await screen.findByText(/共 42 部媒体/)).toBeInTheDocument()
    libView.unmount()
  })

  it('M-52 & L-34 & L-35 & L-37: Demo 脱敏、LiveTv fatal NETWORK_ERROR 销毁 HLS、LibrariesTab 弹窗反馈隔离与无障碍标签', async () => {
    // M-52: Demo Webhook 与插件配置脱敏与合并语义
    setDemoModeOverride(true)
    const demoAuth = await api.authenticateByName('admin', '')
    saveAuth({
      accessToken: demoAuth.AccessToken,
      userId: demoAuth.User.Id,
      serverId: demoAuth.ServerId,
      userName: demoAuth.User.Name,
      isAdmin: true,
      deviceId: 'dev-demo',
    })

    const hooks = await api.getWebhooks()
    expect(hooks[0].HasSecret).toBe(true)
    expect(hooks[0].Secret).toBeUndefined()

    const createdHook = await api.saveWebhook({
      Name: '秘密通知',
      Url: 'https://example.com/hook',
      Secret: 'top-secret-hmac',
      Events: ['PlaybackStart'],
      Enabled: true,
    })
    expect(createdHook.HasSecret).toBe(true)
    expect(createdHook.Secret).toBeUndefined()

    // 插件 GET 返回 '********'
    const tmdbCfg1 = await api.getPluginConfiguration('ambra.metadata.tmdb')
    expect(tmdbCfg1.ApiKey).toBe('********')

    // POST 传 '********' 保留原值
    await api.updatePluginConfiguration('ambra.metadata.tmdb', {
      Enabled: true,
      Priority: 10,
      ApiKey: '********',
      Language: 'en-US',
    })
    const tmdbCfg2 = await api.getPluginConfiguration('ambra.metadata.tmdb')
    expect(tmdbCfg2.ApiKey).toBe('********')
    expect(tmdbCfg2.Language).toBe('en-US')

    // POST 传 '' 清空密钥
    await api.updatePluginConfiguration('ambra.metadata.tmdb', {
      Enabled: true,
      Priority: 10,
      ApiKey: '',
      Language: 'en-US',
    })
    const tmdbCfg3 = await api.getPluginConfiguration('ambra.metadata.tmdb')
    expect(tmdbCfg3.ApiKey).toBe('')

    setDemoModeOverride(false)

    // L-34: LiveTvPage fatal NETWORK_ERROR 调用 hls.destroy()
    saveAuth({
      accessToken: 'tok-live',
      userId: ADMIN_USER.Id,
      serverId: 'srv-1',
      userName: 'admin',
      isAdmin: true,
      deviceId: 'dev-live',
    })
    vi.spyOn(api, 'getUser').mockResolvedValue(ADMIN_USER)
    vi.spyOn(api, 'getLiveTvChannels').mockResolvedValue({
      Items: [{ Id: 'ch-1', Name: '测试台', Number: '1', IsHls: true, StreamFormat: 'hls' }],
      TotalRecordCount: 1,
    })
    vi.spyOn(api, 'getLiveTvPrograms').mockResolvedValue({ Items: [], TotalRecordCount: 0 })
    vi.spyOn(api, 'getLiveTvTimers').mockResolvedValue({ Items: [], TotalRecordCount: 0 })

    let errorHandler: ((evt: string, data: { fatal: boolean; type: string }) => void) | undefined
    vi.spyOn(Hls, 'isSupported').mockReturnValue(true)
    vi.spyOn(Hls.prototype, 'loadSource').mockImplementation(() => undefined)
    vi.spyOn(Hls.prototype, 'attachMedia').mockImplementation(() => undefined)
    const hlsDestroySpy = vi.spyOn(Hls.prototype, 'destroy').mockImplementation(() => undefined)
    vi.spyOn(Hls.prototype, 'on').mockImplementation(((event: string, cb: unknown) => {
      if (event === Hls.Events.ERROR) {
        errorHandler = cb as typeof errorHandler
      }
      return undefined as unknown as Hls
    }) as Hls['on'])

    const liveView = renderWithProviders(<LiveTvPage />, ['/livetv'])
    await waitFor(() => {
      expect(errorHandler).toBeDefined()
    })
    act(() => {
      for (let i = 0; i < 11; i += 1) {
        errorHandler!(Hls.Events.ERROR, { fatal: true, type: Hls.ErrorTypes.NETWORK_ERROR })
      }
    })
    expect(hlsDestroySpy).toHaveBeenCalledTimes(1)
    expect(await screen.findByText(/直播流网络连接失败/)).toBeInTheDocument()
    liveView.unmount()

    // L-35: LibrariesTab 弹窗独立反馈不串扰主页面
    vi.spyOn(api, 'getVirtualFolders').mockResolvedValue([])
    vi.spyOn(api, 'getScanProgress').mockResolvedValue({
      running: false,
      paused: false,
      phase: 'idle',
      totalFiles: 0,
      processedFiles: 0,
      percent: 0,
      activeCount: 0,
      libraries: [],
    } as never)
    const libsView = renderWithProviders(<LibrariesTab />)
    fireEvent.click(await screen.findByRole('button', { name: '新增媒体库' }))
    fireEvent.change(await screen.findByPlaceholderText('例如: 4K 电影'), {
      target: { value: '无路径媒体库' },
    })
    fireEvent.click(screen.getByRole('button', { name: '创建媒体库' }))
    const libErrNodes = await screen.findAllByText('请至少添加一个服务器媒体目录路径')
    // 仅在弹窗内部渲染 1 次，不会同时出现在主页面顶部造成串扰
    expect(libErrNodes).toHaveLength(1)
    libsView.unmount()

    // L-37: MediaCard hover 适配 & Layout / HomePage 纯图标按钮 aria-label
    vi.spyOn(api, 'getUserViews').mockResolvedValue({
      Items: [{ Id: 'lib-1', Name: '电影', Type: 'CollectionFolder', CollectionType: 'movies' }],
      TotalRecordCount: 1,
    })
    vi.spyOn(api, 'getLiveTvInfo').mockResolvedValue({ IsEnabled: false })
    vi.spyOn(api, 'getResumeItems').mockResolvedValue({ Items: [], TotalRecordCount: 0 })
    vi.spyOn(api, 'getNextUp').mockResolvedValue({ Items: [], TotalRecordCount: 0 })
    vi.spyOn(api, 'getItems').mockResolvedValue({ Items: [], TotalRecordCount: 0 })
    vi.spyOn(api, 'getLatestItems').mockResolvedValue([
      { Id: 'm-home-1', Name: '首页电影', Type: 'Movie' },
    ])

    const layoutView = renderWithProviders(
      <Routes>
        <Route element={<Layout />}>
          <Route path="/" element={<HomePage />} />
        </Route>
      </Routes>,
      ['/'],
    )
    expect(screen.getByRole('button', { name: '打开导航菜单' })).toBeInTheDocument()
    expect(await screen.findByRole('button', { name: /向左滚动/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /向右滚动/ })).toBeInTheDocument()
    layoutView.unmount()

    const cardView = renderWithProviders(
      <MediaCard
        item={{ Id: 'm-touch', Name: '触控测试电影', Type: 'Movie' }}
        onMarkPlayed={() => undefined}
      />,
    )
    const playBtn = screen.getByRole('button', { name: '播放 触控测试电影' })
    expect(playBtn.className).toContain('[@media(hover:hover)]')
    cardView.unmount()
  })
})
