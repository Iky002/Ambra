import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  AlertCircle,
  ArrowRight,
  Loader2,
  Lock,
  Server,
  ShieldCheck,
  Sparkles,
  User,
  UserCheck,
} from 'lucide-react'
import { Navigate, useNavigate } from 'react-router-dom'
import { api, getUserImageUrl } from '../api/client'
import { AmbraLogo } from '../components/AmbraLogo'
import { useAuth } from '../context/AuthContext'
import { isDemoMode } from '../demo/demoMode'

export function LoginPage() {
  const { login } = useAuth()
  const navigate = useNavigate()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')
  const demoActive = isDemoMode()

  const handleDemoQuickLogin = async (role: 'admin' | 'guest') => {
    setError('')
    setSubmitting(true)
    try {
      await login(role, '')
      navigate('/', { replace: true })
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Demo 一键登录失败')
    } finally {
      setSubmitting(false)
    }
  }

  const { data: setupStatus } = useQuery({
    queryKey: ['setupStatus'],
    queryFn: () => api.getSetupStatus(),
    enabled: !demoActive,
    retry: false,
  })

  const { data: sysInfo } = useQuery({
    queryKey: ['publicSystemInfo'],
    queryFn: () => api.getPublicSystemInfo(),
    retry: 1,
  })

  const { data: publicUsers } = useQuery({
    queryKey: ['publicUsers'],
    queryFn: () => api.getPublicUsers(),
    retry: 1,
  })

  if (!demoActive && setupStatus && setupStatus.completed === false) {
    return <Navigate to="/setup" replace />
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!username.trim()) {
      setError('请输入用户名')
      return
    }
    setError('')
    setSubmitting(true)
    try {
      await login(username.trim(), password)
      navigate('/', { replace: true })
    } catch (err) {
      setError(err instanceof Error ? err.message : '登录失败，请检查用户名或密码')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="relative flex min-h-screen items-center justify-center overflow-hidden bg-[#07070b] px-4 py-10">
      {/* 影院氛围背景光晕 */}
      <div className="pointer-events-none absolute -left-32 -top-32 h-96 w-96 rounded-full bg-amber-500/15 blur-[130px]" />
      <div className="pointer-events-none absolute -bottom-32 -right-32 h-96 w-96 rounded-full bg-orange-600/15 blur-[130px]" />

      <div className="relative z-10 w-full max-w-md">
        {/* Logo 与服务器名称 */}
        <div className="mb-8 text-center">
          <div className="mb-4 flex justify-center">
            <AmbraLogo size={60} />
          </div>
          <h1 className="text-2xl font-bold tracking-tight text-zinc-50">
            {sysInfo?.ServerName || 'Ambra Media Server'}
          </h1>
          <p className="mt-1.5 flex items-center justify-center gap-1.5 text-xs text-zinc-400">
            <Server className="h-3.5 w-3.5 text-amber-400/80" />
            <span>
              {sysInfo?.ProductName || 'Ambra'} {sysInfo?.Version ? `v${sysInfo.Version}` : 'v0.1'}
            </span>
          </p>
        </div>

        {/* 公开用户快速选择 */}
        {publicUsers && publicUsers.length > 0 && (
          <div className="mb-6">
            <p className="mb-2.5 text-center text-xs font-medium text-zinc-400">选择账号快速登录</p>
            <div className="flex flex-wrap justify-center gap-3">
              {publicUsers.map((u) => {
                const selected = username === u.Name
                return (
                  <button
                    key={u.Id}
                    type="button"
                    onClick={() => {
                      setUsername(u.Name)
                      setError('')
                    }}
                    className={`group flex flex-col items-center gap-1.5 rounded-2xl px-3 py-2.5 transition-all ${
                      selected
                        ? 'bg-amber-500/15 ring-1 ring-amber-400/60'
                        : 'bg-white/[0.03] ring-1 ring-white/8 hover:bg-white/[0.07]'
                    }`}
                  >
                    <div className="flex h-12 w-12 items-center justify-center overflow-hidden rounded-full bg-gradient-to-br from-zinc-800 to-zinc-900 text-base font-bold text-amber-300 ring-2 ring-white/10">
                      {u.PrimaryImageTag ? (
                        <img
                          src={getUserImageUrl(u.Id, u.PrimaryImageTag)}
                          alt={u.Name}
                          className="h-full w-full object-cover"
                        />
                      ) : (
                        u.Name.slice(0, 1).toUpperCase()
                      )}
                    </div>
                    <span className="max-w-[76px] truncate text-xs font-medium text-zinc-200">
                      {u.Name}
                    </span>
                  </button>
                )
              })}
            </div>
          </div>
        )}

        {/* 登录表单卡片 */}
        <form
          onSubmit={handleSubmit}
          className="rounded-2xl border border-white/10 bg-zinc-950/80 p-6 shadow-2xl backdrop-blur-xl sm:p-7"
        >
          <div className="space-y-4">
            <div>
              <label className="mb-1.5 block text-xs font-medium text-zinc-400">用户名</label>
              <div className="relative">
                <User className="pointer-events-none absolute left-3.5 top-1/2 h-4 w-4 -translate-y-1/2 text-zinc-500" />
                <input
                  type="text"
                  autoComplete="username"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  placeholder="输入用户名"
                  className="w-full rounded-xl border border-white/10 bg-white/[0.04] py-2.5 pl-10 pr-4 text-sm text-zinc-100 placeholder-zinc-500 outline-none transition-colors focus:border-amber-400/60 focus:bg-white/[0.06]"
                />
              </div>
            </div>

            <div>
              <label className="mb-1.5 block text-xs font-medium text-zinc-400">密码</label>
              <div className="relative">
                <Lock className="pointer-events-none absolute left-3.5 top-1/2 h-4 w-4 -translate-y-1/2 text-zinc-500" />
                <input
                  type="password"
                  autoComplete="current-password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  placeholder="留空表示无密码账号"
                  className="w-full rounded-xl border border-white/10 bg-white/[0.04] py-2.5 pl-10 pr-4 text-sm text-zinc-100 placeholder-zinc-500 outline-none transition-colors focus:border-amber-400/60 focus:bg-white/[0.06]"
                />
              </div>
            </div>

            {error && (
              <div className="flex items-start gap-2 rounded-xl border border-red-500/30 bg-red-500/10 px-3.5 py-2.5 text-xs text-red-300">
                <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
                <span>{error}</span>
              </div>
            )}

            <button
              type="submit"
              disabled={submitting}
              className="mt-2 flex w-full items-center justify-center gap-2 rounded-xl bg-gradient-to-r from-amber-500 to-orange-500 py-2.5 text-sm font-semibold text-zinc-950 shadow-lg shadow-amber-500/25 transition-all hover:from-amber-400 hover:to-orange-400 disabled:opacity-60"
            >
              {submitting ? (
                <>
                  <Loader2 className="h-4 w-4 animate-spin" />
                  <span>正在连接影院...</span>
                </>
              ) : (
                <>
                  <span>登录 Ambra</span>
                  <ArrowRight className="h-4 w-4" />
                </>
              )}
            </button>
          </div>
        </form>

        {/* Serverless Demo 一键登录面板（仅 Demo 模式渲染） */}
        {demoActive && (
          <div className="mt-4 rounded-2xl border border-amber-500/25 bg-amber-500/[0.06] p-4 backdrop-blur-xl">
            <div className="mb-3 flex items-center gap-1.5 text-xs font-semibold text-amber-300">
              <Sparkles className="h-3.5 w-3.5" />
              <span>Serverless Demo 一键体验（无需密码）</span>
            </div>
            <div className="grid grid-cols-2 gap-2.5">
              <button
                type="button"
                disabled={submitting}
                onClick={() => void handleDemoQuickLogin('admin')}
                className="inline-flex items-center justify-center gap-1.5 rounded-xl bg-amber-500/20 px-3 py-2 text-xs font-semibold text-amber-200 ring-1 ring-amber-400/35 transition-colors hover:bg-amber-500/30 disabled:opacity-50"
              >
                <ShieldCheck className="h-3.5 w-3.5" />
                <span>管理员 (admin)</span>
              </button>
              <button
                type="button"
                disabled={submitting}
                onClick={() => void handleDemoQuickLogin('guest')}
                className="inline-flex items-center justify-center gap-1.5 rounded-xl bg-white/[0.06] px-3 py-2 text-xs font-medium text-zinc-200 ring-1 ring-white/12 transition-colors hover:bg-white/12 disabled:opacity-50"
              >
                <UserCheck className="h-3.5 w-3.5" />
                <span>普通用户 (guest)</span>
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
