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
import { PersonDetailPage } from '../PersonDetailPage'
import { TrendingPage } from '../TrendingPage'

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

  it('TrendingPage：展示热门影片/剧集、支持类型筛选与管理员手动同步热门榜', async () => {
    const user = userEvent.setup()
    const refreshSpy = vi.spyOn(api, 'refreshRankings')
    const Wrapper = createWrapper('/trending')

    render(
      <Wrapper>
        <TrendingPage />
      </Wrapper>,
    )

    expect(await screen.findByRole('heading', { name: '热门趋势' })).toBeInTheDocument()
    expect(await screen.findByText('沙丘 2')).toBeInTheDocument()
    expect(screen.getByText('三体')).toBeInTheDocument()

    // 切换到「剧集」筛选
    await user.click(screen.getByRole('button', { name: '剧集' }))
    await waitFor(() => {
      expect(screen.getByText('三体')).toBeInTheDocument()
      expect(screen.queryByText('沙丘 2')).not.toBeInTheDocument()
    })

    // 管理员点击「立即同步热门榜」
    const syncBtn = await screen.findByRole('button', { name: /立即同步热门榜/i })
    await user.click(syncBtn)
    await waitFor(() => {
      expect(refreshSpy).toHaveBeenCalled()
      expect(screen.getByText(/热门榜同步完成/)).toBeInTheDocument()
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
})
