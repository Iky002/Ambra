import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api, saveAuth } from '../../api/client'
import type { BaseItemDto, LiveTvRecordingInfo, LiveTvTimerInfo } from '../../api/types'
import { AddToCollectionModal } from '../../components/media/AddToCollectionModal'
import { AudioPlayerProvider, useAudioPlayer } from '../../context/AudioPlayerContext'
import { AuthProvider } from '../../context/AuthContext'
import { setDemoModeOverride } from '../../demo/demoMode'
import { resetDemoStore } from '../../demo/mockServer'
import { formatDateTimeForLocale } from '../../i18n/I18nContext'
import { ItemDetailPage } from '../ItemDetailPage'
import { LiveTvPage } from '../LiveTvPage'
import { LoginPage } from '../LoginPage'
import { LiveTvTab } from '../admin/LiveTvTab'

// Mock hls.js so we can inspect instances, trigger MEDIA_ERROR, and verify fallback
const hlsInstances: Array<{
  loadSource: ReturnType<typeof vi.fn>
  attachMedia: ReturnType<typeof vi.fn>
  recoverMediaError: ReturnType<typeof vi.fn>
  destroy: ReturnType<typeof vi.fn>
  on: ReturnType<typeof vi.fn>
  emitError: (data: { fatal: boolean; type: string }) => void
}> = []

vi.mock('hls.js', () => {
  class MockHls {
    static isSupported = vi.fn(() => true)
    static Events = { ERROR: 'hlsError' }
    static ErrorTypes = {
      NETWORK_ERROR: 'networkError',
      MEDIA_ERROR: 'mediaError',
      OTHER_ERROR: 'otherError',
    }

    private handlers = new Map<string, (event: string, data: { fatal: boolean; type: string }) => void>()
    loadSource = vi.fn()
    attachMedia = vi.fn()
    recoverMediaError = vi.fn()
    destroy = vi.fn()
    on = vi.fn((event: string, cb: (event: string, data: { fatal: boolean; type: string }) => void) => {
      this.handlers.set(event, cb)
    })

    constructor() {
      hlsInstances.push({
        loadSource: this.loadSource,
        attachMedia: this.attachMedia,
        recoverMediaError: this.recoverMediaError,
        destroy: this.destroy,
        on: this.on,
        emitError: (data) => {
          this.handlers.get(MockHls.Events.ERROR)?.(MockHls.Events.ERROR, data)
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

function AudioStateProbe() {
  const { queue, currentTrack, isPlaying, playTrack } = useAudioPlayer()
  return (
    <div data-testid="audio-probe">
      <span data-testid="audio-queue-len">{queue.length}</span>
      <span data-testid="audio-current">{currentTrack?.Name || ''}</span>
      <span data-testid="audio-playing">{String(isPlaying)}</span>
      <button
        type="button"
        onClick={() =>
          playTrack({
            Id: 'global-track-1',
            Name: 'Global Song',
            Type: 'Audio',
          } as BaseItemDto)
        }
      >
        启动全局音频
      </button>
    </div>
  )
}

describe('Stream 3 (v0.9.2): LiveTv HLS/DVR/EPG, Cache Invalidation, InstantMix & ThemeSong, Login Modal Polish', () => {
  beforeEach(() => {
    hlsInstances.length = 0
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
      deviceId: 'dev-test-v092',
    })
    vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => undefined)
    vi.spyOn(HTMLMediaElement.prototype, 'load').mockImplementation(() => undefined)
    vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined)
  })

  afterEach(() => {
    vi.useRealTimers()
    setDemoModeOverride(null)
    localStorage.clear()
    sessionStorage.clear()
    vi.restoreAllMocks()
  })

  it('LiveTvPage: hls.js MEDIA_ERROR 最多恢复 2 次，第 3 次展示错误与「重试」按钮，且非 HLS 源触发 MEDIA_ERR_SRC_NOT_SUPPORTED 时自动回退 hls.js', async () => {
    setDemoModeOverride(false)
    vi.spyOn(api, 'getLiveTvChannels').mockResolvedValue({
      Items: [
        {
          Id: 'ch-hls',
          Name: 'HLS 频道',
          Number: '101',
          StreamUrl: 'https://example.com/live/stream.m3u8',
        },
        {
          Id: 'ch-raw',
          Name: '未知扩展名频道',
          Number: '102',
          StreamUrl: 'https://example.com/live/channel-raw',
        },
      ],
      TotalRecordCount: 2,
    })
    vi.spyOn(api, 'getLiveTvPrograms').mockResolvedValue({ Items: [], TotalRecordCount: 0 })

    renderWithProviders(
      <Routes>
        <Route path="/livetv" element={<LiveTvPage />} />
      </Routes>,
      ['/livetv?channel=ch-hls'],
    )

    await screen.findByTestId('livetv-video')
    expect(hlsInstances.length).toBe(1)
    const firstHls = hlsInstances[0]

    // 前 2 次 MEDIA_ERROR 触发 recoverMediaError
    act(() => {
      firstHls.emitError({ fatal: true, type: 'mediaError' })
      firstHls.emitError({ fatal: true, type: 'mediaError' })
    })
    expect(firstHls.recoverMediaError).toHaveBeenCalledTimes(2)
    expect(screen.queryByRole('alert')).toBeNull()

    // 第 3 次超过上限 -> destroy + 显示错误和「重试」按钮
    act(() => {
      firstHls.emitError({ fatal: true, type: 'mediaError' })
    })
    expect(firstHls.recoverMediaError).toHaveBeenCalledTimes(2)
    expect(firstHls.destroy).toHaveBeenCalledTimes(1)
    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toContain('媒体解码多次失败')

    // 点击「重试」重新初始化 hls.js
    fireEvent.click(within(alert).getByRole('button', { name: '重试' }))
    await waitFor(() => {
      expect(hlsInstances.length).toBe(2)
      expect(screen.queryByRole('alert')).toBeNull()
    })

    // 切换到非 .m3u8 频道：默认使用原生 <video>，不创建 hls.js 实例
    fireEvent.click(screen.getByRole('button', { name: /未知扩展名频道/ }))
    await waitFor(() => {
      expect(screen.getByTestId('livetv-epg-panel').textContent).toContain('未知扩展名频道')
    })
    const countBeforeFallback = hlsInstances.length

    // 模拟原生 <video> 触发 MEDIA_ERR_SRC_NOT_SUPPORTED (code = 4) -> 自动回退到 hls.js
    const video = screen.getByTestId('livetv-video') as HTMLVideoElement
    Object.defineProperty(video, 'error', {
      configurable: true,
      value: { code: 4 },
    })
    fireEvent.error(video)

    await waitFor(() => {
      expect(hlsInstances.length).toBe(countBeforeFallback + 1)
    })
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('LiveTvPage: 支持 DVR 预约录制、子标签切换（预约计划 / 已录节目）与取消/删除操作', async () => {
    const timersStore: LiveTvTimerInfo[] = [
      {
        Id: 'timer-1',
        ChannelId: 'ch-demo-01',
        ChannelName: 'CCTV-1 综合高清',
        Name: '新闻联播录制',
        StartDate: '2026-11-25T11:00:00Z',
        EndDate: '2026-11-25T11:30:00Z',
        Status: 'New',
      },
    ]
    const recordingsStore: LiveTvRecordingInfo[] = [
      {
        Id: 'rec-1',
        ChannelId: 'ch-demo-02',
        ChannelName: 'CCTV-6 电影频道',
        Name: '佳片有约回看',
        StartDate: '2026-11-24T12:00:00Z',
        EndDate: '2026-11-24T14:00:00Z',
        Status: 'Completed',
        Path: '/recordings/movie-night.ts',
      },
    ]

    const createTimerSpy = vi.spyOn(api, 'createLiveTvTimer').mockImplementation(async (payload) => {
      const created: LiveTvTimerInfo = {
        Id: 'timer-new',
        ChannelId: payload.ChannelId,
        ChannelName: payload.ChannelName,
        ProgramId: payload.ProgramId,
        Name: payload.Name,
        StartDate: payload.StartDate,
        EndDate: payload.EndDate,
        Status: 'New',
      }
      timersStore.push(created)
      return created
    })
    vi.spyOn(api, 'getLiveTvTimers').mockImplementation(async () => ({
      Items: [...timersStore],
      TotalRecordCount: timersStore.length,
    }))
    const deleteTimerSpy = vi.spyOn(api, 'deleteLiveTvTimer').mockImplementation(async (id) => {
      const idx = timersStore.findIndex((t) => t.Id === id)
      if (idx >= 0) timersStore.splice(idx, 1)
    })
    vi.spyOn(api, 'getLiveTvRecordings').mockImplementation(async () => ({
      Items: [...recordingsStore],
      TotalRecordCount: recordingsStore.length,
    }))
    const deleteRecSpy = vi.spyOn(api, 'deleteLiveTvRecording').mockImplementation(async (id) => {
      const idx = recordingsStore.findIndex((r) => r.Id === id)
      if (idx >= 0) recordingsStore.splice(idx, 1)
    })

    renderWithProviders(
      <Routes>
        <Route path="/livetv" element={<LiveTvPage />} />
      </Routes>,
      ['/livetv?channel=ch-demo-02'],
    )

    const epgPanel = await screen.findByTestId('livetv-epg-panel')
    const scheduleBtns = await within(epgPanel).findAllByRole('button', { name: '预约录制' })
    expect(scheduleBtns.length).toBeGreaterThanOrEqual(2)

    // 点击「预约录制」-> 调用 createLiveTvTimer 并展示内联确认
    fireEvent.click(scheduleBtns[0])
    await waitFor(() => {
      expect(createTimerSpy).toHaveBeenCalledTimes(1)
      expect(screen.getByRole('status').textContent).toContain('已预约录制')
    })

    // 切换到「预约计划」标签 -> 列出并取消预约
    fireEvent.click(screen.getByRole('tab', { name: '预约计划' }))
    const timersPanel = await screen.findByTestId('livetv-timers-panel')
    await within(timersPanel).findByText('新闻联播录制')
    fireEvent.click(within(timersPanel).getAllByRole('button', { name: '取消预约' })[0])
    await waitFor(() => {
      expect(deleteTimerSpy).toHaveBeenCalledWith('timer-1')
    })

    // 切换到「已录节目」标签 -> 列出并删除录制
    fireEvent.click(screen.getByRole('tab', { name: '已录节目' }))
    const recPanel = await screen.findByTestId('livetv-recordings-panel')
    await within(recPanel).findByText('佳片有约回看')
    fireEvent.click(within(recPanel).getByRole('button', { name: '删除录制' }))
    await waitFor(() => {
      expect(deleteRecSpy).toHaveBeenCalledWith('rec-1')
    })
  })

  it('LiveTvPage: 30 秒定时器实时刷新 EPG 进度条', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const baseTime = new Date('2026-11-25T10:00:00Z').getTime()
    vi.setSystemTime(baseTime)

    vi.spyOn(api, 'getLiveTvChannels').mockResolvedValue({
      Items: [{ Id: 'ch-1', Name: '测试台', Number: '1' }],
      TotalRecordCount: 1,
    })
    vi.spyOn(api, 'getLiveTvPrograms').mockResolvedValue({
      Items: [
        {
          Id: 'prog-1',
          ChannelId: 'ch-1',
          Name: '午间新闻',
          StartDate: '2026-11-25T10:00:00Z',
          EndDate: '2026-11-25T10:10:00Z',
        },
      ],
      TotalRecordCount: 1,
    })

    renderWithProviders(
      <Routes>
        <Route path="/livetv" element={<LiveTvPage />} />
      </Routes>,
      ['/livetv'],
    )

    const progressEl = await screen.findByTestId('epg-current-progress')
    expect(progressEl.style.width).toBe('0%')

    // 前进 3 分钟（6 个 30 秒 tick），进度应更新到 30%
    await act(async () => {
      vi.advanceTimersByTime(180_000)
    })
    expect(screen.getByTestId('epg-current-progress').style.width).toBe('30%')
  })

  it('LiveTvTab 添加与删除直播源时同步失效 liveTvInfo 与 liveTv 缓存', async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const invalidateSpy = vi.spyOn(qc, 'invalidateQueries')

    vi.spyOn(api, 'getTunerHosts').mockResolvedValue([
      { Id: 'tuner-1', Type: 'm3u', FriendlyName: '测试源', Url: 'https://example.com/a.m3u' },
    ])
    vi.spyOn(api, 'getListingProviders').mockResolvedValue([])
    vi.spyOn(api, 'getLiveTvChannels').mockResolvedValue({ Items: [], TotalRecordCount: 0 })
    vi.spyOn(api, 'addTunerHost').mockResolvedValue({
      Id: 'tuner-2',
      Type: 'm3u',
      FriendlyName: '新源',
      Url: 'https://example.com/b.m3u',
    })
    vi.spyOn(api, 'deleteTunerHost').mockResolvedValue(undefined)

    renderWithProviders(<LiveTvTab />, ['/admin/livetv'], qc)
    await screen.findByText('测试源')

    // 添加直播源
    fireEvent.change(screen.getByLabelText(/M3U 地址/), {
      target: { value: 'https://example.com/b.m3u' },
    })
    fireEvent.click(screen.getByRole('button', { name: '添加源' }))

    await waitFor(() => {
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['liveTvInfo'] })
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['liveTv'] })
    })

    invalidateSpy.mockClear()

    // 删除直播源（兼容确认删除调谐器弹窗）
    fireEvent.click(screen.getByRole('button', { name: '删除' }))
    const confirmTunerBtn = screen.queryByTestId('confirm-delete-tuner-btn')
    if (confirmTunerBtn) {
      fireEvent.click(confirmTunerBtn)
    }
    await waitFor(() => {
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['liveTvInfo'] })
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['liveTv'] })
    })
  })

  it('AddToCollectionModal 加入合集后同时失效 collections 与 collectionItems 缓存', async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const invalidateSpy = vi.spyOn(qc, 'invalidateQueries')

    vi.spyOn(api, 'getItems').mockResolvedValue({
      Items: [{ Id: 'col-1', Name: '科幻精选', Type: 'BoxSet' } as BaseItemDto],
      TotalRecordCount: 1,
    })
    vi.spyOn(api, 'addCollectionItems').mockResolvedValue(undefined)

    renderWithProviders(
      <AddToCollectionModal
        open
        itemIds={['movie-1']}
        itemLabel="测试电影"
        onClose={vi.fn()}
      />,
      ['/'],
      qc,
    )

    // 等待服务端验证管理员权限后切换到「合集」标签
    await screen.findByRole('tab', { name: /合集/ })
    const colBtn = await screen.findByRole('button', { name: /科幻精选/ })
    fireEvent.click(colBtn)

    await waitFor(() => {
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['collections'] })
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['collectionItems'] })
    })
  })

  it('ItemDetailPage: 音乐专辑支持「即时混音」并将曲目送入全局播放队列', async () => {
    const mixTracks: BaseItemDto[] = [
      { Id: 'mix-1', Name: 'Mix Track 1', Type: 'Audio' },
      { Id: 'mix-2', Name: 'Mix Track 2', Type: 'Audio' },
    ]
    vi.spyOn(api, 'getItem').mockResolvedValue({
      Id: 'album-1',
      Name: '经典原声专辑',
      Type: 'MusicAlbum',
    } as BaseItemDto)
    vi.spyOn(api, 'getItems').mockResolvedValue({ Items: [], TotalRecordCount: 0 })
    vi.spyOn(api, 'getSimilarItems').mockResolvedValue({ Items: [], TotalRecordCount: 0 })
    const mixSpy = vi.spyOn(api, 'getInstantMix').mockResolvedValue({
      Items: mixTracks,
      TotalRecordCount: mixTracks.length,
    })

    renderWithProviders(
      <>
        <AudioStateProbe />
        <Routes>
          <Route path="/item/:id" element={<ItemDetailPage />} />
        </Routes>
      </>,
      ['/item/album-1'],
    )

    const mixBtn = await screen.findByRole('button', { name: '即时混音' })
    fireEvent.click(mixBtn)

    await waitFor(() => {
      expect(mixSpy).toHaveBeenCalledWith('album-1', 'user-demo-admin', 50)
      expect(screen.getByTestId('audio-queue-len').textContent).toBe('2')
      expect(screen.getByTestId('audio-current').textContent).toBe('Mix Track 1')
    })
  })

  it('ItemDetailPage: 主题曲加载失败弹出错误提示，且全局音乐开始播放时自动停止主题曲', async () => {
    vi.spyOn(api, 'getItem').mockResolvedValue({
      Id: 'movie-theme-1',
      Name: '星际穿越',
      Type: 'Movie',
    } as BaseItemDto)
    vi.spyOn(api, 'getSimilarItems').mockResolvedValue({ Items: [], TotalRecordCount: 0 })
    vi.spyOn(api, 'getLocalTrailers').mockResolvedValue([])
    vi.spyOn(api, 'getSpecialFeatures').mockResolvedValue([])
    vi.spyOn(api, 'getThemeSongs').mockResolvedValue({
      Items: [{ Id: 'theme-1', Name: 'Cornfield Chase', Type: 'Audio' } as BaseItemDto],
      TotalRecordCount: 1,
    })

    renderWithProviders(
      <>
        <AudioStateProbe />
        <Routes>
          <Route path="/item/:id" element={<ItemDetailPage />} />
        </Routes>
      </>,
      ['/item/movie-theme-1'],
    )

    // 1. 播放主题曲 -> 触发 onError -> 自动停止并提示「主题曲加载失败」
    const playThemeBtn = await screen.findByRole('button', { name: '播放主题曲' })
    fireEvent.click(playThemeBtn)
    await screen.findByRole('button', { name: '停止主题曲' })

    const audioEl = screen.getByTestId('theme-song-audio')
    fireEvent.error(audioEl)

    await waitFor(() => {
      expect(screen.getByText('主题曲加载失败')).toBeTruthy()
      expect(screen.getByRole('button', { name: '播放主题曲' })).toBeTruthy()
    })

    // 2. 再次播放主题曲 -> 启动全局音乐播放器 -> 自动停止主题曲（互斥）
    fireEvent.click(screen.getByRole('button', { name: '播放主题曲' }))
    await screen.findByRole('button', { name: '停止主题曲' })

    fireEvent.click(screen.getByRole('button', { name: '启动全局音频' }))
    await waitFor(() => {
      expect(screen.getByTestId('audio-playing').textContent).toBe('true')
      expect(screen.getByRole('button', { name: '播放主题曲' })).toBeTruthy()
    })
  })

  it('LoginPage: 忘记密码弹窗支持 Escape 关闭、loading 时禁止遮罩/Esc 关闭并格式化过期时间', async () => {
    localStorage.clear()
    let resolveForgot!: (val: { Action: string; PinFile: string; PinExpirationDate: string }) => void
    vi.spyOn(api, 'forgotPassword').mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveForgot = resolve
        }),
    )

    renderWithProviders(
      <Routes>
        <Route path="/login" element={<LoginPage />} />
      </Routes>,
      ['/login'],
    )

    // 打开弹窗并用 Escape 关闭
    fireEvent.click(screen.getByRole('button', { name: '忘记密码？' }))
    expect(screen.getByRole('dialog')).toBeTruthy()
    fireEvent.keyDown(window, { key: 'Escape' })
    expect(screen.queryByRole('dialog')).toBeNull()

    // 重新打开并提交第一步进入 loading 状态
    fireEvent.click(screen.getByRole('button', { name: '忘记密码？' }))
    fireEvent.change(screen.getByLabelText('用户名'), { target: { value: 'admin' } })
    fireEvent.click(screen.getByRole('button', { name: '下一步' }))

    // loading 期间点击遮罩或按 Escape 不应关闭弹窗
    const dialog = screen.getByRole('dialog')
    fireEvent.click(dialog)
    fireEvent.keyDown(window, { key: 'Escape' })
    expect(screen.getByRole('dialog')).toBeTruthy()

    // 完成请求 -> 展示本地化后的过期时间（使用 i18n formatDateTime）
    const isoDate = '2026-11-25T12:30:00.000Z'
    const expectedFormatted = formatDateTimeForLocale('zh-CN', isoDate)
    await act(async () => {
      resolveForgot({
        Action: 'PinCode',
        PinFile: '/data/passwordreset-01.txt',
        PinExpirationDate: isoDate,
      })
    })

    await waitFor(() => {
      expect(screen.getByText(`有效期至：${expectedFormatted}`)).toBeTruthy()
    })
  })
})
