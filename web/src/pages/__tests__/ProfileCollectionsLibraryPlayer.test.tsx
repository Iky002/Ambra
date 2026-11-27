import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { saveAuth } from '../../api/client'
import { AuthProvider, useAuth } from '../../context/AuthContext'
import { loadPlaybackPrefs } from '../../player/playbackPrefs'
import { CollectionsPage } from '../CollectionsPage'
import { LibraryPage } from '../LibraryPage'
import { PlayerPage } from '../PlayerPage'
import { ProfilePage } from '../ProfilePage'

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

function LogoutTrigger() {
  const { logout } = useAuth()
  return (
    <button type="button" onClick={logout}>
      触发退出
    </button>
  )
}

function renderWithProviders(ui: React.ReactNode, initialEntries: string[]) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={qc}>
      <AuthProvider>
        <MemoryRouter initialEntries={initialEntries}>{ui}</MemoryRouter>
      </AuthProvider>
    </QueryClientProvider>,
  )
}

describe('Batch 3B Web UI 功能测试 (P1-3 / P1-4 / P1-6 / P2-3)', () => {
  beforeEach(() => {
    localStorage.clear()
    saveAuth({
      accessToken: 'tok-batch3b',
      userId: 'u-1',
      serverId: 'srv-1',
      userName: 'Alice',
      isAdmin: false,
      deviceId: 'dev-batch3b',
    })
    vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined)
    vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => undefined)
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('ProfilePage：修改密码、持久化本地播放偏好，以及 logout 调用 /emby/Sessions/Logout', async () => {
    const user = userEvent.setup()
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      const method = (init?.method || 'GET').toUpperCase()
      if (url === '/emby/Users/u-1' && method === 'GET') {
        return jsonRes({
          Id: 'u-1',
          Name: 'Alice',
          ServerId: 'srv-1',
          Policy: { IsAdministrator: false },
        })
      }
      if (url === '/emby/Users/u-1/Password' && method === 'POST') {
        return emptyRes(204)
      }
      if (url === '/emby/Sessions/Logout' && method === 'POST') {
        return emptyRes(204)
      }
      return jsonRes({})
    })

    renderWithProviders(
      <Routes>
        <Route
          path="/profile"
          element={
            <>
              <ProfilePage />
              <LogoutTrigger />
            </>
          }
        />
      </Routes>,
      ['/profile'],
    )

    expect(await screen.findByText('个人中心')).toBeInTheDocument()
    expect(screen.getByText('Alice')).toBeInTheDocument()

    // 切换本地自动连播偏好并验证 localStorage 持久化
    const autoNextSwitch = screen.getByRole('switch', { name: '剧集自动连播下一集' })
    expect(autoNextSwitch).toHaveAttribute('aria-checked', 'true')
    await user.click(autoNextSwitch)
    expect(autoNextSwitch).toHaveAttribute('aria-checked', 'false')
    expect(loadPlaybackPrefs().autoPlayNextEpisode).toBe(false)

    // 修改密码表单校验与提交
    await user.type(screen.getByLabelText('当前密码'), 'old123')
    await user.type(screen.getByLabelText('新密码'), 'new456')
    await user.type(screen.getByLabelText('确认新密码'), 'mismatch')
    await user.click(screen.getByRole('button', { name: '保存新密码' }))
    expect(await screen.findByText('两次输入的新密码不一致')).toBeInTheDocument()

    await user.clear(screen.getByLabelText('确认新密码'))
    await user.type(screen.getByLabelText('确认新密码'), 'new456')
    await user.click(screen.getByRole('button', { name: '保存新密码' }))

    await waitFor(() => {
      expect(
        fetchSpy.mock.calls.some(
          ([u, init]) =>
            String(u) === '/emby/Users/u-1/Password' &&
            (init as RequestInit)?.method === 'POST' &&
            String((init as RequestInit)?.body).includes('"NewPw":"new456"'),
        ),
      ).toBe(true)
    })
    expect(await screen.findByText(/登录密码已修改成功/)).toBeInTheDocument()

    // 验证真实退出登录调用 /emby/Sessions/Logout
    await user.click(screen.getByRole('button', { name: '触发退出' }))
    await waitFor(() => {
      expect(
        fetchSpy.mock.calls.some(
          ([u, init]) =>
            String(u) === '/emby/Sessions/Logout' && (init as RequestInit)?.method === 'POST',
        ),
      ).toBe(true)
    })
  })

  it('CollectionsPage：非管理员隐藏新建合集，管理员打开新建合集弹窗提交 POST /emby/Collections', async () => {
    const user = userEvent.setup()
    const createdCollections: Array<{ Id: string; Name: string; Type: string }> = []

    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      const method = (init?.method || 'GET').toUpperCase()
      if (url === '/emby/Users/u-1') {
        return jsonRes({ Id: 'u-1', Name: 'Alice', Policy: { IsAdministrator: false } })
      }
      if (url === '/emby/Users/u-admin') {
        return jsonRes({ Id: 'u-admin', Name: 'Admin', Policy: { IsAdministrator: true } })
      }
      if (
        (url.startsWith('/emby/Users/u-1/Items') || url.startsWith('/emby/Users/u-admin/Items')) &&
        url.includes('IncludeItemTypes=BoxSet')
      ) {
        return jsonRes({ Items: createdCollections, TotalRecordCount: createdCollections.length })
      }
      if (
        (url.startsWith('/emby/Users/u-1/Items') || url.startsWith('/emby/Users/u-admin/Items')) &&
        url.includes('IncludeItemTypes=Playlist')
      ) {
        return jsonRes({ Items: [], TotalRecordCount: 0 })
      }
      if (url.startsWith('/emby/Collections') && method === 'POST') {
        const body = JSON.parse(String(init?.body || '{}'))
        const created = { Id: 'col-99', Name: body.Name, Type: 'BoxSet' }
        createdCollections.push(created)
        return jsonRes({ Id: created.Id, Name: created.Name })
      }
      return jsonRes({})
    })

    const { unmount } = renderWithProviders(
      <Routes>
        <Route path="/collections" element={<CollectionsPage />} />
        <Route path="/collections/:id" element={<CollectionsPage />} />
      </Routes>,
      ['/collections'],
    )

    expect(await screen.findByText('合集与播放列表')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '新建合集' })).not.toBeInTheDocument()
    unmount()

    saveAuth({
      accessToken: 'tok-batch3b-admin',
      userId: 'u-admin',
      serverId: 'srv-1',
      userName: 'Admin',
      isAdmin: true,
      deviceId: 'dev-batch3b-admin',
    })

    renderWithProviders(
      <Routes>
        <Route path="/collections" element={<CollectionsPage />} />
        <Route path="/collections/:id" element={<CollectionsPage />} />
      </Routes>,
      ['/collections'],
    )

    await user.click(await screen.findByRole('button', { name: '新建合集' }))
    const nameInput = await screen.findByPlaceholderText('例如：诺兰科幻三部曲')
    await user.type(nameInput, '科幻经典合集')
    await user.click(screen.getByRole('button', { name: '创建' }))

    await waitFor(() => {
      expect(
        fetchSpy.mock.calls.some(
          ([u, init]) =>
            String(u).startsWith('/emby/Collections') &&
            (init as RequestInit)?.method === 'POST' &&
            String((init as RequestInit)?.body).includes('科幻经典合集'),
        ),
      ).toBe(true)
    })
    expect(await screen.findByText('已创建合集「科幻经典合集」')).toBeInTheDocument()
  })

  it('LibraryPage：从 URL searchParams 读取 genre/year/tag 并传递到 getItems 请求', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/emby/Users/u-1') {
        return jsonRes({ Id: 'u-1', Name: 'Alice', Policy: { IsAdministrator: false } })
      }
      if (url === '/emby/Users/u-1/Items/lib-movies') {
        return jsonRes({ Id: 'lib-movies', Name: '电影库', Type: 'CollectionFolder' })
      }
      if (url.startsWith('/emby/Genres')) {
        return jsonRes({ Items: [{ Id: 'g1', Name: '科幻' }], TotalRecordCount: 1 })
      }
      if (url.startsWith('/emby/Years')) {
        return jsonRes({ Items: [{ Id: 'y1', Name: '2024' }], TotalRecordCount: 1 })
      }
      if (url.startsWith('/emby/Tags')) {
        return jsonRes({ Items: [{ Id: 't1', Name: '豆瓣Top250' }], TotalRecordCount: 1 })
      }
      if (url.startsWith('/emby/Users/u-1/Items?')) {
        return jsonRes({
          Items: [{ Id: 'm1', Name: '沙丘 2', Type: 'Movie', ProductionYear: 2024 }],
          TotalRecordCount: 1,
        })
      }
      return jsonRes({})
    })

    renderWithProviders(
      <Routes>
        <Route path="/library/:id" element={<LibraryPage />} />
      </Routes>,
      ['/library/lib-movies?genre=%E7%A7%91%E5%B9%BB&year=2024&tag=%E8%B1%86%E7%93%A3Top250'],
    )

    expect(await screen.findByText('沙丘 2')).toBeInTheDocument()

    const itemReq = fetchSpy.mock.calls.find(([u]) =>
      String(u).startsWith('/emby/Users/u-1/Items?'),
    )
    expect(itemReq).toBeDefined()
    const reqUrl = new URL(String(itemReq?.[0]), 'http://localhost')
    expect(reqUrl.searchParams.get('Genres')).toBe('科幻')
    expect(reqUrl.searchParams.get('Years')).toBe('2024')
    expect(reqUrl.searchParams.get('Tags')).toBe('豆瓣Top250')
  })

  it('PlayerPage：M 键静音切换、ArrowDown 归零同步静音、剧集播完展示 8 秒下一集倒计时浮层', async () => {
    const user = userEvent.setup()

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/emby/Users/u-1') {
        return jsonRes({ Id: 'u-1', Name: 'Alice', Policy: { IsAdministrator: false } })
      }
      if (url === '/emby/Users/u-1/Items/ep-1') {
        return jsonRes({
          Id: 'ep-1',
          Name: '第一集：起点',
          Type: 'Episode',
          SeriesId: 'series-1',
          SeriesName: '三体',
          SeasonId: 'season-1',
          ParentIndexNumber: 1,
          IndexNumber: 1,
          RunTimeTicks: 2_400 * 10_000_000,
        })
      }
      if (url.startsWith('/emby/Shows/series-1/Episodes')) {
        return jsonRes({
          Items: [
            {
              Id: 'ep-1',
              Name: '第一集：起点',
              Type: 'Episode',
              ParentIndexNumber: 1,
              IndexNumber: 1,
            },
            {
              Id: 'ep-2',
              Name: '第二集：射手与农场主',
              Type: 'Episode',
              ParentIndexNumber: 1,
              IndexNumber: 2,
            },
          ],
          TotalRecordCount: 2,
        })
      }
      if (url.startsWith('/emby/Items/ep-1/PlaybackInfo')) {
        return jsonRes({
          PlaySessionId: 'sess-ep-1',
          MediaSources: [
            {
              Id: 'src-ep-1',
              Container: 'mp4',
              SupportsDirectPlay: true,
              DirectStreamUrl: '/emby/Videos/ep-1/stream?Static=true&MediaSourceId=src-ep-1',
              MediaStreams: [{ Index: 0, Type: 'Video', Codec: 'h264', Width: 1920, Height: 1080 }],
            },
          ],
        })
      }
      if (url.startsWith('/emby/Sessions/Playing')) {
        return emptyRes(204)
      }
      return jsonRes({})
    })

    const { container } = renderWithProviders(
      <Routes>
        <Route
          path="/play/:id"
          element={
            <>
              <PlayerPage />
              <LocationProbe />
            </>
          }
        />
      </Routes>,
      ['/play/ep-1'],
    )

    // 等待剧集分集列表加载并展示下一集按钮
    expect(await screen.findByLabelText('下一集')).toBeInTheDocument()

    const video = container.querySelector('video') as HTMLVideoElement
    expect(video).toBeTruthy()
    video.volume = 0.1
    video.muted = false

    // M 键切换静音
    fireEvent.keyDown(window, { key: 'm' })
    expect(video.muted).toBe(true)

    // 再按 M 取消静音
    fireEvent.keyDown(window, { key: 'm' })
    expect(video.muted).toBe(false)

    // ArrowDown 音量降至 0 时自动同步 muted = true
    fireEvent.keyDown(window, { key: 'ArrowDown' })
    expect(video.volume).toBe(0)
    expect(video.muted).toBe(true)

    // 触发视频播放结束 -> 显示 8 秒自动连播下一集浮层
    fireEvent.ended(video)
    expect(await screen.findByText('即将播放下一集')).toBeInTheDocument()
    expect(screen.getByText(/S01E02 第二集：射手与农场主/)).toBeInTheDocument()
    expect(screen.getByText('8 秒后自动播放')).toBeInTheDocument()

    // 点击「立即播放」跳转到 /play/ep-2
    await user.click(screen.getByRole('button', { name: '立即播放' }))
    await waitFor(() => {
      expect(screen.getByTestId('location-probe')).toHaveTextContent('/play/ep-2')
    })
  })
})
