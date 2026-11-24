import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { saveAuth } from '../../api/client'
import { AudioPlayerProvider } from '../../context/AudioPlayerContext'
import { AuthProvider } from '../../context/AuthContext'
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

const sampleMovieItem = {
  Id: 'movie-parity-1',
  Name: '星际穿越',
  OriginalTitle: 'Interstellar',
  Type: 'Movie',
  MediaType: 'Video',
  RunTimeTicks: 7_200 * 10_000_000,
  Chapters: [
    { StartPositionTicks: 0, Name: '序幕' },
    { StartPositionTicks: 600 * 10_000_000, Name: '发射升空' },
    { StartPositionTicks: 1800 * 10_000_000, Name: '穿越虫洞' },
  ],
  MediaSources: [
    {
      Id: 'src-parity-1',
      Name: '4K REMUX',
      Path: '/media/movies/Interstellar.2014.2160p.mkv',
      Container: 'mkv',
      Size: 32 * 1024 * 1024 * 1024,
      Bitrate: 38_500_000,
      RunTimeTicks: 7_200 * 10_000_000,
      SupportsDirectPlay: true,
      DefaultAudioStreamIndex: 1,
      DefaultSubtitleStreamIndex: 2,
      MediaStreams: [
        {
          Index: 0,
          Type: 'Video',
          Codec: 'hevc',
          Width: 3840,
          Height: 2160,
          VideoRange: 'HDR10',
          BitRate: 35_000_000,
          IsDefault: true,
        },
        {
          Index: 1,
          Type: 'Audio',
          Codec: 'aac',
          Channels: 6,
          Language: 'eng',
          DisplayLanguage: 'English',
          BitRate: 640_000,
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
          IsForced: true,
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

describe('Stream 3: PlayerPage Emby Parity & Bug Fixes', () => {
  beforeEach(() => {
    localStorage.clear()
    saveAuth({
      accessToken: 'tok-parity',
      userId: 'u-1',
      serverId: 'srv-1',
      userName: 'Tester',
      isAdmin: true,
      deviceId: 'dev-parity',
    })
    vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined)
    vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => undefined)
    vi.spyOn(HTMLMediaElement.prototype, 'load').mockImplementation(() => undefined)
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('初始挂载不触发重复 PlaybackInfo，支持快捷键修饰键守卫、? 帮助浮层、PiP 与章节跳转', async () => {
    const user = userEvent.setup()

    Object.defineProperty(document, 'pictureInPictureEnabled', {
      configurable: true,
      value: true,
    })
    const requestPipSpy = vi.fn().mockResolvedValue({})
    Object.defineProperty(HTMLVideoElement.prototype, 'requestPictureInPicture', {
      configurable: true,
      value: requestPipSpy,
    })

    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/emby/Users/u-1') {
        return jsonRes({ Id: 'u-1', Name: 'Tester', Policy: { IsAdministrator: true } })
      }
      if (url === '/emby/Users/u-1/Items/movie-parity-1') {
        return jsonRes(sampleMovieItem)
      }
      if (url.startsWith('/emby/Items/movie-parity-1/PlaybackInfo')) {
        return jsonRes({
          PlaySessionId: 'sess-parity-1',
          MediaSources: [
            {
              Id: 'src-parity-1',
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
          <AudioPlayerProvider>
            <MemoryRouter initialEntries={['/play/movie-parity-1']}>
              <Routes>
                <Route path="/play/:id" element={<PlayerPage />} />
                <Route path="/item/:id" element={<LocationProbe />} />
              </Routes>
            </MemoryRouter>
          </AudioPlayerProvider>
        </AuthProvider>
      </QueryClientProvider>,
    )

    const video = container.querySelector('video') as HTMLVideoElement
    await waitFor(() => {
      expect(video.src).toContain('MediaSourceId=src-parity-1')
    })

    // 1) 消除初始双请求：PlaybackInfo 仅请求 1 次
    const playbackInfoCalls = fetchSpy.mock.calls.filter(([u]) =>
      String(u).startsWith('/emby/Items/movie-parity-1/PlaybackInfo'),
    )
    expect(playbackInfoCalls.length).toBe(1)

    // 2) 快捷键修饰键守卫：Ctrl+P / Meta+P 不应触发 PiP
    fireEvent.keyDown(window, { key: 'p', ctrlKey: true })
    fireEvent.keyDown(window, { key: 'p', metaKey: true })
    fireEvent.keyDown(window, { key: 'p', altKey: true })
    expect(requestPipSpy).not.toHaveBeenCalled()

    // 正常按 P 触发 PiP
    fireEvent.keyDown(window, { key: 'p' })
    expect(requestPipSpy).toHaveBeenCalledTimes(1)

    // 3) 快捷键 ? 打开帮助浮层，Esc 关闭帮助浮层而不退出页面
    fireEvent.keyDown(window, { key: '?' })
    expect(await screen.findByText('快捷键说明')).toBeInTheDocument()
    expect(screen.getByText('画中画 (PiP)')).toBeInTheDocument()
    fireEvent.keyDown(window, { key: 'Escape' })
    await waitFor(() => {
      expect(screen.queryByText('快捷键说明')).not.toBeInTheDocument()
    })

    // 4) 章节刻度与章节菜单跳转
    Object.defineProperty(video, 'duration', { configurable: true, value: 3600 })
    fireEvent.loadedMetadata(video)
    await waitFor(() => {
      expect(screen.getAllByTestId('chapter-marker').length).toBe(3)
    })

    await user.click(screen.getByTitle('章节列表'))
    const wormholeBtn = await screen.findByRole('button', { name: /穿越虫洞/ })
    await user.click(wormholeBtn)
    expect(video.currentTime).toBe(1800)
  })

  it('画质切换强制走 Server HLS 并携带 MaxStreamingBitrate/MaxWidth/MaxHeight，且 history.length<=1 时退出回退到 /item/:id', async () => {
    const user = userEvent.setup()

    // 让 mountServerHls 在 jsdom 中走原生 HLS 分支以便断言 video.src
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockImplementation((type) =>
      type === 'application/vnd.apple.mpegurl' ? 'probably' : '',
    )

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/emby/Users/u-1') {
        return jsonRes({ Id: 'u-1', Name: 'Tester', Policy: { IsAdministrator: true } })
      }
      if (url === '/emby/Users/u-1/Items/movie-parity-1') {
        return jsonRes(sampleMovieItem)
      }
      if (url.startsWith('/emby/Items/movie-parity-1/PlaybackInfo')) {
        return jsonRes({
          PlaySessionId: 'sess-quality-1',
          MediaSources: [
            {
              Id: 'src-parity-1',
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

    const origHistoryLength = window.history.length
    Object.defineProperty(window.history, 'length', { configurable: true, value: 1 })

    try {
      const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
      const { container } = render(
        <QueryClientProvider client={qc}>
          <AuthProvider>
            <AudioPlayerProvider>
              <MemoryRouter initialEntries={['/play/movie-parity-1']}>
                <Routes>
                  <Route path="/play/:id" element={<PlayerPage />} />
                  <Route path="/item/:id" element={<LocationProbe />} />
                </Routes>
              </MemoryRouter>
            </AudioPlayerProvider>
          </AuthProvider>
        </QueryClientProvider>,
      )

      const video = container.querySelector('video') as HTMLVideoElement
      await waitFor(() => {
        expect(video.src).toContain('/emby/Videos/movie-parity-1/stream')
      })

      // 模拟播放到 125 秒后切换画质到 720p · 4 Mbps
      fireEvent.loadedMetadata(video)
      video.currentTime = 125

      await user.click(screen.getByTitle('切换画质'))
      await user.click(await screen.findByRole('button', { name: '720p · 4 Mbps' }))

      await waitFor(() => {
        expect(video.src).toContain('/emby/Videos/movie-parity-1/master.m3u8')
        expect(video.src).toContain('MaxStreamingBitrate=4000000')
        expect(video.src).toContain('MaxWidth=1280')
        expect(video.src).toContain('MaxHeight=720')
      })

      // 验证切换画质后恢复播放进度 125 秒
      video.currentTime = 0
      fireEvent.loadedMetadata(video)
      await waitFor(() => {
        expect(video.currentTime).toBe(125)
      })

      // 验证 history.length <= 1 时点击返回跳转到 /item/movie-parity-1
      await user.click(screen.getByTitle('返回'))
      expect(await screen.findByTestId('location-probe')).toHaveTextContent('/item/movie-parity-1')
    } finally {
      Object.defineProperty(window.history, 'length', {
        configurable: true,
        value: origHistoryLength,
      })
    }
  })
})

describe('Stream 3: ItemDetailPage Emby Parity (Media Info / Chapters / Subtitles / Cascade)', () => {
  beforeEach(() => {
    localStorage.clear()
    saveAuth({
      accessToken: 'tok-detail',
      userId: 'u-1',
      serverId: 'srv-1',
      userName: 'Tester',
      isAdmin: true,
      deviceId: 'dev-detail',
    })
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('展示媒体信息面板、章节横向条跳转，并支持外挂字幕删除与上传', async () => {
    const user = userEvent.setup()
    const clipboardSpy = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText: clipboardSpy },
    })

    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      const method = (init?.method || 'GET').toUpperCase()
      if (url === '/emby/Users/u-1') {
        return jsonRes({ Id: 'u-1', Name: 'Tester', Policy: { IsAdministrator: true } })
      }
      if (url === '/emby/Users/u-1/Items/movie-parity-1') {
        return jsonRes(sampleMovieItem)
      }
      if (url.includes('/Similar')) {
        return jsonRes({ Items: [], TotalRecordCount: 0 })
      }
      if (url === '/emby/Videos/movie-parity-1/Subtitles/3' && method === 'DELETE') {
        return emptyRes(204)
      }
      if (url === '/emby/Videos/movie-parity-1/Subtitles' && method === 'POST') {
        return emptyRes(204)
      }
      return jsonRes({})
    })

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={qc}>
        <AuthProvider>
          <AudioPlayerProvider>
            <MemoryRouter initialEntries={['/item/movie-parity-1']}>
              <Routes>
                <Route path="/item/:id" element={<ItemDetailPage />} />
                <Route path="/play/:id" element={<LocationProbe />} />
              </Routes>
            </MemoryRouter>
          </AudioPlayerProvider>
        </AuthProvider>
      </QueryClientProvider>,
    )

    // 1) 验证 Emby 风格媒体信息面板
    expect(await screen.findByText('媒体信息')).toBeInTheDocument()
    expect(screen.getByText('/media/movies/Interstellar.2014.2160p.mkv')).toBeInTheDocument()
    expect(screen.getByText('38.50 Mbps')).toBeInTheDocument()
    expect(screen.getByText(/3840×2160/)).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '复制路径' }))
    expect(clipboardSpy).toHaveBeenCalledWith('/media/movies/Interstellar.2014.2160p.mkv')

    // 2) 验证外挂字幕管理弹窗（删除外挂字幕 + 上传新字幕）
    await user.click(screen.getByRole('button', { name: '管理字幕' }))
    expect(await screen.findByText('当前字幕轨道 (2)')).toBeInTheDocument()

    // 仅外挂字幕有删除按钮
    const deleteButtons = screen.getAllByRole('button', { name: '删除' })
    expect(deleteButtons.length).toBe(1)
    await user.click(deleteButtons[0])

    await waitFor(() => {
      expect(
        fetchSpy.mock.calls.some(
          ([u, init]) =>
            String(u) === '/emby/Videos/movie-parity-1/Subtitles/3' &&
            (init as RequestInit)?.method === 'DELETE',
        ),
      ).toBe(true)
    })

    // 上传字幕
    const fileInput = screen.getByLabelText('选择字幕文件') as HTMLInputElement
    const srtFile = new File(['1\n00:00:01,000 --> 00:00:02,000\nHello\n'], 'subs.srt', {
      type: 'text/plain',
    })
    await user.upload(fileInput, srtFile)
    await user.selectOptions(screen.getByLabelText('字幕语言'), 'chs')
    await user.click(screen.getByLabelText('强制字幕 (Forced)'))
    await user.click(screen.getByRole('button', { name: '上传字幕' }))

    await waitFor(() => {
      const uploadCall = fetchSpy.mock.calls.find(
        ([u, init]) =>
          String(u) === '/emby/Videos/movie-parity-1/Subtitles' &&
          (init as RequestInit)?.method === 'POST',
      )
      expect(uploadCall).toBeDefined()
      const payload = JSON.parse(String((uploadCall?.[1] as RequestInit)?.body))
      expect(payload).toMatchObject({
        Language: 'chs',
        Format: 'srt',
        IsForced: true,
      })
      expect(typeof payload.Data).toBe('string')
      expect(payload.Data.length).toBeGreaterThan(0)
    })

    // 关闭字幕弹窗并点击章节卡片跳转到带 startTicks 的播放页
    await user.click(screen.getByRole('button', { name: '关闭' }))
    await user.click(screen.getByRole('button', { name: /发射升空/ }))
    expect(await screen.findByTestId('location-probe')).toHaveTextContent(
      `/play/movie-parity-1?startTicks=${600 * 10_000_000}`,
    )
  })

  it('Series 标记已看成功时级联刷新 seasons / episodes / nextUp / resumeItems 缓存', async () => {
    const user = userEvent.setup()

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      const method = (init?.method || 'GET').toUpperCase()
      if (url === '/emby/Users/u-1') {
        return jsonRes({ Id: 'u-1', Name: 'Tester', Policy: { IsAdministrator: true } })
      }
      if (url === '/emby/Users/u-1/Items/series-1') {
        return jsonRes({
          Id: 'series-1',
          Name: '测试剧集',
          Type: 'Series',
          UserData: { Played: false, IsFavorite: false, PlaybackPositionTicks: 0, PlayCount: 0 },
        })
      }
      if (url.includes('/Shows/series-1/Seasons')) {
        return jsonRes({
          Items: [{ Id: 'season-1', Name: '第 1 季', Type: 'Season' }],
          TotalRecordCount: 1,
        })
      }
      if (url.includes('/Shows/series-1/Episodes')) {
        return jsonRes({ Items: [], TotalRecordCount: 0 })
      }
      if (url.includes('/Shows/NextUp') || url.includes('/Similar')) {
        return jsonRes({ Items: [], TotalRecordCount: 0 })
      }
      if (url === '/emby/Users/u-1/PlayedItems/series-1' && method === 'POST') {
        return jsonRes({ Played: true, PlayCount: 1, PlaybackPositionTicks: 0, IsFavorite: false })
      }
      return jsonRes({})
    })

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const invalidateSpy = vi.spyOn(qc, 'invalidateQueries')

    render(
      <QueryClientProvider client={qc}>
        <AuthProvider>
          <AudioPlayerProvider>
            <MemoryRouter initialEntries={['/item/series-1']}>
              <Routes>
                <Route path="/item/:id" element={<ItemDetailPage />} />
              </Routes>
            </MemoryRouter>
          </AudioPlayerProvider>
        </AuthProvider>
      </QueryClientProvider>,
    )

    const markPlayedBtn = await screen.findByRole('button', { name: '标记已看' })
    await user.click(markPlayedBtn)

    await waitFor(() => {
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['seasons'] })
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['episodes'] })
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['nextUp'] })
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['resumeItems'] })
    })
  })
})
