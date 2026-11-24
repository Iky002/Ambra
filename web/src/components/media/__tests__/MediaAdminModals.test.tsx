import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api, saveAuth } from '../../../api/client'
import { AuthProvider } from '../../../context/AuthContext'
import { setDemoModeOverride } from '../../../demo/demoMode'
import { resetDemoStore } from '../../../demo/mockServer'
import { ItemDetailPage } from '../../../pages/ItemDetailPage'
import { MetadataTab } from '../../../pages/admin/MetadataTab'
import { IdentifyModal } from '../IdentifyModal'
import { ImageManagerModal } from '../ImageManagerModal'
import { MetadataEditorModal } from '../MetadataEditorModal'

function createTestQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
}

async function loginDemoRole(role: 'admin' | 'guest') {
  const res = await api.authenticateByName(role, '')
  saveAuth({
    accessToken: res.AccessToken,
    userId: res.User.Id,
    serverId: res.ServerId,
    userName: res.User.Name,
    isAdmin: Boolean(res.User.Policy?.IsAdministrator),
    deviceId: 'test-dev',
  })
  return res
}

describe('管理员媒体管理组件与页面集成测试', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    resetDemoStore()
    setDemoModeOverride(true)
  })

  afterEach(() => {
    setDemoModeOverride(null)
    localStorage.clear()
    sessionStorage.clear()
    vi.restoreAllMocks()
  })

  it('MetadataEditorModal 支持编辑元数据并一键切换豆瓣/IMDb 榜单标签', async () => {
    const user = userEvent.setup()
    const authRes = await loginDemoRole('admin')
    const item = await api.getItem(authRes.User.Id, 'item-wandering-earth-2')
    const onSaved = vi.fn()
    const onClose = vi.fn()

    render(
      <QueryClientProvider client={createTestQueryClient()}>
        <MetadataEditorModal open item={item} onClose={onClose} onSaved={onSaved} />
      </QueryClientProvider>,
    )

    const nameInput = screen.getByDisplayValue('流浪地球 2')
    await user.clear(nameInput)
    await user.type(nameInput, '流浪地球 2：重制版')

    await user.click(screen.getByRole('button', { name: /\+ 豆瓣 Top250/i }))
    await user.click(screen.getByRole('button', { name: /保存元数据/i }))

    await waitFor(() => {
      expect(onSaved).toHaveBeenCalled()
      expect(onClose).toHaveBeenCalled()
    })

    const updated = await api.getItem(authRes.User.Id, 'item-wandering-earth-2')
    expect(updated.Name).toBe('流浪地球 2：重制版')
    expect(updated.Tags).toContain('豆瓣Top250')
  })

  it('ImageManagerModal 支持删除当前图片、远端选取替换与 URL 替换', async () => {
    const user = userEvent.setup()
    const authRes = await loginDemoRole('admin')
    const item = await api.getItem(authRes.User.Id, 'item-shawshank-redemption')
    const onUpdated = vi.fn()

    render(
      <QueryClientProvider client={createTestQueryClient()}>
        <ImageManagerModal open item={item} onClose={() => {}} onUpdated={onUpdated} />
      </QueryClientProvider>,
    )

    // 1. 当前图片加载并点击删除
    const deleteButtons = await screen.findAllByRole('button', { name: '删除' })
    expect(deleteButtons.length).toBe(2)
    await user.click(deleteButtons[0])
    await waitFor(() => {
      expect(onUpdated).toHaveBeenCalledWith(expect.stringContaining('已删除'))
    })

    // 2. 切换至远端选取并点击「设为当前」
    await user.click(screen.getByRole('button', { name: /远端选取/i }))
    const applyBtns = await screen.findAllByRole('button', { name: '设为当前' })
    expect(applyBtns.length).toBeGreaterThan(0)
    await user.click(applyBtns[0])
    await waitFor(() => {
      expect(onUpdated).toHaveBeenCalledWith(expect.stringContaining('设为当前'))
    })

    // 3. 切换至上传 / URL 替换并通过 URL 替换
    await user.click(screen.getByRole('button', { name: /上传 \/ URL 替换/i }))
    const urlInput = screen.getByPlaceholderText('https://image.tmdb.org/t/p/w500/...')
    await user.type(urlInput, 'https://image.tmdb.org/t/p/w500/gEU2QniE6E77NI6lCU6MxlNBvIx.jpg')
    await user.click(screen.getByRole('button', { name: 'URL 替换' }))
    await waitFor(() => {
      expect(onUpdated).toHaveBeenCalledWith(expect.stringContaining('URL 替换'))
    })
  })

  it('IdentifyModal 支持远端搜索并应用候选匹配', async () => {
    const user = userEvent.setup()
    const authRes = await loginDemoRole('admin')
    const item = await api.getItem(authRes.User.Id, 'item-wandering-earth-2')
    const onApplied = vi.fn()
    const onClose = vi.fn()

    render(
      <QueryClientProvider client={createTestQueryClient()}>
        <IdentifyModal open item={item} onClose={onClose} onApplied={onApplied} />
      </QueryClientProvider>,
    )

    const nameInput = screen.getByPlaceholderText('输入中英文片名')
    const tmdbInput = screen.getByPlaceholderText('例如：27205')
    const imdbInput = screen.getByPlaceholderText('tt1375666')
    await user.clear(nameInput)
    await user.clear(tmdbInput)
    await user.clear(imdbInput)
    await user.type(nameInput, '星际穿越')
    await user.click(screen.getByRole('button', { name: /搜索候选匹配/i }))

    const applyBtns = await screen.findAllByRole('button', { name: /应用此匹配/i })
    await user.click(applyBtns[0])

    await waitFor(() => {
      expect(onApplied).toHaveBeenCalled()
      expect(onClose).toHaveBeenCalled()
    })

    const updated = await api.getItem(authRes.User.Id, 'item-wandering-earth-2')
    expect(updated.Name).toBe('星际穿越')
  })

  it('ItemDetailPage 对管理员展示完整操作栏（含拆分多版本与删除），对普通用户隐藏', async () => {
    const user = userEvent.setup()
    await loginDemoRole('admin')

    const { unmount } = render(
      <QueryClientProvider client={createTestQueryClient()}>
        <AuthProvider>
          <MemoryRouter initialEntries={['/item/item-shawshank-redemption']}>
            <Routes>
              <Route path="/item/:id" element={<ItemDetailPage />} />
              <Route path="/library/:id" element={<div>已返回媒体库</div>} />
            </Routes>
          </MemoryRouter>
        </AuthProvider>
      </QueryClientProvider>,
    )

    expect(await screen.findByText('肖申克的救赎')).toBeInTheDocument()
    expect(await screen.findByRole('button', { name: '编辑元数据' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '编辑图片/海报' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '识别' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '刷新元数据' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '拆分多版本' })).toBeInTheDocument()

    // 点击删除媒体并确认，跳转回媒体库
    await user.click(screen.getByRole('button', { name: '删除媒体' }))
    expect(await screen.findByText('确认删除媒体')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '确认删除' }))
    expect(await screen.findByText('已返回媒体库')).toBeInTheDocument()
    unmount()

    // 切换为普通用户 guest，不应看到管理员操作栏
    resetDemoStore()
    await loginDemoRole('guest')
    render(
      <QueryClientProvider client={createTestQueryClient()}>
        <AuthProvider>
          <MemoryRouter initialEntries={['/item/item-shawshank-redemption']}>
            <Routes>
              <Route path="/item/:id" element={<ItemDetailPage />} />
            </Routes>
          </MemoryRouter>
        </AuthProvider>
      </QueryClientProvider>,
    )

    expect(await screen.findByText('肖申克的救赎')).toBeInTheDocument()
    expect(screen.queryByText('管理员操作')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '删除媒体' })).not.toBeInTheDocument()
  })

  it('管理控制台 MetadataTab 展示榜单缓存并支持一键同步打标与全库搜索', async () => {
    const user = userEvent.setup()
    await loginDemoRole('admin')

    render(
      <QueryClientProvider client={createTestQueryClient()}>
        <AuthProvider>
          <MemoryRouter initialEntries={['/admin/metadata']}>
            <MetadataTab />
          </MemoryRouter>
        </AuthProvider>
      </QueryClientProvider>,
    )

    expect(await screen.findByText('影史 Top 250 与热门榜单缓存')).toBeInTheDocument()
    expect(await screen.findByText('肖申克的救赎')).toBeInTheDocument()

    // 触发榜单同步
    await user.click(screen.getByRole('button', { name: /立即同步并自动打标/i }))
    expect(await screen.findByText(/榜单同步完成/)).toBeInTheDocument()

    // 搜索过滤
    const searchInput = screen.getByPlaceholderText('搜索片名 / 原名 / 演员...')
    await user.type(searchInput, '霸王别姬')
    await waitFor(() => {
      expect(screen.getByText('霸王别姬')).toBeInTheDocument()
      expect(screen.queryByText('肖申克的救赎')).not.toBeInTheDocument()
    })
  })

  it('P0-4: ItemDetailPage 当条目不存在或加载失败时展示错误提示卡片（含重试与返回首页）而非无限加载', async () => {
    const user = userEvent.setup()
    await loginDemoRole('admin')

    render(
      <QueryClientProvider client={createTestQueryClient()}>
        <AuthProvider>
          <MemoryRouter initialEntries={['/item/non-existent-item-999']}>
            <Routes>
              <Route path="/item/:id" element={<ItemDetailPage />} />
              <Route path="/" element={<div>Ambra 影院首页</div>} />
            </Routes>
          </MemoryRouter>
        </AuthProvider>
      </QueryClientProvider>,
    )

    expect(await screen.findByText('无法加载媒体详情')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /重试/i })).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /返回首页/i }))
    expect(await screen.findByText('Ambra 影院首页')).toBeInTheDocument()
  })

  it('P1-7 & P1-8: MetadataEditorModal 正确显示 NfoPath 且保存锁定时失效 item 查询缓存', async () => {
    const user = userEvent.setup()
    const authRes = await loginDemoRole('admin')
    const item = await api.getItem(authRes.User.Id, 'item-wandering-earth-2')
    const qc = createTestQueryClient()
    const invalidateSpy = vi.spyOn(qc, 'invalidateQueries')

    render(
      <QueryClientProvider client={qc}>
        <MetadataEditorModal open item={item} onClose={() => {}} />
      </QueryClientProvider>,
    )

    // 1. 触发原子回写 .nfo，验证返回 NfoPath 被正确展示
    await user.click(screen.getByRole('button', { name: /原子回写 \.nfo/i }))
    expect(await screen.findByText(/已原子回写 NFO：.*\.nfo/)).toBeInTheDocument()

    // 2. 触发保存锁定配置，验证 invalidateQueries 包含 ['item']
    await user.click(screen.getByRole('button', { name: /片名 \(Name\)/i }))
    await user.click(screen.getByRole('button', { name: /保存锁定配置/i }))
    expect(await screen.findByText(/已保存字段锁定/)).toBeInTheDocument()
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['item'] })
  })
})
