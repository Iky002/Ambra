import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { saveAuth } from '../../api/client'
import { AudioPlayerProvider } from '../../context/AudioPlayerContext'
import { AuthProvider } from '../../context/AuthContext'
import { WebSocketProvider } from '../../context/WebSocketContext'
import { ItemDetailPage } from '../ItemDetailPage'
import { LoginPage } from '../LoginPage'
import { PlayerPage } from '../PlayerPage'

function jsonRes(data: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    headers: new Headers({ 'content-type': 'application/json' }),
    json: async () => data,
    text: async () => JSON.stringify(data),
  } as Response
}

function emptyRes(status = 204) {
  return {
    ok: status >= 200 && status < 300,
    status,
    headers: new Headers(),
    json: async () => undefined,
    text: async () => '',
  } as Response
}

class MockWebSocket {
  static OPEN = 1
  static CLOSED = 3
  static instances: MockWebSocket[] = []

  url: string
  readyState = MockWebSocket.OPEN
  onopen: ((ev: Event) => void) | null = null
  onmessage: ((ev: MessageEvent) => void) | null = null
  onerror: ((ev: Event) => void) | null = null
  onclose: ((ev: CloseEvent) => void) | null = null

  constructor(url: string) {
    this.url = url
    MockWebSocket.instances.push(this)
    queueMicrotask(() => {
      this.onopen?.(new Event('open'))
    })
  }

  send() {}

  close() {
    this.readyState = MockWebSocket.CLOSED
  }

  emitMessage(payload: unknown) {
    const data = typeof payload === 'string' ? payload : JSON.stringify(payload)
    this.onmessage?.(new MessageEvent('message', { data }))
  }
}

describe('Stream 3 (v0.9.1): WS Cache Sync, String Seek, Theme Songs & Forgot Password', () => {
  const OriginalWebSocket = globalThis.WebSocket

  beforeEach(() => {
    localStorage.clear()
    MockWebSocket.instances = []
    vi.stubGlobal('WebSocket', MockWebSocket)
    vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined)
    vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => undefined)
    vi.spyOn(HTMLMediaElement.prototype, 'load').mockImplementation(() => undefined)
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.stubGlobal('WebSocket', OriginalWebSocket)
    vi.restoreAllMocks()
  })

  it('WebSocket UserDataChanged 与 LibraryChanged 通过 350ms 防抖批量刷新 React Query 缓存，且卸载时清理定时器', async () => {
    saveAuth({
      accessToken: 'tok-sync',
      userId: 'u-1',
      serverId: 'srv-1',
      userName: 'Admin',
      isAdmin: true,
      deviceId: 'dev-sync',
    })

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/emby/Users/u-1') {
        return jsonRes({ Id: 'u-1', Name: 'Admin', Policy: { IsAdministrator: true } })
      }
      return jsonRes({})
    })

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const invalidateSpy = vi.spyOn(qc, 'invalidateQueries').mockResolvedValue(undefined)

    const { unmount } = render(
      <QueryClientProvider client={qc}>
        <AuthProvider>
          <WebSocketProvider>
            <div>Sync Body</div>
          </WebSocketProvider>
        </AuthProvider>
      </QueryClientProvider>,
    )

    await waitFor(() => {
      expect(MockWebSocket.instances.length).toBe(1)
    })

    const ws = MockWebSocket.instances[0]
    vi.useFakeTimers()

    // 同时发送 UserDataChanged 与 LibraryChanged，验证 350ms 防抖合并去重
    act(() => {
      ws.emitMessage({ MessageType: 'UserDataChanged', Data: {} })
      ws.emitMessage({ MessageType: 'LibraryChanged', Data: {} })
    })

    // 300ms 内尚未触发
    act(() => {
      vi.advanceTimersByTime(300)
    })
    expect(invalidateSpy).not.toHaveBeenCalled()

    // 到达 350ms 后一次性触发去重后的全部 queryKey
    act(() => {
      vi.advanceTimersByTime(60)
    })

    const invalidatedKeys = invalidateSpy.mock.calls.map((call) => {
      const arg = call[0] as { queryKey?: string[] }
      return arg.queryKey?.[0]
    })

    expect(invalidatedKeys).toEqual(
      expect.arrayContaining([
        'resumeItems',
        'nextUp',
        'favorites',
        'episodes',
        'seasons',
        'libraryItems',
        'item',
        'latestItems',
        'userViews',
        'collections',
      ]),
    )
    // libraryItems 应被去重为 1 次
    expect(invalidatedKeys.filter((k) => k === 'libraryItems')).toHaveLength(1)

    // 再次发送并在防抖结束前卸载，确保定时器被清理不触发泄露调用
    invalidateSpy.mockClear()
    act(() => {
      ws.emitMessage({ MessageType: 'UserDataChanged', Data: {} })
    })
    unmount()
    act(() => {
      vi.advanceTimersByTime(500)
    })
    expect(invalidateSpy).not.toHaveBeenCalled()
  })

  it('PlayerPage 支持字符串类型的 SeekPositionTicks 远端 Seek 指令', async () => {
    saveAuth({
      accessToken: 'tok-seek',
      userId: 'u-1',
      serverId: 'srv-1',
      userName: 'Admin',
      isAdmin: true,
      deviceId: 'dev-seek',
    })

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/emby/Users/u-1') {
        return jsonRes({ Id: 'u-1', Name: 'Admin', Policy: { IsAdministrator: true } })
      }
      if (url === '/emby/Users/u-1/Items/movie-seek-1') {
        return jsonRes({
          Id: 'movie-seek-1',
          Name: '测试影片',
          Type: 'Movie',
          MediaType: 'Video',
          RunTimeTicks: 3600 * 10_000_000,
        })
      }
      if (url.startsWith('/emby/Items/movie-seek-1/PlaybackInfo')) {
        return jsonRes({
          PlaySessionId: 'sess-seek-1',
          MediaSources: [
            {
              Id: 'src-mp4',
              Container: 'mp4',
              SupportsDirectPlay: true,
              DefaultAudioStreamIndex: 1,
              MediaStreams: [
                { Index: 0, Type: 'Video', Codec: 'h264', Width: 1920, Height: 1080 },
                { Index: 1, Type: 'Audio', Codec: 'aac', Channels: 2, Language: 'eng' },
              ],
            },
          ],
        })
      }
      if (url.startsWith('/emby/Sessions/Playing')) {
        return emptyRes(204)
      }
      return jsonRes({})
    })

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const { container } = render(
      <QueryClientProvider client={qc}>
        <AuthProvider>
          <WebSocketProvider>
            <AudioPlayerProvider>
              <MemoryRouter initialEntries={['/play/movie-seek-1']}>
                <Routes>
                  <Route path="/play/:id" element={<PlayerPage />} />
                </Routes>
              </MemoryRouter>
            </AudioPlayerProvider>
          </WebSocketProvider>
        </AuthProvider>
      </QueryClientProvider>,
    )

    const video = container.querySelector('video') as HTMLVideoElement
    await waitFor(() => {
      expect(video.src).toContain('MediaSourceId=src-mp4')
      expect(MockWebSocket.instances.length).toBe(1)
    })

    const ws = MockWebSocket.instances[0]

    // 字符串格式 SeekPositionTicks（1200 秒 = 12_000_000_000 ticks）
    act(() => {
      ws.emitMessage({
        MessageType: 'Playstate',
        Data: { Command: 'Seek', SeekPositionTicks: '12000000000' },
      })
    })
    expect(video.currentTime).toBe(1200)

    // 非法负数或非数字字符串不应覆盖当前播放位置
    act(() => {
      ws.emitMessage({
        MessageType: 'Playstate',
        Data: { Command: 'Seek', SeekPositionTicks: '-500' },
      })
      ws.emitMessage({
        MessageType: 'Playstate',
        Data: { Command: 'Seek', SeekPositionTicks: 'invalid' },
      })
    })
    expect(video.currentTime).toBe(1200)
  })

  it('ItemDetailPage 渲染主题曲播放/停止按钮（0.35 音量且卸载清理）并复用 AddToCollectionModal', async () => {
    const user = userEvent.setup()
    saveAuth({
      accessToken: 'tok-detail',
      userId: 'u-1',
      serverId: 'srv-1',
      userName: 'Admin',
      isAdmin: true,
      deviceId: 'dev-detail',
    })

    const pauseSpy = vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => undefined)
    const playSpy = vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined)

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/emby/Users/u-1') {
        return jsonRes({ Id: 'u-1', Name: 'Admin', Policy: { IsAdministrator: true } })
      }
      if (url === '/emby/Users/u-1/Items/movie-theme-1') {
        return jsonRes({
          Id: 'movie-theme-1',
          Name: '星际穿越',
          Type: 'Movie',
          MediaType: 'Video',
          UserData: { Played: false, IsFavorite: false, PlaybackPositionTicks: 0 },
        })
      }
      if (url.startsWith('/emby/Items/movie-theme-1/ThemeSongs')) {
        return jsonRes({
          Items: [{ Id: 'theme-song-1', Name: 'Cornfield Chase', Type: 'Audio' }],
          TotalRecordCount: 1,
        })
      }
      if (url.includes('/LocalTrailers') || url.includes('/SpecialFeatures')) {
        return jsonRes([])
      }
      if (url.includes('/Similar')) {
        return jsonRes({ Items: [], TotalRecordCount: 0 })
      }
      if (url.includes('IncludeItemTypes=BoxSet') || url.includes('IncludeItemTypes=Playlist')) {
        return jsonRes({
          Items: [{ Id: 'col-1', Name: '科幻神作', Type: 'BoxSet' }],
          TotalRecordCount: 1,
        })
      }
      return jsonRes({})
    })

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const { container, unmount } = render(
      <QueryClientProvider client={qc}>
        <AuthProvider>
          <WebSocketProvider>
            <AudioPlayerProvider>
              <MemoryRouter initialEntries={['/item/movie-theme-1']}>
                <Routes>
                  <Route path="/item/:id" element={<ItemDetailPage />} />
                </Routes>
              </MemoryRouter>
            </AudioPlayerProvider>
          </WebSocketProvider>
        </AuthProvider>
      </QueryClientProvider>,
    )

    expect(await screen.findByRole('heading', { name: '星际穿越' })).toBeInTheDocument()

    // 1) 主题曲播放与停止
    const themeBtn = await screen.findByRole('button', { name: '播放主题曲' })
    await user.click(themeBtn)

    expect(await screen.findByRole('button', { name: '停止主题曲' })).toBeInTheDocument()
    const audioEls = Array.from(container.querySelectorAll('audio'))
    const themeAudio = audioEls.find((el) => el.src.includes('/emby/Audio/theme-song-1/universal'))
    expect(themeAudio).toBeDefined()
    expect(themeAudio?.volume).toBeCloseTo(0.35, 2)
    expect(playSpy).toHaveBeenCalled()

    // 2) 复用公共 AddToCollectionModal（含 tablist 角色）
    await user.click(screen.getByRole('button', { name: '加入合集 / 片单' }))
    expect(await screen.findByRole('tablist', { name: '目标容器类型' })).toBeInTheDocument()
    expect(await screen.findByText('科幻神作')).toBeInTheDocument()

    // 3) 离开页面卸载时自动停止主题曲
    pauseSpy.mockClear()
    unmount()
    expect(pauseSpy).toHaveBeenCalled()
  })

  it('LoginPage 显示 v0.9.1 默认版本并完成两步忘记密码 PIN 重置流程', async () => {
    const user = userEvent.setup()

    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      if (url === '/api/setup/status') {
        return jsonRes({ completed: true })
      }
      if (url === '/emby/System/Info/Public') {
        return jsonRes({ ServerName: 'Ambra Home' })
      }
      if (url === '/emby/Users/Public') {
        return jsonRes([])
      }
      if (url === '/emby/Users/ForgotPassword' && init?.method === 'POST') {
        return jsonRes({
          Action: 'PinCode',
          PinFile: '/var/lib/ambra/passwordreset-123456.json',
          PinExpirationDate: '2026-11-25T12:00:00Z',
        })
      }
      if (url === '/emby/Users/ForgotPassword/Pin' && init?.method === 'POST') {
        return jsonRes({
          Success: true,
          UsersReset: ['alice'],
        })
      }
      return jsonRes({})
    })

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={qc}>
        <AuthProvider>
          <MemoryRouter initialEntries={['/login']}>
            <Routes>
              <Route path="/login" element={<LoginPage />} />
            </Routes>
          </MemoryRouter>
        </AuthProvider>
      </QueryClientProvider>,
    )

    // 验证 fallback 版本号已更新为当前 v0.11.x
    expect(await screen.findByText(/v0\.11\.\d+/)).toBeInTheDocument()

    // 打开忘记密码弹窗
    await user.click(screen.getByRole('button', { name: '忘记密码？' }))
    expect(await screen.findByRole('dialog')).toBeInTheDocument()

    // Step 1: 输入用户名并提交
    const userInput = screen.getByLabelText('用户名')
    await user.type(userInput, 'alice')
    await user.click(screen.getByRole('button', { name: '下一步' }))

    // 验证展示服务器 PinFile 路径提示
    expect(await screen.findByText('请在服务器数据目录查看重置 PIN 文件')).toBeInTheDocument()
    expect(screen.getByText('/var/lib/ambra/passwordreset-123456.json')).toBeInTheDocument()

    // Step 2: 输入 6 位 PIN 码并提交
    const pinInput = screen.getByLabelText('6 位重置 PIN 码')
    await user.type(pinInput, '123456')
    await user.click(screen.getByRole('button', { name: '验证并重置密码' }))

    // 验证成功提示并回到登录表单（弹窗关闭）
    expect(await screen.findByText('密码已重置，请使用空密码或新密码登录')).toBeInTheDocument()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()

    expect(
      fetchSpy.mock.calls.some(
        ([u, init]) =>
          String(u) === '/emby/Users/ForgotPassword' &&
          init?.body === JSON.stringify({ EnteredUsername: 'alice' }),
      ),
    ).toBe(true)
    expect(
      fetchSpy.mock.calls.some(
        ([u, init]) =>
          String(u) === '/emby/Users/ForgotPassword/Pin' &&
          init?.body === JSON.stringify({ Pin: '123456' }),
      ),
    ).toBe(true)
  })
})
