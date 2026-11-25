import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { saveAuth } from '../../api/client'
import { AudioPlayerProvider } from '../../context/AudioPlayerContext'
import { AuthProvider } from '../../context/AuthContext'
import { WebSocketProvider } from '../../context/WebSocketContext'
import { setDemoModeOverride } from '../../demo/demoMode'
import { ItemDetailPage } from '../ItemDetailPage'
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

function LocationProbe() {
  const loc = useLocation()
  return <div data-testid="location-probe">{`${loc.pathname}${loc.search}`}</div>
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
  sent: string[] = []

  constructor(url: string) {
    this.url = url
    MockWebSocket.instances.push(this)
    queueMicrotask(() => {
      this.onopen?.(new Event('open'))
    })
  }

  send(data: string) {
    this.sent.push(data)
  }

  close() {
    this.readyState = MockWebSocket.CLOSED
  }

  emitMessage(payload: unknown) {
    const data = typeof payload === 'string' ? payload : JSON.stringify(payload)
    this.onmessage?.(new MessageEvent('message', { data }))
  }
}

const sampleMovieWithExtras = {
  Id: 'movie-ws-1',
  Name: '沙丘 2',
  OriginalTitle: 'Dune: Part Two',
  Type: 'Movie',
  MediaType: 'Video',
  RunTimeTicks: 9_960 * 10_000_000,
  Chapters: [
    { StartPositionTicks: 0, Name: '沙漠伏击', ImageTag: 'ch-tag-0' },
    { StartPositionTicks: 1200 * 10_000_000, Name: '驾驭沙虫', ImageTag: 'ch-tag-1' },
  ],
  MediaSources: [
    {
      Id: 'src-4k',
      Name: '4K REMUX',
      Path: '/media/movies/Dune.Part.Two.2024.2160p.mkv',
      Container: 'mkv',
      Size: 40 * 1024 * 1024 * 1024,
      Bitrate: 42_000_000,
      RunTimeTicks: 9_960 * 10_000_000,
      SupportsDirectPlay: true,
      DefaultAudioStreamIndex: 1,
      DefaultSubtitleStreamIndex: 2,
      MediaStreams: [
        { Index: 0, Type: 'Video', Codec: 'hevc', Width: 3840, Height: 2160, VideoRange: 'HDR10' },
        {
          Index: 1,
          Type: 'Audio',
          Codec: 'aac',
          Channels: 6,
          Language: 'eng',
          DisplayLanguage: 'English',
          IsDefault: true,
        },
        {
          Index: 2,
          Type: 'Subtitle',
          Codec: 'subrip',
          Language: 'chi',
          DisplayTitle: '简体中文',
          IsDefault: true,
          IsExternal: false,
        },
        {
          Index: 3,
          Type: 'Subtitle',
          Codec: 'ass',
          Language: 'eng',
          DisplayTitle: 'English External',
          IsExternal: true,
        },
      ],
    },
  ],
  UserData: {
    Played: false,
    IsFavorite: false,
    PlaybackPositionTicks: 0,
    PlayCount: 0,
  },
}

describe('Stream 3 (v0.9.0): WebSocket Live Client, Player & Extras', () => {
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
    setDemoModeOverride(null)
    vi.stubGlobal('WebSocket', OriginalWebSocket)
    vi.restoreAllMocks()
  })

  it('接收 WebSocket GeneralCommand(DisplayMessage) 并渲染全局 Toast（非 Demo 模式忽略 ambra:ws-mock，Demo 模式允许）', async () => {
    const user = userEvent.setup()
    saveAuth({
      accessToken: 'tok-ws',
      userId: 'u-1',
      serverId: 'srv-1',
      userName: 'Admin',
      isAdmin: true,
      deviceId: 'dev-ws-1',
    })

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/emby/Users/u-1') {
        return jsonRes({ Id: 'u-1', Name: 'Admin', Policy: { IsAdministrator: true } })
      }
      return jsonRes({})
    })

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const { unmount } = render(
      <QueryClientProvider client={qc}>
        <AuthProvider>
          <WebSocketProvider>
            <div>App Body</div>
          </WebSocketProvider>
        </AuthProvider>
      </QueryClientProvider>,
    )

    await waitFor(() => {
      expect(MockWebSocket.instances.length).toBe(1)
    })

    const ws = MockWebSocket.instances[0]
    expect(ws.url).toContain('/embywebsocket?api_key=tok-ws&deviceId=dev-ws-1')

    act(() => {
      ws.emitMessage({
        MessageType: 'GeneralCommand',
        Data: {
          Name: 'DisplayMessage',
          Arguments: {
            Header: '管理员广播',
            Text: '服务器将在 10 分钟后维护',
            TimeoutMs: 60_000,
          },
        },
      })
    })

    expect(await screen.findByText('管理员广播')).toBeInTheDocument()
    expect(screen.getByText('服务器将在 10 分钟后维护')).toBeInTheDocument()

    // 非 Demo 模式下 ambra:ws-mock 不应注入伪造通知
    act(() => {
      window.dispatchEvent(
        new CustomEvent('ambra:ws-mock', {
          detail: {
            MessageType: 'GeneralCommand',
            Data: {
              Name: 'DisplayMessage',
              Arguments: {
                Header: '未授权模拟推送',
                Text: '非 Demo 模式应忽略',
                TimeoutMs: 60_000,
              },
            },
          },
        }),
      )
    })
    expect(screen.queryByText('未授权模拟推送')).not.toBeInTheDocument()

    // 手动关闭通知
    const closeButtons = screen.getAllByRole('button', { name: '关闭通知' })
    await user.click(closeButtons[0])
    await waitFor(() => {
      expect(screen.queryByText('管理员广播')).not.toBeInTheDocument()
    })
    unmount()

    // Demo 模式下通过 ambra:ws-mock 桥触发通知
    setDemoModeOverride(true)
    saveAuth({
      accessToken: 'tok-demo-ws',
      userId: 'demo-admin',
      serverId: 'demo-srv',
      userName: 'Demo Admin',
      isAdmin: true,
      deviceId: 'dev-demo-ws',
    })
    const qcDemo = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={qcDemo}>
        <AuthProvider>
          <WebSocketProvider>
            <div>Demo Body</div>
          </WebSocketProvider>
        </AuthProvider>
      </QueryClientProvider>,
    )

    act(() => {
      window.dispatchEvent(
        new CustomEvent('ambra:ws-mock', {
          detail: {
            MessageType: 'GeneralCommand',
            Data: {
              Name: 'DisplayMessage',
              Arguments: {
                Header: 'Demo 推送',
                Text: '欢迎体验实时通知',
                TimeoutMs: 60_000,
              },
            },
          },
        }),
      )
    })

    expect(await screen.findByText('Demo 推送')).toBeInTheDocument()
  })

  it('PlayerPage 响应 WebSocket Playstate 远端播控 (Pause/Unpause/Seek/Stop) 并在快捷键浮层打开时屏蔽播控热键', async () => {
    saveAuth({
      accessToken: 'tok-playstate',
      userId: 'u-1',
      serverId: 'srv-1',
      userName: 'Admin',
      isAdmin: true,
      deviceId: 'dev-playstate',
    })

    const playSpy = vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined)
    const pauseSpy = vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => undefined)

    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/emby/Users/u-1') {
        return jsonRes({ Id: 'u-1', Name: 'Admin', Policy: { IsAdministrator: true } })
      }
      if (url === '/emby/Users/u-1/Items/movie-ws-1') {
        return jsonRes(sampleMovieWithExtras)
      }
      if (url.startsWith('/emby/Items/movie-ws-1/PlaybackInfo')) {
        return jsonRes({
          PlaySessionId: 'sess-ws-1',
          MediaSources: [
            {
              Id: 'src-4k',
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
              <MemoryRouter initialEntries={['/play/movie-ws-1']}>
                <Routes>
                  <Route path="/play/:id" element={<PlayerPage />} />
                  <Route path="/item/:id" element={<LocationProbe />} />
                </Routes>
              </MemoryRouter>
            </AudioPlayerProvider>
          </WebSocketProvider>
        </AuthProvider>
      </QueryClientProvider>,
    )

    const video = container.querySelector('video') as HTMLVideoElement
    await waitFor(() => {
      expect(video.src).toContain('MediaSourceId=src-4k')
      expect(MockWebSocket.instances.length).toBe(1)
    })

    const ws = MockWebSocket.instances[0]
    playSpy.mockClear()
    pauseSpy.mockClear()

    // 快捷键浮层打开时，Space 不应触发播放/暂停
    fireEvent.keyDown(window, { key: '?' })
    expect(await screen.findByText('快捷键说明')).toBeInTheDocument()
    fireEvent.keyDown(window, { key: ' ', code: 'Space' })
    expect(playSpy).not.toHaveBeenCalled()
    expect(pauseSpy).not.toHaveBeenCalled()
    fireEvent.keyDown(window, { key: 'Escape' })
    await waitFor(() => {
      expect(screen.queryByText('快捷键说明')).not.toBeInTheDocument()
    })

    // 远端 Pause / Unpause / Seek
    act(() => {
      ws.emitMessage({ MessageType: 'Playstate', Data: { Command: 'Pause' } })
    })
    expect(pauseSpy).toHaveBeenCalledTimes(1)

    act(() => {
      ws.emitMessage({ MessageType: 'Playstate', Data: { Command: 'Unpause' } })
    })
    expect(playSpy).toHaveBeenCalledTimes(1)

    act(() => {
      ws.emitMessage({
        MessageType: 'Playstate',
        Data: { Command: 'Seek', SeekPositionTicks: 900 * 10_000_000 },
      })
    })
    expect(video.currentTime).toBe(900)

    // 远端 Stop -> 上报 Stopped 并退出至详情页
    act(() => {
      ws.emitMessage({ MessageType: 'Playstate', Data: { Command: 'Stop' } })
    })

    expect(await screen.findByTestId('location-probe')).toHaveTextContent('/item/movie-ws-1')
    expect(
      fetchSpy.mock.calls.some(([u]) => String(u) === '/emby/Sessions/Playing/Stopped'),
    ).toBe(true)
  })

  it('非管理员隐藏「管理字幕」按钮，点击章节保留所选版本/音轨/字幕，并渲染章节缩略图、预告片按钮与花絮条', async () => {
    const user = userEvent.setup()
    saveAuth({
      accessToken: 'tok-guest',
      userId: 'u-guest',
      serverId: 'srv-1',
      userName: 'Guest',
      isAdmin: false,
      deviceId: 'dev-guest',
    })

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/emby/Users/u-guest') {
        return jsonRes({ Id: 'u-guest', Name: 'Guest', Policy: { IsAdministrator: false } })
      }
      if (url === '/emby/Users/u-guest/Items/movie-ws-1') {
        return jsonRes(sampleMovieWithExtras)
      }
      if (url === '/emby/Users/u-guest/Items/movie-ws-1/LocalTrailers') {
        return jsonRes([
          { Id: 'trailer-1', Name: '沙丘 2 官方终极预告', Type: 'Trailer', MediaType: 'Video' },
        ])
      }
      if (url === '/emby/Users/u-guest/Items/movie-ws-1/SpecialFeatures') {
        return jsonRes([
          {
            Id: 'extra-1',
            Name: '幕后制作特辑：沙虫特效解析',
            Type: 'Video',
            MediaType: 'Video',
            RunTimeTicks: 600 * 10_000_000,
          },
        ])
      }
      if (url.includes('/Similar')) {
        return jsonRes({ Items: [], TotalRecordCount: 0 })
      }
      return jsonRes({})
    })

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={qc}>
        <AuthProvider>
          <WebSocketProvider>
            <AudioPlayerProvider>
              <MemoryRouter initialEntries={['/item/movie-ws-1']}>
                <Routes>
                  <Route path="/item/:id" element={<ItemDetailPage />} />
                  <Route path="/play/:id" element={<LocationProbe />} />
                </Routes>
              </MemoryRouter>
            </AudioPlayerProvider>
          </WebSocketProvider>
        </AuthProvider>
      </QueryClientProvider>,
    )

    expect(await screen.findByRole('heading', { name: '沙丘 2' })).toBeInTheDocument()

    // 1) 非管理员不显示「管理字幕」按钮
    expect(screen.queryByRole('button', { name: '管理字幕' })).not.toBeInTheDocument()

    // 2) 渲染本地预告片按钮与花絮与特别收录区域
    expect(await screen.findByRole('button', { name: '预告片' })).toBeInTheDocument()
    expect(await screen.findByText('花絮与特别收录')).toBeInTheDocument()
    expect(screen.getByText('幕后制作特辑：沙虫特效解析')).toBeInTheDocument()

    // 3) 章节缩略图使用 /Images/Chapter/{idx}
    const chapterImg = screen.getByAltText('驾驭沙虫') as HTMLImageElement
    expect(chapterImg.src).toContain('/emby/Items/movie-ws-1/Images/Chapter/1')
    expect(chapterImg.src).toContain('tag=ch-tag-1')

    // 4) 切换字幕后点击章节，跳转 URL 同时保留 startTicks 与 mediaSourceId / audio / subtitle
    await user.selectOptions(screen.getByLabelText(/字幕/), '3')
    await user.click(screen.getByRole('button', { name: /驾驭沙虫/ }))

    const loc = await screen.findByTestId('location-probe')
    expect(loc.textContent).toContain('/play/movie-ws-1?')
    expect(loc.textContent).toContain(`startTicks=${1200 * 10_000_000}`)
    expect(loc.textContent).toContain('mediaSourceId=src-4k')
    expect(loc.textContent).toContain('audio=1')
    expect(loc.textContent).toContain('subtitle=3')
  })

  it('点击「预告片」与「花絮与特别收录」分别跳转到对应播放页', async () => {
    const user = userEvent.setup()
    saveAuth({
      accessToken: 'tok-extras',
      userId: 'u-1',
      serverId: 'srv-1',
      userName: 'Admin',
      isAdmin: true,
      deviceId: 'dev-extras',
    })

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/emby/Users/u-1') {
        return jsonRes({ Id: 'u-1', Name: 'Admin', Policy: { IsAdministrator: true } })
      }
      if (url === '/emby/Users/u-1/Items/movie-ws-1') {
        return jsonRes(sampleMovieWithExtras)
      }
      if (url === '/emby/Users/u-1/Items/movie-ws-1/LocalTrailers') {
        return jsonRes([{ Id: 'trailer-1', Name: '官方预告', Type: 'Trailer' }])
      }
      if (url === '/emby/Users/u-1/Items/movie-ws-1/SpecialFeatures') {
        return jsonRes([{ Id: 'extra-1', Name: '删减片段', Type: 'Video' }])
      }
      if (url.includes('/Similar')) {
        return jsonRes({ Items: [], TotalRecordCount: 0 })
      }
      return jsonRes({})
    })

    // 测试点击预告片
    const qc1 = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const { unmount } = render(
      <QueryClientProvider client={qc1}>
        <AuthProvider>
          <WebSocketProvider>
            <AudioPlayerProvider>
              <MemoryRouter initialEntries={['/item/movie-ws-1']}>
                <Routes>
                  <Route path="/item/:id" element={<ItemDetailPage />} />
                  <Route path="/play/:id" element={<LocationProbe />} />
                </Routes>
              </MemoryRouter>
            </AudioPlayerProvider>
          </WebSocketProvider>
        </AuthProvider>
      </QueryClientProvider>,
    )

    await user.click(await screen.findByRole('button', { name: '预告片' }))
    expect(await screen.findByTestId('location-probe')).toHaveTextContent('/play/trailer-1')
    unmount()

    // 测试点击花絮条目
    const qc2 = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={qc2}>
        <AuthProvider>
          <WebSocketProvider>
            <AudioPlayerProvider>
              <MemoryRouter initialEntries={['/item/movie-ws-1']}>
                <Routes>
                  <Route path="/item/:id" element={<ItemDetailPage />} />
                  <Route path="/play/:id" element={<LocationProbe />} />
                </Routes>
              </MemoryRouter>
            </AudioPlayerProvider>
          </WebSocketProvider>
        </AuthProvider>
      </QueryClientProvider>,
    )

    await user.click(await screen.findByRole('button', { name: /删减片段/ }))
    expect(await screen.findByTestId('location-probe')).toHaveTextContent('/play/extra-1')
  })
})
