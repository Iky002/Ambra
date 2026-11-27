import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import Hls from 'hls.js'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api, getAuthStorageKey, saveAuth } from '../../api/client'
import type { BaseItemDto, UserDto } from '../../api/types'
import { AudioPlayerBar, parseLrcText } from '../../components/AudioPlayerBar'
import { IdentifyModal } from '../../components/media/IdentifyModal'
import { MetadataEditorModal } from '../../components/media/MetadataEditorModal'
import { AudioPlayerProvider, useAudioPlayer } from '../../context/AudioPlayerContext'
import { AuthProvider, useAuth } from '../../context/AuthContext'
import { WebSocketContext, type PlaystateCommand } from '../../context/WebSocketContext'
import { setDemoModeOverride } from '../../demo/demoMode'
import { I18nProvider } from '../../i18n/I18nContext'
import { StreamMkvRemuxer } from '../../player/streamMkvRemuxer'
import { CollectionsPage } from '../CollectionsPage'
import { ItemDetailPage } from '../ItemDetailPage'
import { LibraryPage } from '../LibraryPage'
import { LiveTvPage } from '../LiveTvPage'
import { LoginPage } from '../LoginPage'
import { PlayerPage } from '../PlayerPage'

function createQc() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
}

const ADMIN_USER: UserDto = {
  Id: 'u-admin',
  Name: 'admin',
  ServerId: 'srv-1',
  Policy: {
    IsAdministrator: true,
    EnableContentDeletion: true,
    EnableLiveTvAccess: true,
    EnableLiveTvManagement: true,
  },
}

function seedAdminAuth(token = 'tok-worker-d') {
  saveAuth({
    accessToken: token,
    userId: ADMIN_USER.Id,
    serverId: 'srv-1',
    userName: ADMIN_USER.Name,
    isAdmin: true,
    deviceId: 'dev-worker-d',
  })
}

function renderWithApp(
  ui: React.ReactNode,
  initialEntries: string[] = ['/'],
  playstateListeners?: Set<(cmd: PlaystateCommand) => void>,
) {
  const qc = createQc()
  const wsValue = {
    connected: true,
    subscribePlaystate: (cb: (cmd: PlaystateCommand) => void) => {
      playstateListeners?.add(cb)
      return () => {
        playstateListeners?.delete(cb)
      }
    },
  }
  return {
    qc,
    ...render(
      <I18nProvider initialLocale="zh-CN">
        <QueryClientProvider client={qc}>
          <AuthProvider>
            <WebSocketContext.Provider value={wsValue}>
              <AudioPlayerProvider>
                <MemoryRouter initialEntries={initialEntries}>{ui}</MemoryRouter>
              </AudioPlayerProvider>
            </WebSocketContext.Provider>
          </AuthProvider>
        </QueryClientProvider>
      </I18nProvider>,
    ),
  }
}

describe('Worker D 全量安全、播放器内核、音乐会话与影院前台修复验证', () => {
  const originalMediaSource = globalThis.MediaSource

  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    setDemoModeOverride(false)
    vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => undefined)
    vi.spyOn(HTMLMediaElement.prototype, 'load').mockImplementation(() => undefined)
    vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined)
  })

  afterEach(() => {
    if (originalMediaSource === undefined) {
      // @ts-expect-error restore MediaSource
      delete globalThis.MediaSource
    } else {
      globalThis.MediaSource = originalMediaSource
    }
    setDemoModeOverride(null)
    localStorage.clear()
    sessionStorage.clear()
    vi.restoreAllMocks()
  })

  it('[H-01] PlayerPage 仅发送标准 Authorization 头且对 STRM MKV 强制走 proxyDirectUrl (&proxy=1) 进入 StreamMkvRemuxer', async () => {
    seedAdminAuth('sec-token-h01')
    vi.spyOn(api, 'getUser').mockResolvedValue(ADMIN_USER)
    vi.spyOn(api, 'getItem').mockResolvedValue({
      Id: 'strm-mkv-1',
      Name: '网盘 4K STRM 电影',
      Type: 'Movie',
      RunTimeTicks: 7200 * 10_000_000,
    })
    vi.spyOn(api, 'getPlaybackInfo').mockResolvedValue({
      PlaySessionId: 'ps-strm-1',
      MediaSources: [
        {
          Id: 'ms-strm-1',
          Name: '4K STRM',
          Container: 'strm',
          Path: '/media/movies/Inception.2160p.mkv.strm',
          DirectStreamUrl: '/emby/Videos/strm-mkv-1/stream.mkv?Static=true&MediaSourceId=ms-strm-1',
          IsRemote: true,
          SupportsDirectPlay: true,
          MediaStreams: [
            { Index: 0, Type: 'Video', Codec: 'h264', Width: 3840, Height: 2160 },
            { Index: 1, Type: 'Audio', Codec: 'aac', Channels: 2 },
          ],
        },
      ],
    })
    vi.spyOn(api, 'reportPlaying').mockResolvedValue(undefined)
    vi.spyOn(api, 'reportProgress').mockResolvedValue(undefined)
    vi.spyOn(api, 'reportStopped').mockResolvedValue(undefined)

    // @ts-expect-error mock MediaSource capability
    globalThis.MediaSource = { isTypeSupported: vi.fn(() => true) }

    const attachSpy = vi.spyOn(StreamMkvRemuxer.prototype, 'attach').mockResolvedValue({
      durationSec: 7200,
      docType: 'matroska',
      videoTrack: null,
      audioTrack: null,
      cuesCount: 0,
    })

    const bifHeadersCaptured: Array<Record<string, string>> = []
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      if (url.includes('/index.bif')) {
        bifHeadersCaptured.push((init?.headers || {}) as Record<string, string>)
        return new Response('', { status: 404 })
      }
      return new Response('{}', { status: 200, headers: { 'content-type': 'application/json' } })
    })

    renderWithApp(
      <Routes>
        <Route path="/play/:id" element={<PlayerPage />} />
      </Routes>,
      ['/play/strm-mkv-1'],
    )

    await waitFor(() => {
      expect(attachSpy).toHaveBeenCalledTimes(1)
    })

    const [, streamUrl, authHeaders] = attachSpy.mock.calls[0]
    expect(streamUrl).toContain('proxy=1')
    expect(authHeaders?.Authorization).toContain('MediaBrowser ')
    expect(authHeaders?.Authorization).toContain('Token="sec-token-h01"')
    expect(authHeaders?.['X-Emby-Token']).toBeUndefined()
    expect(authHeaders?.['X-Emby-Authorization']).toBeUndefined()

    if (bifHeadersCaptured.length > 0) {
      expect(bifHeadersCaptured[0].Authorization).toContain('Token="sec-token-h01"')
      expect(bifHeadersCaptured[0]['X-Emby-Token']).toBeUndefined()
    }
  })

  it('[H-14] 大体积 (>150MB) 杜比 AC3 MKV 启用 MSE videoOnly + 隐藏同步 <audio> 轻量转码，且音频出错无缝回退 HLS', async () => {
    seedAdminAuth()
    vi.spyOn(api, 'getUser').mockResolvedValue(ADMIN_USER)
    vi.spyOn(api, 'getItem').mockResolvedValue({
      Id: 'movie-dolby',
      Name: '沙丘 2 4K Remux',
      Type: 'Movie',
      RunTimeTicks: 9000 * 10_000_000,
    })
    vi.spyOn(api, 'getPlaybackInfo').mockResolvedValue({
      PlaySessionId: 'ps-dolby',
      MediaSources: [
        {
          Id: 'ms-dolby',
          Name: 'Remux AC3',
          Container: 'mkv',
          Size: 12 * 1024 * 1024 * 1024, // 12 GB > 150 MB
          RunTimeTicks: 9000 * 10_000_000,
          SupportsDirectPlay: false,
          DefaultAudioStreamIndex: 1,
          MediaStreams: [
            { Index: 0, Type: 'Video', Codec: 'h264', Width: 3840, Height: 2160 },
            { Index: 1, Type: 'Audio', Codec: 'ac3', Channels: 6, Language: 'eng' },
          ],
        },
      ],
    })
    vi.spyOn(api, 'reportPlaying').mockResolvedValue(undefined)
    vi.spyOn(api, 'reportProgress').mockResolvedValue(undefined)
    vi.spyOn(api, 'reportStopped').mockResolvedValue(undefined)
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('', { status: 404 }))

    // MSE 支持 H.264 视频但浏览器不支持 AC3 音轨
    // @ts-expect-error mock MediaSource
    globalThis.MediaSource = {
      isTypeSupported: vi.fn((mime: string) => mime.includes('avc1')),
    }

    const attachSpy = vi.spyOn(StreamMkvRemuxer.prototype, 'attach').mockResolvedValue({
      durationSec: 9000,
      docType: 'matroska',
      videoTrack: null,
      audioTrack: null,
      cuesCount: 10,
    })
    vi.spyOn(Hls, 'isSupported').mockReturnValue(true)
    const hlsLoadSpy = vi.spyOn(Hls.prototype, 'loadSource').mockImplementation(() => undefined)
    vi.spyOn(Hls.prototype, 'attachMedia').mockImplementation(() => undefined)
    vi.spyOn(Hls.prototype, 'on').mockImplementation((() => undefined) as unknown as Hls['on'])
    vi.spyOn(Hls.prototype, 'destroy').mockImplementation(() => undefined)

    renderWithApp(
      <Routes>
        <Route path="/play/:id" element={<PlayerPage />} />
      </Routes>,
      ['/play/movie-dolby'],
    )

    await waitFor(() => {
      expect(attachSpy).toHaveBeenCalledTimes(1)
    })
    expect(attachSpy.mock.calls[0][3]).toMatchObject({ videoOnly: true })

    const hybridAudio = screen.getByTestId('hybrid-audio-element') as HTMLAudioElement
    expect(hybridAudio.src).toContain('/emby/Audio/movie-dolby/universal?')
    expect(hybridAudio.src).toContain('AudioCodec=aac')
    expect(hybridAudio.src).toContain('AudioStreamIndex=1')

    // 模拟同步音频流异常 -> 自动无缝降级到服务端 HLS
    fireEvent.error(hybridAudio)
    await waitFor(() => {
      expect(hlsLoadSpy).toHaveBeenCalledTimes(1)
    })
    expect(hlsLoadSpy.mock.calls[0][0]).toContain('/emby/Videos/movie-dolby/master.m3u8')
  })

  it('[M-19] PlayerPage 使用 effectiveDurationSec、支持片头跳过、校验远端播控 PlaySessionId 并提供在线搜索字幕弹窗', async () => {
    seedAdminAuth()
    vi.spyOn(api, 'getUser').mockResolvedValue(ADMIN_USER)
    vi.spyOn(api, 'getItem').mockResolvedValue({
      Id: 'ep-intro-1',
      Name: '第 1 集',
      Type: 'Episode',
      RunTimeTicks: 1500 * 10_000_000, // 25:00
      Chapters: [
        { Name: 'Opening', StartPositionTicks: 0 },
        { Name: '本篇开始', StartPositionTicks: 90 * 10_000_000 },
      ],
    })
    vi.spyOn(api, 'getPlaybackInfo').mockResolvedValue({
      PlaySessionId: 'ps-active-1',
      MediaSources: [
        {
          Id: 'ms-ep-1',
          Name: '1080p',
          Container: 'mp4',
          RunTimeTicks: 1500 * 10_000_000,
          SupportsDirectPlay: true,
          MediaStreams: [
            { Index: 0, Type: 'Video', Codec: 'h264' },
            { Index: 1, Type: 'Audio', Codec: 'aac' },
          ],
        },
      ],
    })
    vi.spyOn(api, 'reportPlaying').mockResolvedValue(undefined)
    vi.spyOn(api, 'reportProgress').mockResolvedValue(undefined)
    vi.spyOn(api, 'reportStopped').mockResolvedValue(undefined)
    const searchSubSpy = vi.spyOn(api, 'searchRemoteSubtitles').mockResolvedValue([
      {
        Id: 'remote-sub-1',
        Name: 'Ep01.CHS.ass',
        ProviderName: 'Assrt',
        Format: 'ass',
        IsHashMatch: true,
      },
    ])
    const downloadSubSpy = vi.spyOn(api, 'downloadRemoteSubtitle').mockResolvedValue(undefined)
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('', { status: 404 }))

    const playstateListeners = new Set<(cmd: PlaystateCommand) => void>()
    const { container } = renderWithApp(
      <Routes>
        <Route path="/play/:id" element={<PlayerPage />} />
      </Routes>,
      ['/play/ep-intro-1'],
      playstateListeners,
    )

    // 即使原生 video.duration 为 0，仍基于 effectiveDurationSec (1500s = 25:00) 展示总时长与章节刻度
    expect(await screen.findByText(/25:00/)).toBeInTheDocument()
    expect(screen.getAllByTestId('chapter-marker').length).toBe(2)

    // 推进到 20s（处于 Opening 片头区间 [0, 90s)），出现「跳过片头」按钮
    const video = container.querySelector('video') as HTMLVideoElement
    Object.defineProperty(video, 'currentTime', { configurable: true, writable: true, value: 20 })
    fireEvent.timeUpdate(video)

    const skipBtn = await screen.findByTestId('skip-intro-button')
    fireEvent.click(skipBtn)
    expect(video.currentTime).toBe(90)

    // 远端播控校验 PlaySessionId：不匹配会话被忽略，匹配会话执行 Pause
    const pauseSpy = vi.spyOn(video, 'pause')
    pauseSpy.mockClear()
    act(() => {
      playstateListeners.forEach((cb) => cb({ Command: 'Pause', PlaySessionId: 'ps-other-device' }))
    })
    expect(pauseSpy).not.toHaveBeenCalled()

    act(() => {
      playstateListeners.forEach((cb) => cb({ Command: 'Pause', PlaySessionId: 'ps-active-1' }))
    })
    expect(pauseSpy).toHaveBeenCalledTimes(1)

    // 打开字幕面板 -> 点击「在线搜索字幕」 -> 搜索并下载外挂字幕
    fireEvent.click(screen.getByTitle('切换字幕 (C)'))
    fireEvent.click(await screen.findByTestId('open-remote-subtitle-modal'))
    expect(await screen.findByText('Ep01.CHS.ass')).toBeInTheDocument()
    expect(searchSubSpy).toHaveBeenCalledWith('ep-intro-1', 'chi')

    fireEvent.click(screen.getByRole('button', { name: '下载' }))
    await waitFor(() => {
      expect(downloadSubSpy).toHaveBeenCalledWith('ep-intro-1', 'remote-sub-1')
    })
  })

  it('[M-20] LRC 支持冒号厘秒与头部元信息保序及双语同时高亮，单曲队列 repeatMode=all 循环不停播并上报会话', async () => {
    const parsed = parseLrcText(
      '作词：周杰伦\n作曲：周杰伦\n[00:20:00]后一句提前出现测试排序\n[00:12:50]Waiting for a sunny day\n[00:12.50]故事的小黄花',
    )
    expect(parsed[0]).toEqual({ timeSec: null, text: '作词：周杰伦' })
    expect(parsed[1]).toEqual({ timeSec: null, text: '作曲：周杰伦' })
    expect(parsed[2]).toEqual({ timeSec: 12.5, text: 'Waiting for a sunny day' })
    expect(parsed[3]).toEqual({ timeSec: 12.5, text: '故事的小黄花' })
    expect(parsed[4]).toEqual({ timeSec: 20, text: '后一句提前出现测试排序' })

    seedAdminAuth()
    vi.spyOn(api, 'getUser').mockResolvedValue(ADMIN_USER)
    vi.spyOn(api, 'getAudioLyrics').mockResolvedValue(
      '[ti:晴天]\n[00:12:50]Waiting for a sunny day\n[00:12.50]故事的小黄花\n[00:18:00]下一句',
    )
    const startSpy = vi.spyOn(api, 'reportPlaybackStart').mockResolvedValue(undefined)
    vi.spyOn(api, 'reportPlaybackProgress').mockResolvedValue(undefined)
    const stopSpy = vi.spyOn(api, 'reportPlaybackStopped').mockResolvedValue(undefined)

    const singleTrack: BaseItemDto = {
      Id: 'song-solo-1',
      Name: '晴天',
      Type: 'Audio',
      RunTimeTicks: 260 * 10_000_000,
    }

    function MusicHarness() {
      const { playQueue, cycleRepeatMode, repeatMode, stop } = useAudioPlayer()
      return (
        <div>
          <span data-testid="repeat-mode">{repeatMode}</span>
          <button type="button" onClick={() => playQueue([singleTrack], 0)}>
            start-solo
          </button>
          <button type="button" onClick={cycleRepeatMode}>
            cycle-repeat
          </button>
          <button type="button" onClick={stop}>
            stop-music
          </button>
          <AudioPlayerBar />
        </div>
      )
    }

    const { container } = renderWithApp(<MusicHarness />)
    fireEvent.click(screen.getByRole('button', { name: 'start-solo' }))

    // off -> all
    fireEvent.click(screen.getByRole('button', { name: 'cycle-repeat' }))
    expect(screen.getByTestId('repeat-mode').textContent).toBe('all')

    const audioEl = container.querySelector('audio') as HTMLAudioElement
    fireEvent.play(audioEl)
    await waitFor(() => {
      expect(startSpy).toHaveBeenCalledWith(
        expect.objectContaining({ ItemId: 'song-solo-1', PlayMethod: 'DirectStream' }),
      )
    })

    // 打开歌词抽屉并推进到 13 秒 -> 验证双语同时间戳两行同时高亮 (font-semibold text-amber-300)
    fireEvent.click(screen.getByRole('button', { name: '同步歌词' }))
    const enLine = await screen.findByText('Waiting for a sunny day')
    const zhLine = await screen.findByText('故事的小黄花')

    Object.defineProperty(audioEl, 'currentTime', { configurable: true, writable: true, value: 13 })
    fireEvent.timeUpdate(audioEl)

    await waitFor(() => {
      expect(enLine.className).toContain('text-amber-300')
      expect(zhLine.className).toContain('text-amber-300')
    })

    // 单曲队列在 repeatMode === 'all' 下触发 onEnded -> 必须归零 currentTime 并再次调用 play()，不停播
    const playSpy = vi.spyOn(audioEl, 'play').mockResolvedValue(undefined)
    fireEvent.ended(audioEl)
    expect(audioEl.currentTime).toBe(0)
    expect(playSpy).toHaveBeenCalled()

    // 停止音乐时触发 reportPlaybackStopped
    fireEvent.click(screen.getByRole('button', { name: 'stop-music' }))
    expect(stopSpy).toHaveBeenCalledWith(expect.objectContaining({ ItemId: 'song-solo-1' }))
  })

  it('[H-14(识别), M-02, L-09] 覆盖按类型识别、修改名称同步 SortName、Episode 隐藏识别/开放合并、删除策略权限、歌单 PlaylistItemId、PIN 键盘与跨标签页登出', async () => {
    seedAdminAuth()
    vi.spyOn(api, 'getUser').mockResolvedValue(ADMIN_USER)

    // 1. IdentifyModal 按真实 MusicAlbum 类型调用 remoteSearch
    const remoteSearchSpy = vi.spyOn(api, 'remoteSearch').mockResolvedValue([])
    const albumItem: BaseItemDto = { Id: 'alb-1', Name: '范特西', Type: 'MusicAlbum' }
    const idView = renderWithApp(
      <IdentifyModal open item={albumItem} onClose={() => undefined} />,
    )
    fireEvent.click(screen.getByRole('button', { name: '搜索候选匹配' }))
    await waitFor(() => {
      expect(remoteSearchSpy).toHaveBeenCalledWith(
        'MusicAlbum',
        expect.objectContaining({ Name: '范特西' }),
      )
    })
    idView.unmount()

    // 2. MetadataEditorModal 修改 Name 且未手动改 SortName 时自动同步为新名称小写
    const updateMetaSpy = vi.spyOn(api, 'updateItemMetadata').mockResolvedValue(undefined)
    const metaView = renderWithApp(
      <MetadataEditorModal
        open
        item={{ Id: 'mov-sort', Name: 'Old Title', SortName: 'old title', Type: 'Movie' }}
        onClose={() => undefined}
      />,
    )
    const nameInput = screen.getByPlaceholderText('例如：肖申克的救赎')
    fireEvent.change(nameInput, { target: { value: 'Dune Part Two' } })
    fireEvent.click(screen.getByRole('button', { name: '保存元数据' }))
    await waitFor(() => {
      expect(updateMetaSpy).toHaveBeenCalledWith(
        'mov-sort',
        expect.objectContaining({ Name: 'Dune Part Two', SortName: 'dune part two' }),
      )
    })
    metaView.unmount()

    // 3. ItemDetailPage: Episode 隐藏独立「识别」按钮，放开「合并多版本」，且普通用户开启 EnableContentDeletion 可见删除按钮
    vi.spyOn(api, 'getItem').mockResolvedValue({
      Id: 'ep-merge-1',
      Name: '试播集',
      Type: 'Episode',
      SeriesId: 'ser-1',
      MediaSources: [{ Id: 'ms-1', Name: '1080p', MediaStreams: [] }],
    })
    vi.spyOn(api, 'getSimilarItems').mockResolvedValue({ Items: [], TotalRecordCount: 0 })

    const epAdminView = renderWithApp(
      <Routes>
        <Route path="/item/:id" element={<ItemDetailPage />} />
      </Routes>,
      ['/item/ep-merge-1'],
    )
    await screen.findByText('试播集')
    expect(screen.queryByRole('button', { name: /识别/ })).toBeNull()
    expect(screen.getByRole('button', { name: /合并版本|合并多版本/ })).toBeInTheDocument()
    epAdminView.unmount()

    // 非管理员但 Policy.EnableContentDeletion = true
    saveAuth({
      accessToken: 'tok-deleter',
      userId: 'u-deleter',
      serverId: 'srv-1',
      userName: 'deleter',
      isAdmin: false,
      deviceId: 'dev-1',
    })
    vi.spyOn(api, 'getUser').mockResolvedValueOnce({
      Id: 'u-deleter',
      Name: 'deleter',
      ServerId: 'srv-1',
      Policy: { IsAdministrator: false, EnableContentDeletion: true },
    })
    const deleterView = renderWithApp(
      <Routes>
        <Route path="/item/:id" element={<ItemDetailPage />} />
      </Routes>,
      ['/item/ep-merge-1'],
    )
    expect(await screen.findByRole('button', { name: /删除/ })).toBeInTheDocument()
    deleterView.unmount()

    // 4. CollectionsPage: 歌单使用 PlaylistItemId 移动与移除重复曲目副本
    seedAdminAuth()
    vi.spyOn(api, 'getItem').mockResolvedValue({
      Id: 'pl-1',
      Name: '重复测试歌单',
      Type: 'Playlist',
      ChildCount: 2,
    })
    vi.spyOn(api, 'getPlaylistItems').mockResolvedValue({
      Items: [
        { Id: 'track-dup', PlaylistItemId: 'entry-1', Name: '同一首歌', Type: 'Audio' },
        { Id: 'track-dup', PlaylistItemId: 'entry-2', Name: '同一首歌', Type: 'Audio' },
      ],
      TotalRecordCount: 2,
    })
    const movePlSpy = vi.spyOn(api, 'movePlaylistItem').mockResolvedValue(undefined)
    const removePlSpy = vi.spyOn(api, 'removePlaylistItems').mockResolvedValue(undefined)

    const colView = renderWithApp(
      <Routes>
        <Route path="/collections/:id" element={<CollectionsPage />} />
      </Routes>,
      ['/collections/pl-1'],
    )
    const removeBtns = await screen.findAllByTitle('移出列表')
    expect(removeBtns).toHaveLength(2)
    const moveUpBtns = screen.getAllByTitle('上移')
    fireEvent.click(moveUpBtns[1])
    await waitFor(() => {
      expect(movePlSpy).toHaveBeenCalledWith('pl-1', 'entry-2', 0)
    })
    fireEvent.click(removeBtns[1])
    await waitFor(() => {
      expect(removePlSpy).toHaveBeenCalledWith('pl-1', ['entry-2'], { isPlaylistItemId: true })
    })
    colView.unmount()

    // 5. LoginPage 找回密码 PIN 输入框兼容十六进制字母（无 inputMode="numeric"）
    localStorage.clear()
    vi.spyOn(api, 'getSetupStatus').mockResolvedValue({
      completed: true,
      server_name: 'Ambra',
      default_language: 'zh-CN',
      has_users: true,
      has_tmdb_key: true,
    })
    vi.spyOn(api, 'getPublicSystemInfo').mockResolvedValue({
      Id: 'srv-1',
      ServerName: 'Ambra',
      Version: '0.10.9',
    })
    vi.spyOn(api, 'getPublicUsers').mockResolvedValue([])
    vi.spyOn(api, 'getQuickConnectEnabled').mockResolvedValue(false)
    vi.spyOn(api, 'forgotPassword').mockResolvedValue({ PinFile: '/config/passwordreset.txt' })

    const loginView = renderWithApp(<LoginPage />, ['/login'])
    fireEvent.click(await screen.findByRole('button', { name: /忘记密码/ }))
    fireEvent.change(await screen.findByPlaceholderText('输入需要重置密码的用户名'), {
      target: { value: 'admin' },
    })
    fireEvent.click(screen.getByRole('button', { name: '下一步' }))
    const pinInput = (await screen.findByPlaceholderText('输入 6 位 PIN 码')) as HTMLInputElement
    expect(pinInput.getAttribute('inputmode')).toBeNull()
    expect(pinInput.getAttribute('autocapitalize')).toBe('characters')
    loginView.unmount()

    // 6. AuthContext 监听 storage 事件实现多标签页同步登出
    seedAdminAuth('tok-tab-sync')
    function StorageSyncProbe() {
      const { auth } = useAuth()
      return <span data-testid="sync-token">{auth?.accessToken ?? 'logged-out'}</span>
    }
    const syncView = renderWithApp(<StorageSyncProbe />)
    expect(screen.getByTestId('sync-token').textContent).toBe('tok-tab-sync')
    localStorage.removeItem(getAuthStorageKey())
    act(() => {
      window.dispatchEvent(
        new StorageEvent('storage', {
          key: getAuthStorageKey(),
          newValue: null,
        }),
      )
    })
    expect(screen.getByTestId('sync-token').textContent).toBe('logged-out')
    syncView.unmount()
  })

  it('[影院前台补齐] LibraryPage 高级筛选 + >120 视口虚拟滚动，LiveTvPage 系列追录 + 频道映射 + DVR 录像 HLS 播放', async () => {
    seedAdminAuth()
    vi.spyOn(api, 'getUser').mockResolvedValue(ADMIN_USER)
    vi.spyOn(api, 'getItem').mockResolvedValue({
      Id: 'lib-4k',
      Name: '4K 影院库',
      Type: 'CollectionFolder',
      CollectionType: 'movies',
    })
    vi.spyOn(api, 'getGenres').mockResolvedValue({ Items: [], TotalRecordCount: 0 })
    vi.spyOn(api, 'getYears').mockResolvedValue({ Items: [], TotalRecordCount: 0 })
    vi.spyOn(api, 'getTags').mockResolvedValue({ Items: [], TotalRecordCount: 0 })
    vi.spyOn(api, 'getItemFilters2').mockResolvedValue({
      Genres: ['科幻'],
      Years: [2024],
      Codecs: ['hevc', 'av1'],
    })

    const bulkItems: BaseItemDto[] = Array.from({ length: 180 }, (_, idx) => ({
      Id: `mov-bulk-${idx + 1}`,
      Name: `电影条目 #${idx + 1}`,
      Type: 'Movie',
    }))
    const getItemsSpy = vi.spyOn(api, 'getItems').mockResolvedValue({
      Items: bulkItems,
      TotalRecordCount: 180,
    })

    const libView = renderWithApp(
      <Routes>
        <Route path="/library/:id" element={<LibraryPage />} />
      </Routes>,
      ['/library/lib-4k'],
    )

    // >120 条时启用视口虚拟滚动，仅挂载可视区 + 缓冲行，不一次性挂载全部 180 张卡片
    const virtualContainer = await screen.findByTestId('library-virtual-scroll')
    expect(virtualContainer).toBeInTheDocument()
    const renderedLinks = within(virtualContainer).getAllByRole('link')
    expect(renderedLinks.length).toBeGreaterThan(0)
    expect(renderedLinks.length).toBeLessThan(180)
    expect(screen.getByTestId('virtual-grid-bottom-spacer')).toBeInTheDocument()

    // 展开高级筛选面板并组合设置 4K + HDR + HEVC + ≥8.0 + 已赞
    fireEvent.click(screen.getByTestId('toggle-advanced-filters'))
    fireEvent.change(screen.getByLabelText('按分辨率筛选'), { target: { value: '4K' } })
    fireEvent.change(screen.getByLabelText('按视频编码筛选'), { target: { value: 'hevc' } })
    fireEvent.change(screen.getByLabelText('按最低评分筛选'), { target: { value: '8' } })
    fireEvent.click(screen.getByTestId('filter-hdr-toggle'))
    fireEvent.click(screen.getByTestId('filter-liked-toggle'))

    await waitFor(() => {
      expect(getItemsSpy).toHaveBeenLastCalledWith(
        ADMIN_USER.Id,
        expect.objectContaining({
          is4K: true,
          isHDR: true,
          videoCodecs: 'hevc',
          minCommunityRating: 8,
          isLiked: true,
          filters: 'IsLiked',
        }),
      )
    })
    libView.unmount()

    // LiveTvPage: 系列追录 + 频道映射 + DVR 录像 HLS 回看
    vi.spyOn(api, 'getLiveTvChannels').mockResolvedValue({
      Items: [{ Id: 'ch-cctv1', Name: 'CCTV-1 综合', Number: '1', IsHls: true }],
      TotalRecordCount: 1,
    })
    vi.spyOn(api, 'getLiveTvPrograms').mockResolvedValue({ Items: [], TotalRecordCount: 0 })
    vi.spyOn(api, 'getLiveTvTimers').mockResolvedValue({ Items: [], TotalRecordCount: 0 })
    vi.spyOn(api, 'getSeriesTimers').mockResolvedValue({
      Items: [{ Id: 'st-99', Name: '天下足球', ChannelId: 'ch-cctv1', RecordNewOnly: true }],
      TotalRecordCount: 1,
    })
    const createStSpy = vi
      .spyOn(api, 'createSeriesTimer')
      .mockResolvedValue({ Id: 'st-100', Name: '新闻周刊', RecordNewOnly: true })
    const deleteStSpy = vi.spyOn(api, 'deleteSeriesTimer').mockResolvedValue(undefined)
    vi.spyOn(api, 'getListingProviders').mockResolvedValue([
      { Id: 'prov-xmltv', Type: 'xmltv', Path: 'http://epg.local/guide.xml' },
    ])
    vi.spyOn(api, 'getChannelMappings').mockResolvedValue({
      TunerChannels: [{ Id: 'ch-cctv1', Name: 'CCTV-1 综合', Number: '1' }],
      ProviderChannels: [{ Id: 'epg-cctv1', Name: 'CCTV1' }],
      Mappings: [],
    })
    const setMappingSpy = vi
      .spyOn(api, 'setChannelMapping')
      .mockResolvedValue({ TunerChannelId: 'ch-cctv1', ProviderChannelId: 'epg-cctv1' })
    vi.spyOn(api, 'getLiveTvRecordings').mockResolvedValue({
      Items: [
        {
          Id: 'rec-ts-1',
          Name: '晚间新闻录制.ts',
          ChannelName: 'CCTV-1 综合',
          Status: 'Completed',
          Path: '/dvr/rec-ts-1.ts',
        },
      ],
      TotalRecordCount: 1,
    })

    vi.spyOn(Hls, 'isSupported').mockReturnValue(true)
    const hlsLoadSpy = vi.spyOn(Hls.prototype, 'loadSource').mockImplementation(() => undefined)
    vi.spyOn(Hls.prototype, 'attachMedia').mockImplementation(() => undefined)
    vi.spyOn(Hls.prototype, 'on').mockImplementation((() => undefined) as unknown as Hls['on'])
    vi.spyOn(Hls.prototype, 'destroy').mockImplementation(() => undefined)

    const liveView = renderWithApp(<LiveTvPage />, ['/livetv'])

    // 1. 系列追录 Tab
    fireEvent.click(await screen.findByRole('tab', { name: '系列追录' }))
    expect(await screen.findByText('天下足球')).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('追录节目名称'), { target: { value: '新闻周刊' } })
    fireEvent.click(screen.getByRole('button', { name: '添加追录' }))
    await waitFor(() => {
      expect(createStSpy).toHaveBeenCalledWith(
        expect.objectContaining({ Name: '新闻周刊', RecordNewOnly: true }),
      )
    })
    fireEvent.click(screen.getByRole('button', { name: '取消追录' }))
    await waitFor(() => {
      expect(deleteStSpy).toHaveBeenCalledWith('st-99')
    })

    // 2. 频道映射 Tab
    fireEvent.click(screen.getByRole('tab', { name: '频道映射' }))
    await waitFor(() => {
      const provSelect = screen.getByLabelText('选择节目单源') as HTMLSelectElement
      expect(provSelect.value).toBe('prov-xmltv')
    })
    const mappingSelect = await screen.findByLabelText('CCTV-1 综合 EPG 频道映射')
    fireEvent.change(mappingSelect, { target: { value: 'epg-cctv1' } })
    fireEvent.click(await screen.findByRole('button', { name: '保存映射' }))
    await waitFor(() => {
      expect(setMappingSpy).toHaveBeenCalledWith(
        expect.objectContaining({
          ProviderId: 'prov-xmltv',
          TunerChannelId: 'ch-cctv1',
          ProviderChannelId: 'epg-cctv1',
        }),
      )
    })

    // 3. 已录节目 Tab：.ts 录像优先通过 hls.js 加载 /emby/LiveTv/LiveRecordings/{id}/hls/master.m3u8
    hlsLoadSpy.mockClear()
    fireEvent.click(screen.getByRole('tab', { name: /已录节目/ }))
    fireEvent.click(await screen.findByRole('button', { name: /播放/ }))
    await waitFor(() => {
      expect(hlsLoadSpy).toHaveBeenCalledTimes(1)
    })
    expect(hlsLoadSpy.mock.calls[0][0]).toContain(
      '/emby/LiveTv/LiveRecordings/rec-ts-1/hls/master.m3u8',
    )

    liveView.unmount()
  })
})
