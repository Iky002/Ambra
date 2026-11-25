import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api, saveAuth } from '../../api/client'
import { AudioPlayerProvider } from '../../context/AudioPlayerContext'
import { AuthProvider } from '../../context/AuthContext'
import { setDemoModeOverride } from '../../demo/demoMode'
import { resetDemoStore } from '../../demo/mockServer'
import { AdminPage } from '../AdminPage'
import { UsersTab } from '../admin/UsersTab'
import { WebhooksTab } from '../admin/WebhooksTab'

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

describe('Stream 4 (v0.9.2): Webhooks Admin Tab, UsersTab Parental Controls & Demo Mocks', () => {
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
      deviceId: 'dev-test-v092',
    })
  })

  afterEach(() => {
    setDemoModeOverride(null)
    localStorage.clear()
    sessionStorage.clear()
    vi.restoreAllMocks()
  })

  it('AdminPage 注册「Webhook 通知」标签页并正确路由至 /admin/webhooks', async () => {
    renderWithProviders(
      <Routes>
        <Route path="/admin/*" element={<AdminPage />} />
      </Routes>,
      ['/admin/webhooks'],
    )

    const webhookNav = await screen.findByRole('link', { name: /Webhook 通知/ })
    expect(webhookNav.getAttribute('href')).toBe('/admin/webhooks')
    await screen.findByText('Webhook 事件通知')
    await screen.findByText('Discord 媒体入库通知')
  })

  it('WebhooksTab 支持列表展示、发送测试（显示状态码与耗时）、新增、编辑与删除', async () => {
    renderWithProviders(<WebhooksTab />, ['/admin/webhooks'])

    // 1. 初始列表渲染
    const initialCard = await screen.findByTestId('webhook-card-wh-demo-01')
    expect(within(initialCard).getByText('Discord 媒体入库通知')).toBeTruthy()
    expect(within(initialCard).getByText('https://discord.com/api/webhooks/demo/ambra-notify')).toBeTruthy()
    expect(within(initialCard).getByText('已启用')).toBeTruthy()
    expect(within(initialCard).getByText('LibraryChanged')).toBeTruthy()
    expect(within(initialCard).getByText('PlaybackStart')).toBeTruthy()

    // 2. 发送测试 -> 展示 HTTP 200 & 42 ms
    fireEvent.click(within(initialCard).getByRole('button', { name: '发送测试' }))
    const testResultBadge = await screen.findByTestId('webhook-test-result-wh-demo-01')
    expect(testResultBadge.textContent).toContain('HTTP 200')
    expect(testResultBadge.textContent).toContain('42 ms')

    // 3. 新增 Webhook
    fireEvent.click(screen.getByRole('button', { name: '新增 Webhook' }))
    fireEvent.change(screen.getByLabelText('名称 *'), { target: { value: 'Slack 播放告警' } })
    fireEvent.change(screen.getByLabelText('推送 URL *'), {
      target: { value: 'https://hooks.slack.com/services/T00/B00/XXX' },
    })
    fireEvent.change(screen.getByLabelText('HMAC 密钥（可选）'), {
      target: { value: 'super-secret-key' },
    })
    // 取消勾选默认全部事件并选择 PlaybackStop & SessionStart
    fireEvent.click(screen.getByLabelText(/PlaybackStop/))
    fireEvent.click(screen.getByLabelText(/SessionStart/))
    fireEvent.click(screen.getByRole('button', { name: '创建 Webhook' }))

    await screen.findByText('Slack 播放告警')
    expect(screen.getByText('https://hooks.slack.com/services/T00/B00/XXX')).toBeTruthy()
    expect(screen.getByText('PlaybackStop')).toBeTruthy()
    expect(screen.getByText('SessionStart')).toBeTruthy()

    // 4. 编辑 Webhook
    fireEvent.click(within(initialCard).getByRole('button', { name: '编辑' }))
    const nameInput = await screen.findByLabelText('名称 *')
    fireEvent.change(nameInput, { target: { value: 'Discord 入库与任务通知' } })
    fireEvent.click(screen.getByRole('button', { name: '保存修改' }))
    await screen.findByText('Discord 入库与任务通知')

    // 5. 删除 Webhook
    fireEvent.click(within(screen.getByTestId('webhook-card-wh-demo-01')).getByRole('button', { name: '删除' }))
    await screen.findByText('确认删除 Webhook')
    fireEvent.click(screen.getByRole('button', { name: '确认删除' }))

    await waitFor(() => {
      expect(screen.queryByTestId('webhook-card-wh-demo-01')).toBeNull()
    })
  })

  it('WebhooksTab 正确处理空状态与接口错误状态', async () => {
    vi.spyOn(api, 'getWebhooks').mockResolvedValueOnce([])
    const { unmount } = renderWithProviders(<WebhooksTab />, ['/admin/webhooks'])
    await screen.findByText('尚未配置任何 Webhook 通知')
    unmount()

    vi.spyOn(api, 'getWebhooks').mockRejectedValueOnce(new Error('Webhook 服务异常'))
    renderWithProviders(<WebhooksTab />, ['/admin/webhooks'])
    await screen.findByTestId('query-error-state')
    expect(screen.getByText('无法加载 Webhook 列表')).toBeTruthy()
    expect(screen.getByText('Webhook 服务异常')).toBeTruthy()
  })

  it('UsersTab 支持家长控制分级 (MaxParentalRating) 与屏蔽标签 (BlockedTags) 配置及卡片徽章展示', async () => {
    const updateSpy = vi.spyOn(api, 'updateUserPolicy')

    renderWithProviders(<UsersTab />, ['/admin/users'])

    // 等待用户列表和分级加载完成
    await screen.findByText('guest')
    const policyButtons = screen.getAllByRole('button', { name: '权限策略' })
    // 打开第二个用户 (guest) 的权限策略弹窗
    fireEvent.click(policyButtons[1])

    const ratingSelect = (await screen.findByLabelText(
      '最高允许内容分级 (MaxParentalRating)',
    )) as HTMLSelectElement
    // 等待分级选项加载
    await waitFor(() => {
      expect(within(ratingSelect).getByRole('option', { name: /PG-13/ })).toBeTruthy()
    })
    expect(within(ratingSelect).getByRole('option', { name: '不限制' })).toBeTruthy()

    // 设置 PG-13 (7) 和屏蔽标签
    fireEvent.change(ratingSelect, { target: { value: '7' } })
    const blockedInput = screen.getByLabelText('屏蔽标签 (BlockedTags)')
    fireEvent.change(blockedInput, { target: { value: '18+, 恐怖, 血腥' } })

    fireEvent.click(screen.getByRole('button', { name: '保存策略' }))

    await waitFor(() => {
      expect(updateSpy).toHaveBeenCalledWith(
        'user-demo-guest',
        expect.objectContaining({
          MaxParentalRating: 7,
          BlockedTags: ['18+', '恐怖', '血腥'],
        }),
      )
    })

    // 卡片上应展示家长控制摘要徽章
    await screen.findByText('分级 ≤ PG-13')
    await screen.findByText('屏蔽 3 个标签')

    // 再次打开并恢复为「不限制」且清空屏蔽标签
    fireEvent.click(screen.getAllByRole('button', { name: '权限策略' })[1])
    const ratingSelect2 = await screen.findByLabelText('最高允许内容分级 (MaxParentalRating)')
    fireEvent.change(ratingSelect2, { target: { value: '' } })
    fireEvent.change(screen.getByLabelText('屏蔽标签 (BlockedTags)'), { target: { value: '' } })
    fireEvent.click(screen.getByRole('button', { name: '保存策略' }))

    await waitFor(() => {
      expect(screen.queryByText('分级 ≤ PG-13')).toBeNull()
      expect(screen.queryByText('屏蔽 3 个标签')).toBeNull()
    })
  })

  it('Demo Mock 完整覆盖 DVR Timers/Recordings、Webhooks、ParentalRatings 与 InstantMix', async () => {
    // 1. LiveTv Timers CRUD
    const timersBefore = await api.getLiveTvTimers()
    expect(timersBefore.TotalRecordCount).toBeGreaterThanOrEqual(1)

    const createdTimer = await api.createLiveTvTimer({
      ChannelId: 'ch-demo-01',
      Name: '晚间新闻录制',
      StartDate: new Date(Date.now() + 1800_000).toISOString(),
      EndDate: new Date(Date.now() + 3600_000).toISOString(),
    })
    expect(createdTimer.Id).toBeTruthy()
    expect(createdTimer.Name).toBe('晚间新闻录制')

    const timersAfterCreate = await api.getLiveTvTimers()
    expect(timersAfterCreate.Items.some((t) => t.Id === createdTimer.Id)).toBe(true)

    await api.deleteLiveTvTimer(createdTimer.Id)
    const timersAfterDelete = await api.getLiveTvTimers()
    expect(timersAfterDelete.Items.some((t) => t.Id === createdTimer.Id)).toBe(false)

    // 2. LiveTv Recordings GET & DELETE
    const recsBefore = await api.getLiveTvRecordings()
    expect(recsBefore.TotalRecordCount).toBeGreaterThanOrEqual(1)
    const firstRecId = recsBefore.Items[0].Id
    await api.deleteLiveTvRecording(firstRecId)
    const recsAfter = await api.getLiveTvRecordings()
    expect(recsAfter.Items.some((r) => r.Id === firstRecId)).toBe(false)

    // 3. Webhooks GET / POST / Test / DELETE
    const whBefore = await api.getWebhooks()
    expect(whBefore.length).toBeGreaterThanOrEqual(1)

    const savedWh = await api.saveWebhook({
      Name: '自建推送',
      Url: 'https://example.com/hook',
      Events: ['LibraryChanged'],
      Enabled: true,
    })
    expect(savedWh.Id).toBeTruthy()

    const testRes = await api.testWebhook({ Id: savedWh.Id, Url: savedWh.Url })
    expect(testRes).toEqual({ Success: true, StatusCode: 200, DurationMs: 42 })

    await api.deleteWebhook(savedWh.Id)
    const whAfter = await api.getWebhooks()
    expect(whAfter.some((w) => w.Id === savedWh.Id)).toBe(false)

    // 4. ParentalRatings
    const ratings = await api.getParentalRatings()
    expect(ratings).toEqual([
      { Name: 'G', Value: 1 },
      { Name: 'PG', Value: 5 },
      { Name: 'PG-13', Value: 7 },
      { Name: 'R', Value: 9 },
      { Name: 'NC-17', Value: 10 },
    ])

    // 5. InstantMix
    const mix = await api.getInstantMix('audio-demo-01', 'user-demo-admin', 20)
    expect(mix.TotalRecordCount).toBeGreaterThan(0)
    expect(mix.Items.length).toBeGreaterThan(0)
    expect(mix.Items.every((it) => it.Type === 'Audio' || it.MediaType === 'Audio')).toBe(true)
  })
})
