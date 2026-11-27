import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api, saveAuth } from '../../api/client'
import { MediaCard } from '../../components/MediaCard'
import { AudioPlayerProvider } from '../../context/AudioPlayerContext'
import { AuthProvider } from '../../context/AuthContext'
import { setDemoModeOverride } from '../../demo/demoMode'
import { resetDemoStore } from '../../demo/mockServer'
import { LibraryPage } from '../LibraryPage'
import { DashboardTab } from '../admin/DashboardTab'
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

describe('Stream 4 (v0.9.0): Multi-Select Batch Actions, Backup & Restore UI, Regression Fixes & Demo Mocks', () => {
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
      deviceId: 'dev-test-v090',
    })
    vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => undefined)
    vi.spyOn(HTMLMediaElement.prototype, 'load').mockImplementation(() => undefined)
  })

  afterEach(() => {
    setDemoModeOverride(null)
    localStorage.clear()
    sessionStorage.clear()
    vi.restoreAllMocks()
  })

  it('MediaCard 继续观看快捷按钮在移动端可见且桌面端悬停前禁点', () => {
    const { container } = renderWithProviders(
      <MediaCard
        item={{
          Id: 'item-test-1',
          Name: '测试电影',
          Type: 'Movie',
          MediaType: 'Video',
        }}
        onMarkPlayed={vi.fn()}
        onHideFromResume={vi.fn()}
      />,
    )

    const quickBtn = screen.getByRole('button', { name: '标记已看' })
    const wrapper = quickBtn.parentElement
    expect(wrapper).toBeTruthy()
    expect(wrapper?.className).toContain('opacity-100')
    expect(wrapper?.className).toContain('[@media(hover:hover)]:opacity-0')
    expect(wrapper?.className).toContain('[@media(hover:hover)]:pointer-events-none')
    expect(wrapper?.className).toContain('[@media(hover:hover)]:group-hover:opacity-100')
    expect(wrapper?.className).toContain('[@media(hover:hover)]:group-hover:pointer-events-auto')
    expect(wrapper?.className).toContain('[@media(hover:hover)]:focus-within:opacity-100')
    expect(wrapper?.className).toContain('[@media(hover:hover)]:focus-within:pointer-events-auto')
    expect(container).toBeTruthy()
  })

  it('LibraryPage 支持多选批量标记已看/未看、加入合集/新建片单，以及字母筛选同步 URL ?letter=', async () => {
    const togglePlayedSpy = vi.spyOn(api, 'togglePlayed')
    const createPlaylistSpy = vi.spyOn(api, 'createPlaylist')

    renderWithProviders(
      <>
        <LocationDisplay />
        <Routes>
          <Route path="/library/:id" element={<LibraryPage />} />
        </Routes>
      </>,
      ['/library/lib-movies'],
    )

    await screen.findByTestId('library-grid-view')

    // 字母筛选同步 URL ?letter=X
    fireEvent.click(screen.getByRole('button', { name: 'X' }))
    await waitFor(() => {
      expect(screen.getByTestId('location-search').textContent).toContain('letter=X')
    })

    // 恢复全部
    fireEvent.click(screen.getByRole('button', { name: '全部' }))
    await waitFor(() => {
      expect(screen.getByTestId('location-search').textContent).toBe('')
    })

    // 开启多选模式
    fireEvent.click(screen.getByRole('button', { name: '多选' }))
    const bar = await screen.findByTestId('batch-action-bar')
    expect(within(bar).getByText('已选 0 项')).toBeTruthy()

    // 全选本页
    fireEvent.click(within(bar).getByRole('button', { name: '全选本页' }))
    await waitFor(() => {
      expect(within(bar).getByText(/已选 [1-9]\d* 项/)).toBeTruthy()
    })

    // 清空选择
    fireEvent.click(within(bar).getByRole('button', { name: '清空' }))
    expect(within(bar).getByText('已选 0 项')).toBeTruthy()

    // 在列表视图中点击勾选前两项
    fireEvent.click(screen.getByRole('button', { name: '列表视图' }))
    const listView = await screen.findByTestId('library-list-view')
    const rows = within(listView).getAllByRole('link')
    expect(rows.length).toBeGreaterThanOrEqual(2)

    fireEvent.click(rows[0])
    fireEvent.click(rows[1])
    expect(within(bar).getByText('已选 2 项')).toBeTruthy()

    // 批量标记已看
    fireEvent.click(within(bar).getByRole('button', { name: '标记已看' }))
    await waitFor(() => {
      expect(togglePlayedSpy).toHaveBeenCalledTimes(2)
      expect(togglePlayedSpy).toHaveBeenCalledWith('user-demo-admin', expect.any(String), false)
    })
    await screen.findByText(/已将 2 项标记为已看/)

    // 重新勾选 2 项并打开「加入合集/片单」弹窗新建播放列表
    fireEvent.click(rows[0])
    fireEvent.click(rows[1])
    fireEvent.click(within(bar).getByRole('button', { name: '加入合集/片单' }))

    await screen.findByText('加入合集 / 播放列表')
    fireEvent.click(screen.getByRole('tab', { name: /播放列表/ }))
    const nameInput = screen.getByLabelText(/新建播放列表/)
    fireEvent.change(nameInput, { target: { value: '周末观影清单' } })
    fireEvent.click(screen.getByRole('button', { name: '新建并加入' }))

    await waitFor(() => {
      expect(createPlaylistSpy).toHaveBeenCalledWith(
        '周末观影清单',
        'user-demo-admin',
        expect.arrayContaining([expect.any(String)]),
        'Video',
      )
    })
    await screen.findByText(/已新建播放列表「周末观影清单」/)
  })

  it('HealthAndAuditTab 展示数据库备份列表，支持立即创建备份及二次确认恢复', async () => {
    const createSpy = vi.spyOn(api, 'createBackup')
    const restoreSpy = vi.spyOn(api, 'restoreBackup')

    renderWithProviders(<HealthAndAuditTab />, ['/admin/health'])

    await screen.findByText('数据库备份与恢复')
    await screen.findByText('/data/config/backups')
    await screen.findByText('ambra-backup-20261124-030000.zip')

    // 立即创建备份
    fireEvent.click(screen.getByRole('button', { name: '立即创建备份' }))
    await waitFor(() => {
      expect(createSpy).toHaveBeenCalledTimes(1)
    })
    await screen.findByText(/已创建数据库备份/)

    // 点击「恢复此备份」弹出确认框
    const restoreBtns = screen.getAllByRole('button', { name: '恢复此备份' })
    fireEvent.click(restoreBtns[0])

    await screen.findByText('确认恢复数据库备份')
    expect(screen.getByText(/ambra\.db\.restore/)).toBeTruthy()
    expect(screen.getByText(/下次重启服务器/)).toBeTruthy()

    // 确认恢复
    fireEvent.click(screen.getByRole('button', { name: '确认恢复' }))
    await waitFor(() => {
      expect(restoreSpy).toHaveBeenCalledTimes(1)
    })
    await screen.findByText(/已暂存恢复文件/)
  })

  it('DashboardTab 发消息弹窗空内容展示内联校验错误且默认回退版本为 v0.12.1', async () => {
    vi.spyOn(api, 'getSystemInfo').mockResolvedValue({
      Id: 'srv-1',
      ServerName: 'Ambra Test',
      Version: '',
    })

    renderWithProviders(<DashboardTab />, ['/admin/dashboard'])

    await screen.findByText('v0.12.1')

    const msgBtns = await screen.findAllByRole('button', { name: '发消息' })
    fireEvent.click(msgBtns[0])

    await screen.findByText('向会话发送消息')
    // 提交空白消息
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))

    // 校验错误显示在弹窗表单内部
    const submitBtn = screen.getByRole('button', { name: '发送消息' })
    const form = submitBtn.closest('form')
    expect(form).toBeTruthy()
    expect(within(form!).getByText('消息内容不能为空')).toBeTruthy()
  })

  it('LiveTvTab 在查询接口异常时展示可见错误提示而非空状态', async () => {
    vi.spyOn(api, 'getTunerHosts').mockRejectedValue(new Error('Tuner 服务不可用'))
    vi.spyOn(api, 'getListingProviders').mockRejectedValue(new Error('EPG 服务不可用'))
    vi.spyOn(api, 'getLiveTvChannels').mockRejectedValue(new Error('频道列表查询失败'))

    renderWithProviders(<LiveTvTab />, ['/admin/livetv'])

    await screen.findByText(/加载 M3U 直播源失败：Tuner 服务不可用/)
    await screen.findByText(/加载 XMLTV 节目单来源失败：EPG 服务不可用/)
    await screen.findByText(/加载电视频道列表失败：频道列表查询失败/)
    expect(screen.queryByText('暂未配置 M3U 直播源')).toBeNull()
  })

  it('Demo Mock 引擎修复：字幕 base64 DeliveryUrl、剧集标已看级联、WS 事件派发、备份及预告片/花絮', async () => {
    const uid = 'user-demo-admin'

    // 1. 字幕上传使用 data:text/plain;base64,
    const b64 = btoa('WEBVTT\n\n00:00:01.000 --> 00:00:03.000\nHello')
    await api.uploadSubtitle('item-shawshank-redemption', {
      Language: 'eng',
      Format: 'vtt',
      Data: b64,
    })
    const pb = await api.getPlaybackInfo('item-shawshank-redemption', uid)
    const uploadedSub = pb.MediaSources[0].MediaStreams?.find(
      (s) => s.Type === 'Subtitle' && s.DeliveryUrl === `data:text/plain;base64,${b64}`,
    )
    expect(uploadedSub).toBeDefined()

    // 2. PlayedItems 级联 Series -> Season -> Episode
    await api.togglePlayed(uid, 'item-three-body', false)
    const seasons = await api.getSeasons('item-three-body', uid)
    expect(seasons.Items.every((s) => s.UserData?.Played === true)).toBe(true)
    const eps = await api.getEpisodes('item-three-body', seasons.Items[0].Id, uid)
    expect(eps.Items.every((e) => e.UserData?.Played === true)).toBe(true)

    // 取消已看同样级联
    await api.togglePlayed(uid, 'item-three-body', true)
    const epsAfter = await api.getEpisodes('item-three-body', seasons.Items[0].Id, uid)
    expect(epsAfter.Items.every((e) => e.UserData?.Played === false)).toBe(true)

    // 3. Sessions/:id/Message 与 /Playing/:cmd 派发 ambra:ws-mock 事件
    const wsEvents: CustomEvent[] = []
    const listener = (e: Event) => wsEvents.push(e as CustomEvent)
    window.addEventListener('ambra:ws-mock', listener)
    try {
      const sessions = await api.getSessions()
      expect(sessions.length).toBeGreaterThan(0)
      await api.sendSessionMessage(sessions[0].Id, {
        Header: '测试通知',
        Text: '这是一条 Demo 推送',
      })
      await api.sendSessionPlaystateCommand(sessions[0].Id, 'PlayPause')
    } finally {
      window.removeEventListener('ambra:ws-mock', listener)
    }
    expect(wsEvents).toHaveLength(2)
    expect(wsEvents[0].detail).toMatchObject({
      MessageType: 'GeneralCommand',
      Data: { Name: 'DisplayMessage' },
    })
    expect(wsEvents[1].detail).toMatchObject({
      MessageType: 'Playstate',
      Data: { Command: 'PlayPause' },
    })

    // 4. BackupRestore 闭环
    const infoBefore = await api.getBackupInfo()
    expect(infoBefore.BackupDirectory).toBe('/data/config/backups')
    const created = await api.createBackup()
    expect(created.Name).toMatch(/^ambra-backup-.*\.zip$/)
    const restored = await api.restoreBackup(created.Name)
    expect(restored.Restored).toBe(true)
    expect(restored.RequiresRestart).toBe(true)

    // 5. LocalTrailers & SpecialFeatures（同时支持 item-shawshank-redemption 与 item-shawshank）
    const trailers1 = await api.getLocalTrailers(uid, 'item-shawshank-redemption')
    const trailers2 = await api.getLocalTrailers(uid, 'item-shawshank')
    expect(trailers1.length).toBeGreaterThanOrEqual(1)
    expect(trailers2.length).toBeGreaterThanOrEqual(1)

    const extras1 = await api.getSpecialFeatures(uid, 'item-shawshank-redemption')
    const extras2 = await api.getSpecialFeatures(uid, 'item-shawshank')
    expect(extras1.length).toBeGreaterThanOrEqual(1)
    expect(extras2.length).toBeGreaterThanOrEqual(1)
  })
})
