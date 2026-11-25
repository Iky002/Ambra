import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api, getLiveTvRecordingStreamUrl, saveAuth } from '../../api/client'
import type {
  LiveTvRecordingInfo,
  LiveTvTimerInfo,
  NotificationItem,
} from '../../api/types'
import { Layout } from '../../components/Layout'
import { NotificationBell } from '../../components/NotificationBell'
import { AudioPlayerProvider } from '../../context/AudioPlayerContext'
import { AuthProvider } from '../../context/AuthContext'
import { setDemoModeOverride } from '../../demo/demoMode'
import { resetDemoStore } from '../../demo/mockServer'
import { LiveTvPage } from '../LiveTvPage'
import { UsersTab } from '../admin/UsersTab'
import { WEBHOOK_EVENT_OPTIONS, WebhooksTab } from '../admin/WebhooksTab'

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

describe('Stream 3 (v0.9.3): NotificationBell Center, DVR Playback & LiveTV/Webhooks/UsersTab Fixes', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    resetDemoStore()
    setDemoModeOverride(false)
    saveAuth({
      accessToken: 'tok-v093-admin',
      userId: 'user-admin-01',
      serverId: 'server-v093',
      userName: 'admin',
      isAdmin: true,
      deviceId: 'dev-v093',
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

  it('NotificationBell: 展示未读角标、下拉列表、点击标为已读、全部标为已读与管理员发送全员公告', async () => {
    const store: NotificationItem[] = [
      {
        Id: 'n-1',
        Date: new Date(Date.now() - 120_000).toISOString(),
        IsRead: false,
        Name: '媒体库扫描完成',
        Description: '共新增 12 部影片',
        Level: 'Normal',
      },
      {
        Id: 'n-2',
        Date: new Date(Date.now() - 3600_000).toISOString(),
        IsRead: false,
        Name: '磁盘空间预警',
        Description: '录像目录剩余不足 10%',
        Level: 'Warning',
      },
      {
        Id: 'n-3',
        Date: new Date(Date.now() - 7200_000).toISOString(),
        IsRead: true,
        Name: '插件加载异常',
        Description: '请检查插件配置',
        Level: 'Error',
      },
    ]

    vi.spyOn(api, 'getNotificationsSummary').mockImplementation(async () => ({
      UnreadCount: store.filter((n) => !n.IsRead).length,
      MaxUnreadNotificationLevel: 'Warning',
    }))
    vi.spyOn(api, 'getNotifications').mockImplementation(async () => ({
      Notifications: store.map((n) => ({ ...n })),
      TotalRecordCount: store.length,
    }))
    const markReadSpy = vi
      .spyOn(api, 'markNotificationsRead')
      .mockImplementation(async (_uid, ids) => {
        if (!ids || ids.length === 0) {
          store.forEach((n) => {
            n.IsRead = true
          })
        } else {
          store.forEach((n) => {
            if (ids.includes(n.Id)) n.IsRead = true
          })
        }
      })
    const sendAdminSpy = vi
      .spyOn(api, 'sendAdminNotification')
      .mockImplementation(async (payload) => {
        const created: NotificationItem = {
          Id: `n-${store.length + 1}`,
          Date: new Date().toISOString(),
          IsRead: false,
          Name: payload.Name,
          Description: payload.Description,
          Level: payload.Level || 'Normal',
        }
        store.unshift(created)
        return created
      })

    renderWithProviders(<NotificationBell />)

    // 1. 初始未读角标显示 2
    const badge = await screen.findByTestId('notification-unread-badge')
    expect(badge.textContent).toBe('2')

    // 2. 点击打开下拉面板并展示各级别通知
    fireEvent.click(screen.getByTestId('notification-bell-button'))
    const dropdown = await screen.findByTestId('notification-dropdown')
    await within(dropdown).findByText('媒体库扫描完成')
    expect(within(dropdown).getByText('磁盘空间预警')).toBeTruthy()
    expect(within(dropdown).getByText('插件加载异常')).toBeTruthy()
    expect(within(dropdown).getByTestId('notification-unread-dot-n-1')).toBeTruthy()

    // 3. 点击单条未读通知标为已读 -> 未读角标减为 1
    fireEvent.click(within(dropdown).getByTestId('notification-item-n-1'))
    await waitFor(() => {
      expect(markReadSpy).toHaveBeenCalledWith('user-admin-01', ['n-1'])
      expect(screen.getByTestId('notification-unread-badge').textContent).toBe('1')
    })

    // 4. 点击「全部标为已读」-> 未读角标消失
    fireEvent.click(within(dropdown).getByRole('button', { name: '全部标为已读' }))
    await waitFor(() => {
      expect(markReadSpy).toHaveBeenCalledWith('user-admin-01', undefined)
      expect(screen.queryByTestId('notification-unread-badge')).toBeNull()
    })

    // 5. 管理员展开并提交「发送全员公告」表单 -> 刷新通知列表与角标
    fireEvent.click(within(dropdown).getByRole('button', { name: '发送全员公告' }))
    const form = await within(dropdown).findByTestId('admin-notification-form')
    fireEvent.change(within(form).getByLabelText('公告标题'), {
      target: { value: '系统维护通知' },
    })
    fireEvent.change(within(form).getByLabelText('公告内容'), {
      target: { value: '今晚 23:00 例行重启' },
    })
    fireEvent.change(within(form).getByLabelText('通知级别'), {
      target: { value: 'Warning' },
    })
    fireEvent.click(within(form).getByRole('button', { name: '发送公告' }))

    await waitFor(() => {
      expect(sendAdminSpy).toHaveBeenCalledWith({
        Name: '系统维护通知',
        Description: '今晚 23:00 例行重启',
        Level: 'Warning',
      })
      expect(within(dropdown).getByText('系统维护通知')).toBeTruthy()
      expect(screen.getByTestId('notification-unread-badge').textContent).toBe('1')
    })
  })

  it('NotificationBell: 挂载在 Layout 顶栏且普通用户不展示「发送全员公告」入口', async () => {
    saveAuth({
      accessToken: 'tok-v093-user',
      userId: 'user-normal-01',
      serverId: 'server-v093',
      userName: 'guest',
      isAdmin: false,
      deviceId: 'dev-v093',
    })

    vi.spyOn(api, 'getUser').mockResolvedValue({
      Id: 'user-normal-01',
      Name: 'guest',
      ServerId: 'server-v093',
      Policy: { IsAdministrator: false },
    })
    vi.spyOn(api, 'getUserViews').mockResolvedValue({ Items: [], TotalRecordCount: 0 })
    vi.spyOn(api, 'getLiveTvInfo').mockResolvedValue({ IsEnabled: false })
    vi.spyOn(api, 'getNotificationsSummary').mockResolvedValue({ UnreadCount: 1 })
    vi.spyOn(api, 'getNotifications').mockResolvedValue({
      Notifications: [],
      TotalRecordCount: 0,
    })

    renderWithProviders(
      <Routes>
        <Route path="/" element={<Layout />}>
          <Route index element={<div>首页内容</div>} />
        </Route>
      </Routes>,
      ['/'],
    )

    const bellBtn = await screen.findByTestId('notification-bell-button')
    fireEvent.click(bellBtn)
    const dropdown = await screen.findByTestId('notification-dropdown')
    await within(dropdown).findByText('暂无新通知')
    expect(within(dropdown).queryByRole('button', { name: '发送全员公告' })).toBeNull()
  })

  it('LiveTvPage: 频道为空时仍渲染子标签栏，允许访问预约计划与已录节目并支持播放/下载降级', async () => {
    vi.spyOn(api, 'getLiveTvChannels').mockResolvedValue({ Items: [], TotalRecordCount: 0 })
    vi.spyOn(api, 'getLiveTvTimers').mockResolvedValue({
      Items: [
        {
          Id: 'timer-empty-1',
          ChannelId: 'ch-old',
          ChannelName: '旧频道',
          Name: '历史预约',
          StartDate: '2026-11-26T10:00:00Z',
          EndDate: '2026-11-26T11:00:00Z',
          Status: 'New',
        },
      ],
      TotalRecordCount: 1,
    })
    const recordings: LiveTvRecordingInfo[] = [
      {
        Id: 'rec-done-1',
        ChannelId: 'ch-old',
        ChannelName: 'CCTV-6',
        Name: '已完成录像.mkv',
        StartDate: '2026-11-24T12:00:00Z',
        EndDate: '2026-11-24T13:00:00Z',
        Status: 'Completed',
        Path: '/recordings/done.mkv',
      },
      {
        Id: 'rec-inprog-2',
        ChannelId: 'ch-old',
        ChannelName: 'CCTV-1',
        Name: '正在录制中',
        StartDate: '2026-11-25T10:00:00Z',
        EndDate: '2026-11-25T11:00:00Z',
        Status: 'InProgress',
      },
    ]
    vi.spyOn(api, 'getLiveTvRecordings').mockResolvedValue({
      Items: recordings,
      TotalRecordCount: recordings.length,
    })

    renderWithProviders(
      <Routes>
        <Route path="/livetv" element={<LiveTvPage />} />
      </Routes>,
      ['/livetv'],
    )

    // 1. 即使无频道，仍展示空提示且顶部子标签栏可见
    await screen.findByText('暂无可观看的电视频道')
    expect(screen.getByRole('tab', { name: '直播频道' })).toBeTruthy()
    expect(screen.getByRole('tab', { name: '预约计划' })).toBeTruthy()
    expect(screen.getByRole('tab', { name: '已录节目' })).toBeTruthy()

    // 2. 切到「预约计划」能看到历史预约
    fireEvent.click(screen.getByRole('tab', { name: '预约计划' }))
    await screen.findByText('历史预约')

    // 3. 切到「已录节目」：已完成录像有「播放」按钮，InProgress 没有
    fireEvent.click(screen.getByRole('tab', { name: '已录节目' }))
    const recPanel = await screen.findByTestId('livetv-recordings-panel')
    await within(recPanel).findByText('已完成录像.mkv')
    const playButtons = within(recPanel).getAllByRole('button', { name: '播放' })
    expect(playButtons.length).toBe(1)

    // 4. 点击「播放」打开内联播放器，验证 src
    fireEvent.click(playButtons[0])
    const player = await screen.findByTestId('dvr-recording-player')
    const video = within(player).getByTestId('dvr-recording-video') as HTMLVideoElement
    expect(video.getAttribute('src')).toBe(getLiveTvRecordingStreamUrl('rec-done-1'))

    // 5. 触发 MEDIA_ERR_SRC_NOT_SUPPORTED (code = 4) -> 展示直接下载链接
    Object.defineProperty(video, 'error', {
      configurable: true,
      value: { code: 4 },
    })
    fireEvent.error(video)

    const downloadLink = await within(player).findByRole('link', { name: '直接下载录制文件' })
    expect(downloadLink.getAttribute('href')).toBe(getLiveTvRecordingStreamUrl('rec-done-1'))
    expect(downloadLink.hasAttribute('download')).toBe(true)

    // 6. 点击关闭按钮关闭播放器
    fireEvent.click(within(player).getByRole('button', { name: '关闭录像播放器' }))
    expect(screen.queryByTestId('dvr-recording-player')).toBeNull()
  })

  it('LiveTvPage: EPG「已预约」状态随 timers 初始同步，并在取消预约后立即恢复可预约', async () => {
    const now = Date.now()
    const timersStore: LiveTvTimerInfo[] = [
      {
        Id: 'timer-sync-1',
        ChannelId: 'ch-sync-1',
        ChannelName: '同步测试台',
        ProgramId: 'prog-sync-1',
        Name: '同步节目',
        StartDate: new Date(now - 600_000).toISOString(),
        EndDate: new Date(now + 1800_000).toISOString(),
        Status: 'New',
      },
    ]

    vi.spyOn(api, 'getLiveTvChannels').mockResolvedValue({
      Items: [{ Id: 'ch-sync-1', Name: '同步测试台', Number: '10' }],
      TotalRecordCount: 1,
    })
    vi.spyOn(api, 'getLiveTvPrograms').mockResolvedValue({
      Items: [
        {
          Id: 'prog-sync-1',
          ChannelId: 'ch-sync-1',
          Name: '同步节目',
          StartDate: new Date(now - 600_000).toISOString(),
          EndDate: new Date(now + 1800_000).toISOString(),
        },
      ],
      TotalRecordCount: 1,
    })
    vi.spyOn(api, 'getLiveTvTimers').mockImplementation(async () => ({
      Items: [...timersStore],
      TotalRecordCount: timersStore.length,
    }))
    vi.spyOn(api, 'deleteLiveTvTimer').mockImplementation(async (id) => {
      const idx = timersStore.findIndex((t) => t.Id === id)
      if (idx >= 0) timersStore.splice(idx, 1)
    })

    renderWithProviders(
      <Routes>
        <Route path="/livetv" element={<LiveTvPage />} />
      </Routes>,
      ['/livetv'],
    )

    // 初始加载即显示「已预约」且禁用
    const epgPanel = await screen.findByTestId('livetv-epg-panel')
    const scheduledBtn = await within(epgPanel).findByRole('button', { name: '已预约' })
    expect((scheduledBtn as HTMLButtonElement).disabled).toBe(true)

    // 切换到「预约计划」取消该预约
    fireEvent.click(screen.getByRole('tab', { name: '预约计划' }))
    const timersPanel = await screen.findByTestId('livetv-timers-panel')
    fireEvent.click(await within(timersPanel).findByRole('button', { name: '取消预约' }))
    await waitFor(() => {
      expect(timersStore.length).toBe(0)
    })

    // 切回「直播频道」：按钮立即恢复为可点击的「预约录制」
    fireEvent.click(screen.getByRole('tab', { name: '直播频道' }))
    const epgPanelAfter = await screen.findByTestId('livetv-epg-panel')
    const reenabledBtn = await within(epgPanelAfter).findByRole('button', { name: '预约录制' })
    expect((reenabledBtn as HTMLButtonElement).disabled).toBe(false)
  })

  it('WebhooksTab: 同步真实 EventBus 事件列表并在新增/删除弹窗内联展示错误', async () => {
    const eventIds = WEBHOOK_EVENT_OPTIONS.map((e) => e.id)
    expect(eventIds).toEqual([
      '*',
      'LibraryChanged',
      'UserDataChanged',
      'PlaybackStart',
      'PlaybackProgress',
      'PlaybackStop',
      'SessionUpdated',
      'TaskProgress',
    ])
    expect(eventIds).not.toContain('SessionStart')
    expect(eventIds).not.toContain('SessionEnd')

    vi.spyOn(api, 'getWebhooks').mockResolvedValue([
      {
        Id: 'wh-1',
        Name: '现有推送',
        Url: 'https://example.com/hook',
        Events: ['PlaybackProgress', 'SessionUpdated'],
        Enabled: true,
      },
    ])
    vi.spyOn(api, 'saveWebhook').mockRejectedValue(new Error('目标地址拒绝连接'))
    vi.spyOn(api, 'deleteWebhook').mockRejectedValue(new Error('删除接口返回 500'))

    renderWithProviders(<WebhooksTab />, ['/admin/webhooks'])
    await screen.findByText('现有推送')

    // 1. 新增弹窗：取消全部事件提交 -> 弹窗内联显示校验错误
    fireEvent.click(screen.getByRole('button', { name: '新增 Webhook' }))
    fireEvent.change(screen.getByLabelText('名称 *'), { target: { value: '测试 Hook' } })
    fireEvent.change(screen.getByLabelText('推送 URL *'), {
      target: { value: 'https://example.com/test' },
    })
    fireEvent.click(screen.getByLabelText(/全部事件/))
    fireEvent.click(screen.getByRole('button', { name: '创建 Webhook' }))

    expect((await screen.findByRole('alert')).textContent).toContain('请至少选择一个订阅事件')

    // 勾选 PlaybackProgress 后提交 -> 弹窗内联显示 API 错误
    fireEvent.click(screen.getByLabelText(/PlaybackProgress/))
    fireEvent.click(screen.getByRole('button', { name: '创建 Webhook' }))
    await waitFor(() => {
      expect(screen.getByRole('alert').textContent).toContain('目标地址拒绝连接')
    })

    // 关闭新增弹窗，打开删除弹窗 -> 触发删除错误，错误内联展示在删除弹窗内
    fireEvent.click(screen.getByRole('button', { name: '取消' }))
    fireEvent.click(screen.getByRole('button', { name: '删除' }))
    await screen.findByText('确认删除 Webhook')
    fireEvent.click(screen.getByRole('button', { name: '确认删除' }))

    await waitFor(() => {
      expect(screen.getByRole('alert').textContent).toContain('删除接口返回 500')
    })
  })

  it('UsersTab: 按数值分组去重家长控制分级选项，避免重复 value 导致回弹', async () => {
    vi.spyOn(api, 'getUsers').mockResolvedValue([
      {
        Id: 'user-admin-01',
        Name: 'admin',
        ServerId: 'server-v093',
        Policy: { IsAdministrator: true },
      },
    ])
    vi.spyOn(api, 'getVirtualFolders').mockResolvedValue([])
    vi.spyOn(api, 'getParentalRatings').mockResolvedValue([
      { Name: 'G', Value: 1 },
      { Name: 'TV-Y', Value: 1 },
      { Name: 'TV-G', Value: 1 },
      { Name: 'PG', Value: 5 },
      { Name: 'TV-PG', Value: 5 },
      { Name: 'R', Value: 9 },
      { Name: 'TV-MA', Value: 9 },
    ])

    renderWithProviders(<UsersTab />, ['/admin/users'])
    await screen.findByText('admin')
    fireEvent.click(screen.getByRole('button', { name: '权限策略' }))

    const select = (await screen.findByLabelText(
      '最高允许内容分级 (MaxParentalRating)',
    )) as HTMLSelectElement

    await waitFor(() => {
      expect(within(select).getByRole('option', { name: 'G / TV-Y / TV-G (≤ 1)' })).toBeTruthy()
    })

    const options = Array.from(select.options)
    const values = options.map((o) => o.value)
    // 所有 value 必须唯一（含 '' 不限制 + 1, 5, 9）
    expect(values).toEqual(['', '1', '5', '9'])
    expect(new Set(values).size).toBe(values.length)
    expect(within(select).getByRole('option', { name: 'PG / TV-PG (≤ 5)' })).toBeTruthy()
    expect(within(select).getByRole('option', { name: 'R / TV-MA (≤ 9)' })).toBeTruthy()

    // 选择 5 不回弹
    fireEvent.change(select, { target: { value: '5' } })
    expect(select.value).toBe('5')
  })
})
