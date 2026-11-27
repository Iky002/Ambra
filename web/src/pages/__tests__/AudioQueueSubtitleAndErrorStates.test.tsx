import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes, useLocation, useNavigate } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { saveAuth } from '../../api/client'
import type { BaseItemDto } from '../../api/types'
import { AudioPlayerBar } from '../../components/AudioPlayerBar'
import { AudioPlayerProvider, useAudioPlayer } from '../../context/AudioPlayerContext'
import { AuthProvider } from '../../context/AuthContext'
import { setDemoModeOverride } from '../../demo/demoMode'
import { savePlaybackPrefs } from '../../player/playbackPrefs'
import { CollectionsPage } from '../CollectionsPage'
import { FavoritesPage } from '../FavoritesPage'
import { HomePage } from '../HomePage'
import { ItemDetailPage } from '../ItemDetailPage'
import { LibraryPage } from '../LibraryPage'
import { PersonDetailPage } from '../PersonDetailPage'
import { PlayerPage } from '../PlayerPage'
import { SearchPage } from '../SearchPage'
import { TrendingPage } from '../TrendingPage'

function jsonRes(data: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    headers: new Headers({ 'content-type': 'application/json' }),
    json: async () => data,
    text: async () => JSON.stringify(data),
  } as Response
}

function renderWithProviders(ui: React.ReactNode, initialEntries: string[]) {
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

function LocationProbe() {
  const loc = useLocation()
  return <div data-testid="location-probe">{`${loc.pathname}${loc.search}`}</div>
}

describe('Round-2 Web UI Final 4 P1 Polish Fixes', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    setDemoModeOverride(false)
    saveAuth({
      accessToken: 'tok-round2-p1',
      userId: 'u-1',
      serverId: 'srv-1',
      userName: 'Tester',
      isAdmin: true,
      deviceId: 'dev-round2',
    })
    vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined)
    vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => undefined)
    vi.spyOn(HTMLMediaElement.prototype, 'load').mockImplementation(() => undefined)
  })

  afterEach(() => {
    setDemoModeOverride(null)
    vi.restoreAllMocks()
  })

  describe('FE-P1-1: AudioPlayerContext retry & removeFromQueue', () => {
    it('AudioPlayerBar 错误条「重试」调用 retry() 重新加载音频流并恢复播放，且 removeFromQueue 正确维护队列与索引', async () => {
      const user = userEvent.setup()
      const loadSpy = vi.spyOn(HTMLMediaElement.prototype, 'load')
      const playSpy = vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined)

      vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
        const url = String(input)
        if (url === '/emby/Users/u-1') {
          return jsonRes({ Id: 'u-1', Name: 'Tester', ServerId: 'srv-1', Policy: { IsAdministrator: true } })
        }
        return jsonRes({})
      })

      const tracks: BaseItemDto[] = [
        { Id: 't-1', Name: 'Track 1', Type: 'Audio', MediaType: 'Audio', RunTimeTicks: 180_000_0000 },
        { Id: 't-2', Name: 'Track 2', Type: 'Audio', MediaType: 'Audio', RunTimeTicks: 200_000_0000 },
        { Id: 't-3', Name: 'Track 3', Type: 'Audio', MediaType: 'Audio', RunTimeTicks: 220_000_0000 },
      ]

      function AudioHarness() {
        const { playQueue, removeFromQueue, currentIndex, queue, currentTrack } = useAudioPlayer()
        return (
          <div>
            <button type="button" onClick={() => playQueue(tracks, 1)}>
              start-queue
            </button>
            <button type="button" onClick={() => removeFromQueue(0)}>
              remove-first
            </button>
            <button type="button" onClick={() => removeFromQueue(0)}>
              remove-current
            </button>
            <div data-testid="audio-state">
              {`${queue.length}|${currentIndex}|${currentTrack?.Id ?? 'none'}`}
            </div>
          </div>
        )
      }

      const { container } = renderWithProviders(
        <>
          <AudioHarness />
          <AudioPlayerBar />
        </>,
        ['/'],
      )

      await user.click(screen.getByRole('button', { name: 'start-queue' }))
      expect(await screen.findByTestId('audio-player-bar')).toBeInTheDocument()
      expect(screen.getByTestId('audio-state')).toHaveTextContent('3|1|t-2')

      // 触发 <audio> error 事件
      const audioEl = container.querySelector('audio') as HTMLAudioElement
      expect(audioEl).toBeTruthy()
      fireEvent.error(audioEl)
      expect(await screen.findByTestId('audio-playback-error')).toBeInTheDocument()

      loadSpy.mockClear()
      playSpy.mockClear()

      // 点击「重试」应调用 retry() -> audio.load() + audio.play() 且清除错误条
      await user.click(screen.getByRole('button', { name: '重试' }))
      await waitFor(() => {
        expect(loadSpy).toHaveBeenCalledTimes(1)
        expect(playSpy).toHaveBeenCalledTimes(1)
        expect(screen.queryByTestId('audio-playback-error')).not.toBeInTheDocument()
      })
      expect(audioEl.getAttribute('src')).toContain('/emby/Audio/t-2/universal')

      // 模拟 audio.networkState === NETWORK_NO_SOURCE 时点暂停再点播放触发 retry()
      await user.click(screen.getByRole('button', { name: '暂停' }))
      Object.defineProperty(audioEl, 'networkState', { configurable: true, value: 3 })
      loadSpy.mockClear()
      playSpy.mockClear()
      await user.click(screen.getByRole('button', { name: '播放' }))
      await waitFor(() => {
        expect(loadSpy).toHaveBeenCalledTimes(1)
        expect(playSpy).toHaveBeenCalledTimes(1)
      })

      // removeFromQueue：移除当前曲目之前的索引 0，currentIndex 应从 1 变为 0 且当前曲目仍为 t-2
      await user.click(screen.getByRole('button', { name: 'remove-first' }))
      expect(screen.getByTestId('audio-state')).toHaveTextContent('2|0|t-2')

      // 移除当前曲目索引 0，队列剩 1 首 t-3，currentIndex 为 0
      await user.click(screen.getByRole('button', { name: 'remove-current' }))
      expect(screen.getByTestId('audio-state')).toHaveTextContent('1|0|t-3')
    })
  })

  describe('FE-P1-2: subtitle=off 透传与 PlayerPage 不覆盖', () => {
    it('ItemDetailPage 关闭字幕后点击播放携带 subtitle=off', async () => {
      const user = userEvent.setup()
      vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
        const url = String(input)
        if (url === '/emby/Users/u-1') {
          return jsonRes({ Id: 'u-1', Name: 'Tester', ServerId: 'srv-1', Policy: { IsAdministrator: true } })
        }
        if (url === '/emby/Users/u-1/Items/movie-sub') {
          return jsonRes({
            Id: 'movie-sub',
            Name: '字幕测试片',
            Type: 'Movie',
            MediaType: 'Video',
            MediaSources: [
              {
                Id: 'src-sub',
                Name: '4K HDR',
                Container: 'mp4',
                DefaultAudioStreamIndex: 1,
                DefaultSubtitleStreamIndex: 2,
                MediaStreams: [
                  { Index: 0, Type: 'Video', Codec: 'h264', Width: 3840, Height: 2160 },
                  { Index: 1, Type: 'Audio', Codec: 'aac', Language: 'chi', DisplayTitle: '国语 AAC' },
                  { Index: 2, Type: 'Subtitle', Codec: 'subrip', Language: 'chi', DisplayTitle: '简体中文' },
                ],
              },
            ],
          })
        }
        return jsonRes({ Items: [], TotalRecordCount: 0 })
      })

      renderWithProviders(
        <Routes>
          <Route path="/item/:id" element={<ItemDetailPage />} />
          <Route path="/play/:id" element={<LocationProbe />} />
        </Routes>,
        ['/item/movie-sub'],
      )

      expect(await screen.findByText('字幕测试片')).toBeInTheDocument()

      // 将字幕选择器切换为「关闭字幕」(-1)
      const subSelect = screen.getByLabelText(/字幕 \(1\)/)
      await user.selectOptions(subSelect, '-1')

      await user.click(screen.getByRole('button', { name: '立即播放' }))

      const locText = (await screen.findByTestId('location-probe')).textContent || ''
      expect(locText).toContain('/play/movie-sub?')
      expect(locText).toContain('subtitle=off')
    })

    it('PlayerPage 收到 ?subtitle=off 时保持关闭字幕，不被默认字幕或 preferredSubtitleLang 覆盖', async () => {
      savePlaybackPrefs({ preferredSubtitleLang: 'chs' })

      const fetchSpy = vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
        const url = String(input)
        if (url === '/emby/Users/u-1') {
          return jsonRes({ Id: 'u-1', Name: 'Tester', ServerId: 'srv-1', Policy: { IsAdministrator: true } })
        }
        if (url === '/emby/Users/u-1/Items/movie-off') {
          return jsonRes({ Id: 'movie-off', Name: '关闭字幕测试', Type: 'Movie' })
        }
        if (url.startsWith('/emby/Items/movie-off/PlaybackInfo')) {
          return jsonRes({
            PlaySessionId: 'sess-off',
            MediaSources: [
              {
                Id: 'src-off',
                Container: 'mp4',
                SupportsDirectPlay: true,
                DirectStreamUrl: '/emby/Videos/movie-off/stream?Static=true&MediaSourceId=src-off',
                DefaultAudioStreamIndex: 1,
                DefaultSubtitleStreamIndex: 2,
                MediaStreams: [
                  { Index: 0, Type: 'Video', Codec: 'h264', Width: 1920, Height: 1080 },
                  { Index: 1, Type: 'Audio', Codec: 'aac', Language: 'chi' },
                  { Index: 2, Type: 'Subtitle', Codec: 'subrip', Language: 'chi', DisplayTitle: '简体中文' },
                ],
              },
            ],
          })
        }
        return jsonRes({})
      })

      renderWithProviders(
        <Routes>
          <Route path="/play/:id" element={<PlayerPage />} />
        </Routes>,
        ['/play/movie-off?mediaSourceId=src-off&audio=1&subtitle=off'],
      )

      await waitFor(() => {
        expect(
          fetchSpy.mock.calls.some(([u]) => String(u).startsWith('/emby/Items/movie-off/PlaybackInfo')),
        ).toBe(true)
      })

      // 等待一个 tick，确认没有拉取字幕流且 OSD 未展示字幕激活状态
      await new Promise((r) => setTimeout(r, 40))
      expect(fetchSpy.mock.calls.some(([u]) => String(u).includes('/Subtitles/'))).toBe(false)
      expect(screen.queryByText(/字幕:/)).not.toBeInTheDocument()
    })
  })

  describe('FE-P1-3: QueryErrorState 覆盖 7 个主列表/详情页', () => {
    it('CollectionsPage 详情页在 getItem 失败时展示 QueryErrorState，不卡在「加载中...」', async () => {
      vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
        const url = String(input)
        if (url === '/emby/Users/u-1') {
          return jsonRes({ Id: 'u-1', Name: 'Tester', ServerId: 'srv-1', Policy: { IsAdministrator: true } })
        }
        if (url === '/emby/Users/u-1/Items/col-bad') {
          return jsonRes({ message: '合集不存在' }, 404)
        }
        return jsonRes({ Items: [], TotalRecordCount: 0 })
      })

      renderWithProviders(
        <Routes>
          <Route path="/collections/:id" element={<CollectionsPage />} />
        </Routes>,
        ['/collections/col-bad'],
      )

      expect(await screen.findByTestId('query-error-state')).toBeInTheDocument()
      expect(screen.getByText('无法加载合集或播放列表详情')).toBeInTheDocument()
      expect(screen.queryByText('加载中...')).not.toBeInTheDocument()
    })

    it('TrendingPage 在 TMDB 查询失败时展示错误态，且不触发 Trending 回退查询', async () => {
      const fetchSpy = vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
        const url = String(input)
        if (url === '/emby/Users/u-1') {
          return jsonRes({ Id: 'u-1', Name: 'Tester', ServerId: 'srv-1', Policy: { IsAdministrator: true } })
        }
        if (url.includes('Tags=TMDB')) {
          return jsonRes({ message: 'TMDB 榜单服务异常' }, 500)
        }
        return jsonRes({ Items: [], TotalRecordCount: 0 })
      })

      renderWithProviders(
        <Routes>
          <Route path="/trending" element={<TrendingPage />} />
        </Routes>,
        ['/trending'],
      )

      expect(await screen.findByText('无法加载TMDB 热门')).toBeInTheDocument()
      expect(fetchSpy.mock.calls.some(([u]) => String(u).includes('Tags=Trending'))).toBe(false)
    })

    it('HomePage / LibraryPage / FavoritesPage / PersonDetailPage / SearchPage 在请求失败时均渲染 QueryErrorState', async () => {
      vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
        const url = String(input)
        if (url === '/emby/Users/u-1') {
          return jsonRes({ Id: 'u-1', Name: 'Tester', ServerId: 'srv-1', Policy: { IsAdministrator: true } })
        }
        return jsonRes({ message: '后端不可用' }, 500)
      })

      const cases: Array<{ path: string; route: string; element: React.ReactElement }> = [
        { path: '/', route: '/', element: <HomePage /> },
        { path: '/library/lib-1', route: '/library/:id', element: <LibraryPage /> },
        { path: '/favorites', route: '/favorites', element: <FavoritesPage /> },
        { path: '/person/p-1?name=Nolan', route: '/person/:id', element: <PersonDetailPage /> },
        { path: '/search?q=matrix', route: '/search', element: <SearchPage /> },
      ]

      for (const c of cases) {
        const { unmount } = renderWithProviders(
          <Routes>
            <Route path={c.route} element={c.element} />
          </Routes>,
          [c.path],
        )
        const errorEls = await screen.findAllByTestId('query-error-state')
        expect(errorEls.length).toBeGreaterThan(0)
        unmount()
      }
    })
  })

  describe('FE-P1-4: SearchPage 同步外部 URL ?q= 变化', () => {
    it('已在 /search 时通过外部导航更新 ?q=，同步更新输入框和查询且不被旧防抖定时器覆写', async () => {
      const user = userEvent.setup()
      const searchTerms: string[] = []

      vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
        const url = String(input)
        if (url === '/emby/Users/u-1') {
          return jsonRes({ Id: 'u-1', Name: 'Tester', ServerId: 'srv-1', Policy: { IsAdministrator: true } })
        }
        if (url.includes('/Items?')) {
          const parsed = new URL(url, 'http://localhost')
          const term = parsed.searchParams.get('SearchTerm')
          if (term) searchTerms.push(term)
          return jsonRes({ Items: [], TotalRecordCount: 0 })
        }
        return jsonRes({})
      })

      function ExternalCmdKTrigger() {
        const navigate = useNavigate()
        return (
          <button type="button" onClick={() => navigate('/search?q=%E6%98%9F%E9%99%85%E7%A9%BF%E8%B6%8A')}>
            cmdk-jump
          </button>
        )
      }

      renderWithProviders(
        <>
          <ExternalCmdKTrigger />
          <LocationProbe />
          <Routes>
            <Route path="/search" element={<SearchPage />} />
          </Routes>
        </>,
        ['/search?q=%E7%9B%97%E6%A2%A6%E7%A9%BA%E9%97%B4'],
      )

      const input = screen.getByPlaceholderText(/输入片名/) as HTMLInputElement
      expect(input.value).toBe('盗梦空间')

      // 模拟用户在 /search 页通过 ⌘K 跳转到新关键词
      await user.click(screen.getByRole('button', { name: 'cmdk-jump' }))

      await waitFor(() => {
        expect(input.value).toBe('星际穿越')
      })

      // 等待超过 250ms 防抖窗口，确保 URL 与输入框保持「星际穿越」不被覆写
      await new Promise((r) => setTimeout(r, 320))
      expect(input.value).toBe('星际穿越')
      expect(screen.getByTestId('location-probe')).toHaveTextContent(
        '/search?q=%E6%98%9F%E9%99%85%E7%A9%BF%E8%B6%8A',
      )
      expect(searchTerms).toContain('星际穿越')
    })
  })
})
