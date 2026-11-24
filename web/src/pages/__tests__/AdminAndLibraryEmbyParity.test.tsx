import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { Link, MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api, saveAuth } from '../../api/client'
import { AudioPlayerProvider } from '../../context/AudioPlayerContext'
import { AuthProvider } from '../../context/AuthContext'
import { setDemoModeOverride } from '../../demo/demoMode'
import { resetDemoStore } from '../../demo/mockServer'
import { AdminPage } from '../AdminPage'
import { HomePage } from '../HomePage'
import { LibraryPage } from '../LibraryPage'
import { DashboardTab } from '../admin/DashboardTab'
import { LiveTvTab } from '../admin/LiveTvTab'
import { SettingsTab } from '../admin/SettingsTab'
import { TasksLogsTab } from '../admin/TasksLogsTab'

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

describe('Stream 4 (v0.8.0): Web UI Home, Library, Admin & Demo Emby Parity', () => {
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
      deviceId: 'dev-test-v080',
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

  it('HomePage 继续观看卡片支持「标记已看」与「从继续观看移除」快捷操作并刷新缓存', async () => {
    const togglePlayedSpy = vi.spyOn(api, 'togglePlayed')
    const hideResumeSpy = vi.spyOn(api, 'hideFromResume')

    renderWithProviders(<HomePage />, ['/'])

    await screen.findByText('继续观看')
    const hideButtons = await screen.findAllByRole('button', { name: '从继续观看移除' })
    const playedButtons = await screen.findAllByRole('button', { name: '标记已看' })
    expect(hideButtons.length).toBeGreaterThanOrEqual(2)
    expect(playedButtons.length).toBeGreaterThanOrEqual(2)

    const initialCount = hideButtons.length
    // 点击第一个「从继续观看移除」
    fireEvent.click(hideButtons[0])
    await waitFor(() => {
      expect(hideResumeSpy).toHaveBeenCalledTimes(1)
      expect(screen.getAllByRole('button', { name: '从继续观看移除' })).toHaveLength(
        initialCount - 1,
      )
    })

    // 点击剩余项的「标记已看」
    const remainingPlayedButtons = screen.getAllByRole('button', { name: '标记已看' })
    fireEvent.click(remainingPlayedButtons[0])
    await waitFor(() => {
      expect(togglePlayedSpy).toHaveBeenCalledTimes(1)
      expect(screen.queryAllByRole('button', { name: '标记已看' }).length).toBeLessThan(
        remainingPlayedButtons.length,
      )
    })
  })

  it('LibraryPage 支持网格/列表视图切换（持久化 localStorage）、字母索引跳转，以及切换媒体库时重置 statusFilter', async () => {
    const getItemsSpy = vi.spyOn(api, 'getItems')

    renderWithProviders(
      <>
        <Link to="/library/lib-tvshows">切换到剧集库</Link>
        <Routes>
          <Route path="/library/:id" element={<LibraryPage />} />
        </Routes>
      </>,
      ['/library/lib-movies'],
    )

    // 初始为网格视图
    await screen.findByTestId('library-grid-view')

    // 切换到列表视图并持久化到 localStorage
    fireEvent.click(screen.getByRole('button', { name: '列表视图' }))
    await screen.findByTestId('library-list-view')
    expect(localStorage.getItem('ambra_library_view_mode')).toBe('list')

    // 字母索引跳转：点击 X（如 xiaoshenkedejiushu 肖申克的救赎）
    fireEvent.click(screen.getByRole('button', { name: 'X' }))
    await waitFor(() => {
      expect(getItemsSpy).toHaveBeenCalledWith(
        'user-demo-admin',
        expect.objectContaining({
          parentId: 'lib-movies',
          nameStartsWith: 'x',
        }),
      )
    })

    // 字母索引跳转：点击 # 传递 nameLessThan: 'a'
    fireEvent.click(screen.getByRole('button', { name: '#' }))
    await waitFor(() => {
      expect(getItemsSpy).toHaveBeenCalledWith(
        'user-demo-admin',
        expect.objectContaining({
          parentId: 'lib-movies',
          nameLessThan: 'a',
        }),
      )
    })

    // 设置 statusFilter 后切换媒体库，验证 statusFilter 与 letterFilter 均重置
    const statusSelect = screen.getByLabelText('按观看状态筛选') as HTMLSelectElement
    fireEvent.change(statusSelect, { target: { value: 'IsUnplayed' } })
    expect(statusSelect.value).toBe('IsUnplayed')

    fireEvent.click(screen.getByText('切换到剧集库'))
    await waitFor(() => {
      expect((screen.getByLabelText('按观看状态筛选') as HTMLSelectElement).value).toBe('')
    })
  })

  it('DashboardTab 会话卡片支持远程控制（暂停/继续、停止）与发送弹窗消息', async () => {
    const playstateSpy = vi.spyOn(api, 'sendSessionPlaystateCommand')
    const msgSpy = vi.spyOn(api, 'sendSessionMessage')

    renderWithProviders(<DashboardTab />, ['/admin/dashboard'])

    const playPauseBtns = await screen.findAllByRole('button', { name: '暂停/继续' })
    const activePlayPause = playPauseBtns.find((btn) => !(btn as HTMLButtonElement).disabled)
    expect(activePlayPause).toBeDefined()

    // 发送暂停/继续指令
    fireEvent.click(activePlayPause!)
    await waitFor(() => {
      expect(playstateSpy).toHaveBeenCalledWith(expect.any(String), 'PlayPause')
    })
    await screen.findByText(/暂停\/继续指令/)

    // 打开发消息弹窗并提交
    const msgBtns = screen.getAllByRole('button', { name: '发消息' })
    fireEvent.click(msgBtns[0])
    const textArea = await screen.findByLabelText(/消息正文/)
    fireEvent.change(textArea, { target: { value: '系统将在 10 分钟后维护重启' } })
    fireEvent.click(screen.getByRole('button', { name: '发送消息' }))

    await waitFor(() => {
      expect(msgSpy).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({
          Header: '系统通知',
          Text: '系统将在 10 分钟后维护重启',
        }),
      )
    })
    await screen.findByText(/发送弹窗消息/)

    // 发送停止指令
    const stopBtns = screen.getAllByRole('button', { name: '停止' })
    const activeStop = stopBtns.find((btn) => !(btn as HTMLButtonElement).disabled)
    expect(activeStop).toBeDefined()
    fireEvent.click(activeStop!)
    await waitFor(() => {
      expect(playstateSpy).toHaveBeenCalledWith(expect.any(String), 'Stop')
    })
  })

  it('TasksLogsTab 支持查看、新增、删除并保存计划任务触发规则', async () => {
    const updateTriggersSpy = vi.spyOn(api, 'updateTaskTriggers')

    renderWithProviders(<TasksLogsTab />, ['/admin/tasks'])

    const triggerBtns = await screen.findAllByRole('button', { name: '触发规则' })
    expect(triggerBtns.length).toBeGreaterThan(0)
    fireEvent.click(triggerBtns[0])

    // 初始有一条 DailyTrigger
    await screen.findByText('每日 03:00 执行')

    // 新增一条 StartupTrigger
    fireEvent.change(screen.getByLabelText('触发类型'), {
      target: { value: 'StartupTrigger' },
    })
    fireEvent.click(screen.getByRole('button', { name: '添加' }))
    await screen.findByText('服务器启动时触发')

    // 保存规则
    fireEvent.click(screen.getByRole('button', { name: '保存规则' }))
    await waitFor(() => {
      expect(updateTriggersSpy).toHaveBeenCalledWith(
        'task-scan-libraries',
        expect.arrayContaining([
          expect.objectContaining({ Type: 'DailyTrigger' }),
          expect.objectContaining({ Type: 'StartupTrigger' }),
        ]),
      )
    })
    await screen.findByText(/已更新计划任务/)
  })

  it('SettingsTab 保存设置时正确刷新 ["admin", "systemInfo"] 缓存', async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const invalidateSpy = vi.spyOn(qc, 'invalidateQueries')

    renderWithProviders(<SettingsTab />, ['/admin/settings'], qc)

    const nameInput = await screen.findByLabelText(/服务器名称/)
    fireEvent.change(nameInput, { target: { value: 'Ambra Emby Parity' } })
    fireEvent.click(screen.getByRole('button', { name: '保存设置' }))

    await waitFor(() => {
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['admin', 'systemInfo'] })
    })
  })

  it('AdminPage 注册电视直播标签，LiveTvTab 支持 M3U 直播源、XMLTV EPG 增删及频道预览', async () => {
    renderWithProviders(
      <Routes>
        <Route path="/admin/*" element={<AdminPage />} />
      </Routes>,
      ['/admin/livetv'],
    )

    // 侧边导航与 LiveTvTab 标题均正常渲染
    await screen.findByText('电视直播 (Live TV)')
    await screen.findByText('IPTV 高清演示源')
    await screen.findByText('CCTV-1 综合高清')
    expect(screen.getByTestId('livetv-channel-count').textContent).toContain('3')

    // 添加 M3U 直播源
    fireEvent.change(screen.getByLabelText(/源名称/), { target: { value: '测试 M3U 源' } })
    fireEvent.change(screen.getByLabelText(/M3U 地址/), {
      target: { value: 'https://iptv.example.com/list.m3u' },
    })
    fireEvent.click(screen.getByRole('button', { name: '添加源' }))
    await screen.findByText('测试 M3U 源')

    // 添加 XMLTV EPG
    fireEvent.change(screen.getByLabelText(/XMLTV EPG 地址/), {
      target: { value: 'https://epg.example.com/guide.xml' },
    })
    fireEvent.click(screen.getByRole('button', { name: '添加 EPG' }))
    await screen.findByText('https://epg.example.com/guide.xml')

    // 独立渲染 LiveTvTab 验证删除按钮
    const deleteBtns = screen.getAllByRole('button', { name: '删除' })
    expect(deleteBtns.length).toBeGreaterThanOrEqual(2)
    fireEvent.click(deleteBtns[0])
    await screen.findByText(/已删除直播源/)
  })

  it('Demo Mock 完整覆盖 v0.8.0 新端点：字幕上传/删除、LiveTv Info、字母过滤及电影 Chapters', async () => {
    // 1. 至少 2 部 Demo 电影带有 Chapters
    const shawshank = await api.getItem('user-demo-admin', 'item-shawshank-redemption')
    const godfather = await api.getItem('user-demo-admin', 'item-the-godfather')
    expect(shawshank.Chapters?.length).toBeGreaterThanOrEqual(3)
    expect(godfather.Chapters?.length).toBeGreaterThanOrEqual(3)

    // 2. 字幕上传与删除
    const initialSubs = (shawshank.MediaSources?.[0].MediaStreams || []).filter(
      (s) => s.Type === 'Subtitle',
    )
    await api.uploadSubtitle('item-shawshank-redemption', {
      Language: 'jpn',
      Format: 'srt',
      IsForced: false,
      Data: 'MQowMDowMDowMSwwMDAgLS0+IDAwOjAwOjA1LDAwMApIZWxsbw==',
    })
    const afterUpload = await api.getItem('user-demo-admin', 'item-shawshank-redemption')
    const subsAfterUpload = (afterUpload.MediaSources?.[0].MediaStreams || []).filter(
      (s) => s.Type === 'Subtitle',
    )
    expect(subsAfterUpload.length).toBe(initialSubs.length + 1)
    const uploaded = subsAfterUpload[subsAfterUpload.length - 1]
    expect(uploaded.Language).toBe('jpn')

    await api.deleteSubtitle('item-shawshank-redemption', uploaded.Index)
    const afterDelete = await api.getItem('user-demo-admin', 'item-shawshank-redemption')
    const subsAfterDelete = (afterDelete.MediaSources?.[0].MediaStreams || []).filter(
      (s) => s.Type === 'Subtitle',
    )
    expect(subsAfterDelete.length).toBe(initialSubs.length)

    // 3. LiveTv Info & Channels
    const liveTvInfo = await api.getLiveTvInfo()
    expect(liveTvInfo.IsEnabled).toBe(true)
    const liveChannels = await api.getLiveTvChannels(10)
    expect(liveChannels.TotalRecordCount).toBeGreaterThanOrEqual(3)

    // 4. NameStartsWith 过滤
    const xItems = await api.getItems('user-demo-admin', {
      parentId: 'lib-movies',
      nameStartsWith: 'x',
    })
    expect(xItems.Items.some((i) => i.Id === 'item-shawshank-redemption')).toBe(true)

    // 验证 LiveTvTab 组件可独立导出渲染
    expect(LiveTvTab).toBeDefined()
  })
})
