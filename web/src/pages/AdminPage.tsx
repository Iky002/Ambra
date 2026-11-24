import { useState } from 'react'
import {
  Activity,
  ArrowRightLeft,
  CalendarClock,
  FolderOpen,
  HeartPulse,
  Key,
  Puzzle,
  RefreshCw,
  Sparkles,
  Users,
} from 'lucide-react'
import { NavLink, Navigate, Route, Routes } from 'react-router-dom'
import { api } from '../api/client'
import { AmbraLogo } from '../components/AmbraLogo'
import { DashboardTab } from './admin/DashboardTab'
import { DevicesKeysTab } from './admin/DevicesKeysTab'
import { HealthAndAuditTab } from './admin/HealthAndAuditTab'
import { LibrariesTab } from './admin/LibrariesTab'
import { MetadataTab } from './admin/MetadataTab'
import { MigrationTab } from './admin/MigrationTab'
import { PluginsTab } from './admin/PluginsTab'
import { TasksLogsTab } from './admin/TasksLogsTab'
import { UsersTab } from './admin/UsersTab'
import { FeedbackBanner } from './admin/adminUtils'

const ADMIN_TABS = [
  {
    path: 'dashboard',
    label: '概览与监控',
    desc: '状态 · 会话 · 活动日志',
    icon: Activity,
  },
  {
    path: 'users',
    label: '用户与权限',
    desc: '账号 · 密码 · RBAC 策略',
    icon: Users,
  },
  {
    path: 'libraries',
    label: '媒体库管理',
    desc: '挂载目录 · 扫描 · 删除',
    icon: FolderOpen,
  },
  {
    path: 'metadata',
    label: '元数据与榜单',
    desc: '豆瓣/IMDb · 编辑 · 识别',
    icon: Sparkles,
  },
  {
    path: 'devices',
    label: '设备与密钥',
    desc: '设备注销 · API Keys',
    icon: Key,
  },
  {
    path: 'plugins',
    label: '插件与刮削',
    desc: 'TMDB · NFO · STRM 配置',
    icon: Puzzle,
  },
  {
    path: 'tasks',
    label: '任务与日志',
    desc: '计划任务 · 服务器日志',
    icon: CalendarClock,
  },
  {
    path: 'health',
    label: '健康与治理',
    desc: '系统体检 · 审校 · NFO',
    icon: HeartPulse,
  },
  {
    path: 'migration',
    label: 'Emby 迁移',
    desc: '一键导入用户与进度',
    icon: ArrowRightLeft,
  },
] as const

export function AdminPage() {
  const [scanning, setScanning] = useState(false)
  const [scanFeedback, setScanFeedback] = useState<{
    type: 'success' | 'error'
    message: string
  } | null>(null)

  const handleGlobalScan = async () => {
    setScanning(true)
    setScanFeedback(null)
    try {
      await api.refreshLibrary()
      setScanFeedback({ type: 'success', message: '已成功触发全部媒体库扫描任务' })
    } catch (err) {
      setScanFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : '触发扫描失败',
      })
    } finally {
      setScanning(false)
    }
  }

  return (
    <div className="space-y-6 px-4 pt-6 lg:px-8">
      {/* 控制台顶部品牌栏 */}
      <div className="flex flex-col gap-4 border-b border-white/8 pb-5 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex items-center gap-3">
          <AmbraLogo size={40} showWordmark subtitle="管理控制台 · Admin Console" />
        </div>

        <button
          type="button"
          disabled={scanning}
          onClick={handleGlobalScan}
          className="inline-flex items-center justify-center gap-2 rounded-xl bg-amber-500 px-4 py-2.5 text-sm font-semibold text-zinc-950 shadow-lg shadow-amber-500/20 transition-all hover:bg-amber-400 disabled:opacity-60"
        >
          <RefreshCw className={`h-4 w-4 ${scanning ? 'animate-spin' : ''}`} />
          <span>{scanning ? '正在触发全库扫描...' : '触发全库扫描'}</span>
        </button>
      </div>

      <FeedbackBanner
        message={scanFeedback?.message}
        type={scanFeedback?.type}
        onClose={() => setScanFeedback(null)}
      />

      {/* 暗色侧边标签 + 内容主区域 */}
      <div className="flex flex-col gap-6 xl:flex-row">
        {/* 左侧/顶部子导航 */}
        <aside className="shrink-0 xl:w-60">
          <nav className="-mx-1 flex gap-2 overflow-x-auto px-1 pb-4 pt-2 xl:mx-0 xl:flex-col xl:overflow-visible xl:p-0">
            {ADMIN_TABS.map((tab) => {
              const Icon = tab.icon
              return (
                <NavLink
                  key={tab.path}
                  to={`/admin/${tab.path}`}
                  className={({ isActive }) =>
                    `group flex shrink-0 items-center gap-3 rounded-xl border px-3.5 py-2.5 transition-all xl:w-full ${
                      isActive
                        ? 'border-amber-500/35 bg-gradient-to-r from-amber-500/18 to-orange-500/8 text-amber-300 shadow-sm'
                        : 'border-white/6 bg-white/[0.02] text-zinc-400 hover:border-white/12 hover:bg-white/[0.05] hover:text-zinc-100'
                    }`
                  }
                >
                  <Icon className="h-4 w-4 shrink-0" />
                  <div className="min-w-0">
                    <p className="truncate text-sm font-medium">{tab.label}</p>
                    <p className="hidden truncate text-[11px] text-zinc-500 xl:block">{tab.desc}</p>
                  </div>
                </NavLink>
              )
            })}
          </nav>
        </aside>

        {/* 右侧面板内容 */}
        <div className="min-w-0 flex-1 rounded-2xl border border-white/8 bg-white/[0.015] p-4 sm:p-6">
          <Routes>
            <Route index element={<Navigate to="dashboard" replace />} />
            <Route path="dashboard" element={<DashboardTab />} />
            <Route path="users" element={<UsersTab />} />
            <Route path="libraries" element={<LibrariesTab />} />
            <Route path="metadata" element={<MetadataTab />} />
            <Route path="devices" element={<DevicesKeysTab />} />
            <Route path="plugins" element={<PluginsTab />} />
            <Route path="tasks" element={<TasksLogsTab />} />
            <Route path="health" element={<HealthAndAuditTab />} />
            <Route path="migration" element={<MigrationTab />} />
            <Route path="*" element={<Navigate to="dashboard" replace />} />
          </Routes>
        </div>
      </div>
    </div>
  )
}
