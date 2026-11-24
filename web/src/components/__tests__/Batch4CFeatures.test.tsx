import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { saveAuth } from '../../api/client'
import type { BaseItemDto } from '../../api/types'
import { AudioPlayerProvider, useAudioPlayer } from '../../context/AudioPlayerContext'
import { AuthProvider } from '../../context/AuthContext'
import { setDemoModeOverride } from '../../demo/demoMode'
import { resetDemoStore } from '../../demo/mockServer'
import { AdminPage } from '../../pages/AdminPage'
import { ItemDetailPage } from '../../pages/ItemDetailPage'
import { AudioPlayerBar } from '../AudioPlayerBar'
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
})
