import { Component, useEffect, useRef, useState, type ErrorInfo, type ReactNode } from 'react'
import {
  Activity,
  AlertTriangle,
  ArrowRightLeft,
  CalendarClock,
  FolderOpen,
  HeartPulse,
  Key,
  Puzzle,
  Radio,
  RefreshCw,
  SlidersHorizontal,
  Sparkles,
  Users,
  Webhook,
} from 'lucide-react'
import { NavLink, Navigate, Route, Routes, useLocation } from 'react-router-dom'
import { api } from '../api/client'
import { AmbraLogo } from '../components/AmbraLogo'
import { getActiveLocale, translateForLocale, useI18n } from '../i18n/I18nContext'
import { DashboardTab } from './admin/DashboardTab'
import { DevicesKeysTab } from './admin/DevicesKeysTab'
import { HealthAndAuditTab } from './admin/HealthAndAuditTab'
import { LibrariesTab } from './admin/LibrariesTab'
import { LiveTvTab } from './admin/LiveTvTab'
import { MetadataTab } from './admin/MetadataTab'
import { MigrationTab } from './admin/MigrationTab'
import { PluginsTab } from './admin/PluginsTab'
import { SettingsTab } from './admin/SettingsTab'
import { TasksLogsTab } from './admin/TasksLogsTab'
import { UsersTab } from './admin/UsersTab'
import { WebhooksTab } from './admin/WebhooksTab'
import { FeedbackBanner } from './admin/adminUtils'

const ADMIN_TABS = [
  {
    path: 'dashboard',
    labelKey: 'admin.tabDashboard',
    descKey: 'admin.tabDashboardDesc',
    icon: Activity,
  },
  {
    path: 'settings',
    labelKey: 'admin.tabSettings',
    descKey: 'admin.tabSettingsDesc',
    icon: SlidersHorizontal,
  },
  {
    path: 'users',
    labelKey: 'admin.tabUsers',
    descKey: 'admin.tabUsersDesc',
    icon: Users,
  },
  {
    path: 'libraries',
    labelKey: 'admin.tabLibraries',
    descKey: 'admin.tabLibrariesDesc',
    icon: FolderOpen,
  },
  {
    path: 'metadata',
    labelKey: 'admin.tabMetadata',
    descKey: 'admin.tabMetadataDesc',
    icon: Sparkles,
  },
  {
    path: 'devices',
    labelKey: 'admin.tabDevicesKeys',
    descKey: 'admin.tabDevicesKeysDesc',
    icon: Key,
  },
  {
    path: 'plugins',
    labelKey: 'admin.tabPlugins',
    descKey: 'admin.tabPluginsDesc',
    icon: Puzzle,
  },
  {
    path: 'tasks',
    labelKey: 'admin.tabTasksLogs',
    descKey: 'admin.tabTasksLogsDesc',
    icon: CalendarClock,
  },
  {
    path: 'livetv',
    labelKey: 'admin.tabLiveTv',
    descKey: 'admin.tabLiveTvDesc',
    icon: Radio,
  },
  {
    path: 'webhooks',
    labelKey: 'admin.tabWebhooks',
    descKey: 'admin.tabWebhooksDesc',
    icon: Webhook,
  },
  {
    path: 'health',
    labelKey: 'admin.tabGovernance',
    descKey: 'admin.tabGovernanceDesc',
    icon: HeartPulse,
  },
  {
    path: 'migration',
    labelKey: 'admin.tabMigration',
    descKey: 'admin.tabMigrationDesc',
    icon: ArrowRightLeft,
  },
] as const

interface AdminTabErrorBoundaryProps {
  children: ReactNode
}

interface AdminTabErrorBoundaryState {
  hasError: boolean
  errorMessage: string
}

class AdminTabErrorBoundary extends Component<
  AdminTabErrorBoundaryProps,
  AdminTabErrorBoundaryState
> {
  state: AdminTabErrorBoundaryState = {
    hasError: false,
    errorMessage: '',
  }

  static getDerivedStateFromError(error: Error): AdminTabErrorBoundaryState {
    return {
      hasError: true,
      errorMessage: error?.message || translateForLocale(getActiveLocale(), 'admin.errorUnknown'),
    }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('[AdminTabErrorBoundary]', error, info)
  }

  render() {
    if (this.state.hasError) {
      const currentLocale = getActiveLocale()
      return (
        <div className="flex min-h-[280px] flex-col items-center justify-center rounded-2xl border border-red-500/25 bg-red-500/[0.04] p-6 text-center">
          <AlertTriangle className="mb-3 h-8 w-8 text-red-400" />
          <h3 className="text-sm font-semibold text-zinc-100">
            {translateForLocale(currentLocale, 'admin.errorPanelTitle')}
          </h3>
          <p className="mt-1.5 max-w-md text-xs leading-relaxed text-zinc-400">
            {this.state.errorMessage}
          </p>
          <button
            type="button"
            onClick={() => this.setState({ hasError: false, errorMessage: '' })}
            className="mt-4 inline-flex items-center gap-1.5 rounded-xl border border-white/15 bg-white/8 px-4 py-2 text-xs font-medium text-zinc-200 transition-colors hover:bg-white/15"
          >
            <RefreshCw className="h-3.5 w-3.5" />
            <span>{translateForLocale(currentLocale, 'admin.retryLoad')}</span>
          </button>
        </div>
      )
    }
    return this.props.children
  }
}

export function AdminPage() {
  const { t } = useI18n()
  const location = useLocation()
  const navRef = useRef<HTMLElement | null>(null)
  const savedScrollLeftRef = useRef<number>(0)
  const [scanning, setScanning] = useState(false)
  const [scanFeedback, setScanFeedback] = useState<{
    type: 'success' | 'error'
    message: string
  } | null>(null)

  useEffect(() => {
    const nav = navRef.current
    if (!nav) return

    if (savedScrollLeftRef.current > 0 && nav.scrollLeft === 0) {
      nav.scrollLeft = savedScrollLeftRef.current
    }

    const activeEl = nav.querySelector('a[aria-current="page"]') as HTMLElement | null
    if (!activeEl) return

    const navRect = nav.getBoundingClientRect()
    const elRect = activeEl.getBoundingClientRect()
    const hasVisibleBox = navRect.width > 0 && elRect.width > 0
    const isClipped = hasVisibleBox && (elRect.left < navRect.left || elRect.right > navRect.right)
    if (isClipped && typeof activeEl.scrollIntoView === 'function') {
      activeEl.scrollIntoView({ behavior: 'smooth', inline: 'nearest', block: 'nearest' })
    }
  }, [location.pathname])

  const handleGlobalScan = async () => {
    setScanning(true)
    setScanFeedback(null)
    try {
      await api.refreshLibrary()
      setScanFeedback({ type: 'success', message: t('admin.scanTriggeredSuccess') })
    } catch (err) {
      setScanFeedback({
        type: 'error',
        message: err instanceof Error ? err.message : t('admin.scanTriggerFailed'),
      })
    } finally {
      setScanning(false)
    }
  }

  return (
    <div className="space-y-6 px-4 pt-6 lg:px-8">
      <div className="flex flex-col gap-4 border-b border-white/8 pb-5 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex items-center gap-3">
          <AmbraLogo
            size={40}
            showWordmark
            subtitle={t('admin.subtitleBrand')}
          />
        </div>

        <button
          type="button"
          disabled={scanning}
          onClick={handleGlobalScan}
          className="inline-flex items-center justify-center gap-2 rounded-xl bg-amber-500 px-4 py-2.5 text-sm font-semibold text-zinc-950 shadow-lg shadow-amber-500/20 transition-all hover:bg-amber-400 disabled:opacity-60"
        >
          <RefreshCw className={`h-4 w-4 ${scanning ? 'animate-spin' : ''}`} />
          <span>{scanning ? t('admin.scanningAllLibraries') : t('admin.scanAllLibraries')}</span>
        </button>
      </div>

      <FeedbackBanner
        message={scanFeedback?.message}
        type={scanFeedback?.type}
        onClose={() => setScanFeedback(null)}
      />

      <div className="flex flex-col gap-6 xl:flex-row">
        <aside className="shrink-0 xl:w-60">
          <nav
            ref={navRef}
            data-testid="admin-tab-nav"
            onScroll={(e) => {
              savedScrollLeftRef.current = e.currentTarget.scrollLeft
            }}
            className="-mx-1 flex gap-2 overflow-x-auto px-1 pb-4 pt-2 xl:mx-0 xl:flex-col xl:overflow-visible xl:p-0"
          >
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
                    <p className="truncate text-sm font-medium">{t(tab.labelKey)}</p>
                    <p className="hidden truncate text-[11px] text-zinc-500 xl:block">
                      {t(tab.descKey)}
                    </p>
                  </div>
                </NavLink>
              )
            })}
          </nav>
        </aside>

        {/* 右侧面板内容 */}
        <div className="min-w-0 flex-1 rounded-2xl border border-white/8 bg-white/[0.015] p-4 sm:p-6">
          <AdminTabErrorBoundary key={location.pathname}>
            <Routes>
              <Route index element={<Navigate to="dashboard" replace />} />
              <Route path="dashboard" element={<DashboardTab />} />
              <Route path="settings" element={<SettingsTab />} />
              <Route path="users" element={<UsersTab />} />
              <Route path="libraries" element={<LibrariesTab />} />
              <Route path="metadata" element={<MetadataTab />} />
              <Route path="devices" element={<DevicesKeysTab />} />
              <Route path="plugins" element={<PluginsTab />} />
              <Route path="tasks" element={<TasksLogsTab />} />
              <Route path="livetv" element={<LiveTvTab />} />
              <Route path="webhooks" element={<WebhooksTab />} />
              <Route path="health" element={<HealthAndAuditTab />} />
              <Route path="migration" element={<MigrationTab />} />
              <Route path="*" element={<Navigate to="dashboard" replace />} />
            </Routes>
          </AdminTabErrorBoundary>
        </div>
      </div>
    </div>
  )
}
