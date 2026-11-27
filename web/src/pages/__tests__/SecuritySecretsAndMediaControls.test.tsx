import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import Hls from 'hls.js'
import { api, ApiError, getImageUrl, saveAuth } from '../../api/client'
import type { BaseItemDto, PluginInfo } from '../../api/types'
import { ImageManagerModal } from '../../components/media/ImageManagerModal'
import { AudioPlayerProvider } from '../../context/AudioPlayerContext'
import { AuthProvider, useAuth } from '../../context/AuthContext'
import { setDemoModeOverride } from '../../demo/demoMode'
import { I18nProvider } from '../../i18n/I18nContext'
import { ItemDetailPage } from '../ItemDetailPage'
import { LiveTvPage } from '../LiveTvPage'
import { PlayerPage } from '../PlayerPage'
import { ProfilePage } from '../ProfilePage'
import { SetupWizardPage } from '../SetupWizardPage'
import { PluginsTab } from '../admin/PluginsTab'
import { UsersTab } from '../admin/UsersTab'
import { WebhooksTab } from '../admin/WebhooksTab'

function jsonRes(data: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    headers: new Headers({ 'content-type': 'application/json' }),
    json: async () => data,
    text: async () => JSON.stringify(data),
  } as Response
}

function createQc() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
}

describe('Stream 5 (v0.10.4): Audit Remediation Comprehensive Tests', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    setDemoModeOverride(false)
    vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined)
    vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => undefined)
    vi.spyOn(HTMLMediaElement.prototype, 'load').mockImplementation(() => undefined)
  })

  afterEach(() => {
    setDemoModeOverride(null)
    vi.restoreAllMocks()
    localStorage.clear()
    sessionStorage.clear()
  })

  it('R-03.1: WebhooksTab 支持 HasSecret 徽章展示，并区分编辑留空保留原密钥与新建留空不签名提示', async () => {
    vi.spyOn(api, 'getWebhooks').mockResolvedValue([
      {
        Id: 'wh-sec-1',
        Name: '加密推送',
        Url: 'https://hooks.example.com/1',
        Secret: '',
        HasSecret: true,
        Events: ['LibraryChanged'],
        Enabled: true,
      },
    ])

    const qc = createQc()
    render(
      <QueryClientProvider client={qc}>
        <MemoryRouter>
          <WebhooksTab />
        </MemoryRouter>
      </QueryClientProvider>,
    )

    const card = await screen.findByTestId('webhook-card-wh-sec-1')
    expect(within(card).getByText('HMAC 签名')).toBeInTheDocument()

    // 编辑已有带密钥的 Webhook -> 显示「留空保留原密钥」提示
    fireEvent.click(within(card).getByRole('button', { name: '编辑' }))
    const editInput = (await screen.findByLabelText('HMAC 密钥（可选）')) as HTMLInputElement
    expect(editInput.value).toBe('')
    expect(editInput.placeholder).toContain('留空保留原密钥')
    expect(screen.getByText(/留空保留原密钥/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '取消' }))

    // 新建 Webhook -> 显示「留空不签名」提示
    fireEvent.click(screen.getByRole('button', { name: '新增 Webhook' }))
    const createInput = (await screen.findByLabelText('HMAC 密钥（可选）')) as HTMLInputElement
    expect(createInput.placeholder).toContain('留空不签名')
  })

  it('R-03.2: PluginsTab 脱敏处理 ******** 密钥，未修改提交 ********，点击清除密钥提交空字符串', async () => {
    const plugins: PluginInfo[] = [
      {
        Id: 'ambra.builtin.tmdb',
        Name: 'TMDB Scraper',
        Version: '1.0.0',
        BuiltIn: true,
        Enabled: true,
        Priority: 10,
        Kinds: ['metadata'],
      },
    ]
    vi.spyOn(api, 'getPlugins').mockResolvedValue(plugins)
    vi.spyOn(api, 'getPluginConfiguration').mockResolvedValue({
      enabled: true,
      priority: 10,
      api_key: '********',
      language: 'zh-CN',
    })
    const updateSpy = vi.spyOn(api, 'updatePluginConfiguration').mockResolvedValue(undefined)

    const qc = createQc()
    render(
      <QueryClientProvider client={qc}>
        <MemoryRouter>
          <PluginsTab />
        </MemoryRouter>
      </QueryClientProvider>,
    )

    await screen.findByText('TMDB Scraper')
    fireEvent.click(screen.getByRole('button', { name: '编辑配置' }))

    const keyInput = (await screen.findByLabelText('TMDB API Key')) as HTMLInputElement
    // 不预填明文 ********
    expect(keyInput.value).toBe('')
    expect(keyInput.placeholder).toContain('已配置密钥，留空保留原值')

    // 未修改直接保存 -> 提交 ******** 保留旧值
    fireEvent.click(screen.getByRole('button', { name: '保存配置' }))
    await waitFor(() => {
      expect(updateSpy).toHaveBeenCalledWith(
        'ambra.builtin.tmdb',
        expect.objectContaining({ api_key: '********', ApiKey: '********' }),
      )
    })

    // 再次打开并点击「清除密钥」 -> 提交空字符串 ""
    updateSpy.mockClear()
    fireEvent.click(screen.getByRole('button', { name: '编辑配置' }))
    await screen.findByLabelText('TMDB API Key')
    fireEvent.click(screen.getByRole('button', { name: '清除密钥' }))
    fireEvent.click(screen.getByRole('button', { name: '保存配置' }))

    await waitFor(() => {
      expect(updateSpy).toHaveBeenCalledWith(
        'ambra.builtin.tmdb',
        expect.objectContaining({ api_key: '', ApiKey: '' }),
      )
    })
  })

  it('R-03.3: LiveTvPage 非管理员频道缺少 StreamUrl 时依据 IsHls / StreamFormat 正确启用 HLS 播放', async () => {
    saveAuth({
      accessToken: 'tok-guest',
      userId: 'u-guest',
      serverId: 'srv-1',
      userName: 'guest',
      isAdmin: false,
      deviceId: 'dev-guest',
    })

    vi.spyOn(api, 'getUser').mockResolvedValue({
      Id: 'u-guest',
      Name: 'guest',
      ServerId: 'srv-1',
      Policy: { IsAdministrator: false, EnableLiveTvAccess: true },
    })
    vi.spyOn(api, 'getLiveTvChannels').mockResolvedValue({
      Items: [
        {
          Id: 'ch-hls-1',
          Name: 'CCTV-1 综合',
          Number: '1',
          IsHls: true,
          StreamFormat: 'hls',
          // 非管理员响应中后端已脱敏移除 StreamUrl
        },
      ],
      TotalRecordCount: 1,
    })
    vi.spyOn(api, 'getLiveTvPrograms').mockResolvedValue({ Items: [], TotalRecordCount: 0 })
    vi.spyOn(api, 'getLiveTvTimers').mockResolvedValue({ Items: [], TotalRecordCount: 0 })

    const isSupportedSpy = vi.spyOn(Hls, 'isSupported').mockReturnValue(true)
    const loadSourceSpy = vi.spyOn(Hls.prototype, 'loadSource').mockImplementation(() => undefined)
    const attachMediaSpy = vi.spyOn(Hls.prototype, 'attachMedia').mockImplementation(() => undefined)
    const destroySpy = vi.spyOn(Hls.prototype, 'destroy').mockImplementation(() => undefined)

    const qc = createQc()
    render(
      <QueryClientProvider client={qc}>
        <AuthProvider>
          <MemoryRouter initialEntries={['/livetv']}>
            <LiveTvPage />
          </MemoryRouter>
        </AuthProvider>
      </QueryClientProvider>,
    )

    const titles = await screen.findAllByText('CCTV-1 综合')
    expect(titles.length).toBeGreaterThan(0)
    await waitFor(() => {
      expect(loadSourceSpy).toHaveBeenCalledWith(
        expect.stringContaining('/emby/LiveTv/Channels/ch-hls-1/Stream'),
      )
    })
    expect(attachMediaSpy).toHaveBeenCalled()
    isSupportedSpy.mockRestore()
    loadSourceSpy.mockRestore()
    attachMediaSpy.mockRestore()
    destroySpy.mockRestore()
  })

  it('R-03.4: ProfilePage 与 UsersTab 修改自身密码成功后提示其他设备已下线，401 时优雅提示重登', async () => {
    const user = userEvent.setup()
    saveAuth({
      accessToken: 'tok-self',
      userId: 'u-self',
      serverId: 'srv-1',
      userName: 'admin',
      isAdmin: true,
      deviceId: 'dev-1',
    })
    vi.spyOn(api, 'getUser').mockResolvedValue({
      Id: 'u-self',
      Name: 'admin',
      ServerId: 'srv-1',
      Policy: { IsAdministrator: true },
    })
    const pwSpy = vi
      .spyOn(api, 'updateUserPassword')
      .mockResolvedValueOnce(undefined)
      .mockRejectedValueOnce(new ApiError(401, 'Unauthorized'))

    const qc = createQc()
    const { unmount } = render(
      <QueryClientProvider client={qc}>
        <AuthProvider>
          <MemoryRouter initialEntries={['/profile']}>
            <Routes>
              <Route path="/profile" element={<ProfilePage />} />
              <Route path="/login" element={<div data-testid="login-screen">重新登录页</div>} />
            </Routes>
          </MemoryRouter>
        </AuthProvider>
      </QueryClientProvider>,
    )

    await user.type(await screen.findByPlaceholderText('请输入新密码'), 'new-pass-123')
    await user.type(screen.getByPlaceholderText('再次输入新密码'), 'new-pass-123')
    await user.click(screen.getByRole('button', { name: '保存新密码' }))

    expect(await screen.findByText(/其他设备会话已自动下线/)).toBeInTheDocument()

    // 401 响应时优雅跳转重登页
    await user.type(screen.getByPlaceholderText('请输入新密码'), 'another-pw')
    await user.type(screen.getByPlaceholderText('再次输入新密码'), 'another-pw')
    await user.click(screen.getByRole('button', { name: '保存新密码' }))
    expect(await screen.findByTestId('login-screen')).toBeInTheDocument()
    unmount()

    // UsersTab 修改自身密码同样提示其他设备会话已自动下线
    pwSpy.mockResolvedValueOnce(undefined)
    vi.spyOn(api, 'getUsers').mockResolvedValue([
      {
        Id: 'u-self',
        Name: 'admin',
        ServerId: 'srv-1',
        Policy: { IsAdministrator: true },
      },
    ])
    vi.spyOn(api, 'getVirtualFolders').mockResolvedValue([])
    vi.spyOn(api, 'getParentalRatings').mockResolvedValue([])

    render(
      <QueryClientProvider client={createQc()}>
        <AuthProvider>
          <MemoryRouter>
            <UsersTab />
          </MemoryRouter>
        </AuthProvider>
      </QueryClientProvider>,
    )

    await screen.findByText('当前账号')
    await user.click(screen.getByRole('button', { name: '修改密码' }))
    await user.type(await screen.findByPlaceholderText('输入新密码'), 'admin-new-pw')
    await user.click(screen.getByRole('button', { name: '保存密码' }))
    expect(await screen.findByText(/其他设备会话已自动下线/)).toBeInTheDocument()
  })

  it('MH-02: AuthContext 在 logout、ambra:unauthorized 与切号 applyAuthResult 时清空 React Query 缓存', async () => {
    saveAuth({
      accessToken: 'tok-user-a',
      userId: 'u-a',
      serverId: 'srv-1',
      userName: 'UserA',
      isAdmin: false,
      deviceId: 'dev-1',
    })
    vi.spyOn(api, 'getUser').mockResolvedValue({
      Id: 'u-a',
      Name: 'UserA',
      ServerId: 'srv-1',
      Policy: { IsAdministrator: false },
    })
    vi.spyOn(api, 'logout').mockResolvedValue(undefined)

    const qc = createQc()
    qc.setQueryData(['private-user-a'], { secret: 'user-a-data' })

    function AuthProbe() {
      const { logout, applyAuthResult } = useAuth()
      return (
        <div>
          <button type="button" onClick={logout}>
            do-logout
          </button>
          <button
            type="button"
            onClick={() =>
              applyAuthResult({
                AccessToken: 'tok-user-b',
                ServerId: 'srv-1',
                User: { Id: 'u-b', Name: 'UserB', ServerId: 'srv-1', Policy: { IsAdministrator: false } },
              })
            }
          >
            switch-user
          </button>
        </div>
      )
    }

    render(
      <QueryClientProvider client={qc}>
        <AuthProvider>
          <AuthProbe />
        </AuthProvider>
      </QueryClientProvider>,
    )

    // 1) 切号清空缓存
    expect(qc.getQueryData(['private-user-a'])).toEqual({ secret: 'user-a-data' })
    fireEvent.click(screen.getByRole('button', { name: 'switch-user' }))
    expect(qc.getQueryData(['private-user-a'])).toBeUndefined()

    // 2) ambra:unauthorized 清空缓存
    qc.setQueryData(['private-user-b'], { secret: 'user-b-data' })
    window.dispatchEvent(new CustomEvent('ambra:unauthorized'))
    await waitFor(() => {
      expect(qc.getQueryData(['private-user-b'])).toBeUndefined()
    })

    // 3) logout 清空缓存
    qc.setQueryData(['private-after-relogin'], { secret: 'still-cached' })
    fireEvent.click(screen.getByRole('button', { name: 'do-logout' }))
    expect(qc.getQueryData(['private-after-relogin'])).toBeUndefined()
  })

  it('PlayerPage: URL ?audio=2 在支持 HLS 的浏览器中触发 HLS 音轨流，且切换字幕不导致视频重载', async () => {
    saveAuth({
      accessToken: 'tok-player',
      userId: 'u-1',
      serverId: 'srv-1',
      userName: 'Tester',
      isAdmin: true,
      deviceId: 'dev-p',
    })
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockImplementation((type) =>
      type === 'application/vnd.apple.mpegurl' ? 'probably' : '',
    )

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/emby/Users/u-1') {
        return jsonRes({ Id: 'u-1', Name: 'Tester', Policy: { IsAdministrator: true } })
      }
      if (url === '/emby/Users/u-1/Items/movie-audio') {
        return jsonRes({ Id: 'movie-audio', Name: '双音轨电影', Type: 'Movie' })
      }
      if (url.startsWith('/emby/Items/movie-audio/PlaybackInfo')) {
        return jsonRes({
          PlaySessionId: 'sess-audio-1',
          MediaSources: [
            {
              Id: 'src-mp4-1',
              Container: 'mp4',
              SupportsDirectPlay: true,
              DefaultAudioStreamIndex: 1,
              MediaStreams: [
                { Index: 0, Type: 'Video', Codec: 'h264', Width: 1920, Height: 1080 },
                { Index: 1, Type: 'Audio', Codec: 'aac', Channels: 2, Language: 'eng', DisplayTitle: 'English' },
                { Index: 2, Type: 'Audio', Codec: 'aac', Channels: 2, Language: 'jpn', DisplayTitle: '日本語' },
                { Index: 3, Type: 'Subtitle', Codec: 'subrip', Language: 'chi', DisplayTitle: '简体中文' },
              ],
            },
          ],
        })
      }
      if (url.includes('/Subtitles/3/Stream.vtt')) {
        return new Response('WEBVTT\n\n00:00:00.000 --> 00:01:00.000\n测试字幕\n', {
          status: 200,
          headers: { 'content-type': 'text/vtt' },
        })
      }
      return jsonRes({})
    })

    const qc = createQc()
    const { container } = render(
      <QueryClientProvider client={qc}>
        <AuthProvider>
          <AudioPlayerProvider>
            <MemoryRouter initialEntries={['/play/movie-audio?audio=2']}>
              <Routes>
                <Route path="/play/:id" element={<PlayerPage />} />
              </Routes>
            </MemoryRouter>
          </AudioPlayerProvider>
        </AuthProvider>
      </QueryClientProvider>,
    )

    const video = container.querySelector('video') as HTMLVideoElement
    await waitFor(() => {
      expect(video.src).toContain('/master.m3u8')
      expect(video.src).toContain('AudioStreamIndex=2')
    })

    // 切换字幕不应重载视频流
    const srcBefore = video.src
    const pauseSpy = vi.spyOn(video, 'pause')
    pauseSpy.mockClear()

    fireEvent.click(screen.getByTitle('切换字幕 (C)'))
    fireEvent.click(await screen.findByRole('button', { name: /简体中文/ }))
    await waitFor(() => {
      expect(screen.getByText(/字幕:/)).toBeInTheDocument()
    })
    expect(video.src).toBe(srcBefore)
    expect(pauseSpy).not.toHaveBeenCalled()
  })

  it('ImageManagerModal: 多张剧照预览 URL 正确携带各自的 ImageIndex 参数', async () => {
    expect(getImageUrl('item-1', 'Backdrop', { maxWidth: 800, index: 2 })).toContain(
      '/emby/Items/item-1/Images/Backdrop/2?',
    )

    vi.spyOn(api, 'getItemImages').mockResolvedValue([
      { ImageType: 'Backdrop', ImageIndex: 0, ImageTag: 'tag0', Filename: 'backdrop0.jpg' },
      { ImageType: 'Backdrop', ImageIndex: 1, ImageTag: 'tag1', Filename: 'backdrop1.jpg' },
      { ImageType: 'Backdrop', ImageIndex: 2, ImageTag: 'tag2', Filename: 'backdrop2.jpg' },
    ])

    const item: BaseItemDto = { Id: 'movie-multi-bg', Name: '多剧照电影', Type: 'Movie' }
    render(
      <QueryClientProvider client={createQc()}>
        <ImageManagerModal open item={item} onClose={() => undefined} />
      </QueryClientProvider>,
    )

    const imgs = await screen.findAllByRole('img', { name: 'Backdrop' })
    expect(imgs).toHaveLength(3)
    expect(imgs[0].getAttribute('src')).toContain('/Images/Backdrop/0?')
    expect(imgs[1].getAttribute('src')).toContain('/Images/Backdrop/1?')
    expect(imgs[2].getAttribute('src')).toContain('/Images/Backdrop/2?')
  })

  it('SetupWizardPage: 非管理员账号禁止越过 Step 2 管理员校验进入后续配置步骤', async () => {
    const user = userEvent.setup()
    saveAuth({
      accessToken: 'tok-member',
      userId: 'u-member',
      serverId: 'srv-1',
      userName: 'member',
      isAdmin: false,
      deviceId: 'dev-member',
    })

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/api/setup/status') {
        return jsonRes({
          completed: false,
          server_name: 'Ambra',
          default_language: 'zh-CN',
          has_users: true,
          has_tmdb_key: false,
        })
      }
      if (url === '/emby/Users/u-member') {
        return jsonRes({
          Id: 'u-member',
          Name: 'member',
          ServerId: 'srv-1',
          Policy: { IsAdministrator: false },
        })
      }
      return jsonRes({})
    })

    render(
      <QueryClientProvider client={createQc()}>
        <AuthProvider>
          <MemoryRouter initialEntries={['/setup']}>
            <Routes>
              <Route path="/setup" element={<SetupWizardPage />} />
              <Route path="/login" element={<div>Login Page</div>} />
            </Routes>
          </MemoryRouter>
        </AuthProvider>
      </QueryClientProvider>,
    )

    expect(await screen.findByText('当前账号不具备管理员权限')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /下一步：创建管理员/ }))

    expect(await screen.findByText('第 2 步 · 创建管理员账号')).toBeInTheDocument()
    // 非管理员不应看到「已登录，直接下一步」跳过按钮，且提交按钮处于禁用态
    expect(screen.queryByRole('button', { name: /已登录，直接下一步/ })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /创建并继续/ })).toBeDisabled()
  })

  it('ItemDetailPage: 外挂字幕上传校验允许的扩展名与 20MB 体积上限', async () => {
    saveAuth({
      accessToken: 'tok-admin',
      userId: 'u-admin',
      serverId: 'srv-1',
      userName: 'admin',
      isAdmin: true,
      deviceId: 'dev-admin',
    })

    const uploadSpy = vi.spyOn(api, 'uploadSubtitle').mockResolvedValue(undefined)
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/emby/Users/u-admin') {
        return jsonRes({ Id: 'u-admin', Name: 'admin', Policy: { IsAdministrator: true } })
      }
      if (url === '/emby/Users/u-admin/Items/movie-sub-val') {
        return jsonRes({
          Id: 'movie-sub-val',
          Name: '字幕校验测试片',
          Type: 'Movie',
          MediaType: 'Video',
          MediaSources: [
            {
              Id: 'src-sub-1',
              Name: '1080p',
              Container: 'mkv',
              MediaStreams: [{ Index: 0, Type: 'Video', Codec: 'h264' }],
            },
          ],
          UserData: { Played: false, IsFavorite: false, PlaybackPositionTicks: 0, PlayCount: 0 },
        })
      }
      return jsonRes({ Items: [], TotalRecordCount: 0 })
    })

    render(
      <QueryClientProvider client={createQc()}>
        <I18nProvider initialLocale="zh-CN">
          <AuthProvider>
            <AudioPlayerProvider>
              <MemoryRouter initialEntries={['/item/movie-sub-val']}>
                <Routes>
                  <Route path="/item/:id" element={<ItemDetailPage />} />
                </Routes>
              </MemoryRouter>
            </AudioPlayerProvider>
          </AuthProvider>
        </I18nProvider>
      </QueryClientProvider>,
    )

    fireEvent.click(await screen.findByRole('button', { name: '管理字幕' }))
    const fileInput = (await screen.findByLabelText('选择字幕文件')) as HTMLInputElement

    // 1. 非法扩展名 (.exe) 被拦截
    const badFile = new File(['MZ...'], 'malware.exe', { type: 'application/octet-stream' })
    fireEvent.change(fileInput, { target: { files: [badFile] } })
    expect(await screen.findByText(/仅支持上传 \.srt \/ \.vtt \/ \.ass \/ \.ssa \/ \.sub \/ \.sup/)).toBeInTheDocument()

    // 2. 超过 20MB 体积被拦截
    const hugeFile = new File(['x'], 'oversize.srt', { type: 'text/plain' })
    Object.defineProperty(hugeFile, 'size', { value: 21 * 1024 * 1024 })
    fireEvent.change(fileInput, { target: { files: [hugeFile] } })
    expect(await screen.findByText(/不超过 20 MB/)).toBeInTheDocument()
    expect(uploadSpy).not.toHaveBeenCalled()
  })
})
