import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api, saveAuth } from '../../api/client'
import type { BaseItemDto } from '../../api/types'
import { MediaCard } from '../../components/MediaCard'
import { AudioPlayerProvider } from '../../context/AudioPlayerContext'
import { AuthProvider } from '../../context/AuthContext'
import { HomePage } from '../HomePage'
import { ItemDetailPage } from '../ItemDetailPage'
import { LibraryPage } from '../LibraryPage'
import { PlayerPage } from '../PlayerPage'

function createWrapper(initialEntry = '/') {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return (
      <QueryClientProvider client={qc}>
        <AuthProvider>
          <AudioPlayerProvider>
            <MemoryRouter initialEntries={[initialEntry]}>{children}</MemoryRouter>
          </AudioPlayerProvider>
        </AuthProvider>
      </QueryClientProvider>
    )
  }
}

describe('v0.9.5 Web UI Bug Fixes & Emby Parity', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    saveAuth({
      accessToken: 'tok-v095',
      userId: 'u-1',
      serverId: 'srv-1',
      userName: 'Tester',
      isAdmin: true,
      deviceId: 'dev-v095',
    })
    vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined)
    vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => undefined)
    vi.spyOn(HTMLMediaElement.prototype, 'load').mockImplementation(() => undefined)
    vi.spyOn(api, 'getUser').mockResolvedValue({
      Id: 'u-1',
      Name: 'Tester',
      ServerId: 'srv-1',
      Policy: { IsAdministrator: true, EnableContentDownloading: true },
    })
  })

  afterEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    vi.restoreAllMocks()
  })

  it('LibraryPage：初始挂载保留 URL 中的 ?letter=A 与 ?studio=... 筛选', async () => {
    vi.spyOn(api, 'getItem').mockResolvedValue({
      Id: 'lib-movies',
      Name: '电影库',
      Type: 'CollectionFolder',
      CollectionType: 'movies',
    })
    vi.spyOn(api, 'getGenres').mockResolvedValue({ Items: [], TotalRecordCount: 0 })
    vi.spyOn(api, 'getYears').mockResolvedValue({ Items: [], TotalRecordCount: 0 })
    vi.spyOn(api, 'getTags').mockResolvedValue({ Items: [], TotalRecordCount: 0 })

    const getItemsSpy = vi.spyOn(api, 'getItems').mockResolvedValue({
      Items: [
        {
          Id: 'm-1',
          Name: 'Arrival',
          Type: 'Movie',
          Studios: [{ Id: 'st-1', Name: 'Paramount Pictures' }],
        },
        {
          Id: 'm-2',
          Name: 'Alien',
          Type: 'Movie',
          Studios: [{ Id: 'st-2', Name: '20th Century Fox' }],
        },
      ],
      TotalRecordCount: 2,
    })

    const Wrapper = createWrapper('/library/lib-movies?letter=A&studio=Paramount%20Pictures')
    render(
      <Wrapper>
        <Routes>
          <Route path="/library/:id" element={<LibraryPage />} />
        </Routes>
      </Wrapper>,
    )

    expect(await screen.findByText('Arrival')).toBeInTheDocument()
    expect(screen.queryByText('Alien')).not.toBeInTheDocument()
    expect(screen.getByText('制片厂：Paramount Pictures')).toBeInTheDocument()

    // 确认 A 字母按钮处于选中态且 getItems 收到了 nameStartsWith: 'a'
    const letterABtn = screen.getByRole('button', { name: 'A' })
    expect(letterABtn).toHaveAttribute('aria-pressed', 'true')
    await waitFor(() => {
      expect(getItemsSpy).toHaveBeenCalledWith(
        'u-1',
        expect.objectContaining({
          parentId: 'lib-movies',
          nameStartsWith: 'a',
        }),
      )
    })
  })

  it('MediaCard：为 TMDB热门 / 豆瓣热门电影 / 豆瓣热门剧集 显示「热门」徽章', () => {
    const Wrapper = createWrapper('/')
    const items: BaseItemDto[] = [
      { Id: 't-1', Name: '电影A', Type: 'Movie', Tags: ['TMDB热门'] },
      { Id: 't-2', Name: '电影B', Type: 'Movie', Tags: ['豆瓣热门电影'] },
      { Id: 't-3', Name: '剧集C', Type: 'Series', Tags: ['豆瓣热门剧集'] },
    ]

    render(
      <Wrapper>
        <div>
          {items.map((it) => (
            <MediaCard key={it.Id} item={it} />
          ))}
        </div>
      </Wrapper>,
    )

    expect(screen.getAllByText('热门')).toHaveLength(3)
  })

  it('HomePage：在继续观看下方渲染「接下来播放」书架', async () => {
    vi.spyOn(api, 'getUserViews').mockResolvedValue({
      Items: [{ Id: 'lib-tv', Name: '剧集', Type: 'CollectionFolder', CollectionType: 'tvshows' }],
      TotalRecordCount: 1,
    })
    vi.spyOn(api, 'getResumeItems').mockResolvedValue({ Items: [], TotalRecordCount: 0 })
    vi.spyOn(api, 'getItems').mockResolvedValue({ Items: [], TotalRecordCount: 0 })
    vi.spyOn(api, 'getLatestItems').mockResolvedValue([])
    const nextUpSpy = vi.spyOn(api, 'getNextUp').mockResolvedValue({
      Items: [
        {
          Id: 'ep-next-1',
          Name: '第 2 集：暗流涌动',
          Type: 'Episode',
          MediaType: 'Video',
          SeriesName: '三体',
          SeriesId: 'ser-1',
          ParentIndexNumber: 1,
          IndexNumber: 2,
        },
      ],
      TotalRecordCount: 1,
    })

    const Wrapper = createWrapper('/')
    render(
      <Wrapper>
        <HomePage />
      </Wrapper>,
    )

    expect(await screen.findByRole('heading', { name: '接下来播放' })).toBeInTheDocument()
    expect(screen.getByText('第 2 集：暗流涌动')).toBeInTheDocument()
    expect(nextUpSpy).toHaveBeenCalledWith('u-1', { limit: 12 })
  })

  it('ItemDetailPage：展示制片厂芯片、TMDB/IMDb/豆瓣外链、过滤热门标签以及分集「详情」入口', async () => {
    vi.spyOn(api, 'getItem').mockResolvedValue({
      Id: 'series-1',
      ParentId: 'lib-tv',
      Name: '幕府将军',
      Type: 'Series',
      Tags: ['TMDB热门', '豆瓣热门剧集', '历史史诗'],
      Studios: [{ Id: 'st-fx', Name: 'FX Productions' }],
      ProviderIds: {
        Tmdb: '126308',
        Imdb: 'tt2788316',
        Douban: '30299502',
      },
    })
    vi.spyOn(api, 'getSeasons').mockResolvedValue({
      Items: [{ Id: 'season-1', Name: '第 1 季', Type: 'Season' }],
      TotalRecordCount: 1,
    })
    vi.spyOn(api, 'getEpisodes').mockResolvedValue({
      Items: [
        {
          Id: 'ep-101',
          Name: '按针',
          Type: 'Episode',
          IndexNumber: 1,
          ParentIndexNumber: 1,
          UserData: { Played: false, IsFavorite: false, PlaybackPositionTicks: 0, PlayCount: 0 },
        },
      ],
      TotalRecordCount: 1,
    })
    vi.spyOn(api, 'getNextUp').mockResolvedValue({ Items: [], TotalRecordCount: 0 })
    vi.spyOn(api, 'getSimilarItems').mockResolvedValue({ Items: [], TotalRecordCount: 0 })
    vi.spyOn(api, 'getLocalTrailers').mockResolvedValue([])
    vi.spyOn(api, 'getSpecialFeatures').mockResolvedValue([])
    vi.spyOn(api, 'getThemeSongs').mockResolvedValue({ Items: [], TotalRecordCount: 0 })

    const Wrapper = createWrapper('/item/series-1')
    render(
      <Wrapper>
        <Routes>
          <Route path="/item/:id" element={<ItemDetailPage />} />
        </Routes>
      </Wrapper>,
    )

    // 制片厂芯片
    const studioLink = await screen.findByRole('link', { name: 'FX Productions' })
    expect(studioLink).toHaveAttribute(
      'href',
      '/library/lib-tv?studio=FX%20Productions',
    )

    // 外部资料链接
    const tmdbLink = screen.getByRole('link', { name: 'TMDB' })
    expect(tmdbLink).toHaveAttribute('href', 'https://www.themoviedb.org/tv/126308')
    expect(tmdbLink).toHaveAttribute('target', '_blank')
    expect(tmdbLink).toHaveAttribute('rel', 'noopener noreferrer')

    const imdbLink = screen.getByRole('link', { name: 'IMDb' })
    expect(imdbLink).toHaveAttribute('href', 'https://www.imdb.com/title/tt2788316/')

    const doubanLink = screen.getByRole('link', { name: '豆瓣' })
    expect(doubanLink).toHaveAttribute('href', 'https://movie.douban.com/subject/30299502/')

    // 热门趋势徽章存在，且不重复渲染为 #TMDB热门 / #豆瓣热门剧集
    expect(screen.getByText('热门趋势')).toBeInTheDocument()
    expect(screen.getByText('#历史史诗')).toBeInTheDocument()
    expect(screen.queryByText('#TMDB热门')).not.toBeInTheDocument()
    expect(screen.queryByText('#豆瓣热门剧集')).not.toBeInTheDocument()

    // 分集详情入口链接
    const epDetailLink = await screen.findByRole('link', { name: '查看 按针 详情' })
    expect(epDetailLink).toHaveAttribute('href', '/item/ep-101')
  })

  it('PlayerPage：支持画面填充模式切换（object-contain/object-cover）与多版本切换器', async () => {
    const user = userEvent.setup()

    vi.spyOn(api, 'getItem').mockResolvedValue({
      Id: 'movie-multi-1',
      Name: '沙丘 2',
      Type: 'Movie',
      MediaType: 'Video',
    })
    vi.spyOn(api, 'getPlaybackInfo').mockResolvedValue({
      PlaySessionId: 'sess-multi-1',
      MediaSources: [
        {
          Id: 'src-4k',
          Name: '4K REMUX',
          Container: 'mp4',
          SupportsDirectPlay: true,
          DefaultAudioStreamIndex: 1,
          MediaStreams: [
            { Index: 0, Type: 'Video', Codec: 'h264', Width: 3840, Height: 2160 },
            { Index: 1, Type: 'Audio', Codec: 'aac', Channels: 2, Language: 'eng' },
          ],
        },
        {
          Id: 'src-1080p',
          Name: '1080p WEB-DL',
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
    vi.spyOn(api, 'reportPlaying').mockResolvedValue(undefined)
    vi.spyOn(api, 'reportProgress').mockResolvedValue(undefined)
    vi.spyOn(api, 'reportStopped').mockResolvedValue(undefined)

    const Wrapper = createWrapper('/play/movie-multi-1')
    const { container } = render(
      <Wrapper>
        <Routes>
          <Route path="/play/:id" element={<PlayerPage />} />
        </Routes>
      </Wrapper>,
    )

    const video = container.querySelector('video') as HTMLVideoElement
    await waitFor(() => {
      expect(video.src).toContain('MediaSourceId=src-4k')
    })

    // 默认 object-contain，点击切换画面比例后变为 object-cover
    expect(video.className).toContain('object-contain')
    const aspectBtn = screen.getByRole('button', { name: '切换画面比例' })
    await user.click(aspectBtn)
    expect(video.className).toContain('object-cover')

    // 多版本切换器：打开版本菜单并切换到 1080p WEB-DL
    const versionBtn = await screen.findByRole('button', { name: '切换视频版本' })
    await user.click(versionBtn)
    const webDlOption = await screen.findByRole('button', { name: /1080p WEB-DL/ })
    await user.click(webDlOption)

    await waitFor(() => {
      expect(video.src).toContain('MediaSourceId=src-1080p')
    })
  })
})
