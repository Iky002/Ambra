import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query'
import { Loader2, ShieldAlert } from 'lucide-react'
import { HashRouter, Link, Navigate, Route, Routes } from 'react-router-dom'
import { api } from './api/client'
import { Layout } from './components/Layout'
import { ScrollToTop } from './components/ScrollToTop'
import { AudioPlayerProvider } from './context/AudioPlayerContext'
import { AuthProvider, useAuth } from './context/AuthContext'
import { WebSocketProvider } from './context/WebSocketContext'
import { isDemoMode } from './demo/demoMode'
import { I18nProvider, useI18n } from './i18n/I18nContext'
import { AdminPage } from './pages/AdminPage'
import { CollectionsPage } from './pages/CollectionsPage'
import { FavoritesPage } from './pages/FavoritesPage'
import { HomePage } from './pages/HomePage'
import { ItemDetailPage } from './pages/ItemDetailPage'
import { LibraryPage } from './pages/LibraryPage'
import { LiveTvPage } from './pages/LiveTvPage'
import { LoginPage } from './pages/LoginPage'
import { PersonDetailPage } from './pages/PersonDetailPage'
import { PlayerPage } from './pages/PlayerPage'
import { ProfilePage } from './pages/ProfilePage'
import { SearchPage } from './pages/SearchPage'
import { SetupWizardPage } from './pages/SetupWizardPage'
import { TrendingPage } from './pages/TrendingPage'

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      refetchOnWindowFocus: false,
      retry: 1,
    },
  },
})

function SetupLoadingScreen() {
  return (
    <div className="flex min-h-screen items-center justify-center bg-[#09090b] text-zinc-400">
      <Loader2 className="h-7 w-7 animate-spin text-amber-400" />
    </div>
  )
}

function RequireAuth({
  children,
  needsSetup = false,
  setupLoading = false,
}: {
  children: React.ReactNode
  needsSetup?: boolean
  setupLoading?: boolean
}) {
  const { auth } = useAuth()
  if (!auth) {
    if (setupLoading) {
      return <SetupLoadingScreen />
    }
    return <Navigate to={needsSetup ? '/setup' : '/login'} replace />
  }
  return <>{children}</>
}

function RequireAdmin({ children }: { children: React.ReactNode }) {
  const { isAdmin, userLoading } = useAuth()
  const { t } = useI18n()
  if (userLoading) {
    return (
      <div className="flex min-h-[60vh] items-center justify-center text-sm text-zinc-400">
        {t('auth.verifyingAdmin')}
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
          <h1 className="text-lg font-semibold text-zinc-100">{t('auth.noAdminTitle')}</h1>
          <p className="mt-2 text-xs leading-relaxed text-zinc-400">
            {t('auth.noAdminDesc')}
          </p>
          <Link
            to="/"
            replace
            className="mt-6 inline-flex items-center justify-center rounded-xl bg-amber-500 px-5 py-2.5 text-sm font-semibold text-zinc-950 transition-colors hover:bg-amber-400"
          >
            {t('auth.backHome')}
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

  const { data: setupStatus, isLoading: setupLoading } = useQuery({
    queryKey: ['setupStatus'],
    queryFn: () => api.getSetupStatus(),
    enabled: !auth && !demoActive,
    retry: false,
  })

  const waitingSetupStatus = !auth && !demoActive && setupLoading
  const needsSetup = !demoActive && setupStatus?.completed === false && !setupStatus?.has_users

  return (
    <Routes>
      <Route path="/setup" element={<SetupWizardPage />} />
      <Route
        path="/login"
        element={
          auth ? (
            <Navigate to="/" replace />
          ) : waitingSetupStatus ? (
            <SetupLoadingScreen />
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
          <RequireAuth needsSetup={needsSetup} setupLoading={waitingSetupStatus}>
            <PlayerPage />
          </RequireAuth>
        }
      />
      <Route
        path="/"
        element={
          <RequireAuth needsSetup={needsSetup} setupLoading={waitingSetupStatus}>
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
        <Route path="livetv" element={<LiveTvPage />} />
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
    <I18nProvider>
      <QueryClientProvider client={queryClient}>
        <AuthProvider>
          <WebSocketProvider>
            <AudioPlayerProvider>
              <HashRouter>
                <ScrollToTop />
                <AppRoutes />
              </HashRouter>
            </AudioPlayerProvider>
          </WebSocketProvider>
        </AuthProvider>
      </QueryClientProvider>
    </I18nProvider>
  )
}
