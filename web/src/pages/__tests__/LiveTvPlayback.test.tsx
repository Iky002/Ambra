import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api, embyFetch, saveAuth } from '../../api/client'
import { Layout } from '../../components/Layout'
import { AudioPlayerProvider } from '../../context/AudioPlayerContext'
import { AuthProvider } from '../../context/AuthContext'
import { setDemoModeOverride } from '../../demo/demoMode'
import { resetDemoStore } from '../../demo/mockServer'
import { LibraryPage } from '../LibraryPage'
import { LiveTvPage } from '../LiveTvPage'
import { HealthAndAuditTab } from '../admin/HealthAndAuditTab'
import { LiveTvTab } from '../admin/LiveTvTab'

const hlsInstances: Array<{
  config: Record<string, unknown>
  loadSource: ReturnType<typeof vi.fn>
  attachMedia: ReturnType<typeof vi.fn>
  recoverMediaError: ReturnType<typeof vi.fn>
  swapAudioCodec: ReturnType<typeof vi.fn>
  startLoad: ReturnType<typeof vi.fn>
  destroy: ReturnType<typeof vi.fn>
  on: ReturnType<typeof vi.fn>
  emit: (event: string, data?: unknown) => void
}> = []

vi.mock('hls.js', () => {
  class MockHls {
    static isSupported = vi.fn(() => true)
    static Events = { ERROR: 'hlsError', FRAG_BUFFERED: 'hlsFragBuffered' }
    static ErrorTypes = {
      NETWORK_ERROR: 'networkError',
      MEDIA_ERROR: 'mediaError',
      OTHER_ERROR: 'otherError',
    }

    private handlers = new Map<string, Array<(event: string, data?: unknown) => void>>()
    loadSource = vi.fn()
    attachMedia = vi.fn()
    recoverMediaError = vi.fn()
    swapAudioCodec = vi.fn()
    startLoad = vi.fn()
    destroy = vi.fn()
    on = vi.fn((event: string, cb: (event: string, data?: unknown) => void) => {
      const list = this.handlers.get(event) ?? []
      list.push(cb)
      this.handlers.set(event, list)
    })

    constructor(config: Record<string, unknown> = {}) {
      hlsInstances.push({
        config,
        loadSource: this.loadSource,
        attachMedia: this.attachMedia,
        recoverMediaError: this.recoverMediaError,
        swapAudioCodec: this.swapAudioCodec,
        startLoad: this.startLoad,
        destroy: this.destroy,
        on: this.on,
        emit: (event: string, data?: unknown) => {
          for (const cb of this.handlers.get(event) ?? []) {
            cb(event, data)
          }
        },
      })
    }
  }
  return { default: MockHls }
})

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

function LocationDisplay() {
  const loc = useLocation()
  return <div data-testid="location-search">{loc.search}</div>
}

describe('Stream 4 (v0.9.1): Live TV Watch Page, Library Multi-Select Polish, Backup Inline Feedback & Demo Mocks', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    resetDemoStore()
    setDemoModeOverride(true)
    saveAuth({
      accessToken: 'demo_tok_admin',
      userId: 'user-demo-admin',
      serverId: 'ambra-serverless-demo-01',
      userName: 'admin',
      isAdmin: true,
      deviceId: 'dev-test-v091',
    })
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

  it('LiveTvPage 支持 ?channel= 预选、EPG 节目单展示、频道搜索与快速切换', async () => {
    renderWithProviders(
      <>
        <LocationDisplay />
        <Routes>
          <Route path="/livetv" element={<LiveTvPage />} />
        </Routes>
      </>,
      ['/livetv?channel=ch-demo-02'],
    )

    // 预选 CCTV-6 电影频道并渲染播放器和 EPG
    const video = await screen.findByTestId('livetv-video')
    expect(video).toBeTruthy()
    const epgPanel = await screen.findByTestId('livetv-epg-panel')
    expect(within(epgPanel).getByText('CCTV-6 电影频道')).toBeTruthy()
    await waitFor(() => {
      expect(within(epgPanel).getByText('佳片有约：经典科幻之夜')).toBeTruthy()
      expect(within(epgPanel).getByText('中国电影报道')).toBeTruthy()
    })

    // 搜索过滤频道
    const searchInput = screen.getByLabelText('搜索频道')
    fireEvent.change(searchInput, { target: { value: '401' } })
    const list = screen.getByTestId('livetv-channel-list')
    expect(within(list).getByText('4K 超高清测试台')).toBeTruthy()
    expect(within(list).queryByText('CCTV-1 综合高清')).toBeNull()

    // 点击切换到 4K 超高清测试台，同步更新 ?channel=ch-demo-03
    fireEvent.click(within(list).getByRole('button', { name: /4K 超高清测试台/ }))
    await waitFor(() => {
      expect(screen.getByTestId('location-search').textContent).toContain('channel=ch-demo-03')
      expect(within(epgPanel).getByText('4K 超高清测试台')).toBeTruthy()
    })
  })

  it('LiveTvPage 处理空频道状态（管理员可见配置入口）与接口错误状态（QueryErrorState）', async () => {
    vi.spyOn(api, 'getLiveTvChannels').mockResolvedValueOnce({ Items: [], TotalRecordCount: 0 })

    const { unmount } = renderWithProviders(
      <Routes>
        <Route path="/livetv" element={<LiveTvPage />} />
      </Routes>,
      ['/livetv'],
    )

    await screen.findByText('暂无可观看的电视频道')
    const configLink = screen.getByRole('link', { name: /前往配置直播源/ })
    expect(configLink.getAttribute('href')).toBe('/admin/livetv')
    unmount()

    vi.spyOn(api, 'getLiveTvChannels').mockRejectedValueOnce(new Error('Live TV 服务离线'))
    renderWithProviders(
      <Routes>
        <Route path="/livetv" element={<LiveTvPage />} />
      </Routes>,
      ['/livetv'],
    )

    await screen.findByTestId('query-error-state')
    expect(screen.getByText('无法加载电视频道')).toBeTruthy()
    expect(screen.getByText('Live TV 服务离线')).toBeTruthy()
  })

  it('Layout 侧边栏在 Live TV 启用时展示「电视直播」导航，LiveTvTab 频道卡片包含「观看」跳转链接', async () => {
    const { unmount } = renderWithProviders(
      <Routes>
        <Route path="/" element={<Layout />}>
          <Route index element={<div>Home</div>} />
        </Route>
      </Routes>,
      ['/'],
    )

    const liveTvNav = await screen.findByRole('link', { name: '电视直播' })
    expect(liveTvNav.getAttribute('href')).toBe('/livetv')
    unmount()

    renderWithProviders(<LiveTvTab />, ['/admin/livetv'])
    const watchLinks = await screen.findAllByRole('link', { name: '观看' })
    expect(watchLinks.length).toBeGreaterThanOrEqual(3)
    expect(watchLinks[0].getAttribute('href')).toBe('/livetv?channel=ch-demo-01')
  })

  it('LibraryPage 切换筛选条件或音乐子标签时自动清空已选项，防止隐藏项误入批量操作', async () => {
    renderWithProviders(
      <Routes>
        <Route path="/library/:id" element={<LibraryPage />} />
      </Routes>,
      ['/library/lib-movies'],
    )

    await screen.findByTestId('library-grid-view')

    // 开启多选并全选本页
    fireEvent.click(screen.getByRole('button', { name: '多选' }))
    const bar = await screen.findByTestId('batch-action-bar')
    fireEvent.click(within(bar).getByRole('button', { name: '全选本页' }))
    await waitFor(() => {
      expect(within(bar).getByText(/已选 [1-9]\d* 项/)).toBeTruthy()
    })

    // 切换状态筛选 -> 清空选择
    fireEvent.change(screen.getByLabelText('按观看状态筛选'), { target: { value: 'IsUnplayed' } })
    await waitFor(() => {
      expect(within(bar).getByText('已选 0 项')).toBeTruthy()
    })

    // 再次全选，切换字母筛选 -> 清空选择
    fireEvent.click(within(bar).getByRole('button', { name: '全选本页' }))
    await waitFor(() => {
      expect(within(bar).getByText(/已选 [1-9]\d* 项/)).toBeTruthy()
    })
    fireEvent.click(screen.getByRole('button', { name: 'X' }))
    await waitFor(() => {
      expect(within(bar).getByText('已选 0 项')).toBeTruthy()
    })
  })

  it('HealthAndAuditTab 在「数据库备份与恢复」卡片内部直接展示内联备份与恢复反馈', async () => {
    renderWithProviders(<HealthAndAuditTab />, ['/admin/health'])

    const backupCard = await screen.findByTestId('backup-restore-card')
    await screen.findByText('ambra-backup-20261124-030000.db')

    // 创建备份 -> 反馈直接显示在 backup-restore-card 内部
    fireEvent.click(screen.getByRole('button', { name: '立即创建备份' }))
    await waitFor(() => {
      expect(within(backupCard).getByText(/已创建数据库备份/)).toBeTruthy()
    })

    // 恢复备份 -> 反馈同样显示在 backup-restore-card 内部
    const restoreBtns = within(backupCard).getAllByRole('button', { name: '恢复此备份' })
    fireEvent.click(restoreBtns[0])
    await screen.findByText('确认恢复数据库备份')
    fireEvent.click(screen.getByRole('button', { name: '确认恢复' }))

    await waitFor(() => {
      expect(within(backupCard).getByText(/已暂存恢复文件/)).toBeTruthy()
    })
  })

  it('Demo Mock 完整覆盖 ForgotPassword / Pin / ThemeSongs / LiveTv Programs & Stream', async () => {
    // 1. ForgotPassword（文件名绝不嵌入 PIN）
    const forgotRes = await api.forgotPassword('admin')
    expect(forgotRes.Action).toBe('PinCode')
    expect(forgotRes.PinFile).toBe('/demo-data/passwordreset-demo01.txt')
    expect(forgotRes.PinFile).not.toMatch(/\d{6}/)

    // 2. ForgotPassword/Pin
    const pinRes1 = await api.forgotPasswordPin('123456')
    expect(pinRes1).toEqual({ Success: true, UsersReset: ['admin'] })
    const pinRes2 = await api.forgotPasswordPin('654321')
    expect(pinRes2).toEqual({ Success: true, UsersReset: ['admin'] })
    await expect(api.forgotPasswordPin('abc')).rejects.toThrow()

    // 3. ThemeSongs
    const themes = await api.getThemeSongs('item-shawshank-redemption')
    expect(themes.TotalRecordCount).toBe(1)
    expect(themes.Items[0].Type).toBe('Audio')
    expect(themes.Items[0].Name).toContain('原声主题曲')

    // 4. LiveTv/Programs（全量与按频道筛选）
    const allProgs = await api.getLiveTvPrograms()
    expect(allProgs.Items.length).toBeGreaterThanOrEqual(5)
    const ch1Progs = await api.getLiveTvPrograms(['ch-demo-01'])
    expect(ch1Progs.Items.length).toBeGreaterThan(0)
    expect(ch1Progs.Items.every((p) => p.ChannelId === 'ch-demo-01')).toBe(true)

    // 5. LiveTv/Channels/{Id}/Stream
    const streamRes = await embyFetch<{ ChannelId: string; StreamUrl: string }>(
      '/emby/LiveTv/Channels/ch-demo-01/Stream',
    )
    expect(streamRes.ChannelId).toBe('ch-demo-01')
    expect(streamRes.StreamUrl).toContain('data:video/mp4;base64,')
  })
})

describe('LiveTV Playback Hardening & Tuner Host Refresh UI', () => {
  beforeEach(() => {
    hlsInstances.length = 0
    localStorage.clear()
    sessionStorage.clear()
    resetDemoStore()
    setDemoModeOverride(false)
    saveAuth({
      accessToken: 'tok_admin_hardening',
      userId: 'user-demo-admin',
      serverId: 'ambra-test-server',
      userName: 'admin',
      isAdmin: true,
      deviceId: 'dev-test-hardening',
    })
    vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => undefined)
    vi.spyOn(HTMLMediaElement.prototype, 'load').mockImplementation(() => undefined)
    vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined)
    vi.spyOn(api, 'getLiveTvPrograms').mockResolvedValue({ Items: [], TotalRecordCount: 0 })
    vi.spyOn(api, 'getLiveTvTimers').mockResolvedValue({ Items: [], TotalRecordCount: 0 })
    vi.spyOn(api, 'getSeriesTimers').mockResolvedValue({ Items: [], TotalRecordCount: 0 })
  })

  afterEach(() => {
    vi.useRealTimers()
    setDemoModeOverride(null)
    localStorage.clear()
    sessionStorage.clear()
    vi.restoreAllMocks()
  })

  it('连续 10 次 fatal MEDIA_ERROR 按退避延迟恢复（偶数次调用 swapAudioCodec），第 11 次销毁并显示 details', async () => {
    vi.spyOn(api, 'getLiveTvChannels').mockResolvedValue({
      Items: [
        {
          Id: 'ch-live-1',
          Name: 'CCTV-1 HLS',
          Number: '1',
          IsHls: true,
          StreamUrl: 'https://example.com/live/1.m3u8',
        },
      ],
      TotalRecordCount: 1,
    })

    renderWithProviders(
      <Routes>
        <Route path="/livetv" element={<LiveTvPage />} />
      </Routes>,
      ['/livetv?channel=ch-live-1'],
    )

    await screen.findByTestId('livetv-video')
    expect(hlsInstances).toHaveLength(1)
    const hls = hlsInstances[0]
    expect(hls.config).toMatchObject({
      enableWorker: true,
      lowLatencyMode: false,
      liveSyncDurationCount: 3,
      liveMaxLatencyDurationCount: 10,
      maxBufferLength: 30,
      backBufferLength: 30,
      manifestLoadingMaxRetry: 4,
      levelLoadingMaxRetry: 4,
      fragLoadingMaxRetry: 6,
    })

    vi.useFakeTimers()
    for (let attempt = 1; attempt <= 10; attempt += 1) {
      const expectedDelay = Math.min(500 * attempt, 3000)
      act(() => {
        hls.emit('hlsError', {
          fatal: true,
          type: 'mediaError',
          details: 'bufferAppendError',
        })
      })
      // 尚未到延迟时间，不立即触发恢复
      act(() => {
        vi.advanceTimersByTime(expectedDelay - 1)
      })
      expect(hls.recoverMediaError).toHaveBeenCalledTimes(attempt - 1)

      act(() => {
        vi.advanceTimersByTime(1)
      })
      expect(hls.recoverMediaError).toHaveBeenCalledTimes(attempt)
      expect(hls.swapAudioCodec).toHaveBeenCalledTimes(Math.floor(attempt / 2))
    }
    expect(screen.queryByRole('alert')).toBeNull()

    // 第 11 次超过上限 -> destroy 并展示含 details 的错误
    act(() => {
      hls.emit('hlsError', {
        fatal: true,
        type: 'mediaError',
        details: 'bufferAppendError',
      })
    })
    vi.useRealTimers()
    expect(hls.destroy).toHaveBeenCalledTimes(1)
    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toContain('媒体解码多次失败')
    expect(alert.textContent).toContain('(bufferAppendError)')
  })

  it('恢复后稳定播放 15 秒（FRAG_BUFFERED / playing）自动重置恢复计数，后续 MEDIA_ERROR 不会累积失败', async () => {
    vi.spyOn(api, 'getLiveTvChannels').mockResolvedValue({
      Items: [
        {
          Id: 'ch-stable',
          Name: 'Stable Channel',
          Number: '2',
          IsHls: true,
          StreamUrl: 'https://example.com/live/stable.m3u8',
        },
      ],
      TotalRecordCount: 1,
    })

    renderWithProviders(
      <Routes>
        <Route path="/livetv" element={<LiveTvPage />} />
      </Routes>,
      ['/livetv?channel=ch-stable'],
    )

    const video = await screen.findByTestId('livetv-video')
    expect(hlsInstances).toHaveLength(1)
    const hls = hlsInstances[0]

    vi.useFakeTimers()
    // 连续触发 8 次 MEDIA_ERROR
    for (let i = 1; i <= 8; i += 1) {
      act(() => {
        hls.emit('hlsError', { fatal: true, type: 'mediaError', details: 'bufferStalledError' })
      })
      act(() => {
        vi.advanceTimersByTime(Math.min(500 * i, 3000))
      })
    }
    expect(hls.recoverMediaError).toHaveBeenCalledTimes(8)

    // 触发稳定播放信号并经过 15 秒 -> 计数清零
    act(() => {
      hls.emit('hlsFragBuffered', {})
      fireEvent.playing(video)
    })
    act(() => {
      vi.advanceTimersByTime(15_000)
    })

    // 再触发 5 次 MEDIA_ERROR（若未重置则会超过 10 次上限并报错；重置后应全部恢复）
    for (let i = 1; i <= 5; i += 1) {
      act(() => {
        hls.emit('hlsError', { fatal: true, type: 'mediaError', details: 'bufferStalledError' })
      })
      act(() => {
        vi.advanceTimersByTime(Math.min(500 * i, 3000))
      })
    }
    vi.useRealTimers()
    expect(hls.recoverMediaError).toHaveBeenCalledTimes(13)
    expect(hls.destroy).not.toHaveBeenCalled()
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('bufferAddCodecError 立即快速失败，展示编码不支持错误与 (bufferAddCodecError) 而不重试', async () => {
    vi.spyOn(api, 'getLiveTvChannels').mockResolvedValue({
      Items: [
        {
          Id: 'ch-codec',
          Name: 'HEVC Channel',
          Number: '3',
          IsHls: true,
          StreamUrl: 'https://example.com/live/hevc.m3u8',
        },
      ],
      TotalRecordCount: 1,
    })

    renderWithProviders(
      <Routes>
        <Route path="/livetv" element={<LiveTvPage />} />
      </Routes>,
      ['/livetv?channel=ch-codec'],
    )

    await screen.findByTestId('livetv-video')
    const hls = hlsInstances[0]

    act(() => {
      hls.emit('hlsError', {
        fatal: true,
        type: 'mediaError',
        details: 'bufferAddCodecError',
      })
    })

    expect(hls.destroy).toHaveBeenCalledTimes(1)
    expect(hls.recoverMediaError).not.toHaveBeenCalled()
    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toContain('不支持该频道的音视频编码格式')
    expect(alert.textContent).toContain('(bufferAddCodecError)')
  })

  it('fatal NETWORK_ERROR 按延迟调用 startLoad 最多 10 次，15 秒稳定播放可重置，第 11 次销毁并显示 details', async () => {
    vi.spyOn(api, 'getLiveTvChannels').mockResolvedValue({
      Items: [
        {
          Id: 'ch-net',
          Name: 'Network Channel',
          Number: '4',
          IsHls: true,
          StreamUrl: 'https://example.com/live/net.m3u8',
        },
      ],
      TotalRecordCount: 1,
    })

    renderWithProviders(
      <Routes>
        <Route path="/livetv" element={<LiveTvPage />} />
      </Routes>,
      ['/livetv?channel=ch-net'],
    )

    await screen.findByTestId('livetv-video')
    const hls = hlsInstances[0]

    vi.useFakeTimers()
    // 先触发 2 次并等待稳定播放 15 秒重置
    for (let i = 1; i <= 2; i += 1) {
      act(() => {
        hls.emit('hlsError', { fatal: true, type: 'networkError', details: 'fragLoadError' })
      })
      act(() => {
        vi.advanceTimersByTime(Math.min(500 * i, 3000))
      })
    }
    expect(hls.startLoad).toHaveBeenCalledTimes(2)

    act(() => {
      hls.emit('hlsFragBuffered', {})
    })
    act(() => {
      vi.advanceTimersByTime(15_000)
    })

    // 重置后再连续触发 10 次 NETWORK_ERROR 均恢复
    for (let attempt = 1; attempt <= 10; attempt += 1) {
      const expectedDelay = Math.min(500 * attempt, 3000)
      act(() => {
        hls.emit('hlsError', { fatal: true, type: 'networkError', details: 'fragLoadTimeOut' })
      })
      act(() => {
        vi.advanceTimersByTime(expectedDelay)
      })
    }
    expect(hls.startLoad).toHaveBeenCalledTimes(12)
    expect(screen.queryByRole('alert')).toBeNull()

    // 第 11 次销毁并报错
    act(() => {
      hls.emit('hlsError', { fatal: true, type: 'networkError', details: 'fragLoadTimeOut' })
    })
    vi.useRealTimers()
    expect(hls.destroy).toHaveBeenCalledTimes(1)
    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toContain('网络连接失败')
    expect(alert.textContent).toContain('(fragLoadTimeOut)')
  })

  it('快速切换频道忽略旧 Hls 实例错误回调，且 channels 查询刷新不重建当前活跃 Hls 实例', async () => {
    const getChannelsSpy = vi.spyOn(api, 'getLiveTvChannels').mockImplementation(async () => ({
      Items: [
        {
          Id: 'ch-a',
          Name: '频道 A',
          Number: '10',
          IsHls: true,
          StreamUrl: 'https://example.com/live/a.m3u8',
        },
        {
          Id: 'ch-b',
          Name: '频道 B',
          Number: '20',
          IsHls: true,
          StreamUrl: 'https://example.com/live/b.m3u8',
        },
      ],
      TotalRecordCount: 2,
    }))

    const { qc } = renderWithProviders(
      <Routes>
        <Route path="/livetv" element={<LiveTvPage />} />
      </Routes>,
      ['/livetv?channel=ch-a'],
    )

    await screen.findByTestId('livetv-video')
    expect(hlsInstances).toHaveLength(1)
    const firstHls = hlsInstances[0]

    // 触发 channels query refetch（返回新对象引用），不应重建 Hls 实例
    await act(async () => {
      await qc.invalidateQueries({ queryKey: ['liveTv', 'channels'] })
    })
    await waitFor(() => {
      expect(getChannelsSpy).toHaveBeenCalledTimes(2)
    })
    expect(hlsInstances).toHaveLength(1)
    expect(firstHls.destroy).not.toHaveBeenCalled()

    // 快速切换到频道 B
    fireEvent.click(screen.getByRole('button', { name: /频道 B/ }))
    await waitFor(() => {
      expect(hlsInstances).toHaveLength(2)
    })
    expect(firstHls.destroy).toHaveBeenCalledTimes(1)

    // 旧 Hls 实例在切换后触发 fatal error，应被完全忽略
    act(() => {
      firstHls.emit('hlsError', {
        fatal: true,
        type: 'otherError',
        details: 'staleInstanceError',
      })
    })
    expect(screen.queryByRole('alert')).toBeNull()
    expect(hlsInstances[1].destroy).not.toHaveBeenCalled()
  })

  it('LiveTvTab 为含 Url 的直播源展示刷新按钮、对无 Url 内联源隐藏，点击调用 refreshTunerHost 并展示成功反馈', async () => {
    vi.spyOn(api, 'getTunerHosts').mockResolvedValue([
      {
        Id: 'tuner-url-1',
        Type: 'm3u',
        FriendlyName: '远程 IPTV 源',
        Url: 'https://example.com/iptv.m3u',
      },
      {
        Id: 'tuner-inline-2',
        Type: 'm3u',
        FriendlyName: '内联 M3U 源',
        Url: '',
      },
    ])
    vi.spyOn(api, 'getListingProviders').mockResolvedValue([])
    vi.spyOn(api, 'getLiveTvChannels').mockResolvedValue({ Items: [], TotalRecordCount: 0 })
    const refreshSpy = vi.spyOn(api, 'refreshTunerHost').mockResolvedValue({
      Id: 'tuner-url-1',
      Type: 'm3u',
      FriendlyName: '远程 IPTV 源',
      Url: 'https://example.com/iptv.m3u',
      ImportedChannelCount: 42,
    })

    renderWithProviders(<LiveTvTab />, ['/admin/livetv'])

    await screen.findByText('远程 IPTV 源')
    await screen.findByText('内联 M3U 源')

    const refreshBtn = screen.getByTestId('refresh-tuner-tuner-url-1')
    expect(refreshBtn).toBeTruthy()
    expect(screen.queryByTestId('refresh-tuner-tuner-inline-2')).toBeNull()

    fireEvent.click(refreshBtn)
    await waitFor(() => {
      expect(refreshSpy).toHaveBeenCalledWith('tuner-url-1')
      expect(screen.getByText(/已刷新直播源「远程 IPTV 源」，共导入 42 个频道/)).toBeTruthy()
    })
  })

  it('Demo Mock 支持 POST /emby/LiveTv/TunerHosts/{Id}/Refresh（存在返回 200 及频道数，不存在返回 404）', async () => {
    setDemoModeOverride(true)
    saveAuth({
      accessToken: 'demo_tok_admin',
      userId: 'user-demo-admin',
      serverId: 'ambra-serverless-demo-01',
      userName: 'admin',
      isAdmin: true,
      deviceId: 'dev-test-hardening',
    })
    const refreshed = await api.refreshTunerHost('tuner-demo-01')
    expect(refreshed.Id).toBe('tuner-demo-01')
    expect(refreshed.ImportedChannelCount).toBeGreaterThanOrEqual(3)

    await expect(api.refreshTunerHost('tuner-nonexistent')).rejects.toThrow()
  })
})

