import { useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  ChevronLeft,
  ChevronRight,
  Clapperboard,
  Film,
  Flame,
  FolderHeart,
  Heart,
  Home,
  Library,
  LogOut,
  Menu,
  Music,
  Radio,
  Search,
  Settings,
  ShieldCheck,
  Sparkles,
  Tv,
  UserCheck,
  X,
} from 'lucide-react'
import { Link, NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { api, getUserImageUrl } from '../api/client'
import type { BaseItemDto } from '../api/types'
import { useAudioPlayer } from '../context/AudioPlayerContext'
import { useAuth } from '../context/AuthContext'
import { isDemoMode } from '../demo/demoMode'
import { AmbraLogo } from './AmbraLogo'
import { AudioPlayerBar } from './AudioPlayerBar'
import { ScanProgressBar } from './ScanProgressBar'
import { SearchModal } from './SearchModal'

function libraryIcon(view: BaseItemDto) {
  const ct = (view.CollectionType || '').toLowerCase()
  if (ct === 'movies') return Film
  if (ct === 'tvshows') return Tv
  if (ct === 'music') return Music
  return Clapperboard
}

export function Layout() {
  const { auth, user, isAdmin, logout, switchDemoRole } = useAuth()
  const { currentTrack } = useAudioPlayer()
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const location = useLocation()
  const outletKey = location.pathname.startsWith('/admin') ? '/admin' : location.pathname
  const [collapsed, setCollapsed] = useState(false)
  const [mobileOpen, setMobileOpen] = useState(false)
  const [searchOpen, setSearchOpen] = useState(false)
  const [switchingRole, setSwitchingRole] = useState(false)
  const [avatarFailed, setAvatarFailed] = useState(false)
  const demoActive = isDemoMode()

  useEffect(() => {
    setAvatarFailed(false)
  }, [user?.PrimaryImageTag, auth?.userId])

  const handleToggleDemoRole = async () => {
    setSwitchingRole(true)
    try {
      await switchDemoRole(isAdmin ? 'guest' : 'admin')
      await queryClient.invalidateQueries()
    } finally {
      setSwitchingRole(false)
    }
  }

  const { data: viewsData } = useQuery({
    queryKey: ['userViews', auth?.userId],
    queryFn: () => api.getUserViews(auth!.userId),
    enabled: Boolean(auth?.userId),
    staleTime: 60_000,
  })

  const { data: liveTvInfo } = useQuery({
    queryKey: ['liveTvInfo'],
    queryFn: () => api.getLiveTvInfo().catch(() => null),
    enabled: Boolean(auth?.userId),
    staleTime: 60_000,
    retry: false,
  })

  const views = viewsData?.Items || []

  // Cmd/Ctrl + K 唤起全局即时搜索
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        setSearchOpen((prev) => !prev)
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [])

  // 路由切换关闭移动端抽屉
  useEffect(() => {
    setMobileOpen(false)
  }, [location.pathname])

  const navLinkClass = ({ isActive }: { isActive: boolean }) =>
    `group flex items-center rounded-xl py-2.5 text-sm font-medium transition-all ${
      collapsed ? 'justify-center px-2' : 'gap-3 px-3'
    } ${
      isActive
        ? 'bg-gradient-to-r from-amber-500/20 to-orange-500/10 text-amber-400 ring-1 ring-amber-500/30'
        : 'text-zinc-400 hover:bg-white/6 hover:text-zinc-100'
    }`

  const sidebarContent = (
    <div className="flex h-full flex-col">
      {/* 品牌 Logo */}
      <div
        className={`flex items-center transition-all ${
          collapsed
            ? 'flex-col justify-center gap-2 px-2 py-3.5'
            : 'h-16 justify-between px-4'
        }`}
      >
        <Link to="/" className="flex shrink-0 items-center">
          <AmbraLogo
            size={collapsed ? 32 : 36}
            showWordmark={!collapsed}
            subtitle={collapsed ? undefined : 'Cinema Server'}
          />
        </Link>

        <button
          type="button"
          onClick={() => setCollapsed((c) => !c)}
          className={`hidden items-center justify-center rounded-lg text-zinc-400 hover:bg-white/8 hover:text-zinc-100 lg:flex ${
            collapsed ? 'h-7 w-7 bg-white/[0.04]' : 'p-1.5'
          }`}
          title={collapsed ? '展开侧边栏' : '折叠侧边栏'}
          aria-label={collapsed ? '展开侧边栏' : '折叠侧边栏'}
        >
          {collapsed ? <ChevronRight className="h-4 w-4" /> : <ChevronLeft className="h-4 w-4" />}
        </button>

        <button
          type="button"
          onClick={() => setMobileOpen(false)}
          className="rounded-lg p-1.5 text-zinc-400 hover:bg-white/8 hover:text-zinc-100 lg:hidden"
        >
          <X className="h-5 w-5" />
        </button>
      </div>

      {/* 导航菜单 */}
      <nav className="flex-1 space-y-6 overflow-y-auto px-3 py-4">
        <div className="space-y-1">
          <NavLink to="/" end className={navLinkClass} title="首页">
            <Home className="h-4 w-4 shrink-0" />
            {!collapsed && <span>首页</span>}
          </NavLink>
          <NavLink to="/trending" className={navLinkClass} title="热门">
            <Flame className="h-4 w-4 shrink-0" />
            {!collapsed && <span>热门</span>}
          </NavLink>
          <NavLink to="/collections" className={navLinkClass} title="合集">
            <Library className="h-4 w-4 shrink-0" />
            {!collapsed && <span>合集</span>}
          </NavLink>
          <NavLink to="/favorites" className={navLinkClass} title="我的收藏">
            <Heart className="h-4 w-4 shrink-0" />
            {!collapsed && <span>我的收藏</span>}
          </NavLink>
          {liveTvInfo?.IsEnabled && (
            <NavLink to="/livetv" className={navLinkClass} title="电视直播">
              <Radio className="h-4 w-4 shrink-0" />
              {!collapsed && <span>电视直播</span>}
            </NavLink>
          )}
          <NavLink to="/search" className={navLinkClass} title="探索与搜索">
            <Search className="h-4 w-4 shrink-0" />
            {!collapsed && <span>探索搜索</span>}
          </NavLink>
        </div>

        {/* 媒体库 */}
        {views.length > 0 && (
          <div className="space-y-1">
            {!collapsed && (
              <p className="px-3 pb-1 text-[11px] font-semibold tracking-wider text-zinc-500 uppercase">
                媒体库
              </p>
            )}
            {views.map((view) => {
              const Icon = libraryIcon(view)
              return (
                <NavLink
                  key={view.Id}
                  to={`/library/${view.Id}`}
                  className={navLinkClass}
                  title={view.Name}
                >
                  <Icon className="h-4 w-4 shrink-0" />
                  {!collapsed && <span className="truncate">{view.Name}</span>}
                </NavLink>
              )
            })}
          </div>
        )}

        {/* 系统管理（仅管理员可见） */}
        {isAdmin && (
          <div className="space-y-1">
            {!collapsed && (
              <p className="px-3 pb-1 text-[11px] font-semibold tracking-wider text-zinc-500 uppercase">
                控制台
              </p>
            )}
            <NavLink to="/admin" className={navLinkClass} title="管理控制台">
              <Settings className="h-4 w-4 shrink-0" />
              {!collapsed && <span>管理控制台</span>}
            </NavLink>
          </div>
        )}
      </nav>

      {/* 用户信息与退出 */}
      <div className="border-t border-white/8 p-3">
        <div className="flex items-center justify-between gap-2 rounded-xl bg-white/[0.03] px-2.5 py-2">
          <Link
            to="/profile"
            title="个人中心"
            className="flex min-w-0 flex-1 items-center gap-2.5 rounded-lg transition-colors hover:text-amber-300"
          >
            <div className="flex h-8 w-8 shrink-0 items-center justify-center overflow-hidden rounded-full bg-gradient-to-br from-amber-500/30 to-orange-500/20 text-xs font-bold text-amber-300 ring-1 ring-amber-400/30">
              {auth?.userId && user?.PrimaryImageTag && !avatarFailed ? (
                <img
                  src={getUserImageUrl(auth.userId, user.PrimaryImageTag)}
                  alt={auth.userName}
                  onError={() => setAvatarFailed(true)}
                  className="h-full w-full object-cover"
                />
              ) : (
                (auth?.userName || 'U').slice(0, 1).toUpperCase()
              )}
            </div>
            {!collapsed && (
              <div className="min-w-0">
                <p className="truncate text-sm font-medium text-zinc-100">{auth?.userName}</p>
                <p className="text-[11px] text-zinc-500">{isAdmin ? '管理员' : '普通成员'}</p>
              </div>
            )}
          </Link>
          {!collapsed && (
            <button
              type="button"
              onClick={() => {
                logout()
                navigate('/login')
              }}
              title="退出登录"
              className="rounded-lg p-1.5 text-zinc-400 transition-colors hover:bg-red-500/15 hover:text-red-400"
            >
              <LogOut className="h-4 w-4" />
            </button>
          )}
        </div>
      </div>
    </div>
  )

  return (
    <div className="flex min-h-screen bg-[#09090d] text-zinc-100">
      {/* 桌面端固定侧边栏 */}
      <aside
        className={`hidden shrink-0 border-r border-white/8 bg-[#0c0c12]/95 transition-all duration-300 lg:block ${
          collapsed ? 'w-[74px]' : 'w-60'
        }`}
      >
        <div className="sticky top-0 h-screen">{sidebarContent}</div>
      </aside>

      {/* 移动端抽屉侧边栏 */}
      {mobileOpen && (
        <div className="fixed inset-0 z-40 flex lg:hidden">
          <div className="fixed inset-0 bg-black/70 backdrop-blur-sm" onClick={() => setMobileOpen(false)} />
          <aside className="relative z-10 w-64 border-r border-white/10 bg-[#0c0c12]">
            {sidebarContent}
          </aside>
        </div>
      )}

      {/* 主内容区 */}
      <div className="flex min-w-0 flex-1 flex-col">
        {/* Serverless Demo 顶部克制提示条（仅 Demo 模式渲染） */}
        {demoActive && (
          <div className="flex flex-wrap items-center justify-between gap-2 border-b border-amber-500/20 bg-gradient-to-r from-amber-500/12 via-orange-500/8 to-amber-500/12 px-4 py-1.5 text-xs text-amber-200/95 lg:px-8">
            <div className="flex items-center gap-2">
              <Sparkles className="h-3.5 w-3.5 shrink-0 text-amber-400" />
              <span>
                <strong className="font-semibold text-amber-300">Serverless Demo 模式</strong>
                <span className="hidden sm:inline">（纯前端内存模拟，无需后端 · 所有读写即时生效）</span>
              </span>
            </div>
            <button
              type="button"
              disabled={switchingRole}
              onClick={() => void handleToggleDemoRole()}
              className="inline-flex items-center gap-1.5 rounded-lg border border-amber-400/35 bg-amber-500/15 px-2.5 py-1 text-[11px] font-medium text-amber-200 transition-colors hover:bg-amber-500/25 disabled:opacity-50"
            >
              {isAdmin ? (
                <>
                  <ShieldCheck className="h-3.5 w-3.5 text-amber-300" />
                  <span>管理员视角 · 切换普通用户体验 RBAC</span>
                </>
              ) : (
                <>
                  <UserCheck className="h-3.5 w-3.5 text-sky-300" />
                  <span>普通用户视角 · 切回管理员控制台</span>
                </>
              )}
            </button>
          </div>
        )}

        {/* 管理员实时媒体库扫描与刮削进度顶栏 */}
        {isAdmin && <ScanProgressBar variant="banner" />}

        {/* 顶部毛玻璃导航条 */}
        <header className="sticky top-0 z-30 flex h-16 items-center justify-between gap-4 border-b border-white/6 bg-[#09090d]/75 px-4 backdrop-blur-xl lg:px-8">
          <div className="flex items-center gap-3">
            <button
              type="button"
              onClick={() => setMobileOpen(true)}
              className="rounded-xl p-2 text-zinc-400 hover:bg-white/8 hover:text-zinc-100 lg:hidden"
            >
              <Menu className="h-5 w-5" />
            </button>

            <button
              type="button"
              onClick={() => setSearchOpen(true)}
              className="flex w-56 items-center justify-between gap-3 rounded-xl border border-white/10 bg-white/[0.04] px-3.5 py-2 text-sm text-zinc-400 transition-all hover:border-amber-400/40 hover:bg-white/[0.07] hover:text-zinc-200 sm:w-80 md:w-96"
            >
              <span className="flex items-center gap-2.5 truncate">
                <Search className="h-4 w-4 text-zinc-400" />
                <span className="truncate">搜索影片、剧集、演员...</span>
              </span>
              <kbd className="hidden rounded-md border border-white/10 bg-black/40 px-1.5 py-0.5 text-[11px] font-medium text-zinc-400 sm:inline-block">
                ⌘K
              </kbd>
            </button>
          </div>

          <div className="flex items-center gap-2">
            <Link
              to="/favorites"
              className="hidden items-center gap-1.5 rounded-xl px-3 py-1.5 text-xs font-medium text-zinc-400 transition-colors hover:bg-white/8 hover:text-zinc-100 sm:flex"
            >
              <FolderHeart className="h-4 w-4 text-amber-400/80" />
              <span>收藏</span>
            </Link>
          </div>
        </header>

        <main className={`flex-1 ${currentTrack ? 'pb-40 lg:pb-32' : 'pb-16'}`}>
          <div key={outletKey} className="animate-page-enter">
            <Outlet />
          </div>
        </main>
      </div>

      <AudioPlayerBar />
      <SearchModal open={searchOpen} onClose={() => setSearchOpen(false)} />
    </div>
  )
}
