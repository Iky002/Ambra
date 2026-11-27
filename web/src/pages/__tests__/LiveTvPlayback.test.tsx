import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
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
