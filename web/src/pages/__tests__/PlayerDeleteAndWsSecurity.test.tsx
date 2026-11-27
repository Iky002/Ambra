import fs from 'node:fs'
import path from 'node:path'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import Hls from 'hls.js'
import { MemoryRouter, Route, Routes, useNavigate } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { RequireAdmin } from '../../App'
import { api, ApiError, getStoredAuth, saveAuth } from '../../api/client'
import type { BaseItemDto, PlaybackInfoResponse } from '../../api/types'
import { MetadataEditorModal } from '../../components/media/MetadataEditorModal'
import { RemoteSubtitleModal } from '../../components/media/RemoteSubtitleModal'
import { AudioPlayerProvider, useAudioPlayer } from '../../context/AudioPlayerContext'
import { AuthProvider } from '../../context/AuthContext'
import { WebSocketProvider } from '../../context/WebSocketContext'
import { setDemoModeOverride } from '../../demo/demoMode'
import { resetDemoStore } from '../../demo/mockServer'
import { I18nProvider } from '../../i18n/I18nContext'
import { AssJassubRenderer } from '../../player/assRenderer'
import * as streamMkvMod from '../../player/streamMkvRemuxer'
import { ItemDetailPage } from '../ItemDetailPage'
import { LiveTvPage } from '../LiveTvPage'
import { PlayerPage } from '../PlayerPage'

function createQc() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
}

function renderWithProviders(
  ui: React.ReactNode,
  initialEntries: string[] = ['/'],
  queryClient?: QueryClient,
) {
  const qc = queryClient ?? createQc()
  return {
    qc,
    ...render(
      <I18nProvider initialLocale="zh-CN">
        <QueryClientProvider client={qc}>
          <AuthProvider>
            <AudioPlayerProvider>
              <MemoryRouter initialEntries={initialEntries}>{ui}</MemoryRouter>
            </AudioPlayerProvider>
          </AuthProvider>
        </QueryClientProvider>
      </I18nProvider>,
    ),
  }
}

describe('Subagent 4 Audit Remediation Suite (C-01, H-14, H-15, M-16..M-18, L-11..L-12, 3.2-5..3.2-9)', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    resetDemoStore()
    setDemoModeOverride(false)
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
    setDemoModeOverride(null)
    localStorage.clear()
    sessionStorage.clear()
  })

  it('[C-01] ItemDetailPage 删除确认框展示高危永久物理删盘警告（含剧集全集提示）并拉取展示 DeleteInfo 物理路径清单', async () => {
    const user = userEvent.setup()
    setDemoModeOverride(true)
    const authRes = await api.authenticateByName('admin', '')
    saveAuth({
      accessToken: authRes.AccessToken,
      userId: authRes.User.Id,
      serverId: authRes.ServerId,
      userName: authRes.User.Name,
      isAdmin: true,
      deviceId: 'dev-c01',
    })

    const deleteInfoSpy = vi.spyOn(api, 'getItemDeleteInfo')

    renderWithProviders(
      <Routes>
        <Route path="/item/:id" element={<ItemDetailPage />} />
      </Routes>,
      ['/item/item-three-body'],
    )

    const delTrigger = await screen.findByRole('button', { name: '删除媒体' })
    await user.click(delTrigger)

    expect(await screen.findByText('确认删除媒体')).toBeInTheDocument()
    expect(screen.getByText(/永久物理删除磁盘上的原始媒体文件/)).toBeInTheDocument()
    expect(screen.getByText(/永久物理删除其下所有单集视频文件/)).toBeInTheDocument()
    expect(screen.getByText(/无法恢复且不可撤销/)).toBeInTheDocument()
    expect(screen.queryByText(/将从媒体库索引中移除该条目的元数据/)).not.toBeInTheDocument()

    await waitFor(() => {
      expect(deleteInfoSpy).toHaveBeenCalledWith('item-three-body')
    })
    const listEl = await screen.findByTestId('delete-info-paths-list')
    expect(within(listEl).getByText(/tvshow\.nfo$/)).toBeInTheDocument()
    expect(within(listEl).getAllByRole('listitem').length).toBeGreaterThanOrEqual(4)
  })

  it('[H-14] PlayerPage 在初始 Seek 完成前保护 start/progress/stop 不覆写 0 进度，且切集时 Stopped 携带上一集快照会话 ID', async () => {
    saveAuth({
      accessToken: 'tok-h14',
      userId: 'u-h14',
      serverId: 'srv-1',
      userName: 'admin',
      isAdmin: true,
      deviceId: 'dev-h14',
    })

    vi.spyOn(api, 'getUser').mockResolvedValue({
      Id: 'u-h14',
      Name: 'admin',
      Policy: { IsAdministrator: true },
    })

    const ep1Item: BaseItemDto = {
      Id: 'ep-1',
      Name: '第 1 集',
      Type: 'Episode',
      MediaType: 'Video',
      RunTimeTicks: 24_000_000_000,
      UserData: { PlaybackPositionTicks: 6_000_000_000, PlayCount: 0, IsFavorite: false, Played: false },
    }
    const ep2Item: BaseItemDto = {
      Id: 'ep-2',
      Name: '第 2 集',
      Type: 'Episode',
      MediaType: 'Video',
      RunTimeTicks: 24_000_000_000,
      UserData: { PlaybackPositionTicks: 0, PlayCount: 0, IsFavorite: false, Played: false },
    }
    const pb1: PlaybackInfoResponse = {
      PlaySessionId: 'ps-ep1-unique',
      MediaSources: [
        {
          Id: 'ms-ep1-unique',
          Name: 'ep1.mp4',
          Container: 'mp4',
          SupportsDirectPlay: true,
          MediaStreams: [{ Index: 0, Type: 'Video', Codec: 'h264' }],
        },
      ],
    }
    const pb2: PlaybackInfoResponse = {
      PlaySessionId: 'ps-ep2-unique',
      MediaSources: [
        {
          Id: 'ms-ep2-unique',
          Name: 'ep2.mp4',
          Container: 'mp4',
          SupportsDirectPlay: true,
          MediaStreams: [{ Index: 0, Type: 'Video', Codec: 'h264' }],
        },
      ],
    }

    vi.spyOn(api, 'getItem').mockImplementation(async (_uid, itemId) =>
      itemId === 'ep-1' ? ep1Item : ep2Item,
    )
    vi.spyOn(api, 'getPlaybackInfo').mockImplementation(async (itemId) =>
      itemId === 'ep-1' ? pb1 : pb2,
    )
    vi.spyOn(api, 'getThumbnailSet').mockResolvedValue({ Thumbnails: [] })

    const startSpy = vi.spyOn(api, 'reportPlaying').mockResolvedValue(undefined)
    const progressSpy = vi.spyOn(api, 'reportProgress').mockResolvedValue(undefined)
    const stopSpy = vi.spyOn(api, 'reportStopped').mockResolvedValue(undefined)

    function EpisodeSwitchHarness() {
      const nav = useNavigate()
      return (
        <>
          <button
            type="button"
            data-testid="jump-ep2"
            onClick={() => nav('/play/ep-2', { replace: true })}
          >
            Jump EP2
          </button>
          <PlayerPage />
        </>
      )
    }

    const qc = createQc()
    // 预热 ep-2 playbackInfo 缓存，复现同步重渲染立即覆盖 playbackMetaRef 的最严苛竞态场景！
    qc.setQueryData(['playbackInfo', 'ep-2', 'u-h14', undefined, undefined], pb2)

    renderWithProviders(
      <Routes>
        <Route path="/play/:id" element={<EpisodeSwitchHarness />} />
      </Routes>,
      ['/play/ep-1?startTimeTicks=6000000000'],
      qc,
    )

    await waitFor(() => {
      expect(startSpy).toHaveBeenCalledWith(
        expect.objectContaining({
          ItemId: 'ep-1',
          MediaSourceId: 'ms-ep1-unique',
          PlaySessionId: 'ps-ep1-unique',
          PositionTicks: 6_000_000_000,
        }),
      )
    })

    // 在 initialSeek 完成前触发 pause（触发 reportState('progress')）-> PositionTicks 必须保持 6_000_000_000 而非 0
    const videoEl = document.querySelector('video')!
    fireEvent.pause(videoEl)
    await waitFor(() => {
      expect(progressSpy).toHaveBeenCalledWith(
        expect.objectContaining({
          ItemId: 'ep-1',
          MediaSourceId: 'ms-ep1-unique',
          PlaySessionId: 'ps-ep1-unique',
          PositionTicks: 6_000_000_000,
        }),
      )
    })

    // 切换到 ep-2（此时 ep-2 的 playbackInfo 已命中缓存同步渲染）
    fireEvent.click(screen.getByTestId('jump-ep2'))

    await waitFor(() => {
      expect(stopSpy).toHaveBeenCalledWith(
        expect.objectContaining({
          ItemId: 'ep-1',
          MediaSourceId: 'ms-ep1-unique',
          PlaySessionId: 'ps-ep1-unique',
          PositionTicks: 6_000_000_000,
        }),
      )
    })
  })

  it('[H-15] WebSocketContext 在 <10s 闪断（如 1013）时执行指数退避不风暴重连，并在 1008 策略违规时立即重验鉴权登出', async () => {
    vi.useFakeTimers()
    saveAuth({
      accessToken: 'tok-ws-test',
      userId: 'u-ws-1',
      serverId: 'srv-1',
      userName: 'alice',
      isAdmin: false,
      deviceId: 'dev-ws',
    })

    let rejectUser401 = false
    const getUserSpy = vi.spyOn(api, 'getUser').mockImplementation(async () => {
      if (rejectUser401) {
        throw new ApiError(401, 'Token revoked')
      }
      return { Id: 'u-ws-1', Name: 'alice', Policy: { IsAdministrator: false } }
    })

    class FakeSocket {
      static instances: FakeSocket[] = []
      static OPEN = 1
      static CONNECTING = 0
      readyState = 0
      url: string
      onopen: (() => void) | null = null
      onclose: ((ev: { code: number }) => void) | null = null
      onerror: (() => void) | null = null
      onmessage: ((ev: { data: string }) => void) | null = null

      constructor(url: string) {
        this.url = url
        FakeSocket.instances.push(this)
      }
      send() {}
      close() {
        this.readyState = 3
      }
      triggerOpen() {
        this.readyState = 1
        this.onopen?.()
      }
      triggerClose(code: number) {
        this.readyState = 3
        this.onclose?.({ code })
      }
    }

    const origWS = globalThis.WebSocket
    globalThis.WebSocket = FakeSocket as unknown as typeof WebSocket

    try {
      const qc = createQc()
      render(
        <QueryClientProvider client={qc}>
          <AuthProvider>
            <WebSocketProvider>
              <div>WS Host</div>
            </WebSocketProvider>
          </AuthProvider>
        </QueryClientProvider>,
      )

      await act(async () => {
        await Promise.resolve()
      })
      expect(FakeSocket.instances).toHaveLength(1)

      // 第 1 次连接打开后仅 200ms 即被服务端以 1013 关闭（<10s 未达稳定阈值）
      act(() => {
        FakeSocket.instances[0].triggerOpen()
        vi.advanceTimersByTime(200)
        FakeSocket.instances[0].triggerClose(1013)
      })

      // 首次退避 1000ms 重连
      act(() => {
        vi.advanceTimersByTime(999)
      })
      expect(FakeSocket.instances).toHaveLength(1)
      act(() => {
        vi.advanceTimersByTime(2)
      })
      expect(FakeSocket.instances).toHaveLength(2)

      // 第 2 次连接再次在 100ms 内闪断 -> 延迟必须翻倍为 2000ms（而非被 onopen 重置回 1000ms！）
      act(() => {
        FakeSocket.instances[1].triggerOpen()
        vi.advanceTimersByTime(100)
        FakeSocket.instances[1].triggerClose(1013)
      })
      act(() => {
        vi.advanceTimersByTime(1500)
      })
      expect(FakeSocket.instances).toHaveLength(2)
      act(() => {
        vi.advanceTimersByTime(600)
      })
      expect(FakeSocket.instances).toHaveLength(3)

      // 第 3 次连接保持 >= 10_000ms 稳定在线后，延迟重置回 1000ms
      act(() => {
        FakeSocket.instances[2].triggerOpen()
        vi.advanceTimersByTime(10_500)
        FakeSocket.instances[2].triggerClose(1006)
      })
      act(() => {
        vi.advanceTimersByTime(1050)
      })
      expect(FakeSocket.instances).toHaveLength(4)

      // 收到 1008 Policy Violation -> 立即调用 getUser 重验鉴权，401 时清理会话不再盲目重连
      rejectUser401 = true
      const callsBefore = getUserSpy.mock.calls.length
      await act(async () => {
        FakeSocket.instances[3].triggerOpen()
        FakeSocket.instances[3].triggerClose(1008)
        await Promise.resolve()
        await Promise.resolve()
      })
      expect(getUserSpy.mock.calls.length).toBeGreaterThan(callsBefore)
      expect(getStoredAuth()).toBeNull()

      act(() => {
        vi.advanceTimersByTime(10_000)
      })
      expect(FakeSocket.instances).toHaveLength(4)
    } finally {
      globalThis.WebSocket = origWS
    }
  })

  it('[M-16] AssJassubRenderer 重叠 mount() 竞态自动丢弃过期续体；PlayerPage 过滤非字体附件并在混合音频跨缓冲 Seek 重载后保持倍速', async () => {
    // 1. AssJassubRenderer mountSeq 重叠挂载保护：慢速首次挂载在第二次挂载发起后才返回响应，必须立即中止且不创建孤儿实例
    const renderer = new AssJassubRenderer()
    let resolveSlowSub1!: (res: Response) => void
    const fetchSubSpy = vi.spyOn(globalThis, 'fetch').mockImplementation((input) => {
      const url = String(input)
      if (url === '/sub1.ass') {
        return new Promise<Response>((resolve) => {
          resolveSlowSub1 = resolve
        })
      }
      // /sub2.ass 被立刻取消或返回失败状态前检查 mountSeq
      return Promise.resolve(new Response('[Script Info]\nTitle: Sub2', { status: 404 }))
    })

    const v = document.createElement('video')
    const c = document.createElement('canvas')
    const p1 = renderer.mount(v, c, '/sub1.ass')
    await Promise.resolve()
    await Promise.resolve()
    const p2 = renderer.mount(v, c, '/sub2.ass').catch(() => undefined)
    await p2

    // 让第一次 mount 的慢速 fetch 延迟返回 200 -> 因 seq !== mountSeq，必须静默退出，绝不实例化 JASSUB 覆盖当前状态
    resolveSlowSub1?.(new Response('[Script Info]\nTitle: StaleSub1', { status: 200 }))
    await expect(p1).resolves.toBeUndefined()
    expect((renderer as unknown as { instance: unknown }).instance).toBeNull()
    await renderer.destroy()
    fetchSubSpy.mockRestore()

    // 2. PlayerPage 附件字体扩展名白名单过滤 + 混合双流 Seek 后倍速保持
    localStorage.setItem(
      'ambra_player_ui_prefs',
      JSON.stringify({ volume: 1, muted: false, playbackRate: 1.5, aspectMode: 'contain' }),
    )
    vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined)
    saveAuth({
      accessToken: 'tok-m16',
      userId: 'u-m16',
      serverId: 'srv-1',
      userName: 'admin',
      isAdmin: true,
      deviceId: 'dev-m16',
    })
    vi.spyOn(api, 'getUser').mockResolvedValue({
      Id: 'u-m16',
      Name: 'admin',
      Policy: { IsAdministrator: true },
    })
    vi.spyOn(api, 'getItem').mockResolvedValue({
      Id: 'mkv-hdr-1',
      Name: '杜比测试片',
      Type: 'Movie',
      MediaType: 'Video',
      RunTimeTicks: 36_000_000_000,
    })
    vi.spyOn(api, 'getPlaybackInfo').mockResolvedValue({
      PlaySessionId: 'ps-m16',
      MediaSources: [
        {
          Id: 'ms-m16',
          Name: 'hdr.mkv',
          Container: 'mkv',
          Size: 8 * 1024 * 1024 * 1024,
          SupportsDirectPlay: false,
          SupportsDirectStream: true,
          SupportsTranscoding: true,
          TranscodingUrl: '/emby/Videos/mkv-hdr-1/master.m3u8',
          MediaStreams: [
            { Index: 0, Type: 'Video', Codec: 'h264' },
            { Index: 1, Type: 'Audio', Codec: 'eac3', IsDefault: true },
            { Index: 2, Type: 'Subtitle', Codec: 'ass', IsDefault: true },
          ],
        },
      ],
    })
    vi.spyOn(api, 'getThumbnailSet').mockResolvedValue({ Thumbnails: [] })
    vi.spyOn(api, 'reportPlaying').mockResolvedValue(undefined)
    vi.spyOn(api, 'reportProgress').mockResolvedValue(undefined)
    vi.spyOn(api, 'reportStopped').mockResolvedValue(undefined)
    vi.spyOn(api, 'getMediaAttachments').mockResolvedValue([
      { Index: 0, FileName: 'CustomSub.ttf', Url: '/emby/Videos/mkv-hdr-1/ms-m16/Attachments/0' },
      { Index: 1, FileName: 'HeaderFont.woff2', Url: '/emby/Videos/mkv-hdr-1/ms-m16/Attachments/1' },
      { Index: 2, FileName: 'cover.jpg', Url: '/emby/Videos/mkv-hdr-1/ms-m16/Attachments/2' },
      { Index: 3, FileName: 'evil.js', Url: '/emby/Videos/mkv-hdr-1/ms-m16/Attachments/3' },
    ])

    const origMS = globalThis.MediaSource
    globalThis.MediaSource = {
      isTypeSupported: () => true,
    } as unknown as typeof MediaSource

    try {
      const attachSpy = vi.spyOn(streamMkvMod.StreamMkvRemuxer.prototype, 'attach').mockResolvedValue({
        docType: 'matroska',
        durationSec: 3600,
        cuesCount: 10,
      })
      const mountSpy = vi
        .spyOn(AssJassubRenderer.prototype, 'mount')
        .mockResolvedValue(undefined)

      renderWithProviders(
        <Routes>
          <Route path="/play/:id" element={<PlayerPage />} />
        </Routes>,
        ['/play/mkv-hdr-1?subtitle=2'],
      )

      await waitFor(() => {
        expect(mountSpy).toHaveBeenCalled()
        expect(attachSpy).toHaveBeenCalledWith(
          expect.any(HTMLVideoElement),
          expect.any(String),
          expect.any(Object),
          expect.objectContaining({ videoOnly: true }),
        )
      })
      const passedFonts = mountSpy.mock.calls[0]?.[3]?.fonts || []
      expect(passedFonts).toHaveLength(2)
      expect(
        passedFonts.every((u) => u.includes('/Attachments/0') || u.includes('/Attachments/1')),
      ).toBe(true)

      const videoEl = attachSpy.mock.calls[0]![0]
      const hybridAudio = screen.getByTestId('hybrid-audio-element') as HTMLAudioElement
      // 模拟浏览器在 load() 时将 audio.playbackRate 重置为 1
      const loadSpy = vi.spyOn(hybridAudio, 'load').mockImplementation(() => {
        hybridAudio.playbackRate = 1
      })

      Object.defineProperty(videoEl, 'currentTime', {
        configurable: true,
        writable: true,
        value: 600,
      })
      fireEvent.seeked(videoEl)
      expect(loadSpy).toHaveBeenCalled()
      expect(hybridAudio.playbackRate).toBe(1.5)

      // 即使浏览器在 loadedmetadata 时再次重置，onLoadedMetadata 也会立即恢复为 1.5
      hybridAudio.playbackRate = 1
      fireEvent.loadedMetadata(hybridAudio)
      expect(hybridAudio.playbackRate).toBe(1.5)
    } finally {
      globalThis.MediaSource = origMS
    }
  })

  it('[M-17 & 3.2-5] LiveTvPage 播放直播/录像时暂停后台音乐，支持解绑 EPG 映射，录像 HLS 降级时重挂载 video，且节目单拉取大窗口避免饥饿', async () => {
    const user = userEvent.setup()
    saveAuth({
      accessToken: 'tok-ltv',
      userId: 'u-ltv',
      serverId: 'srv-1',
      userName: 'admin',
      isAdmin: true,
      deviceId: 'dev-ltv',
    })
    vi.spyOn(api, 'getUser').mockResolvedValue({
      Id: 'u-ltv',
      Name: 'admin',
      Policy: { IsAdministrator: true },
    })
    vi.spyOn(api, 'getLiveTvChannels').mockResolvedValue({
      Items: [{ Id: 'ch-1', Name: 'CCTV-1', Number: '1', IsHls: false }],
      TotalRecordCount: 1,
    })
    const progsSpy = vi
      .spyOn(api, 'getLiveTvPrograms')
      .mockResolvedValue({ Items: [], TotalRecordCount: 0 })
    vi.spyOn(api, 'getLiveTvTimers').mockResolvedValue({ Items: [], TotalRecordCount: 0 })
    vi.spyOn(api, 'getListingProviders').mockResolvedValue([{ Id: 'prov-1', Type: 'xmltv', Path: '/epg.xml' }])
    vi.spyOn(api, 'getChannelMappings').mockResolvedValue({
      TunerChannels: [{ Id: 'ch-1', Name: 'CCTV-1', Number: '1' }],
      ProviderChannels: [{ Id: 'epg-1', Name: 'CCTV1 综合' }],
      Mappings: [
        {
          ProviderId: 'prov-1',
          TunerChannelId: 'ch-1',
          ProviderChannelId: 'epg-1',
        },
      ],
      ProviderName: 'prov-1',
    })
    const setMapSpy = vi
      .spyOn(api, 'setChannelMapping')
      .mockResolvedValue({ TunerChannelId: 'ch-1', ProviderChannelId: '' })
    vi.spyOn(api, 'getLiveTvRecordings').mockResolvedValue({
      Items: [
        {
          Id: 'rec-ts-1',
          Name: '新闻联播录像',
          ChannelName: 'CCTV-1',
          Status: 'Completed',
          StartDate: '2026-11-26T12:00:00Z',
          EndDate: '2026-11-26T12:30:00Z',
        },
      ],
      TotalRecordCount: 1,
    })

    let latestHlsErrorCb: ((evt: string, data: { fatal: boolean }) => void) | null = null
    vi.spyOn(Hls, 'isSupported').mockReturnValue(true)
    vi.spyOn(Hls.prototype, 'loadSource').mockImplementation(() => {})
    vi.spyOn(Hls.prototype, 'attachMedia').mockImplementation(() => {})
    vi.spyOn(Hls.prototype, 'destroy').mockImplementation(() => {})
    vi.spyOn(Hls.prototype, 'on').mockImplementation(function (this: Hls, evt: any, cb: any) {
      if (evt === Hls.Events.ERROR) {
        latestHlsErrorCb = cb
      }
      return this
    })

    function MusicStarterHarness() {
      const { playTrack, isPlaying } = useAudioPlayer()
      return (
        <div>
          <button
            type="button"
            data-testid="start-bg-music"
            onClick={() =>
              playTrack({ Id: 'song-1', Name: '测试曲目', Type: 'Audio', MediaType: 'Audio' })
            }
          >
            Play Music
          </button>
          <span data-testid="bg-music-state">{isPlaying ? 'playing' : 'paused'}</span>
          <LiveTvPage />
        </div>
      )
    }

    vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined)

    renderWithProviders(
      <Routes>
        <Route path="/livetv" element={<MusicStarterHarness />} />
      </Routes>,
      ['/livetv'],
    )

    // 3.2-5: EPG 查询携带充足 Limit (>= 2000) 与时间窗口过滤选项
    await waitFor(() => {
      expect(progsSpy).toHaveBeenCalledWith(
        ['ch-1'],
        expect.any(Number),
        expect.objectContaining({
          minEndDate: expect.any(String),
          maxStartDate: expect.any(String),
        }),
      )
    })
    expect(progsSpy.mock.calls[0]?.[1]).toBeGreaterThanOrEqual(2000)

    // 3.2-5: 切换到频道映射 tab，选择空值解绑并点击保存
    await user.click(await screen.findByRole('tab', { name: /频道映射|映射|Mapping/i }))
    const mapSelect = (await screen.findByLabelText('CCTV-1 EPG 频道映射')) as HTMLSelectElement
    expect(mapSelect.value).toBe('epg-1')
    const saveBtn = screen.getByRole('button', { name: /保存映射/i })
    expect(saveBtn).toBeDisabled()

    await user.selectOptions(mapSelect, '')
    expect(saveBtn).not.toBeDisabled()
    await user.click(saveBtn)
    await waitFor(() => {
      expect(setMapSpy).toHaveBeenCalledWith(
        expect.objectContaining({
          TunerChannelId: 'ch-1',
          ProviderChannelId: '',
        }),
      )
    })

    // M-17: 播放后台音乐后打开 DVR 录像 -> 自动暂停后台音乐，且 HLS fatal 错误降级直出时重挂载 <video>
    await user.click(screen.getByTestId('start-bg-music'))
    expect(screen.getByTestId('bg-music-state').textContent).toBe('playing')

    await user.click(screen.getByRole('tab', { name: /已录节目|Recordings/i }))
    const recPanel = await screen.findByTestId('livetv-recordings-panel')
    const playRecBtn = await within(recPanel).findByRole('button', { name: '播放' })
    await user.click(playRecBtn)
    expect(screen.getByTestId('bg-music-state').textContent).toBe('paused')

    const hlsVideoEl = await screen.findByTestId('dvr-recording-video')
    expect(hlsVideoEl.getAttribute('src')).toBeNull()

    act(() => {
      latestHlsErrorCb?.(Hls.Events.ERROR, { fatal: true })
    })

    const directVideoEl = await screen.findByTestId('dvr-recording-video')
    expect(directVideoEl).not.toBe(hlsVideoEl)
    expect(directVideoEl.getAttribute('src')).toContain('/emby/LiveTv/LiveRecordings/rec-ts-1/stream')
  })

  it('[M-18 & L-11] CSP connect-src 收紧、RequireAdmin 在鉴权网络错误时展示可重试错误态、整季标记并发度受限于 <= 6', async () => {
    // M-18: 校验 vercel.json 与 public/_headers 的 connect-src 已移除明文 http: 与 ws:
    const headersRaw = fs.readFileSync(
      path.resolve(process.cwd(), 'public/_headers'),
      'utf-8',
    )
    const vercelRaw = fs.readFileSync(
      path.resolve(process.cwd(), 'vercel.json'),
      'utf-8',
    )
    expect(headersRaw).toContain("connect-src 'self' https: wss: blob: data:;")
    expect(vercelRaw).toContain("connect-src 'self' https: wss: blob: data:;")

    // L-11: RequireAdmin 在 getUser 网络异常（非 401）时渲染 QueryErrorState 并支持点击重试恢复
    saveAuth({
      accessToken: 'tok-admin-net',
      userId: 'u-admin-net',
      serverId: 'srv-1',
      userName: 'admin',
      isAdmin: true,
      deviceId: 'dev-net',
    })
    let shouldFail = true
    const getUserSpy = vi.spyOn(api, 'getUser').mockImplementation(async () => {
      if (shouldFail) {
        throw new Error('上游网关 502 Bad Gateway')
      }
      return { Id: 'u-admin-net', Name: 'admin', Policy: { IsAdministrator: true } }
    })

    const user = userEvent.setup()
    renderWithProviders(
      <RequireAdmin>
        <div data-testid="admin-protected-content">管理后台机密面板</div>
      </RequireAdmin>,
    )

    expect(await screen.findByText(/502 Bad Gateway/)).toBeInTheDocument()
    expect(screen.queryByText(/无管理员权限|需要管理员权限/)).not.toBeInTheDocument()

    shouldFail = false
    await user.click(screen.getByRole('button', { name: /重试|Retry/i }))
    expect(await screen.findByTestId('admin-protected-content')).toBeInTheDocument()
    expect(getUserSpy).toHaveBeenCalledTimes(2)

    // L-11: ItemDetailPage 整季已看批量操作并发数严格 <= 6
    const episodes: BaseItemDto[] = Array.from({ length: 18 }, (_, i) => ({
      Id: `ep-batch-${i + 1}`,
      Name: `第 ${i + 1} 集`,
      Type: 'Episode',
      SeriesId: 'ser-batch',
      SeasonId: 'sea-batch',
      IndexNumber: i + 1,
      ParentIndexNumber: 1,
      UserData: { Played: false, PlayCount: 0, IsFavorite: false, PlaybackPositionTicks: 0 },
    }))
    vi.spyOn(api, 'getItem').mockResolvedValue({
      Id: 'ser-batch',
      Name: '长篇剧集',
      Type: 'Series',
    })
    vi.spyOn(api, 'getSeasons').mockResolvedValue({
      Items: [{ Id: 'sea-batch', Name: '第 1 季', Type: 'Season', IndexNumber: 1 }],
      TotalRecordCount: 1,
    })
    vi.spyOn(api, 'getEpisodes').mockResolvedValue({
      Items: episodes,
      TotalRecordCount: episodes.length,
    })
    vi.spyOn(api, 'getNextUp').mockResolvedValue({ Items: [episodes[0]], TotalRecordCount: 1 })
    vi.spyOn(api, 'getSimilarItems').mockResolvedValue({ Items: [], TotalRecordCount: 0 })
    vi.spyOn(api, 'getLocalTrailers').mockResolvedValue([])
    vi.spyOn(api, 'getSpecialFeatures').mockResolvedValue([])
    vi.spyOn(api, 'getThemeSongs').mockResolvedValue({ Items: [], TotalRecordCount: 0 })

    let inFlight = 0
    let maxInFlight = 0
    const toggleSpy = vi.spyOn(api, 'togglePlayed').mockImplementation(async (_uid, itemId) => {
      if (itemId.startsWith('ep-batch-')) {
        inFlight++
        maxInFlight = Math.max(maxInFlight, inFlight)
        await new Promise((r) => setTimeout(r, 15))
        inFlight--
      }
      return { Played: true, PlayCount: 1, IsFavorite: false, PlaybackPositionTicks: 0 }
    })

    renderWithProviders(
      <Routes>
        <Route path="/item/:id" element={<ItemDetailPage />} />
      </Routes>,
      ['/item/ser-batch'],
    )

    const markSeasonBtn = await screen.findByRole('button', { name: /标记本季已看/i })
    await user.click(markSeasonBtn)
    await waitFor(() => {
      expect(toggleSpy).toHaveBeenCalledTimes(19) // 1 season + 18 episodes
    })
    expect(maxInFlight).toBeLessThanOrEqual(6)
    expect(maxInFlight).toBeGreaterThan(1)
  })

  it('[3.2-6 & 3.2-8] MetadataEditorModal 支持清空年份/评分/分级并自动保存未提交锁定项；RemoteSubtitleModal 切换 itemId 时重置状态', async () => {
    const user = userEvent.setup()
    const updateSpy = vi.spyOn(api, 'updateItemMetadata').mockResolvedValue(undefined)
    const lockSpy = vi.spyOn(api, 'lockItemFields').mockResolvedValue(undefined)

    const sampleItem: BaseItemDto = {
      Id: 'mov-edit-1',
      Name: '测试电影',
      Type: 'Movie',
      ProductionYear: 2022,
      CommunityRating: 8.6,
      OfficialRating: 'PG-13',
      LockedFields: [],
      LockData: false,
    }

    const qc = createQc()
    const { unmount } = render(
      <QueryClientProvider client={qc}>
        <MetadataEditorModal open item={sampleItem} onClose={() => undefined} />
      </QueryClientProvider>,
    )

    const yearInput = screen.getByDisplayValue('2022')
    const ratingInput = screen.getByDisplayValue('8.6')
    const officialInput = screen.getByDisplayValue('PG-13')

    await user.clear(yearInput)
    await user.clear(ratingInput)
    await user.clear(officialInput)

    // 切换字段锁定开关但不单独点「保存锁定配置」，直接点「保存元数据」
    await user.click(screen.getByRole('button', { name: /片名 \(Name\)/i }))
    await user.click(screen.getByRole('button', { name: /保存元数据/i }))

    await waitFor(() => {
      expect(updateSpy).toHaveBeenCalledWith(
        'mov-edit-1',
        expect.objectContaining({
          ProductionYear: null,
          CommunityRating: null,
          OfficialRating: '',
          LockData: false,
          LockedFields: ['Name'],
        }),
      )
      expect(lockSpy).toHaveBeenCalledWith('mov-edit-1', ['Name'], false)
    })
    unmount()

    // 3.2-8: RemoteSubtitleModal 在 itemId 切换时重置 downloadedIds、language 与 searchLanguage
    const searchSubSpy = vi.spyOn(api, 'searchRemoteSubtitles').mockImplementation(async (id, lang) => [
      {
        Id: 'shared-sub-id',
        Name: `${id}-${lang}.srt`,
        Format: 'srt',
      },
    ])
    vi.spyOn(api, 'downloadRemoteSubtitle').mockResolvedValue(undefined)

    const { rerender } = render(
      <I18nProvider initialLocale="zh-CN">
        <QueryClientProvider client={qc}>
          <RemoteSubtitleModal
            open
            itemId="ep-sub-1"
            itemName="第 1 集"
            onClose={() => undefined}
          />
        </QueryClientProvider>
      </I18nProvider>,
    )

    const dlBtn1 = await screen.findByRole('button', { name: '下载' })
    await user.click(dlBtn1)
    expect(await screen.findByText('已下载')).toBeInTheDocument()

    const langSelect = screen.getByLabelText('在线字幕语言') as HTMLSelectElement
    await user.selectOptions(langSelect, 'jpn')
    expect(langSelect.value).toBe('jpn')

    // 切换到 ep-sub-2（弹窗保持打开或跨条目切换）
    rerender(
      <I18nProvider initialLocale="zh-CN">
        <QueryClientProvider client={qc}>
          <RemoteSubtitleModal
            open
            itemId="ep-sub-2"
            itemName="第 2 集"
            onClose={() => undefined}
          />
        </QueryClientProvider>
      </I18nProvider>,
    )

    await waitFor(() => {
      expect((screen.getByLabelText('在线字幕语言') as HTMLSelectElement).value).toBe('chi')
      expect(searchSubSpy).toHaveBeenCalledWith('ep-sub-2', 'chi')
    })
    // 相同的字幕结果 ID 'shared-sub-id' 在新条目下绝不能被误标为「已下载」
    expect(await screen.findByRole('button', { name: '下载' })).not.toBeDisabled()
  })
})
