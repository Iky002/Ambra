import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query'
import { ShieldAlert } from 'lucide-react'
import { HashRouter, Link, Navigate, Route, Routes } from 'react-router-dom'
import { api } from './api/client'
import { Layout } from './components/Layout'
import { AuthProvider, useAuth } from './context/AuthContext'
import { isDemoMode } from './demo/demoMode'
import { AdminPage } from './pages/AdminPage'
import { CollectionsPage } from './pages/CollectionsPage'
import { FavoritesPage } from './pages/FavoritesPage'
import { HomePage } from './pages/HomePage'
import { ItemDetailPage } from './pages/ItemDetailPage'
import { LibraryPage } from './pages/LibraryPage'
import { LoginPage } from './pages/LoginPage'
import { PersonDetailPage } from './pages/PersonDetailPage'
import { PlayerPage } from './pages/PlayerPage'
import { ProfilePage } from './pages/ProfilePage'
import { SearchPage } from './pages/SearchPage'
import { SetupWizardPage } from './pages/SetupWizardPage'
import { TrendingPage } from './pages/TrendingPage'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      refetchOnWindowFocus: false,
      retry: 1,
    },
  },
})

function RequireAuth({
  children,
  needsSetup = false,
}: {
  children: React.ReactNode
  needsSetup?: boolean
}) {
  const { auth } = useAuth()
  if (!auth) {
    return <Navigate to={needsSetup ? '/setup' : '/login'} replace />
  }
  return <>{children}</>
}

function RequireAdmin({ children }: { children: React.ReactNode }) {
  const { isAdmin, userLoading } = useAuth()
  if (userLoading) {
    return (
      <div className="flex min-h-[60vh] items-center justify-center text-sm text-zinc-400">
        正在校验管理员权限...
      </div>
    )
  }
  if (!isAdmin) {
    return (
      <div className="flex min-h-[70vh] items-center justify-center px-4">
        <div className="w-full max-w-md rounded-2xl border border-amber-500/25 bg-zinc-950/90 p-8 text-center shadow-2xl">
          <div className="mx-auto mb-4 flex h-14 w-14 items-center justify-center rounded-2xl bg-amber-500/15 text-amber-400 ring-1 ring-amber-500/30">
            <ShieldAlert className="h-7 w-7" />
          </div>
          <h1 className="text-lg font-semibold text-zinc-100">无权访问管理控制台</h1>
          <p className="mt-2 text-xs leading-relaxed text-zinc-400">
            当前账号不具备管理员权限（IsAdministrator = false）。如需管理服务器配置、用户或媒体库，请联系系统管理员授权。
          </p>
          <Link
            to="/"
            replace
            className="mt-6 inline-flex items-center justify-center rounded-xl bg-amber-500 px-5 py-2.5 text-sm font-semibold text-zinc-950 transition-colors hover:bg-amber-400"
          >
            返回首页
          </Link>
        </div>
      </div>
    )
  }
  return <>{children}</>
}

function AppRoutes() {
  const { auth } = useAuth()
  const demoActive = isDemoMode()

  const { data: setupStatus } = useQuery({
    queryKey: ['setupStatus'],
    queryFn: () => api.getSetupStatus(),
    enabled: !auth && !demoActive,
    retry: false,
  })

  const needsSetup = !demoActive && setupStatus?.completed === false

  return (
    <Routes>
      <Route path="/setup" element={<SetupWizardPage />} />
      <Route
        path="/login"
        element={
          auth ? (
            <Navigate to="/" replace />
          ) : needsSetup ? (
            <Navigate to="/setup" replace />
          ) : (
            <LoginPage />
          )
        }
      />
      <Route
        path="/play/:id"
        element={
          <RequireAuth needsSetup={needsSetup}>
            <PlayerPage />
          </RequireAuth>
        }
      />
      <Route
        path="/"
        element={
          <RequireAuth needsSetup={needsSetup}>
            <Layout />
          </RequireAuth>
        }
      >
        <Route index element={<HomePage />} />
        <Route path="trending" element={<TrendingPage />} />
        <Route path="collections" element={<CollectionsPage />} />
        <Route path="collections/:id" element={<CollectionsPage />} />
        <Route path="person/:id" element={<PersonDetailPage />} />
        <Route path="library/:id" element={<LibraryPage />} />
        <Route path="item/:id" element={<ItemDetailPage />} />
        <Route path="favorites" element={<FavoritesPage />} />
        <Route path="search" element={<SearchPage />} />
        <Route path="profile" element={<ProfilePage />} />
        <Route
          path="admin/*"
          element={
            <RequireAdmin>
              <AdminPage />
            </RequireAdmin>
          }
        />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <AuthProvider>
        <HashRouter>
          <AppRoutes />
        </HashRouter>
      </AuthProvider>
    </QueryClientProvider>
  )
}
