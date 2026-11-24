import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api, saveAuth } from '../../api/client'
import { MergeVersionsModal } from '../../components/media/MergeVersionsModal'
import { AuthProvider } from '../../context/AuthContext'
import { setDemoModeOverride } from '../../demo/demoMode'
import { resetDemoStore } from '../../demo/mockServer'
import { CollectionsPage } from '../CollectionsPage'
import { LibraryPage } from '../LibraryPage'
import { PersonDetailPage } from '../PersonDetailPage'
import { TrendingPage } from '../TrendingPage'
import { MetadataTab } from '../admin/MetadataTab'
import { TasksLogsTab } from '../admin/TasksLogsTab'

function createWrapper(initialEntry = '/') {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return (
      <QueryClientProvider client={qc}>
        <AuthProvider>
          <MemoryRouter initialEntries={[initialEntry]}>{children}</MemoryRouter>
        </AuthProvider>
      </QueryClientProvider>
    )
  }
}

describe('v0.4 前端核心功能：合并多版本、热门趋势、演职员详情、合集页', () => {
  beforeEach(async () => {
    localStorage.clear()
    sessionStorage.clear()
    resetDemoStore()
    setDemoModeOverride(true)

    const authRes = await api.authenticateByName('admin', '')
    saveAuth({
      accessToken: authRes.AccessToken,
      userId: authRes.User.Id,
      serverId: authRes.ServerId,
      userName: authRes.User.Name,
      isAdmin: true,
      deviceId: 'test-dev',
    })
  })

  afterEach(() => {
    setDemoModeOverride(null)
    localStorage.clear()
    sessionStorage.clear()
    vi.restoreAllMocks()
  })

  it('MergeVersionsModal：搜索同类型候选、排除自身并提交合并', async () => {
    const user = userEvent.setup()
    const authRes = await api.authenticateByName('admin', '')
    const baseItem = await api.getItem(authRes.User.Id, 'item-dune-2')
    const onClose = vi.fn()
    const onMerged = vi.fn()
    const mergeSpy = vi.spyOn(api, 'mergeItemVersions')

    const Wrapper = createWrapper('/item/item-dune-2')
    render(
      <Wrapper>
        <MergeVersionsModal open item={baseItem} onClose={onClose} onMerged={onMerged} />
      </Wrapper>,
    )

    expect(await screen.findByText('合并为多版本')).toBeInTheDocument()

    // 搜索候选条目「星际穿越」
    const input = screen.getByPlaceholderText(/搜索片名/)
    await user.type(input, '星际穿越')

    const candidateBtn = await screen.findByRole('button', { name: /星际穿越/i })
    await user.click(candidateBtn)

    const confirmBtn = screen.getByRole('button', { name: /确认合并/i })
    expect(confirmBtn).toBeEnabled()
    await user.click(confirmBtn)

    await waitFor(() => {
      expect(mergeSpy).toHaveBeenCalledWith(['item-dune-2', 'item-interstellar'])
      expect(onMerged).toHaveBeenCalled()
      expect(onClose).toHaveBeenCalled()
    })
  })

  it('TrendingPage：默认三分栏展示 TMDB 热门 / 豆瓣热门电影 / 豆瓣热门剧集，支持单栏切换与手动同步', async () => {
    const user = userEvent.setup()
    const refreshSpy = vi.spyOn(api, 'refreshRankings')
    const Wrapper = createWrapper('/trending')

    render(
      <Wrapper>
        <TrendingPage />
      </Wrapper>,
    )

    expect(await screen.findByRole('heading', { name: '热门趋势' })).toBeInTheDocument()
    // 三分栏总览下应同时展示三个独立分区标题
    expect(await screen.findByRole('heading', { name: 'TMDB 热门' })).toBeInTheDocument()
    expect(await screen.findByRole('heading', { name: '豆瓣热门电影' })).toBeInTheDocument()
    expect(await screen.findByRole('heading', { name: '豆瓣热门剧集' })).toBeInTheDocument()

    // 验证三栏各自命中作品出现
    expect((await screen.findAllByText('沙丘 2')).length).toBeGreaterThanOrEqual(1)
    expect((await screen.findAllByText('机器人之梦')).length).toBeGreaterThanOrEqual(1)
    expect((await screen.findAllByText('漫长的季节')).length).toBeGreaterThanOrEqual(1)

    // 切换到「TMDB 热门」单栏并按「剧集」子筛选
    await user.click(screen.getByRole('button', { name: 'TMDB 热门' }))
    await waitFor(() => {
      expect(screen.queryByRole('heading', { name: '豆瓣热门电影' })).not.toBeInTheDocument()
    })
    await user.click(screen.getByRole('button', { name: '剧集' }))
    await waitFor(() => {
      expect(screen.getByText('幕府将军')).toBeInTheDocument()
      expect(screen.queryByText('沙丘 2')).not.toBeInTheDocument()
    })

    // 切换到「豆瓣热门剧集」单栏
    await user.click(screen.getByRole('button', { name: '豆瓣热门剧集' }))
    expect(await screen.findByText('繁花')).toBeInTheDocument()
    expect(screen.getByText('漫长的季节')).toBeInTheDocument()

    // 管理员点击「立即同步热门榜」
    const syncBtn = await screen.findByRole('button', { name: /立即同步热门榜/i })
    await user.click(syncBtn)
    await waitFor(() => {
      expect(refreshSpy).toHaveBeenCalled()
      expect(screen.getByText(/热门榜同步完成/)).toBeInTheDocument()
    })
  })

  it('MetadataTab：展示 5 项榜单缓存卡片并支持服务端分页、每页条数切换与筛选重置页码', async () => {
    const user = userEvent.setup()
    const fakeItems = Array.from({ length: 45 }, (_, idx) => ({
      Id: `fake-item-${idx + 1}`,
      Name: `测试影片 ${idx + 1}`,
      Type: idx % 2 === 0 ? 'Movie' : 'Series',
      ProductionYear: 2024,
      Tags: idx === 0 ? ['TMDB热门', '豆瓣热门电影'] : [],
    }))

    const getItemsSpy = vi.spyOn(api, 'getItems').mockImplementation(async (_uid, params) => {
      const start = params?.startIndex ?? 0
      const limit = params?.limit ?? 20
      const filtered = fakeItems.filter((it) => {
        if (params?.includeItemTypes && params.includeItemTypes !== 'Movie,Series') {
          if (it.Type !== params.includeItemTypes) return false
        }
        return true
      })
      return {
        Items: filtered.slice(start, start + limit),
        TotalRecordCount: filtered.length,
      }
    })

    const Wrapper = createWrapper('/admin')
    render(
      <Wrapper>
        <MetadataTab />
      </Wrapper>,
    )

    // 验证 5 项榜单卡片渲染（包含豆瓣热门电影与豆瓣热门剧集）
    expect(await screen.findByText('测试影片 1')).toBeInTheDocument()
    expect(screen.getAllByText('豆瓣热门电影').length).toBeGreaterThanOrEqual(1)
    expect(screen.getAllByText('豆瓣热门剧集').length).toBeGreaterThanOrEqual(1)

    // 初始第 1 页：startIndex=0, limit=20
    expect(getItemsSpy).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({ startIndex: 0, limit: 20 }),
    )
    expect(screen.getByText('1–20')).toBeInTheDocument()

    // 点击「下一页」 -> startIndex=20, limit=20
    await user.click(screen.getByRole('button', { name: '下一页' }))
    expect(await screen.findByText('测试影片 21')).toBeInTheDocument()
    expect(getItemsSpy).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({ startIndex: 20, limit: 20 }),
    )
    expect(screen.getByText('21–40')).toBeInTheDocument()

    // 切换每页条数为 50 -> 重置到第 1 页 (startIndex=0, limit=50)
    await user.selectOptions(screen.getByLabelText('每页显示条数'), '50')
    await waitFor(() => {
      expect(getItemsSpy).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({ startIndex: 0, limit: 50 }),
      )
    })
    expect(screen.getByText('1–45')).toBeInTheDocument()

    // 切回每页 20 并翻到第 2 页，再切换类型筛选「电影」 -> 应重置回第 1 页
    await user.selectOptions(screen.getByLabelText('每页显示条数'), '20')
    await user.click(screen.getByRole('button', { name: '下一页' }))
    await waitFor(() => {
      expect(getItemsSpy).toHaveBeenLastCalledWith(
        expect.any(String),
        expect.objectContaining({ startIndex: 20, limit: 20 }),
      )
    })
    await user.click(screen.getByRole('button', { name: '电影' }))
    await waitFor(() => {
      expect(getItemsSpy).toHaveBeenLastCalledWith(
        expect.any(String),
        expect.objectContaining({ includeItemTypes: 'Movie', startIndex: 0, limit: 20 }),
      )
    })
  })

  it('PersonDetailPage：展示演职员资料与库内参演作品', async () => {
    const Wrapper = createWrapper('/person/p-nolan?name=%E5%85%8B%E9%87%8C%E6%96%AF%E6%89%98%E5%BC%97%C2%B7%E8%AF%BA%E5%85%B0')

    render(
      <Wrapper>
        <Routes>
          <Route path="/person/:id" element={<PersonDetailPage />} />
        </Routes>
      </Wrapper>,
    )

    expect(await screen.findByRole('heading', { name: '克里斯托弗·诺兰' })).toBeInTheDocument()
    expect(await screen.findByText('盗梦空间')).toBeInTheDocument()
    expect(screen.getByText('星际穿越')).toBeInTheDocument()
    expect(screen.getByText('奥本海默')).toBeInTheDocument()
  })

  it('CollectionsPage：展示合集 (BoxSet) 与播放列表 Tab', async () => {
    const user = userEvent.setup()
    const Wrapper = createWrapper('/collections')

    render(
      <Wrapper>
        <CollectionsPage />
      </Wrapper>,
    )

    expect(await screen.findByText('科幻史诗合集')).toBeInTheDocument()
    expect(screen.getByText('豆瓣/IMDb 经典必看')).toBeInTheDocument()

    // 切换到播放列表 Tab
    await user.click(screen.getByRole('button', { name: /播放列表/i }))
    expect(await screen.findByText('暂无播放列表')).toBeInTheDocument()
  })

  it('TasksLogsTab：getServerLogs 返回 { Items, TotalRecordCount } 对象时正常渲染不黑屏崩溃', async () => {
    vi.spyOn(api, 'getScheduledTasks').mockResolvedValueOnce([
      {
        Id: 'task-1',
        Name: '扫描媒体库',
        State: 'Idle',
        Category: '媒体库',
      },
    ])
    vi.spyOn(api, 'getServerLogs').mockResolvedValueOnce({
      Items: [
        {
          Name: 'ambra-server-2026.log',
          Size: 2048,
          DateModified: '2026-11-24T10:00:00Z',
        },
      ],
      TotalRecordCount: 1,
    } as unknown as Awaited<ReturnType<typeof api.getServerLogs>>)

    const Wrapper = createWrapper('/admin/tasks')
    render(
      <Wrapper>
        <TasksLogsTab />
      </Wrapper>,
    )

    expect(await screen.findByText('扫描媒体库')).toBeInTheDocument()
    expect(await screen.findByText('ambra-server-2026.log')).toBeInTheDocument()
    expect(screen.getByText('日志文件 (1)')).toBeInTheDocument()
  })

  it('LibraryPage：不再展示手动「合并多版本」切换按钮', async () => {
    const Wrapper = createWrapper('/library/lib-movies')
    render(
      <Wrapper>
        <Routes>
          <Route path="/library/:id" element={<LibraryPage />} />
        </Routes>
      </Wrapper>,
    )

    expect(await screen.findByText('沙丘 2')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /合并多版本/ })).not.toBeInTheDocument()
  })
})
