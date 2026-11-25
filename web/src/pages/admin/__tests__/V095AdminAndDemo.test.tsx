import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api, embyFetch, saveAuth } from '../../../api/client'
import type { ActivityLogEntry, QueryResult } from '../../../api/types'
import { setDemoModeOverride } from '../../../demo/demoMode'
import { resetDemoStore } from '../../../demo/mockServer'
import { DashboardTab } from '../DashboardTab'
import { DevicesKeysTab } from '../DevicesKeysTab'
import { LibrariesTab } from '../LibrariesTab'
import { PluginsTab } from '../PluginsTab'
import { SettingsTab } from '../SettingsTab'
import { TasksLogsTab } from '../TasksLogsTab'

function renderWithQueryClient(ui: React.ReactElement) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
    },
  })
  return {
    queryClient,
    ...render(<QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>),
  }
}

describe('v0.9.5 Admin Error States, Modals, ActivityLog, WeeklyTrigger & Demo Music', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    resetDemoStore()
    setDemoModeOverride(false)
    saveAuth({
      accessToken: 'test-admin-token',
      userId: 'user-admin',
      serverId: 'srv-1',
      userName: 'admin',
      isAdmin: true,
      deviceId: 'dev-test',
    })
  })

  afterEach(() => {
    vi.restoreAllMocks()
    setDemoModeOverride(null)
    localStorage.clear()
    sessionStorage.clear()
  })

  it('Demo lib-music 返回递归 Audio 曲目与 MusicArtist 艺术家，并支持 /Items/Filters 与 Studios 查询', async () => {
    setDemoModeOverride(true)
    const authRes = await api.authenticateByName('admin', '')
    saveAuth({
      accessToken: authRes.AccessToken,
      userId: authRes.User.Id,
      serverId: authRes.ServerId,
      userName: authRes.User.Name,
      isAdmin: true,
      deviceId: 'dev-demo',
    })

    const audioRes = await api.getItems(authRes.User.Id, {
      parentId: 'lib-music',
      includeItemTypes: 'Audio',
      limit: 50,
    })
    expect(audioRes.TotalRecordCount).toBe(8)
    expect(audioRes.Items.every((it) => it.Type === 'Audio')).toBe(true)
    expect(audioRes.Items.map((it) => it.Name)).toEqual(
      expect.arrayContaining(['Cornfield Chase', "那个夏天 (One Summer's Day)"]),
    )

    const artistRes = await api.getItems(authRes.User.Id, {
      parentId: 'lib-music',
      includeItemTypes: 'MusicArtist',
      limit: 50,
    })
    expect(artistRes.TotalRecordCount).toBe(2)
    expect(artistRes.Items.every((it) => it.Type === 'MusicArtist')).toBe(true)
    expect(artistRes.Items.map((it) => it.Name)).toEqual(
      expect.arrayContaining(['Hans Zimmer', '久石让']),
    )

    // /Items/Filters & /Items/Filters2
    const filters = await embyFetch<{
      Genres: string[]
      Tags: string[]
      Years: number[]
      Studios: string[]
      OfficialRatings: string[]
    }>('/emby/Items/Filters?ParentId=lib-movies')
    expect(filters.Genres.length).toBeGreaterThan(0)
    expect(filters.Tags).toContain('豆瓣Top250')
    expect(filters.Years).toContain(1994)
    expect(filters.Studios).toContain('Paramount Pictures')
    expect(filters.OfficialRatings).toContain('R')

    const filters2 = await embyFetch<{
      Genres: Array<{ Name: string; Id: string }>
      Studios: Array<{ Name: string; Id: string }>
    }>('/emby/Items/Filters2?ParentId=lib-movies')
    expect(filters2.Studios.some((s) => s.Name === 'Paramount Pictures')).toBe(true)

    // Studios 筛选参数
    const studioItems = await embyFetch<QueryResult<{ Name: string }>>(
      `/emby/Users/${authRes.User.Id}/Items?ParentId=lib-movies&Studios=${encodeURIComponent('Paramount Pictures')}`,
    )
    expect(studioItems.TotalRecordCount).toBeGreaterThanOrEqual(2)
    expect(studioItems.Items.map((it) => it.Name)).toEqual(
      expect.arrayContaining(['教父', '阿甘正传']),
    )
  })

  it('DevicesKeysTab 踢出设备和吊销 API Key 前均需经过确认弹窗，且查询失败时展示错误态', async () => {
    vi.spyOn(api, 'getDevices').mockResolvedValue({
      Items: [
        {
          Id: 'dev-1',
          Name: '客厅 Apple TV',
          AppName: 'Infuse',
          AppVersion: '8.0',
          LastUserName: 'admin',
        },
      ],
      TotalRecordCount: 1,
    })
    vi.spyOn(api, 'getApiKeys').mockResolvedValue({
      Items: [
        {
          AccessToken: 'tok_secret_1234567890',
          AppName: 'Sonarr',
          DateCreated: new Date().toISOString(),
        },
      ],
      TotalRecordCount: 1,
    })
    const deleteDeviceSpy = vi.spyOn(api, 'deleteDevice').mockResolvedValue(undefined)
    const deleteKeySpy = vi.spyOn(api, 'deleteApiKey').mockResolvedValue(undefined)

    renderWithQueryClient(<DevicesKeysTab />)

    // 点击踢出按钮不应直接调用接口，而应打开确认弹窗
    const kickBtn = await screen.findByRole('button', { name: '踢出' })
    fireEvent.click(kickBtn)
    expect(deleteDeviceSpy).not.toHaveBeenCalled()
    expect(screen.getByText('确认踢出设备')).toBeInTheDocument()

    // 点击确认踢出后才调用 deleteDevice
    fireEvent.click(screen.getByRole('button', { name: '确认踢出' }))
    await waitFor(() => {
      expect(deleteDeviceSpy).toHaveBeenCalledWith('dev-1')
    })

    // 点击吊销 API Key 按钮不应直接调用接口，而应打开确认弹窗
    const revokeBtn = screen.getByRole('button', { name: '吊销' })
    fireEvent.click(revokeBtn)
    expect(deleteKeySpy).not.toHaveBeenCalled()
    expect(screen.getByText('确认吊销 API Key')).toBeInTheDocument()

    // 点击确认吊销后才调用 deleteApiKey
    fireEvent.click(screen.getByRole('button', { name: '确认吊销' }))
    await waitFor(() => {
      expect(deleteKeySpy).toHaveBeenCalledWith('tok_secret_1234567890')
    })
  })

  it('DashboardTab 支持活动日志级别过滤、关键词搜索与分页切换', async () => {
    vi.spyOn(api, 'getSystemInfo').mockResolvedValue({
      Id: 'srv-1',
      ServerName: 'Ambra Test',
      Version: '4.10.2.0',
    })
    vi.spyOn(api, 'getSessions').mockResolvedValue([])
    vi.spyOn(api, 'getScanProgress').mockResolvedValue({
      running: false,
      paused: false,
      phase: 'idle',
      total: 0,
      processed: 0,
      added: 0,
      updated: 0,
      skipped: 0,
      failed: 0,
      percentage: 0,
    })

    const page0Items: ActivityLogEntry[] = [
      {
        Id: 1,
        Name: '用户 admin 登录成功',
        Type: 'AuthenticationSucceeded',
        Date: new Date().toISOString(),
        Severity: 'Info',
      },
      {
        Id: 2,
        Name: 'TMDB 刮削超时警告',
        Type: 'ScraperWarning',
        Date: new Date().toISOString(),
        Severity: 'Warn',
      },
      {
        Id: 3,
        Name: 'FFmpeg 转码失败错误',
        Type: 'TranscodeError',
        Date: new Date().toISOString(),
        Severity: 'Error',
      },
    ]
    const page1Items: ActivityLogEntry[] = [
      {
        Id: 21,
        Name: '第二页历史日志记录',
        Type: 'SystemInfo',
        Date: new Date().toISOString(),
        Severity: 'Info',
      },
    ]

    const logsSpy = vi
      .spyOn(api, 'getActivityLogs')
      .mockImplementation(async (startIndex = 0, _limit = 20) => ({
        Items: startIndex === 0 ? page0Items : page1Items,
        TotalRecordCount: 25,
      }))

    renderWithQueryClient(<DashboardTab />)

    expect(await screen.findByText('用户 admin 登录成功')).toBeInTheDocument()
    expect(logsSpy).toHaveBeenCalledWith(0, 20)
    expect(screen.getByText(/共 25 条记录 · 第 1 \/ 2 页/)).toBeInTheDocument()

    // 级别过滤：Warn
    fireEvent.click(screen.getByRole('button', { name: 'Warn' }))
    expect(screen.getByText('TMDB 刮削超时警告')).toBeInTheDocument()
    expect(screen.queryByText('用户 admin 登录成功')).not.toBeInTheDocument()
    expect(screen.queryByText('FFmpeg 转码失败错误')).not.toBeInTheDocument()

    // 级别过滤：Error
    fireEvent.click(screen.getByRole('button', { name: 'Error' }))
    expect(screen.getByText('FFmpeg 转码失败错误')).toBeInTheDocument()
    expect(screen.queryByText('TMDB 刮削超时警告')).not.toBeInTheDocument()

    // 恢复全部并关键词过滤
    fireEvent.click(screen.getByRole('button', { name: '全部' }))
    fireEvent.change(screen.getByLabelText('搜索活动日志'), { target: { value: 'TMDB' } })
    expect(screen.getByText('TMDB 刮削超时警告')).toBeInTheDocument()
    expect(screen.queryByText('用户 admin 登录成功')).not.toBeInTheDocument()

    // 清空关键词并翻到下一页
    fireEvent.change(screen.getByLabelText('搜索活动日志'), { target: { value: '' } })
    fireEvent.click(screen.getByRole('button', { name: /下一页/ }))

    await waitFor(() => {
      expect(logsSpy).toHaveBeenCalledWith(20, 20)
    })
    expect(await screen.findByText('第二页历史日志记录')).toBeInTheDocument()
    expect(screen.getByText(/第 2 \/ 2 页/)).toBeInTheDocument()
  })

  it('TasksLogsTab 支持 WeeklyTrigger 触发规则配置及服务器日志文件下载', async () => {
    vi.spyOn(api, 'getScheduledTasks').mockResolvedValue([
      {
        Id: 'task-scan',
        Name: '扫描媒体库',
        State: 'Idle',
        Category: '媒体库',
        Triggers: [],
      },
    ])
    vi.spyOn(api, 'getServerLogs').mockResolvedValue([
      {
        Name: 'ambra-20261125.log',
        Size: 1024,
        DateModified: new Date().toISOString(),
      },
    ])
    vi.spyOn(api, 'getServerLogContent').mockResolvedValue('2026-11-25 INFO server started\n')
    const updateTriggersSpy = vi.spyOn(api, 'updateTaskTriggers').mockResolvedValue(undefined)

    const createObjectURLSpy = vi
      .spyOn(URL, 'createObjectURL')
      .mockReturnValue('blob:mock-log-url')
    const revokeObjectURLSpy = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})
    const anchorClickSpy = vi
      .spyOn(HTMLAnchorElement.prototype, 'click')
      .mockImplementation(() => {})

    renderWithQueryClient(<TasksLogsTab />)

    // 打开触发规则弹窗并添加 WeeklyTrigger
    fireEvent.click(await screen.findByRole('button', { name: /触发规则/ }))
    fireEvent.change(screen.getByLabelText('触发类型'), { target: { value: 'WeeklyTrigger' } })
    fireEvent.change(screen.getByLabelText('每周执行星期'), { target: { value: 'Wednesday' } })
    fireEvent.change(screen.getByLabelText('每周执行时间'), { target: { value: '04:30' } })
    fireEvent.click(screen.getByRole('button', { name: /添加/ }))
    fireEvent.click(screen.getByRole('button', { name: /保存规则/ }))

    await waitFor(() => {
      expect(updateTriggersSpy).toHaveBeenCalledWith('task-scan', [
        {
          Type: 'WeeklyTrigger',
          DayOfWeek: 'Wednesday',
          TimeOfDayTicks: (4 * 60 + 30) * 60 * 10_000_000,
        },
      ])
    })

    // 选择日志文件并点击「下载日志」
    fireEvent.click(screen.getByRole('button', { name: /ambra-20261125\.log/ }))
    expect(await screen.findByText(/server started/)).toBeInTheDocument()

    const downloadBtn = screen.getByRole('button', { name: /下载日志/ })
    fireEvent.click(downloadBtn)
    expect(createObjectURLSpy).toHaveBeenCalledTimes(1)
    expect(anchorClickSpy).toHaveBeenCalledTimes(1)
    expect(revokeObjectURLSpy).toHaveBeenCalledWith('blob:mock-log-url')
  })

  it('LibrariesTab / PluginsTab / SettingsTab 查询失败时渲染 QueryErrorState 而非空状态', async () => {
    vi.spyOn(api, 'getVirtualFolders').mockRejectedValue(new Error('媒体库接口异常'))
    vi.spyOn(api, 'getScanProgress').mockResolvedValue({
      running: false,
      paused: false,
      phase: 'idle',
      total: 0,
      processed: 0,
      added: 0,
      updated: 0,
      skipped: 0,
      failed: 0,
      percentage: 0,
    })
    const { unmount: unmountLib } = renderWithQueryClient(<LibrariesTab />)
    expect(await screen.findByText('无法加载媒体库列表')).toBeInTheDocument()
    expect(screen.getByText('媒体库接口异常')).toBeInTheDocument()
    unmountLib()

    vi.spyOn(api, 'getPlugins').mockRejectedValue(new Error('插件服务不可用'))
    const { unmount: unmountPlugins } = renderWithQueryClient(<PluginsTab />)
    expect(await screen.findByText('无法加载插件列表')).toBeInTheDocument()
    expect(screen.getByText('插件服务不可用')).toBeInTheDocument()
    unmountPlugins()

    vi.spyOn(api, 'getServerConfig').mockRejectedValue(new Error('配置读取失败'))
    renderWithQueryClient(<SettingsTab />)
    expect(await screen.findByText('无法加载服务器配置')).toBeInTheDocument()
    expect(screen.getByText('配置读取失败')).toBeInTheDocument()
  })
})
