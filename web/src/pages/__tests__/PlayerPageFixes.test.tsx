import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { saveAuth } from '../../api/client'
import type { BaseItemDto } from '../../api/types'
import { MediaCard } from '../../components/MediaCard'
import { AuthProvider } from '../../context/AuthContext'
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
