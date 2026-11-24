import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api, saveAuth } from '../../api/client'
import type { ScanProgressSnapshot } from '../../api/types'
import { setDemoModeOverride } from '../../demo/demoMode'
import { resetDemoStore } from '../../demo/mockServer'
import { formatDateTime, formatRelativeTime } from '../../pages/admin/adminUtils'
import { ScanProgressBar } from '../ScanProgressBar'

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

describe('adminUtils 零值时间格式化', () => {
  it('formatDateTime 对 Go 零值时间 (0001-01-01) 与 1970 纪元返回 "-"', () => {
    expect(formatDateTime('0001-01-01T00:00:00Z')).toBe('-')
    expect(formatDateTime('1970-01-01T00:00:00Z')).toBe('-')
    expect(formatDateTime(undefined)).toBe('-')
    expect(formatDateTime('')).toBe('-')
  })

  it('formatRelativeTime 对 Go 零值时间返回 "尚未同步"，不会渲染 1/01/01', () => {
    expect(formatRelativeTime('0001-01-01T00:00:00Z')).toBe('尚未同步')
    expect(formatRelativeTime('1970-01-01T00:00:00Z')).toBe('尚未同步')
    expect(formatRelativeTime(undefined)).toBe('从未')
  })

  it('formatDateTime 对正常 ISO 时间正常格式化', () => {
    const formatted = formatDateTime('2026-05-01T12:30:00Z')
    expect(formatted).toContain('2026')
    expect(formatted).not.toBe('-')
  })
})

describe('ScanProgressBar 实时扫描进度组件', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    setDemoModeOverride(null)
    localStorage.clear()
  })

  it('空闲状态下不渲染任何内容', async () => {
    vi.spyOn(api, 'getScanProgress').mockResolvedValue({
      running: false,
      phase: 'idle',
      total: 0,
      processed: 0,
      added: 0,
      updated: 0,
      skipped: 0,
      failed: 0,
      percentage: 0,
      updated_at: new Date().toISOString(),
      libraries: [],
    })

    renderWithQueryClient(<ScanProgressBar variant="banner" />)

    await waitFor(() => {
      expect(api.getScanProgress).toHaveBeenCalled()
    })
    expect(screen.queryByTestId('scan-progress-banner')).not.toBeInTheDocument()
  })

  it('banner 模式展示当前阶段、当前条目、processed / total 与百分比', async () => {
    const activeSnapshot: ScanProgressSnapshot = {
      running: true,
      phase: 'enriching',
      library_id: 'lib-movies',
      library_name: '4K 电影',
      total: 24,
      processed: 14,
      added: 4,
      updated: 9,
      skipped: 1,
      failed: 0,
      current_item: '沙丘 2 (2024)',
      current_action: '刮削 TMDB 中文元数据与海报',
      percentage: 58,
      started_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    }
    vi.spyOn(api, 'getScanProgress').mockResolvedValue(activeSnapshot)

    renderWithQueryClient(<ScanProgressBar variant="banner" />)

    expect(await screen.findByTestId('scan-progress-banner')).toBeInTheDocument()
    expect(screen.getByText('元数据刮削与海报下载')).toBeInTheDocument()
    expect(screen.getByText(/当前：沙丘 2 \(2024\)/)).toBeInTheDocument()
    expect(screen.getByText('14 / 24')).toBeInTheDocument()
    expect(screen.getByText('58%')).toBeInTheDocument()
  })

  it('card 模式展示新增/更新/跳过/失败统计及多库子进度', async () => {
    const now = new Date().toISOString()
    const activeSnapshot: ScanProgressSnapshot = {
      running: true,
      phase: 'discovering',
      library_name: '全部媒体库',
      total: 30,
      processed: 12,
      added: 5,
      updated: 6,
      skipped: 1,
      failed: 0,
      current_item: '星际穿越',
      current_action: '扫描媒体目录与文件',
      percentage: 40,
      started_at: now,
      updated_at: now,
      libraries: [
        {
          library_id: 'lib-movies',
          library_name: '电影库',
          phase: 'enriching',
          total: 20,
          processed: 10,
          added: 4,
          updated: 5,
          skipped: 1,
          failed: 0,
          current_item: '星际穿越',
          current_action: '刮削海报',
          percentage: 50,
          started_at: now,
          updated_at: now,
        },
        {
          library_id: 'lib-tv',
          library_name: '剧集库',
          phase: 'discovering',
          total: 10,
          processed: 2,
          added: 1,
          updated: 1,
          skipped: 0,
          failed: 0,
          current_item: '三体',
          current_action: '扫描文件',
          percentage: 20,
          started_at: now,
          updated_at: now,
        },
      ],
    }
    vi.spyOn(api, 'getScanProgress').mockResolvedValue(activeSnapshot)

    renderWithQueryClient(<ScanProgressBar variant="card" />)

    expect(await screen.findByTestId('scan-progress-card')).toBeInTheDocument()
    expect(screen.getByText('文件发现')).toBeInTheDocument()
    expect(screen.getByText('12 / 30')).toBeInTheDocument()
    expect(screen.getByText('40%')).toBeInTheDocument()
    expect(screen.getByText('新增 5')).toBeInTheDocument()
    expect(screen.getByText('更新 6')).toBeInTheDocument()
    expect(screen.getByText('跳过 1')).toBeInTheDocument()
    expect(screen.getByText('失败 0')).toBeInTheDocument()
    expect(screen.getByText('电影库')).toBeInTheDocument()
    expect(screen.getByText('剧集库')).toBeInTheDocument()
  })
})

describe('Demo Mock /api/admin/scanner/progress 实时模拟', () => {
  beforeEach(() => {
    localStorage.clear()
    resetDemoStore()
    setDemoModeOverride(true)
  })

  afterEach(() => {
    vi.useRealTimers()
    setDemoModeOverride(null)
    localStorage.clear()
    vi.restoreAllMocks()
  })

  it('触发刷新前返回 idle，触发后按时间推进 discovering -> enriching -> idle', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-11-24T10:00:00.000Z'))

    const authRes = await api.authenticateByName('admin', '')
    saveAuth({
      accessToken: authRes.AccessToken,
      userId: authRes.User.Id,
      serverId: authRes.ServerId,
      userName: authRes.User.Name,
      isAdmin: true,
      deviceId: 'test-dev',
    })

    const idle = await api.getScanProgress()
    expect(idle.running).toBe(false)
    expect(idle.phase).toBe('idle')

    await api.refreshLibrary()

    // 300ms 时处于 discovering 阶段
    vi.advanceTimersByTime(300)
    const discovering = await api.getScanProgress()
    expect(discovering.running).toBe(true)
    expect(discovering.phase).toBe('discovering')
    expect(discovering.total).toBeGreaterThan(0)
    expect(discovering.current_item).toBeTruthy()
    expect(discovering.percentage).toBeGreaterThan(0)

    // 2200ms 时处于 enriching 阶段
    vi.advanceTimersByTime(1900)
    const enriching = await api.getScanProgress()
    expect(enriching.running).toBe(true)
    expect(enriching.phase).toBe('enriching')
    expect(enriching.processed).toBeGreaterThanOrEqual(discovering.processed)
    expect(enriching.percentage).toBeGreaterThan(discovering.percentage)

    // 超过 4000ms 后自动回到 idle
    vi.advanceTimersByTime(2500)
    const finished = await api.getScanProgress()
    expect(finished.running).toBe(false)
    expect(finished.phase).toBe('idle')
  })
})
