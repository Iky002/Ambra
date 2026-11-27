import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import Hls from 'hls.js'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { saveAuth } from '../../api/client'
import type { BaseItemDto, MediaStream } from '../../api/types'
import { MediaCard } from '../../components/MediaCard'
import { AudioPlayerProvider, useAudioPlayer } from '../../context/AudioPlayerContext'
import { AuthProvider } from '../../context/AuthContext'
import {
  pickPreferredAudioIndex,
  pickPreferredSubtitleIndex,
  savePlaybackPrefs,
} from '../../player/playbackPrefs'
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

function textRes(body: string, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    headers: new Headers({ 'content-type': 'text/vtt; charset=utf-8' }),
    json: async () => ({}),
    text: async () => body,
  } as Response
}

function LocationProbe() {
  const loc = useLocation()
  return <div data-testid="location-probe">{`${loc.pathname}${loc.search}`}</div>
}

describe('PlayerPage P0-3 & P0-7 修复验证', () => {
  beforeEach(() => {
    localStorage.clear()
    saveAuth({
      accessToken: 'tok-player-test',
      userId: 'u-1',
      serverId: 'srv-1',
      userName: 'Tester',
      isAdmin: true,
      deviceId: 'dev-test',
    })
    vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined)
    vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => undefined)
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('点击「关闭字幕」后保持字幕关闭不回弹，且不触发多余的 Stopped/Playing 上报，同时缺失 DeliveryUrl 时回退到无 /0/ 的字幕 URL', async () => {
    const user = userEvent.setup()

    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/emby/Users/u-1') {
        return jsonRes({
          Id: 'u-1',
          Name: 'Tester',
          ServerId: 'srv-1',
          Policy: { IsAdministrator: true },
        })
      }
      if (url === '/emby/Users/u-1/Items/movie-1') {
        return jsonRes({
          Id: 'movie-1',
          Name: '测试电影',
          Type: 'Movie',
          RunTimeTicks: 3_600 * 10_000_000,
          UserData: {
            Played: false,
            PlaybackPositionTicks: 120 * 10_000_000,
          },
        })
      }
      if (url.startsWith('/emby/Items/movie-1/PlaybackInfo')) {
        return jsonRes({
          PlaySessionId: 'sess-1',
          MediaSources: [
            {
              Id: 'src-1',
              Container: 'mp4',
              SupportsDirectPlay: true,
              DirectStreamUrl: '/emby/Videos/movie-1/stream?Static=true&MediaSourceId=src-1',
              DefaultAudioStreamIndex: 1,
              DefaultSubtitleStreamIndex: 2,
              MediaStreams: [
                { Index: 0, Type: 'Video', Codec: 'h264', Width: 1920, Height: 1080 },
                { Index: 1, Type: 'Audio', Codec: 'aac', Channels: 2, Language: 'chi' },
                {
                  Index: 2,
                  Type: 'Subtitle',
                  Codec: 'subrip',
                  Language: 'chi',
                  DisplayTitle: '简体中文',
                },
              ],
            },
          ],
        })
      }
      if (url.includes('/Subtitles/2/Stream.vtt')) {
        return textRes('WEBVTT\n\n00:00:00.000 --> 00:05:00.000\n你好，世界\n')
      }
      if (url.startsWith('/emby/Sessions/Playing')) {
        return {
          ok: true,
          status: 204,
          headers: new Headers(),
          json: async () => undefined,
          text: async () => '',
        } as Response
      }
      return jsonRes({})
    })

    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })

    const { container } = render(
      <QueryClientProvider client={qc}>
        <AuthProvider>
          <MemoryRouter initialEntries={['/play/movie-1']}>
            <Routes>
              <Route path="/play/:id" element={<PlayerPage />} />
            </Routes>
          </MemoryRouter>
        </AuthProvider>
      </QueryClientProvider>,
    )

    // 1) 等待默认字幕加载并验证无 /0/ 的字幕回退 URL
    await waitFor(() => {
      expect(
        fetchSpy.mock.calls.some(([u]) =>
          String(u).includes('/emby/Videos/movie-1/src-1/Subtitles/2/Stream.vtt'),
        ),
      ).toBe(true)
    })
    expect(
      fetchSpy.mock.calls.some(([u]) => String(u).includes('/Subtitles/2/0/Stream.')),
    ).toBe(false)

    // 确认顶部 OSD 显示默认字幕已激活
    expect(await screen.findByText(/字幕:/)).toBeInTheDocument()

    // 2) 触发视频 loadedmetadata，验证自动续播到 item.UserData.PlaybackPositionTicks (120s)
    const video = container.querySelector('video') as HTMLVideoElement
    expect(video).toBeTruthy()
    Object.defineProperty(video, 'readyState', { configurable: true, value: 1 })
    fireEvent.loadedMetadata(video)
    await waitFor(() => {
      expect(video.currentTime).toBe(120)
    })

    // 3) 打开字幕面板并点击「关闭字幕」
    const subtitleToggleBtn = screen.getByTitle('切换字幕 (C)')
    await user.click(subtitleToggleBtn)
    const closeSubBtn = await screen.findByRole('button', { name: '关闭字幕' })
    await user.click(closeSubBtn)

    // 4) 验证字幕保持关闭，不会回弹到 DefaultSubtitleStreamIndex
    await waitFor(() => {
      expect(screen.queryByText(/字幕:/)).not.toBeInTheDocument()
    })
    // 再次打开字幕面板确认「关闭字幕」处于选中态
    await user.click(subtitleToggleBtn)
    const closeSubBtnAgain = await screen.findByRole('button', { name: '关闭字幕' })
    expect(closeSubBtnAgain.className).toContain('text-amber-300')

    // 5) 验证切换字幕未触发多余的 Stopped -> Playing 周期
    const playingCalls = fetchSpy.mock.calls.filter(([u]) => String(u) === '/emby/Sessions/Playing')
    const stoppedCallsBeforeHide = fetchSpy.mock.calls.filter(
      ([u]) => String(u) === '/emby/Sessions/Playing/Stopped',
    )
    expect(playingCalls.length).toBe(1)
    expect(stoppedCallsBeforeHide.length).toBe(0)

    // 6) 验证 pagehide 触发 keepalive: true 的 Stopped 上报
    window.dispatchEvent(new Event('pagehide'))
    const keepaliveStopCall = fetchSpy.mock.calls.find(
      ([u, init]) =>
        String(u) === '/emby/Sessions/Playing/Stopped' &&
        (init as RequestInit | undefined)?.keepalive === true,
    )
    expect(keepaliveStopCall).toBeDefined()
    expect(JSON.parse(String((keepaliveStopCall?.[1] as RequestInit)?.body))).toMatchObject({
      ItemId: 'movie-1',
      MediaSourceId: 'src-1',
      PlaySessionId: 'sess-1',
      PositionTicks: 120 * 10_000_000,
    })
  })
})

describe('MediaCard 继续观看续播链接 (P0-7)', () => {
  const baseItem: BaseItemDto = {
    Id: 'ep-42',
    Name: '第 1 集',
    Type: 'Episode',
    MediaType: 'Video',
    RunTimeTicks: 2_400 * 10_000_000,
    UserData: {
      Played: false,
      PlaybackPositionTicks: 645_000_000,
      PlayedPercentage: 26.8,
    },
  }

  it('showProgress=true 且存在 PlaybackPositionTicks 时，快速播放按钮携带 startTicks 参数', async () => {
    const user = userEvent.setup()
    render(
      <MemoryRouter initialEntries={['/']}>
        <Routes>
          <Route path="/" element={<MediaCard item={baseItem} showProgress />} />
          <Route path="/play/:id" element={<LocationProbe />} />
        </Routes>
      </MemoryRouter>,
    )

    await user.click(screen.getByTitle('立即播放'))
    expect(await screen.findByTestId('location-probe')).toHaveTextContent(
      '/play/ep-42?startTicks=645000000',
    )
  })

  it('showProgress=false 时，快速播放按钮不附加 startTicks 参数', async () => {
    const user = userEvent.setup()
    render(
      <MemoryRouter initialEntries={['/']}>
        <Routes>
          <Route path="/" element={<MediaCard item={baseItem} showProgress={false} />} />
          <Route path="/play/:id" element={<LocationProbe />} />
        </Routes>
      </MemoryRouter>,
    )

    await user.click(screen.getByTitle('立即播放'))
    expect(await screen.findByTestId('location-probe')).toHaveTextContent('/play/ep-42')
  })
})

describe('Stream C: playbackPrefs 轨道偏好选择 (P0-1)', () => {
  const audioStreams: MediaStream[] = [
    { Index: 1, Type: 'Audio', Codec: 'aac', Language: 'eng', DisplayTitle: 'English' },
    { Index: 2, Type: 'Audio', Codec: 'aac', Language: 'jpn', DisplayTitle: '日本語' },
    { Index: 3, Type: 'Audio', Codec: 'aac', Language: 'chi', DisplayTitle: '国语' },
    { Index: 4, Type: 'Audio', Codec: 'aac', Language: 'kor', DisplayTitle: '한국어' },
  ]

  const subtitleStreams: MediaStream[] = [
    { Index: 5, Type: 'Subtitle', Codec: 'srt', Language: 'eng', DisplayTitle: 'English' },
    { Index: 6, Type: 'Subtitle', Codec: 'ass', Language: 'chi', DisplayTitle: '简体中文' },
    { Index: 7, Type: 'Subtitle', Codec: 'ass', Language: 'chi', DisplayTitle: '繁體中文' },
    { Index: 8, Type: 'Subtitle', Codec: 'vtt', Language: 'jpn', DisplayTitle: '日本語' },
  ]

  it('pickPreferredAudioIndex 支持多语言别名与未命中回退 serverDefault', () => {
    expect(pickPreferredAudioIndex(audioStreams, 'ja', 1)).toBe(2)
    expect(pickPreferredAudioIndex(audioStreams, 'zh', 1)).toBe(3)
    expect(pickPreferredAudioIndex(audioStreams, 'ko', 1)).toBe(4)
    expect(pickPreferredAudioIndex(audioStreams, 'en', 2)).toBe(1)
    expect(pickPreferredAudioIndex(audioStreams, 'fra', 3)).toBe(3)
    expect(pickPreferredAudioIndex(audioStreams, '', 2)).toBe(2)
  })

  it('pickPreferredSubtitleIndex 支持 off 关闭、chs/cht 简繁识别与未命中回退', () => {
    expect(pickPreferredSubtitleIndex(subtitleStreams, 'off', 6)).toBeUndefined()
    expect(pickPreferredSubtitleIndex(subtitleStreams, 'chs', 5)).toBe(6)
    expect(pickPreferredSubtitleIndex(subtitleStreams, 'cht', 5)).toBe(7)
    expect(pickPreferredSubtitleIndex(subtitleStreams, 'jpn', 5)).toBe(8)
    expect(pickPreferredSubtitleIndex(subtitleStreams, 'eng', 6)).toBe(5)
    expect(pickPreferredSubtitleIndex(subtitleStreams, 'spa', 6)).toBe(6)
  })
})

describe('Stream C: PlayerPage 综合修复 (P0-1/P0-2/P0-3/P1-10/P1-11/P0-5)', () => {
  beforeEach(() => {
    localStorage.clear()
    saveAuth({
      accessToken: 'tok-stream-c',
      userId: 'u-1',
      serverId: 'srv-1',
      userName: 'Tester',
      isAdmin: true,
      deviceId: 'dev-stream-c',
    })
    vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined)
    vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => undefined)
    vi.spyOn(HTMLMediaElement.prototype, 'load').mockImplementation(() => undefined)
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('P0-1 & P0-3: 消费 playbackPrefs 默认语言，并在音轨切换后保留 currentTime 与 playbackRate', async () => {
    const user = userEvent.setup()
    savePlaybackPrefs({
      preferredAudioLang: 'jpn',
      preferredSubtitleLang: 'off',
    })

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/emby/Users/u-1') {
        return jsonRes({ Id: 'u-1', Name: 'Tester', Policy: { IsAdministrator: true } })
      }
      if (url === '/emby/Users/u-1/Items/movie-prefs') {
        return jsonRes({
          Id: 'movie-prefs',
          Name: '偏好测试片',
          Type: 'Movie',
          RunTimeTicks: 3_600 * 10_000_000,
        })
      }
      if (url.startsWith('/emby/Items/movie-prefs/PlaybackInfo')) {
        return jsonRes({
          PlaySessionId: 'sess-prefs',
          MediaSources: [
            {
              Id: 'src-prefs',
              Container: 'mp4',
              SupportsDirectPlay: true,
              DefaultAudioStreamIndex: 1,
              DefaultSubtitleStreamIndex: 3,
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
      if (url.startsWith('/emby/Sessions/Playing')) {
        return {
          ok: true,
          status: 204,
          headers: new Headers(),
          json: async () => undefined,
          text: async () => '',
        } as Response
      }
      return jsonRes({})
    })

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const { container } = render(
      <QueryClientProvider client={qc}>
        <AuthProvider>
          <AudioPlayerProvider>
            <MemoryRouter initialEntries={['/play/movie-prefs']}>
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
      expect(video.src).toContain('AudioStreamIndex=2')
    })
    // 偏好字幕为 off，不加载默认中文字幕
    expect(screen.queryByText(/字幕:/)).not.toBeInTheDocument()

    // 模拟播放到 85 秒并切换 1.5x 倍速
    fireEvent.loadedMetadata(video)
    video.currentTime = 85
    await user.click(screen.getByRole('button', { name: '1x' }))
    await user.click(screen.getByRole('button', { name: '1.5x' }))
    expect(video.playbackRate).toBe(1.5)

    // 切换音轨到 English (Index=1)
    await user.click(screen.getByTitle('切换音轨 (A)'))
    await user.click(await screen.findByRole('button', { name: /English/ }))

    await waitFor(() => {
      expect(video.src).toContain('AudioStreamIndex=1')
    })

    // 模拟新流加载完成：恢复 85s 进度与 1.5x 倍速
    video.currentTime = 0
    video.playbackRate = 1
    fireEvent.loadedMetadata(video)
    await waitFor(() => {
      expect(video.currentTime).toBe(85)
      expect(video.playbackRate).toBe(1.5)
    })
  })

  it('P0-2 & P1-11: 当前季最后一集支持跨季 NextUp 下一集，并在切集后重置轨道/进度/错误状态', async () => {
    const user = userEvent.setup()

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/emby/Users/u-1') {
        return jsonRes({ Id: 'u-1', Name: 'Tester', Policy: { IsAdministrator: true } })
      }
      if (url === '/emby/Users/u-1/Items/s1e10') {
        return jsonRes({
          Id: 's1e10',
          Name: '第一季大结局',
          Type: 'Episode',
          SeriesId: 'series-1',
          SeasonId: 'season-1',
          SeriesName: '测试剧集',
        })
      }
      if (url === '/emby/Users/u-1/Items/s2e1') {
        return jsonRes({
          Id: 's2e1',
          Name: '第二季首播',
          Type: 'Episode',
          SeriesId: 'series-1',
          SeasonId: 'season-2',
          SeriesName: '测试剧集',
        })
      }
      if (url.startsWith('/emby/Shows/series-1/Episodes') && url.includes('SeasonId=season-1')) {
        return jsonRes({
          Items: [
            { Id: 's1e9', Name: '第 9 集', Type: 'Episode' },
            { Id: 's1e10', Name: '第一季大结局', Type: 'Episode' },
          ],
          TotalRecordCount: 2,
        })
      }
      if (url.startsWith('/emby/Shows/series-1/Episodes') && url.includes('SeasonId=season-2')) {
        return jsonRes({
          Items: [{ Id: 's2e1', Name: '第二季首播', Type: 'Episode' }],
          TotalRecordCount: 1,
        })
      }
      if (url.startsWith('/emby/Shows/NextUp') && url.includes('SeriesId=series-1')) {
        return jsonRes({
          Items: [
            { Id: 's1e10', Name: '第一季大结局', Type: 'Episode' },
            { Id: 's2e1', Name: '第二季首播', Type: 'Episode', ParentIndexNumber: 2, IndexNumber: 1 },
          ],
          TotalRecordCount: 2,
        })
      }
      if (url.startsWith('/emby/Items/s1e10/PlaybackInfo')) {
        return jsonRes({
          PlaySessionId: 'sess-s1e10',
          MediaSources: [
            {
              Id: 'src-s1e10',
              Container: 'mp4',
              SupportsDirectPlay: true,
              DefaultAudioStreamIndex: 1,
              MediaStreams: [
                { Index: 0, Type: 'Video', Codec: 'h264' },
                { Index: 1, Type: 'Audio', Codec: 'aac', Language: 'chi' },
              ],
            },
          ],
        })
      }
      if (url.startsWith('/emby/Items/s2e1/PlaybackInfo')) {
        return jsonRes({
          PlaySessionId: 'sess-s2e1',
          MediaSources: [
            {
              Id: 'src-s2e1',
              Container: 'mp4',
              SupportsDirectPlay: true,
              DefaultAudioStreamIndex: 2,
              MediaStreams: [
                { Index: 0, Type: 'Video', Codec: 'h264' },
                { Index: 2, Type: 'Audio', Codec: 'aac', Language: 'eng' },
              ],
            },
          ],
        })
      }
      if (url.startsWith('/emby/Sessions/Playing')) {
        return {
          ok: true,
          status: 204,
          headers: new Headers(),
          json: async () => undefined,
          text: async () => '',
        } as Response
      }
      return jsonRes({})
    })

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const { container } = render(
      <QueryClientProvider client={qc}>
        <AuthProvider>
          <AudioPlayerProvider>
            <MemoryRouter initialEntries={['/play/s1e10']}>
              <Routes>
                <Route path="/play/:id" element={<PlayerPage />} />
              </Routes>
            </MemoryRouter>
          </AudioPlayerProvider>
        </AuthProvider>
      </QueryClientProvider>,
    )

    const nextBtn = await screen.findByRole('button', { name: '下一集' })
    expect(nextBtn).toHaveAttribute('title', '下一集：第二季首播')

    await user.click(nextBtn)

    await waitFor(() => {
      expect(screen.getByText(/第二季首播/)).toBeInTheDocument()
    })
    const video = container.querySelector('video') as HTMLVideoElement
    await waitFor(() => {
      expect(video.src).toContain('MediaSourceId=src-s2e1')
      expect(video.src).toContain('AudioStreamIndex=2')
    })
  })

  it('P1-10: 播放错误浮层显示「重试播放」与「返回上一页」，点击「重试播放」可重新发起播放', async () => {
    const user = userEvent.setup()
    let failCount = 0

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/emby/Users/u-1') {
        return jsonRes({ Id: 'u-1', Name: 'Tester', Policy: { IsAdministrator: true } })
      }
      if (url === '/emby/Users/u-1/Items/err-1') {
        return jsonRes({ Id: 'err-1', Name: '异常测试片', Type: 'Movie' })
      }
      if (url.startsWith('/emby/Items/err-1/PlaybackInfo')) {
        failCount += 1
        if (failCount === 1) {
          return jsonRes({ error: 'Network Error' }, 500)
        }
        return jsonRes({
          PlaySessionId: 'sess-recovered',
          MediaSources: [
            {
              Id: 'src-recovered',
              Container: 'mp4',
              SupportsDirectPlay: true,
              MediaStreams: [{ Index: 0, Type: 'Video', Codec: 'h264' }],
            },
          ],
        })
      }
      return jsonRes({})
    })

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={qc}>
        <AuthProvider>
          <AudioPlayerProvider>
            <MemoryRouter initialEntries={['/play/err-1']}>
              <Routes>
                <Route path="/play/:id" element={<PlayerPage />} />
              </Routes>
            </MemoryRouter>
          </AudioPlayerProvider>
        </AuthProvider>
      </QueryClientProvider>,
    )

    expect(await screen.findByText('无法播放该媒体')).toBeInTheDocument()
    const retryBtn = screen.getByRole('button', { name: '重试播放' })
    expect(screen.getByRole('button', { name: '返回上一页' })).toBeInTheDocument()

    await user.click(retryBtn)

    await waitFor(() => {
      expect(screen.queryByText('无法播放该媒体')).not.toBeInTheDocument()
    })
  })

  it('P0-5: 进入 PlayerPage 时自动暂停正在播放的全局音乐播放器', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/emby/Users/u-1') {
        return jsonRes({ Id: 'u-1', Name: 'Tester', Policy: { IsAdministrator: true } })
      }
      if (url === '/emby/Users/u-1/Items/v-1') {
        return jsonRes({ Id: 'v-1', Name: '测试视频', Type: 'Movie' })
      }
      if (url.startsWith('/emby/Items/v-1/PlaybackInfo')) {
        return jsonRes({
          PlaySessionId: 'sess-v1',
          MediaSources: [
            {
              Id: 'src-v1',
              Container: 'mp4',
              SupportsDirectPlay: true,
              MediaStreams: [{ Index: 0, Type: 'Video', Codec: 'h264' }],
            },
          ],
        })
      }
      return jsonRes({})
    })

    function AudioStarter({ onReady }: { onReady: () => void }) {
      const { playTrack, isPlaying } = useAudioPlayer()
      return (
        <div>
          <span data-testid="audio-playing-state">{isPlaying ? 'playing' : 'paused'}</span>
          <button
            type="button"
            onClick={() => {
              playTrack({ Id: 'song-1', Name: 'BGM', Type: 'Audio' })
              onReady()
            }}
          >
            播放背景音乐
          </button>
        </div>
      )
    }

    const user = userEvent.setup()
    let mountPlayer = false
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const { rerender } = render(
      <QueryClientProvider client={qc}>
        <AuthProvider>
          <AudioPlayerProvider>
            <MemoryRouter initialEntries={['/play/v-1']}>
              <AudioStarter
                onReady={() => {
                  mountPlayer = true
                }}
              />
              {mountPlayer && (
                <Routes>
                  <Route path="/play/:id" element={<PlayerPage />} />
                </Routes>
              )}
            </MemoryRouter>
          </AudioPlayerProvider>
        </AuthProvider>
      </QueryClientProvider>,
    )

    await user.click(screen.getByRole('button', { name: '播放背景音乐' }))
    await waitFor(() => {
      expect(screen.getByTestId('audio-playing-state')).toHaveTextContent('playing')
    })

    // 挂载 PlayerPage，验证全局音频自动暂停
    rerender(
      <QueryClientProvider client={qc}>
        <AuthProvider>
          <AudioPlayerProvider>
            <MemoryRouter initialEntries={['/play/v-1']}>
              <AudioStarter onReady={() => undefined} />
              <Routes>
                <Route path="/play/:id" element={<PlayerPage />} />
              </Routes>
            </MemoryRouter>
          </AudioPlayerProvider>
        </AuthProvider>
      </QueryClientProvider>,
    )

    await waitFor(() => {
      expect(screen.getByTestId('audio-playing-state')).toHaveTextContent('paused')
    })
  })

  it('Server HLS 激活期间忽略原生 <video onError>，且 fatal 重试耗尽时先调用 hls.destroy() 再展示错误', async () => {
    let hlsErrorHandler:
      | ((evt: string, data: { fatal: boolean; type: string; details: string }) => void)
      | null = null

    vi.spyOn(Hls, 'isSupported').mockReturnValue(true)
    vi.spyOn(Hls.prototype, 'loadSource').mockImplementation(() => undefined)
    vi.spyOn(Hls.prototype, 'attachMedia').mockImplementation(() => undefined)
    const startLoadSpy = vi.spyOn(Hls.prototype, 'startLoad').mockImplementation(() => undefined)
    const recoverMediaSpy = vi
      .spyOn(Hls.prototype, 'recoverMediaError')
      .mockImplementation(() => undefined)
    const destroySpy = vi.spyOn(Hls.prototype, 'destroy').mockImplementation(() => undefined)
    vi.spyOn(Hls.prototype, 'on').mockImplementation(function (
      this: Hls,
      event: string,
      cb: (...args: unknown[]) => void,
    ) {
      if (event === Hls.Events.ERROR) {
        hlsErrorHandler = cb as (
          evt: string,
          data: { fatal: boolean; type: string; details: string },
        ) => void
      }
      return this
    } as unknown as Hls['on'])

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/emby/Users/u-1') {
        return jsonRes({ Id: 'u-1', Name: 'Tester', Policy: { IsAdministrator: true } })
      }
      if (url === '/emby/Users/u-1/Items/hls-only-1') {
        return jsonRes({ Id: 'hls-only-1', Name: 'HLS 测试片', Type: 'Movie' })
      }
      if (url.startsWith('/emby/Items/hls-only-1/PlaybackInfo')) {
        return jsonRes({
          PlaySessionId: 'sess-hls-only',
          MediaSources: [
            {
              Id: 'src-hls-only',
              Container: 'ts',
              SupportsDirectPlay: false,
              SupportsDirectStream: false,
              SupportsTranscoding: true,
              TranscodingUrl: '/emby/Videos/hls-only-1/master.m3u8?MediaSourceId=src-hls-only',
              MediaStreams: [{ Index: 0, Type: 'Video', Codec: 'hevc' }],
            },
          ],
        })
      }
      if (url.startsWith('/emby/Sessions/Playing')) {
        return {
          ok: true,
          status: 204,
          headers: new Headers(),
          json: async () => undefined,
          text: async () => '',
        } as Response
      }
      return jsonRes({})
    })

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const { container } = render(
      <QueryClientProvider client={qc}>
        <AuthProvider>
          <AudioPlayerProvider>
            <MemoryRouter initialEntries={['/play/hls-only-1']}>
              <Routes>
                <Route path="/play/:id" element={<PlayerPage />} />
              </Routes>
            </MemoryRouter>
          </AudioPlayerProvider>
        </AuthProvider>
      </QueryClientProvider>,
    )

    await waitFor(() => {
      expect(hlsErrorHandler).not.toBeNull()
    })

    const video = container.querySelector('video') as HTMLVideoElement
    expect(video).toBeTruthy()

    // 1) hls.js 激活期间触发原生 <video onError>：必须被忽略，不提前弹出播放失败浮层
    fireEvent.error(video)
    expect(screen.queryByText('无法播放该媒体')).not.toBeInTheDocument()
    expect(destroySpy).not.toHaveBeenCalled()

    // 2) 前 2 次可恢复 fatal 错误分别调用 startLoad / recoverMediaError，不销毁实例
    act(() => {
      hlsErrorHandler!(Hls.Events.ERROR, {
        fatal: true,
        type: Hls.ErrorTypes.NETWORK_ERROR,
        details: 'fragLoadError',
      })
      hlsErrorHandler!(Hls.Events.ERROR, {
        fatal: true,
        type: Hls.ErrorTypes.MEDIA_ERROR,
        details: 'bufferAppendError',
      })
    })
    expect(startLoadSpy).toHaveBeenCalledTimes(1)
    expect(recoverMediaSpy).toHaveBeenCalledTimes(1)
    expect(destroySpy).not.toHaveBeenCalled()
    expect(screen.queryByText('无法播放该媒体')).not.toBeInTheDocument()

    // 3) 第 3 次耗尽重试：先 destroy 实例再展示 HLS 错误，随后 <video onPlaying> 可清理错误状态
    act(() => {
      hlsErrorHandler!(Hls.Events.ERROR, {
        fatal: true,
        type: Hls.ErrorTypes.MEDIA_ERROR,
        details: 'bufferStalledError',
      })
    })
    expect(destroySpy).toHaveBeenCalledTimes(1)
    expect(await screen.findByText('无法播放该媒体')).toBeInTheDocument()

    fireEvent.playing(video)
    await waitFor(() => {
      expect(screen.queryByText('无法播放该媒体')).not.toBeInTheDocument()
    })
  })
})
