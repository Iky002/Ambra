import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { saveAuth } from '../../api/client'
import type { BaseItemDto } from '../../api/types'
import { AudioPlayerProvider, useAudioPlayer } from '../../context/AudioPlayerContext'
import { AuthProvider, useAuth } from '../../context/AuthContext'
import { WebSocketContext, type PlaystateCommand } from '../../context/WebSocketContext'
import { setDemoModeOverride } from '../../demo/demoMode'
import { resetDemoStore } from '../../demo/mockServer'
import { AdminPage } from '../../pages/AdminPage'
import { ItemDetailPage } from '../../pages/ItemDetailPage'
import { AudioPlayerBar, normalizeLyricsResponse, parseLrcText } from '../AudioPlayerBar'
import { Layout } from '../Layout'

function renderWithAppProviders(ui: React.ReactNode, initialEntries: string[]) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={qc}>
      <AuthProvider>
        <AudioPlayerProvider>
          <MemoryRouter initialEntries={initialEntries}>{ui}</MemoryRouter>
        </AudioPlayerProvider>
      </AuthProvider>
    </QueryClientProvider>,
  )
}

function AudioTriggerHarness({ tracks }: { tracks: BaseItemDto[] }) {
  const { playQueue } = useAudioPlayer()
  return (
    <button type="button" onClick={() => playQueue(tracks, 0)}>
      启动测试播放队列
    </button>
  )
}

describe('Batch 4C Web UI 功能测试 (Admin Tab 滚动保持 + 全局音乐播放器与 LRC 歌词)', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    resetDemoStore()
    setDemoModeOverride(true)
    saveAuth({
      accessToken: 'demo_tok_batch4c',
      userId: 'user-demo-admin',
      serverId: 'ambra-serverless-demo-01',
      userName: 'admin',
      isAdmin: true,
      deviceId: 'dev-batch4c',
    })
    vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined)
    vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => undefined)
    vi.spyOn(HTMLMediaElement.prototype, 'load').mockImplementation(() => undefined)
  })

  afterEach(() => {
    setDemoModeOverride(null)
    localStorage.clear()
    sessionStorage.clear()
    vi.restoreAllMocks()
  })

  it('Layout 在 /admin/dashboard -> /admin/devices 切换时保持 AdminPage 挂载且保留横向滚动位置', async () => {
    const user = userEvent.setup()

    renderWithAppProviders(
      <Routes>
        <Route path="/" element={<Layout />}>
          <Route path="admin/*" element={<AdminPage />} />
        </Route>
      </Routes>,
      ['/admin/dashboard'],
    )

    const tabNav = await screen.findByTestId('admin-tab-nav')
    // 模拟用户在窄屏横向滑动标签栏到 260px
    Object.defineProperty(tabNav, 'scrollLeft', {
      value: 260,
      writable: true,
      configurable: true,
    })
    tabNav.dispatchEvent(new Event('scroll'))

    const devicesLink = screen.getByRole('link', { name: /设备与密钥/ })
    await user.click(devicesLink)

    await waitFor(() => {
      expect(devicesLink).toHaveAttribute('aria-current', 'page')
    })

    // 确认 AdminPage 未被卸载重挂（同一 DOM 节点且 scrollLeft 保持 260）
    const tabNavAfter = screen.getByTestId('admin-tab-nav')
    expect(tabNavAfter).toBe(tabNav)
    expect(tabNavAfter.scrollLeft).toBe(260)
  })

  it('AudioPlayerBar + AudioPlayerProvider 支持队列播放、上一曲/下一曲切换及同步 LRC 歌词抽屉', async () => {
    const user = userEvent.setup()
    const testTracks: BaseItemDto[] = [
      {
        Id: 'track-interstellar-1',
        Name: 'Dreaming of the Crash',
        Type: 'Audio',
        MediaType: 'Audio',
        AlbumId: 'album-interstellar-ost',
        Album: '星际穿越 原声大碟',
        AlbumArtist: 'Hans Zimmer',
        Artists: ['Hans Zimmer'],
        RunTimeTicks: 235 * 10_000_000,
      },
      {
        Id: 'track-interstellar-2',
        Name: 'Cornfield Chase',
        Type: 'Audio',
        MediaType: 'Audio',
        AlbumId: 'album-interstellar-ost',
        Album: '星际穿越 原声大碟',
        AlbumArtist: 'Hans Zimmer',
        Artists: ['Hans Zimmer'],
        RunTimeTicks: 126 * 10_000_000,
      },
    ]

    renderWithAppProviders(
      <>
        <AudioTriggerHarness tracks={testTracks} />
        <AudioPlayerBar />
      </>,
      ['/'],
    )

    expect(screen.queryByTestId('audio-player-bar')).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '启动测试播放队列' }))

    expect(await screen.findByTestId('audio-player-bar')).toBeInTheDocument()
    expect(screen.getByText('Dreaming of the Crash')).toBeInTheDocument()

    // 下一曲
    await user.click(screen.getByRole('button', { name: '下一曲' }))
    expect(await screen.findByText('Cornfield Chase')).toBeInTheDocument()

    // 上一曲
    await user.click(screen.getByRole('button', { name: '上一曲' }))
    expect(await screen.findByText('Dreaming of the Crash')).toBeInTheDocument()

    // 打开同步 LRC 歌词抽屉并验证歌词渲染与点击跳转
    await user.click(screen.getByRole('button', { name: '同步歌词' }))
    expect(await screen.findByTestId('lyrics-drawer')).toBeInTheDocument()
    expect(
      await screen.findByText('管风琴低音缓缓铺陈，呼啸的风声掠过玉米田'),
    ).toBeInTheDocument()

    // 打开播放队列弹层
    await user.click(screen.getByRole('button', { name: '播放队列' }))
    expect(await screen.findByTestId('queue-popover')).toBeInTheDocument()
    expect(screen.getByText('播放队列 (2)')).toBeInTheDocument()
  })

  it('ItemDetailPage 渲染 MusicAlbum 专辑曲目表并可通过「播放全部」触发全局音乐播放', async () => {
    const user = userEvent.setup()

    renderWithAppProviders(
      <>
        <Routes>
          <Route path="/item/:id" element={<ItemDetailPage />} />
        </Routes>
        <AudioPlayerBar />
      </>,
      ['/item/album-interstellar-ost'],
    )

    expect(await screen.findByText('星际穿越 原声大碟')).toBeInTheDocument()
    expect(await screen.findByTestId('album-tracklist')).toBeInTheDocument()
    expect(await screen.findByText('Dreaming of the Crash')).toBeInTheDocument()
    expect(screen.getByText('Cornfield Chase')).toBeInTheDocument()
    expect(screen.getByText('No Time for Caution')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '播放全部' }))

    const playerBar = await screen.findByTestId('audio-player-bar')
    expect(playerBar).toBeInTheDocument()
    expect(screen.getByText('正在播放')).toBeInTheDocument()
  })

  it('Stream C (P0-5/P1-13/P2-16): 歌词解析支持 offset/三位分钟/空行间奏、Emby ticks，抽屉互斥、错误态与退出登录清空队列', async () => {
    // 1) LRC 解析：支持 [offset:±ms]、三位分钟与空文本时间戳
    const lrcLines = parseLrcText(
      '[offset:500]\n[00:10.00]第一句\n[01:05.00]\n[105:30.25]超长音频歌词',
    )
    expect(lrcLines).toEqual([
      { timeSec: 9.5, text: '第一句' },
      { timeSec: 64.5, text: '' },
      { timeSec: 105 * 60 + 30.25 - 0.5, text: '超长音频歌词' },
    ])

    // 2) Emby LyricLine.Start 严格按 100ns ticks 换算
    const tickLines = normalizeLyricsResponse({
      Lyrics: [
        { Start: 50_000, Text: '片头极早歌词' },
        { Start: 150_000_000, Text: '第 15 秒' },
      ],
    })
    expect(tickLines[0].timeSec).toBeCloseTo(0.005, 6)
    expect(tickLines[1].timeSec).toBe(15)

    // 3) 播放失败时 isPlaying 为 false 并展示内联错误提示条；抽屉互斥；退出登录清空队列
    vi.spyOn(HTMLMediaElement.prototype, 'play').mockRejectedValueOnce(new Error('Autoplay blocked'))

    function LogoutButton() {
      const { logout } = useAuth()
      return (
        <button type="button" onClick={logout}>
          模拟退出登录
        </button>
      )
    }

    const user = userEvent.setup()
    const testTracks: BaseItemDto[] = [
      {
        Id: 'track-interstellar-1',
        Name: 'Dreaming of the Crash',
        Type: 'Audio',
        MediaType: 'Audio',
        RunTimeTicks: 235 * 10_000_000,
      },
    ]

    const { container } = renderWithAppProviders(
      <Routes>
        <Route
          path="/"
          element={
            <>
              <AudioTriggerHarness tracks={testTracks} />
              <LogoutButton />
              <Layout />
            </>
          }
        >
          <Route index element={<div>首页内容</div>} />
        </Route>
      </Routes>,
      ['/'],
    )

    await user.click(screen.getByRole('button', { name: '启动测试播放队列' }))

    // play() 拒绝 -> 显示错误条且播放按钮应为「播放」（isPlaying=false）
    expect(await screen.findByTestId('audio-playback-error')).toHaveTextContent('Autoplay blocked')
    expect(screen.getByRole('button', { name: '播放' })).toBeInTheDocument()

    // Layout <main> 底部留白扩展为 pb-40 lg:pb-32
    const mainEl = container.querySelector('main')
    expect(mainEl?.className).toContain('pb-40')
    expect(mainEl?.className).toContain('lg:pb-32')

    // 关闭错误提示
    await user.click(screen.getByRole('button', { name: '关闭错误提示' }))
    expect(screen.queryByTestId('audio-playback-error')).not.toBeInTheDocument()

    // <audio onError> 触发时显示错误条
    const audioEl = container.querySelector('audio') as HTMLAudioElement
    fireEvent.error(audioEl)
    expect(await screen.findByTestId('audio-playback-error')).toBeInTheDocument()

    // 歌词抽屉与播放队列互斥打开，且队列删除按钮在触屏下可见 (opacity-70)
    await user.click(screen.getByRole('button', { name: '同步歌词' }))
    expect(await screen.findByTestId('lyrics-drawer')).toBeInTheDocument()
    expect(screen.queryByTestId('queue-popover')).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '播放队列' }))
    expect(await screen.findByTestId('queue-popover')).toBeInTheDocument()
    expect(screen.queryByTestId('lyrics-drawer')).not.toBeInTheDocument()
    const removeBtn = screen.getByRole('button', { name: '从队列移除 Dreaming of the Crash' })
    expect(removeBtn.className).toContain('opacity-70')
    expect(removeBtn.className).toContain('sm:opacity-0')

    // 退出登录后全局音乐播放器自动停止并清空队列
    await user.click(screen.getByRole('button', { name: '模拟退出登录' }))
    await waitFor(() => {
      expect(screen.queryByTestId('audio-player-bar')).not.toBeInTheDocument()
    })
  })

  it('HashRouter 视频播放页 (#/play/movie-1) 下忽略未携带 PlaySessionId 的远端 Playstate 指令', async () => {
    const user = userEvent.setup()
    const pauseSpy = vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => undefined)
    const listeners = new Set<(cmd: PlaystateCommand) => void>()
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const testTracks: BaseItemDto[] = [
      {
        Id: 'track-hash-1',
        Name: 'Hash Route Track',
        Type: 'Audio',
        MediaType: 'Audio',
        RunTimeTicks: 180 * 10_000_000,
      },
    ]

    const prevHash = window.location.hash
    try {
      render(
        <QueryClientProvider client={qc}>
          <AuthProvider>
            <WebSocketContext.Provider
              value={{
                connected: true,
                subscribePlaystate: (cb) => {
                  listeners.add(cb)
                  return () => {
                    listeners.delete(cb)
                  }
                },
              }}
            >
              <AudioPlayerProvider>
                <MemoryRouter initialEntries={['/']}>
                  <AudioTriggerHarness tracks={testTracks} />
                  <AudioPlayerBar />
                </MemoryRouter>
              </AudioPlayerProvider>
            </WebSocketContext.Provider>
          </AuthProvider>
        </QueryClientProvider>,
      )

      await user.click(screen.getByRole('button', { name: '启动测试播放队列' }))
      expect(await screen.findByTestId('audio-player-bar')).toBeInTheDocument()
      pauseSpy.mockClear()

      // HashRouter 处于视频播放路由 #/play/movie-1：无 PlaySessionId 的 Pause 指令应被抑制
      window.location.hash = '#/play/movie-1'
      act(() => {
        for (const cb of listeners) {
          cb({ Command: 'Pause' })
        }
      })
      expect(pauseSpy).not.toHaveBeenCalled()

      // 离开视频播放路由后，同样无 PlaySessionId 的 Pause 指令正常生效
      window.location.hash = '#/music'
      act(() => {
        for (const cb of listeners) {
          cb({ Command: 'Pause' })
        }
      })
      expect(pauseSpy).toHaveBeenCalledTimes(1)
    } finally {
      window.location.hash = prevHash
    }
  })
})
